# Task 157 — Không rõ đã gửi chưa và chưa gửi được — chờ echo, gửi lại, bỏ

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 157 / M4-17). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm, không đổi chữ ký cũ. Gói `send` (internal của
  conversation): `Resend`, `Discard`, `RevealInDoubt`, `ResendResult`, `ErrNotRecoverable`, hằng `RevealSchedule`,
  phương thức `sendCommand.Lookup` (Looker của M0), trường mới `Message.Origin`, `Message.FailReason` (struct
  `send.Message` có 1 chỗ dựng ngoài test: `insert` trong `message.go`; 1 chỗ đọc: `chatMessageOut` của httpapi).
  `Parts` của send thêm một việc nền và một lịch. Gói gốc `conversation`: `ResendMessage`, `DiscardMessage`,
  `ResendResult`, `ErrMessageNotRecoverable`, `SendRevealSchedule`. httpapi: `ResendMessage`, `DiscardMessage` của
  `*handler` (thay bản 501 của `gen.UnimplementedHandler`).
- Gói phụ thuộc ngược (go list): `conversation` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/platform/files`
  (test), `tests/e2e`, testdata kiến trúc; `conversation/internal/send` chỉ được `conversation` nhập; `inbound` (đổi
  câu `ConfirmOwnEcho`) chỉ được `conversation` nhập.
- Operation OpenAPI bị chạm, oasdiff: `resendMessage`, `discardMessage` đã có hợp đồng từ Task 143; không đổi
  `openapi.yaml` (không mã lỗi mới: dùng `version_conflict`, `send_not_allowed`, `channel_read_only`, `not_found` đã có),
  oasdiff không đổi. Xóa hai dòng khỏi `tests/arch/testdata/m4_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn hội thoại là Task 175).
- Câu sqlc, bảng, pha migration: không migration mới. `queries/conversation/send.sql`: thêm `GetRecoverableMessageConversation`, `LockRecoverableMessage`,
  `MarkMessageDiscarded`, `LockConversationsDueReveal`, `RevealInDoubtMessages`, `GetSendLookup`, `RelabelExternalEcho`;
  `queries/conversation/inbound.sql`: `ConfirmOwnEcho` nhận thêm trạng thái `expired`, `failed`, `discarded` (sự thật
  thắng) và trả trạng thái cũ. Bảng chạm: `conversation.messages`, `conversation.conversations` (khóa),
  `platform.jobs` (qua `commands.Abandon`, `queue.Enqueue`).
- Test sẽ chạy: `internal/modules/conversation/internal/send` (`-race`, test mới `indoubt_test.go`, `resend_test.go`
  và các test cũ của gói), `internal/modules/conversation/internal/inbound` (echo), `internal/httpapi` (chỉ
  `-run` các test mới + `TestM4Contract`, `TestErrorSnapshot` trong lúc viết; cả gói ở ci-task), `cmd/crm`
  (`TestWireM4_Frame`), `tests/arch`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục commands
  (`Abandon`, `Status`, `ResolveInDoubt`, Looker), queue (`Enqueue`, `Kind`), schedules (`Schedule` theo tenant),
  realtime; trong `send`: `insert`, `allowed`, `outgoingAttachments`, `lock`, `publish`, `FailReasonText`, `state`.
- Dùng lại / viết mới vì: gửi lại dùng lại nguyên `allowed` (hỏi SendPolicy, chỉ xem), `outgoingAttachments` (kiểm lại
  ảnh còn dùng được), `insert` (chèn tin + lệnh + báo hiệu) của Task 156; bỏ lệnh cũ bằng `commands.Abandon` của M0;
  lịch theo mẫu `presence_cleanup` (việc `task.<lịch>`, 1 lượt, hết hạn sau 1 kỳ). Viết mới chỉ phần trạng thái tin
  (`discarded`, `in_doubt`) vì chỉ module conversation biết tin.

## Soát thiết kế

