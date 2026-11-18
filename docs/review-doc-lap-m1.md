# Review độc lập toàn M1 (Task 404, M1-55c)

Ngày 19/11/2026. Năm reviewer mới (agent riêng, chỉ có spec 2.1, kế hoạch M1, threat model và mã; không có lập luận của
người viết, không đọc tệp task hay commit message làm bằng chứng), chia năm vùng: A kho, B sản phẩm, C kho tệp và ảnh,
D quyền và lát mỏng một đơn đi trọn, E chéo toàn M1 (ca nghiệm thu → test, tên mốc sau dựa vào, threat model, quyền
màn hình, ảnh hưởng của M2). Bản được soát: main `a3f7be3` (M1 task 24–56, 315, 319–334, 389–401; M2 tới Task 72).
Lời giao chung ở cuối tệp. Chi tiết từng vùng (mô tả, bằng chứng, đề xuất, mục "Đã kiểm, không thấy lỗi") ở phần Phụ lục.

**Tổng:** 0 nghiêm trọng, 9 lớn, 20 nhỏ, 4 nghi ngờ (33 phát hiện). 4 phát hiện về tính đúng đã sửa trong Task 404,
mỗi sửa có test; phần còn lại tách thành task bù 406–415 (bảng số task toàn cục).

**Kết luận về M2:** M2 không tạo lại bảng M1, không sửa view `_v` (chỉ thêm cột cuối), không đổi chữ ký hàm Go xuất ra
của M1 (Q1), không đổi thứ tự khóa ADR-46; hai test một đơn đi trọn vẫn đạt sau khi gộp M2. Bốn điểm M2 cần lưu ý (A-03,
D-04, D-05, D-06) gom vào Task 415.

**Ca nghiệm thu:** mọi tên test mà bảng "Tiêu chí → ca kiểm" của kế hoạch M1 nêu đều có trong mã (C10); 33 test Go
chạy lại đều đạt, 362 ca vitest màn hình đạt. Các ca chỉ chạy tay hoặc SKIP trên máy dev (TP03 bom điểm ảnh, TP08, TP09,
TP10, TP01 với imager thật, phần OOM của tiêu chí 7) ghi ở E-04.

## Bảng phát hiện

| Mã | Mức | Tệp:dòng chính | Việc | Xử lý |
|---|---|---|---|---|
| A-01 | lớn | internal/modules/inventory/reserve.go:133-140 | Kiểm kho cho on_hand < phần giữ (Q8) → giao xong (Consume) và ghi số đếm lỗi CHECK, đơn kẹt | Task 406 (cần chủ shop chọn hành vi, đụng Q8) |
| A-02 | nhỏ | migrations/0115_inventory.sql:129 | crm_app INSERT mọi cột stock_balances, stock_reservations | Task 414 |
| A-03 | nghi ngờ | migrations/0204_order.sql:227-232 | M2: order_item_id NULL, ON DELETE SET NULL → giữ hàng mồ côi nếu sau này xóa dòng đơn | Task 415 (ghi cho M2) |
| B-01 | lớn | internal/modules/catalog/imports_apply.go:27-29 | Tệp nhập > ~3.000 biến thể vượt hạn 30 s ở mọi lượt, không bao giờ áp được | Task 407 |
| B-02 | lớn | internal/modules/catalog/internal/article/validate.go:24 | Bài viết sát trần 200 KB lưu được nhưng đọc lại jsonb (có dấu cách) vượt trần → mẫu bị khóa | **Đã sửa** (test `TestArticle_NearLimitReadsBack`) |
| B-03 | nhỏ | internal/modules/catalog/imports.go:201 | Bước kiểm không bắt barcode gõ tay trùng biến thể có sẵn ngoài tệp; áp thì failed với câu báo sai | **Đã sửa** (test `TestImport_BarcodeTakenInDatabase`) |
| B-04 | nhỏ | queries/catalog/barcode.sql | Lần áp giữ khóa dòng bộ đếm barcode tới hết transaction | Task 413 |
| B-05 | nhỏ | internal/modules/catalog/products.go:73-139 | Mẫu nháp trong danh mục đã lưu trữ vẫn sang active được | Task 413 |
| B-06 | nhỏ | internal/modules/catalog/classification.go:539-549 | Tên danh mục có `>`, giá trị thuộc tính có `;` tạo được nhưng nhập Excel sai, trùng khóa đường dẫn | Task 413 |
| B-07 | nhỏ | internal/queries/products_dynsql.go:252-258 | Từ khóa rộng cắt 1.000 mẫu không thứ tự rồi mới sắp → thiếu mẫu vừa sửa | Task 413 |
| C-01 | lớn | docker/compose.prod.yml:173-189 | imager tới được cổng filer/master/volume không xác thực của SeaweedFS | Task 410 (hạ tầng, cần OK) |
| C-02 | lớn | internal/platform/files/process.go:388-396, 600-621 | Lỗi tạm của imager tiêu hết 3 lượt → ảnh hợp lệ thành rejected vĩnh viễn | Task 409 (đổi chính sách Q22, cần chủ shop) |
| C-03 | lớn | docker/compose.prod.yml:110 | Compose máy chủ bật cảnh báo chép kho nhưng không có dịch vụ filesync | Task 410 (hạ tầng, cần OK) |
| C-04 | nhỏ | internal/platform/files/register.go:36-47 | `files.Register` nhận mục đích ảnh standard → ảnh ready chưa qua imager | **Đã sửa** (ca mới trong `TestRegister_Refusals`) |
| C-05 | nhỏ | cmd/imager/main.go:26; tests/arch/boundaries_test.go:122 | Tệp chạy imager vẫn liên kết config, db, pgx qua observability; luật kiến trúc chỉ xét nhập trực tiếp | Task 414 |
| C-06 | nhỏ | scripts/filesync/lib.sh:20 | filesync và luật cảnh báo viết cứng tên bucket `crm-…` | Task 410 |
| C-07 | nhỏ | internal/platform/files/upload.go:25 | Trạng thái `processing` không bao giờ được ghi | Task 409 |
| C-08 | nghi ngờ | docs/do-luong/seaweedfs-m1.md:17 | Chưa xác minh ảnh thô trong crm-tmp thật sự bị xóa sau 24 giờ (R10) | Task 410 |
| D-01 | lớn | internal/modules/fulfillment/fulfillment.go:134-166 | Vận đơn tạo bằng tài khoản hãng của kho khác kho giữ hàng → tồn trừ sai kho, vượt phạm vi kho | **Đã sửa** (test `TestShipment_AccountOfOtherWarehouse`) |
| D-02 | nhỏ | internal/httpapi/handlers_order.go:51-53 | Lên đơn giữ hàng ở kho bất kỳ, không kiểm phạm vi kho | Task 411 (cần quyết định quyền) |
| D-03 | nhỏ | internal/modules/fulfillment/create_shipment.go:151-196 | Lệnh tạo vận đơn rejected/expired/dead để vận đơn kẹt creating, đơn không tạo lại được | Task 408 |
| D-04 | nhỏ | queries/customer/customers.sql | `customer.Create` của M1 không ghi `name_search`, `lifecycle` M2 thêm | Task 415 (ghi cho M2) |
| D-05 | nghi ngờ | internal/modules/order/shipment_progress.go:90-96 | Đơn `cancel_requested` (M2) làm bước vận đơn của M1 lỗi mãi | Task 415 (ghi cho M2) |
| D-06 | nghi ngờ | queries/ord/orders.sql (`SetCurrentShipment`) | Gắn vận đơn tăng version đơn mà không phát sự kiện | Task 415 (ghi cho M2) |
| E-01 | nhỏ | console/src/screens/inventory/DocumentEdit.tsx:100-108 | Màn phiếu kho bỏ qua phạm vi kho của quyền | Task 412 |
| E-02 | nhỏ | console/src/screens/imports/route.tsx:7,12 | Màn Nhập Excel thiếu điều kiện `files.upload`; màn Kiểm kho thiếu `inventory.read` | Task 412 |
| E-03 | nhỏ | openapi/openapi.yaml:764; internal/modules/identity/staff.go:189,246 | Tên lệch kế hoạch: `getOriginalImageURL`; `ReplaceGrants`, `SetLocked` trả `(Change, error)` | Task 415 (sửa kế hoạch theo mã, xem Quyết định) |
| E-04 | nhỏ | internal/imager/pipeline_test.go:148-158 | Ca TP03, TP08, TP09, TP10, TP01 imager thật, OOM tiêu chí 7 không chạy ở CI, SKIP trên máy dev | Task 414 |
| E-05 | nhỏ | docs/threat-model-m1.md:70,125-132 | Lỗ hổng L1 (kiểm loại biến thể, nosniff), L3 (log không có URL ký) chưa task nào làm | Task 414 |
| E-06 | nhỏ | console/src/screens/staff/staffRules.ts:69-75 | Màn Nhân viên khóa gán vai trò mà máy chủ cho phép (quyền `none`) | Task 412 |
| E-07 | lớn | console/src/screens/products/productRules.ts:80-89 | Người có `inventory.read` toàn shop không thấy cột "Có thể bán" | Task 412 |
| E-08 | nhỏ | console/e2e/imports.spec.ts:25-50 | Mock e2e màn Nhập Excel khác câu trả lời thật (tên cột, mã lỗi) | Task 412 |

## Task bù (ghi vào `docs/superpowers/plans/danh-so-task-toan-cuc.md`)

| Số | Tên | Phát hiện | Ghi chú |
|---|---|---|---|
| 406 | Kiểm kho thiếu hàng dưới phần giữ: gửi, giao không lỗi CHECK | A-01 | Cần chủ shop chọn hành vi (Q8): Consume trừ tối đa phần còn và ghi lệch, hay chặn lúc lấy hàng |
| 407 | Nhập Excel lớn: trần dòng của tệp nhập theo thời gian áp, hoặc áp theo lô | B-01 | Tối thiểu: `too_many_rows` lúc kiểm theo trần đo được |
| 408 | Lệnh tạo vận đơn kết thúc hỏng: vận đơn creating → cancelled, gỡ vận đơn hiện hành | D-03 | `OnTerminal` như catalog; không đổi chữ ký M1 |
| 409 | Ảnh: lỗi tạm của imager không tiêu lượt thành rejected vĩnh viễn; trạng thái processing | C-02, C-07 | Đổi chính sách Q22 cho file.process, cần chủ shop |
| 410 | Hạ tầng kho tệp: cô lập SeaweedFS khỏi imager, dịch vụ filesync trên máy chủ, tiền tố bucket, đo TTL crm-tmp | C-01, C-03, C-06, C-08 | Đổi docker/compose: cần OK hạ tầng |
| 411 | Lên đơn: kiểm phạm vi kho của kho giữ hàng | D-02 | Cần quyết định quyền trong docs/quyen.md |
| 412 | Màn hình khớp quyền máy chủ: phạm vi kho, quyền tải tệp, quyền `none`, cột Có thể bán, mock e2e nhập Excel | E-01, E-02, E-06, E-07, E-08 | |
| 413 | Catalog: khóa bộ đếm barcode khi áp, danh mục lưu trữ, ký tự `>`/`;`, thứ tự từ khóa rộng | B-04, B-05, B-06, B-07 | |
| 414 | Gia cố: GRANT cột số dư, imager không liên kết db/config, ca chạy tay có lịch chạy, L1/L3 threat model | A-02, C-05, E-04, E-05 | |
| 415 | Ghi cho M2 và kế hoạch: giữ hàng mồ côi, khách M1 thiếu cột M2, cancel_requested, version đơn; đồng bộ tên kế hoạch | A-03, D-04, D-05, D-06, E-03 | |

## Sửa trong Task 404

- **B-02** `internal/modules/catalog/internal/article/validate.go`: trần 200 KB chỉ kiểm trên dạng JSON gọn (`Check`);
  byte thô chỉ chặn ở `2 × MaxRawBytes` trước khi giải mã (jsonb đọc ra có dấu cách sau `:` và `,`).
- **B-03** `queries/catalog/imports.sql` (`ListVariantSKUsByBarcodes`), `imports_validate.go` (`existingBarcodes`),
  `importer.Refs.ExistingBarcodes`: bước kiểm nạp biến thể có sẵn theo barcode gõ tay của tệp.
- **C-04** `internal/platform/files/register.go`: mục đích ảnh (kể cả standard) → `ErrPurposeMode`.
- **D-01** `internal/modules/fulfillment/fulfillment.go`: tài khoản hãng phải cùng kho với kho giữ hàng của đơn, khác →
  `ErrOrderNotShippable` (404 như tài khoản của người bán khác; không thêm mã lỗi mới vào hợp đồng).

## Phụ lục

### Review M1 — vùng A (kho / inventory)

Phạm vi: `internal/modules/inventory`, migrations 0115/0116/0121/0122, `queries/inventory/**`,
`internal/httpapi/handlers_inventory.go`, `handlers_stocktake.go`, `internal/queries/balances_dynsql.go`.

##### A-01: Kiểm kho giảm on_hand xuống dưới phần giữ → về sau giao xong (Consume) và ghi số đếm lỗi CHECK, đơn kẹt
- Mức: lớn
- Tệp:dòng: internal/modules/inventory/reserve.go:133-140 (startTransit `{bucketInTransit, 1, condNone}`, consume `{bucketOnHand, -1, condNone}`); internal/modules/inventory/stocktake.go:278-281, 135; migrations/0115_inventory.sql:35 (`on_hand int NOT NULL DEFAULT 0 CHECK (on_hand >= 0)`); migrations/0121_stocktakes.sql (`expected_on_shelf int NOT NULL CHECK (expected_on_shelf >= 0)`)
- Spec/kế hoạch: Q8 / KH07 ("ghi chênh lệch, không chặn, **không đụng phần giữ hay đơn**"); 05 mục 2 vòng đời giữ hàng held → in_transit → consumed.
- Mô tả: Q8 cho phép kiểm kho làm on_hand < held (điều kiện `on_shelf` chỉ đòi on_hand − in_transit ≥ |delta|). Nhưng các bước sau của giữ hàng giả định on_hand ≥ held + in_transit: StartTransit tăng in_transit không điều kiện, Consume trừ on_hand với `condNone`. Ca cụ thể (đúng dữ liệu của `TestStocktake_ShortageKeepsHolds`): on_hand 5, giữ 2+2, đếm 2 → on_hand 2, held 4. Hãng lấy cả hai đơn (StartTransit) → held 0, in_transit 4, on_hand 2. Giao xong đơn 1 → on_hand 0, in_transit 2. Giao xong đơn 2 → `on_hand = 0 − 2` vi phạm CHECK `on_hand >= 0` → lỗi 23514 thô (không phải ErrStateConflict/ErrInsufficient), `order.shipment_progress.go:118` trả lỗi "trừ tồn khi giao xong", đơn không bao giờ sang delivered (webhook hãng thử lại mãi / 500). Cùng gốc: khi on_hand < in_transit, `RecordCount` chụp `expected = on_hand − in_transit` âm → vi phạm CHECK `expected_on_shelf >= 0` → không mở đếm lại được biến thể đó (500) — tức là không thể sửa bằng kiểm kho tiếp. Chỉ còn phiếu nhập/điều chỉnh tăng mới gỡ được.
- Bằng chứng: **đã chạy** test tạm qua `go test -overlay` (không tạo tệp trong W; tệp test ở scratchpad `review/ov/a01_test.go`): `go test -count=1 -overlay …/overlay.json ./internal/modules/inventory/ -run TestReviewA01 -v` →
  ```
  sau kiểm: OnHand:2 Held:4 InTransit:0 Available:-2
  sau gửi:  OnHand:2 Held:0 InTransit:4 Available:-2
  RecordCount khi on_hand<in_transit: ghi dòng đếm: ERROR: ... violates check constraint "stocktake_lines_expected_on_shelf_check" (SQLSTATE 23514)
  sau giao 1: OnHand:0 Held:0 InTransit:2
  Consume 2: đổi số dư on_hand: ERROR: ... violates check constraint "stock_balances_on_hand_check" (SQLSTATE 23514)
  ```
  Đọc mã: `order/shipment_progress.go:111-119` gọi `StartTransitByItems` rồi `ConsumeByItems`. Test hiện có không đi tiếp sau áp dụng: `TestStocktake_ShortageKeepsHolds` dừng sau khi kiểm `held` còn nguyên; `TestStocktakeInvariants` chỉ xen thao tác **trước** áp dụng (stocktake_property_test.go:55-80, apply ở cuối lượt).
- Đề xuất sửa: quyết định nghiệp vụ cho trạng thái "âm" (ví dụ Consume trừ on_hand tối đa phần còn và ghi lệch vào sổ/cảnh báo, hoặc StartTransit đòi `on_hand − in_transit ≥ q` và trả lỗi nghiệp vụ rõ "thiếu hàng thật" cho đơn đó; RecordCount kẹp expected về `max(0, …)` hoặc bỏ CHECK); thêm test: kiểm kho thiếu → gửi → giao xong cả hai đơn. Cỡ: nhỏ ở mã, nhưng cần chủ shop chọn hành vi (đụng Q8).

