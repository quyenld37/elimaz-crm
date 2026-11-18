# Task 21 — Image, sao lưu theo lịch, kiểm bản sao lưu hằng tuần

Task 21 (M0-21). Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`, Task 21. Loại việc (quy trình mục 3): A.
Spec: ADR-37 (điều 1–5: sao lưu WAL-G, kiểm bản sao lưu hằng tuần ghi `backup_runs`), ADR-43 (mỗi dịch vụ một tài nguyên có
kiểm sức khỏe; PostgreSQL gắn `/var/lib/postgresql`, `shm_size`; máy chủ chỉ nhận kết nối qua Cloudflare; image dựng ở CI),
ADR-45 (`imager` chỉ ra mạng tới kho tệp), 04 mục 11 (chặn gọi dồn dập: chỉ nhận kết nối qua Cloudflare), ADR-42 (ngân sách
kết nối), Global Constraints của kế hoạch (container không root, secret không trong image).

Việc treo giao cho task này: (1) `crm-ingest` chỉ nhận kết nối qua Cloudflare (Task 14); (2) `stop_grace_period` ≥ 30 s cho
worker/api/ingest (Task 11); (3) RUNBOOK `GRANT pg_monitor TO crm_owner WITH ADMIN OPTION` trước migrate đầu (Task 19);
(4) kiểm bản sao lưu có giới hạn thời gian, `restore-drill.sh` tách mã 1 và 2 của grep, `base-backup.sh` thiếu khóa ghi
`ok=false` (Task 2); (5) compose production chặn imager ra internet (Task 40, ADR-45).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu xuất ra nào đổi tên hay chữ ký. `observability.CheckRules`
  thêm một key cảnh báo mới `backup-verify` (hành vi, không đổi chữ ký); hàm riêng `backupRule` đổi thân. `RuleOptions.BackupExpected`
  giữ nguyên, chỉ thêm nghĩa (bật cả `backup-verify`); 1 chỗ đặt giá trị ngoài test (`alertjobs`, đọc `cfg.BackupExpected`).
- Gói phụ thuộc ngược (go list) của `internal/platform/observability`: `cmd/crm`, `cmd/imager`, `internal/httpapi`,
  `internal/platform/observability/alertjobs`, `internal/platform/queue`. Gói mới `tests/deploy` (chỉ test, không ai nhập).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không migration mới, không câu sqlc mới. Dùng lại `LastBackupRun` (`queries/platform/ops.sql`)
  cho luật mới. Script sao lưu ghi `platform.backup_runs` (cột `duration_seconds` đã có từ 0010; kind `restore_verify` đã có trong CHECK).
- Tệp sửa thêm khi làm (ghi chi tiết ở Khác kế hoạch): `docker/compose.backup-drill.yml` (`additional_contexts`, `init: true`),
  `docker/postgres/Dockerfile`, `docker/postgres/init-drill/01-roles.sql`; tệp mới `scripts/backup/runs-lib.sh`,
  `docker/compose.prod.env.example`.
- Tệp mới: `Dockerfile`, `.dockerignore`, `docker/compose.prod.yml`, `scripts/backup/weekly-verify.sh`,
  `scripts/backup/verify-drill.sh` (diễn tập Step 2), `scripts/ops/image-check.sh` (kiểm Step 1), `tests/deploy/compose_test.go`.
- Sửa tệp có sẵn: `docker/compose.dev.yml` (bốn dịch vụ `crm-migrate`, `crm-ingest`, `crm-api`, `crm-worker` trong profile `app`,
  không đổi dịch vụ cũ), `scripts/backup/base-backup.sh`, `scripts/backup/restore-drill.sh` (grep mã 1/2), `Makefile` (đích
  `image`, `image-check`, `verify-drill`), `internal/platform/observability/rules.go`, `rules_test.go`, `docs/RUNBOOK.md`,
  `docs/do-luong/phep-thu-ngan.md`, `docs/phu-thuoc.md` (cloudflared).
- Docker: chỉ dự án compose `crm-t21` (dev), `crm-t02` (compose diễn tập, tên cố định trong tệp; không phiên nào khác đang dùng —
  kiểm bằng `docker ps` trước khi chạy). Image tự dựng: `crm:dev`, `crm-postgres:18-walg-drill`.
- Test sẽ chạy: `go test -race ./internal/platform/observability/...` và gói phụ thuộc ngược (`alertjobs`, `queue`, `httpapi`,
  `cmd/crm`); `go test ./tests/deploy/...`; `scripts/ops/image-check.sh`; `scripts/backup/verify-drill.sh`;
  `scripts/backup/restore-drill.sh` (sau khi sửa grep); `bash -n` mọi script; `make ci` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (observability: `CheckRules`,
  `RuleOptions`, `CheckLive`); `git grep backup_runs|LastBackupRun|restore_verify`; `scripts/backup/drill-lib.sh` (sinh khóa, `dc`,
  chờ sẵn sàng, dọn); `docker/imager/Dockerfile` (mẫu dựng Go tĩnh, digest `golang:1.27.4-trixie`); `internal/imager/compose_test.go`
  (mẫu test compose bằng lệnh `docker`); `cmd/crm healthcheck` (đã có, chạy được trong image không shell).
- Dùng lại: `drill-lib.sh` cho diễn tập Step 2 (không viết lại bật/dọn cụm); `LastBackupRun` cho luật `backup-verify`; digest
  `golang:1.27.4-trixie` của Task 42 cho stage dựng; `crm healthcheck` cho HEALTHCHECK; image `crm-postgres:18-walg-drill` của Task 2.
- Viết mới: `weekly-verify.sh` (chưa có gì khôi phục vào cụm tạm và đối chiếu); `tests/deploy` (kiểm luật của tệp compose: chưa có
  test nào đọc compose dưới dạng dữ liệu; `internal/imager/compose_test.go` chạy container thật, khác việc).
  `verify-drill.sh` có đoạn bật cụm giống `restore-drill.sh` nhưng gọi chung `drill_up`; phần còn lại khác việc nên không gộp.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
1. Chủ dữ liệu: `platform.backup_runs` là bảng nền tảng (0010); chỉ `internal/platform/observability` và `scripts/backup` chạm, đúng ghi chú 0010.
2. Không ghi bảng module khác: `weekly-verify.sh` chỉ **đếm** `identity.users`, `identity.audit_logs`, `platform.tenants` bằng
   `postgres` trong cụm tạm và cụm thật (công cụ vận hành, không phải mã module); chỉ ghi `platform.backup_runs`.
3. Đọc qua view: không áp dụng — script vận hành ngoài ứng dụng, đếm dòng bằng quyền quản trị (RLS không áp cho superuser, cần đếm mọi tenant).
4–5. Không áp dụng: không có việc nghiệp vụ.
6. Gọi ra ngoài: không có lệnh ra ngoài mới. Sao lưu đi thẳng ra kho S3 ngoài (ADR-37 điều 4); diễn tập dùng SeaweedFS thay.
7. Ngữ cảnh shop: không có truy vấn ứng dụng mới vào bảng có `tenant_id`.
8. Nghiệp vụ không biết HTTP: không áp dụng.
9. Đường ghi có khóa chống trùng: không có API mới.
10. PostgreSQL là nguồn sự thật: kết quả sao lưu và kiểm nằm ở `backup_runs`; cảnh báo đọc từ đó.
11. Công nghệ mới: distroless static (kế hoạch ghi tên), `cloudflared` (đường hầm Cloudflare, ADR-43 "tường lửa của máy hoặc
    đường hầm") chỉ trong compose production — ghi dòng `docs/phu-thuoc.md`. Không thư viện Go mới.
12. Module mẫu: không có module mới.

Mục 6 (đúng khi chạy đồng thời):
1. Hai lượt kiểm cùng lúc: `weekly-verify.sh` khóa bằng `flock` trên tệp trong thư mục tạm của container; lượt thứ hai thoát khác 0 và ghi `ok=false` "đang có lượt khác".
2–5. Không áp dụng: không đường yêu cầu, không transaction ứng dụng.
6. Việc nền lỗi: mọi bước hỏng của `base-backup.sh` (kể cả thiếu khóa) và `weekly-verify.sh` ghi `ok=false` và thoát khác 0;
   cảnh báo `backup-age` (không có base tốt 26 giờ, đã có) và `backup-verify` mới (lần kiểm gần nhất hỏng).
7. Không áp dụng.
8. Tiến trình chết giữa chừng: cụm tạm nằm trong thư mục tạm, `trap` dừng postgres tạm và xóa thư mục; ba vai trò có
   `stop_grace_period: 30s` (worker cần 25 s tắt êm, Task 11).
9. Treo lâu: `wal-g backup-fetch` có `timeout` (mặc định 2 giờ, đổi bằng biến) vì WAL-G treo khi sai/thiếu khóa (Task 2); chờ
   cụm tạm sẵn sàng có hạn; `base-backup.sh` đặt hạn cho `backup-push` và `delete`. Quá hạn = `ok=false`.
10–11. Không áp dụng (không tiền, không vùng rủi ro cao).
12. Dữ liệu cá nhân: `detail` của `backup_runs` chỉ có tên bước hỏng và số đếm, không có nội dung lỗi WAL-G, không dòng dữ liệu.
    Image không chứa `.env*` (kiểm bằng `image-check.sh`). Khóa diễn tập sinh lúc chạy.
13. Quyền: container chạy uid 65532 (distroless nonroot), `read_only`, bỏ mọi capability, `no-new-privileges`. Compose production
    không mở cổng nào ra máy; chỉ `cloudflared` nhận lưu lượng từ Cloudflare.

## Quyết định thay chủ shop

1. **Database tạm = cụm PostgreSQL tạm** trong chính container PostgreSQL (thư mục tạm, cổng riêng, chỉ socket), vì WAL-G khôi phục cả cụm, không khôi phục một database. Khôi phục tới điểm cuối của bản sao lưu (`recovery_target='immediate'`) để so "tại thời điểm bản sao lưu". Phương án khác: container riêng như `restore-drill.sh` — cần Docker trên máy chạy lịch; để ngỏ.
2. **Mốc thời gian của bản sao lưu** đọc từ `backup_label` (`START TIME`) mà WAL-G đặt vào thư mục khôi phục. Dòng ghi sau mốc bắt đầu là cận trên của dòng ghi sau điểm cuối, nên dung sai rộng hơn chút, không bao giờ báo hỏng sai.
3. **Cảnh báo `backup-verify` (urgent)** khi lần `restore_verify` gần nhất `ok=false`, bật cùng `BACKUP_EXPECTED`. Kế hoạch chỉ đòi ghi `ok=false`; không cảnh báo thì bản sao lưu hỏng nằm im trong bảng. Đảo ngược: bỏ một hàm.
4. **Compose production** (`docker/compose.prod.yml`): không dịch vụ nào mở cổng ra máy; lối vào duy nhất là `cloudflared` (Cloudflare Tunnel) trên mạng `edge`; `ingest`, `api` chỉ nằm ở `edge` và `default`. Phương án khác: Traefik + tường lửa chỉ cho dải IP Cloudflare — phụ thuộc tường lửa ngoài compose, dễ quên khi đổi máy. Nền tảng triển khai thật vẫn chốt ở staging (Task 23).
5. **Dịch vụ ứng dụng ở compose dev nằm trong profile `app`** để `make db-up` của các phiên khác không đổi; thêm `crm-migrate` chạy một lần trước ba vai trò.
6. **Giới hạn bộ nhớ** (chưa có số đo, ADR-43): ingest 256 MiB, api 512 MiB, worker 512 MiB; `GOMEMLIMIT` = 90% (230 MiB, 460 MiB, 460 MiB). Đo lại ở Task 22.
7. **Lịch** ghi ở RUNBOOK dạng crontab của máy (02:00 và chủ nhật 03:00 giờ Việt Nam); nơi chạy chốt ở staging như kế hoạch.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước: luật `backup-verify` chưa có | `TestRuleBackupVerify` | `go test -race -count=1 -run TestRuleBackup ./internal/platform/observability/` | `rules_test.go:188: key "backup-verify" có = false, muốn true; các key: []`, FAIL |
| Test đỏ trước: compose chưa có ba vai trò, chưa có compose production | `tests/deploy` | `go test -count=1 ./tests/deploy/` | `couldn't find env file … compose.prod.env.example`, FAIL (5 test production đỏ) |
| Test đỏ trước: cụm diễn tập thiếu quyền cho migrate | `verify-drill.sh` | `scripts/backup/verify-drill.sh` | `migrate: tạo schema platform: ERROR: permission denied for database crm`, exit 1 |
| Luật `backup-verify`: lần kiểm gần nhất hỏng → urgent; đạt lại → thôi; tắt `BackupExpected` → không; không có base tốt + kiểm hỏng → cả `backup-age` và `backup-verify`; `backup-age` cũ không đổi | `TestRuleBackupVerify`, `TestRuleBackupAge` | `go test -race -count=1 ./internal/platform/observability/...` (sau gộp main e863734) | `ok … observability 105.150s`, `ok … alertjobs 2.664s` |
| Ba vai trò ở compose dev và production: healthcheck `/crm healthcheck`, `mem_limit`, `GOMEMLIMIT` = 90%, `stop_grace_period` ≥ 30 s, chỉ đọc, `cap_drop ALL`, không root; dev ở profile `app` | `TestDevCompose_AppServices`, `TestProdCompose_AppServices` | `go test -count=1 ./tests/deploy/` | `ok … tests/deploy 2.779s` |
| Ingest chỉ nhận kết nối qua Cloudflare: không dịch vụ nào mở cổng; mạng `edge` chỉ có cloudflared, crm-api, crm-ingest | `TestProdCompose_OnlyCloudflareIngress` | như trên | ok |
| imager chỉ ở mạng internal, không chung mạng PostgreSQL, không biến bí mật | `TestProdCompose_ImagerIsolated` | như trên | ok |
| Production không `build:`; image bên thứ ba ghim digest; PostgreSQL volume `/var/lib/postgresql`, `shm_size`, biến WAL-G, `init: true` | `TestProdCompose_ImagesPinnedNotBuilt`, `TestProdCompose_PostgresVolume` | như trên | ok |
| Test compose bắt được lỗi thật (lỗi cố ý rồi trả lại) | mở cổng ingest; bỏ `internal`; `stop_grace_period: 20s`; `GOMEMLIMIT: 256MiB` | `go test ./tests/deploy/` với từng bản sửa | cả bốn FAIL đúng chỗ: `crm-ingest mở cổng ra máy`, `imager ở mạng imaging không internal`, `stop_grace_period "20s", cần ≥ 30s`, `GOMEMLIMIT 256 MiB, muốn 230 MiB` |
| Step 1: `docker build`; ba vai trò `healthy`; `/health/ready` 4190, 4191 trả 200; uid khác 0; không `.env*`; kích thước | `scripts/ops/image-check.sh` | `CRM_API_PORT=24190 CRM_INGEST_PORT=24191 scripts/ops/image-check.sh` (sau gộp main) | `IMAGE CHECK OK size_bytes=10346634 build_seconds=52 user=65532:65532 crm-ingest=healthy,uid=65532 crm-api=healthy,uid=65532 crm-worker=healthy,uid=65532`; `docker run --rm crm:dev` thoát 2 (thiếu cấu hình) |
| `.dockerignore` thật sự chặn `.env*` (tệp mồi trong `internal/`) | `image-check.sh` với `.dockerignore` bỏ bốn dòng `.env` | như trên | `LỖI: ngữ cảnh dựng chứa tệp .env*: .dockerignore không chặn`, exit 1; trả lại → OK |
| Step 2: `base-backup.sh` → `base` `ok = true` kèm `duration_seconds`; thiếu khóa → `ok = false`, thoát ≠ 0 | `verify-drill.sh` bước 1, 2 | `CRM_DRILL_PG_PORT=5523 scripts/backup/verify-drill.sh` (sau gộp main) | `base_duration_seconds=4`; bước thiếu khóa qua (script tự kiểm thoát ≠ 0, `ok = false`, detail có `WALG_LIBSODIUM_KEY`) |
| Step 2: `weekly-verify.sh` → `restore_verify` `ok = true` kèm thời gian; đối chiếu ba bảng có dung sai; cụm tạm được dọn | `verify-drill.sh` bước 3 | như trên | `WEEKLY VERIFY OK backup_start='2026-11-18 07:30:29 UTC' duration_seconds=14 tenants=1/2(+1) users=1/1(+0) audit_logs=20/25(+5)` |
| Step 2: làm hỏng bản sao lưu (xóa một object) → `ok = false`, thoát ≠ 0 | `verify-drill.sh` bước 7 | như trên | `deleted=…/tar_partitions/part_001.tar.lz4`; `mất object: ok = false, detail: lỗi ở bước backup-fetch` |
| (sau review) Không đủ chỗ trống cho cụm tạm → hỏng ngay, không tải; `CRM_VERIFY_DIR` chứa `PGDATA` → từ chối, không xóa gì | `verify-drill.sh` bước 5b, 5c | `CRM_DRILL_PG_PORT=5523 scripts/backup/verify-drill.sh` (14:39) | `không đủ chỗ trống cho cụm tạm: trống 411489 MiB, cần 1073741856 MiB`; `CRM_VERIFY_DIR không hợp lệ: chứa dữ liệu thật`; dữ liệu thật còn; cả lượt `VERIFY DRILL OK … verify_duration_seconds=12` |
| Kiểm có giới hạn thời gian (WAL-G treo khi khóa sai) | `verify-drill.sh` bước 6 | như trên | `khóa sai: ok = false, detail: quá hạn 20s ở bước backup-fetch`, `wrongkey_seconds=33` |
| Lệch số dòng ngoài dung sai → hỏng; hai lượt chồng nhau → lượt sau hỏng | `verify-drill.sh` bước 4, 5 | như trên | `lệch số dòng quá dung sai: tenants; tenants=1/3(+1)…`; `đang có lượt khác` |
| Step 2: `CheckRules` có `backup-age` khi không còn base tốt trong 26 giờ | `TestRuleBackupAge` (Task 19), ca cuối của `TestRuleBackupVerify` | như dòng luật | ok |
| Cụm PostgreSQL thật không khởi động lại vì sao lưu/kiểm (sau `init: true`) | `verify-drill.sh` so `pg_postmaster_start_time()` đầu và cuối | như trên | `VERIFY DRILL OK`; trước khi có `init: true` (lần chạy 14:06): log `untracked child process (PID 724) was terminated by signal 9` → `terminating any other active server processes`, script dừng `khóa sai: không ghi restore_verify ok = false` |
| `restore-drill.sh` (grep tách mã 1/2, `init: true`) vẫn đạt | `restore-drill.sh` | `CRM_DRILL_PG_PORT=5523 scripts/backup/restore-drill.sh` | `RESTORE DRILL OK rpo_seconds=59 restore_seconds=9 encrypted=yes nokey_rc=137 wrongkey_rc=137 rows=150` |
| Cổng kiểm | `make ci` | `make ci` một lần trên phần đã stage (trước gộp e863734); sau gộp chạy lại từng bước | build, vet, lint (`0 issues`), gen-check, sqlc-vet, vuln, oasdiff, catalog-check, test-guard, gitleaks, console-check: rc=0. `go test -race ./...`: mọi gói ok trừ 2 test nhạy thời gian của `internal/imager` khi máy đang dựng image song song (chạy lại riêng 3 lần: `ok`). jscpd: 2 đoạn trùng có sẵn trên `main` (Việc phát hiện thêm), không thuộc tệp của task |
| Cổng kiểm sau review và sau gộp main 371a9d3 | `make ci` | `make ci` trên phần đã stage | `rc=0`: build, vet, lint `0 issues`, gen-check, sqlc-vet, `go test -race ./...` (mọi gói ok), vuln, oasdiff, catalog-check, jscpd, test-guard, gitleaks, console-check |
| Cú pháp shell | mọi `scripts/backup/*.sh`, `scripts/ops/*.sh` | `bash -n` | không lỗi (`shellcheck` chưa cài) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `docker/compose.prod.yml` (+ `compose.prod.env.example`, chỉ tên biến) — kế hoạch chỉ ghi sửa `compose.dev.yml`, nhưng
  ba việc treo (ingest chỉ qua Cloudflare, stop_grace_period, imager không ra internet) là luật của compose máy chủ;
  `tests/deploy` kiểm chúng. Chưa chạy trên máy nào (Task 23).
