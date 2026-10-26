package accounts

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
)

func TestAgentExportSnapshot(t *testing.T) {
	s := agentFixture(t)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, model.Beijing)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < 47; i++ {
			id := fmt.Sprintf("child%02d", i)
			if err := put(tx, "users", id, storedUser{User: User{ID: id, Username: id, InviterID: "a", Status: "active"}}); err != nil {
				return err
			}
		}
		for i, o := range []Order{{UserID: "a", Cost: 100, Status: "pending", CreatedAt: now}, {UserID: "d", Cost: 200, Status: "settled", CreatedAt: now}, {UserID: "c", Cost: 900, Status: "refunded", CreatedAt: now}, {UserID: "b", Cost: 900, Status: "pending", CreatedAt: now.AddDate(0, 0, -1)}} {
			if err := put(tx, "orders", fmt.Sprint(i), o); err != nil {
				return err
			}
		}
		return nil
	}))
	before := markSixCombinedStoredState(t, s)
	v, err := s.agentExportAt("admin", "A", now.UTC())
	must(t, err)
	if len(v.Items) != 51 || v.Scope != "A" || v.Day != "2026-10-25" || v.ExportedAt != "2026-10-25T12:00:00+08:00" || v.TodayStake != 300 {
		t.Fatal(v)
	}
	if v.Items[0].ID != "a" || v.Items[0].TeamDeposit != 90000 || v.Items[0].TeamTodayStake != 200 {
		t.Fatal(v.Items[0])
	}
	seen := map[string]bool{}
	for _, n := range v.Items {
		if n.ID != "a" && !seen[n.InviterID] {
			t.Fatal("parent not before child", n)
		}
		if seen[n.ID] {
			t.Fatal("duplicate", n.ID)
		}
		seen[n.ID] = true
	}
	all, err := s.agentExportAt("admin", "", now)
	must(t, err)
	if len(all.Items) <= len(v.Items) || all.TodayStake != 300 {
		t.Fatal(all)
	}
	foundOrphan := false
	for _, n := range all.Items {
		if n.ID == "orphan" {
			foundOrphan = n.MissingParent
		}
	}
	if !foundOrphan {
		t.Fatal("missing orphan")
	}
	if _, err = s.agentExportAt("admin", "child", now); err != ErrNotFound {
		t.Fatal(err)
	}
	raw, err := json.Marshal(all)
	must(t, err)
	for _, key := range []string{"\"phone\"", "hash", "salt", "\"name\"", "\"role\"", "created_at", "SECRET"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(key)) {
			t.Fatal("private field", key)
		}
	}
	if !reflect.DeepEqual(before, markSixCombinedStoredState(t, s)) {
		t.Fatal("export mutated database")
	}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		u, e := get[storedUser](tx, "users", "a")
		if e != nil {
			return e
		}
		u.InviterID = "d"
		return put(tx, "users", "a", u)
	}))
	if _, err = s.agentExportAt("admin", "orphan", now); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatal("cycle must fail", err)
	}
}

