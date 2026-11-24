# Task 461 — Màn Góp ý và khối góp ý trong hồ sơ khách

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 461, M5-49); mockup `docs/mockups/m5/feedback.html` (HV01, HV22). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ màn hình, không đổi mã Go).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng; màn dùng `listFeedback` (lọc `state`, `topic`, `customerId`, con trỏ) và `updateFeedback` (Task 455) có sẵn trong `console/src/api`.
- Chỗ dùng trên màn hình: tạo `console/src/screens/feedback/` (`route.tsx` màn `/khach/gop-y` nhóm "Khách hàng", quyền `customer.feedback.read`; `FeedbackList.tsx`, `FeedbackPanel.tsx` dòng mở; `feedbackRules.ts` chữ của chủ đề, mức, trạng thái; `feedbackData.ts` khóa truy vấn; `CustomerFeedback.tsx` khối trong hồ sơ; testkit, test); sửa `screens/khach/CustomerProfile.tsx` (thêm khối ở cột phải khi có quyền), `screens/khach/khachTestkit.tsx` (API giả `GET /api/v1/feedback`); sửa `app/nav.ts` `isCurrent` (+ `shell/Layout.tsx` truyền danh sách mục): `/khach/gop-y` khớp `also: /khach/:id` của mục "Khách hàng", không sửa thì hai mục cùng sáng. `isCurrent` chỉ dùng ở `Layout.tsx` (grep).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest `src/screens/feedback/`, `src/screens/khach/`, `src/shell/Layout.test.tsx`, `src/app/screens.test.tsx`; `npx tsc --noEmit`; `npx eslint` các tệp sửa; ci-task (cổng tĩnh + console). Không Playwright (lệnh chủ shop 24/11; kế hoạch task không có spec Playwright).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: danh mục Go không liên quan (màn hình). Tìm trong console: danh sách có thanh nhãn lọc + "Tải thêm" (`order-requests/RequestList.tsx`, `ma-giam-gia/MoreBar.tsx`), ô chữ đếm (`web-banners/listEditor.tsx` `Counted`), đường ghi (`app/idempotency.ts` `useWriteAction`), lỗi (`app/errors.ts`), quyền (`don/orderData.ts` `usePerms`), giờ (`app/time.ts` `shortTime`), API giả (`app/screenTestkit.tsx` `stubApi`, `page`, `renderScreens`).
- Dùng lại / viết mới vì: dùng lại `MoreBar`, `Counted`, `useWriteAction`, `unwrap`/`errorMessage`/`ApiError`, `usePerms`, `shortTime`, `stubApi`/`page`/`renderScreens`, lớp CSS có sẵn (`lblbar`, `tbl`, `chip`, `clamp2`, `wrap-any`, `tl`, `box-err`). Viết mới: bảng chữ của chủ đề, mức, trạng thái góp ý (chỉ màn này dùng); dòng mở có ghi chú (hộp 409 ở đây là hộp báo tại chỗ, không phải `ConflictDialog` "giữ/tải bản mới" của danh sách banner: góp ý tải lại ngay và giữ ghi chú trong ô theo mockup C).

## Soát thiết kế

Task chỉ màn hình; mục 5, 6 của quy trình chỉ liên quan ở các câu sau, còn lại không áp dụng (không chạm database, hàng đợi, tiền, tồn, gọi ra ngoài).

- 5.9 Đường ghi có khóa chống trùng: `updateFeedback` qua `useWriteAction` (khóa `Idempotency-Key` sinh lúc bấm, gửi lại cùng khóa khi mạng lỗi). Thân chỉ có `version` và đúng trường đổi (nút trạng thái gửi `state`; "Lưu ghi chú" gửi `note`; Bẫy Task 85).
- 5.12 Theo mẫu: cấu trúc như `order-requests` (route.tsx, rules thuần + test, data, testkit).
- 6.1 Hai người cùng sửa: gửi `version` đã đọc; 409 `version_conflict` → tải lại danh sách, hộp báo "Góp ý vừa được người khác đổi" kèm trạng thái mới, người sửa, giờ (từ bản vừa tải); ghi chú đang gõ giữ trong ô. Lần lưu sau dùng `version` mới.
- 6.2 Bấm hai lần: `useWriteAction` trả lại lời hứa đang chạy; khi đang lưu khóa mọi nút của dòng và ô ghi chú (Bẫy Task 48, 325).
- 6.13 Quyền: thấy màn và khối trong hồ sơ cần `customer.feedback.read`; nút đổi trạng thái, ô ghi chú cần `customer.feedback.manage` (không có thì ghi chú chỉ đọc); liên kết sang hồ sơ khách cần `customer.read`. API trả 403 → câu "Bạn không có quyền xem góp ý của khách" (không vẽ như rỗng). Máy chủ vẫn kiểm.
- Dữ liệu cá nhân: nội dung, SĐT, ghi chú chỉ nằm trong bộ nhớ của React Query; màn không gọi `console.*`, không ghi `localStorage`/`sessionStorage`, không đưa nội dung vào URL (bộ lọc khách dùng mã khách). Nội dung hiện bằng nút chữ của React (không `dangerouslySetInnerHTML`), `<script>` hiện nguyên chữ.
- Phân trang: còn `nextCursor` thì không ghi "đã hết", số trong khối hồ sơ ghi "n gần nhất" (Bẫy Task 84).

