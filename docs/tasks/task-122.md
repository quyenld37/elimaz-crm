# Task 122 — M3-31: Nhập bảng kê COD

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 122, M3-31; ca TI04, phần đọc tệp của TI02). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `internal/platform/xlsx`: **thêm** `ReadRows`, `RawRow` (commit cấu trúc trước: tách phần mở sổ của `Read` thành hàm
    nội bộ `openFirstSheet`, `Read` giữ chữ ký và hành vi; 3 gói gọi `Read`: `catalog`, `catalog/internal/importer`,
    `customer` — không đổi gì ở đó).
  - `internal/modules/cash` (`area_statements.go`): thêm `Service.PrepareStatementImport`, `Service.ImportStatement`,
    `Service.GetStatement`, `Service.ListStatements`, `CheckStatementFormats`, kiểu `StatementFile`,
    `ImportStatementInput`, `StatementDetail`, bí danh `Statement`, `StatementLine`, `MatchTotal`, `StatementParseError`,
    `StatementListFilter`, `DuplicateStatementError`, sentinel `ErrStatementDuplicate`, `ErrCarrierAccountNotFound`,
    `ErrUnknownStatementFormat`, `ErrStatementNotFound`, `ErrStatementFileNotReady`, `ErrInvalidInput`,
    `ErrFilesNotWired`. `statementsHandlers()` trả lệnh `command.cash.statement_parse` (1 chỗ gọi: `wire.go`, không sửa).
  - `internal/httpapi`: `Deps` thêm `Cash *cash.Service`; `errors.go` thêm `codeStatementDuplicate`, trường
    `errorBody.StatementID`, nhánh `DuplicateStatementError` ở `detailedError`, bảng `cashError`; test
    `m3_routes_test.go` (`newM3Env` nối `Files`, `Cash` vào server để route M3 dùng chung).
  - `cmd/crm`: `m3Deps` thêm `Files`; `wireM3` nối `Files` vào `cash.Deps`, gọi `CheckStatementFormats` (định dạng trùng
    khóa → lỗi khởi động); `newModules` thêm tham số `fs files.FileStore` (3 chỗ gọi: `runWorker`, nhánh api, và
    `main_test.go`, đã sửa cả ba); `runWorker` dựng kho tệp; api nối `Deps.Cash = mods.M3.Cash.Service`.
    `wire_m3_test.go`: danh sách lệnh mong đợi thêm `command.cash.statement_parse` ở cả ba ca.
  - Tài liệu: `docs/danh-muc-dung-chung.md` (`make catalog`), `docs/views.md` (mốc đọc `carrier_accounts_v` thêm M3).
- Gói phụ thuộc ngược (go list): `cash` ← `cmd/crm` (và từ task này `internal/httpapi`); `xlsx` ← `catalog`,
  `catalog/internal/importer`, `customer`. Gói mới `cash/internal/{formats,statement}` chỉ `cash` nhập.
- Operation OpenAPI bị chạm, oasdiff: `importCodStatement`, `listCodStatements`, `getCodStatement` thôi trả 501 (xóa
  3 dòng khỏi `tests/arch/testdata/m3_pending.txt`). Hợp đồng không đổi (`openapi.yaml` không sửa) → oasdiff không
  chênh. Mã lỗi dùng mã đã khai: `statement_duplicate` (kèm `statementId`, đã có trong `Error`), `not_found`,
  `invalid_input`, `file_not_ready`, `file_too_large`.
- Chỗ dùng trên màn hình: chưa có (màn Đối soát COD là task màn hình M3 sau; mockup Task 131). Không sửa `console/`.
- Câu sqlc, bảng, pha migration: tệp mới `queries/cash/statements.sql` (đọc `fulfillment.carrier_accounts_v`; chèn,
  đọc, đổi trạng thái `cash.cod_statements`; chèn lô, đọc `cash.cod_statement_lines`). Không migration mới (bảng, quyền
  cột đã có ở 0303). Chỉ dùng cột đã `GRANT UPDATE` (`status, parse_error, line_count, period_from, period_to,
  updated_at, version`).
