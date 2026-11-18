# Task 07 — Identity — người dùng, quyền theo phạm vi, Cloudflare Access có dấu bước hai

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md` (Task M0-07). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `httpapi.Deps` (12 chỗ trong 6 tệp: `server.go`, `errors.go`, `middleware.go`, `middleware_test.go`,
    `cmd/crm/main.go`, `tests/arch/routes_test.go`): **thêm** trường `Access identity.Verifier` và `RateLimit`
    (ngưỡng giới hạn tần suất, mặc định 300/60 s). Không đổi trường cũ; chỗ dùng cũ vẫn biên dịch. `cmd/crm` điền
    `Access`.
  - Hàm không xuất `denyUntilAuth` (1 chỗ dùng: `chain` trong `server.go`) bị **xóa**, thay bằng `auth` → `guard` →
    `userRateLimit` (luật "chuyển sang cách mới thì xóa cách cũ").
  - `knownPermissions()` trong `tests/arch/routes_test.go` (1 chỗ dùng) bị **xóa**, thay bằng `identity.Permissions`.
  - Ký hiệu mới: gói `internal/modules/identity` (`Permission`, `PermissionDef`, `Permissions`, `Scope`, `Grant`,
    `AuthUser`, `Can`, `ScopesFor`, `Verifier`, `CheckMFA`, `ErrInvalidToken`, `ErrMFARequired`, `LoadUser`,
    `ErrUnknownUser`, `ErrLocked`, `SeedOwner`), gói `internal/adapters/cfaccess` (`Options`, `New`), trong
    `tenancy`: `SeedTenant`, `AddRoute` (cho lệnh `crm seed-dev`, chỉ thêm hàm), trong `testkit`: `AccessClaims`,
    `AccessKeys`, `AccessKeyServer`.
- Gói phụ thuộc ngược (go list): `internal/httpapi` được `cmd/crm`, `tests/arch` nhập. `tenancy` được `cmd/crm`
  (mới), identity (mới) và test của `tenancy`, `tests/arch` nhập. `testkit` được test của `db`, `metrics`,
  `tenancy`, `httpapi`, `tests/arch`, `tests/e2e` nhập (chỉ thêm tệp). `identity`, `cfaccess` mới.
  `tests/e2e` khởi động vai trò api có `ACCESS_*` giả: khóa của Access tải lười (lần kiểm thẻ đầu), nên khởi động
  không gọi mạng và e2e phải vẫn xanh.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. `getMe` được hiện thực (trước 501); hình dạng `Me`
  đã khai ở Task 6 nên oasdiff không có thay đổi.
- Chỗ dùng trên màn hình: chưa có màn hình (Task 17).
- Câu sqlc, bảng, pha migration: **mở rộng**. `migrations/0003_identity.sql` tạo `identity.users`, `identity.roles`,
  `identity.role_permissions`, `identity.user_role_grants` (đã có trong bảng thứ tự khóa ADR-46, nhóm 2), RLS bằng
  `platform.enable_tenant_rls`, `crm_app` chỉ `SELECT`. Câu mới `queries/identity/users.sql` (đọc user, grant; chèn
  cho seed), `queries/platform/tenant_routes.sql`, `tenants.sql` thêm câu cho seed. Khối sqlc mới cho
  `queries/identity` → `internal/modules/identity/internal/store`.
- Test sẽ chạy: vùng rủi ro cao (`internal/modules/identity`, `internal/httpapi/guard.go`) nên chạy cả bộ của vùng:
  `go test -race ./internal/modules/identity/... ./internal/adapters/... ./internal/httpapi/...
  ./internal/platform/tenancy/... ./tests/arch/... ./tests/e2e/... ./cmd/...` trên dự án compose `crm-t07` (cổng
  5507); cuối cùng `make ci`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có `tenancy.Resolve`,
  `tenancy.WithTenant`, `db.RetryTx`, `db.TxAPI`, `config` (đã có `AccessTeamDomain`, `AccessAUD`, `AccessMFAClaim`,
  `AccessMFAValues` từ Task 3), `testkit.OpenAppDB`, `StartPostgres`. Tìm `jwt`, `jwks`, `ratelimit`, `limiter`,
  `Retry-After`, `AuthUser`, `rsa` trong `internal/`, `cmd/`, `tools/`: chưa có gì.
- Dùng lại / viết mới vì:
  - Tìm tenant theo email: `tenancy.Resolve("staff_email", …)`; nạp user và grant trong `tenancy.WithTenant` (không
    tự đặt `app.tenant_id`).
  - Biến cấu hình `ACCESS_*`: dùng nguyên `config.Config` của Task 3, không thêm biến.
  - Kiểm chữ ký thẻ: `github.com/golang-jwt/jwt/v5` v5.3.1 (đúng dòng "Kiểm thẻ Cloudflare Access" của ADR-44), không
    tự viết RS256. Đọc JWKS: `encoding/json`, `crypto/rsa`, `math/big` của thư viện chuẩn (bộ JWKS nhỏ, chỉ RSA).
  - Giới hạn tần suất: viết mới trong `httpapi` (chỉ lớp HTTP dùng); thư viện chuẩn không có cửa sổ trượt,
    `golang.org/x/time/rate` là token bucket (không đúng "300 trong 60 s trượt") và không có trong ADR-44.
  - Lỗi HTTP: thêm mã vào `errors.go` (chỗ duy nhất đổi lỗi sang mã HTTP), dùng lại `writeError`.
  - Bật RLS: `platform.enable_tenant_rls`; kiểm quy tắc schema: `tenancy.CheckSchemaRules` có sẵn.
  - Khóa thử, máy chủ khóa: `crypto/rsa` + `net/http/httptest` trong `testkit/accessjwt.go` (testkit là chỗ của
    "JWT thử" theo Cấu trúc file của kế hoạch).

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: người dùng, vai trò, quyền theo phạm vi thuộc `identity` (04 mục 3); bảng trong schema `identity`.
   Định tuyến email → tenant là `platform.tenant_routes` (platform sở hữu): seed ghi qua hàm của `tenancy`, không
   qua `identity`.
2. Không ghi bảng module khác: `identity` chỉ ghi bảng `identity.*`; dòng `tenant_routes` ghi bằng `tenancy.AddRoute`.
3. Đọc dữ liệu module khác: `identity` đọc tenant qua `tenancy.Resolve` (public API của platform), không đọc thẳng.
4. Việc nguyên tử: không áp dụng ở đường yêu cầu (chỉ đọc). Seed chạy ba transaction riêng (tenant, user + quyền,
   định tuyến), mỗi bước chạy lại được (`ON CONFLICT`), chạy lại cả lệnh là đủ khi đứt giữa chừng.
5. Việc sau commit: không áp dụng: không phát sự kiện (`user.*` có khi có đường ghi người dùng).
6. Gọi ra bên ngoài: tải khóa công khai của Cloudflare Access là GET chỉ đọc, chạy lại vô hại, không phải lệnh có
   trạng thái `in_doubt`; nằm ngoài transaction, trong `internal/adapters/cfaccess`.
7. Ngữ cảnh shop: bảng mới có `tenant_id`, `UNIQUE (tenant_id, id)`, unique khác bắt đầu bằng `tenant_id`, khóa
   ngoại ghép `(tenant_id, …)`, RLS ép; đọc trong `tenancy.WithTenant`. Test `CheckSchemaRules` vẫn `[]`.
8. Nghiệp vụ không biết HTTP: `identity` không nhập `net/http` (luật b của `boundaries_test`): phần gọi mạng ở
   `internal/adapters/cfaccess`, `cmd/crm` nối (xem Quyết định 1).
9. Đường ghi: không thêm đường ghi; `guard` đặt trước mọi route không công khai, kể cả route ghi.
10. PostgreSQL là nguồn sự thật: trạng thái user và grant đọc từ PostgreSQL ở mỗi yêu cầu; bộ đếm tần suất trong bộ
    nhớ chỉ điều tiết, không quyết định nghiệp vụ.
11. Thư viện: `golang-jwt/jwt/v5` v5.3.1 có trong ADR-44; không thêm thư viện khác.
12. Module mẫu: task này tạo mẫu (Step 4b, ghi vào CLAUDE.md).

Mục 6 (đúng khi chạy đồng thời):

1. Sửa cùng lúc: không có đường sửa ở yêu cầu. Bộ nhớ đệm khóa và bộ đếm tần suất dùng chung giữa goroutine: khóa
   bằng mutex, test chạy `-race` (100 thẻ `kid` lạ song song).
2. Gửi lại cùng yêu cầu: chỉ đọc. Seed chạy lại được (`ON CONFLICT DO NOTHING/UPDATE`).
3. Thứ tự khóa: seed chạm `platform.tenants` → `platform.tenant_routes` (nhóm 1) và `identity.*` theo thứ tự nhóm 2
   (users → roles → role_permissions → user_role_grants), mỗi nhóm một transaction.
4. Lỗi tạm của database: `WithTenant` qua `RetryTx`; hết lượt → `db.ErrRetryLater` → 503 `retry_later` (không lưu).
5. Mức cô lập: mặc định READ COMMITTED, chỉ đọc.
6–8. Việc nền: không áp dụng. Tiến trình chết: không có trạng thái cần dọn (đệm khóa trong bộ nhớ nạp lại).
9. Treo lâu: tải khóa có hạn 5 s (`http.Client.Timeout`), tối đa một lần mỗi 30 s khi gặp `kid` lạ; Cloudflare
   không trả lời thì giữ khóa cũ, thẻ ký bằng `kid` lạ bị 401. Truy vấn theo giới hạn `TxAPI`.
10–11. Tiền: không áp dụng. Quy luật phải luôn đúng: rapid cho `Can` (200 lượt) và `CheckMFA`.
12. Dữ liệu cá nhân: email không vào log (lỗi của `LoadUser` không mang email; log chỉ có `request_id`, route);
    thẻ không bao giờ vào log; lỗi kiểm thẻ trả mã chung, không nêu lý do chi tiết cho client.
13. Quyền: mặc định từ chối: không thẻ hay thẻ sai → 401; không có dòng `user_role_grants` → 403; quyền không theo
    đích cần phạm vi `all`; `guard` kiểm ở API cho mọi route không công khai; `Deps.Access` nil → 401 (không mở
    cửa khi quên nối). Thẻ chỉ đọc từ header `cf-access-jwt-assertion`, không đọc cookie `CF_Authorization`.

## Quyết định thay chủ shop

1. **(Điều phối viên quyết) `identity` không nhập `net/http`.** `identity` khai `Verifier` (`Verify(ctx, token)
   (email, err)`), hai lỗi `ErrInvalidToken`, `ErrMFARequired` và luật bước hai `CheckMFA(claim, allowed)` (hàm
   thuần, test bằng rapid). Phần tải JWKS và kiểm chữ ký nằm trong bộ nối `internal/adapters/cfaccess` (như hãng
   vận chuyển, Meta trong spec), `cmd/crm` nối hai bên. Vì thế `AccessOptions`/`NewAccessVerifier` của Interfaces
   thành `cfaccess.Options`/`cfaccess.New`. Phương án khác: đặt trong `internal/platform` (nhưng đây là bộ nối một
   nhà cung cấp, không phải hạ tầng chung) hoặc nới luật (b) (trái ADR-44).
2. **(Điều phối viên quyết) Tên trường bước hai cấu hình được** (`ACCESS_MFA_CLAIM`, `ACCESS_MFA_VALUES`, điểm dừng A
   quyết định 3); giá trị thật chốt khi có ứng dụng Access thử. Trường là chuỗi hoặc mảng chuỗi; kiểu khác → thiếu
   bước hai. `ACCESS_MFA_VALUES` rỗng: `config.Load` đã từ chối từ Task 3, `cfaccess.New` kiểm lại (api không khởi
   động; không có cấu hình nào làm mọi thẻ qua).
3. **Danh sách quyền thật thay bản chép của Task 6**; `knownPermissions` bị xóa.
4. **`crm_app` chỉ có `SELECT` trên bảng identity ở M0** (chưa có đường ghi người dùng; seed chạy bằng chủ bảng).
   Task có đường quản lý người dùng thêm `GRANT` trong migration mới. Dễ đảo ngược.
5. **Email chuẩn hóa chữ thường** khi tìm `tenant_routes` (khóa văn bản phân biệt hoa thường) và khi seed; cột
   `users.email` là `citext`.
6. **Làm mới khóa lười:** không có goroutine nền; mỗi lần kiểm thẻ, khóa cũ hơn 10 phút hoặc gặp `kid` lạ thì tải
   lại (tối đa một lần mỗi 30 s, kể cả khi lần trước lỗi, để Cloudflare chập chờn không bị dội). Tải lỗi thì giữ
   khóa cũ. Khóa RSA dưới 2048 bit bị bỏ.
7. **Mã lỗi:** user lạ, user `locked` và thiếu quyền đều 403 `forbidden` (không cho biết email có tồn tại); lỗi tạm
   của database 503 `retry_later`; vượt tần suất 429 `rate_limited` có `retry-after` (giây, làm tròn lên).
8. **Test helper đổi dạng:** `testkit.AccessKeyServer(t) *AccessKeys` (có `TeamDomain`, `Client` tin chứng chỉ của
   máy chủ thử TLS, `Sign`, `SignForeign`, `Rotate`, `Fetches`) thay cho `SignAccessJWT` + `AccessKeyServer(t)
   string`: thẻ ký và khóa phục vụ phải cùng một cặp khóa, và đường tải khóa là `https://` như thật.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đỏ trước khi có mã | cả bốn gói mới/đổi; seed-dev | go test -count=1 ./internal/modules/identity/ ./internal/adapters/... ./internal/httpapi/ ./tests/arch/; go test -run TestSeedDev ./tests/e2e/ | `FAIL … [build failed]` cả bốn; seed: `lệnh lạ ["seed-dev" …]` |
