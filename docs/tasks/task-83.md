# Task 83 — M2-26: Mockup các màn M2

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 83 = M2-26, Điểm dừng B). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu (không có tệp nào trong `CODE_PATHS`).

Căn cứ: kế hoạch M2 mục "Task 83 (M2-26)", Interfaces của Task M2-01 (quyền), M2-02 (55 operation, mã lỗi), M2-03 (khách), M2-05 (đơn, phiếu trả, cài đặt, nguồn đơn), M2-06 (mã, lý do từ chối), M2-08 (16 nhãn, `EditScope`), M2-09…M2-25; quyết định Q2, Q3, Q7, Q8, Q14, Q18, Q23, Q25, Q26, Q27, Q-T57-4, Q-T57-8, Q-X8; spec `11-kich-ban-kiem-thu.md` (DH01, DH02, DH04, DH06, DH09, DH10, DH12, DH13, DH14, DH16, TD01, KM01); hệ thiết kế đã duyệt của M1 (`docs/mockups/m1/m1.css`, gốc `apps/storefront-giftstatus/DESIGN.md`) và khung console thật (`console/src/styles.css`, `console/src/app/nav.ts`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng M2 là của Task M2-02, chưa gộp vào `main`). Mockup chỉ vẽ dữ liệu của các operation đã đặt tên ở bảng Task M2-02: `createOrder`, `getOrder`, `listOrders`, `updateOrder`, `confirmOrder`, `approveOrderDiscount`, `setOrderPending`, `cancelOrder`, `getOrderTimeline`, `addOrderNote`, `checkOrderDuplicates`, `listOrderDuplicates`, `resolveOrderDuplicate`, `createReturn`, `getReturn`, `receiveReturn`, `closeReturn`, `cancelReturn`, `listCustomers`, `createCustomer`, `getCustomer`, `updateCustomer`, `addCustomerIdentity`, `removeCustomerIdentity`, `addCustomerAddress`, `updateCustomerAddress`, `listAdminUnits`, `listCustomerMergeCandidates`, `mergeCustomer`, `undoCustomerMerge`, `getCustomerTimeline`, `addCustomerNote`, `listCustomerTags`, `createCustomerTag`, `updateCustomerTag`, `addCustomerTag`, `removeCustomerTag`, `getCustomerTagRule`, `putCustomerTagRule`, `getCustomerImportTemplate`, `startCustomerImport`, `getCustomerImport`, `listCoupons`, `createCoupon`, `getCoupon`, `updateCoupon`, `createCouponBatch`, `getCouponBatch`, `listCouponRedemptions`, `checkCoupon`, `listTrafficSources`, `createTrafficSource`, `updateTrafficSource`, `getOrderSettings`, `updateOrderSettings`; của M1: `searchVariants`, `createUpload`, `completeUpload`. Chỗ mockup cần trường chưa có trong kế hoạch ghi ở "Việc phát hiện thêm".
- Chỗ dùng trên màn hình: console M1 đã có (`console/src/screens/{products,inventory,staff,roles}`), chưa có màn M2. Mockup là đầu vào của Task M2-27 (Lên đơn), M2-28 (Đơn), M2-29 (Khách), M2-30 (Mã giảm giá, Cài đặt đơn). Nhóm menu mới ("Đơn hàng", "Khách hàng", "Khuyến mãi") sẽ đổi `NAV_GROUPS` của `console/src/app/nav.ts` ở các task đó, không ở task này.
- Câu sqlc, bảng, pha migration: không.
- Tệp sẽ đổi: tạo `docs/mockups/m2/` gồm 9 trang của kế hoạch (`len-don`, `don-danh-sach`, `don-chi-tiet`, `don-trung`, `khach-danh-sach`, `khach-ho-so`, `khach-gop`, `ma-giam-gia`, `cai-dat-don`), `index.html` (trang xem ảnh và bảng hành vi), `README.md` (bảng màn → ca kiểm, hành vi cần OK, kết luận duyệt), `m2.css` (chỉ thành phần mới, nạp sau `../m1/m1.css`), `khung.js` (menu của M2), `soat.mjs` (công cụ chụp và tự soát, chép từ M1 và mở rộng), `png/*`; `docs/tasks/task-83.md`. Không sửa gì trong `docs/mockups/m1/`.
- Test sẽ chạy: không có test mã. "Test" của task là `docs/mockups/m2/soat.mjs` (Playwright Chromium trong bộ đệm npx, như M1): mỗi trang × mỗi cỡ (1512×767, 390×844, 360×800 cho màn dễ tràn) kiểm (1) không cuộn ngang; (2) không chữ bị cắt ngoài chỗ cố ý có dấu `…`; (3) mục chạm ≥ 44 px dưới 1000 px, ≥ 32 px từ 1000 px; (4) màu, cỡ chữ, bo góc thuộc tập có sẵn của DESIGN.md (cùng tập của M1); (5) mọi `<select>` trong `.select-wrap`, chữ lựa chọn không bị cắt; (6) không luật `:hover` nào làm hiện/ẩn (soát cả `m1.css` và `m2.css`); thêm cho M2: (7) mọi số tiền trong phần tử `.money` đúng dạng `690.000 ₫`; (8) mỗi trang có đủ các mục "TRẠNG THÁI" mà kế hoạch đòi (danh sách trong công cụ). Viết công cụ trước, chạy thấy đỏ (chưa có trang), rồi vẽ tới khi xanh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go. Mockup có sẵn: `docs/mockups/m1/` (Task 49, đã duyệt theo Q-X8): `m1.css` (token, khung, nút, ô nhập, bảng → thẻ dưới 760 px, hộp thoại → tấm trượt đáy, toast, chip, thanh bước, vùng thả tệp), `khung.js`, `soat.mjs`, `index.html` (khuôn trang so sánh và bảng hành vi). Console thật (`console/src/styles.css`) chép đúng token đó.
- Dùng lại / viết mới vì: dùng lại nguyên `../m1/m1.css` bằng liên kết (không chép) để màn M2 cùng thành phần với M1 và console; `m2.css` chỉ thêm thành phần M1 chưa có (bảng dòng đơn, thanh nhãn có số đếm, hai cột so sánh, danh sách gợi ý, dòng thời gian, phím tắt), chỉ bằng biến của `m1.css`. `khung.js` viết bản M2 vì menu khác (thêm ba nhóm); giữ đúng cấu trúc HTML của bản M1. `soat.mjs` chép từ M1 rồi đổi danh sách trang, thêm kiểm (7), (8) và soát cả hai tệp CSS; không sửa bản M1 để không đổi bằng chứng của Task 49. Khuôn `index.html` chép từ M1.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã. Các câu dưới làm căn cứ cho **nội dung** mockup (màn hình phải nói đúng luật API sẽ làm).

