# Task 397 — Dọn 2 đoạn trùng do Task 19 mang vào (M0-x2)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm `urlerr.Strip` (gói mới `internal/platform/urlerr`, chỉ thư
  viện chuẩn). Không đổi ký hiệu xuất nào có sẵn: ba hàm `stripURL` không xuất (`observability/alerter.go:186`,
  `scripts/external-probe/probe.go:196`, `metrics/pinger.go:60`) — hai bản đầu bị xóa, gọi thẳng `urlerr.Strip` (2 + 4
  chỗ gọi); bản của metrics giữ phần riêng (`net.DNSError` → `ErrPingDNS`, `net.OpError`) và dùng `urlerr.Strip` cho
  bước bóc `*url.Error`. Test: `openDB` của `metrics_test` thay bằng `testkit.OpenAppDB`; hàm trợ giúp test của
  `queue_test` (`jobRow` thêm `RunAt`, hàm mới `env.dbNow`, `runParallelUntilIdle` nhận thêm `*env`).
- Gói phụ thuộc ngược (go list): `urlerr` mới, chỉ ba gói trên nhập. `observability` được `cmd/crm`, `httpapi`,
  `metrics`… nhập nhưng API không đổi; `scripts/external-probe` là lệnh `main`, không ai nhập.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: `go test -race ./internal/platform/urlerr/... ./internal/platform/metrics/... ./internal/platform/observability/... ./scripts/external-probe/... ./tests/arch/...`;
  `go test -race ./internal/platform/queue/...` cả bộ (vùng rủi ro cao, dù chỉ đổi test); `make jscpd`; `make ci` một
  lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: tìm `url.Error`, `stripURL` trong toàn repo — có 3 bản
  cùng mục đích (bỏ URL chứa token/khóa khỏi lỗi trước khi bọc, log), không bản nào xuất ra. Danh mục: `testkit.OpenAppDB`
  (`internal/testkit/pg.go:42`) làm đúng việc của `openDB` trong `metrics_test` (StartPostgres + db.Open crm_app +
  Cleanup).
- Dùng lại / viết mới vì:
  - Đoạn 1 (`metrics/flusher_test.go` ↔ `observability/helpers_test.go`): dùng lại `testkit.OpenAppDB` ở metrics (xóa
    `openDB`). `observability.newEnv` giữ nguyên vì cần thêm `adminURL`, `appURL` mà `OpenAppDB` không trả.
  - Đoạn 2 (`observability/alerter.go` ↔ `external-probe/probe.go`): ba bản `stripURL` cùng mục đích và cùng lý do
    thay đổi (cách Go gói URL vào lỗi) → viết mới `internal/platform/urlerr.Strip` (gói nền tảng theo việc, chỉ thư viện
    chuẩn để external-probe vẫn "chỉ dùng thư viện chuẩn" — thêm test kiến trúc giữ điều này). Phần bóc
    `net.DNSError`/`net.OpError` của metrics là riêng của nhịp sống (sentinel `ErrPingDNS`), giữ trong metrics.
  - Test hàng đợi: dùng lại `env.ownerExec`, `env.job`.

## Soát thiết kế

- 5.1–5.12: chỉ 5.x về gói chung áp dụng: mã chung nằm `internal/platform/urlerr`, tên danh từ nói việc, không
  `util/common`; không chạm module nghiệp vụ, ranh giới module, OpenAPI, database. Còn lại không áp dụng: không đổi
  kiến trúc, migration, quyền.
- 6.1–6.13: không áp dụng cho mã chạy: `Strip` là hàm thuần, không trạng thái; hành vi bóc lỗi giữ nguyên (lỗi hết hạn
  vẫn khớp `context.DeadlineExceeded`, không lộ URL). Test hàng đợi: sửa cách đo (mốc giờ database trước/sau lượt chạy
  thay vì `now()` lúc đọc) và đẩy việc chờ thử lại ra xa giữa các lượt — không nới tiêu chí: vẫn đòi chờ 4–6 s đúng lịch
  `RetrySchedule[0]` ±20%, vẫn đòi mỗi tin gọi đúng một lần, không chồng hội thoại.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi sửa: jscpd đỏ đúng 2 đoạn | — | `make jscpd` (main `b054999`) | `metrics/flusher_test.go [23:3-29:8]` ↔ `observability/helpers_test.go`; `observability/alerter.go [186:1-191:2]` ↔ `scripts/external-probe/probe.go [196:1-201:2]`; `ERROR: jscpd found 2 new clones` |
