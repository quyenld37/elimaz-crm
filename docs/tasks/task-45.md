# Task 45 — Chép sang kho ngoài mỗi giờ, đối chiếu mỗi ngày, cảnh báo (M1-45)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`, Task 45 (M1-45, TP10). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `observability.RuleOptions` thêm trường `FileSyncExpected bool` (thêm trường, không đổi chữ ký). Chỗ dựng giá trị:
    `cmd/crm/main.go:343` (nối `cfg.FileSyncExpected`), các test của gói `observability` (dựng theo tên trường, không vỡ).
  - `observability.DeepHealthReport` thêm `LastFileSyncAgeM *int64`. Chỗ đọc: `internal/httpapi/handlers_health.go`
    (HealthDeep), `deephealth_test.go`.
  - Hàm nội bộ mới `syncedBuckets()` (ba bucket chép; luật và `/health/deep` dùng chung), không xuất.
  - `config.Config.FileSyncExpected` đã có (Task M1-25), không đổi.
- Gói phụ thuộc ngược (go list) của `internal/platform/observability`: `cmd/crm`, `cmd/imager`, `internal/httpapi`,
  `internal/modules/inventory`, `internal/platform/observability/alertjobs`, `internal/platform/queue`,
  `internal/platform/webhooks`. Gói store sinh `internal/platform/internal/store` (thêm câu, không đổi câu cũ).
  `internal/testkit` (prepareEmpty: tạo role `crm_filesync` cho cụm thử) → mọi gói test dùng testkit; mẫu database
  đổi băm theo bộ migration như thường lệ.
- Operation OpenAPI bị chạm, oasdiff: `healthDeep` — schema `DeepHealth` thêm thuộc tính tùy chọn
  `lastFileSyncAgeM` (int64). Thêm thuộc tính tùy chọn ở câu trả lời: không phá vỡ. `make gen` sinh lại
  `internal/httpapi/gen`, `console/src/api`.
- Chỗ dùng trên màn hình: không màn nào đọc `healthDeep` (console chỉ dùng `getPlatformHealth`); kiểu TS sinh lại.
- Câu sqlc, bảng, pha migration: migration mới `0126_file_sync.sql` (pha mở rộng: bảng mới `platform.file_sync_runs`,
  index, GRANT cho `crm_app` (SELECT, đọc luật) và `crm_filesync` (USAGE schema, INSERT bảng)). Câu mới trong
  `queries/platform/file_sync.sql`: `FileSyncStatus` (mỗi bucket: lần copy tốt cuối, kết quả copy cuối, kết quả
  reconcile cuối). Không chạm câu, bảng cũ.
- Hạ tầng: `docker/filesync/Dockerfile` (mới), `scripts/filesync/{run.sh,reconcile.sh,rclone.conf.tmpl,lib.sh,ls-offsite.sh}`
  (mới), `.dockerignore` (cho `scripts/filesync` vào ngữ cảnh dựng; image chính không chép `scripts/`),
  `docker/compose.dev.yml` (dịch vụ `filesync`: biến môi trường, volume trạng thái, hardening),
  `docker/postgres/init/01-roles.sql`, `docker/postgres/init-drill/01-roles.sql`, `docs/RUNBOOK.md` (role, cảnh báo).
- Test sẽ chạy: `go test -race ./internal/platform/observability/... ./internal/httpapi/... ./internal/testkit/...
  ./cmd/crm/... ./tests/deploy/...`; `go test -tags filesync ./tests/e2e -run TestFileSync` trên compose crm-t45;
  `scripts/agent/ci-task.sh` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục
  `internal/platform/observability` (CheckRules, Alerter, DeepHealth, `backupRule`/`LastBackupRun` là mẫu gần nhất);
  `platform.backup_runs` + `scripts/backup/runs-lib.sh` (ghi dòng kết quả bằng psql); image rclone đã ghim digest ở
  `docker/compose.backup-drill.yml`; `tests/deploy` (`config`, `devProject`) đọc compose đã gộp; testkit
  `prepareEmpty` cho việc cấp quyền cụm.
