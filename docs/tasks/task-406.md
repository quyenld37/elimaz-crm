# Task 406 — Kiểm kho thiếu hàng dưới phần giữ: chặn rõ ràng ở bước lấy/giao (M1-r1)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task bù review độc lập M1). Phạm vi: A-01
trong `docs/review-doc-lap-m1.md`, làm đúng mục "Quyết định cho Task 406…" cuối tệp đó. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): **thêm** `inventory.ErrStockShort`, `inventory.StockShortError`,
  `inventory.ShortHold`, trường `inventory.Shortage.SellerID`, `inventory.Shortage.Holds` (thêm trường, không đổi chữ
  ký); **thêm** `order.ItemOrders`, `order.ItemOrder`. `inventory.Shortage` có 1 chỗ dùng ngoài gói
  (`internal/httpapi/handlers_stocktake.go:103-104`) + test `stocktake_test.go`. `Writer.StartTransit`, `Consume`,
  `StartTransitByItems`, `ConsumeByItems` giữ chữ ký, chỉ thêm một lỗi có thể trả về: chỗ gọi ngoài gói duy nhất
  `internal/modules/order/shipment_progress.go:131,138` (bọc `%w`, không nuốt) → `fulfillment/refresh_status.go:151`
  (OnDone của lệnh tra vận đơn: lỗi → lệnh thử lại theo lịch, hết lượt → dead + cảnh báo; không đổi mã ở đó).
- Hành vi đổi không qua chữ ký: `Writer.PostDocument` phiếu `damage_export` (bước on_hand đòi `on_hand − in_transit ≥ q`
  thay vì `on_hand ≥ q`; số `Available` của `InsufficientError` = min(hàng lỗi, phần trên kệ)) — `documents.go:100,259`;
  `internal/httpapi/errors.go` (`codeStockShort`, nhánh `StockShortError` trong `stockShortage`, dòng bảng
  `inventory.ErrStockShort`); `handlers_stocktake.go` (`ApplyStocktake` + hàm mới `shortOrders`).
- Gói phụ thuộc ngược (go list): inventory ← order, httpapi, cmd/crm, testkit, testkit/shiptest, invtest; qua order ←
  fulfillment, cash, adapters/fakecarrier, tools/fakecarrier.
- Operation OpenAPI bị chạm, oasdiff: `applyStocktake` (thân 200: `StocktakeShortage` thêm trường bắt buộc `orders`,
  schema mới `StocktakeShortOrder`) — chỉ thêm trường ở phản hồi, không phá vỡ; `x-error-codes` + enum `Error.error`
  thêm `stock_short` (mở rộng). Mọi operation trả lỗi của inventory dùng chung bảng `errors.go`.
- Chỗ dùng trên màn hình: `console/src/screens/inventory/Stocktake.tsx` (khối "Thiếu so với phần giữ" hiện thêm mã đơn
  đang giữ), `console/src/app/errors.ts` (câu cho `stock_short`), mock `console/e2e/stocktake.phone.spec.ts`
  (kiểu `ApplyStocktakeResult` có `orders`). `console/src/api/**` sinh lại bằng `make gen`.
- Câu sqlc, bảng, pha migration: không migration. Câu sqlc: `ApplyDelta` (chỉ chú thích), mới
  `HeldReservationsOfVariants` (`queries/inventory/reservations.sql`, đọc `stock_reservations` + `stock_balances`, không
  khóa) và `ItemOrders` (`queries/ord/code.sql`, đọc `ord.order_items` + `ord.orders` của chính module order). Bảng
  không đổi.
- Test sẽ chạy: cả bộ `./internal/modules/inventory/...` (vùng rủi ro cao), `./internal/modules/order/...`,
  `./internal/modules/fulfillment/...`, `./internal/httpapi/...` (stocktake, errors, inventory), `./tests/arch/...`,
  console `npm test` + `tsc` + `eslint`; cuối cùng cổng kiểm của task (ci-task) một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm "tồn thực
  không đủ"); trong inventory: `applyDelta` + `cond` (`condOnShelf`, `condOnHand` đã có), `InsufficientError` (mẫu lỗi
  có chi tiết), `Availability` (đọc số dư để dựng lỗi); `order.Codes` (mã đơn theo id đơn, cùng người bán); httpapi
  `stockShortage` (thân lỗi kèm `variantId`, `available`).
