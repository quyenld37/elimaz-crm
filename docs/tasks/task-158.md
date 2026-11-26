# Task 158 — Trả lời comment, nhắn riêng một lần, ẩn và hiện comment

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 158 / M4-18). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký nào đã có.
  - `customer.Writer.AttachChannelIdentity` (mới, `channel_identity.go`); `validChannelIdentity` (không xuất) tách phần
    kiểm khóa thành `validChannelKey` — 2 chỗ gọi trong gói, hành vi giữ nguyên.
  - `conversation` (`send.go`): thêm `ReplyToComment`, `SendPrivateReply`, `SetCommentHidden`, `PrivateReplyStatus`,
    `ErrPrivateReplyUsed`, `ErrNotCustomerComment`, `ReplyCommentKind`, `PrivateReplyKind`, `HideCommentKind`,
    `CommentInput`, `Comment`, `PrivateReplyState`, `PrivateReplyAvailable/Used/Expired`.
  - `send` (nội bộ): thêm `ReplyToComment`, `PrivateReply` (tên không lặp gói, revive), `SetCommentHidden`,
    `PrivateReplyStatus`, `Comment`, `CommentInput`, `PrivateReplyState`, ba `Kind*`/`*Kind()`, hai sentinel; `Message`
    thêm `Kind`, `ParentMessageID` (2 chỗ dựng: `insert` của send, `chatMessageOut` của httpapi — nay dùng `m.Kind`
    thay hằng `message`, thêm `parentMessageId`).
  - `send.Parts` thêm 3 lệnh: `CommandHandlers` của conversation (1 chỗ: `cmd/crm/wire_m4.go`) và hai test đếm lệnh
    (`conversation/wiring_test.go`, `cmd/crm/wire_m4_test.go`).
  - Nội bộ `send` (dời mã, hành vi giữ nguyên, phủ bởi test có sẵn của Task 156): `sendCommand.Execute` → `run(prepareFunc)`;
    `prepare` đọc kênh qua `channelFor`; `allowed` qua `policyFor`; `insert` ghi lệnh qua `queueOutgoing`;
    `sendCommand.OnDone` → `done`/`begin`/`settleSent`/`markSent`; `payload` thêm `Hidden` (omitempty); `result` thêm
    `RecipientID` (chỉ lệnh nhắn riêng ghi); `failTexts` thêm 2 câu (`private_reply_used`, `private_reply_expired`),
    câu của `expired` sửa thành "Quá hạn chờ (tin 3 phút, trả lời comment và nhắn riêng 5 phút)…" (dùng chung OnTerminal).
  - Test helper của gói send: `env_test.go` thêm `inTx`, `resolve`; `send` viết lại qua `inTx`.
- Gói phụ thuộc ngược (go list): `customer` ← cmd/crm, httpapi, httpapi/web, conversation (+ convtest, calls, inbound,
  templates), order, promotion, testkit/shiptest; `conversation/internal/send` ← conversation; `conversation` ←
  cmd/crm, httpapi.
- Operation OpenAPI bị chạm, oasdiff: hiện thực `replyToComment`, `sendPrivateReply`, `hideComment`, `unhideComment`
  (hợp đồng có sẵn từ Task 143, không sửa `openapi.yaml`, oasdiff không đổi); xóa 4 dòng ở
  `tests/arch/testdata/m4_pending.txt`. Mã lỗi `private_reply_used` đã khai ở `x-error-codes`; thêm vào
  `errors_conversation.go` (cùng `ErrNotCustomerComment` → 422 `invalid_input`) và `errors_snapshot_test.go`.
