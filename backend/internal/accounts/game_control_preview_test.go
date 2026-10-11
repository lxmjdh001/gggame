package accounts

import (
	bolt "go.etcd.io/bbolt"
	"path/filepath"
	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func previewSource(at time.Time) model.Draw {
	d := model.Draw{SchemaVersion: model.SchemaVersion, Issue: at.Format(model.IssueLayout), Timezone: "Asia/Shanghai", ScheduledAt: at, GeneratedAt: at.Truncate(24 * time.Hour), Groups: map[string][]int{}}
	for _, rule := range model.Rules() {
		for i := 0; i < rule.Count; i++ {
			d.Groups[rule.ID] = append(d.Groups[rule.ID], rule.Min+i%(rule.Max-rule.Min+1))
		}
	}
	d.Groups["g1"] = []int{8, 1, 2, 3, 8, 5, 6, 7, 8, 9}
	model.Display(&d)
	return d
}

func TestGameControlPreviewMatchesSettlement(t *testing.T) {
	s, a := setup(t)
	parent := register(t, s, "forecast_parent", "")
	user := register(t, s, "forecast_child", parent.InviteCode)
	must(t, s.Adjust(a.ID, user.ID, 100000, "test", randomID()))
	cfg, e := s.Settings()
	must(t, e)
	cfg.CommissionBPS = [3]int64{333, 222, 111}
	must(t, s.SaveSettings(a.ID, cfg))
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	g, _ := games.Find("ffc-1")
	db, e := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	must(t, e)
	defer db.Close()
	must(t, db.CacheSources([]model.Draw{previewSource(g.Next(now).Add(-games.SourcePublishDelay))}))
	var orders []Order
	// A win, loss and push exercise locked odds, per-order cent rounding and commission exemption.
	for _, id := range []string{"p1.big", "p1.small", "dt.dragon"} {
		in := marketInput(now, id)
		amount := int64(201)
		in.AmountCents = &amount
		in.Multiplier = 0
		o, e := s.PlaceMarketOrder(user.ID, in, now)
		must(t, e)
		orders = append(orders, o)
	}
	// Later settings and quote changes must not affect the projection.
	cfg.CommissionBPS = [3]int64{1000, 0, 0}
	must(t, s.SaveSettings(a.ID, cfg))
	book, e := s.MarketBook(g.ID)
	must(t, e)
	for i := range book.Quotes {
		book.Quotes[i].OddsMilli = 2500
	}
	must(t, s.SaveQuotes(a.ID, g.ID, book.Quotes))
	snapshot, e := s.GameControlWithDraws(now, db)
	must(t, e)
	var row SessionExposure
	for _, v := range snapshot.Sessions {
		if v.SessionID == g.ID {
			row = v
		}
	}
	estimate := row.NextEstimate
	if row.Next == nil || row.Next.Issue != g.Next(now).Format(model.IssueLayout) || estimate.Net == nil || *estimate.Net != -3 || estimate.Stake != 603 || estimate.Payout != 594 || estimate.Commission != 12 || estimate.Orders != 3 {
		t.Fatalf("incorrect projection: %+v / %+v", row, estimate)
	}
	// Repeated preview reads cannot publish draws, settle orders, or create commissions.
	_, e = s.GameControlWithDraws(now, db)
	must(t, e)
	history, e := db.History(g.ID, 1, "")
	must(t, e)
	if history.Total != 0 {
		t.Fatal("preview published a result")
	}
	for _, o := range orders {
		got, e := s.Order(o.ID)
		must(t, e)
		if got.Status != "pending" {
			t.Fatal("preview settled an order")
		}
	}
	before, e := s.Account(parent.ID)
	must(t, e)
	if len(before["commissions"].([]Commission)) != 0 {
		t.Fatal("preview created commissions")
	}
	// Only next-issue pending orders count. Unknown/manual orders make net unavailable.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, o := range []Order{
			{ID: "older", SessionID: g.ID, Issue: g.Due(now).Format(model.IssueLayout), Status: "pending", Cost: 999},
			{ID: "refund", SessionID: g.ID, Issue: row.NextIssue, Status: "refunded", Cost: 999},
			{ID: "manual", SessionID: g.ID, Issue: row.NextIssue, Status: "pending", Cost: 999},
		} {
			if e := put(tx, "orders", o.ID, o); e != nil {
				return e
			}
		}
		return nil
	}))
	snapshot, e = s.GameControlWithDraws(now, db)
	must(t, e)
	for _, v := range snapshot.Sessions {
		if v.SessionID == g.ID && (v.NextEstimate.Net != nil || v.NextEstimate.Orders != 4 || v.NextEstimate.UnresolvedOrders != 1) {
			t.Fatalf("manual order silently estimated: %+v", v)
		}
	}
	for _, o := range orders {
		must(t, s.SettleMarket(o.ID, *row.Next))
	}
	actual, e := s.GameControl(time.Now())
	must(t, e)
	if actual.Today.Net != *estimate.Net {
		t.Fatalf("forecast %d differs from settlement %d", *estimate.Net, actual.Today.Net)
	}
}

