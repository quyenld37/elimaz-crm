# Task 167 — Tin chào có nút trả lời nhanh

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 167, M4-27). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ ký hiệu trong gói nội bộ của module `conversation`
  (không module nào khác thấy được).
  - `internal/automation` (mới): `OnInboxMessage`, `TriggerInboxMessage`, `GreetingConfig`, `GreetingQuickReply`;
    `Parts` thêm lệnh `command.fb.auto_send` (1 chỗ gọi: `conversation/wiring.go`); `validConfig` kiểm thêm cấu hình
    `greeting` (gọi từ `SaveRule`, 1 chỗ: `conversation/automation.go`). `hideCommand.OnTerminal` dùng chung hàm đổi
    trạng thái lệnh → kết quả lượt (`settleRun`, thêm nhánh done), không đổi hành vi.
  - `internal/send` (mới): `KindAutoSend`, `AutoCommandKind`, `AutoInput`, `EnqueueAuto`, `NewAutoCommand` (kiểu
    `AutoCommand`, `Settled`: Execute, OnDone dùng chung `sendCommand`; OnTerminal riêng: in_doubt, expired →
    `discarded`, lý do mới `auto_dropped` có câu tiếng Việt trong `failTexts`). Sửa `sendCommand.prepare` đọc thêm
    `quick_replies` của tin (tin nhân viên luôn NULL → không đổi; hàm `quickReplies`). `sendCommand.OnTerminal` tách
    thân thành `settle(…, auto bool)` + `settleParams` (gocognit), hành vi tin nhân viên giữ nguyên.
  - `internal/inbound`: `onInboxMessage` (không xuất, 1 chỗ gọi `messages.go`) đổi chữ ký để truyền `deps`, sự kiện,
    Page, payload, mã tin; `handler.inbox` bỏ giá trị trả `inserted` (không ai dùng nữa; `thread` vẫn trả), `ok` luôn
    false khi có lỗi; `referral` chỉ đổi cách viết (jscpd).
  - `internal/query`: `Message.QuickReplies` (mới, trường thêm) → `conversation.ChatMessage` (bí danh) →
    `httpapi.chatHistoryOut` (1 chỗ) đặt `quickReplies` của `ChatMessage` (Task 159 ghi chưa trả).
  - Test: `automation/env_test.go` thêm `storeRule`, `setSendMode` (dùng lại ở `autohide_test.go`,
    `greeting_test.go`); `conversation/wiring_test.go` (`checkCommands`) và `cmd/crm/wire_m4_test.go` thêm
    `command.fb.auto_send`.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/conversation` và các gói con
  `automation`, `inbound`, `live`, `query`, `send`; `internal/platform/files` (test), `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. `GreetingConfig` (text 1–640, quickReplies ≤ 13, tiêu đề
  1–20, payload 1–1 000) và `ChatMessage.quickReplies` đã có từ Task 143; `putAutomationRule` dùng sẵn; `listMessages`
  bắt đầu điền `quickReplies` (trường tùy chọn đã khai) → oasdiff không đổi.
- Chỗ dùng trên màn hình: chưa có (màn cài đặt Task 178, màn hội thoại Task 175 chưa làm).
- Câu sqlc, bảng, pha migration: không migration (cột `messages.quick_replies` có từ 0402, `automation_rules`/`runs` từ
  0407). Câu mới: `InsertAutomatedMessage`, `BumpConversationAutomated` (send.sql), `GreetingEligible` (automation.sql); câu sửa:
  `GetOutgoingMessage` (+`quick_replies`), `ListChatMessagesBefore`/`After` (+`quick_replies`).
