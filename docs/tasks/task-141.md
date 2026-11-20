# Task 141 — M4-01: Mockup phần hộp chat

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 141 = M4-01, trước Điểm dừng B). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu (không có tệp nào trong `CODE_PATHS`).

Căn cứ: kế hoạch M4 mục "Task 141 (M4-01)", "Điểm dừng B", Global Constraints của M4; Interfaces của Task M4-03 (bảng operation, quyền `conversation.read|reply|assign|settings`, `channel.manage`, `live.operate`), M4-08 (`SendPolicy`, `DenyReason`, câu lỗi tiếng Việt của `Reason`), M4-11 (`messages.kind|origin|send_status`); quyết định Q7, Q9, Q11, Q19, Q24, Q33, Q34; spec `11-kich-ban-kiem-thu.md` (CH01–CH19, LV04, DH03); Task 411 (`order.create` theo Page không giữ hàng được → 403); hệ thiết kế đã duyệt của M1 (`docs/mockups/m1/m1.css`, gốc `apps/storefront-giftstatus/DESIGN.md`), thành phần M2 (`docs/mockups/m2/m2.css`, `OrderForm` của `console/src/screens/len-don/OrderForm.tsx`) và khung console thật (`console/src/app/nav.ts`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng M4 là của Task M4-03, chưa làm). Mockup chỉ vẽ dữ liệu của các operation đã đặt tên ở bảng Task M4-03: `listChannelAccounts`, `listConversations`, `countConversations`, `getConversation`, `getConversationCustomerCard`, `getConversationOrderContext`, `listMessages`, `listMessageRevisions`, `markConversationRead`, `setConversationStatus`, `getAttachmentUrl`, `sendMessage`, `resendMessage`, `discardMessage`, `replyToComment`, `sendPrivateReply`, `hideComment`, `unhideComment`, `claimConversation`, `assignConversation`, `listConversationAssignments`, `listConversationTags`, `addConversationTag`, `removeConversationTag`, `listAssignableUsers`, `addConversationNote`, `listReplyTemplates`, `renderReplyTemplate`, `setPresence`, `getPresence`, `listFollowUps`, `createReminder`, `completeReminder`, `createCallLog`, `listCustomerCalls`, `setCustomerDoNotContact`; của M2: `createOrder`, `confirmOrder`, `getCustomer`, `listCustomerMergeCandidates`; của M1: `searchVariants`, `createUpload`, `completeUpload`. Chỗ mockup cần trường chưa có trong kế hoạch ghi ở "Việc phát hiện thêm".
- Chỗ dùng trên màn hình: console chưa có màn M4. Mockup là đầu vào của Task M4-34 (Inbox, khung console M4), M4-35 (hội thoại), M4-36 (khung bên phải) sau Điểm dừng B. Nhóm menu mới ("Hộp chat") sẽ đổi `NAV_GROUPS` của `console/src/app/nav.ts` ở Task M4-34, không ở task này. Khung lên đơn dùng lại `OrderForm` của M2 (không sửa ở task này).
- Câu sqlc, bảng, pha migration: không.
- Tệp sẽ đổi: tạo `docs/mockups/m4/` gồm 3 trang của kế hoạch (`inbox.html`, `hoi-thoai.html`, `khung-ben-phai.html`), `index.html` (trang xem ảnh, bảng ca và bảng hành vi, như M1, M2), `README.md` (bảng màn → ca kiểm → khung hình, hành vi cần OK, kết luận duyệt), `m4.css` (chỉ thành phần mới, nạp sau `../m1/m1.css` và `../m2/m2.css`), `khung.js` (menu có nhóm Hộp chat), `soat.mjs` (công cụ chụp và tự soát, chép từ M2 và mở rộng), `png/*`; `docs/tasks/task-141.md`. Không sửa gì trong `docs/mockups/m1/`, `docs/mockups/m2/`.
- Test sẽ chạy: không có test mã. "Test" của task là `docs/mockups/m4/soat.mjs` (Playwright Chromium trong bộ đệm npx, như M1, M2): mỗi trang × mỗi cỡ (1512×767, 390×844, 360×800) kiểm (1) không cuộn ngang, không tràn phải/trái, không tràn khỏi ô bảng; (2) không chữ bị cắt ngoài chỗ cố ý có dấu `…`; (3) mục chạm ≥ 44 px dưới 1000 px, ≥ 32 px từ 1000 px; (4) màu, cỡ chữ, bo góc thuộc tập có sẵn của DESIGN.md (cùng tập M1, M2); (5) mọi `<select>` trong `.select-wrap`, chữ lựa chọn không bị cắt; (6) không luật `:hover` nào làm hiện/ẩn (soát `m1.css`, `m2.css`, `m4.css`); (7) tiền dạng `690.000 ₫`; (8) đủ khung kế hoạch đòi (danh sách `CASES` trong công cụ, gồm mọi ca CH01–CH19, LV04, DH03); thêm cho M4: (9) bong bóng tin không rộng quá khung hội thoại và chữ dài (tên 60 ký tự, comment 2.000 ký tự, tên tệp dài) xuống dòng trong khung; (10) ảnh dọc rất cao bị giới hạn chiều cao. Viết công cụ trước, chạy thấy đỏ (chưa có trang), rồi vẽ tới khi xanh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go. Mockup có sẵn: `docs/mockups/m1/` (token, khung, nút, ô nhập, bảng → thẻ dưới 760 px, hộp thoại → tấm trượt đáy, toast, chip, hộp báo), `docs/mockups/m2/` (`m2.css`: danh sách gợi ý `.sugg`, ảnh đại diện `.avatar`, khối tổng tiền `.sum`, thanh nhãn `.lbl`, dòng thời gian `.tl`, `.kv`, `.note-box`, `.radios`; khung Lên đơn của `len-don.html`; `soat.mjs` có kiểm tràn trái, tràn ô bảng, dạng tiền, đủ trạng thái).
- Dùng lại / viết mới vì: dùng lại nguyên `../m1/m1.css` và `../m2/m2.css` bằng liên kết (không chép) để màn M4 cùng thành phần với M1, M2 và console; `m4.css` chỉ thêm thành phần chưa có (bố cục ba cột hộp chat, dòng hội thoại, bong bóng tin, ô gõ, băng báo trên đầu danh sách, thẻ bài viết), chỉ bằng biến của `m1.css`. `khung.js` viết bản M4 vì menu khác (thêm nhóm Hộp chat đứng đầu); giữ đúng cấu trúc HTML của bản M2. `soat.mjs` chép từ M2 rồi đổi danh sách trang, soát thêm `m4.css` và thêm kiểm (9), (10); không sửa bản M1, M2 để không đổi bằng chứng của Task 49, 83. Khung lên đơn ở khung bên phải vẽ lại theo `OrderForm` của M2 (cùng ô, cùng nút "Lưu và xác nhận") ở bản hẹp, không vẽ thành phần mới.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã. Các câu dưới làm căn cứ cho **nội dung** mockup (màn hình phải nói đúng luật API và adapter sẽ làm).

