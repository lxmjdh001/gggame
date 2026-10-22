package accounts

import (
	"errors"
	"net/http"
	"starlight/backend/internal/games"
	drawstore "starlight/backend/internal/store"
	"time"
)

type randomDrawStore interface {
	RandomPreview(games.Session, string, string, bool, time.Time) (drawstore.DrawPreview, error)
	ApplyPreview(games.Session, string, string, string, time.Time) (*games.Round, error)
}
type manualDrawStore interface {
	ManualPreview(games.Session, string, []int, []int, time.Time) (drawstore.DrawPreview, error)
}
type previewInput struct {
	Numbers         []int  `json:"numbers"`
	ExpectedNumbers []int  `json:"expected_numbers"`
	Issue           string `json:"issue"`
	CandidateID     string `json:"candidate_id"`
	Random          bool   `json:"random"`
}
type previewResponse struct {
	drawstore.DrawPreview
	CurrentEstimate   NextEstimate  `json:"current_estimate"`
	CandidateEstimate *NextEstimate `json:"candidate_estimate"`
}

func (h *HTTP) previewDraw(w http.ResponseWriter, r *http.Request, u User) {
	var in previewInput
	if err := decode(w, r, &in); err != nil {
		result(w, nil, err)
		return
	}
	g, found := games.Find(r.PathValue("session"))
	if !found {
		result(w, nil, ErrNotFound)
		return
	}
	draws, ok := h.Draws.(randomDrawStore)
	if !ok {
		result(w, nil, errors.New("当前服务不支持随机预览"))
		return
	}
	var preview drawstore.DrawPreview
	var err error
	if in.Numbers != nil {
		manual, ok := h.Draws.(manualDrawStore)
		if !ok {
			result(w, nil, errors.New("当前服务不支持手动修改号码"))
			return
		}
		if in.Random || in.CandidateID != "" {
			result(w, nil, errors.New("手动修改不能同时随机或指定预览编号"))
			return
		}
		preview, err = manual.ManualPreview(g, in.Issue, in.Numbers, in.ExpectedNumbers, time.Time{})
	} else {
		preview, err = draws.RandomPreview(g, in.Issue, in.CandidateID, in.Random, time.Time{})
	}
	if err != nil {
		result(w, nil, err)
		return
	}
	out := previewResponse{DrawPreview: preview}
	rounds := []games.Round{preview.Current}
	if preview.Candidate != nil {
		rounds = append(rounds, preview.Candidate.Round)
	}
	estimates, err := h.Store.EstimateRounds(rounds)
	out.CurrentEstimate = estimates[0]
	if len(estimates) > 1 {
		out.CandidateEstimate = &estimates[1]
	}

	// A long calculation cannot return an actionable preview after :00.
	out.ServerTime = time.Now()
	if err == nil && !out.ServerTime.Before(out.CutoffAt) {
		err = errors.New("已超过开奖时间不可修改")
	}
	result(w, out, err)
}
func (h *HTTP) applyDraw(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Issue       string `json:"issue"`
		CandidateID string `json:"candidate_id"`
	}
	if err := decode(w, r, &in); err != nil {
		result(w, nil, err)
		return
	}
	g, found := games.Find(r.PathValue("session"))
	if !found {
		result(w, nil, ErrNotFound)
		return
	}
	draws, ok := h.Draws.(randomDrawStore)
	if !ok {
		result(w, nil, errors.New("当前服务不支持号码采用"))
		return
	}
	round, err := draws.ApplyPreview(g, in.Issue, in.CandidateID, u.ID, time.Time{})
	result(w, round, err)
}
