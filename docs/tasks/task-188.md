# Task 188 — M5-05: Mở rộng hợp đồng và lớp HTTP: x-auth, x-admission, x-body-limit

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 188, M5-05). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `httpapi.Route` (sinh bởi `tools/routegen`) **thêm** ba trường `Auth string`, `Admission string`, `BodyLimit int64`; không đổi trường cũ. Chỗ dùng: `internal/httpapi/server.go` (`chain`), `idempotency.go`, các test dựng `Route{…}` bằng tay trong `internal/httpapi/*_test.go` (trường mới rỗng/0 → cư xử như cũ: `Auth` rỗng coi như `access`/`public` theo `Public`, `BodyLimit` 0 = 1 MiB), `tests/arch/routes_test.go`, `tests/arch/m2contract_test.go`. `routes_gen.go` sinh lại: mọi route có `Auth: "access"` hoặc `Auth: "public"`.
  - `httpapi.Deps` thêm `ServiceKeys ServiceKeyVerifier`, `RateLimiter WebRateLimiter`, `Admission map[string]func(http.Handler) http.Handler` (thêm trường, nil = an toàn: khóa dịch vụ 401, giới hạn theo bộ đếm trong tiến trình, route có admission chưa nối → 503). Chỗ dựng `Deps`: `cmd/crm/main.go`, `tests/e2e/stack_test.go`, `tests/drills/drill_test.go`, `internal/platform/realtime/env_test.go`, `tests/arch/routes_test.go` — không cần sửa (trường mới mặc định).
  - Kiểu mới xuất ra: `httpapi.ServiceKeyVerifier`, `httpapi.WebRateLimiter` (interface ở phía dùng, luật Go 1).
  - Ký hiệu không xuất trong `httpapi` đổi: `bodyLimit(limit, next)` (thêm tham số, một chỗ gọi `chain`); `userRateLimit` giữ chữ ký, thân chuyển sang `limitBy` dùng chung với `serviceRateLimit` mới; `auth` giữ chữ ký, đoạn cuối chuyển sang `withCaller` dùng chung với `serviceKeyAuth` mới (gộp để jscpd không báo trùng, cùng mục đích và cùng lý do đổi). Hằng mới `authAccess`, `authServiceKey`, `defaultServiceRateMax`; hàm mới `admission`, `serviceKeyAuth`, `serviceRateLimit`.
  - `NewServer`/`chain` đổi chuỗi middleware (xem dưới); với route hiện có (đều `access` hoặc `public`, không route nào có `x-body-limit`, `x-admission`) hành vi không đổi.
- Gói phụ thuộc ngược (go list): `internal/httpapi` được nhập bởi `cmd/crm`, `tests/arch`, `tests/e2e`, `tests/drills`, `internal/platform/realtime` (chỉ test). `tools/routegen` không ai nhập.
- Operation OpenAPI bị chạm, oasdiff: **không operation nào đổi**. Chỉ thêm `components`: `securitySchemes.ServiceKey` (`http`, `bearer`, không operation nào dùng), response chung `RetryLater` (503, header `Retry-After`, thân `Error`). oasdiff: chỉ thêm components, không phá vỡ.
- Chỗ dùng trên màn hình: không (client sinh `console/src/api` có thể đổi phần kiểu nếu openapi-typescript sinh components; không màn hình nào dùng).
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: `go test -race ./tools/routegen/... ./internal/httpapi/... ./tests/arch/...`; `scripts/agent/ci-task.sh` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gói giới hạn tần suất dùng chung; `platform/cache` chưa có trên main — Task 184/186 đang làm song song); trong `httpapi`: `userLimiter`/`RateLimit` (bộ đếm cửa sổ trượt theo `uuid`), `retryAfterSeconds`, `writeError`, `writeAuthError`, `origin`, `bodyLimit`, `idempotent`; trong `tests/arch`: `parseGoFiles`, `assertMentions`, `repoRoot`.
- Dùng lại / viết mới vì:
  - Giới hạn tần suất của khóa dịch vụ khi chưa nối `RateLimiter`: dùng lại `newUserLimiter` (một bộ đếm thứ hai, 600/phút = `FallbackMax` của `LimitWebKey` trong kế hoạch Task 186), không viết bộ đếm mới.
  - `bodyLimit` sửa để nhận giới hạn của route (thay vì hằng), không thêm middleware thứ hai.
  - Bước xác thực tách hai nhánh (`auth` giữ nguyên cho route access, `serviceKeyAuth` mới cho route service_key; đoạn cuối chung `withCaller`) — cùng chỗ đặt `authUserKey`, guard dùng chung không đổi.
  - Test kiến trúc mới `webexempt_test.go` dùng `parseGoFiles`, `assertMentions`.

