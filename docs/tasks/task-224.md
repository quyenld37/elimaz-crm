# Task 224 — M6-01 Mockup sáu màn báo cáo (không code)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` («Task 224 (M6-01)», «Điểm dừng B», bảng «Việc cần chủ shop trong M6», Q3–Q25; Interfaces của Task 225, 229–241 cho dữ liệu màn vẽ). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu (không tệp nào trong `CODE_PATHS`).

Căn cứ thêm: `docs/m6/tieu-chi.md` (C1–C8, BC01–BC05, PQ06; số kịch bản chín đơn), `docs/m6/doi-chieu-gia-dinh.md` (G6: màn đặt ở `console/src/screens/reports/**`; G18: ba quyền nhạy cảm `ScopeShop`), spec `01-ma-tran-tinh-nang.md` mục 9. Điều kiện "M1–M5 đóng" của M6-00 được chủ shop miễn ngày 27/11 sáng ("Đồng ý, làm luôn", ghi chú điều phối M6): M6 bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ staging).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (M6 chưa có operation nào trên `main`; Task 225 viết hợp đồng). Mockup chỉ vẽ dữ liệu mà 20 operation của Task 225 trả: `getSalesDashboard` (`ready` / `run_in_background`), `getSalesOrdersReport` (`basis`, `groupBy`, bộ lọc nhiều lựa chọn), `listSalesOrdersDrilldown` (`total`, `items`, `nextCursor`), `getSalesCarriersReport`, `getSalesBestSellers` (`level`, `basis`, `limit` ≤ 500), `getSalesReportFilters`, `getInventoryStock` (`date`, `firstSnapshotDay`), `getInventoryMovements`, `*ReportRun*` (`status: queued|running|ready|failed|expired`, `reason: too_heavy|busy|forbidden|error`, `rowCount`, `truncated`, `expiresAt`, `requestedByName`), `*CustomerExport*`; lỗi `range_too_long`, `bad_date`, `no_snapshot`, `run_expired`, `too_many_runs`, `export_forbidden`, 503 `retry_later`.
- Chỗ dùng trên màn hình: console chưa có màn báo cáo. Mockup là đầu vào của Task 239 (khung mục Báo cáo, Tổng quan, danh sách chi tiết, chạy nền, nút Xuất Excel), 240 (Báo cáo đơn, Hãng vận chuyển, Bán chạy), 241 (Tồn kho, Xuất nhập tồn, Tệp xuất, nút xuất danh sách khách và danh sách sản phẩm trên màn có sẵn `console/src/screens/customers`, `console/src/screens/products`). Menu: nhóm mới "Báo cáo" trong `docs/m6/mockup/khung.js` (mockup); Task 239 sửa `console/src/app/nav.ts` thật.
- Câu sqlc, bảng, pha migration: không.
- Tệp sẽ đổi (chỉ `docs/`): tạo `docs/m6/mockup/{index.html, ghi-chu.md, khung.js, m6.css, soat.mjs, nen-anh.py, tong-quan.html, don.html, van-chuyen.html, ban-chay.html, ton-kho.html, tep-xuat.html, nut-xuat.html}`, ảnh `docs/m6/mockup/*.png`; sửa `docs/tasks/task-224.md`. Không sửa `docs/mockups/m1`–`m5` (chỉ liên kết CSS, chụp ảnh "Hiện tại" của hai màn có sẵn từ mockup M1, M2 đã duyệt).
- Test sẽ chạy: không có test mã. "Test" là `docs/m6/mockup/soat.mjs` (Playwright Chromium trong bộ đệm npx, chép từ `docs/mockups/m5/soat.mjs` của Task 205/514; chụp tệp HTML cục bộ, không phải e2e, không phải `console/e2e`): không cuộn ngang, không tràn, không chữ bị cắt thiếu dấu …, mục chạm ≥ 44 px dưới 1000 px, chỉ màu/cỡ chữ/bo góc có sẵn, ô chọn có mũi tên trong ô, tiền dạng `1.910.000 ₫` trọn ô, đủ khóa trạng thái `CASES` kế hoạch đòi, menu đúng thứ tự nhóm, bảng báo cáo trên điện thoại chỉ cuộn ngang trong khung bảng. Viết `CASES` trước, chạy thấy đỏ (chưa có trang), rồi vẽ tới khi xanh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go (không áp dụng cho mockup). Thành phần mockup có sẵn: M1 `m1.css` (`.kpis/.kpi`, `.tbl` → thẻ dưới 760 px, `.tbl-frame`, `.toolbar`, `.toggle`, `.tabs/.tab`, `.bar`, `.dlg/.dlgs`, `.drawer`, `.box-*`, `.chip-*`, `.toast`, `.empty`, `.more`, `.check`, `.select-wrap`), M2 `m2.css` (`.scroll-x`, `.lblbar/.lbl`, `.split`, `.kv`), M3 `m3.css` (`.stats/.stat`, `.selbar`, `.only-desk/.only-phone`, `.nav-n`), M5 (`khung.js`, `soat.mjs`, `nen-anh.py`, khung `.st`, cách làm `index.html` + bảng hành vi có cột OK).
- Dùng lại / viết mới vì: dùng lại nguyên `m1.css`, `m2.css`, `m3.css` bằng liên kết (`../../mockups/…`), không chép. `khung.js`, `soat.mjs`, `nen-anh.py` chép sang `docs/m6/mockup/` (không sửa bản M5) vì kế hoạch đặt mockup M6 ở `docs/m6/mockup/` và menu M6 khác M5 (thêm nhóm "Hộp chat" của M4 và nhóm "Báo cáo"); bản M5 là bằng chứng của Task 205/514, sửa tại chỗ sẽ đổi bằng chứng đó. `m6.css` chỉ thêm thành phần chưa có, bằng biến của `m1.css`: bảng số liệu không đổi thành thẻ trên điện thoại (cuộn ngang trong khung, cột nhóm dính trái), ô số bấm được, thanh ngang CSS theo ngày (dùng `.bar` của M1), bố cục bảng + ngăn chi tiết bên phải.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã. Các câu dưới làm căn cứ cho **nội dung** mockup (màn phải nói đúng luật mà API của Task 225–238 sẽ làm).

