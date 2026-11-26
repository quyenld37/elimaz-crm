# Task 161 — Ghi chú nội bộ và mẫu trả lời có biến

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 161, M4-21). Loại việc (quy trình mục 3): A

Căn cứ: Interfaces, Step và tiêu chí của Task 161 trong kế hoạch M4 (CH07, 01 mục 6); Global Constraints M4; spec 04
mục 3, 7, 11; ADR-18, 41, 46; tệp task-143 (tên quyền, mã lỗi, `m4_pending.txt`), task-151 (lược đồ, CHECK
`messages_note_internal`), task-169 (mẫu `ErrNotFound`, `errors_conversation.go`, `chatEnv`); quyết định chủ shop 26/11
(nhãn, mẫu trả lời là tài nguyên chung: chỉ `conversation.settings` phạm vi all mới tạo/sửa/xóa mẫu chung).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn.
  - Gói `conversation` (gốc) thêm tệp `notes.go`: `AddNote`, `Note` (ghi chú trả về), `ErrNoteInvalid`; tệp
    `templates.go`: `ReplyTemplate`, `TemplateInput`, `TemplatePatch`, `TemplateWriter`, `ListTemplates`,
    `RenderTemplate`, `RenderInput`, `Rendered`, `VariableMissingError`, lỗi `ErrShortcutTaken`, `ErrTemplateInvalid`,
    `ErrUnknownVariable`, `ErrTemplateForbidden`, `ErrTemplateVersionConflict`, `ErrOrderNotOfCustomer`; `views.go`
    thêm hằng `ViewReplyTemplates`. Mẫu, hội thoại không có dùng lại `ErrNotFound` của Task 169 (không thêm sentinel
    404 mới). Không ký hiệu cũ nào đổi.
  - Phần riêng: `internal/templates` (`templates.go`, `render.go`), `internal/send/note.go`.
  - Mã sinh: `internal/modules/conversation/internal/store/{notes,templates}.sql.go`; sqlc sinh thêm hai kiểu
    `ConversationReplyTemplate`, `ConversationReplyTemplatesV` vào 14 tệp `*/internal/store/models.go` (mọi khối sqlc
    dùng chung lược đồ `migrations`), không đổi kiểu có sẵn.
  - `httpapi` (không xuất ra ngoài): `isConversationRefusal` nhận thêm `*conversation.VariableMissingError` (lỗi thiếu
    biến không lưu theo khóa chống trùng); hàm nhỏ `mapAll`, `optNilUUID`, `quickRepliesIn/Out`, `noteOut`,
    `replyTemplateOut`, `templateWrite` trong `handlers_{notes,templates}.go`.
  - `httpapi`: 6 handler mới (`AddConversationNote`, `ListReplyTemplates`, `CreateReplyTemplate`,
    `UpdateReplyTemplate`, `DeleteReplyTemplate`, `RenderReplyTemplate`) thay chỗ `UnimplementedHandler` (501);
    `errorBody` thêm trường `Name` (chỉ có ở `template_variable_missing`), `detailedError` thêm một nhánh.
- Gói phụ thuộc ngược (go list): `internal/modules/conversation` được `cmd/crm` (wire_m4) và `internal/httpapi` nhập;
  `internal/httpapi` được `cmd/crm` nhập. Phần `internal/*` chỉ gói gốc conversation nhập.
- Operation OpenAPI bị chạm, oasdiff: không sửa hợp đồng (6 operation đã khai ở Task 143: `addConversationNote`,
  `listReplyTemplates`, `createReplyTemplate`, `updateReplyTemplate`, `deleteReplyTemplate`, `renderReplyTemplate`);
  xóa 6 dòng đó khỏi `tests/arch/testdata/m4_pending.txt`. oasdiff: không chênh.
