# Task 81 — M2-24: Đọc đơn — danh sách, chi tiết, lịch sử thao tác, ghi chú

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 81, M2-24). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn.
  - `order`: thêm `ListFilter`, `OrderRow`, `Cursor` (bí danh `customer.ListCursor`), `ParseCursor`, `List` (list_dynsql.go); `OrderDetail`,
    `CustomerSummary`, `ReturnSummary`, `EditScope`, `GetDetail` (read.go); `OrderTimelineItem`, `AuditChange`,
    `AuditSource`, `Timeline`, hằng `TimelineStatus/Edit/Note` (timeline.go); `Note`, `AddNote` (notes.go); sentinel
    `ErrLabelNotFilterable` (errors.go, chỉ thêm dòng); hằng `DefaultListLimit`, `MaxListLimit`, `WarningPossibleDuplicate`;
    kiểu `NoteBody` (che khi ghi log). `order.Get` (M1) giữ nguyên: người gọi
    `internal/httpapi/handlers_order.go` (GetOrder chuyển sang `GetDetail`), `internal/modules/fulfillment`,
    `internal/testkit/shiptest` và test của order không đổi.
  - `identity`: thêm `AuditRecord`, `ListEntityAudit` (audit.go) — đọc tên trường đổi của một thực thể, không trả giá trị.
  - `internal/httpapi` (commit cấu trúc): `handlers_customer_timeline.go` tách `actorNamesOf`, `writeNote`,
    `writtenNote`, `timelineEntry`, `timelineItemOut` (chỉ dùng trong gói; người gọi: `AddCustomerNote`,
    `GetCustomerTimeline`, và ở commit tính năng `ListOrders`, `GetOrder`, `GetOrderTimeline`, `AddOrderNote`).
  - `internal/httpapi` (commit tính năng): tệp mới `handlers_order_read.go` (`ListOrders`, `GetOrderTimeline`,
    `AddOrderNote`, `orderDetailOut`, `orderAudit`); `handlers_order.go` `GetOrder` dùng `GetDetail` (chỉ thêm trường);
    `errors.go` thêm một dòng `order.ErrLabelNotFilterable` → 422 `invalid_input`. Test mới `handlers_order_read_test.go`
    (dùng helper có sẵn `newOrderConfirmEnv`, `serveAs`, `expect`, không sửa tệp helper chung).
  - Test order: `export_list_test.go` (mới, riêng, không sửa `export_test.go` chung) xuất `ListSQL` cho test kế hoạch.
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`,
  `internal/testkit/shiptest`; `identity` ← mọi gói nhập identity (chỉ thêm hàm, không đổi gì có sẵn).
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng đã khai ở Task M2-02). Hiện thực
  `listOrders`, `getOrderTimeline`, `addOrderNote` (đang 501), mở rộng thân `getOrder` (chỉ thêm trường tùy chọn đã khai:
  customer, careBy, createdBy, couponCode, cancelReason, cancelNote, editScope, returns, warnings). oasdiff: không đổi.
- Chỗ dùng trên màn hình: chưa có màn danh sách/chi tiết đơn ở console (task màn hình riêng); client sinh không đổi.
- Câu sqlc, bảng, pha migration:
  - `queries/ord/read.sql` (mới): `OrderExists`, `ListOrderDuplicateLinks`, `ListOrderReturns`. Khách của chi tiết đơn
    đọc qua hàm công khai `customer.Get` (không câu sqlc nào của order đọc khách); danh sách đọc `customer.customers_v`
    trong SQL động. `queries/ord/timeline.sql` (mới): `ListOrderHistory`, `ListOrderNotes`. `queries/ord/notes.sql`
    (mới): `InsertOrderNote`. `queries/identity/audit.sql`: thêm `ListEntityAudit`.
  - SQL động: `internal/modules/order/list_dynsql.go` (ord.orders, ord.order_items, customer.customers_v).
  - Migration mới `0211_order_list_index.sql`: pha **mở rộng** (chỉ thêm index `orders_seller_created (tenant_id,
    seller_id, created_at DESC, id DESC)`), xem "Quyết định thay chủ shop".
  - Bảng đọc: ord.orders, ord.order_items, ord.order_status_history, ord.order_notes (ghi thêm), ord.order_duplicate_links,
    ord.returns, identity.audit_logs (chỉ qua identity), customer.customers_v.
- Test sẽ chạy: `go test -race ./internal/modules/order/... ./internal/modules/identity/... ./internal/httpapi/...
  ./tests/arch/...`; `scripts/agent/ci-task.sh` một lần (identity là vùng rủi ro cao → cả bộ identity; migration mới →
  toàn bộ theo ci-task).

### Lượt 2 (gộp main: Task 77 sửa đơn, 78 hủy, 79 trả/đổi, 80 đơn trùng)

- Gộp `main` một lần (không xung đột); `make gen` sau gộp không chênh.
- Migration `0210_order_list_index.sql` trùng số với `0210_order_edit.sql` (Task 77) → `git mv` thành
  `0211_order_list_index.sql` (tệp chưa lên main; nội dung không đổi). Chỗ nhắc số: chỉ tệp task này.
- `identity.AuditRecord`: bỏ `ChangedFields`, `HasBefore` (chỉ lịch sử đơn dùng, lượt 1), thêm `ListedFields []string`;
  câu `ListEntityAudit` (`queries/identity/audit.sql`) bỏ `changed_fields`, `has_before`, thêm cột `listed_fields` = mảng
  chuỗi ở `after->'fields'` (không phải mảng → rỗng; chỉ tên trường, không giá trị). Người dùng (3, LSP/grep):
  `httpapi.orderAudit`, `order_test.auditSource`, `identity_test.TestListEntityAudit`.
- `order.AuditChange`: bỏ `Fields`, `Diff`, thêm `Edited []string`; `editItem` (timeline.go) chỉ dựng mục sửa từ
  `Edited` (dạng audit `order.update` của Task 77: chỉ `after`, `after.fields` = `EditResult.Fields`); xóa `statusFields`
  và đường so before/after cũ (audit duy nhất có `before` là `order.cancel` của 78, chỉ đổi trạng thái; giữ cách cũ thì
  khóa `hasNote` của 78 thành mục sửa giả).
- `order.GetDetail` gọi `promotion.OrderCouponCode` (Task 78 đổi tên `ActiveCouponCode`; đơn hủy vẫn trả mã đã dùng) —
  bản gộp không build được tới khi sửa.
- `order.Summary` (Task 80) thêm `CareBy *uuid.UUID`; câu `OrderSummaries` (`queries/ord/duplicates.sql`) thêm `o.care_by`;
  `httpapi.orderListItemOut(s, names)` nhận bảng tên — người gọi: `CheckOrderDuplicates`, `ListOrderDuplicates`,
  `ResolveOrderDuplicate` (qua `duplicateOut`). Hợp đồng không đổi (`OrderListItem.careBy` đã khai, tùy chọn).
- Test: `order_test.TestTimeline_DH06` dùng `Writer.Edit` (77) và `Writer.Cancel` (78) thật, audit ghi đúng dạng handler;
  `httpapi.TestOrderRead_EditCancelTimeline` (mới) đi qua PATCH, confirm, cancel thật rồi đọc timeline và chi tiết
  (`cancelReason`, `cancelNote`); `httpapi` test đơn trùng kiểm `careBy` có tên.
- Test sẽ chạy: `go test -race ./internal/modules/order/... ./internal/modules/identity/... ./internal/httpapi/...`;
  `ci-task.sh` một lần (identity vùng rủi ro cao, đổi migration → toàn bộ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db, tenancy, logging,
  textnorm), `customer.Search`/`RootOf`/`ReverseDigits`/`NormalizePhone`/`ErrQueryTooShort`, `orderview.Label`,
  `state.Editable`, `promotion.OrderCouponCode` (lượt 2: Task 78 đổi tên từ `ActiveCouponCode`), `httpapi.staffNames`, `readAsDefaultSeller`, `refusableWrite`, mẫu
  con trỏ keyset của `queries.Cursor`, `customer.TimelineCursor`, `testkit.ExplainAsApp`, `testkit.StartPostgresSeeded`.
- Dùng lại / viết mới vì:
  - Dùng lại: `customer.Search` cho `q` là tên (không viết lại tìm tên), `customer.NormalizePhone`, `customer.ReverseDigits`
    cho đuôi SĐT (cùng luật Task M2-10), `customer.RootOf` cho lọc theo khách gốc, `orderview.Label` (nhãn ở dòng và chi
    tiết), `state.Editable` (EditScope), `promotion.OrderCouponCode` (lượt 2: Task 78 đổi tên từ `ActiveCouponCode`) (mã của đơn), `order.Get` (phần đơn của chi tiết),
    `staffNames` (tên người làm ở handler), `refusableWrite` (ghi chú), `testkit.ExplainAsApp`.
  - jscpd (ci-task lần 1 đỏ, 6 chỗ trùng mới) — trả lời từng chỗ: (1) con trỏ danh sách đơn trùng `queries.Cursor`
    rồi `customer.ListCursor` → **gộp**: `order.Cursor = customer.ListCursor` (cùng khóa keyset `(created_at, id)` giảm,
    cùng mã hóa; order được gọi customer), `order.ParseCursor` chỉ đổi lỗi sang `order.ErrInvalidInput`; (2) ba đoạn
    handler trùng dòng thời gian/ghi chú khách → **gộp** thành helper của httpapi `actorNamesOf`, `writeNote`
    (+ `writtenNote`), `timelineItemOut` (+ `timelineEntry`) trong `handlers_customer_timeline.go`, **commit cấu trúc
    riêng** (chỉ dời mã, hành vi khách không đổi, `TestCustomerNotesTimelineAPI` qua) trước commit tính năng; (3) bộ mở
    pool của test kế hoạch trùng `customer/search_explain_test` → dùng `testkit.OpenStaffSeeded`; (4) bộ nối audit của
    test trùng `orderAudit` của handler → **giữ** (test gói order_test không nhập được httpapi), viết lại gọn.
  - Viết mới: `identity.ListEntityAudit` (chưa có hàm đọc audit; order không được nhập
    identity nên đọc qua tham số `AuditSource` do lớp HTTP nối); kiểm "chuỗi chỉ chữ số" nhỏ trong order (hàm của
    customer không xuất).

### Lượt 2

- Đọc `docs/danh-muc-dung-chung.md`; dùng lại `actorNamesOf`/`staffNames` cho tên người phụ trách ở đơn trùng (không viết
  hàm tra tên mới), `Writer.Edit`, `Writer.Cancel` thật trong DH06. `order.Summary` (80) và `order.OrderRow` (81) gần
  giống: **giữ riêng** — nguồn khác (Summary đọc theo danh sách mã qua sqlc tĩnh, OrderRow từ SQL động có lọc, khóa
  keyset), lý do đổi khác (danh sách lọc thêm trường hiển thị theo M3; đơn trùng chỉ cần cặp); chỉ cùng hình dạng ở hợp
  đồng `OrderListItem`. Gộp ở task màn hình đơn nếu hai bên cần cùng trường.

## Soát thiết kế

5.1 Chủ dữ liệu: ghi chú đơn thuộc `order` (bảng `ord.order_notes` có từ 0204); index mới trên `ord.orders` của order.
5.2 Không ghi bảng module khác: chỉ chèn `ord.order_notes`; audit của AddNote do lớp HTTP ghi bằng `identity.RecordAudit`.
5.3 Đọc module khác qua view: khách qua `customer.customers_v`; audit qua hàm công khai `identity.ListEntityAudit` (lớp
HTTP gọi, order nhận qua `AuditSource`) — order không nhắc schema `identity`.
5.4 Nguyên tử: AddNote + audit trong một WriteTx. 5.5 Không có việc sau commit (không sự kiện; báo hiệu realtime của
ghi chú: không cần ở task này). 5.6 Không gọi ngoài: không áp dụng.
5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant` (readAsDefaultSeller/WriteTx); điều kiện `seller_id` ở tầng dưới
(ADR-29) cho danh sách, chi tiết, lịch sử, ghi chú. 5.8 Module không nhập net/http. 5.9 addOrderNote có IdempotencyKey và
`x-permission: order.update` (hợp đồng có sẵn). 5.10 Không Valkey. 5.11 Không thư viện mới. 5.12 Theo mẫu module
catalog (hàm đọc nhận sellerID), mẫu SQL động `*_dynsql.go` + test trên PostgreSQL thật.
6.1 Đồng thời: chỉ đọc + chèn ghi chú (bảng chỉ ghi thêm, không khóa dòng). Phân trang keyset ổn định khi có đơn mới
chèn (test). 6.2 Gửi lại ghi chú: Idempotency-Key của M0. 6.3 Khóa: không khóa dòng. 6.4 RetryTx: hàm trong WriteTx gán
lại kết quả mỗi lần. 6.5 READ COMMITTED. 6.6–6.8 không áp dụng: không việc nền, không gọi ngoài. 6.9 Truy vấn danh
sách dùng index (list_explain_test 200.000 đơn). 6.10 Tiền chỉ đọc int64. 6.11 Không quy luật ghi mới; quy luật lọc
nhãn = `orderview.Label` của dòng kiểm bằng test bảng mọi nhãn. 6.12 Dữ liệu cá nhân: `q` không vào log hay lỗi; lịch sử
chỉ trả tên trường (không giá trị before/after); ghi chú không vào audit (chỉ độ dài); SĐT ở dòng là `logging.Phone`.
6.13 Quyền: route khai `order.read`/`order.update`; test 403 khi thiếu `order.read`; tenant B 404.

