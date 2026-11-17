# Task 03 — Khung ba vai trò, cấu hình, log che sâu, OpenTelemetry, health, test khởi động

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

Viết trước khi sửa (18/11/2026). Đây là mã Go đầu tiên của module gốc `elimaz.com.vn/crm`: `go list ./...` trả
"matched no packages"; module riêng `spikes/flashsale` không nhập gì từ module gốc.

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có ký hiệu cũ nào bị đổi, 0 chỗ tham chiếu. Ký hiệu
  mới: `config.{Role, RoleIngest, RoleAPI, RoleWorker, RoleMigrate, ParseRole, Config, MigrateConfig, Load, FromOS,
  ErrInvalid}`; `logging.{SensitiveKey, Redact, NewLogger, DailyFile, NewDailyFile, Phone, Address, Secret}`;
  `telemetry.{Setup, Option, WithSyncExporter}`; `observability.{Pinger, Health, NewHealth}`.
  Thêm so với dự kiến (ghi sau khi code): `logging.{DefaultDepth, DefaultKeep, DefaultMaxBytes, DailyFile.Check, DailyFile.Dropped}`,
  `telemetry.ErrSampler`, `observability.Health.Register`; tệp `.gitleaks.toml`; `golang.org/x/net` nâng lên v0.60.0.
- Gói phụ thuộc ngược (go list): chưa có gói nào; sau task: `cmd/crm` nhập `config`, `logging`, `telemetry`,
  `observability`; `logging` và `telemetry` nhập `config`.
- Operation OpenAPI bị chạm, oasdiff: không (chưa có `openapi.yaml`; `/health/live`, `/health/ready` vào hợp đồng ở
  Task 6).
- Chỗ dùng trên màn hình: không (chưa có `console/`).
- Câu sqlc, bảng, pha migration: không. `/health/ready` và nhịp sống của `worker` chỉ gọi `Ping` của pool pgx, không
  câu SQL, không bảng.
- Phụ thuộc mới trong `go.mod` (đều thuộc ADR-44): `github.com/jackc/pgx/v5` v5.13.0, `go.opentelemetry.io/otel`
  v1.47.0 (`sdk`, `trace`, `exporters/otlp/otlptrace/otlptracehttp`). Công cụ: golangci-lint v2.15.0, govulncheck
  v1.9.0, jscpd 5.4.3 (đã ghim trong `tools/versions.mk`, nay cài trên máy).
- Tệp ngoài mã Go: `Makefile` (thêm `ci`, `lint`, `catalog`), `.golangci.yml`, `.jscpd.json`,
  `jscpd-baseline.json`, `docs/danh-muc-dung-chung.md`, `docs/danh-muc-ghi-chu.yaml`, `docs/phu-thuoc.md`.
- Test sẽ chạy: mọi gói mới (`go test -race ./...` của module gốc, gồm `tests/e2e` cần PostgreSQL dev của phiên này
  qua `CRM_TEST_DATABASE_URL`), `tests/arch` (chạy golangci-lint thật), `tools/catalog`; rồi `make ci` phần đã có.
  Không chạm vùng rủi ro cao.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chưa có (task này tạo
  nó); module gốc chưa có gói nào. `spikes/flashsale` có mã nối pgx nhưng là module đo riêng, không vào sản phẩm
  (kế hoạch), không nhập lại.
- Dùng lại / viết mới vì:
  - Log: `log/slog` chuẩn (`slog.NewJSONHandler`) bọc bởi một handler che sâu; không thư viện log ngoài.
  - Xoay tệp log theo ngày: viết mới `DailyFile` (khoảng 100 dòng) vì thư viện chuẩn không có; thư viện xoay tệp
    (lumberjack…) không có trong ADR-44 và xoay theo kích thước chứ không theo ngày.
  - Dấu vết: SDK OpenTelemetry và exporter OTLP HTTP (ADR-44); `tracetest.InMemoryExporter` cho test.
  - Health: `net/http` chuẩn, `http.ServeMux`.
  - Danh mục: `go/doc`, `go/parser`, `go/printer` chuẩn (cùng nguồn dữ liệu mà `go doc` dùng). Tệp ghi chú đọc
    bằng bộ đọc tự viết cho một tập con của YAML (`đường dẫn gói: câu`), vì không có thư viện YAML trong ADR-44.
  - `github.com/google/uuid`: thêm dòng vào `docs/phu-thuoc.md` theo kế hoạch. Mã CRM chưa nhập trực tiếp; nó đã có
    trong `go.mod` dạng `// indirect` v1.6.0 vì OpenTelemetry kéo vào (reviewer chỉ ra).

