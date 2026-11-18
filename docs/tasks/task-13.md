# Task 13 — Sự kiện nội bộ, việc định kỳ, dọn dữ liệu

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Không ký hiệu xuất ra ngoài nào **có sẵn** bị đổi định nghĩa (0 chỗ tham chiếu bị chạm).
  - Gói mới `internal/platform/events` (0 chỗ dùng trước task này): `Event`, `StoredEvent`, `Consumer`, `Registry`,
    `NewRegistry`, `Registry.Register`, `Registry.Emit`, `Registry.JobHandlers`, `ConsumerKind`, `ErrInvalidEvent`,
    `ErrDuplicateEvent`, `ErrInvalidConsumer`.
  - Gói mới `internal/platform/schedules`: `Schedule`, `Registry`, `NewRegistry`, `Registry.Define`, `Ticker`,
    `NewTicker`, `TickerOptions`, `Ticker.Tick`, `Ticker.Run`, `Cleanup`, `NewCleanup`, `Cleanup.RegisterCleanup`,
    `Cleanup.RegisterTenantCleanup`, `Cleanup.Handlers`, `Cleanup.Define`, `CleanupKind`, `TenantCleanupKind`,
    `ErrInvalidSchedule`.
  - `internal/platform/internal/store` (mã sinh): **thêm** câu của `queries/platform/{events,schedules}.sql`; không
    câu cũ nào đổi. `internal/modules/identity/internal/store/models.go` (mã sinh) thêm struct của bảng mới (hai khối
    sqlc dùng chung `migrations`); identity không dùng.
  - `cmd/crm` (`runWorker`, hàm không xuất): đăng ký handler của sự kiện và dọn dữ liệu, chạy `Ticker`.
  - `internal/testkit`: **thêm** `Staff.Count` (đếm bằng chủ bảng) cho test của hai gói mới; không hàm cũ nào đổi.
