# Task 164 — Lấy 20 tin gần nhất khi có hội thoại mới; giữ hội thoại 24 tháng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 164 = M4-24). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới trong `conversation/internal/history`: `OnNewConversation` (hook đường vào), `FetchHistoryKind`
    (`command.fb.fetch_history`), `RetentionSchedule` (`conversation.retention`). `history.Parts` (1 chỗ gọi:
    `conversation/wiring.go`) nay trả lệnh đọc lịch sử (khi có adapter `HistoryFetcher`), lịch và việc dọn.
  - Mới trong `conversation/internal/deps`: `PendingAttachments` (dời từ `inbound.pendingAttachments` không xuất, 3 chỗ
    gọi: `messages.go`, `comments.go`, `echo.go`) và `AdapterAs[T]` (hỏi năng lực tùy chọn của adapter; dùng ở
    `inbound/posts.go` `canFetchPosts` và `history.fetcherOf`) — commit cấu trúc riêng.
  - Không xuất: `inbound.onNewConversation` (2 chỗ gọi: `handler.go` `thread` — hộp thư, `comments.go`
    `commentThread` — comment) đổi từ hàm khung thành phương thức `(acc, id, inbox bool)` gọi
    `history.OnNewConversation` khi là hộp thư.
  - Gói gốc `conversation` (tệp mới `history.go`): hằng `FetchHistoryKind`, `RetentionSchedule`.
- Gói phụ thuộc ngược (go list): `history`, `inbound`, `deps` chỉ được gói gốc `conversation` nhập (deps còn được mọi
  phần nội bộ nhập); gói gốc được `cmd/crm`, `internal/httpapi` nhập. Test đổi: `conversation/wiring_test.go`,
  `cmd/crm/wire_m4_test.go` (thêm lệnh `command.fb.fetch_history`, lịch `conversation.retention`).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (màn hình M4 làm ở Task 174–178).
- Câu sqlc, bảng, pha migration:
  - Không thêm câu cho lịch sử: dùng lại `InsertInboundMessage` (ON CONFLICT DO NOTHING) và `GetConversationForSend`.
  - `queries/conversation/retention.sql` mới: `EnableRetention`, `ExpiredMessageBatch`, `ExpiredEmptyConversations`,
    `DeleteRevisionsOfMessages`, `DeleteExpiredMessages`, `DeleteEmptyConversations` (một câu: nhãn, phân công, hội
    thoại), `ReferencedFiles`; khóa hội thoại dùng lại `LockConversationsForReminders`.
  - `migrations/0414_conversation_retention.sql` (pha mở rộng, chỉ nới): ràng buộc `messages_direction_origin` cho
    `origin = history` cả hai chiều (DROP CONSTRAINT + ADD … NOT VALID + VALIDATE: nới, không cần dòng contract);
    trigger chỉ ghi thêm của `conversation_assignments` cho DELETE khi `app.retention = on`; GRANT DELETE trên
    `conversations`, `conversation_assignments`; index `messages_sent_at (tenant_id, sent_at)` và GIN
    `messages_attachment_files` (jsonb_path_ops) cho câu tìm tệp còn được tham chiếu.
- Test sẽ chạy: `go test -race ./internal/modules/conversation/...` (vùng không nằm trong HIGH_RISK_DIRS; files chỉ
  dùng `MarkDeleted`), `./cmd/crm/` (wire_m4), `./internal/platform/db/` (migrate, contract lint), `./tests/arch/`;
  ci-task chọn theo gói đổi.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands, queue,
  schedules, files, tenancy); `inbound/posts.go` (lệnh chỉ đọc `command.fb.fetch_post`, Task 154); `promotion`
  `birthday_run.go` (lịch theo giờ Việt Nam); `send/command.go`; `channel.HistoryFetcher` + `facebook.FetchHistory`
  (Task 146/148); `files.MarkDeleted`; `config.ConversationRetentionMonths` (đã có, worker).
- Dùng lại / viết mới vì:
  - Dùng lại: `commands.CommandKind`, `commands.DoneHook`, `Looker` (lệnh chỉ đọc: Lookup luôn `not_sent` như
    fetch_post), `channel.Account`, `channel.CredentialsFor`, `ClassifyError`, `files.MarkDeleted`,
    `schedules.Schedule{PerTenant}`, `queue.JobQueue.Enqueue` (lô tiếp theo), `realtime.Bus`, `access.PageScope`,
    `deps.Snippet` không cần (tin lịch sử không đổi đoạn trích).
  - Dời `pendingAttachments` sang `deps.PendingAttachments`: tin lịch sử phải có đúng định dạng danh sách đính kèm chờ
    tải như tin vào (cùng mục đích, cùng lý do đổi) — commit cấu trúc riêng.
  - Viết mới `fetchHistory.load`: khác `fetchPost.load` (đọc hội thoại để lấy PSID rồi tài khoản; fetch_post nhận mã
    tài khoản trong payload). Bảng đổi lỗi kênh → kết quả lệnh trông giống fetch_post nhưng khác nhánh vĩnh viễn
    (lịch sử: done rỗng); nếu jscpd báo trùng thì trả lời ở đây.
  - Giờ Việt Nam: `birthday_run.go` dùng `vnOffset` không xuất của promotion; history viết hằng riêng (một dòng),
    không gộp vì là logic lịch của module khác.

