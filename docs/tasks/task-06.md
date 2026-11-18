# Task 06 — Hợp đồng OpenAPI, bảng route sinh tự động, lớp HTTP

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md` (Task M0-06). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu có sẵn nào đổi chữ ký. Thêm một phương thức
  `(*observability.Health).Check(ctx) error` (phần kiểm database của `/health/ready`, để lớp HTTP mới dùng lại; hàm
  `ready` cũ gọi nó, hành vi không đổi). `observability.NewHealth`/`Register` có 1 nơi dùng (`cmd/crm/main.go`), giữ
  nguyên cho vai trò `ingest`. Ký hiệu mới: gói `internal/httpapi` (`NewServer`, `Deps`, `Route`, `Routes`,
  `RequestID`), gói sinh `internal/httpapi/gen` (ogen), lệnh `tools/routegen`, `tools/apicheck`. Thêm sau khi jscpd báo
  trùng: `testkit.OpenAppDB` (chỉ test mới của task dùng; `testkit` được test của `db`, `tenancy`, `tests/*` nhập, chỉ thêm hàm).
- Gói phụ thuộc ngược (go list): `observability` được `cmd/crm` nhập. `internal/httpapi` mới, chỉ `cmd/crm` và
  `tests/arch` nhập. `cmd/crm`: vai trò `api` chuyển từ mux riêng sang `httpapi.NewServer` (ingest giữ mux health cũ
  tới Task 14); `tests/e2e` (khởi động ba vai trò, `/health/ready`, healthcheck) phải vẫn xanh.
- Operation OpenAPI bị chạm, oasdiff: tạo mới `openapi/openapi.yaml` với 9 operation của M0 (`healthLive`,
  `healthReady`, `healthDeep`, `getMe`, `stream`, `listJobs`, `retryJob`, `abandonJob`, `getPlatformHealth`).
  `main` chưa có tệp nên oasdiff lần đầu bỏ qua. Từ khi gộp, mọi đổi phá vỡ cần dòng trong `breaking-approved.yaml`.
- Chỗ dùng trên màn hình: chưa có màn hình (Task 17 sinh client từ tệp này).
- Câu sqlc, bảng, pha migration: không (không migration, không câu SQL mới).
- Test sẽ chạy: `go test -race ./tools/routegen/... ./internal/httpapi/... ./internal/platform/observability/...
  ./tests/arch/... ./tests/e2e/... ./cmd/...` trên PostgreSQL của dự án compose `crm-t06` (cổng 5506); thử
  `make oasdiff` với tệp mẫu; cuối cùng `make ci` phần đã có. Không chạm vùng rủi ro cao (`guard.go` thuộc Task 7).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có `config`
  (`ConsoleOrigins`), `db`, `logging`, `metrics.Middleware` (đo `http_ms` theo `r.Pattern`), `observability.Health`,
  `telemetry.Setup`. Tìm `request_id`, `MaxBytes`, `recover`, `Origin`, `ServeMux`, `yaml` trong `internal/`,
  `cmd/`, `tools/`: chưa có middleware HTTP nào ngoài `metrics.Middleware`; chưa có bộ đọc YAML.
- Dùng lại / viết mới vì:
  - `metrics.Middleware` bọc ngoài `NewServer` như cũ (nhãn theo mẫu route của ServeMux mới); `telemetry.Setup` đặt
    provider toàn cục cho `otelhttp`.
  - Kiểm database của `/health/ready`: dùng lại `observability.Health` qua phương thức mới `Check` (cùng hạn 1 s,
    cùng lý do đổi), không chép lại.
  - `tools/routegen` đọc hợp đồng bằng `ogen.Parse` (gói `github.com/ogen-go/ogen`, đã có trong ADR-44, giữ cả
    phần mở rộng `x-…`), không thêm thư viện YAML.
  - `otelhttp` v0.72.0 có trong ADR-44 (dòng "Dấu vết"), lần đầu vào `go.mod`; không cần dòng `docs/phu-thuoc.md`.
    ogen kéo theo `go-faster/jx`, `go-faster/errors` (mã sinh nhập thẳng nên thành trực tiếp): đã thêm một dòng
    `docs/phu-thuoc.md` theo ghi chú của reviewer.
  - Mã `request_id`: `uuid.NewString` (google/uuid đã có); middleware, `origin`, lỗi viết mới trong `httpapi`
    (chỉ lớp HTTP dùng).
  - `boundaries_test` dùng lại `repoRoot` của `tests/arch`; đọc `go list -json` (lệnh chuẩn), không thêm công cụ.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: không áp dụng: không có dữ liệu mới.
2. Không ghi bảng module khác: không áp dụng: không ghi bảng nào.
3. Đọc qua view: không áp dụng.
4. Việc nguyên tử: không áp dụng: chưa có đường ghi chạy thật (các handler ghi là Task 12).
5. Việc sau commit: không áp dụng.
6. Gọi ra ngoài: không áp dụng.
7. Ngữ cảnh shop: không áp dụng ở task này (handler chạy `WithTenant` từ Task 7, 12); lớp HTTP không truy vấn bảng
   có `tenant_id`.
8. Nghiệp vụ không biết HTTP: đây là task dựng luật này: `boundaries_test` (b) chặn `internal/modules/**` nhập
   `net/http`, `internal/httpapi/**`, mã ogen.
9. Đường ghi có khóa chống trùng và khai quyền: `routegen` từ chối operation thiếu `x-permission`/`x-public` hoặc
   operation ghi thiếu `IdempotencyKey`; `routes_test` chỉ cho ba route health công khai. Middleware chống trùng là
   Task 9; tới lúc đó mọi route không công khai bị từ chối 401 (mặc định từ chối), nên không đường ghi nào chạy
   được khi chưa có khóa.
10. PostgreSQL là nguồn sự thật: không áp dụng.
11. Thư viện: ogen v1.25.0, `otelhttp` v0.72.0 (đều trong ADR-44); công cụ oasdiff v1.38.0 đã có dòng ở
    `docs/phu-thuoc.md`.
12. Module mẫu: không áp dụng: chưa có (Task 7).

Mục 6 (đúng khi chạy đồng thời):

1–5. Không áp dụng: không có đường ghi, không transaction.
6–8. Không áp dụng: không việc nền, không gọi ra ngoài. Handler panic: `recover` trả 500 và tiến trình chạy tiếp.
9. Treo lâu: thân yêu cầu tối đa 1 MiB (`Content-Length` lớn hơn bị từ chối ngay, thân không khai độ dài bị cắt
   khi đọc); `ReadHeaderTimeout` 10 s giữ như cũ; `/health/ready` hỏi database tối đa 1 s.
10–11. Không áp dụng: không có tiền, không vùng rủi ro cao.
12. Dữ liệu cá nhân: body lỗi chỉ có `{"error","request_id"}`, không vết lỗi; log của panic chỉ có kiểu giá trị
    panic, route và `request_id`, không có giá trị panic hay thân yêu cầu (có thể chứa SĐT). `x-request-id` gửi
    lên chỉ được nhận khi khớp `^[A-Za-z0-9_-]{1,128}$` (không chèn được dòng log lạ).
13. Quyền: mặc định từ chối: route không công khai trả 401 cho tới khi Task 7 thay bằng `auth` + `guard`; route
    ngoài bảng `Routes` → 404; method ghi phải có `Origin` thuộc `CONSOLE_ORIGINS` và `content-type`
    `application/json` (chặn CSRF từ trang khác).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đỏ trước khi có mã | cả bốn gói | go test ./tools/routegen/ ./tools/apicheck/ ./internal/httpapi/ ./tests/arch/ | `FAIL … [build failed]` cả bốn (chưa có `run`, `NewServer`, `Deps`, `chain`) |
| routegen: thiếu quyền → lỗi nêu operationId; ghi thiếu khóa → lỗi; cả hai dấu, `x-public: false`, quyền rỗng, thiếu operationId → lỗi; không ghi tệp | TestRejectsBadContracts (4 ca) | go test -race -count=1 -v ./tools/routegen/ | `--- PASS`; đột biến bỏ luật IdempotencyKey → `FAIL` (đã hoàn lại) |
| routegen: sinh đúng Route (Public, Write, Stream; khóa ở mức đường dẫn được tính), thứ tự cố định | TestGeneratesRoutes | như trên | `--- PASS` |
| openapi.yaml thật sinh không lỗi, chạy lại không đổi tệp | TestRealContractMatchesCommittedRoutes; `make gen` rồi `git status` | như trên; make gen | `--- PASS`; `make gen` không đổi tệp đã có |
| Route công khai đúng {GET /health/live, /ready, /deep}; mọi route khác có quyền thuộc danh sách quyền | TestRoutesPublicSetAndPermissions | go test -race -count=1 -v ./tests/arch/ | `--- PASS` (danh sách quyền chép từ Interfaces Task 7, xem Khác kế hoạch) |
| Số route đăng ký trong NewServer = len(Routes); mọi route được phục vụ; đường ngoài bảng → 404 | TestServerRegistersEveryRoute, TestRoutesAllServed, TestServerRouting | go test -race -count=1 -v ./internal/httpapi/ ./tests/arch/ | `--- PASS` (`newMux` đếm số lần gọi `mux.Handle`: 9 = 9) |
| POST thiếu Origin / Origin lạ / Origin gần giống / text/plain / thiếu content-type → 403 bad_origin; GET không cần Origin | TestOriginRejectsWrites (7 ca), TestOriginSkipsReads | go test -race -count=1 -v ./internal/httpapi/ | `--- PASS`; đột biến bỏ kiểm Origin → `mong 403, có 200` (đã hoàn lại) |
| Thân 1 MiB + 1 byte → 413 (có Content-Length và không khai độ dài); đúng 1 MiB qua | TestBodyLimit, TestServerBodyTooLarge | như trên | `--- PASS` |
| Panic → 500 {"error":"internal","request_id"} bằng x-request-id gửi lên; body và log không có giá trị panic, không vết lỗi | TestPanicReturns500WithoutTrace | như trên | `--- PASS` |
| x-request-id sai mẫu → mã mới; header = ctx | TestRequestIDGeneratedWhenInvalid | như trên | `--- PASS` |
| Mặc định từ chối: route không công khai (GET, POST) → 401 unauthenticated tới khi có auth | TestServerRouting | như trên | `--- PASS`; đột biến bỏ `denyUntilAuth` → `FAIL` (đã hoàn lại) |
| /health/ready: database đóng → 503 unavailable; mọi câu trả lời Cache-Control: no-store | TestHealthReadyDown, TestServerRouting | như trên | `--- PASS` |
| boundaries_test: testdata → đúng 5 lỗi (a–e); mã thật → 0; từng luật (gọi module không có trong cột "Được gọi", platform nhập phần riêng identity…) | TestBoundariesTestdata, TestBoundariesNotCalledModule (12 ca), TestBoundariesRealCode | go test -race -count=1 -v ./tests/arch/ | `--- PASS` cả ba |
| oasdiff: xóa trường bắt buộc → đỏ, nêu getMe; có dòng duyệt → xanh; dòng duyệt cho operation khác / thiếu ngày → đỏ; thêm trường tùy chọn → xanh; main chưa có tệp → bỏ qua; nhánh không có → đỏ | TestBreakingChanges (6 ca), TestMissingBaseSkips, TestMissingRefFails, TestBaseFromRef, TestParseApprovedRejectsIncomplete | go test -race -count=1 -v ./tools/apicheck/; make oasdiff OASDIFF_BASE=-base=tools/apicheck/testdata/base.yaml OASDIFF_REV=…/removed-required.yaml [OASDIFF_APPROVED=…/approved-email.yaml] | `--- PASS`; make: `CHƯA DUYỆT: getMe GET /api/v1/me (response-required-property-removed)` thoát 2; có dòng duyệt `đã duyệt: getMe…` thoát 0; added-optional thoát 0; `make oasdiff` thật: `main chưa có openapi/openapi.yaml, bỏ qua (lần đầu)` |
| api khởi động với lớp HTTP mới, /health/ready, healthcheck, tắt êm | tests/e2e (có sẵn) | go test -race -count=1 ./... | `ok elimaz.com.vn/crm/tests/e2e` |
| Không vỡ gói khác; cổng kiểm | mọi gói; build, vet, lint, jscpd, govulncheck, gitleaks, sqlc vet, test-guard | go test -race -count=1 ./...; CGO_ENABLED=0 go build -trimpath ./...; go vet ./...; golangci-lint run ./...; make jscpd sqlc-vet test-guard; govulncheck ./...; gitleaks dir | mọi gói `ok`; `0 issues.`; `Found 0 clones.`; `No vulnerabilities found.`; `no leaks found`; test-guard thoát 0. gen-check, catalog-check so với git nên chỉ xanh sau commit |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- **Chưa có `auth`, `guard`, `userRateLimit`, `idempotency` trong chuỗi** (Task 7, 9 làm). Thay chỗ của chúng là
  `denyUntilAuth`: mọi route không công khai trả 401. `Deps` chưa có `Access`, `Realtime` (kiểu của Task 7, 15).
- **`routes_test` so quyền với danh sách chép trong test** (`knownPermissions`, đúng sáu quyền ở Interfaces của Task 7),
  vì `identity.Permissions` chưa có. Task 7 thay bằng `identity.Permissions` và xóa bản chép.
- `GET /api/v1/stream` tạm đi tới handler ogen (501) như mọi operation chưa làm; Task 15 thay bằng handler SSE.
  `GET /health/deep` (public) tạm 501 tới Task 20.
- Lệnh kiểm oasdiff là Go (`tools/apicheck`, có test) thay vì lệnh shell trong Makefile: cần đọc JSON của oasdiff và
  tệp duyệt. Dạng dòng duyệt cố định một dòng: `- {operationId: …, reason: "…", approvedOn: YYYY-MM-DD}`.
  `make ci` gọi `oasdiff`.
- Thêm: `openapi/ogen.yml` (cấu hình ogen), `(*observability.Health).Check`, `testkit.OpenAppDB` (gộp đoạn trùng jscpd
  báo giữa hai test mới), header `Cache-Control: no-store` cho mọi câu trả lời của API (giữ hành vi cũ của health).
- Vai trò `api` trong `cmd/crm/main.go` chuyển sang `httpapi.NewServer`; `ingest` giữ mux health cũ tới Task 14.
- `make gen` chạy `ogen` (v1.25.0) và `go run ./tools/routegen`; `gen-check` đòi không có tệp sinh mới chưa commit
  trong `internal/httpapi/gen`.

## Quyết định thay chủ shop

1. **Mặc định từ chối khi chưa có auth:** route không công khai trả 401 `unauthenticated` (phương án khác: để route
   chạy không xác thực tới Task 7, không an toàn; hoặc chưa đăng ký route, trái tiêu chí "số route = len(Routes)").
   Đảo ngược dễ: Task 7 thay một hàm.
2. **Hợp đồng khai sẵn hình dạng M0** theo Interfaces của Task 7, 12, 19, 20 (`Me`, `Job`, `PlatformHealth`,
   `DeepHealth`): trường trả về nào chưa chắc thì để tùy chọn, vì sau khi gộp main, thêm trường là thay đổi không phá
   vỡ còn bớt hay đổi kiểu thì cần dòng duyệt. `listJobs` trả mảng `Job`; `retryJob`/`abandonJob` trả `Job`.
3. **Thay đổi phá vỡ = mọi dòng của `oasdiff breaking`, cả mức WARN** (ví dụ bỏ trường tùy chọn khỏi câu trả lời), chặt
   hơn mức ERR mặc định của oasdiff; nhánh gốc không tồn tại là lỗi (không lặng lẽ bỏ qua), chỉ bỏ qua khi nhánh có mà
   chưa có tệp.
4. **ogen chỉ sinh phần máy chủ**, tắt `ogen/otel` (dấu vết do `otelhttp` trong chuỗi, mỗi yêu cầu một span) và tắt
   "convenient errors" (mọi lỗi đi một đường `writeHandlerError` trong `errors.go`). Đường dẫn khớp ServeMux nhưng sai
   method → 404 như đường lạ.
5. **Log của panic** có `request_id`, `operation_id`, kiểu giá trị panic và stack; **không** có giá trị panic (có thể
   chứa SĐT). Task 20 thêm `RecordError` có lọc.
6. **routegen đọc hợp đồng bằng `ogen.Parse`** (có sẵn trong ADR-44), không thêm thư viện YAML; `x-public` chỉ nhận
   `true` kiểu boolean, `x-permission` phải là chuỗi không rỗng.
7. **boundaries_test (a)** đọc cột "Được gọi" của 04 mục 3 là danh sách module mà module đó được nhập; module chưa có
   trong bảng không được nhập module nào. Luật (b) chặn thêm `github.com/ogen-go/ogen` và `net/http/*` (CLAUDE.md
   "mã ogen").

## Việc phát hiện thêm

- **Mâu thuẫn cho Task 7:** Interfaces của Task 7 đặt `AccessOptions.HTTPClient *http.Client` trong
  `internal/modules/identity`, nhưng luật (b) (spec ADR-44 điểm 4, 04 mục 3) cấm module nghiệp vụ nhập `net/http`.
  Task 7 cần chọn: đặt phần tải khóa Cloudflare Access trong `internal/platform` (hoặc `httpapi`) và truyền vào
  identity một hàm lấy khóa, hoặc xin chủ shop ngoại lệ. Không nới luật ở Task 6.
- Task 7: thay `knownPermissions()` của `tests/arch/routes_test.go` bằng `identity.Permissions`; thay `denyUntilAuth`
  bằng `auth` + `guard` + `userRateLimit`.
- Nhiều test cũ (`db`, `tenancy`, `metrics`) tự mở pool bằng `StartPostgres` + `db.Open`; có thể chuyển sang
  `testkit.OpenAppDB` khi chạm tới (không làm ở đây vì ngoài phạm vi).
- Nhãn chỉ số `http_ms` của đường lạ ở `api` giờ là `GET /` (mẫu bắt mọi đường của 404) thay vì `GET unmatched`.

## Reviewer

Lần 1 (agent reviewer, trên `git diff --cached`): **ĐẠT**, không phát hiện phải sửa. Ghi chú không bắt buộc và cách xử
lý:

1. go-faster/jx, go-faster/errors thành phụ thuộc trực tiếp mà chưa có dòng `docs/phu-thuoc.md` → **đã thêm** một dòng
   (như tiền lệ google/uuid).
2. `TestServerRegistersEveryRoute` không kiểm gì vì `newMux` trả thẳng `len(Routes)` → **đã sửa**: đếm số lần gọi
   `mux.Handle`.
3. `writeHandlerError` ghi `err.Error()` vào log: giữ; từ Task 7, 12 chuỗi lỗi của module không được mang dữ liệu cá
   nhân (luật CLAUDE.md "Lỗi và log"); Task 20 thêm lọc.
4. Dòng duyệt theo `operationId` cho qua mọi thay đổi phá vỡ sau của cùng operation → giữ (đúng chữ kế hoạch); ghi
   chú đầu `breaking-approved.yaml` đã nói dọn dòng sau khi gộp.
5. `fromRef` coi mọi lỗi `git show` là "tệp chưa có" → giữ: nhánh đã được kiểm tồn tại trước.
6. `TestBaseFromRef` qua cả khi máy thiếu oasdiff → **sửa một phần**: đòi đầu ra nêu `oasdiff breaking` (chứng tỏ đã
   lấy tệp từ HEAD và gọi oasdiff), nhưng lỗi "thiếu oasdiff" cũng mang chuỗi này. Giữ vậy: `TestBreakingChanges`
   cần oasdiff chạy thật mới qua, nên máy thiếu công cụ vẫn đỏ.

Lần 2 (sau các sửa trên, soát lại toàn bộ diff đã stage): **ĐẠT**, không phát hiện phải sửa. Hai ghi chú câu chữ
(mục Tái dùng về phụ thuộc go-faster, cách ghi ghi chú 6) đã chỉnh trong tệp này.

Kết luận: ĐẠT
