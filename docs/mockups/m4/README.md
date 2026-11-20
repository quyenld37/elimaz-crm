# Mockup M4 phần hộp chat (Task M4-01, số toàn cục 141)

Đề xuất cho ba phần của Hộp chat ở mốc M4: danh sách hội thoại (Inbox), hội thoại, khung bên phải. Mở `index.html` để xem ảnh ba cỡ của mọi màn, bảng ca kiểm và bảng hành vi. Màn Live, Đã chốt thiếu địa chỉ, Kết nối Page, Cài đặt hộp chat là của Task M4-02 (sẽ thêm vào thư mục này).

- Hệ thiết kế: dùng nguyên `../m1/m1.css` (đã duyệt ở M1, console dùng đúng token đó; gốc `apps/storefront-giftstatus/DESIGN.md`) và `../m2/m2.css` (thành phần M2). `m4.css` chỉ thêm thành phần mới (ba cột hộp chat, dòng hội thoại, bong bóng tin, ô gõ, băng báo, thẻ bài viết) bằng biến có sẵn: không màu, cỡ chữ, bo góc mới.
- Khung: `khung.js` (nhóm "Hộp chat" đứng đầu menu, mục M2, M1 trỏ sang `../m2/`, `../m1/`).
- Ảnh: `png/<màn>-1512.png` (1512 × 767, laptop của chủ shop), `-390.png` (390 × 844), `-360.png` (360 × 800, dòng hội thoại và bong bóng dễ tràn). Chụp cả trang.
- Tự soát và chụp lại: `PW_DIR=<thư mục node_modules/playwright> node docs/mockups/m4/soat.mjs` (Playwright có sẵn trong bộ đệm npx của máy, không thêm vào repo). Kiểm như M2 (không cuộn ngang, không tràn, không chữ bị cắt thiếu dấu …, mục chạm ≥ 44 px dưới 1000 px và ≥ 32 px từ 1000 px, chỉ token có sẵn, ô chọn đúng, không gì chỉ hiện khi rê chuột, tiền `690.000 ₫`, đủ khung kế hoạch đòi) và thêm: bong bóng, dòng hội thoại không tràn khung; ảnh không cao quá 360 px.
- Dữ liệu là dữ liệu giả (tên, SĐT, địa chỉ). Khung viền đứt "Mockup:" là ghi chú, không phải giao diện. Dữ liệu cứng theo kế hoạch: tên khách 60 ký tự không dấu cách, comment 2.000 ký tự, 0 / 1 / 200 hội thoại, 0 / 9 / 120 chưa đọc, 0 / 14 nhãn, ảnh dọc 1080 × 4320, tên tệp dài.

## Màn → ca kiểm → trạng thái đã vẽ

| Màn | Đường (kế hoạch Task M4-34) | Ca kiểm | Trạng thái đã vẽ | Tệp |
|---|---|---|---|---|
| (1) Hộp chat (Inbox) | `/inbox` | CH01, CH02, CH06, CH07, CH08, CH16 | A danh sách đủ loại (tin mới nhảy lên đầu, comment kèm tên bài, tên 60 ký tự, 120 và 9 chưa đọc, 14 nhãn, người lạ lần đầu, 200 hội thoại, cột Page, bộ lọc) · B mất kết nối đang nối lại, lọc "Của tôi" + chưa đọc còn 1 hội thoại không nhãn, tìm "5678" · C 0 hội thoại, 0 chưa đọc, Page chỉ xem, Page mất kết nối · D khách chưa trả lời, gửi mẫu một chạm | `inbox.html` |
| (2) Hội thoại | `/inbox/:conversationId` | CH01–CH14, CH18, CH19 | A tin nhắn đủ loại (lịch sử trước CRM, hệ thống, tin chào tự động, nhân viên, ngoài CRM, ghi chú nội bộ, ảnh dọc cao, tệp tên dài, đang gửi ảnh), "An đang xem", ô gõ còn giờ · B sáu trạng thái gửi, "An đang trả lời" · C comment (nút "Nhận" khi chưa ai nhận, thẻ bài, trả lời công khai, nhắn riêng khóa sau một tin và khi quá 7 ngày, tự trả lời một lần, bị ẩn, khách sửa, khách xóa, 2.000 ký tự) · D ô gõ hết 24 giờ, Human Agent, Page chỉ xem, menu "/" | `hoi-thoai.html` |
| (3) Khung bên phải | cột phải của `/inbox/:conversationId` | DH03, LV04, CH06, CH07, CH08, CH15, CH17 | A khách cũ (số đơn, tỷ lệ nhận/hoàn, tổng chi, 5 đơn gần đây, có thể cùng một khách, một hồ sơ cho tin và comment), nhãn, người phụ trách, nhắc việc, nút gọi · B khách mới chưa có đơn (tên 60 ký tự), đơn nháp từ Live, hoàn tất đơn cạnh chat, gợi ý gắn nhãn "Đã lên đơn" · C chỉ có quyền lên đơn theo Page (Task 411), khách không liên hệ, kết quả cuộc gọi, giao cho | `khung-ben-phai.html` |

