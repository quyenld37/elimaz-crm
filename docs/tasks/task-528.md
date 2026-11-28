# Task 528 — Hai cột Có thể bán, Đã bán/suất trên màn Đợt flash sale (khớp mockup 514)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (task bù lệch mockup của Task 206, không có trong tệp PLAN; giao trong chat điều phối 28/11). Loại việc (quy trình mục 3): A

Việc: thêm vào API số "Đã bán / suất" của đợt (danh sách, một đợt) và "Có thể bán lúc này" của món; hiện hai cột như mockup 514 (`docs/mockups/m5/flash-sales.html` trạng thái A, C), cùng hai dòng số "bán thường còn X → Y" (hộp Lên lịch, mockup D) và "X → Y" (hộp Thêm suất, mockup F) mà QĐ-206-1 bỏ vì cùng thiếu số này.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (tìm theo tên gọi `promotion.X(` trong mọi tệp .go, trừ mã sinh): chỉ THÊM trường, không đổi chữ ký. `promotion.FlashSale` thêm `Quantity, Sold int`; `promotion.FlashItem` thêm `Sellable int`. Hàm có nội dung trả về đổi: `promotion.GetFlashSale` (8 chỗ gọi: handlers_flash_sales.go ×5, flashtest.go, order/flashbuy_test.go, promotion/flash_test.go), `promotion.ListFlashSales` (2: handler, flash_test.go), `promotion.AddFlashItem` (5: handler, flashtest.go, flash_test.go, order/flashbuy_test.go), `promotion.UpdateFlashItem` (2: handler, flash_test.go). Hàm không xuất ra ngoài trong httpapi: `flashSummaryOut` (3 chỗ), `flashItemOut` (5 chỗ). Không ai so sánh nguyên struct (`==`) nên thêm trường không gãy chỗ gọi.
- Ký hiệu test mới: `flashbuytest.Buy` (gói mới `internal/testkit/flashbuytest`, chỉ cho test; 2 chỗ gọi: handlers_flash_counts_test.go, flashverify_test.go — `env.buy` gọi nó).
- Gói phụ thuộc ngược (go list, gói nhập `internal/modules/promotion`): cmd/crm, internal/httpapi, internal/httpapi/web, internal/modules/order, internal/modules/promotion/flashtest, internal/wiring/flashstate, internal/wiring/webcache (ci-task tự test các gói này).
- Operation OpenAPI bị chạm, oasdiff: schema `FlashSaleSummary` thêm `quantity`, `sold` (bắt buộc) → ảnh hưởng `listFlashSales`, `getFlashSale`, `createFlashSale`, `updateFlashSale`, `removeFlashItem`, `scheduleFlashSale`, `closeFlashSale` (mọi đường trả `FlashSale`/`FlashSalePage`); `FlashItem` thêm `sellable` (bắt buộc) → thêm `addFlashItem`, `updateFlashItem`, `addFlashSlots`. Chỉ thêm trường ở phản hồi: oasdiff không phá vỡ. Không thêm mã lỗi.
- Chỗ dùng trên màn hình: `console/src/screens/flash-sales/` — `FlashSaleList.tsx` (cột Đã bán / suất), `FlashSaleEditor.tsx` (cột Có thể bán lúc này của bảng nháp, dòng chú thích dưới bảng), `FlashDialogs.tsx` (ScheduleDialog, AddSlotsDialog), `flashRules.ts` (scheduleSummary, hàm chữ mới), `flashTestkit.tsx` (dữ liệu giả thêm trường); test màn `FlashSaleList.test.tsx`, `FlashSaleEditor.test.tsx`, `flashRules.test.ts`. `console/src/api/**` sinh lại bằng `make gen`. Màn web-campaigns có kiểu riêng, không dùng `FlashItem` của flash (grep `listPrice` chỉ trùng tên trường).
- Câu sqlc, bảng, pha migration: `queries/promotion/flash.sql` — `ListFlashSales` thêm hai cột `quantity` (tổng suất các món), `sold` (tổng đơn vị `sold` từ view công bố `inventory.flash_units_summary_v`, cùng view `FlashLiveItems` đang đọc); câu mới `FlashSaleSold :one` (số đơn vị đã bán của một đợt, cùng view; tổng suất của một đợt cộng trong Go từ món ở `GetFlashSale`). Có thể bán của món: hàm sẵn có `inventory.Availability` (kho của món). Không bảng mới, không migration.
- Tệp đổi thực tế (đối chiếu khi stage): `openapi/openapi.yaml`, `queries/promotion/flash.sql`, mã sinh (`internal/httpapi/gen/oas_json_gen.go`, `oas_schemas_gen.go`, `internal/modules/promotion/internal/store/flash.sql.go`, `console/src/api/types.gen.ts`), `internal/modules/promotion/flash.go`, `internal/httpapi/handlers_flash_sales.go`; test: `internal/httpapi/handlers_flash_counts_test.go` (mới), `internal/httpapi/handlers_flash_sales_test.go` (chỉ tách `seedStock`, không đổi ca), `internal/modules/promotion/flash_test.go` (thêm `TestFlashSale_Counts`), `internal/queries/flashverify/flashverify_test.go` (`env.buy` gọi `flashbuytest.Buy`, không đổi ca), gói mới `internal/testkit/flashbuytest`; màn: 8 tệp trong `console/src/screens/flash-sales/` (5 mã: FlashDialogs.tsx, FlashSaleEditor.tsx, FlashSaleList.tsx, flashRules.ts, flashTestkit.tsx; 3 test: FlashSaleEditor.test.tsx, FlashSaleList.test.tsx, flashRules.test.ts); tài liệu: `docs/tasks/task-206.md` (QĐ-206-1, 2 đánh dấu đã thay), `docs/views.md` (thêm người dùng view `flash_units_summary_v`).
- Test sẽ chạy: httpapi `TestFlashSaleAPI_Counts` (mới) + các test flash sẵn có của gói (`-run '^TestFlashSaleAPI_'`); promotion `TestFlash*` liên quan (`flash_test.go`); vitest `src/screens/flash-sales/`; tsc, eslint tệp sửa. Cả gói httpapi/promotion/order chạy ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (inventory: `Availability`, `LockFlashStock`, `ListBalances`); view `inventory.flash_units_summary_v` (docs/views.md, khai người dùng "màn Đợt flash sale, Task 206"); `promotion.Live` (số từng món); console `flashRules.ts` (`scheduleSummary`, `num`), `formatMoney`.
- Dùng lại / viết mới vì: có thể bán = `inventory.Availability(kho, biến thể)` (đúng hàm đọc số dư, cùng nguồn số `available` mà `LockFlashStock` dùng khi lên lịch, không khóa); đã bán = view công bố `flash_units_summary_v` (PostgreSQL, không đọc Valkey). Không dùng `LockFlashStock` vì nó khóa dòng số dư (đường đọc không được khóa). Không gọi `promotion.Live` cho từng đợt ở danh sách (N+1) — tổng gộp ngay trong câu `ListFlashSales`. Hàm nhỏ mới `fillSellable` (gom món theo kho, gọi `Availability`) chỉ dùng trong promotion.
- Cảnh báo jscpd (lượt ci-task 1) đã trả lời bằng GỘP: (1) lượt mua flash trong test (WithTenant + `order.Writer.BuyFlash` + kiểm bought) trùng `internal/queries/flashverify/flashverify_test.go` → hàm test dùng chung mới `flashbuytest.Buy` (gói mới `internal/testkit/flashbuytest`, theo mẫu `shiptest`: nằm ngoài internal/modules vì nhập order — đặt trong `promotion/flashtest` bị test kiến trúc chặn, promotion không được gọi order), `flashverify_test` gọi nó; (2) nạp tồn kho MAIN trùng `TestFlashSaleAPI_Lifecycle` → `(*flashEnv).seedStock`, cả hai test dùng.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1 chủ dữ liệu: không có dữ liệu mới; số đọc thuộc promotion (món, suất) và inventory (đơn vị suất, số dư).
- 5.2 không ghi bảng module khác: chỉ đọc, không ghi gì.
- 5.3 đọc module khác qua view/public API: đơn vị suất qua view công bố `inventory.flash_units_summary_v`; số dư qua public API `inventory.Availability`. Không đọc thẳng bảng inventory.
- 5.4, 5.5, 5.6: không áp dụng: không ghi, không sự kiện, không gọi ra ngoài.
- 5.7 ngữ cảnh shop: đọc trong `catalogRead` = `tenancy.WithTenant` (RLS lọc tenant); đợt tenant khác → 404 (test).
- 5.8 HTTP mỏng: tính số ở promotion; handler chỉ chép sang gen.
- 5.9 không áp dụng: không thêm đường ghi (đường ghi sẵn có chỉ trả thêm trường).
- 5.10 PostgreSQL là nguồn: "đã bán" đếm đơn vị `sold` trong PostgreSQL, không đọc Valkey (bộ đệm website).
- 5.11 không thêm thư viện. 5.12 theo mẫu sẵn có của module promotion (câu sqlc, test httpapi `flashEnv`).
Mục 6 (đồng thời):
- 6.1 không ghi; số đọc là ảnh chụp lúc đọc (READ COMMITTED, mỗi câu một ảnh), màn tự tải lại 5/20 s. Hai câu (đợt+tổng, số dư) có thể lệch nhau vài đơn vị nếu có lượt mua chen giữa: chấp nhận cho số hiển thị (không quyết định nghiệp vụ dựa trên số này; Lên lịch, Thêm suất vẫn khóa số dư và kiểm lại).
- 6.2 không áp dụng: GET. 6.3 không khóa dòng nào (Availability là SELECT thường) nên không thêm thứ tự khóa. Trong đường ghi (lên lịch, thêm suất) GetFlashSale chạy sau khi tx đã giữ khóa số dư của chính nó: đọc dòng đã khóa bởi chính tx, không chờ.
- 6.4 lỗi tạm: đường đọc dùng `db.TxAPI` như cũ. 6.5 READ COMMITTED mặc định.
- 6.6–6.8 không áp dụng: không việc nền, không gọi ngoài.
- 6.9 câu tổng: subquery tương quan theo `flash_sale_id` (chỉ mục `flash_sale_items_sale`) và `flash_sale_item_id` (chỉ mục `flash_sale_units_pick (tenant_id, flash_sale_item_id, status, id)`); trang tối đa 100 đợt; trong 5 s statement timeout.
- 6.10 không có tiền mới. 6.11 không áp dụng: không quy luật ghi mới (chỉ đếm). 6.12 không dữ liệu cá nhân (số đếm, không tên khách).
- 6.13 quyền: giữ `promotion.flash.read` của GET (bảng route không đổi); số có thể bán của biến thể là số tồn người có quyền xem flash sale vốn thấy được qua lỗi `insufficient_stock` (sellable) — không mở thêm phạm vi kho.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| API trả quantity/sold/sellable: nháp 7/0/20; đợt khác lên lịch 5 suất → sellable 15 cả hai đợt; đơn nháp đang giữ không tính sold; hoàn tất đơn → sold 2; nhả suất → sold 2, live trống 3, sellable 15; danh sách = một đợt | `TestFlashSaleAPI_Counts` | `go test -race -count=1 -v -run '^TestFlashSaleAPI_' ./internal/httpapi/` | `--- PASS: TestFlashSaleAPI_Counts (1.13s)`; cùng lượt PASS `_Lifecycle`, `_LifecyclePermissions`, `_Permissions`, `_Draft`; `ok elimaz.com.vn/crm/internal/httpapi 2.974s` |
| Tenant khác: một đợt 404 not_found, danh sách rỗng | `TestFlashSaleAPI_Counts` (cuối hàm) | như trên | PASS |
| Hàm promotion trả đúng tổng (ListFlashSales, GetFlashSale, AddFlashItem, UpdateFlashItem) | `TestFlashSale_Counts` | ci-task từ xa (run 46158124816) | `ok elimaz.com.vn/crm/internal/modules/promotion 77.386s` |
| Gói phụ thuộc ngược không gãy (order, flashverify dùng `flashbuytest.Buy`, webcache, cmd/crm) | cả gói | ci-task từ xa (run 46158124816) | `ok …/modules/order 175.069s`, `ok …/queries/flashverify 2.595s`, `ok …/wiring/webcache 3.542s`, `ok …/cmd/crm 34.357s` |
| Cổng tĩnh (lint, mã sinh không chênh, jscpd, oasdiff, test kiến trúc, gitleaks) | static | ci-task từ xa (run 46158124816) | `static: success, 7 phút 30 giây` |
| Màn: cột Đã bán / suất cả bốn trạng thái (nháp "— / suất"); cột Có thể bán lúc này (đỏ khi < suất); hộp Lên lịch "còn X → Y"/thiếu; hộp Thêm suất "X → Y"; hàm chữ | FlashSaleList.test "cột Đã bán / suất ở cả bốn trạng thái…", FlashSaleEditor.test "cột Có thể bán lúc này…", "mỗi món nêu có thể bán thường trước → sau…", "thêm suất: có thể bán thường trước → sau…", flashRules.test "đã bán / suất…", "có thể bán trước → sau…" | `cd console && npx tsc --noEmit && npx eslint src/screens/flash-sales/ && npx vitest run src/screens/flash-sales/` | tsc, eslint sạch; `Test Files 4 passed (4)  Tests 67 passed (67)` |
| Ca đỏ không thuộc task: `TestFlashAdmission_LimitsPerSubject` (Task 525) đỏ một lần ở test (3) | chạy lại riêng | `go test -race -count=3 -run '^TestFlashAdmission_LimitsPerSubject$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.362s` (3/3 qua); xem Việc phát hiện thêm |

