# Task 391 — Test dùng database mẫu, dọn database test bị bỏ sót (M0-x4)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. `testkit.StartPostgres` (và `OpenAppDB` gọi nó) đổi cách làm bên trong: nhân bản từ database mẫu thay vì migrate; 30 chỗ gọi trực tiếp `StartPostgres`/`OpenAppDB` (grep `testkit\.StartPostgres|testkit\.OpenAppDB`), cộng các trình trợ giúp trong testkit dùng nó (`OpenStaff`, `sellers`, …). `testkit.CreateDatabase` giữ nguyên hành vi (8 chỗ gọi: `internal/platform/db`, `tests/e2e/migrate_test.go`); chỉ tách phần đặt tên/chuỗi kết nối (`newTestDB`) và mở kết nối quản trị (`withAdmin`) để dùng chung với `cloneFromTemplate`, không thêm dọn. Ngữ nghĩa dữ liệu: giá trị `now()` mà migration ghi vào dữ liệu (0122: `next_run_at` của lịch `inventory.reconcile`, kiểm ở `internal/modules/inventory/reconcile_test.go` `reconcileNextRun`) là của lúc dựng mẫu; tên mẫu kèm giờ UTC nên cũ tối đa một giờ (sửa theo reviewer lượt 1). Ký hiệu mới đều không xuất ra ngoài.
- Gói phụ thuộc ngược (go list): 30 gói có test nhập `internal/testkit` (adapters/cfaccess, fakecarrier, httpapi, mọi module cash/catalog/fulfillment/identity/inventory/order(+pricing), platform/commands, db, events, files, idempotency, metrics, observability, queue, realtime, schedules, selftest, tenancy, webhooks, xlsx, internal/queries, testkit, testkit/shiptest, tests/arch, tests/e2e, tools/loadkit). Chỉ mã test; tệp chạy `crm` không nhập testkit.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có câu sqlc, bảng hay migration mới. Chỉ lệnh quản trị của PostgreSQL thử (`CREATE DATABASE … TEMPLATE`, `ALTER DATABASE … IS_TEMPLATE`, `DROP DATABASE` không FORCE cho database cũ) chạy bằng role `postgres` dev trên database mặc định của máy chủ thử, trong testkit (chỉ tệp test dùng).
- Test sẽ chạy: test mới của testkit (`go test -race ./internal/testkit/...`); vì testkit dùng chung mọi test: `make ci` (toàn bộ `go test -race ./...`) một lần trước khi stage; đo thời gian `StartPostgres` và hai gói identity, catalog trước/sau.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit chỉ có StartPostgres/CreateDatabase/OpenAppDB, không có gì về database mẫu hay dọn); tìm `TEMPLATE`, `is_template`, `pg_advisory`, `DROP DATABASE` trong mã: chỉ có khóa advisory của `db.Migrate` (khóa trong database đang migrate, không dùng được để giữ việc dựng mẫu giữa các database) và `DROP DATABASE … WITH (FORCE)` trong cleanup của `CreateDatabase`.
- Dùng lại / viết mới vì: dùng lại `db.Migrate` (dựng mẫu), `CreateDatabase`'s cách dựng chuỗi kết nối (`serverURL`, `withDB`, `connect`, `exec`), `crypto/sha256` thư viện chuẩn để băm bộ migration. Viết mới trong `internal/testkit` (tệp `pgtemplate.go`): dựng/tìm database mẫu và dọn database cũ — chỉ test cần, không thuộc `internal/platform`. Không thêm thư viện.

## Soát thiết kế