## Soát thiết kế

Mục 5 (khớp kiến trúc):

- 5.1 Đúng chủ dữ liệu: không áp dụng: task không tạo dữ liệu hay bảng.
- 5.2 Không ghi bảng module khác: không áp dụng: không ghi bảng nào.
- 5.3 Đọc qua view: không áp dụng: không đọc bảng nào.
- 5.4 Việc nguyên tử trong một transaction: không áp dụng: không transaction (Task 4 làm `db.RetryTx`).
- 5.5 Việc không nguyên tử sau commit: không áp dụng: không có.
- 5.6 Gọi ra ngoài qua lệnh `in_doubt`: không áp dụng: chỉ xuất dấu vết OTLP (không phải lệnh nghiệp vụ) và
  `crm healthcheck` gọi chính tiến trình của nó.
- 5.7 Ngữ cảnh shop: không áp dụng: không truy vấn bảng có `tenant_id`.
- 5.8 Nghiệp vụ không biết HTTP: đúng: chưa có module nghiệp vụ; HTTP chỉ ở `observability/health.go` (nền tảng) và
  `cmd/crm`.
- 5.9 Đường ghi có khóa chống trùng: không áp dụng: chỉ có `GET /health/*`.
- 5.10 PostgreSQL là nguồn sự thật: đúng: không có trạng thái nào ngoài PostgreSQL ngoài tệp log.
- 5.11 Không thêm công nghệ ngoài ADR-44: pgx, OpenTelemetry thuộc ADR-44; golangci-lint, govulncheck có trong
  Tech Stack; jscpd đã có dòng trong `docs/phu-thuoc.md`; thêm dòng `github.com/google/uuid` theo kế hoạch.
- 5.12 Theo module mẫu: không áp dụng: mẫu có từ Task 7, 12.

Mục 6 (đúng khi chạy đồng thời):

- 6.1 Hai yêu cầu cùng sửa: `DailyFile.Write` được gọi đồng thời từ nhiều goroutine (slog gọi `Write` song song) →
  một `sync.Mutex` giữ tệp, ngày, số byte; test `-race` ghi song song.
- 6.2 Gửi lại cùng yêu cầu: không áp dụng: không có đường ghi.
- 6.3 Thứ tự khóa: không áp dụng: không khóa database.
- 6.4 Lỗi tạm của database: `/health/ready` trả 503 khi `Ping` lỗi hoặc quá 1 s, không thử lại (đúng ý nghĩa
  "chưa sẵn sàng"); nhịp sống `worker` lỗi thì ghi log cảnh báo và thử lại ở nhịp sau.
- 6.5 Mức cách ly: không áp dụng: không transaction.
- 6.6 Việc nền lỗi: không áp dụng: chưa có hàng đợi.
- 6.7 Bên ngoài báo 429: không áp dụng.
- 6.8 Tiến trình chết giữa chừng: SIGTERM → máy chủ HTTP `Shutdown` (tối đa 20 s), đóng pool, đẩy nốt dấu vết
  (tối đa 3 s), đóng tệp log; tổng dưới 25 s; `boot_test` đo.
- 6.9 Treo lâu: `/health/ready` hạn 1 s; máy chủ HTTP có `ReadHeaderTimeout`; `crm healthcheck` hạn 3 s.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật luôn đúng (rapid): không áp dụng: không thuộc vùng rủi ro cao.
- 6.12 Dữ liệu cá nhân: đây là phần chính của task: handler log che sâu mọi khóa khớp mẫu ở mọi cấp (map, slice,
  struct, nhóm, `LogValuer`, `json.Marshaler`), quá độ sâu thì thay bằng `[Truncated]` chứ không in thô; kiểu
  `Phone`, `Address`, `Secret` tự che; `Config` có `LogValue()` không in bí mật; dấu vết chỉ mang `service.name`,
  `deployment.environment`.
