# Tiêu chí nghiệm thu M3 — Trung tâm vận chuyển và tiền COD

Viết ở Task M3-00 (số toàn cục 91, 20/11/2026), trước khi có mã M3, theo mẫu `docs/nghiem-thu-m2.md`. Nguồn: kế hoạch
`../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (mục "Xong M3 khi", "Review Focus", "Bảng ca M3 → task",
"Ngoài phạm vi M3"), spec `08-roadmap.md` hàng M3 và `11-kich-ban-kiem-thu.md` (40 ca có Mốc = M3). Kế hoạch là nguồn gốc;
tệp này viết lại bằng lời nghiệp vụ để chủ shop đọc và duyệt. Hai nơi khác nhau thì sửa cả hai trong cùng commit.

Cột "Kết quả" để trống tới khi task tương ứng xong; Task M3-49 (140) điền lần cuối, kèm kết quả nhân viên chạy trên
staging (Điểm dừng C). Số task là số nội bộ M3 (M3-NN); số toàn cục = 91 + NN. Task mới tách ra ở Task M3-00 mang số
440–459 (xem `docs/m3/doi-chieu-m2.md`).

**Hãng thật:** khóa API và tài liệu của Viettel Post, Best, SPX **chưa có** (chủ shop đang xin). Mọi task M3 trước Phần D
chạy với hãng giả (`fake`) và chế độ nhập mã tay; ca nào chỉ kiểm được với hãng thật ghi rõ "chờ khóa hãng".

## Xong M3 khi

| # | Tiêu chí (lời nghiệp vụ) | Task M3 | Kết quả |
|---|---|---|---|
| 1 | **Gửi hãng thật chạy được.** Ở môi trường thử của cả ba hãng: tạo vận đơn, hủy, theo dõi trạng thái tự về. Hãng nào không cấp API thì đường nhập mã tay chạy trọn: tạo trên trang hãng, dán mã, theo dõi tay, đối soát. | 9–15, 34–39, 49 | |
| 2 | **Bảng kê thật khớp.** Một tệp đối soát COD thật của mỗi hãng (chủ shop gửi) nhập vào CRM khớp đúng từng dòng, tách đúng phí. | 31, 32, 35, 37, 39, 49 | |
| 3 | **Đủ 40 ca kiểm thử.** 40 ca M3 ở bảng dưới đạt: phần "M" bằng test máy, phần "V" do nhân viên kho, kế toán, bán hàng chạy trên staging. | 1–48 | |
| 4 | **Mutation testing** vùng rủi ro cao của M3 (máy trạng thái vận đơn, áp kết quả tra, tạo/hủy vận đơn, sửa tay, toàn bộ `cash`, phần M3 của `order`, `inventory`) đạt ngưỡng 80%. | 49 | |
| 5 | **Review độc lập** toàn mốc không còn phát hiện về tính đúng. | 49 | |
| 6 | **Đối chiếu đêm** của sổ COD và sổ tồn chạy 7 đêm liền trên staging không có lệch. | 33, 49 | |

## Năm tình huống dễ gây hại nhất (Review Focus) — phải có test

| # | Tình huống (lời nghiệp vụ) | Mong đợi | Task |
|---|---|---|---|
| 1 | Bấm "Gửi hãng" hai lần, hai người gửi cùng lúc, máy chết ngay sau khi hãng đã tạo | Đúng một vận đơn còn hiệu lực; hãng nhận lệnh tạo một lần; "không rõ" thì tra hãng trước, không tra được thì chờ người | 12, 14, 48 |
| 2 | Tin hãng lặp, sai thứ tự, thiếu tin "đã lấy hàng", giao thất bại rồi giao lại, giao một phần, đảo "Thành công" | Mỗi món hàng chỉ xuất kho một lần; đối chiếu tồn đêm không lệch | 5, 7, 8, 26, 27, 48 |
| 3 | Cùng bảng kê nhập hai lần, hai kế toán chốt cùng lúc, một vận đơn có trong hai bảng kê | Mỗi dòng bảng kê ghi tiền đúng một lần; bút toán lệch hai vế bị database từ chối | 2, 29, 32, 33 |
| 4 | Người ngoài biết mã vận đơn gửi tin "đã giao"; hai tài khoản trùng mã vận đơn | Đơn không đổi theo tin giả; trạng thái lấy lại từ hãng; tìm vận đơn luôn theo tài khoản | 10, 11, 32 |
| 5 | Hủy khi hãng chưa xác nhận; tin muộn của vận đơn cũ sau khi đổi hãng; hãng từ chối hủy vì đã lấy hàng | Hàng chỉ nhả khi hãng xác nhận hủy; tin cũ không động tới đơn mới; đơn quay về đang giao | 8, 14 |

## 40 ca M3 → ai kiểm

Cột "Máy kiểm ở" là task có test tự động ("M"); cột "Nhân viên kiểm" là màn hình và buổi chạy thử trên staging ("V",
Task M3-49 bước 5). "Phần M2 đã kiểm" ghi phần lõi do M2 làm và đã có test trên `main` (M2 Task 79 = M2-22, 75 = M2-18,
78 = M2-21, 82 = M2-25); M3 chỉ kiểm phần còn lại.

### Đơn hàng, kho

| Ca | Tình huống (lời nghiệp vụ) | Kết quả mong đợi | Phần M2 đã kiểm | Máy kiểm ở (M3) | Nhân viên kiểm | Kết quả |
|---|---|---|---|---|---|---|
| DH03 | Mở khung khách cạnh khung chat khi lên đơn | Thấy lịch sử mua, số lần nhận, số lần hoàn và mức "khách hay hoàn" (gợi ý xin cọc hoặc gọi xác nhận) | Số nhận/hoàn và tỷ lệ (M2-25) | 21 (mức rủi ro) | 43, 49 (sale) | |
| DH15 | Khách đổi địa chỉ sau khi đơn đã gửi hãng | Yêu cầu đổi phải qua hãng (API hoặc nhân viên liên hệ hãng xác nhận); địa chỉ trên đơn CRM chỉ đổi khi hãng đã nhận | Chặn sửa địa chỉ đơn đã gửi hãng (`order_locked_by_shipment`) | 6, 17 | 43, 49 (sale) | |
| KH09 | Hàng giữ cho đơn quá 20 ngày, hoặc đang giao quá 180 ngày | Đơn vào danh sách "Giữ hàng quá hạn" để người xử lý; máy không tự trả hàng về kho | — | 22 | 44 (kho) | |

### Vận chuyển

| Ca | Tình huống (lời nghiệp vụ) | Kết quả mong đợi | Phần M2 đã kiểm | Máy kiểm ở (M3) | Nhân viên kiểm | Kết quả |
|---|---|---|---|---|---|---|
| VC01 | Chủ shop kết nối một tài khoản hãng | Nút "Kiểm tra kết nối" báo đạt; khóa API lưu mã hóa, không hiện lại trên màn, không lọt vào log | — | 9 | 41, 49 (chủ shop) | |
| VC02 | So phí các hãng ngay trên đơn | Phí từng hãng hiện cạnh nút gửi; hãng chậm hay lỗi không chặn hãng khác | — | 13; 34, 36, 38 (chờ khóa hãng) | 42, 49 (sale) | |
| VC03 | Gửi hãng | Có mã vận đơn; đơn hiện "Chờ thu gom" | Gắn vận đơn vào đơn (M2-18) | 12; 34, 36, 38 (chờ khóa hãng) | 42, 49 (sale, kho) | |
| VC04 | In nhãn và đóng gói | In đúng nhãn của hãng; in lại không làm lùi tiến độ đóng gói | — | 6, 16 | 45, 49 (kho) | |
| VC05 | Hãng đến lấy hàng | Trạng thái tự về; hàng chuyển từ "giữ" sang "đang giao" | Đơn đổi khi hãng lấy hàng (M2-18) | 8, 11 | 43 | |
| VC06 | Hãng giao thành công | Đơn "Thành công"; xuất kho đúng một lần | Xuất kho một lần phía đơn (M2-18) | 5, 8 | 43 | |
| VC07 | Giao thất bại rồi giao lại | Đơn vào danh sách "Cần gọi lại" có hạn gọi; không xuất kho hai lần | — | 8, 19 | 44, 49 (sale) | |
| VC08 | Hãng báo cùng một trạng thái ba lần | Đơn, tồn và tiền COD chỉ đổi một lần | Phía đơn, tồn (M2-18) | 7, 8, 29 | — | |
| VC09 | Tin "Đang giao" đến sau tin "Thành công" | Không lùi trạng thái; tin chỉ ghi vào lịch sử vận đơn | — | 7, 8 | — | |
| VC10 | Tin trạng thái của hãng về trước khi CRM nhận kết quả tạo vận đơn | Tin được giữ lại, khớp sau, áp đúng một lần | — | 10, 11, 12 | — | |
| VC11 | Hủy vận đơn rồi gửi lại bằng hãng khác | Đơn về "Đã xác nhận" rồi gửi hãng mới; tin đến muộn của vận đơn cũ không động tới đơn | Gỡ vận đơn (M2-18) | 7, 8, 14 | 43 | |
| VC12 | Hãng từ chối hủy vì đã lấy hàng | Đơn quay lại như cũ (đang giao); hàng không bị trả về sổ | Đơn `cancel_requested` nhận tin "đã lấy hàng" (Task 431) | 6, 7, 14 | 43 | |
| VC13 | Hai tài khoản hãng có mã vận đơn trùng nhau | Không ghép nhầm đơn; vận đơn sau vào "Cần xem" | — | 1, 10, 32 | — | |
| VC14 | Một hãng không cho phần mềm nối thẳng | Nhân viên tạo vận đơn trên trang của hãng rồi dán mã; CRM vẫn theo dõi (cập nhật tay) và đối soát | — | 15 | 43, 49 (kho, kế toán) | |
| VC15 | Tài khoản hãng bị thu hồi quyền | Tài khoản báo "không sẵn sàng"; không tự đổi sang tài khoản khác; đơn cũ vẫn tra được | — | 9, 12 | 41 | |
| VC16 | Lệnh tạo vận đơn quá giờ nhưng hãng thật ra đã tạo | Lệnh thành "không rõ"; máy tra hãng theo mã nội bộ; chỉ có một vận đơn | Lệnh tạo hỏng thì vận đơn `creating` thành hủy (Task 408) | 12, 20, 48 | 42, 44 | |
| VC17 | Có người giả làm hãng gửi tin "đã giao" | Đơn không đổi theo tin; trạng thái thật được lấy lại từ hãng bằng API có xác thực | — | 10, 11 | — | |
| VC18 | Hãng ngừng báo trạng thái | Máy tra bù theo lịch; màn hiện "cập nhật lần cuối lúc…"; đơn vào "Đơn kẹt" và có cảnh báo hằng ngày (chỉ số lượng) | — | 11, 20 | 43, 44 | |

### Tiền

| Ca | Tình huống (lời nghiệp vụ) | Kết quả mong đợi | Phần M2 đã kiểm | Máy kiểm ở (M3) | Nhân viên kiểm | Kết quả |
|---|---|---|---|---|---|---|
| TI01 | Giao xong nhưng hãng chưa chuyển tiền | Tiền nằm ở ngăn "hãng đang giữ"; chưa tính là tiền đã về | — | 2, 29 | 43 | |
| TI02 | Kế toán nhập bảng kê của hãng | Tự khớp từng dòng theo mã vận đơn; tách phí của hãng | — | 31, 32; 35, 37, 39 (chờ tệp thật) | 46, 49 (kế toán) | |
| TI03 | Đối chiếu sổ tiền mỗi đêm | Tổng mọi ngăn bằng 0; số dư từng vận đơn khớp trạng thái vận đơn | — | 33 | 49 (7 đêm trên staging) | |
| TI04 | Nhập cùng một bảng kê hai lần | Lần hai bị từ chối | — | 31 | 46 | |
| TI05 | Hãng chuyển thiếu hoặc lệch số tiền | Vào danh sách "Lệch đối soát"; phần thiếu vẫn còn mở | — | 32 | 46, 49 (kế toán) | |
| TI06 | Bảng kê có dòng của đơn Nhanh cũ | Dòng được đánh dấu "đơn ngoài hệ thống"; không tính vào số liệu | — | 32 | 46, 49 (kế toán) | |
| TI07 | Hai kế toán cùng xử lý một bảng kê | Người lưu sau bị chặn; không ghi trùng tiền | — | 32, 48 | 46 | |
| TI08 | Sửa nhầm một đơn "Thành công" đã có tiền | Xem trước ảnh hưởng; ghi dòng đảo, không xóa dòng cũ | — | 27, 33 | 47 | |
| TI09 | Hủy đơn đã nhận cọc | Ghi nghĩa vụ hoàn đúng số đã thu; chưa có phiếu chi thì chưa coi là đã hoàn. Chính sách mất cọc chưa chốt: người có quyền đóng "không hoàn" kèm lý do | — | 30 | 47 | |
| TI10 | Phần mềm lỡ ghi một bút toán lệch hai vế | Database từ chối ngay | Sổ COD tối thiểu của M1 đã có trigger kiểm cân | 2 | — | |

### Trả, đổi, hoàn

| Ca | Tình huống (lời nghiệp vụ) | Kết quả mong đợi | Phần M2 đã kiểm | Máy kiểm ở (M3) | Nhân viên kiểm | Kết quả |
|---|---|---|---|---|---|---|
| TD02 | Hàng hoàn về tới kho | Hãng báo "đã hoàn" chưa đổi tồn; chỉ khi kho bấm nhận thì tồn mới tăng | Phiếu hoàn của hãng tự tạo khi vận đơn bắt đầu hoàn (M2-22) | 5, 23 | 45, 49 (kho) | |
| TD03 | Hủy đơn đã gửi hãng | Đơn "đang yêu cầu hủy"; máy gửi lệnh hủy cho hãng; hàng chỉ trả về sổ khi hãng xác nhận hủy | Đơn sang `cancel_requested`, nhả hàng khi hãng xác nhận (M2-21) | 6, 14 | 43, 49 (sale) | |
| TD04 | Kiện hoàn về có món tốt, món hỏng, món thiếu | Món tốt về kho bán được, món hỏng vào "lỗi", món thiếu ghi thất lạc và vào "Cần xem" để khiếu nại | Nhận phiếu theo tình trạng cả dòng (M2-22) | 5, 23 | 45, 49 (kho) | |
| TD05 | Khách trả một trong hai món sau khi đã nhận | Phiếu trả theo từng món, không vượt số đã mua; kho nhận theo số lượng; tiền hoàn thành nghĩa vụ hoàn | Phiếu trả, giới hạn số trả (M2-22) | 24, 30 | 47, 49 (sale, kho) | |
| TD06 | Khách đổi size | Một phiếu trả và một đơn mới; không sửa đơn gốc; giá trị món trả cấn trừ vào COD đơn mới | Phiếu đổi và đơn thay thế (M2-22) | 25 | 47, 49 (sale) | |
| TD07 | Khách gửi hai yêu cầu trả cho cùng một món | Tổng số trả không vượt số đã mua | Toàn bộ (M2-22) | 24 (chạy lại sau khi thêm nhận theo số lượng) | — | |
| TD08 | Đã duyệt yêu cầu trả nhưng chưa nhận lại hàng | Chưa tăng tồn, chưa hoàn tiền | Phía tồn (M2-22) | 24 (chạy lại), 30 (chưa sinh nghĩa vụ hoàn) | 47 | |
| TD09 | Khách chỉ nhận một phần kiện hàng | Người có quyền ghi món nào khách nhận và COD thực thu; phần còn lại theo luồng hoàn | — | 5, 6, 26 | 47, 49 (kho, kế toán) | |
| TD10 | Hãng báo "đã giao" rồi sửa lại thành "hoàn" | Ghi bù tồn, tiền COD (bút toán đảo) và chỉ số khách; nếu tiền đã về shop thì không tự đổi tiền mà vào "Cần xem" cho kế toán | — | 5, 6, 7, 8, 27, 29 | 47 | |

## Ca nghiệm thu của spec tháng 10 (`core-e2e/03`) được phủ thêm

AT08 (Task 6, 16), AT10 (8), AT11 (23), AT12 (23), AT13 (M2-22; 24), AT14 (M2-22; 25), AT15 (29), AT16 (32), AT17 (30),
AT30 (32), AT40 (27, 33), AT41 (9), AT42 (10, 32), AT44 (12), AT45 (11, 12), AT46 (10, 11), AT47 (8; phần đơn, tồn
M2-18 đã đạt, M3 thêm "một kết quả tài chính"), AT48 (7, 8), AT49 (14), AT50 (11, 20), AT51 (8), AT53 (22), AT59 (14),
WAT27 (17), WAT28 (M2-22; 24), WAT29 (M2-22; 24). Phần AT06 còn lại cho M3 (dispatch, payment): Task 12, 30.

## Việc điều phối ghi cho M3 phải có test

| Việc | Nguồn | Task M3 | Mong đợi |
|---|---|---|---|
| Lệnh tạo vận đơn thất bại khi đơn đang `cancel_requested` | Task 431 | 12 | Vận đơn `creating` thành hủy (`create_rejected`), đơn thành "Đã hủy" như hãng xác nhận hủy (hàm `Writer.DetachShipment` đã nhận `cancel_requested`); không đơn nào kẹt |
| Hai ca vận đơn kẹt `creating` | Task 408 | 12, 20 | Lệnh tạo kết thúc hỏng thì vận đơn thành hủy (đã có, Task 408); lệnh `in_doubt` hoặc `dead` mà vận đơn còn `creating` thì vào "Đơn kẹt" và "Cần xem" |
| Đơn bị chặn lấy hàng vì kho thiếu (`stock_short`) | Task 406 | 12, 20 | Gửi hãng đơn có hàng giữ thiếu bị từ chối rõ lý do, hoặc hiện cảnh báo trên đơn và "Cần xem" |
| `order.returned` có hai nghĩa (hãng hoàn trọn, khách trả trọn) | Task 79 | 23 | `fulfillment` chỉ chuyển vận đơn `returning → returned` khi sự kiện mang `shipmentId` của phiếu `carrier_return` |
| Đơn đang chờ hãng hủy mà hãng chuyển hoàn | Task 79 | 8, 14 | Đơn rời `cancel_requested` đúng một đường; tồn đi theo luồng hoàn, không nhả hai lần |
| Kho nhận phiếu hoàn khi vận đơn chưa ở `returning` | Task 79 | 23 | Nhận phiếu `carrier_return` chỉ khi vận đơn đã `returning`; vận đơn sang `returned` cùng transaction |

## Ngoài phạm vi M3 (không kiểm ở mốc này)

Biên bản bàn giao cho hãng, khiếu nại vận chuyển, quét mã khi đóng gói, giao một phần tự động, bảng điểm hãng và tự
gợi ý hãng (P2); phiếu trả, đổi và giới hạn số trả (M2 đã làm); adapter nền tảng gom hãng (chỉ khi một hãng từ chối API
và chủ shop chọn nền tảng); quy tắc phí ship báo khách và danh sách hãng trên website (M5); báo cáo theo hãng (M6); thu
hồi hoa hồng khi đảo "Thành công" (module CTV, P2); thông báo trạng thái cho khách và trang tra vận đơn cho khách (P2,
M5); token bucket theo tài khoản (M4); mã hóa lại hàng loạt khi xoay khóa (trước ngày mở); đổi COD sau khi đã gửi hãng
qua API hãng (chưa có ở MVP: hủy và gửi lại).
