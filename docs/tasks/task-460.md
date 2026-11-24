# Task 460 — Màn Bộ phối

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 460, M5-48). Loại việc (quy trình mục 3): A (chỉ màn hình)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ sửa `console/`, không chạm mã Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn dùng client sinh có sẵn: `getLooks`, `previewLooks`, `saveLooks` (Task 454, quyền `content.read` / `content.manage`); `listProducts` (tìm mẫu, `catalog.read`), `getProduct` (màu và ảnh của món vừa chọn, `catalog.read`). oasdiff không chênh.
- Chỗ dùng trên màn hình: thư mục mới `console/src/screens/web-looks/` (`route.tsx` tự đăng ký theo `src/app/loadScreens.ts`, nhóm menu "Website" có sẵn, `/website/bo-phoi`, quyền `content.read`, `order: 20` — sau Banner trang chủ 10, trước Hãng trên web 30, đúng thứ tự HV01). Không sửa `nav.ts`. Sửa cấu trúc màn banner (commit riêng, không đổi hành vi): tách thanh lưu `SaveBar`, hộp 409 `ConflictDialog`, `useReload`, ô chữ đếm `Counted`, `Step`, ô giờ `VnTimeInput` từ `BannerEditor.tsx`, `BannerForm.tsx` sang tệp mới `web-banners/listEditor.tsx` để màn bộ phối dùng lại (jscpd báo 8 đoạn trùng; chữ và hành vi màn banner giữ nguyên, 51 test banner vẫn qua). Dùng lại không sửa: `web-banners/ProductPicker.tsx` (ô tìm mẫu), `web-banners/bannerData.ts` (`headers`, `usePerms`, `PREVIEW_DELAY_MS`), `web-banners/bannerRules.ts` (`moveBanner`, dời lên/xuống), `web-banners/bannerTestkit.tsx` (`grantsOf`), `app/idempotency.ts` (`useWriteAction`), `app/errors.ts` (mã lỗi bộ phối đã có câu từ Task 454), `app/queryClient.ts`, `staff/staffData.tsx` (`Dialog`), `web-campaigns/campaignRules.ts` (`WEBSITE_ORIGIN`), `inventory/Balances.tsx` (`useDebounced`), qua `listEditor.tsx` gián tiếp `couponRules` (giờ VN) và `runeCount`, `app/screenTestkit.tsx`, `don/donTestkit` (`stubDialog`).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest `src/screens/web-looks/` (test mới), `src/screens/web-banners/` (sửa cấu trúc; dùng chung ProductPicker, bannerData, `bannerTestkit.grantsOf`), `src/app/`; `npx tsc --noEmit`; `npx eslint` các tệp sửa; `make jscpd`; test kiến trúc; cổng kiểm của task (ci-task). Playwright và tests/e2e không chạy (chủ shop 24/11): spec `console/e2e/web-looks.spec.ts` (không tràn ngang ở 390/360) viết sẵn.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ liệt kê mã Go (không áp dụng cho console). Trong console đã tìm: màn cùng loại "lưu cả danh sách theo revision" (`web-banners/BannerEditor.tsx`: 409 Giữ/Tải bản mới, khóa khi lưu, báo hiệu realtime chỉ nạp khi chưa sửa, xem trước trễ 350 ms), tìm mẫu (`ProductPicker`), chép liên kết website (`web-campaigns`: `WEBSITE_ORIGIN`, `navigator.clipboard`), đường ghi (`useWriteAction`), hộp thoại (`Dialog`), quyền (`usePerms`), giờ VN (`couponRules`), đếm ký tự (`runeCount`), tìm trễ (`useDebounced`), dời phần tử (`moveTo`), bộ test (`screenTestkit`, `stubDialog`).
- Dùng lại / viết mới vì: dùng lại mọi thứ ở trên, nhập trực tiếp (không chép). Viết mới: `lookRules.ts` (chữ 6 lý do ẩn, kẹp chấm 3–97 %, vị trí chấm mặc định, thêm/bỏ món, chọn ảnh mặc định, nhân bản, lý do khóa Lưu, dựng thân `Look` — nghiệp vụ riêng của bộ phối), `lookData.ts` (truy vấn, xem trước, chi tiết mẫu), các thành phần màn, CSS riêng `web-looks.css` chép lớp `.lk-*` từ `m5.css` của mockup. Phần giống màn banner: jscpd báo 8 đoạn trùng (thanh lưu, hộp 409, tải bản mới, ô chữ đếm, bước, ô giờ, `grantsOf` của testkit, bỏ dấu khi so khớp) → **gộp**: cùng mục đích và cùng lý do đổi (HV19 áp chung "Banner, Bộ phối"), tách vào `web-banners/listEditor.tsx` (commit cấu trúc riêng); `grantsOf` nhập từ `bannerTestkit`; `foldText` của `lookRules` dùng cả cho testkit. Đặt ở `web-banners/` chứ không ở `console/src/shared/`: chỉ hai màn Website dùng, theo tiền lệ nhập chéo giữa các màn (`couponRules`, `campaignRules`, `staffData.Dialog`).

