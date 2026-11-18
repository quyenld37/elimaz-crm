# Task 320 — Ảnh trong bài viết sản phẩm (M1-44b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 320, M1-44b). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: chỉ **thêm** `catalog.ResolveArticleImages`, `catalog.ArticleHTML` (0 chỗ tham
  chiếu cũ). Không đổi chữ ký nào. Đổi hành vi của `Writer.CreateProduct`, `Writer.UpdateProduct` (khối `image` kiểm
  tệp): chỗ gọi là `internal/httpapi/handlers_catalog.go` (2), test của `catalog`, `httpapi`, `catalogtest`/`invtest`
  không tạo khối ảnh (grep `fileId` trong test: chỉ test ảnh sản phẩm, kho tệp, bộ dựng thuần — không qua đường này).
  `RenderArticleHTML`, `DecodeArticle` giữ nguyên.
- Gói phụ thuộc ngược (go list): `internal/httpapi`, `cmd/crm`, `internal/modules/order`, `internal/modules/inventory/invtest`
  nhập `catalog`; chỉ `httpapi` đổi mã (`productOut` nhận bài viết đã gắn ảnh).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. `getProduct`, `createProduct`, `updateProduct` trả
  `articleHtml` nay có `<figure><picture>` cho khối ảnh trỏ tệp sẵn sàng (trước đây luôn bỏ khối ảnh);
  `createProduct`/`updateProduct` trả 422 `invalid_article` cho khối ảnh trỏ tệp không hợp lệ (mã lỗi đã có). Chỉ sửa
  chữ mô tả của `articleHtml` nếu cần — không đổi lược đồ.
- Chỗ dùng trên màn hình: chưa có (trình soạn bài viết là Task M1-50d/326); website M5 sau này gọi `ArticleHTML`.
- Câu sqlc, bảng, pha migration: không có migration, không câu sqlc mới; dùng `files.Status` (GetFile) và
  `files.PublicVariants` (ListPublicVariants) đã có.
