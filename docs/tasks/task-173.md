# Task 173 — Hoàn tất đơn nháp cạnh khung chat, danh sách đã chốt thiếu địa chỉ, nhắc một chạm, nháp hết hạn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 173 = M4-33). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào đã có. Thêm mới ở gói gốc `conversation`
  (tệp `livepending.go`): `ListPendingDrafts`, `RemindPendingDrafts`, `PendingDraftList`, `PendingDraft`,
  `RemindInput`, `RemindOutcome`, hằng `RemindQueued`, `RemindSkipped`, `RemindNotDraftText`,
  `RemindNotInSessionText`, `RemindNoChannelText`; `ClaimLiveComment` dời closure điền mẫu ra hàm `renderForOrder`
  (dùng chung, cùng hành vi); phần `internal/live` (`pending.go`, `remind.go`): `Pending`, `Remind`, `PendingList`,
  `PendingRow`, `CanRemind`, `RemindEnv`, `RemindInput`, `RemindOutcome`, `RemindEvery`, `NotDraftText`,
  `NotInSessionText`, `NoChannelText`; phần `internal/send` (`ask.go`): `PlanRemind`, `RemindPlan` (Allowed, Reason,
  Deliver), `RemindCheck`, `RemindText` — tách bước chọn đường của `AskAddress` ra `planAsk`/`deliver`, cùng hành vi
  (`AskAddress` có 1 chỗ gọi: `live.Claim`). Tiện ích thử: `livetest.Available`, `livetest.PrivateReplies` (thay thân
  `claimEnv.available`, `privateCalls`); e2e: `startHub`, `postFacebookMessageText` (`postFacebookMessage` gọi lại).
- Gói phụ thuộc ngược (go list): `internal/send` ← `conversation`, `automation`, `live`; `internal/live` ←
  `conversation`, `inbound`; `conversation` ← `internal/httpapi`, `cmd/crm`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: hiện thực 2 operation đã khai ở Task 143 (`listPendingDrafts`,
  `remindPendingDrafts`), xóa 2 dòng cuối của `tests/arch/testdata/m4_pending.txt`. Không đổi `openapi.yaml` (oasdiff
  không đổi). Lỗi dùng mã đã có (`not_found`, `validation_failed`, 403 guard). Hoàn tất (LV04) dùng `updateOrder`,
  `confirmOrder` của M2, không thêm route.
- Chỗ dùng trên màn hình: chưa có (màn Live và danh sách thiếu địa chỉ là Task 177).
- Câu sqlc, bảng, pha migration: không migration (cột `remind_count`, `last_reminded_at` đã có ở `0410`). Câu sqlc mới
  `queries/conversation/pending.sql`: `PendingDraftsOfSession`, `SessionClaimCounts`, `SessionOrderClaims`,
  `MarkReminded`, `LastRemindedAt` — đọc `conversation.live_comment_claims`, `messages`, `conversations`, view `ord.orders_v`,
  `customer.customers_v`, `identity.users_v` (view đã công bố, Q1); ghi chỉ `live_comment_claims` (bảng của
  conversation). Tin nhắc ghi qua `send` (bảng `messages`, `platform.jobs` qua commands).
- Test sẽ chạy: `live/{pending_test,remind_test}.go` (LV05, LV06, chính sách nhắc), `live.TestClaim_AskAddress`
  (sau khi tách `planAsk`), `send.TestPrivateReply*`, `httpapi/handlers_live_pending_test.go` (403/404, LV04 đường
  HTTP trọn với PATCH + confirm), `TestM4Contract_StatusCodes`, `tests/arch`; `tests/e2e/live_flow_test.go` viết sẵn,
  không chạy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tenancy, commands,
  realtime); trong conversation: `send.AskAddress` (askPrivate, insert, allowed, policyFor, privateState,
  latestPrivate, ensureInbox, FailReasonText), `send.InboxFor`, `live.Claim` (LockConversationsByID, openDrafts,
  DraftItems), `templates.Render`, `access.Require`; order: `ExpireDrafts` (lịch `order.expire_drafts`), `Edit`,
  `Confirm`; httpapi: `refusableWrite`, `inventoryRead`, `cardInfoOut`, `claimRefOut`, `vnZone`.