- 5.1–5.10, 5.12: không áp dụng: chỉ đổi mã hạ tầng test, không dữ liệu nghiệp vụ, không module, không HTTP.
- 5.11: không thêm thư viện (chỉ `crypto/sha256`, `io/fs`, pgx đã có).
- 6.1 (đồng thời): nhiều tiến trình `go test` (nhiều gói, nhiều phiên trên crm-shared) cùng dựng một mẫu → `pg_advisory_lock` trên database mặc định của máy chủ (khóa advisory theo database, nên mọi phiên khóa trên cùng một database), kiểm lại "mẫu đã xong" sau khi có khóa. Mẫu chỉ được coi là xong khi `datistemplate = true` (đặt ở bước cuối); mẫu dở dang (tiến trình chết giữa chừng) thì người có khóa xóa và dựng lại. Nhiều `CREATE DATABASE … TEMPLATE` cùng lúc từ một mẫu: PostgreSQL cho phép khi không ai nối vào mẫu; mẫu đặt `ALLOW_CONNECTIONS false` nên không ai nối được.
- 6.2: không áp dụng (không có yêu cầu HTTP).
- 6.3 (deadlock): hai khóa advisory: khóa dựng mẫu (727391, chờ, trong `ensureTemplate`, không giữ khóa nào khác) và khóa dọn (727392, chỉ thử). `dropStale` thử khóa dựng mẫu khi đang giữ khóa dọn; lần lấy lồng chỉ là thử, không chờ, nên không deadlock.
- 6.8 (tiến trình chết): mẫu dở dang được nhận ra (chưa `IS_TEMPLATE`) và dựng lại; database test bị bỏ sót được dọn khi cũ hơn 6 giờ.
- 6.9 (treo): mỗi bước có `context.WithTimeout`; cả bước dọn + dựng mẫu + nhân bản tối đa 3 phút.
- Dọn database: chỉ xóa `crm_test_*`/`crm_tpl_*` cũ hơn 6 giờ (tuổi đọc từ thời điểm tạo tệp `PG_VERSION` của database qua `pg_stat_file`, vì 74 database bỏ sót có tên không mốc thời gian), không có kết nối trong `pg_stat_activity`, và xóa bằng `DROP DATABASE` **không** `FORCE`: PostgreSQL tự từ chối nếu có ai vừa nối vào (an toàn cả khi có cuộc đua giữa lúc kiểm và lúc xóa). Không bao giờ xóa mẫu ứng với bộ migration hiện tại.
- 6.4–6.7, 6.10–6.13: không áp dụng (không tiền, không việc nền, không bên ngoài, không dữ liệu cá nhân, không quyền).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mọi test hiện có vẫn qua với testkit mới (sau khi gộp main 9b0a7b7) | toàn bộ `go test -race ./...` trong `make ci` | `make ci` (sau khi stage) | build, vet, lint (0 issues), gen-check, sqlc vet, … xanh; test: 47 gói `ok`, 2 gói đỏ không do task này: `TestClaimLongQueueSameOrderKey/best_effort/head_running=false` vượt trần thời gian (71.45 ms > 50 ms, kế hoạch không Seq Scan) lúc cả bộ chạy trên máy chủ chung — chạy riêng `go test -count=3 -race -run TestClaimLongQueueSameOrderKey ./internal/platform/queue/` → `ok 20.593s`; `TestMakeMutation_CoversHighRiskDirs` (tests/deploy, kiểm Makefile) đỏ sẵn trên main lúc gộp, đã sửa trên main ở 39af757 sau lần gộp (task không chạm Makefile, tests/deploy) |
| Sang giờ sau dùng mẫu mới (dữ liệu now() của migration không cũ quá một giờ); cả giờ dùng chung một mẫu | `TestTemplateMigratesOncePerMigrationSet` (phần `next`: đếm migrate 3) | `go test -count=1 -race -v -run 'Template\|Cleanup' ./internal/testkit/` | `--- PASS: TestTemplateMigratesOncePerMigrationSet (5.67s)` |
| Dọn không xóa mẫu khi khóa dựng mẫu đang bị giữ, kiểm lại tuổi dưới khóa | `TestCleanupStaleDropsOnlyIdleOldDatabases` (phần `builder` giữ `pg_advisory_lock(727391)`) | như trên | `--- PASS: TestCleanupStaleDropsOnlyIdleOldDatabases (3.32s)` |
| Test lịch đối chiếu (dữ liệu now() của 0122) qua với database nhân bản | `TestReconcile_NightlySchedule` | `go test -count=1 -race -run TestReconcile_NightlySchedule ./internal/modules/inventory/` | `ok elimaz.com.vn/crm/internal/modules/inventory` |
| Sau khi sửa theo reviewer lượt 1: mọi test hiện có vẫn qua | toàn bộ `go test -race ./...` trong `make ci`, rồi các cổng sau bước test chạy riêng | `make ci`; `make vuln oasdiff catalog-check jscpd test-guard gitleaks console-check`; `make oasdiff OASDIFF_BASE=-base-ref=$(git merge-base HEAD main)`; `git diff --name-status $(git merge-base HEAD main) -- tests/arch` | build, vet, lint, gen-check, sqlc vet xanh; test: 48 gói `ok`, chỉ `TestMakeMutation_CoversHighRiskDirs` đỏ (sẵn trên main lúc gộp, main đã sửa ở 39af757); vuln, catalog-check, jscpd (0 clones), gitleaks, console-check xanh; oasdiff và test-guard đỏ khi so với đầu main hiện tại vì main có commit mới sau lần gộp (`tests/arch/money_fields_test.go` "bị xóa" = chưa có ở nhánh này); so với điểm tách nhánh: oasdiff xanh, `tests/arch` không đổi |
| Sau lượt 2: test dọn không phụ thuộc khóa của tiến trình khác | `TestCleanupStaleDropsOnlyIdleOldDatabases` và cả nhóm test database mẫu, chạy 2 lần | `go test -count=2 -race -v -run 'Template\|Cleanup' ./internal/testkit/` | 10 `--- PASS`, `ok elimaz.com.vn/crm/internal/testkit 45.140s`; golangci-lint testkit `0 issues` |
| Commit 1 (cấu trúc): tách `prepareEmpty`, `dropAtCleanup` khỏi `CreateDatabase`, hành vi giữ nguyên | các test của `internal/platform/db` (dùng `CreateDatabase` 6 chỗ) | `go test -count=1 -race ./internal/platform/db/` | `ok elimaz.com.vn/crm/internal/platform/db 63.340s` |
| Lần hai cùng bộ migration không migrate lại; thêm migration → mẫu tự dựng lại (một lần) | `TestTemplateMigratesOncePerMigrationSet` | `go test -count=1 -race -run 'Template\|Cleanup' -v ./internal/testkit/` | `--- PASS: TestTemplateMigratesOncePerMigrationSet` (đếm migrate 1 rồi 2; bảng t1, t2 có trong database nhân bản) |
| Nhiều test song song cùng dựng một mẫu: migrate đúng một lần (khóa advisory) | `TestTemplateConcurrentBuildMigratesOnce` (6 test con `t.Parallel`) | như trên | `--- PASS: TestTemplateConcurrentBuildMigratesOnce (1.21s)` |
| Mẫu dở dang (chưa IS_TEMPLATE) bị xóa và dựng lại | `TestTemplateIncompleteIsRebuilt` | như trên | `--- PASS: TestTemplateIncompleteIsRebuilt (0.79s)` |
| `StartPostgres` dùng mẫu của `migrations.FS`; mẫu chặn kết nối; database test không phải mẫu, crm_app nối được, migrate lại không áp dụng gì | `TestStartPostgresClonesMigratedTemplate` (đỏ trước khi đổi `StartPostgres`: `FAIL elimaz.com.vn/crm/internal/testkit 1.316s`) | `go test -count=1 -race ./internal/testkit/...` | `ok elimaz.com.vn/crm/internal/testkit 12.992s` |
| Dọn: chỉ xóa database đúng tiền tố, cũ hơn ngưỡng, không kết nối; giữ database đang có kết nối, giữ mẫu hiện tại, giữ database chưa đủ tuổi; xóa được mẫu cũ | `TestCleanupStaleDropsOnlyIdleOldDatabases` (tiền tố riêng của lần chạy, không chạm database của phiên khác) | như dòng 2 | `--- PASS: TestCleanupStaleDropsOnlyIdleOldDatabases (3.54s)` |
| Đo `StartPostgres` trước/sau (5 lần liên tiếp, máy chủ chung crm-shared đang có phiên khác chạy) | test đo tạm (xóa sau khi đo) | `go test -count=1 -run TestZZBaseline -v ./internal/testkit/` | trước: 1.115 s, 0.845 s, 1.186 s, 1.613 s, 1.406 s (TB ≈ 1.23 s); sau: 0.348 s (lần đầu, có dọn), 0.202 s, 0.129 s, 0.215 s, 0.103 s (TB ≈ 0.2 s) |
| Thời gian cả gói trước/sau (nhiễu lớn do các phiên khác cùng dùng crm-shared, chỉ để tham khảo) | `./internal/modules/identity/...`, `./internal/modules/catalog/...` | `/usr/bin/time -p go test -count=1 -race ./internal/modules/<gói>/...` | identity: 337.9 s → 261.6 s; catalog: 117.7 s → 200.8 s (lần sau trùng lúc tải máy chủ cao; thời gian gói phần lớn là chính các test, không phải dựng database) |
| Dọn database bỏ sót thật trên crm-shared | lần chạy test đầu tiên với mã mới | `psql … "select count(*) from pg_database where datname like 'crm\_test\_%'"` | 74 trước → 64 sau lần chạy đầu (chỉ những database cũ hơn 6 giờ và không kết nối; phần còn lại mới hơn 6 giờ, sẽ được dọn khi đủ tuổi) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tuổi của database khi dọn đọc từ thời điểm tạo tệp `PG_VERSION` của database (`pg_stat_file`, người quản trị dev là superuser) thay vì mốc thời gian trong tên: 74 database bỏ sót có tên `crm_test_<hex>` không có mốc, và các phiên chưa gộp bản này vẫn tạo tên kiểu cũ. Một quy tắc cho mọi database; tên database test giữ dạng cũ.
- Tên mẫu là `crm_tpl_<băm 16 hex>_<YYYYMMDDHH UTC>` (kế hoạch: `crm_tpl_<hash>`): mỗi giờ một mẫu, để dữ liệu `now()` của migration (0122) không cũ quá một giờ — reviewer lượt 1 chỉ ra `TestReconcile_NightlySchedule` sẽ đỏ với mẫu sống nhiều ngày. Giá: migrate một lần mỗi giờ cho mỗi bộ migration (~1 s); mẫu giờ cũ được dọn sau 6 giờ (~11 MB mỗi mẫu).
- Dọn chạy ở mỗi lần nhân bản (không chỉ khi tạo mẫu), sau `pg_try_advisory_lock` (không chờ): một câu SELECT trên `pg_database`, rẻ; nhờ vậy database bỏ sót được dọn kể cả khi mẫu đã có sẵn.

