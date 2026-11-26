# Task 159 — Danh sách và chi tiết hội thoại, thẻ khách, bối cảnh lên đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 159 = M4-19). Loại việc (quy
trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký ký hiệu nào đã có (0 chỗ
  tham chiếu phải sửa).
  - `conversation` (gói gốc, `read.go`, `ordercontext.go`): `List`, `ListFilter`, `ListRow`, `AssigneeMe`,
    `AssigneeNone`, `CountConversations`, `Counts`, `PageCount`, `ListMessages`, `ChatMessage`, `MessageAttachment`,
    `PrivateReply`, `PrivateReplyNone`, `MessagePage`, `CustomerCard`, `Card`, `CardOrder`, `OrderContext`,
    `OrderContextResult`, `StatusOpen|Pending|Done|Spam`, `MaxListLimit`, `ListSinceSlack`, `ErrInvalidStatus`;
    phương thức `Writer.MarkRead`, `Writer.SetStatus`. `detail.go`: `optTime` riêng bỏ, dùng `assign.OptTime`.
  - Gói mới `conversation/internal/query`: `List`, `Filter`, `Assignee`, `Row`, `ListParams`, `BuildListSQL`
    (`list_dynsql.go`), `CountConversations`, `Counts`, `PageCount` (`counts.go`), `Messages`, `Message`, `Attachment`,
    `PrivateReply`, `MessagePage`, `ConversationPage` (`messages.go`), `CustomerCard`, `Card`, `CardOrder`,
    `ConversationRef` (`card.go`).
  - `conversation/internal/assign` (`status.go`): `Writer.SetStatus`, `Writer.MarkRead`, `ErrInvalidStatus`, hằng trạng
    thái, `OptTime`, `lockPermitted` (riêng).
  - `conversation/internal/inbound`: hook `onNewConversation` thành phương thức của handler, gọi
    `assign.RoundRobin.OnNewConversation`; hai chỗ gọi (`handler.go` hộp thư, `comments.go` comment của Task 154).
  - `httpapi`: phương thức `ListConversations`, `CountConversations`, `GetConversation`,
    `GetConversationCustomerCard`, `GetConversationOrderContext`, `ListMessages`, `MarkConversationRead`,
    `SetConversationStatus` (thay 501); hàm riêng `chatHistoryOut`, `historyAttachmentOut`, `chatOrdersOut`; dùng lại
    `conversationDetail`, `assignChange`, `conversationChange`, `customerConversationOut`, `pageStart`, `optUUIDOut`,
    `nonEmpty`. `errors_conversation.go`: dòng `ErrInvalidStatus` → 422 `invalid_input` (mã có sẵn, không thêm
    `x-error-codes`); `errors_snapshot_test.go` thêm dòng.
- Gói phụ thuộc ngược (go list): `conversation` ← `cmd/crm`, `internal/httpapi`; `httpapi` ← `cmd/crm`, `tests/arch`.
  `customer`, `order`, `channel` chỉ được gọi, không đổi.
