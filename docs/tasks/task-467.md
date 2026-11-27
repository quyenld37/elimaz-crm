# Task 467 — CRM: lệnh nhập dữ liệu website, chạy thử khô

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` mục «Task 467 (M5-55)» và «Phát hiện
soát chéo» (Điều phối M5, 27/11). Loại việc (quy trình mục 3): A. Đầu vào: thư mục xuất của website Task 466
(`apps/storefront-crm` `scripts/export-admin-data-for-crm.ts`, đặc tả `docs/tasks/task-466.md` của website, chỉ đọc).
Phạm vi đêm 27/11 (điều phối): viết lệnh, test, chạy khô (`--dry-run`) trên database thử riêng; **không** `--apply` trên
dữ liệu thật hay dev dùng chung. Chủ shop OK 27/11 sáng: chiến dịch đang chạy nhập ở trạng thái tạm dừng, không chuyển
yêu cầu đơn cũ và nhật ký cũ.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): **chỉ thêm, không sửa ký hiệu có sẵn** (0 chỗ tham chiếu cũ bị
  đổi):
  - `promotion.ImportedCampaign` (kiểu mới), `promotion.Writer.ImportCampaign` (mới). Trùng id/link_key trả
    `ErrInvalidInput` sẵn có (không thêm sentinel: lỗi chỉ của lệnh nhập, không qua HTTP; sentinel mới sẽ phải có dòng
    ánh xạ HTTP theo `tests/arch/errors_mapped_test.go`).
  - `customer.ImportedFeedback` (kiểu mới), `customer.Writer.ImportFeedback` (mới); id của tenant khác → `ErrInvalidInput`.
  - `cmd/crm` (gói main): lệnh mới `import-website-data` (`import_website*.go`); `main.go`: lệnh vào danh sách lệnh
    chạy một lần, chuyển qua `runOneOffCommand` (mới, gọi `runDevCommand` có sẵn cho hai lệnh dev).
- Gói phụ thuộc ngược (go list): `internal/modules/promotion` ← cmd/crm, internal/httpapi, internal/httpapi/web,
  internal/modules/order, internal/wiring/webcache; `internal/modules/customer` ← cmd/crm, internal/httpapi,
  internal/httpapi/web, internal/modules/conversation (+ 8 gói con), internal/modules/order, internal/modules/promotion,
  internal/testkit/{livetest,shiptest}. Chỉ thêm hàm nên các gói này chỉ cần build lại (ci-task test theo gói đổi).
- Operation OpenAPI bị chạm, oasdiff: không có (lệnh dòng lệnh, không có API mới).
- Chỗ dùng trên màn hình: không có. Màn Chiến dịch, Banner, Bộ phối, Góp ý, Quà sinh nhật, Nhận đơn web, Hãng trên web
  hiện dữ liệu đã nhập như dữ liệu nhân viên tạo (cùng bảng).
- Câu sqlc, bảng, pha migration: **không có migration.** Thêm 3 câu sqlc: `queries/promotion/web_campaigns.sql`
  `InsertImportedWebCampaign` (giữ id, trạng thái, ngày tạo; `ON CONFLICT DO NOTHING`), `WebCampaignIDExists` (id có
  ở tenant này không, kể cả đã xóa); `queries/customer/feedback.sql` `ImportFeedback` (giữ id, giờ tạo/sửa, trạng thái,
  ghi chú; `ON CONFLICT (id) DO NOTHING`). Bảng chạm (qua public API của module sở hữu): `customer.feedback`,
  `ord.web_intake`, `fulfillment.carrier_accounts` (show_on_web), `fulfillment.channel_carrier_settings`,
  `promotion.web_campaigns`, `promotion.web_campaign_targets`, `promotion.birthday_settings`, `platform.files`
  (ảnh banner, đường tải lên của kho tệp), `content.home_banner_sets/home_banners`, `content.look_sets/looks/look_items`,
  `identity.audit_logs`, `platform.domain_events`, `platform.jobs`. Quyền GRANT của `crm_app` đã đủ (INSERT cả bảng).
- Test sẽ chạy: `go test -race -run 'TestImportWebsite.*' ./cmd/crm/`, `-run 'TestImportCampaign' ./internal/modules/promotion/`,
  `-run 'TestImportFeedback' ./internal/modules/customer/`, `./tests/arch/`; cả gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tenancy.WithTenant,
  files.CreateUpload/CompleteUpload/Status, files.FileStore, filestest), lệnh mẫu `cmd/crm/seed_web_fixture*.go`
  (Task 218/511: đọc tệp JSON chặt, đếm theo loại, audit bằng `identity.RecordAudit`, hãng trên web qua
  `fulfillment.Service`), `recompute.go` (`openCommandDB`: crm_app, RLS vẫn áp), `servicekey.go` (người làm system).
- Dùng lại / viết mới vì:
  - Dùng lại public API của mọi module sở hữu: `content.GetBanners/LoadRefData/ValidateBanners/NormalizeBanners/
    Writer.SaveBanners`, `content.GetLooks/LoadLookRefData/PreviewLooks/Writer.SaveLooks`,
    `promotion.GetBirthdaySettings/Writer.SaveBirthdaySettings`, `order.GetWebIntake/Writer.SetWebIntake`,
    `fulfillment.Service.ListAccounts/UpdateAccount/SaveWebCarrierSettings`, `catalog.DefaultSeller/ListCategories`,
    `files.CreateUpload` + `FileStore.Put` + `files.CompleteUpload` + `files.Status` (đường tải lên chuẩn, ảnh qua imager).
  - Viết mới `promotion.Writer.ImportCampaign`: `CreateCampaign` luôn sinh id mới và chỉ tạo nháp; nhập phải giữ `id`
    (liên kết `/uu-dai/<tên>-<8 hex đầu id>` của website còn chạy), trạng thái (paused/draft) và ngày tạo. Dùng lại
    `checkCampaignInput`, `replaceTargets`, `campaignChanged` bên trong (cùng luật, cùng sự kiện).
  - Viết mới `customer.Writer.ImportFeedback`: `SubmitFeedback` dùng `now` và luôn `new`; nhập phải giữ id (khóa tự
    nhiên lần chạy lại), giờ tạo/sửa, trạng thái, ghi chú. Dùng lại `feedbackParams`, cách gắn khách của
    `SubmitFeedback` (cùng `NormalizePhone` + `identityOwner`; khối gắn khách 10 dòng lặp lại trong `ImportFeedback`:
    gộp vào một hàm là dời cấu trúc của `SubmitFeedback`, để commit riêng — ghi ở Việc phát hiện thêm), `validMultiline`
    cho ghi chú.
  - `db.Savepoint` (`internal/platform/db/savepoint.go`) cho savepoint từng bản ghi, từng loại (jscpd chỉ ra bản tự
    viết trùng; đã xóa bản tự viết). `seedReport.line` (seed_web_fixture.go) in số unmatched theo loại.
    `textnorm` không dùng cho đường tên danh mục (Fold bỏ dấu: "Váy" khớp "Vây" — khớp nhầm).
  - Lệnh ở `cmd/crm` (Step 0 của kế hoạch: «cmd/crm nếu gọn»): như `seed-web-fixture`, không cần gói gốc mới hay luật
    kiến trúc mới.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Chủ dữ liệu: không thêm bảng; mỗi loại ghi vào bảng của module sở hữu (promotion, content, customer, order,
  fulfillment, platform/files).
- 5.2 Không ghi bảng module khác: lệnh chỉ gọi public API; SQL mới nằm trong `queries/promotion`, `queries/customer`
  của chính module.
- 5.3 Đọc module khác qua view: lệnh không đọc bảng nào trực tiếp; danh mục qua `catalog.ListCategories`, ảnh mẫu qua
  `content.LoadLookRefData` (view `catalog.product_images_v`).
- 5.4 Nguyên tử: mỗi loại một transaction (kế hoạch); trong một loại, mỗi bản ghi chạy trong SAVEPOINT riêng nên một
  bản ghi lỗi không làm hỏng các bản ghi khác của loại đó.
- 5.5 Việc sau commit: sự kiện `promotion.web_campaign.changed`, `content.banners.changed`, `content.looks.changed`,
  `order.web_intake.changed`, `carrier.web_settings_changed` phát trong tx bằng API của module (bộ đệm web làm mới).
- 5.6 Gọi ra ngoài: không gọi Nhanh, không gọi website. Kho tệp (S3) gọi ngoài transaction (Put), như ADR-46.
- 5.7 Ngữ cảnh shop: mọi tx là `tenancy.WithTenant(<--tenant>)`; chạy bằng `DATABASE_URL` (crm_app, RLS áp).
- 5.8 Nghiệp vụ không biết HTTP: thêm hàm module không nhập `net/http`.
- 5.9 Đường ghi qua HTTP: không áp dụng (lệnh vận hành một lần, không API). Chống trùng bằng khóa tự nhiên (id cũ).
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện mới: không.
- 5.12 Module mẫu: lệnh theo `seed-web-fixture`; hàm module theo `CreateCampaign`, `SubmitFeedback`.

Mục 6 (đúng khi đồng thời):

- 6.1 Hai bên cùng sửa: banner/bộ phối lưu cả danh sách theo `revision` (đọc rồi `SaveBanners(revision)`; nhân viên
  lưu xen giữa → `ErrRevisionConflict`, loại đó lỗi, chạy lại); cài đặt quà, tạm dừng, cài đặt hãng sửa theo `version`;
  chiến dịch, góp ý `INSERT … ON CONFLICT DO NOTHING`.
- 6.2 Chạy lại: khóa tự nhiên `id` (chiến dịch, banner, bộ phối, góp ý); cài đặt so giá trị trước khi ghi; lần hai 0
  thay đổi (test).
- 6.3 Thứ tự khóa: các loại chạy theo thứ tự nhóm ADR-46 tăng dần: góp ý (5) → nhận đơn web (7) → hãng (8) → chiến
  dịch, cài đặt quà (10: web_campaigns → birthday_settings) → tệp ảnh banner (12b, tx riêng) → banner, bộ phối (12d).
  Chạy khô gộp mọi loại vào **một** transaction rồi lùi (để banner thấy chiến dịch vừa thêm) — vẫn đúng thứ tự tăng
  dần nên không ngược ADR-46.
- 6.4 Lỗi tạm (deadlock, hết giờ chờ khóa): lệnh dừng loại đó, in lỗi, thoát khác 0; chạy lại an toàn (6.2).
- 6.5 Cách ly: mặc định READ COMMITTED; ghi đều có điều kiện (revision/version/ON CONFLICT).
- 6.6–6.8 Việc nền: chỉ xếp `file.process` (ảnh banner) qua `files.CompleteUpload`; lệnh chờ `ready` có trần giờ
  (`--image-wait`, mặc định 3 phút); hết giờ hay `rejected` → banner đó lỗi, phần còn lại vẫn nhập. Tiến trình chết
  giữa chừng: transaction tự lùi, chạy lại.
- 6.9 Treo lâu: dùng thời hạn của pool (30 s câu lệnh, 5 s chờ khóa); chờ ảnh có trần.
- 6.10 Tiền: phần trăm chiến dịch (int), cài đặt quà (int64 đồng, kiểm bằng luật của promotion).
- 6.11 Quy luật: không áp dụng rapid (lệnh một lần); thay bằng ca chạy lại 0 thay đổi, chạy khô không đổi database.
- 6.12 Dữ liệu cá nhân: in và log chỉ số đếm, mã, lý do; không SĐT, tên, nội dung góp ý, ghi chú (test đọc stdout và
  log). `unmatched.json` chỉ đếm theo loại, không in bản ghi.
- 6.13 Quyền: lệnh vận hành chạy trên máy chủ (người có quyền máy); audit `actor_type = 'system'`, `actor_id`
  là mã UUID v5 cố định của `import-website-data`, `request_id = 'import-website-data'`.

## Quyết định thay chủ shop

1. **`--tenant` nhận mã tenant (UUID)**: CRM không có "mã tenant" dạng chữ (`platform.tenants` chỉ có id, tên). Lệnh
   kiểm tenant có trong `tenancy.ListTenantIDs`. Khác: tìm theo tên (mơ hồ khi trùng).
2. **Người làm là tài khoản hệ thống không có dòng `identity.users`**: mã cố định UUID v5 của
   `system:import-website-data` ghi vào `created_by`/`updated_by` (các cột này không có khóa ngoại); audit `actor_type =
   'system'`, `actor_id` = chính mã UUID v5 đó, `request_id = 'import-website-data'`. Khác: tạo user thật (cần tạo tài khoản — chủ shop chưa OK).
3. **Sinh nhật không nhập** (quyết định điều phối 27/11): lệnh chỉ đếm `birthdays.profiles` vào dòng «bỏ qua — nhập bằng
   `scripts/import-birthdays-to-crm.ts`». Cài đặt quà nhập đúng số và `enabled` của website (kể cả khi website chưa
   lưu lần nào: website đang chạy bằng mặc định của nó).
4. **Cài đặt (quà, tạm dừng nhận đơn, hãng ưu tiên) không đè việc nhân viên đã làm trong CRM**: CRM chưa ai lưu
   (`UpdatedBy` nil) → ghi giá trị website; đã bằng → bỏ qua; nhân viên CRM đã lưu giá trị khác → bỏ qua và in «giữ của
   CRM». Khác: luôn đè (mất việc nhân viên làm trước đêm mở).
5. **Banner, bộ phối gộp theo `id`**: danh sách CRM đang có giữ nguyên thứ tự; bản ghi website chưa có thêm vào cuối
   theo thứ tự website; trùng `id` → bỏ qua. Quá 8 banner / 6 bộ trên trang chủ → bản ghi đó lỗi. Khác: thay cả danh
   sách (xóa việc nhân viên CRM đã làm).
6. **Ảnh bộ phối**: `{productId, imagePosition}` → ảnh thứ `imagePosition` (đếm từ 0) trong ảnh đang hiện của mẫu đó
   theo thứ tự CRM hiện (position, id) — website lấy ảnh đại diện Nhanh là 0, rồi các ảnh khác, cùng thứ tự CRM nhận ảnh.
   Không có ảnh ở vị trí đó → bộ đó lỗi. Ảnh riêng của một SKU (`variantId`) → bộ đó lỗi «chọn lại ảnh trong CRM»: CRM
   gắn ảnh theo màu chứ không theo SKU, đoán sẽ ra ảnh sai trên trang chủ. Khác: đoán theo màu của SKU.
7. **Danh mục theo đường tên** (`["Áo","Áo sơ mi"]`): so từng cấp với tên danh mục CRM đang dùng (cắt khoảng trắng hai
   đầu, so đúng từng ký tự, phân biệt hoa thường; không chuẩn hóa NFC: CRM chưa có hàm, thêm `x/text` chỉ cho việc này
   không đáng — tên khác dạng chỉ làm bản ghi lỗi, không khớp nhầm); 0 hay hơn 1 danh mục khớp → bản ghi đó lỗi, không đoán. Chiến dịch theo danh mục
   chỉ gắn đúng danh mục đó (CRM `CampaignCovers` vốn không áp danh mục con — đúng luật website). Tệp 466 không có id
   CRM của danh mục nên không dùng bảng chuyển mã.
8. **Chạy khô là một transaction lùi** cho mọi loại (theo thứ tự ADR-46), để banner trỏ chiến dịch vừa nhập kiểm được
   đúng. Ảnh banner khi chạy khô: chỉ kiểm tệp tại máy (có, ≤ 8 MiB, đúng đầu tệp WebP/JPEG/PNG), không tải lên kho;
   luật banner kiểm với một mã ảnh tạm coi như đã xử lý. Khác: tải ảnh lên khi chạy khô (để lại tệp mồ côi trên kho).
9. **Ảnh banner đi đường tải lên chuẩn** (`CreateUpload` → `FileStore.Put` vào khóa tạm → `CompleteUpload` xếp
   `file.process` → chờ `ready`), không phải `files.Register` như kế hoạch ghi: `Register` từ chối mục đích ảnh
   (ADR-45: ảnh phải qua imager). Lệnh `--apply` cần tiến trình CRM (worker) đang chạy để xử lý ảnh (RUNBOOK).
10. **Hãng trên web**: `configured: false` (website chưa chọn, mọi hãng hiện) → không đổi hãng hiện trên web, in «giữ
    của CRM»; nếu tệp vẫn có `preferred` (website xếp hãng đó lên đầu) → bật hiện trên web cho đúng hãng đó (CRM đòi
    hãng ưu tiên phải hiện trên web) rồi đặt ưu tiên như dưới.
    `configured: true`: mỗi hãng bật (`vtp`/`best`/`spx`) → mọi tài khoản đang hoạt động của hãng đó bật `show_on_web`;
    không có tài khoản nào → lỗi hãng đó. Không tắt hãng khác đang bật trong CRM (dễ đảo ngược, nhân viên tắt tay).
    Hãng ưu tiên: đúng một tài khoản đang hoạt động của hãng đó → đặt ưu tiên (giữ phí dự phòng); 0 hay nhiều → lỗi.
11. **Góp ý mang `id` đã có ở tenant khác** (`customer.feedback.id` là khóa chính toàn bảng) → bản ghi đó lỗi, không
    chạm tenant khác.
12. **Chiến dịch phần trăm vượt trần CRM** → lỗi chiến dịch đó (`ErrPercentTooHigh`), không tự hạ.
13. **`--apply` chạy trong container worker**: chỉ vai trò worker (và api) có cấu hình kho tệp; `--apply` ở vai trò
    khác thoát 2 «--apply cần kho tệp … CRM_ROLE=worker» trước khi mở database. Chạy khô chạy được ở mọi vai trò. Khác:
    dựng kho tệp chỉ khi có banner ảnh (khó đoán trước cho người vận hành).

## Dừng để hỏi

(không có)

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Thư mục mẫu → đúng số từng loại; chạy khô database không đổi (15 bảng đếm trước/sau), không ghi kho tệp; apply đúng dữ liệu (chiến dịch giữ id, enabled → paused, ngày tạo; danh mục theo đường tên; banner ảnh; bộ phối ảnh thứ 2 của P1; góp ý giữ id, trạng thái, ghi chú, gắn khách; quà, tạm dừng, hãng); chạy lại 0 thêm, không thêm audit/sự kiện; tenant khác không đổi; bảng, log không SĐT, nội dung, ghi chú | `TestImportWebsite_SampleDryRunApplyAgain` | `go test -race -count=1 -run '^(TestImportWebsite.*\|TestRun_ImportWebsiteCommandLine)$' ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 2.9s` |
| Một ảnh banner hỏng → banner đó lỗi, 3 banner và mọi loại khác vẫn nhập, thoát khác 0; chạy lại sau sửa ảnh chỉ thêm banner đó | `TestImportWebsite_BrokenBannerImage` | như trên | PASS |
| `version` lạ → thoát 2 trước khi mở database; trường lạ, thiếu data.json, cờ sai → thoát 2 | `TestImportWebsite_VersionAndUsage` | như trên | PASS |
| Đường thật của `run` (config từ biến môi trường, pool crm_app, kiểm tenant, dựng module): chạy khô thoát 0, database không đổi; `--apply` ở vai trò không có kho tệp → thoát 2, không ghi; `--apply` có kho tệp → ghi; tenant lạ, version 2, thiếu cờ → thoát 2; không in SĐT, nội dung | `TestRun_ImportWebsiteCommandLine` | như trên | PASS |
| Bộ phối trùng link key → chỉ bộ đó lỗi, không lùi cả loại (lỗi tìm ra khi chạy khô bản xuất thật) | `TestImportWebsite_LookLinkKeyClash` | như trên | PASS |
| Website chưa chọn hãng mà có ưu tiên → không tắt hãng nào, bật đúng hãng ưu tiên trên web và đặt ưu tiên; không ưu tiên → bỏ qua | `TestImportWebsite_CarriersNotConfiguredPreferred` | như trên | PASS |
| Danh mục: đúng một khớp đủ đường → mã; 0, 2, thiếu cấp cha → lỗi | `TestImportWebsite_CategoryPathResolve` | như trên | PASS |
| Chiến dịch nhập giữ id, link_key, paused, ngày tạo, đích; một sự kiện; nhập lại không thêm; enabled, vượt trần, trùng link_key → lỗi | `TestImportCampaign` | `go test -race -count=1 -run '^TestImportCampaign$' ./internal/modules/promotion/` | `ok … promotion 1.95s` |
| Góp ý nhập giữ id, giờ, trạng thái, ghi chú, gắn khách theo SĐT; nhập lại không thêm; sai → ErrInvalidInput; id của tenant khác → lỗi, không chạm; audit không nội dung, SĐT | `TestImportFeedback` | `go test -race -count=1 -run '^TestImportFeedback$' ./internal/modules/customer/` | `ok … customer 2.06s` |
| Luật kiến trúc (transaction ngoài RetryTx, sentinel có ánh xạ, ranh giới module) | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok … tests/arch 39.2s` |
| Cổng tĩnh + test gói đổi và gói phụ thuộc (cmd/crm, customer, promotion -race; httpapi, order, fulfillment, conversation… thường) | cổng kiểm của task | `ci-task.sh` | `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh b256382b…)`; run 45932809646: static, test (1), test (2) success |
| Chạy khô bản xuất thật của script 466 | test tạm, xóa sau khi chạy | xem «Bảng số chạy khô» | bảng 1 |