## Soát thiết kế

- 5.1 Chủ dữ liệu: tin, hội thoại, nhãn, phân công thuộc `conversation`; tệp thuộc `platform/files` (gọi
  `files.MarkDeleted`, public API).
- 5.2 Không ghi bảng module khác: chỉ `files.MarkDeleted`; đọc `channel` qua public API.
- 5.3 View: không đọc bảng module khác (reply_templates, reminders, call_logs cùng schema conversation).
- 5.4 Nguyên tử: chèn lô tin lịch sử + báo hiệu trong tx đổi lệnh sang done (OnDone); mỗi lô dọn một transaction.
- 5.5 Sau commit: xếp lệnh đọc lịch sử trong tx của tin vào (outbox), đọc Meta ngoài transaction.
- 5.6 Gọi ra ngoài: `command.fb.fetch_history` là lệnh (`in_doubt` khi không rõ; chỉ đọc nên Lookup trả `not_sent`,
  chạy lại vô hại).
- 5.7 Tenant: lệnh và việc dọn chạy trong `WithTenant`; bảng không đổi RLS.
- 5.8 Không HTTP: không nhập net/http.
- 5.9 Đường ghi API: không áp dụng (không có operation).
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: lệnh theo mẫu Task 12/154, lịch theo mẫu promotion birthday.
- 6.1 Đồng thời: chèn tin `ON CONFLICT (tenant_id, channel_account_id, external_id) DO NOTHING` (tin kích hoạt không
  nhân đôi); xóa có điều kiện `sent_at < cutoff`, hội thoại xóa khi câu DELETE thấy không còn tin (snapshot mới, sau
  khi đã khóa hội thoại nên không tin nào chen vào).
- 6.2 Gửi lại: DedupeKey `fetch_history:<conversationId>`; lô dọn tiếp theo DedupeKey theo mã việc.
- 6.3 Thứ tự khóa (ADR-46 nhóm 6 rồi 12b): khóa mọi hội thoại của lô (`FOR UPDATE`, id tăng) → xóa
  `message_revisions`, `messages` → xóa hội thoại rỗng đã khóa kèm `conversation_tag_links`, `conversation_assignments`
  → `platform.files`. Ngoại lệ ghi vào ADR-46: nhãn, phân công bị xóa sau tin nhưng chỉ là dòng của hội thoại đang giữ.
- 6.4 Lỗi tạm: việc nền chạy lại cả tx (queue); hàm chạy lại được (không cộng dồn).
- 6.5 READ COMMITTED + câu có điều kiện.
- 6.6 Việc nền lỗi: lịch thử của queue; lệnh lỗi vĩnh viễn → rejected → dead.
- 6.7 429: `ErrorRateLimited` → not_sent, tự thử lại trong hạn 1 giờ.
- 6.8 Chết giữa chừng: lệnh in_doubt → resolver gọi Lookup → not_sent → chạy lại (chỉ đọc).
- 6.9 Treo: lô 5.000 tin, handler 30 s; lệnh timeout 30 s.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: không thuộc vùng rủi ro cao; test bảng ca (25 tin → 20, chạy lại 0).
- 6.12 Dữ liệu cá nhân: kết quả lệnh (nội dung 20 tin) nằm ở `jobs.result` tới khi lịch dọn việc xong xóa (giống mọi
  kết quả lệnh); không vào log, last_error chỉ có mã lý do.