- Dùng lại / viết mới vì:
  - Luật mới viết trong `rules.go` theo đúng mẫu `backupRule` (một hàm luật trả `[]alert`), thêm vào `tableRules`.
    Không gộp với `backupRule`: khác bảng, khác ngưỡng, khác lý do đổi.
  - Bảng riêng `file_sync_runs` (kế hoạch, spec 05 mục 7.3), không nhét vào `backup_runs` (khác cột bucket, số đối tượng).
  - Script ghi dòng không dùng `runs-lib.sh` của sao lưu: lib đó ghi `backup_runs` qua role chủ trong container diễn
    tập; filesync ghi bằng role `crm_filesync` qua `psql "$FILESYNC_DATABASE_URL"` với biến psql (`:'x'`) — khác bảng,
    khác quyền.
  - Image filesync dựng từ đúng `rclone/rclone:1.74.4@sha256:c619…` đã ghim ở compose diễn tập (một bản rclone cho cả repo).

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1 Chủ dữ liệu: `file_sync_runs` thuộc `platform` (spec 05 mục 7.3), schema `platform`.
- 5.2 Không ghi bảng module khác: Go chỉ đọc `file_sync_runs`; ghi chỉ do container filesync (role riêng).
- 5.3 Đọc qua view: không áp dụng: bảng của chính `platform`, đọc trong `observability` (cùng platform, như `backup_runs`).
- 5.4, 5.5 Nguyên tử / sau commit: không áp dụng: mỗi dòng kết quả một câu INSERT độc lập; không transaction nghiệp vụ.
- 5.6 Gọi ra ngoài: rclone tới kho ngoài và curl nhịp sống chạy trong container riêng, không qua `commands`
  (kế hoạch Q19); chép lại là an toàn (`copy --immutable`, chỉ thêm), nên không cần `in_doubt`.
- 5.7 Tenant: không áp dụng: bảng nền tảng không `tenant_id`, không RLS (kế hoạch, như `backup_runs`).
- 5.8 Nghiệp vụ không biết HTTP: luật nằm ở `observability`; handler chỉ chép thêm một trường.
- 5.9 Đường ghi có khóa chống trùng: không áp dụng: không thêm đường ghi HTTP.
- 5.10 Valkey: không áp dụng.
- 5.11 Công nghệ mới: rclone đã có trong repo (compose diễn tập) và trong spec (ADR-36); gói apk `postgresql18-client`,
  `curl`, `jq`, `tzdata` chỉ trong image filesync, ghim bản. Không thêm thư viện Go.
- 5.12 Theo mẫu: luật theo mẫu `backupRule`; test theo `rules_test.go` (database thật qua `newEnv`).

Đúng khi chạy đồng thời (mục 6):
- 6.1 Hai bên sửa cùng thứ: không có: bảng chỉ INSERT; một container filesync (một tiến trình) ghi.
- 6.2 Gửi lại: chạy lại `rclone copy` không chép trùng (tệp đã có, cùng checksum, bỏ qua); mỗi lượt một dòng mới.
- 6.3, 6.4, 6.5 Khóa, lỗi tạm, cách ly: không khóa dòng; câu đọc của luật là SELECT thuần.
- 6.6 Việc nền lỗi: lượt chép lỗi ghi `ok=false`, lượt sau (60 phút) tự chạy lại; cảnh báo `file-sync-failed`, quá
  90 phút không có lượt tốt thì `file-sync-age` gấp.
- 6.7 429: rclone tự lùi theo `Retry-After` của S3 (`--retries`, `--low-level-retries` mặc định).
- 6.8 Chết giữa chừng: dòng `copy` chỉ ghi khi rclone kết thúc (mốc `finished_at` lúc xong thật — Bẫy đã gặp); chết
  giữa chừng thì không có dòng tốt → quá 90 phút cảnh báo gấp.
- 6.9 Treo lâu: `psql` đặt `statement_timeout`; rclone có `--timeout`, `--contimeout`; một lượt treo thì không có dòng
  tốt → cảnh báo gấp sau 90 phút.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: "kho ngoài không bao giờ mất tệp vì kho chính xóa/sửa" giữ bằng (a) script chỉ dùng `copy --immutable`,
  (b) danh tính `crm-filesync` không có `DeleteObject` ở kho ngoài (đã có ở Task M1-40); test e2e xóa ở kho chính, ghi
  đè ở kho ngoài.
- 6.12 Dữ liệu cá nhân: `detail` chỉ ghi số đếm và dòng lỗi rút gọn của rclone (tên đối tượng là mã, không tên khách);
  văn bản cảnh báo chỉ mang tên bucket và số phút; bucket riêng tư mã hóa tên và nội dung (rclone crypt) trước khi rời máy.
