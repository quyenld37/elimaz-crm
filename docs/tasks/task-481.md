# Task 481 — Dọn công cụ nhỏ: make lint chờ khóa, CREATE CONSTRAINT TRIGGER, tests/drills thoát 4

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi `Makefile` (đích `lint`), test trong gói `tests/deploy` (tệp `affected_test.go`, không xuất ký hiệu) và bản vá `scripts/agent/affected.py` (điều phối viên áp; hook không cho phiên task sửa `scripts/agent/`).
- Gói phụ thuộc ngược (go list): `tests/deploy` là gói chỉ có test, không gói nào nhập. Người dùng `make lint`: `make ci`, `scripts/agent/ci-task.sh` (bước lint), hook Stop; `tests/deploy/ci_test.go` `TestMakeCI_RunsEveryGate` kiểm chuỗi `golangci-lint run ./...` (giữ nguyên chuỗi, thêm cờ phía sau). Người dùng `affected.py`: `ci-task.sh` (dòng 63: thoát khác 0 → `make test` toàn bộ) và `tests/deploy/affected_test.go`.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không đổi migration nào. affected.py chỉ đọc migration: thêm `CREATE CONSTRAINT TRIGGER … ON s.t` vào bộ nhận câu (`STMT_RE`, nhóm trigger) — migration 0119 (`cash.cod_transfers`, `cash.cod_ledger_entries`) nay được thu hẹp thay vì thoát 5.
- Test sẽ chạy: `go test -count=1 -run 'TestAffected_' ./tests/deploy/` trên bản sao tạm có bản vá (scratchpad/copy) và trong worktree (bản chưa vá: test mới phải đỏ); `TestMakeCI_RunsEveryGate`; hai `make lint` song song (bằng chứng việc 1); ci-task (bước "test deploy" vì đổi Makefile).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: danh mục chỉ có mã Go của `internal/platform`, không liên quan công cụ Python/Makefile. Tìm `allow-serial-runners`: đã dùng ở `tests/arch/lint_test.go` (dòng 66). Tìm `CONSTRAINT`: affected.py đã nhận `CONSTRAINT TRIGGER` ở bước nối qua trigger (dòng 213) nhưng chưa ở `STMT_RE` (dòng 74).
- Dùng lại / viết mới vì: dùng lại cờ `--allow-serial-runners` như tests/arch; dùng lại nhóm `trigger` của `STMT_RE` (thêm `(?:CONSTRAINT\s+)?`); thư mục nhãn build dùng lại `go list -e -json` (đã dùng cho cả repo) cho riêng thư mục không khớp. Test dùng lại `affected`, `withMigration`, `wantOnlyAlwaysRun`, `mark`, `wantKinds` sẵn có trong `affected_test.go`.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng — không đổi mã chạy CRM, module, database, quyền, API; chỉ công cụ kiểm (Makefile, affected.py) và test của chúng.
- 6.1 (hai bên cùng lúc dùng cùng thứ): áp dụng cho `make lint` — golangci-lint giữ khóa tệp `$TMPDIR/golangci-lint.lock`; không có `--allow-serial-runners` thì lần chạy thứ hai đỏ ngay ("parallel golangci-lint is running"). Có cờ: lần sau chờ lượt (khóa của chính golangci-lint), kết quả không đổi. Bằng chứng: hai `make lint` cùng lúc đều xanh.
- 6.2–6.13: không áp dụng — không có transaction, khóa database, việc nền, gọi ra ngoài.
- Bộ chọn test không được bỏ sót: (a) CONSTRAINT TRIGGER xếp cùng loại `trigger` (không phải `create`) nên mọi đối tượng trong câu (bảng, hàm, cả bảng sau `FROM`) đều tính là bị chạm, như trigger thường. (b) Thư mục mà MỌI tệp Go bị nhãn build loại khỏi bản dựng thường (`go list -e` báo "build constraints exclude all Go files", không có GoFiles/TestGoFiles/XTestGoFiles): không gói nào của bản dựng thường nhập hay biên dịch nó, `make test` (`go test ./...`) cũng không chạy nó, nên chạy toàn bộ test không kiểm thêm được gì → không chọn gói nào cho thư mục đó, ghi một dòng stderr. Thư mục không khớp vì lý do khác (thư mục `_x`, module lồng…) vẫn thoát 4.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (1) `make lint` có `--allow-serial-runners`, giữ chuỗi `golangci-lint run ./...` | `TestMakeLint_WaitsForOtherRunner` (mới), `TestMakeCI_RunsEveryGate` | `go test -count=1 -run '^(TestMakeLint_WaitsForOtherRunner\|TestMakeCI_RunsEveryGate)$' ./tests/deploy/` | trước khi sửa Makefile: `FAIL … golangci-lint run ./...`; sau: `ok elimaz.com.vn/crm/tests/deploy 0.252s` |
| (1) hai `make lint` cùng lúc đều xanh | (đo tay) | hai `make lint` chạy nền song song, `wait` cả hai | `golangci-lint run ./... --allow-serial-runners` / `0 issues.` / `lint1 exit 0`; `lint2 exit 0` |
| (1) đối chứng: không cờ thì lần thứ hai đỏ | (đo tay) | `golangci-lint run ./... &` rồi 1 s sau `golangci-lint run ./...` | `n2 thoát 3`, `Error: parallel golangci-lint is running`; `n1 thoát 0` |
| (2) `CREATE CONSTRAINT TRIGGER … ON s.t` nhận ra được; 0119 được thu hẹp, chọn gói cash | `TestAffected_ConstraintTriggerIsNarrow`, `TestAffected_ConstraintTriggerOnExistingTableTouchesUsers` (mới) | bản chưa vá (worktree): `go test -count=1 -run '^TestAffected_(…)$' ./tests/deploy/`; bản vá (bản sao tạm `scratchpad/copy`): `go test -count=1 -v -run '^TestAffected_' ./tests/deploy/` | chưa vá: `0119 có CONSTRAINT TRIGGER … thoát 5`, `affected_test.go:367: thoát 5`; vá: `--- PASS: TestAffected_ConstraintTriggerIsNarrow (19.93s)`, `--- PASS: TestAffected_ConstraintTriggerOnExistingTableTouchesUsers (10.34s)`; `affected.py --base <cây rỗng> --split` với 0119: `thoát 0`, 56 gói plain (cash, order, fulfillment… dùng bảng mà hàm/trigger nhắc tới) thay vì toàn bộ |
| (3) đổi tệp trong thư mục chỉ có tệp nhãn build (tests/drills, tests/probe, cả tệp mới) → thoát 0, không gói nào; lẫn với gói thường vẫn chọn gói đó | `TestAffected_BuildTaggedOnlyDirSelectsNothing` (mới) | như trên | chưa vá: `tests/drills/drill_test.go: muốn thoát 0 … có 4 []` (3 ca + `thoát 4`); vá: `--- PASS: TestAffected_BuildTaggedOnlyDirSelectsNothing (19.65s)`; stderr: `affected.py: tests/drills chỉ có tệp nhãn build riêng (ngoài bản dựng thường): không chọn gói nào` |
| (3) thư mục không khớp vì lý do khác vẫn thoát 4 | `TestAffected_UnmatchedGoDirExits4` (đổi ca: thư mục tạm ẩn `.zz481…` thay cho tests/probe) | như trên | chưa vá và vá đều `PASS` (vá: `--- PASS: TestAffected_UnmatchedGoDirExits4 (5.07s)`) |
| Patch áp được lên nhánh và xanh sau khi áp | (cả bộ TestAffected_, TestMake*) | `git apply --check <patch>` trong worktree; bản sao tạm = commit nhánh + patch: `go test -count=1 -run '^(TestAffected_\|TestMake)' ./tests/deploy/` | `apply-check OK`; `ok elimaz.com.vn/crm/tests/deploy 33.611s` |
| Cổng kiểm của task | ci-task | `scripts/agent/ci-task.sh` | `ok elimaz.com.vn/crm/tests/arch 29.010s`; `ok elimaz.com.vn/crm/tests/deploy 38.976s` (-race), `47.174s` (test deploy); `CI-TASK XANH (so với điểm tách nhánh b4dd070…)`; lần 2 sau sửa review (thư mục ẩn): `ok elimaz.com.vn/crm/tests/arch 60.391s`, `ok elimaz.com.vn/crm/tests/deploy 52.745s`, `CI-TASK XANH`; patch dựng lại: `apply-check OK`, bản sao + patch `ok … tests/deploy 33.639s` |
| Lượt 2: 3 test mới commit sau script (`33b6318`); jscpd báo lặp giữa `wantOnlyAlwaysRun` và test mới → tách helper `affectedMigration` (hai chỗ dùng) | 20 `TestAffected_*` | `make jscpd`; `go test -count=1 -run '^TestAffected_' ./tests/deploy/`; `scripts/agent/ci-task.sh` | `Found 0 clones.`; `ok elimaz.com.vn/crm/tests/deploy 27.443s`; ci-task: `ok … tests/arch 27.324s`, `ok … tests/deploy 52.302s` (-race), `37.528s` (test deploy), `CI-TASK XANH (so với điểm tách nhánh b4dd070…)`; `git log HEAD..main` rỗng (gộp main không có gì mới) |
| Không hỏng ca cũ của affected.py | 16 test `TestAffected_*` có sẵn | bản vá: `go test -count=1 -v -run '^TestAffected_' ./tests/deploy/` | 20/20 `--- PASS`, `ok elimaz.com.vn/crm/tests/deploy 29.457s` |