- Tài liệu: `docs/adr/0046-thu-tu-khoa.md` mục khóa tư vấn danh tính kênh thêm đường `AttachChannelIdentity`.
- Chỗ dùng trên màn hình: chưa có (màn hình M4 là Task 174–178); `console/src/api` không đổi.
- Câu sqlc, bảng, pha migration: tệp mới `queries/conversation/comment_actions.sql` (14 câu: `GetCommentForAction`,
  `GetCommentByExternalID`, `LockCommentByID`, `LatestPrivateReply`, `GetConversationThread`, `FindInboxForCommenter`,
  `InsertOutgoingAction`, `GetOutgoingAction`, `SetMergeReplyFlag`, `DeletePageReplyEcho`, `SetOwnExternalID`,
  `RekeyInboxConversation`, `InsertSystemNote`, `CountCommentEdits`) trên `conversation.messages`,
  `conversation.conversations`, `message_revisions`; dùng lại câu có sẵn `InsertRevisionState`, `UpdateCommentState`,
  `UpsertInboxConversation`, `LockConversationForSend`, `LockOutgoingMessage`, `MarkOwnMessageSent`,
  `DeleteUnmatchedEcho`, `BumpConversationOutbound`, `SetMessageCommandJob`. Migration `0415_comment_reply_merge.sql`
  (0414 đã thuộc Task 164), pha **mở rộng**: chỉ thay thân hàm trigger `messages_delete_guard` (nới: cho xóa comment
  `page_external` khi tx đặt `app.merge_reply = on`; giữ nguyên hai điều kiện của 0413).
- Test sẽ chạy: `go test -race` gói `conversation/internal/send` (cả gói, gói nhỏ: test mới `comment_reply_test.go`,
  `private_reply_test.go`, `hide_test.go` + test cũ của Task 156 phủ phần dời mã), `customer`
  (`TestAttachChannelIdentity`), `httpapi` (`TestCommentActionsAPI_*`, `TestSendMessageAPI_*`,
  `TestDomainErrorSnapshot*`, `TestM4Contract_StatusCodes`), `conversation` (`TestWiring`), `cmd/crm`
  (`TestWireM4_Frame`); `tests/arch`; các gói đổi và gói phụ thuộc ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands, queue,
  tenancy, channel), `send` của Task 156 (`Send`, `sendCommand`, `merge.go`), `inbound/comments.go` của Task 154
  (`LockComment`, `InsertRevisionState`, `UpdateCommentState`), `inbound` Task 153 (`UpsertInboxConversation`),
  `customer.Writer` (`AddIdentity`, `FindOrCreateByIdentity`), adapter Facebook (`Send`, `Lookup`, `SendPolicy` đã có
  đủ comment_reply, private_reply, hide và mã 10900), fakegraph (đã có `POST {comment}/comments`, `is_hidden`, nhắn
  riêng một lần → 10900).
- Dùng lại / viết mới vì:
  - Dùng lại nguyên: `sendCommand.outcome/refuse/note`, `OnTerminal` (nhúng `sendCommand`), `AcquireSendToken`,
    `channel.Account`, `CredentialsFor`, `access.Require`, `publish`, `LockConversationForSend`, `MarkOwnMessageSent`,
    `DeleteUnmatchedEcho`, `UpsertInboxConversation`, `InsertRevisionState`, `UpdateCommentState`, `refusableWrite` của
    httpapi.
  - Tách `run` khỏi `sendCommand.Execute` thay vì chép thân (4 lệnh cùng một khuôn: đọc trong tx → lượt gửi → gọi
    kênh → đổi kết quả).
  - Cùng lý do (jscpd báo lặp với đường gửi tin của Task 156): tách `channelFor`, `policyFor`, `queueOutgoing`,
    `begin`/`done`/`settleSent`/`markSent` từ mã của Task 156 và dùng cho cả bốn lệnh; `executeAction` chung cho trả
    lời và nhắn riêng. Cùng mục đích và cùng lý do thay đổi (chính sách, lượt gửi, ghi kết quả của lệnh ra kênh).
  - Viết mới `customer.Writer.AttachChannelIdentity`: `AddIdentity` của M2 không có `scope` và từ chối `fb_psid`
    (ghi chú Task 152); dùng lõi `insertIdentity` và khóa tư vấn của Task 152, không khai lại `AddIdentity` (A15).
  - Câu SQL mới vì câu có sẵn lọc `kind = 'message'`/`origin = 'own_echo_unmatched'` hay thiếu cột (bài, comment
    cha).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: tin, hội thoại thuộc `conversation`; danh tính khách thuộc `customer` (gắn qua API của nó).
