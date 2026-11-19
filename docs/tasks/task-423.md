# Task 423 — Dữ liệu thử lớn dựng một lần làm database mẫu (M1-x9)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 423, M1-x9). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm mới trong `internal/testkit` (gói chỉ dùng trong
  `_test.go`): `Seed`, `StartPostgresSeeded`, `OpenStaffSeeded`, `FixedUUID` (0 chỗ dùng trước task). Không đổi chữ
  ký nào có sẵn. Hàm riêng đổi: `OpenStaff` tách phần mở pool thành `openStaff` (cùng hành vi); `dropStale` thử thêm
  khóa (727393, hashtext(tên)) trước khi xóa một mẫu.
- Gói phụ thuộc ngược (go list): 34 gói test nhập `internal/testkit` → đổi testkit nên ci-task chạy toàn bộ test.
  Gói test được chuyển: `internal/queries`, `internal/modules/customer` (search_explain), `internal/platform/tenancy`
  (search), `internal/platform/queue` (claimplan, besteffort), `internal/platform/schedules` (cleanup).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (chỉ dữ liệu thử trong test; không migration, không câu sqlc).
- Test sẽ chạy: `go test -race ./internal/testkit/` (test mới `TestSeeded*`, `TestCleanupStaleSkipsSeededTemplateBeingBuilt`,
  `TestFixedUUID` và test cũ của Task 391); toàn gói `internal/queries`; `-run TestSearchPlans` customer; `-run TestSearch`
  tenancy; `-run 'TestClaimLongQueueSameOrderKey|TestClaimPlanUsesIndexes'` queue; `-run TestCleanupJobsPlanUsesIndex`
  schedules; rồi ci-task.sh (toàn bộ vì đổi testkit).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ phủ `internal/platform/*`
  (không có gì về database thử). Trong testkit: `cloneFromTemplate`, `ensureTemplate`, `templateName`, `templateState`,
  `cleanupStale`/`dropStale`, `newTestDB`, `dropAtCleanup`, `withAdmin` của Task 391; `EnableAutoExplain`.
- Dùng lại / viết mới vì: dùng lại nguyên `templateName`, `ensureTemplate` (mẫu migration là nguồn của mẫu có dữ liệu),
  `templateState`, `cleanupStale` (cùng luật dọn, cùng tiền tố `crm_tpl_`), `newTestDB`, `dropAtCleanup`. Viết mới
  `createFromTemplate` (vòng thử lại 3D000/55006, dùng cho hai bước: mẫu migration → mẫu có dữ liệu, mẫu có dữ liệu →
  database test). `cloneFromTemplate` có vòng giống hệt nhưng không đổi trong task này (không trộn đổi cấu trúc vào
  commit hành vi; gộp là việc dọn sau, ghi ở "Việc phát hiện thêm"). `FixedUUID` mới: chưa có hàm mã cố định theo tên.

## Soát thiết kế

5.1–5.12: không áp dụng — chỉ đổi mã test (`internal/testkit`, tệp `_test.go`), không có bảng, module, đường ghi,
thư viện mới (`github.com/google/uuid` đã có, `uuid.NewSHA1` là hàm của nó).

6.1 (đồng thời): nhiều tiến trình, nhiều phiên cùng cần một mẫu có dữ liệu chưa có → khóa advisory cấp phiên
(727393, hashtext(tên mẫu)) trên database mặc định, kiểm lại `datistemplate` sau khi có khóa; mẫu dở dang bị xóa,
dựng lại. Khóa theo tên nên dựng bộ lớn không chặn phiên đang cần bộ khác hay mẫu migration (727391). Dọn mẫu cũ
(`dropStale`) thử thêm khóa này: không xóa mẫu đang được dựng lại cùng tên (test `TestCleanupStaleSkipsSeededTemplateBeingBuilt`).
Va chạm hashtext giữa hai tên chỉ làm hai lần dựng chờ nhau, không sai.
6.3 (thứ tự khóa): người dựng mẫu có dữ liệu giữ khóa 727393 rồi (nếu thiếu mẫu migration) lấy 727391; `dropStale`
giữ 727392 → thử 727391 → thử 727393, đều là *thử* (không chờ) nên không vòng chờ.
6.8 (chết giữa chừng): tiến trình dựng chết → khóa cấp phiên tự nhả, mẫu chưa có `datistemplate` → lần sau xóa, dựng lại
(test `TestSeededIncompleteIsRebuilt`); câu seed lỗi → xóa mẫu dở rồi test đỏ.
6.9 (treo lâu): dựng có thời hạn `seedTimeout` 8 phút (lúc máy bận một câu INSERT lớn từng chạy 2 phút).
6.2, 6.4–6.7, 6.10–6.13: không áp dụng — không có yêu cầu HTTP, việc nền, tiền, dữ liệu cá nhân, quyền (chỉ database thử).

