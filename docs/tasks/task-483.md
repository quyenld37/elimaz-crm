# Task 483 — Cổng kiểm biên dịch tests/drills, tests/probe

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

Đề bài (không có mục trong kế hoạch; phát hiện ở Task 481): `tests/drills` (nhãn `drill`) và `tests/probe` (nhãn
`probe`) chỉ có tệp mang nhãn build riêng, nên `make test`, `make ci`, ci-task không biên dịch chúng; lỗi biên dịch chỉ
lộ khi diễn tập tay. Thêm bước chỉ biên dịch + vet (không chạy drill), cho ci-task chạy bước đó, có bằng chứng đỏ/xanh.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (không đổi mã chạy CRM; chỉ thêm hàm test không xuất trong `tests/deploy`).
- Gói phụ thuộc ngược (go list): không có; `tests/deploy` là gói test lá. Tệp đổi: `Makefile` (đích `vet`), `tests/deploy/ci_test.go`.
- Người dùng đích `make vet` (`git grep 'make vet'`): `make ci` (qua phụ thuộc `vet`) và `scripts/agent/ci-task.sh` dòng 18 (`step vet make vet`) — nên ci-task tự có bước mới mà không phải sửa script (xem Quyết định thay chủ shop).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -count=1 ./tests/deploy/...` (ci-task tự chạy khi đổi Makefile); test mới `TestMakeVet_CompilesTagOnlyDirs`, test sửa `TestMakeCI_RunsEveryGate`; chạy thật `make vet` (xanh), cài lỗi biên dịch tạm trong `tests/drills` → `make vet` đỏ, gỡ → xanh; đo thời gian bước.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có gì cho Makefile hay nhãn build. Trong `tests/deploy` đã có `makeDry` (ci_test.go) đọc `make -n`, và `buildTags` (manual_suites_test.go) quét dòng `//go:build` đầu tệp test.
- Dùng lại / viết mới vì: dùng lại `makeDry`. Không dùng `buildTags` vì nó trả tập nhãn của cả repo, còn test mới cần biết THƯ MỤC nào chỉ có tệp mang nhãn (thư mục `go vet ./...` bỏ trắng) và nhãn của từng thư mục; viết hàm nhỏ `tagOnlyDirs` cùng gói. Hai hàm cùng đọc dòng đầu tệp nhưng khác mục đích (lịch chạy tay so với cổng biên dịch) và khác lý do đổi: giữ riêng.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.10: không áp dụng — task chỉ đổi cổng kiểm (Makefile, test deploy), không chạm module, dữ liệu, HTTP, Valkey.
- 5.11: không thêm công cụ; dùng `go vet` có sẵn với cờ `-tags`.
- 5.12: theo mẫu test Makefile có sẵn (`makeDry`, `TestMakeCI_RunsEveryGate`).
- 6.1–6.13: không áp dụng — không chạm database, hàng đợi, tiền, tồn, quyền. Riêng an toàn: bước mới chỉ BIÊN DỊCH + vet (`go vet` không chạy test), nên không bao giờ chạy drill (docker kill, xóa volume) trên máy CI hay máy dev.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, đỏ khi Makefile chưa đổi | `TestMakeVet_CompilesTagOnlyDirs`, `TestMakeCI_RunsEveryGate` | `go test -count=1 -run '^(TestMakeVet_CompilesTagOnlyDirs\|TestMakeCI_RunsEveryGate)$' ./tests/deploy/` | `make ci thiếu bước "go vet -tags drill,probe"`; `make vet phải vet tests/drills (chỉ có tệp nhãn [drill], go vet ./... bỏ qua): có gói []`, tương tự `tests/probe`; FAIL |
| Test tự dò đúng thư mục chỉ có tệp nhãn (không thiếu, không thừa) | `TestMakeVet_CompilesTagOnlyDirs` (chốt: phải thấy `tests/drills`, `tests/probe`) | như trên | lần đỏ chỉ báo đúng hai thư mục này (tests/e2e, internal/... có tệp nhãn lẻ nhưng còn tệp không nhãn → không tính) |
| make ci và make vet có bước vet với nhãn drill, probe; không chạy test | hai test trên | như trên, sau khi sửa Makefile | `--- PASS: TestMakeCI_RunsEveryGate`, `--- PASS: TestMakeVet_CompilesTagOnlyDirs`, `ok elimaz.com.vn/crm/tests/deploy 0.594s` |
| Lỗi biên dịch cài tạm trong tests/drills → bước mới đỏ | chạy thật | thêm `func brokenOnPurpose() int { return "not an int" }` vào `tests/drills/filestore_crash_test.go`; `make vet` | `vet: tests/drills/filestore_crash_test.go:136:37: cannot use "not an int" (untyped string constant) as int value in return statement`, `make: *** [vet] Error 1`, `make vet rc=2` |
| Như trên với tests/probe | chạy thật | cùng dòng lỗi vào `tests/probe/seaweedfs_test.go`; `make vet` | `vet: tests/probe/seaweedfs_test.go:735:37: cannot use "not an int" …`, `make: *** [vet] Error 1` |
| Gỡ lỗi → xanh | chạy thật | `git checkout <tệp>`; `make vet` | `make vet sau khi gỡ rc=0` |
| Thời gian bước (đo) | — | `time go vet -tags drill,probe ./tests/drills/... ./tests/probe/...` | cache lạnh hoàn toàn 48,6 s (lần đầu trên worktree mới); cache ấm 0,9–9 s; trong ci-task (sau `make build`) cả `make vet` (hai dòng) = `== vet: 4 giây` |
| ci-task có bước mới mà không sửa script | log ci-task | `scripts/agent/ci-task.sh` | log có `go vet -tags drill,probe ./tests/drills/... ./tests/probe/...` trong `== vet` (`== vet: 3 giây`); `ok elimaz.com.vn/crm/tests/arch 29.750s`, `ok elimaz.com.vn/crm/tests/deploy 39.605s` (-race) và `39.845s` (bước test deploy); `CI-TASK XANH (so với điểm tách nhánh 695b4e7d…)` |

