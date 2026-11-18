# Task 327 — Màn Nhà cung cấp (M1-51b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 327, M1-51b). Mockup đã duyệt:
`docs/mockups/m1/kho-nha-cung-cap.html` (+ `png/kho-nha-cung-cap-{1512,390,360}.png`); ô chọn có thêm nhanh vẽ ở
`docs/mockups/m1/kho-phieu.html` trạng thái B (lớp `.combo`). API nhà cung cấp có từ Task 29. Loại việc (quy trình
mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ chạm `console/`).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Màn gọi `listSuppliers` (`inventory.read`; `q`,
  `includeArchived`), `createSupplier`, `updateSupplier` (`inventory.suppliers.manage`, `Idempotency-Key`, `version`).
- Chỗ dùng trên màn hình: màn mới `/kho/nha-cung-cap` (nhóm "Kho"), đăng ký thêm một mục trong
  `console/src/screens/inventory/route.tsx` (kế hoạch ghi `src/app/screens.tsx`; từ Task 393 màn tự đăng ký — xem
  Khác kế hoạch); tệp mới `Suppliers.tsx`, `SupplierPicker.tsx`, `supplierRules.ts` (+ test; `supplierTestkit.ts` chỉ
  cho test); lớp CSS của mockup (`.s-name`, `.stbl`, `.combo*`) thêm vào `inventory.css`; `src/styles.css` thêm lớp
  chung `.check` (chép m1.css) và token `--error: var(--buy)` của m1.css (trước đây thiếu nên `.field-err` hiện chữ
  đen ở mọi màn, kể cả hộp Thêm nhân viên — giờ đỏ như mockup); danh sách màn trong `src/app/screens.test.tsx` thêm
  một dòng;
  `Balances.tsx` xuất `useDebounced` (đã có, không đổi hành vi). `SupplierPicker` chưa có nơi dùng (phiếu nhập ở
  Task M1-51c).
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: `cd console && npx vitest run src/screens/inventory src/app/screens.test.tsx`, `npx tsc --noEmit`,
  `npx eslint .`; Playwright `e2e/suppliers.spec.ts` (mới) + `e2e/inventory.spec.ts` (cùng thư mục màn, menu Kho) ở
  1512 và 390 (thêm 360 cho tên dài); `scripts/agent/ci-task.sh` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `console/src/shared/index.ts` (`useWriteAction`,
  `unwrap`, `errorMessage`, `ApiError`, `queryKey`, `hasPermission`); tìm "dialog", "showModal", "combo", "picker",
  "debounce" trong `console/src`: chỉ có `Dialog` và `RefusalBox` ở `screens/staff/staffData.tsx` (màn Vai trò đã dùng
  lại theo đường `../staff/staffData`), `useDebounced` trong `Balances.tsx` (chưa xuất); chưa có ô chọn gõ tìm nào.