- Chỗ dùng trên màn hình: chưa có (màn hộp chat, cài đặt mẫu là Task 175, 178). `console/src/api/**` không đổi.
- Câu sqlc, bảng, pha migration: migration mới `0405_reply_templates.sql` (pha mở rộng: bảng
  `conversation.reply_templates` có RLS, chỉ mục unique một phần phím tắt, khóa ngoại tới `channel.channel_accounts` và
  `platform.files`, view `conversation.reply_templates_v`, GRANT). Câu mới `queries/conversation/templates.sql` (CRUD
  mẫu, danh sách theo tiền tố, đọc tên khách/đơn/nhân viên qua view `customer.customers_v`, `ord.orders_v`,
  `identity.users_v`) và `queries/conversation/notes.sql` (chèn ghi chú). Dùng lại `ReminderConversation` (Task 169)
  để đọc Page, khách của hội thoại. Thêm dòng `conversation.reply_templates_v` vào `docs/views.md`.
- Test sẽ chạy: `go test -race -run` đúng test mới của `internal/modules/conversation` (`TestNote*`,
  `TestTemplate*`, `TestRender*`, `TestReplyTemplatesView*`), `internal/httpapi` (`TestNotesAPI*`,
  `TestReplyTemplatesAPI*`, `TestErrorSnapshot*`, `TestM4Contract*`), `tests/arch` (lockorder, views doc, M4 pending);
  cuối cùng `scripts/agent/ci-task.sh` một lần (gói conversation, httpapi là vùng có sẵn; send là vùng rủi ro cao của M4).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (realtime `Signal`,
  `identity.RecordAudit`, `identity.Can`, `access.Require`, `access.VisiblePages`, `customer.RootOf`,
  `customer.GroupMembers`); mã Task 169 (`reminders.go`: `ErrNotFound = access.ErrNotFound`, `ReminderConversation`,
  `chatEnv`, `errors_conversation.go`, `refusableWrite`+`isConversationRefusal`); `channel.Account` (tên Page);
  handler mẫu `handlers_channel.go`, `handlers_reminders.go` (`optUUID`, `catalogRead`); `staffNames` của httpapi.
- Dùng lại / viết mới vì: dùng lại `access.Require/VisiblePages/PageScope` (kiểm theo Page), `ErrNotFound` (404 không
  lộ tồn tại), `ReminderConversation` (Page + khách của hội thoại), `customer.RootOf/GroupMembers` (đơn cùng nhóm khách),
  `channel.Account` (tên Page), `refusableWrite`, `matchError`, `conversationError` (thêm dòng). Viết mới: bộ phân tích
  biến `{…}` (chưa có chỗ nào thay biến trong chuỗi; mẫu tin của M3/M5 dùng `text/template`, không hợp với cú pháp
  `{ten_khach}` người dùng gõ tay); câu SQL đọc tên khách/đơn/nhân viên riêng cho mẫu (đọc qua view, mỗi câu một cột
  cần, không kéo cả hồ sơ `customer.Get`).

## Soát thiết kế

- 5.1 Chủ dữ liệu: `conversation.reply_templates`, tin `note` thuộc `conversation` (04 mục 3, bảng Sở hữu).
- 5.2 Không ghi bảng module khác.
- 5.3 Đọc module khác qua view: `customer.customers_v` (tên, `name_source` của gốc), `ord.orders_v` (mã, khách, trạng
  thái đơn), `identity.users_v` (`display_name`); tên Page qua public API `channel.Account`; nhóm khách qua
  `customer.RootOf/GroupMembers`. Khóa ngoại `channel_account_id` → `channel.channel_accounts`, `attachment_file_id` →
  `platform.files` (khóa ngoại liên schema như `conversations → customer.customers`).
