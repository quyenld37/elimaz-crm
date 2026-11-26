# Task 156 — Gửi tin — API, lệnh gửi có hạn 3 phút, trạng thái, giới hạn tốc độ, ảnh

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 156 = M4-16). Nhánh gốc: `m4`.
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới trong gói internal `conversation/internal/send`: `Send` (đường API), `Input`, `Message`, `Env`, `CommandKind`,
    lệnh `sendCommand` (Execute, OnDone, OnTerminal), `FailReasonText`; `send.Parts` đổi thân (khung Task 143 trả rỗng →
    một lệnh `command.fb.send_message`), chữ ký giữ nguyên (1 chỗ gọi: `conversation.areas`).
  - Mới ở gói gốc `conversation` (tệp mới `send.go`): `SendMessage`, `SendInput`, `Message`, `SendEnv`, lỗi
    `ErrSendInvalid`, `ErrChannelReadOnly`, `ErrAttachmentNotReady`, `ErrAttachmentRejected`, kiểu lỗi
    `SendNotAllowedError`, `SendMessageKind`, `RegisterFilePurposes`, `OutgoingImagePurpose`, `OutgoingImageFilePurpose`,
    `Attachment`, `FailReasonText` (bí danh của gói `send`: `NotAllowedError`, `KindSendMessage`, `ErrInvalidInput`,
    `ErrReadOnly`, `ErrAttachmentNotReady`, `ErrAttachmentRejected`, `Attachment`, `OutgoingImagePurpose`).
  - Mới ở `channel` (tệp mới `authfail.go`): `ReportAuthFailure(ctx, tx, id, ref)`, `AuthAlertKind`, `ReasonTokenInvalid`,
    `NewAuthAlertCommand` (kế hoạch ghi `channel.ReportAuthFailure` nhưng chưa task nào làm; xem Quyết định 4). Không đổi
    ký hiệu cũ của channel.
  - `internal/adapters/fake`: `Event` thêm trường `AppID` (json `appId`) → `MessagePayload.EchoAppID` của echo (Việc phát
    hiện thêm của Task 153). Chỗ dùng: test của inbound (không đặt trường → giữ hành vi cũ `page_external`).
  - `internal/adapters/facebook/send.go`: tin gửi thành công khi Meta báo mức dùng ≥ 80% → `SendOutcome.RetryAfter` > 0
    (giảm tốc; Quyết định 5). `SendOutcome` không đổi kiểu.
  - `httpapi`: `SendMessage` hiện thực (trước 501); `errorBody` thêm `DenyReason`, `Until` (đã khai trong hợp đồng
    `Error.denyReason`, `Error.until` từ Task 143); hàm mới `conversationDetail` trong `errors_conversation.go`, gọi từ
    `detailedError`.
- Gói phụ thuộc ngược (go list): `conversation/internal/send` ← `conversation` ← `httpapi`, `cmd/crm`, `tests/e2e`;
  `channel` ← mọi gói M4, `httpapi`, `cmd/crm`; `adapters/fake`, `adapters/facebook` ← `cmd/crm`, test inbound.
- Operation OpenAPI bị chạm, oasdiff: `sendMessage` chỉ hiện thực (hợp đồng có từ Task 143, không sửa `openapi.yaml`);
  oasdiff không đổi.
- Chỗ dùng trên màn hình: chưa có (màn hội thoại là Task 175).
- Câu sqlc, bảng, pha migration:
  - Mới `queries/conversation/send.sql` → `internal/modules/conversation/internal/store/send.sql.go`: khóa hội thoại để
    gửi, đọc hội thoại, chèn tin ra `queued`, gắn mã lệnh, đọc/khóa tin của lệnh, ghi lý do từ chối, xóa dòng
    `own_echo_unmatched` cùng `mid` (trả mốc đã nhận/đã xem/cảm xúc để chép sang), đặt `sent`, `failed`, `expired`,
    `checking`, đánh dấu khách chặn Page. Dùng lại `BumpConversationOutbound` (inbound.sql).
  - Bảng chạm: `conversation.conversations`, `conversation.messages` (ghi); `channel.channel_accounts`,
    `channel.send_buckets` qua `channel.Account`, `CredentialsFor`, `AcquireSendToken`, `NoteBlockedUntil`,
    `ReportAuthFailure`; `platform.files`, `platform.file_variants` qua `files.Status`, `files.PublicVariants`;
    `platform.jobs` qua `queue.Enqueue`.
  - Migration mới `0413_conversation_send.sql`, pha mở rộng: (a) dòng `platform.file_purposes` `chat_outgoing`
    (`originals`, `standard`; Quyết định 1); (b) `GRANT DELETE ON conversation.messages TO crm_app` kèm trigger theo
    dòng chỉ cho xóa tin `own_echo_unmatched` (hay khi `app.retention = on`, việc dọn của Task 164); Quyết định 2.
