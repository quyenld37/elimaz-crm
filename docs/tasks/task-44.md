# Task 44 — Ảnh sản phẩm: gắn, sắp xếp, xóa mềm, file_variants_v

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task M1-44). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): **chỉ thêm**, không đổi chữ ký nào đã có.
  - `catalog`: thêm `Image`, `ImageView`, `ErrFileNotReady`, `ErrRestoreExpired`, `Writer.AttachImage`,
    `Writer.ReorderImages`, `Writer.DeleteImage`, `Writer.RestoreImage`, `ProductImages`. `catalog.GetProduct` giữ chữ ký
    (7 chỗ gọi: `handlers_catalog.go` 1, `products_test.go`, `variants_test.go`); ảnh đọc bằng `ProductImages` trong cùng
    tx (xem Quyết định Q1).
  - `files`: thêm `SelectPreset` (luật chọn bộ kích thước dùng chung cho Task 320, 322 và danh sách), gói test
    `files/filestest` (tạo tệp ảnh `ready` có 12 biến thể, đánh dấu `lost`). `files.DownloadURL` đổi hành vi cho mục
    đích ảnh standard (ký tới `orig/<sha>` trên `crm-originals` thay cho `tmp/…`; việc treo của Task 43). Chỗ gọi
    `DownloadURL`: chỉ test của `files` (`register_test.go`, `process_private_test.go`); chưa handler nào gọi.
  - `queries.ProductRow`: thêm `CoverImageID`, `CoverFileID` (một chỗ dùng: `handlers_lists.go`).
  - `config.Config.ImagePresetVersion`: nay đọc cả ở vai trò `api` (trước chỉ `worker`).
- Gói phụ thuộc ngược (go list): `catalog` ← `cmd/crm`, `internal/httpapi`, `inventory/invtest`, `order`;
  `platform/files` ← `cmd/crm`, `internal/httpapi`, `internal/testkit`; `queries` ← `internal/httpapi`;
  `platform/config` ← `cmd/crm`, `httpapi`, `logging`, `metrics`, `observability`, `telemetry`.
- Operation OpenAPI bị chạm, oasdiff: thêm `attachProductImage`, `reorderProductImages`, `deleteProductImage`,
  `restoreProductImage` (đều `catalog.manage`, có `IdempotencyKey`); `Product` thêm trường trả về `images` (bắt buộc ở
  câu trả lời — không phá vỡ); `ProductListItem` thêm `cover` tùy chọn. Mã lỗi `file_not_ready`, `restore_expired` đã
  có trong enum. Không thay đổi phá vỡ.
- Chỗ dùng trên màn hình: màn Sản phẩm chưa có (Task 50, 325); client sinh lại `console/src/api/**`, `tsc` phải qua.
  `console/src/app/errors.ts` đã có câu cho `file_not_ready`, `restore_expired`.
- Câu sqlc, bảng, pha migration: migration mới `0125_product_images.sql` (pha mở rộng: bảng
  `catalog.product_images`, index, view `catalog.product_images_v`, `platform.file_variants_v`, quyền). Câu sqlc mới
  `queries/catalog/images.sql` (khối `catalog` của `sqlc.yaml`). Không sửa câu cũ. `internal/queries/products_dynsql.go`
  thêm một LATERAL đọc `catalog.product_images_v` (view công bố).
- Test sẽ chạy: `go test -race ./internal/modules/catalog/... ./internal/platform/files/... ./internal/queries/...
  ./internal/httpapi/... ./internal/platform/config/... ./tests/arch/... ./internal/platform/tenancy/...` (tenancy:
  `CheckSchemaRules` cho bảng, view mới); `make ci` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `files`
  (`Status`, `PublicVariants`, `Describe`, `Preset`, `OriginalKey`), `catalog.Writer` (`productChanged`, `bumpProduct`,
  `audit`, `lockProductAnyVersion`), `catalogtest`, `testkit.OpenStaff`, `testkit.MemFileStore`, `httpapi.catalogWrite`,
  `readAsDefaultSeller`, `WriteTx`. Tìm "preset", "variant", "cover", "position", "reorder" trong `internal/` — chưa có
  luật chọn bộ kích thước, chưa có hàm sắp thứ tự dùng chung.
