# Task 262 — Phiên kiểm kho mở sổ

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 262 = M7-17). Loại việc (quy trình mục 3): A (vùng rủi ro cao `internal/modules/inventory`)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm mới `inventory.CreateOpening`, `Writer.PostOpening`, `CheckOpening`, `GetOpening`, `ListOpenings`, kiểu `OpeningStatus`, `PostResult`, `CheckResult`, `OpeningMismatch`, `UncountedLinesError`, sentinel `ErrOpeningExists`, `ErrWarehouseNotEmpty`, `ErrUncountedLines`, `ErrOpeningNotPosted`, hằng `StocktakeKindRegular/Opening`. Đổi: struct `inventory.Stocktake` thêm trường `Kind` (chỉ thêm; 2 chỗ dựng `inventory.Stocktake{}` rỗng trong httpapi không đổi); hành vi `Writer.ApplyStocktake` (3 chỗ gọi: `handlers_stocktake.go:96`, `stocktake_test.go:85`, `order/shipment_progress_test.go:413`) — từ chối phiên `opening` (`ErrInvalidInput`), phiên thường không đổi. Lớp HTTP: thân 5 handler khung trong `handlers_inventory_opening.go`; `errorBody` thêm `Count` (`count`, đã khai ở `Error` của openapi từ Task 260); `errors_inventory.go` thêm 4 dòng.
- Gói phụ thuộc ngược (go list): nhập `internal/modules/inventory`: `cmd/crm`, `internal/httpapi`, `internal/modules/order`, `internal/modules/promotion`, `internal/testkit/shiptest`, `internal/testkit/shoptest`, `internal/wiring/webcache`, `tools/loadkit`, `invtest`. Chỉ thêm ký hiệu, nên chỉ build lại; httpapi là chỗ dùng mới.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng (Task 260 đã khai `createOpeningStocktake`, `listOpeningStocktakes`, `getOpeningStocktake`, `postOpeningStocktake`, `checkOpeningStocktake`; mã lỗi `opening_exists`, `warehouse_not_empty`, `uncounted_lines` và trường `count` đã có). Bỏ 5 dòng của mình khỏi `tests/arch/testdata/m7_pending.txt`. oasdiff: không chênh.
- Chỗ dùng trên màn hình: chưa có (màn 10 là Task 273); `console/src/api/**` không đổi vì openapi không đổi.
- Câu sqlc, bảng, pha migration: `migrations/0704_inventory_opening.sql` pha mở rộng: `ALTER TABLE inventory.stocktakes ADD COLUMN kind` (NOT NULL DEFAULT 'regular', CHECK), `GRANT UPDATE (kind)`, index duy nhất một phần `stocktakes_one_opening (tenant_id, warehouse_id) WHERE kind = 'opening' AND status <> 'cancelled'`, view `inventory.opening_status_v` (security_invoker, GRANT SELECT crm_app). Câu mới trong `queries/inventory/opening.sql` (khóa kho, kiểm kho trống, đặt kind, đếm biến thể phải đếm, dòng chưa đếm, trạng thái từ view, so sổ). Sửa `queries/inventory/stocktakes.sql`: `LockStocktake`, `GetStocktake` trả thêm `kind`; `ApplyStocktake` thêm điều kiện `kind = @kind`. Bảng chạm: `inventory.stocktakes`, `stocktake_lines`, `stock_balances`, `stock_ledger` (qua `applyDelta` của M1), `stock_reservations` (đọc), `warehouses` (khóa), `catalog.variants_v`, `catalog.products_v` (đọc).
- Tệp khác đổi theo: `docs/views.md` (mô tả `opening_status_v` cho khớp: "chưa xóa, mọi người bán, tính lúc đọc"); `internal/**/internal/store/models.go` của 16 gói do sqlc sinh lại (kiểu `InventoryOpeningStatusV`, cột `Kind`), không sửa tay.
- Test sẽ chạy: mới `internal/modules/inventory/opening_test.go` (`TestOpening_*`), `opening_property_test.go` (`TestOpeningProperty`), `internal/httpapi/handlers_inventory_opening_test.go` (`TestOpeningAPI_*`); chạy lại test kiểm kho M1 `TestStocktake_*` (KH05, KH13), `TestStocktakeAPI_*`, `TestM7Contract_*` (httpapi), `TestM7Contract_ViewsDoc`, `TestViewsDocMatchesDatabase` (tests/arch, trong ci-task). Cả gói inventory, httpapi trong ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (inventory: `StartStocktake`, `RecordCount`, `Writer.ApplyStocktake`, `GetStocktake`, `CancelStocktake`, `Availability`; httpapi: `inventoryWrite`, `inventoryRead`, `canAt`, `stocktakeAt`, `audit`); grep "opening", "empty", "warehouse_not_empty".
- Dùng lại / viết mới vì: dùng lại `StartStocktake` (mở phiên, mã KK-, kiểm kho đóng, unique một phiên mở), `RecordCount` (đếm từng dòng — không viết đường đếm thứ hai), phần áp dụng của `ApplyStocktake` (`applyLines`/`applyDelta`: sổ `doc_type stocktake`, `doc_id` = id phiên, `stock.changed`) — tách thân thành `w.apply(…, kind)` để `PostOpening` gọi đúng cùng đường ghi sổ; câu `EnsureBalance` có sẵn để chèn dòng số dư 0. Viết mới chỉ phần riêng của mở sổ: kiểm kho trống, đếm dòng phải đếm, so sổ (`CheckOpening`) — M1 không có. Lớp HTTP dùng `inventoryWrite`/`inventoryRead`/`canAt`/`h.audit` như kiểm kho M1.

