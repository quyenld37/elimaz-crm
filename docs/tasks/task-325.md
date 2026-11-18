# Task 325 — Màn Sản phẩm: ảnh (M1-50c)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 325). Mockup đã duyệt:
`docs/mockups/m1/san-pham-sua.html` (khối Ảnh, trạng thái A, D, E). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: chỉ **thêm** `catalog.Writer.SetImageColor` (mới, 0 chỗ dùng cũ) và handler
  `(*handler).SetProductImageColor` (ogen sinh interface `gen.Handler` có thêm phương thức; hiện thực duy nhất là
  `internal/httpapi.handler`). Không đổi chữ ký ký hiệu nào đang có.
- Gói phụ thuộc ngược (`go list`): `internal/modules/catalog` được nhập bởi `cmd/crm`, `internal/httpapi`,
  `catalogtest`, `inventory/invtest`, `order`, `testkit`; `internal/httpapi` bởi `cmd/crm`. Hook test các gói này.
- Operation OpenAPI: thêm `setProductImageColor` (`PUT /api/v1/catalog/products/{id}/images/{imageId}/color`,
  `catalog.manage`, `IdempotencyKey`); oasdiff: chỉ thêm, không phá vỡ. Dùng (không đổi): `createUpload`,
  `completeUpload`, `getFile`, `attachProductImage`, `reorderProductImages`, `deleteProductImage`,
  `restoreProductImage`, `getOriginalImageURL`, `getProduct`.
- Chỗ dùng trên màn hình: `console/src/screens/products/ProductEdit.tsx` (gắn `ImageManager` vào cột phải trên thẻ
  Phân loại, số ảnh ở đầu trang theo ảnh hiện tại, khóa nút Lưu khi một thao tác ảnh đang chạy, version của bản nháp
  nhận version mới sau thao tác ảnh); tệp mới `ImageManager.tsx`, `useUpload.ts`, `imageRules.ts`; CSS khối ảnh thêm
  vào `products.css` (CSS riêng của màn, không sửa `styles.css` chung). Client sinh `console/src/api/**` sinh lại.
- Câu sqlc, bảng, pha migration: thêm câu `SetProductImageColor` trong `queries/catalog/images.sql` (UPDATE cột `color`
  đã có của `catalog.product_images`). Không migration.
- Test sẽ chạy: `go test -race ./internal/modules/catalog/... ./internal/httpapi/...`; console: `npm test`
  (`ImageManager.test.tsx`, `useUpload.test.ts`, `imageRules.test.ts`, `ProductEdit.test.tsx`), `tsc`, eslint;
  Playwright `e2e/product-images.spec.ts` và `e2e/product-edit.spec.ts` (màn sửa mẫu bị chạm); `make ci` một lần trước
  khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `console/src/shared`
  (useWriteAction, unwrap, errorMessage, TOPICS, queryKey, hasPermission); Go: `catalog.Writer` (lockProduct,
  imageColor, imagesChanged, lockImage), `handler.imageWrite`. Màn: `productRules.coverView` (dựng `<picture>` từ
  biến thể ảnh) — tìm theo "srcSet", "picture".
- Dùng lại / viết mới vì:
  - Mọi lần ghi qua `useWriteAction` (khóa chống trùng, gửi lại cùng khóa); không tự viết vòng thử lại.
  - `<picture>` của ô ảnh dùng lại `coverView` (cùng luật avif → webp → jpeg của ảnh bìa) — tham số là
    `{status, variants}` nên ảnh của mẫu dùng thẳng được.
  - Go: `SetImageColor` dùng lại `lockProduct` (khóa theo version), `imageColor` (kiểm màu thuộc biến thể),
    `imagesChanged` (version +1, sự kiện, audit), `imageAudit`; handler dùng lại `imageWrite`.
  - Viết mới: `useUpload.ts` (tải thẳng lên kho: kế hoạch đặt tệp này), `imageRules.ts` (câu theo lý do từ chối, thứ tự
    sau khi dời, nhãn màu) — chưa có gì tương tự.
  - PUT lên `putUrl` dùng `XMLHttpRequest` (không phải gọi `/api/v1`; eslint cấm `fetch` ngoài lớp realtime, và XHR có
    tiến độ tải lên). Không thêm thư viện.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1–5.3: `color` của ảnh là dữ liệu của `catalog` (bảng `catalog.product_images`); chỉ `catalog` ghi.
