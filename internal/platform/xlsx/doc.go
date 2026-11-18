// Package xlsx đọc và ghi tệp Excel (.xlsx) cho mọi mốc của CRM (kế hoạch M1, Task M1-38, C22): nhập sản phẩm (M1),
// nhập khách (M2), bảng kê vận chuyển (M3), xuất báo cáo (M6), mở sổ (M7). Đây là chỗ duy nhất nhập excelize.
//
// Read đọc trang đầu của tệp người dùng tải lên trong giới hạn Limits (kích thước tệp, tổng giải nén theo header zip,
// số dòng, độ dài ô) trước khi excelize giải nén; ô có công thức không được nhận (CellError formula_not_allowed).
// Write ghi mọi ô dạng chữ, qua Sanitize, nên tệp xuất không bao giờ chứa công thức (chặn chèn công thức, 04 mục 13).
// Sanitize là hàm chặn công thức duy nhất: bộ ghi theo luồng của M6 dùng lại nó, không viết hàm thứ hai.
//
// Khi nào không dùng: không nhập excelize ở gói khác (test kiến trúc chặn); không tự ghi ô bằng excelize mà bỏ qua
// Sanitize; không đọc tệp người dùng bằng excelize.OpenFile/OpenReader không giới hạn. Gói không biết nghiệp vụ: kiểm
// dữ liệu từng dòng (giá, SKU…) thuộc module dùng nó. Gói không log và không đưa nội dung ô vào lỗi Go (tệp khách có
// dữ liệu cá nhân).
package xlsx
