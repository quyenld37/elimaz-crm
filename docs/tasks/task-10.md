# Task 10 — Chỉ số theo phút, nhịp sống, nhịp sống gửi ra ngoài

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md` (Task M0-10). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu xuất ra ngoài **có sẵn** nào đổi chữ ký. Gói mới
  `internal/platform/metrics`: `HistBoundsMs`, `Registry` (`Observe`, `Inc`, `Gauge`, `Drain`), `Row`, `Flusher`
  (`Flush`, `Beat`, `Run`), `P95FromHist`, `Pinger` (`Ping`), `Middleware`, `HeartbeatAge`, `HeartbeatMaxAge`,
  `ErrNoHeartbeat`. Chỗ dùng ký hiệu có sẵn: `db.DB.OnRetry`, `db.DB.OnExhausted` (2 chỗ gọi trong `tx.go`, 4 chỗ trong
  `retry_test.go`; task chỉ **gán** hai móc này trong `cmd/crm`, không đổi định nghĩa). Trong `cmd/crm/main.go` chỉ đổi
  ký hiệu không xuất ra ngoài: **xóa** `runWorker` (nhịp sống tạm là dòng log "worker heartbeat" của Task 3, 1 chỗ gọi)
  và hằng `heartbeatEvery`, `heartbeatTimeout`; `serve` chạy `Flusher.Run` ở cả ba vai trò; `serveHTTP` bọc mux bằng
  `metrics.Middleware`; `healthcheck` của worker đổi sang tuổi `process_heartbeats` dưới 90 s.
- Gói phụ thuộc ngược (go list): `internal/platform/metrics` mới, sau task chỉ `cmd/crm` nhập. `metrics` nhập `db`,
  `config`, `internal/platform/internal/store`. Không gói nào nhập `cmd/crm`; `tests/e2e` dựng nó thành tệp chạy.
- Operation OpenAPI bị chạm, oasdiff: không (chưa có `openapi.yaml`, Task 6; `/health/*` không thuộc hợp đồng).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: pha **mở rộng**, `migrations/0006_metrics.sql` (số do kế hoạch Q-X7 giao cho Task 10):
  bảng mới `platform.metrics_minute`, `platform.process_heartbeats` (bảng nền tảng không RLS, đã có trong danh sách
  Global Constraints và trong `docs/adr/0046-thu-tu-khoa.md` nhóm (b)); `GRANT SELECT, INSERT, UPDATE` cho `crm_app`.
  Câu sqlc mới `queries/platform/metrics.sql`: `UpsertMetricMinute`, `UpsertHeartbeat`, `HeartbeatAge`.
  Sinh lại `internal/platform/internal/store` bằng `make gen` (models.go thêm hai kiểu, tệp mới `metrics.sql.go`).
- Test sẽ chạy: `go test -race ./internal/platform/metrics/... ./cmd/... ./tests/arch/... ./tests/e2e/...` và gói phụ
  thuộc `internal/platform/db/...` (mã sinh trong store đổi) trên PostgreSQL riêng của phiên (`crm-t10`, cổng 5510);
  rồi `make ci` phần đã có. Không chạm vùng rủi ro cao.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có config, db, logging,
  observability, telemetry; không có gì về chỉ số, histogram, nhịp sống. Tìm `heartbeat`, `metric`, `hist`, `p95`
  trong `internal/`, `cmd/`, `tests/`: chỉ có `runWorker` tạm của Task 3 (`cmd/crm/main.go`) → xóa, thay bằng
  `Flusher.Beat`; `config.Config.HeartbeatURL` (Task 3) → dùng làm `Pinger.URL`. `spikes/flashsale` có đo độ trễ riêng
  (module riêng, không nhập `internal/`) → không gộp.
- Dùng lại / viết mới vì:
  - `db.RetryTx` cho lần ghi chỉ số và nhịp sống (transaction riêng, đúng nhóm (b) của bảng thứ tự khóa).
  - `testkit.StartPostgres` cho test trên PostgreSQL thật; `observability.NewHealth` cho test `http_ms` của
    `/health/live`.
  - `Registry` tự viết: ADR-44 không có thư viện chỉ số (spec 04 mục "Đo lường", ADR C6: không `/metrics`, không
    Prometheus); OpenTelemetry metrics SDK có trong go.sum nhưng ADR-44 chỉ dùng OTel cho dấu vết, và nó không ghi
    vào PostgreSQL. Một mutex và ba map là đủ.
  - `Pinger` dùng `net/http` chuẩn (không `http.DefaultClient`, linter cấm).
  - Không có cảnh báo dupl/jscpd lúc viết mục này; nếu có sẽ ghi ở "Bằng chứng".

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: `metrics_minute`, `process_heartbeats` thuộc `platform` (05 mục bảng nền tảng; Global Constraints).
2. Không ghi bảng module khác: chỉ `internal/platform/metrics` ghi hai bảng này; `sqlusage_test` kiểm schema.
3. Đọc dữ liệu module khác: không áp dụng: không đọc bảng module nào.
4. Việc nguyên tử: một lần ghi chỉ số là một `RetryTx` riêng, không chung transaction với bảng nghiệp vụ (ADR-46 nhóm (b)).
5. Việc không nguyên tử sau commit: gọi nhịp sống ra ngoài chỉ chạy **sau** khi transaction ghi chỉ số đã commit,
   ngoài `RetryTx` (hàm trong `RetryTx` không gọi mạng).
6. Gọi ra bên ngoài: `GET` tới `HEARTBEAT_URL` không phải lệnh nghiệp vụ, gửi lại không gây hại (idempotent theo bản chất
   của dịch vụ nhận nhịp sống); không cần `in_doubt`. Hạn 5 s, lỗi chỉ log và đếm.
7. Ngữ cảnh shop: không áp dụng: bảng nền tảng không có `tenant_id` (Global Constraints liệt kê cả hai bảng).
8. Nghiệp vụ không biết HTTP: `metrics` là gói nền tảng; `Middleware` là lớp HTTP mỏng, không có module nghiệp vụ nào nhập.
9. Đường ghi có khóa chống trùng: không áp dụng: không có route `/api/v1` mới.
10. PostgreSQL là nguồn sự thật: chỉ số ghi vào PostgreSQL; không Valkey.
11. Thư viện: không thêm thư viện nào.
12. Module mẫu: không áp dụng: chưa có (Task 7, 12).

Mục 6 (đúng khi chạy đồng thời):

1. Hai bên cùng sửa: `Registry` khóa bằng một mutex (test 1.000 goroutine dưới `-race`). Hai lần `Flush` cùng phút (hay
   hai tiến trình khác `process`) cộng dồn bằng `INSERT … ON CONFLICT (bucket, process, name, label) DO UPDATE` (một câu
   nguyên tử trên từng dòng); `Gauge` thay bằng giá trị cuối.
2. Gửi lại: `Flush` lỗi thì giữ lô chưa ghi để ghi ở lần sau (có trần), không cộng hai lần vì lô chỉ bị bỏ sau commit
   thành công. Lỗi commit không rõ kết quả: lô có thể bị ghi hai lần — chấp nhận cho số đo (ghi ở "Quyết định").
3. Thứ tự khóa: transaction chỉ chạm `metrics_minute` (theo thứ tự lô) rồi `process_heartbeats` ở transaction khác;
   hai tiến trình khác `process` không chung dòng. Đúng nhóm (b) của `0046-thu-tu-khoa.md`.
4. Lỗi tạm: qua `RetryTx`; hàm trong đó gán lại kết quả, không cộng vào biến ngoài.
5. Mức cô lập: READ COMMITTED mặc định.
6. Việc nền lỗi: không áp dụng: không dùng hàng đợi (Task 11). Vòng `Run` lỗi thì log `Warn` và chạy tiếp ở nhịp sau.
7. Bên ngoài quá tải (429): nhịp sống chỉ gửi mỗi phút một lần, không thử lại trong vòng; không cần lùi.
8. Tiến trình chết giữa chừng: chỉ số trong bộ nhớ của phút đang gom mất (chấp nhận, spec "mỗi phút một lần");
   nhịp sống ngừng cập nhật → healthcheck worker đỏ sau 90 s, nơi nhận ngoài báo (ADR-38).
9. Treo lâu: ghi qua `RetryTx` với giới hạn của vai trò; `Ping` hạn 5 s; lần ghi cuối khi tắt êm có hạn 5 s (tổng dưới 25 s).
10. Tiền: không áp dụng.
11. Quy luật bất biến bằng rapid: không áp dụng: không thuộc vùng rủi ro cao.
12. Dữ liệu cá nhân: nhãn chỉ là `<METHOD> <pattern>` của route (không đường dẫn thật, không tham số); `HEARTBEAT_URL`
    thường chứa mã bí mật → không log URL (lỗi `*url.Error` được bóc chỉ còn lỗi gốc).
13. Quyền: không áp dụng: không có route mới.

## Quyết định thay chủ shop

1. **Chỉ worker gửi nhịp sống ra ngoài** (`Pinger` chỉ gắn khi `CRM_ROLE=worker`), dù `HEARTBEAT_URL` đọc được ở mọi
   vai trò. Vì sao: ADR-38 ghi "Worker … mỗi lần chạy xong gửi một tín hiệu"; nếu api cũng gửi cùng URL thì api còn
   sống sẽ che việc worker đã chết (HT22). Phương án khác: mỗi vai trò một URL riêng (thêm biến cấu hình, chờ G7 chọn
   dịch vụ). Đảo ngược dễ: một dòng trong `cmd/crm/main.go`.
2. **Tên tiến trình `<vai trò>@<tên máy>`** (`metrics.ProcessName`). Vì sao: `crm healthcheck` chạy trong cùng
   container (cùng tên máy = mã container) phải tìm đúng dòng `process_heartbeats` của chính nó; hai bản worker trên
   hai container không ghi đè nhau. Phương án khác: biến `PROCESS_NAME` (thêm cấu hình, dễ đặt trùng).
3. **`Flush` ghi cả nhịp sống trong cùng transaction** với các dòng chỉ số, và luôn chạy transaction kể cả khi không có
   chỉ số nào; `Pinger.Ping` chạy sau khi transaction đó commit. Vì sao: tín hiệu gửi ra ngoài phải chứng minh "worker
   sống **và** ghi được database"; worker ở M0 gần như chưa có chỉ số (hàng đợi là Task 11), nếu bỏ qua lần ghi rỗng
   thì không bao giờ có tín hiệu. Hai bảng cùng nhóm (b) của bảng thứ tự khóa, không chung với bảng nghiệp vụ.
4. **Lỗi commit không rõ kết quả có thể ghi một lô chỉ số hai lần**: chấp nhận cho số đo (không phải tiền, không phải
   nghiệp vụ). Khi ghi lỗi, lô được giữ (tối đa 10.000 dòng, bỏ dòng cũ nhất) và ghi vào **đúng phút gốc** ở lần sau.
5. **Nhãn `http_ms`**: `<METHOD> <mẫu route>`; yêu cầu không khớp route ghi `<METHOD> unmatched`, phương thức lạ ghi
   `OTHER`. Vì sao: đường dẫn thật có thể chứa SĐT, và phương thức tùy ý từ client làm số nhãn phình không giới hạn.
6. **Counter**: cột `sum` bằng `count` (cột `NOT NULL`), `max` = 0, `hist` NULL. **Gauge**: `count` số lần đặt, `sum`
   giá trị cuối, `max` lớn nhất trong phút.
7. **Lỗi gửi nhịp sống không chứa URL hay địa chỉ máy nhận** (URL thường mang mã bí mật): bóc `*url.Error`,
   `*net.OpError`, `*net.DNSError`.
8. **api/ingest**: lần ghi chỉ số cuối khi tắt êm (tối đa 5 s) chạy song song với lúc tắt HTTP (tối đa 20 s), pool chỉ
   đóng sau khi nó xong; tổng vẫn dưới 25 s. Chỉ số của yêu cầu xử lý trong lúc tắt có thể mất (chấp nhận).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi có mã | cả gói `metrics`; `TestHealthcheckCommand` | `go test -race ./internal/platform/metrics/`; `go test -race -run TestHealthcheckCommand ./tests/e2e/` | `no non-test Go files … [build failed]`; `chưa có worker chạy (chưa có nhịp sống) mà healthcheck worker vẫn 0` |
| 95 × 40 và 5 × 900 → 1 dòng, count 100, max 900, P95 50 | `TestFlushHistogram`, `TestP95FromHist` | `go test -race -count=1 -v ./internal/platform/metrics/` | `--- PASS: TestFlushHistogram`, `--- PASS: TestP95FromHist` |
| Hai lần Flush cùng phút cộng dồn; Gauge giữ giá trị cuối | `TestFlushAccumulatesWithinMinute`, `TestRegistryKinds` | như trên | `--- PASS` cả hai |
| Ghi lỗi giữ lô, ghi đúng một lần vào phút gốc | `TestFlushKeepsBatchOnError` | như trên | `--- PASS` |
| `process_heartbeats.at` được cập nhật; tuổi đọc theo đồng hồ database | `TestHeartbeat` | như trên | `--- PASS` |
| `GET /health/live` sinh `http_ms` nhãn `GET /health/live`; đường lạ không lộ đường dẫn | `TestMiddlewareRecordsRoutePattern` | như trên | `--- PASS` |
| Một lần Flush → một GET | `TestFlushPingsAfterWrite` | như trên | `--- PASS` |
| Máy nhận treo 10 s → Flush trả trong 6 s, đếm `heartbeat_ping_failed` | `TestFlushPingHangDoesNotBlock` | như trên | `--- PASS: TestFlushPingHangDoesNotBlock (5.17s)` |
| URL rỗng không làm gì; lỗi không lộ URL | `TestPingEmptyURLDoesNothing`, `TestPingErrorHidesURL` | như trên | `--- PASS` cả hai |
| 1.000 goroutine cùng Observe dưới -race → count 1000 | `TestRegistryConcurrentObserve` | như trên (có `-race`) | `--- PASS` |
| Worker: nhịp sống tạm (dòng log) bị xóa; healthcheck đỏ khi chưa có nhịp sống, xanh khi worker chạy, đỏ khi database chết | `TestHealthcheckCommand`, `TestBootEachRoleThenGracefulStop/worker` | `go test -race -count=1 -v -run 'TestBootEachRole\|TestHealthcheckCommand' ./tests/e2e/` | `--- PASS: TestHealthcheckCommand`; `worker tắt êm sau 3.739ms` |
| Cả ba vai trò vẫn khởi động và tắt êm với Flusher chạy | `TestBootEachRoleThenGracefulStop` | như trên | `--- PASS` ingest, api, worker |
| Máy chủ HTTP lỗi sớm (cổng bị chiếm) → api thoát khác 0, không treo vì Flusher (phát hiện của reviewer lần 1) | `TestBootPortTakenExitsWithError` | `go test -race -count=1 -run TestBootPortTaken ./tests/e2e/` | trước khi sửa: `FAIL … 12.090s` (treo tới hạn 10 s); sau khi sửa: `ok elimaz.com.vn/crm/tests/e2e 10.189s` (cả gói) |
| Toàn bộ cổng kiểm | build, vet, golangci-lint, gen-check, sqlc vet, test -race, govulncheck, catalog, jscpd, test-guard, gitleaks | `make ci` (CRM_PG_PORT=5510) | `exit=0`; `golangci-lint 0 issues`; `No vulnerabilities found.`; jscpd `Found 0 clones.`; gitleaks `no leaks found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm vào `Flusher` trường `Log *slog.Logger` (nil = `slog.Default()`) để log lỗi gửi nhịp sống ở mép vòng lặp; thêm
  `Flusher.Beat`, `metrics.ProcessName`, `HeartbeatAge`, `HeartbeatMaxAge`, `ErrNoHeartbeat` (healthcheck worker cần),
  kiểu `Kind` và `Row.Kind`, `ErrPingStatus`, `ErrPingDNS`. Middleware nằm ở tệp riêng `middleware.go`.
- Câu sqlc tên `HeartbeatAgeSeconds` (trả giây theo đồng hồ database) thay cho `HeartbeatAge` ghi ở mục Ảnh hưởng.
- `tests/e2e/boot_test.go`: worker hết dòng log "worker heartbeat" (cách tạm của Task 3 bị xóa) nên test khởi động
  worker chờ `crm healthcheck` về 0; test boot dùng database thử đã migrate (`testkit.StartPostgres`); test healthcheck
  worker thêm trường hợp "database sống nhưng chưa có nhịp sống → khác 0". Không bớt assertion nào.
- `docs/danh-muc-ghi-chu.yaml` thêm dòng "khi nào không dùng" cho gói `metrics`; `docs/danh-muc-dung-chung.md` sinh lại.

## Việc phát hiện thêm

- Dải số migration đánh trước (0006 cho Task 10) trong khi goose của M0 không cho chạy migration số nhỏ đến sau: nếu
  Task 10 gộp vào `main` trước Task 5, 7, 8, 9 thì database dev đã có 0006 sẽ từ chối 0002–0005 (database thử mới thì
  không sao). Điều phối viên cần gộp theo thứ tự số, hoặc `make db-reset` database dev sau khi gộp.
- Dọn `metrics_minute` quá 30 ngày thuộc Task 13 (`RegisterCleanup`), chưa làm ở đây.

## Reviewer

Lần 1: CẦN SỬA. Một phát hiện về tính đúng: `cmd/crm/main.go` — api/ingest treo vô hạn khi `serveHTTP` trả lỗi trước
tín hiệu tắt (cổng bị chiếm, `Serve` dừng bất thường), vì `defer wg.Wait()` chờ `flusher.Run(ctx)` mà `ctx` chỉ hủy
khi có SIGTERM. Đã sửa: `flusher.Run` nhận context con, `defer cancel()` chạy trước `wg.Wait()`; thêm test e2e
`TestBootPortTakenExitsWithError` (đỏ trước khi sửa, xanh sau). Hai ghi chú không bắt buộc (gộp `hist` theo số ngăn
của dòng mới; lô gán vào phút lúc Flush) giữ nguyên, đã đúng với spec "mỗi phút một lần" và số ngăn cố định.

Lần 2: ĐẠT. Bản sửa đúng (cancel chạy trước wg.Wait, pool đóng sau), test mới kiểm đúng kịch bản; không còn phát hiện phải sửa.

Kết luận: ĐẠT