func TestAgentExportHTTP(t *testing.T) {
	s := agentFixture(t)
	tokens := map[string]string{}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := put(tx, "users", "disabled-admin", storedUser{User: User{ID: "disabled-admin", Username: "disabled_admin", Role: "admin", Status: "disabled"}}); err != nil {
			return err
		}
		for _, id := range []string{"admin", "member", "b", "disabled-admin"} {
			token, e := newLogin(tx, id)
			if e != nil {
				return e
			}
			tokens[id] = token
		}
		return nil
	}))
	mux := http.NewServeMux()
	(&HTTP{Store: s}).Bind(mux)
	for _, tc := range []struct {
		actor, query string
		code         int
	}{
		{"", "format=data", 401}, {"member", "format=data", 403}, {"b", "format=data", 403},
		{"disabled-admin", "format=data", 403}, {"disabled-admin", "format=xlsx", 403},
		{"admin", "format=data&username=A", 200}, {"admin", "format=xlsx", 200},
		{"admin", "format=svg", 400}, {"admin", "", 400}, {"admin", "format=data&page=1", 400},
		{"admin", "format=xlsx&parent_id=a", 400}, {"admin", "format=data&username=absent", 404},
		{"admin", "format=data&format=xlsx", 400},
	} {
		r := httptest.NewRequest("GET", "/api/v1/admin/agents/export?"+tc.query, nil)
		r.AddCookie(&http.Cookie{Name: "starlight_session", Value: tokens[tc.actor]})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(tc, w.Code, w.Body.String())
		}
		if tc.code == 200 && tc.query == "format=xlsx" {
			if !strings.Contains(w.Header().Get("Content-Disposition"), "agent-relations-") || !strings.Contains(w.Header().Get("Content-Type"), "spreadsheetml") {
				t.Fatal(w.Header())
			}
		} else {
			var body map[string]json.RawMessage
			must(t, json.Unmarshal(w.Body.Bytes(), &body))
			key := "data"
			if tc.code != 200 {
				key = "error"
			}
			if body[key] == nil {
				t.Fatal(body)
			}
		}
	}
}

func TestAgentXLSXStructureAndTypes(t *testing.T) {
	v := AgentExport{Day: "2026-10-26", ExportedAt: "2026-10-26T12:00:00+08:00", Scope: "00123", TodayStake: 200, Items: []AgentExportNode{{ID: "001", Username: "00123", InviterUsername: "=SUM(1,2)<&", Status: "active", Deposit: 12345, TeamDeposit: 1, TodayStake: 0, TeamTodayStake: 200}, {ID: "002", Username: "00999", MissingParent: true, Status: "disabled"}}}
	raw, err := agentXLSX(v)
	must(t, err)
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	must(t, err)
	parts := map[string]string{}
	for _, f := range z.File {
		r, e := f.Open()
		must(t, e)
		b, e := io.ReadAll(r)
		must(t, e)
		must(t, r.Close())
		parts[f.Name] = string(b)
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			_, e = d.Token()
			if e == io.EOF {
				break
			}
			must(t, e)
		}
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if parts[name] == "" {
			t.Fatal(name)
		}
	}
	sheet := parts["xl/worksheets/sheet1.xml"]
	for _, want := range []string{`r="A2" t="inlineStr" s="1"`, `>00123</t>`, `=SUM(1,2)&lt;&amp;`, `r="F2" t="n" s="2"><v>123.45</v>`, `state="frozen"`, `autoFilter ref="A1:I3"`, `r="A1" t="inlineStr" s="3"`, `customWidth="1"`, `>正常</t>`, `>已停用</t>`, `>记录缺失</t>`} {
		if !strings.Contains(sheet, want) {
			t.Fatal("missing", want, sheet)
		}
	}
	if strings.Contains(sheet, "<f>") || !strings.Contains(parts["xl/styles.xml"], `numFmtId="2"`) {
		t.Fatal("unsafe formula or missing money style")
	}
	for _, want := range []string{v.Day, v.ExportedAt, v.Scope, `t="n" s="2"><v>2.00</v>`, "不含本人", "不可逐行累加"} {
		if !strings.Contains(parts["xl/worksheets/sheet2.xml"], want) {
			t.Fatal("missing metadata", want)
		}
	}
	if !strings.Contains(parts["xl/styles.xml"], `<b/>`) || !strings.Contains(parts["xl/styles.xml"], `FFE8EEF7`) {
		t.Fatal("missing header style")
	}
	if !strings.Contains(parts["xl/styles.xml"], `<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>`) {
		t.Fatal("missing default Normal style")
	}
	v.Items[0].Deposit = 1000000000000000
	if _, err = agentXLSX(v); err == nil {
		t.Fatal("precision overflow accepted")
	}
	if !testing.Short() {
		if _, err = agentXLSX(AgentExport{Items: make([]AgentExportNode, agentExcelMaxRows)}); err == nil {
			t.Fatal("row overflow accepted")
		}
	}
}
