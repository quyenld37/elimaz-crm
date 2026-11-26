# Task 169 — Nhắc việc và ghi kết quả cuộc gọi

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 169 = M4-29). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào đã có. Thêm mới trong gói gốc
  `conversation`: kiểu `Reminder`, `ReminderInput`, hàm `CreateReminder`, `CompleteReminder`, `CancelReminder`, `ListMyReminders`,
  `CallLog`, `CallInput`, `Calls{Bus}` (Record), `ListCustomerCalls`, `CustomerConversation`, `ListCustomerConversations`,
  `PageCursor` (bí danh `customer.ListCursor`), sentinel lỗi; `queries.ChatTimeline`, `queries.TimelineKey`, `queries.ChatTimelineRow`. Phần internal
  `reminders.Parts`, `calls.Parts` (khung Task 143, 1 chỗ dùng mỗi hàm: `wiring.go`) giữ chữ ký, nay trả lịch
  `conversation.reminders` + handler. `GetCustomerTimeline` (handler M2, 0 chỗ gọi Go ngoài ogen) đổi thân: gộp thêm
  nguồn hội thoại (người không có `conversation.read` ở Page nào: như cũ). `customer.Timeline`, `customer.TimelineCursor`
  không đổi (Q-X1). Lỗi: tệp mới `internal/httpapi/errors_conversation.go` (`conversationError`), thêm vào
  `domainErrorMappers` (`errors.go`) và 6 dòng chụp + 3 dòng thành viên ở `errors_snapshot_test.go`; không thêm mã lỗi
  mới (`assignee_not_allowed` đã khai ở Task 143).
- Gói phụ thuộc ngược (go list): `internal/modules/conversation` ← `internal/httpapi`, `cmd/crm`; `internal/queries` ←
  `internal/httpapi`, `cmd/crm`. Test chạy: gói conversation (và internal/reminders, internal/calls), queries (chỉ test
  mới), httpapi (chỉ test mới + m4_contract + errors_snapshot), cmd/crm (wire_m4_test), tests/arch.
- Operation OpenAPI bị chạm, oasdiff: hiện thực 7 operation đã khai ở Task 143 (createReminder, listMyReminders,
  completeReminder, cancelReminder, createCallLog, listCustomerCalls, listCustomerConversations), xóa 7 dòng khỏi
  `tests/arch/testdata/m4_pending.txt`; getCustomerTimeline thêm kind `conversation`, `call` (chuỗi mở đã ghi sẵn ở
  hợp đồng). Không sửa `openapi.yaml` → oasdiff không đổi.
- Chỗ dùng trên màn hình: chưa có (màn M4 là Task 174–178). Dòng thời gian khách của M2 (`console/src/features/customers`)
  sẽ nhận kind mới `conversation`, `call`: kiểm khi viết test HTTP rằng thân chỉ có mã, không nội dung.
- Câu sqlc, bảng, pha migration: migration mới `0408_reminders_calls.sql` (pha mở rộng: CREATE TABLE
  `conversation.reminders`, `conversation.call_logs` chỉ ghi thêm (trigger), view `conversation.call_logs_v`, index,
  GRANT). Câu sqlc mới `queries/conversation/reminders.sql`, `calls.sql` (sinh vào `conversation/internal/store`).
  Đọc view `customer.customers_v`, `ord.orders_v` (Q1), `conversation.conversations_v`, `conversation.call_logs_v`.
  Ghi `conversation.messages` (tin system), `conversation.conversations` (status → open khi nhắc tới hạn).
