# 备用网站与自动 SSL

后台新增「备用网站」：显示当前主域名和公网 IPv4，另有固定五个备用域名位置。保存仅注册域名，新增位置默认禁用；仅支持逐个域名检测、申请 SSL 并启用、禁用，不提供批量操作。主域名不允许填入备用位置，也不允许在此菜单关闭。

## 解析与使用

1. 在域名商 DNS 后台添加 A 记录，记录值为本服务器 IPv4（当前配置 `209.74.79.73`）。根域名记录名 `@`，子域名 `www.example.com` 的记录名 `www`。每个完整域名独立占一格，不自动附加 www。
2. 删除指向其他地址的 A、AAAA，CDN 设为仅 DNS。必须所有解析地址都匹配此服务器才允许启用；本版不支持 CDN 代理、其他 IPv6 地址或通配域名。中文域名填 Punycode，不填协议、端口或路径。
3. 保存后检测解析；“申请 SSL 并启用”也会重新检测。HTTP 80 和 HTTPS 443 必须可由公网访问，CAA 必须允许 Let's Encrypt。签发使用 HTTP-01 webroot，不停止 nginx，不把 DNS 账号密码交给网站。
4. 多个域名共享静态文件和后端数据，无跳回主域名。Cookie 保持 host-only，因此每个域名分别登录。
5. 禁用/更换/清空已签发的域名保留其证书与独立 nginx 虚拟主机，HTTP、HTTPS 返回真实 404；旧域名不会因移除而落到主站。ACME 验证路径继续开放以便续期。新域名未签发证书之前只能保证 HTTP 404，HTTPS 在证书签发前无法建立可信连接。

## 一次性安装（通过正式发布流程执行）

首次上线需要安装独立域名服务。新增 backend、domain-manager 二进制与前端应进入同一个 release，保留现有配置、数据库和备份。**不要使用静态资源专用发布脚本来发布此次变更。**

```sh
# 在开发/构建环境构建 Linux amd64，输出到 release/bin/
cd backend
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../release/bin/backend ./cmd/server
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../release/bin/domain-manager ./cmd/domain-manager
```

生产 Ubuntu/Debian 先确认系统包 `/usr/bin/certbot` 已安装且 `certbot.timer` 存在。在包含新二进制的 current release 切换完成后，从此目录以 root 运行：

```sh
bash install-domains.sh
# 经发布流程的快照、健康检查后重启后端，读取 DOMAIN_SOCKET：
systemctl restart starlight-backend
systemctl is-active starlight-backend starlight-domains certbot.timer nginx
curl -fsS http://127.0.0.1:8082/healthz
```

`install-domains.sh` 不改主站虚拟主机、不停止 nginx，不改资金数据，不主动重启业务后端。首次初始化只创建独立的空备用域名配置，不申请证书。安装前应确认 nginx http 块包含 `/etc/nginx/conf.d/*.conf`（发行版默认）。所有安装脚本应先在完整发布包检查后执行。

服务以 root / starlight 组运行，Unix socket `/run/starlight-domains/control.sock` 为 0660，目录 0750，业务后端仍为无特权 starlight 用户。只允许固定的三个操作，不运行输入的命令、不接受文件路径。root 所有状态保存在 `/var/lib/starlight-domains/state.json`（0600），nginx 配置为 `/etc/nginx/conf.d/starlight-backups.conf`；不要给业务用户写这两个位置或其父目录的权限。

根目录、主域名与 IPv4 可由 root 在 systemd 配置调整。已存在状态与主域名/IP 不一致时服务拒绝启动，避免迁移服务器时误用旧解析规则。证书每个完整域名单独签发，cert-name 为 `starlight-<域名 SHA256>`；由 certbot.timer 自动续期，并在成功续期后执行 nginx 检查/重载钩子。保持 HTTP 80 可达。

## 失败、并发与回退

- 所有 HTTP 管理接口验证管理员、同源 JSON 和会话；请求记录进入后台操作日志，任务最终结果进入 `journalctl -u starlight-domains`。
- 版本检查防止不同管理员互相覆盖；只允许一个后台任务，任务期间不可保存或插入其他任务，GET 可读取进度。任务异步，不受业务接口/浏览器请求超时影响。
- DNS/SSL 失败保持原启用状态；未启用的域名不会在证书完成前开放网站。每次任务仅处理指定的一个域名，不影响其他域名。
- nginx 生效前执行 `nginx -t`，另检查未托管 nginx 配置中的同名域名（nginx 对同名仅警告）。失败还原原文件；重载/状态保存失败也尝试回退。证书原始错误可在服务日志及 `/var/log/letsencrypt/` 查看，接口不返回服务器日志。
- 重启读取持久化状态并重新应用受管配置；未完成任务显示中断，不自动重复申请证书。
- 发布/回退前等待域名任务结束，保留状态文件、证书、受管 nginx 配置。回退旧 backend 时菜单不可用，但已配置 nginx 域名仍按原启用/禁用状态访问；不要删除受管配置导致禁用域名落到主站。
- 线上验证至少包括：真实域名检测与签发、多个域名独立登录、HTTP 跳 HTTPS、证书 hostname/有效期、禁用后 HTTP/HTTPS 404、重新启用、替换后旧域名 404、`certbot renew --dry-run`。当前没有实际备用域名，不能宣称这些公网步骤已经验证。

参考：[Certbot webroot 与续期](https://eff-certbot.readthedocs.io/en/stable/using.html)、[nginx HTTPS](https://nginx.org/en/docs/http/configuring_https_servers.html)。
