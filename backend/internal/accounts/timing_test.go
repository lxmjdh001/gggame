package accounts

import (
	bolt "go.etcd.io/bbolt"
	"path/filepath"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestNewAndLegacyOrdersRespectFullClosedWindow(t *testing.T) {
	s, a := setup(t)
	must(t, s.Adjust(a.ID, a.ID, 100000, "封盘测试", randomID()))
	anchor := time.Date(2026, 10, 15, 21, 30, 5, 0, model.Beijing)
	for _, g := range games.All() {
		due := g.Due(anchor)
		values := []int{1, 2, 3, 4, 5}
		switch g.GameID {
		case "k3":
			values = []int{1, 2, 3}
		case "lhc":
			values = []int{1, 2, 3, 4, 5, 6}
		case "pk10":
			values = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		case "pc28":
			values = []int{3}
		}
		window := time.Duration(g.ClosedWindowSeconds()) * time.Second
		for _, offset := range []time.Duration{-window - time.Nanosecond, -window, -15 * time.Second, -5 * time.Second, -4 * time.Second, -time.Nanosecond, 0} {
			now := due.Add(offset)
			expectedOpen := offset < -window || offset == 0
			issue := due.Format(model.IssueLayout)
			if offset == 0 {
				issue = g.Next(now).Format(model.IssueLayout)
			}
			in := MarketOrderInput{SessionID: g.ID, Issue: issue, MarketID: "p1.big", OddsMilli: 1960, UnitCents: 200, Multiplier: 1, Key: randomID()}
			if g.GameID == "k3" {
				in.MarketID, in.OddsMilli = "k3v2.sum.big", 1000
			}
			if g.GameID == "lhc" {
				in.MarketID, in.OddsMilli = "lhc6.p1.big", 1000
			}
			if g.ID == "lhc-au" {
				in.MarketID, in.OddsMilli = "au45.p1.big", 1960
			}
			_, err := s.PlaceMarketOrder(a.ID, in, now)
			if (err == nil) != expectedOpen {
				t.Fatalf("market %s offset %s: %v", g.ID, offset, err)
			}
			old := OrderInput{SessionID: g.ID, Issue: issue, Numbers: values, UnitCents: 200, Multiplier: 1, Key: randomID()}
			_, err = s.PlaceOrder(a.ID, old, now)
			if (err == nil) != (expectedOpen && g.GameID != "lhc") {
				t.Fatalf("legacy %s offset %s: %v", g.ID, offset, err)
			}
		}
	}
}

func TestTimingSettingsMigrationPreservesOtherSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, e := Open(path)
	must(t, e)
	cfg, e := s.Settings()
	must(t, e)
	cfg.Announcement = "preserve me"
	cfg.CommissionBPS = [3]int64{100, 50, 20}
	cfg.Plays[0].Enabled = false
	cfg.Plays[0].UnitCents = 1234
	for i := range cfg.Plays {
		cfg.Plays[i].CloseSeconds = 3
		cfg.Plays[i].MaxStakeCents = 0
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error { return put(tx, "settings", "global", cfg) }))
	must(t, s.Close())
	s, e = Open(path)
	must(t, e)
	defer s.Close()
	got, e := s.Settings()
	must(t, e)
	if got.Announcement != cfg.Announcement || got.CommissionBPS != cfg.CommissionBPS || got.Plays[0].Enabled || got.Plays[0].UnitCents != 1234 {
		t.Fatal("migration changed unrelated settings")
	}
	for _, p := range got.Plays {
		if p.MaxStakeCents != 100000 {
			t.Fatal("missing direct stake limit")
		}
		g, _ := games.Find(p.SessionID)
		if p.CloseSeconds != g.ClosedWindowSeconds() {
			t.Fatal("old timing survived migration")
		}
	}
}
