# Đo cách cài đặt agent cuối M1 (Task 405, M1-55d; Step 4 của Task 56)

Ngày đo: 21/11/2026, trên nhánh `task-405` (= main `e53d001`: M1 task 24–56, 315, 320–334, 389–401, 403, 404, 406–414,
420–429; cùng lúc main đã có M2 và một phần M3, M4), theo mục 12 của `docs/quy-trinh-lam-viec.md`, cùng lệnh với
`docs/do-luong/cai-dat-agent-m0.md` và `cai-dat-agent-m2.md` để ba mốc đặt cạnh nhau. Tệp M2 viết trước tệp này (19/11)
nên cột M1 của nó còn "—"; bảng dưới điền đủ.

Như M0, M2: số đo trên mã (jscpd, `//nolint`) là của **toàn bộ repo** tại ngày đo (đã gồm M2 và phần M3, M4 đã gộp); số
theo task chỉ tính task M1. Giải thích từ tiếng Anh (agent, hook, lint, jscpd, mutation, reviewer…): xem đầu tệp đo M0.

## 1. Tóm tắt, so với M0 và M2

| Chỉ số | M0 (18/11) | M1 (21/11) | M2 (19/11) | Lệnh |
|---|---|---|---|---|
| Mã trùng (jscpd) | 0 đoạn / 444 tệp, 76.813 dòng | **0 đoạn / 900 tệp, 164.120 dòng** (Go 688 tệp / 124.956 dòng; TSX 119 / 24.427; TS 93 / 14.737) | 0 đoạn / 856 tệp, 156.184 dòng | `jscpd --config .jscpd.json --reporters console .` |
| Mốc nền jscpd | rỗng | **rỗng** (`"fingerprints": {}`) | rỗng | `cat jscpd-baseline.json` |
| Điểm mutation vùng cao | thấp nhất 82,4% (sau test thêm) | **thấp nhất 73,6%** lượt gremlins (`internal/imager`, không có test libvips); **92,9%** sau test thêm (`catalog/article_html.go`) | thấp nhất 86,84% (88,42% sau test thêm) | gremlins (mục 2) |
| `//nolint` (bỏ mã sinh) | 646 (346 test, 300 mã chạy) | **1.349** (831 test, 518 mã chạy); 260 trong `catalog`, `inventory`, `platform/files`, `imager`; **0** chỗ không ghi lý do | 1.273 (758 test, 515 mã chạy) | mục 3 |
| Phát hiện reviewer phải sửa | 17 / 30 task | **28 / 71 task** (16 task có; 5 ở Task 49 mockup, 4 ở Task 38 đọc xlsx); reviewer sai **2** | 14 / 34 task; sai 0 | mục 4 |
| Số vòng review | 61 / 30 task (2,0) | **131 / 71 task (1,8)**; 41/71 task > 1 vòng | 58 / 34 task (1,7) | mục 4 |
| Vòng thêm vì gộp main / sửa sau ĐẠT | 27/61 (44%) | **30/131 (23%)**: 21 gộp main, 9 sửa sau ĐẠT (+7 vòng do tách commit cấu trúc/hành vi) | 10/58 (17%) | mục 4 |
| Hook Stop chặn (có ghi) | 4 | **2** (421, 423; cả hai nhầm do test chập chờn khi nhiều phiên dùng chung máy); không task nào tới ngưỡng 8 | 4 | mục 4 |
| Quyết định thay chủ shop | — | **449** / 71 task (≈6,3/task) | 250 / 34 task (≈7,4/task) | mục 4 |
| Review độc lập cuối mốc | `docs/do-luong/review-doc-lap-m0.md` | **0 nghiêm trọng, 9 lớn, 20 nhỏ, 4 nghi ngờ**; 4 sửa trong Task 404, 29 tách thành task bù 406–415 (gộp đủ trừ 415) | 0 nghiêm trọng, 7 lớn, 17 nhỏ, 4 nghi ngờ | `docs/review-doc-lap-m1.md` |
| Độ dài `CLAUDE.md` | 99 dòng | **125 dòng** sau Task 405 (giới hạn 200) | 115 dòng sau Task 89 | `wc -l CLAUDE.md` |

