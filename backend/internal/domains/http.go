package domains

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	respond := func(w http.ResponseWriter, s State, e error) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if e != nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": e.Error()}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": s})
	}
	decode := func(w http.ResponseWriter, r *http.Request, v any) error {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		d.DisallowUnknownFields()
		if e := d.Decode(v); e != nil {
			return errors.New("请求格式错误")
		}
		if d.Decode(new(any)) != io.EOF {
			return errors.New("请求格式错误")
		}
		return nil
	}
	mux.HandleFunc("GET /domains", func(w http.ResponseWriter, r *http.Request) { respond(w, m.Snapshot(), nil) })
	mux.HandleFunc("PUT /domains", func(w http.ResponseWriter, r *http.Request) {
		var in Input
		if e := decode(w, r, &in); e != nil {
			respond(w, State{}, e)
			return
		}
		s, e := m.Save(r.Context(), in)
		respond(w, s, e)
	})
	mux.HandleFunc("POST /domains/actions", func(w http.ResponseWriter, r *http.Request) {
		var in Action
		if e := decode(w, r, &in); e != nil {
			respond(w, State{}, e)
			return
		}
		s, e := m.Start(in)
		respond(w, s, e)
	})
	return mux
}

// Client connects only to a root-created local Unix socket, never to an HTTP
// endpoint supplied by an administrator or domain input.
func Client(socket string) *http.Client {
	return &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
}
func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.nginx(ctx, m.state); e != nil {
		return e
	}
	return m.persist(m.state)
}
