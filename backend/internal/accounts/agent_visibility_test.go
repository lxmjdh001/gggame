package accounts

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func archiveAgents(t *testing.T, s *Store, ids ...string) {
	t.Helper()
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, id := range ids {
			u, err := get[storedUser](tx, "users", id)
			if err != nil {
				return err
			}
			u.Deleted, u.Status = true, "disabled"
			if err = put(tx, "users", id, u); err != nil {
				return err
			}
		}
		return nil
	}))
}

func TestAgentVisibilityCountsThroughDeletedAncestorsAndPreservesHistory(t *testing.T) {
	s := agentFixture(t)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, id := range []string{"a", "b", "c", "d"} {
			if err := put(tx, "orders", id, Order{ID: id, UserID: id, Cost: 100, Status: "settled", CreatedAt: now}); err != nil {
				return err
			}
		}
		return nil
	}))
	archiveAgents(t, s, "b", "c", "sibling")
	before := markSixCombinedStoredState(t, s)
	p, err := s.agentTreeAt("admin", "a", "", 1, now)
	must(t, err)
	a := p.Items[0]
	if a.DirectCount != 0 || a.TeamCount != 1 || a.VisibleChildren != 1 || a.TeamDeposit != 90000 || a.TeamTodayStake != 300 || p.TodayStake != 400 {
		t.Fatal("deleted people counted or historical amounts changed", p)
	}
	branch, err := s.agentTreeAt("admin", "", "a", 1, now)
	must(t, err)
	if branch.Total != 1 || branch.Items[0].ID != "d" || branch.Items[0].DisplayParentID != "a" || branch.Items[0].InviterID != "c" || !branch.Items[0].InviterDeleted || branch.TodayStake != 300 {
		t.Fatal("surviving descendant lost or silently reparented", branch)
	}
	admin, err := s.agentTreeAt("admin", "admin", "", 1, now)
	must(t, err)
	if admin.Items[0].DirectCount != 1 || admin.Items[0].TeamCount != 2 {
		t.Fatal(admin)
	}
	for _, id := range []string{"b", "c", "sibling"} {
		if _, err = s.AgentTree("admin", strings.ToUpper(id), "", 1); err != ErrNotFound {
			t.Fatal(id, err)
		}
		if _, err = s.AgentTree("admin", "", id, 1); err != ErrNotFound {
			t.Fatal(id, err)
		}
		if _, err = s.agentExportAt("admin", id, now); err != ErrNotFound {
			t.Fatal(id, err)
		}
	}
	snapshot, err := s.agentExportAt("admin", "a", now)
	must(t, err)
	if len(snapshot.Items) != 2 || snapshot.Items[0].ID != "a" || snapshot.Items[1].ID != "d" || snapshot.Items[1].DisplayParentID != "a" || snapshot.TodayStake != 400 {
		t.Fatal("export differs from visible tree", snapshot)
	}
	raw, err := agentXLSX(snapshot)
	must(t, err)
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	must(t, err)
	for _, file := range z.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		r, err := file.Open()
		must(t, err)
		sheet, err := io.ReadAll(r)
		must(t, err)
		must(t, r.Close())
		if bytes.Count(sheet, []byte("<row ")) != 3 || !bytes.Contains(sheet, []byte("原上级已删除")) || bytes.Contains(sheet, []byte(">已删除</t>")) {
			t.Fatal("xlsx retained archived cards or lost surviving child", string(sheet))
		}
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("read changed financial history or inviter relations")
	}
	// Restoring the same account restores the original display branch immediately.
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, err := get[storedUser](tx, "users", "b")
		if err != nil {
			return err
		}
		u.Deleted = false
		return put(tx, "users", "b", u)
	}))
	p, err = s.AgentTree("admin", "", "a", 1)
	must(t, err)
	if p.Total != 1 || p.Items[0].ID != "b" || p.Items[0].Status != "disabled" || p.Items[0].TeamCount != 1 || p.Items[0].VisibleChildren != 1 || p.Parent.DirectCount != 1 || p.Parent.TeamCount != 2 {
		t.Fatal(p)
	}
}

func TestAgentVisibilityPaginationFiltersBeforeBounds(t *testing.T) {
	s := agentFixture(t)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 47; i++ {
			id := fmt.Sprintf("visible-child%02d", i)
			if err := put(tx, "users", id, storedUser{User: User{ID: id, Username: id, InviterID: "a", Status: "active", Deleted: i%2 == 0}}); err != nil {
				return err
			}
		}
		return nil
	}))
	archiveAgents(t, s, "b", "c", "d")
	seen := map[string]bool{}
	for page := 1; page <= 2; page++ {
		p, err := s.AgentTree("admin", "", "a", page)
		must(t, err)
		if p.Total != 23 || p.Page != page || p.Parent.DirectCount != 23 || p.Parent.TeamCount != 23 {
			t.Fatal(p)
		}
		for _, n := range p.Items {
			if n.Status == "deleted" || seen[n.ID] {
				t.Fatal(n)
			}
			seen[n.ID] = true
		}
	}
	if len(seen) != 23 {
		t.Fatal(len(seen))
	}
	p, err := s.AgentTree("admin", "", "a", 99)
	must(t, err)
	if p.Page != 2 || len(p.Items) != 3 {
		t.Fatal(p)
	}
}

func TestAgentVisibilityDeletedRootsAndCycles(t *testing.T) {
	s := agentFixture(t)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := put(tx, "users", "archived-root", storedUser{User: User{ID: "archived-root", Username: "archived-root", Deleted: true, Status: "disabled"}}); err != nil {
			return err
		}
		return put(tx, "users", "living-child", storedUser{User: User{ID: "living-child", Username: "living-child", InviterID: "archived-root", Status: "active"}})
	}))
	p, err := s.AgentTree("admin", "", "", 1)
	must(t, err)
	found := false
	for _, n := range p.Items {
		if n.Status == "deleted" {
			t.Fatal(n)
		}
		if n.ID == "living-child" {
			found = true
			if n.DisplayParentID != "" || !n.InviterDeleted || n.InviterID != "archived-root" {
				t.Fatal(n)
			}
		}
	}
	if !found {
		t.Fatal("living descendant of archived root disappeared")
	}
	archiveAgents(t, s, "b", "c")
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, err := get[storedUser](tx, "users", "b")
		if err != nil {
			return err
		}
		u.InviterID = "c"
		return put(tx, "users", "b", u)
	}))
	if _, err = s.AgentTree("admin", "", "", 1); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatal("hidden cycle went undetected", err)
	}
}
