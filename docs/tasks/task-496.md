# Task 496 — Sửa sau gộp M4 đợt 1: xóa ảnh đại diện khi dọn 24 tháng, tệp lịch sử và tệp 0 byte, test chập chờn Valkey/thời gian

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (task sửa sau gộp M4 đợt 1, không có
phần kế hoạch riêng; nguồn việc: `docs/tasks/task-164.md` mục "Gộp vào m4" và "Việc phát hiện thêm", `task-155.md`,
`task-159.md`). Nhánh gốc `m4`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Commit cấu trúc: dời `inbound.FetchAttachmentKind` (2 chỗ dùng, cùng tệp `attachments.go`), `inbound.OnInboundAttachments`
    (3 chỗ gọi: `inbound/messages.go`, `inbound/comments.go`, `inbound/echo.go`) và kiểu không xuất `attachmentPayload`
    (`attachments.go`) sang gói nội bộ `conversation/internal/deps` (tệp mới `deps/attachments.go`):
    `deps.FetchAttachmentKind`, `deps.EnqueueAttachmentFetches`, `deps.AttachmentPayload`. Lý do: `history` phải xếp
    cùng lệnh tải mà không nhập `inbound` (inbound đã nhập history → vòng). Không đổi hành vi, không đổi tên lệnh, payload,
    DedupeKey.
  - Commit hành vi: không thêm, không đổi ký hiệu xuất ra ngoài. Đổi trong thân hàm: `history.insertHistory` (giữ URL,
    trả mã tin), `history.fetchHistory.OnDone` (xếp lệnh tải), `history.retentionHandler.sweep`/`markFiles` (ảnh đại
    diện), `inbound.fetchAttachment.Execute` (tệp 0 byte), `inbound.fetchProfile.avatar` (ảnh 0 byte).
- Gói phụ thuộc ngược (go list): `deps` được mọi phần của conversation nhập (assign, automation, calls, history,
  inbound, live, presence, reminders, send, templates, gói gốc); chỉ thêm ký hiệu, các gói đó không sửa (send, live không
  chạm). `inbound`, `history` chỉ gói gốc `conversation` nhập; gói gốc ← `cmd/crm`, `internal/httpapi`.
- Operation OpenAPI bị chạm, oasdiff: không có. Trạng thái mục đính kèm (`ChatAttachment.status`) là chuỗi tự do, không
  thêm giá trị mới (tệp 0 byte dùng `unavailable`, Quyết định 2).
- Chỗ dùng trên màn hình: không có (màn hình M4 ở Task 174–178).
- Câu sqlc, bảng, pha migration:
  - `queries/conversation/retention.sql`: `DeleteEmptyConversations` trả thêm `avatar_file_id`; `ReferencedFiles` coi
    thêm tệp là ảnh đại diện của một hội thoại còn lại (`conversations.avatar_file_id`). Sinh lại
    `conversation/internal/store` bằng `make gen`. Bảng đọc: `conversation.conversations`; ghi: `platform.files` qua
    `files.MarkDeleted` (như 164).
  - Không có migration (câu mới dùng `IN (SELECT … )` thành hashed subplan một lần quét; không cần index mới).
- Test chập chờn (chỉ sửa test, không đổi mã chạy):
  - `internal/platform/cache`: `ratelimit_test.go`, `conn_test.go` (hạn lệnh của kết nối thử), `readcache_test.go`
    (hạn lệnh qua `dial`; `FallbackBusyConcurrent` chờ theo điều kiện).
  - `internal/platform/observability/deephealth_valkey_test.go`, `internal/wiring/webcache/invalidator_test.go` (hạn
    lệnh của kết nối thử).
  - `internal/httpapi/web/catalog_test.go` (`valkeyCache`, dùng bởi `TestWebShippingQuote`, catalog, shipping…).
  - `internal/httpapi/coupon_guard_test.go` (giới hạn tốc độ qua Valkey).
  - `internal/modules/fulfillment/quote_test.go`, `webcarriers_test.go` (ngưỡng giờ thật).
  - Không chạm `testkit` (đổi testkit làm ci-task chạy toàn bộ), không chạm `conversation/internal/send`, `live`.
