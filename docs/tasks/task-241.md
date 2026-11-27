# Task 241 — Console — Tồn kho, Xuất nhập tồn, Tệp xuất và chạy nền, nút xuất (M6-18)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 241, M6-18). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ chạm `console/` (TypeScript). Không sửa Go, openapi, queries, migration, mã sinh.
- Gói phụ thuộc ngược (go list): không áp dụng. Gói console: `src/screens/reports/route.tsx` (thêm hai dòng `/bao-cao/ton-kho`, `/bao-cao/tep-xuat`), `src/screens/khach/CustomerList.tsx` (một nút), `src/screens/products/ProductList.tsx` (một nút), `src/screens/reports/ExportButton.tsx` giữ nguyên.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng; chỉ gọi `getInventoryStock`, `getInventoryMovements`, `listSalesReportRuns`, `listInventoryReportRuns`, `listCustomerExports`, `get*RunFile`, `getCustomerExportFile`, `createInventoryReportRun`, `createCustomerExport`, `listWarehouses`, `listCustomerTags`.
- Chỗ dùng trên màn hình: màn mới Tồn và xuất nhập tồn, Tệp xuất; nút trên Khách hàng (đầu trang, giữa "Nhập Excel" và "Thêm khách") và Sản phẩm (cạnh "Nhập Excel"); chấm số tệp mới ở menu Tệp xuất.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vitest các tệp mới và `CustomerList.test.tsx`, `ProductList.test.tsx`, `screens.test.tsx`, `src/app/errors.test.ts`; `npx tsc --noEmit`; `npx eslint` trên tệp sửa. Không chạy Playwright, không chạy tests/e2e.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chỉ Go, không có phần console); trong console: `useReport`/`useExportRun`/`ExportButton`/`ExportNotice`/`PeriodPicker`/`ReportsPage`/`format.ts`/`period.ts` (Task 239), `useWriteAction` (idempotency), `Dialog` (staff), `NavCount`, `useWindowVirtualizer` như `ProductList`, `usePerms`, `useTags`.
- Dùng lại / viết mới vì: dùng lại toàn bộ cặp trên cho báo cáo tồn (`RunApi` thêm bản `inventoryRunApi`); `useExportRun` nhận thêm `runApi` + hàm tải tệp theo họ (sửa nhỏ `useRunNotice.ts`: hiện cố định `getSalesReportRunFile`, tồn cần `getInventoryReportRunFile`). Viết mới: bảng tồn ảo, bảng xuất nhập tồn, trang Tệp xuất, hộp xuất khách, hộp xuất sản phẩm (không màn nào có sẵn); `rate`/`money` dùng lại từ `format.ts`, không viết hàm tỷ lệ mới.

## Soát thiết kế

Mục 5.1–5.12 (khớp kiến trúc): không áp dụng: task chỉ ở console, không thêm module/gói Go/bảng/hàng đợi/lệnh ra ngoài/thư viện (`@tanstack/react-virtual` đã có trong ADR-44, dùng như ProductList). Dữ liệu qua client sinh + `unwrap` + TanStack Query, khóa truy vấn bắt đầu bằng chủ đề realtime (`report.run_finished`); màn tự đăng ký bằng `route.tsx`.

Mục 6.1–6.13 (đúng khi chạy đồng thời):
- 6.1/6.2 bấm hai lần: ba nút xuất (Xuất Excel của tồn, Xuất danh sách khách, Xuất danh sách sản phẩm) gửi qua `useWriteAction`: một khóa `Idempotency-Key` cho một lần bấm, bấm lại khi đang gửi nhận lại lời hứa; nút khóa khi đang gửi.
- 6.3–6.5 database: không áp dụng. Phía màn: 503 `retry_later` hiện TEXT_BUSY; 429 (quá 5 lần chạy chưa xong) và 403 `export_forbidden` hiện câu riêng của `errors.ts`.
- 6.6–6.8 việc nền lỗi: lần chạy `failed` có `reason` → câu riêng (quá nặng / bận / không đủ quyền / lỗi), `expired` → "Hết hạn"; không tự tạo lần chạy mới. Báo hiệu `report.run_finished` chỉ báo "tải lại", dữ liệu lấy bằng GET; mất báo hiệu thì khung resync 20 s và `refetchInterval` 20 s của trang Tệp xuất là lưới an toàn.
- 6.9 treo lâu: trang Tệp xuất tải lại mỗi 20 s (kế hoạch), không chồng lượt (một truy vấn).
- 6.10 tiền: màn Tồn không có tiền; số lượng là số nguyên máy chủ trả, màn chỉ định dạng, không tính lại "Có thể bán" (hiện đúng `available` của API).
- 6.11 quy luật: không đếm lại; số "N mã hàng" là số dòng đã tải, kèm "còn nữa" khi còn `nextCursor` (không coi là hết, bẫy Task 84).
- 6.12 dữ liệu cá nhân: hộp xuất khách chỉ có ô chọn, ngày, số (không ô chữ tự do) nên tham số không chứa dữ liệu khách; Tệp xuất hiện bộ lọc bằng nhãn tên, không log gì.
- 6.13 quyền: nút xuất khách chỉ khi `customer.export` phạm vi `all`; nút xuất sản phẩm chỉ khi `report.inventory.read` và `report.export` phạm vi `all`; Tệp xuất chia ba nhóm theo quyền của người xem (sales.read / inventory.read / customer.export); máy chủ vẫn kiểm. Người chỉ có một kho thấy dòng "Phạm vi" và ô chọn kho chỉ có kho đó (API trả 403 cho kho ngoài phạm vi).