## Quyết định thay chủ shop

1. **Seed là danh sách câu SQL, không phải hàm `seed func`** như kế hoạch gợi ý: mã băm phải phủ "nội dung câu seed";
   một hàm Go không băm được, nên `testkit.Seed{Name, SQL []string}` và mã băm là nguyên văn mọi câu. Câu chạy lần lượt
   trên một kết nối người quản trị; bộ nào trước đây chạy bằng chủ bảng trong ngữ cảnh tenant thì mở đầu bằng
   `SET ROLE crm_owner` và `set_config('app.tenant_id', …, false)` — giữ nguyên câu cũ và nguyên tác dụng RLS. Phương án
   khác: hàm seed kèm chuỗi phiên bản do người viết tự tăng (dễ quên tăng → dùng nhầm mẫu cũ).
2. **Mã cố định:** `testkit.FixedUUID(tên)` = UUID v5 theo tên (không gian tên cố định trong testkit). Mã do cột mặc định
   (uuidv7) hay `random()` sinh trong câu vẫn ngẫu nhiên nhưng đóng băng lúc dựng mẫu (cố định trong một giờ).
3. **Không chuyển** các test dựng dữ liệu nhỏ hoặc dựng bằng mã module (xem "Khác kế hoạch").

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Dữ liệu dựng một lần mỗi nội dung; đổi một ký tự câu hay đổi tên bộ là mẫu mới; mẫu migration không migrate lại; ANALYZE (reltuples, pg_stats) đi theo bản sao; bản sao không phải mẫu | `TestSeededBuildsOncePerContent` | `go test -race -count=1 -run 'TestSeeded\|TestCleanupStale\|TestFixedUUID\|TestTemplate' ./internal/testkit/` | `ok elimaz.com.vn/crm/internal/testkit 27.126s` |
| Nhiều tiến trình cùng lúc: dựng một lần (khóa) | `TestSeededConcurrentBuildOnce` (6 test song song cùng giá trị random(), migrate 1 lần) | như trên | ok |
| Mẫu dở dang bị xóa, dựng lại | `TestSeededIncompleteIsRebuilt` | như trên | ok |
| Dọn mẫu cũ cùng luật 391, không xóa mẫu đang được dựng lại | `TestSeededNameFitsAndIsCleanedUp`, `TestCleanupStaleSkipsSeededTemplateBeingBuilt`, test cũ `TestCleanupStaleDropsOnlyIdleOldDatabases` | như trên | ok |
| Mã cố định theo tên | `TestFixedUUID` | như trên | ok |
| Test đã chuyển chạy đúng như trước, cùng tiêu chí | toàn gói `internal/queries`; customer `TestSearchPlans`; tenancy `TestSearchFunctionSeesOnlyOwnTenant`, `TestSearchPlans`, `TestExplainAsAppNeedsAutoExplainForFunctions`; queue `TestClaimLongQueueSameOrderKey` (4 ca), `TestClaimPlanUsesIndexes`; schedules `TestCleanupJobsPlanUsesIndex` | `go test -race -count=1 [-run …] <gói>` | `ok …/queries 153.098s`; `ok …/customer 26.136s`; `ok …/tenancy 44.267s`; `ok …/queue 87.453s`; `ok …/schedules 7.367s` |
| Không nới tiêu chí | diff các tệp `_test.go` chỉ đổi phần dựng dữ liệu; không assertion, ngưỡng nào đổi | `git diff --cached -- '*_test.go'` | — |
| Cổng kiểm của task (toàn bộ test vì đổi testkit) | ci-task | ci-task (chạy ở nền, log ở scratchpad) | Lần 1 ĐỎ ở 3 cổng: jscpd (4 đoạn trùng trong `pgseed_test.go`, đã sửa: `dropNamedTemplateAtCleanup`, `adminExec`; chạy lại jscpd: `Found 0 clones`), test-guard (câu kiểm dời vào testkit, đã ghi ở "Test bị xóa hoặc tắt"), test toàn bộ: catalog, identity, inventory, order, promotion hết giờ 10 phút `go test` lúc load 40–65 (gói không đổi trong task; ngăn xếp kẹt ở câu của PostgreSQL chung, `statement timeout`). Chạy lại riêng: `ok …/catalog 324.427s`, `ok …/identity 412.865s`, `ok …/inventory 316.545s`, `ok …/order 227.613s`, `ok …/promotion 205.622s`; mọi gói khác `ok` |
| Cổng kiểm, lần 2 (sau khi sửa jscpd, test-guard) | ci-task | ci-task | `CI-TASK XANH (so với điểm tách nhánh e66dff2…)`: build, vet, lint, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; test toàn bộ 612 giây xanh |
| Hook Stop | build, vet, lint, test gói đổi và phụ thuộc | hook | mọi gói `ok` trừ customer `TestCreateCustomerPhoneTaken` (flake có sẵn, xem Việc phát hiện thêm); chạy lại `ok …/customer 48.981s` |

