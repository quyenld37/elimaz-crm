# Task 206 — Màn Đợt flash sale (M5-23)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ đổi `console/`, không đụng Go).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Chỉ GỌI 11 operation có sẵn của Task 199–201: `listFlashSales`, `getFlashSale`, `createFlashSale`, `updateFlashSale`, `addFlashItem`, `updateFlashItem`, `removeFlashItem`, `scheduleFlashSale`, `addFlashSlots`, `closeFlashSale`, `flashSaleLive`; thêm `searchVariants` (catalog.read) để tìm biến thể khi thêm món.
- Chỗ dùng trên màn hình: màn mới `console/src/screens/flash-sales/` (đường `/khuyen-mai/flash-sale`, `/khuyen-mai/flash-sale/:id`, tự đăng ký bằng `route.tsx`, nhóm Khuyến mãi, order 40, quyền `promotion.flash.read`; nút ghi theo `promotion.flash.manage`). Không sửa `main.tsx` (quy ước `loadScreens.ts` tự nạp `route.tsx`). `app/errors.ts` đã có mã lỗi flash từ Task 199; thêm câu `flash_overlap` giữ nguyên, không thêm mã mới. Màn khác: chỉ `web-campaigns/CampaignList.tsx` (đổi ô thao tác sang `shared/ActCell.tsx` để hết đoạn trùng jscpd với danh sách flash; hành vi giữ nguyên, 13 tệp test web-campaigns/app vẫn qua) và `CampaignEditor.tsx` (đã stage từ trước).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest `src/screens/flash-sales/*.test.ts(x)` + `src/app/screens.test.tsx` + `src/app/errors.test.ts`; `tsc --noEmit`, eslint các tệp mới. Không chạy Playwright (luật phiên).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: danh mục là của Go nên không có gì cho màn hình; tìm trong `console/src`: `useWriteAction` (app/idempotency.ts), `Dialog` (staff/staffData), `usePerms` (don/orderData), `vnLocalToIso`/`isoToVnLocal` (ma-giam-gia/couponRules), `shortTime` (app/time), `formatMoney` (products/productRules), `useDebounced` (inventory/Balances), `MoreBar` (ma-giam-gia), `subscribeChanged`/`queryKey`/`entityMeta`, bộ test `screenTestkit`, `routeTestkit`; mẫu cấu trúc màn: `screens/web-campaigns` (Task 458).
- Dùng lại / viết mới vì: dùng lại toàn bộ hạ tầng trên. Viết mới chỉ phần nghiệp vụ flash (luật kiểm giá, suất, giờ; dựng chữ; bảng số trực tiếp) trong `flashRules.ts`, vì không màn nào có luật tương đương. Không chép `campaignRules`: chỉ khác luật, cùng dạng.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

