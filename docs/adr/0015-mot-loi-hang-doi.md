# ADR-15 — Một lõi hàng đợi; tách sự kiện nội bộ khỏi lệnh ra ngoài

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); thay phần cơ chế của ADR-04; bổ sung bởi ADR-38, 42, 46
Ngày: 12/11/2026
Kiểm bằng: internal/platform/queue (worker_test.go, ordering_test.go, shutdown_test.go, property_test.go); internal/platform/commands (runner_test.go, property_test.go); `TestWorkerCrashDoesNotSendTwice` (tests/e2e/workercrash_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-15. Tệp này không chép lại quyết định.

Một bảng `jobs` và một đoạn code nhận-việc/thử-lại cho mọi việc nền: sự kiện có thứ tự theo `order_key` + `order_seq`
và dấu xong cùng transaction; lệnh ra ngoài có `in_doubt`; việc định kỳ từ bảng `schedules`. Bỏ pg-boss.

## Kiểm ở đâu

- Nhận việc đồng thời, mất giữ chỗ: `TestTwoWorkersHandleEachJobOnce`, `TestLostLeaseRollsBack`, `TestExpiredLeaseRetry` (internal/platform/queue/worker_test.go).
- Thứ tự, `dead` chặn việc sau: `TestOrderingBySeq`, `TestStrictDeadBlocksAndAlerts` (internal/platform/queue/ordering_test.go);
  `TestEventsRunInSeqOrder`, `TestDeadEventBlocksLaterEvents` (internal/platform/events/emit_test.go).
- Tắt êm: `TestStopWaitsThenStopsClaiming`, `TestStopReturnsUnfinishedJob` (internal/platform/queue/shutdown_test.go).
- Quy luật hàng đợi (rapid): `TestPropertyQueue` (internal/platform/queue/property_test.go).
- Lệnh ra ngoài, `in_doubt`, không gửi hai lần: `TestTimeoutAfterSendInDoubtNeverResent` (internal/platform/commands/runner_test.go);
  `TestPropertyNeverSentTwice` (internal/platform/commands/property_test.go); `TestWorkerCrashDoesNotSendTwice` (tests/e2e/workercrash_test.go).
- Việc định kỳ: `TestTickPerTenant`, `TestTwoTickersNoDuplicate` (internal/platform/schedules/schedules_test.go).
- `trace_context` (ADR-38): `TestTraceFlowsThroughQueue` (internal/platform/queue/trace_test.go).