- 5.1–5.12: không áp dụng cho mã của task (không mã, bảng, route, thư viện mới trong repo; Playwright chạy từ bộ đệm npx của máy, không thêm phụ thuộc). 5.9 (khóa chống trùng): ô gõ khóa nút Gửi khi đang gửi; tin "không rõ đã gửi chưa" chỉ gửi lại khi nhân viên bấm (Q24), có câu cảnh báo khách có thể nhận hai lần.
- 6.1 (hai người cùng làm): dòng "An đang xem", "An đang trả lời" (Q9, CH10); nút "Nhận" chỉ hiện khi hội thoại chưa ai nhận (máy chủ từ chối 409 `already_assigned` nếu đã có người); hội thoại đã có người phụ trách chỉ có "Giao cho…" (cần `conversation.assign`); nhắn riêng một comment hai lần thì lần sau khóa (một tin riêng sống mỗi comment, unique một phần của `messages`).
- 6.2 (bấm hai lần): Gửi, Gửi lại, Bỏ, Nhận, Lưu kết quả cuộc gọi, Lưu và xác nhận đơn: nút khóa khi đang gửi.
- 6.3–6.11: không áp dụng: không có mã chạy. Nội dung liên quan: lệnh gửi `in_doubt` không tự gửi lại (Global Constraints M4 mục 1); tin quá 3 phút thành "chưa gửi được", không gửi trễ (CH19); tin tự động chỉ một lần mỗi comment (CH12, Q11).
- 6.12 (dữ liệu cá nhân): SĐT, địa chỉ chỉ hiện trên màn của người có `conversation.read`/`customer.read` (màn nội bộ); nhắc việc, cuộc gọi, ghi chú chỉ trong CRM; ô tìm theo SĐT: mockup không vẽ URL, việc bộ lọc trên URL của Task M4-34 ghi ở Việc phát hiện thêm 1; mockup dùng tên và số giả.
- 6.13 (quyền): nút theo quyền trong `/api/v1/me`: Giao cho… cần `conversation.assign`; Gửi, Nhận, nhãn, ghi chú cần `conversation.reply`; lên đơn cần `order.create` **ở kho giữ hàng** (Task 411): người chỉ có `order.create` theo Page thấy khung lên đơn khóa kèm lý do bằng chữ. Nút không bấm được hiện mờ **kèm lý do bằng chữ**, không gì chỉ hiện khi rê chuột. Page chỉ xem (`send_mode = read_only`) khóa ô gõ kèm lý do. Kiểm thật vẫn ở API và `SendPolicy`.

