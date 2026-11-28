# Task 524 — Chặn gửi đơn đổi chưa cấn trừ ở máy chủ, lọc nghĩa vụ hoàn theo đơn, người làm bút toán đảo

Kế hoạch: task sửa sau Task 116, 118, 138 (M3, nền `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md`), giao
qua ghi chú điều phối M5 (`2026-11-18-crm-m5-website-flash-sale.md`); không có mục riêng trong tệp kế hoạch. Loại việc
(quy trình mục 3): A.

Phạm vi (từ "Việc phát hiện thêm" của Task 116, 118, 138):
1. Chặn ở máy chủ (`createShipment`) gửi hãng đơn đổi (đơn thay thế) khi CRM chưa ghi phiếu cấn trừ tiền món trả; có
   lối xử lý rõ khi CRM không ghi được phiếu (không kẹt mãi).
2. `listRefundObligations` lọc theo đơn (`orderId`); thẻ Nghĩa vụ hoàn trên chi tiết đơn (mockup B của Task 138).
3. Bút toán đảo COD khi nhân viên đảo "Thành công": người làm là nhân viên đó (sự kiện mang người dùng).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - mới `cash.ExchangeCreditPending(ctx, tx, orderID) (bool, error)` và `cash.ErrExchangeCreditPending` (gọi ở
    `httpapi/handlers_shipment.go`); bên trong `refunds.CreditPending` dùng chung luật với `applyCredit` (tách
    `creditDue`).
  - `cash.Service.ListRefundObligations` đổi chữ ký: tham số `status string` thành bộ lọc mới `cash.RefundFilter{Status,
    OrderID}` — 3 chỗ gọi (handler `handlers_payments.go`, 2 chỗ ở `cash/refunds_test.go`), sửa hết trong task.
  - Test chạm thêm: `cash/m3_plans_test.go` (câu sqlc thêm tham số `order_id`), `cash/exchange_credit_test.go`
    (helper dùng chung với test mới), `console/src/screens/don/donTestkit.tsx` (giả `listRefundObligations`).
  - `fulfillment.StatusChanged` thêm trường `ActorID` (`actorId`, omitzero): 6 chỗ dựng giá trị không đổi (trường mới
    bỏ trống); chỉ `applyRun.emitChanged` điền. Người đọc: `cash.Consumer` (consumer.go), test cash/codtest.
- Gói phụ thuộc ngược (go list): `cash` ← `httpapi`, `cmd/crm`, `testkit/codtest`; `fulfillment` ← `cash`, `httpapi`,
  `cmd/crm`, testkit (`shiptest`, `codtest`). Cả hai là vùng rủi ro cao
  (`cash`, `fulfillment/status.go` không chạm) → ci-task chạy cả bộ `cash`.
- Operation OpenAPI bị chạm, oasdiff: `createShipment` (thân thêm `skipExchangeCredit` tùy chọn; mã lỗi mới
  `exchange_credit_pending` 409), `listRefundObligations` (tham số query tùy chọn `orderId`). Chỉ thêm, không phá vỡ.