- Test sẽ chạy: test mới/sửa của `history` (`TestRetention`, `TestFetchHistory_AttachmentsDownloaded`), `inbound`
  (`TestInboundAttachment_EmptyFile`, `TestFetchProfile_EmptyAvatar`, test cũ của attachments/profile), các test cache,
  web, httpapi, fulfillment vừa sửa (`-run` từng test trên máy); cả gói qua ci-task ở bể.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (files `MarkDeleted`;
  queue `Enqueue`; commands `CommandKind`); trong conversation: `deps.PendingAttachments`, `deps.WithoutURLs`,
  `inbound.OnInboundAttachments`, `history.markFiles`, `ReferencedFiles`; trong test: `fileEnv` (inbound),
  `env` (history: `thread`, `message`, `file`), `fakeClock` (cache), `quoteCarrier` (fulfillment).
- Dùng lại / viết mới vì:
  - Ảnh đại diện: dùng lại nguyên `markFiles` + `ReferencedFiles` của 164 (cùng luật "không xóa tệp còn chỗ khác dùng"),
    chỉ thêm nguồn mã tệp (`avatar_file_id` của hội thoại vừa xóa) và một chỗ dùng vào câu kiểm.
  - Tin lịch sử: dùng lại đúng lệnh tải của 155 (dời `OnInboundAttachments` sang deps, cùng mục đích, cùng lý do đổi),
    không viết đường tải thứ hai.
  - Tệp 0 byte: dùng lại `refuse` (ghi trạng thái mục + rejected) và trạng thái `unavailable` có sẵn.
  - Test chập chờn: dùng `fakeClock`, hạn lệnh `OpTimeout` có sẵn của `cache.DialOptions`; hằng hạn lệnh của test khai
    riêng ở mỗi gói test (một dòng; không đưa vào testkit để không buộc ci-task chạy toàn bộ).

## Soát thiết kế

- 5.1 Chủ dữ liệu: hội thoại, tin thuộc conversation; tệp thuộc `platform/files` (chỉ gọi `files.MarkDeleted`).
- 5.2 Không ghi bảng module khác: chỉ qua `files.MarkDeleted`, `queue.Enqueue`.
- 5.3 View: không đọc bảng module khác (chỉ bảng của conversation).
- 5.4 Nguyên tử: đánh dấu xóa ảnh đại diện trong cùng transaction lô dọn (như tệp đính kèm); xếp lệnh tải tệp của tin
  lịch sử trong tx OnDone của lệnh lịch sử (cùng tx chèn tin).
- 5.5 Sau commit: lệnh tải là việc của hàng đợi (outbox), chạy sau khi tx OnDone commit.
- 5.6 Gọi ra ngoài: lệnh tải chỉ đọc của 155 (Lookup not_sent), không đổi.
- 5.7 Tenant: mọi câu trong tx tenant của việc/lệnh; không bảng mới.
- 5.8 Không HTTP: không nhập net/http.
- 5.9 Đường ghi API: không áp dụng (không có operation).
- 5.10 Valkey: chỉ sửa test; Valkey vẫn chỉ đếm, đệm.
- 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: theo 155 (lệnh tải), 164 (dọn).
- 6.1 Đồng thời: lô dọn đã khóa hội thoại (id tăng) trước khi xóa; `ReferencedFiles` chạy sau khi xóa trong cùng tx nên
  hội thoại vừa xóa không tính là chỗ dùng; ảnh đại diện mới của hội thoại khác chỉ được ghi bởi lệnh hồ sơ (tệp mới,
  mã mới), không dùng lại mã tệp cũ. Lệnh tải tệp lịch sử: DedupeKey `<msgID>:<chỉ số>` như tin vào; tin đã có
  (ON CONFLICT DO NOTHING) không xếp lệnh.
- 6.2 Gửi lại: OnDone chạy lại (RetryTx) thì tin đã chèn ở lần lùi không còn, chèn lại và xếp lại (DedupeKey chống trùng).
- 6.3 Thứ tự khóa (ADR-46): hội thoại (nhóm 6) → tin → platform.files (12b) → platform.jobs (13) như 155, 164; không thêm
  khóa mới.
- 6.4 Lỗi tạm: việc nền chạy lại cả tx; hàm gán lại biến kết quả.
- 6.5 READ COMMITTED + câu có điều kiện (`PatchPendingAttachment` chỉ khi mục còn pending).
- 6.6 Việc nền lỗi: tệp 0 byte → `rejected` (dead ngay, không in_doubt), mục `unavailable`.
- 6.7 429: không đổi.
- 6.8 Chết giữa chừng: không đổi (lệnh chỉ đọc).
- 6.9 Treo: không đổi.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: không thuộc vùng rủi ro cao (files chỉ dùng `MarkDeleted`); test bảng ca.
- 6.12 Dữ liệu cá nhân: ảnh đại diện là dữ liệu cá nhân — nay được đánh dấu xóa đúng hạn 24 tháng. URL có chữ ký của tin
  lịch sử nằm trong `messages.attachments` tới khi lệnh tải xong (như tin vào, Quyết định 4 của 155) và trong
  `jobs.result` của lệnh lịch sử (đã như vậy từ 164); last_error chỉ mã lý do.