- Compose dev: thêm `crm-migrate` (một lần) ngoài ba vai trò; cả bốn ở profile `app`.
- Luật cảnh báo mới `backup-verify` (Quyết định 3) — kế hoạch chỉ đòi ghi `ok = false`.
- `docker/postgres/Dockerfile` mang sẵn script sao lưu (ngữ cảnh dựng phụ `backup`), để lịch của máy chủ gọi trong
  container; `init-drill/01-roles.sql` thêm quyền `CREATE`, `CONNECT` và ba extension như bước dựng máy chủ (việc treo
  Task 4), để migrate thật chạy trên cụm diễn tập.
- `init: true` cho container PostgreSQL (compose diễn tập, production): phát hiện khi đo (xem Bằng chứng, mục "Điều
  phát hiện" của `docs/do-luong/phep-thu-ngan.md`).
- Thêm `scripts/backup/runs-lib.sh` (ghi `backup_runs`, dùng chung cho hai script), `scripts/backup/verify-drill.sh`,
  `scripts/ops/image-check.sh`; đích Make `image`, `app-up`, `app-down`, `image-check`, `verify-drill`.

## Việc phát hiện thêm

- **Compose dev `postgres` (image `postgres:18` gốc) vẫn chạy postmaster làm PID 1.** Không chạy script sao lưu nên chưa
  hại; nếu sau này có `docker exec` dài trong đó thì cũng nên `init: true`. Không sửa vì ngoài phạm vi.
- Image `postgres:18-bookworm` của Task 2 cài `curl` rồi gỡ; `weekly-verify.sh` cần `flock`, `timeout` (util-linux,
  coreutils, có sẵn). Nếu đổi image gốc phải giữ hai lệnh này.
- `tools/versions.mk` vẫn chưa ghim `shellcheck` (Task 2 đã nêu); script shell của sao lưu và vận hành giờ có 8 tệp.
  Nên ghim và cho vào `make ci` (Task 23).
- Nền tảng thật (Coolify hay compose thuần) và nơi chạy lịch chốt ở Task 23; nếu chọn Traefik thay tunnel thì cần tường
  lửa chỉ nhận dải IP Cloudflare (RUNBOOK đã ghi).
- **jscpd đỏ trên `main` lúc làm** (không do task này; Task 397 đã dọn, gộp ở 371a9d3): `metrics/flusher_test.go` ~ `observability/helpers_test.go`, và
  `observability/alerter.go` `stripURL` ~ `scripts/external-probe/probe.go`. Task 397 đang dọn.
- Hai test `internal/imager` (`TestImager_HangKilledWithinDeadline`, `TestExecRunner_TimeoutKillsProcessGroup`) đỏ khi máy
  tải nặng (script giả chưa kịp ghi pid trong 0,5–1 s); chạy riêng thì qua. Không sửa vì ngoài phạm vi.
- Health summary (`/api/v1/platform/health`) mới có `restore_drill`, chưa có `restore_verify`: thêm cần đổi OpenAPI, để
  task màn "Sức khỏe hệ thống" (M1) quyết.

## Reviewer

Vòng 1 (agent `reviewer`, 18/11/2026, trên phần đã stage sau gộp main e863734): **CẦN SỬA**, một phát hiện:
- `weekly-verify.sh` không kiểm chỗ trống trước khi tải cả bản sao lưu vào cùng ổ với cụm thật; ổ đầy làm PostgreSQL
  thật dừng ghi. **Đã sửa:** trước khi tải, đòi chỗ trống ≥ tổng `pg_database_size` + `CRM_VERIFY_MARGIN_BYTES`
  (mặc định 1 GiB), thiếu thì `ok = false` "không đủ chỗ trống", không tải; `verify-drill.sh` bước 5b chứng minh.

Ghi chú không bắt buộc của vòng 1 đã làm luôn (rẻ, chặn hại thật): `CRM_VERIFY_DIR` trùng/chứa/nằm trong `PGDATA` thì
từ chối trước cả khi mở khóa (bước 5c); `cleanup` chỉ xóa khi đã giữ khóa và đã kiểm thư mục. RUNBOOK thêm cách xử lý
khi thiếu chỗ.

Ghi chú để lại cho Task 23 (không sửa ở đây): `crm-ingest` còn ở mạng `default` chung postgres/seaweedfs/jaeger (một
container bị chiếm trên mạng đó vẫn giả được `cf-connecting-ip`); worker prod đặt `FILESYNC_EXPECTED=true` khi compose
prod chưa có filesync; mẫu `START TIME` chỉ nhận tên múi giờ chữ (hỏng theo hướng an toàn nếu đổi `log_timezone`);
tệp mồi `.env` của `image-check.sh` có thể sót nếu script bị giết cứng (đã nằm trong `.gitignore` qua mẫu `.env.*`).

Vòng 2 (cùng agent, trên phần đã stage sau gộp main 371a9d3): **CẦN SỬA**, một phát hiện:
- Bước mới "dừng postgres tạm sót lại theo `postmaster.pid`" có thể gửi SIGQUIT tới tiến trình của cụm thật khi số PID
  trong tệp cũ đã quay vòng (container khởi động lại giữa một lượt). **Đã sửa:** bỏ hẳn bước dừng, chỉ xóa thư mục
  (PostgreSQL tự tắt khi `postmaster.pid` của nó mất). Ghi chú không bắt buộc (so đường dẫn không qua `realpath`;
  khoảng dự phòng 1 GiB nên đo lại với database lớn ở Task 23) để lại.

Vòng 3 (cùng agent, phần đã stage trên main 371a9d3): **ĐẠT**. Không còn phát hiện phải sửa. Ghi chú không bắt buộc
mới: postgres tạm sót của lượt bị giết (container vẫn chạy) khi tự tắt có thể xóa tệp socket cùng tên của lượt mới →
lượt mới báo hỏng sai (không chạm cụm thật); có thể đặt thư mục socket theo từng lượt. Để lại cho Task 23.

Kết luận: ĐẠT
