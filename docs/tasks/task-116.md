# Task 116 — Đổi hàng — tiền cấn trừ trên đơn thay thế (M3-25)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (mục "### Task 116 (M3-25)"). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `refunds.Consumer` (gói nội bộ của cash): `struct{}` → có trường `Orders order.Writer`, `Now func() time.Time`
    (`Deps.Now` của module; nil → time.Now); `Handle` đổi receiver giá trị có tên; một chỗ dựng:
    `cash/area_payments.go` (`paymentsConsumers`, nay là phương thức có receiver để lấy `Events`, `Queue`, `Bus` của module).
  - Thêm `refunds.SourceExchange`, `refunds.ErrCODLockedByShipment`. `cash.ErrCODLockedByShipment` đổi định nghĩa thành
    `= refunds.ErrCODLockedByShipment` (cùng một giá trị sentinel; 7 chỗ dùng ngoài test — `cash/payments.go`,
    `internal/httpapi/errors_*.go` — so bằng `errors.Is`, không đổi). Lý do: gói `refunds` không nhập được `cash` (vòng nhập).
  - Không đổi chữ ký hàm xuất nào của `cash`, `order`.
- Gói phụ thuộc ngược (go list): `refunds` ← `cash` ← `internal/httpapi`, `cmd/crm`, `internal/testkit/codtest`.
- Operation OpenAPI bị chạm, oasdiff: không (không đổi `openapi.yaml`; không có operation `createExchange`, dùng
  `createReturn` của M2).
- Chỗ dùng trên màn hình: không đổi màn hình. Phiếu `exchange_credit` hiện ở danh sách phiếu của đơn sẵn có
  (`listOrderPayments`, enum đã có `exchange_credit`).
- Câu sqlc, bảng, pha migration: thêm 4 câu trong `queries/cash/exchange.sql` (hai câu đọc `ord.returns_v`, đọc tổng
  `exchange_credit` theo mã tham chiếu trong `cash.payments`, kiểm đã có nghĩa vụ `exchange` của phiếu trong
  `cash.refund_obligations` theo khóa unique). Không migration (chống trùng bằng kiểm dưới khóa đơn — xem
  Quyết định). Bảng ghi: `cash.payments` (chèn), `cash.refund_obligations` (chèn); đơn đổi qua `order.Writer.ApplyPaymentTotals`.
- Test sẽ chạy: `internal/modules/cash` (vùng rủi ro cao → cả gói qua ci-task, `-race`), `internal/modules/cash/internal/refunds`
  (rapid), gói nhập cash (`internal/httpapi`, `cmd/crm`, `internal/testkit/codtest`) theo `affected.py`. Trên máy chỉ chạy
  từng test mới bằng `-run`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm chung cho
  "cấn trừ"); `order.Writer.CreateReturn` (`exchange`, M2 Task 22) tạo đơn thay thế; `order.Writer.ApplyPaymentTotals`
  (M2 Task 18); `order.LockSnapshot` (khóa đơn nhóm 7); câu `OrderForPayment`, `PaymentsTotal`, `InsertPayment`,
  `InsertRefundObligation` của `queries/cash`; hàm `open`, `orderOf` của consumer.
- Dùng lại / viết mới vì: dùng lại mọi câu, hàm trên. Không dùng `cash.Service.RecordPayment` (từ chối `exchange_credit`,
  cần người dùng; lại ở gói cha — `refunds` không gọi được). `lockPaymentOrder` của `cash` không gọi được từ `refunds` (vòng
  nhập) → `applyCredit` gọi thẳng `orderOf` (đọc người bán qua `OrderForPayment`) rồi `order.LockSnapshot`, cùng cách. Viết mới: `creditReplacement`, `receivedExchange`, `applyCredit` (luật cấn trừ là của task này).

## Soát thiết kế

Khớp kiến trúc (5.x):
- 5.1 chủ dữ liệu: phiếu thu, nghĩa vụ hoàn là của cash (schema `cash`). 5.2: cash không ghi bảng `ord`; đơn đổi qua
  `ApplyPaymentTotals`. 5.3: đọc phiếu đổi, đơn qua `ord.returns_v`, `ord.orders_v`.
- 5.4: phiếu `exchange_credit` + đẩy tổng về đơn trong một transaction (tx của worker). 5.5: consumer chạy sau commit của
  `CreateReturn`/`ReceiveReturn` (sự kiện nội bộ). 5.6 không áp dụng: không gọi ra ngoài.
