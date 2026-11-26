# Task 168 — Nhắn lại khách chưa trả lời

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 168, M4-28). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu có sẵn. Gói gốc `conversation`
  (tệp mới `followups.go`): `FollowUp`, `ListFollowUps`, `FollowUpSchedule`, `AutoSendKind`. Phần riêng
  `internal/automation` (tệp mới `followup.go`): `FollowUpConfig`, `FollowUpItem`, `ListFollowUps`, `RunFollowUps`,
  `ErrInvalidFollowUp`, `FollowUpSchedule`, `TriggerFollowUpDue`; `automation.go`: `KindAutoSendCommand`, `Parts`
  thêm lệnh auto_send, việc và lịch `conversation.follow_up`; `command.go`: `hideCommand.OnTerminal` dùng hàm chung mới
  `finishStatus` (done → done, dead → failed, còn lại → dropped; cùng ánh xạ cũ cho lệnh ẩn); `rules.go`: `SaveRule`
  gọi `checkFollowUp` cho loại follow_up. Phần riêng `internal/send` (tệp mới `auto.go`): `KindAutoSend`,
  `AutoCommandKind`, `Automated`, `AutoCommand`, `NewAutoCommand`; hàm không xuất `insert` thêm tham số người gửi +
  `origin` và trả thêm mã lệnh, chọn Kind theo nguồn (2 chỗ gọi cũ: `Send` ở message.go, `Resend` ở resend.go — sửa
  cả hai, vẫn origin staff); `allowed` thêm cờ `automated` (2 chỗ gọi cũ sửa, truyền false); `sendCommand.OnTerminal`
  tách thân thành `settle(…, automated)` (OnTerminal cũ gọi với false, không đổi hành vi).
- Test có sẵn phải sửa vì danh sách đăng ký thêm mục: `internal/modules/conversation/wiring_test.go` (TestWiring: lệnh
  auto_send hạn 5 phút, lịch follow_up 300 s), `cmd/crm/wire_m4_test.go` (TestWireM4: lệnh, lịch M4),
  `tests/arch/testdata/m4_pending.txt` (xóa dòng `listFollowUps`: đã làm, luật của tệp đó).
- Gói phụ thuộc ngược (go list): `internal/modules/conversation` ← `internal/httpapi`, `cmd/crm`;
  `conversation/internal/send` ← `conversation`, `conversation/internal/automation` (mới); `internal/automation` ←
  `conversation`.
- Operation OpenAPI bị chạm, oasdiff: `listFollowUps` (GET /api/v1/follow-ups) đã có trong hợp đồng từ Task 143, nay
  có handler thật (trước: Unimplemented). Không đổi `openapi.yaml` → oasdiff không chênh. `updateAutomationRule` với
  `kind = follow_up` đã có lược đồ `FollowUpConfig`; thêm kiểm ở module (templateId phải là mẫu còn sống dùng được
  cho Page khi bật).
- Chỗ dùng trên màn hình: chưa có (màn Inbox Task 174–178 chưa làm); `console/src/api` không đổi.
- Câu sqlc, bảng, pha migration: không migration mới. Câu mới trong `queries/conversation/followups.sql`:
  `ListFollowUpCandidates` (đọc conversations, messages, `customer.customers_v`), `ListEnabledRulesOfKind` (đọc
  automation_rules), `FollowUpTemplateUsable` (đọc reply_templates). Sửa `InsertOutgoingMessage` (`queries/conversation/send.sql`): origin thành tham số `@origin`
  (mã sinh đổi `InsertOutgoingMessageParams`, chỗ gọi duy nhất `send.insert`).
- Test sẽ chạy: `go test -race -run` các test mới của `internal/modules/conversation/internal/automation` và
  `internal/httpapi` (TestFollowUps*), test cũ của `internal/send` liên quan `insert` (TestSend*, TestResend*); cả
  gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (schedules.Schedule,
  queue.Kind, commands.CommandKind, commands.Handler); trong module: `automation.Run` + `Skip` (cổng chung Task 165),
  `automation.finish` (ghi kết quả lượt), `send.insert`/`allowed`/`sendCommand` (đường gửi Task 156), lịch theo tenant
  mẫu `send.revealSchedule` (Task 157), `templates.Render` (Task 161, `Actor` nil = tin tự động),
  `access.VisiblePages` (lọc Page như `query.resolve`), `customer.customers_v` (cờ `do_not_contact` ở dòng gốc).
