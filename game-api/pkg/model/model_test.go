package model

import (
	"testing"
	"time"
)

func TestLegacyDrawAndDisplay(t *testing.T) {
	at := time.Now().Add(-time.Minute).In(Beijing).Truncate(time.Minute).Add(time.Second)
	d := Draw{SchemaVersion: 1, Issue: at.Format(IssueLayout), Timezone: "Asia/Shanghai", ScheduledAt: at, GeneratedAt: time.Now(), Groups: map[string][]int{}}
	for _, r := range Rules()[:6] {
		v := make([]int, r.Count)
		for i := range v {
			v[i] = r.Min + i%(r.Max-r.Min+1)
		}
		d.Groups[r.ID] = v
	}
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
	Display(&d)
	if d.DisplayGroups["g6"][0] != "01" {
		t.Fatal("g6 formatting incorrect")
	}
	d.SchemaVersion = 2
	if Validate(d) == nil {
		t.Fatal("schema2 must require g7")
	}
}
func TestSchema2AndG8Migration(t *testing.T) {
	at := time.Now().Add(-time.Minute).In(Beijing).Truncate(time.Minute).Add(time.Second)
	d := Draw{SchemaVersion: 2, Issue: at.Format(IssueLayout), Timezone: "Asia/Shanghai", ScheduledAt: at, GeneratedAt: time.Now(), Groups: map[string][]int{}}
	for _, r := range Rules()[:7] {
		v := make([]int, r.Count)
		for i := range v {
			v[i] = r.Min + i%(r.Max-r.Min+1)
		}
		d.Groups[r.ID] = v
	}
	if e := Validate(d); e != nil {
		t.Fatal(e)
	}
	d.SchemaVersion = 3
	if Validate(d) == nil {
		t.Fatal("schema3 missing g8 accepted")
	}
	d.Groups["g8"] = []int{1, 2, 3, 4, 5, 6, 1, 2, 3, 4}
	if e := Validate(d); e != nil {
		t.Fatal(e)
	}
	d.Groups["g8"][9] = 7
	if Validate(d) == nil {
		t.Fatal("out of range dice accepted")
	}
}

func TestSchema4AndAdditionalLotteryGroups(t *testing.T) {
	at := time.Now().In(Beijing).Truncate(time.Minute).Add(time.Second)
	d := Draw{SchemaVersion: 4, Issue: at.Format(IssueLayout), Timezone: "Asia/Shanghai", ScheduledAt: at, GeneratedAt: at.Add(-time.Hour), Groups: map[string][]int{}}
	for _, r := range Rules()[:8] {
		for i := 0; i < r.Count; i++ {
			d.Groups[r.ID] = append(d.Groups[r.ID], r.Min+i%(r.Max-r.Min+1))
		}
	}
	if err := Validate(d); err != nil {
		t.Fatal("schema4 incompatible", err)
	}
	d.SchemaVersion = 5
	if Validate(d) == nil {
		t.Fatal("schema5 missing groups accepted")
	}
	for _, id := range []string{"g9", "g10", "g11"} {
		for n := 1; n <= 49; n++ {
			d.Groups[id] = append(d.Groups[id], n)
		}
	}
	d.GroupGeneratedAt = map[string]time.Time{"g9": at, "g10": at, "g11": at}
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
	Display(&d)
	for _, id := range []string{"g7", "g9", "g10", "g11"} {
		if d.DisplayGroups[id][0] != "01" || d.DisplayGroups[id][48] != "49" {
			t.Fatal("formatting", id)
		}
		d.Groups[id][48] = 1
		if Validate(d) == nil {
			t.Fatal("duplicate accepted", id)
		}
		d.Groups[id][48] = 50
		if Validate(d) == nil {
			t.Fatal("out of range accepted", id)
		}
		d.Groups[id][48] = 49
	}
	s, _ := Select(d, "g9", []int{1, 2, 3, 4, 5, 6, 7})
	if !s.GeneratedAt.Equal(at) {
		t.Fatal("new group timestamp lost")
	}
	s, _ = Select(d, "g7", []int{1, 2, 3, 4, 5, 6, 7})
	if !s.GeneratedAt.Equal(d.GeneratedAt) {
		t.Fatal("old group timestamp changed")
	}
}
