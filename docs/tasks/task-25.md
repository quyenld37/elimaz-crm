# Task 25 — Schema của M1, view chỉ đọc giữa module, gói queries, test kiến trúc

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 25 = M1-25). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `tenancy.CheckSchemaRules` giữ nguyên chữ ký, chỉ thêm hai
  luật (f), (g) trong câu SQL `SchemaRuleViolations` (3 chỗ gọi: `schemarules_test.go` ×2, `identity/users_test.go` ×1;
  cả ba phải vẫn ra `[]` trên schema thật). `config.Config` thêm trường (S3, tệp công khai, imager, hãng giả,
  filesync); `config.Load` giữ chữ ký (2 chỗ gọi: `cmd/crm/main.go`, `config_test.go`). Gói mới `internal/queries`
  (chỉ `doc.go`, chưa có ký hiệu xuất).
- Gói phụ thuộc ngược (go list): `internal/platform/config` ← `cmd/crm`, `internal/httpapi`, `internal/platform/{logging,
  metrics,telemetry}`; `internal/platform/tenancy` ← test của `identity`, `tenancy`. `tests/arch` không ai nhập.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không (chưa có `console/`).
- Câu sqlc, bảng, pha migration: sửa `queries/platform/schemarules.sql` (câu chỉ đọc `pg_catalog`), `make gen` sinh lại
  `internal/platform/internal/store`. Migration mới `0112_m1_schemas.sql` (pha mở rộng: chỉ `CREATE SCHEMA` + `GRANT
  USAGE`, chưa bảng nào). Không bảng, không view mới.
- Tệp viết tay dùng chung viết trước một lần (Q37): `docs/adr/0046-thu-tu-khoa.md` (chèn bảng M1, nhóm 12a, 12b),
  `docs/views.md` (mới, mọi view M1), `internal/platform/config/config.go`, `docs/danh-muc-ghi-chu.yaml` (dòng
  `internal/queries`). Không chạm tệp của Task 26 (`identity`, `openapi.yaml`, `cmd/crm/seed.go`, migration 0113).
- Test sẽ chạy: `go test -race ./internal/platform/tenancy/... ./internal/platform/config/... ./tests/arch/...
  ./internal/modules/identity/... ./cmd/crm/... ./internal/httpapi/...`; `tenancy` là vùng rủi ro cao → cả bộ
  `tenancy`; cuối cùng `make ci`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`tenancy.CheckSchemaRules`
  là chỗ duy nhất kiểm luật schema bằng `pg_catalog`); `tests/arch/sqlusage_test.go` (`tableRefs`, `checkSQL`,
  `schemaOfModule`), `boundaries_test.go` (`moduleCalls`, `boundaryViolation`, `goListPackages`), `scan_test.go`
  (`parseGoFiles`, `repoRoot`, `assertMentions`); `config.reader` (`required`, `secret`, `integer`, `boolean`).
  Tìm `views.md`, `security_invoker`, `reloptions` trong repo: chưa có.
- Dùng lại / viết mới vì: luật (f), (g) thêm vào cùng câu `SchemaRuleViolations` (không tạo hàm kiểm thứ hai); luật
  view chéo module thêm vào `checkSQL` (cùng bộ dò bảng), luật nhập của `internal/queries`, `internal/imager` thêm vào
  `boundaryViolation`; bảng "Được gọi" dùng nguyên `moduleCalls()` (đã đủ dòng M1). Viết mới: bộ đọc `docs/views.md`
  (chỉ test kiến trúc dùng, đặt trong `tests/arch`), `viewsdoc_test.go` (so view thật với tài liệu), `config.reader.url`.

## Soát thiết kế

Task chạm database (schema, luật kiểm), quyền schema; không chạm tiền, hàng đợi, đường ghi.

- 5.1 Đúng chủ dữ liệu: sáu schema theo 04 mục 3 / 05 (`catalog`, `inventory`, `customer`, `ord`, `fulfillment`,
  `cash`); bảng do task của module tạo.
- 5.2 Không ghi bảng của module khác: `sqlusage_test` báo mọi lần nhắc bảng của module khác (đọc hay ghi).
- 5.3 Đọc qua view: chính là việc của task: chỉ nhắc `<B>.<tên>_v` có trong `docs/views.md` và khi `A` được gọi `B`;
  `internal/queries` chỉ nhắc view; luật (f) bắt view phải `security_invoker` để RLS của bảng gốc áp cho người đọc.
