# AI 使用说明

本作业允许使用 AI。以下是本项目中 AI 的实际使用情况。

## AI 提供的帮助

- 帮助拆解题目要求：系统边界、可靠性语义、失败处理、取舍与演进。
- 辅助比较 `PostgreSQL + Redis Streams` 和 `PostgreSQL + Kafka` 两种方案。
- 辅助设计 Outbox Pattern、worker 重试、pending 消息恢复等关键流程。
- 辅助生成 Go/Gin/Ent 项目骨架、测试用例和文档初稿。

## AI 建议但未采纳的内容

- 未采用单机 SQLite 方案：虽然更简单，但难以体现高可用和多 worker 并发。
- 未采用 Kafka 作为 MVP 默认方案：Kafka 在长期事件日志和高吞吐场景更强，但会把第一版实现复杂度推高。
- 未采用 exactly-once 目标：外部 HTTP 调用没有可靠 exactly-once 边界，强行承诺会误导系统设计。
- 未引入 Kubernetes、服务网格、管理后台和复杂供应商模板系统：这些属于后续演进，不是本题 MVP 的核心。

## 人工关键决策

- 使用 Go 实现，保持服务部署简单、并发模型清晰。
- 使用 Gin 提供 HTTP API，避免在 API 层引入过重框架。
- 使用 Ent 建模 PostgreSQL 表结构和迁移，但 outbox 并发领取使用原生 SQL `FOR UPDATE SKIP LOCKED`，因为这是可靠性关键路径。
- 选择 `PostgreSQL + Redis Streams` 作为最终方案：比单机方案可靠，比 Kafka 更克制，符合题目对工程判断和复杂度管理的要求。
- 明确采用至少一次投递，并通过 `X-Notification-ID` / `Idempotency-Key` 暴露幂等线索。
