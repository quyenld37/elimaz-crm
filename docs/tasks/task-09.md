# Task 09 — Khóa chống trùng cho mọi yêu cầu ghi (ADR-41), ngoại lệ x-idempotency (ADR-47)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md` (Task M0-09). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `httpapi.Route` (mã sinh, tools/routegen): **thêm** hai trường `IdempotencyMode`, `IdempotencyReason`. 8 chỗ
    dựng `Route{…}` trong `auth_test.go` (4), `middleware_test.go` (4) không khai trường mới nên có giá trị `""`:
    middleware chống trùng chỉ chạy khi `Write && mode ∈ {required, no-replay}`, nên các test cũ giữ nguyên hành vi.
    `tests/arch/routes_test.go`, `cmd/crm` (qua `NewServer`) đọc `Routes`; không chỗ nào so sánh cả struct.
  - `chain` (không xuất, 9 chỗ gọi trong gói `httpapi`): thêm bước `idempotency` sau `userRateLimit`.
  - `writeHandlerError` (không xuất, 4 chỗ gọi): thêm ba nhánh: `db.ErrRetryLater` → 503 `retry_later` kèm
    `Retry-After: 1`; `idempotency.ErrClaimLost` → 409 `request_in_progress`; các nhánh cũ không đổi.
  - Ký hiệu mới: gói `internal/platform/idempotency` (`Scope`, `Stored`, `Claim`, `Acquire` (kế hoạch gọi là `Begin`, xem Khác kế hoạch), `RequestHash`,
    `ErrInProgress`, `ErrKeyReused`, `ErrClaimLost`), trong `httpapi`: `WriteTx`, `ErrNoClaim`; trong `testkit`:
    `Staff.ProbeCounter` (bảng thử `probe.counter` dùng chung cho test chống trùng của `idempotency` và `httpapi`,
    thêm để gỡ đoạn trùng jscpd; chỉ dùng trong test).