## Quyết định thay chủ shop

- Mẫu của bộ migration khác (worktree khác, bản cũ) cũ hơn 6 giờ cũng bị dọn; phiên còn dùng nó sẽ tự dựng lại (nhân bản gặp 3D000 thì dựng lại rồi thử lại). Phương án khác: không bao giờ dọn mẫu — mỗi bộ migration để lại một database mãi mãi (mỗi task thêm migration là thêm một mẫu). Chọn dọn vì dễ đảo ngược (mẫu dựng lại ~1 s) và giữ máy chủ chung sạch.
- Mẫu đặt `ALLOW_CONNECTIONS false` ngoài `IS_TEMPLATE true` để bảo đảm "mẫu không ai kết nối" (một kết nối vào mẫu làm mọi lần nhân bản báo 55006). Có thử lại tối đa 5 lần cho 55006 (kết nối của bước migrate chưa thoát hẳn).

## Việc phát hiện thêm

- `TestClaimLongQueueSameOrderKey` (queue) có trần thời gian 50 ms tuyệt đối: chập chờn khi nhiều phiên cùng chạy test trên crm-shared (lần này 71 ms). Không sửa trong task này (ngoài phạm vi, không nới test).
- `TestMakeMutation_CoversHighRiskDirs` đỏ ở bản main đã gộp (9b0a7b7); main đã sửa ở 39af757, điều phối viên gộp sẽ có.

