# Task 453 — Module content: banner trang chủ, ảnh banner qua kho tệp CRM

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 453, M5-41; làn L1 sau M5-15,
M5-56). Loại việc (quy trình mục 3): A

Nguồn đã đọc (Step 0): website `apps/storefront-bct/src/server/banners/{rules,images}.ts` (bản mới nhất), admin
`apps/admin-camplink/src/lib/banners.mjs`, `components/Banners.tsx`. Giới hạn chép: tối đa 8 banner; kicker 40, tiêu đề
60, nút 30, mô tả ảnh 120 ký tự; ghim ≤ 3; loại `campaign`/`trending`/`new`/`category`/`image`; màu
`auto`/`red`/`black`/`light`; đích ảnh `campaign`/`category`/`product`/`offers`; ảnh JPEG/PNG/WebP ≤ 8 MiB, ≤ 40 MP,
rộng ≥ 600, WebP q82 rộng 2400 và 1200, không phóng to, bỏ metadata, xoay theo EXIF. 9 lý do ẩn (đúng tên website):
`disabled`, `scheduled`, `expired`, `campaign-missing`, `campaign-not-running`, `category-missing`, `no-products`,
`image-missing`, `product-missing`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chủ yếu **thêm**; không đổi chữ ký có sẵn.
  - Module mới `internal/modules/content` (gói gốc): kiểu `Banner`, `BannerSet`, `BannerChange`, `RefData`,
    `CampaignRef`, `CategoryRef`, `ProductRef`, `Status`, `Picture`, `Writer`, `Actor`, `BannersChanged`; hàm
    `GetBanners`, `LoadRefData`, `NormalizeBanners`, `ValidateBanners`, `Visibility`, `Statuses`, `NeedsSold30`,
    `BannerPictures`, `HiddenReasons`, `BannerPurpose`, `RegisterFilePurposes`; phương thức `Writer.SaveBanners`; hằng
    giới hạn, loại, màu, đích, lý do ẩn, `TopicBannersChanged` (`content.banners.changed`), `BannerPurposeKey`
    (`web_banner`); sentinel trong `errors.go`. Trạng thái ảnh dùng `files.FileStatus` có sẵn.
  - `internal/platform/files` (vùng rủi ro cao): thêm trường `Purpose.Preset` (0 = bộ hiện hành IMAGE_PRESET_VERSION,
    giữ nguyên cho mọi mục đích có sẵn), `Purpose.MinWidth` (0 = không kiểm), `VariantSpec.Quality` (0 = mặc định của
    imager như cũ); hằng `PresetBannerV1 = 1001` và dòng của nó trong bảng `presets` (1200, 2400 × WebP q82, JPEG q82).
    `SelectPreset` (xuất ra): bộ ≥ 1000 không lùi về bộ chung (mọi nơi gọi có sẵn truyền bộ < 1000, không đổi).
    Sửa trong gói (không xuất): `samePurpose`, `validate`, `layoutOf`/`readManifest`/`assemble` (bản theo mục đích),
    `findDuplicate` (bản theo mục đích), `contentReason` (rộng < MinWidth → `too_small`), `contentReject`, `output`
    (gửi Quality), `Regenerator.Execute` (bỏ qua tệp mà bản yêu cầu không phải bản của mục đích nó). `Preset(1)` và
    mọi ảnh sản phẩm không đổi.
  - `internal/imager` (vùng rủi ro cao): thêm `Request.MinWidth` (`minWidth`, omitempty) và `ReasonTooSmall`
    (`too_small`): kiểm sau `vipsheader`, trước khi giải mã. Yêu cầu cũ không có trường → không kiểm (tương thích).
  - `internal/platform/cache`: vùng mới `SpaceBanners` (`banners`) trong `Spaces()` (12 → 13).
  - `internal/wiring/webcache`: `EntityHomeBanners`, chủ đề `content.TopicBannersChanged` trong `Topics()`; ánh xạ
    `home_banners` → làm mới `SpaceBanners`; `web_campaign` làm mới thêm `SpaceBanners` (banner chiến dịch ẩn/hiện theo
    chiến dịch).
  - `identity`: quyền `content.read`, `content.manage` (ScopeShop) + nhãn (`roles.go`, nhóm có sẵn "Khuyến mại…", Q10);
    owner, manager nhận qua `defaultRoles()`.
  - `httpapi`: handler ogen `GetBanners`, `PreviewBanners`, `SaveBanners`, `WebHomeBanners`; `errors_content.go`
    (`contentError` vào `domainErrorMappers`); `web.(*Catalog).HomeBanners`, `web.LoadBannerRefs` (đọc bán 30 ngày rồi
    `content.LoadRefData`, dùng chung cho website và màn nhân viên).
  - `cmd/crm`: gọi `content.RegisterFilePurposes()`.
  - Test có sẵn thêm dòng (không nới): `identity` `TestPermissionsList`; `webcache` `TestConsumer_NameTopics`,
    `TestInvalidator_*` (số vùng); `httpapi` `errors_snapshot_test.go`; `tests/arch` `moduleCalls` (content →
    catalog, promotion), `mappedModules` (+content), `TestLockOrderGroupCodes` (+`12d`).
