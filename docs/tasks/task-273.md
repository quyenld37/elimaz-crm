# Task 273 — Màn kiểm kho mở sổ và hàng hoàn của đơn cũ

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 273, M7-28). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không. Task chỉ đổi console (TypeScript); không sửa Go, `openapi.yaml`, `queries/`, migration.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: chỉ GỌI (không sửa) `createOpeningStocktake`, `listOpeningStocktakes`, `getOpeningStocktake`, `postOpeningStocktake`, `checkOpeningStocktake`, `incrementStocktakeLine`, `createStocktakeCountImport`, `getStocktakeCountImport`, `createLegacyReturn`, `listLegacyReturns`, `getLegacyReturn`, cộng M1: `getStocktake`, `recordStocktakeCount`, `cancelStocktake`, `searchVariants`, `listWarehouses`, `getMe`, `createUpload`, `completeUpload`. `console/src/api/**` đã có mã sinh của Task 260.
- Chỗ dùng trên màn hình: thêm `console/src/screens/inventory/opening/{OpeningStocktakePage.tsx,ScanCounter.tsx,CountImport.tsx,useScanQueue.ts,openingRules.ts}` và `screens/inventory/m7Parts.tsx` (ErrorBox, RetryBox, ConfirmFooter, useLater: gom mã lặp mà jscpd bắt) và `legacy/{LegacyReturnForm.tsx,LegacyReturnList.tsx,LegacyReturnPage.tsx,legacyRules.ts}` + test; `screens/inventory/route.tsx` (3 route mới: `/kho/mo-so`, `/kho/mo-so/:id` ẩn khỏi menu theo HV01; `/kho/hang-hoan-cu` có mục menu HV02); `Stocktake.tsx` (xuất `useMeAndWarehouses` để dùng lại; `StocktakeStart`: thêm nút "Mở sổ kho này", HV01). Có thể chạm `app/screens.test.tsx` (thứ tự menu pin). Sửa `app/screenTestkit.tsx` (hai trường tùy chọn `attempts`, `dropNext`, không đổi hành vi cũ).
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: vitest các tệp mới và `screens/inventory/Stocktake.test.tsx`, `app/screens.test.tsx`, `app/errors.test.ts`, `lint-guard.test.ts`; `tsc --noEmit`; eslint các tệp sửa. Không chạy Playwright/e2e (lệnh phiên).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có gói Go. Console đã có: `useWriteAction` (khóa chống trùng, gửi lại cùng khóa), `unwrap`/`errorMessage` (mọi mã lỗi M7 đã có câu ở `app/errors.ts`), `Dialog` (`screens/staff/staffData`), `xhrPut` + `checkLocalFile` + `XLSX_TYPE` (Nhập Excel M1), `WarehouseSelect`, `canAt`, `matchHit`, `stocktake.css`/`inventory.css` (`.scan`, `.last-scan`, `.count-big`, `.stepper`, `.line`), `testkit` (`stubApi`, `renderScreens`).
- Dùng lại / viết mới vì: dùng lại tất cả mục trên. Viết mới `useScanQueue` vì `useWriteAction` nhớ MỘT khóa theo nội dung và chạy một yêu cầu một lúc: ba lần quét cùng mã có nội dung giống nhau sẽ bị gộp thành một khóa, trái với "mỗi lần quét một khóa riêng"; hàng đợi cần khóa cố định cho từng lần quét kể cả khi mất mạng. `useBarcodeScan` (M1) không dùng vì nó đặt số tuyệt đối sau 800 ms (HV03 đổi sang cộng dồn +1). CSS mới chỉ chép các lớp của `docs/mockups/m7/m7.css` (bằng biến token có sẵn).

## Soát thiết kế

- 5.1–5.4, 5.6–5.8, 5.10–5.12 (kiến trúc, sqlc, lỗi/log, thư viện): không áp dụng: task chỉ đổi console, không thư viện mới, không đổi hợp đồng.
- 5.5 (đường ghi duy nhất): mọi ghi qua `useWriteAction` hoặc `useScanQueue` (cùng quy tắc ADR-41: khóa sinh lúc bắt đầu, gửi lại cùng khóa).
- 5.9 (chống trùng): mỗi lần quét một `Idempotency-Key` riêng sinh lúc quét (`crypto.randomUUID`), giữ trong hàng đợi (bộ nhớ + localStorage) tới khi có kết luận; mất mạng/5xx/503/409 `request_in_progress` giữ nguyên khóa gửi lại; 4xx dứt khoát bỏ khỏi hàng đợi và báo. Tạo phiên, Đăng, Nhập tệp, Nhập kho khóa nút khi đang gửi (`useWriteAction`).
- 6.1 (hai người cùng làm): số hiện là số máy chủ trả sau mỗi lần cộng; 409 `stocktake_not_open` khi đang quét → màn tải lại phiên và hiện "đã đăng/đóng".
- 6.2 (bấm hai lần): Đăng, Nhập kho qua `useWriteAction` (nhận lại lời hứa đang chạy).
- 6.3 (tiền): không có tiền. 6.4 (tồn, khóa): máy chủ làm; màn chỉ hiện `warehouse_not_empty`, `uncounted_lines` (count) đúng thân lỗi. 6.5–6.11: không áp dụng (không mã nền, hàng đợi, migration).
- 6.12 (dữ liệu cá nhân): không hiện khách; lỗi dòng Excel chỉ số dòng và mã lỗi; ghi chú chỉ gửi trong phiếu; không log gì ra console.
- 6.13 (quyền): màn ẩn khi thiếu quyền (alsoRequires), ô chọn kho chỉ kho có quyền theo phạm vi (`canAt`), API vẫn kiểm; 403 hiện câu lý do.