## Quyết định thay chủ shop

- QĐ-528-1 (nguồn "Có thể bán lúc này"): số `available` của dòng số dư (biến thể, kho của món — kho MAIN khi thêm món) qua `inventory.Availability`; chưa có dòng số dư → 0. Cùng số mà Lên lịch/Thêm suất kiểm (`LockFlashStock`), nên chữ đỏ (ít hơn số suất) báo đúng lượt Lên lịch sẽ bị từ chối. Phương án khác: cộng mọi kho (sai, vì suất chỉ giữ ở kho của món).
- QĐ-528-2 ("Đã bán / suất"): suất = tổng `quantity` các món; đã bán = số đơn vị `sold` (đơn nháp đã hoàn tất) từ `inventory.flash_units_summary_v`; đơn vị đang giữ cho đơn nháp KHÔNG tính là đã bán (khớp cột "Đã bán" của bảng số trực tiếp, mockup F). Đợt nháp hiện "— / suất" như mockup (máy chủ trả sold 0). Phương án khác: tính cả đang giữ (lệch với bảng số trực tiếp).
- QĐ-528-3 ("transaction chỉ đọc"): đọc trong `catalogRead` (một transaction tenant chỉ chạy SELECT, `db.TxAPI`) như mọi GET; nền tảng `db.TxOptions` chưa có chế độ READ ONLY và thêm nó là sửa vùng rủi ro cao `platform/db` ngoài phạm vi task. Phương án khác: thêm `AccessMode` vào TxOptions (task nền tảng riêng nếu cần).
- QĐ-528-4 (hai dòng hộp thoại): làm luôn "giữ 300 · bán thường còn 820 → 520" (Lên lịch) và "Có thể bán thường của mã này 118 → 68" (Thêm suất) vì cùng thiếu số với QĐ-206-1 và luật "code khớp mockup". Mockup không vẽ ca thiếu hàng trong hộp Lên lịch: chọn "bán thường chỉ còn 96, thiếu 54" (chữ đỏ); hộp Thêm suất nhập quá số có thể bán: không hiện "X → Y" (máy chủ sẽ trả bảng thiếu như cũ). Dễ đảo: chỉ là chữ trên màn.
- QĐ-528-5: các đường ghi trả `FlashItem`/`FlashSale` (thêm/sửa món, lên lịch, thêm suất, đóng) cũng trả số mới, vì trường bắt buộc trong một schema dùng chung; promotion tính ở `GetFlashSale`, `AddFlashItem`, `UpdateFlashItem`.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ngoài hai cột: làm luôn hai dòng số của hộp Lên lịch và hộp Thêm suất (QĐ-528-4) vì cùng nguyên nhân QĐ-206-1.
- Gộp hai đoạn test lặp do jscpd báo (mục Tái dùng): gói test mới `internal/testkit/flashbuytest`, `(*flashEnv).seedStock`.

