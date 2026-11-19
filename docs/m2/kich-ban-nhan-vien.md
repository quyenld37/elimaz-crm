# Kịch bản nhân viên chạy trên staging — phần "V" của mốc M2

Viết ở Task M2-32 (số toàn cục 89, 19/11/2026). Dùng ở **Điểm dừng C**, chỉ khi chủ shop đã OK riêng hai việc: (1) triển
khai bản M2 lên staging; (2) cho 1–2 nhân viên chạy kịch bản này (khoảng 2 giờ). Máy đã tự kiểm phần "M" của từng ca
(tên test ở `docs/nghiem-thu-m2.md`); phần này kiểm điều máy không thấy: màn hình dễ hiểu, đúng chữ, bấm được trên máy
thật của shop.

Cách ghi: mỗi bước đánh dấu **Đạt** hay **Không đạt**; không đạt thì ghi điều đã thấy (chụp màn hình nếu được) ở cột
Ghi chú. Không ghi SĐT, địa chỉ thật của khách vào tệp này — dùng dữ liệu thử bên dưới.

Người chạy: ……………… Ngày: ……………… Bản đang chạy (mã commit ở trang Sức khỏe): ………………

## Chuẩn bị (người làm kỹ thuật, trước buổi chạy)

- Tài khoản: một nhân viên vai trò **Bán hàng** (`sale`), một **Quản lý** (`manager`).
- Sản phẩm thử: mẫu "Áo thử M2", hai biến thể **A-M** (giá 250.000đ, tồn 3) và **A-L** (giá 250.000đ, tồn 1), ở kho chính.
- Khách thử: **Chị Thử Một**, SĐT `0900000101` (đã có một đơn giao xong); **Anh Thử Hai**, chưa có SĐT.
- Mã giảm giá: `THUM2` giảm 10%, hạn từ hôm nay tới hết tháng, **tối đa 2 lượt**.
- Ghi số tồn "Có thể bán" của A-M, A-L ở màn Sản phẩm trước khi bắt đầu.

## DH01 — Lên đơn nhanh: tổng đúng, Đã xác nhận, hàng tạm giữ

| # | Bước | Mong đợi | Đạt / Không đạt | Ghi chú |
|---|---|---|---|---|
| 1 | Nhân viên Bán hàng mở **Đơn → Lên đơn** (`/don/moi`). Ô "Tìm khách theo đuôi SĐT, tên hoặc mã KH" gõ `0101` | Hiện "Chị Thử Một" trong vòng chưa tới 1 giây | | |
| 2 | Chọn Chị Thử Một. "Thêm dòng": A-M số lượng 2. Phí ship 30.000đ. Giảm dòng 20.000đ | Tạm tính 500.000đ, Tổng = 500.000 − 20.000 + 30.000 = **510.000đ**, Thu hộ (COD) 510.000đ | | |
| 3 | Nguồn đơn: "Nhập tay". Bấm **Lưu và xác nhận** | Báo tạo đơn thành công, có mã đơn `DH…`; đơn ở trạng thái **Đã xác nhận** | | |
| 4 | Mở màn **Sản phẩm**, xem A-M | "Có thể bán" giảm đúng 2 so với lúc chuẩn bị | | |
| 5 | Bấm **Lưu và xác nhận** hai lần thật nhanh ở một đơn mới (A-M số lượng 1) | Chỉ ra **một** đơn mới trong danh sách Đơn | | |

## DH04 — Đơn nhập tay, nguồn "Nhập tay"

| # | Bước | Mong đợi | Đạt / Không đạt | Ghi chú |
|---|---|---|---|---|
| 1 | Mở đơn vừa tạo ở DH01 từ danh sách **Đơn** | Mục "Phụ trách, nguồn" ghi nguồn **Nhập tay** | | |
| 2 | Ở danh sách Đơn, lọc nguồn "Nhập tay" | Đơn vừa tạo có trong danh sách | | |

## DH06 — Lịch sử thao tác trên đơn

| # | Bước | Mong đợi | Đạt / Không đạt | Ghi chú |
|---|---|---|---|---|
| 1 | Mở đơn DH01, bấm sửa: đổi số lượng A-M từ 2 thành 1, **Lưu thay đổi** | Tổng tính lại đúng; đơn vẫn Đã xác nhận | | |
| 2 | Thêm ghi chú nội bộ "khách hẹn giao chiều" | Ghi chú hiện ở mục "Ghi chú nội bộ" kèm tên người viết | | |
| 3 | Xem mục **Lịch sử thao tác** ("Ai làm, lúc nào, đổi gì") | Có đủ: tạo đơn, xác nhận, sửa (đổi gì), ghi chú; đúng tên nhân viên, giờ; mới nhất ở trên; **không** hiện SĐT hay địa chỉ trong dòng lịch sử | | |