- Gói phụ thuộc ngược (go list): `platform/files` ← `cmd/crm`, `httpapi`, `cash`, `cash/internal/statement`, `catalog`,
  `customer`, `fulfillment`, `order`, `files/filestest`, `queries/webcatalog`; `imager` ← `cmd/crm`, `cmd/imager`,
  `platform/files`, `testkit`; `platform/cache` ← `cmd/crm`, `httpapi`, `httpapi/web`, `observability`,
  `wiring/webcache`; `identity` ← nhiều gói (chỉ thêm quyền). `content` mới ← `httpapi`, `httpapi/web`,
  `wiring/webcache`, `cmd/crm`.
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm. Nhân viên: `getBanners` `GET /api/v1/content/banners`
  (`content.read`), `previewBanners` `POST /api/v1/content/banners/preview` (`content.read`, `x-idempotency: exempt`),
  `saveBanners` `PUT /api/v1/content/banners` (`content.manage`, `IdempotencyKey`). Website: `webHomeBanners`
  `GET /api/v1/web/content/banners` (`x-auth: service_key`, `web.catalog.read`, ETag/304). Mã lỗi mới trong
  `x-error-codes` (câu tiếng Việt trong `console/src/app/errors.ts`).
- Chỗ dùng trên màn hình: màn banner là Task 208 (song song, không làm ở đây); `console/src/api/**` sinh lại bằng
  `make gen`; thêm câu lỗi vào `errors.ts`.
- Câu sqlc, bảng, pha migration: `migrations/0521_content_banners.sql` (pha mở rộng: CREATE SCHEMA, TABLE, RLS, GRANT,
  dòng `platform.file_purposes` `web_banner`). Bảng `content.home_banner_sets`, `content.home_banners`. Khối sqlc mới
  `content` (`queries/content/banners.sql` → `internal/modules/content/internal/store`); sqlc sinh lại `models.go` của
  mọi gói store (thêm bảng). Đọc view: `catalog.products_v`, `variants_v`, `categories_v`, `sellers_v`,
  `product_images_v`, `platform.file_variants_v`, `promotion.web_campaigns_v`, `web_campaign_targets_v`.