- Operation OpenAPI bị chạm, oasdiff: 8 operation đã khai ở Task 143 (`listConversations`, `countConversations`,
  `getConversation`, `getConversationCustomerCard`, `getConversationOrderContext`, `listMessages`,
  `markConversationRead`, `setConversationStatus`); task này hiện thực, **không sửa** `openapi.yaml` → oasdiff không
  đổi, `console/src/api` không đổi. Xóa 8 dòng tương ứng khỏi `tests/arch/testdata/m4_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn M4 ở Task 174–176).
- Câu sqlc, bảng, pha migration: **không migration** (index `conversations_page_status`,
  `conversations_assignee_status`, `conversations_recent`, `conversation_tag_links_tag`, `messages_conversation` đã có
  ở 0402). Câu sqlc mới: `queries/conversation/card.sql` (đơn gần đây, đơn nháp của nhóm khách đọc `ord.orders_v`; tên
  nguồn của khách đọc `customer.customers_v`; bối cảnh lên đơn), `queries/conversation/read.sql` (tin theo trang
  trước/sau kèm người gửi, tin riêng, số lần sửa; đánh dấu đã đọc; đổi trạng thái; đơn nháp của nhiều hội thoại; đếm).
  SQL dựng lúc chạy: `conversation/internal/query/list_dynsql.go` (bộ lọc danh sách). Đọc view `customer.customers_v`,
  `identity.users_v`, `ord.orders_v` (đã công bố trong `docs/views.md`).
- Test sẽ chạy: test mới `internal/modules/conversation/internal/query/{list_dynsql_test.go,card_test.go,messages_test.go}`
  (có `env_test.go`), `internal/modules/conversation/explain_test.go` (`TestListQueries_Plan`),
  `internal/modules/conversation/internal/inbound/roundrobin_test.go`, `internal/httpapi/handlers_conversation_read_test.go`,
  `errors_snapshot_test.go`; `tests/arch` (m4contract, dynsql, sqlusage, sellerfilter). Cả gói qua `ci-task`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`tenancy.WithTenant`,
  `identity.Can`, `identity.RecordAudit`, `realtime.Bus.Publish`); `conversation/internal/access` (`Require`,
  `VisiblePages`, `PageScope`, `Signal`, `ErrNotFound`); `conversation.Get` + httpapi `conversationDetail` (Task 160:
  chi tiết, `sendPolicy`); `calls.CustomerConversation`, câu `ConversationTagsOf`, httpapi `customerConversationOut`
  (Task 169: dòng hội thoại); `customer.ListCursor`/`ParseListCursor` (con trỏ trang); `customer.Search`,
  `customer.Get`, `customer.IsDoNotContact`, `customer.MergeCandidates`; `order.ActiveSource`; `conversation`
  cài đặt Page và `LiveSlot` (Task 160); `assign.RoundRobin` (Task 160).
- Dùng lại / viết mới vì:
  - Chi tiết hội thoại: dùng nguyên `conversation.Get` + `conversationDetail` (không viết `query/detail.go` như kế hoạch
    ghi; Task 160 đã viết, ghi chú điều phối M4).
  - Dòng danh sách: cùng kiểu `CustomerConversation` và `customerConversationOut` của Task 169; câu danh sách là SQL
    dựng lúc chạy (bộ lọc tùy chọn chọn index khác nhau) nên nằm ở `list_dynsql.go`; nhãn đọc bằng `ConversationTagsOf`
    có sẵn, đơn nháp bằng câu sqlc (giữ luật người bán của `*_dynsql.go`: câu đọc `ord.orders_v` nằm ở sqlc).
  - Tìm khách: `customer.Search` của M2 (không viết cách tìm thứ hai); nhóm theo `customers_v.root_id`.
  - Thẻ khách: chỉ số lấy nguyên `customer.Get` (`Stats.ReturnRateBP`), không công thức thứ hai; đơn đọc
    `ord.orders_v` theo `Profile.MemberIDs` (= `customer.GroupMembers`).
  - Nguồn đơn: `order.ActiveSource`; khung giờ live dùng `LiveSlot` và cách đọc cài đặt của Task 160.
  - Đổi trạng thái, đánh dấu đã đọc: viết mới trong `assign` (cùng chỗ đang khóa dòng hội thoại, so version, audit,
    báo hiệu như `Writer.Assign`) — dùng lại `lockConversation`, `access.Signal`.

## Soát thiết kế

5.1 Dữ liệu đọc/ghi thuộc `conversation` (trạng thái, số chưa đọc). 5.2 Không ghi bảng module khác. 5.3 Đọc module
khác qua view (`customer.customers_v`, `identity.users_v`, `ord.orders_v`) và public API (`customer.Search`, `Get`,
`IsDoNotContact`, `MergeCandidates`, `order.ActiveSource`, `channel.Account`). 5.4 Hai đường ghi chạy trong tx của bên
gọi (`refusableWrite`). 5.5 Báo hiệu `conversation.changed` qua `realtime.Bus` trong tx (outbox). 5.6 không áp dụng:
không gọi ra ngoài (chính sách gửi hỏi adapter, không gọi mạng). 5.7 Mọi câu trong `tenancy.WithTenant` (RLS). 5.8
Module không nhập `net/http`. 5.9 Hai route ghi đã có `IdempotencyKey`, `x-permission` (Task 143); quyền theo Page kiểm
lại ở module (PQ04: không có quyền → 404). 5.10 không áp dụng: không màn hình. 5.11 Không thêm thư viện. 5.12 Theo mẫu
`Writer` của Task 160.

6.1 Đổi trạng thái: khóa dòng hội thoại `FOR UPDATE`, so `version` dưới khóa, `UPDATE` tăng `version`; version cũ →
409. Đánh dấu đã đọc: khóa dòng hội thoại (đường vào tăng `unread_count` dưới cùng khóa), đặt `unread_count` = số tin
khách sau tin `upToMessageId` (tin đến sau lúc màn hình đọc không bị xóa số), không tăng `version` (không làm hỏng lần
giao/đổi trạng thái đang mở của người khác). 6.2 Route ghi qua middleware chống trùng; đặt cùng trạng thái, đánh dấu
lại → không lỗi. 6.3 Chỉ khóa một dòng `conversations` (nhóm 6), đọc `messages` không khóa → không vòng chờ. Nối xoay
vòng vào đường vào: thứ tự khóa đã ghi ở ADR-46 (Task 160). 6.4 Hàm trong `db.RetryTx` gán lại mọi biến kết quả. 6.5
READ COMMITTED + khóa dòng; danh sách `updatedSince` lùi 30 s để không sót dòng của transaction commit muộn. 6.6–6.8
không áp dụng: không việc nền mới. 6.9 Chịu `lock_timeout` của `TxAPI`. 6.10 không áp dụng: M4 không tính tiền (đọc
`grand_total` từ `ord.orders_v`). 6.11 Bộ lọc và phân trang kiểm bằng bảng ca cố định (30 hội thoại; 120 hội thoại 3
trang). 6.12 Audit đổi trạng thái chỉ ghi tên trường; chuỗi tìm `q` (SĐT, tên) không vào log, lỗi. Thẻ khách chỉ có
SĐT khi người gọi có `customer.read`. 6.13 Danh sách lọc theo `VisiblePages`; chi tiết, tin, thẻ khách, bối cảnh, đánh
dấu đã đọc, đổi trạng thái của Page không có quyền → 404.

## Quyết định thay chủ shop

1. **Đánh dấu đã đọc giữ số tin đến sau mốc.** `unread_count` = số tin khách sau `upToMessageId` (không đặt cứng 0 như
   kế hoạch ghi): tin vừa tới sau lúc màn hình đọc vẫn là "chưa đọc". Đọc tới tin mới nhất → 0 (đúng ca kế hoạch). Số
   không bao giờ tăng (`LEAST` với số đang có): mốc cũ của màn hình chậm sau khi người khác đã đọc hết giữ nguyên 0
   (reviewer vòng 1).
   Không tăng `version` (đánh dấu đã đọc không làm lần giao, đổi trạng thái đang mở của người khác bị 409). Tin không
   thuộc hội thoại → 404. Phương án khác: luôn đặt 0 (mất tin đến giữa chừng).
2. **Phân trang tin:** `olderCursor` chỉ có khi còn tin cũ hơn; `newerCursor` luôn là mốc tin mới nhất của trang (trang
   `after` rỗng trả lại đúng mốc đã gửi) để màn hình hỏi tin mới. Gửi cả `before` và `after` → 422 `invalid_input`.
3. **Số chưa đọc theo Page** (`counts.byPage.unread`) = số hội thoại mở/chờ còn tin chưa đọc (không cộng dồn số tin; spam,
   done không tính); `open` = số hội thoại đang mở; `mine`, `unassigned` chỉ đếm hội thoại đang mở trên Page được xem.
4. **Thẻ khách:** `recentOrders` = 5 đơn mới nhất trừ đơn nháp của cả nhóm (mọi người bán của shop: khách không có chiều
   người bán, ADR-29 điểm 4); `openDrafts` = đơn nháp của cả nhóm (tối đa 20). `mergeSuggestionCustomerId` (gốc đầu tiên
   của `customer.MergeCandidates`) chỉ trả cho người có `customer.read` như SĐT.
5. **Giờ bắt đầu phiên live** lấy từ `conversation.posts.created_time` của bài `kind = live` (bảng `live_sessions` của
   Task 170 chưa có). Không khung giờ nào chứa giờ bắt đầu, hay bài live chưa biết giờ → nguồn comment của Page. Task
   170 đổi sang giờ bắt đầu của phiên nếu cần (ghi ở Việc phát hiện thêm).
6. **Xoay vòng chạy cho mọi hội thoại mới** (inbox và comment, sau khi gộp Task 154: hai chỗ gọi cùng một hook).
7. **Số lần sửa của tin** (`editedCount`) = số dòng `message_revisions` mà nội dung sau nó (dòng kế, hay nội dung hiện tại)
   khác nội dung dòng đó; dòng ẩn, hiện, xóa (giữ nguyên nội dung, Task 154) không tính.
8. **`privateReply.state`** = `send_status` của tin riêng đang sống (queued, sent, checking, in_doubt — đúng index một
   phần CH05); tin riêng đã lỗi, hết hạn, bỏ → `none` (nhắn lại được). Tin không phải comment không có `privateReply`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH01, CH06, CH07: 30 hội thoại, mỗi bộ lọc ra đúng số dòng (open 12, pending 8, spam 4, nhãn 7, nhãn đã xóa 0, của tôi 6, chưa ai 18, theo mã 6, comment 10, inbox 20, chưa đọc 15, open + của tôi 3, comment + chưa đọc 5, Page B 0); mới nhất trước; dòng có tên khách, người phụ trách, nhãn; assignee lạ → ErrInvalidInput | `TestList_Filters` | `go test -race -count=1 ./internal/modules/conversation/internal/query/`; ci-task | `ok …/conversation/internal/query 2.592s`; ci-task `ok … 7.967s` |
| 120 hội thoại, limit 50 → 3 trang 50/50/20, không trùng, không sót, đúng thứ tự (có dòng cùng giờ); con trỏ qua chuỗi; updatedSince: dòng T − 20 s (commit muộn) có, T − 40 s không | `TestList_PagingAndUpdatedSince` | như trên | PASS |
| q "5678", "lan nguyen", SĐT đủ → hội thoại của Lan và của hồ sơ B đã gộp vào Lan; "56" → ErrQueryTooShort; không khớp → rỗng | `TestList_Search` | như trên | PASS |
| explain 200.000 hội thoại: Page + trạng thái, người phụ trách (mọi Page; + trạng thái, hai Page), nhãn, không lọc → không Seq Scan, đủ 51 dòng | `TestListQueries_Plan` (+ `TestCoreQueries_Plan` của Task 151 vẫn xanh) | `go test -count=1 -run '^(TestListQueries_Plan\|TestCoreQueries_Plan)$' ./internal/modules/conversation/`; ci-task | `ok …/conversation 4.881s`; ci-task `ok … 29.090s` |
| CH09: chi tiết có sendPolicy: tin khách cuối 25 giờ → allowed false, window_closed; 2 giờ → allowed, standard, until = +24 giờ; Page chỉ xem → read_only | `TestConversationReadAPI_SendPolicy` | `go test -count=1 -run '^TestConversationReadAPI_' ./internal/httpapi/`; ci-task | `ok …/internal/httpapi 2.054s`; ci-task `ok … 72.515s` |
| PQ04 cho M4: chỉ có quyền Page A → danh sách, số đếm chỉ Page A; chi tiết, tin, thẻ khách, bối cảnh, đã đọc, đổi trạng thái của Page B (và mã lạ) → 404 / ErrNotFound; chỉ đọc mà đổi trạng thái → 403 | `TestPageScope_PQ04` (module), `TestConversationReadAPI_PQ04` (API) | như hai dòng trên | PASS |
| DH03 phần hiển thị (C3): 8 giao, 2 hoàn → returnRateBP 2000 = `customer.Get` (cả Stats bằng nhau); hồ sơ gộp → recentOrders 5 đơn mới nhất của cả hai, mới nhất trước; openDrafts; không customer.read → không SĐT, gợi ý gộp; có → SĐT | `TestCustomerCard`, `TestConversationReadAPI_CardAndOrderContext` | như trên | PASS |
| 01 mục 2: comment bài live 12:15 giờ VN, khung 11:00–14:00 → "Live ca trưa"; live 15:00 → nguồn comment; bài thường → comment; hộp thư → nguồn hộp thư; Page chưa cài đặt → nil; nguồn đã tắt → nil; người bán của Page | `TestOrderContext`, `TestConversationReadAPI_CardAndOrderContext` | như trên | PASS |
| status với expectedVersion cũ → 409 (ErrVersionConflict); đúng → done, version + 1, audit chỉ trạng thái; cùng trạng thái → không đổi; thiếu reply → ErrNotFound; trạng thái lạ → ErrInvalidStatus; API: 409 version_conflict, 200 chi tiết, gửi lại cùng khóa → Idempotent-Replayed | `TestSetStatus`, `TestConversationReadAPI_MessagesReadStatus` | như trên | PASS |
| Đánh dấu đã đọc: đọc tới tin 2/3 → còn 1, tới tin 3 → 0, đọc lùi về tin 2 → vẫn 0, version không đổi; tin hội thoại khác → 404; API 204 | `TestMarkRead`, `TestConversationReadAPI_MessagesReadStatus`, `TestConversationReadAPI_PQ04` | như trên | PASS |
| Tin theo trang (before/after, olderCursor/newerCursor); ghi chú Task 154: tin thường không privateReply, ẩn/xóa false, editedCount 0, đính kèm rỗng; comment có tin riêng sent → {sent, mã}, tin riêng lỗi → none; comment trả lời → parentMessageId; sửa 2 lần + 1 lần ẩn → editedCount 2 | `TestMessages_Paging`, `TestMessages_Flags`, `TestConversationReadAPI_MessagesReadStatus` | như trên | PASS |
| Ghi chú điều phối: xoay vòng nối vào đường vào (hội thoại mới chưa có người phụ trách); Page chưa bật → không giao; [u1,u2] → u1, u2, u1; tin thứ hai không đổi; 3 sự kiện round_robin. Bỏ lời gọi → test đỏ (đã thử) | `TestInbound_RoundRobinOnNewConversation` | `go test -race -count=1 -run '^TestInbound_RoundRobinOnNewConversation$' ./internal/modules/conversation/internal/inbound/` | `ok … 2.276s`; khi bỏ lời gọi: `FAIL … sự kiện conversation.assigned = 0, muốn 3` |
| Bảng lỗi, hợp đồng M4 (8 operation hết 501, xóa khỏi `m4_pending.txt`) | `TestDomainErrorSnapshot*`, `TestM4Contract_*` | ci-task | PASS |
| Cổng tĩnh + test (sau khi gộp m4: testkit đổi → toàn bộ trừ e2e) | — | ci-task (lần 1 trước review; lần 2 sau khi sửa phát hiện của reviewer) | Cả hai lần mọi cổng tĩnh xanh (build, vet, lint, gen-check, sqlc-vet, catalog, jscpd 0 clone, gitleaks, oasdiff, test-guard, kiến trúc). Lần 1 (913 giây): mọi gói `ok` trừ `internal/platform/cache` (Valkey `context deadline exceeded`). Lần 2 (1797 giây, máy rất tải): mọi gói của task `ok` (`conversation` 43.7s, `…/query` 7.9s, `…/inbound` 13.3s, `…/assign` 3.8s, `httpapi` 123.7s, `cmd/crm` 8.5s); đỏ ở ba gói task không chạm, đều là ngưỡng giờ/Valkey: `platform/cache` (Valkey degraded), `httpapi/web` `TestWebShippingQuote` (bộ đệm Valkey), `fulfillment` `TestQuoteNow_TimeoutClamped` (13,4 s, muốn ~10 s). Chạy lại riêng: `go test -race -count=1 ./internal/platform/cache/` → `ok 24.203s`; `-run '^TestWebShippingQuote$' ./internal/httpapi/web/` → `ok 9.806s`; `-run '^TestQuoteNow_TimeoutClamped$' ./internal/modules/fulfillment/` → `ok 12.156s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không có `query/detail.go`: chi tiết hội thoại dùng `conversation.Get` + `conversationDetail` của Task 160 (ghi chú
  điều phối M4). Bối cảnh lên đơn ở `conversation/ordercontext.go` (gói gốc) vì dùng lại cách đọc cài đặt Page,
  `LiveSlot` của Task 160 (gói gốc); thẻ khách, danh sách, tin, số đếm ở `internal/query`.
