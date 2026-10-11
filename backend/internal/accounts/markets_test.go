package accounts

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
	"sync"
	"testing"
	"time"
)

func marketInput(now time.Time, id string) MarketOrderInput {
	g, _ := games.Find("ffc-1")
	return MarketOrderInput{SessionID: g.ID, Issue: g.Next(now).Format(model.IssueLayout), MarketID: id, OddsMilli: 1960, UnitCents: 200, Multiplier: 50, Key: randomID()}
}
func marketRound(in MarketOrderInput, n []int) games.Round {
	g, _ := games.Find(in.SessionID)
	at, _ := model.ParseIssue(in.Issue)
	return games.Round{SessionID: in.SessionID, Issue: in.Issue, SourceIssue: in.Issue, SourceGroup: g.Group, Origin: "scheduled", DrawAt: at.Add(games.SourcePublishDelay), Numbers: n}
}
func TestMarketOddsSnapshotAutomaticSettlement(t *testing.T) {
	s, a := setup(t)
	p := register(t, s, "market_parent", "")
	u := register(t, s, "market_child", p.InviteCode)
	must(t, s.Adjust(a.ID, u.ID, 50000, "测试资金", randomID()))
	cfg, e := s.Settings()
	must(t, e)
	cfg.CommissionBPS = [3]int64{500, 0, 0}
	must(t, s.SaveSettings(a.ID, cfg))
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	in := marketInput(now, "p1.big")
	o, e := s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	same, e := s.PlaceMarketOrder(u.ID, in, now.Add(time.Minute))
	must(t, e)
	if same.ID != o.ID {
		t.Fatal("retry created second order")
	}
	book, e := s.MarketBook("ffc-1")
	must(t, e)
	for i, q := range book.Quotes {
		if q.MarketID == "p1.big" {
			book.Quotes[i].OddsMilli = 1800
		}
	}
	must(t, s.SaveQuotes(a.ID, "ffc-1", book.Quotes))
	if e = s.SettleOrder(a.ID, o.ID, "settled", "不能覆盖锁定赔率", 1, true); e == nil {
		t.Fatal("manual payout override allowed")
	}
	r := marketRound(in, []int{8, 1, 2, 3, 4})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.SettleMarket(o.ID, r); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	settled, e := s.Order(o.ID)
	must(t, e)
	if settled.OddsMilli != 1960 || settled.Payout != 19600 || settled.Outcome != "win" {
		t.Fatal("snapshot or payout wrong", settled)
	}
	account, e := s.Account(u.ID)
	must(t, e)
	if account["user"].(User).Balance != 59600 {
		t.Fatal("duplicate or incorrect payout")
	}
	parent, e := s.Account(p.ID)
	must(t, e)
	if parent["commission_available_cents"].(int64) != 500 {
		t.Fatal("commission not exactly once")
	}
	pending, e := s.PendingMarkets()
	must(t, e)
	if len(pending) != 0 {
		t.Fatal("pending index not cleared")
	}
	stale := marketInput(now, "p1.big")
	if _, e = s.PlaceMarketOrder(u.ID, stale, now); e == nil {
		t.Fatal("stale odds accepted")
	}
	book.Quotes[0].Enabled = false
	must(t, s.SaveQuotes(a.ID, "ffc-1", book.Quotes))
	stale.OddsMilli = 1800
	if _, e = s.PlaceMarketOrder(u.ID, stale, now); e == nil {
		t.Fatal("disabled selection accepted")
	}
}
func TestPushLossRefundAndSourceValidation(t *testing.T) {
	s, a := setup(t)
	parent := register(t, s, "push_parent", "")
	u := register(t, s, "push_child", parent.InviteCode)
	must(t, s.Adjust(a.ID, u.ID, 50000, "测试资金", randomID()))
	cfg, e := s.Settings()
	must(t, e)
	cfg.CommissionBPS = [3]int64{1000, 0, 0}
	must(t, s.SaveSettings(a.ID, cfg))
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	in := marketInput(now, "dt.dragon")
	o, e := s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	r := marketRound(in, []int{1, 2, 3, 4, 1})
	r.Origin = "simulated_history"
	if e = s.SettleMarket(o.ID, r); e == nil {
		t.Fatal("simulation settled")
	}
	r.Origin = "scheduled"
	r.Issue = "200001010000"
	if e = s.SettleMarket(o.ID, r); e == nil {
		t.Fatal("wrong issue settled")
	}
	r = marketRound(in, []int{1, 2, 3, 4, 1})
	must(t, s.SettleMarket(o.ID, r))
	after, e := s.Account(u.ID)
	must(t, e)
	if after["user"].(User).Balance != 50000 {
		t.Fatal("push did not return stake")
	}
	p, e := s.Account(parent.ID)
	must(t, e)
	if len(p["commissions"].([]Commission)) != 0 {
		t.Fatal("push generated commission")
	}
	in = marketInput(now, "p1.small")
	o, e = s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	must(t, s.SettleMarket(o.ID, marketRound(in, []int{9, 0, 0, 0, 0})))
	settled, e := s.Order(o.ID)
	must(t, e)
	if settled.Payout != 0 || settled.Outcome != "lose" {
		t.Fatal("losing order paid")
	}
	in = marketInput(now, "p1.even")
	o, e = s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	must(t, s.SettleOrder(a.ID, o.ID, "refunded", "管理员撤销", 0, false))
	must(t, s.SettleMarket(o.ID, marketRound(in, []int{2, 0, 0, 0, 0})))
	c, e := s.Order(o.ID)
	must(t, e)
	if c.Status != "refunded" {
		t.Fatal("cancelled order auto settled")
	}
	if e = s.SaveQuotes(u.ID, "ffc-1", nil); e == nil {
		t.Fatal("unprivileged odds update")
	}
	if _, e = s.MarketBook("nonexistent"); e == nil {
		t.Fatal("unknown session allowed")
	}
}
func TestMarketPersistenceAndPayoutRounding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.db")
	s, e := Open(path)
	must(t, e)
	_, e = s.Bootstrap("admin", testPassword)
	must(t, e)
	a, _, e := s.Login("admin", testPassword)
	must(t, e)
	u := register(t, s, "rounding_user", "")
	must(t, s.Adjust(a.ID, u.ID, 100, "测试资金", randomID()))
	cfg, e := s.Settings()
	must(t, e)
	for i := range cfg.Plays {
		cfg.Plays[i].UnitCents = 1
	}
	must(t, s.SaveSettings(a.ID, cfg))
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	in := marketInput(now, "p1.big")
	in.UnitCents = 1
	in.Multiplier = 1
	o, e := s.PlaceMarketOrder(u.ID, in, now)
	must(t, e)
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	defer s.Close()
	pending, e := s.PendingMarkets()
	must(t, e)
	if len(pending) != 1 || pending[0].RuleSnapshot.RuleVersion != markets.Version {
		t.Fatal("pending order snapshot lost")
	}
	// Restart recovery uses the real settlement loop and the persisted pending index.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.RunSettlement(ctx, fixedRoundReader{marketRound(in, []int{5, 0, 0, 0, 0})}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		pending, e = s.PendingMarkets()
		must(t, e)
		if len(pending) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restart settlement did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	o, e = s.Order(o.ID)
	must(t, e)
	if o.Payout != 1 {
		t.Fatal("fractional cents not floored")
	}
}

type fixedRoundReader struct{ round games.Round }

func (r fixedRoundReader) Get(session, issue string) (games.Round, error) {
	if session != r.round.SessionID || issue != r.round.Issue {
		return games.Round{}, ErrNotFound
	}
	return r.round, nil
}