- 5.1–5.12: không áp dụng cho mã của task (không mã, bảng, route, thư viện mới trong repo; Playwright chạy từ bộ đệm npx của máy, không thêm phụ thuộc). 5.9 (mọi đường ghi có khóa chống trùng): Lên đơn vẽ ca mất mạng rồi gửi lại cùng khóa (`useWriteAction`, Q-T57-4: khóa của thao tác cũng là khóa nghiệp vụ), nút ghi chuyển "Đang lưu…" và khóa trong lúc chờ.
- 6.1 (hai người cùng sửa): Chi tiết đơn có câu "Đơn đã thay đổi, tải lại để xem bản mới" giữ phần đang gõ (`version_conflict`); Cài đặt đơn, Mã giảm giá, Hồ sơ khách có cùng dạng câu.
- 6.2 (bấm hai lần): Tạo đơn, Hủy đơn, Gộp khách, Hoàn tác gộp, Tạo đợt mã, Nhận hàng trả đều có nút khóa khi đang gửi; Gộp và Hủy có bước xác nhận.
- 6.3–6.11: không áp dụng: không có mã chạy. Phần nội dung liên quan: hết hàng giữa chừng (409 `out_of_stock` kèm `available`), mã hết lượt (`exhausted`), cả lần sửa bị từ chối khi kho không đủ (DH10) vẽ như lỗi làm lùi trọn vẹn.
- 6.12 (dữ liệu cá nhân): SĐT, địa chỉ chỉ hiện trên màn của người có `customer.read`/`order.read` (màn nội bộ, kế hoạch Task M2-29); lịch sử thao tác chỉ ghi tên trường ("Địa chỉ giao: đã đổi"), không giá trị; kết quả nhập Excel hiện số dòng và cột, không giá trị ô; danh sách lượt dùng mã không có SĐT; mockup dùng tên và số giả.
- 6.13 (quyền): nút theo `EditScope` và quyền trong `/api/v1/me`; nút không được bấm hiện mờ **kèm lý do bằng chữ** (không ẩn khi người dùng cần biết vì sao, không chỉ hiện khi rê chuột); "Nhận hàng trả" ẩn khi không có `order.return.receive` (kế hoạch Task M2-28); nhãn theo quy tắc không có nút gỡ. Kiểm thật vẫn ở API.

