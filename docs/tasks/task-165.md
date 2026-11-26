# Task 165 — Lõi quy tắc tự động và tự ẩn comment

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 165 = M4-25). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**. Gói `conversation/internal/automation` (chỉ gói
  gốc `conversation` nhập): `Trigger`, `Outcome` (+ hằng), `Rule`, `Run`, `Skip`, `KindAutoHide`…, `phone.Contains`,
  `ExtractPhones` (tên trong kế hoạch là `phone.Contains`/`phone.Extract`, xem Khác kế hoạch), `OnCommentCreated`,
  `LoadRule`, `ListRules`, `SaveRule`, `HideCommandKind`, `KindHideCommand`, `Parts` (đã có: 1 chỗ gọi
  `conversation/wiring.go`, nay trả lệnh ẩn tự động). Gói gốc `conversation`: thêm `AutomationRule`,
  `ListAutomationRules`, `Writer.UpdateAutomationRule`, `EntityAutomationRule`. Gói `inbound`: hàm không xuất `onCommentCreated` (1 chỗ gọi, `comments.go`) đổi chữ ký thành
  `(ctx, tx, deps.Deps, webhooks.Inbound, channel.ChannelAccount, channel.CommentPayload, msgID, convID, isLive)` rồi tự
  dựng `automation.Trigger`. Ký hiệu mới khác trong gói `internal/automation` (chỉ `conversation` dùng): `HideConfig`,
  `Kinds`, `HideOff`/`HidePhoneOnly`/`HideAll`, `ErrInvalidRule`, `ErrRuleVersionConflict`, `TriggerCommentCreated`.
  `models.go` của 14 gói store do sqlc sinh lại (thêm 3 struct `ConversationAutomation*`).
- Gói phụ thuộc ngược (go list): `internal/modules/conversation/internal/automation` ← `conversation`; `inbound` ←
  `conversation`, test của `send`; `conversation` ← `internal/httpapi`, `cmd/crm`, `tests/e2e`, `internal/queries` (đọc
  view). Không đổi chữ ký nào của `conversation` đang dùng.
- Operation OpenAPI bị chạm, oasdiff: `listAutomationRules`, `updateAutomationRule` (đã khai ở Task 143, không sửa
  hợp đồng); bỏ hai dòng khỏi `tests/arch/testdata/m4_pending.txt`. oasdiff: không đổi.
- Chỗ dùng trên màn hình: chưa có (màn cài đặt là Task 178); `console/src/api` không đổi vì hợp đồng không đổi.
- Câu sqlc, bảng, pha migration: migration mới `0407_automation.sql` pha **mở rộng** (CREATE TABLE
  `conversation.automation_rules`, `conversation.automation_runs`, view `conversation.automation_rules_v`, RLS, GRANT);
  câu mới `queries/conversation/automation.sql` (đọc/khóa/lưu quy tắc, chèn lượt chạy `ON CONFLICT DO NOTHING`, gắn
  lệnh, kết thúc lượt theo lệnh, đọc comment để ẩn, khóa comment theo id); dùng lại `GetPageSettings`,
  `InsertRevisionState`, `UpdateCommentState`. Thứ tự khóa ADR-46 nhóm 6: messages → message_revisions →
  automation_rules → automation_runs (đã có trong khung).
- Test sẽ chạy: `go test -race -run` từng test mới của `automation` (`TestContainsPhone`, `TestExtractPhones`,
  `TestAutoHide_*` 7 test, `TestRun_*` 3 test, `TestProperty_AtMostOneCommand`, `TestAutomationRulesView`), test mới của
  `httpapi` (`TestAutomationRulesAPI`), test cũ phải sửa: `TestWiring` (conversation), `TestWireM4_Frame` (cmd/crm) —
  danh sách lệnh có thêm lệnh ẩn tự động; `TestViewsDocParses` (tests/arch) — số view 47 → 48; test cũ của `inbound`
  (`TestInbound_OnlyWebhookNotesInbound`, `TestInbound_PolicyEnforcementRouted`, `TestComment*`), `tests/arch`; cả gói ở
  `ci-task.sh`. Bảng lỗi HTTP không đổi (xem Khác kế hoạch 3).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (queue, commands,
  tenancy, db), `channel.AcquireSendToken`, `channel.CredentialsFor`, `channel.Account`, `customer.IsDoNotContact`,
  `access.Require`, `assign.Signal`, `Writer.staffAudit`, mẫu cài đặt Page (`pagesettings.go`: version 0 = mặc định),
  lệnh gửi `send/command.go` (Execute ngoài tx, outcome theo kết quả kênh). Tìm "phone": `customer` có chuẩn hóa SĐT
  (`textnorm`/`phone` của M2) nhưng là chuẩn hóa một số đã biết, không dò số trong câu tự do có dấu cách/chấm.
