// Package reporting là module báo cáo vận hành (kế hoạch M6, ADR-20): schema reporting gồm lần chạy báo cáo
// (report_runs) và chụp tồn hằng ngày (stock_snapshot_days, stock_daily_snapshots, chỉ ghi thêm). Module đọc dữ liệu
// của module khác chỉ qua view _v đã công bố (docs/views.md), không nhập Go của module nghiệp vụ nào ngoài phần công
// khai của identity.
package reporting