- 5.1 Chủ dữ liệu: tin, hội thoại thuộc `conversation`; không bảng mới.
- 5.2 Không ghi bảng module khác: `platform.jobs` chỉ qua `commands.Abandon` và `queue.Enqueue`; Page qua `channel.Account`.
- 5.3 Đọc module khác: `channel.Account`, `files.Status` (public API).
- 5.4 Nguyên tử: bỏ tin cũ + tin mới + lệnh mới + bỏ lệnh cũ trong một `WriteTx`.
- 5.5 Sau commit: lệnh gửi mới chạy sau commit (`Enqueue`); báo hiệu chỉ đi khi commit.
- 5.6 Gọi ra ngoài: không tự gửi lại tin `in_doubt` (Looker chỉ trả `done` hay `unknown`, không bao giờ `not_sent`); gửi
  lại chỉ khi nhân viên bấm, phản hồi `mayDuplicate`.
- 5.7 Ngữ cảnh shop: mọi câu trong tx tenant (WriteTx, việc của lịch theo tenant, `tenancy.WithTenant` trong Lookup).
- 5.8 HTTP mỏng: handler chỉ gọi `conversation.ResendMessage`/`DiscardMessage`, đổi lỗi ở `errors_conversation.go`.
- 5.9 Đường ghi: hai POST có `IdempotencyKey` và `x-permission: conversation.reply` (Task 143); kiểm quyền theo Page trong module.
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: đường ghi theo mẫu Task 12 (WriteTx, sentinel, errors_<vùng>.go).
- 6.1 Đồng thời: đổi tin cũ bằng câu có điều kiện trạng thái sau khi khóa hội thoại rồi tin; hai `resend` song song khác
  khóa → người sau thấy tin đã `discarded` → 409 `version_conflict` (test).
- 6.2 Gửi lại yêu cầu: khóa chống trùng của M0; cùng khóa → cùng thân.
- 6.3 Thứ tự khóa (ADR-46): nhóm 6 hội thoại → tin; nhóm 13 `platform.jobs` (Abandon, Enqueue) sau cùng. Lịch hiện tin
  khóa các hội thoại theo mã tăng dần rồi mới đổi tin. Echo (`ConfirmOwnEcho`) giữ như Task 153/156: chỉ khóa một dòng tin.
- 6.4 Lỗi tạm: WriteTx/RetryTx của M0; hàm trong tx gán lại biến kết quả mỗi lần chạy.
- 6.5 Cách ly: READ COMMITTED + điều kiện trạng thái trong câu UPDATE.
- 6.6 Việc nền lỗi: việc của lịch 1 lượt, kỳ sau chạy lại (15 s).
- 6.7 429: không áp dụng mới (Task 156 đã lùi theo Meta; tin `failed` lý do `rate_limited` được gửi lại tay).
- 6.8 Chết giữa chừng: giữ chỗ hết hạn → `in_doubt` (M0) → OnTerminal đặt `checking`; Execute không chạy lại (test).
- 6.9 Treo: Lookup chỉ đọc database, hạn Timeout của loại lệnh.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: một `client_ref` tới Meta tối đa một lần — gửi lại tạo `client_ref` mới, tin cũ không bao giờ quay lại
  `queued`; test đếm lời gọi Meta.
- 6.12 Dữ liệu cá nhân: lý do bỏ lệnh là mã ngắn (`resend`, `discard`), chỉ số chỉ có nhãn kênh; không nội dung tin.
- 6.13 Quyền: `conversation.reply` trên Page của hội thoại, không có → 404 như không có tin.

## Quyết định thay chủ shop

1. **Lý do tạm cho gửi lại tin `failed`:** `rate_limited` và `not_sent` (Task 156 ghi cả hai là lý do tạm). Lý do khác
   (chặn, cửa sổ đóng, token…) → 409 `version_conflict`. Phương án khác: chỉ `rate_limited` như kế hoạch ghi ví dụ.
2. **Trạng thái không gửi lại/bỏ được → 409 `version_conflict`** (màn hình đang cũ), không thêm mã lỗi mới; lệnh cũ
   đang chạy (`ErrCommandRunning`) cũng vậy. Phương án khác: mã `message_not_recoverable` riêng (thêm vào x-error-codes).
3. **Gửi lại không có `humanAgent`:** hợp đồng `resendMessage` không có thân; cửa sổ 24 giờ đóng mà chỉ còn thẻ nhân
   viên → 409 `send_not_allowed` `window_closed` kèm hạn (như gửi mới thiếu `humanAgent`); nhân viên gõ lại bằng
   "Trả lời (nhân viên)". Phương án khác: thêm thân `{humanAgent}` vào hợp đồng (thêm trường, không phá).