### Đo trước / sau (máy chung crm-shared, `go test -race -count=1`, thời gian đồng hồ của cả lệnh)

Máy không lúc nào rảnh trong phiên (load 40–65, 18 tiến trình `go test` của các phiên khác lúc đo): mọi số dưới là lúc
máy bận (nhiều hơn 4 phiên). Chưa đo được lúc máy rảnh (ghi ở Việc phát hiện thêm).

| Gói / test | Trước (load) | Sau, mẫu đã có (load) | Sau, lần đầu trong giờ (dựng mẫu) |
|---|---|---|---|
| `internal/queries` (toàn gói) | 566 s (40) | 156 s (44–47; `TestPlansHaveNoSeqScan` 12,8 s; `TestProducts` 133 s là thời gian của chính các câu thử) | 325 s (40, dựng song song với lần đo "trước"); một lần 609 s hết giờ 10 phút của `go test` lúc load 63 (sang giờ mới, dựng lại) |
| customer `TestSearchPlans` | 51 s (42) | 34 s (63) | — |
| tenancy `^TestSearch` | 26 s (42) | 22 s (63) | — |
| queue `TestClaimLongQueueSameOrderKey`, `TestClaimPlanUsesIndexes` | 79 s (41) | 67 s (63) | 87 s |
| schedules `TestCleanupJobsPlanUsesIndex` | 10 s (41) | 22 s (63; phần lớn là biên dịch gói lúc máy bận) | — |

Lợi chính là tải PostgreSQL chung: mỗi bộ chỉ chạy các câu `INSERT … generate_series` một lần mỗi giờ cho mọi phiên,
thay vì mỗi lần chạy test của mỗi phiên (`internal/queries` trước dựng 2 lần mỗi lần chạy).

## Test bị xóa hoặc tắt

Không test nào bị xóa, tắt hay nới ngưỡng. Một số câu `t.Fatal` kiểm *lỗi khi dựng dữ liệu* (không phải tiêu chí của
test) dời vào testkit cùng các câu dựng: lỗi nối hay lỗi một câu seed giờ làm test đỏ ở `runSeed` / `ensureSeeded`
(`t.Fatalf("testkit: dựng dữ liệu %q vào database mẫu %s: %v", …)`), như trước.

- `internal/platform/tenancy/search_test.go`: số câu kiểm giảm từ 17 xuống 15 — hai `t.Fatal` của `probeSearch` (lỗi
  `pgx.Connect` và lỗi từng câu dựng) dời vào testkit vì `probeSearch` thành `probeSeed` (danh sách câu). Mọi câu kiểm
  của `TestSearchFunctionSeesOnlyOwnTenant`, `TestSearchPlans`, `TestExplainAsAppNeedsAutoExplainForFunctions` giữ nguyên.
- `internal/queries/fixture_test.go`: 4 → 2 — `t.Fatal` lỗi nối và `t.Fatalf("dựng dữ liệu: …")` dời vào testkit.
- `internal/modules/customer/search_explain_test.go`: 10 → 9 — `t.Fatal` của câu ANALYZE (giờ là câu cuối của seed,
  lỗi thì testkit báo). Câu kiểm số danh tính 125.000–135.000 giữ nguyên.

## Khác kế hoạch

- Chữ ký: `testkit.StartPostgresSeeded(t, testkit.Seed{Name, SQL})` thay cho `(t, key, seed func)` (lý do ở Quyết định 1).
  Thêm `testkit.OpenStaffSeeded` (gói schedules dùng `testkit.Staff`) và `testkit.FixedUUID`.
- Test được chuyển (dữ liệu cố định, viết bằng SQL): `internal/queries` (`newFixture`: 10.000 mẫu, 50.000 biến thể…),
  `customer` `TestSearchPlans` (100.000 khách), `tenancy` `TestSearchFunctionSeesOnlyOwnTenant`, `TestSearchPlans`,
  `TestExplainAsAppNeedsAutoExplainForFunctions` (200.000 tên), `queue` `TestClaimLongQueueSameOrderKey` (4 ca × 150.000
  việc, cả `TestClaimLongQueueTimeBudget` nhãn perf dùng chung), `TestClaimPlanUsesIndexes` (100.000 việc), `schedules`
  `TestCleanupJobsPlanUsesIndex` (105.000 việc).