- Dùng lại / viết mới vì: chặn bằng điều kiện có sẵn của câu `ApplyDelta` (`on_shelf` cho bước lấy hàng, `on_hand`
  cho bước giao) — không viết câu đổi số dư mới. Lỗi mới `StockShortError` riêng chứ không dùng lại
  `InsufficientError`/`ErrStateConflict`: khác nghĩa (giữ hàng đã có, tồn thực bị kiểm kho làm thiếu; cách xử lý là
  điều chỉnh, đổi kho, hủy) và khác mã HTTP (`stock_short`). Thân lỗi HTTP dùng lại `stockShortage` (thêm một nhánh).
  `order.ItemOrders` viết mới: `Codes` nhận mã đơn, ở đây chỉ có mã dòng đơn (inventory chỉ biết `order_item_id`).

## Soát thiết kế

5.1 Chủ dữ liệu: số dư, giữ hàng thuộc inventory; mã đơn thuộc order — inventory chỉ trả `order_item_id`, httpapi gọi
public API `order.ItemOrders` để ra mã đơn (inventory không được gọi order, 04 mục 3). 5.2 Không ghi bảng module khác:
đúng (chỉ đọc). 5.3 Đọc module khác qua view: order đọc bảng của chính nó; httpapi gọi hàm công khai của order. 5.4
Nguyên tử: chặn xảy ra trong transaction của bên gọi (lỗi → lùi cả transaction, không ghi nửa chừng). 5.5, 5.6 không áp
dụng: không sự kiện mới, không gọi ra ngoài. 5.7 Mọi câu mới chạy trong tx tenant của bên gọi (RLS). 5.8 Module không
nhập HTTP; đổi lỗi sang 409 `stock_short` ở `errors.go` một chỗ. 5.9 Không thêm đường ghi; `applyStocktake` đã có khóa
chống trùng và `x-permission`. 5.10, 5.11 không áp dụng: không Valkey, không thư viện mới. 5.12 Theo mẫu lỗi có chi tiết
(`InsufficientError`: kiểu lỗi có `Is` sentinel).

