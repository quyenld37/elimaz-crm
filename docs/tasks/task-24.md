# Task 24 — Đối chiếu Interface M0, nghiệm thu, threat model M1

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 24 = M1-24). Loại việc (quy trình mục 3): A, chỉ đổi tài liệu.

Mã đối chiếu: `main` tại `2e512ea` (M0 Task 0–13 đã gộp). Task 14 đang gộp: nhánh `task-14` (`f0edece`) đọc để tham khảo, chưa coi là mã thật. Task 15–23 chưa làm.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task chỉ đọc mã (`go doc`, `grep`), không sửa tệp `.go` nào.
- Gói phụ thuộc ngược (go list): không áp dụng, không đổi gói nào.
- Operation OpenAPI bị chạm, oasdiff: không. Chỉ đọc `openapi/openapi.yaml` để lấy tên `operationId` hiện có (9 operation, xem bảng đối chiếu dòng 4).
- Chỗ dùng trên màn hình: không (chưa có `console/`).
- Câu sqlc, bảng, pha migration: không. Chỉ đọc `migrations/0001`–`0008`, `queries/platform/schedules.sql`.
- Tệp sẽ đổi: `docs/tasks/task-24.md`, `docs/nghiem-thu-m1.md` (mới), `docs/threat-model-m1.md` (mới); ngoài repo `apps/crm`: tệp kế hoạch M1 (thư mục `docs/superpowers/plans` của dự án, không nằm trong git), chỉ các chỗ lệch ghi ở bảng "Sửa kế hoạch".
- Test sẽ chạy: không có test mã. Kiểm bằng lệnh đọc: `go doc` từng gói, `grep` kế hoạch sau khi sửa (không còn `idempotency.Begin`, `testkit.SignAccessJWT`, `events.Emit` dạng hàm gói), `git diff --stat` chỉ ba tệp `docs/`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có mục tài liệu; tìm `threat-model`, `nghiem-thu` trong `docs/` → chưa có tệp nào (threat model M0 là việc của Task M0-23). Khung STRIDE theo spec `04-kien-truc.md` mục 17 và `08-roadmap.md` "Cách làm việc ở mỗi mốc".
- Dùng lại / viết mới vì: dùng lại nguyên bảng "Tiêu chí nghiệm thu → ca kiểm thử", "Xong M1 khi", "Vùng rủi ro cao của M1" của kế hoạch M1 (trỏ tới, không chép khác lời); viết mới hai tệp vì Interface của Task M1-24 đòi.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, hàng đợi, tiền, quyền trong mã.

- 5.1–5.12: không áp dụng: không có mã, bảng, route hay thư viện mới. Riêng 5.6 (gọi ra ngoài qua lệnh `in_doubt`) và 5.11 (thư viện) được dùng làm căn cứ khi viết threat model.
- 6.1–6.13: không áp dụng cho mã của task này. 6.9 (handler việc nền tối đa 30 s, ADR-26) dẫn tới quyết định D3 dưới đây; 6.12, 6.13 dùng làm căn cứ ở threat model.

## Đối chiếu Interface M0 với mã thật

Cột "Mã thật" đọc bằng `go doc ./internal/...` trên `main` `2e512ea`. "Khớp" nghĩa là tên, chữ ký và lỗi trả về đúng như kế hoạch M1 dùng.