- Gói phụ thuộc ngược (go list): `internal/httpapi` được `cmd/crm`, `tests/arch` nhập (và test e2e qua tệp chạy).
  `internal/platform/tenancy` không đổi mã, chỉ thêm ca test (`schemarules_test.go`). `tools/routegen` là lệnh, không
  gói nào nhập; đầu ra `routes_gen.go` sinh lại bằng `make gen`. Gói mới `idempotency` chỉ được `httpapi` nhập.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml` (M0 không có operation `exempt`/`no-replay`). Hai
  operation ghi `abandonJob`, `retryJob` có thêm `IdempotencyMode: "required"` trong bảng route; oasdiff: không đổi.
- Chỗ dùng trên màn hình: chưa có màn hình (Task 17). Màn hình sẽ phải gửi `Idempotency-Key` (đã là tham số bắt
  buộc của hai operation ghi từ Task 6).
- Câu sqlc, bảng, pha migration: **mở rộng**. `migrations/0005_idempotency.sql` tạo `platform.idempotency_keys`
  (đã có trong bảng thứ tự khóa `docs/adr/0046-thu-tu-khoa.md`, nhóm 13, luôn cuối transaction), RLS bằng
  `platform.enable_tenant_rls`, `crm_app` được `SELECT, INSERT, UPDATE, DELETE`. Câu mới
  `queries/platform/idempotency.sql` (khối sqlc `queries/platform` có sẵn → `internal/platform/internal/store`).
- Test sẽ chạy: vùng rủi ro cao (`internal/platform/idempotency`) nên chạy cả bộ của vùng:
  `go test -race ./internal/platform/idempotency/... ./internal/httpapi/... ./internal/platform/tenancy/...
  ./tools/routegen/... ./tests/arch/... ./tests/e2e/... ./cmd/...` trên dự án compose `crm-t09` (cổng 5509); cuối
  cùng `make ci`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có `tenancy.WithTenant`,
  `db.RetryTx`, `db.TxAPI`, `db.ErrRetryLater`, `db.TransientCode`, `testkit.OpenStaff`, `testkit.AccessKeyServer`.
  Tìm `idempot`, `Idempotency`, `sha256`, `canonical`, `Retry-After`, `replay` trong `internal/`, `cmd/`, `tools/`:
  chỉ có chú thích hẹn Task 9 (`server.go`, `db/retry.go`) và một câu mẫu trong testdata của `sqlusage`.
- Dùng lại / viết mới vì:
  - Mọi transaction (bước ghi nhận khóa, `Release`, transaction nghiệp vụ của `WriteTx`) đi qua
    `tenancy.WithTenant` → `db.RetryTx` (thử lại của ADR-46 có sẵn, không tự viết vòng lặp).
  - Lỗi HTTP: thêm mã vào `errors.go` (chỗ duy nhất đổi lỗi sang mã HTTP), dùng lại `writeError`,
    `writeHandlerError`.
  - Nhân viên và tenant của phạm vi khóa: `authUser(ctx)` của Task 7 (không đọc lại database).
  - Mẫu header `^[A-Za-z0-9_-]{1,128}$`: trùng chữ với `requestIDPattern` của `middleware.go` nhưng khác mục đích và
    khác lý do thay đổi (ADR-41 cho khóa; x-request-id là luật log). Giữ hai hằng riêng, theo luật "chỉ gộp khi cùng
    mục đích và cùng lý do thay đổi".
  - Băm: `crypto/sha256`, chuẩn hóa JSON bằng `encoding/json` (`UseNumber`, khóa map được xếp khi mã hóa lại);
    không cần thư viện ngoài.
  - Test: rapid (đã có trong ADR-44, dùng ở Task 5, 7), testkit có sẵn; route thử `_probe` dựng bằng `chain` như
    `auth_test.go`.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: khóa chống trùng là việc của nền tảng (04 mục 5.5, ADR-41): bảng `platform.idempotency_keys`, mã
   trong `internal/platform/idempotency`.
2. Không ghi bảng module khác: gói chỉ ghi `platform.idempotency_keys`. Bảng thử của test nằm trong schema `probe`
   tạo trong database thử.
3. Đọc dữ liệu module khác: không áp dụng: không đọc bảng module nào.
4. Việc nguyên tử: dấu "đã xong" và kết quả ghi **trong transaction nghiệp vụ** (`Claim.Complete(ctx, tx, …)` gọi từ
   `WriteTx` trong cùng `fn`), ADR-41. Bước ghi nhận khóa là transaction ngắn riêng, commit trước (ADR-41 bảng).
5. Việc sau commit: không áp dụng: không phát sự kiện.
6. Gọi ra bên ngoài: không áp dụng; `WriteTx` ghi rõ `fn` không gọi mạng (chạy tới 4 lần).
7. Ngữ cảnh shop: bảng có `tenant_id`, khóa chính `(tenant_id, actor, route, key)` bắt đầu bằng `tenant_id`, RLS ép
   bằng `platform.enable_tenant_rls`; mọi câu chạy trong `tenancy.WithTenant`. `CheckSchemaRules` kiểm bảng này
   (không đưa vào danh sách bảng nền tảng không RLS, xem Quyết định 1).
8. Nghiệp vụ không biết HTTP: gói `idempotency` không nhập `net/http`; mã HTTP, header nằm ở `httpapi/idempotency.go`
   và `errors.go`.
9. Đường ghi: đây là task dựng luật này. Mọi route ghi `required` (mặc định) đi qua middleware; `exempt`/`no-replay`
   chỉ qua cờ trong `openapi.yaml` có lý do, `routegen` và test kiến trúc chặn khai sai.
10. PostgreSQL là nguồn sự thật: khóa nằm trong PostgreSQL, cùng transaction với kết quả (ADR-41 "Đã cân nhắc").
11. Thư viện: không thêm thư viện (rapid, pgx, uuid đã có).
12. Module mẫu: gói nền tảng theo mẫu `tenancy`/`db`: câu SQL trong `queries/platform`, sqlc, test gói `_test` trên
    PostgreSQL thật.

Mục 6 (đúng khi chạy đồng thời):

1. Hai yêu cầu cùng khóa cùng lúc: `INSERT … ON CONFLICT DO NOTHING` (một bên thắng); bên thua đọc dòng bằng câu
   **thứ hai** `SELECT … FOR UPDATE` (READ COMMITTED: câu mới có ảnh chụp mới nên thấy dòng vừa commit; không đọc
   chung một câu với INSERT vì ảnh chụp của câu đó có thể không thấy dòng của bên kia). Nhận lại dòng `running` quá
   hạn bằng `UPDATE … WHERE status = 'running' AND locked_until < now` dưới khóa dòng. `Complete` và `Release` có
   điều kiện `status = 'running' AND locked_until = <hạn của mình>` (hạn giữ chỗ dùng làm thẻ chủ: bên nhận lại đặt
   hạn mới lớn hơn), nên yêu cầu cũ chậm (không chết) không thể ghi "xong" đè lên bên đã nhận lại: `Complete` 0 dòng →
   `ErrClaimLost` → transaction nghiệp vụ lùi → 409.
2. Gửi lại: đây là cơ chế của mục này (ADR-41); test 50 yêu cầu song song.
3. Thứ tự khóa: `idempotency_keys` nhóm 13, luôn cuối transaction nghiệp vụ (`Complete` là câu cuối của `fn`); bước
   ghi nhận khóa là transaction riêng chỉ chạm một dòng.
4. Lỗi tạm: `WithTenant` chạy lại tới 3 lần; hết lượt (`db.ErrRetryLater`) → lùi, `Release`, 503 `retry_later` kèm
   `Retry-After: 1`, không lưu (ADR-41, 46). Bước ghi nhận khóa hết lượt cũng 503.
5. Mức cô lập: READ COMMITTED (mặc định của RetryTx), câu có điều kiện và `FOR UPDATE`.
6. Việc nền lỗi: không áp dụng (dọn khóa hết hạn là Task 13).
7. Bên ngoài 429: không áp dụng.
8. Tiến trình chết: dòng `running` còn lại; sau 60 s (`locked_until`) yêu cầu mới nhận lại; an toàn vì nếu lần trước
   đã ghi gì thì dòng đã `done` cùng transaction (test "tiến trình chết").
9. Treo lâu: các transaction dùng `db.TxAPI` (khóa 1 s, câu 5 s). `Release` chạy trên ctx tách khỏi việc hủy của
   yêu cầu, có hạn 5 s.
10. Tiền: không áp dụng.
11. Quy luật: rapid 100 lượt: số lần bộ đếm tăng bằng đúng số khóa có kết quả cuối, mỗi khóa tăng tối đa 1 lần.
12. Dữ liệu cá nhân: `response_body` là thân câu trả lời của handler (spec 05: "chỉ phần cần để trả lại"; handler
    chịu trách nhiệm không trả dữ liệu cá nhân thừa); thân yêu cầu chỉ lưu dưới dạng sha256. Chế độ `no-replay` không
    lưu thân. Không log khóa, thân, mã băm.
13. Quyền: middleware chạy sau `auth`, `guard`, `userRateLimit` (yêu cầu bị từ chối quyền không tạo dòng khóa).
    Phạm vi khóa gồm tenant và người gọi, nên người khác không đọc được kết quả của nhau (test hai người dùng).

## Quyết định thay chủ shop

1. **`platform.idempotency_keys` không thêm vào danh sách "bảng nền tảng không RLS" của `schemarules_test.go`.**
   Ghi chú giao việc bảo thêm, nhưng danh sách đó là danh sách **miễn** kiểm RLS; kế hoạch (Global Constraints) và
   ADR-41 nói bảng này có RLS. Thêm vào là tắt kiểm RLS của chính bảng cần RLS. Thay vào đó thêm test: bảng có mặt,
   qua kiểm, và bỏ `FORCE` thì `CheckSchemaRules` báo đúng bảng này. Phương án khác: thêm vào danh sách miễn (yếu hơn).
   Đảo ngược: một dòng trong test.
2. **Phạm vi `route` của khóa là mẫu route (`rt.Pattern`); mã băm gồm method, đường dẫn thật (kèm query) và thân
   chuẩn hóa.** Cùng khóa cho hai đích khác nhau của cùng route (thử lại việc A rồi việc B) → 422 thay vì phát lại
   kết quả của A. Phương án khác: `route` là đường dẫn thật (hai kết quả độc lập); cũng đúng nhưng che lỗi dùng lại
   khóa của client.
3. **Người gọi (`actor`) là `user:<mã nhân viên>`**; route ghi `required` không có nhân viên trong ctx (route công
   khai) → 500 và log lỗi lập trình, vì không dựng được phạm vi (ADR-47: route công khai ghi phải `exempt`). M0 không
   có route như vậy; M5 thêm `service_key:<mã>`.
4. **Mẫu sai của header → 400 `idempotency_key_invalid`** (kế hoạch chỉ nói 400, không nêu mã).
5. **Handler trả lời mà không gọi `WriteTx`** (ví dụ ogen trả 400 vì thân sai, handler từ chối theo đích): middleware
   `Release` dòng khóa khi handler xong, để lần gửi lại chạy từ đầu thay vì 409 trong 60 s. Đã gọi `WriteTx` mà `fn`
   lỗi thì dòng giữ `running` đúng như kế hoạch. Lý do an toàn: `Release` chỉ xóa dòng `running` có đúng hạn của
   mình. Phương án khác: để dòng tới hết hạn (khách bị 409 oan 60 s).
6. **`response_body` là `jsonb`** như kế hoạch: lần phát lại trả JSON tương đương (PostgreSQL chuẩn hóa khoảng trắng,
   thứ tự khóa), không đúng từng byte; test so theo nội dung JSON. Handler có thân rỗng (`body == nil`) lưu NULL.
7. **Khóa `done` đã quá `expires_at` mà chưa bị dọn** được coi như khóa mới (ghi đè), vì ADR-41 chỉ giữ 72 giờ.
9. **Nhánh task-09 tiến nhanh (fast-forward) tới `main` (`a54898e`, đã có Task 08, migration 0004) trước khi
   commit.** `test-guard` của `make ci` so cây làm việc với `main` nên coi hai tệp test của Task 08 là "bị xóa"; nhánh
   chưa có commit riêng nên tiến nhanh không đổi lịch sử. Migration 0005 chạy sau 0004 đúng thứ tự gộp của kế hoạch
   (Q-X7). Hai tệp `models.go` sinh lại bằng `make gen`. Phương án khác: ghi hai tệp vào "Test bị xóa" (sai sự thật).
8. **`Complete` không khớp thẻ chủ** → `ErrClaimLost`, đổi thành 409 `request_in_progress` (một yêu cầu khác đã nhận
   lại khóa).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 50 yêu cầu song song cùng khóa, cùng thân → bộ đếm tăng đúng 1; mỗi trả lời 200 cùng thân hoặc 409 | `TestIdempotencyFiftyParallelSameKey` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency` | `--- PASS: TestIdempotencyFiftyParallelSameKey (0.38s)` |
