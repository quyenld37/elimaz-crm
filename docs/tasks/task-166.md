# Task 166 — Tự trả lời comment và tự nhắn riêng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 166 = M4-26). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, trừ một câu sqlc. Câu `InsertOutgoingAction`
  (`queries/conversation/comment_actions.sql`) thêm tham số `@origin` (trước đây cố định `'staff'`): trường mới
  `store.InsertOutgoingActionParams.Origin`, 1 chỗ gọi (`send/action.go`, `insertAction`). Gói `send` (chỉ gói gốc
  `conversation` và nay `automation` nhập): thêm `KindAutoReplyComment`, `KindAutoPrivateReply`, `AutoReplyCommentKind`,
  `AutoPrivateReplyKind`, `QueueAutoCommentReply`, `QueueAutoPrivateReply`, `AutoCommands`, `AutoTerminalFunc`,
  `AutoPrivateAllowed`, `ErrAutoNotAllowed`; `actionRow`
  (không xuất) thêm `origin`. Gói `automation` (chỉ `conversation`, `inbound` nhập): thêm `ReplyConfig`,
  `PrivateMessageConfig`, `ReplyIndex`, `KindPrepareAutoPrivate`; `Parts` trả thêm hai lệnh và một việc; `OnCommentCreated`
  gọi thêm tự trả lời (chữ ký không đổi); `SaveRule` kiểm thêm cấu hình `auto_reply_comment` (mẫu có thật).
- Gói phụ thuộc ngược (go list): `send` ← `conversation`, `automation` (mới, không vòng: `send` không nhập `automation`);
  `automation` ← `conversation`, `inbound`; `conversation` ← `internal/httpapi`, `cmd/crm`, `tests/e2e`. Sửa
  `queries/conversation/*.sql` → sqlc sinh lại `internal/modules/conversation/internal/store` (và các gói store khác nếu
  `make gen` chép model).
- Operation OpenAPI bị chạm, oasdiff: không (cấu hình `AutoReplyCommentConfig` đã có ở Task 143/165). oasdiff không đổi.
- Chỗ dùng trên màn hình: chưa có (Task 178); `console/src/api` không đổi.
- Câu sqlc, bảng, pha migration: không migration. Câu mới trong `queries/conversation/automation.sql`:
  `CountRecentAutomationRuns` (đếm lượt `enqueued|done` cùng quy tắc, cùng `subject_ref`, cùng tiền tố `external_ref`
  trong N giờ, đồng hồ database); trong `comment_actions.sql`: `GetMessageCommandJob` (mã lệnh vừa gắn vào tin); dùng
  lại `GetAutomationRunByJob` (việc chuẩn bị tìm lượt của nó theo mã việc). Sửa `InsertOutgoingAction` (thêm `origin`). Bảng chạm: `conversation.messages` (tin `origin = automation`),
  `conversation.automation_runs`, `conversation.conversations` (hộp thư), `platform.jobs`.
  Thứ tự khóa (ADR-46): trả lời công khai trong tx comment vào (khách 5 → hội thoại comment 6 → messages → automation_runs
  → jobs 13); tin riêng tự động **không** khóa hộp thư trong tx comment vào (hộp thư có thể có id nhỏ hơn hội thoại comment
  đã khóa) mà ghi việc `task.conversation.auto_private_reply` chạy tx riêng: hai hội thoại theo id tăng dần như đường nhân
  viên (Task 158) → messages → jobs (lệnh mới) → cập nhật lượt `automation_runs` (dòng của chính lượt này; lệnh chưa
  commit nên Runner chưa chạm được, không tranh khóa). Lõi `Run` (Task 165) chèn lượt trước khi gọi act: giữ nguyên.
