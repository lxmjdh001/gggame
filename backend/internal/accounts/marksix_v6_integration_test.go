package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func TestMarkSixV6QuoteMigrationArchivesPricesAndOnlyAddsMissingTiers(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	old := map[string]map[string]Quote{}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, g := range games.All() {
			if g.GameID != "lhc" {
				continue
			}
			qs := []Quote{}
			old[g.ID] = map[string]Quote{}
			for i, m := range markets.LegacyMarkSixCatalog() {
				q := Quote{MarketID: m.ID, OddsMilli: 2000 + int64(i), Enabled: i%3 != 0}
				qs = append(qs, q)
				old[g.ID][q.MarketID] = q
			}
			// Already persisted replacement prices take precedence over any legacy quote.
			qs = append(qs, Quote{MarketID: "lhc6.p1.big", OddsMilli: 8888, Enabled: false}, Quote{MarketID: "lhc6.link.three_two", OddsMilli: 1999, Enabled: false, TierOddsMilli: map[string]int64{"hit2": 7777}})
			if err := put(tx, "market_quotes", g.ID, qs); err != nil {
				return err
			}
		}
		return nil
	}))
	before := markSixCombinedStoredState(t, s)
	must(t, s.Close())
	var err error
	s, err = Open(path)
	must(t, err)
	after := markSixCombinedStoredState(t, s)
	for name, values := range before {
		if name == "market_quotes" || name == "audit" {
			continue
		}
		if !reflect.DeepEqual(values, after[name]) {
			t.Fatal("migration changed unrelated bucket", name)
		}
	}
	for _, g := range games.All() {
		if g.GameID != "lhc" {
			if !reflect.DeepEqual(before["market_quotes"][g.ID], after["market_quotes"][g.ID]) {
				t.Fatal("other game quote changed", g.ID)
			}
			continue
		}
		var quotes []Quote
		must(t, json.Unmarshal(after["market_quotes"][g.ID], &quotes))
		byID := map[string]Quote{}
		for _, q := range quotes {
			byID[q.MarketID] = q
		}
		for id, previous := range old[g.ID] {
			if !reflect.DeepEqual(previous, byID[id]) {
				t.Fatal("archived quote changed", g.ID, id)
			}
		}
		for _, m := range markets.Catalog("lhc") {
			q, ok := byID[m.ID]
			if !ok {
				t.Fatal("missing current quote", g.ID, m.ID)
			}
			switch m.ID {
			case "lhc6.p1.big":
				if q.OddsMilli != 8888 || q.Enabled {
					t.Fatal("existing replacement reset", q)
				}
			case "lhc6.link.three_two":
				if q.OddsMilli != 1999 || q.Enabled || !reflect.DeepEqual(q.TierOddsMilli, map[string]int64{"hit2": 7777, "hit3": 1000}) {
					t.Fatal("partial tiers reset", q)
				}
			default:
				if previous, exists := old[g.ID][m.ID]; exists {
					if !reflect.DeepEqual(q, previous) {
						t.Fatal("unchanged current quote reset", q)
					}
				} else if ids := markSixV6PreviousIDs(m.ID); len(ids) > 0 {
					previous := old[g.ID][ids[0]]
					if q.OddsMilli != previous.OddsMilli || q.Enabled != previous.Enabled {
						t.Fatal("custom migration lost", m.ID, q, previous)
					}
				} else {
					if q.OddsMilli != m.DefaultOdds || !q.Enabled {
						t.Fatal("new default wrong", q)
					}
					for _, tier := range m.PayoutTiers {
						if q.TierOddsMilli[tier.Key] != 1000 {
							t.Fatal("new tier default wrong", q)
						}
					}
				}
			}
		}
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	if !reflect.DeepEqual(after, markSixCombinedStoredState(t, s)) {
		t.Fatal("second open reran migration or changed persisted quote maps")
	}
}

