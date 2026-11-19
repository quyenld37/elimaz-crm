# Đo cách cài đặt agent cuối M2 (Task 89, M2-32 Step 4)

Ngày đo: 19/11/2026, trên nhánh `task-89` (= main sau khi gộp M2 task 57–87, 90, 425, 427), theo mục 12 của
`docs/quy-trinh-lam-viec.md`, cùng lệnh với `docs/do-luong/cai-dat-agent-m0.md` để đặt cạnh nhau. M1 **chưa có tệp đo**
riêng (Task 56 của M1 tách thành 403, 404; không task nào viết `cai-dat-agent-m1.md`), nên cột M1 chỉ có số lấy được từ
tài liệu khác (`docs/review-doc-lap-m1.md`); ô "—" là chưa đo.

Như M0: số đo trên mã (jscpd, `//nolint`) là của **toàn bộ repo** tại ngày đo; số theo task chỉ tính task M2.
Giải thích từ tiếng Anh (agent, hook, lint, jscpd, mutation, reviewer…): xem đầu tệp đo M0.

## 1. Tóm tắt, so với M0 và M1

| Chỉ số | M0 (18/11) | M1 | M2 (19/11) | Lệnh |
|---|---|---|---|---|
| Mã trùng (jscpd) | 0 đoạn / 444 tệp, 76.813 dòng | — | **0 đoạn / 856 tệp, 156.184 dòng** (Go 644 tệp / 117.056 dòng; TSX 119 / 24.391; TS 93 / 14.737) | `jscpd --config .jscpd.json --reporters console .` |
| Mốc nền jscpd | rỗng | — | **rỗng** (`"fingerprints": {}`) | `cat jscpd-baseline.json` |
| Điểm mutation vùng cao | thấp nhất 82,4% (sau test thêm) | — | **thấp nhất 86,84%** (88,42% sau test thêm) | gremlins (mục 2) |
| `//nolint` (bỏ mã sinh) | 646 (346 test, 300 mã chạy) | — | **1.273** (758 test, 515 mã chạy); 274 trong `customer`, `order`, `promotion`; **0** chỗ không ghi lý do | mục 3 |
| Phát hiện reviewer phải sửa | 17 / 30 task | — | **14 / 34 task** (6 ở Task 83 mockup); reviewer sai **0** | mục 4 |
| Số vòng review | 61 / 30 task (2,0) | — | **58 / 34 task (1,7)**; 18/34 task > 1 vòng | mục 4 |
| Vòng thêm vì gộp main / sửa sau ĐẠT | 27/61 (44%) | — | **10/58 (17%)** (+5 vòng do tách commit cấu trúc/hành vi) | mục 4 |
| Hook Stop chặn (có ghi) | 4 | — | **4** (2 đúng, 2 nhầm do test chập chờn khi nhiều phiên dùng chung máy); không task nào tới ngưỡng 8 | mục 4 |
| Quyết định thay chủ shop | — | — | **250** quyết định / 34 task (≈7,4/task) | mục 4 |
| Review độc lập cuối mốc | `docs/do-luong/review-doc-lap-m0.md` | 0 nghiêm trọng, 9 lớn, 20 nhỏ, 4 nghi ngờ | **0 nghiêm trọng, 7 lớn, 17 nhỏ, 4 nghi ngờ** | `docs/review-doc-lap-m2.md` |
| Độ dài `CLAUDE.md` | 99 dòng | — | **115 dòng** sau Task 89 (giới hạn 200) + 4 tệp luật theo vùng 64 dòng | `wc -l CLAUDE.md` |

## 2. Điểm mutation vùng cao M2

Lệnh, chế độ chạy và lý do từng đột biến còn sống: `docs/mutation.md` mục "Lần chạy M2". Ngưỡng 80% (như M0).

| Vùng | Bị giết / còn sống / không phủ | Hiệu quả gremlins | Sau test thêm | Thời gian |
|---|---|---|---|---|
| `internal/modules/order/**` | 787 / 92 / 205 | 89,53% | 91,01% | 3 giờ 21 phút |
| `internal/modules/promotion/**` | 165 / 25 / 78 | 86,84% | 88,42% | 21 phút |
| `customer/{merge,fororder,stats}.go` | 81 / 9 / 19 | 90,00% | 90,00% | 25 phút |