| # | Kế hoạch M1 ghi | Mã thật | Kết luận | Sửa thế nào |
|---|---|---|---|---|
| 1 | `db.DB`, `db.RetryTx`, `db.TxOptions`, `db.ErrRetryLater`; `testkit.StartPostgres` (M0-04) | `(*db.DB).RetryTx(ctx, TxOptions, fn func(pgx.Tx) error) error`; `TxOptions{LockTimeout, StatementTimeout, IsoLevel, Settings}`; `ErrRetryLater`; `testkit.StartPostgres(t) (adminURL, ownerURL, appURL)`; thêm `testkit.OpenStaff`, `OpenAppDB` | Khớp (`RetryTx` là phương thức của `*DB`) | Không sửa |
| 2 | `docs/adr/0046-thu-tu-khoa.md` có đủ nhóm M0; nhóm 2 ghi `users → roles → user_role_grants` | Tệp có nhóm 1–13. Nhóm 2: `users → roles → role_permissions → user_role_grants → user_credentials → …` (`role_permissions` thêm ở M0 Task 7) | **Lệch**: kế hoạch M1 bỏ sót `identity.role_permissions`, mà Task M1-56 sửa đúng bảng này | Sửa dòng nhóm 2 của Global Constraints |
| 3 | `tenancy.WithTenant`, `Resolve`, `CheckSchemaRules`, SQL `platform.enable_tenant_rls(regclass)`, `testkit.ExplainAsApp`, `RequireNoSeqScan` (M0-05) | `WithTenant(ctx, d, tenantID, fn, o ...db.TxOptions) error`; `Resolve(ctx, d, kind, externalKey) (uuid.UUID, bool, error)`; `CheckSchemaRules(ctx, owner *pgxpool.Pool, platformTables []string) ([]string, error)`; hàm SQL ở `0002_rls_helpers.sql`; `ExplainAsApp(ctx, appDB, tenantID, sql, args...) ([]string, error)`; `RequireNoSeqScan(t, nodes)` | Khớp | Không sửa |
| 4 | `openapi.yaml`, `x-permission`/`x-public`, `IdempotencyKey`, `routegen`, `httpapi.Deps`, `errors.go` (`retry_later`) (M0-06) | Có đủ; `Route{Method, Pattern, OperationID, Permission, Public, Write, Stream, IdempotencyMode, IdempotencyReason}`; `errors.go`: `retry_later` + `Retry-After: 1`. `operationId` hiện có: `healthLive`, `healthReady`, `healthDeep`, `getMe`, `stream`, `listJobs`, `retryJob`, `abandonJob`, `getPlatformHealth` — không tên nào trùng tên M1 định thêm | Khớp | Không sửa |
| 5 | `x-idempotency: exempt` + `x-idempotency-reason` (M0-06, M0-09) | `Route.IdempotencyMode` = `required`, `exempt`, `no-replay` | Khớp; M1 không dùng | Không sửa |
| 6 | `identity.Permission`, `Permissions`, `PermissionDef`, `ScopeNone`, `ScopeByTarget`, `Scope`, `Grant`, `AuthUser`, `Can`, `ScopesFor`, `LoadUser`; bảng `users`, `roles(id, tenant_id, key, name)`, `role_permissions`, `user_role_grants`; `crm seed-dev`; **`testkit.SignAccessJWT`**, `AccessKeyServer` (M0-07) | Có đủ trừ `SignAccessJWT`: thật là `testkit.AccessKeyServer(t) *AccessKeys` rồi `(*AccessKeys).Sign(testkit.AccessClaims) string`. `Scope{Type, ID string}` (ID là chuỗi). `users.status` ∈ `active`, `locked`; `users` chưa có `version`, `updated_at` (M1-26 thêm `version` — đúng). `user_role_grants.scope_type` đã nhận `warehouse` | **Lệch tên** `SignAccessJWT` | Sửa bảng Interface; ghi chú `Scope.ID` là chuỗi ở Task M1-29 (`doc.WarehouseID.String()`) |
| 7 | `identity.RecordAudit`, `AuditEntry` (M0-08) | `RecordAudit(ctx, tx, AuditEntry) error`; `AuditEntry{TenantID; ActorType, ActorID, Action, EntityType, EntityID string; Before, After map[string]any; IP, RequestID string}` | Khớp (ActorID, EntityID là chuỗi) | Không sửa |
| 8 | **`idempotency.Begin`**, `idempotency.Claim` (`Complete`, `Release`), middleware, `httpapi.WriteTx` (M0-09) | `idempotency.Acquire(ctx, d, Scope{TenantID, Actor, Route}, key, reqHash, now) (Claim, *Stored, error)`; `Claim` là interface `Complete(ctx, tx, status, body)`, `Release(ctx)`; middleware ở `internal/httpapi/idempotency.go`; `WriteTx(ctx, Deps, tenantID, fn func(pgx.Tx) (status int, body any, final bool, err error)) error` | **Lệch tên**: `Begin` đã đổi thành `Acquire` | Sửa bảng Interface |
| 9 | `metrics.Registry` (`Observe`, `Inc`, `Gauge`) (M0-10) | `Observe(name, v float64, label)`, `Inc(name, label string, by int64)`, `Gauge(name, v float64, label)`; `NewRegistry()` | Khớp | Không sửa |
| 10 | `queue.Kind`, `Policy`, `DefaultPolicy`, `JobQueue.Enqueue`, `NewJob`, `Handler`, `TerminalHook`, `ErrPermanent` (M0-11) | `Kind{Name, Policy}`; `Policy{Mode, MaxAttempts, BlockOnDead, OnLeaseExpired, ExpiresAfter, Timeout}`; `DefaultPolicy` 8 lần, 30 s; `Enqueue(ctx, tx, Kind, NewJob) (id, created, err)`; `NewJob{TenantID *uuid.UUID, Payload, DedupeKey, OrderKey, OrderSeq, RunAt}`; `Handler.Handle(ctx, Job, tx) (json.RawMessage, error)`; `OnTerminal(ctx, Job, status, tx) error`. Bảng `jobs` không RLS (spec 04 mục 11) | Khớp | Không sửa |
| 11 | `commands.Handler`, `Outcome`, `Looker`, `DoneHook`, `CommandKind`, `ResolveInDoubt`; `testkit.FakeExternal` (M0-12) | Có đủ, đúng chữ ký. `CommandKind(name, expiresAfter, timeout)`: tài liệu ghi "timeout ≤ 30 s"; mã không chặn số lớn hơn (giữ chỗ 90 s) | Khớp về tên. **Lệch spec** ở kế hoạch M1-43: `CommandKind("file.process", time.Hour, 60*time.Second)` (và `file.process_private`) vượt giới hạn 30 s của spec 04 mục 5.1, ADR-26 và ADR-45 | Sửa về `30*time.Second` theo ADR-45 (quyết định D3) |
| 12 | **`events.Emit`**, `Event`, `Consumer`, **`Register`**; **`schedules.Define`**, `Schedule` (M0-13) | Không có hàm gói: `(*events.Registry).Emit(ctx, tx, q queue.JobQueue, e Event) (uuid.UUID, int, error)`, `(*events.Registry).Register(Consumer) error`, `events.NewRegistry()`; `(*schedules.Registry).Define(Schedule) error`; `Schedule{Name, Every, PerTenant, Kind}` — **không có giờ chạy đầu**: lịch mới chạy kỳ đầu ngay lúc Ticker ghi lịch (`InsertSchedule … ON CONFLICT (name) DO NOTHING`), các kỳ sau neo theo `next_run_at` đã có | **Lệch tên** (phương thức của `Registry`); **lệch khả năng**: Task M1-36 đòi "`next_run_at` đầu tiên 19:30 UTC" mà `Define` không đặt được | Sửa bảng Interface; Task M1-36: migration `0122` chèn sẵn dòng `platform.schedules` với `next_run_at` 19:30 UTC kế tiếp (quyết định D2) |
| 13 | `webhooks.Source`, `InboundEvent`, `Route{Kind, ExternalKey}`, `SplitResult`; `webhooks.InboundHandler`, `InboundKind` (M0-14, M0-15) | Chưa có trên `main`. Nhánh `task-14`: `Source{Key, Challenge, VerifySignature, Split}`, `InboundEvent{Type, DedupeKey, Route, OrderKey, OccurredAt, Payload}`, `Route{Kind, ExternalKey string}`, `SplitResult{Events, Skipped}`, `selftest.Source` — khớp kế hoạch | **Chờ Task 14** (đang gộp); `InboundHandler`, `InboundKind` **chờ Task 15** | Ghi "chờ" vào bảng |
| 14 | `realtime.Bus.Publish`, `Signal`, `Entity`; `testkit.OpenSSE` (M0-16); bảng ghi M1 dùng ở 27, 29, 35, 43 | Chưa có (`internal/platform/realtime` không tồn tại; operation `stream` có trong hợp đồng). Theo kế hoạch M0: `Bus.Publish(ctx, tx, Signal{TenantID, Topic, Requires, Scope, Entity})` | **Chờ Task 16**. Cột "M1 dùng ở" lệch: thân kế hoạch dùng báo hiệu ở 27, 28, 43 (và lớp realtime màn hình ở 48), không ở 29, 35 | Sửa cột "M1 dùng ở"; ghi "chờ" |
| 15 | Console: client sinh, `createRealtimeClient`, `Banner`, lint, formatter (M0-17) | Chưa có thư mục `console/` | **Chờ Task 17** | Ghi "chờ" |
| 16 | `observability.Alerter`, `CheckRules`, `RuleOptions`, `/health/deep` (M0-19) | `observability` chỉ có `Health`, `NewHealth`, `Pinger`. `healthDeep` có trong hợp đồng nhưng handler trả 501 (`UnimplementedHandler`) | **Chờ Task 19** | Ghi "chờ" |
| 17 | `observability.RecordError`, `GET /api/v1/platform/health` (M0-20) | `RecordError` chưa có; `getPlatformHealth` có trong hợp đồng, handler 501 | **Chờ Task 20** | Ghi "chờ" |
| 18 | `docker/compose.dev.yml` (PostgreSQL 5442, Jaeger, **SeaweedFS 8333 "đã có ở M0"**), `Dockerfile` (M0-01, 02, 21) | `compose.dev.yml`: PostgreSQL `${CRM_PG_PORT:-5442}`, Jaeger `${CRM_JAEGER_UI_PORT:-16686}`, `${CRM_OTLP_PORT:-4318}`; **không có SeaweedFS**. SeaweedFS chỉ ở `compose.backup-drill.yml` (`chrislusf/seaweedfs:4.48@sha256:4e61d15f…`, cổng 8333 chỉ `expose` trong mạng compose vì cổng 8333 của máy này bị dự án khác giữ — task-02.md quyết định 5; kiểm lại hôm nay bằng `lsof`: vẫn bị giữ). `Dockerfile` chính chưa có | **Lệch**: SeaweedFS dev chưa có ở M0; Task M1-40 phải tạo (kế hoạch đã giao Task M1-40 viết dịch vụ này, chỉ câu "đã có ở M0" sai). `Dockerfile` **chờ Task 21**, không chặn M1 (M1-42, M1-45 viết Dockerfile riêng) | Sửa Global Constraints (cổng dev) và Task M1-40: cổng máy qua biến `CRM_S3_PORT` (mặc định 8333), `CRM_S3_OFFSITE_PORT` (8334), cùng kiểu `CRM_PG_PORT` (quyết định D1) |
| 19 | `docs/RUNBOOK.md`, CI, `.gremlins.yaml` (M0-23); bảng ghi M1 dùng ở "mọi task có migration, 46, 55" | Chưa có `docs/RUNBOOK.md`, `.github/`, `.gremlins.yaml`. `make ci` đã có (Task 3); test kiến trúc `boundaries_test`, `sqlusage_test`, `dynsql_test` đã có (Task 4, 6) | **Chờ Task 23**. Cột "M1 dùng ở" lệch: task có migration không cần RUNBOOK; các task sửa RUNBOOK là 33, 45, 46, M1-55 | Sửa cột; ghi "chờ" |
| 20 | Dải migration M0 `0001`–`0099` (Q-X7) | `main`: `0001`–`0008`; `task-14` thêm `0009` | Khớp (nhỏ hơn `0100`) | Không sửa |