- Test sẽ chạy: `conversation/{reminders_test.go,calls_test.go}`, `queries/chat_timeline_dynsql_test.go`,
  `httpapi/handlers_reminders_calls_test.go` (gồm dòng thời gian C7), `TestM4Contract_StatusCodes`,
  `TestDomainErrorSnapshot*`, `TestWiringEmpty`, `TestWireM4_Frame`, `tests/arch` (`TestViewsDocParses` đếm 46 view).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (cursor, limit, audit,
  realtime, schedules); `access.Require`, `access.VisiblePages`, `access.PageScope` (Task 143/151);
  `customer.RootOf`, `customer.GroupMembers`, `customer.TimelineCursor`, `customer.Timeline` (M2);
  `identity.RecordAudit`, `identity.Can`, `identity.StaffHas`; `realtime.Bus.Publish`; mẫu lịch theo tenant
  `order.expireDraftsSchedule`; `httpapi.refusableWrite`, `catalogRead`, `staffNames`, `timelineItemOut`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên, dùng lại `customer.ListCursor`/`ParseListCursor` có sẵn (con trỏ trang
  cuộc gọi, hội thoại của khách, như `handlers_feedback.go`), `db.NullUUID`, `optUUID` của httpapi (jscpd báo trùng khi
  tự viết, đã bỏ bản tự viết). Dòng thời gian gộp dùng `customer.TimelineCursor` (cùng khóa `(at, kind, ref_id)`). Viết
  mới: hàm gộp hai trang dòng thời gian ở handler (một chỗ dùng); `ptrIf` nhỏ trong `calls` (không có bản công khai
  đổi `pgtype.UUID` → con trỏ; `queue.fromPgUUID`, `inventory.fromPgUUID` là riêng của gói).

## Soát thiết kế

- 5.1 chủ dữ liệu: nhắc việc, cuộc gọi thuộc `conversation` (Q14), bảng trong schema `conversation`.
- 5.2 không ghi bảng module khác: chỉ ghi bảng của `conversation` và `identity.audit_logs` qua `identity.RecordAudit`.
- 5.3 đọc qua view: `customer.customers_v`, `ord.orders_v`; `internal/queries` đọc `conversation.conversations_v`,
  `conversation.call_logs_v` (view mới, không có `note`).
- 5.4 nguyên tử: ghi cuộc gọi + tin system + nhắc gọi lại + audit trong một transaction (`WriteTx`).
- 5.5 việc không nguyên tử: báo hiệu realtime phát qua `Bus.Publish(tx)` (ghi trong tx, gửi sau commit).
- 5.6 gọi ra ngoài: không áp dụng (không gọi Meta; gọi điện là `tel:` trên máy nhân viên).
- 5.7 tenant: bảng mới có `tenant_id`, RLS ép, UNIQUE (tenant_id, id), khóa ngoại ghép; mọi câu trong WithTenant.
- 5.8 không HTTP trong module: đúng; handler mỏng trong `handlers_reminders.go`, `handlers_calls.go`.
- 5.9 đường ghi: 4 route ghi đã có `Idempotency-Key`, `x-permission` từ Task 143.
- 5.10 Valkey: không áp dụng.
- 5.11 thư viện: không thêm.
- 5.12 module mẫu: theo Task 7/27/28 (public API ở gói gốc, sqlc, test trên PostgreSQL thật).
- 6.1 đồng thời: done/cancel là `UPDATE … WHERE status IN ('pending','fired')` có điều kiện (hai lần song song → một
  200, một 409 `version_conflict`); lịch nhắc dùng `UPDATE … WHERE status = 'pending'` nên hai worker không bắn hai lần.
- 6.2 gửi lại: Idempotency-Key của M0; cuộc gọi chỉ ghi thêm.
- 6.3 thứ tự khóa (ADR-46 nhóm 6): lịch nhắc khóa `conversations` theo id tăng dần (FOR UPDATE) rồi mới UPDATE
  `reminders`; ghi cuộc gọi chỉ INSERT theo thứ tự `reminders` → `messages` → `call_logs` (khóa khách, hội thoại chỉ
  là FOR KEY SHARE của khóa ngoại).