## Soát thiết kế

- 5.1–5.7: không áp dụng: không có dữ liệu, bảng, transaction hay sự kiện mới; chỉ lớp HTTP và trình sinh bảng route.
- 5.8 Nghiệp vụ không biết HTTP: interface `ServiceKeyVerifier`, `WebRateLimiter` nằm ở `httpapi` (phía dùng); module không đổi.
- 5.9 Mọi đường ghi có khóa chống trùng và quyền: giữ nguyên luật M0 (`x-idempotency`); routegen thêm: route `service_key` phải có `x-permission` bắt đầu `web.` (không được `x-public`, `x-authenticated`); test kiến trúc kiểm lại trên bảng route. Route `service_key` khai `exempt` bị test `webexempt` quét AST để chắc không ghi.
- 5.10 Valkey không quyết định nghiệp vụ: `RateLimiter` chỉ điều tiết; khi không nối thì lùi về bộ đếm trong tiến trình (không bao giờ bỏ giới hạn).
- 5.11 Không thêm thư viện: chỉ thư viện chuẩn (`go/ast`, `net/netip`).
- 5.12 Theo mẫu: middleware theo mẫu M0 (`chain` một chỗ), lỗi đổi ở `errors*.go`.
- 6.1–6.11: không áp dụng: không đổi dữ liệu, tiền, tồn, hàng đợi. 6.2: thứ tự mới đặt cổng admission **trước** idempotency (yêu cầu bị cổng từ chối không tạo dòng `idempotency_keys`), có test.
- 6.12 Dữ liệu cá nhân: khóa dịch vụ (bí mật) không vào log: `serviceKeyAuth` không log header; lỗi đi `writeAuthError`.
- 6.13 Quyền: mặc định từ chối — route `service_key` chỉ đọc `Authorization: Bearer ek_…` và bỏ qua thẻ Access; route `access` bỏ qua `Authorization`. Chưa nối bộ kiểm khóa (`ServiceKeys` nil) → 401. Route có `x-admission` mà `Deps.Admission` thiếu tên → 503 (đóng, không cho qua). Kiểm `Origin` vẫn áp cho mọi route ghi trừ `service_key` (máy chủ website gọi máy chủ, không có trình duyệt).

## Quyết định thay chủ shop

