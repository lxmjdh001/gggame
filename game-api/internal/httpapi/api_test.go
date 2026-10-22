package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"starlight/gameapi/internal/generator"
	"starlight/gameapi/internal/httpapi"
	"starlight/gameapi/internal/storage"
	"starlight/gameapi/pkg/client"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestAPIAndGoClient(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	server := httptest.NewServer((&httpapi.API{Store: s, Token: "test-token"}).Handler())
	defer server.Close()
	c := client.New(server.URL, "test-token")
	ctx := context.Background()
	_, err = c.Latest(ctx)
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 503 {
		t.Fatalf("empty latest: %v", err)
	}
	at := time.Now().Add(-2 * time.Minute).Truncate(time.Minute).Add(time.Second)
	d, err := generator.New().Generate(at)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(d); err != nil {
		t.Fatal(err)
	}
	latest, err := c.Latest(ctx)
	if err != nil || latest.Issue != d.Issue {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	selected, err := c.Select(ctx, d.Issue, "g2", []int{10, 1, 5})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selected.Numbers, []int{d.Groups["g2"][9], d.Groups["g2"][0], d.Groups["g2"][4]}) {
		t.Fatal("position order changed")
	}
	group, err := c.Select(ctx, "latest", "g4", nil)
	if err != nil || len(group.Numbers) != 9 {
		t.Fatal("g4 length incorrect")
	}
	history, err := c.History(ctx, 20, "")
	if err != nil || len(history.Items) != 1 {
		t.Fatal("history failed")
	}
	for _, id := range []string{"g7", "g9", "g10", "g11"} {
		full, err := c.Select(ctx, d.Issue, id, nil)
		if err != nil || len(full.Numbers) != 49 {
			t.Fatalf("%s full group: %v", id, err)
		}
		seven, err := c.Select(ctx, d.Issue, id, []int{1, 2, 3, 4, 5, 6, 7})
		if err != nil || !reflect.DeepEqual(seven.Numbers, d.Groups[id][:7]) {
			t.Fatalf("%s seven results: %v", id, err)
		}
		last, err := c.Select(ctx, d.Issue, id, []int{49})
		if err != nil || last.Numbers[0] != d.Groups[id][48] || len(last.DisplayNumbers[0]) != 2 {
			t.Fatalf("%s last position: %v", id, err)
		}
	}
	legacy, err := generator.New().Generate(at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	legacy.SchemaVersion = 4
	for _, id := range []string{"g9", "g10", "g11"} {
		delete(legacy.Groups, id)
	}
	if err = s.Save(legacy); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path, token, method string
		status              int
	}{
		{"/api/v1/draws/latest", "", "GET", 401},
		{"/api/v1/draws/invalid", "test-token", "GET", 400},
		{"/api/v1/draws/202613010000", "test-token", "GET", 400},
		{"/api/v1/draws/202501010000", "test-token", "GET", 404},
		{"/api/v1/draws/latest/groups/g4?positions=10", "test-token", "GET", 400},
		{"/api/v1/draws/latest/groups/g6?positions=10", "test-token", "GET", 200},
		{"/api/v1/draws/latest/groups/g1?positions=1,1", "test-token", "GET", 400},
		{"/api/v1/draws/latest/groups/g1?positions=", "test-token", "GET", 400},
		{"/api/v1/draws/latest/groups/g12", "test-token", "GET", 400},
		{"/api/v1/draws/latest/groups/g9?positions=50", "test-token", "GET", 400},
		{"/api/v1/draws/latest/groups/g10?positions=7,7", "test-token", "GET", 400},
		{"/api/v1/draws/latest/groups/g11", "", "GET", 401},
		{"/api/v1/draws/" + legacy.Issue + "/groups/g9", "test-token", "GET", 404},
		{"/api/v1/draws?limit=101", "test-token", "GET", 400},
		{"/api/v1/draws?before=invalid", "test-token", "GET", 400},
		{"/api/v1/draws/latest", "test-token", "POST", 405},
		{"/healthz", "", "GET", 200},
		{"/readyz", "", "GET", 503},
	}
	for _, tt := range cases {
		req, _ := http.NewRequest(tt.method, server.URL+tt.path, nil)
		if tt.token != "" {
			req.Header.Set("Authorization", "Bearer "+tt.token)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tt.status {
			t.Errorf("%s: status %d, want %d; %s", tt.path, resp.StatusCode, tt.status, body)
		}
	}
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/groups", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var metadata model.Envelope[model.Metadata]
	if err = json.NewDecoder(resp.Body).Decode(&metadata); err != nil || len(metadata.Data.Rules) != 11 || metadata.Data.TriggerSecond != 0 || metadata.Data.IntervalSeconds != 86400 {
		t.Fatal("metadata mismatch")
	}
}
