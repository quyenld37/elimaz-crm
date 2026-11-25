# Task 153 — Tin nhắn vào — khách, hội thoại, tin, echo, đã nhận, đã xem

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 153 = M4-13). Nhánh gốc: `m4`.
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới: `inbound.NewHandler(d deps.Deps, source string) webhooks.InboundHandler` (gói internal của conversation, 0 chỗ
    dùng ngoài `inbound.Parts`); `inbound.Parts` đổi thân (khung Task 143 trả rỗng → một handler cho mỗi khóa trong
    `d.Registry`), chữ ký giữ nguyên (1 chỗ gọi: `conversation.areas`).
  - Mới: `deps.TopicConversationChanged`, `deps.EntityConversation`; `conversation.TopicConversationChanged` thành bí
    danh của hằng trong deps (giá trị không đổi; 0 chỗ dùng ngoài module ngoài `topics.go`).
  - `conversation.InboundHandlers` (1 chỗ dùng: `cmd/crm/wire_m4.go`): chữ ký không đổi, nay trả handler thật cho
    `facebook`, `fakechat` (mỗi adapter có trong Registry).
- Gói phụ thuộc ngược (go list): `internal/modules/conversation/internal/inbound` ← `internal/modules/conversation` ←
  `cmd/crm`. `deps` ← conversation và mọi phần internal của nó. Không sửa `internal/platform/tenancy` (chỉ migration
  mở thêm loại tuyến).
- Operation OpenAPI bị chạm, oasdiff: không có (đường vào là việc nền).
- Chỗ dùng trên màn hình: không (màn hình M4 là task 174–178).
- Câu sqlc, bảng, pha migration:
  - Mới `queries/conversation/inbound.sql` → `internal/modules/conversation/internal/store/inbound.sql.go` (make gen):
    upsert hội thoại inbox, chèn tin chống trùng, cập nhật hội thoại khi có tin vào/ra, xác nhận echo của CRM, đã
    nhận, đã xem, cảm xúc, sửa tin (khóa dòng tin, ghi `message_revisions`).
  - Bảng chạm: `conversation.conversations`, `conversation.messages`, `conversation.message_revisions` (ghi);
    `channel.channel_accounts` qua `channel.AccountByExternal`, `channel.NoteInbound`; `customer.*` qua
    `customer.Writer.FindOrCreateByIdentity`.
  - Migration mới `0412_conversation_inbound.sql`, pha mở rộng: (a) `CREATE OR REPLACE` `platform.put_tenant_route`
    nhận thêm loại `fakechat_page` (thân giữ nguyên, chỉ thêm một giá trị được phép); (b) khóa ngoại ghép
    `(tenant_id, channel_account_id) → channel.channel_accounts (tenant_id, id)` cho `page_settings`, `posts`,
    `conversations`, `messages` (Task 151 để lại).
- Tệp khác: `inbound/hooks.go` (hook khung giao việc, lịch sử, tự động), `inbound/attachments.go`
  (`OnInboundAttachments`, khung cho Task 155), `inbound/profile.go` (`OnNewCustomer(ctx, tx, customerID,
  conversationID)`, khung cho Task 155); `docs/views.md` (dòng khóa ngoại xuyên module mới); `tests/e2e/stack_test.go`
  (tách hàm dùng chung `ingestServer`, `postSigned`, `nextChanged` để e2e mới không lặp mã; `postPingTo`, `nextPong`,
  `newStack` gọi lại các hàm đó, hành vi giữ nguyên, thêm header `Content-Type: application/json` cho gói selftest);
  `cmd/crm/wire_m4_test.go`, `internal/modules/conversation/wiring_test.go` (khung không còn rỗng: mỗi adapter một
  handler vào).