- Dùng lại / viết mới vì: nhắc dùng lại nguyên khuôn gửi của xin địa chỉ (Task 172): tách phần chọn đường
  (chỉ xem → nhắn riêng/tin thường → bỏ) của `AskAddress` thành `planAsk` có tham số thứ tự (xin địa chỉ: nhắn riêng
  trước; nhắc: hộp thư trước theo kế hoạch), `PlanRemind` chỉ là thứ tự khác cùng hàm gửi, `RemindCheck` là
  `planAsk` không khóa, không ghi (cột `canRemind` của danh sách) — không lặp mã chính sách. Khóa hội thoại dùng lại
  `LockConversationsByID`. Dòng đơn dùng lại `DraftItems`. Hết hạn (LV06) không viết gì mới: lịch M2 có sẵn. Câu
  SQL danh sách mới vì chưa có câu nào gom ghi nhận theo đơn.

## Soát thiết kế

- 5.1 chủ dữ liệu: ghi nhận nhắc (`remind_count`, `last_reminded_at`) thuộc `conversation`; đơn thuộc `order` (chỉ
  đọc qua `ord.orders_v`; hoàn tất, hết hạn là đường của M2).
- 5.2 không ghi bảng module khác: đúng (chỉ `live_comment_claims`, `messages`, lệnh gửi).
- 5.3 đọc module khác qua view đã công bố `ord.orders_v`, `ord.order_items_v`, `customer.customers_v`,
  `identity.users_v`.
- 5.4 nguyên tử: một `WriteTx` cho cả lần nhắc: khóa hội thoại → mỗi đơn: chọn đường → câu UPDATE có điều kiện
  (60 phút) → tin + lệnh. Đơn bị bỏ (không còn nháp, đã nhắc, chính sách) không ghi gì và không chặn đơn khác; lỗi
  hạ tầng lùi cả lần (RetryTx chạy lại), không có trạng thái nửa chừng.
- 5.5/5.6 gọi ra ngoài: tin nhắc là lệnh `command.fb.send_message` / `command.fb.private_reply` của Task 156/158
  (tin của nhân viên bấm, `sender_staff_id` = người bấm), ghi trong transaction, chạy sau commit; mỗi đơn một lệnh.
- 5.7 tenant: mọi câu trong `WriteTx` / `inventoryRead` (tenant), RLS của bảng đã có.
- 5.8 không HTTP trong module: handler mỏng `handlers_live_pending.go`.
- 5.9 đường ghi: route nhắc có `Idempotency-Key`, `x-permission: live.operate` (Task 143) → bấm lại cùng khóa trả
  cùng kết quả; khóa mới trong 60 phút → mọi đơn `skipped` "Đã nhắc lúc …".
- 5.10 Valkey: không áp dụng. 5.11 thư viện: không thêm. 5.12 module mẫu: public API ở gói gốc, sqlc, test PostgreSQL
  thật (đồng hồ giả: `now` truyền vào).
- 6.1 đồng thời — thứ tự khóa ADR-46: (1) mọi hội thoại comment của các đơn được nhắc và hộp thư của khách, một câu
  `ORDER BY id FOR UPDATE` (nhóm 6) → (2) dòng ghi nhận chính của đơn: đọc lại `last_reminded_at` sau khóa, rồi
  `UPDATE … WHERE last_reminded_at IS NULL OR ≤ now − 60 phút` làm chốt thứ hai (0 dòng → bỏ "đã nhắc") → (3) `messages` dòng mới + `jobs` (13). Không khóa khách (đường gửi tin
  không khóa khách, như Task 156). Hai người bấm nhắc cùng lúc: người sau chờ ở khóa hội thoại, câu UPDATE đọc lại
  dòng mới (READ COMMITTED) → 0 dòng → bỏ; có test hai lần nhắc song song chỉ một tin mỗi đơn.
