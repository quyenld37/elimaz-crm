# Task 433 — Mã giảm giá: trả lượt khi hãng hoàn, mã 0đ, sửa đơn khi mã hết hạn (C-03/04/05)

Kế hoạch: task bù sau review độc lập M2 — `docs/review-doc-lap-m2.md` dòng C-03, C-04, C-05 (Phụ lục vùng C); hành vi chốt ở `.dieu-phoi-m5/so-dieu-phoi.md` mục "Chủ shop 27/11 khuya". Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (grep, gopls): `promotion.Reprice` đổi chữ ký `(ctx, tx, orderID, in EvalInput)` → `(ctx, tx, orderID, base int64)` — 4 chỗ gọi: `order/edit.go` (1), `promotion/redeem_test.go` (2), `promotion/merge_test.go` (1). Thêm `promotion.ReasonZeroDiscount`. `promotion.Evaluate` giữ chữ ký, thêm lý do `zero_discount` (chỗ gọi: `Redeem`, `Check` trong gói; test `evaluate_test.go`). `promotion.EvalInput` thêm một trường không xuất (`phoneOwned`), không đổi chỗ dựng.
- Hàm không xuất đổi: `order.(*editRun).applyCoupon` (giữ mã → `Reprice(base)`, không dựng `evalInput` nữa), `order.(Writer).applyCoupon` (bỏ nhánh `Discount == 0` không còn tới được), `order.(Writer).receiveCarrier` (+ `releaseCarrierCoupon` mới, `returns_qty.go`), `promotion.rejectReason` (bỏ `min_subtotal`, chuyển sang `amountOf`), `promotion.boundMatches` (+ `phoneOwned`). Tệp khác: `openapi/breaking-approved.yaml` (Quyết định 5), `docs/review-doc-lap-m2.md` (đánh dấu đã sửa), mã sinh ogen và `console/src/api/types.gen.ts` (`make gen`).
- Gói phụ thuộc ngược (`go list`): promotion ← `order`, `httpapi`, `httpapi/web`, `wiring/webcache`, `cmd/crm`; order ← httpapi, web, cmd/crm… (ci-task tự tính).
- Operation OpenAPI bị chạm, oasdiff: thêm giá trị `zero_discount` vào enum `CouponRejectReason` (dùng ở `checkCoupon` 200 và thân lỗi `coupon_rejected` của `createOrder`, `editOrder`, đơn web) và enum `WebQuoteCoupon.reason` (`webQuote`, `webCreateOrder`…). Thêm giá trị enum ở phản hồi: oasdiff xem là cảnh báo, không chặn (kiểm bằng `make oasdiff`).
- Chỗ dùng trên màn hình: `console/src/app/errors.ts` `COUPON_REASON_MESSAGES` thêm `zero_discount: "Mã không giảm được cho đơn này"`; `orderRules.test.ts` (bảng đủ lý do), `labels.test.ts` (mọi lý do có chữ) đọc lại; `console/src/api/types.gen.ts` sinh lại.
- Câu sqlc, bảng, pha migration: không câu SQL mới, không migration. Đọc `customer` qua public API `customer.HasPhone` (đã có, chỉ đọc).
- Test sẽ chạy: promotion `TestEvaluate`, `TestReprice`, `TestReprice_ZeroDiscount`, `TestRedeem_ZeroDiscount` (gồm Check), `TestRedeem_EmitsCouponUsed`, `TestCheck`, các test gộp khách (`merge_test.go`); order (tệp mới `coupon_review_test.go`) `TestCarrierReturn_ReleasesCoupon`, `TestCarrierReturn_KeepsCouponWhenFlagOff`, `TestCarrierReturn_ConcurrentReceiveReleasesOnce`, `TestCustomerReturn_KeepsCoupon`, `TestCreateCoupon_ZeroDiscount`, `TestEdit_KeepCouponAfterExpiry`, `TestCoupon_BoundPhoneSameRuleCreateAndEdit`; có sẵn `TestEdit_Coupon`, `TestEdit_Invariants`, `TestCreateFromWeb_Invariants`, `TestQuoteWeb_*`, `TestCreateCoupon_BoundPhoneUsesRecipient`, `TestReceiveReturnQty_CarrierSplit`; console vitest `orderRules.test.ts`, `labels.test.ts`, `errors.test.ts`. Cả gói order, promotion (-race) và gói phụ thuộc chạy ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm trả lượt mã ngoài `promotion.Release`/`Detach`); `customer.HasPhone` (Task 202: SĐT có là danh tính của nhóm khách) — đúng việc cần cho luật SĐT gắn mã.
- Dùng lại / viết mới vì: dùng lại `promotion.Release` (trả lượt khi hãng hoàn, cùng đường với hủy đơn: tôn trọng `release_on_cancel`, ghi `release_reason`), `customer.HasPhone`. Viết mới hàm thuần `amountOf` và `ownsBoundPhone`; helper test `createCoupon`, `couponOrderEdited` (gộp 4 chỗ tạo mã lặp trong test order) (tách phần tính tiền khỏi `Evaluate` để `Reprice` dùng chung, không lặp công thức).