- Test sẽ chạy: `go test -race ./internal/platform/xlsx/... ./internal/modules/cash/... ./internal/httpapi/
  ./cmd/crm/ ./tests/arch/`; fuzz `FuzzParseAmount`, `FuzzParseRow` mỗi cái 60 s. Vùng rủi ro cao chạm:
  `internal/modules/cash` → cả bộ `cash`. Không sửa `internal/platform/files` (chỉ gọi `files.ReadContent`).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `files.ReadContent` (đọc tệp ngoài transaction có
  trần byte, kiểm mục đích, trạng thái — đúng việc), `xlsx.Read` (đòi tiêu đề ở dòng 1, bỏ ô công thức, so tên cột
  nguyên văn — không hợp: định dạng hãng có `HeaderRow`, nhiều tên chấp nhận cho một cột, kế hoạch đòi đọc giá trị đã
  tính của ô công thức), `textnorm.Fold` (bỏ dấu, chữ thường — dùng chuẩn hóa tên cột tiêu đề), `encoding/csv`,
  `commands.CommandKind`/`Looker`/`DoneHook`/`queue.TerminalHook`, `queue.JobQueue.Enqueue`, `tenancy.WithTenant`,
  `httpapi.refusableWrite`, `handler.audit`, `staffNames`, `optPtr`, `db.NullUUID`, `testkit.Staff.CountIn`. Mẫu lệnh
  đọc tệp + ghi lô: `customer.Importer` (Task M2-13).
- Dùng lại / viết mới vì:
  - Viết mới `xlsx.ReadRows` (kế hoạch cho phép, hàm mới, không đổi `Read`); phần mở sổ có giới hạn dùng chung với
    `Read` qua `openFirstSheet` (tách ở commit cấu trúc riêng), không chép.
  - `ParseAmount` viết mới: không có hàm đọc số tiền có dấu phân cách nghìn nào trong danh mục (catalog đọc giá trong
    `importer` là luật riêng của module catalog, không dùng chung theo luật "logic nghiệp vụ ở lại module").
  - jscpd (lần chạy cổng kiểm đầu) báo 8 đoạn trùng mới: đổi sang `db.NullUUID`, `CountIn`, gộp ba hàm con trỏ thành
    một hàm generic, dùng chung bảng ba ca lỗi của kế hoạch giữa test đọc tệp và test lệnh (`planFormatCases`), test
    tải tệp đi đúng đường PUT ký sẵn như trình duyệt; còn 0 đoạn mới.
  - Ghi lô dòng: `INSERT … SELECT unnest(...) ON CONFLICT DO NOTHING` (câu sqlc), không dùng `CopyFrom` (không có
    `ON CONFLICT`, chạy lại không an toàn).

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `cash.cod_statements`, `cash.cod_statement_lines` thuộc cash (04 mục 3); tệp gốc thuộc kho tệp.
- 5.2 Không ghi bảng module khác: chỉ ghi hai bảng của cash và `platform.jobs` qua `queue.Enqueue`.
- 5.3 Đọc module khác qua view: khóa hãng của tài khoản đọc `fulfillment.carrier_accounts_v`; tệp qua `files.ReadContent`.
- 5.4 Nguyên tử: chèn bảng kê + ghi lệnh đọc tệp cùng transaction (`WriteTx`).
- 5.5 Không nguyên tử sau commit: đọc, phân tích tệp là lệnh `command.cash.statement_parse` chạy ngoài transaction.
- 5.6 Gọi ra ngoài: chỉ kho tệp (đọc), lệnh có Looker (bảng kê đã `parsed` → done, ngược lại not_sent); không gửi gì
  ra bên ngoài nên chạy lại an toàn (ghi dòng có điều kiện).
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; bảng đã có RLS (0303).
- 5.8 Module không nhập `net/http`, ogen; handler mỏng ở `handlers_cod_statements.go`.
- 5.9 `importCodStatement` có `IdempotencyKey` và `x-permission` (đã có trong hợp đồng); từ chối (trùng, tài khoản
  không có, định dạng lạ) không lưu vào khóa chống trùng (`refusableWrite`).