##### A-02: crm_app được INSERT mọi cột của stock_balances và stock_reservations (lớp DB không giữ "số dư chỉ đổi qua sổ")
- Mức: nhỏ
- Tệp:dòng: migrations/0115_inventory.sql:129 (`GRANT INSERT ON inventory.stock_balances, inventory.stock_ledger, inventory.stock_reservations TO crm_app;`)
- Spec/kế hoạch: ADR-07 ("số dư là bảng dẫn xuất"), Global Constraints M1 (sổ chỉ ghi thêm, quyền DB là lớp chặn thứ hai); comment đầu 0115 "Số dư chỉ đổi bằng một câu UPDATE có điều kiện".
- Mô tả: UPDATE đã giới hạn cột, nhưng INSERT thì không: tiến trình ứng dụng có thể chèn dòng số dư với `on_hand = 1000` (hoặc dòng giữ hàng `state = 'consumed'`) mà không có dòng sổ. Mã hiện tại chỉ chèn qua `EnsureBalance` (số 0) và `InsertReservation` (state mặc định) nên không có lỗi đang chạy; chỉ là lớp DB không chặn được lỗi tương lai (đối chiếu hằng đêm sẽ báo, nhưng sau một đêm).
- Bằng chứng: 0115 dòng 129–131; `queries/inventory/balances.sql` EnsureBalance chỉ chèn `(tenant_id, variant_id, warehouse_id, seller_id)`; `reservations.sql` InsertReservation không có `state`.
- Đề xuất sửa: migration mới `REVOKE INSERT … ; GRANT INSERT (tenant_id, variant_id, warehouse_id, seller_id) ON inventory.stock_balances`, `GRANT INSERT (id, tenant_id, order_item_id, variant_id, warehouse_id, quantity, expires_at) ON inventory.stock_reservations` + test "crm_app chèn on_hand ≠ 0 → 42501". Cỡ: nhỏ.

##### A-03: M2 (0204) cho phép order_item_id NULL với ON DELETE SET NULL — DB không bảo đảm chỉ dòng giữ hàng `released` mất liên kết
- Mức: nghi ngờ (ghi cho M2; M1 hiện chưa có đường xóa dòng đơn)
- Tệp:dòng: migrations/0204_order.sql:227-232; internal/modules/inventory/byitems.go (ReservationIDsByItems tìm theo order_item_id)
- Spec/kế hoạch: 05 mục 2 B2 ("giữ hàng chỉ đổi trạng thái theo chuyển trạng thái của đơn"), docs/views.md dòng 59 ("sửa đơn bỏ được dòng **đã nhả hàng**").
- Mô tả: 0204 đổi giả định của M1-28/30 (`order_item_id NOT NULL`, mỗi giữ hàng thuộc một dòng đơn có thật). Nếu đường sửa đơn của M2 (Task M2-20) xóa một `ord.order_items` còn giữ hàng `held`/`in_transit`, FK đặt `order_item_id = NULL`: các hàm `…ByItems` không tìm thấy dòng đó nữa, `held` kẹt mãi (đối chiếu vẫn khớp nên không báo). Điều kiện "chỉ released" chỉ nằm trong comment, không có trigger/CHECK. Hiện chưa có câu `DELETE FROM ord.order_items` nào trong mã (grep rỗng), nên chưa phải lỗi đang chạy.
- Bằng chứng: `grep -rn "DELETE FROM ord.order_items" queries internal` → rỗng; 0204 dòng 225–232; crm_app hiện chưa có quyền DELETE trên `ord.order_items` (0117:139 chỉ SELECT, INSERT; 0204 không GRANT DELETE).
- Đề xuất sửa: khi làm M2-20, thêm trigger BEFORE DELETE trên `ord.order_items` (hoặc kiểm trong cùng tx) từ chối khi còn giữ hàng không `released`/`consumed`, kèm test. Cỡ: nhỏ.

#### Ghi về M2 so với giả định của inventory M1
- M2 không đổi chữ ký hàm công khai nào của `inventory` (`git diff` các merge 60–63, 90 trên `internal/modules/inventory`, `queries/inventory`: chỉ mã sinh `models.go` và `@order_item_id::uuid` trong `InsertReservation` để nhận NULL). Không CREATE lại bảng hay sửa view `inventory.*_v`.
- `0204_order.sql` thêm `ord.orders.warehouse_id` → FK `inventory.warehouses` (đọc kho, không khóa) và nới `stock_reservations.order_item_id` (A-03). Thứ tự khóa đơn → tồn giữ nguyên (`order/shipment_progress.go`: khóa đơn, rồi `StartTransitByItems`/`ConsumeByItems` tự khóa số dư trước giữ hàng).

#### Đã kiểm, không thấy lỗi
- **Bán vượt khi đồng thời / chiếc cuối:** `ApplyDelta` là một câu UPDATE có điều kiện `on_hand − damaged − held − in_transit ≥ q`, READ COMMITTED đánh giá lại sau khi chờ khóa; không đọc ra Go rồi ghi lại. Có test `TestReserve_LastItem`, `TestReserve_FiftyOnTen`, `TestReserve_PartialShortageRollsBack`, thuộc tính `TestInventoryInvariants` (1–4 goroutine, gồm phiếu kho mọi loại).
- **Thứ tự khóa so với `docs/adr/0046-thu-tu-khoa.md` nhóm 11:** Reserve sắp theo `(variant_id, warehouse_id)` rồi chèn giữ hàng (dòng mới); transition khóa số dư (`LockBalancesOfReservations … ORDER BY … FOR UPDATE`) trước, rồi giữ hàng theo id; PostDocument khóa phiếu (UPDATE có điều kiện) → dòng phiếu (trigger FOR SHARE phiếu) → số dư theo variant_id (một kho); ApplyStocktake khóa phiên → số dư theo variant_id; RecordCount khóa phiên, đọc số dư không khóa. Đơn (nhóm 7) khóa trước tồn (nhóm 11). `EnsureBalance` (`ON CONFLICT DO NOTHING`) không khóa dòng đã có, chạy theo cùng thứ tự. Có `TestReserve_NoDeadlockOnCrossOrder`, `TestCore_NoDeadlockOnCrossOrder`.
- **RLS/tenant:** mọi bảng mới `platform.enable_tenant_rls`; unique đều bắt đầu bằng `tenant_id`; khóa ngoại ghép `tenant_id`; tenant của dòng mới lấy từ dòng cha qua RLS (kho, phiếu, phiên); `InsertReconcileRun` lấy `app.tenant_id`; view `security_invoker`. Có `TestTenantIsolation`.
- **sum() rỗng:** chỉ `ReconcileBatch` dùng `sum` — đều `coalesce(…, 0)`; `ListStocktakeLines` coalesce khi chưa có dòng số dư.
- **Hàm trong RetryTx không cộng dồn:** `Writer` không giữ trạng thái; `refusableWrite`/`WriteTx` gán lại `out, refused, result` mỗi lần; Reserve/ApplyStocktake/Reconcile tạo biến kết quả trong thân hàm; lỗi nghiệp vụ trả `final=false` → transaction lùi (PostDocument thiếu hàng không để lại phiếu `posted`).
- **Tiền int64:** `unit_cost bigint` CHECK 0…2^53−1, Go `*int64`, kiểm `maxMoney`.
- **Q7 tách create/post:** handler `CreateStockDocument`/`ReplaceStockDocumentLines` cần `inventory.documents.create` (adjust cần thêm `post`), `PostStockDocument` chỉ `post`, kiểm theo kho của phiếu đọc trong cùng tx; vai trò mẫu `warehouse` có create, không có post (`identity/roles.go:55`, `docs/quyen.md:69-70`). Ghi sổ đòi đúng `version` nên người duyệt ghi đúng bản đã xem.
- **Q8 không đụng phần giữ:** ApplyStocktake chỉ đổi `on_hand` (điều kiện `on_shelf`, chặt hơn kế hoạch `on_hand + delta ≥ 0` nhưng tương đương "số đếm + thay đổi sau lúc đếm ≥ 0"); held, in_transit, giữ hàng không đổi; có Shortages + lệnh `inventory.shortage_alert` chỉ mang mã. (Hậu quả về sau: A-01.)
- **Sổ chỉ ghi thêm thật:** trigger theo câu lệnh chặn UPDATE/DELETE/TRUNCATE mọi role; `REVOKE ALL … FROM PUBLIC`, crm_app chỉ SELECT, INSERT; unique bước chống ghi hai lần; test `TestLedger_AppendOnly` (crm_app → 42501, chủ bảng → append-only).
- **Phiếu kho:** trigger chỉ cho sửa dòng khi phiếu nháp; post song song một bên thắng (`TestPost_ConcurrentOnce`, `TestInventoryAPI_PostIdempotent`); phiếu nhiều dòng thiếu một dòng → lùi hết; xuất không ăn phần giữ (KH10); nhà cung cấp chỉ cho phiếu nhập (CHECK + Go).
- **Kiểm kho:** một phiên mở mỗi kho (unique một phần); áp dụng hai lần song song một bên thắng; version phiên tăng mỗi lần ghi số đếm nên áp đúng bản đã xem; KH05 (`TestStocktake_InTransitNotAdjusted`, `TestStocktake_DeliveredAfterCount`).
- **Đối chiếu đêm (KH13):** mỗi lô một câu (một snapshot cho số dư, sổ, giữ hàng của cùng dòng) nên không báo lệch giả; ba phép so đúng 05 mục 2; chỉ đọc; lịch 19:30 UTC chèn ở 0122; mismatches chỉ mã và số.
- **`balances_dynsql.go`:** tham số hóa đầy đủ (không nối chuỗi người dùng), lọc `seller_id` + `warehouse_id`, quyền kho kiểm ở handler (`canAt` trước khi đọc); `OnlyShort` = `available < 0`.

### Review vùng B — sản phẩm (catalog)

Tổng: 0 nghiêm trọng, 2 lớn (B-01, B-02), 5 nhỏ (B-03 đến B-07), 0 nghi ngờ. Phạm vi: main tại a3f7be3.

#### Phát hiện

##### B-01: Tệp nhập hợp lệ (≤ 20.000 dòng) nhưng trên ~3.000–4.000 biến thể không bao giờ áp được: việc áp một transaction vượt hạn 30 s ở mọi lượt
- Mức: lớn
- Tệp:dòng: internal/platform/xlsx/limits.go:13 (MaxRows 20_000); internal/modules/catalog/imports_apply.go:27-29 (`queue.DefaultPolicy`, hạn 30 s); imports_validate.go:202 (kiểm dùng `xlsx.DefaultLimits()`)
- Spec/kế hoạch: Task 323 "thời gian áp … quá 30 s là vượt giới hạn handler → tách lô, báo"; Q10 tất cả-hoặc-không.
- Mô tả: Bước kiểm nhận tệp tới 20.000 dòng và cho `ready` + nút áp. Việc `catalog.import.apply` ghi cả plan trong một transaction có hạn 30 s, ~6,3 ms/biến thể (mỗi biến thể qua Writer: sự kiện + audit + báo hiệu). Tệp 5.000 dòng → mỗi lượt hết hạn, lùi, thử lại 8 lượt giãn tới hàng giờ → dead → `failed` không lỗi dòng nào. Nhân viên thấy "Sẵn sàng" lúc xem trước rồi chờ hàng giờ và nhận "failed" không lý do. Không mất dữ liệu (lùi sạch) nhưng tính năng hỏng với tệp mà bước kiểm tuyên bố hợp lệ.
- Bằng chứng: chính tệp `docs/tasks/task-323.md:88` đo: "Áp một transaction: 2 phút 7 s → vượt giới hạn 30 s" cho 20.000 dòng; dòng 105 ghi việc treo, đề xuất (a) hạ trần dòng. Trên main hiện tại `MaxRows` vẫn 20_000 và không có trần riêng cho nhập sản phẩm (grep `MaxRows` chỉ thấy limits.go/read.go).
- Đề xuất sửa: trần riêng cho tệp nhập sản phẩm (ví dụ 3.000 dòng, `Limits{MaxRows: …}` ở `check`) báo `too_many_rows` lúc kiểm, kèm test; hoặc đưa vào quyết định của chủ shop nếu chưa chốt. cỡ: nhỏ

##### B-02: Bài viết gần trần 200 KB lưu được nhưng đọc lại từ jsonb thì vượt trần → mẫu không đọc, không sửa được nữa
- Mức: lớn
- Tệp:dòng: internal/modules/catalog/internal/article/validate.go:24 (`len(trimmed) > MaxRawBytes` trên byte thô); internal/modules/catalog/products.go:193-197 (`productFromRow` → `DecodeArticle(row.Article)`); products.go:113 (`newProduct` lưu bản `json.Marshal` gọn)
- Spec/kế hoạch: Task 27 (giới hạn bài viết 200 KB; sửa mẫu có version); Q34.
- Mô tả: Lúc ghi, giới hạn 200 KB được kiểm trên JSON gọn (đầu vào và `json.Marshal` trong `Check`). PostgreSQL lưu `jsonb` và trả về dạng văn bản có dấu cách sau mỗi `:` và `,` (`[{"type": "list", "items": ["a", "b"]}]`). Mọi đường đọc (`GetProduct`, `lockProduct` trong `UpdateProduct`, `AddVariant`, `SetClassification`, ảnh…) gọi `DecodeArticle` → `Validate` kiểm lại `len(raw) > MaxRawBytes` trên chuỗi đã nở → `ErrInvalidArticle`. Mẫu đó thành không mở được, không sửa được bài viết (vì `UpdateProduct` khóa mẫu bằng `productFromRow` trước), không thêm/sửa biến thể, không phân loại được — chỉ sửa được bằng SQL tay. Cửa sổ lỗi: bài viết toàn đoạn văn ~600 byte dưới trần; bài viết nhiều danh sách tới ~10 KB dưới trần.
- Bằng chứng: đo bằng psql trên database test: 200 khối list × 50 mục × 16 ký tự → JSON gọn 195.201 byte (≤ 204.800, lưu được) nhưng `jsonb::text` 205.800 byte (> 204.800): `with b as (select jsonb_agg(jsonb_build_object('type','list','items',(select jsonb_agg(repeat('x',16)) from generate_series(1,50)))) j from generate_series(1,200)) select octet_length(j::text), octet_length(replace(replace(j::text, ', ', ','), ': ', ':'))` → `205800|195201`. psql cũng xác nhận dạng `[{"type": "list", "items": ["a", "b"]}]`.
- Đề xuất sửa: không kiểm cỡ byte thô khi đọc từ database (để `Check` — vốn tự `json.Marshal` gọn — giữ giới hạn), hoặc so cỡ sau khi nén lại; thêm test lưu bài viết sát trần rồi `GetProduct`. cỡ: nhỏ

##### B-03: Bước kiểm không bắt barcode gõ tay trùng biến thể có sẵn ngoài tệp → xem trước "ready" nhưng áp thì failed với câu báo sai
- Mức: nhỏ
- Tệp:dòng: internal/modules/catalog/internal/importer/validate.go:311-353 (`checkSKUs` dựng `barcodeSKU` chỉ từ `refs.ExistingSKUs`); internal/modules/catalog/imports.go:192-201 (`existingRefs` chỉ nạp biến thể có SKU nằm trong tệp); imports_apply_write.go:242-246 (câu báo lúc áp)
- Spec/kế hoạch: KH01 "dòng sai báo rõ từng dòng"; Task 47 xem trước; chú thích `checkSKUs` "barcode gõ tay … không thuộc biến thể khác".
- Mô tả: Tệp có dòng mới SKU `X1` với Barcode `8930000000017` trùng barcode của biến thể `Y9` đang có (SKU `Y9` không nằm trong tệp). `ExistingSKUs` không có `Y9` nên `checkBarcode` không thấy; bản nhập thành `ready`, xem trước báo tạo được. Lúc áp, `InsertVariant` dính `variants_barcode_key` → `ErrBarcodeTaken` → cả bản nhập `failed` với câu "Barcode vừa được dùng cho biến thể khác **sau khi kiểm tệp**" — sai sự thật (đã trùng từ trước), nhân viên phải tải lại mà bước kiểm vẫn sẽ cho qua lần nữa. Không mất dữ liệu (tất cả-hoặc-không giữ được).
- Bằng chứng: `existingRefs` gọi `ListVariantsBySKUs(... Skus: skus)` với `skus` từ tệp; `checkSKUs`: `for sku, ex := range v.refs.ExistingSKUs { if ex.Barcode != "" { barcodeSKU[ex.Barcode] = sku } }` — không có nguồn nào khác cho barcode có sẵn.
- Đề xuất sửa: trong `existingRefs` nạp thêm biến thể theo các barcode gõ tay có trong tệp (`barcode = ANY(@barcodes)`, có seller_id) và đưa vào `barcodeSKU`; thêm ca test. cỡ: nhỏ