- Dùng lại / viết mới vì: dùng lại mọi hàm nền trên; lỗi HTTP dùng lại `conversation.ErrInvalidInput`,
  `ErrVersionConflict`, `ErrNotFound` (không thêm sentinel mới vào bảng lỗi). jscpd báo 3 đoạn giống
  (`automation/command.go` ~ `send/command.go`: khối đọc trong tx tenant và lấy lượt gửi; `automation.go` ~ `assign.go`:
  kiểm quyền + Page): đã viết lại `Execute` thành `ready`/`read`/`takeToken`/`hideOutcome` và thêm `requirePage` — giữ
  riêng vì khác lý do thay đổi (lệnh ẩn tự động ghi kết quả vào `automation_runs`; gộp sẽ phải sửa `send/command.go`
  của Task 156 đang được Task 157/158 sửa song song). Viết mới: bộ dò SĐT trong văn bản (mẫu riêng Q27, không có sẵn);
  lệnh ẩn tự động riêng (xem Quyết định 1) — giống khung `sendCommand` nhưng khác lý do thay đổi (kết quả ghi vào
  `automation_runs`, không đổi trạng thái gửi của tin) nên không gộp.

## Soát thiết kế

5.1 Chủ dữ liệu: quy tắc và lượt chạy thuộc `conversation` (05 ERD), schema `conversation`.
5.2 Không ghi bảng module khác: đọc `channel.Account`, `customer.IsDoNotContact` qua API công khai; ghi chỉ bảng của
`conversation`. 5.3 Đọc module khác: qua hàm công khai (không đọc bảng). 5.4 Nguyên tử: lượt chạy + lệnh ẩn ghi trong
cùng tx của sự kiện comment vào; kết quả lệnh (done/failed/dropped) ghi trong tx của Runner. 5.5 Không cần nguyên tử:
gọi Meta là lệnh `commands`. 5.6 Gọi ra ngoài: lệnh `command.fb.auto_hide_comment`, không rõ → `in_doubt`, hook ghi
`dropped`, không tự gửi lại, không Looker. 5.7 Tenant: bảng mới `tenant_id NOT NULL`, `UNIQUE (tenant_id, id)`, FK ghép,
`platform.enable_tenant_rls`; mọi câu trong tx tenant. 5.8 HTTP mỏng: `handlers_automation.go` chỉ đổi kiểu. 5.9 PUT có
`IdempotencyKey`, `x-permission conversation.settings` (đã khai). 5.10 Không dùng Valkey. 5.11 Không thêm thư viện.
5.12 Theo mẫu `pagesettings.go` (version 0 mặc định, audit, báo hiệu) và `send/command.go`.
6.1 Đồng thời: lưu quy tắc khóa dòng + so `version`, lần đầu `INSERT … ON CONFLICT … WHERE version = expected` (0 dòng →
409); lượt chạy chống trùng bằng unique `(tenant_id, rule_id, external_ref)` → 0 dòng là đã chạy. 6.2 Gửi lại: PUT qua
middleware chống trùng; xử lý lại comment không ra lệnh thứ hai (unique + `DedupeKey` của lệnh). 6.3 Khóa: đúng nhóm 6
(messages → message_revisions → automation_runs trong OnDone; quy tắc chỉ đọc không khóa ở đường vào). 6.4/6.5 Mặc định
nền tảng (RetryTx của handler vào, READ COMMITTED). 6.6 Lệnh lỗi hết lượt → `dead` → `failed`. 6.7 429: `not_sent` có
`RetryAfter`, thùng lượt gửi của Page. 6.8 Chết giữa chừng → `in_doubt` → `dropped`. 6.9 Timeout lệnh 30 s. 6.10 Không
áp dụng: không có tiền. 6.11 rapid: `TestProperty_AtMostOneCommand` (xử lý lại, hai worker, bật/tắt giữa chừng → ≤ 1
lệnh mỗi (quy tắc, comment)). 6.12 Dữ liệu cá nhân: nội dung comment, SĐT không vào log/audit/payload lệnh (payload chỉ
mã tin, mã lượt); audit quy tắc chỉ cờ, chế độ, số bài ngoại lệ. 6.13 Quyền: GET `conversation.read`, PUT
`conversation.settings` theo Page (`access.Require`; không có ở Page → 404, không có quyền → 403 ở guard).

