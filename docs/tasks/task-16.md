# Task 16 — Realtime sau interface RealtimeBus

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**. Gói mới `internal/platform/realtime`: `Signal`,
  `Entity`, `Frame`, `Bus`, `PGHub`, `NewPGHub`, `PGHubOptions`, `ServeSSE`, `SSEOptions`, các sentinel lỗi. Trong
  `httpapi`: thêm trường `Deps.Realtime realtime.Bus` và `Deps.SSE realtime.SSEOptions` (struct `Deps` có 0 chỗ dùng
  ngoài `cmd/crm/main.go:serveHTTP`, `tests/arch/routes_test.go` và test của `httpapi`, đều dựng bằng tên trường nên
  không vỡ). `chain`/`newMux` (không xuất) đổi: route `Stream` đi tới handler SSE thay cho handler ogen. Trong
  `testkit`: thêm `OpenSSE`, `SSEClient`, `(*AccessKeys).Verifier` (dựng bộ kiểm thẻ `cfaccess` thử; `httpapi/auth_test.go`
  chuyển sang dùng nó vì jscpd báo trùng với test của realtime). Không đổi ký hiệu cũ nào.
- Gói phụ thuộc ngược (go list): `httpapi` ← `cmd/crm`, `tests/arch` (test); `realtime` mới ← `httpapi`, `cmd/crm`;
  `testkit` ← test của mọi gói (chỉ thêm tệp).
- Operation OpenAPI bị chạm, oasdiff: `stream` (`GET /api/v1/stream`) đã có trong hợp đồng từ Task 6; chỉ sửa câu
  `summary` (ghi "Task 15" sai thành "Task 16"), không đổi phản hồi → oasdiff không phá vỡ. `make gen` sinh lại.
- Chỗ dùng trên màn hình: chưa có màn hình (Task 17 làm lớp realtime phía console).
- Câu sqlc, bảng, pha migration: thêm `queries/platform/realtime.sql` (`Notify`: `SELECT pg_notify('rt', $1)`,
  `Listen`: `LISTEN rt` nếu sqlc nhận; không thì `LISTEN` nằm trong `*_dynsql.go` có test). Không bảng, không
  migration (`pg_notify`, `LISTEN` không cần quyền riêng).