## Bằng chứng

Lệnh tự soát (gọi tắt `SOAT`): `PW_DIR=~/.npm/_npx/5e2e484947874241/node_modules/playwright node docs/mockups/m4/soat.mjs` (Playwright 1.60.0 của bộ đệm npx, như Task 49, 83).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Công cụ soát chạy đỏ trước khi có trang | `soat.mjs` | `SOAT --no-shot` (lúc chưa có trang) | `m4.css: chưa có`, `inbox: chưa có inbox.html`, `hoi-thoai: …`, `khung-ben-phai: …`, `index: …`, `SOÁT MOCKUP: 5 lỗi`, exit 1 |
| Công cụ bắt thiếu khung, ảnh quá cao, bong bóng tràn khung, chữ tràn (kiểm chính công cụ) | sửa tạm `hoi-thoai.html`: bỏ mọi `tt-khong-ro`, ảnh dọc cao 900 px bỏ giới hạn, một bong bóng `nowrap` không giới hạn rộng, tên tệp `nowrap` | `SOAT --no-shot hoi-thoai` | `thiếu trạng thái "tt-khong-ro"`, `ảnh cao 900px > 360px`, `tràn khỏi khung 271px: div.bub`, `tràn phải 286px: b "bang-do-so-do…"`, `cuộn ngang`; `SOÁT MOCKUP: 12 lỗi`; chép lại bản cũ → `SOÁT MOCKUP OK: 1 trang, 3 ảnh` |
| Công cụ bắt lỗi thật khi vẽ (khung bên phải lượt đầu) | `soat.mjs` | `SOAT khung-ben-phai` | `mục chạm thấp 20px < 32px: button.ibtn "×"` (nút gỡ nhãn), `cuộn ngang: trang rộng 574px > 390px` (tên 60 ký tự trong thẻ khách), `mục chạm thấp 19px: a.btn "‹ Hội thoại"`; `SOÁT MOCKUP: 16 lỗi`; sửa `m4.css` (nút gỡ nhãn 32/44 px, `.sec > * { min-width: 0 }`, `.only-phone` thành `inline-flex`) → OK |
| Ba màn của kế hoạch + `index.html`, chụp 1512×767, 390×844, 360×800 (360 cho cả ba màn) | `soat.mjs` | `SOAT` | `SOÁT MOCKUP OK: 4 trang, 11 ảnh` (lần chạy cuối); `ls docs/mockups/m4/png` → 11 tệp (`hoi-thoai`, `inbox`, `khung-ben-phai` mỗi màn 3 cỡ; `index` 2 cỡ), 6,9 MB |
| Mỗi ca CH01–CH19, LV04, DH03 có ít nhất một khung hình; mọi phần Interfaces liệt kê có khung (sáu trạng thái gửi, bốn trạng thái ô gõ, ba băng, 0/1/200 hội thoại, 0/9/120 chưa đọc, 0/14 nhãn…) | `soat.mjs` kiểm (8), danh sách `CASES` (khóa `chNN`, `lv04`, `dh03` và khóa Interfaces) | `SOAT` | 0 lỗi "thiếu trạng thái"; bảng ca → khung hình 21 dòng trong `README.md` và `index.html` |
| Dữ liệu cứng: tên 60 ký tự không dấu cách, comment 2.000 ký tự, ảnh dọc 1080 × 4320, tên tệp dài | đo bằng Python; soát (9), (10) | `python3 len()`; `SOAT` | tên = 60, comment = 2000; ảnh dọc cao 360 px; không tràn ở 360 |
| Không cuộn ngang, không tràn, mục chạm đủ, chỉ token có sẵn, ô chọn đúng, không gì chỉ hiện khi rê chuột, tiền `690.000 ₫` | `soat.mjs` (1)–(10) | `SOAT` | 0 lỗi lần chạy cuối |
| Soát bằng mắt ảnh 1512, 390, 360 (Step 3) | đọc ảnh | — | Đã sửa: nhãn "Tin tự động" màu nhạt trên nền trắng (đổi sang `--label`); nút "‹" bị gạch chân; nút "‹ Hội thoại" mất dạng nút ở điện thoại. Không thấy chữ tràn hay nút bị che ở 360 |
| Nội dung khớp kế hoạch, spec | đối chiếu | — | Danh sách khách chưa trả lời theo `listFollowUps` của Task M4-28 (mặc định 3 giờ, loại khách quá 24 giờ/không liên hệ/đã chặn: đã bỏ dòng "quá 24 giờ" vẽ lúc đầu); "không liên hệ" chỉ chặn tin tự động (Q13: đã bỏ khóa nút gọi vẽ lúc đầu); "đang kiểm tra" 60 giây và "không rõ đã gửi chưa" theo Q24; "chưa gửi được" sau 3 phút (CH19); Human Agent 7 ngày, không cho tin tự động (Task M4-08); câu lỗi lấy từ bảng `Reason` của Task M4-08; tự trả lời comment "câu 2/5" (Q11) |
| Số tiền khớp nhau | tính tay | — | Khung lên đơn: DAM0201-BE-L × 2 = 2 × 1.290.000 = 2.580.000; + ship 30.000 = 2.610.000 (cùng số ở đơn nháp, gợi ý nhãn) |
| Bảng hành vi, mỗi hành vi một dòng để chủ shop OK riêng | `README.md`, `index.html` | `grep -c '^| HV' docs/mockups/m4/README.md` | `31` (HV01–HV31, cột OK = "chưa"), gồm HV28: nhân viên chỉ có quyền lên đơn theo Page (Task 411) |
| Gộp `main` trước khi stage | — | `git merge main` | fast-forward tới `31bb78e`, không xung đột (task không chạm tệp nào của main) |
| Cổng kiểm của task một lần trước khi stage | `ci-task.sh` | `CRM_PG_PORT=5600 ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 31bb78e…)`: build, vet, lint `0 issues.`, catalog-check, jscpd, gitleaks, oasdiff, test-guard, `ok elimaz.com.vn/crm/tests/arch`; gen-check, sqlc-vet bỏ (không đổi nguồn sinh, SQL); không có gói Go nào đổi nên không chạy test gói |
| Chỉ đổi tài liệu, không chạm `CODE_PATHS`, không sửa `docs/mockups/m1/`, `m2/` | — | `git diff --cached --name-only` | chỉ `docs/mockups/m4/**` và `docs/tasks/task-141.md` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thư mục `docs/mockups/m4/` thay cho `docs/mockup/m4/` của kế hoạch (xem Quyết định thay chủ shop).
- Thêm tệp ngoài danh sách của kế hoạch: `index.html` (trang xem ảnh và hai bảng, như M1, M2), `m4.css`, `khung.js`, `soat.mjs` (công cụ chụp và tự soát cho Step 2–3). Đều trong `docs/mockups/m4/`, không phải mã CRM.
- Ảnh 360 cho cả ba màn (kế hoạch: 360 cho dòng hội thoại và bong bóng): khung bên phải có tên 60 ký tự và khung lên đơn hẹp.
- Bảng "Kết luận duyệt" để "Chờ điều phối viên duyệt" (Điểm dừng B không phải việc của phiên làm task, như Task 83).
- Kế hoạch không đòi reviewer mã cho task mockup; vẫn chạy agent `reviewer` theo yêu cầu của phiên.
- Agent `design-critic` không có trong phiên này; soát bằng `soat.mjs` và soát mắt.

