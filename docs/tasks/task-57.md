# Task 57 — M2-00: Đối chiếu Interface M0, M1 với mã thật; nghiệm thu; threat model M2

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 57 = M2-00). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu.

Mã đối chiếu: `main` tại `ca62e09` (merge Task 393). Phần M1 chưa gộp: 45, 46, 52, 53, 323, 325–331, 334, M1-55 (đóng mốc); M0-23.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task chỉ đọc mã (`go doc -all`, `grep`), không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không. Chỉ đọc `createOrder`, `getOrder`, `searchVariants`, `createUpload`, `completeUpload`, `getFile` và schema `CreateOrderRequest`, `Order`, `ShipAddress`.
- Chỗ dùng trên màn hình: không. Chỉ đọc `console/src/app/*`, `console/playwright.config.ts`, `console/e2e/fixtures.ts`.
- Câu sqlc, bảng, pha migration: không. Đọc schema thật bằng một test tạm (database thử `testkit.StartPostgres` trên PostgreSQL chung cổng 5600, tự xóa; tệp test đã xóa, không commit).
- Tệp sẽ đổi: `docs/tasks/task-57.md`, `docs/tasks/hien-tai.md` (script), `docs/m2/doi-chieu-m1.md` (mới), `docs/nghiem-thu-m2.md` (mới), `docs/threat-model-m2.md` (mới, bản đầu). Ngoài repo `apps/crm` (thư mục kế hoạch không nằm trong git): tệp kế hoạch M2, các chỗ lệch ghi ở mục "Khác kế hoạch".
- Test sẽ chạy: không có test mã mới. `make ci` trên `main` một lần (bước 2 của kế hoạch); test đọc schema tạm (`go test -run TestProbe`).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có mục tài liệu. Tìm `threat-model`, `nghiem-thu` trong `docs/`: có bản M1 (`threat-model-m1.md`, `nghiem-thu-m1.md`, Task 24), chưa có bản M2.
- Dùng lại / viết mới vì: dùng lại khuôn của Task 24 (M1-24: bảng đối chiếu, "task nào chạy được ngay", nghiệm thu, threat model STRIDE theo ranh giới). Nội dung nghiệm thu chép ý từ các mục "Xong M2 khi", "Review Focus", "Ánh xạ ca kiểm" của kế hoạch M2 (trỏ tới, không đặt tiêu chí mới).

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã.

- 5.1–5.12: không áp dụng: không có mã, bảng, route hay thư viện mới. Dùng làm căn cứ khi chọn Q-T57-1 (đường ghi là `Writer`, mẫu module M1), Q-T57-6 (một chỗ chứa luật vai trò mẫu).
- 6.1–6.13: không áp dụng cho mã của task này. Thứ tự khóa (ADR-46) dùng làm căn cứ cho Q-T57-7 và threat model mục (5).

## Kết quả đối chiếu

Bảng đầy đủ G1–G17, phần kiểm riêng và "task M2 nào chạy được ngay": `docs/m2/doi-chieu-m1.md`.

- Không dòng nào "thiếu"; không có điểm phải dừng hỏi. Mọi điểm "kiểm riêng" (`full_name`, `root_id`, `position`, cột và thứ tự `customers_v`, `orders_v`, `ord.order_code_seq`, `file_purposes`, `RegisterPurpose`, `docs/quyen.md`, `permissions_doc_test`) đều đạt.
- Hai nhánh viết sẵn không cần: `VariantsForSale` và bốn hàm theo dòng đơn của `inventory` đã có; gói đọc Excel ở `internal/platform/xlsx`.
- Khác chữ ký hay cơ chế: G2, G5 (thêm cột), G6, G8 (phương thức `Writer`, `order.Actor`, `sellerID`, thân `createOrder`), G7 (unique dòng đơn có `seller_id`, `CHECK status` đủ 7 trạng thái), G11 (`mode`, `ReadContent`), G12 (`invtest.NewShop`), G13 (màn tự đăng ký), G15 (dòng spec đã có), G16 (`SyncDefaultRoles`).
- Chạy được ngay: M2-01…31 và 33 theo bảng Phụ thuộc; phải chờ: M2-32 (cần M1-55 và M0-23).

