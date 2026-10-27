package domains

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

var configHeader = regexp.MustCompile(`^# configuration file (.+):$`)
var serverNames = regexp.MustCompile(`(?m)(?:^|[;{}])\s*server_name\s+([^;]+);`)

// nginx -t only warns about duplicate names, so explicitly reject collisions
// with every unmanaged server block, including the protected primary site.
func checkConflicts(ctx context.Context, managed string, names []string) error {
	raw, e := exec.CommandContext(ctx, "/usr/sbin/nginx", "-T").CombinedOutput()
	if e != nil {
		return errors.New("无法读取当前 nginx 配置，请检查域名服务日志")
	}
	var part strings.Builder
	filename := ""
	check := func() error {
		if filename == managed {
			return nil
		}
		for _, match := range serverNames.FindAllStringSubmatch(part.String(), -1) {
			for _, token := range strings.Fields(match[1]) {
				token = strings.Trim(token, "\"'")
				for _, name := range names {
					if name != "" && strings.EqualFold(token, name) {
						return errors.New("域名已由其他网站配置使用，请先移除冲突配置：" + name)
					}
				}
			}
		}
		return nil
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if h := configHeader.FindStringSubmatch(line); h != nil {
			if e := check(); e != nil {
				return e
			}
			filename = h[1]
			part.Reset()
			continue
		}
		line = strings.SplitN(line, "#", 2)[0]
		part.WriteString(line)
		part.WriteByte('\n')
	}
	return check()
}