- Test sẽ chạy: `content` (cả gói, mới); `platform/files` (cả bộ — vùng rủi ro cao); `imager` (cả gói; ca libvips thật
  bỏ qua trên máy dev); `httpapi -run 'Banner|ErrorSnapshot|ErrorCodes'`; `httpapi/web -run 'Banner'`;
  `wiring/webcache`; `platform/cache -run Spaces`; `identity -run 'Permission'`; `tests/arch`; console `errors.test.ts`;
  cả bộ ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `files.Status`, `files.PublicVariants`,
  `files.SelectPreset`, `files.RegisterPurpose` (mẫu `order.RequestPhotoPurpose`); `refusableWrite`, `h.audit`,
  `readAsDefaultSeller` (httpapi); `events.Registry.Emit`, `realtime.Bus.Publish` (mẫu `promotion.campaignChanged`);
  `cache.ReadCache.GetOrLoad` + `etag`/`matches` của `httpapi/web` (mẫu `web.Campaigns`); `webcatalog.Sold30` (bán 30
  ngày, đã có); mẫu "một dòng giữ version cả danh sách" của `order.SetWebIntake` (INSERT … ON CONFLICT DO NOTHING rồi
  FOR UPDATE).
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên. Viết mới luật banner (`ValidateBanners`, `Visibility`) trong
  `content` vì là nghiệp vụ riêng của banner (chép từ website). Trạng thái chiến dịch "đang chạy" tính trong `content` từ
  `promotion.web_campaigns_v` (state + giờ) thay vì nhập Go `promotion.Running` — `content` chỉ đọc view (kế hoạch);
  cùng một dòng điều kiện, có test. Bộ kích thước banner: thêm vào bảng `presets` có sẵn của `files` (không làm bộ xử lý
  ảnh thứ hai).

## Soát thiết kế

- 5.1 Chủ dữ liệu: banner thuộc module mới `content` (kế hoạch, bảng "Sở hữu" M5), schema `content`.
- 5.2 Không ghi bảng module khác: chỉ ghi `content.*`; audit `content.banners.save` qua `identity.RecordAudit` ở lớp
  HTTP; tệp ảnh do đường tải tệp của M1 (`files.upload`) ghi.
- 5.3 Đọc module khác: chỉ view công bố của `catalog`, `promotion`, `platform` (sqlc); `content` không nhập Go
  `order`, `promotion`; "bán 30 ngày" (view `ord`) do lớp HTTP đọc qua `webcatalog.Sold30` rồi truyền vào `LoadRefData`.
- 5.4 Nguyên tử: thay danh sách + `revision` + sự kiện + báo hiệu + audit + kết quả chống trùng trong một transaction.
- 5.5 Không nguyên tử: bộ đệm website làm mới qua sự kiện → `webcache` sau commit; hạn sống 60 s cho đổi theo giờ.
- 5.6 Gọi ra ngoài: không (ảnh qua kho tệp CRM, imager là dịch vụ nội bộ có sẵn).
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; hai bảng có `tenant_id`, RLS, khóa ngoại `platform.tenants`.
- 5.8 Không biết HTTP: `content` không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi: `saveBanners` có `IdempotencyKey` + `content.manage`; `previewBanners` `exempt` (chỉ tính, không ghi).
- 5.10 PostgreSQL là nguồn sự thật; Valkey chỉ giữ bản trả website 60 s.
- 5.11 Không thêm thư viện.
- 5.12 Theo module mẫu: gói gốc công khai, store riêng `internal/store`, sentinel ở `content/errors.go`, đổi HTTP ở
  `errors_content.go`, test `_test` trên PostgreSQL thật, `t.Parallel()`.
- 6.1 Hai người cùng sửa: khóa dòng `home_banner_sets` (FOR UPDATE) rồi so `revision` → 409 `version_conflict`.
  Dòng chưa có: INSERT … ON CONFLICT DO NOTHING rồi khóa (revision 0).
- 6.2 Gửi lại: `Idempotency-Key`; test 50 lần lưu song song cùng khóa → một lần đổi, một audit, một sự kiện.
- 6.3 Thứ tự khóa: nhóm mới **12d** `content` (`home_banner_sets → home_banners → look_sets → looks → look_items`), chèn
  vào ADR-46 sau 12c; đường lưu khóa `home_banner_sets` rồi xóa/chèn `home_banners`, rồi 13 (khóa chống trùng, jobs).
  Đọc `platform.files` qua `files.Status` không khóa (không chạm 12b).