M1 là mốc lớn nhất tới nay (71 tệp task so với 30 của M0, 34 của M2) vì ngày 18–19/11 kế hoạch chia nhỏ (task con
320–334), thêm task sửa test chập chờn (400, 420, 421, 428, 429) và mười task bù sau review độc lập (406–415).

## 2. Điểm mutation vùng cao M1

Lệnh, chế độ chạy, ba điều chỉnh khi đọc số của gremlins v0.6.0 và lý do từng đột biến còn sống: `docs/mutation.md` mục
"Lần chạy 19/11/2026 — vùng cao M1" (Task 403). Ngưỡng 80% (như M0). Không đo lại ở task này.

| Vùng | Hiệu quả lượt gremlins | Sau test thêm |
|---|---|---|
| `catalog/internal/importer/apply.go` | 80,0% | 100% |
| `fulfillment/status.go` | 100% | 100% |
| `modules/cash` | 86,0% | 100% |
| `internal/imager` | 73,6% (chỉ phần chạy trên macOS, không libvips) | 99,3% |
| `catalog/article_html.go` | 80,4% | 92,9% |
| `platform/files` (trừ `memstore.go`) | 75,7% (18 đột biến cuối chưa chạy) | 94,3% |
| `modules/inventory` | 91,9% (45 đột biến chưa chạy, máy ngủ) | 97,0% |
| `modules/order` (6 tệp M1) | 77,1% | 98,6% |
| `modules/identity` (`staff.go`, `roles.go`) | 91,53% (đo 18/11) | — |

Ba vùng dưới 80% ở lượt đầu (`imager`, `platform/files`, `order`); sau test thêm mọi vùng ≥ 92,9%. "Sau test thêm" là
cài lại từng đột biến còn sống vào bản sao mã và chạy đúng test mới, chưa chạy lại cả lượt. Hai lượt (`inventory`,
`platform/files`) chưa chạy hết vì máy dev ngủ do nhiệt giữa đêm 20/11: lượt CI theo lịch đo đủ khi các thư mục này vào
`HIGH_RISK_DIRS` (đã vào, `scripts/agent/lib.sh`).

## 3. `//nolint`

Lệnh như M0: `git grep -n "//nolint" -- '*.go' ':!internal/httpapi/gen/**' ':!**/internal/store/**' ':!internal/httpapi/routes_gen.go' | wc -l`
→ 1.349; chỉ `*_test.go` → 831; bốn vùng M1 (`internal/modules/{catalog,inventory}`, `internal/platform/files`,
`internal/imager`, bỏ mã sinh) → 260. Chỗ không ghi lý do: `git grep -nE "//nolint:[a-zA-Z0-9,]+ *$" -- '*.go'` → 0.
Tăng 76 so với ngày đo M2 (19/11) là phần test thêm của Task 403 và các task bù M1, cộng phần M3, M4 đã gộp.

## 4. Phát hiện của reviewer theo task (M1)

Nguồn: mục `## Reviewer`, "Quyết định thay chủ shop", "Test bị xóa hoặc tắt" của `docs/tasks/task-NN.md`, NN ∈ 24–54,
56, 315, 320–331, 333, 334, 389, 390, 393, 395, 398, 400, 401, 403, 404, 406–414, 420, 421, 423, 426, 428, 429 (71 tệp;
đọc bằng một agent rồi đối chiếu tay vài dòng; cùng cách phân loại M0, M2). Không có tệp: `task-55.md` (việc của M1-56
nằm ở `task-56.md`), `task-332.md` (xong trong commit Task 29). 395 và 398 cũng có trong bảng M0 (số khớp; cộng hai mốc
thì đừng tính hai lần). Không tính 402 (chưa làm, dời) và 405 (task này). QĐ = số quyết định thay chủ shop.

