# Task 121 — M3-30: Phiếu thu, cọc, nghĩa vụ hoàn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 121, M3-30; ca TI09, AT17; Q-2, Q-17). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới ở `cash`: `RecordPayment`, `PaymentInput`, `PaymentView`, `PayRefund`, `WaiveRefund`, `RefundObligation`,
    `ListOrderPayments`, `ListRefundObligations`, các phương thức `Service` tương ứng, sentinel `ErrCODLockedByShipment`,
    `ErrPaymentNotAllowed`, `ErrPaymentExceedsTotal`, `ErrRefundExceedsRemaining`, `ErrObligationClosed`,
    `ErrObligationNotFound`, `ErrOrderNotFound`, `ErrReasonRequired`, hằng `TopicPaymentReceived`, `RefundsConsumerName`.
    Chưa có chỗ dùng (0 tham chiếu) ngoài `internal/httpapi/handlers_payments.go`, `errors.go`.
  - Mới ở `order`: `Writer.MarkRefunded` (0 tham chiếu trước task; cash là bên gọi duy nhất). Không đổi chữ ký nào
    đã có: `ApplyPaymentTotals` (0 bên gọi ngoài test của order), `LockSnapshot` (fulfillment dùng, không đổi).
  - `cash.Module.paymentsConsumers` (riêng tư, khung Task 95) trả consumer `cash_refunds`.
  - `order`: thêm hàm riêng `Writer.lockForPayment` (kiểm nối + khóa đơn), `ApplyPaymentTotals` và `MarkRefunded`
    dùng chung (chỉ dời mã, hành vi giữ nguyên; trả lời jscpd).
  - `cmd/crm`: hai test liệt kê consumer (`TestWireM3_KeepsM1Parts`, `TestNewModulesRegistersCash`) thêm
    `cash_refunds` / `event.cash_refunds` vì consumer mới được nối qua khung `paymentsConsumers` (không sửa `wire_m3.go`).
- Gói phụ thuộc ngược (go list): `cash` ← `cmd/crm`, `internal/httpapi`, `tests/e2e`; `order` ← 5 gói (`cash` mới
  thêm, `fulfillment`, `httpapi`, `cmd/crm`, testkit). Test arch đã cho phép `cash` nhập `order` (boundaries_m3).
- Operation OpenAPI bị chạm, oasdiff: không đổi operation (`recordPayment`, `listOrderPayments`,
  `listRefundObligations`, `payRefundObligation`, `waiveRefundObligation` đã có từ Task 95, đang 501); chỉ thêm hai mã
  vào enum `Error.error` (`payment_exceeds_total`, `refund_exceeds_remaining`) — thêm giá trị enum ở câu trả lời, không
  phá vỡ. Xóa năm dòng khỏi `tests/arch/testdata/m3_pending.txt`.
- Chỗ dùng trên màn hình: chưa có màn hình gọi năm operation (màn Tiền là task M3 sau); `console/src/api` không đổi
  (enum lỗi không vào mã sinh); `console/src/app/errors.ts` thêm hai thông báo cho hai mã mới.
- Câu sqlc, bảng, pha migration: bảng `cash.payments`, `cash.refund_obligations` đã có từ 0303 (Task 93), không cần
  migration mới (0319 không dùng). Câu mới: `queries/cash/payments.sql` (chèn phiếu, tổng phiếu của đơn, danh sách,
  đơn qua `ord.orders_v`), `queries/cash/refunds.sql` (chèn nghĩa vụ ON CONFLICT DO NOTHING, khóa nghĩa vụ, chi có
  điều kiện, miễn, danh sách). `queries/ord/shiphooks.sql` thêm `SetPaymentRefunded`.
- Test sẽ chạy: `go test -race ./internal/modules/cash/... ./internal/modules/order/... ./internal/httpapi/...
  ./tests/arch/...` (cash, order là vùng rủi ro cao: cả bộ của hai gói); `scripts/agent/ci-task.sh` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có helper con
  trỏ trang chung — gói httpapi dùng mã uuidv7 cuối trang như `ListCodStatements`); `order.LockSnapshot` (khóa đơn
  nhóm 7, trả seller, trạng thái, vận đơn hiện hành, grand_total), `order.Writer.ApplyPaymentTotals` (M2 Task 18),
  `events.Registry.Emit`, `Service.emit` của cash, `identity` audit qua `h.audit`, `refusableWrite`, `catalogRead`,
  `staffNames` của httpapi, `testkit/shiptest` (cửa hàng, đơn, vận đơn thật).
- Dùng lại / viết mới vì: dùng lại mọi thứ trên. Viết mới `order.Writer.MarkRefunded` vì order chưa có đường đặt
  `refunded` (ghi chú ở `order/payments.go`: "refunded do cash đặt ở M3 qua hàm riêng"); `ApplyPaymentTotals` chỉ
  sinh unpaid/partial/paid. Consumer `cash_refunds` viết mới (khác `Consumer` sổ COD: khác topic, khác bảng).