## Việc nối bắt buộc (giao kèm)

(a) `HandlePolicy` đã được nối vào handler vào (`handler.go`, nhánh `EventPolicyEnforcement`, có
`TestInbound_PolicyEnforcementRouted`) và (b) `NoteInbound` chỉ ghi khi `Via = webhook` (`handler.account`, có
`TestInbound_OnlyWebhookNotesInbound`) — cả hai đã có trên m4 (gộp ở Task 150/170). Task này kiểm lại bằng cách chạy hai
test đó (bảng bằng chứng), không viết lại.

## Quyết định thay chủ shop

1. **Lệnh ẩn tự động là loại lệnh riêng `command.fb.auto_hide_comment`** (trong `automation/command.go`), không dùng
   `command.fb.hide_comment` của Task 158. Vì: Task 158 chạy song song, chưa có mã trên m4 (kế hoạch ghi Task 165 song
   song được với 158 nhưng lại dùng lệnh của 158); hai task cùng đăng ký một loại lệnh thì worker không khởi động; lệnh
   tự động còn cần `OnTerminal` riêng (done/failed/dropped vào `automation_runs`) và không có `Looker` (không rõ thì bỏ,
   04 mục 5.3), khác lệnh ẩn tay (có `Looker`, tự kết luận done). Phương án khác: chờ 158 rồi dùng chung lệnh, thêm
   nhánh "nguồn automation" vào hook của 158. Đảo ngược: đổi `enqueueHide` sang lệnh của 158 và xóa `command.go`.
2. **Mẫu SĐT cho phép một dấu cách/chấm/gạch ngay sau `84`**: `(?:\+?84[\s.\-]?|0)[35789](?:[\s.\-]?\d){8}`. Mẫu
   trong kế hoạch không có dấu cách sau `84` nên ca bắt buộc `"+84 912 345 678"` (đúng) không khớp. Ranh giới chữ số
   kiểm bằng mã (RE2 không có nhìn trước/sau).
3. **auto_hide không bị chặn ở comment live**: kế hoạch không ghi cổng live cho tự ẩn; ẩn SĐT trong comment live là điều
   shop muốn (che SĐT khỏi người xem). `skipped_live` để Task 166 (tự trả lời) dùng qua `Skip`.
4. **ci-task chạy từ xa (mặc định)**: từ 26/11 ci-task mặc định đẩy một commit tạm lên nhánh `ci/task-165` của origin
   để chạy trên GitHub Actions rồi tự xóa nhánh đó (cấu hình chủ shop chọn 26/11, ghi ở CLAUDE.md bước 5). Lệnh giao
   task cấm push; hook chỉ cho chạy đúng lệnh ci-task nên không đặt được `CRM_CI_REMOTE=0` (lối chạy máy). Tôi coi đây
   là đường kiểm chủ shop đã duyệt, không phải push mã của task: không đẩy nhánh `task-165`, không deploy. Lần chạy đầu
   bị dừng ở máy giữa chừng (hết giới hạn 10 phút của lệnh nền); lần hai chạy hết và tự dọn nhánh tạm.