- 5.4, 5.5, 5.6: không áp dụng: không có đường ghi, sự kiện hay lệnh ra ngoài.
- 5.7 Ngữ cảnh shop: `GRANT USAGE` cho `crm_app`, `crm_search` như `identity` ở M0; quyền bảng cấp từng bảng ở task tạo
  bảng (cách của `0003_identity.sql`, không `ALTER DEFAULT PRIVILEGES`). Luật (g) giữ mã hàng duy nhất theo người bán.
- 5.8 Nghiệp vụ không biết HTTP: luật (b) giữ nguyên; `internal/imager` thêm luật không nhập database, cấu hình.
- 5.9, 5.10: không áp dụng (không route, không Valkey).
- 5.11 Thư viện: không thêm thư viện.
- 5.12 Theo module mẫu: luật mới cùng chỗ với luật M0 (`CheckSchemaRules`, `tests/arch`).
- 6.1–6.11: không áp dụng: không có mã chạy lúc ứng dụng chạy trừ đọc cấu hình; migration chỉ tạo schema.
- 6.12 Dữ liệu cá nhân: `S3_SECRET_ACCESS_KEY`, `FAKECARRIER_WEBHOOK_SECRET` không vào `Config.LogValue` (chỉ cờ đã cấu
  hình hay chưa).
- 6.13 Quyền: `crm_app` chỉ có `USAGE` trên schema, chưa có quyền bảng nào; mặc định từ chối.

## Quyết định thay chủ shop

| # | Chỗ chưa rõ | Đã chọn | Vì sao, phương án khác |
|---|---|---|---|
| D0 | Kế hoạch M1 ghi "chưa thực hiện trước khi chủ shop duyệt" | Làm theo kế hoạch M1 bản 2 mà điều phối viên đã duyệt thay chủ shop (chủ shop giao toàn quyền 18/11) | Chủ shop đổi ý thì task này chỉ thêm schema rỗng và luật kiểm, gỡ bằng một migration mới |
| D1 | Kế hoạch vừa ghi "biến chưa có mã dùng vẫn là tùy chọn", vừa đòi test "thiếu `S3_ENDPOINT` ở `worker` → lỗi" | Nhóm S3 (`S3_ENDPOINT`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `FILES_PUBLIC_BASE_URL`) ở `api`, `worker` là **tất cả hoặc không**: không khai biến nào thì hợp lệ (tới Task 41); đã khai một biến thì thiếu biến nào báo tên biến đó | Cả hai câu của kế hoạch đều đúng; tiến trình M0 đang chạy không cần biến mới. Task 41 đổi thành bắt buộc bằng cách bỏ nhánh "không khai gì". Phương án khác: bắt buộc ngay (phải sửa mọi cấu hình chạy M0 khi chưa có kho tệp) |
| D2 | `IMAGER_*`, `PORT` của `imager` và biến của `crm-filesync` (rclone, `FILESYNC_DATABASE_URL`) có vào `config.go` không | Không: `cmd/imager` không được nhập `internal/platform/config` (luật nhập của chính task này), `crm-filesync` là container shell không có mã Go. `config.go` chỉ nhận biến của `api`, `worker`, `ingest` | Đặt ở `config.go` thì `imager` phải nhập gói cấu hình đọc cả bí mật — trái Global Constraints. Task 42 tự đọc 4 biến của `imager` |
| D3 | Luật (g) "trừ danh sách ngoại lệ ghi trong `docs/views.md`" trong khi `CheckSchemaRules` chạy trên database, không đọc tệp | Ngoại lệ là index được đánh dấu `COMMENT ON INDEX … IS 'seller-exempt: <lý do>'`; `docs/views.md` mục "Ngoại lệ luật (g)" liệt kê chúng, `viewsdoc_test` đòi hai tập bằng nhau | Giữ nguyên chữ ký `CheckSchemaRules` (Task 26 song song dùng nó); ngoại lệ vẫn chỉ có ở một nơi có kiểm. Phương án khác: thêm tham số danh sách ngoại lệ (đổi 3 chỗ gọi) |
| D4 | Luật (g) và luật (b) `UNIQUE (tenant_id, id)` | Unique đúng `(tenant_id, id)` không tính vào luật (g) | Luật (b) đòi nó; `id` đã duy nhất toàn cục nên không cần `seller_id` |
| D5 | Luật (f) áp cho view loại nào, schema nào | Mọi view thường (`relkind = 'v'`) ngoài `pg_catalog`, `information_schema`, `pg_*` và view của extension. View vật chất hóa (`m`) không có `security_invoker` nên không áp (M6, M7 dùng thì mốc đó thêm luật riêng). Giá trị nhận: `true`, `on`, `yes`, `1` | Đúng ý "mọi view trong schema module" và "cả `platform`" |
| D6 | Chỗ đặt bảng "Được gọi" | `moduleCalls()` đã có đủ dòng M1 từ M0 (cả 04 mục 3); giữ nguyên, chỉ thêm luật cho `internal/queries`, `internal/imager`, `cmd/imager` và dùng cùng bảng cho luật view | Không có hai bảng "Được gọi" |
| D7 | `FILESYNC_EXPECTED` thuộc vai trò nào | Đọc ở mọi vai trò, mặc định `false` (luật cảnh báo Task 45 chạy ở `worker`, `/health/deep` ở `api`) | Biến chỉ bật cảnh báo, không bí mật |
| D8 | Biến hãng giả ở production | `FAKECARRIER_URL` hoặc `FAKECARRIER_WEBHOOK_SECRET` có mặt khi `CRM_ENV=production` → lỗi cấu hình; ngoài production: có `FAKECARRIER_URL` thì bí mật bắt buộc, ≥ 32 ký tự. Đọc ở `ingest`, `worker` | Hãng giả không bao giờ chạy ở production (Q2) |