- Chỗ dùng trên màn hình: `console/src/screens/shipping/ShippingPanel.tsx` (khối Gửi hãng: chờ cấn trừ + lối "Gửi không
  cấn trừ"), thẻ mới `console/src/screens/payments/OrderRefundsCard.tsx` trên `don/OrderDetail.tsx`,
  `console/src/app/errors.ts` (câu của `exchange_credit_pending`), mã sinh `console/src/api/**`.
- Câu sqlc, bảng, pha migration: sửa `queries/cash/refunds.sql` `ListRefundObligations` (thêm `sqlc.narg(order_id)`;
  dùng chỉ mục unique `refund_obligations_source_key (tenant_id, order_id, source, return_id)` có sẵn). Không bảng
  mới, không migration.
- Tài liệu: `docs/views.md` (payload `shipment.status_changed` thêm `actorId`).
- Test sẽ chạy: `go test -race -run` các test mới/sửa ở `internal/modules/cash` (exchange_credit_test, refunds_test,
  postings_test), `internal/modules/fulfillment` (correction_test), `internal/httpapi` (create_shipment_exchange_test,
  handlers_payments_test); console vitest các tệp ShippingPanel, OrderRefundsCard, errors; cả gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm "đơn đổi
  đã cấn trừ chưa"); trong `cash/internal/refunds`: `applyCredit`, `creditAmount`, câu `ExchangeReturnOfReplacement`,
  `ExchangeCredit`, `ExchangeObligationExists`, `PaymentsTotal`; `order.LockSnapshot`; `refusableWrite`; màn:
  `useReplacedReturn`, `PayRefundDialog`, `WaiveRefundDialog`, `obligationState`, `SOURCE_TEXT`, `Act`.
- Dùng lại / viết mới vì: luật "còn phải cấn trừ" tách từ `applyCredit` thành `creditDue` dùng cho cả consumer và
  kiểm lúc gửi hãng (một nguồn, không chép luật sang fulfillment hay httpapi). Câu SQL cũ dùng lại hết, chỉ thêm bộ lọc
  đơn. Thẻ trên đơn dùng lại hai hộp thoại chi/miễn và luật trạng thái của trang Nghĩa vụ hoàn.

## Soát thiết kế

- 5.1 chủ dữ liệu: phiếu cấn trừ, nghĩa vụ hoàn, sổ COD thuộc `cash`; luật "đã cấn trừ chưa" đặt trong `cash`.
- 5.2 không ghi bảng module khác: kiểm lúc gửi hãng chỉ đọc; không ghi gì của cash từ đường gửi hãng.
- 5.3 đọc qua view: `cash` đọc `ord.orders_v`, `ord.returns_v` như cũ; khóa đơn qua `order.LockSnapshot` (public API).
- 5.4 nguyên tử: kiểm chạy trong cùng transaction của `createShipment`, SAU `Ship` (đơn đã khóa FOR UPDATE, quyền theo
  kho đã kiểm → không lộ đơn ngoài phạm vi); còn phải cấn trừ → lỗi từ chối, transaction lùi (vận đơn, lệnh gửi hãng
  không còn).
- 5.5 việc sau commit: không đổi (consumer `cash_refunds`, `cash` giữ nguyên đường sự kiện).
- 5.6 gọi ra ngoài: lệnh gửi hãng chỉ được ghi khi qua kiểm; từ chối thì lệnh lùi cùng transaction.
- 5.7 tenant: mọi câu trong WriteTx/WithTenant sẵn có.
- 5.8 HTTP mỏng: handler chỉ nối `Ship` rồi `cash.ExchangeCreditPending`; đổi lỗi sang HTTP ở `errors_cash.go`.
- 5.9 đường ghi: `createShipment` đã có Idempotency-Key, `x-permission`; lỗi mới là từ chối (không lưu theo khóa: gửi
  lại sau khi có phiếu thì qua).
- 5.10 Valkey: không áp dụng.
- 5.11 thư viện: không thêm.
- 5.12 module mẫu: sentinel trong `cash`, mã lỗi theo luật Task 441.
- 6.1 đồng thời: consumer cấn trừ và gửi hãng cùng khóa đơn trước (ADR-46 nhóm 7) → nối tiếp; kiểm đọc dưới khóa đơn.
  Test song song: N lần gửi hãng + N lần giao order.created → vận đơn đã commit thì luôn có đúng một phiếu cấn trừ,
  consumer không lỗi.
- 6.2 gửi lại: cùng khóa → một vận đơn; lần bị từ chối không lưu, gửi lại sau khi có phiếu thì 202.
- 6.3 thứ tự khóa: đơn → (cash đọc, không khóa thêm) → như `Ship` cũ. Không thêm khóa mới.
- 6.4 lỗi tạm: WriteTx sẵn có chạy lại; hàm kiểm không giữ biến ngoài.
- 6.5 cách ly: READ COMMITTED + khóa dòng đơn.
- 6.6 việc nền lỗi: consumer `cash_refunds` dead → đơn đổi chưa có phiếu: lối xử lý là `skipExchangeCredit` (khách
  trả đủ COD, tiền món trả thành nghĩa vụ exchange khi kho nhận — đường tiền đã có từ Task 116) hoặc chạy lại việc.
- 6.7, 6.8, 6.9: không áp dụng (không gọi ngoài mới, không treo mới).
- 6.10 tiền: không ghi tiền mới; bút toán đảo chỉ đổi người làm. Sổ chỉ ghi thêm.
- 6.11 quy luật: không đổi quy luật sổ; thêm test song song cho bất biến "đã gửi hãng ⇒ đã cấn trừ hoặc bỏ qua có chủ
  ý".
- 6.12 dữ liệu cá nhân: `actorId` là mã người dùng (không phải dữ liệu cá nhân của khách); không log thêm.
- 6.13 quyền: `skipExchangeCredit` dùng quyền `fulfillment.ship` của chính lệnh (tiền khách không mất: phần món trả
  hoàn qua nghĩa vụ); audit ghi cờ. Lọc `orderId` dùng quyền `cash.cod.read` sẵn có.

## Quyết định thay chủ shop

- **QĐ-524-1 (chặn gửi hãng đơn đổi):** máy chủ trả 409 `exchange_credit_pending` khi đơn đổi còn phải cấn trừ (luật
  của consumer: phiếu đổi có tiền hoàn, đơn chưa có phiếu `exchange:<phiếu>`, chưa chốt nghĩa vụ exchange, `min(tiền
  hoàn, tổng − đã thu) > 0`). Lối xử lý khi CRM không ghi được phiếu: thân `skipExchangeCredit: true` (người gửi chọn
  rõ, ghi audit) → gửi với COD đủ; khi kho nhận món trả, consumer mở nghĩa vụ exchange bằng đủ tiền hoàn (đường Task
  116 đã test: `TestExchangeCredit_ShippedBeforeConsumer`). Vì sao: không ghi tiền từ đường gửi hãng, tiền khách không
  mất, dễ đảo (bỏ cờ). Khác: (a) gửi hãng tự ghi phiếu cấn trừ thay consumer — ghi tiền từ lệnh của người không có quyền
  tiền, và báo phí cũ thành stale lặp mãi khi transaction lùi; (b) chỉ chặn, lối thoát là chạy lại việc nền — màn chưa
  có trang việc nền, nhân viên kẹt.
- **QĐ-524-2 (người làm bút toán của bước do nhân viên làm):** `shipment.status_changed` thêm `actorId` khi bước có
  nguồn `staff` và người dùng; `cash` ghi mọi bút toán của sự kiện đó (đảo collected, và collected/returned của sửa tay
  tài khoản nhập tay) với tác nhân `staff` + mã người đó; sự kiện cũ, nguồn khác → `carrier` như trước. Lý do bút toán
  đảo giữ câu tiếng Việt "Nhân viên sửa…" (đã có từ Task 138, là lý do staff_corrected; cột lý do là chữ cho kế toán
  đọc; mã `staff_corrected` nằm ở lịch sử đơn).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Luật "còn phải cấn trừ" = luật consumer (đơn đổi chưa phiếu → true; có phiếu, đơn thường, đơn rẻ hơn đã cấn đủ → false) | `TestExchangeCreditPending_Rules` | `go test -race -count=1 -run '^(TestExchangeCreditPending_.*\|TestExchangeCredit_.*)$' ./internal/modules/cash/` | `ok elimaz.com.vn/crm/internal/modules/cash 4.501s` |
| Đã chốt nghĩa vụ exchange (nhận khi đang gửi, vận đơn gỡ) hay đơn thay thế hủy → không chặn | `TestExchangeCreditPending_SettledOrCancelled`, `TestExchangeCredit_ShippedBeforeConsumer` (thêm câu kiểm) | như trên | ok |
| Song song: 4 lần gửi hãng (Ship + kiểm dưới khóa đơn) và 4 lần giao order.created → vận đơn đã commit thì có đúng 1 phiếu cấn trừ, COD vận đơn 2.951, consumer không lỗi | `TestExchangeCreditPending_ConcurrentShipAndCredit` | như trên | ok |
| API: đơn đổi chưa cấn trừ → 409 `exchange_credit_pending`, không vận đơn, không lệnh gửi hãng; gửi lại cùng khóa sau khi có phiếu → 202 + 1 lệnh; `skipExchangeCredit` → 202 COD đủ 280.000, audit có cờ; đơn thường 202 | `TestCreateShipmentAPI_ExchangeCreditPending` | `go test -race -count=1 -run '^(TestCreateShipmentAPI_ExchangeCreditPending\|TestCreateShipmentAPI_VC03\|TestPaymentsAPI_RefundObligations)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 3.606s` |
| Mã lỗi mới đổi ở một chỗ | `errors_snapshot_test.go` (dòng `cash.ErrExchangeCreditPending` → 409) | cổng kiểm task (gói httpapi) | xanh |
| Lọc nghĩa vụ theo đơn (đúng đơn, ghép trạng thái, đơn không có → rỗng, trang theo con trỏ) | `TestRefund_FilterByOrder` | `go test -race -count=1 -run '^(TestRefund_FilterByOrder\|TestRefund_CancelledDepositTI09)$' ./internal/modules/cash/` | `ok … 2.148s` |
| API `?orderId=` đúng đơn → 1, đơn khác → 0, sai dạng → 400 | `TestPaymentsAPI_RefundObligations` | lệnh httpapi ở trên | ok |
| Kế hoạch câu lọc theo đơn không Seq Scan bảng lớn | `TestM3ListPlans_Large/nghĩa_vụ_hoàn_theo_đơn` | cổng kiểm task từ xa | xanh (run 46133907984: test (1–6) success) |
| Bút toán đảo khi nhân viên đảo: tác nhân staff = đúng người, lý do "Nhân viên sửa…"; hãng / sự kiện cũ → carrier | `TestPostings_ReversalReasonBySource` (5 ca) | `go test -race -count=1 -run '^TestPostings_ReversalReasonBySource$' ./internal/modules/cash/` | `ok … 2.230s` |
| Sự kiện đảo của nhân viên mang `actorId`; bước của hãng không | `TestCorrection_TI08` | `go test -race -count=1 -run '^TestCorrection_TI08$' ./internal/modules/fulfillment/` | `ok … 1.980s` |
| Màn: đơn đổi chờ cấn trừ → tích "Gửi không cấn trừ" thì gửi, thân `skipExchangeCredit: true`; đơn thường không có ô; đơn đổi đã có phiếu thu khác (màn tưởng gửi được) mà máy chủ trả 409 → hiện câu lỗi và ô bỏ qua, gửi lại có cờ | `ShippingPanel.test.tsx` (3 ca mới) | `npx vitest run src/screens/shipping/ShippingPanel.test.tsx src/screens/payments` | `Tests 66 passed` |
| Màn: thẻ Nghĩa vụ hoàn trên đơn (chỉ đơn này, chưa chi, chi gửi đúng nghĩa vụ, Miễn mờ với Kế toán, đã chi đủ không nút, không nghĩa vụ/không quyền → không thẻ, lỗi đọc → câu lỗi) | `OrderRefundsCard.test.tsx` (5 ca) | như trên | passed |
| Câu tiếng Việt cho mã lỗi mới | `src/app/errors.test.ts` | `npx vitest run src/app/errors.test.ts` | passed |
| Cổng tĩnh + test gói ảnh hưởng | cổng kiểm task (ci-task.sh) | ci-task.sh | `CI-TASK XANH` run 46134816575 (static + test 1–6 success); sau sửa review (chỉ console): run 46135825921 static success |

## E2E viết sẵn, chưa chạy

(không có: CRM nội bộ không Playwright theo chủ shop 28/11; đủ ca ở test gói cash, httpapi, fulfillment.)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không có mục kế hoạch riêng; phạm vi theo ghi chú điều phối. Ngoài ba việc: `m3_plans_test.go` thêm ca kế hoạch câu
  lọc theo đơn (tham số mới của câu sqlc); tách helper `shipReplacement`, `detachShipment` trong
  `exchange_credit_test.go` (jscpd), `exchangeReturn` trong `ShippingPanel.test.tsx`.

## Việc phát hiện thêm

- `skipExchangeCredit` khi consumer chỉ chậm (chưa chết): consumer sau đó gặp `ErrCODLockedByShipment` → việc dead +
  cảnh báo dead thường (đúng như đường Task 116, tiền vẫn đúng nhờ nghĩa vụ exchange). Có thể cho consumer bỏ qua êm ca
  "đơn đã gửi có chủ ý" nếu cảnh báo gây nhiễu.
- Console chưa có trang Việc nền (listJobs/retryJob chỉ có API): nhân viên không tự chạy lại việc `cash_refunds` dead.

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA — ô "Gửi không cấn trừ" chỉ hiện theo dấu hiệu "đã thu = 0" của màn; đơn đổi có phiếu thu khác (vd. cọc)
  mà chưa có phiếu cấn trừ thì máy chủ vẫn 409 còn màn không cho lối bỏ qua → kẹt. Đã sửa: 409 `exchange_credit_pending`
  làm hiện ô bỏ qua (`serverWait`), test vitest mới. Theo ghi chú không bắt buộc: nút Miễn hoàn của thẻ mờ khi chưa có
  `cash.refunds.waive` (câu lý do chỉ khi quyền đã tải, như trang Nghĩa vụ hoàn); sửa tên tệp ở mục Test sẽ chạy.
- Lượt 2: ĐẠT, không còn phát hiện phải sửa; bảng bằng chứng đối chiếu từng dòng với test thật. Ghi chú không bắt buộc
  (không sửa): khi `serverWait` bật mà màn đang đọc phiếu đổi, tích ô bỏ qua cũng bỏ câu "đang đọc" (máy chủ vẫn quyết);
  `serverWait` mất nếu `Compose` bị dựng lại (hiện không xảy ra).
