# Task 189 — M5-06: Khóa dịch vụ của website (ADR-18)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 189, M5-06; đối chiếu
`docs/m5/doi-chieu-gia-dinh.md` dòng M0, M0b, M1r). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `identity.AuthUser` **thêm** `Kind string` (`identity.KindStaff` | `identity.KindService`) và `ServiceKeyID
    uuid.UUID`; không đổi trường cũ. Nơi dựng `AuthUser{…}`: `identity.LoadUser` (đặt `Kind: KindStaff`) và nhiều test
    (giữ nguyên: trường mới rỗng). `Can`, `GrantedAt` không đổi.
  - `identity.IsKnown`, `identity.KindOf`: thân đổi để nhận thêm tám quyền `web.*` của `identity.ServiceKeyPermissions`
    (danh sách mới, **không** nằm trong `identity.Permissions`, xem Quyết định 1). Chỗ gọi: `tests/arch/routes_test.go`
    (route `web.*` hợp lệ), `realtime/bus.go`, `identity/roles.go`, `permissions.go`. `identity.Permissions` thêm một
    quyền `identity.service_keys.manage` (toàn shop). Vai trò tự định nghĩa từ chối quyền `web.*` (`unknown_permission`).
  - Mới trong `identity`: `ServiceKeyPermissions`, `IsServiceKeyPermission`, `GenerateServiceKey`, `ServiceKey`,
    `CreateServiceKeyInput`, `CreateServiceKey`, `RevokeServiceKey`, `ListServiceKeys`, `ServiceKeyRecord`,
    `LoadServiceKey`, `CheckServiceKey`, `TouchServiceKeys`, `RouteKindServiceKey`, `TopicServiceKeyRevoked`, sentinel
    `ErrPermissionNotForKey`, `ErrInvalidKey`, `ErrRevoked`, `ErrIPNotAllowed`, `ErrServiceKeyNotFound`.
  - `httpapi`: mới `NewServiceKeyVerifier`, `KeyVerifier` (kiểu cụ thể, khớp interface `ServiceKeyVerifier`
    của Task 188), `ServiceKeyOptions`, `NewWebKeyLimiter`; không xuất: `serviceRateLimit` đổi chủ thể sang
    `svc:<ServiceKeyID>`, `idempotent` dựng actor bằng hàm mới `idempotencyActor` (`user:<UserID>` như cũ, `svc:<ServiceKeyID>`).
  - `config.Config` thêm `RateLimitsJSON` (RATE_LIMITS_JSON, chỉ api).
  - Không sửa `internal/platform/idempotency` (chỉ chỗ dựng `Scope.Actor` ở `httpapi/idempotency.go` đổi; xem Khác kế hoạch).
- Gói phụ thuộc ngược (go list): `identity` ← `cmd/crm`, `adapters/cfaccess`, `httpapi`, `modules/catalog`,
  `modules/customer`, `platform/files`, `platform/realtime` (+`realtimetest`), `tools/loadkit`, cùng test của
  `tests/arch`, `tests/e2e`; `httpapi` ← `cmd/crm`, `tests/*`; `config` ← nhiều gói (chỉ thêm trường).
- Operation OpenAPI bị chạm, oasdiff: **thêm** ba operation `listServiceKeys` (GET), `createServiceKey` (POST,
  `x-idempotency: no-replay`), `revokeServiceKey` (POST, `IdempotencyKey`), cùng quyền `identity.service_keys.manage`;
  schema mới `ServiceKey`, `CreatedServiceKey`, `CreateServiceKeyRequest`, `RevokeServiceKeyRequest`; `x-error-codes`
  thêm `ip_not_allowed`, `permission_not_allowed_for_key`. Chỉ thêm, không phá vỡ.