- 5.2 Không ghi bảng module khác: danh tính qua `customer.Writer.AttachChannelIdentity`; token, lượt gửi qua `channel`.
- 5.3 Đọc module khác: `channel.Account`, `CredentialsFor` (public API); không đọc bảng `customer.*`.
- 5.4 Nguyên tử: API ghi tin + hội thoại + lệnh trong một WriteTx; OnDone gắn danh tính + đặt tin sent trong một tx.
- 5.5 Không nguyên tử: gọi Meta là lệnh `commands` sau commit.
- 5.6 Gọi ngoài: ba lệnh `command.fb.reply_comment` (5 phút), `command.fb.private_reply` (5 phút),
  `command.fb.hide_comment` (1 giờ, `AutoRetryNotSent`); không chắc → `in_doubt` (tin `checking`); trả lời comment và
  ẩn có `Looker` (tra Meta), nhắn riêng không có (Meta không cho tra; người quyết ở Task 157).
- 5.7 Ngữ cảnh shop: mọi câu trong WriteTx/`tenancy.WithTenant`; không bảng mới; RLS áp cả DELETE.
- 5.8 Không HTTP trong module: đúng; handler mỏng `handlers_comment_actions.go`.
- 5.9 Khóa chống trùng, quyền: hợp đồng có sẵn `IdempotencyKey`, `x-permission: conversation.reply`; kiểm thêm quyền
  trên Page bằng `access.Require` (không có → 404).
- 5.10 PostgreSQL là nguồn sự thật: đúng (unique một phần `messages_one_live_private_reply` chặn tin riêng thứ hai).
- 5.11 Không thư viện mới.
- 5.12 Theo mẫu: đường ghi Task 12/156, lệnh `commands` Task 156, khóa ADR-46 nhóm 5 → 6.
- 6.1 Đồng thời: nhắn riêng khóa hội thoại comment (FOR UPDATE) trước khi kiểm "đã nhắn" → các yêu cầu cùng comment
  chạy lần lượt; unique một phần là chốt cuối (23505 → `ErrPrivateReplyUsed`). Ẩn: OnDone khóa dòng comment, so trạng
  thái rồi mới ghi (webhook ẩn tới trước → không ghi trùng lịch sử).
- 6.2 Gửi lại: khóa chống trùng của API; lệnh `DedupeKey` = mã tin (trả lời, nhắn riêng), mã mới mỗi yêu cầu ẩn/hiện
  (mỗi lần bấm là một ý muốn); 20 yêu cầu khác khóa → 1 lời gọi Meta (test).
- 6.3 Thứ tự khóa: nhóm 5 (khóa tư vấn danh tính → khách) trước nhóm 6; hai hội thoại (comment, hộp thư) khóa theo id
  tăng dần; hội thoại → tin → message_revisions. Hội thoại hộp thư vừa tạo (uuidv7 mới) luôn lớn hơn hội thoại comment.
- 6.4 Lỗi tạm trên đường yêu cầu: `WriteTx` có sẵn (chạy lại); hàm trong tx gán lại mọi biến.
- 6.5 Cách ly: READ COMMITTED + khóa dòng + câu có điều kiện (`send_status <> 'sent'`, `external_id IS NULL`).
- 6.6 Việc nền lỗi: Runner có sẵn (8 lượt, dead → tin failed qua OnTerminal).
- 6.7 429: `outcome` có sẵn (not_sent + RetryAfter), lượt gửi của Page dùng chung với tin nhắn.
- 6.8 Chết giữa chừng: lệnh `in_doubt` → tin `checking`; trả lời comment/ẩn tra lại bằng Looker, thấy đã có → done
  không gọi POST lần hai (test).
- 6.9 Treo: hạn 30 s mỗi lần chạy lệnh; tx của API ngắn.
- 6.10 Tiền: không áp dụng: không chạm tiền.
- 6.11 Quy luật: "một comment chỉ một tin riêng sống" — test 20 yêu cầu song song + unique một phần; không thêm rapid
  (property_test của Task 156 đã phủ "một lệnh không gửi hai lần" cho Runner chung).
- 6.12 Dữ liệu cá nhân: payload chỉ mã tin (+ cờ ẩn); `jobs.result` của nhắn riêng có PSID người nhận (cần cho OnDone
  gắn danh tính; không vào log, không vào last_error); lỗi, log, audit không có nội dung, PSID.
- 6.13 Quyền: `conversation.reply` trên Page của hội thoại/comment, mặc định từ chối (404).

## Quyết định thay chủ shop