1. **Interface đặt ở `httpapi`, không ở `identity`/`cache`.** Kế hoạch ghi `Deps.ServiceKeys identity.ServiceKeyVerifier` và `Deps.RateLimiter cache.RateLimiter`, nhưng cả hai gói/kiểu chưa có trên main (M5-06 làm sau task này; `platform/cache` đang được Task 184/186 làm song song, tạo gói ở đây sẽ xung đột). Luật Go 1 cũng đòi interface ở phía dùng. Chọn: `httpapi.ServiceKeyVerifier{Verify(ctx, bearer, clientIP netip.Addr) (identity.AuthUser, error)}` (đúng chữ ký kế hoạch) và `httpapi.WebRateLimiter{AllowWebKey(ctx, subject string) (allowed bool, retryAfter time.Duration)}`; M5-06 nối bản thật (`identity.NewServiceKeyVerifier` tự khớp interface; một bộ chuyển vài dòng gọi `cache.RateLimiter.Allow(ctx, cache.LimitWebKey, subject)`). Phương án khác: tạo trước gói `cache` rỗng / thêm interface vào `identity` (xung đột với task song song, mở rộng vùng rủi ro cao). Dễ đảo ngược: đổi kiểu một trường.
2. **Ánh xạ `cache.ErrBusy` → 503 `retry_later` chưa làm** vì `cache.ErrBusy` là của Task 185 (chưa có). Mã `retry_later` và response `RetryLater` của hợp đồng đã có; Task 185 (hoặc task đầu tiên nhập `cache` vào `httpapi`) thêm một dòng vào `errors.go`. Ghi ở "Việc phát hiện thêm".
3. **Không tạo middleware `ipAllow` riêng**: `AuthUser` chưa mang danh sách CIDR; kế hoạch M5-06 cho `Verify` nhận `clientIP` và trả `ErrIPNotAllowed`. `serviceKeyAuth` đọc IP từ `Cf-Connecting-Ip` (sai/thiếu → địa chỉ rỗng, bộ kiểm quyết) và truyền vào `Verify`. M5-06 thêm ánh xạ `ErrIPNotAllowed` → 403.
4. **Kiểm `Origin` áp cho mọi route ghi trừ `service_key`** (kế hoạch ghi "Auth=access && Write"): route công khai ghi (đăng nhập M7) vẫn qua kiểm origin như M0 — chặt hơn, giữ nguyên test M0 `TestOriginRejectsWrites` (route công khai).
5. **Không thêm schema `ErrorBody` trùng với `Error`**: hợp đồng đã có schema `Error` chính là thân lỗi; response `RetryLater` dùng `Error`. Thêm `ErrorBody` cùng nội dung sẽ là hai nguồn cho một thứ.
6. **Chủ thể giới hạn tần suất của khóa dịch vụ là `"svc:" + AuthUser.UserID`** cho tới khi M5-06 thêm `AuthUser.ServiceKeyID` (bộ kiểm của M5-06 đổi sang `ServiceKeyID`; ghi ở "Việc phát hiện thêm").
7. **`x-admission` là khung**: routegen đọc và kiểm khóa, `chain` gắn `Deps.Admission[tên]`; bộ điều tiết thật (`AdmissionGate`) là Task 187 (đợt 2, flash sale). Tên admission không có trong `Deps.Admission` → 503 `retry_later` (đóng).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| routegen: `/api/v1/web/x` không khai `x-auth` → lỗi nêu `operationId` (cùng: `x-auth` ngoài path web, giá trị lạ, `x-public` + `service_key`, quyền không `web.*`) | `TestRejectsBadContracts/web-path-access.yaml` (stderr nêu `webNoAuth`, `thingsServiceKey`, `webAuthAccess`, `webPublic`, `webStaffPermission`; `webOk` không bị báo) | `go test -race ./tools/routegen/` | `ok elimaz.com.vn/crm/tools/routegen` |
| routegen: `x-admission` trên route access / exempt / đọc / tên sai → lỗi | `TestRejectsBadContracts/admission-on-access.yaml` (`admissionOnAccess`, `admissionOnExempt`, `admissionOnRead`, `admissionBadName`; `admissionOk` không bị báo) | như trên | ok |
| routegen: `x-body-limit` 16 MiB → lỗi (cùng 1023 byte, chuỗi) | `TestRejectsBadContracts/body-limit-too-big.yaml` (`bodyTooBig`, `bodyTooSmall`, `bodyNotInt`; `bodyOk` 8 MiB không bị báo) | như trên | ok |
| routegen sinh `Auth`, `Admission`, `BodyLimit` | `TestGeneratesWebRoutes` (`web-ok.yaml`), `TestGeneratesRoutes`, `TestGeneratesIdempotencyModes` | như trên | ok |
| `openapi.yaml` thật sinh không lỗi, chạy lại không đổi tệp | `TestRealContractMatchesCommittedRoutes`, `TestRealContractWritesRequired`; `make gen` trong ci-task (gen-check) | ci-task | xem dòng ci-task |
| service_key: thẻ Access hợp lệ, không khóa → 401; Authorization sai dạng không tới bộ kiểm; khóa sai 401; khóa đúng 200 và bộ kiểm nhận IP `cf-connecting-ip`; access: chỉ có `Bearer ek_x_y` → 401, không gọi bộ kiểm khóa | `TestServiceKeyAuthSeparatedFromAccess` | `go test -race ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi` (ci-task) |
| service_key ghi không `Origin` → tới bước auth (401), access ghi không `Origin` → 403 `bad_origin` | `TestOriginOnlyForNonServiceWrites`; M0 `TestOriginRejectsWrites` vẫn xanh | như trên | ok |
| service_key `exempt` không `Idempotency-Key` → 200; `required` → 400 `idempotency_key_required` | `TestServiceKeyIdempotencyModes` | như trên | ok |
| `x-body-limit: 6291456` nhận 2 MiB → qua (cả không khai độ dài); 7 MiB → 413; mặc định 2 MiB → 413, đúng 1 MiB → qua | `TestBodyLimitPerRoute` | như trên | ok |
| `Deps.Admission["flash"]` trả 503 → 503 trước chống trùng, 0 dòng `idempotency_keys` (PostgreSQL thật); cổng cho qua: cổng chưa thấy khóa, handler thấy khóa; tên chưa nối → 503 có `Retry-After` | `TestAdmissionBeforeIdempotency` | như trên | ok |
| Giới hạn tần suất khóa dịch vụ: `RateLimiter` từ chối → 429, `Retry-After: 2`, chủ thể `svc:<mã>`; chưa nối → 600/phút rồi 429; bộ đếm tách nhân viên | `TestServiceKeyRateLimit` | như trên | ok |
| Lỗi lạ của bộ kiểm → 500, log không có khóa | `TestServiceKeyVerifierErrorNotLogged` | như trên | ok |
| `webexempt_test`: tệp mẫu handler exempt gọi `WriteTx` (qua hàm phụ) / `RecordAudit` (truyền làm giá trị) → báo tên operation; handler không thấy → báo; mã thật → 0 | `TestWebExemptSample`, `TestWebExemptRealCode` | `go test -race ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch` (ci-task) |
| `routes_test` M0 vẫn đạt; mọi route service_key có quyền `web.*` (cùng luật Auth/Admission/BodyLimit; bảng giả 7 ca sai, 4 ca đúng) | `TestRoutesPublicSetAndPermissions`, `TestRoutesAllServed`, `TestRoutesIdempotencyModes`, `TestRoutesAuthRules` | như trên | ok |
| Cổng tĩnh + test gói đổi và phụ thuộc | build, vet, lint, gen-check (sinh lại không chênh), catalog, jscpd, gitleaks, oasdiff (chỉ thêm components: không báo phá vỡ), test-guard; test `cmd/crm`, `internal/httpapi`, `internal/platform/realtime`, `tests/arch`, `tests/e2e`, `tools/routegen` | ci-task (lần 1: test xanh hết, đỏ gen-check vì chưa stage và jscpd 3 đoạn trùng → gộp `limitBy`, `withCaller`, `keyDeps`; lần 2 sau khi stage) | `CI-TASK XANH (so với điểm tách nhánh d5cb112…)`; lần 1: `ok …/tools/routegen 3.036s`, `ok …/internal/httpapi 61.424s`, `ok …/tests/arch 34.927s`; lần 2: `ok …/internal/httpapi 56.845s`, `ok …/tests/arch 26.881s`, `ok …/tests/e2e 49.290s` |
| Test trước code: test mới của httpapi không biên dịch trước khi có `Deps.ServiceKeys`, `Route.Auth`…; routegen đỏ trước khi sửa `main.go` (ba tệp mới thoát 0, chuỗi sinh thiếu `Auth`) | — | `go test ./tools/routegen/` | `FAIL elimaz.com.vn/crm/tools/routegen` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Xem "Quyết định thay chủ shop" 1, 2, 3, 4, 5.

