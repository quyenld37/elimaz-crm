# Mockup M7: Kiểm kho mở sổ và Hàng hoàn đơn cũ (Task M7-02, số toàn cục 247)

Mockup cho màn 10 và 13 trong bảng màn của Task 247 (kế hoạch `docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md`); màn sẽ code ở Task 273 (M7-28). Đợt này (điều phối M7, 28/11/2026) chỉ vẽ và chốt hai màn này; mười ba màn còn lại vẽ cùng task màn của chúng ở đợt sau (danh sách và trạng thái: `docs/khoi-dong/mockup-m7.md`). Mở `index.html` để xem ảnh "Hiện tại / Đề xuất", bảng ca và bảng hành vi.

- Hệ thiết kế: dùng nguyên `../m1/m1.css` (đã duyệt ở M1, console dùng đúng token đó), `../m2/m2.css`, `../m3/m3.css`. `m7.css` chỉ thêm thành phần mới (thanh tiến độ đếm, nút ±1 lớn, dòng lần quét có trạng thái gửi, ứng viên khi mã trùng hai người bán, lựa chọn có chữ, nút tình trạng Bán lại được / Hỏng, dòng hàng hoàn, nhóm theo ngày) bằng biến có sẵn: không màu, cỡ chữ, bo góc mới.
- Khung: `khung.js` (nhóm menu theo `console/src/app/nav.ts` trên main 28/11: Hộp chat · Đơn hàng · Vận chuyển · Tiền · Khách hàng · Khuyến mãi · Website · Sản phẩm · Kho · Báo cáo · Hệ thống · Quản trị; mỗi nhóm chỉ ghi vài mục tiêu biểu, nhóm Kho đủ mục). Mockup vẽ với tài khoản nhân viên kho.
- Ảnh đề xuất: `shots/<màn>-1512.png` (1512 × 767), `-390.png` (390 × 844), `-360.png` (360 × 800). Chụp cả trang.
- Ảnh hiện tại: dùng lại ảnh mockup đã duyệt `../m1/png/kho-kiem-kho-*.png` (Kiểm kho thường) và `../m3/png/kho-*.png` (trạng thái D: Nhận hàng hoàn của đơn CRM); không chép ảnh.
- Tự soát và chụp lại: `PW_CHANNEL=chrome PW_DIR=<thư mục node_modules/playwright> node docs/mockups/m7/soat.mjs` (Playwright trong bộ đệm npx, không thêm vào repo; `PW_CHANNEL=chrome` dùng Google Chrome của máy khi chưa có trình duyệt của Playwright), rồi `python3 docs/mockups/m7/nen-anh.py`. Kiểm như M5 (không cuộn ngang, không tràn, không chữ bị cắt thiếu dấu …, mục chạm ≥ 44 px dưới 1000 px và ≥ 32 px từ 1000 px, chỉ token có sẵn, ô chọn đúng, không gì chỉ hiện khi rê chuột, đủ khung kế hoạch đòi, không thẻ script ngoài `khung.js`) và thêm: nút "Đăng" khóa ở mọi trạng thái còn mã chưa đếm.
- Dữ liệu là dữ liệu giả. Khung viền đứt "Mockup:" là ghi chú, không phải giao diện. Dữ liệu khó: tên sản phẩm 80 và 86 ký tự, 412 mã phải đếm, 3.000 dòng Excel, 7 kiện trong 3 ngày, 0 / 1 / nhiều dòng; lỗi tải, 403, 409, 422.
- Đường dẫn ghi trong bảng (`/kho/mo-so`, `/kho/hang-hoan-cu`) là đề xuất theo cách đặt tên của console (`/kho/kiem-kho`, `/kho/phieu`); Task 273 chốt đường thật.

## Màn → ca kiểm → trạng thái đã vẽ

