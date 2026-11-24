# Task 195 — Yêu cầu về đơn hàng (dời từ admin, ADR-28)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 195, M5-12, làn L1 sau M5-11 và
M5-44; gồm "Bổ sung Task 450 — luồng đủ như admin"). Loại việc (quy trình mục 3): A

Đã kiểm trước khi viết: nhóm 7 của `docs/adr/0046-thu-tu-khoa.md` đã có `ord.order_requests` (sau `order_settings`),
`order_request_messages`, `order_request_files` (chỉ chèn); migration số `0504` còn trống; quyền khóa dịch vụ
`web.requests.write`, `web.orders.read` đã có (Task 189); mối đe dọa của M5-12 ở `docs/threat-model-m5.md` dòng 95–98.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn.
  - `order` (tệp mới `requests.go`, `requests_web.go`, `requests_staff.go`, `filepurposes.go`): kiểu `Request`,
    `RequestLine`, `RequestLineDetail`, `RequestDetail`, `RequestSummary`, `RequestMessage`, `ShipBack`, `ShipBackInput`,
    `Outcome`, `CreateRequestInput`, `RequestFilter`, `StatusChange`, `RequestChange`; hằng loại, trạng thái, tác giả,
    giới hạn, `RequestPhotoPurposeKey`, `MaxRequestPhotoBytes`, `TopicOrderRequestSignal`; hàm `RequestPhotoPurpose`,
    `RegisterFilePurposes`, `CanTransitionRequest`, `ListRequests`, `GetRequest`, `ListWebRequests`; phương thức
    `Writer.CreateRequestFromWeb`, `Writer.CreateCancelRequest`, `Writer.ReplyRequest`, `Writer.SetRequestStatus`,
    `Writer.CustomerReplyRequest`, `Writer.CustomerCancelRequest`; sentinel mới `ErrWindowClosed`, `ErrQtyExceeded`,
    `ErrPhotoRequired`, `ErrTooManyFiles`, `ErrDuplicateOpen`, `ErrFileRejected`, `ErrFileNotFound`,
    `ErrRequestNotAllowed`, `ErrRequestNotFound`, `ErrRequestLimit`. `Settings` thêm trường `ExchangeWindowDays` (chỉ
    đọc; `Settings` chỉ dựng bằng `settingsOf`, không literal ngoài gói nên không chỗ gọi nào gãy).
  - `identity`: hai quyền `order.requests.read`, `order.requests.manage` (`ScopeNone`) vào `Permissions`, nhãn trong
    `roles.go`; `sale` thêm cả hai (owner, manager tự nhận). Test có sẵn thêm dòng: `TestPermissionsList`,
    `TestDefaultRoles_Content` (quyền của sale); `docs/quyen.md` hai dòng.
  - `httpapi`: 9 handler mới do ogen sinh interface (`handlers_order_requests.go`, `handlers_web_requests.go`); bảng lỗi
    thêm trong `errors_web.go` (vùng đơn website, đã nằm trong `domainErrorMappers`); `h.audit`
    (`handlers_inventory.go`) tách phần ghi thành `recordAudit` để `auditService` (khóa dịch vụ) dùng chung (jscpd);
    dòng mới ở `errors_snapshot_test.go`.
  - `filestest`: thêm `Rejected(t, tx, tenantID, purpose)` (dòng `rejected` như sau khi `imager` từ chối) — helper test.
  - `cmd/crm/main.go`: gọi `order.RegisterFilePurposes()` cùng chỗ các mục đích M2, M3.
  - Tài liệu: `docs/threat-model-m5.md` dòng 95–98 đổi tên test theo test thật.
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `httpapi`, `httpapi/web`, `cash`, `cash/internal/refunds`,
  `fulfillment`, `testkit/codtest`, `testkit/shiptest`, `wiring/webcache`; `identity` ← `cmd/crm`,
  `adapters/cfaccess`, `httpapi`, `catalog`, `customer`, `platform/files`, `platform/realtime`,
  `realtime/realtimetest`, `tools/loadkit`. `order` mới nhập `platform/files` (không vòng: files không nhập order).
  `platform/files` test (`purpose_test.go` `allPurposes`) thêm `order.RequestPhotoPurpose()`.
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm. Website (`x-auth: service_key`, Idempotency-Key bắt buộc ở route
  ghi): `webUploadRequestFile` `POST /api/v1/web/files` (`web.requests.write`, `x-body-limit: 6291456`, thân `image/*`),
  `webCreateRequest` `POST /api/v1/web/orders/{externalRef}/requests` (`web.requests.write`), `webListRequests`
  `GET /api/v1/web/orders/{externalRef}/requests` (`web.orders.read`), `webReplyRequest`
  `POST /api/v1/web/orders/{externalRef}/requests/{ref}/messages`, `webCancelRequest`
  `POST /api/v1/web/orders/{externalRef}/requests/{ref}/cancel` (`web.requests.write`). Nhân viên:
  `listOrderRequests` `GET /api/v1/order-requests`, `getOrderRequest` `GET /api/v1/order-requests/{id}`
  (`order.requests.read`), `replyOrderRequest` `POST /api/v1/order-requests/{id}/messages`, `setOrderRequestStatus`
  `POST /api/v1/order-requests/{id}/status` (`order.requests.manage`, Idempotency-Key). Mã lỗi mới trong
  `x-error-codes`: `window_closed`, `qty_exceeded`, `photo_required`, `too_many_files`, `duplicate_open`,
  `file_rejected`, `file_not_found`, `request_not_allowed` (câu tiếng Việt ở `console/src/app/errors.ts`).