- Tệp khác: `cmd/crm/main.go` (đăng ký `conversation.RegisterFilePurposes()` và `channel.NewAuthAlertCommand`),
  `cmd/crm/wire_m4_test.go`, `internal/modules/conversation/wiring_test.go` (khung có thêm lệnh
  `command.fb.send_message`), `docs/adr/0046-thu-tu-khoa.md` không đổi (bảng không mới).
- Test sẽ chạy: test mới trong `internal/modules/conversation/internal/send/` (message, merge, property), test
  `internal/httpapi` lọc `-run 'SendMessage'`, `internal/modules/channel` (`TestReportAuthFailure`),
  `internal/adapters/fake`, `internal/adapters/facebook` (`TestSend…`), `internal/modules/conversation` (wiring,
  schema), `internal/modules/conversation/internal/inbound` (echo), `cmd/crm` (wire_m4), `tests/arch`; e2e không chạy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands, queue,
  realtime, files, testkit); `commands.CommandKind`, `commands.Handler/DoneHook`, `queue.TerminalHook` (M0 Task 12);
  `channel.AcquireSendToken`, `NoteBlockedUntil`, `MarkStatus`, `CredentialsFor`, `Account` (Task 145); cảnh báo theo
  tài khoản `accountAlert`/`enqueueAlert` của channel (Task 145, 149); `files.Status`, `files.PublicVariants` (M1);
  `access.Require`, `access.PageScope` (Task 151); `BumpConversationOutbound`, `ConfirmOwnEcho` (Task 153);
  `httpapi.refusableWrite` (đường ghi không lưu lỗi nghiệp vụ); `realtimetest.RecordingBus`, `testkit.ChannelAccount`,
  `testkit.DrainDue`, `fakegraph` (Task 146).
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên; cảnh báo token hỏng dùng lại `accountAlert` của channel (chỉ
  thêm một kind). Viết mới phần nghiệp vụ gửi (send.sql, gói `send`), không có hàm chung gửi tin.

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1 Tin ra thuộc conversation; thùng lượt gửi, trạng thái Page thuộc channel; tệp thuộc platform/files.
- 5.2 Không ghi bảng module khác: channel qua `AcquireSendToken`, `NoteBlockedUntil`, `ReportAuthFailure`; tệp chỉ đọc.
- 5.3 Đọc tệp qua `files.Status`, `files.PublicVariants` (public API của gói files), tài khoản qua `channel.Account`.
- 5.4 Ghi tin + hội thoại + lệnh trong một transaction (`WriteTx`); kết quả lệnh ghi trong transaction của Runner.
- 5.5 Gọi Meta sau commit, qua lệnh `command.fb.send_message`.
- 5.6 Lệnh có `in_doubt` (giữ chỗ hết hạn, Execute lỗi, `unknown`) → tin `checking`; không tự gửi lại; `not_sent` mới
  tự gửi lại (`AutoRetryNotSent = true`), hết 3 phút → `expired`.
- 5.7 Mọi câu trong `tenancy.WithTenant`/`WriteTx`; migration không thêm bảng (RLS sẵn có áp cả DELETE).
- 5.8 Gói send không nhập `net/http`; handler `handlers_send.go` mỏng.
- 5.9 `sendMessage` có `IdempotencyKey`, `x-permission: conversation.reply` (Task 143); `access.Require` kiểm phạm vi
  Page ở máy chủ.
- 5.10 Không dùng Valkey; thùng lượt gửi trong PostgreSQL.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu Task 12 (đường ghi, lệnh), Task 73 (lệnh nhiều module).

Đồng thời (mục 6):
- 6.1 Hai lần gửi cùng hội thoại: khóa dòng hội thoại (`FOR UPDATE`) rồi chèn; đổi trạng thái tin luôn là `UPDATE` có
  điều kiện (`send_status = 'queued'`…), không đọc rồi ghi.
