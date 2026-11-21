# Task 186 — M5-03: RateLimiter có dự phòng trong tiến trình

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 186, M5-03). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** ký hiệu mới trong `internal/platform/cache`: `Limit`, `RateDecision`, `RateLimiter`, `NewRateLimiter`, `Limits`, `DefaultLimits`, `ParseLimits`, `ErrBadLimits`. Không sửa hay đổi tên ký hiệu có sẵn (`Conn`, `Breaker`, `SubjectKey`, `ReadCache` giữ nguyên) → 0 chỗ tham chiếu phải sửa. `httpapi.WebRateLimiter` (Task 188) không đổi; Task 189 nối `cache.RateLimiter` vào nó.
- Gói phụ thuộc ngược (go list): `internal/platform/cache` được nhập bởi `internal/platform/observability`, `internal/wiring/webcache` (và `cmd/crm` qua hai gói đó). Hai gói này không dùng ký hiệu mới; vẫn build/test lại qua ci-task.
- Operation OpenAPI bị chạm, oasdiff: không có (không đổi `openapi.yaml`).
- Tệp khác: `docs/danh-muc-dung-chung.md` (sinh lại bằng `make catalog`, thêm các ký hiệu mới); `internal/platform/cache/export_test.go` (mới, chỉ cho test).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (Valkey, không PostgreSQL).
- Test sẽ chạy: `go test -race ./internal/platform/cache/...` (mới: `ratelimit_test.go`, `ratelimit_property_test.go`, `limits_test.go`; có sẵn: conn, breaker, readcache, acl), gói phụ thuộc ngược qua ci-task. Test thời gian `ratelimit_perf_test.go` (nhãn `perf`) chạy một lần bằng tay.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `cache` (Conn.Do, Breaker, SubjectKey, EntityKey, keyPart); tìm "limiter", "rate" trong `internal/`: có `httpapi.userLimiter` (cửa sổ trượt theo `uuid.UUID`, không trần số chủ thể, nằm trong `httpapi`) và `httpapi.WebRateLimiter` (interface phía dùng của Task 188).
- Dùng lại / viết mới vì: dùng lại `Conn.Do` (hạn 50 ms, cầu dao 5 lỗi/5 s, chỉ số valkey_ms), `keyPart` (kiểm `Name`), hàm `eval` một khóa của `readcache.go`, `SubjectKey` cho chủ thể. Viết mới bộ đếm dự phòng: `userLimiter` là cửa sổ trượt lưu mọi mốc thời gian (tới Max phần tử mỗi chủ thể, 1.200 với web_key) và không có trần chủ thể; kế hoạch đòi cửa sổ cố định cùng cửa sổ với Valkey và trần 100.000 chủ thể, và `cache` không được nhập `httpapi`. Hai bộ đếm khác lý do thay đổi (nhân viên M0 / website M5) nên giữ riêng. LRU dùng `container/list` của thư viện chuẩn.

## Soát thiết kế

Mục 5:
- 5.1–5.3: không áp dụng: không có bảng, không đọc dữ liệu module nào.
- 5.4, 5.5: không áp dụng: không có transaction; `Allow` là lệnh mạng, không gọi trong `RetryTx` (doc của `RateLimiter` ghi rõ).
- 5.6: không áp dụng: Valkey là hạ tầng nội bộ, không phải bên ngoài có `in_doubt`. INCR quá hạn có thể đã chạy ở máy chủ: chỉ làm đếm dư một lượt (chặt hơn), không sai nghiệp vụ.
- 5.7: không áp dụng: khóa không theo tenant (chủ thể là khóa dịch vụ, SĐT băm, IP băm — đều duy nhất toàn hệ thống). Mã gọi muốn tách theo shop thì đưa vào `subjectKey`.
- 5.8: đúng: gói `platform/cache`, module nghiệp vụ không nhập (luật kiến trúc (l)); không `net/http`.
- 5.9: không áp dụng: không có đường ghi HTTP.
- 5.10: đúng: Valkey chỉ điều tiết; giới hạn tần suất là lớp ngoài, giới hạn mỗi khách của flash sale vẫn chốt trong PostgreSQL (threat model M5, FS05).
- 5.11: không thêm thư viện (valkey-go, rapid đã có trong ADR-44).
- 5.12: theo mẫu của `readcache.go` (Task 185): interface của kế hoạch, hàm dựng trả interface có `//nolint:ireturn` kèm lý do, test Valkey thật bằng `testkit.StartValkey`, Valkey ngừng bằng `vk.Stop`.

