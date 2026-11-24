# Task 454 — content: bộ phối

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 454, M5-42; làn L1 sau M5-41).
Loại việc (quy trình mục 3): A

Nguồn đã đọc (Step 0): website `apps/storefront-bct/src/server/looks/{rules,store,watch}.ts`,
`src/features/looks/look-link.ts`, `src/features/text.ts` (`slugify`); admin `apps/admin-camplink/src/lib/looks.mjs`.
Giới hạn chép: tối đa 500 bộ, tối đa 6 bật trên trang chủ; 1–3 sản phẩm khác nhau mỗi bộ; chấm x, y 0–100 %; tên
1–28, tiêu đề ≤ 60 (trống = tên các món), dòng dẫn ≤ 90, câu chuyện ≤ 220, màu ≤ 60, cỡ người mẫu ≤ 6 ký tự; chiều cao
130–200 cm; ≤ 6 danh mục thêm; khung giờ bắt đầu < kết thúc; ảnh là một ảnh sản phẩm của các món. 6 lý do ẩn (đúng tên
website, đúng thứ tự kiểm): `disabled`, `scheduled`, `expired`, `product-missing`, `sold-out`, `photo-missing`. Liên kết
`/bo-phoi/<slugify(tên, 60)>-<8 hex đầu của id>`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chủ yếu **thêm**.
  - `internal/platform/textnorm`: thêm `Slug(s string, max int) string` (thân lấy nguyên từ `promotion.CampaignSlug`).
    `promotion.CampaignSlug` (2 chỗ gọi: `httpapi/web/campaigns.go`, `webcampaign_price_test.go`) giữ chữ ký, thân gọi
    `textnorm.Slug(name, 60)` — commit cấu trúc riêng.
  - `internal/modules/content`: kiểu `Look`, `LookItem`, `LookSet` (+ `AuditFields`), `LookChange`, `LookRefData`
    (+ `PhotoOf`), `LookProduct`, `LookImage`, `LookStatus`, `LooksChanged`; hàm `GetLooks`, `LoadLookRefData`,
    `LookProductIDs`, `NormalizeLooks`, `ValidateLooks`, `ForLink`, `LookVisibility`, `LookStatuses`,
    `LookHiddenReasons`, `LookLinkKey`, `LookSlug`, `LookPath`, `LookAutoTitle`, `LookModelLine`, `LookCategories`;
    phương thức `Writer.SaveLooks`; hằng giới hạn, `LookHiddenSoldOut`, `LookHiddenPhotoMissing` (4 lý do còn lại dùng
    chung hằng `Hidden*` của banner), `TopicLooksChanged` (`content.looks.changed`); sentinel `ErrTooManyLooks`,
    `ErrTooManyHomeLooks`, `ErrInvalidLook`, `ErrLookItems`, `ErrLookDot`, `ErrLookPhoto`, `ErrLookWindow`,
    `ErrLookNotFound`. Cấu trúc (không đổi hành vi): `Writer.bannersChanged` (không xuất, 1 chỗ gọi) thay bằng
    `Writer.listChanged` dùng chung cho banner và bộ phối (jscpd).
  - `internal/queries/webcatalog`: thêm `InStock(ctx, tx, productIDs) ([]uuid.UUID, error)` (mẫu có biến thể bán được
    còn hàng ở kho mặc định; content không được đọc view `inventory`).
  - `internal/platform/cache`: vùng mới `SpaceLooks` (`looks`) trong `Spaces()` (13 → 14).
  - `internal/wiring/webcache`: `EntityHomeLooks`, chủ đề `content.TopicLooksChanged` trong `Topics()`; ánh xạ
    `home_looks` → làm mới `SpaceLooks`.
  - `httpapi`: handler ogen `GetLooks`, `PreviewLooks`, `SaveLooks`, `WebHomeLooks`, `WebLook`; `errors_content.go`
    thêm dòng bộ phối (bảng tách hai phần banner / bộ phối, cùng thứ tự kiểm; `ErrLookNotFound` → 404 `not_found`);
    `web.(*Catalog).HomeLooks`, `web.(*Catalog).Look`, `web.LoadLookRefs`, `web.ImageVariantsOut`. Cấu trúc (không đổi
    hành vi): `bannerList` dùng hai hàm mới không xuất `stampOut`, `categoriesOut` (dùng chung với `lookList`, jscpd).
  - Tài liệu: `docs/views.md` (dòng sự kiện `content.looks.changed`), `docs/adr/0046-thu-tu-khoa.md` (nhóm 12d: cách lưu
    bộ phối), `docs/danh-muc-dung-chung.md` (`make catalog`: `textnorm.Slug`); hợp đồng mẫu
    `testdata/contracts/web-look-links.json` + `web-look.gen.mjs` (hàm website chép nguyên văn).
  - Test có sẵn thêm dòng (không nới): `webcache` `TestConsumer_NameTopics`, `TestConsumer_Payloads`,
    `TestInvalidator_Mapping`, `TestInvalidator_SystemResync` (số vùng 14); `httpapi` `errors_snapshot_test.go`.
  - Sửa test có sẵn theo yêu cầu chủ shop: `internal/platform/cache` `TestConn_OpTimeout` bỏ ngưỡng 80 ms giờ thật,
    kiểm bằng hành vi (lỗi hạn của chính `Do` khi ctx cha không có hạn; hạn đặt cho lệnh ≤ OpTimeout).
