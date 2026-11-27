# Task 514 — Mockup màn Đợt flash sale (đợt 2 của M5-22)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 205 = M5-22, phần đợt 2: màn **Đợt flash sale**; màn sẽ code ở Task 206 = M5-23). Số do điều phối M5 cấp (27/11); chủ shop 27/11 sáng OK đợt 2 flash sale phía CRM. Loại việc (quy trình mục 3): A, chỉ đổi tài liệu (không có tệp nào trong `CODE_PATHS`).

Căn cứ: kế hoạch M5 «Task 205» (Interfaces, dòng **Đợt flash sale**), «Task 206» (Interfaces, Step 1), task nền «Task 199» (bảng, giới hạn, lỗi `not_a_discount`, `not_editable`, operation soạn), «Task 200» (lên lịch giữ suất, `insufficient_stock` liệt kê mọi món, `overlap`, thêm suất, đóng đợt, `flashSaleLive`, báo hiệu `flash_sale.changed`), «Task 202» (nhả suất khi đợt mở / đã đóng), QĐ6, QĐ8, QĐ24, QĐ31; spec `11-kich-ban-kiem-thu.md` FS01 (V), FS08, FS17; mockup đợt 1 `docs/mockups/m5/` và `docs/tasks/task-205.md`; hệ thiết kế CRM M1 (`docs/mockups/m1/m1.css`) và thành phần M2, M3, M5 đợt 1.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (flash sale chưa có trên `main`: `grep -i flash openapi/openapi.yaml` chỉ ra một câu mô tả của Task 191). Mockup chỉ vẽ dữ liệu của operation kế hoạch khai cho Task 199, 200: `listFlashSales` (`?status=`), `getFlashSale`, `createFlashSale`, `updateFlashSale`, `addFlashItem`, `updateFlashItem`, `removeFlashItem`, `scheduleFlashSale` (thân `{version}`), `addFlashSlots` (thân `{add}`), `closeFlashSale`, `flashSaleLive` (`Quantity, Available, Held, Sold, Released, Buyers` mỗi món); tìm biến thể bằng `searchVariants` có sẵn (M1). Lỗi: `insufficient_stock` (`{variantId, sku, need, sellable}` mọi món thiếu), `overlap`, `not_a_discount`, `not_editable`, `not_for_sale`, `version_conflict`, 422 kiểm trường (giờ đóng trước giờ mở, đợt dài hơn 7 ngày, biến thể đã có trong đợt). Chỗ mockup cần trường chưa có ghi ở "Việc phát hiện thêm".
- Chỗ dùng trên màn hình: console chưa có màn flash sale. Mockup là đầu vào của Task 206 (`console/src/screens/flash-sales/*`, đường `/khuyen-mai/flash-sale`, `/khuyen-mai/flash-sale/:id`). Menu: thêm mục "Flash sale" vào nhóm Khuyến mãi của `khung.js` (mockup), Task 206 sửa menu console thật.
- Câu sqlc, bảng, pha migration: không.
- Tệp sẽ đổi (chỉ `docs/`): tạo `docs/mockups/m5/flash-sales.html`, ảnh `shots/flash-sales-{1512,390,360}.png`; sửa `docs/mockups/m5/khung.js` (mục Flash sale), `soat.mjs` (thêm trang, khóa `CASES`, bỏ "Flash sale" khỏi danh sách mục cấm của menu), `m5.css` (chỉ thêm thành phần mới nếu cần, bằng biến có sẵn), `menu.html` (câu "Flash sale là đợt 2" → có mục), `index.html` và `README.md` (màn (12), ca FS01, hành vi HV26+), chụp lại `shots/menu-*.png`, `shots/index-*.png`; `docs/tasks/task-514.md`; thêm một dòng vào `../../docs/superpowers/plans/danh-so-task-toan-cuc.md` (ngoài repo `apps/crm`). Không sửa `docs/mockups/m1/`–`m4/`.
- Test sẽ chạy: không có test mã. "Test" là `docs/mockups/m5/soat.mjs` (Playwright Chromium trong bộ đệm npx, như Task 205; chụp tệp HTML cục bộ, không phải e2e): kiểm (1)–(12) của Task 205 cho `flash-sales` ở 1512 × 767, 390 × 844, 360 × 800, cộng `menu` và `index`; (8) `CASES` của `flash-sales` đủ mọi trạng thái Task 205/206 đòi. Viết khóa `CASES` trước, chạy thấy đỏ (chưa có trang), rồi vẽ tới khi xanh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go. Thành phần mockup có sẵn: M1 (`.tbl` → thẻ dưới 760 px, `.dlg`/`.dlgs`, `.box-*`, `.chip-*`, `.kpi`, `.tabs`, `.field`, `.unit`, `.toast`, `.empty`), M2 (`.kv`, `.acts`, `.reason`), M3 (`.selbar`, `.only-desk`/`.only-phone`, `.nav-n`), M5 đợt 1 (`khung.js`, `soat.mjs`, `nen-anh.py`, `.st` khung trạng thái, cách làm `index.html`/`README.md`, bảng HV).
- Dùng lại / viết mới vì: dùng lại nguyên các tệp CSS M1–M3 và `m5.css` bằng liên kết; dùng lại `khung.js`, `soat.mjs`, `nen-anh.py` của Task 205 (sửa tại chỗ, không chép bản mới) vì cùng một bộ mockup M5. Chỉ viết mới CSS cho thanh số trực tiếp của một món (thanh chia phần đã bán / đang giữ / còn trống) nếu thành phần có sẵn không đủ, đặt trong `m5.css`, chỉ bằng biến của `m1.css`.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã. Các câu dưới làm căn cứ cho **nội dung** mockup (màn phải nói đúng luật mà API của Task 199–202 sẽ làm).