5. **Cấu hình mặc định của loại chưa lưu** (API trả khi version 0, tắt): auto_hide `off`; tự trả lời một câu mẫu, mọi
   bài, 24 giờ; tin chào một câu chào; nhắn lại 4 giờ với `templateId` = uuid rỗng (chưa có mẫu; hợp đồng bắt buộc
   trường này). Màn hình (Task 178) nhận biết "chưa cấu hình" qua `version = 0`. Task 166–168 kiểm mẫu có thật khi lưu.
6. **Lượt bị bỏ qua (cũ, chỉ xem, không liên hệ) cũng giữ chỗ `external_ref`**: sự kiện đó không chạy lại sau dù Page
   đổi chế độ — đúng "mỗi sự kiện một lượt" của 05 mục 3; quy tắc tắt thì không ghi gì (đúng kế hoạch).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Bảng `phone.Contains` (8 ca kế hoạch + 5 ca thêm), dạng `0xxxxxxxxx` | `TestContainsPhone`, `TestExtractPhones` | `go test -race -count=1 -run '^(TestContainsPhone\|TestExtractPhones\|TestAutoHide_.*\|TestRun_.*\|TestAutomationRulesView)$' ./internal/modules/conversation/internal/automation/` | `ok … automation 3.471s` |
| CH11 phone_only: 1 lệnh, Meta `is_hidden`, tin `is_hidden` còn dòng, lịch sử nguồn automation, lượt done; không SĐT → 0 | `TestAutoHide_PhoneOnly` | như trên | ok |
| all, bài ngoại lệ, tắt, `off` | `TestAutoHide_AllExceptAndOff` | như trên | ok |
| comment của Page không bao giờ | `TestAutoHide_PageCommentNever`, `TestRun_NoRowGates` | như trên | ok |
| read_only → skipped_read_only; cũ 16 phút (hỏi bù) → skipped_stale; 0 lệnh | `TestAutoHide_ReadOnlyAndStale` | như trên | ok |
| tin lịch sử → không dòng | `TestAutoHide_HistoryNever`, `TestRun_NoRowGates` | như trên | ok |
| Xử lý lại qua webhook, comment_poll, backfill → 1 lượt, 1 lệnh, 1 lời gọi Meta | `TestAutoHide_ReprocessedOnce` | như trên | ok |
| in_doubt → dropped, không lệnh thứ hai, không gửi lại | `TestAutoHide_InDoubtDropped` | như trên | ok |
| Cổng Run: không liên hệ → skipped_dnc (không chặn ẩn), already_run, Skip(limit), act lỗi lùi | `TestRun_DoNotContactAndOnce`, `TestRun_ActSkipAndError` | như trên | ok |
| View `automation_rules_v` đúng cột docs, không `config`, 1 dòng sau PUT; version cũ, cấu hình sai | `TestAutomationRulesView`; `TestViewsDocParses` | như trên; `go test -count=1 -run '^(TestViewsDoc.*)$' ./tests/arch/` | ok; `ok … tests/arch 1.837s` |
| Thuộc tính rapid 200 lượt: xử lý lại, hai worker, bật/tắt giữa chừng → ≤ 1 lệnh mỗi (quy tắc, comment) | `TestProperty_AtMostOneCommand` | `go test -count=1 -run '^(TestProperty_AtMostOneCommand)$' ./internal/modules/conversation/internal/automation/` | `ok … 24.993s` (bản -race trước khi tối ưu: ok 342 s) |
| API: GET đủ 4 loại version 0; PUT không quyền → 403; Page khác → 404; `mode = "abc"` → 400; kind khác → 422; lưu → 200 v1 + audit + view 1 dòng; version cũ → 409 | `TestAutomationRulesAPI` | `go test -race -count=1 -run '^(TestAutomationRulesAPI)$' ./internal/httpapi/` | `--- PASS: TestAutomationRulesAPI`, ok |
| Việc nối (a) HandlePolicy, (b) NoteInbound chỉ webhook — đã có trên m4 | `TestInbound_PolicyEnforcementRouted`, `TestInbound_OnlyWebhookNotesInbound`, `TestComment*` | `go test -race -count=1 -run '^(TestInbound_OnlyWebhookNotesInbound\|TestInbound_PolicyEnforcementRouted\|TestComment.*)$' ./internal/modules/conversation/internal/inbound/` | `ok … inbound 3.023s` |
| Nối dây: lệnh ẩn tự động có trong danh sách lệnh | `TestWiring`, `TestWireM4_Frame` | `go test -count=1 -run '^(TestWiring.*\|Test.*Wir.*)$' ./internal/modules/conversation/ ./cmd/crm/` | ok, ok |
| Cổng kiểm của task (cổng tĩnh + test gói đổi và gói phụ thuộc, -race cho gói sửa) | mọi test của các gói trong kế hoạch test | ci-task (`scripts/agent/ci-task.sh`) | `CI-TASK XANH (từ xa, so với điểm tách nhánh 2f7d2932…)`, run 45788015465: static success 13 phút 45 giây, test (1)–(6) success |
| Kiểm test có bắt lỗi: tạm bỏ cổng FromPage/IsHistory và cổng cũ trong `Run` | `TestAutoHide_HistoryNever`, `TestAutoHide_ReadOnlyAndStale` | như dòng 1 | FAIL đúng chỗ (`lịch sử: 1 dòng, 1 lệnh`; `cũ: lượt "done"`), đã trả mã |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

