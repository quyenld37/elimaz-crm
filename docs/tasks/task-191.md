# Task 191 — Còn/hết, đã bán 30 ngày, địa giới hành chính

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 191, M5-08). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu cũ.
  - `webcatalog`: thêm `Availability`, `Sold30`, kiểu `WebAvailability`, `WebSold`, hằng `MaxAvailabilityIDs`, `LowStockMax`, `SoldWindow`, hàm `NotSoldStatuses` (cho test so hằng của order); hàm riêng `defaultWarehouse` tách từ `attachVariants` (hành vi giữ nguyên). Không đổi `Products`, `ProductsByID`, `Changes`, `Taxonomy` (0 chỗ gọi đổi).
  - `customer`: thêm `ListAllWards(ctx, tx)` (mọi xã, theo tỉnh rồi tên). `ListWards` giữ nguyên (chặn ở `MaxWardHits = 50`, 2 chỗ gọi: `handlers_admin_units.go`, test customer) — không đổi.
  - `web` (lớp HTTP): thêm `(*Catalog).Availability`, `(*Catalog).Sold30`, `(*Catalog).Locations`; `httpapi` thêm 3 handler `WebAvailability`, `WebSold30d`, `WebLocations` (ogen sinh interface). Hàm riêng mới `eachCached` (đọc từng mã qua bộ đệm, lần thiếu đầu tải mọi mã còn lại một lần) dùng chung cho `byIDs` của Task 190 và `Availability` (jscpd báo trùng; cùng mục đích, cùng lý do đổi) — hành vi `byIDs` không đổi, các test `TestWebCatalog_*` của Task 190 chạy lại.
  - `invtest` (helper test của inventory): thêm `Export(t, tx, w, warehouseID, variantID, qty)` (phiếu xuất qua public API của inventory), dùng ở 3 test (webcatalog, web, cmd/crm); không đổi `SeedStock`, `Shop`.
  - Test có sẵn được sửa (chỉ cấu trúc, không đổi điều kiểm): `cmd/crm/webcache_test.go` tách phần nối vai trò api/worker thành `newCrossRole` (thêm bộ bọc `invalidations` ghi lần xóa khóa của Invalidator) dùng cho test giá của Task 190 và test còn/hết mới; `internal/httpapi/web/catalog_test.go` thêm `valkeyCache`, `countingCache` ghi hạn sống theo vùng; `customer/adminunits_test.go` tách `seedManyWards`; `webcatalog/explain_test.go` thêm ca `availabilityOf`.
- Gói phụ thuộc ngược (go list): `queries/webcatalog` ← `httpapi/web` ← `httpapi` ← `cmd/crm`; `modules/customer` có 30 gói nhập (chỉ thêm hàm, không đổi gì của họ).
- Operation OpenAPI bị chạm, oasdiff: thêm 3 operation `webAvailability` (`GET /api/v1/web/availability?variantIds=`, ≤ 100), `webSold30d` (`GET /api/v1/web/catalog/sold-30d`), `webLocations` (`GET /api/v1/web/locations`, ETag/304); `x-auth: service_key`, `x-permission: web.catalog.read` (quyền có từ Task 189, không thêm dòng `docs/quyen.md`). Schema mới `WebAvailability`, `WebAvailabilityItem`, `WebSold30`, `WebSoldItem`, `WebLocations`, `WebProvince`, `WebWard`. oasdiff: chỉ thêm.
- Chỗ dùng trên màn hình: không (route của website, console không gọi).
- Câu sqlc, bảng, pha migration: không migration. Thêm `queries/webcatalog/availability.sql` (`AvailabilityOf`: `catalog.variants_v` + `inventory.stock_available_v` kho MAIN), `queries/webcatalog/sold30.sql` (`Sold30`: `ord.orders_v` + `ord.order_items_v` + `catalog.variants_v`), `queries/customer/admin_units.sql` thêm `ListAllAdminWards` (bảng `customer.admin_units` của chính customer). `make gen` sinh lại store của webcatalog và customer.
- Test sẽ chạy: `internal/queries/webcatalog` (cả gói, nhỏ), `internal/httpapi/web` (cả gói), `cmd/crm -run TestWebCache_` (đường worker → bộ đệm), `internal/modules/customer -run 'ListAllWards|ListWardsLimitCapped'`, `tests/arch` (sqlusage, boundaries); cả gói và vùng rủi ro cao inventory (invtest đổi) ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `cache.ReadCache` (+ `SpaceAvailability`, `SpaceSold30`, `SpaceLocations` có sẵn từ Task 185), `webcache` consumer/Invalidator (đã xóa `SpaceAvailability` theo mã biến thể khi `stock.changed` — không cần sửa), `etag`/`matches`/`dedupe` trong `httpapi/web/catalog.go` (Task 190), `inventory.Availability` (module; đọc trong transaction của đơn — không dùng vì webcatalog không nhập module, luật f), `customer.ListProvinces`/`ListWards`, `testkit.SeedAdminUnits`, `inventory.DefaultWarehouseCode` (đã có bản sao `webcatalog.DefaultWarehouseCode` + test so hai hằng).
- Dùng lại / viết mới vì: dùng lại ReadCache, etag/matches/dedupe, WarehouseByCode (sqlc có sẵn), ListProvinces. Viết mới `customer.ListAllWards` vì `ListWards` chặn 50 xã mỗi lần (ô chọn xã của màn hình) nên `ListWards(…, 10000)` như kế hoạch ghi chỉ trả 50 — không đủ cho tỉnh > 50 xã; nới chặn của `ListWards` sẽ đổi hành vi màn hình nhân viên. Câu `AvailabilityOf` viết mới (không dùng `VariantsOf` vì nó đọc theo mẫu, ở đây theo biến thể).