- 5.10 Không dùng Valkey.
- 5.11 Không thêm thư viện (excelize đã có, chỉ gói `xlsx` nhập).
- 5.12 Theo mẫu module `catalog`/`inventory` (gói con `internal/`, sentinel ở gói gốc qua bí danh), mẫu lệnh `customer.Importer`.

Mục 6:
- 6.1 Hai yêu cầu nhập cùng tệp cùng lúc: ràng buộc `cod_statements_file_key (tenant_id, carrier_key, file_sha256)` +
  `ON CONFLICT DO NOTHING` rồi đọc mã cũ → một bên 202, bên kia 409 `statement_duplicate` (test song song).
- 6.2 Gửi lại cùng khóa: `WriteTx` lưu kết quả 202; khóa tự nhiên là băm nội dung tệp (đổi tên tệp vẫn 409).
- 6.3 Thứ tự khóa: chỉ chèn `cod_statements` rồi `jobs` (ADR-46 nhóm 9 rồi jobs cuối); ghi lô chỉ chạm
  `cod_statement_lines`; `OnDone` đổi `cod_statements` trong tx của Runner trước câu đổi `jobs`.
- 6.4 Lỗi tạm trên đường yêu cầu: `WriteTx` (`RetryTx`) có sẵn; hàm trong tx gán lại mọi biến kết quả.
- 6.5 READ COMMITTED: ON CONFLICT chờ transaction kia, câu SELECT sau đó thấy dòng đã commit.
- 6.6 Việc nền lỗi: lỗi tạm (database, kho) → `not_sent` tự chạy lại (8 lượt) → hết lượt `dead` → `OnTerminal`
  đặt `parse_failed` `read_failed`; lỗi định dạng → `rejected` (dead ngay) với `parse_error` đã ghi.
- 6.7 Không áp dụng: không gọi bên ngoài có 429.
- 6.8 Worker chết giữa lô: giữ chỗ hết hạn → `in_doubt` → Looker (`not_sent` vì chưa `parsed`) → chạy lại; dòng chèn
  `ON CONFLICT (tenant_id, statement_id, line_no) DO NOTHING` nên đúng số dòng (test 2.500 dòng, chết sau lô 1).
- 6.9 Hạn lệnh 25 s; tệp ≤ 10 MiB, ≤ 50.000 dòng, lô 1.000 dòng một transaction.
- 6.10 Tiền `int64` đồng; `ParseAmount` từ chối số lẻ khác `.0`, vượt 2^53; âm chỉ ở phí khác, thực nhận. Task này
  không ghi sổ.
- 6.11 Rapid/fuzz: `FuzzParseAmount`, `FuzzParseRow` (không sập; số đọc được luôn trong giới hạn, ghi lại ra chuỗi
  đọc lại đúng số); test quy luật `ParseAmount` bằng rapid.
- 6.12 Dữ liệu cá nhân: tệp là dữ liệu không tin được (04 mục 13); chỉ lưu cột của `cod_statement_lines` (không tên,
  SĐT, địa chỉ); lỗi chỉ có mã, cột, số dòng, không giá trị ô; không log nội dung (test cột `ten_nguoi_nhan`,
  `so_dien_thoai` không vào bảng nào, không vào log, không vào `last_error`).
- 6.13 Quyền: `cash.statements.import` (nhập), `cash.cod.read` (đọc) do guard kiểm theo hợp đồng; tài khoản hãng của
  tenant khác → 404.

