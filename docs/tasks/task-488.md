# Task 488 — Bổ sung dữ liệu theo mockup: banner, bộ phối, hãng trên web, nhật ký (API + màn)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

Phạm vi (prompt điều phối 24/11, không có mục riêng trong kế hoạch; đối chiếu Việc phát hiện thêm và Quyết định thay
chủ shop của task-459, 460, 208, 462 và mockup `docs/mockups/m5/`): thêm API thiếu rồi cập nhật bốn màn.

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn.
  `files.FileStatus` thêm `Width`, `Height`, `RejectReason` (dựng một chỗ `files.Status`; dùng ở content, catalog,
  customer, order — chỉ đọc `Status`/`Deleted`, không đổi). `content.LookRefData` thêm `Colors`; mới `content.LookColor`,
  `content.LookSkip`, `content.PreviewLooks` (kiểm cấp danh sách rồi tách bộ chưa đủ). `fulfillment.WebCarrierSettings`
  thêm `UpdatedBy`, `UpdatedAt`; `fulfillment.WebCarrierStat` thêm `ShowOnWeb`; `WebCarrierStats` trả cả tài khoản đang
  ẩn (bên gọi duy nhất `handlers_web_carriers.go` tách hai danh sách); mới `(*Service).ReorderWebCarriers`.
  `identity.AuditLog` thêm `EntityName` (service_key do identity tự đọc); mới `identity.ListAuditActors`,
  `identity.AuditActor`. httpapi: `stampOut` (2 chỗ gọi: banner, bộ phối) thay bằng `stampInto` (3 chỗ: thêm hãng trên
  web).
- Gói phụ thuộc ngược (go list): `content` ← `httpapi`, `httpapi/web`, `wiring/webcache`, `cmd/crm`; `platform/files` ←
  content, customer, cash, catalog, order, fulfillment, `queries/webcatalog`, `httpapi`, `cmd/crm`, `filestest`;
  `fulfillment`, `identity` ← gần như mọi gói (chỉ thêm, không gói nào phải sửa).
- Operation OpenAPI bị chạm, oasdiff: `getBanners`/`saveBanners` (`HomeBannerList.updatedByName`,
  `HomeBannerImage.width/height/rejectReason`); `getLooks`/`saveLooks` (`LookList.updatedByName`,
  `LookProductRef.colors`); `previewLooks` (`LookPreview.skipped`; bộ chưa đủ không còn làm 422 cả danh sách — lỗi cấp
  danh sách vẫn 422); `getWebCarrierSettings`/`saveWebCarrierSettings` (`updatedBy`, `updatedAt`, `updatedByName`,
  `hiddenCarriers`); mới `reorderWebCarriers` (`PUT /api/v1/web-carrier-settings/order`,
  fulfillment.carrier_accounts.manage, Idempotency-Key, `accountIds` uniqueItems → 400 bad_request); `listAuditLogs` (`AuditLog.entityName`); mới
  `listAuditActors` (`GET /api/v1/identity/audit-actors`, audit.read). Chỉ thêm → oasdiff không báo phá vỡ.
- Chỗ dùng trên màn hình: `console/src/screens/web-banners/` (BannerEditor 409 có tên, giờ; BannerImage kích thước, lý
  do từ chối, tự tải lại khi tệp đổi trạng thái qua báo hiệu `files.file`), `web-looks/` (màu từ `colors` thay
  `getProduct`, xem trước gửi cả danh sách và hiện lý do bộ bị bỏ qua, 409 có tên), `web-carriers/` (số 30 ngày cho
  dòng ẩn, đổi thứ tự một lần gọi, 409 có tên, giờ), `audit/` (ô lọc người từ API, cột Mục dùng `entityName`, nhãn
  thiếu, `content.looks` toàn shop).
- Câu sqlc, bảng, pha migration: không migration (cột đã có: `channel_carrier_settings.updated_by/updated_at`,
  `platform.files.width/height/reject_reason`). `queries/content/looks.sql` mới `LookProductColors`
  (`catalog.variants_v`); `queries/fulfillment/channel_settings.sql` `WebCarrierStats` thêm `show_on_web`, bỏ lọc
  show_on_web; `GetWebCarrierSettings`/`LockWebCarrierSettings`/`UpdateWebCarrierSettings` trả thêm updated_by,
  updated_at; mới `LockWebCarriersForOrder`, `SetWebSort`; `queries/identity/audit_list.sql` mới `AuditActors` (tên khóa dịch vụ là
  thực thể dùng lại câu `AuditActorNames`).
