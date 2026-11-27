# M6 — tiêu chí nghiệm thu (Báo cáo vận hành)

Task 223 (M6-00), 27/11/2026. Nguồn: kế hoạch `docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md`, gồm dòng "Xong
M6 khi", "Bảng phủ ca kiểm của M6" và "Ngoài phạm vi M6", cùng spec 2.1 (`08-roadmap.md` hàng M6,
`11-kich-ban-kiem-thu.md` nhóm "Báo cáo" và PQ06).

**Cách đọc bảng:**

- Mỗi ca viết bằng lời nghiệp vụ: người dùng làm gì, thấy gì.
- Cột "Kiểm bằng" trỏ tới tệp test của task sẽ viết nó (số toàn cục, kèm số trong mốc). Tên hàm test cụ thể do task
  đó đặt. Khi đóng mốc (Task 244), `docs/m6/nghiem-thu.md` chép tên hàm thật và kết quả.
- "Máy" là test tự động. "Người" là buổi nhân viên chạy thử trên staging (Task 244 bước 5).

**Số kịch bản chín đơn** (Task 227 dựng, mọi task sau dùng chung): một ngày, Page P1 và P2.

- Lên đơn 8 (O7 còn nháp nên không tính); hủy 1 (O6).
- Doanh số tiền hàng của đơn lên chưa hủy: 1.910.000 đ trên 7 đơn; giá trị trung bình 272.857 đ.
- Phí ship thu khách: 120.000 đ.
- Thành công 2 (O1, O9), doanh số 750.000 đ; hoàn 1 (O2), 250.000 đ; khách trả 1 cái, 150.000 đ.
- Chờ xác nhận 1, chờ gửi hãng 1, đang yêu cầu hủy 1.
- Người chỉ có quyền Page P1 thấy 4 đơn lên, 1.150.000 đ, và không thấy đơn web, đơn nhập tay.

## Quy ước chung mọi báo cáo phải giữ

| # | Tiêu chí | Kiểm bằng |
|---|---|---|
| C1 | Ngày tính theo giờ Việt Nam (UTC+7). Đơn lên lúc 23:59:59 thuộc hôm đó; lúc 00:00:00 thuộc hôm sau | Task 227 (M6-04) `period_test.go`, `orders_dynsql_test.go` |
| C2 | Nháp chưa chốt không vào số nào. "Doanh số" là tiền hàng sau giảm giá (chưa gồm ship), kèm chú thích "chưa phải doanh thu kế toán" | Task 227 `orders_dynsql_test.go`; Task 229 (M6-06) `dashboard_test.go` |
| C3 | Mỗi màn ghi rõ loại ngày đang lọc (ngày lên đơn / ngày hoàn tất / ngày tạo vận đơn) và giờ cập nhật | Task 239–240 (M6-16, 17), test thành phần vitest; mockup Task 224 |
| C4 | Một đơn chỉ đếm một lần, dù có hai vận đơn, nhiều dòng hàng hay quay trạng thái nhiều lần | Task 227 `orders_dynsql_test.go`; Task 230 (M6-07) `orders_property_test.go` |
| C5 | Nhân viên chỉ có quyền ở Page P1 chỉ thấy đơn của P1. Đơn web, đơn nhập tay chỉ người có quyền toàn shop thấy. Không có quyền xem báo cáo thì không thấy gì | Task 227 `scope_test.go`, `scope_property_test.go`; Task 230 `orders_test.go`; Task 237 (M6-14) `exports_test.go` |
| C6 | Tỷ lệ có mẫu số 0 hiện "—", không hiện 0% | Task 230 `orders_property_test.go`; Task 231 (M6-08) `carriers_test.go` |
| C7 | Báo cáo chỉ có số của shop mình (người bán `own`) và tenant mình; không lẫn người bán khác, shop khác | Task 227 `orders_dynsql_test.go`; Task 235 (M6-12) `runs_test.go`; Task 237 `handlers_reports_file_test.go` (404 tenant khác) |
| C8 | Báo cáo (trừ tệp danh sách khách) không chứa SĐT, địa chỉ, tên khách. Không con số nào của báo cáo lọt vào log hay nhật ký | Task 237 `exports_test.go` (bắt log); Task 238 (M6-15) `customers_export_test.go` |

## Sáu ca của mốc

