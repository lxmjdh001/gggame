package games

import (
	"fmt"
	"starlight/gameapi/pkg/model"
	"time"
)

const DrawSecond = 5
const CloseBeforeMinute = 10
const ClosedWindowSeconds = CloseBeforeMinute + DrawSecond

// Source slots retain :01 for compatibility; numbers are now prepared at midnight.
// Business results publish at :05, four seconds after the source slot timestamp.
const SourcePublishDelay = 4 * time.Second

type Session struct {
	ID              string `json:"id"`
	GameID          string `json:"game_id"`
	GameName        string `json:"game_name"`
	Name            string `json:"name"`
	IntervalMinutes int    `json:"interval_minutes"`
	OffsetMinutes   int    `json:"offset_minutes"`
	Group           string `json:"group"`
	Positions       []int  `json:"positions"`
	Format          string `json:"format"`
	Rule            string `json:"rule"`
}

func All() []Session {
	return []Session{
		{"ssc-10", "ssc", "时时彩", "10分钟场", 10, 0, "g2", []int{1, 2, 3, 4, 5}, "digits", "0–9 五位不重复号码，每10分钟一期"},
		{"ffc-1", "ffc", "分分彩", "1分钟场", 1, 0, "g1", []int{1, 2, 3, 4, 5}, "digits", "取第一组前5位，可重复"},
		{"ffc-3", "ffc", "分分彩", "3分钟场", 3, 0, "g1", []int{6, 7, 8, 9, 10}, "digits", "取第一组后5位，可重复"},
		{"ffc-5", "ffc", "分分彩", "5分钟场", 5, 0, "g1", []int{3, 4, 5, 6, 7}, "digits", "取第一组第3至7位，可重复"},
		{"k3-1", "k3", "极速快三", "1分钟场", 1, 0, "g8", []int{1, 2, 3}, "triple", "每1分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组前3位"},
		{"k3-3", "k3", "福彩快三", "3分钟场", 3, 0, "g8", []int{8, 9, 10}, "triple", "每3分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组后3位"},
		{"k3-5", "k3", "幸运快三", "5分钟场", 5, 0, "g8", []int{4, 5, 6}, "triple", "每5分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组第4、5、6位"},
		{"k3-zj", "k3", "浙江快三", "20分钟场", 20, 0, "g8", []int{1, 4, 7}, "triple", "每20分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组第1、4、7位"},
		{"k3-fj", "k3", "福建快三", "20分钟场", 20, 0, "g8", []int{2, 5, 8}, "triple", "每20分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组第2、5、8位"},
		{"k3-gd", "k3", "广东快三", "20分钟场", 20, 0, "g8", []int{3, 6, 9}, "triple", "每20分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组第3、6、9位"},
		{"k3-hn", "k3", "湖南快三", "20分钟场", 20, 0, "g8", []int{4, 7, 10}, "triple", "每20分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组第4、7、10位"},
		{"k3-tw", "k3", "台湾快三", "20分钟场", 20, 0, "g8", []int{1, 5, 9}, "triple", "每20分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组第1、5、9位"},
		{"k3-gx", "k3", "广西快三", "20分钟场", 20, 0, "g8", []int{2, 6, 10}, "triple", "每20分钟开奖，提前10秒截止：1–6骰子号码，可重复；取第八组第2、6、10位"},
		{"pk10-10", "pk10", "北京PK10", "10分钟场", 10, 0, "g6", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, "padded", "01–10 不重复排列，按顺序对应第1至第10名"},
		{"lhc-daily", "lhc", "大马六合彩", "5分钟场", 5, 0, "g7", []int{1, 2, 3, 4, 5, 6, 7}, "marksix", "每5分钟开奖，提前1分钟截止：1–49，6个正码＋1个特别号；单式选6个不同号码"},
		{"lhc-au", "lhc", "悉尼六合彩", "15分钟场", 15, 0, "g10", []int{8, 9, 10, 11, 12, 13, 14}, "marksix", "每15分钟开奖，提前1分钟截止：1–49开出7个不同号码，前6个平码（正码）＋第7个特码；单式选6个不同号码。取第十组第8至14位"},
		{"lhc-tw", "lhc", "台湾六合彩", "20分钟场", 20, 0, "g10", []int{1, 2, 3, 4, 5, 6, 7}, "marksix", "每20分钟开奖，提前1分钟截止：1–49，6个正码＋1个特别号；单式选6个不同号码"},
		{"lhc-hk", "lhc", "极速六合彩", "1分钟场", 1, 0, "g9", []int{1, 2, 3, 4, 5, 6, 7}, "marksix", "每1分钟开奖，提前10秒截止：1–49，6个正码＋1个特别号；单式选6个不同号码"},
		{"lhc-sg", "lhc", "五分六合彩", "5分钟场", 5, 0, "g11", []int{1, 2, 3, 4, 5, 6, 7}, "marksix", "每5分钟开奖，提前1分钟截止：1–49，6个正码＋1个特别号；单式选6个不同号码"},
		{"lhc-bj", "lhc", "十分六合彩", "10分钟场", 10, 0, "g9", []int{8, 9, 10, 11, 12, 13, 14}, "marksix", "每10分钟开奖，提前1分钟截止：1–49，6个正码＋1个特别号；单式选6个不同号码"},
		{"lhc-id", "lhc", "印尼六合彩", "20分钟场", 20, 0, "g7", []int{8, 9, 10, 11, 12, 13, 14}, "marksix", "每20分钟开奖，提前1分钟截止：1–49，6个正码＋1个特别号；单式选6个不同号码"},
		{"lhc-vn", "lhc", "越南六合彩", "20分钟场", 20, 0, "g11", []int{8, 9, 10, 11, 12, 13, 14}, "marksix", "每20分钟开奖，提前1分钟截止：1–49，6个正码＋1个特别号；单式选6个不同号码"},
		{"pc28-5", "pc28", "PC蛋蛋", "5分钟场", 5, 0, "g1", []int{2, 5, 8}, "sum", "第一组第2、5、8位相加，和值范围0–27"},
	}
}

