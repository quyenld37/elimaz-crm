# Task 37 — Hai người bán trong dữ liệu thử

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 37 = M1-37, KH14, ADR-29). Loại việc
(quy trình mục 3): A

## Ảnh hưởng

Khảo sát trước khi viết test: mọi câu trong `queries/catalog`, `queries/ord` có nhắc `products`, `variants`, `orders`,
`order_items` mà không có `seller_id` trong `WHERE`/`ON`: `LockProduct`, `UpdateProduct`, `BumpProductVersion`,
`SetProductCategory`, `GetVariantProductID`, `LockVariant`, `UpdateVariant`, `LockOrder`, `SetCurrentShipment`,
`SetOrderStatus`, `OrderItemIDs`, `CategoryInUse` (và các câu `INSERT`, có `seller_id` trong danh sách cột). Hệ quả
thấy được: đường sửa mẫu, biến thể, phân loại qua API (người bán mặc định) sửa được mẫu của người bán đối tác nếu biết
mã (ghi chú reviewer Task 27 số 3). Đường ảnh đã tự kiểm người bán ở handler (`imageWrite`).

- Ký hiệu Go xuất ra ngoài sẽ đổi (thêm tham số `sellerID uuid.UUID` ngay sau `tx`; tìm chỗ dùng bằng `grep` vì phiên
  không có LSP, đã đếm từng tên):
  - `catalog.Writer.UpdateProduct` (handler + 4 chỗ trong test), `AddVariant` (handler + 2), `UpdateVariant`
    (handler + 1), `SetClassification` (handler + 1), `AttachImage` (handler + 2), `ReorderImages`, `DeleteImage`,
    `RestoreImage` (handler + 1 mỗi hàm).
  - `order.Lock` (fulfillment 2 chỗ + test 2), `order.AttachShipment` (fulfillment 1 + test 1),
    `order.Writer.ApplyShipmentProgress` (fulfillment 1 + test 1).
  - Helper test: `shiptest.Shop.Order`, `Shop.Shipment` (thêm người bán; cash, fulfillment, e2e), `invtest.NewShop`
    (người bán thứ hai đổi mã `P` → `PARTNER1`), mới `catalogtest.Sellers`, `testkit.SeedSellers`,
    `testkit.ForEachSeller`.
- Gói phụ thuộc ngược (`go list`): `catalog` ← `cmd/crm`, `internal/httpapi`, `inventory/invtest`, `order`; `order` ←
  `internal/httpapi`, `fulfillment`, `testkit/shiptest`; `testkit` ← mọi gói test có database, `tools/loadkit`;
  `shiptest` ← test của `cash`, `fulfillment`, `tests/e2e`; `invtest` ← `httpapi`, test của `inventory`, `order`,
  `shiptest`.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Hành vi đổi: `updateProduct`, `addVariant`,
  `updateVariant`, `setProductClassification` với mã của người bán khác người bán mặc định → 404 `not_found` (trước:
  sửa được). Đường ảnh đã 404 từ Task 44.
- Chỗ dùng trên màn hình: không (màn hình không biết người bán).
- Câu sqlc, bảng, pha migration: sửa 11 câu nêu trên (thêm `seller_id = @seller_id`), `GetSellerByCode` thêm
  `tenant_id = @tenant_id` (ghi chú reviewer Task 27 số 2); `make gen` sinh lại `catalog/internal/store`,
  `order/internal/store`. Không migration: index (`tenant_id, seller_id, id`) đã có (unique ghép của Task 27, 30).
  `CategoryInUse` giữ nguyên, ghi ngoại lệ (danh mục dùng chung cả shop, spec 05: không có `seller_id`).