| Sau khi sửa: jscpd sạch, không đổi `.jscpd.json` hay baseline | — | `make ci` (bước jscpd) | `Found 0 clones.` |
| `urlerr.Strip` bỏ URL, giữ lỗi gốc, giữ DeadlineExceeded, giữ nguyên lỗi khác/nil | `TestStripRemovesURL`, `TestStripKeepsDeadline`, `TestStripPassesOtherErrors` | `go test -race ./internal/platform/urlerr/` (đỏ trước: `no non-test Go files`, sau `ok`) | `ok …/urlerr` |
| external-probe chỉ nhập thư viện chuẩn + `urlerr` | `TestExternalProbeStdlibOnly` | `go test -race ./tests/arch/...` | `ok …/tests/arch` |
| Hành vi metrics, observability, probe giữ nguyên | cả gói | `make ci` (đã stage, sau gộp main `e863734`) | `ok …/metrics 65.6s`, `ok …/observability 111.2s`, `ok …/scripts/external-probe` |
| Test hàng đợi không còn lệch vì máy bận, tiêu chí giữ nguyên | `TestHandlerErrorRollsBackAndReschedules`, `TestBestEffortConcurrentConversation` | `go test -race -count=3 -run '…' ./internal/platform/queue/`; cả gói trong `make ci` | `ok 7.9s` (3 lần); `ok …/queue 125.8s` |
| Toàn bộ cổng | — | `make ci` | `EXIT=0`: lint `0 issues`, govulncheck `No vulnerabilities found`, jscpd 0, gitleaks `no leaks found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ngoài hai đoạn trùng, sửa hai test hàng đợi có biên thời gian sát mà Task 319 ghi (kế hoạch mục Task 397 dặn "xem
  lại"): không nới tiêu chí, chỉ sửa cách đo.
- `.golangci.yml`: thêm `urlerr.Strip(` vào `wrapcheck.ignore-sigs` (hàm bóc lớp như `errors.Unwrap` đã có trong danh
  sách; bên gọi luôn tự bọc bằng `fmt.Errorf`). Không đổi ngưỡng hay danh sách bỏ qua của jscpd.

## Quyết định thay chủ shop

1. Đoạn trùng `stripURL` (observability ↔ external-probe): gộp vào gói mới `internal/platform/urlerr` (chỉ thư viện
   chuẩn) và cho cả ba nơi (kể cả metrics) dùng. Vì: ba bản cùng mục đích, cùng lý do đổi; external-probe vẫn "chỉ thư
   viện chuẩn" (test kiến trúc mới `TestExternalProbeStdlibOnly` giữ). Phương án khác: thêm vào baseline jscpd (kế
   hoạch cấm), cho external-probe nhập `observability` (kéo pgx, otel vào tệp chạy ngoài VPS), viết lại hàm khác dáng để
   né máy dò (không thật sự gộp). Dễ đảo ngược: một gói 20 dòng.
2. Đoạn trùng test (metrics ↔ observability): dùng lại `testkit.OpenAppDB` ở metrics; giữ `observability.newEnv` (cần
   thêm `adminURL`, `appURL`).
3. Test hàng đợi: `TestHandlerErrorRollsBackAndReschedules` so `run_at` với mốc giờ database trước/sau lượt chạy (đòi
   `run_at ∈ [trước+4 s, sau+6 s]`) thay vì `run_at − now()` lúc đọc ∈ [4, 6] (thời gian từ lúc ghi tới lúc đọc làm lệch
   biên dưới: lần đỏ 3,96 s). `TestBestEffortConcurrentConversation`: 50 tin cùng hội thoại chạy nối nhau, máy bận thì
   cả vòng dài hơn lịch thử lại đầu (4–6 s), tin lỗi bị nhận lại → gọi 2 lần; `runParallelUntilIdle` nay đẩy việc chờ
   thử lại ra 1 giờ giữa hai lượt (lúc không worker nào chạy). Tiêu chí test giữ nguyên.

## Việc phát hiện thêm

1. `imager.TestImager_HangKilledWithinDeadline` đỏ một lần trong `make ci` lúc máy bận (`script giả chưa chạy: stat
   …/pid`), chạy riêng qua 3/3. Không thuộc task này; cùng loại hạn thời gian sát, nên có task xem lại.
2. `make ci` có lúc dừng ở `lint` vì "parallel golangci-lint is running" khi nhiều phiên chạy cùng máy (golangci-lint
   khóa toàn máy); phải chạy lại.

## Reviewer

Lượt 1: ĐẠT. Không phát hiện phải sửa (đã kiểm: bóc URL giữ nguyên ở cả ba nơi; hai test hàng đợi không nới tiêu chí —
`now()` của lệnh dời lịch nằm giữa hai mốc `clock_timestamp()`; đẩy `run_at` chỉ chạm tin cố ý lỗi, worker bỏ qua
`run_at` thì `callsOf != 1` vẫn đỏ; test kiến trúc lọc `.Standard` đúng). Ghi chú không bắt buộc đã sửa: chú thích
`makeDue` bị lệch lên trên `dbNow` (đặt lại chỗ); mục Ảnh hưởng thêm `env.dbNow`.

Lượt 2 (sau sửa chú thích): ĐẠT. Không phát hiện phải sửa; diff mã giống lượt 1 ngoài chỗ chuyển chú thích `makeDue`.