## Bằng chứng

Lệnh tự soát (gọi tắt `SOAT`): `PW_DIR=~/.npm/_npx/5e2e484947874241/node_modules/playwright node docs/mockups/m2/soat.mjs` (Playwright 1.60.0 của bộ đệm npx, như Task 49).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Công cụ soát chạy đỏ trước khi có trang | `soat.mjs` | `SOAT --no-shot` (lúc chưa có trang) | `m2.css: chưa có`, `len-don: chưa có len-don.html` … `SOÁT MOCKUP: 11 lỗi`, exit 1 |
| Công cụ bắt thiếu trạng thái và tiền sai dạng (kiểm chính công cụ) | sửa tạm `don-chi-tiet.html`: bỏ `data-ca="hop-huy"`, đổi một số tiền thành `13160000đ` | `SOAT --no-shot don-chi-tiet` | `thiếu trạng thái "hop-huy"`, `tiền sai dạng "13160000đ"`, `SOÁT MOCKUP: 6 lỗi`; trả tệp về bản cũ → `SOÁT MOCKUP OK: 1 trang, 3 ảnh` |
| Công cụ bắt chữ tràn trái / tràn khỏi ô bảng (kiểm thêm sau khi soát mắt thấy lỗi mà công cụ M1 không bắt) | `don-danh-sach` bản lỗi (tên công ty dài trong thẻ dòng ở 390) | `SOAT --no-shot don-danh-sach` | `tràn trái 82px: span.ellip "Công ty TNHH…"`, `tràn khỏi ô bảng 115px`, `SOÁT MOCKUP: 4 lỗi`; sửa trang → OK |
| 9 màn của kế hoạch + `index.html`, chụp 1512×767 và 390×844, thêm 360 cho 8 màn dễ tràn | `soat.mjs` | `SOAT` | `SOÁT MOCKUP OK: 10 trang, 28 ảnh` (lần chạy cuối, sau sửa theo reviewer lượt 1); `ls docs/mockups/m2/png` → 28 tệp: `cai-dat-don(2) don-chi-tiet(3) don-danh-sach(3) don-trung(3) index(2) khach-danh-sach(3) khach-gop(3) khach-ho-so(3) len-don(3) ma-giam-gia(3)`, 13 MB |
| Đủ trạng thái kế hoạch đòi cho từng màn (Lên đơn: 0 dòng, 20 dòng, tên 120 ký tự, hết hàng 409, chờ duyệt giảm giá, mã hết lượt, mất mạng gửi lại cùng khóa, cảnh báo đơn trùng; Chi tiết đơn: ba hộp thoại, báo đơn đã thay đổi; …) | `soat.mjs` kiểm (8), danh sách `CASES` | `SOAT` | 0 lỗi "thiếu trạng thái"; tên 120 ký tự đo bằng `python3 len()` = 120 |
| Không cuộn ngang, không tràn, mục chạm đủ, chỉ token có sẵn, ô chọn đúng, không gì chỉ hiện khi rê chuột, tiền `690.000 ₫` | `soat.mjs` (1)–(8) | `SOAT` | 0 lỗi lần chạy cuối |
| Soát bằng mắt ảnh 1512 và 390 (Step 1–2) | đọc ảnh | — | Đã sửa: cột tên sản phẩm bị ép hẹp ở 1512 (thu cột số); tên sản phẩm lệch giữa trong thẻ dòng ở 390; tên công ty dài tràn trái ở 390; nhãn ô "Hoàn tác" lặp hai lần ở 390; chữ và số tiền trong một ô bị tách hai đầu ở 390 (Mã giảm giá, phiếu đổi) |
| Số tiền khớp nhau (Step 2) | tính tay | — | Lên đơn A: 2.020.000 − 100.000 − 150.000 (10% của 1.920.000, chặn 150.000) + 30.000 = 1.800.000; Chi tiết đơn A: 13.160.000 − 1.000.000 − 1.100.000 = 11.060.000, giảm tay 2.100.000 / 13.160.000 = 15,96% → 16% (làm tròn lên như `ManualDiscountBP`); 64 món; 20 dòng: tạm tính 14.520.000, giảm dòng 290.000. Đã sửa hai chỗ lệch (tổng 20 dòng; tổng DH0001235 ở danh sách). Theo reviewer: bộ số DH0001102 làm lại theo `RefundFor` (DAM0123-BE-M mua 3, giá sau giảm 1.860.000: phiếu mở 2 món 1.240.000, lần này 1 món cuối 620.000 = 1.860.000 − 1.240.000; phiếu đổi TUI0090-NAU 290.000) |
| Mỗi nút gọi một operation có quyền phù hợp (Step 2) | đối chiếu bảng Task M2-02 | — | Xem mục Ảnh hưởng (danh sách operation); nút không có operation riêng ghi ở "Việc phát hiện thêm" (1), (2), (4) |
| Bảng hành vi, mỗi hành vi một dòng để chủ shop OK riêng; bảng màn → ca kiểm → trạng thái | `README.md`, `index.html` | `grep -c '^| HV' docs/mockups/m2/README.md` | `38` (HV01–HV38, cột OK = "chưa"); 10 ca DH01, DH02, DH04, DH06, DH09, DH13, DH14, DH16, TD01, KM01 đều có chỗ vẽ |
| `make ci` một lần trước khi stage (task chỉ đổi tài liệu) | `make ci` | `CRM_PG_PORT=5600 make ci` | **Không chạy hết**: bị dừng ở giới hạn 10 phút của tác vụ nền khi đang chạy `go test -race`. Phần đã chạy: build, vet, `golangci-lint` `0 issues.`, `make gen` không làm lệch tệp sinh (`git status` sạch ngoài tệp task), 36 gói `ok`, 0 `FAIL`. Không chạy lại vì diff không chạm mã |
| Gộp `main` trước khi stage | — | `git merge main` | `Already up to date.` (main tại `abd3410`) |
| Chỉ đổi tài liệu, không chạm `CODE_PATHS`, không sửa `docs/mockups/m1/` | — | `git diff --cached --name-only` | chỉ `docs/mockups/m2/**` và `docs/tasks/task-83.md` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm tệp ngoài danh sách của kế hoạch: `index.html` (trang xem ảnh, như M1), `m2.css`, `khung.js`, `soat.mjs` (công cụ chụp và tự soát cho Step 1–2). Đều trong `docs/mockups/m2/`, không phải mã CRM.
- Ảnh 360 cho 8 màn (kế hoạch: "thêm 360 cho màn dễ tràn chữ"): Lên đơn, Danh sách đơn, Chi tiết đơn, Đơn trùng, Khách hàng, Hồ sơ khách, Gộp khách, Mã giảm giá.
- Thêm trạng thái "Quản lý nhãn và quy tắc nhãn" vào màn Khách hàng (kế hoạch Task 83 không liệt kê nhưng Task M2-29 làm `TagsEditor`, `TagRuleEditor`; không vẽ thì Task M2-29 không có màn đã chốt) — theo reviewer lượt 1.
- Step 3 (điều phối viên chốt từng màn, ghi ngày, người chốt) không phải việc của phiên làm task: `README.md` có bảng "Kết luận duyệt" để trống "Chờ điều phối viên duyệt", như Task 49. Step 5 commit lần hai (bản đã chốt) thuộc lượt chốt đó.
- Kế hoạch ghi "Step R: không cần reviewer mã"; vẫn chạy agent `reviewer` trên phần đã stage theo yêu cầu của phiên.
- Agent `design-critic` không có trong phiên này; soát bằng `soat.mjs` và soát mắt.
- Công cụ soát thêm hai kiểm so với bản M1 (tràn trái, tràn khỏi ô bảng) sau khi soát mắt thấy lỗi bản M1 không bắt; bản M1 không sửa.