- Test sẽ chạy: test mới `internal/modules/conversation/internal/automation/autoreply_test.go` (`TestAutoReply_*`);
  test cũ phải chạy lại: `TestAutoHide_*`, `TestRun_*`, `TestProperty_AtMostOneCommand` (automation), `TestPrivateReply_*`,
  `TestReplyToComment*`/`TestComment*` (send, vì `insertAction` đổi), `TestWiring` (conversation), `TestWireM4_Frame`
  (cmd/crm), `tests/arch`; cả gói ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (queue, commands,
  tenancy), lõi `automation.Run`/`Skip`/`finish` (Task 165), `send.insertAction`, `inboxOf`/`ensureInbox`,
  `latestPrivate`, `privateState`, `policyFor`, `replyCommand`/`privateCommand` (Task 158: Execute, OnDone gộp echo
  `feed`, gắn PSID), `templates.Render` (Task 161, `Actor = nil` cho tin tự động), `customer.IsDoNotContact` (qua `Run`).
- Dùng lại / viết mới vì: dùng lại toàn bộ thân lệnh của Task 158 (bọc trong `autoCommand` chỉ đổi Kind, bỏ `Looker`,
  thêm ghi kết quả lượt) — không chép Execute/OnDone; dùng lại `insertAction` (thêm `origin`) và khóa hộp thư của đường
  nhân viên. Viết mới: chọn câu xoay vòng, giới hạn mỗi người mỗi bài (câu đếm), việc chuẩn bị tin riêng tự động (không có
  sẵn đường ghi tin riêng ngoài tx comment vào mà không cần nhân viên).

## Soát thiết kế

5.1 Chủ dữ liệu: tin, lượt chạy thuộc `conversation`. 5.2 Không ghi bảng module khác (khách chỉ đọc cờ qua
`customer.IsDoNotContact`; gắn PSID do `privateCommand.OnDone` sẵn có). 5.3 Đọc module khác qua hàm công khai
(`channel.Account`). 5.4 Nguyên tử: lượt + tin trả lời + lệnh trong tx comment vào; lượt + tin riêng + lệnh trong tx
của việc chuẩn bị; kết quả lệnh ghi trong tx của Runner. 5.5 Gọi Meta chỉ qua lệnh `commands`. 5.6 Không rõ đã gửi →
`in_doubt`, không `Looker`, lượt `dropped`, không tự gửi lại (04 mục 5.3); chắc chắn chưa gửi → tự thử lại trong hạn 5
phút; quá hạn → `expired` → `dropped`. 5.7 Tenant: mọi câu trong tx tenant; payload chỉ mã tin/mã lượt. 5.8 Không thêm
HTTP. 5.9 Không thêm route. 5.10 Không Valkey. 5.11 Không thêm thư viện (`hash/fnv` thư viện chuẩn). 5.12 Theo mẫu lệnh
Task 158 và lõi Task 165.
6.1 Đồng thời: một lượt mỗi `(quy tắc, external_ref)` nhờ unique; tin riêng: khóa hội thoại comment (cùng dòng đường nhân
viên khóa) rồi kiểm `latestPrivate`, unique một phần chặn cuối; giới hạn mỗi người mỗi bài: các comment của cùng người
cùng bài nằm trong cùng hội thoại comment, tx comment vào khóa khách (nhóm 5) rồi hội thoại → hai comment nối tiếp, câu
đếm (READ COMMITTED) thấy lượt đã commit. 6.2 Xử lý lại: unique lượt + `DedupeKey` lệnh/việc. 6.3 Khóa đúng ADR-46 (xem
Ảnh hưởng; tin riêng tách tx để không khóa hộp thư sau hội thoại comment). 6.4/6.5 Mặc định nền tảng. 6.6 Việc chuẩn
bị lỗi hết lượt → `dead` → lượt `failed` (TerminalHook). 6.7 429: lệnh Task 158 sẵn có (`not_sent` + `RetryAfter`). 6.8
Chết giữa chừng → `in_doubt` → `dropped`. 6.9 Timeout 30 s. 6.10 Không tiền. 6.11 Test hai worker cho tin riêng
nhân viên + tự động cùng lúc; thuộc tính chạy một lần đã có (Task 165). 6.12 Dữ liệu cá nhân: nội dung câu, tin riêng chỉ
nằm trong `messages.body`; payload việc/lệnh chỉ mã; `SubjectRef` là mã hội thoại comment (không PSID). 6.13 Quyền: không
có route mới; lưu quy tắc vẫn `conversation.settings` (Task 165).

## Quyết định thay chủ shop