- 6.4 lỗi tạm: `db.RetryTx` qua WriteTx; hàm trong tx gán lại biến kết quả mỗi lần chạy.
- 6.5 READ COMMITTED + câu có điều kiện.
- 6.6 việc nền lỗi: lịch `conversation.reminders` MaxAttempts 1, hết hạn sau một kỳ (kỳ sau chạy lại, như
  `order.expire_drafts`); việc chạy lại không hại (điều kiện status = 'pending').
- 6.7 429: không áp dụng.
- 6.8 tiến trình chết: transaction lùi, nhắc vẫn `pending`, kỳ sau bắn.
- 6.9 treo: lô tối đa 200 nhắc mỗi kỳ.
- 6.10 tiền: không áp dụng.
- 6.11 rapid: không áp dụng (không thuộc vùng rủi ro cao).
- 6.12 dữ liệu cá nhân: ghi chú nhắc việc và cuộc gọi không vào log, audit (audit chỉ ghi tên trường), không vào báo
  hiệu, không vào `call_logs_v` và dòng thời gian khách; tin system chỉ có nhãn kết quả, không có ghi chú.
- 6.13 quyền: route kiểm quyền; module kiểm `conversation.reply` trên Page của hội thoại (không có → 404), giao nhắc
  cho người khác cần `conversation.assign` trên Page đó (→ 403); danh sách cuộc gọi, hội thoại, dòng thời gian lọc theo
  Page có `conversation.read`; `/me/reminders` chỉ của chính người gọi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH17/Q14: không nghe máy có hội thoại → 1 `call_logs`, 1 tin `system` "Đã gọi: không nghe máy"; audit không chứa ghi chú | `TestRecordCall`, `TestChatAPI_Calls` | `go test -race -count=1 -run '^(TestRecordCall|…)$' ./internal/modules/conversation/`; `… ./internal/httpapi/` | `ok … conversation 4.717s`; `ok … httpapi 5.260s` |
| `callback` thiếu `callbackAt` → 400; có giờ 15:00 → 1 nhắc `call_callback` lúc 15:00 cho người gọi, tin "hẹn gọi lại lúc 15:00 26/11/2026" | `TestRecordCall`, `TestChatAPI_Calls` | như trên | ok |
| Dữ liệu sai, khách không có, hội thoại Page không được trả lời, hội thoại/đơn không thuộc khách; `call_logs` chỉ ghi thêm | `TestRecordCallRefusals` | như trên | ok |
| B gộp vào A: `GET /customers/{B}/calls` thấy cuộc gọi ghi trên A; cuộc gọi gắn hội thoại Page không xem được bị bỏ; trang 2 + 1 | `TestListCustomerCalls`, `TestChatAPI_Calls` | như trên | ok |
| `GET /customers/{id}/conversations` theo nhóm khách, lọc Page, nhãn, bài viết | `TestListCustomerConversations`, `TestChatAPI_Calls` | như trên | ok |
| Nhắc 15:00 → lịch 14:59:30 chưa bắn, 15:00:30 → `fired`, hội thoại `done` → `open`, báo hiệu `reminder.fired` (phạm vi Page), chạy lại không bắn lần hai; `/me/reminders?status=fired` của người khác không thấy | `TestFireDueReminders` | `go test -race … ./internal/modules/conversation/` | ok |
| Sale không có `conversation.assign` giao nhắc cho người khác → 403; `done` hai lần song song → một 200, một 409 | `TestCreateReminder`, `TestCloseReminder`, `TestChatAPI_Reminders` | như trên + `./internal/httpapi/` | ok |
| C7: 2 đơn, 1 ghi chú, hội thoại hộp thư Page A, comment Page B (hồ sơ đã gộp), 1 cuộc gọi trên hội thoại A → cả hai Page 6 mục đúng thứ tự; chỉ Page A 5; trang 3 + 3 không trùng, không sót; thân không có nội dung tin, ghi chú cuộc gọi; người không có quyền hộp chat → 3 mục như M2 | `TestChatAPI_CustomerTimeline`, `TestChatTimeline` | `go test -race … ./internal/httpapi/`; `go test … -run '^TestChatTimeline$' ./internal/queries/` | `ok … httpapi`; `ok … queries 1.366s` |
| Lịch `conversation.reminders` nối vào worker | `TestWiringEmpty`, `TestWireM4_Frame` | `go test -count=1 -run '^TestWireM4' ./cmd/crm/` | `ok … cmd/crm 0.738s` |
| Hợp đồng: 7 operation hết 501, 401/403 đúng | `TestM4Contract_StatusCodes` | `go test -race … ./internal/httpapi/` | ok |
| Cổng kiểm của task | toàn bộ cổng tĩnh + test | ci-task (log ngoài worktree) | `CI-TASK XANH (so với điểm tách nhánh aafa087…)`; test toàn bộ trừ e2e 433 giây |