##### B-04: Việc áp bản nhập giữ khóa dòng `catalog.barcode_counters` (một dòng mỗi tenant) tới hết transaction → mọi thao tác tạo biến thể barcode tự sinh bị chặn suốt lần áp
- Mức: nhỏ
- Tệp:dòng: queries/catalog/barcode.sql (`NextBarcodeNumber`: `ON CONFLICT (tenant_id) DO UPDATE`); internal/modules/catalog/variants.go:166-172 (`insertVariant` → `freeBarcode` → `barcode.NextEAN13`); imports_apply.go:239-246 (cả plan ghi trong một transaction)
- Spec/kế hoạch: ADR-46 nhóm 4 (barcode_counters cuối nhóm); Task 323 (mục tiêu áp < 20 s, hạn 30 s).
- Mô tả: Biến thể đầu tiên không có barcode trong lần áp khóa dòng bộ đếm của tenant; khóa giữ tới commit (4,5 s cho 1.000 biến thể theo số đo của task, tới 30 s với tệp lớn). Trong lúc đó mọi `CreateProduct`/`AddVariant` qua API với barcode trống chờ dòng này; `lock_timeout` 5 s → lỗi tạm. Ngoài ra có vòng chờ thật: lần áp đang giữ bộ đếm rồi chèn mẫu mã `X` trong khi một yêu cầu API đã chèn (chưa commit) mẫu mã `X` và đang chờ bộ đếm → deadlock (PostgreSQL hủy một bên; nếu là lần áp thì lỗi không phải stepError → cả việc chạy lại). Thứ tự khóa ADR-46 bị vi phạm theo nghĩa: sau khi giữ `barcode_counters`, lần áp còn lấy khóa unique của `products`/`variants` mới.
- Bằng chứng: đọc mã như trên; `docs/tasks/task-323.md:70` ghi thời gian áp 4,46 s; `internal/platform/db/db.go:79` đặt `lock_timeout` mặc định.
- Đề xuất sửa: cấp trước một dải số barcode cho cả plan ở đầu lần áp (một câu `UPDATE … SET next = next + n RETURNING`) rồi dùng dần, hoặc chấp nhận và ghi vào threat model/ADR-46; cỡ: nhỏ

##### B-05: Luật "danh mục lưu trữ không còn mẫu active" chỉ chặn ở phía lưu trữ — mẫu nháp trong danh mục đã lưu trữ vẫn chuyển sang active được
- Mức: nhỏ
- Tệp:dòng: queries/catalog/categories.sql:52-56 (`CategoryInUse` chỉ đếm mẫu `active`); internal/modules/catalog/classification.go:249-266 (`ArchiveCategory`); internal/modules/catalog/products.go:73-139 (`UpdateProduct` đổi `status` không xem danh mục)
- Spec/kế hoạch: Task 34 "lưu trữ `Váy` khi còn mẫu active → category_in_use"; chú thích `ArchiveCategory` "Danh mục đã lưu trữ không gán được cho mẫu".
- Mô tả: Mẫu nháp M gán danh mục Váy → lưu trữ Váy thành công (mẫu nháp không chặn) → `PATCH status=active` cho M thành công: kết quả là mẫu đang bán nằm trong danh mục đã lưu trữ, đúng trạng thái mà luật lưu trữ muốn tránh (M5 dựng kệ theo danh mục active sẽ mất mẫu này hoặc hiện danh mục đã ẩn). Cũng xảy ra theo kiểu đua: `ArchiveCategory` đọc `CategoryInUse` lúc M còn nháp trong khi một transaction khác đang đổi M sang active (UpdateProduct không khóa chia sẻ danh mục). Nhập Excel đổi trạng thái mẫu có sẵn cũng đi qua `UpdateProduct` nên cùng lỗ.
- Bằng chứng: `UpdateProduct` chỉ gọi `lockProduct`, `newProduct`, `q.UpdateProduct` — không có `shareLockActiveCategory`/kiểm `category_id`; `CategoryInUse`: `p.status = 'active' AND p.deleted_at IS NULL`.
- Đề xuất sửa: khi `UpdateProduct` đổi sang `active` và mẫu có `category_id`, khóa chia sẻ danh mục (đúng thứ tự ADR-46: danh mục trước mẫu — tức đọc category_id rồi khóa danh mục trước `lockProduct`, kiểm lại sau) và từ chối nếu đã lưu trữ; hoặc ghi rõ luật chỉ một chiều. cỡ: nhỏ

##### B-06: Tên danh mục chứa ">" và giá trị thuộc tính chứa ";" được tạo nhưng không nhập Excel đúng được; hai danh mục có thể trùng khóa đường dẫn
- Mức: nhỏ
- Tệp:dòng: internal/modules/catalog/classification.go:539-549 (`categoryText` chỉ `validLine`); internal/modules/catalog/imports.go:155-162 (`refs.Categories[importer.CategoryPath(names...)] = c.ID` — ghi đè khi trùng khóa); internal/modules/catalog/internal/importer/validate.go:185-199 (`strings.Split(s, ">")`), 201-209 (`splitList` tách `;`)
- Spec/kế hoạch: Task 38 (danh mục dạng `Váy > Váy dài`, nhiều giá trị cách bằng `;`).
- Mô tả: Danh mục gốc tên `Áo > Sơ mi` và danh mục `Sơ mi` con của `Áo` cùng ra khóa `"Áo > Sơ mi"`; map giữ cái duyệt sau → tệp nhập chọn "Áo > Sơ mi" có thể gán mẫu vào sai danh mục mà không báo lỗi. Giá trị thuộc tính `Cotton; lụa` không bao giờ khớp (bị tách thành hai giá trị) → `unknown_attribute_value` dù chọn đúng.
- Bằng chứng: trích ở trên; không chỗ nào cấm `>` trong tên danh mục hay `;` trong giá trị thuộc tính.
- Đề xuất sửa: cấm `>` trong tên danh mục và `;` trong giá trị thuộc tính/mã nhãn lúc tạo, sửa (ErrInvalidInput), hoặc phát hiện khóa trùng trong `classificationRefs`. cỡ: nhỏ

##### B-07: Lọc danh sách mẫu theo từ khóa rộng lấy 1.000 mẫu khớp bất kỳ (không thứ tự) rồi mới sắp theo `updated_at` → thiếu mẫu vừa sửa, trang sau không nhất quán
- Mức: nhỏ
- Tệp:dòng: internal/queries/products_dynsql.go:252-258 (`p.product_id IN (SELECT catalog.search_products(..., 1000))`); migrations/0123_catalog_search.sql (`search_products` … `LIMIT least(greatest(lim, 0), 1000)` không `ORDER BY`)
- Spec/kế hoạch: Task 39/390 (danh sách mẫu tìm theo tên, trang theo con trỏ updated_at); chú thích ProductFilter "tối đa 1.000 mẫu khớp".
- Mô tả: Khi một từ khóa khớp hơn 1.000 mẫu (Task 390 tự nêu "dam" khớp 2.000 mẫu), tập 1.000 mẫu được chọn tùy kế hoạch thực thi, không phải 1.000 mẫu sửa gần nhất. Trang 1 có thể không có mẫu vừa sửa; mỗi trang tính lại tập con nên trang 2 có thể lặp hay bỏ mẫu so với trang 1. Màn hình không báo kết quả bị cắt.
- Bằng chứng: thân `search_products` là ba nhánh `UNION` rồi `LIMIT`, không `ORDER BY`; `productsSQL` sắp `ORDER BY p.updated_at DESC` sau khi đã cắt.
- Đề xuất sửa: cho `search_products` trả mẫu theo `updated_at DESC, id DESC` (có index `products_list_updated`) trước khi cắt, hoặc nhận con trỏ; ít nhất báo "kết quả bị cắt" cho màn hình. cỡ: nhỏ

#### M2 có làm hỏng giả định của catalog không

Không thấy. Các commit M2 chạm vùng catalog chỉ là `internal/modules/catalog/internal/store/models.go` (sqlc sinh lại vì có bảng mới), `catalogtest/seed.go` (hàm dựng dữ liệu test). Migration 02xx (`0201`, `0202`, `0204`, `0205`, `0290`, `0291`) không CREATE lại, không ALTER bảng hay view `catalog.*`; ADR-46 nhóm 4 không đổi; `catalog.VariantForSale`, `VariantsForSale`, `DefaultSeller` giữ chữ ký, module `order` (M2) gọi đúng với `in.SellerID`. Lưu ý cho M5: `catalog.products_v.article` trả jsonb dạng có dấu cách — bên đọc qua `DecodeArticle` sẽ dính B-02.

#### Đã kiểm, không thấy lỗi

- Chiều người bán (ADR-29): mọi câu đọc/khóa/sửa trong `queries/catalog/{products,variants,imports}.sql` có `seller_id`; câu không có seller (`GetImportForValidation`, `LockImport`, liên kết phân loại và ảnh theo `product_id`) chỉ chạy sau khi đã khóa/đọc mẫu theo seller hoặc trong việc nền theo mã bản nhập. Khóa ngoại ghép `(tenant_id, seller_id, product_id)` của variants, `(tenant_id, seller_id)` của products và imports. Handler luôn dùng `DefaultSeller` của tenant người gọi; test `TestCatalogAPI_PartnerSellerIs404`, `TestTwoSellers_SameSKUAndIsolation`.
- Version/khóa dòng: `UpdateProduct`, `AddVariant`, `SetClassification`, ảnh: `LockProduct … FOR UPDATE` rồi so version; `UpdateVariant`: đọc product_id (không đổi được vì không có GRANT UPDATE) → khóa mẫu → khóa biến thể → so version; `UPDATE … WHERE version = @version` dự phòng. `ApplyImport` khóa dòng bản nhập rồi so trạng thái, version.
- Thứ tự khóa ADR-46 nhóm 4 trên đường tay: danh mục (FOR SHARE) → mẫu → biến thể → bộ đếm barcode; `UpdateCategory` khi đổi cha khóa cả cây theo id tăng dần trước. Đường áp: khóa tư vấn → imports → danh mục (chia sẻ, tăng dần) → mẫu có sẵn (tăng dần) → ghi (ngoại lệ ở B-04).
- Nhập lại cùng tệp (KH01): SHA-256 nội dung, `FindAppliedImportBySHA` lúc kiểm và lại lúc áp dưới khóa tư vấn theo người bán; unique một phần `(tenant_id, seller_id, file_sha256) WHERE status='applied'`; hai bản cùng tệp đều ready → bản sau duplicate (`TestApply_SameFileBothReady`). Tệp khác byte cùng nội dung: SKU có sẵn → update/unchanged, unique SKU chặn trùng.
- Tất cả hoặc không: cả plan trong một savepoint; lỗi nghiệp vụ → lùi savepoint, `failed` + một lỗi dòng; lỗi khác → lùi cả transaction, chạy lại; `Recheck` sau khi khóa; dời tạm màu/size (`moveAside`) cho đổi chỗ, đổi dây chuyền; test `TestApply_AllOrNothingProperty`, `TestApply_WriteErrorRollsBackEverything`, `TestApply_TimeoutMidWriteRetries`; `OnTerminal` đặt `failed` cho dead/expired; lệnh kiểm `rejected` → dead → `failed`.
- Ghi theo điều kiện trạng thái: `FinishValidation`, `FailValidation`, `StartApply`, `FinishApply`, `FailApply`, `MarkApplyDuplicate` đều có `WHERE status = …`; `oneRow` chặn chạm 0 dòng; CHECK của bảng giữ quan hệ status với sha, plan, duplicate_of, applied_*.
- An ninh Excel: trần tệp 5 MiB, tổng giải nén theo header ≤ 50 MiB và giải nén thử từng phần (đúng số byte, CRC) trước excelize; trần tổng ô 10 triệu trên mọi phần XML; từ chối phần trùng tên, trang đầu mơ hồ; `UnzipSizeLimit`, `UnzipXMLSizeLimit`; `recover` quanh excelize; công thức tìm bằng quét thẻ `<f>` (cả công thức dùng chung) → `formula_not_allowed`; lỗi excelize không bọc nguyên văn. `Write`/`Sanitize`: mọi ô là chữ, thêm dấu nháy trước `= + - @`, tab, CR, LF; bỏ ký tự XML không hợp lệ; cắt 32.767 không giữa ký tự; danh sách chọn ở trang ẩn `_lists`.
- SQL động `products_dynsql.go`: mọi giá trị qua `$n`; chỉ ghép hằng (`Limit` int đã kiểm 1–100, trạng thái theo danh sách trắng). Hàm `SECURITY DEFINER` `search_variants`, `search_products`: `search_path` cố định, tự lọc `tenant_id = app.tenant_id` (NULL ngoài ngữ cảnh) và `seller_id` ở mọi nhánh kể cả LATERAL; thoát ký tự đặc biệt của LIKE; EXECUTE chỉ cho `crm_app`.
- RLS: `enable_tenant_rls` cho sellers, products, variants, barcode_counters, categories, attributes, attribute_values, tags, product_attribute_values, product_tags, imports; view `security_invoker`; GRANT UPDATE theo cột (không có `tenant_id`, `seller_id`, `product_id`, `deleted_at`); `crm_app` không ghi `sellers`.
- R7 `RenderArticleHTML`: khuôn cố định, chữ qua `html/template` (thoát theo ngữ cảnh, `src`/`srcset` lọc URL), `article.Check` chạy lại phòng thủ, URL ảnh chỉ https (http chỉ localhost), không userinfo, không ký tự tách srcset; khối image chỉ trỏ tệp `product_image` ready của tenant (`checkArticleFiles`); `Block.Image` có thẻ `json:"-"`.
- Giá: int64 đồng trong [0, 2^53−1] ở Go, CHECK ở database, bộ đọc Excel (nhận `350000`, `350.000`; từ chối `350,5`, số âm, vượt trần).
- Ngừng bán (KH12): `variants_v.sellable` = mẫu và biến thể active, mẫu chưa xóa; `VariantForSale` trả `ErrNotForSale`; đơn M2 kiểm qua `VariantsForSale` (`TestCreateOrder_DiscontinuedVariant`, `handlers_order_test` ca KH12).
- KH14: đơn trộn người bán bị chặn ở Go và khóa ngoại database (`TestOrderItems_SellerFK`, `tests/e2e/mot_don_di_tron_test.go`).
- Tìm biến thể: barcode đúng cả chuỗi, SKU/mã mẫu tiền tố theo index text_pattern_ops, tên trigram; 0129 cắt từng nhóm trước khi nối (Task 390).
- Bộ đếm barcode EAN-13 tiền tố 2 tăng trong một câu; trùng barcode gõ tay → sinh lại tối đa 5 lần.
- `tools/catalog` là bộ sinh `docs/danh-muc-dung-chung.md` (không liên quan sản phẩm): chỉ đọc mã nguồn, ghi một tệp — không có vấn đề tính đúng.
- Ca KH01 có đủ test theo kế hoạch Task 47/323 (200 mẫu, 7 lỗi dòng, cùng tệp hai lần, 3 giá đổi, SKU bị lấy giữa chừng, hai lần áp song song, 50 yêu cầu cùng Idempotency-Key, thuộc tính 100 lượt).


### Review độc lập M1 — vùng C: kho tệp và ảnh

Phạm vi đã đọc: `internal/platform/files/*` (mọi tệp không phải test), `internal/imager/*`, `cmd/imager/main.go`,
`queries/platform/files.sql`, `queries/platform/file_sync.sql`, `queries/catalog/images.sql`, migration 0124–0126,
`internal/modules/catalog/{images.go,image_original.go,article_html.go}` (phần ảnh), `internal/httpapi/handlers_files.go`,
`cmd/crm/main.go` (nối lệnh), `internal/platform/commands/{runner.go,indoubt.go}` và `queries/platform/{commands,jobs}.sql`
(để hiểu not_sent/in_doubt), `scripts/filesync/*`, `docker/{imager,filesync,seaweedfs}/*`, `docker/compose.{dev,prod}.yml`,
`tests/arch/boundaries_test.go`, `docs/do-luong/{imager-m1,seaweedfs-m1}.md`; kế hoạch Task 40–46, 320–322, Q11–Q22, Q38.

Test đã chạy: `go test -race -count=1 ./internal/platform/files/...` → `ok elimaz.com.vn/crm/internal/platform/files 80.209s`.

Tổng: 0 nghiêm trọng, 3 lớn, 4 nhỏ, 1 nghi ngờ.

---

