package accounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
	"starlight/gameapi/pkg/model"
)

func TestMarkSixRequestedOddsProfileAllSessions(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	otherBefore, err := s.MarketBook("ssc-10")
	must(t, err)
	count := 0
	var exported MarketBook
	var exportPreset MarkSixOddsPreset
	for _, g := range games.All() {
		if g.GameID != "lhc" {
			continue
		}
		count++
		book, err := s.MarketBook(g.ID)
		must(t, err)
		before := markSixCombinedStoredState(t, s)
		preset, err := s.MarkSixOddsPreset(g.ID)
		must(t, err)
		if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
			t.Fatal("preview mutated database")
		}
		if preset.Configured+preset.Preserved != 718 {
			t.Fatal("incomplete preset", preset.Configured, preset.Preserved)
		}
		qs := map[string]Quote{}
		for _, q := range preset.Quotes {
			qs[q.MarketID] = q
		}
		check := func(id string, base, current, zero int64) {
			t.Helper()
			q := qs[id]
			if q.OddsMilli != base || q.CurrentZodiacOddsMilli != current || q.ZeroTailOddsMilli != zero {
				t.Fatalf("%s: %+v expected %d/%d/%d", id, q, base, current, zero)
			}
		}
		for p := 1; p <= 7; p++ {
			for n := 1; n <= 49; n++ {
				check(fmt.Sprintf("p%d.n%d", p, n), 42000, 0, 0)
			}
		}
		for n := 1; n <= 49; n++ {
			check(fmt.Sprintf("lhc6.regular.n%d", n), 5000, 0, 0)
		}
		for z := 0; z < 12; z++ {
			check(fmt.Sprintf("lhc6.any.zodiac.z%d", z), 1850, 1700, 0)
			check(fmt.Sprintf("lhc6.special.zodiac.z%d", z), 11600, 9500, 0)
		}
		for n := 0; n < 10; n++ {
			check(fmt.Sprintf("lhc6.any.tail.t%d", n), 1850, 0, 0)
		}
		for _, face := range []string{"big", "small", "odd", "even"} {
			check("lhc6.sum."+face, 1950, 0, 0)
		}
		for _, m := range book.Markets {
			if isProfileFace(m) && (m.UIGroup == "special" || m.UIGroup == "position_numbers" || m.UIGroup == "position_faces") {
				base := int64(1980)
				if m.UIGroup == "position_faces" {
					base = 1960
				}
				if m.Kind == "lhc_color" {
					base = 2900
					if m.Pick == "red" {
						base = 2700
					}
					if m.UIGroup == "position_faces" {
						base = 2750
						if m.Pick == "red" {
							base = 2650
						}
					}
				}
				check(m.ID, base, 0, 0)
			}
			if m.Kind == "lhc_half_wave" {
				check(m.ID, 5580, 0, 0)
			}
			if m.Kind == "lhc_half_digit" {
				check(m.ID, 5300, 0, 0)
			}
		}
		for n, rate := range map[int]int64{2: 5300, 3: 3500, 4: 2350, 5: 2000, 6: 1900, 7: 1600, 8: 1400, 9: 1230, 10: 1130, 11: 1040} {
			special := int64(0)
			if n == 2 {
				special = 4800
			}
			check(fmt.Sprintf("lhc6.combined_zodiac.in.%d", n), rate, special, 0)
		}
		for n, rate := range map[int]int64{5: 2100, 6: 2400, 7: 2900, 8: 3800, 9: 4100, 10: 4900, 11: 6300, 12: 1900} {
			check(fmt.Sprintf("lhc6.miss.%d", n), rate, 0, 0)
		}
		for n, rate := range map[int]int64{2: 3100, 3: 7000, 4: 16000, 5: 42000} {
			zero := int64(0)
			if n == 2 {
				zero = 4000
			}
			check(fmt.Sprintf("lhc6.link_tail.%d", n), rate, 0, zero)
		}
		for n, rate := range map[int][2]int64{2: {3500, 3000}, 3: {10000, 9000}, 4: {30000, 28000}, 5: {50000, 25000}} {
			check(fmt.Sprintf("lhc6.link_zodiac.%d", n), rate[0], rate[1], 0)
		}
		for n, rate := range map[int][2]int64{2: {3000, 2500}, 3: {5500, 5000}, 4: {20000, 15000}} {
			check(fmt.Sprintf("lhc6.miss_zodiac.%d", n), rate[0], rate[1], 0)
		}
		if !reflect.DeepEqual(qs["lhc6.link.two_special"].TierOddsMilli, map[string]int64{"special": 33000, "hit2": 52000}) {
			t.Fatal("wrong two-special tiers")
		}
		must(t, s.SaveQuotes("admin", g.ID, preset.Quotes))
		actual, err := s.MarketBook(g.ID)
		must(t, err)
		if !reflect.DeepEqual(actual.Quotes, preset.Quotes) {
			t.Fatal("save did not persist", g.ID)
		}
		if g.ID == "lhc-tw" {
			exported = actual
			exportPreset = preset
		}
	}
	if count != 8 {
		t.Fatal("expected all eight sessions", count)
	}
	otherAfter, err := s.MarketBook("ssc-10")
	must(t, err)
	if !reflect.DeepEqual(otherBefore, otherAfter) {
		t.Fatal("other game changed")
	}
	before := markSixCombinedStoredState(t, s)
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("reopen changed configured prices")
	}
	// Optional generated local UI fixture; never points at a production store.
	if dir := os.Getenv("MARKSIX_ODDS_QA_DIR"); dir != "" {
		must(t, os.MkdirAll(dir, 0700))
		for name, value := range map[string]any{"book.json": exported, "preset.json": exportPreset, "other-book.json": otherAfter} {
			data, e := json.MarshalIndent(value, "", "  ")
			must(t, e)
			must(t, os.WriteFile(filepath.Join(dir, name), data, 0600))
		}
	}
}