- Chỗ dùng trên màn hình: màn Yêu cầu về đơn hàng là M5-24 (Task 207, không làm ở đây). `console/src/api/**` sinh lại
  bằng `make gen`; thêm câu lỗi vào `errors.ts`. Màn Vai trò hiện nhãn hai quyền mới (lấy từ API).
- Câu sqlc, bảng, pha migration: `migrations/0504_order_requests.sql` (pha mở rộng: CREATE TABLE ×3, RLS, GRANT,
  trigger chỉ ghi thêm cho tin nhắn; `ALTER TABLE ord.order_settings ADD COLUMN exchange_window_days int NOT NULL
  DEFAULT 7` có CHECK 1–60; một dòng `platform.file_purposes ('order_request_photo','private','private') ON CONFLICT DO
  NOTHING`). Câu mới `queries/ord/requests.sql` (khối sqlc `order` có sẵn); `queries/ord/settings.sql` thêm cột vào ba
  câu đọc/RETURNING. sqlc sinh lại `models.go` của mọi gói store (thêm bảng, cột).
- Test sẽ chạy: `order -run 'Request'` (mới) + `Settings`; `httpapi -run 'OrderRequest|ErrorSnapshot|ErrorCodes|Route'`;
  `httpapi/web -run 'Request'`; `identity -run 'Permission|Role|Doc'`; `platform/files -run 'Purposes'`; console
  `errors.test.ts`; `tests/arch`; cả gói và vùng rủi ro cao (`order`, `files`, `identity`) ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `files.RegisterPrivateImage`, `files.TmpKey`,
  `files.Status`, `files.DownloadURL`, `files.PurposeOf`, `FileStore.Put` (kho tệp M1, R10); `filestest.Image`,
  `filestest.Uploaded`, `filestest.MemFileStore`; `order.ResolveListSearch` (q → mã đơn, SĐT, tên khách không dấu qua
  `customer.Search`); `customer.ListCursor` (con trỏ keyset `created_at, id`, như danh sách đơn); `store.LockOrder`
  (khóa đơn, nhóm 7); `WebOrderByExternalRef`; `refusableWrite`, `WriteTx`, `h.audit`, `catalogRead`, `staffNames`;
  `realtime.Bus.Publish`; `order.ErrVersionConflict`, `ErrInvalidTransition`, `ErrInvalidInput`, `ErrNotFound`;
  mẫu `fulfillment.RegisterFilePurposes` + `0306` cho mục đích tệp.
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên. Viết mới: sơ đồ trạng thái yêu cầu (khác sơ đồ đơn của
  `order/internal/state`, khác lý do thay đổi — luồng của admin); mã yêu cầu `YC-XXXXXXXX` (8 ký tự ngẫu nhiên
  `crypto/rand`, bảng chữ không lẫn; không dùng sequence như mã đơn vì mã yêu cầu khách thấy trên web, không nên đoán
  được số lượng). Kiểm loại, cỡ tệp trước khi `Put` đọc `files.PurposeOf(...).AllowedTypes/MaxBytes` (không chép giới
  hạn). Tên nhân viên trong tin nhắn đọc bằng `staffNames` có sẵn.

## Soát thiết kế

