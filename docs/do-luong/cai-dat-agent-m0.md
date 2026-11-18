# Đo cách cài đặt agent cuối M0 (Task 23 Step 6b)

Ngày đo: 18/11/2026, trên nhánh `task-23` (commit `4b752d1`). Đây là **mốc so sánh cho M1**: cuối M1 đo lại đúng các
lệnh dưới đây rồi đặt hai cột cạnh nhau.

Lưu ý quan trọng: M0 và M1 chạy xen nhau, nên mã ở `4b752d1` đã có cả mã M1 (catalog, inventory, màn hình…). Các số
đo trên mã (jscpd, `//nolint`) là của **toàn bộ repo** tại ngày đo, không tách riêng được phần M0. Các số đo theo task
(reviewer, hook) chỉ tính task M0.

Giải thích từ tiếng Anh dùng trong tệp (một lần):
- **agent**: Claude chạy như một người làm việc tự động, tự đọc, viết và chạy lệnh.
- **hook**: đoạn kiểm tự chạy ở một thời điểm cố định; **hook Stop** chạy khi agent định báo "xong" và chặn nếu
  build, test, lint chưa qua.
- **lint**: công cụ đọc mã tìm lỗi hay gặp; `//nolint` là dòng chú thích tắt lint ở đúng một chỗ.
- **jscpd**, **dupl**: công cụ dò đoạn mã chép trùng. **baseline** (mốc nền): danh sách đoạn trùng cũ được chấp nhận.
- **mutation** (đột biến): cố ý sửa sai mã rồi xem test có đỏ không; điểm càng cao thì test càng bắt được lỗi thật.
- **reviewer**: agent chỉ đọc, soát phần mã sắp commit. **vòng/lượt review**: một lần reviewer soát.
- **CLAUDE.md**: tệp luật agent đọc đầu mỗi phiên. **LSP/gopls**: công cụ tra mã Go (ví dụ "hàm này được gọi ở đâu").

## 1. Tóm tắt số đo

| Chỉ số | Số đo 18/11 | Lệnh |
|---|---|---|
| Mã trùng (jscpd) | **0 đoạn trùng, 0 dòng (0,00%)** trên 444 tệp, 76.813 dòng (Go 396 tệp/68.460 dòng; TS 28/4.457; TSX 20/3.896) | `jscpd --config .jscpd.json --reporters console .` |
| Mốc nền jscpd | `jscpd-baseline.json` rỗng (`"fingerprints": {}`): không có đoạn trùng nào được "cho qua" | `cat jscpd-baseline.json` |
| Điểm mutation vùng cao | lần đầu: 2/6 gói dưới 80% (queue 77,6%, commands 79,4%); sau test thêm: thấp nhất 82,4% (queue) | `make mutation` (mục 2) |
| `//nolint` (bỏ mã sinh) | **646 chỗ**: 346 trong test, 300 trong mã chạy thật; 645/646 có lý do (1 chỗ không lý do là tệp mẫu cố ý sai `tests/arch/testdata/lint/bad.go`) | mục 3 |
| Phát hiện của reviewer phải sửa | **17**, nằm ở 10/30 task M0 có review; reviewer sai **1** lần | mục 4 |
| Số vòng review | **61 vòng / 30 task** (trung bình 2,0); 17/30 task cần hơn 1 vòng | mục 4 |
| Hook Stop chặn | **Không có nhật ký**; đếm được 4 lần có ghi lại trong tệp task (M0) | mục 5 |
| Chủ shop sửa agent | **Không có nhật ký**; mục "Bẫy đã gặp" của CLAUDE.md vẫn "(chưa có)" | mục 6 |
| Vá công cụ quy trình | **12 commit** sửa `scripts/agent/` và `.githooks` trong 15–18/11 | `git log --format='%h %ad %s' --date=short -- scripts/agent .githooks` |
| Độ dài `CLAUDE.md` | **99 dòng** (giới hạn 200) + 4 tệp luật theo vùng 64 dòng | `wc -l CLAUDE.md`, `wc -l .claude/rules/*.md` |

## 2. Điểm mutation

