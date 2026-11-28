# Task 138 — Tiền và trả/đổi trên đơn (M3-47)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` mục "Task 138 (M3-47)"; mockup `docs/mockups/m3/tra-doi.html` (màn 6), `docs/mockups/m3/tien-tren-don.html` (màn 8), HV26–HV29, HV33–HV35. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm trường `Source string` (json `source,omitempty`) vào
  `fulfillment.StatusChanged` (struct payload của `shipment.status_changed`; 14 tệp nhắc tên, mọi chỗ dựng đều theo tên
  trường nên thêm trường không gãy). Không đổi chữ ký hàm nào. Hằng không xuất `reasonStaffCorrected` mới trong
  `internal/modules/cash/consumer.go` (việc chuyển từ Task 118: bút toán đảo ghi lý do theo nguồn).
- Gói phụ thuộc ngược (go list): `fulfillment` được nhập bởi cmd/crm, adapters/carrier/**, httpapi, httpapi/web, cash
  (+ internal/formats, internal/statement), fulfillment/internal/**, testkit/codtest, testkit/shiptest, wiring/webcache,
  tools/fakecarrier/server — chỉ build lại; hành vi chỉ đổi ở cash consumer (đọc trường mới).
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml`. Màn dùng operation có sẵn: `recordPayment`,
  `listOrderPayments`, `listRefundObligations`, `payRefundObligation`, `waiveRefundObligation`, `previewCodAdjustment`,
  `createCodAdjustment`, `getShipmentCod`, `recordPartialDelivery`, `previewShipmentCorrection`,
  `applyShipmentCorrection`, `createReturn`, `getReturn`, `receiveReturn`, `cancelReturn`.
- Chỗ dùng trên màn hình: chi tiết đơn `/don/:id` (`OrderDetail.tsx`: thêm khối "Tiền đã thu"), hộp Trả/đổi
  (`ReturnDialog.tsx`, `returnRules.ts`: cột "Còn được trả", tiền cấn trừ của đơn đổi, sửa dòng thiếu một phần/giao một
  phần), khối Vận chuyển (`ShipmentCard.tsx`: "Đảo Thành công", "Ghi giao một phần"; `CodBox.tsx`: sổ COD và "Điều chỉnh
  COD"; `ShippingPanel.tsx` (`Compose`): đơn đổi chỉ gửi hãng sau khi có phiếu cấn trừ — từ Task 116), API giả của
  test `don/donTestkit.tsx` (listOrderPayments, ghi phiếu) và `shipping/shippingTestkit.tsx` (getReturn), trang mới
  `/refund-obligations` (nhóm menu "Tiền", HV01). Thư mục mới `console/src/screens/payments/` (có `route.tsx`) và
  `console/src/screens/returns/` (chỉ hộp thoại, không route).
- Tài liệu hợp đồng sự kiện: `docs/views.md` dòng `shipment.status_changed` thêm `source` (phát hiện reviewer lượt 1).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest các tệp test mới/sửa (`screens/payments/*.test.tsx`, `screens/returns/*.test.tsx`,
  `screens/don/ReturnDialog.test.tsx`, `screens/don/returnRules.test.ts`, `screens/shipping/ShipmentCard.test.tsx`,
  `screens/shipping/ShippingPanel.test.tsx`, `app/screens.test.tsx`), tsc, eslint; Go: `-run` test mới của cash
  (`TestPostings_ReversalReasonBySource`) và fulfillment (`TestCorrection_TI08` thêm kiểm `source`); cả gói ở ci-task
  (cash, fulfillment là vùng rủi ro cao → cả bộ -race của hai gói).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chỉ gói Go; không có gì
  cho màn); `console/src/shared/`; tìm `formatMoney`, `useWriteAction`, `Dialog`, `Act`, `DialogActions`/`DialogNotes`,
  `useShipmentCod`, `useGrants`/`canAt`, `usePerms`, testkit `donTestkit`, `shippingTestkit`, `screenTestkit`.
- Dùng lại / viết mới vì: dùng lại `formatMoney` (productRules), `useWriteAction` (khóa chống trùng), `Dialog`
  (staffData), `Act`, `DialogActions`, `DialogNotes` (shipping), `useShipmentCod`, `useGrants`+`canAt` (quyền theo kho),
  `refundFor`/`returnableLines` (returnRules), `ReceiveReturnDialog` (ba ô Tốt/Hỏng/Thiếu của Task 136, không viết lại),
  testkit của `don` và `shipping` (mở rộng API giả, không tạo testkit thứ ba cho cùng màn). Viết mới: các hộp thoại tiền
  (chưa có màn nào gọi các operation này), trang Nghĩa vụ hoàn, luật tiền nhỏ `paymentRules.ts` (nhãn phương thức, COD
  sau khi thu, chữ trạng thái nghĩa vụ, tiền cấn trừ của đơn đổi theo `refunds.creditAmount`). Không thêm thư viện.

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1, 5.2: không thêm dữ liệu, không ghi bảng mới; Go chỉ thêm một trường payload sự kiện (fulfillment là chủ sự kiện)
  và cash đọc nó. 5.3: cash vẫn chỉ đọc payload sự kiện, không đọc bảng fulfillment.
- 5.4 Sự kiện: trường mới `omitempty`, sự kiện cũ (không có `source`) đọc ra "" → lý do cũ của hãng (giữ hành vi cũ,
  không cần migration dữ liệu).
- 5.5, 5.6: không việc nền mới, không gọi ra ngoài.
- 5.7 Ngữ cảnh shop: không câu SQL mới. 5.8: module không biết HTTP (không đổi).
- 5.9 Đường ghi: mọi thao tác của màn qua `useWriteAction` (khóa sinh lúc bấm, giữ khi tự gửi lại); "đảo", "điều chỉnh"
  hai bước xem trước → xác nhận gửi `token`/`previewToken`; `409 preview_stale` → tự xem trước lại và hiện bản mới kèm
  câu "Số liệu vừa đổi, đây là bản xem trước mới".
- 5.10 Valkey: không áp dụng. 5.11 Không thư viện mới. 5.12 Theo mẫu: màn tự đăng ký `route.tsx`, hộp thoại như khối
  Vận chuyển (Task 134).

Đúng khi chạy đồng thời (mục 6):
- 6.1 Hai người cùng sửa: máy chủ khóa và so token (đảo, điều chỉnh) — token cũ 409 → xem trước lại; phiếu thu vượt
  tổng → 409 `payment_exceeds_total`; chi vượt số còn → 409 `refund_exceeds_remaining` (câu có sẵn trong errors.ts).
- 6.2 Bấm hai lần: nút khóa khi đang gửi, một khóa chống trùng cho một lần bấm.
- 6.3–6.9: không áp dụng (không khóa, hàng đợi, lịch mới). Consumer cash vẫn chạy trong tx của worker; chỉ đổi chuỗi lý
  do, không đổi thứ tự hay khóa.
- 6.10 Tiền: `int64` đồng ở máy chủ; màn nhập số nguyên không dấu, hiện `formatMoney` (9 chữ số), âm có `−`. Tiền cấn
  trừ và COD đơn đổi trên màn chỉ là ước tính trước (`min(R, G)` như `refunds.creditAmount` với đơn mới chưa thu, phí
  ship 0 như `createReplacement`); số thật do consumer ghi.
- 6.11 Quy luật: test thường cho luật tiền nhỏ của màn (không cần rapid: hàm min/max một dòng).
- 6.12 Dữ liệu cá nhân: không log; lý do nhân viên gõ chỉ gửi API (máy chủ đã lọc).
- 6.13 Quyền: khối Tiền đã thu theo `cash.payments.record`; chi hoàn `cash.refunds.record`; miễn `cash.refunds.waive`
  (người khác thấy nút mờ "Chỉ Chủ được miễn hoàn"); danh sách nghĩa vụ `cash.cod.read`; điều chỉnh `cash.adjust`; đảo,
  giao một phần `fulfillment.correct` theo kho (người khác nút mờ kèm lý do, HV29). Máy chủ vẫn kiểm; 403 hiện câu lỗi.

## Bằng chứng

Lệnh (V) = `cd console && npx vitest run src/screens/payments src/screens/returns src/screens/don src/screens/shipping`.
Lần chạy cuối sau khi gộp main: `Test Files  16 passed (16)`, `Tests  181 passed (181)`; thêm
`src/screens/warehouse/ src/screens/ops-lists/ src/app/screens.test.tsx`: `Test Files  21 passed (21)`,
`Tests  243 passed (243)`. Lần đầu (trước khi viết mã) các ca mới đỏ: `Failed Tests 23` (mọi ca mới của PaymentsBox,
RefundObligationsPage, CodAdjustmentDialog, CorrectionDialog, ReturnDialog, returnRules, ShippingPanel; paymentRules
không nạp được module).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Step 1 (1): hộp nhận phiếu trả của M2 có ba ô Tốt / Hỏng / Thiếu, tổng khác số trả thì nút khóa (đã làm ở Task 136, không viết lại) | `OrderDetail.test.tsx` ca "Nhận vào kho" / "Còn thiếu 2 món chưa phân loại (DAM0123-BE-M)." | (V) | `✓` (trong 181 qua) |
| Step 1 (2), TD06, HV28: đổi hàng hiện "Cấn trừ 177.049 ₫ — khách trả thêm 2.951 ₫"; Món mới / Cấn trừ / Phí ship 0 / COD đơn mới; món trả đắt hơn → COD 0, dư thành nghĩa vụ | `ReturnDialog.test.tsx` "món mới 180.000, món trả 177.049 → …", "món trả đắt hơn: COD 0, …"; `paymentRules.test.ts` `exchangeMoney` (3 ca) | (V) | `✓` |
| HV27: cột "Đã/đang trả", "Còn được trả", ghi mã phiếu đang mở, hộp "Tạo phiếu là đã duyệt … nghĩa vụ hoàn 620.000 ₫" | `ReturnDialog.test.tsx` "HV27: cột Còn được trả…", "dòng hết số trả vì phiếu đang mở…" | (V) | `✓` |
| Việc để lại Task 115: tiền hoàn ước tính với dòng trộn Tốt/Hỏng/Thiếu (missing > 0) và phiếu giao một phần, như `priceReturnLines` | `returnRules.test.ts` "dòng thiếu một phần (…mixed…)", "phiếu giao một phần (tiền hoàn 0)…" | (V) | `✓` |
| Step 1 (3), TI09, HV34: nghĩa vụ hoàn hiện "Chưa chi — chưa tính là đã hoàn"; chi một phần ("Sau khi chi: … nghĩa vụ vẫn mở."), vượt số còn → khóa; 409 `refund_exceeds_remaining`; miễn có lý do; không phải Chủ → nút mờ "Chỉ Chủ được miễn hoàn"; lọc trạng thái; rỗng; 9 chữ số | `RefundObligationsPage.test.tsx` (7 ca); `paymentRules.test.ts` `obligationState` | (V) | `✓` |
| Step 1 (4), TI08, HV35: đảo "Thành công" hai bước (lý do → xem trước → xác nhận gửi `previewToken`); bị chặn → câu Blocking, không nút xác nhận, chỉ lối "Điều chỉnh COD"; `409 preview_stale` → bản xem trước mới + câu; không quyền → nút mờ | `CorrectionDialog.test.tsx` describe 'Đảo "Thành công" (TI08)' (4 ca) | (V) | `✓` |
| Step 1 (5), HV35: điều chỉnh COD hai bước; cảnh báo ngăn âm vẫn cho xác nhận; `409 preview_stale` → bản mới, token mới; đảo bút toán: danh sách bỏ bút toán đã đảo và bút toán đảo; sổ COD mới nhất trên đầu, dòng đảo có lý do + người làm | `CodAdjustmentDialog.test.tsx` (4 ca) | (V) | `✓` |
| Step 1 (6), TD10, HV33: ghi cọc hiện COD trước → sau, thân đúng, có khóa chống trùng; vượt số còn phải thu → khóa; đơn đã gửi hãng → nút mờ "Đơn đã gửi hãng, không đổi được tiền thu hộ…"; 409 `cod_locked_by_shipment` → câu trong hộp; không quyền → không có khối | `PaymentsBox.test.tsx` (5 ca đầu); `paymentRules.test.ts` `paymentBlock`, `codAfter` | (V) | `✓` |
| TD09, HV29: giao một phần — món khách nhận, tiền thực thu ≤ COD, lý do, câu tác động, thân gửi đủ mọi dòng; giao đủ → khóa; chờ thu gom chỉ khi cần xem; không quyền → nút mờ | `CorrectionDialog.test.tsx` describe "Giao một phần (TD09, HV29)" (4 ca) | (V) | `✓` |
| Từ Task 116: đơn đổi chỉ gửi hãng sau khi có phiếu cấn trừ | `ShippingPanel.test.tsx` "đơn đổi chưa có phiếu cấn trừ: nút gửi mờ…" | (V) | `✓` |
| Từ Task 116: kho nhận thiếu (tiền hoàn cuối < đã cấn trừ) → cảnh báo chênh lệch và cách xử lý | `PaymentsBox.test.tsx` "đơn đổi: phiếu cấn trừ… kho nhận thiếu → cảnh báo…"; `paymentRules.test.ts` `shortCredit` | (V) | `✓` |
| Từ Task 118 (b): nhân viên đảo → bút toán đảo COD ghi "Nhân viên sửa…"; hãng sửa / sự kiện cũ không `source` → "Hãng sửa…" | `TestPostings_ReversalReasonBySource` | `go test -race -count=1 -run '^(TestPostings_ReversalReasonBySource\|TestPostings_ReversalBeforeRemit)$' ./internal/modules/cash/` | `ok  elimaz.com.vn/crm/internal/modules/cash 2.304s` (trước khi sửa: không biên dịch, `p.Source undefined`) |
| Sự kiện đảo của nhân viên mang `source: staff` | `TestCorrection_TI08` (thêm kiểm `payload->>'source' = 'staff'`) | `go test -race -count=1 -run '^TestCorrection_TI08$' ./internal/modules/fulfillment/` | `ok  elimaz.com.vn/crm/internal/modules/fulfillment 2.187s` |
| Menu "Tiền" có "Nghĩa vụ hoàn", quy ước màn | `src/app/screens.test.tsx` | (V) + `src/app/screens.test.tsx` | `✓` (trong 243 qua) |
| tsc, eslint các tệp sửa | — | `npx tsc --noEmit -p .`; `npx eslint src/screens/payments src/screens/returns <tệp sửa>` | không lỗi |
| Mã lặp | — | `jscpd --min-tokens 50 --min-lines 5 … console/src` (lọc tệp của task) | 0 đoạn trùng của task (3 đoạn lần đầu đã gỡ: hook `useReplacedReturn`, hàm `openExchange` của test, thứ tự import) |
| Cổng tĩnh + test gói bị ảnh hưởng (race: cash và gói con, fulfillment; thường: cmd/crm, adapters/carrier/**, httpapi, httpapi/web, conversation, files, realtime, queries, testkit/**, wiring/**, tools/fakecarrier/**) | ci-task | `ci-task.sh` (từ xa, github) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/46110016597`, `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh cc91925e…)`; static success, test (1)–(6) success. Lượt trước (run 46108966840) chỉ đỏ ở prettier của console (10 tệp), lint/jscpd/6 phần test xanh; lệnh chờ lượt đó bị dừng do giới hạn 10 phút của lệnh nền, kết quả đọc bằng `gh run view` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 138 không có spec Playwright hay tests/e2e; "so ảnh với mockup" của Step R cần Playwright — để
tới lượt chạy Playwright sau khi xong toàn bộ task theo luật chủ shop 24/11)

## Test bị xóa hoặc tắt

(không có. `ReturnDialog.test.tsx` "order.create theo kho khác…": bốn dòng mở hộp ở chế độ đổi hàng chuyển vào hàm
`openExchange` dùng chung, không bỏ câu kiểm nào.)

## Khác kế hoạch

- Tệp thêm ngoài danh sách: `payments/paymentRules.ts` (+test), `payments/RefundDialogs.tsx` (hộp chi, miễn tách khỏi
  trang), `payments/codLedger.ts` (chữ sổ COD), `payments/exchangeData.ts` (hook đọc phiếu đổi của đơn đổi, dùng ở hai
  khối). `returns/` không có `route.tsx`: chỉ chứa hộp thoại mở từ khối Vận chuyển (loadScreens chỉ nạp thư mục có
  route; màn có đường dẫn mới là `/refund-obligations` ở `payments/route.tsx`).
- `ReceiveReturnDialog.tsx` không sửa: ba ô Tốt / Hỏng / Thiếu đã làm ở Task 136 (test có sẵn trong `OrderDetail.test.tsx`).
- Thêm mã Go nhỏ ngoài danh sách tệp (việc chuyển từ Task 118): trường `Source` của `fulfillment.StatusChanged`, lý do
  bút toán đảo theo nguồn trong `cash/consumer.go`.
- Khung COD thêm sổ COD của vận đơn (mockup trạng thái D) và nút "Điều chỉnh COD"; `CodBox` nhận vận đơn, mã đơn, quyền
  điều chỉnh (một chỗ gọi: `ShipmentExtras`).
- Nút "Tạo phiếu đổi" giữ chữ cũ (mockup ghi "Tạo phiếu đổi và đơn mới"; câu dưới tiền đơn đổi đã ghi "Tạo phiếu đổi
  và đơn mới") để không đổi test M2.
- Step R "so ảnh với mockup": chưa chạy (Playwright để cuối, luật 24/11).

## Quyết định thay chủ shop

- **QĐ-138-1 (nghĩa vụ hoàn của một đơn):** `listRefundObligations` không lọc theo đơn, nên nghĩa vụ xem và xử lý ở trang
  `/refund-obligations` (menu Tiền); khối Tiền đã thu của đơn hiện các phiếu "Hoàn tiền cho khách" đã chi. Vì sao: không
  đổi hợp đồng API. Khác: thêm tham số `orderId` cho `listRefundObligations` rồi vẽ thẻ "Nghĩa vụ hoàn · DH…" trên đơn
  như mockup B (ghi ở Việc phát hiện thêm).
- **QĐ-138-2 (lý do bút toán đảo khi nhân viên đảo, từ Task 118):** thêm `source` vào payload `shipment.status_changed`
  (omitempty, sự kiện cũ đọc ra "" → lý do của hãng như trước); cash chọn câu theo nguồn. Người làm của bút toán vẫn là
  loại "carrier" vì payload không mang người dùng (ghi ở Việc phát hiện thêm). Khác: thêm cả `actorId` vào payload
  (rộng hơn "sửa nhỏ phần tiền").
- **QĐ-138-3 (đơn đổi chỉ gửi hãng sau khi có phiếu cấn trừ, từ Task 116):** màn dùng "đơn đã thu > 0" (`prepaidTotal`)
  làm dấu hiệu đã có phiếu cấn trừ, vì người gửi hãng (Bán hàng) không có `cash.payments.record` để đọc phiếu; phiếu đổi
  có tiền hoàn 0 thì không chặn. Chỉ là chặn ở màn (máy chủ vẫn để consumer báo `ErrCODLockedByShipment` khi gửi trước).
  Khác: chặn ở máy chủ trong `createShipment` (đổi hành vi API, task riêng).
- **QĐ-138-4 (kho nhận thiếu làm số đã cấn trừ > tiền hoàn cuối):** không có đường API sửa phiếu cấn trừ (sổ phiếu chỉ
  ghi thêm), nên màn hiện cảnh báo đỏ trên khối Tiền đã thu của đơn đổi với số chênh và cách xử lý (chưa gửi hãng: ghi
  phiếu thu thêm; đã gửi: kế toán thu riêng hoặc ghi chú bỏ qua). Khác: phiếu điều chỉnh cấn trừ (task cash sau).
- **QĐ-138-5 (tiền đơn đổi trên hộp trả/đổi):** màn ước tính `min(tiền hoàn, tổng món mới)`, phí ship 0 (như
  `createReplacement`, `refunds.creditAmount` với đơn mới chưa thu) và ghi rõ "số cuối do CRM ghi". Khác: thêm operation
  xem trước đơn đổi (đổi hợp đồng).
- **QĐ-138-6 (đảo "Thành công" cần lý do trước khi xem trước):** `ShipmentCorrectionRequest` đòi `reason` ở cả bước xem
  trước, nên ô lý do đứng trước nút "Xem trước" và khóa sau khi đã xem (đổi lý do = đóng hộp mở lại). Mockup vẽ lý do và
  tác động cùng lúc.
- **QĐ-138-7 (nút "Ghi giao một phần"):** hiện trên thẻ vận đơn khi đang chuyển, giao thất bại, hoặc chờ thu gom mà vận
  đơn đang "Cần xem" (như `partial.Decide`); trang "Cần xem" đã có lối "Mở đơn" sang đây.

## Việc phát hiện thêm

- `listRefundObligations` chưa lọc theo đơn (QĐ-138-1): thêm `orderId` để vẽ thẻ nghĩa vụ trên chi tiết đơn như mockup.
- Bút toán đảo COD khi nhân viên đảo vẫn có người làm loại "carrier" (payload `shipment.status_changed` không mang người
  dùng): thêm `actorId` vào payload khi bước do nhân viên làm, rồi cash ghi tác nhân staff.
- Kho nhận thiếu hàng của phiếu đổi (QĐ-138-4): cần đường ghi điều chỉnh phiếu cấn trừ hoặc nghĩa vụ "khách nợ shop".
- Chặn gửi hãng đơn đổi chưa cấn trừ mới ở màn (QĐ-138-3); nên chặn ở máy chủ (`createShipment`).
- Nghĩa vụ hoàn không có báo hiệu realtime: trang đọc lại sau thao tác của chính nó, không tự đổi khi người khác chi.

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA — `docs/views.md` (dòng `shipment.status_changed`) chưa ghi trường `source` mới của payload mà cash giờ
  đọc. Đã sửa: thêm `source` (giá trị, khi nào rỗng, cash dùng làm gì), mục Ảnh hưởng nêu tệp này. Theo ghi chú không bắt
  buộc: `openCodes` của `returnRules.ts` tính cả phiếu `received`.
- Lượt 2: ĐẠT, không còn phát hiện phải sửa. Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí.
- Ghi chú không bắt buộc còn lại (không sửa): chữ lý do "Nhân viên sửa…" (cash) khác "Nhân viên đổi…" (dòng thời gian
  đơn, `labels.ts`); ca hiếm của QĐ-138-3 (cash không ghi phiếu cấn trừ → màn chặn gửi hãng mãi; đã ở Việc phát hiện
  thêm: nên chặn ở máy chủ).
- ci-task sau khi sửa: `CI-TASK XANH` (run 46112005886, static success; test Go không đổi so với lượt 46110016597 xanh).
