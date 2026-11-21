# Task 205 — M5-22: Mockup các màn CRM của M5 đợt 1 — Điểm dừng B

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 205 = M5-22, Điểm dừng B; mở rộng ở Task 450). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu (không có tệp nào trong `CODE_PATHS`).

Căn cứ: kế hoạch M5 mục "Task 205 (M5-22)" (Interfaces, các dòng "(Task 450)"), "Chuyển toàn bộ admin sang CRM" (Kiểm kê admin, Xếp module, menu console), "Việc cần chủ shop trong M5" (OK riêng từng hành vi khác admin; hình do điều phối viên chốt, Q-X8, C3-3), Interfaces của các task màn hình sẽ làm theo mockup: Task 207 (M5-24 Yêu cầu), 208 (M5-25 Khóa dịch vụ, Hãng trên website), 209 (M5-26 Quà sinh nhật, bốn tab theo Task 468), 458–462 (M5-46 → 50) và các task nền: 189 (khóa dịch vụ), 195 + "Bổ sung Task 450" (yêu cầu về đơn), 197 + 468 (đợt mã, sinh nhật), 198 (hãng trên web, phí dự phòng), 452 (chiến dịch), 453 (banner), 454 (bộ phối), 455 (góp ý), 456 (nhận đơn web), 457 (nhật ký); spec `11-kich-ban-kiem-thu.md` (WB04, WB05; FS01 thuộc đợt 2); admin đang chạy `apps/admin` `origin/main` `7199466` (`src/components/*.tsx`, `src/lib/*.mjs`) và website `apps/storefront` `origin/main` `90627d0` (`src/server/looks/rules.ts`, `banners/rules.ts`); hệ thiết kế CRM đã duyệt ở M1 (`docs/mockups/m1/m1.css`) và thành phần M2, M3 (`m2.css`, `m3.css`); cách làm mockup của Task 131 (`docs/mockups/m3/`).

## Ca kiểm

