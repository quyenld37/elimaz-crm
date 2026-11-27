# Task 234 — Báo cáo tồn hôm nay, tồn một ngày đã qua, xuất nhập tồn (M6-11)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 234 = M6-11). Loại việc (quy trình mục 3): A

Điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 sáng 27/11 và cho bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ
staging); điều kiện "M1–M5 đóng" của M6-00 coi như được miễn. Chủ shop vắng, giao toàn quyền quyết định trong phạm vi
kế hoạch, spec (xem "Quyết định thay chủ shop").

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `internal/modules/reporting/stock.go` (tệp giữ chỗ của Task
  228). `StockParams` thêm trường `WarehouseID`, `SKUPrefix` (`q`), `Cursor`, `Limit`, `Validate` kiểm thật (chỗ dùng:
  `stockDef` trong cùng tệp, `registry_test.go` gọi `DecodeParams("stock", {})` — vẫn qua). `MovementsParams` thêm
  `WarehouseID`, `SKUPrefix`, `Cursor`, `Limit` (chỗ dùng: `movementsDef`; `registry_test.go` ca 367 ngày — vẫn
  `ErrRangeTooLong`). Thêm mới: `StockCursor` (+ `String`, `ParseStockCursor`), `StockRow`, `StockReport`,
  `MovementRow`, `MovementsReport`, `StockPage` (phần lọc kho, tiền tố, con trỏ, limit chung, nhúng vào hai kiểu tham
  số), `ErrNoSnapshot`, `ErrWarehouseForbidden`, `ErrUnknownDocType`, hằng nguồn
  `SourceLive`/`SourceSnapshot`, `DefaultStockLimit`/`MaxStockLimit`. `Defs()` không đổi (stockDef, movementsDef gán
  thêm `JSON`). Handler `GetInventoryStock`, `GetInventoryMovements` (`internal/httpapi/handlers_reports_stock.go`) thay
  thân 501. Tệp test mới `internal/modules/reporting/export_test.go` (cửa cho test EXPLAIN dựng câu như lúc chạy).
- Gói phụ thuộc ngược (go list): `reporting` được nhập bởi `cmd/crm`, `internal/httpapi`. Không đổi chữ ký họ dùng.
- Operation OpenAPI bị chạm, oasdiff: `getInventoryStock`, `getInventoryMovements` — hiện thực đúng hợp đồng đã có
  (Task 225), không sửa `openapi.yaml`; oasdiff không đổi. Xóa hai dòng khỏi `tests/arch/testdata/m6_pending.txt`.
  Đã gộp `main` (f1af3db7, fast-forward, không xung đột) trước review. Mã lỗi `no_snapshot` đã có ở `x-error-codes`; thêm dòng đổi lỗi ở `internal/httpapi/errors_reports.go` và
  `errors_snapshot_test.go` (`ErrNoSnapshot` → 404 `no_snapshot`, `ErrWarehouseForbidden` → 403 `forbidden`).
- Chỗ dùng trên màn hình: `console/src/screens/reports/StockReport.tsx`, `inventoryData.ts` (Task 240 đã đọc đúng hợp
  đồng); không sửa console.
- Câu sqlc, bảng, pha migration: tệp mới `queries/reporting/stock.sql` (`SnapshotDayInfo`: ngày chụp đầu tiên và ngày
  hỏi đã chụp chưa), `sqlc generate` sinh `internal/modules/reporting/internal/store/stock.sql.go`. SQL dựng lúc chạy
  ở `stock_dynsql.go`, `movements_dynsql.go` (lọc kho, tiền tố SKU, con trỏ). Chỉ đọc: `inventory.stock_available_v`,
  `inventory.stock_ledger_v`, `inventory.warehouses_v`, `catalog.variants_v` (đã có trong `viewReadExceptions` của
  reporting), `reporting.stock_daily_snapshots`, `reporting.stock_snapshot_days`. Không migration (chỉ mục
  `stock_ledger_balance`, `stock_ledger_created`, `variants_sku_prefix` có sẵn).
- Test sẽ chạy: trong lúc viết `go test -race -count=1 -run '^(TestStock.*|TestMovements.*)$'
  ./internal/modules/reporting/`, `-run '^TestReportsStock.*$' ./internal/httpapi/`; cả gói reporting, httpapi,
  cmd/crm, tests/arch ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có bộ mã hóa con
  trỏ chung: mỗi module tự có, vd `reporting.DrillCursor`, `inventory`/`catalog` cursor); trong reporting:
  `sqlBuilder` (`b.Arg`), `ParsePeriod`/`PeriodParams`, `Today`, `VN`, `DataScope`/`ScopeFor`, `newDef`,
  `Interactive.Run`/`runReport` (httpapi), mẫu `DrillCursor` (base64url nhị phân), mẫu khoảng tiền tố
  `[q, q || U+10FFFF)` của `catalog.search_variants` (0123); `reportseed.Ledger`, `SnapshotHandler` (Task 233) để dựng
  bản chụp thật trong test; `testkit.ExplainAsApp` không trả tên bảng nên test EXPLAIN tự đọc kế hoạch JSON.
