# Task 247 — Mockup màn M7 (Điểm dừng B)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 247 = M7-02; màn sẽ code ở Task 273 = M7-28). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu (không có tệp nào trong `CODE_PATHS`).

Phạm vi đợt này (điều phối M7, 28/11): chỉ vẽ và chốt **màn 10 (Kiểm kho mở sổ)** và **màn 13 (Hàng hoàn của đơn cũ)**. Mười ba màn còn lại (1–9, 11, 12, 14, 15) vẽ cùng task màn của chúng ở đợt sau; `docs/khoi-dong/mockup-m7.md` ghi trạng thái "chưa vẽ" cho từng màn.

Căn cứ: kế hoạch M7 «Task 247» (bảng màn 10, 13; Step 1–5), «Task 260» (operation `createOpeningStocktake`, `postOpeningStocktake`, `checkOpeningStocktake`, `incrementStocktakeLine`, `createStocktakeCountImport`, `getStocktakeCountImport`, `createLegacyReturn`, `listLegacyReturns`; quyền `inventory.stocktake.opening`, `inventory.legacy_return.create`, `inventory.stocktake`), «Task 262» (mở sổ: chỉ kho trống, một phiên mỗi kho, `uncounted_lines`, `warehouse_not_empty`, `opening_exists`, `CheckOpening`), «Task 263» (cộng dồn `by ∈ [−1000, 1000]`, `ambiguous_code`, `negative_count`, `unknown_code`, nhập Excel `replace|add`, lỗi dòng `unknown_code|ambiguous_code|not_integer|negative|formula`, `import_already_applied`, tệp `.xlsx` ≤ 5 MiB), «Task 264» (hàng hoàn cũ, Q11: tham chiếu 3–64 ký tự, chuẩn hóa bỏ khoảng trắng + chữ hoa, `legacy_return_exists` kèm id cũ, dòng `sellable|damaged`, qty 1–1000, ghi chú ≤ 500), «Task 273» (Interfaces, Step 1), Q11, Q14; spec MO02, MO08; mockup M1 `docs/mockups/m1/kho-kiem-kho.html` (màn kiểm kho thường), M3 `docs/mockups/m3/kho.html#nhan-hoan` (nhận hàng hoàn của hãng); màn console hiện có `console/src/screens/inventory/Stocktake.tsx`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml`. Các operation M7 chưa có trên `main` (`grep -n "opening-stocktakes\|legacy-returns\|increments\|count-imports" openapi/openapi.yaml` → 0 dòng; hợp đồng do Task 260 thêm). Mockup chỉ vẽ dữ liệu kế hoạch khai cho Task 260–264, cộng operation M1 có sẵn: `startStocktake`/`getStocktake`/`recordStocktakeCount`/`cancelStocktake` (`/api/v1/inventory/stocktakes…`), `createUpload` (tệp, mục đích `inventory_count_import`), tìm biến thể M1. Chỗ mockup cần trường chưa có ghi ở "Việc phát hiện thêm".
- Chỗ dùng trên màn hình: console có `console/src/screens/inventory/Stocktake.tsx` (`/kho/kiem-kho`, `/kho/kiem-kho/:id`) và `console/src/screens/warehouse/ReturnReceiptPage.tsx` (nhận hàng hoàn của hãng). Mockup là đầu vào của Task 273 (`console/src/inventory/opening/*`, `console/src/inventory/legacy/*`). Không sửa console.
- Câu sqlc, bảng, pha migration: không.
- Tệp sẽ đổi (chỉ `docs/`): tạo `docs/mockups/m7/{README.md,index.html,khung.js,m7.css,kiem-kho-mo-so.html,hang-hoan-don-cu.html,soat.mjs,nen-anh.py}`, ảnh `docs/mockups/m7/shots/*.png`; tạo `docs/khoi-dong/mockup-m7.md`; `docs/tasks/task-247.md`. Không sửa `docs/mockups/m1/`–`m6/`.
- Test sẽ chạy: không có test mã. "Test" là `docs/mockups/m7/soat.mjs` (chép từ `docs/mockups/m5/soat.mjs`; Playwright Chromium trong bộ đệm npx, chụp tệp HTML cục bộ, không phải e2e, không phải console Playwright): kiểm (1)–(9), (11) của M5 (không cuộn ngang, không tràn, chữ không bị cắt thiếu dấu …, mục chạm ≥ 44 px dưới 1000 px, chỉ token có sẵn, ô chọn đúng, đủ trạng thái `CASES`, không thẻ script ngoài `khung.js`) ở 1512 × 767, 390 × 844, 360 × 800 cho cả hai màn; thêm (13) màn kiểm kho mở sổ: nút "Đăng" có `disabled` ở mọi trạng thái còn dòng chưa đếm. Viết khóa `CASES` trước, chạy thấy đỏ (chưa có trang), rồi vẽ tới khi xanh. Chạy một lần cuối (lời dặn phiên: Playwright để khi xong việc), trước đó soát bằng đọc mã HTML.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go. Thành phần mockup có sẵn: M1 (`m1.css`: `.scan`, `.last-scan`, `.stepper`, `.count-big`, `.line(s)`, `.savebar`, `.dlg`, `.box-*`, `.chip-*`, `.field`, `.tbl`, `.empty`, `.toast`, `.phone-col`), M2 (`m2.css`), M3 (`m3.css`: `.rline`, `.q3`, `.tl`, `.only-desk`/`.only-phone`), M5 (`khung.js`, `soat.mjs`, `nen-anh.py`, `.st` khung trạng thái, cách làm `index.html`/`README.md`, bảng HV).
- Dùng lại / viết mới vì: dùng lại nguyên `m1.css`, `m2.css`, `m3.css` bằng liên kết; `khung.js`, `soat.mjs`, `nen-anh.py` chép từ M5 sang `docs/mockups/m7/` (như M5 chép từ M3: mỗi mốc một bộ mockup, menu và danh sách trang riêng; sửa tệp M5 thì đổi bằng chứng của Task 205/514). `m7.css` chỉ thêm thành phần chưa có (thanh tiến độ đếm, hàng đợi quét khi mất mạng), chỉ bằng biến của `m1.css`. Ảnh "Hiện tại" dùng lại ảnh đã có `docs/mockups/m1/png/kho-kiem-kho-*.png`, `docs/mockups/m3/png/kho-*` (nếu có) bằng đường dẫn tương đối, không chép ảnh.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã. Các câu dưới làm căn cứ cho **nội dung** mockup (màn phải nói đúng luật mà API của Task 262–264 sẽ làm).

- 5.1–5.12: không áp dụng cho mã của task (không mã, bảng, route, thư viện mới trong repo; Playwright chạy từ bộ đệm npx). 5.9 (chống trùng): mỗi lần quét là một yêu cầu `+1` có `Idempotency-Key` riêng; mất mạng thì xếp hàng trong trình duyệt và gửi lại **cùng khóa** (Task 273) — vẽ trạng thái "chờ gửi". Tạo phiên, Đăng, Nhập tệp, Nhập kho hàng hoàn khóa nút khi đang gửi.
- 6.1 (hai người cùng làm): đếm cộng dồn (`incrementStocktakeLine`), nhiều người cùng đếm một phiên; số hiện là số máy chủ trả sau mỗi lần cộng, không phải số đếm trên máy này. Người khác đăng phiên giữa chừng → 409 `stocktake_not_open` → màn chuyển sang "Đã đăng".
- 6.2 (bấm hai lần): như 5.9; Đăng hai lần chỉ đăng một lần (Task 262), lần sau 409.
- 6.3 (tiền): không có tiền trên hai màn.
- 6.4 (tồn, khóa): mở sổ chỉ trên kho trống (`warehouse_not_empty` cả lúc tạo và lúc đăng); đăng đặt tồn = số đếm; sau đăng sai số sửa bằng phiếu điều chỉnh có lý do (M1) — màn ghi rõ. Hàng hoàn cũ: dòng "Hỏng" vào ngăn hỏng, không bán được (KH08).
- 6.5–6.11: không áp dụng: không có mã chạy. Nội dung liên quan: nhập Excel chạy ở worker (`queued → running → done|failed`), màn tự hỏi lại trạng thái; nhập lại cùng tệp chế độ "Đặt bằng số trong tệp" không đổi gì, chế độ "Cộng thêm" lần hai bị chặn (`import_already_applied`).
- 6.12 (dữ liệu cá nhân): không hiện dữ liệu khách. Hàng hoàn cũ chỉ có mã vận đơn/mã đơn cũ; ghi chú chỉ nằm trong phiếu (không vào nhật ký). Lỗi dòng của tệp chỉ có số dòng và mã lỗi, không hiện nội dung ô.
- 6.13 (quyền): tạo, đăng, xem kết quả kiểm mở sổ: `inventory.stocktake.opening` (theo kho); đếm, nhập tệp: `inventory.stocktake` (M1); hàng hoàn cũ: `inventory.legacy_return.create` (theo kho). Kho không có quyền không có trong ô chọn; vào thẳng bằng đường dẫn → 403 có câu lý do. Không gì chỉ hiện khi rê chuột.

## Ca kiểm

| Mã ca | Test | Kết quả |
|---|---|---|
| MO02 (phần màn hình) | `soat.mjs` kiểm (8): khóa `mo02` và các khóa trạng thái A–G của `kiem-kho-mo-so` | xem Bằng chứng |
| MO08 (phần màn hình) | `soat.mjs` kiểm (8): khóa `mo08` và các khóa trạng thái A–C của `hang-hoan-don-cu` | xem Bằng chứng |

## Bằng chứng

Lệnh tự soát (gọi tắt `SOAT`): `PW_CHANNEL=chrome PW_DIR=~/.npm/_npx/9833c18b2d85bc59/node_modules/playwright node docs/mockups/m7/soat.mjs` (Playwright của bộ đệm npx chạy Google Chrome của máy; chụp tệp HTML cục bộ, không phải e2e, không phải Playwright của console).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Công cụ soát đỏ trước khi có trang | `soat.mjs` (`PAGES` 3 trang, `CASES` 42 + 28 khóa) | `SOAT --no-shot` | `m7.css: chưa có`, `kiem-kho-mo-so: chưa có kiem-kho-mo-so.html`, `hang-hoan-don-cu: chưa có …`, `index: chưa có index.html`, `SOÁT MOCKUP: 4 lỗi` |
| Lần soát đầu sau khi vẽ bắt lỗi thật | `soat.mjs` (1), (3) | `SOAT --no-shot kiem-kho-mo-so hang-hoan-don-cu` | `SOÁT MOCKUP: 12 lỗi`: mục chạm thấp ("Còn 7 mã chưa đếm", liên kết PN/PL trong hộp kết quả), `tràn phải 17px: label.is-bad "Hỏng"` ở 360 → đổi liên kết thành nút, dòng hàng hoàn trên điện thoại xếp tình trạng thành một dòng riêng |
| Màn 10 đủ trạng thái bảng Task 247 và Task 273 (tạo, quét +1 trên điện thoại 390, −1, cộng dồn nhiều người, mất mạng gửi lại cùng khóa, mã lạ, mã trùng hai người bán, số âm, nhập Excel và lỗi từng dòng, còn N dòng chưa đếm, đăng, 422 `uncounted_lines`, 409, `warehouse_not_empty`, kết quả kiểm khớp/lệch, 403, lỗi tải) | `soat.mjs` kiểm (8), 43 khóa (thêm `loi-vao` cho HV01) | `SOAT` | `SOÁT MOCKUP OK: 3 trang, 8 ảnh` |
| Màn 13 đủ trạng thái (mã tham chiếu đã nhập rồi kèm liên kết, dòng hỏng, danh sách theo ngày, lỗi ô, rỗng, 403) | `soat.mjs` kiểm (8), 28 khóa | `SOAT` | như trên |
| Nút Đăng khóa khi còn dòng chưa đếm | `soat.mjs` kiểm (13) | `SOAT` | như trên (0 lỗi `nút "Đăng" không khóa`) |
| Không cuộn ngang, không tràn, mục chạm đủ, chỉ token có sẵn (cả `m7.css`), ô chọn đúng, không gì chỉ hiện khi rê chuột — 1512, 390, 360 | `soat.mjs` (1)–(7), (9), (11) | `SOAT` | như trên |
| Soát bằng mắt ảnh 1512, 390, 360 | đọc ảnh cắt khúc | — | Đã sửa: mã hàng xuống dòng giữa chừng trong bảng kiểm lệch (thêm `nowrap`); bỏ "Bạn đếm 4, Lan đếm 3" và "3 người đang đếm" (máy chủ không lưu số đếm theo người, chỉ `counted_by`/`counted_at` lần cuối) → "Lần cuối: Lan, 20:41"; thêm thẻ lối vào từ Kiểm kho (HV01 phải có hình). Số liệu khớp: 405 + 7 = 412; 4 dòng + "3 mã nữa" = 7; Excel 2.996 + 4 lỗi = 3.000 dòng; hàng hoàn 7 kiện = 2 + 4 + 1, 16 cái (3 hỏng); kiện VTP12345678 3 cái = 2 tốt + 1 hỏng ở cả form, kết quả, danh sách |
| Nén ảnh | `nen-anh.py` | `python3 docs/mockups/m7/nen-anh.py` | `shots/: 4.6 MB -> 1.4 MB` |
| Sau sửa review lượt 1 (chụp lại, nén lại) | `soat.mjs`, `nen-anh.py` | `SOAT`; `python3 docs/mockups/m7/nen-anh.py` | `SOÁT MOCKUP OK: 3 trang, 8 ảnh`; `shots/: 4.6 MB -> 1.4 MB` |
| Gộp `main` trước khi stage | — | `git merge main` | gộp tới `964af7c4` (fast-forward, không xung đột) |
| Cổng kiểm của task | `scripts/agent/ci-task.sh` | (một lần, trước khi stage) | `static: success, 8 phút 52 giây`; `test từ xa: không gói nào`; `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 964af7c4…)` |
| Chỉ đổi tài liệu, không chạm `CODE_PATHS`, không sửa `docs/mockups/m1/`–`m6/` | — | `git diff --cached --name-only` | 18 tệp, chỉ `docs/mockups/m7/**` (8 tệp + 8 ảnh), `docs/khoi-dong/mockup-m7.md`, `docs/tasks/task-247.md` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chỉ vẽ màn 10 và 13 (phạm vi M7 đợt này do điều phối M7 giao, 28/11); 13 màn còn lại ghi "chưa vẽ" trong `docs/khoi-dong/mockup-m7.md`, vẽ cùng task màn của chúng. Step 5 commit: `docs(crm): mockup màn M7 kiểm kho mở sổ và hàng hoàn đơn cũ (Điểm dừng B)` (tên kế hoạch "mockup các màn M7 đã duyệt" sẽ sai vì chưa có 13 màn).
- Step 4 "điều phối viên duyệt": Claude chốt thay (điều phối M7 giao), ghi ở `docs/mockups/m7/README.md` mục "Kết luận duyệt" và bảng HV.
- Màn 13 thêm ảnh 390, 360 (kế hoạch chỉ đòi 1512): nhân viên kho nhập hàng hoàn ngay ở kệ bằng điện thoại, tên sản phẩm dài.
- `soat.mjs` thêm biến `PW_CHANNEL` (dùng Chrome của máy): máy không có trình duyệt của Playwright (`~/Library/Caches/ms-playwright` không có), không tải thêm gì.
- Agent `design-critic` không có trong phiên (chỉ có `reviewer`); soát bằng `soat.mjs`, soát mắt và `reviewer`.

## Quyết định thay chủ shop

- **Chốt hình màn 10, 13 và 11 dòng hành vi HV01–HV11** (Điểm dừng B): điều phối M7 giao Claude chốt như HV của M4. Phương án khác: để "chưa" chờ chủ shop — chặn Task 273. Đảo ngược: chủ shop sửa dòng nào thì ghi vào README, Task 273 làm theo.
- **Mở sổ không có mục menu riêng (HV01)**: việc làm một lần mỗi kho, mục menu sẽ thừa mãi sau đêm mở; lối vào là nút "Mở sổ kho này" ở Kiểm kho (hiện khi có quyền mở sổ kho đó; không có API đọc trước "kho trống", nên kho đã có hàng thì bấm mới báo `warehouse_not_empty`) và màn Sẵn sàng (màn 7, sau). Phương án khác: mục "Mở sổ kho" ở nhóm Kho; hoặc thêm API đọc kho trống để ẩn nút (Việc phát hiện thêm 10).
- **Hàng hoàn đơn cũ có mục menu (HV02)** ở nhóm Kho sau "Nhận hàng hoàn": dùng nhiều tuần sau ngày mở, nhân viên kho cần tìm nhanh. Phương án khác: tab trong màn Nhận hàng hoàn.
- **Không có ô gõ số đếm ở màn mở sổ (HV04)**: đếm cộng dồn nhiều người; ô "đặt số" sẽ ghi đè số người khác vừa cộng. Sửa bằng −1; mã không có hàng dùng "Nhập 0" (chỉ ở dòng chưa đếm, gọi `recordStocktakeCount` của M1 với 0). Phương án khác: giữ ô số như M1 (nguy cơ ghi đè).
- **Mã trùng hai người bán**: hiện ứng viên, bấm "Cộng 1" ở món đúng (gửi lại bằng SKU của ứng viên). Cần trường API — Việc phát hiện thêm 2.
- **Chế độ nhập Excel mặc định "Đặt bằng số trong tệp"** (`replace`): nhập lại cùng tệp không đổi gì, an toàn khi bấm nhầm hai lần; "Cộng thêm" phải chọn tay.
- **Kho có phiếu vào giữa chừng khi đăng**: màn báo, nói rõ kho không mở sổ được nữa (Q14) và có nút "Hủy phiên MS-0001…" (`cancelStocktake` của M1, quyền theo Việc phát hiện thêm 8) để chuyển sang Kiểm kho thường.
- **Hàng hoàn cũ: mỗi dòng một tình trạng**, cùng mã có cả tốt và hỏng thì hai dòng (đúng `LegacyLine{VariantID, Qty, Condition}` của Task 264). Danh sách mặc định 7 ngày gần nhất.
- **Đường đề xuất** `/kho/mo-so`, `/kho/mo-so/:id`, `/kho/hang-hoan-cu`; Task 273 chốt.
- **Ảnh "Hiện tại" dùng ảnh mockup M1, M3 đã duyệt** (console làm đúng các mockup đó); không chạy console để chụp (lời dặn phiên: không chạy Playwright của console trong task).

## Phụ thuộc task để sau

- Màn 7 (Sẵn sàng dữ liệu, Task 272, để sau) là lối vào thứ hai của mở sổ và nơi hiện mốc A; mockup màn 10 chỉ ghi "đây là mốc A của đêm mở".

## Việc phát hiện thêm

Không tự sửa kế hoạch hay hợp đồng; ghi để Task 260 (hợp đồng), 262–264 (máy chủ), 273 (màn hình) xử lý.

1. **"Còn N mã chưa đếm" và danh sách mã chưa đếm**: kế hoạch có view `inventory.opening_status_v` (`lines_expected`, `lines_counted`) nhưng chưa có operation trả số này hay danh sách biến thể chưa có dòng (Task 273 đòi "lấy từ máy chủ"). Task 260/262: thêm vào `getStocktake` cho phiên `opening` (hoặc operation riêng) `linesExpected`, `linesCounted`, `unitsCounted` và danh sách chưa đếm có phân trang.
2. **`incrementStocktakeLine` khi mã trùng hai người bán**: chỉ nhận `{code, by}`; lỗi `ambiguous_code` trả ứng viên nhưng không có cách chọn một ứng viên nếu SKU cũng trùng. Task 263: thêm `variantId` tùy chọn (khi có thì bỏ qua tra mã), ứng viên trả `variantId, sku, sellerName, productName, optionLabel`.
3. **Người đếm lần cuối**: mockup hiện "Lần cuối: Lan, 20:41" từ `counted_by`, `counted_at` (M1 đã có); phản hồi `incrementStocktakeLine` chỉ ghi `counted`. Task 263 trả thêm hai trường này (hoặc Task 273 bỏ dòng đó).
4. **Lý do nhập Excel thất bại**: `count_imports.status = 'failed'` chưa có mã lý do (thiếu cột "Mã"/"Số đếm", tệp hỏng, quá 5 MiB khi đọc). Task 263 thêm `failureCode`.
5. **Ai đăng**: hộp "đã được Lan đăng lúc 21:04" cần người đăng. Bảng `inventory.stocktakes` của M1 có `applied_by`, `applied_at` (`migrations/0121_stocktakes.sql`), nhưng schema `Stocktake` của `getStocktake` chỉ có `appliedAt`, **thiếu `appliedBy`** (`openapi/openapi.yaml`, schema `Stocktake`). Task 260/262 thêm `appliedBy` (tên người), hoặc Task 273 bỏ tên.
6. **409 `legacy_return_exists`**: kế hoạch chỉ ghi `ExistingID`; màn cần `createdAt`, kho, người nhập để nói "nhập lúc 10:14 02/12 vào Kho chính (Hùng)" và dẫn tới ngày đó trong danh sách. Task 264 trả thêm, hoặc Task 273 chỉ hiện liên kết.
7. **`listLegacyReturns`**: chưa ghi trường trả về; màn cần `reference`, kho, số cái bán lại được / hỏng, người, giờ, mã phiếu nhập và phiếu chuyển hàng lỗi; chưa ghi phân trang. Task 260/264 ghi.
8. **Hủy phiên mở sổ**: cách thoát khi đăng bị `warehouse_not_empty` là hủy phiên (`cancelStocktake` của M1); kế hoạch chưa nói `cancelStocktake` áp cho phiên `opening` và quyền nào hủy. Task 262 ghi rõ (đề xuất: như M1, `inventory.stocktake`).
9. **"Nhập 0" chạy song song với quét**: "Nhập 0" dùng `recordStocktakeCount` (đặt số tuyệt đối); nếu người khác vừa quét mã đó thì có thể ghi đè về 0. Task 262/273: chỉ cho "Nhập 0" khi dòng chưa tồn tại (máy chủ từ chối nếu đã có dòng) hoặc thêm `ifAbsent`.
10. **Kho trống trước khi bấm, và phiên mở sổ đã có (reviewer lượt 1)**: không operation nào cho biết trước một kho "chưa có phiếu, tồn, hàng giữ" — mockup để nút "Mở sổ kho này" luôn hiện khi có quyền (HV01), lỗi báo sau khi bấm. 409 `opening_exists` của Task 262 chỉ là sentinel, không kèm phiên: mockup hộp `da-co-phien-mo-so` cần `stocktakeId`, mã phiên, người tạo, giờ tạo để "Vào phiên MS-0001" (console M1 chỉ nhớ phiên mở trên chính máy đó, `console/src/screens/inventory/Stocktake.tsx`). Task 262 trả các trường này trong thân 409 (hoặc thêm cách liệt kê phiên đang mở của một kho); không có thì Task 273 chỉ hiện câu báo, không có nút vào phiên.

## Reviewer

Lượt 1: **CẦN SỬA**, một phát hiện gồm năm chỗ mockup vẽ dữ liệu API kế hoạch không trả mà tệp task chưa ghi: (1) nút "Mở sổ kho này" chỉ hiện khi kho trống — không có API đọc trước → đổi thành hiện khi có quyền, lỗi báo sau khi bấm (HV01 sửa ở README, index, mockup), Việc phát hiện thêm 10; (2) 409 `opening_exists` hiện phiên, người tạo, nút vào phiên — thêm Việc phát hiện thêm 10; (3) "37 mã đếm 0" — bỏ khỏi hộp xác nhận và HV07; (4) "đã cộng lúc 20:15" — bỏ giờ; (5) thanh tiến độ 40% khi đọc tệp — bỏ thanh. Ghi chú không bắt buộc đã làm: dòng chạy trong `nen-anh.py` trỏ m7; chú thích `khung.js` nói tài khoản nhân viên kho; hộp kho có hàng giữa chừng có nút "Hủy phiên…" thay "Báo Chủ"; ca số âm dùng mã khác (AO0789-XANH-L); Việc phát hiện thêm 5 ghi rõ `getStocktake` thiếu `appliedBy`. Chụp lại mọi ảnh, `SOAT` xanh.

Lượt 2: **ĐẠT**. Năm chỗ của lượt 1 sửa đúng và đồng bộ (mockup, README, index); Việc phát hiện thêm 5, 10 đối chiếu đúng với `openapi.yaml` (schema `Stocktake` thiếu `appliedBy`) và `Stocktake.tsx` (chỉ nhớ phiên trên chính máy đó); số liệu khớp; không phát hiện mới. Reviewer không tự chạy được `soat.mjs` (chỉ lệnh đọc).

Kết luận: ĐẠT