## Soát thiết kế

- 5.1–5.3: dữ liệu thuộc inventory (cột `kind`, view `opening_status_v` trong schema inventory); không ghi bảng module khác; đọc catalog qua `catalog.variants_v`, `catalog.products_v`.
- 5.4: tạo phiên = một transaction (khóa kho, kiểm trống, `StartStocktake`, đặt kind); đăng = một transaction (khóa phiên, kiểm, chèn số dư, áp dụng, kiểm lại, audit).
- 5.5: `stock.changed` do đường áp dụng của M1 phát như cũ (Writer.Events/Realtime), không gọi gì ra ngoài.
- 5.6: không áp dụng: không gọi dịch vụ ngoài.
- 5.7: mọi câu chạy trong `tenancy.WithTenant` (qua `inventoryWrite`/`inventoryRead`, test dùng `e.app`); cột mới trên bảng đã có RLS; index ghép `tenant_id`.
- 5.8: module không nhập net/http; lỗi HTTP ở `errors_inventory.go`, thân `count` ở `errors.go` (`detailedError`).
- 5.9: `createOpeningStocktake`, `postOpeningStocktake` có `IdempotencyKey` required và `x-permission` (Task 260).
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12: theo module mẫu inventory (Task 28) và kiểm kho M1.
- 6.1: hai lần tạo cùng kho: khóa dòng kho `FOR NO KEY UPDATE` (nhóm 11 đứng đầu) rồi kiểm phiên mở sổ đã có → `ErrOpeningExists`; lớp sau: index một phần `stocktakes_one_opening` (vi phạm → `ErrOpeningExists`). Hai lần đăng khác khóa: `LockStocktake FOR UPDATE` + `ApplyStocktake … WHERE status='open'` → bên sau `ErrStocktakeNotOpen`.
- 6.2: cùng khóa → middleware chống trùng (50 yêu cầu song song, test).
- 6.3: thứ tự khóa ADR-46 nhóm 11: `warehouses` (chỉ lúc tạo) → `stocktakes` → `stocktake_lines` (trigger FOR SHARE) → `stock_balances` theo variant_id tăng dần (EnsureBalance, applyDelta theo `listLines` ORDER BY variant_id). Phiếu nhập ghi sổ chen giữa: kiểm trống trước khi áp dụng **và kiểm lại sau khi áp dụng** (khi đã giữ khóa số dư của các biến thể đếm; phiếu đã commit trước đó bị thấy → lùi, phiếu commit sau phải chờ khóa số dư nên đứng sau mở sổ).
- 6.4: lỗi tạm do `db.TxAPI`/RetryTx của lớp HTTP chạy lại cả transaction; hàm gán lại mọi biến kết quả trong thân.
- 6.5: READ COMMITTED + khóa dòng; câu kiểm trống đọc bản mới nhất mỗi câu.
- 6.6–6.8: không áp dụng: không việc nền, không gọi ngoài; transaction tự lùi khi lỗi.
- 6.9: kiểm trống dùng EXISTS; sổ của kho trống không có dòng nên nhanh; so sổ dùng `stock_ledger_step_key` (tenant, doc_type, doc_id, …). Không thêm index cho `stock_ledger (warehouse_id)` (shop nhỏ, câu chỉ chạy khi mở/đăng/kiểm phiên mở sổ).
- 6.10: không áp dụng: không tiền.
- 6.11: rapid `TestOpeningProperty` (100 lượt): 1–40 biến thể, số đếm 0–500 → on_hand = counted, Σ sổ = Σ đếm, `CheckOpening` ok.
- 6.12: audit chỉ có mã phiên, kho, số dòng, số đơn vị; không dữ liệu cá nhân.
- 6.13: quyền `inventory.stocktake.opening` theo kho, kiểm bằng `canAt` ở kho của yêu cầu (tạo) hoặc kho của phiên (đăng, kiểm); đọc bằng `inventory.read` theo kho; danh sách lọc theo kho được giao; tenant khác 404. Đường áp dụng M1 (`inventory.stocktake.apply`) không đăng được phiên mở sổ (tránh bỏ qua kiểm đủ dòng, kho trống).