## Soát thiết kế

- 5.1 Chủ dữ liệu: lượt mã thuộc promotion; không bảng mới. 5.2 order chỉ gọi public API `promotion.Release`; promotion chỉ gọi `customer.HasPhone` (đọc). 5.3 Không đọc thẳng bảng module khác (HasPhone là API của customer). 5.4 Trả lượt khi kho nhận kiện hoàn nằm trong cùng transaction nhận phiếu (đổi đơn returned + nhả hàng). 5.5, 5.6 không áp dụng: không sự kiện, lệnh ngoài mới. 5.7 Mọi câu trong tx tenant của bên gọi. 5.8 Module không nhập HTTP. 5.9 Không thêm đường ghi; nhận phiếu, sửa đơn, tạo đơn giữ khóa chống trùng sẵn có. 5.10, 5.11 không áp dụng. 5.12 Theo mẫu `cancel.go` (Release trước tồn).
- 6.1 Đồng thời: nhận phiếu hoàn và một đơn khác cùng mã → cùng khóa dòng mã (nhóm 10) nên tuần tự; hai lần nhận cùng phiếu → khóa đơn + phiếu ở version, lần sau `ErrVersionConflict`/không ở requested; Release lần hai trả false. Test song song: hai yêu cầu nhận cùng phiếu chạy cùng lúc → lượt chỉ trả một lần.
- 6.2 Gửi lại: không đổi (khóa chống trùng của các đường ghi; Release không trả hai lần).
- 6.3 Thứ tự khóa: nhận phiếu hãng hoàn: đơn, phiếu (nhóm 7) → [vận đơn đã đọc] → coupons → coupon_customer_uses → coupon_redemptions (10, Release) → stock_balances (11). Release đặt trước `ReleaseInTransitByItems`. Luật SĐT: `HasPhone` chỉ đọc, không khóa.
- 6.4 Lỗi tạm: RetryTx của bên gọi; không biến cộng dồn. 6.5 READ COMMITTED; đọc lượt dưới khóa dòng mã.
- 6.6–6.9 không áp dụng: không việc nền, gọi ngoài.
- 6.10 Tiền: phần giảm 0 → không áp (từ chối `zero_discount`), không tiêu lượt; Reprice tính bằng cùng `amountOf`. Tổng đơn vẫn qua `pricing.Compute`.
- 6.11 Quy luật: "used_count = số lượt active", "không có lượt active phần giảm 0" — thêm vào bất biến của test thuộc tính sửa đơn.
- 6.12 Dữ liệu cá nhân: không log SĐT; lý do lỗi chỉ là mã.
- 6.13 Quyền: không đổi.

## Quyết định thay chủ shop

