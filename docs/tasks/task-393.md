# Task 393 — Màn hình tự đăng ký, bỏ tệp định tuyến dùng chung (M1-x2)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (mục "Task 393 (M1-x2)", ngay trước Task 50). Loại việc (quy trình mục 3): D (dọn cấu trúc) + A (cơ chế tự đăng ký).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có; task chỉ chạm `console/` (TypeScript).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go). Phía console, ký hiệu bị chạm và chỗ dùng (grep cả `src`, `e2e`, `docs`):
  - `SCREENS` (`src/app/screens.tsx`): 1 chỗ dùng mã (`src/shell/App.tsx`) + 3 chỗ nhắc trong chú thích/danh mục (`src/app/nav.ts`, `src/shared/index.ts`, `docs/danh-muc-ghi-chu.yaml` → sinh `docs/danh-muc-dung-chung.md`). Danh sách cũ bị **xóa**, thay bằng `SCREENS` dựng từ `import.meta.glob` (cùng tên, cùng kiểu `readonly Screen[]`, nên `App.tsx` không đổi chữ ký).
  - `Screen` (`src/app/nav.ts`): giữ nguyên trường; tệp màn khai kiểu mới `ScreenRoute = Screen & { order: number }` (thêm thứ tự trên menu, vì thứ tự mảng cũ không còn).
  - Thư mục `src/features/{inventory,products,roles,staff}` → `src/screens/<cùng tên>` (dời nguyên, `git mv`): đường import tương đối giữa các màn (`roles` nhập `../staff/staff.css`) giữ nguyên vì cùng cấp. Không tệp nào ngoài `screens.tsx` nhập từ `features/`.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: menu và định tuyến (`Layout`, `AppRoutes` nhận `screens`); cả 7 route hiện có (`/san-pham`, `/san-pham/:id`, `/kho/ton`, `/nhan-vien`, `/nhan-vien/:id/quyen`, `/vai-tro`, `/vai-tro/:id`) giữ đúng path, nhãn, nhóm, quyền, `also`, `hidden` và thứ tự menu.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npm test` (mọi test đơn vị, gồm test mới `src/app/screens.test.tsx`), `npx tsc --noEmit`, `npx eslint .`, và toàn bộ e2e Playwright hiện có (`npx playwright test`, chromium + webkit cho `.phone`) vì mọi màn đều đổi chỗ tệp. Hook Stop và `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `console/src/shared` (có `NAV_GROUPS`, `allowedScreens`, `navSections` từ `src/app/nav.ts`; chưa có gì nạp màn theo thư mục). Tìm `import.meta.glob`, `glob` trong `console/`: chưa ai dùng.
- Dùng lại / viết mới vì: dùng lại `Screen`, `NAV_GROUPS`, `navSections`, `AppRoutes` như cũ; tự đăng ký dùng `import.meta.glob` có sẵn của Vite (đúng kế hoạch, không thêm thư viện). Viết mới một hàm `loadScreens` (kiểm quy ước lúc nạp: thiếu `screens`, thiếu/sai `path`, `label`, `group`, `permission`, `order`, `element`, trùng `path` giữa hai tệp → lỗi nêu tên tệp) đặt ở `src/app/loadScreens.ts` (mẫu glob nằm riêng trong `src/app/screens.ts` một dòng), vì chỉ khung dùng; không đưa vào `src/shared`. CSS: `styles.css` hiện chỉ còn lớp chung chép từ `m1.css` (khung, nút, ô nhập, bảng, danh sách, nút bật tắt, biểu mẫu hai cột); CSS riêng đã nằm trong thư mục màn (`products.css`, `inventory.css`, `staff.css`, `roles.css`) và dời theo thư mục.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

Phạm vi chỉ là console (mã khung, không dữ liệu, không API):