Mục 6:
- 6.1 Đồng thời: Valkey: một script `INCR` + `PEXPIRE` (chỉ khi khóa chưa có hạn) nguyên tử ở máy chủ → 1.000 goroutine cùng chủ thể, `Max 50` ra đúng 50 (test). Dự phòng: một `sync.Mutex` bao đọc-sửa-ghi của map + danh sách LRU; test `-race` song song.
- 6.2: không áp dụng: không ghi dữ liệu; gọi lại là một lượt đếm mới (đúng nghĩa giới hạn).
- 6.3–6.5: không áp dụng: không có khóa database.
- 6.6, 6.8: không áp dụng: không có việc nền. Tiến trình chết: bộ đếm dự phòng mất (chỉ là điều tiết); khóa Valkey tự hết hạn sau cửa sổ + 1 s.
- 6.7: chiều ngược lại: từ chối trả `RetryAfter` = thời gian tới hết cửa sổ, tối thiểu 1 s (để lớp HTTP đặt `Retry-After`).
- 6.9 Treo: mỗi lệnh hạn 50 ms của `Conn`; cầu dao mở sau 5 lỗi liền → các lời gọi sau không chạm mạng (test kiểm gauge cầu dao và `Degraded`). Số "≤ 60 ms mỗi lời gọi" của kế hoạch đo ở test nhãn `perf` (luật Task 400: đúng/sai không lấy ngưỡng giờ thật).
- 6.10: không áp dụng: không có tiền.
- 6.11: quy luật "số Allowed mỗi cửa sổ ≤ Max (Valkey) / ≤ FallbackMax (dự phòng)" có test rapid 100 lượt (`ratelimit_property_test.go`).
- 6.12 Dữ liệu cá nhân: khóa Valkey là `rl:{name}:{subject}:{windowStartUnix}`; chủ thể là SĐT/IP thì bên gọi truyền kết quả `SubjectKey` (HMAC); test `SCAN rl:*` bằng `crm_admin` không thấy chuỗi gốc. `subjectKey` chỉ nhận `^[A-Za-z0-9_:-]{1,160}$` (không ghép chuỗi tùy ý vào khóa). Chỉ số chỉ mang nhãn tên giới hạn, không mang chủ thể.
- 6.13: không áp dụng: không kiểm quyền (lớp HTTP gọi sau `guard`).

## Quyết định thay chủ shop

1. **Giới hạn mặc định là hàm, không phải biến gói.** Kế hoạch ghi "các hằng `LimitWebKey = Limit{…}`", nhưng Go không có hằng kiểu struct và luật (`gochecknoglobals`, luật Go mục 4) cấm biến gói đổi được. Chọn `type Limits struct{ WebKey, FlashCustomer, FlashIP, WebOrderCustomer Limit }` + `DefaultLimits()` trả đúng bốn số của QĐ23, và `ParseLimits(raw string) (Limits, error)` đọc đè từ JSON (`RATE_LIMITS_JSON`). Phương án khác: bốn hàm `LimitWebKey()`… (đổi lại dễ; cách chọn cho phép đọc đè một chỗ).
2. **Dạng của `RATE_LIMITS_JSON`:** `{"web_key": {"max": 1500, "window_s": 60, "fallback_max": 700}, …}`; chỉ ghi tên cần đè, trường vắng giữ mặc định; tên lạ, trường lạ, số ≤ 0, `fallback_max > max`, cửa sổ ngoài [1 s, 1 giờ] → `ErrBadLimits` (tiến trình không lên, thay vì chạy với số sai). Chuỗi rỗng → mặc định.
3. **Không sửa `internal/platform/config` trong task này.** `config` không nhập được `cache` (`cache` → `metrics` → `config`: vòng nhập), nên kiểm JSON phải ở `cache.ParseLimits`; đọc biến và gọi `ParseLimits` là phần nối ở `cmd/crm`, việc của Task 189 (nối `RateLimiter` thật). Ghi ở "Việc phát hiện thêm". Phương án khác: thêm trường thô `RateLimitsJSON` vào `config` ngay bây giờ (trường chưa ai dùng).
4. **"Không chậm quá 60 ms mỗi lời gọi" tách sang test nhãn `perf`** (`ratelimit_perf_test.go`, `make perf-test`); test đúng/sai kiểm cầu dao đã mở (gauge `valkey_breaker_open{gate}` = 1) và `Degraded`, theo luật Task 400 trong CLAUDE.md.
5. **Limit hay chủ thể sai** (tên ngoài `^[A-Za-z0-9_-]{1,100}$`, `Max`/`FallbackMax` ≤ 0, `FallbackMax > Max`, cửa sổ không phải bội của 1 s hay ngoài [1 s, 1 giờ], chủ thể ngoài `^[A-Za-z0-9_:-]{1,160}$`): `Allow` từ chối (`RetryAfter` 1 s), đếm `ratelimit_denied{invalid}`; không chạm Valkey. Chọn đóng (fail closed) vì đó là lỗi lập trình mà test của bên gọi sẽ thấy ngay; mở cho qua thì một chủ thể sai sẽ thoát mọi giới hạn.
6. **Đồng hồ đi lùi ở bộ đếm dự phòng:** lượt có cửa sổ cũ hơn cửa sổ đang giữ được đếm vào cửa sổ đang giữ (không làm lại từ 0) → không bao giờ cho dư.
7. Chỉ số `ratelimit_degraded` mang nhãn tên giới hạn (đếm mỗi lời gọi đi đường dự phòng), giống `ratelimit_denied`.

