# Task 184 — M5-01: Valkey hai tiến trình cho dev; gói platform/cache

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 184, M5-01). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): gói mới `internal/platform/cache` (`Conn`, `Dial`,
  `DialOptions`, `Breaker`, `NewBreaker`, `BreakerOptions`, `ErrUnavailable`, `ErrBadOp`, `ErrBadURL`, `ErrBadKey`,
  `SubjectKey`, `EntityKey`, `Health` cùng `HealthOK|Down|Off`) —
  chưa ai dùng. `config.Config` thêm trường `ValkeyCacheURL`, `ValkeyGateURL`, `CacheKeySecret` (chỉ vai trò api; 10
  gói nhập `config`, không gói nào dựng `Config{…}` theo vị trí nên thêm trường không gãy). `observability.DeepHealth`
  thêm tham số `Valkey` (1 chỗ gọi: `internal/httpapi/handlers_health.go`, sửa một dòng truyền `observability.Valkey{}`);
  `DeepHealthReport` thêm trường `Valkey`. `testkit.StartValkey` mới.
- Tệp hạ tầng: `docker/compose.dev.yml` (dịch vụ `valkey-cache`, `valkey-gate` trong profile `valkey`, volume
  `valkey-gate`), `docker/valkey/{cache.conf,gate.conf,users.acl.tmpl,start.sh}`, `Makefile` (`make dev` bật profile
  `valkey`). `config`: `httpURL` và `valkeyURL` cùng dùng hàm kiểm mới `urlVar` (jscpd báo trùng; câu lỗi của `httpURL`
  giữ nguyên chữ). `go.mod`: thêm `valkey-go` v1.0.79, `valkeyotel` v1.0.79.
- Gói phụ thuộc ngược (go list): `config` có 10 gói nhập; `observability` có 8 gói nhập (chỉ `httpapi` gọi
  `DeepHealth`). `cache` mới, chưa gói nào nhập.
