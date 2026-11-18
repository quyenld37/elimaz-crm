# Task 315 — Quyền theo phạm vi không khóa quyền không có phạm vi; vai trò mẫu tự có (M1-26b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (mục Task 315, M1-26b). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `identity.Can` (giữ chữ ký, đổi nghĩa với quyền `ScopeNone`): 13 chỗ dùng ở 9 tệp — `httpapi/guard.go`,
    `httpapi/auth_test.go`, `identity/{roles.go,staff.go (3),permissions_test.go (3),staff_property_test.go,users_test.go}`,
    `tests/e2e/seed_test.go`. `hasAllPermissions`/`canTouch` (luật gán, khóa) dùng `Can` nên theo nghĩa mới; không chỗ
    nào cần sửa mã, chỉ sửa test/chú thích mô tả nghĩa cũ.
  - `httpapi.Route` (mã sinh): thêm trường `Authenticated bool` (x-authenticated). Chỗ dùng: `guard.go`, `server.go`
    (chain), `tests/arch/routes_test.go`, `tools/routegen` (nguồn sinh).
  - Mới: `identity.SyncDefaultRoles(ctx, owner *db.DB) (int, error)` — gọi từ `cmd/crm` (`runMigrate`).
  - `EnsureDefaultRoles`: giữ chữ ký; câu `GetRoleByKey` thêm `FOR SHARE` (2 chỗ dùng: `roles.go`, `users.go`).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/adapters/cfaccess`, `internal/httpapi`, `tests/arch`, `tests/e2e`
  (và `tools/routegen` tự đứng).
- Operation OpenAPI bị chạm, oasdiff: `getMe` — bỏ `x-permission: realtime.subscribe`, thêm `x-authenticated: true`
  (extension, không đổi lược đồ yêu cầu/trả lời; không phá vỡ). Client màn hình không đổi.
- Chỗ dùng trên màn hình: console gọi `/api/v1/me` lúc mở; nhân viên chưa có grant giờ nhận 200 `grants: []` thay vì 403.
- Câu sqlc, bảng, pha migration: `queries/identity` `GetRoleByKey` thêm `FOR SHARE`; bảng `identity.roles`,
  `identity.role_permissions` (chỉ ghi thêm, như cũ). Không migration mới (không đổi schema).
- Test sẽ chạy: vùng cao → cả bộ `./internal/modules/identity/...`, `./internal/httpapi/...` (guard.go);
  `./tools/routegen/...`, `./tests/arch/...`, `./tests/e2e/...` (migrate, seed), `./cmd/crm/...`; rồi `make ci`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` — `tenancy.ListTenantIDs`
  (danh sách tenant), `tenancy.WithTenant` (ngữ cảnh), `identity.EnsureDefaultRoles` (thêm vai trò/quyền mẫu còn thiếu,
  bỏ qua vai trò customized). Tìm "tenant", "role", "sync", "x-public" trong mã.
- Dùng lại / viết mới vì: bước đồng bộ = vòng `ListTenantIDs` × `WithTenant(EnsureDefaultRoles)`, không viết lại luật
  thêm quyền. Chế độ "chỉ cần đăng nhập" viết mới trong routegen/guard (chưa có), cùng cách khai với `x-public`.

## Soát thiết kế

- 5.1 Đúng chủ dữ liệu: vai trò, quyền thuộc `identity`; bước đồng bộ là hàm của `identity`, `cmd/crm` chỉ gọi.
- 5.2 Không ghi bảng module khác: chỉ ghi `identity.roles`, `identity.role_permissions`; đọc `platform.tenants` qua
  `tenancy.ListTenantIDs` (public API).