- 6.2 Bấm hai lần: `Idempotency-Key` (test 50 yêu cầu song song cùng khóa → 1 tin, 1 lời gọi Meta); `client_ref`
  unique.
- 6.3 Thứ tự khóa (ADR-46): API: `conversations` (6) → `messages` (6) → `jobs`, `idempotency_keys` (13). Kết quả lệnh:
  `channel_accounts`/`send_buckets` (3, `NoteBlockedUntil`, `ReportAuthFailure`) **trước**, rồi `conversations` (6) →
  `messages` (6) → `jobs` (13). Echo vào (Task 153) cũng khóa hội thoại trước khi chèn dòng `own_echo_unmatched`, nên
  gộp echo và chèn echo không đan nhau (test echo trước/sau).
- 6.4 Lỗi tạm trên đường API: `WriteTx` chạy lại; trong Execute lỗi database trước khi gọi Meta → `not_sent` (chưa gửi
  gì, tự thử lại), không bao giờ `in_doubt` vì lỗi của chính CRM.
- 6.5 READ COMMITTED + câu có điều kiện.
- 6.6 Lệnh lỗi: 8 lượt, hết hạn 3 phút → `expired`; `rejected` → `dead` → tin `failed`.
- 6.7 Meta 613/429: `not_sent` + `RetryAfter` của Meta (≥ 60 s); thùng hết lượt: `not_sent` + `RetryAfter` của thùng,
  `KeepAttempt` (không tiêu lượt; hạn 3 phút là trần).
- 6.8 Worker chết sau khi Meta nhận: giữ chỗ hết hạn → `in_doubt` → tin `checking` (OnTerminal).
- 6.9 Hạn Execute 30 s; mỗi lời gọi Graph 10 s.
- 6.10 không áp dụng: không có tiền.
- 6.11 rapid: chuỗi hành vi kênh ngẫu nhiên → mỗi `client_ref` tới kênh không quá 1 lần, mỗi `mid` đúng 1 dòng.
- 6.12 Nội dung tin, PSID không vào payload việc (chỉ `messageId`), `last_error` (chỉ mã lý do), log, cảnh báo (chỉ mã
  tài khoản, tên Page).
- 6.13 `conversation.reply` theo Page (404 khi không có quyền); tệp của tenant khác → 404 (RLS).

## Quyết định thay chủ shop

1. **Mục đích tệp `chat_outgoing` đăng ký ở task này** (migration 0413 + `conversation.RegisterFilePurposes`): kế hoạch
   giao cho Task 155 (chưa làm), nhưng Task 156 cần nó để gửi ảnh. Bucket `originals`, chế độ `standard`, nhận
   JPEG/PNG/WebP ≤ 20 MiB, nhân viên tải lên (`Upload`), xử lý bằng `file.process` thường (biến thể công khai
   `img/<sha>/…`). Task 155 thêm ba mục đích còn lại, không khai lại `chat_outgoing`. Phương án khác: chờ Task 155.
2. **Xóa dòng `own_echo_unmatched` khi gộp echo**: bảng `messages` chưa cho `crm_app` DELETE. Mở DELETE kèm trigger
   theo dòng chỉ cho xóa tin `own_echo_unmatched` (hoặc khi `app.retention = on` cho việc dọn của Task 164), mọi tin khác
   bị chặn ở database. Dòng bị xóa chép mốc đã nhận, đã xem, cảm xúc, giờ của kênh sang tin của mình. Phương án khác:
   hàm `SECURITY DEFINER` riêng (phải tự lọc tenant), hay giữ hai dòng (trái tiêu chí "đúng 1 dòng mang mid").
3. **`fail_reason` lưu mã lý do** (`window_closed`, `blocked_by_user`, `read_only`, `auth`, `rate_limited`,
   `rejected`…), không lưu câu tiếng Việt: Task 157 cần biết "lý do tạm (rate_limited)" mà không so chuỗi (CLAUDE.md);
   câu tiếng Việt là `send.FailReasonText(mã)` (có test), màn hình/API danh sách tin (Task 159) dùng hàm đó. Phương án
   khác: lưu câu tiếng Việt như kế hoạch ghi (Task 157 phải so chuỗi).
4. **`channel.ReportAuthFailure` viết ở task này** (kế hoạch nhắc nhưng không task nào trước tạo): tài khoản → `degraded`
   lý do `token_invalid` (`MarkStatus`), lần đầu đổi thì ghi một cảnh báo gấp `channel.auth_alert` (key
   `channel-auth:<id>`, dùng lại `accountAlert`). Task 150 (sức khỏe kênh) dùng lại hàm này.
