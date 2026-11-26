# Task 497 — Sửa sau gộp M4 đợt 2: trả lời comment kẹt không rõ đã gửi, mốc tin tự động, echo tin tự động, test chập chờn send/live

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (task sửa sau gộp M4 đợt 2, không có trong bảng; nhánh gốc `m4`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. `send.Resend`, `send.Discard` (2 chỗ gọi:
  `conversation/send.go` `ResendMessage`, `DiscardMessage` → `httpapi/handlers_send.go`) giữ chữ ký, mở rộng hành vi:
  nhận thêm tin kind `comment` (trả lời comment) và `private_reply` (tin riêng) của nhân viên/tự động; tin gửi lại giữ
  `origin` và nút trả lời nhanh của tin cũ; `Discard` trả đúng `Kind`, `ParentMessageID`. Ký hiệu không xuất đổi (mọi chỗ
  gọi trong gói send đã sửa): `allowed` (thêm `automated`), `insert` (nhận `outgoing`), `queueOutgoing` (thêm
  `origin`); mới: `outgoing`, `resendMessage`, `resendAction`, `publishAction`, `privateUsed` (dùng chung cho
  `PrivateReply` và gửi lại tin riêng); inbound: `bumpOutbound` mới trong `echo.go`.
- Gói phụ thuộc ngược (go list): `send` ← `conversation`, `automation`, `cmd/crm`, `internal/httpapi`; `inbound` ←
  `conversation`, `cmd/crm`, `internal/httpapi`; `automation`, `live` (chỉ test đổi). ci-task tự chọn: race automation,
  inbound, live, send; thường cmd/crm, httpapi, httpapi/web, conversation và các gói con, platform/files, realtime.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. `resendMessage`, `discardMessage` nay nhận thêm tin
  comment/tin riêng (hợp đồng ghi "tin", không giới hạn kind); `discardMessage` trả đúng `kind`, `parentMessageId`.
- Chỗ dùng trên màn hình: chưa có (màn hình M4 Task 174–178 chưa làm).
- Câu sqlc, bảng, pha migration: `queries/conversation/send.sql` — `LockRecoverableMessage`,
  `GetRecoverableMessageConversation` (thêm kind comment, private_reply; trả thêm kind, parent_external_id, post_id,
  quick_replies), `InsertOutgoingMessage` (thêm `@origin`, `quick_replies`); `queries/conversation/followups.sql` —
  `ListFollowUpCandidates` (bỏ tin page_external sau tin tự động auto_dropped của cùng lần khách im); echo
  own_echo_unmatched dùng lại `BumpConversationAutomated` (send.sql, Task 167). Bảng `conversation.messages`,
  `conversation.conversations`. Không migration. Mã sinh: `store/send.sql.go`, `store/followups.sql.go` (`make gen`).
- Test sẽ chạy: send `TestRecoverCommentReply_InDoubt`, `TestRecoverPrivateReply_DiscardUnlocks`,
  `TestRecoverPrivateReply_Resend`, `TestAutomatedKeepsStaffMark`, `TestResendGreetingKeepsQuickReplies`,
  `TestSendProperty_AtMostOncePerClientRef` + test cũ của resend/indoubt/private_reply; inbound
  `TestEchoOwnUnmatchedKeepsStaffMark` + `TestEcho*`; automation `TestFollowUps_EchoOfDroppedAutoNotStaff` +
  `TestFollowUps_*`; live `TestTwoWorkersNeverReadSameSession`; httpapi `TestSendRecovery*`; ci-task cả các gói trên;
  lặp ở bể bằng `scripts/ci/merge-test.sh` cho send, live (-race).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì cho gửi
  lại/echo — phần riêng của conversation); trong gói send: `lockRecoverable`, `markDiscarded`, `abandon`, `insertAction`,
  `commentOf`/`parentOf`, `latestPrivate`, `privateState`, `policyFor`, `queueOutgoing`; câu `BumpConversationAutomated`
  (Task 167); `testkit.DrainJobs`, đồng hồ giả `clock.Freeze` của env test send; `eventually` của test live.
- Dùng lại / viết mới vì: gửi lại comment/tin riêng dùng lại `insertAction` + lệnh của Task 158 (không viết lệnh mới);
  mốc nhân viên dùng lại `BumpConversationAutomated`; không viết hàm chung mới.

## Soát thiết kế

