#!/usr/bin/env bash
set -euo pipefail
release=${1:?release directory name required}
[[ "$release" =~ ^[0-9]{8}-[0-9]{6}$ ]]
base=/opt/starlight
release_dir="$base/releases/$release"
test -f "$release_dir/bin/backend"
test -f "$release_dir/bin/game-api"
test -f "$release_dir/www/index.html"
if ! id starlight >/dev/null 2>&1; then
    useradd --system --home-dir /var/lib/starlight --shell /usr/sbin/nologin starlight
fi
install -d -m 0750 -o starlight -g starlight /var/lib/starlight /var/lib/starlight/source /var/lib/starlight/backend
chmod 0755 "$base" "$base/releases" "$release_dir" "$release_dir/bin"
chmod 0755 "$release_dir/bin/backend" "$release_dir/bin/game-api"
find "$release_dir/www" -type d -exec chmod 0755 {} +
find "$release_dir/www" -type f -exec chmod 0644 {} +
if [ -e "$base/current" ] && [ ! -L "$base/current" ]; then
    echo 'Refusing to replace a non-symlink current directory' >&2
    exit 1
fi
if [ -L "$base/current" ]; then
    readlink "$base/current" > "$release_dir/previous-release.txt"
fi
ln -sfn "$release_dir" "$base/current"
install -m 0644 "$release_dir/config/starlight-source.service" /etc/systemd/system/
install -m 0644 "$release_dir/config/starlight-backend.service" /etc/systemd/system/
# Do not overwrite an existing HTTPS configuration on future updates.
if [ ! -f /etc/nginx/sites-available/js18188.com ]; then
    install -m 0644 "$release_dir/config/nginx.conf" /etc/nginx/sites-available/js18188.com
fi
ln -sfn /etc/nginx/sites-available/js18188.com /etc/nginx/sites-enabled/js18188.com
if [ -L /etc/nginx/sites-enabled/default ]; then
    unlink /etc/nginx/sites-enabled/default
fi
nginx -t
systemctl daemon-reload
systemctl enable starlight-source starlight-backend nginx
systemctl restart starlight-source
for attempt in $(seq 1 60); do
    if curl -fsS http://127.0.0.1:8081/readyz >/dev/null; then break; fi
    sleep 1
done
curl -fsS http://127.0.0.1:8081/readyz
systemctl restart starlight-backend
for attempt in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:8082/healthz >/dev/null; then break; fi
    sleep 1
done
curl -fsS http://127.0.0.1:8082/healthz
systemctl reload nginx
systemctl is-active starlight-source starlight-backend nginx
