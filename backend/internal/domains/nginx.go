package domains

import (
	"fmt"
	"strings"
)

// Only normalized domain names and root-owned installation paths enter this
// template. No HTTP input can provide nginx directives, commands or file paths.
func Render(c Config, s State) string {
	var b strings.Builder
	b.WriteString("# Managed by starlight-domains; do not edit.\n")
	all := append(append([]Slot{}, s.Slots...), s.Retired...)
	for _, slot := range all {
		d := slot.Domain
		if d == "" {
			continue
		}
		fmt.Fprintf(&b, "server {\n listen 80;\n listen [::]:80;\n server_name %s;\n server_tokens off;\n", d)
		fmt.Fprintf(&b, " location ^~ /.well-known/acme-challenge/ { root %s; try_files $uri =404; }\n", c.Webroot)
		if slot.Enabled {
			fmt.Fprintf(&b, " location / { return 301 https://%s$request_uri; }\n", d)
		} else {
			b.WriteString(" location / { return 404; }\n")
		}
		b.WriteString("}\n")
		cert, key := certPaths(c, d)
		// A valid HTTPS response needs a previously issued certificate even when
		// access is disabled. Keep certificates and virtual hosts on disable/remove.
		if _, e := certificate(c, d); e != nil && slot.SSL != "ready" {
			continue
		}
		fmt.Fprintf(&b, "server {\n listen 443 ssl;\n listen [::]:443 ssl;\n server_name %s;\n ssl_certificate %s;\n ssl_certificate_key %s;\n ssl_protocols TLSv1.2 TLSv1.3;\n server_tokens off;\n if ($host != %s) { return 404; }\n", d, cert, key, d)
		if !slot.Enabled {
			b.WriteString(" return 404;\n}\n")
			continue
		}
		fmt.Fprintf(&b, ` root %s;
 index index.html;
 client_max_body_size 1m;
 add_header X-Content-Type-Options nosniff always;
 add_header Referrer-Policy strict-origin-when-cross-origin always;
 add_header X-Frame-Options SAMEORIGIN always;
 location /api/ {
  proxy_pass http://127.0.0.1:8082;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
  proxy_set_header X-Real-IP $remote_addr;
  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
  proxy_set_header X-Forwarded-Proto $scheme;
  proxy_connect_timeout 5s;
  proxy_read_timeout 30s;
  add_header Cache-Control "no-store" always;
 }
 location /assets/ { try_files $uri =404; expires 1y; add_header Cache-Control "public, immutable"; }
 location = /index.html { add_header Cache-Control "no-store" always; }
 location ~ /\. { deny all; }
 location / { try_files $uri $uri/ /index.html; }
}
`, c.Root)
	}
	return b.String()
}