## Quyết định thay chủ shop

1. **Băm tệp tự tính, không lấy `platform.files.content_sha256`.** Kế hoạch ghi "lấy từ `content_sha256`", nhưng cột
   này chỉ được ghi cho ảnh (luồng xử lý ảnh); tệp `cod_statement` dừng ở `uploaded` với `content_sha256` rỗng. Chọn:
   lớp HTTP đọc tệp ngoài transaction (`files.ReadContent`, ≤ 10 MiB) và băm SHA-256 nội dung trước `WriteTx`. Đúng ý
   TI04 (băm nội dung, đổi tên vẫn trùng). Phương án khác: sửa kho tệp ghi băm cho mọi tệp lúc `completeUpload` (vùng
   rủi ro cao `platform/files`, ngoài phạm vi task).
2. **Ô công thức trong .xlsx: đọc giá trị Excel đã tính lúc lưu, không chạy công thức.** Kế hoạch có hai câu trái nhau
   ("ô công thức → `formula_not_allowed`" và test "`=600000+30000` có giá trị đã tính 630.000 → đọc 630.000"). Chọn
   theo test (tệp của hãng hay có cột tính sẵn); ô công thức **không có** giá trị đã lưu → `parse_failed`
   `formula_not_allowed`. An toàn: CRM không chạy công thức, không xuất lại ô đó. Đảo lại dễ: một nhánh trong `parse.go`.
3. **Dòng có số tiền mà trống mã vận đơn → `parse_failed` `missing_value` (cột, dòng)**, không bỏ qua ngầm (Q-16
   "không đoán"); dòng trống hoàn toàn bị bỏ. Ô tiền trống ở cột bắt buộc → `missing_value`; ở cột tùy chọn → 0.
4. **Số dòng lưu (`line_no`) và số dòng trong lỗi là số dòng trong tệp** (dòng Excel; dòng bắt đầu bản ghi CSV), để
   kế toán mở tệp tìm đúng chỗ.
5. **Định dạng mặc định của hãng** (`format` rỗng): định dạng đầu tiên của adapter hãng đó (thứ tự adapter khai), không
   có thì `elimaz_v1`. `format` chỉ định phải là `elimaz_v1` hoặc định dạng của đúng hãng của tài khoản, không thì 422
   `invalid_input`.
6. **`ParseKind` là hàm**, không phải biến cấp gói (luật `gochecknoglobals` của repo); cùng tên lệnh, hạn 25 s.
7. **`elimaz_v1` bắt buộc 4 cột** `ma_van_don, tien_cod, phi_van_chuyen, thuc_nhan`; `phi_khac`, `ngay_chuyen` tùy chọn.
   Ngày nhận `dd/mm/yyyy`, `d/m/yyyy`, `yyyy-mm-dd` và số ngày kiểu Excel (ô ngày của .xlsx). Kỳ bảng kê
   (`period_from/to`) = ngày chuyển nhỏ nhất/lớn nhất của các dòng.
8. **Lỗi thêm ngoài danh sách kế hoạch** (cùng dạng `{"code", "column"?, "line"?}`): `too_large`, `too_many_columns`,
   `cell_too_long`, `duplicate_column`, `bad_date`, `bad_file`, `bad_encoding`, `no_lines`, `missing_value`,
   `file_unavailable`, `unknown_format`, `read_failed`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): `Read` giữ hành vi sau khi tách `openFirstSheet` | test `Read*` của `xlsx` (`TestReadSizeLimits`, `TestReadZipBomb`, `TestReadZipBombLyingHeader`, `TestReadNotXlsx`, `TestReadFormulaKinds`, `TestReadTooManyRows`…) + test `Import*` của `customer` (đọc qua `Read`); nhánh excelize panic không có test, xác nhận bằng đọc mã (defer `recover` đăng ký trước `Close`, `Read` trả `nil, nil, err`) | `go test -race -count=1 ./internal/platform/xlsx/ ./internal/modules/customer/ -run 'Import\|Read'` | `ok xlsx 12.577s`, `ok customer 16.834s`; `golangci-lint run ./internal/platform/xlsx/...` → `0 issues` |
