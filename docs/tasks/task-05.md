# Task 05 — Ngữ cảnh tenant, RLS, khóa ghép, kiểm quy tắc schema

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu **có sẵn** nào đổi chữ ký. `db.RetryTx`,
  `db.TxOptions`, `db.Setting`, `db.DB` chỉ được dùng thêm (không sửa). Ký hiệu mới: gói `internal/platform/tenancy`
  (`WithTenant`, `Resolve`, `ListTenantIDs`, `CheckSchemaRules`, `ErrNoTenant`), trong `internal/testkit`
  (`ExplainAsApp`, `RequireNoSeqScan`, `EnableAutoExplain`). Mã sqlc sinh thêm trong `internal/platform/internal/store`
  (câu mới, không đổi câu cũ).
- Gói phụ thuộc ngược (go list): `internal/platform/db` đang được `cmd/crm`, `internal/testkit` nhập (không đổi gì ở
  đó). `internal/platform/internal/store` được `db` nhập: chỉ thêm hàm sinh, mã cũ không đổi. `internal/testkit` được
  test của `db`, `tests/arch`, `tests/e2e` nhập: chỉ thêm hàm. `tenancy` mới, chưa ai nhập (Task 7, 9, 12, 14 sẽ nhập).
- Operation OpenAPI bị chạm, oasdiff: không (chưa có `openapi.yaml`).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: pha **mở rộng**, `migrations/0002_rls_helpers.sql` (số theo Q-X7: Task 5 = 0002; Task 10
  chạy song song giữ 0006): chỉ tạo hàm `platform.enable_tenant_rls(regclass)`, không tạo bảng nên không thêm dòng vào
  `docs/adr/0046-thu-tu-khoa.md`. Câu sqlc mới: `queries/platform/tenant_routes.sql` (đọc `platform.tenant_routes`,
  `platform.tenants`, crm_app đã có quyền SELECT từ Task 4) và `queries/platform/schemarules.sql` (đọc `pg_catalog`).
- Test sẽ chạy: `go test -race ./internal/platform/tenancy/... ./internal/testkit/... ./internal/platform/db/...
  ./tests/arch/...` trên PostgreSQL của dự án compose `crm-t05` (cổng 5505). `tenancy` là vùng rủi ro cao: chạy cả bộ
  của gói, có test rapid. Cuối: `make ci` phần đã có.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có config, db, logging,
  observability, telemetry. Không có gì về tenant, RLS, kiểm schema, EXPLAIN. Tìm `tenant`, `set_config`, `EXPLAIN`,
  `pg_catalog`, `rls` trong `internal/`, `tests/`, `scripts/`: có `db.RetryTx` + `db.Setting` (Task 4 làm sẵn chỗ cắm
  `app.tenant_id`), câu `SetTxConfig` (đặt mọi tham số trong một lượt), `testkit.StartPostgres`;
  `scripts/backup/search-plan-probe.sql` của Task 2 (psql, cụm diễn tập) là nguồn của bảng và hàm tìm thử.
- Dùng lại / viết mới vì:
  - `WithTenant` dùng lại `db.RetryTx` và `db.Setting` (không tự mở transaction, không vòng thử lại riêng), nên
    `app.tenant_id` đi cùng câu `set_config` có sẵn của Task 4.
  - `Resolve`, `ListTenantIDs`, `CheckSchemaRules`: câu tĩnh qua sqlc vào `internal/platform/internal/store` có sẵn.
  - `testkit.ExplainAsApp` viết mới (chưa có gì đọc kế hoạch chạy). Bảng và hàm tìm thử trong `search_test` chép theo
    tệp SQL của Task 2 (cụm diễn tập không dùng chung được với test Go); tệp shell của Task 2 giữ nguyên.
  - `rapid` v1.3.0 (ADR-44) thêm vào `go.mod` lần đầu cho test thuộc tính; không cần dòng `docs/phu-thuoc.md`.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: `tenant_routes`, `tenants` thuộc `platform` (đọc, không thêm bảng). Hàm `platform.enable_tenant_rls`
   thuộc `platform`, module nào cũng gọi trong migration của mình (luật migration số 5).