- Chỗ dùng trên màn hình: không (màn "Khóa cho website" là task màn hình sau; client sinh `console/src/api` đổi phần kiểu).
- Câu sqlc, bảng, pha migration: migration mở rộng `0501_service_keys.sql` (bảng mới `identity.service_keys`, RLS, quyền
  của `crm_app`: SELECT, INSERT, UPDATE theo cột `revoked_at, revoked_by, last_used_at, version`); câu mới
  `queries/identity/service_keys.sql` (Insert, List, GetByPrefix, Revoke, GetForRevoke, Touch). Bảng đã có ở nhóm 2
  ADR-46 (sau `user_sessions`, đã kiểm). `platform.put_tenant_route` đã nhận `service_key` (0290 dòng 25, đã kiểm),
  không sửa hàm.
- Test sẽ chạy: `go test -race ./internal/modules/identity/... ./internal/httpapi/... ./internal/platform/config/...
  ./cmd/crm/... ./tests/arch/...`; `scripts/agent/ci-task.sh` một lần trước review (vùng rủi ro cao identity: cả bộ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`tenancy.PutRoute`,
  `tenancy.Resolve`, `tenancy.WithTenant`, `identity.RecordAudit`, `realtime.Bus.SubscribeSystem`/`Publish` với
  `Signal.System`, `cache.RateLimiter`, `cache.ParseLimits`, `cache.Dial`); `httpapi.WriteTx`, `writeAuthError`,
  `matchError`, `limitBy`, `newUserLimiter`; `identity.ErrVersionConflict`, `ErrInvalidInput`.
- Dùng lại / viết mới vì:
  - Tuyến khóa → tenant: `tenancy.PutRoute(…, "service_key", prefix)` và `tenancy.Resolve` (không ghi bảng tuyến trực tiếp).
  - Báo hiệu thu hồi: `realtime.Bus.Publish` với `System: true` trong `WriteTx`; bộ kiểm nghe bằng `SubscribeSystem`.
  - Giới hạn theo khóa: `cache.RateLimiter.Allow` với `Limits.WebKey` (bộ chuyển vài dòng `NewWebKeyLimiter` vào
    interface `WebRateLimiter` của Task 188); không viết bộ đếm mới.
  - Đệm trong tiến trình của bộ kiểm khóa: viết mới (map + hạn + thế hệ), vì `cache.ReadCache` là bộ đệm Valkey dùng
    chung giữa tiến trình, không xóa ngay theo báo hiệu trong tiến trình và không được giữ băm khóa ngoài PostgreSQL.
  - Sinh ngẫu nhiên: `crypto/rand` + `encoding/base32` thư viện chuẩn; so băm `crypto/subtle`.

## Soát thiết kế

- 5.1 Chủ dữ liệu: khóa dịch vụ thuộc `identity` (ADR-18, kế hoạch), bảng `identity.service_keys`.
- 5.2 Không ghi bảng module khác: tuyến qua `tenancy.PutRoute` (hàm của platform); không chạm bảng khác.
- 5.3 không áp dụng: không đọc dữ liệu module khác.
- 5.4 Nguyên tử: tạo khóa = tuyến + dòng khóa + audit trong một transaction (`WriteTx`); thu hồi = UPDATE có điều kiện +
  audit + báo hiệu trong một transaction.
- 5.5 Sau commit: báo hiệu thu hồi chỉ tới khi commit (pg_notify); `last_used_at` ghi ngoài đường yêu cầu bằng goroutine có chủ.
- 5.6 không áp dụng: không gọi dịch vụ ngoài.
- 5.7 Bảng mới có `tenant_id`, RLS ép, `UNIQUE (tenant_id, id)`, unique khác bắt đầu bằng `tenant_id`, khóa ngoại ghép
  `(tenant_id, created_by) → identity.users`; mọi câu trong `WithTenant` (`CheckSchemaRules` = `[]`, có test).
- 5.8 Module `identity` không nhập `net/http`, `realtime` (vòng nhập): bộ kiểm có đệm và goroutine nằm ở `httpapi`,
  phần thuần (tách, so băm, CIDR) ở `identity.CheckServiceKey`.
