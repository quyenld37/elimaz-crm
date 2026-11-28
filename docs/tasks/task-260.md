# Task 260 — Hợp đồng phần khởi động

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task M7-15, mục "Task 260"); tên thật
của mã M1–M6: `docs/khoi-dong/doi-chieu-gia-dinh-m7.md` (Task 245). Loại việc (quy trình mục 3): A (hợp đồng, Q23).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `identity.Permissions` (thêm 10 quyền, không đổi quyền cũ;
  56 chỗ dùng ngoài gói đọc danh sách, không chỗ nào đếm cứng ngoài test của gói), `identity.ServiceKeyPermissions`
  (thêm `web.catalog.preview`), `identity.DefaultRoles` (thêm quyền vào năm vai trò mẫu); `permissionLabels` (nội bộ,
  thêm 10 nhãn). Mới: `inventory.RegisterCountImportPurpose`, `inventory.CountImportJobs`, `inventory.CountImportDeps`,
  `order.RegisterFallbackPurpose`, `order.FallbackImportJobs`, `order.FallbackImportDeps` (khung rỗng, nối trong
  `cmd/crm`). Mã sinh ogen: 32 operation mới (handler, params, schema), trường mới tùy chọn của `gen.Error`
  (`count`, `existingId`, `candidates`) và `gen.WebProduct.preview`; `httpapi.Routes` thêm 32 dòng.
- Gói phụ thuộc ngược (go list): 25 gói nhập `internal/modules/identity`; `internal/httpapi` (handler khung, test hợp
  đồng), `cmd/crm` (nối dây), `tests/arch` (test hợp đồng). Không đổi chữ ký hàm có sẵn nào.
- Operation OpenAPI bị chạm, oasdiff: thêm 32 operation (29 của kế hoạch + `listOpeningStocktakes`,
  `getOpeningStocktake`, `getLegacyReturn`, xem Khác kế hoạch); sửa mô tả `setChannelSendMode` (`use_go_live`),
  `updateAutomationRule` (`page_read_only`); thêm `preview` tùy chọn vào `WebProduct` (`webCatalogProducts`,
  `webCatalogProductsByIds`, `webCatalogChanges` dùng chung schema); thêm 24 mã vào `x-error-codes` và 3 trường tùy
  chọn vào `Error`. oasdiff chỉ báo thêm mới (không breaking).
- Chỗ dùng trên màn hình: `console/src/api/**` sinh lại; `console/src/app/errors.ts` thêm câu tiếng Việt cho 24 mã
  mới (errors.test.ts đòi đủ). Chưa màn nào gọi operation mới (Task 273, 274 làm màn).
- Câu sqlc, bảng, pha migration: không có (không migration, không câu SQL mới). Tài liệu: `docs/adr/0046-thu-tu-khoa.md`
  chèn 9 bảng M7 (nhóm 6, 7, 8, 11, 12a); `docs/views.md` thêm hai dòng view mới (chưa tạo, test bỏ qua);
  `docs/quyen.md` thêm 11 dòng; `docs/danh-muc-ghi-chu.yaml` bổ sung câu cho `metrics` và `observability`.
- Test sẽ chạy: `go test -run` từng test mới/sửa trên máy (`TestM7*` của `internal/httpapi`, `tests/arch`;
  `TestPermissions_M7*`, `TestDefaultRoles_M7*`, `TestPermissionsList`, `TestDefaultRoles_Content`,
  `TestPermissionsDoc_MatchesCode`, `TestPermissionCatalog_Complete` của `identity`; `TestRegisterFilePurposes_MatchTable`
  của `cmd/crm`); cả gói và cổng tĩnh (oasdiff, gen-check, lint) ở `scripts/agent/ci-task.sh` trên bể từ xa.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (files.RegisterPurpose,
  background.Collect, queue.Handler); mẫu hợp đồng M6 (Task 225: `handlers_reports_*.go` khung, `m6_contract_test.go`,
  `tests/arch/m6contract_test.go`, `m6_pending.txt`), M4 (`pendingOps`, `checkContractOps`), khung thứ tự khóa
  (`lockGroups`, `isSubsequence` của `tests/arch/lockorder_frame_test.go`), `readViewsDoc` (`tests/arch`).