## Bằng chứng

Test viết trước, chạy đỏ: `go test ./internal/platform/cache/...` → `undefined: RateLimiter`, `undefined: newRateLimiter` (build failed). Sau khi viết mã, mọi dòng dưới chạy bằng `go test -race -count=1 -v -run 'RateLimit|Limits' ./internal/platform/cache/` (Valkey 9.1 thật trong container riêng của testkit), trừ dòng perf.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Max 5, cửa sổ 1 s: lần 1–5 được, lần 6 từ chối, RetryAfter ≤ 1 s; cửa sổ mới được lại; RetryAfter = tới hết cửa sổ (6,5 s), tối thiểu 1 s; chỉ số `ratelimit_denied` nhãn tên | `TestRateLimiter_ValkeyFixedWindow` | lệnh trên | `--- PASS: TestRateLimiter_ValkeyFixedWindow (0.23s)` |
| Hai chủ thể độc lập | `TestRateLimiter_ValkeyFixedWindow` (svc:b), `TestRateLimiter_FallbackInProcess` (dự phòng) | lệnh trên | PASS |
| 1.000 goroutine cùng chủ thể, Max 50 → đúng 50 Allowed, 0 Degraded | `TestRateLimiter_ParallelExact` | lệnh trên | `--- PASS: TestRateLimiter_ParallelExact (0.26s)` |
| Valkey dừng: FallbackMax 2 → lần 3 từ chối, Degraded, không lỗi; cầu dao mở sau 5 lỗi (gauge `valkey_breaker_open{gate}` = 1); `ratelimit_degraded` = 6 | `TestRateLimiter_FallbackInProcess` | lệnh trên | `--- PASS: TestRateLimiter_FallbackInProcess (0.36s)` |
| Valkey dừng: mỗi lời gọi ≤ 60 ms (200 lời gọi, cả 5 lời gọi trước khi cầu dao mở) | `TestRateLimiter_FallbackCallTime` (nhãn `perf`) | `go test -race -count=1 -tags perf -v -run FallbackCallTime ./internal/platform/cache/` | `lời gọi chậm nhất khi Valkey ngừng: 663.25µs`, `--- PASS` |
| Thuộc tính rapid 100 lượt: Allowed mỗi (chủ thể, cửa sổ) = min(số gọi, Max) có Valkey, = min(số gọi, FallbackMax) dự phòng; RetryAfter trong [1 s, Window] | `TestRateLimiter_PropertyPerWindow` | lệnh trên | `--- PASS: TestRateLimiter_PropertyPerWindow (0.52s)` |
| Khóa `SCAN rl:*` (crm_admin) không chứa SĐT, IP gốc khi chủ thể là `SubjectKey`; dạng `rl:{name}:{subject}:{windowStartUnix}` | `TestRateLimiter_KeysHaveNoPersonalData` | lệnh trên | `--- PASS: TestRateLimiter_KeysHaveNoPersonalData (0.26s)` |
| Khóa có hạn sống = phần còn lại của cửa sổ + 1 s (đồng hồ giả ở ms 300 → hạn 1.700 ms, PTTL trong (1000, 1700]) | `TestRateLimiter_KeyExpires` | lệnh trên; sau sửa vòng 1 thêm `go test -race -count=3 -run KeyExpires ./internal/platform/cache/` | `ok … 2.489s` (3 lần PASS) |
| Dự phòng tối đa 100.000 chủ thể, bỏ cái cũ nhất khi đầy | `TestRateLimiter_FallbackEvictsOldest` (trần 3), `TestRateLimiter_FallbackDefaultCap` (100.001 chủ thể → giữ 100.000) | lệnh trên | PASS (0.00s, 0.50s) |
| Conn nil luôn dự phòng; đồng hồ lùi không làm bộ đếm về 0; tên giới hạn khác tách bộ đếm | `TestRateLimiter_NilConn` | lệnh trên | PASS |
| Limit, chủ thể sai → từ chối, RetryAfter 1 s, `ratelimit_denied{invalid}` (12 ca) | `TestRateLimiter_InvalidInput` | lệnh trên | PASS |
| Hằng QĐ23 (4 giới hạn) | `TestDefaultLimits` | lệnh trên | PASS |
| `RATE_LIMITS_JSON` đè một phần; 12 dạng sai → `ErrBadLimits` | `TestParseLimits` | lệnh trên | PASS |
| Khóa dịch vụ 1.200/phút, yêu cầu 1.201 bị từ chối (threat model M5 dòng D) | `TestRateLimit_ServiceKeyPerMinute` | lệnh trên | `--- PASS: TestRateLimit_ServiceKeyPerMinute (0.51s)` |
| Lint gói | — | `golangci-lint run ./internal/platform/cache/...` (và `--build-tags perf`) | `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Giới hạn mặc định là `DefaultLimits()` trả `Limits{WebKey, FlashCustomer, FlashIP, WebOrderCustomer}` thay cho bốn biến `LimitWebKey`… (Quyết định 1). Bên dùng viết `cache.DefaultLimits().WebKey` hay giá trị từ `ParseLimits`.
- Thêm `ParseLimits`, `ErrBadLimits` (đọc đè `RATE_LIMITS_JSON`, Quyết định 2), `export_test.go` (`NewRateLimiterWithCap` để test trần chủ thể), `limits_test.go`, `ratelimit_perf_test.go` (Quyết định 4).
- Không sửa `config` (Quyết định 3).

## Việc phát hiện thêm

- Task 189 (nối `RateLimiter` thật ở `cmd/crm`): đọc biến tùy chọn `RATE_LIMITS_JSON` (thêm vào `config`, chỉ giữ chuỗi thô) và gọi `cache.ParseLimits`; lỗi `ErrBadLimits` thì tiến trình api không lên. Bộ nối `httpapi.WebRateLimiter.AllowWebKey(ctx, subject)` gọi `RateLimiter.Allow(ctx, limits.WebKey, subject)`.
- Chú thích `internal/httpapi/ratelimit.go` (dòng 21, 27) còn nhắc `LimitWebKey`; Task 189 sửa thành `DefaultLimits().WebKey` khi nối. Các task sau của kế hoạch (dòng 564, 782, 1100) dùng tên `LimitWebKey`, `LimitWebOrderCustomer`, `LimitFlashCustomer`, `LimitFlashIP`: đọc là `Limits.WebKey`… (Quyết định 1).

## Reviewer

Vòng 1: **CẦN SỬA** — một phát hiện: `TestRateLimiter_KeyExpires` lấy `time.Now()` của máy test rồi so với PTTL Valkey đếm lùi theo giờ của nó (bẫy Task 400: hai đồng hồ), hỏng khi lời gọi rơi vào vài ms cuối của giây. Đã sửa: đồng hồ giả cố định `windowBase() + 300 ms`, hạn đặt ra luôn 1.700 ms, kiểm PTTL trong (1000, 1700]. Ghi chú không bắt buộc của reviewer (chú thích `httpapi/ratelimit.go` còn nhắc `LimitWebKey`; tên biến trong các task sau) chép vào "Việc phát hiện thêm"; ghi chú về rapid (một lượt quá 1 s giờ thật) để nguyên: mỗi lượt chạy vài ms, khóa sống ≥ 1 s giờ thật.

Vòng 2: **ĐẠT** — không có phát hiện phải sửa; bản sửa vòng 1 chỉ dùng một đồng hồ (Valkey), cận (1000, 1700] chỉ hỏng khi treo > 700 ms. Ảnh hưởng dự kiến khớp thực tế; 14 dòng bằng chứng đều trỏ tới test kiểm đúng tiêu chí; các quyết định thay chủ shop không trái kế hoạch, spec.

Kết luận: ĐẠT
