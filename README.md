# rc_tobias

RightCapital AI Coding 作业：高可靠外部 HTTP 通知投递服务 MVP。

## 问题理解

企业内部业务系统在注册、付款、购买等事件发生后，需要通知不同外部供应商。业务系统不关心外部 API 返回的业务结果，但需要尽可能确认“通知请求被稳定、可靠地送达”。

本系统的边界是：接收内部系统提交的 HTTP 通知任务，持久化任务状态，异步投递到目标 URL，失败后重试，长期失败后保留人工补偿入口。

本系统明确不解决：

- 不解析供应商返回体中的业务语义，只用 HTTP 状态码判断投递是否成功。
- 不做供应商模板配置中心，MVP 直接接收目标 URL、method、headers、body。
- 不承诺 exactly-once。外部 HTTP 调用天然可能重复，系统采用至少一次投递。
- 不做管理后台、多租户权限、复杂审批流。
- 本地 Compose 不模拟 PostgreSQL 主备或 Redis Cluster；生产环境应使用托管 HA 或主备方案。

## 架构设计

技术栈：

- Go
- Gin：HTTP API
- Ent：PostgreSQL schema、迁移和常规查询
- PostgreSQL：通知任务、投递尝试、outbox 事件的事实来源
- Redis Streams：短期投递队列、consumer group、pending 消息恢复

系统架构图：

```mermaid
flowchart LR
    biz[业务系统]
    api[API 服务 Gin<br/>无状态 可多副本]
    relay[Outbox Relay<br/>可多实例]
    worker[Worker<br/>可多实例 多协程]
    pg[(PostgreSQL<br/>notifications<br/>delivery_attempts<br/>outbox_events)]
    redis[(Redis Streams<br/>notification-deliveries<br/>consumer group)]
    vendor[外部供应商 API]

    biz -->|POST /notifications| api
    api -->|写入 notifications + outbox 同一事务<br/>查询状态 · 人工 retry| pg

    relay -->|领取到期 outbox SKIP LOCKED<br/>标记 published / 失败回退| pg
    relay -->|XADD| redis
    redis -->|XREADGROUP 仅含 ID<br/>XAUTOCLAIM 恢复 pending| worker

    worker -->|按 ID 加载任务 · 标记 delivering<br/>记录尝试与结果 · 失败写下一次 outbox| pg
    worker -->|HTTP 通知 · Idempotency-Key| vendor

    api -.->|/readyz| pg
    api -.->|/readyz| redis
```

核心数据流：

1. 业务系统调用 `POST /notifications`。
2. API 在同一个 PostgreSQL 事务中写入 `notifications` 和 `outbox_events`。
3. relay 扫描到期 outbox 事件，使用 `FOR UPDATE SKIP LOCKED` 并发领取，发布到 Redis Streams。
4. worker 通过 Redis consumer group 消费消息（消息仅含通知 ID），按 ID 从 PostgreSQL 加载完整任务、标记 `delivering` 后调用外部 HTTP API。
5. 2xx 记为成功；网络错误、超时、429、5xx 等按指数退避重试。
6. 重试会在 PostgreSQL 中写入新的到期 outbox 事件。
7. 达到最大尝试次数后任务进入 `failed`，可通过人工 retry 接口重新入队。

## 可靠性策略

投递语义：**至少一次**。

选择至少一次的原因：

- HTTP 调用没有可靠的分布式事务边界。
- worker 可能在“外部 API 已收到请求，但本地未完成状态更新”时崩溃。
- Redis pending message reclaim 和 outbox 重新发布都可能导致重复投递。

因此每次请求会携带：

- `X-Notification-ID`
- `Idempotency-Key`

外部供应商或适配层应基于该 ID 做幂等处理。

失败处理：

- 2xx：`succeeded`
- 网络错误、超时、408、409、425、429、5xx：`retrying`
- 其他 4xx：`failed`
- 达到 `max_attempts`：`failed`

外部系统长期不可用时，系统不会无限重试。每次可重试失败都会按指数退避写入下一次 outbox 事件；达到 `max_attempts` 后任务进入 `failed`，保留完整尝试历史，业务方或值班人员可以通过人工 retry 接口重新入队。这个选择的判断依据是：第一版系统要保证请求不丢、状态可查、失败可补偿，但不把供应商长期故障转化为无限堆积和不可控流量。

Redis 发布失败时，outbox 事件会释放回 `pending`，等待下一轮 relay 重新发布。relay 崩溃在“Redis 发布成功、DB 未标记 published”之间时，后续会重复发布，这是为了不丢消息而接受重复。