1. **`SubjectRef` = mã hội thoại comment**, không phải `"<post_id>:<from_id>"` của kế hoạch: hội thoại comment có khóa luồng
   đúng `<bài>:<người comment>` (một hội thoại mỗi người mỗi bài), nên đếm như nhau mà cột `subject_ref` không chứa PSID
   (dữ liệu cá nhân, lõi Task 165 ghi "không chứa dữ liệu cá nhân"). Đảo ngược: đổi một dòng trong `autoreply.go`.
2. **Tin riêng tự động ghi qua việc `task.conversation.auto_private_reply` (tx riêng)**, không trong tx comment vào: tx
   comment vào đã khóa hội thoại comment; hộp thư của khách thường có id nhỏ hơn (khách nhắn trước khi comment), khóa nó
   sau là ngược thứ tự ADR-46 (deadlock với đường nhân viên Task 158 khóa hộp thư trước). Lượt ghi mã việc chuẩn bị, việc
   đổi sang mã lệnh gửi khi ghi tin; việc chết → lượt `failed`. Phương án khác: khóa hộp thư khi id lớn hơn, còn lại bỏ
   qua (mất tin riêng) — loại. Trễ thêm một lượt worker (dưới 1 giây).
3. **Lệnh tự động ở gói `send`** (`send.AutoCommands`, bọc lệnh của Task 158), không ở `automation/autoreply.go` như ghi
   ở mục Files: thân lệnh (Execute, gộp echo, gắn PSID) là phần không xuất của `send`; bọc lại thay vì chép. Kết quả lượt
   ghi qua hàm do `automation` truyền vào (không vòng nhập).
4. **Không rõ đã gửi (in_doubt)**: lượt `dropped`, tin vẫn theo đường Task 157 (`checking` → "không rõ đã gửi chưa") để
   nhân viên thấy và tự quyết; hệ thống không tự gửi lại (không `Looker`).
5. **Câu xoay vòng**: `FNV-1a 32 bit(comment_id) mod len(replies)` (`hash/fnv.New32a`; kế hoạch ghi "fnv32").
6. **Giới hạn mỗi người mỗi bài so bằng đồng hồ database** (`created_at > now() - N giờ`), một đồng hồ duy nhất (Bẫy đã
   gặp); test lùi `created_at` của lượt trước 2 giờ / 25 giờ.
7. **Kiểm cấu hình khi lưu**: `privateMessage` có `templateId` thì mẫu phải còn và là mẫu chung hoặc của chính Page; không
   có cả `templateId` lẫn `text` (hoặc text rỗng/quá 2.000 ký tự) → 422 `invalid_input` như Task 165.
8. Comment cũ hơn 7 ngày, đã có tin riêng (nhân viên hay tự động), Page không có adapter → `skipped_policy` (kiểm ở tx
   comment vào và kiểm lại dưới khóa trong việc chuẩn bị).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH12, 01 mục 6: 3 câu + tin riêng → 1 trả lời công khai (câu = FNV-1a tính độc lập trong test), 1 tin riêng trong hộp thư, cả hai origin automation, sent, lượt done; xử lý lại 3 lần (webhook, comment_poll, backfill) → vẫn 1 + 1 | `TestAutoReply_OncePerComment` | `go test -race -count=1 ./internal/modules/conversation/internal/automation/` | `ok … automation 15.940s` |