- 5.3 không áp dụng: không đọc dữ liệu module khác ngoài API của tenancy.
- 5.4 Mỗi tenant đồng bộ trong một transaction của tenant đó; giữa các tenant không cần nguyên tử (chạy lại được).
- 5.5, 5.6 không áp dụng: không có việc sau commit, không gọi ra ngoài.
- 5.7 Mọi truy vấn trong `tenancy.WithTenant`; không bảng mới.
- 5.8 Module không nhập `net/http`; luật "chỉ cần đăng nhập" ở `guard.go` + routegen.
- 5.9 `getMe` là route đọc; routegen chặn `x-authenticated` trên route ghi (route ghi luôn cần quyền).
- 5.10, 5.11 không áp dụng: không Valkey, không thư viện mới.
- 5.12 Theo module mẫu: lỗi bọc một lần, test ở gói `_test` trên PostgreSQL thật.
- 6.1 Đồng bộ chạy cùng lúc với chủ shop sửa vai trò (Task M1-56): `GetRoleByKey … FOR SHARE` khóa dòng vai trò trước
  khi đọc `customized` (thứ tự nhóm 2: roles → role_permissions), nên sửa vai trò đã commit thì đồng bộ thấy
  `customized`, sửa sau thì chờ đồng bộ xong. Hai lần migrate cùng lúc: `ON CONFLICT DO NOTHING` ở cả hai câu ghi.
- 6.2 Chạy lại không đổi gì (idempotent theo dữ liệu); không có đường HTTP ghi mới.
- 6.3 Khóa theo đúng thứ tự roles → role_permissions trong một tenant; mỗi tenant transaction riêng.
- 6.4 `WithTenant` chạy lại khi 40P01/55P03 (RetryTx); migrate là tiến trình chạy một lần, lỗi thì thoát khác 0.
- 6.5 READ COMMITTED + `FOR SHARE`.
- 6.6–6.8 không áp dụng: không việc nền, không gọi ngoài; tiến trình chết giữa chừng → tenant dở lùi, lần migrate sau làm tiếp.
- 6.9 Dùng mặc định timeout của db.
- 6.10 không áp dụng: không tiền.
- 6.11 Quy luật `Can` có rapid (`TestCanProperty`, sửa mô hình theo nghĩa mới); quy luật PQ07 (`TestStaffInvariantsProperty`) giữ nguyên và phải vẫn đạt.
- 6.12 Log của bước đồng bộ chỉ có số tenant; không email.
- 6.13 Mặc định từ chối giữ nguyên: không grant nào → mọi route có quyền 403; quyền có phạm vi vẫn kiểm theo đích;
  `/me` vẫn qua auth (thẻ + MFA + tài khoản active), chỉ trả dữ liệu của chính người gọi. Gán quyền: người gán phải có
  quyền hiệu lực (`Can`) — quyền `ScopeNone` của người được gán hiệu lực toàn shop dù gán theo kho, nên người gán cũng
  chỉ cần có nó ở bất kỳ grant nào; không ai nhận được quyền hiệu lực mà người gán không có (không tự nâng quyền).

## Quyết định thay chủ shop

- **Q1 — Cách khai "/me chỉ cần đăng nhập".** Chọn: dấu mới `x-authenticated: true` trong `openapi.yaml` (routegen sinh
  `Route.Authenticated`; guard cho qua khi auth đã nạp được nhân viên). routegen chặn: đi cùng `x-permission` hay
  `x-public`, giá trị khác `true`, dùng trên route ghi; `tests/arch` đòi tập route này đúng bằng `GET /api/v1/me`.
  Vì: hợp đồng vẫn là nguồn duy nhất, không có "quyền giả", không lan sang route ghi được. Phương án khác: một quyền
  giả mọi vai trò đều có (sai với "người chưa có gán vẫn mở được /me"); bỏ qua guard theo tên route (khó thấy). Đảo
  ngược: trả `x-permission` cho `getMe`.
- **Q2 — Nhân viên chưa có gán nào.** Kế hoạch ghi "người không có gán nào → 403" cạnh "/me chỉ cần đăng nhập". Chọn:
  `/me` 200 với `grants: []` (đúng Step 1 của Task 26, thay quyết định D1 của Task 26), mọi route có quyền khác 403
  (PQ03 "không thấy gì": `/me` chỉ trả email và mã của chính người gọi).