- 5.1–5.12: không áp dụng cho mã của task (không mã, bảng, route, thư viện mới trong repo; Playwright chạy từ bộ đệm npx). 5.9 (chống trùng): mọi nút ghi (Tạo đợt, Lưu, Thêm món, Sửa món, Bỏ món, Lên lịch, Thêm suất, Đóng sớm) khóa khi đang gửi; một `Idempotency-Key` mỗi lần bấm, giữ nguyên khi tự gửi lại sau 503 (`Retry-After`) — vẽ trạng thái "đang gửi lại".
- 6.1 (hai người cùng làm): đợt và món gửi `version` → 409 `version_conflict` → "Đợt vừa được người khác sửa, tải lại" (Task 206); lên lịch khi người khác vừa thêm món cũng là 409.
- 6.2 (bấm hai lần): như 5.9.
- 6.3 (tiền): giá flash là `int64` đồng, hiện `199.000 ₫`; giá flash phải thấp hơn giá bán thường (QĐ24) → lỗi `not_a_discount` ngay ô giá.
- 6.4 (tồn, khóa): lên lịch trừ "có thể bán" ngay lúc bấm (QĐ8, bút toán `flash_hold`), không phải lúc mở — hộp xác nhận nêu số suất bị giữ khỏi bán thường mỗi món và "có thể bán" còn lại; thiếu hàng thì không giữ món nào (kiểm hết rồi mới ghi), bảng liệt kê **mọi** món thiếu (mã hàng, cần, có thể bán). Đóng đợt: suất còn trống trả về tồn bán thường ngay; suất khách đang giữ vẫn hoàn tất được trong hạn, hết hạn thì về tồn bán thường (FS17, Task 202).
- 6.5–6.11: không áp dụng: không có mã chạy. Nội dung liên quan: số trực tiếp tự làm mới khi có báo hiệu `flash_sale.changed`, mỗi 5 s khi đợt đang mở, 20 s ở trạng thái khác (Task 206); giờ mở/đóng tự động do nhịp 15 s, nhưng lượt mua chỉ được nhận đúng trong khung giờ (QĐ8) — màn ghi "tự mở lúc …".
- 6.12 (dữ liệu cá nhân): màn không hiện SĐT, tên khách mua; chỉ số người mua (`Buyers`).
- 6.13 (quyền): `promotion.flash.read` xem, `promotion.flash.manage` ghi (Task 199). Người chỉ có quyền đọc thấy màn không có nút ghi, kèm câu lý do; không gì chỉ hiện khi rê chuột. Kiểm thật vẫn ở API.