| Ca | Tiêu chí bằng lời nghiệp vụ | Kiểm bằng (máy) | Kiểm bằng (người) |
|---|---|---|---|
| **BC01** Tổng quan trong ngày | Mở "Tổng quan", người xem thấy cho hôm nay: số đơn lên, doanh số, giá trị trung bình, số đơn chờ xử lý (chờ xác nhận, chờ gửi hãng, đang yêu cầu hủy), số theo kênh. Mỗi số **bằng đúng** số đơn của danh sách đơn khi lọc cùng ngày, cùng trạng thái. Màn tự tải lại mỗi 60 giây và hiện "Cập nhật lúc HH:MM:SS" | Task 229 (M6-06) `dashboard_test.go`, `dashboard_dynsql_test.go` (số kịch bản, so câu đếm độc lập, so danh sách chi tiết), `handlers_reports_dashboard_test.go`; Task 242 (M6-19) `tests/e2e/reports_golden_test.go` (so `GET /api/v1/orders`) | Task 239 (M6-16) màn; Task 244 bước 5: nhân viên so ba số với danh sách đơn |
| **BC02** (AT25) Báo cáo đơn và danh sách chi tiết | Xem đơn theo trạng thái, nguồn, nhân viên, kênh, Page, hãng, ngày. **Tổng của bảng = tổng các ô = số dòng của danh sách chi tiết** khi bấm vào ô, với mọi bộ lọc. Nhóm thiếu dữ liệu hiện "Không xác định", không tự gán vào Facebook. Giá trị trung bình không chia cho 0. Bán chạy theo SKU và theo mẫu; báo cáo hãng có tỷ lệ thành công = thành công ÷ (thành công + hoàn) của vận đơn đã kết thúc | Task 230 (M6-07) `orders_test.go`, `orders_property_test.go` (rapid: tổng = các ô = chi tiết), `drilldown_test.go`, `filters_test.go`; Task 231 (M6-08) `carriers_test.go`; Task 232 (M6-09) `bestsellers_test.go`; Task 242 `reports_golden_test.go` | Task 240 (M6-17); Task 244 bước 5: bấm một ô, đếm dòng |
| **BC03** Tồn một ngày đã qua | Chọn một ngày đã qua, thấy tồn của ngày đó lấy từ bản chụp lúc 24:00 giờ Việt Nam, không tính lại từ đầu. Bản chụp đúng bằng tổng sổ tồn tới mốc, kể cả khi lịch chụp chạy trễ, chạy hai lần cùng lúc, hoặc worker ngừng vài ngày (ngày thiếu được bù, tối đa 7 ngày mỗi lần). Xuất nhập tồn trong kỳ khớp sổ | Task 233 (M6-10) `snapshot_test.go`, `snapshot_property_test.go`; Task 234 (M6-11) `stock_test.go`, `movements_test.go` | Task 241 (M6-18) màn Tồn kho, Xuất nhập tồn |
| **BC04** Xuất Excel chạy nền | Bấm "Xuất Excel": máy nhận yêu cầu ngay, chạy nền, báo khi xong; người tạo tải về trong 7 ngày. Đường dẫn tải hết hạn sau 5 phút. Tệp mở được bằng Excel, Google Sheets, LibreOffice. Không ô nào là công thức: ô gõ `=…` hiện nguyên chữ. Quá 200.000 dòng thì tệp có dòng cuối "Đã cắt ở 200.000 dòng". Người khác không tải được (404); sau hạn → 410; mỗi lần tải ghi nhật ký. Bấm 50 lần cùng lúc chỉ ra một lần chạy; tối đa 5 lần đang chờ mỗi người. Xuất được danh sách sản phẩm (cột theo mẫu nhập, không có giá vốn) | Task 235 (M6-12) `runs_test.go`, `runs_property_test.go`, `cleanup_test.go`, `handlers_reports_runs_test.go`; Task 236 (M6-13) `internal/platform/xlsx/{writer_test.go, fuzz_test.go, property_test.go}`, `tests/arch/xlsx_test.go`; Task 237 (M6-14) `exports_test.go`, `exports_retry_test.go`, `columns_test.go`, `products_export_test.go`, `handlers_reports_file_test.go`; Task 242 `reports_golden_test.go` | Task 241 (M6-18); Task 244 bước 5: xuất, chờ thông báo, tải, mở tệp, thử ô `=` |
| **BC05** Báo cáo nặng giữa giờ bán | Báo cáo chạy giữa ca live không làm chậm lên đơn: mỗi tiến trình `api` dùng tối đa 2 kết nối cho báo cáo; báo cáo quá 30 giây tự chuyển chạy nền; chạy nền quá 60 giây báo "Báo cáo quá nặng, thu hẹp khoảng thời gian". p95 tạo đơn dưới 400 ms và không quá 1,25 lần lúc không có báo cáo | Task 228 (M6-05) `interactive_test.go` (2 chỗ, 30 s, chỉ đọc, không chạy song song trong PostgreSQL); Task 235 `runs_test.go` (chỗ của worker, `too_heavy`); Task 243 (M6-20) `tests/perf/reports_perf_test.go`, `loadtest/k6/reports.js` | — (chỉ máy) |
| **PQ06** Xuất danh sách khách | Chỉ người có quyền `customer.export` ở phạm vi toàn shop thấy nút và xuất được. Quyền theo Page thì bị từ chối (403). Tệp có mã khách, tên, SĐT chính dạng `0…`, số đơn, thành công, hoàn, tỷ lệ hoàn, tổng chi, ngày mua cuối, nhãn, ngày tạo; không có địa chỉ; mỗi khách đã gộp ra một dòng. Ô bắt đầu bằng `=` bị vô hiệu. Ba bước (yêu cầu, xong, tải) đều ghi nhật ký; nhật ký, log và tham số lưu lại không chứa SĐT hay tên. Tệp hết hạn sau 24 giờ. Bộ lọc không nhận chữ tự do | Task 225 (M6-02) `permissions_test.go`, `permissions_doc_test.go`, `roles_test.go`; Task 236 `writer_test.go`; Task 238 (M6-15) `customers_export_test.go`, `customers_export_property_test.go`, `handlers_reports_customers_test.go` | Task 244 bước 5: người không có quyền không thấy nút; người có quyền xuất được; chủ shop xem dòng nhật ký |

