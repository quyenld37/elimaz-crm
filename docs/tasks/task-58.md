# Task 58 — M2-01: Quyền M2, vai trò mẫu

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 58, M2-01). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào; chỉ đổi **giá trị** của `identity.Permissions` (thêm 15 phần tử) và `identity.DefaultRoles` (thêm quyền M2 vào sale, warehouse, accountant; owner, manager tự có theo luật). Chỗ đọc `Permissions` ngoài gói (grep `identity.Permissions`): `tests/arch/routes_test.go` (route khai quyền phải có trong danh sách — chưa route M2 nào), `tests/e2e/seed_test.go` (chủ có mọi quyền), `internal/httpapi/handlers_identity_test.go:461` (số mục danh sách quyền = `len(Permissions)`). `DefaultRoles`: `tests/e2e/migrate_test.go`, `internal/httpapi/handlers_identity_test.go:284-287` (so theo `DefaultRoles`, tự theo). Trong gói: `defaultRoles`, `IsKnown`, `KindOf`, `Can`, `PermissionCatalog` (cần nhãn cho 15 quyền mới: `permissionLabels`).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/adapters/cfaccess`, `internal/httpapi`, `internal/modules/catalog`, `internal/platform/files`, `internal/platform/realtime`, `internal/testkit`, `tools/loadkit`; cộng `tests/arch`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: không (route M2 khai ở Task M2-02). `GET /api/v1/identity/permissions` trả thêm 15 mục (dữ liệu, không đổi hợp đồng).
- Chỗ dùng trên màn hình: màn Vai trò (M1-56) và Nhân viên đọc danh sách quyền từ API: hiện thêm 15 ô quyền trong nhóm "Đơn hàng", "Khách hàng", "Khuyến mại…". Không sửa console.
- Câu sqlc, bảng, pha migration: không câu sqlc, không bảng, không migration (Q-T57-6: không có `0200`; tenant cũ nhận quyền qua `SyncDefaultRoles` sau migrate).
- Test sẽ chạy: `go test -race ./internal/modules/identity/...` (vùng rủi ro cao: cả bộ identity), `./internal/httpapi/...` (danh sách quyền, vai trò), `./tests/arch/...`, `./tests/e2e/...` (seed, migrate đồng bộ vai trò); cuối cùng `make ci` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (identity: `DefaultRoles`, `EnsureDefaultRoles`, `SyncDefaultRoles`, `PermissionCatalog`); grep `RoleTemplates|EnsureRoleTemplates` — không có, không tạo.
- Dùng lại / viết mới vì: dùng lại nguyên `DefaultRoles` (chỉ thêm khóa vào danh sách), `EnsureDefaultRoles`/`SyncDefaultRoles` (không đổi mã — luật "chỉ thêm, customized không nhận, owner luôn đủ" đã đúng việc migration 0200 định làm), bảng nhãn `permissionLabels` và nhóm có sẵn (`groupOrder`, `groupCustomer`, `groupPromo`). Không viết hàm mới. Test dùng lại `rolesTenant`, `ensureRoles`, `listRoles`, `countRoleRows`, `testkit.Staff`.

## Soát thiết kế

- 5.1 chủ dữ liệu: quyền và vai trò thuộc `identity` (04 mục 3); chỉ sửa trong module này.
- 5.2, 5.3: không áp dụng: không ghi, không đọc bảng của module khác.
- 5.4, 5.5, 5.6: không áp dụng: không có đường ghi nghiệp vụ, sự kiện hay gọi ra ngoài mới.
- 5.7 ngữ cảnh shop: `EnsureDefaultRoles` chạy trong `tenancy.WithTenant` (có sẵn, không đổi).
- 5.8, 5.9: không áp dụng: không route, không HTTP mới.
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12 module mẫu: theo mẫu identity (test gói `_test` trên PostgreSQL thật, rapid cho `Can`).
- 6.1 sửa đồng thời: `EnsureDefaultRoles` khóa dòng vai trò FOR SHARE trước khi đọc `customized` (có sẵn); thêm quyền là `INSERT … ON CONFLICT DO NOTHING`. Không đổi.
- 6.2 gửi lại: chạy lại `SyncDefaultRoles` không đổi gì (test chạy hai lần).
- 6.3 thứ tự khóa: không đổi (ADR-46 nhóm 2: roles → role_permissions).
- 6.4–6.10: không áp dụng: không đường yêu cầu, việc nền, tiền mới.
- 6.11 quy luật: thuộc tính `Can` (rapid, 200 lượt) chạy với danh sách quyền mới.
- 6.12: không áp dụng: không dữ liệu cá nhân.
- 6.13 quyền: mặc định từ chối giữ nguyên; vai trò đã sửa (`customized`) và vai trò tự định nghĩa không tự nhận quyền M2; hai quyền ghi chính sách toàn shop là `ScopeShop` (xem Quyết định thay chủ shop).

## Quyết định thay chủ shop

1. **Loại phạm vi của `promotion.manage`, `order.settings.manage` là `ScopeShop` (toàn shop), 13 quyền M2 còn lại `ScopeNone`.** Kế hoạch (Q6) ghi "mọi quyền M2 là `ScopeNone`: cần grant phạm vi `all`" — viết trước Task 315; sau Task 315, `ScopeNone` có hiệu lực từ grant **bất kỳ** phạm vi nào, còn "chỉ hiệu lực ở `all`" là `ScopeShop`. Luật đã duyệt trong `docs/quyen.md` (Task 315 lượt 2): "Quyền mới ghi chính sách áp cho cả shop (cài đặt shop, giá chung, mã giảm giá, …) là loại `toàn shop`". Tạo, sửa mã giảm giá và cài đặt đơn, nguồn đơn là đúng loại đó. Chọn vậy vì khớp luật mới hơn và ý của Q6 ("cần grant `all`"), an toàn hơn (hẹp hơn). Phương án khác: để cả 15 là `ScopeNone` đúng chữ kế hoạch — nhân viên được gán Quản lý cho một kho sẽ sửa được mã giảm giá toàn shop. Đảo lại: đổi hai dòng trong `Permissions` và `docs/quyen.md` (nới, không cần migration).
2. Nhãn tiếng Việt cho màn Vai trò (`permissionLabels`) của 15 quyền tự đặt theo việc trong bảng route M2 (Task M2-02); sửa chữ sau không ảnh hưởng gì khác.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi viết mã | 5 test dưới | `go test -race ./internal/modules/identity/...` (trước khi sửa `permissions.go`, `roles.go`) | `FAIL TestPermissionsList: mong 37 quyền, có 22`; `TestDefaultRoles_Content`, `TestEnsureDefaultRoles_M2` ("owner thiếu customer.read"), `TestEnsureDefaultRoles_M2AddOnly`, `TestSyncDefaultRoles_M2` đỏ |
| 15 quyền mới đúng loại phạm vi, không khóa trùng | `TestPermissionsList` | `go test -race -run '…' -v ./internal/modules/identity/` | `--- PASS: TestPermissionsList` |
| Vai trò mẫu đúng bảng kế hoạch | `TestDefaultRoles_Content` | như trên | `--- PASS` |
| Tenant mới: 5 vai trò; sale không có `customer.merge`, `order.discount.approve`, `order.return.receive`; warehouse có `order.return.receive`; manager, owner đủ 15 | `TestEnsureDefaultRoles_M2` | như trên | `--- PASS (2.08s)` |
| Gọi hai lần không đổi số dòng; xóa tay `order.cancel` khỏi sale → thêm lại; thêm tay `customer.merge` → còn | `TestEnsureDefaultRoles_M2AddOnly` | như trên | `--- PASS (1.80s)` |
| `SyncDefaultRoles` (thay 0200): owner customized nhận đủ 15, chủ `LoadUser` → `Can` đúng cả 15 (có `customer.merge`); warehouse customized và `custom_ab12cd34` không nhận gì | `TestSyncDefaultRoles_M2` | như trên | `--- PASS (1.74s)` |
| `docs/quyen.md` khớp mã (khóa, loại phạm vi, vai trò mẫu) | `TestPermissionsDoc_MatchesCode` | như trên | `--- PASS`; lần đầu đỏ vì dòng `order.read` thiếu `warehouse` → đã sửa |
| Nhãn màn Vai trò đủ cho quyền mới | `TestPermissionCatalog_Complete` | như trên | `--- PASS` |
| Thuộc tính `Can` (rapid 200 lượt) với danh sách mới | `TestCanProperty` | như trên | `--- PASS` |
| Không vỡ test cũ của identity | cả gói | `go test -race ./internal/modules/identity/...` | `ok … identity 12.0s` (chạy chọn lọc), cả bộ trong `make ci` |
| Cổng kiểm toàn bộ | `make ci` | `make ci CRM_PG_PORT=5600` (sau `git merge main`: "Already up to date") | `exit=0`; `ok …/modules/identity 151.7s`, `ok …/httpapi 121.1s`, `ok …/tests/arch 41.6s`, `ok …/tests/e2e 277.5s`; gitleaks "no leaks found" |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `promotion.manage`, `order.settings.manage` là `ScopeShop`, không `ScopeNone` (Quyết định thay chủ shop 1).
- Sửa một ô của dòng M1 `order.read` trong `docs/quyen.md` (cột "Vai trò mẫu nhận" thêm `warehouse`, việc ghi "Kho nhận từ M2"): kế hoạch bảo "không sửa dòng của M0, M1", nhưng bảng kế hoạch cho Kho nhận `order.read` và `TestPermissionsDoc_MatchesCode` đòi cột này khớp `DefaultRoles`. Không đổi khóa, loại phạm vi.
- Thêm câu "chính sách toàn shop của M2: …" vào đoạn liệt kê quyền `toàn shop` của `docs/quyen.md`.
- Kế hoạch gọi `sale`, `warehouse`, `accountant` theo bảng; nhãn tiếng Việt cho 15 quyền (`permissionLabels`) không có trong kế hoạch nhưng test `TestPermissionCatalog_Complete` của M1-56 đòi.

## Việc phát hiện thêm

- Câu cảnh báo `admin_needs_shop_scope` ("Quyền quản trị chỉ hiệu lực khi gán toàn shop") giờ cũng nói về `promotion.manage`, `order.settings.manage` (chính sách, không hẳn "quản trị"). Đúng về hành vi; chữ trên màn có thể đổi thành "Quyền quản trị, chính sách shop…" ở Task M2-30 (màn Mã giảm giá, Cài đặt đơn) nếu chủ shop muốn.

## Reviewer

Kết luận: ĐẠT (lượt 1, trên `git diff --cached` sau `make ci` xanh).

- Không có phát hiện phải sửa. Lệch kế hoạch `ScopeShop` cho `promotion.manage`, `order.settings.manage`: reviewer xác nhận đúng luật `docs/quyen.md` và ý "cần grant `all`" của Q6; hẹp hơn, không cần migration; Quản lý theo kho vốn đã có cảnh báo `admin_needs_shop_scope` (vì `audit.read`), không phải hành vi mới.
- Mọi test kiểm đúng điều nó nói (đọc database thật, `TestSyncDefaultRoles_M2` kiểm giả lập tenant cũ có hiệu lực trước khi đồng bộ).
- Ghi chú không bắt buộc (không sửa): (1) vai trò Kho có `order.read` (`ScopeNone`) nên nhân viên Kho gán theo một kho vẫn xem mọi đơn, kể cả SĐT, địa chỉ khách — đúng bảng kế hoạch và Q6 (lọc theo phạm vi để M4), chủ shop nên biết; (2) `TestSyncDefaultRoles_M2` chỉ kiểm sale trong các vai trò chưa sửa (cùng đường mã với manager, accountant); (3) chữ cảnh báo — đã ở "Việc phát hiện thêm".