Lượt CẦN SỬA chỉ vì tệp task (số ca, bằng chứng trống, Ảnh hưởng) tính vào "Câu chữ", như Task 67 của M2. Nhận xét
sai của reviewer tính vào "Sai", như Task 05 của M0.

| Task | Vòng | Phải sửa | Sai | Câu chữ | Ghi chú (≈) | QĐ | Ghi chú chính |
|---|---|---|---|---|---|---|---|
| 24 | 0 | – | – | – | – | 4 | chỉ tài liệu, không cần reviewer |
| 25 | 2 | 0 | 0 | 0 | 3 | 9 | vòng 2 sau ĐẠT (sửa `viewsdoc_test` theo ghi chú) |
| 26 | 1 | 0 | 0 | 0 | 4 | 5 | |
| 27 | 2 | 0 | 0 | 0 | 7 | 9 | vòng 2 do gộp main |
| 28 | 3 | 0 | 0 | 1 | 3 | 9 | 2 commit; lượt 2 sau ĐẠT |
| 29 | 1 | 0 | 0 | 0 | 3 | 10 | |
| 30 | 2 | 0 | 0 | 0 | 4 | 9 | tách phần sang Task 333, rồi gộp main |
| 31 | 3 | 0 | 0 | 1 | 4 | 12 | lần 2 sau sửa jscpd, lần 3 gộp main |
| 32 | 1 | 0 | 0 | 0 | 3 | 7 | |
| 33 | 1 | 1 | 0 | 1 | 4 | 3 | SQL RUNBOOK ngoài ngữ cảnh tenant ra 0 dòng |
| 34 | 1 | 0 | 0 | 1 | 1 | 7 | Ảnh hưởng thiếu kiểu |
| 35 | 2 | 1 | 0 | 1 | 1 | 6 | cảnh báo Telegram bỏ qua `RetryAfter` khi 429, nuốt lỗi |
| 36 | 3 | 0 | 0 | 0 | 4 | 5 | 2 commit + 1 lượt gộp main |
| 37 | 2 | 0 | 1 | 0 | 3 | 4 | reviewer đòi câu kiểm e2e đã có |
| 38 | 5 | 4 | 0 | 1 | 3 | 11 | đọc xlsx: vượt trần số ô; gói và excelize đọc hai trang khác nhau |
| 39 | 2 | 2 | 0 | 2 | 1 | 9 | `sum` tập rỗng ra NULL; barcode hoa/thường không tìm được |
| 40 | 2 | 1 | 0 | 1 | 4 | 6 | `t.Skip` mới không ghi ở "Test bị xóa hoặc tắt" |
| 41 | 1 | 0 | 0 | 0 | 5 | 14 | |
| 42 | 2 | 0 | 0 | 2 | 3 | 7 | 2 commit; Ảnh hưởng mâu thuẫn Q1 |
| 43 | 2 | 1 | 0 | 0 | 5 | 12 | imager bận (503) vẫn tốn lượt, ảnh tốt bị từ chối vĩnh viễn |
| 44 | 2 | 0 | 0 | 0 | 2 | 10 | sau ĐẠT: gỡ trùng jscpd và gộp main |
| 45 | 1 | 0 | 0 | 1 | 3 | 8 | Ảnh hưởng thiếu `models.go` |
| 46 | 2 | 1 | 0 | 1 | 1 | 6 | tin `manifest.json` không kiểm biến thể, tệp mất về ready |
| 47 | 1 | 0 | 0 | 1 | 5 | 8 | tên câu sqlc trong Ảnh hưởng |
| 48 | 2 | 2 | 0 | 0 | 5 | 12 | 2xx thân hỏng bỏ khóa chống trùng; vẽ route khi `/me` đang tải |
| 49 | 2 | 5 | 0 | 1 | 5 | 10 | mockup vẽ hành vi máy chủ không làm |
| 50 | 1 | 0 | 0 | 0 | 2 | 6 | |
| 51 | 3 | 0 | 0 | 1 | 2 | 6 | 2 lượt gộp main |
| 52 | 4 | 0 | 0 | 4 | 8 | 8 | 2 lần CẦN SỬA chỉ vì tệp task; lượt 2 do gộp main |
| 53 | 3 | 0 | 0 | 0 | 3 | 7 | lượt 2 sau ĐẠT (e2e chập chờn), lượt 3 gộp main |
| 54 | 1 | 0 | 0 | 1 | 3 | 6 | Ảnh hưởng "hai câu" thay vì ba |
| 56 | 5 | 0 | 0 | 1 | 6 | 13 | tách 2 commit; 2 lượt gộp main; thêm hàm sau ĐẠT |
| 315 | 1 | 0 | 0 | 0 | 4 | 5 | |
| 320 | 2 | 0 | 0 | 1 | 3 | 4 | lượt 2 gộp main; Tái dùng ghi sai nguồn |
| 321 | 3 | 0 | 0 | 2 | 3 | 3 | CẦN SỬA chỉ vì bảng bằng chứng trống; gộp main |
| 322 | 1 | 0 | 0 | 0 | 2 | 6 | |
| 323 | 3 | 1 | 1 | 0 | 3 | 9 | đổi màu+size dây chuyền ra `option_taken`; ghi chú unique sai |
| 324 | 2 | 0 | 0 | 0 | 4 | 9 | lần 2 gộp main |
| 325 | 2 | 1 | 0 | 0 | 6 | 5 | thao tác ảnh khi đang lưu mẫu gây xung đột, mất nháp |
| 326 | 1 | 0 | 0 | 3 | 1 | 6 | Tái dùng sai, số ca 15≠14 |
| 327 | 2 | 0 | 0 | 0 | 4 | 7 | lượt 2 gộp main |
| 328 | 1 | 0 | 0 | 1 | 3 | 8 | |
| 329 | 3 | 0 | 0 | 0 | 5 | 9 | 2 commit + 1 lượt gộp main |
| 330 | 3 | 0 | 0 | 1 | 4 | 4 | CẦN SỬA chỉ vì số ca; lần 3 sau ĐẠT (403 vẫn hiện bảng cũ) |
| 331 | 2 | 0 | 0 | 1 | 2 | 6 | lượt 2 gộp main |
| 333 | 1 | 0 | 0 | 0 | 2 | 7 | |
| 334 | 1 | 0 | 0 | 0 | 3 | 5 | |
| 389 | 1 | 0 | 0 | 0 | 2 | 3 | |
| 390 | 2 | 0 | 0 | 1 | 1 | 2 | lượt 2 sau ĐẠT |
| 393 | 2 | 0 | 0 | 1 | 3 | 7 | 2 commit |
| 395 | 2 | 0 | 0 | 0 | 5 | 5 | đã tính ở M0; lần 2 sau ĐẠT (sửa RUNBOOK) |
| 398 | 1 | 0 | 0 | 1 | 0 | 2 | đã tính ở M0 |
| 400 | 1 | 0 | 0 | 0 | 2 | 2 | |
| 401 | 1 | 0 | 0 | 0 | 2 | 2 | phần (2) chưa làm vì hook chặn `scripts/agent/` |
| 403 | 2 | 1 | 0 | 1 | 1 | 9 | so `lost_at` (đồng hồ database) với `time.Since` (đồng hồ máy) |
| 404 | 1 | 0 | 0 | 0 | 2 | 5 | |
| 406 | 1 | 0 | 0 | 0 | 3 | 4 | |
| 407 | 1 | 0 | 0 | 1 | 2 | 3 | |
| 408 | 2 | 1 | 0 | 1 | 1 | 3 | `in_doubt` → chạy lại → `dead` hủy vận đơn, có thể ra hai kiện COD |
| 409 | 3 | 2 | 0 | 0 | 4 | 7 | lệnh không bao giờ `dead`; gán trạng thái trước commit |
| 410 | 3 | 2 | 0 | 1 | 3 | 5 | đường lui RUNBOOK không chạy được |
| 411 | 1 | 0 | 0 | 0 | 3 | 5 | |
| 412 | 1 | 0 | 0 | 0 | 3 | 5 | |
| 413 | 1 | 0 | 0 | 1 | 2 | 3 | |
| 414 | 2 | 0 | 0 | 0 | 3 | 5 | 2 commit |
| 420 | 3 | 2 | 0 | 0 | 1 | 3 | quét sót `time.After` làm tiêu chí; so chuỗi lời bọc |
| 421 | 2 | 0 | 0 | 0 | 4 | 7 | lượt 2 gộp main |
| 423 | 1 | 0 | 0 | 0 | 4 | 3 | |
| 426 | 1 | 0 | 0 | 0 | 1 | 2 | |
| 428 | 2 | 0 | 0 | 1 | 3 | 3 | lần 2 sau ĐẠT và gộp main |
| 429 | 1 | 0 | 0 | 0 | 2 | 3 | |
| **Tổng** | **131** | **28** | **2** | **40** | **≈216** | **449** | |