Thấp nhất **86,84%** (lượt gremlins), 88,42% sau test thêm; M0 thấp nhất 77,65% lần đầu, 82,35% sau test thêm. Đột
biến có ý nghĩa tiền/an ninh bộ test cũ không bắt: nền tính mã giảm % (`create_coupon.go:38`) và điều kiện duyệt lại
mới của C-02 (`edit.go:484`, vì test viết sau lúc gremlins sao mã) — đều đã có test giết.

## 3. `//nolint`

Lệnh như M0: `git grep -n "//nolint" -- '*.go' ':!internal/httpapi/gen/**' ':!**/internal/store/**' ':!internal/httpapi/routes_gen.go' | wc -l`
→ 1.273; chỉ tệp `*_test.go` → 758; ba module M2 (`internal/modules/{customer,order,promotion}`, bỏ mã sinh) → 274.
Chỗ không ghi lý do (`//nolint:<tên>` đứng cuối dòng, không có `//` theo sau): `git grep -nE "//nolint:[a-zA-Z0-9,]+ *$"` → 0.
Số tăng gần gấp đôi so với M0 theo cỡ mã (dòng Go tăng ≈ 1,7 lần).

## 4. Phát hiện của reviewer theo task (M2)

Nguồn: mục `## Reviewer`, "Quyết định thay chủ shop", "Test bị xóa hoặc tắt" của `docs/tasks/task-NN.md`, NN ∈ 57–87, 90,
425, 427 (đọc tay, cùng cách phân loại M0). QĐ = số quyết định thay chủ shop.

| Task | Vòng | Phải sửa | Sai | Câu chữ | Ghi chú (≈) | QĐ | Ghi chú chính |
|---|---|---|---|---|---|---|---|
| 57 | 0 | – | – | – | – | 9 | chỉ tài liệu, không cần reviewer |
| 58 | 1 | 0 | 0 | 0 | 3 | 2 | |
| 59 | 2 | 1 | 0 | 0 | 5 | 12 | `ErrStatusConflict` → 422 giấu lỗi dữ liệu lệch (về 500 có log) |
| 60 | 2 | 0 | 0 | 0 | 2 | 6 | vòng 2 sau ĐẠT: cổng kiểm đỏ `TestHeadersStoredRedacted` |
| 61 | 2 | 2 | 0 | 0 | 2 | 3 | test so đúng danh sách tỉnh sẽ đỏ khi có dữ liệu thật; lệch Step 0 (tách phần A/B) chưa ghi rõ |
| 62 | 2 | 0 | 0 | 1 | 6 | 7 | 2 commit (cấu trúc + hành vi); Ảnh hưởng thiếu `cmd/crm/seed.go` |
| 63 | 1 | 0 | 0 | 1 | 2 | 8 | câu bảng bằng chứng "không đổi lượt" sai phạm vi |
| 64 | 2 | 0 | 0 | 1 | 4 | 4 | vòng 2 sau ĐẠT (sửa `ManualDiscountBP` theo ghi chú) |
| 65 | 1 | 0 | 0 | 0 | 3 | 6 | |
| 66 | 2 | 1 | 0 | 0 | 6 | 8 | `useExisting` khóa ngược ADR-46 nhóm 5 → deadlock với `AddIdentity` |
| 67 | 2 | 0 | 0 | 2 | 2 | 7 | chỉ tệp task: cổng kiểm chưa có kết quả, số ca 22≠23 |
| 68 | 1 | 0 | 0 | 0 | 2 | 3 | |
| 69 | 2 | 1 | 0 | 1 | 3 | 7 | `TestM2Routes_Unimplemented` gọi route vừa làm (404) |
| 70 | 2 | 0 | 0 | 0 | 7 | 12 | lượt 2 do gộp main (`maxNoteRunes` trùng) |
| 71 | 1 | 0 | 0 | 1 | 1 | 8 | Ảnh hưởng thiếu `promotion.Actor` |
| 72 | 1 | 0 | 0 | 0 | 3 | 5 | |
| 73 | 5 | 0 | 0 | 3 | 8 | 10 | 2 commit + review lại sau ĐẠT + 2 lượt gộp main |
| 74 | 1 | 0 | 0 | 1 | 2 | 6 | Ảnh hưởng thiếu `transition.go` |
| 75 | 2 | 0 | 0 | 0 | 3 | 7 | lượt 2 do gộp main (`dh01` trùng tên) |
| 76 | 1 | 0 | 0 | 0 | 2 | 6 | reviewer không chạy được `go test` (hook chỉ cho lệnh đọc) |
| 77 | 4 | 0 | 0 | 3 | 4 | 16 | 2 commit + 2 lượt gộp main (thứ tự khóa hai mã, trùng hàm test) |
| 78 | 2 | 1 | 0 | 1 | 3 | 10 | `ExpireDrafts` khóa sai thứ tự nhiều đơn → deadlock (45 → 0) |
| 79 | 1 | 0 | 0 | 0 | 4 | 8 | |
| 80 | 1 | 0 | 0 | 0 | 3 | 7 | |
| 81 | 4 | 0 | 0 | 2 | 7 | 11 | Ảnh hưởng ghi tên câu sqlc không có thật; 2 commit + 1 lượt gộp main |
| 82 | 1 | 0 | 0 | 0 | 2 | 6 | |
| 83 | 2 | 6 | 0 | 0 | 8 | 12 | mockup vẽ hành vi máy chủ không làm (`merge_not_latest`, nhận hàng theo món, số `RefundFor`…) |
| 84 | 2 | 1 | 0 | 1 | 2 | 7 | `VariantPicker` báo "hết hàng" nhầm khi trang số dư còn `nextCursor` |
| 85 | 2 | 1 | 0 | 0 | 4 | 9 | sửa đơn gửi `trafficSourceId` không ai yêu cầu |
| 86 | 1 | 0 | 0 | 0 | 3 | 8 | |
| 87 | 1 | 0 | 0 | 0 | 3 | 9 | |
| 90 | 2 | 0 | 0 | 0 | 5 | 7 | 2 commit (cấu trúc + hành vi) |
| 425 | 1 | 0 | 0 | 1 | 1 | 3 | câu Tái dùng sai |
| 427 | 1 | 0 | 0 | 0 | 2 | 1 | |
| **Tổng** | **58** | **14** | **0** | **19** | **≈117** | **250** | |

