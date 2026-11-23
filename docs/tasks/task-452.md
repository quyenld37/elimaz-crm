# Task 452 — Chiến dịch giảm giá web trong promotion

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 452, M5-40; QĐ32, QĐ33). Loại việc (quy trình mục 3): A

Luật website đã đọc (Step 0, `apps/storefront` `origin/main` 90627d0): `src/server/campaigns/{rules,store}.ts`,
`src/server/promotions/engine.ts` (`campaignPrice`, `cheapestProgram`), `src/features/promotions/campaign-link.ts`,
`src/features/text.ts` (`slugify`); admin `origin/main` `src/lib/campaigns.mjs` (`campaignStatus`, `campaignPath`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu cũ.
  - `promotion` (gói gốc): thêm kiểu `WebCampaign`, `WebCampaignInput`, `WebCampaignFilter`, `PreviewCounts`, `Preview`,
    `PreviewSample`, `VariantRef`, `Applied`, `CampaignChange`; hằng trạng thái, phạm vi, `TopicWebCampaignChanged`;
    hàm `CampaignStatus`, `CampaignPrice`, `BestCampaign`, `CampaignCovers`, `CampaignSlug`, `CampaignLinkKey`,
    `ActiveForVariants`, `GetWebCampaign`, `ListWebCampaigns`, `ListCampaignTargets`, `WebCampaigns`,
    `WebCampaignByLinkKey`, `MaxPercent`; phương thức `Writer.{CreateCampaign,SaveCampaign,PreviewCampaign,
    SetCampaignState,DeleteCampaign}`; sentinel `ErrPreviewRequired`, `ErrPercentTooHigh`, `ErrCampaignRunning`.
    `Writer` thêm trường dùng sẵn (`Events`, `Queue` đã có). Không đổi chữ ký có sẵn: 0 chỗ gọi đổi
    (`promotion` có 3 gói nhập: `cmd/crm`, `internal/httpapi`, `internal/modules/order`).
  - `cache`: thêm `SpaceCampaigns` vào `Spaces()` (10 → 11 vùng; `invalidator_test` so số vùng — sửa số trong test,
    điều kiểm giữ nguyên: resync làm mới mọi vùng).
  - `webcache`: thêm `EntityWebCampaign` và chủ đề `promotion.TopicWebCampaignChanged` trong `Topics()`; ánh xạ →
    `BumpSpace(SpaceCampaigns)`.
  - `promotion` thêm cả `Running`, `CampaignPath`, `CampaignTarget`, `WebCampaign.AuditFields`, `WebCampaignChanged`,
    `SettingsKey`, `DefaultMaxPercent` (bổ sung sau review vòng 1).
  - `web` (lớp HTTP website): thêm `(*Catalog).Campaigns`, `(*Catalog).Campaign`, `(*Catalog).WithClock` (trường `now`;
    `httpapi/server.go` nối `.WithClock(d.Now)` để trạng thái chiến dịch theo đồng hồ của Deps).
  - `sqlc.yaml`: khối promotion thêm kiểu `*int32` cho `preview_revision`, `preview_models`, `preview_variants`; sqlc
    sinh lại `models.go` của mọi gói store (thêm hai bảng mới).
  - `httpapi`: 10 handler mới do ogen sinh interface (8 nhân viên, 2 website); mã lỗi mới trong `errors_promotion.go`.
- Gói phụ thuộc ngược (go list): `promotion` ← `cmd/crm`, `internal/httpapi`, `internal/modules/order`; `httpapi/web` ←
  `httpapi`; `wiring/webcache` ← `cmd/crm`; `platform/cache` ← `cmd/crm`, `httpapi`, `httpapi/web`,
  `platform/observability`, `wiring/webcache`.
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm. Nhân viên (đường phẳng như M2): `listWebCampaigns`,
  `createWebCampaign`, `getWebCampaign`, `saveWebCampaign`, `previewWebCampaign`, `setWebCampaignState`,
  `deleteWebCampaign`, `listCampaignTargets` (`promotion.read` / `promotion.manage` của M2, không thêm quyền, không sửa
  `docs/quyen.md`). Website (`x-auth: service_key`, `web.catalog.read`): `webCampaigns`, `webCampaign`. Mã lỗi mới
  trong `x-error-codes`: `preview_required`, `percent_too_high`, `campaign_running` (kèm câu tiếng Việt ở
  `console/src/app/errors.ts`).
- Chỗ dùng trên màn hình: chưa có màn hình chiến dịch (task sau của M5, KM04); chỉ thêm câu lỗi vào `errors.ts`.
  `console/src/api/**` sinh lại bằng `make gen`.
- Câu sqlc, bảng, pha migration: `migrations/0520_promotion_web_campaigns.sql` (pha mở rộng: chỉ CREATE, GRANT, VIEW):
  bảng `promotion.web_campaigns`, `promotion.web_campaign_targets` (RLS), view `promotion.web_campaigns_v`,
  `promotion.web_campaign_targets_v`. Câu mới `queries/promotion/web_campaigns.sql` (khối sqlc `promotion` có sẵn).
  Đọc view `catalog.products_v`, `catalog.variants_v`, `catalog.categories_v` (chọn đích, đếm xem trước). Cài đặt
  trần phần trăm: khóa `promotion.web_campaigns` của `platform.settings` (gói `settings`, chỉ đọc).
- Test sẽ chạy: test mới của `promotion` (`-run 'WebCampaign|Campaign'`), `internal/httpapi -run 'WebCampaign'`,
  `internal/httpapi/web -run 'Campaign'`, `internal/wiring/webcache` (cả gói, nhỏ), `tests/arch`, `errors_snapshot`,
  `errorcodes`; console `npm test` (errors.test.ts); cả gói và vùng rủi ro cao ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `settings.Get` (cài đặt theo tenant), `textnorm.Fold`
  (bỏ dấu tiếng Việt), `events.Registry.Emit`, `realtime.Bus` (báo hiệu), `cache.ReadCache` + `etag`/`matches` của
  `httpapi/web`, `refusableWrite`/`WriteTx`/`h.audit`/`idCursor`/`catalogRead`/`readAsDefaultSeller` của `httpapi`,
  `promotion.ErrVersionConflict`, `ErrNotFound`, `ErrInvalidInput`, `Writer.signal` của M2, `catalog.DefaultSeller`,
  `NewCode`/`codegen` (không hợp: mã chiến dịch là uuid).
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên. `revision` lệch dùng lại `ErrVersionConflict` (409
  `version_conflict`) thay vì sentinel mới cùng nghĩa. `CampaignSlug` dùng `textnorm.Fold` rồi đổi ký tự ngoài
  `[a-z0-9]` thành `-` (không thêm x/text). Viết mới: luật giá, trạng thái, phạm vi (chép đúng website, không có sẵn
  trong CRM); câu SQL chiến dịch.

## Soát thiết kế

Luật chép từ website (mỗi luật có test):
- Đầu vào (`campaignInputSchema`): tên trim 1–120; phạm vi `all`/`products`/`categories`; đích 0 khi `all`, 1–30 và
  không trùng khi khác; phần trăm nguyên 1–99; bắt đầu < kết thúc; `allowCoupon` mặc định tắt; `clearance` tùy chọn.
- Trần `campaignMaxPercent`: mặc định 50, giá trị cài 1–99 (ngoài khoảng → 50); áp khi lưu và khi bật.
- `campaignPrice = Math.round(list × (100 − %) / 100)`; áp khi `0 < giá < niêm yết`; `cheapestProgram`: giá thấp
  nhất, hòa thì mã chương trình nhỏ hơn (website so chuỗi `campaign:<id>`, tương đương so chuỗi id).
- `campaignCovers`: `all`, hoặc đích chứa mã mẫu (`products`) / mã danh mục của mẫu (`categories`, đúng danh mục của mẫu,
  không tính danh mục cha).
- Chạy khi `state = enabled` và `startsAt ≤ now < endsAt`; trạng thái hiển thị (`campaignStatus` của admin): `draft`,
  `paused` theo state; còn lại `scheduled` / `active` / `ended` theo giờ, `endsAt` đúng giờ là `ended`.
- `saveCampaign`: `revision` lệch → 409; đang `enabled` → 409 (website `INVALID_STATE`; ở đây `campaign_running`);
  mới là `draft`, sửa giữ state. `setCampaignState`: lý do trim 1–300; `enabled`/`paused`; bản xem trước phải khớp.
  `deleteCampaign`: lý do 1–300, `revision`, đang `enabled` → 409; banner trỏ chiến dịch tự ẩn (task 453).
- `campaignPreview`: các biến thể được phủ có giá sau giảm hợp lệ; số mẫu, số biến thể, 12 mẫu giá đầu.
- Liên kết `/uu-dai/<slugify(tên, 60)>-<8 hex đầu của id>`; `campaignKeyOf` lấy phần sau dấu `-` cuối.
- Audit giữ bản trước, bản sau, lý do (`discount_campaign_audit`) → `identity.audit_logs`.

5.1 Chủ dữ liệu: `promotion` (QĐ32, QĐ33, bảng chuyển admin "Chiến dịch giảm giá"); bảng ở schema `promotion`.
5.2 Không ghi bảng module khác; cài đặt đọc qua `settings.Get` (platform). 5.3 Đọc catalog qua view `products_v`,
`variants_v`, `categories_v`. Module khác (content, queries) đọc chiến dịch qua `promotion.web_campaigns_v`,
`web_campaign_targets_v`. 5.4 Mỗi lệnh ghi một transaction: khóa dòng chiến dịch, đổi, ghi đích, audit, sự kiện.
5.5 Bộ đệm website làm mới sau commit: sự kiện `promotion.web_campaign.changed` → consumer `webcache` → báo hiệu hệ
thống → Invalidator. 5.6 Không gọi ngoài. 5.7 Bảng mới có `tenant_id`, RLS, unique/khóa ngoại ghép tenant; mọi câu
trong `WithTenant`/`WriteTx`. 5.8 Module không nhập net/http; handler mỏng. 5.9 Mọi POST/PUT có `IdempotencyKey` và
`x-permission`. 5.10 Valkey chỉ đệm danh sách chiến dịch cho website; giá thật khi báo giá/tạo đơn tính lại từ
PostgreSQL bằng `ActiveForVariants` (M5-09, 10). 5.11 Không thư viện mới. 5.12 Theo mẫu M2 (`promotion.Writer`,
audit ở httpapi, lỗi sentinel, `errors_promotion.go`).

6.1 Sửa đồng thời: mọi lệnh ghi khóa dòng chiến dịch (`FOR UPDATE`) rồi so `revision`; lệch → 409. Đích chỉ chèn/xóa
theo chiến dịch đã khóa (ADR-46 nhóm 10: `web_campaigns` → `web_campaign_targets`). 6.2 `Idempotency-Key` cho mọi
lệnh ghi; 50 lần cùng khóa → một lần đổi (test). 6.3 Một lệnh chỉ khóa một chiến dịch rồi đích của nó: không vòng.
Bật đọc `catalog.variants_v` không khóa. 6.4 Lỗi tạm đi qua `WriteTx` (RetryTx); thân hàm gán lại mọi biến kết quả.
6.5 READ COMMITTED + khóa dòng. 6.6–6.8 Consumer `webcache` dùng lịch thử lại có sẵn của hàng đợi; không gọi ngoài.
6.9 Đếm xem trước một câu theo index `variants_product_idx`/khóa chính; danh sách có trang (≤ 50). 6.10 Tiền `int64`
đồng; `list × (100 − %)` ≤ (2^53 − 1) × 99 < 2^63 không tràn; kết quả ngoài `(0, list)` không áp. 6.11 rapid: mọi giá
và phần trăm → giá trong `(0, list)` hoặc không áp; khớp công thức số thực của website trong vùng số nguyên an toàn.
6.12 Không dữ liệu cá nhân (tên chiến dịch, lý do do nhân viên nhập; audit giữ mã nhân viên). 6.13 Quyền
`promotion.read`/`promotion.manage` (sale không có manage → 403); website `web.catalog.read`; tenant khác 404.

## Quyết định thay chủ shop

1. **Trần phần trăm lưu ở `platform.settings`, khóa `promotion.web_campaigns`, trường `maxPercent`** (M2 không có bảng cài
   đặt của promotion). Chưa cài hay ngoài 1–99 → 50, đúng `campaignMaxPercent` của website. Task này chỉ đọc; chưa có
   API sửa trần (xem Việc phát hiện thêm). Phương án khác: cột riêng trong bảng mới của promotion. Dễ đảo: đổi chỗ đọc
   trong `MaxPercent`.
2. **Mã chiến dịch sinh bằng uuid v4 (`gen_random_uuid()`, Go `uuid.New()`), không `uuidv7()` như Interfaces ghi.**
   Liên kết website dùng 8 hex đầu của id; 8 hex đầu của uuidv7 là giờ tạo nên hai chiến dịch tạo trong cùng ~65 giây
   trùng liên kết. Thêm cột sinh `link_key` duy nhất trên cả shop (ngoại lệ luật (g) có COMMENT và dòng trong
   `docs/views.md`); trùng thì sinh lại (tối đa 5 lần). Phương án khác: lấy 8 hex cuối (đổi luật liên kết của website).
3. **Sửa, xóa bị từ chối khi `state = enabled` ở mọi thời điểm** (đúng website `INVALID_STATE`), không chỉ "trong giờ
   chạy" như kế hoạch: nếu cho sửa chiến dịch đã bật nhưng chưa tới giờ, phần trăm mới sẽ chạy mà không qua xem trước.
   Muốn sửa: tạm dừng → sửa → xem trước → bật.
4. **Danh mục sản phẩm web (M5-07) không nhúng giá chiến dịch**; website tính giá hiển thị từ `webCampaigns` (đích, %,
   giờ) bằng cùng luật làm tròn (bảng mẫu chung), báo giá/tạo đơn tính lại bằng `ActiveForVariants`. Lý do: website giữ
   bản sao danh mục theo luồng `changes` (mốc `updated_at` của mẫu); giá chiến dịch đổi theo giờ bắt đầu/kết thúc và theo
   chiến dịch mà mẫu không đổi, nên bản sao sẽ giữ giá cũ. Vì vậy sự kiện `promotion.web_campaign.changed` làm mới vùng
   đệm `campaigns` (mới), không xóa khóa sản phẩm. Phương án khác: nhúng giá vào mẫu và thêm chiến dịch vào luồng
   `changes` (mọi mẫu của chiến dịch cả shop đổi mỗi lần bật/tắt).
5. **`VariantRef` có thêm `ListPrice`**: bên gọi (báo giá, tạo đơn) đã có giá niêm yết đang dùng; tránh đọc lại giá trong
   promotion và lệch với giá dòng.
6. **`revision` lệch dùng lại `ErrVersionConflict` (409 `version_conflict`)** thay vì sentinel `ErrRevisionConflict` mới
   cùng nghĩa; lỗi mới đặt trong `errors_promotion.go` (luật Task 441: lỗi của vùng promotion ở tệp của vùng) thay vì tệp
   `errors_promotion_campaign.go`.
7. **Xem trước không tăng `revision`, không phát sự kiện, không audit**; bật cần đủ ba điều: bản đã ghi đúng `revision`,
   số nhân viên gửi bằng bản đã ghi, và bằng số tính lại lúc bật (biến thể đang bán của người bán, `variants_v.sellable`).
8. **Liên kết bỏ dấu bằng `textnorm.Fold`** (bảng tiếng Việt, không thêm x/text): chữ Latin có dấu ngoài tiếng Việt
   (ñ, ç) ra "-" thay vì chữ gốc; chỉ làm phần tên khác, mã cuối vẫn tìm đúng (website chuyển hướng). Bảng mẫu 13 tên
   (tiếng Việt, ký hiệu, emoji, cắt 60 rơi vào "-") khớp đúng.
9. **Bảng mẫu `testdata/contracts/web-campaign-{rounding,links}.json` sinh ngay ở task này** bằng
   `web-campaign.gen.mjs` (chép nguyên hàm của website `origin/main` 90627d0) vì website chưa sinh (M5-29); M5-29 sinh lại
   từ engine thật và kiểm cùng tệp.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Bảng mẫu làm tròn 200 dòng (1 đồng, giá lẻ, 1 %, 99 %, giá lớn) khớp website; giá ≤ 0, % ngoài 1–99 không áp [WB06] | `TestCampaignPrice_MatchesWebsiteTable` | `go test -count=1 -run '^(TestCampaignPrice_MatchesWebsiteTable\|TestCampaignLink_MatchesWebsiteTable\|TestCampaignStatus\|TestBestCampaign\|TestCampaignCovers)$' ./internal/modules/promotion/` (trước khi có mã: `undefined: promotion.CampaignPrice`) | `ok internal/modules/promotion 0.555s` |
| Liên kết `/uu-dai/<slug>-<8 hex>` khớp bảng mẫu website (13 tên) | `TestCampaignLink_MatchesWebsiteTable` | như trên | ok |
| Trạng thái draft/paused/scheduled/active/ended, biên đúng giờ | `TestCampaignStatus` | như trên | ok |
| Hai chiến dịch → giá thấp hơn; hòa → id nhỏ; giá sau giảm = niêm yết không áp | `TestBestCampaign` | như trên | ok |
| Phạm vi all/products/categories (không tính danh mục cha); giá thắng, allowCoupon, revision; ngoài giờ, tạm dừng, nháp không áp; `WebCampaigns` không nháp/tạm dừng | `TestCampaignCovers`, `TestActiveForVariants` | `go test -race -count=1 -run '^(TestCampaignPrice_Invariants\|TestWebCampaign_Lifecycle\|TestWebCampaign_MaxPercent\|TestWebCampaign_PreviewStaleCounts\|TestWebCampaign_InvalidInput\|TestActiveForVariants\|TestWebCampaignByLinkKey)$' ./internal/modules/promotion/` | `ok internal/modules/promotion 4.117s` |
| rapid: mọi giá, % → giá trong (0, list) hoặc không áp; bằng Math.round số thực của website trong vùng an toàn; % lớn hơn không cho giá cao hơn | `TestCampaignPrice_Invariants` (200 lượt, TestMain) | như dòng trên | ok |
| Vòng đời: bật chưa xem trước; đổi tên rồi bật bằng bản cũ → `ErrPreviewRequired`; sửa/xóa khi bật `ErrCampaignRunning`; revision cũ `ErrVersionConflict`; 5 sự kiện Seq 1–5, xóa có `deleted` | `TestWebCampaign_Lifecycle` | như dòng trên | ok |
| 60 % với trần 50 → `ErrPercentTooHigh`; trần 70 tạo được; hạ trần thì bật bị từ chối; trần cài sai → 50 | `TestWebCampaign_MaxPercent` | như dòng trên | ok |
| Thêm biến thể sau khi xem → bật bị từ chối; xem lại thì bật được | `TestWebCampaign_PreviewStaleCounts` | như dòng trên | ok |
| Đầu vào sai (12 ca), lý do 301 ký tự → `ErrInvalidInput` | `TestWebCampaign_InvalidInput` | như dòng trên | ok |
| Link key: nháp, đã xóa → không có; tạm dừng đọc được; hoa thường | `TestWebCampaignByLinkKey` | như dòng trên | ok |
| HTTP: sale 403; 60 % → 422 `percent_too_high`; 422 `preview_required` (chưa xem, bản cũ); 409 `campaign_running` (sửa, xóa); 409 `version_conflict`; 400 lý do rỗng; xóa → đọc 404; audit create/save/state×2/delete, 3 dòng có lý do | `TestWebCampaignAPI_Lifecycle` | `go test -race -count=1 -run '^(TestWebCampaignAPI_Lifecycle\|TestWebCampaignAPI_ConcurrentSameKey\|TestWebCampaignAPI_ListTargetsTenant)$' ./internal/httpapi/` | `ok internal/httpapi 3.102s` |
| 50 `setWebCampaignState` song song cùng khóa → revision 2, một audit có lý do, 2 sự kiện (tạo, tạm dừng) | `TestWebCampaignAPI_ConcurrentSameKey` | như dòng trên | ok |
| Lọc trạng thái, tìm tên, trang; đích mẫu theo tên không dấu; loại đích lạ 400; tenant khác đọc/ghi → 404 | `TestWebCampaignAPI_ListTargetsTenant` | như dòng trên | ok |
| `webCampaigns` chỉ đã bật chưa hết (không nháp, tạm dừng, đã xóa, đã hết), có slug, linkKey, đích, clearance, status; ETag, 304; hạn đệm 30 s | `TestWebCampaigns` | `go test -race -count=1 -run '^(TestWebCampaigns\|TestWebCampaign_ByLinkKey)$' ./internal/httpapi/web/` | `ok internal/httpapi/web 2.562s` |
| `webCampaign`: tạm dừng 200 (status paused); nháp, xóa, mã lạ 404; chiến dịch **đang bật** của tenant B (kiểm đọc được trong tenant B) → 404 với khóa của tenant A; sai dạng 400; thiếu quyền 403; không khóa 401 | `TestWebCampaign_ByLinkKey` (`foreignCampaign` sửa sau review vòng 1) | như dòng trên | `ok internal/httpapi/web 2.291s` |
| Cổng kiểm của task: build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard, console (1064 test), test -race gói đổi (httpapi, httpapi/web, promotion, cache, webcache, tests/arch), test thường gói phụ thuộc | — | `ci-task.sh` (trên phần đã stage) | Mọi cổng **xanh** trừ một test ngoài phạm vi: `TestHandlerRunsInTenantContext` (`internal/platform/queue`, gói chỉ chạy vì `models.go` sinh lại) đỏ một lần — việc còn `pending`, `RunInS 0.106` (việc hẹn giờ hơi sau, một vòng `RunOnce` bỏ sót: dạng chập chờn của Task 451). Chạy lại riêng `go test -count=5 -run '^TestHandlerRunsInTenantContext$' ./internal/platform/queue/` → `ok 0.768s`. Lần chạy thứ hai (sau sửa review vòng 1): mọi cổng xanh, `platform/queue` qua; đỏ một test ngoài phạm vi cùng dạng: `TestUnknownOutcomesInDoubt/unknown` (`internal/platform/commands`, việc còn `pending`, `RunInS 0.107`); chạy riêng `go test -count=5 -run '^TestUnknownOutcomesInDoubt$' ./internal/platform/commands/` → `ok 1.233s`. Không sửa (ngoài phạm vi; ghi ở Việc phát hiện thêm) |
| Sự kiện `promotion.web_campaign.changed` → báo hiệu `web_campaign` → Invalidator làm mới vùng `campaigns` [WB06] | `TestConsumer_NameTopics`, `TestConsumer_Payloads`, bảng của `invalidator_test.go`, `TestInvalidator_SystemResync` (11 vùng) | `go test -count=1 ./internal/wiring/webcache/` | `ok internal/wiring/webcache 1.740s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Quyết định 2, 3, 4, 5, 6 ở trên. Không sửa `internal/queries/webcatalog` (quyết định 4); thêm vùng đệm
  `cache.SpaceCampaigns` (10 → 11 vùng, sửa số trong `invalidator_test`), view thứ hai `web_campaign_targets_v` (cho
  banner Task 453 cần đích), sửa số view trong `viewsdoc_test` (39 → 41).
- Test luật giá (`webcampaign_price_test.go`) viết trước và chạy thấy FAIL (chưa có `CampaignPrice`…); test trên database
  của module, HTTP, web viết ngay sau mã của từng phần trong cùng lượt, không có lần chạy FAIL riêng.

## Việc phát hiện thêm

- Chưa có API/màn hình sửa trần `maxPercent` (website đổi bằng biến môi trường). Cần một task cài đặt promotion (hoặc
  gộp vào màn hình chiến dịch của M5) trước khi chủ shop muốn chạy chiến dịch > 50 %.
- Chuyển dữ liệu chiến dịch có sẵn từ `commerce.sqlite` của website sang bảng mới (giữ nguyên id để liên kết đã gửi
  khách còn chạy) thuộc task chuyển dữ liệu của M5/M7, chưa làm ở đây.
- `internal/platform/queue` `TestHandlerRunsInTenantContext` chập chờn (một vòng `RunOnce` bỏ sót việc có `run_at` hơi
  sau giờ máy; cùng dạng đã sửa ở Task 451 cho test khác): cần chờ theo sự kiện hoặc chạy tới khi hết việc.
  Cùng dạng ở `internal/platform/commands` `TestUnknownOutcomesInDoubt/unknown` (đỏ một lần ở ci-task lần 2).
- Tên chiến dịch: hợp đồng kiểm `maxLength: 120` trước khi trim, website trim trước rồi đếm → tên 120 ký tự có khoảng
  trắng hai đầu bị CRM từ chối (400) mà website nhận. Ca biên; sửa khi làm màn hình chiến dịch (màn hình trim trước khi
  gửi) hoặc nới `maxLength` của hợp đồng (reviewer vòng 2).

## Reviewer

Kết luận: ĐẠT (vòng 2).

- Vòng 1 — CẦN SỬA: ca "tenant khác → 404" của `webCampaign` dùng chiến dịch nháp của tenant B nên 404 đến từ luật
  "nháp", không chứng minh tách tenant. Đã sửa `foreignCampaign` (`internal/httpapi/web/campaigns_test.go`): xem trước,
  bật, kiểm đọc được trong tenant B rồi mới gọi bằng khóa của tenant A. Bổ sung ký hiệu còn thiếu vào mục Ảnh hưởng.
- Vòng 2 — ĐẠT: không còn phát hiện phải sửa. Cả 9 quyết định thay chủ shop đúng và an toàn. Hai test đỏ ngoài phạm vi
  (`platform/queue`, `platform/commands`) không liên quan diff (chỉ `models.go` sinh thêm struct; cùng dạng lệch đồng
  hồ trước `RunOnce` như Task 451). Ghi chú không bắt buộc: chú thích `foreignCampaign` nói "khóa dịch vụ" nhưng kiểm
  bằng hàm module; `maxLength: 120` của tên kiểm trước khi trim (website trim trước); `ILIKE` không thoát `%`, `_` (chỉ
  làm tìm rộng hơn). Ghi vào Việc phát hiện thêm, không sửa trong task này.