- **Q3 — Luật gán quyền (PQ07) theo nghĩa mới của `Can`.** `hasAllPermissions` vẫn dùng `Can`, nên quyền `none` của
  vai trò được gán chỉ đòi người gán có quyền đó ở bất kỳ grant nào (trước: phạm vi `all`). Vì: quyền `none` giờ hiệu
  lực toàn shop với cả hai bên, nên tập quyền hiệu lực của người được gán vẫn nằm trong của người gán (không tự nâng
  quyền; `TestStaffInvariantsProperty` 200 lượt vẫn đạt). Quyền theo kho vẫn đòi đúng kho. "Chủ còn hoạt động" vẫn chỉ
  đếm `owner` phạm vi `all`; người có `owner` theo một kho không khóa hay gỡ được Chủ `all` (thiếu quyền kho ở `all`).
  Phương án khác: giữ luật cũ (chặt hơn, nhưng lệch với cách quyền có hiệu lực). Hệ quả cần biết: gán `owner` hay
  `manager` theo một kho cho quyền `none` của vai trò đó (với `owner`: cả `identity.users.manage`, `platform.*`) trên
  toàn shop — đúng quyết định của kế hoạch; đã ghi ở `docs/quyen.md`.
- **Q4 — "Tạo shop".** CRM chưa có đường tạo shop nào ngoài `crm seed-dev` (đã gọi `EnsureDefaultRoles`). Chọn: bước
  đồng bộ sau `migrate` chạy `EnsureDefaultRoles` cho **mọi** shop, nên shop tạo bằng bất kỳ cách nào cũng có đủ vai
  trò mẫu ở lần migrate kế tiếp. Không cần migration (không đổi schema).
- **Q5 — Chạy đồng thời.** `GetRoleByKey` thêm `FOR SHARE` để lần sửa vai trò của Task M1-56 không bị đồng bộ ghi đè
  (Soát thiết kế 6.1). Dễ đảo ngược.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Vai trò Kho gán theo kho A: `/me` 200, đọc sản phẩm (`catalog.read`) 200, phiếu kho A 200, kho B 403; người không có gán: mọi route có quyền 403 | `TestWarehouseScopedRole` (route thử `_probe/catalog`, `_probe/warehouse/{id}`) | `go test -count=1 -v ./internal/httpapi/ -run 'TestAuthUsersAndGrants\|TestGetMe\|TestWarehouseScopedRole\|TestStaffAPI_LockAndCreate'` | `--- PASS: TestWarehouseScopedRole (0.42s)` |
| `/me` chỉ cần đăng nhập: thiếu `realtime.subscribe` 200; chưa có grant 200 `grants: []`; bị khóa 403; email lạ 403 | `TestGetMe`, `TestStaffAPI_LockAndCreate` | như trên | `--- PASS: TestGetMe (0.25s)`, `--- PASS: TestStaffAPI_LockAndCreate (0.39s)` |
| `Can`: quyền `none` từ grant ở bất kỳ phạm vi; quyền theo đích không đổi | `TestCanTable`, `TestCanProperty` (rapid), `TestAuthUsersAndGrants` | `go test -count=1 -v ./internal/modules/identity/ -run 'TestCanTable\|TestCanProperty\|TestSyncDefaultRoles\|TestStaffInvariantsProperty\|TestPermissionsDoc_MatchesCode\|TestEnsureDefaultRoles'` | `--- PASS` cả ba |
| Shop mới có đủ vai trò mẫu; quyền mới vào vai trò mẫu chưa sửa; vai trò đã sửa, vai trò tự đặt không đổi; chạy hai lần không đổi | `TestSyncDefaultRoles`; `tests/e2e` `TestMigrateSyncsDefaultRoles` (tệp `crm` thật, vai trò migrate) | như trên; `go test -count=1 -v ./tests/e2e/ -run 'TestMigrateSyncsDefaultRoles\|TestSeedDev'` | `--- PASS: TestSyncDefaultRoles (0.25s)`, `--- PASS: TestMigrateSyncsDefaultRoles (12.72s)`, `--- PASS: TestSeedDev (3.39s)` |
| Luật PQ07 vẫn đúng với nghĩa mới của `Can` | `TestStaffInvariantsProperty` (200 lượt), cả bộ identity | như trên; `make ci` | `--- PASS: TestStaffInvariantsProperty (17.45s)`; `ok …/modules/identity 95.691s` |
| routegen: `x-authenticated` sinh `Authenticated: true`; chặn khi đi cùng dấu khác, khác `true`, trên route ghi; tập route chỉ cần đăng nhập = `GET /api/v1/me` | `TestRejectsBadContracts/authenticated-bad.yaml`, `TestGeneratesRoutes`, `tests/arch` `TestRoutesPublicSetAndPermissions` | `go test -count=1 -v ./tools/routegen/`; `go test -count=1 -v ./tests/arch/ -run TestRoutesPublicSetAndPermissions` | `--- PASS` cả ba |
| `docs/quyen.md` khớp mã | `TestPermissionsDoc_MatchesCode` | như trên | `--- PASS` |
| Toàn bộ cổng kiểm (build, vet, lint, gen-check, sqlc vet, `go test -race ./...`, govulncheck, arch, oasdiff, jscpd, test-guard, danh mục, gitleaks) | — | `make ci CRM_PG_PORT=5815` | `exit=0` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chưa có route sản phẩm, phiếu kho thật (Task 27, 28): ca "GET sản phẩm 200, ghi phiếu kho A 200 / B 403" kiểm qua
  chuỗi middleware thật (auth → guard → handler kiểm đích) trên hai route thử, cùng cách các route thử `_probe` của M0.
