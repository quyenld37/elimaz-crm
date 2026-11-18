# Task 26 — Identity M1 — quyền mới, vai trò mẫu, quản lý nhân viên (PQ07)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 26 = M1-26). Loại việc (quy trình mục 3): A. Vùng rủi ro cao: `internal/modules/identity` (chạy cả bộ của gói).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences, kiểm bằng `grep -rn` trên `*.go`):
  - `identity.Permissions` (thêm 16 quyền, không bớt): dùng ở `tests/arch/routes_test.go:30` (qua `IsKnown`), `tests/e2e/seed_test.go:48`, `internal/modules/identity/{users_test.go:172,175, permissions_test.go:73–94}`. Không đổi kiểu nên mọi chỗ dùng giữ nguyên; `TestPermissionsList` cập nhật danh sách mong đợi theo kế hoạch M1 (chặt hơn, không nới).
  - `identity.SeedOwner` (giữ chữ ký): `cmd/crm/seed.go:35`, `users_test.go:160`. Bên trong đổi sang gọi `EnsureDefaultRoles` (vai trò `owner` lấy từ `DefaultRoles`).
  - Thêm mới (không ai dùng trước): `RoleDef`, `DefaultRoles`, `EnsureDefaultRoles`, `StaffRow`, `StaffGrant`, `GrantInput`, `RoleRow`, `ListStaff`, `GetStaff`, `ListRoles`, `CreateStaff`, `ReplaceGrants`, `SetLocked`, sentinel `ErrSelfModify`, `ErrGrantExceedsActor`, `ErrLastOwner`, `ErrEmailTaken`, `ErrVersionConflict`, `ErrStaffNotFound`, `ErrInvalidInput`.