##### C-01: imager tới được cổng nội bộ không xác thực của SeaweedFS (master, volume, filer) — ảnh độc chiếm được imager là đọc được bucket riêng tư và xóa vĩnh viễn mọi bản
- Mức: lớn
- Tệp:dòng: docker/compose.prod.yml:173 (`"server", "-dir=/data", "-ip.bind=0.0.0.0", "-s3", …`), docker/compose.prod.yml:178 (`networks: [default, imaging]`), docker/compose.prod.yml:189 (imager `networks: [imaging]`); tương tự docker/compose.dev.yml:76–79
- Spec/kế hoạch: ADR-45 (imager là chỗ giải mã dữ liệu lạ, cô lập: "chỉ ra mạng tới kho tệp" qua đường dẫn ký sẵn), ADR-36 ("ứng dụng không xóa vĩnh viễn được tệp nào"), R10, Review Focus 3 và 4 của kế hoạch M1.
- Mô tả: SeaweedFS chạy `weed server` một container (master 9333, volume 8080, filer 8888, S3 8333) với `-ip.bind=0.0.0.0` và gắn vào mạng `imaging`, cùng mạng với imager. Chỉ cổng S3 có kiểm danh tính (`s3.json`); filer, master, volume không có `security.toml`/JWT. Mối đe dọa mà ADR-45 dựng imager để chặn chính là lỗi giải mã của libvips bị khai thác (RCE trong tiến trình con chạy uid 65534 trong container imager). Khi đó kẻ tấn công gọi thẳng filer HTTP (ví dụ `GET`/`DELETE http://seaweedfs:8888/buckets/crm-private/...?recursive=true`), vượt qua danh tính S3: đọc ảnh riêng tư của khách và tệp Excel trong `crm-private`, ảnh gốc trong `crm-originals`, và xóa cả thư mục phiên bản (`.versions`), tức là xóa vĩnh viễn. Cách cô lập hiện có chỉ được test cho PostgreSQL và internet (`TestImagerCompose_CannotReachPostgres`, `TestImagerCompose_NoInternet`), không test cho các cổng nội bộ của kho.
- Bằng chứng: lệnh `command` ở compose.prod.yml:173 không có `-filer=false`, không có cấu hình bảo mật; `grep -i "8888\|9333\|filer\|security.toml\|jwt"` trên threat-model-m1.md, compose, compose_test.go không ra mục nào xử lý việc này; docs/do-luong/seaweedfs-m1.md:4 ghi rõ "`weed server` một container: master, volume, filer, S3".
- Đề xuất sửa: đặt SeaweedFS chỉ nghe S3 trên mạng `imaging` (ví dụ một proxy chỉ chuyển cổng 8333 nằm ở `imaging`, còn SeaweedFS chỉ ở `default`), hoặc bật JWT của SeaweedFS (`security.toml`) cho filer, volume, master; thêm test compose: từ container imager, nối `seaweedfs:8888`, `:9333`, `:8080` → thất bại. Cỡ: nhỏ (compose + một test), nhưng phải đo lại Task 40.

##### C-02: Lỗi tạm (imager bận, không tới được, quá giờ) tiêu hết 3 lượt và biến ảnh hợp lệ thành `rejected processing_failed` vĩnh viễn; chạy lại lệnh dead không cứu được
- Mức: lớn
- Tệp:dòng: internal/platform/files/upload.go:56–60 (`MaxAttempts = 3`), internal/platform/files/process.go:388–396 (busy, unreachable → `notSent`), internal/platform/files/process.go:600–621 (`OnTerminal` dead → `processing_failed`), internal/platform/files/process.go:236–237 (`load`: `StatusRejected` → trả `rejected` ngay), internal/imager/client.go:56–60 (chờ chỗ trống tính vào hạn 30 s của lệnh); queries/platform/jobs.sql:89 (`attempts = u.attempts + 1` mỗi lần nhận việc), queries/platform/commands.sql:11 (`attempts < max_attempts`)
- Spec/kế hoạch: Q22 (việc chạy lại an toàn: `AutoRetryNotSent`, `not_sent` khi lỗi tạm); ADR-45/tiêu chí 7 (ảnh treo không làm hỏng việc khác); spec 04 mục 5.3 (lệnh dead do lỗi tạm thì người có quyền chạy lại).
- Mô tả: mọi `not_sent` (kể cả `busy` và `imager_unreachable`) tính một lượt vì lượt tăng lúc nhận việc. Với lịch thử lại 5 s, 30 s (queue/retry.go), imager ngừng quá khoảng 35 s (deploy lại imager, container bị OOM rồi khởi động lại) là mọi ảnh đang chờ đi hết 3 lượt → `dead` → `OnTerminal` ghi tệp `rejected processing_failed`. Tương tự khi tải nhiều ảnh: worker nhận 8 việc (Concurrency 8) nhưng client chỉ cho 2 việc tới imager; việc thứ 7–8 chờ 3 vòng, mỗi ảnh thật đo được 4–10,5 s (docs/do-luong/imager-m1.md) → quá 30 s → `busy`, mất lượt; `file.regenerate` xếp cả kho ảnh cùng lúc làm tình huống này dễ xảy ra với ảnh nhân viên vừa tải. Khi tệp đã `rejected`, chạy lại lệnh dead bằng `POST /api/v1/platform/jobs/{id}/retry` vô ích vì `load` trả ngay `rejected(jb.row.RejectReason)`; nhân viên phải tải lại ảnh. Test `TestFileProcess_ManyImagesNoBusyRetry` chỉ dùng imager giả trễ 300 ms nên không bắt được ca này.
- Bằng chứng: process.go:388–396 `if be, isBusy := errors.AsType[*imager.BusyError](err); isBusy { return res, notSent("busy", be.RetryAfter), false }` … `return res, notSent("imager_unreachable", 0), false`; process.go:620 `return p.markRejected(ctx, tx, *j.TenantID, payload.FileID, reasonProcessingFailed)`; process.go:236 `case jb.row.Status == StatusRejected: return jb, rejected(jb.row.RejectReason.String), false`.
- Đề xuất sửa: phân biệt lỗi do nội dung với lỗi hạ tầng: `processing_failed` không coi là kết luận vĩnh viễn (`load` cho xử lý lại tệp `rejected` có lý do `processing_failed`, hoặc OnTerminal để tệp `uploaded` và cảnh báo), và/hoặc chờ chỗ trống của imager trước khi nhận việc (không tính lượt khi `busy`); thêm test imager tắt 60 s rồi bật lại → ảnh vẫn `ready`. Cỡ: nhỏ–lớn (đổi chính sách, cần chủ shop đồng ý vì đổi kế hoạch).

##### C-03: Compose máy chủ bật cảnh báo chép kho ngoài nhưng không có dịch vụ filesync
- Mức: lớn
- Tệp:dòng: docker/compose.prod.yml:110 (`FILESYNC_EXPECTED: "true"`), cả tệp không có dịch vụ `filesync`
- Spec/kế hoạch: Task 45 (container `crm-filesync` chép mỗi giờ, TP10), ADR-36 (mất tối đa ~1 giờ), Task 46/TP09 (dựng lại từ bản sao), Điểm dừng M1-D (staging).
- Mô tả: trên staging/production, worker tính luật `file-sync-age` (gấp) nhưng không có tiến trình nào chép kho sang kho ngoài và ghi `platform.file_sync_runs`. Kết quả: không có bản sao ngoài máy cho ảnh và tệp riêng tư (TP09, TP10 không đạt trên máy thật), còn cảnh báo gấp kêu liên tục ngay từ lúc dựng. Kế hoạch cho phép để trống kho thật tới khi chủ shop tạo tài khoản, nhưng khi đó phải đặt `FILESYNC_EXPECTED=false` hoặc có dịch vụ cùng biến chờ.
- Bằng chứng: `grep -n -i filesync docker/compose.prod.yml` → chỉ dòng 110; `grep -i filesync docker/compose.prod.env.example` → không có gì.
- Đề xuất sửa: thêm dịch vụ `filesync` (image dựng ở CI, user 65534, `read_only`, `/tmp` tmpfs, volume `/state`, biến `FILESYNC_*` không có khóa `crm-app`) vào compose.prod.yml và env.example, hoặc để `FILESYNC_EXPECTED` theo biến môi trường kèm ghi chú RUNBOOK. Cỡ: nhỏ.

##### C-04: `files.Register` nhận mục đích ảnh chế độ standard → tệp ảnh `ready` chưa qua imager
- Mức: nhỏ
- Tệp:dòng: internal/platform/files/register.go:36–47
- Spec/kế hoạch: Task 41 Interfaces ("để không có tệp `ready` nào chưa qua `imager`"), ADR-45 ("ảnh chỉ được giải mã trong imager"), Global Constraints "Tệp".
- Mô tả: `Register` chỉ chặn `p.Private`. Mốc sau gọi `Register` với `product_image` (hay một mục đích `Image` standard mới) sẽ tạo dòng `ready` không có `content_sha256`, không có biến thể, nội dung chưa được kiểm loại thật. `catalog.AttachImage` chấp nhận tệp đó (chỉ kiểm `ready` + `product_image`, images.go `checkImageFile`), ảnh hiện thành "lost"; `DownloadURL` trả `ErrNotReady`; `file.verify` bỏ qua (mainTargets trả nil khi thiếu mã băm). Hiện chưa có bên gọi nào trong mã (đã grep `files.Register(`), nên là lỗ API cho M2–M7.
- Bằng chứng: register.go:37–42 `switch { case !ok: … case p.Private: return …ErrPurposeMode … }` — không có nhánh `p.Image`.
- Đề xuất sửa: `case p.Image:` → `ErrPurposeMode` (ảnh từ máy chủ phải qua `RegisterPrivateImage` hoặc một hàm đăng ký ảnh standard có xếp `file.process`); thêm ca vào `TestRegister_Refusals`. Cỡ: nhỏ.

##### C-05: Luật kiến trúc (g) chỉ xét lần nhập trực tiếp; tệp chạy imager vẫn liên kết `config`, `db`, pgx, migrations
- Mức: nhỏ
- Tệp:dòng: cmd/imager/main.go:26 (nhập `internal/platform/observability`), tests/arch/boundaries_test.go:122 và :178 (chỉ duyệt `p.Imports`)
- Spec/kế hoạch: Global Constraints M1 ("`internal/imager` không nhập … `internal/platform/{db,tenancy,config,idempotency}`, pgx"), ADR-45 (imager không nối database, không cấu hình bí mật).
- Mô tả: `cmd/imager` dùng `observability.CheckLive` cho healthcheck; gói `observability` kéo theo `internal/platform/config`, `internal/platform/db`, pgx, pgxpool và `migrations`. Không có đường nào mở kết nối lúc chạy, nhưng luật mà test kiến trúc tuyên bố đang không được giữ ở mức tệp chạy, và test không bắt được vì chỉ xét lần nhập trực tiếp.
- Bằng chứng: `go list -deps ./cmd/imager | grep -E "elimaz|pgx"` → có `internal/platform/config`, `internal/platform/db`, `github.com/jackc/pgx/v5`, `github.com/jackc/pgx/v5/pgxpool`, `elimaz.com.vn/crm/migrations`, `internal/platform/observability`.
- Đề xuất sửa: tách `CheckLive` ra một gói nhỏ chỉ dùng `net/http` (hoặc viết healthcheck ngay trong `cmd/imager`); cho luật (g) xét `go list -deps` của `cmd/imager`. Cỡ: nhỏ.

##### C-06: filesync và luật cảnh báo viết cứng tên bucket `crm-…`, bỏ qua `S3_BUCKET_PREFIX`
- Mức: nhỏ
- Tệp:dòng: scripts/filesync/lib.sh:20 (`FILESYNC_BUCKETS="crm-public crm-originals crm-private"`), internal/platform/observability/rules.go:321 (`syncedBuckets`)
- Spec/kế hoạch: Global Constraints biến `S3_BUCKET_PREFIX` (mặc định `crm-`), Task 45.
- Mô tả: ứng dụng đặt tên bucket thật là `S3_BUCKET_PREFIX + bucket` (s3store.go:73), còn filesync chép và cảnh báo xét đúng ba tên `crm-*`. Đặt tiền tố khác (staging dùng chung một kho với production, như biến cho phép) thì filesync chép nhầm hoặc lỗi; cảnh báo theo dõi tên không ai dùng. Kho ngoài thì đã có `FILESYNC_DST_BUCKET_PREFIX` nhưng phía nguồn không có.
- Bằng chứng: hai dòng trích ở trên; s3store.go:73 `return aws.String(s.prefix + string(b)), nil`.
- Đề xuất sửa: thêm `FILESYNC_SRC_BUCKET_PREFIX` (mặc định `crm-`), ghi bucket logic (`public`…) vào `file_sync_runs.bucket`, `syncedBuckets` theo đó; hoặc ghi rõ ràng buộc "tiền tố phải là crm-" trong RUNBOOK và `Config.Load`. Cỡ: nhỏ.

##### C-07: Trạng thái `processing` không bao giờ được ghi; ảnh đang xử lý vẫn hiện `uploaded`
- Mức: nhỏ
- Tệp:dòng: internal/platform/files/upload.go:25 (hằng `StatusProcessing` có nhưng không câu SQL nào đặt), queries/platform/files.sql:95, :102 (chỉ đọc `processing` trong điều kiện)
- Spec/kế hoạch: 05 mục 7.3 (vòng đời `uploaded → processing → ready|rejected`), Task 41 (`GET /api/v1/files/{id}` trả trạng thái; `DownloadURL`: `uploaded`/`processing` → `ErrNotReady`).
- Mô tả: không ảnh nào kẹt ở `processing` (vì không ai đặt), nhưng màn hình và mốc sau không phân biệt "chưa đến lượt" với "đang xử lý"; sau 3 lượt lỗi tạm (C-02) tệp nhảy thẳng từ `uploaded` sang `rejected`. Đây là lệch với spec về trạng thái quan sát được, không phải lỗi dữ liệu.
- Bằng chứng: `grep -rn "processing'" internal queries migrations` (bỏ test, mã sinh) chỉ ra hai câu điều kiện và hằng Go.
- Đề xuất sửa: hoặc ghi `processing` trong transaction đọc ngắn ở đầu `Execute` (OnTerminal, `SetFileRejected` đã nhận trạng thái này), hoặc ghi vào spec rằng M1 không dùng `processing`. Cỡ: nhỏ.

##### C-08: Chưa xác minh ảnh thô (còn EXIF/GPS) thật sự bị xóa khỏi `crm-tmp` sau 24 giờ
- Mức: nghi ngờ
- Tệp:dòng: docs/do-luong/seaweedfs-m1.md:17 ("**Chưa xác minh** đối tượng thật sự biến mất sau 24 giờ"); internal/platform/files/process_private.go:16–17 (dựa vào "tự xóa sau 24 giờ")
- Spec/kế hoạch: R10 (ảnh riêng tư không giữ bản thô), Q18 (`crm-tmp` tự xóa sau 24 giờ), Task 40 mục 6.
- Mô tả: toàn bộ lý lẽ R10 cho ảnh riêng tư (ảnh chat của khách ở M4, ảnh kèm yêu cầu ở M5) là bản thô chỉ nằm ở `crm-tmp` và biến mất sau một ngày. Phép thử chỉ đọc lại được quy tắc vòng đời; đường TTL nhanh đang tắt, việc xóa dựa vào vòng quét lifecycle của SeaweedFS 4.48 mà chưa ai thấy chạy. Nếu vòng quét không chạy, ảnh thô có vị trí GPS của khách nằm lại vô hạn (và `Delete` của ứng dụng chỉ tạo dấu xóa nếu sau này bật phiên bản).
- Bằng chứng: trích ở trên; không có test nào kiểm đối tượng `crm-tmp` biến mất.
- Đề xuất sửa: phép đo một lần (đặt đồng hồ hệ thống hoặc quy tắc 1 ngày rồi chờ vòng quét) trước M4, hoặc thêm lệnh dọn `tmp/<tenant>/<id>` sau khi `file.process_private` xong (danh tính có quyền xóa trên `crm-tmp`, không phiên bản). Cỡ: nhỏ.

---

#### Đã kiểm, không thấy lỗi

