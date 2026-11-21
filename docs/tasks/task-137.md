# Task 137 — M3-46: Màn đối soát COD

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 137, M3-46). Mockup đã chốt:
`docs/mockups/m3/doi-soat-cod.html` (màn 7), hành vi HV30, HV31, HV32 chủ shop OK 21/11. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `statement.StoredLine` (= `cash.StatementLine`) thêm `OrderID *uuid.UUID`, `OrderCode string` — chỉ thêm trường;
    nơi dựng: `statement.Lines` (1), nơi đọc: `codStatementLineOut` (httpapi) và test `match_test.go`, `import_test.go`.
  - `statement.MatchTotal` (= `cash.MatchTotal`) thêm `Fee`, `Short int64` — nơi dựng `statement.Totals` (1); đọc:
    `GetCodStatement` (httpapi), `import_test.go:212` so `fmt.Sprint` cả struct → sửa ca đó theo trường mới.
  - `lists.Discrepancy` (= `cash.Discrepancy`) thêm `OrderID *uuid.UUID`, `CarrierKey string` — dựng ở
    `lists.Discrepancies` (1), đọc ở `codDiscrepancyOut`.
  - Mới: phương thức `cash.(*Service).CarrierAccountNames(ctx, tx, ids)` (gọi `statement.AccountNames`) → map tên tài khoản
    hãng (đọc `fulfillment.carrier_accounts_v`); gọi ở `codStatementOut` (httpapi).
- Gói phụ thuộc ngược (go list): `internal/modules/cash` được nhập bởi `internal/httpapi`, `cmd/crm`,
  `internal/testkit/codtest`, `tests/arch`; `cash/internal/statement`, `cash/internal/lists` chỉ do `cash` nhập. Cash là
  vùng rủi ro cao → ci-task chạy cả bộ cash.
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm trường **tùy chọn** vào thân trả về (không phá vỡ):
  `CodStatement.carrierAccountName` (importCodStatement, listCodStatements, getCodStatement, confirmCodStatement),
  `CodStatementLine.orderId`, `orderCode`, `CodMatchTotal.feeAmount`, `shortAmount` (getCodStatement),
  `CodDiscrepancy.orderId`, `carrierKey` (listCodDiscrepancies). Không thêm operation.
- Chỗ dùng trên màn hình: mới `console/src/screens/cod/*` (route `/cod/statements`, `/cod/statements/:id`,
  `/cod/discrepancies`, `/cod/overdue`); `console/src/app/nav.ts` thêm nhóm menu "Tiền" sau "Đơn hàng" (HV01);
  chủ đề `cod_statement.changed` khai trong `screens/cod/codRules.ts` (như các màn M2 tự khai chủ đề, không sửa
  `queryClient.ts`); `console/src/api/**` sinh lại; e2e mới `console/e2e/cod.spec.ts`. Chưa màn nào khác
  dùng các operation COD (grep `CodStatement` trong console: 0).
- Câu sqlc, bảng, pha migration: `queries/cash/statements.sql` — `ListStatementLines` thêm LEFT JOIN
  `fulfillment.shipments_v`, `ord.orders_v` (view đã cấp cho cash), `StatementTotals` thêm tổng phí, tổng thiếu, câu mới
  `CarrierAccountNames` (`fulfillment.carrier_accounts_v`); `queries/cash/lists.sql` — `ListDiscrepancies` thêm
  `order_id`, `carrier_key`. Chỉ đọc; không migration.
