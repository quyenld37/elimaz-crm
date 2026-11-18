# Task 56 — Vai trò tự định nghĩa (API và màn)

Tiêu đề: Task 56 (M1-56). Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (mục "Task 55 (M1-56)"). Mockup đã duyệt: `docs/mockups/m1/vai-tro.html` (+ `png/vai-tro-{1512,390,360}.png`). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (luật quyền, `internal/modules/identity`).

## Ảnh hưởng

**Lượt 2** (gộp main lần 3; quyền xem danh sách vai trò):
- Gộp main (Task 22 thử tải, 51 màn Tồn kho, 324 màn sửa mẫu): xung đột `console/e2e/staff.spec.ts` (test menu: giữ cả
  bản của main — mở "/" có khai API sản phẩm — lẫn bản của task — mở `/nhan-vien` thấy câu không có trang) và mã sinh
  (`oas_router_gen.go`, `console/src/api/{index,sdk.gen}.ts`: lấy một bên rồi `make gen` từ `openapi.yaml` đã gộp).
- Sau gộp, jscpd báo `noHorizontalScroll` trùng giữa `console/e2e/inventory.spec.ts` (main) và `roles.spec.ts`: dời vào
  `console/e2e/fixtures.ts`, hai spec import.
- Ký hiệu Go: không đổi ký hiệu xuất ra; `handler` thêm phương thức `ListRolesToManage` (ogen sinh interface mới,
  1 hiện thực). Phần chung đọc vai trò của `ListRoles` tách thành `(*handler).roles` (2 chỗ gọi).
- Operation OpenAPI: mới `listRolesToManage` (GET /api/v1/identity/roles/manage, `identity.roles.manage`, cùng schema
  `Role`); `listRoles` giữ nguyên (`identity.users.manage`, màn Nhân viên cần để gán vai trò). Thêm route: oasdiff
  không phá vỡ. Guard (vùng cao) không đổi: mỗi route vẫn một quyền.
- Màn: `console/src/features/roles/roleData.ts` gọi `listRolesToManage`, khóa `["identity.roles","manage"]` (con của
  khóa `identity.roles` nên `invalidateQueries(ROLES_KEY)` sau khi ghi làm mới cả hai màn); `console/e2e/roles.spec.ts`
  (khai đường mới; ca người chỉ có `identity.roles.manage`).
- sqlc, bảng, migration: không đổi (dùng `identity.ListRoles` có sẵn).
- Test sẽ chạy: `go test -race ./internal/httpapi/ -run 'RolesAPI|Route'`, `./tests/arch/...` (bảng route), `make gen`;
  console `npx vitest run`, `tsc`, `eslint`, Playwright `e2e/roles.spec.ts`, `e2e/staff.spec.ts` (chromium); `make ci`
  một lần sau gộp main (commit 1) và một lần trước review commit 2.

