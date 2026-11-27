# Task 233 — Chụp tồn hằng ngày (BC03)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 233 = M6-10). Loại việc (quy trình mục 3): A

Điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 sáng 27/11 và cho bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ
staging); điều kiện "M1–M5 đóng" của M6-00 coi như được miễn. Làm theo phần "Bổ sung Task 223" của kế hoạch (G4 khớp:
số dư và sổ ghi cùng transaction; mốc chụp dùng `created_at` của sổ). Đã soát lại 27/11: câu ghi số dư duy nhất của mã
thật là `ApplyDelta` (UPDATE) và `EnsureBalance` (chèn dòng 0) trong `queries/inventory/balances.sql`; mọi đường đổi số
dư (kể cả flash sale `flash_hold`/`flash_release` của 0540) đi qua `inventory.applyDelta` cùng dòng sổ.

## Ảnh hưởng

- Test sửa thêm: `internal/modules/reporting/schema_test.go` (tách hàm `appCount` dùng chung, xem Tái dùng).
- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): trong `internal/modules/reporting/snapshot.go` (tệp giữ chỗ
  của Task 228). `SnapshotHandler` thêm trường (`Now`, `Grace`, `MaxDays`, `Metrics`; kiểu đang là struct rỗng);
  `NewSnapshotHandler(Deps)` giữ chữ ký (1 chỗ gọi: `wiring.go` `TxHandlers`); `Kind()`, `Handle(...)` giữ chữ ký
  (chỗ gọi: worker qua `TxHandlers`, `cmd/crm/wire_m6_test.go` đọc `Kind().Name`). Thêm mới: `SnapshotKind()` (loại
  việc kèm chính sách), `DaysToSnapshot`, `SnapshotEvery`, `SnapshotGrace`, `SnapshotMaxDays`. `snapshotSchedules()`
  (không xuất; 1 chỗ gọi: `Schedules()` trong `wiring.go`) đổi từ `nil` sang một lịch. `SnapshotKindName` giữ.
- Gói phụ thuộc ngược (go list): `reporting` được nhập bởi `cmd/crm`, `internal/httpapi`, (test)
  `internal/platform/files`, `internal/modules/conversation`. Không đổi gì họ dùng ngoài `Schedules()` (cmd/crm nối
  lịch vào Ticker của worker). Test kéo theo: `cmd/crm` (`TestWireM6` đang đòi lịch rỗng tới Task 233, 235 → sửa
  thành đúng một lịch `reporting.stock_snapshot`).
- Operation OpenAPI bị chạm, oasdiff: không (việc nền, không HTTP).
- Chỗ dùng trên màn hình: không (màn tồn kho là Task 241; báo cáo tồn ngày đã qua đọc bản chụp ở Task 234).
- Câu sqlc, bảng, pha migration: tệp mới `queries/reporting/snapshot.sql` (hai câu `SnapshotBounds`, `TakeSnapshot`),
  `make gen` sinh `internal/modules/reporting/internal/store/snapshot.sql.go`. Bảng ghi: `reporting.stock_snapshot_days`,
  `reporting.stock_daily_snapshots` (chỉ INSERT, đúng quyền của 0601). Đọc: view công bố `inventory.stock_available_v`,
  `inventory.stock_ledger_v` (đã có trong `viewReadExceptions["reporting"]` của Task 226). Không migration; chỉ mục
  `stock_ledger (tenant_id, created_at)` có từ 0603.
- Test sẽ chạy: `go test -race -count=1 -run '^(TestDaysToSnapshot.*|TestSnapshot.*)$' ./internal/modules/reporting/`;
  `go test -count=1 -run 'Wire' ./cmd/crm/`; `go test -count=1 ./tests/arch/`; phần còn lại (cả gói reporting, cmd/crm,
  httpapi) ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`queue.Kind`,
  `queue.DefaultPolicy`, `queue.ErrPermanent`, `schedules.Schedule`, `metrics.Registry.Gauge`, `tenancy.WithTenant`);
  `reporting.{VN, Today, DayCutoff}` (Task 227, `period.go`); `reportseed.Ledger` (Task 227) để dựng sổ; mẫu việc định
  kỳ theo tenant `promotion.birthdayRunHandler` / `order` `expireDrafts` (Kind có `ExpiresAfter = Every`);
  `testkit.OpenStaff`, `Staff.Tenant`, `SetRapidChecks`.