- Dùng lại / viết mới vì: dùng lại toàn bộ đường gửi (chèn tin, ghi lệnh, Execute, OnDone, chính sách gửi) — lệnh
  `command.fb.auto_send` chỉ là một bọc của `sendCommand` khác Kind (hạn 5 phút), khác OnTerminal (in_doubt → tin
  `discarded` + lượt `dropped`, không hỏi nhân viên) và gắn kết quả vào `automation_runs`. Cổng chung của quy tắc dùng
  lại `Run` (không viết cổng riêng). Câu ứng viên viết mới: chưa có câu nào lọc "nhân viên nói sau cùng, cửa sổ còn
  ≥ 5 phút"; một câu dùng chung cho danh sách và lịch.

## Soát thiết kế

5.1 Dữ liệu thuộc module conversation (automation_runs, messages); không bảng mới.
5.2 Không ghi bảng module khác; cờ không liên hệ chỉ đọc.
5.3 Đọc khách qua view `customer.customers_v` (cột `do_not_contact`, `root_id`, `full_name`), không đọc bảng.
5.4 Một lượt nhắn lại (khóa hội thoại → lượt automation_runs → tin queued → lệnh) nằm trong một transaction của việc
    lịch.
5.5 Gọi Meta sau commit qua lệnh `command.fb.auto_send`.
5.6 Không rõ đã gửi → `in_doubt` → tin `discarded` lý do `in_doubt`, lượt `dropped`; không tự gửi lại (04 mục 5.3
    "Tin tự động: in_doubt → bỏ, ghi nhật ký"). Echo có metadata tới sau vẫn đặt tin sent (sự thật thắng, Task 157).
5.7 Mọi câu trong tx tenant (việc lịch chạy trong WithTenant của worker; API trong `inventoryRead`).
5.8 Module không nhập net/http; handler mỏng `handlers_followups.go`.
5.9 Đường ghi mới không có (GET chỉ đọc; nhắn một chạm dùng `renderReplyTemplate` + `sendMessage` sẵn có). Lượt tự
    động chống trùng bằng unique `(tenant_id, rule_id, external_ref)` với `external_ref = fu:<hội thoại>:<tin nhân
    viên cuối>`; lệnh có DedupeKey = mã tin.
5.10 Không Valkey.
5.11 Không thư viện mới.
5.12 Theo mẫu: autohide.go (Run + act), indoubt.go (lịch theo tenant).
6.1 Hai lượt lịch chồng nhau cùng tenant: unique của automation_runs → lượt sau `already_run`, 0 lệnh. Khách nhắn
    giữa lúc đọc ứng viên và lúc gửi: khóa hội thoại rồi kiểm lại `last_customer_message_at` trên dòng đã khóa.
6.2 Lịch chạy lại (xử lý lại) → cùng `external_ref` → không lệnh thứ hai.
6.3 Khóa: hội thoại (theo mã tăng dần, ADR-46 nhóm 6) → automation_runs → tin; cùng thứ tự với đường gửi.
6.4 Việc lịch: MaxAttempts 1, kỳ sau chạy lại (như send_reveal).
6.5 READ COMMITTED + khóa dòng hội thoại.
6.6 Lệnh gửi lỗi tạm tự thử lại trong hạn 5 phút; hết hạn → tin expired, lượt dropped; Meta từ chối → failed.
6.7 Giới hạn tốc độ: lệnh lấy lượt gửi của Page (AcquireSendToken) như tin nhân viên.
6.8 Tiến trình chết: lệnh in_doubt → bỏ (5.6).
6.9 Mỗi lượt lịch giới hạn 100 hội thoại mỗi Page; câu ứng viên của lịch bỏ hội thoại mà tin nhân viên cuối đã có
    lượt của quy tắc (NOT EXISTS trên unique của automation_runs), nên lô không bị hội thoại đã xử lý chiếm và lượt sau
    làm tiếp phần còn lại (sửa theo reviewer vòng 1).