## E2E viết sẵn, chưa chạy

Không có: task không có ca e2e; đủ ca ở test httpapi (`TestFlashSaleAPI_Counts`), promotion (`TestFlashSale_Counts`) và vitest màn. Không chạy Playwright (luật chủ shop 28/11). Chụp 1512/390 so mockup: chờ lượt kiểm màn hình cuối mốc.

## Việc phát hiện thêm

- `TestFlashAdmission_LimitsPerSubject` (internal/httpapi/admission_test.go, Task 525) chập chờn: ci-task 28/11 (run 46158124816, test (3)) báo "khách 0 lượt 3: 200, muốn 429 too_fast" sau 0,07 s; chạy lại 3 lần trên máy đều qua. Nghi cửa sổ giới hạn 10 s (bộ đếm dự phòng trong bộ nhớ) cắt ngang giữa lượt 2 và lượt 3 nên bộ đếm về 0. Task này không chạm admission. Đề xuất task sửa: test dùng đồng hồ giả hoặc kiểm theo cửa sổ.
- Lượt ci-task trước (run 46140677143) đỏ ở ba test EXPLAIN no-seq-scan (order, dashboard, carriers) do hết giờ lúc nạp dữ liệu; Task 526 trên main đã sửa, lượt sau khi gộp main xanh ở các test đó.

## Reviewer

Kết luận: ĐẠT (lượt 2, 28/11).

- Lượt 1 CẦN SỬA: chỉ tệp task — mục Ảnh hưởng ghi câu sqlc `FlashSaleTotals` (thật là `FlashSaleSold`, tổng suất cộng trong Go) và "7 tệp" màn (thật 8, gồm 3 tệp test). Đã sửa.
- Lượt 2 ĐẠT: mã không đổi; không phát hiện về tính đúng; không test bị xóa, tắt, nới. Test kiểm đúng tiêu chí (mua, hoàn tất, nhả suất bằng hàm thật; so cả một đợt và danh sách; tenant khác 404; tập rỗng, chưa có dòng số dư → 0).
- Ghi chú không bắt buộc (không sửa, phong cách): `fmt` ở flashRules.ts trùng `num` đã có ở ba tệp màn; `stockAfter(...)` gọi hai lần một dòng ở FlashDialogs.tsx.
