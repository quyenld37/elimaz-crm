# Task 391 — Test dùng database mẫu, dọn database test bị bỏ sót (M0-x4)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. `testkit.StartPostgres` (và `OpenAppDB` gọi nó) đổi cách làm bên trong: nhân bản từ database mẫu thay vì migrate; 30 chỗ gọi trực tiếp `StartPostgres`/`OpenAppDB` (grep `testkit\.StartPostgres|testkit\.OpenAppDB`), cộng các trình trợ giúp trong testkit dùng nó (`OpenStaff`, `sellers`, …). `testkit.CreateDatabase` giữ nguyên hành vi (8 chỗ gọi: `internal/platform/db`, `tests/e2e/migrate_test.go`), chỉ thêm bước dọn và tên có mốc thời gian. Ký hiệu mới đều không xuất ra ngoài.
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
- 6.3 (deadlock): một khóa advisory duy nhất, không lồng khóa khác.
- 6.8 (tiến trình chết): mẫu dở dang được nhận ra (chưa `IS_TEMPLATE`) và dựng lại; database test bị bỏ sót được dọn khi cũ hơn 6 giờ.
- 6.9 (treo): mỗi bước có `context.WithTimeout`; chờ khóa dựng mẫu tối đa 2 phút.
- Dọn database: chỉ xóa `crm_test_*`/`crm_tpl_*` cũ hơn 6 giờ (tuổi đọc từ thời điểm tạo tệp `PG_VERSION` của database qua `pg_stat_file`, vì 74 database bỏ sót có tên không mốc thời gian), không có kết nối trong `pg_stat_activity`, và xóa bằng `DROP DATABASE` **không** `FORCE`: PostgreSQL tự từ chối nếu có ai vừa nối vào (an toàn cả khi có cuộc đua giữa lúc kiểm và lúc xóa). Không bao giờ xóa mẫu ứng với bộ migration hiện tại.
- 6.4–6.7, 6.10–6.13: không áp dụng (không tiền, không việc nền, không bên ngoài, không dữ liệu cá nhân, không quyền).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): tách `prepareEmpty`, `dropAtCleanup` khỏi `CreateDatabase`, hành vi giữ nguyên | các test của `internal/platform/db` (dùng `CreateDatabase` 6 chỗ) | `go test -count=1 -race ./internal/platform/db/` | `ok elimaz.com.vn/crm/internal/platform/db 63.340s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Commit 1 (cấu trúc, `pg.go` tách `prepareEmpty`/`dropAtCleanup`): **ĐẠT**, không phát hiện phải sửa. Ghi chú của reviewer: hẹn xóa database ngay sau `CREATE DATABASE` (sớm hơn câu GRANT) là chủ ý và còn bịt chỗ rò database khi câu GRANT hỏng.
