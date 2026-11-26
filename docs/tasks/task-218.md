# Task 218 — Bộ e2e website với CRM — phần CRM: lệnh seed-web-fixture

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 218 = M5-35, **chỉ phần CRM**: lệnh `crm seed-web-fixture`; phần website đã xong ở `apps/storefront-crm` nhánh `crm/m5`, commit 65e5504, đặc tả tệp ở `docs/tasks/task-218.md` của website mục «Định dạng tệp fixture cho CRM»). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**:
  - `catalog`: trường `ID uuid.UUID` trong `CategoryInput`, `ProductInput`, `VariantInput` (uuid.Nil → database tự
    sinh uuidv7 như cũ). Struct literal có tên trường ở 45 chỗ (`grep catalog.{Product,Variant,Category}Input{`) không
    phải sửa; `normalizeVariant`, `applyVariantPatch` (nội bộ) giữ `ID`.
  - `customer`: hàm `EnsureFixtureAdminUnits(ctx, tx, []AdminUnit) (inserted, conflicts, err)` (chèn đơn vị hành
    chính thử khi chưa có mã; mã đã có khác cấp/tên/cha → trả trong `conflicts`, không chèn gì). Dùng kiểu `AdminUnit`
    có sẵn; không thêm sentinel (lỗi chỉ có ở lệnh dòng lệnh, `errAdminUnitConflict` của `cmd/crm`; sentinel module mới
    sẽ phải có dòng ánh xạ HTTP vô nghĩa theo `tests/arch/errors_mapped_test.go`). Chỉ chạy được bằng chủ bảng.
  - `cmd/crm` (gói main): lệnh `seed-web-fixture`, tệp mới `seed_web_fixture.go` (đọc, kiểm tệp, nối, `runDevCommand`
    gộp nhánh service-key và seed-web-fixture để `run` không quá dài) và `seed_web_fixture_apply.go` (ghi qua public
    API); `main.go` thêm lệnh vào hai `switch` của `run` (thay nhánh `isServiceKeyCommand`).
- Gói phụ thuộc ngược (go list): `catalog` ← `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`,
  `internal/modules/content`, `inventory/invtest`, `order`, `testkit/shoptest`, `wiring/webcache`, `tools/loadkit`;
  `customer` ← `cmd/crm`, `httpapi`, `httpapi/web`, `conversation/**`, `order`, `promotion`, `testkit/{livetest,shiptest}`.
  Không chỗ nào phải sửa (chỉ thêm trường có giá trị không mặc định cũ).
- Tài liệu: `docs/RUNBOOK.md` mục mới «Danh mục mẫu cho e2e website (`crm seed-web-fixture`, Task 218)».
- Operation OpenAPI bị chạm, oasdiff: không (lệnh dòng lệnh, không có đường HTTP).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không migration. Sửa câu `InsertProduct`, `InsertVariant` (`queries/catalog/products.sql`,
  `variants.sql`), `InsertCategory` (`categories.sql`): cột `id` = `coalesce(nullif(@id, uuid rỗng), uuidv7())` (đúng
  mặc định của cột ở 0114, 0120; `store.Insert*Params` thêm trường `ID uuid.UUID`, rỗng = như cũ). Câu mới `InsertFixtureAdminUnit` (`queries/customer/admin_units.sql`,
  `ON CONFLICT (code) DO NOTHING`) — bảng `customer.admin_units` không tenant, crm_app chỉ SELECT nên chỉ chủ bảng ghi
  được. Mã sinh lại: `internal/modules/catalog/internal/store/{products,variants,categories}.sql.go`,
  `internal/modules/customer/internal/store/admin_units.sql.go`.