- Test sẽ chạy: test mới trong `internal/modules/conversation/internal/inbound/` (messages, echo, watermark,
  concurrency); `internal/modules/conversation/` (schema_test, explain_test sửa dữ liệu thử cho khóa ngoại mới,
  wiring_test); `tests/arch`; `cmd/crm` (wire_m4_test). E2E `tests/e2e/fb_inbound_latency_test.go` viết sẵn, không chạy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (realtime, queue,
  webhooks, testkit), `customer.Writer.FindOrCreateByIdentity` (Task 152), `channel.AccountByExternal`,
  `channel.NoteInbound` (Task 145), `webhooks.EnqueueInbound`, `webhooks.InboundJobHandlers` (M0/M2),
  `realtimetest.RecordingBus`, `testkit.ChannelAccount`, `testkit.DrainDue`, `testkit.MakeDueSoon`,
  `access.PageScope` (Task 151), `facebook.New(...).Split` cho fixture `testdata/echo_business_suite.json`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên; viết mới chỉ phần nghiệp vụ của đường vào (câu SQL trong
  `queries/conversation/inbound.sql`, các hàm xử lý trong `inbound/`). Hàm cắt đoạn trích 80 ký tự viết mới trong
  inbound (không có hàm chung cắt theo rune; việc riêng của hội thoại).

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1 Dữ liệu mới (hội thoại, tin) thuộc conversation; khách thuộc customer, tài khoản Page thuộc channel.
- 5.2 Không ghi bảng module khác: khách qua `customer.Writer.FindOrCreateByIdentity`, mốc của Page qua
  `channel.NoteInbound`.
- 5.3 Đọc tài khoản Page qua public API `channel.AccountByExternal` (đã có), không đọc bảng channel.
- 5.4 Một sự kiện vào = một transaction (của việc `inbound.<nguồn>`): khách + hội thoại + tin + báo hiệu nguyên tử.
- 5.5 Báo hiệu qua `Bus.Publish` trong tx (chỉ đi khi commit); không sự kiện nội bộ mới (`message.received` chỉ phát
  khi có module nghe, Q8 — chưa có). Hook các task sau (đính kèm 155, giao việc 160, lịch sử 164, tự động 167, hồ sơ
  155) là hàm khung không làm gì trong `inbound/hooks.go`.
- 5.6 Không gọi ra ngoài (handler chạy trong tx, không mạng).
- 5.7 Mọi truy vấn trong tx tenant của việc vào (`tenancy.WithTenant` do hàng đợi mở); khóa ngoại mới ghép
  `tenant_id`.
- 5.8 inbound không nhập `net/http`.
- 5.9 Không có route ghi mới; chống trùng: tầng 1 `jobs.dedupe_key`, tầng 2 khóa tự nhiên `messages_external_key`
  (`ON CONFLICT DO NOTHING`), `conversations_thread_key`.
- 5.10 Không Valkey. 5.11 Không thư viện mới. 5.12 Theo mẫu module (sqlc trong `queries/conversation`, test
  PostgreSQL thật).

Đúng khi chạy đồng thời (mục 6):
- 6.1 / 6.3 **Thứ tự khóa của đường vào (ADR-46):** (1) `channel.AccountByExternal` chỉ đọc, `channel.NoteInbound`
  UPDATE có điều kiện một dòng `channel_accounts` (nhóm 3, mỗi phút tối đa một lần; tx khác cùng Page thấy điều kiện
  sai sau khi chờ, không ghi); (2) `customer.Writer.FindOrCreateByIdentity`: khóa tư vấn danh tính rồi dòng khách
  (nhóm 5); (3) `conversation.conversations` upsert (khóa dòng hội thoại, nhóm 6); (4) `messages` chèn/cập nhật,
  (5) `message_revisions` chèn; (6) cập nhật lại dòng hội thoại đã giữ ở (3). Khóa ngoại mới chỉ lấy KEY SHARE trên
  `channel_accounts`, không xung đột với FOR NO KEY UPDATE của `NoteInbound`. Echo của CRM chỉ khóa một dòng
  `messages`, không chạm hội thoại (hội thoại đứng trước tin trong nhóm 6). Đã nhận/đã xem chỉ UPDATE các dòng tin
  của một hội thoại; cùng hội thoại xử lý lần lượt (OrderKey theo Page + người). Test 200 sự kiện, 20 hội thoại, 4
  worker: `pg_stat_database.deadlocks` không tăng.