- Dùng lại / viết mới vì: dùng lại `DayCutoff`, `Today`, `VN` cho mốc 24:00 giờ VN (không viết bản thứ hai);
  `reportseed.Ledger` cho test theo ca; khung `queue.Handler` + `schedules.Schedule` có sẵn. Viết mới `DaysToSnapshot`
  (luật chọn ngày của Q7, riêng của chụp tồn, không có hàm tương tự) và hai câu SQL. Test thuộc tính chèn sổ và số dư
  bằng một transaction của chủ bảng (hàm cục bộ trong test) thay cho 80 lần `reportseed.Ledger` mỗi lượt để 200 lượt
  rapid chạy nhanh; luật "số dư = tổng sổ" giữ như `Ledger`.
- jscpd (ci-task lượt 1) báo hai đoạn trùng mới: hàm đếm trong ngữ cảnh tenant của `snapshot_test.go` trùng
  `schemaEnv.appCount` (Task 226) → gộp: tách hàm `appCount(t, staff, tenant, q, args…)` trong `schema_test.go`, cả hai
  gọi nó (cùng mục đích, cùng lý do đổi); hai goroutine chạy `Handle` trong `TestSnapshot_Concurrent` → một hàm
  `startRun`.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1 Chủ dữ liệu: hai bảng chụp tồn thuộc `reporting` (0601, Task 226). 5.2: chỉ ghi bảng của `reporting`.
- 5.3: đọc tồn qua `inventory.stock_available_v`, `inventory.stock_ledger_v` (view công bố, có trong luật kiến trúc).
- 5.4, 5.5: chụp một ngày là một câu SQL (đọc số dư và sổ trong cùng ảnh dữ liệu, chèn dòng ngày + dòng chụp); các ngày
  của một lượt nằm trong transaction của việc. Không sự kiện, không lệnh.
- 5.6, 5.8, 5.9, 5.10, 5.11: không áp dụng (không gọi ngoài, không HTTP, không đường ghi API, không Valkey, không thư
  viện mới).
- 5.7: việc theo tenant: worker chạy `Handle` trong `WithTenant`; câu SQL lấy `tenant_id` từ `app.tenant_id`; việc
  không có tenant → `ErrPermanent`.
- 5.12: theo mẫu việc định kỳ theo tenant có sẵn (`promotion`, `order` drafts); SQL tĩnh qua sqlc.

Mục 6 (đồng thời):
- 6.1 Hai lượt cùng lúc (lịch trùng, worker chạy lại): `INSERT … ON CONFLICT (tenant_id, day) DO NOTHING` trên dòng
  ngày; lượt sau chờ lượt trước commit rồi không chèn gì (dòng chụp chỉ chèn khi dòng ngày vừa chèn). Không khóa dòng có
  sẵn; chỉ chèn dòng mới; ngày theo thứ tự tăng dần ở mọi lượt nên hai lượt chờ nhau cùng một chiều (không deadlock).
  Test `TestSnapshot_Concurrent`.
- 6.2: không phải đường API; việc lặp lại an toàn nhờ khóa tự nhiên `(tenant_id, day)`.
- 6.3: ADR-46 nhóm 12a (`stock_snapshot_days` → `stock_daily_snapshots`): mỗi câu chèn dòng ngày trước rồi dòng chụp.
- 6.4, 6.6: lỗi database → việc trả lỗi, hàng đợi thử lại theo `DefaultPolicy` (8 lần) rồi `dead` → cảnh báo
  `dead:reporting.stock_snapshot` có sẵn của M0. Kind dùng đúng `DefaultPolicy`, không đặt `ExpiresAfter` (sau review:
  hết hạn 15 phút làm việc lỗi thành `expired` sau lần thử 4–5, không tới `dead`, không cảnh báo). Việc các kỳ
  không dồn vì lịch chạy cùng worker; khi worker ngừng thì không sinh việc mới, kỳ sau tự bù ngày thiếu.
- 6.5: READ COMMITTED; mỗi ngày một câu nên số dư và sổ cùng một ảnh dữ liệu.
- 6.8: chết giữa chừng → transaction lùi, không ngày nào nửa vời; lượt sau bù.
- 6.9: mỗi lượt tối đa 7 ngày (`MaxDays`), trong hạn 30 s của việc; mốc chụp chỉ lấy ngày đã qua 24:00 giờ VN ít nhất
  10 phút (`Grace`), đủ cho transaction ghi sổ bắt đầu trước mốc kịp commit (hạn câu lệnh 30 s).
- 6.7, 6.10, 6.12, 6.13: không áp dụng (không gọi ngoài, không tiền, không dữ liệu cá nhân — kết quả việc chỉ có ngày và
  số dòng; không quyền người dùng).
- 6.11: quy luật "giá trị chụp = tổng delta có `created_at` < mốc" có test rapid (`TestSnapshotProperty`).