## Task M1 nào chạy được ngay

Số là số nội bộ M1 (Task M1-NN). "Chờ M0" chỉ tính Interface M0 chưa gộp; điều kiện trong M1 (cột "Cần xong trước" của kế hoạch) và việc chủ shop duyệt kế hoạch vẫn áp dụng.

| Task M1 | Chờ M0 | Ghi chú |
|---|---|---|
| 25 | — | Chạy được ngay |
| 26 | — | Chạy được ngay |
| 27 | **Task 16** | Báo hiệu `catalog.product` |
| 28 | **Task 16** | Báo hiệu `inventory.balance` |
| 29, 30, 32, 34, 37, 38, 39, 41, 44, 47 | — | Chỉ chờ task M1 trước nó |
| 31 | **Task 14, 15** | `webhooks.Source`, `selftest.Source` (14), `InboundHandler`, `InboundKind` (15) |
| 33 | **Task 14, 15, 18, 23** | `ingest` thật; dấu vết liền `ingest` → worker (18); mục RUNBOOK (23). Staging (M0-23 bước 7) không chặn: Điểm dừng M1-A ghi "chưa chạy trên staging" |
| 35, 36 | **Task 19** | `Alerter.Notify` |
| 40 | — | Chạy được ngay (tự tạo SeaweedFS dev, quyết định D1) |
| 42 | — | Chạy được sau M1-40 |
| 43 | **Task 16** | `Bus.Publish` trong `OnDone` |
| 45 | **Task 19, 23** | `CheckRules`, `deephealth.go` (19); mục RUNBOOK (23) |
| 46 | **Task 23** | Mục RUNBOOK "Dựng lại kho tệp" |
| 48, 50, 51, 52, 54, M1-56 | **Task 17** | Khung `console/` |
| 49 | — | Chạy được ngay (chỉ tài liệu, dựa `DESIGN.md`) |
| 53 | **Task 17, 20** | `RecordError`, `getPlatformHealth` |
| M1-55 | **Task 22, 23** | Bộ k6 (22); `.gremlins.yaml`, CI, RUNBOOK, staging (23) |