- Dùng lại / viết mới vì: dùng lại `pendingOps`, `expect`, `serveAs`, `expectNoToken`, `contractKey`, `rolePerms`,
  `testServer` (httpapi), `checkContractOps`, `lockGroups`, `isSubsequence`, `readViewsDoc` (arch),
  `checkPermissionScopes`, `requireRolesHave` (identity test M6); schema có sẵn `ChannelAccount`, `Actor`,
  `StocktakeStatus`, `ProductStatus`, `AutomationRuleKind`, tham số `Cursor`, `Limit`, `ChannelID`, `StocktakeID`,
  `ImportID`, `IdempotencyKey`. Viết mới: bảng operation M7 (`m7Operations`), `m7_pending.txt`, hai hàm khung nối
  dây trong `inventory`, `order` (chưa có chỗ nối việc nền cần kho tệp cho hai module này).

## Soát thiết kế

5.1 Chủ dữ liệu: task không tạo bảng; ADR-46 ghi bảng M7 vào nhóm của module chủ (conversation, ord, fulfillment,
inventory, reporting) theo kế hoạch. 5.2–5.4: không áp dụng (không mã nghiệp vụ, handler trả 501). 5.5–5.6: không
áp dụng (chưa có việc nền; hàm nối dây trả danh sách rỗng, không lịch nào chạy). 5.7: không áp dụng (không truy vấn
mới). 5.8: handler ở `internal/httpapi`, module không nhập HTTP. 5.9: mọi route ghi M7 có `IdempotencyKey` (required,
không exempt) và `x-permission`; test `TestM7Contract_Operations` đòi. 5.10, 5.11: không áp dụng (không Valkey, không
thư viện mới). 5.12: theo mẫu hợp đồng M6 (Task 225).

6.1–6.11: không áp dụng (không ghi dữ liệu; lock order chỉ là tài liệu cho task sau, kiểm bằng test đọc ADR-46).
6.12 Dữ liệu cá nhân: schema mới không có SĐT, tên khách (sự kiện hãng ngoài hệ thống chỉ mã vận đơn; lỗi nhập chỉ số
dòng và mã). 6.13 Quyền: 10 quyền mới mặc định từ chối; `reporting.launch.decide` chỉ `owner`; `web.catalog.preview`
chỉ khóa dịch vụ (không vai trò nào nhận, vai trò tự định nghĩa → `unknown_permission` như mọi quyền `web.*`).

## Quyết định thay chủ shop

- QĐ-260-1 (phạm vi): làm hợp đồng cho **cả Phần C** (Task 261–271 và CLI của 275), kể cả task để sau (266–268,
  270, 271, 275), vì kế hoạch cấm task sau sửa `openapi.yaml`, `permissions.go`, `roles.go`, ADR-46; task để sau chỉ
  còn trả 501 tới khi làm. Không thêm gì của đường đăng nhập riêng (248–259, ghi chú điều phối). Phương án khác: chỉ hợp
  đồng của đợt này (261–265, 269) — loại vì lúc mở 266–271 sẽ phải mở lại tệp dùng chung.
- QĐ-260-2 (loại phạm vi): `reporting.launch.run`, `reporting.launch.decide` là `ScopeShop` (luật Task 315 sau kế
  hoạch: quyền ghi chính sách toàn shop là `toàn shop`; đêm mở là việc của cả shop); `reporting.launch.read`,
  `catalog.launch.publish`, `fulfillment.unmatched.read`, `order.fallback.import` giữ `ScopeNone` như kế hoạch (cùng loại
  `catalog.manage`, `order.create`); ba quyền theo đích giữ như kế hoạch. Phương án khác: giữ `ScopeNone` cho cả ba
  quyền `reporting.launch.*` (kế hoạch 18/11) — loại vì trái luật Task 315; đổi lại sau là đổi loại phạm vi của quyền
  đã có (phải duyệt), nên chọn chặt ngay.