1. **C-05: phần giảm 0đ → từ chối với lý do mới `zero_discount`** ("Mã không giảm được cho đơn này") ở `Evaluate` (sau `min_subtotal`), nên `Check` báo trên form, `Redeem` không tiêu lượt (tạo/sửa đơn trả 422 `coupon_rejected`). Khác: bỏ qua mã im lặng mà vẫn tạo đơn — người lên đơn không biết mã không được áp.
2. **C-03: giữ mã đã áp khi sửa đơn** — `Reprice` chỉ kiểm luật phụ thuộc tiền (`min_subtotal`, phần giảm > 0) rồi tính lại; không kiểm lại tắt/hạn/kênh/người bán/gắn khách (đã kiểm lúc áp). Áp mã MỚI lúc sửa vẫn qua `Redeem` đủ luật hiện hành. Khác: hỏi lại mọi luật (hành vi cũ) — khách mất ưu đãi đã hứa.
3. **C-03 luật SĐT gắn mã, chung cho tạo và sửa**: mã gắn SĐT khớp khi SĐT của đơn (lúc tạo: SĐT nhập cho khách, không thì SĐT người nhận; lúc sửa: SĐT người nhận) trùng, **hoặc** SĐT gắn mã là danh tính SĐT của nhóm khách của đơn (`customer.HasPhone`). Vế danh tính kiểm ở cả `Redeem` và `Check`, nên đơn đặt hộ của khách có SĐT gắn mã dùng được mã lúc tạo lẫn lúc sửa. Khác: lưu SĐT dùng lúc tạo vào đơn (cần migration, thêm dữ liệu cá nhân). Đảo ngược dễ: một nhánh trong `boundMatches`.
4. **C-04: kho nhận kiện hoàn của hãng (phiếu `carrier_return`, đơn confirmed → returned) → `promotion.Release` lý do `carrier_returned`**, như hủy trước khi giao: trả lượt tổng và lượt của khách, dòng lượt `released` kèm lý do (nhật ký), mã tắt `release_on_cancel` thì giữ lượt (cùng cờ với hủy đơn). Phiếu giao một phần, khách trả, đổi hàng → không trả lượt. Trả lượt lúc kho nhận (đơn sang returned), không lúc hãng báo hoàn (đơn còn confirmed, hàng chưa về). Khác: trả lượt khi vận đơn báo "đang hoàn".

5. **Hợp đồng API: thêm giá trị `zero_discount` vào enum lý do mã** (`CouponRejectReason`, `WebQuoteCoupon.reason`). Vì thân lỗi chung (`Error.reason`) tham chiếu enum này nên oasdiff báo "thêm giá trị enum ở phản hồi" cho 345 operation; đã thêm 345 dòng vào `openapi/breaking-approved.yaml` (cùng một lý do, như Task 517). Chỉ thêm giá trị, không đổi kiểu; console sinh lại và có câu tiếng Việt; website gặp lý do lạ thì hiện câu chung. Khác: không thêm lý do mới mà dùng `min_subtotal` (sai nghĩa với mã không có mức tối thiểu).
6. **Đơn web: giỏ chỉ có hàng của chiến dịch cấm mã + mã** → trước đây mã "áp" 0đ và tiêu lượt; nay báo giá trả `applied=false, reason=zero_discount`, gửi thẳng (sync) bị từ chối `coupon_invalid`, gửi qua hàng đợi thì tạo đơn không mã và đánh dấu cần xem (đường có sẵn cho mã bị từ chối). Test thuộc tính `TestCreateFromWeb_Invariants` sửa mô hình (`couponBase`) cho khớp luật mới.

## Dừng để hỏi

