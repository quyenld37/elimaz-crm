# M6 — ghi chú mockup sáu màn báo cáo (Task 224, M6-01)

Kế hoạch: `docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` «Task 224», «Điểm dừng B», bảng «Việc cần chủ shop
trong M6». Mở `index.html` để xem ảnh của mọi màn; mỗi trang HTML mở trực tiếp trong trình duyệt.

## Trạng thái duyệt

| Việc | Ai | Ngày | Kết quả |
|---|---|---|---|
| Chốt hình sáu màn (Điểm dừng B, Q-X8, C3-3; soát ba cỡ 1512, 390, 360, agent `design-critic`) | Điều phối viên | — | chờ |
| Xem lại ảnh (không chặn) | Chủ shop | — | chờ |
| OK riêng từng hành vi (a)–(d) dưới đây | Chủ shop | — | chờ |

Task 239–241 chỉ bắt đầu cho màn đã chốt hình. Dòng hành vi chưa OK không chặn việc code (commit cục bộ, đổi lại được),
chỉ chặn đúng phần đó khi đưa lên staging (Task 244 bước 4).

## Tệp

| Tệp | Màn | Đường | Task màn hình | Ca |
|---|---|---|---|---|
| `tong-quan.html` | M1 Tổng quan | `/bao-cao/tong-quan` | 239 (M6-16) | BC01, BC05, C2, C3, C5 |
| `don.html` | M2 Báo cáo đơn | `/bao-cao/don` | 240 (M6-17) | BC02, C4, C5, C6 |
| `van-chuyen.html` | M3 Hãng vận chuyển | `/bao-cao/van-chuyen` | 240 (M6-17) | BC02 (tỷ lệ hãng), C6 |
| `ban-chay.html` | M4 Bán chạy | `/bao-cao/ban-chay` | 240 (M6-17) | BC02 (bán chạy) |
| `ton-kho.html` | M5 Tồn và xuất nhập tồn | `/bao-cao/ton-kho` | 241 (M6-18) | BC03 |
| `tep-xuat.html` | M6 Tệp xuất và chạy nền | `/bao-cao/tep-xuat` | 241 (M6-18) | BC04 |
| `nut-xuat.html` | Nút xuất trên màn Khách hàng (M2) và Sản phẩm (M1) | có sẵn | 241 (M6-18) | PQ06 |
| `khung.js`, `m6.css` | Menu (nhóm "Báo cáo"), thành phần mới | — | 239 | — |
| `soat.mjs`, `nen-anh.py` | Tự soát, chụp, nén ảnh | — | — | — |
| `*-1512.png`, `*-390.png`, `*-360.png` | Ảnh đề xuất; `hien-tai-*.png`: hai màn có sẵn | — | — | — |

Chụp lại: `PW_DIR=<thư mục node_modules/playwright> node docs/m6/mockup/soat.mjs` rồi
`python3 docs/m6/mockup/nen-anh.py`. Playwright lấy từ bộ đệm npx, không thêm vào repo; chỉ mở tệp HTML cục bộ (không
phải e2e).

## Dữ liệu cứng đã vẽ

- Tên sản phẩm 120 ký tự (Bán chạy, Tồn): cắt một dòng bằng dấu …, đủ tên ở `title`.
- Tên nhân viên dài (Báo cáo đơn, danh sách chi tiết, Tệp xuất): cắt bằng dấu …, đủ tên ở `title`.
- Tên tài khoản hãng dài (Hãng vận chuyển).
- 0 đơn (trạng thái trống), 1 đơn, 412 đơn (Xem thêm, 200 dòng mỗi lần), 1.200 SKU (danh sách cuộn; Bán chạy 50 → 500).
- Tỷ lệ "—" khi mẫu số 0 (Chưa phân công, Best Express, Không xác định); giá trị TB "—" khi không có đơn.
- Tiền 9 chữ số `123.456.789 ₫` vừa ô ở 360.
- Người chỉ có quyền một Page (Tổng quan, Báo cáo đơn) và một kho (Tồn): thấy dòng "Phạm vi", không thấy nút xuất.
- Đang tải, lỗi tải, 503 bận, chạy nền, quá nặng, 403, 429, 410.

Trạng thái "Hôm nay" của Tổng quan và trạng thái A, B, E, F của Báo cáo đơn dùng số của kịch bản chín đơn (Task 227,
`docs/m6/tieu-chi.md`): 8 đơn lên (O7 nháp không tính), 1 hủy (O6), 1.910.000 ₫ trên 7 đơn, TB 272.857 ₫, thành công 2
(O1, O9, 750.000 ₫), hoàn 1 (O2, 250.000 ₫), khách trả 1 cái 150.000 ₫, chờ xác nhận O4 / chờ gửi hãng O5 / đang yêu
cầu hủy O8; theo kênh Facebook 6 (5 chưa hủy, 1.300.000 ₫), Website 1 (360.000 ₫), Nhập tay 1 (250.000 ₫); người chỉ
có P1 thấy O1, O2, O8, O9 (4 đơn, 1.150.000 ₫). Báo cáo đơn nhóm theo nhân viên: An 4 (1.150.000 ₫, thành công 2, hoàn
1, 66,67%) / Bình 3 (400.000 ₫, hủy 1, "—") / Chưa phân công 1 (360.000 ₫) (Task 230, 240); ngăn "4 đơn" của An là O1,
O2, O8, O9. Bán chạy mức mẫu, đơn lên chưa hủy: Áo A 7 cái 4 đơn 1.050.000 ₫, Quần B 2 cái 500.000 ₫ trước Váy C 2 cái
360.000 ₫ (Task 232). Tồn: "Có thể bán 18" (Task 241). Các trạng thái khác (7 ngày, tháng này, hãng vận chuyển) là số
giả, cộng khớp trong từng bảng.

