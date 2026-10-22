package accounts

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestK3V3MigrationPreservesPricesSwitchesAndV2Orders(t *testing.T) {
	s, path, now := fundsFixture(t)
	defer func() { s.Close() }()
	g, _ := games.Find("k3-1")
	type accepted struct {
		order Order
		input MarketOrderInput
		face  int
	}
	var oldOrders []accepted
	// Recreate orders accepted under v2, including their idempotency records.
	// Real stakes remain reserved; the upgrade must not mutate these records.
	for _, kind := range []string{"single", "double"} {
		for face := 1; face <= 6; face++ {
			in := MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: fmt.Sprintf("k3v3.single.n%d", face), OddsMilli: 1000, UnitCents: 200, Multiplier: 1, Key: randomID()}
			o, err := s.PlaceMarketOrder("member", in, now)
			must(t, err)
			in.MarketID = fmt.Sprintf("k3v2.%s.n%d", kind, face)
			if kind == "double" {
				o.RuleSnapshot.Kind = "dice_double"
				o.RuleSnapshot.Group = "短牌"
				o.RuleSnapshot.Label = fmt.Sprintf("%d-%d", face, face)
			}
			in.OddsMilli, in.Key = 2345, randomID()
			o.MarketID, o.OddsMilli = in.MarketID, in.OddsMilli
			o.RuleSnapshot.ID, o.RuleSnapshot.RuleVersion = in.MarketID, 2
			o.RuleSnapshot.Rule = "旧版三军、短牌在对应围骰出现时中奖，每注派彩一次。"
			must(t, s.db.Update(func(tx *bolt.Tx) error {
				if err := put(tx, "orders", o.ID, o); err != nil {
					return err
				}
				return remember(tx, "market_order:member", in.Key, o.ID, in)
			}))
			oldOrders = append(oldOrders, accepted{o, in, face})
		}
	}

	storedBefore := map[string][]Quote{}
	expected := map[string][]Quote{}
	settings, err := s.Settings()
	must(t, err)
	for _, session := range games.All() {
		book, err := s.MarketBook(session.ID)
		must(t, err)
		if session.GameID != "k3" {
			storedBefore[session.ID] = k3StoredQuotes(t, s, session.ID)
			continue
		}
		var legacy, want []Quote
		for i, q := range book.Quotes {
			q.OddsMilli, q.Enabled = 1800+int64(i*17), i%3 != 0
			want = append(want, q)
			q.MarketID = strings.Replace(q.MarketID, "k3v3.", "k3v2.", 1)
			legacy = append(legacy, q)
		}
		for _, m := range k3RetiredPairMarkets() {
			legacy = append(legacy, Quote{MarketID: m.ID, OddsMilli: 2111, Enabled: false})
		}
		// An already migrated quote takes precedence over its retired v2 quote.
		if session.ID == g.ID {
			already := Quote{MarketID: "k3v3.single.n1", OddsMilli: 3777, Enabled: false}
			legacy = append(legacy, already)
			for i := range want {
				if want[i].MarketID == already.MarketID {
					want[i] = already
				}
			}
		}
		expected[session.ID], storedBefore[session.ID] = want, legacy
		must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "market_quotes", session.ID, legacy) }))
	}
	if len(expected) != 9 {
		t.Fatal("expected nine K3 rooms")
	}
	before := pk10SizeFinancialState(t, s)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	if !reflect.DeepEqual(before, pk10SizeFinancialState(t, s)) {
		t.Fatal("K3 migration changed financial records or idempotency keys")
	}
	afterSettings, err := s.Settings()
	must(t, err)
	if !reflect.DeepEqual(settings, afterSettings) {
		t.Fatal("K3 migration changed room settings")
	}
	for id, want := range expected {
		book, err := s.MarketBook(id)
		must(t, err)
		if len(book.Markets) != 31 || len(book.Quotes) != 31 || !reflect.DeepEqual(pk10SizeQuoteMap(book.Quotes), pk10SizeQuoteMap(want)) {
			t.Fatalf("%s: active prices or switches not preserved", id)
		}
		stored := k3StoredQuotes(t, s, id)
		if len(stored) != 64 || !reflect.DeepEqual(stored[:len(storedBefore[id])], storedBefore[id]) {
			t.Fatalf("%s: migration must retain original quotes and add only missing replacements", id)
		}
		// Customizing a replacement must persist across every subsequent restart.
		for i := range book.Quotes {
			if strings.HasPrefix(book.Quotes[i].MarketID, "k3v3.") {
				book.Quotes[i].OddsMilli += 123
				book.Quotes[i].Enabled = !book.Quotes[i].Enabled
			}
		}
		must(t, s.SaveQuotes("admin", id, book.Quotes))
		expected[id] = book.Quotes
	}
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	for _, session := range games.All() {
		if session.GameID != "k3" {
			if !reflect.DeepEqual(storedBefore[session.ID], k3StoredQuotes(t, s, session.ID)) {
				t.Fatal("migration changed another game's quotes", session.ID)
			}
			continue
		}
		book, err := s.MarketBook(session.ID)
		must(t, err)
		if !reflect.DeepEqual(book.Quotes, expected[session.ID]) {
			t.Fatal("restart reset configured replacement odds", session.ID)
		}
		stored := k3StoredQuotes(t, s, session.ID)
		if len(stored) != 64 {
			t.Fatal("restart appended duplicate quotes", session.ID)
		}
		quoteMap := pk10SizeQuoteMap(stored)
		for _, q := range storedBefore[session.ID] {
			if !strings.HasPrefix(q.MarketID, "k3v3.") && !reflect.DeepEqual(quoteMap[q.MarketID], q) {
				t.Fatal("saving replacement odds changed retired quotes", session.ID, q)
			}
		}
	}
	var paid int64
	for _, old := range oldOrders {
		stored, err := s.Order(old.order.ID)
		must(t, err)
		if !reflect.DeepEqual(stored, old.order) {
			t.Fatal("migration changed accepted v2 order", stored.ID)
		}
		retry, err := s.PlaceMarketOrder("member", old.input, now)
		must(t, err)
		if retry.ID != old.order.ID {
			t.Fatal("retired v2 retry created a second order")
		}
		round := marketRound(old.input, []int{old.face, old.face, old.face})
		must(t, s.SettleMarket(old.order.ID, round))
		must(t, s.SettleMarket(old.order.ID, round))
		settled, err := s.Order(old.order.ID)
		must(t, err)
		if settled.Outcome != "win" || settled.Payout != 469 || settled.OddsMilli != 2345 || !reflect.DeepEqual(settled.RuleSnapshot, old.order.RuleSnapshot) {
			t.Fatalf("accepted v2 triple must retain its original single payout: %+v", settled)
		}
		paid += settled.Payout
	}
	assertFunds(t, s, "member", 100000-int64(len(oldOrders))*200+paid, 0, 0)
}