- 5.1–5.3: dữ liệu của conversation, chỉ đọc `customer.customers_v` như cũ; không ghi bảng module khác.
- 5.4: gửi lại = bỏ tin cũ + chèn tin mới + bỏ lệnh cũ trong một transaction (WriteTx) như Task 157.
- 5.5–5.6: gửi lại ra Meta chỉ qua lệnh (reply_comment, private_reply, send_message), không gửi lại khi không chắc trừ khi
  nhân viên bấm (MayDuplicate báo cho người).
- 5.7: mọi câu trong tx tenant. 5.8: không đụng HTTP ngoài handler sẵn có. 5.9: hai thao tác đã có khóa chống trùng và
  `x-permission` (Task 157). 5.10: không Valkey. 5.11: không thư viện mới. 5.12: theo mẫu Task 157/158.
- 6.1: tin cũ đổi bằng `MarkMessageDiscarded` có điều kiện trạng thái đã đọc dưới khóa; tin riêng: unique
  `messages_one_live_private_reply` chặn hai tin riêng sống → `ErrPrivateReplyUsed`.
- 6.2: khóa chống trùng của API; bấm lại sau khi đã gửi lại → tin cũ discarded → ErrNotRecoverable (409).
- 6.3: thứ tự khóa ADR-46 nhóm 6: hội thoại của tin → tin; tin riêng: không khóa hội thoại comment (chỉ đọc comment
  cha, Execute hỏi lại chính sách); lệnh cũ bỏ sau mọi khóa dòng (nhóm 13).
- 6.4–6.5: WriteTx sẵn có. 6.6–6.9: lệnh như Task 158. 6.10: không tiền.
- 6.11: quy luật "một client_ref tới kênh tối đa 1 lần" (rapid) giữ nguyên câu kiểm, chỉ bỏ phụ thuộc giờ thật.
- 6.12: không log nội dung, PSID. 6.13: gửi lại/bỏ comment, tin riêng đòi `conversation.reply` trên Page của hội thoại chứa
  tin (như tin thường).

## Quyết định thay chủ shop

1. **Gửi lại câu trả lời comment, tin riêng dùng lệnh của nhân viên** (`command.fb.reply_comment`,
   `command.fb.private_reply`), kể cả khi tin cũ là tin tự động (Task 166): người đã bấm, nên lần này không rõ đã gửi thì
   lại theo luồng "người quyết" của Task 157 (không bỏ êm như lệnh tự động). Tin mới giữ `origin` của tin cũ (tự động vẫn
   là tự động: không đổi mốc nhân viên, không thành "tin người thật" cho Task 168) và ghi `sender_staff_id` = người bấm.
   Phương án khác: dùng lại lệnh tự động — in_doubt lần hai sẽ bị bỏ mà không ai biết.
2. **Bỏ tin riêng (`discarded`) mở lại nhắn riêng** (câu `LatestPrivateReply`, unique `messages_one_live_private_reply`
   đã không tính discarded). Nếu thật ra Meta đã nhận: echo có metadata tới sau đặt lại tin `sent` (sự thật thắng, Task
   157); không có echo thì lần nhắn riêng mới bị Meta từ chối (10900) → `failed private_reply_used`, lại khóa — không gửi
   trùng tới khách. Gửi lại tin riêng in_doubt trả `mayDuplicate = true` (thận trọng, dù Meta chặn tin riêng thứ hai).
3. **Gửi lại khi comment cha đã xóa hay không còn trong CRM** → 409 `send_not_allowed` `comment_removed` (như gửi mới).
4. **Tin chào gửi lại giữ `origin = automation` và nút trả lời nhanh** (Việc 4): cột `quick_replies` chép từ tin cũ,
   SendPolicy hỏi như tin tự động (không thẻ nhân viên), lệnh `command.fb.send_message` (đọc nút từ cột như Task 167).
   Phương án khác (spec không nói): thành tin nhân viên như trước — làm Task 168 coi tin chào là "nhân viên đã trả lời".
5. **Echo không khớp của CRM (`own_echo_unmatched`) không đổi mốc nhân viên** (Việc 2, 3): đó là tin của chính CRM, tin
   nhân viên đã đặt mốc lúc chèn; tin tự động không đặt. Chỉ `page_external` (người gửi ngoài CRM) đặt mốc.
