# Task 326 — Màn Sản phẩm: bài viết (M1-50d)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 326, Q34, R7). Mockup đã duyệt:
`docs/mockups/m1/san-pham-sua.html` (thẻ "Bài viết chi tiết", trạng thái A, B, C). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: không có. Chỉ đổi **mô tả** của trường `Product.articleHtml` trong
  `openapi/openapi.yaml` (việc treo từ Task 320: nói rõ HTML có ảnh) → `make gen` sinh lại chú thích trong
  `internal/httpapi/gen/**` và `console/src/api/types.gen.ts`; không đổi kiểu, không đổi chữ ký.
- Gói phụ thuộc ngược (`go list`): không gói Go nào đổi mã chạy; `internal/httpapi/gen` chỉ đổi chú thích (gói nhập nó:
  `internal/httpapi`, `cmd/crm`).
- Operation OpenAPI: không thêm/bớt. Dùng (không đổi): `getProduct` (đọc `article`, `images`), `updateProduct`
  (gửi `article`), `createProduct` (gửi `article`). oasdiff: chỉ đổi mô tả, không phá vỡ.
- Chỗ dùng trên màn hình: `console/src/screens/products/ProductEdit.tsx` (gắn `ArticleEditor` ở cột trái dưới thẻ
  Thông tin; lỗi `invalid_article` từ máy chủ → tô đúng khối); `editRules.ts` (bản nháp có `article`, `savePlan` gửi
  `article` trong bước sửa mẫu khi bài khác bản đã lưu, `createBody` gửi `article`, `validate` kiểm khối). Tệp mới:
  `ArticleEditor.tsx`, `articleRules.ts`, `article.css` (CSS riêng của trình soạn, chép đúng luật `.blocks`, `.block`,
  `.tabs`, `.preview`, `.addbar`, `.block-gap` của `docs/mockups/m1/m1.css`; không sửa `styles.css` chung).
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: console `npm test` (`articleRules.test.ts`, `ArticleEditor.test.tsx`, `ProductEdit.test.tsx`,
  `editRules.test.ts`), `npx tsc --noEmit`, `npx eslint .`; Playwright `e2e/product-article.spec.ts` (mới) và các spec
  của màn bị chạm `e2e/product-edit.spec.ts`, `e2e/product-images.spec.ts`; Go: `go test ./internal/httpapi/...`
  (mã sinh đổi chú thích); `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `console/src/shared`
  (useWriteAction, unwrap, errorMessage, ApiError); trong màn Sản phẩm: `productRules.coverPicture` (dựng
  `<picture>` từ biến thể ảnh), `imageRules.moveTo` (dời phần tử trong mảng), `editRules.charCount` (đếm ký tự như máy
  chủ), testkit `productsTestkit.tsx`, e2e `fixtures.ts`. Tìm "picture", "srcSet", "moveTo", "charCount".
- Dùng lại / viết mới vì:
  - Ảnh nhỏ của khối ảnh và ảnh trong "Xem trước": dùng lại `coverPicture` (cùng luật avif → webp → jpeg).
  - Đổi thứ tự khối: dùng lại `moveTo` của `imageRules.ts`.
  - Đếm ký tự: cùng cách của `editRules.charCount` (điểm mã), viết lại một dòng trong `articleRules.ts` vì `editRules`
    nhập `articleRules` (tránh vòng nhập).
  - Lưu: không thêm lời gọi ghi mới — bài viết đi trong bước `updateProduct`/`createProduct` đã có (qua
    `useWriteAction`, cùng khóa chống trùng).
  - Viết mới `articleRules.ts`: chuyển khối API ↔ khối đang soạn, kiểm khối trên màn theo đúng luật của
    `article.Validate`/`Check` (Go, gói internal nên màn không gọi được) để chỉ ra khối sai. Không có gì tương tự trong
    console.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1–5.3: bài viết là dữ liệu của `catalog` (`products.article`); màn chỉ gửi qua `updateProduct`/`createProduct`.
- 5.4, 5.5, 5.6, 5.10: không áp dụng: không đổi đường ghi, không việc nền, không gọi ra ngoài.
- 5.7: không áp dụng (không truy vấn mới).
- 5.8: máy chủ là nơi quyết định bài hợp lệ (`invalid_article`); màn kiểm trước cùng luật chỉ để chỉ ra khối sai, máy
  chủ vẫn kiểm lại.
- 5.9: không operation mới; yêu cầu ghi vẫn qua `useWriteAction` (`Idempotency-Key`).
- 5.11: không thêm thư viện. Chữ hiện bằng phần tử văn bản của React, không `dangerouslySetInnerHTML` (R7), không dùng
  `articleHtml` trên màn (kế hoạch: bàn điều khiển không hiển thị nó).
- 5.12: theo mẫu màn sửa mẫu (Task 324) và khối Ảnh (Task 325).

Mục 6 (đúng khi chạy đồng thời):
- 6.1 Hai người cùng sửa: bài viết gửi trong bước sửa mẫu, khóa theo `version` → 409 `version_conflict` → hộp "Mẫu đã
  thay đổi, tải lại." (đã có). Thao tác ảnh tăng version thì bản nháp nhận version mới (Task 325), bài viết không mất.
- 6.2 Gửi lại: cùng khóa của bước sửa mẫu; mạng rớt gửi lại cùng khóa.
- 6.3–6.9, 6.11: không áp dụng: không khóa, transaction, việc nền mới.
- 6.10 Tiền: không có.
- 6.12 Dữ liệu cá nhân: không có.
- 6.13 Quyền: sửa cần `catalog.manage` (API kiểm); thiếu quyền → màn chỉ hiện "Xem trước", không có nút sửa.

## Quyết định thay chủ shop

1. **Chỉ ra khối sai khi máy chủ trả `invalid_article`**: thân lỗi theo luật chỉ có `{error, request_id}`, không nói
   khối nào. Chọn: màn kiểm trước cùng luật của `article.Validate` (`articleRules.checkArticle`: chữ trống, quá 5.000 ký
   tự, danh sách 0 hoặc quá 50 dòng, ký tự điều khiển, ảnh chưa chọn, quá 200 khối, quá 200 KB) và khi máy chủ vẫn trả
   `invalid_article` thì `locateInvalid` tìm khối sai (thêm luật mà chỉ máy chủ biết chắc: khối ảnh trỏ tệp không còn
   là ảnh sẵn sàng của mẫu); không tìm ra → câu chung của `invalid_article`. Phương án khác: thêm chỉ số khối vào thân
   lỗi (đổi luật thân lỗi chung và `errors.go`, ngoài phạm vi màn) — ghi vào "Việc phát hiện thêm".
2. **Cấp tiêu đề và chú thích ảnh**: mockup không có ô chọn cấp tiêu đề (2/3) hay ô chú thích ảnh. Chọn: giữ nguyên giá
   trị đã lưu, khối tiêu đề mới cấp 2, khối ảnh mới không chú thích; không thêm ô ngoài mockup (luật màn hình 1).
   Phương án khác: thêm ô — cần mockup và OK của chủ shop (ghi vào "Việc phát hiện thêm").
3. **Thu gọn bài dài**: mockup vẽ bài 30 khối hiện 1–4 và 29–30. Chọn: bài trên 8 khối hiện 4 khối đầu, 2 khối cuối,
   nút "Khối a–b (n khối) đang thu gọn — bấm để mở"; có khối sai trong đoạn thu gọn hay vừa dời khối thì mở hết (khối
   sai, khối vừa dời không bị giấu).
4. **Nút "+ Ảnh" khi mẫu chưa có ảnh sẵn sàng** (trang thêm mẫu, hay mẫu chưa tải ảnh): nút tắt kèm câu "Tải ảnh lên
   khối Ảnh trước, rồi thêm khối ảnh." Phương án khác: cho thêm khối ảnh trống rồi báo lỗi khi lưu — nhân viên không
   biết chọn ảnh ở đâu.
5. **Ảnh trong bài trỏ ảnh đã xóa khỏi mẫu**: máy chủ vẫn nhận nếu tệp còn sẵn sàng, nên màn không chặn trước khi gửi;
   ô chọn hiện "Ảnh không còn dùng được" để nhân viên thấy và đổi, "Xem trước" bỏ khối đó.
6. **Người thiếu `catalog.manage`**: thẻ bài viết chỉ hiện "Xem trước" (không thẻ Soạn, không nút) vì cả biểu mẫu đã
   tắt cho người chỉ xem.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Khối API ↔ khối soạn đi một vòng giữ thứ tự, chữ có `< >`; kiểm khối theo luật `article.Validate` (trống, 5.000 ký tự, 0/quá 50 dòng, ký tự điều khiển, ảnh chưa chọn, 200 khối, 200 KB); tìm khối sai khi máy chủ trả `invalid_article`; câu trạng thái C; thu gọn 5–28; lựa chọn ảnh | `articleRules.test.ts` (14 ca) | `npx vitest run src/screens/products/` | trước khi có mã: `Cannot find module './articleRules'`; sau: `Tests 136 passed (136)` |
| Trình soạn: 30 khối đếm và thu gọn 5–28, bấm mở; ↑ ↓ × (khối đầu/cuối tắt); 0 khối câu trống; thêm 4 loại; chọn ảnh "Ảnh 2 · Be"; chưa có ảnh → "+ Ảnh" tắt; Xem trước: `<img src=x onerror=alert(1)>` hiện nguyên văn, chỉ 1 thẻ img (của khối ảnh), không alert; lỗi tô đúng khối, khối sai trong đoạn thu gọn thì mở; chỉ xem → chỉ Xem trước | `ArticleEditor.test.tsx` (7 ca) | như trên | trước: `Failed to resolve import "./ArticleEditor"`; sau: qua |
| Bản nháp mang bài viết: không đổi → không bước; đổi thứ tự → bước sửa mẫu gửi cả bài; lưu xong hết thay đổi; validate/createBody | `editRules.test.ts` (4 ca mới) | như trên | qua |
| Màn sửa mẫu: sửa bài → `updateProduct` gửi `article` đúng thứ tự; khối sai → không gửi, hộp "Chưa lưu: bài viết có 1 khối sai." + khối tô đỏ, sửa thì tắt; máy chủ trả `invalid_article` → lỗi tại đúng khối ảnh (ảnh đã xóa), chọn ảnh khác thì hết đỏ; không tìm ra khối → câu chung; tạo mẫu kèm bài | `ProductEdit.test.tsx` (5 ca mới) | như trên | trước: 5 ca đỏ (`Unable to find … "+ Đoạn văn"`); sau: qua |
| Trình duyệt 1512 và 390: trạng thái A (30 khối, thu gọn, chữ `< >` nguyên văn, ảnh nhỏ, không tràn, ảnh chụp); Xem trước không chạy mã (không hộp thoại, không `img[onerror]`); 0 khối → thêm 30 khối, lưu, tải lại → đúng 30 khối đúng thứ tự; máy chủ trả `invalid_article` → khối 30 tô đỏ, chọn ảnh khác rồi lưu qua | `e2e/product-article.spec.ts` (8 ca) + spec màn bị chạm `product-edit`, `product-images`, `products` | `CRM_E2E_PORT=5126 npx playwright test e2e/product-article.spec.ts e2e/product-edit.spec.ts e2e/product-images.spec.ts e2e/products.spec.ts --project=chromium` | `37 passed (22.9s)` (sau khi gộp main) |
| tsc, eslint, prettier, toàn bộ test màn hình, bản dựng | — | `make console-check` | `Test Files 22 passed`, `Tests 266 passed (266)`, `built in 1.55s`, gitleaks dist `no leaks found` |
| Mã sinh khớp nguồn (mô tả `articleHtml`) | — | `make gen-check` (trong `make ci`) | qua sau khi stage mã sinh |
| Cổng kiểm còn lại | — | `make ci CRM_PG_PORT=5600` (sau khi gộp main a4492e7), rồi `make vuln oasdiff catalog-check jscpd gitleaks console-check` | build, vet, lint `0 issues`, gen-check, sqlc-vet, `go test -race ./...` 46 gói `ok` (gồm `httpapi`, `catalog`); `No vulnerabilities found`; oasdiff qua (chỉ đổi mô tả); catalog-check qua; jscpd `Found 0 clones`; `no leaks found`. Hai chỗ không qua, đều ngoài task (xem "Việc phát hiện thêm"): `tests/deploy` `TestMakeMutation_CoversHighRiskDirs`; `make test-guard` so với `main` đã đi tiếp (51b34a5) nên báo tệp test của main mới là "bị xóa". Phần của task: `git diff --cached --name-status a4492e7` không xóa tệp test nào, không thêm `skip/only`. |

Ảnh chụp trạng thái A (`console/test-results/…/bai-viet-trang-thai-a-{1512,390}.png`) so với
`docs/mockups/m1/png/san-pham-sua-1512.png`: cùng thẻ "Bài viết chi tiết", số khối, thẻ Soạn/Xem trước gạch đỏ, khối
có nhãn "n · Loại" và ↑ ↓ × (nút tắt nền xám), ô danh sách kèm "Mỗi dòng là một ý", khối ảnh có ảnh nhỏ và ô chọn,
dòng thu gọn viền đứt, câu "Chữ được giữ nguyên văn…", thanh "Thêm khối:". Sai khác do dữ liệu: ảnh thử là khối màu đơn;
khối 30 trong mockup đang lỗi (đỏ), ảnh chụp là khối đúng. Ảnh 390 chụp cả phần tử nên đầu trang và thanh Lưu dính
đáy đè lên ảnh (do cách chụp, không phải bố cục).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `articleRules.ts` (luật thuần của trình soạn) và `article.css` (CSS riêng của trình soạn) ngoài hai tệp kế hoạch
  ghi; `editRules.ts` đổi để bản nháp mang bài viết (lưu chung bước sửa mẫu).
- Test component và luật viết trước và đỏ trước khi có mã; spec Playwright `product-article.spec.ts` viết sau khi màn
  chạy được (không có lần chạy đỏ ghi lại).
- Việc treo từ Task 320 làm trong task này: mô tả `articleHtml` trong `openapi.yaml` nói rõ HTML có ảnh (thẻ, thuộc
  tính, bỏ khối ảnh chưa sẵn sàng); `make gen`.

## Việc phát hiện thêm

- Thân lỗi `invalid_article` không có chỉ số khối: màn đoán khối sai theo luật đã biết (Quyết định 1). Muốn chắc chắn
  thì cần thêm trường (ví dụ `block`) vào thân lỗi — đổi luật thân lỗi chung, cần chủ shop quyết.
- Sau khi gộp main a4492e7: `tests/deploy` `TestMakeMutation_CoversHighRiskDirs` đỏ ("make mutation không đột biến
  internal/platform/db, webhooks, modules/inventory") — commit a4492e7 mở rộng vùng rủi ro cao nhưng Makefile chưa
  thêm các thư mục đó vào `make mutation`. Task này không chạm Makefile, `tests/deploy`. Cần điều phối viên sửa trên main.
- `make test-guard` so với nhánh `main` đang đi tiếp (sau lần gộp có 51b34a5 với tệp test `order/internal/pricing`), nên
  báo các tệp test mới của main là "bị xóa" ở nhánh này. Không phải xóa thật; gộp main lần sau sẽ hết.
- Test `internal/imager` (`TestImager_HangKilledWithinDeadline`, `TestExecRunner_TimeoutKillsProcessGroup`) đỏ một lần
  khi máy tải nặng (load ~18), chạy riêng thì qua (`ok … imager 8.309s`).
- Mockup chưa có ô chọn cấp tiêu đề (h2/h3) và ô chú thích ảnh (chú thích là `alt` của ảnh trên web): cần mockup mới
  nếu muốn nhân viên sửa được hai thứ này.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lần 1, trên phần đã stage sau khi gộp main a4492e7). Không có phát hiện phải sửa.
Ghi chú không bắt buộc: (1) mục Tái dùng nói dùng lại `charCount` nhưng `articleRules.ts` tự viết lại — đã sửa lời ghi;
(2) số ca `articleRules.test.ts` là 14, không phải 15 — đã sửa; (3) khi không tìm ra khối, câu chung của
`invalid_article` nói "khối được đánh dấu" dù không khối nào đánh dấu — câu chữ, để nguyên; (4) `locateInvalid` chỉ đoán
(Quyết định 1), cần chỉ số khối trong thân lỗi để chắc chắn — đã ghi ở "Việc phát hiện thêm".
