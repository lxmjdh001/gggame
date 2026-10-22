# Go 游戏号码 API

独立号码服务，每天北京时间 **00:00** 一次性生成当天 **1,440 批 g1–g11**，覆盖 `00:00` 至 `23:59`。业务后端按开奖期号读取已保存号码，不在开奖时临时生成。

## 号码规则

| 组 | 范围 | 数量 | 可重复 |
| --- | --- | --- | --- |
| g1 | 0–9 | 10 | 是 |
| g2 | 0–9 | 10 | 否 |
| g3 | 1–9 | 10 | 是 |
| g4 | 1–9 | 9 | 否 |
| g5 | 1–10 | 10 | 是 |
| g6 | 1–10 | 10 | 否 |
| g7 | 1–49 | 49 | 否 |
| g8 | 1–6 | 10 | 是 |
| g9 | 1–49 | 49 | 否 |
| g10 | 1–49 | 49 | 否 |
| g11 | 1–49 | 49 | 否 |

各批独立随机。采用 `crypto/rand` 和 Fisher–Yates 洗牌，保留生成顺序。g6/g7/g9/g10/g11 同时提供补零显示字段。位置从1开始。

## 启动与鉴权

在 `game-api` 目录运行 `go run ./cmd/server`，或 `go build -o bin/game-api ./cmd/server` 后运行 `./bin/game-api`。需要 Go 1.25+。

| 环境变量 | 默认值 | 用途 |
| --- | --- | --- |
| GAME_API_ADDR | 127.0.0.1:8081 | 监听地址 |
| GAME_API_DB | data/draws.db | 持久化数据库 |
| GAME_API_TOKEN | 未设置 | 优先使用的 Bearer 密钥 |
| GAME_API_TOKEN_FILE | data/source-api.token | 未配置环境密钥时自动创建32字节随机密钥，文件权限0600 |

所有 `/api/` 接口必须提供 `Authorization: Bearer <密钥>`。密钥只配置在号码源与业务后端，不放入前端代码。跨主机部署应使用 HTTPS 和相同密钥。默认无 CORS。

首次启动立即准备当天；已存在的批次保持不变。之后每天00:00自动准备新的一天。服务重启不会重抽已完成的日期。当天首次准备可能需要数秒，接口在事务提交后才能读取整天号码。

## 后端调用接口

成功返回 `{ "data": ... }`；失败返回 `{ "error": {"code":"...","message":"..."} }`。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/groups` | g1–g11规则；generation_mode=daily_pre_generated，batches_per_day=1440 |
| GET | `/api/v1/draws/{issue}` | 精确查询批次，推荐业务取号使用 |
| GET | `/api/v1/draws/{issue}/groups/{group}?positions=1,2,3` | 按固定位置选取号码 |
| GET | `/api/v1/draws?limit=100&before={issue}` | 倒序分页同步；before不包含自身 |
| GET | `/api/v1/draws/latest` | 最后已保存的批次，正常为当天23:59，**不是最近已开奖期** |
| GET | `/healthz` | 存活与下次每日生成时间，无需密钥 |
| GET | `/readyz` | 当天批次准备状态，无需密钥；未准备完成返回503 |

期号为北京时间 `YYYYMMDDHHmm`。例如 `202610111930` 是19:30使用的批次，业务结果在19:30:05公布。`scheduled_at` 保留旧协议的19:30:01，表示逻辑批次时间；**实际生成时间看 `generated_at`**，新批次通常当天00:00生成。新数据 `schema_version=5`，支持提前生成；旧版1/2/3/4数据兼容。

```sh
# 密钥通过环境变量配置，勿写入浏览器
curl -H "Authorization: Bearer $GAME_API_TOKEN" \
  'http://127.0.0.1:8081/api/v1/draws/202610111930/groups/g2?positions=1,2,3,4,5'
```

Go 客户端见 `pkg/client`；`pkg/apikey.Load(path, false)` 可读取密钥文件。业务后端示例：

```go
key, err := apikey.Load("../game-api/data/source-api.token", false)
if err != nil { return err }
api := client.New("http://127.0.0.1:8081", key)
result, err := api.Select(ctx, "202610111930", "g2", []int{1,2,3,4,5})
```

`Draw` 查询完整批次；`History` 分页同步；`Select` 返回 numbers/display_numbers；`APIError` 提供状态码和错误码。缺失期号返回404，不能改取latest代替。机器可读定义见 [OpenAPI](docs/openapi.json)。

## 持久化与发布

- 全天号码和完成标记在一个 bbolt 事务提交，失败全部回滚并重试，避免部分日期对外可见。
- 完成日期不可覆盖；若完成标记存在但批次丢失，报错并要求恢复原数据，不重新抽号。
- 首次升级保留旧分钟批次，补齐当前日期；当前日期的生成时间如实记录。不会自动补造过去日期。
- 业务后端预先缓存至私有存储；只有到该批次的05秒才把对应场次结果写入公开开奖表。已缓存日期可在号码API暂时离线时继续开奖。
- 按开奖期号直接映射，不再向前偏移一个场次周期。同批次各场次在同一事务公布，不提前公开长周期号码。
- 全天号码包含未来结果，任何未开奖批次都不得由业务后端转发至浏览器。
- 单实例、数据库文件锁、持久化同步。重启复用数据库；请备份数据库及密钥。没有多节点共享存储或选主。
- 系统时钟应保持同步；00:00触发和05秒发布属于软件调度，不是硬实时保证。发生存储故障时等待恢复，不换号。

验证：`go test -race ./...`、`go vet ./...`、`go build -o bin/game-api ./cmd/server`。

## 新增六合彩号码组

g7、g9、g10、g11 每批各自随机洗牌 01–49，组内不重复，组间可重复。完整组接口返回49个号码；六合彩游戏每期取7个：前6个为正码，第7个为特别号。

调用 `/api/v1/draws/{issue}/groups/g9?positions=1,2,3,4,5,6,7`，g10/g11 同理，沿用服务器端 Bearer 鉴权；不要把密钥或未来号码开放给浏览器。省略 positions 会返回该组全部49个号码。旧历史日期未包含新增组时返回404 GROUP_UNAVAILABLE。

升级启动时仅为当天已生成的 schema4 批次补充 g9–g11，整天在一个事务中提交。g1–g8、原 generated_at 和 scheduled_at 保持不变，新组实际生成时间记录在 group_generated_at；按组查询的 generated_at 使用该组时间。补充失败整天回滚，重启复用已保存的新组。过去日期及 schema1/2/3 不补造。后续每天直接生成11组。业务服务已有当天8组缓存继续用于现有10个场次；新增游戏映射尚未添加。

可配置不同名称游戏分别取不同组或同组不同位置（例如1–7、8–14），因此4组源足以映射6个模拟游戏；共享同组排列的结果存在关联，不能描述为6组独立随机源。
