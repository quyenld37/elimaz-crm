# Task 225 — M6-02 Hợp đồng OpenAPI của M6, năm quyền mới, biến cấu hình, nhóm khóa 12a

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 225 = M6-02). Loại việc (quy trình mục 3):
A (hợp đồng, quyền, cấu hình; không bảng mới).

**Điều kiện bắt đầu:** kế hoạch đòi M1–M5 đóng. Chủ shop đã miễn sáng 27/11/2026 (duyệt kế hoạch M6, cho bắt đầu dù
M5 chưa đóng mốc, Task 221 còn chờ staging; ghi chú điều phối M6 và QĐ-223-1). Làm theo mã thật ở `main` 7919960a và các
quyết định QĐ-223-1…11 của `docs/tasks/task-223.md` (G18: không viết `0600`, ba quyền nhạy cảm `ScopeShop`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `identity.Permissions` (thêm 5 phần tử, không đổi kiểu): 11 tệp dùng (roles.go, content/module.go, tests/arch
    routes_test, tests/e2e seed/migrate, test của identity, httpapi). Các test đếm `len(identity.Permissions)` tự theo.
  - `identity.DefaultRoles` (giá trị đổi: owner, manager tự nhận mọi quyền mới; sale, warehouse, accountant thêm tay):
    dùng ở `EnsureDefaultRoles`/`SyncDefaultRoles` và test identity. `permissionLabels()` (không xuất) thêm 5 dòng nhãn.
  - `config.Config`: thêm trường `Report ReportConfig` và kiểu mới `config.ReportConfig` (chỉ thêm; 16 gói nhập
    `config`, không gói nào phải sửa). `Load` thêm kiểm biến `REPORT_*`, `CUSTOMER_EXPORT_TTL`.
  - Mã sinh: `gen.Handler` thêm 20 phương thức; `gen.UnimplementedHandler` có sẵn nên `*handler` vẫn biên dịch.
    `httpapi.Routes` (routes_gen.go) thêm 20 dòng.
- Gói phụ thuộc ngược (go list): `internal/modules/identity` — 28 gói nhập (kể cả test); `internal/platform/config` —
  16; `internal/httpapi/gen` — 2 (`httpapi`, `cmd/crm` qua httpapi). ci-task chạy test các gói này.
- Operation OpenAPI bị chạm, oasdiff: **chỉ thêm** 20 operation dưới `/api/v1/reports/**` (bảng kế hoạch Task M6-02,
  dòng gộp tách thành 20) và các schema mới `Report*`, `Sales*`, `Inventory*`, `CustomerExport*`; thêm 10 mã vào
  `x-error-codes` của `Error`. Không sửa operation, schema có sẵn → oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: `console/src/api/**` sinh lại (thêm kiểu, không đổi kiểu có sẵn);
  `console/src/app/errors.ts` thêm câu tiếng Việt cho 10 mã mới (test `errors.test.ts` đòi đủ). Không màn nào (màn
  M6 là Task 239–241, cần mockup).
- Câu sqlc, bảng, pha migration: không câu sqlc, không bảng, **không migration** (QĐ-223-6: không viết
  `0600_role_templates_m6.sql`; `identity.SyncDefaultRoles` của vai trò `migrate` đồng bộ quyền cho tenant đã có; số
  `0600` bỏ trống). `docs/adr/0046-thu-tu-khoa.md` điền dòng 12a (chỉ tài liệu, bảng tạo ở Task 226).
- Tài liệu: `docs/quyen.md` (5 dòng, câu liệt kê quyền `toàn shop`), `docs/adr/0046-thu-tu-khoa.md` (dòng 12a),
  `docs/danh-muc-dung-chung.md` (sinh lại bằng `make catalog`: kiểu `config.ReportConfig`).
- Test có sẵn phải sửa theo quyền, route mới (chỉ thêm giá trị): `identity` `TestPermissionsList`,
  `TestDefaultRoles_Content`; `httpapi` `TestM3Routes_PendingList` (biết `m6_pending.txt`); `tests/arch`
  `lockorder_frame_test.go` (khung nhóm 12a).
- Test sẽ chạy: `internal/modules/identity` (`TestPermissions_M6Scopes`, `TestCan_M6`, `TestDefaultRoles_M6`,
  `TestEnsureDefaultRoles_M6`, `TestSyncDefaultRoles_M6Existing`, `TestPermissionsDoc_MatchesCode`,
  `TestPermissionCatalog_Complete`), `internal/platform/config` (`TestLoad_Report*`), `internal/httpapi`
  (`TestM6Contract_StatusCodes`, `TestErrorCodes_*`), `tests/arch` (`TestM6Contract_*`, `TestRoutes*`,
  `TestLockOrderGroupCodes`, `TestLockOrderFrame`, `TestMoneyFields*`), console `src/app/errors.test.ts`; rồi
  ci-task (gói đổi + gói phụ thuộc; identity là vùng rủi ro cao → cả bộ identity).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (config reader,
  identity), mẫu hợp đồng đầu mốc của M3 (`m3_pending.txt`, `handlers_corrections.go`) và M4 (`m4contract_test.go`,
  `m4_pending.txt`, `internal/httpapi/m4_contract_test.go`, `roles_m4_test.go`); khung thứ tự khóa
  `tests/arch/lockorder_frame_test.go`.
- Dùng lại / viết mới vì:
  - Dùng lại `checkContractOps`, `pendingOps`, `serveAs`, `rolesTenant`/`ensureRoles`/`rolesByKey`/`execAll`,
    `lockGroups`, các tham số chung `Cursor`, `IdempotencyKey`, schema `Money`, response `Error` của hợp đồng.
  - Handler giữ chỗ: mọi operation mới trả 501 qua `gen.UnimplementedHandler` (có sẵn). Tệp
    `handlers_reports_*.go` tám vùng chỉ chứa thân giữ chỗ để task sau thay trong đúng tệp của mình (cách của M3,
    `handlers_corrections.go`).
  - Config: thêm hàm đọc thời lượng `duration` vào `reader` (chưa có; các biến cũ đọc số nguyên giờ). Viết mới vì biến
    M6 ghi dạng `60s`, `168h`, `5m` như kế hoạch.

## Soát thiết kế

- 5.1–5.7, 5.10: không áp dụng — task không tạo bảng, không ghi dữ liệu, không đọc view; chỉ hợp đồng, quyền, cấu
  hình. Bảng `reporting.*` và RLS là Task 226.
- 5.8: handler giữ chỗ ở `internal/httpapi` mỏng (trả 501), không nhập module nghiệp vụ.
- 5.9: ba `POST` (kế hoạch ghi "bốn", bảng operation chỉ có ba) (`createSalesReportRun`, `createInventoryReportRun`, `createCustomerExport`) đều có `x-permission` và
  tham số `IdempotencyKey` bắt buộc; không `x-idempotency: exempt` (Q19 cuối, kế hoạch). Mọi `GET` có `x-permission`.
- 5.11: không thêm thư viện.
- 5.12: theo mẫu hợp đồng đầu mốc của M3, M4.
- 6.1–6.11: không áp dụng — không đường ghi, không hàng đợi, không tiền ghi sổ. Tiền trong hợp đồng là
  `integer/int64/maximum 2^53−1` (`TestMoneyFields`), tỷ lệ `int32` phần vạn, `nullable`.
- 6.3 thứ tự khóa: điền nhóm 12a `reporting` (`report_runs` → `stock_snapshot_days` → `stock_daily_snapshots`), trước
  12b tệp; ghi luật "khóa `report_runs` trước `files`".
- 6.12: hợp đồng báo cáo không có trường SĐT, địa chỉ, tên khách (danh sách chi tiết chỉ mã đơn, mã khách); `params`
  của lần chạy không nhận chữ tự do (khóa lọc, ngày, mã). Tham số `q` của tồn kho là tiền tố SKU, không phải dữ liệu
  cá nhân.
- 6.13 quyền: mặc định từ chối — 5 quyền mới; hai quyền đọc `ScopeByTarget` (Page, kho), ba quyền nhạy cảm
  `ScopeShop` (chỉ hiệu lực ở gán `all`, Q13, Q14, QĐ-223-6). Vai trò `customized` không nhận quyền mới (trừ owner).
  Test 403 cho mọi route mới với thẻ không có quyền báo cáo.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 20 route M6 đúng đường, quyền, cờ ghi; 3 route ghi bắt buộc Idempotency-Key; không route lạ dưới /api/v1/reports | `tests/arch` `TestM6Contract_Operations` | `go test -count=1 -run 'Contract\|LockOrder\|Routes' ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 1.079s` |
| Danh sách chờ 501 chỉ có operation M6, không trùng | `TestM6Contract_PendingList` | như trên | `ok` |
| Tập route công khai không đổi, quyền của route thuộc danh sách quyền | `TestRoutesPublicSetAndPermissions` (có sẵn) | như trên | `ok` |
| Mọi route M6: không thẻ 401; thẻ chỉ audit.read 403 forbidden; owner (POST có khóa, thân hợp lệ) 501 not_implemented; đủ 20 route | `internal/httpapi` `TestM6Contract_StatusCodes` | `go test -count=1 -run '^(TestM6Contract_StatusCodes\|TestM4Contract_StatusCodes\|TestM3Routes_PendingList)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 3.120s` |
| Route 501 của M6 không bị test M3 bắt nhầm | `TestM3Routes_PendingList` | như trên | `ok` |
| 10 mã lỗi mới trong x-error-codes, không trùng; mỗi mã có câu tiếng Việt | `TestErrorCodes_ContractListWellFormed`; console `errors.test.ts` | `go test -run TestErrorCodes ./internal/httpapi/`; `cd console && npx vitest run src/app/errors.test.ts` | `ok`; `Tests 17 passed (17)` |
| Loại phạm vi: 2 quyền đọc theo đích (page, warehouse), 3 quyền ScopeShop; danh mục màn Vai trò đúng đích | `TestPermissions_M6Scopes`, `TestPermissionsList`, `TestPermissionCatalog_Complete` | `go test -count=1 ./internal/modules/identity/` | `ok elimaz.com.vn/crm/internal/modules/identity 26.522s` |
| `Can`: report.export gán page:p1 → sai, gán all → đúng; report.sales.read p1 đúng, p2 sai | `TestCan_M6` | như trên | `ok` |
| Vai trò mẫu: owner, manager đủ 5; accountant sales.read + export; sale sales.read; warehouse inventory.read; customer.export chỉ owner, manager | `TestDefaultRoles_M6`, `TestDefaultRoles_Content` | như trên | `ok` |
| EnsureDefaultRoles tenant mới: owner đủ, sale có sales.read không có customer.export; chạy hai lần không đổi số dòng; sale đã sửa không nhận lại, owner có | `TestEnsureDefaultRoles_M6` | như trên | `ok` (PostgreSQL thật) |
| Tenant có sẵn thiếu quyền M6: SyncDefaultRoles thêm vào vai trò mẫu chưa sửa và owner; không bớt quyền; sale đã sửa tay không nhận; chủ Can đủ 5 | `TestSyncDefaultRoles_M6Existing` | như trên | `ok` |
| docs/quyen.md khớp mã (5 dòng mới, đúng loại phạm vi) | `TestPermissionsDoc_MatchesCode` | như trên | `ok` |
| Config mặc định: api 2 chỗ, 30s, 5, link 5m; worker 2 chỗ, 60s, 200000 dòng, 168h, 24h; ingest, migrate không đọc | `TestLoad_ReportDefaults` | `go test -count=1 -run 'Report' -v ./internal/platform/config/` | `--- PASS: TestLoad_ReportDefaults` |
| REPORT_BACKGROUND_TIMEOUT=80s, 75s → lỗi nêu tên; số chỗ ngoài 1–8, thời lượng sai dạng, TmpDir tương đối → lỗi nêu tên; api không đòi REPORT_TMP_DIR | `TestLoad_ReportBadValues` | như trên | `--- PASS` |
| Đè giá trị hợp lệ | `TestLoad_ReportOverrides` | như trên | `--- PASS` |
| REPORT_TMP_DIR chưa bắt buộc (QĐ-225-1) | `TestLoad_ReportTmpDirOptionalUntilExport` | như trên | `--- PASS` |
| ADR-46: 12a = reporting, 3 bảng đúng thứ tự; mỗi mã nhóm một lần, 12 < 12a < 12b < 12c | `TestLockOrderFrame`, `TestLockOrderGroupCodes` | `go test -run TestLockOrder ./tests/arch/` | `ok` |
| Tiền là int64, maximum 2^53−1 | `TestMoneyFields*` (có sẵn) | `go test -count=1 ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 54.196s` |
| oasdiff: không thay đổi phá vỡ | `make oasdiff` | `make oasdiff` | thoát 0, không dòng phá vỡ |
| Mã sinh khớp nguồn; console biên dịch | `make gen`, `npx tsc --noEmit` | `make gen && git status`; `cd console && npx tsc --noEmit` | sinh lại sạch; tsc không lỗi |
| Cổng tĩnh + test gói đổi và gói phụ thuộc (identity vùng rủi ro cao; -race cho httpapi, identity, config) | cổng kiểm của task | ci-task | `CI-TASK XANH (từ xa trên blacksmith…)`, run 45935068554. Lần 1 (run 45934269831) đỏ: test-guard, `TestPermissionsList`, `TestDefaultRoles_Content`, `TestM3Routes_PendingList` — đã sửa |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

Chủ shop vắng tới sáng, giao toàn quyền trong phạm vi kế hoạch, spec. Mỗi dòng chọn phương án đúng spec, an toàn, dễ
đảo ngược.

| # | Câu hỏi | Chọn | Vì sao | Phương án khác |
|---|---|---|---|---|
| QĐ-225-1 | `REPORT_TMP_DIR` "bắt buộc ở worker" (kế hoạch) làm worker của `docker/compose.dev.yml`, `compose.prod.yml` và `tests/e2e` (`roleEnv`) không khởi động được, mà đổi `docker/` thì phải hỏi | Task này đọc biến, kiểm đường dẫn tuyệt đối khi có đặt, **chưa bắt buộc**. Task M6-14 (= 237, người dùng đầu tiên: `os.CreateTemp`) đặt bắt buộc cùng lúc thêm biến vào compose (cùng lượt hỏi chủ shop về `bootstrap.sh`, mục G5 của Task 223) | Không đổi hạ tầng, không gãy compose/e2e trên `main`; đổi sang bắt buộc là một dòng `r.required` | Bắt buộc ngay và sửa compose: cần OK đổi `docker/`; mặc định `/tmp`: tmpfs trong RAM của container 512 MB, chọn chỗ ghi là việc hạ tầng |
| QĐ-225-2 | Mã lỗi mới "trong `internal/httpapi/errors.go`" khi chưa có sentinel nào của `reporting` (module tạo ở Task 226–228) | Khai 10 mã ở `x-error-codes` (hợp đồng) và câu tiếng Việt ở `console/src/app/errors.ts`; **không** thêm hằng `code*` Go. Task tạo sentinel thêm `errors_reporting.go` theo luật Task 441 (bảng `errorMapping`, `domainErrorMappers`, `errors_snapshot_test.go`) | Hằng không dùng bị linter `unused` chặn; luật Task 441 đặt mã của một vùng ở `errors_<vùng>.go`, không ở `errors.go`; `TestErrorCodes_DeclaredInContract` chỉ đòi chiều Go → hợp đồng, nên khai trước hợp đồng là cách M4 đã làm | Thêm hằng kèm `//nolint:unused`: lách linter |
| QĐ-225-3 | Mọi `GET` báo cáo trả gì khi hết chỗ, quá hạn (Task M6-05: "handler của mọi GET báo cáo gọi `Interactive.Run`") | Bảy GET báo cáo (tổng quan, đơn, danh sách chi tiết, hãng, bán chạy, tồn, xuất nhập tồn) trả `oneOf [<báo cáo>, RunInBackground]` phân biệt bằng `status` (`ready` / `run_in_background`), như `DashboardResponse` của kế hoạch. `getSalesReportFilters` trả thẳng | Đúng hành vi M6-05 mô tả cho mọi GET; khai một lần ở hợp đồng đầu mốc, task sau không phải sửa hợp đồng | Chỉ dashboard có `oneOf`: các task sau phải đổi kiểu trả (thay đổi phá vỡ) |
| QĐ-225-4 | `params` của lần chạy: lược đồ chặt hay tự do | Đối tượng tự do (`ReportParams`, tối đa 30 khóa, thân tối đa 1 MiB mặc định); máy chủ giải bằng `DecodeParams` (M6-05), trường lạ → 400 `bad_params` | Kế hoạch đòi `bad_params` cho trường lạ (M6-05, M6-15: `{"q":"Nguyễn"}` → `bad_params`); lược đồ chặt làm ogen trả 400 `bad_request` trước handler | Lược đồ `oneOf` theo từng báo cáo: lặp danh sách tham số hai nơi |
| QĐ-225-5 | `report` của hai `POST` lần chạy: một danh sách khóa chung hay theo họ | Một enum `ReportKey` chín khóa cho cả hai; khóa của họ khác → máy chủ trả 400 `unknown_report` (`Lookup`, M6-05) | Đúng `Lookup(f, k)` của kế hoạch; mã `unknown_report` mới có đường tới | Enum theo họ: `unknown_report` không bao giờ trả |
| QĐ-225-6 | Không viết `0600_role_templates_m6.sql` (kế hoạch) | Theo QĐ-223-6: quyền mới vào `Permissions` + `DefaultRoles`; `SyncDefaultRoles` (vai trò `migrate`) đồng bộ tenant đã có; số `0600` bỏ trống. Test "tenant có sẵn" giả lập tenant cũ bằng cách xóa quyền M6 rồi đồng bộ (`TestSyncDefaultRoles_M6Existing`), cùng mẫu với M2, M3 | Mã thật không ghi quyền bằng migration từ M2 | Migration chép tay danh sách quyền lần hai |

## Khác kế hoạch

- Không có `migrations/0600_role_templates_m6.sql` (QĐ-225-6, QĐ-223-6); test "migration 0600 trên tenant có sẵn" thay
  bằng `TestSyncDefaultRoles_M6Existing`.
- `REPORT_TMP_DIR` chưa bắt buộc (QĐ-225-1); test `Load(RoleWorker)` thiếu biến → lỗi đổi thành
  `TestLoad_ReportTmpDirOptionalUntilExport` (thiếu → chạy, TmpDir rỗng) và ca `REPORT_TMP_DIR=tmp/reports` → lỗi nêu
  tên.
- Mã lỗi khai ở hợp đồng và màn hình, chưa ở Go (QĐ-225-2).
- `internal/httpapi/reports_stub_test.go` của kế hoạch đặt tên `m6_contract_test.go` (cùng mẫu M4
  `m4_contract_test.go`); thêm `tests/arch/m6contract_test.go` + `testdata/m6_pending.txt` (mẫu M4).
- Tám tệp `handlers_reports_*.go` giữ chỗ (dashboard, orders, filters, carriers, bestsellers, stock, runs, customers) —
  đúng danh sách kế hoạch; mỗi thân trả `ht.ErrNotImplemented` như `handlers_corrections.go` của M3.
- Bán chạy: tham số `basis` là `placed | delivered` (theo Interfaces của Task M6-09), không phải `placed | completed`.
- Test có sẵn sửa theo quyền mới (chỉ thêm giá trị, không bớt câu kiểm): `TestPermissionsList` (5 quyền M6),
  `TestDefaultRoles_Content` (quyền M6 của sale, warehouse, accountant), `TestM3Routes_PendingList` (biết thêm
  `m6_pending.txt`, như đã làm với `m4_pending.txt`). Test M4 không đổi: hàm dùng chung của test M6
  (`checkPermissionScopes`, `requireRolesHave`, `expectNoToken`, `contractKey`, `m6PendingList`) đặt trong tệp M6 (lần
  cổng kiểm đầu đỏ ở test-guard khi tôi tách hàm trong `roles_m4_test.go`; đã trả nguyên tệp M4).
- Gộp `main` (bắt buộc một lần trước review): `main` vẫn ở 7919960a = điểm tách, không có gì để gộp.

## Việc phát hiện thêm

- Task M6-14 (237): đặt `REPORT_TMP_DIR` bắt buộc ở worker và thêm biến (cùng thư mục ghi được) vào `docker/compose.*.yml`
  — cần OK đổi hạ tầng của chủ shop (QĐ-225-1).
- Task tạo sentinel của `reporting` (226–228) thêm `internal/httpapi/errors_reporting.go` cho 10 mã đã khai
  (QĐ-225-2).

## Reviewer

Kết luận: ĐẠT (lượt 1, agent `reviewer` trên `git diff --cached`).

- Phát hiện phải sửa: không có.
- Ảnh hưởng: khớp cả hai chiều (Permissions +5, DefaultRoles, permissionLabels, config.ReportConfig, 20 operation, 8
  tệp giữ chỗ, 10 mã lỗi, tài liệu; errors.go và 0600 không đổi có lý do QĐ-225-2, QĐ-225-6).
- Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí; không test nào bị tắt, nới.
- Ghi chú không bắt buộc (không sửa): (1) mục 5.9 ghi "bốn POST" — đã sửa chữ thành ba; (2) câu lỗi của
  `REPORT_BACKGROUND_TIMEOUT=80s` ghi khoảng "[1s, 1m15s]" trong khi 75s bị chặn ở lần kiểm thứ hai (kết quả đúng); (3)
  bốn quyền `report.*` nằm trong nhóm "Khuyến mại, báo cáo, cộng tác viên" của màn Vai trò (đúng tên nhóm có sẵn).

## Gộp vào main

Phiên gộp tuần tự ngày 27/11, `git merge --no-ff --no-commit task-225` trên `main` 2846a2c5.

- Xung đột tay: chỉ `docs/quyen.md` (dòng liệt kê quyền quản trị toàn shop): `main` thêm `promotion.flash.manage`
  (Task 199), nhánh thêm ba quyền M6. Giải: giữ cả hai — danh sách M5 có `promotion.flash.manage`, rồi mục "của M6".
- Xung đột mã sinh (`internal/httpapi/gen/*`, `console/src/api/*`): không sửa tay; `openapi/openapi.yaml` tự gộp
  không xung đột (đủ operation của cả hai bên), chạy `make gen` sinh lại, không còn dấu xung đột.
- Tự gộp, đã đọc lại: `identity/permissions.go`, `roles.go` (nhãn `promotion.flash.manage`, `content.*` của main và
  năm nhãn M6 cùng có), `permissions_test.go`, `roles_test.go`, `routes_gen.go`. Không có `errors*.go`, `main.go`,
  wiring test chạm ở nhánh; `m6_pending.txt` mới (main chưa có mã M6 nào nên không bỏ dòng nào).
- Trên máy: `go build ./...` qua; `go vet` các gói identity, httpapi, config, tests/arch, cmd/crm qua; `gofmt -l` rỗng.
- Test từ xa: `CRM_CI_POOL_SLOTS=vps=0 CRM_CI_LABEL=integrate-main-225 scripts/ci/merge-test.sh <tree 1b3dcb70> HEAD
  <gói> <race>`; gói `./cmd/crm ./internal/httpapi ./internal/modules/identity ./internal/platform/config ./tests/arch`,
  `-race` cho httpapi, identity, config. Kết quả: static success (build, vet, lint, gen-check, jscpd, test-guard…),
  test (1) (2) success → `MERGE-TEST XANH (blacksmith)`, run 45938097179. Không có test chập chờn.
- Reviewer (phần gộp, `git diff --cached`): **ĐẠT** lượt 1, không phát hiện phải sửa. Đã đối chiếu 352 operation
  trong `openapi.yaml` = 352 trong mã sinh (đủ 7 operation flash sale của main và 20 operation M6); `m6_pending.txt`
  đúng 20 operation chưa có mã thật; quyền, vai trò mẫu, `quyen.md` giữ đủ phần của cả hai bên.