- 6.13 Quyền: không đổi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Việc 1 — ảnh đại diện: 4 hội thoại quá hạn bị xóa (3 có tin 26 tháng, 1 rỗng sẵn), mỗi cái một ảnh đại diện → tệp chỉ nó dùng (2) có `deleted_at`; tệp còn là ảnh đại diện của hội thoại giữ, còn là đính kèm của tin còn lại, ảnh của hội thoại giữ → giữ; `files = 2`; lần hai 0 | `TestRetention_Avatars` (retention_test.go) | `go test -race -count=1 -run '^(TestFetchHistory_AttachmentsDownloaded\|TestRetention_Avatars\|TestRetention\|TestFetchHistory_NewConversation\|TestFetchHistory_ExistingConversation\|TestRetention_Continues\|TestRetention_OutsideHours\|TestRetention_RevisionsNeedFlag)$' ./internal/modules/conversation/internal/history/` | trước sửa: `FAIL … {Messages:3 Conversations:4 Files:0 …}`; sau: `--- PASS: TestRetention_Avatars (0.86s)`; 8 test PASS, `ok …/history 2.553s` |
| Việc 2 — tin lịch sử có đính kèm: OnDone ghi mục pending giữ URL, đúng 1 lệnh `command.fb.fetch_attachment` (payload `{messageId, index: 0}`, khóa `<msgID>:0`; sticker không URL không lệnh; tin đã có cùng mã ngoài không thêm lệnh); worker chạy lệnh → mục ảnh `blocked` (máy lạ), URL bị xóa, sticker giữ pending | `TestFetchHistory_AttachmentsDownloaded` (history_test.go) | như trên | trước sửa: `FAIL … [{Status:pending URL:} …], muốn … mục ảnh có URL`; sau: `--- PASS (0.93s)`; `TestFetchHistory_NewConversation`, `_ExistingConversation` PASS |
| Việc 3 — tệp 0 byte: mục `unavailable` (không pending mãi), lệnh dead `rejected: empty_file`, 0 in_doubt, 0 dòng tệp | `TestInboundAttachment_EmptyFile` (attachments_test.go) | `go test -race -count=1 -run '^(TestInboundAttachment_EmptyFile\|TestFetchProfile_EmptyAvatar\|TestFetchProfile_CH08\|TestInboundAttachment_Refusals\|TestInboundFile_TP04\|TestInboundImage_TP02)$' ./internal/modules/conversation/internal/inbound/` | trước sửa: `FAIL … Status:pending … URL:https://cdn.fakechat.test/f/rong.pdf`; sau: `ok …/inbound 2.915s` |
| Việc 3 — ảnh đại diện 0 byte: tên vẫn đặt, không ảnh, lệnh hồ sơ done, 0 in_doubt/dead, 0 dòng tệp | `TestFetchProfile_EmptyAvatar` (profile_test.go) | như trên | PASS cả trước sửa (thân rỗng nhận dạng `text/plain`, không phải ảnh, nên đã bị bỏ); giữ làm chốt, thêm kiểm `Size <= 0` tường minh |
| Việc 4 — cache: test cần Valkey trả lời dùng hạn lệnh 2 s (`testOpTimeout`), `TestConn_OpTimeout` vẫn kiểm hạn mặc định 50 ms (`dialOp(…, 0)`); `TestReadCache_FallbackBusyConcurrent` chờ theo điều kiện, câu kiểm chặt lại đúng 2 thành công / 3 bận | `TestRateLimiter_KeysHaveNoPersonalData`, `_ValkeyFixedWindow`, `_KeyExpires`, `_FallbackInProcess`, `_PeekDoesNotCount`, `TestConn_OpTimeout`, `TestConn_ServerDown`, `TestReadCache_HitMissInvalidateBump`, `_ValkeyDownFallsBack`, `_FallbackBusyConcurrent` | `go test -race -count=1 -run '^(…10 test trên…)$' ./internal/platform/cache/` | `ok …/platform/cache 10.104s` |
| Việc 4 — web, coupon, health, webcache: kết nối Valkey của test có hạn lệnh 2 s (`valkeyTestOpTimeout`) | `TestWebShippingQuote`, `TestCouponGuard_ValkeyDownFallback`, `TestDeepHealthValkey`, `TestEndToEnd_PriceChangedInvalidatesProduct` | `go test [-race] -count=1 -run '^<tên>$'` từng gói | `ok …/httpapi/web 2.696s`, `ok …/httpapi 5.600s`, `ok …/observability 1.254s`, `ok …/webcache 0.876s` |
| Việc 4 — fulfillment: bỏ ngưỡng giờ thật (≤ 3,5 s; 9–11 s); kiểm hạn ctx hãng giả nhận (≤ 3 s; kẹp: hạn − lúc gọi ≤ 10 s và hạn − lúc bắt đầu ≥ 10 s, một đồng hồ đơn điệu) và lần treo bị ctx cắt | `TestQuoteNow_ParallelTimeoutNoDB`, `TestQuoteNow_TimeoutClamped`, `TestQuoteForWeb_PreferredAndTimeout` | `go test -race -count=1 -run '^(…)$' ./internal/modules/fulfillment/` | `ok …/fulfillment 11.714s`; `--- PASS: TestQuoteForWeb_PreferredAndTimeout (3.44s)` |
| Tự soát trước ci-task (sau gộp m4 4e8cd0e5, không xung đột) | — | `make gen` (git status không đổi); `go build ./...`; `go vet`, `golangci-lint run` các gói đổi; `make jscpd`; `go test -count=1 ./tests/arch/` | qua; lint 0 (sau khi thêm `//nolint:wrapcheck` cho 3 chỗ gọi của commit 1); jscpd `Found 0 clones`; `ok tests/arch 144.942s` |
| Cổng của task | cổng tĩnh + test -race các gói đổi + test thường gói phụ thuộc (conversation/…, cmd/crm, files, realtime) | ci-task (tree 1c021b79, điểm tách 4e8cd0e5) | run 45813266241 (blacksmith): plan, static success; test (2) success; test (1) failure chỉ `TestSendProperty_AtMostOncePerClientRef` (gói `conversation/internal/send`: task này không sửa; đã ghi chập chờn trên m4 ở tệp task-164 lượt gộp 1; để đợt 2 theo giao việc) |
| Việc 4 — không chập chờn khi chạy lặp ở bể: cùng tree 1c021b79 chạy thêm 2 lượt song song (8 gói: cache, httpapi/web, httpapi, fulfillment, observability, webcache, history, inbound; tất cả -race), cộng lượt ci-task là 3 lượt xanh | các gói nêu bên | `CRM_CI_STATIC=false merge-test.sh 1c021b79 HEAD <tệp 8 gói> <tệp 8 gói>` ×2 | run 45814037163 `MERGE-TEST XANH (blacksmith)` (test 1, 2 success); run 45814080473 `MERGE-TEST XANH (blacksmith)` (test 1, 2 success) |
| Commit 1 (cấu trúc): dời lệnh xếp tải tệp sang deps, không đổi hành vi (tên lệnh, payload, DedupeKey giữ nguyên); không còn tên cũ | `TestInboundAttachment_Refusals`, `TestInboundFile_TP04`; grep | `go build ./internal/modules/conversation/...`; `go test -race -count=1 -run '^(TestInboundAttachment_Refusals\|TestInboundFile_TP04)$' ./internal/modules/conversation/internal/inbound/`; `grep -rn "OnInboundAttachments\|attachmentPayload" internal cmd tests` | build qua; `ok …/inbound 3.338s`; grep 0 dòng |

