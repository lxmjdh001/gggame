package scheduler

import (
	"context"
	"log/slog"
	"starlight/gameapi/pkg/model"
	"time"
)

func StartOfDay(now time.Time) time.Time {
	t := now.In(model.Beijing)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, model.Beijing)
}
func NextDay(now time.Time) time.Time { return StartOfDay(now).AddDate(0, 0, 1) }

type Generator interface {
	Generate(time.Time) (model.Draw, error)
}

type DailyStore interface {
	PrepareDay(time.Time, func(time.Time) (model.Draw, error)) error
}
type Daily struct {
	Store     DailyStore
	Generator Generator
	Logger    *slog.Logger
}

// A day is committed atomically. Restarts reuse it; requests never generate numbers.
func (s *Daily) Run(ctx context.Context) {
	for ctx.Err() == nil {
		now := time.Now()
		err := s.Store.PrepareDay(StartOfDay(now), s.Generator.Generate)
		delay := time.Until(NextDay(now))
		if err != nil {
			s.Logger.Error("daily generation pending", "error", err)
			delay = 5 * time.Second
		} else {
			s.Logger.Info("daily numbers ready", "date", now.In(model.Beijing).Format("2006-01-02"), "batches", 1440)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
