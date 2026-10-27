#!/usr/bin/env bash
# Optional one-time service installation. Does not touch the primary vhost,
# accounts database or existing financial/game settings.
set -euo pipefail
[[ $(id -u) == 0 ]]
base=$(cd -- "$(dirname -- "$0")" && pwd)
test -x /opt/starlight/current/bin/domain-manager
command -v certbot >/dev/null
[[ $(command -v certbot) == /usr/bin/certbot ]]
/usr/sbin/nginx -t
id starlight >/dev/null
# Preflight before modifying service/configuration files.
systemctl cat certbot.timer >/dev/null
nginx_config=$(/usr/sbin/nginx -T 2>&1)
if ! [[ "$nginx_config" == *"/etc/nginx/conf.d/*.conf"* ]]; then
    echo 'nginx must include /etc/nginx/conf.d/*.conf in its http block' >&2
    exit 1
fi
install -d -m 0755 -o root -g root /var/lib/starlight-domains /var/lib/starlight-domains/acme /etc/letsencrypt /var/lib/letsencrypt /var/log/letsencrypt
install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
install -m 0755 "$base/renew-domain-certificates.sh" /etc/letsencrypt/renewal-hooks/deploy/starlight-nginx
install -m 0644 "$base/starlight-domains.service" /etc/systemd/system/starlight-domains.service
install -d -m 0755 /etc/systemd/system/starlight-backend.service.d
cat > /etc/systemd/system/starlight-backend.service.d/domains.conf <<'CONFIG'
[Unit]
Wants=starlight-domains.service
After=starlight-domains.service

[Service]
Environment=DOMAIN_SOCKET=/run/starlight-domains/control.sock
CONFIG
# Debian/Ubuntu Certbot package provides this timer; fail if renewal is absent.
systemctl cat certbot.timer >/dev/null
systemctl daemon-reload
systemctl enable --now certbot.timer starlight-domains.service
systemctl is-active --quiet starlight-domains.service
# Release workflow restarts the backend after its snapshot/backup checks.
# Avoid interrupting active betting or financial requests in this installer.
echo 'Domain service installed. Restart backend through the release workflow to load DOMAIN_SOCKET.'