## Việc phát hiện thêm

Không tự sửa kế hoạch hay hợp đồng; ghi để task sau xử lý.

1. **Số đếm trên thanh nhãn của Danh sách đơn** (kế hoạch: "thanh nhãn có số đếm"): `listOrders` (Task M2-24) chỉ có danh sách theo con trỏ, không có số đếm theo nhãn. Cần một operation đọc mới (ví dụ `countOrdersByLabel`, `order.read`, chỉ nhãn đang mở) hoặc trường trong trả lời. Task M2-24/M2-28 quyết; mockup chỉ đếm nhãn đơn đang mở (Nháp, Mới, Chờ duyệt, Chờ khách, Hết hàng, Đã xác nhận, Đang yêu cầu hủy).
2. **Danh sách lần gộp của một khách** (kế hoạch Task M2-29: "danh sách lần gộp có nút Hoàn tác"): bảng Task M2-02 có `undoCustomerMerge` nhưng không có operation liệt kê `customer_merges` của nhóm. Cần thêm trường `merges` vào `getCustomer` hoặc operation `listCustomerMerges` (`customer.read`). Task M2-11/M2-29 quyết.
3. **Hàng của `listCustomers`** cần: mã, tên, một SĐT, nhãn, số đơn, đã chi, mua gần nhất, số hồ sơ đã gộp. Kế hoạch Task M2-10 chỉ ghi `SearchHit` (có `Stats`, `Phones`), chưa ghi nhãn và số hồ sơ trong nhóm.
4. **Tải danh sách mã của đợt** (kế hoạch Task M2-30: "tải được danh sách mã"): chưa có operation xuất; `listCoupons?batchId=` theo con trỏ đủ để màn tự dựng tệp, hoặc thêm xuất ở M6 cùng mọi tệp xuất (Q13). Task M2-30 quyết; mockup vẽ nút "Tải danh sách mã (.xlsx)".
5. **Tên người phụ trách, người làm** trong danh sách đơn và lịch sử: `OrderRow.CareBy` là id; cần tên hiển thị (`Actor{id, name}` của Task M2-02) trong `OrderRow`.
6. **Giá hiện hành bên cạnh giá lúc đặt** ở Chi tiết đơn (KH11, giúp nhân viên giải thích khi giá đổi): `GetDetail` không có giá hiện hành. Không bắt buộc; Task M2-28 bỏ dòng này nếu không thêm trường.
7. **Đường dẫn màn Gộp khách**: kế hoạch không đặt; mockup dùng `/khach/:id/gop`. Task M2-29 thêm vào `route.tsx`.
8. Câu báo lỗi trong mockup (10 lý do từ chối mã, `out_of_stock`, `merge_not_latest`, `merge_window_expired`, `version_conflict`, `setting_out_of_range`, `code_taken`, `max_uses_below_used`, `query_too_short`) là bản đề xuất cho `console/src/app/errors.ts` (Task M2-27–30).
9. Ảnh chụp 28 tệp, khoảng 13 MB trong git; nén PNG ở lần chốt nếu cần nhẹ hơn (như ghi chú 8 của Task 49).
10. **Tình trạng hàng trả theo dòng, không theo món** (`ReceiveLine{OrderItemID, Condition}`, `UNIQUE (return_id, order_item_id)`): mockup vẽ một tình trạng cho cả dòng; hai món cùng dòng khác tình trạng phải tạo hai phiếu. Nếu shop hay gặp ca này thì Task M2-22 cân nhắc tách số lượng theo tình trạng.
11. **Phần đơn thay thế của phiếu đổi** (`CreateReturnInput.Replacement`: dòng mới, địa chỉ) chưa vẽ trong hộp thoại Trả/đổi; Task M2-28 dùng lại bảng dòng của form Lên đơn trong hộp thoại.
12. **Danh sách phiếu trả cho kho** (`listReturns` có trong hợp đồng) chưa có màn; nhân viên kho hiện chỉ thấy phiếu qua chi tiết đơn. Đề xuất thêm vào màn Đơn (nhãn "Phiếu trả chờ nhận") ở Task M2-28.
13. Câu chữ có số cố định (24 giờ, 10%, 7 ngày) là giá trị mặc định; màn thật đọc từ `getOrderSettings`.