- **Tổng vòng 58, phải sửa 14, sai 0, câu chữ 19, ghi chú ≈117** — cộng cột của bảng.
- **Task cần > 1 vòng: 18/34** (59, 60, 61, 62, 64, 66, 67, 69, 70, 73, 75, 77, 78, 81, 83, 84, 85, 90) — đếm dòng
  Vòng ≥ 2. Trong đó **10 task có lượt CẦN SỬA** (59, 61, 66, 67, 69, 78, 81, 83, 84, 85) — đếm chữ "CẦN SỬA".
- **Vòng thêm vì gộp main hoặc sửa sau khi đã ĐẠT: 10/58 (17%)** — 60 (1), 64 (1), 70 (1), 73 (3), 75 (1), 77 (2),
  81 (1); đếm các lượt ghi "gộp main"/"review lại" mà lượt trước đã ĐẠT. Thêm **5 vòng do tách 2 commit** cấu trúc/hành
  vi (62, 73, 77, 81, 90), không tính vào 10.
- **Hook Stop chặn có ghi: 4 lần / 4 task** — lệnh `grep -n -i "hook stop\|stop chặn\|hook chặn\|stop-check"`:
  59 (jscpd chặn ba `module.go` trùng → gộp `platform/background`; chặn đúng), 425 (chặn vì `TestM2Routes_Unimplemented`
  đỏ sẵn trên main; chặn đúng, lỗi ngoài task), 73 và 81 (báo đỏ test chập chờn `TestPublicVariants`,
  `TestCleanupStale…` do chạy chồng/DB chung; chặn nhầm). Không task nào ghi tới ngưỡng 8. Hook khác (không phải
  Stop): hook Bash chặn gọi `ci-task.sh` ở 59, 65; hook của reviewer không cho chạy `go test` ở 76.
- **Task có mục "Quyết định thay chủ shop": 34/34, tổng 250 quyết định** — đếm mục đánh số/gạch đầu dòng/dòng bảng
  (D1–D12) trong mục đó; trung bình ≈7,4/task; nhiều nhất Task 77 (16).
- **Task có "Test bị xóa hoặc tắt" khác "(không có)": 12/34** — 59, 60, 61, 62, 67, 70, 73, 74, 76, 77, 90, 425
  (425 mở đầu "(không có)" nhưng ghi sửa hai test). Xóa thật chỉ 3: 73 (4 ca 501), 74 (ca `r7`), 77 (hàm hỗ trợ trùng
  `usedCount`, `editItem`); còn lại là đổi kỳ vọng theo hành vi mới, không nới.
- (Thêm) "Khác kế hoạch" có nội dung ở 31/34 task (≈124 gạch đầu dòng); "Việc phát hiện thêm" có nội dung ở 30/34
  (≈105 gạch) — đếm gạch đầu dòng bằng awk, gần đúng.