| Cùng người cùng bài 2 giờ → cả hai skipped_limit; 25 giờ → chạy; khác bài → chạy | `TestAutoReply_PerUserPerPost` | như trên | ok |
| Live, includeLive = false → skipped_live; bật → chạy | `TestAutoReply_Live` | như trên | ok |
| Nhân viên nhắn riêng trước → lượt tự động skipped_policy, trả lời công khai done, 1 tin riêng tới Meta; nhân viên + tự động cùng lúc (hai worker) → đúng 1 tin riêng tới Meta | `TestAutoReply_StaffPrivateReply` | như trên; thêm `go test -race -count=3 -run '^TestAutoReply_StaffPrivateReply$' …/automation/` | ok (3 lượt) |
| do_not_contact → skipped_dnc; read_only → skipped_read_only; exceptPostIds, ngoài postIds, comment của Page → không lượt | `TestAutoReply_Gates` | như dòng 1 | ok |
| Meta 10900 cho tin riêng tự động → tin failed, lượt failed, đúng 1 lời gọi (không thử lại) | `TestAutoReply_Meta10900` | như dòng 1 | ok |
| Tin riêng theo mẫu (điền {ten_page}); mẫu không có, không mẫu không chữ → ErrInvalidRule | `TestAutoReply_Template` | như dòng 1 | ok |
| Tên trường JSON của cấu hình khớp hợp đồng | `TestAutoReply_ConfigJSON` | như dòng 1 | ok |
| Lõi Task 165 không hỏng | `TestAutoHide_*`, `TestRun_*`, `TestProperty_AtMostOneCommand` | như dòng 1 (cả gói) | ok |
| Đường nhân viên Task 158 không đổi (insertAction thêm origin) | `TestPrivateReply_*`, `TestReply*`, `TestComment*`, `TestHide*` | `go test -race -count=1 -run '^(TestPrivateReply_.*\|TestReply.*\|TestComment.*\|TestHide.*)$' ./internal/modules/conversation/internal/send/` | `ok … send 2.914s` |
| Nối dây: hai lệnh mới (hạn 5 phút), việc chuẩn bị đúng một handler | `TestWiring`, `TestWireM4_Frame` | `go test -count=1 -run '^(TestWiring.*\|Test.*Wir.*)$' ./internal/modules/conversation/ ./cmd/crm/` | ok, ok |
| Test kiến trúc | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok … tests/arch 161.481s` |
| Test bắt lỗi: tạm bỏ cổng giới hạn và cổng live | `TestAutoReply_PerUserPerPost`, `TestAutoReply_Live` | `go test -count=1 -run '^(TestAutoReply_PerUserPerPost\|TestAutoReply_Live)$' …/automation/` | FAIL đúng chỗ (`muốn "skipped_limit"`, `muốn "skipped_live"`), đã trả mã |
| Cổng kiểm của task (cổng tĩnh + test gói đổi và phụ thuộc, -race cho gói sửa) | các gói trong kế hoạch test | ci-task của quy trình | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 7247c96f…)`, run 45806141136: static, test (1), test (2) success |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

1. Lệnh tự động nằm ở `send/auto.go` (bọc lệnh Task 158); `autoreply.go` chứa cấu hình, cổng riêng, việc chuẩn bị
   (Quyết định 3); thêm việc `task.conversation.auto_private_reply` (Quyết định 2).
2. `SubjectRef` = mã hội thoại comment (Quyết định 1).
3. Sửa câu sqlc `InsertOutgoingAction` (thêm `origin`); thêm `GetMessageCommandJob`, `CountRecentAutomationRuns`.
4. Sửa test nối dây `TestWiring`, `TestWireM4_Frame` (thêm hai lệnh; việc không có lịch không ghép cặp với lịch).
5. `hideCommand.OnTerminal` dùng chung `terminalOutcome` (cùng hành vi).

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 166 không có test e2e)

## Việc phát hiện thêm

- Việc transactional không có handler trong worker bị đánh `dead` ngay ("no handler"): worker chỉ chạy một phần
  handler (như trong test) giết việc của phần khác. Test của task này dùng handler giữ chỗ (`holdPrepare`).
- Tin tự động (trả lời công khai, tin riêng) đi qua `queueOutgoing` → `BumpConversationOutbound` nâng cả
  `last_staff_message_at` của hội thoại. Chưa câu nào đọc cột này; Task 168 (nhắn lại khách chưa trả lời) nếu dùng nó để
  biết "nhân viên đã trả lời" thì phải tách tin origin automation ra.
