# ADR-41 — Khóa chống trùng cho mọi thao tác ghi

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); câu trả lời "thử lại" và xóa dòng khóa bổ sung bởi ADR-46; ngoại lệ có tên ở ADR-47
Ngày: 15/11/2026
Kiểm bằng: internal/httpapi/idempotency_test.go; internal/platform/idempotency (idempotency_test.go, property_test.go); `TestRoutesIdempotencyModes` (tests/arch/routes_test.go); `TestRetryFiftyParallelOneResult` (tests/e2e/idempotency_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-41. Tệp này không chép lại quyết định.

Mọi yêu cầu ghi mang `Idempotency-Key`; kết quả và dấu "đã xong" ghi cùng transaction với thay đổi nghiệp vụ. Cùng khóa
đã xong → trả lại kết quả; đang chạy → 409 (quá 60 giây thì nhận lại); khác nội dung → 422; thiếu khóa → 400. Chỉ lưu
kết quả cuối; khóa có phạm vi, giữ 72 giờ, bảng `idempotency_keys` có RLS.

## Kiểm ở đâu

- 50 lần song song cùng khóa: `TestIdempotencyFiftyParallelSameKey` (internal/httpapi/idempotency_test.go); `TestRetryFiftyParallelOneResult` (tests/e2e/idempotency_test.go);
  `TestOrderAPI_SameKey50` (internal/httpapi/handlers_order_test.go); M2: `TestOrderAPI_CancelSameKey50` (handlers_order_cancel_test.go),
  khóa nghiệp vụ của đơn sau khi bản ghi chống trùng hết hạn `TestOrderAPI_BusinessKeyAfterExpiry` (handlers_order_m2_test.go),
  `TestCreate_BusinessKeyReplay` (internal/modules/order/create_m2_test.go).
- Tiến trình chết sau bước ghi nhận khóa: `TestIdempotencyDeadProcessReclaimedAfterLock` (idempotency_test.go); `TestAcquireReclaimsStaleRunning` (internal/platform/idempotency/idempotency_test.go).
- "Thử lại" không được phát lại: `TestIdempotencyRetryAnswerNotStored`, `TestIdempotencyRetryLaterNotStored` (internal/httpapi/idempotency_test.go).
- Cùng khóa khác nội dung → 422: `TestIdempotencySameKeyDifferentBody422`; thiếu khóa → 400: `TestIdempotencyHeaderRules` (internal/httpapi/idempotency_test.go).
- Phạm vi khóa: `TestIdempotencyTwoUsersIndependent` (httpapi), `TestScopesAreIndependent` (internal/platform/idempotency); quy luật: `TestPropertyCounterMatchesFinalKeys`.
- Liệt kê route ghi: `TestRoutesIdempotencyModes` (tests/arch/routes_test.go); `TestRealContractWritesRequired` (tools/routegen/main_test.go).
- Màn hình giữ khóa khi tự thử lại: console/src/app/idempotency.test.ts.