| Thẻ hợp lệ có MFA + grant `audit.read` all → 200; không thẻ, sai aud, sai iss, hết hạn, khóa khác → 401 unauthenticated | TestAuthTokens (route thử `GET /api/v1/_probe`) | go test -race -count=1 -v ./internal/httpapi/ | `--- PASS: TestAuthTokens` |
| Thiếu trường MFA hoặc không chứa giá trị cho phép → 401 mfa_required | TestAuthTokens, TestVerify, TestCheckMFA, TestCheckMFAProperty | như trên; go test -race -count=1 -v ./internal/adapters/... ./internal/modules/identity/ | `--- PASS`; đột biến bỏ `CheckMFA` → `FAIL` ở cfaccess và httpapi (đã hoàn lại) |
| Thẻ chỉ trong cookie `CF_Authorization` (và chỉ `Authorization: Bearer`) → 401 | TestAuthTokens | như trên | `--- PASS`; đột biến đọc cookie → `chỉ cookie: mã 200, mong 401` (đã hoàn lại) |
| Email không có trong tenant_routes → 403; user locked → 403; khóa giữa hai yêu cầu → yêu cầu hai 403 | TestAuthUsersAndGrants, TestLoadUserErrors | như trên | `--- PASS` |
| Vai trò có `audit.read` phạm vi `page:p1` → 403; user không có dòng user_role_grants → mọi route 403 (kể cả route Stream) | TestAuthUsersAndGrants | như trên | `--- PASS`; đột biến guard luôn cho qua → `audit.read chỉ trên page:p1: mã 200, mong 403` (đã hoàn lại) |
| `conversation.read` trên page:p1: /page/p1 200, /page/p2 403; `scope_type='all', scope_id='x'` → lỗi CHECK (cả page rỗng, loại lạ, status lạ) | TestAuthUsersAndGrants, TestGrantScopeCheck | như trên | `--- PASS` |
| Quy luật Can (rapid, 200 lượt): đúng khi và chỉ khi có grant của p phạm vi all hoặc đúng target; Grants rỗng luôn sai; grant quyền khác không làm Can đúng | TestCanProperty, TestCanTable, TestScopesFor | go test -race -count=1 -v ./internal/modules/identity/ | `--- PASS`; đột biến bỏ điều kiện "quyền theo đích" → `[rapid] failed after 15 tests: Can([{realtime.subscribe {page p1}}] …) = true, mong false` (đã hoàn lại) |
| Ngưỡng thử 5/s: thứ 6 → 429 retry-after 1; user khác 200; sau 1 s 200; route Stream không đếm; cửa sổ trượt; mặc định 300/60 s; không vượt khi song song; dọn user cũ | TestUserRateLimitHTTP, TestUserLimiterSliding, TestRateLimitDefaults, TestUserLimiterConcurrent, TestUserLimiterSweeps | go test -race -count=1 -v ./internal/httpapi/ | `--- PASS` cả năm |
| Máy chủ khóa đổi kid: thẻ kid mới nhận sau một lần tải lại; 100 thẻ kid lạ song song → 1 lần tải; làm mới sau 10 phút; máy chủ khóa lỗi giữ khóa cũ, không dội trong 30 s; chưa có khóa → 401 | TestKeyRotation, TestUnknownKidThrottled, TestPeriodicRefreshKeepsOldKeysOnFailure, TestNoKeysYet, TestNewRejectsBadOptions | go test -race -count=1 -v ./internal/adapters/... | `--- PASS` cả năm (-race, httptest TLS, khóa RSA tự sinh) |
| `GET /api/v1/me` qua NewServer → `{userId, email, grants}`; thiếu `realtime.subscribe` → 403 | TestGetMe | go test -race -count=1 -v ./internal/httpapi/ | `--- PASS` |
| Deps thiếu Access → 401 (không mở cửa) | TestAuthWithoutVerifierDenies | như trên | `--- PASS` |
| LoadUser nạp đúng grant, email khác hoa thường, tách tenant; crm_app chỉ đọc, ngoài ngữ cảnh 0 dòng | TestLoadUserGrants, TestLoadUserTenantIsolation, TestAppRoleReadOnlyAndRLS | go test -race -count=1 -v ./internal/modules/identity/ | `--- PASS` |
| CheckSchemaRules vẫn `[]` sau 0003 | TestSchemaRulesStillClean, TestCheckSchemaRulesCleanSchema | như trên; ./internal/platform/tenancy/ | `--- PASS` |
| Route khai quyền thuộc `identity.Permissions` (bỏ bản chép của Task 6); identity không nhập net/http | TestRoutesPublicSetAndPermissions, TestBoundariesRealCode | go test -race -count=1 -v ./tests/arch/ | `--- PASS` |
| `crm seed-dev <email>` (migrate): tenant ELIMAZ, owner mọi quyền all, định tuyến; chạy 2 lần không thêm dòng; vai trò khác → lỗi nêu migrate | TestSeedDev, TestSeedOwner, TestSeedTenantAndAddRoute | go test -race -count=1 -v ./tests/e2e/ ./internal/modules/identity/ ./internal/platform/tenancy/ | `--- PASS` |
| Không vỡ gói khác; cổng kiểm | mọi gói; build, vet, lint, vuln, oasdiff, jscpd, test-guard, gitleaks, sqlc vet, gen ổn định | go test -race -count=1 ./...; make build vet lint vuln oasdiff jscpd test-guard; make sqlc-vet; gitleaks dir; make gen rồi git status | 15 gói `ok`, không `FAIL`; make thoát 0 (`0 issues.`, `No vulnerabilities found.`, `Found 0 clones.`); sqlc vet `ok`; `no leaks found`; `make gen` không đổi thêm tệp. gen-check, catalog-check so với git nên chỉ xanh sau commit (danh mục đã sinh lại) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `AccessOptions`/`NewAccessVerifier` (Interfaces đặt trong identity) thành `cfaccess.Options`/`cfaccess.New` trong
  `internal/adapters/cfaccess`; `access_test.go` của identity thành `internal/adapters/cfaccess/cfaccess_test.go`
  (Quyết định 1). Luật bước hai vẫn ở identity (`CheckMFA`).
