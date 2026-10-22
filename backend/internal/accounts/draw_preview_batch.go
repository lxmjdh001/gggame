package accounts

import (
	"errors"
	"net/http"
	"starlight/backend/internal/games"
	drawstore "starlight/backend/internal/store"
	"time"
)

type batchDrawStore interface {
	RandomPreviews(games.Session, string, []string, int, time.Time) ([]drawstore.DrawPreview, error)
}
type batchCandidate struct {
	Candidate *drawstore.DrawCandidate `json:"candidate"`
	Estimate  NextEstimate             `json:"estimate"`
}
type batchPreviewResponse struct {
	Current         games.Round      `json:"current"`
	CurrentEstimate NextEstimate     `json:"current_estimate"`
	Candidates      []batchCandidate `json:"candidates"`
	CutoffAt        time.Time        `json:"cutoff_at"`
	ServerTime      time.Time        `json:"server_time"`
}

func (h *HTTP) previewDrawBatch(w http.ResponseWriter, r *http.Request, u User) {
	var in struct {
		Issue        string   `json:"issue"`
		Count        int      `json:"count"`
		CandidateIDs []string `json:"candidate_ids"`
	}
	if err := decode(w, r, &in); err != nil {
		result(w, nil, err)
		return
	}
	g, ok := games.Find(r.PathValue("session"))
	if !ok {
		result(w, nil, ErrNotFound)
		return
	}
	draws, ok := h.Draws.(batchDrawStore)
	if !ok {
		result(w, nil, errors.New("当前服务不支持批量试算"))
		return
	}
	previews, err := draws.RandomPreviews(g, in.Issue, in.CandidateIDs, in.Count, time.Time{})
	if err != nil {
		result(w, nil, err)
		return
	}
	if len(previews) == 0 {
		result(w, nil, errors.New("未能读取号码预览"))
		return
	}
	out := batchPreviewResponse{Current: previews[0].Current, CutoffAt: previews[0].CutoffAt, Candidates: []batchCandidate{}}
	rounds := []games.Round{out.Current}
	for _, preview := range previews {
		if preview.Candidate != nil {
			out.Candidates = append(out.Candidates, batchCandidate{Candidate: preview.Candidate})
			rounds = append(rounds, preview.Candidate.Round)
		}
	}
	// Every comparison uses one account transaction, even if new bets arrive.
	estimates, err := h.Store.EstimateRounds(rounds)
	if err != nil {
		result(w, nil, err)
		return
	}
	out.CurrentEstimate = estimates[0]
	for i := range out.Candidates {
		out.Candidates[i].Estimate = estimates[i+1]
	}
	out.ServerTime = time.Now()
	if !out.ServerTime.Before(out.CutoffAt) {
		result(w, nil, errors.New("已超过开奖时间不可修改"))
		return
	}
	result(w, out, nil)
}
