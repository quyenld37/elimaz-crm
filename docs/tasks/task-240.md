# Task 240 — Console — Báo cáo đơn, Hãng vận chuyển, Bán chạy (M6-17)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md (Task 240, M6-17)`. Loại việc (quy trình mục 3): A

Điều kiện "M1–M5 đóng" của M6-00 được miễn (chủ shop duyệt 27/11 sáng, theo ghi chú điều phối). Hình ba màn đã CHỐT (27/11); hành vi (a), (b) OK theo ghi chú điều phối.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không áp dụng, task chỉ sửa console (TypeScript), không đụng Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không có gói Go đổi).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng; màn chỉ gọi `getSalesOrdersReport`, `listSalesOrdersDrilldown` (qua `DrilldownPanel`), `getSalesCarriersReport`, `getSalesBestSellers`, `getSalesReportFilters`, và `createSalesReportRun`/`getSalesReportRun` (chạy nền, xuất Excel).
- Chỗ dùng trên màn hình: thêm 3 màn `/bao-cao/don`, `/bao-cao/van-chuyen`, `/bao-cao/ban-chay` vào nhóm "Báo cáo" (route.tsx, order 20/30/40; quyền `report.sales.read`). Sửa `DrilldownPanel.tsx` (thêm 2 prop tùy chọn `label`, `filterNote`; Tổng quan không truyền nên không đổi; `DrilldownPanel.test.tsx`, `Dashboard.test.tsx` là test đã có chạy lại). `ExportButton`, `useReport`, `useMorePages`, `PeriodPicker` dùng nguyên, không sửa; `ReportBanners.tsx` thêm nút "Thử lại" cho lần chạy nền hỏng khi tạo (xem Khác kế hoạch); `reportsTestkit.tsx` chỉ thêm `export`.
- Câu sqlc, bảng, pha migration: không áp dụng.
- Test sẽ chạy: vitest `src/screens/reports/` (test mới + test cũ của Dashboard, DrilldownPanel, route), `src/app/screens.test.tsx` (thứ tự menu), `src/app/errors.test.ts` (không đổi mã lỗi), `tsc --noEmit`, eslint tệp sửa. Không chạy Playwright, tests/e2e (luật phiên).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chỉ liệt kê Go; console không có danh mục) nên tìm trực tiếp trong `console/src/screens/reports`: `useReport` (tải, chạy nền, 503), `useExportRun` + `ExportButton`/`ExportNotice` (xuất Excel), `PeriodPicker` + `period.ts`, `ReportsPage`, `ReportBanners`, `DrilldownPanel` + `cells.ts`, `format.ts` (`money`, `rate`, `vnd`), `useMorePages`; `MoreBar` của ma-giam-gia; `carrierName` của warehouse.
- Dùng lại / viết mới vì: dùng lại toàn bộ các thành phần trên. Viết mới chỉ phần riêng của ba màn và khung chung của riêng chúng (jscpd đòi gom: `SalesReportPage.tsx` = trang + Tải lại + Xuất Excel + thông báo, `ReportPeriodPicker`, `useReportPeriod.ts`, `CellNumber.tsx` gồm `MoneyCell`): `salesTables.ts` (kiểm dạng kết quả chạy nền của ba báo cáo), `MultiFilter.tsx` (bộ lọc nhiều lựa chọn không có ô gõ; chưa màn nào có), ba màn, và `salesTablesTestkit.tsx` (API giả của ba báo cáo, mở rộng `stubApi` dùng chung của `reportsTestkit`). Không có hàm tỷ lệ/trung bình ở console: mọi số do máy chủ trả (làm tròn xuống theo hợp đồng, 66,66%), màn chỉ định dạng bằng `rate()`.

## Soát thiết kế

