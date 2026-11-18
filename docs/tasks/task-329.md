# Task 329 — Màn Kiểm kho trên điện thoại (M1-51d)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 329, M1-51d). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ chạm `console/` và tệp task).
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói Go nào.
- Operation OpenAPI bị chạm, oasdiff: chỉ gọi, không sửa `openapi.yaml`: `listWarehouses`, `startStocktake`, `getStocktake`, `recordStocktakeCount`, `applyStocktake`, `cancelStocktake` (Task 35), `searchVariants`, `listBalances` (đọc tên mã của dòng đếm từ máy khác), `getMe` (phạm vi quyền theo kho). oasdiff không đổi.
- Chỗ dùng trên màn hình: thư mục màn có sẵn `console/src/screens/inventory/`: thêm `Stocktake.tsx`, `useBarcodeScan.ts`, `stocktakeRules.ts`, `stocktake.css` và test đơn vị; `route.tsx` thêm hai dòng (`/kho/kiem-kho` trên menu nhóm Kho, `/kho/kiem-kho/:id` ẩn). `console/e2e/stocktake.phone.spec.ts` mới (WebKit 390 + chromium). Đăng ký màn: ca thêm vào `console/src/screens/inventory/route.test.tsx` (sau khi gộp main, Task 389 chuyển test đăng ký về từng thư mục màn; `src/app/screens.test.tsx` chung không đổi). `Balances.tsx`, `styles.css`, `fixtures.ts`, `idempotency.ts` giữ nguyên.
- Commit cấu trúc riêng (trước commit màn): dời `shortTime` (giờ "HH:mm dd/MM", múi Việt Nam) từ `console/src/screens/errors/errorsRules.ts` sang mã chung `console/src/app/time.ts` (thêm hằng `VN_TIME_ZONE`), xuất qua `console/src/shared/index.ts`, `docs/danh-muc-dung-chung.md` sinh lại (`make catalog`). Nơi dùng cũ: `ErrorsPage.tsx` (2 chỗ) nhập từ chỗ mới; `errorsRules.ts` dùng `VN_TIME_ZONE` cho `clockTime`; ca test `shortTime` dời nguyên sang `console/src/app/time.test.ts`. Không còn bản `shortTime` nào khác (grep).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run` (cả bộ màn hình, 33 tệp), `npx tsc --noEmit`, `npx eslint .`, `npx playwright test e2e/stocktake.phone.spec.ts` (webkit 390; chromium 1512, 390, 360), `e2e/inventory.spec.ts` (cùng thư mục màn), `e2e/errors.spec.ts` (màn Sự kiện lỗi dùng `shortTime` đã dời); cổng kiểm theo task (ci-task.sh) một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` phần console/src/shared (`useWriteAction`, `unwrap`, `errorMessage`, `ApiError`, `queryKey`, `TOPICS`, `hasPermission`); `Balances.tsx` (`signed`); `errorsRules.ts` (`shortTime`, giờ VN); fixture e2e (`json`, `writeRecorder`, `noHorizontalScroll`, `PHONE`); tìm "debounce|setTimeout" và "scan|barcode" trong `console/src`: chưa có hook quét mã hay hàng đợi lưu chậm.
- Dùng lại / viết mới vì: dùng lại `useWriteAction` (khóa chống trùng, tự gửi lại cùng khóa khi bận/mạng; bấm đúp nhận lại đúng lời hứa đang chạy) cho lưu số đếm, mở, áp dụng, hủy; `unwrap`/`errorMessage`; `signed` của `Balances.tsx` (cùng thư mục màn); lớp CSS của khung (`dlg`, `savebar`, `box-*`, `chip-*`, `ibtn`, `select-wrap`). Viết mới `useBarcodeScan` (kế hoạch yêu cầu; chưa có gì tương tự) và `stocktakeRules.ts` (phân loại dòng xem trước, câu xác nhận, quyền theo kho, khớp mã quét). Giờ "HH:mm dd/MM": jscpd báo trùng với `errorsRules.shortTime` (cùng mục đích, cùng lý do đổi: cách viết giờ trên màn hình) → gộp: dời về `src/app/time.ts`, xuất qua `src/shared` (luật console 6), cả hai màn dùng. Hộp thoại xác nhận: `Dialog` của màn Nhân viên nằm trong `screens/staff/staffData.tsx` (không phải mã chung) → viết `Confirm` nhỏ trong màn (không nút ×, không tấm trượt phải), không nhập chéo màn; gom về `src/shared` để ở Việc phát hiện thêm. Fixture e2e: dùng `writeRecorder`/`WriteLog` của `fixtures.ts` cho nhật ký ghi (jscpd). Lớp CSS của mockup chưa có trong `styles.css` (`scan`, `last-scan`, `count-big`, `stepper`, `lines`, `line`, `phone-col`) chép vào `stocktake.css` của màn, như `inventory.css` làm.

