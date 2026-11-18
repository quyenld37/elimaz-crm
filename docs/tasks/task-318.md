# Task 318 — Migration chạy được khi số đến lệch thứ tự (M0-18c)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md` (mục "Task 318 (M0-18c)", ngay trước
Task 19). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. `db.Migrate` đổi hành vi (chạy cả
  migration số nhỏ hơn số lớn nhất đã áp dụng). Chỗ gọi: `cmd/crm/main.go:282` (vai trò migrate) và
  `internal/testkit/pg.go:35` (dựng database mẫu cho test); `db.MigrateFS` (chỉ cho test, `export_test.go`).
- Gói phụ thuộc ngược (go list): mọi gói nhập `internal/platform/db` (cmd/crm, httpapi, identity, commands, files,
  idempotency, metrics, queue, schedules, tenancy, webhooks, testkit). Chúng chỉ thấy khác khi database đã có số lớn
  hơn số đến sau; database mẫu của test dựng mới từ đầu nên kết quả không đổi.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không thêm câu sqlc, bảng hay migration. Bảng `platform.goose_db_version` và
  `platform.migration_checksums` giữ nguyên; thứ tự dòng trong `goose_db_version` có thể khác thứ tự số (goose ghi theo
  thứ tự chạy).
- Test sẽ chạy: cả bộ `internal/platform/db` (vùng được giao chạy hết), cộng gói nhập db mà hook chọn;
  `go test -race ./internal/platform/db/...`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `db` (Migrate,
  LintContract); `grep -rn goose` trong mã: chỉ `internal/platform/db/migrate.go` dựng provider goose.
- Dùng lại / viết mới vì: dùng lại nguyên `migrate()` (khóa advisory, `verifyChecksums`, `lintPending`,
  `recordChecksums`); chỉ thêm tùy chọn `goose.WithAllowOutofOrder(true)` có sẵn trong goose v3.30.1 (ADR-44). Test
  dùng lại `realPlus`, `regclassExists`, `testkit.CreateDatabase`. Không viết hàm mới.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1–5.10: không áp dụng: không thêm dữ liệu, bảng, đường ghi, gọi ra ngoài hay dữ liệu nghiệp vụ; chỉ đổi cách
  goose chọn tệp để chạy.
- 5.11: không thêm thư viện; `WithAllowOutofOrder` là tùy chọn của goose đã có trong ADR-44.
- 5.12: giữ đúng cấu trúc `migrate.go`, test ở gói `db_test` trên PostgreSQL thật như các test migrate hiện có.

Mục 6 (đúng khi chạy đồng thời):

- 6.1 (hai lần chạy cùng lúc): khóa `pg_advisory_lock(727001)` vẫn bao cả lần chạy, kể cả khi đọc danh sách tệp thiếu;
  test hai `Migrate` song song có tệp đến lệch số: mỗi tệp áp dụng đúng một lần.
- 6.2: không áp dụng (không có yêu cầu HTTP).
- 6.3: không áp dụng: mỗi tệp chạy trong transaction riêng của goose như cũ.
- 6.4–6.7, 6.10, 6.13: không áp dụng: không có đường yêu cầu, việc nền, tiền hay quyền.
- 6.8 (chết giữa chừng): tệp lệch số lỗi thì transaction của nó tự lùi, các tệp trước đã ghi checksum (như cũ);
  test: tệp lệch số có lỗi SQL không để lại bảng.
- 6.9: giới hạn ADR-26 của kết nối goose giữ nguyên.
- 6.11 (quy luật luôn đúng): checksum của tệp đã áp dụng vẫn kiểm trước khi chạy (không nới), luật `-- contract:` vẫn
  kiểm trên mọi tệp sắp chạy, kể cả tệp lệch số (`Status` của goose báo tệp thiếu là `pending`); mỗi điều có test.
- 6.12: không áp dụng: lỗi chỉ nêu tên tệp.

Rủi ro đã cân: tệp số nhỏ đến sau chạy **sau** tệp số lớn đã có. Tệp phụ thuộc vào bảng của tệp số lớn hơn vẫn chạy
được; tệp mà tệp số lớn hơn phụ thuộc thì đã phải có sẵn khi tệp đó chạy, nên không có trường hợp mới bị vỡ. Luật
"trong một mốc gộp theo thứ tự số khi được" giữ nguyên, ghi vào Global Constraints và RUNBOOK.

## Bằng chứng