- Gói phụ thuộc ngược (go list): `textnorm` ← `cash/internal/formats`, `catalog`, `customer`,
  `fulfillment/internal/locations`, `inventory`, `promotion`, `queries`, `tools/adminunits` (chỉ thêm hàm);
  `content` ← `cmd/crm`, `httpapi`, `httpapi/web`, `wiring/webcache`; `webcatalog` ← `httpapi`, `httpapi/web`;
  `platform/cache` ← `cmd/crm`, `httpapi`, `httpapi/web`, `observability`, `wiring/webcache`.
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm. Nhân viên: `getLooks` `GET /api/v1/content/looks` (`content.read`),
  `previewLooks` `POST /api/v1/content/looks/preview` (`content.read`, `x-idempotency: exempt`), `saveLooks`
  `PUT /api/v1/content/looks` (`content.manage`, `IdempotencyKey`). Website (`x-auth: service_key`,
  `web.catalog.read`, ETag/304): `webHomeLooks` `GET /api/v1/web/content/looks?home=true|false`, `webLook`
  `GET /api/v1/web/content/looks/{linkKey}`. Mã lỗi mới trong `x-error-codes` (câu tiếng Việt trong
  `console/src/app/errors.ts`).
- Chỗ dùng trên màn hình: màn Bộ phối là Task 460 (chưa làm); `console/src/api/**` sinh lại bằng `make gen`; thêm câu
  lỗi vào `errors.ts`.
- Câu sqlc, bảng, pha migration: `migrations/0522_content_looks.sql` (pha mở rộng: CREATE TABLE, RLS, GRANT). Bảng
  `content.look_sets`, `content.looks`, `content.look_items`. Câu mới `queries/content/looks.sql` (khối sqlc `content`
  có sẵn); `queries/webcatalog/availability.sql` thêm `InStockProducts`. sqlc sinh lại `models.go` của mọi gói store.
  Đọc view: `catalog.products_v`, `variants_v`, `sellers_v`, `categories_v`, `product_images_v` (content);
  `catalog.variants_v`, `inventory.stock_available_v`, `inventory.warehouses_v` (webcatalog).
- Test sẽ chạy: `content` (cả gói); `promotion -run 'Slug|Link'`; `textnorm`; `queries/webcatalog -run InStock`;
  `httpapi -run 'Looks|ErrorSnapshot|ErrorCodes'`; `httpapi/web -run 'Look'`; `wiring/webcache`;
  `platform/cache` (cả gói, vì sửa test); `tests/arch`; console `errors.test.ts`; cả bộ ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `textnorm.Fold`, `promotion.CampaignSlug`/
  `CampaignLinkKey` (cùng `slugify` của website), `catalog.ResolveImages` (ảnh sản phẩm theo bộ kích thước của web),
  `webcatalog.Availability`, `files.PublicVariants`; mẫu banner của Task 453 trong cùng module (`home_banner_sets`,
  `SaveBanners`, `LoadRefData`, `bannerList`, `web.HomeBanners`, `etag`/`matches`, `refusableWrite`, `h.audit`,
  `sortedValues`, `statusesOut`), câu `ActiveCategories` có sẵn của content.