Lượt 1:

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm `identity.RoleInput`, `CreateRole`,
  `UpdateRole`, `ArchiveRole`, `RoleChange`, `PermissionInfo`, `PermissionCatalog`, sentinel `ErrOwnerRoleLocked`,
  `ErrRoleInUse`, `ErrUnknownPermission`, `ErrRoleNameTaken`, `ErrRoleArchived`, `ErrRoleNotFound`. Đổi:
  `identity.RoleRow` thêm trường `Version`, `HolderCount` (không đổi trường cũ; 1 chỗ dùng ngoài gói:
  `internal/httpapi/handlers_identity.go` ListRoles); `ReplaceGrants` (giữ chữ ký; thêm khóa vai trò `FOR SHARE` và
  lỗi `ErrRoleArchived` cho grant mới thêm của vai trò đã lưu trữ; 1 chỗ gọi ngoài gói: handlers_identity.go).
  `LoadUser` giữ chữ ký nhưng câu `ListUserGrants` bỏ quyền của vai trò đã lưu trữ (dùng ở `users.go`, `staff.go`).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/adapters/cfaccess`, `internal/httpapi`, `internal/modules/catalog`,
  `internal/platform/files`, `internal/platform/realtime`, `internal/testkit` (hook Stop tự test các gói này).
- Operation OpenAPI bị chạm, oasdiff: mới `createRole` (POST /api/v1/identity/roles), `updateRole` (PUT
  /api/v1/identity/roles/{id}), `archiveRole` (POST /api/v1/identity/roles/{id}/archive) — đều `identity.roles.manage`
  + `IdempotencyKey`; `listPermissions` (GET /api/v1/identity/permissions, `identity.roles.manage`). `listRoles` thêm
  trường trả về `id`, `version`, `holderCount` (thêm trường ở câu trả lời: không phá vỡ). Mã lỗi `owner_role_locked`,
  `role_in_use`, `role_name_taken`, `unknown_permission`, `role_archived` đã có trong `x-error-codes` (Task 26/48).
- Chỗ dùng trên màn hình: mới `console/src/features/roles/{RoleList.tsx,RoleEdit.tsx,roleRules.ts,...}`, màn "Vai trò"
  trong `console/src/app/screens.tsx` (nhóm Quản trị, quyền `identity.roles.manage`); `console/e2e/roles.spec.ts`;
  `console/e2e/staff.spec.ts` (kiểu `Role` có thêm trường bắt buộc); `StaffGrants.tsx` đã lọc vai trò `active` và
  đánh "(tự tạo)" — chỉ kiểm lại, đổi nếu cần. `errors.ts` đã có câu cho mọi mã lỗi mới.
- Câu sqlc, bảng, pha migration: mới `queries/identity/roles.sql` (tạo, sửa tên, thêm/gỡ quyền, khóa vai trò, đếm
  người giữ, lưu trữ); `queries/identity/staff.sql` `ListRolesWithPermissions` thêm `version`, đếm người giữ; thêm câu
  khóa vai trò `FOR SHARE`; `queries/identity/users.sql` `ListUserGrants` lọc `r.status = 'active'`. Bảng
  `identity.roles`, `role_permissions`, `user_role_grants`, `audit_logs` (chỉ chèn). Cột `customized`, `status`,
  `version` có từ 0113; thêm migration `0128_identity_custom_roles.sql` chỉ GRANT cho `crm_app` (pha mở rộng; Quyết
  định 8, phát hiện khi test chạy bằng `crm_app`).
- Đổi cấu trúc không đổi hành vi (commit riêng, trước commit tính năng): `internal/httpapi/handlers_identity.go`
  `staffWrite` dùng hàm chung `identityWrite[O]` (+ `written`, `snapshot`, `record`) để đường ghi vai trò dùng lại;
  `internal/modules/identity/staff.go` tách `freshGrants` khỏi `afterChecks`, `nonNil` thành generic.
- Test sẽ chạy: cả bộ `go test -race ./internal/modules/identity/...` (vùng cao), `./internal/httpapi/...`,
  `./tests/arch/...` (routes, permissions doc), gói phụ thuộc ngược qua hook; console `npm test`, `tsc`, `eslint`,
  Playwright `e2e/roles.spec.ts` và `e2e/staff.spec.ts` (Chromium, 1512 và 390); `make ci` một lần trước review.

## Tái dùng

- Lượt 2: dùng lại `identity.ListRoles`, `roleOut`, `tenancy.WithTenant` (tách thân `ListRoles` thành `h.roles` để
  hai operation dùng chung, tránh trùng); schema `Role` có sẵn; `fixtures.ts` `api.on`. Không viết mới hàm nghiệp vụ.

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục identity, httpapi
  (`WriteTx`, `RecordAudit`, `authUser`), console shared (`useWriteAction`, `unwrap`, `errorMessage`, `queryKey`);
  trong identity: `Can`, `hasAllPermissions`, `roleIndex`, `isUniqueViolation`, `ListRoles`, mẫu `staffWrite`.
- Dùng lại / viết mới vì: luật "có quyền ở phạm vi all" dùng `Can(actor, p, &Scope{all})` (cùng hàm của
  `hasAllPermissions`); đọc lại quyền người thao tác trong transaction dùng câu `ListUserGrants` có sẵn; ghi qua
  `WriteTx` + `RecordAudit` qua hàm chung `identityWrite` (tách từ `staffWrite`, vì jscpd/dupl báo hai đường ghi
  trùng: cùng mục đích — ghi identity kèm audit, lỗi nghiệp vụ không lưu khóa — và cùng lý do đổi); màn dùng `useWriteAction`, `RefusalBox`, `Dialog` của
  `features/staff/staffData.tsx` (dời không cần: import chéo hai màn cùng nhóm Quản trị). Viết mới: `roles.go` phần
  tự định nghĩa (luật riêng của vai trò, khác luật gán của `staff.go`), bảng nhãn quyền (kế hoạch: một bảng trong
  `roles.go`). Helper e2e `writeRecorder`, `json` dời vào `console/e2e/fixtures.ts`
  (jscpd báo trùng giữa `roles.spec.ts` và `staff.spec.ts`); `nonNil` của `staff.go` thành generic thay cho bản riêng
  của quyền.

## Soát thiết kế

Lượt 2: 5.9 operation đọc mới khai `x-permission: identity.roles.manage` (guard mặc định từ chối, không đổi guard);
5.7 đọc trong `WithTenant` của tenant người gọi như `listRoles`; 6.13 mặc định từ chối giữ nguyên — người thiếu cả hai
quyền vẫn 403 ở cả hai đường; không ghi, không khóa, 6.1–6.12 không áp dụng (chỉ đọc).


Mục 5:
- 5.1–5.3: dữ liệu vai trò thuộc `identity` (04 mục 3); chỉ đọc, ghi bảng của schema `identity`.
- 5.4: tạo/sửa/lưu trữ vai trò + audit trong một transaction (`WriteTx`).
- 5.5, 5.6, 5.10: không áp dụng: không sự kiện, lệnh ra ngoài, Valkey. Phiên của người giữ vai trò nhận quyền mới ở
  yêu cầu kế tiếp (LoadUser mỗi yêu cầu).
- 5.7: mọi câu trong `WithTenant`/`WriteTx` của tenant người thao tác; không bảng mới.
- 5.8: module không nhập HTTP; handler mỏng.
- 5.9: ba operation ghi có `IdempotencyKey` và `x-permission: identity.roles.manage`.
- 5.11: không thêm thư viện (khóa ngẫu nhiên bằng `crypto/rand`).
- 5.12: theo mẫu `identity` (sqlc, sentinel, test PostgreSQL thật, rapid) và mẫu đường ghi Task 12.

Mục 6:
- 6.1: sửa/lưu trữ mang `version`; khóa dòng `identity.roles` `FOR UPDATE` rồi so version → `version_conflict`.
  Trùng tên: database không có ràng buộc tên duy nhất (task không có migration) nên tạo/sửa tên lấy khóa tư vấn theo
  tenant (`pg_advisory_xact_lock`) trước mọi khóa dòng rồi mới kiểm tên — xem "Quyết định thay chủ shop".
- 6.2: `Idempotency-Key`, lỗi nghiệp vụ không lưu (final=false) như `staffWrite`; 50 lần cùng khóa → 1 audit.
- 6.3: thứ tự ADR-46 nhóm 2: `UpdateRole`/`ArchiveRole` chỉ khóa `roles` (FOR UPDATE) rồi đổi `role_permissions`,
  đọc `user_role_grants` không khóa; `ReplaceGrants` khóa `users` → `roles` (FOR SHARE, theo id tăng dần) →
  `user_role_grants`; `EnsureDefaultRoles` khóa `roles` FOR SHARE → `role_permissions`. Lưu trữ và gán cùng lúc: bên sau
  chờ khóa dòng vai trò rồi thấy kết quả bên trước (READ COMMITTED, câu sau khi chờ thấy dữ liệu đã commit) → hoặc
  `role_archived` hoặc `role_in_use`; test 50 lượt kiểm `pg_stat_database.deadlocks` không tăng.
- 6.4: lỗi tạm đi qua `db.RetryTx` của `WriteTx` (hàm chạy lại được: biến kết quả gán lại mỗi lần).
- 6.5: READ COMMITTED + khóa dòng như trên.
- 6.6–6.8, 6.10: không áp dụng (không việc nền, bên ngoài, tiền).
- 6.9: giới hạn chung của `db.TxAPI`.
- 6.11: rapid 200 lượt nối bộ của Task 26: xen kẽ gán/gỡ/khóa/tạo/sửa vai trò → luôn còn Chủ hoạt động; không ai có
  quyền mà người gán hay người sửa vai trò không có lúc làm.
- 6.12: audit vai trò ghi tên vai trò (không phải dữ liệu cá nhân), trạng thái, version, danh sách quyền trước/sau.
- 6.13: mặc định từ chối: module tự kiểm `identity.roles.manage` (không chỉ guard); quyền thêm/gỡ phải có ở phạm vi
  all; vai trò Chủ khóa; quyền của vai trò đã lưu trữ không còn hiệu lực (LoadUser bỏ qua).

## Quyết định thay chủ shop

Lượt 2:
12. **Xem danh sách vai trò bằng `identity.roles.manage`** (quyết định của điều phối viên; thay Quyết định 6): thêm
    operation đọc `listRolesToManage` (GET /api/v1/identity/roles/manage, quyền `identity.roles.manage`) cho màn Vai trò;
    `listRoles` giữ `identity.users.manage` vì màn Nhân viên cần danh sách vai trò để gán, mà người chỉ có
    `identity.users.manage` không được thấy màn Vai trò. Không có quyền "xem vai trò" riêng nên không thêm. Phương án
    khác: cho `x-permission` nhận danh sách quyền (đủ một trong số đó) — phải đổi guard, routegen (vùng rủi ro cao)
    cho một route; đổi `listRoles` sang `identity.roles.manage` — làm hỏng màn Nhân viên của người chỉ quản lý nhân
    viên. Đảo ngược: xóa operation, màn quay lại `listRoles`.
13. **Gộp xung đột test menu** (`staff.spec.ts`): giữ cả hai ý — mở "/" (main: khai API sản phẩm, menu không có Nhân
    viên) rồi mở `/nhan-vien` (task: câu "không có trang này") — một test kiểm cả hai.

Lượt 1:

1. **Số tệp task là 56** theo lệnh của phiên (bảng số toàn cục ghi M1-56 → 55). Giữ `task-56.md`, tiêu đề "Task 56
   (M1-56)". Phương án khác: tạo `task-55.md` — trái lệnh giao việc.
2. **Tên vai trò duy nhất** (không phân biệt hoa thường, bỏ khoảng trắng hai đầu, tính cả vai trò đã lưu trữ) kiểm
   trong mã dưới khóa tư vấn theo tenant, không thêm migration (kế hoạch ghi "task này không có migration"). Phương án
   khác: unique index `(tenant_id, lower(name))` ở migration mới — chắc hơn nhưng trái kế hoạch và dễ trùng số
   migration với phiên song song; có thể thêm sau, dễ đảo ngược.
3. **Vai trò đã lưu trữ không cho quyền** (LoadUser bỏ qua), vì lưu trữ chỉ chặn khi còn người *active* giữ; người bị
   khóa còn giữ vai trò đã lưu trữ mà được mở lại thì không lấy lại quyền. Gỡ grant cũ của vai trò đã lưu trữ vẫn được;
   chỉ chặn *thêm* grant mới (`role_archived`).
4. **Gỡ quyền lạ** (khóa không còn trong `identity.Permissions`) khỏi vai trò không cần người sửa có quyền đó (quyền lạ
   không cho ai gì, `Can` từ chối); thêm quyền lạ → `unknown_permission`.
5. **Vai trò đã lưu trữ không sửa được** (`role_archived`), màn chỉ "Xem" như mockup; chưa có "khôi phục" (không có
   trong kế hoạch, ghi ở Việc phát hiện thêm).
6. **`listRoles` giữ quyền `identity.users.manage`** (hợp đồng Task 26, đổi là đổi quyền của route đang chạy). Màn Vai
   trò hiện với `identity.roles.manage`; người có quyền này mà thiếu `identity.users.manage` thấy câu báo thiếu quyền
   xem danh sách (vai trò mẫu chỉ Chủ có cả hai).
7. **"Người đang giữ"** = số nhân viên `active` có ít nhất một grant của vai trò (khớp luật lưu trữ `role_in_use`).
8. **Thêm migration `0128_identity_custom_roles.sql` chỉ cấp quyền** (kế hoạch ghi "task này không có migration"):
   `crm_app` chỉ có `SELECT` trên `identity.roles`, `identity.role_permissions`, nên không ghi được vai trò; `SELECT …
   FOR UPDATE/FOR SHARE` cũng cần quyền `UPDATE` cột. Cấp `INSERT` + `UPDATE (name, customized, status, version)` trên
   `roles`, `INSERT, DELETE` trên `role_permissions`. Pha mở rộng, không bảng, không cột. Số 0128: số đầu tiên sau các số
   kế hoạch đã cấp (0126, 0127; Task 315 ghi "0128 trở đi"); không nhánh nào đang mở dùng 0128. Phương án khác: ghi vai
   trò bằng hàm `SECURITY DEFINER` — phức tạp hơn, không cần.
9. **Lưu trữ cũng đòi người lưu trữ có mọi quyền của vai trò ở toàn shop** (lưu trữ = gỡ mọi quyền khỏi người còn giữ
   bị khóa), cùng luật "gỡ quyền" của kế hoạch. Phương án khác: chỉ cần `identity.roles.manage` — dễ nới (sửa một dòng).
10. **`ArchiveRole` trả `RoleChange`** (trước/sau, để ghi audit và trả vai trò cho màn) thay vì chỉ `error`; `CreateRole`
    trả `RoleRow` (kiểu vai trò đã có của Task 26, thêm `Version`, `HolderCount`) thay vì kiểu `Role` mới — tránh hai
    kiểu cùng nghĩa.
11. **`ReplaceGrants` khóa FOR SHARE mọi vai trò được thêm hay gỡ** (không chỉ vai trò được thêm) rồi đọc lại quyền của
    chúng trước khi kiểm luật "không vượt quyền người gán", để lần sửa vai trò chạy cùng lúc không làm luật kiểm trên
    quyền cũ.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Lượt 2, commit 2: người chỉ có `identity.roles.manage` (không `identity.users.manage`) mở được danh sách vai trò; `listRoles` vẫn đòi `identity.users.manage`; người thiếu quyền 403 | `TestRolesAPI_PermissionsAndEffect` (đỏ trước: `X liệt kê vai trò để quản lý: 404 not_found`) | `go test -race -count=1 -v -run 'TestRolesAPI|TestRoutes' ./internal/httpapi/ ./tests/arch/` | `--- PASS: TestRolesAPI_PermissionsAndEffect (5.08s)`; `TestRoutesPublicSetAndPermissions`, `TestRoutesAllServed` PASS |
| Lượt 2, commit 2: màn Vai trò với người chỉ quản lý vai trò hiện danh sách, mở được vai trò; menu không có Nhân viên; API giả đòi đúng quyền như máy chủ | `roles.spec.ts` "chỉ có quyền quản lý vai trò (không quản lý nhân viên)…" (đỏ trước: không thấy dòng "Kho phụ") + mọi ca cũ của roles, staff | `CRM_E2E_PORT=5156 npx playwright test e2e/roles.spec.ts e2e/staff.spec.ts --project=chromium`; `npx vitest run`, `tsc`, `eslint` | `33 passed (18.6s)`; `Tests 174 passed` |
| Lượt 2, commit 2: cổng kiểm | `make ci` | `make ci CRM_PG_PORT=5600` (sau khi stage); oasdiff và test-guard so với bản main đã gộp `4b752d1` | build, vet, lint `0 issues`, gen-check, sqlc-vet, `go test -race ./...` 47 gói ok (identity 346s, httpapi 312s, tests/e2e 83s), govulncheck `0 vulnerabilities` trong mã; `oasdiff` so với main hiện tại đỏ vì main đã có thêm Task 321 (`getOriginalImageURL`) sau lần gộp (không gộp lại theo lệnh) → `go run ./tools/apicheck -base-ref=4b752d1 …` thoát 0; `test-guard` so với main hiện tại báo hai tệp test của Task 321 "bị xóa" (cùng lý do) → `git diff --diff-filter=D 4b752d1 HEAD -- '*_test.go' '*.spec.ts'` rỗng; catalog-check, jscpd `Found 0 clones`, gitleaks `no leaks found`, console-check vitest `174 passed` |
| Lượt 2, commit 1: gộp main lần 3 (Task 22, 51, 324) không hỏng gì; test menu giữ ca của cả hai bên | `make ci`; `staff.spec.ts` "menu: mục Nhân viên…" | `make ci CRM_PG_PORT=5600`; sau khi dời `noHorizontalScroll` vào `fixtures.ts`: `make jscpd gen-check test-guard gitleaks console-check`; `CRM_E2E_PORT=5156 npx playwright test --project=chromium` | `go test -race ./...` 47 gói ok, 0 FAIL (identity 535s, httpapi 522s, tests/e2e 117s); lint `0 issues`; jscpd lần đầu báo 1 clone mới (`noHorizontalScroll` ở `inventory.spec.ts` của main và `roles.spec.ts`) → dời vào `fixtures.ts` → `Found 0 clones`; gen-check, test-guard qua; gitleaks `no leaks found`; vitest `174 passed`; Playwright chromium `66 passed`, sau khi dời `40 passed` (roles, inventory, staff) |
| Tạo vai trò có quyền người tạo thiếu → từ chối; có đủ → tạo, khóa `custom_` + 8 ký tự | `TestCustomRole_CannotExceedActor`; `TestRolesAPI_CreateUpdateArchive` | `go test -race -v ./internal/modules/identity/`; `go test -race ./internal/httpapi/ -run 'RolesAPI|StaffAPI|ErrorCodes|Route'` | `--- PASS: TestCustomRole_CannotExceedActor (3.49s)`; `ok elimaz.com.vn/crm/internal/httpapi 81.230s` (403 `grant_exceeds_actor`, 201) |
| Không tự nâng quyền qua vai trò mình giữ; gỡ quyền mình không có, lưu trữ vai trò có quyền mình không có đều bị chặn; quyền theo kho chỉ ở một kho không đủ | `TestCustomRole_CannotExceedActor` | `go test -race -v ./internal/modules/identity/` | PASS |
| Vai trò Chủ không sửa, không lưu trữ (409 `owner_role_locked`) | `TestCustomRole_OwnerLocked`; `TestRolesAPI_CreateUpdateArchive` | `go test -race -v ./internal/modules/identity/`; `go test -race ./internal/httpapi/ -run 'RolesAPI|StaffAPI|ErrorCodes|Route'` | PASS |
| Sửa vai trò mẫu → `customized`; `EnsureDefaultRoles` không thêm lại; owner đủ quyền; người giữ mất quyền ngay | `TestCustomRole_DefaultRoleCustomized` | `go test -race -v ./internal/modules/identity/` | `--- PASS (3.33s)` |
| Lưu trữ khi còn người active giữ → `role_in_use`; quyền lạ 422; trùng tên 409; version cũ 409; tên sai 400; sửa vai trò đã lưu trữ, gán mới vai trò đã lưu trữ → `role_archived` | `TestCustomRole_ArchiveAndInput`; `TestRolesAPI_CreateUpdateArchive`; `TestStaffErrorCodes` | `go test -race -v ./internal/modules/identity/`; `go test -race ./internal/httpapi/ -run 'RolesAPI|StaffAPI|ErrorCodes|Route'` | PASS |
| Vai trò đã lưu trữ không cho quyền | `TestCustomRole_ArchivedGivesNothing` | `go test -race -v ./internal/modules/identity/` | `--- PASS (2.65s)` |
| Song song 50 lượt: lưu trữ và gán cùng lúc → đúng một trong hai kết cục, `deadlocks` không tăng | `TestCustomRole_ArchiveVsAssignConcurrent` | `go test -race -v ./internal/modules/identity/` | `--- PASS (4.09s)` |
| Song song 50 lượt: hai lần tạo cùng tên → đúng một lần qua | `TestCustomRole_NameTakenConcurrent` | `go test -race -v ./internal/modules/identity/` | `--- PASS (4.40s)` |
| 50 lần cùng `Idempotency-Key` → 1 vai trò, 1 audit | `TestRolesAPI_CreateIdempotent` | `go test -race ./internal/httpapi/ -run 'RolesAPI|StaffAPI|ErrorCodes|Route'` | PASS |
| Quy luật (rapid 200 lượt): luôn còn Chủ; không ai có quyền người gán/người sửa vai trò không có; owner đủ quyền | `TestRoleInvariantsProperty`, `TestStaffInvariantsProperty` | `go test -race -v ./internal/modules/identity/` | `[rapid] OK, passed 200 tests (1m14.7s)`; `[rapid] OK, passed 200 tests (46.99s)` |
| Mọi quyền có nhãn, nhóm, loại phạm vi; `listPermissions` 403 với K, 200 với Chủ | `TestPermissionCatalog_Complete`; `TestRolesAPI_PermissionsAndEffect` | `go test -race -v ./internal/modules/identity/`; `go test -race ./internal/httpapi/ -run 'RolesAPI|StaffAPI|ErrorCodes|Route'` | PASS |
| Gán vai trò tự tạo → người đó gọi được đúng operation của quyền trong vai trò, không hơn | `TestRolesAPI_PermissionsAndEffect` | `go test -race ./internal/httpapi/ -run 'RolesAPI|StaffAPI|ErrorCodes|Route'` | PASS (permissions 200, users 403) |
| `crm_app` chỉ có đúng quyền ghi mới; RLS chặn tenant khác | `TestAppRolePrivilegesAndRLS` | `go test -race -count=1 -run TestAppRolePrivilegesAndRLS ./internal/modules/identity/` | `ok … 2.123s` |
| Màn (1512, 390): danh sách, Chủ chỉ xem, tạo với quyền mờ kèm lý do, sửa có version, câu báo trùng tên/xung đột, lưu trữ bị chặn kèm tên người giữ, hỏi lại rồi gửi; vai trò tự tạo hiện ở màn Nhân viên | `console/e2e/roles.spec.ts` (15 test) | `CRM_E2E_PORT=5156 npx playwright test e2e/roles.spec.ts --project=chromium` | `15 passed (8.1s)` |
| Màn Nhân viên, Sản phẩm, khung không hỏng (sau khi gộp main) | mọi spec `console/e2e` | `CRM_E2E_PORT=5156 npx playwright test --project=chromium`; `--project=webkit` | `48 passed (16.8s)`; `1 passed (5.6s)` |
| Luật hiển thị của màn | `roleRules.test.ts`, mọi vitest | `npx vitest run` | `Tests 135 passed (135)` (sau khi gộp main) |

Cổng kiểm `make ci` (CRM_PG_PORT=5600, bộ chung): lần 1 (trước gộp main) mọi bước qua trừ hai gói không liên quan hết
giờ/ngưỡng thời gian dưới tải (`inventory` 10 phút, `imager` TestImager_HangKilledWithinDeadline; chạy riêng imager qua).
Lần 2 (sau gộp main) bị ngắt vì PostgreSQL chung sập vào chế độ phục hồi (`SQLSTATE 57P03 the database system is in
recovery mode`, hàng trăm test mọi gói) — sự cố hạ tầng của bộ chung, phiên này không chạy lệnh docker nào. Sau khi DB
lên lại và gộp main lần 2, chạy lại từng phần: build, golangci-lint (0 issues), gen-check, oasdiff, catalog-check, jscpd
(0 clones), test-guard, gitleaks, console-check (vitest 135 qua, vite build), govulncheck (không lỗ hổng); `go test -race`
identity (ok 383s), httpapi (ok 252s), tests/arch (ok), cfaccess, realtime, cmd/crm, tests/e2e (ok 194s; một lần
TestBinaryWorkerHandlesSelftest hết hạn chờ ingest, chạy lại qua), tests/deploy, internal/queries, catalog, files.

## Test bị xóa hoặc tắt

- Không xóa, không tắt. **Sửa** `TestAppRolePrivilegesAndRLS` (`users_test.go`): bốn câu "crm_app không ghi được vai
  trò, quyền của vai trò" của M0/M1-26 trái với yêu cầu của task này (API vai trò chạy bằng `crm_app`), nên chuyển sang
  danh sách "được phép" (đúng các quyền của migration 0128) và thay bằng các câu vẫn phải bị chặn chặt hơn: đổi `key`,
  đổi `tenant_id` của vai trò, xóa vai trò, sửa dòng `role_permissions`; thêm ca RLS chặn chèn vai trò cho tenant khác.

## Khác kế hoạch

- Có migration `0128` (chỉ GRANT; Quyết định 8). Tệp task là `task-56.md` (Quyết định 1).
- `ArchiveRole` trả `(RoleChange, error)`, `CreateRole` trả `RoleRow` (Quyết định 10).
- Thêm sentinel `ErrRoleNotFound` (404 `not_found`) và `ErrRoleArchived` cho sửa vai trò đã lưu trữ.
- Màn: thêm `console/src/features/roles/{roleRules.ts,roleRules.test.ts,roleData.ts,roles.css}` ngoài hai tệp kế hoạch
  ghi; `staffData.tsx` (`RefusalBox` nhận câu xung đột riêng, câu phụ cho mã lỗi vai trò), `staffRules.ts` (xuất
  `isCustom`). `StaffGrants.tsx` không cần đổi (đã lọc vai trò `active`, đánh "(tự tạo)").

## Việc phát hiện thêm

- **Trùng số tệp task:** theo `danh-so-task-toan-cuc.md`, task đóng mốc M1-55 có số toàn cục 56; tệp `task-56.md` nay
  là của M1-56. Khi giao task đóng mốc, cần chọn số tệp khác (hoặc chủ shop quyết cách đặt) để `new-task.sh` không ghi
  đè.
- Sau khi gộp main (Task 50 thêm màn Sản phẩm làm màn đầu tiên), test `staff.spec.ts` "menu: mục Nhân viên chỉ hiện…"
  mở "/" nên bị chuyển tới `/san-pham` và gọi API sản phẩm chưa khai (đỏ chập chờn, 4/6 lần). Đã sửa test mở
  `/nhan-vien` (trang "không tìm thấy") và kiểm thêm câu đó; điều kiện về menu giữ nguyên. Chạy lặp 6 lần: 6 qua.
- Lưu trữ đếm người giữ mà không khóa `users`: nếu đúng lúc đó có người mở khóa một nhân viên đang giữ vai trò, vai trò
  đã lưu trữ có thể còn `holderCount` > 0. Không mất an toàn (vai trò đã lưu trữ không cho quyền), chỉ là số hiển thị.

- Chưa có "khôi phục" vai trò đã lưu trữ (kế hoạch không có). Cần thì thêm operation `restoreRole`.
- ~~`listRoles` vẫn đòi `identity.users.manage`…~~ Đã xử lý ở lượt 2 (Quyết định 12).

## Reviewer

Kết luận lượt 2, commit 2/2 (`listRolesToManage`, màn Vai trò chỉ cần `identity.roles.manage`): **ĐẠT**. Không có
phát hiện về tính đúng; đã đối chiếu router ogen (nhánh `manage` chỉ khớp đúng đường, `/roles/{id}` và `/archive` giữ
nguyên; `PUT /roles/manage` nay 405), guard mặc định từ chối (K 403), khóa truy vấn con của `ROLES_KEY` nên làm mới sau
khi ghi vẫn đúng, regex e2e không bắt nhầm GET `/roles/manage`, oasdiff/test-guard so với bản main đã gộp hợp lệ.

Kết luận lượt 2, commit 1/2 (gộp main lần 3: Task 22, 51, 324): **ĐẠT**. Không có phát hiện về tính đúng; đã đối chiếu
test menu (chỉ thêm, không nới điều kiện nào), `openapi.yaml` đã gộp đủ operation của hai bên, mã sinh khớp (go vet
qua), `noHorizontalScroll` dời nguyên thân, `screens.tsx` giữ đủ màn. Ghi chú không bắt buộc (không sửa): ba spec cũ
(`products`, `staff`, `product-edit`) còn bản cục bộ của `noHorizontalScroll` (jscpd không bắt).

Lượt 1:

Kết luận commit 1/2 (đổi cấu trúc: `identityWrite` generic, `freshGrants`, `nonNil` generic): **ĐẠT**. Không có phát
hiện về tính đúng; đã đối chiếu thứ tự lỗi nghiệp vụ → lỗi khác → audit, `final=false`, before rỗng khi tạo, fn chạy lại
được. Ghi chú: `freshGrants` thêm `Email` (vô hại, không ai đọc); commit 2 phải thêm sentinel vai trò vào
`isStaffRefusal` (đã có).

Kết luận commit 2/2 (tính năng, sau khi gộp main lần 1 — Task 320/50): **ĐẠT**. Không có phát hiện về tính đúng; đã
đối chiếu tự nâng quyền, thứ tự khóa, lưu trữ và gán cùng lúc ở READ COMMITTED, LoadUser bỏ vai trò lưu trữ, migration
0128, mã lỗi, chống trùng, màn hình, phần gộp main, sửa test menu (chặt hơn, không nới). Ghi chú không bắt buộc (không
sửa): chú thích `ShareLockRoles` ghi "vai trò được thêm" trong khi mã khóa mọi vai trò thêm hay gỡ; test RLS so chuỗi
lỗi thay vì mã `42501`. Gộp main lần 2 (Task 395: compose, tools/devaccess, tests/deploy) không chạm tệp nào của task.

Kết luận lượt 1 trên toàn bộ task (trước khi tách commit): **ĐẠT**, không phát hiện tính đúng. Ghi chú đã xử lý: mục
Ảnh hưởng sửa (bỏ kiểu `identity.Role`, ghi migration 0128); tách commit cấu trúc riêng; ghi trùng số tệp task và
`holderCount` của vai trò đã lưu trữ vào "Việc phát hiện thêm".
