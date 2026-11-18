// Package orderview là nguồn duy nhất của nhãn gộp của đơn (kế hoạch M2, Task M2-08, Q32): đơn có ba trục riêng —
// trạng thái đơn, đóng gói, vận đơn hiện hành (05 mục 2) — còn nhân viên thấy một nhãn theo cách gọi quen trên Nhanh.
//
// Gói thuần, chỉ thư viện chuẩn, không database: module order (màn đơn), internal/queries (báo cáo M6) và cảnh báo
// vận chuyển của M3 cùng gọi Label; không mốc nào viết bản thứ hai. Chữ tiếng Việt của từng nhãn nằm ở màn hình
// (console, labels.ts), theo đúng thứ tự Labels().
package orderview