- Dùng lại / viết mới vì:
  - Slug: chuyển thân `promotion.CampaignSlug` thành `textnorm.Slug(s, max)` (cùng mục đích: chép `slugify` của
    website, cùng lý do thay đổi); `CampaignSlug` gọi lại, `LookSlug` dùng chung. Link key 8 hex là một dòng, giữ riêng
    ở mỗi module (liên kết của từng loại có thể đổi độ dài riêng như website: `CAMPAIGN_KEY_LENGTH`, `LOOK_KEY_LENGTH`).
  - Ảnh: dùng `catalog.ResolveImages` (content được gọi catalog) để biết ảnh đã xử lý và lấy đường dẫn — không tự đọc
    `platform.file_variants_v`.
  - Còn hàng: thêm `webcatalog.InStock` (gói đọc chung được đọc view `inventory`); `webcatalog.Availability` nhận mã
    biến thể, không nhận mã mẫu, và trả từng biến thể — viết câu gọn theo mã mẫu.
  - Luật bộ phối (`ValidateLooks`, `LookVisibility`) viết mới trong `content`: nghiệp vụ riêng của bộ phối (chép website).
    Khung lưu (khóa dòng tập, so revision, thay danh sách, phát sự kiện) giống banner nhưng bảng, luật khác: viết theo
    cùng mẫu, không gộp (khác lý do thay đổi).

## Soát thiết kế

- 5.1 Chủ dữ liệu: bộ phối thuộc `content` (kế hoạch, bảng "Sở hữu" M5), schema `content`.
- 5.2 Không ghi bảng module khác: chỉ ghi `content.look_*`; audit `content.look.save` qua `identity.RecordAudit` ở lớp
  HTTP (QĐ38: hành động `content.look.*`).
- 5.3 Đọc module khác: chỉ view công bố của `catalog` (sqlc) và `catalog.ResolveImages`; còn hàng đọc ở lớp HTTP qua
  `webcatalog.InStock` rồi truyền vào `LoadLookRefData` (content không đọc `inventory`).
- 5.4 Nguyên tử: thay danh sách + `revision` + sự kiện + báo hiệu + audit + kết quả chống trùng trong một transaction.
- 5.5 Không nguyên tử: bộ đệm website làm mới qua sự kiện → `webcache` sau commit; hạn sống 60 s cho đổi theo giờ, tồn,
  mẫu, ảnh (không có sự kiện riêng).
- 5.6 Gọi ra ngoài: không. Báo Telegram khi bộ đang bật bị ẩn vẫn ở website (Task 464 "vẫn báo Telegram"); CRM trả
  kèm các bộ trang chủ đang bị ẩn và lý do để website báo.
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; ba bảng có `tenant_id`, RLS, khóa ngoại `platform.tenants`.
- 5.8 Không biết HTTP: `content` không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi: `saveLooks` có `IdempotencyKey` + `content.manage`; `previewLooks` `exempt` (chỉ tính).
- 5.10 PostgreSQL là nguồn sự thật; Valkey chỉ giữ bản trả website 60 s.
- 5.11 Không thêm thư viện.
- 5.12 Theo module mẫu: như banner (gói gốc công khai, store riêng, sentinel ở `errors.go`, đổi HTTP ở
  `errors_content.go`, test `_test` trên PostgreSQL thật, `t.Parallel()`).
- 6.1 Hai người cùng sửa: khóa dòng `look_sets` (FOR UPDATE; chưa có thì INSERT … ON CONFLICT DO NOTHING rồi khóa)
  rồi so `revision` → 409; giới hạn 6 trang chủ kiểm **sau khi khóa** trên danh sách sẽ ghi (danh sách thay cả, nên
  không có đường nào cộng dồn vượt 6). Test hai lần lưu song song cùng revision.
