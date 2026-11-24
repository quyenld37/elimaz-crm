# Task 479 — affected.py: TRUNCATE/DELETE chỉ tính khi là câu lệnh

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — chỉ thêm hàm test (gói `deploy_test`, không xuất ra ngoài).
- Gói phụ thuộc ngược (go list): không có — `tests/deploy` là gói chỉ có test, không ai nhập.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có migration mới. Đổi `scripts/agent/affected.py` (công cụ chọn test, không phải mã chạy CRM), hàm `migration_scope`: bỏ `TRUNCATE`, `DELETE FROM` khỏi `NARROW_RE` (quét cả tệp); thêm `FUNC_BODY_RE` (thân `AS $tag$…$tag$` của câu CREATE FUNCTION), `DEL_STMT_RE` (TRUNCATE/DELETE ở đầu câu hay ngay sau `;` trong câu, ngoài thân hàm → thoát 5), `DEL_ANY_RE` (`DELETE FROM` ở bất kỳ đâu ngoài thân hàm → thoát 5), `BODY_DEL_RE`, `BODY_TRUNC_RE`, `TRUNC_WORDS_RE` (trong thân hàm: tên bảng sau DELETE FROM, mọi tên trong danh sách TRUNCATE phải có schema, qua `qualified()`, không thì thoát 5); chỉ phần `AS $tag$…$tag$` của câu `CREATE [OR REPLACE] FUNCTION` được miễn (`FUNC_BODY_RE`, thay `BODY_RE`); `DEL_STMT_RE` tìm TRUNCATE/DELETE ở đầu câu hay ngay sau `;` còn sót trong câu gộp (sau review lượt 1, 2). Docstring dòng 10. Bên gọi `ci-task.sh`, `stop-check.sh` chỉ đọc mã thoát, không đổi. Phiên task không được sửa `scripts/agent/` (hook): bản vá ở scratchpad, điều phối viên áp + commit (như Task 478).
- Test sẽ chạy: `go test -count=1 -run 'TestAffected_' ./tests/deploy/` trên bản sao tạm đã áp bản vá (cả bộ affected vì đổi luật chung); ci-task trên worktree (gói `tests/deploy`; ca mới đỏ trên worktree tới khi bản vá được áp — ghi ở Bằng chứng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: danh mục chỉ có mã Go. Trong `affected.py` đã có `statements()` (tách câu ngoài `$tag$`), `strip_sql()`, `qualified()`, `WRITE_RE` (luật "ghi trong thân hàm phải có schema"). Trong test: `withMigration`, `affected`, `emptyTree`, `wantOnlyAlwaysRun`, `mark`, `wantKinds` (Task 473/478).
- Dùng lại / viết mới vì: dùng lại `statements()` để xét đầu câu, `qualified()` cho tên bảng trong thân hàm (cùng luật với `WRITE_RE`), toàn bộ trợ giúp test. Viết mới `FUNC_BODY_RE` (thân `AS $tag$…$tag$` của câu CREATE FUNCTION, cùng định nghĩa thẻ `\$\w*\$` như `statements()`), vì `statements()` giữ thân hàm trong câu.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng — chỉ đổi công cụ chọn test (Python, thư viện chuẩn) và test của nó; không chạm module, HTTP, database, thư viện.
- 6.1–6.13: không áp dụng — không database, hàng đợi, tiền, tồn, quyền. Rủi ro riêng: BỎ SÓT (migration thật sự xóa dữ liệu mà chỉ chạy một phần test). Chống: mọi lệnh thu hẹp khác giữ luật quét cả tệp (kể cả thân hàm); câu TRUNCATE/DELETE ngoài thân hàm → toàn bộ; `DELETE FROM` bất kỳ đâu ngoài thân hàm → toàn bộ; câu không nhận ra (`WITH … DELETE`, `CREATE RULE … DO DELETE`, `EXECUTE`) vẫn → toàn bộ; trong thân hàm, DELETE/TRUNCATE trên tên không schema hay động (`EXECUTE 'DELETE FROM ' || t`) → toàn bộ. Lưới an toàn giữ nguyên: toàn bộ test -race trên main sau mỗi đợt gộp.

## Bằng chứng

Thư mục bản vá (ngoài git): `/private/tmp/claude-501/-Users-luongducquyen-Documents-Th-i-trang/3f611c4b-a555-41c3-82e1-b84aa5a61f94/scratchpad/task-479/` — `affected-479.patch` (vá `scripts/agent/affected.py`; áp từ gốc repo bằng `git apply`), `affected.py` (bản đầy đủ đã vá; `affected.orig.py` là bản gốc), `affected_test-479.patch` và `affected_test.go` (bản đầy đủ của `tests/deploy/affected_test.go`), `before.tsv`/`after.tsv` (phân loại từng migration), `classify.sh`, `runtest.sh` (dựng bản sao `copy/` = clone nhánh task-479 + chép bản vá và test + `git add -u`, chạy cả bộ `TestAffected_`), `reasons.sh` (lý do thoát 5 từng ca). Nhánh đã gộp main (fast-forward tới `ee6c84e`, có 0504 của Task 195).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test mới đỏ trên affected.py cũ (test trước) | `TestAffected_AppendOnlyTriggerMigrationsAreNarrow`, `TestAffected_TruncateInTriggerOrBodyIsNarrow`, `TestAffected_DeleteOrTruncateStatementRunsAll` | `go test -count=1 -run '^(…ba tên…)$' ./tests/deploy/` (worktree, script cũ) | `FAIL`: `migrations/0004_audit.sql … thoát 5`, `0115…`, `0504…`, `affected_test.go:426: thoát 5`; ca "→ 5" qua (bản cũ vốn đã thoát 5) |
| Trigger chặn TRUNCATE trên bảng mới → hẹp; 0504 chọn `internal/modules/order` (plain) | `TestAffected_AppendOnlyTriggerMigrationsAreNarrow` (0004, 0115, 0504 thật) | `bash runtest.sh` → `go test -count=1 -v -run TestAffected_ ./tests/deploy/` (bản sao đã vá) | `--- PASS`; cả bộ (bản vá cuối) `ok elimaz.com.vn/crm/tests/deploy 32.033s` (17 hàm TestAffected cũ + mới) |
| Bảng mới + hàm + trigger `BEFORE UPDATE OR DELETE OR TRUNCATE` + `REVOKE UPDATE, DELETE, TRUNCATE` + DELETE FROM / `TRUNCATE TABLE ONLY a, b RESTART IDENTITY CASCADE` (có schema) trong thân hàm mới → chỉ gói luôn chạy | `TestAffected_TruncateInTriggerOrBodyIsNarrow` | như trên | `--- PASS` |
| `TRUNCATE ord.x;`, `DELETE FROM ord.x WHERE …;`, truncate chữ thường sau câu khác, DELETE sau thân hàm, DELETE lồng trong WITH và trong INSERT, xóa động (`EXECUTE 'DELETE FROM ' \|\| t`), DELETE/TRUNCATE bảng không schema trong thân hàm, TRUNCATE danh sách có tên không schema, chữ TRUNCATE trong thông báo, câu DELETE (reviewer lượt 1) và câu TRUNCATE (reviewer lượt 2: sau COMMENT, sau DEFAULT, sau thân hàm nháy đơn) nằm giữa hai chuỗi nháy đơn có `$$` → 5 | `TestAffected_DeleteOrTruncateStatementRunsAll` (15 ca) | như trên; `bash reasons.sh` (lý do) | `--- PASS`; lý do (`reasons.sh`, bản vá cuối, đủ 15 ca): `có lệnh 'TRUNCATE'`, `'DELETE FROM'`, `'truncate'`, `'DELETE FROM'` ×3, `đối tượng ''' không ghi rõ schema`, `đối tượng 'tags' không ghi rõ schema` ×2, ca `$$` trong chuỗi: `có lệnh 'DELETE FROM'`, danh sách TRUNCATE: `'tags'`, thông báo: `không rõ bảng của TRUNCATE trong thân hàm 'bảng này''`, ba ca `$$` bọc TRUNCATE: `có lệnh 'TRUNCATE'` ×3 |
| Lệnh thu hẹp cũ giữ nguyên (DROP, contract, DO $$, quyền cả schema, ALTER COLUMN, câu lạ…) | `TestAffected_NarrowingOrUnsureMigrationRunsAll` (cũ, nay dùng `wantRunsAll`) | như trên | `--- PASS` |
| Phân loại toàn bộ 71 migration (coi là mới, `--base` cây rỗng) trước/sau (đo lại với bản vá cuối, kết quả như nhau) | đo tay | `bash classify.sh affected.orig.py > before.tsv`; `bash classify.sh affected.py > after.tsv` | Trước: 20 FULL / 51 HẸP. Sau: 15 FULL / 56 HẸP. Đổi loại: 0004, 0115, 0117, 0118, 0504 FULL→HẸP (chỉ tạo bảng/hàm mới + trigger chỉ-ghi-thêm có sự kiện TRUNCATE + REVOKE … TRUNCATE, không câu xóa nào — đúng là chỉ thêm). 0119 vẫn FULL nhưng lý do đổi từ `'TRUNCATE'` sang `không nhận ra câu 'CREATE CONSTRAINT TRIGGER …'` (đúng: STMT_RE chưa nhận constraint trigger). 0201, 0204, 0300, 0303 (có trigger TRUNCATE) vẫn FULL vì là pha `-- contract:`. Không migration nào HẸP→FULL. |
| 0504 cụ thể | đo tay | `echo migrations/0504_order_requests.sql \| affected.py --base <cây rỗng> --split` | cũ: thoát 5 (toàn bộ, -race); mới: thoát 0, 70/108 gói, tất cả `plain` (rộng vì 0504 còn `ALTER TABLE ord.order_settings ADD COLUMN` — bảng đã có, gói order được nhiều gói nhập) |
| Cổng kiểm của task trên worktree (chỉ tệp task đã stage, test và script ở bản vá) | ci-task | `scripts/agent/ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh ee6c84e…)`; jscpd `Found 0 clones`, gitleaks `no leaks found`, `ok elimaz.com.vn/crm/tests/arch 123.899s` |
| Sau review lượt 1 và 2, phần sửa chỉ ở bản vá ngoài git (worktree không đổi mã) nên không chạy lại ci-task; kiểm lại trên bản sao bằng các lệnh hàng dưới | — | — | — |
| Vet, lint, -race, trùng mã, cú pháp Python | cả gói `tests/deploy` | `go vet ./tests/deploy/`; `golangci-lint run ./tests/deploy/...`; `go test -race -count=1 ./tests/deploy/...`; `make jscpd`; `python3 -m py_compile affected.py` (bản sao) | (bản vá cuối) `0 issues.`; `ok … 30.834s`; `Found 0 clones`; `py-ok`; `go vet ./tests/deploy/` `vet-ok` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Hook không cho phiên task sửa hay `git add` `scripts/agent/`. Như Task 478: bản vá `affected.py` và test mới nằm ở thư mục bản vá phía trên (ngoài git), đã chạy trên bản sao tạm. Test mới KHÔNG commit lượt này (trên nhánh còn affected.py cũ, ca mới đỏ → ci-task/hook đỏ). Lượt này chỉ commit tệp task.
- **Việc của điều phối viên:** (1) từ gốc repo `git apply …/task-479/affected-479.patch` (chỉ đổi `scripts/agent/affected.py`) và commit; (2) mở lại phiên task 479 để áp `affected_test-479.patch` (hoặc chép `affected_test.go`) vào `tests/deploy/affected_test.go`, chạy `go test -count=1 -run TestAffected ./tests/deploy/`, review và commit.
- `UPDATE … SET` không nằm trong `NARROW_RE` cũ (câu UPDATE đã được STMT_RE nhận, chạm bảng đó): không cần đổi.

## Quyết định thay chủ shop

1. **`DELETE FROM` ở bất kỳ đâu ngoài thân hàm → toàn bộ**, không chỉ ở đầu câu (đề bài: đầu câu). Lý do: dạng lồng (`WITH d AS (DELETE …)`, `INSERT … (DELETE …)`) vẫn xóa dữ liệu; ngoài thân hàm, chữ `DELETE FROM` không có nghĩa nào khác. TRUNCATE chỉ tính đầu câu (TRUNCATE không lồng được; chữ TRUNCATE ngoài đầu câu chỉ có trong sự kiện trigger, GRANT/REVOKE). Phương án khác: chỉ đầu câu (dạng lồng vẫn bị bắt vì câu không nhận ra, nhưng dựa vào luật khác).
2. **DELETE/TRUNCATE trong thân hàm** coi như INSERT/UPDATE trong thân hàm (luật `WRITE_RE` có sẵn): được, nếu mọi tên bảng có schema; tên không schema, xóa động, chữ TRUNCATE không theo sau bởi đúng một tên bảng (vd trong thông báo lỗi) → toàn bộ (thận trọng, có thể làm một migration hiếm chạy toàn bộ thừa). Phương án khác: giữ luật cũ cho thân hàm (mọi chữ TRUNCATE trong thân hàm → toàn bộ).
3. Không thêm `CREATE CONSTRAINT TRIGGER` vào STMT_RE (0119 vẫn FULL) — ngoài phạm vi đề bài, ghi ở Việc phát hiện thêm.

## Việc phát hiện thêm

- `CREATE CONSTRAINT TRIGGER` chưa được STMT_RE nhận (0119 và mọi migration sau có constraint trigger → toàn bộ test). Có thể thêm `(?:CONSTRAINT\s+)?` vào nhóm `trigger` của STMT_RE (khối nối `defs` đã nhận dạng này). Không làm (ngoài phạm vi).
- Thân hàm `LANGUAGE sql AS '…'` (nháy đơn thay vì `$$`) không bị `FUNC_BODY_RE` bỏ: `DELETE FROM` trong đó → toàn bộ (thận trọng, không bỏ sót). Có từ trước.

## Reviewer

Lượt 1: **ĐẠT** (không phát hiện phải sửa). Ghi chú không bắt buộc: (1) chuỗi nháy đơn chứa `$$` (`COMMENT … 'a $$'; DELETE FROM ord.orders; COMMENT … 'b $$';`) làm `BODY_RE`/`statements()` nuốt câu DELETE → thoát 0 (bản cũ bắt được); (2) ca `TRUNCATE ord.x;`/`DELETE FROM` vẫn thoát 5 kể cả bỏ `DEL_STMT_RE` (STMT_RE không nhận câu), test chỉ kiểm mã thoát; (3) CONSTRAINT TRIGGER đã ghi ở Việc phát hiện thêm. Đã sửa (1): thân `$tag$` chỉ được miễn ở câu `CREATE [OR REPLACE] FUNCTION`, mọi câu khác xét nguyên văn; thêm ca test. (2) giữ: lý do được đối chiếu bằng `reasons.sh` (bảng bằng chứng); luật thừa một lớp là cố ý.

Lượt 2: **CẦN SỬA** — cùng cơ chế với (1) nhưng cho TRUNCATE: `COMMENT … 'a $$'; TRUNCATE catalog.tags; COMMENT … 'b $$';` (và biến thể `DEFAULT 'a $$'`) → `statements()` gộp ba câu, câu gộp không bắt đầu bằng TRUNCATE → thoát 0. Ghi chú thêm: hàm có thân nháy đơn chứa `$$` rồi TRUNCATE cũng lọt qua `BODY_RE`. Đã sửa: `DEL_STMT_RE` = `(?:^|;)\s*(TRUNCATE|DELETE)\b` tìm trong cả câu (dấu `;` còn trong câu gộp); thân hàm chỉ là `AS $tag$…$tag$` của câu CREATE FUNCTION (`FUNC_BODY_RE`). Thêm 3 ca test. Ghi chú không sửa: thân `LANGUAGE sql AS 'TRUNCATE x'` (nháy đơn, không schema) nay không bị bắt — hàm không chạy lúc migrate, chỉ lệch luật "phải ghi schema"; `strip_sql` cắt `--` trong chuỗi (có từ trước).

Lượt 3: **ĐẠT** (không phát hiện phải sửa). Ba ca lọt của lượt 2 đã chặn; reviewer thử thêm các kiểu lệch cặp `$$` (DEFAULT tham số hàm, `COMMENT … 'b AS $$'`, `BEGIN ATOMIC`) đều thoát 5. Ghi chú không bắt buộc: còn một đường gần như không gặp (thân hàm nháy đơn chứa đúng chữ `AS $$`, rồi TRUNCATE có schema, rồi chuỗi có `$$`) — không sửa, lưới an toàn là toàn bộ test -race trên main; ca TRUNCATE/DELETE đứng một mình không chứng minh riêng nhánh đầu câu (ba ca `$$` chứng minh nhánh `;`); hai dòng chữ dùng tên cũ `BODY_RE` — đã sửa.

Kết luận: ĐẠT
