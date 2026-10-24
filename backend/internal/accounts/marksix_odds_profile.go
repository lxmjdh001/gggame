package accounts

import (
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/backend/internal/games"
	"starlight/backend/internal/markets"
)

const MarkSixOddsProfileID = "marksix-2026-10-23"

type MarkSixProfileResult struct {
	SessionID  string `json:"session_id"`
	Configured int    `json:"configured"`
	Preserved  int    `json:"preserved"`
	Changed    bool   `json:"changed"`
}

// ApplyMarkSixOddsPreset is an offline deployment operation, not an HTTP route.
// All eight sessions commit together; switches, retired quotes and accounting
// buckets are untouched. Reapplying an already installed profile is a no-op.
func (s *Store) ApplyMarkSixOddsPreset(actor string) ([]MarkSixProfileResult, error) {
	var result []MarkSixProfileResult
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		now := time.Now()
		for _, g := range games.All() {
			if g.GameID != "lhc" {
				continue
			}
			due := g.Next(now)
			if g.MarketCatalogIDAt(due) != "lhc" {
				return errors.New("六合彩目录尚未切换，不能应用倍率方案")
			}
			old, err := get[[]Quote](tx, "market_quotes", g.ID)
			if err != nil {
				return err
			}
			book := MarketBook{SessionID: g.ID, Markets: markets.Catalog("lhc"), MarkSixContext: markets.MarkSixContextAt(due)}
			active := map[string]bool{}
			for _, m := range book.Markets {
				active[m.ID] = true
			}
			for _, q := range old {
				if active[q.MarketID] {
					book.Quotes = append(book.Quotes, q)
				}
			}
			preset, err := MarkSixPresetForBook(book)
			if err != nil {
				return err
			}
			byID := map[string]Quote{}
			for _, q := range preset.Quotes {
				byID[q.MarketID] = q
			}
			stored := append([]Quote(nil), old...)
			for i, q := range stored {
				if updated, ok := byID[q.MarketID]; ok {
					stored[i] = updated
				}
			}
			changed := !reflect.DeepEqual(old, stored)
			if changed {
				if err := put(tx, "market_quotes", g.ID, stored); err != nil {
					return err
				}
				detail, err := json.Marshal(map[string]any{"profile_id": MarkSixOddsProfileID, "before": old, "after": stored})
				if err != nil {
					return err
				}
				if err := audit(tx, actor, "odds.profile.update", g.ID, string(detail)); err != nil {
					return err
				}
			}
			result = append(result, MarkSixProfileResult{SessionID: g.ID, Configured: preset.Configured, Preserved: preset.Preserved, Changed: changed})
		}
		if len(result) != 8 {
			return errors.New("六合彩场次数量与本次八场发布方案不一致")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type MarkSixOddsPreset struct {
	ProfileID  string  `json:"profile_id"`
	Quotes     []Quote `json:"quotes"`
	Configured int     `json:"configured"`
	Preserved  int     `json:"preserved"`
}

// Explicit, reviewable preset. Opening a database never silently overwrites
// operator prices. Unspecified markets, paused switches and old orders stay as-is.
func (s *Store) MarkSixOddsPreset(session string) (MarkSixOddsPreset, error) {
	b, err := s.MarketBook(session)
	if err != nil {
		return MarkSixOddsPreset{}, err
	}
	return MarkSixPresetForBook(b)
}

func MarkSixPresetForBook(b MarketBook) (MarkSixOddsPreset, error) {
	if b.MarkSixContext == nil {
		return MarkSixOddsPreset{}, errors.New("仅支持六合彩场次")
	}
	if len(b.Markets) != len(b.Quotes) {
		return MarkSixOddsPreset{}, errors.New("赔率表不完整，请重新读取")
	}
	byID := make(map[string]markets.Market, len(b.Markets))
	for _, m := range b.Markets {
		byID[m.ID] = m
	}
	out := MarkSixOddsPreset{ProfileID: MarkSixOddsProfileID, Quotes: make([]Quote, 0, len(b.Quotes))}
	seen := map[string]bool{}
	for _, q := range b.Quotes {
		m, exists := byID[q.MarketID]
		if !exists || seen[q.MarketID] {
			return MarkSixOddsPreset{}, errors.New("赔率表含未知玩法")
		}
		seen[q.MarketID] = true
		q.TierOddsMilli = maps.Clone(q.TierOddsMilli)
		configured := true
		switch {
		case m.Kind == "number" && (m.UIGroup == "special" || m.UIGroup == "position_numbers"):
			q.OddsMilli = 42000
		case m.Kind == "lhc_any_number":
			q.OddsMilli = 5000
		case strings.HasPrefix(m.ID, "lhc6.sum."):
			q.OddsMilli = 1950
		case m.UIGroup == "position_faces":
			q.OddsMilli = faceOdds(m, 1960, 2650, 2750, 2750)
		case m.UIGroup == "position_numbers":
			q.OddsMilli = faceOdds(m, 1980, 2700, 2900, 2900)
		case m.UIGroup == "special" && isProfileFace(m):
			q.OddsMilli = faceOdds(m, 1980, 2700, 2900, 2900)
		case m.UIGroup == "zodiac_tail" && m.Kind == "lhc_zodiac":
			q.OddsMilli, q.CurrentZodiacOddsMilli = 1850, 1700
		case m.ID != "" && strings.HasPrefix(m.ID, "lhc6.any.tail."):
			q.OddsMilli = 1850
		case m.UIGroup == "special_zodiac":
			q.OddsMilli, q.CurrentZodiacOddsMilli = 11600, 9500
		case m.Kind == "lhc_half_wave":
			q.OddsMilli = 5580
		case m.Kind == "lhc_half_digit":
			q.OddsMilli = 5300
		case m.ID == "lhc6.link.two_special":
			q.OddsMilli = 33000
			q.TierOddsMilli = map[string]int64{"special": 33000, "hit2": 52000}
		case m.Kind == "lhc_combined_zodiac":
			q.OddsMilli = map[int]int64{2: 5300, 3: 3500, 4: 2350, 5: 2000, 6: 1900, 7: 1600, 8: 1400, 9: 1230, 10: 1130, 11: 1040}[m.Selection.PickCount]
			q.CurrentZodiacOddsMilli = 0
			if m.Selection.PickCount == 2 {
				q.CurrentZodiacOddsMilli = 4800
			}
		case m.Kind == "lhc_miss":
			n, _ := strconv.Atoi(m.Pick)
			q.OddsMilli = map[int]int64{5: 2100, 6: 2400, 7: 2900, 8: 3800, 9: 4100, 10: 4900, 11: 6300, 12: 1900}[n]
		case m.Kind == "lhc_link_tail":
			n, _ := strconv.Atoi(m.Pick)
			q.OddsMilli = map[int]int64{2: 3100, 3: 7000, 4: 16000, 5: 42000}[n]
			q.ZeroTailOddsMilli = 0
			if n == 2 {
				q.ZeroTailOddsMilli = 4000
			}
		case m.Kind == "lhc_link_zodiac":
			n, _ := strconv.Atoi(m.Pick)
			q.OddsMilli = map[int]int64{2: 3500, 3: 10000, 4: 30000, 5: 50000}[n]
			q.CurrentZodiacOddsMilli = map[int]int64{2: 3000, 3: 9000, 4: 28000, 5: 25000}[n]
		case m.Kind == "lhc_miss_zodiac":
			n, _ := strconv.Atoi(m.Pick)
			q.OddsMilli = map[int]int64{2: 3000, 3: 5500, 4: 20000}[n]
			q.CurrentZodiacOddsMilli = map[int]int64{2: 2500, 3: 5000, 4: 15000}[n]
		default:
			configured = false
		}
		if err := validateMarketQuote(m, q); err != nil {
			return MarkSixOddsPreset{}, err
		}
		if configured {
			out.Configured++
		} else {
			out.Preserved++
		}
		out.Quotes = append(out.Quotes, q)
	}
	return out, nil
}

func isProfileFace(m markets.Market) bool {
	switch m.Kind {
	case "lhc_size", "lhc_parity", "lhc_digit_size", "lhc_digit_parity", "lhc_tail_size", "lhc_color":
		return true
	}
	return false
}
func faceOdds(m markets.Market, face, red, blue, green int64) int64 {
	if m.Kind == "lhc_color" {
		return map[string]int64{"red": red, "blue": blue, "green": green}[m.Pick]
	}
	return face
}