## Ca kiểm

| Mã ca (11-kich-ban-kiem-thu.md) | Test | Kết quả |
|---|---|---|
| FS01 (V) | `soat.mjs` kiểm (8): khóa `fs01` (trạng thái B) và các khóa soạn, thêm món, lên lịch | xem Bằng chứng |
| FS08, FS17 (phần màn hình) | `soat.mjs` kiểm (8): khóa `fs08` (trạng thái G), `fs17` (hộp Đóng sớm) | xem Bằng chứng |

## Bằng chứng

Lệnh tự soát (gọi tắt `SOAT`): `PW_DIR=~/.npm/_npx/5e2e484947874241/node_modules/playwright node docs/mockups/m5/soat.mjs` (Playwright của bộ đệm npx, như Task 205; chụp tệp HTML cục bộ, không phải e2e).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Công cụ soát đỏ trước khi có trang | `soat.mjs` (thêm `flash-sales` vào `PAGES`, 52 khóa `CASES`, khóa `flash-sale` của menu) | `SOAT --no-shot flash-sales menu` | `flash-sales: chưa có flash-sales.html`, `menu @1512/@390/@360: thiếu trạng thái "flash-sale"`, `SOÁT MOCKUP: 4 lỗi` |
| Màn Đợt flash sale đủ trạng thái Task 205/206 đòi (danh sách đợt, soạn nháp, thêm món, lên lịch, đang mở có số trực tiếp, thêm suất, đóng đợt, thiếu hàng liệt kê mọi món, 409 tải lại, 503 tự gửi lại, rỗng, nhiều món, tên dài, chỉ xem) | `soat.mjs` kiểm (8) | `SOAT flash-sales` | `SOÁT MOCKUP OK: 1 trang, 3 ảnh` (52 khóa, 0 "thiếu trạng thái") |
| Không cuộn ngang, không tràn, mục chạm đủ, chỉ token có sẵn (cả `.fs-bar`, `.fs-key` mới trong `m5.css`), ô chọn đúng, không gì chỉ hiện khi rê chuột, tiền `199.000 ₫` trọn ô — 1512, 390, 360 | `soat.mjs` (1)–(12) | `SOAT flash-sales menu index` | `SOÁT MOCKUP OK: 3 trang, 8 ảnh` |
| Mục menu mới không làm hỏng trang đợt 1 | `soat.mjs` mọi trang | `SOAT --no-shot` | `SOÁT MOCKUP OK: 13 trang, 38 ảnh` |
| Tên đợt 120 ký tự | `python3 len()` | — | `120` ("Flash sale mừng sinh nhật ELIMAZ sáu tuổi: … đồng giá từ 199k!"); 2 dòng (`clamp2`) ở danh sách, xuống dòng ở tiêu đề trạng thái E, không tràn ở 360 |
| Soát bằng mắt ảnh 1512, 390, 360 | đọc ảnh cắt khúc | — | Đã sửa: bảng trong hộp Lên lịch bị cắt cột phải ở 1512 (khung bảng `overflow: hidden` nên công cụ không bắt) → đổi thành danh sách; 6 ô số trực tiếp xếp một cột trên điện thoại → 2 cột; số liệu khớp nhau (suất 1.200 = 300+400+300+200, còn trống 214, đang giữ 38, đã bán 948; người mua mỗi món = giữ + bán khi giới hạn 1; tổng suất nháp 840, sau khi giảm hai món thiếu 758) |
| Nén ảnh | `nen-anh.py` (chỉ nén ảnh chưa ở bảng màu) | `python3 docs/mockups/m5/nen-anh.py` | `shots/: 12.9 MB -> 7.6 MB` (lần chụp đầu); sau sửa review chụp lại `flash-sales`, `index` rồi nén lại |
| Sau sửa review lượt 1 | `soat.mjs` | `SOAT flash-sales index` | `SOÁT MOCKUP OK: 2 trang, 5 ảnh` |
| Gộp `main` trước khi stage | — | `git merge main` | `Already up to date.` (main `e47ee485`) |
| Chỉ đổi tài liệu, không chạm `CODE_PATHS`, không sửa `docs/mockups/m1/`–`m4/` | — | `git diff --cached --name-only` | chỉ `docs/mockups/m5/**` và `docs/tasks/task-514.md` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch đặt `flash-sales.html` trong Task 205; làm ở task riêng 514 (đợt 2, số do điều phối M5 cấp). Commit theo yêu cầu phiên: `docs(crm): mockup màn Đợt flash sale (đợt 2 của M5-22)`; bảng "Kết luận duyệt" của README thêm dòng (12) "Chờ điều phối viên duyệt".
- Ảnh thêm cỡ 360 (luật mockup "360 khi chữ có thể tràn": tên đợt 120 ký tự, tên món dài).
- Agent `design-critic` không có trong phiên (thư mục agent của dự án chỉ có `reviewer`); soát bằng `soat.mjs`, soát mắt và agent `reviewer`.
- Không có ảnh "Hiện tại": admin không có flash sale.

