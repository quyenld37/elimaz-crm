# Mockup M5 đợt 1: các màn chuyển từ admin sang CRM (Task M5-22, số toàn cục 205)

Đề xuất cho các màn CRM của M5 đợt 1 (kế hoạch `docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md`, Task 205 và các dòng "(Task 450)"; menu theo mục "Xếp module trên CRM"). Màn **Đợt flash sale** (`flash-sales.html`) là đợt 2, chưa vẽ. Mở `index.html` để xem ảnh "Hiện tại / Đề xuất" của mọi màn, bảng ca và bảng hành vi. Điểm dừng B: điều phối viên chốt hình theo hệ thiết kế CRM (Q-X8, C3-3); chủ shop OK riêng từng hành vi khác admin.

- Hệ thiết kế: dùng nguyên `../m1/m1.css` (đã duyệt ở M1, console dùng đúng token đó), `../m2/m2.css`, `../m3/m3.css`. `m5.css` chỉ thêm thành phần mới (dòng tin hai phía Khách thấy / Nội bộ, ô khóa hiện một lần, khung xem trước banner, ảnh bộ phối có chấm, dòng đang chọn của danh sách sắp xếp, menu điện thoại mở, ô bật/tắt có chữ) bằng biến có sẵn: không màu, cỡ chữ, bo góc mới.
- Khung: `khung.js` (menu đủ 10 nhóm: Đơn hàng · Vận chuyển · Tiền · Khách hàng · Khuyến mãi · **Website** · Sản phẩm · Kho · Hệ thống · Quản trị; mục M1–M3 trỏ sang mockup của mốc đó).
- Ảnh đề xuất: `shots/<màn>-1512.png` (1512 × 767, laptop của chủ shop), `-390.png` (390 × 844), `-360.png` (360 × 800, mọi màn đều có tên dài). Chụp cả trang.
- Ảnh hiện tại: `shots/hien-tai-*-{1512,390}.png`, chụp admin `apps/admin` origin/main `7199466` build và chạy riêng trên máy (cổng khác 4175/4176) với máy chủ giả trả dữ liệu mẫu; không phải dữ liệu thật của shop, không gọi website hay Nhanh.
- Tự soát và chụp lại: `PW_DIR=<thư mục node_modules/playwright> node docs/mockups/m5/soat.mjs` (Playwright có sẵn trong bộ đệm npx của máy, không thêm vào repo), rồi nén ảnh `python3 docs/mockups/m5/nen-anh.py` (Pillow, bảng 256 màu). Kiểm như M3 (không cuộn ngang, không tràn, không chữ bị cắt thiếu dấu …, mục chạm ≥ 44 px dưới 1000 px và ≥ 32 px từ 1000 px, chỉ token có sẵn, ô chọn đúng, không gì chỉ hiện khi rê chuột, tiền `690.000 ₫` nằm trọn trong ô, đủ khung kế hoạch đòi) và thêm: khóa dịch vụ đầy đủ chỉ nằm trong ô hiện một lần (chỉ đọc, có nút Chép và lời nhắc); nội dung khách gửi có `<script>` hiện nguyên chữ; thứ tự nhóm menu đúng, không còn Tổng quan, Đơn cần kiểm tra, Kết nối Nhanh, Flash sale.
- Dữ liệu là dữ liệu giả. Khung viền đứt "Mockup:" là ghi chú, không phải giao diện. Dữ liệu khó: tên khách 60 ký tự, tên chiến dịch 120, tên khóa 80, tên dịp 28; 0 / 1 / nhiều dòng; đủ trạng thái; lỗi tải, 409, 403, chỉ xem.
- Đường dẫn ghi trong bảng (`/don/yeu-cau`, `/website/…`, `/khuyen-mai/…`, `/quan-tri/nhat-ky`) là đề xuất theo cách đặt tên của console (`/don`, `/khach`, `/ma-giam-gia`); task màn hình chốt đường thật.

## Màn → ca kiểm → trạng thái đã vẽ

