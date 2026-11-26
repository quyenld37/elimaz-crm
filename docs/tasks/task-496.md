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
  - `internal/platform/cache`: `ratelimit_test.go`, `conn_test.go` (hạn lệnh của kết nối thử).
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
| Commit 1 (cấu trúc): dời lệnh xếp tải tệp sang deps, không đổi hành vi (tên lệnh, payload, DedupeKey giữ nguyên); không còn tên cũ | `TestInboundAttachment_Refusals`, `TestInboundFile_TP04`; grep | `go build ./internal/modules/conversation/...`; `go test -race -count=1 -run '^(TestInboundAttachment_Refusals\|TestInboundFile_TP04)$' ./internal/modules/conversation/internal/inbound/`; `grep -rn "OnInboundAttachments\|attachmentPayload" internal cmd tests` | build qua; `ok …/inbound 3.338s`; grep 0 dòng |

## E2E viết sẵn, chưa chạy

(không có)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có phần kế hoạch riêng)

## Quyết định thay chủ shop

## Việc phát hiện thêm

(không có)

## Reviewer

Commit 1/2 (cấu trúc: dời lệnh xếp tải tệp sang deps): **Kết luận: ĐẠT** — không phát hiện phải sửa. Thân
`EnqueueAttachmentFetches` giống từng dòng `OnInboundAttachments`; tên lệnh, hạn, tag JSON, DedupeKey giữ nguyên; đủ 3
chỗ gọi; không vòng nhập mới (deps chỉ nhập channel, commands, queue vốn đã nhập).