- Tên, người phụ trách, đoạn bài, cờ đơn nháp của dòng danh sách đọc bằng câu sqlc thứ hai theo mã (`ConversationRowExtras`)
  thay vì JOIN trong câu dựng lúc chạy: để câu có bộ lọc chỉ chạm `conversations` (planner không hash-join cả bảng
  khách), và câu đọc `ord.orders_v` nằm ở sqlc. Nhãn đã xóa kiểm trước bằng `TagAlive` (lọc theo nhãn chỉ chạm
  `conversation_tag_links`).
- Tên kiểu tin của lịch sử là `conversation.ChatMessage` (`conversation.Message` đã là tin vừa gửi của Task 156).
- `optTime` của `conversation/detail.go` chuyển thành `assign.OptTime` (dùng chung cho detail, query; jscpd).
- `TestCoreQueries_Plan` (Task 151) dùng chung hàm mở dữ liệu mẫu `planStaff` với test EXPLAIN mới; dữ liệu mẫu thêm
  20 nhãn và ≈ 15.000 dòng gắn nhãn.
- Test module viết sau phần mã đọc (đã chứng minh test bắt lỗi bằng cách tắt lại chỗ nối xoay vòng → đỏ; test HTTP viết
  trước handler, đỏ 501).

## Việc phát hiện thêm

