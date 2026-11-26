// Package conversation sở hữu hội thoại, tin, comment, bài viết, nhãn, mẫu trả lời, phân công, quy tắc tự động, phiên
// live, nhắc việc, cuộc gọi (spec 04 mục 3; kế hoạch M4). Được gọi channel, customer, order, identity (Q1 của M4:
// conversation điều phối chốt đơn từ comment live trong một transaction); order không gọi conversation.
//
// Phần công khai: chủ đề báo hiệu realtime (topics.go), điểm nối phần chạy nền (wiring.go: Deps và các hàm gom
// handler vào, lệnh, việc nền, lịch, luồng nền). Mỗi phần nằm trong internal/<phần>/ và khai Parts của mình; task của
// phần chỉ sửa tệp của phần đó (khung dựng ở Task 143, M4-03).
package conversation