- 6.2 gửi lại: `Idempotency-Key` của M0; khóa khác trong 60 phút chặn bằng câu UPDATE có điều kiện.
- 6.3 khóa: như 6.1. 6.4 lỗi tạm: `WriteTx` chạy `db.RetryTx`; thân hàm dựng lại danh sách kết quả mỗi lần.
- 6.5 READ COMMITTED + khóa dòng. 6.6 việc nền: lệnh gửi giữ nguyên chính sách (hết hạn, `in_doubt`); hết hạn nháp
  là lịch `order.expire_drafts` của M2 (bỏ đơn đang bị khóa để kỳ sau; chạy lại không đổi gì — có test).
- 6.7 429: không gọi ngoài trong transaction. 6.8 tiến trình chết: transaction lùi; lệnh chạy lại theo khuôn commands.
- 6.9 treo lâu: một transaction, tối đa 500 đơn mỗi lần (giới hạn của hợp đồng), không chờ mạng.
- 6.10 tiền: `grandTotal` đọc từ đơn (int64 đồng), không tính.
- 6.11 quy luật: không thêm rapid (không có bất biến số học mới); bất biến "mỗi đơn ≤ 1 tin nhắc trong 60 phút" có
  test song song.
- 6.12 dữ liệu cá nhân: không log nội dung tin, tên, SĐT; danh sách chỉ trả cho người có `conversation.read` trên Page
  của phiên.
- 6.13 quyền: danh sách cần `conversation.read` trên Page của phiên (không có → 404); nhắc cần `live.operate` trên
  Page (không có ở Page nào → 403 guard; Page khác → 404). Hoàn tất dùng quyền của M2 (`order.update`,
  `order.confirm`).

## Bằng chứng

Test viết trước mã: lần chạy đầu không biên dịch (`undefined: conversation.PendingDraftList`, `ListPendingDrafts`,
`RemindPendingDrafts`… — LSP và `go vet`), tức FAIL trước khi có mã.

