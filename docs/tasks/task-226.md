# Task 226 — Schema reporting, ba bảng, chỉ mục còn thiếu, mục đích tệp

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 226 = M6-03). Loại việc (quy trình mục 3): A

Điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 sáng 27/11 và cho bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ
staging); điều kiện "M1–M5 đóng" của M6-00 coi như được miễn. Đối chiếu giả định với mã thật: `docs/m6/doi-chieu-gia-dinh.md`
(Task 223), làm theo phần "Bổ sung Task 223" của kế hoạch (G5, G7, G16).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn. Thêm mới ở gói mới
  `internal/modules/reporting`: `PurposeReportExport`, `PurposeCustomerExport`, `XLSXContentType`, `MaxExportBytes`,
  `FilePurposes()`, `RegisterFilePurposes()` (0 chỗ tham chiếu trước task; sau task: `cmd/crm/main.go` và ba test so cả
  bảng mục đích tệp).
- `cmd/crm/main.go`: các lời gọi `*.RegisterFilePurposes()` dời vào hàm mới `registerFilePurposes()` (không xuất ra
  ngoài; gọi một chỗ lúc khởi động, trước `files.CheckPurposes`) và thêm `reporting.RegisterFilePurposes()`; test mới
  `cmd/crm/purposes_test.go` đòi sổ khớp bảng (đỏ khi bỏ lời gọi, đã thử).
- Mã sinh: `make gen` thêm ba kiểu `Reporting*` (bảng mới) vào `models.go` của 14 gói `internal/store` (sqlc đọc cả thư
  mục migration cho mọi khối); không sửa tay.
- Gói phụ thuộc ngược (go list): gói mới, chưa ai nhập. Sau task: `cmd/crm`; test `internal/platform/files` (`files_test`),
  `internal/modules/conversation` (`conversation_test`) nhập để giữ tập mục đích khớp bảng.
- Operation OpenAPI bị chạm, oasdiff: không (hợp đồng M6 đã có ở Task 225).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không câu sqlc. Migration **pha mở rộng**, chỉ thêm:
  - `0601_reporting.sql`: schema `reporting`, ba bảng mới `report_runs`, `stock_snapshot_days`, `stock_daily_snapshots`
    (RLS, hai bảng chụp tồn chỉ ghi thêm).
  - `0603_report_indexes.sql`: ba chỉ mục trên bảng của module khác (G16 sau Task 223): `ord.order_status_history
    (tenant_id, to_status, occurred_at)`, `inventory.stock_ledger (tenant_id, created_at)`, `ord.returns (tenant_id,
    received_at)`. Không đổi dữ liệu, không đổi cột.
  - `0604_file_purposes_m6.sql`: hai dòng `platform.file_purposes` (`report_export`, `customer_export`, bucket `private`).
  - Không tạo hay sửa view của module nào (không `0602`, không sửa `docs/views.md`: mọi view M6 đọc đã công bố, cột
    "Mốc đọc" đã ghi M6).
- Test kiến trúc: `tests/arch/boundaries_test.go` (`moduleCalls["reporting"] = {"identity"}`), `tests/arch/sqlusage_test.go`
  (`viewReadExceptions["reporting"]` = 19 view M6 đọc; luật mới: không ghi INSERT/UPDATE/DELETE/MERGE vào view `_v` của
  schema khác), testdata mẫu.
- Test sẽ chạy: `go test -race -run` các test mới của `internal/modules/reporting`; `TestPurposes_GoMatchesTable`
  (`internal/platform/files`), `TestFilePurposes_MatchTable` (`internal/modules/conversation`); `go test ./tests/arch/`.
  Còn lại ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `files`
  (`RegisterPurpose`, `CheckPurposes`, `Register`, `CreateUpload`), `testkit` (`OpenStaff`, `CheckSchemaRulesClean`,
  `CheckOwnerAppendOnly`, `Columns`, `User`), `platform.enable_tenant_rls`; mẫu đăng ký mục đích của `cash/purposes.go`,
  `conversation/filepurposes.go`; mẫu trigger chỉ ghi thêm của `inventory.stock_ledger` (0115); mẫu chỉ mục 0307, 0527.
