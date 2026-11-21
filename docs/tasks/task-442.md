# Task 442 — Sửa lỗi có sẵn: phiếu hoàn của hãng hủy được; view shipments_v cột tỉnh trống

Kế hoạch: task chung của điều phối viên (21/11, nhóm 1 "nền và sửa lỗi"), không có trong kế hoạch M1; số toàn cục
`x-fix` (đã thêm dòng `| chung (21/11) | x-fix | 442 | Phiếu hoàn hãng hủy được; shipments_v cột tỉnh |` vào
`../../docs/superpowers/plans/danh-so-task-toan-cuc.md`, tệp ngoài repo crm). Loại việc (quy trình mục 3): A

Gốc: (1) Task 117, Việc phát hiện thêm (reviewer lượt 1): `CancelReturn` hủy được phiếu `carrier_return` requested →
hàng hoàn của hãng kẹt `in_transit` (vận đơn đã `returning`, phiếu hủy rồi thì không còn đường nhận về kho), đơn kẹt
confirmed. (2) Task 108, Việc phát hiện thêm: view `fulfillment.shipments_v` đọc `ship_to ->> 'province_code'`
(`migrations/0300_fulfillment_m3.sql:341`) mà `shipToDoc` (`area_create.go`) ghi khóa `provinceCode` → cột luôn NULL.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. Đổi hành vi `order.Writer.CancelReturn`
  (`internal/modules/order/returns.go`): phiếu `carrier_return` → `ErrReturnNotAllowed` như phiếu `partial_delivery`
  (Task 117). Chỗ gọi ngoài test: 1 (`internal/httpapi/handlers_order_returns.go` `CancelReturn`, đường
  `POST /api/v1/returns/{returnId}/cancel`); chỗ gọi trong test: `returns_test.go` (`cancelReturn` helper, dùng ở
  `returns_test.go`, `returns_property_test.go`, `returns_qty_customer_test.go`, `returns_race_test.go`,
  `partial_return_test.go`) — các ca đó hủy phiếu customer_return/exchange, không đổi. `ErrReturnNotAllowed` đã có ánh
  xạ 422 `return_not_allowed` ở `errors.go` (không sửa tệp đó).
- Gói phụ thuộc ngược (go list): order ← cmd/crm, httpapi, cash (+internal/refunds), fulfillment, testkit/codtest,
  testkit/shiptest. Chữ ký không đổi nên chỉ build lại; test chạy order, httpapi (đường cancelReturn), fulfillment (luồng
  hoàn), cash (consumer phiếu).
- Operation OpenAPI bị chạm, oasdiff: không đổi yaml; `cancelReturn` đã khai `return_not_allowed` (422).
- Chỗ dùng trên màn hình: `console/src/screens/don/ReturnsCard.tsx` hiện nút "Hủy phiếu" cho mọi phiếu requested →
  ẩn nút với phiếu `carrier_return` và `partial_delivery` (máy chủ từ chối; bẫy "chỉ vẽ hành vi máy chủ làm"). Test:
  `console/src/screens/don/OrderDetail.test.tsx` (ca phiếu trả có sẵn) thêm ca hai loại phiếu không có nút.
- Câu sqlc, bảng, pha migration: không câu sqlc mới. Migration mới `0331_fulfillment_shipments_v_province.sql`:
  `CREATE OR REPLACE VIEW fulfillment.shipments_v` giữ nguyên 35 cột, thứ tự, kiểu (text), `security_invoker`; chỉ đổi
  `ship_to ->> 'province_code'` → `ship_to ->> 'provinceCode'`. Pha: mở rộng (không thu hẹp, không cần `-- contract`).
  View phụ thuộc: không view nào trong `migrations/` đọc `shipments_v` (grep; không viết test `pg_depend` — `CREATE OR REPLACE VIEW` giữ nguyên cột nên view phụ thuộc, nếu có, không vướng).
  Nơi đọc `shipments_v`: `queries/cash/*.sql`, `internal/queries/picking_dynsql.go` — không đọc `province_code` (grep),
  nên không đổi kết quả của chúng. GRANT giữ (CREATE OR REPLACE giữ quyền). `docs/views.md` dòng `shipments_v` ghi
  `province_code = ship_to->>'province_code'` → sửa thành `provinceCode`.
- Test đổi để gộp đoạn trùng (jscpd, không đổi điều test kiểm): `internal/httpapi/partial_delivery_test.go` thêm helper
  `shipAPIEnv.returnOf(shipment, kind)` (dùng ở `TestRecordPartialDeliveryAPI` và `TestCancelCarrierReturnAPI`);
  `shipment_manual_test.go` thêm `shipAPIEnv.grantManual()` (dùng ở `TestShipmentManualAPI` và `TestCancelCarrierReturnAPI`).