1. Tên hàm SĐT: `automation.ContainsPhone`, `automation.ExtractPhones` (kế hoạch: `phone.Contains`, `phone.Extract`) —
   kế hoạch đặt `phone.go` trong gói `automation`, không có gói `phone`.
2. Lệnh `command.fb.auto_hide_comment` thay `command.fb.hide_comment` (Quyết định 1).
3. `config.kind` khác loại trên đường dẫn → **422** `invalid_input` (dùng lại `conversation.ErrInvalidInput` như cài
   đặt Page), không 400: thêm một sentinel 400 vào bảng lỗi làm `dupl` đỏ ở `errors_conversation.go`/`errors_order.go`.
   Cấu hình sai lược đồ (`mode = "abc"`) vẫn **400** do hợp đồng chặn, đúng kế hoạch.
4. Thứ tự: mã được viết trước một phần test (phone, engine, autohide viết trước test); bù bằng bước làm hỏng tạm mã để
   chắc test bắt lỗi (dòng cuối bảng bằng chứng).
5. Thêm `TestMain` (rapid 200 lượt) và `env_test.go` trong `automation`.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 165 không có test e2e)

## Việc phát hiện thêm

- Máy Meta giả (`internal/testkit/fakegraph`, `comment`): `POST /{comment-id}` với comment không có trên máy giả làm
  panic (con trỏ nil) thay vì trả lỗi Graph 100. Test của task này luôn `AddComment` trước; sửa testkit để task khác.
- Kế hoạch ghi Task 165 song song được với 158 nhưng Interfaces lại dùng lệnh của 158 (Quyết định 1): Task 158 nên
  biết có `command.fb.auto_hide_comment` để không ghi đè trạng thái ẩn (lệnh ẩn tay sau đó vẫn đúng: chỉ đổi cờ).

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên phần đã stage). Phát hiện phải sửa: không có. Ghi chú không bắt buộc đã
xử lý: mục Ảnh hưởng mô tả lệch chữ ký `onCommentCreated` và thiếu vài ký hiệu nội bộ → đã sửa. Ghi chú còn lại cho điều
phối viên: sau lần gộp m4 (2f7d2932) của task này, m4 nhận thêm Task 155 (`6fcce01e`, `487f12d7`) và Task 494
(`21998b0c`); Task 155 chạm cùng `inbound/comments.go` (chữ ký `OnInboundAttachments`, `OnNewCustomer`),
`inbound.Parts` (thêm `command.fb.fetch_attachment`, `command.fb.fetch_profile`) và `cmd/crm/wire_m4_test.go` — khi gộp,
danh sách lệnh mong đợi của `checkCommands` (`wiring_test.go`) và `TestWireM4_Frame` phải ghép cả hai bên. Theo lệnh
giao task, sau khi ĐẠT và commit không gộp thêm. `OnCommentCreated` đọc `ConversationCustomer` mỗi comment dù auto_hide
không cần (một câu đọc theo khóa chính; Task 166 dùng).
