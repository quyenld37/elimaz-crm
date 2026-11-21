# Task 113 — M3-22: Danh sách Giữ hàng quá hạn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 113 = M3-22). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Hàm không xuất `readableWarehouses` (httpapi) thêm tham số `perm`: 1 chỗ gọi (`ListStockDocuments`), cập nhật trong commit cấu trúc.
- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** ký hiệu mới, không đổi ký hiệu có sẵn (0 chỗ tham chiếu bị chạm). Mới ở `inventory` (tệp `aged.go`): `AgedReservations`, `AgedRow`, `AgedPage`, `AgedCursor`, `ParseAgedCursor`. Mới ở `internal/queries` (tệp `aged_dynsql.go`): `AgedRefs`, `AgedOrderRef`. Thân `(*handler).ListAgedReservations` (đang trả 501) thay bằng mã thật.
- Gói phụ thuộc ngược (go list): `inventory` ← cmd/crm, internal/httpapi, inventory/invtest, modules/order, testkit, testkit/shiptest; `internal/queries` ← internal/httpapi. Vùng rủi ro cao: `internal/modules/inventory` → chạy cả bộ test của inventory.
- Operation OpenAPI bị chạm, oasdiff: `listAgedReservations` (GET /api/v1/inventory/aged-reservations, `inventory.aged.read`) đã có trong `openapi.yaml` từ Task M3-04; không sửa hợp đồng → oasdiff không đổi. Xóa dòng `listAgedReservations` khỏi `tests/arch/testdata/m3_pending.txt`.
- Chỗ dùng trên màn hình: chưa có màn nào gọi (grep `console/src` ngoài `src/api` = 0); không sửa console.
- Câu sqlc, bảng, pha migration: câu mới `queries/inventory/aged.sql` (`AgedReservations`, chỉ SELECT trên `inventory.stock_reservations`). M1 chưa có chỉ mục `(tenant_id, state, state_changed_at)` (chỉ có `stock_reservations_balance_state (tenant_id, variant_id, warehouse_id, state)`) → tạo `migrations/0307_aged_index.sql`, `CREATE INDEX` thường (không CONCURRENTLY, xem Quyết định thay chủ shop), pha mở rộng (chỉ thêm). Mã đơn, SKU ghép trong `internal/queries` qua view công bố `ord.order_items_v`, `ord.orders_v`, `catalog.variants_v`. Không ghi bảng nào.
- Test sẽ chạy: `go test -race ./internal/modules/inventory/...` (cả bộ, vùng rủi ro cao), `./internal/queries/...`, `./internal/httpapi/ -run 'Aged|M3Routes'`, `./tests/arch/...`; rồi cổng kiểm `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (con trỏ trang: chỉ có con trỏ riêng từng module như `order.ReturnCursor`, `files.LostCursor`; không có gói con trỏ chung); `fulfillment.LoadSettings` + `Settings.AgedDays` (Task M3-04) cho ngưỡng 20/180; `identity.ScopesFor` cho phạm vi kho; `testkit.ExplainAsApp`, `testkit.OpenStaffSeeded` cho test kế hoạch câu; `invtest`, `orderAPIEnv` cho test handler.
- Dùng lại / viết mới vì: dùng lại `fulfillment.LoadSettings` (không đọc JSON cài đặt lần hai), `identity.ScopesFor`, `h.inventoryRead` (tenancy.WithTenant). Viết mới `AgedCursor` (khóa trang là `(state_changed_at, id)`, khác các con trỏ có sẵn chỉ theo id) theo mẫu `order.ReturnCursor` (base64url, hỏng → `ErrInvalidInput`). `readableWarehouses` trong `handlers_inventory.go` gắn cứng `inventory.read`; bản chép cho `inventory.aged.read` bị jscpd báo trùng → gộp: commit cấu trúc riêng (commit 1) cho `readableWarehouses(u, perm)` nhận quyền (1 chỗ gọi: `ListStockDocuments`, truyền `permInvRead`, hành vi không đổi), commit hành vi (commit 2) dùng lại nó. Cảnh báo jscpd thứ hai (vòng lặp đọc `items` giống `listCodes` trong test) xử lý bằng đọc JSON vào struct có kiểu.

## Soát thiết kế

- 5.1 Chủ dữ liệu: giữ hàng thuộc `inventory`; câu đọc ở `queries/inventory`. Không có dữ liệu mới (chỉ chỉ mục trên bảng của inventory).
- 5.2 Không ghi bảng nào (chỉ đọc; KH09 "không tự trả hàng", AT53 không tạo việc) — test đếm `stock_reservations`, `stock_ledger`, `jobs` trước/sau.
- 5.3 Mã đơn, SKU đọc qua view công bố trong gói `internal/queries` (inventory không được gọi order nên không đọc `ord.*` từ inventory).
- 5.4, 5.5, 5.6 không áp dụng: không ghi, không sự kiện, không gọi ngoài.
- 5.7 Mọi câu chạy trong `tenancy.WithTenant` (`h.inventoryRead`); bảng có sẵn RLS.
- 5.8 Module không nhập HTTP; handler mỏng: đọc cài đặt, gọi `inventory.AgedReservations`, `queries.AgedRefs`, ghép.
- 5.9 Đường đọc: không cần khóa chống trùng; quyền `inventory.aged.read` đã khai `x-permission`.
- 5.10, 5.11 không áp dụng (không Valkey, không thư viện mới).
- 5.12 Theo mẫu đường đọc (Task 7) và `ListStockDocuments` (lọc kho theo phạm vi).
- 6.1–6.8 không áp dụng: chỉ đọc, không khóa, không việc nền, không gọi ngoài.
- 6.9 Câu đọc có chỉ mục mới; test `ExplainAsApp` trên 100.000 dòng giữ hàng → không Seq Scan; trang ≤ 100 dòng.
- 6.10 không áp dụng (không tiền).
- 6.11 Quy luật "chỉ đọc" kiểm bằng đếm dòng sau 3 lần chạy; biên ngưỡng kiểm bằng ca biên đúng từng giây.
- 6.12 Không log; kết quả chỉ có mã, số lượng, không dữ liệu cá nhân.
- 6.13 Mặc định từ chối: không có grant `inventory.aged.read` → guard 403; grant theo kho → chỉ thấy dòng của kho được giao; gửi `warehouseId` ngoài phạm vi → 403.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi viết mã: test đỏ | cả tệp `aged_test.go` | `go vet ./internal/modules/inventory/` | `undefined: inventory.AgedPage` (FAIL biên dịch) |
| KH09/AT53 biên: held từ 01/12 00:00:01 +07 không có; đúng 20 ngày có (AgeDays 20); in_transit đúng 180 ngày có, 179 ngày không; dòng released cũ không có | `TestAgedReservations_Boundaries` | `go test -race ./internal/modules/inventory/ -run Aged -count=1` | `ok elimaz.com.vn/crm/internal/modules/inventory 5.800s` |
| Held = 25 → dòng 20 ngày biến mất (mức module) | `TestAgedReservations_Boundaries` (đoạn `Held = 25`) | như trên | ok |
| Held = 25 trong cài đặt vận chuyển (`platform.settings`) → dòng 21 ngày biến khỏi API | `TestAgedAPI_Settings` | `go test -race ./internal/httpapi/ -run 'TestAgedAPI\|TestM3Routes' -count=1` | `ok elimaz.com.vn/crm/internal/httpapi 7.757s` |
| Chạy 3 lần → số dòng held, in_transit, `stock_ledger`, `platform.jobs` không đổi | `TestAgedReservations_ReadOnly`, `TestAgedAPI_Scope` (snapshot trước/sau) | hai lệnh trên | ok |
| Quyền chỉ kho W2 → không thấy dòng kho chính; xin `warehouseId` kho chính → 403; không quyền → 403; toàn shop thấy cả hai | `TestAgedAPI_Scope`; mức module `TestAgedReservations_Warehouses` (rỗng không nil → 0 dòng) | như trên | ok |
| `ExplainAsApp` trên 100.000 dòng giữ hàng → không Seq Scan (mọi kho, theo kho, chỉ held, chỉ in_transit, trang sau) | `TestAgedReservations_Plan` | `go test ./internal/modules/inventory/ -run AgedReservations_Plan -count=1 -v` | 5 ca PASS, kế hoạch `[Limit Sort Bitmap Heap Scan BitmapOr Bitmap Index Scan Bitmap Index Scan]` |
| Phân trang (cũ nhất trước, hai dòng cùng mốc, không lặp, không sót, con trỏ qua chuỗi) | `TestAgedReservations_Pages`, `TestAgedAPI_Settings` (limit=1 + nextCursor) | như trên | ok |
| Đầu vào sai: ngưỡng ≤ 0, trạng thái lạ, con trỏ hỏng → ErrInvalidInput; API con trỏ hỏng → 400 `invalid_input` | `TestAgedReservations_InvalidInput`, `TestAgedAPI_Scope` | như trên | ok |
| Mã đơn, SKU ghép qua view công bố; tenant khác, mã lạ không có; rỗng không lỗi | `TestAgedRefs` | `go test -race ./internal/queries/ -run TestAgedRefs -count=1` | `ok elimaz.com.vn/crm/internal/queries 3.252s` |
| Operation không còn trả 501; cả bộ inventory (vùng rủi ro cao), queries, kiến trúc | `TestM3Routes_*`, cả gói | `go test -race ./internal/modules/inventory/... ./internal/queries/... ./tests/arch/... -count=1` | `ok …/inventory 93.147s`, `ok …/queries 47.331s`, `ok …/tests/arch 27.123s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Migration 0307 không dùng `CONCURRENTLY` (xem Quyết định thay chủ shop).
- Chữ ký `AgedReservations` trả thêm con trỏ trang sau: `([]AgedRow, *AgedCursor, error)` (kế hoạch ghi `([]AgedRow, error)`); `page Page` là `AgedPage{State, After, Limit}` — `State` mang tham số `state=` của API.
- `inventory.AgedRow` không có `OrderID`, `OrderCode`, `SKU`: inventory không được đọc view của `order` (test kiến trúc `sqlusage`, cột "Được gọi"); ba trường này ghép ở `internal/queries.AgedRefs` đúng như kế hoạch ghi "ghép trong gói queries", rồi handler trả đủ `AgedReservation` của hợp đồng.

