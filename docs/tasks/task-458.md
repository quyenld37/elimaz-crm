# Task 458 — Màn Chiến dịch giảm giá

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 458, M5-46). Loại việc (quy trình mục 3): A (chỉ màn hình)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa `console/`, không chạm mã Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn dùng (client sinh có sẵn từ Task 452): `listWebCampaigns`, `getWebCampaign`, `createWebCampaign`, `saveWebCampaign`, `listCampaignTargets`, `previewWebCampaign`, `setWebCampaignState`, `deleteWebCampaign`. oasdiff không chênh.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/web-campaigns/` (`route.tsx` tự đăng ký theo `src/app/loadScreens.ts`, nhóm menu "Khuyến mãi" có sẵn, `/khuyen-mai/chien-dich`, trang ẩn `/khuyen-mai/chien-dich/moi`, `/khuyen-mai/chien-dich/:id`). Không sửa `nav.ts` (nhóm "Website" là Task 462). Dùng lại không sửa: `app/idempotency.ts` (`useWriteAction`), `app/errors.ts` (`unwrap`, `errorMessage`, `ApiError`; mã lỗi `percent_too_high`, `preview_required`, `campaign_running`, `version_conflict` đã có câu từ Task 452), `app/queryClient.ts` (`queryKey`, chủ đề realtime `promotion.web_campaign` của máy chủ), `staff/staffData.tsx` (`Dialog`), `don/orderData.ts` (`usePerms`), `ma-giam-gia/couponRules.ts` (`vnLocalToIso`, `isoToVnLocal`), `ma-giam-gia/MoreBar.tsx`, `inventory/Balances` (`useDebounced`), `app/time.ts` (`shortTime`), `app/screenTestkit.tsx`. `app/screens.test.tsx` không liệt kê màn theo tên (chỉ kiểm quy ước) nên không sửa.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest `src/screens/web-campaigns/` (test mới), `src/app/screens.test.tsx`; `npx tsc --noEmit`; `npx eslint` các tệp mới; `make jscpd`; cổng kiểm của task (ci-task). Playwright và tests/e2e không chạy (chủ shop 24/11); kế hoạch Task 458 không có spec Playwright riêng.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ liệt kê mã Go (không áp dụng cho console). Trong console đã tìm: đường ghi chống trùng (`useWriteAction`), hộp thoại (`Dialog` của staffData, dùng ở Task 207), quyền (`usePerms`), giờ VN của ô `datetime-local` (`vnLocalToIso`/`isoToVnLocal` ở couponRules), hàng "Tải thêm" (`MoreBar`), tìm trễ (`useDebounced`), chép vào bộ nhớ tạm (CarrierAccountDetail gọi thẳng `navigator.clipboard`, không có hàm chung), bộ test (`screenTestkit` của Task 207: `stubApi`, `page`, `renderScreens`, `emptyWrites`; `stubDialog` của donTestkit; `grantsOf` của couponTestkit — jscpd báo trùng nên nhập lại), `formatMoney` (products/productRules).
- Dùng lại / viết mới vì: dùng lại mọi thứ ở trên. Viết mới: `campaignRules.ts` (chữ trạng thái, mô tả phạm vi, kiểm form, dựng liên kết website — nghiệp vụ riêng của màn), CSS riêng `web-campaigns.css` chép từ khối `<style>` của mockup (`.cp`, `.picks`, `.res`) và `.toast`, `.unit` (đang nằm trong css riêng của màn khác, gắn tiền tố màn nên không nhập chéo).

## Soát thiết kế

Task chỉ màn hình; mục 5, 6 của quy trình chỉ liên quan ở các câu sau, còn lại không áp dụng (không chạm database, hàng đợi, tiền, tồn — tiền chỉ hiện số máy chủ trả).

- 5.9 Mọi đường ghi có khóa chống trùng: mọi lệnh ghi qua `useWriteAction` (khóa sinh khi bấm, giữ khi tự gửi lại); mã chiến dịch và `revision` nằm trong thân của `run` để khóa đổi theo bản. Mỗi thao tác (tạo, lưu, xem trước, bật/tạm dừng, xóa) một `useWriteAction` riêng.
- 5.12 Theo mẫu: cấu trúc giống `order-requests` (Task 207) và `ma-giam-gia`: route.tsx, *Rules.ts thuần + test, *Testkit.tsx.
- 6.1 Hai người cùng sửa: gửi `revision` đã đọc; 409 `version_conflict` → giữ nguyên form, hiện hộp "Chiến dịch vừa được người khác đổi" và liên kết mở bản mới ở tab khác (mockup D); không tự gửi lại.
- 6.2 Bấm hai lần: `useWriteAction` trả lại lời hứa đang chạy; nút khóa khi đang gửi.
- 6.13 Quyền: nút ghi chỉ hiện với `promotion.manage`; máy chủ vẫn kiểm. Không quyền ghi: form chỉ đọc, chép liên kết vẫn dùng được.
- Bật chiến dịch (luật của máy chủ, Task 452): hộp Kích hoạt tự gọi `previewWebCampaign` của revision hiện tại khi mở, nút Kích hoạt khóa tới khi có bản xem trước và lý do; gửi đúng số `preview` đã thấy. 422 `preview_required` → câu "Danh sách sản phẩm đã đổi…" và nút "Xem trước lại"; 422 `percent_too_high` → câu vượt trần (HV17).
- Màn chỉ vẽ dữ liệu API có (luật Bẫy đã gặp): xem "Quyết định thay chủ shop" cho phần mockup có mà API không có.

## Quyết định thay chủ shop

1. **Liên kết chép:** API trả `path` (`/uu-dai/<tên>-<linkKey>`), không trả tên miền. Chọn ghép `https://elimaz.com.vn` (tên miền website trong spec README) + `path`, khai một hằng `WEBSITE_ORIGIN` trong `campaignRules.ts`. Phương án khác: chỉ chép đường dẫn (khách không mở được), hay đọc tên miền từ cấu hình (chưa có). Dễ đổi: một hằng.
2. **Trần phần trăm:** API không trả trần (cài đặt `promotion.web_campaigns`). Gợi ý ô ghi "1–99; vượt trần của shop thì bị từ chối", câu lỗi `percent_too_high` nêu phần trăm đang nhập, không nêu số trần (mockup ghi "(50%)" — không có nguồn). Ghi Việc phát hiện thêm.
3. **Tên đích khi mở chiến dịch có sẵn:** `getWebCampaign` chỉ trả `targetIds`. Màn đọc `listCampaignTargets` theo trang 100 cho tới khi tìm đủ tên (tối đa 20 trang); không tìm thấy thì hiện "Mục không còn (…8 ký tự cuối mã)". Số mẫu trong mỗi danh mục ("64 mẫu" của mockup) không có trong API → không vẽ. Ghi Việc phát hiện thêm.
4. **Xem trước trong form:** API chỉ xem trước bản đã lưu (`revision`). Nút "Xem trước giá" khi form đã sửa sẽ lưu bản nháp trước rồi xem trước (đúng câu mockup "theo … đang nhập"); chiến dịch mới thì tạo nháp rồi xem trước. Mã SKU dưới tên ở bảng mẫu (mockup) không có trong `WebCampaignSample` → chỉ hiện tên sản phẩm.
5. **Cảnh báo banner khi xóa:** API không cho biết banner nào dùng chiến dịch; hiện câu chung "Banner trang chủ nào đang dùng chiến dịch này sẽ tự ẩn" (đúng câu kế hoạch), không nêu số thứ tự banner.
6. **Phạm vi mặc định của chiến dịch mới:** "Danh mục được chọn" (lựa chọn đầu mockup vẽ), không mặc định "Toàn bộ sản phẩm" để tránh giảm cả shop do quên chọn.
7. **Ô giờ:** dùng ô `datetime-local` giờ Việt Nam như màn Mã giảm giá (cùng hàm đổi giờ), thay ô chữ "05/01/2027 00:00" của mockup.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Bật khi chưa xem trước → nút khóa; xem trước xong + lý do → gửi đúng số đã thấy | `CampaignList.test.tsx` › Kích hoạt › "chưa xem trước xong → nút khóa; xem trước xong + lý do → gửi số đã thấy", "thiếu lý do → nút khóa" | `cd console && npx vitest run src/screens/web-campaigns` | 44 passed (44) |
| Máy chủ giả 422 hiện đúng câu (preview_required, percent_too_high HV17) | Kích hoạt › "422 preview_required → câu phải xem trước lại, nút khóa; Xem trước lại → bật được", "422 percent_too_high → câu vượt trần…"; Sửa chiến dịch › "vượt trần khi lưu → câu dưới ô phần trăm" | như trên | qua |
| 409 giữ form | Sửa chiến dịch › "lưu gửi revision đã đọc và nội dung; 409 version_conflict → giữ nguyên chữ, hộp báo, liên kết mở bản mới" (lưu lại gửi revision mới đọc); Danh sách › "409 version_conflict trong hộp: hiện câu, tải lại danh sách" | như trên | qua |
| Tên 120 ký tự không tràn ở 390 | Danh sách › "tên 120 ký tự: cắt hai dòng (clamp2), tên đủ trong title" (jsdom không đo bố cục: kiểm lớp `clamp2` + `overflow-wrap:anywhere` của styles.css; so ảnh 390 để tới khi chạy Playwright) | như trên | qua |
| Không `promotion.manage` → không nút ghi | Danh sách › "không có promotion.manage: không nút Tạo, Sửa, Kích hoạt, Tạm dừng, Xóa; vẫn chép liên kết"; Sửa › "không có promotion.manage: form chỉ đọc…" | như trên | qua |
| Liên kết chép đúng dạng | Danh sách › "chép liên kết: đúng https://elimaz.com.vn/uu-dai/<tên>-<khóa>"; `campaignRules.test.ts` › "liên kết chép" | như trên | qua |
| Lọc trạng thái, tìm, trang 10/20/50 (Tải thêm) | Danh sách › "tìm, lọc trạng thái, cỡ trang gửi q, status, limit; mặc định 20", "cỡ trang 10: còn trang sau thì có Tải thêm" | như trên | qua |
| Form: tên, %, phạm vi + chọn đích ≤ 30, giờ VN, allowCoupon, clearance | Tạo chiến dịch › "chọn 2 danh mục, dùng kèm mã → gửi đúng thân (giờ VN → UTC)…", "đủ 30 đích: ô chưa chọn bị khóa…", "đổi phạm vi sang sản phẩm…", "ô sai báo dưới ô"; `campaignRules.test.ts` › form | như trên | qua |
| Xem trước giá | Sửa › "Xem trước giá khi form đã sửa: lưu trước rồi xem trước…", "Xem trước giá khi chưa sửa…" | như trên | qua |
| Tạm dừng / sửa khi chạy / xóa có lý do và cảnh báo banner | Danh sách › "tạm dừng: lý do bắt buộc…", "Sửa khi đang chạy: hộp Tạm dừng và sửa…", "xóa: cảnh báo banner…", "xóa … 409 campaign_running"; Sửa › "chiến dịch đang chạy mở thẳng…" | như trên | qua |
| Màn đăng ký đúng nhóm "Khuyến mãi" | `route.test.tsx` | như trên | qua |
| Cổng kiểm của task | toàn bộ console (vitest, tsc, eslint, vite build, gitleaks), jscpd, test kiến trúc | ci-task (log ngoài worktree) | `CI-TASK XANH`; vitest `Tests 1142 passed (1142)`; jscpd `Found 0 clones` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 458 không đòi spec Playwright; so ảnh với mockup để tới khi chủ shop cho chạy Playwright)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ngoài ba tệp kế hoạch ghi (`CampaignList.tsx`, `CampaignEditor.tsx`, `route.tsx`) có thêm `CampaignDialogs.tsx` (hộp thoại), `campaignRules.ts`, `campaignData.ts`, `web-campaigns.css` và test — cùng cách chia của màn Yêu cầu về đơn (Task 207).
- Sau 409 ở form: ngoài giữ chữ đang gõ (kế hoạch), màn đọc bản mới làm gốc để lần Lưu sau đi được (không thì mọi lần lưu lại đều 409).

