# Task 79 — M2-22: Trả hàng, đổi hàng, phiếu hoàn của hãng (phía đơn)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 79 = M2-22). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn nào (0 chỗ tham chiếu phải sửa). Chỉ
  thêm. Gói `order`: `Return`, `ReturnItem`, `ReturnLineInput`, `CreateReturnInput`, `ReceiveLine`, `ReturnFilter`,
  `ReturnCursor`, `ParseReturnCursor`, `ReturnReceived`, hằng loại/trạng thái/tình trạng phiếu, `TopicOrderReturned`,
  `TopicOrderPartiallyReturned`, `Writer.CreateReturn`, `Writer.OnShipmentReturning`, `CarrierReturnFor`,
  `Writer.ReceiveReturn`, `Writer.CloseReturn`, `Writer.CancelReturn`, `GetReturn`, `ListReturns`. Gói `inventory`:
  `ReturnLine`, `Writer.ReleaseInTransitByItems`, `Writer.ReceiveCustomerReturn`, `Writer.MoveToDamaged`. Sentinel
  `ErrReturnQtyExceeds`, `ErrReturnNotAllowed` đã có (Task M2-02) và đã ánh xạ ở `internal/httpapi/errors.go` (422).
- Sửa nhỏ ở mã có sẵn (cấu trúc, không đổi hành vi): `internal/httpapi/handlers_order.go` (tách `shipAddressIn`),
  `internal/httpapi/handlers_order_cancel.go` (`CancelOrder` dùng `orderWriteAsDefaultSeller`; test
  `TestOrderAPI_Cancel` vẫn qua).
- Gói phụ thuộc ngược (go list): `internal/modules/order` ← `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`,
  `internal/testkit/shiptest`; `internal/modules/inventory` ← `order`, `httpapi`, `fulfillment`, `cmd/crm` và các gói
  test. order, inventory là vùng rủi ro cao → chạy cả bộ của hai gói.
- Operation OpenAPI bị chạm, oasdiff: `createReturn`, `listReturns`, `getReturn`, `receiveReturn`, `closeReturn`,
  `cancelReturn` (hợp đồng có sẵn từ Task M2-02, đang 501). Chỉ bỏ dòng `description` "Chưa làm…": không phá vỡ;
  `make gen` đổi chú thích trong `internal/httpapi/gen/oas_{handlers,server,unimplemented}_gen.go`. Audit mới:
  `order.return.create`, `order.return.receive`, `order.return.close`, `order.return.cancel`. `docs/views.md`: thêm
  dòng sự kiện `order.partially_returned`/`order.returned` (payload `ReturnReceived`).
- Chỗ dùng trên màn hình: chưa có (hộp thoại trả hàng ở Task M2-26 `ReturnDialog.tsx`); `console/src/api/**` sinh lại
  theo `make gen` (chỉ chú thích đổi, nếu có).
- Câu sqlc, bảng, pha migration: không migration (bảng `ord.returns`, `ord.return_items`, sequence, quyền cột đã có ở
  0204). Tệp mới `queries/ord/returns.sql` (khóa dòng đơn, phiếu, dòng phiếu; chèn phiếu, dòng; đổi trạng thái phiếu;
  tăng version đơn khi nhận phiếu khách trả; đọc, danh sách). inventory dùng lại `ApplyDelta`, `EnsureBalance`,
  `ReservationIDsByItems` có sẵn.
- Test sẽ chạy: `go test -race ./internal/modules/order/... ./internal/modules/inventory/...` (cả bộ vùng rủi ro cao),
  `go test -race ./internal/httpapi/ -run 'Return|M2Routes|Error'`, `./tests/arch/...`; cổng ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục order, inventory,
  queries (con trỏ trang); tìm `RefundFor`, `applyTransition`, `lockOrder`, `byItems`, `applyDelta`, `nextReturnCode`,
  `Writer.Cancel`, `Writer.Create`, `ListCursor`, `queries.Cursor`.