- Test sẽ chạy: `go test -race -run` đúng test mới/sửa trong `internal/platform/files`, `internal/modules/content`,
  `internal/modules/fulfillment`, `internal/modules/identity`, `internal/httpapi`; `tests/arch`; vitest bốn thư mục màn
  + `src/app/errors.test.ts`; ci-task cho gói đổi + phụ thuộc (files, identity, fulfillment/... vùng rủi ro cao → bộ
  vùng do ci-task chạy). Không e2e/Playwright.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `staffNames` (httpapi, tên nhân viên theo mã — dùng cho
  `updatedByName` như `webIntakeOut`, `feedback`); `stampOut` (banner/bộ phối); `refusableWrite`, `catalogRead`;
  `identity.AuditActorNames` (mẫu cho câu đọc tên khóa); `fulfillment.lockWebSettings`/`writeWebSettings` (đổi thứ tự
  dùng lại để tăng version, phát sự kiện, báo hiệu); `catalog.variants_v` (màu); console: `useWriteAction`,
  `useRealtime`/báo hiệu có sẵn, `shortTime`, `ApiError`.
- Dùng lại / viết mới vì: tên mục nhật ký đọc qua public API sẵn có của từng module (`order.GetRequest`,
  `inventory.GetDocument`, `fulfillment.GetAccount`) trong lớp HTTP, vì identity không được đọc module khác; không có
  hàm "tên thực thể" chung để dùng lại.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: ảnh (platform/files), banner/bộ phối (content), hãng trên web (fulfillment), nhật ký (identity) —
  mỗi phần thêm ở module sở hữu; không bảng mới.
- 5.2 Không ghi bảng module khác: đổi thứ tự chỉ ghi `fulfillment.carrier_accounts`, `channel_carrier_settings`.
- 5.3 Màu đọc `catalog.variants_v` (đã công bố); tên mục nhật ký đọc qua public API của order, inventory, fulfillment ở
  lớp HTTP; tên nhân viên qua `identity.GetStaff` (staffNames).
- 5.4 Đổi thứ tự: một transaction (khóa tài khoản, ghi web_sort, tăng version cài đặt, sự kiện, báo hiệu, audit).
- 5.5 Sự kiện `carrier.web_settings_changed` qua `events.Emit` trong tx (consumer webcache có sẵn làm mới thứ tự).
  5.6 không áp dụng: không gọi ra ngoài.
- 5.7 Mọi câu trong `tenancy.WithTenant` (`refusableWrite`, `catalogRead`). Không bảng mới.
- 5.8 Module không nhập HTTP; handler mỏng. 5.9 Đường ghi mới `reorderWebCarriers` có Idempotency-Key và
  `x-permission`; đường đọc mới có `x-permission`.
- 5.10 không áp dụng: không Valkey. 5.11 Không thư viện mới. 5.12 Theo mẫu catalog/identity/fulfillment có sẵn.
- 6.1 Đổi thứ tự: khóa mọi tài khoản đang hiện trên web theo mã tăng dần rồi khóa dòng cài đặt và so version; tập tài
  khoản gửi lên khác tập đang hiện → 409 (danh sách cũ). Hai lệnh đổi thứ tự cùng lúc: lệnh sau lệch version → 409.
- 6.2 Idempotency-Key qua `refusableWrite`; lỗi nghiệp vụ (409, 422) không lưu vào khóa.
- 6.3 ADR-46 nhóm 8: carrier_accounts (mã tăng dần) trước channel_carrier_settings — đúng thứ tự của
  SaveWebCarrierSettings/UpdateAccount; test hai đường song song không deadlock (đổi thứ tự ∥ tắt hiện).
- 6.4 `refusableWrite` chạy trong RetryTx: gán lại mọi biến kết quả trong thân hàm. 6.5 READ COMMITTED + khóa dòng.
- 6.6–6.8 không áp dụng: không việc nền, không gọi ngoài. 6.9 Câu mới chạm vài chục dòng mỗi tenant (tài khoản hãng,
  nhân viên, khóa), màu theo `product_id = ANY` (index sẵn của variants). 6.10 không áp dụng: không tiền (phí dự phòng
  không đổi). 6.11 Quy luật đổi thứ tự (thứ tự kết quả = thứ tự gửi, tập không đổi) kiểm bằng test bảng + rapid.