## Soát thiết kế

Task chỉ màn hình; mục 5, 6 của quy trình chỉ liên quan ở các câu sau, còn lại không áp dụng (không chạm database, hàng đợi, tiền, tồn).

- 5.9 Đường ghi có khóa chống trùng: `saveLooks` qua `useWriteAction` (thân gồm `revision` và cả danh sách). `previewLooks` là POST chỉ đọc không có khóa (hợp đồng không khai `IdempotencyKey`), gọi bằng `unwrap`.
- 5.12 Theo mẫu: cấu trúc giống `web-banners` (Task 459): route.tsx, lookRules.ts thuần + test, lookData.ts, lookTestkit.tsx.
- 6.1 Hai người cùng sửa: gửi `revision` đã đọc; 409 `version_conflict` → hộp "Bộ phối vừa được người khác lưu" (HV19) với "Giữ bản đang sửa" (giữ bản sửa; lần Lưu sau vẫn 409) và "Tải bản mới" (bỏ bản sửa, nạp bản máy chủ). Báo hiệu realtime `content.looks` tải lại nhưng chỉ nạp vào màn khi chưa sửa gì.
- 6.2 Bấm hai lần: `useWriteAction` trả lại lời hứa đang chạy; khi đang lưu khóa mọi thao tác (thêm, chọn, ô Trang chủ, lên/xuống, nhân bản, xóa, sửa ô, chọn ảnh, kéo/đặt chấm, bỏ thay đổi).
- 6.13 Quyền: thấy màn cần `content.read`; sửa cần `content.manage`; tìm mẫu, đọc màu và ảnh của món mới cần `catalog.read` (owner, manager có đủ). Thiếu quyền ghi: mọi ô và nút sửa khóa, câu "Bạn chỉ có quyền xem…", chép liên kết vẫn dùng được. Máy chủ vẫn kiểm.
- Giới hạn: ≤ 500 bộ (nút thêm, nhân bản khóa), ≤ 6 bộ trên trang chủ (ô Trang chủ của bộ chưa bật khóa kèm câu "tối đa 6"), 1–3 món; máy chủ vẫn trả 422 nếu lọt (câu tiếng Việt từ `errors.ts`).
- Màn chỉ vẽ dữ liệu API có (Bẫy đã gặp): xem "Quyết định thay chủ shop".

## Quyết định thay chủ shop

