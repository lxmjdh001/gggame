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