## Quyết định thay chủ shop

Chủ shop vắng mặt, giao toàn quyền trong phạm vi kế hoạch và spec. Mọi quyết định dưới chỉ ở mockup, đổi được trước khi code (Task M2-27–30); mỗi quyết định có ảnh hưởng tới nhân viên đều có dòng riêng trong bảng hành vi để chủ shop OK.

| # | Chỗ chưa rõ | Đã chọn | Vì sao; phương án khác |
|---|---|---|---|
| D1 | Chỗ của ba màn M2 trên menu (`NAV_GROUPS` của M1 có 4 nhóm cố định) | Thêm ba nhóm Đơn hàng, Khách hàng, Khuyến mãi **đứng đầu**; Cài đặt đơn nằm trong nhóm Đơn hàng (HV01) | Lên đơn, tra đơn là việc hằng ngày nhiều nhất. Khác: thêm vào cuối, hoặc đưa Cài đặt đơn vào Quản trị |
| D2 | Lên đơn là trang hay hộp thoại; Gộp khách là trang hay hộp thoại | Cả hai là trang riêng; Hủy, Trả/đổi, Nhận hàng trả, Nhập Excel, Tắt mã là hộp thoại (tấm trượt đáy trên điện thoại, như D6 của M1) | Kế hoạch đặt đường `/don/moi`; gộp cần chỗ cho hai cột. Khác: lên đơn trong ngăn trượt cạnh danh sách |
| D3 | Phím tắt (kế hoạch chỉ nói "phím tắt tìm khách, thêm dòng, chuyển ô, lưu") | F2 tìm khách, F3 thêm dòng, Tab chuyển ô, Ctrl/⌘+S lưu (mới), Ctrl/⌘+Enter lưu và xác nhận (HV03) | Phím F không đụng phím gõ chữ tiếng Việt; Ctrl+S quen tay "lưu". Khác: `/` tìm khách, Alt+N thêm dòng |
| D4 | Nút mặc định của Lên đơn | "Lưu và xác nhận" (HV04) | DH01 mong đợi đơn "Đã xác nhận". Khác: "Lưu (mới)" làm mặc định nếu shop muốn duyệt lại mọi đơn |
| D5 | Số đếm trên thanh nhãn | Chỉ nhãn của đơn đang mở; Đã hủy, Thành công, Đã hoàn không có số (HV14) | Đếm cả lịch sử tốn truy vấn mà không giúp ra việc; API chưa có số đếm (phát hiện 1) |
| D6 | Gợi ý tìm khách có hiện đủ SĐT không | Che giữa số trong gợi ý (`•••• •• 5678`), hiện đủ khi đã chọn khách và ở hồ sơ (HV05) | Màn lên đơn hay được chiếu, quay live; người có `customer.read` vẫn xem đủ ở hồ sơ. Khác: hiện đủ (kế hoạch Task M2-29 cho phép) |
| D7 | Đơn vị nhập ở Cài đặt đơn (máy chủ lưu phút, phần vạn) | Giờ, %, ngày (HV37) | Dễ hiểu cho nhân viên; màn đổi đơn vị. Khác: nhập phút |
| D8 | Nguồn hệ thống "Website" (khóa `web`, M5 tìm theo khóa) | Đổi tên, sắp xếp được; không có nút Tắt (HV38) | Tắt sẽ làm đơn web bị từ chối `traffic_source_inactive` ở M5. Khác: cho tắt kèm cảnh báo |
| D9 | Sửa đơn vẽ ở đâu (kế hoạch M2-26 không có màn Sửa) | Sửa mở lại form Lên đơn điền sẵn, trường bị khóa theo `EditScope` hiện mờ kèm lý do (HV23); không vẽ màn riêng | Một form một cách gõ; `OrderForm` đã định dùng lại. Khác: hộp thoại sửa riêng (`EditOrderDialog` của Task M2-28) |
| D10 | Nút duyệt ở Chi tiết đơn | Một nút "Duyệt giảm giá và xác nhận" cho đơn Mới (đúng `ApproveDiscount`: `new → confirmed`) (HV17) | Khớp hàm của Task M2-19; một bấm. Khác: tách hai nút |
| D11 | Đường của màn Gộp khách | `/khach/:id/gop` | Con của hồ sơ khách; ghi ở phát hiện 7 |
| D12 | Công cụ chụp | Playwright của bộ đệm npx, không thêm vào repo (như D10 của Task 49) | Không phải phụ thuộc của CRM |