## Bảng số chạy khô

Chạy khô trên database thử riêng (database ngẫu nhiên của `testkit.OpenStaff` trên bộ chung crm-shared, tenant mẫu:
danh mục "Áo" > "Áo sơ mi", mẫu P1 hai ảnh, P2 một ảnh, khách SĐT có sẵn, tài khoản hãng vtp, best). Đếm 15 bảng trước/sau:
không đổi. Không chạy `--apply` trên dữ liệu thật hay dev dùng chung.

**1. Bản xuất thật của script 466** (`apps/storefront-crm` d5041fe, `exportAdminData` trên dữ liệu mẫu `dataDir()` của
test 466, đầu ra ở thư mục tạm của phiên, không ghi vào repo website; mẫu website P1, P2 thay bằng P1, P2 của CRM, P3–P5
cố ý không có trong CRM; ảnh banner mẫu của website là chữ, không phải ảnh; không có tài khoản hãng spx):

```
import-website-data: CHẠY KHÔ (đã lùi, không ghi gì), tenant 01a21af9-9c58-787e-a987-076c188976c1
loại                   thêm  bỏ qua   lỗi
góp ý                    20       0     0
tạm dừng nhận đơn         1       0     0
hãng trên web             1       0     2
chiến dịch                3       0     0
cài đặt quà               1       0     0
banner                    5       0     3
bộ phối                   1       0     7
sinh nhật: 30 hồ sơ không nhập ở lệnh này (nhập bằng scripts/import-birthdays-to-crm.ts của website), 1 từ chối không xuất
unmatched.json (không nhập): look 2
- hãng trên web: lỗi hãng spx: không có tài khoản hãng đang hoạt động
- hãng trên web: lỗi hãng ưu tiên spx: có 0 tài khoản đang hoạt động, cần đúng 1 (chọn trong CRM)
- chiến dịch: 1a83374d-e4ba-4cd7-ac72-1722bf1fa956 đang bật trên website → nhập ở tạm dừng: xem trước rồi bật lại
- banner: lỗi 00000000-0000-4000-8003-000000000006: tệp ảnh banner sai: banner-images/11111111111111111111111111111111.webp không phải ảnh WebP, JPEG, PNG
- banner: lỗi 00000000-0000-4000-8003-000000000007: tệp ảnh banner sai: banner-images/22222222222222222222222222222222.webp không phải ảnh WebP, JPEG, PNG
- banner: lỗi 00000000-0000-4000-8003-000000000008: danh mục Nhanh 3 (1 cấp): 0 danh mục CRM cùng đường tên, cần đúng 1
- bộ phối: lỗi 00000000-0000-4000-8004-000000000002: ảnh người mẫu không khớp ảnh sản phẩm trong CRM: ảnh riêng của một SKU, chọn lại ảnh trong CRM
- bộ phối: lỗi 00000000-0000-4000-8004-000000000003: ảnh người mẫu không khớp ảnh sản phẩm trong CRM: mẫu 00000000-0000-4000-8001-000000000003 có 0 ảnh, website chọn ảnh thứ 0 (từ 0)
- bộ phối: lỗi 00000000-0000-4000-8004-000000000004: bộ phối 2: mã rỗng, trùng hay trùng link: content: bộ phối không hợp lệ
- bộ phối: lỗi 00000000-0000-4000-8004-000000000005: ảnh người mẫu không khớp ảnh sản phẩm trong CRM: mẫu 00000000-0000-4000-8001-000000000004 có 0 ảnh, website chọn ảnh thứ 0 (từ 0)
- bộ phối: lỗi 00000000-0000-4000-8004-000000000006: ảnh người mẫu không khớp ảnh sản phẩm trong CRM: mẫu 00000000-0000-4000-8001-000000000005 có 0 ảnh, website chọn ảnh thứ 1 (từ 0)
- bộ phối: lỗi 00000000-0000-4000-8004-000000000007: bộ phối 2: mã rỗng, trùng hay trùng link: content: bộ phối không hợp lệ
- bộ phối: lỗi 00000000-0000-4000-8004-000000000008: bộ phối 2: mã rỗng, trùng hay trùng link: content: bộ phối không hợp lệ
```

