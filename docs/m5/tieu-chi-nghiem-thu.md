# M5 — Tiêu chí nghiệm thu (Task 183, M5-00)

33 ca có mốc M5 trong spec `11-kich-ban-kiem-thu.md` (WB01–WB14, FS01–FS18, HT27), viết lại bằng lời nghiệp vụ. Mỗi ca
một dòng: điều phải đúng, phần **không** thuộc ca này, task kiểm (số nội bộ M5-NN, số toàn cục = 183 + NN; bảng "Ca
kiểm của M5 và task phủ" cuối kế hoạch), loại kiểm theo spec 11: **M** máy (test tự động), **V** nhân viên chạy thử
trên bản thử, **D** diễn tập (thử tải, hỏng giữa chừng, ghi kết quả buổi chạy).

## Hai đợt làm (chủ shop 21/11/2026: "Làm M5")

- **Đợt 1 — Website qua CRM (làm trước):** website đọc sản phẩm, giá, tồn và tạo đơn qua CRM thay Nhanh; mã giảm, quà
  sinh nhật, yêu cầu về đơn, hãng trên web; bộ đệm đọc và giới hạn tần suất. Task: M5-00 → M5-15 trừ M5-04 (183–198 trừ
  187), M5-22 (205, phần năm màn trừ màn flash sale), M5-24 → 26 (207–209), M5-27 → 32 (210–215), M5-35 (218, bỏ ca
  flash), M5-38 (221, phần đợt 1), M5-39 (222, chuẩn bị).
- **Đợt 2 — Flash sale (để sau, chờ chủ shop xác nhận):** M5-04 `AdmissionGate` (187), M5-16 → 21 (199–204), M5-23
  (206), M5-33 (216), M5-34 (217), M5-36 (219), M5-37 (220). Website giữ `FLASH_SALE_ENABLED=false`. Không task đợt 1
  nào được dựa vào mã của đợt 2; ca nào có phần của cả hai đợt được tách thành hai dòng tiêu chí dưới đây.

Mốc đợt 1 xong khi: (1) cả ba bộ e2e của website (chromium, `playwright.promo.config.ts`, `playwright.webkit.config.ts`)
đạt với `COMMERCE_SOURCE=crm` trên CRM cục bộ (WB01–WB14; chín test đỏ sẵn trên `main` so với mốc `main`); (2) tắt hoặc
xóa sạch Valkey giữa lúc website đọc và đặt hàng vẫn đúng (phần đợt 1 của HT27, FS13); (3) mọi ca đợt 1 dưới đây có
test hoặc buổi chạy thử ghi kết quả. Mốc đợt 2 thêm điều kiện (2)–(5) của "Xong M5 khi" trong kế hoạch (FS03, FS16,
FS10/HT27/FS12, FS05).

## Đợt 1 — Website qua CRM

| Ca | Tiêu chí (lời nghiệp vụ) | Ngoài phạm vi ca này | Task kiểm | Loại |
|---|---|---|---|---|
| WB01 | Khách đặt hàng trên website: đơn hiện trong CRM ở trạng thái "Mới", nguồn "Website", đúng món, số lượng, giá của CRM, địa chỉ, phí ship; hàng được giữ ngay lúc tạo đơn; khách cũ (cùng SĐT) gắn đúng hồ sơ, khách mới có hồ sơ mới | Nhân viên xác nhận, gửi hãng (luồng M2, M3 có sẵn); phí ship thật từ hãng khi tích hợp hãng còn tạm dừng (chỉ phí dự phòng, xem QĐ-183-4) | M5-10, 11, 15, 29, 31, 35; V ở M5-38 | MV |
| WB02 | Đổi tên, mô tả, ảnh, giá, tồn, ngừng bán ở CRM thì website hiện theo: giá và thông tin sau một nhịp hỏi thay đổi của website (15 s; bộ đệm của CRM bị xóa theo sự kiện, hạn sống 60 s chỉ là lưới an toàn), còn/hết trong vài giây (hạn sống 5 s); bài viết có ảnh hiện đúng, ảnh mất thì khối ảnh bị bỏ chứ không vỡ trang | Banner, bộ phối, chiến dịch (vẫn ở admin website, P2) | M5-02, 07, 08, 28, 35; V ở M5-38 | MV |
| WB03 | Khách tra đơn của mình thấy đúng trạng thái thật của CRM; khóa dịch vụ chỉ đọc được đơn kênh web theo mã đơn web, không đọc được đơn kênh khác; câu trả lời không có địa chỉ, SĐT, ghi chú nội bộ | Kiểm "đúng người mua" vẫn do website làm như hiện nay (QĐ25) | M5-06, 13, 29, 35 | M |
| WB04 | Khách gửi yêu cầu hủy hoặc đổi từ trang đơn (kèm ảnh nếu có): yêu cầu hiện trên màn "Yêu cầu về đơn hàng" của CRM cho nhân viên trả lời; ảnh chỉ nhân viên xem được bằng link có hạn; tệp giả ảnh bị từ chối | Gửi Zalo/SMS báo khách (P2) | M5-12, 13, 24, 30, 35; V ở M5-38 | MV |
| WB05 | Khách dùng quà sinh nhật: website xin CRM phát mã, khách nhập mã lúc đặt, CRM kiểm điều kiện và ghi lượt dùng trong cùng lần tạo đơn; mã còn hạn trên website được nhập vào CRM trước ngày mở | Chọn khách có sinh nhật và gửi thiệp (website giữ, QĐ17) | M5-14, 26, 30, 35; V ở M5-38 | MV |
| WB06 | Đơn web có giảm theo chiến dịch của website: từng dòng ghi mã chiến dịch, phần trăm, số tiền giảm; CRM tự tính lại theo bảng làm tròn dùng chung, giảm không vượt giá dòng, phần trăm 1–90 | Quản lý chiến dịch trong CRM (P2, KM04) | M5-09, 10, 11, 35 | M |
| WB07 | Bấm "Đặt hàng" hai lần, hay mất mạng sau khi bấm rồi gửi lại: chỉ có một đơn (cùng mã đơn web, cùng khóa chống trùng) | — | M5-11, 29, 35 | M |
| WB08 | Hai khách tranh món cuối: một người đặt được, người kia nhận "hết hàng" kèm số còn; tồn không âm | Hàng flash sale (đợt 2) | M5-10, 11, 35 | M |
| WB09 | Món trong giỏ bị ngừng bán trước khi đặt: CRM từ chối món đó, báo rõ món nào; không tự đổi sang món khác | — | M5-07, 09, 10, 28, 35 | M |
| WB10 | Giá hoặc khuyến mãi đổi khi giỏ để lâu: đặt thẳng thì CRM từ chối và trả báo giá mới để khách xác nhận lại; đơn gửi lại từ hàng đợi cục bộ (khách đã thấy trang thành công) vẫn tạo theo giá CRM, gắn cờ "cần xem lại giá" cho nhân viên gọi khách | — | M5-09, 10, 11, 29, 35 | M |
| WB11 | Mã giảm giá còn một lượt, hai khách dùng cùng lúc: đúng một đơn được giảm, đơn kia bị báo mã đã hết lượt | — | M5-11, 14, 30, 35 | M |
| WB12 | Khách bấm hủy đúng lúc nhân viên gửi hãng: chỉ một bên thành công — hoặc đơn hủy và không có vận đơn, hoặc có vận đơn và khách nhận "yêu cầu hủy đã gửi cho nhân viên" | — | M5-13 | M |
| WB13 | Ai đó sửa giá, tiền giảm, phí ship trong yêu cầu gửi lên: CRM chỉ dùng giá, mã giảm, phí của chính nó; số của website chỉ để so phát hiện chênh | — | M5-09, 10, 11, 29, 35 | M |
| WB14 | CRM ngừng giữa lúc khách mua: website vẫn xem được sản phẩm từ bản sao cục bộ; đơn nằm trong hàng đợi cục bộ và tự gửi lại khi CRM chạy lại; không tạo trùng | Diễn tập CRM ngừng giữa tải flash sale (đợt 2: dòng "WB14 (phần đợt 2)") | M5-28, 29; D là diễn tập `crm-outage.spec.ts` ở M5-35 | MD |
| FS13 (phần đợt 1) | Đổi giá hoặc tồn ở CRM khi website đang đệm: website đổi theo trong vài giây (báo hiệu xóa khóa bộ đệm, hạn sống ngắn làm lưới an toàn); lúc đặt hàng luôn tính theo giá và tồn thật trong PostgreSQL | Trang flash sale và số đo dưới tải (đợt 2, M5-36) | M5-02, 07 | M |
| HT27 (phần đợt 1) | Valkey ngừng hoặc bị xóa sạch giữa giờ bán: website đọc chậm hơn nhưng đúng; đặt hàng không sai số, không trùng đơn; giới hạn tần suất tự chuyển sang bộ đếm trong tiến trình; bộ đệm tự đầy lại | Cờ hết suất, số còn, bộ điều tiết flash sale (đợt 2) | M5-01, 02, 03, 11 | M (phần D của HT27 thuộc đợt 2, M5-37) |

## Đợt 2 — Flash sale (để sau, chờ chủ shop xác nhận)

| Ca | Tiêu chí (lời nghiệp vụ) | Ngoài phạm vi ca này | Task kiểm | Loại |
|---|---|---|---|---|
| FS01 | Quản lý tạo đợt (món, giá flash thấp hơn giá thường, số suất, giới hạn mỗi khách 1–10, hạn giữ 5–30 phút, giờ mở/đóng); khi lên lịch, số suất bị trừ khỏi "có thể bán" bằng bút toán sổ, bán thường và bán trên chat không lấy được phần này | Sửa giá sau khi lên lịch (QĐ9: chỉ thêm suất, đóng sớm) | M5-16, 17, 22, 23; V ở M5-38 | MV |
| FS02 | Tới giờ mở, khách xem trang flash sale tải nhanh từ bộ đệm; bấm mua được đơn nháp giữ hàng có hạn, thấy đồng hồ hạn giữ | Giao diện chi tiết theo mockup chốt ở Điểm dừng C | M5-20, 21, 33, 34, 36; V ở M5-38 | MV |
| FS03 | 20.000 người ảo tranh 500 suất: đúng 500 suất được giữ, không âm, không trùng; người không mua được nhận câu trả lời p95 < 300 ms ở 1.000 lượt/giây | Số đo chính thức trên bản thử (cần OK chủ shop, Điểm dừng D) | M5-18 (nhỏ), 20 (HTTP), 36 (đột biến); D ở M5-38 | MD |
| FS04 | Lượt bấm vượt sức ghi: phần vượt nhận "đang đông, thử lại sau vài giây"; website tự thử lại cùng khóa chống trùng, không tạo trùng đơn | — | M5-04, 20, 34 | M |
| FS05 | Một khách bấm mua 100 lần song song: số đã lấy không vượt giới hạn mỗi khách; lượt thừa bị giới hạn tần suất | Xác minh SĐT bằng OTP (QĐ4) | M5-03, 18, 20, 36 | M |
| FS06 | Khách giữ suất mà không hoàn tất: hết hạn thì đơn nháp hủy, suất mở lại cho người khác, số đã lấy của khách trừ lại; món không báo hết oan | — | M5-19, 21, 36 | M |
| FS07 | Vừa hết suất: người bấm sau nhận "hết suất" ngay; trang đệm đúng lại sau vài giây; suất mở lại khi có người không hoàn tất | — | M5-04, 08, 18, 20, 21 | M |
| FS08 | Đợt kết thúc còn suất chưa ai lấy: suất trả về tồn bán thường bằng bút toán ngược; đối chiếu đêm sổ kho khớp | — | M5-17, 19 | M |
| FS09 | Món đang flash sale cũng bán trên chat và bán thường: hai phần tồn tách nhau, không bên nào bán vào phần bên kia | — | M5-08, 17 | M |
| FS10 | Valkey ngừng giữa đợt: đợt vẫn chạy chậm hơn (bộ điều tiết 50%), đúng số suất | — | M5-03, 04, 20, 37 | MD |
| FS11 | CRM hoặc database ngừng giữa đợt: website hiện "tạm ngưng" cho flash sale, món bán thường vẫn nhận đơn chờ, không nhận đơn chờ cho món flash | — | M5-34, 35, 37 | MD |
| FS12 | Giết tiến trình giữa lúc giữ suất và tạo đơn: hoặc có cả đơn và suất, hoặc không có gì; không suất treo vô chủ | — | M5-18, 37 | M |
| FS13 (phần đợt 2) | Trang flash sale đệm 2 s đổi theo giá, số còn dưới tải | — | M5-36 | M |
| FS14 | Thứ tự được mua khi quá đông: **QĐ5** — chưa làm hàng chờ theo thứ tự đến; bài thử tải ghi phân bố "thứ tự đến so với mua được" để xem lại sau đợt thật đầu tiên | Hàng chờ công bằng tuyệt đối | QĐ5; số đo ở M5-36; V ở M5-38 | V |
| FS15 | Thanh toán trong hạn giữ: **QĐ3** — COD như đơn thường; hạn giữ mặc định 10 phút, chỉnh 5–30 phút theo món | Trả trước, QR (P2) | QĐ3; M5-19, 21, 33, 34; V ở M5-38 | V |
| FS16 | Ghi bền 50 đơn/giây trong 5 phút (15.000 suất): không lỗi, không âm, không trùng; thời gian trả lời trong mục tiêu | Số đo chính thức trên bản thử (Điểm dừng D) | M5-36; D ở M5-38 | MD |
| FS17 | Đóng đợt khi còn người giữ suất: ai hoàn tất trong hạn vẫn mua được; ai hết hạn thì suất về tồn bán thường; không suất nào mở lại trong đợt đã đóng; sổ kho khớp | — | M5-17, 19, 36 | M |
| FS18 | Khách chưa đăng nhập, chưa nhập SĐT bấm mua: **QĐ4** — phải nhập SĐT trước khi mua (không bắt đăng nhập, không OTP); giới hạn tính theo hồ sơ khách của số đó, thêm giới hạn theo IP | OTP (rủi ro biết trước: nhập số người khác) | QĐ4; M5-20, 33, 34; V ở M5-38 | MV |
| WB14 (phần đợt 2) | CRM ngừng giữa lúc tải flash sale: website vẫn xem được, đơn thường nằm chờ và tự gửi lại, không tạo trùng | — | M5-37 bước (e) | D |
| HT27 (phần đợt 2) | Valkey ngừng hoặc bị xóa sạch giữa đợt flash: không bán vượt; cờ hết suất và số còn tự dựng lại từ PostgreSQL | — | M5-04, 20, 21, 37 | MD |

Kiểm đếm: 14 ca WB + 18 ca FS + HT27 = 33 ca; WB14, FS13 và HT27 mỗi ca hai dòng (một dòng mỗi đợt). Ba ca còn mở trong spec
(FS14, FS15, FS18) có quyết định QĐ5, QĐ3, QĐ4 của kế hoạch; chủ shop xác nhận lại khi mở đợt 2.

Ca liên quan nhưng không thuộc M5: KM03 (M2: trả lượt mã khi hủy — WB12, M5-13 dùng lại), KM04 (P2: dời chiến dịch,
banner, bộ phối).