- 6.12 Dữ liệu cá nhân: tên nhân viên, tên khóa, mã yêu cầu/phiếu/tên tài khoản hãng chỉ trả cho người có quyền đọc màn
  đó (audit.read xem tên mục: đã thấy mã thực thể; tên khách không trả — xem Quyết định). Không log.
- 6.13 Quyền: route mới khai `x-permission`, test 403 khi thiếu quyền.

## Quyết định thay chủ shop

- **Tên mục nhật ký đọc ở lớp HTTP qua public API từng module** (`order.GetRequest` với người bán mặc định,
  `inventory.GetDocument`, `fulfillment.GetAccount`), mỗi thực thể một lần mỗi trang (≤ 50); không tìm thấy → không có
  tên. Chỉ ba loại (yêu cầu YC…, phiếu kho, tài khoản hãng) và khóa dịch vụ (identity tự đọc). Tên khách không trả (dữ
  liệu cá nhân). Phương án khác: câu đọc tên theo lô trong từng module (thêm public API ở ba module rủi ro cao) — để
  sau nếu trang nhật ký chậm.
- **Đổi thứ tự tăng version của tài khoản đổi số** (như sửa web_sort qua updateCarrierAccount): lệnh bật/ẩn đọc version
  trước đó sẽ 409 và màn tải lại. Tập gửi lên khác tập đang hiện (có người vừa bật/ẩn, mã trùng lọt qua) → 409
  version_conflict, không đổi gì. Mã trùng bị chặn ở hợp đồng (uniqueItems → 400 bad_request của ogen).
- **Màn hãng trên web:** bật/ẩn vẫn lưu từng tài khoản (PATCH M3); chỉ đổi thứ tự dùng API mới. Bản nháp chọn sẵn/phí
  đọc ở version mà chính người đó vừa tăng khi đổi thứ tự thì gửi version mới (không tự báo 409 cho mình). Hộp 409 chỉ
  nêu người, giờ khi lỗi đến từ thứ tự hay chọn sẵn/phí (stamp của cài đặt); 409 của một tài khoản chỉ "Đã tải lại."
- **Banner, bộ phối 409:** sau 409 màn đọc lại danh sách (không thay bản đang sửa) để hộp nêu "<tên> lưu lúc <giờ>."
  như mockup; chưa đọc được thì giữ câu cũ.
- **Ảnh banner tự cập nhật:** ảnh đã lưu đang xử lý → truy vấn theo chủ đề `files.file` (máy chủ chỉ báo hiệu cho
  quyền files.upload); không có quyền đó thì vẫn thấy khi danh sách tải lại (báo hiệu content.banners, resync 20 s).
- **Xem trước bộ phối gửi mọi bộ đã chọn ảnh:** máy chủ bỏ bộ chưa đủ, trả lý do ở `skipped` (mã lỗi lưu sẽ trả); lỗi
  của cả danh sách (quá 6 bộ trang chủ…) vẫn 422 như trước. Bộ chưa chọn ảnh không gửi (hợp đồng đòi `photoFileId`
  uuid, chuỗi rỗng → 400 cả yêu cầu; reviewer lần 1): xem trước nêu lý do màn tự tính (`lookProblem`). Phương án khác
  (nới hợp đồng cho `photoFileId` rỗng) đổi hợp đồng lưu, không chọn.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trạng thái tệp có cỡ ảnh (ready) và lý do từ chối (rejected) | `TestStatus_SizeAndRejectReason` (3 ca) | `go test -race -count=1 -run '^TestStatus' ./internal/platform/files/` | ok |