- Gói phụ thuộc ngược (go list): `internal/platform/internal/store` được `db`, `idempotency`, `metrics`, `queue`,
  `tenancy` nhập (chỉ thêm hàm). `events`, `schedules` mới chỉ được `cmd/crm` nhập. `cmd/crm` không gói nào nhập
  (test e2e chạy tệp chạy).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: **mở rộng**, `migrations/0008_events_schedules.sql`:
  - `platform.domain_events` đúng Interfaces của kế hoạch, thêm `UNIQUE (tenant_id, id)` và **bật RLS** (Quyết định 1),
    trigger `BEFORE UPDATE OR DELETE` báo lỗi (chỉ ghi thêm); `crm_app` chỉ `SELECT, INSERT`.
  - `platform.schedules` đúng Interfaces, không RLS (không có `tenant_id`; spec 04 mục 11 liệt kê); `crm_app` `SELECT,
    INSERT, UPDATE`.
  - Index phục vụ dọn: `idempotency_keys (tenant_id, expires_at)` (Task 9 ghi), `jobs (finished_at) WHERE status IN
    ('done','expired')`. `metrics_minute` đã có khóa chính bắt đầu bằng `bucket`.
  - Quyền dọn: `GRANT DELETE ON platform.jobs, platform.metrics_minute TO crm_app` (`idempotency_keys` đã có DELETE).
  - Thứ tự khóa: `domain_events` (a) và `schedules` (b) đã có trong `docs/adr/0046-thu-tu-khoa.md` mục "Bảng M0 ngoài
    thứ tự nhóm"; không đổi tệp. Câu xóa của việc dọn khóa dòng đã có (`DELETE`): mỗi câu chỉ một bảng, theo lô, `FOR
    UPDATE SKIP LOCKED` (không chờ dòng người khác giữ); `jobs` và `idempotency_keys` là nhóm 13 (cuối), xóa rồi mới
    tới câu `done` của chính việc dọn (cùng nhóm 13, `idempotency_keys` → `jobs` đúng thứ tự); `metrics_minute` (b)
    chỉ có dòng cũ 30 ngày, bộ ghi chỉ số chỉ ghi phút hiện tại nên không gặp nhau.
- Test sẽ chạy: gói mới `./internal/platform/events/...`, `./internal/platform/schedules/...`; gói có store đổi
  (`./internal/platform/...`, có vùng rủi ro cao `queue`, `tenancy`, `idempotency` → chạy cả bộ của vùng),
  `./internal/modules/identity/...` (store sinh lại, schemarules), `./tests/arch/...`, `./tests/e2e/...` (vai trò
  worker), trên dự án compose `crm-t13` (cổng 5513); cuối cùng `make ci` (phần chạy được trên máy).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có `queue.JobQueue`,
  `queue.Handler`, `queue.Kind`, `queue.DefaultPolicy`, `queue.ErrPermanent`, `queue.NewWorker`, `db.RetryTx`,
  `db.TxWorker`, `tenancy.WithTenant`, `tenancy.ListTenantIDs`, `metrics.Registry`. Tìm `emit`, `event`, `schedule`,
  `cron`, `ticker`, `cleanup`, `DELETE FROM`, `expires_at` trong `internal/`, `cmd/`, `queries/`: chỉ có chú thích
  hẹn Task 13 (migration 0005, task-09/10) và câu quét `expires_at` của `queue` (việc pending quá hạn, khác mục đích).
- Dùng lại / viết mới vì:
  - Sự kiện đi qua `queue.JobQueue.Enqueue` (chống trùng `DedupeKey`, `OrderKey`/`OrderSeq`, `BlockOnDead`) và
    handler là `queue.Handler`: không viết hàng đợi thứ hai (spec 04 mục 5.1 "một lõi").
  - Handler của consumer chạy trong transaction worker giao (đã là `WithTenant(job.tenant_id)`), không mở transaction.
  - Việc định kỳ: `Ticker` ghi việc bằng `Enqueue` trong `db.RetryTx`; danh sách tenant bằng `tenancy.ListTenantIDs`.
  - Dọn dữ liệu là handler `queue.Handler` chạy theo lịch. Câu xóa `platform.jobs` nằm trong
    `queries/platform/schedules.sql` (kế hoạch giao `cleanup.go` xóa `jobs`); gói `queue` vẫn là gói duy nhất đổi
    trạng thái việc. Ghi chú "khi nào không dùng" của `queue` trong `docs/danh-muc-ghi-chu.yaml` ghi ngoại lệ này;
    thêm ghi chú cho `events`, `schedules`; `docs/danh-muc-dung-chung.md` sinh lại.
  - Test dùng `testkit.OpenStaff` (hai pool) thay vì tự dựng; jscpd báo trùng phần dựng pool và đếm với
    `queue/helpers_test.go` → thêm `Staff.Count` vào testkit và dùng lại (helper của `queue` giữ nguyên, không chạm
    vùng rủi ro cao); hai đoạn trùng trong cùng tệp (phát hai sự kiện ngược thứ tự, dừng worker e2e) tách thành
    hàm test.
  - Viết mới: tính "kỳ" của lịch (mốc gần nhất ≤ now), không có sẵn ở đâu.
  - Không thêm thư viện.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: sự kiện nội bộ, lịch, dọn dữ liệu là của `platform` (spec 04 mục 5.1, 5.4): bảng schema `platform`,
   mã trong `internal/platform/{events,schedules}`.
2. Không ghi bảng module khác: `events` chỉ ghi `domain_events` và (qua `JobQueue`) `jobs`; consumer của module tự ghi
   bảng của mình trong transaction được giao. `schedules` chỉ ghi `schedules`, `jobs` (qua `JobQueue`) và xóa dòng cũ
   của các bảng nền tảng (`jobs`, `metrics_minute`, `idempotency_keys`).
3. Đọc dữ liệu module khác: không áp dụng.
4. Việc nguyên tử: `Emit` nhận `pgx.Tx` của nghiệp vụ, chèn sự kiện và việc cùng transaction (lùi thì 0 sự kiện, 0 việc).
5. Việc sau commit: đây là cơ chế `events.Emit` (mỗi consumer một việc `event.<consumer>`).
6. Gọi ra bên ngoài: không; consumer chạy trong transaction nên không gọi mạng (ghi ở doc `Consumer`).
7. Ngữ cảnh shop: `domain_events` có `tenant_id` và bật RLS (Quyết định 1): `Emit` chạy trong `WithTenant` của nghiệp
   vụ (WITH CHECK chặn ghi sự kiện cho tenant khác), consumer đọc trong `WithTenant(job.tenant_id)`.
   `schedules` không có `tenant_id`. Lịch theo tenant tạo mỗi tenant một việc (spec 04 mục 5.1, 11). Dọn
   `idempotency_keys` (RLS) chạy trong việc theo tenant (Quyết định 2).
8. Nghiệp vụ không biết HTTP: hai gói không nhập `net/http`.
9. Đường ghi: không áp dụng (không route).
10. PostgreSQL là nguồn sự thật: lịch và sự kiện là bảng PostgreSQL.
11. Thư viện: không thêm.
12. Module mẫu: gói nền tảng như `queue`: SQL trong `queries/platform`, sqlc, test gói `_test` trên PostgreSQL thật.

Mục 6 (đúng khi chạy đồng thời):

1. Hai Ticker cùng lúc: `SELECT … WHERE next_run_at <= now FOR UPDATE SKIP LOCKED`, đặt `next_run_at` mới cùng
   transaction với việc ghi việc; thêm lưới `DedupeKey = "<name>:<slot>"`. Hai `Emit` cùng `(aggregate, seq)`: unique
   `(tenant_id, aggregate_type, aggregate_id, seq)` → `ErrDuplicateEvent`.
2. Gửi lại: việc của consumer có `DedupeKey = eventID`; việc định kỳ có `DedupeKey` theo kỳ.
3. Thứ tự khóa: Ticker khóa `schedules` rồi chỉ chèn `jobs` (ADR-46 (b)); dọn: mỗi câu một bảng, `SKIP LOCKED`, xem
   Ảnh hưởng. `Emit` chỉ chèn (dòng mới).
4. Lỗi tạm: mọi transaction qua `RetryTx`/`WithTenant`; handler có thể chạy lại (câu xóa và đọc chạy lại được).
5. Mức cô lập: READ COMMITTED.
6. Việc nền lỗi: `event.*` theo lịch thử lại của hàng đợi, `BlockOnDead` (việc dead chặn việc sau của đúng đối tượng).
   `task.*` (dọn): `MaxAttempts 1` + hết hạn sau một kỳ ("thử lại ở kỳ sau", spec 04 mục 5.1); dead → chỉ số
   `jobs_dead` có sẵn.
7. Bên ngoài 429: không áp dụng.
8. Tiến trình chết: hàng đợi lo (về hàng chờ); Ticker chết giữa transaction → lùi, kỳ sau chạy.
9. Treo lâu: handler dọn tối đa 5.000 dòng mỗi bảng, trong `Policy.Timeout` 30 s; Ticker dùng `db.TxWorker`.
10. Tiền: không áp dụng.
11. Quy luật: test đồng thời (hai Ticker, Concurrency 4 với sự kiện ngược thứ tự); queue đã có rapid cho thứ tự.
12. Dữ liệu cá nhân: payload việc chỉ `{"eventId"}`/`{"slot"}`; payload sự kiện ghi ở doc là chỉ mang mã bản ghi.
13. Quyền: không áp dụng.

## Quyết định thay chủ shop

1. **`platform.domain_events` bật RLS** (khác Global Constraints của kế hoạch, theo spec). Spec 04 mục 11 chỉ miễn RLS
   cho bảng worker phải đọc của mọi tenant (`jobs`, `webhook_deliveries`, `schedules`, `metrics_minute`,
   `tenant_routes`); `domain_events` không nằm trong danh sách đó, luôn được ghi trong transaction nghiệp vụ có tenant
   và đọc trong handler chạy `WithTenant(job.tenant_id)`, lại mang dữ liệu của shop. Bật RLS an toàn hơn, đảo ngược dễ
   (một migration tắt RLS). Phương án khác: không RLS như kế hoạch, thêm bảng vào `platformTables` của hai test.
2. **Dọn `idempotency_keys` chạy theo tenant.** Bảng có RLS ép, việc không tenant thấy 0 dòng. Thêm lịch
   `platform.cleanup_tenant` (3.600 s, theo tenant) và `RegisterTenantCleanup` cùng chữ ký với `RegisterCleanup`;
   handler chạy trong `WithTenant` (Task 9 đã ghi hướng này). Phương án khác: hàm SECURITY DEFINER do role BYPASSRLS
   làm chủ (nới quyền `crm_search`, trái Global Constraints), hoặc đặt cả `platform.cleanup` theo tenant (xóa bảng
   không tenant N lần, việc `tenant_id` NULL không bao giờ được dọn).
3. **Sổ đăng ký là giá trị, không phải biến toàn cục.** Kế hoạch ghi `func Register`, `func Define`, `func
   RegisterCleanup` cấp gói; luật Go của repo (go-backend mục 4, linter `gochecknoglobals`) cấm biến toàn cục thay đổi
   được. Chọn `events.NewRegistry()` với `Register`/`Emit`/`JobHandlers`, `schedules.NewRegistry()` với `Define`,
   `schedules.NewCleanup()` với `RegisterCleanup`; `cmd/crm` dựng sổ sự kiện bằng một hàm dùng chung cho `api` và
   `worker` khi có consumer (M0 chưa có consumer nào). Ngữ nghĩa giữ nguyên (nạp tĩnh lúc khởi động). Phương án khác:
   biến toàn cục kèm `nolint`.
4. **Lịch `platform.resolve_in_doubt` chưa nối ở task này.** `commands.ResolveInDoubt` thuộc Task 12, đang làm song
   song ở worktree khác, chưa có trong nhánh này. Cơ chế `Define` đã sẵn; khi gộp sau Task 12 chỉ cần một handler gọi
   `ResolveInDoubt` và một dòng `Define` (ghi ở Việc phát hiện thêm). Phương án khác: chờ Task 12 (chặn task).
5. **Kỳ của lịch:** `slot` = mốc `next_run_at + k·every` lớn nhất ≤ now (lịch trễ 10 giờ chạy đúng một kỳ, kỳ mới
   nhất); `next_run_at` mới = `slot + every` (> now). Lịch mới định nghĩa có `next_run_at = now` (chạy ngay kỳ đầu).
6. **Chính sách việc dọn:** `MaxAttempts: 1`, `ExpiresAfter` = một kỳ (spec 04 mục 5.1: việc định kỳ lỗi thì thử lại
   ở kỳ sau). Lô 5.000 dòng mỗi bảng mỗi lần.

## Bằng chứng

Máy: dự án compose `crm-t13`, PostgreSQL 18 cổng 5513 (`CRM_TEST_DATABASE_URL`). Test viết trước, chạy đỏ trước khi
có mã: `go test ./internal/platform/events/ ./internal/platform/schedules/` → `[build failed]` (gói chưa có);
`TestWorkerRoleRunsSchedules` → `relation "platform.schedules" does not exist`. Lệnh chung bên dưới:
`go test -race -count=1 ./internal/platform/events/ ./internal/platform/schedules/ ./tests/e2e/` → `ok … events 5.4s`,
`ok … schedules 5.7s`, `ok … e2e 20.4s`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `Emit` trong transaction lùi → 0 sự kiện, 0 việc | `TestEmitRolledBackLeavesNothing` | lệnh chung | PASS |
| Hai consumer cùng topic → 2 việc đúng kind `event.<c>`, `DedupeKey = id`, `OrderKey`, `OrderSeq`, `BlockOnDead`, payload `{"eventId"}`; consumer không nghe → 0 việc; dòng sự kiện đúng | `TestEmitOneJobPerListeningConsumer`, `TestEmitWithoutConsumersStoresEvent` | lệnh chung | PASS |
| B lỗi lần đầu → A done, gọi 1 lần; B thử lại rồi done (attempts 2) | `TestConsumerFailureIsIndependent` | lệnh chung | PASS |
| **Seq 2 rồi Seq 1 (mỗi cái một transaction), worker Concurrency 4 → nhận `delivered` trước** (lặp 5 database) | `TestEventsRunInSeqOrder` | lệnh chung | PASS |
| `delivered` lỗi tới dead → `returned` pending, 0 lần chạy | `TestDeadEventBlocksLaterEvents` | lệnh chung | PASS |
| Trùng `(aggregate, seq)` → `ErrDuplicateEvent`; đối tượng khác cùng seq được | `TestEmitDuplicateSeq` | lệnh chung | PASS |
| Đầu vào sai → `ErrInvalidEvent`; consumer sai → `ErrInvalidConsumer` | `TestEmitValidation`, `TestRegisterValidation` | lệnh chung | PASS |
| RLS: Emit cho tenant khác tenant của transaction bị chặn; UPDATE/DELETE bị chặn (crm_app không quyền, chủ bảng bị trigger) | `TestEmitOtherTenantRejectedByRLS`, `TestDomainEventsAppendOnly` | lệnh chung | PASS |
| Lịch 60 s theo tenant, 2 tenant → 2 việc đúng tenant và `DedupeKey`; Tick lần hai cùng lúc → 0; `next_run_at` = kỳ + 60 s | `TestTickPerTenant`, `TestTickGlobal` | lệnh chung | PASS |
| Hai Ticker cùng Tick → tổng 2 (lặp 5 database) | `TestTwoTickersNoDuplicate` | lệnh chung | PASS |
| `next_run_at` cũ 10 giờ → đúng 1 kỳ (kỳ mới nhất), `next_run_at > now` | `TestTickCatchUpRunsOnce` | lệnh chung | PASS |
| Chưa tới hạn → 0; lịch lạ không bị đụng; đổi chu kỳ cập nhật dòng, giữ `next_run_at`; `Define` sai → `ErrInvalidSchedule`; `Run` tick ngay và dừng theo ctx | `TestTickNotDueAndUnknown`, `TestDefineUpdatesEvery`, `TestDefineValidation`, `TestRunTicksAndStops` | lệnh chung | PASS |
| Dọn: done 31 ngày xóa, 29 ngày còn, dead 60 ngày còn, expired 31 ngày xóa, pending cũ còn; chỉ số 31 ngày xóa, 29 còn; khóa quá hạn xóa ở cả 2 tenant, còn hạn giữ | `TestCleanupRemovesOnlyOldRows` | lệnh chung | PASS |
| Mỗi lần tối đa 5.000 dòng (5.003 việc cũ → còn 3) | `TestCleanupBatchLimit` | lệnh chung | PASS |
| Hàm đăng ký thêm chạy 1 lần (không tenant) và 1 lần mỗi tenant trong đúng ngữ cảnh tenant | `TestRegisteredCleanupsRun` | lệnh chung | PASS |
| Vai trò worker thật: ghi hai lịch dọn, hai việc dọn `done`, việc done 31 ngày bị xóa; SIGTERM thoát 0 | `TestWorkerRoleRunsSchedules` | lệnh chung | PASS |
| Cổng kiểm đầy đủ: build, vet, golangci-lint (0 issues), gen-check, sqlc vet, `go test -race ./...` (mọi gói, gồm vùng rủi ro cao `queue`, `tenancy`, `idempotency`, `identity`), govulncheck, oasdiff, catalog-check, jscpd (0 clone), test-guard, gitleaks | — | `make ci` | thoát 0 |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Quyết định 1 (RLS của `domain_events`), 2 (`RegisterTenantCleanup`, lịch `platform.cleanup_tenant`), 3 (sổ đăng ký
  là giá trị), 4 (`resolve_in_doubt` chưa nối).

## Việc phát hiện thêm

- Khi gộp sau Task 12: nối lịch `platform.resolve_in_doubt` (30 s) gọi `commands.ResolveInDoubt` (Quyết định 4).
- Khi có consumer đầu tiên (mốc sau): vai trò `api` phải dựng sổ bằng `newEvents()` trong `cmd/crm` và truyền xuống
  module để `Emit` thấy cùng danh sách consumer như worker (reviewer).
- Việc dọn thành `dead` (MaxAttempts 1) không bị xóa: lỗi kéo dài dồn mỗi giờ một việc mỗi tenant; chỉ số `jobs_dead`
  báo, cảnh báo Telegram là Task 19 (reviewer).
- Task 14: đăng ký dọn `webhook_deliveries` quá 30 ngày bằng `Cleanup.RegisterCleanup` trong `cmd/crm`.

## Reviewer

Kết luận: ĐẠT (lần 1, không có phát hiện phải sửa). Reviewer kiểm: hai Ticker đồng thời (`SKIP LOCKED`, `next_run_at`
đổi cùng transaction, khóa chống trùng theo kỳ, kỳ tính từ `next_run_at` trong database nên lệch giờ nhỏ không tạo
trùng; `sync` chờ khóa nhưng Tick không bao giờ chờ lại, không khóa chéo); `slotAt` và độ chính xác microsecond;
`Emit` (cùng transaction, nhận lỗi trùng bằng mã và tên ràng buộc, RLS); handler consumer (`ErrPermanent`, đọc trong
`WithTenant`); câu xóa (một bảng mỗi câu, lô 5.000, chỉ `done`/`expired`, không ảnh hưởng thứ tự theo đối tượng,
index khớp); GRANT; goroutine của Ticker có chủ và dừng theo `ctx`. Bốn quyết định thay chủ shop: không trái spec.
Ghi chú không bắt buộc (không đổi mã): `newEvents` chưa nối vào vai trò api (chưa có consumer, ghi ở Việc phát hiện
thêm); việc dọn `dead` không bị xóa (chỉ số `jobs_dead` báo); `TestEmitValidation` chưa có ca `TenantID` rỗng (mã đã
kiểm).