Ba nhánh bắt đầu được ngay sau khi chủ shop duyệt kế hoạch: 25 và 26 (lõi dữ liệu, rồi dừng ở 27 chờ Task 16), 40 → 42 (kho tệp, 41 cần thêm 25, 26), 49 (mockup).

## Sửa kế hoạch M1

Chỉ đổi tên, chữ ký và cột "M1 dùng ở"/"Đối chiếu"; không đổi hành vi, tiêu chí hay tên M1 mà mốc sau dựa vào.

| Chỗ | Trước | Sau |
|---|---|---|
| Bảng Interface, dòng `idempotency` | `idempotency.Begin` | `idempotency.Acquire(ctx, d, Scope, key, reqHash, now) (Claim, *Stored, error)` |
| Bảng Interface, dòng `identity` | `testkit.SignAccessJWT`, `AccessKeyServer` | `testkit.AccessKeyServer(t)` + `(*AccessKeys).Sign(AccessClaims)` |
| Bảng Interface, dòng `events`/`schedules` | `events.Emit`, `Register`; `schedules.Define` | Phương thức của `events.Registry`, `schedules.Registry`; `Schedule` không có giờ chạy đầu |
| Bảng Interface, dòng `realtime`, `RUNBOOK` | "M1 dùng ở" 27, 29, 35, 43; "mọi task có migration, 46, 55" | 27, 28, 43, 48; 33, 45, 46, M1-55 |
| Bảng Interface, cột "Đối chiếu" | "đã đối chiếu 18/11" | Kết quả Task M1-24: khớp, đã sửa, hoặc chờ Task NN |
| Bảng Interface, dòng compose | "SeaweedFS 8333" | SeaweedFS dev chưa có ở M0, Task M1-40 tạo |
| Global Constraints, thứ tự khóa nhóm 2 | `users → roles → user_role_grants` | `users → roles → role_permissions → user_role_grants` |
| Global Constraints, cổng dev | "SeaweedFS chính 8333 (đã có ở M0)" | 8333 qua `CRM_S3_PORT`, 8334 qua `CRM_S3_OFFSITE_PORT`; Task M1-40 tạo |
| Bảng Interface, ghi chú chung | — | Trong thân kế hoạch, `events.Emit`, `schedules.Define`, `realtime.Publish(Signal{…})` đọc là phương thức tương ứng (`Registry.Emit`, `Registry.Define`, `Bus.Publish(ctx, tx, Signal{TenantID, …})`); không sửa từng chỗ trong thân |
| Bảng Interface, đoạn cuối | — | Danh sách task M1 chạy được ngay và task chờ M0 (như bảng trên) |
| Task M1-29 (quyền theo kho) | `&Scope{"warehouse", doc.WarehouseID}` | `doc.WarehouseID.String()` |
| Task M1-36 | `schedules.Define(...)`, "`next_run_at` đầu tiên 19:30 UTC" | Kèm cách làm: migration `0122` chèn dòng lịch, và một ca test cho giờ chạy đầu (D2) |
| Task M1-40 | SeaweedFS "chính 8333" | Thêm biến cổng, dùng lại bản ghim của `compose.backup-drill.yml` |
| Task M1-43 | `CommandKind("file.process"…, 60*time.Second)`, `file.process_private` cũng vậy | `30*time.Second`, kèm một dòng nêu căn cứ ADR-45 và cách làm khi số đo vượt 30 s (D3) |
| Đầu tệp (khối "Trước khi bắt đầu M1") | — | Một câu tóm tắt kết quả đối chiếu ngày 18/11 |
| Global Constraints, thứ tự khóa nhóm 2 | (như dòng trên) | (như dòng trên) |