Đọc: mọi lỗi là đúng luật (mẫu không có trong CRM, ảnh không phải ảnh, danh mục "Váy" không có, ảnh riêng SKU, hãng spx
không có tài khoản); bộ 4, 7, 8 lỗi vì mã mẫu của test website trùng 8 hex đầu `00000000` với bộ 1 (xem Việc phát hiện
thêm). Bảng không có SĐT, tên, nội dung góp ý.

**2. Thư mục mẫu của test CRM** (`cmd/crm/testdata/admin-export`):

```
import-website-data: CHẠY KHÔ (đã lùi, không ghi gì), tenant 01a21af9-9c58-787e-a987-076c188976c1
loại                   thêm  bỏ qua   lỗi
góp ý                     3       0     0
tạm dừng nhận đơn         1       0     0
hãng trên web             3       0     0
chiến dịch                3       0     0
cài đặt quà               1       0     0
banner                    4       0     0
bộ phối                   2       0     0
sinh nhật: 2 hồ sơ không nhập ở lệnh này (nhập bằng scripts/import-birthdays-to-crm.ts của website), 1 từ chối không xuất
unmatched.json (không nhập): campaign 1, look 1
- chiến dịch: a1000000-0000-4000-8000-000000000001 đang bật trên website → nhập ở tạm dừng: xem trước rồi bật lại
```

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Sinh nhật không nhập (điều phối 27/11); khóa góp ý là `id` thay `(created_at, client_hash)` (466 không xuất
  `client_hash`); ảnh banner qua đường tải lên thay `files.Register` (quyết định 9); chạy khô một transaction (quyết
  định 8); chưa chạy `--apply` trên bản sao dev (đêm nay chỉ chạy khô, điều phối 27/11): «chạy thử thật» của Step 4 là
  chạy khô bản xuất thật của script 466 trên dữ liệu mẫu của test website vào database thử riêng (bảng ở trên).
  `docs/RUNBOOK.md` mục «Nhập dữ liệu website đêm mở» có lệnh; chạy thật trên máy chủ ở M7.