| Màn | Đường, task màn hình | Ca | Trạng thái đã vẽ | Tệp | Ảnh hiện tại |
|---|---|---|---|---|---|
| (10) Kiểm kho mở sổ | Kho → Kiểm kho → Mở sổ · `/kho/mo-so`, `/kho/mo-so/:id` · Task 273 (M7-28) | MO02 | A lối vào từ Kiểm kho, tạo phiên (kho trống, báo số mã phải đếm), kho đã có hàng (`warehouse_not_empty`), đã có phiên mở sổ (`opening_exists`), đang kiểm thường (`stocktake_open`) · B đếm bằng quét mã trên điện thoại: mỗi lần quét +1, nút −1, số là tổng của mọi người kèm người đếm lần cuối, mã lạ (`unknown_code`), mã trùng hai người bán (`ambiguous_code`, chọn ứng viên), không bớt dưới 0 (`negative_count`), mất mạng: lần quét chờ gửi, gửi lại cùng khóa · C còn 7 mã chưa đếm (từ máy chủ), Nhập 0, nút Đăng khóa · D nhập Excel: Đặt bằng số trong tệp / Cộng thêm, đang đọc tệp, kết quả và lỗi từng dòng (`unknown_code`, `ambiguous_code`, `not_integer`, `formula`), tệp đã cộng (`import_already_applied`), tệp sai loại/cỡ, không đọc được (`failed`) · E xác nhận đăng, còn mã chưa đếm (`uncounted_lines`), người khác đã đăng (`stocktake_not_open`), kho có phiếu vào giữa chừng (`warehouse_not_empty`) · F đã mở sổ, kiểm lại khớp / lệch / đã có phiếu khác sau giờ mở bán, sửa bằng phiếu điều chỉnh · G không quyền, lỗi tải | `kiem-kho-mo-so.html` | `../m1/png/kho-kiem-kho-{1512,390}.png` |
| (13) Hàng hoàn đơn cũ | Kho → Hàng hoàn đơn cũ · `/kho/hang-hoan-cu` · Task 273 (M7-28) | MO08 | A nhập một kiện: kho, mã tham chiếu (chuẩn hóa), quét thêm món, số lượng, Bán lại được / Hỏng, ghi chú ≤ 500 (chỉ trong phiếu), kết quả phiếu nhập + phiếu chuyển hàng lỗi · B kiện đã nhập rồi (`legacy_return_exists`, liên kết lần nhập cũ), mã tham chiếu ngắn, số lượng 0, mã lạ, chưa có món, đang gửi · C danh sách theo ngày, khoảng ngày, nhiều ngày, rỗng, không quyền, lỗi tải | `hang-hoan-don-cu.html` | `../m3/png/kho-{1512,390}.png` (trạng thái D) |

## Ca kiểm của spec → khung hình

| Ca | Nội dung | Vẽ ở |
|---|---|---|
| MO02 | Kiểm đếm toàn bộ kho để mở sổ; tồn mở sổ bằng đúng số đếm; không đăng hai lần | (10) A–F |
| MO08 | Hàng hoàn của đơn cũ về kho sau ngày mở: phiếu nhập có mã tham chiếu duy nhất, hàng hỏng vào ngăn hỏng (Q11) | (13) A–C |

## Hành vi khác màn đã có ở M1–M6 (duyệt từng dòng)