## Bằng chứng

Trước khi viết mã (Step 2) các test mới đều đỏ: `TestCheckSchemaRulesViewsAndSeller` (7 ca con "nhận []"),
`TestSQLUsageViewRules`, `TestBoundariesTestdata` ("mong 9, có 6"), `TestBoundariesNotCalledModule` (9 ca),
`TestViewsDocMatchesDatabase` (`schema "catalog" does not exist`), `config` không dựng được (thiếu trường).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (f) `probe.bad_v` không `security_invoker` → 1 dòng; `good_v` → 0; `noname` thiếu `_v` → 1; cả view `platform` | `TestCheckSchemaRulesViewsAndSeller` (ca view) | `go test -race ./internal/platform/tenancy/...` | `ok … tenancy 16.266s` |
| (g) `probe.sellerish` `UNIQUE (tenant_id, sku)` → 1 dòng; `(tenant_id, seller_id, sku)` → 0; `(tenant_id, id)` và index `seller-exempt` không tính | `TestCheckSchemaRulesViewsAndSeller` (ca seller) | như trên | `ok` |
| `CheckSchemaRules` trên database thật (đã có 0112) → `[]` | `TestCheckSchemaRulesCleanSchema`, `identity` `users_test` | như trên; `go test -race ./internal/modules/identity/...` | `ok`; `ok … identity 10.579s` |
| sqlusage: `queries/inventory` nhắc `catalog.variants_v` → 0; `internal/queries` nhắc `platform.file_variants_v` → 0, `platform.file_variants` → 1, `catalog.products` → 1; `catalog.variants` → 1; `queries/catalog` nhắc `inventory.stock_available_v` → 1; view chưa có trong `docs/views.md` → 1 | `TestSQLUsageViewRules`; mã thật `TestSQLUsageProductCode` | `go test -race ./tests/arch/...` | `ok … tests/arch 6.870s` |
| boundaries: `inventory` nhập `order` → lỗi; `internal/imager` nhập pgx → lỗi; `cmd/imager` nhập `config` → lỗi; `internal/queries` nhập `catalog` → lỗi; mã thật → 0 | `TestBoundariesTestdata`, `TestBoundariesNotCalledModule`, `TestBoundariesRealCode` | như trên | `ok` |
| viewsdoc: mọi view `_v` trong database có dòng đúng cột; view lạ, sai cột, index miễn không ghi → báo; `docs/views.md` đủ 20 view M1 | `TestViewsDocMatchesDatabase`, `TestViewsDocParses` | như trên | `ok` |
| config: thiếu `S3_ENDPOINT` ở `worker` → lỗi nêu tên; không khai biến M1 vẫn hợp lệ; giá trị sai, hãng giả ở production → lỗi; bí mật không vào `LogValue` | `TestLoadWorkerMissingS3EndpointNamesVariable`, `TestLoadM1WithoutNewVariablesStillValid`, `TestLoadM1BadValues`, `TestLoadS3Valid`, `TestLoadM1WorkerAndIngestValues` | `go test -race ./internal/platform/config/...` | `ok … config 1.663s` |
| Không gói nào khác vỡ (cấu hình, schema mới) | `httpapi`, `e2e` | `go test -race ./internal/httpapi/... ./tests/e2e/...` | `ok … httpapi 18.155s`, `ok … e2e 25.297s` |
| Toàn bộ cổng kiểm | — | `make ci` (sau khi stage) | thoát 0: `0 issues.`, mọi gói `ok`, `No vulnerabilities found.`, jscpd 0 bản trùng mới, `no leaks found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `tests/arch/dynsql_test.go` không phải sửa: luật "SQL chỉ trong `*_dynsql.go` có test đi kèm" đã áp cho
  `internal/queries` như mọi gói; luật "chỉ nhắc view" nằm ở `sqlusage_test` (đọc cả `*_dynsql.go`).
- `moduleCalls()` (bảng "Được gọi") đã có đủ dòng M1 từ M0, không thêm dòng (D6).
- Biến của `imager` và `crm-filesync` không vào `config.go` (D2).
- `docs/danh-muc-dung-chung.md` sinh lại vì `Config` có trường mới.
- `make ci` lần chạy cuối (sau lượt review 2) qua mọi bước tới `test-guard`, rồi `test-guard` báo
  `webhooks/split_test.go` "bị xóa", `tenancy_test.go` "giảm 47 → 44 câu kiểm". Nguyên nhân: Task 15 gộp vào `main`
  (`7273c72`) trong lúc task này làm, `test-guard` so với `main` còn nhánh này tách từ `b8aabbb`. Phần stage không xóa
  tệp nào và không chạm hai tệp đó (`git diff --cached --diff-filter=D` rỗng). Lần `make ci` trước (cùng mã, trước
  khi Task 15 gộp) thoát 0. Khi gộp nhánh này vào `main` (sau Task 15) hai tệp đó còn nguyên; xung đột duy nhất có thể
  có là `docs/danh-muc-dung-chung.md` (tệp sinh: chạy `make catalog` sau khi gộp).

## Việc phát hiện thêm

- `tools/catalog` chỉ quét `internal/platform/*`, nên dòng `internal/queries` trong `docs/danh-muc-ghi-chu.yaml` chưa
  hiện trong `docs/danh-muc-dung-chung.md`. Muốn hiện thì sửa `tools/catalog` (nhận nhiều gốc) ở task khác, ví dụ Task
  M1-39 khi gói có ký hiệu xuất đầu tiên.
- Reviewer (ghi chú, không chặn): `moduleCalls()["reporting"]` là `nil` nên M6 phải thêm dòng "Được gọi" cho
  `reporting` trước khi đọc view; luật imager chỉ kiểm nhập trực tiếp (Task 42 có thể thêm kiểm bắc cầu); chặn hãng giả
  dựa vào đúng chuỗi `CRM_ENV=production` mà `CRM_ENV` chưa có danh sách giá trị hợp lệ (nên chốt khi có cấu hình máy
  chủ, M0 Task 23).
- Task M1-41 phải đổi nhóm S3 thành bắt buộc ở `api`, `worker` (bỏ nhánh "không khai gì", D1).

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, hai lượt trên `git diff --cached`).

- Lượt 1: ĐẠT, không phát hiện phải sửa. Đã soát luật (f), (g) (NULL của LEFT JOIN, index biểu thức, `indnkeyatts`,
  không báo đôi với luật (c), `reloptions` NULL, view của extension), `checkRef` (không lỗ để đọc bảng module khác),
  `imagerForbidden`, `viewsdoc_test`, nhóm S3 tất cả hoặc không, hãng giả ở production, `LogValue`, migration 0112,
  ADR-46 (chỉ chèn, giữ thứ tự M0), 20 dòng `docs/views.md`. Ghi chú không bắt buộc: `TestViewsDocMatchesDatabase`
  tự tạo `catalog.sellers_v` nên sẽ đỏ khi Task M1-27 tạo view thật → **đã sửa** (ca sai cột dùng `probe.wrongcols_v`);
  các ghi chú còn lại chép vào "Việc phát hiện thêm".
- Lượt 2 (sau khi sửa `viewsdoc_test.go`): ĐẠT, không phát hiện phải sửa.
