# ADR-17 — Realtime chỉ báo hiệu; màn hình tự tải lại

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); thay phần phát lại của ADR-05; nhịp và interface `RealtimeBus` bổ sung bởi ADR-42
Ngày: 12/11/2026
Kiểm bằng: internal/platform/realtime (sse_test.go, reconnect_test.go, silent_test.go); console/src/realtime/client.test.ts; console/src/shell/Banner.test.tsx

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-17. Tệp này không chép lại quyết định.

SSE chỉ gửi "đối tượng X vừa đổi" và `resync`; dữ liệu luôn lấy bằng API thường. Mỗi lần nối bắt đầu bằng `resync`;
`api` tự nối lại `LISTEN` và phát `resync`. Màn hình tự canh mất kết nối và hiện băng báo. Bỏ bảng `realtime_events`.

## Kiểm ở đâu

- Báo hiệu, `resync`, gộp báo hiệu: `TestStreamResyncPublishRollback`, `TestStreamBatchDedupe`, `TestStreamTenantIsolation` (internal/platform/realtime/sse_test.go).
- Nối lại `LISTEN` phát `resync`: `TestReconnectResyncsClients` (internal/platform/realtime/reconnect_test.go).
- Kết nối chết im lặng: `TestSilentDeadConnectionDetected` (internal/platform/realtime/silent_test.go).
- Phía màn hình (nối lại, băng "mất kết nối"): console/src/realtime/client.test.ts, console/src/shell/Banner.test.tsx.
- API "tải phần mới" cho màn danh sách: theo từng màn ở M1–M4 (ví dụ `TestListsAPI`, internal/httpapi/handlers_lists_test.go).