## Việc phát hiện thêm

- (Máy chủ, promotion) `getWebCampaign`/`WebCampaign` nên trả tên, mã của đích (`targets: [{id, code, name}]`) và số mẫu mỗi danh mục; màn đang phải đọc trang `listCampaignTargets` để lấy tên.
- (Máy chủ, promotion) Chưa có API đọc trần phần trăm (`promotion.web_campaigns`) cho màn ghi "trần của shop 50%" (mockup, HV17) và chưa có màn để Chủ nâng trần.
- (Máy chủ, promotion) `WebCampaign` nên trả liên kết đầy đủ (tên miền website theo cấu hình) thay cho `path`.
- (Máy chủ, content) Xóa chiến dịch: API chưa cho biết banner nào đang trỏ chiến dịch (mockup nêu "Banner trang chủ số 3").
- (Máy chủ, promotion) `WebCampaignSample` chưa có SKU biến thể (mockup hiện mã dưới tên).

## Reviewer

Kết luận: ĐẠT (agent reviewer, 24/11, trên `git diff --cached` 13 tệp). Phát hiện phải sửa: không có. Đã đối chiếu luật bật chiến dịch của máy chủ (`checkEnable`) với hộp Kích hoạt, khóa chống trùng, chủ đề realtime, không `as`/`any`/fetch thẳng.
Ghi chú không bắt buộc (không sửa, ghi để chủ shop biết):
- Cờ `previewNow` nằm trong history state: tải lại trang sửa vừa tạo bằng "Xem trước giá" sẽ gọi xem trước thêm một lần (vô hại, không tăng revision).
- Gặp 409 trong hộp thoại ở danh sách: danh sách tải lại nhưng hộp giữ revision cũ; đóng rồi mở lại hộp từ dòng mới là đi được.
- Sau 409 ở form, lần Lưu tiếp ghi đè bản của người kia (form đã lấy bản mới làm gốc), đúng câu mockup D — chủ shop lưu ý khi xem lại.