- 6.2 Gửi lại: `Idempotency-Key`; test lưu song song cùng khóa → một lần đổi, một audit.
- 6.3 Thứ tự khóa: ADR-46 nhóm 12d đã có `look_sets → looks → look_items`; lưu khóa `look_sets`, xóa `looks` (xóa
  dây chuyền `look_items`), chèn `looks` rồi `look_items`; không chạm bảng banner. Đọc `platform.files` qua
  `catalog.ResolveImages` không khóa.
- 6.4 Lỗi tạm: `refusableWrite` chạy lại cả transaction; `SaveLooks` gán lại mọi biến kết quả.
- 6.5 Cách ly: READ COMMITTED; kiểm ảnh lúc lưu là ảnh chụp lúc đó — đổi sau chỉ làm bộ ẩn `photo-missing`.
- 6.6–6.8: không áp dụng (không việc nền mới, không gọi ngoài).
- 6.9 Treo lâu: giới hạn câu lệnh mặc định của `db`; lưu tối đa 500 bộ × 4 câu chèn trong một transaction (chốt an toàn
  của website; thường vài chục bộ).
- 6.10 Tiền: không áp dụng (bộ phối không mang giá; website tính giá từ danh mục).
- 6.11 Quy luật: rapid — mọi danh sách `ValidateLooks` nhận đều có ≤ 6 trang chủ, ≤ 500 bộ, 1–3 món khác nhau mỗi bộ,
  chấm trong 0–100.
- 6.12 Dữ liệu cá nhân: không có.
- 6.13 Quyền: đọc `content.read`, ghi `content.manage`; website `web.catalog.read`; test 403, tenant khác.

## Quyết định thay chủ shop

- **Q1 — `on_home` thay `enabled` của website**: bộ không lên trang chủ vẫn mở bằng link; trang link bỏ qua lý do
  `disabled` (như `ignoreEnabled` của website). Bộ bị ẩn vì lý do khác (hết giờ, món ngừng bán, hết hàng, ảnh mất) thì
  link trả 404 như trang `/bo-phoi` của website không dựng được bộ.
- **Q2 — Chỉ kiểm ảnh thuộc sản phẩm của bộ khi bộ mới hoặc đổi ảnh/món.** Lưu là thay cả danh sách: nếu kiểm lại mọi
  bộ, một ảnh sản phẩm bị xóa sau này làm cả danh sách không lưu được. Bộ không đổi ảnh và món giữ nguyên, ẩn
  `photo-missing`. Món không bắt buộc đang bán lúc lưu (ẩn `product-missing`, như website).
- **Q3 — Chấm ngoài 0–100 → 422 `look_dot_invalid`** (kế hoạch "chấm 120 % → 422"); trong khoảng thì làm tròn 2 chữ số
  (cột numeric(5,2)). Màn hình (Task 460) tự kẹp khi kéo.
- **Q4 — Còn hàng do lớp HTTP đọc** (`webcatalog.InStock`, kho mặc định, biến thể bán được) và truyền vào
  `LoadLookRefData`, như "bán 30 ngày" của banner: luật kiến trúc không cho `content` đọc view `inventory`.
- **Q5 — "Món đang bán"** (`product-missing` khi không): mẫu active, chưa xóa, người bán hoạt động, có biến thể bán được
  giá > 0 (website: mẫu có trong danh mục đang bán, 0 ₫ không bán). "Hết hàng" (`sold-out`): không biến thể bán được nào
  còn hàng ở kho mặc định. "Ảnh mất" (`photo-missing`): ảnh không còn là ảnh của món đang bán nào trong bộ, hoặc chưa
  xử lý xong.
- **Q6 — Website nhận bộ đã dựng sẵn phần không phụ thuộc giá**: tiêu đề (trống = tên các món như `autoTitle`), dòng
  người mẫu (`modelLine`), ảnh (đường dẫn các cỡ của bộ ảnh sản phẩm), mô tả ảnh, danh mục cho "Bán chạy cho dịp này"
  (danh mục chọn, trống = danh mục của các món, chỉ danh mục đang dùng), link key và slug; giá, cỡ, màu còn hàng và
  "bán chạy" website tự lấy từ danh mục, như `resolveLooks` cũ.
