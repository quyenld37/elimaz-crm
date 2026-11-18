# Task 392 — make mutation phủ đúng vùng rủi ro cao, một nguồn danh sách (M0-23b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi `Makefile` (đích `mutation`, biến
  `MUTATION_DIRS`, `MUTATION_HTTPAPI_EXCLUDE`), thêm test trong `tests/deploy/ci_test.go` (gói test, không xuất ký hiệu).
- Gói phụ thuộc ngược (go list): không có (`tests/deploy` không bị gói nào nhập). Biến `MUTATION_*` chỉ dùng trong
  `Makefile` (grep). `HIGH_RISK_DIRS` đọc ở `lib.sh`, `affected.py`, `test-guard.sh`, `stop-check.sh` của hook: không đổi.
  Lịch CI `mutation` gọi `make mutation`: lệnh không đổi.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -race ./tests/deploy/ -run TestMakeMutation` (chỉ `make -n`, không chạy gremlins thật), cả gói
  `tests/deploy`, rồi `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có mã Go, không có gì
  cho danh sách thư mục của Makefile; grep `HIGH_RISK_DIRS` cả repo: nguồn duy nhất đã có là dòng `HIGH_RISK_DIRS=` trong
  `scripts/agent/lib.sh`.
- Dùng lại / viết mới vì: dùng lại chính dòng đó làm nguồn: Makefile đọc nó bằng `$(shell sed …)`, không chép danh sách.
  Không phải sửa `lib.sh` (bị khóa với agent) nên không cần tệp danh sách mới. Hàm `makeDry` của `tests/deploy` dùng lại
  cho test mới.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.10: không áp dụng: chỉ đổi đích Makefile của mutation testing, không chạm dữ liệu, module, HTTP, database.
- 5.11: không thêm công nghệ; gremlins đã có (ADR-44, Task 23); `sed` có sẵn trên macOS và máy CI.
- 5.12: theo mẫu Task 23 (đích Makefile + test `make -n` trong `tests/deploy`).
- 6.1–6.10, 6.12, 6.13: không áp dụng: không chạm database, hàng đợi, tiền, tồn, quyền, dữ liệu cá nhân.
- 6.11: liên quan gián tiếp: ADR-25 đòi vùng rủi ro cao được kiểm mạnh; task đưa `platform/db`, `platform/webhooks`,
  `modules/inventory` vào mutation testing, ngưỡng 80% giữ nguyên ở `.gremlins.yaml`. Đọc ra danh sách rỗng thì
  `make mutation` báo lỗi, không "qua" vì không đột biến gì.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `make mutation` phủ đủ `HIGH_RISK_DIRS` (thêm `platform/db`, `platform/webhooks`, `modules/inventory`) | `TestMakeMutation_CoversHighRiskDirs` | `go test -race -count=1 ./tests/deploy/ -run TestMakeMutation -v` | trước khi sửa: `FAIL … không đột biến internal/platform/db` (và webhooks, inventory); sau: `--- PASS` |
