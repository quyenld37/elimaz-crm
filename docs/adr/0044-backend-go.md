# ADR-44 — Backend viết bằng Go

Trạng thái: đã duyệt (chủ shop chốt 15/11/2026); thay ADR-02 và ADR-33
Ngày: 15/11/2026
Kiểm bằng: `make ci` (build, vet, lint, gen-check, sqlc-vet, `test -race`, vuln, oasdiff); tests/arch (boundaries, sqlmode, dynsql, sqlcvet, lint); `TestBootEachRoleThenGracefulStop` (tests/e2e/boot_test.go); phép đo flash sale `docs/do-luong/flash-sale-go.md`

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-44. Tệp này không chép lại quyết định.

Backend (`ingest`, `api`, `worker`, `imager`) viết bằng Go 1.27, ghim bằng `toolchain` trong `go.mod`. Bộ thư viện
chọn cho từng việc theo bảng của spec (net/http, ogen, pgx v5 cấm simple protocol, sqlc, goose, `log/slog`,
OpenTelemetry, rapid, gremlins…). Bốn chốt của ADR-33 ở dạng Go. Đo đường mua flash sale bằng Go đầu M0.

## Kiểm ở đâu

- Chốt 1 (trình biên dịch, vet, lint, mã sinh không lệch): `make build vet lint gen-check`; `TestLintCatchesPlantedMistakes`, `TestLintCleanOnRealCode` (tests/arch/lint_test.go).
- Chốt 2 (khởi động từng tiến trình, tắt êm): `TestBootEachRoleThenGracefulStop`, `TestBootMissingVariableFails` (tests/e2e/boot_test.go); `TestRun_HealthcheckFailsWhenNotRunning` (cmd/imager/main_test.go).
- Chốt 3 (mọi câu SQL có kiểm): `TestSqlcVet` (tests/arch/sqlcvet_test.go); `TestDynSQLProductCode` (tests/arch/dynsql_test.go).
- Chốt 4 (nghiệp vụ không nhập `net/http`, ogen; không nhập `internal` của module khác): `TestBoundariesNotCalledModule`, `TestBoundariesRealCode` (tests/arch/boundaries_test.go).
- Cấm simple protocol: `TestSQLModeReportsSample`, `TestSQLModeProductCode` (tests/arch/sqlmode_test.go). Lỗ hổng: `make vuln` (govulncheck).
- `-race`: `make test`. CI đỏ khi có tranh chấp cố ý cài vào: chưa có test (phát hiện Task 23).
- Log và dấu vết không chứa SĐT, email: `TestLoggerRedactsStructAndTypes`, `TestRedactNested` (internal/platform/logging/redact_test.go); `TestInternalErrorPersonalDataNotLeaked` (internal/httpapi/personaldata_test.go).
- Phép đo flash sale: spikes/flashsale/buy_test.go, `docs/do-luong/flash-sale-go.md` (laptop; số chính thức đo lại trên máy thử).
- Mutation testing (gremlins): mutation testing gremlins `make mutation`, `.gremlins.yaml`, kết quả `docs/mutation.md` (18/11/2026: mọi gói vùng cao ≥ 80% sau test thêm; queue, commands tính bằng cài tay từng đột biến, chờ lượt gremlins theo lịch xác nhận).
