# Task 519 — Menu Tệp xuất nhận nhiều quyền (vai kho)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi: không có (không chạm Go). Ký hiệu TypeScript: interface `Screen` (console/src/app/nav.ts) thêm trường tùy chọn `orPermissions`; `canSee` đổi logic; `loadScreens.parseScreen` đọc thêm trường. Tham chiếu: canSee dùng ở nav.ts (allowedScreens), AppRoutes; Screen dùng ở screens.ts, test.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi Go).
- Operation OpenAPI bị chạm: không. Đã đối chiếu máy chủ: list/get/file của `/reports/sales/runs` đòi `report.sales.read`, của `/reports/inventory/runs` đòi `report.inventory.read`, của customers đòi `customer.export`; mỗi họ lọc theo `Family` (handlers_reports_runs.go) và test httpapi `TestReportRuns_HTTPErrors` đã có ca "không quyền" (kho → sales runs 403). Máy chủ đã đúng → chỉ sửa console.
- Chỗ dùng trên màn hình: menu "Tệp xuất" (screens/reports/route.tsx), route khung AppRoutes. RunsPage đã lọc từng họ theo quyền (FAMILIES) nên vai kho chỉ thấy họ tồn kho; ExportsNavCount tương tự.
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: vitest src/app/screens.test.tsx, src/screens/reports/route.test.tsx, RunsPage.test.tsx, tsc, eslint tệp sửa.

## Tái dùng

- Đã kiểm: danh mục Go không liên quan (chỉ console); đã có `alsoRequires` (và) ở Screen, `hasPermission`, `reportAccess.can`.
- Dùng lại `hasPermission`; thêm `orPermissions` (HOẶC) vì `alsoRequires` là VÀ, không biểu diễn được "một trong".

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

5.x: không áp dụng — chỉ console (không database, tiền, hàng đợi, khóa, giao dịch). Quyền thật vẫn do máy chủ kiểm từng lệnh; menu chỉ ẩn/hiện. 6.x: không áp dụng — không có trạng thái chung hay ghi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| canSee nhận quyền chính HOẶC quyền thay thế, vẫn đòi alsoRequires | screens.test.tsx "quyền thay thế của màn (orPermissions)" (4 ca) | `cd console && npx vitest run src/app/screens.test.tsx src/screens/reports src/screens/ops-lists` | 20 file, 206 test qua |
| Tệp xuất: vai kho thấy, vai không quyền báo cáo thì không; menu vai kho = Tồn + Tệp xuất | route.test.tsx "Tệp xuất: vai kho…" | như trên | qua |
| Máy chủ lọc đúng loại tệp theo quyền | `TestReportRuns_HTTPErrors` ca "không quyền" (kho → sales runs 403) + x-permission từng họ ở openapi.yaml | đọc mã, không chạy lại (không đổi Go) | đúng sẵn |
| 409 rồi lưu lại gửi version mới (Task 135) | CallbacksPage.test.tsx "409 version_conflict: … lưu lại gửi version mới" (đỏ trước khi sửa: version 1; xanh sau) | vitest src/screens/ops-lists | qua |
| Thân newAddress khi gửi đi | CallbacksPage.test.tsx "'Giao lại tới địa chỉ mới': thân gửi newAddress…" | như trên | qua |
| tsc, eslint | | `npx tsc --noEmit`; `npx eslint <tệp sửa>` | sạch |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không gộp Dashboard và MovementsReport sang thành phần chung (SalesReportPage, useReportPeriod, CellNumber): đổi lớn hai màn báo cáo, rủi ro hồi quy lớn hơn lợi, lệnh giao chỉ cho làm "khi nhỏ và an toàn". Ghi lại ở Việc phát hiện thêm.

## Quyết định thay chủ shop

- QĐ-519-a: thêm trường `orPermissions` (HOẶC) cho Screen thay vì đổi `permission` thành mảng: không phá các route.tsx hiện có, `alsoRequires` giữ nghĩa VÀ. Phương án khác: `permission: string | string[]` (đổi mọi chỗ đọc).
- QĐ-519-b: lỗi 409 ở hộp "Ghi kết quả cuộc gọi" / "Giao lại": hộp lấy việc mới nhất từ danh sách vừa tải lại (version mới) thay vì đóng hộp; người dùng thấy câu "người khác vừa sửa" và bấm Lưu lại được. Phương án khác: đóng hộp kèm báo.
- QĐ-519-c: điều kiện "M1–M5 đóng" của M6 được miễn (chủ shop duyệt 27/11).

## Việc phát hiện thêm

- Gộp Dashboard và MovementsReport sang SalesReportPage/useReportPeriod/CellNumber chưa làm (xem Khác kế hoạch); nên là task riêng.

## Reviewer

Kết luận: ĐẠT. Không có phát hiện ảnh hưởng tính đúng. Ghi chú: mục Ảnh hưởng bổ sung CallbacksPage.tsx (biến `fresh`, cũng áp cho AssignDialog) và opsTestkit.tsx (route /admin-units giả); ca 409 bấm Lưu trong waitFor có thể chập chờn nếu cache chưa cập nhật (test chạy xanh ở ci-task).
