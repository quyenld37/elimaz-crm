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

**Lượt 2 (gộp main 37a25a1, Task 32 sổ COD):** không đổi ký hiệu Go nào của Task 36. Xung đột duy nhất ở
`cmd/crm/main.go` (`runWorker`): main đổi `newEvents()` thành trả `(*events.Registry, error)`; giữ cách gọi mới của main
và giữ dòng đăng ký `inventory.NewReconcileHandler`. Mã sinh: `make gen` thêm `InventoryReconcileRun(sV)` vào
`internal/modules/cash/internal/store/models.go` (sqlc chép mọi bảng vào mỗi module). Migration 0119 (main) và 0122
(task này) không đụng nhau. Test sẽ chạy: `go test -race ./cmd/crm/... ./internal/modules/inventory/... ./internal/modules/cash/...`,
rồi `make ci` một lần.

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

Lượt 2: không viết mã mới, không có gì để tái dùng thêm; giải xung đột giữ đủ đăng ký của hai bên.

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

Lượt 2: chỉ gộp main; thiết kế không đổi (mục 5, 6 ở trên giữ nguyên). Kiểm lại: worker vẫn đăng ký handler
đối chiếu cùng consumer sự kiện của sổ COD (5.2, 5.4 không đổi).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): tách phần gửi cảnh báo của lệnh thiếu hàng, hành vi không đổi | `TestShortageAlertCommand`, `TestStocktake_ShortageKeepsHolds` | `go test -race -run 'TestShortageAlertCommand\|TestStocktake_ShortageKeepsHolds' ./internal/modules/inventory/` | `ok elimaz.com.vn/crm/internal/modules/inventory 6.371s` |
| KH13: dữ liệu sạch sau chuỗi thao tác M1-28–35 (nạp, giữ, gửi, giao, nhả, phiếu nhập/xuất/lỗi, kiểm kho) → `ok`; held của X lệch giữ hàng → đúng 1 mục `{X, held_vs_reservations}`; on_hand của Y sửa thẳng → mục `ledger_vs_balance` đúng Y; `reconcile_runs_v.mismatch_count` = số mục; mismatches chỉ có khóa cho phép; tenant B không thấy lần chạy của A | `TestReconcile_ReportsExactVariant` | `go test -race -count=1 -v -run TestReconcile ./internal/modules/inventory/` | `--- PASS: TestReconcile_ReportsExactVariant (0.74s)` |
| Ba phép so riêng: sửa held không kèm sổ → 2 mục (sổ ngăn held + giữ hàng); in_transit lệch giữ hàng → `in_transit_vs_reservations` | `TestReconcile_EachCheck` | như trên | `--- PASS: TestReconcile_EachCheck (0.57s)` |
| 50.000 số dư (5.000 biến thể × 10 kho) xong dưới 60 s, statement_timeout 30 s, đếm đủ | `TestReconcile_LargeTenant` | như trên | `50.000 số dư: 381.407917ms`, `--- PASS (9.61s)` (gồm nạp dữ liệu) |
| Lịch: dòng chèn sẵn 19:30 UTC kế tiếp; Tick 10:00 UTC → 0 việc; Tick 19:30 UTC → đúng 1 việc mỗi tenant (2); worker chạy → mỗi tenant 1 lần chạy; tenant lệch có đúng 1 lệnh cảnh báo; lệnh gửi khóa `stock-mismatch:<tenantId>`, normal, văn bản `2 mã hàng lệch sổ` (chỉ số); việc không tenant → rejected | `TestReconcile_NightlySchedule` | như trên | `--- PASS: TestReconcile_NightlySchedule (0.52s)` |
| Cả bộ vùng tồn kho và gói nối | toàn bộ test `inventory`, `cmd/crm` | `go test -race -count=1 ./internal/modules/inventory/... ./cmd/crm/...` | `ok elimaz.com.vn/crm/internal/modules/inventory 62.413s` |
| Gói phụ thuộc ngược, lịch, e2e worker, kiến trúc (bản đã merge main cc42a68) | order, fulfillment, httpapi, schedules, e2e, arch | `make ci` (bước test) | `ok …/order 135.692s`, `ok …/fulfillment 46.101s`, `ok …/httpapi 255.609s`, `ok …/platform/schedules 64.538s`, `ok …/tests/e2e 68.289s`, `ok …/tests/arch 23.594s` |
| Cổng kiểm còn lại | build, vet, lint, gen-check, sqlc-vet, vuln, oasdiff, catalog-check, jscpd, test-guard, gitleaks, console-check | `make ci` rồi `make vuln oasdiff catalog-check jscpd test-guard gitleaks console-check` | build…sqlc-vet qua trong `make ci`; lượt sau `exit=0`, `no leaks found`. Bước test của `make ci` có 1 lỗi ngoài phạm vi: `TestImager_HangKilledWithinDeadline` (xem Việc phát hiện thêm), chạy riêng 3 lần đều `ok` |
| Lượt 2: bản đã gộp main 37a25a1 (Task 32) — xung đột `cmd/crm/main.go` giải giữ cả hai; mã sinh khớp | gói nối, kho, COD, phụ thuộc ngược, e2e, kiến trúc; gen-check | `go test -race -count=1 ./cmd/crm/... ./internal/modules/cash/...` rồi `make ci` | `ok …/cmd/crm 1.532s`, `ok …/cash 17.983s`; `make ci` `exit=0`: `ok …/inventory 261.293s`, `ok …/cash 51.314s`, `ok …/order 76.560s`, `ok …/fulfillment 24.400s`, `ok …/httpapi 209.716s`, `ok …/schedules 66.590s`, `ok …/tests/e2e 86.409s`, `ok …/tests/arch 23.831s`, `ok …/internal/imager 9.916s`, `no leaks found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `Reconcile` ngoài việc đọc còn ghi luôn dòng `inventory.reconcile_runs` (trả `Run.ID`); "chỉ đọc" giữ đúng cho số dư,
  sổ, giữ hàng (không khóa, không sửa). Lý do: kết quả và dòng ghi cùng một transaction, test đọc được view.
- Mục lệch có thêm khóa `bucket` (chỉ ở `ledger_vs_balance`) ngoài năm khóa của kế hoạch, để biết ngăn nào lệch sổ;
  không phải dữ liệu cá nhân.
- Ca "sửa thẳng held + 1 của X → đúng 1 mục held_vs_reservations": sửa held mà không kèm sổ thì lệch cả phép (a) lẫn
  (b) → 2 mục (kiểm ở `TestReconcile_EachCheck`). Ca "đúng 1 mục" trong `TestReconcile_ReportsExactVariant` sửa held
  kèm một dòng sổ held (giả lỗi mất dòng giữ hàng).

## Việc phát hiện thêm

- `internal/imager` `TestImager_HangKilledWithinDeadline` (Task 44, vừa gộp từ main) lỗi một lần trong `make ci` khi máy
  tải nặng ("script giả chưa chạy … pid: no such file"): hạn 1 s cho script giả khởi động quá ngắn. Chạy riêng 3/3 qua.
  Không sửa (ngoài phạm vi).
- Lô của câu đối chiếu cố định 2.000; handler việc nền giới hạn 30 s. 50.000 số dư mất ~0,4 s, nên còn xa giới hạn;
  khi một tenant tới vài triệu dòng số dư cần chia việc theo kho. Mỗi câu cũng cộng toàn bộ sổ của 2.000 dòng số dư,
  nên thời gian tăng theo độ dài lịch sử sổ (test chỉ có một dòng sổ mỗi số dư): khi sổ dài nhiều năm cần đo lại, có
  thể thêm số dư chốt theo kỳ (reviewer ghi chú).
- Khóa cảnh báo cố định mỗi tenant (`stock-mismatch:<tenantId>`, theo kế hoạch): nếu Alerter chống lặp lâu hơn 24 giờ
  thì đêm lệch liên tiếp có thể không báo lại; cần soát khi chỉnh luật chống lặp (reviewer ghi chú).

## Quyết định thay chủ shop

1. **Giờ chạy đầu bằng dòng chèn sẵn ở migration 0122** (D2 của Task 24): `date_trunc('day', now(), 'UTC') + 19:30`,
   đã qua thì cộng 1 ngày; `ON CONFLICT DO NOTHING`. Không đổi gói `schedules`. Đảo ngược: xóa dòng.
2. **Việc định kỳ chạy 1 lần, quá 24 giờ chưa chạy thì expired** (như lịch dọn, spec 04 mục 5.1: lỗi thì chạy kỳ sau;
   việc dead được luật cảnh báo hàng đợi báo). Phương án khác: thử lại 8 lần — có thể báo trùng nhiều lần trong đêm.
3. **"n mã hàng" = số biến thể khác nhau có lệch** (một biến thể lệch nhiều phép hay nhiều kho vẫn tính 1);
   `mismatch_count` của view = số mục. Khóa cảnh báo `stock-mismatch:<tenantId>` theo kế hoạch (Alerter chống lặp theo
   khóa). Lệnh cảnh báo dùng lại phần gửi của lệnh thiếu hàng (commit cấu trúc riêng e42b86d).
4. **Lưu đủ mọi mục lệch** trong `mismatches` (không cắt) để `mismatch_count` luôn đúng; trường hợp xấu (mọi dòng
   lệch) là vài MB một đêm, chấp nhận được.
5. **Thêm CHECK `ok = (jsonb_array_length(mismatches) = 0)`** và `checked >= 0` để dòng kết quả không tự mâu thuẫn.

## Reviewer

Commit 1 (cấu trúc, tách `notify` trong `shortagealert.go`): **ĐẠT**. Không phát hiện về tính đúng; key, severity,
văn bản, log, not_sent, RetryAfter giữ nguyên. Ghi chú: `notify` cố định severity normal (lệnh lệch sổ cũng normal).

Commit 2 (hành vi: migration 0122, câu đối chiếu, `reconcile.go`, lịch, handler, lệnh, nối `cmd/crm`; bản đã merge main
cc42a68): **ĐẠT**. Không phát hiện về tính đúng: keyset và một snapshot mỗi lô đúng, lịch 19:30 UTC giữ được qua
`ON CONFLICT DO NOTHING`, migration 0122 chạy được nhờ `WithAllowOutofOrder`, cảnh báo không có dữ liệu cá nhân, quyền
crm_app chỉ SELECT/INSERT. Hai ghi chú không bắt buộc (sổ dài nhiều năm; khóa cảnh báo cố định) đã chép vào "Việc phát
hiện thêm".

Lượt 2 (gộp main 37a25a1, Task 32 sổ COD; giải xung đột `cmd/crm/main.go` `runWorker`, `make gen`): **ĐẠT**. Không
phát hiện về tính đúng: `newEvents()` theo chữ ký mới của main ở cả ba chỗ gọi, lịch và handler đối chiếu tồn giữ
nguyên, `cash/internal/store/models.go` chỉ thêm hai kiểu đối chiếu do sqlc sinh, migration 0119 và 0122 không đụng
nhau. Ghi chú không bắt buộc: `newEvents()` dựng ba registry riêng (cách của main, không do bản gộp).
