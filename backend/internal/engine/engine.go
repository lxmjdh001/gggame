package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"starlight/backend/internal/games"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/model"
	"sync"
	"time"
)

type Source interface {
	Latest(context.Context) (model.Draw, error)
	History(context.Context, int, string) (model.Page, error)
}
type Engine struct {
	Store  *store.Store
	Source Source
	Logger *slog.Logger
}

func randomInt(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

// This generator is only used during explicitly requested simulated history initialization.
// Live rounds are always projected from the independent source API.
func synthetic(at time.Time) (model.Draw, error) {
	d := model.Draw{SchemaVersion: model.SchemaVersion, Issue: at.Format(model.IssueLayout), Timezone: "Asia/Shanghai", ScheduledAt: at, GeneratedAt: time.Now().In(model.Beijing), Groups: map[string][]int{}}
	for _, rule := range model.Rules() {
		values := make([]int, rule.Count)
		if rule.AllowRepeat {
			for i := range values {
				v, err := randomInt(rule.Max - rule.Min + 1)
				if err != nil {
					return d, err
				}
				values[i] = v + rule.Min
			}
		} else {
			pool := make([]int, rule.Max-rule.Min+1)
			for i := range pool {
				pool[i] = rule.Min + i
			}
			for i := len(pool) - 1; i > 0; i-- {
				j, err := randomInt(i + 1)
				if err != nil {
					return d, err
				}
				pool[i], pool[j] = pool[j], pool[i]
			}
			copy(values, pool)
		}
		d.Groups[rule.ID] = values
	}
	model.Display(&d)
	return d, model.Validate(d)
}
func (e *Engine) Seed(now time.Time) error {
	// Exclude this minute so a source already being generated cannot collide with seeded history.
	cutoff := now.In(model.Beijing).Truncate(time.Minute).Add(-time.Nanosecond)
	for _, session := range games.All() {
		done, err := e.Store.Seeded(session.ID)
		if err != nil {
			return err
		}
		if done {
			continue
		}
		items := make([]games.Round, 0, 100)
		last := session.Due(cutoff)
		for i := 0; i < 100; i++ {
			at := last.Add(-time.Duration(i*session.IntervalMinutes)*time.Minute - games.SourcePublishDelay)
			issue := at.Format(model.IssueLayout)
			d, err := e.Store.SeedSource(issue)
			if errors.Is(err, store.ErrNotFound) {
				d, err = synthetic(at)
				if err != nil {
					return err
				}
				if err = e.Store.SaveSeedSource(d); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if _, present := d.Groups[session.Group]; !present {
				extra, err := synthetic(at)
				if err != nil {
					return err
				}
				d, err = e.Store.ExtendSeedSource(extra)
				if err != nil {
					return err
				}
			}
			r, err := games.Project(session, d, "simulated_history")
			if err != nil {
				return err
			}
			items = append(items, r)
		}
		if err = e.Store.CommitSeed(session.ID, items); err != nil {
			return err
		}
	}
	cursor, err := e.Store.LiveCursor()
	if err != nil {
		return err
	}
	if cursor == "" {
		return e.Store.CommitLive(nil, cutoff.Format(model.IssueLayout))
	}
	return nil
}
func (e *Engine) Sync(ctx context.Context) error {
	return e.syncAt(ctx, time.Now())
}

// Publication runs before networking, so a source outage cannot delay cached results.
func (e *Engine) syncAt(ctx context.Context, now time.Time) error {
	if err := e.publishDue(now); err != nil {
		return err
	}
	if err := e.prefetch(ctx, now); err != nil {
		return err
	}
	return e.publishDue(now)
}
func (e *Engine) prefetch(ctx context.Context, now time.Time) error {
	cursor, err := e.Store.SourceCursor()
	if err != nil {
		return err
	}
	if cursor == "" {
		cursor, err = e.Store.LiveCursor()
		if err != nil {
			return err
		}
	}
	latest, err := e.Source.Latest(ctx)
	if err != nil {
		return err
	}
	if latest.Issue <= cursor {
		return nil
	}
	pending := []model.Draw{}
	before := ""
	for {
		page, err := e.Source.History(ctx, 100, before)
		if err != nil {
			return err
		}
		stop := false
		for _, d := range page.Items {
			if d.Issue <= cursor {
				stop = true
				break
			}
			if err := model.Validate(d); err != nil {
				return err
			}
			if d.GeneratedAt.After(now) {
				return fmt.Errorf("source generation timestamp is in future")
			}
			if d.SchemaVersion < 4 && d.ScheduledAt.After(now) {
				return fmt.Errorf("future legacy source rejected")
			}
			pending = append(pending, d)
		}
		if stop || page.NextBefore == "" {
			break
		}
		if page.NextBefore == before {
			return fmt.Errorf("source pagination stalled")
		}
		before = page.NextBefore
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Issue < pending[j].Issue })
	return e.Store.CacheSources(pending)
}
func (e *Engine) publishDue(now time.Time) error {
	cursor, err := e.Store.LiveCursor()
	if err != nil {
		return err
	}
	pending, err := e.Store.DueSources(cursor, now.Add(-games.SourcePublishDelay))
	if err != nil {
		return err
	}
	for _, d := range pending {
		if err := model.Validate(d); err != nil {
			return err
		}
		items := []games.Round{}
		for _, session := range games.All() {
			if !session.MatchesSource(d.ScheduledAt) {
				continue
			}
			if _, ok := d.Groups[session.Group]; !ok {
				continue
			}
			r, err := games.Project(session, d, "scheduled")
			if err != nil {
				return err
			}
			r.PublishedAt = now.In(model.Beijing)
			items = append(items, r)
		}
		if err = e.Store.CommitLive(items, d.Issue); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) Run(ctx context.Context) {
	// Networking has its own worker so timeouts never block :05 publication.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := e.prefetch(runCtx, time.Now())
			cancel()
			if err != nil && ctx.Err() == nil {
				e.Logger.Warn("source prefetch pending", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer wg.Wait()
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		if err := e.publishDue(time.Now()); err != nil && ctx.Err() == nil {
			e.Logger.Warn("result publication pending", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
