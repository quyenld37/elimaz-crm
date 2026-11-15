---
paths:
  - "console/**"
---

# Luật màn hình nhân viên (React + Vite)

1. Chưa có màn hình nào được duyệt. Không dựng màn hình cho nhân viên khi chưa có mockup chủ shop đã chốt.
2. Gọi API **chỉ** qua client sinh trong `console/src/api/` (không sửa tay). Không `fetch` thẳng tới `/api/v1`, trừ lớp realtime `console/src/realtime/`.
3. Không `any`, không ép kiểu (`as`) trên dữ liệu từ API. Kiểu sai thì sửa hợp đồng OpenAPI rồi sinh lại.
4. Mọi yêu cầu ghi mang header `Idempotency-Key` sinh khi người dùng bắt đầu thao tác; tự thử lại thì giữ nguyên khóa.
5. Câu chữ hiển thị là tiếng Việt cho nhân viên; băng báo dùng đúng câu của spec 04 mục 6.
6. Mã dùng chung của màn hình nằm trong `console/src/shared/` và có trong danh mục dùng chung.