| Một nguồn: Makefile không chép mục nào của `HIGH_RISK_DIRS` | `TestMakeMutation_SingleSourceAndFileEntries` | như trên | trước: `FAIL … Makefile chép lại mục internal/platform/tenancy …` (6 mục); sau: `--- PASS` |
| Mục là tệp lẻ (`guard.go`): loại mọi tệp `.go` khác và thư mục con của gói, không loại chính nó | `TestMakeMutation_SingleSourceAndFileEntries` | thử phá tạm Makefile (bỏ `-E '/'`, bỏ loại `server.go`) rồi chạy lại | `FAIL … không loại internal/httpapi/gen/x.go`, `… không loại internal/httpapi/server.go`; trả Makefile về: `ok` |
| Ngưỡng 80% giữ nguyên, không chạy mutation thật trong test | `TestMakeMutation_CoversHighRiskDirs` (kiểm `--config .gremlins.yaml`) | `make -n mutation` | `.gremlins.yaml` không đổi `efficacy: 80`; test chỉ dùng `make -n` |
| Cổng kiểm của task (sau khi gộp `main` 789202f) | `tests/deploy`, `tests/arch` | `make build vet lint gen-check sqlc-vet catalog-check jscpd gitleaks oasdiff test-guard` (từng đích); `go test -race -count=1 ./tests/deploy/... ./tests/arch/...` | mọi đích `rc=0`; `ok elimaz.com.vn/crm/tests/deploy 5.015s`, `ok elimaz.com.vn/crm/tests/arch 18.539s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Cổng kiểm: CLAUDE.md (gộp từ main) đổi sang `scripts/agent/ci-task.sh` thay `make ci`, nhưng hook Bash chặn agent chạy
  tệp đó. Đã chạy đúng các bước của nó qua đích `make` tương ứng và test các gói bị ảnh hưởng (`tests/deploy`, không gói
  Go nào nhập; cộng `tests/arch`). Không có vuln/console vì không đổi go.mod, console. Lần `make ci` trước khi gộp main:
  mọi bước qua tới `oasdiff`, đỏ ở đó do main đã có Task 323 mà nhánh chưa gộp; sau khi gộp thì `oasdiff` qua.
- Lỗi "parallel golangci-lint is running" (phiên khác chạy cùng lúc): chạy lại đến khi rảnh, không đổi cấu hình.

## Quyết định thay chủ shop

- Nguồn danh sách: Makefile đọc thẳng dòng `HIGH_RISK_DIRS="…"` của `scripts/agent/lib.sh` (bằng `sed`), không tạo tệp
  danh sách mới. Vì sao: lib.sh đã là nguồn của hook, đọc từ đó không phải sửa lib.sh (bị khóa với agent), không có hai
  nơi để lệch. Phương án khác: tệp `tools/high-risk-dirs.txt` cho cả hai bên đọc — cần điều phối viên sửa lib.sh, thêm
  một bước; không cần. Đảo ngược dễ: đổi một dòng `MUTATION_HIGH_RISK` trong Makefile.
- Mục là tệp lẻ (`guard.go`): danh sách tệp bị loại tính lúc chạy từ các tệp `.go` trong gói (trước đây là danh sách tên
  viết tay, tệp handler mới thêm có thể lọt vào lượt đột biến). Phương án khác: giữ danh sách viết tay — dễ lệch.

## Việc phát hiện thêm

- Hook `guard-bash.sh` chặn agent chạy `scripts/agent/ci-task.sh` dù CLAUDE.md bảo phiên làm task chạy nó; cần điều phối
  viên cho phép lệnh này (tệp hook bị khóa với agent).
- Ba gói mới vào mutation (`platform/db`, `platform/webhooks`, `modules/inventory`) chưa đo lần nào; lượt CI theo lịch
  hoặc một lần `make mutation` sẽ cho biết có đạt 80% không.

## Reviewer

Kết luận: ĐẠT (một vòng, không có phát hiện phải sửa).

- Đã kiểm: `sed` và `$$` trong Makefile đọc đúng chín mục; mẫu `-E` khớp cách gremlins v0.6.0 so đường dẫn (tương đối
  tính từ gói, `internal/engine/engine.go`, `internal/exclusion/rules.go`); danh sách rỗng thì `make mutation` dừng; tên
  JSON không trùng; ngưỡng 80% giữ nguyên; test bắt được thiếu thư mục, chép danh sách, thiếu mẫu loại trừ.
- Ghi chú không bắt buộc (không sửa): mất `$$` cuối mẫu thì test không bắt (hậu quả nhỏ); thiếu `lib.sh` thì sed in lỗi
  ra stderr ở mọi lần gọi make nhưng không hỏng đích nào; ba gói mới chưa đo (đã ghi ở Việc phát hiện thêm).
- Reviewer bị hook chặn chạy `go test`/`make -n`; hai lệnh này người làm đã chạy (bảng bằng chứng).