- **Task cần > 1 vòng: 41/71.** **Task có ít nhất một lỗi phải sửa: 16/71** (33, 35, 38, 39, 40, 43, 46, 48, 49, 323,
  325, 403, 408, 409, 410, 420).
- **Vòng thêm vì gộp main hoặc sửa sau ĐẠT: 30/131 (23%)** — gộp main 21 vòng (27, 30, 31, 36, 38, 44, 51 ×2, 52 ×2,
  53, 56 ×2, 320, 321, 324, 327, 329, 331, 421, 428), sửa sau ĐẠT 9 vòng (25, 28, 31, 53, 56, 323, 330, 390, 395). Cao
  hơn M2 (17%) vì luật "gộp main đúng một lần trước lượt review cuối" chỉ có từ 19/11, sau phần lớn task M1.
- **Hook Stop chặn có ghi: 2** — lệnh `grep -n -i "hook stop\|stop chặn\|hook chặn\|stop-check"`: 421
  (`TestCleanupStaleDropsOnlyIdleOldDatabases` đỏ khi hook Stop và ci-task chạy cùng lúc), 423 (`TestCreateCustomerPhoneTaken`
  chập chờn vì UUID ngẫu nhiên); cả hai chặn nhầm, gốc đã sửa ở Task 428 và 421. Hook khác (không phải Stop): hook của
  reviewer không cho chạy `go test` (28, 34, 54, 395); hook chặn sửa `scripts/agent/` (401); chặn commit tạm chưa review (409).