1. **Gắn PSID người nhận tin riêng bằng `customer.Writer.AttachChannelIdentity` (mới) thay cho `customer.AddIdentity`
   của M2 như kế hoạch ghi.** Lý do: `AddIdentity` của M2 không có `Scope` và từ chối `fb_psid` (ghi chú Task 152),
   nên không gắn được PSID theo Page. Hàm mới dùng đúng lõi `insertIdentity` và khóa tư vấn của Task 152 (nhóm 5,
   trước khóa hội thoại), không khai lại `AddIdentity` (A15). PSID đã thuộc hồ sơ khác → `*IdentityTakenError`, không
   gộp. Phương án khác: mở rộng `AddIdentity` cho `fb_psid` có Scope (đổi luật của M2, ảnh hưởng nhiều nơi gọi).
2. **Gộp dòng webhook `feed` của chính câu trả lời bằng cờ transaction `app.merge_reply` (migration 0415).** Trigger
   của 0413 chặn xóa mọi tin trừ `own_echo_unmatched`; nay cho xóa thêm comment `page_external` chỉ khi transaction
   bật cờ, cờ chỉ bật quanh câu `DeletePageReplyEcho` (OnDone và Lookup của lệnh trả lời). Dòng đã có lịch sử không xóa
   (giữ dòng CRM không mã). Phương án khác: hàm `SECURITY DEFINER` riêng; hay giữ cả hai dòng và đánh dấu (màn hình
   phải lọc). Dễ đảo: migration sau thay thân hàm lại.
3. **Chọn hội thoại hộp thư cho tin riêng:** đúng khóa luồng = mã người comment; không có thì hộp thư mới nhất của
   cùng khách trên Page; không có nữa thì tạo với khóa luồng = mã người comment; khi Meta báo `recipient_id` khác thì
   hộp thư vừa dựng (chưa có tin khách) đổi khóa luồng sang PSID thật để lần gửi sau đúng người.
4. **PSID đã thuộc khách khác → tin hệ thống gợi ý** trong hội thoại hộp thư (không mang PSID, không gộp); tin riêng
   vẫn `sent`.
5. **Comment không thuộc hội thoại / không phải của khách → 422 `invalid_input`** (hợp đồng không có mã riêng; không
   thêm mã mới). Tin không phải comment ở `/messages/{id}/hide` → 404 (như không có).
6. **Ẩn/hiện:** mỗi lần bấm một lệnh (`DedupeKey` mới; khóa chống trùng của API chặn bấm lặp); trả 202 kèm trạng
   thái trong CRM lúc này; comment đã xóa → 409 `send_not_allowed` `comment_removed`; lệnh hỏng hay hết hạn không đổi
   dòng comment (`OnTerminal` không làm gì) — xem Việc phát hiện thêm.
7. **Trả lời comment "không rõ đã gửi":** Lookup thấy câu trả lời trên Meta thì ghi mã vào dòng CRM (gộp dòng feed
   nếu có) trong transaction ngắn rồi trả done, vì `ResolveInDoubt` gọi `OnDone` với kết quả rỗng. Mã thấy đã thuộc dòng
   khác (câu trả lời cũ cùng chữ của CRM) → giữ in_doubt, không coi là đã gửi (reviewer vòng 1). Còn một ca không
   phân biệt được: câu cũ cùng chữ do Page trả lời ngoài CRM trong 60 s quanh lúc bấm — Meta không cho tra theo mã của
   CRM; người quyết ở Task 157.
8. **Trạng thái nhắn riêng:** `used` khi có tin riêng `queued|sent|checking|in_doubt` hay `failed` lý do
   `private_reply_used` (đúng kế hoạch); `available` kèm hạn 7 ngày; còn lại `expired`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH05 trả lời: 1 `POST /{comment}/comments` đúng nội dung, tin `sent` kind comment, comment cha, có `sender_staff_id` | `TestReplyToComment_CH05` | `go test -race -count=1 ./internal/modules/conversation/internal/send/` | `ok … send 34.257s` |