5. **Giảm tốc khi Meta báo mức dùng ≥ 80%**: adapter Facebook đặt `SendOutcome.RetryAfter` (mức dùng ≥ 80% → 1 giây,
   Meta báo thời gian chờ → đúng thời gian đó) cho tin đã gửi; lệnh ghi `NoteBlockedUntil(now + RetryAfter)` trong
   OnDone. Phương án khác: thêm trường mới vào `SendOutcome` (đổi kiểu chung của mọi adapter).
6. **Thẻ Human Agent lúc chạy lệnh**: lệnh hỏi lại `SendPolicy` và gửi theo cách nó trả (kể cả khi cửa sổ 24 giờ vừa
   đóng trong 3 phút chờ và Page đã bật Human Agent: tin vẫn do nhân viên gõ, đúng luật Meta). Không lưu cờ
   `humanAgent` theo tin. Phương án khác: lưu cờ và từ chối nếu chế độ đổi.
7. **Thiếu `humanAgent: true` khi chỉ còn gửi được bằng thẻ nhân viên** → 409 `send_not_allowed`, `denyReason =
   window_closed`, `until` = hạn 7 ngày (để màn hình biết còn gửi được bằng nút "Trả lời (nhân viên)"); không thêm giá
   trị enum mới (oasdiff coi thêm enum ở phản hồi là phá vỡ).
8. **Tệp sai mục đích** (`chat_image`, `chat_attachment`, `product_image`…) hay bị từ chối → 422 `attachment_rejected`;
   chưa xử lý xong → 422 `attachment_not_ready`; tệp không có hay của tenant khác → 404.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH03: gửi chữ → queued, 1 lệnh chỉ mang `messageId`; worker → Meta đúng 1 POST /messages, `metadata = client_ref`, RESPONSE, không tag; tin sent có mã Meta; 2 báo hiệu; chạy lại worker không gửi thêm | `TestSendText_CH03` | `go test -race -count=1 ./internal/modules/conversation/internal/send/` và CI | CI `ok …/conversation/internal/send 45.808s` |
