# Task 399 — Dọn mã trùng trong test (M0-x1)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- `make jscpd` trên main (8fd3e75) đỏ với 3 đoạn trùng mới: (1) `internal/platform/db/migrate_test.go` 79–84 ≈ 239–244 (chạy `Migrate`/`MigrateFS` hai lần song song); (2) `internal/platform/queue/errors_test.go` 117–122 ≈ `worker_test.go` 104–109 (handler trả `ErrPermanent` + enqueue); (3) `console/e2e/shell.spec.ts` 5–11 ≈ `staff.spec.ts` 174–180 (vòng `for vp of [DESKTOP, PHONE]` + `describe` + `test.use`).
- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có; chỉ thêm hàm trợ giúp không xuất trong tệp `_test.go` của gói `db_test`, `queue_test`.
- Gói phụ thuộc ngược (go list): không áp dụng (tệp `_test.go` không được gói nào nhập).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không đổi màn hình; chỉ test e2e: thêm `forEachViewport` vào `console/e2e/fixtures.ts`, dùng ở `shell.spec.ts`, `staff.spec.ts`.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -race ./internal/platform/db/... ./internal/platform/queue/...`; `cd console && npx tsc --noEmit && npx eslint . && npx playwright test e2e/shell.spec.ts e2e/staff.spec.ts`; `make jscpd`; `make ci` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` và `internal/testkit` không có hàm "chạy hai lần song song"; gói queue đã có `newFH`, `kind` trong `helpers_test.go`; console/e2e đã có `DESKTOP`, `PHONE` trong `fixtures.ts`.
- Dùng lại / viết mới vì: viết mới, mỗi cái trong đúng gói của nó (chỉ một gói dùng, không đưa vào `testkit`): `migrateTwiceConcurrently` trong `migrate_test.go`; `(*env).enqueuePermanentError` trong `queue/helpers_test.go` (dùng lại `newFH`, `kind`); `forEachViewport` trong `console/e2e/fixtures.ts` (dùng lại `DESKTOP`, `PHONE`, `test`). Cả ba đoạn cùng mục đích và cùng lý do thay đổi nên gộp. Không nới ngưỡng jscpd, không thêm tệp vào `ignore`, không sửa `jscpd-baseline.json`.

## Soát thiết kế

- 5.1–5.11: không áp dụng: chỉ đổi mã test, không đổi dữ liệu, module, HTTP, thư viện.
- 5.12 Module mẫu: hàm trợ giúp test đặt trong gói test của chính gói đó, theo luật dùng lại của CLAUDE.md.
- 6.1 Đồng thời: hàm gộp giữ nguyên cách chạy hai lần song song (`sync.WaitGroup.Go`, chờ hết rồi mới kiểm), nên hai test đồng thời vẫn kiểm đúng điều cũ.
- 6.2–6.13: không áp dụng: không đổi hành vi mã chạy thật; test giữ nguyên điều kiện kiểm (chỉ dời mã).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi sửa: jscpd đỏ (test thất bại) | `make jscpd` | `make jscpd` trên 8fd3e75 | `Found 3 clones (3 new)` … `ERROR: jscpd found 3 new clones not in the baseline (allowed: 0)` |
| Sau khi sửa (đã gộp main 8b7d8f0): không còn đoạn trùng, không nới ngưỡng, không thêm ignore | `make jscpd` | `make jscpd` | `Found 0 clones.`; `git diff main -- .jscpd.json jscpd-baseline.json` rỗng |
| Test db vẫn qua với -race (gồm hai test chạy song song đã gộp) | `TestMigrateConcurrentRunsApplyOnce`, `TestMigrateOutOfOrderConcurrentRunsApplyOnce` + cả gói | `go test -race -count=1 ./internal/platform/db/...` | `ok  elimaz.com.vn/crm/internal/platform/db 41.155s` |
| Test queue vẫn qua với -race | `TestPermanentErrorNotRecorded`, `TestPermanentErrorGoesDeadOnce` + cả gói | `go test -race -count=1 ./internal/platform/queue/...` | `ok  elimaz.com.vn/crm/internal/platform/queue 144.850s` |
| e2e console vẫn qua, cùng số test (shell 2, staff 9 khai báo; 21 lượt chạy) | `e2e/shell.spec.ts`, `e2e/staff.spec.ts` | `CRM_E2E_PORT=5399 npx playwright test e2e/shell.spec.ts e2e/staff.spec.ts --project=chromium` | `21 passed (7.1s)` |
| tsc, eslint, prettier, vitest, gitleaks, catalog, oasdiff, govulncheck, lint | các bước của `make ci` | `make ci` (lần 1, đã stage) rồi `make vuln oasdiff catalog-check jscpd test-guard gitleaks console-check` | golangci-lint `0 issues.`; `No vulnerabilities found.`; `no leaks found`; console-check EXIT 0; test-guard EXIT 0 sau khi ghi lý do ở mục dưới |
| `make ci` toàn bộ | — | `make ci` | Chưa xanh hết vì hai lỗi không do task này (xem "Việc phát hiện thêm"): `TestImager_HangKilledWithinDeadline` chập chờn (chạy riêng 3/3 qua); sau khi gộp main 8b7d8f0, `go vet` đỏ ở `internal/modules/catalog/classification_test.go:358` (`e.bus.signals undefined`) — có sẵn trên main |

