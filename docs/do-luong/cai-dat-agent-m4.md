# Đo cách cài đặt agent cuối M4 (Task 182, M4-42 Step 5)

Ngày đo: 28/11/2026, trên nhánh `task-182` (= `main` sau khi gộp M4 task 141–180, kèm phần M5, M6 đã gộp song song),
theo mục 12 của `docs/quy-trinh-lam-viec.md` và cùng lệnh với `docs/do-luong/cai-dat-agent-m2.md`. M3 chưa có tệp đo
riêng; ô "—" là chưa đo. Giải thích từ tiếng Anh (agent, hook, lint, jscpd, mutation, reviewer…): xem đầu tệp đo M0.

Khác M2: máy Mac 16 GB không chạy test nặng (chủ shop 27/11), nên mutation testing chạy trên GitHub Actions bằng bộ chạy
tạm (mục 2) và jscpd lấy từ cổng tĩnh của `scripts/agent/ci-task.sh` ở bể từ xa.

## 1. Tóm tắt, so với M2

| Chỉ số | M2 (19/11) | M4 (28/11) | Lệnh |
|---|---|---|---|
| Cỡ mã M4 | — | 42.810 dòng Go (bỏ mã sinh) trong `channel`, `conversation`, `adapters/{facebook,fake}`, trong đó 21.216 dòng test | `git ls-files … \| xargs wc -l` |
| Mã trùng (jscpd) | 0 đoạn | 0 đoạn mới (cổng `jscpd` của ci-task xanh với mốc nền của repo) | `make jscpd` (trong ci-task) |
| Điểm mutation vùng cao | thấp nhất 86,84% | xem mục 2 (lượt đầu và sau test thêm) | gremlins (mục 2) |
| `//nolint` (bỏ mã sinh) | 1.273 (758 test) | **3.825** (2.037 test); 534 trong `channel`, `conversation`, `adapters/facebook`; **0** chỗ không ghi lý do | mục 3 |
| Task M4 có tệp task | 34 (M2) | 39 tệp (141–143, 145–180; 144 để trống) | `ls docs/tasks/task-1{4..8}?.md` |
| Kết luận reviewer | — | 37/39 ĐẠT (142 chỉ tài liệu, không cần reviewer; 180 ĐẠT từng commit) | mục `## Reviewer` của từng tệp |
| Review độc lập cuối mốc | 0 nặng, 7 lớn, 17 nhỏ | **0 nặng, 2 trung bình (đã sửa), 4 thấp** | `docs/nghiem-thu/m4.md` |
| Độ dài `CLAUDE.md` | 115 dòng | **129 dòng** (giới hạn 200) | `wc -l CLAUDE.md` |

## 2. Mutation testing vùng cao M4

Vùng (kế hoạch M4, "Vùng rủi ro cao"): `internal/modules/channel`, `internal/adapters/facebook/{signature,policy,send,
download}.go`, `internal/modules/conversation/internal/{inbound,send,automation,live,access}`. Cách chạy, kết quả từng
gói và lý do mỗi đột biến còn sống: `docs/mutation.md`, mục "Lần chạy M4".

## 3. `//nolint`

Lệnh như M2: `git grep -n "//nolint" -- '*.go' ':!internal/httpapi/gen/**' ':!**/internal/store/**'
':!internal/httpapi/routes_gen.go' | wc -l` → 3.825; chỉ `*_test.go` → 2.037; ba vùng M4 → 534. Chỗ không ghi lý do:
`git grep -nE "//nolint:[a-zA-Z0-9,]+ *$" -- '*.go'` → 0. Số tăng theo cỡ mã (M3–M6 cùng gộp từ 19/11).

## 4. Nhận xét

- Phần lớn đột biến còn sống của M4 nằm ở nhánh lỗi database và ở hành vi mà test của **gói khác** kiểm (gremlins chỉ
  chạy test của chính gói bị đột biến): ví dụ cờ `IsHistory` của `inbound/hooks.go` do `automation` kiểm
  (`TestGreeting_ReadOnlyAndHistory`). Đo lại bằng chế độ `--integration` tốn vài giờ máy mỗi gói; chưa làm.
- Review độc lập tìm được 2 chỗ lệch spec (04 mục 5.3, 02 mục 2) mà reviewer theo task không thấy: cả hai là chính sách
  chung nằm giữa hai task (Task 157 với 166; Task 156 với 158). Bài học ghi vào "Việc phát hiện thêm" của Task 182.