- Test sẽ chạy: test mới `cmd/crm/seed_web_fixture_test.go` (PostgreSQL thật, `testkit.OpenStaff`,
  `testkit.StartPostgres`), test mới ở `catalog` (`fixedid_test.go`: `TestCreateWithFixedID`) và `customer`
  (`adminunits_test.go`: `TestEnsureFixtureAdminUnits`); tệp mẫu `cmd/crm/testdata/web-fixture.json` (sinh bằng
  `npx jiti scripts/export-fixture-for-crm.ts` ở apps/storefront-crm nhánh crm/m5, commit b1ef7e3, không sửa); gói catalog, customer, cmd/crm và
  gói phụ thuộc qua `ci-task` (`tests/arch` cho luật SQL, transaction).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`tenancy.WithTenant`,
  `db.RetryTx`, `identity.LoadUser`, `textnorm.Fold`); lệnh có sẵn `seed-dev` (`openOwner`, `errSeedRole`),
  `service-key` (`flag.FlagSet`, chặn production, `serviceKeyExit`), `newModules` (sổ sự kiện có đủ consumer, kể cả
  `webcache`); catalog `Writer.{CreateCategory,UpdateCategory,CreateProduct,UpdateProduct,AddVariant,UpdateVariant,
  CreateAttribute,AddAttributeValue,SetClassification}`, `GetProduct`, `ListCategories`, `ListAttributes`,
  `GetClassification`, `EnsureDefaultSeller` (như `seed-dev`); inventory `EnsureDefaultWarehouse`, `Availability`, `CreateDocument`, `ReplaceLines`,
  `Writer.PostDocument` (phiếu `adjust`); promotion `ListCoupons`, `Writer.{CreateCoupon,UpdateCoupon}`;
  `testkit.SeedAdminUnits` (chỉ cho test, mã có chữ T).
- Dùng lại / viết mới vì:
  - Mọi ghi đi qua các hàm public trên (không SQL viết tay trên bảng của module khác). Viết mới chỉ: thêm `ID` vào ba
    kiểu đầu vào (website tính id CRM bằng UUID v5 từ id fixture — không có cách nào khác để tạo bản ghi đúng id);
    `customer.EnsureFixtureAdminUnits` (chưa có hàm ghi địa giới: danh mục chỉ do migration ghi; `testkit.SeedAdminUnits`
    là mã test, không gọi được từ lệnh chạy thật).
  - Đọc JSON bằng `encoding/json` với `DisallowUnknownFields`; UUID v5 bằng `uuid.NewSHA1` (đã có trong `google/uuid`).

## Soát thiết kế

- 5.1 Không thêm dữ liệu mới: mẫu, biến thể, danh mục, thuộc tính thuộc `catalog`; tồn thuộc `inventory`; mã thuộc
  `promotion`; địa giới thuộc `customer` (bảng tham chiếu). 5.2 Lệnh ở `cmd/crm` chỉ gọi public API của từng module;
  module không gọi nhau mới. 5.3 Đọc qua hàm public (`GetProduct`, `Availability`, `ListCoupons`…). 5.4 Một
  transaction tenant (`tenancy.WithTenant` trên pool chủ bảng như `seed-dev`) cho cả danh mục, mẫu, mã, tồn, địa giới:
  lỗi giữa chừng → không ghi gì. Thân hàm chạy lại được trong `RetryTx`: bộ đếm gán lại đầu thân hàm, in kết quả sau
  commit. 5.5 Sự kiện `product.updated`, `price.changed`, `stock.changed` do Writer của module phát qua sổ sự kiện của
  `newModules` (đủ consumer như khi nhân viên sửa, gồm `webcache` → làm mới bộ đệm web). 5.6 Không gọi ra ngoài. 5.7
  Mọi câu chạm bảng có `tenant_id` trong `WithTenant`; `admin_units` không có tenant. 5.8 Module không nhập HTTP. 5.9
  Không có đường ghi HTTP; lệnh tự chống trùng bằng "so rồi mới ghi" (chạy lại cùng tệp = 0 thay đổi). 5.10 Không dùng
  Valkey. 5.11 Không thư viện mới. 5.12 Theo mẫu lệnh `seed-dev`, `service-key`.