Task chỉ có console: không chạm database, hàng đợi, tiền (không cộng trừ), tồn, module Go.
- 5.1–5.3 Chủ dữ liệu, ghi bảng, đọc view: không áp dụng (không có dữ liệu mới, không truy cập bảng).
- 5.4–5.6 Giao dịch, việc sau commit, gọi ngoài: không áp dụng.
- 5.7 Ngữ cảnh shop: không áp dụng ở console; API tự áp.
- 5.8 Nghiệp vụ không biết HTTP: không áp dụng.
- 5.9 Đường ghi: duy nhất `POST …/sales/runs` cho Xuất Excel/chạy nền, đi qua `useWriteAction` (khóa `Idempotency-Key` sinh lúc bấm, giữ khi thử lại), quyền khai ở openapi từ Task 225.
- 5.10 PostgreSQL là nguồn thật: không áp dụng. 5.11 Không thêm thư viện: đúng (chỉ React, TanStack, react-router có sẵn). 5.12 Theo mẫu: theo màn Tổng quan (Task 239) và Tồn (Task 241).
- 6.1–6.9, 6.11: không áp dụng (không có tranh chấp dữ liệu phía máy chủ). Phía màn: bấm hai lần "Xuất Excel" nhận lại đúng lời hứa (useWriteAction); đổi bộ lọc khi lần tải cũ còn chạy thì kết quả cũ bị bỏ (useReport: khóa theo tham số, generation).
- 6.10 Tiền: màn không tính tiền, chỉ định dạng số nguyên đồng do máy chủ trả.
- 6.12 Dữ liệu cá nhân: danh sách chi tiết chỉ mã đơn, mã khách (API không trả tên, SĐT, địa chỉ); màn không log gì; tên nhân viên chỉ là nhãn dòng.
- 6.13 Quyền: màn khai `report.sales.read` (menu); nút Xuất Excel chỉ hiện khi có `report.export` phạm vi toàn shop; máy chủ vẫn kiểm (403). Người chỉ có quyền theo Page: dòng "Phạm vi" (ReportsPage salesScope); danh sách bộ lọc Page lấy từ `getSalesReportFilters` (đã giới hạn theo phạm vi ở máy chủ).

## Quyết định thay chủ shop

- QĐ-240-a: cột "Số đơn" của cơ sở "Ngày hoàn tất" là chữ tĩnh, không bấm được. Vì: máy chủ không có metric "thành công + hoàn" (chỉ `delivered`, `returned` với `basis=completed`), vẽ nút bấm sẽ sai. Mockup vẽ bấm được; kế hoạch chỉ yêu cầu ô số bấm được trong đúng phạm vi API. Phương án khác: thêm metric mới ở API (ngoài phạm vi task 240).
- QĐ-240-b: báo cáo Hãng hiện một dòng mỗi tài khoản hãng đúng như API trả, tên hãng là liên kết ở mọi dòng; không có dòng tổng phụ theo hãng. Vì: API không trả tổng phụ, tính ở màn là tự tính tỷ lệ/trung bình (trái luật "màn chỉ định dạng"). Mockup vẽ dòng hãng + dòng thụt vào; khác biệt ghi ở "Khác kế hoạch".
- QĐ-240-c: dòng tổng của Báo cáo đơn không bấm được (như mockup); mọi ô số trong dòng thường bấm được khi > 0.
- QĐ-240-d: kỳ, cơ sở, chiều nhóm, bộ lọc và ô đang mở nằm trên địa chỉ (`ky/tu/den`, `tinh`, `nhom`, `kenh/nguon/nv/tao/page/hang`, `o/b/d/k`) để nút quay lại và tải lại trang về đúng báo cáo; giá trị lạ bị bỏ.
- QĐ-240-e: tỷ lệ hiện đúng số máy chủ trả (làm tròn xuống, 66,66%), theo điều phối viên 27/11; mockup 66,67% chỉ là hình.
- QĐ-240-f: "Xem thêm" của Bán chạy gọi lại cùng báo cáo với `limit=500` (một lần, không phân trang), nút ẩn khi đã hiện đủ hay đã 500; thanh "Đang hiện N / M" lấy từ `totalRows`.

## Bằng chứng

