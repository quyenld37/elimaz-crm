# Task 447 — Database mẫu của test: đặt tên theo nội dung, không theo giờ; mỗi bộ chỉ giữ bản mới nhất

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task chung x-tpl của điều phối viên 21/11, dòng trong `danh-so-task-toan-cuc.md`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không. `StartPostgres`, `StartPostgresSeeded`, `OpenStaffSeeded`,
  `OpenAppDB`, `Seed`, `FixedUUID` giữ chữ ký và nghĩa. Chỉ đổi ký hiệu không xuất trong `internal/testkit`:
  `cleanupStale` (nhận khóa dọn `int64` thay kiểu `cleanupLocks`, bỏ kiểu này và `serverLocks`), `dropStale`,
  `seedLockKey` → `nameLockKey` (cùng giá trị 727393, nay là khóa theo tên của mọi mẫu), thêm `pruneOlderHours`,
  `dropTemplateDB`. Chỗ dùng: chỉ trong `internal/testkit` (pgtemplate.go, pgseed.go, pgtemplate_test.go, pgseed_test.go).
- Gói phụ thuộc ngược (go list): mọi gói test nhập testkit (172 tệp `.go`); hành vi họ thấy không đổi (vẫn database đã
  migrate, dữ liệu now() của migration cũ tối đa một giờ). Đổi testkit → cổng kiểm của task chạy toàn bộ test.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không (chỉ câu quản trị trên `pg_database` của máy chủ thử).
- Test sẽ chạy: `go test -race ./internal/testkit/` (mọi test cũ của pgtemplate/pgseed + test mới mô phỏng 3 tiến trình
  × 3 giờ giả × 2 bộ migration × {mẫu migration, mẫu có dữ liệu}), `TestReconcile_NightlySchedule` (inventory, dữ liệu
  now() của 0122), rồi cổng kiểm của task (toàn bộ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ phủ `internal/platform/*`.
  Trong testkit: `templateName`, `templateState`, `ensureTemplate`, `ensureSeeded`, `createFromTemplate`,
  `cleanupStale`/`dropStale`, `isCode`, `withAdmin`, `dropAtCleanup`, `privateLocks` (test).
- Dùng lại / viết mới vì: dùng lại cả cách đặt tên, dựng, nhân bản. Viết mới `dropTemplateDB` (thử khóa theo tên → kiểm
  lại dưới khóa → bỏ dấu mẫu → DROP không FORCE) dùng chung cho dọn 6 giờ (`dropStale`) và dọn giờ cũ
  (`pruneOlderHours`): một đường xóa mẫu duy nhất, thay đoạn khóa lồng trong `dropStale`.

## Soát thiết kế

- 5.1–5.10, 5.12: không áp dụng: chỉ đổi mã hạ tầng test, không dữ liệu nghiệp vụ, module, HTTP.
- 5.11: không thêm thư viện.
- 6.1 (đồng thời): mọi mẫu (mẫu migration và mẫu có dữ liệu) có khóa theo tên (727393, hashtext(tên)). Người dựng
  giữ khóa đó (dựng mẫu migration giữ thêm khóa chung 727391 như cũ, để tương thích với phiên còn chạy mã cũ). Người xóa
  (dọn giờ cũ, dọn 6 giờ) chỉ *thử* khóa của đúng tên sẽ xóa, kiểm lại dưới khóa, bỏ dấu mẫu (`IS_TEMPLATE false`, không
  thì DROP lỗi 42809) rồi `DROP DATABASE` không FORCE. Bản sao đang chạy `CREATE DATABASE … TEMPLATE` giữ ShareLock trên
  database nguồn nên DROP chờ bản sao xong (không làm hỏng bản sao); bản sao chưa bắt đầu thì gặp 3D000 và vòng thử lại
  dựng lại mẫu. Superuser nhân bản được cả database đã bỏ dấu mẫu, nên bỏ dấu giữa chừng không làm hỏng bản sao.
- 6.3 (deadlock): người dựng mẫu có dữ liệu giữ khóa tên stpl rồi (nếu thiếu mẫu migration) chờ 727391 rồi khóa tên tpl;
  người dựng mẫu migration chờ 727391 rồi khóa tên tpl (cùng thứ tự). Người xóa chỉ thử khóa, không chờ khóa advisory nào;
  DROP chờ khóa database của bản sao, mà bản sao (một câu CREATE) không chờ khóa advisory nào → không vòng chờ.
- 6.8 (chết giữa chừng): khóa cấp phiên tự nhả; mẫu dở (chưa `datistemplate`) bị người dựng sau xóa, dựng lại; mẫu giờ cũ
  dở dang bị dọn giờ cũ xóa khi không ai giữ khóa tên.
- 6.9 (treo): giữ thời hạn cũ (3 phút / `seedTimeout`).
- 6.2, 6.4–6.7, 6.10–6.13: không áp dụng: không HTTP, tiền, việc nền, bên ngoài, dữ liệu cá nhân, quyền.

## Quyết định thay chủ shop

1. **Giữ giờ UTC trong tên mẫu, thêm dọn giờ cũ** (không bỏ giờ). Lý do có thật (Task 391, reviewer lượt 1): migration
   0122 ghi `next_run_at` theo now() cho lịch `inventory.reconcile`, `TestReconcile_NightlySchedule` đỏ nếu mẫu sống
   nhiều ngày; dữ liệu seed cũng đóng băng now() (Task 423 đã nới một test theo giới hạn một giờ). Chi phí "mỗi giờ dựng
   lại" như cũ, nhưng mỗi khi một tiến trình dùng mẫu giờ h của băm B, mọi mẫu băm B giờ < h (kể cả mẫu có dữ liệu dựng
   từ chúng) bị xóa ngay, chỉ cần khóa theo tên của chính mẫu đó. Số mẫu còn lại ≤ (số bộ migration dùng trong 6 giờ
   qua) × (1 + số loại seed). Phương án khác: tên chỉ theo nội dung, dựng lại tại chỗ khi mẫu quá một giờ — cũng chặn
   số mẫu nhưng phải ghi mốc dựng vào database, mẫu có dữ liệu phải mang mốc của mẫu migration nguồn, đổi nhiều hơn.
   Đảo ngược phương án đã chọn: bỏ lời gọi `pruneOlderHours`.