- Dùng lại / viết mới vì: dùng lại hết các hàm trên. Viết mới chỉ: tệp mục đích của `reporting` (theo đúng mẫu
  `cash.StatementPurpose`/`RegisterFilePurposes`), hàm trigger `reporting.stock_snapshots_append_only()` (mỗi module có
  hàm trigger riêng trong schema của mình, như `inventory.stock_ledger_append_only`; không dùng chung hàm của schema khác
  vì sẽ là phụ thuộc chéo schema). Trong `sqlusage_test` thêm hàm `writeRefs` cạnh `tableRefs` (dùng lại `newSQLTables`,
  `item`).

## Soát thiết kế

- 5.1 Chủ dữ liệu: ba bảng thuộc `reporting` (ADR-20, spec 05 schema reporting), nằm trong schema `reporting`.
- 5.2 Không ghi bảng module khác: `0603` chỉ thêm chỉ mục cho bảng của `ord`, `inventory` (pha mở rộng, không đổi dữ
  liệu); `0604` chỉ thêm dòng danh mục `platform.file_purposes` theo cơ chế M1 (R5). Luật mới trong `sqlusage_test` chặn
  `reporting` (và mọi module) ghi vào view của schema khác.
- 5.3 Đọc qua view: `viewReadExceptions["reporting"]` chỉ liệt kê view `_v` đã công bố; đọc bảng gốc vẫn bị báo.
- 5.4, 5.5, 5.6: không áp dụng (chưa có đường ghi, sự kiện, lệnh ra ngoài; các task sau làm).
- 5.7 Ngữ cảnh shop: ba bảng có `tenant_id`, `enable_tenant_rls`, `UNIQUE (tenant_id, id)` cho bảng có `id`, khóa ngoại
  ghép `(tenant_id, day)`; `CheckSchemaRules` rỗng (test).
- 5.8: gói `reporting` chỉ nhập `internal/platform/files`.
- 5.9: không áp dụng (không có endpoint).
- 5.10: không áp dụng (không Valkey).
- 5.11: không thêm thư viện.
- 5.12: theo mẫu `cash`, `conversation` cho mục đích tệp; test ở gói `_test` trên PostgreSQL thật.
- 6.1 Đồng thời: `report_runs.version` có sẵn cho task sau; hai bảng chụp tồn chỉ ghi thêm, `UNIQUE (tenant_id, day)`
  làm khóa tự nhiên một ngày một lần chụp.
- 6.2 Gửi lại: không áp dụng ở task này (bảng có khóa tự nhiên `(tenant_id, day)` cho việc chụp lặp).
- 6.3 Thứ tự khóa: nhóm 12a đã ghi ở ADR-46 (`report_runs` → `stock_snapshot_days` → `stock_daily_snapshots`), Task 225; đã
  kiểm, khớp tên bảng. Khóa ngoại `stock_daily_snapshots → stock_snapshot_days` chỉ khóa `KEY SHARE` dòng ngày cha khi
  chèn dòng con, đúng thứ tự nhóm (ngày trước, dòng sau).
- 6.4 Lỗi tạm: test `transaction_read_only` → `25006` không thử lại (hàm gọi 1 lần).
- 6.5 READ COMMITTED: không đổi.
- 6.6–6.9: không áp dụng (chưa có việc nền). Chỉ mục tạo không CONCURRENTLY (xem Quyết định thay chủ shop).
- 6.10 Tiền: không có cột tiền. `file_bytes bigint`.
- 6.11 rapid: không áp dụng (chỉ schema, chưa có quy luật mã).
- 6.12 Dữ liệu cá nhân: `params jsonb` chỉ chứa mã, ngày, khóa lọc (luật của task sau); bảng chụp tồn không có dữ liệu
  cá nhân. Test nối `users_v` đòi tenant B không thấy tên người của A.