6.1 Đồng thời: điều kiện nằm trong câu UPDATE có điều kiện (`on_hand − in_transit ≥ q` khi lấy hàng, `on_hand ≥ q`
khi giao), READ COMMITTED đánh giá lại sau khi chờ khóa — hai đơn cùng lấy phần cuối trên kệ: một qua, một
`stock_short`. Có test song song. 6.2 Không đường ghi mới; lấy hàng, giao đi qua lệnh tra vận đơn đã chống trùng. 6.3
Thứ tự khóa ADR-46 nhóm 11 không đổi (khóa số dư → giữ hàng; đơn nhóm 7 trước); câu đọc mới không khóa. 6.4, 6.5 như
cũ (READ COMMITTED + câu có điều kiện). 6.6 Lệnh tra vận đơn gặp `stock_short`: OnDone lỗi → thử lại theo lịch (nhân
viên điều chỉnh tồn xong thì lần sau qua), hết lượt → dead + cảnh báo — không đánh lỗi vĩnh viễn vì tình trạng gỡ được.
6.7–6.9 không áp dụng: không gọi ngoài, không đổi hạn giờ. 6.10 không chạm tiền. 6.11 Quy luật: thêm vào test rapid kiểm
kho các thao tác **sau** áp dụng (lấy hàng, giao, nhả, nhập, phiếu kho mọi loại, rồi đếm lại) → không lỗi CHECK, mọi
cột ≥ 0, in_transit ≤ on_hand, sổ = số dư. 6.12 Lỗi, cảnh báo chỉ mang mã (uuid, số lượng), không dữ liệu cá nhân; danh
sách đơn trả về chỉ có mã đơn. 6.13 Quyền: danh sách đơn chỉ trả trong `applyStocktake` (đã đòi
`inventory.stocktake.apply` ở kho của phiên); chỉ mã đơn và số lượng, không nội dung đơn hay khách.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, đỏ trước khi có mã | `TestStockShort_*` (5 test), `TestStocktakeInvariants` | `go test -race -count=1 ./internal/modules/inventory/ -run 'TestStockShort\|TestStocktakeInvariants'` (chỉ có khai báo kiểu) | đỏ: "lấy hàng đơn 2: <nil>; mong ErrStockShort", "Consume: … violates check constraint stock_balances_on_hand_check (SQLSTATE 23514)", "qua 2, chặn 0", "xuất hàng lỗi: <nil>", rapid FAIL |
| A-01 đúng ca review: on_hand 5, giữ 2+2, đếm 2 → đơn 1 lấy, giao được; đơn 2 lấy → `ErrStockShort` (kèm mã giữ hàng, biến thể, kho, cần 2, trên kệ 0), giữ hàng vẫn held, số dư và sổ không đổi; đếm lại được (expected 0); nhập 2 → đơn 2 giao xong, mọi ngăn 0 | `TestStockShort_PickupBlockedAfterStocktake` | `go test -race -count=1 -timeout 20m ./internal/modules/inventory/ -run 'TestStockShort\|TestStocktake_\|TestPost\|TestInventoryInvariants' -rapid.checks=50` | `ok … inventory 137.697s` |
| Hai đơn lấy phần cuối song song: một qua, một `stock_short`, không lỗi khác | `TestStockShort_ConcurrentPickup` | như trên | ok |
| Lớp chặn ở bước giao: on_hand 1 < in_transit 2 → `ErrStockShort` (trên kệ 1), không CHECK, không đổi gì | `TestStockShort_ConsumeGuard` | như trên | ok |
| Xuất hàng lỗi không trừ on_hand dưới phần đang giao; `available` = 0 | `TestStockShort_DamageExportKeepsInTransit` | như trên | ok |
| Áp phiên thiếu trả người bán + mọi giữ hàng held (mã giữ hàng tăng dần, dòng đơn, số lượng), bỏ dòng đã đi giao, bỏ biến thể không thiếu; cả hai người bán | `TestStockShort_ApplyListsHeldOrders` | như trên | ok |
| Quy luật sau áp dụng (rapid 300 lượt): lấy, giao, nhả, nhập, phiếu kho mọi loại → chỉ `ErrInsufficient`/`ErrStateConflict`/`ErrStockShort`; cột ≥ 0, in_transit ≤ on_hand, sổ = số dư; đếm lại mọi biến thể được | `TestStocktakeInvariants` (+ `afterApply`) | `go test -race -count=1 -timeout 90m ./internal/modules/inventory/...` | qua (không có trong danh sách FAIL); cả gói: chỉ `TestReconcile_LargeTenant` đỏ — xem dòng cuối |
| Bộ inventory cũ không đổi hành vi (vùng rủi ro cao) | cả gói `./internal/modules/inventory/...` (gồm `TestInventoryInvariants`, `TestReserve_*`, `TestPost*`, `TestStocktake*`, deadlock) | như trên | mọi test qua trừ `TestReconcile_LargeTenant` |
| Đường order: sau kiểm kho thiếu, đơn 2 picked_up/delivered → lỗi Is `inventory.ErrStockShort`, applied=false, đơn confirmed, số dư không đổi; nhập thêm thì giao xong | `TestApplyShipmentProgress_StockShortAfterStocktake` | `go test -race -count=1 -timeout 20m ./internal/modules/order/... ./internal/modules/fulfillment/...` | `ok … order 650.811s`, `ok … fulfillment 22.452s` |
| `order.ItemOrders` lọc đúng người bán, tenant; rỗng → map rỗng | `TestItemOrders` (+ `TestCodes` sau khi gộp helper) | `go test -race -count=1 ./internal/modules/order/ -run 'TestItemOrders\|TestCodes'` | `ok … order 8.304s`; sau khi thêm `o.seller_id`: `ok 3.644s` |
| Câu đọc đơn có điều kiện người bán (ADR-29) | `tests/arch` `TestSellerFilterProductCode` | `go test -count=1 ./tests/arch/...` | lần đầu đỏ ("ItemOrders: ord.orders AS o thiếu điều kiện seller_id") → thêm `o.seller_id = @seller_id` → `ok … tests/arch 40.754s` |
| 409 `stock_short` kèm `variantId`, `available` (trên kệ), đúng 4 trường; sentinel cũng 409 | `TestDomainErrors_StockShort` | `go test -race -count=1 ./internal/httpapi/ -run 'TestDomainErrors\|TestStocktakeAPI\|TestInventoryAPI'` | `ok … httpapi 60.360s` |
| API `applyStocktake` trả `shortages[].orders` (mã đơn, mã hiển thị, số lượng, theo thứ tự giữ hàng) | `TestStocktakeAPI_ShortageListsOrders` | như trên | ok |
| Hợp đồng chỉ mở rộng; mã sinh khớp | oasdiff, `make gen` | cổng ci-task lượt 1; `make gen` rồi so `git diff` trước/sau | oasdiff qua; "gen khớp" (gen-check của ci-task đỏ chỉ vì mã sinh chưa stage) |
| Màn hình: câu cho `stock_short` (mọi mã có câu), khối thiếu hiện "Đơn đang giữ: DH… (n)" | `errors.test.ts`; e2e `KH07` (chromium + webkit) | `npx vitest run src` ; `CRM_E2E_PORT=5291 npx playwright test e2e/stocktake.phone.spec.ts -g KH07` ; `npx tsc --noEmit`, `npx eslint`, `npx prettier --write` | `Tests 436 passed`; `2 passed`; tsc, eslint sạch; prettier đã sửa định dạng `Stocktake.tsx` |
| jscpd không đoạn trùng mới | — | `jscpd … --fail-on-new-clones=0 .` | lần đầu 2 đoạn trùng trong test → gộp helper (`inTransit`, `twoSellerOrders`) → "Found 0 clones" |
| Cổng ci-task lượt 2 (sau khi stage) | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check; test gói đổi + phụ thuộc | cổng kiểm của task (ci-task), lượt 2 | mọi cổng tĩnh xanh; test: `ok` httpapi 417.7s, order 468.8s, fulfillment 59.9s, cash 561.6s, customer 528.4s, tests/arch, platform/*; đỏ chỉ do hết giờ 10 phút (catalog, identity, promotion — không chạm — và inventory, chạy đủ với `-timeout 90m` ở dòng trên) và `TestReconcile_LargeTenant` (dòng dưới) |
| Đỏ do môi trường, không do task | `TestReconcile_LargeTenant` (INSERT 50.000 số dư bằng chủ bảng hết statement_timeout 30 s); ci-task: catalog, customer, identity hết 10 phút, queue `TestStopAfterLongHandlerReleases`, e2e `migrate … signal: killed` | chạy đúng test đó trên worktree tạm của `main` | `main` cũng đỏ y hệt: "sửa thẳng bằng chủ bảng: … statement timeout (SQLSTATE 57014)"; load average máy 40–60; các gói kia task không chạm |

## Quyết định thay chủ shop

1. **Chặn ở bước lấy hàng (StartTransit), không chỉ ở Consume.** Quyết định 19/11 nói "ở bước lấy hàng/giao". Chọn:
   lấy hàng đòi `on_hand − in_transit ≥ q` (điều kiện `on_shelf` có sẵn), giao đòi `on_hand ≥ q` (lớp chặn thứ hai).
   Vì sao: giữ bất biến in_transit ≤ on_hand nên hàng đã đi giao luôn trừ được khi giao xong, và đếm lại kiểm kho không
   bao giờ chụp `expected_on_shelf` âm (phần thứ hai của A-01). Phương án khác: chỉ chặn ở Consume — đơn đã đi giao rồi
   mới bị kẹt, khó gỡ hơn.
2. **Xuất hàng lỗi cũng không trừ on_hand xuống dưới phần đang giao** (`condOnHand` → `condOnShelf` cho bước on_hand
   của phiếu `damage_export`). Đây là đường còn lại phá bất biến trên sau kiểm kho (đếm 0 khi còn hàng lỗi trên sổ).
   Số "có thể xuất" trả kèm `insufficient_stock` = min(hàng lỗi, phần trên kệ). Đảo ngược: trả lại `condOnHand`.
3. **Lệnh tra vận đơn gặp `stock_short` thử lại theo lịch, không dead ngay:** tình trạng gỡ được (nhân viên điều chỉnh
   tồn, lần thử sau qua); hết lượt thì dead + cảnh báo có sẵn. Không đổi mã fulfillment.
4. **Danh sách đơn trả trong phản hồi `applyStocktake`** (trường `orders` trong mỗi `shortages[]`: mã đơn, mã hiển thị,
   số lượng gộp theo đơn, theo thứ tự giữ hàng), chỉ đơn còn `held` (đơn đã đi giao vẫn trừ được). Màn Kiểm kho hiện
   "Đơn đang giữ: DH… (n)". Không thêm vào lệnh cảnh báo (payload chỉ mang mã phiên và số mã, A16). Mã HTTP: 409
   (cùng nhóm `insufficient_stock`), thân kèm `variantId`, `available` = phần trên kệ còn cho dòng giữ hàng.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ngoài A-01 theo đúng chữ, sửa thêm phiếu xuất hàng lỗi (`damage_export`) vì đó là đường còn lại làm on_hand < in_transit
  (xem Quyết định 2). Không migration, không đổi CHECK (giữ `on_hand ≥ 0`, `expected_on_shelf ≥ 0` làm lớp chặn cuối).

## Việc phát hiện thêm

- Lệnh tra vận đơn gặp `stock_short` chỉ thử lại theo lịch rồi dead + cảnh báo chung `dead:<kind>`; M3 (vận chuyển) nên có
  cảnh báo/màn hình riêng "đơn bị chặn lấy hàng vì thiếu tồn thực" cho nhân viên (chưa làm, ngoài phạm vi).
- Máy chạy test đang quá tải (load 40–60): nhiều gói vượt 10 phút mặc định của `go test` trong ci-task (catalog, customer,
  identity, inventory) và `TestReconcile_LargeTenant` đỏ cả trên `main` (statement_timeout khi dựng 50.000 dòng). Điều phối
  viên nên chạy lại trên máy rảnh.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lượt 1, trên phần đã stage sau khi gộp main d8ef50d).

- Phát hiện phải sửa: không có. Thứ tự khóa ADR-46 không đổi; chặn bằng câu `ApplyDelta` có điều kiện (`on_shelf` lấy
  hàng, `on_hand` giao, `on_shelf` xuất hàng lỗi) giữ bất biến in_transit ≤ on_hand; khi bị chặn bên gọi lùi cả
  transaction; số `Available` của xuất hàng lỗi đã tính tay đúng; mã sinh khớp nguồn.
- Ảnh hưởng dự kiến khớp thực tế (không chỗ gọi bị sót); mọi dòng bằng chứng trỏ tới test có thật và kiểm đúng điều nêu;
  không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc (không sửa): `checkInvariants` dừng ở thông báo đầu tiên nên khi available âm các phép kiểm sau
  nó trong cùng câu không chạy (in_transit ≤ on_hand vẫn được kiểm riêng) — có từ trước, ngoài phạm vi;
  `Shortage.SellerID` rỗng khi `Holds` rỗng (đã ghi ở chú thích); `stockShort` dùng `bs[0]` an toàn vì dòng số dư đã khóa.