- Dùng lại / viết mới vì:
  - Dùng lại `files.Status` (kiểm tệp trước khi gắn), `files.PublicVariants` (một câu cho mọi ảnh của mẫu và mọi ảnh
    bìa của trang danh sách), `Writer.bumpProduct` + `productChanged` (sự kiện `product.updated`, báo hiệu),
    `Writer.audit`, `lockProductAnyVersion`/`lockProduct` (khóa mẫu trước, ADR-46 nhóm 4), `catalogWrite` ở handler.
  - Viết mới `files.SelectPreset`: luật "bản `IMAGE_PRESET_VERSION` nếu đủ, không thì lùi" chưa có; đặt ở `files` vì
    là luật của bộ kích thước (biết `Preset(v)`), Task 320, 322 và danh sách sản phẩm dùng lại.
  - Viết mới `files/filestest` (như `catalogtest`): test của catalog (và Task 320, 50) cần tệp ảnh `ready` mà không
    chép SQL của `platform.files`, không chạy imager.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `catalog.product_images` thuộc catalog (spec 04 mục 3; ADR-46 nhóm 4 đã ghi). View
  `platform.file_variants_v` thuộc kho tệp (platform) — tạo cùng migration theo kế hoạch.
- 5.2 Không ghi bảng module khác: catalog chỉ ghi `catalog.product_images`, `catalog.products` (version).
- 5.3 Đọc module khác qua view/public API: trạng thái tệp qua `files.Status`, đường dẫn qua `files.PublicVariants`;
  SQL của catalog không nhắc `platform.*`. Khóa ngoại `(tenant_id, file_id) → platform.files` là ngoại lệ kế hoạch ghi
  (ADR-27 mục 4). `internal/queries` chỉ đọc `catalog.product_images_v`.
- 5.4 Nguyên tử: kiểm tệp, khóa mẫu, ghi ảnh, tăng version, sự kiện, audit trong một transaction (`WriteTx`).
- 5.5 Sau commit: `product.updated` qua `events.Emit` (outbox), báo hiệu realtime trong tx (được phép mất).
- 5.6 Gọi ngoài: không áp dụng — không gọi dịch vụ ngoài (ký đường dẫn S3 là phép tính cục bộ, không ở task này).
- 5.7 Tenant: bảng mới `tenant_id`, `UNIQUE (tenant_id, id)`, unique khác bắt đầu bằng `tenant_id`, khóa ngoại ghép,
  `enable_tenant_rls`; view `security_invoker = true`. Mọi truy vấn trong `WriteTx`/`WithTenant`.
- 5.8 Không HTTP trong module: đúng; handler mỏng.
- 5.9 Đường ghi: 4 operation có `x-permission: catalog.manage` và `IdempotencyKey`.
- 5.10 Valkey: không áp dụng — không dùng.
- 5.11 Thư viện mới: không có.
- 5.12 Module mẫu: theo `catalog` (Writer, version, sentinel), lỗi đổi HTTP ở `errors.go`.

Mục 6:
- 6.1 Đồng thời: gắn và sắp lại so `version` của mẫu sau khi khóa dòng mẫu (`FOR UPDATE`); xóa/khôi phục khóa mẫu
  (không so version, như sửa biến thể) rồi khóa dòng ảnh; mọi thay đổi tăng version mẫu → hai người sắp lại cùng
  version: đúng một người thành công. Gắn cùng tệp hai lần đồng thời: khóa mẫu tuần tự hóa, lần sau gặp dòng đã có.
- 6.2 Gửi lại: `Idempotency-Key`; khóa tự nhiên `UNIQUE (tenant_id, product_id, file_id)`.
- 6.3 Thứ tự khóa: `catalog.products` → `catalog.product_images` (ADR-46 nhóm 4); `platform.files` chỉ đọc không khóa.
- 6.4 Lỗi tạm: `WriteTx` đã chạy lại theo ADR-46.
- 6.5 Cách ly: READ COMMITTED + khóa dòng mẫu.
- 6.6 Việc nền: không áp dụng — không có việc nền mới.
- 6.7 429: không áp dụng.
- 6.8 Chết giữa chừng: transaction tự lùi.
- 6.9 Treo: giới hạn chung của `db`; câu đọc có index `(tenant_id, product_id, position) WHERE deleted_at IS NULL`.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: thứ tự sau sắp lại là hoán vị đúng của ảnh còn lại (test bảng + rapid cho `SelectPreset`); không
  phải vùng rủi ro cao.
