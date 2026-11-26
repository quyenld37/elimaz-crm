# Task 511 — Seed e2e: hãng giả trên web, thứ tự mẫu/biến thể; thứ tự biến thể cho website

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 511 do điều phối M5 cấp 27/11, sau Task 218; nguồn: `docs/tasks/task-218.md` mục «Khác kế hoạch», «Việc phát hiện thêm» và bảng «Tên hãng»/«Số tiền phụ thuộc phí ship» ở `apps/storefront-crm/docs/tasks/task-218.md`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Phần (1) — thứ tự biến thể trên web: **kiểm, không đổi mã**. Catalog không có khái niệm thứ tự biến thể do nhân viên
  đặt: bảng `catalog.variants` (0114, 0502) không có cột vị trí; màn hình nhân viên (`ListProductVariants … ORDER BY id`)
  và API web (`queries/webcatalog/products.sql` `VariantsOf … ORDER BY v.product_id, v.variant_id`) cùng xếp theo id =
  uuidv7 = thứ tự tạo. Với dữ liệu thật website hiện đúng thứ tự nhân viên thấy ở CRM (không phải lỗi lộn xộn); chỉ dữ
  liệu seed (id UUID v5) lộn. Thêm thứ tự do nhân viên đặt là thiết kế dữ liệu → «Dừng để hỏi».
- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**:
  - `catalog.ProductInput`: trường `CreatedAt time.Time` (rỗng → `now()` như mặc định cột). Struct literal có tên trường
    nên chỗ dùng (`grep catalog.ProductInput{`: httpapi, importer, test, shoptest, invtest, loadkit, cmd/crm) không phải sửa.
  - `tools/fakecarrier/server.Options`: trường `QuoteFee int64` (0 → công thức cũ); `tools/fakecarrier` đọc biến
    `FAKECARRIER_QUOTE_FEE`. Chỗ dùng `server.New(server.Options{…})` (5 tệp test + main) có tên trường, không phải sửa.
  - `fulfillment`: kiểu `SeedLocation`, hàm `EnsureSeedLocations(ctx, tx, carrierKey, []SeedLocation) (int, error)`
    (ghi mã địa giới thử cho hãng với mã hãng = mã CRM, chỉ dòng thiếu/lệch; cho seed và test, cùng khuôn
    `EnsureCarrierAccount`); `LocationMap` dùng chung hàm nội bộ mới `rowsOf`, `locationsCommand.OnDone` dùng chung hàm nội bộ mới
    `upsertLocations` (sắp theo (level, our_code), rỗng không ghi; không đổi hành vi). Không sentinel mới.
  - `cmd/crm` (gói main): cờ `--web-carrier`, `--web-carrier-fee` của `seed-web-fixture`; bước `webCarrier` trong
    `seed_web_fixture_apply.go` (tệp mới `seed_web_fixture_carrier.go`); `webFixtureSeeder` thêm `Fulfillment
    *fulfillment.Service`, `Carrier webCarrierSeed`; mẫu tạo với `CreatedAt = mốc cố định + position giây`.