- **"Test bị xóa hoặc tắt" có nội dung: 11/71** (26, 37, 41, 56, 400, 401, 409, 414, 420, 421, 423); không task nào
  xóa hay tắt một test thật. Nới duy nhất: 409 nâng hạn chờ một lần chạy 200 → 500 ms ở hai test (hạn chống treo, không
  phải tiêu chí). 400, 420, 421 dời câu kiểm giờ thật sang nhãn `perf` hoặc thay bằng chờ theo sự kiện.
- **Chủ shop phải sửa agent:** không đo được từ tệp task — chủ shop vắng mặt phần lớn M1 (giao toàn quyền 18/11), các
  lần sửa đi qua điều phối viên (chia nhỏ task, task bù, luật mới trong lời giao), không qua tệp task.

## 5. Lỗi lặp lại ở từ hai task (mục 12: lỗi lần thứ hai thêm vào "Bẫy đã gặp")

| Lỗi | Task | Trong "Bẫy đã gặp" |
|---|---|---|
| Đếm lượt thử, bận, kết quả không chắc sai: 429/503 tốn lượt hoặc bị bỏ qua; lệnh không bao giờ tới `dead`; `in_doubt` dẫn tới hủy; 2xx thân hỏng coi là dứt khoát | 35, 43, 48, 408, 409 | **Thêm ở Task 405** |
| Tin trạng thái chưa xác nhận: gán trạng thái vào biến trước commit; tin bản kê mà không đối chiếu kho | 46, 409 | **Thêm ở Task 405** |
| So hai đồng hồ (database với máy test); giờ thật hoặc `time.After` làm tiêu chí đúng/sai | 400, 403, 420 (421) | **Thêm ở Task 405** (phần giờ thật đã có ở mục Lệnh từ Task 400) |
| Màn hình lúc đang tải, đang lưu, vừa lỗi: vẽ như rỗng hay bảng cũ; thao tác khi đang ghi | 48, 325, 330 | **Thêm ở Task 405** |
| Lệnh trong RUNBOOK hay đường lui chưa chạy thật | 33, 410 (395) | **Thêm ở Task 405** |
| RLS ra 0 dòng; `sum` tập rỗng ra NULL | 33, 39 | Đã có (gom 18/11) |
| Tệp task sai: Ảnh hưởng, bằng chứng, số ca, Tái dùng (câu chữ ở ≈27 task) | 28, 31, 33, 34, 39, 40, 42, 45, 47, 51, 52, 54, 56, 320, 321, 326, 328, 330, 331, 390, 393, 403, 407, 408, 410, 413, 428 | Đã có — vẫn lặp nhiều nhất, như M0 và M2 |
| Mockup vẽ hành vi máy chủ không làm | 49 (M2: 83) | Đã có ("Màn hình: chỉ vẽ hành vi…") — vẫn lặp ở hai mốc |