- **R10, `file.process` từ chối mục đích private:** `load` (process.go:223–226) trả `purpose_private`/`purpose_mode` trước mọi lần gọi kho hay imager; `OnTerminal` không đụng tệp của chế độ khác (process.go:617); `RegisterPrivateImage` chỉ nhận `Private`; `Register` chặn `Private`; `PublicVariants`, `Describe`, view `platform.file_variants_v` lọc `mode = 'standard'` và `bucket = 'public'`; `ListFilesMissingPreset` loại mục đích private; `DownloadURL` nhánh private chỉ ký biến thể JPEG lớn nhất trên `BucketPrivate`; đường private không gọi `CopyIfAbsent` sang originals, khóa đầu ra `priv/<tenant>/<file>/v<n>/…`.
- **Gọi mạng ngoài transaction (ADR-46 điểm 3):** `CompleteUpload`/`InspectUpload` gọi Head ngoài tx; `Execute` của process/regenerate/verify chỉ mở transaction đọc ngắn; `OnDone`/`OnTerminal` không gọi kho hay imager (chỉ SQL, Enqueue, Publish trong tx); `CreateUpload`, `DownloadURL`, `OriginalURL` chỉ ký tại chỗ; `ReadContent` đọc kho sau khi đóng tx; `writeManifest` ngoài tx.
- **Ảnh treo bị giết đúng hạn, worker không ảnh hưởng (tiêu chí 7):** `ExecRunner` dùng `Setpgid` + `cmd.Cancel` giết cả nhóm bằng SIGKILL, `WaitDelay` 1 s, môi trường tối thiểu, `prlimit --as`; mỗi bước có hạn `IMAGER_STEP_TIMEOUT`; worker hủy HTTP sau 30 s thì ngữ cảnh yêu cầu ở imager bị hủy và tiến trình con bị giết; worker chỉ cho 2 việc tới imager (các loại việc khác vẫn chạy). Test `TestFileProcess_HangDoesNotBlockWorker`, `TestImager_HangKilledWithinDeadline`, `TestExecRunner_TimeoutKillsProcessGroup` có mặt và qua (gói files).
- **Không ảnh kẹt:** tệp `uploaded` luôn có lệnh xếp cùng tx (`ApplyUpload`, `RegisterPrivateImage`); dead/expired → `OnTerminal` → `rejected`; in_doubt → `Lookup` trả `not_sent` → resolver xếp lại. Không có đường nào đặt `processing` (xem C-07).
- **Giới hạn kích thước và điểm ảnh:** kiểm ở `CreateUpload`, chữ ký PUT có `content-length`; `ApplyUpload` so kích thước, loại; `Open` có `maxBytes` và `limitedBody`; imager giới hạn tải nguồn bằng `LimitReader(MaxBytes+1)`, nhận loại bằng byte đầu (DetectContentType + mimetype), bộ đọc libvips phải khớp loại, `width*height > MaxPixels` từ chối trước khi giải mã; `--size down`, `keep=none`; `contentReason` kiểm lại khi dùng bản kê dùng chung.
- **SSRF/path traversal:** khóa trên kho chỉ gồm tiền tố cố định, uuid, sha hex, số (keys.go); `Register` đòi tiền tố `priv/<tenant trong ngữ cảnh>/`, chặn `..`, `//`; imager chỉ nhận URL do worker ký, không theo chuyển hướng, không proxy, không giải nén; tên tải về chỉ nằm trong `filename*` RFC 5987 (store.go:86–107). (Phần mạng nội bộ của kho: C-01.)
- **RLS:** `platform.files`, `file_variants`, `catalog.product_images` bật `enable_tenant_rls`; quyền cột `UPDATE` của `crm_app` trên `files` không gồm tenant, purpose, tên, loại khai, kích thước; `file_purposes` chỉ SELECT; `file_sync_runs` không RLS, `crm_filesync` chỉ INSERT; mọi câu trong `tenancy.WithTenant` hoặc tx của Runner (đã có tenant).
- **Chống trùng:** `DedupeKey = fileId` khi complete (`TestUpload_ConcurrentComplete`); `findDuplicate` cùng tenant, cùng mục đích, cùng bộ kích thước; biến thể public theo sha, PUT `If-None-Match: *`; `InsertFileVariant ON CONFLICT DO NOTHING`; regenerate `fileId:v<n>`; verify theo lượt + mã tệp cuối.
- **Ảnh sản phẩm (Task 44):** khóa mẫu trước, dòng ảnh sau (ADR-46 nhóm 4); `files.Status` chỉ đọc; xóa mềm, khôi phục trong 30 ngày bằng điều kiện trong SQL (`RestoreProductImage`); `MaxImages`; màu phải thuộc biến thể của mẫu; audit chỉ mã, không tên tệp.
- **Ảnh trong bài viết (Task 320):** một lời gọi `PublicVariants`, không gọi khi không có khối ảnh; tệp tenant khác, private, lost → bỏ khối; khối mới phải `ready` + `product_image`.
- **Ảnh gốc URL có hạn (Task 321, TP05):** `x-permission: catalog.manage`, hạn 5 phút, `ExpiresAt` tính trước khi ký, chỉ mục đích ảnh standard, tệp xóa/mất → 404.
- **Tạo lại kích thước (Task 322, TP07):** nguồn `orig/<sha>`, không đổi trạng thái hay `preset_version` của tệp, ảnh gốc mất → `source_missing` mà tệp vẫn `ready`, private không được xếp; `SelectPreset` lùi về bản đủ gần nhất.
- **Đối chiếu kho, tệp mất (Task 46):** lô 500, hạn 20 s, lỗi Head khác "không có" thì không đánh dấu gì; `OnDone` khóa theo mã tăng dần và so `version`; tải lại ảnh cùng nội dung → `enqueueRestore` đưa tệp `lost` về `ready`.
- **filesync (Task 45):** chỉ `rclone copy --immutable`, không sync/move/delete; `crm-tmp` không chép; bucket riêng tư qua crypt; mật khẩu bị xóa khỏi môi trường sau khi ghi cấu hình 600 vào /tmp; giá trị vào SQL qua biến psql; danh tính `crm-filesync` chỉ đọc ở kho chính; luật 90 phút, failed, mismatch đúng như kế hoạch.
- **Lệch kế hoạch đã được ghi, không gây sai:** `Lookup` luôn trả `not_sent` thay vì "đủ đầu ra → done" như Interfaces Task 43; chạy lại đọc bản kê và không gọi imager lần hai (`TestFileProcess_LookerAfterLostAnswer`), kết quả tương đương.
- **M2 so với giả định của M1:** không migration 02xx nào đụng `platform.files`, `file_purposes`, `file_variants`, `product_images`, `file_sync_runs`; không commit gộp M2 nào sửa `internal/platform/files`, `internal/imager`, `scripts/filesync` (git log --first-parent); không chỗ gọi API tệp ngoài catalog và httpapi.
- **Tên test nghiệm thu có trong mã:** `TestFileProcess_ReadyWithTwelveVariants`, `TestImager_RejectsHTMLAsJPEG`, `TestImager_PixelBombRejectedBeforeDecode`, `TestCreateUpload_TooLarge`, `TestOriginalURL_ExpiresAndNeedsPermission`, `TestImageSoftDeleteRestore`, `TestAttachImage_OrderAndColor`, `TestRegeneratePresetV2`, `TestVerify_MarksLost`, `TestFileStoreCrash`, `TestRule_FileSyncAge`, `TestArticleHTML_WithImages`; có `scripts/files/restore-drill.sh`.

### Vùng D — quyền (identity M1) và lát mỏng "một đơn đi trọn"

Reviewer độc lập, đọc mã trên nhánh task-404 (= main, HEAD a3f7be3). Chỉ đọc; chạy test để xác minh:

- `go test -race -count=1 ./internal/modules/identity/...` → `ok (237.8s)`.
- `go test -count=1 -v ./tests/e2e/ -run 'TestMotDonDiTron$|TestMotDonDiTron_HaiDonSongSongTon1'` → cả hai `PASS` (đường
  một đơn đi trọn vẫn chạy trên HEAD sau khi gộp M2).

Tóm tắt: 0 nghiêm trọng, 1 lớn, 3 nhỏ, 2 nghi ngờ.

---

##### D-01: Quyền và kho của vận đơn lấy theo kho của tài khoản hãng, còn tồn bị trừ ở kho giữ hàng của đơn — hai kho có thể khác nhau
- Mức: lớn
- Tệp:dòng: internal/httpapi/handlers_shipment.go:32-40; internal/modules/fulfillment/fulfillment.go:134-166; internal/modules/order/shipment_progress.go:97-121; internal/modules/order/create.go:104-109
- Spec/kế hoạch: Task 31 (M1-31) "createShipment (`fulfillment.ship` theo đích `warehouse` = `carrier_accounts.warehouse_id`)", B6 (quyền vận chuyển theo kho), Task 30 (đơn có `WarehouseID` giữ hàng); ADR-18 (phạm vi kho).
- Mô tả: Đơn giữ hàng ở `in.WarehouseID` (do người lên đơn chọn, hoặc kho chính). Handler tạo vận đơn chỉ kiểm `fulfillment.ship` ở kho của **tài khoản hãng** người gọi chọn; `CreateForOrder` chỉ kiểm tài khoản cùng người bán với đơn, **không kiểm kho của tài khoản = kho giữ hàng của đơn**. Khi hãng báo lấy hàng/giao xong, `ApplyShipmentProgress` chuyển và trừ đúng các dòng giữ hàng của đơn (ở kho giữ hàng). Tình huống: đơn giữ hàng ở kho W2; nhân viên kho W1 (chỉ có `fulfillment.ship` theo `warehouse:W1`) tạo vận đơn bằng tài khoản hãng của W1 → được phép (202); hãng đến lấy ở W1, nhưng `on_hand` của W2 bị trừ, W1 không đổi → sổ tồn hai kho cùng sai, và người chỉ có quyền W1 đã làm đổi tồn W2 (vượt phạm vi kho). Lỗi tồn tại từ M1 (CreateManualInput đã có WarehouseID trước M2); M2 còn ghi `ord.orders.warehouse_id` nên dữ liệu so sánh đã sẵn.
- Bằng chứng:
  ```go
  // handlers_shipment.go
  wh, err := fulfillment.AccountWarehouse(ctx, tx, req.CarrierAccountId)
  if err := canAt(u, wh, permShip); err != nil { return nil, err }
  s, err := fulfillment.CreateForOrder(ctx, tx, params.ID, req.CarrierAccountId, …)
  // fulfillment.go CreateForOrder: chỉ order.Lock(a.SellerID…), order.Get, kiểm CurrentShipmentID, Status — không so a.WarehouseID với o.WarehouseID
  // shipment_progress.go: items := q.OrderItemIDs(...); stock.StartTransitByItems / ConsumeByItems(items) — theo dòng giữ hàng của đơn
  ```
  `inventory.byItems` (internal/modules/inventory/byitems.go) đổi trạng thái đúng các dòng `stock_reservations` của dòng đơn, mang `warehouse_id` của lúc giữ hàng.
- Đề xuất sửa: trong `CreateForOrder`, đơn có `WarehouseID` khác `a.WarehouseID` → lỗi nghiệp vụ mới (ví dụ `ErrAccountWrongWarehouse`, 422); test hai kho: tài khoản W1 + đơn giữ ở W2 → từ chối, tồn không đổi. Cỡ: nhỏ.

##### D-02: Lên đơn giữ hàng ở bất kỳ kho nào theo `warehouseId` của yêu cầu, không kiểm phạm vi kho
- Mức: nhỏ
- Tệp:dòng: internal/httpapi/handlers_order.go:51-53; internal/modules/order/create.go:104-109
- Spec/kế hoạch: ADR-18 (quyền theo phạm vi kho); Task 30 thân yêu cầu `warehouseId?`; Task 315 ("phạm vi chỉ thu hẹp quyền mà tài nguyên có phạm vi").
- Mô tả: `order.create` là quyền `ScopeNone`, handler nhận `warehouseId` tùy ý và `CreateManual` giữ hàng ở kho đó mà không ai kiểm người gọi có quyền gì ở kho này. Nhân viên chỉ được gán theo `warehouse:W1` có thể khóa (held) hàng của W2, làm giảm "có thể bán" ở W2. Không mất dữ liệu nhưng là thao tác tồn ngoài phạm vi kho; cộng với D-01 thì đi trọn được tới trừ tồn W2.
- Bằng chứng: `WarehouseID: req.WarehouseId.Or(uuid.Nil)` (handlers_order.go:52) truyền thẳng; `draft` chỉ thay uuid.Nil bằng `inventory.DefaultWarehouse`.
- Đề xuất sửa: khi `warehouseId` có giá trị, kiểm `identity.Can(u, "inventory.read" hoặc quyền giữ hàng phù hợp, warehouse:<id>)` (hoặc quyết định rõ trong docs/quyen.md rằng lên đơn được chọn mọi kho). Cỡ: nhỏ.

##### D-03: Vận đơn kẹt ở `creating` khi lệnh tạo bị từ chối, hết hạn hay dead — đơn không tạo lại được vận đơn, hàng giữ mãi
- Mức: nhỏ
- Tệp:dòng: internal/modules/fulfillment/create_shipment.go:170-196 (Execute trả rejected), 151-161 (CreateCommand không hiện thực `queue.TerminalHook`); migrations/0118_fulfillment.sql:52 (unique một phần `WHERE status <> 'cancelled'`); internal/modules/fulfillment/fulfillment.go:156-158
- Spec/kế hoạch: Task 31 (lệnh `fulfillment.create_shipment` hết hạn sau 10 phút), Q22; 05 mục 2 (vận đơn hiện hành).
- Mô tả: `CreateForOrder` gắn vận đơn `creating` làm vận đơn hiện hành và ghi lệnh hết hạn sau 10 phút. Nếu hãng trả 4xx (`rejected`), worker ngừng quá 10 phút (`expired`), hay hết 8 lượt (`dead`), không có gì đưa vận đơn sang `cancelled` hay gỡ `current_shipment_id`: catalog có `OnTerminal` cho tình huống tương tự (`internal/modules/catalog/imports_apply.go:91`), fulfillment thì không. Kết quả: lần tạo vận đơn sau cho cùng đơn → `ErrShipmentExists` (409), dòng giữ hàng ở `held` mãi, M1 không có đường hủy vận đơn. Không mất tiền/hàng, nhưng đơn kẹt cần sửa tay bằng SQL.
- Bằng chứng: `grep -rn TerminalHook internal/modules` chỉ ra catalog; `CreateCommand` chỉ có `Execute`, `Lookup`, `OnDone`. `if o.CurrentShipmentID != nil { return Shipment{}, ErrShipmentExists }`.
- Đề xuất sửa: `CreateCommand.OnTerminal` (rejected/expired/dead, không in_doubt) → vận đơn còn `creating` sang `cancelled`, gỡ `current_shipment_id` qua một hàm mới của order (không đổi chữ ký M1), kèm test; hoặc ghi rõ vào "Việc phát hiện thêm" cho M3 kèm runbook sửa tay. Cỡ: nhỏ.

##### D-04: `customer.Create` của M1 (vẫn là đường tạo khách của `POST /api/v1/orders`) không ghi `name_search`, `lifecycle` mà M2 thêm
- Mức: nhỏ (ghi cho M2; không làm hỏng lát mỏng)
- Tệp:dòng: queries/customer/customers.sql (InsertCustomer chỉ ghi id, tenant_id, root_id, full_name); internal/httpapi/handlers_order.go:113; migrations/0201_customer.sql:22-23, 41-50
- Spec/kế hoạch: Q1 (M2 chỉ mở rộng; hàm M1 giữ chữ ký, hành vi phải còn đúng); 05 mục 1 (`name_search`, `lifecycle`).
- Mô tả: 0201 điền `name_search` cho dòng có sẵn **lúc migrate**, cột có `DEFAULT ''`. Khách tạo sau đó qua đường M1 (`createOrder` → `customer.Create`) có `name_search = ''` và `lifecycle = 'lead'` dù đã có đơn. Khi M2 bật tìm khách theo tên (index `customers_name_search_trgm` đã có), các khách này không tìm thấy. Hiện chưa có câu tìm khách trong `queries/customer`, nên chưa lộ.
- Bằng chứng: `INSERT INTO customer.customers (id, tenant_id, root_id, full_name) VALUES (@id, …, @id, @full_name);`; `git diff abd3410^1 HEAD -- internal/modules/customer/customer.go` chỉ thêm hằng, `Create` không đổi.
- Đề xuất sửa: M2 cho `customer.Create` ghi `name_search = textnorm.Fold(fullName)` (thân hàm, giữ chữ ký) hoặc chuyển `createOrder` sang đường tạo khách M2; thêm test "khách tạo qua đơn M1 tìm được theo tên". Cỡ: nhỏ.

##### D-05: Đơn ở `cancel_requested` (M2 cho phép khi có vận đơn hiện hành) làm lệnh tra trạng thái vận đơn lỗi mãi
- Mức: nghi ngờ (M2 mới có máy trạng thái, chưa có đường ghi hủy; sẽ thành lỗi khi M2/M3 nối)
- Tệp:dòng: internal/modules/order/shipment_progress.go:90-96; internal/modules/fulfillment/refresh_status.go:149-153; internal/modules/order/internal/state/machine.go (cạnh `confirmed → cancel_requested` khi có vận đơn)
- Spec/kế hoạch: Task 333 (ApplyShipmentProgress: lỗi → bên gọi lùi cả transaction); 05 mục 2 (đơn và vận đơn hai trục riêng).
- Mô tả: `ApplyShipmentProgress` chỉ nhận đơn `confirmed`/`delivered`, trạng thái khác → `ErrStatusConflict`. `RefreshCommand.OnDone` trả lỗi đó → DoneHook lỗi thì lệnh không thành done; mỗi lần tra lại vẫn lỗi → vận đơn không bao giờ sang `shipping`/`delivered`, không phát `shipment.status_changed`, sổ COD không ghi `collected` dù hãng đã thu tiền. M2 `state.Check` đã cho `confirmed → cancel_requested` khi có vận đơn hiện hành, đúng là lúc hãng có thể vẫn giao. Giả định "đơn có vận đơn hiện hành luôn ở confirmed" của M1 không còn đúng khi đường hủy của M2 được nối.
- Bằng chứng: `switch o.Status { case StatusDelivered: return false, nil; case StatusConfirmed: default: return false, fmt.Errorf("%w: áp %s lên đơn %s", ErrStatusConflict, step, o.Status) }`; `if _, err := w.ApplyShipmentProgress(...); err != nil { return fmt.Errorf("áp bước vận đơn lên đơn: %w", err) }`.
- Đề xuất sửa: trước khi M2 nối đường `cancel_requested`, quyết định ở order: `cancel_requested` + bước của vận đơn hiện hành → vẫn áp tồn (và `delivered` thì đơn → delivered hay giữ chờ người xử lý), không trả lỗi làm kẹt lệnh; thêm ca test. Cỡ: nhỏ.