- 6.4 Lỗi tạm: `refusableWrite` chạy lại cả transaction; hàm lưu gán lại biến kết quả.
- 6.5 Cách ly: READ COMMITTED; kiểm ảnh, chiến dịch, sản phẩm lúc lưu là ảnh chụp lúc đó — đổi sau đó chỉ làm banner ẩn
  (lý do hiện cho nhân viên), không làm dữ liệu sai.
- 6.6–6.8: không áp dụng (không việc nền mới; ảnh dùng lệnh `file.process` có sẵn).
- 6.9 Treo lâu: giới hạn câu lệnh mặc định của `db`.
- 6.10 Tiền: không áp dụng (banner không mang giá; website tính giá từ chiến dịch).
- 6.11 Quy luật: test bảng cho từng luật và từng lý do ẩn.
- 6.12 Dữ liệu cá nhân: không có (banner là nội dung shop).
- 6.13 Quyền: đọc `content.read`, ghi `content.manage` (toàn shop, owner, manager); website `web.catalog.read`; test
  403, tenant khác.

## Quyết định thay chủ shop

- **Q1 — Bộ kích thước riêng theo mục đích (`Purpose.Preset`), đánh số từ 1000.** Bộ `PresetBannerV1 = 1001` (1200,
  2400 × WebP, JPEG, chất lượng 82) như Step 0 của kế hoạch chọn; số ≥ 1000 để không lẫn IMAGE_PRESET_VERSION (1, 2…).
  Tạo lại kích thước bộ chung bỏ qua tệp banner và ngược lại (không trộn cỡ ảnh sản phẩm vào banner). Phương án khác:
  thêm cỡ 1200/2400 vào bộ chung v2 — đổi mọi ảnh sản phẩm, trái "không đổi bộ của ảnh sản phẩm".
- **Q2 — Rộng ≥ 600 kiểm ở cả imager và files.** Imager nhận `minWidth` (omitempty) và từ chối `too_small` trước khi
  giải mã; files kiểm lại kết quả (imager cũ chưa triển khai lại vẫn ra đúng). Phương án khác: chỉ kiểm ở files — imager
  phải giải mã và ghi biến thể của ảnh sẽ bị từ chối.
- **Q3 — Chuẩn hóa như admin cũ thay vì từ chối trường thừa.** Trường không thuộc loại bị bỏ khi lưu (banner ảnh không
  chữ, màu, ghim; loại khác không ảnh, đích) — đúng `bannerPayloadOne` của admin, màn 208 không phải xóa tay.
- **Q4 — Chiến dịch, danh mục, mẫu đích không còn vẫn lưu được** (banner ẩn với lý do `campaign-missing`,
  `category-missing`, `product-missing` như website); chỉ chặn khi lưu: ghim ngoài nguồn, ảnh bị từ chối / không có / đã
  xóa / sai mục đích / của tenant khác. Ảnh đang xử lý lưu được (ẩn `image-missing` tới khi xong).
- **Q5 — Phạm vi chiến dịch theo danh mục so đúng mã** (như giá chiến dịch của `promotion`, website `campaignCovers`),
  còn banner danh mục gồm danh mục con (như `categoryScope` của website).
- **Q6 — "Mẫu hiện được"** (nguồn của banner, đích sản phẩm): mẫu active, chưa xóa, người bán hoạt động, có biến thể bán
  được giá > 0 và có ảnh đã xử lý — như `item()` của website. Giá chiến dịch (≤ 0 hay ≥ niêm yết) không xét ở CRM: website
  vẫn tự lọc khi dựng banner.
- **Q7 — Website nhận banner đang hiện + mã nguồn (chiến dịch, danh mục, ghim) + đường dẫn ảnh; mẫu và giá trên banner
  website tự lấy** từ API danh mục, chiến dịch đã có (như `resolveBanners` cũ). Bộ đệm 60 s, làm mới theo
  `content.banners.changed` và `promotion.web_campaign.changed`.
