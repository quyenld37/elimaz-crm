# Task 328 — Màn Phiếu kho (M1-51c)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 328, M1-51c). Mockup đã duyệt: `docs/mockups/m1/kho-phieu.html` (+ `png/kho-phieu-{1512,390}.png`). API chứng từ kho của Task 29, nhà cung cấp của Task 332. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ chạm `console/`.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi mã Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn gọi `listStockDocuments`, `createStockDocument`,
  `getStockDocument`, `replaceStockDocumentLines`, `postStockDocument`, `cancelStockDocument`, `searchVariants`,
  `listSuppliers`, `createSupplier` (thêm nhanh trong ô chọn), `listWarehouses`, `listBalances` (tên mã hàng và "có thể
  bán" của dòng), `getMe` (quyền).
- Chỗ dùng trên màn hình: thêm vào thư mục `console/src/screens/inventory/` `DocumentList.tsx`, `DocumentEdit.tsx`,
  `documentRules.ts` (+ test), đăng ký ở `inventory/route.tsx` (hai dòng: `/kho/phieu`, `/kho/phieu/:id` ẩn).
  `Balances.tsx` thêm nút "Tạo phiếu kho" và "Tạo phiếu nhập" (việc Task 51 để lại cho task này, mockup kho-ton.html).
  `inventory.css` thêm lớp của mockup phiếu (combo, stepper, ltbl, totals). Không sửa `src/app/*`.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run` (cả bộ unit của console, nhanh), `npx tsc --noEmit`, `npx eslint .`,
  Playwright `e2e/stock-documents.spec.ts`, `e2e/inventory.spec.ts` (Balances đổi), `e2e/shell.spec.ts` (menu có
  thêm mục), cổng 5128; `scripts/agent/ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục console
  (`useWriteAction`, `unwrap`, `errorMessage`, `queryKey`/`TOPICS`, `hasPermission`); tìm "Dialog", "formatMoney",
  "parseMoney", "SupplierPicker", "useDebounced" trong `console/src`.
- Dùng lại / viết mới vì: dùng lại `useWriteAction` (mọi đường ghi, khóa chống trùng), `unwrap`/`errorMessage` (câu
  "Không đủ hàng: còn {available}" đã có ở `ERROR_MESSAGES`), `Dialog` của `screens/staff/staffData.tsx` (như
  `roles` đã nhập), `formatMoney` (`products/productRules.ts`) và `parseMoney` (`products/editRules.ts`) — nhập chéo
  thư mục màn như tiền lệ roles→staff; việc dời chúng vào `src/shared` ghi ở Việc phát hiện thêm. Viết mới:
  `documentRules.ts` (nhãn loại phiếu, cộng dòng khi quét, kiểm số lượng, tổng) vì là luật riêng của phiếu kho.
  jscpd báo 4 đoạn trùng mới ở lần chạy cổng kiểm đầu: gộp hết (ô chọn kho → `WarehouseSelect`, thay dòng →
  `useReplaceLines`, hai hộp xác nhận → `ConfirmDialog`, nhập trong test → `import * as kit`); lần sau 0 đoạn trùng.
  `SupplierPicker` của Task 327 chưa có trên `main` (327 chạy song song, chưa commit): xem Quyết định thay chủ shop Q1.

## Soát thiết kế

- 5.1–5.3: màn chỉ đọc, ghi qua API của module chủ (`inventory` cho phiếu, nhà cung cấp; `catalog` cho tìm biến thể).
- 5.4: tạo phiếu rồi thay dòng là hai yêu cầu API riêng (hợp đồng không có tạo kèm dòng); phiếu vừa tạo là nháp, chưa
  đổi tồn, nên lỡ bước hai thì chỉ còn phiếu nháp rỗng, người dùng lưu lại dòng được (màn giữ dòng đang soạn).
- 5.5–5.6, 5.10: không áp dụng (màn hình).
- 5.7–5.8: không áp dụng (không mã Go).
- 5.9: mọi đường ghi qua `useWriteAction` (header `Idempotency-Key`, giữ khóa khi tự gửi lại); quyền đã khai ở
  `openapi.yaml` (Task 29, 332).
- 5.11: không thêm thư viện.
- 5.12: theo màn mẫu `products` (route tự đăng ký, `id = "moi"` là tạo mới, test vitest + Playwright API giả).
- 6.1: sửa dòng, ghi sổ, hủy gửi `version`; `version_conflict` → câu tải lại, không tự ghi đè.
- 6.2: ghi sổ có hộp xác nhận lần hai; nút thành "Đang ghi sổ…" và khóa; bấm đúp chỉ một yêu cầu (useWriteAction trả
  lại lời hứa đang chạy) — có test.
- 6.3–6.9: không áp dụng (máy chủ lo khóa, giới hạn; màn chỉ gửi).
- 6.10: giá nhập là số nguyên đồng (`parseMoney`), thành tiền tính bằng số nguyên; tổng chỉ để xem.
- 6.11: không vùng rủi ro cao.
- 6.12: tên nhà cung cấp, ghi chú không vào log (màn không log).
- 6.13: nút theo quyền thật (`inventory.documents.create`, `.post`, `inventory.suppliers.manage`); không quyền ghi sổ
  → nút mờ kèm lý do (mockup D); máy chủ vẫn kiểm theo kho (403 hiện câu lỗi).

## Quyết định thay chủ shop

- **Q1 — Ô chọn nhà cung cấp khi `SupplierPicker` (Task 327) chưa có:** kế hoạch cho Task 328 dùng `SupplierPicker` của
  327, nhưng 327 chạy song song và chưa commit gì. Chọn: viết ô chọn trong `DocumentEdit.tsx` (`SupplierField`, đúng
  hình mockup: gõ tên → danh sách `listSuppliers`, dòng "+ Thêm nhà cung cấp "…"" gọi `createSupplier` qua
  `useWriteAction`, chỉ hiện khi có `inventory.suppliers.manage` và đã có kết quả tìm của đúng chữ đang gõ). Không tạo
  tệp `SupplierPicker.tsx` để không giành tệp với 327. Khi 327 gộp: thay `SupplierField` bằng `SupplierPicker`, xóa
  bản này (Việc phát hiện thêm). Phương án khác: chờ 327 (chặn task), hay tạo cùng tên tệp (xung đột khi gộp).
- **Q2 — Đầu phiếu chỉ chọn trước lần lưu đầu:** mockup B vẽ ô chọn loại, kho, ghi chú trên phiếu nháp đã có mã, nhưng
  API không có thao tác sửa đầu phiếu (chỉ `replaceStockDocumentLines`). Chọn: phiếu mới có đủ ô chọn; phiếu nháp đã
  lưu hiện loại, kho, nhà cung cấp, ghi chú dạng chữ (như mockup F). Luật console 1 "chỉ vẽ hành vi máy chủ làm".
  Phương án khác: thêm API sửa đầu phiếu — việc backend ngoài task.
- **Q3 — Cột của danh sách:** `listStockDocuments` trả phiếu không kèm dòng, không có người tạo. Chọn: bỏ cột "Số
  lượng" (22 cái · 3 mã) và chỉ hiện giờ ở cột "Tạo" (không tên người). Phương án khác: gọi `getStockDocument` từng phiếu
  (N+1 yêu cầu) hay thêm trường vào API (ngoài task). Ghi ở Việc phát hiện thêm.
- **Q4 — Tên mã hàng của dòng:** dòng phiếu chỉ có `variantId`. Chọn: lấy từ kết quả `searchVariants` khi thêm dòng
  (nhớ trong bộ đệm truy vấn) và từ số dư của kho (`listBalances`, tối đa 10 trang × 100, cũng cho cột "Có thể bán").
  Dòng không tìm được tên (biến thể chưa từng có số dư ở kho, mở lại từ phiên khác) hiện "Mã hàng chưa tải được tên".
  Phương án đúng lâu dài: API trả SKU, tên ở dòng phiếu (Việc phát hiện thêm).
- **Q5 — Ghi sổ ở phiếu mới:** phiếu chưa lưu chỉ có "Lưu nháp"; "Ghi sổ…" hiện sau khi phiếu có mã (như mockup B, phiếu
  đã có mã PN-000124). Ghi sổ khi dòng còn đổi: lưu dòng trước rồi ghi sổ với version mới (một hộp xác nhận).
- **Q6 — Lọc kho ở danh sách:** thêm lựa chọn "Mọi kho" (mặc định, không gửi `warehouseId`) vì API cho phép; mockup chỉ
  vẽ ô đang chọn "Kho chính".
- **Q7 — Câu ở hộp xác nhận theo loại phiếu:** mockup chỉ vẽ phiếu nhập ("Tồn ở Kho chính tăng 22 cái (3 mã)"). Loại
  khác theo luật Task 29: xuất "giảm", chuyển hàng lỗi "chuyển sang hàng lỗi", xuất hàng lỗi "hàng lỗi và tồn cùng
  giảm", điều chỉnh "tăng/giảm tổng". Hủy phiếu có hộp xác nhận cùng kiểu (mockup không vẽ).
- **Q8 — Nút ở màn Tồn kho:** thêm "Tạo phiếu kho" (đầu trang) và "Tạo phiếu nhập" (kho chưa có hàng, chọn sẵn loại
  nhập) theo mockup kho-ton.html, việc Task 51 để lại cho task này; chỉ hiện khi có `inventory.documents.create`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Phiếu nhập 10, thêm nhanh "Xưởng Hoa" trong ô chọn → ghi sổ → bảng tồn có tồn 10, phiếu hiện tên NCC (1512, 390) | `stock-documents.spec.ts` "phiếu nhập 10 cái, thêm nhanh Xưởng Hoa…" | `CRM_E2E_PORT=5128 npx playwright test e2e/stock-documents.spec.ts --project=chromium` | 6 passed (21.1s) |
| Thứ tự yêu cầu: createSupplier → createStockDocument → replaceStockDocumentLines → postStockDocument, mỗi cái có `Idempotency-Key`; bấm đúp "Ghi sổ" chỉ gửi một lần | cùng test (dblclick) + `DocumentEdit.test.tsx` "bấm đúp Ghi sổ chỉ gửi một lần…" | như trên; `npx vitest run src/screens/inventory` | qua |
| Phiếu xuất không có ô nhà cung cấp | `stock-documents.spec.ts` "phiếu xuất: không có ô nhà cung cấp…"; `DocumentEdit.test.tsx` "đổi loại sang phiếu xuất…", "không có ô nhà cung cấp; cột có thể bán…" | như trên | qua |
| Phiếu xuất vượt phần giữ → "Không đủ hàng: còn 2", phiếu vẫn nháp, tồn không đổi | cùng hai test trên (hộp lỗi + lỗi ở dòng, chip "Nháp", `store.stock` giữ nguyên) | như trên | qua |
| Không quyền ghi sổ: nút mờ có lý do (mockup D); chỉ quyền xem: không nút sửa | `DocumentEdit.test.tsx` "không có quyền ghi sổ…", "chỉ quyền xem…" | `npx vitest run src/screens/inventory` | qua |
| Hủy phiếu có xác nhận; version_conflict có nút Tải lại; sửa dòng rồi ghi sổ thì lưu dòng trước | `DocumentEdit.test.tsx` "hủy phiếu…", "người khác vừa sửa…", "sửa số lượng rồi ghi sổ…" | như trên | qua |
| Danh sách: cột, lọc gửi status/kind, phiếu hủy mờ; NCC 200 ký tự không tràn ở 390; phiếu đã ghi sổ chỉ xem | `DocumentList.test.tsx` (2 ca); `stock-documents.spec.ts` "danh sách: tên nhà cung cấp 200 ký tự…"; `DocumentEdit.test.tsx` "chỉ xem: tên nhà cung cấp, kho, tổng" | như trên | qua |
| Luật dòng: quét cộng 1, bước −/+, kiểm số, tổng, đổi sang API | `documentRules.test.ts` | `npx vitest run src/screens/inventory` | qua |
| Màn Tồn kho: nút tạo phiếu theo quyền; e2e tồn kho, khung không đổi | `Balances.test.tsx` "quyền xem: không có nút tạo phiếu…"; `inventory.spec.ts`, `shell.spec.ts` | `npx vitest run`; `npx playwright test e2e/stock-documents.spec.ts e2e/inventory.spec.ts e2e/shell.spec.ts --project=chromium` | vitest 32 tệp, 316 ca qua; playwright 16 qua + 2 đỏ do câu chọn chữ "Nhà cung cấp" trùng câu gợi ý mới → sửa `exact: true`; sau khi gộp đoạn trùng chạy lại cả ba spec: 18 passed |
| Đăng ký màn | `inventory/route.test.tsx` | `npx vitest run` | qua |
| jscpd không đoạn trùng mới | — | `make jscpd` | `Found 0 clones.` (lần cổng kiểm đầu: 4 đoạn mới, đã gộp) |
| Cổng kiểm của task (tĩnh, test gói đổi, so hợp đồng, test-guard) | — | `ci-task.sh` (lần 2) | `CI-TASK XANH (so với điểm tách nhánh dffac01)`; lần 1 đỏ: jscpd 4 đoạn trùng (đã gộp) và test kiến trúc `parallel golangci-lint is running` (khóa lint của phiên khác, lần 2 `ok tests/arch`) |
| tsc, eslint | — | `npx tsc --noEmit && npx eslint .` | sạch |
| Ảnh chụp so với mockup | `console/test-results/kho-phieu-{,ncc-,soan-,xac-nhan-,xuat-,da-ghi-}{1512,390}.png` | spec trên | Khớp bố cục mockup A, B, C, E, F; khác có chủ ý: Q2 (đầu phiếu dạng chữ sau lần lưu đầu), Q3 (không cột Số lượng) |

Thứ tự test trước code: `documentRules.test.ts`, `DocumentList.test.tsx`, `DocumentEdit.test.tsx` viết trước, chạy đỏ
(`Test Files 3 failed`, chưa có mã), rồi mới viết mã. `stock-documents.spec.ts` viết sau mã (lần chạy đầu đã qua).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi "Modify: `console/src/app/screens.tsx`"; quy ước hiện tại (Task 393, 389) là màn tự đăng ký ở
  `src/screens/inventory/route.tsx` và test đăng ký ở `route.test.tsx` của màn — làm theo quy ước hiện tại.
- Thêm `documentRules.ts`, `documentsTestkit.tsx` (luật dòng tách khỏi component; API giả dùng chung cho hai test).
- Không dùng `SupplierPicker` của Task 327 (chưa có): Q1.

## Việc phát hiện thêm

- Khi Task 327 gộp: thay `SupplierField` trong `DocumentEdit.tsx` bằng `SupplierPicker`, xóa bản cũ (Q1).
- API phiếu kho: dòng phiếu nên trả SKU, tên mẫu, màu, size; danh sách nên trả tổng số cái, số mã và tên người tạo
  (mockup A, F). Cần task backend (mở rộng hợp đồng, không phá vỡ) rồi màn bỏ phần đọc số dư để lấy tên (Q3, Q4).
- API không có sửa đầu phiếu nháp (loại, kho, nhà cung cấp, ghi chú) như mockup B vẽ (Q2).
- Ô nhà cung cấp: gõ tên mà không chọn trong danh sách rồi lưu thì phiếu không có nhà cung cấp, chưa có lời báo (reviewer).
- `formatMoney`, `parseMoney`, `Dialog` đang nhập chéo thư mục màn (products, staff); nên dời vào `src/shared` (luật
  console 6) ở một task cấu trúc riêng.

## Reviewer

Kết luận: ĐẠT (lượt 1, trên bản đã gộp main dffac01).

- Phát hiện phải sửa: không có.
- Ảnh hưởng: khớp; ghi chú việc thay ô chọn kho ở `Balances.tsx` bằng `WarehouseSelect` chỉ nêu ở mục Tái dùng.
- Bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng điều tiêu chí nói; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc (không sửa trong task này): debounce tự viết ở `SupplierField` và `formatTime` gần giống mã
  có sẵn (`useDebounced` của Balances, `shortTime` của màn Sự kiện lỗi) — gộp vào `src/shared` cùng việc dời
  `formatMoney`/`Dialog`; gõ tên nhà cung cấp mà không chọn rồi lưu thì phiếu không có nhà cung cấp (màn hiện "—");
  trình soạn không tự đổi trạng thái khi người khác ghi sổ cùng lúc (máy chủ trả `version_conflict`, có nút Tải lại).