- Test sẽ chạy: `go test -race ./internal/modules/catalog/... ./internal/httpapi/...` (gói đổi + gói nhập), gồm
  `FuzzRenderArticleHTML` (hạt giống) và rapid 500 lượt; `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `files`
  (`PublicVariants`, `SelectPreset`, `Status`); tìm trong module `catalog` theo tên (`ResolveImages`, `ImageView`, `checkImageFile` của Task 44,
  `RenderArticleHTML`, `DecodeArticle` của Task 27); `filestest.Image/Uploaded/MarkLost` cho test.
- Dùng lại / viết mới vì: dùng lại `files.PublicVariants` (một câu SQL cho mọi tệp), `files.SelectPreset` (luật
  `IMAGE_PRESET_VERSION` chung với `GetProduct`), `catalog.ImageView` (BaseURL + Preset), `checkImageFile` (luật tệp
  ready + `product_image` + cùng tenant, như gắn ảnh sản phẩm), `RenderArticleHTML` (không đụng khuôn HTML). Viết mới
  chỉ `ResolveArticleImages` (gom `fileId` của khối, điền `Image`) và `ArticleHTML` (ghép 3 bước) vì kế hoạch đặt tên
  chúng; không gộp với `ResolveImages` vì đầu vào khác (khối bài viết vs ảnh sản phẩm) — phần chung (PublicVariants +
  SelectPreset + đổi kiểu) tách thành một hàm nhỏ dùng cho cả hai.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `catalog.products.article` thuộc catalog; tệp thuộc kho tệp (platform) — chỉ đọc.
- 5.2 Không ghi bảng module khác: không ghi bảng mới nào; vẫn chỉ `catalog.products`.
- 5.3 Đọc module khác qua public API: `files.Status`, `files.PublicVariants`; SQL của catalog không nhắc `platform.*`.
- 5.4 Nguyên tử: kiểm tệp của khối ảnh trong cùng transaction ghi mẫu (`WriteTx`); lỗi → lùi cả.
- 5.5 Sau commit: không đổi (vẫn `product.updated` qua outbox).
- 5.6 Gọi ngoài: không áp dụng — không gọi mạng; đường dẫn ghép từ `FILES_PUBLIC_BASE_URL` + khóa.
- 5.7 Tenant: mọi đọc trong tx tenant; RLS của `platform.files` làm tệp tenant khác như không có → `invalid_article`.
- 5.8 Không HTTP trong module: đúng.
- 5.9 Đường ghi: không thêm operation; `createProduct`/`updateProduct` đã có `x-permission`, `IdempotencyKey`.
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện mới: không có.
- 5.12 Module mẫu: theo catalog (sentinel `ErrInvalidArticle`, đổi HTTP ở `errors.go` đã có).

Mục 6:
- 6.1 Đồng thời: tệp có thể đổi trạng thái (mất, xóa) sau khi lưu bài viết — chấp nhận: lúc dựng HTML khối đó bị
  bỏ (R7, không ô vỡ). Không cần khóa `platform.files`.
- 6.2 Gửi lại: `Idempotency-Key` đã có; kiểm tệp không có tác dụng phụ.
- 6.3 Thứ tự khóa: `files.Status` chỉ đọc, không khóa; khóa mẫu như cũ.
- 6.4 Lỗi tạm: `WriteTx` chạy lại như cũ.
- 6.5 Cách ly: READ COMMITTED, đủ cho đọc trạng thái tệp.
- 6.6 Việc nền: không áp dụng.
- 6.7 429: không áp dụng.
- 6.8 Chết giữa chừng: transaction tự lùi.
- 6.9 Treo/tải: dựng HTML gọi `PublicVariants` đúng **một** lần khi có khối ảnh, **0** lần khi không có (D4-5); kiểm
  tệp lúc lưu một `files.Status` mỗi `fileId` khác nhau **mới** (≤ 200 khối, chỉ lúc ghi).
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: chống chèn mã của bộ dựng giữ nguyên (fuzz, rapid của Task 27 chạy lại); ảnh chỉ thêm đường dẫn do
  máy chủ ghép, vẫn qua `safeImageURL`.
- 6.12 Dữ liệu cá nhân: không log; lỗi không mang nội dung bài viết.
- 6.13 Quyền: đọc `catalog.read`, ghi `catalog.manage` như cũ; ảnh riêng tư (R10) không bao giờ có đường dẫn: chỉ nhận
  `product_image` lúc lưu, `PublicVariants` lọc mục đích `private`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Khối ảnh trỏ tệp đang xử lý, `catalog_import`, đã xóa, tenant khác, không có → `ErrInvalidArticle` (tạo và sửa); tệp ready → lưu được; giữ khối cũ có tệp mất → lưu được | `TestArticleImages_SaveChecksFile` | `go test -race ./internal/modules/catalog/ -run 'Article'` | `ok … catalog 7.4s` (trước khi viết mã: build FAIL `undefined: catalog.ArticleHTML`) |
| API: tệp đang xử lý → 422 `invalid_article`; `createProduct`, `getProduct` trả `articleHtml` có một `<picture>`, `src` = `FILES_PUBLIC_BASE_URL/img/…` | `TestCatalogAPI_ArticleImages` | `go test -race ./internal/httpapi/ -run TestCatalogAPI` | `ok … httpapi 24.7s` (trước khi sửa handler: FAIL "articleHtml của createProduct thiếu ảnh: <p>Lụa</p>") |
| R7: đúng một `<picture>`, mọi `src`/`srcset` khớp `^base/img/<sha64>/v1/`; tệp lost → HTML bằng từng byte bản không có khối ảnh; `IMAGE_PRESET_VERSION=2` → như v1; bài viết sai → `ErrInvalidArticle` | `TestArticleHTML_WithImages` | như trên | ok |
| D4-5: 3 khối ảnh → `PublicVariants` đúng 1 lần (đếm câu `ListPublicVariants` qua tx bọc); không khối ảnh, rỗng, `[]` → 0 lần ở cả `ArticleHTML` và `ResolveArticleImages` | `TestArticleHTML_PublicVariantsCalls` | như trên | ok |
| Bộ dựng của Task 27 không đổi (golden, chống chèn mã, rapid 500 lượt, fuzz) | `TestRenderArticleHTML_*`, `FuzzRenderArticleHTML` | `go test -run XXX -fuzz FuzzRenderArticleHTML -fuzztime 20s ./internal/modules/catalog/` | `PASS` (453 171 lượt) |
| Ảnh sản phẩm (Task 44) không đổi sau khi tách `publicImageVariants` | `TestAttachImage_*`, `TestImageSoftDeleteRestore`, `TestProductImages_Lost`, `TestCatalogAPI_Images` | `make ci` | `ok … catalog 150.8s`, `ok … httpapi 319.8s` |
| Cổng kiểm trên bản đã gộp main `53a9941` (Task 50) | build, vet, golangci-lint, gen-check, sqlc vet, `go test -race -count=1 ./...`, vuln, oasdiff, catalog-check, jscpd, test-guard, gitleaks, console-check | các bước của `make ci` chạy lần lượt (test với `-timeout 40m` vì máy tải 18–26) | `0 issues`; `ok … catalog 336.7s`, `ok … tests/e2e 349.6s`; `No vulnerabilities found`; `Found 0 clones`; `no leaks found`; test-guard sạch. 4 test nhạy thời gian ở gói không đổi hỏng khi tải cao: `TestIdempotencyReplayExactBytes` (httpapi), `TestReconcile_LargeTenant` (inventory), `TestStrictDeadBlocksAndAlerts` (queue), `TestStreamBatchDedupe` (realtime) → chạy lại riêng `-count=1`/`-count=3`: đều `ok` |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

- **Q1 — `ResolveArticleImages`, `ArticleHTML` nhận `view ImageView` thay cho `baseURL string`.** Vì: kế hoạch đòi
  "chọn bản bộ kích thước như `GetProduct`" và test `IMAGE_PRESET_VERSION=2 → v1`, mà chỉ có `baseURL` thì không biết
  bản muốn; `ImageView{BaseURL, Preset}` của Task 44 mang đúng hai giá trị đó. Phương án khác: thêm tham số `preset
  int` (dài chữ ký, lặp kiểu đã có). Đảo ngược dễ: chỉ một chỗ gọi trong mã (handler).
- **Q2 — Kiểm tệp của khối ảnh nằm trong `catalog` (`checkArticleFiles`, gọi từ `CreateProduct`/`UpdateProduct`),
  không trong `internal/article/validate.go`.** Vì: `article` là gói thuần (không tx), `Validate`/`Check` còn được bộ
  dựng HTML gọi lại; kiểm tệp cần tx tenant. Dùng lại `checkImageFile` của ảnh sản phẩm (cùng luật: có trong tenant,
  chưa xóa, `ready`, `product_image`). Phương án khác: truyền một hàm kiểm vào `Validate` (làm gói thuần phụ thuộc DB).
- **Q3 — Chỉ kiểm tệp **mới** so với bài viết đang lưu.** Sửa mẫu (tên, hay bài viết vẫn giữ khối ảnh cũ) khi tệp của
  khối cũ đã bị mất/xóa sau đó vẫn lưu được; khối đó bị bỏ khi dựng HTML (website không ô vỡ). Vì: tệp mất không phải
  lỗi của nhân viên, không nên chặn mọi lần sửa. Phương án khác: kiểm mọi khối mỗi lần lưu bài viết (buộc xóa khối ảnh
  mất trước khi sửa chữ). Sửa tên/mô tả/trạng thái không kiểm tệp.
- **Q4 — Câu trả lời của `createProduct` cũng có ảnh trong bài viết:** handler tạo mẫu nay trả
  `productWithImages` (đọc lại mẫu trong cùng transaction) thay cho `productOut(p, nil)`, để `articleHtml` của tạo, sửa,
  đọc giống nhau. Phương án khác: gọi `ResolveArticleImages` riêng trong handler tạo (jscpd báo trùng).

## Khác kế hoạch

- Chữ ký `ResolveArticleImages`/`ArticleHTML` nhận `ImageView` (Q1).
- `validate.go` không gọi `files.Status` (Q2): kiểm tệp ở `catalog.checkArticleFiles`; `validate.go`, `article.go` chỉ
  sửa chú thích.
- `getProduct` dùng `ResolveArticleImages` + `RenderArticleHTML` trên khối đã đọc (đúng hai bước sau của `ArticleHTML`;
  bước `DecodeArticle` đã chạy trong `GetProduct`), không mã hóa lại khối thành JSON để gọi `ArticleHTML`. Test API so
  `articleHtml` của tạo và đọc; test module kiểm `ArticleHTML`.
- Phần chung đọc + chọn bộ kích thước tách thành `publicImageVariants` (images.go), `ResolveImages` của Task 44 dùng lại.

## Việc phát hiện thêm

- `internal/imager` `TestImager_HangKilledWithinDeadline` hỏng một lần trong `make ci` khi máy tải nặng (nhiều phiên
  chạy song song): "script giả chưa chạy: stat …/pid". Chạy riêng `go test -race -count=3 ./internal/imager/` → ok.
  Không đụng gói này; nên xem lại thời gian chờ của test (việc của task khác).
- Cùng loại, khi máy tải 20+ (nhiều phiên song song): `TestIdempotencyReplayExactBytes`, `TestReconcile_LargeTenant`,
  `TestStrictDeadBlocksAndAlerts`, `TestStreamBatchDedupe`, `TestIngestFakeCarrierOnlyOutsideProduction`,
  `TestBinaryWorkerHandlesSelftest` hỏng vì hết hạn; chạy riêng đều qua.
- Mô tả `articleHtml` trong `openapi.yaml` chưa nói HTML có ảnh của khối image (câu hiện tại không sai; đổi cần
  `make gen`) — để task chạm hợp đồng sản phẩm sau (M1-50d/326).

## Reviewer

Kết luận: **ĐẠT** (hai lượt).

- Lượt 1 (nền `37a25a1`): ĐẠT, không có phát hiện phải sửa. Ghi chú không bắt buộc: Q3 an toàn vì M1 chưa deploy (bài
  viết lưu từ Task 27 chưa kiểm `fileId` sẽ không bị kiểm lại, lúc dựng ảnh riêng tư vẫn bị lọc); chú thích test nói
  "sửa tên" mà chỉ thử sửa bài viết (sửa tên không gọi `checkArticleFiles`, rõ khi đọc mã); mô tả `articleHtml` trong
  OpenAPI chưa nói có ảnh (ghi vào Việc phát hiện thêm); mục Tái dùng ghi sai nguồn danh mục (đã sửa chữ).
- Lượt 2 (sau khi gộp main `53a9941`, cùng mã): ĐẠT, không có phát hiện phải sửa. D4-5, R7, R10, kiểm tệp khi lưu
  đúng; `productOut` chỉ còn gọi từ `productWithImages` nên không đường nào dựng `articleHtml` thiếu bước gắn ảnh;
  phần mới của main (Task 33, 36, 50) không dựng `articleHtml` hay tạo khối `image` ở chỗ khác.