- 6.13 Quyền: role `crm_filesync` chỉ `INSERT` một bảng, `CONNECTION LIMIT 1`, `NOBYPASSRLS`; danh tính kho
  `crm-filesync` chỉ đọc kho chính; container không có `S3_SECRET_ACCESS_KEY` của `crm-app`.

## Quyết định thay chủ shop

- **Q1. Ai tạo role `crm_filesync`.** Kế hoạch ghi `CREATE ROLE` trong migration 0126; nhưng migration chạy bằng
  `crm_owner`, không có `CREATEROLE` (task-24.md đã báo trước). Chọn: người quản trị tạo role như ba role M0
  (`docker/postgres/init/01-roles.sql`, `init-drill/01-roles.sql`, bước dựng PostgreSQL của RUNBOOK; testkit tạo cho cụm
  thử, kể cả cụm dev đã dựng trước task này), migration 0126 chỉ GRANT; thiếu role thì migrate dừng có lỗi rõ. Vì: không
  nới quyền của `crm_owner` (cho `CREATEROLE` thì role chạy migration tạo được role đăng nhập bất kỳ); giống cách đã làm
  với `pg_monitor` ở Task 19. Phương án khác: cấp `CREATEROLE` cho `crm_owner` và `CREATE ROLE` trong migration (đúng chữ
  kế hoạch, nhưng nới quyền); bọc GRANT trong điều kiện "role có thì cấp" (lỗi im lặng). Đảo ngược: dễ.
- **Q2. Hai bucket mã hóa.** Kế hoạch: "`crm-public` (không mã hóa), `crm-originals`, `crm-private` (qua remote
  `crypt`)"; threat model M1 dòng I ghi `crm-originals`, `crm-private` mã hóa. Chọn mã hóa cả hai (ảnh gốc có thể là ảnh
  khách gửi chưa xử lý). Mỗi bucket một remote crypt trỏ đúng bucket kho ngoài (crypt mã hóa cả tên thư mục; remote đặt ở
  gốc sẽ đòi tạo bucket tên mã hóa, danh tính kho ngoài không được tạo bucket). Hai bucket dùng chung một cặp mật khẩu
  `RCLONE_CRYPT_PASSWORD`, `RCLONE_CRYPT_PASSWORD2` (đúng tên biến ở Global Constraints).
- **Q3. Tên bucket ở kho ngoài.** Thêm `FILESYNC_DST_BUCKET_PREFIX` (mặc định `crm-`, giữ đúng tên ở dev): tên bucket
  Backblaze B2 duy nhất toàn cầu, `crm-public` gần như chắc đã có người dùng. Dòng `file_sync_runs.bucket` luôn là tên
  bucket kho chính.
- **Q4. "Số tệp mới hơn lần chép thành công cuối" của đối chiếu.** Role `crm_filesync` không được `SELECT`, nên script
  không đọc được lần chép tốt cuối từ database. Chọn: `run.sh` ghi mốc **bắt đầu** lượt chép tốt của từng bucket vào
  volume `/state` (`last-ok-<bucket>`), `reconcile` đếm tệp kho chính mới hơn mốc đó bằng `rclone size --max-age`. Chưa
  có mốc thì không trừ (chặt hơn). Phương án khác: cấp `SELECT` cho role (trái "không gì khác" của Interface).
- **Q5. Nhịp sống ra ngoài** chỉ gửi khi cả ba bucket chép tốt (nơi nhận báo khi vắng nhịp = chép ngừng hay lỗi), không
  gửi sau mọi lượt. Kế hoạch ghi "xong cả lượt gửi curl", không nói lượt lỗi; gửi cả lượt lỗi thì nơi nhận không bao giờ
  biết chép hỏng khi cảnh báo bên trong cũng hỏng.
- **Q6. Lịch đối chiếu.** Vòng lặp của `run.sh` thức mỗi 60 s: chép khi đủ `FILESYNC_INTERVAL_S` (3600) kể từ lượt
  trước; đối chiếu một lần mỗi ngày khi giờ Việt Nam đã qua 03:30 và hôm nay chưa đối chiếu (ngày ghi trong `/state`).
  Container khởi động lại sau 03:30 vẫn đối chiếu đúng một lần trong ngày. `reconcile.sh` riêng để chạy tay.