## Soát thiết kế

- 5.1–5.8, 5.10: không áp dụng: không đổi máy chủ, bảng, transaction, sự kiện; màn chỉ gọi API có sẵn.
- 5.9: mọi đường ghi của màn (`startStocktake`, `recordStocktakeCount`, `applyStocktake`, `cancelStocktake`) đi qua `useWriteAction` (Idempotency-Key); quyền khai sẵn ở `openapi.yaml` (Task 35).
- 5.11: không thêm thư viện (React, TanStack Query đã chọn).
- 5.12: theo màn mẫu tự đăng ký (`route.tsx`), luật thuần có test đơn vị, e2e với API giả (`fixtures.ts`).
- 6.1 Hai người cùng đếm một mã: gửi số tuyệt đối kèm `lineVersion` của dòng; máy chủ trả 409 `version_conflict` → màn không ghi đè, lấy lại phiên, hiện số trên máy chủ và báo người khác vừa đếm mã này để người đếm quyết định gõ lại. Áp dụng/hủy gửi `version` của phiên vừa lấy.
- 6.2 Bấm đúp áp dụng, mạng chập: `useWriteAction` trả lại lời hứa đang chạy (một POST), lỗi chưa rõ kết quả gửi lại cùng khóa; nút khóa khi đang gửi. Lưu số đếm: một hàng đợi, một yêu cầu một lúc; lỗi chưa rõ kết quả thì thử lại cùng thân (cùng khóa nếu không có mã khác chen giữa; khóa mới thì máy chủ trả 409 version_conflict vì lineVersion cũ → màn tải lại, thấy đúng số của mình → coi là đã lưu, không ghi hai lần). Rời màn khi còn số chờ gửi → gửi ngay một lần (không chờ kết quả).
- 6.3–6.11: không áp dụng: không khóa, không việc nền, không tiền ở màn.
- 6.12 Dữ liệu cá nhân: màn không có dữ liệu khách; bộ nhớ trình duyệt (localStorage) chỉ giữ mã phiên theo kho, không có số điện thoại, địa chỉ, token.
- 6.13 Quyền: API kiểm `inventory.stocktake` / `inventory.stocktake.apply` / `inventory.read` theo kho; màn đọc phạm vi trong `getMe` để khóa nút Áp dụng (trạng thái F) và chỉ cho mở phiên ở kho được đếm; API vẫn là chốt chặn (403 → câu lỗi).

## Quyết định thay chủ shop