- 6.13 Quyền: không áp dụng: `/health/live`, `/health/ready` công khai theo kế hoạch (không trả dữ liệu).

## Quyết định thay chủ shop

1. **Tỷ lệ lấy mẫu dấu vết** (Điểm dừng A, quyết định 2): bỏ biến `OTEL_SAMPLE_RATIO` trong Interfaces của kế hoạch,
   thay bằng hai biến chuẩn của OpenTelemetry `OTEL_TRACES_SAMPLER` (nhận `always_on`, `always_off`,
   `traceidratio`, `parentbased_always_on`, `parentbased_always_off`, `parentbased_traceidratio`) và
   `OTEL_TRACES_SAMPLER_ARG` (số trong [0, 1]). Mặc định `parentbased_traceidratio` với `1.0`, tức đúng sampler
   `ParentBased(TraceIDRatioBased(1.0))` của kế hoạch và "không lấy mẫu ở tải thường" của ADR-38. Chỉ
   `config.Load` đọc hai biến này; `Setup` đặt sampler tường minh nên SDK không tự đọc lại. Phương án khác: giữ
   `OTEL_SAMPLE_RATIO` riêng của CRM (không chuẩn, công cụ vận hành không hiểu).
2. **Nhịp sống của `worker`:** bảng `platform.process_heartbeats` có ở Task 10, và Task 3 không làm phần database.
   Ở task này `worker` `Ping` database mỗi 15 s (lần đầu ngay khi chạy) và ghi một dòng log
   `{"msg":"worker heartbeat"}`; `boot_test` chờ dòng đó. `crm healthcheck` với `worker` kiểm `Ping` database
   (hạn 3 s). Task 10 thay bằng kiểm tuổi `process_heartbeats` dưới 90 s và xóa cách tạm này. Phương án khác: tạo
   bảng ngay ở Task 3 (lấn sang Task 4 và 10).
3. **`/health/ready` dùng `Ping` của pool** thay cho câu `SELECT 1`: `Ping` cũng là một vòng hỏi đáp thật với
   PostgreSQL, và không đưa chuỗi SQL vào mã ngoài sqlc / `*_dynsql.go` (luật Global Constraints). Hạn 1 s giữ đúng.
4. **Pool pgx tạm trong `cmd/crm`:** mở bằng `pgxpool.NewWithConfig` với `MaxConns = DB_MAX_CONNS` và
   `QueryExecModeCacheStatement`. Task 4 thay bằng `db.Open` (giới hạn thời gian ADR-26, otelpgx) và xóa hàm tạm.
5. **Vai trò `migrate`:** `config.Load(RoleMigrate, …)` làm đủ (đọc `DATABASE_OWNER_URL`); chạy `CRM_ROLE=migrate`
   ở task này thoát mã khác 0 với thông báo "chưa có, Task 4" thay vì im lặng không làm gì.
6. **`telemetry.Setup` nhận thêm tùy chọn** (`...Option`, có `WithSyncExporter`) để test cắm
   `tracetest.InMemoryExporter`; lời gọi `Setup(ctx, cfg)` của kế hoạch vẫn đúng.
7. **"Lỗi so bằng chuỗi"** trong `lint_test`: không linter nào trong danh sách bắt `err.Error() == "…"`; `errorlint`
   bắt `err == ErrX`. Mẫu cài sẵn có cả hai; `golangci-lint` phải báo `err == ErrX`, và một test AST trong cùng
   `tests/arch/lint_test.go` báo `err.Error()` đứng trong phép so `==`/`!=` hay trong `strings.Contains/HasPrefix/
   HasSuffix/EqualFold`, trên mẫu và trên mã thật (phải 0).
8. **`os.Getenv` trong tệp `_test.go`** được phép (test đọc `CRM_TEST_DATABASE_URL`); luật chỉ nhắm mã chạy thật.
9. **Ngày của tệp log theo UTC** (thời gian lưu trữ của CRM là UTC); tệp đổi lúc 07:00 giờ Việt Nam.
10. **`boot_test` dùng PostgreSQL dev qua `CRM_TEST_DATABASE_URL`** (Makefile dựng từ `CRM_PG_PORT`), chưa dùng
    testcontainers vì `internal/testkit` có ở Task 4. Thiếu biến hay database không lên thì test đỏ, không bỏ qua.