5.x (kiến trúc): không áp dụng cho Go; màn chỉ gọi client sinh qua `unwrap`, lỗi đổi chữ ở `app/errors.ts` (một chỗ); không tự gọi `fetch`. Quyền: màn cần `promotion.flash.read`, nút ghi ẩn khi thiếu `promotion.flash.manage` (API vẫn chặn).
6.x (đúng khi chạy đồng thời): mọi lệnh ghi qua `useWriteAction` (cùng `Idempotency-Key` khi tự gửi lại 503/409/mạng; khóa mới khi nội dung đổi); thân gửi chứa `version` đã thấy (khóa lạc quan, `version_conflict` → "Đợt vừa được người khác sửa, tải lại"); khóa mọi nút khi một lần ghi đang chạy (hộp thoại không đóng giữa chừng); đang tải/lỗi tải không vẽ như dữ liệu rỗng; tự làm mới 5 s khi đợt `open` (lượt mua không phát sự kiện), 20 s các trạng thái khác, và tải lại ngay khi nhận `flash_sale.changed`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Danh sách rỗng, 30 đợt (20 + Xem thêm 10), 4 trạng thái, lọc `?status=`, lỗi tải không vẽ như rỗng | `FlashSaleList.test.tsx`: "không có đợt…", "30 đợt…", "đủ bốn trạng thái…", "lọc trạng thái…", "lỗi tải…" | `npx vitest run src/screens/flash-sales` | PASS (cả thư mục) |
| Tạo đợt gửi `Idempotency-Key`; 503 rồi tự gửi lại CÙNG khóa, chỉ một đợt | `FlashSaleList.test.tsx`: "gửi tên và giờ VN → UTC…", "503 retry_later rồi tự gửi lại đúng cùng Idempotency-Key…" | như trên | PASS |
| Lên lịch thiếu hàng hiện đúng 2 mã hàng (cần, có thể bán, thiếu), đợt vẫn Nháp | `FlashSaleEditor.test.tsx`: "thiếu hàng 2 mã…" | như trên | PASS |
| Đóng sớm cần xác nhận (không gửi trước khi bấm Đóng đợt; Hủy không gửi) | `FlashSaleEditor.test.tsx`: "đóng sớm cần xác nhận…", "Hủy ở hộp đóng sớm…" | như trên | PASS |
| Báo hiệu `flash_sale.changed` → bảng trực tiếp tải lại trong 1 s | `FlashSaleEditor.test.tsx`: "báo hiệu flash_sale.changed…" (`waitFor` timeout 1000 ms) | như trên | PASS |
| Không quyền → không nút ghi (danh sách, nháp, đang mở) | `FlashSaleList.test.tsx`: "không có promotion.flash.manage…"; `FlashSaleEditor.test.tsx`: "không có promotion.flash.manage…", "không quyền quản lý: số trực tiếp vẫn hiện…" | như trên | PASS |
| Tên 120 ký tự xuống dòng không tràn | `FlashSaleList.test.tsx` ("title + lớp clamp2"), `FlashSaleEditor.test.tsx` ("lớp wrap-any") — kiểm lớp CSS, chưa chụp ảnh (không chạy Playwright theo luật phiên) | như trên | PASS (phần chụp: chờ) |
| HV29: sau lên lịch chỉ Thêm suất, Đóng sớm; đã đóng chỉ xem | `flashRules.test.ts` `actionsOf`; `FlashSaleEditor.test.tsx` "thành công…", "Đợt đã đóng" | như trên | PASS |
| HV31/HV28: giá flash < giá thường, suất 1–20.000, mỗi khách 1–10, giữ 5–30 phút, mặc định 1 và 10 | `flashRules.test.ts` `validateItem / toItemInput`; `FlashSaleEditor.test.tsx` "tìm biến thể…", "thêm món hợp lệ…" | như trên | PASS |
| 409 `version_conflict` → "Đợt vừa được người khác sửa, tải lại" + Tải lại đọc lại đợt | `FlashSaleEditor.test.tsx`: "409 version_conflict…" | như trên | PASS |
| Số trực tiếp lỗi tải không vẽ như 0; hộp đóng sớm không bịa số | `FlashSaleEditor.test.tsx`: "số trực tiếp lỗi tải…" | như trên | PASS |
| Sửa chỉ gửi trường đổi + version | `FlashSaleEditor.test.tsx`: "sửa tên, giờ…", "sửa món…" | như trên | PASS |
| Đăng ký route đúng nhóm Khuyến mãi, quyền `promotion.flash.read`, order 40; không phá menu | `route.test.tsx`; `src/app/screens.test.tsx` | `npx vitest run src/screens/flash-sales src/screens/web-campaigns src/app` | PASS: 13 tệp, 193 test |
| Kiểu và lint sạch | — | `npx tsc --noEmit`; `npx eslint src/screens/flash-sales` | không lỗi |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