## Quyết định thay chủ shop

- **D1 — SeaweedFS dev và cổng 8333.** Chọn: Task M1-40 thêm SeaweedFS vào `compose.dev.yml` với cổng máy `127.0.0.1:${CRM_S3_PORT:-8333}` (kho ngoài giả `${CRM_S3_OFFSITE_PORT:-8334}`), cùng bản ghim `chrislusf/seaweedfs:4.48@sha256:4e61…` đã dùng ở Task 2. Vì: cổng 8333 trên máy này đang bị dự án khác giữ; mặc định vẫn đúng spec (8333); giống cách `CRM_PG_PORT` cho nhiều phiên song song. Phương án khác: đổi hẳn sang cổng khác (lệch bảng cổng của spec); chỉ `expose` trong mạng compose như Task 2 (test chạy từ máy không tới được, trình duyệt không tải thẳng lên được).
- **D2 — Giờ chạy đầu của lịch đối chiếu đêm (Task M1-36).** Chọn: migration `0122` của Task M1-36 chèn sẵn dòng `platform.schedules('inventory.reconcile', 86400, true, <19:30 UTC kế tiếp>)`; Ticker gặp dòng đã có thì giữ `next_run_at` (`ON CONFLICT DO NOTHING`) và các kỳ sau neo theo giờ đó. Vì: không đổi Interface M0 (`Schedule`), dễ đảo ngược (xóa dòng). Phương án khác: thêm trường `FirstRunAt` hay `Anchor` vào `schedules.Schedule` (đổi gói M0 vùng nền tảng, cần task riêng).
- **D3 — Hạn 60 s của `file.process`, `file.process_private` (Task M1-43).** Chọn: sửa kế hoạch về `30*time.Second`. Vì: ADR-45 (đã duyệt) ghi "toàn bộ thời gian tạo biến thể của một ảnh phải nằm trong giới hạn 30 giây mỗi việc của worker" và "vượt 30 giây: tách thành một việc cho mỗi biến thể"; spec 04 mục 5.1, ADR-26 cùng số; spec đứng trên kế hoạch (quy trình mục 2) và kế hoạch không nêu lý do cho 60 s. Dễ đảo ngược: một số trong kế hoạch, chưa có mã. Phương án khác: giữ 60 s (cần ADR mới thay ADR-45, chủ shop duyệt); dừng hỏi (chặn vô ích, spec đã trả lời).
- **D4 — Cách ghi "chờ Task NN".** Interface của Task 14 đọc trên nhánh `task-14` khớp kế hoạch nhưng vẫn ghi "chờ Task 14" vì chưa gộp vào `main`. Phương án khác: coi là đã có (rủi ro nhánh còn đổi khi gộp).