## Quyết định thay chủ shop

1. **Biến thể phải đếm** = biến thể `active` của sản phẩm `draft`/`active` **chưa xóa** (`deleted_at IS NULL`), mọi người bán của tenant (một kho chứa hàng của mọi người bán, dòng đếm mang `seller_id`). Vì sao: sản phẩm đã xóa không bán, không phải đếm. Phương án khác: tính cả sản phẩm đã xóa (bắt đếm hàng không còn trong danh mục).
2. **`lines_expected` của view tính lúc đọc** (không chụp lúc đăng): phiên đang mở thấy ngay biến thể mới thêm; phiên đã đăng có thể lệch nếu sau đó thêm biến thể. `PostResult.LinesExpected` là số lúc đăng. Phương án khác: thêm cột chụp số lúc đăng (cần thêm cột, để sau nếu màn cần).
3. **Đường áp dụng M1 (`applyStocktake`) từ chối phiên mở sổ** với 400 `invalid_input` (không thêm mã lỗi mới): phiên mở sổ chỉ đăng qua `postOpeningStocktake` (kiểm đủ dòng, kho trống). Phương án khác: mã mới `use_opening_post` (thêm mã, câu tiếng Việt ở console).
4. **Kiểm lại phiên chưa đăng** (`checkOpeningStocktake` trên phiên `open`/`cancelled`) → 409 `stocktake_open` (mã có sẵn: "phiên kiểm đang mở / chưa đăng"), không trả `ok:false` giả. Phương án khác: mã mới.
5. **Kiểm kho trống làm hai lần khi đăng**: trước và sau bước áp dụng (lần sau bỏ qua sổ của chính phiên), để phiếu nhập commit chen giữa cũng bị bắt. Kế hoạch chỉ ghi một lần (bước 3).
6. Audit `inventory.opening_applied` (và `inventory.opening.start` lúc mở, theo cách đặt tên `inventory.stocktake.start` của M1) ghi ở handler bằng `h.audit` trong cùng transaction, như kiểm kho M1, thay vì gọi `RecordAudit` trong module.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mở trên kho trống: 12 dòng phải đếm (3 mẫu × 4 biến thể active, 1 discontinued không tính), 0 dòng; mở lần hai → ErrOpeningExists | `TestOpening_CreateCountPostCheck` | `go test -race -count=1 -run '^(TestOpening_.*\|TestOpeningProperty\|TestStocktake_.*)$' ./internal/modules/inventory/` | `--- PASS: TestOpening_CreateCountPostCheck (0.56s)`; `ok … inventory 29.640s` |
| Thiếu 1 dòng → UncountedLinesError{1}; đủ dòng (dòng cuối đếm 0) → đăng: linesExpected 12, countedLines 12, units = Σ; on_hand = counted, có thể bán (stock_available_v, KH04) = counted, số dòng sổ = số dòng counted > 0, Σ delta = units | `TestOpening_CreateCountPostCheck` | như trên | PASS |
| CheckOpening ok, mismatches rỗng; chèn tay một dòng sổ (owner) → ok:false, đúng biến thể, SKU | `TestOpening_CreateCountPostCheck` | như trên | PASS |
| Sau đăng: đếm → ErrStocktakeNotOpen, đăng lại → ErrStocktakeNotOpen, mở sổ thứ hai → ErrOpeningExists; phiên kiểm thường sau mở sổ chụp on_hand − in_transit (KH05) | `TestOpening_CreateCountPostCheck` | như trên | PASS |
| Kho có phiếu nhập → ErrWarehouseNotEmpty; kho có phiên thường mở → ErrStocktakeOpen; kho lạ → ErrNotFound; hủy phiên mở sổ rồi mở lại được | `TestOpening_CreateRefused` | như trên | `--- PASS: TestOpening_CreateRefused (0.52s)` |
| Phiếu nhập ghi sổ sau khi mở, trước khi đăng (tx khác) → đăng bị từ chối warehouse_not_empty, 0 dòng sổ của phiên, phiên còn mở; kiểm phiên chưa đăng → ErrOpeningNotPosted | `TestOpening_ImportBeforePost` | như trên | `--- PASS: TestOpening_ImportBeforePost (0.63s)` |
| Dòng đếm chụp expected_on_shelf ≠ 0 → ErrWarehouseNotEmpty | `TestOpening_LineExpectedNotZero` | như trên | `--- PASS: TestOpening_LineExpectedNotZero (0.51s)` |
| ApplyStocktake (M1) không đăng phiên mở sổ; PostOpening/CheckOpening không nhận phiên thường | `TestOpening_RoutesSeparated` | như trên | `--- PASS: TestOpening_RoutesSeparated (0.93s)` |
| Hai lần mở song song → một phiên; hai lần đăng song song → một lần ghi sổ, bên kia ErrStocktakeNotOpen | `TestOpening_Concurrent` | như trên | `--- PASS: TestOpening_Concurrent (0.98s)` |
| Thuộc tính rapid 100 lượt: 1–40 biến thể, đếm 0–500 → on_hand = counted, Σ sổ = Σ đếm, CheckOpening ok | `TestOpeningProperty` | như trên | `--- PASS: TestOpeningProperty (20.69s)` |
| Kiểm kho M1 không đổi (KH05, KH13) | `TestStocktake_*` (13 test) | như trên | 13 × `--- PASS` |
| API: tạo 201 linesExpected 3; lần hai 409 opening_exists; quyền W1 → 403 ở W2; thiếu dòng 422 `{"error":"uncounted_lines","count":1}`; 50 post cùng khóa + 2 khóa khác song song → đúng một khóa 200, sổ 2 dòng (không nhân đôi), còn lại 409 stocktake_not_open/request_in_progress; đọc, danh sách theo kho được giao, check ok; check phiên chưa đăng 409 stocktake_open; đếm sau đăng 409 stocktake_not_open; tenant khác 404; audit inventory.opening.start, inventory.opening_applied | `TestOpeningAPI_Flow` | `go test -race -count=1 -run '^(TestOpeningAPI_.*\|TestStocktakeAPI_.*\|TestM7Contract_.*)$' ./internal/httpapi/` | `--- PASS: TestOpeningAPI_Flow (2.65s)`; `ok … httpapi 3.571s` |
| API: kho có hàng 409 warehouse_not_empty; applyStocktake trên phiên mở sổ 400 invalid_input, tồn không đổi | `TestOpeningAPI_Refused` | như trên | `--- PASS: TestOpeningAPI_Refused (2.31s)` |
| Cổng tĩnh (lint, gen, oasdiff, jscpd, test kiến trúc, gitleaks) + test gói đổi và phụ thuộc (-race cho httpapi, inventory) | ci-task | `scripts/agent/ci-task.sh` (từ xa, github run 46128672853) | `static: success`, `test (1..6): success`, `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 020b5403…)` |
| Hợp đồng M7 và kiểm kho M1 qua API không đổi | `TestM7Contract_StatusCodes`, `TestStocktakeAPI_*` | như trên | PASS |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký: `CreateOpening(ctx, tx, warehouseID, a)` trả `(Stocktake, linesExpected int, error)` (cả phiên thay vì chỉ id, handler cần mã, trạng thái); `PostOpening`, `CreateOpening` không nhận `now` (giờ ghi do `now()` của database như kiểm kho M1). `CheckOpening` không nhận `Actor`. `OpeningMismatch` (không phải `Mismatch`: trùng tên kiểu đã có ở `reconcile.go`), thêm trường `SKU` theo hợp đồng `OpeningMismatch` của Task 260.
- Bước 4–5 của kế hoạch: `PostOpening` gọi thân chung `w.apply(…, kind)` của `ApplyStocktake` (không gọi `ApplyStocktake` công khai, vì đường công khai giờ chỉ nhận phiên thường); audit ghi ở handler (`h.audit`), không gọi `RecordAudit` trong module.
- Lần đầu ci-task (lượt trước) đỏ vì lint (gci, gocognit ở test, nolint thừa); đã tách hàm trợ giúp test (`assertOpeningPosted`, `assertOpeningTamper`, `assertOpeningClosed`, `openingRun`, `onHandMap`, `postOpeningConcurrently`, `assertOpeningReads`), chạy lại xanh.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 262 không có e2e; màn 10 là Task 273)