- 5.4: đổi màu, tăng version, sự kiện `product.updated`, audit trong **một** transaction (`catalogWriteAsDefaultSeller`).
- 5.5, 5.6, 5.10: không áp dụng: không gọi ra ngoài, không Valkey. Sự kiện qua `bumpProduct` có sẵn.
- 5.7: handler mới chạy trong `catalogWriteAsDefaultSeller` (`tenancy.WithTenant` + người bán mặc định); tenant khác → 404.
- 5.8: handler mỏng (đổi kiểu, gọi Writer); màn không quyết định nghiệp vụ (màu hợp lệ do API kiểm).
- 5.9: operation mới có `x-permission: catalog.manage` và `IdempotencyKey`; màn gửi qua `useWriteAction`.
- 5.11: không thêm thư viện.
- 5.12: theo mẫu module có chiều người bán (Task 27) và bốn operation ảnh của Task 44.

Mục 6 (đúng khi chạy đồng thời):
- 6.1 Hai người cùng sửa: đổi màu khóa dòng mẫu theo `productVersion` (`lockProduct`) → 409 `version_conflict`.
  Màn: mọi thao tác ảnh chạy **lần lượt** (một hàng đợi trong `ImageManager`) và lấy version mới nhất của bản nháp;
  câu trả lời mang version mới. Version mới = version gửi + 1 → bản nháp nhận version đó (không đè thay đổi của người
  khác). Xóa/khôi phục không kiểm version ở API: nếu version trả về lớn hơn version đã biết + 1 (người khác vừa sửa)
  → màn hiện hộp "Mẫu đã thay đổi, tải lại." và không đổi version của bản nháp. Nút Lưu mẫu khóa khi thao tác ảnh
  đang chạy; ngược lại khi đang lưu, mọi thao tác ảnh bị khóa (nút trong fieldset bị tắt, kéo thả và lời gọi bị bỏ
  qua) và gắn ảnh vừa xử lý xong chờ tới khi lưu xong — hai bên không bao giờ gửi song song.
- 6.2 Gửi lại: mỗi thao tác ảnh một khóa (`useWriteAction`); mạng rớt gửi lại cùng khóa. `createUpload`,
  `completeUpload` cũng vậy. Gắn lại tệp đã gắn → 409 `already_exists` → màn bỏ dòng tải lên, báo "Ảnh này đã có trong mẫu. Tải lại trang để
  thấy." (không gắn thêm, không báo xung đột).
- 6.3: khóa dòng mẫu rồi dòng ảnh (ADR-46 nhóm 4, như `lockImage`).
- 6.4–6.9, 6.11: không áp dụng: không việc nền mới, không gọi ra ngoài. Chờ ảnh xử lý: hỏi `getFile` mỗi 2 s
  (báo hiệu `files.file` đánh thức sớm), tối đa 60 s rồi báo "Xử lý ảnh không xong, chọn lại ảnh".
- 6.10 Tiền: không có.
- 6.12 Dữ liệu cá nhân: không có; audit chỉ mã ảnh, mã tệp, màu, vị trí (`imageAudit`).
- 6.13 Quyền: đổi màu cần `catalog.manage` (API kiểm, test 403); màn ẩn nút ảnh khi thiếu `catalog.manage`, ẩn vùng
  tải lên khi thiếu `files.upload`.

## Quyết định thay chủ shop

1. **Thêm API đổi màu ảnh đã gắn** (`setProductImageColor`): mockup có ô chọn "Màu của ảnh N" trên mỗi ảnh mà chưa có
   API (Task 44 ghi treo). Chọn: `PUT …/images/{imageId}/color` thân `{productVersion, color?}` (thiếu `color` = ảnh
   chung), khóa theo version như gắn/sắp. Phương án khác: xóa rồi gắn lại (mất vị trí, hai lần ghi) — bỏ.
2. **Đổi tên màu biến thể không kéo theo màu của ảnh** (treo từ Task 44): xử lý trên màn ảnh — ảnh mang màu không còn
   biến thể nào hiện ô chọn "Màu: X (không còn biến thể)" và ghi chú, nhân viên chọn màu mới bằng API ở mục 1. Phương
   án khác: đổi màu biến thể tự đổi màu ảnh ở API (đổi hành vi `updateVariant`, ngoài phạm vi màn) — ghi vào "Việc phát
   hiện thêm".
3. **"Tải lại" ảnh bị mất:** chọn một ảnh, tải lên, gắn với màu của ảnh mất, dời vào đúng vị trí của ảnh mất rồi xóa
   ảnh mất (xóa mềm, khôi phục được). Phương án khác: chỉ gắn vào cuối — nhân viên phải tự dời và xóa.