- Test sẽ chạy: `automation` (-race: `TestGreeting_*`, `TestAutoHide_*`, `TestRun_*`), `send` (-race cả gói), `inbound`,
  `query` (sửa mã), `conversation` (`TestWiring*`), `cmd/crm` (`TestWireM4_Frame`), `httpapi` (`TestGreetingRuleAPI`),
  `tests/arch` — cả gói đổi và phụ thuộc ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (gói platform: không có
  gì về gửi tin); tìm "quick", "auto_send", "Automated", "origin automation": có `channel.QuickReply`, adapter Facebook
  đã gửi `quick_replies` (`messageBody`), cột `messages.quick_replies`, `templates.quickRepliesJSON` (riêng của mẫu, dạng
  lưu cột của bảng mẫu), lõi `automation.Run` (cổng chung Task 165), lệnh gửi `send.sendCommand` (Task 156/157), khớp
  echo theo `client_ref` (`inbound.confirmOwn`, không phân biệt origin).
- Dùng lại / viết mới vì: dùng lại `automation.Run` (không viết cổng riêng), `LoadRule`, lệnh gửi của `send` (prepare,
  SendPolicy với `Automated`, lượt gửi của Page, OnDone gộp echo) — lệnh `command.fb.auto_send` chỉ khác Kind (hạn 5
  phút), không có Looker và OnTerminal (không rõ / hết hạn → bỏ, ghi kết quả lượt). Viết mới: câu chèn tin origin
  automation (câu cũ cố định `staff`), câu kiểm "khách lần đầu nhắn Page", cấu hình tin chào. Lệnh gửi tự động đặt ở
  `send` để Task 168 (`follow_up`, cùng lệnh `command.fb.auto_send`) dùng lại; kết quả lượt truyền vào bằng hàm (send
  không nhập automation). Dạng lưu nút (`send.quickReply`, `query.storedQuickReply`) giữ riêng từng gói như
  `storedAttachment`/`Attachment` hiện có (gói con không nhập nhau). jscpd: 3 đoạn trùng mới đã gỡ (helper test
  `storeRule`/`setSendMode`, viết lại `referral`); `make jscpd` 0 mới.

## Soát thiết kế