(không có)

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| C-04: kho nhận kiện hãng hoàn → trả lượt tổng + lượt khách, dòng lượt `released` lý do `carrier_returned`, mã 1 lượt dùng lại được | `order` `TestCarrierReturn_ReleasesCoupon` | `go test -race -count=1 -run '^(TestEdit_Coupon\|TestEdit_KeepCouponAfterExpiry\|TestCarrierReturn_ReleasesCoupon\|TestCarrierReturn_ConcurrentReceiveReleasesOnce\|TestReceiveReturnQty_CarrierSplit\|TestCoupon_BoundPhoneSameRuleCreateAndEdit)$' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 2.450s` |
| C-04: mã tắt `release_on_cancel` → giữ lượt; khách đã nhận rồi trả → không trả lượt | `TestCarrierReturn_KeepsCouponWhenFlagOff`, `TestCustomerReturn_KeepsCoupon` | lần chạy lẻ đầu (9 test của tệp) + ci-task (gói order -race) | ok |
| C-04 đồng thời: hai lần nhận cùng phiếu song song → đúng một lần thành công, lượt trả đúng một lần, mã khác không đổi | `TestCarrierReturn_ConcurrentReceiveReleasesOnce` | như dòng đầu (-race) | ok |
| C-05: phần giảm 0 → `zero_discount`; Redeem không tăng lượt, không dòng lượt, không sự kiện; Check báo cùng lý do; Reprice ra 0 → từ chối | `promotion` `TestEvaluate` (4 ca mới), `TestRedeem_ZeroDiscount`, `TestReprice_ZeroDiscount`, `TestRedeem_EmitsCouponUsed`; `order` `TestCreateCoupon_ZeroDiscount`, `TestQuoteWeb_CampaignAndCoupon` | `go test -race -count=1 -run '^(TestEvaluate\|TestReprice\|TestReprice_ZeroDiscount\|TestRedeem_ZeroDiscount\|TestCheck\|TestRedeemMerge.*\|Test.*Merge.*)$' ./internal/modules/promotion/`; `go test -race -count=1 -run '^TestRedeem_EmitsCouponUsed$' ./internal/modules/promotion/` | `ok elimaz.com.vn/crm/internal/modules/promotion 4.025s`; `ok … 2.059s` |
| C-03: mã hết hạn rồi bị tắt sau lúc áp → sửa số lượng giữ mã, tính lại (93.000, 86.000); dưới `min_subtotal` vẫn từ chối; bỏ rồi áp lại → luật hiện hành (`disabled`, `expired`) | `order` `TestEdit_KeepCouponAfterExpiry`; `promotion` `TestReprice` | như dòng đầu và dòng C-05 | ok |
| C-03: luật SĐT gắn mã giống nhau lúc tạo và lúc sửa (danh tính SĐT của nhóm khách) | `TestCoupon_BoundPhoneSameRuleCreateAndEdit`, `TestCreateCoupon_BoundPhoneUsesRecipient` | như dòng đầu | ok |
| Bất biến: không lượt active giảm 0; used_count = số lượt active; đơn web theo luật mới | `TestEdit_Invariants`, `TestCreateFromWeb_Invariants`, `TestQuoteWeb_*` (rapid) | `go test -race -count=1 -run '^(TestCustomerReturn_KeepsCoupon\|TestCreateFromWeb_Invariants\|TestQuoteWeb.*\|TestEdit_Property.*\|TestEdit_Invariants.*)$' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 153.865s` |
| Màn hình: lý do `zero_discount` có câu "Mã không giảm được cho đơn này" | `orderRules.test.ts`, `labels.test.ts`, `errors.test.ts` | `cd console && npx vitest run src/screens/len-don/orderRules.test.ts src/screens/don/labels.test.ts src/app/errors.test.ts`; `npx tsc --noEmit`; `npx eslint src/app/errors.ts src/screens/len-don/orderRules.test.ts` | `Test Files 3 passed (3)`, `Tests 56 passed (56)`; tsc, eslint sạch |
| Hợp đồng: thêm enum đã duyệt | oasdiff | `make oasdiff` | exit 0 (`đã duyệt: … zero_discount …`) |
| Cổng tĩnh + test gói đổi và phụ thuộc (order, promotion -race; httpapi, web, customer, fulfillment… thường) | ci-task | ci-task (lượt cuối, sau khi sửa gocognit, jscpd, `TestRedeem_EmitsCouponUsed` của lượt đỏ trước) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/46043924991`; `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 0b5c878e…)` |

## Test bị xóa hoặc tắt

Không xóa, không tắt test nào. Đổi kỳ vọng theo luật mới (C-05):
- `promotion/redeem_test.go` `TestRedeem_EmitsCouponUsed`: lượt thứ hai với tạm tính 0 trước đây được dùng (tiêu lượt, phát `coupon.used`); nay mong `zero_discount`, không phát sự kiện, `used_count` cuối 1 (trước 2).
- `order/web_property_test.go` `TestCreateFromWeb_Invariants`: mô hình ca thêm `couponBase` (giỏ chỉ có dòng chiến dịch cấm mã + mã → mã không áp được).
- `order/webquote_test.go` `TestQuoteWeb_CampaignAndCoupon`: ca "chỉ dòng chiến dịch" kiểm thêm `applied=false, reason=zero_discount`.
- `order/edit_test.go`: `TestEdit_Coupon` dùng helper chung `couponOrderEdited`; `editCoupon` dùng `createCoupon` (bỏ mã lặp, jscpd). Kỳ vọng không đổi.

## Khác kế hoạch

- Task bù sau review (không có trong tệp kế hoạch M5): làm theo `docs/review-doc-lap-m2.md` C-03/04/05 và quyết định đã chốt; bảng của review cập nhật "Đã sửa (Task 433)".
- `receiveCarrier` tách `releaseCarrierCoupon` (gocognit > 20 ở lượt ci-task đầu).
- ci-task chạy 4 lượt: lượt 1 bị dừng ở giới hạn 10 phút của lệnh nền (lỗi của phiên, không phải của ci-task); lượt 2 đỏ (gocognit, jscpd, `TestRedeem_EmitsCouponUsed`); lượt 3 đỏ (jscpd: helper tạo mã lặp); lượt 4 xanh.

## Việc phát hiện thêm

- Website (`apps/storefront*`, ngoài repo CRM): nên có câu riêng cho lý do mã `zero_discount` ("Mã không giảm được cho đơn này") ở ô nhập mã; hiện đọc lý do lạ thành câu chung.
- `openapi/breaking-approved.yaml`: 346 dòng của Task 433 (gồm `webFlashBuy` thêm lúc gộp) xóa sau khi gộp vào main (theo hướng dẫn đầu tệp).

- (Gộp vào main) Sau Task 435: lên đơn khách mới với SĐT đã thuộc khách X → `cust.Phone` vẫn là SĐT của X nên mã gắn SĐT của X áp được, lượt mỗi khách tính trên khách mới. Chủ shop quyết có dùng `PhoneConflictWith` để không coi SĐT đó là SĐT của đơn khi kiểm mã gắn SĐT hay không.

## Reviewer

Kết luận: ĐẠT (lượt 1 ĐẠT, không phát hiện phải sửa; lượt 2 ĐẠT sau khi khôi phục 3 dòng chú thích của `TestEdit_LockedByShipment` mà lượt 1 ghi chú bị xóa nhầm).

- Đã kiểm: thứ tự khóa ở đường nhận phiếu hãng hoàn (đơn, phiếu nhóm 7 → `Release` nhóm 10 → `ReleaseInTransitByItems` nhóm 11, giống `cancel.go`); `Release` gọi hai lần/song song không trừ thêm (hai lần nhận cùng phiếu tuần tự ở khóa đơn, lần sau gặp phiếu không còn requested); `ownsBoundPhone` chỉ đọc, gán lại mỗi lần (an toàn khi RetryTx chạy lại); `Reprice` chỉ `amountOf`; đường web sync/queued với `zero_discount`; số chỗ gọi `Reprice`, tên test khớp mã thật.
- Ghi chú không bắt buộc (không sửa): `webquote.go:289` nhánh `r.Discount == 0` không còn tới được; báo giá web gọi Check không có `CustomerID` nên vế "SĐT của nhóm khách" không chạy trên báo giá (như trước, không lùi); mã giữ lại khi sửa đơn không kiểm lại gắn khách (đúng Quyết định 2).

## Gộp vào main

- Lệnh: `git merge --no-ff --no-commit task-433` trên main `b07c801d`. Không có xung đột chữ: mọi tệp chung (`openapi.yaml`, mã sinh `internal/httpapi/gen/*`, `console/src/api/types.gen.ts`, `console/src/app/errors.ts`, `orderRules.test.ts`) tự gộp.
- Mã sinh: `make gen` trên bản gộp không tạo chênh với phần đã stage (mã sinh khớp nguồn). `go build ./...`, `go vet` gói order, promotion, httpapi: qua.
- `openapi/breaking-approved.yaml`: giữ dòng của Task 517 (main) và 345 dòng của Task 433 (cùng cách main đã làm với Task 517); việc xóa dòng sau gộp đã ghi ở "Việc phát hiện thêm".
- Reviewer lượt gộp 1 (CẦN SỬA): `webFlashBuy` (Task 203, vào main sau điểm tách nhánh) trả `Error` có `reason` → `CouponRejectReason`, nên thêm `zero_discount` cũng là thay đổi phá vỡ của nó mà 345 dòng không có. Sửa: thêm dòng duyệt `webFlashBuy` cùng lý do. `make oasdiff` (so với main) trên bản gộp: thoát 0, mọi dòng "đã duyệt".
- Ghi chú reviewer (không sửa, ngoài phạm vi gộp): sau Task 435, đơn "khách mới" mang SĐT của khách X vẫn khớp mã gắn SĐT của X (so chuỗi `cust.Phone`), giới hạn lượt mỗi khách tính trên khách mới. Ghi vào "Việc phát hiện thêm".
- Test từ xa lượt 1 (cây `009f99fa`, 23 gói gồm cmd/crm, httpapi, httpapi/web, order, promotion, cash, fulfillment, customer, conversation, tests/arch…; `-race` order, promotion): run 46053981307 trên github — 6 phần test xanh, cổng tĩnh đỏ ở oasdiff (chính lỗi `webFlashBuy` ở trên). Không có test chập chờn.
- Lượt 2 sau sửa (chỉ đổi `breaking-approved.yaml` và tệp task; cổng tĩnh + `./tests/arch`): run 46055323402 — `static: success`, `test (1): success`, `MERGE-TEST XANH (github)`.
- Reviewer lượt gộp 2: ĐẠT (dòng `webFlashBuy` có, phần còn lại giữ đủ hai bên; đã sửa số 345 → 346 dòng theo ghi chú).