- Dùng lại / viết mới vì: dùng lại `sqlBuilder`, `PeriodParams` (qua `ParsePeriod`), `Today`, `ScopeFor`, `runReport`, cách
  khoảng tiền tố của 0123. Viết mới `StockCursor` (khóa keyset `(sku, variant_id, warehouse_id)` khác `DrillCursor`
  `(mốc, mã đơn, total)`: khác dữ liệu, khác lý do đổi — giữ riêng). Phần lọc chung của hai câu (người bán, kho, tiền
  tố, con trỏ) gom một hàm `stockFilter` dùng cho cả ba câu (live, snapshot, xuất nhập tồn).

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1, 5.2: chỉ đọc; không ghi bảng nào.
- 5.3: đọc dữ liệu module khác chỉ qua view công bố (`inventory.*_v`, `catalog.variants_v`), bảng của chính
  `reporting`; không nhập Go của inventory, catalog.
- 5.4, 5.5, 5.6: không áp dụng (không transaction ghi, không sự kiện, không gọi ngoài).
- 5.7: chạy trong `tenancy.WithTenant(TxReport)` của `Interactive.Run` (crm_app, RLS); lọc thêm `seller_id` = người bán
  own (ADR-29).
- 5.8: hai GET có `x-permission: report.inventory.read`; phạm vi kho từ `ScopeFor` (grant `warehouse`/`all`);
  `warehouseId` ngoài phạm vi → 403 `forbidden`; lỗi đổi ở `errors_reports.go`.
- 5.9, 5.10, 5.11: không áp dụng (không đường ghi, không Valkey, không thư viện mới).
- 5.12: theo mẫu báo cáo có sẵn (carriers, drilldown); SQL động trong `*_dynsql.go` có `*_dynsql_test.go` trên
  PostgreSQL thật; SQL tĩnh qua sqlc.

Mục 6 (đồng thời, đúng):
- 6.1, 6.2, 6.3, 6.4, 6.6, 6.7, 6.8: không áp dụng — chỉ đọc trong transaction chỉ đọc, không khóa, không lệnh.
- 6.5: READ COMMITTED; mỗi trang là **một** câu SQL nên số dư, sổ, tên đọc cùng một ảnh dữ liệu (xuất nhập tồn: số dư
  hiện tại và sổ từ đầu kỳ trong một câu). Giữa hai trang dữ liệu có thể đổi (keyset không trùng, không sót theo khóa).
- 6.9: hạn giờ, chỗ của `Interactive` (30 s, 2 chỗ); `limit` ≤ 500; EXPLAIN không Seq Scan trên `stock_ledger`; tiền tố
  SKU dùng chỉ mục `variants_sku_prefix`.
- 6.10: không tiền.
- 6.11: quy luật `Closing = Opening + Imported + ReturnedIn − Sold − ExportedOther + Adjusted` và `Closing` kỳ tới ngày
  d = `on_hand` bản chụp ngày d có test.
- 6.12: không dữ liệu cá nhân (chỉ SKU, tên mẫu, màu, size, kho, số lượng).
- 6.13: quyền theo phạm vi kho (test người chỉ có kho W1).

## Quyết định thay chủ shop

- **QĐ-234-1 — đầu kỳ, cuối kỳ của xuất nhập tồn tính từ số dư hiện tại trừ sổ từ mốc** (cuối kỳ = `on_hand` hiện tại −
  sổ `on_hand` từ cuối kỳ; đầu kỳ = `on_hand` hiện tại − sổ `on_hand` từ đầu kỳ), không cộng cả sổ từ ngày đầu tới đầu
  kỳ như chữ của kế hoạch. Vì: số dư luôn bằng tổng sổ (ghi cùng transaction, G4), nên hai cách ra cùng số, nhưng
  cách này chỉ đọc sổ từ đầu kỳ (qua chỉ mục `stock_ledger_balance`) thay vì cả lịch sử, và cùng công thức với chụp tồn
  của Task 233 nên `Closing` khớp bản chụp. Phương án khác: cộng sổ từ đầu (đọc toàn bộ lịch sử mỗi lần xem). Đảo
  ngược: đổi biểu thức `since` trong `movements_dynsql.go`.