- 6.13 Quyền: `crm_app` không UPDATE/DELETE/TRUNCATE hai bảng chụp tồn (REVOKE + trigger chặn cả chủ bảng); mục đích
  tệp xuất `Upload = false` nên trình duyệt không tải lên được (`ErrUploadNotAllowed`).

## Quyết định thay chủ shop

- **QĐ-226-1 — `0603` không dùng `CREATE INDEX CONCURRENTLY`/`NO TRANSACTION`** (kế hoạch ghi CONCURRENTLY). Chọn
  `CREATE INDEX IF NOT EXISTS` thường trong transaction của migrate, vì tiền lệ đã chốt của repo (0307, 0328, 0509, 0527):
  CONCURRENTLY chờ mọi phiên đang mở, kể cả lần migrate thứ hai đang chờ khóa cố vấn của `db.Migrate`, nên hai lần
  migrate song song kẹt nhau (`TestMigrateConcurrentRunsApplyOnce`); CRM chưa chạy dữ liệu thật, bảng còn nhỏ. Đảo
  ngược: khi có dữ liệu lớn, task sau tạo chỉ mục bằng tay CONCURRENTLY trước rồi migrate (IF NOT EXISTS bỏ qua).
- **QĐ-226-2 — tên hàm `RegisterFilePurposes()`** thay cho `RegisterPurposes()` của kế hoạch: sáu module đã có (`cash`,
  `fulfillment`, `customer`, `order`, `content`, `conversation`) đều dùng tên này; một tên cho một việc. Kèm `FilePurposes()`
  để test so cả bảng (như `conversation.InboundFilePurposes`). Đảo ngược: đổi tên bằng gopls.
- **QĐ-226-3 — luật "không ghi view của schema khác"** viết chung cho mọi module trong `sqlusage_test` (kế hoạch chỉ
  đòi ca của `reporting`), vì view `_v` công bố chỉ để đọc ở mọi module (ADR-27); mã hiện có không vi phạm (test
  `TestSQLUsageProductCode` xanh).
- **QĐ-226-4 — `report_runs.seller_id`** không có khóa ngoại sang `catalog` (kế hoạch không ghi; module chỉ đọc view, bản
  ghi lịch sử như bảng chụp tồn). `requested_by` cũng không khóa ngoại sang `identity.users` (như kế hoạch).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `CheckSchemaRules` → `[]` sau 0601, 0603, 0604 | `TestReportingSchema_Rules` | `go test -race -count=1 ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 4.341s` |