- 5.1–5.12: không áp dụng cho mã của task (không mã, bảng, route, thư viện mới trong repo; Playwright chạy từ bộ đệm npx). 5.9 (chống trùng): nút "Xuất Excel", "Xuất danh sách khách", "Xuất danh sách sản phẩm" và lần tự chuyển chạy nền khóa khi đang gửi; một `Idempotency-Key` mỗi lần bấm, giữ khi tự gửi lại (Task 239) — vẽ trạng thái "Đang gửi…".
- 6.1 (hai người cùng làm): báo cáo chỉ đọc; không có `version`. Không áp dụng.
- 6.2 (bấm hai lần): như 5.9; 429 `too_many_runs` (5 lần chưa xong mỗi người, Q22) có câu riêng.
- 6.3 (tiền): tiền `int64` đồng, hiện `1.910.000 ₫`; ô 9 chữ số (`123.456.789 ₫`) phải vừa ô ở 360. "Doanh số" = tiền hàng sau giảm giá, chưa gồm ship (Q3), chú thích "chưa phải doanh thu kế toán" trên mọi màn có doanh số (C2). Tỷ lệ phần vạn hiện `66,67%`, mẫu số 0 hiện "—" (Q5, C6).
- 6.4 (tồn): Tồn kho hôm nay = số dư hiện tại; ngày đã qua = bản chụp 24:00 giờ VN (Q7); chọn ngày trước bản chụp đầu → "Chưa có bản chụp trước ngày dd/mm/yyyy" (`no_snapshot`). Cột "Có thể bán" = tồn sổ − đang giữ − đang giao − hàng lỗi (cùng công thức M1).
- 6.5–6.11: không áp dụng: không có mã chạy. Nội dung liên quan: tổng quan tự tải lại mỗi 60 s và hiện "Cập nhật lúc HH:MM:SS" (Q21); báo cáo quá 30 s hoặc hết chỗ tự chuyển chạy nền (Q8) → "Báo cáo nặng, đang chạy nền. Sẽ báo khi xong"; chạy nền quá 60 s → "Báo cáo quá nặng, thu hẹp khoảng thời gian"; tệp quá 200.000 dòng có cờ "đã cắt" (Q11); trang Tệp xuất tự tải lại khi có báo hiệu và mỗi 20 s.
- 6.12 (dữ liệu cá nhân): báo cáo không hiện SĐT, địa chỉ, tên khách (C8); danh sách chi tiết chỉ hiện mã khách. Tệp danh sách khách có SĐT nên hộp xuất không có ô chữ tự do (Q13), ghi rõ hạn 24 giờ và ghi nhật ký.
- 6.13 (quyền): `report.sales.read` theo Page, `report.inventory.read` theo kho (Q14); người chỉ có một Page thấy dòng "Phạm vi: <tên Page>", không thấy đơn web/nhập tay và **không thấy nút xuất** (`report.export` cần phạm vi `all`, `ScopeShop`, G18); không có quyền → 403 có câu; nút xuất danh sách khách chỉ cho `customer.export` phạm vi `all`. Không gì chỉ hiện khi rê chuột. Kiểm thật vẫn ở API.