Lệnh chung: `cd console && npx vitest run src/screens/reports src/app` → 20 tệp, 218 test qua; `npx tsc --noEmit` sạch; `npx eslint src/screens/reports` sạch; `npx prettier --write` các tệp sửa. Test viết trước mã (tệp test và testkit có trước, thành phần viết sau); chưa chạy được bản đỏ riêng vì thành phần chưa tồn tại lúc viết test (import thất bại).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chiều care_by: An 4 / Bình 3 / Chưa phân công 1, Tổng 8, 1.910.000, TB 272.857, 66,66% / "—" | `OrdersReport.test.tsx` "nhóm Nhân viên phụ trách: An 4 / Bình 3 / Chưa phân công 1…" | `npx vitest run src/screens/reports/OrdersReport.test.tsx` | qua; GET đúng `from=…&to=…&basis=placed&groupBy=care_by` |
| Bấm ô "4" của An → "4 đơn" | "bấm ô 4 của An: ngăn chi tiết 'Nguyễn Thị An · Đơn lên · 4 đơn'…" | cùng lệnh | qua; drilldown gửi `metric=placed`, `cellDimension=care_by`, `cellKey`=mã An |
| Doanh số mở `placed_active`, Thành công mở `delivered` | "bấm ô Doanh số mở metric=placed_active…" | cùng lệnh | qua |
| Ngày hoàn tất ẩn cột Đơn lên, Hủy | "đổi sang Ngày hoàn tất: cột Đơn lên, Hủy ẩn…" và "bấm ô Thành công của cơ sở hoàn tất gửi basis=completed" | cùng lệnh | qua |
| Bộ lọc Page P1 gửi `pageId=P1`; hai Page gửi hai `pageId`; chi tiết mang cùng bộ lọc | "lọc Page P1: gửi pageId=P1; chọn thêm P2…" | cùng lệnh | qua |
| Giá trị lạ trên địa chỉ bị bỏ; bộ lọc không có ô gõ | "giá trị lạ trên địa chỉ…", "bộ lọc không có ô gõ chữ; Bỏ lọc xóa hết" | cùng lệnh | qua |
| Tên nhân viên 120 ký tự không vỡ bảng (title đủ tên, khung cuộn ngang `.scroll-x`) | "tên nhân viên 120 ký tự…", `CarriersReport.test.tsx` "hai tài khoản GHN… tên tài khoản dài…", `BestSellers.test.tsx` "tên sản phẩm 120 ký tự…" | `npx vitest run src/screens/reports` | qua (jsdom kiểm lớp và khung cuộn; không đo pixel, Playwright để sau) |
| Chạy nền: đúng một POST có Idempotency-Key, xong thì hiện số | "chạy nền: đúng một POST …/runs…" | cùng lệnh | qua |
| Xuất Excel gửi đúng `report`, `format`, `params`; ẩn khi thiếu quyền | "xuất Excel (có report.export)…", "người chỉ có quyền một Page…" (cả ba màn) | `npx vitest run src/screens/reports` | qua |
| Trống, lỗi, đang tải, kỳ quá 366 ngày | "không có đơn khớp bộ lọc", "lỗi tải…", "kỳ quá 366 ngày: không gọi báo cáo", "đổi Nhóm theo… số cũ không hiện khi đang tải" | cùng lệnh | qua |
| Hãng: VTP "66,66%", Best "—", Tự giao 100,00%, Tổng do máy chủ | `CarriersReport.test.tsx` "gọi đúng kỳ 7 ngày; VTP 66,66%…" | `npx vitest run src/screens/reports/CarriersReport.test.tsx` | qua |
| Bấm tên hãng mở Báo cáo đơn `groupBy=carrier`, `carrierKey` đúng hãng, cùng kỳ | "bấm tên hãng mở Báo cáo đơn nhóm theo Hãng…", "hãng tự giao lọc carrierKey=self" | cùng lệnh | qua |
| Bán chạy SKU: 50 dòng đầu / 1.200, "Xem thêm" gửi `limit=500` một lần, 500 dòng, hết nút | `BestSellers.test.tsx` "tháng này, SKU…", "'Xem thêm' gửi limit=500…" | `npx vitest run src/screens/reports/BestSellers.test.tsx` | qua |
| Mức mẫu: Áo A 7 đứng đầu, Quần B trước Váy C; không cột SKU | "Mẫu: Áo A 7 đứng đầu, Quần B trước Váy C…" | cùng lệnh | qua |
| Đổi cơ sở sang Đơn thành công gửi `basis=delivered` và về 50 dòng | "đổi 'Tính trên' sang Đơn thành công…" | cùng lệnh | qua |
| Menu: ba mục mới, thứ tự 20/30/40, quyền `report.sales.read` | `route.test.tsx` (hai ca sửa) | `npx vitest run src/screens/reports/route.test.tsx src/app` | qua |
| Thanh chạy nền hiện lỗi tạo lần chạy và Thử lại | `ReportBanners.test.tsx` | `npx vitest run src/screens/reports/ReportBanners.test.tsx` | qua |
| Dashboard, DrilldownPanel, StockReport… không hỏng sau khi sửa `DrilldownPanel`, `ReportBanners` | các test cũ của thư mục reports | `npx vitest run src/screens/reports src/app` | 218 test qua |