## Quyết định thay chủ shop

- Danh sách theo kho, gồm mọi người bán của shop ở kho đó (không lọc theo người bán mặc định): quyền `inventory.aged.read` là quyền theo kho; giống `ListStockDocuments`. Khác: lọc người bán mặc định như `ListBalances` — dễ thêm sau.
- SKU lấy từ `catalog.variants_v` (SKU hiện tại, đúng kế hoạch), không lấy `sku_snapshot` của dòng đơn.
- Chỉ mục tạo bằng `CREATE INDEX` thường trong transaction, **không** `CONCURRENTLY` như kế hoạch ghi: bản `CONCURRENTLY` làm đỏ `TestMigrateConcurrentRunsApplyOnce` (CREATE INDEX CONCURRENTLY chờ mọi phiên đang mở, kể cả lần migrate thứ hai đang chờ khóa cố vấn của `db.Migrate` → `55P03` / `42P07`). CRM chưa chạy dữ liệu thật, bảng nhỏ (cùng lý do 0211). Phương án khác: sửa `db.Migrate` để chạy tệp NO TRANSACTION ngoài khóa — đổi hạ tầng migrate, ngoài phạm vi. Khi có dữ liệu thật, chỉ mục mới lớn nên tạo `CONCURRENTLY` bằng tay theo RUNBOOK. Không có phần Down (theo 0211).
- Thứ tự: cũ nhất trước (theo `state_changed_at`, rồi mã giữ hàng); con trỏ theo cặp đó.
- Dòng giữ hàng mất liên kết dòng đơn (chỉ xảy ra khi dòng đơn bị xóa, M2 chỉ xóa dòng đã nhả) vẫn hiện, `orderItemId`/`orderId` là uuid rỗng, `orderCode` rỗng — không giấu hàng đang bị giữ.

## Việc phát hiện thêm

(không có)

## Reviewer

Commit 1 (cấu trúc, `readableWarehouses(u, perm)`): **ĐẠT** — chỉ đổi cấu trúc, chỗ gọi duy nhất truyền `permInvRead`, không sót chỗ gọi (cả test). Ghi chú: sửa dòng migration ở mục Ảnh hưởng cho khớp (đã sửa).
