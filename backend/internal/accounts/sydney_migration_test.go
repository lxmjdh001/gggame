package accounts

import (
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func TestSydney49MigrationPreservesLatestQuotesAndPendingSnapshots(t *testing.T) {
	s, path, _ := fundsFixture(t)
	g, _ := games.Find("lhc-au")
	oldTime := games.Sydney49Start().Add(-time.Hour + 10*time.Second)
	stake := int64(200)
	in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(oldTime).Format(model.IssueLayout), MarketID: "au45.p8.n45", OddsMilli: 44100, UnitCents: 200, AmountCents: &stake, Key: randomID()}
	order, err := s.PlaceMarketOrder("member", in, oldTime)
	must(t, err)
	oldQuotes := []Quote{}
	for _, m := range markets.Catalog("lhc-au") {
		oldQuotes = append(oldQuotes, Quote{MarketID: m.ID, OddsMilli: m.DefaultOdds, Enabled: true})
	}
	for i := range oldQuotes {
		switch oldQuotes[i].MarketID {
		case "au45.p1.odd":
			oldQuotes[i].OddsMilli = 1876
			oldQuotes[i].Enabled = false
		case "au45.p7.n45":
			oldQuotes[i].OddsMilli = 64000
		case "au45.p8.n1":
			oldQuotes[i].OddsMilli = 73000
		}
	}
	// Earlier 49-number quotes are archived with the same ID as the new ones.
	// Current au45 settings must win when both exist; unmatched 46–49 are kept.
	oldQuotes = append(oldQuotes, Quote{MarketID: "p7.n45", OddsMilli: 82000, Enabled: false}, Quote{MarketID: "p7.n46", OddsMilli: 83000, Enabled: false})
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("rule_migrations")).Delete([]byte("au49-v1")); err != nil {
			return err
		}
		return put(tx, "market_quotes", g.ID, oldQuotes)
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	defer func() { s.Close() }()
	got, err := s.Order(order.ID)
	must(t, err)
	if !reflect.DeepEqual(got.RuleSnapshot, order.RuleSnapshot) || got.OddsMilli != order.OddsMilli || got.Status != "pending" {
		t.Fatal("migration changed accepted legacy order", got)
	}
	var stored []Quote
	must(t, s.db.View(func(tx *bolt.Tx) error {
		var err error
		stored, err = get[[]Quote](tx, "market_quotes", g.ID)
		return err
	}))
	byID := map[string]Quote{}
	for _, q := range stored {
		if _, dup := byID[q.MarketID]; dup {
			t.Fatal("duplicated migrated quote", q.MarketID)
		}
		byID[q.MarketID] = q
	}
	for _, m := range markets.Catalog("lhc") {
		q, ok := byID[m.ID]
		if !ok {
			t.Fatal("missing new quote", m.ID)
		}
		if m.Kind == "parity" && len(m.Positions) == 1 && m.Positions[0] == 1 && m.Pick == "odd" && (q.OddsMilli != 1876 || q.Enabled) {
			t.Fatal("latest positional parity setting lost", q)
		}
		if strings.HasPrefix(m.ID, "lhc5.combined_zodiac.") && (q.OddsMilli != 1000 || !q.Enabled) {
			t.Fatal("new combined set default incorrect", q)
		}
	}
	if byID["p7.n45"].OddsMilli != 64000 || !byID["p7.n45"].Enabled || byID["p7.n46"].OddsMilli != 83000 || byID["p7.n46"].Enabled || byID["au45.p8.n1"].OddsMilli != 73000 {
		t.Fatal("position or retired quote incorrectly migrated")
	}
	must(t, s.SettleMarket(order.ID, marketRound(in, []int{1, 2, 3, 4, 5, 6, 23, 45})))
	settled, err := s.Order(order.ID)
	must(t, err)
	if settled.Outcome != "win" || settled.Payout != 8820 || settled.OddsMilli != 44100 {
		t.Fatal("legacy 45/8 order no longer settles from its snapshot", settled)
	}
	// Reopening may add unrelated new quotes, but cannot rerun inheritance or
	// replace later administrator settings on an already migrated selection.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := range stored {
			if stored[i].MarketID == "p7.n45" {
				stored[i].OddsMilli = 55555
			}
		}
		return put(tx, "market_quotes", g.ID, stored)
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	must(t, s.db.View(func(tx *bolt.Tx) error {
		v, err := get[[]Quote](tx, "market_quotes", g.ID)
		if err != nil {
			return err
		}
		for _, q := range v {
			if q.MarketID == "p7.n45" && q.OddsMilli != 55555 {
				t.Fatal("migration reran after restart", q)
			}
		}
		return nil
	}))
}

