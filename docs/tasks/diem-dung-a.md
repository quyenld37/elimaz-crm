# Điểm dừng A — quyết định (18/11/2026)

Chủ shop vắng mặt và đã giao toàn quyền quyết định (lệnh /goal đêm 17–18/11). Điều phối viên quyết thay, ghi lại để chủ shop xem lại.

## Kết quả đo (trên laptop, chỉ để tham khảo)

| Ngưỡng ADR-44 | Kết quả | Nguồn |
|---|---|---|
| p95 phía máy chủ < 300 ms ở 1.000 lượt/giây | Đạt: 8,8–9,9 ms (tắt dấu vết) | `docs/do-luong/flash-sale-go.md` |
| 500 suất ra đúng 500 đơn | Đạt cả 6 lần | như trên |
| Không khách nào vượt giới hạn | Đạt cả 8 lần | như trên |
| 50 đơn/giây trong 5 phút | Đạt: 15.000 đơn, p95 8,3 ms | như trên |
| Bật dấu vết làm p95 tăng ≤ 10% | **Chưa kết luận**: tăng 2,4–4,5 lần nhưng Jaeger chạy cùng máy | như trên |
| Khôi phục database có bấm giờ | `rpo_seconds=58`, `restore_seconds=4` trên database rất nhỏ, mã hóa đúng | `docs/do-luong/phep-thu-ngan.md` |
| Tạo lại container giữ dữ liệu | Đạt | như trên |
| Tìm kiếm dưới RLS dùng index qua hàm riêng | Đạt: 0,5 ms so với 21,7 ms quét bảng | như trên |

## Quyết định

1. **Sang Phần B (Task 3 trở đi).** Go đạt mọi ngưỡng đo được trên máy này với khoảng dư rất lớn (p95 ~10 ms so với ngưỡng 300 ms), nên không có lý do xem lại ADR-44.
2. **Chi phí dấu vết:** chưa kết luận được trên laptop. Task 3 làm tỷ lệ lấy mẫu dấu vết cấu hình được theo biến môi trường chuẩn của OpenTelemetry (`OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG`), giữ mặc định theo spec. Giá trị chốt cho production quyết sau khi đo lại trên máy thử với Jaeger ở máy khác.
3. **Bị giữ lại, chờ chủ shop OK riêng** (cần máy chủ hoặc tài khoản):
   - Task 1 Step 6: đo lại trên máy thử 13.140.64.56.
   - Phép thử ngắn số 3: SSE qua Cloudflare trên máy thử.
   - Ứng dụng Access thử để đọc trường "đã qua bước hai" (ADR-35). Task 7 làm tên trường và giá trị cấu hình được (`ACCESS_MFA_CLAIM`, `ACCESS_MFA_VALUES`), test bằng thẻ giả ký bằng khóa thử; giá trị thật chốt khi có ứng dụng Access thử. Nếu thẻ thật không mang dấu đó, ADR-35 đòi CRM tự hỏi bước hai: việc này đã có trong kế hoạch M7 (TOTP).