- 5.4 Mỗi đường ghi một transaction của bên gọi (WriteTx): mẫu + audit; ghi chú + báo hiệu.
- 5.5 Không có việc sau commit; báo hiệu realtime ghi trong tx (bus của nền tảng tự phát sau commit).
- 5.6 Không gọi ra ngoài: ghi chú không tạo lệnh, không job (CH07, có test đếm `platform.jobs` trước/sau).
- 5.7 Bảng mới có `tenant_id`, RLS (`platform.enable_tenant_rls`), unique `(tenant_id, id)`, khóa ngoại ghép; mọi câu
  chạy trong `tenancy.WithTenant`.
- 5.8 Module không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi có `IdempotencyKey`, `x-permission` (đã khai ở Task 143).
- 5.10 Không áp dụng (không Valkey).
- 5.11 Không thêm thư viện (`regexp`, `strings`, `unicode/utf8` của thư viện chuẩn).
- 5.12 Theo mẫu Task 169 (phần trong `internal/<phần>`, gốc chỉ bọc; lỗi sentinel, đổi HTTP ở `errors_conversation.go`).
- 6.1 Hai người sửa cùng mẫu: khóa dòng (`FOR UPDATE`) rồi so `version`, khác → 409; xóa mềm cũng qua version (xóa
  lần hai → 404). Hai người tạo cùng phím tắt cùng lúc: chỉ mục unique một phần bắt (23505 → 409 `already_exists`).
- 6.2 Gửi lại cùng yêu cầu: `Idempotency-Key` (middleware có sẵn); lỗi nghiệp vụ không lưu theo khóa.
- 6.3 Thứ tự khóa: chỉ khóa một dòng `reply_templates` (nhóm 6 ADR-46, đã có tên bảng); ghi chú chỉ chèn `messages`
  (khóa ngoại lấy KEY SHARE trên `conversations`, đúng thứ tự conversations → messages). Không khóa nhiều bảng.
- 6.4, 6.5 `db.RetryTx` qua `WriteTx`; hàm trong tx gán lại biến kết quả mỗi lần chạy; READ COMMITTED.
- 6.6–6.8 Không áp dụng: không việc nền, không gọi ra ngoài.
- 6.9 Câu đọc có giới hạn (danh sách mẫu tối đa 200 dòng).
- 6.10 Không áp dụng: không tiền.
- 6.11 Quy luật "ghi chú không bao giờ ra ngoài": giữ bằng CHECK của database (`messages_note_internal`) và test.
  Hàm thay biến có test quy luật rapid (mọi chuỗi không có `{tên}` hợp lệ giữ nguyên; kết quả không còn biến đã biết).
- 6.12 Dữ liệu cá nhân: nội dung ghi chú, thân mẫu đã điền tên khách không vào log, audit, báo hiệu; audit mẫu chỉ ghi
  tên trường đổi và phím tắt (phím tắt do shop đặt, không phải dữ liệu khách).
- 6.13 Quyền: ghi chú cần `conversation.reply` trên Page của hội thoại (không có → 404); đọc mẫu `conversation.read`
  (mẫu của Page chỉ thấy khi đọc được Page đó); tạo/sửa/xóa **mẫu chung** cần `conversation.settings` phạm vi all,
  mẫu của một Page cần `conversation.settings` trên Page đó (403, kiểm ở handler qua module, test cả hai ca); hiển thị
  mẫu cần `conversation.read` trên Page của hội thoại (404).

## Quyết định thay chủ shop

1. **Mẫu riêng một Page: `conversation.settings` trên Page đó là đủ**; mẫu chung cần phạm vi all (đúng quyết định 26/11).
   Chuyển mẫu giữa phạm vi (Page → chung, chung → Page khác) cần quyền ở cả phạm vi cũ và mới. Vì: quyết định chủ shop
   chỉ nói về tài nguyên chung; cài đặt theo Page vẫn theo quyền của Page. Phương án khác: mọi mẫu (cả mẫu Page) đều cần
   phạm vi all. Đảo ngược: `checkManage` trong `internal/templates/templates.go` bỏ nhánh Page.