- Test sẽ chạy: `go test -race ./internal/modules/order/... -run 'CancelReturn|Return'`,
  `./internal/modules/fulfillment/... -run 'SchemaM3|ShipmentsV'`, `./internal/httpapi/... -run 'Return|ShipmentManual'`,
  `cd console && npm test -- OrderDetail`, tsc, eslint; rồi `ci-task.sh` (đổi migration → toàn bộ test).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không cần hàm chung);
  order: nhánh chặn `partial_delivery` có sẵn trong `CancelReturn` (Task 117), sentinel `ErrReturnNotAllowed`, khung
  `changeReturn`; test: `openCarrierReturn`, `pickedUpDH01`, `cancelReturn`, `balanceIs`, `getReturn`, `carrierReturnFor`;
  fulfillment test: `shipEnv.order`, `ship`, `ownerExec`, `app`; httpapi test: `shipAPIEnv`, `manualAccount`,
  `manualStatus`, `confirmedOrder`, `post`, `serveAs`, `decodeJSON`; testkit `CheckViews`.
- Dùng lại / viết mới vì: dùng lại hết; chặn hủy gộp vào điều kiện có sẵn (một `slices.Contains`/so hai loại) thay vì
  nhánh mới; không hàm mới ngoài test.

## Soát thiết kế

- 5.1–5.3: phiếu trả thuộc order (chặn trong `order.Writer.CancelReturn`); view thuộc fulfillment (migration của
  fulfillment). 5.4: chặn trước mọi thay đổi trong transaction có sẵn của `changeReturn` (khóa đơn → phiếu, kiểm trạng
  thái, rồi `change`): lần từ chối lùi cả tx, không đổi gì. 5.5–5.6: không sự kiện mới, không gọi ra ngoài. 5.7: view
  `security_invoker` giữ RLS của bảng; test đọc bằng crm_app trong tenant. 5.8: không đổi handler (lỗi đi qua
  `errors.go` có sẵn). 5.9: đường `cancelReturn` giữ quyền `order.return`, Idempotency-Key. 5.10–5.11: không Valkey,
  không thư viện. 5.12: theo mẫu chặn của Task 117 (cùng sentinel, cùng chỗ).
- 6.1–6.5: không khóa mới; chặn chạy dưới khóa đơn → phiếu có sẵn (ADR-46), đọc `kind` của phiếu đã khóa (kind không đổi
  sau tạo). 6.4: hàm `change` không cộng dồn biến. 6.6–6.9: không áp dụng (không consumer, không gọi ra ngoài). 6.10:
  tiền: chặn hủy giữ đúng sổ tồn (in_transit chỉ về kho khi kho nhận phiếu) và tiền COD (cash nghe `order.returned` khi
  nhận); không đổi số. 6.11: không quy luật mới (một nhánh điều kiện; test bảng hai loại bị chặn, hai loại vẫn hủy
  được do test có sẵn). 6.12: view không thêm dữ liệu cá nhân (chỉ mã tỉnh, như cột đã công bố). 6.13: quyền không đổi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước khi có mã (lỗi 1, phía đơn) | `TestCancelReturn_CarrierRefused` | `go test -race -count=1 ./internal/modules/order/ -run TestCancelReturn_CarrierRefused` | trước sửa: `hủy phiếu hoàn của hãng: <nil>; muốn ErrReturnNotAllowed` FAIL; sau: `ok …/order 3.121s` |
