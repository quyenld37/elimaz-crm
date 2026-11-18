# ADR-05 — Realtime bằng SSE

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); phần phát lại theo `Last-Event-ID` từ `realtime_events` thay bởi ADR-17; nhịp và interface bổ sung bởi ADR-42
Ngày: 11/11/2026
Kiểm bằng: `TestReconnectResyncsClients` (internal/platform/realtime/reconnect_test.go); `TestStreamHeaders` (internal/platform/realtime/sse_test.go); `TestNotifyWakesWorker` (internal/platform/queue/worker_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-05. Tệp này không chép lại quyết định.

Quyết định còn hiệu lực: Server-Sent Events tới trình duyệt; giữa các tiến trình API dùng `LISTEN/NOTIFY` của
PostgreSQL. Phần phát lại bị bỏ (ADR-17).

## Kiểm ở đâu

- Luồng SSE và header: `TestStreamHeaders`, `TestStreamResyncPublishRollback` (internal/platform/realtime/sse_test.go).
- `LISTEN` nối lại thì phát `resync`: `TestReconnectResyncsClients` (internal/platform/realtime/reconnect_test.go).
- `NOTIFY` đánh thức worker: `TestNotifyWakesWorker` (internal/platform/queue/worker_test.go).