| Mã ca (11-kich-ban-kiem-thu.md) | Test | Kết quả |
|---|---|---|
| WB04 (V), WB05 (V) | `soat.mjs` kiểm (8): khóa `wb04` trong `CASES` của `order-requests`, `wb05` của `birthday-gifts` | xem Bằng chứng |
| FS01 (V) | không vẽ: màn Đợt flash sale thuộc đợt 2 (Task 450) | — |

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml`. Mockup chỉ vẽ dữ liệu có trong operation mà kế hoạch đã khai cho các task nền (chưa gộp vào `openapi.yaml`): `listOrderRequests`, `getOrderRequest`, `replyOrderRequest`, `setOrderRequestStatus` (195 + Bổ sung Task 450); `listServiceKeys`, `createServiceKey`, `revokeServiceKey` (189); `listCarrierAccounts`, `updateCarrierAccount` (M3, cờ `showOnWeb`, `webSort`), `getWebCarrierSettings`, `saveWebCarrierSettings` (198); `listCouponBatches`, `getCouponBatch` (197), `getBirthdaySettings`, `saveBirthdaySettings`, `runBirthdayGifts`, `listBirthdayTodo`, `markBirthdayMessaged`, `listBirthdayCustomers`, `exportBirthdayCustomers` (468); `listWebCampaigns`, `getWebCampaign`, `createWebCampaign`, `saveWebCampaign`, `previewWebCampaign`, `setWebCampaignState`, `deleteWebCampaign`, `listCampaignTargets` (452); `getBanners`, `previewBanners`, `saveBanners` + tải tệp mục đích `web_banner` (453); `getLooks`, `previewLooks`, `saveLooks` (454); `listFeedback`, `updateFeedback` (455); `getWebIntake`, `setWebIntake` (456); `listAuditLogs` (457). Chỗ mockup cần trường chưa có ghi ở "Việc phát hiện thêm".
- Chỗ dùng trên màn hình: console chưa có màn nào của M5. Mockup là đầu vào của Task 207, 208, 209, 458, 459, 460, 461, 462 sau Điểm dừng B. Nhóm "Website" và mục "Nhật ký thao tác" sẽ đổi `NAV_GROUPS` của `console/src/app/nav.ts` ở Task 462, không ở task này. Khối Góp ý gắn vào hồ sơ khách M2 (`docs/mockups/m2/khach-ho-so.html`), phần Hãng trên website gắn vào màn Kết nối vận chuyển M3 (`docs/mockups/m3/ket-noi-van-chuyen.html`): vẽ lại phần đó trong trang M5, không sửa mockup M2, M3.
- Câu sqlc, bảng, pha migration: không.
- Tệp sẽ đổi: tạo `docs/mockups/m5/` gồm 11 trang theo kế hoạch (`order-requests.html`, `service-keys.html`, `web-carriers.html`, `birthday-gifts.html`, `web-campaigns.html`, `web-banners.html`, `web-looks.html`, `feedback.html`, `web-intake.html`, `audit.html`, `menu.html`; **không** có `flash-sales.html`, đợt 2), `index.html` (trang so sánh "Hiện tại / Đề xuất": ảnh admin và ảnh đề xuất, bảng ca, bảng hành vi), `README.md`, `m5.css` (chỉ thành phần mới, nạp sau `../m1/m1.css`, `../m2/m2.css`, `../m3/m3.css`), `khung.js` (menu M5 đủ nhóm), `soat.mjs` (công cụ chụp và tự soát, chép từ M3 rồi đổi), `shots/*` (ảnh đề xuất `<màn>-{1512,390,360}.png` và ảnh admin hiện tại `hien-tai-*.png`); `docs/tasks/task-205.md`. Không sửa gì trong `docs/mockups/m1/`, `m2/`, `m3/`, `m4/`.
- Test sẽ chạy: không có test mã. "Test" của task là `docs/mockups/m5/soat.mjs` (Playwright Chromium trong bộ đệm npx, như Task 49, 83, 131, 141): mỗi trang × mỗi cỡ (1512×767, 390×844, 360×800) kiểm (1) không cuộn ngang, không tràn phải/trái, không tràn khỏi ô bảng; (2) không chữ bị cắt ngoài chỗ cố ý có dấu `…`; (3) mục chạm ≥ 44 px dưới 1000 px, ≥ 32 px từ 1000 px; (4) màu, cỡ chữ, bo góc thuộc tập có sẵn (cùng tập M1–M4); (5) mọi `<select>` trong `.select-wrap`, chữ lựa chọn không bị cắt; (6) không luật `:hover` nào làm hiện/ẩn (soát `m1.css`, `m2.css`, `m3.css`, `m5.css`); (7) tiền dạng `690.000 ₫`; (8) đủ khung kế hoạch đòi (`CASES`: mọi phần Interfaces liệt kê, ca WB04, WB05, dữ liệu khó); (9) tiền nằm trọn trong ô; thêm cho M5: (10) khóa dịch vụ hiện một lần nằm trong ô chỉ đọc có nút chép và lời nhắc, không ô nào khác chứa chuỗi khóa `ek_…_…` đầy đủ; (11) nội dung khách gửi có `<script>` hiện nguyên chữ (không có thẻ `script` nào trong trang ngoài `khung.js`); (12) `menu.html`: thứ tự nhóm menu đúng mục "Xếp module" (Đơn hàng, Vận chuyển, Tiền, Khách hàng, Khuyến mãi, Website, Sản phẩm, Kho, Hệ thống, Quản trị), không có mục "Tổng quan", "Đơn cần kiểm tra", "Kết nối Nhanh", "Flash sale" (đợt 2). Viết công cụ trước, chạy thấy đỏ (chưa có trang), rồi vẽ tới khi xanh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go. Mockup có sẵn: `docs/mockups/m1/` (token, khung, nút, ô nhập, bảng → thẻ dưới 760 px, hộp thoại → tấm trượt đáy, toast, chip, hộp báo, kpi, tab, vùng thả tệp, thanh tiến độ, ô ảnh `.imgs`/`.img-tile`/`.img-ph` có trạng thái đang xử lý/hỏng), `docs/mockups/m2/` (`.kv`, `.tl`, `.acts`/`.act` nút mờ kèm lý do, `.radios`, `.dlgs`, `.lbl` thanh nhãn có số, `.olist`/`.oitem` danh sách sắp xếp được, `.note-box`, `.split`, `.side-c`), `docs/mockups/m3/` (`soat.mjs`, `.nav-n` số đếm menu, `.selbar` thanh chọn nhiều, `.copy` ô sao chép, `.unit`/`.set-g` ô số có đơn vị, `.only-desk`/`.only-phone`; cách làm `index.html`, `README.md`, bảng HV).
- Dùng lại / viết mới vì: dùng lại nguyên `../m1/m1.css`, `../m2/m2.css`, `../m3/m3.css` bằng liên kết (không chép) để màn M5 cùng thành phần với console; `m5.css` chỉ thêm thành phần chưa có (dòng tin hai phía có nhãn "Khách thấy"/"Nội bộ", ô khóa hiện một lần, khung xem trước banner máy tính/điện thoại, ảnh bộ phối có chấm sản phẩm, thẻ banner trong danh sách), chỉ bằng biến của `m1.css`. `khung.js` viết bản M5 vì menu khác (nhóm Website, mục mới ở Đơn hàng, Khách hàng, Khuyến mãi, Quản trị); giữ đúng cấu trúc HTML của bản M3. `soat.mjs` chép từ M3 rồi đổi danh sách trang, cỡ chụp, thư mục ảnh `shots/`, soát thêm `m5.css`; bỏ kiểm ô khóa API của M3 (M5 không có ô nhập khóa hãng), thêm (10)–(12); không sửa bản M1–M4 để không đổi bằng chứng của Task 49, 83, 131, 141.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã. Các câu dưới làm căn cứ cho **nội dung** mockup (màn hình phải nói đúng luật API sẽ làm).

- 5.1–5.12: không áp dụng cho mã của task (không mã, bảng, route, thư viện mới trong repo; Playwright chạy từ bộ đệm npx của máy, không thêm phụ thuộc). 5.9 (chống trùng): mọi nút ghi (Gửi trả lời, Đổi trạng thái, Tạo khóa, Thu hồi, Lưu cài đặt quà, Tạo mã ngay, Đã nhắn, Lưu/Kích hoạt/Tạm dừng/Xóa chiến dịch, Lưu banner, Lưu bộ phối, Lưu góp ý, Tạm dừng/Mở lại nhận đơn) khóa khi đang gửi (một `Idempotency-Key` mỗi thao tác); tạo khóa dịch vụ là `no-replay`: gửi lại cùng khóa → 409 `already_processed`, màn báo "khóa đã tạo, không hiện lại được" (Task 208).
- 6.1 (hai người cùng làm): đổi trạng thái yêu cầu, thu hồi khóa, lưu cài đặt hãng trên web, cài đặt quà, nhận đơn web, góp ý gửi `version` → 409 → "vừa được người khác đổi — tải lại"; chiến dịch, banner, bộ phối gửi `revision` → 409 giữ nguyên bản đang sửa (Task 458, 459, 460); "Tạo mã ngay" khi lượt khác đang chạy → 409 `another_run_in_progress`.
- 6.2 (bấm hai lần): như 5.9.
- 6.3–6.11: không áp dụng: không có mã chạy. Nội dung liên quan: yêu cầu mới đến theo báo hiệu `order_request.changed` (danh sách tự làm mới, số đếm menu); ảnh khách gửi là ảnh riêng tư qua link ký có hạn 10 phút, trạng thái `processing`/`rejected` hiện riêng (Task 195, R10); ảnh banner qua kho tệp CRM, đang xử lý thì nút lưu khóa kèm lý do (Task 459); tạm dừng nhận đơn chỉ chặn đơn mới, đơn khách đã gửi trong hàng đợi của website vẫn vào (Task 456).
- 6.12 (dữ liệu cá nhân): nội dung tin, góp ý hiện dạng văn bản thuần (có `<script>` thì hiện nguyên chữ); nhật ký thao tác chỉ hiện tên trường đổi, không hiện giá trị (Task 457: audit không giữ giá trị); khóa dịch vụ chỉ hiện một lần lúc tạo, danh sách chỉ có tiền tố; SĐT ở danh sách sinh nhật hiện che giữa, đầy đủ chỉ trong nút "Mở Zalo" và tệp xuất CSV (xuất ghi audit có số dòng, Task 468); mockup dùng tên, SĐT, địa chỉ giả.
- 6.13 (quyền): nút theo quyền trong `/api/v1/me`: `order.requests.read` / `order.requests.manage` (195), `identity.service_keys.manage` (189), `fulfillment.carrier_accounts.read` / `.manage` (M3, 198), `promotion.read` / `promotion.manage` (M2, 452), `promotion.birthday.manage` (468), `content.read` / `content.manage` (453), `customer.feedback.read` / `.manage` (455), `order.read` / `order.web_intake.manage` (456), `audit.read` (M0, 457). Người chỉ có quyền đọc thấy màn không có nút ghi, kèm câu lý do; không gì chỉ hiện khi rê chuột. Kiểm thật vẫn ở API.

## Bằng chứng

Lệnh tự soát (gọi tắt `SOAT`): `PW_DIR=~/.npm/_npx/5e2e484947874241/node_modules/playwright node docs/mockups/m5/soat.mjs` (Playwright của bộ đệm npx, như Task 49, 83, 131, 141).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Công cụ soát chạy đỏ trước khi có trang | `soat.mjs` | `SOAT --no-shot` (lúc chưa có trang) | `m5.css: chưa có`, `order-requests: chưa có order-requests.html`, … (11 màn + `index`), `SOÁT MOCKUP: 13 lỗi`, exit 1 |
| Công cụ bắt đúng lỗi mới của M5 (kiểm chính công cụ) | sửa tạm: chép khóa đầy đủ vào cột Tiền tố, bỏ `readonly` và đổi `autocomplete="on"` ở ô `.once`, bỏ khóa `xac-nhan-thu-hoi`; bỏ `<script>` khỏi dòng góp ý `xss-text`; `khung.js` thêm mục "Tổng quan" và đổi chỗ nhóm Website/Sản phẩm | `SOAT --no-shot service-keys feedback menu` | `khóa dịch vụ đầy đủ nằm ngoài ô hiện một lần`, `ô khóa hiện một lần không phải ô chỉ đọc`, `… thiếu autocomplete="off"`, `thiếu trạng thái "xac-nhan-thu-hoi"`, `[xss-text] không hiện nguyên chữ "<script>"`, `menu còn mục "Tổng quan"`, `thứ tự nhóm menu sai: … Sản phẩm · Website …`; `SOÁT MOCKUP: 27 lỗi`, exit 1. Chép lại bản đúng → `SOÁT MOCKUP OK: 3 trang, 9 ảnh` |
| Công cụ bắt lỗi thật khi vẽ | `soat.mjs` | `SOAT web-campaigns`, `SOAT web-banners`, `SOAT menu` | `chữ trong ô chọn bị cắt "20 dòng mỗi trang"`, `"Tất cả trạng thái"` ở 360/390 → rút chữ lựa chọn; `"Xả hàng thu đông (Đang áp dụng)"` ở 360 → dời trạng thái xuống dòng gợi ý; menu điện thoại `mục chạm thấp 40px < 44px: a.nav-a` (78 lỗi) và nút "Mở" `mục chạm hẹp 20px` (20 lỗi) → `.drawer-nav .nav-a` cao 44 px, nút `.btn` |
| 11 màn của kế hoạch (đợt 1, không có `flash-sales.html`) + `index.html`, chụp 1512×767, 390×844, 360×800 | `soat.mjs` | `SOAT` | `SOÁT MOCKUP OK: 12 trang, 35 ảnh` (lần chạy cuối) |
| Mọi phần Interfaces của Task 205 (và dòng "(Task 450)"), 207–209, 458–462 có khung; dữ liệu khó (tên 60/80/120/28 ký tự, 0 / 1 / nhiều dòng, 409, 403, chỉ xem, lỗi tải) | `soat.mjs` kiểm (8), `CASES` 248 khóa | `SOAT` | 0 lỗi "thiếu trạng thái"; độ dài đo bằng `python3 len()`: tên khách 60, tên khóa 80 (`WebsiteELIMAZBanChinh…2026`), tên chiến dịch 120, tên dịp 28 (`ThứHaiĐầuTuầnMặcGìĐểTựTinNhé`) |
| Ca V của spec: WB04, WB05 | `soat.mjs` kiểm (8) khóa `wb04`, `wb05` | `SOAT` | có khung (Yêu cầu A; Quà sinh nhật D); FS01 thuộc đợt 2, không vẽ |
| Không cuộn ngang, không tràn, mục chạm đủ, chỉ token có sẵn, ô chọn đúng, không gì chỉ hiện khi rê chuột (soát `m1.css`, `m2.css`, `m3.css`, `m5.css`), tiền `690.000 ₫` nằm trọn ô, khóa hiện một lần đúng, `<script>` hiện nguyên chữ, thứ tự menu | `soat.mjs` (1)–(12) | `SOAT` | 0 lỗi lần chạy cuối |
| Ảnh admin hiện tại để so | build `apps/admin` origin/main `7199466` (bản `git archive` trong scratchpad, `node_modules` đọc từ `apps/storefront-bct`), máy chủ giả trả dữ liệu mẫu cho mọi đường GET, cổng 4790/4791 (không dùng 4175/4176/4180/4181/4188), Playwright chụp | — | 29 ảnh `shots/hien-tai-*.png` (Tổng quan, Chiến dịch + sửa, Banner, Bộ phối, Sinh nhật 3 tab, Yêu cầu + chi tiết, Góp ý, Vận hành website đang dừng / đang mở, Nhật ký, menu điện thoại); mở xem: đều là màn admin có dữ liệu, không có băng lỗi; hai máy chủ dừng bằng đúng PID |
| Soát bằng mắt ảnh 1512, 390 | đọc ảnh | — | Đã sửa: số đếm "Đã nhận hàng gửi về 0" trong khi bảng có YC0000139 (→ 1, Đang mở 8, menu 8); dòng banner: ô Bật và ↑↓ xuống dòng lệch nhau (→ chung một hàng); câu giải thích của mockup nằm trong hộp lỗi Hãng trên web (→ ghi chú `.mk`); menu máy tính bị cắt ở 767 px trong ảnh `menu` (→ cột menu không dính trong trang menu); dòng Đã hết hạn thiếu nút Sửa như admin; Bộ phối bỏ "Xem thêm" (lưu là lưu cả danh sách nên tải hết) |
| Nén ảnh | `nen-anh.py` (Pillow, 256 màu) | `python3 docs/mockups/m5/nen-anh.py` | `shots/: 22.6 MB -> 6.5 MB`, 64 tệp; xem lại ảnh nén (`web-looks-1512`) chữ, viền rõ |
| Bảng hành vi, mỗi hành vi một dòng để chủ shop OK riêng | `README.md`, `index.html` | `grep -c '^| HV' docs/mockups/m5/README.md` | `25` (HV01–HV25, cột OK = "chưa") |
| Gộp `main` trước khi stage | — | `git merge main` | `Already up to date.` (main `d5cb112`) |
| Cổng kiểm của task một lần trước khi stage | `ci-task.sh` | `ci-task.sh` của task | `CI-TASK XANH (so với điểm tách nhánh d5cb112…)`: catalog-check, jscpd `Found 0 clones`, gitleaks `no leaks found`, oasdiff, test-guard, `ok elimaz.com.vn/crm/tests/arch`; không có gói Go nào đổi nên không chạy test gói |
| Chỉ đổi tài liệu, không chạm `CODE_PATHS`, không sửa `docs/mockups/m1/`–`m4/` | — | `git diff --cached --name-only` | chỉ `docs/mockups/m5/**` và `docs/tasks/task-205.md` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm tệp ngoài danh sách của kế hoạch: `README.md`, `m5.css`, `khung.js`, `soat.mjs` (chụp và tự soát), `nen-anh.py` (nén ảnh), ảnh admin `shots/hien-tai-*.png`. Đều trong `docs/mockups/m5/`, không phải mã CRM.
- Không có `flash-sales.html` (đợt 2, Task 450) dù danh sách Files của kế hoạch có.
- Ảnh thêm cỡ 360 cho mọi màn (kế hoạch: 1512 và 390): luật mockup "360 khi chữ có thể tràn"; mọi màn có tên dài.
- Màn Hãng trên website vẽ thành trang riêng `web-carriers.html` có ghi chú là phần cuối của màn Kết nối vận chuyển (M3); bảng tài khoản phía trên không vẽ lại.
- Commit theo kế hoạch ghi `docs(crm): mockup năm màn M5 đã chốt`; task này chưa được chốt (Điểm dừng B là việc của điều phối viên), nên commit ghi "chờ chốt" và bảng "Kết luận duyệt" để trống, như Task 131.
- Kế hoạch không đòi reviewer mã cho task mockup; vẫn chạy agent `reviewer` theo yêu cầu của phiên. Agent `design-critic` không có trong phiên này; soát bằng `soat.mjs` và soát mắt.

## Quyết định thay chủ shop

- **Chụp admin thật cho cột "Hiện tại"** bằng bản build riêng của `origin/main` và máy chủ giả có dữ liệu mẫu (không mở admin của chủ shop ở 4176, không gọi website thật, không cài gói từ mạng: dùng `node_modules` có sẵn của `apps/storefront-bct`, next 16.4.6 thay 16.4.2). Phương án khác: chỉ mô tả bằng chữ như M3. Đảo ngược: xóa `shots/hien-tai-*`.
- **Đường dẫn màn** (`/don/yeu-cau`, `/khuyen-mai/chien-dich`, `/khuyen-mai/qua-sinh-nhat`, `/khach/gop-y`, `/website/banner`, `/website/bo-phoi`, `/website/nhan-don`, `/website/khoa-dich-vu`, `/quan-tri/nhat-ky`) theo cách đặt tên tiếng Việt của console; kế hoạch chưa ghi. Task màn hình chốt đường thật.
- **Hãng trên web: bật/tắt và thứ tự lưu ngay từng tài khoản, chọn sẵn và phí dự phòng có nút Lưu riêng** (HV09): hai nhóm dữ liệu đi qua hai operation khác nhau (`updateCarrierAccount` có `version` từng tài khoản; `saveWebCarrierSettings`), một nút Lưu chung có thể lưu một nửa. Ẩn hãng đang chọn sẵn thì máy chủ tự bỏ chọn sẵn cùng transaction (Task 198 "Bổ sung Task 450", như `toggleCarrier` của admin), màn báo "đã bỏ chọn sẵn"; ẩn hãng cuối cùng bị chặn (máy chủ 422 `last_web_carrier`). Phương án khác: một nút Lưu gọi nhiều operation và báo phần lỗi. Cần chủ shop OK: HV09.
- **Hãng trên web chỉ liệt kê tài khoản API** (không liệt kê tài khoản nhập mã tay, tự giao): `QuoteForWeb` bỏ tài khoản không báo được phí. Xem Việc phát hiện thêm 3.
- **Số "vận đơn trong 30 ngày" mỗi tài khoản** (HV12): theo Task 198 "Bổ sung Task 450" (lấy từ `fulfillment.shipments_v`); đếm vận đơn CRM thay đơn Nhanh. Operation trả số này ghi ở Việc phát hiện thêm 10.
- **Yêu cầu: tin nhân viên chọn "Khách thấy" / "Ghi chú nội bộ"** (HV04): Interfaces Task 205 đòi; `Reply(…, visible bool, …)` của Task 195.
- **Yêu cầu hủy đơn đã gửi hãng: nút "Mở đơn để hủy vận đơn"**, không hủy ngay trên màn yêu cầu (HV07): hủy vận đơn là luồng của M3 (`cancelShipment`, lý do, hãng xác nhận); màn yêu cầu không gọi API của module khác.
- **Bộ phối có ô "Câu mở đầu", "Câu chuyện"** (HV20): bảng `content.looks` của Task 454 có `headline ≤ 90`, `story ≤ 220`; website đã đọc hai trường này (`server/looks/rules.ts`), admin chưa có ô.
- **Nhật ký: lọc người và khoảng ngày, không có ô tìm chữ** (HV24): `listAuditLogs` của Task 457 chỉ có `action`, `actorId`, `entityType`, `entityId`, `from`, `to`.
- **Mức hài lòng góp ý hiện bằng chữ** (HV22): mặt cười là ký tự màu ngoài bộ token.

## Việc phát hiện thêm

Không tự sửa kế hoạch hay hợp đồng; ghi để task sau xử lý.

1. **Số đếm yêu cầu đang mở trên menu** (thay Tổng quan, QĐ37): Task 195 không có operation đếm; Task 207 thêm (ví dụ `counts` trong `listOrderRequests`) hoặc đếm từ danh sách lọc "Đang mở".
2. **Biến thể muốn đổi sang kèm số còn hàng** trên chi tiết yêu cầu: `RequestLine` của Task 195 chỉ có `VariantID, Qty, Reason`; "Bổ sung Task 450" thêm `want_variant_id` nhưng `getOrderRequest` chưa ghi trả tên, số còn. Task 195/207 thêm hoặc bỏ số còn.
3. **Tài khoản nhập mã tay, tự giao có được bật `show_on_web`?** Kế hoạch Task 198 không nói; mockup M3 (`ket-noi-van-chuyen.html`) vẽ "Shop tự giao · Hiện trên web: Có · thứ tự 3", trong khi `QuoteForWeb` bỏ tài khoản không báo được phí. Task 198/208 thống nhất (mockup M5 không liệt kê hai loại này).
4. **Trần phần trăm chiến dịch** (`max_percent`, mặc định 50): Task 452 Step 0 chọn chỗ lưu nhưng không có màn sửa; câu lỗi mockup "nhờ Chủ nâng trần". Task 452/458 chọn: màn cài đặt khuyến mãi hoặc chỉ sửa bằng lệnh.
5. **Nhật ký: số dòng xuất CSV, số mã của lượt chạy** — Task 468 ghi audit "có số dòng" nhưng `listAuditLogs` (Task 457) chỉ trả tên trường; mockup hiện "117 dòng" ở dòng xuất CSV. Task 457 trả thêm phần tóm tắt không có dữ liệu cá nhân, hoặc bỏ.
6. **Quà sinh nhật "Khách đã lưu sinh nhật" và "Sắp gửi · 7 ngày tới"** (4 ô số đầu trang như admin): Task 468 chưa có operation tổng hợp; Task 209 thêm hoặc bỏ ô.
7. **Tạo khóa dịch vụ: tên quyền tiếng Việt** (Đọc danh mục, Tạo đơn web…) cần bảng nhãn ở console; `docs/quyen.md` là nơi một nguồn.
8. **Danh sách góp ý: số đếm theo trạng thái** (thanh lọc có số như admin): `listFeedback` (Task 455) chưa trả `counts`.
9. **Ảnh đã nén 256 màu** chỉ để xem; khi chốt nếu cần ảnh gốc thì chạy `soat.mjs` không chạy `nen-anh.py`.
10. **Số vận đơn 30 ngày mỗi tài khoản hãng** (Task 198 "Bổ sung Task 450" nói hiện ở M5-25 nhưng `listCarrierAccounts`, `getWebCarrierSettings` chưa có trường): Task 198/208 thêm trường vào một operation đọc.
11. **Số đếm theo từng trạng thái ở thanh lọc Yêu cầu** (10 nhãn có số, HV03): `ListRequests` (Task 195) chỉ trả danh sách và con trỏ; Task 195/207 trả `counts` theo trạng thái (như mục 8 của góp ý) hoặc bỏ số khỏi nhãn (mục 1 chỉ nói số trên menu).
12. **Khối "Lịch sử" của chi tiết yêu cầu** (ai đổi trạng thái, trả lời, lúc nào): `RequestDetail` (Task 195) chưa ghi trả lịch sử; Task 195/207 trả từ audit hoặc tin, hoặc bỏ khối.
13. **Cảnh báo "Banner số 3 đang dùng chiến dịch này" khi xóa chiến dịch**: màn chiến dịch cần đọc banner của `content`; Task 458 chỉ đòi cảnh báo chung "banner dùng chiến dịch này sẽ ẩn". Task 452/458 chọn: thêm số banner dùng chiến dịch vào `getWebCampaign`, hoặc câu chung.
14. **Đầu trang Quà sinh nhật**: ô "Mã đã dùng · năm 2026", câu "n khách đến hạn đang chờ đơn giao thành công" của lượt chạy gần nhất (cùng mục 6) chưa có operation trả.

## Reviewer

Lượt 1: **CẦN SỬA**, bốn phát hiện: (1) màn chặn ẩn hãng đang chọn sẵn, trái Task 198 "Bổ sung Task 450" (máy chủ tự bỏ ưu tiên cùng transaction) và admin (`toggleCarrier` tự bỏ tích) — đã vẽ lại: ẩn được, hộp báo "đã bỏ chọn sẵn", sửa câu ở Quyết định thay chủ shop; (2) bỏ "số đơn 30 ngày mỗi hãng" trái Task 198 "Bổ sung Task 450" — đã vẽ "n vận đơn trong 30 ngày", HV12 đổi nội dung, Việc phát hiện thêm 10; (3) khóa dịch vụ vẽ tên người tạo/thu hồi mà `listServiceKeys` không trả — đã bỏ tên; (4) số đếm theo trạng thái ở thanh lọc yêu cầu chưa có trong `ListRequests` — ghi Việc phát hiện thêm 11. Ghi chú không bắt buộc đã làm: hộp vượt trần 60% không gắn chiến dịch 10%; banner 2 trỏ "Trang Ưu đãi" thay chiến dịch không có trong danh sách; dòng nhật ký "Tạo khóa dịch vụ 08:14" khớp khóa tạo 08:14 21/11; bộ phối 1 ghi đủ 3 món; Đỗ Hải Yến sinh 28/11 (gửi mã hôm nay đúng luật 7 ngày); Việc phát hiện thêm 12 (lịch sử yêu cầu), 13 (cảnh báo banner khi xóa chiến dịch), 14 (ô số sinh nhật). Chụp lại, `SOAT` xanh.

Lượt 2: **ĐẠT**. Bốn phát hiện lượt 1 sửa đúng (ẩn hãng chọn sẵn tự bỏ chọn sẵn, hãng cuối cùng bị chặn 422 `last_web_carrier`; số vận đơn 30 ngày nhất quán A/B, HV12 đổi; khóa dịch vụ chỉ dùng trường `listServiceKeys` trả; counts yêu cầu ở Việc phát hiện thêm 11); không phát hiện mới. Reviewer không tự chạy được `soat.mjs` (hook chỉ cho lệnh đọc), đối chiếu mã nguồn công cụ.

Kết luận: ĐẠT