| Webhook `feed` của câu trả lời tới trước OnDone → còn đúng 1 dòng, là dòng CRM; trigger 0415: comment `page_external` chưa bật cờ → bị chặn, bật cờ → xóa được, bật cờ mà comment của khách → bị chặn | `TestReplyToComment_FeedEchoBeforeDone` (`tryDelete`) | như trên | ok |
| Trả lời in_doubt → Lookup thấy → done, không POST lần hai | `TestReplyToComment_InDoubtLookup` | như trên | ok |
| Trả lời in_doubt, Meta chỉ có câu trả lời cũ cùng chữ (mã đã thuộc dòng CRM khác) → giữ in_doubt, tin mới không mang mã cũ | `TestReplyToComment_InDoubtOldReplySameText` | như trên | ok |
| Trả lời: chữ rỗng/quá 8 000, comment khác, hội thoại hộp thư → lỗi, không ghi; comment đã xóa → comment_removed | `TestReplyToComment_Invalid`, `TestReplyToComment_Removed` | như trên | ok |
| Nhắn riêng → tin trong hộp thư mới của khách, comment cha, 1 lời gọi Meta, `privateReply.state` available → used | `TestPrivateReply_CH05` | như trên | ok |
| Khách đã có hộp thư → tin vào đó | `TestPrivateReply_ExistingInbox`, `TestCommentActionsAPI_CH05` | như trên; `go test -race -count=1 -run '^(TestCommentActionsAPI_.*\|TestDomainErrorSnapshot.*\|TestM4Contract.*)$' ./internal/httpapi/` | `ok … httpapi 5.290s` |
| 20 nhắn riêng song song khác khóa → 1 thành công, 19 `private_reply_used`, 1 lời gọi Meta; lần sau 409 | `TestPrivateReply_OnlyOnce`, `TestCommentActionsAPI_PrivateReplyRace` | như trên | ok |
| Comment cách 7 ngày → 409 `send_not_allowed` `private_reply_expired` | `TestPrivateReply_Expired`, `TestCommentActionsAPI_Refusals` | như trên | ok |
| Meta 10900 → tin `failed` lý do `private_reply_used` có câu riêng; lần sau 409 | `TestPrivateReply_Meta10900` | như trên | ok |
| `recipient_id` khác → thêm danh tính `fb_psid`, hộp thư đổi khóa luồng; đã thuộc khách khác → không gộp, có gợi ý | `TestPrivateReply_RecipientIdentity`, `TestAttachChannelIdentity` | như trên; `go test -race -count=1 -run '^TestAttachChannelIdentity$' ./internal/modules/customer/` | ok |
| CH11 ẩn → Meta `is_hidden = true`, CRM `is_hidden`, vẫn trong hội thoại, lịch sử staff; hiện lại | `TestHideComment_CH11` | gói send như trên | ok |
| Ẩn in_doubt + Lookup thấy đã ẩn → done, không POST lần hai; webhook ẩn tới trước → không ghi trùng | `TestHideComment_InDoubtLookup`, `TestHideComment_AlreadyHidden` | như trên | ok |
| Ẩn tin không phải comment của khách, comment đã xóa, không quyền | `TestHideComment_Invalid`, `TestCommentActionsAPI_Refusals` | như trên | ok |
| Page `read_only` → ba thao tác 409 `channel_read_only`, không tin, không lệnh | `TestCommentActions_ReadOnly`, `TestCommentActionsAPI_Refusals` | như trên | ok |
| HTTP: 201/202, kind, `parentMessageId`; mã lỗi mới trong bảng; operation không còn 501 | `TestCommentActionsAPI_CH05`, `TestCommentActionsAPI_Hide`, `TestDomainErrorSnapshot`, `TestM4Contract_StatusCodes` | lệnh httpapi như trên | ok |
| Ba lệnh có trong nối, đúng hạn 5 phút / 5 phút / 1 giờ | `TestWiring`, `TestWireM4_Frame` | `go test -race -count=1 -run '^TestWiring$' ./internal/modules/conversation/`; `… -run '^TestWireM4_Frame$' ./cmd/crm/` | ok, ok |
| Luật kiến trúc | `tests/arch` | `go test -count=1 -timeout 25m -v ./tests/arch/` | `ok … tests/arch 290.083s` |
| Cổng tĩnh + test gói đổi (race: cmd/crm, httpapi, conversation, send, customer) và gói phụ thuộc | cả gói | `scripts/agent/ci-task.sh` (từ xa; vòng 1 run 45793139496, sau sửa review run 45795078248 trên blacksmith) | `static: success`, `test (1): success`, `test (2): success`, `CI-TASK XANH` |


## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `customer.AttachChannelIdentity` thay `customer.AddIdentity` (Quyết định 1).
- Kế hoạch ghi "API chi tiết hội thoại comment trả `privateReply`": hàm `conversation.PrivateReplyStatus` đã có và có
  test; việc đưa nó ra HTTP thuộc `listMessages`/`getConversation` (Task 159, còn trong `m4_pending.txt`), task này
  không làm route đó.