6. **Echo hỏi bù (`page_external`, không mã CRM, không mid để ghép) sau một tin tự động bị bỏ vì không rõ
   (`auto_dropped`) trong cùng lần khách im không được tính là "tin người thật" cho nhắn lại** (Việc 3). Không ghép theo
   nội dung (Task 157 Quyết định 5 cấm đoán theo nội dung); dòng page_external vẫn hiện trong hội thoại như tin Page gửi
   ngoài CRM. Cái giá: nhân viên trả lời từ Business Suite đúng sau tin tự động bị bỏ, khách vẫn im, thì không được nhắn
   lại tự động (thiếu một tin, không thừa — đúng hướng an toàn của Q11 "một bước"). Phương án khác: chặn mọi nhắn lại thứ
   hai trong một lần khách im — loại vì đổi hành vi đã duyệt của Task 168 (`TestFollowUps_ScheduleOneStep`: tin nhân
   viên mới thì nhắn lại lần nữa). Echo có metadata đã ghép sẵn (ConfirmOwnEcho đặt tin tự động `sent`, không thêm dòng).


## Bằng chứng

Trên máy (đỏ trước khi sửa mã, xanh sau): `go test -race -count=1 -run '^(…)$' ./internal/modules/conversation/internal/<gói>/`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Việc 1: câu trả lời comment in_doubt gửi lại được (kind comment, cùng comment cha, mayDuplicate, Meta lần 2, tin mới sent) và bỏ được (trả kind comment, comment cha) | `send.TestRecoverCommentReply_InDoubt` | lệnh trên, gói send | trước: `Resend: conversation: không có`; sau: `ok` |
| Việc 1: tin riêng in_doubt bỏ được, báo hiệu hội thoại comment, nhắn riêng mở lại (available) và nhắn riêng lại được | `send.TestRecoverPrivateReply_DiscardUnlocks` | như trên | trước: `bỏ tin riêng: … conversation: không có`; sau: `ok` |
| Việc 1: tin riêng in_doubt gửi lại: tin riêng mới cùng hộp thư, cùng comment cha, Meta lần 2, trạng thái nhắn riêng trỏ tin mới; gửi lại tin đã bỏ → lỗi | `send.TestRecoverPrivateReply_Resend` | như trên | trước: `Resend: conversation: không có`; sau: `ok` |
| Việc 2: tin chào, nhắn lại (EnqueueAuto, Automated), tự trả lời comment, tự nhắn riêng không đổi `last_staff_message_at`; tin nhân viên thì đổi | `send.TestAutomatedKeepsStaffMark` | như trên | trước: `comment …: mốc nhân viên 2026-11-26 18:45:59…; muốn nil` (và tin riêng); sau: `ok` |
| Việc 3: echo own_echo_unmatched không đổi mốc nhân viên (Business Suite vẫn đổi: `TestEchoFromBusinessSuite`) | `inbound.TestEchoOwnUnmatchedKeepsStaffMark` | như trên, gói inbound | trước: `LastStaffAt:2026-11-20 10:00:00`; sau: `ok` (cả `TestEcho*`) |
| Việc 3: echo không mã CRM của tin nhắn lại bị bỏ (auto_dropped) không làm 168 nhắn thêm; đối chứng page_external thường vẫn được nhắn lại | `automation.TestFollowUps_EchoOfDroppedAutoNotStaff` | như trên, gói automation | trước: `sau echo không mã: 2 lệnh, muốn 1`; sau: `ok` (cả `TestFollowUps_*`) |
| Việc 4: gửi lại tin chào failed giữ origin automation, nút trả lời nhanh (cột và thân gửi Meta có 2 `quick_replies`), không đổi mốc nhân viên | `send.TestResendGreetingKeepsQuickReplies` | như trên | trước: `Origin:staff`; sau: `ok` |
| Không phá luồng cũ: gửi lại/bỏ tin thường, CH18, CH19, gửi đồng thời, tin riêng một lần, API | `send.TestInDoubt_CH18`, `TestRecover_States`, `TestExpired_CH19`, `TestResend_Concurrent`, `TestDiscard_LateEcho`, `TestPrivateReply_*`; `httpapi.TestSendRecovery*` | như trên | `ok` |
| Việc 5 (send): gốc là thùng lượt gửi cạn theo giờ thật | `send.TestSendProperty_AtMostOncePerClientRef` + câu đo tạm (không commit) đọc `min(tokens)` của `channel.send_buckets` sau mỗi tin | `go test -count=1 -v -run '^TestSendProperty_AtMostOncePerClientRef$'` | bản cũ `PROBE min tokens: 0.00108`; bản mới `PROBE min tokens: 48`, `[rapid] OK, passed 100 tests` |
| Việc 5 (live) trên máy | `live.TestTwoWorkersNeverReadSameSession` | `go test -count=3 -race -run '^TestTwoWorkersNeverReadSameSession$' ./…/live/` | `ok … 8.679s` |
| Việc 5: chạy lặp ở bể (cả gói send, live, -race) | toàn bộ test của send, live | `CRM_CI_STATIC=false CRM_CI_LABEL=t497-repN scripts/ci/merge-test.sh bc24ba07… HEAD <send,live> <send,live>` ×3 (blacksmith) và ×2 `CRM_CI_RUNNER=github` | run 45816401319, 45816409349, 45816422895 (blacksmith), 45816773815, 45816784037 (github, 7 phút 47–49 giây/lượt): cả 5 `MERGE-TEST XANH` |
| Cổng tĩnh + test gói đổi và phụ thuộc | ci-task | `scripts/agent/ci-task.sh` | run 45815670037: static, test (1), test (2) success; `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách 4e8cd0e5)` |
| Tự soát rẻ | jscpd, vet, lint, arch | `make jscpd`; `go vet`, `golangci-lint run ./internal/modules/conversation/...`; `go test -count=1 ./tests/arch/` | `Found 0 clones`; `0 issues`; `ok … tests/arch 204s` |

