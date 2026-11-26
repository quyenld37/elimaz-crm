# Task 151 — Lược đồ hội thoại lõi

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 151 = M4-11). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu có sẵn. Mới: `access.PageScope`,
  `access.Require`, `access.VisiblePages`, `access.ErrNotFound` (gói `conversation/internal/access`, 0 chỗ dùng ngoài
  gói lúc này: `go list` không gói nào nhập nó); `conversation.ViewConversations`, `conversation.ViewTags` (hằng tên
  view, `views.go`); gói sinh `conversation/internal/store` (sqlc, mới).
- Gói phụ thuộc ngược (go list): `internal/modules/conversation/internal/access` — không gói nào nhập; gói gốc
  `conversation` chỉ được `cmd/crm` (`wire_m4.go`) nhập; task chỉ thêm hằng nên không đổi hành vi ở đó.
- Operation OpenAPI bị chạm, oasdiff: không có (task chỉ lược đồ, truy vấn, kiểm quyền).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: migration mới `0402_conversation_core.sql` (pha mở rộng: CREATE SCHEMA/TABLE/INDEX/
  VIEW/TRIGGER, GRANT; không thu hẹp). Bảng mới `conversation.{tags,page_settings,posts,conversations,
  conversation_tag_links,conversation_assignments,messages,message_revisions}`, view `conversation.conversations_v`,
  `conversation.tags_v`. Khóa ngoại tới bảng có sẵn: `customer.customers (tenant_id, id)`, `identity.users (tenant_id,
  id)`. Câu sqlc mới (khối mới trong `sqlc.yaml` → `internal/modules/conversation/internal/store`):
  `queries/conversation/conversations.sql` (`ListConversationsByPage`, `ListConversationsByAssignee`),
  `messages.sql` (`ListMessages`, `ListLiveComments`), `posts.sql` (`UpsertPost`).
- Test sẽ chạy: `internal/modules/conversation/...` (`schema_test.go`, `explain_test.go`, `access_test.go`,
  `wiring_test.go`), `tests/arch` (lockorder, viewsdoc, views_m4, sqlusage, boundaries), `internal/platform/tenancy`
  (CheckSchemaRules chạy trên toàn bộ migration). Migration mới chỉ thêm → ci-task chọn gói dùng đối tượng nó chạm.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit:
  `OpenStaff`, `OpenStaffSeeded`, `CheckSchemaRulesClean`, `CheckViews`, `CheckOwnerAppendOnly`, `ExplainAsApp`,
  `EnableAutoExplain`, `RequireNoSeqScan`; tenancy: `WithTenant`, `CheckSchemaRules`; identity: `Can`, `ScopesFor`,
  `Scope`, `ScopePage`); mẫu migration chỉ ghi thêm của `0004_audit.sql`, `0115_inventory.sql` (trigger theo câu lệnh
  chặn UPDATE, DELETE, TRUNCATE mã 42501 "append-only"), `platform.enable_tenant_rls`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm testkit trên và `identity.Can`/`ScopesFor` (access chỉ bọc quyền
  theo Page: dựng `Scope{page, id}` và đổi "không quyền" thành `ErrNotFound`). Viết mới: hàm trigger
  `conversation.*_append_only` (mỗi bảng chỉ ghi thêm có hàm riêng như mẫu, vì thông báo nêu tên bảng; riêng
  `message_revisions` cho `DELETE` khi `app.retention = on`, Task 164). `storeSQL` trong test: mẫu của
  `customer/search_explain_test.go` là hàm test cục bộ của gói khác (gói `_test` không nhập được), chép lại ngắn.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: hội thoại, tin, bài viết, nhãn, phân công, cài đặt Page thuộc `conversation` (04 mục 3); mọi bảng
  trong schema `conversation`.
- 5.2 Không ghi bảng module khác: migration chỉ tạo bảng `conversation.*`; khóa ngoại tới `customer.customers`,
  `identity.users` là ràng buộc, không ghi.
