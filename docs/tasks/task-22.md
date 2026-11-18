# Task 22 — Bộ thử tải M0 có tiêu chí đạt (ADR-42)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `metrics.P95FromHist` đổi thành `metrics.QuantileFromHist(hist, pct)` (phân vị bất kỳ; bộ thử cần p95 và p99 của `job_pickup_ms`). Tham chiếu: chỉ `internal/platform/metrics/registry.go` (định nghĩa) và `registry_test.go`, `flusher_test.go` (grep `P95FromHist` toàn repo: 3 tệp, không có chỗ dùng trong mã chạy). Không ký hiệu xuất ra ngoài nào khác đổi; `tools/loadkit` là chương trình mới (package main).
- Gói phụ thuộc ngược (go list): `internal/platform/metrics` được nhập bởi cmd/crm, queue, webhooks, realtime, observability, httpapi… nhưng không gói nào gọi `P95FromHist`, nên chỉ cần build lại; test chạy: `./internal/platform/metrics/...`. `tools/loadkit` không ai nhập.
- Operation OpenAPI bị chạm, oasdiff: không có (bộ thử chỉ gọi `POST /hooks/selftest`, `GET /api/v1/stream`, `GET /api/v1/me`, `GET /api/v1/platform/jobs` sẵn có).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có câu sqlc, bảng hay migration mới. `tools/loadkit/stats_dynsql.go` chỉ ĐỌC (chủ bảng, ngoài tiến trình ứng dụng): `platform.jobs`, `platform.metrics_minute`, `pg_catalog.pg_stat_user_tables`; có `stats_dynsql_test.go` chạy trên PostgreSQL thật. Dữ liệu thử dựng bằng API công khai sẵn có (`tenancy.SeedTenant`, `identity.SeedOwner`, `tenancy.AddRoute`).
- Tệp mới: `loadtest/k6/{common.js,normal.js,heavy.js,soak.js,chaos.sh}`, `loadtest/run.sh`, `loadtest/compose.loadtest.yml`, `loadtest/README.md`, `tools/loadkit/*`, `docs/do-luong/thu-tai-m0.md`.
- Test sẽ chạy: `go test -race ./internal/platform/metrics/... ./tools/loadkit/...`; `k6 inspect` cho từng tệp k6; một lần chạy khói ngắn cả đường (run.sh với thời lượng 1 phút); rồi các bài đo của kế hoạch (normal, heavy, chaos+heavy, soak rút ngắn). `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tìm "phân vị", "p95", "SSE", "thẻ", "seed"); `internal/testkit` (`AccessKeyServer`, `OpenSSE`, `Sign`); `tests/e2e/stack_test.go` (`postPingTo`, `nextPong`); `cmd/crm/seed.go`; `metrics.P95FromHist`, `metrics.HistBoundsMs`.
- Dùng lại / viết mới vì:
  - Dùng lại `tenancy.SeedTenant`, `identity.SeedOwner`, `tenancy.AddRoute` để dựng 50 nhân viên giả (cùng đường với `crm seed-dev`).
  - Dùng lại histogram `job_pickup_ms` mà worker đã ghi vào `platform.metrics_minute` (Task 10) cho chỉ tiêu "việc nền từ lúc ghi tới lúc nhận"; tổng quát `P95FromHist` thành `QuantileFromHist` thay vì viết hàm phân vị thứ hai (xóa tên cũ, không bọc).
  - Dùng `cfaccess.Verifier` thật trong test của máy chủ khóa giả (thẻ do bộ thử ký phải qua đúng bộ kiểm của api).
  - Viết mới máy chủ khóa Access giả, client SSE và ký thẻ trong `tools/loadkit`: `testkit.AccessKeyServer`, `testkit.OpenSSE` đòi `testing.TB` (chỉ chạy trong `go test`) và chứng chỉ của `httptest` chỉ cho 127.0.0.1, còn container api gọi `host.docker.internal`; client SSE của bộ thử phải tự nối lại và đếm trùng, việc testkit không làm. Phần trùng là vài dòng mã hóa JWKS (giữ riêng: một bên là hạ tầng test, một bên là công cụ đo; khác lý do thay đổi).
  - Ký HMAC của webhook selftest viết lại trong k6 (JavaScript, `k6/crypto`): không có mã Go nào chạy được trong k6.
  - Dùng `logging.Err` cho lỗi ghi log của công cụ (test kiến trúc `TestRawErrorLogProductCode` bắt `err.Error()` thô), `realtime.EventChanged`, `realtime.Entity`, `selftest.PongTopic`, `selftest.Key` để đọc khung SSE, `db.Open` để mở pool.

## Soát thiết kế

Bộ thử tải không thêm nghiệp vụ; nó gửi tải từ ngoài và đọc số đo. Mục 5:

- 5.1 Đúng chủ dữ liệu: không áp dụng: không có dữ liệu hay bảng mới.
- 5.2 Không ghi bảng module khác: dựng dữ liệu thử chỉ qua public API (`SeedTenant`, `SeedOwner`, `AddRoute`); `chaos.sh` thêm việc `command.selftest` vào `platform.jobs` bằng chủ bảng trên database thử (đúng cách `tests/e2e` làm), không chạm production.
- 5.3 Đọc qua view: `stats_dynsql.go` đọc `platform.jobs`, `platform.metrics_minute` (cùng chủ platform) và thống kê của PostgreSQL; không đọc bảng module nghiệp vụ.
- 5.4, 5.5, 5.6: không áp dụng: không có transaction nghiệp vụ, sự kiện hay lệnh ra ngoài mới. Lệnh `command.selftest` dùng `Loopback` sẵn có (không gửi gì ra ngoài).
- 5.7 Ngữ cảnh shop: công cụ chạy bằng chủ bảng (đọc thống kê toàn bảng, như `tests/e2e`), không phải tiến trình ứng dụng; không chạy ở production.
- 5.8 Nghiệp vụ không biết HTTP: không áp dụng: không có mã module.
- 5.9 Đường ghi có khóa chống trùng: không áp dụng: không thêm route.
- 5.10 Valkey: không áp dụng.
- 5.11 Công nghệ mới: k6 (ADR-42 đề xuất, đã cài v2.0.0) chạy ngoài repo Go, không vào go.mod. Công cụ Go chỉ dùng thư viện chuẩn và gói đã có trong go.mod (`golang-jwt/jwt/v5`, pgx). Ghi một dòng k6 vào `docs/phu-thuoc.md` nếu chưa có.
- 5.12 Theo module mẫu: công cụ đặt trong `tools/` như `tools/apicheck`, `tools/routegen`; SQL chạy lúc chạy chỉ trong `*_dynsql.go` có test PostgreSQL thật.

Mục 6:

- 6.1 Đồng thời: công cụ không sửa dữ liệu dùng chung; bộ gom số đo SSE của 50 client dùng mutex, test chạy `-race`.
- 6.2 Gửi lại: mỗi ping có `eventId` duy nhất (`lt.<run>.<ms gửi>.<VU>.<lượt>`); k6 không tự gửi lại; đếm trùng ở client SSE là một tiêu chí đạt.
- 6.3–6.5 Khóa, lỗi tạm, cách ly: không áp dụng: không có transaction ghi. Câu đọc thống kê là câu đơn.
- 6.6 Việc nền lỗi: bộ thử đếm việc `dead`, `in_doubt` sau mỗi bài và ghi vào kết quả.
- 6.7 Bên ngoài 429: không áp dụng. Lượt đọc API của mỗi nhân viên giả giữ dưới giới hạn 300 yêu cầu / 60 s của api (`httpapi.RateLimit`), để 429 không lẫn vào số đo.
- 6.8 Tiến trình chết giữa chừng: đúng là việc của `chaos.sh`: giết `crm-api`, `crm-worker`, ngắt PostgreSQL 60 s; đo thời gian trở lại, sự kiện thiếu (so số webhook nhận 200 với số việc `inbound.selftest` done), lệnh gửi hai lần (việc `command.selftest` có `attempts > 1`).
- 6.9 Treo lâu: mọi lời gọi HTTP của công cụ có hạn; client SSE nối lại có lùi dần; truy vấn thống kê có hạn 30 s.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: "mỗi eventId tới client đúng một lần" và "không lệnh nào gửi hai lần" là tiêu chí đạt của bộ thử, đo trên hệ chạy thật.
- 6.12 Dữ liệu cá nhân: email nhân viên giả `nv01@loadtest.invalid`… và thẻ ký bằng khóa sinh mỗi lần chạy, chỉ nằm trong thư mục kết quả (bỏ khỏi git); không ghi thẻ vào log.
- 6.13 Quyền: nhân viên giả dùng vai trò owner qua `SeedOwner` (cần `realtime.subscribe`, `platform.jobs.manage`); api vẫn kiểm thẻ bằng bộ kiểm thật, chỉ trỏ tới máy chủ khóa giả qua `ACCESS_TEAM_DOMAIN` và `SSL_CERT_FILE` trong tệp compose riêng của bộ thử, không đổi mã api.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Phân vị bất kỳ từ histogram (p95, p99 của `job_pickup_ms`) | `TestQuantileFromHist`, `TestQuantileFromHistRejectsBadPercent`, `TestFlushHistogram` | `go test -race ./internal/platform/metrics/` | `ok … 34.2s` |
| Thẻ của bộ thử qua đúng bộ kiểm Cloudflare Access thật; không tin CA thì bị từ chối | `TestAccessTokenPassesRealVerifier`, `TestWriteAccessFiles`, `TestStaffEmail` | `go test -race -count=1 ./tools/loadkit/` | `ok … 16.2s` |
| Client SSE: đọc pong, bỏ khung lạ, tự nối lại, lùi dần khi lỗi; đếm trùng, thiếu, độ trễ | `TestSSEClientReadsAndReconnects`, `TestSSEClientRetriesOnError`, `TestCollector`, `TestEventIDRoundTrip`, `TestPercentileNearestRank` | như trên | `ok` |
| Chấm đúng từng ngưỡng (vượt một chút → KHÔNG ĐẠT; thiếu số đo → CHƯA ĐO; bài hỏng giữa tải, bài ngâm) | `TestEvaluateNormalAllPass`, `TestEvaluateEachThresholdFails` (11 ca), `TestEvaluateMissingDataIsNotPass`, `TestEvaluateChaos`, `TestEvaluateSoak`, `TestReportCommand` | như trên | `ok` |
| Số đo database của lượt chạy (việc, lệnh gửi hai lần, histogram trong khung, bảng jobs) trên PostgreSQL thật; dựng 50 nhân viên chạy lại được | `TestReadStats`, `TestSeedStaff` | như trên | `ok` |
| Tệp k6 đọc được | — | `k6 inspect loadtest/k6/{normal,heavy,soak}.js` | cả ba `ok` |
| Cả đường chạy thật (khói 1 phút): 50 client nhận đủ 600/600 eventId, trùng 0, mất 0 | `loadtest/run.sh` | `DURATION=1m loadtest/run.sh normal` | bảng chấm in ra; số đo trễ vượt ngưỡng (máy tải ~20) |
| Cổng kiểm toàn bộ (sau khi gộp main ba lần: Task 333, 396, 38, 39) | — | `make ci` (build → test-guard), rồi `make gitleaks console-check` sau khi xóa thẻ thử khỏi thư mục kết quả | mọi bước xanh; `go test -race ./...` gồm `tools/loadkit ok 8.8s`; govulncheck 0; test-guard qua |
| Bài thường 30 phút, bài nặng, hỏng giữa tải, ngâm rút ngắn | `loadtest/run.sh` | xem `docs/do-luong/thu-tai-m0.md` | số đo tham khảo trên laptop |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm tệp ngoài danh sách Files của kế hoạch: `loadtest/run.sh` (dựng, lấy mẫu, chấm một bài), `loadtest/compose.loadtest.yml` (image riêng, api trỏ tới máy chủ khóa giả), `tools/loadkit/*` (phần Go: thẻ Access, 50 client SSE, số đo database, chấm ngưỡng). Lý do: k6 v2 không có client SSE sẵn, không ký được thẻ Access mà api thật chấp nhận, không đọc được PostgreSQL; ngưỡng "mỗi eventId tới client đúng một lần", "việc nền ghi tới nhận", "không lệnh nào gửi hai lần" chỉ đo được ngoài k6.
- `metrics.P95FromHist` đổi thành `metrics.QuantileFromHist(hist, pct)` (bộ thử cần p99 của `job_pickup_ms`); tên cũ không còn chỗ dùng nào trong mã chạy.
- `QuantileFromHist` nằm cùng commit với bộ thử (chỗ dùng duy nhất của nó), không tách commit: đây là mở rộng hành vi (phân vị bất kỳ) kèm nơi dùng, không phải dời hay đổi tên thuần.
- Lượt đọc API của bài nặng tăng 4× (2 lượt/giây mỗi nhân viên) thay vì 10×: 10× là 5 lượt/giây mỗi nhân viên, chạm đúng giới hạn 300 / 60 s của api và sẽ đo 429 thay vì tốc độ.
- Bài hỏng giữa tải không chấm các ngưỡng độ trễ (chỉ in ra để tham khảo) và đòi "không client nào nhận trùng" thay cho "mỗi eventId tới mỗi client đúng một lần": client mất luồng lúc api chết nhận `resync` rồi tải lại (thiết kế 04 mục 6), nên sự kiện trong khoảng mất luồng không tới qua SSE; "không mất sự kiện" vẫn chấm đủ ở database.
- Thời điểm "trở lại" của `chaos.sh`: ba vai trò healthy (Docker lấy mẫu healthcheck mỗi 5 s) và không còn việc pending quá hạn 2 s; tính từ lúc giết (khởi động lại ngay) hoặc lúc bật lại PostgreSQL. Kế hoạch không định nghĩa "trở lại"; đây là định nghĩa chặt nhất đo được từ ngoài.

## Quyết định thay chủ shop

- **Ngâm 8 giờ chưa chạy đủ trên laptop; chạy rút ngắn để tham khảo.** Chọn: `soak.js` để mặc định 8 giờ đúng kế hoạch, lần này chạy rút ngắn (`DURATION`), ghi rõ tiêu chí nào CHƯA ĐO. Vì sao: phiên này không giữ được tiến trình 8 giờ (giới hạn chạy nền 2 giờ), máy đang chạy nhiều phiên khác (load average ~20 trên 10 nhân) nên số đo 8 giờ ở đây cũng không phải tiêu chí chính thức; tiêu chí đạt chính thức đo trên máy thử (Task 23 bước 7.9, cần OK của chủ shop). Phương án khác: để tiến trình chạy tách khỏi phiên qua đêm (không ai theo dõi, không ghi được kết quả vào commit). Đảo ngược: `loadtest/run.sh soak` bất cứ lúc nào.
- **api kiểm thẻ thật, trỏ tới máy chủ khóa giả, không thêm cờ "bỏ qua Access".** Chọn: lớp phủ compose đặt `ACCESS_TEAM_DOMAIN=host.docker.internal:<cổng>` và `SSL_CERT_FILE` tới CA của bộ thử. Vì sao: không đổi mã api, không mở đường vòng xác thực nào có thể lọt vào production; thẻ đi đúng bộ kiểm `cfaccess`. Phương án khác: biến môi trường tắt kiểm thẻ ở dev (mở rủi ro bảo mật, trái ADR-35). Đảo ngược: xóa `loadtest/compose.loadtest.yml`.
- **Image riêng `crm:loadtest`.** Vì sao: `crm:dev` dùng chung giữa các phiên song song, phiên khác dựng lại thì bộ thử đo nhầm mã. Đảo ngược: `CRM_IMAGE=crm:dev`.

## Việc phát hiện thêm

- **PostgreSQL của `docker/compose.dev.yml` không có `init: true`** (đo 18/11 lúc 08:47:54 UTC trong bài normal): PostgreSQL là PID 1, healthcheck `pg_isready` quá hạn dưới tải bị bỏ lại, thoát mã 2, postmaster coi là tiến trình con sập ("untracked child process … exited with exit code 2") và khởi động lại mọi kết nối (crash recovery, ~100 s hàng đợi đứng). Bộ thử đã bật `init: true` trong lớp phủ của mình; cần sửa ở `docker/compose.dev.yml` và nhất là compose của máy thử, máy thật (Task 23 bước 7.4). Không tự sửa vì ngoài phạm vi task.
- **`COMMIT` bị hủy vì `lock_timeout` (55P03) dưới tải** (`tx_retry{55P03}` 178 lần trong 30 phút ở 10 sự kiện/giây trên laptop): transaction có `pg_notify` phải giữ khóa toàn cục của hàng đợi NOTIFY lúc commit; khi máy chậm, chờ quá 5 s là lỗi. ADR-42 đã ghi trần NOTIFY; cần đo lại trên máy thử trước khi kết luận.
- **Mọi ping selftest của một tenant cùng `OrderKey` (= tenantKey)** nên việc `inbound.selftest` chạy nối tiếp: thông lượng đường selftest bị giới hạn bởi một hàng; đây là thiết kế của nguồn thử, không phải của kênh thật. Ghi để đọc đúng số đo của bài nặng.
- **Worker gửi dấu vết tới `http://jaeger:4318` bị 404** (log `traces export: failed … 404`) trên compose dev: đường OTLP HTTP của Jaeger 2.23 hoặc biến `OTEL_EXPORTER_OTLP_ENDPOINT` của compose chưa khớp. Ngoài phạm vi.

## Reviewer

Kết luận: ĐẠT (lần 1 trên bản đã gộp main tới Task 39; lần 2 sau ba chỉnh nhỏ: ĐẠT).

- Không có phát hiện phải sửa. Reviewer kiểm: dedupe_key của `inbound.selftest` là eventId nên đếm theo tiền tố lượt chạy đúng; `attempts > 1` chỉ có thể chặt hơn, không cho ĐẠT sai; CHƯA ĐO khi thiếu số đo; không chạm mã api, thẻ qua `cfaccess` thật, CA và khóa mới mỗi lượt, `tokens.json` 0600 và bị xóa khi xong; collector không race; script chạy được với bash 3.2.
- Ghi chú không bắt buộc, đã làm: (1) thêm dòng "Khác kế hoạch" về cách chấm bài hỏng giữa tải; (3) vòng chờ hàng đợi cạn của `run.sh` không thoát khi `loadkit stats` lỗi tạm; (4) `LOADTEST_ACCESS_LISTEN` cho máy Linux (máy thử).
- Ghi chú giữ nguyên, có lý do: (2) `loadkit report` chạy tay thiếu `db.json` có thể cho ĐẠT ở dòng chết, và lần hỏng chaos.sh chưa ghi thì không hiện — `run.sh` luôn ghi đủ; (5) `chaos.sh` thêm lệnh chậm hơn 2/giây vì mỗi lệnh qua `docker compose exec` — chỉ đổi số lệnh dùng để đo.