- `origin/m4` trên GitHub cũ hơn `m4` cục bộ (`7b93b025` là tổ tiên của điểm tách); `git merge m4` không có gì mới.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên phần đã stage). Phát hiện phải sửa: không có. Đã đối chiếu: mọi nơi gọi
`insertAction`/`InsertOutgoingActionParams` (đường nhân viên vẫn ghi staff), thứ tự khóa tin riêng tự động giống đường
nhân viên, từng dòng bảng bằng chứng, không test bị tắt, payload chỉ mã, `SubjectRef` không có PSID. Ghi chú không bắt
buộc: (1) nhánh in_doubt → `dropped` và việc chuẩn bị chết → `failed` chưa có test riêng (kế hoạch không đòi; mã đi
qua `terminalOutcome`); (2) tin tự động đi qua `queueOutgoing` nên cũng nâng `last_staff_message_at` — chuyển sang
"Việc phát hiện thêm" cho Task 168; (3) `env_test.go` sửa phần dựng test, không nêu ở Ảnh hưởng.

## Gộp vào m4

Gộp `task-166` (55c99695) vào `m4` (d92ba1e8, đã có Task 167 tin chào, 171 live, 164 lịch sử). Xung đột và cách giải:

- `send/auto.go` (cả hai bên cùng tạo, add/add): bản m4 là lệnh `command.fb.auto_send` của Task 167, bản nhánh là hai
  lệnh tự trả lời comment, tự nhắn riêng của Task 166 — khác nội dung. Giữ nguyên `auto.go` của m4; phần của nhánh
  chuyển sang tệp mới `send/autoaction.go`. Kiểu hàm trùng `AutoTerminalFunc` (nhánh) = `Settled` (m4, cùng chữ ký):
  bỏ `AutoTerminalFunc`, `AutoCommands` nhận `Settled`. Kiểu `autoCommand` đổi tên `autoActionCommand` (tránh lẫn với
  `AutoCommand` của Task 167).
- `automation/command.go`, `autoreply.go`: hàm ghi kết quả lượt trùng — m4 `settleRun`, nhánh `terminalOutcome` +
  `finishAuto` (cùng ánh xạ done→done, dead→failed, còn lại→dropped). Giữ `settleRun`; bỏ `terminalOutcome`,
  `finishAuto`; `prepareHandler.OnTerminal` và `AutoCommands` dùng `settleRun`; bỏ import `commands` thừa.
- `automation/automation.go`: `Parts` giữ đủ lệnh ẩn (165), `auto_send` (167), hai lệnh tự động (166) và việc
  `prepareHandler` (166); chú thích gói giữ cả hai đoạn.
- `automation/rules.go`: chỉ chú thích `SaveRule` (giữ cả greeting và auto_reply_comment).
- `conversation/wiring_test.go`: lệnh mong đợi `… autoHideKind, autoSendKind, autoReplyKind, autoPrivateKind …`
  (đúng thứ tự đăng ký); lịch giữ `RetentionSchedule` (164) và tách việc không có lịch `task.conversation.auto_private_reply`.
- `cmd/crm/wire_m4_test.go`: giữ bản m4 (`wantM4Commands`, luồng nền live của 171), thêm hai lệnh của 166 sau
  `command.fb.auto_send`; `checkM4Schedules` (tự gộp) tách việc chuẩn bị không có lịch.
- `make gen`: không chênh. `go build ./...`, `go vet` cmd/crm, conversation/..., channel/...: sạch.
- Test từ xa: `CRM_CI_POOL_SLOTS=mac=0,vps=0 CRM_CI_LABEL=integrate-m4-166 scripts/ci/merge-test.sh 12c8f1cb… HEAD`
  (gói: cmd/crm, internal/httpapi, conversation/..., channel/..., adapters/facebook/..., tests/arch; -race: cmd/crm,
  conversation/...) → run 45809336136: static success (build, vet, lint, jscpd, gen-check), test (1), (2) success;
  `MERGE-TEST XANH (blacksmith)`, lượt đầu, không chập chờn.
- Reviewer (gộp vào m4): ĐẠT lượt 1, không có phát hiện phải sửa. Đã đối chiếu: `autoaction.go` = bản nhánh trừ ba
  điểm đổi tên/kiểu; `settleRun` cùng ánh xạ cho hideCommand, prepareHandler, hai lệnh tự động; `Parts` đủ 4 lệnh, 1
  việc; hai test nối dây đúng thứ tự; tệp tự gộp không mất phần nào; không có tương tác sai giữa tin chào và tự trả lời.