## Quyết định thay chủ shop

- **Thư mục `docs/mockups/m4/`** (kế hoạch ghi `docs/mockup/m4/`, kể cả ở Điểm dừng B). Vì sao: mockup M1, M2 đã ở `docs/mockups/`, trang M4 liên kết `../m1/m1.css`, `../m2/m2.css`; ghi chú điều phối của phiên cũng chỉ `docs/mockups/m4/`. Phương án khác: theo đúng chữ kế hoạch (thư mục riêng, liên kết `../../mockups/…`). Đảo ngược: `git mv` một thư mục.
- **Chọn Page đặt trên đầu cột danh sách** (kế hoạch: "cột Page"). Vì sao: ở 1512 bốn cột (Page, danh sách, hội thoại, khung phải) làm cột hội thoại còn khoảng 350 px; shop chỉ có 2 Page. Phương án khác: cột Page riêng 184 px, khung phải thu vào nút. Ghi thành HV03 để chủ shop OK.
- **"Khách chưa trả lời" mặc định 3 giờ**, chỉ hiện khách còn cửa sổ 24 giờ, không hiện khách không liên hệ hay đã chặn: đúng `listFollowUps` của Task M4-28 (`afterHours=3`). HV09.
- **Khách "không liên hệ" không khóa nút gọi, ô gõ**: Q13 chỉ chặn tin tự động, tin nhân viên gõ vẫn gửi kèm cảnh báo. HV29.
- **Nhân viên chỉ có `order.create` theo Page: khung lên đơn khóa sẵn kèm lý do và cách xin quyền** (Task 411 để màn chỉ báo 403 sau khi bấm, ghi "ẩn nút trước cần mockup"). Vì sao: nhân viên chat là người hay được gán theo Page nhất; biết trước đỡ gõ cả đơn rồi mới bị từ chối. API vẫn kiểm. Cần chủ shop OK riêng: HV28. Phương án khác: giữ cách Task 411 (bấm rồi báo lỗi).
- **Comment, tin dài hiện 8 dòng rồi "Xem hết (n ký tự)"** (HV22); **ảnh cao tối đa 360 px** (HV13); **"Gửi lại" ở tin không rõ đã gửi không phải nút nổi bật** (HV14) để không khuyến khích gửi trùng.
- **Ô "hoàn" của thẻ khách tô đỏ theo nhãn "Hay hoàn" của M2** (không đặt ngưỡng mới). HV24.