2. **Dựng mẫu migration vẫn giữ khóa chung 727391** (thêm khóa tên): phiên ở worktree chưa gộp mã mới vẫn dựng dưới khóa
   chung và xóa mẫu dở bằng FORCE; bỏ khóa chung thì hai bản mã có thể dựng trùng một tên (cùng bộ migration) và làm đỏ
   test của nhau. Chỉ đường *xóa* bỏ khóa chung (đúng yêu cầu: không chờ khóa dựng của tên khác).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test mới đỏ trước khi có mã (mẫu chồng theo giờ) | `TestTemplateNoPileUpUnderConcurrentHours` | `go test -count=1 -race -run 'TestTemplatePrune\|TestTemplateNoPileUp' ./internal/testkit/` (trước khi viết `pruneOlderHours`) | `bộ migration còn 6 mẫu [… _2026112108 … _2026112110_s97a50e064d36], muốn ≤ 2`, `FAIL` |
| 3 tiến trình song song × 3 giờ giả × 2 bộ migration × {mẫu migration, mẫu có dữ liệu}: mọi bản sao dùng được (không 42809, không mất mẫu đang nhân bản), cuối cùng mỗi bộ ≤ 2 mẫu, đều của giờ mới nhất | `TestTemplateNoPileUpUnderConcurrentHours` (`cloneBoth`, `assertOnlyHour`) | `go test -count=4 -race -v -run 'TestTemplatePrune\|TestTemplateNoPileUp\|TestCleanupStale' ./internal/testkit/` | 4/4 `--- PASS: TestTemplateNoPileUpUnderConcurrentHours` (2,16–4,80 s) |
| Dùng mẫu giờ sau xóa mẫu giờ trước cùng băm (cả mẫu có dữ liệu); không chạm bộ migration khác; mẫu đang bị giữ khóa tên không bị xóa, nhả khóa thì lần sau xóa; khóa dựng mẫu chung 727391 đang bị giữ không chặn việc xóa | `TestTemplatePruneKeepsNewestHourPerFamily` | như trên | 4/4 `--- PASS: TestTemplatePruneKeepsNewestHourPerFamily` |
| Dọn 6 giờ: khóa tên của mẫu bị giữ → không xóa; khóa chung 727391 bị giữ → vẫn xóa; luật cũ (idle/tpl xóa, busy/keep/young còn) | `TestCleanupStaleDropsOnlyIdleOldDatabases`, `TestCleanupStaleSkipsSeededTemplateBeingBuilt` | như trên | 4/4 PASS mỗi test |
| Mọi test cũ của pgtemplate/pgseed còn qua | cả gói testkit | `go test -count=1 -race ./internal/testkit/` | `ok elimaz.com.vn/crm/internal/testkit 36.170s` |
| Dữ liệu now() của migration 0122 vẫn mới (lý do giữ giờ trong tên) | `TestReconcile_NightlySchedule` | `go test -count=1 -race -run TestReconcile_NightlySchedule ./internal/modules/inventory/` | `ok elimaz.com.vn/crm/internal/modules/inventory 2.791s` |
| Cổng kiểm của task (toàn bộ test vì đổi testkit) | ci-task | ci-task (ở nền, log ngoài worktree) | Lần 1 bị dừng ở 10 phút giới hạn nền của công cụ (giữa bước test), trước đó ĐỎ lint (`gocognit` 26 của test mới) và jscpd (6 dòng khóa tên trùng giữa hai test) → tách `cloneBoth`/`assertOnlyHour`, dùng `lockName`/`unlockName`. Lần 2: `CI-TASK XANH (so với điểm tách nhánh 5a3ce0ae…)`, test toàn bộ 399 giây, 0 bước ĐỎ |
| Trên crm-shared thật sau lần chạy toàn bộ | — | `psql … "select left(datname,25), count(*) from pg_database where datname like 'crm\_tpl\_%' group by 1"` | bộ `crm_tpl_be6fbf7c5ca7da6f_` (migration của main): 3 mẫu → 1; bộ `crm_tpl_d69aa50809968458_` (12 mẫu, phiên khác chạy mã cũ) để nguyên — sẽ bị dọn khi mã mới chạy ở đó hoặc khi đủ 6 giờ |