- **QĐ-234-2 — ngày hỏi bằng hôm nay (giờ VN) đọc số dư (nguồn `live`)**, như bỏ trống; ngày đã qua chưa chụp (vd hôm
  qua trước 00:10, lúc lịch chụp chưa chạy) → 404 `no_snapshot`. Hôm nay tính bằng đồng hồ của tiến trình (`time.Now`),
  một đồng hồ với `Validate`. Đảo ngược: một dòng trong `stockReport`.
- **QĐ-234-3 — kỳ xuất nhập tồn được phép kéo qua hôm nay** (ngày cuối trong tương lai: cuối kỳ = số dư hiện tại; kế
  hoạch không cấm, hợp đồng chỉ nói kỳ ≤ 366 ngày); ngày mai của báo cáo tồn vẫn 400 `bad_date` đúng hợp đồng.
- **QĐ-234-4 — tiền tố SKU viết hoa trước khi lọc** (SKU luôn viết hoa theo CHECK của 0114), bỏ dấu cách đầu cuối;
  rỗng sau khi bỏ dấu cách = không lọc. Khoảng tiền tố `[p, p || U+10FFFF)` như `catalog.search_variants` (0123).
- **QĐ-234-5 — thứ tự trang theo SKU `COLLATE "C"`** (thứ tự byte), cùng một biểu thức ở ORDER BY và so sánh keyset,
  không phụ thuộc collation của database.
- **QĐ-234-6 — `doc_type` lạ** (ngoài bảy loại) trong ngăn `on_hand` của kỳ → `ErrUnknownDocType` (500, không đổi mã
  lỗi hợp đồng). Hai loại flash sale của 0540 (`flash_hold`, `flash_release`) chỉ ghi ngăn `held` nên không vào xuất
  nhập tồn; ghi ở ngăn `on_hand` thì báo lỗi (test).
- Chỉ đọc tồn của người bán own (ADR-29), như mọi báo cáo M6.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Hôm nay → `live`, áo A-S {18,0,0,0}, available 18; `date` = hôm nay cũng `live`; 16/11 → `snapshot` {20,0,2,0}, available 18; 15/11, 17/11 đúng bản chụp | `TestStock_LiveAndSnapshot` | `go test -race -count=1 -run '^(TestStock.*|TestMovements.*)$' ./internal/modules/reporting/` | `ok … reporting 2.703s` |
| 10/11 (trước bản chụp đầu) và 20/11 (chưa chụp) → `ErrNoSnapshot`; hôm nay trả `FirstSnapshotDay = 2026-11-15`; ngày mai, ngày sai dạng → `ErrBadDate` | `TestStock_LiveAndSnapshot` | như trên | ok |
| Đọc từ bản chụp: dòng sổ chèn muộn (chủ bảng) trước mốc 16/11 → 16/11 vẫn on_hand 20 | `TestStock_SnapshotNotRecomputed` | như trên | ok |
| Người chỉ có kho W1 không thấy W2 (hôm nay và bản chụp); `warehouseId = W2` → `ErrWarehouseForbidden`; dòng mọi ngăn 0 ẩn; không phạm vi kho → không dòng | `TestStock_WarehouseScope`, `TestStock_NoInventoryScope` | như trên | ok |
| 1.200 biến thể, limit 500 → trang 500, 500, 200; không trùng; tăng dần (SKU, biến thể, kho); tiền tố `  ao-  ` → đúng 600 SKU AO-, cả hôm nay lẫn bản chụp | `TestStock_Pagination` | như trên | ok |
| Tham số sai (limit, con trỏ, tiền tố > 64) → `ErrBadParams`; con trỏ đi về | `TestStock_BadParams` | như trên | ok |
| EXPLAIN crm_app: tiền tố dùng `variants_sku_prefix`; không Seq Scan trên `variants`, `stock_daily_snapshots` | `TestStockExplainSKUPrefix` | `go test -race -count=1 -run '^(TestStockExplain.*\|TestMovementsExplain.*)$' ./internal/modules/reporting/` | `ok … reporting 2.258s` |
| EXPLAIN crm_app xuất nhập tồn (all, một kho, tiền tố): không Seq Scan trên `stock_ledger` | `TestMovementsExplainNoLedgerSeqScan` | như trên | ok |
| Xuất nhập tồn 16/11 áo A-S: đầu 20, nhập 0, nhập trả 1, bán 2, điều chỉnh −1, cuối 18 = on_hand bản chụp 16/11; kỳ tới hôm nay cuối = on_hand hiện tại; đủ bảy doc_type (áo B: xuất khác 2, điều chỉnh 2, lỗi cuối kỳ 1); held/flash_hold không vào cột; dòng chỉ có sổ trước kỳ ẩn; đẳng thức cuối kỳ mọi dòng | `TestMovements_PlanScenario` | `go test -race -count=1 -run '^(TestStock.*|TestMovements.*)$' ./internal/modules/reporting/` | ok |
| Xuất nhập tồn: phạm vi kho, lọc W2 → sentinel 403, tiền tố, trang keyset, kỳ sai, `doc_type` lạ ở on_hand → `ErrUnknownDocType` | `TestMovements_ScopeAndErrors` | như trên | ok |
| HTTP: JSON đúng hợp đồng (live, không firstSnapshotDay khi chưa chụp, nextCursor), kho ngoài phạm vi 403 `forbidden`, 404 `no_snapshot`, 400 `bad_date`, con trỏ hỏng 400 `bad_params`; GET xuất nhập tồn đúng số | `TestReportsStock_Report` | `go test -count=1 -run '^TestReportsStock_Report$' ./internal/httpapi/` | `ok … httpapi 1.273s` |
| Bảng lỗi: `ErrWarehouseForbidden` → 403 forbidden, `ErrNoSnapshot` → 404 no_snapshot | `TestDomainErrorSnapshot*` | `go test -count=1 -run '^(TestDomainErrorSnapshot.*)$' ./internal/httpapi/` | ok |
| Hai operation không còn 501, xóa khỏi `m6_pending.txt` | `TestM6Contract_StatusCodes`, `TestM3Routes_PendingList` | `go test -count=1 -run '^(TestM3Routes_PendingList\|TestM6Contract_StatusCodes)$' ./internal/httpapi/` | `--- PASS` cả hai |
| ci-task lượt 1 (GitHub, run 46060465878): test mọi gói ảnh hưởng xanh; cổng tĩnh đỏ (funlen, gofumpt, nolintlint ở test; jscpd 2 đoạn) → đã sửa (tách hàm test, `planLedger` dùng lại của snapshot_test, hằng `stockVariantCols`/`stockVariantJoin`) | — | `scripts/agent/ci-task.sh` | `CI-TASK ĐỎ` (chỉ static) |
| ci-task lượt 2 (GitHub, run 46062075640): cổng tĩnh + test httpapi, reporting (-race), cmd/crm, httpapi/web, conversation, files, realtime | toàn bộ gói ảnh hưởng | `scripts/agent/ci-task.sh` | `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh f1af3db7)` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 234 không có test e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `Cursor` trong hai kiểu tham số là chuỗi mờ (`StockCursor.String`), không phải `*StockCursor`: tham số JSON của lần
  chạy nền giữ đúng tên/kiểu tham số query `cursor` của GET (như `DrilldownParams.Cursor` của Task 230).