// NumberRule describes business results, which can differ from the raw source pool.
func (s Session) NumberRule() model.Rule {
	for _, rule := range model.Rules() {
		if rule.ID == s.Group {
			rule.Count = len(s.Positions)
			return rule
		}
	}
	return model.Rule{}
}
func (s Session) MarketCatalogID() string {
	return s.GameID
}
func Find(id string) (Session, bool) {
	for _, s := range All() {
		if s.ID == id {
			return s, true
		}
	}
	return Session{}, false
}

// The fixed epoch must not move at midnight, month boundaries, or server restarts.
func HongKongScheduleStart() time.Time {
	return time.Date(2026, 10, 13, 21, 30, DrawSecond, 0, model.Beijing)
}

// Keep the last daily issue intact, including its saved numbers and pending orders.
// New rounds begin at 22:00 after that issue settles, then run at :00/:20/:40.
func TwentyMinuteMarkSixStart() time.Time {
	return time.Date(2026, 10, 14, 22, 0, DrawSecond, 0, model.Beijing)
}

// Existing sessions retain their historical calendars before this boundary.
// Their identities and source positions remain unchanged when the cadence changes.
func RegionalMarkSixStart() time.Time {
	return time.Date(2026, 10, 15, 12, 0, DrawSecond, 0, model.Beijing)
}

// IsTwentyMinuteMarkSix identifies the sessions in the October 14 transition.
// It describes their historical schedule, not their current configured interval.
func (s Session) IsTwentyMinuteMarkSix() bool {
	return s.ID == "lhc-tw" || s.ID == "lhc-daily" || s.ID == "lhc-au"
}

// SimulatedDue preserves the original HK 48-hour seed migration. New history
// generation follows Due, which includes each historical schedule transition.
func (s Session) SimulatedDue(now time.Time) time.Time {
	if s.ID != "lhc-hk" {
		return s.Due(now)
	}
	anchor := HongKongScheduleStart()
	interval := 48 * time.Hour
	delta := now.Sub(anchor)
	steps := delta / interval
	if delta < 0 && delta%interval != 0 {
		steps--
	}
	return anchor.Add(steps * interval)
}
func (s Session) ClosedWindowSeconds() int {
	if s.GameID == "lhc" && s.ID != "lhc-hk" {
		return 60 + DrawSecond
	}
	return ClosedWindowSeconds
}

// scheduleAt selects the cadence active at now. The two newly added sessions
// use their regular cadence backwards as well, allowing simulated history.
func (s Session) scheduleAt(now time.Time) (time.Time, time.Duration) {
	now = now.In(model.Beijing)
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, DrawSecond, 0, model.Beijing).Add(time.Duration(s.OffsetMinutes) * time.Minute)
	interval := time.Duration(s.IntervalMinutes) * time.Minute
	if now.Before(RegionalMarkSixStart()) {
		switch s.ID {
		case "lhc-tw", "lhc-daily", "lhc-au":
			interval = 20 * time.Minute
			if now.Before(TwentyMinuteMarkSixStart()) {
				anchor = time.Date(now.Year(), now.Month(), now.Day(), 21, 30, DrawSecond, 0, model.Beijing)
				interval = 24 * time.Hour
			}
		case "lhc-hk":
			anchor = HongKongScheduleStart()
			interval = 48 * time.Hour
			if now.Before(HongKongScheduleStart()) {
				anchor = time.Date(now.Year(), now.Month(), now.Day(), 21, 30, DrawSecond, 0, model.Beijing)
				interval = 24 * time.Hour
			}
		case "lhc-sg", "lhc-bj":
			anchor = time.Date(now.Year(), now.Month(), now.Day(), 21, 30, DrawSecond, 0, model.Beijing)
			interval = 24 * time.Hour
		}
	}
	return anchor, interval
}