## Bằng chứng

Miễn điều kiện "M1–M5 đóng" của M6-00 theo ghi chú điều phối 27/11 (chủ shop duyệt kế hoạch M6 sáng 27/11). Backend Task 234, 237, 238 chưa gộp vào nhánh này: màn viết theo hợp đồng OpenAPI đã gộp (Task 225), test dùng API giả.

Lệnh chung: `cd console && npx vitest run <tệp>`; tsc: `npx tsc --noEmit`; lint: `npx eslint src/screens/reports src/screens/khach src/screens/products`; định dạng: `npx prettier --check ...`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Hôm nay hiện "Có thể bán 18", giờ cập nhật, một GET không có ngày | `StockReport.test.tsx` "hôm nay: Có thể bán 18…" | vitest 4 tệp reports | 32/32 qua |
| Chọn 10/11 khi bản chụp đầu 15/11 → "Chưa có bản chụp trước ngày 15/11/2026" | "chọn 10/11 khi bản chụp đầu là 15/11…" | như trên | qua |
| Ngày đã chụp từ bản chụp; ngày tương lai bị chặn, không gọi API | "ngày đã chụp…", "ngày tương lai bị chặn…" | như trên | qua |
| 1.200 dòng chỉ dựng phần thấy, còn nextCursor thì "còn nữa", Tải thêm nối trang | "1.200 dòng: tải 500…" (DOM < 100 dòng; sau Tải thêm "1.000 mã hàng", `cursor=500`) | như trên | qua |
| Tìm theo đầu mã SKU gửi `q`, chọn kho gửi `warehouseId` | "tìm theo đầu mã SKU…" | như trên | qua |
| Người một kho: dòng Phạm vi, không nút xuất; Xuất Excel một POST có khóa | "người chỉ có một kho…", "Xuất Excel gửi đúng một POST…" | như trên | qua |
| Xuất nhập tồn: kỳ, cột, −1, cuối kỳ, còn nữa, xuất `stock_movements` | `MovementsReport.test.tsx` (5 ca) | như trên | qua |
| Nút xuất sản phẩm: ẩn thiếu quyền / phạm vi, một POST có `Idempotency-Key` `report: "products"`, 429 hiện câu riêng | `ProductsExportButton.test.tsx` (5 ca) | như trên | qua |
| Tệp xuất: 3 lần chạy → Tải về chỉ ở dòng Xong; gọi `…/file` rồi mở url; url lạ không mở; Xem cho dạng bảng; nhóm theo quyền; báo hiệu → tải lại | `RunsPage.test.tsx` (12 ca) | như trên | qua |
| Chấm số tệp mới ở menu | `RunsPage.test.tsx` "ExportsNavCount" (3 ca) | như trên | qua |
| Nút Xuất danh sách khách: ẩn khi không có `customer.export` toàn shop, vị trí giữa Nhập Excel và Thêm khách, không ô chữ, đúng `params`, một POST có khóa, ngày sai khóa nút, 403 | `CustomerList.test.tsx` (5 ca mới) | `npx vitest run src/screens/khach/CustomerList.test.tsx` | 13/13 qua |
| Nút trên màn Sản phẩm đúng chỗ/quyền | `ProductList.test.tsx` (2 ca mới) | vitest 6 tệp | 115/115 qua (lần chạy đầu 1 ca lỗi do test viết sai, đã sửa) |
| Không phá màn Tổng quan, nav, câu lỗi | `Dashboard.test.tsx`, `useReport.test.tsx`, `screens.test.tsx`, `errors.test.ts` | như trên | qua |
| `route.test.tsx` ghim thêm hai màn mới; `reportsTestkit.tsx` tách `stubApi` dùng chung (jscpd) | `route.test.tsx` | vitest reports + app (18 tệp) | 209/209 qua |
| Kiểu, lint, định dạng | tsc, eslint, prettier | các lệnh trên | sạch |

## Quyết định thay chủ shop