1. **Bố cục:** danh sách bên trái, form bên phải (lưới `.ed` của `m5.css`, như màn banner); dưới 1200 px xếp chồng. Mockup vẽ các trạng thái A, B, C nối tiếp nhau; với 124–500 bộ, để form dưới danh sách phải cuộn rất xa. Dễ đổi: một lớp CSS.
2. **Xem trước chỉ gửi bộ đã đủ thông tin:** `previewLooks` kiểm như lưu, một bộ đang điền dở (chưa tên, chưa món, chưa ảnh) làm cả danh sách 422. Màn chỉ gửi bộ đủ; bộ dở hiện "Chưa đủ để hiện: <thiếu gì>" ở danh sách, nút Lưu khóa kèm lý do "Bộ n chưa …". Phương án khác: không xem trước khi còn bộ dở (mất trạng thái mọi bộ khi đang thêm bộ mới).
3. **Màu và ảnh của món vừa chọn:** `getLooks`/`previewLooks` chỉ có ảnh của mẫu đã có trong danh sách gửi đi, không có danh sách màu. Màn đọc `getProduct` (`catalog.read`, owner và manager có) để lấy màu biến thể và ảnh đã xử lý; màu mặc định là màu đầu có ảnh (như `addPiece` của admin cũ), ảnh mặc định là ảnh đầu của màu đó khi bộ chưa có ảnh. Ô màu có thêm "Tự chọn màu còn hàng" (màu rỗng của API). Thiếu `catalog.read` thì không tìm được mẫu (câu của `ProductPicker`).
4. **Vị trí chấm:** chấm mới đặt theo vị trí mặc định của admin cũ (1 món 45 %, 2 món 32/66 %, 3 món 25/50/75 % chiều cao, giữa chiều ngang); khác admin cũ, chấm đã đặt **không** bị dàn lại khi thêm/bỏ món (admin cũ dàn lại hết, làm mất chấm đã kéo). Kẹp 3–97 % như admin (máy chủ nhận 0–100), làm tròn số nguyên; thêm phím mũi tên dời 1 % (bàn phím).
5. **Nhân bản:** bản sao ngay sau bộ gốc, mã mới (liên kết mới), **không** lên trang chủ (như banner "bản sao tắt"; tránh vượt 6).
6. **Chép liên kết:** chỉ bộ đã lưu (bộ mới chưa có trang trên website), dùng `path` của bản đã lưu (website tìm bộ theo khóa 8 ký tự và chuyển tên cũ sang tên mới). Tên miền dùng chung `WEBSITE_ORIGIN` của màn Chiến dịch (Task 458, Quyết định 1).
7. **Câu "tối đa 6":** hiện một lần trên danh sách khi trang chủ đã đủ 6 (không lặp ở từng dòng của tới 500 bộ); ô Trang chủ khóa có `title` cùng câu. Bộ đang bật luôn tắt được.
8. **Xem trước không có giá:** API bộ phối không trả giá, size, còn hàng (website tự lấy, Task 464), nên khung xem trước chỉ có tên món, không có "590.000 ₫" như mockup. Tiêu đề chỉ hiện khi đã nhập (tiêu đề tự động hiện làm chữ mờ của ô).
9. **Hộp 409:** như màn banner (Task 459 Quyết định 2): không có tên người lưu (API chỉ có mã).
10. **Xếp thứ tự:** nút lên/xuống như mockup (không kéo thả dòng: mockup không vẽ). Đang tìm thì khóa lên/xuống kèm câu "Xóa ô tìm để đổi thứ tự.".
11. **Xóa:** hộp xác nhận theo mockup; bộ đã lưu ghi liên kết sẽ không mở được, bộ chưa lưu ghi "chưa có liên kết"; xóa khỏi bản đang sửa, áp dụng khi bấm Lưu ("Bỏ thay đổi" lấy lại).
12. **Giờ hiện:** hai ô giờ Việt Nam, trống = luôn hiện (không thêm nút "Luôn hiện": mockup không vẽ).

## Bằng chứng