## Quyết định thay chủ shop

Đã ghi vào kế hoạch M2, mục "Quyết định thay chủ shop", mã Q-T57-1…8 (chọn gì, vì sao, cách đổi lại). Tóm tắt:

1. Q-T57-1: hàm ghi mới của `order`, `inventory` là phương thức của `Writer` như M1. Khác: hàm gói nhận `Writer` (hai kiểu đường ghi, bỏ).
2. Q-T57-2: thêm `Type` vào `order.Actor` của M1, không khai kiểu thứ hai. Khác: kiểu `Actor` mới (trùng tên với M1, trái Q30).
3. Q-T57-3: hàm đơn mới nhận `sellerID` như M1 (ADR-29). Khác: bỏ tham số (trái ADR-29).
4. Q-T57-4: khóa nghiệp vụ tạo đơn giữ cách M1 (dựng từ `Idempotency-Key`); dùng lại `ErrDuplicateOrder`/`idempotency_key_reused`, không thêm `idempotencyKey` vào thân, không `order_key_reused`. Khác: thêm trường thân (hai khóa phải khớp nhau, thêm tên lỗi thứ hai). Dễ đảo: thêm trường tùy chọn sau không phá vỡ hợp đồng.
5. Q-T57-5: thân `createOrder` nới `customer` thành tùy chọn, thêm `customerId`. Khác: operation mới (trái Q-X2: giữ operation M1).
6. Q-T57-6: Task M2-01 không viết migration 0200; quyền M2 tới tenant cũ qua `SyncDefaultRoles` (chạy sau mỗi migrate). Khác: migration chép luật vai trò mẫu sang SQL (hai chỗ chứa luật).
7. Q-T57-7: thứ tự khóa chỉ chèn tên chưa có, không dời `returns`, `return_items`.
8. Q-T57-8: màn M2 thêm bằng thư mục `console/src/screens/<tên>/`; `console/src/features/…` trong kế hoạch đọc là `console/src/screens/…`; thành phần dùng lại giữa màn ở `console/src/shared/`.
9. Tên task do điều phối viên giao có "nghiệm thu; threat model M2", trong khi kế hoạch M2 đặt threat model ở Task M2-32. Chọn: viết **bản đầu** `docs/nghiem-thu-m2.md`, `docs/threat-model-m2.md` ngay ở task này (như Task 24 làm cho M1), Task M2-32 đổi thành "Modify: hoàn thiện". Khác: để nguyên cho M2-32 (mất cơ hội soát an toàn trước khi viết mã). Dễ đảo: hai tệp chỉ là tài liệu.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chữ ký Go thật của M1 | — | `go doc -all ./internal/modules/{order,inventory,catalog,customer,identity}`, `go doc ./internal/platform/{files,xlsx}`, `go doc ./internal/testkit`, `./internal/modules/inventory/invtest`, `./internal/modules/catalog/catalogtest` | `Writer.CreateManual(ctx, tx, in, actor Actor)`; `AttachShipment(ctx, tx, sellerID, orderID, shipmentID)`; `Writer.{Reserve,Release,StartTransit,Consume,ReleaseHeldByItems,StartTransitByItems,ConsumeByItems}`; `DefaultWarehouse`, `VariantsForSale`, `files.ReadContent`, `xlsx.Read` có |
| Cột và thứ tự view, ràng buộc, sequence (R2, R3, R5) | `TestProbe` (tạm, đã xóa) | `CRM_TEST_DATABASE_URL=…:5600… go test -count=1 -run TestProbe -v ./internal/zzprobe/` | `customer.customers_v id, root_id, full_name, created_at`; `ord.orders_v id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id, created_at`; sequence `ord.order_code_seq`; `file_purposes` `product_image originals standard`, `catalog_import private standard`; `max(version_id) = 128`; PASS |
| Thân `createOrder` | — | `sed -n 2296,2330p openapi/openapi.yaml`; `grep orderBusinessKey internal/httpapi/handlers_order.go` | `required: [customer, items, shipAddress, shippingFee]`, không `customerId`; khóa nghiệp vụ `uuid.NewSHA1(…userID…requestKey)` |
| Mã lỗi M1 | — | `sed -n 350,370p internal/httpapi/errors.go` | `ErrTotalOverflow` 422 `total_overflow`; `ErrDuplicateOrder` 422 `idempotency_key_reused`; `ErrShipmentAlreadyAttached` chưa ánh xạ |
| Vai trò mẫu đồng bộ sau migrate | — | `grep -n SyncDefaultRoles cmd/crm/main.go` | dòng 421 gọi `identity.SyncDefaultRoles(ctx, owner)` |
| Mốc jscpd | — | `python3 -c "…len(d['fingerprints'])" jscpd-baseline.json` | 0 dấu vân tay |
| `make ci` trên `main` | toàn bộ | `make ci CRM_PG_PORT=5600 CRM_S3_PORT=20600 CRM_S3_OFFSITE_PORT=21600` (trên `ca62e09` = `main`) | thoát 0: golangci-lint `0 issues.`, 47 gói `ok` dưới `go test -race`, govulncheck `No vulnerabilities found.`, jscpd không đoạn trùng mới, gitleaks `no leaks found`, console build xong |
| Kế hoạch không còn tên cũ | — | `grep -n "OrderKeyReused\|order_key_reused\|identity.AuthUser" <kế hoạch M2>` | chỉ còn trong dòng quyết định Q-T57 và dòng G8 gốc (ghi lịch sử, đoạn "Đối chiếu với mã" ngay dưới bảng nói tên thật) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch M2 đã sửa (tệp ngoài git): dòng đầu tệp; mục Quyết định (Q-T57-1…8); đoạn "Đối chiếu với mã" dưới bảng G1–G17; Task M2-01 (bỏ migration 0200, test dùng `SyncDefaultRoles`); Task M2-02 (mã lỗi M1 thật, bỏ `ErrOrderKeyReused`/`order_key_reused`, thân `createOrder` thật, `SeedCatalog` dựng trên `invtest.NewShop`); Task M2-13 (`files.ReadContent`, `xlsx.Read`, câu `INSERT` có cột); Task M2-16 (`Writer.Create`, `inventory.Writer.Reserve`, lớp bọc `CreateManual` chữ ký thật, bỏ nhánh `byitems.go`, test khóa nghiệp vụ); Task M2-18 (bốn hàm là phương thức `Writer`, nhận `sellerID`); Task M2-20 (`byitems.go` là Modify); Task M2-27–30 (màn tự đăng ký, không sửa `router.tsx`); Task M2-32 (threat model, nghiệm thu là Modify); bảng Phụ thuộc dòng 27; cây tệp (`byitems.go`).
- Thêm hai tệp mà Interface của M2-00 không ghi (`nghiem-thu-m2.md`, `threat-model-m2.md`): quyết định 9.

## Việc phát hiện thêm

- Module `customer` của M1 chưa có tệp test riêng (`go test` báo `[no test files]`); Task M2-03, M2-09 là nơi có test đầu tiên.
- `ErrShipmentAlreadyAttached` của M1 chưa có trong `internal/httpapi/errors.go` (hiện chỉ dùng trong `fulfillment`); Task M2-02 đã được giao thêm mã `shipment_already_attached`.
- Mutation testing (`.gremlins.yaml`), `docs/threat-model-m0.md` chưa có (M0-23 chưa gộp): Task M2-32 phải chờ.

## Reviewer

Kết luận: không cần (task chỉ đổi tài liệu; quy trình 4.2 bước 6, kế hoạch M2-00 Step R).