## E2E viết sẵn, chưa chạy

(không có)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không có phần kế hoạch riêng (task sửa sau gộp). Commit 2 thêm `//nolint:wrapcheck` cho 3 chỗ gọi
  `deps.EnqueueAttachmentFetches` (lint báo sau commit 1; chỉ chú thích, không đổi mã).
- Env thử của `history` có thêm kho tệp trong bộ nhớ (lệnh tải tệp cần kho; trước đây tin lịch sử không xếp lệnh tải).

## Quyết định thay chủ shop

1. **Ảnh đại diện còn chỗ dùng thì giữ**, theo đúng luật tệp đính kèm của 164: không đánh dấu xóa khi tệp còn là ảnh
   đại diện của một hội thoại còn lại, đính kèm của một tin còn lại hay tệp của mẫu trả lời. Ảnh đại diện gắn với hội
   thoại (`conversations.avatar_file_id`); khách không còn hội thoại nào thì ảnh đã theo hội thoại cuối bị xóa. Không
   thêm migration: câu kiểm dùng `IN (SELECT avatar_file_id …)` (một lần quét mỗi lô, lịch chạy đêm). Phương án khác:
   index một phần trên `avatar_file_id` (thêm migration) — làm khi bảng lớn.
2. **Tệp 0 byte → mục `unavailable`, lệnh `rejected` lý do `empty_file`** (dead ngay, không in_doubt). Dùng trạng thái
   có sẵn (`ChatAttachment.status` là chuỗi tự do, màn hình M4 chưa làm) thay vì thêm `empty`. Phương án khác: trạng
   thái riêng `empty` (đổi một hằng).