- Kế hoạch không ghi migration cho task này; cần 0415 (Quyết định 2).

## Việc phát hiện thêm

- Hợp đồng `ChatPrivateReplyRef.state` ghi "none hoặc send_status của tin riêng", kế hoạch ghi
  `available | used | expired` (+ `at`): Task 159 cần chốt một dạng khi đưa `PrivateReplyStatus` ra `ChatMessage`.
- Lệnh ẩn/hiện hỏng hay hết hạn không có chỗ báo cho nhân viên (comment không có `send_status`); màn hình (Task 175)
  có thể cần đọc trạng thái lệnh hay một cột riêng. Cùng chỗ: lệnh ẩn chết vì token hỏng chưa gọi `ReportTokenInvalid` như lệnh gửi tin (reviewer).

- (Gộp vào m4, reviewer.) Câu trả lời comment, tin riêng lên `in_doubt` (OnTerminal chung của lệnh gửi → checking →
  lịch send_reveal của 157) nhưng gửi lại/bỏ của 157 chỉ nhận tin `kind = 'message'` → nhân viên không xử lý được; tin
  riêng in_doubt còn tính là "đã dùng lượt" nên comment đó không nhắn riêng lại được. Trên nhánh 158 trước khi gộp tin
  cũng kẹt ở `checking` (không lối ra), nên không phải lỗi do gộp; cần chủ shop chọn: mở `discardMessage` (và có thể
  `resendMessage`) cho kind comment/private_reply, hay cách khác. Chưa làm (ngoài phạm vi phiên gộp).

## Reviewer

Kết luận: ĐẠT (vòng 2, trên bản đã gộp m4 21998b0c).

- Vòng 1: CẦN SỬA — `TestReplyToComment_FeedEchoBeforeDone` thử xóa comment của khách nên không kiểm được điều kiện
  cờ `app.merge_reply` của trigger 0415. Đã sửa: helper `tryDelete` kiểm ba ca (page_external chưa bật cờ → chặn; bật
  cờ → xóa được; bật cờ mà comment của khách → chặn). Ghi chú vòng 1 cũng đã xử lý: Lookup khớp nhầm câu trả lời cũ
  cùng chữ → giữ in_doubt (`SetOwnExternalID` đếm dòng, test `TestReplyToComment_InDoubtOldReplySameText`); câu
  "quá hạn" đúng 3/5 phút; ADR-46 thêm đường `AttachChannelIdentity`.
- Vòng 2: không còn phát hiện phải sửa. Ghi chú không bắt buộc: dòng feed của câu trả lời đã có lịch sử thì không
  xóa, Lookup giữ in_doubt chờ người quyết (Task 157) — an toàn hơn trước; ca hiếm Page tự trả lời ngoài CRM cùng chữ
  trong cửa sổ tra vẫn có thể bị lấy mã (đã ghi ở Quyết định 7).
- Ghi chú khác của reviewer (không sửa, đã ghi ở Việc phát hiện thêm): lệnh ẩn chết vì token hỏng không gọi
  `ReportTokenInvalid` (OnTerminal của lệnh ẩn không làm gì).

## Gộp vào m4

Phiên gộp tuần tự 26/11 (nhánh task-158 vào m4 sau Task 159, 165, 157). Xung đột và cách giải:

- `internal/httpapi/errors_conversation.go`, `errors_snapshot_test.go`: m4 đã tách bảng thành `chatErrorMappings` +
  `sendErrorMappings` (Task 159); hai dòng của 158 (`ErrPrivateReplyUsed` 409 `private_reply_used`,
  `ErrNotCustomerComment` 422) đặt cuối `sendErrorMappings`, sau `ErrInvalidStatus` (159) và
  `ErrMessageNotRecoverable` (157); snapshot giữ đủ bốn dòng. Các lỗi này khác nhau, thứ tự kiểm không đổi nghĩa.
- `tests/arch/testdata/m4_pending.txt`: bỏ `replyToComment`, `sendPrivateReply`, `hideComment`, `unhideComment` (158
  làm) và giữ m4 đã bỏ các dòng của 159, 165, 157.