- 6.12 Dữ liệu cá nhân: audit chỉ mã ảnh, mã tệp, màu, vị trí; không tên tệp gốc.
- 6.13 Quyền: `catalog.manage` cho ghi, `catalog.read` cho đọc (ảnh trong `getProduct`, ảnh bìa trong
  `listProducts`); ảnh riêng tư (R10) không bao giờ có đường dẫn: chỉ gắn được `product_image`, view lọc
  `mode = 'standard'`.

## Quyết định thay chủ shop

- **Q1 — `GetProduct` giữ chữ ký; ảnh đọc bằng `catalog.ProductImages(ctx, tx, sellerID, productID, ImageView)`**
  trong cùng tx, handler ghép vào `Product.images` (cả câu trả lời của tạo, sửa mẫu). Vì: đường dẫn ảnh cần
  `FILES_PUBLIC_BASE_URL` và `IMAGE_PRESET_VERSION` (cấu hình của tiến trình) mà các chỗ khác gọi `GetProduct`
  (test, đơn) không cần. Phương án khác: thêm tham số vào `GetProduct` (sửa 7 chỗ gọi).
- **Q2 — Gắn lại tệp đã gắn:** ảnh đang hiện → `ErrAlreadyExists` (409 `already_exists`); ảnh đã xóa mềm của cùng mẫu
  → hồi lại dòng đó (bỏ `deleted_at`, màu mới, cuối danh sách), không giới hạn 30 ngày vì nhân viên chủ động gắn lại.
  Vì: `UNIQUE (tenant_id, product_id, file_id)` của kế hoạch. Phương án khác: báo lỗi và bắt khôi phục.
- **Q3 — Màu của ảnh** (không bắt buộc) phải là một màu đang có của biến thể trong mẫu (so đúng chuỗi sau khi bỏ khoảng
  trắng đầu cuối), ngược lại `ErrInvalidInput`. Vì: ảnh gắn màu để website đổi ảnh khi chọn màu (KH02); màu lạ không
  khớp biến thể nào là lỗi gõ. Phương án khác: nhận mọi chuỗi ≤ 50 ký tự.
- **Q4 — Vị trí:** gắn → cuối (lớn nhất + 1); sắp lại nhận **đúng** danh sách mã ảnh đang hiện (không thiếu, không
  thừa, không trùng) → vị trí 1..n, ngược lại `ErrInvalidInput`; xóa mềm giữ vị trí cũ; khôi phục về vị trí cũ (thứ tự
  đọc: vị trí rồi mã ảnh). Tối đa 50 ảnh đang hiện mỗi mẫu (`ErrInvalidInput` khi vượt). Vì: rõ ràng, hoàn tác đúng
  chỗ cũ (TP06).
- **Q5 — Xóa ảnh đã xóa, khôi phục ảnh đang hiện:** không làm gì, trả ảnh hiện tại, không sự kiện, không audit
  (chạy lại an toàn).
- **Q6 — Trạng thái ảnh:** `ready` khi tệp có biến thể công khai của bộ kích thước chọn được; ngược lại `lost` (tệp
  bị đánh dấu mất, hay tệp bị xóa ở kho tệp) — không có `variants`. Vì: `PublicVariants` chỉ trả tệp ready chưa xóa;
  màn hình chỉ cần biết hiện ảnh được hay không (TP12).
- **Q7 — `DownloadURL` của ảnh standard (việc treo Task 43):** mục đích ảnh chế độ standard (`product_image`) ký tới
  `OriginalKey(content_sha256)` trên `crm-originals` (ảnh gốc bất biến), không còn ký tới `tmp/…` (tự xóa 24 giờ).
  Chưa ready → `ErrNotReady` như cũ. Task 321 thêm API, hạn 5 phút, tên tải về.
- **Q8 — Ảnh bìa trong `listProducts` (việc treo Task 39, 49):** trường tùy chọn `cover {imageId, status, variants}` =
  ảnh đang hiện có vị trí nhỏ nhất; `internal/queries` đọc mã ảnh, mã tệp từ `catalog.product_images_v`, handler gọi
  `files.PublicVariants` **một lần** cho cả trang rồi `files.SelectPreset`.
