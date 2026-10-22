package games

import (
	"reflect"
	"starlight/gameapi/pkg/model"
	"testing"
)

func TestAustraliaFiltersSourceWithoutChangingOtherRegions(t *testing.T) {
	au, _ := Find("lhc-au")
	tw, _ := Find("lhc-tw")
	// The epoch is a valid boundary for both the 15-minute and 20-minute sessions.
	at := RegionalMarkSixStart().Add(-SourcePublishDelay)
	for rotate := 0; rotate < 49; rotate++ {
		raw := []int{}
		for i := 0; i < 49; i++ {
			raw = append(raw, (i+rotate)%49+1)
		}
		d := model.Draw{Issue: at.Format(model.IssueLayout), ScheduledAt: at, Groups: map[string][]int{"g10": raw}}
		r, err := Project(au, d, "scheduled")
		if err != nil {
			t.Fatal(err)
		}
		filtered := []int{}
		for _, n := range raw {
			if n <= 45 {
				filtered = append(filtered, n)
			}
		}
		if !reflect.DeepEqual(r.Numbers, filtered[7:15]) {
			t.Fatal("wrong filtered projection", r.Numbers)
		}
		for i, p := range r.Positions {
			if raw[p-1] != r.Numbers[i] {
				t.Fatal("incorrect source provenance")
			}
		}
		r, err = Project(tw, d, "scheduled")
		if err != nil || !reflect.DeepEqual(r.Numbers, raw[:7]) {
			t.Fatal("Taiwan changed", err)
		}
		raw[48] = raw[0]
		if _, err = Project(au, d, "scheduled"); err == nil {
			t.Fatal("duplicate source accepted")
		}
	}
}