## Việc phát hiện thêm

Không tự sửa kế hoạch hay hợp đồng; ghi để task sau xử lý.

1. **Bộ lọc Inbox trên URL** (Task M4-34 để bộ lọc trên URL, `listConversations` nhận `q` qua query string): ô tìm theo SĐT sẽ đưa SĐT lên URL và lịch sử trình duyệt. Task M4-34 cân nhắc không đưa `q` lên URL của màn (vẫn gửi API như M2).
2. **Dòng của `listConversations`** cần: 3 nhãn đầu + tổng số nhãn, tên bài của comment cuối, tên người phụ trách (`Actor{id, name}`, như phát hiện 5 của Task 83), cờ hồ sơ "khách tiềm năng mới". Task M4-19 đối chiếu.
3. **Khóa khung lên đơn trước (HV28)** cần console biết kho giữ hàng sẽ dùng (kho chính khi bỏ trống) để so với grant `order.create` trong `/api/v1/me`: thêm `warehouseId` mặc định vào `getConversationOrderContext` hoặc trường "có quyền lên đơn ở kho mặc định". Task M4-19/M4-36 quyết; không thêm thì giữ cách Task 411.
4. **Thẻ khách** (`getConversationCustomerCard`) cần nhãn khách của M2 (để tô ô hoàn, chip "Khách quen") và cờ `do_not_contact` kèm người, ngày đặt.
5. **"Giao cho…" hiện số hội thoại đang phụ trách** mỗi nhân viên: `listAssignableUsers` chưa ghi trường đếm. Task M4-20 thêm hoặc bỏ số khỏi màn.
6. **"Lấy địa chỉ từ tin của khách" (HV26)** chỉ chép thân tin vào ô (không cần operation); nếu chủ shop muốn tự tách tỉnh/xã thì là việc mới.
7. **Đồng hồ còn lại của ô gõ** dùng `getConversation().sendPolicy` (đã có ở Task M4-35); chỉ cần đối chiếu `Until` của chế độ Human Agent (7 ngày) để hiện hạn.
8. Câu báo trong mockup ("Không rõ đã gửi chưa", "Chưa gửi được", lý do `DenyReason`, băng Page chỉ xem/mất kết nối) là bản đề xuất cho `console/src/app/errors.ts` (Task M4-34–36).
9. **Nút "Trả lại (bỏ nhận)"** dùng `assignConversation {userId: null}` (Task M4-20) nên cần `conversation.assign`: người chỉ có `conversation.reply` không thấy nút này (mockup vẽ với tài khoản Chủ). Task M4-36 ẩn nút theo quyền.
10. **Tin "Lỗi" có nút hay không** (HV14): Task M4-35 không cho gửi lại tin lỗi, nhưng Task M4-17 cho gửi lại tin `failed` vì lý do tạm (`rate_limited`) và cho "Bỏ" tin `failed`. Mockup vẽ lỗi vĩnh viễn (khách chặn Page) không có nút; Task M4-35 quyết có hiện "Gửi lại"/"Bỏ" cho lỗi tạm.
11. Ảnh chụp 11 tệp, khoảng 6,9 MB trong git; nén PNG ở lần chốt nếu cần nhẹ hơn (như Task 49, 83).