- Chạy lệnh thật: chỉ trên database thử riêng (testkit), không trên crm-shared (xem bảng bằng chứng).
- 6.1 Sửa có version: đọc version hiện tại rồi gọi `Update*` trong cùng tx (khóa dòng, so version) — nhân viên sửa cùng
  lúc → `ErrVersionConflict` → lệnh lỗi, tx lùi, chạy lại. 6.2 Chạy lại = so từng trường, không đổi gì; tồn đặt tuyệt
  đối (delta = số trong tệp − có thể bán), không cộng dồn. 6.3 Thứ tự khóa theo ADR-46: nhóm 4 catalog (danh mục →
  thuộc tính → giá trị → mẫu theo id tăng dần, mỗi mẫu rồi biến thể của nó) → nhóm 10 promotion (mã) → nhóm 11 inventory
  (một phiếu điều chỉnh, `PostDocument` khóa số dư theo variant_id tăng dần). 6.4–6.5 `WithTenant` dùng `RetryTx` mặc
  định. 6.6–6.8 Không việc nền; tiến trình chết giữa chừng → tx lùi. 6.9 Một transaction ~ vài trăm câu (9 mẫu, 39 biến
  thể) — dưới trần 60 s; tệp lớn (`--extra-products` ≤ 400) vẫn một tx, chấp nhận ở dev. 6.10 Giá `int64` đồng, kiểm
  > 0 và ≤ `catalog.MaxMoney`. 6.11 Không vùng quy luật mới (tồn đi qua `PostDocument` có sẵn). 6.12 Log và stdout chỉ in
  số đếm, mã tenant; không in email nhân viên (`--staff-email`), không SĐT. 6.13 Lệnh chỉ chạy khi `CRM_ENV` ≠
  `production` (kiểm trước khi đọc tệp, trước khi mở database) và `CRM_ROLE=migrate` (chủ bảng); người làm audit là
  nhân viên `--staff-email` (phải có trong tenant).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tạo người bán (mặc định), danh mục, mẫu, biến thể, thuộc tính, phân loại, mã giảm, tồn, địa giới **đúng id** của tệp, qua public API, một transaction | `TestSeedWebFixture_CreateRerunUpdate` (`checkFirstReport`: tạo category 6, product 9, variant 39, attribute 7, attribute_value 11, coupon 1, admin_unit 4; tồn 38 biến thể; `checkSeededProduct`, `checkSeededRest` đọc lại bằng crm_app) | `go test -race -count=1 -run 'SeedWebFixture\|ParseWebFixture' ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 2.546s` |
| Chạy lại cùng tệp → 0 thay đổi (không sự kiện, audit mới) | `TestSeedWebFixture_CreateRerunUpdate` (lần 2), `TestRunSeedWebFixture_Command` (in `tạo 0; sửa 0;`) | như trên | ok |
| Tệp đã đổi giá/tồn (WB02) → sửa đúng 1 biến thể + 1 tồn, có `price.changed`, tồn tuyệt đối | `TestSeedWebFixture_CreateRerunUpdate` (lần 3, 4) | như trên | ok |
| `CRM_ENV=production` → từ chối trước khi đọc tệp, mở database, thoát 2 | `TestRunSeedWebFixture_Refusals` | như trên | ok |
| version lạ, tệp hỏng (28 kiểu) → thoát 2, không ghi gì | `TestParseWebFixture_Rejects`, `TestRunSeedWebFixture_Refusals` (version 2 qua lệnh) | như trên | ok |
| Lỗi giữa chừng → không ghi gì | `TestSeedWebFixture_FailureLateRollsBackAll` (lỗi ở bước mã giảm, sau khi địa giới, danh mục, thuộc tính, mẫu đã ghi: mọi bảng về 0, không sự kiện mới); `TestSeedWebFixture_AdminUnitConflict` (mã địa giới trùng khác tên → `errAdminUnitConflict`) | như trên | ok |
| Lệnh RUNBOOK chạy qua đúng đường của `main` (`run`: `config.Load` từ biến môi trường `CRM_ROLE=migrate`, `CRM_ENV`, `DATABASE_URL`, `DATABASE_OWNER_URL`; chọn lệnh; mã thoát) trên PostgreSQL thử: lần đầu, chạy lại, production với tệp không có → thoát 2, thiếu cờ → thoát 2 | `TestRun_SeedWebFixtureCommandLine` | `go test -race -count=1 -v -run '^(TestRun_SeedWebFixtureCommandLine)$' ./cmd/crm/` | `lần đầu: seed-web-fixture: tenant 01a2196c-…; tạo admin_unit 4, attribute 7, attribute_value 11, category 6, coupon 1, product 9, variant 39; sửa stock 38; ảnh chưa seed: 19` — `--- PASS (1.30s)`. Không chạy trên database dev dùng chung crm-shared (lệnh ghi vào tenant ELIMAZ của nhiều phiên khác); test là cùng hàm `run` mà `main` gọi |
| Sự kiện tới consumer như khi nhân viên sửa (webcache làm mới bộ đệm web); không in email | `TestRunSeedWebFixture_Command`, `TestSeedWebFixture_ModulesForMigrate` | như trên | ok |
| Id đặt trước ở catalog, id rỗng vẫn uuidv7 | `TestCreateWithFixedID` | `go test -race -count=1 -run '^(TestCreateWithFixedID)$' ./internal/modules/catalog/` | `ok … 1.911s` |
| Địa giới thử: chèn khi thiếu, chạy lại 0, xung đột không chèn, crm_app 42501 | `TestEnsureFixtureAdminUnits` | `go test -race -count=1 -run '^(TestEnsureFixtureAdminUnits)$' ./internal/modules/customer/` | `ok … 2.092s` |
| Cổng tĩnh tự soát | — | `golangci-lint run ./cmd/crm/... ./internal/modules/catalog/... ./internal/modules/customer/...`; `make jscpd`; `go test ./tests/arch/` (TestErrorsMapped) | `0 issues.`; `Found 0 clones.`; ok |