- jscpd (4 đoạn trùng mới ở lần ci-task đầu), trả lời từng đoạn: tham số uuid rỗng được → dùng `db.NullUUID` (bỏ
  `optUUID` tự viết); `ApplyPaymentTotals`/`MarkRefunded` cùng mục đích (khóa đơn để ghi tiền) → gộp vào
  `lockForPayment`; vòng dựng trang của `ListOrderPayments` giống `ListCodStatements` chỉ bề ngoài (khác vùng, khác lý do
  đổi) → giữ riêng, viết lại thành `paymentsOut` đọc tên người ghi một lần cho cả trang; helper test `orderD2` giống
  helper của fulfillment bề ngoài (khác dữ liệu ca) → tách `payEnv.variant`, giữ riêng.

## Soát thiết kế

5.1 Chủ dữ liệu: phiếu thu, nghĩa vụ hoàn thuộc `cash` (04 mục 3 "thanh toán"); bảng ở schema `cash` (0303).
5.2 Không ghi bảng module khác: tổng đã thu và `refunded` đẩy về đơn bằng `order.Writer.ApplyPaymentTotals`,
`MarkRefunded` (public API của order).
5.3 Đọc qua view: seller, trạng thái, loại đơn đọc `ord.orders_v`; khóa đơn bằng `order.LockSnapshot`.
5.4 Nguyên tử: ghi phiếu + đổi tổng đơn trong một transaction (WriteTx); chi hoàn + phiếu âm + tổng đơn cùng tx.
5.5 Sau commit: `payment.received` là sự kiện nội bộ; nghĩa vụ hoàn sinh bởi consumer sau khi đơn hủy/nhận phiếu trả.
5.6 Gọi ra ngoài: không áp dụng (không gọi hãng).
5.7 Tenant: mọi câu chạy trong WriteTx/catalogRead/tx của worker (tenancy); bảng đã có RLS, khóa ngoại ghép tenant.
5.8 HTTP mỏng: handler đọc yêu cầu, gọi `cash.Service`, đổi lỗi ở `errors.go`.
5.9 Ghi có khóa chống trùng và quyền: ba operation ghi đã có `IdempotencyKey`, `x-permission` (Task 95).
5.10 Valkey: không áp dụng.
5.11 Thư viện mới: không.
5.12 Module mẫu: đường ghi theo Task 12 (WriteTx, audit, sentinel); consumer theo `statementsConsumer`.
6.1 Đồng thời: ghi phiếu khóa đơn trước (nhóm 7) nên hai phiếu của cùng đơn tuần tự và tổng không lệch; chi hoàn là
`UPDATE … SET paid_amount = paid_amount + a WHERE … AND paid_amount + a <= amount AND status = 'open'` dưới khóa đơn,
test hai kế toán song song mỗi người 200.000 → đúng một thành công.
6.2 Gửi lại: Idempotency-Key ở HTTP; nghĩa vụ có khóa unique `(tenant, order, source, return_id)` NULLS NOT DISTINCT,
chèn ON CONFLICT DO NOTHING (sự kiện giao lại hai lần → một nghĩa vụ, có test).
6.3 Thứ tự khóa: đơn (nhóm 7) → nghĩa vụ (nhóm 9, FOR UPDATE) → chèn phiếu (dòng mới; khóa KEY SHARE lên nghĩa vụ đã
giữ). Mọi đường chạm phiếu/nghĩa vụ của một đơn đều khóa đơn trước nên không có vòng chờ. Consumer chỉ chèn nghĩa vụ
mới (không khóa đơn): đơn đã cancelled thì không đường nào còn ghi phiếu thu dương (RecordPayment từ chối dưới khóa).
6.4 Lỗi tạm: WriteTx/RetryTx có sẵn; thân hàm gán lại mọi biến kết quả.
6.5 Mức cách ly: READ COMMITTED + câu có điều kiện + khóa dòng.
6.6 Việc nền lỗi: consumer trả lỗi để hàng đợi thử lại; payload hỏng → `queue.ErrPermanent`.
6.7 429: không áp dụng.
6.8 Chết giữa chừng: mọi việc trong một tx; consumer giao lại an toàn (ON CONFLICT).
6.9 Treo lâu: câu ngắn, một đơn.
6.10 Tiền: `int64` đồng; phiếu chỉ ghi thêm (trigger 0303), phiếu chi âm `method = refund` (CHECK dấu); tổng phiếu của
đơn sau khi chi đủ nghĩa vụ hủy = 0.
6.11 Quy luật: test bảng và song song; quy luật "paid_amount ≤ amount" có CHECK ở database và điều kiện ở câu UPDATE.
6.12 Dữ liệu cá nhân: sự kiện, audit chỉ mang mã, số tiền, phương thức; `reference` (mã giao dịch ngân hàng) và lý do
miễn chỉ ở bảng/audit theo kế hoạch ("audit có lý do"), không vào log.
6.13 Quyền: `cash.payments.record` (ghi/đọc phiếu), `cash.cod.read` (danh sách nghĩa vụ), `cash.refunds.record` (chi),
`cash.refunds.waive` (miễn) kiểm ở guard; test Kế toán (không có waive) → 403.