| Commit 2: TI02 phần đọc tệp — elimaz_v1.csv 3 dòng → parsed, 3 dòng đúng số nguyên (630.000/32.000/598.000; 0/25.000/−25.000; 450000/30000/420000) | `TestImport_ThreeLinesParsed`, `TestParse_ElimazThreeLines` | `go test -race -count=1 -v ./internal/modules/cash/internal/statement/` | `--- PASS: TestImport_ThreeLinesParsed`, `--- PASS: TestParse_ElimazThreeLines`; `ok … statement` |
| TI04: nhập lại cùng tệp → 409 `statement_duplicate` kèm mã cũ; đổi tên tệp vẫn trùng; hai lần song song → 1 bảng kê | `TestImport_DuplicateRejected`, `TestCodStatementsAPI` (409 + `statementId`) | như trên; `go test -race -count=1 ./internal/httpapi/ -run TestCodStatementsAPI` | `--- PASS: TestImport_DuplicateRejected`; `ok …/internal/httpapi` |
| .xlsx ô `=600000+30000` giá trị đã tính 630.000 → 630.000, không chạy công thức; công thức không giá trị → `formula_not_allowed` | `TestImport_XlsxFormulaUsesCachedValue`, `TestParse_ElimazThreeLines`, `TestParse_FormatErrors/công_thức_không_giá_trị`, `TestReadRowsFormulas` | như trên; `go test -race ./internal/platform/xlsx/` | PASS; `ok …/xlsx` |
| Thiếu `tien_cod` → `parse_failed` `missing_column`; `12,5` dòng 17 → `bad_amount` line 17; 50.001 dòng → `too_many_rows` (50.000 được) | `TestImport_FormatErrorsFailStatement` (3 ca + quá MaxBytes của lệnh), `TestParse_FormatErrors` (17 ca + 50.000 dòng) | như trên | `--- PASS` từng ca con |
| Tệp 11 MiB → từ chối trước khi đọc hết | `TestImport_TooLargeRejectedBeforeRead` (`files.ErrTooLarge`, cấp thêm < 1 MiB), `TestParse_FormatErrors/11_MiB` | như trên | PASS |
| Zip bomb ~2 MiB nén / 2 GiB giải nén → `parse_failed` < 5 s, bộ nhớ < 100 MiB | `TestImport_ZipBombFailsFast` | như trên | `bom 2074 KB: parse_failed sau 18.8ms, cấp thêm 9 MB`; PASS |
| Cột `ten_nguoi_nhan`, `so_dien_thoai` (và `dia_chi`) không vào bảng, lệnh, log | `TestImport_PersonalDataNotStored` | như trên | PASS |
| Worker chết giữa lô 2 của tệp 2.500 dòng → chạy lại → đúng 2.500 dòng | `TestImport_WorkerDiesMidBatchResumes` (in_doubt → `ResolveInDoubt` → not_sent → chạy lại) | như trên | PASS |
| Hết lượt vì lỗi tạm → `parse_failed` `read_failed` (OnTerminal); Lookup not_sent/done | `TestImport_RetriesExhaustedAndLookup` | như trên | PASS |
| Từ chối: tài khoản lạ/tenant khác (404), định dạng lạ (422), tệp chưa tải xong hay sai mục đích (409 `file_not_ready`), tệp không có (404) | `TestImport_Refusals`, `TestCodStatementsAPI` (404, 422, 409 `file_not_ready`, 404, 403 Kho) | như trên | PASS |
| `getCodStatement` phân trang keyset theo `line_no`, tổng theo `match_status`, lọc; `listCodStatements` | `TestImport_ThreeLinesParsed`, `TestListStatements`, `TestCodStatementsAPI` (nextCursor "3", con trỏ sai 400) | như trên | PASS |
| `ParseAmount` nhận/từ chối các dạng kế hoạch, vượt 2^53; quy luật đọc lại đúng số (rapid) | `TestParseAmount`, `TestParseAmount_RoundTrip` | như trên | PASS |
| `FuzzParseAmount` 60 s, `FuzzParseRow` 60 s không sập | `FuzzParseAmount`, `FuzzParseRow` | `go test -run XXX -fuzz '^FuzzParseAmount$' -fuzztime 60s ./internal/modules/cash/internal/statement/` (và `FuzzParseRow`) | `elapsed: 1m0s, execs: 2850420 … PASS`; `elapsed: 1m1s, execs: 117519 … PASS` |
| Sổ định dạng: elimaz_v1, định dạng hãng, trùng Key → lỗi khởi động, mặc định của hãng | `TestRegistry`, `TestRegistryFor`, `TestColumnName`, `TestParse_CarrierHeaderRow`, `TestWireM3_KeepsM1Parts` | `go test -race ./internal/modules/cash/internal/formats/ ./cmd/crm/` | `ok …/formats`, `ok …/cmd/crm` |
| `xlsx.ReadRows`: tiêu đề không ở dòng 1, công thức, giới hạn | `TestReadRowsHeaderNotFirst`, `TestReadRowsFormulas`, `TestReadRowsLimits` | `go test -race ./internal/platform/xlsx/` | `ok …/xlsx` |
| Cổng kiểm của task (cổng tĩnh + test gói đổi, phụ thuộc, vùng rủi ro cao) | — | `ci-task.sh` | lần 1: mọi test `ok` (cash 77 s, files 109 s, httpapi, arch…), chỉ `jscpd` ĐỎ → đã sửa (xem Tái dùng); lần 2: `CI-TASK XANH (so với điểm tách nhánh fc3b605…)`, mọi bước xanh (test phần đổi 87 s) |