- 5.3 Đọc qua view: câu sqlc chỉ đọc bảng `conversation.*`; công bố `conversations_v`, `tags_v` cho M6, M7.
- 5.4, 5.5, 5.6: không áp dụng (task không có đường ghi nghiệp vụ, sự kiện hay lệnh ra ngoài).
- 5.7 Ngữ cảnh shop: mọi bảng có `tenant_id`, `UNIQUE (tenant_id, id)`, khóa ngoại ghép, RLS ép; test
  `CheckSchemaRulesClean` và test tenant B không đọc được của A.
- 5.8 Không HTTP: `access` chỉ nhập `identity`, `uuid`.
- 5.9 Không áp dụng: không có route ghi.
- 5.10 PostgreSQL là nguồn sự thật: ràng buộc nghiệp vụ (ghi chú không ra ngoài, một tin riêng mỗi comment) nằm ở
  CHECK, unique một phần trong database.
- 5.11 Không thêm thư viện.
- 5.12 Theo module mẫu: migration như `0523`, test lược đồ như `order/schema_test.go`, test kế hoạch như
  `customer/feedback_counts_test.go`.

Mục 6:
- 6.1 Đồng thời: unique một phần `messages_one_live_private_reply` chặn hai tin riêng đang sống cho một comment kể cả khi
  hai transaction chèn cùng lúc (unique của database, không kiểm ở Go); `version` có sẵn trên bảng người sửa.
- 6.2 Gửi lại: `UNIQUE (tenant_id, channel_account_id, external_id)` của `messages`, `UNIQUE (tenant_id, client_ref)`,
  `UNIQUE (tenant_id, channel_account_id, kind, external_thread_ref)` của `conversations`, `UpsertPost` `ON CONFLICT`.
- 6.3 Thứ tự khóa: tám bảng mới đã có trong nhóm 6 của ADR-46 (Task 143 đối chiếu); `TestLockOrder_M4TablesListed`.
- 6.4–6.9: không áp dụng (không transaction, việc nền mới).
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: không vùng rủi ro cao; quy luật ràng buộc kiểm bằng test bảng.
- 6.12 Dữ liệu cá nhân: `body`, `sender_name`, `last_snippet` là dữ liệu cá nhân; task không log gì.
- 6.13 Quyền: `access.Require` mặc định từ chối, thiếu quyền trên Page → `ErrNotFound` (không lộ sự tồn tại); grant
  phạm vi kho không cho thấy Page nào.

## Quyết định thay chủ shop

1. **Chưa có khóa ngoại `channel_account_id → channel.channel_accounts`.** Bảng `channel.channel_accounts` do Task 145
   (`0401_channel.sql`) tạo, chưa gộp vào `m4` (nhánh task-145 chưa có migration); khóa ngoại tới bảng chưa có làm
   migration 0402 không chạy được và mọi test của nhánh này đỏ. Chọn: cột `channel_account_id uuid NOT NULL` không có
   khóa ngoại tới `channel`; ghi vào "Việc phát hiện thêm" để thêm bằng một migration sau khi 145 gộp (`ALTER TABLE …
   ADD FOREIGN KEY (tenant_id, channel_account_id) REFERENCES channel.channel_accounts (tenant_id, id)`, chỉ thêm ràng
   buộc). Phương án khác: (a) gộp nhánh 145 chưa duyệt vào đây (kéo mã chưa review); (b) tạo tạm `channel.channel_accounts`
   ở 0402 (đụng bảng của module khác, xung đột với 0401). Đảo ngược: migration thêm khóa ngoại như trên.
2. **`views.go` chứa hằng tên hai view công bố** (`ViewConversations`, `ViewTags`) kèm chú thích cột; kế hoạch chỉ nêu
   tên tệp. Test lược đồ dùng hằng này. Phương án khác: hàm đọc view (chưa task nào cần). Đảo ngược: xóa tệp.
3. **`conversations.customer_id NOT NULL`**: mọi hội thoại gắn một khách (Task 152 tìm-hoặc-tạo khách trước). Kế hoạch
   ghi khóa ngoại, không nói rỗng; NOT NULL chặt hơn, nới về sau (`DROP NOT NULL`) là thay đổi mở rộng.
