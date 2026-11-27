# Task 239 — Console — khung mục Báo cáo và màn Tổng quan (M6-16)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 239, M6-16). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ chạm `console/` (TypeScript). Không sửa Go, openapi, queries, migration.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go). Gói console: `src/app/nav.ts` (thêm 1 phần tử vào `NAV_GROUPS`: "Báo cáo" giữa "Kho" và "Hệ thống"); `screens.test.tsx` đọc `NAV_GROUPS` nên dùng tự nhiên.
- Operation OpenAPI bị chạm, oasdiff: chỉ ĐỌC qua client sinh (không đổi hợp đồng): `getSalesDashboard`, `listSalesOrdersDrilldown`, `createSalesReportRun` (POST, khóa chống trùng), `getSalesReportRun`, `getSalesReportRunFile`, `getMe`. Không thêm mã lỗi (không đụng `x-error-codes`, `errors.ts`).
- Chỗ dùng trên màn hình: màn mới `/bao-cao/tong-quan` (nhóm menu "Báo cáo"); khung `src/screens/reports/` dùng lại bởi Task 240, 241 (Báo cáo đơn, Hãng, Bán chạy, Tồn, Tệp xuất). Thành phần chung ở đây: `format.ts`, `PeriodPicker`, `useReport`, `RunNotice`, `DrilldownPanel`, `ExportButton`.
- Tài liệu: `docs/m6/mockup/ghi-chu.md` (ghi "Claude chốt thay chủ shop 27/11" vào bảng trạng thái và cột OK của (a)–(d), theo ghi chú điều phối ở kế hoạch M6).
- Câu sqlc, bảng, pha migration: không áp dụng.
- Test sẽ chạy: vitest `src/screens/reports/*.test.ts(x)`, `src/app/screens.test.tsx` (quy ước route, quyền trong docs/quyen.md, order không trùng), `npx tsc --noEmit`, `npx eslint` trên tệp sửa. KHÔNG chạy Playwright/e2e (lệnh chủ shop 24/11).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: mục `console/src/shared` của `docs/danh-muc-dung-chung.md`; `useWriteAction` (khóa chống trùng, thử lại cùng khóa, 503 retry_later), `unwrap`/`errorMessage`/`ApiError`, `queryKey`+`onRealtimeChanged` (báo hiệu → tải lại truy vấn cùng chủ đề), `MoreBar` ("Xem thêm"), `screenTestkit` (`stubApi`, `renderScreens`, `meReply`, `ORDER_PAGE`), `routeTestkit`, `shortTime` (app/time.ts), `.btn`, `.toggle`, `.box`, `.empty`, `.more`, `.field`, `.input`, `.money` của styles.css.
- Dùng lại / viết mới vì: `formatMoney` có ở `screens/products/productRules.ts` nhưng là của màn sản phẩm (nhập liệu, có ₫ riêng) → `reports/format.ts` viết riêng theo Interfaces của kế hoạch (`money`, `rate`, `count`, `dayLabel`; Q-X2: tỷ lệ phần vạn → "66,67%" làm tròn xuống như máy chủ), không nhập chéo màn. Không thêm thư viện (không biểu đồ: thanh ngang bằng CSS `.bar`, Q15).

## Soát thiết kế

Mục 5.1–5.12 (khớp kiến trúc): không áp dụng: task chỉ ở console, không thêm module/gói Go/bảng/hàng đợi/lệnh ra ngoài/thư viện (5.x về module, database, hàng đợi, sự kiện: không chạm). Riêng 5.x liên quan: lấy dữ liệu bằng client sinh qua `unwrap` + TanStack Query, khóa truy vấn bắt đầu bằng chủ đề realtime (`report.sales`, `report.run_finished`); không fetch thẳng; không tạo QueryClient/RealtimeClient thứ hai; màn tự đăng ký bằng `route.tsx`.