- Gói phụ thuộc ngược (go list): `catalog` ← `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`,
  `internal/modules/content`, `inventory/invtest`, `order`, `testkit/shoptest`, `wiring/webcache`, `tools/loadkit`
  (không chỗ nào phải sửa); `tools/fakecarrier/server` ← `tools/fakecarrier`, test của `internal/httpapi`,
  `internal/adapters/carrier/carriertest`, `internal/modules/fulfillment` (không phải sửa); `fulfillment` (chỉ thêm) ←
  `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `internal/adapters/carrier/**`, `cash`, `order` (test),
  `testkit/shiptest` (không phải sửa).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không migration. Sửa câu `InsertProduct` (`queries/catalog/products.sql`): cột
  `created_at = coalesce(sqlc.narg(created_at), now())` (đúng mặc định cột của 0114); `store.InsertProductParams` thêm
  `CreatedAt pgtype.Timestamptz`. Mã sinh lại: `internal/modules/catalog/internal/store/products.sql.go`. Fulfillment:
  public API có sẵn (`EnsureCarrierAccount`, `GetAccount`, `UpdateAccount`, `GetWebCarrierSettings`,
  `SaveWebCarrierSettings`) và hàm mới `EnsureSeedLocations` (câu có sẵn `ListCarrierLocationCodes`,
  `UpsertCarrierLocations`) trên `fulfillment.carrier_accounts`, `carrier_locations`, `channel_carrier_settings`.
- Tài liệu: `docs/RUNBOOK.md` mục «Danh mục mẫu cho e2e website» thêm hai cờ và biến `FAKECARRIER_QUOTE_FEE`;
  `docs/superpowers/plans/danh-so-task-toan-cuc.md` thêm dòng Task 511.
- Test sẽ chạy: tệp mới `cmd/crm/seed_web_fixture_carrier_test.go` (`TestSeedWebFixture_WebCarrier`,
  `TestSeedWebFixture_WebCarrierFailureRollsBack`, `TestSeedWebFixture_CreatedAtOrder`,
  `TestRunSeedWebFixture_WebCarrierFlags`), `TestRunSeedWebFixture_Command` thêm lượt `--web-carrier`; `catalog`
  `TestCreateWithFixedCreatedAt` (`fixedid_test.go`); `fulfillment` `TestEnsureSeedLocations` (tệp mới
  `seed_locations_test.go`); `tools/fakecarrier/server` `TestQuoteFixedFee` (tệp mới `quote_fee_test.go`);
  `tools/fakecarrier` `TestLoadConfig` thêm ca `FAKECARRIER_QUOTE_FEE`; gói đổi và gói phụ thuộc qua `ci-task`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md`; lệnh `seed-dev`
  (`fulfillment.EnsureCarrierAccount` + `fake.CarrierKey`, `inventory.EnsureDefaultWarehouse`); fulfillment
  `Service.UpdateAccount` (`AccountPatch{Name, ShowOnWeb}`), `Service.SaveWebCarrierSettings`, `GetWebCarrierSettings`,
  `AccountView.AuditFields`, `WebCarrierSettings.AuditFields`; `identity.RecordAudit` (cùng tên hành động như handler
  HTTP: `fulfillment.carrier_account.update`, `fulfillment.web_carrier_settings.update`); `mods.M3.Fulfillment.Service`
  của `newModules` (Service có sổ sự kiện, báo hiệu như tiến trình thật); catalog `ProductInput.ID` (Task 218, cùng
  khuôn cho `CreatedAt`).
- Dùng lại / viết mới vì:
  - Hãng giả: dùng adapter `fake` của M3 (không thêm hãng); tài khoản riêng `external_account_ref = web-e2e` (không
    đổi tên `fake-1` của `seed-dev`). Mọi ghi qua public API của fulfillment, không SQL tay.
  - Viết mới `CreatedAt` trong `ProductInput`: catalog không có cách nào đặt `created_at`; khuôn giống `ID` (rỗng = như
    cũ, đường HTTP không đặt).
  - Viết mới `fulfillment.EnsureSeedLocations`: hãng giả khai khả năng địa giới nên báo phí đòi mã đã khớp; đồng bộ
    thật (`SyncLocations`) cần khóa API, worker và tên trùng danh mục hãng — địa giới thử của tệp không có ở đó. Dùng
    lại hai câu sqlc có sẵn; hàm nội bộ `rowsOf` gộp đoạn dựng `locations.Row` của `LocationMap` (tránh lặp).
  - Viết mới `QuoteFee` ở hãng giả: công thức phí theo cân nặng + 0,5 % COD không cho phí cố định 30.000 mà test website
    chờ; một nhánh nhỏ trong `quote`, mặc định giữ công thức cũ.

## Soát thiết kế

- 5.1 Không dữ liệu mới: tài khoản hãng, cài đặt hãng trên web thuộc `fulfillment`; `created_at` của mẫu thuộc
  `catalog`. 5.2 `cmd/crm` chỉ gọi public API. 5.3 Đọc qua `GetAccount`, `GetWebCarrierSettings`. 5.4 Vẫn một
  transaction tenant cho cả lệnh (lỗi ở bước hãng → lùi cả mẫu, mã, tồn). 5.5 `UpdateAccount`,
  `SaveWebCarrierSettings` tự phát `carrier.web_settings_changed`/báo hiệu (consumer webcache làm mới vùng carriers,
  shipquote). 5.6 Lệnh seed không gọi hãng (chỉ ghi cài đặt); báo phí lúc chạy đi đường `QuoteForWeb` có sẵn. 5.7 Mọi
  câu trong `WithTenant`. 5.8 Module không nhập HTTP; hãng giả là công cụ dev riêng. 5.9 Không đường HTTP mới; lệnh so
  rồi mới ghi (chạy lại = 0 thay đổi). 5.10 Không Valkey. 5.11 Không thư viện mới. 5.12 Theo khuôn `seed-dev` và bước
  có sẵn của `seed-web-fixture`.
- 6.1 `UpdateAccount`, `SaveWebCarrierSettings` khóa dòng rồi so version (đọc version trong cùng tx). 6.2 Chạy lại:
  so tên, `show_on_web`, ưu tiên, phí dự phòng; giống thì không ghi. 6.3 Thứ tự khóa ADR-46: bước hãng đặt sau catalog
  (nhóm 4) và trước promotion (10), inventory (11): `inventory.EnsureDefaultWarehouse` chỉ `INSERT … ON CONFLICT DO
  NOTHING` + đọc không khóa (như `seed-dev`), rồi `carrier_accounts` → `carrier_locations` (upsert theo (level,
  our_code) tăng dần) → `channel_carrier_settings` (nhóm 8, đúng thứ tự trong nhóm). 6.4–6.5 `WithTenant` dùng `RetryTx`; thân hàm dựng mới `fixtureApply` mỗi lần. 6.6–6.8 Không việc nền
  mới. 6.9 Thêm ~6 câu. 6.10 Phí `int64` đồng, kiểm 0–`fulfillment.MaxWebFallbackFee` trước khi mở database (cờ sai →
  thoát 2); hãng giả kiểm `FAKECARRIER_QUOTE_FEE` > 0. 6.11 Không vùng quy luật mới. 6.12 Không in email; tên hãng là
  tên cửa hàng đặt, không phải dữ liệu cá nhân. 6.13 Lệnh vẫn chặn production; hãng giả chỉ nối khi `CRM_ENV` ≠
  production và có `FAKECARRIER_URL` (không đổi).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (1) Thứ tự biến thể: CRM có thứ tự do nhân viên đặt không, API web có trả khác không | đọc mã: `catalog.variants` không cột vị trí (0114, 0502); `queries/catalog/variants.sql` `ListProductVariants … ORDER BY id`; `queries/webcatalog/products.sql` `VariantsOf … ORDER BY v.product_id, v.variant_id` | `grep -rn "ALTER TABLE catalog.variants" migrations/` | chỉ `ADD COLUMN weight_grams` (0502): không có thứ tự → «Dừng để hỏi» |
| `--web-carrier`: tài khoản hãng giả đúng tên, hiện trên web, ưu tiên, phí dự phòng; báo phí cho web (QuoteForWeb, đường thật của website, hãng giả `tools/fakecarrier` chạy trong httptest với phí cố định) = đúng một lựa chọn «Hãng giao hàng A» 30.000, không phải ước tính; audit 2 dòng; chạy lại 0; đổi tên/phí sửa đúng phần đó (audit 4, sự kiện `carrier.web_settings_changed` 2); một tài khoản; không cờ → không tài khoản | `TestSeedWebFixture_WebCarrier` | `go test -race -count=1 -run '^(TestSeedWebFixture_.*\|TestRunSeedWebFixture_.*\|TestRun_SeedWebFixtureCommandLine\|TestParseWebFixture_.*)$' ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 3.543s` |
| Lỗi ở bước hãng (sau mẫu) → lùi cả transaction | `TestSeedWebFixture_WebCarrierFailureRollsBack` (phí quá trần qua seeder → `ErrSettingOutOfRange`; mẫu 0, tài khoản hãng 0) | như trên | ok |
| Cờ sai → thoát 2 trước khi đọc tệp (phí −1, 500001, `x`, phí không kèm tên, tên rỗng) | `TestRunSeedWebFixture_Refusals` (5 ca mới trong vòng `usage`) | như trên | ok |
| Đường lệnh `runSeedWebFixture` với `--web-carrier` (newModules, Service của M3) in `sửa carrier_account 1, carrier_location 4, web_carrier_settings 1;`, cài đặt có phí 30000 | `TestRunSeedWebFixture_Command` (lượt 3) | như trên | ok |
| Ngày tạo mẫu = 2026-01-01 + position giây; «Mới nhất» đứng đầu là position lớn nhất | `TestSeedWebFixture_CreatedAtOrder` | như trên | ok |
| Seed cũ không đổi (tạo/sửa/chạy lại, lùi, địa giới trùng, lệnh thật) | `TestSeedWebFixture_CreateRerunUpdate`, `TestSeedWebFixture_AdminUnitConflict`, `TestSeedWebFixture_FailureLateRollsBackAll`, `TestRun_SeedWebFixtureCommandLine` | như trên | ok |
| catalog: `CreatedAt` đặt trước giữ đúng giờ; rỗng = `now()` của database (một đồng hồ) | `TestCreateWithFixedCreatedAt` (+ `TestCreateWithFixedID` không đổi) | `go test -race -count=1 -run '^(TestCreateWithFixedCreatedAt\|TestCreateWithFixedID)$' ./internal/modules/catalog/` | `ok … 3.393s` |
| fulfillment: mã địa giới thử: lần đầu 4, LocationMap tra được, chạy lại 0, mã lệch sửa 1, hãng khác/tenant khác không thấy; đồng bộ thật sau khi tách `upsertLocations`, `rowsOf` không đổi | `TestEnsureSeedLocations`, `TestLocations_SyncAndRerun`, `TestLocations_CursorContinues`, `TestLocations_CarrierErrors`, `TestLocations_Refusals` | `go test -race -count=1 -run '^(TestEnsureSeedLocations\|TestLocations_SyncAndRerun\|TestLocations_CursorContinues\|TestLocations_CarrierErrors\|TestLocations_Refusals)$' ./internal/modules/fulfillment/` | `ok … 2.802s` |
| Hãng giả: `QuoteFee` → một dịch vụ std đúng phí bất kể cân nặng/COD/thử hàng; hỏi fast → rỗng; 0 → công thức cũ; `FAKECARRIER_QUOTE_FEE` đọc đúng, `0`, `-1`, `3e4`, `x` → lỗi | `TestQuoteFixedFee`, `TestLoadConfig` | `go test -race -count=1 -run '^(TestQuoteFixedFee\|TestLoadConfig)$' ./tools/fakecarrier/...` | `ok …/tools/fakecarrier 1.529s`, `ok …/server 1.995s` |
| Cổng tĩnh tự soát | — | `make jscpd`; `golangci-lint run ./cmd/crm/... ./internal/modules/catalog/... ./internal/modules/fulfillment/... ./tools/fakecarrier/...`; `go test -count=1 ./tests/arch/`; `make gen` + `git status` | `Found 0 clones.`; `0 issues.`; `ok … 94.591s`; mã sinh chỉ `products.sql.go` (đã có trong diff) |

CI_RESULT: lượt 1 (ci-task sau khi gộp main c3dd185f, bể blacksmith, run 45890826132): `static: success`,
`test (1): success`, `test (2): success` — `REMOTE-TEST XANH`, `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách
nhánh c3dd185f…)`.

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

Không có test e2e mới ở CRM (lệnh có test Go trên PostgreSQL thật ở trên). Bộ e2e website với nguồn CRM
(`playwright.crm.config.ts`) vẫn để tới đóng mốc; lệnh chạy cần thêm `--web-carrier "Hãng giao hàng A"` và hãng giả
`FAKECARRIER_QUOTE_FEE=30000` (xem RUNBOOK).

## Khác kế hoạch

1. **Ảnh sản phẩm: không làm** (như giao): cần kho tệp + imager trên máy e2e; lệnh vẫn in "ảnh chưa seed: N".
2. **Thứ tự biến thể theo tệp: không làm** vì catalog không có khái niệm thứ tự (xem «Dừng để hỏi»).
3. **Ngày tạo chỉ đặt khi tạo mẫu**: mẫu đã có từ lần seed trước (Task 218) giữ ngày tạo cũ — catalog không có API sửa
   ngày tạo và không thêm đường sửa; cần đúng thứ tự «Mới nhất» thì seed vào database mới (ghi ở RUNBOOK).
4. **Thêm `fulfillment.EnsureSeedLocations`** (ngoài phạm vi chữ của giao việc): không có nó, hãng giả báo phí trả
   `address_unmapped` và web chỉ nhận phí ước tính không tên hãng (đã thấy ở lần chạy test đầu).

## Quyết định thay chủ shop

1. **Phí cố định 30.000 nằm ở hãng giả** (`FAKECARRIER_QUOTE_FEE`), không đổi công thức mặc định. Phương án khác: chỉ
   đặt phí dự phòng 30.000 (web thấy "phí ước tính" không tên hãng → test chọn theo tên "Hãng giao hàng A" hỏng); đổi
   công thức của hãng giả (làm đổi số của test M3). Dễ đảo: bỏ biến là về công thức cũ. Seed vẫn đặt phí dự phòng =
   phí để web báo được phí khi hãng giả không chạy.
2. **Tài khoản hãng riêng `web-e2e`**, không đổi tên `fake-1` của `seed-dev`; là **ưu tiên** trên web (radio đang tích
   đầu tiên là hãng này). Phương án khác: dùng `fake-1` (đổi tên tài khoản dev nhân viên đang thấy là "Hãng giả").
3. **Mã địa giới thử của hãng giả = mã CRM** qua hàm seed mới của fulfillment (mã hãng giả trùng mã CRM là quy ước có
   sẵn của `tools/fakecarrier`). Phương án khác: chạy đồng bộ thật — cần khóa API, `FIELD_ENC_KEYS`, worker và thêm địa
   giới thử vào danh sách tĩnh của hãng giả; nặng và gắn hãng giả với dữ liệu website.
4. **`createdAt = 2026-01-01 00:00 UTC + position giây`** đúng đặc tả website («mốc cố định + position giây»; position
   lớn hơn là mới hơn), qua trường tùy chọn `ProductInput.CreatedAt` (khuôn của `ID`, Task 218; đường HTTP không đặt).
5. **Audit của bước hãng** ghi bằng nhân viên `--staff-email`, cùng tên hành động như màn hình
   (`fulfillment.carrier_account.update`, `fulfillment.web_carrier_settings.update`).

## Dừng để hỏi

1. **Thứ tự biến thể do nhân viên đặt (thiết kế dữ liệu).** Kiểm 27/11: CRM không có thứ tự biến thể; màn hình nhân
   viên và API web cùng xếp theo id = uuidv7 = **thứ tự tạo**. Với dữ liệu thật, website hiện đúng thứ tự nhân viên
   thấy ở CRM (không phải lỗi lộn xộn ảnh hưởng khách ngay), nhưng biến thể thêm sau (vd thêm cỡ XS sau S/M/L) luôn đứng
   cuối và nhân viên không sắp lại được. Muốn sửa cần chủ shop chọn: (a) thêm cột `position` cho biến thể + màn kéo
   thả, web `ORDER BY position, id`; (b) xếp cố định theo bảng cỡ chuẩn (XS < S < M < L < XL…) ở API web; (c) giữ như
   nay. Seed e2e (id UUID v5) chỉ theo tệp được khi có (a).

## Cho website

- Hãng giao hàng: chạy CRM với `--web-carrier "Hãng giao hàng A"` (mặc định phí 30000) và hãng giả
  `FAKECARRIER_QUOTE_FEE=30000`, `FAKECARRIER_URL` cho api/worker → trang đặt hàng có đúng một lựa chọn «Hãng giao hàng
  A» 30.000, đang là ưu tiên. Không cần đổi test chọn theo tên.
- «Mới nhất»: `createdAt` của mẫu = 2026-01-01T00:00:00Z + `position` giây (chỉ với database seed mới).
- Thứ tự biến thể: vẫn theo id CRM (UUID v5 với dữ liệu seed) tới khi chủ shop chọn ở «Dừng để hỏi»; test website
  không nên giả định thứ tự cỡ/màu của tệp.

## Việc phát hiện thêm

1. Chú thích của `VariantsOf` (`queries/webcatalog/products.sql`) ghi "theo thứ tự tạo" — đúng vì id là uuidv7, nhưng
   sai với id đặt trước (seed); nếu chọn (a) ở «Dừng để hỏi» thì sửa cùng lúc.

## Reviewer

- **Vòng 1: ĐẠT.** Không có phát hiện phải sửa. Đã kiểm: phần (1) tự grep lại (`variants.sql:16 ORDER BY id`,
  `webcatalog/products.sql ORDER BY v.product_id, v.variant_id`, 0114/0502 không cột vị trí) — «Dừng để hỏi» đúng;
  `TestSeedWebFixture_WebCarrier` đi qua `QuoteForWeb` thật tới `tools/fakecarrier` trong httptest (không bản giả);
  lùi transaction, cờ sai, lệnh thật, ngày tạo, `EnsureSeedLocations`, phí cố định đều kiểm đúng tiêu chí; test cũ chỉ
  thay đoạn parse+seed bằng `seedSample()`, assertion giữ nguyên. Ghi chú không bắt buộc: (a) tách `upsertLocations`
  là đổi cấu trúc chung commit với đổi hành vi — hành vi không đổi, đã thêm tên vào mục Ảnh hưởng; (b)
  `EnsureDefaultWarehouse` (nhóm 11, chỉ chèn nếu thiếu) trước `carrier_accounts` (nhóm 8) như `seed-dev`, đã giải
  thích ở 6.3; (c) `synced_at` của địa giới thử lấy giờ máy — chỉ dữ liệu thử.