- **Q7. `file-sync-age` khi chưa có lần chép nào** (đã bật `FILESYNC_EXPECTED`): báo gấp ("chưa có lần chép tốt"), như
  `backup-age` khi chưa có bản sao lưu. `/health/deep` `lastFileSyncAgeM` = tuổi lần chép tốt cũ nhất trong ba bucket,
  vắng khi còn bucket chưa chép; không tính vào `ok` của `/health/deep` (kế hoạch chỉ thêm trường; cảnh báo lo việc báo).
- **Q8. Bucket mã hóa so theo kích thước.** rclone crypt không có tổng kiểm chung với kho chính, nên `--checksum` rơi về
  so kích thước ở hai bucket mã hóa: tệp ở kho ngoài bị ghi đè cùng kích thước sẽ không bị `--immutable` bắt. Test ghi
  đè làm ở `crm-public` (md5). Kiểm nội dung bản mã hóa cần `rclone cryptcheck` (đọc lại cả bucket, tốn băng thông kho
  ngoài): ghi ở "Việc phát hiện thêm", không làm ở đây.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TP10: chép tốt cuối 89 phút → không `file-sync-age`; 91 phút → có, `urgent`; `FILESYNC_EXPECTED=false` → không bao giờ; reconcile `ok=false` → `file-sync-mismatch` | `TestRule_FileSyncAge` | `go test -race -count=1 -v -run 'FileSync\|TestDeepHealth' ./internal/platform/observability/` | `--- PASS: TestRule_FileSyncAge (0.63s)` |
| Một bucket quá hạn là đủ báo, văn bản chỉ nêu bucket quá hạn | `TestRule_FileSyncAgeAnyBucket` | như trên | `--- PASS: TestRule_FileSyncAgeAnyBucket (0.52s)` |
| `file-sync-failed` theo lần copy gần nhất; lần tốt sau đó hết báo; reconcile tốt sau lệch hết báo | `TestRule_FileSyncFailed` | như trên | `--- PASS: TestRule_FileSyncFailed (0.70s)` |
| `/health/deep` có `lastFileSyncAgeM` (lần tốt cũ nhất trong 3 bucket; nil khi còn bucket chưa có) | `TestDeepHealthFileSync`, `TestHealthDeepFileSyncAge` (httpapi, JSON) | như trên; `go test -race -count=1 -run TestHealthDeep ./internal/httpapi/` | `--- PASS: TestDeepHealthFileSync (0.53s)`; `ok elimaz.com.vn/crm/internal/httpapi` |
| Role `crm_filesync`: INSERT được; SELECT bảng bất kỳ (kể cả `file_sync_runs`), UPDATE, DELETE, INSERT bảng khác → 42501; không tạo bảng; `CONNECTION LIMIT 1`, không BYPASSRLS | `TestFileSyncRole` | như dòng 1 | `--- PASS: TestFileSyncRole (0.50s)` |
| Container `filesync` không có `S3_SECRET_ACCESS_KEY`/khóa của `crm-app`; dùng `crm-filesync`, role `crm_filesync`; cứng hóa, không root | `TestDevCompose_FilesyncIdentity` | `go test -count=1 -run TestDevCompose ./tests/deploy/` | `ok elimaz.com.vn/crm/tests/deploy` (đỏ trước khi có dịch vụ: 6 lỗi "thiếu …") |
| TP10 e2e: 3 đối tượng mỗi bucket → một lượt → kho ngoài có 3 mỗi bucket (đọc qua crypt); tên và nội dung bucket mã hóa không đọc thẳng được; `crm-tmp` không chép; lượt hai 0 tệp; xóa ở kho chính → kho ngoài còn; ghi đè ở kho ngoài → lượt sau `ok=false` và bản ở kho ngoài không bị chép đè; đối chiếu khớp → ok, kho ngoài thiếu → `ok=false`, tệp mới hơn lần chép tốt cuối được trừ | `TestFileSync` (build tag `filesync`) | `docker compose -f docker/compose.dev.yml --profile filesync build filesync && go test -tags filesync -count=1 -v -run TestFileSync ./tests/e2e/` (bộ crm-t45) | `--- PASS: TestFileSync (58.74s)`; log: `copy crm-public: 1 tệp lỗi (rclone thoát 6) … immutable file modified`; `reconcile crm-public: ok=false; kho chính 2 tệp …; kho ngoài 0 tệp` |
| Cổng kiểm của task (đổi migration, testkit → toàn bộ test) | mọi test + lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check | `ci-task.sh` của scripts agent | lần 1 đỏ (gocognit `TestDevCompose_FilesyncIdentity`, gofumpt `rules.go`; đã sửa); lần 2 `CI-TASK XANH (so với điểm tách nhánh 97f26b05…)` |
| Vòng lặp: chép khi khởi động, chép lại sau chu kỳ, dừng êm khi TERM | thử tay | `docker compose --profile filesync run -d -e FILESYNC_INTERVAL_S=60 filesync`; đếm `file_sync_runs`; `docker stop` | 3 dòng lúc 18:15:01, 6 dòng lúc 18:16:02; `docker stop` 0,24 s, mã thoát 0, log `nhận tín hiệu dừng` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `CREATE ROLE crm_filesync` không nằm trong migration 0126 mà ở bước dựng role của người quản trị (Q1); tệp thêm:
  `docker/postgres/init/01-roles.sql`, `init-drill/01-roles.sql`, `internal/testkit/pg.go` (`prepareEmpty` tạo role;
  `templateRecipe` lên v2), RUNBOOK. Migration 0126 cấp thêm `SELECT` cho `crm_app` (luật cảnh báo đọc bảng).