CI_RESULT: lượt 1 (ci-task, bể blacksmith, run 45885791047): cổng tĩnh `static: success`; `test (1): success`;
`test (2): failure` chỉ ở `customer.TestSearchPlans/đuôi_số_4_chữ_số` (test so kế hoạch EXPLAIN của
`customer.customers`, bảng task này không chạm; chạy lại trên máy 3/3 lần `ok`). Lượt 2 (sau sửa theo reviewer vòng
1, run 45887156579, blacksmith): `static: success`, `test (1): success`, `test (2): success` — `REMOTE-TEST XANH`,
`CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 733654b1…)`.

## Test bị xóa hoặc tắt

(không có)

## E2E viết sẵn, chưa chạy

Phần CRM không có test e2e riêng (lệnh có test Go trên PostgreSQL thật ở trên). Bộ e2e website với nguồn CRM
(`playwright.crm.config.ts`, `tests/e2e/crm-outage.spec.ts`) do phiên website viết sẵn ở apps/storefront-crm; Step 2,
3 của kế hoạch (chạy ba bộ, bảng kết quả WB01–WB14) để tới đóng mốc theo luật chủ shop 24/11 — chưa chạy.

## Khác kế hoạch

1. **Ảnh sản phẩm chưa seed** (đặc tả website: tải `images[].file` qua module tệp, chờ `ready`): cần kho S3, worker
   chạy imager và luồng tải lên + chờ xử lý trong lệnh dòng lệnh; lớn hơn phạm vi lệnh seed. Tệp vẫn đọc trường
   `images`, lệnh in "ảnh chưa seed: N". Ghi ở "Việc phát hiện thêm".
2. **Không nhả giữ hàng cũ** (đặc tả website bước 3): giữ hàng thuộc đơn; nhả chúng là sửa đơn ngoài đường của
   order. Thay vào đó tồn đặt theo **có thể bán** (delta = stock − có thể bán), nên mỗi lượt e2e vẫn bắt đầu với đúng số
   có thể bán của tệp.
3. **Lượt đã dùng của mã không đặt về 0**: không xóa lượt dùng của đơn có thật; số lượt tối đa = `usageLimit` + số đã
   dùng → mã còn đúng `usageLimit` lượt sau mỗi lần seed.
4. **`createdAt = mốc + position giây` không làm được**: catalog không có API đặt `created_at`; mọi mẫu tạo trong một
   transaction có cùng `created_at`. Thứ tự "Mới nhất" của website ở nguồn CRM không theo thứ tự fixture.
5. **Hãng giả hiện trên web và phí cố định chưa làm**: `seed-dev` đã tạo tài khoản hãng giả `fake-1`; bật hiện trên web
   là cài đặt của `fulfillment` (màn "Hãng trên website"). Ghi ở "Việc phát hiện thêm".
6. `combinableWithSale`, `measureCm`: CRM chưa có trường tương ứng, lệnh đọc rồi bỏ qua. `productIds` khác rỗng bị từ
   chối (CRM chưa có mã theo sản phẩm).

## Quyết định thay chủ shop

1. **Mẫu thuộc người bán mặc định của tenant, không tạo người bán theo `seller.id` của tệp.** Đường web
   (`catalog.DefaultSeller`: người bán own sớm nhất) chỉ bán hàng của người bán đó; người bán thứ hai thì website không
   thấy mẫu và đơn web không tìm được biến thể. Website không dùng `seller.id`. Phương án khác: tạo người bán đúng id và
   đổi đường web (trái ADR-29 và ngoài phạm vi). Dễ đảo: chỉ một dòng trong `fixtureApply.run`.
2. **Lệnh chạy bằng `CRM_ROLE=migrate` (chủ bảng) như `seed-dev`**, vì `customer.admin_units` chỉ chủ bảng ghi được và
   `catalog.sellers` crm_app không chèn được. Người làm audit là nhân viên `--staff-email` (bắt buộc; tenant lấy từ
   nhân viên). Phương án khác: hai pool (crm_app cho tenant, chủ bảng cho địa giới) — hai transaction, mất "lỗi giữa
   chừng không ghi gì".