// Due retains every historical schedule. Publication stays at :05; the
// displayed draw time is :00, including the first draw after each transition.
func (s Session) Due(now time.Time) time.Time {
	anchor, interval := s.scheduleAt(now)
	delta := now.Sub(anchor)
	steps := delta / interval
	if delta < 0 && delta%interval != 0 {
		steps--
	}
	return anchor.Add(steps * interval)
}
func (s Session) Next(now time.Time) time.Time {
	_, interval := s.scheduleAt(now)
	next := s.Due(now).Add(interval)
	if s.IsTwentyMinuteMarkSix() && now.Before(TwentyMinuteMarkSixStart()) {
		if next.After(TwentyMinuteMarkSixStart()) {
			next = TwentyMinuteMarkSixStart()
		}
	}
	if s.GameID == "lhc" && now.Before(RegionalMarkSixStart()) && next.After(RegionalMarkSixStart()) {
		next = RegionalMarkSixStart()
	}
	return next
}
func (s Session) Matches(at time.Time) bool { return s.Due(at).Equal(at) }

func (s Session) MatchesSource(at time.Time) bool { return s.Matches(at.Add(SourcePublishDelay)) }

func (s Session) CloseAt(drawAt time.Time) time.Time {
	if drawAt.Before(RegionalMarkSixStart()) {
		switch s.ID {
		case "lhc-hk", "lhc-sg", "lhc-bj":
			return drawAt.Add(-(15*60 + DrawSecond) * time.Second)
		case "lhc-tw", "lhc-daily", "lhc-au":
			if drawAt.Before(TwentyMinuteMarkSixStart()) {
				return drawAt.Add(-(15*60 + DrawSecond) * time.Second)
			}
		}
	}
	return drawAt.Add(-time.Duration(s.ClosedWindowSeconds()) * time.Second)
}

func (s Session) BettingOpen(now time.Time) bool { return now.Before(s.CloseAt(s.Next(now))) }

// Stored pre-upgrade rounds retain their original :01 timestamp.
func (s Session) MatchesStored(at time.Time) bool { return s.Matches(at) || s.MatchesSource(at) }

type Round struct {
	SessionID      string    `json:"session_id"`
	Issue          string    `json:"issue"`
	SourceIssue    string    `json:"source_issue"`
	SourceGroup    string    `json:"source_group"`
	Positions      []int     `json:"positions"`
	Numbers        []int     `json:"numbers"`
	DisplayNumbers []string  `json:"display_numbers"`
	Sum            *int      `json:"sum,omitempty"`
	DrawAt         time.Time `json:"draw_at"`
	PublishedAt    time.Time `json:"published_at"`
	Origin         string    `json:"origin"`
}

func Project(s Session, d model.Draw, origin string) (Round, error) {
	if !s.MatchesSource(d.ScheduledAt) {
		return Round{}, fmt.Errorf("source does not match session boundary")
	}
	positions := s.Positions
	if s.ID == "lhc-au" {
		var err error
		positions, err = s.sydneySourcePositions(d.Groups[s.Group], d.ScheduledAt)
		if err != nil {
			return Round{}, err
		}
	}
	selection, err := model.Select(d, s.Group, positions)
	if err != nil {
		return Round{}, err
	}
	r := Round{SessionID: s.ID, Issue: d.Issue, SourceIssue: d.Issue, SourceGroup: s.Group, Positions: append([]int(nil), positions...), Numbers: selection.Numbers, DisplayNumbers: selection.DisplayNumbers, DrawAt: d.ScheduledAt.Add(SourcePublishDelay), PublishedAt: time.Now().In(model.Beijing), Origin: origin}
	if s.Format == "sum" || s.Format == "triple" {
		sum := 0
		for _, n := range r.Numbers {
			sum += n
		}
		r.Sum = &sum
	}
	return r, nil
}

type History struct {
	Items      []Round `json:"items"`
	NextBefore string  `json:"next_before,omitempty"`
	Total      int     `json:"total"`
}
type State struct {
	Session       Session   `json:"session"`
	Latest        *Round    `json:"latest"`
	ServerTime    time.Time `json:"server_time"`
	ExpectedIssue string    `json:"expected_issue"`
	Pending       bool      `json:"pending"`
	NextDrawAt    time.Time `json:"next_draw_at"`
	BetCloseAt    time.Time `json:"bet_close_at"`
	BettingOpen   bool      `json:"betting_open"`
}