### 持久化边界与对账（已知取舍）

PostgreSQL 是任务状态的事实来源，但有一个重要边界：**消息一旦被 relay 标记为 `published`，后续的再投递驱动就依赖 Redis，而不再依赖 PostgreSQL**。

- worker 崩溃、消息未 ack 时，由 Redis consumer group 的 `XAUTOCLAIM` 重新认领重投——前提是 Redis 仍保有该 pending 记录。
- 重试本身会回到 PostgreSQL：每次失败写入一条新的到期 outbox 事件，再由 relay 重新发布。
- 唯一不被 PostgreSQL 兜底的窗口是：消息已 `published`、但在被任何 worker 消费前，Redis 恰好在此窗口崩溃丢失了该 stream 记录。此时 relay 不会再捡它（只捡 `pending`/`publishing`），任务会滞留在非终态。

当前对这个窗口的取舍：

- Redis 开启 AOF（compose 中 `appendonly yes`），默认 `everysec` 刷盘，崩溃最多丢约 1 秒写入，这是 MVP 接受的取舍。
- 计划中的收口手段是一个 **PostgreSQL 对账器（reconciler）**：周期性扫描 `next_attempt_at` 已到期、却仍停留在 `pending`/`delivering`/`retrying` 的通知，重新写入 outbox 事件，让 PostgreSQL 真正成为再投递的兜底来源。MVP 暂不实现，避免第一版引入第四个常驻组件。

为支撑 relay 的热路径，`outbox_events` 上建有一个**部分索引**（仅覆盖 `pending`/`publishing` 行，按 `scheduled_at`、`created_at` 排序），让“领取到期事件”的扫描不随已投递历史增长而变慢；`delivery_attempts` 也建有 `(notification_id, created_at)` 索引支撑尝试历史查询。

## 安全边界

通知目标由业务系统提交，因此 API 必须避免被滥用为访问内部网络的代理。本系统实现了基础 SSRF 防护：

- 只允许 `http` 和 `https` URL。
- 只允许 `POST`、`PUT`、`PATCH` 方法。
- 创建任务时拒绝 `localhost`、loopback、private、link-local、multicast、unspecified 和 carrier-grade NAT 等内部地址。
- worker 真正发起 HTTP 请求前会再次解析目标地址并校验解析结果，降低 DNS rebinding 风险。
- HTTP client 不自动跟随重定向，避免外部 URL 通过 3xx 跳转到内部地址。

SSRF 负例：

```bash
make ssrf-test
```

该目标会提交 `http://127.0.0.1:80/metadata` 作为通知 URL。预期返回 `400 Bad Request`，错误信息包含 `target resolves to a private or internal address`，并且不会写入投递队列。

## 中间件选择与替代方案

| 组件 | 选择原因 | 不使用时的替代方案 |
| --- | --- | --- |
| PostgreSQL | 作为通知任务、投递尝试和 outbox 的事实来源，提供事务、索引、审计历史和并发领取能力。API 写任务和 outbox 可以在同一事务提交，避免“任务已创建但未入队”的丢失窗口。 | 最简替代是只用 Redis 或内存队列，但任务状态、审计和崩溃恢复会变弱；也可以用 MySQL 实现类似模型。若要进一步简化 MVP，可使用 PostgreSQL 定时轮询 outbox，不引入 Redis，但 worker 扩展和 pending 恢复能力较弱。 |
| Redis Streams | 作为轻量投递队列，支持 consumer group、并发 worker、pending message reclaim，部署和本地评审成本低于 Kafka。消息中只放通知 ID，完整状态仍在 PostgreSQL。 | 可以不用 Redis，直接让 worker 轮询 PostgreSQL outbox；实现更少，但数据库热路径压力更大，worker 消费协调也更弱。也可以换成 Kafka/RabbitMQ/SQS；Kafka 更适合长期事件日志和多订阅者场景，但对本题 MVP 偏重。 |
| Gin | API 层只需要接收任务、查询状态和人工 retry，Gin 足够轻量，启动和测试简单。 | 可替换为 Go 标准库 `net/http`，减少依赖；如果需要更完整的企业框架能力，也可用 Echo/Fiber，但本题收益不明显。 |
| Ent | 用于 schema、迁移和常规查询，减少手写 CRUD 和结构漂移。可靠性关键路径中的 outbox 领取仍使用原生 SQL `FOR UPDATE SKIP LOCKED`。 | 可全部使用 `database/sql` 或 sqlc，控制力更强但样板代码更多；对笔试 MVP 来说 Ent 在可读性和速度上更合适。 |