- 6.2 Cùng `mid` hai lần (khác khóa chống trùng) → `ON CONFLICT DO NOTHING`, 0 dòng → dừng, không đổi bộ đếm. Sửa
  tin: bỏ qua khi `edited_at` đã ≥ mốc sửa (giao lại hay đến muộn). Echo của CRM: chỉ đổi từ
  `queued|checking|in_doubt`, không đổi khi `mid` đã có ở dòng khác (không vi phạm unique, không vòng thử lại).
- 6.4 Việc nền: lỗi tạm → lịch thử lại của hàng đợi; payload hỏng → `queue.ErrPermanent`.
- 6.5 READ COMMITTED + câu có điều kiện (`GREATEST`, `ON CONFLICT`, đoạn trích chỉ đổi khi tin mới hơn tin cuối).
- 6.6 Như 6.4. 6.7 Không áp dụng: không gọi bên ngoài. 6.8 Tiến trình chết → tx lùi, việc chạy lại; chạy lại an toàn
  nhờ khóa tự nhiên. 6.9 Handler chỉ vài câu SQL. 6.10 Không áp dụng: không có tiền.
- 6.11 Quy luật "cùng tin hai lần → một kết quả" và "thứ tự đến không đổi kết quả cuối" kiểm bằng test bảng và test
  đảo thứ tự (không phải vùng rủi ro cao rapid bắt buộc).
- 6.12 Nội dung tin, PSID không vào log, lỗi, payload việc (payload chỉ mang mã bản nhận); lỗi bọc chỉ có loại sự
  kiện; test đọc log mức debug của worker và payload mọi việc mới tạo, không thấy `xin chào shop 0912345678`.
- 6.13 Báo hiệu đòi `conversation.read` phạm vi `page:<channel_account_id>` (Q36).

## Quyết định thay chủ shop

1. **Số migration 0412.** Kế hoạch đã chia 0401–0411 cho các task khác; phần mở loại tuyến và khóa ngoại không có số
   riêng → lấy số kế tiếp 0412. Phương án khác: chèn vào số của task khác (gây xung đột). Database M4 chưa chạy ở máy
   chủ nên thứ tự goose không ảnh hưởng.
2. **Mở `fakechat_page` bằng `CREATE OR REPLACE`** (0290 ghi "không mốc nào CREATE OR REPLACE lại hàm này"): điều phối
   viên giao rõ việc này; thân hàm giữ nguyên từng dòng, chỉ thêm một loại được phép (mở rộng, đảo được bằng một
   migration thay lại). Phương án khác: hàm mới `put_fakechat_route` (thêm đường ghi tuyến thứ hai, phải sửa
   `tenancy.PutRoute`).
3. **Ứng dụng "của CRM" theo nguồn:** `facebook` → `Config.Meta.AppID`; kênh giả `fakechat` → chuỗi `fakechat`
   (kênh giả không có app id). `EchoAppID` rỗng hay khác → `page_external`. Phương án khác: thêm trường vào Deps
   (phải sửa `cmd/crm/wire_m4.go`, tệp chỉ Task 143 sửa).
4. **Danh tính kênh giả** dùng loại `fb_psid` với phạm vi là mã Page giả (customer chỉ nhận `fb_psid` ở M4).
5. **Tin không có `mid`** (postback không mid): `external_id = "postback:<psid>:<ms>"`; tin hệ thống "đến từ quảng cáo"
   `external_id = "referral:<psid>:<ms>"` để chống trùng bằng cùng khóa tự nhiên. Tin hệ thống: `kind system`,
   `origin system`, `direction out` (ràng buộc `messages_direction_origin`), không đổi mốc hội thoại.
6. **Tài khoản Page không có trong shop** (tuyến còn mà Page đã gỡ): bỏ qua, đếm `inbound_ignored{account_missing}`,
   việc xong (không dead). Sửa/cảm xúc cho tin chưa có: bỏ qua, đếm `inbound_ignored{<loại>_unknown_message}`.
7. **Đổi trạng thái `done|pending → open`** tăng `version` của hội thoại (người đang mở bản cũ sẽ nhận xung đột);
   cập nhật mốc, chưa đọc không tăng `version`.