## Reviewer

Commit 2 (hành vi), lượt 1: **CẦN SỬA** — (1) dữ liệu `now()` của migration 0122 bị đóng băng ở lúc dựng mẫu, mẫu sống nhiều ngày làm `TestReconcile_NightlySchedule` đỏ → sửa: tên mẫu kèm giờ UTC, test "sang giờ sau dựng mẫu mới"; (2) dọn có thể xóa mẫu cùng tên vừa được dựng lại → sửa: xóa mẫu chỉ khi giữ được `pg_try_advisory_lock(templateLockKey)` và kiểm lại tuổi dưới khóa, có test. Sửa thêm câu chữ mục Ảnh hưởng (CreateDatabase), Soát thiết kế 6.9 (3 phút), ghi chú `templateRecipe` cạnh `prepareEmpty`, ghi chú khóa advisory theo database.

Commit 2, lượt 2: **CẦN SỬA** — hai chỗ sửa lượt 1 đúng và đủ; `TestCleanupStaleDropsOnlyIdleOldDatabases` có thể đỏ thất thường khi tiến trình khác trên crm-shared đang giữ khóa dọn/khóa dựng mẫu (cleanupStale chỉ thử khóa) → sửa: test cho kết nối `admin` giữ sẵn (chờ) hai khóa trước các lần gọi dọn mong có xóa (khóa cấp phiên lấy lồng được); sửa câu 6.3 (hai khóa, lồng chỉ thử). Ghi chú không bắt buộc (dọn mỗi tiến trình một lần bằng sync.Once) không làm: biến toàn cục thay đổi được bị luật Go cấm, và câu SELECT dọn rẻ.

Commit 1 (cấu trúc, `pg.go` tách `prepareEmpty`/`dropAtCleanup`): **ĐẠT**, không phát hiện phải sửa. Ghi chú của reviewer: hẹn xóa database ngay sau `CREATE DATABASE` (sớm hơn câu GRANT) là chủ ý và còn bịt chỗ rò database khi câu GRANT hỏng.