##### D-06: `AttachShipment` tăng `ord.orders.version` mà không phát sự kiện — phá bất biến "version = seq của sự kiện cuối" mà `CreateManual` nêu
- Mức: nghi ngờ (chưa thấy consumer nào cần seq liền nhau)
- Tệp:dòng: queries/ord/orders.sql (`SetCurrentShipment`: `version = version + 1`); internal/modules/order/create.go:129; internal/modules/order/events.go:13-14
- Spec/kế hoạch: docs/views.md "Sự kiện công bố" (Seq = version của đơn sau thay đổi).
- Mô tả: Tạo đơn: version 2 = seq của `order.confirmed`. Gắn vận đơn: version 3, không có sự kiện. Giao xong: `order.delivered` seq 4. Realtime/consumer nào dựa vào `payload.version` liền mạch (phát hiện bỏ lỡ) sẽ coi seq 3 là mất. `queue` OrderStrict chỉ so thứ tự, không đòi liền nhau (queries/platform/jobs.sql:80), nên hiện không kẹt.
- Bằng chứng: `UPDATE ord.orders SET current_shipment_id = @shipment_id, version = version + 1 …` không kèm `w.emit`.
- Đề xuất sửa: ghi rõ trong docs/views.md rằng seq có thể nhảy (không liền nhau), hoặc phát `order.shipment_attached`. Cỡ: nhỏ.

---

#### Đã kiểm, không thấy lỗi

**Identity M1 (Task 26, 315, 55/M1-56, 54 phần API)**
- `0113_identity_m1.sql`: chỉ thêm cột có DEFAULT, hàm, view, quyền; `identity.users_v` `security_invoker`, không có email; `crm_app` không có INSERT trên `platform.tenant_routes`.
- `platform.add_staff_route` (Q25): `SECURITY DEFINER`, `search_path = ''`, tenant chỉ lấy từ `app.tenant_id`, thiếu ngữ cảnh → 42501, email rỗng → 22023, lowercase, `REVOKE … FROM PUBLIC` + `GRANT … TO crm_app`; trùng → 23505 → `ErrEmailTaken`, cả tx lùi (WriteTx `final=false` → rollback, kiểm `internal/httpapi/idempotency.go:223`). Giới hạn vốn có: `crm_app` tự đặt được `app.tenant_id` — cùng ranh giới với RLS, không phải lỗi riêng của hàm. 0290 `put_tenant_route` không nhận `staff_email`, không thay hàm 0113.
- PQ07 trong `staff.go`: tự sửa mình → `ErrSelfModify` (cả grant lẫn khóa/mở); thêm hoặc gỡ grant đòi actor có mọi quyền của vai trò trên đúng phạm vi (`hasAllPermissions`; quyền `none` từ grant bất kỳ, `toàn shop` đòi `all`, `theo đích` đòi `all` hoặc đúng đích — khớp docs/quyen.md dòng 29-31, là bản sau Task 315 lượt 2 của luật Task 26); khóa/mở đòi quyền trên mọi grant hiện có; kiểm lại quyền actor đọc trong tx (actor vừa bị hạ/khóa); vai trò đã lưu trữ không gán mới được.
- Chủ cuối cùng, đồng thời: `LockStaffRows` khóa target, actor và mọi Chủ active theo id tăng dần trong một câu; đếm `CountActiveOwners` là câu mới ở READ COMMITTED (mặc định `db/tx.go:93`) nên bên chờ thấy kết quả bên trước. Hai Chủ gỡ/khóa nhau: tập khóa chồng nhau → tuần tự. Test `TestLock_LastOwnerConcurrent` (50 lượt × 2 loại) qua.
- Vai trò `owner` không sửa, không lưu trữ được (`lockRole`), nên không có đường làm Chủ mất `identity.users.manage`; `EnsureDefaultRoles` luôn bổ sung đủ quyền cho owner, không thêm quyền cho vai trò `customized`; khóa `GetRoleByKey … FOR SHARE` chống chạy cùng `UpdateRole`.
- Vai trò tự định nghĩa: thêm/gỡ quyền đòi actor có quyền đó ở `all` (không tự nâng bằng sửa vai trò mình giữ); trùng tên chặn bằng khóa tư vấn theo tenant; lưu trữ vs gán cùng lúc: roles `FOR UPDATE` vs `FOR SHARE`, thứ tự users → roles (ADR-46 nhóm 2), không khóa chéo. `ListUserGrants` bỏ vai trò archived.
- Lỗi → HTTP ở một chỗ (`errors.go:306-319`) đúng mã kế hoạch (`cannot_modify_self` 403, `grant_exceeds_actor` 403, `last_owner` 409, `email_taken` 409, `version_conflict` 409, `owner_role_locked` 409, `role_in_use` 409, `unknown_permission` 422, `role_name_taken` 409). Audit nhân viên không mang email/tên.
- Tên test nghiệm thu có trong mã: `TestReplaceGrants_*`, `TestLock_LastOwner*`, `TestPermissionsDoc_MatchesCode`, `TestCustomRole_CannotExceedActor`, `TestStaffInvariantsProperty`, `TestRoleInvariantsProperty`.

**Lát mỏng (Task 30, 333, 31, 32, 33, 395)**
- Giữ hàng cùng transaction với tạo đơn: `CreateManual` chèn đơn, dòng, lịch sử rồi `stock.Reserve(ctx, tx, …)` trong cùng tx; thiếu hàng → lỗi, WriteTx lùi cả tx. e2e hai đơn tranh chiếc cuối qua.
- Mã đơn một chỗ (`order.nextCode`, sequence, Q26); mã vận đơn một chỗ (`NewShipmentCode`, giờ VN cố định, base32 Crockford, Q36); trạng thái `creating`, cột `external_account_ref` đúng tên M3.
- Chỉ vận đơn hiện hành đổi đơn và tồn: `ApplyShipmentProgress` khóa đơn trước, so `current_shipment_id`, gọi lại cùng bước → `applied=false`; unique một phần một đơn một vận đơn chưa hủy; tạo vận đơn khóa đơn trước nên hai lần tạo cùng lúc tuần tự.
- Webhook chỉ là tín hiệu: `fakecarrier.Source.VerifySignature` HMAC-SHA256 so `hmac.Equal`, bí mật rỗng → sai; `SignalHandler` không gọi hãng, chỉ ghi lệnh `refresh_status` (dedupe `evt:<eventId>`), tìm vận đơn theo tracking **và** `external_account_ref`; trạng thái thật chỉ đọc qua `GetStatus`; `Advance` chỉ tiến.
- Lệnh tạo vận đơn in_doubt: lỗi không phân loại / quá giờ → trả lỗi (in_doubt, không tự gửi); chỉ lỗi dial → `ErrNotSent`; 503 → not_sent; `Lookup` theo mã CRM, done do tra lại → ghi lệnh refresh lấy mã hãng. `refresh_status` đọc an toàn nên `not_sent` khi lỗi.
- Sổ COD hai vế: kiểm ở Go (`validate`) và trigger hoãn tới commit trên cả hai bảng (≥ 2 dòng, tổng 0); chỉ ghi thêm (trigger chặn UPDATE/DELETE/TRUNCATE); consumer chống trùng theo loại của vận đơn và chạy thứ tự nghiêm theo vận đơn (`events/consumers.go:69`).
- `fakecarrier` chỉ đăng ký khi `cfg.Env != "production"` và có URL (`cmd/crm/main.go:188-201, 250-256, 510-515`); `config.Load` cấm biến FAKECARRIER_* ở production; giá trị `CRM_ENV` lạ là lỗi khởi động; test e2e `ingest_test.go:60` kiểm 404 ở production.
- Lỗi vận chuyển, đơn → HTTP ở `errors.go` (`shipment_exists` 409, `out_of_stock` 409, `mixed_sellers` 422…); thiếu quyền ở kho là từ chối không lưu theo khóa.

**M2 so với M1 (Q1)**
- Không migration M2 nào `CREATE` lại bảng M1 (`customer.customers`, `ord.orders`, `ord.order_items`, `ord.order_status_history`, `fulfillment.*`, `cash.*`); chỉ `ALTER … ADD COLUMN` có DEFAULT/NULL, ràng buộc `NOT VALID` rồi `VALIDATE`, `SET NOT NULL`/`DROP CONSTRAINT` có dòng `-- contract:`; view chỉ `CREATE OR REPLACE` thêm cột cuối.
- `orders_check1` bị bỏ đúng là CHECK `cod_amount BETWEEN 0 AND grand_total` của 0117 (CHECK bảng thứ hai không tên), thay bằng `cod_amount = max(grand_total − prepaid_total, 0)` mà `CreateManual` thỏa (cod = grand, prepaid 0).
- Chữ ký hàm Go xuất ra của `customer`, `order`, `fulfillment`, `cash`, `identity` giữa trước M2 (`abd3410^1`) và HEAD: không hàm nào mất hay đổi chữ ký (chỉ `store.GetOrder` nội bộ đổi sang `GetOrderBase`); struct M1 chỉ thêm trường. `CreateManual` thêm gộp dòng cùng biến thể (cần cho unique `order_items_variant_key`) và ghi `warehouse_id`, `ship_phone` — đổi thân, không đổi chữ ký.
- FK `stock_reservations → order_items` đổi sang `ON DELETE SET NULL`, cột cho phép NULL: các hàm `*ByItems` của M1 vẫn tìm theo `order_item_id`, không ảnh hưởng đường một đơn đi trọn (e2e qua).

### Vùng E — soát chéo toàn M1 (ca nghiệm thu, tên mốc sau dựa vào, threat model, quyền màn hình, ảnh hưởng của M2)

Reviewer độc lập, chỉ đọc. Ngày 19/11/2026, worktree task-404 (= main a3f7be3). Test chạy với
`CRM_TEST_DATABASE_URL` (crm_app @127.0.0.1:5600), mỗi lần một gói, `-race -count=1`.

#### Phát hiện

##### E-01: Màn phiếu kho bỏ qua phạm vi kho của quyền — hiện nút "Tạo phiếu", "Ghi sổ…", chọn kho mà máy chủ sẽ trả 403
- Mức: nhỏ
- Tệp:dòng: console/src/screens/inventory/DocumentEdit.tsx:100-108, 179-180, 400, 620-621; console/src/screens/inventory/DocumentList.tsx:31,72; console/src/screens/inventory/Balances.tsx:52,98,315; console/src/app/nav.ts:27-29
- Spec/kế hoạch: CLAUDE.md "Bẫy đã gặp" — "Màn hình: chỉ vẽ hành vi mà máy chủ thật sự làm (quyền, chặn, lỗi); mock trong test khớp quyền thật"; quyen.md: `inventory.documents.create`, `inventory.documents.post` là "theo đích: warehouse".
- Mô tả: `hasPermission(grants, p)` chỉ xem có grant của quyền ở bất kỳ phạm vi nào (`grants.some(g => g.permission === p)`). Máy chủ kiểm đúng kho (`canAt(u, warehouseID, …)` trong `internal/httpapi/handlers_inventory.go:35-41` → 403 `forbidden`). Tình huống: nhân viên được gán "Kho" theo kho A và "Bán hàng" (có `inventory.read`) toàn shop → `listWarehouses` trả cả A, B; màn "Phiếu mới" cho chọn kho B (danh sách `list` lọc theo `status === "active"`, không lọc theo `inventory.documents.create` ở kho đó) và nút Lưu nháp; mở phiếu nháp của kho B thì nút "Ghi sổ…" sáng nếu người đó có `inventory.documents.post` ở kho A. Bấm → 403. Màn Kiểm kho thì làm đúng (`stocktakeRules.ts:17 canAt` xét `scope.type === "all" || id === warehouseId`) nên hai màn kho không nhất quán.
- Bằng chứng: `DocumentEdit.tsx:104-105` `canCreate: hasPermission(g, "inventory.documents.create"), canPost: hasPermission(g, "inventory.documents.post")`; `documentsTestkit.tsx:89-93` `grantsOf` chỉ dựng grant `scope: { type: "all" }` → không test nào phủ grant theo kho cho màn phiếu.
- Đề xuất sửa: dùng `canAt(grants, perm, warehouseId)` (đã có ở `stocktakeRules.ts`) cho nút ghi sổ/soạn và lọc danh sách kho của "Phiếu mới", nút "Tạo phiếu" ở Balances theo kho đang xem; thêm ca test grant theo kho; cỡ: nhỏ.

##### E-02: Màn Nhập Excel mở cho người có `catalog.import` nhưng thiếu `files.upload` — bước tải lên luôn 403
- Mức: nhỏ
- Tệp:dòng: console/src/screens/imports/route.tsx:7,12; console/src/screens/imports/ImportPage.tsx:72-75
- Spec/kế hoạch: CLAUDE.md "Bẫy đã gặp" (màn hình chỉ vẽ hành vi máy chủ làm); quyen.md: `catalog.import` và `files.upload` là hai quyền tách, vai trò tự định nghĩa (Task M1-56) có thể có cái này mà không có cái kia.
- Mô tả: route chỉ đòi `catalog.import`; `createUpload`/`completeUpload` đòi `files.upload` (routes_gen.go). Vai trò tự định nghĩa có `catalog.import` mà không có `files.upload` thấy màn, chọn tệp, rồi nhận 403 chung chung (`problemOf` chỉ tách `file_too_large`, `file_type_not_allowed`). Chú thích ở route.tsx:7 đã biết ("Tải lên cần thêm files.upload (API kiểm)") nhưng màn không báo trước.
- Bằng chứng: `route.tsx:12 permission: "catalog.import"`; `grep files.upload console/src/screens/imports` → chỉ dòng chú thích.
- Cùng dạng: route Kiểm kho (`console/src/screens/inventory/route.tsx:45,54`) chỉ đòi `inventory.stocktake`, nhưng màn gọi `listWarehouses`, `getStocktake`, `listBalances` (đều `inventory.read`) và `searchVariants` (`catalog.read`). Vai trò mẫu có đủ; chỉ vai trò tự định nghĩa thiếu `inventory.read` mới gặp (vào được màn nhưng không tải được kho).
- Đề xuất sửa: màn kiểm thêm `files.upload`, thiếu thì hiện câu "Cần thêm quyền tải tệp" thay vì nút tải lên (hoặc vai trò tự định nghĩa tự kèm `files.upload` khi chọn `catalog.import`); route Kiểm kho đòi cả `inventory.read`; cỡ: nhỏ.

##### E-03: Tên/chữ ký lệch giữa kế hoạch M1 (mốc chủ) và mã: operationId `getOriginalImageURL`; `ReplaceGrants`, `SetLocked` trả `(Change, error)`
- Mức: nhỏ
- Tệp:dòng: openapi/openapi.yaml:764 (`operationId: getOriginalImageURL`); internal/modules/identity/staff.go:189, 246
- Spec/kế hoạch: kế hoạch M1 dòng 1336 (Task 321), 1595 (Task 325) ghi `getOriginalImageUrl`; dòng 424 ghi `ReplaceGrants(...) error`, `SetLocked(...) error`; kế hoạch M7 dòng 614 dựa vào "`SetLocked(ctx, tx, actor, userID, locked)` (M1, giữ nguyên chữ ký)". M1 là mốc chủ của tên (Q-X1, Q-X2).
- Mô tả: (1) operationId khác hoa thường; client TS sinh ra vẫn là `getOriginalImageUrl` (console/src/api/sdk.gen.ts:303) nên màn không lỗi, nhưng hợp đồng (oasdiff, mốc sau tra theo operationId) và kế hoạch lệch. (2) Hai hàm trả thêm `Change` — đã ghi quyết định ở docs/tasks/task-26.md:96 nhưng kế hoạch M1 không được sửa theo (Task M1-24 đặt luật "sửa thẳng vào tệp kế hoạch khi lệch"). Không gây lỗi chạy; M7 chỉ cần biết chữ ký thật.
- Bằng chứng: `routes_gen.go`: `OperationID: "getOriginalImageURL"`; `staff.go:246 func SetLocked(...) (Change, error)`.
- Đề xuất sửa: sửa kế hoạch M1 (và dòng giả định M7) theo mã; không đổi operationId đã gộp; cỡ: nhỏ.