3. **Tin lịch sử của Page có đính kèm cũng được tải** (như echo của ứng dụng khác, Task 155): tin Page cũ trước khi có
   CRM hay từ Business Suite không có tệp ở CRM. Phương án khác: chỉ tải tin của khách.
4. **Test chập chờn sửa gốc, không chạm testkit:** kết nối Valkey trong test có hạn lệnh 2 s (hằng riêng mỗi gói test;
   đưa vào testkit sẽ buộc ci-task chạy toàn bộ); mã chạy thật giữ 50 ms. Test fulfillment kiểm hạn ctx mà hãng giả
   nhận thay cho thời gian chạy cả lời gọi. Không tắt test, không bỏ câu kiểm; `FallbackBusyConcurrent` chặt hơn trước
   (đúng 2/3, trước cho phép ≥ 2).

## Việc phát hiện thêm

1. `conversation/internal/send` `TestSendProperty_AtMostOncePerClientRef` vẫn chập chờn (đỏ ở ci-task của task này;
   gói không sửa) — đợt 2 theo giao việc.
2. `cmd/crm/webcache_test.go` dựng bộ đệm qua `newWebCache` (hạn lệnh 50 ms của mã thật) nên có thể chập chờn cùng kiểu
   khi máy tải; chưa sửa (cần đường đặt hạn cho test qua cấu hình, ngoài các gói được giao).
3. `fulfillment` `TestLabels_RetryThenTerminal` ("chưa có mã hãng", task-119): nghi lệnh tạo vận đơn lỗi tạm khi máy tải
   rồi hẹn thử lại 4 s mà `DrainDue` không chạy; chưa đủ chứng cứ để sửa, để đợt 2.
4. `SetConversationAvatar` thay ảnh đại diện không đánh dấu xóa tệp ảnh cũ (lệnh hồ sơ hiện chỉ chạy một lần cho khách
   mới nên chưa gặp).

## Reviewer

Commit 1/2 (cấu trúc: dời lệnh xếp tải tệp sang deps): **Kết luận: ĐẠT** — không phát hiện phải sửa. Thân
`EnqueueAttachmentFetches` giống từng dòng `OnInboundAttachments`; tên lệnh, hạn, tag JSON, DedupeKey giữ nguyên; đủ 3
chỗ gọi; không vòng nhập mới (deps chỉ nhập channel, commands, queue vốn đã nhập).

Commit 2/2 (hành vi: ảnh đại diện khi dọn 24 tháng, tải tệp của tin lịch sử, tệp 0 byte, test chập chờn; sau gộp m4
4e8cd0e5): **Kết luận: ĐẠT** — không phát hiện phải sửa. Đã soát: tin đã có trả về trước khi xếp lệnh tải; mã sinh
khớp SQL, `avatar_file_id` chỉ có ở `conversations`; `markFiles` vẫn sắp id tăng trước khi khóa `platform.files`
(ADR-46 12b), OnDone lịch sử chỉ thêm dòng `platform.jobs`; `Size` của bộ tải là độ dài thật nên tệp thật không bị từ
chối nhầm; không `t.Skip`, không bỏ hay nới câu kiểm (`FallbackBusyConcurrent` chặt hơn, câu thời gian fulfillment
thay bằng câu tương đương trên một đồng hồ, đọc sau `wg.Wait()` nên không tranh chấp); test dừng Valkey bị từ chối kết
nối ngay nên hạn 2 s không đổi chúng. Ghi chú không bắt buộc (đã làm): thêm `readcache_test.go`,
`deephealth_valkey_test.go`, `invalidator_test.go` vào mục Ảnh hưởng.
