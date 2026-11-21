# Task 136 — M3-45: Màn kho — in nhãn theo lô, phiếu nhặt, đóng gói, nhận hàng hoàn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 136, M3-45). Loại việc (quy trình mục 3): A.
Mockup đã chốt: `docs/mockups/m3/kho.html` (màn 5), hành vi HV23–HV26 (chủ shop OK 21/11).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (tìm theo tên, LSP findReferences):
  - `queries.OrderShipment` thêm trường `CarrierAccountID`, `Mode`, `LabelPrintedAt` (thêm trường, không đổi chữ ký;
    nơi dựng: `scanOrderListRow` trong `internal/queries/orders_dynsql.go`; nơi đọc: `handlers_order_read.go`,
    `order/list_m3_dynsql_test.go`).
  - `queries.OrderListRow` thêm `PackingStatus` (đã đọc cột, trước chỉ dùng tính nhãn).
  - `queries.OrderFilter` thêm `WarehouseID *uuid.UUID`, `LabelPrinted *bool` (nơi dựng: `listOrdersFilter` ở
    `handlers_order_read.go`; test `order/list_m3_dynsql_test.go`, `order/list_test.go` dựng bằng tên trường nên không gãy).
  - `order.ReturnItem` thêm `Sellable`, `Damaged`, `Missing int` (nơi dựng: `withItems` trong `order/returns.go`; nơi
    đọc: `returnOut` trong `httpapi/handlers_order_returns.go`). Không có nơi nào khác dựng `order.ReturnItem`.
- Gói phụ thuộc ngược (go list): `internal/queries` ← `internal/httpapi`; `internal/modules/order` ← `cmd/crm`,
  `internal/httpapi`, `modules/cash`, `cash/internal/refunds`, `modules/fulfillment`, `testkit/codtest`, `testkit/shiptest`.
  `internal/modules/order` là vùng rủi ro cao → chạy cả bộ của gói (ci-task tự chạy).
- Operation OpenAPI bị chạm (chỉ thêm trường/tham số tùy chọn, không phá vỡ; oasdiff trong ci-task):
  - `listOrders`: tham số mới `warehouseId` (uuid), `labelPrinted` (boolean); `OrderListItem.packingStatus`;
    `OrderShipmentSummary.id`, `carrierAccountId`, `mode`, `labelPrintedAt`.
  - `ReturnItem.sellable`, `damaged`, `missing` (chỉ có sau khi kho nhận) → mọi operation trả `Return`
    (`createReturn`, `getReturn`, `listReturns`, `listOrderReturns`, `receiveReturn`, `closeReturn`, `cancelReturn`).
- Chỗ dùng trên màn hình: mới `console/src/screens/warehouse/` (route `/warehouse/pack`, `/warehouse/picking-list`,
  `/warehouse/returns/receive`, `/warehouse/returns/receive/:returnId`); sửa `console/src/screens/don/ReceiveReturnDialog.tsx`
  (ba ô Tốt / Hỏng / Thiếu; dùng ở `ReturnsCard.tsx` và trang nhận hàng hoàn của kho; thêm `ReceiveReturnCard`), mới
  `don/receiveRules.ts`; xóa `CONDITION_TEXT`, `Condition` ở `don/labels.ts` (chỉ hộp cũ dùng). `OrderList.tsx` không đổi
  (chỉ có thêm trường tùy chọn trong kiểu sinh). Lớp HTTP: hàm mới `orderShipmentOut` (handlers_order_read.go, tách ra
  để `ListOrders` dưới ngưỡng gocognit).
- Câu sqlc, bảng, pha migration: `queries/ord/returns.sql` câu `ListReturnItemsOf` đọc thêm `sellable_qty`,
  `damaged_qty`, `missing_qty` (cột có từ migration 0302, GRANT SELECT sẵn); `orders_dynsql.go` đọc thêm
  `s.carrier_account_id`, `s.mode`, `s.label_printed_at` của view `fulfillment.shipments_v` (cột có sẵn, docs/views.md
  đã kê), điều kiện mới `o.warehouse_id = $n` và `s.label_printed_at IS [NOT] NULL`. Không migration.