- 5.1–5.7, 5.9, 5.10: không áp dụng: không có dữ liệu, bảng, transaction, đường ghi, lệnh ra ngoài hay Valkey nào được chạm.
- 5.8: không áp dụng (không có mã Go).
- 5.11: không thêm thư viện; `import.meta.glob` là của Vite đã có (ADR-44, `docs/chon-cong-cu-console.md`).
- 5.12: theo mẫu khung có sẵn (`nav.ts`, `router.tsx`, `App.tsx`); quy ước mới ghi trong chú thích đầu `src/app/screens.ts` và dòng "khi nào không dùng" của `console/src/shared`.
- 6.1–6.12: không áp dụng: không database, hàng đợi, tiền, tồn, dữ liệu cá nhân.
- 6.13 Quyền: không đổi. Mỗi màn vẫn khai `permission`; `allowedScreens` vẫn lọc menu và route (màn không có quyền không có route); API vẫn kiểm quyền thật. Tệp màn thiếu `permission` → lỗi khi nạp (không bao giờ thành màn không cần quyền) — có test.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): dời `src/features/*` → `src/screens/*`, không đổi hành vi | mọi test đơn vị (dời cùng thư mục) | `cd console && npx tsc --noEmit && npx eslint . && npm test` | tsc, eslint không lỗi; `Test Files 16 passed (16)`, `Tests 174 passed (174)` |
| Commit 1: các màn vẫn mở, menu, quyền như cũ | toàn bộ e2e hiện có | `CRM_E2E_PORT=5193 npx playwright test` | `68 passed (18.0s)` (chromium + webkit) |
| Commit 2: test đỏ trước | `src/app/screens.test.tsx` | `npx vitest run src/app/screens.test.tsx` (trước khi có mã) | `Failed to resolve import "./loadScreens"` |
| Thêm `src/screens/_thu/route.tsx` đúng quy ước → có mục menu (đang sáng) và route, không sửa tệp chung | `thêm thư mục màn mới… › src/screens/_thu/route.tsx → có mục menu và route` (nạp bằng bản sao y nguyên `src/app/screens.ts` trong thư mục tạm) | `npx vitest run src/app/screens.test.tsx` | `Tests 16 passed (16)` |
| Hai thư mục màn trùng path → lỗi nêu path và hai tệp | `… › hai thư mục màn trùng path → nạp báo lỗi nêu cả hai tệp`; `loadScreens kiểm quy ước › trùng path giữa hai tệp` | như trên | qua |
| Tệp sai quy ước (không xuất `screens`, rỗng, thiếu/sai `path`, `label`, `group`, `permission`, `order`, `element`, `also`, `hidden`) → lỗi nêu tệp và trường | `loadScreens kiểm quy ước › %s → lỗi nêu tên tệp và trường` (11 ca) | như trên | qua. Thêm kiểm lúc biên dịch: route.tsx khai `readonly ScreenRoute[]` nên thiếu trường là lỗi `tsc` |
| Không đổi hành vi: đủ 7 route cũ, đúng path, nhãn, nhóm, quyền, `also`, `hidden`, thứ tự menu; màn đầu (`/` → `/san-pham`) như cũ | `SCREENS nạp từ src/screens/*/route.tsx › đủ các màn đã có…`; `xếp theo nhóm menu rồi order` | như trên | qua |
| Danh sách cũ đã xóa, glob chạy trong bản dựng | — | `git rm src/app/screens.tsx`; `npx vite build` | `✓ built in 392ms` |
| Cổng console + e2e hiện có vẫn qua | toàn bộ | `npx tsc --noEmit && npx eslint . && npx prettier --check . && npm test`; `CRM_E2E_PORT=5193 npx playwright test` | không lỗi; `Test Files 17 passed (17)`, `Tests 190 passed (190)`; `68 passed (17.4s)` |

## Quyết định thay chủ shop