`git merge m4`: `Already up to date` (m4 = điểm tách 4e8cd0e5).

## Test bị xóa hoặc tắt

Không xóa, không tắt test nào; không bỏ câu kiểm nào. Đổi để bỏ phụ thuộc giờ thật (Việc 5):

- `send.TestSendProperty_AtMostOncePerClientRef`: đồng hồ của lệnh gửi là đồng hồ giả, tiến 1 s mỗi tin. Gốc: thùng lượt
  gửi của Page (50, nạp 20/giây theo đồng hồ của lệnh) cạn khi máy gửi hơn 20 tin/giây; đo trên máy: bản cũ thùng xuống
  0,001 lượt, bản mới thấp nhất 48 lượt. Câu kiểm quy luật giữ nguyên.
- `live.TestTwoWorkersNeverReadSameSession`: khóa 300 ms → 2 s, gia hạn 80 → 100 ms (hạn cục bộ 1,95 s chịu được lần
  gia hạn chậm); câu "mỗi phiên đúng một luồng, chủ là w1/w2" chờ theo điều kiện (eventually) trước và sau cửa sổ quan
  sát khoảng 50 lần quét, thay cho một lần chụp sau 2,5 s; trần chờ "luồng dừng khi mất khóa", "nhận lại" 3 s/5 s →
  10 s (chỉ chống treo, không phải tiêu chí). Câu kiểm không chồng nhau, live_lease_lost ≥ 1 giữ nguyên.
- `automation.TestFollowUps_InDoubtDropped`: phần đầu chuyển vào helper `droppedFollowUp` (cùng câu kiểm) để dùng chung
  với test mới (jscpd).

## Khác kế hoạch

- Dọn cấu trúc nhỏ trong cùng commit để qua jscpd (không đổi hành vi): `privateUsed` dùng chung ánh xạ
  "đã nhắn riêng" của `PrivateReply` và gửi lại tin riêng; helper test `bumpStaff`, `droppedFollowUp`
  (automation), `privateInDoubt`, `fbAccount` (send).

## Việc phát hiện thêm

- `resendable` không nhận tin `discarded`, nên ghi chú của Task 167 ("nhân viên vẫn gửi lại được tin đã bỏ") không đúng
  với mã: tin tự động bị bỏ (`auto_dropped`) không gửi lại được bằng nút. Không đổi ở đây (spec CH18/CH19 chỉ nói in_doubt,
  expired, failed); màn hình (Task 175) cần biết.
- Gửi lại câu trả lời comment/tin riêng của lệnh tự động: lượt quy tắc (automation_runs) vẫn trỏ lệnh cũ (`dropped`),
  không cập nhật theo lần gửi lại của nhân viên (đúng: lượt là việc của quy tắc, lần sau là của người).

## E2E viết sẵn, chưa chạy

Không có (kế hoạch không có e2e cho việc sửa này).

## Reviewer

Kết luận: ĐẠT (lượt 1, trên phần đã stage, 15 tệp). Không có phát hiện phải sửa. Reviewer đối chiếu đủ chỗ gọi của
`allowed`, `insert`, `queueOutgoing`; mã sinh khớp SQL; mỗi dòng bằng chứng trỏ test có thật và kiểm đúng tiêu chí; đổi
ở test live/send/automation khớp mục "Test bị xóa hoặc tắt". Ghi chú không bắt buộc (không sửa): `Discard` tin riêng mà
comment cha không còn trong CRM gửi thêm một báo hiệu thừa cho mã hội thoại rỗng (vô hại); gửi lại tin riêng không khóa
hội thoại comment, đúng nhờ unique `messages_one_live_private_reply`; cái giá của Quyết định 6 chủ shop nên biết.