## API

API 面向内部业务系统和运维人员。所有请求和响应均使用 JSON；时间字段为 RFC3339 格式。

### 接口概览

| 方法 | 路径 | 说明 | 成功状态码 |
| --- | --- | --- | --- |
| `GET` | `/healthz` | 进程存活检查。 | `200` |
| `GET` | `/readyz` | 依赖就绪检查，验证 PostgreSQL 和 Redis 可用。 | `200` |
| `POST` | `/notifications` | 创建一条外部 HTTP 通知任务。 | `202` |
| `GET` | `/notifications/{id}` | 查询通知任务状态。 | `200` |
| `GET` | `/notifications/{id}/attempts` | 查询通知任务的投递尝试历史。 | `200` |
| `POST` | `/notifications/{id}/retry` | 对 `failed` 任务执行人工重新入队。 | `202` |

错误响应统一格式：

```json
{
  "error": "invalid notification request: max_attempts must be between 1 and 20"
}
```

常见错误码：

| 状态码 | 场景 |
| --- | --- |
| `400` | JSON 非法、通知 ID 非 UUID、请求字段校验失败、目标 URL 被 SSRF 规则拒绝。 |
| `404` | 通知任务不存在。 |
| `409` | 当前状态不允许该操作，例如对非 `failed` 任务调用 retry。 |
| `500` | 服务端内部错误。 |
| `503` | `/readyz` 检查依赖不可用。 |

通知状态：

| 状态 | 含义 |
| --- | --- |
| `pending` | 任务已持久化，等待 relay 发布或等待 worker 消费。 |
| `delivering` | worker 正在执行一次 HTTP 投递。 |
| `retrying` | 本次投递失败，已安排下一次重试。 |
| `succeeded` | 外部 HTTP API 返回 2xx，任务完成。 |
| `failed` | 不可重试错误或达到 `max_attempts`，等待人工补偿。 |

### 创建通知：`POST /notifications`

请求字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `url` | string | 是 | 外部供应商 HTTP(S) 地址，必须是绝对 URL，只允许 `http`/`https`，且不能解析到内部网络地址。 |
| `method` | string | 否 | HTTP 方法，默认 `POST`，仅允许 `POST`、`PUT`、`PATCH`。 |
| `headers` | object | 否 | 透传给外部供应商的请求头。Header name 必须合法，Header value 不能包含换行。 |
| `body` | JSON | 否 | 透传给外部供应商的 JSON body，最大 256 KiB。 |
| `max_attempts` | integer | 否 | 最大投递次数，默认 `5`，范围 `1` 到 `20`。 |

```bash
curl -X POST http://localhost:8080/notifications \
  -H 'Content-Type: application/json' \
  -d '{
    "url": "https://vendor.example/hooks",
    "method": "POST",
    "headers": {"Content-Type": "application/json"},
    "body": {"event": "registered", "user_id": "u_123"},
    "max_attempts": 5
  }'
```

返回 `202 Accepted`：

```json
{
  "id": "b3f86dd9-b06d-4b63-9d7f-bcdfd7d27628",
  "url": "https://vendor.example/hooks",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "event": "registered",
    "user_id": "u_123"
  },
  "status": "pending",
  "attempt_count": 0,
  "max_attempts": 5,
  "created_at": "2026-06-28T20:19:07.419052Z",
  "updated_at": "2026-06-28T20:19:07.419052Z"
}
```

### 查询通知：`GET /notifications/{id}`

```bash
curl http://localhost:8080/notifications/{id}
```

返回 `200 OK`：

```json
{
  "id": "b3f86dd9-b06d-4b63-9d7f-bcdfd7d27628",
  "url": "https://vendor.example/hooks",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "event": "registered",
    "user_id": "u_123"
  },
  "status": "succeeded",
  "attempt_count": 1,
  "max_attempts": 5,
  "created_at": "2026-06-28T20:19:07.419052Z",
  "updated_at": "2026-06-28T20:19:08.302045Z"
}
```

失败或等待重试时，响应可能包含：

- `last_error`：最近一次错误。
- `next_attempt_at`：下一次重试时间。

### 查询尝试历史：`GET /notifications/{id}/attempts`

```bash
curl http://localhost:8080/notifications/{id}/attempts
```

返回 `200 OK`：