| CH03: cùng `Idempotency-Key` 50 lần song song → 1 tin, 1 lệnh, 201 cùng thân hoặc 409 request_in_progress | `TestSendMessageAPI_CH03` | `go test -race -count=1 -run 'TestSendMessageAPI' ./internal/httpapi/` và CI | `ok …/internal/httpapi 3.935s`; CI `ok …/internal/httpapi 48.118s` |
| Q23: echo không metadata tới trước phản hồi → đúng 1 dòng mang mid, origin staff, sent, giữ mốc đã nhận; echo sau, echo có metadata → không thêm dòng | `TestEchoBeforeResponse`, `TestEchoAfterResponseAndWithMetadata` | như dòng 1 | PASS |
| CH04: ảnh `chat_outgoing` ready → `attachment.payload.url = FILES_PUBLIC_BASE_URL/img/<sha>/v1/1280.jpeg` (JPEG lớn nhất), không text; processing → not_ready; `product_image`, `rejected` → attachment_rejected; tenant khác, không có → 404; chữ + ảnh, rỗng, khoảng trắng, > 2 000 ký tự → 400 | `TestSendImage_CH04`, `TestSendMessageAPI_Refusals` | như dòng 1, 2 | PASS |
| CH09: cách 25 giờ → 409 send_not_allowed `window_closed`, không tin/lệnh; Human Agent bật, 3 ngày, `humanAgent: true` → Meta nhận `tag = HUMAN_AGENT`; thiếu → 409 window_closed kèm `until` = +7 ngày | `TestSendPolicy_CH09`, `TestSendMessageAPI_Refusals` | như dòng 1, 2 | PASS |
| MO03: Page `read_only` → 409 channel_read_only; chuyển `read_only` sau khi đã ghi lệnh → lệnh dead, tin failed `read_only`, Meta 0 lời gọi | `TestSendReadOnly_MO03`, `TestSendMessageAPI_Refusals` | như dòng 1, 2 | PASS |
| Meta 551 → failed `blocked_by_user`, hội thoại bị chặn, lần sau từ chối; 190 → failed `auth`, Page degraded `token_invalid`, đúng 1 lệnh cảnh báo gấp; 613 → lệnh pending hẹn ≥ 60 s, tin queued `rate_limited`, last_error không có nội dung tin | `TestSendMetaErrors`, `TestReportAuthFailure` | như dòng 1; `go test -race -count=1 -run TestReportAuthFailure ./internal/modules/channel/` | PASS; CI `ok …/modules/channel 3.911s` |
| Giới hạn tốc độ: thùng 50, nạp 20/s, đồng hồ đứng → Meta đúng 50 lời gọi, 10 tin queued; +1 s → cả 60 sent | `TestSendRateLimit` | như dòng 1 | PASS |
| Giảm tốc khi Meta báo mức dùng ≥ 80% (adapter đặt RetryAfter, lệnh ghi blocked_until) | `TestSend_SlowDownHint`, `TestSendSlowDown` | `go test -count=1 -run 'TestSend' ./internal/adapters/facebook/` và CI | PASS; CI `ok …/adapters/facebook 12.039s` |
| in_doubt → tin checking có mốc, không gửi lại; quá 3 phút → lệnh expired, tin expired; Kind hạn 3 phút, Execute 30 s | `TestSendInDoubtAndExpired`, `TestSendCommandKind`, `TestWiring` | như dòng 1; CI `ok …/modules/conversation 20.956s` | PASS |
| Quy luật (rapid, 100 lượt): mỗi client_ref tới kênh ≤ 1 lần, mỗi mid đúng 1 dòng (trừ tin không rõ chưa echo: 0), tin sent luôn có mã | `TestSendProperty_AtMostOncePerClientRef` | `go test -race -count=1 -run TestSendProperty ./internal/modules/conversation/internal/send/` | `ok … 30.826s` |
| 0413: crm_app xóa được `own_echo_unmatched`, xóa tin khác → 42501 | `TestMessagesDeleteGuard` | như dòng 1 | PASS |
| Echo kênh giả mang app id (`appId`) → `EchoAppID` | `TestSplit_Payload` | `go test -count=1 ./internal/adapters/fake/` | CI `ok …/adapters/fake 1.468s` |
| Mục đích `chat_outgoing` khớp bảng; khung M4 có lệnh `command.fb.send_message`; sendMessage không còn 501 | `TestPurposes_GoMatchesTable`, `TestWireM4_Frame`, `TestWiring`, test kiến trúc (`m4_pending.txt` bỏ `sendMessage`) | CI | `ok …/platform/files 79.264s`, `ok …/cmd/crm 4.641s`, `== test kiến trúc: 47 giây` |
| Cổng tĩnh + test các gói bị ảnh hưởng (sau khi gộp m4 7b93b02) | toàn bộ | `ci-task.sh` của quy trình (log ngoài worktree) | `CI-TASK XANH (so với điểm tách nhánh aafa087…)`; jscpd 0 clone; oasdiff, gitleaks, test-guard qua |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 156 không có e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tệp mới ngoài danh sách của kế hoạch: `send/command.go` (Execute của lệnh, tách khỏi `message.go` cho dễ đọc),
  `send/env_test.go` (môi trường thử dùng chung), `channel/authfail.go` (Quyết định 4); `channel/alert.go` thêm hàm
  `urgentPageAlert` dùng chung cho cảnh báo đăng ký và cảnh báo token (jscpd báo trùng); thân văn bản cảnh báo đăng ký
  giữ nguyên chữ.
- Hàm cắt đoạn trích `snippet` của inbound dời sang `deps.Snippet` (đổi cấu trúc nhỏ, cùng commit) để tin nhân viên gửi
  dùng đúng luật đoạn trích của tin vào/echo; hành vi inbound không đổi.
- `internal/platform/files/purpose_test.go`: thêm `conversation.OutgoingImageFilePurpose()` vào danh sách mục đích của
  `TestPurposes_GoMatchesTable` (như mọi mốc thêm mục đích); không sửa mã của gói files.
- "Echo tới trước phản hồi Send API" kiểm bằng kênh giả: fakegraph không có `EchoBeforeResponse`; test giao echo không
  metadata qua đường vào thật bằng worker chỉ chạy đường vào **trước** khi lệnh gửi chạy (`TestEchoBeforeResponse`),
  logic gộp không phụ thuộc kênh.
- "Client SSE nhận 2 báo hiệu" kiểm bằng `realtimetest.RecordingBus` (đếm `conversation.changed` của hội thoại); đường
  SSE là của M0, không đổi.
