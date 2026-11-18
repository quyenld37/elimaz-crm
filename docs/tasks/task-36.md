# Task 36 — Đối chiếu tồn hằng đêm

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 36, M1-36, KH13). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu cũ nào đổi chữ ký. Thêm mới trong `inventory`:
  `Reconcile`, `Run`, `Mismatch`, `ReconcileSchedule`, `ReconcileEvery`, `ReconcileBatch`, `NewReconcileHandler`,
  `StockMismatchAlertKind`, `NewStockMismatchCommand`. `shortagealert.go`: tách phần gửi cảnh báo (không xuất ra ngoài)
  thành hàm dùng chung trong gói để lệnh mới gọi lại; `NewShortageAlertCommand` giữ nguyên chữ ký (1 chỗ dùng: `cmd/crm/main.go`).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/order`, `inventory/invtest` (và test
  của `fulfillment` qua invtest). Chỉ `cmd/crm` đổi (đăng ký lịch, handler, lệnh).
- Operation OpenAPI bị chạm, oasdiff: không (việc nền, không có đường HTTP).
- Chỗ dùng trên màn hình: không. View `inventory.reconcile_runs_v` cho M6, M7 (dòng đã có trong `docs/views.md`).
- Câu sqlc, bảng, pha migration: migration `0122_inventory_reconcile.sql` pha mở rộng (CREATE TABLE `inventory.reconcile_runs`
  có RLS, view `reconcile_runs_v`, GRANT, chèn dòng `platform.schedules` 19:30 UTC kế tiếp — quyết định D2 của Task 24).
  Câu mới `queries/inventory/reconcile.sql`: `ReconcileBatch` (chỉ đọc stock_balances, stock_ledger, stock_reservations),
  `InsertReconcileRun`. `make gen` sinh lại `internal/modules/inventory/internal/store`.
- Test sẽ chạy: `go test -race ./internal/modules/inventory/...` (cả bộ: inventory là vùng tồn kho), `./cmd/crm/...`,
  `./internal/platform/schedules/...` (không đổi mã nhưng test lịch chạy với migration mới), gói phụ thuộc ngược
  (`order`, `fulfillment`, `httpapi`), `tests/e2e` (TestWorkerRoleRunsSchedules), `tests/arch`; cuối cùng `make ci` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `schedules.Registry.Define`/`Ticker` (lịch theo tenant),
  `queue.Handler` (việc transactional, worker tự đặt WithTenant), `commands.CommandKind` + `commands.Handler` (gửi
  cảnh báo ngoài transaction), `observability.Alerter` qua interface `inventory.Notifier` có sẵn (Task 35),
  `testkit.OpenStaff`, helper test `invEnv` của gói, `invtest.SeedStock`. Tìm "reconcile", "sum(delta)": chỉ có
  `checkInvariants`/`checkLedgerAndHolds` trong test (không phải mã sản phẩm).
- Dùng lại / viết mới vì: dùng lại toàn bộ các phần trên; lịch không cần đổi gói `schedules` (dòng chèn sẵn trong
  migration, D2). Lệnh cảnh báo lệch sổ dùng lại phần gửi của lệnh thiếu hàng (tách thành hàm chung trong gói, commit
  cấu trúc riêng) thay vì chép. Câu đối chiếu viết mới vì chưa có câu sản phẩm nào so sổ với số dư; hai helper test
  cùng việc giữ nguyên (test của task khác, đổi ngoài phạm vi).

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `reconcile_runs` thuộc inventory (ADR-46 nhóm 11 đã ghi), schema `inventory`.
- 5.2 Không ghi bảng module khác: chỉ ghi `inventory.reconcile_runs`, việc trên `platform.jobs` qua `Enqueue`.
- 5.3 Đọc module khác qua view: không đọc bảng module khác (chỉ ba bảng của inventory).
- 5.4 Nguyên tử: ghi dòng kết quả và lệnh cảnh báo cùng transaction của việc định kỳ.
- 5.5 Không nguyên tử đi sau commit: gửi Telegram là lệnh `commands` chạy sau commit.
- 5.6 Gọi ra ngoài: lệnh cảnh báo; `AutoRetryNotSent` như lệnh thiếu hàng (Alerter chống lặp theo khóa).
- 5.7 Ngữ cảnh shop: lịch PerTenant, worker chạy handler trong WithTenant; bảng mới bật RLS, khóa ngoại tenant.
- 5.8 Không HTTP: không.
- 5.9 Đường ghi HTTP: không áp dụng (việc nền, chống trùng bằng DedupeKey theo kỳ của Ticker và DedupeKey của lệnh = mã lần chạy).
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện mới: không.
- 5.12 Module mẫu: hàm đọc trong gói gốc, SQL qua sqlc, test `_test` trên PostgreSQL thật.

Mục 6:
- 6.1 Đồng thời: chỉ đọc số dư; mỗi lô một câu SELECT nên số dư, sổ và giữ hàng của một dòng được so trên cùng một
  snapshot (mọi đường đổi tồn ghi ba thứ trong cùng transaction) → không báo lệch giả khi đơn đang chạy. Giữa hai lô
  có thể khác snapshot, không ảnh hưởng vì mỗi dòng chỉ so trong một câu.
- 6.2 Trùng: DedupeKey theo kỳ (một việc mỗi tenant mỗi đêm); lệnh cảnh báo DedupeKey = mã lần chạy.
- 6.3 Deadlock: không khóa dòng nào (SELECT thường, không FOR UPDATE); chỉ chèn dòng mới.
- 6.4 Lỗi tạm: worker chạy lại theo Policy.
- 6.5 Cách ly: READ COMMITTED, mỗi lô một câu (xem 6.1).
- 6.6 Việc lỗi: như lịch dọn — 1 lần, quá hạn thì expired, lỗi thành dead và luật cảnh báo hàng đợi báo; kỳ sau chạy lại.
- 6.7 429: lệnh cảnh báo chờ đúng RetryAfter của Telegram (dùng chung phần gửi).
- 6.8 Chết giữa chừng: transaction lùi, không ghi dòng kết quả; kỳ sau chạy.
- 6.9 Treo: duyệt theo khóa (variant_id, warehouse_id) từng lô 2.000 dùng index khóa chính; mỗi câu ngắn; handler 30 s
  (đo 50.000 số dư trong test).
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: đã có rapid của Task 28/35 cho bất biến; test này kiểm đối chiếu phát hiện đúng dòng sai.
- 6.12 Dữ liệu cá nhân: `mismatches` chỉ có mã biến thể, mã kho, tên phép so, ngăn, hai số; văn bản cảnh báo chỉ có số.
- 6.13 Quyền: không có đường HTTP; crm_app chỉ được SELECT, INSERT `reconcile_runs` (không UPDATE, DELETE).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): tách phần gửi cảnh báo của lệnh thiếu hàng, hành vi không đổi | `TestShortageAlertCommand`, `TestStocktake_ShortageKeepsHolds` | `go test -race -run 'TestShortageAlertCommand\|TestStocktake_ShortageKeepsHolds' ./internal/modules/inventory/` | `ok elimaz.com.vn/crm/internal/modules/inventory 6.371s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Commit 1 (cấu trúc, tách `notify` trong `shortagealert.go`): **ĐẠT**. Không phát hiện về tính đúng; key, severity,
văn bản, log, not_sent, RetryAfter giữ nguyên. Ghi chú: `notify` cố định severity normal (lệnh lệch sổ cũng normal).