## Bằng chứng

Lệnh tự soát (gọi tắt `SOAT`): `PW_DIR=~/.npm/_npx/5e2e484947874241/node_modules/playwright node docs/m6/mockup/soat.mjs` (Playwright của bộ đệm npx, như Task 205/514; chỉ mở tệp HTML cục bộ, không phải e2e hay `console/e2e`).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Công cụ soát đỏ trước khi có trang | `soat.mjs` (8 trang trong `PAGES`, khóa `CASES` của 7 trang) | `SOAT --no-shot` | `m6.css: chưa có`, `tong-quan: chưa có tong-quan.html` … `index: chưa có index.html`, `SOÁT MOCKUP: 9 lỗi` |
| Sáu màn đủ Interfaces Task 224 (M1–M6) và dữ liệu cứng (tên 120 ký tự, tên nhân viên dài, 0 đơn, 1 đơn, 1.200 SKU, "—", 9 chữ số, người một Page không nút xuất), cộng trạng thái của Task 239–241 Step 1 (đang tải, lỗi, 503, chạy nền, quá nặng, 403, 429, 410, không có bản chụp) | `soat.mjs` kiểm (8): 48 khóa `tong-quan`, 33 `don`, 19 `van-chuyen`, 20 `ban-chay`, 27 `ton-kho`, 31 `tep-xuat`, 15 `nut-xuat` | `SOAT` | `SOÁT MOCKUP OK: 8 trang, 23 ảnh` (0 "thiếu trạng thái") |
| Không cuộn ngang, không tràn, mục chạm ≥ 44 px dưới 1000 px, chỉ token có sẵn (cả `m6.css`), ô chọn đúng, không gì chỉ hiện khi rê chuột, tiền `1.910.000 ₫` trọn ô — 1512, 390, 360 | `soat.mjs` (1)–(9), (11) | `SOAT` | như trên |
| Bảng số liệu chỉ cuộn ngang trong khung, không đổi thành thẻ, đầu bảng còn ở 390/360 (Task 240 Step 1); vùng một Page / một kho không có nút "Xuất"; "Tổng quan" chỉ trong nhóm Báo cáo; ô mẫu số 0 hiện "—"; thứ tự 12 nhóm menu | `soat.mjs` (12)–(16) | `SOAT` | như trên |
| Hai ảnh "Hiện tại" của màn bị thêm nút (c), (d) | `soat.mjs` `CURRENT` | `SOAT` | `hien-tai-khach-{1512,390}.png`, `hien-tai-san-pham-{1512,390}.png` |
| Số khớp kịch bản chín đơn (Task 227 O1–O9) và cộng khớp trong mỗi bảng (sau review lượt 1) | đối chiếu tay | — | Tổng quan hôm nay: 8 dòng ngăn chi tiết = O1, O2, O3, O4, O5, O6, O8, O9; tiền hàng chưa hủy 300+250+150+360+250+150+450 = 1.910.000; theo kênh Facebook 6/5/1.300.000, Website 1/1/360.000, Nhập tay 1/1/250.000, tổng 8/7/1.910.000; ô Chờ xác nhận → O4 360.000; P1 = O1, O2, O8, O9 = 4 đơn 1.150.000, TB 287.500. Báo cáo đơn: An O1 O2 O8 O9 = 4 / 1.150.000 / thành công 2 / hoàn 1 / 66,67%; Bình O3 O5 O6 = 3 / 400.000 / hủy 1 / "—"; Chưa phân công O4 = 1 / 360.000; tổng 8 / 1.910.000 / 66,67%; ngăn "4 đơn" = 4 dòng cộng 1.150.000; theo ngày hoàn tất: số đơn = thành công + hoàn (62 = 58 + 4 …, tổng 76 = 71 + 5), doanh số chỉ đơn giao xong = 21.880.000 = ô Thành công 7 ngày của Tổng quan. Bán chạy mẫu: Áo A 7 / 4 đơn / 1.050.000, Quần B 2 / 500.000 trước Váy C 2 / 360.000 (Task 232). 7 ngày: chưa hủy 7+16+31+11+0+13+14 = 92, 28.734.000; thành công 2+14+18+12+0+13+12 = 71, 21.880.000. Hãng: Vận đơn tạo = không gồm đã hủy (Task 231): GHN 39 + 11 = 50, tổng 50+30+4+3 = 87 = đang đi 18 + đang hoàn 1 + thất bại 1 + thành công 56 + hoàn 11; VTP 16 ÷ 24 = 66,67%; phí TB = phí ÷ vận đơn tạo (1.248.000 ÷ 50 = 24.960; 2.142.000 ÷ 87 = 24.621). Tồn: 24−3−2−1 = 18; xuất nhập tồn 30+20+2−27−0−1 = 24 = tồn sổ hôm nay |
| Tên sản phẩm 120 ký tự | `python3 len()` | — | `120` |
| Soát bằng mắt ảnh 1512, 390, 360 | đọc ảnh | — | Đã sửa: ô số trống ở trạng thái F (Tổng quan) chữ dính số → `.kpi` dạng lưới; ô chọn "Nhóm theo: Nhân viên phụ trách" bị cắt → ô chọn có nhãn phía trên (`.rep-f`); trang Tệp xuất cuộn ngang 1574 px (nhãn ẩn `.sr` của cột thao tác có `position: absolute` thoát khỏi khung bảng) → nhãn hiện "Thao tác"; cột thao tác bị đẩy khỏi khung ở 1512 → tên dài cắt 200 px, chip "Đã cắt" xuống dòng; dòng tài khoản hãng lệch dòng (`&nbsp;` trước khối) → thụt lề bằng `padding` |
| Nén ảnh | `nen-anh.py` | `python3 docs/m6/mockup/nen-anh.py` | `docs/m6/mockup: 9.7 MB -> 2.8 MB` (lần chụp đầu); sau sửa review chụp lại cả bộ: `9.8 MB -> 2.8 MB` |
| Sau sửa review lượt 1 (số theo O1–O9; bỏ cột API không trả: Hủy/Thành công/Hoàn theo kênh, Đơn lên/Hủy theo ngày, Tiền hàng hoàn, Mã mẫu; thêm cột Đang hoàn, Vận đơn tạo không gồm đã hủy; bỏ nút Của tôi/Mọi người; câu quyền tải và hạn 24 giờ; trạng thái mẫu Nháp) | `soat.mjs` | `SOAT` | `SOÁT MOCKUP OK: 8 trang, 23 ảnh` |
| Cổng tĩnh của task (gitleaks, jscpd…; không gói Go nào đổi) | `ci-task.sh` | `scripts/agent/ci-task.sh` | `static: success`, `test từ xa: không gói nào`, `CI-TASK XANH (từ xa trên blacksmith …)`, run 45934269596 |
| Gộp `main` trước khi stage | — | `git merge main` | `Already up to date.` (main `7919960a`) |
| Chỉ đổi tài liệu | — | `git diff --cached --name-only` | chỉ `docs/m6/mockup/**` và `docs/tasks/task-224.md` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm trang `nut-xuat.html` (ngoài sáu màn) để vẽ hành vi (c), (d) trên hai màn có sẵn; kèm ảnh "Hiện tại" chụp từ mockup đã duyệt M2 `khach-danh-sach.html`, M1 `san-pham-danh-sach.html` (không chạy console thật).
- Ảnh 360 cho mọi màn (kế hoạch: "thêm 360 cho các bảng có thể tràn chữ"; cả sáu màn đều có bảng số rộng).
- Thêm `khung.js`, `m6.css`, `soat.mjs`, `nen-anh.py` (kế hoạch chỉ ghi `index.html`, `*.png`, `ghi-chu.md`): cần để vẽ và tự soát như M5.
- Kế hoạch ghi màn đặt ở `console/src/features/reports/**`; G6 của `doi-chieu-gia-dinh.md` đổi thành `console/src/screens/reports/**`. Mockup không chạm mã; ghi chú dùng đường của G6.
- Agent `design-critic` không có trong phiên (chỉ có `reviewer`); theo kế hoạch đó là việc của điều phối viên ở Điểm dừng B. Soát bằng `soat.mjs`, soát mắt và agent `reviewer`.
- Điều kiện "M1–M5 đóng" của M6-00 được chủ shop miễn ngày 27/11 (ghi ở đầu tệp).