## DH09 — Cùng SĐT, cùng sản phẩm trong vài giờ → "Đơn trùng"

| # | Bước | Mong đợi | Đạt / Không đạt | Ghi chú |
|---|---|---|---|---|
| 1 | Tạo thêm một đơn cho Chị Thử Một, A-M số lượng 1, **Lưu và xác nhận** | Đơn tạo được, không bị chặn | | |
| 2 | Đợi khoảng 1 phút, mở **Đơn → Đơn trùng** (`/don/trung`) | Hiện cặp gồm đơn DH01 và đơn vừa tạo; hai đơn **không** bị tự hủy | | |
| 3 | Ở danh sách Đơn | Hai đơn có nhãn "Nghi trùng" | | |
| 4 | Ở cặp đơn trùng, chọn **Hủy đơn này** cho đơn mới, nhập lý do, xác nhận | Đơn mới thành Đã hủy; cặp biến khỏi màn Đơn trùng; A-M "Có thể bán" tăng lại 1 | | |

## DH13 — SĐT trùng hồ sơ khác → gợi ý; chỉ người có quyền mới gộp

| # | Bước | Mong đợi | Đạt / Không đạt | Ghi chú |
|---|---|---|---|---|
| 1 | Bán hàng lên đơn cho **Anh Thử Hai** (khách có sẵn), SĐT người nhận `0900000101` (số của Chị Thử Một), **Lưu và xác nhận** | Đơn tạo được cho Anh Thử Hai; hai hồ sơ **không** tự gộp; hồ sơ Anh Thử Hai vẫn "chưa có SĐT" | | |
| 2 | Mở hồ sơ Anh Thử Hai (`/khach/…`) | Hiện "Có thể là cùng một khách:" Chị Thử Một. Với Bán hàng: không có nút gộp, hoặc bấm thì báo không có quyền | | |
| 3 | Quản lý mở hồ sơ Anh Thử Hai → **Gộp với khách khác** → chọn Chị Thử Một → chọn hồ sơ đại diện → **Gộp** | Hai hồ sơ thành một khách; số đơn, "Đã chi" cộng của cả hai | | |
| 4 | Quản lý bấm **Hoàn tác** lần gộp vừa rồi | Hai hồ sơ tách lại như trước; đơn của ai về đúng người đó | | |

## TD01 — Hủy đơn chưa gửi hãng → hàng về "có thể bán" ngay

| # | Bước | Mong đợi | Đạt / Không đạt | Ghi chú |
|---|---|---|---|---|
| 1 | Lên đơn A-L số lượng 1 (chiếc cuối), **Lưu và xác nhận** | A-L "Có thể bán" về 0 | | |
| 2 | Mở đơn → **Hủy đơn** → chọn lý do → xác nhận | Đơn thành **Đã hủy**; lịch sử có dòng hủy kèm lý do | | |
| 3 | Mở màn Sản phẩm, xem A-L | "Có thể bán" về lại 1 ngay (không cần chờ) | | |
| 4 | Lên đơn mới A-L số lượng 1 | Tạo được | | |

## KM01 — Mã có hạn và giới hạn lượt; hết lượt thì từ chối

| # | Bước | Mong đợi | Đạt / Không đạt | Ghi chú |
|---|---|---|---|---|
| 1 | Quản lý mở **Mã giảm giá** (`/ma-giam-gia`) | Thấy mã `THUM2`, Đã dùng 0/2, Đang bật | | |
| 2 | Bán hàng lên đơn (A-M số lượng 1), ô mã gõ `THUM2` | Báo "Áp được: giảm 25.000đ · còn 2 lượt"; tổng trừ đúng 25.000đ | | |
| 3 | **Lưu và xác nhận**. Lên đơn thứ hai dùng `THUM2` | Đơn thứ hai cũng áp được (còn 1 lượt), lưu được | | |
| 4 | Lên đơn thứ ba, gõ `THUM2` | Bị từ chối, lý do tiếng Việt dễ hiểu (hết lượt); không lưu được đơn kèm mã | | |
| 5 | Hủy một trong hai đơn đã dùng mã; quay lại màn Mã giảm giá | Đã dùng còn 1/2 (lượt được trả) | | |
| 6 | Bán hàng thử mở **Tạo mã** | Không có nút hoặc báo không có quyền | | |

## Kết luận buổi chạy

| Ca | Đạt / Không đạt | Ghi chú |
|---|---|---|
| DH01 | | |
| DH04 | | |
| DH06 | | |
| DH09 | | |
| DH13 | | |
| TD01 | | |
| KM01 | | |

Nhận xét chung của nhân viên (chữ có dễ hiểu không, chỗ nào phải hỏi, chỗ nào chậm): ………………