2. Không ghi bảng module khác: task không ghi bảng nào ngoài bảng thử `probe.*` của test.
3. Đọc qua view: không áp dụng: chưa có module nghiệp vụ.
4. Việc nguyên tử: `WithTenant` là một `RetryTx`, cả `fn` chạy trong một transaction.
5. Việc sau commit: không áp dụng: không phát sự kiện.
6. Gọi ra ngoài: không áp dụng. Tài liệu `WithTenant` nhắc lại luật "hàm không gọi mạng, chạy lại được".
7. Ngữ cảnh shop: đây là task dựng chính luật này: `app.tenant_id` đặt bằng `set_config(…, true)` (chỉ sống trong
   transaction), policy dùng `NULLIF(…, '')` để kết nối đã dùng lại trả 0 dòng thay vì lỗi; `CheckSchemaRules` kiểm
   RLS + FORCE + NULLIF, `UNIQUE (tenant_id, id)`, unique bắt đầu bằng `tenant_id`, khóa ngoại ghép, không bảng trong
   `public`.
8. Nghiệp vụ không biết HTTP: `tenancy` không nhập `net/http`.
9. Đường ghi có khóa chống trùng: không áp dụng: không có route.
10. PostgreSQL là nguồn sự thật: đúng; không dùng Valkey.
11. Thư viện: thêm `pgregory.net/rapid` v1.3.0 (có trong ADR-44), chỉ dùng trong test.
12. Module mẫu: không áp dụng: chưa có (Task 7).

Mục 6 (đúng khi chạy đồng thời):

1. Hai yêu cầu cùng sửa: không áp dụng: không có đường ghi nghiệp vụ.
2. Gửi lại: không áp dụng.
3. Thứ tự khóa: không tạo bảng mới nên không đổi `docs/adr/0046-thu-tu-khoa.md`.
4. Lỗi tạm thời: `WithTenant` dựng trên `RetryTx`; `app.tenant_id` nằm trong câu `set_config` đầu **mỗi** lần chạy
   lại (transaction mới), có test.
5. Mức cô lập: giữ READ COMMITTED của `RetryTx` (hoặc `IsoLevel` bên gọi truyền).
6–8. Việc nền, 429, tiến trình chết: không áp dụng; transaction chết tự lùi, `app.tenant_id` mất theo.
9. Treo lâu: dùng giới hạn theo transaction của `RetryTx` (`o[0]` hoặc `DefaultTx` của pool).
10. Tiền: không áp dụng.
11. Quy luật luôn đúng: `tenancy` là vùng rủi ro cao → test rapid 100 lượt: dữ liệu ngẫu nhiên của 1–5 tenant, mọi
    truy vấn trong `WithTenant(x)` chỉ trả dòng của `x`, ngoài ngữ cảnh 0 dòng.
12. Dữ liệu cá nhân: lỗi của `CheckSchemaRules`, `WithTenant` chỉ nêu tên bảng, tên ràng buộc; không giá trị dòng.
    Mã tenant (uuid) không phải dữ liệu cá nhân.
13. Quyền: `crm_app` (`NOBYPASSRLS`) chạy mọi truy vấn của test; policy có `WITH CHECK` nên không chèn được dòng của
    tenant khác; `enable_tenant_rls` thu quyền `EXECUTE` của `PUBLIC`; hàm tìm thử `SECURITY DEFINER` do `crm_search`
    làm chủ, `search_path` cố định.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đỏ trước khi có mã | cả gói | go test ./internal/platform/tenancy/... | `FAIL elimaz.com.vn/crm/internal/platform/tenancy [build failed]` (chưa có gói) |