- 5.9 Ba operation khai quyền; hai đường ghi có `IdempotencyKey` (`createServiceKey` là `no-replay`).
- 5.10 Valkey chỉ đếm tần suất; khóa, thu hồi, quyền đều từ PostgreSQL. Valkey ngừng → bộ đếm trong tiến trình.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu: module identity (sqlc, sentinel), đường ghi Task 12, lỗi ở `errors_identity.go`.
- 6.1 Thu hồi: `UPDATE … WHERE id AND version AND revoked_at IS NULL` (0 dòng → phân biệt không có / lệch version).
  `last_used_at` chỉ ghi khi mốc cũ hơn (`last_used_at IS NULL OR last_used_at < @at`) nên hai tiến trình ghi lệch thứ tự không lùi mốc.
- 6.2 Gửi lại `createServiceKey` cùng khóa → 409 `already_processed`, không tạo khóa thứ hai, không lưu thân (bí mật).
  Thu hồi gửi lại → phát lại kết quả đã lưu.
- 6.3 Khóa: một dòng `service_keys` mỗi thu hồi; tạo khóa chỉ chèn (tuyến platform rồi dòng khóa, giống M3 với
  carrier_account). Không đường nào khóa hai bảng nhóm 2 theo thứ tự ngược.
- 6.4 `WriteTx`/`WithTenant` có thử lại; hàm trong tx gán lại kết quả (khóa sinh mới ở mỗi lần chạy lại; bí mật chỉ
  trả ra sau commit).
- 6.5 READ COMMITTED + câu có điều kiện.
- 6.6–6.8 không áp dụng: không việc nền, không gọi ngoài. Goroutine ghi `last_used_at` lỗi thì bỏ lượt (chỉ là mốc tham khảo), ghi cảnh báo không kèm khóa.
- 6.9 Đọc khóa trong `WithTenant` mặc định (giới hạn câu 30 s); bộ kiểm có đệm 5 s nên không chạm database mỗi yêu cầu.
- 6.10, 6.11 không áp dụng: không tiền, tồn. (Quy luật "không bao giờ lộ bí mật" có test quét mọi cột.)
- 6.12 Bí mật: không log, không audit, không lưu trong `idempotency_keys`, không trong tuyến; chỉ lưu băm SHA-256.
  Tiền tố (không bí mật) được lưu, hiện, ghi audit. Lỗi của bộ kiểm không mang khóa.
- 6.13 Mặc định từ chối: khóa chỉ mang quyền `web.*` đã cấp; quyền ngoài tám quyền → 422; nhân viên không giữ được
  quyền `web.*`; route của nhân viên không nhận khóa (Task 188). CIDR rỗng = mọi IP; có CIDR mà IP thiếu/sai → 403.

## Quyết định thay chủ shop

1. **Tám quyền `web.*` nằm trong `identity.ServiceKeyPermissions`, không trong `identity.Permissions`.** `IsKnown`,
   `KindOf` nhận cả hai danh sách; `docs/quyen.md` có đủ chín dòng mới (test tài liệu so với cả hai danh sách). Vì:
   `Permissions` là bộ quyền của nhân viên (vai trò Chủ "đủ mọi quyền", danh mục của màn Vai trò, test seed) — nếu
   thêm `web.*` vào đó thì Chủ, Quản lý tự nhận quyền của khóa và màn Vai trò hiện chúng. Phương án khác: thêm vào
   `Permissions` rồi loại ở từng chỗ (năm chỗ, dễ sót). Đảo ngược được: chuyển dòng giữa hai danh sách.
2. **`identity.service_keys.manage` là loại `toàn shop` (ScopeShop), chỉ vai trò `owner`.** Kế hoạch ghi "đều
   ScopeNone", nhưng luật `docs/quyen.md` (Task 315 lượt 2) đòi quyền quản trị là `toàn shop`; chặt hơn và đúng luật.
   Quản lý không nhận (kế hoạch: chỉ owner).