1. **Tìm phiên đang mở của kho (trạng thái A, "Kho chính đang có phiên KK-0007 … Vào phiên"):** API không có danh sách phiên, và 409 `stocktake_open` không kèm mã phiên. Chọn: màn nhớ phiên (theo kho) mà máy này đã mở hoặc đã vào (localStorage), hỏi lại `getStocktake` và chỉ hiện khi phiên còn mở; mỗi phiên có đường dẫn riêng `/kho/kiem-kho/<id>` để gửi cho người cùng đếm. Bấm "Bắt đầu" mà kho đã có phiên do máy khác mở → hộp thông tin "đang có phiên chưa xong, nhờ người mở gửi đường dẫn phiên". Không hiện "do Hùng" (API không trả người mở). Phương án khác: thêm operation danh sách phiên (đổi hợp đồng, ngoài phạm vi task) → ghi Việc phát hiện thêm. Đảo ngược dễ: khi có API, thay nguồn của hộp thông tin.
2. **Tên mã của dòng đếm từ máy khác:** dòng của phiên chỉ có `variantId`. Tên lấy từ kết quả quét trên máy này; mã còn thiếu tên thì đọc `listBalances` của kho theo trang (100 dòng, tối đa 20 trang) tới khi đủ; vẫn thiếu → hiện "Mã chưa có trong Tồn kho". Phương án khác: API trả kèm SKU/tên trong `StocktakeLine` (đổi hợp đồng) → Việc phát hiện thêm.
3. **Hai người đếm cùng mã (409 `version_conflict`)**: không tự ghi đè số của người kia; xem 6.1.
4. **Hộp "Thiếu so với phần giữ" ở xem trước** ghi "đang giữ N, đếm được M → có thể bán K"; bỏ "cho 2 đơn" của mockup vì API không trả số đơn. Trạng thái E bỏ "bởi Lan" (API không trả người áp dụng); câu cảnh báo giữ nguyên vì máy chủ thật sự ghi lệnh cảnh báo (`inventory/stocktake.go`, `shortageAlertKind`).
5. **Hủy phiên** có hộp xác nhận (thao tác không lùi được); mockup chỉ vẽ nút. Đảo ngược dễ: bỏ hộp.
6. **Đường dẫn** `/kho/kiem-kho` (thứ tự 30 trong nhóm Kho, theo `khung.js`: Tồn kho, Phiếu kho, Kiểm kho, Nhà cung cấp), quyền menu `inventory.stocktake` (người chỉ có `inventory.read` không đếm được nên không thấy mục).
7. **Rời màn trong vòng 800 ms sau lần quét cuối** (bấm menu, đổi trang trong CRM): số đang chờ được gửi ngay một lần lúc rời màn, không chờ kết quả, không thử lại; lỗi lúc đó không ai thấy. Phương án khác: chặn rời màn bằng hộp hỏi (hành vi mới, mockup không có). Đóng tab/tải lại trang trong 800 ms vẫn có thể mất số cuối (chưa chặn `beforeunload`, mockup không có hộp đó).
8. **Ô quét không hiện ở trạng thái C** (xem trước); đầu phiên ("KK-0007 · Kho chính") chỉ ở trạng thái B, đúng như mockup.
9. **Hộp "Không thấy mã"** đặt ngay dưới ô quét thay vì dưới thanh nút như ảnh mockup (mockup vẽ các trạng thái nối nhau; dưới thanh nút dính đáy thì người quét không thấy).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước khi có mã | `stocktakeRules.test.ts`, `useBarcodeScan.test.ts`, `Stocktake.test.tsx` | `cd console && npx vitest run src/screens/inventory` (chưa có mã) | `Test Files 3 failed \| 1 passed (4)` (không nạp được mô-đun) |
| KH05: quét `2000000000015` 7 lần → đếm 7; gửi đúng một PUT số tuyệt đối `{counted: 7}` có Idempotency-Key, đúng mốc 800 ms (đồng hồ giả: 799 ms chưa gửi); xem trước "Không chỉnh", "Tồn 10, đang giao 3", 7/7/0 | e2e `điện thoại 390 › KH05: mở phiên, quét 2000000000015 bảy lần → đếm 7, lưu một lần; xem trước 'không chỉnh' khi đang giao 3` (webkit iPhone 390 + chromium 390); đơn vị `quét 2000000000015 bảy lần liền → tra một lần, số đếm 7, gửi một lần số tuyệt đối 7 sau 800 ms`, `lineKind › KH05: …` | `cd console && CRM_E2E_PORT=5129 npx playwright test e2e/stocktake.phone.spec.ts e2e/inventory.spec.ts e2e/errors.spec.ts`; `npx vitest run` | `35 passed`; `Test Files 33 passed`, `Tests 336 passed` |
| KH07: xem trước chip "Thiếu so với phần giữ", 5/2/−3, hộp "1 mã sẽ thiếu…"; hộp xác nhận đúng câu mockup; áp dụng gửi `version` của phiên; xong hiện "Thiếu so với phần giữ (1)" QU0789-DEN-M −2 và câu cảnh báo | e2e `KH07: đếm thiếu khi có đơn đang giữ → …`; đơn vị `preview`, `confirmText (trạng thái D)`, `lineKind › KH07` | như trên | qua |
| Áp dụng hai lần (bấm đúp) → một yêu cầu | e2e `bấm đúp Áp dụng → chỉ một yêu cầu áp dụng` (API giả trả chậm 400 ms; số POST `/apply` = 1) | như trên | qua |
| F: chỉ quyền đếm → nút Áp dụng tắt, có lý do; ô chọn kho chỉ kho được đếm (phạm vi kho, kho đóng) | e2e `F: chỉ có quyền đếm …`; đơn vị `StocktakeStart › …` (3 ca), `canAt` | như trên | qua |
| Hook: máy quét dạng bàn phím (Enter), dời mốc 800 ms, ±/gõ số, mã không có, hàng đợi một yêu cầu, thử lại cùng khóa, 409 version_conflict (không ghi đè / đã có đúng số), lỗi dứt khoát, flush, dòng máy khác, rời màn trước 800 ms | `useBarcodeScan.test.ts` (14 ca) | `npx vitest run src/screens/inventory` | `Tests 48 passed`; ca "rời màn" đỏ trước khi sửa (`1 failed \| 13 passed`) |
| Trạng thái A: 409 `stocktake_open` → hộp thông tin; phiên máy này đã mở → "Vào phiên KK-0007" (15:30 18/11); Hủy phiên có xác nhận, gửi `version`; mã không thấy → hộp đỏ; tên mã máy khác đọc từ Tồn kho | e2e `A: …`, `Hủy phiên: …`, `mã không có trong danh mục → …`, `KH07` (dòng SET tên đầy đủ) | như trên | qua |
| Khổ 360 (tên dài) và 1512 (cột ≤ 640 px, mục menu sáng) không tràn ngang | e2e `điện thoại hẹp 360 › …`, `máy tính 1512 › …` | như trên | qua |
| Đăng ký màn: `/kho/kiem-kho` (menu Kho, order 30, quyền `inventory.stocktake`), `/kho/kiem-kho/:id` ẩn | `screens/inventory/route.test.tsx` | `npx vitest run` | qua |
| Dời `shortTime` không đổi hành vi | `src/app/time.test.ts` (3 ca dời nguyên từ `errorsRules.test.ts`), e2e `errors.spec.ts` | như trên | qua (`9 passed` errors.spec chạy riêng) |
| Ảnh chụp so với mockup 390/360/1512 (A, B, C, D, E) | e2e với `CRM_E2E_SHOTS` | `CRM_E2E_SHOTS=<scratchpad> … playwright test e2e/stocktake.phone.spec.ts` | khớp bố cục, chữ, chip, số; khác có chủ ý ở Quyết định 4, 8, 9 |
| Cổng kiểm theo task | — | ci-task.sh (một lần) | đỏ `lint` (golangci-lint của phiên khác chạy cùng lúc: "parallel golangci-lint is running") và `jscpd` (6 đoạn trùng mới); còn lại xanh (build, vet, gen, sqlc, vitest, tsc, eslint, vite build, gitleaks, test kiến trúc). Sau khi sửa: `make jscpd` → `Found 0 clones`; `make lint` → `0 issues`; `npx tsc --noEmit`, `npx eslint .`, `npx prettier --check src e2e` sạch |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không có `console/src/app/screens.tsx` (đã thay bằng tự đăng ký `route.tsx`, Task 393): thêm dòng vào `screens/inventory/route.tsx`, ca đăng ký ở `screens/inventory/route.test.tsx` (Task 389).
- Thêm tệp ngoài danh sách kế hoạch: `stocktakeRules.ts` (+ test), `stocktake.css` (luật thuần và lớp CSS của màn, theo mẫu màn Tồn kho), và commit cấu trúc dời `shortTime` về `src/app/time.ts` (Tái dùng).
- "Ảnh chụp so với mockup đã chốt": chụp bằng chính e2e khi đặt `CRM_E2E_SHOTS=<thư mục>` (không commit ảnh, như Task 330), so bằng mắt với `docs/mockups/m1/png/kho-kiem-kho-{390,360,1512}.png`; không có ảnh chuẩn so từng điểm ảnh trong CI.

