# ADR-38 — Quan sát hệ thống: OpenTelemetry, Jaeger, nhịp sống gửi ra ngoài, canh mức máy

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); bổ sung ADR-19, ADR-30 (mục 2, 3); bộ đo theo Go (ADR-44)
Ngày: 15/11/2026
Kiểm bằng: `TestPipelineUnderTwoSecondsOneTrace` (tests/e2e/pipeline_test.go); `TestSetupDisabledExportsNothing` (internal/platform/telemetry/telemetry_test.go); `TestInternalErrorPersonalDataNotLeaked` (internal/httpapi/personaldata_test.go); `TestRuleHeartbeats` (internal/platform/observability/rules_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-38. Tệp này không chép lại quyết định.

OpenTelemetry gắn từ M0, dấu vết đi qua `jobs` bằng `trace_context`, lưu ở Jaeger v2; log tệp JSON theo ngày có mã dấu
vết; giữ `metrics_minute`, `error_events`; nhịp sống gửi ra ngoài; thêm một điểm kiểm không thuộc Cloudflare; canh mức
máy độc lập với database. Dữ liệu cá nhân không bao giờ nằm trong dấu vết.

## Kiểm ở đâu

- Một sự kiện đi trọn webhook → việc nền → lời gọi ra ngoài thành một dấu vết liền: `TestPipelineUnderTwoSecondsOneTrace` (tests/e2e/pipeline_test.go);
  `TestTraceFlowsThroughQueue` (internal/platform/queue/trace_test.go).
- Tắt bằng biến cấu hình: `TestSetupDisabledExportsNothing` (internal/platform/telemetry/telemetry_test.go).
- Quét dữ liệu nhạy cảm trong span: `TestInternalErrorPersonalDataNotLeaked` (internal/httpapi/personaldata_test.go), `TestHandlerErrorPersonalDataNotLeaked`
  (internal/platform/queue/personaldata_test.go). Log có `trace_id`: `TestLoggerAddsTraceID` (internal/platform/logging/redact_test.go).
- Nhịp sống quá hạn thì báo động: `TestRuleHeartbeats`, `TestRuleHeartbeatPing` (rules_test.go); `TestFlushPingsAfterWrite` (internal/platform/metrics/flusher_test.go).
- Chi phí bộ đo: `docs/do-luong/flash-sale-go.md` (laptop: chưa đạt ngưỡng 10%, đo lại trên máy thử ở Task 23 bước 7).
- Điểm kiểm ngoài Cloudflare, bộ canh mức máy (Beszel): Task 23 bước 7 (cần OK chủ shop).