- Không chuyển: `inventory` `TestReconcile_LargeTenant` (dữ liệu dựng qua mã module — `catalogtest.Sellers`,
  `inventory.EnsureDefaultWarehouse` — chép thành SQL là đổi dữ liệu và lệch khi module đổi; 50.000 dòng, vài giây);
  `customer` `TestListWardsLimitCapped` (80 dòng), `httpapi` `TestListAdminUnitsAPI` (60 dòng), `schedules`
  `TestCleanupBatchLimit` (5.003 dòng, test chạy dọn trên chính dữ liệu đó): nhỏ, nhân bản không nhanh hơn bao nhiêu.
- Dữ liệu: câu giữ nguyên văn, trừ (a) mã tenant, người bán, kho, danh mục, nhãn: uuidv7 ngẫu nhiên → `FixedUUID`
  (queries, customer, queue; tenancy vốn đã cố định); (b) customer: `$1::int` (tham số) viết thẳng số `100000::int` /
  `2000::int`, chạy bằng `SET ROLE crm_owner` + `set_config('app.tenant_id', …)` thay cho `Staff.Exec` (cùng role, cùng
  RLS); tenant chèn kèm mã thay `Staff.Tenant`; (c) queue `longQueueSeed`: hạn giữ chỗ việc đầu đang running
  `now() + 1 hour` → `now() + 1 day` — mẫu cũ tới một giờ, hạn 1 giờ có thể vừa hết lúc đo và việc đầu thành nhận
  được (đổi kết quả test). Số dòng, giá trị kiểm, ANALYZE / VACUUM ANALYZE giữ nguyên, nằm trong mẫu. `random()`,
  `hashtext(id)` của customer vẫn ngẫu nhiên theo lần dựng (tiêu chí test là khoảng 125.000–135.000, không đổi).
- Thứ tự trong customer: kiểm số danh tính của A chạy sau khi nhân bản (trước: ngay sau khi chèn), cùng câu, cùng ngưỡng.

## Việc phát hiện thêm

- Mẫu có dữ liệu theo giờ như mẫu migration (tên gồm tên mẫu migration): lần đầu mỗi giờ vẫn trả toàn bộ chi phí dựng
  (`internal/queries` lúc load 63 từng hết giờ 10 phút của `go test`, như trước task). Có thể cho mẫu có dữ liệu sống
  lâu hơn một giờ nếu chứng minh được dữ liệu now() của migration không ảnh hưởng các test đã chuyển: cần quyết định riêng.
- `customer` `TestCreateCustomerPhoneTaken` (write_test.go:81) chập chờn: kiểm lỗi không chứa "912" nhưng thông điệp lỗi có
  mã khách uuidv7 ngẫu nhiên, mã kết thúc bằng `…d912` thì đỏ (gặp ở hook Stop 19/11). Sửa: kiểm không chứa cả SĐT
  `0912345678` / `912345678`, hoặc dùng mã cố định. Ngoài phạm vi task này.
- Chưa đo lúc máy rảnh (máy chung bận suốt phiên): điều phối viên đo lại lúc vắng phiên.
- `cloneFromTemplate` (Task 391) có vòng thử lại giống `createFromTemplate` mới: gộp được bằng một commit đổi cấu trúc riêng.

## Reviewer

Kết luận: ĐẠT (agent reviewer, một vòng, trên `git diff --cached` đã gộp main).

- Phát hiện phải sửa: không có. Reviewer đã đối chiếu: dữ liệu giữ nguyên trừ 3 điểm đã ghi (FixedUUID, số viết thẳng,
  hạn giữ chỗ 1 ngày); vai trò chạy câu và RLS tương đương (customer `SET ROLE` + `set_config`, queue/schedules chủ bảng
  không tenant, tenancy/queries người quản trị; bảng TEMP cần một kết nối, `runSeed` dùng một kết nối); khóa không vòng
  chờ; không nới tiêu chí.
- Ghi chú không bắt buộc: chưa đo lúc máy rảnh (đã ghi ở Việc phát hiện thêm); lần đầu ci-task đỏ — đã chạy lại, xanh;
  tên `TestSeededNameFitsAndIsCleanedUp` chỉ kiểm tiền tố (việc dọn thật do test `cleanupStale` phủ); gộp
  `createFromTemplate` với `cloneFromTemplate` để sau.