| Lỗi 1 qua HTTP: vận đơn tay shipping → returning tạo phiếu carrier_return; cancelReturn → 422 `return_not_allowed`, phiếu vẫn requested cùng version | `TestCancelCarrierReturnAPI` | `go test -race -count=1 ./internal/httpapi/ -run 'TestCancelCarrierReturnAPI\|TestRecordPartialDeliveryAPI\|TestShipmentManual'` | bỏ tạm nhánh chặn: `= 200 … status:cancelled …, muốn 422 return_not_allowed` FAIL; có nhánh: `ok …/httpapi 2.967s` |
| Lỗi 1, phía đơn: phiếu vẫn requested, đơn confirmed cùng version, tồn V1, V2 không đổi; sau đó kho vẫn nhận phiếu → restocked | `TestCancelReturn_CarrierRefused` | như trên; `go test -race -count=1 ./internal/modules/order/ -run 'CancelReturn\|Return'` | `ok …/order 48.002s` (các ca hủy customer_return, exchange có sẵn vẫn qua) |
| Màn Đơn: phiếu carrier_return, partial_delivery chờ nhận có "Nhận hàng trả", không có "Hủy phiếu"; customer_return vẫn có "Hủy phiếu" (ca có sẵn) | `OrderDetail.test.tsx` "phiếu %s chờ nhận: … không có Hủy phiếu" (2 ca it.each), "thiếu order.return.receive…" | `cd console && npx vitest run src/screens/don/`, `npx tsc --noEmit`, `npx eslint src/screens/don/`, `npx prettier --check` | `returnCancellable` luôn true: 2 ca × FAIL; sau sửa `Tests 59 passed (59)` (sau gộp main); tsc, eslint, prettier sạch |
| Lỗi 2: test thất bại trước migration; sau 0331 đơn có mã tỉnh 79 → `province_code` "79", đơn không mã tỉnh → NULL (crm_app, tenant, qua `Ship` thật) | `TestSchemaM3_ShipmentsVProvince` | `go test -race -count=1 ./internal/modules/fulfillment/ -run TestSchemaM3` | trước: `có mã tỉnh: province_code = NULL, muốn 79` FAIL; sau: `ok …/fulfillment 3.846s` |
| View giữ đúng 35 cột, thứ tự, security_invoker; RLS giữ | `TestSchemaM3_Views`, `TestSchemaM3_ViewsTenantIsolation`, `tests/arch` (viewsdoc so docs/views.md) | như trên; ci-task | `ok` |
| Đoạn trùng | jscpd | `jscpd --config .jscpd.json --similarity --baseline jscpd-baseline.json --fail-on-new-clones=0 .` | lần 1: 2 đoạn trùng mới (test) → gộp `returnOf`, `grantManual`; sau: `Found 0 clones.` |
| Mọi cổng tĩnh + toàn bộ test (đổi migration) sau gộp main (Task 436) và gộp đoạn trùng, đã stage | ci-task lần 2 | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 99c55cc…)`, 426 giây; `ok order 287.774s`, `ok fulfillment 147.080s`, `ok inventory 124.195s`, `ok cash 54.372s`, `ok httpapi 76.671s`, `ok tests/arch 26.917s`, `ok tests/e2e 49.870s`; console-check xanh. Lần 1 (chưa stage) ĐỎ: gen-check (do chưa stage), jscpd 2 đoạn trùng (đã gộp), `TestStuck_CreatingAndStockShort` chập chờn (Việc phát hiện thêm) |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

1. **Mã lỗi khi hủy phiếu hoàn của hãng: 422 `return_not_allowed`** (sentinel `ErrReturnNotAllowed`, đã có trong hợp đồng
   `cancelReturn` và `errors.go`), không phải 409 `invalid_transition` như lời giao gợi ý "ví dụ". Lý do: cùng một chỗ
   chặn, cùng mã với phiếu `partial_delivery` (Task 117, Quyết định 10) và với các ca "phiếu không hủy được" có sẵn;
   không sửa `errors.go` (Task 441 đang tách). Phương án khác: sentinel mới → 409 — phải sửa `errors.go`, hai loại phiếu
   cùng luật mà khác mã. Đảo ngược được: đổi sentinel ở một dòng.
2. **Phiếu hoàn của hãng không có đường hủy nào** (kể cả quản lý). Hàng thật sự mất thì kho nhận phiếu với số "thiếu"
   (ghi thất lạc, có sẵn từ M3-24). Phương án khác: cho hủy kèm nhả in_transit và đưa đơn về trạng thái trước — là thiết
   kế mới (đụng tồn, COD), cần chủ shop chốt. Đảo ngược được: bỏ điều kiện `ReturnCarrier`.
3. **Màn Đơn ẩn nút "Hủy phiếu"** với phiếu `carrier_return` và `partial_delivery` (trước đây hiện mà bấm sẽ lỗi, cả với
   phiếu giao một phần của Task 117). Không thêm câu giải thích trên màn (không đổi giao diện ngoài việc bớt một nút).
4. **View: đọc đúng khóa `provinceCode`**, không `coalesce` với khóa cũ: chỉ một bên ghi `ship_to` (`insertForShip`, luôn
   `provinceCode`), nên dữ liệu đã có cũng đúng ngay sau migration. Số migration 0331 theo lời giao.

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `TestStuck_CreatingAndStockShort` (`internal/modules/fulfillment/lists_stuck_test.go:309`, "lệnh tạo = pending, muốn
  in_doubt") đỏ một lần trong ci-task lần 1 khi máy bận (toàn bộ test, nhiều phiên song song); chạy riêng 3/3 lần xanh.
  Task này không chạm luồng tạo vận đơn, hàng chờ hay test đó. Có vẻ phụ thuộc thời gian chờ quá giờ của hãng giả: nên
  đưa vào đợt test chập chờn (như Task 440/443).

## Reviewer

Lượt 1 (agent reviewer trên `git diff --cached`, 12 tệp, sau gộp main Task 436): **ĐẠT**.

- Phát hiện phải sửa: không có. Đã kiểm: `CancelReturn` có 1 chỗ gọi ngoài test; test có sẵn chỉ hủy phiếu
  customer_return/exchange nên không gãy; ngoài 0118, 0300 không migration nào định nghĩa lại `shipments_v`, 0331 giữ
  35 cột đúng thứ tự; chỉ `shipToDoc` ghi `ship_to` (khóa `provinceCode`); ở console chỉ `ReturnsCard` có nút hủy phiếu
  trả; không test bị tắt, không sửa mã sinh, migration cũ hay `errors.go`.
- Ghi chú không bắt buộc: (1) mục Ảnh hưởng ghi "kiểm bằng `pg_depend` trong test" mà không có test đó — đã sửa câu thành
  kiểm bằng grep; (2) `deref` trong `schema_m3_test.go` không trùng tên trong gói — giữ.

Kết luận: ĐẠT