- 5.1 Chủ dữ liệu: yêu cầu về đơn thuộc `order` (kế hoạch: bảng "Sở hữu" M5, dòng "Yêu cầu về đơn hàng"); ba bảng
  trong schema `ord`. Tệp ảnh thuộc kho tệp M1 (`platform.files`), order chỉ giữ mã tệp.
- 5.2 Không ghi bảng module khác: chỉ ghi `ord.*`; ảnh ghi qua public API `files.RegisterPrivateImage` (lớp HTTP);
  audit qua `identity.RecordAudit` ở lớp HTTP (order không nhập identity).
- 5.3 Đọc module khác: `files.Status`, `files.DownloadURL` (public API); tên khách qua `customer.Search` trong
  `ResolveListSearch` có sẵn; tên nhân viên qua `identity.GetStaff` ở lớp HTTP.
- 5.4 Nguyên tử: tạo yêu cầu + gắn tệp + báo hiệu + audit + kết quả chống trùng trong một transaction
  (`refusableWrite`/`WriteTx`). Tải ảnh: `Put` ảnh thô lên `crm-tmp` **ngoài** transaction (R10), rồi một
  transaction: `RegisterPrivateImage` (dòng `uploaded` + lệnh `file.process_private`) + audit.
- 5.5 Không nguyên tử: xử lý ảnh bằng lệnh `file.process_private` của M1 sau commit; báo hiệu màn hình qua bus (gửi
  sau commit).
- 5.6 Gọi ra ngoài: không có (không gửi Zalo/SMS — P2 của WB04). `Put` lên kho tệp là hạ tầng của M1; lỗi → 503
  `retry_later`, không ghi dòng.
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; bảng mới có `tenant_id`, RLS (`platform.enable_tenant_rls`),
  `UNIQUE (tenant_id, id)`, khóa ngoại ghép `(tenant_id, order_id)`, `(tenant_id, request_id)`; mọi unique bắt đầu bằng
  `tenant_id`; `CheckSchemaRulesClean`.
- 5.8 Không biết HTTP: `order` không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi: 6 route ghi đều có `Idempotency-Key` (website: required theo `TestRealContractWritesRequired`) và
  `x-permission`; 3 route đọc có `x-permission`.
- 5.10 PostgreSQL là nguồn sự thật: mọi kiểm (cửa sổ đổi, số lượng, trùng yêu cầu hủy, giới hạn 5 yêu cầu/đơn/24 giờ)
  bằng câu đọc trong transaction đã khóa đơn; không dùng Valkey.
- 5.11 Không thêm thư viện.
- 5.12 Theo module mẫu: phương thức `Writer`, sentinel trong `order/errors.go`, đổi HTTP ở `errors_web.go`; test `_test`
  trên PostgreSQL thật, `t.Parallel()`.
- 6.1 Hai người cùng sửa: đổi trạng thái khóa dòng yêu cầu (`FOR UPDATE`) rồi so `version` (lệch → 409); khách trả lời,
  khách hủy cũng khóa dòng và kiểm trạng thái; trả lời của nhân viên chỉ chèn tin (không đổi version, không đụng người
  đổi trạng thái). Hai yêu cầu đổi cùng lúc trên một đơn: tạo yêu cầu khóa dòng đơn trước (`LockOrder`, nhóm 7) nên số
  còn đổi được tính tuần tự; thêm index unique một phần "một yêu cầu hủy đang mở mỗi đơn" làm chốt ở database.
- 6.2 Gửi lại: `Idempotency-Key` (middleware); tệp đã gắn yêu cầu khác không gắn lại được (`UNIQUE (tenant_id, file_id)`).
  Tải ảnh gửi lại cùng khóa → câu trả lời đã lưu (cùng `fileId`), không `Put` lại.
- 6.3 Thứ tự khóa: `orders` (7) → chèn `order_requests` → chèn `order_request_messages`, `order_request_files`; đổi
  trạng thái: chỉ khóa `order_requests` (không khóa đơn). Đọc `platform.files` không khóa. Đúng nhóm 7 ADR-46.