| # | Màn | Hành vi đề xuất | Hiện tại | Duyệt |
|---|---|---|---|---|
| HV01 | Kiểm kho | Kiểm kho mở sổ là màn riêng, không có mục menu (một lần mỗi kho): thẻ "Bắt đầu phiên kiểm" của Kiểm kho thêm nút "Mở sổ kho này" khi người dùng có quyền mở sổ kho đó (kho đã có hàng thì bấm sẽ báo `warehouse_not_empty`); kho đã có phiên mở sổ thì báo và dẫn vào phiên đó. | Kiểm kho (M1) chỉ có phiên thường. | OK (Claude, 28/11) |
| HV02 | Menu | Mục mới "Hàng hoàn đơn cũ" ở nhóm Kho, ngay sau "Nhận hàng hoàn"; chỉ người có quyền nhập hàng hoàn cũ thấy. | Không có. | OK (Claude, 28/11) |
| HV03 | Mở sổ · đếm | Mỗi lần quét gửi ngay một lần cộng 1; nhiều người cùng đếm một mã thì cộng dồn, không ghi đè; số to là tổng của mọi người, kèm người đếm lần cuối. | Số của dòng là số đặt, lưu sau khi ngừng quét chưa tới 1 giây; người lưu sau ghi đè. | OK (Claude, 28/11) |
| HV04 | Mở sổ · đếm | Sửa quét thừa bằng nút −1 lớn; không có ô gõ số để đặt lại. Mã không có hàng dùng "Nhập 0" ở danh sách chưa đếm. | Ô số gõ trực tiếp kèm − / +. | OK (Claude, 28/11) |
| HV05 | Mở sổ · đếm | Mất mạng: lần quét xếp hàng trên máy ("Chờ gửi · +1"), có mạng lại tự gửi đúng các lần đó, cùng khóa, không đếm trùng; nhắc không đóng trang. | Dòng hiện "Chưa lưu được, đang thử lại". | OK (Claude, 28/11) |
| HV06 | Mở sổ | Phải có số cho mọi mã đang bán mới đăng được: "405 / 412 mã", tab "Chưa đếm (7)" từ máy chủ; nút Đăng khóa kèm lý do. | Chỉ sửa mã đã quét; mã không quét giữ nguyên tồn. | OK (Claude, 28/11) |
| HV07 | Mở sổ | Không có bước "Xem chênh lệch"; Đăng hỏi xác nhận số mã, tổng cái; đăng xong có "Kiểm lại" (mốc A của đêm mở) và lối sang phiếu điều chỉnh. | Xem chênh lệch → Áp dụng. | OK (Claude, 28/11) |
| HV08 | Mở sổ · quyền | Đăng mở sổ dùng quyền "mở sổ kho" theo kho: Chủ, Quản lý và nhân viên kho được giao kho đăng được (vai trò mẫu Task 260). | Áp dụng kiểm kho thường chỉ Chủ, Quản lý. | OK (Claude, 28/11) |
| HV09 | Mở sổ · Excel | Nhập số đếm từ .xlsx (cột Mã, Số đếm): "Đặt bằng số trong tệp" (mặc định, nhập lại không đổi gì) hoặc "Cộng thêm" (mỗi tệp một lần); đọc ở nền; lỗi từng dòng bằng số dòng và câu lỗi, không chép nội dung ô. | Kiểm kho không nhập tệp. | OK (Claude, 28/11) |
| HV10 | Hàng hoàn cũ | Màn riêng cho đơn của công cụ cũ: mã tham chiếu gõ tay, không gắn đơn hay vận đơn; mỗi kiện một lần (trùng báo và dẫn tới lần nhập cũ); tình trạng chỉ Bán lại được / Hỏng, không có Thiếu, không so số đã gửi; Nhập kho là tồn tăng ngay (phiếu nhập, cộng phiếu chuyển hàng lỗi khi có món hỏng). | Nhận hàng hoàn (M3) theo phiếu hoàn của đơn CRM: Tốt / Hỏng / Thiếu, tổng phải bằng số đã gửi. | OK (Claude, 28/11) |
| HV11 | Hàng hoàn cũ | Mỗi dòng một tình trạng; cùng mã có cả tốt và hỏng thì quét lần nữa để có dòng thứ hai. Danh sách nhóm theo ngày, mặc định 7 ngày gần nhất. | — | OK (Claude, 28/11) |

## Kết luận duyệt (Điểm dừng B, Q-X8)

Điều phối M7 (28/11/2026) giao Claude chốt màn 10 và 13 thay chủ shop, như HV của M4 (chủ shop vắng tới sáng, giao toàn quyền trong phạm vi kế hoạch và spec). Soát theo hệ thiết kế: `soat.mjs` xanh ở 1512, 390, 360; soát bằng mắt mọi ảnh. Task 273 code đúng mockup này.

| Màn | Kết luận | Người chốt | Ngày |
|---|---|---|---|
| (10) Kiểm kho mở sổ | Chốt, HV01, HV03–HV09 OK như mockup | Claude (UX), điều phối M7 giao | 28/11/2026 |
| (13) Hàng hoàn đơn cũ | Chốt, HV02, HV10, HV11 OK như mockup | Claude (UX), điều phối M7 giao | 28/11/2026 |

Chủ shop xem lại khi quay lại; đổi ý dòng nào thì ghi thêm một dòng ở đây (dòng, sửa gì, ngày) và Task 273 làm theo bản sửa. Các trường API mockup cần mà kế hoạch chưa ghi: `docs/tasks/task-247.md`, mục "Việc phát hiện thêm".