## Test bị xóa hoặc tắt

(không có). Sửa kỳ vọng (không nới): `TestWiringEmpty`, `TestWireM4_Frame` nay đòi đúng một lịch
`conversation.reminders` và một handler cùng Kind (trước: không có gì); `TestViewsDocParses` đếm 46 view (+`call_logs_v`).

## Khác kế hoạch

1. Logic ở `internal/reminders`, `internal/calls` (tệp khung của kế hoạch); phần công khai ở gói gốc
   `conversation/reminders.go`, `calls.go` (bí danh kiểu + hàm mỏng) vì `httpapi` không nhập được gói internal. Danh sách
   hội thoại của khách nằm trong `internal/calls` (cùng phạm vi nhóm khách và Page với danh sách cuộc gọi).
2. Test module đặt ở gói gốc (`conversation/reminders_test.go`, `calls_test.go`, fixture chung `chatenv_test.go`)
   thay vì `internal/reminders/reminders_test.go`, `internal/calls/calls_test.go`: hai tệp dùng chung một fixture và test
   qua public API. Test HTTP ở `handlers_reminders_calls_test.go` (gồm ca dòng thời gian C7) thay vì
   `handlers_customer_timeline_test.go`.
3. `internal/queries/chat_timeline.go` → `chat_timeline_dynsql.go` (+ `_dynsql_test.go`): luật kiến trúc chỉ cho
   `tx.Query` trong tệp `*_dynsql.go`. `ChatTimelineRow.Summary` là `map[string]string` (kế hoạch ghi `map[string]any`)
   vì `TimelineItem.summary` của hợp đồng là map chuỗi; khóa đúng như kế hoạch (`conversationId`, `pageId`, `kind`,
   `outcome`).
4. Bảng `reminders` thêm cột `created_by` (người tạo nhắc; khác `user_id` khi trưởng nhóm giao) và ràng buộc
   `fired` → có `fired_at`; index thêm `reminders_conversation`, `call_logs_conversation`. `call_logs.created_at` ghi
   bằng giờ ứng dụng (cùng đồng hồ với tin hệ thống, nhắc gọi lại; tránh so hai đồng hồ — bẫy Task 400).
5. Con trỏ trang của `/customers/{id}/calls` và `/conversations` dùng lại `customer.ListCursor` (thời điểm + mã),
   như `handlers_feedback.go`; con trỏ hỏng → 400 `invalid_input` qua `customer.ErrInvalidInput`.
6. Viết mã trước khi chạy test lần đầu (test viết ngay sau trong cùng lượt, không có lần chạy FAIL ghi lại) — lệch thứ
   tự TDD của quy trình; bù lại mỗi tiêu chí có test riêng ở bảng bằng chứng.

## Quyết định thay chủ shop

1. **Nhắc đã đóng → 409 `version_conflict`** (không thêm mã lỗi mới): cùng nghĩa "màn hình đang cũ, tải lại" như các
   cập nhật có điều kiện khác; không phải sửa `x-error-codes`, `errors.ts`. Phương án khác: mã riêng
   `reminder_closed`. Đảo ngược: thêm mã vào hợp đồng, màn hình và một dòng ở `errors_conversation.go`.