- 6.4 Lỗi tạm: `WriteTx` chạy lại cả transaction; fn gán lại biến kết quả mỗi lần; mã `YC-…` sinh trong fn.
- 6.5 Cách ly: READ COMMITTED + khóa dòng đơn/yêu cầu.
- 6.6–6.8: không có việc nền mới (xử lý ảnh là lệnh có sẵn của M1, lịch thử lại của M1).
- 6.9 Treo lâu: giới hạn câu lệnh, chờ khóa mặc định của `db`; tải ảnh ≤ 6 MiB (x-body-limit).
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: "tổng số đang yêu cầu đổi của một biến thể ≤ số đã giao" — test bảng ca + test hai yêu cầu song
  song (một thắng). Sơ đồ trạng thái: test mọi cặp (cạnh cho phép qua, cạnh cấm → `invalid_transition`).
- 6.12 Dữ liệu cá nhân: lời nhắn của khách (`customer_note`), nội dung tin (`body`) không vào audit (chỉ tên trường
  `changed_fields`), log, báo hiệu (chỉ mã yêu cầu); địa chỉ gửi trả là địa chỉ của shop. Ảnh khách chỉ ở `crm-tmp`
  (thô) và `crm-private` (biến thể), xem bằng link ký 10 phút.
- 6.13 Quyền: website — `web.requests.write` (ghi), `web.orders.read` (đọc), chỉ đơn kênh web của đúng `externalRef`;
  nhân viên — `order.requests.read` (đọc, nhận báo hiệu), `order.requests.manage` (trả lời, đổi trạng thái); mặc định từ
  chối, test 403.

## Quyết định thay chủ shop

- **Q1 — Làm đủ luồng của admin, kể cả hai đường khách của website hiện tại** (`webReplyRequest`: khách trả lời khi
  nhân viên hỏi thêm, needs_info → in_review; `webCancelRequest`: khách rút khi còn received). Kế hoạch chỉ liệt kê ba
  route website nhưng "Bổ sung Task 450" đòi luồng đủ như admin, và website hiện có hai việc này (`staff.ts`
  `customerReply`, `service.ts` `cancel`). Phương án khác: để M5-30 thêm — website sẽ mất tính năng khi chuyển sang CRM.
- **Q2 — Lý do, dòng, giới hạn lấy đúng website hiện tại** (`features/support/contracts.ts`): lý do cấp yêu cầu (hủy
  CHANGED_MIND/DUPLICATE/ADDRESS_CHANGE/OTHER; đổi SIZE/QUALITY/WRONG_ITEM/OTHER), lý do dòng (TOO_SMALL … OTHER), lời
  nhắn 5–1000 ký tự, ≤ 30 dòng, ≤ 20 chiếc mỗi dòng, ghi chú dòng ≤ 300, đổi/trả phải xác nhận tình trạng hàng
  (`conditionConfirmed`), tối đa 5 yêu cầu một đơn trong 24 giờ (→ 429 `too_fast`). Ảnh tối đa 5 (kế hoạch; website cũ
  3). Cần ảnh khi lý do dòng (hoặc lý do yêu cầu) là DEFECT hay WRONG_ITEM.
- **Q3 — Số còn đổi được trừ mọi yêu cầu đổi/trả chưa bị từ chối hay rút** (kể cả đã resolved), như website hiện tại
  (`closedStates = CANCELLED, REJECTED`). Kế hoạch ghi "đang mở (open, in_progress)" — với luồng mới, yêu cầu đổi đã xong
  vẫn phải giữ số (không đổi hai lần cùng chiếc). Ca của kế hoạch (cũ bị rejected → được) vẫn đúng.
- **Q4 — Giữ loại `return`** trong CHECK và Go (luật như đổi: đã giao, trong hạn, số lượng, ảnh) dù website hiện chưa
  gửi loại này; nới CHECK sau là thu hẹp/đổi ràng buộc, khó hơn thêm sẵn. `confirmed`/`items_received` chỉ cho `exchange`
  (CHECK của bảng cũng chặn).
- **Q5 — Hạn đổi đọc từ cột `ord.order_settings.exchange_window_days`** (mặc định 7, CHECK 1–60 như biến môi trường
  của website), chỉ đọc; **chưa** đưa lên API/màn Cài đặt đơn (website hiện tại cũng chỉ đổi bằng biến môi trường).
  `webListRequests` trả `exchangeWindowDays` để website hiện hạn. Phương án khác: thêm vào `SettingsPatch` + API —
  thêm thay đổi hợp đồng và màn hình ngoài phạm vi.
- **Q6 — Lúc giao = lần cuối đơn chuyển `delivered` trong `ord.order_status_history`**; hạn tính theo giờ (D + N×24 giờ,
  như website), không theo ngày lịch.
