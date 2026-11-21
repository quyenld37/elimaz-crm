# Task 437 — M2-r8: Màn tổng tạm tính khi sửa đơn có mã % / mức tối thiểu

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (task bù sau review độc lập M2: `docs/review-doc-lap-m2.md`, phát hiện D-02; số toàn cục 437 = M2-r8). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa console. Máy chủ giữ nguyên: `internal/modules/order/edit.go` `applyCoupon` (giữ mã mà tạm tính sau giảm đổi → `promotion.Reprice`, có thể từ chối `min_subtotal`; không đổi → giữ số cũ). Màn bám đúng luật này.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không có. Màn chỉ dùng trường đã có của `Order` (`couponCode`, `couponDiscount`, `subtotal`, `lineDiscountTotal`, `orderDiscount`).
- Chỗ dùng trên màn hình: `console/src/screens/len-don/OrderForm.tsx` (khối `keepCoupon`/`couponDiscount`/`after`, dòng 245-258; truyền thêm cờ cho `TotalsPanel`), `console/src/screens/len-don/TotalsPanel.tsx` (prop mới `couponRecalc`; grep `TotalsPanel`: chỉ OrderForm dùng). OrderForm dùng ở `/don/moi` (lên đơn: không đổi vì `eo` undefined) và hộp Sửa đơn `console/src/screens/don/EditOrderDialog.tsx`. Test: `console/src/screens/don/OrderDetail.test.tsx` (ca mới), `console/src/screens/don/donTestkit.tsx` (đếm lần gọi coupon-check).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/screens/don src/screens/len-don`, `npx tsc --noEmit`, `npx eslint .`; `scripts/agent/ci-task.sh` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `console/src/shared` (luật: tiền hiện trước chỉ tính bằng `pricing.ts` cạnh OrderForm, không tự cộng trừ ở màn); `pricing.ts` (`compute`), `tryCompute` trong OrderForm, `formatMoney`; grep "tính lại khi lưu", "couponRecalc" trong `console/src`: chưa có.
- Dùng lại / viết mới vì: dùng lại `before` (kết quả `tryCompute` có sẵn với `couponDiscount: 0`) làm tổng chưa trừ mã và `base` có sẵn; không viết hàm tính tiền mới. Thêm một prop cho TotalsPanel (chưa có thành phần nào hiện "số chưa chắc" để dùng lại). Testkit: dùng lại `installFake`/`order()` của `donTestkit.tsx`, thêm một bộ đếm.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5 (khớp kiến trúc): chỉ đổi màn; gọi API qua client sinh (không thêm lời gọi nào); không `as`, không `any`; câu chữ tiếng Việt; không mã dùng chung mới. Các câu về module Go, sqlc, migration, hàng đợi, adapter: không áp dụng (không đổi máy chủ).
- 6.1–6.9 (đồng thời, chống trùng, khóa, lỗi tạm, cách ly, việc nền, 429, chết giữa chừng, treo): không áp dụng — không thêm lệnh ghi; lệnh sửa đơn giữ nguyên `useWriteAction` + `version`.
- 6.10 Tiền: số trên màn vẫn là số tính trước, số máy chủ trả là số cuối. Sửa D-02: khi giữ mã mà tạm tính sau giảm (`subtotal − giảm dòng − giảm cấp đơn`) khác bản đang lưu, máy chủ tính lại mã (`Reprice`, có thể từ chối `min_subtotal`), nên màn không được trừ số giảm cũ: hiện "tính lại khi lưu" cho phần giảm theo mã, Tổng/COD ghi rõ "chưa trừ mã" và tính bằng `pricing.ts` với giảm mã 0 (số đúng với nhãn). Không còn báo lỗi giả "giảm quá tiền hàng" do số mã cũ. Điều kiện so cùng công thức `base` của `applyCoupon` (edit.go:506-507, 533).
- 6.11 Quy luật: không có quy luật vùng rủi ro cao ở màn; vitest phủ: tạm tính không đổi (số cũ), đổi số lượng, đổi giảm cấp đơn, chỉ đổi phí ship (số cũ), đổi lại như cũ (số cũ trở lại), bỏ mã, đơn không có mã.
- 6.12 Dữ liệu cá nhân: không áp dụng (không log, không gửi thêm gì).
- 6.13 Quyền: không đổi (sửa mã vẫn theo `editScope.coupon`).

## Quyết định thay chủ shop

- Chọn cách (a) của review D-02 (chỉ màn): khi giữ mã cũ mà tạm tính sau giảm đổi, dòng "Giảm theo mã" hiện "Tính lại khi lưu", dòng Tổng và Thu hộ đổi nhãn "… (chưa trừ mã)" với số tính bằng `pricing.ts` không trừ mã, thêm một câu dưới khối Tiền nói máy chủ sẽ tính lại mã và đơn dưới mức tối thiểu của mã thì không lưu được. Vì sao: đúng spec "tiền hiển thị đúng, không tính ở client khác máy chủ", không đổi API (giao việc: chỉ màn, không chạm `errors.go`), dễ đảo ngược (một prop). Phương án khác: (b) thêm tham số cho `checkCoupon` bỏ qua lượt của chính đơn để xem trước đúng số giảm — đổi hợp đồng và module promotion (vùng rủi ro cao), ghi ở "Việc phát hiện thêm"; (c) tự tính % ở màn — trái luật "không tự cộng trừ ở màn", không biết điều kiện mã.
- Mockup `docs/mockups/m2/len-don.html` không có trạng thái này; chỉ đổi chữ trong các dòng có sẵn, dùng lớp CSS có sẵn (`hint`), không thêm khối hay bố cục mới. Thêm trạng thái vào mockup cho điều phối viên duyệt: ghi ở "Việc phát hiện thêm".

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Giữ mã, tạm tính sau giảm không đổi: hiện số giảm đang lưu (−186.000), Tổng/COD 1.674.000, không gọi coupon-check | `OrderDetail.test.tsx` › "sửa đơn giữ mã giảm giá" › "tạm tính sau giảm chưa đổi: hiện số giảm đang lưu, tổng chắc; không kiểm mã" | `cd console && npx vitest run src/screens/don/OrderDetail.test.tsx -t "giữ mã"` | 5 passed |
| Đổi số lượng (mã %): "Giảm theo mã: Tính lại khi lưu", "Tổng (chưa trừ mã) 2.550.000", câu báo; đổi lại số cũ thì −186.000 trở lại; 0 lần coupon-check | "đổi số lượng: giảm theo mã 'Tính lại khi lưu', tổng ghi chưa trừ mã; đổi lại như cũ thì số cũ trở lại" | như trên | qua |
| Số giảm cũ lớn hơn tiền hàng sau sửa: không báo lỗi giả "Giảm giá đơn lớn hơn tạm tính", nút Lưu bật (máy chủ quyết mức tối thiểu) | "số giảm cũ lớn hơn tiền hàng sau sửa: …" | như trên | qua |
| Đổi giảm cấp đơn → tính lại; chỉ đổi phí ship → giữ số giảm (cùng điều kiện `base != oldBase` của edit.go:533) | "đổi giảm cấp đơn: tính lại khi lưu; chỉ đổi phí ship: giữ số giảm, tổng chắc" | như trên | qua |
| Bỏ mã / đơn không mã: không có câu tính lại, giảm theo mã 0 | "bỏ mã rồi đổi số lượng: …; đơn không có mã: …" | như trên | qua |
| Test bắt được lỗi cũ | tạm bỏ `\|\| couponRecalc` trong OrderForm.tsx rồi chạy lại | như trên | 3 failed (đổi số lượng, số giảm cũ lớn hơn, đổi giảm cấp đơn); khôi phục → 5 passed |
| Không hỏng màn Lên đơn, Chi tiết đơn | toàn bộ `src/screens/don`, `src/screens/len-don` | `npx vitest run src/screens/don src/screens/len-don` | 11 files, 340 passed (sau gộp main 99c55cc) |
| Cổng tĩnh + console | ci-task | `scripts/agent/ci-task.sh` | "CI-TASK XANH (so với điểm tách nhánh 99c55cce…)"; console 71 files, 898 passed; jscpd 0 clones; oasdiff, test-guard, arch qua |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- (b) API xem trước giảm của mã khi sửa đơn (`checkCoupon` có `orderId` để bỏ qua lượt của chính đơn): màn sẽ hiện đúng số giảm và lý do từ chối trước khi lưu. Cần đổi hợp đồng + module promotion; chờ chủ shop chọn.
- Mockup M2 `len-don.html`: thêm trạng thái "Sửa đơn giữ mã, tạm tính đã đổi" để điều phối viên duyệt câu chữ.
- (Reviewer ghi, có từ trước) `keepCoupon` theo `couponCode` (mã đã chốt khi rời ô) còn lần lưu gửi `couponText` (chữ đang gõ, OrderForm.tsx ~278): gõ mã khác mà chưa rời ô đã bấm lưu thì số tạm hiện theo mã cũ; máy chủ vẫn quyết số cuối nên không sai tiền.

## Reviewer

Kết luận: ĐẠT (vòng 1, agent reviewer trên `git diff --cached` sau khi gộp main 99c55cc). Không có phát hiện phải sửa. Reviewer đối chiếu: `base` của màn (`subtotal − discountTotal` khi mã 0, pricing.ts:87,97) bằng `base` của edit.go:506-507; điều kiện `keepCoupon && couponCode !== "" && base !== savedBase` khớp edit.go:533; `after = before` khi tính lại nên hết lỗi giả chặn nút Lưu; không `as`/`any`, không thêm lời gọi API. Ghi chú không bắt buộc: vấn đề `couponCode`/`couponText` có từ trước (ghi ở Việc phát hiện thêm); mockup chưa có trạng thái mới (đã ghi).
