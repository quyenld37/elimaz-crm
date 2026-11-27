# Task 245 — Đối chiếu giả định M7

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 245 = M7-00). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu. Kết quả: `docs/khoi-dong/doi-chieu-gia-dinh-m7.md`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task không sửa mã Go).
- Gói phụ thuộc ngược (go list): không có.
- Operation OpenAPI bị chạm, oasdiff: không có (chỉ đọc `openapi/openapi.yaml`).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (chỉ đọc `migrations/*.sql`, `docs/views.md`, `docs/adr/0046-thu-tu-khoa.md`; không sửa ADR-46 vì không bảng nào thiếu).
- Test sẽ chạy: `go test -count=1 -run '^TestViewsDocMatchesDatabase$' ./tests/arch/` (bằng chứng cột view thật, thay `\d+` bằng tay); lệnh đếm bảng của ADR-46 và lệnh dò số migration (bảng Bằng chứng). Không có mã nên không chạy ci-task (ghi ở "Khác kế hoạch").

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không viết hàm, kiểu nào.
- Dùng lại / viết mới vì: theo mẫu có sẵn `docs/m5/doi-chieu-gia-dinh.md` (Task 183) và `docs/m6/doi-chieu-gia-dinh.md`: bảng một dòng mỗi giả định, cột kết luận `khớp`/`khác tên`/`thiếu`, tên thật kèm `tệp:dòng`.

## Soát thiết kế

Không áp dụng mục 5.1–5.12, 6.1–6.13: task chỉ đổi tài liệu, không chạm database, tiền, quyền, hàng đợi. Riêng 6.3 (thứ tự khóa): đã kiểm 131 bảng của M0–M6 đều có tên trong ADR-46 (G18), không chèn gì. Các điểm phát hiện khi đối chiếu được ghi cho task M7 tương ứng trong cột "Việc phải làm" của tệp đối chiếu, không làm ở đây.

## Quyết định thay chủ shop

Chủ shop vắng tới sáng, giao toàn quyền trong phạm vi kế hoạch, spec.

| # | Câu hỏi | Chọn | Vì sao | Phương án khác |
|---|---|---|---|---|
| QĐ-245-1 | Kế hoạch đòi `\d+` trên database dev đã migrate; database `crm` của bộ chung `crm-shared` chưa migrate, mẫu `crm_tpl_*` là của phiên khác | Lấy cột view từ `docs/views.md` và chạy `TestViewsDocMatchesDatabase` (test tự dựng database riêng, chạy đủ migration, so từng cột của mọi view công bố với tài liệu) | Cùng thông tin với `\d+` nhưng do máy so, không đụng hạ tầng chung; kết nối vào database mẫu sẽ làm phiên khác không nhân bản được mẫu | `make db-up` riêng rồi `\d+` (cấm trong đợt này: bộ chung); mở database mẫu (ảnh hưởng phiên khác) |
| QĐ-245-2 | G02 kế hoạch ghi "Điểm dừng A kết luận thẻ Access có dấu bước hai", nhưng `docs/tasks/diem-dung-a.md` ghi chưa kết luận (chờ ứng dụng Access thử) | Ghi kết luận `khác (chưa kết luận)`, không dừng | Chỉ task 251, 255 (nhóm đăng nhập để sau) dùng; kiểm thẻ thật là việc ngoài máy, làm khi mở nhóm đó | Dừng hỏi ngay (chặn vô ích, không task nào của đợt cần) |
| QĐ-245-3 | G15 phần website (`COMMERCE_SOURCE`, công tắc bảo trì) không thấy trong `apps/storefront`; "Nếu thiếu" là dừng, báo chủ shop | Không dừng đợt này; ghi câu hỏi có đề xuất ở mục 5 tệp đối chiếu | Task dùng (270, 276) đều để sau; đợt này không task nào cần | Dừng cả task 245 (chặn 246, 247, 260–265, 269, 273 không lý do) |
| QĐ-245-4 | Test máy "mọi bảng có tên trong ADR-46" (G18) thuộc Task 248 (để sau) | Ghi vào "Phụ thuộc task để sau"; Task 260 tự quyết có làm không | Hôm nay 0 bảng thiếu nên không có rủi ro tức thời; thêm việc vào 260 là việc của phiên 260 theo kế hoạch | Giao bắt buộc cho 260 (nới phạm vi một task khác) |