| A chèn mỗi bảng một dòng; B đọc 0; ngoài ngữ cảnh 0 | `TestReportingSchema_TenantIsolation` | như trên | ok |
| crm_app UPDATE/DELETE/TRUNCATE hai bảng chụp tồn → 42501; chủ bảng → `append-only`; report_runs crm_app sửa, xóa được | `TestReportingSchema_SnapshotsAppendOnly` | như trên | ok |
| CHECK của report_runs (json có file_id, failed không reason, reason khi không failed, customers json/sai report, products json/sai family, giá trị lạ, row_count âm); ngày chưa có `stock_snapshot_days` → 23503; ngày của tenant khác → 23503; một ngày hai lần → 23505; số lượng âm → 23514 | `TestReportingSchema_RunChecks` | như trên | ok |
| `identity.users_v` đúng `id, display_name, status`; không migration 06xx tạo/thay view; B nối report_runs với view không thấy người của A | `TestReportingSchema_UsersView` | như trên | ok |
| `pg_indexes` có ba chỉ mục 0603, hai chỉ mục G16 có sẵn, bốn chỉ mục của reporting | `TestReportingSchema_Indexes` | như trên | ok |
| `transaction_read_only=on`: INSERT report_runs → 25006, hàm chạy 1 lần | `TestReportingSchema_ReadOnlyTxNoRetry` | như trên | ok |
| `file_purposes` có `report_export`, `customer_export` private/standard; sổ Go sau `RegisterFilePurposes` (xlsx, 100 MiB, không Upload) | `TestFilePurposes_ExportRows` | như trên | ok |
| `files.Register` report_export/customer_export → ready, private; khóa lạ → `ErrUnknownPurpose`; chèn thẳng `report_exportx` → 23503; `CreateUpload` → `ErrUploadNotAllowed` | `TestFilePurposes_RegisterAndUpload` | như trên | ok |
| Khởi động: sổ khớp bảng sau `registerFilePurposes()`; đỏ khi bỏ lời gọi reporting | `TestRegisterFilePurposes_MatchTable` | `go test -race -count=1 -run '^TestRegisterFilePurposes_MatchTable$' ./cmd/crm/` | `ok 2.624s`; bỏ tạm lời gọi: `FAIL … customer_export: có trong bảng, chưa RegisterPurpose; report_export: …` |
| Test so cả bảng của M1 và M4 vẫn khớp | `TestPurposes_GoMatchesTable`, `TestFilePurposes_MatchTable` | `go test -race -count=1 -run … ./internal/platform/files/`, `./internal/modules/conversation/` | `ok 4.062s`, `ok 1.933s` |
| sqlusage: reporting đọc `ord.orders_v` → đạt; đọc `ord.orders` → báo tệp; `UPDATE ord.orders_v` → báo; ghi bảng của mình → đạt; mã thật không vi phạm luật ghi view | `TestSQLUsageViewRules` (`queries/reporting/m6_*.sql`), `TestSQLUsageProductCode` | `go test -count=1 ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 212.343s` |
| boundaries: reporting nhập order → (a); nhập identity, platform/files, queries/orderview → đạt | `TestBoundariesTestdata`, `TestBoundariesNotCalledModule` | như trên | ok |
| Cổng tĩnh + test các gói bị ảnh hưởng (bản đã gộp main 70fcb0cf) | ci-task | ci-task.sh lần 2, sau `make gen` | `CI-TASK XANH (từ xa trên blacksmith…)`, run 45943091152; lần 1 đỏ chỉ ở gen-check (chưa `make gen`), test xanh |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `RegisterFilePurposes()` thay `RegisterPurposes()` (QĐ-226-2); `0603` không CONCURRENTLY (QĐ-226-1).
- Không sửa `docs/views.md` và không thêm luật view vào `sqlusage_test` theo nghĩa "G7 chưa có": luật đã có, chỉ thêm
  ngoại lệ `reporting` (G7 sau Task 223).
- `order_items (tenant_id, order_id)` không thêm (G16 sau Task 223: đã có chỉ mục `(tenant_id, seller_id, order_id, …)`).

## Việc phát hiện thêm

- Cho task chụp tồn (M6-10): `stock_snapshot_days` chỉ ghi thêm, `row_count NOT NULL`, khóa ngoại của
  `stock_daily_snapshots` không deferrable → phải biết số dòng trước khi chèn dòng ngày (đếm trong cùng transaction
  rồi chèn ngày, rồi chèn dòng), không chèn ngày trước rồi sửa `row_count` sau.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên `git diff --cached` của bản đã gộp main 70fcb0cf).

- Phát hiện phải sửa: không có.
- Step R: ba bảng có trong ADR-46 nhóm 12a (`docs/adr/0046-thu-tu-khoa.md:39`) đúng thứ tự; migration 0601, 0603, 0604
  trong dải 0600–0699, 0602 bỏ trống.
- Bốn quyết định thay chủ shop được đánh giá an toàn (QĐ-226-1 có tiền lệ 0307/0328/0509/0527 và
  `TestMigrateConcurrentRunsApplyOnce`).
- Ghi chú không bắt buộc đã xử lý: sửa hai con số lệch trong tệp task (19 view, 14 gói store); lưu ý `row_count` của
  ngày chụp ghi vào "Việc phát hiện thêm". TRUNCATE `stock_snapshot_days` bằng crm_app không có ca riêng (REVOKE và
  trigger cùng chặn; ca TRUNCATE của bảng con có trong test).