func TestMarkSixConditionalOddsLockAndRejectStalePrice(t *testing.T) {
	s, path, _ := fundsFixture(t)
	defer func() { s.Close() }()
	preset, err := s.MarkSixOddsPreset("lhc-tw")
	must(t, err)
	must(t, s.SaveQuotes("admin", "lhc-tw", preset.Quotes))
	cases := []struct {
		id     string
		values []int
		odds   int64
		draw   []int
	}{
		{"lhc6.any.zodiac.z6", nil, 1700, []int{1, 13, 25, 37, 49, 2, 14}},
		{"lhc6.any.zodiac.z0", nil, 1850, []int{7, 13, 25, 37, 49, 2, 14}},
		{"lhc6.special.zodiac.z6", nil, 9500, []int{2, 3, 4, 5, 6, 7, 1}},
		{"lhc6.combined_zodiac.in.2", []int{0, 6}, 4800, []int{2, 3, 4, 5, 6, 7, 1}},
		{"lhc6.link_zodiac.2", []int{5, 6}, 3000, []int{1, 13, 25, 37, 49, 2, 14}},
		{"lhc6.link_tail.2", []int{0, 1}, 4000, []int{1, 10, 25, 37, 49, 2, 14}},
		{"lhc6.link_tail.2", []int{1, 2}, 3100, []int{1, 13, 25, 37, 49, 2, 14}},
		{"lhc6.miss_zodiac.2", []int{0, 6}, 2500, []int{2, 14, 26, 38, 3, 15, 27}},
		{"lhc6.position_special.p1.big", nil, 1980, []int{25, 1, 2, 3, 4, 5, 6}},
		{"lhc6.p1.big", nil, 1960, []int{25, 1, 2, 3, 4, 5, 6}},
		{"lhc6.special.halfdigit.red_odd", nil, 5300, []int{3, 4, 5, 6, 7, 8, 12}},
	}
	var accepted []Order
	var inputs []MarketOrderInput
	for _, tc := range cases {
		in := markSixV6Input(t, s, tc.id, tc.values)
		in.OddsMilli = tc.odds + 1
		before := markSixCombinedStoredState(t, s)
		if _, err := s.PlaceMarketOrder("member", in, markSixV6Time()); err == nil {
			t.Fatal("stale conditional price accepted", tc.id)
		}
		if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
			t.Fatal("rejection mutated funds")
		}
		in.OddsMilli = tc.odds
		o, e := s.PlaceMarketOrder("member", in, markSixV6Time())
		must(t, e)
		if o.OddsMilli != tc.odds {
			t.Fatal("did not lock actual price", o)
		}
		accepted = append(accepted, o)
		inputs = append(inputs, in)
	}
	book, err := s.MarketBook("lhc-tw")
	must(t, err)
	for i := range book.Quotes {
		q := &book.Quotes[i]
		q.OddsMilli = 1000
		q.CurrentZodiacOddsMilli = 0
		q.ZeroTailOddsMilli = 0
	}
	must(t, s.SaveQuotes("admin", "lhc-tw", book.Quotes))
	must(t, s.Close())
	s, err = Open(path)
	must(t, err)
	for i, o := range accepted {
		retry, e := s.PlaceMarketOrder("member", inputs[i], markSixV6Time().Add(time.Hour))
		must(t, e)
		if retry.ID != o.ID {
			t.Fatal("retry duplicated order")
		}
		must(t, s.SettleMarket(o.ID, marketRound(inputs[i], cases[i].draw)))
		settled, e := s.Order(o.ID)
		must(t, e)
		if settled.Outcome != "win" || settled.Payout != 201*cases[i].odds/1000 {
			t.Fatal("wrong locked payout", cases[i], settled)
		}
	}
}