3. **Thêm `ID` tùy chọn vào `CategoryInput`, `ProductInput`, `VariantInput`** của catalog (rỗng = như cũ): cách duy nhất
   để tạo đúng id mà website tính (Quyết định 1 của phiên website). Đường HTTP không đặt trường này.
4. **Tồn đặt theo "có thể bán"**, **mã còn đúng `usageLimit` lượt** (xem Khác kế hoạch 2, 3).
5. **Mã thuộc tính = tên bỏ dấu nối "-"** (`xuat-xu`, `chat-lieu`…); thuộc tính, giá trị đã có cùng mã/giá trị thì dùng
   lại (chia sẻ với dữ liệu nhân viên tạo).
6. **Biến thể ở CRM không có trong tệp giữ nguyên** (không ngừng bán): seed chỉ thêm/sửa, không xóa.

## Việc phát hiện thêm

1. **Ảnh của fixture**: thêm bước tải ảnh (cờ `--images-root`, kiểm `sha256`, `files.CreateUpload` + `Put` + chờ
   `ready` do worker xử lý, `AttachImage` theo `position`/`color`) — cần kho tệp và worker chạy trên máy e2e.
2. **Hãng giả trên web cho e2e** (bảng "Tên hãng"/"Số tiền phụ thuộc phí ship" của website): seed bật `fake-1` hiện
   trên web với tên "Hãng giao hàng A" và phí cố định 30.000 — cần public API ghi cài đặt hãng trên web của fulfillment
   gọi được từ lệnh (hiện ghi qua handler HTTP `SaveWebCarrierSettings`).
3. **Thứ tự biến thể trên web theo id** (`queries/webcatalog/products.sql` `VariantsOf … ORDER BY v.product_id,
   v.variant_id`): với id UUID v5 thứ tự cỡ/màu không theo fixture. Website nên xếp theo cỡ hoặc CRM thêm cột vị trí.
4. **Thứ tự giá trị thuộc tính** là vị trí chung của giá trị (lần đầu gặp), không phải `position` từng mẫu trong tệp
   (vd `test-skirt-long` có "Chéo Hàn nhập" trước "Tơ Hàn" ở fixture; ở CRM "Tơ Hàn" có trước).
5. Bộ đệm địa giới web 1 ngày (`locationsTTL`): sự kiện của seed không làm mới khóa địa giới; RUNBOOK ghi "seed trước khi
   bật api".

## Reviewer

- **Vòng 1: CẦN SỬA.** (1) `TestSeedWebFixture_ConflictRollsBack` không chứng minh lùi transaction (lỗi xảy ra trước
  câu ghi đầu tiên) → thêm `TestSeedWebFixture_FailureLateRollsBackAll` (lỗi ở bước mã giảm, sau khi đã ghi địa giới,
  danh mục, thuộc tính, mẫu; kiểm mọi bảng về 0, không sự kiện mới), đổi tên test cũ thành
  `TestSeedWebFixture_AdminUnitConflict`. (2) Lệnh RUNBOOK chưa chạy qua `run` → thêm `TestRun_SeedWebFixtureCommandLine`
  (đi qua `config.Load` từ biến môi trường, chọn lệnh, mã thoát; production với tệp không có → 2), trích dòng in vào
  bằng chứng và RUNBOOK, ghi biến bắt buộc. Ghi chú không bắt buộc (người bán mặc định thay `seller.id`, các điểm lệch
  đặc tả website) đã có ở "Quyết định thay chủ shop", "Khác kế hoạch".
- **Vòng 2: ĐẠT.** Không có phát hiện phải sửa. Đã kiểm: test lùi chứng minh cả transaction lùi (lỗi chỉ ở bước mã
  giảm, sau địa giới, danh mục, thuộc tính, mẫu); `TestRun_SeedWebFixtureCommandLine` đi đúng `run` của `main`, chặn
  production trước khi đọc tệp; không đường HTTP nào đặt `ID` mới; `version++` đúng vì `UpdateVariant`/`AddVariant` tăng
  version mẫu; tồn thực sau khi đặt ≥ 0; chia phiếu theo `MaxDocumentLines`; percent × 100 khớp phần vạn. Ghi chú không
  bắt buộc: tên `EnsureDefaultSeller`/`EnsureDefaultWarehouse` ở mục Tái dùng (đã sửa); kiểm `sha256` ảnh đi cùng việc
  seed ảnh (Khác kế hoạch 1).
