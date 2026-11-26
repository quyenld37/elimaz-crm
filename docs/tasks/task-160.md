# Task 160 — Người phụ trách, xoay vòng, nhãn, cài đặt Page

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 160 = M4-20). Loại việc (quy
trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký ký hiệu nào đã có (0 chỗ tham
  chiếu phải sửa).
  - `conversation` (gói gốc): `Writer` (đường ghi: `Claim`, `Assign`, `CreateTag`, `UpdateTag`, `DeleteTag`, `AddTag`,
    `RemoveTag`, `UpdatePageSettings`), hàm đọc `Get`, `ListAssignments`, `ListTags`, `TaggedConversationIDs`,
    `GetPageSettings`, `AssignableUsers`, `ValidLiveSlots`, `NewRoundRobin` (hook xoay vòng cho đường vào), kiểu
    `Conversation` (nhúng `CustomerConversation` của Task 169), `Assignment`, `Person`, `Tag` (= `calls.Tag` của Task
    169), `TagInput`, `TagPatch`, `PageSettings`, `PageSettingsInput`, `LiveSlot`, hằng `AssignReason*`, `EntityTag`,
    `EntityPageSettings`; sentinel mới `ErrVersionConflict`, `ErrAlreadyAssigned`, `ErrTagNotFound`, `ErrTagNameTaken`,
    `ErrInvalidInput` (`ErrNotFound`, `ErrAssigneeNotAllowed` đã có từ Task 169, dùng lại).
  - `conversation/internal/assign`: `Writer.Claim`, `Writer.Assign`, `History`, `Eligible`, `RoundRobin`,
    `NextInRotation`, `Signal`, `PtrUUID`, `IDOrNil` (khung `Parts` giữ nguyên).
  - `conversation/internal/access` (sau khi gộp m4): thêm `ErrAssigneeNotAllowed` (một sentinel chung; 
    `reminders.ErrAssigneeNotAllowed` của Task 169 trỏ về nó, cùng mã 422) và `Signal` (thân hàm chuyển từ
    `reminders.Signal`, hàm đó giữ chữ ký, gọi lại `access.Signal`; 2 chỗ gọi trong reminders không đổi).
  - `testkit`: `Staff.Conversation`, `Staff.TrafficSource`; gói test mới `conversation/convtest` (`Conversation`,
    `Staff`, `Load`).
  - `identity`: thêm `StaffWithPermissionAt(ctx, tx, p, target)` (nhân viên `active` có quyền p ở phạm vi all hoặc
    đúng target; vai trò lưu trữ không tính, như `ListUserGrants`).
  - `httpapi`: phương thức `ClaimConversation`, `AssignConversation`, `ListConversationAssignments`,
    `ListAssignableUsers`, `ListConversationTags`, `CreateConversationTag`, `UpdateConversationTag`,
    `DeleteConversationTag`, `AddConversationTag`, `RemoveConversationTag`, `GetPageSettings`, `UpdatePageSettings`
    (thay 501 của `gen.UnimplementedHandler`); `conversationDetail` dùng `Deps.Channels` (đã có từ Task 149, cmd đã nối)
    để hỏi `SendPolicy`; `isConversationRefusal` (Task 169) thêm lỗi của channel, order và `errForbiddenTarget`.
- Gói phụ thuộc ngược (go list): `conversation` ← `cmd/crm`, `internal/httpapi` (từ Task 169); `identity` ← 11 gói
  (httpapi, modules, testkit…; chỉ thêm hàm nên không chỗ nào phải sửa); `httpapi` ← `cmd/crm`, `tests/arch`.
- Operation OpenAPI bị chạm, oasdiff: 12 operation đã khai ở Task 143 (`claimConversation`, `assignConversation`,
  `listConversationAssignments`, `listAssignableUsers`, `listConversationTags`, `createConversationTag`,
  `updateConversationTag`, `deleteConversationTag`, `addConversationTag`, `removeConversationTag`, `getPageSettings`,
  `updatePageSettings`); task này hiện thực, **không sửa** `openapi.yaml` → oasdiff không đổi, `console/src/api` không
  đổi. Xóa 12 dòng tương ứng khỏi `tests/arch/testdata/m4_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn M4 ở Task 174–178).
- Câu sqlc, bảng, pha migration: **không migration** (bảng `conversation.tags`, `page_settings`,
  `conversation_tag_links`, `conversation_assignments` và quyền GRANT đã có ở 0402). Câu sqlc mới
  `queries/conversation/assign.sql`, `tags.sql`, `pagesettings.sql` (khối `conversation` của `sqlc.yaml` có sẵn), một
  câu mới `ListStaffWithPermissionAt` trong `queries/identity/staff.sql`. Đọc view `identity.users_v`,
  `customer.customers_v`, `ord.orders_v` (công bố trong `docs/views.md`, không cần sửa). `docs/adr/0046-thu-tu-khoa.md`:
  thêm dòng đường xoay vòng (khóa `page_settings` sau dòng hội thoại vừa chèn).
- Test sẽ chạy: test mới `internal/modules/conversation/internal/assign/{assign_test.go,roundrobin_test.go}`,
  `internal/modules/conversation/{tags_test.go,pagesettings_test.go}`, `internal/modules/identity` (ca mới
  `TestStaffWithPermissionAt`), `internal/httpapi/handlers_assign_test.go`, `handlers_tags_test.go`,
  `handlers_page_settings_test.go`, `errors_snapshot_test.go`; `tests/arch` (m4contract, lockorder). Cả gói qua
  `ci-task` (identity, httpapi là vùng rủi ro cao/gói lớn).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`tenancy.WithTenant`,
  `identity.RecordAudit`, `identity.GrantedAt`, `identity.Can`, `events.Registry.Emit`, `realtime.Bus.Publish`);
  `conversation/internal/access` (`Require`, `PageScope`, `VisiblePages`, `ErrNotFound`); `channel.Account`,
  `channel.MessagingAdapter.SendPolicy`; `customer.IsDoNotContact`; `order.ActiveSource`; httpapi `refusableWrite`,
  `inventoryRead`, `channelAccountOut`, `matchError`; mẫu `channel.Writer` (khóa dòng, so version, audit, báo hiệu),
  `customer` nhãn khách (`handlers_customer_tags.go`: tên trùng → `already_exists`).
- Dùng lại / viết mới vì:
  - Quyền theo Page: dùng `access.Require` (không có quyền → 404 như không có); nhãn chung (Q7, chủ shop 26/11) dùng
    `identity.GrantedAt(u, conversation.settings, Scope{all})` — đúng nghĩa "grant phạm vi all".
  - Danh sách nhân viên được giao viết mới `identity.StaffWithPermissionAt` (chưa có hàm nào trả "ai có quyền p trên
    đích"; `StaffHas` chỉ kiểm một người ở bất kỳ phạm vi; `ListStaff` trả vai trò, không trả quyền). Đặt ở `identity`
    vì cách tính quyền từ vai trò (bỏ vai trò lưu trữ) thuộc `identity`, không chép sang `conversation`.
  - Nhãn hội thoại viết mới trong `conversation` (không gộp với nhãn khách `customer.tags`: khác chủ bảng, khác lý do
    thay đổi — luật "logic nghiệp vụ ở lại module").
  - Chi tiết hội thoại trả về sau khi nhận/giao (`ConversationDetail` theo hợp đồng): viết `conversation.Get` và
    `conversationDetailOut` (httpapi) một lần để Task 159 (`getConversation`) dùng lại, không viết lần hai.
  - Lỗi: tệp mới `errors_conversation.go` theo luật Task 441.

## Soát thiết kế

5.1 Dữ liệu thuộc `conversation` (phân công, nhãn, cài đặt Page); quyền tính ở `identity`. 5.2 Không ghi bảng module
khác. 5.3 Đọc module khác chỉ qua view công bố (`identity.users_v`, `customer.customers_v`, `ord.orders_v`) và public
API (`channel.Account`, `customer.IsDoNotContact`, `order.ActiveSource`, `identity.StaffWithPermissionAt`). 5.4 Mọi
hàm ghi chạy trong tx của bên gọi (`refusableWrite`/`WriteTx`; hook xoay vòng chạy trong tx của sự kiện vào). 5.5 Sự
kiện `conversation.assigned` qua `events.Registry.Emit` trong cùng tx (outbox), `Seq` = `conversations.version` mới
(tăng dưới khóa dòng). 5.6 không áp dụng: không gọi ra ngoài. 5.7 Mọi câu trong `tenancy.WithTenant` (RLS). 5.8 Module
không nhập `net/http`; handler mỏng. 5.9 Route ghi có `IdempotencyKey`, `x-permission` (Task 143); quyền theo Page
kiểm lại ở module/handler (PQ04). 5.10 không áp dụng: không màn hình. 5.11 Không thêm thư viện. 5.12 Theo mẫu
`channel.Writer` / `catalog.Writer`.

6.1 Nhận/giao: khóa dòng hội thoại `FOR UPDATE`, so người phụ trách/`version` dưới khóa, một câu `UPDATE` tăng
`version` → hai người cùng nhận: người sau thấy đã có người → 409 `already_assigned`. Cài đặt Page: khóa dòng
`page_settings` rồi so `version`; chưa có dòng (version 0) → `INSERT`, hai lần chèn đồng thời → vi phạm khóa chính →
`version_conflict`. Nhãn: `UPDATE … WHERE` dưới khóa dòng; tên trùng do index unique `tags_name_live` → 409. 6.2 Route
ghi qua middleware chống trùng; nhận lại khi đã là mình, gắn lại nhãn, gỡ nhãn không có → không lỗi, không ghi gì
thêm. 6.3 Thứ tự khóa (ADR-46 nhóm 6): gắn nhãn khóa `tags` (FOR SHARE) → `conversations` → `conversation_tag_links`;
nhận/giao chỉ khóa `conversations` → chèn `conversation_assignments`; xoay vòng chạy trong tx của tin vào sau khi
`conversations` mới đã chèn (dòng chưa ai thấy) rồi khóa `page_settings` — ngược thứ tự bảng nhưng không có đường nào
giữ `page_settings` rồi chờ `conversations` (PUT cài đặt chỉ khóa `page_settings`), nên không vòng chờ; ghi ngoại lệ
này vào ADR-46. 6.4 `db.RetryTx` chạy lại được: mọi biến kết quả gán lại trong thân hàm. 6.5 READ COMMITTED + khóa
dòng. 6.6–6.8 không áp dụng: không việc nền, lịch mới (sự kiện qua outbox có sẵn). 6.9 Khóa chịu `lock_timeout` của
`TxAPI`/`TxIngest`. 6.10 không áp dụng: không tiền. 6.11 Quy luật xoay vòng (chọn lần lượt, bỏ người bị khóa) kiểm
bằng bảng ca; nhận song song kiểm bằng hai goroutine. 6.12 Audit chỉ ghi mã người, tên trường; không tên khách, nội
dung tin. 6.13 Tenant khác/Page không có quyền → 404; thiếu hẳn quyền → 403 (guard).

## Quyết định thay chủ shop

1. **Nhận/giao trả `ConversationDetail` đầy đủ theo hợp đồng.** Task 159 (chi tiết hội thoại) chưa làm; tôi viết
   `conversation.Get` + `conversationDetail` (httpapi) một lần (Page, `sendPolicy` mục đích `message` từ adapter của
   kênh qua `Deps.Channels`, `doNotContact`, nhãn, người phụ trách; phần hội thoại dùng `customerConversationOut` của
   Task 169) để Task 159 dùng lại. Phương án khác: đổi hợp đồng trả `ConversationSummary` (oasdiff báo phá vỡ) — không
   chọn.
2. **`already_assigned` không kèm người đang nhận trong thân lỗi** (thân lỗi luôn `{error, request_id}`; thêm trường
   phải đổi schema `Error`). Màn hình tải lại chi tiết để thấy người đang nhận. Đảo ngược: thêm trường `assigneeId`
   vào `Error` và `detailedError`.
3. **Cài đặt Page không tạo dòng lúc đọc** (kế hoạch: tạo lười ở lần đọc đầu). `GET` khi chưa có dòng trả mặc định với
   `version = 0` (đúng mô tả `expectedVersion` của hợp đồng: "0 khi Page chưa có dòng cài đặt"); `PUT` với
   `expectedVersion = 0` mới chèn dòng. Lý do: đọc chạy trong tx đọc, không ghi; kết quả thấy được như nhau.
4. **`askAddressTemplateId` khác null → 422 `invalid_input`** cho tới khi bảng mẫu trả lời (Task 161, 0405) có trên
   m4: chưa có bảng thì không kiểm được "có trong mẫu". Task 161 hay task sau thay chốt này bằng kiểm thật (ghi ở Việc
   phát hiện thêm).
5. **Nguồn đơn chỉ kiểm các giá trị mới đổi** (`order.ActiveSource`): nguồn đã lưu rồi bị tắt không chặn việc sửa mục
   khác (Task 159 tự trả `null` cho nguồn đã tắt).
6. **Khung giờ live**: `from < to` trong cùng ngày (không vắt qua nửa đêm); chồng nhau khi `a.from < b.to && b.from <
   a.to` (khung chạm mép 11:00–12:00 và 12:00–13:00 không chồng).
7. **`Seq` của `conversation.assigned` = `version` mới của hội thoại** (bất biến của `events`: Seq từ version tăng dưới
   khóa). Kế hoạch ghi "seq 1, 2" cho hội thoại mới; với `version` mặc định 1 thì hai lần đổi cho seq 2, 3. Test kiểm
   seq tăng và bằng `version` sau mỗi lần đổi.
8. **Báo hiệu**: đổi người phụ trách, nhãn của hội thoại, cài đặt Page → `conversation.changed` phạm vi Page (thực thể
   `conversation` / `page_settings`); thêm/sửa/xóa nhãn chung → `conversation.changed` thực thể `conversation_tag`,
   không phạm vi (mọi người có `conversation.read`). Gắn/gỡ nhãn không ghi audit (thao tác thường ngày như gửi tin);
   tạo/sửa/xóa nhãn, cài đặt Page, nhận/giao có audit.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH06: tự nhận → "Của tôi" của An; quản lý giao Bình → của Bình có, của An không; lịch sử 2 dòng self, manager; 2 sự kiện `conversation.assigned` seq = version; 2 audit | `TestAssign_CH06` (assign), `TestAssignAPI_CH06` (httpapi) | `go test -race -count=1 -run '^TestAssign_CH06$' ./internal/modules/conversation/internal/assign/`; ci-task | `ok …/conversation/internal/assign 3.159s`, `ok …/internal/httpapi 61.584s` |
| Sale không có `conversation.assign` → 403; người nhận không có quyền trên Page (kể cả bị khóa) → 422 `assignee_not_allowed`; người giao ngoài Page → 404; version cũ → 409; bỏ giao → dòng `unassign`; người chỉ có `conversation.reply` (không đọc được Page) tự nhận → 422 | `TestAssign_Refusals`, `TestAssignAPI_CH06` | như trên | PASS |
| Hai nhân viên cùng bấm nhận → một 200, một 409 `already_assigned` | `TestClaim_Concurrent`, `TestAssignAPI_ConcurrentClaim` | như trên | PASS |
| Xoay vòng [u1,u2,u3]: 6 hội thoại → u1,u2,u3,u1,u2,u3; khóa u2 → bỏ qua; đã có người phụ trách → không đổi; tắt / chưa có cài đặt → không giao | `TestRoundRobin`, `TestNextInRotation` | `go test -race -count=1 ./internal/modules/conversation/internal/assign/` | PASS |
| CH07 nhãn: "Đã lên đơn" gắn 3 hội thoại → lọc ra 3; gắn lại không lỗi, không báo hiệu lại; tên trùng khác hoa thường → 409; xóa mềm → lọc 0, không còn trong danh sách; gắn nhãn đã xóa → 404 | `TestTags_CH07`, `TestTags_Update`, `TestTagsAPI_Q7` | `go test -race -count=1 -run '^TestTags_' ./internal/modules/conversation/`; ci-task | `ok …/conversation 6.739s` |
| Q7 (chủ shop 26/11): tạo/sửa/xóa nhãn chung cần `conversation.settings` phạm vi all; grant chỉ một Page → 403; không có quyền → 403 | `TestTagsAPI_Q7` | ci-task | PASS |
| Cài đặt Page: khung chồng nhau → 422 `invalid_input`; nguồn đơn lạ/đã tắt → 422 `traffic_source_inactive`; hợp lệ → 200 version 1 rồi 2, audit, báo hiệu theo Page; version cũ → 409; chỉ đọc → 403; Page khác → 404; mặc định version 0 | `TestPageSettings`, `TestPageSettingsAPI` | như trên | PASS |
| Danh sách "Giao cho…": active, có `conversation.read` ở all hay đúng Page; bị khóa, vai trò lưu trữ, Page khác, tenant khác → không | `TestStaffWithPermissionAt`, `TestAssignAPI_CH06` | `go test -race -count=1 -run '^TestStaffWithPermissionAt$' ./internal/modules/identity/` | `ok …/identity 2.544s` |
| Bảng lỗi, hợp đồng M4 (12 operation hết 501, xóa khỏi `m4_pending.txt`) | `TestDomainErrorSnapshot*`, `TestM4Contract_StatusCodes`, `TestM4Contract_PendingList` | ci-task | PASS |
| Cổng tĩnh + test (testkit đổi → toàn bộ trừ e2e) | — | ci-task (sau khi gộp m4) | `CI-TASK XANH`; lint 0 issues, jscpd 0 clone mới, oasdiff, test-guard qua; test 589 giây |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Cài đặt Page không tạo dòng lười lúc đọc (Quyết định 3). Hook xoay vòng là `conversation.NewRoundRobin(…)` (phương
  thức có chữ ký của hook đường vào `func(ctx, pgx.Tx, uuid.UUID) error`), vì cần sổ sự kiện và hàng đợi.
- Không có `GET /conversations?assignee=me&tagId=` (Task 159 làm); "Của tôi" kiểm bằng câu `ListConversationsByAssignee`
  có sẵn của Task 151, lọc theo nhãn bằng `conversation.TaggedConversationIDs` (Task 159 dùng lại).
- Gộp m4 (Task 149, 169): `ErrNotFound`, `ErrAssigneeNotAllowed`, `errors_conversation.go`, `Deps.Channels` đã có; giữ
  của Task 169 và thêm dòng của mình; hai đoạn trùng (jscpd) với Task 169 gộp: `access.Signal` và
  `customerConversationOut`; `Tag` của gói gốc = `calls.Tag`.

## Việc phát hiện thêm

1. **Nối hook xoay vòng vào đường tin vào** (Task 153 chưa gộp vào m4 khi gộp lần này): `inbound/hooks.go`
   `onNewConversation` của Task 153 là khung trả nil; khi gộp 153 cần gọi `conversation.NewRoundRobin(d.Events,
   d.Queue)` (hay `assign.RoundRobin{W: assign.Writer{Events: d.Events, Queue: d.Queue}}.OnNewConversation`) trong
   handler vào (cần `Deps`, nên hook đổi từ hàm sang trường/closure). Test xoay vòng ở task này gọi hook trực tiếp.
2. **`askAddressTemplateId`** (Quyết định 4): khi bảng `conversation.reply_templates` (Task 161) có trên m4, thay chốt
   "khác null → 422" ở `validSettingsShape` bằng kiểm mẫu có thật, chưa xóa.
3. **Task 159** dùng lại `conversation.Get`, `(*handler).conversationDetail`, `TaggedConversationIDs` cho
   `getConversation` và bộ lọc `tagId`, không viết bản thứ hai.

## Reviewer

Kết luận: ĐẠT (lượt 1, trên bản đã gộp m4 có Task 149, 169). Phát hiện phải sửa: không có. Ảnh hưởng khớp hai chiều
(không chỗ đổi ngoài mục, không chỗ gọi sót); bảng bằng chứng: mọi test có thật và kiểm đúng tiêu chí, kể cả Q7 (grant
một Page → 403 cho tạo, sửa, xóa; phạm vi all → 201); không test nào bị tắt hay nới; không vi phạm luật dự án.
Ghi chú không bắt buộc (chưa sửa, không ảnh hưởng tính đúng hiện tại): `StaffWithPermissionAt` không áp `Effective` —
chỉ đúng cho quyền theo đích (hiện chỉ gọi với `conversation.read`); người có `conversation.assign` mà không có
`conversation.read` trên Page giao được nhưng nhận 404 (tx lùi, dữ liệu không đổi).