1. **Thư mục màn là `src/screens/<tên>/` và dời luôn mã màn vào đó** (đúng kế hoạch: "dời các màn đã có sang quy ước này", CSS riêng nằm trong thư mục màn). Phương án khác: giữ mã ở `src/features/` và chỉ đặt `route.tsx` ở `src/screens/` — bỏ vì tách màn làm hai nơi. Dễ đảo ngược: chỉ là `git mv` (commit 1 riêng).
2. **Một `route.tsx` khai được nhiều route** (`export const screens: readonly ScreenRoute[]`), vì một màn có trang con (danh sách + sửa mẫu, nhân viên + quyền, vai trò + sửa vai trò) dùng chung CSS và mã. Phương án khác: mỗi trang con một thư mục — bỏ vì phải tách thư mục đang dùng chung.
3. **Giữ tên trường cũ `label`, `group`, `permission`** (kế hoạch viết mô tả "title, nhóm menu, quyền cần"), thêm `order`. Đổi tên sẽ chạm `Layout`, `router`, test khung mà không thêm giá trị. `order` chỉ có trong `ScreenRoute` (kiểu của tệp màn); `Screen` mà khung dùng không đổi.
4. **Thứ tự**: xếp theo nhóm menu (`NAV_GROUPS`), rồi `order`, rồi `path`. Màn đầu tiên khi mở `/` vẫn là `/san-pham` như cũ.
5. **Kiểm quy ước lúc nạp, không chỉ lúc biên dịch**: kiểu chung của `import.meta.glob` không kiểm được tệp quên khai kiểu, nên `loadScreens` kiểm từng trường và ném lỗi nêu tên tệp; ứng dụng không chạy với màn thiếu quyền. Kiểm bằng type guard, không ép kiểu (luật console 3).
6. **Test "thêm màn thử" chạy trong thư mục tạm `src/probe-<uuid>/`** (bản sao y nguyên `src/app/screens.ts` + `screens/_thu/route.tsx`) thay vì ghi thật vào `src/screens/_thu/`: ghi thật sẽ lẫn vào lần nạp của test khác chạy song song và dev server đang mở. Bản sao từng byte nên vẫn chứng minh đúng mẫu glob thật.
7. **`styles.css` không tách thêm**: phần còn lại đều là lớp chung chép từ `m1.css` (có tiêu đề "Lớp chung…"); CSS riêng của màn đã ở trong thư mục màn. `.toggle` hiện chỉ màn Tồn kho dùng nhưng là lớp của hệ thiết kế `m1.css`, giữ chung. `roles` vẫn nhập `../staff/staff.css` (dùng chung lớp với Nhân viên) như trước.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Step 1 ghi màn thử ở `console/src/screens/_thu/route.tsx`; test đặt nó trong thư mục tạm `src/probe-<uuid>/screens/_thu/` cùng bản sao y nguyên `src/app/screens.ts` (lý do: Quyết định 6).
- Tên trường giữ `label`, `group`, `permission` thay vì "title, nhóm menu, quyền cần" của kế hoạch (Quyết định 3).

## Việc phát hiện thêm

- Kế hoạch M1 (`docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`) còn ghi đường `console/src/features/...` cho Task 50b–50d, 52, 53, 325–331. Sau task này đường đúng là `console/src/screens/<tên>/` (kèm `route.tsx`). Không sửa kế hoạch trong task này (ngoài phạm vi); điều phối viên sửa khi giao các task đó.
- (Ghi chú reviewer commit 2) Nếu vitest bị giết giữa chừng, thư mục tạm `console/src/probe-*` có thể sót lại; có thể thêm `probe-*` vào `.gitignore` của console. Chưa làm (không ảnh hưởng tính đúng).
- (Ghi chú reviewer commit 2) `loadScreens` kiểm trùng `path` nhưng không kiểm `also` của màn này trùng `path` màn khác (danh sách cũ cũng không kiểm).

## Reviewer

Commit 1 (cấu trúc, dời thư mục): **ĐẠT**. Không có phát hiện phải sửa; 27 tệp dời giống 100%, chỉ đổi 7 dòng import trong `screens.tsx`, không còn chỗ trỏ `features/`. Ghi chú không bắt buộc: kế hoạch còn ghi `features/` cho task sau → đã ghi ở "Việc phát hiện thêm".

Commit 2 (màn tự đăng ký): **ĐẠT**. Không có phát hiện phải sửa. Đã đối chiếu 7 route với `screens.tsx` cũ (path, nhãn, nhóm, quyền, also, hidden, thứ tự menu, màn đầu `/san-pham`); màn thiếu `permission` làm hỏng lần nạp nên không lọt; không `as`/`any` ngoài test; test `_thu` render qua `Layout` và `AppRoutes` thật. Ghi chú không bắt buộc (mục "Khác kế hoạch" thiếu dòng cho thư mục thử → đã thêm; `.gitignore` cho `probe-*`, trùng `also` → ghi ở "Việc phát hiện thêm").

Kết luận: ĐẠT
