package accounts

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
)

func TestAllMarkSixAndK3RandomAdoptionFlowsThroughPublishedSettlement(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	must(t, err)
	defer db.Close()
	// A shared boundary after the regional schedule migration. Every money path
	// receives an explicit clock; this test never waits for wall-clock publication.
	at := time.Date(2026, 10, 16, 13, 0, 5, 0, model.Beijing)
	now := at.Add(-20 * time.Second)
	issue := at.Format(model.IssueLayout)
	source := previewSource(at.Add(-games.SourcePublishDelay))
	must(t, db.CacheSources([]model.Draw{source}))
	sessions := []games.Session{}
	orders := []Order{}
	for _, g := range games.All() {
		if g.GameID != "lhc" && g.GameID != "k3" {
			continue
		}
		sessions = append(sessions, g)
		prefix := "p1."
		if g.GameID == "k3" {
			prefix = "k3v2.sum."
		}
		if g.GameID == "lhc" {
			prefix = "lhc6.special."
		}
		if g.ID == "lhc-au" {
			prefix = "au45.p8."
		}
		book, err := s.marketBookAt(g, at)
		must(t, err)
		selections := []MarketSelection{}
		for _, q := range book.Quotes {
			if q.MarketID == prefix+"big" || q.MarketID == prefix+"small" {
				selections = append(selections, MarketSelection{MarketID: q.MarketID, OddsMilli: q.OddsMilli})
			}
		}
		if len(selections) != 2 {
			t.Fatalf("%s missing complementary markets", g.ID)
		}
		batch, err := s.PlaceMarketBatch("member", MarketBatchInput{SessionID: g.ID, Issue: issue, UnitCents: 200, AmountCents: 1000, Key: randomID(), Selections: selections}, g.CloseAt(at).Add(-time.Second))
		must(t, err)
		orders = append(orders, batch.Orders...)
	}
	if len(sessions) != 17 || len(orders) != 34 {
		t.Fatal("expected eight Mark Six and nine K3 sessions")
	}
	assertFunds(t, s, "member", 66000, 34000, 0)
	adopted := map[string]games.Round{}
	var expectedPayout int64
	for i, g := range sessions {
		single, err := db.RandomPreviews(g, issue, nil, 1, now)
		must(t, err)
		if len(single) != 1 || single[0].Candidate == nil {
			t.Fatal(g.ID, "single random failed")
		}
		previews, err := db.RandomPreviews(g, issue, nil, 5, now)
		must(t, err)
		if len(previews) != 5 {
			t.Fatal(g.ID, "missing comparison candidates")
		}
		ids := []string{}
		for _, p := range previews {
			ids = append(ids, p.Candidate.ID)
		}
		refresh, err := db.RandomPreviews(g, issue, ids, 0, now.Add(time.Second))
		must(t, err)
		for j := range previews {
			if previews[j].Candidate.ID != refresh[j].Candidate.ID || previews[j].Candidate.BaseVersion != refresh[j].Candidate.BaseVersion || !reflect.DeepEqual(previews[j].Candidate.Round.Numbers, refresh[j].Candidate.Round.Numbers) {
				t.Fatal(g.ID, "refresh rerolled draft")
			}
		}
		before, err := db.PreviewRound(g, at)
		must(t, err)
		if !reflect.DeepEqual(before.Numbers, previews[0].Current.Numbers) {
			t.Fatal(g.ID, "random preview changed effective numbers")
		}
		chosen := previews[i%5].Candidate
		other := sessions[(i+1)%len(sessions)]
		otherBefore, err := db.PreviewRound(other, at)
		must(t, err)
		if _, err = db.ApplyPreview(other, issue, chosen.ID, "admin", now); err == nil {
			t.Fatal(g.ID, "candidate accepted for another session")
		}
		estimates, err := s.EstimateRounds([]games.Round{chosen.Round})
		must(t, err)
		expectedPayout += estimates[0].Payout
		result, err := db.ApplyPreview(g, issue, chosen.ID, "admin", now)
		must(t, err)
		adopted[g.ID] = *result
		if _, err = db.ApplyPreview(g, issue, single[0].Candidate.ID, "admin", now); err == nil {
			t.Fatal(g.ID, "stale candidate adopted")
		}
		otherAfter, err := db.PreviewRound(other, at)
		must(t, err)
		if otherBefore.Issue != otherAfter.Issue || otherBefore.SourceIssue != otherAfter.SourceIssue || !reflect.DeepEqual(otherBefore.Numbers, otherAfter.Numbers) {
			t.Fatal(g.ID, "adoption changed another session")
		}
		if _, err = db.Get(g.ID, issue); err != store.ErrNotFound {
			t.Fatal(g.ID, "adoption published before draw")
		}
	}
	forecast, err := s.UserStatus("member", now, db)
	must(t, err)
	if forecast.Summary.Orders != 34 || forecast.Summary.Payout != expectedPayout {
		t.Fatal("user estimate does not use adopted results")
	}
	control, err := s.GameControlWithDraws(now, db)
	must(t, err)
	for _, row := range control.Sessions {
		if want, ok := adopted[row.SessionID]; ok && (row.Next == nil || !reflect.DeepEqual(row.Next.Numbers, want.Numbers) || row.NextEstimate.Orders != 2) {
			t.Fatal(row.SessionID, "game control did not refresh adopted draw")
		}
	}
	assertFunds(t, s, "member", 66000, 34000, 0)
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	plans, err := s.settlementPlans()
	must(t, err)
	if len(plans) != 1 {
		t.Fatal("shared issue settlement missing")
	}
	plan := plans[0]
	must(t, s.prepareUsers(plan, planUsers(plan), db))
	assertFunds(t, s, "member", 66000, 34000, 0)
	rounds := []games.Round{}
	for _, g := range sessions {
		r, err := games.Project(g, source, "scheduled")
		must(t, err)
		rounds = append(rounds, r)
	}
	must(t, db.CommitLive(rounds, issue))
	published := map[string]games.Round{}
	for _, g := range sessions {
		r, err := db.Get(g.ID, issue)
		must(t, err)
		if !reflect.DeepEqual(r.Numbers, adopted[g.ID].Numbers) {
			t.Fatal(g.ID, "publication overwrote adopted random numbers")
		}
		published[g.ID] = r
	}
	must(t, s.commitUsers(plan, planUsers(plan), published, at))
	must(t, s.commitUsers(plan, planUsers(plan), published, at.Add(time.Second)))
	must(t, s.cleanupPlan(plan))
	var actualPayout int64
	for _, original := range orders {
		o, err := s.Order(original.ID)
		must(t, err)
		if o.Status != "settled" || !reflect.DeepEqual(o.ResultNumbers, adopted[o.SessionID].Numbers) {
			t.Fatal(o.ID, "settlement ignored chosen candidate")
		}
		actualPayout += o.Payout
	}
	if actualPayout != expectedPayout {
		t.Fatal("actual payout differs from candidate estimate")
	}
	assertFunds(t, s, "member", 66000+expectedPayout, 0, 0)
}