## Reviewer

Lượt 1: **CẦN SỬA**, hai phát hiện: (1) hộp "Giao cho…" ghi lọc theo "quyền trả lời", kế hoạch lọc theo `conversation.read` (`listAssignableUsers`, `assignConversation` → 422 `assignee_not_allowed`) — đã sửa câu ở `khung-ben-phai.html`, HV31 (README, index); (2) đường dẫn màn `/hop-chat` mâu thuẫn kế hoạch Task M4-34 (`/inbox`, `/inbox/:conversationId`) — đã đổi theo kế hoạch, bỏ phát hiện "kế hoạch không đặt". Ghi chú không bắt buộc đã làm: bỏ nút "Nhận" trên hội thoại đã có người phụ trách (409 `already_assigned`), sửa 6.1; danh sách chưa trả lời gồm cả "chờ khách"; "Chưa có đơn đã xác nhận nào"; HV28 dẫn Task 412; Việc phát hiện thêm 1 (SĐT trên URL), 7, 9 (cần `conversation.assign`), 10 (tin lỗi tạm). Chạy lại `SOAT` → `SOÁT MOCKUP OK: 4 trang, 11 ảnh`; chạy lại cổng kiểm của task.

Lượt 2: **CẦN SỬA**, một phát hiện: bỏ nút "Nhận" ở hội thoại đã có người phụ trách làm mất nút "Nhận" khỏi cả mockup (Interfaces khung bên phải "Nhận / Giao cho…", CH06) trong khi bảng ca kiểm vẫn ghi có. Đã sửa: thêm nút "Nhận" (kèm "Giao cho…" và câu lý do) ở đầu hội thoại comment chưa ai nhận (`hoi-thoai.html`, trạng thái C); dòng CH06 của README, index ghi đúng chỗ vẽ; thêm khóa `ch06`, `nut-nhan` vào `CASES` của trang hội thoại để công cụ giữ khung này. `SOAT` → `SOÁT MOCKUP OK: 4 trang, 11 ảnh`. Không chạy lại cổng kiểm (chỉ đổi HTML, công cụ soát, ảnh; lượt 2 đã xanh trên cùng phần không phải tài liệu).

Lượt 3: **ĐẠT**. Phát hiện lượt 2 đã sửa đúng: nút "Nhận" ở hội thoại comment chưa ai nhận (`hoi-thoai.html` trạng thái C, khóa `nut-nhan`, `ch06`); hội thoại đã có người phụ trách chỉ có "Giao cho…" (khớp 409 `already_assigned`); dòng CH06 của README, index đúng chỗ vẽ. Không có phát hiện phải sửa. Reviewer không chạy được `node` (hook chặn), nên phiên làm task chạy lại `SOAT --no-shot` sau review → `SOÁT MOCKUP OK: 4 trang, 11 ảnh`, exit 0.

Kết luận: ĐẠT