Lệnh chung (thư mục `console/`): `npx vitest run src/screens/web-looks src/screens/web-banners src/app` → `Tests 172 passed (172)`; `npx tsc --noEmit` → không lỗi (gồm `e2e/web-looks.spec.ts`); `npx eslint src/screens/web-looks src/screens/web-banners e2e/web-looks.spec.ts` → 0 lỗi; `make jscpd` → `Found 0 clones`; `go test -count=1 ./tests/arch/` → `ok`; cổng kiểm của task (ci-task) → `CI-TASK XANH (so với điểm tách nhánh 115f282…)`, console-check `Test Files 112 passed (112)`, `Tests 1329 passed (1329)` (lần chạy lại sau reviewer lần 1).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Bật bộ thứ 7 → ô khóa, câu "tối đa 6"; tắt một bộ thì mở; đếm x/6, đang hiện | `LookEditor.test.tsx` "7 bộ, 6/6 trang chủ: đếm, lý do ẩn kèm tên món; bộ thứ 7 khóa ô Trang chủ, câu tối đa 6"; `lookRules.test.ts` "trang chủ tối đa 6…", "đủ → null; quá 6 bộ trên trang chủ → câu tối đa 6" | `npx vitest run src/screens/web-looks` | `Tests 43 passed (43)` |
| Kéo chấm ra mép → kẹp 3–97 %; chạm ảnh đặt chấm đang chọn; phím mũi tên | "kéo chấm ra ngoài mép phải dưới → kẹp ở 97 %…"; `lookRules.test.ts` "kẹp 3–97 %…", "điểm bấm → %…", "phím mũi tên dời 1 %…" | như trên | qua |
| 409 → hộp tải lại, không mất bản đang sửa | "409 → hộp người khác vừa lưu; Giữ bản đang sửa giữ bản; Tải bản mới nạp bản máy chủ"; "người khác lưu trong lúc đang sửa: giữ bản sửa; Bỏ thay đổi nạp bản mới nhất" | như trên | qua |
| 390 không tràn ngang | `e2e/web-looks.spec.ts` (viết sẵn — xem mục E2E) | — | chưa chạy (luật chủ shop 24/11) |
| Chọn 1–3 sản phẩm, màu, ảnh từ ảnh sản phẩm, tên gợi ý, câu mở đầu, người mẫu, danh mục, giờ; thân gửi đúng, revision, khóa chống trùng | "thêm bộ: tên gợi ý, chọn 3 món (màu, ảnh tự chọn), đổi ảnh, tiêu đề tự động, lưu gửi đúng thân"; `lookRules.test.ts` "thêm món…", "ảnh để chọn…", "bỏ ô chữ rỗng…" | như trên | qua |
| Bỏ món → ảnh chuyển sang món còn lại | "bỏ món đang có ảnh → ảnh chuyển sang ảnh của món còn lại"; `lookRules.test.ts` "bỏ món…" | như trên | qua |
| Nhân bản (không lên trang chủ), xóa có xác nhận "liên kết… sẽ không mở được nữa", xếp thứ tự | "nhân bản: bản sao ngay sau, không lên trang chủ; xóa hỏi xác nhận kèm liên kết sẽ không mở được"; `lookRules.test.ts` "nhân bản…" | như trên | qua |
| ≤ 500 bộ | "đủ 500 bộ → nút thêm và Nhân bản khóa, câu tối đa 500" | như trên | qua |
| Chép liên kết /bo-phoi/… | "chép liên kết đúng https://elimaz.com.vn/bo-phoi/<tên>-<khóa>, báo đã chép; bộ chưa lưu không chép được"; `lookRules.test.ts` "liên kết đầy đủ của trang bộ" | như trên | qua |
| Tìm theo tên dịp hoặc món | "tìm theo tên dịp hoặc tên món; đang tìm thì không đổi thứ tự"; `lookRules.test.ts` "tìm theo tên dịp hoặc tên món…" | như trên | qua |
| 6 lý do ẩn đúng chữ | `lookRules.test.ts` "đủ 6 lý do…", "dòng danh sách…"; `LookEditor.test.tsx` "lý do ẩn %s: dòng danh sách và khung xem trước đúng chữ" (6 ca) | như trên | qua |
| Xem trước: chỉ gửi bộ đủ thông tin, trạng thái về danh sách, máy tính/điện thoại; khung có ảnh, chấm, câu mặc định, dòng người mẫu | "sửa → previewLooks chỉ gửi bộ đủ thông tin…", "bộ đang hiện: ảnh đã chọn, chấm, câu mở đầu mặc định, dòng người mẫu, tên món" | như trên | qua |
| Lý do khóa Lưu (tên, món, ảnh, chiều cao, giờ) | `lookRules.test.ts` "lý do khóa Lưu" (5 ca) | như trên | qua |
| Đang lưu khóa mọi thao tác (cả đặt/kéo chấm) | "đang lưu: khóa ô Trang chủ, lên/xuống, thêm, nhân bản, sửa, chấm, bỏ thay đổi" (chạm, kéo, phím mũi tên lúc đang lưu không dời chấm — kiểm trước khi lưu xong, sửa sau reviewer lần 1) | như trên | qua |
| Món về muộn sau Bỏ thay đổi không bị thêm lại | "chọn mẫu rồi bấm Bỏ thay đổi trước khi đọc xong chi tiết mẫu → món không được thêm lại" (đỏ khi gỡ điều kiện `resets` trong `pickProduct`, xanh khi có; chờ theo sự kiện — chi tiết mẫu vào bộ nhớ đệm — không chờ giờ thật) | như trên | qua |
| Chỉ xem (thiếu content.manage), vẫn chép liên kết | "chỉ content.read → câu chỉ xem, ô và nút sửa khóa, vẫn chép liên kết" | như trên | qua |
| Lỗi 422 hiện câu tiếng Việt | "lỗi 422 của máy chủ hiện câu tiếng Việt, giữ bản đang sửa" | như trên | qua |
| Đăng ký route | `route.test.tsx` | như trên | qua |
| Màn banner không đổi hành vi sau khi tách phần chung | toàn bộ `src/screens/web-banners` (có 409, đang lưu, Bỏ thay đổi, lỗi 422) | `npx vitest run src/screens/web-banners` | `Tests 51 passed (51)` |

## E2E viết sẵn, chưa chạy

