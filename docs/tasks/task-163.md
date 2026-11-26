# Task 163 — Ai đang xem, ai đang trả lời

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Task 163, M4-23). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `presence.Parts` (gói nội bộ, 1 chỗ gọi: `conversation/wiring.go`)
  đổi từ Parts rỗng sang Parts có 1 việc nền + 1 lịch. Ký hiệu mới ở gói gốc `conversation`: `PresenceState` (+3 hằng),
  `PresenceEntry`, `SetPresence`, `ListPresence`, `ErrNotFound`, `PresenceCleanupSchedule` — chưa có chỗ dùng cũ.
  `httpapi.handler` thêm `GetPresence`, `SetPresence` (thay `gen.UnimplementedHandler`).
- Gói phụ thuộc ngược (go list): `conversation` được nhập bởi `cmd/crm` (wire_m4.go) và từ task này `internal/httpapi`.
  `TestWiringEmpty` (conversation) và `TestWireM4_Frame` (cmd/crm) đang đòi khung rỗng → sửa kỳ vọng: đúng một lịch
  `conversation.presence_cleanup` và một việc nền của nó.
- Operation OpenAPI bị chạm, oasdiff: `getPresence`, `setPresence` (hợp đồng đã viết ở Task 143, không đổi
  `openapi.yaml`, oasdiff không chênh); xóa hai dòng khỏi `tests/arch/testdata/m4_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn hội thoại là Task 175); `console/src/api` không đổi.
- Câu sqlc, bảng, pha migration: migration mới `0406_presence.sql` (pha mở rộng: CREATE UNLOGGED TABLE, RLS, GRANT);
  `queries/conversation/presence.sql` mới (ConversationPage, RefreshPresence, UpsertPresence, ListPresence,
  DeleteExpiredPresence); đọc `identity.users_v` (conversation được gọi identity, 04 mục 3) → thêm M4 vào cột "Mốc đọc"
  của dòng `identity.users_v` trong `docs/views.md`. Bảng `conversation.presence` đã có dòng nhóm 6 ADR-46 (cuối nhóm).
- Mã sinh: `make gen` (sqlc đọc mọi migration) thêm kiểu `ConversationPresence` vào `internal/**/store/models.go` của
  mọi khối sqlc (13 tệp, chỉ thêm) và tạo `conversation/internal/store/presence.sql.go`. Lớp HTTP: `errors.go` thêm
  `conversationError` vào `domainErrorMappers`; tệp mới `errors_conversation.go` (conversation.ErrNotFound → 404
  `not_found`, mã đã có); dòng mới trong `errors_snapshot_test.go`. Chỉ số mới `idempotency_keys_created` nhãn
  `setPresence` (counter, `platform.metrics_minute`).
- Test sẽ chạy: `internal/modules/conversation/...` (`-race`, gói có mã viết tay), `internal/httpapi`
  (`handlers_presence_test.go`, rồi cả gói ở ci-task), `cmd/crm` (`TestWireM4_Frame`), `tests/arch`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (realtime.Bus/Signal,
  schedules.Schedule, queue.Kind/Handler, httpapi.WriteTx, tenancy.WithTenant, metrics.Registry.Inc); kiểm quyền theo
  Page `access.Require`/`access.PageScope` (Task 151); lịch theo tenant mẫu `order.expire_drafts` (drafts.go);
  `realtimetest.RecordingBus` để đếm báo hiệu; test httpapi theo mẫu `handlers_jobs_test.go` (`serveAs`, `testServerDeps`).
- Dùng lại / viết mới vì: dùng lại toàn bộ các phần trên; chỉ viết mới câu SQL của bảng presence (bảng mới) và hàm
  nghiệp vụ trong `internal/presence`. Lấy mã Page của hội thoại viết câu `ConversationPage` riêng (chưa có câu đọc một
  hội thoại theo id: Task 159 chưa gộp).

## Soát thiết kế

- 5.1 chủ dữ liệu: bảng `conversation.presence` trong schema `conversation` (04 mục 6, ADR-46 nhóm 6).
- 5.2 không ghi bảng module khác: chỉ ghi `conversation.presence`.
- 5.3 đọc module khác qua view: tên nhân viên đọc qua `identity.users_v` (conversation được gọi identity).
- 5.4/5.5 nguyên tử: một nhịp tim = một transaction (WriteTx: upsert + báo hiệu trong tx + lưu khóa chống trùng);
  báo hiệu phát trong tx (Bus.Publish chỉ tới client khi commit). Không có sự kiện nội bộ.
- 5.6 gọi ra ngoài: không áp dụng (không gọi Meta).
- 5.7 tenant: bảng mới có tenant_id, RLS ép (`platform.enable_tenant_rls`), khóa chính ghép tenant, khóa ngoại ghép
  (tenant_id, conversation_id) → conversations ON DELETE CASCADE; mọi câu trong WithTenant (WriteTx, lịch PerTenant).
- 5.8 nghiệp vụ không biết HTTP: handler mỏng ở httpapi; module nhận `identity.AuthUser`, `realtime.Bus`, giờ.
- 5.9 khóa chống trùng, quyền: hợp đồng có `x-permission: conversation.read` + `IdempotencyKey` (Q9: khóa mới mỗi
  lần); quyền theo Page kiểm trong module (`access.Require`, không có → 404).
- 5.10 Valkey: không áp dụng.
- 5.11 thư viện: không thêm.
- 5.12 module mẫu: theo identity/order (sqlc trong store, sentinel ở gói gốc, lỗi đổi HTTP ở `errors_conversation.go`).
- 6.1 đồng thời: làm mới hạn là một câu `UPDATE … WHERE state = @state AND expires_at > @now` (khóa dòng); 0 dòng →
  `INSERT … ON CONFLICT DO UPDATE`. Hai nhịp tim cùng lúc của cùng người có thể phát 2 báo hiệu — vô hại (báo hiệu chỉ
  bảo màn hình đọc lại).
- 6.2 gửi lại: `Idempotency-Key` (middleware); gửi lại cùng khóa trả 204 đã lưu, không đổi gì.
- 6.3 thứ tự khóa: chỉ khóa dòng `conversation.presence` (cuối nhóm 6); đọc `conversations` không khóa; lịch dọn xóa
  theo một câu DELETE.
- 6.4 lỗi tạm: WriteTx/tenancy có RetryTx; hàm trong tx gán lại biến kết quả mỗi lần.
- 6.5 cách ly: READ COMMITTED + câu có điều kiện.
- 6.6 việc nền lỗi: việc của lịch MaxAttempts 1, hết hạn sau một kỳ (60 s) như `order.expire_drafts`; kỳ sau dọn lại.
- 6.7, 6.8: không áp dụng (không gọi ra ngoài); bảng UNLOGGED mất khi PostgreSQL khởi động lại là được phép (04 mục 6).
- 6.9 treo: timeout chuẩn của TxAPI.
- 6.10 tiền: không áp dụng. 6.11 rapid: không áp dụng (không phải vùng rủi ro cao; quy luật đơn giản, test theo ca).
- 6.12 dữ liệu cá nhân: báo hiệu chỉ có mã hội thoại; trả về tên hiển thị nhân viên (không phải dữ liệu khách).
- 6.13 quyền: route đòi `conversation.read` (guard); trên Page của hội thoại kiểm `access.Require` → 404 (không lộ tồn
  tại); hội thoại tenant khác → RLS → 404. Báo hiệu đòi `conversation.read` phạm vi `page:<id>`.

## Quyết định thay chủ shop

1. **"Ghi chỉ số số dòng idempotency_keys mỗi nhịp tim"** làm hai việc: test đo và khẳng định đúng 1 dòng mỗi nhịp tim
   (gửi lại cùng khóa: 0 dòng), và counter `idempotency_keys_created` nhãn `setPresence` cộng 1 mỗi nhịp tim đã lưu,
   ghi vào `platform.metrics_minute` như mọi chỉ số, để theo ngưỡng 5 triệu dòng của Q9. Phương án khác: chỉ đo trong
   test (không theo dõi được khi chạy thật), hoặc đếm chung mọi route trong middleware chống trùng (rộng hơn task).
   Đảo ngược: xóa 3 dòng trong `handlers_presence.go`.
2. **Một đồng hồ cho ghi, đọc, dọn:** giờ do Go truyền vào (api: `Deps.Now`, worker: `conversation.Deps.Now`, nil →
   `time.Now`), không dùng `now()` của PostgreSQL — theo bẫy "test so thời gian chỉ dùng một đồng hồ"; test cắm giờ để
   kiểm hết hạn 30 s, 31 s mà không chờ giờ thật.
3. **Không ghi audit cho nhịp tim** (không phải thay đổi nghiệp vụ, 8.640 dòng/người/ngày). Trả 204 kể cả khi chỉ gia
   hạn.
4. **Danh sách theo tên hiển thị** (rồi mã người) cho ổn định trên màn hình; người bị khóa vẫn hiện tới khi dòng hết
   hạn (tối đa 30 s).
5. **Báo hiệu khi `bus` nil trả lỗi** (tx lùi, 500) thay vì panic — chỉ xảy ra khi tiến trình nối sai.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| CH10: An viewing → Bình thấy [An:viewing], 1 báo hiệu; 5 lần viewing → không thêm; typing → 1 báo hiệu, Bình thấy typing; An không thấy mình | `presence.TestSet_SignalsOnlyOnChange`; `httpapi.TestPresence_HeartbeatAndSignals` | `go test -race -count=1 -v ./internal/modules/conversation/internal/presence/`; `go test -race -count=1 -v -run '^(TestPresence_.*)$' ./internal/httpapi/` | `--- PASS: TestSet_SignalsOnlyOnChange (0.51s)`; `--- PASS: TestPresence_HeartbeatAndSignals (1.26s)` |
| LV07 phần hiển thị: drafting_order → Bình thấy "An đang lên đơn" | cùng hai test trên (bước drafting_order) | như trên | PASS |
| Không nhịp tim 31 s rồi lịch dọn → Bình thấy [], 1 báo hiệu (2 người hết hạn cùng hội thoại → 1 báo hiệu; dọn lại → 0) | `presence.TestCleanup_ExpiredRows`; `httpapi.TestPresence_ExpiresAfterCleanup` | như trên | `--- PASS: TestCleanup_ExpiredRows (0.51s)`; `--- PASS: TestPresence_ExpiresAfterCleanup (0.96s)` |
| Dòng hết hạn chưa dọn: không hiện; nhịp tim lại → "mới xuất hiện", có báo hiệu | `presence.TestSet_ExpiredRowReappears` | như trên | `--- PASS: TestSet_ExpiredRowReappears (0.47s)` |
| Không quyền trên Page / không có hội thoại / tenant khác → 404, không ghi, không báo hiệu; không conversation.read → 403; trạng thái lạ → 400; thiếu bus → lỗi | `presence.TestAccess_NotFound`; `httpapi.TestPresence_Access` | như trên | `--- PASS: TestAccess_NotFound (0.51s)`; `--- PASS: TestPresence_Access (1.30s)` |
| Số dòng idempotency_keys mỗi nhịp tim (Q9) | `httpapi.TestPresence_HeartbeatAndSignals` (6 nhịp tim → 6 dòng, counter 6; gửi lại cùng khóa → 0) | như trên | `idempotency_keys mỗi nhịp tim: 1.00 dòng (6 nhịp tim; …)` |
| Lịch `conversation.presence_cleanup` 60 s theo tenant, nối vào worker | `presence.TestParts_Schedule`; `conversation.TestWiring`; `cmd/crm TestWireM4_Frame` | `go test -count=1 -run '^TestWireM4_Frame$' ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm` |
| 404 của conversation.ErrNotFound trong bảng đổi lỗi | `TestDomainErrorSnapshot` | `go test -race -count=1 -run '^TestDomainErrorSnapshot' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.284s` |
| Cổng kiểm của task | toàn bộ | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh aafa087…)`, 88 gói ok (affected.py thoát 5 → toàn bộ trừ e2e, 476 giây) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Handler gọi public API ở gói gốc (`conversation/presence.go`: `SetPresence`, `ListPresence`, `ErrNotFound`) vì
  `httpapi` không nhập được `conversation/internal/presence`; logic nằm ở `internal/presence/presence.go` như kế hoạch.