- 6.13 Quyền: báo hiệu đòi `conversation.read` trên Page; không có API.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Q33: hội thoại mới → 1 lệnh lịch sử; 25 tin trên Meta giả (20 khách, 4 Page, tin vừa nhận) → 20 tin: 19 `history` (4 `out`) + tin vừa nhận `customer` không nhân đôi; `unread_count` = 1; 0 lệnh khác (gửi, tự động); 2 báo hiệu (tin vào + một cho cả lô) | `TestFetchHistory_NewConversation` (history_test.go) | `go test -race -count=1 ./internal/modules/conversation/internal/history/` | `--- PASS: TestFetchHistory_NewConversation (0.66s)`; cả gói `ok … history 2.624s` |
| Hội thoại đã có nhận tin mới → không thêm lệnh lịch sử | `TestFetchHistory_ExistingConversation` | như trên | `--- PASS (0.65s)` |
| H11/Q17: tin 25 tháng bị xóa cùng lịch sử sửa; tệp chỉ nó dùng → `deleted_at`; tệp tin mới còn dùng → giữ; tin 23 tháng còn; hội thoại chỉ có tin quá hạn xóa cùng nhãn, phân công; hội thoại có hẹn gọi lại giữ; lần hai xóa 0 | `TestRetention` (retention_test.go) | như trên | `--- PASS: TestRetention (0.65s)` |
| Lô đầy → ghi việc lô tiếp theo; việc tiếp theo chạy cả ngoài khung giờ | `TestRetention_Continues` | như trên | `--- PASS (0.67s)` |
| Ngoài 2:00–2:59 giờ Việt Nam → `skipped: outside_hours`, không xóa | `TestRetention_OutsideHours` | như trên | `--- PASS (0.61s)` |
| `DELETE` trên `message_revisions` không bật cờ → lỗi 42501 | `TestRetention_RevisionsNeedFlag` | như trên | `--- PASS (0.67s)` |
| Nối dây: lệnh `command.fb.fetch_history` khi có facebook, lịch `conversation.retention` mỗi giờ theo tenant | `TestWiring`, `TestWireM4_Frame` | `go test -count=1 -run '^(TestWiring\|TestWireM4_Frame)$' ./internal/modules/conversation/ ./cmd/crm/` | `ok … conversation 1.498s`, `ok … cmd/crm 1.882s` |
| Ràng buộc nới không làm hỏng luật cũ (ghi chú không ra ngoài, chiều/nguồn, chỉ ghi thêm) | `TestSchema_*` (schema_test.go, 6 test) | `go test -race -count=1 -run '^(TestSchema_RulesViewsTenant\|TestSchema_ConversationChecks\|TestSchema_NoteNeverLeaves\|TestSchema_OnePrivateReplyPerComment\|TestSchema_AppendOnly\|TestSchema_ForeignKeys)$' ./internal/modules/conversation/` | `ok … conversation 2.263s` |
| Cổng của task | ci-task lần 2: build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard, console, test kiến trúc, test toàn bộ trừ e2e (nhánh có thay đổi testkit của m4) | `ci-task.sh` | mọi cổng tĩnh xanh, test kiến trúc 145 s xanh; test: mọi gói `ok` (conversation 39 s, history 3,5 s, inbound 7 s, send 74 s, cmd/crm 12 s, platform/db 31 s) trừ `platform/cache` `TestRateLimiter_KeysHaveNoPersonalData` (`khóa rl:* = [], muốn 2`) — gói task không chạm; chạy lại riêng 3 lần: `ok` cả 3 (Việc phát hiện thêm 6) |

Ghi chú: kế hoạch đòi "không dòng `automation_runs` nào" — bảng đó do Task 165 tạo (0407 chưa có trên m4); test đòi 0
lệnh `command.%` nào ngoài lệnh lịch sử, và OnDone không gọi hook tự động. Sau ci-task chỉ sửa một chỗ phòng thủ trong
`fetchHistory.Execute` (lấy adapter sau khi chắc có fetcher); gói history chạy lại: `ok 2.624s`, lint 0.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch không ghi migration; cần `0414_conversation_retention.sql` (Quyết định 1, 2): nới ràng buộc chiều/nguồn,
  cho phép DELETE khi bật cờ trên `conversation_assignments`, GRANT DELETE, hai index.
- Tin lịch sử dùng lại câu `InsertInboundMessage` có sẵn (không thêm `InsertHistoryMessage`); đọc hội thoại dùng lại
  `GetConversationForSend`, khóa hội thoại dùng lại `LockConversationsForReminders` (cùng việc: khóa theo id tăng).
- Thêm `conversation/history.go` (gói gốc công bố `FetchHistoryKind`, `RetentionSchedule` cho test nối dây) và dòng
  ngoại lệ của việc dọn trong `docs/adr/0046-thu-tu-khoa.md`.
- Thêm `deps.AdapterAs[T]` (hỏi năng lực tùy chọn của adapter): jscpd báo `fetcherOf` trùng `canFetchPosts`; cùng mục
  đích, cùng lý do đổi → gộp, `canFetchPosts` dùng nó. Cùng commit cấu trúc với `deps.PendingAttachments`.
- Lịch "02:30" chạy trong khung 2:00–2:59 (Quyết định 3).

## Quyết định thay chủ shop