| Màu bán được của mẫu; xem trước bỏ bộ chưa đủ kèm mã lỗi, lỗi cả danh sách vẫn trả | `TestLoadLookRefData_Colors`, `TestPreviewLooks_SkipsIncomplete`, `TestLookLinks_MatchWebsite` | `go test -race -count=1 -run '^(TestPreviewLooks_SkipsIncomplete|TestLoadLookRefData_Colors|TestLookLinks_MatchWebsite|TestValidateLooks.*)$' ./internal/modules/content/` | ok |
| Banner/bộ phối API: updatedByName, width/height/rejectReason, colors, skipped | `TestBannersAPI_MockupFields`, `TestLooksAPI_MockupFields`, `TestLooksAPI_SaveRulesAndRead`, `TestLooksAPI_OtherTenant` | `go test -race -count=1 -run '^(TestBannersAPI.*|TestLooksAPI.*)$' ./internal/httpapi/` | ok |
| Đổi thứ tự: web_sort 1,2; version +1, người/giờ lưu; 1 sự kiện; chỉ tài khoản đổi số tăng version; version cũ, thiếu, thừa, lạ → ErrVersionConflict không đổi gì | `TestReorderWebCarriers` | `go test -race -count=1 -run '^(TestReorderWebCarriers|TestWebCarrierStats_IncludesHidden|TestReorderWebCarriers_ConcurrentHide|TestWebCarrier.*)$' ./internal/modules/fulfillment/` | ok |
| Số 30 ngày cả tài khoản chưa hiện, trừ revoked | `TestWebCarrierStats_IncludesHidden` | như trên | ok |
| Đổi thứ tự ∥ tắt hiện 10 vòng: không deadlock, chỉ lỗi nghiệp vụ | `TestReorderWebCarriers_ConcurrentHide` | như trên | ok |
| API đổi thứ tự: 403 người đọc, 200 thứ tự mới + tên người lưu + audit, 409 ×3, 400 mã trùng; GET có hiddenCarriers, chưa lưu không có người/giờ (cả hai ở `TestReorderWebCarriersAPI`); lưu chọn sẵn/phí cũ không đổi (`TestWebCarrierSettingsAPI`) | `TestReorderWebCarriersAPI`, `TestWebCarrierSettingsAPI` | `go test -race -count=1 -run '^(TestReorderWebCarriersAPI|TestWebCarrierSettingsAPI)$' ./internal/httpapi/` | ok |
| Người làm cho ô lọc: cả đã khóa/thu hồi, xếp tên, không lẫn tenant | `TestListAuditActors` | `go test -race -count=1 -run '^(TestListAuditActors|TestListAuditLogs_ServiceKeyEntityName|TestListAuditLogs_ActorNamesNoValues)$' ./internal/modules/identity/` | ok |
| Tên khóa dịch vụ là thực thể; mã lạ/không uuid/loại khác → rỗng | `TestListAuditLogs_ServiceKeyEntityName` | như trên | ok |
| API nhật ký: entityName YC (dòng audit thật), mã phiếu, không còn → vắng; audit-actors 403/200 không email; tên tài khoản hãng | `TestAuditLogsAPI_EntityNamesAndActors`, `TestAuditLogsAPI_CarrierAccountName` | `go test -race -count=1 -run '^(TestAuditLogsAPI.*)$' ./internal/httpapi/` | ok |
| Màn nhật ký: ô người từ API (cả người chưa có dòng, đã khóa), lỗi API → chỉ "Mọi người"; Mục hiện tên thay mã; bộ phối toàn shop; nhãn mới | `AuditLog.test.tsx` (lọc người, mỗi dòng, chưa tải được người làm), `auditRules.test.ts` (thực thể, ô lọc người làm) | `cd console && npx vitest run src/screens/audit` | 19/19 qua |
| Màn hãng trên web: một PUT thứ tự cả danh sách kèm version + khóa; số 30 ngày cho hãng ẩn; 409 nêu người, giờ; nháp sau tự đổi thứ tự gửi version mới | `WebCarriers.test.tsx` (4 ca mới), `webCarrierRules.test.ts` (moveOrder, số vận đơn, người lưu) | `npx vitest run src/screens/web-carriers src/screens/carriers` | 90/90 qua |
| Màn bộ phối: gửi mọi bộ có ảnh, không gửi bộ chưa có ảnh (máy chủ giả trả 400 như ogen); lý do bộ chưa đủ (máy chủ bỏ qua hay màn tự tính) ở xem trước; 409 nêu tên, giờ; màu từ colors không cần catalog.read | `LookEditor.test.tsx` (4 ca mới/sửa: "bộ đã có ảnh mà máy chủ bỏ qua…", "sửa → previewLooks gửi mọi bộ đã có ảnh…", "409 → hộp nêu ai lưu…", "màu của món lấy từ danh sách…") | `npx vitest run src/screens/web-looks` | 46/46 qua |
| Hợp đồng: photoFileId rỗng → 400 cả lần xem trước (lý do màn lọc) | `TestLooksAPI_MockupFields` (ca "ảnh rỗng") | `go test -race -count=1 -run '^TestLooksAPI_MockupFields$' ./internal/httpapi/` | ok |
| Màn banner: cỡ ảnh từ danh sách (không gọi getFile), lý do từ chối; báo hiệu files.file → đọc lại, Lưu mở; 409 nêu tên, giờ | `BannerEditor.test.tsx` (3 ca mới) | `npx vitest run src/screens/web-banners` | 54/54 qua |
| Cổng kiểm của task (cổng tĩnh, test gói đổi + phụ thuộc + vùng rủi ro cao, console) | — | ci-task (log ngoài worktree `…/scratchpad/logs/ci-488.log`) | `CI-TASK XANH (so với điểm tách nhánh 104bd03)`; `ok …/internal/httpapi`, `…/modules/fulfillment`, `…/modules/identity`, `…/modules/content`, `…/platform/files`; console `Test Files 121 passed`, `Tests 1404 passed`; jscpd 0 clones |
| Cổng tĩnh tự soát | — | `make jscpd`; `golangci-lint run` 5 gói; `npx tsc --noEmit`; `npx eslint` 5 thư mục màn; `go test ./tests/arch/`; `npx vitest run src/app/errors.test.ts src/app/screens.test.tsx` | 0 clones; 0 issues; sạch; ok; 51/51 |