- Thêm `internal/httpapi/errors_conversation.go` (kế hoạch không ghi; cần để 404 đi qua bảng đổi lỗi, luật Task 441).

## E2E viết sẵn, chưa chạy

(không có: kế hoạch task không có test e2e)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, 26/11).

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến khớp thực tế (presence.Parts, ký hiệu mới ở gói gốc, handler, bảng đổi lỗi, migration 0406, 5 câu
  sqlc, 13 models.go sinh, docs/views.md, chỉ số idempotency_keys_created); TestPresenceTopic chưa nêu nhưng vô hại.
- Bảng bằng chứng: mọi dòng trỏ test có thật và kiểm đúng tiêu chí (CH10, LV07, dọn sau 31 s, Q9, 404/403/400).
- Ghi chú không bắt buộc: lớp HTTP chưa có ca 404 tenant khác (đã có ở test module, cùng đường ErrNotFound); hai nhịp
  tim đầu đồng thời có thể phát 2 báo hiệu (đã ghi ở 6.1, vô hại).

## Lượt sửa gộp m4

**Xung đột** (`git merge --no-commit m4`, nhánh m4 đã có Task 145, 146, 147, 148, 152, 169):

| Tệp | Hai bên | Cách giải |
|---|---|---|
| `internal/httpapi/errors.go` | 163 thêm `conversationError`; m4 thêm `channelError` | giữ cả hai trong `domainErrorMappers`: `… contentError, channelError, conversationError` (thứ tự của m4) |
| `internal/httpapi/errors_conversation.go` | cả hai tạo tệp; 163 chỉ có `ErrNotFound`, m4 (169) có `ErrNotFound` + 5 lỗi nhắc việc + `isConversationRefusal` | lấy bản m4 (chứa dòng của 163, cùng thứ tự kiểm) |
| `internal/httpapi/errors_snapshot_test.go` | 163: 1 dòng conversation; m4: 3 dòng channel + 6 dòng conversation | lấy bản m4 (chứa dòng của 163) |
| `cmd/crm/wire_m4_test.go` | 163: lịch duy nhất presence_cleanup, không kênh; m4: kênh fakechat ngoài production, lịch duy nhất reminders | giữ kiểm kênh/nguồn của m4; lịch mong đợi = `[conversation.presence_cleanup, conversation.reminders]`, mỗi lịch theo tenant, một handler cùng Kind |
| `internal/modules/conversation/wiring_test.go` | 163 `TestWiring` (1 lịch presence) + `TestPresenceTopic`; m4 `TestWiringEmpty` (1 lịch reminders) | một `TestWiring` kiểm đủ 2 lịch (theo tenant, 60 s, handler cùng Kind); giữ `TestPresenceTopic`; thêm `jobFor` chọn handler theo Kind của lịch |
| `tests/arch/testdata/m4_pending.txt` | 163 bỏ `getPresence`, `setPresence`; m4 bỏ `createReminder` | bỏ cả ba (cả hai bên đã hiện thực) |
| 14 tệp `internal/**/internal/store/models.go` (mã sinh) | — | lấy bản m4 rồi `make gen` |