Lần chạy 18/11/2026 (`make mutation`, gremlins v0.6.0; chi tiết và lý do từng đột biến còn sống: `docs/mutation.md`):

| Gói | Lần đầu | Sau test thêm |
|---|---|---|
| tenancy | 100% (12/12) | 100% |
| queue | 77,6% (66/85) | 82,4% (70/85) |
| commands | 79,4% (27/34) | 88,2% (30/34) |
| idempotency | 100% (15/15) | 100% |
| identity | 89,8% (106/118) | 91,5% (108/118) |
| guard.go | 85,7% (6/7) | 100% (7/7) |

Hai đột biến còn sống có ý nghĩa an ninh (guard.go: route đọc có quyền thành "chỉ cần đăng nhập"; staff.go: người bị
gỡ quyền giữa chừng vẫn sửa được nhân viên) — review mỗi task không bắt được; nay có test. Số "không phủ" của queue (60)
lớn: nhánh lỗi database và vòng nền chưa có test mức gói.

(Vùng cao theo CLAUDE.md: `internal/platform/{tenancy,queue,commands,idempotency}`, `internal/modules/identity`,
`internal/httpapi/guard.go`; cấu hình công cụ ở `.gremlins.yaml`.)

## 3. `//nolint` và lý do

Lệnh: `git grep -n "//nolint" -- '*.go' ':!internal/httpapi/gen/**' ':!**/internal/store/**' ':!internal/httpapi/routes_gen.go'`,
rồi đếm theo tên linter sau `//nolint:` và gom phần lý do sau `//`. Thư mục `spikes/` có 0 chỗ.

| Linter (việc nó kiểm) | Số chỗ | Lý do hay gặp |
|---|---|---|
| `wrapcheck` (lỗi trả ra phải được bọc thêm ngữ cảnh) | 371 | 256 chỗ ghi "test"; còn lại "sentinel của <module>" / "đã bọc trong <gói>" (lỗi đã bọc một lần ở dưới, đúng luật CLAUDE.md) |
| `forcetypeassert` (ép kiểu không kiểm) | 81 | 57 "test"; 24 "fn trên trả *gen.X" ở handler HTTP |
| `ireturn` (hàm trả interface) | 77 | 58 "chữ ký do ogen sinh (gen.Handler)": bắt buộc theo mã sinh; còn lại Runner, adapter theo hãng |
| `gosec` (an toàn) | 60 | đường dẫn tệp do công cụ dev/đo đưa vào, "đã chặn trên" |
| `gochecknoglobals` (biến toàn cục) | 19 | Interfaces kế hoạch khai là `var`, chỉ đọc |
| `forbidigo` (hàm bị cấm, ví dụ `os.Exit`, đọc env) | 12 | `main`, testkit đọc biến môi trường test |
| `err113` (lỗi phải là sentinel) | 9 | lỗi cấu hình một lần trong `tools/` |
| khác (`errorlint` 6, `errchkjson` 5, `noctx` 4, `contextcheck` 2, `spancheck`, `nilnil`, `nilerr`, `containedctx` mỗi cái 1) | 21 | so đúng `http.ErrAbortHandler`, JSON chỉ chuỗi |

Nhận xét: `nolintlint` (bắt ghi lý do) chạy đúng: 0 chỗ thiếu lý do ngoài tệp mẫu. Nhưng 369 chỗ (57%) là ba loại
"test", "chữ ký ogen" và "fn trên trả *gen.X": tắt lặp lại theo khuôn, nên tắt theo vùng trong `.golangci.yml` thay vì
từng dòng (đề xuất 5).

## 4. Phát hiện của reviewer theo task (M0)

Nguồn: mục `## Reviewer` của từng `docs/tasks/task-*.md` (đọc tay). Cột: **Phải sửa** = phát hiện đúng, ảnh hưởng tính
đúng, đã sửa; **Sai** = reviewer nói sai; **Câu chữ** = phong cách, câu chữ trong tài liệu/tệp task (reviewer được dặn
bỏ qua phong cách, nên rất ít); **Ghi chú** = ghi chú không bắt buộc (đã làm + để lại). Số ghi chú là đếm gần đúng
(≈), vì tệp task gộp nhiều ý trong một gạch đầu dòng.