- Dùng lại / viết mới vì: dùng lại `pricing.RefundFor` (tiền hoàn từng dòng, đơn vị cuối nhận phần lẻ),
  `nextReturnCode` (mã TH), `lockOrder`/`lockedOrder`/`applyTransition` (khóa đơn, đơn sang returned), `Writer.Create`
  (đơn thay thế), `Writer.Cancel` (hủy đơn thay thế khi hủy phiếu đổi), `inventory.byItems`/`transition` (nhả giữ hàng
  in_transit theo dòng đơn), `applyDelta`/`changed` (đổi số dư, sổ, stock.changed), `EnsureBalance`. Viết mới: con trỏ
  trang phiếu trả (`ReturnCursor`, chỉ mã phiếu uuidv7) — cùng kiểu `customer.ListCursor` nhưng thuộc module order, khác
  lý do thay đổi nên không gộp vào `queries.Cursor` của catalog.
- jscpd (lần chạy ci-task đầu báo 7 đoạn trùng mới) — đã gộp hết: `shipAddressIn` (địa chỉ giao của hợp đồng →
  `order.ShipAddress`, dùng ở `createOrderInput` và đơn thay thế), `orderWriteAsDefaultSeller` (đường ghi order với người
  bán mặc định và người làm, dùng ở `CancelOrder` và bốn handler ghi phiếu — cùng mục đích, cùng lý do thay đổi),
  `changeReturn` (khung khóa → kiểm trạng thái → đổi → báo hiệu → đọc lại của nhận, đóng, hủy phiếu), helper test
  `openCarrierReturn`. Lần chạy lại: `Found 0 clones.`

## Soát thiết kế

5.1 Chủ dữ liệu: phiếu trả, dòng phiếu thuộc `ord` (M2 là chủ duy nhất, Q-X2); số dư, sổ, giữ hàng thuộc `inventory`.
5.2 Không ghi bảng module khác: tồn đổi qua ba hàm mới của `inventory` (A5, Q-X1); khách qua `customer` trong `Create`;
lượt mã qua `promotion.Release` trong `Cancel`. 5.3 Đọc module khác: chỉ qua hàm công khai. 5.4 Nguyên tử: tạo phiếu đổi
+ đơn thay thế (giữ hàng) một transaction; nhận phiếu + nhập kho + đổi đơn + sự kiện một transaction; hủy phiếu đổi +
hủy đơn thay thế một transaction. 5.5 Sau commit: `order.partially_returned`/`order.returned` (cash, fulfillment, thống
kê khách nghe) qua `events.Emit` (outbox); tiền hoàn do `cash` ghi ở M3 (Q16). 5.6 Gọi ra ngoài: không có. 5.7 Tenant:
mọi câu trong `WriteTx`/`WithTenant`; bảng đã có RLS (0204); câu chạm `ord.orders`/`ord.order_items` có `seller_id`.
5.8 Không HTTP trong module; handler mỏng `handlers_order_returns.go`. 5.9 Sáu operation có `x-permission`
(`order.return`, `order.return.receive`, `order.read`), route ghi có `IdempotencyKey` (sẵn trong hợp đồng). 5.10 Không
Valkey. 5.11 Không thư viện mới. 5.12 Theo mẫu `order` (Writer, sentinel, test PostgreSQL thật, rapid).

6.1 Đồng thời: mọi đường khóa đơn `FOR UPDATE` trước (hai phiếu trả cùng đơn chạy lần lượt, phiếu sau đọc tổng đã trả
sau khi phiếu trước commit → `ErrReturnQtyExceeds`); phiếu sửa bằng `version` + điều kiện trạng thái (`WHERE status =
<cũ> AND version = @v`); số dư bằng câu có điều kiện của `ApplyDelta`. Test `returns_race_test.go` (hai phiếu cho đơn vị
cuối). 6.2 Gửi lại: `Idempotency-Key` của route; `OnShipmentReturning` gọi lần hai → `false` (kiểm dưới khóa đơn +
unique riêng phần `returns_carrier_shipment_key`, `ON CONFLICT DO NOTHING`); nhận lần hai → phiếu không còn `requested`
→ `ErrReturnNotAllowed`. 6.3 Thứ tự khóa ADR-46: đơn (nhóm 7; hai đơn — phiếu đổi có đơn thay thế — theo `id` tăng dần)
→ dòng đơn → phiếu → dòng phiếu → mã (nhóm 10, chỉ khi hủy đơn thay thế) → số dư tồn (nhóm 11, inventory tự sắp
`(variant_id, warehouse_id)` tăng dần). Đơn thay thế tạo bằng `Create` sau khi đã khóa đơn gốc: khách đã có
(`CustomerID`, không gắn SĐT) nên không khóa dòng khách (nhóm 5) — `promote` chỉ đổi khách đang `lead`, mà khách có đơn
đã giao đã là `customer`. 6.4 Lỗi tạm: `WriteTx`/`db.RetryTx` chạy lại; hàm gán lại mọi biến kết quả. 6.5 READ
COMMITTED + khóa dòng. 6.6 không áp dụng: không có việc nền mới. 6.7 không áp dụng: không gọi bên ngoài. 6.8 Chết giữa
chừng: transaction lùi. 6.9 Mỗi phiếu tối đa 100 dòng (hợp đồng). 6.10 Tiền: `int64` đồng; tiền hoàn từng dòng bằng
`pricing.RefundFor` (không vượt `net_amount`, đơn vị cuối nhận phần lẻ); không ghi sổ tiền (cash M3). 6.11 Quy luật:
rapid `returns_property_test.go` (Σ trả ≤ mua; Σ hoàn ≤ net, = net khi trả hết) và nối ba hàm mới vào
`TestInventoryInvariants` (sổ = số dư, giữ hàng = số dư, damaged ≤ on_hand). 6.12 Dữ liệu cá nhân: sự kiện, audit chỉ
mã, số; lý do trả chỉ ghi "có hay không" ở audit. 6.13 Quyền: route ở bảng sinh; nhận hàng cần `order.return.receive`
(vai trò `sale` không có → 403, có test).