## Test bị xóa hoặc tắt

Không xóa, không tắt test nào. Bộ kiểm test-guard báo hai dòng dưới đây; không có câu kiểm nào bị bớt:

- `internal/platform/queue/errors_test.go`: số câu kiểm giảm từ 13 xuống 12 — báo nhầm: biểu thức đếm `t\.Errorf\(` khớp cả `fmt.Errorf(` trong handler giả trả `ErrPermanent`; dòng đó dời sang `enqueuePermanentError` ở `helpers_test.go`. Mọi `t.Fatal*`/`t.Error*` của `TestPermanentErrorNotRecorded` giữ nguyên.
- `internal/platform/queue/worker_test.go`: số câu kiểm giảm từ 45 xuống 44 — báo nhầm cùng lý do (`TestPermanentErrorGoesDeadOnce`, mọi câu kiểm giữ nguyên).

## Khác kế hoạch

- Kế hoạch nói 2 đoạn trùng (db, queue); trên main có thêm đoạn thứ 3 ở `console/e2e` (Task 54 gộp sau khi kế hoạch ghi). Gộp cả ba để `make jscpd` xanh (xem Q1).

## Quyết định thay chủ shop

- **Q1 — Gộp luôn đoạn trùng thứ 3 ở `console/e2e`.** Chọn: gộp, vì mục đích task là `make jscpd` (và `make ci`) xanh trên main; để lại thì mọi task vẫn đỏ. Phương án khác: chỉ gộp 2 đoạn theo kế hoạch và mở task mới cho console (chậm, main vẫn đỏ). Dễ đảo ngược: chỉ là hàm trợ giúp test.

## Việc phát hiện thêm

- **main đang đỏ ở `go vet`** (sau `6099b06 merge: Task 28`): `internal/modules/catalog/classification_test.go:358,395,396` dùng `e.bus.signals`, nhưng Task 28 (`b3efe8c`) đã chuyển `RecordingBus` sang `testkit` với trường không xuất (có `Signals()`). Gộp riêng từng task thì qua, gộp chung thì lỗi. Không sửa ở đây (ngoài phạm vi, tránh xung đột với phiên khác cũng sửa); cách sửa đề xuất: thay `e.bus.signals = nil` bằng ghi `before := len(e.bus.Signals())` rồi kiểm phần mới `e.bus.Signals()[before:]`.

- `internal/imager` `TestImager_HangKilledWithinDeadline` chập chờn khi chạy cả `make ci` (máy tải nặng): `pipeline_test.go:571: script giả chưa chạy: stat …/pid: no such file` sau 1,01 s; chạy riêng 3/3 lần qua. Ngoài phạm vi (không chạm `imager`); cần task sửa test cho chờ script giả khởi động thay vì hạn cứng.
- Bộ kiểm test-guard đếm `fmt.Errorf(` là câu kiểm (biểu thức `t\.(…|Errorf)\(` không neo đầu từ), nên dời mã tạo lỗi bị báo "giảm câu kiểm". Đề xuất neo `\bt\.` — tệp quy trình chỉ chủ shop đổi.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lần 1, trên `git diff --cached` sau khi gộp main 8b7d8f0).

- Phát hiện phải sửa: không có. Đã đối chiếu: hai test migrate vẫn chạy song song thật và giữ ngữ nghĩa (một test sắp xếp, một test không); handler `ErrPermanent`, kind `t.perm`, `DedupeKey: "k"` giữ nguyên; `forEachViewport` giữ tên describe và thứ tự DESKTOP rồi PHONE, 21 lượt chạy khớp; không chạm cấu hình jscpd, baseline; hai báo "giảm câu kiểm" của test-guard là báo nhầm (không mất `t.Fatal*`/`t.Error*` nào).
- Ghi chú không bắt buộc: tên hàm trong mục Tái dùng ghi sai (`permanentErrorHandler`) → đã sửa thành `(*env).enqueuePermanentError`.