4. **PUT lên kho bằng `XMLHttpRequest`** (không phải `fetch`): eslint chỉ cho lớp realtime gọi `fetch`; đường PUT là
   đường ký sẵn của kho tệp, không phải `/api/v1`. Thất bại mạng thử lại tối đa 3 lần (PUT cùng khóa kho là lặp lại an
   toàn).
5. **Ảnh gốc** mở bằng thẻ `<a>` ẩn trỏ đường dẫn ký sẵn (máy chủ đặt tên tải về), không mở tab mới (trình duyệt chặn
   cửa sổ mở sau `await`).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| API đổi màu ảnh: đặt, bỏ màu, version +1, audit; màu lạ, version cũ, ảnh đã xóa, mẫu khác bị từ chối | `TestSetImageColor` | `go test -race -count=1 -run Image ./internal/modules/catalog/` | `ok … catalog 18.189s` (trước khi có mã: `SetImageColor undefined`) |
| `PUT …/images/{imageId}/color`: 200 kèm mẫu, bỏ màu; 400 invalid_input, 409 version_conflict, 403 chỉ xem, 404 tenant khác | `TestCatalogAPI_Images` → `checkImageColor` | `go test -race -count=1 -run Image ./internal/httpapi/` | `ok … httpapi 7.164s` |
| Câu cho từng lý do từ chối (trạng thái E), thứ tự dời/thay ảnh mất, màu không còn biến thể | `imageRules.test.ts` (18 ca) | `npm test` | `Tests 236 passed (236)` (sau sửa theo reviewer lần 1) |
| Tải thẳng lên kho: createUpload → PUT kèm headers → completeUpload → getFile mỗi 2 s; 3 ảnh (một `html-as.jpg`) → 2 sẵn sàng, 1 "Tệp không phải ảnh"; cỡ tệp báo ngay; PUT rớt mạng gửi lại tối đa 3 lần; quá 60 s báo | `useUpload.test.ts` (5 ca) | `npm test` | như trên |
| Khối Ảnh: ảnh bìa, ảnh mất có ghi chú, dời ← → theo version, xóa → Hoàn tác (10) đếm lùi, tự tắt, khôi phục; đổi màu; Ảnh gốc; gắn ảnh xử lý xong (chờ khi đang lưu); Tải lại ảnh mất; chỉ xem | `ImageManager.test.tsx` (19 ca, gồm "đang lưu mẫu → không gửi gì", "already_exists") | `npm test` | như trên |
| Gắn vào màn sửa mẫu: số ảnh theo ảnh hiện tại; thao tác ảnh → lần lưu sau gửi version mới; version nhảy → hộp "Mẫu đã thay đổi"; ảnh đang tải không mất khi lưu | `ProductEdit.test.tsx` (6 ca mới, gồm "đang lưu mẫu → nút khối Ảnh bị khóa") | `npm test` | như trên |
| Trình duyệt 1512 và 390: trạng thái A (ảnh bìa, ảnh mất, ô màu, không tràn, ảnh chụp); tải 3 ảnh (một `html-as.jpg`) → 2 ảnh, 1 "Tệp không phải ảnh"; xóa → Hoàn tác → ảnh về đúng chỗ; dời ảnh | `e2e/product-images.spec.ts` (8 ca) + `product-edit.spec.ts`, `products.spec.ts` (màn bị chạm) | `CRM_E2E_PORT=5125 npx playwright test e2e/product-images.spec.ts e2e/product-edit.spec.ts e2e/products.spec.ts --project=chromium` | `29 passed (15.5s)` (sau khi gộp main) |
| tsc, eslint, prettier | — | `npx tsc --noEmit`, `npx eslint .`, `npx prettier --check src e2e` | không lỗi |
| Cổng kiểm toàn bộ | — | `make ci CRM_PG_PORT=5600`, sau khi sửa trùng và gộp main (691c5b3): `make build vet lint gen-check sqlc-vet vuln test-guard gitleaks catalog-check oasdiff jscpd console-check` | build, vet, lint `0 issues`, gen-check, sqlc-vet, `No vulnerabilities found`, test-guard, `no leaks found`, catalog-check, oasdiff (chỉ thêm), jscpd `Found 0 clones`, console `Tests 233 passed`, bản dựng xong |
| `go test -race ./...` | toàn bộ | trong `make ci` | mọi gói `ok` trừ `platform/files` `TestPublicVariants` và `platform/queue` `TestStopAfterLongHandlerReleases` (gói không đổi; máy chung đang tải nặng, "context deadline exceeded"); chạy riêng hai test đó: `ok … files 4.709s`, `ok … queue 4.259s`. Sau khi sửa: `catalog/...`, `httpapi/...`, `identity` (phần mới từ main) `ok` |