## Ca kiểm của spec → khung hình

| Ca | Nội dung (spec 11) | Vẽ ở |
|---|---|---|
| CH01 | Khách nhắn: tin hiện ngay, hội thoại lên đầu, tăng chưa đọc | Inbox A (dòng nền hồng); Hội thoại A (tin vào) |
| CH02 | Comment vào cùng hộp chat, ghi rõ thuộc bài nào | Inbox A (chữ "Comment" + tên bài); Hội thoại C (thẻ bài viết) |
| CH03 | Trả lời: "đang gửi" rồi "đã gửi" | Hội thoại A, B |
| CH04 | Gửi ảnh cho khách | Hội thoại A (ảnh đang tải lên, đang gửi) |
| CH05 | Trả lời comment và nhắn riêng; nút nhắn riêng khóa sau một tin | Hội thoại C |
| CH06 | Tự nhận hoặc được giao; thấy ở "Của tôi" | Inbox A, B (lọc "Của tôi"); Hội thoại C (nút "Nhận" ở hội thoại chưa ai nhận); Khung bên phải A (Giao cho…, Trả lại), C (hộp Giao cho) |
| CH07 | Nhãn, mẫu trả lời, ghi chú nội bộ (không gửi ra ngoài) | Inbox A (lọc nhãn); Hội thoại A (ghi chú), D (menu "/"); Khung bên phải A (nhãn) |
| CH08 | Người lạ lần đầu: tự tạo hồ sơ khách tiềm năng | Inbox A (chip "Khách tiềm năng mới"); Hội thoại A (tin hệ thống); Khung bên phải B |
| CH09 | Quá 24 giờ: ô gõ khóa, ghi lý do và hạn | Hội thoại D (hết 24 giờ) |
| CH10 | Hai người cùng mở: "An đang xem"/"An đang trả lời" | Hội thoại A, B |
| CH11 | Tự ẩn comment có SĐT: ẩn trên Page, vẫn hiện trong CRM | Hội thoại C |
| CH12 | Tự trả lời comment, tin chào: mỗi comment một tin tự động | Hội thoại A (tin chào), C (tự trả lời + tự nhắn riêng) |
| CH13 | Khách sửa/xóa comment: CRM cập nhật, giữ lịch sử | Hội thoại C |
| CH14 | Nhân viên trả lời bằng ứng dụng Meta: tin vẫn hiện | Hội thoại A ("Gửi từ ứng dụng Meta (ngoài CRM)") |
| CH15 | Vừa nhắn vừa comment: một hồ sơ khách | Khung bên phải A (dòng "Một hồ sơ gắn với tài khoản Facebook này qua cả tin nhắn và comment") |
| CH16 | Khách chưa trả lời: gửi mẫu một chạm; tự gửi một bước nếu bật | Inbox D (cài đặt tự gửi ở Task M4-02) |
| CH17 | Ghi kết quả cuộc gọi vào hồ sơ và hội thoại | Khung bên phải A (nút), C (hộp kết quả) |
| CH18 | Mạng rớt khi gửi: "không rõ đã gửi chưa", không tự gửi lại | Hội thoại B |
| CH19 | Quá 3 phút chưa đi: "chưa gửi được", gửi lại hoặc bỏ | Hội thoại B |
| LV04 | Khách gửi địa chỉ: hoàn tất đơn cạnh chat, đơn "Đã xác nhận" | Khung bên phải B |
| DH03 | Khung khách cạnh chat: lịch sử mua, tỷ lệ nhận và hoàn | Khung bên phải A |

## Thay đổi hành vi so với công cụ cũ (cần chủ shop OK từng dòng)

Theo luật "Behaviour changes need own OK": mỗi dòng cần chủ shop OK riêng; điều phối viên chốt hình ở Điểm dừng B (Q-X8) và không chặn Task M4-34–36 (làm theo mockup, chủ shop đổi ý thì sửa ở vòng sau). Cột "Hiện tại" ghi theo spec 01, 02 (shop đang dùng Vpage cho hai Page); chưa khảo sát chi tiết thì ghi "chưa khảo sát".