- Phần lọc chung tách thành kiểu `StockPage` nhúng vào `StockParams`, `MovementsParams` (tên JSON giữ nguyên).
- Đầu kỳ tính theo QĐ-234-1.
- Thêm `queries/reporting/stock.sql` (`SnapshotDayInfo`; SQL tĩnh phải qua sqlc) và `export_test.go`; test EXPLAIN ở
  `stock_dynsql_test.go`, `movements_dynsql_test.go` (luật kiến trúc: mỗi `*_dynsql.go` có test đi kèm).

## Việc phát hiện thêm

- (reviewer) `StockReport`, `MovementsReport` không có tag JSON (như `CarriersReport` có sẵn); Task 235 (kết quả lần
  chạy nền dạng `json` phải bằng đúng JSON của GET) cần đổi sang kiểu của hợp đồng khi ghi kết quả.
- Đầu kỳ xuất nhập tồn (QĐ-234-1) và bản chụp (Task 233) cùng dựa vào bất biến số dư = tổng sổ; nếu đối soát tồn phát
  hiện lệch thì cả hai lệch theo.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lượt 1, trên phần đã stage sau khi gộp main f1af3db7: 17 tệp, +1.584/−19).

- Phát hiện phải sửa: Không có.
- Ảnh hưởng dự kiến khớp thực tế (grep không còn chỗ dùng cấu trúc tham số cũ); `ErrUnknownDocType` cố ý không đổi
  mã → 500 (QĐ-234-6); hai operation khớp hợp đồng, không sửa openapi.
- Bảng bằng chứng: mọi test có thật và kiểm đúng tiêu chí (HTTP qua `testServer` thật; EXPLAIN đọc kế hoạch JSON bằng
  crm_app dưới RLS).
- QĐ-234-1..6 chấp nhận: đẳng thức cuối kỳ đúng theo cấu tạo câu; `flash_hold`/`flash_release` chỉ ghi ngăn held
  (`inventory/flash.go`), nên không đường ghi thật nào làm báo cáo 500.
- Ghi chú không bắt buộc: mục Tái dùng nhắc `DayCutoff` nhưng chỉ dùng gián tiếp qua `Period()`; tag JSON cho Task 235
  (đã ghi ở Việc phát hiện thêm).