## Quyết định thay chủ shop

- **Nhóm menu "Báo cáo" đặt giữa "Kho" và "Hệ thống"** (12 nhóm: Hộp chat … Kho, Báo cáo, Hệ thống, Quản trị). Vì sao: báo cáo đọc số của mọi nhóm vận hành phía trên; nhóm cuối là cài đặt, quản trị. Phương án khác: đầu menu (trước Hộp chat), hoặc ngay sau Đơn hàng. Đảo ngược: một dòng `NAV_GROUPS` ở Task 239.
- **Tên mục "Tồn và xuất nhập tồn"** thay cho "Tồn kho" của kế hoạch (M5): nhóm Kho đã có mục "Tồn kho" (màn vận hành của M1); hai mục cùng tên dễ nhầm. Tiêu đề, đường `/bao-cao/ton-kho` giữ nguyên. Phương án khác: "Báo cáo tồn kho".
- **Bảng báo cáo không đổi thành thẻ trên điện thoại**: cuộn ngang trong khung, cột nhóm dính trái (Task 240 Step 1 đòi "cuộn ngang trong bảng, không cuộn cả trang"). Thành phần mới `.rt` trong `m6.css`, chỉ bằng token có sẵn.
- **Ô số bấm được có gạch chân chấm** (`.cell-n`) để thấy bấm được mà không cần rê chuột; ô đang xem tô đậm. Ô 0 không bấm được.
- **Ngăn chi tiết rộng 520 px**, dưới 1200 px xuống dưới bảng; dưới 1000 px là trang riêng có nút quay lại (hành vi (a), vẫn chờ chủ shop OK).
- **Tiền hiện kèm "₫"** (`1.910.000 ₫`) như mọi mockup và màn CRM đã có; kế hoạch Task 239 ghi `money(1910000)` → `"1.910.000"`. Xem "Việc phát hiện thêm" 1.
- **Hộp xuất danh sách khách** dùng đúng các trường của `CustomerExportParams` (Task 238): nhãn, loại khách, ngày tạo, ngày mua cuối, số đơn tối thiểu; không ô chữ.
- **Giá trị TB làm tròn tới đồng** trong mockup (272.857 ₫ của kịch bản cũng là làm tròn). Cách làm tròn thật do Task 229/239 chốt.
- **Ảnh đặt thẳng trong `docs/m6/mockup/`** (kế hoạch ghi `docs/m6/mockup/*.png`), không thư mục `shots/` như M5.

