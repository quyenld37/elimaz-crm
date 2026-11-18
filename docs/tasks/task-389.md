# Task 389 — Test đăng ký màn hình theo từng thư mục màn (M1-x3)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 389, M1-x3). Loại việc (quy trình mục 3): D (dọn cấu trúc test, không đổi hành vi màn hình)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task không chạm mã Go.
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói Go).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không đổi mã màn nào. Chỉ đổi test: `console/src/app/screens.test.tsx` (bỏ danh sách viết cứng, thay bằng kiểm quy ước trên mọi `src/screens/*/route.tsx` tìm bằng `import.meta.glob`); thêm `console/src/screens/{products,inventory,errors,staff,roles}/route.test.tsx` (mỗi màn tự kiểm path, nhãn, nhóm, quyền, trang ẩn, `also`, `order` của mình). Thêm `console/src/app/routeTestkit.ts` (`routeRows`: chỉ test nhập, đổi một màn thành một dòng để so; tránh 5 đoạn map giống nhau mà jscpd báo trùng). Mã `loadScreens.ts`, `screens.ts`, `nav.ts`, các `route.tsx` không đổi. Tệp nhắc tới `screens.test.tsx`: chỉ `docs/tasks/task-393.md`, `task-330.md` (lịch sử, không sửa).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `cd console && npx vitest run src/app src/screens`, `npx tsc --noEmit`, `npx eslint .`; cổng kiểm của task (ci-task.sh) một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có mã Go; phía console đã có `loadScreens` (kiểm đủ trường, nhóm hợp lệ, trùng path, xếp thứ tự) và `NAV_GROUPS`. Bảng quyền chuẩn là `docs/quyen.md` (Go có `TestPermissionsDoc_MatchesCode` giữ khớp với `identity.Permissions`); console chưa có danh sách quyền tĩnh nào (API `GET /identity/permissions` chỉ có lúc chạy).
- Dùng lại / viết mới vì: test chung gọi lại `loadScreens` cho phần đủ trường, nhóm, trùng path (không chép lại luật); chỉ viết mới hai luật mà `loadScreens` không kiểm và task đòi: quyền phải có trong `docs/quyen.md` (đọc bảng từ tệp, không chép danh sách quyền vào test), `order` không trùng giữa các mục menu hiện (không `hidden`) trong cùng nhóm. Hai luật này chỉ nằm trong test (task chỉ đổi cấu trúc test, không đổi hành vi nạp màn).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng — task chỉ đổi tệp test của console, không chạm database, tiền, quyền thật, hàng đợi, API, module Go. Về quyền: test mới chỉ **đọc** `docs/quyen.md` để kiểm màn khai quyền có thật; quyền thật vẫn do API kiểm.
- 6.1–6.13: không áp dụng — không có mã chạy đồng thời, transaction hay khóa. Test probe tạo thư mục tạm tên ngẫu nhiên (`probe-<uuid>`) như trước, nên chạy song song không đụng nhau.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test chung không còn danh sách màn viết cứng; kiểm quy ước trên mọi `route.tsx` tìm bằng glob (đủ trường, nhóm, không trùng path, quyền thuộc `docs/quyen.md`, `order` không trùng trong nhóm) | `screens.test.tsx` › "quy ước trên mọi src/screens/*/route.tsx" (4 ca) | `cd console && npx vitest run src/app/screens.test.tsx src/screens/*/route.test.tsx` | `Test Files 6 passed (6)`, `Tests 30 passed (30)` |
| Luật mới bắt được màn sai | `screens.test.tsx` › "conventionProblems bắt màn sai quy ước" (5 ca: đúng, quyền lạ, trùng order, trang ẩn/khác nhóm được trùng, lỗi của loadScreens) | như trên | qua |
| Thêm màn thử đúng quy ước → test chung không sửa vẫn kiểm được màn đó | `screens.test.tsx` › "thêm thư mục màn mới…" › "màn thử được test quy ước chung kiểm luôn…" (+ 2 ca probe cũ giữ nguyên) | như trên | qua |
| "Màn X có đường Y, quyền Z" dời vào thư mục từng màn, giữ đủ 8 dòng của test cũ (thêm cột `order`) | `src/screens/{products,inventory,errors,staff,roles}/route.test.tsx` | như trên | 5 tệp qua |
| Test thật sự bắt lỗi | đột biến tạm `errors/route.tsx` (quyền `platform.health.xem`) | như trên, rồi `git checkout` tệp | 2 ca đỏ: test quy ước báo `khai quyền "platform.health.xem" không có trong docs/quyen.md`, `errors/route.test.tsx` đỏ |
| Không đổi hành vi, cổng tĩnh xanh | toàn bộ test console + tsc + eslint + jscpd + test kiến trúc | `ci-task.sh` | `Test Files 29 passed`, `Tests 294 passed`; jscpd 0 đoạn trùng mới; `CI-TASK XANH` |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

- Luật "thứ tự không trùng trong nhóm" áp cho mục menu **hiện**; trang ẩn (`hidden: true`) được dùng lại `order` của màn cha. Vì sao: các trang con hiện có (`/san-pham/:id`, `/nhan-vien/:id/quyen`, `/vai-tro/:id`) đang cùng `order` với màn cha, mà task không đổi hành vi màn; trang ẩn không có chỗ trên menu nên `order` của nó không ảnh hưởng gì. Phương án khác: bắt mọi màn khác `order` (phải sửa 3 `route.tsx`, trái "không đổi hành vi").
- Bảng quyền lấy từ `docs/quyen.md` (đọc tệp lúc test) thay vì chép danh sách quyền vào test: bảng này là bảng chuẩn, Go đã giữ nó khớp `identity.Permissions`; chép lại sẽ thành một danh sách chung nữa mà mọi task thêm quyền phải sửa. Phương án khác: gọi API `GET /identity/permissions` (cần máy chủ chạy, không hợp test đơn vị).
- Hai luật mới (quyền thuộc bảng, `order` không trùng) chỉ nằm trong test, không đưa vào `loadScreens`: task chỉ đổi cấu trúc test. Đưa vào lúc nạp được nếu chủ shop muốn (việc riêng).

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1). Không có phát hiện phải sửa. Reviewer đối chiếu đủ 8 dòng của danh sách cũ (path, nhãn, nhóm, quyền, hidden, also) với 5 tệp `route.test.tsx`, cột `order` khớp các `route.tsx`; regex bảng quyền khớp 37 dòng, đều trong bảng của `docs/quyen.md`; số ca 30 khớp. Ghi chú không bắt buộc: `routeTestkit.ts` nằm ở `src/app` (chỉ test dùng, theo tiền lệ `productsTestkit.tsx`); hai luật mới chỉ chạy trong test (đã ghi ở Quyết định thay chủ shop).