4. **Khóa ngoại chưa đặt** cho `page_settings.ask_address_template_id` (bảng `reply_templates` của Task 161),
   `inbox/comment_traffic_source_id` (bảng của `ord`, chỉ đọc qua view), `avatar_file_id` (Task 155),
   `round_robin_*` (mảng): bảng đích chưa có hoặc thuộc module khác chỉ công bố view. `assignee_id`, `from/to_user_id`,
   `actor_id`, `created_by`, `sender_staff_id` có khóa ngoại ghép tới `identity.users` chỉ ở `assignee_id` (kế hoạch
   nêu); cột người làm khác giữ uuid trơn như `audit_logs.actor_id`.
5. **Quyền của `crm_app`**: SELECT, INSERT mọi bảng; UPDATE trên bảng sửa được; DELETE trên `conversation_tag_links`
   (gỡ nhãn) và `message_revisions` (chỉ khi `app.retention = on`, trigger chặn còn lại); không UPDATE/DELETE trên
   `conversation_assignments`, `message_revisions` (cùng trigger chặn cả chủ bảng).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Lược đồ đúng luật tenant (`CheckSchemaRules` → rỗng); hai view `security_invoker`, đúng cột docs/views.md; `tags_v` bỏ nhãn đã xóa; tenant B không đọc hội thoại, nhãn, bài của A; tên nhãn trùng (không phân biệt hoa thường) → 23505, trùng tên nhãn đã xóa được | `TestSchema_RulesViewsTenant` (`internal/modules/conversation/schema_test.go`) | `go test -count=1 -race -run '^(TestUpsertPost\|TestSchema_.*)$' ./internal/modules/conversation/` | trước khi viết mã: `undefined: conversation.ViewTags` (build failed); sau: `ok … conversation 2.287s` |
| Hội thoại: comment phải có bài, inbox không; trùng luồng → 23505; `unread_count` âm → 23514 | `TestSchema_ConversationChecks` | như trên | `ok` |
| CH07 nền: ghi chú có `send_status`/`external_id`/`command_job_id`, ghi chú đi vào → 23514; tin nhân viên/tự động thiếu `send_status` → 23514; `direction in ⇔ origin customer`; `external_id`, `client_ref` trùng → 23505 | `TestSchema_NoteNeverLeaves` (13 ca + client_ref) | như trên | `ok` |
| CH05 nền: hai `private_reply` đang sống (queued/sent/checking/in_doubt) cùng comment → 23505; comment khác được; tin đầu `failed` → chèn được; tin đã kết thúc (failed/expired/discarded) không bị chặn | `TestSchema_OnePrivateReplyPerComment` | như trên | `ok` |
| Chỉ ghi thêm: crm_app UPDATE/DELETE trên `conversation_assignments`, `message_revisions` → 42501; chủ bảng bị trigger chặn ("append-only"); `app.retention = on` → DELETE `message_revisions` xóa 1 dòng, UPDATE vẫn 42501 | `TestSchema_AppendOnly` | như trên | `ok` |
| Khóa ngoại ghép: khách tenant khác, tin không hội thoại, nhãn không có, người phụ trách lạ → 23503; người phụ trách đúng, cài đặt Page mặc định được; `automation_max_age_minutes` 4 → 23514; bài trùng → 23505 | `TestSchema_ForeignKeys` | như trên | `ok` |
| `UpsertPost`: gửi lại cùng mã → cùng bài; loại không bị `unknown` ghi đè; trường rỗng giữ giá trị cũ | `TestUpsertPost` | như trên | `ok` |
| PQ04 nền: `Require` grant `conversation.read` page:A hỏi Page B → `ErrNotFound`; all được; quyền khác, grant kho cùng mã, không grant, quyền lạ → `ErrNotFound`; `PageScope`; `VisiblePages` (all; sắp, bỏ trùng; bỏ kho và mã hỏng) | `TestRequire`, `TestPageScope`, `TestVisiblePages` (`internal/access/access_test.go`) | `go test -count=1 ./internal/modules/conversation/...` | trước: `undefined: access.Require` (build failed); sau: `ok … conversation/internal/access 0.864s` |
| 04 mục 7: bốn truy vấn lõi bằng crm_app trên 200.000 hội thoại (2 Page), 500.000 tin, ANALYZE: không Seq Scan, mỗi câu có dòng | `TestCoreQueries_Plan` (`explain_test.go`) | `go test -count=1 -run '^TestCoreQueries_Plan$' ./internal/modules/conversation/` | `ok … conversation 3.165s` (lần đầu đỏ `listLiveComments: 0 dòng` vì dữ liệu mẫu không có comment live; đã sửa dữ liệu mẫu và thêm chặn mã rỗng) |
| Bảng mới có trong ADR-46; view có dòng docs/views.md (44 view); `sqlc vet`; mã sinh khớp | `TestLockOrder_M4TablesListed`, `TestViewsDocMatchesDatabase`, `TestViewsDocParses`, `TestSqlcVet` | ci-task | mọi cổng tĩnh xanh (build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, test kiến trúc); `ok … tests/arch 26.699s`, `ok … conversation 45.693s`, `ok … conversation/internal/access 1.591s` |
| Toàn bộ test (affected chọn toàn bộ vì mã sinh mọi store đổi) | ci-task | ci-task | ĐỎ đúng một test ngoài phạm vi: `TestPostWithIDIdempotent` (cash; so `occurred_at` với `now()` trong cửa sổ 1 phút, máy đang bận); chạy riêng `go test -count=3 -run '^TestPostWithIDIdempotent$' ./internal/modules/cash/` → `ok`. Mọi gói khác `ok`. |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không có khóa ngoại tới `channel.channel_accounts` (Quyết định 1).
- `tests/arch/viewsdoc_test.go`: số view mong đợi 42 → 44 (hai view mới).
- Thêm ngoài danh sách cột của kế hoạch: `tags.created_at`, `page_settings.updated_at`; CHECK dạng cho `color` (tên
  token), `live_slots`, `attachments` (mảng JSON), độ dài `external_id` của bài, `external_thread_ref`; index
  `conversation_assignments (tenant_id, conversation_id, created_at)`, `message_revisions (tenant_id, message_id,
  recorded_at)` để đọc lịch sử theo đối tượng.