- QĐ-260-3 (phản hồi 409 thêm cho M4): codebase không khai mã HTTP riêng (mọi lỗi qua `default: Error`); "thêm 409
  `use_go_live`, `page_read_only`" = thêm mã vào `x-error-codes` và mô tả operation. Khai `"409"` riêng sẽ đổi kiểu
  ogen của handler M4 có sẵn. Dễ đảo ngược.
- QĐ-260-4 (nối dây): `conversation` (lịch, lệnh, việc của 266) và `reporting` (lịch `reporting.watch_day` của 271)
  đã có chỗ nối trong module (`conversation.CommandHandlers/JobHandlers/Schedules` qua `wireM4`,
  `reporting.JobHandlers/TxHandlers/Schedules` qua `wireM6`): task đó thêm phần tử trong module, không cần sửa
  `cmd/crm`. Chỉ hai việc cần kho tệp chưa có chỗ nối: `inventory.CountImportJobs`, `order.FallbackImportJobs` (khung
  trả rỗng, nối trong `cmd/crm/wire_m7.go`), cùng `RegisterCountImportPurpose`, `RegisterFallbackPurpose` (rỗng, gọi ở
  `registerFilePurposes`) và lệnh `crm seed-staging` (khung trả lỗi "chưa làm", Task 275). Khung rỗng không đăng ký
  lịch nào, nên không sinh việc chạy lỗi. Phương án khác: đăng ký lịch trỏ tới handler 501 như kế hoạch — loại vì việc
  sẽ `dead` và cảnh báo mỗi giờ.
- QĐ-260-5 (tệp khung trong module): không tạo tệp khung rỗng cho hàm nghiệp vụ (`launch.go`, `opening.go`…,
  `metrics/query.go`, `observability/tenantsummary.go`): tệp mới chỉ task đó sửa nên không xung đột; hàm khung xuất ra
  ngoài trả lỗi sẽ vào danh mục dùng chung như thật. Chỉ tạo tệp có hàm nối dây (`count_import.go`, `fallback_import.go`)
  và 11 tệp handler. Ghi chú "khi nào không dùng" của `DailyP95`, `TenantOpsSummary` viết sẵn vào dòng gói `metrics`,
  `observability` của `docs/danh-muc-ghi-chu.yaml` (tệp dùng chung).

## Phụ thuộc task để sau

- Operation của 266 (`getT2Report`), 267 (`goLivePage`, `pageBackToReadOnly`), 268 (`getFallbackTemplate`,
  `createFallbackImport`, `getFallbackImport`), 270 (9 operation `…/launch-runs…`), 271 (`getWatch`, `closeWatch`) trả
  501 và nằm trong `tests/arch/testdata/m7_pending.txt` tới khi task đó làm.