## Việc phát hiện thêm

1. Task 239 `format.ts`: kế hoạch ghi `money(1910000) === "1.910.000"` (không "₫"), còn mockup M1–M6 và màn CRM hiện có hiện "1.910.000 ₫". Task 239 chọn một cách: giữ `money()` không ký hiệu và thêm "₫" ở chỗ hiện, hoặc sửa test của kế hoạch. Không sửa ở Task 224.
2. Kế hoạch chưa nói cách làm tròn "Giá trị TB" (chia nguyên cắt phần lẻ hay làm tròn). Kịch bản chín đơn 1.910.000 ÷ 7 = 272.857,14 cho cùng kết quả cả hai cách; Task 229 nên chốt và có ca test số lẻ ≥ ,5.
3. Phí ship thu khách của kịch bản chín đơn: bảng O1–O9 (Task 227) ghi ship 30.000 cho O1, O2, O4, O8, O9 → 150.000, còn dòng "Số đúng của kịch bản" (Task 227) và `docs/m6/tieu-chi.md` ghi 120.000. Mockup vẽ theo từng đơn (150.000). Task 227/229 chốt một số và sửa chỗ kia.
4. Làm tròn tỷ lệ: Task 230 ghi `SuccessRateBp` "làm tròn xuống" nhưng ví dụ 2/3 = 6667 (làm tròn gần nhất; làm tròn xuống ra 6666). Mockup làm tròn gần nhất (66,67%, 93,55%). Task 230/231 chốt.
5. `listSalesReportRuns` không có tham số lọc theo người tạo, `ReportRun` chỉ có `requestedByName`: mockup Tệp xuất không vẽ nút "Của tôi / Mọi người", chỉ ghi Quản lý thấy mọi lần chạy (cột Người tạo). Nếu cần lọc, Task 225/235 thêm tham số.