- **Q8 — `getBanners` không trả mọi sản phẩm chọn được**, chỉ mẫu danh sách nhắc (ghim, đích) kèm `showable`; chiến
  dịch (kèm phạm vi, đích, đang chạy) và danh mục trả đủ. Màn 208 chọn mẫu bằng tìm sản phẩm có sẵn, lọc theo phạm vi
  chiến dịch/danh mục trả kèm. Lý do: shop có tới hàng chục nghìn mẫu.
- **Q9 — Mã lỗi:** mỗi luật một mã 422 (`too_many_banners`, `banner_campaign_required`, `banner_category_required`,
  `banner_image_required`, `banner_window_invalid`, `too_many_pins`, `pin_not_in_source`, `banner_image_invalid`,
  `invalid_banner` cho chữ quá dài / loại, màu, đích lạ / trùng); ảnh bị từ chối dùng `file_rejected` có sẵn (hằng dời
  sang `errors.go` vì nay hai vùng dùng; câu tiếng Việt mở rộng thêm "quá nhỏ"). Hợp đồng không đặt `maxItems: 8`,
  `maxLength` sát luật để ogen không trả 400 trước (kế hoạch đòi 422).
- **Q10 — Quyền:** `content.read`, `content.manage` nhóm "Khuyến mại…" của màn Vai trò (không thêm nhóm mới).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 9 banner → 422; mỗi loại thiếu trường bắt buộc → đúng mã; ghim 4 → 422; ghim ngoài nguồn; khung giờ ngược/bằng; chữ quá dài; ảnh bị từ chối, đã xóa, sai mục đích, không có | `TestValidateBanners` (28 ca con) | `go test -race -count=1 ./internal/modules/content/` | `ok … 1.9s` |
| Mỗi lý do ẩn một ca (9/9, test tự đếm đủ `HiddenReasons()`), ca hiện, đúng giờ bắt đầu/kết thúc | `TestVisibility`, `TestCampaignRunning` | như trên | `ok` |
| Chuẩn hóa như admin cũ (bỏ trường không thuộc loại, cắt khoảng trắng, màu auto), không sửa bản gốc | `TestNormalizeBanners` | như trên | `ok` |
| Lưu thay cả danh sách theo revision; revision cũ → `ErrRevisionConflict`; mỗi lần lưu một sự kiện `content.banners.changed` Seq = revision | `TestSaveBanners_ReplaceByRevision` | như trên | `ok` |
| Kiểm khi lưu trên dữ liệu thật: ảnh rejected → `ErrFileRejected`, ảnh tenant khác → `ErrImageInvalid`, ghim ngoài danh mục, 9 banner; lần hỏng không ghi | `TestSaveBanners_ChecksReferences` | như trên | `ok` |
| Nguồn banner trên PostgreSQL: danh mục cha gồm con, mẫu không ảnh/không giá không tính, bán chạy theo sold30, chiến dịch đã xóa → `campaign-missing`, tạm dừng → `campaign-not-running` | `TestLoadRefData_PoolAndVisibility` | như trên | `ok` |
| Tenant khác riêng biệt | `TestBanners_OtherTenant`, `TestBannersAPI_OtherTenant` (ảnh của A dùng ở B → 422 `banner_image_invalid`) | như trên; `go test -race -count=1 -run '^TestBannersAPI_' ./internal/httpapi/` | `ok … 2.5s` |
| API: sale 403; mỗi luật → 422 đúng mã (10 ca, gồm `file_rejected`); lưu → revision 1, trạng thái hiện/ẩn có lý do, URL ảnh `/v1001/2400.webp`, `/1200.webp`; revision cũ 409; xem trước không ghi, cũng kiểm luật; một audit `content.banners.save` | `TestBannersAPI_SaveRulesAndRead` | như dòng trên | `ok` |
| 50 lần lưu song song cùng khóa → một lần (revision 1), một audit, một sự kiện, mọi 200 cùng thân | `TestBannersAPI_ConcurrentSameKey` | như dòng trên | `ok` |
| `webHomeBanners`: không banner tắt, hết giờ, chưa tới giờ, ảnh đang xử lý, nguồn không mẫu; đúng thứ tự; URL ảnh bộ banner-v1, ảnh điện thoại riêng hoặc dùng ảnh máy tính nhỏ; ETag → 304; TTL 60 s; khóa thiếu quyền 403 | `TestWebHomeBanners` | `go test -race -count=1 -run '^TestWebHomeBanners$' ./internal/httpapi/web/` | `ok … 2.0s` |
| Ảnh banner: bộ 1001 = 1200/2400 × WebP, JPEG q82, imager nhận MinWidth 600; ảnh sản phẩm cùng worker vẫn v1 (12 biến thể); ảnh trùng dùng lại theo bộ của mục đích | `TestFileProcess_PurposePreset` | `go test -race -count=1 -run '^(TestFileProcess_PurposePreset|TestFileProcess_TooSmall|TestRegenerate_SkipsPurposeWithOwnPreset|TestPurpose_PresetRules)$' ./internal/platform/files/` | `ok … 2.2s` |
| Ảnh rộng < 600 → rejected `too_small` (imager mới từ chối, hay imager cũ trả 500 px thì files tự từ chối, không biến thể); ảnh sản phẩm 500 px vẫn ready | `TestFileProcess_TooSmall` | như trên | `ok` |
| Tạo lại bộ chung không chạm ảnh banner; luật khai `Preset` ≥ 1000, `MinWidth` ≥ 0, khai lại khác → panic | `TestRegenerate_SkipsPurposeWithOwnPreset`, `TestPurpose_PresetRules` | như trên | `ok` |
| Imager: rộng < MinWidth → `too_small` trước khi giải mã (vips 0 lần); đúng ngưỡng nhận; MinWidth 0 không kiểm; MinWidth âm là yêu cầu sai. Không phóng to: `TestImager_NoUpscale` có sẵn (libvips thật, chạy ở `make test-imager`, bỏ qua trên máy dev) | `TestImager_MinWidth` | `go test -count=1 -run '^TestImager_MinWidth$' ./internal/imager/` | `ok` |
| Sổ mục đích Go khớp bảng (dòng `web_banner` của 0521) | `TestPurposes_GoMatchesTable` | `go test -count=1 -run '^TestPurposes_GoMatchesTable$' ./internal/platform/files/` | `ok` |
| Mã lỗi mới đổi đúng HTTP | `TestDomainErrorSnapshot` (11 dòng mới) | `go test -count=1 -run 'ErrorSnapshot|ErrorCodes|DomainError' ./internal/httpapi/` | `ok` |
| Sự kiện → làm mới vùng `banners`; chiến dịch đổi cũng làm mới `banners`; 13 vùng | `TestConsumer_Payloads`, `TestConsumer_NameTopics`, `TestInvalidator_Mapping`, `TestInvalidator_SystemResync` | `go test -race -count=1 ./internal/wiring/webcache/` | `ok` |
| Quyền mới toàn shop khớp `docs/quyen.md` | `TestPermissionsList`, `TestPermissionsDoc_MatchesCode` | `go test -count=1 -run 'Permission|Doc' ./internal/modules/identity/` | `ok` |
| Câu lỗi màn hình cho mã mới | `errors.test.ts` | `npx vitest run src/app/errors.test.ts` | `17 passed` |
| Luật kiến trúc (ranh giới content, nhóm khóa 12d, lỗi có ánh xạ, SQL chỉ view công bố) | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok … 40.6s` |
| Cổng kiểm của task | (cả bộ) | `ci-task.sh` (sau `git merge main`: Already up to date, main = 99e2d69) | Lần 1 đỏ gen-check, catalog-check (chưa stage mã sinh), prettier `errors.ts` — đã sửa. Lần 2: mọi cổng tĩnh xanh; mọi gói xanh (`content`, `files` 72 s, `httpapi` 33 s, `httpapi/web`, `imager`, `identity`, `webcache`, gói phụ thuộc gồm `order`, `catalog`, `inventory`…) trừ `platform/cache` `TestConn_OpTimeout` (126 ms > 80 ms, test đo giờ thật lúc máy tải; gói chỉ thêm một hằng). Chạy lại: test đó 3/3 `ok`, cả gói `go test -race -count=1 ./internal/platform/cache/` `ok … 9.0s` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 453 không có test e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ca "tenant khác 404": danh sách banner là một bộ mỗi tenant, không có mã trên đường dẫn; kiểm bằng "B thấy rỗng, ảnh của
  A dùng ở B → 422 `banner_image_invalid`, lưu của B không đổi A".
- Ca "ảnh 1000 px không phóng to" dựa vào `TestImager_NoUpscale` có sẵn (imager `--size down`, chỉ chạy được với
  libvips thật); task này không đổi cách tạo biến thể.
- Thêm ngoài Files của kế hoạch: `imager` (`MinWidth`, `too_small` — Q2), `files/filestest.Image` tạo biến thể theo bộ của
  mục đích, vùng `cache.SpaceBanners` + ánh xạ `webcache` (cho hạn sống 60 s và làm mới theo sự kiện), câu lỗi
  `console/src/app/errors.ts`, `tests/arch/errors_mapped_test.go` (+content), hằng `codeFileRejected` dời từ
  `errors_web.go` sang `errors.go` (hai vùng dùng), `TestConsumer_Payloads` tách bảng ca ra hàm (funlen khi thêm ca).

## Việc phát hiện thêm

- `internal/platform/cache` `TestConn_OpTimeout` so thời gian chờ thật với ngưỡng 80 ms: đỏ một lần ở ci-task khi máy
  chạy nhiều phiên (126 ms), chạy lại xanh. Ứng viên cho đợt sửa test chập chờn (luật Task 400: không lấy giờ thật làm
  tiêu chí). Không sửa ở đây.
- (Reviewer) Imager so `MinWidth` với chiều rộng đọc từ đầu tệp, trước khi xoay theo EXIF: ảnh lưu dọc nhưng hiện
  ngang (hoặc ngược lại) quanh ngưỡng 600 px có thể bị từ chối nhầm / nhận nhầm. Hiếm; sửa cần imager đọc hướng EXIF.
- (Reviewer) `EnqueueRegenerate` bộ chung mới vẫn xếp lệnh cho tệp banner rồi bỏ qua (đúng, tốn một lệnh mỗi tệp).
- Website (Task 464/459) đọc `webHomeBanners` và tự dựng mẫu, giá cho banner như `resolveBanners` cũ; màn 208 chọn mẫu
  ghim bằng tìm sản phẩm có sẵn (Q8).

## Reviewer

Kết luận: ĐẠT (lần 1, agent reviewer trên `git diff --cached` sau `git merge main` — Already up to date, 99e2d69).

- Phát hiện phải sửa: không có. Đã soi: thứ tự khóa 12d và chạy lại an toàn của `SaveBanners`; migration chỉ thêm, RLS,
  GRANT tối thiểu; vùng rủi ro cao `files` (bộ theo mục đích không đổi ảnh sản phẩm, `SelectPreset` không lùi từ bộ ≥
  1000, tìm trùng theo bộ của mục đích, tạo lại bỏ qua bộ không thuộc mục đích, `MinWidth = 0` không đổi gì); imager
  `MinWidth` trước khi giải mã, tương thích yêu cầu cũ; ranh giới `content` (không `net/http`, không `order`,
  `promotion`); lỗi đổi ở `errors_content.go`; mã sinh khớp nguồn.
- Ảnh hưởng: lệch tài liệu (`ImageRef` không có, nhóm quyền, thiếu `web.LoadBannerRefs`, hành vi `SelectPreset`) — đã sửa
  mục Ảnh hưởng. Bảng bằng chứng: mọi test có thật, kiểm đúng điều ghi; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc: xoay EXIF quanh ngưỡng 600 px, lệnh tạo lại thừa cho tệp banner (chép vào "Việc phát hiện
  thêm"); bộ đệm trễ tới 60 s khi mẫu, danh mục, ảnh đổi (đã ghi ở 5.5, Q7).