## Test bị xóa hoặc tắt

Không test nào bị xóa, tắt hay nới. Một test đổi ý nghĩa theo hành vi mới (có chủ ý):

- `TestCleanupStaleDropsOnlyIdleOldDatabases` (pgtemplate_test.go): phần "đang có người giữ khóa dựng mẫu chung → mẫu cũ
  chưa bị xóa" đổi thành "đang có người giữ khóa **theo tên** của mẫu → chưa bị xóa", thêm phần mới "khóa dựng mẫu chung
  727391 đang bị giữ → mẫu cũ **vẫn** bị xóa" (đúng yêu cầu task: dọn không chờ khóa dựng của tên khác). Kết quả cuối
  (idle, tpl bị xóa; busy, keep còn) giữ nguyên.
- `privateLocks` (cặp khóa) → `privateCleanupLock` (chỉ còn khóa dọn); `TestCleanupStaleSkipsSeededTemplateBeingBuilt`
  dùng `lockName`/`unlockName` thay câu khóa viết tay (cùng khóa 727393, hashtext(tên)); assertion giữ nguyên.

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Giai đoạn chuyển tiếp (reviewer): phiên ở worktree chưa gộp mã này dựng mẫu migration chỉ dưới khóa chung 727391,
  không lấy khóa tên; nếu phiên mã mới cùng bộ migration sang giờ h đúng lúc phiên mã cũ đang dựng mẫu giờ h−1, mẫu đó
  có thể bị xóa giữa chừng và một test của phiên cũ đỏ một lần. Hết khi mọi worktree gộp main.
- Bộ `crm_tpl_d69aa50809968458_` (12 mẫu) trên crm-shared của phiên đang chạy mã cũ: không xóa tay (không phải của task
  này); mã mới dọn khi đủ 6 giờ hoặc khi phiên đó gộp main.

## Reviewer

Kết luận: **ĐẠT** (lượt 1, trên phần đã stage sau khi gộp main — main không có gì mới). Không có phát hiện phải sửa.
Reviewer soát: lý do giữ giờ trong tên có thật (Task 391 dòng 7, 58, 73); `dropTemplateDB` chỉ thử khóa tên, bỏ dấu mẫu
trước DROP (tránh 42809); DROP chờ bản sao đang chạy; thứ tự khóa không vòng chờ; `nameLockKey` giữ 727393 nên tương
thích mã cũ; test 3 tiến trình kiểm cuối không phụ thuộc thứ tự chạy. Ghi chú không bắt buộc: test giữ khóa chung thật
727391 một lúc ngắn (có chủ ý); giai đoạn chuyển tiếp với mã cũ (đã ghi ở "Việc phát hiện thêm"); DROP gặp 55006 để lại
mẫu đã bỏ dấu, phiên sau dựng lại (~1 s), không sai.