- 5.7: mọi câu chạy trong tx của worker đã đặt tenant (RLS). 5.8: không `net/http`. 5.9 không áp dụng: không có đường ghi
  HTTP mới (đường ghi là consumer; chống trùng ở 6.2). 5.10, 5.11 không áp dụng. 5.12: theo consumer mẫu `cash_refunds`.

Đúng khi chạy đồng thời (6.x):
- 6.1, 6.2 (giao lại, chạy song song; thêm sau reviewer lượt 1): kho đã nhận phiếu đổi (đã có nghĩa vụ `exchange` của phiếu) →
  `applyCredit` không cấn trừ nữa (phần tiền đã chốt lúc nhận) — chặn đường "gửi hãng → việc tạo dead → nhận phiếu → gỡ vận
  đơn → chạy lại việc" trả tiền hoàn hai lần; lần nhận giữ khóa đơn thay thế tới commit nên lần cấn trừ song song thấy nghĩa vụ.
- 6.1, 6.2 (giao lại, chạy song song): khóa đơn thay thế (`order.LockSnapshot`, `FOR UPDATE`) trước, rồi mới kiểm "đã có
  phiếu `exchange_credit` mã `exchange:<returnId>`" → hai lần giao cùng lúc nối tiếp nhau trên khóa dòng đơn, lần sau thấy
  phiếu đã có và không ghi gì. Nghĩa vụ hoàn: unique `(tenant, order, source, return)` + `ON CONFLICT DO NOTHING`.
  Test: giao hai lần liền và hai goroutine giao song song → đúng một phiếu.
- 6.3 thứ tự khóa (ADR-46): đơn (nhóm 7) rồi `cash.payments`/`refund_obligations` (nhóm 9). Nhận phiếu đổi: khóa đơn thay
  thế (nhóm 7), chèn nghĩa vụ của đơn gốc không khóa đơn gốc (chèn, không sửa dòng đơn). Không khóa hai đơn cùng lúc.
- 6.4, 6.5: tx của worker; hàm chạy lại được (mọi biến gán lại trong thân). 6.6: lỗi tạm → hàng đợi thử lại; đơn thay thế đã
  gửi hãng → `ErrCODLockedByShipment` bọc `queue.ErrPermanent` → việc `dead` ngay có cảnh báo `dead:<kind>` (thường).
- 6.7–6.9 không áp dụng (không gọi ngoài; câu ngắn). 6.10: `int64` đồng; sổ phiếu chỉ ghi thêm. 6.11: cash là vùng rủi ro
  cao → rapid cho luật chia tiền (`credit = min(R, G − đã thu)`, `cod = G − tổng ≥ 0`, `dư = max(R' − credit, 0)`).
- 6.12: payload chỉ mã và số; lỗi chỉ mã đơn, mã phiếu, số tiền.

## Quyết định thay chủ shop

- **QĐ-116-1 (chống trùng):** kiểm tồn tại dưới khóa đơn (`method = 'exchange_credit' AND reference = 'exchange:<returnId>'`),
  không thêm migration unique. Vì sao: mọi lần ghi phiếu của đơn đã khóa dòng đơn trước (ADR-46 nhóm 7), nên kiểm dưới khóa là
  đủ và không phải giữ số migration. Phương án khác: migration mới có unique index một phần (dễ thêm sau nếu có đường ghi
  `exchange_credit` thứ hai không khóa đơn).
- **QĐ-116-2 (người ghi phiếu):** `actor_id = uuid.Nil` (hệ thống), tác nhân order `Type = system` như đơn nháp hết hạn
  (`order/drafts.go`). Vì sao: consumer không có người dùng; `ord.returns_v` không công bố `created_by`. Phương án khác: thêm
  `created_by` vào view rồi ghi người tạo phiếu đổi.
- **QĐ-116-3 (tiền hoàn đổi khi kho nhận thiếu — luật "tính lại khi nhận"):** lúc tạo đơn thay thế: `credit = min(R, G − đã
  thu)` với R là tiền hoàn của phiếu lúc tạo. Lúc kho nhận phiếu đổi: R' = tiền hoàn cuối (payload `refundAmount`); nếu chưa có
  phiếu cấn trừ (sự kiện tạo đơn chưa chạy — hai sự kiện thuộc hai đối tượng khác nhau nên không có thứ tự) thì cấn trừ ngay
  bằng R'; đơn thay thế đã gửi hãng mà chưa cấn trừ → không cấn trừ (khách trả đủ COD), credit = 0; nghĩa vụ hoàn `exchange` của **đơn gốc** = R' − credit
  khi > 0. R' < credit (khách được cấn nhiều hơn số hàng về): không mở nghĩa vụ, không tự sửa phiếu (sổ phiếu chỉ ghi thêm,
  `exchange_credit` phải dương) — ghi ở "Việc phát hiện thêm". Nghĩa vụ đặt ở đơn gốc vì tiền hoàn là của hàng trả (như
  `customer_return`); đặt ở đơn thay thế thì `PayRefund` sẽ làm giảm tổng đã thanh toán của đơn đó.