## Dừng để hỏi

Không có. Mọi lệch đều chọn được theo spec (mục Quyết định thay chủ shop). Duyệt cả kế hoạch M1 vẫn là việc của chủ shop trước Task M1-25 (bảng "Việc cần chủ shop trong M1").

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mọi dòng bảng Interface đã so với mã thật | đọc mã | `go doc -short` 13 gói (`db`, `tenancy`, `idempotency`, `metrics`, `queue`, `commands`, `events`, `schedules`, `observability`, `identity`, `testkit`, `httpapi`, `config`) và `go doc` từng ký hiệu | 20 dòng ở bảng "Đối chiếu": khớp, lệch đã sửa (dòng 2, 6, 8, 11, 12, 18; cột "M1 dùng ở" của 14, 19), hoặc chờ Task 14–23 (dòng 13–17, 19) |
| Tên thật mà kế hoạch giờ dùng có trong mã | đọc mã | `go doc ./internal/platform/idempotency Acquire`; `… testkit AccessKeys.Sign`; `… events Registry.Emit`; `… schedules Registry.Define` | `func Acquire(ctx, d *db.DB, s Scope, key string, reqHash []byte, now time.Time) (Claim, *Stored, error)`; `func (k *AccessKeys) Sign(c AccessClaims) string`; `func (r *Registry) Emit(ctx, tx, q queue.JobQueue, e Event) (uuid.UUID, int, error)`; `func (r *Registry) Define(s Schedule) error` |
| Tên cũ, Interface chờ không có trong mã | đọc mã | `go doc` cho `idempotency Begin`, `testkit SignAccessJWT`, `observability Alerter`, gói `realtime` | cả bốn: "không có" |
| Kế hoạch không còn tên cũ ngoài ghi chú sửa | grep | `grep -n "idempotency.Begin\|SignAccessJWT\|60\*time.Second\|(đã có ở M0)\|đã đối chiếu 18/11"` trên tệp kế hoạch M1 | còn 3 dòng, đều là câu ghi chú sửa ("kế hoạch ghi … mã thật là …", "bản trước ghi 60 s") |
| Migration M0 nhỏ hơn 0100 | đọc tệp | `ls migrations`; `git diff main...task-14 --stat` | `main`: `0001`–`0008`; `task-14`: thêm `0009` |
| Lịch đặt sẵn `next_run_at` thì Ticker giữ (căn cứ D2) | đọc mã | `queries/platform/schedules.sql` (`InsertSchedule … ON CONFLICT (name) DO NOTHING`), `schedules.go` `slotAt` | kỳ đầu lấy từ dòng đã có; kỳ sau `next + k·every` |
| Cổng 8333 bị giữ (căn cứ D1) | lệnh | `lsof -nP -iTCP:8333 -sTCP:LISTEN` | một tiến trình Docker khác đang nghe `*:8333` |
| Hạn 30 s là luật của spec (căn cứ D3) | đọc spec | `07-adr.md` ADR-45 "Quyết định", "Xem lại khi"; `04-kien-truc.md` dòng 230 | "Toàn bộ thời gian tạo biến thể của một ảnh phải nằm trong giới hạn 30 giây"; "mỗi handler bị giới hạn 30 giây" |
| Hai tài liệu mới đủ phần Interface đòi | đọc tệp | `docs/nghiem-thu-m1.md`, `docs/threat-model-m1.md` | 8 tiêu chí bằng lời nghiệp vụ, bảng 23 ca, 9 vùng cao, ngoài phạm vi; sơ đồ và 6 bảng STRIDE, mỗi mối đe dọa có task/test, 4 lỗ hổng L1–L4 |
| Chỉ đổi tài liệu | git | `git status --short` | ba tệp `docs/` của task này, cộng tệp trạng thái task do `new-task.sh` đổi |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `internal/httpapi.Deps` ghi chú "Task 15 thêm Realtime", kế hoạch M0 đặt realtime ở Task 16. Việc của M0 Task 16, không sửa ở đây.
- Kế hoạch M1-45 tạo `crm_filesync` bằng `CREATE ROLE` trong migration; M0 tạo role ở `docker/postgres/init`. Task M1-45 cần kiểm cách tạo role trước khi viết (không phải lệch Interface, chỉ ghi để task đó biết).

- Threat model tìm thấy bốn lỗ hổng chưa có trong kế hoạch (L1–L4 ở `docs/threat-model-m1.md` mục cuối): kiểm `content-type` biến thể ảnh và `nosniff` cho bucket công khai (M1-40, M1-43); `imager` kiểm host của URL (M1-42); đường dẫn ký sẵn không vào log, span (M1-41, M1-43); khóa chống xóa của kho ngoài (M1-45, cần chủ shop). Đề xuất cho các task đó, không tự sửa kế hoạch.

## Reviewer

Kết luận: không cần (task chỉ đổi tài liệu, CLAUDE.md bước 7).
