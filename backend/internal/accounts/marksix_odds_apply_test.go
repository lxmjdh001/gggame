package accounts

import (
	"reflect"
	"testing"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
)

func TestMarkSixApplyPresetAtomicAndIdempotent(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	before := markSixCombinedStoredState(t, s)
	if _, err := s.ApplyMarkSixOddsPreset("member"); err == nil {
		t.Fatal("member applied administrative profile")
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("rejected actor mutated store")
	}
	result, err := s.ApplyMarkSixOddsPreset("admin")
	must(t, err)
	if len(result) != 8 {
		t.Fatal("incomplete application", result)
	}
	for _, r := range result {
		if !r.Changed || r.Configured != 647 || r.Preserved != 71 {
			t.Fatal("wrong coverage", r)
		}
		b, e := s.MarketBook(r.SessionID)
		must(t, e)
		p, e := MarkSixPresetForBook(b)
		must(t, e)
		if !reflect.DeepEqual(b.Quotes, p.Quotes) {
			t.Fatal("profile readback mismatch", r.SessionID)
		}
	}
	after := markSixCombinedStoredState(t, s)
	for name, records := range before {
		if name != "market_quotes" && name != "audit" && !reflect.DeepEqual(records, after[name]) {
			t.Fatal("unrelated accounting changed", name)
		}
	}
	result, err = s.ApplyMarkSixOddsPreset("admin")
	must(t, err)
	for _, r := range result {
		if r.Changed {
			t.Fatal("repeat is not no-op", r)
		}
	}
	if !reflect.DeepEqual(after, markSixCombinedStoredState(t, s)) {
		t.Fatal("repeat wrote configuration or audits")
	}
}

func TestMarkSixApplyPresetRollsBackAllEightOnLastFailure(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	last := ""
	for _, g := range games.All() {
		if g.GameID == "lhc" {
			last = g.ID
		}
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		qs, e := get[[]Quote](tx, "market_quotes", last)
		if e != nil {
			return e
		}
		// An invalid unspecified quote must abort after earlier sessions have
		// tentatively changed inside the same transaction.
		for i := range qs {
			if qs[i].MarketID == "lhc6.regular.zodiac.z0" {
				qs[i].OddsMilli = -1
			}
		}
		return put(tx, "market_quotes", last, qs)
	}))
	before := markSixCombinedStoredState(t, s)
	if _, err := s.ApplyMarkSixOddsPreset("admin"); err == nil {
		t.Fatal("invalid last session accepted")
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("partial eight-session update committed")
	}
}