## Test bị xóa hoặc tắt

(không có). `TestWireM3_KeepsM1Parts` được **thêm** tên lệnh mong đợi (không nới); 3 dòng của
`tests/arch/testdata/m3_pending.txt` bị xóa vì ba operation đã làm (đúng luật của tệp đó).

## Khác kế hoạch

- `file_sha256` tự băm (Quyết định 1); ô công thức đọc giá trị đã tính (Quyết định 2); `ParseKind()` là hàm (Quyết định 6).
- Sửa `cmd/crm/wire_m3.go`, `modules.go`, `main.go` (kế hoạch ghi "task sau không sửa tệp này") theo giao việc của
  điều phối viên: task này nối kho tệp cho bảng kê; chỉ thêm trường `Files`, tham số `fs` và một dòng kiểm định dạng.
- `getCodStatement`, `listCodStatements` làm luôn ở task này (hợp đồng ghi Task M3-31); test `.xlsx` dựng tệp trong
  test (`book`), không thêm tệp `.xlsx` vào `testdata/statements` (chỉ hai tệp `.csv`).
- `OnDone` chưa ghi việc khớp: lệnh `cash.statement.match` do Task 123 (M3-32) định nghĩa; Task 123 thêm dòng xếp
  việc trong `OnDone` (`parse_command.go`).

## Việc phát hiện thêm

- Khóa chống trùng `(tenant_id, carrier_key, file_sha256)` (0303, đúng kế hoạch) làm bảng kê đã `parse_failed` (kể cả vì
  lỗi tạm `read_failed`, hay sai định dạng rồi kế toán sửa chọn định dạng khác) không nhập lại được cùng tệp: luôn 409
  trỏ về bảng kê lỗi. Cần chủ shop quyết: cho nhập lại khi bảng kê cũ `parse_failed` (ví dụ đọc lại bảng kê đó, hay
  khóa unique bỏ qua dòng `parse_failed` bằng migration mới) — để Task 123 hoặc một task sau.