## Quyết định thay chủ shop

1. **Đường dẫn và thư mục**: kế hoạch ghi `console/src/inventory/...`; console thật dùng `console/src/screens/<màn>/` và tự đăng ký route (quy ước `loadScreens`). Chọn `screens/inventory/opening/`, `screens/inventory/legacy/`, đường `/kho/mo-so`, `/kho/mo-so/:id`, `/kho/hang-hoan-cu` đúng đề xuất của mockup. Phương án khác: thư mục `src/inventory/` (trái quy ước, không được nạp).
2. **Danh sách "Chưa đếm (N)" kèm tên mã**: API không có (Task 247 Việc phát hiện thêm 1). Chọn: số "còn N mã" lấy từ máy chủ (`linesExpected − linesCounted`) và hộp giải thích; thêm ô "Nhập 0 cho mã": quét/gõ mã → tra `searchVariants` → `recordStocktakeCount` counted 0 (chỉ khi mã chưa có dòng trong phiên, tránh ghi đè số của người khác — Việc phát hiện thêm 9). Phương án khác: tải mọi biến thể rồi trừ (nặng, 3.000 mã); hoặc đợi API (chặn task).
3. **Mã trùng hai người bán**: API trả `candidates` có `variantId, sellerId, sku` nhưng không nhận `variantId`: nút "Cộng 1" của mỗi ứng viên gửi lại lần quét với `code = sku` của ứng viên (khóa mới). Nếu SKU ứng viên vẫn trùng thì lần gửi lại báo `ambiguous_code` tiếp và hộp hướng dẫn "ghi SKU". Tên người bán không có trong ứng viên nên hiện `sku` và mã người bán rút gọn.
4. **"Lần cuối: Lan, 20:41" và "Đã được Lan đăng"**: API không trả người đếm/người đăng (Việc phát hiện thêm 3, 5) → bỏ tên người, chỉ hiện giờ máy chủ khi có (`appliedAt`).
5. **Tên sản phẩm của dòng quét**: `incrementStocktakeLine` chỉ trả `variantId, counted`; tra `searchVariants` theo mã đã quét (như M1) để hiện tên, một lần mỗi mã (nhớ trong phiên). Tra không được thì hiện mã quét.
6. **Hủy phiên khi `warehouse_not_empty` lúc đăng**: dùng `cancelStocktake` của M1 (Việc phát hiện thêm 8); nếu máy chủ từ chối thì hiện câu lỗi của mã đó.
7. **409 `legacy_return_exists`**: thân chỉ có `existingId`; màn gọi `getLegacyReturn` hiện giờ nhập, kho, và liên kết tới ngày đó trong danh sách; gọi lỗi thì chỉ hiện câu báo.
8. **`opening_exists` không kèm phiên** (Việc phát hiện thêm 10): màn tra `listOpeningStocktakes?warehouseId=` (có sẵn) để tìm phiên đang đếm và hiện nút "Vào phiên".

9. **Quét lại cùng mã ở Hàng hoàn cũ (HV11)**: mockup nói "quét lần nữa để có dòng thứ hai" nhưng bộ đếm số lượng trên dòng làm quét nhiều lần thành nhiều dòng. Chọn: quét cộng 1 vào dòng "Bán lại được" của mã, chưa có thì tạo dòng mới; muốn có cả tốt và hỏng của cùng mã thì đổi dòng đã quét sang Hỏng rồi quét lần nữa (ra dòng Bán lại được mới). Câu gợi ý dưới danh sách đổi theo. Phương án khác: mỗi lần quét một dòng (phải gỡ từng dòng khi đếm chục cái).
10. **Khoảng ngày của danh sách hàng hoàn cũ**: giữ ô gõ `dd/mm/yyyy` như mockup (ô nhận khi gõ đúng một ngày có thật), mặc định 7 ngày gần nhất theo giờ Việt Nam. Còn trang sau thì hiện "Đã tải N kiện, còn nữa" và nút Xem thêm (không coi là hết).
11. **Quyền của màn phiên** (`/kho/mo-so/:id`): đòi thêm `inventory.stocktake` (đếm dùng `incrementStocktakeLine`, quyền của M1) ngoài `inventory.stocktake.opening`.