- **Q7 — Mã yêu cầu `YC-` + 8 ký tự ngẫu nhiên** (bảng chữ 32 ký tự không lẫn), không dùng sequence: mã hiện cho khách,
  không nên lộ số yêu cầu của shop. Trùng (40 bit) → sinh lại tối đa 3 lần.
- **Q8 — Tệp không dùng được** (không có, tenant khác, sai mục đích, đã xóa, đã gắn yêu cầu khác) → 422 `file_not_found`
  (kế hoạch Step 1 ghi 422; threat model ghi 404 — chọn 422 để không lẫn với "đơn không có" 404 của cùng route).
  Một tệp chỉ thuộc một yêu cầu (`UNIQUE (tenant_id, file_id)`).
- **Q9 — Ghi chú nội bộ được ghi cả khi yêu cầu đã đóng; tin hiện cho khách thì không** (→ 422 `invalid_transition`).
  Trả lời của nhân viên không tăng `version` (không làm hỏng lần đổi trạng thái đang mở của người khác).
- **Q10 — Lỗi nghiệp vụ của yêu cầu không lưu theo `Idempotency-Key`** (`refusableWrite`), như Task 456: trạng thái có
  thể đổi, gửi lại cùng khóa thì xét lại. Ảnh: `Put` lỗi → 503 `retry_later` không lưu.
- **Q11 — Ảnh của màn nhân viên có thêm trạng thái `missing`** (tệp đã dọn/hết hạn/mất trên kho) thay vì lỗi 500 làm
  hỏng cả trang yêu cầu.
- **Q12 — `CreateCancelRequest` (cho M5-13)** tạo yêu cầu hủy lý do OTHER, lời nhắn tùy chọn ≤ 1000 ký tự; luật như yêu
  cầu hủy từ web (đơn new/confirmed, một yêu cầu hủy đang mở).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đơn giao D: đổi D+7 được, D+8 → `window_closed`; chưa giao → `request_not_allowed`; đơn lạ → 404; báo hiệu `order_request.changed` cho `order.requests.read`, chỉ mã yêu cầu | `TestRequest_ExchangeWindow` | `go test -race -count=1 -run '^(TestRequest.*)$' ./internal/modules/order/` | `ok … 2.798s` (13 test PASS) |