8. **Echo `page_external` và `own_echo_unmatched`** (tin thật đã tới khách) cập nhật `last_message_at`, `last_snippet`
   (khi mới hơn) và `last_staff_message_at`; echo của CRM khớp `client_ref` chỉ đổi dòng tin (không khóa hội thoại sau
   tin, giữ thứ tự nhóm 6; đường gửi Task 156 cập nhật hội thoại). Tệp đính kèm của echo cũng gọi
   `OnInboundAttachments` (Task 155 quyết định tải).
9. **Đã xem** đặt cả `delivered_at` còn trống (đã xem thì đã nhận); chỉ tin `kind message` trong hội thoại inbox.
10. **Hook `OnNewCustomer` nhận thêm mã hội thoại** (gọi sau khi upsert hội thoại): Task 155 cần `conversationId` cho
   lệnh `command.fb.fetch_profile`, lúc tạo khách hội thoại chưa có.
11. **Khách của hội thoại** là hồ sơ giữ danh tính (`ChannelCustomer.CustomerID`), không phải gốc đại diện: gốc có thể
   đổi khi gộp; màn hình đọc gốc qua customer.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH08: tin đầu của PSID → 1 khách lead `name_source = placeholder`, danh tính `fb_psid` phạm vi mã Page, 1 hội thoại inbox, 1 tin in, chưa đọc 1, mốc tin khách = mốc Meta; CH01 (phần module): 1 báo hiệu `conversation.changed` đòi `conversation.read` trên `page:<id>`, chỉ số `webhook_to_signal_ms` | `TestInboundFirstMessageCreatesLeadAndConversation` | `go test -race -count=1 ./internal/modules/conversation/...` | `ok …/conversation/internal/inbound 4.327s`; ci-task `ok …/inbound 3.518s`, `CI-TASK XANH` |
| Tin thứ hai → chưa đọc 2, đoạn trích tin mới (cắt 80 ký tự); HT03: cùng mid qua việc khóa khác → vẫn 2 tin, chưa đọc 2 | `TestInboundSecondMessageAndDuplicateMID` | như trên | PASS |
| Hai tin ngược thứ tự → mốc, đoạn trích của tin mới hơn | `TestInboundOutOfOrderKeepsNewest` | như trên | PASS |
| CH14: echo app_id 263902037430900 (fixture thật qua `facebook.Split`) → 1 tin out `page_external`, chưa đọc không đổi, `last_staff_message_at` cập nhật; giao lại → 1 tin. Echo tới trước tin khách → tạo hội thoại | `TestEchoFromBusinessSuite`, `TestEchoCreatesConversation` | như trên | PASS |
| Echo của CRM có metadata = `client_ref` (queued, checking, in_doubt) → đúng tin đó `sent` có `external_id`, không thêm dòng; tin failed không đổi | `TestEchoOwnMatchesClientRef` | như trên | PASS |
| Echo của CRM không metadata → 1 dòng `own_echo_unmatched`; giao lại → vẫn 1 | `TestEchoOwnUnmatched` | như trên | PASS |
| Đã nhận với watermark giữa hai tin ra → chỉ tin cũ hơn có `delivered_at`; tin quá 7 ngày không đổi; đã xem đặt `read_at` | `TestWatermarkDeliveredAndRead` | như trên | PASS |
| Sửa tin → 1 dòng `message_revisions` giữ nội dung cũ; giao lại / lần sửa cũ hơn → không thêm | `TestMessageEditKeepsRevision` | như trên | PASS |
| Cảm xúc thả / bỏ | `TestMessageReaction` | như trên | PASS |
| Hội thoại done/pending nhận tin → open (version + 1); spam giữ | `TestInboundReopensConversation` | như trên | PASS |
| Tài khoản read_only, disconnected → tin vẫn lưu | `TestInboundStoresForReadOnlyAndDisconnectedAccounts` | như trên | PASS |
| Nút bấm không mid, tin "đến từ quảng cáo" (không nới cửa sổ), giao lại không trùng | `TestInboundPostbackAndReferral` | như trên | PASS |
| Loại chưa xử lý, Page không có trong shop → `inbound_ignored`, việc xong | `TestInboundIgnoresUnknownTypeAndAccount` | như trên | PASS |
| Tuyến `fakechat_page` ghi được (migration 0412); tin kênh giả qua adapter fakechat | `TestInboundFakechat` | như trên | PASS |
| Thứ tự khóa: 200 sự kiện, 20 hội thoại, 4 worker → 0 lần RetryTx gặp 40P01 (móc `OnRetry`/`OnExhausted` của `db.DB` mà worker dùng), `pg_stat_database.deadlocks` không tăng (kiểm thêm), không việc dead, đếm đúng | `TestInboundConcurrentNoDeadlock` | `go test -race -count=3 -run '^TestInboundConcurrentNoDeadlock$' ./internal/modules/conversation/internal/inbound/` | `ok … 7.293s` |
| Log debug và payload mọi việc không chứa `xin chào shop 0912345678` | `TestInboundDoesNotLeakContent` | như dòng 1 | PASS |
| Khóa ngoại 0412: bài, hội thoại, tin, cài đặt trỏ Page không có / Page tenant khác → 23503 | `TestSchema_ForeignKeys` | như dòng 1 | `ok …/conversation 14.357s` |
| Nối: mỗi adapter một handler vào | `TestWiring`, `TestWireM4_Frame` | như dòng 1; `go test -count=1 -run '^TestWireM4_Frame$' ./cmd/crm/` | `ok …/cmd/crm 0.929s` |
| CH01 đường thật (dưới 2.000 ms, p95, Page 2 không nhận) | `TestFBInboundLatency` (tests/e2e) | chỉ `go vet ./tests/e2e/`, golangci-lint 0 issues | **viết sẵn, chưa chạy** (luật phiên) |