Ghi chú vận hành: lần ci-task đầu bị dừng ở giới hạn 10 phút của lệnh nền giữa lúc `tests/deploy` chạy test affected.py, để lại thư mục ẩn tạm `.zz481334007239/` trong worktree; lần chạy thứ hai vì thế thoát 4 (chạy toàn bộ test), đã dừng lần đó, xóa thư mục rác (của chính phiên này), chạy lại với giới hạn 1 giờ → xanh như trên.

## Quyết định thay chủ shop

- **Đặt bước mới vào chính đích `make vet` (dòng thứ hai), không tạo đích riêng.** Vì `make ci` và `scripts/agent/ci-task.sh` (dòng 18 `step vet make vet`) đều đã gọi `make vet`, nên ci-task tự chạy bước mới mà không phải sửa `scripts/agent` (hook cấm phiên task sửa). Đo: < 20 s khi cache ấm (4 s cả bước vet trong ci-task) nên "luôn chạy", không lọc theo tệp đổi (lọc theo gói drill/probe nhập sẽ phải dựng đồ thị phụ thuộc — chúng nhập ~40 gói nội bộ — tốn hơn chính bước vet). Phương án khác: đích `vet-tagonly` riêng + thêm `step` trong ci-task (cần điều phối viên áp patch). Đảo ngược dễ: xóa một dòng.
- **Không gửi patch bắt buộc cho ci-task.** Thư mục `/private/tmp/claude-501/-Users-luongducquyen-Documents-Th-i-trang/3f611c4b-a555-41c3-82e1-b84aa5a61f94/scratchpad/task-483/` có `README.md` giải thích, và `ci-task-comment.patch` TÙY CHỌN (chỉ thêm chú thích vào ci-task.sh, affected.py; `git apply --check` qua).
- **Test tự dò thư mục chỉ có tệp nhãn** thay vì ghi cứng hai thư mục: thư mục loại này thêm sau sẽ làm test đỏ cho tới khi thêm vào `make vet`.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có mục trong kế hoạch; làm theo đề bài giao trong phiên)

## Việc phát hiện thêm

- Tệp test mang nhãn nằm LẪN trong gói thường cũng không được biên dịch bởi `go vet ./...`: `tests/e2e/files_pipeline_test.go` (`imager`), `tests/e2e/filesync_test.go` (`filesync`), 4 tệp `perf` (`internal/platform/queue`, `internal/modules/inventory`, `internal/imager`), 1 tệp `imagercompose` (`internal/imager`). Lỗi biên dịch ở đó cũng chỉ lộ khi chạy tay. Cách sửa gợi ý: thêm vào `make vet` `go vet -tags imager,filesync,perf,imagercompose <đúng các gói đó>` (cần đo; `tests/e2e` nhập nhiều gói). Ngoài phạm vi Task 483 (đề bài chỉ drills, probe), không tự làm.

## Reviewer

Kết luận: ĐẠT (agent reviewer, 24/11, trên phần đã stage: Makefile, tests/deploy/ci_test.go, tests/deploy/tagonly_test.go, tệp task).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp: đích `vet` (Makefile), `make ci` phụ thuộc `vet`, `ci-task.sh:18` gọi `make vet`; nhãn drill, probe chỉ có ở 5 tệp trong hai thư mục nên không kéo thêm mã khác vào bản dựng.
- Bằng chứng: hai test đọc `make -n` thật, đỏ khi Makefile chưa đổi; `go vet` biên dịch cả tệp `_test.go` nên bắt được lỗi biên dịch của drill; không test nào bị nới.
- Ghi chú không bắt buộc (không sửa): mục Ảnh hưởng nên nêu tên tệp `tests/deploy/tagonly_test.go` (tệp test mới, chứa `tagOnlyDirs`, `skipScanDir`, `firstLineTag`, `TestMakeVet_CompilesTagOnlyDirs`); nếu `tests/drills` có thư mục con sau này, test sẽ đòi thêm `./tests/drills/<con>/...` (đỏ thừa, không bỏ sót); patch tùy chọn nằm ở thư mục tạm — nội dung chỉ là hai dòng chú thích, đã tóm ở mục Quyết định.