| Hạn đổi đọc từ cài đặt (mặc định 7; đặt 3 → D+4 bị chặn) | `TestRequest_ExchangeWindowSetting` | như trên | PASS |
| Giao 2, yêu cầu mở đổi 1, mới 2 → `qty_exceeded`; cũ bị rejected → 2 được; biến thể ngoài đơn → invalid | `TestRequest_QtyHeld` | như trên | PASS |
| Hai yêu cầu đổi song song trên 2 chiếc → đúng một thắng | `TestRequest_ConcurrentExchange` | như trên | PASS |
| DEFECT không ảnh → `photo_required`; 6 ảnh → `too_many_files`; ảnh tenant khác, sai mục đích, không có, đã gắn → `file_not_found`; imager từ chối → `file_rejected`; uploaded + ready được, giữ thứ tự | `TestRequest_Files` | như trên | PASS |
| Hai yêu cầu hủy mở → `duplicate_open` (cả `CreateCancelRequest`); hủy đơn đã giao → không được; yêu cầu thứ 6 trong 24 giờ → `ErrRequestLimit`, sau 25 giờ được | `TestRequest_CancelAndLimit` | như trên | PASS |
| Đầu vào sai (8 ca) → invalid_input | `TestRequest_InvalidInput` | như trên | PASS |
| Mọi cặp (from, to) của 8 × 6 trạng thái: cạnh có trong sơ đồ qua, cạnh khác `invalid_transition`; confirmed cho cancel → `invalid_transition` | `TestRequest_Transitions` | như trên | PASS |
| Version cũ → 409; needs_info thiếu tin, lý do 501, mã vận đơn 61, hạn hôm qua, địa chỉ ngắn, kết quả sai trạng thái, thiếu hướng dẫn → invalid; từ chối không lý do → `ErrReasonRequired`; hạn lưu 23:59:59 giờ VN; resolved ghi người, giờ | `TestRequest_StatusRules` | như trên | PASS |
| Trả lời visible → website thấy; ghi chú nội bộ → không [WB04]; khách trả lời needs_info → in_review; khách rút received → cancelled (gửi lại như cũ); tin cho khách khi đã đóng → `invalid_transition`, ghi chú vẫn được | `TestRequest_MessagesAndCustomer` | như trên | PASS |
| `UPDATE`/`DELETE ord.order_request_messages` → append-only (chủ bảng), crm_app không sửa được | `TestRequest_MessagesAppendOnly` | như trên | PASS |
| Danh sách: mới trước, phân trang, lọc trạng thái, loại, q mã yêu cầu/mã đơn/tên không dấu; trạng thái lạ → invalid; tenant B không thấy, đọc → not found | `TestRequest_ListAndTenant` | như trên | PASS |
| `files.Register` với `order_request_photo` → `ErrPurposeMode` (R10) | `TestRequestPhotoPurpose_PrivateOnly` | như trên | PASS |
| gif → 422 `file_type_not_allowed`, text/html → 415, 6 MiB+1 → 413, không dòng tệp; HTML khai jpeg → 201, dòng `uploaded` bucket private, ảnh thô chỉ `crm-tmp` khóa `TmpKey`, 1 lệnh `file.process_private`, 0 `file.process`; gửi lại cùng khóa → cùng fileId; khóa đọc → 403; audit | `TestOrderRequestAPI_Upload` | `go test -race -count=1 -run '^(TestOrderRequestAPI_.*\|TestDomainErrorSnapshot.*\|TestErrorCodes.*)$' ./internal/httpapi/` | `ok … 2.992s` |
| Kho tạm lỗi → 503 `retry_later`, không dòng tệp, không lưu khóa | `TestOrderRequestAPI_UploadStoreDown` | như trên | PASS |
| Website tạo yêu cầu kèm ảnh (từ chối, lạ, thiếu, đơn lạ, khóa sai quyền); audit không lời nhắn; báo hiệu; qty_exceeded; nhân viên 403 / sale đọc, tìm theo mã; ảnh processing không url, ready có link `crm-private/priv/<tenant>/<file>/`, bị từ chối sau → rejected; trả lời + ghi chú, audit không nội dung; website chỉ thấy tin visible, `exchangeWindowDays` 7 [WB04] | `TestOrderRequestAPI_WebAndStaffFlow` | như trên | PASS |
| API trạng thái: 409, 422 invalid_transition, rejected không lý do 422; needs_info → khách trả lời qua web → in_review; xác nhận đổi, audit chỉ tên trường; khách rút đang xử lý → 422, rút received → cancelled hai lần, một audit; tenant khác → 404 | `TestOrderRequestAPI_Status` | như trên | PASS |
| Mã lỗi mới đổi đúng HTTP, có trong `x-error-codes` | `TestDomainErrorSnapshot`, `TestErrorCodes_DeclaredInContract` | như trên | PASS |
| Mục đích tệp Go khớp bảng (cả `order_request_photo` private) | `TestPurposes_GoMatchesTable` | `go test -count=1 -run 'TestPurposes\|TestRegister' ./internal/platform/files/` | `ok … 1.161s` (đỏ trước khi thêm vào `allPurposes`) |
| Quyền mới, vai trò sale, khớp `docs/quyen.md` | `TestPermissionsList`, `TestDefaultRoles_Content`, `TestPermissionsDoc_*` | `go test -count=1 -run 'Permission\|Role\|Doc' ./internal/modules/identity/` | `ok … 21.580s` (đỏ ở `TestDefaultRoles_Content` trước khi sửa danh sách sale) |
| Schema: RLS, unique tenant_id | `TestCheckSchemaRulesCleanSchema` | `go test -count=1 -run TestCheckSchemaRulesCleanSchema ./internal/platform/tenancy/` | `ok` |
| Câu lỗi màn hình | `errors.test.ts` | `npx vitest run src/app/errors.test.ts` | `17 passed` |
| Cổng kiểm của task | (cả bộ) | `ci-task.sh` | `CI-TASK XANH` (toàn bộ `go test -race ./...` vì migration có chữ TRUNCATE trong trigger chỉ ghi thêm: 567 s; lint, gen-check, sqlc vet, jscpd, gitleaks, oasdiff, test-guard, console). Lần 1 đỏ jscpd (3 đoạn trùng, đã gộp: `recordAudit`, `webRequestWrite`, `inApp`), lần 2 đỏ `TestSellerFilterProductCode` (ADR-29: câu đọc đơn thiếu `seller_id`, đã thêm và test người bán khác) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký theo bảng đối chiếu của kế hoạch (dòng M5-12): phương thức `Writer` nhận `sellerID` và `order.Actor` (không
  `identity.AuthUser`); `Reply` → `Writer.ReplyRequest(…, visible, a, now)`, `SetStatus` → `Writer.SetRequestStatus(…,
  StatusChange, version, a, now)` (kèm tin, hướng dẫn gửi trả, kết quả của "Bổ sung Task 450"); audit ở lớp HTTP. Đọc
  (`GetRequest`, `ListRequests`, `ListWebRequests`) nhận `sellerID` theo luật ADR-29 (`TestSellerFilterProductCode`); lớp
  HTTP truyền người bán mặc định (`catalog.DefaultSeller`, như đơn web). Trả lời, đổi trạng thái chỉ chạm bảng yêu cầu
  (không đọc `ord.orders`) nên không nhận `sellerID`.