## Quyết định thay chủ shop

1. **Đường `/khach/gop-y`, nhóm "Khách hàng"** (theo mockup README, HV01). Đường này khớp `also: /khach/:id` của mục "Khách hàng" nên menu sẽ sáng hai mục: sửa `isCurrent` để mục có path khớp đúng đường thắng các mục chỉ khớp qua `also`. Phương án khác: đường ngoài `/khach` (lệch mockup).
2. **Thanh lọc trạng thái không có số đếm:** `listFeedback` không trả số theo trạng thái (mockup vẽ "Mới 12"). Chỉ vẽ dữ liệu API có. Thêm sau khi API có số.
3. **Liên kết hồ sơ ghi tên khách** ("Hồ sơ Trần Thị Thu Hà"): API trả `customerId`, `customerName`, không có mã KH. Dòng không gắn ghi "Không gắn hồ sơ" (API không nói lý do, mockup ghi lý do).
4. **Mã đơn hiện chữ thuần**, không liên kết: `orderRef` là chữ website gửi, không có mã đơn CRM.
5. **Nút trạng thái:** dòng mở hiện nút cho hai trạng thái khác trạng thái hiện tại ("Đánh dấu chưa xem", "Đánh dấu đã xem", "Đã phản hồi khách") để bấm nhầm thì sửa lại được (API cho mọi chuyển). Mockup chỉ vẽ dòng "Mới". Mở dòng không tự đánh dấu đã xem.
6. **Một dòng mở một lúc**; "Mở"/"Đóng" ở cột cuối như mockup.
7. **409:** hộp báo tại chỗ trong dòng mở (mockup C), giữ ghi chú đang gõ, nút dùng version mới; không tự gửi lại.
8. **Khối hồ sơ:** đọc 5 góp ý mới nhất của nhóm khách (`customerId` = gốc); không còn trang sau → "Góp ý (n)", còn → "Góp ý · 5 gần nhất". Nút "Xem ở Góp ý" mở `/khach/gop-y?khach=<mã khách>` (lọc theo khách, có nút bỏ lọc). Chưa có góp ý → một dòng "Khách chưa gửi góp ý nào". Không có quyền → không có khối (mockup C).
9. **Mức hài lòng bằng chữ** (HV22): 1 Rất không hài lòng, 2 Không hài lòng, 3 Bình thường, 4 Hài lòng, 5 Rất hài lòng; không có → "—". Chữ "Bình thường" mockup không vẽ, chọn theo thang 5 mức.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Nội dung có `<script>` hiện nguyên chữ, không thành phần tử | `FeedbackList.test.tsx` "mỗi dòng: giờ, chủ đề, mức bằng chữ, nội dung thuần…" (`document.querySelector("script")` null); `ProfileFeedback.test.tsx` ca đầu (`<b>x</b>` hiện chữ) | `npx vitest run src/screens/feedback src/screens/khach src/shell` | `Tests  106 passed (106)` |
| Còn `nextCursor` thì không hiện "hết" | "120 góp ý: trang 50, còn trang sau thì không ghi hết…" ("50 góp ý, còn nữa" → "100…" → "120 góp ý", 3 con trỏ khác nhau); hồ sơ: "còn trang sau: không ghi tổng, ghi 5 gần nhất" | như trên | qua |
| 409 → tải lại, giữ ghi chú, lần sau dùng version mới | "409: tải lại, báo trạng thái mới, người sửa, giờ; giữ ghi chú đang gõ…" (hộp có `"Đã phản hồi" (Lê Hồng Nhung, 10:30 21/11)`, lần 2 gửi `{version: 2, note}`, khóa mới) | như trên | qua |
| Không quyền → 403 có thông báo | "API trả 403: câu không có quyền, không vẽ như rỗng"; "sửa bị 403: câu không có quyền"; hồ sơ: "API lỗi 403: câu không có quyền trong khối, hồ sơ vẫn mở", "không có quyền xem góp ý: không có khối, không gọi API"; "chỉ có quyền xem: ghi chú hiện chữ, không có nút, không có ô"; "không có quyền customer.read: tên khách gắn hiện chữ, không liên kết" | như trên | qua |
| PATCH chỉ gửi trường đổi, có Idempotency-Key | "đổi trạng thái chỉ gửi version và state…" (`{version:1,state:"answered"}`); "lưu ghi chú chỉ gửi version và note; xóa hết là note rỗng…" | như trên | qua |
| Đang lưu khóa thao tác | "đang lưu: khóa ô ghi chú và mọi nút của dòng" | như trên | qua |
| Lọc trạng thái, chủ đề, khách | "lọc trạng thái bằng thanh nhãn, chủ đề bằng ô chọn…"; "?khach= lọc theo khách (customerId), có nút bỏ lọc" | như trên | qua |
| Khối hồ sơ: góp ý của nhóm khách, liên kết sang màn | `ProfileFeedback.test.tsx` "góp ý của nhóm khách…" (`customerId=C1`, `limit=5`, href `/khach/gop-y?khach=C1`), "chưa có góp ý: một dòng", "góp ý không có mức: chỉ chủ đề" | như trên | qua |
| Nội dung, SĐT không vào log trình duyệt, localStorage | "không ghi nội dung, liên lạc vào log trình duyệt hay localStorage" (spy `console.*`, `Storage.prototype.setItem` không được gọi) | như trên | qua |
| Menu: chỉ mục Góp ý sáng ở `/khach/gop-y` | `Layout.test.tsx` "mục có path khớp đúng đường thắng mục chỉ khớp qua also…"; đăng ký màn `feedback/route.test.tsx`; quy ước `app/screens.test.tsx` | `npx vitest run src/screens/feedback src/screens/khach src/shell src/app` | qua |
| Chữ mức, nút trạng thái | `feedbackRules.test.ts` | như trên | qua |
| Cổng kiểm của task | toàn bộ cổng tĩnh + console (tsc, eslint, prettier, vitest, vite build) + test kiến trúc | ci-task của task (một lần) | `Tests  1354 passed (1354)`, `Found 0 clones.`, `CI-TASK XANH (so với điểm tách nhánh 8ae2543…)` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 461 không có spec Playwright; đủ ca nằm ở test vitest ở trên.)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `console/src/screens/feedback/{FeedbackPanel.tsx,CustomerFeedback.tsx,feedbackRules.ts,feedbackData.ts,feedback.css}` ngoài hai tệp kế hoạch ghi (tách cho gọn, cùng thư mục).
- Sửa `app/nav.ts` `isCurrent` + `shell/Layout.tsx` (Quyết định 1) và thêm `disabled` cho `Counted` (`web-banners/listEditor.tsx`) để khóa ô ghi chú khi đang lưu.
- Khối hồ sơ hiện ở cuối cột phải (sau Thành viên nhóm), như mockup B "thêm ở cột phải".

