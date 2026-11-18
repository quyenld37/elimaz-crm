# Task 321 — Ảnh gốc (M1-44c)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`, Task 321 (M1-44c). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu cũ nào, chỉ thêm. Mới: `files.OriginalURL`, `files.SignedURL`, `files.OriginalTTL` (gói `internal/platform/files`); `catalog.OriginalImageURL` (gói `internal/modules/catalog`); handler `GetOriginalImageURL` (ogen sinh interface). `files.DownloadURL` được gọi lại bên trong `OriginalURL`, chữ ký giữ nguyên (0 chỗ dùng ngoài gói files ngoài test).
- Gói phụ thuộc ngược (go list): của `platform/files`: `cmd/crm`, `internal/httpapi`, `internal/modules/catalog`, `files/filestest`, `internal/testkit`. Của `modules/catalog`: `cmd/crm`, `internal/httpapi`, `inventory/invtest`, `modules/order`. Chỉ thêm hàm nên các gói này không phải sửa; build lại cả.
- Operation OpenAPI bị chạm, oasdiff: thêm `GET /api/v1/catalog/products/{id}/images/{imageId}/original` `getOriginalImageURL` (`x-permission: catalog.manage`), schema mới `SignedUrl {url, expiresAt}`. Chỉ thêm → oasdiff không có thay đổi phá vỡ. Task 324 song song cũng thêm dòng vào openapi.yaml: chỉ thêm khối của mình.
- Chỗ dùng trên màn hình: chưa có (nút "Tải ảnh gốc" thuộc Task 325, M1-50c); `console/src/api/**` sinh lại theo `make gen`.
- Câu sqlc, bảng, pha migration: thêm câu `ProductImageOriginal` vào `queries/catalog/images.sql` (đọc `catalog.product_images` + `catalog.products`, cùng module). Không migration, không bảng mới. Kho tệp: chỉ đọc dòng `platform.files` qua hàm có sẵn (`DownloadURL`/`getFile`), không SQL mới.
- Test sẽ chạy: `go test -race ./internal/platform/files/...` (thêm `original_url_test.go`, chạy trên SeaweedFS của bộ chung crm-shared để kho thật từ chối đường dẫn hết hạn), `./internal/modules/catalog/...`, `./internal/httpapi/...` (thêm `TestOriginalURL_ExpiresAndNeedsPermission`), build `./cmd/crm`; `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `files` (`DownloadURL`, `FileStore.PresignGet`, `contentDisposition`), tìm "Presign", "DownloadURL", "orig" trong `internal/`.
- Dùng lại / viết mới vì: dùng lại `files.DownloadURL` (đã kiểm ready, tenant, đã xóa, mất, và với ảnh mục đích standard đã ký tới `orig/<sha>` trên `BucketOriginals`, đặt `Content-Disposition: attachment` qua `PresignGet`). Viết mới `files.OriginalURL` mỏng chỉ để: (1) từ chối tệp không phải ảnh standard (riêng tư R10, tệp xuất) — `DownloadURL` với ảnh riêng tư ký bản JPEG chứ không phải ảnh gốc; (2) gắn đuôi tệp theo loại phát hiện (`jpg`/`png`/`webp`) vào tên tải về; (3) trả kèm `ExpiresAt`. Phần catalog (`OriginalImageURL`) đọc ảnh + mã mẫu bằng một câu sqlc mới vì `ProductImages` không trả mã mẫu và không cần biến thể; dùng lại `checkImageFile`-kiểu đổi lỗi sang sentinel của catalog. Handler dùng lại `readAsDefaultSeller`.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: ảnh sản phẩm thuộc `catalog`, tệp thuộc `platform/files`; không có dữ liệu mới.
- 5.2 Không ghi bảng module khác: chỉ đọc; không ghi gì.
- 5.3 Đọc module khác qua view/public API: catalog đọc tệp qua hàm công khai `files.OriginalURL`, không đọc `platform.files`.
- 5.4, 5.5, 5.6: không áp dụng — đường chỉ đọc, ký đường dẫn tính tại chỗ không gọi mạng (ADR-46 cho phép trong transaction).
- 5.7 Ngữ cảnh shop: đọc trong `readAsDefaultSeller` (tenancy.WithTenant + người bán mặc định, ADR-29); tệp của tenant khác như không có (RLS).
- 5.8 Nghiệp vụ không biết HTTP: catalog nhận `files.FileStore` (interface), không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi có khóa chống trùng: không áp dụng — GET chỉ đọc; khai `x-permission: catalog.manage`.
- 5.10, 5.11: không áp dụng — không Valkey, không thư viện mới.
- 5.12 Module mẫu: theo `catalog` (hàm đọc nhận `sellerID`), sentinel lỗi của catalog, đổi lỗi ở `errors.go` (sentinel đã có, không cần dòng mới).
- 6.1–6.11: không áp dụng — không ghi, không khóa, không tiền, không hàng đợi.
- 6.9 Treo: ký đường dẫn không gọi mạng; truy vấn trong timeout chuẩn của TxAPI.
- 6.12 Dữ liệu cá nhân: ảnh sản phẩm không phải dữ liệu cá nhân; ảnh riêng tư (R10) bị từ chối (`OriginalURL` chỉ nhận ảnh standard); không log URL ký.
- 6.13 Quyền: mặc định từ chối; operation khai `catalog.manage`, người chỉ `catalog.read` → 403 (test). Đường dẫn hạn 5 phút (TP05), hết hạn kho trả 403 (test trên SeaweedFS).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, đỏ trước khi có mã | `TestOriginalURL_*` | `go test -race -run 'TestOriginalURL' ./internal/platform/files/ ./internal/httpapi/` | trước khi có mã: `undefined: files.OriginalURL` (build failed); HTTP `getOriginalImageUrl: 501 not_implemented` |
| TP05: người có `catalog.manage` lấy đường dẫn, đọc đúng ảnh gốc `orig/<sha>`; `expiresAt` = 5 phút; tải về `attachment` tên `DAM01-2.jpg` | `TestOriginalURL_ExpiresAndNeedsPermission` (`internal/httpapi/handlers_catalog_original_test.go`) | `go test -race -count=1 -v -run 'TestOriginalURL' ./internal/platform/files/ ./internal/httpapi/` | `--- PASS: TestOriginalURL_ExpiresAndNeedsPermission (49.69s)`, `ok elimaz.com.vn/crm/internal/httpapi` |
| TP05: người chỉ có `catalog.read` → 403 `forbidden`, thân không có đường dẫn | như trên | như trên | PASS (như trên) |
| Ảnh đã xóa, mẫu không có, tenant khác → 404 `not_found`; sau khi xóa ảnh đầu, tên đổi thành `DAM01-1.jpg` (thứ tự hiện) | như trên | như trên | PASS (như trên) |
| TP05: hạn thử 2 s, đọc sau 3 s → kho thật (SeaweedFS của crm-shared) trả 403; trong hạn đọc được, `Content-Disposition: attachment` có `DAM01-2.jpg`; `ExpiresAt` đúng | `TestOriginalURL_ExpiresOnStore` (`internal/platform/files/original_url_test.go`) | như trên | `--- PASS: TestOriginalURL_ExpiresOnStore (47.87s)` |
| R10 và trạng thái tệp: ảnh riêng tư, tệp không phải ảnh, mất, đã xóa, tenant B → `ErrNotFound`; đang xử lý → `ErrNotReady` | `TestOriginalURL_Refusals` | như trên | `--- PASS: TestOriginalURL_Refusals (19.23s)`, `ok elimaz.com.vn/crm/internal/platform/files` |
| Không làm hỏng phần khác của các gói đổi (ảnh sản phẩm, catalog, kho tệp) | cả bộ test của gói | `make ci` (sau khi gộp main có Task 395) | `ok .../internal/httpapi 302.769s`, `ok .../internal/modules/catalog 135.319s`, `ok .../internal/platform/files 239.916s`, govulncheck `No vulnerabilities found`; lần đó oasdiff đỏ chỉ vì main vừa có Task 324 → đã gộp lại, chạy lại `make ci` (dòng dưới) |
| Toàn bộ cổng kiểm trên bản đã gộp main (Task 395, Task 324) | mọi test + cổng | `make ci` | exit 0: `ok .../internal/httpapi 330.111s`, `ok .../internal/modules/catalog 150.703s`, `ok .../internal/platform/files 269.899s`; apicheck (oasdiff) không có thay đổi phá vỡ; jscpd `Found 0 clones`; test-guard qua; gitleaks `no leaks found`; console `Tests 161 passed` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `internal/modules/catalog/image_original.go` (`catalog.OriginalImageURL`): kế hoạch chỉ ghi `original_url.go` của kho tệp, nhưng đọc ảnh + mã mẫu và đổi lỗi kho tệp sang sentinel của catalog là việc của module catalog (handler mỏng, luật 5.8).
- Handler đặt ở `internal/httpapi/handlers_catalog_images.go` (cạnh bốn operation ảnh của Task 44), không ở `handlers_catalog.go`.
- operationId là `getOriginalImageURL` (kế hoạch ghi `getOriginalImageUrl`): luật đặt tên của revive (`URL` viết hoa) chặn tên handler `GetOriginalImageUrl` do ogen sinh. Đường dẫn và hành vi giữ nguyên.
- Schema trả về đặt tên `SignedUrl` `{url, expiresAt}` (dùng lại được cho đường dẫn tải khác).
- Ca "hạn thử 2 s, đọc sau 3 s → kho trả 403" chạy ở gói files trên SeaweedFS thật (`TestOriginalURL_ExpiresOnStore`), không qua HTTP: handler luôn ký đúng 5 phút (`files.OriginalTTL`), thêm cấu hình hạn chỉ để test là thừa. Test HTTP `TestOriginalURL_ExpiresAndNeedsPermission` kiểm `expiresAt` = 5 phút, đọc được, attachment, 403 với `catalog.read`, 404 cho ảnh đã xóa, mẫu khác, tenant khác.

## Quyết định thay chủ shop

- **"Vị trí" trong tên tệp tải về** là thứ tự hiện 1, 2, … trong các ảnh đang hiện (theo `position`, `id`), không phải giá trị cột `position`. Vì: sau khi xóa ảnh, cột `position` có lỗ (ảnh còn lại có thể là `position = 2` dù đứng đầu); tên tệp theo thứ tự nhìn thấy trên màn hình dễ hiểu hơn. Phương án khác: dùng thẳng cột `position` (một dòng SQL để đổi lại).
- **Ảnh riêng tư (R10) và tệp không phải ảnh**: `files.OriginalURL` trả `ErrNotFound` (404) thay vì ký bản JPEG như `DownloadURL`, vì "ảnh gốc" của ảnh riêng tư không được tồn tại ngoài kho riêng tư. Catalog chỉ gắn `product_image` nên đường API không chạm ca này.
- **Đuôi tệp** theo loại phát hiện được (`image/jpeg` → `.jpg`, `png`, `webp`).

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lần 2).

- Lần 1: CẦN SỬA, chỉ vì bảng bằng chứng còn trống; mã đúng ở mọi điểm soát (quyền `catalog.manage`, tenant/người bán, R10, hạn và `ExpiresAt`, `Content-Disposition` không chèn header được, đổi lỗi, SQL thứ tự hiện). Ghi chú không bắt buộc: `files.ErrExpired` thành 500 → đã đổi sang `ErrNotFound`; tên operation trong mục Ảnh hưởng → đã sửa.
- Lần 2 (sau khi gộp main có Task 324, sinh lại mã): ĐẠT, không phát hiện phải sửa; xác nhận `getProductClassification` của Task 324 còn nguyên trong `openapi.yaml` và `routes_gen.go`. Ghi chú không bắt buộc: ca "mẫu khác" của test HTTP dùng mã mẫu ngẫu nhiên (điều kiện `product_id` của SQL vẫn chặn ca thật) — giữ nguyên.