- Test sẽ chạy: `go test -race ./internal/queries/... ./internal/modules/order/... ./internal/httpapi/ -run
  'ListM3|OrderRead_List|ReturnAPI'` khi viết; `cd console && npm test -- warehouse don`, `npx tsc --noEmit`,
  `npx eslint .`; trước review: `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung (`docs/danh-muc-dung-chung.md`, `console/src/shared/index.ts`) và tìm theo tên:
  `useWriteAction` (khóa chống trùng), `unwrap`/`errorMessage`, `queryKey`/`subscribeChanged` (báo hiệu
  `label_batch.ready`, `order.changed`), `shortTime`, `formatMoney` (`products/productRules`), `Dialog`
  (`staff/staffData`), `WarehouseSelect` (`inventory`), `renderScreens`/`reply`/`bodyOf` (`app/screenTestkit`),
  `useDebounced`. Lớp CSS có sẵn: `.lblbar`, `.toast` (don.css), `.tbl`, `.check`, `.box`, `.chip`, `.empty`, `.cols`.
- Dùng lại / viết mới vì:
  - Dùng lại mọi thứ ở trên. Viết mới `warehouse.css` chỉ chép các lớp M3 chưa có trong console (`.selbar`, `.c-chk`,
    `.q3`, `.rline`, `.paper`, `.fx`, `.oitem` của kho) từ `docs/mockups/m3/m3.css`, cộng `@media print` của phiếu nhặt.
  - `CARRIER_NAME` (mã hãng → tên hiển thị) viết trong `warehouse/warehouseRules.ts`: console chưa có bảng tên hãng
    (Task 132–135 làm song song có thể thêm; trùng thì gộp ở task sau, ghi Việc phát hiện thêm).
  - Máy chủ: thêm trường vào câu và kiểu sẵn có, không thêm hàm mới.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1, 5.2: không thêm bảng, không ghi bảng mới. Đường ghi dùng operation có sẵn (createLabelBatch, setPackingStatus,
  receiveReturn).
- 5.3: danh sách đơn đọc vận đơn qua view công bố `fulfillment.shipments_v` (cột đã có trong `docs/views.md`), như cũ.
- 5.4–5.6: không áp dụng: không đổi đường ghi, không gọi ra ngoài.
- 5.7: câu mới chạy trong `tenancy.WithTenant` có sẵn của handler; không bảng mới.
- 5.8: module order chỉ thêm trường int; lớp HTTP mỏng như cũ.
- 5.9: không operation ghi mới; console gửi `Idempotency-Key` qua `useWriteAction` cho mọi lần ghi.
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: màn theo mẫu màn Đơn, Kho (route.tsx tự đăng ký, testkit API giả).

Mục 6 (đúng khi chạy đồng thời):
- 6.1: đóng gói chỉ đi tới do máy chủ quyết (Task 107, `already_further`); màn chỉ hiện kết quả từng dòng, không tự
  suy. Nhận hàng hoàn gửi `version` của phiếu (409 version_conflict khi người khác đã nhận).
- 6.2: mỗi thao tác một khóa chống trùng sinh khi bấm; tự gửi lại giữ khóa (`useWriteAction`).
- 6.3–6.9: không áp dụng: chỉ đọc thêm cột, không khóa, không việc nền mới.
- 6.10: tiền chỉ hiện (`formatMoney`), không tính.
- 6.11: không áp dụng: không đổi quy luật vùng rủi ro cao (chỉ đọc thêm cột của dòng phiếu trả).
- 6.12: phiếu nhặt không có địa chỉ, SĐT (API không trả); màn không log gì. Danh sách đóng gói không hiện SĐT.
- 6.13: nút hiện theo quyền (`fulfillment.print`, `fulfillment.pack`, `order.return.receive`); API vẫn kiểm theo kho.
  `warehouseId` chỉ là bộ lọc của danh sách đọc đã kiểm `order.read`, không mở thêm dữ liệu.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chọn 5 vận đơn hai hãng → hai lô nhãn, câu "1 vận đơn tự giao không có nhãn hãng" (VC04) | `ShipmentsToPackPage.test.tsx` › "5 vận đơn hai hãng → hai lô, câu tự giao; lô sẵn sàng có tệp PDF; in không đổi cột Đóng gói" | `cd console && npx vitest run src/screens/warehouse` | `Tests 110 passed (110)` (warehouse + don) |
| In lại không đổi cột "Đóng gói" (HV24) | cùng test trên: chỉ một yêu cầu ghi `/api/v1/label-batches`, chip Đóng gói giữ nguyên | như trên | qua |
| Lô lỗi thử lại đúng vận đơn của tài khoản, khóa mới | "lô lỗi: 'Thử lại lô …' gửi lại đúng vận đơn…" | như trên | qua |
| "Đã đóng gói" rồi chọn lại "Đang đóng gói" → "đã ở bước sau" | "Đã đóng gói rồi chọn lại Đang đóng gói → 'đã ở bước sau', dòng đó không đổi"; `warehouseRules.test.ts` › packingMessage | như trên | qua |
| Nhận hàng hoàn gửi 2 nhập 1/0/0 → nút khóa, "Còn thiếu 1 món chưa phân loại" (TD04) | `ReturnReceiptPage.test.tsx` › "gửi 2, nhập 1 / 0 / 0 → nút khóa…"; `receiveRules.test.ts` | như trên | qua |
| Nhận đủ gửi ba số mỗi dòng + version; hiện ba số máy chủ trả | `ReturnReceiptPage.test.tsx` › "1 tốt 1 hỏng / 1 thiếu…"; `OrderDetail.test.tsx` › "nhận hàng trả: ba ô…" | như trên | qua |
| Hãng báo đã hoàn chưa đổi tồn (TD02): câu trên trang | `ReturnReceiptPage.test.tsx` › "phiếu hoàn chờ nhận của hãng và giao một phần…" | như trên | qua |
| Phiếu nhặt gom theo mẫu, kê theo đơn, không địa chỉ/SĐT (HV25) | `PickingListPrint.test.tsx` | như trên | qua |
| Ở 390 (và 360, 1512) ô nhập không tràn, không cuộn ngang | `e2e/warehouse.spec.ts` (3 khổ × 3 màn; `inputsInside`, `noHorizontalScroll`) | `CRM_E2E_PORT=5236 npx playwright test e2e/warehouse.spec.ts --project=chromium` | `9 passed (5.5s)` |
| API: dòng danh sách có packingStatus, vận đơn id/carrierAccountId/mode/labelPrintedAt; lọc labelPrinted, warehouseId | `order/list_m3_dynsql_test.go` › `TestListM3_PackFields`; `httpapi/handlers_order_list_m3_test.go` › `TestOrderRead_ListTimelineM3` | `go test -race -count=1 ./internal/modules/order/ -run TestListM3`; `go test -race ./internal/httpapi/ -run 'TestOrderRead_ListTimelineM3\|TestReturnAPI'` | `ok` cả hai |
| API: ReturnItem có sellable/damaged/missing sau khi nhận, không có trước | `httpapi/handlers_order_returns_test.go` › `TestReturnAPI_ReceiveQty` | như trên | `ok` |
| Cổng tĩnh + test gói đổi, phụ thuộc, vùng cao (order) | — | `ci-task.sh` (scripts/agent) | `CI-TASK XANH (so với điểm tách nhánh 501f55c…)` |
| So ảnh mockup | `test-results/kho-{dong-goi,in-nhan,phieu-nhat,nhan-hoan}-{1512,390,360}.png` so `docs/mockups/m3/png/kho-{1512,390,360}.png` | chạy e2e trên | khớp bố cục, chữ, chip, thanh đen, ba ô; khác: xem "Khác kế hoạch" |

## Test bị xóa hoặc tắt

- Không tắt, không nới test nào. Hai test viết lại theo hành vi mới (không bỏ kiểm tra nào):
  `OrderDetail.test.tsx` "nhận hàng trả…" (ô chọn → ba ô, thêm kiểm nút khóa); `labels.test.ts` dòng
  `CONDITION_TEXT` đổi sang `QTY_TEXT` vì `CONDITION_TEXT` (chữ của ô chọn cũ) đã xóa cùng hộp cũ.

## Khác kế hoạch

- Hộp thoại nhận hàng (ReceiveReturnDialog) đổi hành vi: bỏ ô chọn một tình trạng cả dòng, thay bằng ba ô số (HV26).
  Test cũ `OrderDetail.test.tsx` "nhận hàng trả: tình trạng từng dòng" viết lại theo ba ô (không nới: vẫn kiểm đường,
  version, thân; thêm kiểm nút khóa và câu thiếu). Đổi cấu trúc và hành vi chung một commit vì tệp được viết lại toàn bộ
  theo hành vi mới (không có bước "dời giữ nguyên hành vi" nào).
- Máy chủ: thêm trường/tham số tùy chọn (Ảnh hưởng) — kế hoạch ghi "task máy chủ riêng"; điều phối viên cho phép làm
  trong task này nếu nhỏ.
- `docs/mockups/m3/README.md`: cột "Chủ shop OK" ghi "OK chủ shop 21/11" cho HV23–HV26 (màn này; chủ shop OK 21/11
  theo lời giao task) và HV30–HV32 (cùng nguồn với Task 137, kế hoạch M3 dòng 84). Không đổi dòng nào khác.
- Trang Nhận hàng hoàn đọc thêm phiếu `kind=partial_delivery` (kế hoạch chỉ ghi `carrier_return`): mockup trạng thái D
  có dòng "Giao một phần · 1 món về kho" trong "Phiếu hoàn chờ nhận" (hàng khách không nhận cũng về kho qua kho nhận,
  Task 442).
- Thêm route `/warehouse/returns/receive` (danh sách, mục menu) bên cạnh `/:returnId` của kế hoạch; trang phiếu nhặt
  `/warehouse/picking-list` (ẩn) để in ở tab riêng.

## Quyết định thay chủ shop

- **Số đếm trên thanh lọc (200, 38, 61…) không hiện.** API không có phép đếm theo bộ lọc; thêm là việc máy chủ lớn hơn
  "trường tùy chọn". Thanh lọc vẫn đủ năm nút, bấm là lọc thật (máy chủ). Khác: thêm API đếm (task sau).
- **Tải thêm bằng nút "Tải thêm"** thay câu "cuộn để tải tiếp" và tổng "200 vận đơn": không có tổng từ API; nút rõ
  ràng hơn và giữ luật "còn nextCursor thì chưa coi là hết". Đảo ngược dễ (đổi sang cuộn vô hạn).
- **Cột Nhãn "Đã in 15:12 21/11"** (có ngày, theo `shortTime` chung của console) thay "Đã in 15:12": nhãn in hôm trước
  không bị hiểu nhầm là hôm nay.
- **Phiếu nhặt không ghi "bởi <tên nhân viên>"**: `getMe` không trả tên. Khác: thêm tên vào `Me` (task khác).
- **Danh sách phiếu hoàn không ghi ngày "hãng báo đã hoàn về 20/11"**, chỉ "hãng báo đã hoàn về" (theo trạng thái
  vận đơn `returned`): `Shipment` chưa có giờ hãng báo hoàn trong hợp đồng.
- **Tài khoản nhập tay = "Không có nhãn hãng"** (chủ shop tạm dừng tích hợp hãng): cột Nhãn dựa trên `mode` của vận
  đơn; In nhãn vẫn gửi máy chủ, máy chủ bỏ qua và màn kể "n vận đơn nhập tay không có nhãn hãng".
- **Đường dẫn tiếng Anh `/warehouse/...`** theo Interfaces của kế hoạch (màn M1 dùng `/kho/...`); menu vẫn trong nhóm Kho.
- **HV01–HV16 để "chưa"** dù lời giao task bảo ghi "nếu chưa ghi": lời giao chỉ dẫn OK của chủ shop cho HV23–HV26;
  reviewer chỉ ra không có nguồn chủ shop cho HV01–HV16 (kế hoạch dòng 1773 còn "Chờ điều phối viên duyệt"). Để
  "chưa" là an toàn và dễ đổi: điều phối viên ghi khi gộp nếu chủ shop đã OK (hoặc ghi "điều phối viên duyệt (Q-X8)").
- **Ô nhập mặc định 0/0/0** (bắt kho đếm thật, TD02), không điền sẵn "Tốt = số đã gửi".

## Việc phát hiện thêm

- Bảng tên hãng `carrierName` (warehouse/warehouseRules.ts) có thể trùng với bảng tên hãng mà Task 132–135 thêm song
  song; gộp vào một chỗ (`console/src/shared`) ở task sau khi cả hai đã vào main.
- API đếm theo bộ lọc của màn Đóng gói; tên nhân viên trong `Me`; giờ hãng báo hoàn trong `Shipment` (xem Quyết định).

## Reviewer

Kết luận: ĐẠT (lần 3, 21/11).

- Lần 1 CẦN SỬA: README mockup ghi "OK chủ shop 21/11" cho HV01–HV16, HV30–HV32 mà tệp task không nêu nguồn. Phần mã
  không có phát hiện.
- Lần 2 CẦN SỬA: lời giao task chỉ dẫn OK của chủ shop cho HV23–HV26; HV01–HV16 không có nguồn là chủ shop. Sửa: trả
  HV01–HV16 về "chưa" (ghi ở Quyết định thay chủ shop); HV30–HV32 giữ (nguồn chung Task 137, kế hoạch M3 dòng 84).
- Lần 3 ĐẠT: không còn phát hiện phải sửa; bảng bằng chứng, Ảnh hưởng khớp mã; cách cũ (`CONDITION_TEXT`, ô chọn một
  tình trạng) đã xóa hẳn.
- Ghi chú không bắt buộc: bộ lọc "Chưa in nhãn" vẫn hiện vận đơn tự giao, nhập tay (không bao giờ có giờ in nhãn); để
  task sau nếu chủ shop muốn loại ra. Khi gộp với Task 137, dòng HV30–HV32 hai bên cùng chữ.