- Test helper: `testkit.AccessKeyServer(t) *AccessKeys` có `Sign`, `SignForeign`, `SignWithKid`, `Rotate`,
  `SetFailing`, `Fetches` thay `SignAccessJWT` (Quyết định 8). Thêm `testkit.OpenStaff` (pool crm_app và pool chủ
  bảng; dựng tenant, user, grant) dùng chung cho test identity, httpapi, tenancy.
- Thêm ngoài danh sách tệp: `internal/httpapi/handlers_me.go` (getMe); `tenancy.SeedTenant`, `tenancy.AddRoute`,
  `tenancy.ErrRouteConflict` và hai câu sqlc platform (seed ghi `platform.*` phải qua mã platform);
  `identity.SeedOwner`, `NormalizeEmail`, `RouteKindStaffEmail`, `IsKnown`, `KindOf`; `tests/e2e/seed_test.go`,
  `internal/platform/tenancy/seed_test.go`.
- Mã lỗi mới trong `errors.go`: `forbidden`, `mfa_required`, `rate_limited`, `retry_later`.
- Test rapid của identity chạy 200 lượt qua `TestMain` (đặt `-rapid.checks=200` khi người chạy chưa đặt).

## Việc phát hiện thêm

- **Thứ tự migration:** `0003` vào sau `0006` (Task 10 gộp trước). Database mới (test, staging lần đầu) không sao;
  database dev nào đã chạy `0006` thì goose báo migration thiếu (`Migrate` của M0 không bật out-of-order theo Q-X7):
  cần `make db-reset` cho database dev đó. Chưa có môi trường thật nên không ảnh hưởng dữ liệu.
