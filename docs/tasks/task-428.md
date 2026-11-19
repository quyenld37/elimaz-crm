# Task 428 — Test đỏ oan khi nhiều phiên dùng chung máy (M1-x11)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 428, M1-x11). Loại việc (quy trình mục 3): A

Chỉ sửa test và hàm trợ giúp của test; không đổi mã chạy thật, không nới tiêu chí, không retry, không skip.

## Gốc của từng chỗ đỏ oan

1. `tests/arch` (`TestLintCatchesPlantedMistakes`, `TestLintCleanOnRealCode`, `TestPackagePatternSkipsStrayDirs`):
   thông báo "parallel golangci-lint is running" không đến từ thư mục cache mà từ **khóa tệp**
   `os.TempDir()/golangci-lint.lock` (mã golangci-lint v2.15.0 `pkg/commands/run.go` `acquireFileLock`): mọi lần
   `golangci-lint run` trên máy dùng chung một khóa trong `$TMPDIR`, lần đến sau thử 5 giây rồi bỏ. Hook Stop, ci-task
   và lint của phiên khác chạy cùng lúc → test đỏ.
2. `observability.TestFileSyncRole`: nối vào máy chủ chung bằng chính role `crm_filesync` (`CONNECTION LIMIT 1`, giới
   hạn tính trên cả máy chủ) → hai phiên cùng lúc thì một bên nhận `53300 too many connections for role`.
3. `loadkit.TestReadStats` (`dead = 0, muốn ≥ 1`, báo cáo Task 425): câu đếm dead so `created_at` (đồng hồ của máy chủ
   PostgreSQL, `now()`) với `to` lấy bằng `time.Now()` của máy chạy test. Đồng hồ container chạy trước máy vài ms
   (đo 19/11: +0,5…+2 ms, có lần +100 ms lúc máy bận) → việc dead vừa ghi có `created_at > to`, bị loại khỏi khung.
4. `rules_test.go:71`, `:323`, `audit_test.go:87`: kiểm "văn bản không chứa `0911`" trong chuỗi có UUID ngẫu nhiên;
   UUID hex chứa mẩu `0911` với xác suất ~0,05–0,1%.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không. Chỉ đổi hàm không xuất trong tệp `_test.go`:
  `golangciLint` (`tests/arch/lint_test.go`, chỗ gọi: `lint_test.go` ×2, `skipdirs_test.go` ×1, `lintlock_test.go` ×2;
  chữ ký giữ nguyên), hàm mới `lintTempDir` (`lint_test.go`); tệp mới `tests/arch/lintlock_test.go` (`holdLintLock`,
  `TestLintNotBlockedByOtherRunners`, `TestLintWaitsForSameWorktreeRunner`); hàm mới `filesyncMember`
  (`rules_filesync_test.go`); test
  `TestFileSyncRole`, `TestReadStats`, `TestRuleDeadNoPayload`, `TestRuleErrorsWired`, hàm `orderEntry` và
  `TestRecordAuditStoresFieldNamesNotPersonalValues` (+ ca lỗi dòng 204) của `identity/audit_test.go`.
- Gói phụ thuộc ngược (go list): không có (chỉ đổi tệp test của gói `arch`, `observability`, `identity`, `main` của
  `tools/loadkit`).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không. Test tạo role tạm (cụm máy chủ, tên ngẫu nhiên, xóa khi xong) là thành viên
  của `crm_filesync`; không đổi role thật, không đổi `docker/`, migration hay testkit.
- Test sẽ chạy: `go test -race ./tests/arch/ ./internal/platform/observability/ ./internal/modules/identity/
  ./tools/loadkit/`; chạy lặp ≥ 50 lần từng test bị sửa trong lúc chạy song song golangci-lint và test khác; ci-task.sh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có trợ giúp chạy
  golangci-lint hay tạo role tạm); `internal/testkit` (`StartPostgres`, `randomSuffix` không xuất).
- Dùng lại / viết mới vì: dùng lại `connect`, `newEnv` của test observability; tên role tạm dùng `crypto/rand` tại
  chỗ (testkit không xuất hàm sinh hậu tố, thêm hàm xuất vào testkit là đổi vùng ngoài phạm vi cho 1 chỗ dùng).

## Soát thiết kế

