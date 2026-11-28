# Task 518 — Dọn openapi/breaking-approved.yaml sau 433, 517

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có; chỉ sửa tệp cấu hình `openapi/breaking-approved.yaml`
- Gói phụ thuộc ngược (go list): không có; chỉ `tools/apicheck` đọc tệp này lúc chạy
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng; bỏ 356 dòng duyệt (346 của Task 433, 10 của Task 517), cả hai đã vào main (5e14ec76, fc263ee7)
- Chỗ dùng trên màn hình: không có
- Câu sqlc, bảng, pha migration: không có
- Test sẽ chạy: `make oasdiff` (so với main), `go test ./tools/apicheck/`

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không có mã mới
- Dùng lại / viết mới vì: dùng nguyên cổng oasdiff có sẵn

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

Không áp dụng: chỉ xóa dòng cấu hình, không chạm database, tiền, quyền, hàng đợi hay đồng thời. Dòng duyệt chỉ cho qua thay đổi so với main; main đã chứa thay đổi nên không cần nữa. Tệp giữ `approved: []` và phần chú thích.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Cổng oasdiff xanh khi bỏ hết dòng duyệt | make oasdiff | `make oasdiff` | thoát 0, không báo CHƯA DUYỆT |
| Cổng vẫn bắt thay đổi phá vỡ thật | tools/apicheck | `go run ./tools/apicheck -base-ref=3387e18e^ -rev openapi/openapi.yaml -approved openapi/breaking-approved.yaml` | `CHƯA DUYỆT: healthDeep ... zero_discount`, thoát 1 |
| Bộ đọc dòng duyệt không hỏng với danh sách rỗng | go test | `go test -count=1 ./tools/apicheck/` | ok |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (reviewer). Không có phát hiện phải sửa; số dòng (346 của 433, 10 của 517), hợp đồng không đổi, cổng vẫn bắt thay đổi thật đều khớp.