## Quyết định thay chủ shop

1. **`refunded` của đơn**: chỉ đặt khi một nghĩa vụ `order_cancel` được chi đủ và tổng phiếu của đơn về 0 (đúng ca
   TI09: "chi 50.000 → paid, payment_status = refunded, tổng payments = 0"). Nghĩa vụ `customer_return` (đơn đã giao,
   thường đã thu COD qua hãng) chi đủ thì **không** đổi `prepaid_total`, `cod_amount`, `payment_status` của đơn: COD
   của vận đơn đã khóa (Q-17) và khách chỉ trả lại một phần. Phương án khác: trừ phiếu chi vào prepaid (làm COD đổi
   sau khi gửi hãng → `ErrLockedByShipment`, sai) hoặc đặt `refunded` cho cả đơn trả một phần (gây hiểu nhầm).
   Đảo được: thêm luật trong `PayRefund`, không đổi bảng.
2. **`PayRefund` gọi `ApplyPaymentTotals` chỉ với nghĩa vụ `order_cancel`** (đơn đã hủy, không còn vận đơn hiện
   hành), với `max(Σ phiếu, 0)`. Kế hoạch ghi chung "order.ApplyPaymentTotals"; làm chung sẽ hỏng với đơn đã giao (lý
   do ở 1).
3. **Thêm `order.Writer.MarkRefunded`** (order là vùng rủi ro cao, ngoài tệp được giao): cần vì order chưa có hàm đặt
   `refunded`; hàm chỉ thêm, có test riêng trong order.
4. **Ghi phiếu thu bị từ chối khi**: đơn có vận đơn hiện hành → 409 `cod_locked_by_shipment`; đơn không ở `draft`,
   `new`, `confirmed` (đã hủy, đang chờ hủy, đã giao, đã hoàn) → 409 `invalid_transition`; tổng phiếu vượt
   `grand_total` → 409 `payment_exceeds_total` (mã mới; tiền thừa không có chỗ ghi sổ). Chi vượt số còn phải hoàn →
   409 `refund_exceeds_remaining` (mã mới); nghĩa vụ đã `paid`/`waived` → 409 `invalid_transition`.
5. **Lý do miễn rỗng/chỉ khoảng trắng → 422 `invalid_input`** (`ErrReasonRequired`); chuỗi rỗng bị ogen chặn trước
   (`minLength: 1`) và trả 400 như mọi lỗi dạng yêu cầu.
6. **Tên consumer `cash_refunds`** (kế hoạch viết `cash.refunds`): theo quy ước tên consumer đã có (`cash`,
   `cash_statements`; tên là một phần của kind việc `event.<tên>`).
7. **Phiếu trả `carrier_return`, `partial_delivery`**: consumer bỏ qua (kế hoạch chỉ nêu `customer_return` và
   `exchange`); ghi ở Việc phát hiện thêm.