- **Q7 — `webHomeLooks?home=true`** trả các bộ hiện trên trang chủ (≤ 6, theo vị trí) và `hidden`: các bộ đang bật
  trang chủ mà bị ẩn vì `product-missing`, `sold-out`, `photo-missing` (kèm món gây ra) để website báo Telegram như
  `watch.ts` (Task 464). `home=false` trả mọi bộ mở được bằng link (cho trang `/bo-phoi`, sơ đồ trang). Bộ đệm 60 s mỗi
  loại, làm mới theo `content.looks.changed`.
- **Q8 — Mã lỗi 422**: `too_many_looks`, `too_many_home_looks`, `look_items_invalid` (0 hay > 3 món, món trùng),
  `look_dot_invalid`, `look_photo_invalid`, `look_window_invalid`, `invalid_look` (mã rỗng/trùng/link key trùng, tên
  rỗng, chữ quá dài, chiều cao ngoài 130–200, > 6 danh mục). Hợp đồng để giới hạn rộng để ogen không trả 400 trước.
- **Q9 — Slug dùng chung `textnorm.Slug`** (commit cấu trúc riêng trước commit tính năng, cùng commit đó gộp
  `listChanged`, `stampOut`, `categoriesOut`).
- **Q10 — Tiêu đề tự động dùng tên mọi món còn trong danh mục (chưa xóa)**, kể cả món ngừng bán: màn hình thấy được tên
  thay vì trống; bộ đang hiện thì mọi món đều đang bán nên website thấy đúng như `autoTitle` cũ.