5.1 Chủ dữ liệu: tin, lượt chạy thuộc `conversation`. 5.2 Không ghi bảng module khác. 5.3 Đọc `channel.Account`,
`customer.IsDoNotContact` (trong `Run`) qua API công khai. 5.4 Nguyên tử: tin nhận, lượt chạy, tin chào queued, lệnh
`command.fb.auto_send` cùng một tx của sự kiện vào; kết quả lệnh (tin sent/failed/discarded, lượt done/failed/dropped)
cùng tx của Runner. 5.5 Gọi Meta là lệnh, ngoài tx. 5.6 Lệnh ra ngoài: hạn 5 phút, 30 s mỗi lần, tự thử lại khi chắc
chưa gửi; không rõ → in_doubt → tin `discarded` + lượt `dropped`, không Looker, không tự gửi lại (04 mục 5.3 "bỏ, ghi
nhật ký"). 5.7 Tenant: mọi câu trong tx tenant của sự kiện; câu mới dùng `current_setting('app.tenant_id')`. 5.8 HTTP:
không thêm handler (PUT có sẵn); `chatHistoryOut` chỉ đổi kiểu. 5.9 Không đổi hợp đồng. 5.10 Không Valkey. 5.11 Không
thêm thư viện. 5.12 Theo mẫu `autohide.go` (quy tắc gọi `Run`) và `send/message.go` (chèn tin + lệnh + báo hiệu).
6.1 Đồng thời: hai sự kiện của cùng hội thoại xếp theo khóa hội thoại (upsert khóa dòng); chống chạy hai lần bằng unique
`automation_runs (tenant_id, rule_id, external_ref = "greet:<conversation_id>")` và `DedupeKey` của lệnh = mã tin.
6.2 Xử lý lại tin đầu: tin trùng mã ngoài dừng trước hook; nếu tới hook thì `Run` → `already_run`. 6.3 Khóa: hội thoại
(đã khóa bởi upsert ở đường vào) → messages (chèn) → automation_runs (chèn) → messages (chèn tin chào, dòng mới) →
hội thoại (cập nhật dòng đã khóa); Runner: hội thoại → tin → automation_runs (ADR-46 nhóm 6). 6.4/6.5 Mặc định nền tảng.
6.6 Lệnh lỗi hết lượt → dead → tin failed, lượt failed. 6.7 429 → not_sent có RetryAfter; thùng lượt gửi của Page. 6.8
Chết giữa chừng → in_doubt → tin discarded, lượt dropped. 6.9 30 s mỗi lần. 6.10 Không tiền. 6.11 Một lần mỗi hội thoại
(xử lý lại, tin thứ hai) có test; thuộc tính chung "≤ 1 lệnh mỗi (quy tắc, sự kiện)" đã có ở `TestProperty_AtMostOneCommand`
(cùng `Run`). 6.12 Dữ liệu cá nhân: payload lệnh chỉ mã tin; không log nội dung, PSID; `subject_ref` = mã hội thoại.
6.13 Quyền: PUT `conversation.settings` (có sẵn); tin tự động không có người gửi (`sender_staff_id` NULL).

## Quyết định thay chủ shop

1. **"Lần đầu khách nhắn Page" = ngoài tin vừa nhận, hội thoại chỉ có tin hệ thống, và khách không có hội thoại hộp thư
   khác với Page** (câu `GreetingEligible`), không dùng cờ "hội thoại vừa tạo" của upsert như kế hoạch ghi. Vì: sự kiện
   referral (khách bấm quảng cáo / liên kết m.me) tạo hội thoại trước tin đầu (chỉ chèn tin hệ thống), nên theo cờ
   "vừa tạo" khách từ quảng cáo — nhóm cần chào nhất — không bao giờ được chào; hội thoại do Page mở trước (nhân viên
   nhắn, tin riêng từ comment, echo từ Business Suite) có tin khác hệ thống → không chào, đúng Q11. Một lần mỗi hội thoại
   vẫn do unique `greet:<conversation_id>`. Phương án khác: đúng chữ kế hoạch (chỉ `inserted`). Đảo ngược: thêm điều
   kiện `inserted` vào `OnInboxMessage`.
2. **Lệnh `command.fb.auto_send` nằm ở gói `send`** (dùng chung prepare/Execute/OnDone của lệnh gửi nhân viên), kết quả
   lượt báo về automation qua hàm `Settled`; Task 168 dùng lại. Không có Looker; không rõ (`in_doubt`) hay quá 5 phút
   (`expired`) → tin `discarded` lý do `auto_dropped`, lượt `dropped` (04 mục 5.3 "bỏ, ghi nhật ký"): không vào lịch
   "không rõ đã gửi" của Task 157, không hỏi nhân viên. Nhân viên vẫn có thể bấm "gửi lại" tin đã bỏ như mọi tin
   discarded (quyết định của người, không phải tự gửi). Phương án khác: lệnh riêng trong `automation` (chép lại khung
   gửi như `hideCommand`) — trùng mã nhiều.
3. **Chi tiết hội thoại trả `quickReplies` của tin** (Task 159 ghi chưa trả): trường đã có trong hợp đồng `ChatMessage`;
   để nhân viên thấy tin chào kèm nút nào. Không đổi `openapi.yaml`.
4. **Kiểm giới hạn cấu hình tin chào cả ở module** (`SaveRule` → `ErrInvalidRule`, 422) ngoài hợp đồng (400), cho bên
   gọi khác HTTP; cấu hình hỏng đọc lúc chạy → coi như tắt.
5. **Tin tự động không đổi `last_staff_message_at`** (câu mới `BumpConversationAutomated`; chỉ mốc tin cuối và đoạn
   trích). Vì: cột là "tin nhân viên"; nếu tin chào đặt mốc này, hội thoại khách mới trông như nhân viên đã trả lời và
   danh sách "khách chưa trả lời" của Task 168 (`last_staff_message_at > last_customer_message_at`) sẽ nhắn lại khách
   chỉ vì tin chào. Phương án khác: dùng chung `BumpConversationOutbound` như tin Page gửi ngoài CRM.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| PSID mới nhắn lần đầu → 1 lệnh `command.fb.auto_send`; Meta nhận 2 nút đúng tiêu đề/payload, `messaging_type = RESPONSE`; tin CRM origin automation, sent, có `quick_replies`, không người gửi; hội thoại: đoạn trích là tin chào, `last_staff_message_at` vẫn rỗng (Quyết định 5); lượt done; tin thứ hai, xử lý lại tin đầu (hỏi bù) → không thêm (CH12) | `TestGreeting_FirstMessageOnce` | `go test -race -count=1 -run '^(TestGreeting_.*\|TestAutoHide_.*\|TestRun_.*)$' ./internal/modules/conversation/internal/automation/` | `ok … automation 4.747s` |
| Khách đã có hội thoại hộp thư cũ (done) nhắn lại → không chào; quy tắc tắt → không lượt | `TestGreeting_ReturningCustomerNotGreeted` | như trên | ok |
| Khách bấm "Tư vấn size" → tin vào `quick_reply_payload = "SIZE"`, `body = "Tư vấn size"`; không chào lại | `TestGreeting_QuickReplyPressed` | như trên | ok |
| Sau referral (chỉ tin hệ thống) vẫn chào; `do_not_contact` → `skipped_dnc`, 0 lệnh (Quyết định 1) | `TestGreeting_AfterReferralAndDoNotContact` | như trên | ok |
| `read_only` → `skipped_read_only`, 0 lệnh; tin lịch sử → không dòng | `TestGreeting_ReadOnlyAndHistory` | như trên | ok |
| Không rõ đã gửi → tin `discarded`, lượt `dropped`, lệnh in_doubt, không gửi lại | `TestGreeting_InDoubtDropped` | như trên | ok |
| 14 nút, tiêu đề 21 ký tự, tiêu đề/payload/chữ rỗng, payload 1 001, chữ 641 → `ErrInvalidRule`; 13 nút, tiêu đề 20; không nút → lưu được | `TestGreeting_ConfigLimits` | như trên | ok |
| `PUT` 14 nút → 400; tiêu đề 21 ký tự → 400; 2 nút → 200 v1 | `TestGreetingRuleAPI` | `go test -count=1 -run '^(TestGreetingRuleAPI)$' ./internal/httpapi/` | `ok … httpapi 4.271s` (qua ngay từ đầu: hợp đồng `GreetingConfig` có từ Task 143) |
| Chi tiết hội thoại trả `quickReplies` đúng thứ tự; tin không nút → rỗng | `TestMessages_QuickReplies` (+ `TestMessages_*` cũ) | `go test -race -count=1 -run '^(TestMessages_.*)$' ./internal/modules/conversation/internal/query/` | `ok … query 2.153s` (trước khi viết mã: build failed, thiếu trường) |
| Đường vào không đổi hành vi (tin, comment, referral) | `TestInbound*`, `TestComment*` | `go test -race -count=1 -run '^(TestInbound.*\|TestComment.*)$' ./internal/modules/conversation/internal/inbound/` | `ok … inbound 6.202s` |
| Lệnh gửi của nhân viên không đổi (OnTerminal tách hàm) | cả gói `send` | `go test -race -count=1 ./internal/modules/conversation/internal/send/` | `ok … send 26.664s` |
| Nối dây: `command.fb.auto_send` hạn 5 phút, 30 s, có trong danh sách lệnh | `TestWiring*`, `TestWireM4_Frame` | `go test -count=1 -run '^(TestWiring.*\|Test.*Wir.*)$' ./internal/modules/conversation/ ./cmd/crm/` | ok, ok |
| Test kiến trúc | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok … tests/arch 177.778s` |
| Kiểm test có bắt lỗi: tạm bỏ điều kiện `Eligible`; tạm cho lệnh tự động OnTerminal như lệnh nhân viên | `TestGreeting_ReturningCustomerNotGreeted`, `TestGreeting_InDoubtDropped` | như dòng 1 (không -race) | FAIL đúng chỗ (`khách cũ: lượt "done", 1 dòng, 1 lệnh`; `tin chào: {… Status:checking …}`), đã trả mã |
| Cổng kiểm của task | gói đổi + phụ thuộc (-race gói sửa) | ci-task (bản m4, chạy từ xa) | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 28ecf7998fd9…)`, lần 1 run 45801036700; lần 2 (sau Quyết định 5) run 45802370955: plan, static, test (1), test (2) success, `CI-TASK XANH` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

1. Điều kiện chào (Quyết định 1).
2. Ngoài hai tệp kế hoạch ghi (`greeting.go`, `greeting_test.go`): lệnh gửi tự động ở `send/auto.go`, câu SQL
   (`send.sql`, `automation.sql`, `read.sql`), hook đường vào, đọc `quickReplies` (Quyết định 2, 3).
3. Ca "14 nút / 21 ký tự → PUT 400" đã qua trước khi viết mã (hợp đồng Task 143); thêm ca module 422.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 167 không có test e2e)