- Test sẽ chạy: `go test -race ./internal/platform/realtime/... ./internal/httpapi/... ./cmd/crm/... ./tests/arch/...
  ./tests/e2e/...`; `make ci`. Không chạm vùng rủi ro cao (`tenancy`, `queue`, `commands`, `idempotency`, `identity`,
  `guard.go`): chỉ gọi `identity.Can` có sẵn.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db.Backoff`,
  `db.DB`, `metrics.Registry.Gauge`, `observability`, `tenancy`), `identity.Can`/`AuthUser`, `httpapi` (`authUser`,
  `writeError`, `headerWriter.Unwrap` đã chuẩn bị cho Flush của SSE, `Route.Stream` bỏ qua giới hạn tần suất),
  `testkit` (`StartPostgres`, `Staff`, `AccessKeyServer`, `Do`). Tìm `LISTEN`, `pg_notify`, `event-stream`, `Flush`.
- Dùng lại / viết mới vì: dùng lại `identity.Can` (lọc quyền, không viết luật quyền thứ hai), `metrics.Registry.Gauge`,
  câu sqlc cho `pg_notify`, chuỗi middleware có sẵn (auth, guard → 403), `testkit.StartPostgres`/`AccessKeyServer`.
  Viết mới gói `realtime` (chưa có gì làm việc này). Không dùng `db.Backoff`: đó là lùi 20–80 ms có nhiễu cho thử lại
  transaction; nối lại `LISTEN` cần 500 ms → 5 s theo kế hoạch, mục đích khác. Kết nối `LISTEN` là một `pgx.Conn` riêng,
  không phải pool thứ hai (kế hoạch đòi không qua bộ gom; ngân sách ADR-42 đã tính "api 20 cộng 1 kết nối LISTEN").

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: realtime thuộc `platform` (04 mục 3 "realtime"). Không bảng nào.
- 5.2, 5.3: không áp dụng: không đọc, ghi bảng nào.
- 5.4 Nguyên tử: `Publish` chạy `pg_notify` trong `tx` của bên gọi → báo hiệu chỉ phát khi transaction commit, mất khi
  lùi (đúng 04 mục 6 "khi thay đổi commit, database NOTIFY").
- 5.5: báo hiệu là phần của transaction nghiệp vụ (PostgreSQL hoãn tới commit), không gọi gì ra ngoài trong tx.
- 5.6: không áp dụng: không gọi bên ngoài.
- 5.7 Ngữ cảnh shop: mỗi báo hiệu mang `TenantID`; hub chỉ đẩy cho client cùng tenant (test tenant B không nhận của A).
  Kênh `rt` dùng chung database nên lọc tenant ở hub là bắt buộc, không dựa vào RLS.
- 5.8: `realtime` là gói platform, được nhập `net/http` (như `webhooks`); module nghiệp vụ chỉ gọi `Publish` với `tx`.
  Lớp `httpapi` chỉ đọc nhân viên đã xác thực rồi gọi `realtime.ServeSSE`.
- 5.9: không áp dụng: route đọc (`GET`), có `x-permission: realtime.subscribe`.
- 5.10: không áp dụng: không Valkey (interface `Bus` để đổi sang Valkey sau, 04 mục 6).
- 5.11: không thêm thư viện (pgx, net/http chuẩn).
- 5.12: theo mẫu đường đọc (`handlers_me.go`: đọc `AuthUser` từ ctx) và luật SQL qua sqlc.

Mục 6:
- 6.1 Đồng thời: danh sách client trong hub có khóa; mỗi kết nối SSE có hàng chờ riêng có khóa, `send` không chặn
  (client chậm không làm chậm hub). Test 500 client dưới `-race`.
- 6.2: không áp dụng: không ghi. Báo hiệu trùng trong một lô 300 ms gửi một lần (test).
- 6.3, 6.4, 6.5: không áp dụng: không khóa dòng; `pg_notify` chạy trong tx của bên gọi, lỗi tạm do `RetryTx` của bên
  gọi lo.
- 6.6 Lỗi nền: kết nối `LISTEN` rớt → `listening = false`, nhịp tim ngừng, nối lại lùi 500 ms → 5 s, xong thì `resync`
  cho mọi client (spec 04 mục 6). Báo hiệu lỡ trong lúc rớt không cần phát lại (ADR-17).
- 6.7: không áp dụng.
- 6.8 Tiến trình chết: client tự nối lại, nhận `resync` đầu tiên. Tắt êm: `http.Server.Shutdown` không chờ được luồng
  SSE dài, nên `cmd/crm` đóng các luồng SSE qua `RegisterOnShutdown`.
- 6.9 Treo: chờ thông báo có hạn và `Ping` để phát hiện kết nối chết im lặng; mỗi kết nối SSE dừng khi client đóng.
- 6.10: không áp dụng: không có tiền.
- 6.11: không áp dụng: không thuộc vùng rapid bắt buộc.
- 6.12 Dữ liệu cá nhân: báo hiệu chỉ mang tenant, chủ đề, phạm vi, loại và mã đối tượng; doc của `Signal` cấm nội
  dung. Log không ghi payload. JSON > 7.000 byte → lỗi (giới hạn 8.000 byte của `NOTIFY`).
- 6.13 Quyền: route cần `realtime.subscribe` (guard có sẵn → 403); mỗi báo hiệu kiểm thêm `identity.Can(u, Requires,
  Scope)`, `Requires` lạ hoặc rỗng → `Publish` từ chối. Grant chụp lúc nối: thu quyền có hiệu lực ở lần nối lại kế
  tiếp (xem Quyết định thay chủ shop); dữ liệu vẫn lấy qua API có kiểm quyền mỗi lần.

## Bằng chứng

Lệnh chung: `go test -race -count=1 -v ./internal/platform/realtime/` (PostgreSQL dev cổng 5516, dự án compose
`crm-t16`). Lần chạy đầy đủ: `ok … realtime 26.367s`; chạy lặp `-count=5`: `ok … realtime 50.475s` (không chập chờn).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Khung đầu `resync` `{}`; `Publish` `t.hello` → `changed` đúng `topic`, `entity` trong 1 s; tx lùi → không nhận gì trong 1 s | `TestStreamResyncPublishRollback` | lệnh chung | `--- PASS (3.06s)` |
| Tenant B không nhận báo hiệu của A | `TestStreamTenantIsolation` | lệnh chung | `--- PASS (2.58s)` |
| `Requires: conversation.read`, `Scope: page:p2`: chỉ `page:p1` không nhận; `all` nhận; đúng `page:p2` nhận; `conversation.read` chỉ trên p3 không nhận | `TestStreamScopedPermission` | lệnh chung | `--- PASS (2.94s)` |
| Có quyền khác trên `page:p2` nhưng không có `conversation.read` → không nhận | `TestHubFiltersByRequiredPermissionNotScopeOnly` | lệnh chung | `--- PASS (1.52s)` |
| Ba `Publish` giống hệt nhau trong một lô → 1 khung | `TestStreamBatchDedupe` | lệnh chung | `--- PASS (2.05s)` |
| Không quyền `realtime.subscribe` → 403 `forbidden` | `TestStreamForbiddenWithoutSubscribe` | lệnh chung | `--- PASS (1.83s)` |
| `: ping` theo nhịp khi listening | `TestStreamHeaders` | lệnh chung | `--- PASS (2.11s)` |
| JSON > 7.000 byte → `ErrSignalTooLarge`; thiếu tenant, topic, quyền lạ, quyền theo đích thiếu `Scope` → `ErrInvalidSignal` | `TestPublishRejectsBadSignals` | lệnh chung | PASS |
| Kết nối `LISTEN` chết im lặng (proxy nuốt byte, không RST) → `listening = false` trong 3 s (`CheckEvery` 200 ms, `PingTimeout` 500 ms); trước khi sửa: đỏ "vẫn báo listening sau 3 s" | `TestSilentDeadConnectionDetected` | `go test -race -count=3 -run 'Silent\|Reconnect\|BadSignals' ./internal/platform/realtime/` | đỏ trước khi sửa; sau sửa `ok` |
| `pg_terminate_backend` lên kết nối `LISTEN` → `listening = false`, không `: ping`; trong 10 s nhận thêm `resync`; `Publish` sau đó vẫn tới | `TestReconnectResyncsClients` | lệnh chung | `--- PASS (3.56s)` |
| 500 client cùng lúc dưới `-race`, mỗi client nhận đủ một báo hiệu; đóng hết → `clients = 0` | `TestManyClients` | lệnh chung | `--- PASS (3.32s)` |
| Test bắt được lỗi (mutation tay): bỏ lọc trùng → `TestStreamBatchDedupe` đỏ; ping cả khi rớt → `TestReconnectResyncsClients` đỏ (":ping"); bỏ lọc tenant → `TestStreamTenantIsolation` đỏ; bỏ resync khi nối lại → `TestReconnectResyncsClients` đỏ | như trên | sửa tạm bằng `sed`, chạy `-run`, trả lại tệp | cả 4 `FAIL`, sau khi trả lại thì PASS |
| Gói phụ thuộc | cả bộ | `go test -race -count=1 ./internal/platform/realtime/ ./internal/httpapi/... ./cmd/crm/... ./tests/...` | `ok` realtime 15.9s, httpapi 18.7s, arch 8.0s, e2e 26.9s |
| Lint | — | `make lint` | `0 issues.` |
| Toàn bộ cổng | `make ci` (sau khi stage, lần cuối sau reviewer lượt 3) | `make ci OASDIFF_BASE=-base-ref=7923a49` | build, vet, `golangci-lint` `0 issues.`, gen-check, sqlc vet, `go test -race ./...` 24 gói `ok` không `FAIL` (realtime 61.7s), govulncheck "No vulnerabilities found", oasdiff qua, catalog-check qua, jscpd "Found 0 clones"; test-guard đỏ chỉ vì so với `main` mới (xem dòng dưới) |
| test-guard so với `main` đỏ vì các test chỉ `main` mới có (Task 25, 26, 40), không do task này | — | `git diff --cached 7923a49 --diff-filter=D --name-only` (rỗng); `git cat-file -e 7923a49:<tệp>` | không xóa tệp nào; các tệp test-guard nêu không có ở điểm tách nhánh; test duy nhất có sẵn bị đổi là `auth_test.go` (`verifier()` gọi `testkit.Verifier`, cùng Options) |
| Không có bí mật | — | `make gitleaks` | "no leaks found" |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch không ghi chữ ký khởi chạy hub và handler: thêm `NewPGHub(PGHubOptions{URL, Metrics, Logger, MinBackoff,
  MaxBackoff, CheckEvery})`, `(*PGHub).Run(ctx)`, `ServeSSE(w, r, bus, u, SSEOptions{Batch, Heartbeat, MaxPending,
  Stop})`, kiểu `Frame{Event, Data}`. Lớp HTTP: `Deps.Realtime`, `Deps.SSE`, `handlers_stream.go` (route `Stream` đi
  tới handler SSE thay cho ogen), mã lỗi `stream_unavailable` (503) khi chưa nối `Realtime`. `cmd/crm` (api) dựng hub,
  chạy `Run`, đóng luồng SSE khi tắt.
- "Gộp mỗi 300 ms" làm theo kiểu: khung đầu tiên chờ gửi mở một lô, 300 ms sau gửi cả lô (không phải nhịp cố định):
  báo hiệu lẻ trễ tối đa 300 ms như nhau, và test lô đúng mà không phụ thuộc may rủi lúc nhịp rơi.
- `SSEClient.Next` trả khung chú thích với event `":ping"` (kế hoạch không nói cách báo nhịp tim cho test).
- Thêm `PGHubOptions.PingTimeout` (mặc định 5 s) và test `TestSilentDeadConnectionDetected` (reviewer lượt 1).
- `Publish` từ chối quyền theo đích (`conversation.read`) mà thiếu `Scope` (reviewer lượt 1, ghi chú).
- Thêm chỉ số đếm `rt_reconnect`, `rt_bad_payload` cạnh hai gauge của kế hoạch.
- Ca "quyền khác trên `page:p2`" thử thẳng trên hub (`Subscribe` với `AuthUser` dựng tay): M0 chỉ có một quyền theo
  đích (`conversation.read`), nên "quyền khác trên p2" không dựng được bằng grant thật có ý nghĩa; ca thật qua HTTP thử
  bằng nhân viên có nhiều quyền toàn shop và `conversation.read` trên p3.
- Sửa câu `summary` của `stream` trong `openapi.yaml` ("Task 15" → "Task 16") và chú thích `Deps`.

## Quyết định thay chủ shop

1. **Quyền chụp lúc nối.** Hub lọc theo `AuthUser` nạp khi mở luồng; thu quyền hay khóa tài khoản có hiệu lực ở lần
   nối lại kế tiếp. Vì sao: báo hiệu chỉ là "tải lại", dữ liệu vẫn qua API kiểm quyền mỗi lần (spec 04 mục 6, ADR-17);
   nạp quyền mỗi báo hiệu là một truy vấn cho mỗi client mỗi báo hiệu. Phương án khác: luồng tự đóng sau N phút để
   buộc xác thực lại (ghi ở Việc phát hiện thêm).
2. **Lô quá 1.000 khung khác nhau → thay bằng một `resync`.** Client chậm không làm phình bộ nhớ; hub không bao giờ
   chặn vì một client (`send` chỉ thêm vào lô). Phương án khác: bỏ khung mới (mất báo hiệu không báo ai).
3. **`resync` cho mọi client sau mỗi lần `LISTEN` được, kể cả lần đầu**: client nối trước khi hub nghe được có thể lỡ
   báo hiệu. Thừa một `resync` là vô hại.
4. **Phát hiện kết nối chết im lặng**: không có thông báo trong 15 s thì `Ping` có hạn 5 s; hỏng hay quá hạn → coi như
   rớt. Hạn chờ chỉ đặt
   hạn đọc trên socket (`DeadlineContextWatcherHandler`), không gửi yêu cầu hủy tới PostgreSQL mỗi 15 s.
5. **Hạn ghi 10 s mỗi lần ghi SSE** (`ResponseController.SetWriteDeadline`): client treo không giữ goroutine mãi.
6. **Tắt êm**: `http.Server.Shutdown` không cắt luồng dài nên chờ hết 10 s rồi báo lỗi; `cmd/crm` đóng mọi luồng SSE
   qua `RegisterOnShutdown`, client tự nối lại (có `resync`).
7. **`LISTEN` qua sqlc** (`queries/platform/realtime.sql`) như `pg_notify`, không cần tệp `*_dynsql.go`.

## Việc phát hiện thêm

- Luồng SSE không kiểm lại thẻ Access hay trạng thái tài khoản trong lúc mở (Quyết định 1). Nên cân nhắc giới hạn
  đời luồng (ví dụ 15 phút rồi đóng, client tự nối lại và xác thực lại) khi làm Task 17 hoặc M1.
- Cảnh báo "kết nối LISTEN rớt" (04 mục 8) đọc gauge `rt_listening`: thêm vào luật cảnh báo ở task cảnh báo.
- `TestRecordAfterLongExecute` (gói `commands`, task này không chạm) đỏ một lần trong `make ci` lần 1 khi máy bận
  (`in_doubt` thay vì `done`), chạy riêng `-count=3` thì qua: test phụ thuộc thời gian, dễ chập chờn khi máy tải nặng.
- Hub: `pgx.ConnectConfig` và `LISTEN` chưa có hạn riêng; nên đặt `ConnectTimeout` (ví dụ 5 s) để hồi phục nhanh khi
  mạng mất im lặng đúng lúc nối lại (reviewer lượt 2, không ảnh hưởng tính đúng).
- `make ci` so oasdiff với `main` đỏ vì `main` đã có route `identity` của Task 25, 26, 40 (nhánh này tách trước đó):
  không phải thay đổi của task này. So với điểm tách nhánh `7923a49` thì qua. Gộp nhánh sẽ hết.
- `TestLintCatchesPlantedMistakes` đỏ một lần vì "parallel golangci-lint is running" (phiên khác trên máy giữ khóa);
  chạy lại thì qua.
- Kiểm HTTP/2 và không bị giữ lại (buffering) qua Cloudflare, Traefik là việc ngoài máy (spec 04 mục 6), chưa làm.

## Reviewer

Kết luận: ĐẠT

- Lượt 1: CẦN SỬA. (1) `Ping` sau 15 s không có hạn → kết nối chết im lặng giữ `listening = true` tới khi TCP tự bỏ
  → đã sửa (`PingTimeout` 5 s) và thêm test đỏ-rồi-xanh `TestSilentDeadConnectionDetected`. (2) Danh mục dùng chung
  ghi "CẦN GHI CHÚ" cho `realtime` → thêm dòng vào `docs/danh-muc-ghi-chu.yaml`, `make catalog`. Ghi chú đã làm:
  gauge `rt_listening` chỉ ghi trong `setListening` (không bị ghi đè đồng thời); `Publish` từ chối quyền theo đích
  thiếu `Scope`. Ghi chú không làm: `blockReconnect` thu CONNECT của PUBLIC (mỗi test một database riêng, vô hại);
  tên `TestStreamHeaders` chỉ kiểm ping (Content-Type kiểm trong `OpenSSE`).
- Lượt 2: ĐẠT, không phát hiện phải sửa. Ghi chú không làm (chép vào Việc phát hiện thêm): `ConnectConfig`, `Listen`
  không có hạn riêng (chậm hồi phục khi mạng mất đúng lúc nối lại, `listening = false` vẫn báo đúng); `OpenSSE` gọi
  `t.Fatalf` trong goroutine phụ của `TestManyClients` (chỉ ảnh hưởng đường báo lỗi).
- Sau lượt 2: `make ci` đỏ ở jscpd toàn repo (đoạn dựng `cfaccess.New` trong `env_test.go` trùng `httpapi/auth_test.go`)
  → gộp thành `testkit.(*AccessKeys).Verifier`, cả hai test dùng nó. Cần review lại phần này.
- Lượt 3 (toàn bộ phần stage, sau khi gộp `Verifier`): ĐẠT, không phát hiện phải sửa; không vòng nhập gói
  (`cfaccess_test` là gói ngoài), không vi phạm luật ranh giới, hành vi test auth không đổi.

## Lượt 2 (gộp main)

Việc: `git merge main` vào `task-16` (không rebase) sau khi `main` nhận Task 314, 25, 26, 40, 49. Điểm tách nhánh
`7923a49`; main lúc gộp `badcaae`.

### Ảnh hưởng (lượt 2)

- Tệp cả hai bên cùng đổi: `docs/danh-muc-ghi-chu.yaml` (xung đột), `docs/danh-muc-dung-chung.md`,
  `openapi/openapi.yaml`, `internal/httpapi/errors.go`, mã sinh `internal/httpapi/gen/{oas_handlers,oas_router,
  oas_server,oas_unimplemented}_gen.go` (git tự trộn). Các tệp khác chỉ một bên đổi.
- Ký hiệu Go xuất ra ngoài: không đổi thêm gì so với lượt 1; phần của main (identity PQ07, queue `OrderMode`, gói
  `internal/queries`) vào nguyên vẹn. `errors.go` giữ cả `codeStreamUnavailable` (task 16) và `identityError` + mã
  PQ07 (Task 26).
- Operation OpenAPI: hợp đồng sau gộp = route nhân viên, vai trò của main + `stream` (chỉ sửa `summary`). Mã sinh
  không sửa tay: `make gen` trên `openapi.yaml` đã trộn, so `git diff` → không chênh với bản git tự trộn.
- Chỗ dùng trên màn hình, câu sqlc, migration: không thêm gì; migration của Task 25 vào từ main, không sửa tệp cũ.
- Test sẽ chạy: `make ci` toàn bộ (gộp chạm nhiều gói, có vùng rủi ro cao `queue`, `identity`, `tenancy` từ main).

### Tái dùng (lượt 2)

- Không viết mã mới. Giải xung đột danh mục: giữ cả dòng `internal/platform/realtime` (task 16) và
  `internal/queries` (Task 25); `make catalog` sinh lại `danh-muc-dung-chung.md`.

### Soát thiết kế (lượt 2)

- Chỉ áp dụng phần trộn: `errors.go` vẫn là một chỗ đổi lỗi sang HTTP; `stream_unavailable` và mã identity không
  trùng nhau, `identityError` chạy trước `switch` không bắt lỗi của realtime (realtime không trả sentinel identity).
- Ngân sách kết nối (ADR-42): main không đổi `poolOptions`; kết nối `LISTEN` riêng của api vẫn là +1 như lượt 1.
- Các mục 5.x, 6.x khác: không áp dụng (không có mã mới; mã của main đã được review ở task của nó).

### Quyết định thay chủ shop (lượt 2)

1. **Cổng compose khi chạy `make ci`**: 8333 bị dự án khác chiếm, nên dựng `crm-t16` với `CRM_S3_PORT=8516`,
   `CRM_S3_OFFSITE_PORT=8517` (Makefile, compose đã có biến này). Chỉ ảnh hưởng máy này, đảo ngược bằng `down -v`.

### Bằng chứng (lượt 2)

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Không còn dấu xung đột | — | `grep -c '<<<<' docs/danh-muc-ghi-chu.yaml` | `0` |
| Mã sinh khớp nguồn đã trộn | bước gen của `make ci` | `make gen` rồi `git diff --exit-code -- internal` | không chênh |
| Realtime vẫn đúng sau gộp | gói `realtime`, `httpapi` | `make ci` (`go test -race ./...`) | `ok .../internal/platform/realtime 39.268s`, `ok .../internal/httpapi 64.839s` |
| Phần của main vẫn đúng | gói `identity`, `queue`, `tests/arch`, `tests/e2e` | `make ci` | `ok .../identity 107.752s`, `ok .../queue 108.348s`, `ok .../tests/arch 12.286s`, `ok .../tests/e2e 43.591s` |
| Lint, hợp đồng, danh mục, trùng, test-guard, bí mật | cổng của `make ci` | `make ci` | `0 issues.`; apicheck so `main` qua; danh mục không chênh; jscpd `Found 0 clones`; test-guard qua; gitleaks `no leaks found`; exit 0, 0 dòng `FAIL` |

### Reviewer (lượt 2)

Kết luận: ĐẠT, không phát hiện phải sửa. Phần task-16 mang vào so với `main` đúng 24 tệp của lượt 1, không còn dấu
xung đột; `openapi.yaml` và mã sinh chỉ khác `summary` của `stream`; `errors.go` chỉ thêm `codeStreamUnavailable`
(handler SSE gọi thẳng `writeError`, không qua `identityError`); `routes_gen.go` như main, chỉ `stream` có
`Stream: true`, route nhân viên mới đi ogen; `realtime` nhập `identity` hợp luật (c) của `boundaries_test.go`;
`realtime.sql` không đọc bảng hay view nên không thuộc luật view của Task 25. Ghi chú: dọn compose `crm-t16` khi xong.