- `crm seed-staging` (275) trả lỗi "chưa làm"; `RegisterFallbackPurpose`, `FallbackImportJobs` (268) rỗng.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 32 operation đúng đường, quyền, route ghi `required`, không công khai; danh sách 501 khớp `m7_pending.txt` | `TestM7Contract_Operations`, `TestM7Contract_PendingList` | lệnh A: `go test -count=1 -v -run '^(TestM7Contract_Operations\|TestM7Contract_PendingList\|TestM7Contract_LockOrder\|TestLockOrderFrame\|TestM7Contract_ViewsDoc\|TestViewsDocParses)$' ./tests/arch/` (chạy sau khi gộp main 27d4fb0d) | `--- PASS: TestM7Contract_Operations (0.00s)`, `--- PASS: TestM7Contract_PendingList (0.02s)`, `ok elimaz.com.vn/crm/tests/arch 0.930s` |
| 32 operation: không thẻ 401, không quyền 403, owner 501 theo `m7_pending.txt` | `TestM7Contract_StatusCodes` | `go test -count=1 -v -run '^TestM7Contract_StatusCodes$' ./internal/httpapi/` | `--- PASS: TestM7Contract_StatusCodes (0.71s)`, `ok elimaz.com.vn/crm/internal/httpapi 1.408s` |
| Thứ tự khóa: nhóm 6, 7, 8, 11, 12a đúng kế hoạch, một dòng 12a | `TestM7Contract_LockOrder`, `TestLockOrderFrame` | lệnh A | `--- PASS: TestM7Contract_LockOrder (0.08s)`, `--- PASS: TestLockOrderFrame (0.06s)` |
| Hai view mới trong views.md, `channel_accounts_v` không đổi | `TestM7Contract_ViewsDoc`, `TestViewsDocParses` | lệnh A | `--- PASS: TestM7Contract_ViewsDoc (0.08s)`, `--- PASS: TestViewsDocParses (0.06s)` |
| Quyền, vai trò mẫu, `web.catalog.preview`, quyen.md khớp mã | `TestPermissionsList`, `TestDefaultRoles_Content`, `TestDefaultRoles_M7`, `TestPermissions_M7Targets`, `TestCan_M7`, `TestEnsureDefaultRoles_M7`, `TestServiceKeyPermissions`, `TestPermissionsDoc_MatchesCode`, `TestPermissionCatalog_Complete` | `go test -count=1 -v -run '^(TestPermissionsList\|TestDefaultRoles_Content\|TestDefaultRoles_M7\|TestPermissions_M7Targets\|TestCan_M7\|TestEnsureDefaultRoles_M7\|TestServiceKeyPermissions\|TestPermissionsDoc_MatchesCode\|TestPermissionCatalog_Complete)$' ./internal/modules/identity/` | 9 dòng `--- PASS` (vd `--- PASS: TestEnsureDefaultRoles_M7 (0.65s)`, `--- PASS: TestCan_M7 (0.00s)`), `ok elimaz.com.vn/crm/internal/modules/identity 1.104s` |
| Nối dây: mục đích tệp khớp bảng; seed-staging khung | `TestRegisterFilePurposes_MatchTable`, `TestSeedStaging_Frame` | `go test -count=1 -v -run '^(TestRegisterFilePurposes_MatchTable\|TestSeedStaging_Frame)$' ./cmd/crm/` | `--- PASS: TestSeedStaging_Frame (0.00s)`, `--- PASS: TestRegisterFilePurposes_MatchTable (0.40s)`, `ok elimaz.com.vn/crm/cmd/crm 1.141s` |
| Mã lỗi mới có câu tiếng Việt | `errors.test.ts` | `cd console && npx vitest run src/app/errors.test.ts` | `17 passed` |
| Cổng tĩnh (lint, jscpd, oasdiff, gen-check, test-guard, gitleaks) + test theo gói, bản đã gộp main và đã stage | ci-task lượt cuối | `ci-task.sh` của task (run 46104164441, tree 58dc143c) | `static: success`, `test (1)`…`test (6): success`, `REMOTE-TEST XANH`, `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 27d4fb0d…)` |
| (lịch sử) Lượt trước khi gộp main | ci-task lượt 3 | run 46066536900; reporting chạy riêng run 46068628201 | static xanh, phần test 2 đỏ ở 3 test EXPLAIN của `reporting` (`statement timeout` khi dựng dữ liệu mẫu, gói không sửa); reporting riêng XANH; lượt cuối ở trên xanh cả 6 phần |

## Test bị xóa hoặc tắt

(không xóa, không tắt test nào; không nới assertion.) Hai chỗ thu phạm vi có lý do, ghi ở "Khác kế hoạch":
`TestM3Routes_PendingList` đặt `RateLimit{Max: 10_000}` cho môi trường test (gọi mọi route bằng một người); hai test
M6 bỏ qua đường `/api/v1/reports/launch-*`, `/api/v1/reports/watch*` (do `TestM7Contract_*` phủ).