## Việc phát hiện thêm

- Khi mở Page thật (06 mục 2 bước 5), mọi khách cũ nhắn lần đầu sau khi CRM kết nối đều là "hội thoại mới" trong CRM
  (CRM chưa có lịch sử) → được chào dù đã từng nhắn Page. Giảm bớt: chạy chế độ chỉ xem trước (hội thoại được tạo, quy
  tắc chưa gửi), quy tắc tắt mặc định. Sửa tận gốc cần biết khách có lịch sử trên Meta (Task 164 lấy 20 tin gần nhất
  chạy sau, bằng lệnh): đề xuất Task 164 hoặc 178 cân nhắc hỏi Graph trước khi chào, hay chỉ bật tin chào sau khi đã
  chạy chỉ xem đủ lâu.
- (Reviewer, cho Task 168) Quyết định 5 còn một đường hiếm: echo của tin chào tới không có metadata (dòng
  `own_echo_unmatched`) hay qua hỏi bù (`page_external`) trước `OnDone` → `inbound/echo.go` gọi
  `BumpConversationOutbound`, đặt `last_staff_message_at` theo tin chào; `OnDone` gộp dòng echo nhưng không trả mốc.
  Task 168 dựa vào mốc này nên cần xét (vd. chỉ đếm tin `origin = staff`).