- Operation OpenAPI bị chạm, oasdiff: không operation nào đổi (`healthDeep` giữ nguyên hợp đồng; trường `valkey` của
  JSON thêm ở task nối `cache.Dial` vào `api`, xem "Khác kế hoạch").
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (luật cảnh báo mới đọc `MetricSeries` sẵn có).
- Test sẽ chạy: `go test -race ./internal/platform/cache/... ./internal/platform/config/... ./internal/platform/observability/... ./tests/arch/... ./tests/deploy/... ./internal/httpapi/...` (gói đổi và gói nhập chúng), rồi `scripts/agent/ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có gói nối
  Valkey, cầu dao hay băm khóa (`grep -i 'breaker\|cầu dao\|hmac\|valkey'` không thấy mã Go nào ngoài testdata). Chỉ số
  dùng `metrics.Registry` (`Observe`, `Inc`, `Gauge`); luật cảnh báo dùng `streak`, `MetricSeries` sẵn có trong
  `observability/rules.go`; cấu hình dùng `reader.secret`, `reader.optional` của `config`; testkit dùng `docker` CLI như
  `filestest/store.go`.
- Dùng lại / viết mới vì: cầu dao viết mới (vài chục dòng, thư viện chuẩn; ADR-44 không có thư viện cầu dao, thêm
  thư viện cho việc này trái mục 11). HMAC bằng `crypto/hmac` + `crypto/sha256` chuẩn.

## Soát thiết kế

- 5.1–5.9, 5.12: không áp dụng — không dữ liệu nghiệp vụ, không bảng, không đường ghi, không gọi dịch vụ ngoài
  bằng lệnh; mã nằm trong `internal/platform/cache` (gói danh từ, theo luật mã chung).
- 5.10 (PostgreSQL là nguồn sự thật): gói chỉ là lớp nối; `boundaries_test` thêm luật (l) cấm `internal/modules/**`
  nhập `internal/platform/cache`; ghi chú danh mục "không dùng cho quyết định nghiệp vụ, không gọi trong RetryTx".
  `/health/deep` không đổi `ok` theo Valkey (ngừng là chậm, không hỏng).
- 5.11: valkey-go v1.0.79 và `valkeyotel` nằm trong ADR-44 (spec 07, dòng "Nối Valkey"); không thư viện nào khác.
- 6.1 (đồng thời): `Breaker` giữ trạng thái dưới một mutex; nửa mở cho đúng một lượt thử (cờ `trial`), các lượt khác
  trả `ErrUnavailable`; test chạy song song có `-race`. Kết nối lại lười chỉ một goroutine thử (`TryLock`).
- 6.8, 6.9 (treo): mỗi `Do` có hạn `OpTimeout` 50 ms (test `CLIENT PAUSE`); nối lại lười có hạn quay số riêng
  `dialTimeout` 200 ms và `ConnWriteTimeout` 1 s, chỉ một lượt nối mỗi lúc (`TryLock`, các lượt khác `ErrUnavailable`),
  nên khi Valkey ngừng mỗi lần chỉ một lượt `Do` chậm tới mức đó, rồi cầu dao mở.
- 6.12 (dữ liệu cá nhân): khóa chứa SĐT, IP, email đi qua `SubjectKey` (HMAC-SHA256 bằng `CACHE_KEY_SECRET`); nhãn chỉ
  số chỉ là tên cố định (`name:op`, op kiểm bằng biểu thức); `valkeyotel` không bật `db.statement` (mặc định không ghi
  lệnh và tham số vào span); lỗi trả ra không chứa khóa, test quét log.
- 6.2–6.7, 6.10, 6.11, 6.13: không áp dụng — không database, tiền, hàng đợi, quyền; Valkey không giữ sự thật nên
  không có quy luật nghiệp vụ cần rapid.

## Quyết định thay chủ shop

Chủ shop vắng, giao toàn quyền trong phạm vi kế hoạch và spec. Mỗi mục: chọn gì, vì sao, phương án khác.

1. **`testkit.StartValkey` dựng một container riêng cho mỗi test bằng `docker` CLI, không testcontainers-go.** Vì sao:
   cùng quyết định 1 của task-04 (testcontainers kéo vài chục module); test cần dừng container và `CLIENT PAUSE` nên
   không dùng chung container compose của phiên. Container dùng đúng image (cùng digest), `gate.conf` và ACL dựng từ
   `users.acl.tmpl` như compose; xóa bằng `docker rm -f` khi test xong. Đảo ngược dễ: chỉ đổi `testkit/valkey.go`.
   Phương án khác: testcontainers-go v0.45.0 (ADR-44).
2. **`Dial` không lỗi khi Valkey chưa lên** (chỉ lỗi khi URL sai): kết nối lười, lần nối lỗi tính là một lỗi của cầu
   dao. Vì sao: Valkey ngừng lúc `api` khởi động phải là "chậm đi", không phải `api` không chạy (ADR-39). Phương án
   khác: `Dial` lỗi khi không nối được (api chết theo Valkey — trái ADR-39).
3. **Gauge `valkey_breaker_open` được đặt ở mỗi lần `Do` và mỗi lần đổi trạng thái** (Registry xóa gauge sau mỗi lần
   ghi phút): có lượt gọi thì mỗi phút có giá trị, luật 2 phút đọc được. Không lượt gọi nào thì không có dòng, không
   cảnh báo (không ai bị chậm). Phương án khác: thêm bộ lấy mẫu định kỳ vào `metrics.Flusher` (ngoài danh sách tệp).
4. **ACL thêm `+@connection`** (HELLO, PING, CLIENT SETINFO mà valkey-go gửi khi nối; `-@dangerous` vẫn bỏ CLIENT KILL…).
   Vì sao: thiếu thì valkey-go không nối được bằng người dùng ACL. Phương án khác: liệt kê từng lệnh.
5. **Valkey dev nằm trong profile `valkey`** (`make dev` bật, `make db-up` không): nhiều phiên song song trên một máy
   cùng chạy `make db-up` thì cổng mặc định 6390, 6391 tranh nhau; test không cần compose (testkit dựng container
   riêng). Cổng đổi được bằng `CRM_VALKEY_CACHE_PORT`, `CRM_VALKEY_GATE_PORT`. `mem_limit` 320m / 192m (trần maxmemory
   256mb / 128mb của kế hoạch cộng phần phụ) cho máy colima 4 GB. Phương án khác: không profile (đúng chữ kế hoạch,
   nhưng `make db-up` của phiên thứ hai lỗi cổng).
6. **Dựng ACL bằng `docker/valkey/start.sh`** (tệp thêm ngoài danh sách): bỏ dòng chú thích của mẫu, thay mật khẩu từ
   biến môi trường vào tmpfs `/etc/valkey/users.acl`, rồi chạy `docker-entrypoint.sh valkey-server` (người dùng
   `valkey`, không phải root). Compose và testkit cùng gọi một script nên test kiểm đúng đường của compose.
7. **Cầu dao không tính lượt fn trả `ErrUnavailable`** (goroutine khác đang nối lại, Conn đã đóng): lượt đó không tới
   Valkey. Lỗi của lệnh mà máy chủ trả (khóa không có, NOPERM, WRONGTYPE) không tính; OOM, LOADING, BUSY, MASTERDOWN,
   TRYAGAIN, NOAUTH, quá hạn, lỗi mạng thì tính. Lỗi của lệnh trả nguyên (để `valkey.IsValkeyNil` dùng được).

## Bằng chứng

Lệnh A: `go test -race -count=1 ./internal/platform/cache/... ./internal/platform/config/... ./internal/platform/observability/... ./tests/arch/... ./tests/deploy/...` → mọi gói `ok` (cache 4.2 s, config 1.6 s, observability 10.0 s, arch 48 s, deploy 4.7 s). Lệnh B: cổng kiểm của task (`ci-task.sh`), kết quả ở dòng cuối bảng.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `Dial` bằng crm_cache, `Ping` được, `SET c:x 1` rồi `GET` ra `1`; khóa không có là lỗi nil, không đếm `valkey_errors`; `valkey_ms` nhãn `cache:get` | `cache.TestConn_PingSetGet` | A | ok |
| `CLIENT PAUSE 1000 ALL` (crm_admin) → `Do` lỗi dưới 80 ms, là `ErrUnavailable` + `DeadlineExceeded`; `valkey_errors cache:set` = 1 | `cache.TestConn_OpTimeout` | A, thêm `-count=3` | ok |
| Cầu dao (đồng hồ giả): 5 lỗi liền → lần 6 `ErrUnavailable`, `fn` không gọi; 4,999 s vẫn mở; đủ 5 s đúng một lượt thử; thử hỏng mở lại; thử tốt đóng; gauge 0 → 1 → 0 | `cache.TestBreaker_OpenTrialClose` | A | ok |
| Lỗi xen giữa không cộng dồn | `cache.TestBreaker_SuccessResetsCount` | A | ok |
| Nửa mở: 20 goroutine cùng lúc → đúng 1 lượt thử, 19 `ErrUnavailable` | `cache.TestBreaker_SingleTrial` | A (`-race`) | ok |
| Bên gọi tự hủy ctx không mở cầu dao, không đóng từ lượt thử | `cache.TestBreaker_CallerCancelNeutral` | A | ok |
| `SubjectKey` không chứa SĐT, ổn định, khác theo khóa bí mật, phần băm 32 hex, phân cách kind/value | `cache.TestSubjectKey` | A | ok |
| `EntityKey("c", t, "product", "a b")` lỗi (và rỗng, `:`, `*`, 101 ký tự, có dấu, xuống dòng, tiền tố sai); đúng dạng → `c:<t>:product:<id>` | `cache.TestEntityKey` | A | ok |
| ACL: crm_cache `FLUSHALL`, `CONFIG`, `KEYS` → NOPERM; crm_cache ghi `gt:x`, `rl:x` → NOPERM; crm_gate ghi `c:x`, `g:x` → NOPERM; mỗi người ghi được tiền tố của mình, crm_gate HSET; crm_admin FLUSHALL được; default không vào được | `cache.TestACL` | A | ok |
| `Conn` nil: `Do`, `Ping` → `ErrUnavailable`, `fn` không gọi, không panic; `Health` = off | `cache.TestConn_Nil` | A | ok |
| Valkey ngừng: `Dial` vẫn được, lệnh `ErrUnavailable`, `Health` down, gauge 1 | `cache.TestConn_ServerDown` | A | ok |
| URL, tên, op sai → lỗi không lộ mật khẩu; op sai → `ErrBadOp` | `cache.TestDial_BadInput` | A | ok |
| Lệnh lỗi với khóa từ `SubjectKey` → dòng log không chứa SĐT | `cache.TestConn_ErrorLogHasNoPhone` | A | ok |
| `Load(RoleAPI)` có `VALKEY_CACHE_URL` mà thiếu `CACHE_KEY_SECRET` → lỗi nêu tên; khóa 20 ký tự → lỗi; sai scheme, không người dùng, không host → lỗi nêu tên biến, không lộ mật khẩu; worker, ingest bỏ qua | `config.TestLoadValkey` | A | ok |
| `/health/deep` (mức `DeepHealth`): không nối → off/off; chạy → ok/ok; container dừng → `OK` vẫn true, cache = gate = down | `observability.TestDeepHealthValkey` | A | ok |
| Luật `valkey-down:cache` không có khi mới 1 phút, có (`normal`) khi 2 phút liền, hết khi đóng; `valkey-down:gate` riêng; tiến trình ngừng ghi thì hết | `observability.TestRuleValkeyDown` | A | ok |
| `boundaries_test` với `testdata/boundaries/internal/modules/modcache` → `internal/modules/modcache nhập …/internal/platform/cache: (l)`; ca đơn lẻ (l); mã thật → 0 | `arch.TestBoundariesTestdata`, `TestBoundariesNotCalledModule`, `TestBoundariesRealCode` | A | ok |
| Cổng kiểm của task (build, vet, lint, gen-check, catalog-check, jscpd, gitleaks, oasdiff, test-guard, vuln; test toàn bộ vì đổi go.mod/testkit; test deploy) | mọi gói | B (lần 2, sau khi gộp `urlVar` cho jscpd; lần 1 đỏ ở jscpd và hai cổng so mã sinh vì tệp chưa stage) | `CI-TASK XANH (so với điểm tách nhánh d5cb1124e03b04647e6599e078a0bb65005da16f)` |
| Compose dev dựng được hai tiến trình, healthy | — | `CRM_VALKEY_CACHE_PORT=6584 CRM_VALKEY_GATE_PORT=6585 docker compose -p crm-t184 -f docker/compose.dev.yml up -d --wait valkey-cache valkey-gate` | `Container crm-t184-valkey-cache-1 Healthy`, `…-valkey-gate-1 Healthy` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `Conn.Do` nhận thêm `op string` (`Do(ctx, op, fn)`): nhãn `name:op` của `valkey_ms`, `valkey_errors` cần tên thao
  tác, mà valkey-go không cho biết lệnh trong `fn` nếu không thêm module `valkeyhook` (ngoài ADR-44). `op` phải khớp
  `^[a-z0-9_.-]{1,40}$`. Chỉ các task sau trong cùng gói gọi `Do`.
- `DialOptions` thêm `Tracing bool` (bọc `valkeyotel` khi dấu vết bật), cùng cách `db.PoolOptions.Tracing`.
- Luật kiến trúc mới đặt tên (l), không phải (f): (a)–(k) đã dùng.
- `/health/deep`: báo cáo `observability.DeepHealthReport.Valkey` có `cache`, `gate` (`ok|down|off`), test ở mức
  `DeepHealth`. Trường `valkey` trong JSON (sửa `openapi.yaml`, mã ogen, handler) và việc nối hai `Conn` vào `api` để lại
  cho Task 189 (danh sách tệp của nó có `openapi.yaml`, `server.go`, `cmd/crm/main.go` với `cache.Dial` hai kết nối);
  task này không đụng `openapi`, `internal/httpapi/gen` vì Task 188 đang sửa cùng vùng song song. Log cảnh báo một lần
  khi URL rỗng cũng ở chỗ nối đó (`cmd/crm`).
- `Breaker` có hàm khởi tạo `NewBreaker(name, o, m)` (kế hoạch chỉ ghi `type Breaker`): gauge cần tên và Registry.
- Tách `urlVar` khỏi `httpURL` (cấu trúc) nằm cùng commit với phần thêm `valkeyURL` (hành vi): việc tách chỉ để hai
  hàm không trùng (jscpd), chữ lỗi giữ nguyên, test sẵn có của `httpURL` vẫn qua.

## Việc phát hiện thêm

- Task 189: thêm `valkey: {cache, gate}` vào schema `DeepHealth` của `openapi.yaml`, handler `HealthDeep` truyền hai
  `Conn` (`observability.Valkey{Cache: …, Gate: …}`) thay cho `observability.Valkey{}`.

## Reviewer

Kết luận: ĐẠT (lượt 1, trên phần đã stage gồm 31 tệp, sau khi gộp main và ci-task XANH).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp; không còn chỗ gọi chữ ký cũ của `DeepHealth`; `httpURL` chuyển hẳn sang `urlVar`; không test nào bị
  xóa, tắt, nới; hai thư viện mới nằm trong ADR-44.
- Ghi chú không bắt buộc: (1) câu 6.8 ghi sai hạn quay số — đã sửa câu trong tệp này; (2) `go.mod` để `valkey-go`,
  `valkeyotel` nhãn `// indirect` dù nhập trực tiếp (build, gen-check vẫn qua; không sửa để không đổi mã sau review —
  `go mod tidy` ở lần đổi go.mod sau sẽ chuyển); (3) hạn 30 s của `StartValkey` tính cả lúc kéo image lần đầu; (4)
  `cache.conf` chỉ được kiểm bằng lần dựng compose, testkit dùng `gate.conf` như kế hoạch.