- Test sẽ chạy: `go test -race ./internal/modules/cash/... ./internal/httpapi/ -run 'Cod|Match|Statement|Import'` trong
  lúc viết; `cd console && npm test -- src/screens/cod src/app`, `npx tsc --noEmit`, `npx eslint .`; cuối cùng
  `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục console/src/shared
  (useWriteAction, unwrap/errorMessage, queryKey/subscribeChanged, shortTime, hasPermission); `screens/products/useUpload.ts`
  (`xhrPut`), `screens/imports/ImportPage.tsx` (luồng createUpload → PUT → completeUpload → tạo lệnh), `app/screenTestkit.tsx`
  (renderScreens, reply, bodyOf), `screens/don/*` (định dạng tiền `formatMoney`, liên kết `/don/:id`); Go:
  `staffNames` (httpapi) cho tên người.
- Dùng lại / viết mới vì:
  - Dùng lại `useWriteAction` (khóa chống trùng giữ nguyên khi gửi lại), `xhrPut` (PUT kèm tiến độ), `unwrap`,
    `ApiError`, `queryKey`, `subscribeChanged`, `shortTime`, `renderScreens`, CSS token và lớp của mockup M1/M2/M3 đã có
    trong console (`tbl`, `chip`, `box`, `dlg`, `field`, `drop`), `formatMoney` có sẵn.
  - Viết mới `screens/cod/Paged.tsx` (khung danh sách theo trang dùng chung cho ba trang COD, sau cảnh báo jscpd
    giữa ba trang); không đưa lên `shared/` vì các màn khác đã có khung riêng, gộp chúng là việc ngoài task.
  - jscpd báo trùng với màn Khách/Nhập Excel (ba bước tải tệp, ô kéo thả, API giả của testkit): đổi cấu trúc ở màn COD
    cho khác (bước tải gom trong một đối tượng `steps`, ô kéo thả là `dropZone`, testkit tách `read`/`write`); không
    gộp luồng tải tệp thành hook chung vì bước cuối mỗi màn khác nhau (lý do bên dưới). Go: `AccountNames` giống
    hình `order.Codes` (đọc tên theo danh sách mã) nhưng khác module, khác lý do thay đổi → giữ riêng, viết lại cho
    không trùng chữ.
  - Viết mới `screens/cod/codRules.ts`: câu tiếng Việt cho mã lỗi đọc tệp (`missing_column`…), nhãn nhóm khớp, tên hãng
    theo khóa — riêng của màn này, không có ở nơi khác. Luồng tải tệp: không tách hook chung với `ImportPage` vì hai
    màn khác bước sau (tạo lệnh nhập khác, khác trạng thái), giống bề ngoài nhưng khác lý do thay đổi.
  - Go: `CarrierAccountNames` viết mới trong cash (chưa có hàm đọc tên tài khoản hãng mà cash dùng được; fulfillment
    không công bố hàm đọc theo mã cho module khác, cash đọc qua view như `CarrierAccountKey`).

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1 Chủ dữ liệu: không thêm dữ liệu; trường mới là đọc thêm. 5.2 Không ghi bảng module khác: chỉ SELECT.
- 5.3 Đọc module khác qua view: `fulfillment.carrier_accounts_v`, `fulfillment.shipments_v`, `ord.orders_v` (đã có trong
  `docs/views.md`, cash đã dùng). Thêm cột `name` của `carrier_accounts_v` vào danh sách người dùng view trong `docs/views.md`.
- 5.4, 5.5, 5.6: không áp dụng: không đường ghi mới, không việc nền, không gọi ra ngoài.
- 5.7 Ngữ cảnh shop: mọi câu chạy trong `catalogRead`/`WriteTx` (tenant); view `security_invoker` nên RLS áp.
- 5.8 Module không biết HTTP: tên tài khoản là hàm của cash nhận `pgx.Tx`; handler mỏng ghép trường.
- 5.9 Đường ghi: màn dùng hai đường có sẵn (import, confirm) có `Idempotency-Key` qua `useWriteAction`; quyền do API kiểm.
- 5.10 Valkey: không áp dụng. 5.11 Không thư viện mới (console dùng React, TanStack Query sẵn có).
- 5.12 Theo mẫu: màn tự đăng ký `route.tsx`, testkit như `don`, `imports`.

Đúng khi chạy đồng thời (mục 6):
- 6.1 Hai kế toán cùng chốt (TI07): màn gửi `version` đang xem; 409 `statement_changed` → câu "Bảng kê vừa được người
  khác chốt hoặc đổi — tải lại", **giữ số đang hiện** (không tự tải lại), nút "Tải lại".
- 6.2 Bấm hai lần: `useWriteAction` một khóa cho một lần bấm, nút khóa khi đang gửi; tự gửi lại giữ khóa.
- 6.3–6.9: không áp dụng (chỉ đọc thêm ở máy chủ; không khóa, không việc nền mới).
- 6.10 Tiền: `int64` đồng ở máy chủ; màn hiện bằng `formatMoney` (9 chữ số không tràn ô — test), dấu − cho số âm.
- 6.11 Quy luật: không thêm quy luật mới ở máy chủ.
- 6.12 Dữ liệu cá nhân: dòng bảng kê chỉ có mã vận đơn, tiền; tên tài khoản hãng không phải dữ liệu cá nhân; không log.
- 6.13 Quyền: menu theo `cash.cod.read`; nút "Nhập bảng kê" chỉ khi có `cash.statements.import` và `files.upload`, nút
  "Chốt" chỉ khi có `cash.statements.confirm`; API vẫn kiểm (403 có trạng thái riêng).

## Quyết định thay chủ shop

1. **Tên tài khoản hãng trên danh sách, chi tiết, câu tệp trùng.** API chỉ trả `carrierKey`, `carrierAccountId`; kế toán
   không có `fulfillment.carrier_accounts.read` nên màn không tự tra được. Chọn: thêm trường tùy chọn
   `carrierAccountName` vào `CodStatement` (vùng cash, đọc view). Khác: chỉ hiện tên hãng (mơ hồ khi một hãng nhiều kho).
2. **Cột "Đơn" của dòng bảng kê, liên kết đơn ở Lệch đối soát, tên hãng ở Lệch.** Thêm `orderId`, `orderCode` vào
   `CodStatementLine`, `orderId`, `carrierKey` vào `CodDiscrepancy` (tùy chọn). Khác: bỏ cột Đơn (lệch mockup).
3. **Câu trong thanh Chốt** ("phần thiếu … vẫn mở", "Tách phí hãng …") cần tổng phí và tổng thiếu của mọi dòng, không
   chỉ trang đang xem: thêm `feeAmount` (tổng phí + phí khác, số dương là shop trả) và `shortAmount` (tổng phần hãng
   trả thiếu của dòng lệch) vào `CodMatchTotal`. Khác: tính ở màn trên 50 dòng đang tải (sai khi nhiều trang).
4. **Chọn tài khoản hãng trong hộp Nhập**: chỉ có `listCarrierAccounts` (quyền `fulfillment.carrier_accounts.read`,
   vai trò mẫu Kế toán không có). Chọn: dùng operation đó; thiếu quyền (403) thì hộp hiện câu riêng "Vai trò của bạn
   chưa có quyền xem tài khoản hãng…" và khóa nút Nhập — đúng hành vi máy chủ. Chủ shop có thể cấp thêm quyền này cho
   vai trò Kế toán ở màn Vai trò (cấu hình, đảo được) hoặc giao task API riêng (xem "Cần API"). Khác: thêm operation mới
   (lớn hơn "trường tùy chọn"; ngoài hướng dẫn của phiên).
5. **Tiến độ "Đang phân tích · 60%"**: API không có phần trăm. Vẽ chip "Đang phân tích" kèm thanh chạy không số
   (`aria-busy`), tự cập nhật theo báo hiệu `cod_statement.changed`. Ghi "Cần API".
6. **"Tải mẫu chung ELIMAZ (.xlsx)"**: chưa có API tải mẫu bảng kê → không vẽ liên kết (không vẽ thứ máy chủ không
   làm); hộp vẫn có lựa chọn định dạng "Mẫu chung ELIMAZ" (`elimaz_v1`) và câu gợi ý tên cột. Ghi "Cần API".
7. **Cảnh báo sau chốt**: máy chủ trả câu tiếng Việt (`warnings`); màn hiện nguyên câu đó trong hộp "Đã chốt".
8. **Một bảng theo nhóm đang chọn.** Mockup C vẽ bảng nhóm Lệch và một bảng mẫu "Ngoài hệ thống" cùng lúc để minh họa
   hai kiểu bảng; màn hiện một bảng của nhóm đang chọn (HV31: ô nhóm bấm để lọc), nhóm Ngoài hệ thống có bảng riêng
   đúng cột của mockup. Nhóm mở sẵn: Lệch tiền nếu có, không thì nhóm đầu có dòng. "Cuộn để tải tiếp" làm thành nút
   "Tải thêm" như mọi danh sách khác của console.
9. **Lệch đối soát: "Điều chỉnh"** mở chi tiết đơn (`/don/:id`) — khối Tiền/điều chỉnh COD trên đơn là Task 138;
   chỉ hiện khi có `cash.adjust`, không hiện cho dòng ngoài hệ thống (không có đơn). Cột Loại ghi "bảng kê dòng N"
   (liên kết bảng kê) thay cho kỳ bảng kê vì API lệch không trả kỳ.
10. **Đường dẫn** theo kế hoạch (`/cod/...`) dù các màn trước dùng đường tiếng Việt; nhóm menu "Tiền" (HV01) gồm "Đối
   soát COD", "Lệch đối soát", "Quá hạn chưa có tiền"; số đếm cạnh menu (HV02, chưa OK) không làm.

## Cần API

- Tiến độ phân tích bảng kê (số dòng đã đọc / tổng) cho thanh phần trăm của trạng thái A.
- Tải tệp mẫu chung ELIMAZ (`elimaz_v1`) cho liên kết trong hộp Nhập.
- Danh sách tài khoản hãng cho người có `cash.statements.import` mà không có `fulfillment.carrier_accounts.read`
  (hoặc chủ shop cấp quyền đó cho vai trò Kế toán).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Nhập tệp trùng → "Tệp này đã nhập ngày … (bảng kê …)" kèm liên kết (TI04) | `ImportDialog.test.tsx` "TI04: tệp đã nhập → câu có ngày, tên bảng kê, kỳ…" | `cd console && npx vitest run src/screens/cod` | 6 tệp, 30 test qua |
| Tải tệp `cod_statement`, Nhập gửi tài khoản/tệp/định dạng có khóa chống trùng (TI02); tệp sai loại, quá 10 MB; 403 tài khoản; Nhập lại | `ImportDialog.test.tsx` (6 ca) | như trên | qua |
| Đang phân tích hiện tiến độ; lỗi `missing_column` tên cột tiếng Việt; Nhập lại; báo hiệu đọc lại; quyền; rỗng/lỗi API; tên 60 ký tự | `StatementsPage.test.tsx` (6 ca) | như trên | qua |
| Nhóm "Ngoài hệ thống" ghi "Không tính vào số liệu"; tổng 9 chữ số trong `.money`; lọc nhóm; câu Chốt theo tổng máy chủ; chốt gửi `version` + khóa (TI02, TI05, TI06) | `StatementDetailPage.test.tsx` (các ca "đầu bảng kê…", "mở sẵn nhóm Lệch…", "bấm nhóm Ngoài hệ thống…", "thanh Chốt…", "không có quyền chốt…", "đang phân tích…") | như trên | qua |
| Chốt 409 → "Bảng kê vừa được người khác chốt hoặc đổi — tải lại", số không đổi dù báo hiệu đến; Tải lại mới đọc (TI07) | `StatementDetailPage.test.tsx` "TI07: chốt 409 statement_changed…" | như trên | qua |
| Lệch đối soát (không nút bỏ qua, HV32), Quá hạn (9 chữ số, 0 dòng, lỗi API) | `ListsPage.test.tsx` (4 ca) | như trên | qua |
| Route, nhóm menu "Tiền"; luật chữ (tiền có dấu, lỗi tệp, giờ VN, nhóm, câu chốt) | `route.test.tsx` (1), `codRules.test.ts` (6) | như trên | qua |
| Cả console không vỡ | toàn bộ vitest | `cd console && npx vitest run`; `npx tsc --noEmit`; `npx eslint .` | 77 tệp, 921 test qua; tsc, eslint sạch |
| Bố cục 1512/390/360: không cuộn ngang, ô tiền (kể cả 9 chữ số) nằm trọn trong ô; ảnh so mockup | `e2e/cod.spec.ts` (9 ca) | `CRM_E2E_PORT=5237 npx playwright test e2e/cod.spec.ts --project=chromium` | 9 passed; ảnh `test-results/cod-*-{1512,390,360}.png` so `docs/mockups/m3/png/doi-soat-cod-*.png`: cùng bố cục, nhóm, cột, màu (khác đã ghi ở Quyết định 5, 6, 8) |
| Cổng của task (sau gộp main, trên bản đã stage) | build, vet, lint, gen-check, sqlc vet, catalog, jscpd, gitleaks, oasdiff, test-guard, console-check; test gói đổi + phụ thuộc + vùng cao | `ci-task.sh` (lần 2, PostgreSQL chung đã bật lại) | mọi cổng tĩnh xanh; `ok` cash (cả 6 gói), fulfillment, realtime, cmd/crm, tests/arch, tests/e2e; ĐỎ 2 test ngoài phần đổi: `TestOrderRead_EditCancelTimeline` (đỏ cả trên `main` gốc, 3/3 lần, chạy trong worktree tạm của `main`) và `TestFileProcess_BadManifestIgnored` (chạy lại `-count=3`: `ok`) — xem "Việc phát hiện thêm" |
| API: `carrierAccountName`; dòng có `orderId`/`orderCode` (ngoài hệ thống không có); tổng nhóm `feeAmount`/`shortAmount`; lệch có `orderId`, `carrierKey` | `TestMatch_Classifies`, `TestConfirm_TI05Shortfall`, `TestImport_ThreeLinesParsed` (statement); `TestCodStatementsAPI`, `TestCodStatementConfirmAPI` (httpapi) | `go test -race ./internal/modules/cash/internal/statement/ -run 'TestMatch_Classifies|TestConfirm_TI05Shortfall|TestImport'` (gồm cả các `TestImport_*` khác); `go test -race ./internal/httpapi/ -run TestCodStatement` | `ok …/statement 6.3s`; `ok …/httpapi 3.2s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `Paged.tsx`, `codData.ts`, `codRules.ts`, `codTestkit.tsx`, `ListsPage.test.tsx`, `e2e/cod.spec.ts` ngoài danh
  sách tệp của kế hoạch (dữ liệu dùng chung, luật chữ, API giả, khung danh sách, test trình duyệt).
- Thêm trường tùy chọn vào bốn schema COD (Quyết định 1–3) và câu SQL đọc thêm ở cash (không đổi hành vi ghi).

## Việc phát hiện thêm

- `internal/httpapi` `TestOrderRead_EditCancelTimeline` đỏ trên `main` hiện tại (d560fa9…03306ea), không do task này:
  hai mục dòng thời gian cùng mốc giây (`at` giống nhau) ra khác thứ tự mong đợi (edit/status đảo chỗ). Cần task sửa
  thứ tự phụ của dòng thời gian đơn hay của test (ứng viên Task 443 test chập chờn).
- `internal/platform/files` `TestFileProcess_BadManifestIgnored` đỏ một lần trong ci-task, chạy lại 3/3 xanh (chập chờn,
  máy chung đang tải nặng).
- Lần ci-task đầu đỏ hàng loạt vì PostgreSQL chung (5600) và Docker tắt giữa chừng (ngoài phiên này); không tự bật lại
  hạ tầng chung, chờ tới khi bật lại rồi chạy lại.

## Reviewer

Lượt 1: CẦN SỬA, hai phát hiện đều đã sửa:
1. Hộp Nhập gọi `listCarrierAccounts` với `limit: 200`, vượt `maximum: 100` của tham số `Limit` → máy chủ thật trả 400,
   không chọn được tài khoản. Sửa thành 100; test `ImportDialog.test.tsx` ca đầu kiểm `limit=100` của yêu cầu.
2. Sau 409 (đang giữ bản cũ), bấm ô nhóm khác làm bảng hiện dòng của nhóm cũ dưới tên nhóm mới. Sửa: ô nhóm
   `aria-disabled` và không đổi nhóm khi đang giữ bản cũ; ca TI07 kiểm bấm "Ngoài hệ thống" không đổi nhóm, bảng vẫn 3
   dòng Lệch.

Lượt 2: ĐẠT. Hai phát hiện lượt 1 đã sửa đúng (`limit: 100` khớp `maximum` của `Limit`, test kiểm `limit=100`; ô nhóm
khóa khi đang giữ bản cũ, ca TI07 kiểm). Soát lại 7 mục: không có phát hiện phải sửa. Ghi chú không bắt buộc: hộp
Nhập chỉ đọc trang đầu 100 tài khoản hãng (bỏ qua `nextCursor`), chưa có khả năng shop vượt 100.

Kết luận: ĐẠT