Ca của mốc khác mà M6 chạy lại với các route mới: PQ02 (nhật ký không có SĐT), PQ03 (nhân viên chưa được giao không
thấy gì), PQ04 (quyền Page A không xem được Page B), PQ08 (không đọc được dữ liệu shop khác). Kiểm bằng: Task 225
`routes_test.go`; Task 227 `scope_test.go`, `scope_property_test.go`; Task 235, 237, 238 (404 với tenant khác).

## Xong M6 khi

| # | Điều kiện | Kiểm bằng |
|---|---|---|
| X1 | Kịch bản đơn thử đi qua API thật của M2, M3. Mọi con số trên năm báo cáo khớp số đếm trực tiếp trên bảng gốc và khớp danh sách chi tiết khi bấm vào | Task 242 (M6-19) `tests/e2e/reports_golden_test.go` |
| X2 | BC01, BC02, BC03, BC05, PQ06 đạt phần máy; BC01, BC04 đạt phần nhân viên chạy thử trên staging | Các test ở bảng trên; Task 244 (M6-21) bước 5, ghi ở `docs/m6/nghiem-thu.md` |
| X3 | Mỗi báo cáo 31 ngày, trên dữ liệu gấp 10 lần quy mô hiện tại, trả lời dưới 2 giây; không `Seq Scan` trên bảng lớn bằng role ứng dụng | Task 243 (M6-20) `tests/perf/reports_perf_test.go`; `EXPLAIN` bằng `testkit.ExplainAsApp` + `RequireNoSeqScan` ở Task 227–234 |
| X4 | Chạy báo cáo nặng giữa lúc đang lên đơn: p95 tạo đơn dưới 400 ms và không quá 1,25 lần lúc không có báo cáo | Task 243 `loadtest/k6/reports.js`, `tests/perf/reports_perf_test.go` |
| X5 | Tệp Excel mở được bằng Excel, Google Sheets, LibreOffice; không ô nào là công thức; xuất được danh sách sản phẩm | Task 236 `writer_test.go`, `fuzz_test.go`, `property_test.go`; Task 237 `products_export_test.go`; Task 244 bước 5 (mở bằng tay) |
| X6 | Vùng rủi ro cao của M6 (`scope.go`, `runs.go`, `customers_export*.go`, `internal/platform/xlsx`) qua mutation testing và review độc lập | Task 244 (M6-21) |

Điều kiện chờ chủ shop trước khi đưa lên staging:

- OK riêng cho bốn thay đổi hành vi (a)–(d) ở `docs/m6/mockup/ghi-chu.md`.
- Chọn người được `customer.export`.
- Hướng xử lý hạn tự xóa của tệp xuất trên kho (xem `docs/m6/doi-chieu-gia-dinh.md`, G5).

## Ngoài phạm vi M6

- Báo cáo khách hàng (mới/quay lại, theo nhóm), lãi gộp (chờ Q05), khuyến mại, Zalo, SMS, báo cáo Vpage (tốc độ phản
  hồi, hội thoại theo nhân viên): P2.
- Khoảng 80 báo cáo chi tiết của Nhanh: P3. Mỗi báo cáo thêm là một `Def` mới trên view.
- Bảng điểm từng hãng theo tỉnh, gợi ý hãng: P2.
- Báo cáo bán lẻ, bán sỉ, sổ kế toán: bỏ hoặc P3.
- Số "Đã bán" cho website: thuộc M5, không phải báo cáo của nhân viên.
- Nhập danh sách khách từ Excel: phần nhập không thuộc M6.
- Báo cáo theo người bán, công nợ người bán: khi mở sàn. Hôm nay chỉ lọc ngầm theo người bán `own`.
- Thư viện biểu đồ, báo cáo gửi email theo lịch, bản sao database chỉ đọc cho báo cáo, bảng tổng hợp: chưa cần
  (ADR-20).