func TestSydney49NewPeriodsAcceptStandardAndCombinedMarkets(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	g, _ := games.Find("lhc-au")
	now := games.Sydney49Start().Add(10 * time.Second)
	stake := int64(200)
	for _, tc := range []struct {
		id     string
		values []int
		odds   int64
	}{
		{"p7.n49", nil, 48020}, {"lhc6.combined_zodiac.in.2", []int{5, 6}, 1000},
	} {
		in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: tc.id, OddsMilli: tc.odds, Values: tc.values, UnitCents: 200, AmountCents: &stake, Key: randomID()}
		order, err := s.PlaceMarketOrder("member", in, now)
		must(t, err)
		outcome, payout, err := calculateMarket(order, marketRound(in, []int{1, 2, 3, 4, 5, 6, 49}))
		must(t, err)
		wantOutcome := "win"
		if strings.HasPrefix(tc.id, "lhc6.") {
			wantOutcome = "push"
		}
		if outcome != wantOutcome || payout != stake*tc.odds/1000 {
			t.Fatal("new Sydney period failed to use standard special ball", outcome, payout)
		}
	}
	stale := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: "au45.p7.n45", OddsMilli: 44100, UnitCents: 200, AmountCents: &stake, Key: randomID()}
	if _, err := s.PlaceMarketOrder("member", stale, now); err == nil {
		t.Fatal("old 45/8 browser selection accepted for new49 period")
	}
	oldTime := games.Sydney49Start().Add(-time.Hour + 10*time.Second)
	stale.MarketID = "lhc6.combined_zodiac.in.2"
	stale.Values = []int{5, 6}
	stale.OddsMilli = 1000
	stale.Issue = g.Next(oldTime).Format(model.IssueLayout)
	stale.Key = randomID()
	if _, err := s.PlaceMarketOrder("member", stale, oldTime); err == nil {
		t.Fatal("new combined rule accepted for old45 period")
	}
}

func TestSydney49UpcomingQuotesFollowPreCutoverAdminChanges(t *testing.T) {
	quotes := []Quote{
		{MarketID: "au45.p7.n45", OddsMilli: 54321, Enabled: false}, {MarketID: "p7.n45", OddsMilli: 48020, Enabled: true},
		{MarketID: "p7.n49", OddsMilli: 62000, Enabled: false}, {MarketID: "au45.p8.n1", OddsMilli: 70000, Enabled: false},
		{MarketID: "lhc5.combined_zodiac.in.2", OddsMilli: 1880, Enabled: true},
	}
	originalQuotes := append([]Quote(nil), quotes...)
	updated := syncUpcomingSydney49Quotes(quotes)
	byID := map[string]Quote{}
	for _, q := range updated {
		byID[q.MarketID] = q
	}
	if byID["p7.n45"].OddsMilli != 54321 || byID["p7.n45"].Enabled {
		t.Fatal("pre-cutover administrator edit did not reach upcoming standard quote")
	}
	for _, id := range []string{"p7.n49", "au45.p8.n1", "lhc5.combined_zodiac.in.2"} {
		var original Quote
		for _, q := range originalQuotes {
			if q.MarketID == id {
				original = q
			}
		}
		if !reflect.DeepEqual(byID[id], original) {
			t.Fatal("unmatched or retired quote changed", id)
		}
	}
	if _, ok := byID["p8.n1"]; ok {
		t.Fatal("retired second supplementary ball leaked into new standard book")
	}
}

func TestSydney49ClassicNumbersAndStatusUseIssueRule(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	g, _ := games.Find("lhc-au")
	stake := int64(200)
	for _, tc := range []struct {
		name       string
		now        time.Time
		max, count int
		catalog    string
	}{
		{"legacy", games.Sydney49Start().Add(-time.Hour + 10*time.Second), 45, 8, "lhc-au"},
		{"first_new_issue", games.Sydney49Start().Add(-14*time.Minute - 50*time.Second), 49, 7, "lhc"},
		{"after_boundary", games.Sydney49Start().Add(10 * time.Second), 49, 7, "lhc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			due := g.Next(tc.now)
			numbers := []int{1, 2, 3, 4, 5, tc.max}
			in := OrderInput{SessionID: g.ID, Issue: due.Format(model.IssueLayout), Numbers: numbers, UnitCents: 200, AmountCents: &stake, Key: randomID()}
			if !validateNumbersAt(g, in.Numbers, due) {
				t.Fatal("historical number validator rejected valid classic selection")
			}
			if _, err := s.PlaceOrder("member", in, tc.now); err == nil {
				t.Fatal("retired classic entry still accepts new orders")
			}
			in.Numbers = []int{1, 2, 3, 4, 5, tc.max + 1}
			in.Key = randomID()
			if _, err := s.PlaceOrder("member", in, tc.now); err == nil {
				t.Fatal("out-of-range classic selection accepted")
			}
			book, err := s.marketBookAt(g, due)
			must(t, err)
			if len(book.Markets) != len(markets.Catalog(tc.catalog)) || len(book.Quotes) != len(book.Markets) {
				t.Fatal("book/quotes use wrong issue catalog")
			}
			if (book.MarkSixContext != nil) != (tc.catalog == "lhc") {
				t.Fatal("context does not follow issue format")
			}
			status, err := s.UserStatus("member", tc.now, nil)
			must(t, err)
			found := false
			for _, row := range status.Sessions {
				if row.SessionID == g.ID {
					found = true
					if row.NumberRule.Count != tc.count || row.NumberRule.Min != 1 || row.NumberRule.Max != tc.max || row.NumberRule.AllowRepeat || row.Issue != due.Format(model.IssueLayout) {
						t.Fatal("preview status uses current rule for historical/new period", row)
					}
				}
			}
			if !found {
				t.Fatal("missing Sydney preview status")
			}
		})
	}
	assertFunds(t, s, "member", 100000, 0, 0)
}
