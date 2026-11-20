# Task 115 — M3-24: Khách trả hàng — nhận theo số lượng và tiền hoàn trên phiếu của M2

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 115, M3-24). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. Đổi hành vi của `order.Writer.ReceiveReturnQty` (3 chỗ gọi: `internal/httpapi/handlers_order_returns.go:177`, `internal/modules/order/returns_qty_test.go`, `internal/modules/fulfillment/return_received_test.go:63`) và của `ReceiveReturn` M2 (cùng thân `markReceived`; tình trạng cả dòng cho kết quả như cũ vì `RefundFor(R, Q, 0, Q, 0) = R`, `RefundFor(R, Q, 0, 0, 0) = 0`). Hàm riêng đổi: `markReceived`, `receive` (bỏ chặn thiếu một phần), `priceReturnLines` (`returns.go`). Kiểu sinh `store.ReturnedLinesOfOrderRow` thêm trường `MissingQty` (2 chỗ dùng: `priceReturnLines`, `receivedTopic`).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/adapters/carrier`, `internal/httpapi`, `internal/modules/{cash,customer,fulfillment}`, `internal/testkit/shiptest`. Không gói nào đọc `ReturnedLinesOfOrderRow` (gói store là internal).
- Operation OpenAPI bị chạm, oasdiff: không. `receiveReturn` đã nhận ba số (Task 114); dòng `mixed` ra API đã bỏ trường `condition` (`returnOut`).
- Chỗ dùng trên màn hình: không đổi mã màn hình. Màn nhận phiếu trả gửi ba số thì nay phiếu khách trả có dòng thiếu một phần được nhận (trước 422 `invalid_input`).
- Câu sqlc, bảng, pha migration: `queries/ord/returns.sql` `ReturnedLinesOfOrder` thêm cột `ri.missing_qty` (bảng `ord.return_items`, cột đã có từ 0302). `SetReturnItemQty` (returns_qty.sql) giữ nguyên. Không migration (không dùng số 0317).
- Tệp test đổi: `returns_qty_customer_test.go` (mới), `returns_qty_property_test.go` (thêm `TestReceivedRefund_Property`), `returns_qty_test.go` (bỏ một câu kiểm, xem mục dưới), `returns_property_test.go` và `returns_test.go` (tách hàm test dùng chung `drawDeliveredOrder`, `propCreate`, `propPick`, `propErr`, `returnedOf`, `lineBreach`, `exchangeReturn`; câu kiểm M2 giữ nguyên).
- Test sẽ chạy: `go test -race ./internal/modules/order/...` (vùng rủi ro cao, cả bộ: gồm test M2 Task 22 `TestCreateReturn_TD05PerLine`, `TestReceiveReturn_TD08Restock`, `TestCreateReturn_LastUnitRace`, `TestReturns_Invariants`), `./internal/modules/fulfillment/...`, `./internal/modules/cash/...`, `./internal/httpapi/...` (gói nhập order); ci-task.sh một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm chia tiền ngoài `pricing` của order); grep `RefundFor`, `ReturnedLinesOfOrder`, `markReceived`.
- Dùng lại / viết mới vì: dùng lại `CreateReturn`, `CancelReturn`, `ReceiveReturn`/`ReceiveReturnQty` (M2 Task 22, M3 Task 114), `pricing.RefundFor` (chia tiền hoàn, đơn vị cuối nhận phần lẻ), `inventory.ReceiveCustomerReturn` (qua `receiveCustomer` có sẵn), sự kiện `ReturnReceived` (đã có `kind`, `refundAmount`). Không viết hàm hay kiểu mới ngoài một hàm nhỏ `receivedRefund` (tiền hoàn của dòng đã nhận) để test quy luật thuần.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `ord.returns`, `ord.return_items` của `order`; tồn của `inventory` (qua `ReceiveCustomerReturn`).
- 5.2 Không ghi bảng module khác: chỉ ghi `ord.*`; tồn qua public API của `inventory`.
- 5.3 Đọc qua public API: cash đọc tiền hoàn từ payload `refundAmount` của sự kiện, không đọc `ord.*`.
- 5.4 Nguyên tử: ghi ba số + tiền hoàn từng dòng, phiếu restocked, nhập kho, version đơn, sự kiện: một transaction của bên gọi.
- 5.5 Không nguyên tử: nghĩa vụ hoàn tiền do cash tạo khi nghe sự kiện (Task 30), sau commit.
- 5.6 Gọi ra ngoài: không áp dụng.
- 5.7 Ngữ cảnh shop: mọi câu trong tx tenant của bên gọi (WriteTx).
- 5.8 Không HTTP trong module: không đổi.
- 5.9 Khóa chống trùng, quyền: `receiveReturn` sẵn có `IdempotencyKey`, `order.return.receive`; không đổi.
- 5.10 Valkey: không áp dụng.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu: đường ghi Writer trong tx bên gọi (Task 73), giữ khung `changeReturn`/`receiveQty` của M2, M3-23.

Mục 6:
- 6.1 Đồng thời: không đổi khóa: đơn → phiếu (version) → dòng phiếu → số dư. Tạo phiếu đọc `ReturnedLinesOfOrder` khi đã khóa đơn, nên đọc thấy `missing_qty` đã commit của mọi lần nhận trước.
- 6.2 Gửi lại: Idempotency-Key ở HTTP; nhận lần hai → `version_conflict` / `return_not_allowed` như cũ.
- 6.3 Thứ tự khóa: không thêm khóa (ADR-46 nhóm 7 → 10 → 11 như M2).
- 6.4 Lỗi tạm: WriteTx chạy lại; mọi biến kết quả (`total`) gán lại trong thân.
- 6.5 Mức cách ly: READ COMMITTED + khóa dòng, như cũ.
- 6.6 Việc nền: không áp dụng (không thêm consumer).
- 6.7 429: không áp dụng.
- 6.8 Chết giữa chừng: transaction lùi, phiếu còn requested.
- 6.9 Treo: không áp dụng.
- 6.10 Tiền: `int64` đồng. Tiền hoàn dòng nhận = `RefundFor(R, Q, 0, S+D, 0)` (R: tiền hoàn đã chia lúc tạo phiếu cho Q đơn vị) ≤ R; đơn vị thiếu không hoàn. Lần tạo phiếu sau tính "đã hoàn trước" của dòng phiếu có món thiếu bằng phần lẽ ra floor(net × Q / qty) như luật `missing` của M2, để đơn vị cuối không nhận tiền món thiếu. Tổng tiền hoàn của dòng đơn ≤ net (test rapid).
- 6.11 Quy luật: rapid 200 lượt (tạo, hủy, nhận theo số lượng trên đơn 1–3 dòng, số lượng 1–5): Σ số trả ≤ số mua; Σ tiền hoàn ≤ net; tồn tăng đúng S+D. Rapid thuần cho `receivedRefund`.
- 6.12 Dữ liệu cá nhân: payload chỉ mã và số, không đổi.
- 6.13 Quyền: không đổi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TD05/AT13: trả 2 vB, nhận S1 D1 M0 → vB on_hand +2, damaged +1; hoàn dòng 354.098; `order.partially_returned` (kind customer_return, refundAmount 354.098), không `order.returned`; đơn delivered, version +1 | `TestReceiveReturnQty_CustomerTD05` | `go test -race ./internal/modules/order/ -run '…' -v` | `--- PASS: TestReceiveReturnQty_CustomerTD05 (0.37s)` |
| Trả 2 vB, nhận S1 D0 M1 → on_hand +1; hoàn 177.049; dòng mixed 1/0/1; sự kiện refundAmount 177.049, missingQty 1 | `TestReceiveReturnQty_CustomerPartialMissing` | như trên | `--- PASS: TestReceiveReturnQty_CustomerPartialMissing (0.28s)` |
| Món thiếu không hoàn ở phiếu sau (C ×3 net 300.003: 200.002 → nhận S1 M1 = 100.001; phiếu 2 = 100.001; phiếu 3 → `ErrReturnQtyExceeds`) | `TestReceiveReturnQty_MissingShareNotPaidLater` | như trên | `--- PASS: TestReceiveReturnQty_MissingShareNotPaidLater (0.40s)` |
| exchange nhận S0 D1 M1 → on_hand +1, damaged +1, hoàn 177.049, sự kiện kind exchange; đơn thay thế không đổi | `TestReceiveReturnQty_ExchangePartialMissing` | như trên | `--- PASS: TestReceiveReturnQty_ExchangePartialMissing (0.26s)` |
| Thuộc tính rapid 200 lượt (TestMain): tạo/hủy/nhận theo số lượng, đơn 1–3 dòng số 1–5 → Σ số trả ≤ mua; Σ hoàn ≤ net; on_hand += Σ(S+D), damaged += Σ D; trả hết không thiếu → hoàn = net | `TestReceiveReturnQty_CustomerInvariants` | như trên | `--- PASS: TestReceiveReturnQty_CustomerInvariants (25.18s)` |
| Tiền hoàn dòng: 0 ≤ hoàn ≤ R; M = 0 → R; S+D = 0 → 0; còn lại floor(R(S+D)/Q) | `TestReceivedRefund_Property` | như trên | `--- PASS: TestReceivedRefund_Property (0.00s)` |
| TD07/WAT28 (M2 Task 22 chạy lại): vượt số trả, hai phiếu cùng lúc → `return_qty_exceeds` | `TestCreateReturn_TD05PerLine`, `TestCreateReturn_LastUnitRace` | như trên | `--- PASS: TestCreateReturn_TD05PerLine (0.28s)`, `--- PASS: TestCreateReturn_LastUnitRace (0.99s)` |
| TD08/WAT29 (M2 chạy lại): phiếu requested không đổi tồn, không sự kiện hoàn; nhận → restocked | `TestReceiveReturn_TD08Restock`, `TestReceiveReturn_DamagedAndMissing`, `TestReturns_Invariants` | như trên | `--- PASS` cả ba (0.20s, 0.22s, 15.81s) |
| Cổng của task: build, vet, lint, gen-check, sqlc-vet, jscpd, oasdiff, test-guard; test gói đổi + phụ thuộc + vùng cao | (cả bộ) | `scripts/agent/ci-task.sh` | mọi cổng tĩnh xanh; mọi gói `ok` trừ `internal/modules/customer` (`TestStatsConcurrentWithMerge` panic ở `stats.go:108`, không chạm bởi task này; chạy lại `go test -race ./internal/modules/customer/ -run TestStatsConcurrentWithMerge -count=3` → `ok`, xem Việc phát hiện thêm) |

## Test bị xóa hoặc tắt

- `internal/modules/order/returns_qty_test.go` `TestReceiveReturnQty_Customer`: bỏ câu kiểm "phiếu khách trả có dòng thiếu một phần → `ErrInvalidInput`" (27 → 26 câu kiểm). Câu đó ghi rõ là chặn tạm cho tới M3-24 ("tiền hoàn một phần của dòng thiếu là việc của Task M3-24"); task này làm đúng việc đó, hành vi mới kiểm ở `TestReceiveReturnQty_CustomerPartialMissing`, `_ExchangePartialMissing`, `_MissingShareNotPaidLater`, `_CustomerInvariants`.

## Khác kế hoạch

- Kế hoạch ghi sửa `queries/ord/returns_qty.sql`; câu cần đổi là `ReturnedLinesOfOrder` ở `queries/ord/returns.sql` (thêm `missing_qty`), `SetReturnItemQty` giữ nguyên.
- Test rapid trên PostgreSQL đặt ở `returns_qty_customer_test.go` (gói `order_test`, cần `orderEnv`); `returns_qty_property_test.go` (gói `order`, nội bộ) thêm test rapid thuần `TestReceivedRefund_Property`.
- Để jscpd không báo trùng: tách phần dùng chung của hai test rapid (`drawDeliveredOrder`, `propCreate`, `propPick`, `propErr`, `returnedOf`, `lineBreach`) trong `returns_property_test.go`, và `exchangeReturn` trong `returns_test.go` (TD06 dùng lại). Chỉ dời mã test, câu kiểm của M2 giữ nguyên.

## Quyết định thay chủ shop

1. **Tiền hoàn của dòng nhận thiếu một phần tính từ tiền hoàn đã chia lúc tạo phiếu:** `RefundFor(R, Q, 0, S+D, 0)` (R, Q: tiền hoàn, số của dòng phiếu). Khớp hai ca của kế hoạch (354.098 khi S1 D1; 177.049 khi S1 M1) và luôn ≤ R nên tổng không vượt net. Phương án khác: chia lại từ `net` của dòng đơn cùng các phiếu khác — phụ thuộc vào phiếu tạo/hủy sau, khó đối chiếu với số khách đã thấy lúc tạo phiếu.
2. **Món thiếu không được hoàn ở phiếu sau:** lần tạo phiếu sau tính "đã hoàn trước" của dòng có món thiếu (cả dòng hay một phần) là phần lẽ ra floor(net × Q / qty), mở rộng luật `missing` của M2. Phương án khác (đếm tiền thực hoàn) để đơn vị cuối nhận luôn tiền món thiếu — trái "đơn vị thiếu không hoàn tiền".
3. **Món thiếu vẫn tính là "đã trả" của dòng đơn** (giới hạn số trả, chọn `order.returned` hay `order.partially_returned`): số trên phiếu đã dùng hết quyền trả của các đơn vị đó, như M2 làm với dòng `missing` cả dòng. Muốn khách trả lại món thiếu thì nhân viên tạo phiếu mới sau khi chỉnh — để chủ shop quyết sau nếu cần.
4. **Không migration** (số 0317 không dùng): cột `missing_qty` đã có từ 0302.

## Việc phát hiện thêm

- Cho Task 116 (cấn trừ đơn thay thế): `refund_amount` của phiếu đổi có thể giảm khi kho nhận thiếu (một phần hay cả dòng, cả ở M2); phần đã cấn trừ lúc tạo phiếu có thể lớn hơn tiền hoàn cuối cùng.
- Test chập chờn có sẵn: `internal/modules/customer` `TestStatsConcurrentWithMerge` panic `index out of range [0] with length 0` ở `stats.go:108` (`locked[0]` khi không khóa được dòng nào lúc gộp chạy song song). Không do task này (không chạm customer); chạy lại 3 lần đều qua. Cần task riêng sửa `lockGroupsOnce` (tập khóa rỗng → thử lại).
- Màn Đơn ước tính tiền hoàn của phiếu mới ở `console/src/screens/don/returnRules.ts:54` theo luật cũ (chỉ dòng `condition = missing` tính phần lẽ ra). Dòng trộn có món thiếu ra API không có `condition` và không có ba số, nên bản ước tính có thể cao hơn số máy chủ tính cho đơn vị cuối; máy chủ vẫn là số đúng. Cần thêm `sellableQty/damagedQty/missingQty` vào `ReturnItem` của hợp đồng rồi sửa màn (task riêng, chạm openapi + console).

## Reviewer

Kết luận: ĐẠT (lượt 1, agent `reviewer`, trên phần đã stage).

- Phát hiện phải sửa: không có. Đã kiểm `receivedRefund` (≤ R; thân M2 cho kết quả như cũ), `priceReturnLines` với `MissingQty > 0` (khớp dữ liệu cũ nhờ backfill 0302), thứ tự khóa không đổi, cách cũ (chặn thiếu một phần, `refund = 0`) đã xóa hẳn.
- Ghi chú không bắt buộc: Task 116 cấn trừ đơn thay thế bằng Σ `refund_amount` lúc tạo phiếu đổi; nhận thiếu một phần làm số này giảm sau đó (ghi ở Việc phát hiện thêm). Quy luật "món thiếu không hoàn ở phiếu sau" chỉ có một ca cụ thể, không có ở test rapid.
