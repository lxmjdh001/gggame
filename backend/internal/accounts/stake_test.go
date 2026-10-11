package accounts

import (
	"sync"
	"testing"
	"time"
)

func TestDirectStakeValidation(t *testing.T) {
	p := defaults().Plays[0]
	for _, c := range []struct {
		amount int64
		mult   int
		valid  bool
	}{
		{1000, 0, true}, {2000, 0, true}, {5000, 0, true}, {10000, 0, true}, {20000, 0, true}, {50000, 0, true},
		{2550, 0, true}, {0, 0, false}, {-100, 0, false}, {199, 0, false}, {100001, 0, false}, {50000, 1, false},
	} {
		cost, err := orderStake(p, &c.amount, c.mult)
		if (err == nil) != c.valid || (err == nil && cost != c.amount) {
			t.Fatalf("%+v: cost=%d error=%v", c, cost, err)
		}
	}
	if cost, e := orderStake(p, nil, 3); e != nil || cost != 600 {
		t.Fatal("legacy multiplier broken", cost, e)
	}
	if _, e := orderStake(p, nil, 250); e == nil {
		t.Fatal("legacy max multiplier bypassed")
	}
}

func TestDirectAmountOrdersAndSettlement(t *testing.T) {
	s, a := setup(t)
	must(t, s.Adjust(a.ID, a.ID, 1000000, "金额投注测试", randomID()))
	now := time.Now().Add(-time.Hour).Truncate(time.Minute).Add(10 * time.Second)
	in := marketInput(now, "p1.big")
	amount := int64(50000)
	in.AmountCents = &amount
	in.Multiplier = 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.PlaceMarketOrder(a.ID, in, now); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	o, e := s.PlaceMarketOrder(a.ID, in, now.Add(time.Minute))
	must(t, e)
	if o.Cost != 50000 || o.Multiplier != 0 {
		t.Fatal("quick 500 yuan amount wrong", o)
	}
	must(t, s.SettleMarket(o.ID, marketRound(in, []int{9, 1, 2, 3, 4})))
	o, e = s.Order(o.ID)
	must(t, e)
	if o.Payout != 98000 {
		t.Fatal("wrong direct-amount payout", o.Payout)
	}
	profile, e := s.Account(a.ID)
	must(t, e)
	if profile["user"].(User).Balance != 1048000 {
		t.Fatal("duplicate deduction or wrong payout")
	}
	changed := in
	other := int64(20000)
	changed.AmountCents = &other
	if _, e = s.PlaceMarketOrder(a.ID, changed, now); e == nil {
		t.Fatal("idempotency key accepted different amount")
	}
	// Non-multiples of the old two-yuan unit keep their precise cent amount.
	decimal := int64(2550)
	legacy := input(now)
	legacy.AmountCents = &decimal
	legacy.Multiplier = 0
	legacy.Key = randomID()
	old, e := s.PlaceOrder(a.ID, legacy, now)
	must(t, e)
	if old.Cost != 2550 {
		t.Fatal("custom decimal amount rounded")
	}
	must(t, s.SettleOrder(a.ID, old.ID, "refunded", "测试退回", 0, false))
	cfg, e := s.Settings()
	must(t, e)
	cfg.Plays[1].MaxStakeCents = 20000
	must(t, s.SaveSettings(a.ID, cfg))
	in.Key = randomID()
	if _, e = s.PlaceMarketOrder(a.ID, in, now); e == nil {
		t.Fatal("updated stake limit ignored")
	}
}