2. **`{ma_van_don}` được lưu trong mẫu** (biến có trong danh sách của kế hoạch), nhưng lúc hiển thị trả
   `template_variable_missing` (`name = ma_van_don`) cho tới Task 162. Vì: lưu trước không hại, Task 162 chỉ thêm giá
   trị (không đổi dữ liệu đã lưu). Phương án khác: chặn lúc lưu tới Task 162. Đảo ngược: bỏ `VarTrackingCode` khỏi
   `knownVariable`.
3. **Ghi chú không đổi hội thoại** (`last_message_at`, đoạn trích, số chưa đọc chỉ theo tin với khách), nhưng có báo
   hiệu `conversation.changed` để màn khác thấy ngay. Vì: ghi chú không phải tin của khách, không được đẩy hội thoại lên
   đầu hộp chat hay đổi đoạn trích (đoạn trích hiện ở danh sách). Đảo ngược: thêm một câu UPDATE trong `AddNote`.
4. **Ghi chú trên Page không có `conversation.reply` → 404** (không lộ tồn tại, như Task 169); không có quyền đó ở đâu
   cả → 403 ở guard.
5. **Hiển thị mẫu (POST …/render) chạy qua đường ghi có khóa chống trùng** (`refusableWrite`): hợp đồng đòi
   `Idempotency-Key`; kết quả lưu theo khóa, lỗi (thiếu biến, 404) không lưu, gửi lại xét lại. Không đổi dữ liệu.
6. **Mã lỗi:** không thêm mã mới. Biến lạ lúc lưu, đơn không phải của khách, dữ liệu sai → 422 `invalid_input`; phím
   tắt trùng → 409 `already_exists` (đúng summary của `createReplyTemplate`); thiếu phạm vi → 403 `forbidden`; thiếu
   giá trị → 422 `template_variable_missing` kèm `name` (trường `name` đã khai ở Task 143).
7. **Tệp đính kèm của mẫu:** chỉ khóa ngoại tới `platform.files` (tệp không có → 422). Mục đích `chat_outgoing` và
   trạng thái sẵn sàng chưa có (Task 155, 156 thêm); lúc gửi (Task 156) kiểm lại. Phương án khác: chặn tệp tới khi có
   mục đích. Đảo ngược: thêm kiểm trong `validate`.
8. **`{ten_khach}` lấy tên của gốc nhóm khách** (sau gộp), tên tạm (`name_source = placeholder`) → "bạn";
   **`{ma_don}`** không chỉ định → đơn chưa hủy mới nhất của **cả nhóm khách** (mọi hồ sơ cùng gốc); chỉ định → đơn phải
   thuộc nhóm khách (đơn nháp cũng được).
9. **Danh sách không có `pageId`** trả mẫu chung và mẫu của mọi Page người gọi đọc được (màn cài đặt mẫu cần); có
   `pageId` → mẫu chung + mẫu Page đó (menu "/").

## Bằng chứng