### Lượt 2

5.2/5.3: identity chỉ đọc thêm một cột của bảng của nó; order vẫn không nhắc schema identity. 6.12 Dữ liệu cá nhân:
`listed_fields` chỉ là tên trường do bên ghi liệt kê (`EditField*`), không giá trị; test identity kiểm SĐT trong after
không lọt vào kết quả; DH06 kiểm không lộ địa chỉ/SĐT thật
sau `Writer.Edit`. 6.13: không route mới. Migration 0211: pha mở rộng, chỉ đổi số.

## Quyết định thay chủ shop

1. **Audit vào lịch sử đơn qua `AuditSource`:** `order` không được nhập `identity` (04 mục 3) và không được nhắc schema
   `identity` (luật migrations 4). Chọn: thêm `identity.ListEntityAudit` (chỉ tên trường, không giá trị), lớp HTTP nối
   vào `order.Timeline(ctx, tx, sellerID, id, src AuditSource)`. Khác kế hoạch: Timeline nhận thêm `sellerID` (ADR-29:
   mọi hàm đọc nhận người bán) và `src`. Phương án khác: handler tự gộp hai nguồn (logic gộp nằm ở lớp HTTP — trái luật
   lớp HTTP mỏng). Đảo ngược dễ.
2. **(Lượt 1, lượt 2 thay bằng Quyết định 8) Mục "sửa" lấy từ audit có `before`** (bản ghi so trước/sau thật, `changed_fields` đúng), bỏ trường trạng thái đã có
   ở lịch sử trạng thái (`status`, `pendingReason`, `version`, …); audit chỉ có `after` (tạo, xác nhận, đặt chờ) đã có
   dòng lịch sử trạng thái nên không lặp. Việc cho Task 77: audit `order.update` ghi cả Before và After.