Mục 6.1–6.13 (đúng khi chạy đồng thời):
- 6.1/6.2 bấm hai lần, gửi lại: `POST …/runs` qua `useWriteAction` (một khóa `Idempotency-Key` sinh lúc bấm, giữ khi tự thử lại do mạng/503/409); chỉ tạo lần chạy nền MỘT lần cho một bộ tham số (ref theo khóa tham số); bấm "Xuất Excel" hai lần → nhận lại lời hứa đang chạy.
- 6.3–6.5 deadlock, lỗi tạm, cách ly: không áp dụng (không database). 6.4 phía màn: 503 `retry_later` hiện "Hệ thống đang bận, thử lại sau ít giây" (TEXT_BUSY).
- 6.6–6.8 việc nền lỗi, bên ngoài quá tải, tiến trình chết: lần chạy `failed` có `reason` → câu riêng (quá nặng / bận / lỗi / không quyền); không tự tạo lần chạy mới sau khi `failed`; mạng rớt giữa chừng → giữ khóa, bấm "Thử lại" gửi lại cùng khóa. Báo hiệu `report.run_finished` chỉ báo "tải lại", dữ liệu lấy bằng `GET …/runs/{id}`; mất báo hiệu thì resync 20 s của khung vẫn tải lại.
- 6.9 treo lâu: tự tải lại 60 s chỉ khi tab đang hiện (`document.visibilityState`), dừng khi tab ẩn, không chồng lượt (một truy vấn).
- 6.10 tiền: `number` nguyên đồng từ API, chỉ định dạng khi hiện; không cộng trừ tiền ở màn (tổng do máy chủ trả); ô bấm gửi lại đúng `metric`/`cellKey` của dòng.
- 6.11 quy luật: số ở đầu danh sách chi tiết = `total` của máy chủ, không đếm lại.
- 6.12 dữ liệu cá nhân: danh sách chi tiết chỉ hiện mã đơn, mã khách, tên nhân viên phụ trách; không log; không có SĐT/địa chỉ/tên khách (API không trả).
- 6.13 quyền: menu theo `report.sales.read`; nút "Xuất Excel" chỉ khi `/api/v1/me` có `report.export` phạm vi `all`; máy chủ vẫn kiểm (403 `export_forbidden` hiện câu riêng). Người chỉ có một Page thấy dòng "Phạm vi" (từ grants `report.sales.read` phạm vi `page`).

## Bằng chứng

Lệnh chạy trong `apps/crm/console` (sau khi gộp `main` một lần; sau vòng sửa 1 của reviewer: thư mục reports 7 tệp 68 test qua, cùng `src/app` 12 tệp 149 test qua). Test lẻ viết TRƯỚC code (lần chạy đầu: thiếu mô-đun → đỏ).