- `ord.orders` không có index theo `conversation_id`: cờ "có đơn nháp" (Task 160, 169, 159) lọc thêm `status = 'draft'`
  nên dùng được index một phần `orders_draft_expires`; nếu cần đọc mọi đơn của một hội thoại thì module order (chủ bảng)
  thêm index.
- Task 170: thay giờ bắt đầu phiên live (đang lấy `posts.created_time`) bằng giờ của `live_sessions` khi bảng có.
- `avatarUrl` của khách trong dòng danh sách chưa trả (cột `avatar_file_id`; đường dẫn ảnh có hạn ở Task 155).
- `quickReplies` của tin chưa trả trong lịch sử (tin chào có nút, Task 167).

## Reviewer

Kết luận: ĐẠT (vòng 2).

- Vòng 1: CẦN SỬA — đánh dấu đã đọc gán thẳng `unread_count` = số tin khách sau mốc, nên mốc cũ của màn hình chậm (sau
  khi người khác đã đọc hết) làm số chưa đọc tăng lại. Đã sửa: `SetUnreadAfter` dùng `LEAST(c.unread_count, …)`, thêm
  ca đọc lùi vào `TestMarkRead`, cập nhật Quyết định 1; chạy lại ci-task.
- Vòng 2: ĐẠT, không còn phát hiện phải sửa; đối chiếu đủ mục Ảnh hưởng, bảng bằng chứng, test không bị nới. Ghi chú
  không bắt buộc (không sửa): dòng danh sách hiện tên của hồ sơ gắn hội thoại (hồ sơ con khi đã gộp) còn thẻ khách
  hiện tên gốc — chuyện hiển thị; test API "version cũ" gửi version lớn hơn hiện tại (vẫn 409; ca version cũ hơn có ở
  `TestSetStatus`).