1. **Tin lịch sử của khách mang `origin = history`, `direction = in`.** Spec Q33 đòi đánh dấu `origin = history`,
   nhưng ràng buộc `messages_direction_origin` (05, Task 151) đòi `in ⇔ customer` nên tin khách đọc lại không ghi được.
   Chọn: nới ràng buộc thành `origin = 'history' OR (direction = 'in') = (origin = 'customer')` (chỉ nới, dòng cũ vẫn
   thỏa; đảo ngược được bằng migration sau). Phương án khác: ghi tin khách là `customer` (mất dấu "lịch sử", trái Q33)
   hoặc ghi `out` (sai chiều).
2. **Xóa hội thoại quá hạn cần quyền xóa và cờ.** `conversation_assignments` chặn mọi DELETE; thêm nhánh DELETE khi
   `app.retention = on` (cùng cách của `message_revisions`, `messages`) và GRANT DELETE `conversations`,
   `conversation_assignments`. Hội thoại có `reminders` hay `call_logs` gắn vào thì **giữ** (không xóa dữ liệu
   gọi/hẹn mà kế hoạch không nhắc; crm_app không có quyền xóa/sửa hai bảng đó). `ord.orders.conversation_id` không có
   khóa ngoại: đơn giữ mã hội thoại đã xóa (đơn giữ lâu hơn hội thoại).
3. **Giờ chạy.** Lịch nền không neo được giờ cố định (kỳ đầu là lúc worker khởi động). Chọn như lịch quà sinh nhật:
   lịch mỗi giờ theo tenant, chỉ dọn khi giờ Việt Nam là 2 (2:00–2:59), nên mỗi ngày đúng một lượt quanh 02:xx. Lô đầy
   (5.000 tin hay 1.000 hội thoại) → ghi ngay việc lô tiếp theo (`{"continue": true}`, bỏ qua khung giờ). Phương án
   khác: thêm neo giờ vào `schedules` (đổi nền tảng, ngoài phạm vi).
4. **Tệp đính kèm.** Chỉ đánh dấu xóa tệp không còn tin nào (đính kèm `fileId`) hay mẫu trả lời nào (kể cả mẫu đã xóa
   mềm) dùng — ảnh mẫu gửi cho nhiều khách vẫn dùng ở tin mới. Ảnh đại diện hội thoại (`avatar_file_id`) chưa đụng.
5. **Tin lịch sử quá hạn giữ** (Meta trả tin cũ hơn 24 tháng) không chèn, để việc dọn không phải xóa lại.
6. **Nội dung 20 tin nằm ở `jobs.result`** của lệnh tới khi lịch dọn việc đã xong (`DeleteFinishedJobs`) xóa; đây là
   cách kế hoạch định (OnDone đọc kết quả của Execute), không vào log.
7. **Hook chỉ cho hội thoại hộp thư:** hội thoại comment mới không lấy lịch sử (Q33 nói hộp thư).

## Việc phát hiện thêm

1. Tin lịch sử có đính kèm được ghi danh sách chờ tải (`status: pending`) như tin vào; Task 155 (tải đính kèm) nối
   `OnInboundAttachments` chỉ cho đường vào — cần gọi cả cho tin lịch sử (`history.insertHistory`).
2. Hook xoay vòng (Task 160) vẫn chưa nối vào `inbound.onNewConversation` (đã ghi ở tệp task-160); hook giờ là phương
   thức có `h.d`, nối thêm một dòng.
3. Ảnh đại diện hội thoại bị xóa theo hạn giữ chưa được đánh dấu xóa tệp (Task 155 tạo ảnh đại diện; quyết định khi
   làm 155).
4. Tin trễ qua webhook mà lệnh lịch sử đã ghi trước (cùng `mid`) bị coi là trùng nên không tăng chưa đọc; chỉ xảy ra
   khi webhook trễ hơn lệnh đọc lịch sử.
5. `TestLintCleanOnRealCode` (tests/arch, chạy golangci-lint cả repo) quá 10 phút khi chạy tay lúc máy tải nặng
   (26/11); trong ci-task test kiến trúc xanh (145 giây).
6. `internal/platform/cache` `TestRateLimiter_KeysHaveNoPersonalData` đỏ một lần trong ci-task (máy tải nặng, quét
   `rl:*` ra rỗng), chạy lại riêng 3 lần đều qua: test chập chờn, cần điều phối viên xem.

## Reviewer

Commit 1/2 (cấu trúc: `deps.PendingAttachments`, `deps.AdapterAs`): **Kết luận: ĐẠT** — không phát hiện phải sửa;
định dạng JSON đính kèm, chuỗi bọc lỗi chép nguyên; đủ 3 chỗ gọi tên cũ đã đổi (grep `pendingAttachment` chỉ còn
deps.go); `canFetchPosts` trả cùng kết quả kể cả registry nil; bản index tự build, vet, test inbound `ok 6.4s`.

Commit 2/2 (hành vi): CHƯA REVIEW