11. **Danh mục sinh bằng `go/doc`** (thư viện chuẩn, cùng nguồn của `go doc -all`) thay vì phân tích văn bản của
    lệnh `go doc`; kết quả như kế hoạch nói (tên gói, câu đầu doc gói, chữ ký hàm và kiểu xuất ra ngoài).
12. **Che theo tên khóa ở cả dạng snake_case** (xem "Khác kế hoạch"). Phương án khác: giữ đúng mẫu và đòi thẻ json
    snake_case cho mọi trường nhạy cảm (dễ quên, lộ dữ liệu).

## Bằng chứng

Lệnh chung: `go test -race -count=1 -v ./...` với PostgreSQL của phiên (`crm-t03`, cổng 5503,
`CRM_TEST_DATABASE_URL` đã đặt) → 7 gói `ok`, 52 dòng PASS, 0 FAIL. Trước khi viết mã: cùng lệnh → mọi gói
`FAIL [build failed]` / `go build ... cmd/crm: directory not found` (đỏ đúng chỗ).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `Load(RoleAPI, rỗng)` lỗi nêu cả `DATABASE_URL` và `ACCESS_AUD` (và mọi biến thiếu khác) | `config.TestLoadAPIEmptyListsEveryMissingVariable` | `go test -race ./internal/platform/config/` | `ok` (PASS) |
| `SELFTEST_WEBHOOK_SECRET` 10 ký tự → lỗi; `HEALTH_TOKEN` ngắn → lỗi | `TestLoadIngestShortSecretFails`, `TestLoadAPIShortHealthTokenFails` | như trên | PASS |
| `ingest` hợp lệ không đòi `ACCESS_AUD`; mặc định cổng, ngân sách kết nối 5/20/10/2 | `TestLoadIngestDoesNotNeedAccess`, `TestLoadDefaultsPerRole` | như trên | PASS |
| `worker` (và `api`, `ingest`) có `DATABASE_OWNER_URL` trong env → không trường nào chứa giá trị đó (duyệt reflection) | `TestLoadNonMigrateNeverHoldsOwnerURL`; `TestLoadMigrateNeedsOwnerURL` cho chiều ngược | như trên | PASS |
| Tỷ lệ lấy mẫu đọc từ `OTEL_TRACES_SAMPLER`/`_ARG`, mặc định `parentbased_traceidratio` 1.0, giá trị sai bị từ chối (Điểm dừng A) | `TestLoadTracesSampler` (9 ca) | như trên | PASS |
| `Config` ghi log không lộ bí mật | `TestConfigLogValueHidesSecrets` | như trên | PASS |
| `Redact` ví dụ của kế hoạch: không còn `0912345678`, `Bearer x`, `xin chào`; còn `ok` | `logging.TestRedactNested` | `go test -race ./internal/platform/logging/` | PASS |
| Che qua struct, con trỏ, slice, thẻ json, `json.Marshaler`; quá độ sâu → `[Truncated]` | `TestRedactStructsPointersMarshalers`, `TestRedactDepthLimitNeverPrintsRaw`, `TestSensitiveKey` | như trên | PASS |
| `slog.Any("order", struct{ShipAddress}{"12 Lê Lợi"})` → `[Redacted]`, không `Lê Lợi`; `Phone("0911")` → không `0911`; nhóm, `With`, nhóm tên nhạy cảm | `TestLoggerRedactsStructAndTypes` | như trên | PASS |
| Log trong span có `trace_id` (và `span_id`) bằng mã của span; ngoài span thì không | `TestLoggerAddsTraceID` | như trên | PASS |
| `DailyFile`: 23:59 và 00:01 → hai tệp; 31 ngày → còn 30 (không đụng tệp vai trò khác); quá `MaxBytes` → bỏ dòng, đếm; ghi song song dưới `-race` | `TestDailyFileRollsAtMidnight`, `TestDailyFileKeeps30`, `TestDailyFileDropsOverMaxBytes`, `TestDailyFileConcurrentWrites` | như trên | PASS |
| Không `LOG_DIR` → không tạo tệp; có `LOG_DIR` → tệp `<vai trò>-YYYY-MM-DD.log`, đã che | `TestNewLoggerWritesDailyFileOnlyWithLogDir` | như trên | PASS |
| `OTEL_ENABLED=false` → span no-op, không xuất gì | `telemetry.TestSetupDisabledExportsNothing` | `go test -race ./internal/platform/telemetry/` | PASS |
| Exporter giả `tracetest.InMemoryExporter` → span có `service.name = crm-api` | `TestSetupEnabledHasServiceName` | như trên | PASS |
| Sampler theo cấu hình: tỷ lệ 0 → 0 span, 1 → đủ, `always_off` → 0… (7 ca); sampler lạ → lỗi | `TestSetupSamplerFromConfig`, `TestSetupRejectsUnknownSampler` | như trên | PASS |
| `/health/live` → 200 `{"status":"ok"}`; `/health/ready` database lên → 200, xuống → 503 `{"status":"unavailable","checks":{"db":"down"}}`, treo → 503 sau khoảng 1 s; chỉ GET | `observability.TestHealthLive`, `TestHealthReadyUp`, `TestHealthReadyDown`, `TestHealthReadyTimesOutAfterOneSecond`, `TestHealthOnlyGET` | `go test -race ./internal/platform/observability/` | PASS |
| `tools/catalog` trên gói mẫu → có chữ ký hàm, phương thức, kiểu, câu đầu doc gói, dòng ghi chú; không lộ mục chưa xuất; sinh hai lần giống hệt | `TestCatalogWithNotes`, `TestCatalogDeterministic`, `TestParseNotesRejectsBadLine` | `go test -race ./tools/catalog/` | PASS |
| Gói thiếu ghi chú → dòng `CẦN GHI CHÚ`, thoát khác 0, stderr nêu gói | `TestCatalogMissingNoteFails` | như trên | PASS |
| `golangci-lint` báo đúng 4 mẫu cài sẵn (`fmt.Println`, `os.Getenv` ngoài config: forbidigo; `//nolint` trần: nolintlint; `err == ErrX`: errorlint) | `arch.TestLintCatchesPlantedMistakes` | `go test -race ./tests/arch/` | PASS |
| Lỗi so bằng chuỗi (`err.Error() == "…"`) bị bắt trên mẫu; mã thật 0 chỗ | `TestErrorStringCompareDetector`, `TestNoErrorStringCompareInRealCode` | như trên | PASS |
| Mã thật sạch lint | `TestLintCleanOnRealCode`; `golangci-lint run ./...` | như trên; `make lint` | PASS; `0 issues.` |
| `boot_test`: tệp chạy dựng với `CGO_ENABLED=0 -trimpath`; `ingest`, `api` → `/health/ready` 200, `worker` → dòng `worker heartbeat` trong 10 s; SIGTERM → mã 0 trong 25 s; stdout chỉ có dòng log JSON | `e2e.TestBootEachRoleThenGracefulStop` | `go test -race -v ./tests/e2e/` | PASS; "ingest tắt êm sau 959µs", "api … 727µs", "worker … 1.08ms" |
| Thiếu một biến bắt buộc → mã khác 0, stderr nêu tên biến; `CRM_ROLE` lạ → mã khác 0 | `TestBootMissingVariableFails`, `TestBootUnknownRoleFails` | như trên | PASS |
| `crm healthcheck`: api chưa chạy → khác 0, đang chạy → 0; worker với database sống → 0, chết → khác 0 | `TestHealthcheckCommand` | như trên | PASS |
| Tắt êm khi exporter dấu vết không tới được (kiểm tay, không phải test tự động) | chạy `worker` với `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:1`, `LOG_DIR`, rồi `kill -TERM` | lệnh tay | thoát trong dưới 1 s, dòng `đã tắt êm`, tệp `worker-2026-11-17.log` (ngày UTC) |
| `make ci` (phần đã có): build, vet, lint, test -race, govulncheck, danh mục không lệch, jscpd so mốc nền, test-guard, gitleaks | — | `make ci COMPOSE_PROJECT=crm-t03` | `exit=0`; govulncheck "No vulnerabilities found"; jscpd "Found 0 clones"; gitleaks "no leaks found" (lịch sử và thư mục) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Xem "Quyết định thay chủ shop" mục 1, 2, 3, 4, 5, 10, 12.
- `SensitiveKey` so cả dạng snake_case của khóa (`BankAccount` → `bank_account`, `IDNumber` → `id_number`): mẫu của
  kế hoạch chỉ có dạng gạch dưới nên bỏ sót tên trường Go. Che nhiều hơn, không ít hơn.