## Bằng chứng

Lệnh chung (gọi tắt `VT`): `cd console && npx vitest run <tệp>`. Không chạy Playwright, không chạy e2e (lời dặn phiên).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Quét 3 lần mã A → 3 yêu cầu `+1` khác khóa | `OpeningStocktakePage.test.tsx` "quét 3 lần mã A…"; `useScanQueue.test.ts` "quét 3 lần mã A…" | `VT src/screens/inventory/opening` | 3 `POST …/increments` body `{code, by:1}`, 3 khóa khác nhau, số to = 3 (máy chủ giả) |
| Mất mạng giữa lần 2 → gửi lại ĐÚNG khóa cũ, máy chủ đếm 3 | `OpeningStocktakePage.test.tsx` "mất mạng giữa lần 2…"; `useScanQueue.test.ts` 4 ca (chưa tới máy chủ, đã đếm nhưng mất câu trả lời, tải lại trang, 503/409 chưa rõ) | như trên | khóa lần 2 xuất hiện 3 lần liền ([k1,k2,k2,k2,k3]), tổng 3, banner "Mất mạng · 2 lần quét đang chờ gửi" rồi tắt |
| 360 px không cuộn ngang | `m7.css.test.ts` (luật CSS co cột, khớp mockup `docs/mockups/m7/m7.css`); `e2e/mo-so-hang-hoan.phone.spec.ts` viết sẵn | `VT src/screens/inventory/m7.css.test.ts` | 16 ca qua; spec trình duyệt CHƯA CHẠY (xem "E2E viết sẵn, chưa chạy") |
| Nhập Excel hiện lỗi từng dòng theo mã lỗi | `CountImport.test.tsx` "lỗi từng dòng theo mã…"; `openingRules.test.ts` `importRowText` 5 mã | `VT src/screens/inventory/opening` | 5 dòng `Dòng/Lỗi` đúng câu theo mã, không nội dung ô; tệp .csv và quá 5 MB báo ngay không gọi máy chủ; 409 `import_already_applied`; `failed`; `queued/running` |
| Đăng khi máy chủ trả `uncounted_lines` hiện số dòng | `OpeningStocktakePage.test.tsx` "máy chủ trả 422 uncounted_lines (count 2)…" | như trên | "Chưa đăng: còn 2 mã chưa đếm." |
| "Còn N dòng chưa đếm" lấy từ máy chủ; Đăng khóa khi còn | `OpeningStocktakePage.test.tsx` "tiến độ lấy từ máy chủ…", "còn 7 mã chưa đếm → nút Đăng khóa…"; `openingRules.test.ts` `postBlockedReason` | như trên | "405 / 412 mã", "Còn 7 mã chưa đếm", nút `disabled` kèm lý do; đủ mã → hộp xác nhận 412 mã, 8.214 cái, một `POST …/post` có khóa |
| Kết quả kiểm hiện mã lệch | `OpeningStocktakePage.test.tsx` mục F (3 ca) | như trên | Khớp / Lệch 2 mã (bảng Mã, Đếm, Sổ, Tồn) / Sổ khớp (changedSince) |
| 409 `legacy_return_exists` hiện liên kết | `LegacyReturn.test.tsx` "409 legacy_return_exists…" | `VT src/screens/inventory/legacy` | "Kiện VTP12345678 đã nhập rồi.", giờ và người nhập từ `getLegacyReturn`, liên kết `#kien-<id>` |
| Hàng hoàn cũ: chuẩn hóa mã, quét, tình trạng, kết quả hai phiếu, danh sách theo ngày | `LegacyReturn.test.tsx` (11 ca), `legacyRules.test.ts` (13 ca và một bảng ca) | như trên | body đúng `{warehouseId, reference:"VTP12345678", note, lines}`; liên kết phiếu nhập và phiếu chuyển hàng lỗi; "3 kiện · 10 cái (1 hỏng) trong 2 ngày"; rỗng; lỗi tải |
| Nút "Mở sổ kho này" (HV01) và đăng ký route (HV01, HV02) | `Stocktake.test.tsx` ca mới; `route.test.tsx` | `VT src/screens/inventory` | link `/kho/mo-so?kho=<id>` chỉ khi có quyền mở sổ kho đó; 3 route mới, 2 ẩn khỏi menu |
| Mọi mã lỗi có câu tiếng Việt, lint cấm any/as/fetch | `app/errors.test.ts`, `lint-guard.test.ts`, `app/screens.test.tsx` | `VT src/app/errors.test.ts src/lint-guard.test.ts src/app/screens.test.tsx` | `Test Files 23 passed, Tests 288 passed` (inventory + app, sau khi gom mã lặp); toàn bộ console `185 files, 2006 tests passed` sau khi thêm hai trường tùy chọn vào `screenTestkit` |
| Kiểu và lint | — | `cd console && npx tsc --noEmit`; `npx eslint src/screens/inventory src/app/screenTestkit.tsx e2e/mo-so-hang-hoan.phone.spec.ts` | 0 lỗi |
| Gộp `main` trước khi stage | — | `git merge main` | fast-forward tới `020b5403` (Task 235), không xung đột |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đường tệp: kế hoạch ghi `console/src/inventory/{opening,legacy}/…`; mã thật nằm ở `console/src/screens/inventory/{opening,legacy}/` theo quy ước nạp màn của console (Quyết định 1). Tên tệp đúng kế hoạch: `OpeningStocktakePage.tsx`, `ScanCounter.tsx`, `CountImport.tsx`, `LegacyReturnForm.tsx`, `LegacyReturnList.tsx`; thêm `useScanQueue.ts`, `openingRules.ts`, `legacyRules.ts`, `LegacyReturnPage.tsx`, `m7.css`, test.
- Danh sách "Chưa đếm (N)" kèm tên mã của mockup trạng thái C: không làm (API không có, Quyết định 2). Thay bằng số "còn N mã" từ máy chủ và ô "Nhập 0 cho mã".
- Mockup "Lần cuối: Lan, 20:41" và "đã được Lan đăng": bỏ tên người (Quyết định 4).
- Sửa `app/screenTestkit.tsx`: thêm hai trường tùy chọn `attempts` và `dropNext` vào `WriteQueue` để giả lập rớt mạng (không thay đổi test khác; cả bộ console qua).