## Quyết định thay chủ shop

- **QĐ-233-1 — kẹp giá trị chụp âm về 0.** Sổ tồn ghi `created_at` = `now()` lúc **bắt đầu** transaction, nên một
  transaction bắt đầu 23:59:59 (trừ 2) có thể commit sau transaction bắt đầu 00:00:01 (nhập 5). Khi đó "số dư hiện tại −
  sổ từ mốc" của ngày trước ra −2: không ứng với số dư nào từng commit, và chèn vào bảng chụp sẽ vỡ CHECK ≥ 0, làm việc
  chết 8 lần rồi `dead` và chặn mọi ngày sau. Chọn: mỗi ngăn kẹp về 0 (`greatest(…, 0)` trong `TakeSnapshot`), test
  `TestSnapshot_NegativePrefixClamped`. Phương án khác: (a) để việc lỗi và báo động — chặn cả chuỗi chụp vì một ca
  hợp lệ; (b) đổi sổ sang `clock_timestamp()` lúc ghi — đổi module inventory (vùng rủi ro cao), ngoài phạm vi; vẫn
  không loại hết ca commit ngược thứ tự. Dễ đảo ngược: bỏ `greatest` trong một câu SQL, không migration.
- **Gauge `stock_snapshot_lag_days` ghi trong `Handle` (trước commit)**, nhãn là tenant, giống `track_poll` của
  fulfillment. Nếu commit lỗi, việc thử lại và ghi lại gauge ngay lượt sau (15 phút), nên số sai tạm không kéo dài;
  hàng đợi không có móc sau-commit để làm khác.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Luật chọn ngày: từ max(last+1, ngày sổ đầu) tới ngày có mốc ≤ now − grace, tối đa maxDays | `TestDaysToSnapshot` | `go test -race -count=1 -run '^(TestDaysToSnapshot.*|TestSnapshot.*)$' ./internal/modules/reporting/` | `ok … reporting 13.104s` |
| Sổ của kế hoạch: ngày 15 {20,0,0,0}, 16 {20,0,2,0}, 17 {18,0,0,0}; lần hai rỗng, số dòng không đổi | `TestSnapshot_PlanLedger` | như trên | ok |
| 00:05 chưa chụp ngày 17, 00:11 chụp | `TestSnapshot_Grace` | như trên | ok |
| Bù ngày: 09–15, rồi 16–17, rồi rỗng; gauge `stock_snapshot_lag_days` 2, 0, 0 | `TestSnapshot_CatchUp` | như trên | ok |
| Hai lượt cùng lúc: lượt B chờ khóa rồi không chèn; mỗi ngày một dòng, 3 dòng chụp | `TestSnapshot_Concurrent` | như trên | ok |
| Biến thể mọi ngăn 0 / tạo sau mốc → không dòng | `TestSnapshot_ZeroAndLateVariants` | như trên | ok |
| Sổ ghi muộn trước mốc sau khi chụp → bản chụp không đổi | `TestSnapshot_AppendOnlyAfterLateLedger` | như trên | ok |
| Tenant B không đọc, không ghi của A | `TestSnapshot_TenantIsolation` | như trên | ok |
| QĐ-233-1 kẹp âm về 0 | `TestSnapshot_NegativePrefixClamped` | như trên | ok |
| Việc không tenant → `ErrPermanent` | `TestSnapshot_NoTenant` | như trên | ok |
| Kind, lịch 15 phút theo tenant, Grace 10 phút, MaxDays 7 | `TestSnapshotKindAndSchedule`; `TestWireM6` | như trên; `go test -count=1 -run 'Wire' ./cmd/crm/` | ok; `ok … cmd/crm 1.029s` |
| Thuộc tính: giá trị chụp = tổng delta trước mốc (200 lượt rapid) | `TestSnapshotProperty` | `go test -count=1 -run '^TestSnapshotProperty$' -v ./internal/modules/reporting/` | `--- PASS: TestSnapshotProperty (7.63s)` |
| Cả gói reporting (sau sửa lint, sau gộp main fc263ee7) | toàn gói | `go test -race -count=1 ./internal/modules/reporting/` | `ok … reporting 31.704s` |
| ci-task lượt 1 (từ xa, GitHub, run 45979512746): test của mọi gói ảnh hưởng | cmd/crm, reporting (-race); httpapi, httpapi/web, conversation, files, realtime | ci-task | test (1)–(4) success; cổng tĩnh ĐỎ (gocognit, nolintlint, revive, jscpd) → đã sửa |
| Cổng tĩnh sau sửa (chạy hẹp trên máy, xem dưới) | lint, jscpd, kiến trúc, mã sinh | `golangci-lint run -j 2 ./internal/modules/reporting/...`; `jscpd --config .jscpd.json --similarity --baseline jscpd-baseline.json --fail-on-new-clones=0 internal/modules/reporting internal/testkit/reportseed`; `go test -count=1 -skip 'TestLint' ./tests/arch/`; `make gen` + `git status` | `0 issues.`; `Found 0 clones.`; `ok … tests/arch 3.000s`; không chênh |