- `.golangci.yml`: `exhaustive.default-signifies-exhaustive: true` (một `default` tường minh là đủ); gosec bỏ qua
  G101, G204, G304 chỉ trong `_test.go` (test dựng tiến trình con, đọc tệp tạm, chuỗi kết nối giả); forbidigo cho
  `os.Getenv` trong `_test.go` (quyết định 8).
- Thêm `.gitleaks.toml` (có trong "Cấu trúc file" của kế hoạch, Task 3 không liệt kê nhưng `make ci` cần): luật mặc
  định cộng ngoại lệ cho ví dụ "đừng làm" trong tài liệu skill đã ghim, bí mật giả 32 ký tự của test, tên tệp log
  trong test.
- Nâng `golang.org/x/net` (gián tiếp, qua exporter OTLP) từ v0.59.0 lên v0.60.0 vì govulncheck báo 5 lỗ hổng
  (GO-2026-6603, 6610, 6611, 6612, 6617) có đường gọi tới; sau khi nâng: "No vulnerabilities found".
- jscpd 5.4.3: khóa `similarity` trong `.jscpd.json` không tự bật dò hàm giống nhau (đã thử), nên `make jscpd` truyền
  `--similarity` tường minh; `semantic` để tắt (mặc định).

## Việc phát hiện thêm