- Hạn giữ 400 ngày của tệp bảng kê (kế hoạch Task 95: "đặt bằng `ExpiresAt` của tệp khi Task 31 nhận bảng kê"): kho
  tệp M1 không có hàm đặt `expires_at` cho tệp đã tải lên, và `expires_at` hiện nghĩa là "tệp tạm hết hạn, không tải
  được" (`DownloadURL` → `ErrExpired`). Không đặt ở task này; cần một task của `platform/files` (vùng rủi ro cao).

- `TestImager_HangKilledWithinDeadline` (`internal/imager/pipeline_test.go:571`) đỏ một lần khi máy tải nặng (chờ
  script giả chạy chỉ 1 giây). Không sửa ở đây (vùng rủi ro cao, ngoài phạm vi); để task của imager chờ theo sự kiện.

## Reviewer

- Commit 1 (cấu trúc, 613fbaa): lần 1 CẦN SỬA (dòng bằng chứng ghi "cả bộ", "panic excelize" không khớp lệnh đã chạy;
  không đổi mã) → sửa dòng → lần 2 **ĐẠT**.
- Commit 2 (hành vi): **ĐẠT** lần đầu, không phát hiện phải sửa. Reviewer xác nhận: Execute/Lookup/OnDone/OnTerminal
  đúng khi chạy lại sau in_doubt (OnTerminal chỉ ghi `read_failed` khi dead/expired; câu đổi trạng thái có điều kiện
  `imported/parsing`); chống trùng ON CONFLICT + đọc mã cũ, có ca song song; giới hạn đọc tệp; không dữ liệu cá nhân ở
  bảng/lệnh/log/audit; UPDATE chỉ chạm cột đã GRANT (0303); hai quyết định thay chủ shop khớp mã. Hai ghi chú không bắt
  buộc: (1) ghi rõ tệp sai mục đích trả 409 `file_not_ready` trong bảng bằng chứng — đã sửa dòng; (2) bảng kê
  `parse_failed` vì lỗi tạm không nhập lại được cùng tệp — đã ghi vào "Việc phát hiện thêm".
- Lượt gộp main (sau khi Task 99, 100 gộp): xung đột ở `cmd/crm/main.go` (nối `Deps`) và `internal/httpapi/server.go`
  (import, trường `Deps`): giữ cả hai bên — `Cash: mods.M3.Cash.Service` và `Fulfillment: mods.M3.Fulfillment.Service`,
  cả hai import `cash`, `fulfillment`; `gofmt`. Không đổi mã khác. Kiểm: `go build ./...`, `go vet ./...`,
  `go test -race ./internal/httpapi/... ./cmd/crm/... ./tests/arch/...` (ok cả ba), cổng `ci-task.sh`: lần 1 ĐỎ chỉ ở
  `TestImager_HangKilledWithinDeadline` ("script giả chưa chạy" sau 1 giây, máy tải 14; `internal/imager` không đổi ở
  nhánh hay lượt gộp), chạy lẻ `-count=3` ok; lần 2 `CI-TASK XANH`. Test chờ 1 giây dễ chập chờn khi máy tải nặng:
  ghi ở "Việc phát hiện thêm" cho task của imager.
- Commit gộp: **ĐẠT** lần đầu. Reviewer đối chiếu hai chiều (`git diff main --cached` chỉ còn phần của Task 122;
  `git diff HEAD --cached` chỉ còn phần main mang vào): `Deps` có cả `Cash` và `Fulfillment`, cả hai đều có handler dùng;
  `errors.go` giữ `fulfillmentError` của main và `cashError` của task này; `m3_pending.txt` bỏ đúng dòng của cả hai bên;
  `views.md` đủ dòng của Task 99, 100, 122.

Kết luận: ĐẠT