4. **Echo có `metadata` của CRM cho tin `failed`, `expired`, `discarded` → `sent`** (sự thật thắng, kế hoạch ghi cho
   `discarded`; áp cả hai trạng thái kia vì echo chứng minh Meta đã gửi). Chỉ số `late_echo_after_discard` chỉ đếm khi
   tin cũ là `discarded`.
5. **Echo đọc qua hỏi bù (không `metadata`, không app id → dòng `page_external`) tới trước phản hồi Send** (ghi chú
   Task 148): khi lệnh gửi ghi xong có mã ngoài `mid` của chính Send trả về, dòng `page_external` cùng `mid` là echo
   của chính tin này → đổi nhãn thành `own_echo_unmatched` rồi gộp như Q23 (không cần migration: trigger chỉ cho xóa
   `own_echo_unmatched`). Tin `in_doubt` không có `mid` thì không khớp được (không đoán theo nội dung) — nhân viên quyết.
6. **Tin trả về của `discard`** là tin cũ với `sendStatus = discarded`.
7. **`failReason` của `ChatMessage`** trả mã lý do (cột `fail_reason`, vd `rate_limited`), không câu tiếng Việt: màn
   hình (Task 175) đổi mã sang câu bằng bảng của nó hay `FailReasonText`. Phương án khác: trả câu tiếng Việt.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH18: DropAfterRead → lệnh in_doubt, tin checking; 3 lượt worker + tra lại → Meta 1 lời gọi; +59 s còn checking, +61 s in_doubt + báo hiệu; gửi lại → mayDuplicate, tin cũ discarded, lệnh cũ expired, tin mới sent; bỏ → không thêm lời gọi, lệnh expired | `TestInDoubt_CH18` (+ `checkReveal`) | `go test -race -count=1 ./internal/modules/conversation/internal/send/` | `ok …/send 30.224s` |