## Reviewer

- **Lượt 1: CẦN SỬA**, 6 phát hiện, đã sửa cả 6: (1) ca `merge_not_latest` vẽ ở chỗ máy chủ không chặn (lần gộp sau chỉ dời hồ sơ khác vào cùng hồ sơ giữ lại) → vẽ lại: nhóm KH000231 bị gộp tiếp vào KH001877, nên lần gộp KH003990 vào KH000231 phải chờ; (2) hộp Nhận hàng trả chọn tình trạng theo món trong khi API lưu theo dòng → một tình trạng mỗi dòng, ghi phát hiện 10; (3) số của DH0001102 không theo `RefundFor`, phiếu đổi hoàn 0 ₫, "Đã trả" không khớp → làm lại bộ số (xem Bằng chứng); (4) cặp đơn trùng 1 ghi "cùng SĐT người nhận" mà hai số khác → "Cùng khách"; (5) HV16 nói "không ẩn" trái hình vẽ → viết lại: không áp dụng thì ẩn, bị chặn thì mờ kèm lý do; (6) thiếu màn nhãn và quy tắc nhãn → thêm trạng thái E ở màn Khách hàng, HV35. Ghi chú không bắt buộc đã làm: `updateCustomerAddress` và bốn operation nhãn thêm vào Ảnh hưởng; ngày mua gần nhất thống nhất 18/11; câu sau tạo đơn "có thể trùng" (dấu nghi trùng do việc nền đặt sau); hộp Yêu cầu hủy đủ 5 lý do và ghi chú; gợi ý nhập số lẻ giờ cho mức 10 phút; HV36 nút cần quyền riêng không hiện; phát hiện 11–13 (đơn thay thế, danh sách phiếu trả, số cố định).

- **Lượt 2: ĐẠT**, không phát hiện mới; đối chiếu lại từng sửa của lượt 1 (luật `UndoMerge`, `ReceiveLine` theo dòng, số `RefundFor` của DH0001102, `matched_by`, HV16, màn nhãn khớp `TagCondition`). Ghi chú không bắt buộc: chép kết luận vào đây (đã làm); thêm "E quản lý nhãn" vào bảng màn của README (đã có sẵn ở dòng (5)).

Kết luận: ĐẠT