**Lỗi sau gộp:**
- Build: `conversation.ErrNotFound` khai hai lần (`presence.go` của 163, `reminders.go` của m4), cả hai đều là `access.ErrNotFound` (cùng một giá trị). Xóa khai báo phía 163 trong `presence.go` (và import `access`), không đổi tên: đổi tên sẽ tạo hai tên cho cùng một sentinel; mọi chỗ dùng `conversation.ErrNotFound` vẫn đúng.
- Test giả định module chỉ có 1 việc nền (`len(jobs) != 1`) gãy khi có 2: `TestPresence_ExpiresAfterCleanup` (163, `handlers_presence_test.go`) và `TestFireDueReminders` (169, `reminders_test.go`) giờ chọn handler theo Kind của lịch đúng tên.

**Bằng chứng:**

| Kiểm | Lệnh | Kết quả |
|---|---|---|
| build | `go build ./...` | qua |
| vet | `go vet ./internal/modules/conversation/... ./internal/httpapi/ ./cmd/crm/` | qua |
| lỗi HTTP | `go test -count=1 ./internal/httpapi/ -run 'Errors'` | `ok elimaz.com.vn/crm/internal/httpapi 2.089s` |
| wire M4 | `go test -count=1 ./cmd/crm/ -run 'WireM4'` | `ok elimaz.com.vn/crm/cmd/crm 0.740s` |
| gói chạm, -race | `go test -race -count=1 ./internal/modules/conversation/... ./internal/httpapi/ ./cmd/crm/ ./internal/modules/channel/...` | `ok conversation 4.777s`, `ok conversation/internal/access`, `ok conversation/internal/presence`, `ok httpapi 34.705s`, `ok cmd/crm 12.800s`, `ok channel 3.952s` |
| kiến trúc | `go test -count=1 ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 14.657s` |
| lint | `golangci-lint run ./internal/modules/conversation/... ./internal/httpapi/... ./cmd/crm/... ./internal/modules/channel/...` | `0 issues.` |