- **QĐ-116-4 (đơn thay thế đã hủy, phiếu đổi đã hủy):** không ghi phiếu cấn trừ; phiếu cấn trừ đã ghi trước khi đơn thay thế
  bị hủy không còn tính cho khách (credit = 0 lúc nhận → nghĩa vụ đủ R'; khi hủy, `PaidBeforeCancel` đã loại `exchange_credit`
  nên không hoàn hai lần). Phiếu đổi bị hủy thì đơn thay thế bị hủy cùng (M2 `CancelReturn`), nên chỉ cần kiểm đơn dưới khóa.
  Đơn thay thế không còn ở draft/new/confirmed (đã giao…) mà chưa cấn trừ → không cấn trừ. Đơn thay thế đã gửi hãng khi consumer chạy:
  lỗi vĩnh viễn `ErrCODLockedByShipment` (theo kế hoạch). Không phát `payment.received` cho phiếu cấn trừ (chưa consumer nào
  nghe; kiểu sự kiện ở gói cha) — ghi ở "Việc phát hiện thêm".

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TD06/AT14: phiếu exchange_credit 177.049 (mã `exchange:<phiếu>`, người ghi hệ thống), cod 2.951, partial, không phiếu tiền mặt, đơn gốc không đổi dòng/tổng/trạng thái/tiền; nhận đủ → không nghĩa vụ | `TestExchangeCredit_TD06` | (A) `go test -race -count=1 -run '^(TestExchangeCredit_.*\|TestRefundsConsumer_.*)$' ./internal/modules/cash/` | `--- PASS: TestExchangeCredit_TD06` |
| Món rẻ hơn 150.000: cấn trừ 150.000, cod 0, paid; chưa nhận → không nghĩa vụ; nhận → nghĩa vụ exchange 27.049 của đơn gốc gắn phiếu, giao lại vẫn một | `TestExchangeCredit_CheaperReplacement` | (A) | `--- PASS: TestExchangeCredit_CheaperReplacement` |
| order.created giao hai lần (liền và song song) → một phiếu, đơn không đổi sau lần đầu | `TestExchangeCredit_Idempotent`, `TestExchangeCredit_ConcurrentFirst` (4 goroutine ở lần đầu) | (A) | `--- PASS` cả hai |
| Đơn thay thế đã gửi hãng → `ErrCODLockedByShipment` + `ErrPermanent`, không phiếu, COD giữ; nhận sau đó → nghĩa vụ đủ 177.049; gỡ vận đơn rồi chạy lại order.created → vẫn 0 phiếu (không hoàn hai lần; phát hiện của reviewer lượt 1) | `TestExchangeCredit_ShippedBeforeConsumer` | (A) | `--- PASS: TestExchangeCredit_ShippedBeforeConsumer` |
| Luật "tính lại khi nhận": nhận trước khi tạo → cấn trừ một lần, dư 27.049 | `TestExchangeCredit_ReceivedBeforeCreated` | (A) | `--- PASS` |
| Đơn thay thế bị hủy sau cấn trừ → nghĩa vụ đủ 177.049; giao lại không ghi thêm | `TestExchangeCredit_ReplacementCancelled` | (A) | `--- PASS` |
| Phiếu đổi lạ → `ErrPermanent`; ca cũ của consumer giữ nguyên | `TestExchangeCredit_UnknownReturn`, `TestRefundsConsumer_NoObligation`, `TestRefundsConsumer_CustomerReturn` | (A) | `--- PASS` cả ba; `ok elimaz.com.vn/crm/internal/modules/cash 2.621s` |
| Quy luật chia tiền (rapid) và ca biên | `TestExchangeSplitProperty`, `TestExchangeSplitEdges` | `go test -count=1 ./internal/modules/cash/internal/refunds/` | `ok .../cash/internal/refunds 0.463s` |
| Cả gói bị ảnh hưởng + cổng tĩnh | ci-task | ci-task.sh (từ xa, github; race: cash và gói con; thường: cmd/crm, httpapi, httpapi/web, conversation, fulfillment, files, realtime, codtest) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/46063492912` (lượt sau khi sửa phát hiện reviewer; lượt trước 46061906495 cũng xanh), `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh f1af3db7…)`; static success (lint, jscpd, test-guard, test kiến trúc), test (1)–(6) success. Hai lượt trước đỏ ở cổng tĩnh (gocognit, revive, jscpd, test-guard; rồi contextcheck) — đã sửa trong test |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có test e2e cho task này)

## Test bị xóa hoặc tắt

- `internal/modules/cash/refunds_test.go` `TestRefundsConsumer_NoObligation`: bỏ phần tử `order.ReturnExchange` khỏi vòng
  lặp; vòng còn một phần tử nên thay bằng hàm dựng `returnEvent` (câu `t.Fatal(err)` của json.Marshal chuyển vào hàm dựng
  thành `e.t.Fatal`, nên số câu kiểm `t.` của tệp giảm 30 → 29, không mất kiểm tra nào). Lý do và ca thay thế ở "Khác kế hoạch".

## Khác kế hoạch

- Kế hoạch ghi sửa `order.ReturnExchange` → `order.OrderTypeExchange` ở commit cấu trúc riêng: dòng đó nằm đúng trong nhánh
  `order.created` được viết lại ở task này (cùng chuỗi "exchange", không đổi hành vi), nên gộp vào commit hành vi.
- `handleExchange` của khung tách thành `creditReplacement` (order.created) và `receivedExchange` (kho nhận) cùng
  `applyCredit` dùng chung, trong `refunds/exchange.go`.
- `refunds_test.go` `TestRefundsConsumer_NoObligation`: bỏ ca phiếu `exchange` với mã phiếu ngẫu nhiên (trước: không nghĩa vụ,
  không lỗi; nay đúng là `queue.ErrPermanent` vì phiếu không có) — ca đó chuyển sang `TestExchangeCredit_UnknownReturn` (chặt hơn).
  Thêm hàm dựng `returnEvent` dùng chung cho hai test.
- Thêm test ngoài kế hoạch: song song lần đầu, nhận trước khi tạo, đơn thay thế bị hủy, phiếu lạ, rapid chia tiền.

## Việc phát hiện thêm

- Kho nhận thiếu làm tiền hoàn cuối R' nhỏ hơn số đã cấn trừ: khách được cấn nhiều hơn số hàng về. Không tự sửa được (sổ phiếu
  chỉ ghi thêm, `exchange_credit` phải dương, đơn thay thế có thể đã gửi hãng). Cần một đường cho nhân viên (cảnh báo hoặc phiếu
  điều chỉnh) — đề xuất gom vào màn đổi hàng Task 138 hay một task cash sau.
- Phiếu cấn trừ không phát `payment.received` (kiểu sự kiện ở gói `cash`, consumer ở gói con; chưa ai nghe sự kiện này). Khi
  có consumer cần mọi phiếu thu thì dời `PaymentReceived` xuống gói dùng chung của cash.
- Màn đổi hàng (Task 138): chỉ cho gửi hãng đơn thay thế sau khi có phiếu cấn trừ (test `TestExchangeCredit_ShippedBeforeConsumer`).

## Reviewer

- Lượt 1: CẦN SỬA — một phát hiện: đơn thay thế đã gửi hãng → việc `order.created` dead → kho nhận phiếu (nghĩa vụ đủ R') →
  vận đơn bị gỡ → chạy lại việc → cấn trừ thêm → khách được hoàn hai lần. Đã sửa: `applyCredit` không cấn trừ khi đã có
  nghĩa vụ `exchange` của phiếu (câu `ExchangeObligationExists`), thêm bước gỡ vận đơn + chạy lại vào
  `TestExchangeCredit_ShippedBeforeConsumer`. Ghi chú không bắt buộc (chú thích test, mục Tái dùng nhắc `lockOrder`) đã sửa.
- Lượt 2: ĐẠT — không còn phát hiện phải sửa. Bản sửa kiểm nghĩa vụ dưới khóa đơn thay thế đúng khi chạy song song (READ
  COMMITTED), giao lại không mở thêm nghĩa vụ (`ON CONFLICT DO NOTHING`), ca R' = 0 đúng (refund_amount ghi lại lúc nhận), test
  gỡ vận đơn chặn được lỗi. Ghi chú không bắt buộc: số câu sqlc trong mục Ảnh hưởng (đã sửa thành 4).

Kết luận: ĐẠT