6.10 Không áp dụng: không tiền.
6.11 Thuộc tính "mỗi tin nhân viên tối đa một lần nhắn lại tự động": test chạy lịch nhiều lần, hai lịch song song.
6.12 Nội dung mẫu đã điền tên khách không vào log, audit, last_error (payload lệnh chỉ mã tin).
6.13 API lọc theo `access.VisiblePages` (conversation.read); pageId không được xem → danh sách rỗng (như
    listConversations); x-permission conversation.read ở guard.

## Quyết định thay chủ shop

1. **"Nhân viên nói sau cùng" tính theo tin người thật**, không theo cột `last_staff_message_at`: tin cuối hướng ra,
   loại `message`, nguồn `staff` hay `page_external` (nhân viên trả lời từ Meta Business Suite), không ở trạng thái
   failed/expired/discarded. Vì: `last_staff_message_at` cũng tăng khi tin tự động (tin chào, chính tin nhắn lại) đi
   ra; nếu tính theo cột, tin nhắn lại tự động sẽ thành "tin nhân viên mới" và sinh lượt thứ hai (trái "một bước", Q11).
   Hệ quả: sau khi tự nhắn lại, khách vẫn ở danh sách cho nhân viên tự quyết (mốc là tin nhân viên). Phương án khác:
   dùng cột như kế hoạch viết và loại riêng tin automation — phức tạp hơn, cùng kết quả cho lịch.
2. **Lệnh `command.fb.auto_send`** đặt trong phần `send` (bọc `sendCommand`, hạn 5 phút) và nhận một hàm ghi kết quả
   lượt từ `automation` (tránh vòng nhập). Task 167 (tin chào, đang làm song song) cũng cần lệnh này: khi gộp, dùng
   chung một bản (ghi ở "Việc phát hiện thêm").
3. **Mẫu của nhắn lại tự động chỉ gửi chữ**: ảnh và nút trả lời nhanh của mẫu không gửi (đường gửi tin của Task 156
   chưa mang nút; ảnh mẫu không phải mục đích chat_outgoing). Mẫu không có, đã xóa, của Page khác, thiếu biến → lượt
   `skipped_policy`, không gửi. Bật quy tắc mà `templateId` không phải mẫu dùng được cho Page → 422 (ErrInvalidInput)
   ngay lúc lưu. Phương án khác: gửi kèm ảnh — để khi màn cài đặt (Task 178) cần.
4. **OccurredAt của lượt = lúc lịch chạy** (không phải mốc tin nhân viên + afterHours), để bật quy tắc lúc khách đã im
   lâu vẫn nhắn được một lần trong cửa sổ còn mở; cổng "sự kiện cũ" của `Run` vì vậy không chặn nhắn lại.
5. **Danh sách sắp theo cửa sổ sắp đóng trước** (`last_customer_message_at` tăng dần), tối đa `limit` (mặc định 50,
   tối đa 100); `pageId` không được xem → rỗng.
6. **Tin nhắn lại tự động hết hạn (expired)** vẫn hiện "chưa gửi được" trong hội thoại và nhân viên có thể gửi lại tay
   (đường Task 157); `in_doubt` thì tin thành `discarded` lý do in_doubt (không hỏi nhân viên).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH16 danh sách: A có; B (khách trả lời sau), C (cửa sổ còn < 5 phút), D (do_not_contact), E (Page không được xem), F (chặn Page), G (done), H (mới im 2 giờ), I (chỉ tin tự động) không; C2 (cửa sổ còn 10 phút) có; sắp cửa sổ đóng sớm trước; pageId không được xem → rỗng; afterHours 0/24 → lỗi | `TestFollowUps_List` | `go test -race -count=1 -run '^TestFollowUps' ./internal/modules/conversation/internal/automation/` | `--- PASS: TestFollowUps_List (0.68s)` |
