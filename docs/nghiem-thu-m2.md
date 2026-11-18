# Nghiệm thu M2 — Khách hàng, đơn hàng, mã giảm giá

Viết ở Task M2-00 (số toàn cục 57, 18/11/2026), trước khi có mã M2. Nguồn: kế hoạch
`docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (mục "Xong M2 khi", "Review Focus", "Ánh xạ ca kiểm M2 → task"),
spec `08-roadmap.md` hàng M2 và `11-kich-ban-kiem-thu.md` (các ca Mốc = M2). Kế hoạch là nguồn gốc; tệp này viết lại bằng
lời nghiệp vụ để chủ shop đọc và duyệt. Hai nơi khác nhau thì sửa cả hai trong cùng commit.

Cột "Kết quả" để trống tới khi task tương ứng xong; Task M2-32 điền lần cuối, kèm kết quả nhân viên chạy trên staging.
Số task là số nội bộ M2 (M2-NN); số toàn cục = 57 + NN (M2-33 = 90).

## Xong M2 khi

| # | Tiêu chí (lời nghiệp vụ) | Task M2 | Kết quả |
|---|---|---|---|
| 1 | **Lên đơn nhanh.** Nhân viên tìm khách bằng 4 số cuối SĐT hoặc tên không dấu, chọn hàng, bấm "Tạo đơn": tổng tiền đúng, đơn ở "Đã xác nhận", hàng được giữ ngay. Bấm "Tạo đơn" hai lần chỉ ra một đơn. | 07, 10, 16, 27 | |
| 2 | **Hai nhân viên tranh chiếc cuối.** Chỉ một đơn giữ được hàng, người kia thấy "hết hàng"; tồn không bao giờ âm. Sửa đơn tăng số lượng khi kho không đủ thì bị từ chối trọn vẹn, đơn cũ còn nguyên. | 16, 20 | |
| 3 | **Giảm giá không lệch một đồng.** Giảm cả đơn được chia về từng món; cộng lại đúng bằng số giảm. Giảm quá ngưỡng thì đơn chờ người có quyền duyệt. | 05, 07, 17, 19 | |
| 4 | **Mã giảm giá đúng lượt.** Mã có hạn và giới hạn lượt; còn một lượt mà hai đơn dùng cùng lúc thì chỉ một đơn được; hủy đơn đã dùng mã thì lượt được trả, không trả hai lần. Mã không hợp lệ bị từ chối kèm lý do tiếng Việt. | 06, 14, 17, 21 | |
| 5 | **Hồ sơ khách sạch.** SĐT người nhận (đặt hộ) không tự thành SĐT của khách; SĐT trùng hồ sơ khác chỉ gợi ý, không tự gộp; gộp nhầm hoàn tác được trong 30 ngày, đơn về đúng chỗ. Nhập danh sách khách bằng Excel không tạo trùng. | 09, 11, 13 | |
| 6 | **Hủy, trả, đổi đúng tồn.** Hủy đơn chưa gửi hãng: hàng về "có thể bán" ngay. Đơn đã gửi hãng: thành "yêu cầu hủy", chưa nhả hàng cho tới khi hãng xác nhận. Trả, đổi hàng ghi đúng kho (hàng tốt, hàng lỗi). | 18, 21, 22 | |
| 7 | **Đơn trùng, lịch sử, ghi chú.** Cùng SĐT, cùng sản phẩm trong vài giờ → đơn được đánh dấu "Đơn trùng" để nhân viên xử lý; mỗi đơn có lịch sử ai làm gì lúc nào. | 23, 24 | |
| 8 | **Tìm khách nhanh trên 1 triệu khách.** Tìm theo đuôi SĐT p95 dưới 300 ms (1 triệu khách giả). Tạo đơn p95 dưới 400 ms, đọc danh sách đơn p95 dưới 200 ms ở tải 10 lần bình thường, không deadlock, lần thử lại transaction cạn dưới 1%. | 31 | |
| 9 | **Đủ ca kiểm thử, màn hình đã duyệt.** 19 ca M2 của `11-kich-ban-kiem-thu.md` đạt; các ca nghiệm thu spec 10/2026 thuộc M2 đạt (AT05, AT06, AT07, AT21, AT28, AT29, WAT07 phần CRM; phần phía đơn của AT13, AT14, WAT28, WAT29, WAT31). Màn Lên đơn, Đơn, Khách, Mã giảm giá làm đúng mockup đã chốt (Điểm dừng B). Mutation testing vùng rủi ro cao đạt ngưỡng, review độc lập đạt. Nhân viên chạy phần "V" trên staging (cần OK của chủ shop, Điểm dừng C). | 26–30, 32 | |

## Tiêu chí → ca kiểm thử

"M" là máy tự kiểm (tên test ghi ở tệp task khi viết), "V" là nhân viên chạy ở Điểm dừng C.

| Ca | Tình huống (lời nghiệp vụ) | Máy kiểm ở | Màn hình, nhân viên | Kết quả |
|---|---|---|---|---|
| DH01 | Lên đơn nhanh: tổng đúng, Đã xác nhận, hàng tạm giữ | 07, 16 | 27, 32 (V) | |
| DH02 | Tìm khách bằng đuôi SĐT hoặc tên không dấu dưới 0,3 giây | 10, 31 | 29 | |
| DH04 | Đơn nhập tay, nguồn "Nhập tay" | 16 | 27, 32 (V) | |
| DH05 | Giảm cả đơn chia về từng món, không lệch 1 đồng | 05, 07, 17 | 27 | |
| DH06 | Lịch sử thao tác trên đơn | 24 | 28, 32 (V) | |
| DH07 | Bấm "Tạo đơn" hai lần | 16 | 27 | |
| DH08 | Hai nhân viên tranh chiếc cuối | 16 | — | |
| DH09 | Cùng SĐT, cùng sản phẩm trong vài giờ → "Đơn trùng" | 23 | 28, 32 (V) | |
| DH10 | Sửa tăng số lượng khi kho không đủ → từ chối trọn | 20 | 28 | |
| DH11 | Hai người cùng sửa một đơn → người sau được báo "đơn đã thay đổi" | 09, 19, 20 | 28 | |
| DH12 | Đặt hộ: SĐT người nhận không thành SĐT khách | 09, 16 | 27 | |
| DH13 | SĐT trùng hồ sơ khác → gợi ý; chỉ người có quyền mới gộp | 09, 11, 16 | 27, 29, 32 (V) | |
| DH14 | Gộp nhầm → hoàn tác trong 30 ngày, đơn về đúng chỗ | 11, 25 | 29 | |
| DH16 | Giảm quá mức → chờ người có quyền duyệt | 17, 19, 20 | 27, 28 | |
| KH11 | Đổi giá: đơn cũ giữ giá, đơn mới giá mới | 16, 20 | — | |
| TD01 | Hủy đơn chưa gửi hãng → hàng về "có thể bán" ngay | 21 | 28, 32 (V) | |
| KM01 | Mã có hạn và giới hạn lượt; hết lượt thì từ chối | 06, 14, 17 | 30, 32 (V) | |
| KM02 | Mã hết hạn hoặc không đủ điều kiện → từ chối, nêu lý do | 06, 14, 17 | 27, 30 | |
| KM03 | Hủy đơn đã dùng mã → lượt được trả | 06, 21 | — | |

Ca của mốc sau mà M2 phủ phần phía đơn (nghiệm thu đầy đủ ở mốc của chúng): DH03, DH15, TD02, TD03, TD05–TD08, WB11,
WB12, KH14, PQ02, PQ08 (xem kế hoạch M2, mục "Ánh xạ ca kiểm").

## Vùng rủi ro cao của M2

`internal/modules/order/**`, `internal/modules/promotion/**`, `internal/modules/customer/{merge.go,fororder.go,stats.go}`,
phần M2 thêm vào `internal/modules/inventory` (`byitems.go`, `returns.go`), migration 0201–0208. Có test sinh tình huống
ngẫu nhiên (rapid) và phải qua mutation testing ở Task M2-32.

## Ngoài phạm vi M2

Gửi đơn cho hãng thật, đối soát COD, hoàn tiền (M3); khung chat, tạo đơn từ hội thoại (M4); đơn từ website, flash sale
(M5); báo cáo (M6); tài khoản khách trên web (M7). M2 không gọi dịch vụ ngoài nào và không đọc dữ liệu Nhanh.