- (Reviewer) Chỉ số `send_click_to_api_ms` đang ghi cả cho lệnh gửi tự động (không có thao tác bấm): lẫn số đo của
  nhân viên; nên tách nhãn hay bỏ ghi cho `command.fb.auto_send` ở một task sau.
- Gói `queue` không có hằng `StatusDone` cho TerminalHook; `send.Settled` nhận `commands.StatusDone` cho kết quả done.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên phần đã stage, 26 tệp). Phát hiện phải sửa: không có. Reviewer đã soát
`GreetingEligible` (customer_id NOT NULL, hội thoại done mở lại), hai sự kiện đồng thời (OrderKey + khóa upsert + unique
`greet:` + DedupeKey), `settle`/`settleParams` giữ hành vi tin nhân viên (ReportTokenInvalid trước lock), `validConfig`,
Quyết định 1–5 đúng Q11 và 04 mục 5.3. Ghi chú không bắt buộc: (1) đường echo hiếm làm lệch Quyết định 5 → ghi vào
Việc phát hiện thêm cho Task 168; (2) nhánh "khách có hội thoại hộp thư khác" không có test riêng (chỉ xảy ra khi gộp
khách / PSID khác vì unique thread key) — giữ; (3) chỉ số click_to_api ghi cả lệnh tự động → Việc phát hiện thêm;
`TestGreeting_InDoubtDropped` chỉ kiểm `FailReason` khác rỗng — chấp nhận.
