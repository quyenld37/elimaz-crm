# Task 49 — Mockup các màn M1

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 49 = M1-49). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu (không có tệp nào trong `CODE_PATHS`).

Căn cứ: kế hoạch M1 mục "Task 49 (M1-49)", các Interface của Task M1-26, 27, 29, 34, 35, 38, 41, 43, 44, 46, 47, 48, 50–56; Q23, Q24, Q32, Q34, Q35, Q-X8; spec `01-ma-tran-tinh-nang.md` mục 3, 8; hệ thiết kế `apps/storefront-giftstatus/DESIGN.md`; hợp đồng `openapi/openapi.yaml` (`PlatformHealth`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml`. Mockup chỉ vẽ dữ liệu của các operation kế hoạch đã đặt tên (`listProducts`, `getProduct`, `listBalances`, `createStockDocument`, `postStockDocument`, `listSuppliers`, `startStocktake`, `recordStocktakeCount`, `applyStocktake`, `createCatalogImport`, `getCatalogImport`, `applyCatalogImport`, `getPlatformHealth`, `listLostFiles`, `verifyFiles`, `listStaff`, `replaceStaffGrants`, `lockStaff`, `listRoles`, `createRole`, `updateRole`, `archiveRole`, `listPermissions`). Chỗ mockup cần trường chưa có trong hợp đồng ghi ở "Việc phát hiện thêm".
- Chỗ dùng trên màn hình: chưa có `console/` (Task M0-17 chưa làm). Mockup là đầu vào của Task M1-50 (Sản phẩm), M1-51 (Kho), M1-52 (Nhập Excel), M1-53 (Sức khỏe, Sự kiện lỗi, Tệp bị mất), M1-54 (Nhân viên), M1-56 (Vai trò); Task M1-48 dựng khung điều hướng theo khung trong mockup.
- Câu sqlc, bảng, pha migration: không.
- Tệp sẽ đổi: tạo `docs/mockups/m1/` (13 trang HTML của kế hoạch, `m1.css` dùng chung, `khung.js` menu chung, `soat.mjs` công cụ chụp và tự soát, `png/*-{1512,390}.png` và `*-360.png` cho màn dễ tràn); `docs/tasks/task-49.md`; `docs/tasks/hien-tai.md` (qua script).
- Test sẽ chạy: không có test mã. "Test" của task là công cụ tự soát `docs/mockups/m1/soat.mjs` (Playwright Chromium, có sẵn trên máy trong bộ đệm npx, không thêm vào repo): với mỗi trang × mỗi cỡ (1512×767, 390×844, 360×800 cho màn dễ tràn) kiểm (1) không cuộn ngang; (2) không chữ bị cắt ngoài các chỗ cố ý cắt có dấu `…`; (3) mục chạm ≥ 44 px dưới 1000 px, ≥ 32 px từ 1000 px; (4) mọi màu, bo góc, cỡ chữ tính ra đều thuộc tập có sẵn của DESIGN.md; (5) mọi `<select>` nằm trong `.select-wrap`; (6) không luật `:hover` nào làm hiện/ẩn thứ gì; rồi chụp ảnh. Viết công cụ trước, chạy thấy đỏ (chưa có trang), rồi vẽ trang cho tới khi xanh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go, không có mục màn hình. Tìm mockup cũ: `docs/mockups/` (dự án) có hai mockup website (`khoi-mua-hang-1711.html`, `so-do-san-pham-1711.html`), `docs/admin/*/mockup` có mockup admin dùng `admin-globals.css` của website; repo CRM chưa có mockup nào. Hệ thiết kế: `apps/storefront-giftstatus/DESIGN.md` và `src/styles/tokens.css` (giá trị token).
- Dùng lại / viết mới vì: dùng lại nguyên giá trị token của `tokens.css` (màu, khoảng cách), bốn cỡ bo góc, cỡ chữ, luật `.select-wrap`, nút ≥ 44 px trên điện thoại, chữ C+ của DESIGN.md. Không dùng `admin-globals.css` vì đó là kiểu cũ của admin website, không phải hệ thiết kế đã duyệt. Viết mới `m1.css` (một tệp cho cả 13 trang, để các màn cùng một khung và cùng thành phần) và `soat.mjs` (chưa có công cụ tự soát mockup nào trong repo CRM; luật `radius-scale.spec.ts` của website chỉ chạy trên trang website).

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã. Các câu dưới dùng làm căn cứ cho **nội dung** mockup (màn hình phải nói đúng luật mà API sẽ làm).

- 5.1–5.12: không áp dụng cho mã của task (không mã, bảng, route, thư viện mới trong repo; Playwright chỉ chạy từ bộ đệm npx của máy, ghi ở công cụ, không thêm phụ thuộc). 5.9 (mọi đường ghi có khóa chống trùng): mockup vẽ nút bấm một lần, nút ghi chuyển sang "Đang lưu…" và khóa lại trong lúc chờ; khi tự gửi lại không hiện gì khác (Task M1-48 `useWriteAction`).
- 6.1 (hai người cùng sửa): mockup Sửa mẫu, Nhân viên, Vai trò, Nhà cung cấp có trạng thái "đã thay đổi, tải lại" cho `version_conflict`.
- 6.2 (bấm hai lần): ghi sổ phiếu, áp dụng kiểm kho, áp dụng nhập Excel có bước xác nhận và nút khóa khi đang gửi.
- 6.3–6.11: không áp dụng: không có mã chạy.
- 6.12 (dữ liệu cá nhân): SĐT nhà cung cấp, email nhân viên chỉ hiện trên màn của người có quyền; màn Sự kiện lỗi chỉ hiện `message` đã che (M0-20 `ScrubMessage`), cảnh báo chỉ mang mã (Task M1-35, M1-36), mockup dùng câu mẫu không có SĐT.
- 6.13 (quyền): mockup hiện nút không bấm được **kèm lý do bằng chữ** (không ẩn, không chỉ hiện khi rê chuột) cho ghi sổ khi thiếu `inventory.documents.post`, quyền vượt người sửa ở màn Vai trò, vai trò vượt người gán ở màn Nhân viên; mục menu không có quyền thì không hiện và URL trực tiếp hiện "Không có quyền" (Task M1-53). Kiểm thật vẫn ở API.

## Bằng chứng

Lệnh tự soát (gọi tắt `SOAT`): `PW_DIR=~/.npm/_npx/5e2e484947874241/node_modules/playwright node docs/mockups/m1/soat.mjs` (Playwright 1.60.0, Chromium có sẵn trên máy).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Công cụ soát chạy đỏ trước khi có trang | `soat.mjs` | `SOAT --no-shot` (lúc chưa có trang) | `index: chưa có index.html` … `SOÁT MOCKUP: 13 lỗi`, exit 1 |
| Công cụ bắt được chữ bị cắt trong ô chọn (kiểm chính công cụ) | trang tạm có lựa chọn dài | `SOAT --no-shot <trang tạm>` | `chữ trong ô chọn bị cắt: select … "Trạng thái: Đang bán dài dài"` ×2, `SOÁT MOCKUP: 3 lỗi`; trang tạm đã xóa |
| 12 màn + `index.html` có đủ, chụp 1512×767 và 390×844, thêm 360 cho 7 màn dễ tràn (bảng biến thể, kiểm kho, tên dài) | `soat.mjs` | `SOAT` | `SOÁT MOCKUP OK: 13 trang, 33 ảnh`; `ls docs/mockups/m1/png` → 33 tệp: `index(2) kho-kiem-kho(3) kho-nha-cung-cap(3) kho-phieu(2) kho-ton(2) nhan-vien(3) nhap-excel(2) san-pham-danh-sach(3) san-pham-sua(3) su-kien-loi(3) suc-khoe(2) tep-bi-mat(2) vai-tro(3)` |
| Không chữ tràn, không cuộn ngang; mục chạm ≥ 44 px dưới 1000 px, ≥ 32 px từ 1000 px; chỉ màu, cỡ chữ, bo góc có sẵn của DESIGN.md; mọi `<select>` trong `.select-wrap`; không gì chỉ hiện khi rê chuột | `soat.mjs` kiểm (1)–(6) trên kiểu tính ra của mọi phần tử | `SOAT` | 0 lỗi (lần chạy cuối). Lỗi công cụ đã bắt và đã sửa trong lúc vẽ: liên kết trong câu và mã phiếu thấp 17–22 px, nút "Sửa" co còn 34 px ở 360, màu đen mặc định của ô chọn, ô chọn cắt chữ "Không gắn màu" ở 390 |
| Soát bằng mắt ảnh chụp (Step 2) | đọc ảnh 1512, 390, 360 | — | Đã sửa: dòng cuối bảng căn phải trên điện thoại; thẻ tồn kho 5 dòng → lưới 4 ô; ảnh mẫu 1 cột → 2 cột ở 390; thanh lưu dính đáy đè nội dung trong ảnh cả trang (để tĩnh, có ghi chú); danh sách nhà cung cấp làm giãn hàng ô |
| Mockup chỉ vẽ dữ liệu API có | đối chiếu với `ProductRow`, `BalanceRow`, `listLostFiles`, `PlatformHealth` | đọc kế hoạch Task M1-39, M1-46, `openapi.yaml` | Đã bỏ: tổng "50 trên N", số đếm ở nút lọc thiếu, vị trí ảnh và giờ kiểm cuối ở Tệp bị mất; thêm ô chọn kho ở Danh sách. Theo reviewer lượt 1: bỏ tên và giờ người sửa trong câu `version_conflict` (lỗi 409 chỉ có mã), bỏ dòng "tệp nhập Excel bị mất" (`file.verify` chỉ duyệt tệp `ready`, tệp Excel dừng ở `uploaded`), ảnh đang xử lý vẽ thành dòng "Đang tải lên" tách khỏi lưới (chưa gắn vào mẫu, `AttachImage` đòi `ready`), Quản lý được gán vai trò Quản lý (luật M1-26: chỉ đòi quyền của vai trò được gán). Chỗ còn cần trường mới ghi ở "Việc phát hiện thêm" |
| Bảng thay đổi hành vi, mỗi hành vi một dòng để chủ shop OK riêng | `index.html` mục "Thay đổi hành vi" | `grep -c '<tr><td data-label="#"' docs/mockups/m1/index.html` | `35` (H01–H35), cột "Chủ shop OK" = "chưa". Lượt reviewer 1 bắt thiếu 5 dòng (khóa, lưu trữ nhà cung cấp, lưu trữ vai trò, hủy phiên kiểm, ảnh đang tải lên) → đã thêm H31–H35 |
| Chỉ đổi tài liệu, không chạm `CODE_PATHS` | — | `git diff --cached --name-only` | 50 tệp, chỉ `docs/mockups/m1/**` và `docs/tasks/task-49.md` (tệp trạng thái task do script quản lý, hook chặn stage tay) |

Dữ liệu cứng của kế hoạch → chỗ vẽ:

| Dữ liệu | Màn · trạng thái |
|---|---|
| Tên mẫu 80 ký tự | Danh sách A; Sửa mẫu A (tiêu đề, ô tên); Tồn kho A; Phiếu kho B; Kiểm kho B |
| Bài viết 0 khối, 30 khối | Sửa mẫu B (0 khối); A (30 khối: 4 khối đầu, 24 khối thu gọn, khối 29 chứa `<img src=x onerror=alert(1)>` hiện nguyên văn, khối 30 lỗi) |
| Nhà cung cấp tên 200 ký tự | Nhà cung cấp A; Phiếu kho A, F |
| Vai trò 40 quyền | Vai trò B (Chủ, 40/40, chỉ xem) |
| 0, 1, 200 mẫu | Danh sách D, C, A |
| Mẫu ngừng bán | Danh sách B; Tồn kho A; biến thể Be · L ở Sửa mẫu A |
| Ảnh đang xử lý, bị từ chối (từng lý do), bị mất | Sửa mẫu A (mục "Đang tải lên": một ảnh đang xử lý, hai ảnh bị từ chối; ô ảnh 3 bị mất), E (bảng lý do → câu); Danh sách A (ảnh bị mất, chưa có ảnh) |
| Tồn âm (thiếu so với phần giữ) | Tồn kho A, B; Danh sách A; Kiểm kho C, E |
| Phiếu nháp, đã ghi sổ | Phiếu kho A, B, F |
| Lỗi Excel 7 dòng | Nhập Excel C (đúng 7 dòng của `loi-tung-dong.xlsx`, Task M1-38) |
| Cảnh báo gấp và thường | Sức khỏe A (chip "GẤP" / "Thường", phân biệt bằng chữ) |
| Nhân viên bị khóa, Chủ cuối cùng | Nhân viên A (Đỗ Thị Mai đã khóa), D ("Phải còn ít nhất một Chủ đang hoạt động") |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm ba tệp ngoài danh sách của kế hoạch: `m1.css` (kiểu chung của 13 trang), `khung.js` (menu chung, để 12 màn không chép 12 bản menu), `soat.mjs` (công cụ chụp và tự soát cho Step 1–2). Đều trong `docs/mockups/m1/`, không phải mã CRM.
- Ảnh 360 chụp cho 7 màn (kế hoạch: "bảng biến thể và kiểm kho", và "màn có chữ dễ tràn"): thêm Danh sách, Nhà cung cấp (tên 200 ký tự), Sự kiện lỗi (thông báo dài), Nhân viên, Vai trò.
- Agent `design-critic` không có trong phiên này. Chạy nó thuộc Điểm dừng M1-C của điều phối viên (kế hoạch), không thuộc Step 1–3 của task; Step 2 làm bằng `soat.mjs` và soát ảnh bằng mắt.
- Không có ảnh "Hiện tại" của Nhanh: CRM không đọc Nhanh (CLAUDE.md). Cột "Hiện tại" của bảng hành vi ghi theo spec 01 và khảo sát, "chưa khảo sát" khi chưa biết.

## Việc phát hiện thêm

Không tự sửa kế hoạch hay hợp đồng; ghi để task sau xử lý.

1. **Màn Sự kiện lỗi (Task M1-53) cần trường API chưa có:** `ErrorSummary` của `GET /api/v1/platform/health` chỉ có `fingerprint, name, role, count, lastSeenAt`; màn (8) theo kế hoạch cần "lần đầu" và thông báo (đã che). Bảng `platform.error_events` có `first_seen_at`, `message`. Cần thêm `firstSeenAt`, `message` vào `ErrorSummary` (mở rộng, không phá vỡ) hoặc một operation riêng; Task M1-53 quyết.
2. **`listProducts` (Task M1-39) chưa ghi cột ảnh bìa, đường dẫn danh mục, trạng thái mẫu** mà màn (1) cần ("cột ảnh, mã, tên, giá, có thể bán"; `ProductRow` ghi "mẫu, số biến thể, giá nhỏ nhất/lớn nhất, tổng `available` ở kho"). Ảnh bìa cần trạng thái `ready`/`lost` để vẽ ô "Ảnh bị mất" (ảnh đang xử lý chưa gắn vào mẫu nên không thể là ảnh bìa, Task M1-44). Task M1-39 hoặc M1-50 bổ sung. `ListProducts` tính có thể bán theo **một** kho (`WarehouseID`), nên mockup có ô chọn kho trên thanh lọc.
3. `listProducts`, `listBalances` phân trang con trỏ, không trả tổng số dòng: mockup không hiện "50 trên N", nút lọc "Thiếu so với phần giữ" không kèm số.
4. Màn Vai trò hiện "Người đang giữ": `listRoles` (Task M1-56) không có trường này; màn tính từ `listStaff`, hoặc thêm trường. Task M1-56 quyết.
5. Câu báo lỗi trong mockup (lý do từ chối ảnh, `cannot_modify_self`, `grant_exceeds_actor`, `last_owner`, `email_taken`, `version_conflict`, `insufficient_stock`, `supplier_name_taken`, `role_in_use`, …) là bản đề xuất cho `console/src/app/errors.ts` (Task M1-48); lý do `source_missing` (Task M1-43) cũng cần câu.
6. Câu hỏi mở O3 (nhân viên kho có dùng điện thoại không) vẫn mở; mockup Kiểm kho giả định có (Q23).
7. Cột "Có thể bán" ở Danh sách là tổng các biến thể nên che biến thể đang âm (AO0321 hiện 133 trong khi AO0321-HONG-M là −1). Task M1-39/M1-50 cân nhắc thêm cờ "có biến thể thiếu so với phần giữ" vào `ProductRow`.
8. Ảnh chụp 33 tệp, khoảng 14 MB trong git. Nếu cần nhẹ hơn thì nén PNG ở lần chốt (Điểm dừng M1-C).

## Quyết định thay chủ shop

Chủ shop vắng mặt, giao toàn quyền trong phạm vi kế hoạch và spec. Mọi quyết định dưới chỉ ở mockup, đổi được trước khi code (Task M1-48, M1-50…56).

| # | Chỗ chưa rõ | Đã chọn | Vì sao; phương án khác |
|---|---|---|---|
| D1 | Khung điều hướng CRM (kế hoạch, DESIGN.md chưa có) | Menu trái 240 px từ 1000 px, bốn nhóm (Sản phẩm, Kho, Hệ thống, Quản trị); dưới 1000 px thanh trên 56 px và menu trượt từ trái; mục đang mở có vạch đỏ bên trái | Theo DESIGN.md: điểm gãy 1000, menu điện thoại trượt từ trái, trang đang mở đánh dấu vạch đỏ. Khác: dải menu ngang như website (hết chỗ khi thêm M2–M7) |
| D2 | Định dạng mã phiếu kho, mã phiên kiểm (kế hoạch chỉ có cột `code`) | `PN-` nhập, `PX-` xuất, `PL-` chuyển hàng lỗi, `XL-` xuất hàng lỗi, `DC-` điều chỉnh + 6 số; kiểm kho `KK-` + 4 số | Dễ đọc cho nhân viên kho. Task M1-29, M1-35 chốt khi làm; đổi chỉ là chữ |
| D3 | Cỡ nút trên laptop | Cao 40 px (điện thoại 44 px) | DESIGN.md: ≥ 32 px trên laptop, ≥ 44 px trên điện thoại; `.btn` 48 px của website dành cho khách mua; màn nội bộ dày dữ liệu. Khác: 48 px như website |
| D4 | Màu cho cảnh báo thường (DESIGN.md không có vàng, cam) | Thường: nền `--soft`, chữ mực; gấp, lỗi: đỏ `--buy`; mọi trạng thái kèm chữ ("GẤP", "Thường", "NGỪNG 3 PHÚT") | Chỉ token có sẵn; không phân biệt chỉ bằng màu (Task M1-53). Khác: xin chủ shop một màu cảnh báo mới |
| D5 | Bảng trên điện thoại | Dưới 760 px mỗi dòng thành một thẻ có nhãn cột; tồn kho dùng lưới 4 ô | Không cuộn ngang ở 390; bốn ngăn tồn đọc được trong một khối. Khác: bảng cuộn ngang |
| D6 | Cách xác nhận ghi sổ, áp dụng, khóa, lưu trữ | Hộp xác nhận (laptop), tấm trượt đáy (dưới 1000 px) | DESIGN.md: máy tính bảng, điện thoại dùng tấm trượt đáy. Mỗi hộp xác nhận có dòng riêng ở bảng hành vi: H01 (ghi sổ phiếu), H10 (áp dụng kiểm kho), H14 (áp dụng Excel), H31 (khóa nhân viên), H32 (lưu trữ nhà cung cấp), H33 (lưu trữ vai trò) |
| D7 | Danh sách 40 quyền (kế hoạch đòi 40; M0–M1 mới có 22) | 22 quyền thật + 18 quyền ví dụ của mốc sau, gắn nhãn "mốc sau" | Thử độ dài đúng kế hoạch mà không giả là quyền thật. Tên thật lấy từ `listPermissions` |
| D8 | Kiểm kho trên laptop | Cùng một cột rộng 640 px như điện thoại | Q23: thiết kế cho điện thoại trước; một bố cục cho cả hai |
| D9 | Thời gian tự lưu số đếm hiện cho nhân viên | "khi ngừng quét chưa tới 1 giây" | `useBarcodeScan` của kế hoạch: 800 ms; câu dễ hiểu hơn con số |
| D10 | Công cụ chụp ảnh | Playwright có sẵn trong bộ đệm npx của máy, không thêm vào repo, không ghi `docs/phu-thuoc.md` | Không phải phụ thuộc của CRM (chỉ chạy tay để chụp mockup). Khác: dùng Playwright của `console/` khi Task M0-17 có |

## Reviewer

Task chỉ đổi tài liệu (quy trình mục 4.2 bước 6 cho bỏ qua reviewer); vẫn chạy agent `reviewer` trên `git diff --cached` để có một lượt đọc độc lập.

- **Lượt 1: CẦN SỬA**, 5 phát hiện, đã sửa cả 5: (1) màn Nhân viên khóa nhầm vai trò Quản lý với người sửa là Quản lý (luật M1-26 chỉ đòi quyền của vai trò được gán); (2) ảnh đang xử lý vẽ như đã gắn vào mẫu (có vị trí, nút dời, làm ảnh bìa) trong khi `AttachImage` đòi `ready` → thành mục "Đang tải lên"; (3) dòng "tệp nhập Excel bị mất" không thể xảy ra (`file.verify` chỉ duyệt tệp `ready`) → bỏ; (4) bảng hành vi thiếu dòng cho khóa, lưu trữ nhà cung cấp, lưu trữ vai trò, hủy phiên kiểm, ảnh đang tải lên; nút "Chép thay đổi của tôi ra trước" không có trong kế hoạch → thêm H31–H35, bỏ nút, sửa D6; (5) câu `version_conflict` có tên và giờ người sửa mà lỗi 409 không trả → bỏ. Ghi chú không bắt buộc đã làm: thêm dòng "Kho phụ" ở trạng thái C, Kế toán chỉ phạm vi toàn shop, bỏ lý do `source_missing` (chỉ ở đường ảnh riêng tư), thêm phát hiện 7 (tổng có thể bán che biến thể âm); tên nhà cung cấp đo lại đúng 200 ký tự.
- **Lượt 2: ĐẠT**, không phát hiện mới. Ghi chú không bắt buộc: chữ của H18 ("báo lý do trên ô ảnh") → đã đổi thành "ở dòng Đang tải lên" (chỉ chữ trong `index.html`, ảnh `index-*.png` chụp lại).

Kết luận: ĐẠT