## Quyết định thay chủ shop

- **Thêm mục "Flash sale" vào menu mockup** (`khung.js`, nhóm Khuyến mãi, sau Quà sinh nhật) và bỏ "Flash sale" khỏi danh sách mục cấm của `soat.mjs`: Task 206 thêm mục menu thật. **Chỉ chụp lại** `flash-sales`, `menu`, `index`; ảnh 10 màn đợt 1 giữ nguyên (menu trong ảnh chưa có mục Flash sale) để không đổi bằng chứng Task 205 và không thêm ~6 MB ảnh. Phương án khác: chụp lại mọi màn. Đảo ngược: chạy `SOAT` không tham số rồi `nen-anh.py`.
- **Thanh lọc trạng thái không có số đếm**: `listFlashSales` chỉ nhận `?status=` (không vẽ trường API không có). Việc phát hiện thêm 2.
- **"Người mua" ở đầu trang là tổng theo món** (ghi rõ "cộng theo món"): `flashSaleLive` chỉ trả `Buyers` mỗi món. Phương án khác: bỏ ô tổng.
- **Đợt nháp không có nút Xóa**: kế hoạch không có operation xóa/hủy đợt nháp (`closeFlashSale` chỉ cho `scheduled`/`open`). Việc phát hiện thêm 1.
- **Đã lên lịch nhưng chưa mở cũng có Đóng sớm** (Task 200: `CloseFlashSale` nhận `scheduled`/`open`) — đường duy nhất để "hủy lịch"; câu lý do ở trạng thái E nói vậy.
- **Lỗi thêm suất khi thiếu hàng** hiện ngay dưới ô (một món), cùng nội dung `insufficient_stock` (cần, có thể bán) như lên lịch.
- **Đang mở, món hết suất** hiện "Hết suất lúc này: còn n suất đang giữ, khách nào không hoàn tất trong hạn thì suất mở lại" (nghĩa hết suất của FS07).
- **Danh sách hành vi** HV26–HV31 ghi vào README/index, cột OK = "chưa"; theo Task 205 Step 2, Task 206 bắt đầu sau khi điều phối viên chốt hình, không chờ các dòng này (đều là QĐ đã có trong kế hoạch).

## Hành vi so với admin

Flash sale là **mới**, admin (`apps/admin`) không có màn hay luồng nào tương ứng. Danh sách hành vi để chủ shop xem (README.md, index.html, cột "Chủ shop OK" = chưa): HV26 mục menu Flash sale ở Khuyến mãi; HV27 Lên lịch giữ suất khỏi bán thường ngay lúc bấm (QĐ8, QĐ31); HV28 thiếu hàng không giữ món nào, liệt kê mọi mã thiếu, không hai đợt chồng giờ cùng biến thể; HV29 sau lên lịch chỉ Thêm suất, Đóng sớm, đã đóng chỉ xem; HV30 đóng sớm trả suất trống về bán thường, khách đang giữ vẫn hoàn tất trong hạn (FS17); HV31 mặc định món (mỗi khách 1, giữ 10 phút, giá flash thấp hơn giá thường, một lượt mua một đơn riêng, không mã giảm giá; QĐ6, QĐ24).

## Việc phát hiện thêm