- 5.1–5.12: không áp dụng: không thêm dữ liệu, module, đường ghi hay thư viện; chỉ sửa test.
- 6.x (đồng thời): chính là nội dung task. Khóa golangci-lint: mỗi worktree một `TMPDIR` riêng cho tiến trình lint của
  test (khóa riêng theo worktree) cộng `--allow-serial-runners` (cùng worktree chạy hai lần thì lần sau **chờ** thay vì
  bỏ sau 5 giây; giới hạn bởi timeout của `go test`). Cache golangci-lint giữ nguyên thư mục mặc định dùng chung: cache
  là bản sao cache của Go, an toàn khi nhiều tiến trình cùng dùng, nên vẫn ấm. Role tạm: tên ngẫu nhiên trên cụm, mỗi
  test một role, kết nối của test không chiếm chỗ duy nhất của `crm_filesync`; vẫn kiểm `rolconnlimit = 1`,
  `rolbypassrls = false`, `rolcanlogin = true` của `crm_filesync` (thuộc tính role thật, không chiếm kết nối).

## Quyết định thay chủ shop

- Lint: kế hoạch gợi ý "thư mục cache riêng"; gốc thật là khóa tệp trong `$TMPDIR`, nên đổi chỗ khóa (TMPDIR riêng
  theo worktree, trong thư mục cache người dùng, ổn định giữa các lần chạy) và giữ cache mặc định. Phương án khác:
  `--allow-parallel-runners` (bỏ hẳn khóa — hai lần chạy cùng worktree tranh CPU, không cần thiết) hoặc cache riêng
  theo worktree (cache nguội ở mỗi worktree mới, tốn đĩa, không chữa khóa). Đảo ngược: xóa vài dòng trong `golangciLint`.
- `TestFileSyncRole`: nối bằng role tạm `crm_fs_<ngẫu nhiên>` `LOGIN NOBYPASSRLS CONNECTION LIMIT 1 INHERIT`, thành
  viên của `crm_filesync` → có đúng quyền migration cấp cho `crm_filesync` qua kế thừa. Ý định giữ nguyên: mọi lệnh
  bị cấm vẫn bị 42501, `crm_filesync` vẫn phải `rolconnlimit = 1`, `rolbypassrls = false`. Phương án khác: kiểm chỉ
  bằng `has_table_privilege` (không thử lệnh thật — yếu hơn).
- `TestReadStats`: lấy `to` bằng `clock_timestamp()` của chính máy chủ PostgreSQL sau khi ghi dữ liệu, cùng đồng hồ với
  `created_at`. Không nới khung, không cộng thêm khoảng đệm.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tái hiện lint đỏ trước khi sửa: khóa trong $TMPDIR chung bị giữ → lint của test bỏ | `TestLintNotBlockedByOtherRunners`, `TestLintWaitsForSameWorktreeRunner` (viết trước, mã cũ) | `go test -count=1 -run 'TestLintNotBlockedByOtherRunners\|TestLintWaitsForSameWorktreeRunner' ./tests/arch/` | trước: `FAIL … Error: parallel golangci-lint is running` và `lint xong sau 499ms khi khóa còn bị giữ`; sau: `ok … tests/arch 8.106s` |