##### E-04: Ca nghiệm thu TP03 (bom điểm ảnh), TP08, TP09, TP10 (`filesync_test`), TP01 với imager thật và phần OOM của tiêu chí Xong 7 không chạy ở CI hay `make ci` — trên máy dev chúng SKIP mà gói vẫn `ok`
- Mức: nhỏ (rủi ro quy trình; không thấy lỗi mã)
- Tệp:dòng: internal/imager/pipeline_test.go:148-158 (`requireTools` → `t.Skipf` trừ khi `IMAGER_REQUIRE_VIPS=1`), :452-453; tests/drills/filestore_crash_test.go:1 (`//go:build drill`); tests/e2e/filesync_test.go:1 (`//go:build filesync`); tests/e2e/files_pipeline_test.go:1 (`//go:build imager`); .github/workflows/ci.yml:54-57 (chỉ `make db-up`, `make ci`); Makefile:53-56 (`test-imager` riêng, chạy trong docker)
- Spec/kế hoạch: kế hoạch M1 "Tiêu chí nghiệm thu → ca kiểm thử" (TP01, TP03, TP08, TP09, TP10), "Xong M1 khi" mục 5, 6, 7; `docs/nghiem-thu-m1.md` cột Kết quả của các dòng này còn trống.
- Mô tả: `go test ./internal/imager/...` trên máy không có libvips báo `ok` nhưng `TestImager_PixelBombRejectedBeforeDecode`, `TestImager_OutOfMemoryKilled`, `TestImager_TruncatedDecodeFailed`, `TestExecRunner_MemoryLimit`, `TestServer_StillAnswersAfterOOM` đều SKIP. CI chỉ chạy `make ci` (không có `test-imager`, không có tag `drill`/`filesync`/`imager`). Vì vậy bằng chứng "đạt" của TP03 (một nửa), TP08, TP09, TP10 (phần container), TP01 (đường imager thật) và tiêu chí 7 (OOM) phụ thuộc người nhớ chạy tay; một hồi quy ở libvips/Dockerfile/compose sẽ không đỏ ở đâu. Không có chỗ nào trong repo ghi lần chạy gần nhất (cột Kết quả trống).
- Bằng chứng: `go test -race -count=1 ./internal/imager/ -v` (19/11) → `--- SKIP: TestImager_PixelBombRejectedBeforeDecode`, `--- SKIP: TestImager_OutOfMemoryKilled`, … `ok elimaz.com.vn/crm/internal/imager 11.307s`; `grep test-imager .github` → không có.
- Đề xuất sửa: Task M1-55 chạy `make test-imager`, `make verify-drill`/`restore-drill.sh`, `go test -tags filesync,imager ./tests/e2e/...` và ghi kết quả + ngày vào `docs/nghiem-thu-m1.md`; cân nhắc thêm job CI `test-imager` (đã có Dockerfile `--target test`); cỡ: nhỏ.

##### E-05: Threat model M1 — lỗ hổng L1 và L3 vẫn "Chưa có", không task nào làm hay ghi lý do không làm
- Mức: nhỏ
- Tệp:dòng: docs/threat-model-m1.md:70, 125-132; internal/platform/files/process.go:294-357 (`makeVariants`: không `Head` kiểm `content-type` biến thể trước `ready`)
- Spec/kế hoạch: threat-model-m1.md mục cuối: "Mỗi dòng là đề xuất cho task ghi kèm; task đó thêm test trong cùng commit, hoặc ghi lý do không làm vào tệp task của nó" — L1 → M1-43, M1-40; L3 → M1-41, M1-43.
- Mô tả: (L1) `file.process` tin kết quả `imager`; chỉ `Head` để biết khóa có hay chưa, không so `content-type`/định dạng của biến thể đã ghi, và chưa có cấu hình `X-Content-Type-Options: nosniff` cho bucket công khai (RUNBOOK chỉ đặt cho `crm-console`). (L3) Không có test nào bắt log/span của `createUpload`, `file.process` không chứa `X-Amz-Signature` (`grep -r X-Amz-Signature --include=*_test.go` → không có). Đọc mã hiện tại không thấy chỗ log URL ký (imager chỉ log trạng thái, `server.go:58-61`) nên L3 là thiếu test chứ chưa phải rò rỉ. L2 thì task-42.md:173 đã ghi lý do (chặn bằng mạng `internal`). `grep -l "L1\|nosniff" docs/tasks/task-4*.md` → không có.
- Bằng chứng: như trên.
- Đề xuất sửa: ghi quyết định cho L1, L3 (làm ở M1-55 hoặc ghi lý do chấp nhận rủi ro) và cập nhật cột "Task / test" của threat model; với L3 thêm một test log-capture; cỡ: nhỏ.

##### E-06: Màn Nhân viên khóa nút gán vai trò mà máy chủ cho phép: `canGrant` bỏ qua luật "quyền `none` thỏa ở bất kỳ grant nào"
- Mức: nhỏ
- Tệp:dòng: console/src/screens/staff/staffRules.ts:69-75; so với internal/modules/identity/roles.go:156-158 và permissions.go:137-157
- Spec/kế hoạch: docs/quyen.md "Gán vai trò cho một nhân viên: … (quyền `none`: người gán có nó ở bất kỳ grant nào …)"; CLAUDE.md "màn hình chỉ vẽ hành vi mà máy chủ thật sự làm".
- Mô tả: máy chủ kiểm `Can(actor, p, &scope)`; với quyền `ScopeNone` câu này đúng khi actor có `p` ở bất kỳ grant nào. Màn đòi `g.scope.type === "all" || (cùng loại và cùng mã đích)` cho mọi quyền, kể cả quyền `none`. Ví dụ: người gán có `identity.users.manage` toàn shop và vai trò "Kho" chỉ theo kho A; gán cho người khác vai trò tự định nghĩa chỉ có `catalog.read` phạm vi toàn shop → máy chủ cho (200), màn làm mờ lựa chọn. Màn Vai trò thì đã xử lý đúng (`roleRules.ts:22-23` có nhánh `p.scopeType === "none"`), nên hai màn quyền không nhất quán. Không có hại an ninh (màn chặt hơn), chỉ làm nhân viên không làm được việc hợp lệ.
- Bằng chứng: `staffRules.ts:72` `(g) => g.permission === p && (g.scope.type === "all" || (g.scope.type === scope.type && g.scope.id === scope.id))`; `Can`: `case kind == ScopeNone || target == nil || g.Scope == *target: return true`.
- Đề xuất sửa: `canGrant` cần biết loại phạm vi của từng quyền (đã có ở `listPermissions` → `scopeType`; hoặc trả kèm trong `listRoles`) và áp đúng ba nhánh của `Can`; thêm ca test; cỡ: nhỏ.

##### E-07: Danh sách sản phẩm: người có `inventory.read` toàn shop (Chủ, Quản lý, mọi vai trò mẫu gán toàn shop) không bao giờ thấy cột "Có thể bán" — việc treo của Task 50 chưa làm dù `listWarehouses` đã có
- Mức: lớn (lệch màn đã duyệt cho đúng người dùng chính; không hỏng dữ liệu)
- Tệp:dòng: console/src/screens/products/productRules.ts:80-89; console/src/screens/products/ProductList.tsx:55-56,138
- Spec/kế hoạch: kế hoạch M1 Task 50 (cột "Có thể bán" trên danh sách, mockup `san-pham-danh-sach`), "Xong M1 khi" mục 8 ("mọi màn hình M1 làm đúng mockup"); CLAUDE.md "màn hình chỉ vẽ hành vi máy chủ thật làm (quyền …)".
- Mô tả: `warehouseOptions` chỉ lấy kho từ grant `inventory.read` có `scope.type === "warehouse"`; grant `all` bị bỏ qua, nên Chủ/Quản lý/nhân viên được gán toàn shop có `warehouses = []` → không ô chọn kho, không cột "Có thể bán". Máy chủ thì cho họ đọc mọi kho (`listWarehouses` trả mọi kho khi `Can(u, inventory.read, kho)`, `listProducts` nhận `warehouseId`). docs/tasks/task-50.md:97-103,118-120 ghi đây là tạm thời "khi có `listWarehouses` thì thay `warehouseOptions`"; Task 51 (gộp sau, 4b752d1) đã thêm `listWarehouses` (internal/httpapi/handlers_inventory.go:57-75) nhưng `productRules.ts` không đổi. Kết quả: trong cấu hình thực tế (5 vai trò mẫu gán toàn shop) không ai thấy tồn trên danh sách sản phẩm; nhân viên gán theo kho thấy nhãn "Kho <uuid>" thay vì tên kho.
- Bằng chứng: `productRules.ts:86` `grants.filter((g) => g.permission === "inventory.read" && g.scope.type === "warehouse")`; `ProductList.tsx:138` `{warehouses.length > 0 && (`; task-50.md:100-101 "kể cả Chủ có `inventory.read` toàn shop … → không có ô chọn kho và không có cột".
- Đề xuất sửa: lấy kho từ `listWarehouses` (đã lọc theo quyền ở máy chủ) như màn Tồn kho/Phiếu kho; sửa test "không có kho trong grant" thành "API trả 0 kho"; cỡ: nhỏ.

##### E-08: Mock của e2e màn Nhập Excel (ca KH01 "e2e màn nhập") không khớp câu trả lời thật của máy chủ (tên cột, mã lỗi)
- Mức: nhỏ
- Tệp:dòng: console/e2e/imports.spec.ts:25-37, 45-50; so với internal/modules/catalog/internal/importer/template.go:20,29 và importer/validate_test.go:100-108
- Spec/kế hoạch: CLAUDE.md "mock trong test khớp quyền thật" (bẫy đã gặp); kế hoạch M1 bảng ca KH01 "e2e màn nhập".
- Mô tả: máy chủ trả `column` là tiêu đề cột của tệp mẫu, có dấu sao (`"Tên mẫu*"`, `"SKU*"`, `"Giá bán*"`) và mã `duplicate_sku_in_file`, `invalid_color_hex`, `inconsistent_product_fields`, `sku_belongs_to_other_product`… Mock dùng `"Tên mẫu"`, `"SKU"`, `"Giá bán"` (không sao) và mã `duplicate_sku`, `invalid_color`, `model_mismatch`, `sku_taken` (không có trong mã Go). Màn không rẽ nhánh theo mã nên không vỡ, nhưng ảnh chụp/so mockup ở e2e đang chụp một bảng lỗi mà máy chủ thật không bao giờ trả: màn thật hiện cột "Tên mẫu*" (`importRules.ts:60 columnText` trả nguyên chuỗi). Ca KH01 phía màn vì vậy chưa được kiểm với dữ liệu thật.
- Bằng chứng: `imports.spec.ts:27` `[5, "Tên mẫu", "missing_required", …]`, `:29` `[12, "SKU", "duplicate_sku", …]`; `validate_test.go` mong `{12, "SKU*", "duplicate_sku_in_file"}`; `grep -r '"sku_taken"' internal/modules/catalog` → không có.
- Đề xuất sửa: lấy bảy lỗi của mock từ đúng `loi-tung-dong.xlsx`/đầu ra `Validate` (hoặc copy nguyên văn), và quyết định màn có bỏ `*` khi hiện tên cột hay không; cỡ: nhỏ.

#### Bảng ca → test → có trong mã? → đã chạy? → kết quả

Lệnh: `go test -race -count=1 ./<gói> -run '<tên>' -v` (mỗi lần một gói), ngày 19/11/2026. "Không chạy (ràng buộc)" = cần docker/compose/libvips, brief cấm.

| Ca | Test (tên ở kế hoạch) | Có trong mã? (tệp:dòng) | Đã chạy? | Kết quả / ghi chú |
|---|---|---|---|---|
| KH01 | `TestValidate_SevenSeededErrors` | catalog/internal/importer/validate_test.go:93 | có | PASS — đúng 7 lỗi dòng/cột/mã, plan rỗng |
| KH01 | `TestApply_200Models` | catalog/imports_test.go:198 | có | PASS (7,2 s) — 200 mẫu, 1.000 biến thể, audit |
| KH01 | `TestApply_SameFileTwice` (+`…EndToEnd`) | catalog/imports_test.go:299, 319 | có | PASS — duplicate, không đổi dữ liệu |
| KH01 | e2e màn nhập | console/e2e/imports.spec.ts | không (playwright) | mock lệch máy chủ — E-08 |
| KH02 | `TestCreateProduct_SixVariants` | catalog/products_test.go:117 | có | PASS |
| KH02 | `TestAttachImage_OrderAndColor` | catalog/images_test.go:170 | có | PASS |
| KH02 | e2e màn sửa mẫu, ảnh | console/e2e/product-edit.spec.ts, product-images.spec.ts | không (playwright); vitest màn: có | vitest `src/screens src/app` 40 tệp, 362 ca PASS |
| KH03 | `TestPostImport_IncreasesOnHandAndLedger` | inventory/documents_test.go:90 | có | PASS |
| KH04 | `TestAvailability_Formula` | inventory/reserve_test.go:19 | có | PASS — công thức ở hàm và view |
| KH04 | `TestInventoryInvariants` (rapid) | inventory/property_test.go:29 | có | PASS (13,8 s) |
| KH05 | `TestStocktake_InTransitNotAdjusted` | inventory/stocktake_test.go:125 | có | PASS — 10/3/7 → delta 0, 0 dòng sổ |
| KH06 | `TestCreateProduct_DuplicateSKU` | catalog/products_test.go:153 | có | PASS |
| KH07 | `TestStocktake_ShortageKeepsHolds` | inventory/stocktake_test.go:169 | có | PASS — held giữ, available −2, 1 lệnh cảnh báo; lệnh `inventory.shortage_alert` được nối ở cmd/crm/main.go:361 |
| KH08 | `TestPostDamage_ReducesAvailable` | inventory/documents_test.go:175 | có | PASS — giữ 9 khi bán được 8 → ErrInsufficient |
| KH10 | `TestPostExport_CannotEatHeld` | inventory/documents_test.go:147 | có | PASS |
| KH12 | `TestVariantForSale_Discontinued` | catalog/variants_test.go:123 | có | PASS |
| KH12 | `TestCreateOrder_DiscontinuedVariant` | order/create_test.go:144 | có | PASS — đơn cũ đọc được, tên chụp giữ |
| KH13 | `TestReconcile_ReportsExactVariant` | inventory/reconcile_test.go:96 | có | PASS |
| KH14 | `TestOrderItems_SellerFK` | order/create_test.go:120 | có | PASS |
| KH14 | bộ `forEachSeller` | testkit/sellers.go:32 `ForEachSeller`; dùng ở order, inventory, fulfillment, cash, catalog | có (order) | PASS — `TestAttachShipment`, `TestApplyShipmentProgress_Flow`, `…DeliveredWithoutPickup` chạy seller-0, seller-1. Tên ở kế hoạch viết thường, mã là `ForEachSeller`/`eachSeller` |
| PQ07 | `TestReplaceGrants_*` | identity/staff_test.go:142, 169, 177, 429 | có | PASS (4 test) |
| PQ07 | `TestLock_LastOwner*` | identity/staff_test.go:210, 239 | có | PASS (Concurrent 6,5 s) |
| TP01 | `TestFileProcess_ReadyWithTwelveVariants` | platform/files/process_test.go:27 | có | PASS — imager giả, MemStore; đường imager thật ở `tests/e2e/files_pipeline_test.go` (tag `imager`): không chạy (ràng buộc) — E-04 |
| TP03 | `TestImager_RejectsHTMLAsJPEG` | imager/pipeline_test.go:439 | có | PASS |
| TP03 | `TestImager_PixelBombRejectedBeforeDecode` | imager/pipeline_test.go:452 | có | **SKIP** (không có libvips; chỉ `make test-imager` trong docker) — E-04 |
| TP03 | `TestCreateUpload_TooLarge` | platform/files/upload_test.go:22 | có | PASS (6 ca con) |
| TP05 | `TestOriginalURL_ExpiresAndNeedsPermission` | httpapi/handlers_catalog_original_test.go:38 | có | PASS — 5 phút, 403 cho catalog.read, 404 tenant khác; phần "hết hạn thật" ở `TestOriginalURL_ExpiresOnStore` (files/original_url_test.go:60): PASS trên SeaweedFS dev (403 sau 3 s) |
| TP06 | `TestImageSoftDeleteRestore` | catalog/images_test.go:317 | có | PASS — quá 30 ngày → ErrRestoreExpired |
| TP07 | `TestRegeneratePresetV2` | platform/files/regenerate_test.go:194 | có | PASS |
| TP08 | `drill: TestFileStoreCrash` | tests/drills/filestore_crash_test.go:39 (tag `drill`, `docker kill`) | không (ràng buộc) | — E-04 |
| TP09 | `scripts/files/restore-drill.sh` | có (cùng `drill-lib.sh`, `crash-drill.sh`) | không (ràng buộc) | — E-04 |
| TP10 | `TestRule_FileSyncAge` | platform/observability/rules_filesync_test.go:40 | có | PASS (+`AnyBucket`, `FileSyncFailed`) |
| TP10 | `filesync_test` | tests/e2e/filesync_test.go:282 `TestFileSync` (tag `filesync`) | không (ràng buộc) | — E-04 |
| TP12 | `TestVerify_MarksLost` | httpapi/handlers_files_verify_test.go:142 | có | PASS |
| TP12 | e2e màn tệp mất | console/e2e/lost-files.spec.ts; ô "Ảnh bị mất" ở ImageManager.tsx:426,518, ProductList.tsx:457 | không (playwright) | — |
| TP11 | — | — | — | còn mở (O1), đúng kế hoạch |
| ngoài mã ca | `TestSupplier_OnImportOnly` | inventory/suppliers_test.go:55 | có | PASS |
| ngoài mã ca | `TestArticle_RejectsUnknownBlock` | catalog/products_test.go:351 | có | PASS |
| ngoài mã ca | `TestCustomRole_CannotExceedActor` | identity/roles_custom_test.go:79 | có | PASS |
| Xong 1, 4 | `TestMotDonDiTron`, `TestMotDonDiTron_HaiDonSongSongTon1` | tests/e2e/mot_don_di_tron_test.go:41, 123 | có | PASS (5,0 s; 3,0 s) — nhập 5, đơn 2, còn 3, COD đúng; hai đơn song song → một 201, một 409 |
| Xong 7 | `TestImager_HangKilledWithinDeadline`, `TestFileProcess_HangDoesNotBlockWorker`, `TestExecRunner_TimeoutKillsProcessGroup` | imager/pipeline_test.go:546; files/process_test.go:235; imager/runner_test.go:48 | có (imager) | PASS ở imager; phần OOM (`TestImager_OutOfMemoryKilled`, `TestServer_StillAnswersAfterOOM`) SKIP — E-04 |