func TestMarkSixV6RetiredClassicRejectsNewButReplaysAndSettlesOld(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	now := markSixV6Time()
	g, _ := games.Find("lhc-tw")
	stake := int64(200)
	oldInput := OrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), Numbers: []int{1, 2, 3, 4, 5, 6}, UnitCents: 200, AmountCents: &stake, Key: randomID()}
	before := markSixCombinedStoredState(t, s)
	if _, err := s.PlaceOrder("member", oldInput, now); err == nil || !strings.Contains(err.Error(), "已下架") {
		t.Fatal("retired new classic accepted", err)
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("rejected legacy entry charged funds")
	}
	// Reserve real funds through the remaining classic entry, then recreate an
	// archived pre-release LHC order plus its exact accepted request fingerprint.
	seed := input(now)
	seed.AmountCents = &stake
	seed.Multiplier = 0
	order, err := s.PlaceOrder("member", seed, now)
	must(t, err)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := releaseIssueStake(tx, order); err != nil {
			return err
		}
		if err := reserveIssueStake(tx, "member", g.ID, oldInput.Issue, stake, defaultUserIssueLimit); err != nil {
			return err
		}
		order.SessionID = g.ID
		order.SessionName = g.Name
		order.GameName = g.GameName
		order.Issue = oldInput.Issue
		order.Numbers = oldInput.Numbers
		if err := put(tx, "orders", order.ID, order); err != nil {
			return err
		}
		return remember(tx, "order:member", oldInput.Key, order.ID, oldInput)
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	retry, err := s.PlaceOrder("member", oldInput, now.Add(time.Hour))
	must(t, err)
	if retry.ID != order.ID {
		t.Fatal("old retry failed")
	}
	assertFunds(t, s, "member", 99800, 200, 0)
	must(t, s.SettleOrder("admin", order.ID, "settled", "保留历史六号单式人工结算", 500, true))
	assertFunds(t, s, "member", 100300, 0, 0)
	retry, err = s.PlaceOrder("member", oldInput, now.Add(time.Hour))
	must(t, err)
	if retry.ID != order.ID || retry.Payout != 500 {
		t.Fatal("settled retry failed")
	}
	oldInput.Key = randomID()
	if _, err = s.PlaceOrder("member", oldInput, now); err == nil {
		t.Fatal("old browser may still create new classic")
	}
}

func TestMarkSixV6HTTPPreservesTierMapsAndRejectsIncompleteAdminQuote(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	markSixV6Prices(t, s)
	tokens := map[string]string{}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, id := range []string{"admin", "member"} {
			token, err := newLogin(tx, id)
			if err != nil {
				return err
			}
			tokens[id] = token
		}
		return nil
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	call := func(method, path, actor string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		must(t, err)
		r := httptest.NewRequest(method, "http://localhost:5173"+path, strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:5173")
		if actor != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tokens[actor]})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	get := call("GET", "/api/v1/markets/lhc-tw", "", nil)
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"tier_odds_milli":{"hit2":3500,"hit3":5250}`) || !strings.Contains(get.Body.String(), `"payout_tiers"`) {
		t.Fatal("market wire lacks tier contract", get.Code, get.Body.String())
	}
	book, err := s.MarketBook("lhc-tw")
	must(t, err)
	valid := call("PUT", "/api/v1/admin/markets/lhc-tw", "admin", map[string]any{"quotes": book.Quotes})
	if valid.Code != 200 {
		t.Fatal(valid.Code, valid.Body.String())
	}
	for i := range book.Quotes {
		if book.Quotes[i].MarketID == "lhc6.link.three_two" {
			book.Quotes[i].TierOddsMilli = map[string]int64{"hit2": 3500}
		}
	}
	invalid := call("PUT", "/api/v1/admin/markets/lhc-tw", "admin", map[string]any{"quotes": book.Quotes})
	if invalid.Code != 400 {
		t.Fatal("HTTP accepted partial tiers", invalid.Code, invalid.Body.String())
	}
	// Exercise accepted-order replay over HTTP independently of the current real
	// cutoff clock. The store tests cover fresh acceptance and all stale tiers.
	in := markSixV6Input(t, s, "lhc6.link.three_two", []int{1, 13, 25})
	order, err := s.PlaceMarketOrder("member", in, markSixV6Time())
	must(t, err)
	response := call("POST", "/api/v1/market-orders", "member", in)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"tier_odds_milli":{"hit2":3500,"hit3":5250}`) || !strings.Contains(response.Body.String(), order.ID) {
		t.Fatal("HTTP lost accepted tiers", response.Code, response.Body.String())
	}
	malformed := map[string]any{"tier_odds_milli": map[string]any{"hit2": "1.85", "hit3": 5250}}
	response = call("POST", "/api/v1/market-orders", "member", malformed)
	if response.Code != 400 {
		t.Fatal("malformed tier JSON accepted", response.Code)
	}
	must(t, s.SettleMarket(order.ID, marketRound(in, []int{1, 13, 25, 37, 49, 2, 14})))
	response = call("POST", "/api/v1/market-orders", "member", in)
	for _, field := range []string{`"payout_tier":"hit3"`, `"payout_multiplier":1`, `"payout_odds_milli":5250`} {
		if response.Code != 200 || !strings.Contains(response.Body.String(), field) {
			t.Fatal("HTTP lacks explainable payout", field, response.Body.String())
		}
	}
}
