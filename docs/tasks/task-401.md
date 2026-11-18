# Task 401 — main đỏ: test mutation bắt nhầm đích perf-test (M1-x6)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 401, M1-x6). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi tệp test `tests/deploy/ci_test.go` (gói `deploy_test`): sửa `TestMakeMutation_SingleSourceAndFileEntries`, thêm hàm không xuất `mutationSource` và test `TestMutationSource_OnlyMutationRecipeAndItsVars`.
- Gói phụ thuộc ngược (go list): không có (gói `_test`, không ai nhập).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -count=1 ./tests/deploy/...` (cả gói, nơi lỗi xảy ra); `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ liệt kê gói `internal/platform`, không có bộ đọc Makefile; trong `tests/deploy` có `makeDry`, `highRiskDirs`, `checkSingleFileMutation` (dùng lại), không có hàm tách công thức của một đích.
- Dùng lại / viết mới vì: viết mới `mutationSource` (tách công thức đích `mutation` và định nghĩa các biến/hàm make nó dùng, bắc cầu) vì chưa có và chỉ test này cần.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.10: không áp dụng: task chỉ sửa test về Makefile, không chạm dữ liệu, module, HTTP, gọi ngoài.
- 5.11: không thêm thư viện (chỉ `regexp`, `strings` chuẩn).
- 5.12: theo mẫu test sẵn có trong `tests/deploy/ci_test.go` (test bảng cho các ca âm/dương).
- 6.1–6.13: không áp dụng: không database, hàng đợi, tiền, tồn, quyền, dữ liệu cá nhân.
- Ý định Task 392 giữ nguyên, không nới: danh sách vùng cao của `make mutation` chỉ đọc từ `scripts/agent/lib.sh`. Phạm vi kiểm đổi từ "cả Makefile" sang "công thức đích `mutation` + mọi biến/hàm nó dùng (bắc cầu)"; thêm kiểm dương: phần đó phải đọc `scripts/agent/lib.sh`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi sửa: main đỏ đúng như kế hoạch | `TestMakeMutation_SingleSourceAndFileEntries` | `go test -count=1 -run TestMakeMutation_SingleSource ./tests/deploy/` | `ci_test.go:331: Makefile chép lại mục internal/platform/queue của HIGH_RISK_DIRS` → FAIL |
| `go test ./tests/deploy/...` xanh trên bản đã sửa | cả gói | `go test -race -count=1 ./tests/deploy/...` | `ok elimaz.com.vn/crm/tests/deploy 3.892s` |
| Ca dương: đích khác (perf-test) nhắc gói → xanh; biến chỉ đích khác dùng không tính; dòng sau công thức mutation không thuộc đích | `TestMutationSource_OnlyMutationRecipeAndItsVars/{đích_khác_nhắc_gói:_hợp_lệ, biến_không_ai_dùng_trong_mutation:_không_tính, dòng_sau_công_thức_mutation_không_thuộc_đích}` | `go test -count=1 -v -run TestMutationSource ./tests/deploy/` | 7/7 ca PASS |
| Ca âm (mẫu): chép danh sách vào công thức mutation, vào biến mutation dùng (bắc cầu), vào hàm `$(call …)`, vào dòng tiếp nối `\` → bị bắt | `TestMutationSource_OnlyMutationRecipeAndItsVars/{chép_danh_sách_vào_công_thức_mutation, chép_danh_sách_vào_biến_mutation_dùng_(bắc_cầu), chép_danh_sách_vào_hàm_make_mutation_gọi, chép_vào_dòng_tiếp_nối_của_biến}` | như trên | PASS (trả đúng mục bị chép) |
| Ca âm trên Makefile thật: thử đổi `for d in $(MUTATION_DIRS)` thành `for d in internal/platform/queue internal/platform/db` → đỏ; rồi `git checkout Makefile` | `TestMakeMutation_SingleSourceAndFileEntries` | `sed -i '' … Makefile && go test -count=1 -run TestMakeMutation_SingleSource ./tests/deploy/; git checkout Makefile` | `ci_test.go:334: Makefile chép lại mục internal/platform/queue của HIGH_RISK_DIRS cho đích mutation` → FAIL (đúng ý); Makefile trả về nguyên trạng |
| Không nới "một nguồn": thêm kiểm dương phần mutation phải đọc `scripts/agent/lib.sh`; giữ nguyên kiểm tệp lẻ `guard.go` | `TestMakeMutation_SingleSourceAndFileEntries`, `TestMakeMutation_CoversHighRiskDirs` | `go test -count=1 -run TestMakeMutation ./tests/deploy/` | PASS |
| Cổng tĩnh của task | — | `scripts/agent/ci-task.sh` (một lần) | `CI-TASK XANH (so với điểm tách nhánh 99518a1…)`; build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh. Lưu ý: bước test chỉ chạy `tests/arch` (xem "Dừng để hỏi"), nên `tests/deploy` chạy tay ở dòng 2 |
| ci-task.sh chạy tests/deploy khi Makefile đổi | — | — | CHƯA LÀM: hook chặn sửa `scripts/agent/` (xem "Dừng để hỏi") |

## Test bị xóa hoặc tắt

(không có). `TestMakeMutation_SingleSourceAndFileEntries` đổi phạm vi kiểm từ cả Makefile sang phần quyết định đích `mutation` (đúng việc kế hoạch Task 401 giao), có thêm kiểm dương đọc từ `scripts/agent/lib.sh`.

## Khác kế hoạch

- Phần (2) "thêm luật vào `ci-task.sh`, có test" chưa làm: `scripts/agent/*` là cấu hình quy trình, hook `guard-edit.sh`/`guard-bash.sh` chặn và CLAUDE.md cấm tìm cách vượt hook. Bản vá đề xuất ở "Dừng để hỏi".

## Quyết định thay chủ shop

- Phạm vi kiểm "một nguồn": chọn "dòng khai đích `mutation` + công thức (dòng bắt đầu bằng tab ngay sau) + định nghĩa mọi biến/hàm make được nhắc `$(X)`/`${X}`/`$(call X,…)`, bắc cầu; nối dòng `\`". Vì sao: đúng câu kế hoạch "kiểm trong công thức của đích mutation và các biến nó dùng", không nới (biến trung gian chép danh sách vẫn bị bắt). Phương án khác: chạy `make -p` lấy cơ sở dữ liệu biến đã mở rộng — bỏ, vì sau mở rộng danh sách đọc từ lib.sh trông y như danh sách chép, không phân biệt được.
- Phần (2): không sửa `scripts/agent/`, ghi bản vá đề xuất cho chủ shop (dễ đảo ngược, không vượt hook).

## Dừng để hỏi

Cần chủ shop (hoặc phiên được phép) sửa `scripts/agent/` — hook chặn phiên task:

1. **Lỗi lớn hơn kế hoạch biết:** `scripts/agent/affected.py` không khớp được gói nào trên máy này. `git rev-parse --show-toplevel` trả đường dẫn "Thời trang" dạng Unicode NFD, `go list` trả dạng NFC, nên `os.path.relpath` ra `../../../../../Thời trang/…` và `by_dir` không bao giờ trùng → `ci-task.sh` của **mọi** task chỉ chạy `tests/arch`, không chạy test gói đổi (kiểm: `relpath = ../../../../../Thời trang/apps/crm/.worktrees/task-401/tests/deploy`, `is_normalized('NFC', git) = False`, `go = True`). Sửa: trong `affected.py` chuẩn hóa cả hai về NFC, ví dụ `root = unicodedata.normalize("NFC", root)` và `os.path.relpath(unicodedata.normalize("NFC", p["Dir"]), root)`.
2. **Makefile không kích test nào:** `affected.py` chỉ xét tệp `.go`, `go.mod`, `go.sum`. Đề xuất thêm vào `ci-task.sh`, sau khối test:
   `if printf '%s\n' "$changed" | grep -E -q '^(Makefile|tools/versions\.mk|scripts/agent/|\.github/)'; then step "test deploy (đổi Makefile/scripts/agent)" go test -count=1 ./tests/deploy/...; fi`
   kèm test (ví dụ trong `tests/deploy`: chạy `ci-task.sh` không được vì nặng; đề xuất tách luật chọn test thành hàm đọc stdin trong `affected.py` và test nó bằng danh sách tệp mẫu: `Makefile` → có `./tests/deploy/...`).

## Việc phát hiện thêm

- Lỗi NFD/NFC ở `affected.py` (mục 1 "Dừng để hỏi"): các task đã gộp từ 18/11 có thể đã qua ci-task mà không chạy test gói đổi; main vẫn được điều phối viên chạy toàn bộ sau mỗi đợt gộp nên lỗi lọt sẽ hiện ở đó (như Task 401 này).

## Reviewer

Kết luận: ĐẠT (lần 1). Phát hiện phải sửa: không có. Reviewer ghi nhận phần (2) chưa làm vì hook chặn `scripts/agent/`, đã ghi đúng ở "Khác kế hoạch"/"Dừng để hỏi"; task chưa trọn theo "Xong khi" cho tới khi chủ shop quyết phần (2) và lỗi NFD/NFC của `affected.py` (nên báo sớm). Ghi chú không bắt buộc (không sửa, Makefile hiện không dùng): `mutationSource` không bắt `define…endef`, `export X =`, `X != …`, `include`, đích là prerequisite của `mutation`; `makeRefRe` khớp nhầm `$$(basename` của shell, vô hại.