3. **Bộ kiểm khóa có đệm nằm ở `httpapi` (`httpapi.NewServiceKeyVerifier`), không ở `identity`.** `realtime` nhập
   `identity`, nên `identity` không nhận `realtime.Bus` được (kế hoạch tự ghi điều này cho `Publish`). Phần thuần
   (tách khóa, so băm hằng thời gian, kiểm CIDR, dựng `AuthUser`) là `identity.CheckServiceKey`.
4. **Actor chống trùng của nhân viên giữ `user:<UserID>`** (kế hoạch ghi `u:`); đổi tiền tố sẽ làm khóa đang chạy của
   nhân viên thành dòng mới. Khóa dịch vụ: `svc:<ServiceKeyID>`. Dựng ở `httpapi/idempotency.go` (chỗ duy nhất dựng
   `Scope`), không đổi gói `platform/idempotency` (gói nền không nhập `identity`).
5. **`AuthUser.UserID` của khóa dịch vụ là `uuid.Nil`**: mọi chỗ cần "ai" của khóa đọc `ServiceKeyID` (giới hạn tần
   suất, chống trùng, audit); mã cũ lỡ dùng `UserID` làm người tạo sẽ hỏng rõ (khóa ngoại) thay vì ghi nhầm.
6. **Chỉ quay số Valkey `gate`** ở vai trò api (cho `RateLimiter`); kết nối `cache` chưa có bên dùng (ReadCache nối ở
   task API đọc của website). URL trống → `Conn` nil → bộ đếm trong tiến trình (`FallbackMax`), ghi cảnh báo một lần.
7. **Lệnh `crm service-key`** chạy bằng `DATABASE_URL` (`crm_app`, RLS vẫn áp) của vai trò đang chạy, như
   `recompute-customer-stats`; từ chối khi `CRM_ENV=production`. `revoke --id`
   tìm tenant bằng cách thử từng tenant (như `recompute-customer-stats`), người thu hồi ghi `system`, `revoked_by` NULL.