- Tệp thêm ngoài danh sách Files: `scripts/filesync/lib.sh` (phần chung của run/reconcile), `scripts/filesync/ls-offsite.sh`
  (liệt kê kho ngoài đã giải mã: test e2e và kiểm tay), `.dockerignore` (cho `scripts/filesync` vào ngữ cảnh dựng),
  `docker/compose.dev.yml` (biến, volume `filesync-state`, cứng hóa của dịch vụ đã có từ M1-40), `openapi.yaml` + mã sinh
  + `handlers_health.go` (trường `lastFileSyncAgeM` của `/health/deep` đi qua hợp đồng), `cmd/crm/main.go` (nối
  `FileSyncExpected`), `tests/deploy/compose_test.go` (ca "không có khóa của crm-app" kiểm ở mọi lần chạy test, không chỉ
  khi bật tag `filesync`), `internal/httpapi/handlers_health_test.go`.
- `reconcile.sh` không tự chạy theo lịch riêng: vòng lặp của `run.sh` gọi phần đối chiếu (Q6).
- Step 6 (kho thật, cần chủ shop): chưa chạy với kho thật; RUNBOOK ghi rõ.

## Việc phát hiện thêm

- `docker/compose.prod.yml` đặt `FILESYNC_EXPECTED: "true"` cho worker nhưng chưa có dịch vụ `filesync` (task-21.md đã
  ghi): deploy bản này lên production khi chưa thêm dịch vụ thì `file-sync-age` báo gấp mỗi 30 phút. Cần một task thêm
  `filesync` vào compose production (image dựng sẵn, biến từ tệp env) cùng lúc với Step 6, hoặc tạm đặt `false`. Không
  sửa ở đây (ngoài Files của task, cần tài khoản kho ngoài).
- Bucket mã hóa: `--immutable` chỉ so kích thước (Q8). Nếu cần bắt cả ghi đè cùng kích thước: thêm `rclone cryptcheck`
  hằng tuần (đọc lại cả bucket ở kho ngoài, tốn băng thông, cần chủ shop chọn).
- Kho ngoài chưa có khóa chống xóa (threat model L4, chờ chủ shop chọn nhà cung cấp): danh tính chép không xóa được, nhưng
  người có tài khoản kho ngoài vẫn xóa được.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lần 1, trên phần đã stage sau khi gộp main và `ci-task.sh` xanh). Phát hiện phải sửa:
không có. Ghi chú không bắt buộc (không sửa, ghi lại cho task sau):
- `reconcile_all` ghi ngày đối chiếu vào `/state` cả khi không ghi được dòng nào (database tạm hỏng): ngày đó không có
  dòng `reconcile`. Lúc database hỏng thì cảnh báo khác đã kêu; muốn chặt hơn thì chỉ ghi ngày khi đủ ba dòng.
- `CONNECTION LIMIT 1` áp cả cụm: container `filesync` chạy lâu trên cùng cụm với test thì `TestFileSyncRole` có thể hiếm
  khi gặp "too many connections for role".
- `compose.prod.yml` đặt `FILESYNC_EXPECTED=true` khi chưa có dịch vụ `filesync` (đã ở "Việc phát hiện thêm").
- Mục Ảnh hưởng thiếu dòng cho các `models.go` do sqlc sinh lại (thêm kiểu `FileSyncRun`), và `deephealth_test.go` không
  đổi (test mới ở `rules_filesync_test.go`).