| Màn | Đường, task màn hình | Ca | Trạng thái đã vẽ | Tệp | Ảnh admin hiện tại |
|---|---|---|---|---|---|
| (1) Yêu cầu về đơn hàng | Đơn hàng → Yêu cầu về đơn hàng · `/don/yeu-cau`, `/don/yeu-cau/:id` · Task 207 (M5-24) | WB04 | A danh sách lọc 10 nhãn trạng thái có số, lọc loại, tìm không dấu, 11 dòng đủ 8 trạng thái và 4 loại, tên 60 ký tự, 200 dòng theo con trỏ · B chi tiết đổi hàng: đơn, món muốn đổi sang, 3 ảnh (sẵn sàng, đang xử lý, bị từ chối), tin hai phía Khách thấy / Nội bộ, nội dung có <script> hiện nguyên chữ, đổi trạng thái · C Đã xác nhận đổi: địa chỉ gửi trả, mã kiện, hạn (lỗi nhập) · D hộp Cần bổ sung, Từ chối, Hoàn tất · E đã đóng, khách đã hủy, yêu cầu hủy đơn đã gửi hãng · F rỗng, lỗi tải, 409, chỉ xem | `order-requests.html` | `shots/hien-tai-yeu-cau-{1512,390}.png`, `shots/hien-tai-yeu-cau-chi-tiet-{1512,390}.png` |
| (2) Hãng trên website | Website → Hãng trên web = phần cuối màn Kết nối vận chuyển (M3) · Task 208 (M5-25) | — | A bật hiện, thứ tự, số vận đơn 30 ngày, chọn sẵn hoặc rẻ nhất, phí dự phòng (mặc định tắt), tài khoản không hoạt động không bật được · B ẩn hãng chọn sẵn thì tự bỏ chọn sẵn, ẩn hãng cuối cùng bị chặn, đang lưu · C 409, chỉ xem, 0 tài khoản | `web-carriers.html` | `shots/hien-tai-van-hanh-{1512,390}.png` |
| (3) Khóa dịch vụ | Website → Khóa dịch vụ · `/website/khoa-dich-vu` · Task 208 (M5-25) | — | A 4 khóa (đang dùng, giới hạn IP, tên 80 ký tự chưa dùng, đã thu hồi), chỉ hiện tiền tố · B tạo khóa: tên, quyền web, IP · C khóa hiện một lần, nút Chép, lời nhắc · D gửi lại 409 đã xử lý, thu hồi có xác nhận, 409, 0 khóa | `service-keys.html` | — (màn mới) |
| (4) Quà sinh nhật | Khuyến mãi → Quà sinh nhật · `/khuyen-mai/qua-sinh-nhat` · Task 209 (M5-26, bốn tab theo Task 468) | WB05 | Đầu trang: trạng thái quà, lần chạy gần nhất, 4 số, Tạo mã ngay · A Cần nhắn: chọn nhiều, Chép tin, Mở Zalo, Đã nhắn, rỗng · B Tất cả khách: 8 trạng thái, tháng sinh, tìm, Xuất CSV, xem thêm · C Cài đặt quà: 5 ô số (lỗi bội số 1.000), xem trước câu khách thấy, lượt khác đang chạy (409) · D Đợt mã: đợt theo ngày, đợt rỗng, mã chưa tặng / đã tặng / đã dùng (đơn) / hết hạn, không quyền | `birthday-gifts.html` | `shots/hien-tai-sinh-nhat-can-nhan-{1512,390}.png`, `shots/hien-tai-sinh-nhat-tat-ca-{1512,390}.png`, `shots/hien-tai-sinh-nhat-cai-dat-{1512,390}.png` |
| (5) Chiến dịch giảm giá | Khuyến mãi → Chiến dịch giảm giá · `/khuyen-mai/chien-dich` · Task 458 (M5-46) | — | A 5 chiến dịch đủ 5 trạng thái, tên 120 ký tự, lọc, tìm, cỡ trang, chép liên kết · B form: phạm vi, chọn đích (2/30, đủ 30/30), giờ VN, dùng kèm mã, hàng cắt mã, xem trước giá · C kích hoạt có lý do, phải xem trước lại, vượt trần, tạm dừng, sửa khi đang chạy · D xóa (banner tự ẩn), 409 giữ form, chỉ xem, 0 chiến dịch | `web-campaigns.html` | `shots/hien-tai-chien-dich-{1512,390}.png`, `shots/hien-tai-chien-dich-sua-{1512,390}.png` |
| (6) Banner trang chủ | Website → Banner trang chủ · `/website/banner` · Task 459 (M5-47) | — | A 7 banner (lý do ẩn), sửa banner ảnh: ảnh máy tính sẵn sàng, ảnh điện thoại đang xử lý → Lưu khóa · B banner giảm giá: chiến dịch, màu nền, chữ tự động, ghim 3, theo giờ chiến dịch · C tải ảnh: đang tải, lệch tỉ lệ, bị từ chối · D xem trước máy tính / điện thoại, banner ẩn, 9 lý do · E đủ 8, đang lưu khóa hết, 409 giữ bản sửa, chỉ xem, 0 banner | `web-banners.html` | `shots/hien-tai-banner-{1512,390}.png` |
| (7) Bộ phối | Website → Bộ phối · `/website/bo-phoi` · Task 460 (M5-48) | — | A 124 bộ, 6/6 trang chủ (1 ẩn vì hết hàng), bộ thứ 7 khóa ô Trang chủ, chép liên kết · B sửa: tên dịp 28 ký tự + 5 tên gợi ý, 3 sản phẩm và màu, chọn ảnh, chấm kẹp ở 97%, tiêu đề tự động, câu mở đầu, câu chuyện, danh mục thêm, chiều cao/size, giờ hiện · C xem trước, lý do ẩn, 6 lý do · D xóa (liên kết chết), 409, 0 bộ | `web-looks.html` | `shots/hien-tai-bo-phoi-{1512,390}.png` |
| (8) Góp ý | Khách hàng → Góp ý · `/khach/gop-y` + khối Góp ý trong hồ sơ khách · Task 461 (M5-49) | — | A lọc trạng thái có số, chủ đề, 5 dòng đủ 5 chủ đề, gắn / không gắn hồ sơ, <script> hiện nguyên chữ, dòng mở: đơn, trang, thiết bị, ghi chú nội bộ, đổi trạng thái, còn trang sau · B khối Góp ý trong hồ sơ khách · C rỗng, 409, 403 | `feedback.html` | `shots/hien-tai-gop-y-{1512,390}.png` |
| (9) Nhận đơn web | Website → Nhận đơn web · `/website/nhan-don` · Task 462 (M5-50) | — | A đang nhận · B hộp tạm dừng (nút khóa khi chưa có lý do; đơn đã gửi vẫn vào) · C đang tạm dừng: lý do, ai, lúc nào; mở lại có xác nhận · D đang lưu, 409, chỉ xem | `web-intake.html` | `shots/hien-tai-van-hanh-{1512,390}.png`, `shots/hien-tai-van-hanh-dang-mo-{1512,390}.png` |
| (10) Nhật ký thao tác | Quản trị → Nhật ký thao tác · `/quan-tri/nhat-ky` · Task 462 (M5-50) | — | A mới nhất trước; người làm là nhân viên, khóa dịch vụ, hệ thống; lọc hành động, người, khoảng ngày; chỉ tên trường đổi; xem thêm theo con trỏ · B rỗng, 403 | `audit.html` | `shots/hien-tai-nhat-ky-{1512,390}.png` |
| (11) Menu | Khung · `console/src/app/nav.ts` · Task 462 (M5-50) | — | Menu đủ 10 nhóm theo mục "Xếp module": nhóm Website mới, mục mới ở Đơn hàng, Khách hàng, Khuyến mãi, Quản trị, số đếm yêu cầu đang mở; không Tổng quan, Đơn cần kiểm tra, Kết nối Nhanh, Flash sale; menu điện thoại mở | `menu.html` | `shots/hien-tai-tong-quan-{1512,390}.png` |

