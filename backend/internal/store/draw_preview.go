package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"math/big"
	"slices"
	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
	"time"
)

var drawOverrides = []byte("private_draw_overrides_v1")
var drawCandidates = []byte("private_draw_candidates_v1")
var drawChangeLog = []byte("private_draw_changes_v1")

type drawOverride struct {
	Round   games.Round `json:"round"`
	Version int         `json:"version"`
}
type DrawCandidate struct {
	ID          string      `json:"id"`
	Round       games.Round `json:"round"`
	BaseVersion int         `json:"base_version"`
}
type DrawPreview struct {
	Current    games.Round    `json:"current"`
	Candidate  *DrawCandidate `json:"candidate"`
	CutoffAt   time.Time      `json:"cutoff_at"`
	ServerTime time.Time      `json:"server_time"`
}

func drawKey(session, issue string) []byte { return []byte(session + ":" + issue) }
func override(tx *bolt.Tx, session, issue string) (*drawOverride, error) {
	b := tx.Bucket(drawOverrides)
	if b == nil {
		return nil, nil
	}
	raw := b.Get(drawKey(session, issue))
	if raw == nil {
		return nil, nil
	}
	var value drawOverride
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return &value, nil
}
func previewRound(tx *bolt.Tx, g games.Session, at time.Time) (*games.Round, int, error) {
	issue := at.In(model.Beijing).Format(model.IssueLayout)
	value, err := override(tx, g.ID, issue)
	if err != nil {
		return nil, 0, err
	}
	if value != nil {
		return &value.Round, value.Version, nil
	}
	b := tx.Bucket(sourceCache)
	if b == nil {
		return nil, 0, nil
	}
	raw := b.Get([]byte(issue))
	if raw == nil {
		return nil, 0, nil
	}
	var d model.Draw
	if err = json.Unmarshal(raw, &d); err != nil {
		return nil, 0, err
	}
	round, err := games.Project(g, d, "scheduled")
	if err != nil {
		return nil, 0, err
	}
	if !round.DrawAt.Equal(at) {
		return nil, 0, errors.New("prepared result time mismatch")
	}
	return &round, 0, nil
}
func editableDraw(g games.Session, issue string, now time.Time) (time.Time, error) {
	at, err := model.ParseIssue(issue)
	if err != nil {
		return time.Time{}, errors.New("开奖期号不正确")
	}
	at = at.Add(games.SourcePublishDelay)
	if !now.Before(at.Truncate(time.Minute)) {
		return time.Time{}, errors.New("已超过开奖时间不可修改")
	}
	if !g.Matches(at) || !g.Next(now).Equal(at) {
		return time.Time{}, errors.New("期号已更新，请关闭弹窗后重试")
	}
	return at, nil
}
func sampleNumbers(g games.Session, base games.Round) (games.Round, error) {
	rule := g.NumberRuleAt(base.DrawAt)
	if rule.ID == "" {
		return base, errors.New("号码规则不存在")
	}
	numbers := make([]int, rule.Count)
	pool := make([]int, rule.Max-rule.Min+1)
	for i := range pool {
		pool[i] = rule.Min + i
	}
	for i := range numbers {
		limit := len(pool)
		if !rule.AllowRepeat {
			limit -= i
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
		if err != nil {
			return base, err
		}
		j := int(n.Int64())
		if rule.AllowRepeat {
			numbers[i] = pool[j]
		} else {
			j += i
			pool[i], pool[j] = pool[j], pool[i]
			numbers[i] = pool[i]
		}
	}
	return setNumbers(g, base, numbers)
}

func setNumbers(g games.Session, base games.Round, numbers []int) (games.Round, error) {
	rule := g.NumberRuleAt(base.DrawAt)
	if len(numbers) != rule.Count {
		return base, fmt.Errorf("请输入%d个号码", rule.Count)
	}
	if rule.ID == "" {
		return base, errors.New("号码规则不存在")
	}
	seen := map[int]bool{}
	for _, n := range numbers {
		if n < rule.Min || n > rule.Max {
			return base, fmt.Errorf("号码必须在%d至%d之间", rule.Min, rule.Max)
		}
		if !rule.AllowRepeat && seen[n] {
			return base, errors.New("该场次号码不可重复")
		}
		seen[n] = true
	}
	numbers = append([]int(nil), numbers...)
	base.Numbers = numbers
	base.DisplayNumbers = make([]string, len(numbers))
	base.Sum = nil
	sum := 0
	for i, n := range numbers {
		if g.Format == "padded" || g.Format == "marksix" {
			base.DisplayNumbers[i] = fmt.Sprintf("%02d", n)
		} else {
			base.DisplayNumbers[i] = fmt.Sprint(n)
		}
		sum += n
	}
	if g.Format == "sum" || g.Format == "triple" {
		base.Sum = &sum
	}
	return base, nil
}
func candidate(tx *bolt.Tx, id string) (*DrawCandidate, error) {
	b := tx.Bucket(drawCandidates)
	if b == nil {
		return nil, errors.New("号码预览已失效，请重新生成预览")
	}
	raw := b.Get([]byte(id))
	if raw == nil {
		return nil, errors.New("号码预览已失效，请重新生成预览")
	}
	var c DrawCandidate
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
func checkCandidate(c *DrawCandidate, g games.Session, issue string, version int) error {
	if _, err := setNumbers(g, c.Round, c.Round.Numbers); err != nil {
		return errors.New("号码规则已更新，请重新生成预览")
	}
	if c.Round.SessionID != g.ID || c.Round.Issue != issue {
		return errors.New("号码预览与场次期号不匹配")
	}
	if c.BaseVersion != version {
		return errors.New("该期号码已被其他操作修改，请重新生成预览")
	}
	return nil
}

// RandomPreview stores only a private draft; it never changes the effective draw.
func (s *Store) RandomPreview(g games.Session, issue, id string, random bool, now time.Time) (DrawPreview, error) {
	return s.drawPreview(g, issue, id, random, nil, nil, now)
}

// Manual drafts share the same cutoff, adoption transaction and audit as random drafts.
func (s *Store) ManualPreview(g games.Session, issue string, numbers, expected []int, now time.Time) (DrawPreview, error) {
	at, err := model.ParseIssue(issue)
	if err != nil {
		return DrawPreview{}, errors.New("开奖期号不正确")
	}
	count := g.NumberRuleAt(at).Count
	if len(numbers) != count || len(expected) != count {
		return DrawPreview{}, fmt.Errorf("请提供%d个新号码及原号码", count)
	}
	return s.drawPreview(g, issue, "", false, numbers, expected, now)
}

type drawSampler func(games.Session, games.Round) (games.Round, error)

// Batch generation shares one transaction, so failures cannot leave partial drafts.
func drawPreviewTx(tx *bolt.Tx, g games.Session, issue, id string, random bool, numbers, expected []int, now time.Time, sample drawSampler) (DrawPreview, error) {
	var out DrawPreview
	if now.IsZero() {
		now = time.Now()
	}
	at, err := editableDraw(g, issue, now)
	if err != nil {
		return out, err
	}
	current, version, err := previewRound(tx, g, at)
	if err != nil {
		return out, err
	}
	if current == nil {
		return out, errors.New("本期预生成号码尚未同步")
	}
	out = DrawPreview{Current: *current, CutoffAt: at.Truncate(time.Minute), ServerTime: now}
	if numbers != nil && !slices.Equal(current.Numbers, expected) {
		return out, errors.New("当前号码已变更，请关闭弹窗并刷新后重试")
	}
	if random || numbers != nil {
		var round games.Round
		if numbers != nil {
			round, err = setNumbers(g, *current, numbers)
		} else {
			round, err = sample(g, *current)
		}
		if err != nil {
			return out, err
		}
		token := make([]byte, 24)
		if _, err = rand.Read(token); err != nil {
			return out, err
		}
		out.Candidate = &DrawCandidate{ID: hex.EncodeToString(token), Round: round, BaseVersion: version}
		b, err := tx.CreateBucketIfNotExists(drawCandidates)
		if err != nil {
			return out, err
		}
		// Expired drafts cannot be adopted and need not accumulate indefinitely.
		cursor := b.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var c DrawCandidate
			if err = json.Unmarshal(v, &c); err != nil {
				return out, err
			}
			if !now.Before(c.Round.DrawAt.Truncate(time.Minute)) {
				if err = cursor.Delete(); err != nil {
					return out, err
				}
			}
		}
		raw, err := json.Marshal(out.Candidate)
		if err != nil {
			return out, err
		}
		return out, b.Put([]byte(out.Candidate.ID), raw)
	}
	if id != "" {
		out.Candidate, err = candidate(tx, id)
		if err != nil {
			return out, err
		}
		return out, checkCandidate(out.Candidate, g, issue, version)
	}
	return out, nil
}

func (s *Store) drawPreview(g games.Session, issue, id string, random bool, numbers, expected []int, now time.Time) (DrawPreview, error) {
	var out DrawPreview
	operation := func(tx *bolt.Tx) error {
		var err error
		out, err = drawPreviewTx(tx, g, issue, id, random, numbers, expected, now, sampleNumbers)
		return err
	}
	var err error
	if random || numbers != nil {
		err = s.db.Update(operation)
	} else {
		err = s.db.View(operation)
	}
	return out, err
}

// Adoption, version checking, and the immutable change record are one transaction.
// The clock is checked on arrival and after waiting for the write lock. Readers
// wait for physical commit too, so a slow commit cannot expose the previous draw
// to payout preparation. A transaction already committing must not be undone.
func (s *Store) ApplyPreview(g games.Session, issue, id, actor string, now time.Time) (*games.Round, error) {
	clock := time.Now
	if !now.IsZero() {
		clock = func() time.Time { return now }
	}
	return s.applyPreview(g, issue, id, actor, clock)
}

func (s *Store) applyPreview(g games.Session, issue, id, actor string, clock func() time.Time) (*games.Round, error) {
	if _, err := editableDraw(g, issue, clock()); err != nil {
		return nil, err
	}
	s.drawMu.Lock()
	defer s.drawMu.Unlock()
	var out *games.Round
	err := s.db.Update(func(tx *bolt.Tx) error {
		now := clock()
		at, err := editableDraw(g, issue, now)
		if err != nil {
			return err
		}
		current, version, err := previewRound(tx, g, at)
		if err != nil {
			return err
		}
		if current == nil {
			return errors.New("本期预生成号码尚未同步")
		}
		c, err := candidate(tx, id)
		if err != nil {
			return err
		}
		if err = checkCandidate(c, g, issue, version); err != nil {
			return err
		}
		if b := tx.Bucket(rounds).Bucket([]byte(sessionKey(g.ID))); b != nil && b.Get([]byte(issue)) != nil {
			return errors.New("本期结果已发布，不可修改")
		}
		b, err := tx.CreateBucketIfNotExists(drawOverrides)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(drawOverride{Round: c.Round, Version: version + 1})
		if err != nil {
			return err
		}
		if err = b.Put(drawKey(g.ID, issue), raw); err != nil {
			return err
		}
		log, err := tx.CreateBucketIfNotExists(drawChangeLog)
		if err != nil {
			return err
		}
		record, err := json.Marshal(struct {
			Actor  string      `json:"actor"`
			At     time.Time   `json:"at"`
			Before games.Round `json:"before"`
			After  games.Round `json:"after"`
		}{actor, now, *current, c.Round})
		if err != nil {
			return err
		}
		if err = log.Put([]byte(id), record); err != nil {
			return err
		}
		if !clock().Before(at.Truncate(time.Minute)) {
			return errors.New("已超过开奖时间不可修改")
		}
		out = &c.Round
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, err
}