| A, B chỉ thấy dòng của mình | TestTenantsSeeOnlyOwnRows | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS: TestTenantsSeeOnlyOwnRows` |
| Pool MaxConns 1: sau WithTenant(A), câu ngoài ngữ cảnh 0 dòng, không lỗi (Review Focus 3) | TestReusedConnectionOutsideContextSeesNothing | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS`; đột biến bỏ NULLIF khỏi policy → `invalid input syntax for type uuid: "" (SQLSTATE 22P02)` (test đỏ, đã hoàn lại) |
| WithTenant(A) chèn dòng của B → vi phạm policy | TestInsertForOtherTenantViolatesPolicy | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` (SQLSTATE 42501) |
| WithTenant(B) chèn con trỏ cha của A → lỗi khóa ngoại | TestChildPointingToOtherTenantParentViolatesForeignKey | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` (SQLSTATE 23503) |
| app.tenant_id, giới hạn, Settings của o[0] có ở mỗi lần chạy lại; không sửa Settings của bên gọi | TestWithTenantSetsContextOnEveryAttempt | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` (2 lần chạy, cả hai thấy tenant, 1500ms, 7s); đột biến `append(opt.Settings, …)` không sao chép → `WithTenant ghi vào mảng Settings của bên gọi` (đỏ, đã hoàn lại) |
| Hết lượt → db.ErrRetryLater; uuid.Nil → ErrNoTenant | TestWithTenantExhaustedReturnsRetryLater, TestWithTenantRejectsNilTenant | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` cả hai (4 lần gọi) |
| Resolve fb_page/p1 → tenant; khóa không có → ok=false; ListTenantIDs | TestResolveAndListTenantIDs | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` |
| CheckSchemaRules: schema sạch → []; 4 bảng vi phạm → 4 dòng; thêm bảng public → dòng thứ 5 | TestCheckSchemaRulesCleanSchema, TestCheckSchemaRulesFourViolationsThenPublic | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` cả hai; đột biến bỏ NULLIF → báo `policy tenant_isolation không đúng biểu thức` cho 2 bảng thử |
| Từng luật riêng (RLS tắt, không policy, policy mở thêm, WITH CHECK lạ, thiếu UNIQUE (tenant_id,id), unique index sai cột đầu, khóa ngoại ghép sai cặp) | TestCheckSchemaRulesEachRule (7 ca) | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` 7/7 |
| search_test: hàm SECURITY DEFINER chỉ ra dòng của tenant, ngoài ngữ cảnh 0; `%` không khớp mọi thứ | TestSearchFunctionSeesOnlyOwnTenant | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` |
| Qua hàm: Bitmap Index Scan, RequireNoSeqScan đạt; LIKE thẳng → Seq Scan | TestSearchPlans, TestRequireNoSeqScanFails, TestExplainAsAppNeedsAutoExplainForFunctions | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `--- PASS` cả ba |
| Thuộc tính rapid 100 lượt, 1–5 tenant | TestPropertyQueriesOnlySeeOwnTenant | go test -race -count=1 -v ./internal/platform/tenancy/ -args -rapid.v | `[rapid] OK, passed 100 tests` |
| Không vỡ gói khác | mọi gói | go test -race -count=1 ./... | mọi gói `ok` (db, tenancy, tests/arch, tests/e2e, tools/catalog…) |
| Cổng kiểm phần còn lại | build, vet, lint, sqlc vet, jscpd, test-guard, govulncheck, gitleaks dir | CGO_ENABLED=0 go build -trimpath ./...; go vet ./...; golangci-lint run ./...; make sqlc-vet jscpd test-guard; govulncheck ./...; gitleaks dir | build/vet im lặng; `0 issues.`; `ok tests/arch`; `Found 0 clones.`; test-guard thoát 0; `No vulnerabilities found.`; `no leaks found`. gen-check, catalog-check so với git nên chỉ xanh sau commit (chạy lại sau commit) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `queries/platform/schemarules.sql` (kế hoạch chỉ liệt `tenant_routes.sql`): `CheckSchemaRules` đọc `pg_catalog`
  bằng câu tĩnh qua sqlc, đúng luật SQL tĩnh.
- Thêm `testkit.EnableAutoExplain` ngoài Interfaces: kế hoạch bên trong hàm `SECURITY DEFINER` chỉ đọc được qua
  auto_explain (như Task 2).
- Sửa tên tệp thử trong `internal/platform/db/migrate_test.go` từ `0002_*.sql` sang `9001/9002/9003_*.sql`: tệp thử
  trùng số với migration thật `0002_rls_helpers.sql` làm goose báo trùng phiên bản. Không bớt assertion nào.