## Ca kiểm của spec → khung hình

| Ca | Nội dung (spec 11) | Vẽ ở |
|---|---|---|
| WB04 | Khách gửi yêu cầu hủy hoặc đổi từ trang đơn hàng → yêu cầu vào CRM cho nhân viên xử lý | Yêu cầu về đơn hàng A–F |
| WB05 | Khách dùng quà sinh nhật → mã được kiểm và ghi lượt dùng ở CRM | Quà sinh nhật B, D (mã "Đã dùng · DH0001288") |
| FS01 | Flash sale | Đợt 2, chưa vẽ |

## Hành vi khác admin hiện tại (chủ shop OK từng dòng)

Theo luật "Behaviour changes need own OK" và bảng "Việc cần chủ shop trong M5": mỗi dòng cần chủ shop OK riêng; hình do điều phối viên chốt, việc OK hành vi tách riêng. Dòng chưa OK chỉ chặn đúng màn có hành vi đó (Task 207; phần Hãng trên web của 208; 209; 458–462). Khóa dịch vụ (phần còn lại của Task 208) bắt đầu ngay khi điều phối viên chốt hình.

| # | Màn | Hành vi đề xuất | Admin hiện tại | Chủ shop OK |
|---|---|---|---|---|
| HV01 | Menu | Mọi mục của admin vào menu CRM: nhóm mới "Website" (Banner trang chủ, Bộ phối, Hãng trên web, Nhận đơn web, Khóa dịch vụ) giữa Khuyến mãi và Sản phẩm; "Yêu cầu về đơn hàng" ở Đơn hàng; "Góp ý" ở Khách hàng; "Chiến dịch giảm giá", "Quà sinh nhật" ở Khuyến mãi; "Nhật ký thao tác" ở Quản trị. | Admin riêng, menu: Tổng quan · Cửa hàng · Cần xử lý · Hệ thống. | OK chủ shop 21/11 (chat) |
| HV02 | Menu | Không có màn Tổng quan (QĐ37): số yêu cầu đang mở là số đếm cạnh mục "Yêu cầu về đơn hàng"; trạng thái nhận đơn ở màn Nhận đơn web; hoạt động gần đây ở Nhật ký thao tác. Không có "Đơn cần kiểm tra", "Kết nối Nhanh". | Tổng quan có 2 ô số, trạng thái website, 5 dòng hoạt động. | OK chủ shop 21/11 (chat) |
| HV03 | Yêu cầu | Danh sách là bảng có thanh lọc trạng thái kèm số đếm và lọc loại; mở một yêu cầu là trang chi tiết riêng. | Danh sách thẻ gập/mở, lọc "Đang mở" bằng ô chọn. | OK chủ shop 21/11 (chat) |
| HV04 | Yêu cầu | Tin nhân viên chọn "Khách thấy" hoặc "Ghi chú nội bộ" (khách không thấy). | Mọi tin nhân viên khách đều thấy. | OK chủ shop 21/11 (chat) |
| HV05 | Yêu cầu | Chi tiết hiện món khách muốn đổi, biến thể muốn đổi sang kèm số còn, và ảnh khách gửi (mở bằng link có hạn 10 phút; ảnh đang xử lý, bị từ chối hiện riêng); lý do hiện bằng chữ. | Không hiện ảnh; lý do hiện mã thô ("SIZE"). | OK chủ shop 21/11 (chat) |
| HV06 | Yêu cầu | Thêm loại "Trả hàng" (M5-12) bên cạnh Đổi hàng, Hủy đơn, Hỏi về đơn. | Ba loại: đổi, hủy, hỏi. | OK chủ shop 21/11 (chat) |
| HV07 | Yêu cầu | Yêu cầu hủy đơn đã gửi hãng có nút "Mở đơn để hủy vận đơn": hủy, đổi hãng, tạo đơn đổi làm ngay trong CRM, rồi quay lại Hoàn tất. | "Hủy đơn, tạo đơn đổi và hoàn tiền vẫn thực hiện trên Nhanh." | OK chủ shop 21/11 (chat) |
| HV08 | Yêu cầu | Cần bổ sung, Từ chối, Hoàn tất mở hộp thoại riêng (Cần bổ sung bắt buộc lời nhắn; Từ chối, Hoàn tất hỏi xác nhận vì không mở lại được). | Một form; chỉ Hoàn tất, Từ chối hỏi xác nhận. | OK chủ shop 21/11 (chat) |
| HV09 | Hãng trên web | Chọn hãng hiện trên website ở CRM, trên tài khoản hãng của màn Kết nối vận chuyển (một nơi lưu); bật/tắt và thứ tự lưu ngay từng tài khoản; chọn sẵn và phí dự phòng có nút Lưu riêng. | Một danh sách 3 hãng Nhanh, một nút Lưu, không đổi thứ tự. | OK chủ shop 21/11 (chat) |
| HV10 | Hãng trên web | Có thứ tự hãng khách thấy (↑ ↓); tài khoản tạm ngừng hoặc cần cấp quyền lại không bật được, có câu lý do. | Không có thứ tự; danh sách lấy từ đơn Nhanh 30 ngày. | OK chủ shop 21/11 (chat) |
| HV11 | Hãng trên web | Thêm "Phí dự phòng" (mặc định tắt): khi mọi hãng không báo được phí, khách thấy phí này ghi "ước tính". | Không có. | OK chủ shop 21/11 (chat) |
| HV12 | Hãng trên web | Số "vận đơn trong 30 ngày" mỗi tài khoản đếm vận đơn trong CRM (không còn đếm đơn Nhanh). | "n đơn trong Nhanh 30 ngày qua" mỗi hãng. | OK chủ shop 21/11 (chat) |
| HV13 | Khóa dịch vụ | Màn mới: Chủ tạo khóa cho website (chọn quyền, IP); khóa hiện một lần, chép ngay; thu hồi có xác nhận, có hiệu lực trong vài giây. | Không có (admin dùng tài khoản đăng nhập). | OK chủ shop 21/11 (chat) |
| HV14 | Quà sinh nhật | Quà sinh nhật ở CRM, bốn tab: Cần nhắn, Tất cả khách, Cài đặt quà, thêm tab "Đợt mã" (mã theo ngày, đã tặng, đã dùng kèm đơn, hết hạn); mã tạo trong CRM. | Ba tab; mã tạo trong Nhanh. | OK chủ shop 21/11 (chat) |
| HV15 | Quà sinh nhật | Cài đặt có ô "Lưu ngày sinh trước ít nhất (0–120 ngày)"; không hiện trạng thái kết nối Zalo OA, email (website giữ kết nối, tự gửi). | Có khối "Kênh gửi tự động" Zalo OA / Email. | OK chủ shop 21/11 (chat) |
| HV16 | Chiến dịch | Kích hoạt, tạm dừng, sửa khi đang chạy, xóa là hộp thoại trên danh sách; danh sách tải theo "Xem thêm" (giữ chọn 10/20/50 dòng). | Màn đổi sang khung kích hoạt riêng; phân trang số. | OK chủ shop 21/11 (chat) |
| HV17 | Chiến dịch | Vượt trần phần trăm báo "vượt trần của shop (50%), nhờ Chủ nâng trần". | Câu lỗi nói biến PROMO_CAMPAIGN_MAX_PERCENT trên máy chủ. | OK chủ shop 21/11 (chat) |
| HV18 | Banner | Ảnh banner tải lên kho tệp của CRM: có trạng thái "Đang xử lý ảnh", ảnh có thể bị từ chối sau xử lý (rộng dưới 600 px); đang xử lý thì nút Lưu khóa kèm lý do. | Website xử lý ảnh ngay khi tải, lỗi báo ngay. | OK chủ shop 21/11 (chat) |
| HV19 | Banner, Bộ phối | Người khác vừa lưu: hộp "Giữ bản đang sửa / Tải bản mới", không mất bản đang sửa. | Báo "Chọn Bỏ thay đổi để tải bản mới rồi sửa lại". | OK chủ shop 21/11 (chat) |
| HV20 | Bộ phối | Thêm ô "Câu mở đầu" (≤ 90) và "Câu chuyện" (≤ 220) mà website đã hỗ trợ. | Admin chưa có hai ô này. | OK chủ shop 21/11 (chat) |
| HV21 | Bộ phối | Ảnh người mẫu chọn từ ảnh sản phẩm trong CRM. | Ảnh lấy từ URL ảnh Nhanh. | OK chủ shop 21/11 (chat) |
| HV22 | Góp ý | Mức hài lòng hiện bằng chữ ("2/5 · Không hài lòng"); góp ý có SĐT trùng khách thì gắn hồ sơ, có liên kết sang hồ sơ và khối "Góp ý" trong hồ sơ khách; lọc trạng thái bằng thanh nhãn có số. | Mặt cười; không gắn khách. | OK chủ shop 21/11 (chat) |
| HV23 | Nhận đơn web | Tạm dừng/mở lại nhận đơn là màn riêng "Nhận đơn web" (nhóm Website), tách khỏi hãng giao hàng. | Chung trang "Vận hành website" với hãng giao hàng. | OK chủ shop 21/11 (chat) |
| HV24 | Nhật ký | Chỉ hiện tên trường đổi, không hiện nội dung (lý do, tên, mức giảm); người làm có thể là nhân viên, khóa dịch vụ của website, hệ thống; lọc người và khoảng ngày, không có ô tìm chữ; xem thêm theo con trỏ. | Hiện "Lý do: …", tên chiến dịch, mức giảm; ô tìm; phân trang số. | OK chủ shop 21/11 (chat) |
| HV25 | Nhật ký | Nhật ký cũ của admin không chuyển vào CRM (QĐ38), chỉ giữ bản xuất CSV. | — | OK chủ shop 21/11 (chat) |

## Kết luận duyệt (Điểm dừng B, Q-X8)

| Màn | Kết luận | Người chốt | Ngày |
|---|---|---|---|
| (1)–(11) | Chờ điều phối viên duyệt | — | — |

Điều phối viên ghi kết luận từng màn ở bảng trên (chốt / sửa gì); chủ shop ghi OK từng dòng HV. Không bắt đầu Task 207–209, 458–462 cho màn chưa chốt.