## Soát thiết kế

- 5.1 Chủ dữ liệu: không có dữ liệu mới; tồn của inventory, đơn của ord, địa giới của customer.
- 5.2 Không ghi gì (chỉ đọc).
- 5.3 Đọc module khác qua view: `queries/webcatalog` chỉ nhắc `catalog.variants_v`, `inventory.stock_available_v`, `inventory.warehouses_v`, `ord.orders_v`, `ord.order_items_v` (sqlusage_test). Địa giới đọc qua public API `customer.ListProvinces`/`ListAllWards` (A19, B11).
- 5.4–5.6 không áp dụng: chỉ đọc, không sự kiện, không gọi ngoài.
- 5.7 Mọi câu chạy trong `tenancy.WithTenant` (`db.TxAPI`); `admin_units` không có tenant_id, chạy được trong WithTenant.
- 5.8 webcatalog không nhập net/http; handler mỏng.
- 5.9 Chỉ GET, có `x-permission: web.catalog.read`, `x-auth: service_key`.
- 5.10 Valkey chỉ đệm còn/hết để hiện (ADR-39); đặt hàng vẫn kiểm tồn trong PostgreSQL (Task 192/193). Hạn sống 5 s là lưới an toàn khi báo hiệu mất.
- 5.11 Không thư viện mới. 5.12 Theo mẫu Task 190 (ReadCache.GetOrLoad bọc WithTenant).
- 6.1–6.4 không áp dụng: chỉ đọc một transaction READ COMMITTED, không khóa. Lỗi tạm của pgx đi qua `RetryTx` của WithTenant có sẵn; thân hàm gán lại kết quả mỗi lần chạy.
- 6.5 READ COMMITTED đủ: mỗi điểm cuối một câu SELECT (Availability một câu; Sold30 một câu; Locations hai câu đọc danh mục tĩnh).
- 6.6–6.8 không áp dụng (không việc nền, không gọi ngoài).
- 6.9 Câu `Sold30` theo index `orders_status_created (tenant_id, status, created_at)` và `order_items` theo đơn; đệm 600 s nên mỗi tenant tối đa một lần đọc mỗi 10 phút (thêm lần dự phòng khi Valkey ngừng, có giới hạn đồng thời của ReadCache). `AvailabilityOf` theo khóa chính biến thể và khóa chính số dư (≤ 100 mã).
- 6.10 Không tiền. 6.11 Không quy luật vùng rủi ro cao (chỉ đọc). 6.12 Không dữ liệu cá nhân (đơn chỉ lấy số lượng; không SĐT, địa chỉ). 6.13 Quyền `web.catalog.read`; khóa thiếu quyền → 403, thẻ nhân viên → 401 (test).

## Quyết định thay chủ shop