Lệnh chung: M = `go test -race -count=1 -run '^(TestTemplate|TestRender|TestFill|TestReplyTemplatesView|TestNote_)'
./internal/modules/conversation/internal/templates/ ./internal/modules/conversation/internal/send/`; A = `go test -race
-count=1 -run '^(TestNotesAPI_AddNote|TestReplyTemplatesAPI_.*|TestDomainError.*|TestM4Contract.*)$' ./internal/httpapi/`.
Trước khi viết mã: cả hai đỏ (không biên dịch: `undefined: send.AddNote`, `undefined: templates.Writer`…). Sau (bản đã
gộp m4 1d343e8): `ok …/internal/templates 6.480s`, `ok …/internal/send 6.189s`, `ok …/internal/httpapi 6.510s`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH07: ghi chú → 1 tin `note`/`out`/`staff`, không trạng thái gửi, không mã ngoài, không lệnh; số dòng `jobs` trước = sau; 1 báo hiệu `conversation.changed` theo Page | `TestNote_NeverLeaves` (module), `TestNotesAPI_AddNote` (API 201, gửi lại cùng khóa `Idempotent-Replayed`, vẫn 1 tin) | M, A | PASS |
| CH07: `UPDATE … SET send_status = 'queued'` (cả `external_id`, `command_job_id`) trên ghi chú → vi phạm CHECK `messages_note_internal` | `TestNote_CheckBlocksSending` | M | PASS (23514) |
| Ghi chú: chỉ đọc / Page khác → 404 (API: không reply ở đâu → 403 guard), rỗng, > 4.000 ký tự → lỗi; 4.000 ký tự có dấu được | `TestNote_AccessAndLength`, `TestNotesAPI_AddNote` | M, A | PASS |
| 01 mục 6: `xindiachi` "Chào {ten_khach}, …" cho Lan → "Chào Lan, …"; tên tạm → "Chào bạn, …"; nút cũng điền biến | `TestRender_CustomerName`, `TestReplyTemplatesAPI_ListAndRender` | M, A | PASS |
| `{ten_page}`, `{nhan_vien}` (tin tự động → tên Page) | `TestRender_PageAndStaff` | M | PASS |
| `{ma_don}` khách chưa có đơn → 422 `template_variable_missing`, `name = ma_don`; mới nhất chưa hủy của nhóm khách; chỉ định; đơn khách khác / không có → 422; `{ma_van_don}` → thiếu giá trị (Task 162) | `TestRender_OrderCode`, `TestReplyTemplatesAPI_ListAndRender` (thân lỗi có `name`) | M, A | PASS |
| `{abc}` lúc lưu → 422 (thân, nút); phím tắt trùng khác hoa thường → 409; dữ liệu sai (phím tắt, tiêu đề, thân, 14 nút, nút 21 ký tự, tệp/Page không có) → 422; xóa mềm rồi dùng lại phím tắt | `TestTemplateCreate_ValidationAndUnique`, `TestReplyTemplatesAPI_PermissionsAndCRUD` | M, A | PASS |
| Quyết định chủ shop 26/11 (hai ca): `conversation.settings` chỉ Page A tạo/sửa/xóa mẫu chung → 403; tạo/sửa/xóa mẫu Page A → được; không chuyển mẫu Page A thành chung; phạm vi all → được | `TestTemplatePermissions_CommonNeedsAllPages`, `TestReplyTemplatesAPI_PermissionsAndCRUD` | M, A | PASS |
| Sửa có `expectedVersion` (cũ → 409), chỉ đổi trường gửi; audit create/update/delete (`changed_fields = {body}`, không có nội dung thân); xóa lần hai → 404; tenant B không xóa được mẫu của A | `TestTemplateUpdateDelete_Version`, `TestTemplateCreate_ValidationAndUnique` | M | PASS |
| Mẫu giới hạn Page 1 không có khi hội thoại thuộc Page 2; `q = "xin"` → có `xindiachi`; khớp tiền tố tiêu đề không phân biệt hoa thường; Page không đọc được → 404 | `TestTemplateList_PageAndPrefix`, `TestReplyTemplatesAPI_ListAndRender` | M, A | PASS |
| Hiển thị cho hội thoại không có quyền → 404; mẫu Page A ở hội thoại Page B → 404 | `TestRender_AccessAndScope`, `TestReplyTemplatesAPI_ListAndRender` | M, A | PASS |
| `reply_templates_v`: mẫu đã xóa không có; không cột `body`; cột khớp `docs/views.md`; tenant B không thấy | `TestReplyTemplatesView`; `TestViewsDoc*` (tests/arch, 47 view) | M; `go test -count=1 -skip '^TestLintNotBlockedByOtherRunners$' ./tests/arch/` | PASS; `ok elimaz.com.vn/crm/tests/arch 149.367s` |
| Quy luật thay biến (rapid): chuỗi không biến giữ nguyên; kết quả không còn biến; `Variables` đúng tập | `TestFill_Properties` | M | PASS |
| Mã lỗi mới đúng thứ tự, có trong snapshot; 6 operation không còn 501 (`m4_pending.txt`) | `TestDomainErrorSnapshot*`, `TestM4Contract_StatusCodes`, `TestM4Contract_Operations` | A; tests/arch | PASS |
| Thứ tự khóa: bảng mới của `0405` có tên trong ADR-46 | `TestLockOrder_M4TablesListed` | tests/arch | PASS |
| Cổng của task (mọi cổng tĩnh, test kiến trúc, test toàn bộ trừ e2e vì bản gộp m4 đổi testkit) | — | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh aafa087…)`; `ok …/internal/modules/conversation/internal/templates 4.111s`, `…/send 2.762s`, `…/conversation 39.680s`, `…/internal/httpapi 94.376s`, `…/tests/arch 16.181s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ca "`resend` một ghi chú → 404": `resendMessage` là của Task 157 (còn trong `m4_pending.txt`, trả 501), chưa kiểm
  được qua API. Quy luật "ghi chú không bao giờ có trạng thái gửi" đã giữ bằng CHECK `messages_note_internal`
  (`TestNote_CheckBlocksSending`: `send_status`, `external_id`, `command_job_id` đều bị chặn); Task 157 thêm ca resend.