8. **Sự kiện `payment.received`**: aggregate `payment` (mã phiếu, Seq 1), payload chỉ mã phiếu, mã đơn, phương thức,
   số tiền. Hiện chưa consumer nào nghe.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Cọc 200.000 → cod 430.000, partial; gửi hãng → vận đơn COD 430.000; payment.received | cash `TestRecordPayment_DepositBeforeShipment` | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...` | `ok elimaz.com.vn/crm/internal/modules/cash 46.895s` |
| Sau gửi hãng ghi chuyển khoản → 409 cod_locked_by_shipment | cash `TestRecordPayment_DepositBeforeShipment`; httpapi `TestPaymentsAPI_Record` | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...`; `go test -race -count=1 ./internal/httpapi/... ./tests/arch/...` | cash ok; `ok elimaz.com.vn/crm/internal/httpapi 57.025s` |
| Đầu vào sai, vượt tổng đơn (409 payment_exceeds_total), đơn hủy (invalid_transition), đơn lạ 404, Kho 403 | cash `TestRecordPayment_Refusals`; httpapi `TestPaymentsAPI_Record` | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...`; `go test -race -count=1 ./internal/httpapi/... ./tests/arch/...` | ok |
| Danh sách phiếu của đơn, trang | cash `TestListOrderPayments`; httpapi `TestPaymentsAPI_Record` | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...` | ok |
| TI09/AT17: hủy D2 có cọc → hàng nhả, một nghĩa vụ order_cancel 200.000 open, danh sách mở có D2, chưa refunded | cash `TestRefund_CancelledDepositTI09` (`checkCancelOpened`); httpapi `TestPaymentsAPI_RefundObligations` | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...` | ok |
| Chi 150.000 → open; 60.000 → vượt; 50.000 → paid, đơn refunded, tổng phiếu 0 | cash `TestRefund_CancelledDepositTI09` (`checkPaySequence`); httpapi (409 refund_exceeds_remaining) | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...` | ok |
| Hai kế toán chi song song 200.000 → đúng một thành công | cash `TestPayRefund_Concurrent` | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...` | ok |
| WaiveRefund thiếu lý do → 422; có → waived, audit có lý do; Kế toán (không waive) → 403 | cash `TestWaiveRefund`; httpapi `TestPaymentsAPI_RefundObligations` (`checkWaiveAPI`) | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...`; `go test -race -count=1 ./internal/httpapi/... ./tests/arch/...` | ok |
| order.cancelled giao lại hai lần → một nghĩa vụ | cash `TestRefund_CancelledDepositTI09` (`checkCancelOpened`) | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...` | ok |
| Phiếu trả khách nhận xong → nghĩa vụ customer_return 177.049; exchange/carrier_return không mở | cash `TestRefundsConsumer_CustomerReturn`, `TestRefundsConsumer_NoObligation` | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...` | ok |
| order.Writer.MarkRefunded: chỉ khi prepaid 0, gọi lại an toàn | order `TestMarkRefunded` | `go test -race -count=1 ./internal/modules/cash/... ./internal/modules/order/...` | order: lần chạy cả gói đỏ 1 ca `TestReturns_Invariants` (trùng `shipments_code_key` của vận đơn giả ngẫu nhiên, không chạm mã task); `go test -race -count=3 -run TestReturns_Invariants ./internal/modules/order/` → `ok 121.432s`; TestMarkRefunded qua |
| Cổng tĩnh (build, vet, lint, gen-check, sqlc-vet, jscpd, gitleaks, oasdiff, test-guard, console) + test gói đổi, phụ thuộc, vùng cao | toàn bộ gói bị ảnh hưởng (cmd/crm, httpapi, cash, order, fulfillment, customer, tests/e2e, tests/arch) | `ci-task.sh` lần 2, sau khi sửa jscpd và test nối cmd/crm | `CI-TASK XANH (so với điểm tách nhánh 1a068c8…)`; `ok …/cmd/crm 1.655s`, `ok …/internal/httpapi 318.970s`, `ok …/modules/cash 289.436s`, `ok …/modules/order 508.497s` |
| Năm operation hết 501 (m3_pending), enum lỗi khớp hợp đồng | tests/arch | `go test -race -count=1 ./internal/httpapi/... ./tests/arch/...` | `ok elimaz.com.vn/crm/tests/arch 21.342s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Quyết định 1–3, 6 ở trên.

## Việc phát hiện thêm

- Đơn có cọc bị khách từ chối nhận (phiếu `carrier_return`) và giao một phần (`partial_delivery`) chưa sinh nghĩa vụ
  hoàn cọc; cần quyết định chính sách (Q07) và một task riêng.
- `order` `TestReturns_Invariants` chập chờn: `testkit.StubShipment` sinh mã vận đơn ngẫu nhiên trùng `shipments_code_key`
  (gặp 1/4 lần chạy ngày 21/11); cần task sửa testkit.

## Reviewer

Kết luận: ĐẠT (lần 1, trên bản đã gộp main 1a068c8, agent `reviewer` a33ae765ec3256570).

- Phát hiện phải sửa: không có.
- Đã soát riêng: không có bút toán đảo ngoài `cash.Reverse` (chi hoàn là phiếu `refund` số âm ở `cash.payments`, không
  chạm sổ COD); thứ tự khóa đơn (nhóm 7) → nghĩa vụ (nhóm 9) → chèn phiếu; `refundAmount` của consumer là
  `receivedRefund` của Task 115 (chỉ món tốt + hỏng, món thiếu không hoàn); đơn hủy sau gửi hãng vẫn chi được vì
  `ClearShipmentOfCancelled` bỏ vận đơn hiện hành.
- Ghi chú không bắt buộc (đã sửa chữ ở mục Ảnh hưởng, không đổi mã): thêm `console/src/app/errors.ts`; `console/src/api`
  thực tế không đổi; số gói phụ thuộc của order là 5. Consumer nghe `order.created` của mọi đơn (một việc nền mỗi đơn)
  — để Task M3-25 xem lại khi viết `handleExchange`.