| Lint của test không đỏ khi lint khác chạy song song; cùng worktree thì chờ | 5 test lint của `tests/arch` ×50, song song vòng `go test -run 'TestLintCatchesPlantedMistakes$\|TestLintWaitsForSameWorktreeRunner$'` ×50 cùng worktree và `golangci-lint run ./...` ×15 | `go test -race -count=50 -v -run 'TestLintCatchesPlantedMistakes\|TestLintCleanOnRealCode\|TestPackagePatternSkipsStrayDirs\|TestLintNotBlockedByOtherRunners\|TestLintWaitsForSameWorktreeRunner' ./tests/arch/` | `50 PASS` mỗi test (5 dòng đếm), `ok … tests/arch 645.350s`; vòng song song 50 `ok`, 0 `RUNFAIL` |
| Tái hiện `TestFileSyncRole` đỏ: kết nối duy nhất của `crm_filesync` bị chiếm (psql `pg_sleep`, dừng bằng PID) | `TestFileSyncRole` (mã cũ) | `psql -U crm_filesync … pg_sleep(25) & P=$!; go test -count=1 -run 'TestFileSyncRole$' ./internal/platform/observability/; kill $P` | `FAIL … too many connections for role "crm_filesync" (SQLSTATE 53300)` |
| Sau sửa: qua khi `crm_filesync` bị chiếm; vẫn 42501 cho lệnh cấm, `rolconnlimit = 1`; role tạm được xóa | `TestFileSyncRole` | như trên với `pg_sleep(40)`, `go test -race -count=3 …`; `select count(*) from pg_roles where rolname like 'crm_fs_test_%'` | `ok … observability 2.062s`; `0` role tạm còn lại |
| Lặp ≥ 50 lần song song lint và test khác: role, mẩu số, readStats | `TestFileSyncRole`, `TestRuleDeadNoPayload`, `TestRuleErrorsWired`, `TestReadStats`, `TestRecordAuditStoresFieldNamesNotPersonalValues`, `TestRecordAuditValidation` | `go test -race -count=50 -run '…' ./internal/platform/observability/` / `./tools/loadkit/` / `./internal/modules/identity/` (chạy cùng lúc với lint ×15 và tests/arch ×50) | `ok … observability 111.275s`, `ok … tools/loadkit 56.660s`, `ok … identity 92.049s` |
| `TestReadStats`: gốc lệch đồng hồ | `TestReadStats` | `go test -race -count=150 -run 'TestReadStats$' ./tools/loadkit/` (trước sửa); đo `now()` của máy chủ so `time.Now()` 3000 lần | không tái hiện được lúc đo (150/150 qua, 0/3000 lệch dương); gốc suy từ mã: mọi điều kiện khác của câu đếm dead luôn thỏa, chỉ `created_at > to` cho ra 0 (báo cáo Task 425 gặp `dead = 0`) |
| ci-task | toàn bộ cổng | `scripts/agent/ci-task.sh` (sau khi gộp main) | `CI-TASK XANH (so với điểm tách nhánh 01173b96…)`, `== test (gói đổi, phụ thuộc, vùng cao): 659 giây`, gồm `ok … tests/arch 31.128s`, `ok … observability 15.032s`, `ok … tools/loadkit 4.057s`, `ok … modules/identity 158.365s`; 36 dòng `ok`, 0 `FAIL` |
| ci-task lần 2 (sau sửa theo reviewer lần 1: kiểm `rolcanlogin`) | toàn bộ cổng | `ci-task.sh` lần 2 | `CI-TASK XANH (so với điểm tách nhánh 01173b96…)`, `== test (gói đổi, phụ thuộc, vùng cao): 753 giây`, gồm `ok … tests/arch 41.746s`, `ok … observability 20.137s`, `ok … tools/loadkit 4.147s`, `ok … modules/identity 202.055s`; 36 dòng `ok`, 0 `FAIL` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Lint: không dùng thư mục cache riêng (xem Quyết định thay chủ shop; gốc là khóa, không phải cache).

## Việc phát hiện thêm

- `tools/loadkit` chạy thật (`readStats`) cũng lấy khung `[from, to]` bằng đồng hồ máy chạy công cụ trong khi
  `created_at` theo đồng hồ PostgreSQL; khung ở lượt chạy thật dài hàng phút nên lệch vài ms gần như không đổi số đo.
  Không sửa (ngoài phạm vi: task chỉ chữa test đỏ oan).
- Lần chạy lặp đầu tìm ra lỗi chính test mới (`holdLintLock` dùng `LOCK_NB`, và golangci-lint xóa tệp khóa sau khi
  nhả): đã sửa trước khi stage (chờ khóa, kiểm tệp đang khóa vẫn là tệp ở đường dẫn), chạy lặp lại 50/50.

## Reviewer

Kết luận: ĐẠT (lần 2, trên bản đã gộp main).

- Lần 1: ĐẠT, không phát hiện phải sửa. Ghi chú đã làm theo: sửa câu Soát thiết kế cho khớp mã (không kiểm role tạm);
  thêm kiểm `rolcanlogin = true` của `crm_filesync` để giữ phần "role thật đăng nhập được" mà trước đây test kiểm
  ngầm khi nối bằng chính role đó.
- Lần 2: ĐẠT, không phát hiện phải sửa. Ghi chú không làm: `syscall.Flock` không build trên Windows (dev macOS, CI
  Linux); role tạm `crm_fs_test_*` có thể sót nếu tiến trình test bị giết giữa chừng (vô hại, tên ngẫu nhiên).