- Test sẽ chạy: `go test -race` của `tests/arch`, `internal/modules/{catalog,inventory,order,fulfillment,cash}`,
  `internal/httpapi`, `internal/testkit/...`, `tests/e2e` (mot_don_di_tron); rồi `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chỉ gói platform, không
  có helper người bán); `catalogtest.Seller` (tạo một người bán bằng câu ghi của catalog), `invtest.Shop` (đã có
  `Partner` mã `P`), `catalog.EnsureDefaultSeller`; test kiến trúc `sqlusage_test.go` có `lineCmtRe`, `strLitRe` (bỏ
  chú thích, chuỗi trong SQL) và cách quét `queries/**` + `*_dynsql.go`; `scan_test.go` có `parseGoFiles`,
  `assertMentions`.
- Dùng lại / viết mới vì:
  - `testkit.SeedSellers` gọi `catalog.EnsureDefaultSeller` + `catalogtest.Seller` qua một hàm mới
    `catalogtest.Sellers(t, tx, tenant)` (một chỗ tạo cặp ELIMAZ/PARTNER1); `invtest.NewShop` gọi cùng hàm đó thay vì
    tự tạo người bán `P` (bỏ cách cũ).
  - `sellerfilter_test.go` dùng lại `lineCmtRe`, `strLitRe`, `parseGoFiles`, `assertMentions`, `repoRoot`; viết mới
    phần tách câu theo `-- name:` và luật bí danh + `seller_id` (luật khác `checkSQL`: nó kiểm schema, không kiểm
    điều kiện).
  - `shiptest.Shop` thêm tài khoản hãng giả của người bán đối tác (`fake-2`) thay vì test tự tạo.

## Soát thiết kế

Chỉ phần chạm: câu đọc, khóa dòng của catalog và order; test.

- 5.1–5.3: không thêm dữ liệu, không ghi bảng module khác; fulfillment vẫn gọi public API của order (`Lock`,
  `AttachShipment`, `ApplyShipmentProgress`) và truyền người bán của tài khoản hãng (đã có trong vận đơn đọc ra).
- 5.4–5.6: không đổi ranh giới transaction, sự kiện, lệnh.
- 5.7: mọi câu sửa vẫn chạy trong `tenancy.WithTenant`; thêm điều kiện `seller_id` không bỏ điều kiện tenant (RLS).
  `GetSellerByCode` thêm điều kiện `tenant_id` tường minh.
- 5.8: module không nhập HTTP; handler chỉ lấy người bán mặc định rồi truyền xuống.
- 5.9, 5.13: không thêm route; quyền giữ nguyên. Mã của người bán khác → 404 (không lộ là có tồn tại).
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12: theo module mẫu catalog (ADR-29: mọi hàm đọc nhận `sellerID`); mở rộng cho hàm sửa vì câu khóa dòng cũng là
  câu đọc.
- 6.1: khóa dòng giữ nguyên (`FOR UPDATE` theo khóa chính, thêm điều kiện `seller_id` không đổi thứ tự khóa ADR-46
  nhóm 4, 7). Version vẫn so sau khóa.
- 6.2–6.10: không áp dụng (không đổi chống trùng, thời gian, tiền).
- 6.11: quy luật hiện có (rapid) không đổi; thêm vòng hai người bán cho test luồng chính.
- 6.12: không ghi dữ liệu cá nhân mới.
- 6.13: kiểm ở API: 404 cho mẫu, biến thể của người bán khác.

## Quyết định thay chủ shop

- **Q-T37-1 Hàm sửa nhận `sellerID`.** Kế hoạch chỉ ghi "sửa câu nào thiếu điều kiện người bán". Chọn thêm tham số
  `sellerID` cho 8 hàm sửa của `catalog.Writer` và 3 hàm của `order` mà fulfillment gọi, lọc ở câu SQL dưới cùng (đúng
  ADR-29 "điều kiện ở tầng dưới cùng"). Phương án khác: giữ chữ ký, handler đọc `GetProduct` trước (như ảnh) và ghi
  ngoại lệ cho câu khóa — bỏ vì dựa vào bên gọi nhớ kiểm. Đảo ngược dễ (chỉ chữ ký Go và câu SQL, không migration).
- **Q-T37-2 Test đầu cuối với người bán đối tác.** API luôn chạy với người bán mặc định (giao diện không có khái niệm
  người bán), nên `mot_don_di_tron_test` không thể "chạy như" người bán đối tác qua HTTP. Chọn: chạy luồng đơn với dữ
  liệu có hai người bán (đối tác có mẫu cùng SKU, có tồn), kiểm tồn của đối tác không đổi, danh sách chỉ có mẫu của
  ELIMAZ, đọc mẫu đối tác → 404, lên đơn với biến thể đối tác → 422. Vòng "từng người bán" chạy ở test module (đơn,
  kho, vận chuyển, COD) gọi thẳng public API với `sellerID` của từng người. Phương án khác: thêm cách chọn người bán
  vào API — trái kế hoạch (ADR-29: chưa làm tính năng sàn).
- **Q-T37-3 Phạm vi `sellerfilter_test`.** Đúng kế hoạch: `queries/catalog`, `queries/ord` và mọi `*_dynsql.go`. Câu
  của `inventory` (lấy `seller_id` từ biến thể qua `catalog.variants_v` theo `variant_id`) không quét: kho theo biến
  thể, người bán suy ra từ biến thể (spec 05). Luật: mỗi lần nhắc bảng/view có bí danh phải có `<bí danh>.seller_id =`
  (hoặc `= <bí danh>.seller_id`); không bí danh thì `seller_id =`; `INSERT` phải có cột `seller_id`.
- **Q-T37-4 Ghi chú reviewer Task 27.** (1) sửa chú thích `DefaultSeller`, `GetDefaultSeller`: "tạo sớm nhất (id
  uuidv7 nhỏ nhất)", không phải mã; (2) `GetSellerByCode` thêm `tenant_id`; (3) làm trong Q-T37-1; (4)
  `ErrInvalidArticle` từ bài viết cũ ra 422: không đổi (không liên quan người bán, không hỏng dữ liệu; ghi ở Việc phát
  hiện thêm); (5) test gửi lại cùng khóa sau một lần 409: thêm vào test API catalog của task này.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước (Step 2): `sellerfilter_test` trên mã thật báo đúng 11 câu thiếu điều kiện người bán; test catalog, order mới không biên dịch (chữ ký chưa có `sellerID`) | `TestSellerFilterProductCode` | `go test -count=1 -run TestSellerFilter ./tests/arch/` (trước khi sửa SQL) | `FAIL` — 11 dòng: `BumpProductVersion`, `LockProduct`, `SetProductCategory`, `UpdateProduct`, `GetVariantProductID`, `LockVariant`, `UpdateVariant`, `OrderItemIDs`, `LockOrder`, `SetCurrentShipment`, `SetOrderStatus` |
| `sellerfilter_test` trên `testdata`: `SELECT … FROM catalog.variants WHERE sku = $1` → báo; join thiếu `p.seller_id`; `INSERT` thiếu cột; `ord.orders` khóa theo id; `*_dynsql.go` join `order_items_v` thiếu → báo đúng 5 dòng; câu đúng (bí danh không `AS`, `= ANY`, `variants.seller_id`, bảng ngoài phạm vi, chú thích, chuỗi) không báo | `TestSellerFilterReportsSample` | `go test -race -count=1 ./tests/arch/...` | `ok` |
| Mã thật → 0 vi phạm; dòng miễn duy nhất (`CategoryInUse`) còn được dùng | `TestSellerFilterProductCode` | như trên | `ok` |
| Cùng SKU `AO01-DO-S` (mẫu `AO01`) ở hai người bán tồn tại song song; với từng người bán: đọc, sửa mẫu, thêm/sửa biến thể, phân loại, 4 hàm ảnh bằng người bán kia → `ErrNotFound`, không đổi version; bằng chính người bán → sửa được | `TestTwoSellers_SameSKUAndIsolation` (`ForEachSeller`) | `go test -race -count=1 ./internal/modules/catalog/...` | `ok …/catalog 24.712s` |
| Đơn: tạo đơn, gắn vận đơn, áp picked_up/delivered, giao không qua lấy hàng — đạt với cả hai người bán; `Lock`, `AttachShipment`, `ApplyShipmentProgress` bằng người bán khác → `ErrNotFound`, đơn và tồn không đổi; đơn trộn biến thể hai người bán → `ErrMixedSellers` và lỗi khóa ngoại 23503 (có sẵn) | `TestCreateManual_Totals`, `TestAttachShipment`, `TestApplyShipmentProgress_Flow`, `TestApplyShipmentProgress_DeliveredWithoutPickup` (`eachSeller`), `TestLock`, `TestOrderItems_SellerFK` | `go test -race -count=1 ./internal/modules/order/...` | `ok …/order 58.520s` |
| Kho: giữ hàng, đang giao rồi trừ, phiếu nhập ghi sổ — đạt với cả hai người bán (sự kiện `stock.changed` mang đúng `sellerId`) | `TestReserve_HoldsAndRecords`, `TestTransitThenConsume`, `TestPostImport_IncreasesOnHandAndLedger` (`eachSeller`) | `go test -race -count=1 ./internal/modules/inventory/...` | `ok …/inventory 92.986s` |
| Vận chuyển: tạo vận đơn → awaiting_pickup → shipping → delivered, webhook cùng eventId 3 lần — đạt với cả hai người bán (mỗi người tài khoản hãng riêng); tài khoản hãng của người bán khác → `ErrOrderNotShippable`, không vận đơn | `TestShipment_HappyPath`, `TestRefresh_SameEventThreeTimes`, `TestShipment_AccountOfOtherSeller` (`eachSeller`) | `go test -race -count=1 ./internal/modules/fulfillment/...` | `ok …/fulfillment 17.598s` |
| COD: shipping rồi delivered, tổng sổ 0 — đạt với cả hai người bán | `TestConsumerShippingThenDelivered` (`eachSeller`) | `go test -race -count=1 ./internal/modules/cash/...` | `ok …/cash 33.658s` |
| API: mẫu, biến thể PARTNER1 → 404 `not_found` ở `getProduct`, `updateProduct`, `addVariant`, `updateVariant`, `getClassification`, `setProductClassification`, `reorderProductImages`, `deleteProductImage`; dữ liệu PARTNER1 không đổi; ELIMAZ tạo `AO01` với SKU trùng của PARTNER1 → 201; `listProducts` chỉ 1 mẫu; đơn trộn → 422 `mixed_sellers` (có sẵn) | `TestCatalogAPI_PartnerSellerIs404`, `TestOrderAPI_Refusals` | `go test -race -count=1 -run 'TestCatalogAPI_PartnerSellerIs404\|TestCatalogAPI_RetrySameKey\|TestOrderAPI' ./internal/httpapi/` | `ok …/httpapi 6.861s` |
| Ghi chú reviewer Task 27 số 5: 409 rồi gửi lại cùng khóa với version đúng → 200 | `TestCatalogAPI_RetrySameKeyAfterConflict` | như trên | `ok` |
| Đầu cuối với dữ liệu hai người bán (Q-T37-2): luồng đơn qua 3 vai trò + hãng giả đạt; tồn PARTNER1 7 không đổi, sổ = số dư; danh sách chỉ mẫu ELIMAZ (`listedAvailable`: `GET /api/v1/catalog/products?warehouseId=MAIN` đòi đúng 1 mẫu, gọi sau khi mẫu PARTNER1 đang bán đã có); đọc mẫu PARTNER1 → 404; lên đơn biến thể PARTNER1 → 422 `mixed_sellers` | `TestMotDonDiTron` | `go test -race -count=1 -v -run '^TestMotDonDiTron$' ./tests/e2e/` | `--- PASS: TestMotDonDiTron (1.90s)` |

| Cổng kiểm toàn bộ trên phần đã stage (nền main `ff2046d`) | build, vet, golangci-lint, gen-check, sqlc vet, `go test -race ./...`, govulncheck, test kiến trúc, e2e | `make ci` | lint `0 issues.`; mọi gói `ok` (arch, e2e 51.986s, httpapi 60.115s, catalog, order, inventory, cash, fulfillment); `No vulnerabilities found.`; bước oasdiff báo `regenerateImages` "bị xóa" vì main đã tiến tới `cdccbf9` (Task 322 thêm đường này) sau lần gộp — xem dòng dưới |
| Bản gộp với main mới `cdccbf9` (Task 322) | build, vet, sinh mã, apicheck, lint, test của arch, httpapi, files, catalog | worktree tạm từ `main` + `git apply --index` diff đã stage; `go build ./... && go vet ./...`; `make gen` + `git diff --exit-code`; `go run ./tools/apicheck -base-ref=main …`; `golangci-lint run ./...`; `go test -race ./tests/arch/... ./internal/httpapi/... ./internal/platform/files/... ./internal/modules/catalog/...` | build, vet sạch; sinh mã không chênh; apicheck không báo; `0 issues.`; mọi gói `ok` |

## Gộp main

`git merge main` chạy trước khi stage, lúc main là `ff2046d` ("Already up to date"). Sau đó main tiến lên `cdccbf9`
(Task 322). Gộp lần nữa không làm được trong phiên: `git merge` đòi index sạch, cổng commit chặn commit tạm khi chưa có
dấu duyệt, và hook không cho chạm `docs/tasks/hien-tai.md` (main cũng đổi tệp này). Task 322 không chạm tệp mã nào
của task này (chỉ `files`, `handlers_files.go`, mã sinh, `openapi.yaml`, `main.go`) và không gọi hàm nào đã đổi chữ ký
(`git grep` trên `main`). Bản gộp đã kiểm ở worktree tạm (dòng cuối bảng bằng chứng). Điều phối viên gộp như thường lệ.

## Test bị xóa hoặc tắt

(không có). Test sửa: `TestCreateManual_Totals` so tên dòng đơn bằng mẫu `Áo thử - Đỏ / S<n>` thay vì đúng `S1` (lượt
người bán thứ hai dùng biến thể số khác trên cùng database); `TestShipment_HappyPath`, `TestRefresh_SameEventThreeTimes`,
`TestTransitThenConsume`, `TestPostImport…`, `TestConsumerShippingThenDelivered` đếm lệnh, sự kiện, bút toán theo
vận đơn/ngăn số dư của lượt thay vì đếm cả bảng; số kiện hãng giả so với trước lượt. Không bỏ câu kiểm nào.

## Khác kế hoạch

- `ForEachSeller` theo đúng chữ ký kế hoạch; mỗi module thêm `eachSeller` (dựng một database, chạy thân test lần lượt
  với từng người bán) vì mã người bán chỉ có sau khi dựng database.
- Người bán thứ hai của `invtest.Shop` đổi mã `P` → `PARTNER1` (cùng `catalogtest.Sellers`); `shiptest.Shop` có thêm
  tài khoản hãng `fake-2` của PARTNER1; `Shop.Order`, `Shop.Shipment` nhận người bán.

## Việc phát hiện thêm

- Ghi chú reviewer Task 27 số 4 (`ErrInvalidArticle` khi bài viết đã lưu hỏng ra 422): không sửa ở đây (không liên
  quan người bán); để task sửa bài viết sau nếu cần.
- `fulfillment.Get`, `GET /api/v1/shipments/{id}` không lọc người bán (bảng `shipments` không có `seller_id`, suy ra
  qua tài khoản hãng); ngoài phạm vi luật của kế hoạch (chỉ `products`, `variants`, `orders`, `order_items`). Khi mở
  tính năng sàn nên xét.
- Chống trùng của lệnh tra trạng thái dùng khóa `evt:<eventId>` theo tenant, không kèm tài khoản hãng: hai hãng/tài
  khoản trùng eventId trong một tenant sẽ bị gộp. Test phát hiện khi chạy hai người bán (đã đổi eventId trong test);
  với hãng thật eventId duy nhất theo hãng, nhưng M3 (nhiều hãng) nên thêm carrier/account vào khóa.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lượt 2, trên `git diff --cached`).

- Lượt 1: CẦN SỬA, một phát hiện. Bằng chứng e2e ghi "danh sách chỉ mẫu ELIMAZ" mà reviewer không thấy câu kiểm.
  Thực tế câu kiểm đã có: `listedAvailable` đòi đúng 1 mẫu, gọi sau khi mẫu PARTNER1 đã tạo. Đã sửa dòng bằng chứng
  và thêm chú thích ở test. Phần SQL và Go không có phát hiện: 11 câu chỉ thêm `seller_id`, không mất điều kiện cũ;
  thứ tự khóa ADR-46 không đổi; fulfillment truyền đúng người bán của tài khoản hãng; không còn chỗ gọi chữ ký cũ.
- Lượt 2: không có phát hiện phải sửa. Reviewer kiểm lại lập luận gộp main: 33 tệp của Task 322 không trùng tệp nào
  trong 50 tệp đã stage; không dùng chữ ký đã đổi; không thêm `*_dynsql.go`.
- Ghi chú không bắt buộc (không sửa):
  1. Luật sellerfilter còn hai chỗ lỏng. Với bảng không có bí danh, chỉ cần một `seller_id =` ở bất kỳ đâu trong câu
     là đạt. Tên bảng không ghi schema thì không được quét. Chưa có câu thật nào rơi vào hai trường hợp này; nên siết
     lại khi có `*_dynsql.go` dùng các bảng đó.
  2. Một số chú thích dài quá 120 ký tự (lint không bắt).
  3. Mục "Gộp main" có thể liệt kê thêm `errors.go`, `console/src/app/errors.ts`, `docs/danh-muc-dung-chung.md`,
     `queries/platform/files.sql` của Task 322. Kết luận không đổi.