| Tiêu chí (kế hoạch Step 1) | Test | Lệnh | Kết quả |
|---|---|---|---|
| `money(1910000)="1.910.000"`, `rate(6667)="66,67%"`, `rate(null)="—"` | `format.test.ts` (money, vnd, rate, count, dayLabel) | `npx vitest run src/screens/reports` | 7 tệp, 68 test qua (sau vòng sửa 1) |
| PeriodPicker: 01/01/2026–03/01/2027 → nút Xem khóa, có lời giải thích; ngày tương lai; kỳ mẫu theo ngày VN; kỳ trên địa chỉ | `PeriodPicker.test.tsx` "chọn 01/01/2026–03/01/2027 …", `period.test.ts` (vnToday, presetPeriod, periodError, parse/writePeriod) | như trên | qua |
| Dashboard: 8 đơn lên, 1.910.000, 272.857, Chờ xử lý 1/1/1, dòng Tổng theo kênh | `Dashboard.test.tsx` "Hôm nay: 8 đơn lên…", "khối Chờ xử lý 1 / 1 / 1; bảng Theo kênh…" | như trên | qua |
| Bấm ô "Chờ xác nhận" → `metric=pending_confirm`, đầu bảng "1 đơn", dòng O4 | `Dashboard.test.tsx` "bấm ô 'Chờ xác nhận' …"; `DrilldownPanel.test.tsx` "Chờ xác nhận: đầu bảng '1 đơn' …" | như trên | qua |
| API trả `run_in_background` → đúng một POST `…/runs` có Idempotency-Key; báo hiệu `report.run_finished` → hiện số | `useReport.test.tsx` "run_in_background → đúng MỘT POST …", "báo hiệu report.run_finished …"; `Dashboard.test.tsx` "run_in_background → thông báo …" | như trên | qua |
| Mạng lỗi rồi thử lại → cùng Idempotency-Key | `useReport.test.tsx` "mạng lỗi khi tạo lần chạy rồi thử lại → cùng Idempotency-Key" (4 lần gửi cùng 1 khóa, bấm Thử lại là khóa thứ 5 vẫn cùng) | như trên | qua |
| 503 `retry_later` → "Hệ thống đang bận, thử lại sau ít giây" | `useReport.test.tsx` "503 retry_later …", `Dashboard.test.tsx` "503 retry_later …" | như trên | qua |
| Đồng hồ giả: sau 60 s gọi lại một lần; tab ẩn không gọi | `useReport.test.tsx` "đồng hồ giả: sau 60 s …" (59 s: 1 lần; 61 s: 2 lần; ẩn: vẫn 2; hiện lại: 3) | như trên | qua |
| Không có `report.export` (hay chỉ phạm vi page) → không có nút Xuất Excel; có thì bấm → một POST xlsx; tệp xong → "Tải về"; 403 `export_forbidden` | `Dashboard.test.tsx` mô tả "Xuất Excel (hành vi (b))" (4 test), "người chỉ có quyền một Page …" | như trên | qua |
| Tên nhân viên 120 ký tự cắt `…` có `title`; ngày không đơn không có nút; 0 đơn → trạng thái trống; tiền 9 chữ số | `DrilldownPanel.test.tsx` "tên nhân viên 120 ký tự …"; `Dashboard.test.tsx` "bấm 7 ngày … ngày không đơn không có nút", "không có đơn …", "tiền 9 chữ số" | như trên | qua |
| 412 đơn: Xem thêm theo con trỏ, 200 dòng mỗi lần | `DrilldownPanel.test.tsx` "412 đơn …" | như trên | qua |
| Ô Thành công/Hoàn dùng cơ sở "ngày hoàn tất" (`basis=completed`) đúng số của ô | `Dashboard.test.tsx` "ô Thành công mở danh sách theo ngày hoàn tất …" | như trên | qua |
| Menu: nhóm Báo cáo giữa Kho và Hệ thống; màn đăng ký đúng quyền | `route.test.tsx`; `src/app/screens.test.tsx` (NAV_GROUPS, quyền có trong docs/quyen.md, order không trùng) | `npx vitest run src/screens/reports src/app src/shell` | 15 tệp, 182 test qua |
| Đổi kỳ sau khi bấm xuất: thông báo giữ đúng kỳ lúc bấm (reviewer vòng 1) | `Dashboard.test.tsx` "đổi kỳ sau khi bấm xuất …" | như trên | qua |
| "Tải lại" lúc đang chờ lần chạy nền không tạo thêm lần chạy; lần chạy không đọc được (404) không treo ở "đang chạy nền" (reviewer vòng 1) | `useReport.test.tsx` "Tải lại khi đang chờ lần chạy nền …", "không đọc được lần chạy nền (404) …" | như trên | qua (68 test của thư mục) |
| Không đoạn lặp mới (cổng jscpd, ngưỡng 5 dòng/50 token) | jscpd chạy trên toàn `console/src` | `jscpd --config .jscpd.json --similarity console/src` rồi lọc `screens/reports` | không còn cặp nào chứa tệp của task (trước khi gọn: 7 cặp, đã gộp vào `reportsTestkit.tsx` và `MoneyTd`, `CountMoneyTds`) |
| Không lộ khóa bí mật | gitleaks | `gitleaks dir console/src/screens/reports` | no leaks found |
| Kiểu và lint sạch | tsc, eslint | `npx tsc --noEmit`; `npx eslint src/screens/reports src/app/nav.ts` | không lỗi, không cảnh báo |
| Mã lỗi có câu tiếng Việt (không thêm mã mới) | `src/app/errors.test.ts` | `npx vitest run src/app/errors.test.ts` | 17 test qua |