| # | Màn | Hành vi đề xuất | Hiện tại | Chủ shop OK |
|---|---|---|---|---|
| HV01 | Khung | Menu thêm nhóm "Hộp chat" đứng đầu, trước Đơn hàng. | Vpage là công cụ riêng, ngoài CRM. | chưa |
| HV02 | Khung | Máy tính: ba cột (danh sách 340 px, hội thoại, khung khách 360 px). Điện thoại: mỗi lúc một màn (danh sách → hội thoại → "Khách"), nút "‹" quay lại. | Vpage: chưa khảo sát bố cục. | chưa |
| HV03 | Inbox | Chọn Page là danh sách trên đầu cột hội thoại (không phải cột riêng), mỗi Page có số chưa đọc; trên 99 hiện "99+". | Vpage có chọn Page; chưa khảo sát. | chưa |
| HV04 | Inbox | Tin mới: hội thoại nhảy lên đầu, nền hồng 5 giây, tăng số chưa đọc (như đơn vừa đổi ở M2). Không có âm báo. | Chưa khảo sát. | chưa |
| HV05 | Inbox | Dòng hội thoại: nhãn tối đa 3 rồi "+n"; comment ghi "Comment" và tên bài; người phụ trách ở cuối dòng ("Của tôi", "Chưa ai nhận", tên). | Vpage có 14 nhãn chung. | chưa |
| HV06 | Inbox | Bộ lọc mặc định: Đang mở, Mọi người, Mọi nhãn, Tin và comment; hai nút bật/tắt "Chỉ chưa đọc", "Khách chưa trả lời"; "Tải thêm 50" thay số trang. | Chưa khảo sát. | chưa |
| HV07 | Inbox | Mất kết nối cập nhật tự động: băng "Mất kết nối, đang nối lại", danh sách tự tải lại mỗi 20 giây, nút "Tải lại ngay". | — | chưa |
| HV08 | Inbox | Page chỉ xem và Page mất kết nối báo bằng băng trên đầu danh sách và chip cạnh tên Page. | — | chưa |
| HV09 | Inbox | "Khách chưa trả lời": chọn số giờ im lặng (mặc định 3) và mẫu; mỗi dòng một nút "Gửi mẫu" gửi ngay, dòng chuyển "Đã gửi". Không hiện khách quá 24 giờ, khách không liên hệ, khách đã chặn Page. | Nhắn lại tay trên Vpage; chưa khảo sát. | chưa |
| HV10 | Hội thoại | Tin lịch sử lấy từ Facebook (trước khi kết nối CRM) hiện nhạt, viền đứt, có chữ "trước khi kết nối CRM"; không tính chưa đọc. | — | chưa |
| HV11 | Hội thoại | Tin tự động: bong bóng viền, chữ "Tin tự động" và tên quy tắc. Tin gửi từ ứng dụng Meta: bong bóng xám đậm, "Gửi từ ứng dụng Meta (ngoài CRM)", không biết người gửi. Tin nhân viên ghi tên người gửi. | — | chưa |
| HV12 | Hội thoại | Ghi chú nội bộ là tab "Ghi chú nội bộ" của ô gõ; bong bóng nền hồng viền đứt, chữ "Chỉ nhân viên thấy", không bao giờ gửi cho khách. | Chưa khảo sát Vpage có ghi chú không. | chưa |
| HV13 | Hội thoại | Ảnh cao tối đa 360 px, bấm xem cỡ thật; tệp không phải ảnh chỉ có nút "Tải về", không xem trước. | — | chưa |
| HV14 | Hội thoại | Trạng thái gửi hiện bằng chữ dưới tin. "Không rõ đã gửi chưa" và "Chưa gửi được" có hai nút Bỏ / Gửi lại cùng cỡ (Gửi lại không nổi bật vì khách có thể nhận hai lần); "Lỗi" kèm lý do tiếng Việt, không có nút gửi lại. | — | chưa |
| HV15 | Hội thoại | Ô gõ khóa (không ẩn) khi hết 24 giờ hoặc Page chỉ xem, kèm lý do và hạn; vẫn viết được ghi chú nội bộ; có nút "Gọi khách", "Nhắc tôi lúc…". | — | chưa |
| HV16 | Hội thoại | Page có thẻ Human Agent: sau 24 giờ nút gửi đổi chữ thành "Trả lời (nhân viên)", hiện hạn 7 ngày; đồng hồ còn lại luôn hiện, chữ đỏ ở chế độ này. | — | chưa |
| HV17 | Hội thoại | Enter xuống dòng; Ctrl/⌘ + Enter gửi. | Chưa khảo sát (Vpage có thể Enter gửi). | chưa |
| HV18 | Hội thoại | Gõ "/" mở danh sách mẫu ngay trên ô gõ; Enter chèn mẫu vào ô (điền sẵn biến) để sửa, không gửi luôn. | Vpage có mẫu trả lời; chưa khảo sát cách chèn. | chưa |
| HV19 | Hội thoại | Mỗi comment luôn có ba nút "Trả lời công khai", "Nhắn riêng", "Ẩn trên Page" (không chỉ khi rê chuột). Nhắn riêng mờ kèm lý do sau một tin hoặc khi comment quá 7 ngày. | — | chưa |
| HV20 | Hội thoại | Comment bị ẩn vẫn hiện đủ trong CRM, chip "Đã ẩn trên Page" kèm lý do (tự ẩn vì có SĐT) và nút "Hiện lại trên Page". | — | chưa |
| HV21 | Hội thoại | Comment khách sửa: chip "Khách đã sửa" và lịch sử sửa mở sẵn dưới comment. Khách xóa: bong bóng viền đứt "Khách đã xóa…" và giữ nội dung trước khi xóa. | — | chưa |
| HV22 | Hội thoại | Tin, comment dài hiện 8 dòng rồi nút "Xem hết (n ký tự)". | — | chưa |
| HV23 | Hội thoại | "An đang xem", "An đang trả lời…" là dải ngay dưới đầu hội thoại. | — | chưa |
| HV24 | Khung bên phải | Thẻ khách: 4 ô số (số đơn, tổng chi, nhận hàng, hoàn); ô hoàn tô đỏ khi khách có nhãn "Hay hoàn" của M2; 5 đơn gần đây có liên kết, "Xem cả n đơn". | Vpage không có lịch sử mua (Nhanh riêng). | chưa |
| HV25 | Khung bên phải | Khách có đơn nháp từ Live: khung lên đơn mở sẵn đơn nháp đó để hoàn tất; Page, hội thoại, nguồn đơn điền sẵn, không sửa ở đây. | Chốt Live trên công cụ cũ: chưa khảo sát. | chưa |
| HV26 | Khung bên phải | Nút "Lấy địa chỉ từ tin 09:14 của khách" chép nguyên văn tin khách vào ô địa chỉ để nhân viên sửa (không tự tách tỉnh, xã). | Nhân viên chép tay. | chưa |
| HV27 | Khung bên phải | Lưu đơn xong: gợi ý một chạm "Gắn nhãn Đã lên đơn", không tự gắn. | — | chưa |
| HV28 | Khung bên phải | Nhân viên chỉ có quyền "Tạo đơn" theo Page (không theo kho, Task 411): khung lên đơn hiện khóa sẵn kèm lý do và cách xin quyền, thay vì để bấm rồi báo lỗi 403. Vẫn trả lời, gắn nhãn, ghi chú được. Cùng việc "khóa nút trước" mà Task 411 giao cho Task 412 (màn Lên đơn): hai màn dùng chung một cách. | Task 411: máy chủ trả 403, màn Lên đơn chỉ báo lỗi sau khi bấm. | chưa |
| HV29 | Khung bên phải | Khách "không liên hệ": hộp đỏ ở thẻ khách; CRM không gửi tin tự động; nhân viên vẫn nhắn, gọi được; có nút "Bỏ cờ không liên hệ" (cần quyền sửa khách). | — | chưa |
| HV30 | Khung bên phải | Ghi kết quả cuộc gọi là hộp thoại 6 lựa chọn; "Hẹn gọi lại" bắt nhập giờ và tự tạo nhắc việc cho người gọi. | Chưa khảo sát. | chưa |
| HV31 | Khung bên phải | "Giao cho…" là hộp chọn nhân viên đang làm việc có quyền xem hội thoại ở Page đó, kèm số hội thoại đang phụ trách; người không có quyền giao chỉ thấy "Nhận". | Chưa khảo sát. | chưa |

## Kết luận duyệt (Điểm dừng B, Q-X8)

| Màn | Kết luận | Người chốt | Ngày |
|---|---|---|---|
| (1)–(3) | Chờ điều phối viên duyệt | — | — |

Điều phối viên ghi kết luận từng màn và từng dòng hành vi ở bảng trên (chốt / sửa gì). Không bắt đầu Task M4-34–36 cho màn chưa chốt.