| Thiếu khóa → 400 `idempotency_key_required`; sai mẫu (129 ký tự, ký tự lạ, ngoài ASCII) → 400 `idempotency_key_invalid`; 128 ký tự chạy | `TestIdempotencyHeaderRules` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency` | `--- PASS` |
| Cùng khóa, thân khác → 422 `idempotency_key_reused` | `TestIdempotencySameKeyDifferentBody422`, `TestAcquireDifferentBodyIsKeyReused` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency`; `go test -race -count=1 -v ./internal/platform/idempotency/` | `--- PASS` cả hai |
| Khóa `done` gửi lại sau 1 giờ → kết quả cũ, `Idempotent-Replayed: true`, bộ đếm không tăng | `TestIdempotencyReplayAfterOneHour`, `TestAcquireNewKeyCompleteThenReplay` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency`; `go test -race -count=1 -v ./internal/platform/idempotency/` | `--- PASS` cả hai |
| Tiến trình chết sau bước ghi nhận khóa (running, `locked_until` đã qua) → chạy được, bộ đếm tăng đúng 1; running còn hạn → 409 | `TestIdempotencyDeadProcessReclaimedAfterLock`, `TestAcquireReclaimsStaleRunning`, `TestAcquireRunningIsInProgress` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency`; `go test -race -count=1 -v ./internal/platform/idempotency/` | `--- PASS` cả ba |
| Claim cũ (bị nhận lại) không ghi "xong", không xóa dòng mới; transaction của nó lùi | `TestAcquireReclaimsStaleRunning` | `go test -race -count=1 -v ./internal/platform/idempotency/` | `--- PASS: TestAcquireReclaimsStaleRunning (0.15s)` |
| "Thử lại" (`final = false`) → không còn dòng khóa; gửi lại chạy lại từ đầu | `TestIdempotencyRetryAnswerNotStored`, `TestReleaseDeletesRowSoKeyRunsAgain` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency`; `go test -race -count=1 -v ./internal/platform/idempotency/` | `--- PASS` cả hai |
| `db.ErrRetryLater` → 503 `retry_later`, `Retry-After: 1`, không lưu | `TestIdempotencyRetryLaterNotStored` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency` | `--- PASS` |
| Handler lỗi giữa transaction → bộ đếm không đổi, dòng vẫn running, lần sau 409 | `TestIdempotencyHandlerErrorKeepsRunning` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency` | `--- PASS` |
| Cùng khóa ở hai người dùng → hai kết quả độc lập; tenant B không thấy dòng của A | `TestIdempotencyTwoUsersIndependent`, `TestScopesAreIndependent` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency`; `go test -race -count=1 -v ./internal/platform/idempotency/` | `--- PASS` cả hai |
| Thuộc tính rapid 100 lượt: số lần bộ đếm tăng = số khóa có kết quả cuối | `TestPropertyCounterMatchesFinalKeys` | `go test -race -count=1 -v ./internal/platform/idempotency/` | `[rapid] OK, passed 100 tests` |
| 20 lần `Acquire` song song cùng khóa → đúng 1 Claim, 19 `ErrInProgress` | `TestAcquireConcurrentSameKeyOneClaim` | `go test -race -count=1 -v ./internal/platform/idempotency/` (và `-count=5`) | `--- PASS` |
| Mã băm: chuẩn hóa JSON, khác method/đường dẫn/thân thì khác | `TestRequestHash` | `go test -race -count=1 -v ./internal/platform/idempotency/` | `--- PASS` |
| Thân nil → `response_body` NULL; khóa hết 72 giờ chưa dọn → khóa mới | `TestCompleteNilBodyStoresNull`, `TestExpiredDoneIsNewKey` | `go test -race -count=1 -v ./internal/platform/idempotency/` | `--- PASS` cả hai |
| `no-replay`: lần hai cùng khóa cùng thân → 409 `already_processed`, `response_body` NULL, không lộ bí mật; khác thân 422; thiếu khóa 400 | `TestIdempotencyNoReplay` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency` | `--- PASS` |
| `exempt`: không header 200; cùng khóa thân khác → cả hai 200 theo thân, không phát lại; 0 dòng khóa; route required bên cạnh 400; thiếu Origin 403; `WriteTx` → 500, `ErrNoClaim` trong log | `TestIdempotencyExempt` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency` | `--- PASS` |
| Từ chối trước `WriteTx` → khóa được trả lại; bị guard chặn → không tạo dòng | `TestIdempotencyUnusedClaimReleased`, `TestIdempotencyAfterGuard` | `go test -race -count=1 -v ./internal/httpapi/ -run Idempotency` | `--- PASS` cả hai |
| `routegen`: 6 tệp sai → thoát khác 0 và nêu `operationId`; `exempt-ok`, `noreplay-ok` → Route mang cờ và lý do; `openapi.yaml` thật → mọi route ghi `required` | `TestRejectsBadContracts`, `TestGeneratesIdempotencyModes`, `TestRealContractWritesRequired`, `TestGeneratesRoutes` | `go test -count=1 -v ./tools/routegen/` | `ok elimaz.com.vn/crm/tools/routegen` |
| Test kiến trúc: route exempt/no-replay có lý do và là POST; route ghi khác `required`; GET `""`; bảng giả báo `operationId` | `TestRoutesIdempotencyModes` | `go test -race -v ./tests/arch/ -run RoutesIdempotency` | `--- PASS: TestRoutesIdempotencyModes` |
| `idempotency_keys` có RLS, qua kiểm schema, bỏ FORCE thì bị báo | `TestCheckSchemaRulesCoversIdempotencyKeys` | `go test -race -v ./internal/platform/tenancy/ -run CoversIdem` | `--- PASS` |
| Không mở transaction ngoài `db.RetryTx` | `TestTxUsageProductCode` | `go test -race ./tests/arch/` | `--- PASS` (sau khi đổi `Begin` → `Acquire`) |
| Toàn bộ cổng kiểm: build, vet, lint, `gen-check` (mã sinh khớp), `sqlc vet`, `go test -race ./...`, govulncheck, oasdiff, danh mục, jscpd, test-guard, gitleaks | — | `make ci` (dự án compose crm-t09, cổng 5509) | `EXIT 0`; mọi gói `ok`; `jscpd: Found 0 clones`; `no leaks found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

1. **`idempotency.Begin` đổi tên thành `idempotency.Acquire`** (cùng chữ ký, cùng hành vi). Test kiến trúc
   `TestTxUsageProductCode` của Task 4 (kế hoạch, Task 4: "không lời gọi `.Begin(`… ngoài `internal/platform/db`")
   chặn mọi lời gọi có tên `.Begin(`, nên `httpapi` gọi `idempotency.Begin(...)` làm test đỏ. Hai chỗ của kế hoạch
   mâu thuẫn; chọn giữ test chặn mở transaction (không nới test), đổi tên hàm bằng `gopls rename`. Phương án khác: sửa
   test kiến trúc để bỏ qua `idempotency.Begin` (nới một cổng kiểm, trái CLAUDE.md). Đảo ngược: một lần rename.
   Task sau đọc kế hoạch thấy `Begin` thì dùng `Acquire`; `Claim`, `Stored`, `Scope`, `RequestHash` đúng tên kế hoạch.

## Việc phát hiện thêm

- Task 13 (dọn dữ liệu): bảng chưa có index cho `expires_at`; việc dọn theo lô 5.000 chạy theo từng tenant
  (`WithTenant`, RLS ép) nên nên thêm index `(tenant_id, expires_at)` trong migration của Task 13 khi viết câu dọn.
- `writeAuthError` trả 503 `retry_later` mà không kèm `Retry-After` (Task 7); `writeHandlerError` của task này có.
  Ngoài phạm vi, không sửa.
- Kế hoạch ghi tên `idempotency.Begin`; mã dùng `Acquire` (xem Khác kế hoạch). Task 12, 18 đọc mục này.
- (Reviewer) `WriteTx` lưu thân bằng `json.Marshal(body)`: kiểu ogen chỉ có `MarshalJSON` trên con trỏ, nên handler
  của Task 12, 18 phải truyền con trỏ (`&gen.X{…}`), không thì thân lưu mang tên trường Go và lần phát lại khác lần đầu.
- (Reviewer) Database dev nào đã chạy migration `0006` (Task 10) trước khi có `0005` thì goose mặc định báo thiếu
  `0005`: dựng lại database dev (`make db-reset`). Database mới chạy đúng thứ tự.
- (Reviewer) Hạn giữ chỗ tính bằng giờ máy ứng dụng (`time.Now()`); nhiều bản `api` lệch giờ có thể nhận lại khóa sớm,
  nhưng thẻ chủ (`ErrClaimLost`) vẫn giữ đúng một kết quả.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, một vòng, trên `git diff --cached` sau `make ci` sạch).

- Phát hiện phải sửa: không có. Reviewer kiểm từng điểm: gửi song song (INSERT ON CONFLICT rồi SELECT FOR UPDATE câu
  riêng), nhận lại khóa quá hạn và thẻ chủ `locked_until` (`ErrClaimLost`), "xong" trong transaction nghiệp vụ,
  "thử lại" và `retry_later` không lưu, `no-replay` không lưu thân, `exempt` không chạm bảng và `WriteTx` báo
  `ErrNoClaim`, RLS và phạm vi khóa, đủ 6 luật `routegen` của ADR-47 điểm 2, migration chỉ mở rộng và có trong bảng
  thứ tự khóa nhóm 13, middleware chạy sau `auth`/`guard`/`userRateLimit`, không log khóa, thân, mã băm.
- Ghi chú không bắt buộc (đã ghi vào "Việc phát hiện thêm" hoặc mục Ảnh hưởng, không đổi mã): thân của `WriteTx` phải
  là con trỏ kiểu ogen; thứ tự migration 0005 sau 0006 trên database dev cũ; hạn theo giờ máy ứng dụng;
  `testkit.Staff.ProbeCounter` thiếu trong mục Ảnh hưởng (đã thêm).
- Ghi chú 2 (route dựng tay có `Write` mà `IdempotencyMode` rỗng thì không có khóa): bảng sinh không bao giờ rỗng và
  `TestRoutesIdempotencyModes` chặn; giữ nguyên vì test cũ của Task 6, 7 dựng `Route` tay không khai chế độ.