| Task | Vòng | Phải sửa | Sai | Câu chữ | Ghi chú (≈) | Ghi chú chính |
|---|---|---|---|---|---|---|
| 00 | 0 | – | – | – | – | không có mã, chưa dùng reviewer |
| 01 | 2 | 2 | 0 | 0 | 4 | thiếu chạy lại ADR-46; thiếu số đo |
| 02 | 1 | 0 | 0 | 0 | 3 | |
| 03 | 1 | 0 | 0 | 2 | 4 | sót tên trong Ảnh hưởng, câu sai về `go.mod` |
| 04 | 3 | 2 | 0 | 0 | 9 | `sqlusage` sót bảng; báo nhầm `FOR UPDATE` |
| 05 | 2 | 0 | 1 | 0 | 5 | reviewer nói Ảnh hưởng thiếu `migrate_test.go` — thực ra đã có |
| 06 | 2 | 0 | 0 | 2 | 6 | test `TestServerRegistersEveryRoute` không kiểm gì (đã sửa) |
| 07 | 1 | 0 | 0 | 0 | 4 | |
| 08 | 1 | 0 | 0 | 0 | 3 | |
| 09 | 1 | 0 | 0 | 1 | 4 | |
| 10 | 2 | 1 | 0 | 0 | 2 | api treo khi cổng bị chiếm |
| 11 | 3 | 1 | 0 | 1 | 2 | hạn ghi tạo trước handler |
| 12 | 3 | 1 | 0 | 1 | 6 | cùng lỗi hạn ghi như Task 11, ở `commands` |
| 13 | 1 | 0 | 0 | 0 | 3 | |
| 14 | 1 | 0 | 0 | 0 | 5 | |
| 15 | 2 | 0 | 0 | 2 | 4 | |
| 16 | 3 | 2 | 0 | 0 | 6 | `Ping` không hạn; thiếu dòng danh mục |
| 17 | 1 | 0 | 0 | 0 | 5 | lệch quy trình (bí danh TypeScript 6) — chờ chủ shop xác nhận |
| 18 | 1 | 0 | 0 | 0 | 2 | |
| 19 | 7 | 3 | 0 | 0 | 8 | mất cảnh báo `in_doubt`, `dead`, `new-error`; 4 vòng do gộp main |
| 20 | 3 | 1 | 0 | 1 | 6 | ghi lỗi trước trạng thái việc |
| 21 | 3 | 2 | 0 | 0 | 8 | ổ đầy; SIGQUIT nhầm cụm thật |
| 22 | 2 | 0 | 0 | 0 | 5 | |
| 314 | 1 | 0 | 0 | 0 | 4 | |
| 317 | 2 | 0 | 0 | 1 | 2 | |
| 318 | 1 | 0 | 0 | 0 | 1 | |
| 319 | 4 | 2 | 0 | 1 | 9 | lộ địa chỉ; ghi lỗi khi database tắt |
| 395 (M1-33b) | 2 | 0 | 0 | 0 | 5 | |
| 396 | 1 | 0 | 0 | 0 | 3 | |
| 397 | 2 | 0 | 0 | 1 | 1 | |
| 398 (M1-x1) | 1 | 0 | 0 | 1 | 0 | |
| 399 | 1 | 0 | 0 | 1 | 0 | test-guard báo nhầm 2 lần (lỗi của công cụ, không của reviewer) |
| **Tổng** | **61** | **17** | **1** | **16** | **≈130** | |

- Task 394 không có tệp task (không đo được). Task 395, 398 mang nhãn M1 nhưng được tính vì nằm trong danh sách giao.
- **Bỏ qua M1** (task 24–54, 315, 320, 324, 333, 334): chưa phân loại ở đây, để đo cuối M1 theo cùng bảng.
- 17 lỗi phải sửa đều là lỗi chạy đồng thời/hạn giờ/lộ dữ liệu, loại test thường không bắt; 2 lần cùng một lỗi (hạn ghi
  tạo trước khi làm việc: Task 11 rồi Task 12) — đủ điều kiện ghi vào "Bẫy đã gặp" nhưng chưa ghi.