- Trạng thái theo "Bổ sung Task 450" (received … cancelled) thay `open/in_progress/…` của Interfaces gốc; bảng thêm cột
  `condition_confirmed`, `ship_back`, `outcome`; `reason_code` NOT NULL; `order_request_files` thêm `position` và
  `UNIQUE (tenant_id, file_id)`; index unique một phần "một yêu cầu hủy đang mở mỗi đơn".
- Handler website đặt ở `internal/httpapi/handlers_web_requests.go` (như `handlers_web_intake.go`), không tạo
  `internal/httpapi/web/requests.go`; test HTTP của cả hai phía ở `internal/httpapi/handlers_order_requests_test.go`
  (một môi trường có cả khóa dịch vụ và thẻ nhân viên).
- Ca "Người dùng có `order.requests.read` nhận báo hiệu SSE trong 1 s": kiểm báo hiệu phát đúng chủ đề
  `order_request.changed`, `Requires: order.requests.read`, chỉ mã yêu cầu (`TestRequest_ExchangeWindow`,
  `TestOrderRequestAPI_WebAndStaffFlow`); việc lọc theo quyền và giao trong 1 s là của `realtime` (đã có test ở M0).
- Ca ảnh sau khi xử lý (biến thể chỉ ở `priv/<tenant>/<file>/`, không còn EXIF, link hết hạn → 403): phần xử lý là của
  M1 (`TestProcessPrivate_NeverInPublicBucket` và các test `process_private_test.go`, `download_variants_test.go`);
  test của task này dùng `filestest.Image` (ready, biến thể private) và kiểm link ký trỏ tới `crm-private` dưới
  `priv/<tenant>/<file>/`, ảnh đang xử lý không url, ảnh bị từ chối `rejected` không url.

## Việc phát hiện thêm

- Website cần "đủ điều kiện đổi" (hạn, số đã giao, số đang yêu cầu) để hiện nút "Yêu cầu đổi hàng" như hiện nay
  (`exchangeEligibility`): task này chỉ trả `exchangeWindowDays`; ngày giao và số còn đổi được nên thêm vào tra đơn web
  (M5-13) hoặc M5-30.
- Màn Cài đặt đơn chưa có ô "Hạn đổi hàng (ngày)" (Q5); thêm khi chủ shop cần đổi khác 7.
- Đơn giao một phần: số còn đổi được tính theo số đã mua của dòng đơn, chưa trừ phần không giao (website hiện tại cũng
  vậy).

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, trên phần đã stage sau ci-task XANH).

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến khớp thực tế (thêm `Request.AuditFields()` chưa nêu — chỉ thêm). Thứ tự khóa đúng ADR-46 nhóm 7.
- Bảng bằng chứng: mọi test có thật và kiểm đúng tiêu chí; không test nào bị xóa, tắt, nới; test mới có `t.Parallel()`.
- Ghi chú không bắt buộc: log Warn khi `Put` lỗi rồi trả `retry_later` (nhánh này `writeHandlerError` không log, nên
  chỉ một lần); trùng mã YC ba lần liền → 500 (xác suất ~0); ca imager thật, link hết hạn, SSE 1 s giao cho test của M1
  và `realtime` (đã ghi ở "Khác kế hoạch"); dòng D của threat model ghi "theo khách" trong khi mã giới hạn theo đơn
  (5 yêu cầu/24 giờ) — để điều phối viên chỉnh câu chữ khi gom tài liệu.