- `console/e2e/web-looks.spec.ts`: "danh sách 7 bộ, 6/6 trang chủ, bộ thứ 7 khóa; form 3 món; xem trước — không tràn ngang" (1512 × 767, 390 × 844), "hộp xóa có liên kết dài không tràn ngang" (1512, 390), "tên 28 ký tự không dấu cách, tên món dài không tràn ngang" (360 × 800). Biên dịch qua `tsc`, eslint sạch; chưa chạy Playwright (luật chủ shop 24/11). Tiêu chí "390 không tràn ngang" của kế hoạch chỉ kiểm được ở đây (jsdom không dàn trang). Tên thử 28 ký tự "ThứHaiĐầuTuầnMặcGìĐểTựTinNhé" như mockup.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi ba tệp (`LookList.tsx`, `LookEditor.tsx`, `route.tsx`); thêm `LookForm.tsx`, `LookStage.tsx`, `LookPreview.tsx`, `lookRules.ts`, `lookData.ts`, `lookTestkit.tsx`, `web-looks.css` theo khuôn màn banner (luật thuần tách riêng để test).
- Thêm commit cấu trúc tách phần chung của màn banner sang `web-banners/listEditor.tsx` (jscpd).

## Việc phát hiện thêm

- API bộ phối không cho biết màu biến thể của mẫu; màn đọc `getProduct` từng món (cần `catalog.read`). Vai trò chỉ có `content.manage` mà không có `catalog.read` sẽ không thêm món được: có thể cho `getLooks`/`previewLooks` trả màu của mẫu.
- `previewLooks` trả 422 cho cả danh sách khi một bộ chưa đủ; màn đã lách bằng cách chỉ gửi bộ đủ. Có thể cân nhắc cho xem trước trả lỗi theo từng bộ.

## Reviewer

- Commit 1 (cấu trúc: tách `web-banners/listEditor.tsx`): **ĐẠT**, không có phát hiện phải sửa. Reviewer đối chiếu từng đoạn tách (thanh lưu, hộp 409, `useReload`, `Counted`, `VnTimeInput`, `Step`): chữ, thứ tự, điều kiện khóa giữ nguyên; 51 test banner đi qua đúng các phần đã tách. Ghi chú: nhánh `area`, `hint` của `Counted` cần test ở commit 2 (ô "Câu chuyện" (area) và "Dòng tiêu đề" (hint, placeholder tự động) vẽ trong mọi test form của `LookEditor.test.tsx`; test "thêm bộ…" kiểm placeholder tiêu đề tự động).
- Commit 2 lần 1: **CẦN SỬA** — phép kiểm "chạm ảnh lúc đang lưu" đọc thân PUT đã gửi trước cú chạm nên luôn đúng. Đã sửa: kiểm vị trí chấm ngay sau chạm/kéo/phím, trước khi lưu xong. Ghi chú không bắt buộc đã làm: mục Ảnh hưởng ghi đúng `moveBanner`, `PREVIEW_DELAY_MS`; tên 28 ký tự ở spec e2e; chọn mẫu rồi bấm Bỏ thay đổi trước khi đọc xong chi tiết → món không được thêm lại (đếm số lần đặt lại bản sửa, có test). Ghi chú còn lại: trùng 8 ký tự đầu mã (xác suất ~3·10⁻⁵ với 500 bộ, máy chủ trả 422) không xử lý; hành vi khác admin cũ (Quyết định 3, 4) báo chủ shop trong báo cáo cuối.
- Commit 2 lần 2: **ĐẠT**, không có phát hiện phải sửa. Ghi chú không bắt buộc đã làm: test "món về muộn" chờ theo sự kiện (`qc.getQueryData` của chi tiết mẫu) thay cho 50 ms giờ thật (luật Bẫy đã gặp: đúng/sai chờ theo sự kiện); kiểm lại test đỏ khi gỡ phần sửa. Chỉ sửa test nên không chạy lại ci-task (đã chạy lại một lần sau lần 1); `npx vitest run src/screens/web-looks` → `Tests 43 passed (43)`, tsc, eslint sạch. Ghi chú còn lại để báo chủ shop: chọn mẫu rồi bấm Lưu ngay trước khi đọc xong chi tiết mẫu thì món đó bị bỏ (nhất quán với bản đã lưu).
- Commit 2 lần 3: **ĐẠT**, không có phát hiện phải sửa (chỉ soát lại test "món về muộn" chờ theo sự kiện; khóa bộ nhớ đệm khớp `productKey`).
