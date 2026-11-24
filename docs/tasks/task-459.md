# Task 459 — Màn Banner trang chủ

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 459, M5-47). Loại việc (quy trình mục 3): A (chỉ màn hình)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa `console/`, không chạm mã Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn dùng client sinh có sẵn: `getBanners`, `previewBanners`, `saveBanners` (Task 453); `createUpload`, `completeUpload`, `getFile` (kho tệp M1, qua `useUploads`); `listProducts` (tìm mẫu để ghim và làm đích, quyền `catalog.read`). oasdiff không chênh.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/web-banners/` (`route.tsx` tự đăng ký theo `src/app/loadScreens.ts`, nhóm menu "Website" có sẵn từ Task 208, `/website/banner`, quyền `content.read`). Không sửa `nav.ts`. Sửa nhỏ mã dùng chung của màn Sản phẩm: `products/useUpload.ts` thêm tùy chọn `purpose` (mặc định `product_image`) và đổi chữ ký `Uploads.add` từ trả `void` sang trả `string[]` (mã dòng của từng tệp; chỗ gọi hiện có `ProductEdit.tsx`, `ImageManager.tsx` bỏ qua giá trị trả về, bản giả `fakeUploads` trong `ImageManager.test.tsx` dùng `vi.fn()` vẫn hợp kiểu — không đổi hành vi), `products/imageRules.ts` thêm câu cho lý do từ chối `too_small` (chỉ mục đích có `MinWidth` như `web_banner` gặp). Dùng lại không sửa: `app/idempotency.ts` (`useWriteAction`), `app/errors.ts` (mã lỗi banner đã có câu từ Task 453), `app/queryClient.ts`, `staff/staffData.tsx` (`Dialog`), `don/orderData.ts` (`usePerms`), `ma-giam-gia/couponRules.ts` (`vnLocalToIso`, `isoToVnLocal`), `web-campaigns/campaignRules.ts` (`runeCount`), `inventory` (`useDebounced`), `app/screenTestkit.tsx`.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest `src/screens/web-banners/` (test mới), `src/screens/products/` (useUpload, imageRules, ImageManager — vì sửa `useUpload.ts`, `imageRules.ts`), `src/app/screens.test.tsx`; `npx tsc --noEmit`; `npx eslint` các tệp sửa; `make jscpd`; cổng kiểm của task (ci-task). Playwright và tests/e2e không chạy (chủ shop 24/11); kế hoạch Task 459 không có spec Playwright riêng.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ liệt kê mã Go (không áp dụng cho console). Trong console đã tìm: tải ảnh lên kho tệp (`products/useUpload.ts` `useUploads`: createUpload → PUT → completeUpload → chờ xử lý, cùng khóa chống trùng), câu lý do từ chối ảnh (`products/imageRules.ts` `rejectText`), đường ghi (`useWriteAction`), hộp thoại (`Dialog`), quyền (`usePerms`), giờ VN của ô `datetime-local` (`couponRules`), đếm ký tự (`runeCount`), tìm trễ (`useDebounced`), dời phần tử (`imageRules.moveTo`), bộ test (`screenTestkit`, `stubDialog`, `grantsOf`).
- Dùng lại / viết mới vì: dùng lại mọi thứ ở trên. `useUploads` chỉ khác mục đích tệp: thêm tùy chọn `purpose` thay vì chép hook (một cách tải ảnh, CLAUDE.md "dùng lại trước khi viết mới"). Viết mới: `bannerRules.ts` (chữ loại, lý do ẩn, cảnh báo tỉ lệ, nhân bản, dựng thân `HomeBanner` theo loại, lý do khóa Lưu — nghiệp vụ riêng của màn), `bannerData.ts` (truy vấn, xem trước trễ 350 ms), CSS riêng `web-banners.css` chép từ `m5.css` của mockup (khung xem trước, danh sách sắp xếp).

## Soát thiết kế

Task chỉ màn hình; mục 5, 6 của quy trình chỉ liên quan ở các câu sau, còn lại không áp dụng (không chạm database, hàng đợi, tiền, tồn).

- 5.9 Đường ghi có khóa chống trùng: `saveBanners` qua `useWriteAction` (thân gồm `revision` và cả danh sách, khóa đổi theo bản); tải ảnh qua `useUploads` (khóa riêng mỗi tệp). `previewBanners` miễn khóa (máy chủ khai exempt), gọi bằng `unwrap` trực tiếp, chỉ đọc.
- 5.12 Theo mẫu: cấu trúc giống `web-campaigns` (Task 458): route.tsx, *Rules.ts thuần + test, *Data.ts, *Testkit.tsx.
- 6.1 Hai người cùng sửa: gửi `revision` đã đọc; 409 `version_conflict` → hộp "Banner vừa được người khác lưu" (HV19) với "Giữ bản đang sửa" (đóng hộp, giữ bản sửa; lần Lưu sau vẫn báo 409 vì revision cũ) và "Tải bản mới" (bỏ bản sửa, nạp bản máy chủ). Báo hiệu realtime `content.banners` tải lại dữ liệu nhưng không ghi đè bản đang sửa (chỉ nạp khi chưa sửa gì).
- 6.2 Bấm hai lần: `useWriteAction` trả lại lời hứa đang chạy; khi đang lưu khóa mọi thao tác (thêm, chọn loại, bật/tắt, lên/xuống, kéo thả, nhân bản, xóa, sửa ô, tải ảnh, bỏ thay đổi).
- 6.13 Quyền: thấy màn cần `content.read`; sửa cần `content.manage`; tải ảnh cần thêm `files.upload`; tìm mẫu để ghim cần `catalog.read` (owner, manager có đủ theo `docs/quyen.md`). Thiếu quyền ghi: mọi ô và nút sửa khóa, câu "Bạn chỉ có quyền xem…". Máy chủ vẫn kiểm.
- Ảnh (HV18): ảnh đang tải, đang xử lý → nút Lưu khóa kèm lý do; ảnh bị từ chối → lý do và khóa Lưu; máy chủ vẫn trả 422 `file_rejected` nếu lọt.
- Màn chỉ vẽ dữ liệu API có (luật Bẫy đã gặp): xem "Quyết định thay chủ shop".

## Quyết định thay chủ shop

1. **"Theo thời gian chiến dịch":** làm như admin cũ (`apps/admin-*/src/components/Banners.tsx`): chép giờ bắt đầu, kết thúc của chiến dịch vào hai ô giờ; thêm nút "Luôn hiện" (xóa giờ) như admin. Banner ảnh trỏ tới chiến dịch cũng có nút này. Phương án khác: để trống giờ (chiến dịch ngừng thì banner ẩn với lý do `campaign-not-running`). Dễ đổi: một nút.
2. **Hộp 409 (HV19):** API không trả tên người lưu (chỉ `updatedBy` là mã), nên hộp ghi "Người khác vừa lưu danh sách banner", không có tên, giờ như mockup "Trần Minh Thư lưu lúc 11:42". "Giữ bản đang sửa" giữ revision cũ, nên lần Lưu sau vẫn báo 409 (không bao giờ ghi đè âm thầm); "Tải bản mới" bỏ bản sửa, nạp bản máy chủ.
3. **Chọn mẫu để ghim / làm đích "Một sản phẩm":** dùng `listProducts` (quyền `catalog.read`, owner và manager có) lọc `status=active`, banner danh mục gửi `categoryId` (gồm con); banner giảm giá lọc trên màn theo phạm vi chiến dịch (sản phẩm chọn, danh mục chọn và con cháu). Mẫu chưa "hiện được" (thiếu ảnh, giá) vẫn có thể hiện trong kết quả: máy chủ trả 422 `pin_not_in_source`, màn hiện câu tiếng Việt. Thiếu `catalog.read` → câu "Cần quyền xem sản phẩm để tìm mẫu".
4. **Kích thước ảnh, cảnh báo lệch tỉ lệ:** `HomeBannerImage` không có rộng/cao; màn đọc `getFile` (có `width`, `height`, quyền `files.upload`) khi ảnh sẵn sàng. Không có `files.upload` → không hiện kích thước, không cảnh báo.
5. **Tiến độ tải (mockup "Đang tải ảnh… 60%"):** `useUploads` không báo phần trăm; màn hiện "Đang tải ảnh…" kèm tên tệp, không có thanh phần trăm.
6. **Xem trước:** chữ trống hiện "(chữ tự động)"/"Tiêu đề tự động" (website tự điền, CRM không biết chữ đó); sản phẩm và giá không vẽ (website tự lấy, Task 464). Ảnh mới tải hiện trong khung xem trước sau khi lưu (getBanners mới trả đường ảnh); trước đó khung ghi "Chưa có ảnh đã xử lý".
7. **Kiểm tệp trước khi tải:** chỉ JPEG/PNG/WebP ≤ 8 MB (luật mục đích `web_banner`) — báo ngay, không gọi `createUpload`. Rộng < 600 px chỉ biết sau khi kho xử lý (lý do `too_small`, thêm câu vào `imageRules.ts`).
8. **Ảnh đã lưu bị mất/xóa/từ chối:** khóa nút Lưu kèm lý do (máy chủ cũng trả 422 `banner_image_invalid`/`file_rejected`), mời tải ảnh khác hoặc bỏ ảnh.
9. **Xóa banner không hỏi xác nhận:** chỉ xóa khỏi bản đang sửa; "Bỏ thay đổi" lấy lại (mockup không vẽ hộp xác nhận).
10. **Báo đã lưu:** sau khi lưu hiện "Đã lưu danh sách banner." (mockup không vẽ; giống màn Chiến dịch).

## Bằng chứng

Lệnh chung (thư mục `console/`): `npx vitest run src/screens/web-banners src/screens/products src/app` → `Tests 270 passed (270)`; `npx tsc --noEmit` → không lỗi; `npx eslint src/screens/web-banners src/screens/products` → 0 lỗi; `make jscpd` → `Found 0 clones`; `scripts/agent/ci-task.sh` → `CI-TASK XANH (so với điểm tách nhánh 1472f08…)` (console-check 64 s, test kiến trúc `ok elimaz.com.vn/crm/tests/arch`).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 8 banner → nút thêm, ô loại, Nhân bản khóa, câu "Đã đủ 8" | `BannerEditor.test.tsx` "8 banner → ô loại và nút thêm khóa, câu Đã đủ 8" | `npx vitest run src/screens/web-banners` | `Tests 51 passed (51)` |
| Ảnh đang xử lý → Lưu khóa kèm lý do (ảnh đã lưu và ảnh vừa tải) | "ảnh điện thoại đã lưu đang xử lý → Lưu khóa kèm lý do", "tải ảnh: đang xử lý → Lưu khóa; xong → Sẵn sàng, cảnh báo lệch tỉ lệ, Lưu gửi mã tệp mới"; `bannerRules.test.ts` "lý do khóa nút Lưu" (4 ca) | như trên | qua |
| Ảnh bị từ chối hiện lý do; tệp > 8 MB báo ngay | "ảnh bị từ chối vì quá nhỏ…", "tệp quá 8 MB hay không phải ảnh…"; `imageRules.test.ts` ca `too_small` | `npx vitest run src/screens/products` | qua |
| Cảnh báo lệch tỉ lệ > 2 % | `bannerRules.test.ts` "tỉ lệ ảnh" (3 ca), ca tải ảnh 1920 × 600 → "khoảng 19%" | như trên | qua |
| Mỗi lý do ẩn đúng chữ (9) | `bannerRules.test.ts` "đủ 9 lý do…"; `BannerEditor.test.tsx` "lý do ẩn %s: dòng danh sách và khung xem trước đúng chữ" (9 ca) | như trên | qua |
| 409 → hộp tải lại, không mất bản đang sửa | "409 → hộp người khác vừa lưu; Giữ bản đang sửa giữ bản; Tải bản mới nạp bản máy chủ" | như trên | qua |
| Đang lưu khóa mọi thao tác, kể cả kéo thả | "đang lưu: khóa bật/tắt, lên/xuống, thêm, sửa, bỏ thay đổi, kéo thả" (thả lọt vào vẫn không đổi thứ tự); "kéo thả đổi thứ tự khi không lưu" | như trên | qua |
| Thêm, bật/tắt, lên/xuống, nhân bản (bản sao tắt), xóa; lưu gửi revision, khóa chống trùng | "thêm Xu hướng, bật/tắt, lên, nhân bản…"; `bannerRules.test.ts` "thêm, đổi loại, nhân bản, dời" | như trên | qua |
| Bỏ thay đổi; báo hiệu khi đang sửa không ghi đè bản sửa, Bỏ thay đổi nạp bản mới nhất | "Bỏ thay đổi trả về bản đã lưu", "người khác lưu trong lúc đang sửa: giữ bản sửa; Bỏ thay đổi nạp bản mới nhất của máy chủ" (reviewer lần 1), "sửa rồi hoàn tác tay sau khi người khác lưu → màn nạp bản mới nhất, không vẽ bản cũ" (reviewer lần 2), "bản mới từ máy chủ bỏ mất banner đang chọn, hay danh sách đầu rỗng → form hiện banner đầu của bản mới" (reviewer lần 3) | như trên | qua |
| Ghim ≤ 3 thuộc nguồn; Theo thời gian chiến dịch; bộ đếm chữ | "ghim tối đa 3 mẫu thuộc chiến dịch…", "banner danh mục: tìm mẫu lọc theo danh mục của banner"; `ProductPicker.test.ts` "campaignFilter" (3 ca: theo sản phẩm, theo danh mục và con cháu có mẫu bị loại, toàn shop) | như trên | qua |
| Xem trước sau 350 ms với bản đang sửa; máy tính/điện thoại | "sửa xong 350 ms → previewBanners…" | như trên | qua |
| Chỉ xem (thiếu content.manage) | "chỉ content.read → câu chỉ xem, mọi ô và nút sửa khóa" | như trên | qua |
| Lỗi 422 hiện câu tiếng Việt | "lỗi 422 của máy chủ hiện câu tiếng Việt, giữ bản đang sửa" | như trên | qua |
| Thân gửi đúng loại | `bannerRules.test.ts` "thân gửi máy chủ" (3 ca) | như trên | qua |
| `useUploads` mục đích `web_banner`, `add` trả mã dòng | `useUpload.test.ts` "tùy chọn purpose (ảnh banner, Task 459)…" | `npx vitest run src/screens/products` | qua |
| Đăng ký route | `route.test.tsx` | như trên | qua |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 459 không có spec Playwright)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `HomeBannerList` không trả tên người lưu cuối (`updatedBy` chỉ là mã) → hộp 409 không nêu được tên như mockup HV19. Cần API trả tên (Task sau của content).
- `HomeBannerImage` không trả rộng/cao, lý do từ chối → màn phải gọi `getFile` (cần `files.upload`); ảnh đã lưu bị từ chối chỉ hiện câu chung. Nên thêm `width`, `height`, `rejectReason` vào `HomeBannerImage`.
- Ảnh đang xử lý đã lưu: `content.banners` chỉ báo hiệu khi lưu, nên trạng thái "đang xử lý" của ảnh đã lưu không tự đổi thành sẵn sàng tới khi tải lại màn.

## Reviewer

Lần 1: CẦN SỬA — "Bỏ thay đổi" nạp `base` (gốc cũ) nên sau báo hiệu của người khác màn vẽ bản cũ như bản đã lưu. Đã sửa: nạp `server` (bản mới nhất đã đọc), thêm test "người khác lưu trong lúc đang sửa…". Ghi chú không bắt buộc đã làm: test `campaignFilter` (`ProductPicker.test.ts`), xóa ảnh đang tải của banner khi đổi loại, ghi đổi chữ ký `Uploads.add` vào mục Ảnh hưởng. Chạy lại ci-task: XANH.

Lần 2: CẦN SỬA — sửa rồi hoàn tác tay về đúng gốc cũ sau khi có báo hiệu bản mới thì màn vẫn vẽ bản cũ (đồng bộ chỉ chạy lúc `server` đổi). Đã sửa: mỗi lần vẽ, không có thay đổi chưa lưu và gốc khác bản mới nhất đã đọc thì nạp bản mới nhất (bỏ biến `seen`); thêm test "sửa rồi hoàn tác tay…". Ghi chú không bắt buộc đã làm: bỏ `moveTo` viết lại trong `bannerRules.ts`, dùng `imageRules.moveTo` (khớp mục Tái dùng).

Lần 3: CẦN SỬA — nạp bản mới từ máy chủ không chọn lại banner đang sửa: bản mới không còn banner đã chọn (hay màn mở lúc rỗng rồi có banner) thì form báo "Chưa có banner" sai. Đã sửa: banner đang sửa suy ra mỗi lần vẽ (đã chọn còn thì giữ, không thì banner đầu), bỏ bước chọn lại trong `resetTo`; thêm test "bản mới từ máy chủ bỏ mất banner đang chọn…". Không xóa ảnh đang tải khi đồng bộ (theo ghi chú reviewer).

Lần 4: ĐẠT — không còn phát hiện phải sửa; ba lỗi đồng bộ với máy chủ (lần 1–3) đã sửa, mỗi lỗi một test làm hỏng được nếu bỏ phần sửa; Ảnh hưởng khớp (chỗ gọi `Uploads.add` không hỏng); 51 test của màn khớp bảng bằng chứng; không `.skip`/`.only`, không ép kiểu dữ liệu API. Ghi chú không bắt buộc: màn nhập mã từ thư mục màn khác thay vì `console/src/shared/` (như Task 458) — không phải lỗi tính đúng.

Kết luận: ĐẠT