3. **Nhãn của vận đơn** (`awaiting_pickup`, `shipping`, `delivery_failed`, `returning`) gửi lọc ở M2 → 422 mã có sẵn
   `invalid_input` (sentinel `order.ErrLabelNotFilterable`), không thêm mã lỗi mới vào hợp đồng và errors.ts. Nhãn
   `confirmed/packing/packed` dịch như `orderview.Label` khi chưa có trục vận đơn (đúng nhãn hiển thị ở M2).
4. **Index mới `orders_seller_created`** (migration 0211, chỉ thêm): danh sách mặc định (mọi trạng thái, mới nhất trước)
   không có index theo `created_at` cho mọi trạng thái nên là Seq Scan + sắp xếp trên 200.000 đơn. Index thường (không
   CONCURRENTLY): CRM chưa chạy thật, bảng nhỏ; số 0210 trùng với Task 77 khi gộp, lượt 2 đổi thành 0211.
5. `q` chỉ chữ số dưới 4 số (không phải SĐT đủ) → 422 `query_too_short` (dùng `customer.ErrQueryTooShort`, như tìm
   khách); `q` là tên → tối đa 100 nhóm khách khớp đầu (giới hạn của `customer.Search`).
6. `itemCount` = tổng số lượng các dòng; `customerId`, `customerName` của dòng là của khách gốc (hợp đồng: "Gốc của
   khách"). `createdTo` không gồm mốc (nửa khoảng `[from, to)`).
7. Lịch sử đơn trả mới nhất trước (như dòng thời gian khách), không phân trang (đơn có ít mục; hợp đồng `OrderTimeline`
   không có nextCursor).

8. **(Lượt 2) Task 77 không ghi `before` cho `order.update`**, chỉ `after` kèm `after.fields` (tên trường đổi). Chọn: phía đọc
   (của task này) hiểu `after.fields` qua `identity.ListEntityAudit.ListedFields`, không sửa handler của 77. Lý do: tên
   trường của 77 chính xác hơn (`shipAddress`, `lines` không có trong `AuditFields`, so before/after sẽ chỉ ra `subtotal`,
   `grandTotal`…); sửa 77 ngoài phạm vi. Đường so before/after của lượt 1 không còn bên ghi nào dùng → xóa (luật "chuyển
   sang cách mới thì xóa cách cũ"). Quy ước: bên ghi muốn có mục sửa trong lịch sử đơn thì ghi `after.fields`. Phương án
   khác: 77 ghi `Before` đủ khóa. Đảo ngược dễ.
11. **(Lượt 2) Người phụ trách ở đơn trùng**: `order.Summary.CareBy` + tên qua `actorNamesOf` ở cả ba operation đơn trùng
   (Task 80 để trống chờ task này, ghi chú 7 của task-80). Không đổi hợp đồng.
9. **(Lượt 2) Đơn hủy qua xử lý đơn trùng** (Task 80 chỉ ghi audit `order.duplicate.resolve`): không thêm audit
   `order.cancel` — lịch sử trạng thái đã ghi người hủy, lý do `duplicate`, nên lịch sử đơn có mục hủy. Audit chỉ `after`
   không có `fields` → không thành mục sửa.
10. **(Lượt 2) Lý do hủy trong chi tiết đơn**: `GetOrder` đã trả `cancelReason`, `cancelNote` từ lượt 1 (cột của 78 có từ
   0204); lượt 2 thêm test qua đường hủy thật.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (Lượt 1, thay bằng dòng lượt 2) DH06: tạo → sửa địa chỉ + số lượng → xác nhận → hủy → ghi chú: 4 mục thao tác + 1 ghi chú, đúng người, mới nhất trước; mục sửa ghi `shipAddress`, `lines` = `changed`, không địa chỉ/SĐT; audit chỉ có after và audit chỉ đổi trạng thái không thành mục | `order_test.TestTimeline_DH06` (`checkTimeline`) | `go test -race -count=1 -run 'TestGetDetail\|TestTimeline\|TestAddNote' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 4.078s` |
| Ghi chú: thân rỗng/trắng/quá 2.000 ký tự → ErrInvalidInput; người bán khác, đơn không có → ErrNotFound; chỉ ghi thêm | `order_test.TestAddNote_Validation` | như trên | ok |
| Lọc: nhãn `awaiting_discount_approval`, `confirmed`; kênh manual; nguồn; khách gốc A gồm đơn của B đã gộp (lọc bằng A và bằng B); người phụ trách; trạng thái; lý do chờ; ngày `[from,to)`; nghi trùng; `q` mã (cả chữ thường) → 1 đơn; `q=5678` → đơn đuôi 5678; `q` SĐT đủ; `q=hong nhung` → đơn của nhóm; `labels=shipping` → ErrLabelNotFilterable; `q=56` → ErrQueryTooShort; nhãn lạ → ErrInvalidInput; dòng mang khách gốc, tổng số lượng, SĐT chuẩn; người bán khác 0 đơn | `order_test.TestList_Filters` | `go test -race -count=1 -run 'TestList_\|TestGetDetail' ./internal/modules/order/` | ok |
| Lọc nhãn khớp `orderview.Label` của dòng; mỗi đơn đúng một nhãn; hai nhãn là "hoặc" | `order_test.TestList_LabelMatchesRowLabel` | như trên | ok |
| Phân trang: 250 đơn, limit 100 → 100/100/50, đúng thứ tự (created_at, id) giảm, không trùng không sót; chèn 5 đơn mới giữa trang 1 và 2 → trang 2, 3 vẫn đúng; con trỏ hỏng → ErrInvalidInput | `order_test.TestList_KeysetPagination` | như trên | ok |
| list_explain (200.000 đơn, generate_series): mặc định, trang sau, lọc trạng thái, nhãn, khách, đuôi SĐT, SĐT đủ, mã → không Seq Scan trên `ord.orders`; tạm bỏ migration 0211 thì 2 ca mặc định Seq Scan (đã thử rồi trả lại) | `order_test.TestListPlans` | `go test -race -count=1 -run TestListPlans -v ./internal/modules/order/` | 8/8 PASS; khi tạm bỏ 0211: `kế hoạch Seq Scan trên ord.orders (bảng Seq Scan: [orders order_items])` |
| Chi tiết: đơn, nhãn, EditScope, khách gốc của hồ sơ đã gộp, mã SALE10, cảnh báo `possible_duplicate` chỉ cặp chưa xử lý, phiếu trả; đơn hủy → EditScope chỉ CareBy; người bán khác / không có → ErrNotFound | `order_test.TestGetDetail` | như trên | ok |
| (Lượt 1, `HasBefore` đã bỏ ở lượt 2 — xem dòng lượt 2) `identity.ListEntityAudit`: đúng thực thể, cũ trước, tên trường đổi, HasBefore, không mang giá trị before/after, không lẫn tenant khác | `identity_test.TestListEntityAudit` | `go test -race -count=1 -run TestListEntityAudit ./internal/modules/identity/` | `ok ... 1.853s` |
| HTTP listOrders: lọc nhãn, kênh, q, nextCursor 2 trang; `label=shipping` → 422 invalid_input; `q=56` → 422 query_too_short; con trỏ hỏng → 400; thiếu order.read → 403 | `httpapi.TestOrderRead_List` | `go test -count=1 -run 'TestOrderRead_' -v ./internal/httpapi/` | `--- PASS: TestOrderRead_List (2.42s)` |
| HTTP getOrder chi tiết: customer, createdBy (tên), editScope, warnings, returns; thiếu order.read → 403 | `httpapi.TestOrderRead_Detail` | như trên | `--- PASS (1.92s)` |
| HTTP timeline + ghi chú: 3 mục có tên người làm; audit `order.add_note` không chép nội dung, không thành mục sửa; thân trắng → 400; chỉ xem → 403; đơn không có → 404; thiếu order.read → 403 | `httpapi.TestOrderRead_TimelineNotes` | như trên | `--- PASS (2.19s)` |
| PQ08: đơn tenant B → 404 ở getOrder, timeline, ghi chú; danh sách A không có đơn B | `httpapi.TestOrderRead_TenantIsolation` | như trên | `--- PASS (1.15s)` |
| Không vỡ đường cũ (createOrder, confirm, route M2, mã lỗi khai trong hợp đồng) | `TestOrderAPI_*`, `TestOrderConfirm*`, `TestM2*`, `TestErrorCodes*` | `go test -race -count=1 -run 'TestOrderRead_\|TestOrderAPI_\|TestOrderConfirm\|TestM2\|TestErrorCodes' ./internal/httpapi/` | `ok ... 24.617s` |
| Kiến trúc, lint | `tests/arch` | `go test -count=1 ./tests/arch/...`; `golangci-lint run ./internal/... ./tests/...` | `ok ... 9.981s`; `0 issues.` |
| Cổng kiểm của task | — | `ci-task.sh` (lần 1 trước khi stage; lần 2 sau khi stage và sửa jscpd) | Lần 1: mọi test qua; đỏ `gen-check` (mã sinh chưa stage) và `jscpd` (6 chỗ trùng, đã xử lý, xem Tái dùng). Lần 2: build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard qua; test toàn bộ (đổi migration): `ok` order 104.7s, identity 129.0s; đỏ 2 test chập chờn ngoài phạm vi: `TestLoggerRedactsStructAndTypes` (dấu giờ `40.980911` chứa "0911"), `TestCustomerWriteAPI` (`handlers_customer_write_test.go:85`, mã ngẫu nhiên trong thân lỗi); chạy lại `-count=3` cả hai `ok` (3.7s, 1.2s) |
| (Lượt 2) DH06 với hàm thật: `Writer.Edit` (địa chỉ + số lượng) → `Confirm` → `Writer.Cancel` (lý do, ghi chú) → ghi chú; audit đúng dạng handler 77/78: 4 mục thao tác + 1 ghi chú, đúng người, mới nhất trước; mục sửa đúng `lines`, `shipAddress` = changed, `action`; audit hủy (có `hasNote`) không thành mục sửa; không lộ địa chỉ/SĐT cũ, mới | `order_test.TestTimeline_DH06` | `go test -race -count=1 -run 'TestTimeline\|TestGetDetail\|TestAddNote\|TestList_' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 6.495s` |
| (Lượt 2) `ListEntityAudit.ListedFields`: mảng `after.fields` → đúng tên; `fields` không phải mảng hay không có → rỗng (không nil); SĐT trong after không lọt ra | `identity_test.TestListEntityAudit` | `go test -race -count=1 -run TestListEntityAudit ./internal/modules/identity/` | `ok ... 2.625s` (trước khi sửa mã: không biên dịch, `c.ListedFields undefined`) |
| (Lượt 2) DH06 qua HTTP: POST tạo → PATCH địa chỉ + số lượng (77) → POST cancel (78) → timeline 3 mục (hủy lý do `wrong_info`, sửa đúng 3 khóa summary, tạo) đúng tên người; không lộ SĐT, địa chỉ, ghi chú hủy; getOrder trả `cancelReason`, `cancelNote`, `cancelledAt` | `httpapi.TestOrderRead_EditCancelTimeline` (`checkTimelineItems`) | `go test -count=1 -run 'TestOrderRead_' ./internal/httpapi/` | `ok ... 4.056s` |
| (Lượt 2) Đơn trùng có người phụ trách: check, list, resolve trả `careBy {id, name}` của đơn có người phụ trách, đơn kia không có | `httpapi.TestOrderAPI_Duplicates` (`checkCareBy`) | `go test -count=1 -run TestOrderAPI_Duplicates ./internal/httpapi/` | trước khi sửa: FAIL 3 chỗ `careBy ... = <nil>`; sau: `ok ... 1.188s` |
| (Lượt 2) Cổng kiểm của task trên bản gộp | — | `ci-task.sh` (một lần, nền) | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check qua; test toàn bộ (đổi migration): mọi gói `ok` (order 519s, identity 166s, httpapi…) trừ `internal/testkit` `TestCleanupStaleDropsOnlyIdleOldDatabases` (`khóa dọn database thử: timeout` — chờ advisory lock trên PostgreSQL chung với phiên khác; task không chạm testkit); hook Stop gặp đúng test đó; chạy riêng: `ok elimaz.com.vn/crm/internal/testkit 2.833s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `Timeline(ctx, tx, sellerID, id, src)` thay vì `Timeline(ctx, tx, id)`; `AddNote(ctx, tx, sellerID, id, body, a, now)`
  thêm `sellerID` (ADR-29). Xem Quyết định 1.
- DH06 (lượt 1): sửa, hủy chưa có trên main nên dựng giả; lượt 2 thay bằng `Writer.Edit`, `Writer.Cancel` thật.
- Thêm migration `0211_order_list_index.sql` (kế hoạch không ghi), xem Quyết định 4.
- `ListFilter.Statuses` là `[]string` (kế hoạch ghi `[]state.Status`: gói `state` nằm trong `internal/` của order, lớp
  HTTP không nhập được); hằng `order.Status*` là giá trị.
- `list_explain_test` kiểm "không Seq Scan trên `ord.orders`" bằng tên bảng trong kế hoạch JSON (hàm `seqScans` của
  test): `ord.order_items` của dữ liệu mẫu rỗng nên Seq Scan bảng rỗng là đúng; `testkit.ExplainAsApp` chỉ trả loại nút.
- Chỉ số khách (`stats`) đọc nguyên từ `customer.Get`; số liệu thật tính ở Task M2-25, test không đòi số > 0.

## Việc phát hiện thêm

- Task 80 (đơn trùng): hằng `order.WarningPossibleDuplicate = "possible_duplicate"` đã khai ở `read.go`; dùng lại, không
  khai lần hai.
- Test chập chờn ngoài phạm vi (cho điều phối viên): `internal/platform/logging` `TestLoggerRedactsStructAndTypes` tìm
  "0911" trong cả dòng log nên trúng dấu giờ (ví dụ `…40.980911+07:00`); `internal/httpapi` `TestCustomerWriteAPI`
  (`handlers_customer_write_test.go:85`, kiểm thân `identity_taken`) trúng mẩu số trong `ownerId`/`request_id` ngẫu nhiên.
  Cùng loại lỗi với Task 421; không sửa ở task này.
- Màn hình đơn (console) chưa dùng `listOrders`, `getOrderTimeline`, `addOrderNote`.
- (Lượt 2) Task 77 ghi audit `order.update` chỉ có `after` (kèm `after.fields`), không có `before`: lịch sử đơn đọc
  `after.fields` (Quyết định 8). Không sửa 77.
- (Lượt 2) Test chập chờn ngoài phạm vi: `internal/testkit` `TestCleanupStaleDropsOnlyIdleOldDatabases` hết giờ 60s chờ
  advisory lock khi nhiều phiên dùng chung PostgreSQL 5600 (đỏ cả ở ci-task và hook Stop, chạy riêng qua).
- (Lượt 2) `ListEntityAudit` lấy 500 dòng cũ nhất: đơn có > 500 dòng audit mất mục mới nhất (ghi chú reviewer lượt 1,
  chưa sửa; khó xảy ra).

## Reviewer

Commit 1 (cấu trúc, `handlers_customer_timeline.go`): **ĐẠT**. Không có phát hiện phải sửa; hành vi AddCustomerNote,
GetCustomerTimeline giữ nguyên (201, refusal, tên người viết, Note chỉ ở kind note, summary rỗng không gửi). Ghi chú:
lượt review commit tính năng cần kiểm handler đơn dùng đúng các helper này, không giữ bản chép riêng.

Commit 2 (tính năng): lượt 1 **CẦN SỬA** — mục Ảnh hưởng ghi sai tên câu sqlc (`OrderOfSeller`, `GetOrderCustomer`
không có thật); đã sửa thành `OrderExists`, `ListOrderDuplicateLinks`, `ListOrderReturns`, khách qua `customer.Get`.
Lượt 2: **ĐẠT** — không phát hiện phải sửa; handler đơn dùng đúng helper của commit 1; quyết định thay chủ shop không
trái spec. Ghi chú không bắt buộc (không sửa): `ListEntityAudit` lấy 500 dòng cũ nhất (đơn > 500 dòng audit mất lần
sửa mới nhất, khó xảy ra); `customer.Get` đọc dư danh tính, địa chỉ; "chỉ ghi thêm" của ghi chú do trigger 0204 bảo
đảm, không do `TestAddNote_Validation`; số migration 0210 cần điều phối viên kiểm khi gộp.

Kết luận: ĐẠT

Lượt 2 (gộp main, bản gộp đã stage): **ĐẠT** — không phát hiện phải sửa. Đã kiểm: đổi số 0211 không còn trùng; mọi chỗ
dùng `AuditRecord`/`AuditChange`/`orderListItemOut` đã đổi, không còn `HasBefore`, `ActiveCouponCode`; đường so
before/after cũ đã xóa (không bọc điều kiện); chỉ `UpdateOrder` ghi `after.fields`; mã sinh khớp nguồn; bốn dòng bằng
chứng lượt 2 kiểm đúng điều ghi. Ghi chú không bắt buộc: đánh dấu hai dòng bằng chứng lượt 1 đã được thay (đã làm);
`jsonb_array_elements_text` trả nguyên phần tử không phải chuỗi nếu sau này có bên ghi đặt object vào `after.fields`
(hiện chỉ `[]string`, không sửa); giới hạn 500 dòng audit cũ nhất vẫn còn (Việc phát hiện thêm).

Kết luận: ĐẠT