## E2E viết sẵn, chưa chạy

- `console/e2e/mo-so-hang-hoan.phone.spec.ts`: 6 test (3 khổ 360×800, 390×844, 1512×767 × 2 màn): "Kiểm kho mở sổ: 412 mã, tên dài, quét hai lần → hai yêu cầu +1 khác khóa, nút Đăng khóa, không cuộn ngang" và "Hàng hoàn đơn cũ: thêm món tên dài, hai tình trạng, không cuộn ngang". Đã qua `tsc` và `eslint`, chưa chạy (lời dặn phiên: Playwright để khi xong toàn bộ M7).

## Phụ thuộc task để sau

- Không có: mọi operation dùng ở đây đã có trong hợp đồng của Task 260 (máy chủ Task 262–264 là task khác của đợt này). Màn chạy được khi máy chủ của chúng vào; trước đó trả `not_implemented` (501) hiện câu "Chức năng này chưa có".

## Việc phát hiện thêm

1. Danh sách mã chưa đếm kèm tên (tab "Chưa đếm") cần API (Task 247, mục 1): khi có, thay ô "Nhập 0 cho mã" bằng danh sách có nút Nhập 0 từng dòng.
2. `incrementStocktakeLine` chưa nhận `variantId`: chọn ứng viên khi mã trùng hai người bán phải gửi lại bằng SKU của ứng viên; SKU trùng giữa hai người bán thì vẫn `ambiguous_code` (Task 247, mục 2). Ứng viên chưa có tên người bán, màn hiện 6 ký tự cuối mã người bán.
3. `getStocktake` của phiên mở sổ trả mọi dòng (đến 3.000): ô "Nhập 0" gọi nó mỗi lần bấm để biết mã đã có số chưa; nên có `ifAbsent` ở `recordStocktakeCount` (Task 247, mục 9).
4. `LegacyReturn` chưa có mã phiếu (PN-…): liên kết hiện "Xem phiếu nhập" theo id, không có mã dễ đọc.
5. `listLegacyReturns` không lọc theo kho trên màn (API có `warehouseId`); thêm ô lọc kho khi có nhu cầu.

## Reviewer

Kết luận: ĐẠT (reviewer, bản đã gộp main 1086279f; ci-task xanh từ xa, run 46132261679).
Không có phát hiện ảnh hưởng tính đúng. Ghi chú đã sửa: số ca LegacyReturn/legacyRules, mục Ảnh hưởng thêm `screenTestkit.tsx`. Còn lại: chạy spec e2e viết sẵn ở 360 px trước khi đóng M7; `PERM_OPENING` khai hai nơi (trùng nhỏ).