8. Thu hồi khóa đã thu hồi (cùng version) → `version_conflict` (409), không phải lỗi riêng.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test trước code: test mới của identity, httpapi không biên dịch trước khi có `GenerateServiceKey`, `AuthUser.Kind`, `NewServiceKeyVerifier`… | `servicekeys_test.go`, `servicekey_test.go`, `handlers_servicekeys_test.go` | `go vet ./internal/httpapi/` (và gopls) | `undefined: identity.GenerateServiceKey`, `undefined: NewServiceKeyVerifier`, `undefined: KeyVerifier` |
| Khóa đúng dạng `ek_` + 12 + `_` + 52 ký tự base32 thường, băm = sha256(khóa đầy đủ); thiếu byte ngẫu nhiên → lỗi; sai dạng không tách được | `TestGenerateServiceKey_Format`, `TestSplitServiceKey` | `go test -race ./internal/modules/identity/...` | `ok …/identity` |
| `crm_app` chèn thẳng `platform.tenant_routes` → permission denied; tạo khóa ghi đúng 1 tuyến `service_key` trỏ đúng tenant; tenant B `PutRoute` cùng tiền tố → `ErrRouteTaken`; tiền tố trùng lần đầu → khóa nhận tiền tố thứ hai, 1 tuyến; trùng hai lần → lỗi, không ghi gì; trùng trong chính tenant → sinh lại | `TestCreateServiceKey_Routes` | như trên | ok |
| Tám quyền `web.*` có trong `ServiceKeyPermissions`, loại none, không trong bộ quyền nhân viên, không vai trò mẫu; `identity.service_keys.manage` toàn shop, chỉ owner | `TestServiceKeyPermissions`, `TestPermissionsList` (thêm dòng), `TestDefaultRoles_Content` | như trên | ok |
| `permissions_doc_test` xanh với chín dòng mới (`docs/quyen.md` = `Permissions` ∪ `ServiceKeyPermissions`) | `TestPermissionsDoc_MatchesCode` | như trên | ok |
| Owner đã `customized` vẫn nhận `identity.service_keys.manage` khi `SyncDefaultRoles`; manager đã sửa không nhận | `TestSyncDefaultRoles_ServiceKeysManage` | như trên | ok |
| Vai trò tự định nghĩa không nhận quyền `web.*` | `TestCustomRole_RejectsServiceKeyPermission` | như trên | ok |
| Quyền nhân viên/lạ cho khóa → `ErrPermissionNotForKey`; tên rỗng, quá dài, không quyền, CIDR sai → `ErrInvalidInput`; chuẩn hóa quyền, CIDR | `TestCreateServiceKey_Validation` | như trên | ok |
| `CheckServiceKey`: khóa đúng → `Kind: service`, đúng quyền đã cấp, phạm vi all, `UserID` rỗng; sai một ký tự → `ErrInvalidKey`; tiền tố lạ → `ErrInvalidKey`; CIDR: IP ngoài, thiếu IP → `ErrIPNotAllowed`, IP trong (cả dạng `::ffff:`) → được; thu hồi → `ErrRevoked`; thu hồi mà sai bí mật vẫn `ErrInvalidKey` | `TestCheckServiceKey` | như trên | ok |
| Thu hồi: tenant khác, mã lạ → `ErrServiceKeyNotFound`; version cũ, thu hồi lần hai → `ErrVersionConflict`; audit staff/system, `revoked_by`; danh sách chỉ tenant mình | `TestRevokeAndListServiceKeys` | như trên | ok |
| `last_used_at` chỉ tiến lên, không đổi version | `TestTouchServiceKeys` | như trên | ok |
| Tạo qua API → 201 kèm khóa đầy đủ một lần; danh sách không có `secret`; gửi lại cùng `Idempotency-Key` → 409 `already_processed`, không bí mật, vẫn 1 khóa, `response_body` NULL; quét mọi cột (dòng ép text) của `service_keys`, `audit_logs`, `idempotency_keys`, `tenant_routes`: không có phần bí mật (đối chứng: câu quét thấy tiền tố); Quản lý 403; quyền `identity.users.manage` → 422 `permission_not_allowed_for_key`; CIDR sai → 400; tenant B không thấy khóa của A, thu hồi → 404; version cũ → 409 `version_conflict`; thu hồi đúng → 200, version 2 | `TestServiceKeysAPI_CreateListRevoke` | `go test -race ./internal/httpapi/` | `ok …/httpapi` |
| Khóa đúng → 200; sai một ký tự, tiền tố lạ → 401; khóa chỉ `web.catalog.read` gọi route thử cần `web.orders.create` → 403; `allowedCidrs: ["10.0.0.0/8"]`: `cf-connecting-ip: 1.2.3.4` → 403 `ip_not_allowed`, thiếu IP → 403, `10.1.2.3` → 200; thu hồi qua API → 401 trong ≤ 1 s nhờ báo hiệu PostgreSQL thật (đệm 1 giờ); khóa khác không ảnh hưởng | `TestServiceKeyRoute_VerifyAndRevoke` | như trên | ok |
| Báo hiệu mất (`SubscribeSystem` không phát): còn được ở giây 4, `ErrRevoked` ở giây 5 (đồng hồ giả); `Run` dừng → hủy đăng ký | `TestServiceKeyVerifier_TTLWithoutSignal` | như trên | ok |
| Báo hiệu thu hồi, resync xóa đệm ngay; chủ đề khác không xóa | `TestServiceKeyVerifier_SignalInvalidates` | như trên | ok |
| 100 yêu cầu trong 1 phút → `last_used_at` ghi đúng 1 lần (mốc yêu cầu đầu); phút sau ghi lần 2; ghi bằng goroutine của `Run` | `TestServiceKeyVerifier_LastUsedOncePerMinute` | như trên | ok |
| Lỗi database của bộ kiểm không mang khóa, không được đệm | `TestServiceKeyVerifier_ErrorNotCached` | như trên | ok |
| Giới hạn theo khóa qua `cache.RateLimiter` thật (ngưỡng thử 5/s, Valkey không nối) → yêu cầu 6 nhận 429 có `Retry-After`; khóa khác 200; chủ thể `svc:<ServiceKeyID>` | `TestServiceKeyRoute_RateLimitPerKey`, `TestServiceKeyRateLimit` (Task 188, đổi sang `ServiceKeyID`) | như trên | ok |
| Actor chống trùng `svc:<ServiceKeyID>`: cùng `Idempotency-Key` của khóa A và khóa B → hai dòng độc lập; A gửi lại → phát lại; nhân viên `user:<UserID>` | `TestServiceKeyRoute_IdempotencyActor`; test M0 trong `idempotency_test.go` vẫn xanh | như trên | ok |
| Tuyến tạo qua API tìm ra đúng tenant (`tenancy.Resolve`) | `TestServiceKeysAPI_RouteResolves` | như trên | ok |
| Bảng mã lỗi: năm sentinel mới (401, 401, 403 `ip_not_allowed`, 422 `permission_not_allowed_for_key`, 404); mã có trong `x-error-codes` và `console/src/app/errors.ts` | `TestDomainErrorSnapshot` (5 dòng mới), `TestErrorCodes_DeclaredInContract`, console `errors.test.ts` | ci-task | xem dòng ci-task |
| `CheckSchemaRules` vẫn `[]` (bảng mới RLS, unique, khóa ngoại ghép) | `TestSchemaRulesStillClean` | `go test -race ./internal/modules/identity/...` | ok |
| Lệnh `crm service-key`: production → từ chối; sai tham số → cách gọi; revoke đi qua tenant không có khóa, thu hồi ở đúng tenant bởi system (`revoked_by` NULL), phát báo hiệu thu hồi; mã lạ → lỗi; lần hai → `ErrVersionConflict` | `TestServiceKeyCommand_ArgsAndProduction`, `TestRevokeServiceKeyAnyTenant` | `go test -race ./cmd/crm/` | `ok …/cmd/crm` |
| `RATE_LIMITS_JSON` đọc ở api; sai → không dựng được (`cache.ErrBadLimits`); đúng → giới hạn theo khóa, `fallback_max` khi không có Valkey | `TestLoadRateLimitsJSON`, `TestNewWebKeys_RateLimits` | `go test -race ./internal/platform/config/ ./cmd/crm/` | ok |
| Hợp đồng thật: route ghi đều `required`, riêng `createServiceKey` `no-replay` kèm đúng lý do | `TestRealContractWritesRequired` | `go test -race ./tools/routegen/` | `ok …/tools/routegen` |
| Cổng tĩnh + toàn bộ test (đổi migration) | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check, `go test` toàn bộ | ci-task | lần 1 ĐỎ: `gen-check` (chưa sinh lại sau khi sửa một mô tả), `jscpd` (3 đoạn trùng mới → `openCommandDB`, `runVerifier`, `optTime` viết gọn), `tools/routegen` (`TestRealContractWritesRequired` chưa biết route no-replay); mọi gói khác xanh. Lần 2: `CI-TASK XANH (so với điểm tách nhánh 5fe8e27…)`; `ok …/internal/modules/identity 161.679s`, `ok …/internal/httpapi 114.232s`, `ok …/cmd/crm 3.273s`, `ok …/tools/routegen 1.870s`, `ok …/tests/arch 27.478s`, `ok …/tests/e2e 52.141s`. Lần 3 (sau sửa của reviewer lượt 1, chỉ đổi `openapi.yaml` và mã sinh): `CI-TASK XANH`; `ok …/internal/httpapi 55.715s`, `ok …/cmd/crm 3.620s`, `ok …/tests/e2e 49.301s` |