## Việc phát hiện thêm

- Task 185 (M5-02) hoặc task đầu tiên nhập `platform/cache` vào `httpapi`: thêm dòng `cache.ErrBusy` → 503 `retry_later` + `Retry-After: 1` ở `internal/httpapi/errors.go` (kế hoạch giao cho M5-05, nhưng sentinel chưa tồn tại).
- Task 189 (M5-06): nối `Deps.ServiceKeys`, `Deps.RateLimiter` (bộ chuyển từ `cache.RateLimiter` + `cache.LimitWebKey`), đổi chủ thể giới hạn tần suất và actor chống trùng sang `ServiceKeyID`, thêm ánh xạ `ErrInvalidKey`/`ErrRevoked` (401), `ErrIPNotAllowed` (403).

## Reviewer

Kết luận: ĐẠT (lượt 1, agent `reviewer`, trên `git diff --cached` 18 tệp).

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến khớp thực tế; `bodyLimit` không còn chỗ gọi chữ ký cũ; test ở tệp mới `webroute_test.go` thay vì `middleware_test.go` (chấp nhận).
- Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng điều tiêu chí nói; `TestRealContractWritesRequired` bị siết thêm (hậu tố `Auth: "access"`), không nới.
- Quyết định thay chủ shop 1, 3, 4, 5, 7 được đánh giá an toàn (bỏ kiểm Origin cho service_key không có rủi ro CSRF vì trình duyệt không tự gửi `Authorization`; thiếu cổng/bộ kiểm đều đóng).
- Ghi chú không bắt buộc: (a) Task 189 phải đổi chủ thể giới hạn sang `ServiceKeyID` (đã ở "Việc phát hiện thêm"); (b) tên `accessAuth` ở mục Tái dùng lệch mã — đã sửa chữ; (c) test tách bộ đếm chỉ so con trỏ — để nguyên.