Mọi tên test ở cột cuối bảng kế hoạch đều có trong mã (C10 đạt); không thấy test nào chỉ kiểm bề mặt trong các ca đã đọc (KH01, KH04, KH05, KH07, KH08, KH10, KH12, TP05, TP06, TP12). `docs/nghiem-thu-m1.md`: cột Kết quả chỉ có tiêu chí 1, 4; các dòng còn lại trống vì Task M1-55 (số toàn cục 56, Q29) chưa gộp.

#### Kiểm tên mà mốc sau dựa vào (mục 2) — kết quả

- `docs/views.md` ↔ database: `TestViewsDocMatchesDatabase` PASS (mọi view `_v` trong DB có dòng, đúng cột theo thứ tự); 21 view M1 (`identity.users_v` … `cash.cod_balances_v`) khớp danh sách Task 25 của kế hoạch từng cột.
- `security_invoker`: mọi `CREATE VIEW` trong 01xx/02xx có `WITH (security_invoker = true)`; `TestSchemaViewsAndRules` (order, chạy `CheckSchemaRulesClean` trên DB đủ migration) PASS → luật (a)–(g) rỗng.
- ADR-46: 35 bảng tạo trong migration 01xx đều có dòng trong `docs/adr/0046-thu-tu-khoa.md` (script so `CREATE TABLE` với tên trong tệp: 35/35 OK); thứ tự nhóm 4, 7, 8, 9, 11, 12b khớp bảng Global Constraints; khóa tư vấn của M1-56, M1-47b có ghi. `TestLockOrderFrame`, `TestLockOrderGroupCodes` PASS.
- Quyền: `TestPermissionsDoc_MatchesCode` PASS (khóa, loại phạm vi, vai trò mẫu khớp `identity.Permissions`/`DefaultRoles`). Mọi operation M1 (catalog, inventory, files, identity, platform/files, orders, shipments trong routes_gen.go) có `x-permission` đúng quyền kế hoạch ghi (so từng dòng với kế hoạch dòng 426, 509, 629, 660, 703, 781, 882, 905, 1017, 1043, 1164, 1289, 1336, 1821); chỉ `/me` là `x-authenticated` (Task 315). Lệch đã duyệt: `platform.files.manage`, `identity.roles.manage` thành `toàn shop` (Task 315 lượt 2, ghi ở quyen.md).
- Hàm, kiểu công bố: `RenderArticleHTML`, `ArticleHTML`, `files.{Register,RegisterPrivateImage,DownloadURL,PublicVariants,MarkDeleted,Status,ServerKey}`, `FileStore.Put`, `testkit.{MemFileStore,SeedSellers,ForEachSeller}`, `fulfillment.NewShipmentCode`, `order.nextCode`, `order.Writer.CreateManual`, `inventory.Actor`, `identity.{EnsureDefaultRoles,SyncDefaultRoles,ListStaff,CreateStaff,Can}`, trạng thái `creating`, cột `external_account_ref`, `fee`: đều có. Lệch: E-03.
- Sự kiện công bố: 7 topic ở views.md đều có hằng trong mã (`catalog/events.go:17-18`, `inventory/movement.go:95`, `order/events.go:16-18`, `fulfillment/create_shipment.go:32`); aggregate `balance` + mã `<variant>:<warehouse>` khớp.

#### Ảnh hưởng của M2 lên giả định M1 (mục 5)

Đã xem diff của các merge M2 có trong W (57, 58, 83, 64, 65, 59, 60, 61, 62, 90, 72) với `migrations/02xx`, `docs/views.md`, `docs/quyen.md`, ADR-46, mã M1:
- Không `CREATE` lại bảng M1; mọi thay đổi bảng M1 là `ALTER` mở rộng, ràng buộc `NOT VALID` → `VALIDATE`. Thu hẹp có dòng `-- contract:` (0204:146 `warehouse_id SET NOT NULL` sau khi điền; 0201:57 `code SET NOT NULL`). Bỏ ràng buộc chỉ là nới (`orders_check1`, `order_status_history_actor_type_check`, `stock_reservations.order_item_id DROP NOT NULL` + FK `ON DELETE SET NULL`, có dòng ở views.md "Khóa ngoại xuyên module").
- View M1 (`customer.customers_v`, `ord.orders_v`) chỉ nối cột vào cuối; `TestViewsDocMatchesDatabase` PASS.
- ADR-46: M2 chỉ chèn bảng vào nhóm có sẵn (5, 7, 10) và thêm khung M3/M4; thứ tự bảng M1 trong mỗi nhóm giữ nguyên.
- quyen.md: M2 thêm 14 quyền; dòng M1 chỉ đổi `order.read` thêm vai trò `warehouse` (ghi "Kho nhận từ M2" — thêm vai trò nhận là cho phép).
- Chữ ký Go M1: `order.Get`, `CreateManual`, `ApplyShipmentProgress`, … giữ nguyên; `Order`, `Item`, `ShipAddress` chỉ thêm trường; `CreateManual` nay gộp dòng trùng biến thể (`mergeLines`, do unique `order_items_variant_key` mới) — thay đổi hành vi nhưng tương thích; test M1 của order (KH12, KH14, shipment progress) và e2e một đơn đi trọn vẫn PASS.
- Không thấy M2 làm hỏng giả định M1. Ghi chú: main lúc soát đã có thêm `ac53f01` (Task 63) và `811a025` (Task 66) sau a3f7be3 — không có trong W nên không soát.

#### Đã kiểm, không thấy lỗi

- C10: mọi tên test của bảng ca nghiệm thu và ba test "ngoài mã ca" có trong mã; 33 test chạy được trên máy dev đều PASS (bảng trên).
- Nội dung test các ca KH01, KH04, KH05, KH07, KH08, KH10, KH12, KH14 (`TestOrderItems_SellerFK`), TP05 (cả hạn thật trên SeaweedFS), TP06 (quá 30 ngày), TP12 (đánh dấu mất, tải lại về ready, tenant khác không lộ) kiểm đúng điều ca yêu cầu, không chỉ bề mặt.
- Tiêu chí Xong 1 và 4 chạy lại cục bộ PASS (e2e với ingest/api/worker và hãng giả thật).
- views.md ↔ DB ↔ kế hoạch; luật (f) security_invoker trên DB đủ migration; ADR-46 đủ dòng cho mọi bảng M1; quyen.md ↔ mã; x-permission ↔ kế hoạch; sự kiện công bố.
- Threat model M1: đối chiếu từng dòng có biện pháp + test: `createUpload` cần `files.upload`; `size_mismatch` (files/upload_test.go); `ErrObjectExists`/`CopyIfAbsent`; tên tệp không vào khóa (`original_name` test); SVG 422, HTML-as-JPEG; tenant khác 404; `TestCreateUpload_TooLarge`; xóa chỉ dấu xóa (s3store_test, probe); imager không bí mật/không tới postgres/chỉ đọc (`internal/imager/compose_test.go`, tag riêng); treo/giết nhóm tiến trình; bom điểm ảnh (SKIP ở dev); `TestProcessPrivate_NeverInPublicBucket`, `TestFileProcess_RefusesPrivatePurpose`; `TestRule_FileSyncAge`; `crm_filesync` chỉ INSERT (tests/deploy/compose_test.go, filesync_test); bom zip, `formula_not_allowed`, `HYPERLINK`/`Sanitize`, fuzz `Read`; webhook hãng giả 401/404 production/`inbound_unrouted`. Imager và `files` không log URL ký (server.go:58-61 chỉ log trạng thái). Lỗ hổng còn treo: E-05 (L1, L3); L2 có lý do ghi ở task-42.md:173; L4 chờ chủ shop.
- Màn hình (chỉ phần quyền): route của mọi màn M1 đòi đúng quyền đọc chính của operation (products `catalog.read`, imports `catalog.import`, inventory `inventory.read`/`inventory.stocktake`, staff `identity.users.manage`, roles `identity.roles.manage`, lost-files `platform.files.manage`, health/errors `platform.health.read`); ProductEdit `catalog.manage`/`files.upload`; Kiểm kho dùng `canAt` theo kho; màn Vai trò `canChange` đúng luật `none`/`all`. Lệch: E-01, E-02, E-06, E-07, E-08.

### Lời giao chung cho năm reviewer

#### Review độc lập M1 — lời giao chung

Bạn là reviewer ĐỘC LẬP của mốc M1 (Sản phẩm và kho; kho tệp và xử lý ảnh) của một CRM Go + PostgreSQL + React.
Bạn chỉ có spec, kế hoạch và mã. Không có lập luận của người viết; đừng tin mô tả trong `docs/tasks/*.md` hay commit message — kiểm bằng mã và test.

#### Nguồn
- Repo (git worktree, nhánh task-404 = main hiện tại): `apps/crm` (gọi tắt W). Luật dự án: `W/CLAUDE.md`, `W/docs/quy-trinh-lam-viec.md` (mục 5, 6), `W/.claude/agents/reviewer.md` (7 mục soát).
- Spec 2.1: `docs/ (gốc repo ELIMAZ)/crm/specs/2026-11-10-crm-rieng/` — ADR trong `07-adr.md` (ADR-07, 14, 18, 24, 25, 27, 29, 36, 41, 44, 45, 46); `04-kien-truc.md` mục 5.5, 11, 16; `05-du-lieu-erd.md` mục 2, 6, 7.3; `11-kich-ban-kiem-thu.md` (ca có Mốc = M1).
- Kế hoạch M1: `docs/ (gốc repo ELIMAZ)/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Quyết định thay chủ shop Q1–Q38, "Tiêu chí nghiệm thu → ca kiểm thử" dòng ~107, Global Constraints ~236, Review Focus ~276, rồi từng task).
- `W/docs/threat-model-m1.md`, `W/docs/views.md`, `W/docs/quyen.md`, `W/docs/adr/0046-thu-tu-khoa.md`.
- Phạm vi M1 trên main: các commit gộp `merge: Task NN` với NN ∈ 24–56, 315, 319–334, 389–401 (`git log --first-parent --oneline main | grep "merge: Task"`; xem một task: `git show -m --first-parent --stat <merge>` / `git diff <merge>^1 <merge>`). main đã có thêm M2 (task 57–90; migration 02xx): KHÔNG review M2, nhưng GHI nếu M2 làm hỏng giả định của M1 (đổi chữ ký, CREATE lại bảng M1, sửa view, thứ tự khóa...).

#### Ràng buộc
- CHỈ ĐỌC. Không sửa, không tạo tệp trong W, không git commit/stash/checkout. Chỉ ghi tệp kết quả của bạn (đường dẫn nêu ở phần riêng).
- Được chạy test Go để xác minh: `cd W && go test -race -count=1 ./internal/<gói>/... -run <Tên>` (DB test đã cấu hình qua biến môi trường CRM_TEST_DATABASE_URL; test tự tạo database riêng). KHÔNG chạy make ci, make test, make db-up/db-down/db-reset, docker compose; KHÔNG pkill/killall. Mỗi lần chạy chỉ một gói.
- Chỉ báo phát hiện về TÍNH ĐÚNG, an ninh, mất dữ liệu, đồng thời, lệch spec/kế hoạch, test thiếu cho ca nghiệm thu, tên mà mốc sau dựa vào bị lệch. Bỏ qua phong cách.
- Mỗi phát hiện phải kiểm chứng bằng đọc mã thật (trích dòng) — nếu chỉ nghi ngờ, ghi mức "nghi ngờ".

#### Đầu ra (tiếng Việt, Markdown) — ghi vào tệp được giao, rồi trả lời ngắn (≤ 15 dòng) tóm tắt số phát hiện theo mức.
Mỗi phát hiện:
```
### <mã>: <tiêu đề một dòng>
- Mức: nghiêm trọng | lớn | nhỏ | nghi ngờ
- Tệp:dòng: <đường dẫn tương đối W>:<dòng>
- Spec/kế hoạch: <mục bị lệch>
- Mô tả: <điều sai, tình huống cụ thể gây sai>
- Bằng chứng: <trích mã / lệnh đã chạy và kết quả>
- Đề xuất sửa: <ngắn>; cỡ: nhỏ (<50 dòng, 1 gói) | lớn
```
Cuối tệp: mục "Đã kiểm, không thấy lỗi" liệt kê những gì đã soát mà ổn (để biết độ phủ).

## Quyết định cho Task 406, 409, 410, 411 (19/11, chủ shop giao điều phối viên tự chọn phương án an toàn)

Chủ shop trả lời trong chat 19/11: "OK, mày tự chọn". Mỗi quyết định ghi cách đảo ngược.

- **406 (A-01):** giữ Q8 — kiểm kho ghi đúng số đếm thật, kể cả thấp hơn phần đang giữ. Ở bước lấy hàng/giao (Consume): nếu tồn không đủ cho phần giữ của đơn thì **chặn** bằng lỗi nghiệp vụ rõ ràng (`stock_short`, 422/409 theo errors.go) thay vì lỗi CHECK 500; không trừ âm, không tự nhả giữ, không tự hủy đơn. Sau khi áp phiên kiểm kho làm on_hand < phần giữ: trả kèm danh sách đơn đang giữ bị thiếu để nhân viên xử lý (điều chỉnh, đổi kho, hủy). Đảo ngược: chọn phương án "Consume trừ tối đa phần còn và ghi lệch".
- **409 (C-02, C-07):** lỗi tạm của imager (hết giờ, mất kết nối, 5xx, imager khởi động lại) **không** tính vào 3 lượt của lỗi ảnh; thử lại theo lùi dần có trần (ví dụ tối đa 24 giờ), ảnh ở trạng thái `processing` (ghi thật trạng thái này) trong lúc chờ; chỉ lỗi do chính tệp ảnh (không đọc được, sai định dạng, quá cỡ) mới thành `rejected`. Hết trần thời gian thì `rejected processing_failed` và cảnh báo. Đảo ngược: quay về Q22 cũ (3 lượt cho mọi lỗi).
- **410 (C-01, C-03, C-06, C-08):** chỉ sửa tệp trong repo, **không** deploy, không vào máy chủ: tách mạng compose để imager không tới được filer/master/volume của SeaweedFS (imager chỉ nhận/trả dữ liệu qua worker); thêm dịch vụ `filesync` vào `docker/compose.prod.yml` (ghim bản, biến môi trường, không bí mật trong repo); tiền tố bucket đọc từ biến môi trường thay vì viết cứng `crm-`; thêm phép đo/test xác minh crm-tmp xóa sau 24 giờ trên compose dev. Đảo ngược: revert commit.
- **411 (D-02):** mặc định từ chối — lên đơn chỉ giữ hàng được ở kho nằm trong phạm vi kho của nhân viên (quyền tạo đơn theo phạm vi kho, giống phiếu kho); ngoài phạm vi trả 403 như các đường có phạm vi khác; ghi vào `docs/quyen.md`; màn Lên đơn (M2-27) chỉ cho chọn kho trong phạm vi. Chủ shop/Quản lý toàn shop không bị ảnh hưởng. Đảo ngược: bỏ kiểm phạm vi ở handler.