## Test bị xóa hoặc tắt

Không xóa, không tắt test nào. Sửa kỳ vọng của test cũ theo hành vi mới của kế hoạch:

- `tools/routegen/main_test.go` `TestRealContractWritesRequired`: thêm đúng một ngoại lệ `createServiceKey` và đòi nó
  `no-replay` kèm đúng lý do (luật openapi 3 cho phép thao tác trả bí mật một lần). Route ghi khác vẫn phải `required`.
- `internal/modules/identity/roles_test.go` `TestDefaultRoles_Content`: Quản lý không nhận `identity.service_keys.manage`
  (kế hoạch: chỉ owner) — thêm một điều kiện loại như `identity.users.manage`.
- `internal/modules/identity/permissions_test.go`: thêm dòng `identity.service_keys.manage` vào bảng kỳ vọng.
- `internal/httpapi/webroute_test.go` `serviceUser`, `TestServiceKeyRateLimit`: người gọi khóa dịch vụ giờ có `Kind`,
  `ServiceKeyID` (không dùng `UserID`); chủ thể giới hạn `svc:<ServiceKeyID>` (Task 188 đã giao việc này cho Task 189).

## Khác kế hoạch

- Xem Quyết định 1–8.
- `RevokeServiceKey` trả `(ServiceKey, error)` thay vì tiền tố: câu trả lời của `revokeServiceKey` cần cả khóa; tiền
  tố là `KeyPrefix`.