## Test bị xóa hoặc tắt

(không có) — `TestAffected_UnmatchedGoDirExits4` giữ tên và ý (thư mục Go không khớp gói → thoát 4) nhưng đổi ca: tests/probe (chỉ tệp nhãn `probe`) nay đúng ra phải thoát 0 (việc 3), nên dùng thư mục tạm ẩn `.zz481…` ở gốc module (go list ./... bỏ qua thư mục ẩn, không có lỗi nhãn build; sau review lần 1 đổi từ `_zz481…` sang thư mục ẩn để `walkGoFiles` của tests/arch, chỉ bỏ thư mục ẩn, không đọc phải nó khi `go test ./...` chạy song song trên main).

## Khác kế hoạch

(không có)

## Quyết định thay chủ shop

- Việc 3, chọn "không chọn gói nào" cho thư mục chỉ có tệp nhãn build, thay vì `go list -tags` rồi chạy test với nhãn: test nhãn `drill` làm hỏng thật (docker kill, xóa volume), `probe` cần SeaweedFS riêng — không được chạy tự động trong ci-task; còn chạy `go test` không nhãn trên gói đó thì lỗi "build constraints exclude all Go files". Toàn bộ test (`make test`) cũng không chạy chúng, nên thoát 4 → toàn bộ chỉ tốn ~15 phút mà không kiểm thêm gì. Đảo ngược dễ: bỏ khối "tagged" trong affected.py.
- Cập nhật lượt 2 (24/11): điều phối viên đã áp phần script của bản vá ở commit `33b6318` trên nhánh này; ba test mới vào commit sau đó (lượt này), nên main không lúc nào có test đỏ. Ghi chú gốc dưới đây giữ để đối chiếu.
- Bản vá affected.py ở `/private/tmp/claude-501/-Users-luongducquyen-Documents-Th-i-trang/3f611c4b-a555-41c3-82e1-b84aa5a61f94/scratchpad/task-481/affected.py.patch` (hook không cho phiên task sửa `scripts/agent/`; bản đầy đủ của tệp: `…/task-481/affected.py`). Ba test mới cho việc 2, 3 (`TestAffected_BuildTaggedOnlyDirSelectsNothing`, `TestAffected_ConstraintTriggerIsNarrow`, `TestAffected_ConstraintTriggerOnExistingTableTouchesUsers`) đỏ trên affected.py chưa vá, nên **đi cùng patch đó** (patch sửa cả `scripts/agent/affected.py` và `tests/deploy/affected_test.go`, so với commit của nhánh này) thay vì commit trên nhánh: commit chúng trước sẽ làm hook Stop, ci-task của phiên này và main (sau gộp, trước khi áp patch) đỏ. Điều phối viên áp patch bằng `git apply` sau khi gộp nhánh → script và test vào cùng một commit. Phương án khác: commit test trên nhánh, main đỏ tới khi áp patch (loại vì làm đỏ main và mọi task gộp main trong khoảng đó).