- Dùng lại / viết mới vì: dùng lại `Dialog` (cùng cách màn Vai trò nhập), `useWriteAction` (khóa chống trùng, tự gửi
  lại), `unwrap`/`errorMessage`, `hasPermission`, `useDebounced` (xuất ra từ `Balances.tsx`, cùng thư mục, không chép),
  lớp chung `tbl`, `toolbar`, `field`, `field-err`, `hint`, `chip`, `empty`, `box`, `check`. Không dùng `RefusalBox`:
  câu thứ hai của nó là câu riêng của màn Nhân viên (vai trò, Chủ); mockup nhà cung cấp có câu khác ("Có người vừa sửa
  nhà cung cấp này…"). Viết mới `SupplierPicker` (chưa có ô chọn gõ tìm) và luật kiểm số điện thoại trong
  `supplierRules.ts` (cùng luật máy chủ `internal/modules/inventory/suppliers.go`: bỏ dấu cách, chấm, gạch;
  `^\+?[0-9]{8,15}$`; máy chủ vẫn là nơi quyết).

## Soát thiết kế

- 5.1–5.8, 5.10: không áp dụng: chỉ màn hình, không đổi máy chủ, database, module.
- 5.9: mọi lần ghi (`createSupplier`, `updateSupplier`) qua `useWriteAction`: `Idempotency-Key` sinh khi bấm, gửi lại
  cùng khóa khi mạng hay máy chủ bận; quyền đã khai trong `openapi.yaml`.
- 5.11: không thêm thư viện.
- 5.12: theo màn mẫu (`screens/staff`, `screens/inventory/Balances.tsx`) và luật màn hình console.
- 6.1: sửa và lưu trữ gửi `version` đang xem; máy chủ trả `version_conflict` → hộp "Nhà cung cấp đã thay đổi, tải
  lại." có nút "Tải lại" (trạng thái C), không tự ghi đè.
- 6.2: khóa chống trùng; bấm hai lần khi đang gửi nhận lại cùng lời hứa (`useWriteAction`), nút tắt khi đang gửi.
  Thêm nhanh trong ô chọn: một `createSupplier` cho một lần bấm (test đếm).
- 6.3–6.11: không áp dụng (không database, tiền, việc nền).
- 6.12: số điện thoại chỉ hiện trên màn; không log, không vào câu báo lỗi.
- 6.13: nút Thêm, Sửa, Lưu trữ, Dùng lại và dòng "+ Thêm nhà cung cấp" chỉ hiện khi có `inventory.suppliers.manage`;
  máy chủ vẫn kiểm (`forbidden` hiện câu của mã lỗi). Xem danh sách cần `inventory.read` (quyền của màn).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tên 200 ký tự không tràn ở 1512, 390, 360; bảng trạng thái A (số theo nhóm, —, đã lưu trữ mờ, Dùng lại) | e2e `suppliers.spec.ts` "trạng thái A: tên 200 ký tự không tràn…" (1512, 390), "tên 200 ký tự không tràn ở 360"; vitest `Suppliers.test.tsx` "danh sách đang dùng; số di động hiện theo nhóm…" | `CRM_E2E_PORT=5127 npx playwright test e2e/suppliers.spec.ts e2e/inventory.spec.ts --project=chromium` | `19 passed` (11 suppliers + 8 inventory); sau sửa tự soát chạy lại suppliers: `11 passed` |
| `SupplierPicker` thêm nhanh "Xưởng Hoa" → đúng một `createSupplier` có `Idempotency-Key`, ô chọn nhận nhà cung cấp mới | `SupplierPicker.test.tsx` "thêm nhanh "Xưởng Hoa": một createSupplier…" (bấm hai lần, `writes` dài 1, khóa UUID) | `cd console && npx vitest run src/screens/inventory` | `Tests  31 passed (31)` |
| Ô chọn: chọn có sẵn không ghi, gõ lại bỏ lựa chọn; tên đã có, không có quyền → không thêm nhanh; trùng tên (đã lưu trữ) → câu dưới ô; Esc đóng; giá trị truyền vào sau thì hiện tên | `SupplierPicker.test.tsx` 6 ca còn lại | như trên | như trên |
| Trạng thái B: số sai báo dưới ô, không gửi; tên trùng báo dưới ô Tên; lưu đúng gửi một lần, khóa mới | `Suppliers.test.tsx` "thêm: số điện thoại sai…"; e2e "trạng thái B…" | như trên | như trên |
| Trạng thái C: lưu trữ có xác nhận, gửi version; người khác vừa sửa → hộp + Tải lại; sửa gặp version_conflict → Tải lại mở bản mới | `Suppliers.test.tsx` "sửa khi người khác vừa sửa…", "lưu trữ có xác nhận…", "lưu trữ khi người khác vừa sửa…"; e2e "trạng thái C…" | như trên | như trên |
| Trạng thái D; quyền chỉ xem không có nút ghi | `Suppliers.test.tsx` "chưa có nhà cung cấp nào…", "chỉ có quyền xem…"; e2e "trạng thái D…", "chỉ có quyền xem…" | như trên | như trên |
| Luật số điện thoại cùng máy chủ; đếm ghi chú theo ký tự; điều kiện thêm nhanh | `supplierRules.test.ts` | như trên | như trên |
| Màn đăng ký đúng nhóm Kho, thứ tự sau Tồn kho | `src/app/screens.test.tsx` | `npx vitest run src/screens/inventory src/app` | `Tests  85 passed (85)` |
| Ảnh chụp so với mockup | `test-results/nha-cung-cap-{1512,390,360}.png`, `nha-cung-cap-them-*.png`, `nha-cung-cap-luu-tru-*.png` so bằng mắt với `docs/mockups/m1/png/kho-nha-cung-cap-*.png` | e2e trên | khớp bố cục, câu chữ, chip, nút, chữ báo đỏ; khác có chủ ý: số "+84…" không chia nhóm, ô tìm ghi "Tìm theo tên" (xem Quyết định) |
| Cổng tĩnh và test theo vùng | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check, test kiến trúc | `ci-task.sh` (sau khi gộp main 50389d9) | mọi cổng xanh trừ test kiến trúc: `Error: parallel golangci-lint is running` (phiên khác giữ khóa); chạy lại `go test -count=1 ./tests/arch/...` → `ok elimaz.com.vn/crm/tests/arch 15.275s`. Sau sửa tự soát: `make console-check` → `Tests  306 passed (306)` (lần trước đó 1 ca realtime trễ giờ, xem Việc phát hiện thêm) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi sửa `console/src/app/screens.tsx`; tệp này không còn từ Task 393 (màn tự đăng ký bằng
  `src/screens/<tên>/route.tsx`). Thêm mục vào `src/screens/inventory/route.tsx`.

## Quyết định thay chủ shop

- **Ô tìm ghi "Tìm theo tên"** thay vì "Tìm theo tên hoặc số điện thoại" của mockup: `listSuppliers` chỉ tìm trong tên
  (`queries/inventory/suppliers.sql`, cột `name_search`); luật màn hình là chỉ vẽ việc máy chủ thật sự làm. Phương án
  khác: thêm tìm theo số ở API (task riêng, ghi ở Việc phát hiện thêm), rồi đổi lại câu.
- **Số điện thoại chia nhóm chỉ với số 10 chữ số bắt đầu bằng 0** ("0912 345 678"); số dạng khác hiện đúng như máy chủ
  lưu ("+842439361234"; mockup vẽ "+84 24 3936 1234"). Máy chủ lưu số đã bỏ dấu cách; chia nhóm số quốc tế cần bảng mã
  vùng. Dễ đổi trong `formatPhone`.
- **Nút ghi ẩn khi thiếu `inventory.suppliers.manage`** (mockup vẽ với tài khoản Chủ); cột Thao tác bỏ luôn. Máy chủ
  vẫn chặn.
- **"Dùng lại" không hỏi xác nhận** (mockup chỉ vẽ hộp xác nhận cho Lưu trữ); gửi ngay `status: active` với version.
- **Ô chọn hiện tối đa 8 gợi ý** (`PICKER_LIMIT`); gõ thêm để thu hẹp. Mockup vẽ 2 dòng, không nói giới hạn.
- **Tìm không khớp** hiện "Không có nhà cung cấp nào khớp" (mockup chỉ vẽ trống hẳn); cùng cách màn Tồn kho.
- **Thêm token `--error`** vào `styles.css` (chép m1.css): chữ báo lỗi dưới ô đỏ như mockup ở mọi màn.

## Việc phát hiện thêm

- `Dialog` (và `RefusalBox`) nằm ở `screens/staff/staffData.tsx`; màn Vai trò và giờ màn Nhà cung cấp nhập qua
  `../staff/staffData`. Nên dời `Dialog` sang `console/src/shared/` (task dọn code riêng).
- `listSuppliers` chưa tìm theo số điện thoại như mockup gợi ý; nếu cần, thêm vào câu `ListSuppliers` rồi đổi câu gợi ý.
- `src/realtime/client.test.ts` ca "200 rồi im lặng: sau watchdogMs…" đỏ một lần khi máy tải nặng (thứ tự
  `reconnecting`/`connected` đảo), chạy lại qua; test dựa thời gian thật, nên chuyển sang đồng hồ giả.

## Reviewer

Kết luận: ĐẠT (lượt 1, trên bản đã gộp main 50389d9). Phát hiện phải sửa: không có. Reviewer đối chiếu luật số điện
thoại với `internal/modules/inventory/suppliers.go`, điều kiện thêm nhanh với ràng buộc `suppliers_name_key`, mọi lần
ghi qua `useWriteAction` có `version` trong body; Ảnh hưởng khớp thực tế cả hai chiều; mọi dòng bằng chứng trỏ tới test
có thật và kiểm đúng điều nói. Ghi chú không bắt buộc (không sửa, ghi lại): dòng "+ Thêm…" có thể hiện thoáng qua
trong 300 ms chờ tìm (bấm thì máy chủ trả `supplier_name_taken`, câu báo hiện, không sai); nên dời cả `useDebounced`,
`SEARCH_DELAY_MS` cùng `Dialog` sang `console/src/shared/` trong task dọn code; `ListSuppliers` cắt ở 200 dòng mà màn
không báo (danh bạ nhỏ, ghi để biết).