## E2E viết sẵn, chưa chạy

(không có: không có spec Playwright/e2e trong phạm vi; đủ ca ở test gói và vitest)

## Test bị xóa hoặc tắt

- `internal/modules/identity/auditlog_list_test.go` (`TestListAuditLogs_ActorNamesNoValues`): số câu kiểm giảm 19 → 17
  vì tạo khóa dịch vụ và ghi các dòng audit chuyển sang hàm phụ `mustWebKey`, `recordMinutely`
  (`auditactors_test.go`, để gỡ đoạn lặp jscpd); hai câu `t.Fatal` đó nằm trong hàm phụ, ca kiểm không đổi.

## Khác kế hoạch

- Bỏ câu `AuditServiceKeyNames` dự định ở mục Ảnh hưởng: tên khóa là thực thể dùng lại `AuditActorNames`.
- Test cũ `TestListAuditLogs_ActorNamesNoValues` dùng hai hàm phụ mới (`mustWebKey`, `recordMinutely`) thay đoạn lặp
  (jscpd), không đổi ca kiểm. Ca "↓ ở VTP ghi webSort từng tài khoản" của `WebCarriers.test.tsx` thay bằng ca một lệnh
  PUT thứ tự (hành vi đổi theo task); luật `reorder` thay bằng `moveOrder` cùng test.

## Việc phát hiện thêm

- Tên mục nhật ký đọc từng thực thể (≤ 50 lần mỗi trang); nếu trang nhật ký chậm thì thêm câu đọc tên theo lô ở order,
  inventory, fulfillment.
- Người không có quyền files.upload không nhận báo hiệu `files.file`: ảnh banner đã lưu đang xử lý chỉ cập nhật khi
  danh sách tải lại (resync 20 s).

## Reviewer

Kết luận: ĐẠT (lần 2). Lần 1 CẦN SỬA một phát hiện: màn bộ phối gửi bộ chưa có ảnh với `photoFileId: ""` → ogen trả
400 cho cả lần xem trước, máy chủ giả của vitest không bắt được. Đã sửa: màn chỉ gửi bộ đã chọn ảnh, lý do bộ chưa có ảnh
do màn tự tính (`skipText`/`lookProblem`), máy chủ giả trả 400 như ogen, thêm ca API `photoFileId` rỗng → 400; ci-task
XANH lại. Lần 2 reviewer đối chiếu lại: thứ tự khóa ADR-46 nhóm 8, RetryTx, `x-permission` + Idempotency-Key, RLS,
không dữ liệu cá nhân cho audit.read, bảng bằng chứng khớp test. Ghi chú không bắt buộc: chú thích đầu `lookData.ts`
còn chữ "gửi cả danh sách" (chưa sửa để giữ dấu duyệt; chỉ câu chữ); dòng bằng chứng hiddenCarriers đã ghi lại đúng test.