Không tự làm; chủ shop (hoặc task sau) quyết.

1. `scripts/agent/stop-check.sh` gọi `jscpd --config .jscpd.json <tệp>` không kèm `--similarity`, nên hook Stop chỉ
   dò trùng nguyên văn; `make ci` thì có. Tệp hook chỉ chủ shop sửa được.
2. Máy này chưa cài golangci-lint, govulncheck, jscpd dù đã ghim trong `tools/versions.mk`; Task 3 đã cài đúng bản
   (`go install …/golangci-lint/v2/cmd/golangci-lint@v2.15.0`, `govulncheck@v1.9.0`, `npm i -g jscpd@5.4.3`). Nên có
   lệnh `make tools` cài đúng các bản ghim (đề xuất cho Task 23 / CI).
3. `Makefile` đọc tên dự án compose từ `COMPOSE_PROJECT`, còn phiên song song đặt `COMPOSE_PROJECT_NAME`: `make db-up`
   trần sẽ dùng dự án `crm`. Phiên này luôn truyền `COMPOSE_PROJECT=crm-t03`. Đề xuất mặc định
   `COMPOSE_PROJECT ?= $(or $(COMPOSE_PROJECT_NAME),crm)`.
4. govulncheck còn báo 2 lỗ hổng trong module đang require mà mã không gọi tới (không chặn); xem lại khi nâng phụ
   thuộc ở Task 4.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, một vòng, trên đúng phần đã stage: 33 tệp, 3.458 dòng thêm).

- Phát hiện phải sửa: không có.
- Ảnh hưởng: khớp; sót tên `DailyFile.Dropped` trong danh sách ký hiệu (kế hoạch có đòi "bỏ dòng và đếm"), và câu
  "uuid chưa vào `go.mod`" không đúng vì OpenTelemetry kéo nó vào dạng `// indirect`. Đã sửa câu chữ trong tệp này và
  `docs/phu-thuoc.md` (chỉ tài liệu, không đổi mã đã duyệt).
- Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng điều tiêu chí nói; dòng kiểm tay ghi rõ là kiểm tay.
- Ghi chú không bắt buộc (không sửa, ghi lại để task sau biết): nhóm `slog.Group` lồng rất sâu có thể đi quá 8 cấp
  (khóa vẫn bị che theo tên nên không lộ); giá trị kiểu `error` ghi nguyên `Error()`; `catalog-check` không bắt tệp
  danh mục chưa được git theo dõi; dựng `DailyFile` bằng struct literal mà quên `Now` sẽ panic (mọi chỗ dùng
  `NewDailyFile`).