- QĐ-206-1 (cột "Có thể bán lúc này", dòng "bán thường còn 820 → 520" của mockup): BỎ khỏi màn. Vì `FlashItem` không trả số có thể bán và `listBalances` cần chọn kho (món flash không khai kho trong API); thêm trường phải đổi hợp đồng ngoài phạm vi task. Thay bằng: hộp xác nhận lên lịch nêu số suất giữ từng món; thiếu hàng thì máy chủ trả bảng mã thiếu (đã đủ nhu cầu "biết thiếu gì"). Phương án khác: gọi `listBalances` kho đầu danh sách như `VariantPicker` (đoán kho, dễ sai). Dễ đảo: thêm cột khi hợp đồng có số này (xem Việc phát hiện thêm 1).
- QĐ-206-2 (cột "Đã bán / suất" của danh sách): BỎ. `FlashSaleSummary` không có tổng suất, đã bán; gọi `/live` từng đợt là N+1. Dòng phụ vẫn nêu số món và trạng thái.
- QĐ-206-3 (lỗi chồng giờ nêu tên đợt chồng): máy chủ trả `flash_overlap` không kèm đợt nào, nên hộp lỗi nêu lý do và cách xử lý nhưng không có liên kết "Mở đợt …". Dễ đảo khi hợp đồng thêm trường.
- QĐ-206-4 (đợt đã lên lịch): dùng chung bảng số trực tiếp với đợt đang mở (mockup E vẽ bảng tĩnh): cùng dữ liệu, thêm cột còn trống, giữ; dòng nhỏ "Mỗi khách n · giữ m phút" thay hai cột riêng. Để Thêm suất ở cả hai trạng thái theo Interfaces của kế hoạch.
- QĐ-206-5 (đợt nhiều món): hiện đủ mọi món, không có "Hiện 2 / 12 món" của mockup E (máy chủ không phân trang món; tối đa vài chục dòng).
- QĐ-206-6 (hộp lỗi lên lịch): hiện ngay trong hộp xác nhận (không đóng hộp), để người dùng thấy lỗi cạnh nút thử lại; nội dung đúng mockup D (bảng thiếu hàng, chồng giờ).

## Dừng để hỏi

(không có)

## E2E viết sẵn, chưa chạy

Không có: kế hoạch Task 206 chỉ đòi test thành phần (vitest, máy chủ HTTP giả). Chụp 1512 và 390 so với mockup: CHỜ (luật phiên: không chạy Playwright); việc chụp để lượt kiểm màn hình cuối mốc.

## Khác kế hoạch

- Các chênh lệch với mockup nêu ở QĐ-206-1 đến 5. `console/src/main.tsx` không sửa (màn tự đăng ký bằng `route.tsx`, quy ước `loadScreens.ts`).
- Chữ "Quản lý flash sale" ở dòng chỉ-xem lấy đúng mockup; khóa quyền thật là `promotion.flash.manage`.

## Việc phát hiện thêm

1. `FlashItem`/`FlashSaleSummary` nên trả thêm: số có thể bán của biến thể (để cột "Có thể bán lúc này", "820 → 520", "118 → 68" như mockup), tổng suất và đã bán của đợt (cột "Đã bán / suất"), đợt chồng trong `flash_overlap` (để mở thẳng đợt đó). Cần task hợp đồng riêng (Task 199–200 sở hữu API).
2. Hộp thoại dùng `<dialog>` + `showModal` như `Dialog` của staff; test jsdom phải `stubDialog()` (đã dùng).

## Reviewer

Kết luận: ĐẠT (28/11, reviewer, đọc code + grep; không có phát hiện phải sửa).
Ghi chú không chặn: (1) `shared/ActCell.tsx` chưa export trong `shared/index.ts` nên chưa vào danh mục dùng chung (cổng danh mục xanh; để dọn ở lượt sau); (2) `CampaignEditor.tsx` chỉ đổi `Field` thành export để màn flash dùng (phụ thuộc giữa hai màn); (3) ảnh chụp 1512/390 chờ lượt kiểm màn hình cuối mốc. ci-task XANH (static trên github, run 46133787348).