## Quyết định thay chủ shop

1. `platformTables` của `CheckSchemaRules` ghi dạng `schema.bảng` (`platform.tenant_routes`), theo luật C15; phương
   án khác: tên trần như Global Constraints (dễ trùng tên giữa hai schema).
2. Luật policy so **đúng nguyên văn** biểu thức chuẩn ở cả `USING` và `WITH CHECK`, và báo mọi policy khác trên bảng
   (một policy `USING (true)` thêm vào sẽ mở toang vì các policy cho phép cộng bằng OR). Kế hoạch chỉ nói "có NULLIF";
   chặt hơn, dễ nới sau nếu cần policy đặc biệt.
3. `WithTenant(uuid.Nil)` trả `tenancy.ErrNoTenant`, không chạy hàm (tenant rỗng luôn là lỗi của bên gọi).
4. `ExplainAsApp` mở một kết nối riêng từ cấu hình pool của `appDB` (để nhận NOTICE của auto_explain), đặt ngữ cảnh
   bằng `set_config(…, false)` trên kết nối đó rồi đóng; báo lỗi khi kế hoạch có `Function Scan` mà auto_explain chưa
   bật (tránh `RequireNoSeqScan` đạt sai). Phương án khác: thêm móc `OnNotice` vào `db.PoolOptions` (đổi gói `db`
   chỉ để phục vụ test).
5. `enable_tenant_rls` chạy với quyền người gọi (không `SECURITY DEFINER`), thu `EXECUTE` của `PUBLIC`: chỉ chủ hàm
   (`crm_owner`) và superuser gọi được, đúng vai trò migrate.
6. Bảng thử của `search_test` giữ 200.000 dòng như Task 2 (mỗi test ~1 s) để kế hoạch chạy giống phép thử gốc.

## Việc phát hiện thêm

- Danh sách bảng nền tảng có `tenant_id` hiện chỉ nằm trong test (`platformTables()` của `schemarules_test.go`); task
  sau thêm bảng nền tảng có `tenant_id` phải thêm vào đó. Có thể đưa thành hằng công khai của `tenancy` khi có bên
  gọi thứ hai (ví dụ cổng kiểm của Task 23).
- `TestCheckSchemaRulesCleanSchema` chạy trên schema thật sau migrate nên là cổng kiểm luật bảng cho mọi migration
  sau (đỏ ngay khi bảng mới thiếu RLS, khóa ghép…).

## Reviewer

Lần 1 (agent reviewer, trên `git diff --cached`): **ĐẠT**, không phát hiện phải sửa. Ba ghi chú không bắt buộc:

1. `ExplainAsApp` vẫn có thể đạt sai nếu auto_explain bật mà không NOTICE nào về → **đã sửa**: có `Function Scan` mà
   không đọc được kế hoạch nào của auto_explain thì trả lỗi.
2. Test "không sửa Settings của bên gọi" không bắt được hồi quy (mảng hết sức chứa) → **đã sửa**: mảng có sức chứa 4,
   kiểm phần tràn; đột biến bỏ sao chép làm test đỏ.
3. Luật (b) đòi đúng thứ tự `(tenant_id, id)` (chặt hơn mức PostgreSQL cần) → giữ, đúng chữ của kế hoạch.

Lần 2 (sau hai sửa trên, soát lại toàn bộ diff đã stage): **ĐẠT**, không phát hiện phải sửa. Ghi chú không bắt
buộc, giữ nguyên: (1) `ExplainAsApp` đếm cả NOTICE của câu ngoài cùng; đã có hai lớp chặn (`log_nested_statements`,
`TestSearchPlans` đòi `Bitmap Index Scan`); (2) câu "app.tenant_id thêm cuối thắng Settings khác" dựa vào thứ tự
`set_config` của `SetTxConfig`, chưa có test riêng; (3) cho rằng mục Ảnh hưởng chưa nêu `migrate_test.go`: đã nêu
(dòng "Test có sẵn bị chạm").

Kết luận: ĐẠT
