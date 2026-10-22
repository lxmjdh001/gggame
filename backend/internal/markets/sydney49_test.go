package markets

import (
	"testing"
	"time"

	"starlight/backend/internal/games"
	"starlight/gameapi/pkg/model"
)

func TestSydney49StandardCatalogAndOldAu45SnapshotsStayIndependent(t *testing.T) {
	g, _ := games.Find("lhc-au")
	if g.MarketCatalogID() != "lhc" {
		t.Fatal("Sydney does not use standard current catalog")
	}
	at := games.Sydney49Start()
	round := func(numbers []int) games.Round {
		return games.Round{SessionID: g.ID, Issue: at.Format(model.IssueLayout), SourceIssue: at.Format(model.IssueLayout), SourceGroup: g.Group, Origin: "scheduled", DrawAt: at, Numbers: numbers}
	}
	special, _ := Find("lhc", "p7.n49")
	if got, err := Evaluate(special, g, round([]int{1, 2, 3, 4, 5, 6, 49})); err != nil || got != "win" {
		t.Fatal("new standard number 49 failed", got, err)
	}
	if _, err := Evaluate(special, g, round([]int{1, 2, 3, 4, 5, 6, 7, 45})); err == nil {
		t.Fatal("new standard snapshot accepted eight balls")
	}
	template, _ := findLegacyMarkSix("lhc5.combined_zodiac.in.2")
	combined, err := BindSelection(template, []int{5, 6}, at)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Evaluate(combined, g, round([]int{1, 2, 3, 4, 5, 6, 49})); err != nil || got != "win" {
		t.Fatal("Sydney combined bet failed", got, err)
	}
	before := round([]int{1, 2, 3, 4, 5, 6, 49})
	before.DrawAt = at.Add(-15 * time.Minute)
	if _, err := Evaluate(combined, g, before); err == nil {
		t.Fatal("combined snapshot accepted a legacy Sydney period")
	}
	// Candidate production uses standard v2 size/parity; workspace has v4.
	// Either must validate seven standard balls, never be confused with au45 v2.
	for _, m := range LegacyMarkSixCatalog() {
		if (m.Kind != "size" && m.Kind != "parity") || len(m.Positions) != 1 || m.Positions[0] != 7 {
			continue
		}
		if _, err := Evaluate(m, g, round([]int{1, 2, 3, 4, 5, 6, 49})); err != nil {
			t.Fatal("standard Sydney face confused with old45 snapshot", m.ID, err)
		}
	}
	old, _ := Find("lhc-au", "au45.p8.n45")
	oldRound := round([]int{1, 2, 3, 4, 5, 6, 23, 45})
	oldRound.DrawAt = at.Add(-15 * time.Minute)
	if got, err := Evaluate(old, g, oldRound); err != nil || got != "win" {
		t.Fatal("old eighth-position order no longer settles", got, err)
	}
	for _, numbers := range [][]int{{1, 2, 3, 4, 5, 6, 45}, {1, 2, 3, 4, 5, 6, 23, 49}, {1, 2, 3, 4, 5, 6, 45, 45}} {
		invalid := oldRound
		invalid.Numbers = numbers
		if _, err := Evaluate(old, g, invalid); err == nil {
			t.Fatal("old45 contract accepted wrong count/range/duplicates", numbers)
		}
	}
	other, _ := games.Find("lhc-tw")
	otherRound := oldRound
	otherRound.SessionID = other.ID
	otherRound.DrawAt = other.Due(at)
	if _, err := Evaluate(old, other, otherRound); err == nil {
		t.Fatal("old Sydney-only snapshot accepted in another region")
	}
}