2. **"Hẹn gọi lại" bắt buộc gắn hội thoại** (thiếu `conversationId` → 400): `conversation.reminders.conversation_id`
   NOT NULL và `Reminder.conversationId` bắt buộc trong hợp đồng; nút gọi nằm ở khung bên phải của hội thoại (Task 176).
   Phương án khác: cho `conversation_id` rỗng (đổi hợp đồng, phá vỡ). Đảo ngược: migration mới bỏ NOT NULL + đổi schema.
3. **Kiểm người được giao nhắc bằng `identity.StaffHas(conversation.reply)`** (phạm vi bất kỳ, không riêng Page của hội
   thoại): người nhận phải có quyền tự đóng nhắc (route done/cancel đòi `conversation.reply`; sửa theo reviewer lượt 1).
   identity chưa có hàm công khai kiểm quyền của người khác theo phạm vi; thêm hàm đó là sửa vùng rủi ro cao ngoài task.
   Còn lại: người chỉ trả lời Page B vẫn nhận được nhắc của hội thoại Page A (thấy trong `/me/reminders`, mở hội thoại
   hay đóng nhắc thì 404). Đảo ngược: thêm `identity.StaffCan(ctx, tx, userID, p, scope)` rồi đổi một dòng ở
   `reminders.Create`.
4. **Tin hệ thống của cuộc gọi không đổi `last_message_at`, `unread_count` của hội thoại** (không đẩy hội thoại lên đầu
   hộp thư vì việc của chính nhân viên); hiện trong lịch sử tin theo `sent_at`. Đảo ngược: thêm UPDATE hội thoại.
5. **Cuộc gọi không gắn hội thoại** hiện với mọi người có `conversation.read` (Page bất kỳ) — như kế hoạch "cuộc gọi
   gắn hội thoại của Page không được xem thì bỏ". **Ai đóng được nhắc:** người được nhắc, hoặc người có
   `conversation.assign` trên Page của hội thoại; người khác → 404.

## Việc phát hiện thêm

- identity chưa có hàm kiểm quyền của một nhân viên khác theo phạm vi cụ thể (Quyết định 3); Task 160
  (`assignee_not_allowed` khi giao hội thoại) sẽ cần cùng hàm.
- Thời điểm trong thân API trả theo múi giờ phiên database (+07:00), không chuẩn hóa UTC — đúng ở mọi handler cũ, ghi
  lại để màn hình M4 đọc bằng `Date` chuẩn.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 169 không có test e2e; đủ ca ở test gói conversation, queries, httpapi)

## Reviewer

Lượt 1: CẦN SỬA — người được giao nhắc chỉ cần `conversation.read` nên không tự đóng được nhắc (done/cancel đòi
`conversation.reply`). Đã sửa: kiểm `StaffHas(conversation.reply)`, test giao cho người chỉ đọc → 422, người được giao tự
đóng được (`TestCreateReminder`); Quyết định 3 cập nhật.

Lượt 2: ĐẠT — không còn phát hiện phải sửa. Ghi chú không bắt buộc của reviewer: (1) ca hẹp còn lại của Quyết định 3
(người có `conversation.reply` chỉ ở Page B vẫn được giao nhắc Page A; cần `identity.StaffCan` theo phạm vi, cùng việc
Task 160); (2) chú thích của `ErrAssigneeNotAllowed`/`reminders.Create` còn chỗ ghi "quyền đọc" — để task sau sửa chữ;
(3) báo hiệu `reminder.fired` tới mọi người đọc Page (không nội dung), màn hình Task 176 lọc theo `userId`; (4) tạo,
đóng nhắc không ghi audit (kế hoạch không đòi).

Kết luận: ĐẠT