Ảnh chụp trạng thái A (`test-results/…/anh-trang-thai-a-{1512,390}.png`) so với `docs/mockups/m1/png/san-pham-sua-*.png`:
cùng lưới 2 cột, chip "1 · Ảnh bìa" nền đậm, ô ảnh mất nền xám viền đứt có ghi chú, ô màu, ← → Ảnh gốc ×. Sai khác do
dữ liệu: ảnh thử là khối màu đơn (SVG), mockup là ô màu chuyển.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi `useUpload(file)` làm luôn bước `attachProductImage`. Làm: `useUploads()` (ở trang sửa mẫu, ngoài biểu
  mẫu) lo tới khi ảnh ready; `ImageManager` gắn. Lý do: gắn cần version của bản nháp (chỉ biểu mẫu biết), còn biểu mẫu
  được dựng lại sau mỗi lần lưu/tải lại — để hook trong biểu mẫu thì ảnh đang tải bị mất (test "ảnh đang tải lên không
  mất khi lưu mẫu").
- Thêm operation `setProductImageColor` và tệp `imageRules.ts` (Quyết định 1, 2).
- `app/idempotency.ts` xuất thêm `defaultSleep`, `abortReason`, `useAbortOnUnmount` để `useUpload.ts` dùng lại (jscpd
  báo trùng); `useWriteAction` dùng `useAbortOnUnmount`, không đổi hành vi (test idempotency qua). Go: tách
  `lockImageRow`, `Writer.lockProductVersion` dùng chung; test: `refusal`/`checkRefusals`, `otherProduct`, `ok`/`fail`
  vào `productsTestkit.tsx`.
- Thêm `console/src/probe-*/` vào `.gitignore` (việc treo từ reviewer Task 393).

## Việc phát hiện thêm

- Đổi tên màu của biến thể (`updateVariant`) vẫn không kéo theo màu đã ghi trên ảnh; màn ảnh nay hiện "(không còn biến
  thể)" và cho chọn màu mới. Nếu chủ shop muốn tự đổi theo, cần một task API riêng.
- Trang "Thêm mẫu" chưa có khối Ảnh (mockup chỉ vẽ trang sửa; gắn ảnh cần mã mẫu): lưu mẫu trước rồi thêm ảnh.
- PUT thẳng lên kho từ trình duyệt cần CORS của SeaweedFS cho gốc của console (hạ tầng, chưa kiểm ở đây — e2e dùng
  đường PUT giả cùng gốc).

## Reviewer

Lần 1: **CẦN SỬA** — khi đang lưu mẫu, thao tác ảnh vẫn chạy song song với các bước lưu (cùng khóa theo version) →
xung đột oan, mất bản nháp. Đã sửa: `ImageManager` bỏ qua mọi thao tác do người bấm khi `busy` (nút đã tắt trong
fieldset; chặn thêm kéo thả, `draggable=false`); test `ImageManager` "đang lưu mẫu (busy) → … không gửi gì" và
`ProductEdit` "đang lưu mẫu → nút của khối Ảnh bị khóa" (đỏ trước khi sửa). Ghi chú không bắt buộc đã làm:
`already_exists` báo "Ảnh này đã có trong mẫu…" thay vì "Mẫu đã thay đổi" (test mới); `xhrPut` gỡ bộ nghe abort khi
PUT xong. Không làm: thứ tự kiểm version/trùng tệp của `AttachImage` (Task 44, cùng khóa vẫn đúng), khóa chống trùng
khi hai tệp giống hệt sau lỗi không rõ kết quả (không hỏng dữ liệu).

Lần 2 (bản stage sau sửa): **ĐẠT**. Không có phát hiện phải sửa. Reviewer đính chính lần 1: các nút vốn đã khóa trong
fieldset lúc lưu; lỗ hổng thật chỉ là kéo thả, nay đã chặn. Ghi chú không bắt buộc (không sửa): thông báo Hoàn tác vẫn
đếm lùi khi đang lưu (ảnh vẫn khôi phục được 30 ngày); tên ca e2e "dời ảnh… lưu mẫu sau đó" không bấm Lưu (vế đó do
unit test `ProductEdit` phủ).
