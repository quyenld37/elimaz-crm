# ADR-04 — Hàng đợi trong PostgreSQL

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); phần cơ chế (`webhook_inbox`, `outbox`, pg-boss) thay bởi ADR-15 và ADR-16; bổ sung bởi ADR-42
Ngày: 11/11/2026
Kiểm bằng: `TestEnqueueRolledBackLeavesNoRow` (internal/platform/queue/enqueue_test.go); `TestEmitRolledBackLeavesNothing` (internal/platform/events/emit_test.go); phần cơ chế kiểm ở ADR-15, ADR-16

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-04. Tệp này không chép lại quyết định.

Quyết định còn hiệu lực: hàng đợi nằm trong PostgreSQL, việc ghi việc nền nằm trong cùng transaction với thay đổi
nghiệp vụ. Cơ chế cụ thể (bảng `jobs`, tiến trình `ingest`) ở ADR-15, ADR-16; Valkey chỉ dùng cho việc khác (ADR-39).

## Kiểm ở đâu

- Ghi việc cùng transaction, lùi thì không còn dòng: `TestEnqueueRolledBackLeavesNoRow`
  (internal/platform/queue/enqueue_test.go), `TestEmitRolledBackLeavesNothing` (internal/platform/events/emit_test.go).
- Độ tuổi hàng đợi được canh: `TestRuleQueueLag` (internal/platform/observability/rules_test.go),
  `TestOldestPendingGauge` (internal/platform/queue/worker_test.go).