## Việc phát hiện thêm

- Sau khi Task 145 gộp vào `m4`: thêm migration (số kế tiếp trong dải 04xx) đặt khóa ngoại ghép
  `(tenant_id, channel_account_id) → channel.channel_accounts (tenant_id, id)` cho `conversation.page_settings`,
  `posts`, `conversations`, `messages` (điều phối viên giao cho Task 152 hoặc task gộp).
- Gộp với Task 145: cả hai thêm khối vào `sqlc.yaml`, dòng vào `docs/views.md` và số view của `TestViewsDocParses`
  (145 thêm `channel.channel_accounts_v` → 45).
- Test chập chờn có sẵn: `cash` `TestPostWithIDIdempotent` so `occurred_at` với `now()` trong cửa sổ 1 phút, đỏ khi
  máy bận (ci-task 25/11); qua khi chạy riêng.

## Reviewer

Kết luận: ĐẠT (lượt 1, 25/11). Không phát hiện phải sửa. Đã đối chiếu migration 0402 với kế hoạch (CH07
`messages_note_internal`, CH05 `messages_one_live_private_reply`, trigger chỉ ghi thêm và ngoại lệ `app.retention`,
RLS 8 bảng, `UNIQUE (tenant_id, id)`, khóa ngoại ghép, GRANT theo Quyết định 5), thứ tự khóa nhóm 6 ADR-46 đủ 8 bảng,
từng dòng bảng bằng chứng khớp test. Ghi chú không bắt buộc: `models.go` sinh lại của 12 gói store khác chỉ thêm
struct (nêu ở bằng chứng); `app.retention` session nào cũng tự bật được — đúng thiết kế của kế hoạch; kế hoạch ghi việc
dọn theo hạn giữ là "Task 24" (số cục bộ M4-24 = Task 164 toàn cục), tệp này ghi 164.
