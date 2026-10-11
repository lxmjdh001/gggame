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
		{"k3-1", "k3", "快3", "1分钟场", 1, 0, "g8", []int{1, 2, 3}, "triple", "1–6骰子号码，取第八组前3位"},
		{"k3-3", "k3", "快3", "3分钟场", 3, 0, "g8", []int{8, 9, 10}, "triple", "1–6骰子号码，取第八组后3位"},
		{"k3-5", "k3", "快3", "5分钟场", 5, 0, "g8", []int{4, 5, 6}, "triple", "1–6骰子号码，取第八组第4、5、6位"},
		{"pk10-10", "pk10", "PK10", "10分钟场", 10, 0, "g6", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, "padded", "01–10 不重复排列，按顺序对应第1至第10名"},
		{"lhc-daily", "lhc", "澳门六合彩", "每日21:30", 1440, 1290, "g7", []int{1, 2, 3, 4, 5, 6, 7}, "marksix", "项目自定义每日21:30开奖：1–49，6个正码＋1个特别号；单式选6个不同号码"},
		{"pc28-5", "pc28", "PC蛋蛋", "5分钟场", 5, 0, "g1", []int{2, 5, 8}, "sum", "第一组第2、5、8位相加，和值范围0–27"},
	}
}
func Find(id string) (Session, bool) {
	for _, s := range All() {
		if s.ID == id {
			return s, true
		}
	}
	return Session{}, false
}

// Due aligns business publication to Beijing midnight and second 05.
func (s Session) Due(now time.Time) time.Time {
	now = now.In(model.Beijing)
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, DrawSecond, 0, model.Beijing).Add(time.Duration(s.OffsetMinutes) * time.Minute)
	interval := time.Duration(s.IntervalMinutes) * time.Minute
	delta := now.Sub(anchor)
	steps := delta / interval
	if delta < 0 && delta%interval != 0 {
		steps--
	}
	return anchor.Add(steps * interval)
}
func (s Session) Next(now time.Time) time.Time {
	return s.Due(now).Add(time.Duration(s.IntervalMinutes) * time.Minute)
}
func (s Session) Matches(at time.Time) bool { return s.Due(at).Equal(at) }

func (s Session) MatchesSource(at time.Time) bool { return s.Matches(at.Add(SourcePublishDelay)) }

func (s Session) CloseAt(drawAt time.Time) time.Time {
	return drawAt.Add(-time.Duration(ClosedWindowSeconds) * time.Second)
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
	selection, err := model.Select(d, s.Group, s.Positions)
	if err != nil {
		return Round{}, err
	}
	r := Round{SessionID: s.ID, Issue: d.Issue, SourceIssue: d.Issue, SourceGroup: s.Group, Positions: append([]int(nil), s.Positions...), Numbers: selection.Numbers, DisplayNumbers: selection.DisplayNumbers, DrawAt: d.ScheduledAt.Add(SourcePublishDelay), PublishedAt: time.Now().In(model.Beijing), Origin: origin}
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