## Phụ thuộc task để sau

(không có: Task 263 đếm cộng dồn và nhập Excel dùng phiên này; Task 269 đọc `opening_status_v`)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (hai lượt trên phần đã stage; lượt 2 sau khi bỏ phép kiểm `len(winners)` lặp và bổ sung mục Ảnh hưởng).

- Phát hiện phải sửa: không có.
- Ghi chú không bắt buộc, giữ nguyên: `ListOpenings` lấy tối đa 200 dòng rồi mới lọc quyền kho ở handler (shop nhỏ, khó chạm); gửi `version` cũ vào `applyStocktake` trên phiên mở sổ nhận `version_conflict` thay vì `invalid_input` (chấp nhận được); kế hoạch ghi giữ hàng "chưa released/consumed", mã dùng `state IN ('held','in_transit')` — tương đương theo CHECK của `0115_inventory.sql`.
- Đã sửa theo ghi chú: số tệp `models.go` sinh lại là 15 (không phải 16).

## Gộp vào main

Xung đột khi gộp `task-262` vào main (sau Task 264, 260/263, 265, 219, 524):
- `internal/httpapi/errors.go`: `errorBody` — giữ cả `ExistingID` (Task 264) và `Count` (Task 262).
- `internal/httpapi/errors_inventory.go`: giữ import `math`, const của cả hai bên; `inventoryDetail` kiểm `DuplicateReferenceError` (409 legacy_return_exists + existingId) rồi `UncountedLinesError` (422 uncounted_lines + count). Thứ tự `errorMapping` (tự gộp) giữ nguyên, `ErrInvalidInput` cuối.
- `tests/arch/testdata/m7_pending.txt`: bỏ 5 thao tác mở sổ (nhánh làm) và getCatalogLaunchReadiness/publishProducts/unpublishProducts (main đã làm). Còn 20 dòng, đều trả 501.
- `internal/modules/inventory/opening_test.go`: bỏ `secondWarehouse` của nhánh (trùng tên, giống hệt bản trong `env_test.go` của Task 264).
- 15 tệp `**/internal/store/models.go` (mã sinh): lấy bản main, `make gen` (thêm `InventoryOpeningStatusV`, cột `Kind`). Migration 0704 nằm trước 0706/0707 của main, không phụ thuộc lẫn nhau (runner cho thứ tự lệch).

Kiểm trên máy: `go build ./...`, `go vet ./internal/httpapi/ ./internal/modules/inventory/... ./cmd/crm/ ./tests/arch/` → qua.
Reviewer (phần giải xung đột): **ĐẠT**, không phát hiện phải sửa; ghi chú: xác nhận migration lệch thứ tự bằng test từ xa.
Test từ xa: `CRM_CI_POOL_SLOTS=vps=0 CRM_CI_LABEL=integrate-main-262 scripts/ci/merge-test.sh <tree f85eca53> HEAD` (gói: ./cmd/crm, ./internal/httpapi, ./internal/modules/inventory/..., ./tests/arch; -race: httpapi, inventory) → run 46158968816 trên github: static success, plan success, test (1)–(5) success, 40 phút máy, `MERGE-TEST XANH (github)`. Không có test chập chờn.