Không gói nào ngoài `httpapi`, `cmd/crm` nhập `internal/modules/conversation` (`go list`).
| snapshot lỗi (thêm theo ghi chú reviewer) | `go test -count=1 ./internal/httpapi/ -run 'DomainError'` | `ok elimaz.com.vn/crm/internal/httpapi 0.674s` |

**Reviewer (lượt gộp): ĐẠT.** Không có phát hiện phải sửa. Đã đối chiếu: errors.go/errors_conversation.go/snapshot đủ
dòng hai bên, `ErrNotFound` vẫn là dòng kiểm đầu; thứ tự `Schedules[i]`/`Jobs[i]` khớp `areas()` (presence trước
reminders); xóa `ErrNotFound` ở presence.go đúng vì cùng giá trị `access.ErrNotFound`; hai test chọn handler theo Kind
giữ nguyên ý; `m4_pending.txt` bỏ ba operation đã hiện thực. Ghi chú: mẫu `-run 'Errors'` không khớp test snapshot (đã
chạy thêm `-run 'DomainError'`, dòng trên).

## Lượt sửa gộp m4 (lần 2, sau Task 149)

- **Xung đột:** chỉ `cmd/crm/wire_m4_test.go`, phần chú thích của `TestWireM4_Frame` (task-163 ghi hai lịch presence_cleanup + reminders; m4 ghi lệnh Page của Task 149 và "lịch duy nhất là reminders"). Thân test git tự gộp đủ: kênh fakechat, lệnh `channel.SubscribePageKind`/`UnsubscribePageKind` (149), hai lịch `conversation.presence_cleanup` (163) và `conversation.reminders` (169).
- **Sửa:** viết chú thích gộp cả ba phần. Sau gộp, `TestLintCleanOnRealCode` báo gocognit 24 > 20 ở `TestWireM4_Frame` → tách phần kiểm lịch ra hàm phụ `checkM4Schedules` (cùng ca kiểm, không bớt điều kiện). `tests/arch/testdata/m4_pending.txt`: git tự gộp, giữ việc 163 bỏ `getPresence`/`setPresence`. Không có tên khai trùng; `make gen` không chênh.
- **Bằng chứng:**
  - `go build ./...` → không lỗi; `go vet ./cmd/crm/ ./internal/modules/channel/... ./internal/modules/conversation/...` → không lỗi.
  - `go test -count=1 ./cmd/crm/ -run 'WireM4' -v` → `--- PASS: TestWireM4_Frame`, `ok elimaz.com.vn/crm/cmd/crm`.
  - `go test -race -count=1 ./cmd/crm/ ./internal/httpapi/ ./internal/modules/channel/... ./internal/adapters/facebook/... ./internal/modules/conversation/...` → tất cả `ok` (cmd/crm, httpapi 95s, channel, facebook, conversation, access, presence).
  - `go test -count=1 ./tests/arch/` → `ok ... 124.440s` (trước khi tách hàm: FAIL gocognit).
  - `golangci-lint run` trên các thư mục chạm → `0 issues.`; `make jscpd` → `Found 0 clones.`
- **Reviewer (lượt gộp lần 2):** ĐẠT, không có phát hiện phải sửa. Đã đối chiếu từng điều kiện kiểm của `TestWireM4_Frame` với bản m4: lệnh Page (149), hai lịch presence_cleanup (163) + reminders (169), fakechat, FIELD_ENC_KEYS sai đều còn; `checkM4Schedules` giữ đủ điều kiện; mã 149 giống hệt MERGE_HEAD; `m4_pending.txt` đúng.
