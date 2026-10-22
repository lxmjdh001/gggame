package accounts

import (
	"path/filepath"
	"reflect"
	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestK3RegionalMigrationPreservesExistingSettingsAndOdds(t *testing.T) {
	s, path, _ := fundsFixture(t)
	cfg, err := s.Settings()
	must(t, err)
	newIDs := map[string]bool{"k3-zj": true, "k3-fj": true, "k3-gd": true, "k3-hn": true, "k3-tw": true, "k3-gx": true}
	old := cfg
	old.Plays = nil
	for _, play := range cfg.Plays {
		if newIDs[play.SessionID] {
			continue
		}
		if play.SessionID == "k3-3" {
			play.Enabled, play.UnitCents, play.UserIssueLimitCents = false, 500, 345678
		}
		old.Plays = append(old.Plays, play)
	}
	quotes := map[string][]Quote{}
	for _, id := range []string{"k3-1", "k3-3", "k3-5"} {
		book, err := s.MarketBook(id)
		must(t, err)
		book.Quotes[0].OddsMilli = 1810
		must(t, s.SaveQuotes("admin", id, book.Quotes))
		quotes[id] = book.Quotes
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for id := range newIDs {
			if err := tx.Bucket([]byte("market_quotes")).Delete([]byte(id)); err != nil {
				return err
			}
		}
		return put(tx, "settings", "global", old)
	}))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	defer s.Close()
	updated, err := s.Settings()
	must(t, err)
	if len(updated.Plays) != len(games.All()) {
		t.Fatal("new sessions were not added")
	}
	for _, before := range old.Plays {
		var after PlaySetting
		for _, play := range updated.Plays {
			if play.SessionID == before.SessionID {
				after = play
			}
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("existing setting changed", before.SessionID)
		}
	}
	for id, want := range quotes {
		book, err := s.MarketBook(id)
		must(t, err)
		if !reflect.DeepEqual(want, book.Quotes) {
			t.Fatal("existing quotes changed", id)
		}
	}
	for id := range newIDs {
		book, err := s.MarketBook(id)
		must(t, err)
		if len(book.Markets) == 0 || len(book.Quotes) != len(book.Markets) {
			t.Fatal("new catalog/quotes missing", id)
		}
	}
	assertFunds(t, s, "member", 100000, 0, 0)
}

func TestAllK3RegionsPreviewAdoptionAndSettlementRemainSeparate(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	must(t, err)
	defer db.Close()
	at := time.Now().In(model.Beijing).Add(-24 * time.Hour).Truncate(time.Hour).Add(5 * time.Second)
	now := at.Add(-30 * time.Second)
	d := previewSource(at.Add(-games.SourcePublishDelay))
	must(t, db.CacheSources([]model.Draw{d}))
	var orders []Order
	var sessions []games.Session
	for _, g := range games.All() {
		if g.GameID != "k3" {
			continue
		}
		sessions = append(sessions, g)
		in := MarketBatchInput{SessionID: g.ID, Issue: at.Format(model.IssueLayout), UnitCents: 200, AmountCents: 1000, Key: randomID(), Selections: []MarketSelection{{MarketID: "k3v2.sum.big", OddsMilli: 1000}, {MarketID: "k3v2.sum.small", OddsMilli: 1000}}}
		result, err := s.PlaceMarketBatch("member", in, now)
		must(t, err)
		orders = append(orders, result.Orders...)
	}
	if len(sessions) != 9 || len(orders) != 18 {
		t.Fatal("expected nine independent game submissions")
	}
	assertFunds(t, s, "member", 82000, 18000, 0)
	zj, _ := games.Find("k3-zj")
	fj, _ := games.Find("k3-fj")
	fjBefore, err := db.PreviewRound(fj, at)
	must(t, err)
	previews, err := db.RandomPreviews(zj, at.Format(model.IssueLayout), nil, 5, now)
	must(t, err)
	if len(previews) != 5 {
		t.Fatal("five-way random preview unavailable")
	}
	preview, err := db.ManualPreview(zj, at.Format(model.IssueLayout), []int{6, 1, 1}, previews[0].Current.Numbers, now)
	must(t, err)
	_, err = db.ApplyPreview(zj, preview.Current.Issue, preview.Candidate.ID, "admin", now)
	must(t, err)
	fjAfter, err := db.PreviewRound(fj, at)
	must(t, err)
	if !reflect.DeepEqual(fjBefore.Numbers, fjAfter.Numbers) {
		t.Fatal("adoption changed another region")
	}
	forecast, err := s.UserStatus("member", now, db)
	must(t, err)
	if forecast.Summary.Orders != 18 || forecast.Summary.UserProfit == nil {
		t.Fatal("regional forecast incomplete")
	}
	var rounds []games.Round
	for _, g := range sessions {
		r, err := games.Project(g, d, "scheduled")
		must(t, err)
		rounds = append(rounds, r)
	}
	must(t, db.CommitLive(rounds, d.Issue))
	var payout int64
	for _, o := range orders {
		r, err := db.Get(o.SessionID, o.Issue)
		must(t, err)
		must(t, s.SettleMarket(o.ID, r))
		must(t, s.SettleMarket(o.ID, r))
		settled, err := s.Order(o.ID)
		must(t, err)
		payout += settled.Payout
		if o.SessionID == zj.ID && !reflect.DeepEqual(settled.ResultNumbers, []int{6, 1, 1}) {
			t.Fatal("adopted result was not settled")
		}
	}
	if forecast.Summary.Payout != payout || *forecast.Summary.UserProfit != payout-18000 {
		t.Fatal("preview disagrees with settlement")
	}
	assertFunds(t, s, "member", 82000+payout, 0, 0)
}
