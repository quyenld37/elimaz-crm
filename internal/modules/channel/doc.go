// Package channel sở hữu tài khoản kênh nhắn tin (Page Facebook), token mã hóa, chế độ chỉ xem, giới hạn tốc độ gửi
// và bộ adapter kênh (spec 04 mục 3, mục 4; kế hoạch M4).
//
// Phần công khai: kiểu chuẩn hóa dùng chung cho mọi kênh (ChannelAccount, SendPolicy, OutboundCommand, sự kiện vào
// chuẩn hóa ở events.go), interface MessagingAdapter mà mỗi kênh hiện thực (internal/adapters/<kênh>, chỉ cmd/crm nối
// vào Registry), giao diện tùy chọn kiểm bằng ép kiểu, và điểm nối phần chạy nền (wiring.go). Mọi kiểu dữ liệu riêng
// của Meta ở lại trong adapter; module conversation chỉ thấy kiểu của gói này.
//
// Khung dựng ở Task 143 (M4-03): hiện thực ở Task 145 (tài khoản, token, chỉ xem, giới hạn gửi), 149 (kết nối Page),
// 150 (sức khỏe kênh), 179 (xóa dữ liệu theo yêu cầu của Meta).
package channel
