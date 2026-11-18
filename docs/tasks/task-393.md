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
- Dùng lại / viết mới vì: dùng lại `Screen`, `NAV_GROUPS`, `navSections`, `AppRoutes` như cũ; tự đăng ký dùng `import.meta.glob` có sẵn của Vite (đúng kế hoạch, không thêm thư viện). Viết mới một hàm `loadScreens` (kiểm quy ước lúc nạp: thiếu `screens`, thiếu/sai `path`, `label`, `group`, `permission`, `order`, `element`, trùng `path` giữa hai tệp → lỗi nêu tên tệp) đặt ở `src/app/screens.ts`, vì chỉ khung dùng; không đưa vào `src/shared`. CSS: `styles.css` hiện chỉ còn lớp chung chép từ `m1.css` (khung, nút, ô nhập, bảng, danh sách, nút bật tắt, biểu mẫu hai cột); CSS riêng đã nằm trong thư mục màn (`products.css`, `inventory.css`, `staff.css`, `roles.css`) và dời theo thư mục.

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

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Kế hoạch M1 (`docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`) còn ghi đường `console/src/features/...` cho Task 50b–50d, 52, 53, 325–331. Sau task này đường đúng là `console/src/screens/<tên>/` (kèm `route.tsx`). Không sửa kế hoạch trong task này (ngoài phạm vi); điều phối viên sửa khi giao các task đó.

## Reviewer

Commit 1 (cấu trúc, dời thư mục): **ĐẠT**. Không có phát hiện phải sửa; 27 tệp dời giống 100%, chỉ đổi 7 dòng import trong `screens.tsx`, không còn chỗ trỏ `features/`. Ghi chú không bắt buộc: kế hoạch còn ghi `features/` cho task sau → đã ghi ở "Việc phát hiện thêm".

Kết luận: ĐẠT
