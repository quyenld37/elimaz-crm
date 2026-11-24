# Task 455 — customer: góp ý của khách

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 455 / M5-43, làn L1, sau M5-42).
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu có sẵn. Thêm ở `customer`:
  `FeedbackInput`, `Feedback`, `FeedbackFilter`, `Writer.SubmitFeedback`, `Writer.UpdateFeedback`, `ListFeedback`,
  `FeedbackTopics`, hằng trạng thái `FeedbackNew/Seen/Answered`. `identity.Permissions` thêm 2 dòng (`customer.feedback.read`,
  `customer.feedback.manage`, ScopeShop), `identity.ServiceKeyPermissions` thêm `web.feedback.write` (9 chỗ dùng
  ServiceKeyPermissions ngoài test: chỉ đọc danh sách, không đổi kiểu). Mã sinh: thêm 3 operation trong `gen`.
  Hàm riêng (không xuất) đổi cấu trúc, không đổi hành vi: `customer.List` dùng `ListCursor.params` + `pageOf` mới
  (search.go); `validNote` gọi `validMultiline` mới (notes.go). `identity/roles.go` thêm nhãn của 2 quyền mới.
- Gói phụ thuộc ngược (go list): `customer` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`,
  `order`, `promotion`, `testkit/shiptest`; chỉ thêm ký hiệu nên chỉ build lại, không đổi hành vi của chúng.
  `identity` (permissions, roles, servicekeys) nhập bởi hầu hết gói; danh sách quyền thêm dòng → vai trò mặc định owner,
  manager tự nhận (SyncDefaultRoles), test `TestPermissionsList`, `TestServiceKeyPermissions` sửa theo.
- Operation OpenAPI bị chạm, oasdiff: thêm `webSubmitFeedback` (POST /api/v1/web/feedback, x-auth service_key,
  `web.feedback.write`, IdempotencyKey required → 201), `listFeedback` (GET /api/v1/feedback,
  `customer.feedback.read`), `updateFeedback` (PATCH /api/v1/feedback/{id}, `customer.feedback.manage`, IdempotencyKey).
  Chỉ thêm → oasdiff không có thay đổi phá vỡ. Không thêm mã lỗi mới (dùng invalid_input, not_found, version_conflict).
- Chỗ dùng trên màn hình: chưa có màn (Task 461 làm màn Góp ý). Sinh lại `console/src/api/**` (make gen). Màn Khóa dịch
  vụ (`console/src/screens/service-keys/serviceKeyRules.ts`) thêm quyền `web.feedback.write` để chủ shop cấp được.
- Câu sqlc, bảng, pha migration: migration `0523_customer_feedback.sql` (pha mở rộng: CREATE TABLE, index, RLS, GRANT;
  nhóm khóa 5 ADR-46, đã có sẵn tên `customer.feedback` trong ADR). Câu mới `queries/customer/feedback.sql`:
  `InsertFeedback`, `UpdateFeedback`, `FeedbackExists`, `ListFeedback`. Không sửa câu có sẵn (dùng lại `GetIdentityOwner`,
  `GetRootOf`).
- Test sẽ chạy: `go test -race -run 'Feedback' ./internal/modules/customer/`, `./internal/httpapi/ -run Feedback`,
  `./internal/httpapi/web/ -run Feedback`, `./internal/modules/identity/ -run 'TestPermissionsList|TestServiceKeyPermissions'`,
  `go test ./tests/arch/`, console `vitest` của service-keys và `errors.test.ts`; cả gói ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (cursor, audit, chuẩn hóa
  SĐT, kiểu tự che); tìm `Cursor struct` (customer.ListCursor, TimelineCursor, order.ReturnCursor, files.LostCursor),
  `identityOwner`, `NormalizePhone`, `fieldsAudit`, `auditEntity`, `validText`, `logging.Secret`, `refusableWrite`,
  `catalogRead`, `clampLimit`.
- Dùng lại / viết mới vì: dùng lại `customer.ListCursor` + `ParseListCursor` (cùng khóa sắp xếp created_at, id giảm
  dần), `NormalizePhone`, `identityOwner` (trả cả gốc), `RootOf`, `auditEntity`/`fieldsAudit`, `clampLimit`,
  `logging.Secret` cho nội dung, tên, liên lạc, ghi chú; phía HTTP dùng `refusableWrite`, `catalogRead`,
  `isCustomerRefusal`, `staffNames`. Viết mới chỉ phần riêng của góp ý (kiểm trường, câu SQL, handler mỏng).

## Soát thiết kế

5.1 Chủ dữ liệu: `customer` (kế hoạch mục Xếp module 21/11: góp ý thuộc customer); bảng ở schema `customer`.
5.2 Không ghi bảng module khác: chỉ ghi `customer.feedback`; audit qua `identity.RecordAudit` (public API).
5.3 Đọc module khác qua view: không đọc module khác (khách là của chính customer).
5.4 Nguyên tử: chèn góp ý + audit trong một transaction (refusableWrite).
5.5 Việc sau commit: không có (không sự kiện, không báo hiệu; màn Task 461 tự tải).
5.6 Gọi ra ngoài: không áp dụng.
5.7 Ngữ cảnh shop: bảng có tenant_id, RLS (`platform.enable_tenant_rls`), UNIQUE (tenant_id, id), FK ghép
(tenant_id, customer_id) → customers; mọi câu chạy trong WithTenant.
5.8 Module không biết HTTP: customer không nhập net/http, gen.
5.9 Đường ghi có khóa chống trùng và quyền: hai đường ghi khai `x-permission` và `IdempotencyKey` (required).
5.10 Valkey: chỉ giới hạn tần suất theo khóa (serviceRateLimit có sẵn cho mọi route service_key); không quyết định nghiệp vụ.
5.11 Không thêm thư viện.
5.12 Theo mẫu: module customer (Writer, sentinel), đường ghi Task 12, web như Task 468 (`handlers_web_birthdays.go`).
6.1 Hai người sửa cùng góp ý: cột `version`, `UPDATE … WHERE version = $v` (0 dòng → đọc tồn tại → 409/404).
6.2 Gửi lại: Idempotency-Key do website sinh khi mở form; middleware lưu kết quả cùng tx. Không có khóa tự nhiên
unique ở bảng (khách gửi hai góp ý giống nhau là hợp lệ); import M5-55 dùng (created_at, client_hash) có index.
6.3 Thứ tự khóa: Submit chỉ đọc danh tính (không khóa) rồi chèn feedback (nhóm 5, sau customers); Update chỉ khóa một dòng
feedback. Không khóa nhiều bảng → không deadlock mới.
6.4 Lỗi tạm: refusableWrite/RetryTx có sẵn; thân hàm gán lại kết quả mỗi lần.
6.5 Cách ly: READ COMMITTED + UPDATE có điều kiện.
6.6–6.8 Việc nền, 429 bên ngoài, tiến trình chết: không áp dụng (không việc nền, không gọi ra ngoài).
6.9 Treo lâu: hạn mặc định của db.TxAPI.
6.10 Tiền: không áp dụng.
6.11 Quy luật: không thuộc vùng rủi ro cao; test ca biên (độ dài, SĐT lạ, email, khách đã gộp).
6.12 Dữ liệu cá nhân: body, name, contact, note là `logging.Secret` trong Go; audit chỉ tên trường (+ chủ đề, trạng thái
không cá nhân); test quét audit và log không chứa nội dung, SĐT.
6.13 Quyền: `customer.feedback.read`/`.manage` ScopeShop (owner, manager mặc định); `web.feedback.write` chỉ khóa dịch vụ.

## Quyết định thay chủ shop

- **Ai có quyền góp ý:** `customer.feedback.read`/`.manage` chỉ vai trò mẫu Chủ, Quản lý (như admin website hiện chỉ chủ
  shop xem). Bán hàng không có sẵn. Khác: cho Bán hàng xem. Đảo ngược: chủ shop tự thêm quyền vào vai trò ở màn Vai trò.
- **Giới hạn trường kế hoạch không ghi:** mã đơn ≤ 60, băm địa chỉ ≤ 128 ký tự (website dùng mã W… và băm hex); mood không
  bắt buộc (form website cho bỏ trống). Nới bằng migration mở rộng nếu cần.
- **Khóa dịch vụ:** thêm quyền riêng `web.feedback.write` (theo kế hoạch), mặc định tích khi tạo khóa mới; nhãn của
  `web.requests.write` bỏ chữ "góp ý" (màn Khóa dịch vụ, docs/quyen.md) để không nhầm. Khóa đang dùng phải được chủ shop
  cấp thêm quyền này trước khi website (Task 465) gửi góp ý qua CRM.
- **Lỗi trường sai:** sai theo hợp đồng (quá 2000 ký tự, chủ đề lạ, điểm 6) do ogen chặn trước handler → 400
  `bad_request`; sai mà hợp đồng không bắt được (chỉ khoảng trắng, ký tự điều khiển) → 400 `invalid_input`. Cả hai là 400
  như kế hoạch; không thêm mã lỗi mới.
- **Audit:** `customer.feedback.submit` ghi tên trường có giá trị và chủ đề; `customer.feedback.update` ghi trạng thái mới
  và `note: true` (không nội dung). Chủ đề, trạng thái không phải dữ liệu cá nhân.
- **Không báo hiệu realtime** khi có góp ý mới (kế hoạch không yêu cầu; màn Task 461 tự tải). Thêm sau nếu màn cần.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| SĐT của khách có sẵn → gắn; đọc trả gốc (cả khi đã gộp); hoàn tác gộp tách đúng; SĐT lạ không gắn, không tạo khách, không thêm danh tính; email không gắn; audit không chứa nội dung, SĐT | `TestSubmitFeedback_LinksByPhone` | `go test -race -count=1 -run Feedback ./internal/modules/customer/` | `ok … customer 2.942s` |
| 2001 ký tự, chủ đề lạ, điểm ngoài 1–5, trường quá dài, ký tự điều khiển → ErrInvalidInput, không lưu | `TestSubmitFeedback_Validation` | như trên | ok |
| version cũ → ErrVersionConflict; lạ, tenant khác → ErrNotFound; không trường nào → ErrInvalidInput; ghi chú "" xóa; audit không chứa ghi chú | `TestUpdateFeedback_VersionAndAudit` | như trên | ok |
| Lọc theo khách (gốc hay hồ sơ con) trả góp ý của nhóm; lọc trạng thái, chủ đề; trang theo con trỏ; tenant khác không thấy | `TestListFeedback_FiltersAndPaging` | như trên | ok |
| Web: khóa thiếu quyền 403; thiếu Idempotency-Key 400; 201 gắn khách; gửi lại cùng khóa → cùng mã, một dòng; SĐT lạ không tạo khách; log + audit không chứa nội dung, SĐT, tên | `TestWebFeedback_Submit` | `go test -race -count=1 -run '^TestWebFeedback_' ./internal/httpapi/web/` | `ok … httpapi/web 2.165s` |
| Web: 2001 ký tự → 400 bad_request, chỉ khoảng trắng → 400 invalid_input, không lưu; 2000 ký tự → 201 | `TestWebFeedback_Invalid` | như trên | ok |
| Nhân viên: không quyền 403; đọc có nội dung nguyên chữ (`<script>`), khách gắn; lọc khách, trạng thái; con trỏ hỏng 400; sửa → version 2 + tên người sửa; version cũ 409; không trường 400; lạ 404; tenant khác 404 / rỗng | `TestFeedbackAPI_ListUpdate` | `go test -race -count=1 -run '^TestFeedbackAPI_ListUpdate$' ./internal/httpapi/` | `ok … httpapi 2.402s` |
| Quyền mới có trong danh sách, ScopeShop; khóa dịch vụ có `web.feedback.write` | `TestPermissionsList`, `TestServiceKeyPermissions` | `go test -count=1 -run 'TestPermission\|TestServiceKey' ./internal/modules/identity/` | ok |
| Màn Khóa dịch vụ: 10 quyền, mặc định tích 9 | `ServiceKeys.test.tsx`, `serviceKeyRules.test.ts` | `cd console && npx vitest run src/screens/service-keys` | `17 passed` |
| Cổng kiểm của task (cổng tĩnh + test gói đổi và phụ thuộc) | ci-task | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 115f282…)` |

## E2E viết sẵn, chưa chạy

(không có — kế hoạch Task 455 không có e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Handler website đặt ở `internal/httpapi/handlers_web_feedback.go` (package httpapi) thay vì `internal/httpapi/web/feedback.go`:
  đường ghi cần `refusableWrite` (chống trùng, transaction) của package httpapi; theo đúng mẫu Task 468
  (`handlers_web_birthdays.go`). Test web vẫn ở `internal/httpapi/web/feedback_test.go` như kế hoạch.
- `customer_id` lưu **hồ sơ giữ danh tính SĐT** (kế hoạch ghi "gắn customer_id (gốc)"); đọc luôn trả gốc hiện tại và lọc
  theo cả nhóm. Lý do: lưu gốc lúc gửi thì hoàn tác gộp để góp ý theo SĐT của hồ sơ con kẹt ở gốc cũ; cách này giống ghi
  chú khách (05 mục 1, B5). Hành vi kế hoạch đòi ("gắn đúng khách gốc cả khi đã gộp", "lọc theo khách trả góp ý của
  gốc") giữ nguyên ở API (`TestSubmitFeedback_LinksByPhone`, `TestListFeedback_FiltersAndPaging`).
- `UpdateFeedback` trả `(Feedback, error)` (kế hoạch không ghi kiểu trả) để PATCH trả góp ý sau khi sửa.
- Tái dùng: tách `ListCursor.params` và `pageOf` (search.go) dùng chung cho `List` và `ListFeedback` (jscpd báo lặp); tách
  `validMultiline` dùng chung cho ghi chú khách và góp ý. Không đổi hành vi của `List`, `AddNote`.

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lần 1, trên phần đã stage sau ci-task xanh). Không có phát hiện phải sửa. Reviewer kiểm riêng RLS/tenant
(bảng, câu chèn, ca tenant khác ở ba lớp), version 409 (UPDATE có điều kiện + FeedbackExists), idempotency (required,
gửi lại cùng khóa một dòng), dữ liệu cá nhân (audit chỉ tên trường, kiểu tự che, test quét audit và log), khớp khách theo
SĐT (identityOwner, không tạo khách), lọc theo nhóm; chấp nhận hai mục "Khác kế hoạch" (lưu hồ sơ giữ SĐT; vị trí handler
web). Ghi chú không bắt buộc (không sửa): mô tả `webSubmitFeedback` trong openapi ghi "trường sai → 400 invalid_input",
thực tế sai theo hợp đồng trả 400 `bad_request` (cùng mã trạng thái); trường tùy chọn chỉ có khoảng trắng coi như không gửi.