ci-task lượt 2, 3 không chạy được từ xa: GitHub từ chối job ("recent account payments have failed or your spending
limit needs to be increased", run 45983129743), Blacksmith hết phút tháng (2718 ≥ 2700), Ubicloud không nhận máy sau
180 s (run 45983278105, tạm hỏng tới 18:07). Phần đổi sau lượt 1 chỉ là test của reporting và đổi tên tham số `max` →
`maxDays`; đã chạy bù trên máy đúng các cổng bị đỏ ở lượt 1 (bảng trên). Toàn bộ `TestLintCleanOnRealCode` (lint cả
repo) chưa chạy lại: điều phối viên chạy ở merge-test.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi `var SnapshotKind = queue.Kind{…}`; mã là `func SnapshotKind() queue.Kind` (cùng kiểu
  `RunCommandKind()` của Task 228, tránh biến cấp gói — luật `gochecknoglobals`); chính sách đúng `queue.DefaultPolicy`.
- `SnapshotBounds` trả ngày bằng `to_char(max(day), 'YYYY-MM-DD')` (không phụ thuộc `DateStyle` của phiên; gợi ý reviewer).
- Thuộc tính chạy 200 lượt (số lượt chung của gói trong TestMain), nhiều hơn 100 của kế hoạch.

## Việc phát hiện thêm

- Hạ tầng CI 27/11 chiều: GitHub Actions báo lỗi thanh toán/hạn mức chi tiêu (mọi job không chạy), Blacksmith hết phút
  tháng, Ubicloud không nhận máy. Cần chủ shop xem mục Billing của GitHub (việc ngoài máy, không tự làm).
- (Reviewer gợi ý) Việc định kỳ có `ExpiresAfter` ngắn của `promotion` (sinh nhật) và `order` (`expireDrafts`) có thể
  cũng không bao giờ tới `dead` khi lỗi kéo dài (hết hạn trước lần thử thứ 8), nên không có cảnh báo. Chưa kiểm, ngoài
  phạm vi Task 233.

## E2E viết sẵn, chưa chạy

Không có (kế hoạch Task 233 không có test e2e).

## Reviewer

Kết luận: ĐẠT (lượt 2, 27/11).

- Lượt 1: CẦN SỬA — `SnapshotKind()` đặt `ExpiresAfter = 15 phút`: việc lỗi mãi thành `expired` sau lần thử 4–5 (lần
  chờ kế tiếp 1 giờ), không bao giờ tới `dead`, không có cảnh báo `dead:reporting.stock_snapshot` mà kế hoạch đòi; không
  có luật cảnh báo cho `expired`. Đã sửa: dùng đúng `queue.DefaultPolicy`, test đòi `k.Policy == queue.DefaultPolicy`.
  Gợi ý thêm (đã làm): `SnapshotBounds` dùng `to_char(max(day), 'YYYY-MM-DD')` thay cho `::text` (không phụ thuộc
  `DateStyle`); sửa tên test ở 6.1; thêm `schema_test.go` vào Ảnh hưởng.
- Lượt 2: ĐẠT, không phát hiện phải sửa. Mọi ký hiệu khớp mục Ảnh hưởng; mọi dòng bằng chứng có test kiểm đúng điều
  đó; không test nào bị xóa, tắt hay nới.

## Sửa jscpd (lượt sau commit b5a134a8)

- Lỗi: jscpd báo mã lặp mới giữa `handlers_catalog.go` (`optString`) và `reporting/snapshot.go` (`optional`). Sửa: bỏ hàm `optional`, thay bằng hai khối `if` gán con trỏ (không đổi hành vi).
- Bằng chứng: ci-task (chạy trên Mac, `CRM_CI_RUNNER=mac`) XANH: jscpd 0 clone, lint 0 issue, test kiến trúc ok, `-race` cmd/crm + reporting ok, test thường httpapi, httpapi/web, conversation, files, realtime ok. (Lần đầu gen-check đỏ chỉ vì sửa chưa stage; stage rồi chạy lại.)
- Reviewer: ĐẠT, không phát hiện phải sửa (hai khối `if` tương đương hàm cũ; `b` không bị ghi sau đó).