- Gói phụ thuộc ngược (`go list -f '{{.ImportPath}} {{.Imports}}' ./...`): `identity` được nhập bởi `cmd/crm`, `internal/adapters/cfaccess`, `internal/httpapi`, cộng `tests/arch`, `tests/e2e` (test). Sẽ chạy test của cả năm.
- Operation OpenAPI bị chạm, oasdiff: chỉ **thêm** 6 operation (`listStaff`, `createStaff`, `replaceStaffGrants`, `lockStaff`, `unlockStaff`, `listRoles`) và schema mới (`StaffMember`, `StaffGrant`, `CreateStaffRequest`, `ReplaceGrantsRequest`, `Role`), tham số `StaffID`. Không sửa operation hay schema cũ → oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: chưa có `console/` (M0 Task 17 chưa làm); màn Nhân viên là Task M1-54/M1-56.
- Câu sqlc, bảng, pha migration: migration mới `0113_identity_m1.sql`, pha **mở rộng**: thêm cột `users.version`, `roles.customized`, `roles.status`, `roles.version` (NOT NULL có DEFAULT, không cần dòng contract); hàm `platform.add_staff_route(citext)` `SECURITY DEFINER`; view `identity.users_v` (`security_invoker`); cấp thêm `INSERT`, `UPDATE (display_name, status, version)` trên `identity.users`, `INSERT`, `DELETE` trên `identity.user_role_grants` cho `crm_app`. `crm_app` vẫn không có `INSERT` trên `platform.tenant_routes`. Câu sqlc mới trong `queries/identity/staff.sql`; `queries/identity/users.sql` bỏ `UpsertRole` (thay bằng `InsertRoleIfMissing`, `GetRoleByKey`). Bảng chạm: `identity.users`, `roles`, `role_permissions`, `user_role_grants`, `audit_logs`, `platform.tenant_routes` (qua hàm). Thứ tự khóa: nhóm 1 (`platform.tenant_routes`, chỉ chèn dòng mới) và nhóm 2 đã có trong `docs/adr/0046-thu-tu-khoa.md`; không có bảng mới nên tệp đó không đổi (Task 25 giữ tệp này).
- Test sẽ chạy: `go test -race ./internal/modules/identity/...` (cả bộ, vùng cao), `./internal/httpapi/...`, `./cmd/crm/...`, `./internal/adapters/cfaccess/...`, `./tests/arch/...`, `./tests/e2e/...` (seed-dev); `make gen` rồi `git diff` sạch; golangci-lint, `sqlc vet`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` các mục `db` (`RetryTx`), `tenancy` (`WithTenant`, `AddRoute`, `Resolve`), `idempotency`, `logging`; tìm `23505`, `UniqueViolation`, `FOR UPDATE`, `version` trong `internal/` → chỉ có `events/emit.go` so mã `23505` tại chỗ (không có hàm chung); không có gói nào quản lý nhân viên hay vai trò.
- Dùng lại / viết mới vì:
  - Dùng lại: `identity.Can` cho luật "không gán quá quyền của mình" (không viết bộ kiểm quyền thứ hai); `identity.RecordAudit`; `httpapi.WriteTx` + middleware chống trùng; mẫu `changeJob` của `handlers_jobs.go` (từ chối nghiệp vụ không lưu vào khóa chống trùng); `testkit.OpenStaff`, `AccessKeyServer`, `authEnv`, `expect` của test httpapi; câu `ListUserGrants` có sẵn để nạp lại quyền của người thao tác trong transaction.
  - `tenancy.AddRoute` không dùng được cho `createStaff`: nó cần chủ bảng (`crm_app` không có `INSERT` trên `tenant_routes`); kế hoạch Q25 đòi hàm `SECURITY DEFINER` chỉ ghi cho tenant trong ngữ cảnh.
  - So mã lỗi `23505` viết tại chỗ trong `staff.go` (như `events/emit.go`): hai chỗ, khác ràng buộc, chưa đủ để thành hàm chung.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Chủ dữ liệu: người dùng, vai trò, grant thuộc `identity` (04 mục 3); mọi cột mới trong schema `identity`. Tuyến email ở `platform.tenant_routes` thuộc nền tảng, ghi qua hàm `platform.add_staff_route` (Q25).
- 5.2 Không ghi bảng module khác: chỉ ghi bảng `identity`; tuyến email qua hàm do nền tảng công bố.
- 5.3 Đọc qua view: `identity.users_v` công bố cho M6, M7; identity không đọc module nào khác.
- 5.4 Nguyên tử: tạo nhân viên + tuyến email + audit trong một transaction; thay grant + audit + khóa chống trùng trong một transaction (`WriteTx`).
- 5.5 Sau commit: không có việc nền nào.
- 5.6 Gọi ra ngoài: không có.
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; không bảng mới (chỉ thêm cột), view `security_invoker` nên RLS của `users` áp cho người đọc.
- 5.8 Nghiệp vụ không biết HTTP: luật ở `identity/staff.go`; handler `handlers_identity.go` chỉ đổi kiểu.
- 5.9 Đường ghi: bốn operation ghi đều có `IdempotencyKey` và `x-permission: identity.users.manage`.
- 5.10 Valkey: không dùng.
- 5.11 Thư viện: không thêm (pgx, ogen, rapid đã có).
- 5.12 Module mẫu: theo `identity` (Task 7) và đường ghi `handlers_jobs.go` (Task 12).

Mục 6 (đồng thời):

- 6.1 Hai người cùng sửa: `users.version` cho `replaceStaffGrants` (`version` cũ → `ErrVersionConflict`). Hai Chủ khóa hoặc gỡ quyền của nhau: mỗi thao tác khóa trước `SELECT … FROM identity.users WHERE id = ANY(<người bị sửa, người thao tác>) OR <Chủ còn hoạt động> ORDER BY id FOR UPDATE`, rồi mới đọc lại grant và đếm số Chủ còn hoạt động bằng câu mới (READ COMMITTED: câu mới thấy dữ liệu bên kia vừa commit). Test 50 lượt song song.
- 6.2 Gửi lại: `Idempotency-Key`; kết quả lưu cùng transaction (`WriteTx`); từ chối nghiệp vụ không lưu (gửi lại thì xét lại). Email trùng: unique `(tenant_id, email)` và khóa chính `tenant_routes`.
- 6.3 Thứ tự khóa: nhóm 2 `users` (theo `id` tăng dần, một câu) → `roles` (chỉ đọc; `EnsureDefaultRoles` chèn/khóa `roles` rồi `role_permissions`) → `user_role_grants` (xóa/chèn); `tenant_routes` chỉ chèn dòng mới; `audit_logs` chỉ chèn; `idempotency_keys` cuối.
- 6.4 Lỗi tạm: qua `db.RetryTx` (trong `WithTenant`, `WriteTx`); hàm trong transaction gán biến kết quả lại từ đầu mỗi lần chạy.
- 6.5 Mức cách ly: READ COMMITTED + khóa dòng như 6.1.
- 6.6–6.8: không áp dụng: không có việc nền, không gọi bên ngoài.
- 6.9 Treo lâu: `db.TxAPI` (lock_timeout 5 s) qua `WriteTx`.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: rapid 200 lượt trên chuỗi gán/gỡ/khóa/mở ngẫu nhiên: luôn còn ≥ 1 Chủ hoạt động; không ai nhận quyền mà người gán không có lúc gán.
- 6.12 Dữ liệu cá nhân: audit không ghi email (chỉ mã nhân viên, trạng thái, khóa vai trò, phạm vi); lỗi không mang email; `users_v` không có `email`.
- 6.13 Quyền: mặc định từ chối (guard có sẵn); luật thêm ở module: không tự sửa mình, không gán/gỡ/khóa vượt quyền của mình (kiểm cả theo grant lúc yêu cầu đến và grant đọc lại trong transaction), không làm số Chủ hoạt động về 0.

## Quyết định thay chủ shop

- **D1 — `/api/v1/me` của nhân viên mới chưa có grant.** Kế hoạch Step 1 ghi "200 với `grants: []`", nhưng hợp đồng M0 khai `getMe` cần `realtime.subscribe` và guard (vùng cao) từ chối route không quyền; test M0 `TestAuthUsersAndGrants` đang đòi 403. Chọn: **giữ hành vi M0** (403 ở mọi route, kể cả `/me`) — đúng PQ03 "không thấy gì cả" mà kế hoạch ghi "giữ nguyên". Phương án khác: thêm chế độ "chỉ cần đăng nhập" cho `getMe` (đổi guard và hợp đồng M0, cần task riêng). Dễ đảo ngược.
- **D2 — Thứ tự kiểm luật.** Chọn: tự sửa mình → không có người đó → `version` → vượt quyền (theo grant lúc yêu cầu đến, như guard) → thực hiện → "Chủ cuối cùng" → vượt quyền lần hai theo grant **đọc lại trong transaction** (người thao tác vừa bị khóa → `ErrLocked`, 403). Vì: cho đúng kết quả mà kế hoạch đòi (M gỡ `owner` của Chủ duy nhất → 403 `grant_exceeds_actor`; hai Chủ tranh nhau → bên thua 409 `last_owner`) mà vẫn không cho người vừa bị hạ quyền gán tiếp. Phương án khác: chỉ kiểm theo grant lúc yêu cầu đến (hở một khoảng nhỏ).
- **D3 — Test song song 50 lượt gọi thẳng `SetLocked`/`ReplaceGrants`** với `AuthUser` nạp trước cho cả hai Chủ, không qua HTTP. Vì: qua HTTP, bên chậm có thể bị chặn ngay ở bước xác thực (403 vì đã bị khóa) nên "bên kia 409" không xác định. Lớp HTTP có test riêng cho mã 409 `last_owner`.
- **D4 — Lỗi đầu vào.** Kế hoạch không có mã cho vai trò lạ, phạm vi sai, email rỗng: thêm `ErrInvalidInput` → 400 `invalid_input`; nhân viên không có (hoặc của tenant khác) → 404 `not_found`.
- **D5 — Kiểm `warehouse:<id>` có tồn tại không:** chưa làm (bảng kho là Task M1-28); chỉ kiểm mã đích khác rỗng, tối đa 128 ký tự.

## Bằng chứng

Đỏ trước: sau khi viết test, `go vet ./internal/modules/identity/ ./internal/httpapi/` báo `undefined: identity.GrantInput`, `identity.EnsureDefaultRoles`… (chưa có mã); lần chạy đầu sau khi viết mã: `TestPermissionsDoc_MatchesCode` đỏ (chưa có `docs/quyen.md`), `TestAppRoleReadOnlyAndRLS` đỏ (luật M0 "crm_app chỉ đọc", xem mục dưới).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| M gán `owner` cho K → `grant_exceeds_actor`; M gán `warehouse`/`sale` theo kho → được; M gỡ `owner` của A, M khóa A → `grant_exceeds_actor`; K không có `identity.users.manage` → từ chối | `TestReplaceGrants_ExceedsActor`, `TestStaffAPI_ReplaceGrants`, `TestStaffAPI_LockAndCreate` | `go test -race -count=1 -v ./internal/modules/identity/ ./internal/httpapi/ -run '…'` | `--- PASS` cả ba; HTTP 403 `grant_exceeds_actor` |
| K gọi bất kỳ operation nào của nhân viên → 403 | `TestStaffAPI_Permissions` | như trên | `--- PASS` (6 operation, 403 `forbidden`) |
| A sửa grant, khóa, mở chính A → `cannot_modify_self` | `TestReplaceGrants_SelfModify`, `TestStaffAPI_LockAndCreate` | như trên | `--- PASS` |
| Chủ cuối cùng: actor là Chủ đã bị khóa khóa/gỡ Chủ còn lại → `ErrLastOwner`; người thao tác vừa bị khóa → `ErrLocked` | `TestLock_LastOwner` | như trên | `--- PASS` |
| Song song 50 lượt × 2 loại (khóa nhau, gỡ `owner` của nhau): đúng một bên thành công, bên kia `ErrLastOwner`, luôn còn 1 Chủ | `TestLock_LastOwnerConcurrent` | như trên (`-race`) | `--- PASS (4.14s)` |
| Mã HTTP: `last_owner` 409, `email_taken` 409, `version_conflict` 409, `cannot_modify_self` 403, `grant_exceeds_actor` 403, `invalid_input` 400, `not_found` 404 | `TestStaffErrorCodes` | như trên | `--- PASS` |
| `version` cũ → 409 `version_conflict`; vai trò/phạm vi sai → 400; nhân viên không có → 404 | `TestReplaceGrants_VersionAndInput`, `TestStaffAPI_ReplaceGrants` | như trên | `--- PASS` |
| 50 lần cùng `Idempotency-Key` → một dòng audit, mỗi câu 200 hoặc 409 `request_in_progress` | `TestStaffAPI_ReplaceGrantsIdempotent` | như trên | `--- PASS` |
| `createStaff` 201, email đã có tuyến ở tenant khác → 409 `email_taken`; nhân viên mới chưa grant: mọi route 403 kể cả `/me` (quyết định D1) | `TestCreateStaff`, `TestStaffAPI_LockAndCreate` | như trên | `--- PASS` |
| `platform.add_staff_route` chỉ ghi cho tenant trong ngữ cảnh, ngoài ngữ cảnh lỗi; `crm_app` `INSERT tenant_routes` → 42501 | `TestAddStaffRoute` | như trên | `--- PASS` |
| `crm_app` chỉ ghi đúng phần cần (không đổi email, không xóa user, không sửa vai trò, quyền) | `TestAppRolePrivilegesAndRLS` | như trên | `--- PASS` (9 câu bị từ chối) |
| `identity.users_v`: cột `id, display_name, status`, `security_invoker=true`, tenant B không thấy dòng của A; khớp `docs/views.md` | `TestUsersView`, `tests/arch` `TestViewsDocMatchesDatabase` | như trên; `go test -count=1 -v ./tests/arch/ -run 'View|Routes'` | `--- PASS` cả hai |
| `EnsureDefaultRoles` chạy 2 lần không đổi số dòng; vai trò `customized` không được thêm quyền, `owner` luôn đủ | `TestEnsureDefaultRoles_Idempotent`, `TestEnsureDefaultRoles_CustomizedKept`, `TestSeedOwner`, `tests/e2e` `TestSeedDev` | như trên; `make ci` | `--- PASS` |
| Năm vai trò mẫu đúng kế hoạch; 22 quyền đúng loại phạm vi; `docs/quyen.md` khớp mã (khóa, loại phạm vi, vai trò mẫu) | `TestDefaultRoles_Content`, `TestPermissionsList`, `TestPermissionsDoc_MatchesCode` | như trên | `--- PASS` |
| Quy luật rapid 200 lượt: luôn ≥ 1 Chủ hoạt động; không ai nhận/mất grant mà người gán thiếu quyền lúc gán; không ai tự sửa mình | `TestStaffInvariantsProperty` | như trên | `--- PASS (23.14s)` |
| Route mới khai quyền có trong `identity.Permissions`, đủ khóa chống trùng | `tests/arch` `TestRoutesPublicSetAndPermissions`, `TestRoutesIdempotencyModes`, `TestRoutesAllServed` | như trên | `--- PASS` |
| Toàn bộ cổng kiểm | build, vet, golangci-lint, gen-check, sqlc vet, `go test -race ./...`, govulncheck, oasdiff, jscpd, test-guard, catalog, gitleaks | `make ci CRM_PG_PORT=5526 COMPOSE_PROJECT=crm-t26` (sau khi stage, trên `main` `f6aa637`) | `exit=0`; golangci-lint `0 issues`; jscpd `Found 0 clones`; gitleaks `no leaks found` |

## Test bị xóa hoặc tắt

- `internal/modules/identity/users_test.go`: `TestAppRoleReadOnlyAndRLS` (M0: "crm_app sửa users phải bị từ chối") **thay** bằng `TestAppRolePrivilegesAndRLS`. Lý do: kế hoạch M1-26 cố ý cho `crm_app` ghi `identity.users` (tạo, khóa nhân viên). Test mới chặt hơn: giữ ca RLS ngoài ngữ cảnh, và kiểm 9 câu ghi ngoài phần được cấp đều bị từ chối (đổi email, đổi tenant, xóa user, mọi ghi vào `roles`, `role_permissions`, sửa `user_role_grants`), cộng một ca được phép (đổi `status`, `version`).
- `internal/modules/identity/permissions_test.go`: `TestPermissionsList` chỉ thêm 16 quyền M1 vào danh sách mong đợi (không bớt ca nào).
- `internal/httpapi/handlers_jobs_test.go`: `jobsEnv.do` và đoạn dựng máy chủ chuyển sang hàm chung `serveAs`, `testServer` (`serve_test.go`, để hết đoạn trùng jscpd); không đổi ca nào.

## Khác kế hoạch

- `ReplaceGrants`, `SetLocked` trả `(Change, error)` thay vì `error` (`Change{Before, After StaffRow}`), như `queue.RetryJob` trả trước/sau: audit cần trạng thái đọc **sau khi khóa dòng**, chỉ module có. Chưa ai ngoài `httpapi` gọi hai hàm này.
- Thêm ngoài danh sách Interface: `GetStaff`, `ListRoles`, `RoleRow`, `RoleKeyOwner`, `Change`, `User.CreatedAt`, `ErrStaffNotFound` (404), `ErrInvalidInput` (400 `invalid_input`) — quyết định D4.
- `/api/v1/me` của nhân viên mới: 403, không 200 `grants: []` (quyết định D1).
- Test song song chạy ở module, không qua HTTP (quyết định D3).
- `SeedOwner` (dùng bởi `crm seed-dev`) gọi `EnsureDefaultRoles`; `cmd/crm/seed.go` chỉ đổi chú thích. Câu `UpsertRole` bị bỏ (thay bằng `InsertRoleIfMissing` + `GetRoleByKey`, không đổi tên vai trò đã có). Database dev cũ giữ tên vai trò owner "Chủ shop"; database mới là "Chủ".
- Giữa task, `main` đã gộp Task 15 và Task 25 (có `0112`, `docs/views.md`, `viewsdoc_test`): nhánh chưa có commit nên đưa lên `main` bằng tiến nhanh (`git merge --ff-only main`), phần đã stage giữ nguyên, rồi chạy lại `make gen` và `make ci`.

## Việc phát hiện thêm

- **Quyền `ScopeNone` trong vai trò gán theo kho (cần quyết trước Task M1-29, M1-54):** theo `Can` của M0, quyền `ScopeNone` chỉ có hiệu lực khi grant phạm vi `all`. Vai trò mẫu Kho, Bán hàng có `catalog.read`, `realtime.subscribe`, `order.create`, `inventory.suppliers.manage` (đều `ScopeNone`); gán Kho phạm vi `warehouse:w1` thì K **không** có các quyền này (không mở được cả `/me`). Muốn dùng được phải gán thêm một grant phạm vi `all`, hoặc đổi luật `Can` (vùng cao, task riêng). Không tự sửa.
- Tenant mới tạo ngoài `seed-dev` chưa có vai trò mẫu; và quyền mới của mốc sau chỉ vào tenant đã có khi `EnsureDefaultRoles` được chạy lại. Cần một chỗ gọi (lúc tạo tenant, hoặc lệnh `migrate` sau migration) — việc của task tạo tenant hoặc M1-56.
- Kiểm `warehouse:<id>` có thật khi có bảng kho (Task M1-28) — quyết định D5.

## Reviewer

Kết luận: ĐẠT (agent `reviewer` trên `git diff --cached`, 18/11/2026). Không có phát hiện ảnh hưởng tính đúng.

- Đã kiểm: thứ tự luật trong `staff.go` đúng D2; `LockStaffRows` tuần tự hóa hai Chủ thao tác lên nhau (luôn khóa người bị sửa và người thao tác, kể cả ca gỡ grant), câu sau khóa thấy dữ liệu vừa commit; thứ tự khóa khớp ADR-46 nhóm 2; hàm `add_staff_route` (`SECURITY DEFINER`, `search_path = ''`, `REVOKE PUBLIC`) và quyền mới của `crm_app` đủ hẹp; từ chối nghiệp vụ không lưu vào khóa chống trùng, tx lùi; audit không có email, tên; mã lỗi đúng kế hoạch; các test kiểm đúng điều tên test nói.
- Ghi chú không bắt buộc (không sửa trong task này): (1) test rapid dùng chính `identity.SymmetricDiff` để tính grant bị chạm — nếu hàm sai thì cả mã và test cùng sai (reviewer đọc hàm thấy đúng); (2) vế "khóa mọi Chủ còn hoạt động" của `LockStaffRows` chỉ có tác dụng khi hai người không phải Chủ cùng có quyền khóa Chủ — chưa có vai trò như vậy, nên thêm test ở Task M1-56; (3) gom code dựng máy chủ thử (`serve_test.go`) đi chung commit với hành vi mới — chỉ là thay đổi cấu trúc của test, giữ chung để jscpd qua; (4) báo chủ shop riêng D1 và mục "Quyền `ScopeNone` trong vai trò gán theo kho".