ci-task (cổng từ xa): CHƯA qua được vì hạ tầng, không phải vì mã. Lần 1 bị dừng ở 10 phút khi đang xếp hàng; lần 2: Blacksmith hết phút tháng (2718 ≥ 2700), Ubicloud không có máy, GitHub báo "The job was not started because recent account payments have failed or your spending limit needs to be increased" (run 45983626921, job static 1 giây, 0 bước). Việc sửa là của chủ shop (thanh toán GitHub), không tự làm. Task chỉ đổi `console/` và tài liệu, không đổi Go, nên các cổng Go (build, vet, lint, test gói, arch) không bị chạm; các cổng liên quan đã chạy ở máy theo bảng trên. `go test ./tests/arch/` không chạy ở máy theo lệnh điều phối (tốn RAM); chạy lại khi bể có phút.

Chụp màn 1512 × 767 và 390 so với mockup (Step 4 của kế hoạch): CHƯA làm, vì lệnh chủ shop 24/11 cấm chạy Playwright trong lúc làm task. Đã đối chiếu mockup bằng cách đọc HTML/CSS `docs/m6/mockup/tong-quan.html`, `m6.css` và chép luật CSS vào `reports.css` (cùng tên lớp, cùng biến). Chênh lệch còn lại ghi ở "Khác kế hoạch".

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

- **Điều kiện "M1–M5 đóng" của M6-00:** coi như được miễn (chủ shop duyệt 27/11 sáng, ghi chú điều phối M6); mockup chốt thay chủ shop 27/11 (hình sáu màn; hành vi (a)–(d)), đã ghi vào `docs/m6/mockup/ghi-chu.md`.
- **Chỗ đặt mã console:** `console/src/screens/reports/` (quy ước màn tự đăng ký của console, `route.tsx`), không phải `features/reports/` như kế hoạch viết. Vì: `loadScreens` chỉ nạp `src/screens/*/route.tsx` và luật không cho thêm route ngoài quy ước. Phương án khác: thư mục `features/` + sửa nạp màn — trái luật chung.
- **Cơ sở ngày của ô số:** ô "Thành công", "Hoàn" của Tổng quan mở danh sách với `basis=completed` (Tổng quan đếm theo ngày hoàn tất; máy chủ lọc `delivered` theo ngày lên đơn nếu không gửi `basis`, nên không gửi sẽ lệch số của ô). Phương án khác: không gửi basis — sai số.
- **Cỡ trang danh sách chi tiết 200** (mockup trạng thái E: "200 dòng mỗi lần"), tối đa của API.
- **Không dùng `useQuery` cho GET báo cáo:** khung gọi `onRealtimeResync` mỗi 20 s tải lại mọi truy vấn đang mở; báo cáo nặng không được tải lại 20 s/lần và Tổng quan phải 60 s (Q21). Hook `useReport` tự lo tải lại 60 s (chỉ tab đang hiện). Truy vấn của lần chạy nền thì dùng TanStack với khóa `report.run_finished` nên báo hiệu và resync tự tải lại. Phương án khác: sửa `onRealtimeResync` (đụng mã chung, ngoài phạm vi).
- **Dòng "Phạm vi" cho người chỉ có Page:** nói chung "chỉ đơn của Page bạn được giao" thay vì tên Page (không có API tên Page cho người chỉ có `report.sales.read`; mockup vẽ tên Page). Đổi khi có nguồn tên.
- **Nút "Tệp xuất"/"Mở Tệp xuất" trỏ `/bao-cao/tep-xuat`:** màn Tệp xuất là Task 241, tới lúc đó liên kết mới có đích (cùng đợt M6). Chấm số tệp mới ở menu "Tệp xuất" (quyết định (b)) làm ở Task 241 cùng mục menu đó.
- **Ô 0 đơn là khối tĩnh, không phải liên kết** (không có gì để mở; mockup F vẽ `div`).