## 5. Lỗi lặp lại ở từ hai task (mục 12: lỗi lần thứ hai thêm vào "Bẫy đã gặp")

| Lỗi | Task | Trong "Bẫy đã gặp" |
|---|---|---|
| Khóa ngược thứ tự ADR-46 → deadlock (phải sửa 66, 78; ghi chú/lượt sau 70, 77) | 66, 70, 77, 78 | **Đã có** ("Khóa nhiều bảng…") — vẫn lặp 2 lần phải sửa |
| Mục Ảnh hưởng / bảng bằng chứng / Tái dùng sai tên, sai số ca, thiếu ký hiệu | 62, 63, 67, 69, 71, 73, 74, 77, 78, 81, 84, 425 | **Đã có** ("Mục Ảnh hưởng, bảng bằng chứng…") — vẫn lặp 12 task |
| Màn hình vẽ/gửi điều máy chủ không làm (mockup sai luật máy chủ; đọc trang chưa hết coi là 0; gửi trường không đổi) | 83, 84, 85 | **Đã có một phần** ("Màn hình: chỉ vẽ hành vi…"); ca "dữ liệu phân trang dở" và "PATCH gửi trường người dùng không đổi" chưa ghi |
| `TestM2Routes_Unimplemented` lấy một route "chưa làm" cụ thể → gãy khi task khác làm route đó (67 đổi → 69 phải sửa; 82 đổi → main đỏ → 425, 427 sửa gốc) | 67, 69, 82→425, 427 | **Chưa có** (đã sửa gốc ở 427: handler giả) |

Lặp ≥ 2 task nhưng chỉ là ghi chú không bắt buộc (không vào Bẫy): tenant thiếu dòng mặc định (`EnsureDefaults`) → 500
(62, 72, 74); hủy đơn qua đường khác không có audit `order.cancel` riêng (79, 80); test đọc bộ đếm deadlock của cả
database thử (74, 78). Lặp không do reviewer bắt: trùng tên/trùng thân hàm sau gộp main (70, 75, 77 — build/jscpd bắt).

Task 89 thêm hai dòng vào "Bẫy đã gặp" của `CLAUDE.md`: màn hình đọc dữ liệu phân trang chưa hết / gửi trường người dùng
không đổi; test lấy một route "chưa làm" cụ thể làm mẫu.

## 6. Nhận xét và đề xuất sửa quy trình

- **Gộp main làm thêm vòng review giảm mạnh** (44% → 17%) nhờ luật "gộp main đúng một lần trước lượt review cuối".
- **Hook Stop chặn nhầm vì test chập chờn khi nhiều phiên dùng chung máy** (73, 81) — đã có task 420, 421, 428 làm
  test chờ đúng điều kiện. Đề xuất giữ.
- **Mục Ảnh hưởng ghi sai vẫn lặp ở 12/34 task** dù đã có dòng "Bẫy đã gặp": luật chữ không đủ. Đề xuất chuyển thành
  máy chặn: hook commit đối chiếu tên hàm `Test…` và ký hiệu Go trong bảng bằng chứng/Ảnh hưởng với `git grep` (tên
  không có trong mã → chặn). Cần chủ shop OK (đổi tệp quy trình).
- **Khóa ngược thứ tự ADR-46 lặp 2 lần phải sửa** (66, 78) dù đã có luật; test hai đường song song bắt được cả hai lần.
  Đề xuất: mỗi đường ghi mới chạm từ hai nhóm khóa phải có test "song song không deadlock" ghi trong Interfaces của kế
  hoạch (M3 trở đi).
- **Mockup (Task 83) chiếm 6/14 phát hiện phải sửa**: mockup vẽ hành vi máy chủ không làm. Đề xuất: bước soát mockup
  đối chiếu từng nút với `x-permission` và mã lỗi của `openapi.yaml` trước khi trình.
- **Mutation testing M2 chưa nằm trong `make mutation`/lịch CI**: danh sách vùng cao `HIGH_RISK_DIRS` (tệp quy trình,
  chỉ chủ shop sửa) chưa có `order`, `promotion`, `customer`. Xem "Dừng để hỏi" của Task 89.
- **Mutation của gói `order` chậm** (bộ test của gói chạy ≈ 2–4 phút trên PostgreSQL thật khi máy tải nặng; ≈ 900 đột
  biến): lượt đầy đủ mất nhiều giờ. Đề xuất cho lịch CI: chạy theo phần đổi (`gremlins --diff`) mỗi tuần, lượt đầy đủ
  mỗi mốc.