- **Q9 — `platform.file_variants_v` thêm `v.bucket = 'public'`** ngoài điều kiện của kế hoạch (thừa khi
  `mode = 'standard'`, nhưng giữ đúng luật "biến thể riêng tư không bao giờ có đường dẫn công khai" kể cả khi dữ liệu
  lệch), cùng điều kiện với `ListPublicVariants`.
- **Q10 — `IMAGE_PRESET_VERSION` đọc ở vai trò `api`** (mặc định 1, như worker) để `getProduct` chọn bộ kích thước.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước (Step 2) | `images_test.go`, `variants_view_test.go` | `go vet ./internal/modules/catalog/ ./internal/platform/files/` | `undefined: catalog.ImageView`, `undefined: files.SelectPreset` |
| KH02: 3 ảnh (một ảnh màu "Đỏ") đúng thứ tự, mỗi ảnh 12 đường dẫn `…/img/…/v1/`; sắp lại đúng; view 3 dòng; version 4; 5 `product.updated`; 4 audit; version cũ → `ErrVersionConflict` | `TestAttachImage_OrderAndColor` | `go test -race -count=1 -run Image -v ./internal/modules/catalog/` | `--- PASS: TestAttachImage_OrderAndColor` |
| Sắp lại thiếu, thừa, trùng, mã lạ, rỗng, kèm ảnh đã xóa → `ErrInvalidInput` | `TestReorderImages_Invalid` | như trên | PASS |
| Tệp `uploaded` → `ErrFileNotReady`; `catalog_import` → `ErrFileNotReady`; tenant khác, không có, đã xóa → `ErrNotFound`; màu lạ/rỗng → `ErrInvalidInput`; mẫu của người bán khác → `ErrNotFound` | `TestAttachImage_Refused` | như trên | PASS |
| Gắn lại tệp đang hiện → `ErrAlreadyExists`; tệp đã xóa → hồi dòng cũ ở cuối, màu mới (Q2) | `TestAttachImage_Again` | như trên | PASS |
| TP06: xóa → còn 2 (cả view); khôi phục → 3, về vị trí cũ; 31 ngày → `ErrRestoreExpired`; 2 audit; xóa/khôi phục lặp lại không đổi version | `TestImageSoftDeleteRestore` | như trên | PASS |
| TP12: ảnh `lost` → status lost, không variants; ảnh khác vẫn 12 | `TestProductImages_Lost` | như trên | PASS |
| 8 người sắp lại cùng version cùng lúc → đúng 1 thành công | `TestReorderImages_Concurrent` | như trên | PASS |
| `file_variants_v`: ready 12 dòng; cột `file_id, preset_version, width, format, key`; `security_invoker=true`; lost, MarkDeleted, private (R10), tenant B → 0 dòng | `TestFileVariantsView` | `go test -race -count=1 ./internal/platform/files/...` | `ok … 97.043s` |
| Chọn bộ kích thước: v1 đủ → 12; muốn 2 → lùi v1; thiếu → nil; rapid: không bao giờ bản > muốn, chỉ biến thể có thật | `TestSelectPreset` | như trên | PASS |
| Q7: `DownloadURL` ảnh sản phẩm ký tới `originals/orig/<sha>`, đọc đúng byte, không `/tmp/` | `TestDownloadURL_ProductImageSignsOriginal` | như trên | PASS |
| API: gắn 201 kèm images; 409 `file_not_ready`; 404 tệp tenant khác; 403 chỉ xem; 409 `version_conflict`; sắp lại 200, thiếu → 400 `invalid_input`; `listProducts` có `cover` ảnh đầu 12 biến thể; xóa 200, `getProduct` còn 1; khôi phục 200; 31 ngày → 410 `restore_expired`; tenant B xóa → 404; ảnh mất → 200, lost, không variants | `TestCatalogAPI_Images` | `go test -race -count=1 -run 'TestCatalogAPI\|TestListsAPI' ./internal/httpapi/` | `ok … 14.040s` |
| Q8: ảnh bìa = ảnh vị trí 1; mẫu không ảnh → nil; danh sách (cả có kho, lọc đủ) không Seq Scan với 30.000 ảnh | `TestProducts/ListColumns`, `TestPlansHaveNoSeqScan` | `go test -count=1 ./internal/queries/...` | lần đầu FAIL `Seq Scan` (bảng ảnh rỗng) → thêm ảnh vào dữ liệu đo → `ok … 140.756s` |
| Q10: `IMAGE_PRESET_VERSION` ở api mặc định 1, 0 → lỗi nêu tên | `TestLoadM1OptionalVariablesHaveDefaults`, `TestLoadM1BadValues/preset_0_ở_api` | `go test -count=1 ./internal/platform/config/` | đỏ trước (`nhận: <nil>`), sau sửa `ok` |
| Cổng kiểm toàn bộ, trên bản đã gộp main 291320c và đã stage | `make ci` | `make ci` | `EXIT 0`; 43 gói `ok` (catalog 62 s, files 152 s, httpapi 161 s, queries 157 s, tenancy 81 s, tests/arch, tests/e2e 67 s); `0 issues.`; `No vulnerabilities found.`; jscpd `Found 0 clones.`; gitleaks `no leaks found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `GetProduct` (hàm Go) không đổi chữ ký; ảnh đọc bằng `catalog.ProductImages` cùng tx, API `getProduct` trả `images`
  (Q1). Câu trả lời của `createProduct`, `updateProduct` và bốn operation ảnh cũng có `images` (bốn operation ảnh trả
  mẫu sau khi đổi, không phải một ảnh).
- `DeleteImage`, `RestoreImage` trả `(Image, error)`; `ReorderImages` trả `error` (kế hoạch không ghi kiểu trả về).
- Thêm `catalog.ResolveImages`, `files.SelectPreset`, gói `files/filestest`, câu sqlc `SetFileLost` (dùng trong
  `filestest`; Task 46 dùng lại khi đối chiếu) — kế hoạch không liệt kê.
- `platform.file_variants_v` thêm `v.bucket = 'public'` (Q9). Hai index mới trên `catalog.product_images`.
- Làm thêm hai việc treo được giao trong ghi chú: `DownloadURL` ảnh standard ký tới `orig/<sha>` (Q7, việc của Task 43);
  ảnh bìa trong `listProducts` (Q8, việc của Task 39, 49). Dữ liệu đo của `internal/queries` thêm 30.000 ảnh để
  `TestPlansHaveNoSeqScan` đo cả LATERAL ảnh bìa.
- `IMAGE_PRESET_VERSION` đọc ở vai trò `api` (Q10).

## Việc phát hiện thêm

- Màu của ảnh so với màu biến thể lúc gắn; đổi tên màu của biến thể sau đó không đổi màu đã ghi trên ảnh (ảnh giữ màu
  cũ, website không khớp). Màn Sản phẩm (Task 325) nên cho sửa màu ảnh, hoặc đổi màu biến thể kéo theo ảnh.
- Chưa có operation sửa màu của một ảnh đã gắn (kế hoạch không có); hiện phải xóa rồi gắn lại.
- Dọn tệp (Q16, mốc sau) phải xét `catalog.product_images` (cả dòng đã xóa mềm còn trong hạn 30 ngày) trước khi xóa tệp
  trên kho; index `product_images_file_idx` có sẵn cho việc đó.

## Reviewer

Lần 1 (bản stage trên main 41eb936): ĐẠT, không có phát hiện phải sửa. Ghi chú không bắt buộc: khôi phục về vị trí
cũ có thể trùng `position` với ảnh đã sắp lại (thứ tự đọc `(position, id)` vẫn xác định, lần sắp lại sau đánh số lại);
mẫu đủ 50 ảnh mà gắn lại tệp đang hiện nhận 400 thay vì 409 (chỉ khác mã lỗi). Không sửa.

Sau review lần 1: gỡ 4 đoạn trùng jscpd (`nextPosition`, `Writer.lockImage`, `withTwoImages`, `usePrivatePurpose`) và
gộp main tới 291320c (Task 31, 35).

Lần 2 (bản stage trên main 291320c): ĐẠT — gỡ trùng giữ nguyên hành vi, thứ tự khóa không đổi, gộp không mất phần nào
của main, không test nào bị nới.

Kết luận: ĐẠT