## Quyết định thay chủ shop

1. **Hàm nhận thêm `sellerID`** (`CreateReturn`, `OnShipmentReturning`, `CarrierReturnFor`, `ReceiveReturn`,
   `CloseReturn`, `CancelReturn`, `GetReturn`, `ListReturns`): kế hoạch ghi chữ ký không có người bán; chọn thêm như mọi
   hàm đơn của M2 (ADR-29, Task 75, 76, 78). Phương án khác: tự đọc người bán từ phiếu — bỏ lớp chặn người bán.
2. **Phiếu hoàn của hãng, dòng `missing`: không nhả giữ hàng `in_transit`** (vẫn đang giao, chờ M3 xử lý thất lạc,
   TD04 "món thiếu được ghi nhận riêng"); chỉ dòng `sellable`/`damaged` gọi `ReleaseInTransitByItems`. Kế hoạch ghi
   "ReleaseInTransitByItems(mọi dòng)" nhưng cũng ghi "missing → không ghi kho"; nhả dòng thiếu sẽ làm món đã mất thành
   hàng có thể bán (bán vượt). Phương án khác: nhả mọi dòng rồi M3 trừ `on_hand` khi xử lý thất lạc.
3. **Tiền hoàn của phiếu `carrier_return` = 0** (hàng chưa giao, COD chưa thu; tiền trả trước do `cash` tự tính từ thanh
   toán khi nghe `order.returned`, Q16).
4. **Phiếu thứ hai trên cùng dòng sau khi có dòng `missing`**: phần tiền "lẽ ra" của dòng thiếu vẫn tính vào "đã hoàn
   trước" (`RefundFor(net, qty, 0, q, 0)`), để đơn vị cuối không nhận tiền của món thiếu. Tổng hoàn = `net_amount` khi
   trả hết và không có món thiếu.
5. **Nhận phiếu khách trả / đổi tăng `version` của đơn gốc** (không đổi trạng thái, không đổi trường nào khác) để sự kiện
   `order.partially_returned`/`order.returned` có `seq` riêng trên cùng chuỗi sự kiện của đơn (unique
   `(aggregate, seq)`). Payload `ReturnReceived` = `Changed` + `returnId`, `kind`, `refundAmount`. Phiếu hoàn của hãng:
   `order.returned` do `applyTransition` phát (payload `Changed`), M3 thêm trường khi cần (kế hoạch M3 Task 23).
   "Trả đủ" = mọi dòng đơn có Σ số lượng ở các phiếu đã nhận (`restocked`, `closed`) bằng số đã mua.
6. **Đơn thay thế**: người bán, khách, kho, kênh của đơn gốc; không mã giảm giá (gửi mã → `ErrInvalidInput`); địa chỉ
   thiếu thì lấy địa chỉ đơn gốc; ý định mặc định `confirmed` khi địa chỉ đủ tỉnh, xã, không thì `new`.