func TestGameControlPreviewDayRollover(t *testing.T) {
	s, _ := setup(t)
	db, e := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	must(t, e)
	defer db.Close()
	now := time.Date(2026, 10, 11, 23, 59, 59, 0, model.Beijing)
	snapshot, e := s.GameControlWithDraws(now, db)
	must(t, e)
	for _, row := range snapshot.Sessions {
		if row.Next != nil || row.NextEstimate.Net != nil || row.NextDrawAt.Day() != 12 {
			t.Fatalf("missing tomorrow source: %+v", row)
		}
	}
	at := time.Date(2026, 10, 12, 0, 0, 1, 0, model.Beijing)
	must(t, db.CacheSources([]model.Draw{previewSource(at)}))
	snapshot, e = s.GameControlWithDraws(now, db)
	must(t, e)
	for _, row := range snapshot.Sessions {
		if row.SessionID == "lhc-daily" {
			continue
		}
		if row.Next == nil || row.Next.Issue != "202610120000" || row.NextEstimate.Net == nil || *row.NextEstimate.Net != 0 {
			t.Fatalf("midnight preview missing: %+v", row)
		}
	}
}

func TestRandomCandidateUsedByPayoutPreparationAndPublication(t *testing.T) {
	s, _, at, _ := batchFixture(t, 1, 1)
	defer s.Close()
	db, e := store.Open(filepath.Join(t.TempDir(), "draws.db"))
	must(t, e)
	defer db.Close()
	g, _ := games.Find("ffc-1")
	issue := at.Format(model.IssueLayout)
	source := previewSource(at.Add(-games.SourcePublishDelay))
	must(t, db.CacheSources([]model.Draw{source}))
	draft, e := db.RandomPreview(g, issue, "", true, at.Add(-6*time.Second))
	must(t, e)
	estimate, e := s.EstimateRound(draft.Candidate.Round)
	must(t, e)
	accepted, e := db.ApplyPreview(g, issue, draft.Candidate.ID, "admin", at.Add(-6*time.Second))
	must(t, e)
	must(t, s.discoverSettlementPlans(at.Add(-15*time.Second)))
	plans, e := s.settlementPlans()
	must(t, e)
	if len(plans) != 1 {
		t.Fatal("settlement plan missing")
	}
	p := plans[0]
	must(t, s.prepareUsers(p, planUsers(p), db))
	assertNoEarlyPayout(t, s)
	if _, e = db.ApplyPreview(g, issue, draft.Candidate.ID, "admin", at.Add(-5*time.Second)); e == nil || e.Error() != "已超过开奖时间不可修改" {
		t.Fatal("cutoff message mismatch", e)
	}
	original, e := games.Project(g, source, "scheduled")
	must(t, e)
	must(t, db.CommitLive([]games.Round{original}, issue))
	published, e := db.Get(g.ID, issue)
	must(t, e)
	if digestResult(published) != digestResult(*accepted) {
		t.Fatal("preview/publication mismatch")
	}
	must(t, s.commitUsers(p, planUsers(p), map[string]games.Round{g.ID: published}, at))
	actual, e := s.GameControl(at)
	must(t, e)
	if estimate.Net == nil || actual.Today.Net != *estimate.Net || actual.Today.Payout != estimate.Payout || actual.Today.Commission != estimate.Commission {
		t.Fatalf("random preview differs from payout: %+v / %+v", estimate, actual.Today)
	}
}