| CH16 tự gửi một bước: tắt mặc định → 0; bật → đúng 1 lệnh cho A, Meta nhận đúng 1 tin chữ mẫu đã điền tên, tin origin automation sent, lượt done; chạy lại → 0; sau tin tự động 4 giờ → 0; nhân viên gửi tin mới, 3 giờ sau → lượt mới | `TestFollowUps_ScheduleOneStep` | `go test -race -count=1 -run '^TestFollowUps' ./internal/modules/conversation/internal/automation/` | `--- PASS: TestFollowUps_ScheduleOneStep (0.72s)` |
| read_only → skipped_read_only, 0 lệnh; mẫu đã xóa → skipped_policy; hai lịch song song → đúng 1 lượt, 1 lệnh | `TestFollowUps_GatesAndConcurrency` | `go test -race -count=1 -run '^TestFollowUps' ./internal/modules/conversation/internal/automation/` | `--- PASS: TestFollowUps_GatesAndConcurrency (0.72s)` |
| Tin tự động in_doubt → lượt dropped, tin discarded/in_doubt, Meta 1 lần gọi (không gửi lại); Meta từ chối → failed | `TestFollowUps_InDoubtDropped` | `go test -race -count=1 -run '^TestFollowUps' ./internal/modules/conversation/internal/automation/` | `--- PASS: TestFollowUps_InDoubtDropped (0.72s)` |
| Bật follow_up với mẫu không có / của Page khác / rỗng → ErrInvalidRule; tắt → lưu được | `TestFollowUps_SaveRuleChecksTemplate` | `go test -race -count=1 -run '^TestFollowUps' ./internal/modules/conversation/internal/automation/` | `--- PASS: TestFollowUps_SaveRuleChecksTemplate (0.63s)` |
| GET /api/v1/follow-ups: lọc theo Page được đọc, đủ trường, pageId, afterHours 400 | `TestFollowUpsAPI_List` | `go test -race -count=1 -run '^TestFollowUpsAPI' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.301s` |
| PUT follow_up bật với mẫu không có → 422 invalid_input; tắt → 200 | `TestFollowUpsAPI_RuleNeedsTemplate` | `go test -race -count=1 -run '^TestFollowUpsAPI' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.301s` |
| Đăng ký: lệnh command.fb.auto_send hạn 5 phút, lịch conversation.follow_up 300 s theo tenant | `TestWiring`, `TestWireM4` | `go test -count=1 -run '^TestWiring$' ./internal/modules/conversation/`; `go test -count=1 -run 'M4' ./cmd/crm/` | `ok … conversation 0.530s`; `ok … cmd/crm 0.716s` |
| listFollowUps đã làm (không còn 501) | `TestM4Contract_StatusCodes`, `TestM3Routes_PendingList` | `go test -count=1 -run '^(TestM4Contract_StatusCodes\|TestM3Routes_PendingList)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.278s` |
| Không hồi quy đường gửi, quy tắc cũ | cả gói send, automation | `go test -race -count=1 ./internal/modules/conversation/internal/automation/ ./internal/modules/conversation/internal/send/` | `ok … automation 16.935s`; `ok … send 27.691s` |
| Lô lịch không bị hội thoại đã có lượt chiếm: 101 hội thoại đã chạy, hội thoại mới tới hạn vẫn được nhắn (test đỏ khi bỏ lọc rule_id, đã thử) | `TestFollowUps_BatchNotStarved` | `go test -race -count=1 -run '^TestFollowUps' ./internal/modules/conversation/internal/automation/` | `--- PASS: TestFollowUps_BatchNotStarved (2.06s)` |
| CI_RESULT | ci-task (cổng tĩnh + test gói đổi và phụ thuộc; race cho cmd/crm, httpapi, conversation, automation, send) | `scripts/agent/ci-task.sh` | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 28ecf7998fd9…)`; lượt đầu đỏ ở TestM4Contract_StatusCodes, TestM3Routes_PendingList (dòng listFollowUps còn trong m4_pending.txt) → đã xóa dòng. Sau sửa reviewer vòng 1: lượt chạy đỏ ở `TestSendProperty_AtMostOncePerClientRef` ("worker chưa hết việc sau 50 vòng", vòng chờ của test trên máy từ xa; môi trường test gói send không có lệnh mới, tại máy qua 6/6 kể cả -race) → chạy lại: `CI-TASK XANH` run 45806822597 |

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 168 không có e2e; màn hình ở Task 174–178)

## Khác kế hoạch

- Kế hoạch ghi `followup.go` là "tệp khung" nhưng Task 165 chưa tạo: tạo mới.
- Thêm kiểm mẫu khi bật quy tắc follow_up (Quyết định 3) và định nghĩa "nhân viên nói sau cùng" theo tin người thật
  (Quyết định 1).

## Việc phát hiện thêm

- `TestSendProperty_AtMostOncePerClientRef` (gói send, có từ Task 156) chập chờn một lần trên máy từ xa: hết 50 vòng
  chờ worker. Không thuộc phạm vi task; ghi để điều phối viên theo dõi.
- (Ghi chú reviewer, chưa sửa, hiếm) Tin nhắn lại tự động bị in_doubt mà echo của nó chỉ tới qua hỏi bù không có
  metadata (dòng `page_external`, không mã kênh để ghép) sẽ bị coi là tin người thật → có thể thêm một lần nhắn lại
  sau afterHours. Cần xử lý cùng việc ghép echo của Task 157 nếu thí nghiệm E8 cho thấy echo thiếu metadata.

- Task 167 (tin chào) cũng dùng `command.fb.auto_send`: khi gộp với Task 167, giữ một định nghĩa lệnh (bản ở
  `internal/send/auto.go` của task này) và để Task 167 dùng `send.Automated` (có thể thêm nút trả lời nhanh).

## Reviewer

Vòng 1: CẦN SỬA — lịch đọc tối đa 100 ứng viên mỗi Page sắp theo cửa sổ; hội thoại đã có lượt vẫn là ứng viên tới
khi cửa sổ đóng nên chiếm lô, hội thoại mới tới hạn không được nhắn. Đã sửa: `ListFollowUpCandidates` nhận
`rule_id` (lịch) và bỏ hội thoại đã có lượt `fu:<hội thoại>:<tin nhân viên>`; thêm `TestFollowUps_BatchNotStarved`.

Vòng 2: ĐẠT — bản sửa đúng (mã `fu:` dựng trong SQL khớp mã Go; chỉ lịch lọc theo quy tắc, danh sách giữ nguyên;
ON CONFLICT vẫn giữ); `TestFollowUps_BatchNotStarved` bắt được lỗi. Không còn phát hiện phải sửa. Ghi chú không bắt
buộc: ca echo `page_external` hiếm (đã ghi ở Việc phát hiện thêm); mỗi lượt lịch nay chỉ khóa hội thoại thật sự tới hạn.

Kết luận: ĐẠT

## Gộp vào m4

Gộp `task-168` vào `m4` (sau Task 157, 158, 164, 165, 166, 167, 171). Xung đột 9 tệp:

- **Trùng lệnh `command.fb.auto_send`** (`send/auto.go` add/add, `send/message.go`, `send/merge.go`): Task 167 đã gộp
  trước một bản lệnh gửi tự động (`EnqueueAuto`, `InsertAutomatedMessage` origin automation, `BumpConversationAutomated`
  không đổi mốc tin nhân viên, `settleParams(auto)`: in_doubt/expired → discarded `auto_dropped`). Giữ bản của m4 (một
  định nghĩa lệnh, một `AutoCommand`), bỏ các đổi của nhánh ở `insert`, `allowed`, `settle`, `resend.go` và tham số
  `@origin` của `InsertOutgoingMessage` (`send.sql`; `make gen` sinh lại `store/send.sql.go` như m4). Thêm vào
  `send/auto.go` hàm `Automated(ctx, tx, d, conversationID, text, now) (jobID, error)` giữ đúng hợp đồng của nhánh
  (kiểm chữ, khóa hội thoại, hộp thư, Page chỉ xem → ErrReadOnly, SendPolicy với cờ tự động, chế độ thẻ nhân viên →
  *NotAllowedError) rồi gọi `EnqueueAuto`. `followup.go` gọi `send.Automated(ctx, tx, d, …)`.
- Hệ quả hành vi đã thống nhất ở Task 167: tin nhắn lại tự động không rõ đã gửi → `discarded/auto_dropped` (nhánh ghi
  lý do `in_doubt`); sửa kỳ vọng của `TestFollowUps_InDoubtDropped` theo. Tin tự động không còn đổi
  `last_staff_message_at` (câu ứng viên của Task 168 đọc tin theo origin nên không bị ảnh hưởng).
- `automation/automation.go`: `Parts` giữ lệnh ẩn tự động, `send.NewAutoCommand(d, settleRun)`, `send.AutoCommands`
  (Task 166), việc `prepareHandler` (166) và thêm `followUpHandler` cùng lịch `conversation.follow_up` (168); giữ hằng
  `KindAutoSendCommand` (dùng bởi `conversation.AutoSendKind`). `command.go`: giữ `settleRun` của m4 (nhánh đặt tên
  `finishStatus`, cùng thân), mọi chỗ dùng nay là `settleRun`. `rules.go`: giữ kiểm `auto_reply_comment` (166) và thêm
  kiểm `follow_up` (168).
- `tests/arch/testdata/m4_pending.txt`: bỏ `listFollowUps` (nhánh làm thật; bốn thao tác comment m4 đã bỏ ở Task 158).
- `cmd/crm/wire_m4_test.go`: giữ bản m4 (luồng nền Task 171, `wantM4Commands`); lệnh auto_send ghi bằng
  `conversation.AutoSendKind` (Task 167, 168); lịch `conversation.follow_up` đã tự gộp vào `checkM4Schedules`.
- `internal/modules/conversation/wiring_test.go`: giữ danh sách lệnh, việc của m4; `autoSendKind = conversation.AutoSendKind`;
  thêm lịch `conversation.follow_up` mỗi 300 s; bỏ kiểm hạn auto_send trùng.

Bằng chứng: trên máy `make gen` (không chênh mã sinh so m4 ngoài `followups.sql.go` của nhánh), `go build ./...`,
`go vet` các gói `cmd/crm`, `internal/httpapi`, `internal/modules/conversation/...`, `tests/arch` qua.

Reviewer phiên gộp (vòng 1): ĐẠT — không phát hiện phải sửa. `send.Automated` giữ đúng hợp đồng của nhánh (kiểm chữ,
khóa hội thoại ADR-46 nhóm 6, ErrReadOnly, SendPolicy Automated=true, thẻ nhân viên → *NotAllowedError, không ghi gì
trước khi báo lỗi); ca Task 167 không đổi; `Parts`, `settleRun`, `SaveRule`, wiring test, `m4_pending.txt` đúng;
đổi kỳ vọng `discarded/auto_dropped` khớp `settleParams` (hành vi đã thống nhất ở Task 167, không phải nới test).
Ghi chú không bắt buộc: ca echo `page_external` của tin tự động bị bỏ (đã có ở Việc phát hiện thêm).

Merge-test từ xa (cây đã stage so HEAD m4, 23 gói: `cmd/crm`, `internal/httpapi`, `internal/modules/conversation/...`,
`internal/modules/channel/...`, `internal/adapters/facebook/...`, `tests/arch`; `-race`: `conversation`, `automation`,
`send`, `cmd/crm`): run 45810994844 trên blacksmith — plan, static (build, vet, lint, jscpd, kiểm mã sinh…), test (1),
test (2) đều success; `MERGE-TEST XANH (blacksmith)`, 11 phút máy. Xanh ngay lần đầu, không có test chập chờn.