- Thêm `identity.SplitServiceKey`, `LoadServiceKey`, `CheckServiceKey`, `TouchServiceKeys`, `ServiceKeyRecord` (phần
  thuần và câu đọc mà bộ kiểm ở `httpapi` cần, Quyết định 3).
- Tạo khóa dùng `db.Savepoint` (luật ADR-46: không `Begin` ngoài gói `db`). `cmd/crm` thêm `openCommandDB` dùng chung
  cho hai lệnh vận hành (gỡ đoạn trùng jscpd với `recompute.go`).

## Việc phát hiện thêm

- (Reviewer lượt 1) Khóa giả đúng dạng (`ek_` + tiền tố lạ) không được đệm nên mỗi yêu cầu chạm `tenancy.Resolve` trước
  bước giới hạn theo khóa. Task sau của đường web (hoặc M5 bảo vệ quá tải) cân nhắc giới hạn theo IP (`cache.SubjectKey`
  "ip") trước bước xác thực khóa, hoặc đệm ngắn "tiền tố không có".
- Màn "Khóa cho website" (danh sách, tạo hiện khóa một lần, thu hồi có xác nhận — HV13 đã OK) là task màn hình sau;
  API đã có.

## Reviewer

Lượt 1 (agent `reviewer`, `git diff --cached`): **CẦN SỬA** — một phát hiện: lệnh `sed` xóa nhầm
`additionalProperties: false` ở 70 schema thân yêu cầu khác (máy chủ sẽ âm thầm nhận trường lạ). Đã sửa: khôi phục
đúng 70 dòng từ bản `HEAD`, giữ nguyên phần thêm của task, khai `additionalProperties: false` cho
`CreateServiceKeyRequest`, `RevokeServiceKeyRequest`, `make gen` (mã sinh không còn mất dòng `unexpected field` nào).
Các quyết định thay chủ shop 1–8 được đánh giá an toàn. Ghi chú không bắt buộc: khóa giả đúng dạng chạm database
(`tenancy.Resolve`) trước giới hạn theo khóa — ghi ở "Việc phát hiện thêm".

Lượt 2 (agent `reviewer`, trên `git diff --cached` sau khi sửa): **ĐẠT**. Không còn phát hiện phải sửa; `openapi.yaml`
chỉ còn dòng thêm (76 → 78 dòng `additionalProperties: false`), mã sinh không mất kiểm tra trường lạ nào. Ghi chú không
bắt buộc: `Deps.Realtime` nil thì thu hồi chỉ có hiệu lực sau hạn đệm 5 s (bản chạy thật luôn nối hub) — chấp nhận;
tên `KeyVerifier` và điều kiện của `TouchServiceKeys` trong tệp task đã sửa cho khớp mã.

Kết luận: ĐẠT