| Echo có metadata ở +30 s → sent, không bao giờ in_doubt; tra lại → done | `TestInDoubt_EchoWithin60s` | như trên | như trên |
| Worker chết khi giữ lệnh → in_doubt, Execute không gọi lại | `TestInDoubt_WorkerCrash` | như trên | như trên |
| CH19: 613 liên tục → expired; +6 giờ Meta không nhận thêm; gửi lại khi cửa sổ mở → sent; cửa sổ đóng → send_not_allowed | `TestExpired_CH19` | như trên | như trên |
| Hai resend song song khác khóa → một thành công, một ErrNotRecoverable, một tin mới | `TestResend_Concurrent` | như trên | như trên |
| Trạng thái hợp lệ / không hợp lệ, quyền theo Page, chỉ xem | `TestRecover_States` | như trên | như trên |
| Echo sau discard → sent, `late_echo_after_discard` = 1 | `TestDiscard_LateEcho` | như trên | như trên |
| Echo hỏi bù (page_external) tới trước phản hồi Send → gộp còn 1 dòng | `TestOnDone_MergesBackfilledEcho` | như trên | như trên |
| Lịch send_reveal 15 s theo tenant, việc 1 lượt | `TestRevealSchedule`; `TestWiring`; `TestWireM4_Frame` | `go test -count=1 -run 'TestWireM4\|TestWiring' ./cmd/crm/ ./internal/modules/conversation/` | `ok …/cmd/crm 1.378s`, `ok …/conversation 0.465s` |
| Echo metadata cho checking, in_doubt, failed, expired, discarded → sent (sự thật thắng) | `TestEchoOwnMatchesClientRef` | `go test -race -count=1 ./internal/modules/conversation/internal/inbound/` | `ok …/inbound 11.071s` |
| HTTP: resend in_doubt 201 mayDuplicate, lần hai 409 version_conflict; expired mayDuplicate false; discard 200; discard tin sent 409; tin không có 404 | `TestSendRecoveryAPI` | `go test -count=1 -run '^(TestSendRecoveryAPI\|TestSendMessageAPI_CH03\|TestSendMessageAPI_Refusals\|TestDomainErrorSnapshot.*\|TestM4Contract_StatusCodes)$' ./internal/httpapi/` | `ok …/httpapi 1.232s` (PASS từng test) |
| Lỗi → HTTP, hai operation hết 501 | `TestDomainErrorSnapshot*`, `TestM4Contract_StatusCodes` | như trên | PASS |
| Kiến trúc | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok …/tests/arch 207.074s` |
| Cổng tĩnh | lint, jscpd | `golangci-lint run ./internal/modules/conversation/... ./internal/httpapi/ ./cmd/crm/`; `make jscpd` | `0 issues.`; `Found 0 clones.` |

| Sau khi gộp m4 (6fcce01e, Task 155): các test trên chạy lại | như trên | `go test -race -count=1 ./internal/modules/conversation/internal/send/ ./internal/modules/conversation/internal/inbound/`; `-run` httpapi như trên; `TestWireM4\|TestWiring` | `ok …/send 49.744s`, `ok …/inbound 29.881s`, `ok …/httpapi 2.071s`, `ok …/cmd/crm 0.811s`, `ok …/conversation 1.519s`; lint `0 issues.`, jscpd `Found 0 clones.` |
| ci-task (trước gộp) | cổng tĩnh + test gói đổi và phụ thuộc, từ xa | `scripts/agent/ci-task.sh` | `CI-TASK XANH (từ xa …)` run 45786599862 |
| ci-task (sau gộp m4) | như trên | như trên | ĐỎ chỉ một test ngoài phạm vi: `live.TestTwoWorkersNeverReadSameSession` (run 45790770519; mọi gói task đổi xanh). Gói `live` chỉ phụ thuộc mã sinh `store` (task chỉ thêm câu, đổi `ConfirmOwnEcho` mà `live` không gọi). Chạy trên máy: `-count=5` bản task 1 lỗi, `-count=10` bản task 0 lỗi, `-count=5` + `-count=10` bản m4 sạch 0 lỗi → test chập chờn theo giờ thật (lease 300 ms, ngủ 2,5 s) khi máy bận; không chạy lại ci-task (tốn ~57 phút Actions). |

## Gộp m4

- m4 tiến tới 6fcce01e (Task 155) sau khi bắt đầu; nhánh đưa lên m4 (fast-forward) rồi áp lại phần của 157 bằng 3-way
  (không dùng stash). Xung đột: `inbound/echo.go` (giữ `withoutURLs` của 155 và `confirmOwn` của 157; `echo` giữ
  `refs`/`withoutURLs` của 155 và gọi `confirmOwn`), `conversation/send.go` (155 dời `RegisterFilePurposes` sang
  `filepurposes.go`: bỏ bản trong send.go, giữ phần 157), `wiring_test.go` (ghi chú gộp cả hai). `make gen` không chênh.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Dọn cấu trúc nhỏ trong cùng commit để qua lint/jscpd (không đổi hành vi): `writeChatMessage`, `sendEnv` dùng chung
  cho `SendMessage` và `DiscardMessage` (httpapi); bảng lỗi conversation tách `chatErrorMappings` + `sendErrorMappings`
  (giữ thứ tự kiểm; dupl với `errors_order.go`); `inbound.confirmOwn` tách khỏi `echo` (gocognit); `inTx` trong test
  của send. `TestEchoOwnMatchesClientRef` đổi kỳ vọng tin `failed` (nay sent, Quyết định 4), thêm `expired`, `discarded`.

## Việc phát hiện thêm

- Echo hỏi bù (`page_external`) có ảnh tạo lệnh tải tệp (Task 155) rồi bị `OnDone` đổi nhãn và xóa (Task 157 Quyết
  định 5): cần kiểm lệnh tải tệp bỏ qua êm khi tin không còn (reviewer Task 157). Chưa làm, ngoài phạm vi.
- `live.TestTwoWorkersNeverReadSameSession` (Task 170) chập chờn khi máy bận: lấy giờ thật làm tiêu chí (lease 300 ms,
  gia hạn 80 ms, ngủ 2,5 s), trái luật Task 400 ("đúng/sai không lấy ngưỡng giờ thật"). Đỏ một lần trên GitHub (run
  45790770519) và 1/5 trên máy. Cần task sửa riêng (đồng hồ giả hay chờ theo sự kiện).

## Reviewer

Kết luận: ĐẠT (lượt 1, trên bản đã gộp m4 6fcce01e). Phát hiện phải sửa: không có. Đã soát: thứ tự khóa (hội thoại →
tin → platform.jobs; lịch hiện tin khóa hội thoại theo mã tăng dần; echo chỉ khóa một dòng tin), `ConfirmOwnEcho` CTE
`FOR UPDATE` trả đúng trạng thái cũ, trigger xóa cho phép đổi nhãn rồi xóa, `Abandon` (đã kết thúc → nil, đang chạy →
409), `Lookup` không bao giờ trả `not_sent`; các quyết định thay chủ shop không trái spec (04 mục 5.3, CH18, CH19).
Ghi chú không bắt buộc (không sửa): Ảnh hưởng thiếu câu `GetRecoverableMessageConversation` (đã thêm vào mục Ảnh
hưởng); `TestRecover_States` chưa có ca gửi lại `failed`+`not_sent` và bỏ tin `in_doubt` (cùng nhánh mã với ca đã có);
chỉ số `late_echo_after_discard` tăng trong tx webhook có thể đếm dư khi tx chạy lại (giống `Observe` sẵn có); dòng
`page_external` có ảnh của echo hỏi bù tạo lệnh tải tệp trước khi bị đổi nhãn rồi xóa — lệnh đó cần bỏ qua êm khi tin
không còn (ghi vào Việc phát hiện thêm).

## Gộp vào m4

Phiên gộp tuần tự 26/11, `git merge --no-ff --no-commit task-157` trên m4 (`769dff1f`, sau Task 165).

Xung đột (2 tệp), giữ đủ hai bên:

1. `internal/httpapi/errors_conversation.go` — hai bên cùng tách bảng `conversationError` để tránh `dupl` với
   `errors_order.go`: m4 (Task 159) tách bằng hai lần `matchError` lồng nhau và thêm `ErrInvalidStatus`; nhánh tách
   thành `chatErrorMappings()` + `sendErrorMappings()` nối bằng `slices.Concat` và thêm `ErrMessageNotRecoverable`.
   Giữ cấu trúc của nhánh (một bảng nối, cùng thứ tự kiểm), thêm `ErrInvalidStatus` (422 `invalid_input`) vào
   `sendErrorMappings` ngay trước `ErrMessageNotRecoverable` (409 `version_conflict`) — đúng thứ tự của m4 (cuối
   bảng); hai sentinel khác nhau nên thứ tự giữa chúng không đổi kết quả.
2. `internal/httpapi/errors_snapshot_test.go` — giữ cả hai dòng (`ErrInvalidStatus`, `ErrMessageNotRecoverable`).

Tự gộp, đã xem lại tay: `m4_pending.txt` bỏ `resendMessage`, `discardMessage` (còn 10 thao tác, không còn thao tác đã
làm); `cmd/crm/wire_m4_test.go` (`checkM4Schedules` thêm `conversation.SendRevealSchedule`; lệnh của 155, 154, 156,
165, 170 còn đủ); `conversation/wiring_test.go` (`checkReminders` thêm `send_reveal` mỗi 15 s; `checkCommands` của Task
165 giữ nguyên vì 157 không thêm lệnh).

Bằng chứng: `go build ./...`, `go vet` (httpapi, conversation/..., cmd/crm, tests/arch) qua trên máy; test từ xa xem dòng
dưới.

| Kiểm | Lệnh | Kết quả |
|---|---|---|
| build, vet trên máy | `go build ./...`; `go vet ./internal/httpapi ./internal/modules/conversation/... ./cmd/crm ./tests/arch` | qua |
| cổng tĩnh + test gói (cmd/crm, httpapi, conversation/..., channel/..., adapters/facebook/..., tests/arch, migrations; -race: cmd/crm, httpapi, conversation/..., tests/arch) | `CRM_CI_POOL_SLOTS=mac=0 CRM_CI_LABEL=integrate-m4-157 scripts/ci/merge-test.sh <tree 4c93eb72> HEAD …` | `static: success` (gồm dupl/jscpd), `test (1)`, `test (2)`: success; `MERGE-TEST XANH (blacksmith)`, run 45796376622 |

Reviewer (lượt gộp): **ĐẠT** — không có phát hiện phải sửa; đã so ba bản (m4, nhánh, bản stage) của bảng lỗi: đủ 20 dòng của `chatErrorMappings`, 6 dòng của `sendErrorMappings`, thứ tự giữ nguyên; thứ tự lịch khớp `areas()`; mã sinh khớp SQL. Ghi chú không bắt buộc (chép lại chú thích "vì sao chia hai bảng" của m4) không sửa.