- **Q11 — `webLook` không có ETag** (như `webCampaign`); đọc từ cùng bản đệm của `webHomeLooks?home=false`.
- **Q12 — `previewLooks` trả kèm mẫu và ảnh** của các món gửi lên (màn 460 chọn ảnh người mẫu cho món vừa thêm, chưa lưu).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 501 bộ, 7 bộ trang chủ, 0/4 món, món trùng/không mã, chấm 120 %/âm/NaN/biên, ảnh ngoài bộ/không có/thiếu, khung giờ ngược/bằng, mã rỗng/trùng/link key trùng, mọi giới hạn chữ, chiều cao 129/130/200/201, 7 danh mục | `TestValidateLooks` (39 ca) | `go test -race -count=1 ./internal/modules/content/` | `ok … 2.2s` |
| Bộ đã lưu không đổi ảnh/món không kiểm lại ảnh (Q2); đổi món hay ảnh thì kiểm | `TestValidateLooks_UnchangedPhotoNotRechecked`, `TestSaveLooks_ChecksReferences` (phần cuối) | như trên | `ok` |
| Mỗi lý do ẩn một ca (đủ 6, test tự đếm `LookHiddenReasons()`), đúng thứ tự, món gây ra, trang link bỏ qua `disabled` | `TestLookVisibility` | như trên | `ok` |
| Tiêu đề tự động, dòng người mẫu, danh mục "Bán chạy cho dịp này" như website | `TestLookDerived` | như trên | `ok` |
| `LookSlug`, `LookLinkKey`, `LookPath` đúng `look-link.ts` (12 dòng do hàm website sinh); `textnorm.Slug`; `CampaignSlug` vẫn đúng bảng website | `TestLookLinks_MatchWebsite`, `TestSlug`, `TestCampaignLink_MatchesWebsiteTable` có sẵn | như trên; `go test -count=1 ./internal/platform/textnorm/`; promotion trong ci-task | `ok` |
| Quy luật (rapid): mọi danh sách lưu được có ≤ 500 bộ, ≤ 6 trang chủ, 1–3 món khác nhau, chấm trong ảnh, ảnh thuộc món | `TestValidateLooks_Property` | như dòng 1 | `ok` |
| Lưu thay cả danh sách theo revision trên PostgreSQL: đọc lại đủ trường đã chuẩn hóa, đúng thứ tự bộ và món; revision cũ → `ErrRevisionConflict`; mỗi lần lưu một sự kiện Seq = revision | `TestSaveLooks_ReplaceByRevision` | như dòng 1 | `ok` |
| Kiểm khi lưu trên dữ liệu thật: ảnh ngoài bộ, ảnh tenant khác, 7 trang chủ, 4 món → lỗi đúng, không ghi | `TestSaveLooks_ChecksReferences` | như dòng 1 | `ok` |
| Hai lần lưu song song cùng revision, đang 5 bộ trang chủ, mỗi lần bật thêm 1 → một thành công, một `ErrRevisionConflict`; cuối 6 trang chủ | `TestSaveLooks_ConcurrentSameRevision`; qua HTTP `TestLooksAPI_Concurrent` (một 200, một 409 `version_conflict`) | như dòng 1; `go test -race -count=1 -run 'Look' ./internal/httpapi/` | `ok … 2.9s` |
| Lý do ẩn trên dữ liệu thật: món ngừng bán / đã xóa / chưa giá → product-missing; không còn hàng → sold-out; ảnh bị xóa khỏi mẫu → photo-missing; ảnh của món thứ hai hiện; ảnh có đường dẫn, màu | `TestLoadLookRefData_Visibility` | như dòng 1 | `ok` |
| Tenant khác riêng biệt | `TestLooks_OtherTenant`, `TestLooksAPI_OtherTenant` (ảnh của A dùng ở B → 422 `look_photo_invalid`) | như trên | `ok` |
| API: sale 403; mỗi luật → 422 đúng mã (11 ca: `too_many_home_looks`, `too_many_looks`, `look_items_invalid` ×3, `look_dot_invalid`, `look_photo_invalid` ×2, `look_window_invalid`, `invalid_look` ×2); lưu → revision 1, trạng thái (sold-out, món, tiêu đề tự động, link), mẫu, ảnh; 409; xem trước không ghi, cũng kiểm luật; một audit `content.look.save` | `TestLooksAPI_SaveRulesAndRead` | `go test -race -count=1 -run 'Look' ./internal/httpapi/` | `ok` |
| 20 lần lưu cùng khóa → một lần đổi, một audit, một sự kiện | `TestLooksAPI_Concurrent` (phần cuối) | như trên | `ok` |
| `webHomeLooks?home=true` ≤ 6 đúng thứ tự, tiêu đề tự động, dòng người mẫu, ảnh bộ v1, link; `hidden` có bộ hết hàng (món, tên món), không có bộ hết giờ; `home=false` có bộ không lên trang chủ; ETag → 304; TTL 60 s; khóa thiếu quyền 403 | `TestWebHomeLooks` | `go test -race -count=1 -run 'Look' ./internal/httpapi/web/` | `ok … 2.9s` |
| `webLook` bộ không lên trang chủ 200 (link key hoa/thường); hết giờ, mã lạ, đã xóa → 404 `not_found`; 403 | `TestWebLook` | như trên | `ok` |
| Còn hàng theo mẫu ở kho MAIN: có tồn → có; tồn 0, đã xuất hết, biến thể ngừng bán, mã lạ → không; rỗng → rỗng | `TestInStock` | `go test -race -count=1 -run '^TestInStock$' ./internal/queries/webcatalog/` | `ok … 2.8s` |
| Mã lỗi mới đổi đúng HTTP | `TestDomainErrorSnapshot` (8 dòng mới) | `go test -count=1 -run 'ErrorSnapshot' ./internal/httpapi/` | `ok` |
| Sự kiện → làm mới vùng `looks`; 14 vùng | `TestConsumer_Payloads`, `TestConsumer_NameTopics`, `TestInvalidator_Mapping`, `TestInvalidator_SystemResync` | `go test -race -count=1 ./internal/wiring/webcache/` | `ok … 2.9s` |
| `TestConn_OpTimeout` không còn ngưỡng giờ thật: lỗi hạn riêng của `Do` (ctx cha không có hạn), hạn đặt cho lệnh ≤ OpTimeout | `TestConn_OpTimeout` | `go test -race -count=3 -run '^TestConn_OpTimeout$' ./internal/platform/cache/` | `ok … 5.7s` (3 lần) |
| Câu lỗi màn hình cho mã mới | `errors.test.ts` | `npx vitest run src/app/errors.test.ts` | `17 passed` |
| Luật kiến trúc (ranh giới content không đọc inventory, SQL chỉ view công bố, lỗi có ánh xạ) | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok … 62.9s` |
| Cổng kiểm của task | (cả bộ) | `ci-task.sh` (sau `git merge main`: Already up to date, main = 1472f08) | `CI-TASK XANH`: mọi cổng tĩnh (build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard, console, arch) xanh; `-race`: content, httpapi 28 s, httpapi/web, promotion 44 s, cache, textnorm, webcatalog, webcache `ok`; gói phụ thuộc 198 s `ok` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 454 không có test e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm ngoài Files của kế hoạch: `textnorm.Slug` (Q9), `webcatalog.InStock` (Q4), vùng `cache.SpaceLooks` + ánh xạ
  `webcache` (bộ đệm 60 s, làm mới theo sự kiện như banner), câu lỗi `console/src/app/errors.ts`, hợp đồng mẫu
  `testdata/contracts/web-look-links.json`, sửa `TestConn_OpTimeout` (chủ shop giao trong lượt này).
- Ca "tenant khác 404": danh sách là một bộ mỗi tenant, không có mã trên đường dẫn của nhân viên; kiểm bằng "B thấy rỗng,
  ảnh của A dùng ở B → 422, lưu của B không đổi A" (như banner).

## Việc phát hiện thêm

- Website (Task 464): đọc `webHomeLooks?home=true` cho trang chủ và báo Telegram từ `hidden` (thay `watch.ts` đọc
  commerce.sqlite); trang `/bo-phoi/[slug]` đọc `webLook` theo `lookKeyOf(slug)`; giá, cỡ, màu còn hàng, "Bán chạy cho
  dịp này" tự lấy từ danh mục như `resolveLooks` cũ. Lệnh nhập (Task 467) đổi ảnh Nhanh của bộ sang `photoFileId`.
- Bộ đệm website trễ tới 60 s khi mẫu, tồn, ảnh đổi (không có sự kiện riêng, như banner).

## Reviewer

- Commit 1/2 `ae24bce` (cấu trúc: `textnorm.Slug`, `listChanged`, `stampOut`, `categoriesOut`): **ĐẠT** lần 1, agent
  reviewer trên `git diff --cached`. Không phát hiện phải sửa; xác nhận `CampaignSlug`, sự kiện/báo hiệu banner, thân
  `getBanners` không đổi; không còn chỗ gọi tên cũ. Ghi chú: chú thích của `slug_test.go` nhắc tệp hợp đồng chỉ có từ
  commit 2.
- Commit 2/2 (tính năng bộ phối): **ĐẠT** lần 1, agent reviewer trên `git diff --cached` sau `git merge main` (Already up
  to date, main = 1472f08). Không phát hiện phải sửa. Đã soi: giới hạn 6 trang chủ kiểm sau `FOR UPDATE` của
  `look_sets` (cả lần lưu đầu song song: lần sau chờ rồi 409); thứ tự khóa 12d; chạy lại an toàn (gán lại kết quả);
  migration chỉ thêm, RLS, GRANT như 0521; 6 lý do ẩn đúng thứ tự `explainLooks`, `lookWatched` đúng 3 lý do của
  `watch.ts`; `content` không đọc view `inventory`; lỗi đổi một chỗ (`errors_content.go`, `x-error-codes`, snapshot);
  mọi dòng bằng chứng có test thật (đếm lại 39 ca, 11 ca 422); `TestConn_OpTimeout` là thay tiêu chí cho đúng, không
  nới. Ghi chú không bắt buộc: `web.ImageVariantsOut` có thể thay vòng lặp cùng việc ở `web/catalog.go:250` (để việc
  sau); chặn link key trùng là luật thêm so với website (Q8).