func TestK3V3AllTriplesPreviewPreparedSettlementAndReplay(t *testing.T) {
	for face := 1; face <= 6; face++ {
		t.Run(fmt.Sprintf("%d%d%d", face, face, face), func(t *testing.T) {
			s, path, _ := fundsFixture(t)
			defer func() { s.Close() }()
			g, _ := games.Find("k3-1")
			now := time.Now().Add(-24 * time.Hour).Truncate(time.Hour).Add(10 * time.Second)
			at := g.Next(now)
			in := MarketBatchInput{SessionID: g.ID, Issue: at.Format(model.IssueLayout), UnitCents: 200, AmountCents: 200, Key: randomID()}
			for _, id := range k3CurrentIDs() {
				in.Selections = append(in.Selections, MarketSelection{MarketID: id, OddsMilli: 1000})
			}
			placed, err := s.PlaceMarketBatch("member", in, now)
			must(t, err)
			round := marketRound(MarketOrderInput{SessionID: g.ID, Issue: in.Issue}, []int{face, face, face})
			reader := &batchReader{rounds: map[string]games.Round{g.ID: round}}
			var forecast NextEstimate
			for _, o := range placed.Orders {
				forecast.add(o, &round)
			}
			winnerIDs := map[string]bool{fmt.Sprintf("k3v2.triple.n%d", face): true, "k3v2.triple.any": true}
			if face >= 2 && face <= 5 {
				winnerIDs[fmt.Sprintf("k3v2.sum.n%d", face*3)] = true
			}
			wantPayout := int64(len(winnerIDs)) * 200
			if forecast.Orders != 31 || forecast.UnresolvedOrders != 0 || forecast.Payout != wantPayout {
				t.Fatalf("preview must pay only matching triple/full triple/offered sum: %+v", forecast)
			}
			must(t, s.discoverSettlementPlans(at))
			plans, err := s.settlementPlans()
			must(t, err)
			if len(plans) != 1 {
				t.Fatal("expected one settlement plan", plans)
			}
			plan := plans[0]
			users := planUsers(plan)
			must(t, s.prepareUsers(plan, users, reader))
			if preparedCount(t, s) != 1 {
				t.Fatal("missing private prepared payout")
			}
			assertFunds(t, s, "member", 100000-31*200, 31*200, 0)
			must(t, s.Close())
			s, err = Open(path)
			must(t, err)
			must(t, s.commitUsers(plan, users, reader.rounds, at.Add(time.Second)))
			must(t, s.commitUsers(plan, users, reader.rounds, at.Add(time.Second)))
			for _, o := range placed.Orders {
				settled, err := s.Order(o.ID)
				must(t, err)
				wantOutcome, want := "lose", int64(0)
				if winnerIDs[o.MarketID] {
					wantOutcome, want = "win", 200
				}
				if settled.Outcome != wantOutcome || settled.Payout != want || !reflect.DeepEqual(settled.RuleSnapshot, o.RuleSnapshot) {
					t.Fatalf("%s incorrect prepared settlement: %+v", o.MarketID, settled)
				}
			}
			assertFunds(t, s, "member", 100000-31*200+wantPayout, 0, 0)
		})
	}
}
