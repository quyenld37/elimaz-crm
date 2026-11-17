---
name: crm-task
description: Quy trình bắt buộc để agent crm-implementer làm MỘT task của CRM ELIMAZ (Go + React) — đọc, ghi Ảnh hưởng và Tái dùng, soát thiết kế, test trước, cổng máy, bằng chứng, trả kết quả cho điều phối. Dùng khi được giao một task CRM hoặc sửa một lỗi trong apps/crm.
user-invocable: true
---

# Làm một task CRM

Nguồn đầy đủ: `docs/quy-trinh-lam-viec.md`. Tệp này là các bước bắt buộc, theo đúng thứ tự. Bước nào không làm được thì dừng và trả về điều phối kèm lý do; không bỏ qua, không làm tắt. Khi dừng giữa chừng để hỏi: thêm mục `## Dừng để hỏi` vào cuối tệp task, ghi câu hỏi cụ thể và phương án đề xuất (hook cho phép dừng khi mục này có nội dung).

## 0. Xác định việc

- Đọc `docs/tasks/hien-tai.md` để biết số task. Tệp task là `docs/tasks/task-NN.md` (điều phối đã tạo từ mẫu).
- Đọc task trong kế hoạch, mục Interfaces của các task nó dùng, mục spec và ADR mà task trỏ tới, `CLAUDE.md`.
- Loại việc (quy trình mục 3): A task, B sửa lỗi, D dọn code, E thay đổi phá vỡ. Gặp loại C (đổi thiết kế) hoặc F (ngoài máy): **dừng**, trả về điều phối.
- Thiếu điều kiện sẵn sàng (quy trình mục 4.1) hoặc spec mơ hồ, mâu thuẫn: dừng, trả về điều phối kèm câu hỏi cụ thể.

## 1. Ảnh hưởng (trước khi viết test)

Điền mục "Ảnh hưởng" của tệp task theo bốn tầng (quy trình mục 7):

- Ký hiệu Go sẽ đổi: dùng công cụ `LSP` (`findReferences`, `incomingCalls`), ghi số chỗ dùng. Không chỉ tìm theo chữ.
- Gói phụ thuộc ngược: `go list -f '{{.ImportPath}} {{.Imports}}' ./...`.
- Hợp đồng API, màn hình; câu sqlc, bảng, pha migration; test sẽ chạy.

## 2. Tái dùng

Điền mục "Tái dùng" (quy trình mục 8): đọc `docs/danh-muc-dung-chung.md`, tìm theo tên và theo việc. Ghi dùng lại cái gì, hoặc viết mới vì sao. Không tạo gói `util`, `common`, `helpers`, `shared`.

## 3. Soát thiết kế

Điền mục "Soát thiết kế": trả lời từng mục 5.1–5.12 và 6.1–6.13 của quy trình. Mục không áp dụng: "không áp dụng: <lý do>". Câu trả lời nào lộ ra một chỗ task chưa tính: dừng, trả về điều phối.

## 4. Test trước, code sau

- Viết đúng các test trong kế hoạch (tên, giá trị kiểm), chạy thấy đỏ, rồi viết code cho xanh, rồi dọn.
- Dời, đổi tên, tách hàm: commit riêng, không lẫn với thay đổi hành vi. Đổi tên bằng rename của gopls.
- Sửa định nghĩa một ký hiệu thì sửa mọi chỗ dùng. Chuyển sang cách mới thì xóa cách cũ.
- Vùng rủi ro cao: thêm test sinh tình huống ngẫu nhiên (rapid) cho quy luật phải luôn đúng.
- Không làm gì ngoài task. Thấy việc khác cần làm: ghi vào "Việc phát hiện thêm".

## 5. Cổng máy

- Hook chạy build, vet, lint, test của gói đổi và gói phụ thuộc khi bạn định dừng. Không qua thì sửa tiếp. Không tắt, không nới test, không né hook bằng lệnh shell.
- Hook chặn 8 lần liền, hoặc sửa hai lần vẫn không qua một lỗi: dừng, trả về điều phối.
- Từ Task 3 trở đi: chạy `make ci` trước khi trả kết quả.

## 6. Bằng chứng

Điền bảng "Bằng chứng": mỗi tiêu chí → tên test → lệnh đã chạy → trích kết quả thật. Mọi điều chưa làm, chưa kiểm được ghi rõ. Test bị xóa, tắt hay nới: ghi tên và lý do ở mục riêng.

## 7. Trả về điều phối

Không commit (điều phối commit sau khi reviewer ĐẠT). Trả về, bằng tiếng Việt:

1. Tóm tắt 5–10 dòng: đã làm gì, bằng chứng chính, khác kế hoạch, chưa làm hay chưa kiểm được gì.
2. Danh sách tệp đã đổi.
3. Câu hỏi cho chủ shop nếu có.
