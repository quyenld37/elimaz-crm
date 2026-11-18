# ADR-16 — Tiến trình `ingest` riêng, có tệp tạm; tách gói webhook

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); bổ sung ADR-04, ADR-06
Ngày: 12/11/2026
Kiểm bằng: internal/platform/webhooks (ingest_test.go, spool_test.go, split_test.go); `TestDatabaseOutageKeepsWebhooks` (tests/e2e/dboutage_test.go); `TestRuleSpool` (internal/platform/observability/rules_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-16. Tệp này không chép lại quyết định.

Vai trò `ingest` chỉ làm: bước GET xác minh, kiểm chữ ký, ghi `webhook_deliveries`, trả 200; database lỗi thì ghi tệp
tạm có `fsync` rồi nạp lại theo thứ tự. Worker tách mỗi gói thành từng sự kiện có chống trùng, thứ tự, thử lại riêng.
Webhook của hãng chỉ là tín hiệu.

## Kiểm ở đâu

- Nhận, kiểm, ghi, trả 200; chỉ phục vụ `/hooks/*` và `/health`: `TestPostValidStoresRawBodyAndOneSplitJob`, `TestChallengeGET`,
  `TestRejects`, `TestIngestServesOnlyHooksAndHealth` (internal/platform/webhooks/ingest_test.go).
- Tệp tạm khi database lỗi: `TestDatabaseDownSpoolsAndHealthCounts` (ingest_test.go); `TestSpoolWriteThenDrainKeepsOrderAndReceivedAt`
  (internal/platform/webhooks/spool_test.go); `TestDatabaseOutageKeepsWebhooks` (tests/e2e/dboutage_test.go).
- Tách gói, chống trùng, loại lạ đếm bỏ qua: `TestSplitRoutesEvents`, `TestSplitDedupesAcrossPackets`, `TestSplitOneFailureDoesNotBlockPacket` (internal/platform/webhooks/split_test.go).
- Cảnh báo còn tệp tạm: `TestRuleSpool` (internal/platform/observability/rules_test.go).
- Webhook hãng chỉ là tín hiệu: `TestRefresh_ForgedSignal` (internal/modules/fulfillment/refresh_status_test.go).