Không tự sửa kế hoạch hay hợp đồng; ghi để Task 199, 200, 206 xử lý.

1. **Không có thao tác xóa/hủy đợt nháp**: operation của Task 199 chỉ có tạo, sửa, thêm/sửa/bỏ món; `closeFlashSale` chỉ cho `scheduled`/`open`. Đợt nháp tạo nhầm nằm mãi trong danh sách. Task 199 thêm `deleteFlashSale` (chỉ `draft`) hoặc cho `closeFlashSale` nhận `draft`.
2. **`listFlashSales`**: chưa ghi phân trang và tổng số đợt (Task 206 test "30 đợt"; mockup "Hiện 5 / 30 đợt" — không có tổng thì chỉ hiện "Xem thêm" khi còn con trỏ) và chưa trả số theo trạng thái, số món, tổng suất, đã bán mỗi đợt (cột "Đã bán / suất"). Task 199/206 chọn trường trả về.
3. **"Có thể bán lúc này" mỗi món** (đợt nháp) và "bán thường còn a → b" ở hộp Lên lịch, Thêm suất: `getFlashSale` chưa ghi trả số có thể bán của biến thể. Task 199 thêm (đọc `inventory`) hoặc Task 206 bỏ (khi đó thiếu hàng chỉ thấy lúc bấm).
4. **Lỗi `overlap`**: kế hoạch nói "nêu đợt chồng" nhưng chưa ghi trường (mã biến thể, tên, giờ, id đợt chồng). Task 200 trả `{variantId, sku, saleId, saleName, startsAt, endsAt}`.
5. **Giá thường mỗi món** (cột "Giá thường", lỗi `not_a_discount` kèm giá thường) cần trong `getFlashSale`/lỗi 422; Task 199 ghi trường.
6. **Số khách khác nhau của cả đợt**: `flashSaleLive` chỉ có `Buyers` mỗi món; nếu chủ shop cần số người mua của đợt, Task 200 thêm.
7. **Số khách đang giữ, số đơn đã hoàn tất**: `flashSaleLive` trả `Held`, `Sold` là số **đơn vị** (một dòng đơn có thể giữ tới 10 đơn vị), nên màn ghi "suất đang giữ", "suất đã bán". Nếu cần số khách đang giữ hay số đơn, Task 200 thêm trường.
8. **Tìm biến thể khi thêm món**: `searchVariants` (`VariantSearchHit`) chỉ có `price` và `sellable` (đúng/sai), không có số có thể bán; mockup chỉ hiện "đang bán"/"ngừng bán". Số có thể bán của món đã thêm thuộc mục 3.

## Reviewer

Lượt 1: **CẦN SỬA**, hai phát hiện: (1) số đơn vị `Held`/`Sold` của `flashSaleLive` bị gọi là "khách đang giữ", "đơn đã hoàn tất" (ô số, dòng hết suất, hộp Đóng sớm) — đã đổi thành "suất đang giữ", "suất đã bán", thêm Việc phát hiện thêm 7; (2) kết quả tìm biến thể hiện "có thể bán 412" mà `searchVariants` không trả — đã đổi thành "đang bán", thêm Việc phát hiện thêm 8. Ghi chú không bắt buộc đã làm: "Mỗi lượt mua" (thay "đợt"), thêm FS08, FS17 vào bảng ca của `index.html`, bỏ "10–15 phút" ở hộp Đóng sớm. Chụp lại `flash-sales`, `index`; `SOAT flash-sales index` xanh.

Lượt 2: **ĐẠT** (hai reviewer chạy cùng phần đã stage, cả hai ĐẠT). Hai phát hiện lượt 1 sửa đúng; số liệu tính lại khớp (840, 758, 1.200/214/38/948, 908, 200); khoảng giá trị khớp CHECK Task 199; Đóng sớm cho `scheduled`/`open` khớp Task 200; không phát hiện mới. Ghi chú không bắt buộc đã làm: câu "còn n suất đang giữ" ở Quyết định thay chủ shop; Việc phát hiện thêm 2 thêm "tổng số đợt". Reviewer không tự chạy được `soat.mjs` (hook chỉ cho lệnh đọc).

Kết luận: ĐẠT