## Khác kế hoạch

- Thêm 3 operation đọc ngoài bảng 29 của kế hoạch: `listOpeningStocktakes`, `getOpeningStocktake` (`inventory.read`, theo
  kho: màn 10 của Task 273 cần "còn N dòng chưa đếm lấy từ máy chủ", kế hoạch không có đường đọc trạng thái phiên mở
  sổ), `getLegacyReturn` (`inventory.legacy_return.create`: "409 hiện liên kết tới phiếu đã có" cần đọc được phiếu theo
  `existingId`). Tổng 32 operation, 17 route ghi.
- Mã lỗi khai trước cho task sau (kế hoạch chỉ nêu mã ở tiêu chí test của từng task): 24 mã, trong đó tự đặt tên cho ba
  chỗ kế hoạch chỉ ghi số HTTP: `step_not_skippable` (skip bước 2, 6, 7 → 422), `run_closed` (thao tác trên lượt đã
  dừng, đã xong → 409), `decider_invalid` (người quyết không active hoặc thiếu quyền → 422), `old_tool_not_confirmed`
  (`ErrOldToolNotConfirmed` 422). Trường tùy chọn mới của `Error`: `count` (`uncounted_lines`), `existingId`
  (`legacy_return_exists`, `import_exists`), `candidates` (`ambiguous_code`).
- Đổi tên trường kế hoạch `ordersTotal` của bản nhập dự phòng thành `ordersInFile` (test `TestMoneyFieldsContract` coi
  tên kết thúc bằng `Total` là trường tiền; đây là số đơn).
- Test phụ phải sửa vì số route tăng: `TestM3Routes_PendingList` gọi mọi route bằng một chủ shop, vượt ngưỡng 300
  yêu cầu/phút → môi trường test đặt `RateLimit{Max: 10_000}`; `TestViewsDocParses` đếm 53 view (thêm 2 dòng M7);
  hai test M6 bỏ qua đường `/api/v1/reports/launch-*`, `/api/v1/reports/watch*` của M7; `checkViewsDocCols` tách từ
  `TestM2Contract_ViewsDoc` để dùng chung với M7 (jscpd). `web.catalog.preview` thêm vào danh sách của
  `TestServiceKeyPermissions` (không viết test lặp).
- Không sửa `tests/arch/routes_test.go` (luật chung của nó đã phủ route M7: không công khai, quyền có thật, route ghi
  `required`); kiểm riêng ở `TestM7Contract_Operations`.
- Lệnh `crm seed-staging` đi qua `runOneOffCommand` (`cmd/crm/import_website.go`), không thêm nhánh vào `run` (funlen).

## Việc phát hiện thêm

- Cho Task 270: `reportParamError` (`internal/httpapi/errors_reports.go`) áp cho mọi đường `/api/v1/reports/`, kể cả
  route M7 (`listLaunchRuns`…): tham số query sai ra `bad_params`/`bad_date` như M6, không phải `bad_request`.

## Reviewer

Kết luận: ĐẠT (lượt 2, 28/11, bản đã gộp main 27d4fb0d).

- Lượt 1: CẦN SỬA, chỉ ở tệp task: bảng bằng chứng ghi lệnh sai gói (test của `tests/arch` ghi lệnh của `httpapi`),
  lệnh còn `'...'`, dòng ci-task chưa ghi lượt xanh cuối. Mã, hợp đồng, quyền, phạm vi, mã lỗi, nối dây: không lỗi.
- Đã sửa: ghi đúng lệnh từng gói, trích `--- PASS`/`ok` thật, thêm ci-task run 46104164441 XANH; ghi hai chỗ thu
  phạm vi test vào "Test bị xóa hoặc tắt"; ghi chú `reportParamError` cho Task 270.
- Lượt 2: ĐẠT, không còn phát hiện phải sửa.