- 27/61 vòng (44%) là vòng thêm vì gộp main hoặc sửa sau khi đã ĐẠT, không phải vì phát hiện mới.
- Mọi task M0 đều có nội dung ở "Khác kế hoạch" (31/31) và tổng 135 dòng "Việc phát hiện thêm"; 3 task có mục "Test bị
  xóa hoặc tắt" khác "không có".

## 5. Hook Stop chặn bao nhiêu lần

**Không có nhật ký.** `scripts/agent/stop-check.sh` chỉ in lỗi và thoát mã 2, không ghi lại lần chặn; tệp task không có
ô để ghi. Lệnh tìm: `grep -n -i "hook stop\|stop chặn\|hook chặn" docs/tasks/task-*.md` và
`git log --all --format='%h %s' | grep -i "stop\|hook"`. Đếm được những lần có ghi:

| Task | Việc | Loại |
|---|---|---|
| 03 | hook Stop gọi jscpd thiếu `--similarity`, dò kém hơn `make ci` | lỗi của hook |
| 04 | hook Stop chặn sai ở bước lint (đưa tên gói thay vì thư mục) → sửa ở `ca856c9` | lỗi của hook (chặn nhầm) |
| 19 | hook Stop và `make ci` cùng chạy golangci-lint làm test lint đỏ | chạy chồng |
| 319 | hook Stop chặn đoạn trùng 14 dòng giữa hai `recoverPanic` → gộp | chặn đúng |

(M1: Task 38 ghi `make ci` đỏ vì chạy chồng với hook Stop.) Không có task nào ghi tới ngưỡng 8 lần liền.
**Đề xuất cho M1:** hook Stop ghi thêm một dòng `ngày giờ, task, mục không qua` vào tệp nhật ký ngoài git
(ví dụ `.git/crm-stop.log`), `finish-task.sh` đếm và chép số lần vào tệp task. Việc này sửa `scripts/agent/`, chỉ chủ
shop đổi được.

## 6. Chủ shop phải sửa agent bao nhiêu lần

**Không có nhật ký.** "Bẫy đã gặp" trong `CLAUDE.md`: "(chưa có)" (`grep -n "Bẫy đã gặp" -A3 CLAUDE.md`). Tệp task M0
không có ô ghi; `git log --format='%h %s' | grep -i "chủ shop"` chỉ thấy các lần chủ shop **chốt** cách làm, không
thấy lần sửa: đóng gói quy trình (Task 0, sau đó gọn lại ở `9399d97`), cấm `git stash` trong worktree (`371a9d3`),
bộ Docker chung (`04614f9`). Đếm được gián tiếp: 12 commit vá công cụ quy trình trong 4 ngày (mục 1).
**Đề xuất cho M1:** thêm mục "Chủ shop sửa" (một dòng mỗi lần: sửa gì) vào mẫu `docs/tasks/_mau.md`.

## 7. Độ dài CLAUDE.md

| Tệp | Dòng |
|---|---|
| `CLAUDE.md` | 99 / 200 |
| `.claude/rules/console.md` · `go-backend.md` · `migrations.md` · `openapi.md` | 13 · 22 · 15 · 14 (tổng 64) |
| `docs/quy-trinh-lam-viec.md` (quy trình đầy đủ, đọc khi cần) | 270 |

## 8. 17 đề xuất: giữ, chỉnh hay bỏ

