package domains

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	mu        sync.Mutex
	c         Config
	state     State
	resolve   Resolver
	run       Runner
	conflicts func(context.Context, string, []string) error
}

func New(c Config) (*Manager, error) {
	d, e := Normalize(c.Primary)
	if e != nil {
		return nil, e
	}
	c.Primary = d
	ip := net.ParseIP(c.IP)
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return nil, errors.New("服务器公网 IPv4 配置不正确")
	}
	for _, p := range []string{c.StateFile, c.NginxFile, c.Webroot, c.CertRoot, c.Root} {
		if !filepath.IsAbs(p) || strings.ContainsAny(p, " \n\r\t;{}\"'$\\") {
			return nil, errors.New("服务路径配置不正确")
		}
	}
	m := &Manager{c: c, resolve: net.DefaultResolver, run: command, conflicts: checkConflicts, state: State{Version: 1, Primary: d, IP: c.IP, Slots: make([]Slot, 5)}}
	raw, e := os.ReadFile(c.StateFile)
	if e == nil {
		var disk diskState
		if e = json.Unmarshal(raw, &disk); e != nil {
			return nil, e
		}
		m.state = disk.State
		m.state.Retired = disk.Retired
		if m.state.Primary != d || m.state.IP != c.IP || len(m.state.Slots) != 5 {
			return nil, errors.New("现有状态与主域名/IP配置不一致")
		}
		seen := map[string]bool{d: true}
		for _, slot := range append(append([]Slot{}, m.state.Slots...), m.state.Retired...) {
			if slot.Domain == "" {
				continue
			}
			v, e := Normalize(slot.Domain)
			if e != nil || v != slot.Domain || seen[v] {
				return nil, errors.New("域名状态文件无效")
			}
			seen[v] = true
		}
		if m.state.Busy {
			m.state.Busy = false
			m.state.Message = "上次任务因服务重启中断，请重新检测或启用"
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	return m, nil
}
func command(ctx context.Context, name string, args ...string) error {
	out, e := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if e != nil {
		slog.Error("domain command failed", "command", name, "error", e, "output", string(out))
		return fmt.Errorf("%s 执行失败，请查看服务器域名服务日志", filepath.Base(name))
	}
	return nil
}
func clone(s State) State {
	s.Slots = append([]Slot{}, s.Slots...)
	s.Retired = append([]Slot{}, s.Retired...)
	return s
}
func (m *Manager) Snapshot() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := clone(m.state)
	for i := range s.Slots {
		if s.Slots[i].Domain != "" {
			if expiry, e := certificate(m.c, s.Slots[i].Domain); e == nil {
				s.Slots[i].SSL = "ready"
				s.Slots[i].Expires = expiry.UTC().Format(time.RFC3339)
			} else if s.Slots[i].SSL == "ready" {
				s.Slots[i].SSL = "expired"
			}
		}
	}
	return s
}
func atomicWrite(path string, raw []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".domains-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp, path)
}
func (m *Manager) persist(s State) error {
	raw, e := json.MarshalIndent(diskState{State: s, Retired: s.Retired}, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(m.c.StateFile, raw, 0600)
}
func (m *Manager) nginx(ctx context.Context, s State) error {
	old, e := os.ReadFile(m.c.NginxFile)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	existed := e == nil
	if e = atomicWrite(m.c.NginxFile, []byte(Render(m.c, s)), 0644); e != nil {
		return e
	}
	rollback := func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var err error
		if existed {
			err = atomicWrite(m.c.NginxFile, old, 0644)
		} else {
			err = os.Remove(m.c.NginxFile)
		}
		if err != nil {
			slog.Error("domain nginx rollback failed", "error", err)
		}
		if e := m.run(rollbackCtx, "/usr/sbin/nginx", "-t"); e == nil {
			_ = m.run(rollbackCtx, "/bin/systemctl", "reload", "nginx")
		}
	}
	if e = m.run(ctx, "/usr/sbin/nginx", "-t"); e != nil {
		rollback()
		return e
	}
	if e = m.run(ctx, "/bin/systemctl", "reload", "nginx"); e != nil {
		rollback()
		return e
	}
	return nil
}
func (m *Manager) commit(ctx context.Context, next State) error {
	old := clone(m.state)
	if e := m.nginx(ctx, next); e != nil {
		return e
	}
	if e := m.persist(next); e != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if rollbackErr := m.nginx(rollbackCtx, old); rollbackErr != nil {
			slog.Error("domain state rollback failed", "error", rollbackErr)
		}
		return e
	}
	m.state = next
	return nil
}
func (m *Manager) Save(ctx context.Context, in Input) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Busy {
		return State{}, errors.New("正在处理域名任务，请稍后保存")
	}
	if in.Version != m.state.Version {
		return State{}, errors.New("配置已更新，请重新读取后再保存")
	}
	if len(in.Domains) != 5 {
		return State{}, errors.New("必须提交五个备用域名位置")
	}
	next := clone(m.state)
	seen := map[string]bool{m.c.Primary: true}
	names := make([]string, 5)
	for i, raw := range in.Domains {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		d, e := Normalize(raw)
		if e != nil {
			return State{}, e
		}
		if seen[d] {
			return State{}, errors.New("备用域名不能重复，也不能填写主域名")
		}
		seen[d] = true
		names[i] = d
	}
	if e := m.conflicts(ctx, m.c.NginxFile, names); e != nil {
		return State{}, e
	}
	existing := map[string]Slot{}
	for _, s := range append(append([]Slot{}, next.Slots...), next.Retired...) {
		if s.Domain != "" {
			existing[s.Domain] = s
		}
	}
	next.Slots = make([]Slot, 5)
	for i, d := range names {
		if d == "" {
			continue
		}
		s, ok := existing[d]
		if !ok {
			s = Slot{Domain: d, DNS: "unchecked", SSL: "missing"}
		}
		next.Slots[i] = s
		delete(existing, d)
	}
	next.Retired = nil
	for _, s := range existing {
		s.Enabled = false
		next.Retired = append(next.Retired, s)
	}
	next.Version++
	next.Message = "域名已保存；新增域名默认禁用，请检测解析后申请 SSL 并启用"
	if e := m.commit(ctx, next); e != nil {
		return State{}, e
	}
	slog.Info("backup domains saved", "actor", in.Actor, "domains", names, "version", next.Version)
	return clone(m.state), nil
}
func (m *Manager) Start(in Action) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Busy {
		return State{}, errors.New("已有域名任务在处理，请等待完成")
	}
	if in.Version != m.state.Version {
		return State{}, errors.New("配置已更新，请重新读取")
	}
	if in.Action != "check" && in.Action != "enable" && in.Action != "disable" {
		return State{}, errors.New("不支持的域名操作")
	}
	if in.Index < 0 || in.Index >= 5 {
		return State{}, errors.New("域名位置不正确")
	}
	indices := []int{}
	for i, s := range m.state.Slots {
		if s.Domain != "" && in.Index == i {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return State{}, errors.New("请先填写并保存域名")
	}
	next := clone(m.state)
	next.Busy = true
	next.Version++
	next.Message = "正在处理，请等待…"
	if e := m.persist(next); e != nil {
		return State{}, e
	}
	m.state = next
	go m.work(indices, in)
	return clone(m.state), nil
}
func (m *Manager) work(indices []int, in Action) {
	for _, i := range indices {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		m.mu.Lock()
		slot := m.state.Slots[i]
		snapshot := clone(m.state)
		m.mu.Unlock()
		var e error
		if in.Action != "disable" {
			var ips string
			if in.Action == "enable" {
				e = m.conflicts(ctx, m.c.NginxFile, []string{slot.Domain})
			}
			if e == nil {
				ips, e = CheckDNS(ctx, m.resolve, slot.Domain, m.c.IP)
			}
			slot.Checked = time.Now().UTC().Format(time.RFC3339)
			if e != nil {
				slot.DNS = "failed"
			} else {
				slot.DNS = ips
			}
			if e == nil && in.Action == "enable" {
				expiry, certErr := certificate(m.c, slot.Domain)
				if certErr != nil || time.Until(expiry) < 30*24*time.Hour {
					// First expose only HTTP ACME challenge plus 404. No website access is
					// enabled until DNS validation, certificate issuance and nginx test pass.
					e = m.nginx(ctx, snapshot)
					if e == nil {
						e = m.run(ctx, "/usr/bin/certbot", "certonly", "--webroot", "--webroot-path", m.c.Webroot, "--cert-name", certName(slot.Domain), "-d", slot.Domain, "--non-interactive", "--agree-tos", "--register-unsafely-without-email", "--keep-until-expiring")
					}
				}
				if e == nil {
					expiry, e = certificate(m.c, slot.Domain)
					if e == nil {
						slot.SSL = "ready"
						slot.Expires = expiry.UTC().Format(time.RFC3339)
						slot.Enabled = true
					}
				}
			}
		} else {
			slot.Enabled = false
		}
		slot.Error = ""
		if e != nil {
			slot.Error = e.Error()
		}
		m.mu.Lock()
		next := clone(m.state)
		next.Slots[i] = slot
		next.Version++
		if e == nil && in.Action != "check" {
			if err := m.commit(ctx, next); err != nil {
				next.Slots[i] = m.state.Slots[i]
				next.Slots[i].Error = err.Error()
				_ = m.persist(next)
				m.state = next
			}
		} else {
			if err := m.persist(next); err != nil {
				slog.Error("domain result persistence failed", "error", err)
			}
			m.state = next
		}
		slog.Info("backup domain action", "actor", in.Actor, "action", in.Action, "domain", slot.Domain, "error", m.state.Slots[i].Error)
		m.mu.Unlock()
		cancel()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := clone(m.state)
	next.Busy = false
	next.Version++
	next.Message = "任务处理完成，请查看各域名状态"
	if e := m.persist(next); e != nil {
		slog.Error("domain task completion persistence failed", "error", e)
	}
	m.state = next
}
