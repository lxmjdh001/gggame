// Package domains manages only the five optional website names. It runs in a
// separate local service; the account backend never receives root privileges.
package domains

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Normalize(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	if len(d) > 253 || !strings.Contains(d, ".") || net.ParseIP(d) != nil {
		return "", errors.New("请输入完整域名，不要填写协议、路径、端口或 IP")
	}
	for _, label := range strings.Split(d, ".") {
		if !labelPattern.MatchString(label) {
			return "", errors.New("域名格式不正确；中文域名请填写 Punycode")
		}
	}
	tld := d[strings.LastIndex(d, ".")+1:]
	if len(tld) < 2 || strings.Trim(tld, "0123456789") == "" {
		return "", errors.New("域名后缀不正确")
	}
	return d, nil
}

type Slot struct {
	Domain  string `json:"domain"`
	Enabled bool   `json:"enabled"`
	DNS     string `json:"dns"`
	SSL     string `json:"ssl"`
	Expires string `json:"expires,omitempty"`
	Checked string `json:"checked,omitempty"`
	Error   string `json:"error,omitempty"`
}
type State struct {
	Version int    `json:"version"`
	Primary string `json:"primary"`
	IP      string `json:"ip"`
	Slots   []Slot `json:"slots"`
	Retired []Slot `json:"-"`
	Busy    bool   `json:"busy"`
	Message string `json:"message"`
}

// Disk state includes retired names so replacing an input cannot expose the old
// name through nginx's default virtual host after a restart.
type diskState struct {
	State
	Retired []Slot `json:"retired"`
}
type Input struct {
	Version int      `json:"version"`
	Domains []string `json:"domains"`
	Actor   string   `json:"actor"`
}
type Action struct {
	Version int    `json:"version"`
	Index   int    `json:"index"`
	Action  string `json:"action"`
	Actor   string `json:"actor"`
}
type Config struct{ Primary, IP, StateFile, NginxFile, Webroot, CertRoot, Root string }
type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}
type Runner func(context.Context, string, ...string) error

func CheckDNS(ctx context.Context, r Resolver, d, expected string) (string, error) {
	ips, e := r.LookupIPAddr(ctx, d)
	if e != nil {
		return "", errors.New("未查到解析，请添加 A 记录并等待生效")
	}
	if len(ips) == 0 {
		return "", errors.New("域名没有 A/AAAA 解析")
	}
	seen := []string{}
	target := net.ParseIP(expected)
	for _, ip := range ips {
		seen = append(seen, ip.IP.String())
		if !ip.IP.Equal(target) {
			return strings.Join(seen, ", "), fmt.Errorf("解析含非本站地址 %s；请只保留指向 %s 的 A 记录，并移除未配置的 AAAA/CDN 代理", ip.IP, target)
		}
	}
	return strings.Join(seen, ", "), nil
}
func certName(d string) string { return fmt.Sprintf("starlight-%x", sha256.Sum256([]byte(d))) }
func certPaths(c Config, d string) (string, string) {
	base := filepath.Join(c.CertRoot, "live", certName(d))
	return filepath.Join(base, "fullchain.pem"), filepath.Join(base, "privkey.pem")
}
func certificate(c Config, d string) (time.Time, error) {
	path, key := certPaths(c, d)
	raw, e := os.ReadFile(path)
	if e != nil {
		return time.Time{}, e
	}
	if _, e = os.Stat(key); e != nil {
		return time.Time{}, e
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return time.Time{}, errors.New("证书格式无效")
	}
	cert, e := x509.ParseCertificate(block.Bytes)
	if e != nil {
		return time.Time{}, e
	}
	if e = cert.VerifyHostname(d); e != nil {
		return time.Time{}, e
	}
	if time.Now().Before(cert.NotBefore) || !time.Now().Before(cert.NotAfter) {
		return time.Time{}, errors.New("证书尚未生效或已过期")
	}
	return cert.NotAfter, nil
}