1. **"Đã bán 30 ngày" trả theo biến thể, kèm mã mẫu.** Kế hoạch ghi `WebSold{ProductID; Sold}` (theo mẫu); bản đối chiếu M5-00 (`docs/m5/doi-chieu-gia-dinh.md` mục 4, viết sau, đọc mã website thật) ghi "trả theo biến thể (website tự cộng theo mẫu các biến thể đang hiện)". Chọn trả cả hai: `{productId, variantId, sold}` — website cộng theo mẫu được, và bỏ được biến thể đang ẩn như hiện nay. Phương án khác: chỉ theo mẫu (mất khả năng bỏ biến thể ẩn). Dễ đảo: thêm trường, không xóa.
2. **Trạng thái không tính "đã bán": `draft`, `cancelled`, `returned`; cửa sổ `[now − 30 ngày, now)`, mọi kênh, mọi người bán** — đúng bản đối chiếu M5-00 (kế hoạch chỉ ghi "không cancelled"; đơn nháp chưa thành đơn, hoàn tương ứng NOT_SOLD của website). Hạn đệm theo kế hoạch 600 s (bản đối chiếu nói 1 ngày; 600 s mới hơn và rẻ: một câu theo index mỗi 10 phút).
3. **Địa giới đọc bằng hàm mới `customer.ListAllWards`** thay vì `ListWards(…, 10000)` như kế hoạch: `ListWards` chặn ở `MaxWardHits = 50` (ô chọn xã của màn hình), nên gọi theo kế hoạch chỉ ra 50 xã mỗi tỉnh (tỉnh sau 07/2025 có tới hơn 100 xã). Không nới chặn của `ListWards` để màn hình nhân viên giữ nguyên. Hàm mới nằm trong module chủ dữ liệu (customer), đọc bảng của chính nó.
4. **Còn/hết: `Available = có thể bán > 0`, `Low = 1…3`, chỉ theo tồn ở kho MAIN**, không gộp cờ `sellable`/ngừng bán của biến thể (website đã có `status`, `sellable` trong danh mục). Mã biến thể lạ (không có trong cửa hàng) không có trong `items`, như `ids` của Task 190.
5. **Phần "đã giữ" trong test giả lập bằng phiếu xuất của M1** (`invtest.Export`): giữ hàng của đơn (`Reserve`) cần dòng đơn thật (khóa ngoại sang `ord.order_items`), còn bút toán giữ cho đợt flash sale chưa có (M5-17). Phiếu xuất đi cùng đường `applyDelta` → `stock.changed` như giữ hàng, và giảm `available` của `stock_available_v` như `held`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tồn 2 → `available true, low true`; 7 trừ 5 đã giữ (phiếu xuất M1) → còn 2, `low true` [FS09]; 4 → không sắp hết; 0 → hết; biên 3; mã lạ, mã trùng; theo mã tăng dần | `TestAvailability` (webcatalog), `TestWebAvailability` (HTTP, kèm `ttlS 5`, hạn đệm `SpaceAvailability` 5 s) | `go test -race -count=1 -run '^(TestAvailability\|TestAvailability_Limit\|TestSold30)$' ./internal/queries/webcatalog/`; `go test -race -count=1 -run '^(TestWebAvailability\|…)$' ./internal/httpapi/web/` | `ok internal/queries/webcatalog 4.263s`; `ok internal/httpapi/web 9.763s` |
| Đã đệm "còn" (tồn 2) → phiếu xuất 2 (`stock.changed`) → worker chạy consumer webcache → Invalidator xóa khóa `availability/<biến thể>` → lần đọc ngay sau ra "hết" [WB02], [FS07] | `TestWebCache_StockChangedRefreshesAvailability` (cmd/crm, Valkey + PostgreSQL thật, vai trò api + worker; chờ theo sự kiện đúng lần `Invalidate` của biến thể qua bộ bọc `invalidations`, không qua được nhờ hết hạn 5 s) | `go test -race -count=1 -run '^TestWebCache_' ./cmd/crm/ -v` | `--- PASS: TestWebCache_StockChangedRefreshesAvailability (0.64s)`, log "hết hàng tới website sau 13.825541ms kể từ lúc worker bắt đầu" (chỉ quan sát, không phải tiêu chí) |
| Không worker: sau hạn sống 5 s vẫn thấy hết (lưới an toàn) | `TestWebAvailability_TTLWithoutWorker` (Valkey thật, không Invalidator; có lần đọc `SourceCache` trước khi đổi) | như dòng 1 | `ok internal/httpapi/web` |
| 100 mã → 200; 101 → 400 (cả thiếu, sai dạng); `Availability` 101 → `ErrBadLimit` | `TestWebAvailability_Params`, `TestAvailability_Limit` | như dòng 1 | ok |
| `Sold30`: đơn hủy, đơn 31 ngày, nháp, hoàn không tính; đúng `now − 30 ngày` tính, sớm hơn 1 µs không tính, đúng `now` không tính (cửa sổ `[now − 30 ngày, now)`); mọi người bán; mốc `now` lùi 2 ngày đổi cửa sổ | `TestSold30` | `go test -race -count=1 -run '^TestSold30$' ./internal/queries/webcatalog/` | `ok internal/queries/webcatalog 2.114s` |
| `webSold30d` qua HTTP: `items` rỗng khi chưa đơn, `asOf` RFC 3339, đệm 600 s; trạng thái loại trừ trùng hằng `order` | `TestWebSold30d`, `TestNotSoldStatuses` | như dòng 1 | ok |
| `webLocations`: tỉnh có xã (chỉ `code`, `name`, không mã hãng), ETag, lần hai `If-None-Match` → 304 rỗng, đệm 1 ngày | `TestWebLocations` | như dòng 1 | ok |
| Mọi xã của tỉnh > 50 xã (không chặn `MaxWardHits`), theo tỉnh rồi tên | `TestListAllWards` | `go test -race -count=1 -run '^(TestListAllWards\|TestListWardsLimitCapped)$' ./internal/modules/customer/` | `ok internal/modules/customer 2.109s` |
| Quyền: không khóa → 401, khóa thiếu `web.catalog.read` → 403, đúng → 200 (3 route) | `TestWebAvailability_Auth` | như dòng 1 | ok |
| `AvailabilityOf` trên 50.000 biến thể không Seq Scan | `TestPlansHaveNoSeqScan/còn/hết_100_biến_thể` | `go test -count=1 -run '^TestPlansHaveNoSeqScan$' ./internal/queries/webcatalog/` | `ok internal/queries/webcatalog 10.602s` |
| SQL webcatalog chỉ nhắc view `_v`; ranh giới gói | `tests/arch` (sqlusage, boundaries) | trong cổng kiểm của task | `ok elimaz.com.vn/crm/tests/arch 138.496s` |
| `byIDs` của Task 190 không đổi hành vi sau khi dùng `eachCached` | `TestWebCatalog_CacheAndFallback`, `TestWebCatalogProducts_Params`, `TestWebCatalog_ETag` | như dòng 1 | ok |
| Cổng của task | `ci-task.sh` (build, vet, lint, gen-check, sqlc vet, jscpd, gitleaks, oasdiff, test-guard, console, test gói đổi + inventory vùng cao + gói phụ thuộc) | chạy nền, log ngoài worktree | `CI-TASK XANH (so với điểm tách nhánh 282fac0…)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Hàm HTTP đặt ở tệp mới `internal/httpapi/web/availability.go` (còn/hết, đã bán) thay vì thêm vào `catalog.go` (đã 350 dòng); `locations.go` như kế hoạch.
- Test "tạo đơn giữ 2 → worker → hết hàng" đặt ở `cmd/crm/webcache_test.go` (cần nối vai trò api + worker bằng `newModules`, `newWebCache` của `cmd/crm`, như test giá của Task 190), không ở `internal/httpapi/web/availability_test.go`; dùng phiếu xuất thay đơn (Quyết định 5).
- Thêm `customer.ListAllWards` (Quyết định 3) và `invtest.Export` (Ảnh hưởng).

## Việc phát hiện thêm

- Câu `Sold30` chưa có ca trong `TestPlansHaveNoSeqScan` (bộ dữ liệu explain của webcatalog chưa có đơn). Có đệm 600 s nên mỗi tenant tối đa một lần đọc mỗi 10 phút; nên thêm ca explain khi bộ dữ liệu có đơn (gợi ý của reviewer, không bắt buộc).

## Reviewer

Lượt 1: **CẦN SỬA** — (1) `TestWebCache_StockChangedRefreshesAvailability` qua được nhờ hạn sống 5 s nên không chứng minh đường worker → Invalidator; (2) `TestSold30` không kiểm biên 30 ngày thật. Đã sửa: test chờ theo sự kiện đúng lần `Invalidate` của khóa `availability/<biến thể>` (bộ bọc `invalidations`) rồi đọc một lần; `TestSold30` có đơn đúng `now − 30 ngày` (tính), sớm hơn 1 µs (không tính), đúng `now` (không tính). Chạy lại `ci-task.sh`: XANH.

Lượt 2: **ĐẠT** — không có phát hiện phải sửa; hai chỗ trên đã đúng (reviewer tính tay kết quả `TestSold30` cả hai mốc). Ghi chú không bắt buộc: liệt kê thêm `SoldWindow`, `NotSoldStatuses`, `defaultWarehouse`, `invalidations` trong mục Ảnh hưởng (đã thêm); `TestWebAvailability_TTLWithoutWorker` có thể không thấy giai đoạn "còn cũ" nếu bản đệm sắp hết hạn, vẫn đúng nhờ đòi có lần đọc `SourceCache`.

Kết luận: ĐẠT