7. **Hủy phiếu đổi khi đơn thay thế đã gửi hãng (có vận đơn hiện hành) hay đã giao, trả**: `ErrReturnNotAllowed`
   (không hủy phiếu, đơn thay thế đi đường hủy/trả riêng). Đơn thay thế đã hủy trước: chỉ hủy phiếu. Lý do hủy đơn thay
   thế: `other`.
8. **`OnShipmentReturning` chỉ nhận đơn `confirmed`**; đơn khác (kể cả `cancel_requested`) → `ErrInvalidTransition`
   (máy trạng thái không có `cancel_requested → returned`; ghi ở Việc phát hiện thêm).

## Bằng chứng

Lệnh chung: (A) `go test -race -count=1 ./internal/modules/order/... ./internal/modules/inventory/...` →
`ok …/order 95.9s`, `ok …/order/internal/pricing`, `ok …/order/internal/state`, `ok …/inventory 61.8s`;
(B) `go test -race -count=1 ./internal/httpapi/ -run 'TestReturnAPI'` → `--- PASS: TestReturnAPI_Flow`,
`--- PASS: TestReturnAPI_Exchange`, `ok`; (C) `go test -race -count=1 ./internal/modules/order/ -run
TestCancelReturn_ExchangeRaceNoDeadlock` → `ok 2.97s`; (D) `go test -count=1 ./tests/arch/...` → `ok`,
`golangci-lint run ./internal/modules/order/... ./internal/modules/inventory/... ./internal/httpapi/...` → `0 issues.`

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TD05/AT13: trả A ×1 → requested, refundAmount 250.000; phiếu hai A ×1 khi phiếu một mở → ErrReturnQtyExceeds (TD07, WAT28); dòng lạ, trùng, số 0, loại lạ, đổi thiếu đơn thay thế → ErrInvalidInput; người bán khác → ErrNotFound | `TestCreateReturn_TD05PerLine` | A | ok |
| Làm tròn WAT31: C ×2 net 500.001 → 250.000 rồi 250.001 | `TestCreateReturn_RoundingLastUnit` | A | ok |
| TD08/WAT29: requested không đổi tồn, không có order.partially_returned; nhận A sellable → on_hand +1, order.partially_returned refundAmount 250.000, đơn D vẫn delivered (version +1); trả nốt B → order.returned; nhận lần hai → ErrReturnNotAllowed; version cũ → ErrVersionConflict | `TestReceiveReturn_TD08Restock` | A | ok |
| Hàng hỏng: damaged → on_hand +1, damaged +1, Available không đổi; missing → không ghi kho, tiền hoàn 0 | `TestReceiveReturn_DamagedAndMissing` | A | ok |
| TD06/AT14: phiếu exchange + đơn thay thế orderType exchange giữ A-L ×1, cùng khách, ReplacedReturnID; đơn D không đổi; mã giảm giá → ErrInvalidInput; hủy phiếu → đơn thay thế cancelled, A-L nhả | `TestCreateReturn_TD06Exchange`, `TestCancelReturn_ExchangeShipped` | A | ok |
| TD02: OnShipmentReturning(S1) → carrier_return requested đủ dòng, đơn confirmed, tồn không đổi; lần hai, vận đơn cũ → false; CarrierReturnFor; nhận → in_transit 0, đơn returned, nhãn returned, order.returned | `TestCarrierReturn_TD02Requested`, `TestCarrierReturn_TD02Receive`, `TestCarrierReturn_MissingStaysInTransit` | A | ok |
| Đóng, hủy phiếu: trạng thái, version, không có | `TestCloseCancelReturn` | A | ok |
| Danh sách, con trỏ, lọc, người bán khác | `TestListReturns` | A | ok |
| Đơn chưa giao → 422 return_not_allowed; lần hai → 422 return_qty_exceeds; bán hàng gọi receive → 403; kho tạo phiếu → 403; kho nhận → 200 restocked; audit không chứa lý do | `TestReturnAPI_Flow` | B | PASS |
| Đổi hàng qua API, hủy phiếu đổi; đổi thiếu replacement → 400 | `TestReturnAPI_Exchange` | B | PASS |
| Đồng thời: hai phiếu cho đơn vị cuối → một thành công, một ErrReturnQtyExceeds (20 lần) | `TestCreateReturn_LastUnitRace` | A | ok |
| Hai đường song song (hủy phiếu đổi, hủy đơn thay thế) không deadlock, hàng nhả một lần (10 lần) | `TestCancelReturn_ExchangeRaceNoDeadlock` | C | ok |
| Thuộc tính rapid (200 lượt, ≥ 100 của kế hoạch): Σ trả ≤ mua; Σ hoàn ≤ net, = net khi trả hết không thiếu | `TestReturns_Invariants` | A | ok |
| inventory: ReleaseInTransitByItems, ReceiveCustomerReturn, MoveToDamaged (đúng số dư, sổ, lỗi) | `TestReleaseInTransitByItems`, `TestReceiveCustomerReturn`, `TestMoveToDamaged` | A | ok |
| Ba hàm mới nối vào bộ thuộc tính M1 (sổ = số dư, giữ hàng = số dư, damaged ≤ on_hand, không âm) | `TestInventoryInvariants` (kind 6–8) | A | ok |
| Kiến trúc: lọc người bán, câu đổi trạng thái đơn chỉ ở transition.sql, lint | `tests/arch` | D | ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký có thêm `sellerID` (Quyết định 1).
- Không tạo `queries/inventory/returns.sql`: ba hàm mới dùng lại `ApplyDelta`, `EnsureBalance`, `ReservationIDsByItems`.
- `ReleaseInTransitByItems` ghi sổ `doc_type order`, `doc_id` = mã giữ hàng (như mọi bước của giữ hàng; chữ ký kế hoạch
  không có mã phiếu), bước `carrier_return`; `ReceiveCustomerReturn` ghi `doc_type return`, `MoveToDamaged` ghi
  `doc_type damage`, `doc_id` = mã phiếu trả.