```json
{
  "attempts": [
    {
      "id": "fb61bcb9-28b0-44b8-9c97-8234ce731222",
      "notification_id": "b3f86dd9-b06d-4b63-9d7f-bcdfd7d27628",
      "attempt_number": 1,
      "status": "succeeded",
      "http_status": 200,
      "duration_ms": 833,
      "created_at": "2026-06-28T20:19:08.303355Z"
    }
  ]
}
```

失败尝试会包含 `error_message`，例如：

```json
{
  "attempt_number": 1,
  "status": "failed",
  "http_status": 500,
  "error_message": "unexpected HTTP status 500",
  "duration_ms": 194
}
```

### 人工补偿：`POST /notifications/{id}/retry`

```bash
curl -X POST http://localhost:8080/notifications/{id}/retry
```

仅允许对 `failed` 任务重新入队。成功时返回 `202 Accepted`，任务状态重置为 `pending`，`attempt_count` 重置为 `0`，并写入新的 outbox 事件。

如果任务不存在，返回 `404`。如果任务不是 `failed`，返回 `409`。

### 健康检查

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

`/healthz` 只表示进程可响应；`/readyz` 会检查 PostgreSQL 和 Redis，适合作为部署平台的 readiness probe。

## Makefile 命令

项目提供 `Makefile` 作为本地开发、构建和验证的统一入口：

| 命令 | 说明 |
| --- | --- |
| `make help` | 查看所有可用命令。 |
| `make deps` | 下载 Go 依赖。 |
| `make fmt` / `make fmt-check` | 格式化或检查 Go 代码格式。 |
| `make test` | 运行单元测试。 |
| `make vet` | 运行 `go vet`。 |
| `make build` | 构建 `api`、`relay`、`worker` 到 `./bin`。 |
| `make check` | 执行格式检查、`go vet`、单元测试和本地构建。 |
| `make up` | 使用 Docker Compose 构建并启动完整服务栈。 |
| `make health` | 检查 `/healthz` 和 `/readyz`。 |
| `make ps` / `make logs` | 查看 Compose 服务状态或 app 服务日志。 |
| `make e2e-success` | 创建一条通知并验证成功投递。 |
| `make e2e-failure` | 验证失败投递、最大尝试次数和人工 retry。 |
| `make ssrf-test` | 验证 SSRF/内网地址拦截。 |
| `make inspect-state` | 打印 PostgreSQL 状态统计和 Redis consumer group 状态。 |
| `make compose-test` | 从干净卷重建并运行完整 E2E 测试，最后自动清理容器和卷。 |
| `make down` / `make down-volumes` | 停止服务；或停止并删除数据卷。 |

`make compose-test` 需要 Docker、Docker Compose、`curl`、`jq`，并会访问 `https://httpbin.org` 验证真实外部 HTTP 投递。

## 本地运行

```bash
make up
```

健康检查：

```bash
make health
```

不使用 Docker 时，需要本地 PostgreSQL 和 Redis，并在三个终端分别启动 API、relay、worker：

```bash
export RC_NOTIFY_DATABASE_URL='postgres://notify:notify@localhost:5432/notify?sslmode=disable'
export RC_NOTIFY_REDIS_ADDR='localhost:6379'

make run-api
make run-relay
make run-worker
```

## 测试

```bash
make test
make check
make compose-test
```

其中 `make compose-test` 会执行完整链路验证：重新构建并启动 Compose 服务、等待 API ready、验证成功投递、验证失败和人工 retry、验证 SSRF 拦截、断言 PostgreSQL/Redis 状态，最后执行 `docker compose down -v` 清理测试环境。

当前测试覆盖：

- 请求校验和 Gin 路由行为
- 重试策略
- worker 投递状态流转
- outbox relay 发布/失败处理
- SSRF/内网地址拦截
- Redis Stream 消息编码/解析
- 环境配置默认值

## 未来演进

- PostgreSQL 使用托管 HA、主从或 Patroni，连接池和迁移流程独立化。
- Redis 使用 Sentinel/Cluster 或托管 Redis，开启 AOF，并监控 pending length。
- 实现 PostgreSQL 对账器，扫描滞留在非终态的通知并重新入队，关闭 `published` 到被消费之间的 Redis 丢失窗口。
- 按供应商拆分 stream 或 consumer group，避免单个供应商故障拖慢全局。
- 增加供应商级限流、熔断和优先级。
- 增加 metrics、trace、告警和失败任务管理页面。
- 当需要长期事件保留、多消费者订阅或高吞吐事件平台时，引入 Kafka。