- Giới hạn tốc độ: kế hoạch ghi "Meta nhận tối đa 50 lời gọi trong giây đầu" nhưng thùng nạp 20 lượt/giây nên trong
  giây đầu tới 70 lời gọi là đúng luật. Test dùng đồng hồ đứng: đúng 50 lời gọi, 10 tin còn queued; tiến 1 giây → cả 60
  tin sent (`TestSendRateLimit`).
- 50 yêu cầu song song cùng khóa (HTTP): kiểm 1 tin, 1 lệnh, 201 cùng thân hoặc 409 `request_in_progress`; "1 lời gọi
  Meta mỗi tin" kiểm ở test module (`TestSendText_CH03`: chạy worker hai lần vẫn 1 lời gọi).
- Mục đích `chat_attachment`, `chat_image` chưa có (Task 155): ca "sai mục đích" dùng `product_image`, thêm ca tệp
  `rejected`.
- 613: `run_at` kiểm ≥ now + 55 s (đồng hồ PostgreSQL của máy thử lùi được tới 0,36 s, Task 476).
- Không trả `until` cho `window_closed` khi không còn đường gửi (adapter không có mốc); chỉ có `until` khi còn gửi được
  bằng thẻ nhân viên (Quyết định 7).

## Việc phát hiện thêm

- Task 155: không khai lại mục đích `chat_outgoing` (đã có ở 0413 + `conversation.RegisterFilePurposes`). Nếu Task 155
  tạo dòng tệp hay khóa ngoại trỏ tới tin `own_echo_unmatched` (đính kèm của echo), câu `DeleteUnmatchedEcho` của lệnh
  gửi phải chuyển/xóa các dòng đó trước (không thì khóa ngoại chặn xóa và lệnh gửi không ghi được done).
- Task 157: `fail_reason` là mã (Quyết định 3; `rate_limited`, `not_sent` là lý do tạm); tin `checking` có thể còn mã
  `rate_limited`/`not_sent` của lượt trước (không ảnh hưởng trạng thái).
- Task 159: `ChatMessage.failReason` nên trả `conversation.FailReasonText(mã)`; `sender` (Actor) chưa trả ở 201 của
  sendMessage vì cần tên nhân viên (đọc identity) — Task 159 dựng ChatMessage chung.
- Task 150: dùng lại `channel.ReportAuthFailure`; đưa Page `degraded token_invalid` về `active` khi kiểm sức khỏe thấy
  token tốt. API gửi hiện không chặn Page `degraded`/`disconnected` (lệnh gửi từ chối `no_permission` khi không có token).
- `channel.ReportAuthFailure` trong OnTerminal ghi dòng `platform.jobs` mới (cảnh báo, nhóm 13) trước khi khóa hội thoại
  (nhóm 6): chỉ chèn dòng mới với khóa chống trùng riêng theo mã lệnh, không chờ khóa dòng có sẵn; nếu ADR-46 đòi chặt
  thì tách `MarkStatus` và ghi cảnh báo ra hai bước.
- `conversation.message_revisions` có khóa ngoại tới `messages` không `ON DELETE` (0402): task nào ghi bản sửa cho dòng
  `own_echo_unmatched` (echo bị sửa trước khi gộp) thì `DeleteUnmatchedEcho` phải xử lý bản sửa trước (reviewer nêu).
- `TestSendSlowDown` chỉ kiểm `blocked_until` được đặt; việc `AcquireSendToken` từ chối trước mốc đó đã có test ở Task 145.

## Reviewer

Kết luận: ĐẠT (agent reviewer, phần đã stage sau khi gộp m4 7b93b02; ci-task XANH). Phát hiện phải sửa: không có.
Ghi chú không bắt buộc đã xử lý: bổ sung tên còn thiếu ở mục Ảnh hưởng (`channel.ReasonTokenInvalid`,
`conversation.Attachment`, `FailReasonText`, `OutgoingImageFilePurpose`, tên trong gói `send`); khóa ngoại
`message_revisions` → `messages` và độ chặt của `TestSendSlowDown` ghi vào Việc phát hiện thêm. Trigger
`messages_delete_guard` với `app.retention = on` là lớp chống nhầm, không phải chặn quyền (Quyết định 2, chấp nhận).
Dời `snippet` sang `deps.Snippet` chung commit đã nêu ở Khác kế hoạch.