Phí ship thu khách vẽ theo từng đơn của bảng kịch bản (O1, O2, O4, O8, O9 mỗi đơn 30.000 ₫ → 150.000 ₫); dòng tổng
của tiêu chí ghi 120.000 ₫ — chênh lệch ghi ở "Việc phát hiện thêm" của `docs/tasks/task-224.md` để Task 227/229 chốt.

Mỗi cột vẽ đều có trường tương ứng trong kiểu dữ liệu của Task 229–235 (`DashboardReport`, `ChannelRow`, `DayRow`,
`OrdersRow`, `CarrierRow`, `BestSellerRow`, `StockRow`, `MovementRow`, `ReportRun`); không vẽ cột API không trả.

## Thay đổi hành vi so với Nhanh và so với màn có sẵn

Mỗi dòng cần chủ shop OK riêng. Cột "OK của chủ shop" để trống tới khi chủ shop trả lời trong chat.

| # | Màn, task bị ảnh hưởng | Hành vi đề xuất | So với Nhanh / màn có sẵn | Cách đổi lại | OK của chủ shop |
|---|---|---|---|---|---|
| (a) | Tổng quan, Báo cáo đơn (Task 239 `DrilldownPanel`, 240) | Bấm một ô số: trên máy tính (từ 1000 px) mở **ngăn bên phải** liệt kê đơn tạo nên số đó, tổng ở đầu ngăn bằng đúng số trong ô; báo cáo vẫn thấy, ô đang xem tô đậm; dưới 1200 px ngăn xuống dưới bảng. Trên điện thoại mở **trang riêng** có nút "← quay lại" (nút quay lại của trình duyệt cũng về báo cáo). Ngăn chỉ hiện mã đơn, mã khách, không tên/SĐT/địa chỉ; bấm mã đơn mở chi tiết đơn | CRM chưa có báo cáo. Cách Nhanh mở chi tiết chưa đối chiếu được (không có ảnh Nhanh trong repo): chủ shop ghi nếu quen cách khác | Đổi một thành phần (`DrilldownPanel`): mở trang riêng trên mọi cỡ màn, hoặc hộp thoại | |
| (b) | Tổng quan, Báo cáo đơn, Hãng vận chuyển, Bán chạy, Tồn (Task 239 `ExportButton`, 240, 241) | Nút **"Xuất Excel"** ở góc phải đầu trang, cạnh "Tải lại"; bấm là tạo lần chạy nền, thông báo "Đang tạo tệp, xem ở Tệp xuất", tệp tải ở màn Tệp xuất trong 7 ngày. Chỉ người có quyền xuất ở phạm vi toàn shop thấy nút | CRM chưa có báo cáo. Khác cách tải về ngay: mọi lần xuất chạy nền (Q9), tệp lấy ở màn Tệp xuất. Cách xuất của Nhanh chưa đối chiếu được (không có ảnh Nhanh trong repo) | Đổi chỗ đặt nút trong `ExportButton` (một thành phần dùng chung) | |
| (c) | Danh sách khách của M2 (`console/src/screens/customers`, Task 241) | Thêm nút **"Xuất danh sách khách"** ở đầu trang, giữa "Nhập Excel" và "Thêm khách"; mở hộp chọn bộ lọc theo `CustomerExportParams` của Task 238 (nhãn, loại khách, ngày tạo, ngày mua cuối, số đơn tối thiểu; không có ô gõ chữ); tệp có SĐT, hết hạn tải sau 24 giờ (việc tự xóa trên kho chờ quyết định G5); ba bước (yêu cầu, xong, tải) ghi nhật ký. Chỉ người có `customer.export` phạm vi toàn shop (đề xuất: Chủ, Quản lý — H11) | Màn khách hiện tại không có nút xuất | Bỏ nút khỏi màn khách (chỉ một chỗ gọi); API giữ nguyên | |
| (d) | Danh sách sản phẩm của M1 (`console/src/screens/products`) và tab Tồn của báo cáo tồn (Task 241 `ProductsExportButton`) | Thêm nút **"Xuất danh sách sản phẩm"** ở đầu trang sản phẩm (cạnh "Nhập Excel") và ở đầu màn Tồn; hộp chọn trạng thái mẫu và đầu mã SKU; tệp cột theo mẫu Nhập Excel, không có giá vốn, cột cuối "Có thể bán"; chạy nền như mọi tệp xuất | Màn sản phẩm hiện tại chỉ có "Nhập Excel" | Bỏ nút ở một hoặc cả hai chỗ | |

Không phải thay đổi hành vi của màn có sẵn, ghi để chủ shop thấy (đều theo kế hoạch đã duyệt):

- Nhóm menu mới "Báo cáo" giữa "Kho" và "Hệ thống" (quyết định thay chủ shop của Task 224; xem tệp task).
- Mục "Tổng quan" nằm trong nhóm Báo cáo. Đây là màn báo cáo mới của M6, không phải màn Tổng quan của admin cũ mà M5 đã bỏ
  (HV02 của M5).
- Tổng quan tự tải lại mỗi 60 giây khi đang mở (Q21); báo cáo quá 30 giây tự chuyển chạy nền (Q8).