- Hai sửa cấu trúc nhỏ ở mã có sẵn (`shipAddressIn`, `CancelOrder` dùng `orderWriteAsDefaultSeller`) nằm chung commit
  với hành vi mới: chúng chỉ để gộp đoạn trùng mà phần mới tạo ra (cổng jscpd), không đổi hành vi (test hủy đơn qua API
  vẫn qua); tách commit riêng sẽ là commit cấu trúc không có chỗ dùng thứ hai.

## Việc phát hiện thêm

- Đơn `cancel_requested` mà hãng chuyển hoàn: máy trạng thái chưa có cạnh `cancel_requested → returned`; M3 cần quyết
  (hãng từ chối hủy rồi hoàn → `OnShipmentCancelRejected` đưa về `confirmed` trước).

- (reviewer) `order.returned` có hai nghĩa: đơn `confirmed → returned` (phiếu hoàn của hãng, payload `Changed`) và khách
  trả đủ (đơn vẫn `delivered`, payload `ReturnReceived`); consumer M3 (`customer_stats`, `cash`) phân biệt bằng `status`
  / `returnId`.
- (reviewer) Hủy phiếu đổi hủy luôn đơn thay thế nhưng chỉ ghi audit `order.return.cancel` (vết đơn ở lịch sử trạng
  thái); cân nhắc thêm dòng audit `order.cancel` cho đơn thay thế.
- (reviewer) Nhận phiếu `carrier_return` chưa đối chiếu `shipment_id` với vận đơn hiện hành; cần nếu M3 cho đổi vận đơn
  khi đang hoàn. Đơn đã `returned` vẫn giữ `in_transit` của món thiếu cho M3 xử lý (Quyết định 2).

## Reviewer

Kết luận: ĐẠT (lượt 1, trên phần đã stage sau khi gộp main — main không có gì mới — và sau lần chạy ci-task thứ hai
xanh: build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check, test
gói đổi + phụ thuộc + vùng cao 426 giây, EXIT 0).

Phát hiện phải sửa: không có. Reviewer đã kiểm thứ tự khóa ADR-46 (hai đơn theo id tăng dần), đơn thay thế không khóa
dòng khách, hai lớp chống trùng phiếu hoàn của hãng, quyền cột, audit không có lý do trả, mã sinh chỉ đổi chú thích.
Ghi chú không bắt buộc (chép sang Việc phát hiện thêm): `order.returned` hai nghĩa (consumer phân biệt bằng `status`
hoặc `returnId`); hủy đơn thay thế khi hủy phiếu đổi không có audit `order.cancel` riêng; nhận phiếu `carrier_return`
không đối chiếu vận đơn hiện hành; `variantOf` trả `ErrStatusConflict` (chưa ánh xạ, chỉ là ca lý thuyết).