Phải sửa một lần (chưa vào Bẫy): đọc tệp ngoài lệch hai bộ đọc, trần không tính hết các phần (38); đổi dây chuyền
nhiều dòng đụng unique (323); `t.Skip` không khai (40).

## 6. Nhận xét và đề xuất sửa quy trình

- **Tệp task sai vẫn là loại phát hiện nhiều nhất ở cả ba mốc** (M0, M1 ≈27 task, M2 12 task) dù đã có dòng "Bẫy đã
  gặp": luật chữ không đủ. Giữ đề xuất của tệp đo M2: hook commit đối chiếu tên `Test…` và ký hiệu Go trong bảng bằng
  chứng, mục Ảnh hưởng với `git grep` (cần chủ shop OK, đổi tệp quy trình).
- **Lỗi về lượt thử và trạng thái không chắc tập trung ở kho tệp và ảnh** (43, 46, 408, 409, 426): vùng này có lệnh
  `commands`, imager và kho ngoài. Đề xuất: Interfaces của kế hoạch mốc sau ghi rõ bảng "lỗi → tiêu lượt hay không →
  trạng thái cuối" cho mỗi lệnh mới, và test cho từng dòng.
- **Mutation testing tìm được lỗ hổng thật** (lượt đầu dưới 80% ở `imager`, `platform/files`, `order`; Task 403 thêm
  test cho mọi đột biến có nghĩa). Giữ ngưỡng 80% và lượt CI theo lịch; máy dev không chạy được lượt dài qua đêm (ngủ
  do nhiệt) nên lượt đầy đủ đặt ở CI.
- **Ca chạy tay** (TP01 imager thật, diễn tập kho tệp, filesync, libvips) có bảng lịch `docs/ca-chay-tay.md` từ Task
  414 và đã chạy lại đủ ở Task 405 (đều đạt). Giữ.
- **Test WebKit của màn Kiểm kho**: 2/10 ca đỏ trên máy dev ngày 21/11 khi dùng `click`/`dblclick` chuột trên giả lập
  iPhone (`<html> intercepts pointer events`), các ca `tap` cùng nút đạt. Ghi ở "Việc phát hiện thêm" của Task 405 để
  điều phối viên tạo task (không nới test ở task tài liệu).
- **Hook Stop chặn nhầm giảm** (M2 2/4 nhầm; M1 2/2 nhầm, đều trước khi Task 421, 428 sửa gốc). Giữ.