## E2E viết sẵn, chưa chạy

- `tests/e2e/fb_inbound_latency_test.go`: `TestFBInboundLatency` (CH01, dưới 2.000 ms, 20 lần, p95; nhân viên chỉ có
  quyền Page 2 không nhận gì).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Chú thích của `tenancy.PutRoute` (`internal/platform/tenancy/put_route.go`) còn ghi kind chỉ nhận `carrier_account`,
  `fb_page`, `service_key`; nay hàm SQL nhận thêm `fakechat_page`. Không sửa trong task này để không chạm vùng rủi ro
  cao `tenancy` (chỉ là chú thích).
- `facebook.Adapter` còn trả nil tới Task 148: e2e `TestFBInboundLatency` dùng một adapter chỉ có khóa `facebook` cho
  `channel.Registry`; khi Task 148 gộp thì thay bằng `facebook.Adapter(cfg)`.
- Adapter kênh giả (`internal/adapters/fake/split.go`) không đặt `EchoAppID` cho echo: theo Quyết định 3, echo của CRM
  trên kênh giả phải mang app id `fakechat` để khớp `client_ref`; hiện mọi echo kênh giả thành `page_external`. Task
  156 (gửi, gộp echo) cần thêm trường `appId` vào sự kiện giả hay xem lại quy ước.

## Reviewer

Lần 1: CẦN SỬA — `TestInboundConcurrentNoDeadlock` chỉ đọc `pg_stat_database.deadlocks` (PostgreSQL 15+ ghi trễ) và
RetryTx tự chạy lại khi 40P01 nên việc không dead: test có thể qua dù có deadlock. Đã sửa: đếm 40P01 bằng móc
`OnRetry`/`OnExhausted` của `db.DB` mà worker dùng, đòi 0 lần. Ghi chú không bắt buộc (hằng `conversation.read` lặp ở
access, vị trí chú thích e2e, vế thừa trong wire_m4_test) không ảnh hưởng tính đúng, không sửa.

Lần 2: **ĐẠT**. Phần sửa đúng: móc gắn vào đúng `db.DB` của bốn worker, mọi transaction của worker (kể cả nhận, chốt
việc) đi qua RetryTx nên 40P01 nào cũng được đếm; pool riêng mỗi test. Không phát hiện phải sửa. Ghi chú không bắt
buộc: adapter `fake` không đặt `EchoAppID` nên echo kênh giả luôn là `page_external` (đã ghi vào Việc phát hiện thêm);
vế thừa trong `wire_m4_test.go` (không sửa).

Kết luận: ĐẠT