## Test bị xóa hoặc tắt

Không xóa, không tắt. `route.test.tsx`: thêm ba dòng mới vào bảng route kỳ vọng và dùng `find` theo path thay vì chỉ số mảng (thêm màn thì chỉ số đổi); không nới kiểm tra nào.

## Khác kế hoạch

- Cột "Số đơn" của cơ sở hoàn tất là chữ tĩnh (QĐ-240-a); mockup vẽ bấm được.
- Báo cáo Hãng: một dòng mỗi tài khoản, không dòng tổng phụ theo hãng (QĐ-240-b); tên hãng là liên kết ở mọi dòng.
- Chip bộ lọc ghi "N đã chọn" cho mọi số lượng (mockup ghi tên khi chọn ít); bộ lọc Page không có mục "Không gắn Page" (API lọc `pageId` theo mã Page, không có giá trị "không Page").
- Kế hoạch ghi "so ảnh chụp 1512 và 390 với mockup": chưa làm (cấm chạy Playwright trong lúc làm task); ghi ở "E2E viết sẵn, chưa chạy".
- `DrilldownPanel`: thêm hai prop tùy chọn (`label`, `filterNote`); `ReportBanners`: thêm nút "Thử lại" cho lần chạy nền hỏng khi tạo (khoảng trống của Task 241 dùng chung); `reportsTestkit`: xuất `readRoute`, `writeRoute`.

## E2E viết sẵn, chưa chạy

Không có spec Playwright hay tests/e2e trong kế hoạch của task này. Việc còn lại khi tới lượt chạy e2e của M6: chụp ba màn ở 1512 và 390 (360 cho tên dài) đối chiếu `docs/m6/mockup/{don,van-chuyen,ban-chay}-*.png`.

## Việc phát hiện thêm

- Task 241 đã ghi màn Tệp xuất ở quyền `report.sales.read`; điều phối viên dặn màn đó cho nhận cả `report.inventory.read` (vai kho). Route `/bao-cao/tep-xuat` không thuộc task 240 nên không sửa ở đây; cần làm ở task chạm màn đó (`alsoRequires`/nhiều quyền).
- `Dashboard.tsx` tự có `CellButton`/`NumCell` riêng; `CellNumber.tsx` mới làm cùng việc. Gộp Dashboard, MovementsReport sang `SalesReportPage`/`CellNumber`/`useReportPeriod` là việc dọn riêng (không làm ở task này, tránh đổi màn đã duyệt).
- `mockup/ghi-chu.md` sẽ cần dòng "Task 240 đã code" khi đóng mốc M6; không đổi hành vi nên chưa sửa.

## Reviewer

Kết luận: ĐẠT (reviewer, 28/11/2026, lượt 1, không phát hiện phải sửa).

Reviewer đối chiếu mã với Ảnh hưởng, Tái dùng, bảng Bằng chứng: mọi tên test có thật, không có skip/only; tham số gửi API khớp `openapi.yaml`; ô số mở đúng danh sách (metric, cellDimension, cellKey, basis); trạng thái đang tải không hiện số cũ; không dữ liệu cá nhân; quyền đúng. Ghi chú không bắt buộc: (1) `CellNumber` làm cùng việc `CellButton`/`NumCell` của Dashboard (đã khai, hoãn dọn); (2) regex mã lọc rộng hơn uuid của openapi, chỉ ảnh hưởng khi sửa tay địa chỉ (máy chủ trả 400, màn hiện lỗi); (3) câu "ReportBanners không sửa" trong Ảnh hưởng đã sửa cho khớp. Reviewer không chạy lại vitest/tsc/eslint; kết quả đó là của tôi: vitest 218 qua, tsc sạch, eslint sạch, ci-task từ xa XANH (github, run 46055111657: cổng tĩnh gồm jscpd, lint, tsc, vitest console).