- Ca "fakegraph 0 lời gọi": ghi chú không tạo job hay lệnh nào (test đếm `platform.jobs` trước/sau ở module và API), nên
  không có đường nào tới Graph; không dựng máy chủ Meta giả cho test này.
- Thêm tệp ngoài danh sách của kế hoạch: `queries/conversation/notes.sql` (câu chèn ghi chú),
  `internal/modules/conversation/{notes.go,templates.go}` (phần công khai bọc phần riêng, như Task 169),
  `internal/modules/conversation/internal/templates/env_test.go` (dữ liệu thử chung), test API ở
  `internal/httpapi/handlers_templates_test.go` (cả ghi chú và mẫu).

## Việc phát hiện thêm

1. `conversation.page_settings.ask_address_template_id` chưa có khóa ngoại tới `conversation.reply_templates` (0402 ghi
   "thêm khi bảng đích có"). Không thêm ở 0405 vì bảng `page_settings` thuộc Task 160 đang làm song song (test của 160
   có thể chèn mã mẫu ngẫu nhiên). Đề xuất: Task 160 (hay task gộp sau) thêm khóa ngoại và chặn xóa mẫu đang là mẫu xin
   địa chỉ của một Page.
2. `tests/arch` `TestLintNotBlockedByOtherRunners` treo > 6 phút khi máy đang bận (chờ golangci-lint của phiên khác);
   các test kiến trúc còn lại chạy xanh với `-skip` (xem bằng chứng). Việc công cụ, không thuộc task này.

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA — mục Ảnh hưởng ghi tên không có trong mã (`NoteInput`, `Message`, `ErrTemplateNotFound`), thiếu
  `Note`, `ErrNoteInvalid`, `ErrUnknownVariable`. Đã sửa tệp task (không đổi mã); thêm dòng mã sinh `models.go`,
  `isConversationRefusal`, hàm nhỏ trong httpapi.
- Lượt 2: ĐẠT — không còn phát hiện phải sửa; mọi dòng bằng chứng trỏ tới test thật kiểm đúng tiêu chí; mục 4–7 không
  có phát hiện. Ghi chú không bắt buộc: `tests/arch` chạy với `-skip TestLintNotBlockedByOtherRunners` (đã ghi ở Việc
  phát hiện thêm; ci-task chạy đủ `tests/arch` xanh); kết quả hiển thị mẫu (có tên khách) được lưu theo khóa chống
  trùng như các đường ghi khác, không vào log; người có quyền chỉ ở Page A sửa mẫu Page B nhận 403 (biết mẫu tồn tại) —
  mẫu là tài nguyên của shop, chấp nhận được.