- Bộ đếm tần suất đếm theo từng tiến trình api: chạy nhiều bản api thì ngưỡng thực tế nhân theo số bản (M0 một bản).
- Khi có ứng dụng Access thử: chốt `ACCESS_MFA_CLAIM`, `ACCESS_MFA_VALUES` thật (điểm dừng A quyết định 3).

## Reviewer

Lần 1 (agent reviewer, trên `git diff --cached`): **ĐẠT**, không có phát hiện phải sửa. Đã kiểm: chỉ đọc header
`cf-access-jwt-assertion`; RS256 bắt buộc, aud bắt buộc, iss, exp; MFA mặc định từ chối; làm mới khóa giữ mutex, chặn
tải lại 30 s kể cả khi lỗi, giữ khóa cũ; guard mặc định từ chối, Stream vẫn qua guard; `Can`/`ScopesFor` đúng kế hoạch;
migration 0003 đúng khóa ghép, RLS, crm_app chỉ SELECT; lỗi và log không mang email. Ghi chú không bắt buộc và cách xử
lý:

1. Làm mới định kỳ chạy đồng bộ trong mutex: Cloudflare chậm thì yêu cầu chờ tới 5 s, khoảng mỗi 10 phút một lần →
   giữ (đúng hành vi; tránh goroutine nền). Xem lại nếu đo thấy p95 nhảy.
2. Chú thích cũ "cho tới khi Task 7 có auth" ở `internal/httpapi/middleware_test.go:240` → giữ, test vẫn đúng (Deps
   không có Access); sửa khi chạm tệp đó.
3. Migration 0003 số nhỏ hơn 0006 → đã ghi ở "Việc phát hiện thêm".
4. `TestGrantScopeCheck` nhận lỗi CHECK bằng chuỗi → giữ (chỉ trong test).

Kết luận: ĐẠT