## Việc chủ shop cần OK (danh sách để điều phối viên gửi)

- Xem ảnh sáu màn ở `docs/m6/mockup/index.html` (điều phối viên chốt hình ở Điểm dừng B; chủ shop xem lại, không chặn).
- Hành vi (a) ngăn chi tiết bên phải / trang riêng trên điện thoại — Tổng quan, Báo cáo đơn (Task 239, 240).
- Hành vi (b) nút "Xuất Excel" ở góc phải đầu trang năm màn báo cáo, tệp ở màn Tệp xuất (Task 239–241).
- Hành vi (c) nút "Xuất danh sách khách" trên màn Khách hàng (Task 241).
- Hành vi (d) nút "Xuất danh sách sản phẩm" trên màn Sản phẩm và màn Tồn (Task 241).
- (Không chặn) Chỗ đặt nhóm menu "Báo cáo" và tên mục "Tồn và xuất nhập tồn" (quyết định thay chủ shop ở trên).

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA, 5 phát hiện: (1) số chia nhỏ không theo kịch bản O1–O9 (theo kênh, theo nhân viên, theo mẫu, ngăn chi tiết); (2) màn một Page lệch dữ liệu toàn shop; (3) vẽ cột API không trả (Hủy/Thành công/Hoàn theo kênh, Đơn lên/Hủy theo ngày, Tiền hàng hoàn, Mã mẫu); (4) "Vận đơn tạo" tính cả đã hủy, trái Task 231; (5) câu "chỉ bạn tải được", "tự xóa sau 24 giờ" sai với Q12, G5. Ghi chú: ship 120.000/150.000, làm tròn tỷ lệ, nút Của tôi/Mọi người không có tham số API, thiếu "Nháp", tiêu đề Bán chạy B. Đã sửa hết (xem dòng bằng chứng "Sau sửa review lượt 1"), ghi Việc phát hiện thêm 3–5.
- Lượt 2: ĐẠT, không phát hiện mới; reviewer tự cộng lại mọi số ở dòng bằng chứng và đều khớp.