- `cmd/crm/wire_m4_test.go`, `internal/modules/conversation/wiring_test.go`: danh sách lệnh theo đúng thứ tự đăng ký
  (`areas`: inbound, send, automation, live): đọc (+ fetch_post khi có Facebook) → send_message, reply_comment,
  private_reply, hide_comment (send) → auto_hide_comment (165) → live_detect (Facebook).
- `internal/modules/conversation/internal/send/send.go`: `Message` có cả `Origin` (157) lẫn `Kind`, `ParentMessageID`
  (158); `Parts` gồm bốn lệnh của 158 và `Jobs`/`Schedules` send_reveal của 157. `message.go`: tin gửi có cả
  `Origin: originStaff` và `Kind: kindMessage`. `handlers_send.go`: `chatMessageOut` lấy `Kind` từ tin (158) và
  `Origin` từ tin, mặc định staff (157).
- `merge.go`: lấy khung `begin`/`done`/`settleSent` của 158, thêm vào `settleSent` bước `RelabelExternalEcho` của 157
  (echo hỏi bù cùng mã ngoài là echo của chính tin) trước `DeleteUnmatchedEcho` — áp cho gửi tin và tin riêng (cùng
  `settleSent`), như 157 làm cho gửi tin.
- `env_test.go`: hai nhánh cùng khai `env.resolve` (157 trong `indoubt_test.go` trả ba số; 158 đòi số done) → đổi tên
  bản của 158 thành `resolveDone`, sửa hai chỗ gọi (`hide_test.go`, `comment_reply_test.go`).
- Câu SQL trùng tên `LockCommentByID` (165 ở `automation.sql`, 158 ở `comment_actions.sql`; bản 158 thêm
  `channel_account_id`): bỏ bản ở `automation.sql`, giữ bản 158 cho cả hai đường, `make gen` (mã sinh không sửa tay).
- Tương tác không xung đột chữ: `privateCommand` nhúng `sendCommand` nên giờ có `Lookup` của 157 (chỉ đọc database:
  đã có mã ngoài hay sent → done, còn lại giữ in_doubt; không bao giờ not_sent). Chờ echo đúng ý kế hoạch; phần "người quyết" cho tin riêng chưa có
  đường (xem Việc phát hiện thêm); sửa chú thích của `privateCommand`. `replyCommand`, `hideCommand` có `Lookup` riêng (ghi đè).

- Reviewer vòng 1 (gộp): `send.Discard` (157) dựng `Message` không có `Kind`, sau khi `chatMessageOut` lấy `Kind` từ
  tin thì `discardMessage` trả `"kind": ""` (sai enum). Sửa: `Discard` đặt `Kind: kindMessage`
  (`LockRecoverableMessage` chỉ lấy tin kind message); `TestSendRecoveryAPI` kiểm thêm `kind == "message"`.
- Lint ở bể lượt 1: `message.go` sai định dạng (gci) sau khi giải xung đột → gộp dòng `Body` cùng dòng sau.

Bằng chứng:

| Kiểm | Lệnh | Kết quả |
|---|---|---|
| Build, vet trên máy | `go build ./...`; `go vet ./internal/modules/conversation/... ./internal/httpapi/ ./cmd/crm/ ./tests/arch/` | qua |
| Lượt 1 ở bể | `merge-test.sh b631f329 HEAD` (25 gói: cmd/crm, httpapi/..., conversation/..., channel/..., adapters/facebook/..., tests/arch; -race: send, httpapi, cmd/crm, conversation) | `MERGE-TEST ĐỎ (blacksmith)`: chỉ `TestLintCleanOnRealCode` — `message.go:133:1: File is not properly formatted (gci)`; đã sửa |
| Lượt 2 ở bể | `merge-test.sh 47071a5d HEAD` cùng hai tệp gói | `REMOTE-TEST XANH .../runs/45800019597`, `MERGE-TEST XANH (blacksmith)` (static, test 1, test 2 success; 9 phút) |

Không test chập chờn.

Reviewer (gộp): vòng 1 CẦN SỬA (Discard trả kind rỗng; tin comment/tin riêng in_doubt không xử lý được). Vòng 2 ĐẠT:
phát hiện 1 đã sửa và có kiểm trong test; phát hiện 2 đồng ý không sửa trong phiên gộp (đã kẹt ở checking từ nhánh
158, ghi Việc phát hiện thêm).