- Quyền của mục menu "Tệp xuất": `report.sales.read` (Screen chỉ nhận một quyền chính). Chọn vì mọi vai có `report.export` (chủ, quản lý, kế toán) đều có quyền này. Phương án khác: `report.export` (loại người chỉ xem lần chạy nền của mình); bỏ vì người chưa có quyền xuất vẫn có thể có lần chạy nền của báo cáo bán hàng. Hệ quả ghi ở "Việc phát hiện thêm" (người chỉ có quyền kho).
- Nút "Xem" của lần chạy dạng bảng: mở màn báo cáo tương ứng với đúng kỳ/tham số của lần chạy (cùng cách Q8: màn tự hiện kết quả). Phương án khác: hộp hiện kết quả thô; bỏ vì sẽ vẽ lại từng báo cáo hai lần.
- Hạn tải trên Tệp xuất lấy từ `expiresAt` của API (không tự tính 7 ngày / 24 giờ ở màn).
- Chấm số tệp mới ở menu: đếm lần chạy `ready` kể từ lần cuối người dùng mở Tệp xuất (lưu mốc thời gian ở localStorage, bọc try/catch); mở trang Tệp xuất thì về 0.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tệp màn thêm ngoài danh sách của kế hoạch: `InventoryReports.tsx`, `InventoryFilters.tsx`, `VirtualTable.tsx`, `useMorePages.ts`, `inventoryData.ts`, `runsData.ts`, `openFile.ts`, `ExportsNavCount.tsx`, `ExportDialog.tsx`, `PagedReport.tsx`, `ReportBanners.tsx`, `khach/ExportCustomersButton.tsx` (tách cho gọn và để jscpd không báo mã lặp, không đổi hành vi). `reportAccess.ts`, `useRunNotice.ts` (mở tệp qua `openFile`, nhận `runFile` theo họ) sửa nhỏ.
- "Danh sách ảo khi trên 500 dòng": màn luôn cuộn ảo (một đường mã); mỗi lần tải 500 dòng là mức tối đa của API.
- ci-task lần 1 (27/11): vet, lint đỏ do máy (cache build Go `no such file or directory`, ca `package errors is not in std`; task không chạm Go); jscpd đỏ 20 mã lặp mới và console-check đỏ `route.test.tsx` — đã sửa hai cái sau (gom `ExportDialog`, `PagedReport`, `ReportBanners`, `stubApi`, `installFakeNow`; cập nhật `route.test.tsx`); test kiến trúc qua.
- Không có tên nhãn trong tham số `customers` ngoài mã nhãn: trang Tệp xuất tra tên bằng `listCustomerTags` (nhãn đã xóa → "nhãn đã xóa").

## Việc phát hiện thêm

- Người chỉ có quyền kho (vai `warehouse`: `report.inventory.read`, không có `report.sales.read`) không thấy mục menu Tệp xuất, mà báo cáo tồn quá nặng của họ chạy nền và gợi ý "Tệp xuất" (đường đó bị chặn 403). Cần Screen nhận "một trong các quyền" hoặc chủ shop cho vai kho `report.sales.read`; không tự đổi ở task này.
- Chấm số tệp mới đếm mọi lần chạy xong trong danh sách người xem thấy (quản lý thấy cả của người khác); API không có mã người tạo để lọc "của tôi".
- Nút Xem của lần chạy dạng bảng chỉ mang kỳ (from, to, date) sang màn báo cáo, không mang bộ lọc khác (kho, nhóm theo); mở lại chạy báo cáo như bình thường.
- Backend Task 234, 237, 238 chưa có trên nhánh: tên tham số `customers` (createdFrom, createdTo, lastOrderFrom, lastOrderTo, lifecycle, tagIds, minOrders) theo mô tả `ReportParams` của hợp đồng và kế hoạch; cần đối chiếu khi 238 gộp (400 `bad_params` nếu lệch).

## E2E viết sẵn, chưa chạy

Không có: kế hoạch Task 241 không có test e2e hay Playwright riêng; so ảnh chụp 1512/390 với mockup (Step 4) để buổi chạy thử M6-21 vì quy định không chạy Playwright trong lúc làm task.

## Reviewer

Kết luận: ĐẠT (reviewer, 27/11/2026, bản đã stage; ci-task XANH lần 3 sau khi sửa jscpd và `route.test.tsx`).

Phát hiện phải sửa: không có. Ghi chú không bắt buộc: (1) vai kho không thấy menu Tệp xuất (đã ghi ở "Việc phát hiện thêm"); (2) tên tham số xuất khách cần đối chiếu khi Task 238 gộp; (3) `MAX_MIN_ORDERS = 100000` là ràng buộc phía màn, chỉ chặn nhập; (4) `salesRunFile` (useRunNotice) và `salesDef.file` (runsData) trùng việc nhỏ, giữ vì khác chỗ dùng. Reviewer chưa đọc kỹ `MovementsReport.test.tsx`, `ProductsExportButton.test.tsx`, test của `CustomerList` và `ProductList`; các test đó đã chạy qua trong ci-task.