## Khác kế hoạch

- Đường mã: `screens/reports/` thay `features/reports/` (xem trên). Tệp giữ chỗ cho màn của Task 240, 241 **không tạo**: không đăng ký màn rỗng lên menu; hai task sau thêm `route.tsx` dòng của mình (ghi ở đầu `route.tsx`).
- `useRunNotice.ts` chứa hook xuất Excel (`useExportRun`) và `RunNotice.tsx` là khối thông báo dùng chung; thông báo "đang chạy nền" nằm trong `useReport.ts`/`Dashboard.tsx`. Thêm `cells.ts` (ô số, địa chỉ), `period.ts` (luật kỳ), `dashboardData.ts` (kiểm kết quả chạy nền), `reportAccess.ts`, `reports.css`, `ReportsLayout.tsx` — tách để Task 240, 241 dùng lại.
- Sửa `src/app/nav.ts` (thêm nhóm "Báo cáo") và `src/app/screens.test.tsx` (bảng ghim NAV_GROUPS).
- Danh sách chi tiết trên điện thoại: ngăn là trang riêng bằng CSS (`.dd.is-open .rep-main` ẩn dưới 1000 px) + ô nằm trên địa chỉ (`?o=…`) nên nút quay lại của trình duyệt về báo cáo; không có route riêng.
- Chưa chụp ảnh so mockup (cấm Playwright); xem Bằng chứng.

## Việc phát hiện thêm

- Màn Tổng quan, `ExportNotice`: thông báo "Tệp … đã sẵn sàng — Tải về" chỉ hiện khi người dùng còn ở màn Tổng quan; nếu đã sang màn khác thì báo hiệu tới trễ sẽ không có chỗ hiện (khung chưa có kênh thông báo toàn cục). Task 241 làm chấm số tệp mới ở menu Tệp xuất để bù. Không tự làm.
- `report.run_finished` chưa có trong mã Go tại thời điểm viết (Task 235 làm); màn dựa đúng chủ đề và `Entity{"report_run", id}` ghi ở kế hoạch (dòng 146, 674). Nếu Task 235 đặt tên khác thì sửa hằng `TOPIC_RUN_FINISHED` ở `useReport.ts`.

## E2E viết sẵn, chưa chạy

Kế hoạch của task này không có test e2e (chỉ test thành phần và chụp màn so mockup). Không viết `tests/e2e`; Playwright chưa chạy (lệnh chủ shop).

## Reviewer

Kết luận: ĐẠT (vòng 2).

Vòng 1: CẦN SỬA, 2 phát hiện về tính đúng, đã sửa và có test: (1) thông báo xuất Excel dán nhãn theo kỳ đang chọn thay vì kỳ lúc bấm (nút Tải về vẫn tải tệp kỳ cũ) → `useRunNotice.ts` chốt `{id, title}` lúc bấm, test "đổi kỳ sau khi bấm xuất"; (2) "Tải lại" lúc đang chờ lần chạy nền tạo thêm lần chạy nền cho cùng tham số → `useReport.reload()` không làm gì khi đã có lần chạy chưa xong, nút khóa ở phase background, test "Tải lại khi đang chờ lần chạy nền …". Ghi chú không bắt buộc đã nhận: lần chạy đọc không được (404) giờ ra phase lỗi (có test). Chưa làm (không ảnh hưởng tính đúng): trang sau của danh sách chi tiết trả "busy" thì chỉ dừng ở số đang hiện; danh sách chi tiết bị resync 20 s của khung tải lại (cùng cơ chế mọi màn).
Vòng 2: ĐẠT, không còn phát hiện phải sửa. Khoảng hở nhỏ reviewer nêu (lúc POST tạo lần chạy còn bay, `waiting` chưa đúng) không tới được qua màn hình vì nút bị khóa; hook đã chặn POST trùng bằng lời hứa đang chạy của `useWriteAction`.