## Việc phát hiện thêm

- Không cổng nào biên dịch các gói chỉ có tệp nhãn build (`tests/drills` nhãn `drill`, `tests/probe` nhãn `probe`): `make build/vet/lint/test` đều không nhãn. Lỗi biên dịch ở đó chỉ lộ khi chạy diễn tập tay. Gợi ý (task riêng): thêm vào ci-task/make ci bước `go vet -tags drill,probe ./tests/drills/ ./tests/probe/` (chỉ biên dịch, không chạy).

## Reviewer

Kết luận: ĐẠT (lượt 2, agent reviewer, 24/11: 3 test mới + helper `affectedMigration` + tệp task; affected.py đã vào ở `33b6318`). Không phát hiện phải sửa. Ghi chú không bắt buộc (không làm): số dòng `affected_test.go:367` trong trích dẫn cũ là trước khi tách helper; mục Tái dùng chưa nêu tên `affectedMigration` (đã nêu ở dòng bằng chứng lượt 2).

Lần trước (review bản vá): ĐẠT (lần 2, agent reviewer, 24/11; review cả phần đã stage và patch affected.py)

- Lần 1: ĐẠT, không phát hiện phải sửa. Ghi chú: thư mục tạm `_zz481…` của `TestAffected_UnmatchedGoDirExits4` có thể bị `walkGoFiles` (tests/arch) đọc phải khi `go test ./...` chạy song song trên main → đã sửa sang thư mục ẩn `.zz481…`, dựng lại patch (`git apply --check` OK), ci-task lần 2 xanh.
- Lần 2: ĐẠT, không phát hiện phải sửa. Đã kiểm: chuỗi `golangci-lint run ./...` còn nguyên; `stop-check.sh` gọi golangci-lint trực tiếp, không qua `make lint` (không ảnh hưởng); khối `tagged` chỉ lọc bớt `unmatched`, `go list` lỗi → vẫn thoát 4; thư mục ẩn không bị tests/arch, `go list ./...`, `buildTags` (manual_suites_test.go, test không song song) thấy; gốc của hunk test trong patch là bản đã stage.
- Ghi chú không bắt buộc (không làm): thời gian trong bảng bằng chứng dòng (3)-thoát 4 là của lần chạy với `_zz481`; patch nằm trong scratchpad, điều phối viên nên áp sớm.
