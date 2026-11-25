# Task 142 — M4-02: Mockup Live và cài đặt

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 142 = M4-02, trước Điểm dừng B). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu (không có tệp nào trong `CODE_PATHS`).

Căn cứ: kế hoạch M4 mục "Task 142 (M4-02)", "Điểm dừng B"; Interfaces của Task M4-03 (kiểu `channel.Features`, `ChannelAccount.SendMode`, `HumanAgentEnabled`, quyền `channel.manage`, `conversation.settings`, `live.operate`, mã lỗi `comment_claimed`, `open_draft_exists`, `out_of_stock`, `seller_mismatch`, `page_not_accessible`, `page_connected_elsewhere`, `version_conflict`), M4-09 (kết nối Page: OAuth cùng tab, mã system user, Page mới `read_only`, ngắt kết nối), M4-10 (sức khỏe kênh, `degraded`, đọc comment định kỳ `auto|on|off`), M4-20 (cài đặt Page: `live_slots`, xoay vòng, nhãn), M4-21 (mẫu trả lời, biến), M4-25–28 (bốn quy tắc tự động và cấu hình của chúng), M4-30–33 (phiên live, nguồn comment SSE/hỏi định kỳ, chốt, danh sách thiếu địa chỉ, nhắc), M4-37, M4-38 (màn hình sẽ làm theo mockup này); spec `11-kich-ban-kiem-thu.md` (LV01–LV10, CH11, CH12, CH16, HT15); mẫu Task 141 (`docs/tasks/task-141.md`, `docs/mockups/m4/`); yêu cầu mới của chủ shop 25/11 (màn kết nối trung lập với kênh, chừa chỗ Zalo OA, TikTok; Cài đặt ẩn mục theo `Features` của adapter).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml`. Mockup chỉ vẽ dữ liệu của các operation đã đặt ở kế hoạch: `listChannelAccounts`, `startFacebookOAuth`, `exchangeFacebookOAuth`, `connectFacebookPage`, `disconnectChannel`, `runChannelHealthCheck`, đổi `send_mode`/Human Agent (Task M4-05), `getPageSettings`/`putPageSettings`, `listAutomationRules`/`putAutomationRule`, `listConversationTags` (+ tạo, sửa, xóa), `listReplyTemplates` (+ tạo, sửa, xóa), `listAssignableUsers`, `listFollowUps`; Live: `listLiveSessions`, `createLiveSession`, `endLiveSession`, `listLiveComments`, `getLiveCommentContext`, `claimLiveComment`, `listPendingDrafts`, `remindPendingDrafts`; của M1 `searchVariants`; của M2 nguồn đơn, Cài đặt đơn (`draft_ttl_minutes`). Tên operation chính xác là việc của Task M4-03; chỗ mockup cần trường chưa có ghi ở "Việc phát hiện thêm".
- Chỗ dùng trên màn hình: console chưa có màn M4. Mockup là đầu vào của Task M4-37 (Live, thiếu địa chỉ) và M4-38 (kết nối, cài đặt) sau Điểm dừng B. Menu: thêm mục Live, Đã chốt thiếu địa chỉ, Kênh kết nối, Cài đặt hộp chat vào nhóm "Hộp chat" của `docs/mockups/m4/khung.js` (mockup); `NAV_GROUPS` thật đổi ở Task M4-34, không ở task này.
- Câu sqlc, bảng, pha migration: không.
- Tệp sẽ đổi: tạo `docs/mockups/m4/{live.html,thieu-dia-chi.html,ket-noi-page.html,cai-dat.html}`, `png/<4 màn>-{1512,390,360}.png`; sửa `docs/mockups/m4/m4.css` (chỉ thêm khối cuối tệp cho thành phần mới), `khung.js` (4 mục menu), `soat.mjs` (4 trang, `CASES` mới), `index.html` (4 khung hình, bảng ca, bảng hành vi), `README.md` (bảng màn, ca, HV32 trở đi); `docs/tasks/task-142.md`. Không sửa `docs/mockups/m1/`, `m2/`, không sửa ba trang của Task 141.
- Test sẽ chạy: không có test mã. "Test" là `docs/mockups/m4/soat.mjs` (Playwright Chromium trong bộ đệm npx, như Task 141): mỗi trang × 1512/390/360 kiểm (1)–(10) như Task 141 và (8) đủ khung kế hoạch đòi theo `CASES` (mọi ca LV01–LV10, CH11, CH12, CH16, HT15 và các phần Interfaces). Sửa công cụ trước (thêm trang, khóa), chạy thấy đỏ (chưa có trang), rồi vẽ tới khi xanh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go. Mockup có sẵn: `docs/mockups/m1/m1.css` (token, khung, nút, ô nhập, `.toggle`, `.tabs`, chip, hộp báo, bảng → thẻ dưới 760 px, hộp thoại → tấm trượt đáy, `.empty`, `.kpis`), `docs/mockups/m2/m2.css` (`.sugg` gợi ý sản phẩm, `.avatar`, `.sum`, `.kv`, `.radios`, `.act .reason`, `.avail`), `docs/mockups/m2/cai-dat-don.html` (mẫu trang cài đặt của M2), `docs/mockups/m4/m4.css` của Task 141 (`.bnr` băng báo, `.lb` nhãn, `.unr` số chưa đọc, `.sec`, `.stats`, `.pg` chọn Page), `soat.mjs`, `khung.js`.
- Dùng lại / viết mới vì: dùng lại nguyên `../m1/m1.css`, `../m2/m2.css`, `m4.css` bằng liên kết; thêm vào cuối `m4.css` một khối "Task 142" chỉ cho thành phần chưa có (bố cục Live hai cột, dòng comment live, biểu tượng kênh bằng chữ, lưới tính năng kênh, dòng cài đặt có công tắc), chỉ bằng biến có sẵn. Khung chốt dùng lại ô tìm `.sugg`/`.avail` của Lên đơn M2 (cùng `VariantPicker`), không vẽ thành phần mới. `soat.mjs`, `khung.js` mở rộng tại chỗ (cùng thư mục M4, cùng công cụ), không chép bản mới.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã. Các câu dưới làm căn cứ cho **nội dung** mockup.

- 5.1–5.12: không áp dụng cho mã của task (không mã, bảng, route, thư viện mới trong repo; Playwright chạy từ bộ đệm npx, không thêm phụ thuộc). 5.9 (chống trùng): "Tạo đơn nháp và xin địa chỉ", "Nhắc", "Nhắc tất cả", "Lưu" cài đặt khóa khi đang gửi; đơn nháp một comment một đơn (unique `live_comment_claims`), màn chỉ hiện kết quả máy chủ trả.
- 6.1 (hai người cùng làm): LV07 "An đang lên đơn cho comment này" (409 `comment_claimed`) và dòng comment đổi thành "Đã lên đơn … bởi An"; cài đặt sửa cùng lúc → 409 `version_conflict` "Có người vừa sửa, tải lại trang".
- 6.2 (bấm hai lần): nút chốt, nhắc, lưu cài đặt, ngắt kết nối khóa khi đang gửi; "Nhắc" lần hai trong 60 phút máy chủ bỏ qua kèm lý do "đã nhắc lúc …".
- 6.3–6.11: không áp dụng: không có mã chạy. Nội dung liên quan: hết hàng không tạo nháp, không giữ một phần (LV08); nhắc bỏ qua dòng không gửi được và nói lý do; tin tự động không gửi khi Page chỉ xem hay khách không liên hệ (quy tắc `Run` Task M4-25).
- 6.12 (dữ liệu cá nhân): mã system user là ô mật khẩu, xóa ngay sau khi gửi, không hiện lại; không hiện token; SĐT trong comment live chỉ trên màn nội bộ; mockup dùng tên, số giả. Page ID là mã công khai của Page (không phải bí mật).
- 6.13 (quyền): Kết nối, ngắt, chỉ xem, Human Agent cần `channel.manage`; cài đặt Page cần `conversation.settings` trên Page; Live cần `live.operate`, chốt cần thêm `order.create` ở kho giữ hàng (HV28 của Task 141), "Thêm vào đơn" cần `order.update`. Nút không bấm được hiện mờ kèm lý do bằng chữ; không gì chỉ hiện khi rê chuột. Kiểm thật vẫn ở API.

## Bằng chứng

Lệnh tự soát (gọi tắt `SOAT`): `PW_DIR=~/.npm/_npx/5e2e484947874241/node_modules/playwright node docs/mockups/m4/soat.mjs` (Playwright của bộ đệm npx, như Task 141).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Công cụ soát đỏ trước khi có trang | `soat.mjs` (thêm 4 trang và `CASES` trước khi vẽ) | `SOAT --no-shot live thieu-dia-chi ket-noi-page cai-dat` | `live: chưa có live.html`, `thieu-dia-chi: …`, `ket-noi-page: …`, `cai-dat: …`, `SOÁT MOCKUP: 4 lỗi`, exit 1 |
| Công cụ bắt thiếu khung và chữ tràn (kiểm chính công cụ) | sửa tạm `ket-noi-page.html`: đổi `data-ca="zalo-oa"`, nhân bốn lần chữ chip "Không có comment" | `SOAT --no-shot ket-noi-page` | `thiếu trạng thái "zalo-oa"`, `tràn phải 225px: span.chip.feat-no …`; `SOÁT MOCKUP: 13 lỗi`; chép lại bản cũ → `SOÁT MOCKUP OK: 1 trang, 3 ảnh` |
| Công cụ bắt lỗi thật khi vẽ | `soat.mjs` | `SOAT thieu-dia-chi`, `SOAT cai-dat` | `mục chạm hẹp 25px < 44px: a "Live"` (liên kết đường dẫn ngắn) → đổi chữ thành "Hộp chat · Live"; `cuộn ngang: trang rộng 434px > 390px`, `tràn phải 74px: span.chip.feat-no` (chip dài ở khung "sau này" của Cài đặt) → tách bốn chip; cả hai về OK |
| Bốn màn của kế hoạch, chụp 1512×767, 390×844, 360×800 | `soat.mjs` | `SOAT` (mọi trang, chụp lại) | `SOÁT MOCKUP OK: 8 trang, 23 ảnh`, exit 0; `ls docs/mockups/m4/png \| wc -l` → 23 (12 ảnh mới của 4 màn) |
| Mỗi ca LV01–LV10, CH11, CH12, CH16, HT15 có ít nhất một khung hình; mọi phần Interfaces có khung (chọn phiên, số comment nhận/trên Facebook, lọc, ba kết quả lỗi chốt, nhãn hỏi định kỳ, ba trạng thái xin địa chỉ, Nhắc/Nhắc tất cả, nút kết nối, system user, kiểm tra, ngắt, chỉ xem hỏi lại, Human Agent, mọi mục cài đặt Page và cài đặt chung) | `soat.mjs` (8) với `CASES` của 4 trang (38 + 16 + 23 + 29 khóa) | `SOAT` | 0 lỗi "thiếu trạng thái"; bảng ca → khung hình của `README.md` có 31 dòng (thêm LV01–LV03, LV05–LV10, HT15; sửa CH11, CH12, CH16, LV04), cùng bảng ở `index.html` |
| Dữ liệu cứng: 600 comment trong 2 phút, tên 60 ký tự, 50 đơn nháp, 0 đơn nháp | đo bằng Python; soát (1), (2) | `python3 len()`; `SOAT` | tên = 60 ký tự; comment 20:00:00–20:02:00, "600 comment"; bảng "Đang xem 6 / 50 đơn nháp"; trạng thái C "Không còn đơn nào thiếu địa chỉ"; không tràn ở 360 |
| Giới hạn ô có số đúng | tính bằng Python | `python3 len()` | tin chào 75/640 ký tự; nội dung mẫu 116/2000; nút "Tư vấn size áo và váy" = 21 > 20 (ca lỗi) |
| Không cuộn ngang, không tràn, mục chạm đủ, chỉ token có sẵn, ô chọn đúng, không gì chỉ hiện khi rê chuột, tiền `690.000 ₫` | `soat.mjs` (1)–(10) | `SOAT` | 0 lỗi lần chạy cuối (cả `m4.css` mới: không luật `:hover`) |
| Soát bằng mắt ảnh 1512, 390, 360 | đọc ảnh | — | Đã sửa: danh sách kết quả "Nhắc tất cả" bị ép hai cột ở 390 (thêm `.res li` một cột dưới 760 px). Không thấy chữ tràn hay nút bị che |
| Số tiền khớp nhau | tính tay | — | Đầm lụa be L 1.290.000; váy hoa nhí S và áo len kem M 690.000; DH0001242 = 690.000 + 2 × 690.000 = 2.070.000; DH0001238 = 2 × 1.290.000 (2.580.000, như Task 141) + 690.000 sau "Thêm vào đơn" = 3.270.000 |
| Nội dung khớp kế hoạch, spec | đối chiếu Task M4-09, 10, 20, 21, 25–28, 30–33, 37, 38 | — | Page mới `read_only` (Q22); hỏi lại khi tắt chỉ xem đúng câu của Task M4-38; system user không lưu; Human Agent "Chỉ bật khi Meta đã cấp"; mất quyền vẫn nhận tin (HT15); hỏi định kỳ 2 giây, thử lại luồng mỗi 5 phút; mở lại trong khoảng 15 giây, lấy bù từ comment cuối (LV10); `comment_claimed`, `out_of_stock` "Hết hàng (còn N)", `open_draft_exists` hai nút, `seller_mismatch` (Task M4-37); nhắc bỏ qua "đã nhắc lúc …" trong 60 phút, "Đã quá 24 giờ và đã nhắn riêng cho comment này" (Task M4-33); tự ẩn ba chế độ, ≤ 200 bài ngoại lệ; ≤ 5 câu, `perUserPerPostHours` 1–72 mặc định 24, `includeLive` tắt; tin chào ≤ 640 ký tự, ≤ 13 nút, 20 ký tự; nhắn lại 1–23 giờ, tắt mặc định; đọc định kỳ `auto/on/off`; hạn nháp ở Cài đặt đơn M2 (Q2); biến mẫu đúng Task M4-21 |
| Bảng hành vi, mỗi hành vi một dòng để chủ shop OK riêng | `README.md`, `index.html` | `grep -c '^\| HV' docs/mockups/m4/README.md` | `55` (HV32–HV55 mới, cột OK = "chưa"), gồm HV32 "màn Kênh kết nối trung lập với kênh, chừa chỗ Zalo OA/TikTok" và HV33 "ẩn khối theo Features của adapter" |
| Gộp `main` trước khi stage | — | `git merge main` | `Already up to date.` (main ở `6f42973`) |
| Chỉ đổi tài liệu, không chạm `CODE_PATHS`, không sửa `docs/mockups/m1/`, `m2/` | — | `git diff --cached --name-only` | chỉ `docs/mockups/m4/**` và `docs/tasks/task-142.md` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thư mục `docs/mockups/m4/` thay cho `docs/mockup/m4/` của kế hoạch (như Task 141, theo ghi chú điều phối).
- Màn "Kết nối Page" vẽ thành "Kênh kết nối" trung lập với kênh, có khung "sau này" cho Zalo OA, TikTok (yêu cầu mới của chủ shop 25/11). Tên tệp vẫn `ket-noi-page.html` như kế hoạch.
- Sửa thêm ngoài danh sách tệp của kế hoạch: `m4.css` (khối cuối tệp), `khung.js` (4 mục menu), `soat.mjs` (4 trang, `CASES`), `index.html` (4 khung hình, hai bảng). Ảnh 1512 của ba màn Task 141 và `index` chụp lại vì menu bên trái có thêm 4 mục (ảnh 390, 360 của ba màn đó không đổi).
- Không chạy `ci-task.sh` và agent `reviewer`: task chỉ vẽ mockup (tài liệu), theo giao việc của phiên và CLAUDE.md bước 7 ("Task chỉ đổi tài liệu, cấu hình: không cần reviewer").
- Agent `design-critic` không có trong phiên; soát bằng `soat.mjs` và soát mắt.

## Quyết định thay chủ shop

- **Tên Page "ELIMAZ", Page ID 256660660862565** (theo chủ shop 25/11) cho Page chính ở bốn màn mới; Page thứ hai giữ "ELIMAZ Sale" của Task 141 với Page ID ghi rõ "(mã giả)". Ba màn của Task 141 vẫn ghi "ELIMAZ Official": không sửa màn đã xong ngoài phạm vi; ghi ở Việc phát hiện thêm 2. Phương án khác: sửa cả ba màn 141.
- **Biểu tượng kênh bằng chữ trên nền trung tính** ("FB", "Zalo", "TT") cùng một kiểu cho mọi kênh, kể cả Facebook (không token màu thương hiệu nào; "TikTok" sáu chữ không vừa ô 40 px nên dùng "TT", tên đầy đủ ở dòng chữ bên cạnh). Phương án khác: logo ảnh (cần chủ shop OK màu mới). HV32.
- **Khối Cài đặt kênh không hỗ trợ bị ẩn hẳn**, không hiện mờ (đúng chữ chủ shop "ẩn theo Features"); khung "sau này" liệt kê khối còn lại của Zalo OA. HV33.
- **Dòng Zalo OA, TikTok** chỉ nằm trong khung viền đứt "Mockup: trạng thái sau này", nút kết nối mờ kèm lý do "Sau M4"; không vẽ như tính năng M4.
- **Mỗi khối cài đặt có nút Lưu riêng** vì mỗi khối là một tài nguyên riêng có `version` riêng (`PUT …/automation-rules/{kind}`, `PUT …/page-settings`); một nút Lưu chung sẽ gửi nhiều lệnh, lỗi `version_conflict` của một khối khó báo đúng chỗ. HV52. Phương án khác: một nút Lưu chung cuối trang.
- **Tuổi tối đa của sự kiện mặc định 15 phút**: kế hoạch không ghi số mặc định, test Task M4-25 dùng "sự kiện cũ 16 phút → skipped_stale". HV54; Việc phát hiện thêm 3.
- **Đường màn** `/live`, `/live/thieu-dia-chi`, `/settings/inbox` là đề xuất (kế hoạch chỉ đặt `/settings/channels/facebook/callback`); ghi "(đề xuất)" trong README.
- **Khung chốt Live 400 px bên phải**, dưới 1200 px xuống dưới danh sách (cùng nguyên tắc ba cột của Task 141). HV39.

## Việc phát hiện thêm

Không tự sửa kế hoạch hay hợp đồng; ghi để task sau xử lý.

1. **`listChannelAccounts` cần trả tính năng của kênh** (`channel.Features` của adapter) để màn Kênh kết nối hiện cột tính năng và Cài đặt ẩn khối theo kênh (HV32, HV33). Kế hoạch Task M4-03 chỉ có `Features()` trong interface adapter, chưa có trong phản hồi API. Cũng cần `lastWebhookAt`, người và cách kết nối (`token_kind`), `subscribed_fields`, `token_checked_at` cho các dòng "Lần kiểm gần nhất", "Webhook", "Tin gần nhất", "Kết nối bởi". Task M4-03/M4-05 đối chiếu.
2. **Tên Page trong mockup**: Task 141 dùng "ELIMAZ Official"; chủ shop cho tên thật "ELIMAZ" (Page ID 256660660862565). Điều phối viên đồng bộ ba màn 141 khi chốt Điểm dừng B (chỉ đổi chữ).
3. **`automation_max_age_minutes` chưa có giá trị mặc định** trong kế hoạch Task M4-25 (mockup đề xuất 15). Task M4-25 ghi mặc định và khoảng cho phép.
4. **`listLiveSessions` cần số liệu phiên** mà màn Live hiện: số comment có SĐT, số đã lên đơn, số đơn thiếu địa chỉ, `active_source` (hỏi định kỳ), `meta_comment_count`, `detected_by` và giờ nhận biết. Task M4-30/M4-37 đối chiếu.
5. **Danh sách comment live cần trường `isHidden`** (đã có) và người nhắn riêng (`privateReply.by`) để hiện "Đã nhắn riêng · Lê Thu Hằng"; kế hoạch chỉ có `privateReply:{state}`.
6. **Nút "Mở hội thoại để hoàn tất"** cần `conversationId` (hội thoại hộp thư) trong `pending-drafts`; kế hoạch Task M4-33 chưa liệt kê trường này.
7. **Tên trường webhook** trong ô "Kiểm tra kết nối" (message_edits, live_videos) chỉ là ví dụ; danh sách 11 trường do Task M4-09 chốt.
8. **Kênh sau này (Zalo OA, TikTok)**: luật gửi khác Facebook (cửa sổ, tin mẫu trả phí của Zalo) nằm trong `SendPolicy` của từng adapter; màn ô gõ của Task 141 đã đọc `sendPolicy`. Không có việc cho M4; ghi để mốc thêm kênh đọc lại.
9. Ảnh chụp 23 tệp, khoảng 15 MB trong git; nén PNG ở lần chốt nếu cần nhẹ hơn (như Task 141).

## Reviewer

Không chạy: task chỉ đổi tài liệu (mockup HTML, CSS, công cụ chụp, ảnh, tệp task), không có tệp nào trong `CODE_PATHS`; CLAUDE.md bước 7 "Task chỉ đổi tài liệu, cấu hình: không cần reviewer", và giao việc của phiên ghi rõ không cần ci-task, reviewer. Tự soát thay: `soat.mjs` 0 lỗi trên 8 trang, 23 ảnh; soát mắt; đối chiếu nội dung với kế hoạch (bảng bằng chứng).

Kết luận: KHÔNG CẦN (chỉ tài liệu)
