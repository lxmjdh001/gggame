package domains

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(c context.Context, d string) ([]net.IPAddr, error) { return f(c, d) }
func fixture(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	c := Config{Primary: "main.example.com", IP: "209.74.79.73", StateFile: filepath.Join(dir, "state.json"), NginxFile: filepath.Join(dir, "backups.conf"), Webroot: filepath.Join(dir, "acme"), CertRoot: dir, Root: filepath.Join(dir, "www")}
	m, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	m.run = func(context.Context, string, ...string) error { return nil }
	m.conflicts = func(context.Context, string, []string) error { return nil }
	m.resolve = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) { return []net.IPAddr{{IP: net.ParseIP(c.IP)}}, nil })
	return m
}
func saveNames(t *testing.T, m *Manager, names ...string) State {
	t.Helper()
	v := make([]string, 5)
	copy(v, names)
	s, e := m.Save(context.Background(), Input{Version: m.Snapshot().Version, Domains: v, Actor: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func sign(t *testing.T, c Config, d string) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: d}, DNSNames: []string{d}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	raw, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, keyPath := certPaths(c, d)
	if e = os.MkdirAll(filepath.Dir(cert), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), 0600); e != nil {
		t.Fatal(e)
	}
	private, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); e != nil {
		t.Fatal(e)
	}
}
func finish(t *testing.T, m *Manager) State {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Snapshot()
		if !s.Busy {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task did not finish")
	return State{}
}
func TestNormalizeRejectsDirectiveCommandAndURLInputs(t *testing.T) {
	for _, bad := range []string{"https://a.com", "a.com/path", "a.com:443", "*.a.com", "a.com;return 200;", "a.com\nfoo", "$(id).com", "-x.com", "a..com", "localhost", "127.0.0.1", "a.123", "用户.com"} {
		if _, e := Normalize(bad); e == nil {
			t.Fatal("accepted", bad)
		}
	}
	if got, e := Normalize(" A-B.Example.COM "); e != nil || got != "a-b.example.com" {
		t.Fatal(got, e)
	}
}
func TestDNSRejectsMissingMixedAndUnexpectedIPv6(t *testing.T) {
	for _, ips := range [][]net.IPAddr{nil, {{IP: net.ParseIP("1.1.1.1")}}, {{IP: net.ParseIP("209.74.79.73")}, {IP: net.ParseIP("::1")}}, {{IP: net.ParseIP("209.74.79.73")}, {IP: net.ParseIP("1.1.1.1")}}} {
		r := resolverFunc(func(context.Context, string) ([]net.IPAddr, error) { return ips, nil })
		if _, e := CheckDNS(context.Background(), r, "a.example.com", "209.74.79.73"); e == nil {
			t.Fatal("bad resolution accepted", ips)
		}
	}
	m := fixture(t)
	if _, e := CheckDNS(context.Background(), m.resolve, "a.example.com", m.c.IP); e != nil {
		t.Fatal(e)
	}
}
func TestSaveVersionDuplicatePrimaryConflictsAndPersistence(t *testing.T) {
	m := fixture(t)
	s := saveNames(t, m, "A.example.com", "b.example.com")
	if s.Slots[0].Domain != "a.example.com" || s.Slots[0].Enabled {
		t.Fatal(s)
	}
	for _, in := range []Input{{Version: 1, Domains: make([]string, 5)}, {Version: s.Version, Domains: []string{"a.com"}}, {Version: s.Version, Domains: []string{"main.example.com", "", "", "", ""}}, {Version: s.Version, Domains: []string{"a.com", "A.com", "", "", ""}}} {
		if _, e := m.Save(context.Background(), in); e == nil {
			t.Fatal("invalid save accepted", in)
		}
	}
	m.conflicts = func(context.Context, string, []string) error { return errors.New("already assigned") }
	if _, e := m.Save(context.Background(), Input{Version: s.Version, Domains: []string{"c.example.com", "", "", "", ""}}); e == nil {
		t.Fatal("conflict allowed")
	}
	restored, e := New(m.c)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(restored.Snapshot(), s) {
		t.Fatal("persisted state lost")
	}
}
func TestIndividualEnableDisableAndRemovedNamesStay404(t *testing.T) {
	m := fixture(t)
	s := saveNames(t, m, "a.example.com", "b.example.com", "c.example.com")
	var mu sync.Mutex
	issued := []string{}
	m.run = func(_ context.Context, command string, args ...string) error {
		if command == "/usr/bin/certbot" {
			d := "" // Find the domain without assuming option order.
			for i, arg := range args {
				if arg == "-d" {
					d = args[i+1]
				}
			}
			sign(t, m.c, d)
			mu.Lock()
			issued = append(issued, d)
			mu.Unlock()
		}
		return nil
	}
	for i := 0; i < 3; i++ {
		if _, e := m.Start(Action{Version: s.Version, Index: i, Action: "enable", Actor: "admin"}); e != nil {
			t.Fatal(e)
		}
		s = finish(t, m)
		for j := i + 1; j < 3; j++ {
			if s.Slots[j].Enabled {
				t.Fatal("single operation enabled another domain")
			}
		}
	}
	for _, slot := range s.Slots[:3] {
		if !slot.Enabled || slot.SSL != "ready" || slot.Error != "" {
			t.Fatal(slot)
		}
	}
	mu.Lock()
	if len(issued) != 3 {
		t.Fatal(issued)
	}
	mu.Unlock()
	if _, e := m.Start(Action{Version: s.Version, Index: 1, Action: "disable"}); e != nil {
		t.Fatal(e)
	}
	s = finish(t, m)
	if s.Slots[1].Enabled || !s.Slots[0].Enabled {
		t.Fatal("single disable changed neighbors")
	}
	raw, _ := os.ReadFile(m.c.NginxFile)
	if !strings.Contains(string(raw), "server_name b.example.com;") || strings.Count(string(raw), " return 404;") < 3 {
		t.Fatal("disabled name not retained with 404", string(raw))
	}
	s = saveNames(t, m, "d.example.com", "b.example.com", "c.example.com")
	if len(s.Retired) != 1 || s.Retired[0].Domain != "a.example.com" || s.Retired[0].Enabled {
		t.Fatal("removed name lost or active", s.Retired)
	}
	restored, e := New(m.c)
	if e != nil {
		t.Fatal(e)
	}
	if len(restored.state.Retired) != 1 {
		t.Fatal("removed names lost after restart")
	}
	for i := 0; i < 3; i++ {
		if _, e := m.Start(Action{Version: s.Version, Index: i, Action: "disable"}); e != nil {
			t.Fatal(e)
		}
		s = finish(t, m)
	}
	for _, slot := range s.Slots {
		if slot.Enabled {
			t.Fatal("individual disable failed")
		}
	}
}
func TestDNSAndSSLFailureNeverEnableNewDomain(t *testing.T) {
	for _, failure := range []string{"dns", "ssl"} {
		t.Run(failure, func(t *testing.T) {
			m := fixture(t)
			s := saveNames(t, m, "a.example.com")
			if failure == "dns" {
				m.resolve = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("dns unavailable") })
			} else {
				m.run = func(_ context.Context, cmd string, _ ...string) error {
					if cmd == "/usr/bin/certbot" {
						return errors.New("certificate issuance failed")
					}
					return nil
				}
			}
			if _, e := m.Start(Action{Version: s.Version, Index: 0, Action: "enable"}); e != nil {
				t.Fatal(e)
			}
			s = finish(t, m)
			if s.Slots[0].Enabled || s.Slots[0].Error == "" {
				t.Fatal(s)
			}
			raw, _ := os.ReadFile(m.c.NginxFile)
			if strings.Contains(string(raw), "proxy_pass") {
				t.Fatal("failed domain exposed website")
			}
		})
	}
}
func TestNginxValidationFailureRestoresConfigAndState(t *testing.T) {
	m := fixture(t)
	s := saveNames(t, m, "a.example.com")
	before, _ := os.ReadFile(m.c.NginxFile)
	calls := 0
	m.run = func(_ context.Context, cmd string, args ...string) error {
		if cmd == "/usr/sbin/nginx" {
			calls++
			if calls == 1 {
				return errors.New("invalid config")
			}
		}
		return nil
	}
	if _, e := m.Save(context.Background(), Input{Version: s.Version, Domains: []string{"b.example.com", "", "", "", ""}}); e == nil {
		t.Fatal("invalid nginx accepted")
	}
	after, _ := os.ReadFile(m.c.NginxFile)
	if string(before) != string(after) || !reflect.DeepEqual(s, m.Snapshot()) {
		t.Fatal("rollback failed")
	}
}
func TestBusyRejectsConcurrentSaveAndAction(t *testing.T) {
	m := fixture(t)
	s := saveNames(t, m, "a.example.com")
	entered, release := make(chan struct{}), make(chan struct{})
	m.resolve = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		close(entered)
		<-release
		return nil, errors.New("mock")
	})
	if _, e := m.Start(Action{Version: s.Version, Index: 0, Action: "check"}); e != nil {
		t.Fatal(e)
	}
	<-entered
	s = m.Snapshot()
	if _, e := m.Save(context.Background(), Input{Version: s.Version, Domains: make([]string, 5)}); e == nil {
		t.Fatal("busy save allowed")
	}
	if _, e := m.Start(Action{Version: s.Version, Index: 0, Action: "disable"}); e == nil {
		t.Fatal("overlapping jobs")
	}
	close(release)
	finish(t, m)
}

func TestRejectBatchAndOutOfRangeActions(t *testing.T) {
	m := fixture(t)
	s := saveNames(t, m, "a.example.com", "b.example.com")
	for _, index := range []int{-1, -2, 5} {
		for _, action := range []string{"check", "enable", "disable"} {
			if _, e := m.Start(Action{Version: s.Version, Index: index, Action: action}); e == nil {
				t.Fatal("batch/out-of-range action accepted", index, action)
			}
		}
	}
	if m.Snapshot().Busy {
		t.Fatal("invalid action started a job")
	}
}