| # | Đề xuất | Khuyên | Số đo | Vì sao | Chủ shop quyết |
|---|---|---|---|---|---|
| 1 | CLAUDE.md viết tay < 200 dòng | Giữ | 99 dòng | Còn nửa chỗ trống, chưa phình. | |
| 2 | Đoạn phạm vi ở CLAUDE.md gốc | Giữ | 0 lần ghi nhận luật website lọt vào phiên CRM (không đủ dữ liệu để nói hơn) | Không thấy hại, không tốn gì. | |
| 3 | Bốn tệp luật theo vùng | Chỉnh | 64 dòng; Task 318 muốn thêm luật migration nhưng hook chặn | Giữ bốn tệp; thêm đường cho agent đề xuất luật mới qua "Việc phát hiện thêm" để chủ shop chép. | |
| 4 | Hook format, chặn mã sinh/migration, Stop | Chỉnh | 12 commit vá hook trong 4 ngày; 2 lần hook Stop chặn nhầm/kém (Task 3, 4); 2 lần chạy chồng `make ci` | Hook có ích (Task 319 chặn đúng) nhưng cần ghi nhật ký lần chặn và tránh chạy chồng. | |
| 5 | `.golangci.yml` rõ, `nolintlint` bắt lý do | Chỉnh | 646 `//nolint`, 645 có lý do; 369 (57%) lặp khuôn "test"/ogen | Giữ `nolintlint`; tắt `wrapcheck`, `forcetypeassert` cho `_test.go` và `ireturn` cho handler ogen trong cấu hình. | |
| 6 | Test ranh giới, test SQL sớm | Giữ | 13 tệp test trong `tests/arch`; reviewer Task 4 bắt 2 lỗi của chính `sqlusage` | Test kiến trúc đang chạy thật, đã sửa được. | |
| 7 | Lint console chốt ngay, dừng hỏi nếu TS7 không chạy | Giữ | Task 17 dùng bí danh TypeScript 6 thay vì dừng hỏi | Luật đúng; chủ shop còn phải xác nhận Quyết định 1 của Task 17. | |
| 8 | oasdiff trong `make ci` | Giữ | Có trong cả 31 tệp task M0 (mục Ảnh hưởng); không đủ dữ liệu về lần chặn | Không tốn, giữ hợp đồng API ổn định cho M1. | |
| 9 | Mục Ảnh hưởng, Tái dùng mỗi task | Giữ | Reviewer bắt sai ở mục Ảnh hưởng ở 5 task (3, 9, 11, 15, 319) | Mục được đọc và soát thật. | |
| 10 | Reviewer chỉ đọc, một vòng duy nhất | Chỉnh | 61 vòng/30 task; 17/30 task > 1 vòng; 17 lỗi phải sửa, 1 sai | Giữ reviewer (bắt lỗi thật mà test không bắt); sửa chữ "một vòng" thành "tới khi ĐẠT, review lại sau mỗi lần gộp main". | |
| 11 | Danh mục dùng chung sinh tự động | Giữ | `catalog-check` bắt thiếu ghi chú ở Task 11, 16 | Cổng chạy đúng, rẻ. | |
| 12 | `dupl` + jscpd theo mốc nền | Giữ | 0 đoạn trùng, mốc nền rỗng; đã dọn trùng ở Task 16, 319, 397, 399 | Đang giữ repo sạch trùng. | |
| 13 | Báo test bị xóa, tắt, bớt kiểm | Giữ | Nhắc tới ở 23/31 tệp task; 2 báo nhầm (Task 399), đã sửa `f76f26e` | Còn báo nhầm nhưng đã vá. | |
| 14 | Module mẫu trong CLAUDE.md | Giữ | Không đủ dữ liệu (chưa đo số lần lệch mẫu) | M1 nhiều module mới, mẫu càng cần. | |
| 15 | Plugin `gopls-lsp`, `typescript-lsp` | Chỉnh | Không đủ dữ liệu: tệp task không ghi đã dùng LSP thật hay chưa (chữ "findReferences" có sẵn trong mẫu) | Ghi lệnh LSP đã chạy vào mục Ảnh hưởng để đo được ở M1. | |
| 16 | Skill Go đã đọc và ghim; ghim superpowers | Giữ | Không đủ dữ liệu; skill `crm-task`, agent `crm-implementer` đã bỏ ở `9399d97` | Không thấy hại. | |
| 17 | Đo cách cài đặt cuối M0 | Chỉnh | Hai chỉ số (hook Stop, chủ shop sửa) không có nhật ký | Đo tiếp cuối M1, nhưng ghi nhật ký tự động từ đầu M1 (mục 5, 6). | |