## Việc phát hiện thêm

- (Reviewer, không bắt buộc) `pause` gọi `SetWebIntake` không bọc savepoint: nếu câu SQL làm hỏng transaction (không
  phải lỗi version), khi chạy khô các loại sau báo lỗi dây chuyền (dữ liệu không hại, vẫn lùi). Bọc `db.Savepoint` như
  loại khác khi có task sửa sau.
- (Reviewer, không bắt buộc) `lookPhotoFile` đếm mọi ảnh của mẫu theo `position` kể cả ảnh chưa `ready`; nếu CRM có
  ảnh chưa xử lý xen giữa, vị trí có thể lệch so với ảnh website chọn. Rủi ro thấp; kiểm lại khi chạy thật ở M7.
- `customer`: khối gắn khách theo SĐT (`NormalizePhone` + `identityOwner`, 10 dòng) có ở cả `SubmitFeedback` và
  `ImportFeedback`; cùng mục đích, cùng lý do đổi → nên tách một hàm riêng tư (commit cấu trúc riêng).
- Chạy khô bản xuất thật của script 466 (mẫu của test website) cho thấy mã bộ phối mẫu `00000000-…-8004-…` trùng link
  key 8 hex: CRM từ chối (luật content), website thì nhận. Website thật dùng UUID ngẫu nhiên nên khó gặp; nếu gặp, bộ
  đó lỗi riêng và nhân viên tạo lại trong CRM. Chưa sửa ở website (ngoài phạm vi).

## E2E viết sẵn, chưa chạy

Không có: lệnh dòng lệnh, đủ ca ở test của `cmd/crm` trên PostgreSQL thật.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent `reviewer`, trên phần đã stage sau khi gộp main b256382b).

- Phát hiện phải sửa: không có.
- Ảnh hưởng: khớp; `runDevCommand` chỉ còn gọi từ `runOneOffCommand`; không ký hiệu cũ nào đổi. Gợi ý thêm dòng
  RUNBOOK, testdata vào mục Ảnh hưởng (đã có ở Khác kế hoạch).
- Bằng chứng: từng test kiểm đúng tiêu chí; «thoát khác 0» của ảnh hỏng ghép từ `rep.Failed()` và `importWebsiteExit`,
  đủ vì `runImportWebsite` trả `errImportIncomplete` khi có lỗi.
- Luật dự án: không so lỗi bằng chuỗi, log một lần chỉ số đếm, không lộ SĐT/nội dung, SQL qua sqlc, không thư viện mới.
- Ghi chú không bắt buộc (không sửa, ghi ở Việc phát hiện thêm): savepoint cho `pause`; `lookPhotoFile` không lọc
  ảnh chưa `ready`; câu chữ actor_id (đã sửa trong tệp task cho khớp mã).