## Việc phát hiện thêm

- API kiểm kho thiếu danh sách phiên mở theo kho (hoặc mã phiên trong 409 `stocktake_open`), người mở và người áp dụng: cần cho trạng thái A, E đúng hết mockup (Quyết định 1, 4).
- `StocktakeLine` không kèm SKU/tên: màn phải đọc `listBalances` theo trang (Quyết định 2).
- `Dialog` của màn Nhân viên (`screens/staff/staffData.tsx`) và `Confirm` của màn này cùng làm hộp thoại `<dialog>` theo mockup: nên gom một bản về `console/src/shared` (task riêng, chạm màn Nhân viên).
- WebKit trong e2e: lúc đóng trang, yêu cầu luồng realtime `/api/v1/stream` (API giả để treo) bị gửi tiếp qua proxy của Vite tới 4190 (log `ECONNREFUSED` của WebServer); test không đỏ, nhưng `apiGuard` không bắt được. Gặp ở `stocktake.phone.spec.ts`, không gặp ở `shell.phone.spec.ts`. Nên xem lại fixture `api.live` (task riêng).
- Chưa có cảnh báo khi đóng tab trong lúc còn số chờ lưu (Quyết định 7); hẹn giờ thử lại đặt sau khi màn đã gỡ thì không gửi (reviewer).
- Tên mã đọc từ `listBalances` dùng khóa chủ đề `inventory.balance`: mỗi báo hiệu số dư có thể đọc lại tới 20 trang; nên tách khóa không theo chủ đề, hoặc API trả SKU/tên trong `StocktakeLine` (Quyết định 2).
- `getStocktake` của phiên đã áp dụng không trả danh sách thiếu: tải lại trang ở trạng thái E chỉ còn câu tóm tắt và nút "Xem ở Tồn kho".
- Hai người đếm cùng mã ở hai kệ: số tuyệt đối + `lineVersion` nên người lưu sau nhận số của người kia (có câu báo) và phải cộng tay. Cần chủ shop chọn: đếm theo kệ, hay API nhận "cộng thêm" (đổi hợp đồng).

## Reviewer

Kết luận: ĐẠT (commit 1, cấu trúc: dời `shortTime` về `src/app/time.ts`). Không có phát hiện phải sửa; `shortTime`, `SHORT` chép nguyên, `TZ` → `VN_TIME_ZONE`, không còn nơi nhập tên cũ từ `errorsRules`; 3 ca test dời nguyên (không phải xóa).

Commit 2 (màn Kiểm kho): ĐẠT. Không có phát hiện phải sửa; Ảnh hưởng khớp thực tế; bảng bằng chứng đối chiếu từng dòng (14 ca hook, KH05 đồng hồ giả 799/800 ms, KH07 `{version: 6}`, bấm đúp 1 POST). Ghi chú không bắt buộc (không sửa, chép sang Việc phát hiện thêm): hẹn giờ thử lại có thể đặt sau khi màn đã gỡ (số có thể mất mà không ai thấy, trong phạm vi Quyết định 7); truy vấn tên mã theo chủ đề `inventory.balance` có thể đọc lại tới 20 trang mỗi báo hiệu số dư; trạng thái E tải lại trang không có danh sách thiếu; hai người đếm cùng mã ở hai kệ: người lưu sau mất số vừa quét (có câu báo); `signedDelta` có thể dựng từ `signed`.