Lệnh A: `go test -race -count=1 -run '^(TestPending_.*|TestRemind_.*|TestClaim_AskAddress|TestClaim_LV03)$' ./internal/modules/conversation/internal/live/` → `ok … live 2.839s` (sau gộp m4).
Lệnh B: `go test -race -count=1 -run '^(TestLivePendingAPI_.*|TestLiveClaimsAPI_.*)$' ./internal/httpapi/` → `ok … httpapi 2.808s` (sau gộp m4).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| LV05 danh sách: 5 lần chốt (3 nháp, 1 xác nhận bằng Edit địa chỉ + Confirm của M2, 1 hết hạn qua `ExpireDrafts`) → 3 dòng theo giờ chốt, `confirmedCount` 1, `expiredCount` 1; mã đơn, khách, dòng, tổng, hạn nháp, người chốt An, ask queued; `customerRepliedAfterAsk` chỉ đúng với khách nhắn hộp thư sau lúc chốt; `canRemind` cửa sổ còn → được, quá 24 giờ + đã nhắn riêng → không, lý do; xác nhận giữ nguyên có thể bán, hết hạn trả 1; Page không được xem → ErrNotFound | `live.TestPending_ListCounts` | A | ok |
| LV05 nhắc: "Nhắc tất cả" → 3 kết quả: 2 queued (tin thường trong hộp thư, người gửi An, có mã đơn, remind_count 1, last_reminded_at = lúc bấm), 1 skipped "Đã quá 24 giờ và đã nhắn riêng cho comment này" (không ghi nhắc); worker → Meta giả nhận đúng 1+1 tin thường, 0 cho khách thứ ba; bấm lại sau 10 phút → "Đã nhắc lúc HH:MM", không tin mới; 61 phút sau nhắc lại được (remind_count 2); đơn đã xác nhận → "Đơn không còn là nháp", đơn lạ → "Đơn không có trong phiên live này", không chặn đơn khác | `live.TestRemind_LV05` | A | ok |
| Hai người bấm nhắc song song (10 lượt) → mỗi đơn đúng 1 tin; không lỗi; deadlock không tăng | `live.TestRemind_ConcurrentOneMessagePerOrder` | A | ok |
| Chính sách nhắc: quá 24 giờ mà comment chưa nhắn riêng → tin private_reply queued; quá 7 ngày, không hộp thư → skipped có "7 ngày", "hộp thư"; Page chỉ xem → skipped "chỉ xem"; không live.operate, phiên lạ → ErrNotFound; mẫu lạ → ErrNotFound, không ghi tin | `live.TestRemind_Policy` | A | ok |
| LV06: draft_ttl_minutes 60 → hạn nháp ≈ now + 60 phút; 59 phút: không hủy, còn trong danh sách; 61 phút: cancelled draft_expired, có thể bán về 5, danh sách rỗng, expiredCount 1; chạy lại: 0 đơn, không đổi | `live.TestPending_DraftExpires` | A | ok |
| Xin địa chỉ sau khi tách `planAsk` (hành vi cũ giữ nguyên) | `live.TestClaim_AskAddress`, `live.TestClaim_LV03`, `send.TestPrivateReply*`, `send.TestRecover*` | A; `go test -race -count=1 -run '^(TestPrivateReply.*\|TestRecover.*)' ./internal/modules/conversation/internal/send/` | ok; `ok … send 2.750s` |
| LV04 (HTTP, đường trọn trong gói): comment → chốt (có thể bán 3) → khách nhắn hộp thư → danh sách có đơn, customerRepliedAfterAsk true, claimedBy An, grandTotal 240000, canRemind false (Page chỉ xem) → nhắc → 200 skipped "Page đang ở chế độ chỉ xem…" → PATCH địa chỉ (version) → confirm → confirmed, có thể bán vẫn 3, danh sách rỗng, confirmedCount 1; không live.operate → 403; Page khác → 404 (danh sách, nhắc); phiên lạ 404; thiếu templateId → 400 | `httpapi.TestLivePendingAPI_CompleteAndRemind` | B | ok |
| Hợp đồng M4 (2 dòng m4_pending đã xóa), kiến trúc | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok … tests/arch 136.754s` (trước gộp m4); sau gộp chạy trong ci-task |
| Cổng tĩnh rẻ | lint, jscpd | `golangci-lint run ./internal/modules/conversation/... ./internal/httpapi/ ./internal/testkit/livetest/ ./tests/e2e/`; `make jscpd` | `0 issues`; `Found 0 clones` (lần đầu 5 mã lặp → gộp vào `livetest.Available`, `livetest.PrivateReplies`, `fiveClaims`, `startHub`, `uniq`) |
| Cổng của task | ci-task | `scripts/agent/ci-task.sh` | `REMOTE-TEST XANH` run 45824649444 (static, test (1), test (2) success, 14 phút blacksmith); `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh e8c7d8fe…)` |

## E2E viết sẵn, chưa chạy

- `tests/e2e/live_flow_test.go`: `TestLiveFlow_CommentToConfirmedOrder` (LV04 đường trọn qua api, ingest có chữ ký,
  worker gửi tin riêng tới máy Meta giả). Biên dịch, qua vet và lint; chưa chạy theo luật chủ shop 24/11. Cùng các ca
  đã có ở `httpapi.TestLivePendingAPI_CompleteAndRemind` và `live.TestRemind_*` (chạy trong ci-task).
- `tests/e2e/fb_inbound_latency_test.go` đổi cấu trúc (dùng `startHub`, `postFacebookMessageText`, cùng hành vi), chưa
  chạy lại.

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

1. **Đơn bị bỏ vì chính sách không ghi lần nhắc.** Kế hoạch ghi "bấm lại trong 60 phút → cả 3 skipped đã nhắc lúc…",
   nhưng đơn thứ ba chưa từng được nhắc (lần đầu đã bị bỏ vì quá 24 giờ và đã nhắn riêng). Chọn: đơn đó vẫn trả lý do
   chính sách thật, `remind_count` không tăng; chỉ đơn đã thật sự gửi tin mới "Đã nhắc lúc HH:MM". Vì: "đã nhắc" là
   sai sự thật, nhân viên sẽ tưởng khách đã nhận tin. Khác: ghi cả lần bỏ. Đảo ngược: chuyển `MarkReminded` lên trước
   bước chọn đường.
2. **Ghi nhận chính của đơn** là lần chốt đầu tiên của đơn trong phiên (một đơn có thể gồm nhiều comment qua thêm vào
   nháp): người chốt, lúc chốt, kết quả xin địa chỉ, lần nhắc đọc/ghi ở dòng đó; nhắn riêng khi nhắc dùng comment đó.
   Khác: thử lần lượt mọi comment của đơn. Đảo ngược: đổi câu `DISTINCT ON` và `remindOne`.
3. **`confirmedCount`** = đơn của phiên có `confirmed_at` và chưa hủy; **`expiredCount`** = đơn hủy lý do
   `draft_expired`. Đơn nháp chuyển sang chờ duyệt giảm giá (new, chưa xác nhận) không vào số nào.
4. **`ask` trong danh sách**: trạng thái lúc chốt (enum hợp đồng không có `failed`); khi tin xin địa chỉ thật sự
   `failed`/`expired` (đọc `messages.send_status`) thì `reason` là câu lý do gửi lỗi. Khác: thêm giá trị enum (đổi hợp đồng).
5. **Mẫu tin nhắc không có → 404 cho cả lần**, kiểm trước mọi đơn (không phải mỗi đơn một skipped).
6. **`customerRepliedAfterAsk`**: có tin vào (`direction = 'in'`) trong bất kỳ hộp thư nào của cùng khách trên Page
   sau lúc chốt.
7. **Một lần nhắc là một transaction** (WriteTx + Idempotency-Key): đơn bị bỏ không ghi gì nên không chặn đơn khác;
   lỗi hạ tầng lùi cả lần để RetryTx chạy lại; mỗi đơn gửi được có một tin và một lệnh riêng.

## Khác kế hoạch

- Tệp khung: thêm `livepending.go` ở gói gốc, câu SQL `queries/conversation/pending.sql`, tiện ích thử
  `internal/testkit/livetest/check.go`; `send/ask.go` tách `planAsk` (dùng chung xin địa chỉ và nhắc).
- LV04 đủ ca chạy được nằm ở `httpapi.TestLivePendingAPI_CompleteAndRemind`; `tests/e2e/live_flow_test.go` viết sẵn.

## Việc phát hiện thêm

- Bối cảnh lên đơn cạnh khung chat (Task 159) còn chọn nguồn đơn theo giờ đăng bài thay giờ bắt đầu phiên live (ghi
  chú điều phối); task này không chạm bối cảnh đó nên không sửa.
- Danh sách xét `canRemind` từng dòng (vài câu đọc mỗi đơn, tối đa 500 đơn); nếu màn Live (Task 177) hỏi định kỳ với
  phiên rất đông, có thể gom câu đọc chính sách.
- Nhắc đọc trạng thái đơn không khóa đơn: đơn vừa được xác nhận đúng lúc bấm nhắc có thể nhận một tin nhắc thừa (không
  sai dữ liệu). Lỗi điền mẫu của riêng một đơn làm hỏng cả lượt (hiện không có đường nào gây lỗi riêng một đơn).

## Reviewer

Kết luận: ĐẠT (lượt 2, trên phần đã stage sau khi gộp m4 e8c7d8fe).

- Lượt 1: ĐẠT, không phát hiện phải sửa. Đã kiểm: tách `planAsk` giữ nguyên hành vi `AskAddress` (1 chỗ gọi
  `live/claim.go`); thứ tự khóa hội thoại (một câu ORDER BY id) → đọc lại lần nhắc → UPDATE có điều kiện → tin, lệnh;
  `MarkReminded` trước `Deliver`, lỗi thì lùi cả lần; kết quả dựng lại mỗi lần RetryTx; không log dữ liệu cá nhân,
  SQL qua sqlc, không sửa mã sinh. Ghi chú không bắt buộc: tài liệu thiếu `LastRemindedAt`, còn tên cũ
  `RemindAddress` (đã sửa); lỗi điền mẫu của một đơn làm hỏng cả lượt, trạng thái đơn đọc không khóa (ghi ở Việc
  phát hiện thêm).
- Lượt 2 (sau khi sửa chú thích `live.Pending` và tệp task): ĐẠT, không phát hiện phải sửa.