Lệnh chung: `go test -race -count=1 -v -run 'OutOfOrder|ChangedApplied|ContractWithout|Concurrent' ./internal/platform/db/`
(PostgreSQL thật, dự án compose `crm-t318`, cổng 5818). Trước khi sửa `migrate.go`, ba test đánh dấu (đỏ) trả
`detected 1 missing (out-of-order) migration lower than database version (9013): version 9012`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đã chạy 9011, 9013 → thêm 9012 → `Migrate` chạy đúng 9012, không lỗi; lần sau không chạy gì; checksum của tệp lệch số được ghi (sửa nó thì bị từ chối) | `TestMigrateAppliesOutOfOrderFile` (đỏ trước) | lệnh chung | `--- PASS: TestMigrateAppliesOutOfOrderFile (0.44s)` |
| Tệp đã áp dụng bị sửa, kèm một tệp lệch số → vẫn từ chối (`ErrMigration`, nêu tên tệp, "checksum"), tệp lệch số không chạy | `TestMigrateOutOfOrderStillRejectsChangedAppliedFile`, `TestMigrateRejectsChangedAppliedFile` | lệnh chung | `--- PASS` cả hai |
| Tệp lệch số có `DROP` thiếu `-- contract:` → từ chối, nêu tên tệp, DROP không chạy | `TestMigrateOutOfOrderRejectsContractWithoutMarker`, `TestMigrateRejectsContractWithoutMarker` | lệnh chung | `--- PASS` cả hai |
| Tệp lệch số có lỗi SQL → trả lỗi nêu tên tệp, không để lại bảng | `TestMigrateOutOfOrderFailedFileLeavesNoTable` (đỏ trước) | lệnh chung | `--- PASS: TestMigrateOutOfOrderFailedFileLeavesNoTable (0.37s)` |
| Hai `Migrate` song song có tệp lệch số → khóa giữ, tệp áp dụng đúng một lần | `TestMigrateOutOfOrderConcurrentRunsApplyOnce` (đỏ trước), `TestMigrateConcurrentRunsApplyOnce` | lệnh chung | `--- PASS` cả hai |
| Cả bộ test db (vùng được giao) | mọi test `internal/platform/db` | `go test -race -count=1 ./internal/platform/db/...` | `ok elimaz.com.vn/crm/internal/platform/db 12.506s` |
| Gói nhập db dựng database qua `Migrate` không đổi kết quả | tenancy, queue, identity | `go test -race -count=1 ./internal/platform/tenancy/... ./internal/platform/queue/... ./internal/modules/identity/...` | `ok` cả ba (36.8s, 126.2s, 132.2s) |
| Lint | — | `golangci-lint run ./internal/platform/db/...` | `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Step 1 ghi "database đã chạy 0001, 0003 → thêm 0002; checksum của 0001 bị sửa". Test dùng tệp thử số 9011, 9012,
  9013 cộng toàn bộ migration thật (cùng cách các test migrate sẵn có, `realPlus`), vì số 0001–0003 là migration thật
  đã có; ý nghĩa không đổi: hai tệp đã chạy, tệp số ở giữa đến sau, tệp đã chạy bị sửa.
- Thêm test tệp lệch số có lỗi SQL (không có trong Step 1) để chắc tệp lệch số cũng chạy trong transaction riêng.

## Quyết định thay chủ shop

- Sửa luôn chữ trong kế hoạch M0 (Global Constraints dòng dải số migration, dòng A30) và M2-33 (mục "Migration lệch
  thứ tự" và Step 0) cho khớp việc đã làm. Phương án khác: chỉ thêm ghi chú, giữ câu cũ "M0 giữ mặc định" — bỏ vì
  hai câu trái nhau sẽ làm task sau hiểu sai. Dễ đảo ngược (tệp kế hoạch, ngoài repo CRM).
- Luật cho người viết migration ("tệp số nhỏ có thể chạy sau tệp số lớn, không giả định tệp lớn chưa chạy") ghi ở
  `docs/RUNBOOK.md`. Định thêm cả vào tệp luật migration trong thư mục cấu hình quy trình, nhưng hook chặn (chỉ chủ
  shop đổi); để ở "Việc phát hiện thêm".

## Việc phát hiện thêm

- Đề xuất chủ shop thêm luật 7 vào luật migration (`rules/migrations.md` trong thư mục cấu hình agent): "Tệp số nhỏ
  có thể chạy sau tệp số lớn hơn đã áp dụng (`WithAllowOutofOrder`, Task 318): không giả định tệp số lớn hơn chưa
  chạy; vẫn gộp theo thứ tự số trong mốc khi được."
- Các kế hoạch M3, M4, M6, P2 vẫn ghi `WithAllowOutofOrder` "của task chung M2-33" (giả định G-…). Hành vi họ cần
  đã có sẵn từ Task 318; câu chỉ lệch nguồn, không lệch hành vi. Để agent của từng kế hoạch đối chiếu (Task M3-00).

## Reviewer

Kết luận: ĐẠT (lần 1, trên bản đã gộp main 3cfbcfc). Không có phát hiện phải sửa. Reviewer đã đọc mã goose v3.30.1:
tệp lệch số chạy trước theo số tăng dần (`internal/gooseutil/resolve.go`), `Provider.Status` báo tệp chưa có dòng là
`pending` nên `lintPending` kiểm cả tệp lệch số; `verifyChecksums` và khóa advisory giữ nguyên. Ghi chú không bắt
buộc: `TestMigrateOutOfOrderRejectsContractWithoutMarker` là test chặn tái phát (qua cả trước khi sửa), không phải
test đỏ trước — đúng như bảng bằng chứng (chỉ ba test ghi "đỏ trước").