## Việc phát hiện thêm

- Đang lọc một trạng thái (ví dụ "Mới") mà gặp 409 vì người khác đã đổi góp ý sang trạng thái khác: lần tải lại bỏ dòng khỏi danh sách đang lọc, dòng mở (cùng hộp 409 và ghi chú đang gõ) biến mất. "Giữ ghi chú đang gõ" (Quyết định 7) chỉ đúng khi lọc "Tất cả" hoặc trạng thái không đổi. Sửa sau nếu chủ shop cần: giữ bản dòng mở ngoài danh sách. (Reviewer 24/11.)
- Trong lúc tải lại sau 409 các nút đã mở lại; bấm lúc đó gửi version cũ và nhận 409 lần nữa (không sai dữ liệu).

## Reviewer

Kết luận: **ĐẠT** (agent `reviewer`, 24/11, trên `git diff --cached` đã gộp main 8ae2543). Không có phát hiện phải sửa. Đã đối chiếu: mục Ảnh hưởng khớp thực tế (`isCurrent` chỉ một chỗ gọi, đã truyền `menuItems`; `Counted` thêm prop tùy chọn; `customerKey` tách khóa khối hồ sơ khỏi InfiniteData), mọi dòng bảng bằng chứng có test kiểm đúng điều đó, đủ 4 test Step 1 của kế hoạch, không `as` trên dữ liệu API, không `fetch` thẳng. Ghi chú không bắt buộc: hai điểm trải nghiệm ở "Việc phát hiện thêm"; màn nhập `Counted`, `MoreBar`, `usePerms` từ thư mục màn khác (như các màn trước).