func TestMarkSixConditionalValidationAndLunarYear(t *testing.T) {
	for _, year := range []int{2025, 2026, 2027} {
		due := time.Date(year, 6, 1, 12, 20, 5, 0, model.Beijing)
		current := markets.MarkSixZodiac(1, markets.MarkSixLunarYear(due))
		for z := 0; z < 12; z++ {
			m, _ := markets.Find("lhc", fmt.Sprintf("lhc6.any.zodiac.z%d", z))
			m, err := markets.BindSelection(m, nil, due)
			must(t, err)
			q := Quote{MarketID: m.ID, OddsMilli: 1850, CurrentZodiacOddsMilli: 1700}
			want := int64(1850)
			if z == current {
				want = 1700
			}
			if selectionQuoteOdds(m, q) != want {
				t.Fatal("year mapping wrong", year, z)
			}
		}
	}
	// The lunar year flips at the draw, not January 1 or the browser's clock.
	for _, tc := range []struct {
		due    time.Time
		animal int
	}{
		{time.Date(2026, 2, 16, 23, 40, 5, 0, model.Beijing), 5},
		{time.Date(2026, 2, 17, 0, 0, 5, 0, model.Beijing), 6},
	} {
		for _, animal := range []int{5, 6} {
			m, _ := markets.Find("lhc", fmt.Sprintf("lhc6.any.zodiac.z%d", animal))
			m, e := markets.BindSelection(m, nil, tc.due)
			must(t, e)
			want := int64(1850)
			if animal == tc.animal {
				want = 1700
			}
			if selectionQuoteOdds(m, Quote{OddsMilli: 1850, CurrentZodiacOddsMilli: 1700}) != want {
				t.Fatal("lunar boundary price incorrect", tc, animal)
			}
		}
	}
	s, _, _ := fundsFixture(t)
	defer s.Close()
	for _, tc := range []struct {
		id            string
		current, zero int64
	}{{"lhc6.p1.big", 1700, 0}, {"lhc6.link_tail.2", 0, 999}, {"lhc6.any.zodiac.z0", 1000001, 0}, {"lhc6.any.zodiac.z0", 0, 4000}, {"lhc6.link_tail.2", 0, -1}} {
		book, err := s.MarketBook("lhc-tw")
		must(t, err)
		for i := range book.Quotes {
			if book.Quotes[i].MarketID == tc.id {
				book.Quotes[i].CurrentZodiacOddsMilli = tc.current
				book.Quotes[i].ZeroTailOddsMilli = tc.zero
			}
		}
		before := markSixCombinedStoredState(t, s)
		if s.SaveQuotes("admin", "lhc-tw", book.Quotes) == nil {
			t.Fatal("bad conditional quote accepted", tc)
		}
		if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
			t.Fatal("invalid edit wrote data")
		}
	}
}

func TestMarkSixPresetPreservesUnspecifiedPricesAndSwitches(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	b, e := s.MarketBook("lhc-tw")
	must(t, e)
	for i := range b.Quotes {
		q := &b.Quotes[i]
		if q.MarketID == "lhc6.regular.zodiac.z0" {
			q.OddsMilli = 7890
			q.Enabled = false
		}
		if q.MarketID == "lhc6.p1.big" {
			q.Enabled = false
		}
	}
	must(t, s.SaveQuotes("admin", "lhc-tw", b.Quotes))
	p, e := s.MarkSixOddsPreset("lhc-tw")
	must(t, e)
	for _, q := range p.Quotes {
		if q.MarketID == "lhc6.regular.zodiac.z0" && (q.OddsMilli != 7890 || q.Enabled) {
			t.Fatal("unspecified price/switch reset")
		}
		if q.MarketID == "lhc6.p1.big" && (q.OddsMilli != 1960 || q.Enabled) {
			t.Fatal("paused switch reset")
		}
	}
	if _, e := s.MarkSixOddsPreset("ssc-10"); e == nil {
		t.Fatal("other game preset accepted")
	}
}

func TestMarkSixPresetHTTPAdminOnlyAndReadOnly(t *testing.T) {
	s, _, _ := fundsFixture(t)
	defer s.Close()
	tokens := map[string]string{}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, id := range []string{"admin", "member"} {
			token, e := newLogin(tx, id)
			if e != nil {
				return e
			}
			tokens[id] = token
		}
		return nil
	}))
	h := &HTTP{Store: s}
	mux := http.NewServeMux()
	h.Bind(mux)
	handler := h.Protect(mux)
	for _, actor := range []string{"", "member", "admin"} {
		before := markSixCombinedStoredState(t, s)
		r := httptest.NewRequest("GET", "http://localhost:5173/api/v1/admin/marksix-odds-profile/lhc-tw", nil)
		if actor != "" {
			r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tokens[actor]})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if (w.Code == 200) != (actor == "admin") {
			t.Fatal("wrong access", actor, w.Code)
		}
		if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
			t.Fatal("HTTP preview changed store")
		}
	}
}