## Phụ thuộc task để sau

Xem `docs/khoi-dong/doi-chieu-gia-dinh-m7.md` mục 4: test máy ADR-46 (248), dấu bước hai thẻ thật (251, 255), chỉ số gửi bị chặn và đọc token đối chiếu tin (266, 267), nguồn `fallback` + `AllowInactiveSource` + `OutOfStockError` (268), công tắc bảo trì website (270, 276).

## Dừng để hỏi

(không có — không dòng `thiếu` nào thiếu đường "Nếu thiếu"; G15 ghi câu hỏi không chặn ở mục 5 tệp đối chiếu)

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mọi dòng G01–G19 có kết luận và tên thật | đọc tệp | `awk '/^## 1\./,/^## 2\./' docs/khoi-dong/doi-chieu-gia-dinh-m7.md \| grep -cE '^\| G[0-9]{2} \|'` (chỉ bảng mục 1) | `19` |
| (d) Cột view ghi trong tệp là cột thật của database | `TestViewsDocMatchesDatabase` | `go test -count=1 -run '^TestViewsDocMatchesDatabase$' -v ./tests/arch/` | `--- PASS: TestViewsDocMatchesDatabase (3.26s)` / `ok elimaz.com.vn/crm/tests/arch 3.927s` |
| (b) Không bảng M0–M6 nào thiếu trong ADR-46 | lệnh dò | `grep -ohE "CREATE TABLE (IF NOT EXISTS )?[a-z_]+\.[a-z_0-9]+" migrations/*.sql \| awk '{print $NF}' \| sort -u` (131 tên) rồi với mỗi tên `grep -q "\`$t\`" docs/adr/0046-thu-tu-khoa.md \|\| echo MISSING $t` | 131 tên, 0 dòng `MISSING` |
| (a) Số migration lớn nhất, chưa có 07xx | lệnh dò | `for b in task-* m4; git ls-tree --name-only $b migrations/ \| sort \| tail -1`; `ls migrations \| sort \| tail -1` | `0606_files_exports_bucket.sql` (main, task-135/182/204/245/433/435); task-472 `0501`, task-485 `0524`, m4 `0530`; không nhánh nào có `07xx` |
| (e) Lỗi của `order.Create` | đọc mã | `grep -n "OutOfStockError\|ErrNotForSale\|ErrVariantNotFound" internal/modules/order/errors.go internal/modules/order/create_m2.go internal/httpapi/errors_order.go` | `create_m2.go:178–179` đổi `*inventory.InsufficientError` sang `*OutOfStockError{VariantID, Available}`; `errors_order.go:36, 37, 54` → 409 `out_of_stock`, 422 `variant_not_for_sale`, 422 `variant_not_found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `\d+` bằng tay thay bằng `TestViewsDocMatchesDatabase` (QĐ-245-1).
- Task chỉ đổi tài liệu: không chạy `scripts/agent/ci-task.sh` và không gọi reviewer (CLAUDE.md bước 7: "Task chỉ đổi tài liệu, cấu hình: không cần reviewer").

## Việc phát hiện thêm

- Kế hoạch M7 G02 ghi Điểm dừng A "có mang dấu bước hai" — sai so với `docs/tasks/diem-dung-a.md` (chưa kết luận). Không sửa kế hoạch (Step 3: tên thật ghi ở tệp đối chiếu).

## Reviewer

Kết luận: không cần (task chỉ đổi tài liệu, CLAUDE.md bước 7)