- Câu kiểm cũ đổi theo quyết định của kế hoạch (không xóa test): `TestAuthUsersAndGrants` ("audit.read chỉ trên
  page:p1" 403 → 200), `TestGetMe` ("thiếu realtime.subscribe" 403 → 200, thêm ca chưa grant, bị khóa),
  `TestStaffAPI_LockAndCreate` (`/me` của nhân viên mới 403 → 200 `grants: []`), `TestCanTable` và `canModel`.
- `tests/e2e/migrate_test.go` gom phần chạy migrate vào `migrateRunner`; `cmd/crm` gom `openOwner` (jscpd báo trùng
  khi thêm bước đồng bộ). Thay đổi cấu trúc nhỏ, đi chung commit với hành vi để jscpd qua.
- Giữa task, `main` đã gộp Task 314 và 49: nhánh chưa có commit nên tiến nhanh (`git merge --ff-only main`), không
  trùng tệp với task này; chạy lại `make ci`.

## Việc phát hiện thêm

- (Xong trên main, mục 7 của luật `openapi.md`.) ~~Tệp luật hợp đồng API cần thêm `x-authenticated`.~~
- `make ci` đỏ trên mọi nhánh có `console/node_modules` (từ Task 17): gói npm `flatted` chứa tệp Go
  (`golang/pkg/flatted`), `go list ./...` gồm nó nên `golangci-lint run ./...` và `TestLintCleanOnRealCode` đỏ
  (cũng lọt vào vet, test, govulncheck). Cách sửa gợi ý cho một task riêng: một `go.mod` rỗng trong
  `console/node_modules` (bước `console-deps`) hoặc lọc `node_modules` khỏi danh sách gói của Makefile và test lint.
- Task tạo shop sau này (nếu có) gọi `identity.EnsureDefaultRoles` trong transaction tạo shop; hiện bước đồng bộ sau
  `migrate` phủ việc này.
- Console: nhân viên chưa có grant giờ mở được `/me` (200, `grants: []`); màn hình nên hiện "chưa được giao việc"
  thay cho lỗi (task màn hình).

## Reviewer

Kết luận: ĐẠT (lần 1, không có phát hiện phải sửa).

Reviewer kiểm: mọi nhánh của `Can` mới (`canModel` độc lập, khớp); guard chỉ bỏ đòi quyền khi `Authenticated` và
không có quyền và không phải route ghi, routegen và `tests/arch` chặn thêm; PQ07 vẫn chống tự nâng quyền (người có
`owner` theo kho không gán được `owner` `all`, không khóa/gỡ được Chủ `all`); `FOR SHARE` không xung đột với
`SeedOwner`, hai lần đồng bộ khóa cùng thứ tự; `openOwner` đóng pool, lỗi bọc một lần; Ảnh hưởng khớp diff; câu kiểm
đổi kỳ vọng là đổi hành vi theo kế hoạch, đã ghi.

Lượt 1 (commit `13fe6d7`). Ghi chú không bắt buộc (không sửa trong task này): (1) `checkAuthority` của test rapid dùng chính `Can` làm chuẩn —
câu "không tự nâng quyền" dựa vào lập luận Q3 cộng `Can` khớp `canModel`; (2) chủ shop nên biết: gán `owner`/`manager`
theo một kho cho toàn bộ quyền `none` của vai trò trên toàn shop; khi Task M1-56 cho sửa vai trò, thêm
`identity.users.manage` vào vai trò hay gán theo kho sẽ mở quản lý nhân viên toàn shop; (3) Task M1-56: transaction
sửa vai trò khóa `roles` trước `role_permissions` (ADR-46 nhóm 2), làm ngược có thể deadlock với `FOR SHARE` của bước
đồng bộ; (4) `TestSyncDefaultRoles` giả định mỗi test một database riêng (đúng với testkit hiện tại).

## Lượt 2

Giao ngày 18/11/2026 (điều phối viên): (1) gộp `main` (Task 16 realtime, luật `x-authenticated` trong luật hợp đồng
API); (2) quyền quản trị chỉ có hiệu lực từ gán vai trò toàn shop. Hai commit, mỗi commit qua reviewer.

### 2a. Gộp main (commit merge)

- `git merge main` (không rebase). Tự gộp: `cmd/crm/main.go`, `openapi/openapi.yaml`, `internal/httpapi/auth_test.go`,
  mã sinh ogen. Xung đột duy nhất: tệp trạng thái task (giữ "Task 315, đang làm" bằng `new-task.sh`).
- Sau gộp: `make gen` không chênh với bản đã gộp (gen-check của `make ci` qua); `make ci` `exit=0`
  (`ok …/tests/arch`, `ok …/tests/e2e`, govulncheck, oasdiff, jscpd 0 clone, test-guard, gitleaks).
- Việc phát hiện thêm của lượt 1 về luật `openapi.md`: `main` đã có mục 7 `x-authenticated` — xong.
- Reviewer (commit merge): **ĐẠT**, không phát hiện phải sửa. Kiểm: `main.go` giữ `syncDefaultRoles` và hub realtime;
  `getMe` giữ `x-authenticated`, `stream` giữ `x-permission: realtime.subscribe`; `routes_gen.go` khớp; `auth_test.go`
  dùng `testkit.AccessKeys.Verifier` mới, ca `/me` của 315 còn đủ; hub SSE lọc bằng chính `identity.Can` nên theo
  nghĩa mới. Gợi ý cho 2b: thêm ca SSE "báo hiệu cần quyền quản trị không tới người chỉ có quyền đó theo trang".

### 2a'. Gộp main lần 2 (commit merge)

- Reviewer (commit merge lần 2): **ĐẠT**, không phát hiện phải sửa; xác nhận lỗi lint là của main (`Makefile`,
  cấu hình lint, `console/package*.json` giống hệt main), `main.go` giữ đủ phần của Task 315, 16, 42.
- Trong lúc làm 2b, `main` gộp thêm Task 17 (khung màn hình React, client sinh `console/src/api` từ hợp đồng) và
  Task 42 (imager). Cất phần 2b bằng `git stash push -m task315-luot2b-wip`, `git merge main`; xung đột duy nhất: tệp
  trạng thái task (giữ "Task 315, đang làm" bằng `new-task.sh`).
- `make gen` sinh lại `console/src/api/sdk.gen.ts`: client của main sinh trước khi có mô tả `getMe` của lượt 1 (chỉ
  thêm hai dòng chú thích). Không sửa tay.
- Kiểm: build, vet, gen-check, sqlc vet, `go test -race ./...` (mọi gói `ok` trừ hai mục dưới), govulncheck, oasdiff,
  danh mục, jscpd (0 clone), test-guard, gitleaks; console `npm test` 35 qua, `tsc --noEmit` qua.
  - `golangci-lint run ./...` và `tests/arch` `TestLintCleanOnRealCode` đỏ vì `console/node_modules/flatted/golang/pkg/flatted`
    (tệp Go trong gói npm, `go list ./...` gồm nó) — lỗi của `main` từ Task 17, không do gộp; lint trên mọi gói trừ
    `node_modules`: `0 issues`; `go test ./tests/arch/ -skip TestLintCleanOnRealCode` `ok`. Ghi ở Việc phát hiện thêm.
  - `internal/imager` hai test đo thời gian đỏ khi máy tải 20 trên 10 lõi; chạy riêng `-count=2` `ok`.
