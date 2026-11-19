# Sổ tay vận hành CRM

Tệp này lớn dần theo từng task; Task 23 soát lại toàn bộ.

## Màn hình nhân viên (`crm-console`, Task 17)

- Dựng: `cd console && npm ci && npm run build` → tệp tĩnh trong `console/dist`. Container `crm-console` chỉ phục vụ
  tệp tĩnh, không chạy mã phía máy chủ (ADR-34). Trước khi phát hành: `gitleaks dir --no-banner --redact console/dist`
  không thấy gì (`make console-check` đã chạy).
- API và màn hình cùng một nguồn gốc (origin): `/api/*` và `/health/*` chuyển tới `api`, mọi đường khác trả tệp tĩnh;
  đường không có tệp trả `index.html` (ứng dụng một trang).
- Header đề xuất cho mọi câu trả lời của `crm-console`:

  ```
  Content-Security-Policy: default-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'
  X-Content-Type-Options: nosniff
  Referrer-Policy: same-origin
  ```

  `connect-src 'self'` đủ cho client sinh và luồng `/api/v1/stream` vì cùng nguồn gốc. Thêm nguồn ảnh (kho tệp, M1)
  thì thêm đúng tên miền đó vào `img-src`, không dùng `*`.
- `index.html` gửi kèm `Cache-Control: no-cache`; tệp trong `assets/` có mã băm trong tên, gửi
  `Cache-Control: public, max-age=31536000, immutable`.
- Luồng `/api/v1/stream` phải đi qua HTTP/2 suốt đường và không bị cổng vào giữ lại (buffering) (spec 04 mục 6).
- Dev: `cd console && CRM_VITE_PORT=<cổng> npx vite` (mặc định 4192, chuyển `/api`, `/health` sang `CRM_API_URL`,
  mặc định `http://127.0.0.1:4190`). Không dùng 4175, 4176, 4180, 4181.

## Migration (`crm migrate`, Task 3, Task 318)

- `crm migrate` giữ `pg_advisory_lock(727001)` suốt lần chạy: hai tiến trình migrate cùng lúc chạy lần lượt, mỗi tệp
  áp dụng đúng một lần.
- Trước khi chạy, mọi tệp đã áp dụng được so sha256 với `platform.migration_checksums`; tệp bị sửa hay bị mất thì
  từ chối cả lần chạy và nêu tên tệp. Cách sửa: trả tệp về đúng bản đã áp dụng, thay đổi đặt trong tệp mới.
- Tệp sắp chạy có lệnh thu hẹp (`DROP`, `RENAME`, đổi kiểu, `SET NOT NULL`) thiếu dòng `-- contract:` thì từ chối.
- **Số đến lệch thứ tự** (Task 318): tệp có số nhỏ hơn số lớn nhất đã áp dụng (ví dụ `0010` gộp sau `0012`, `0114`
  sau `0124`, do các làn làm song song) vẫn chạy: goose chạy các tệp lệch số trước (theo số tăng dần), rồi tới tệp
  mới. Bảng `platform.goose_db_version` vì thế ghi theo thứ tự chạy, không theo số. Checksum và luật `-- contract:`
  vẫn kiểm cả tệp lệch số.
- Luật gộp không đổi: trong một mốc vẫn gộp theo thứ tự số khi được; lệch thì vẫn chạy, không đổi số của migration
  đã gộp. Tệp đến sau không được giả định tệp số lớn hơn chưa chạy (ví dụ không tạo lại bảng mà tệp số lớn đã tạo).

## Image và compose máy chủ (Task 21)

- Image chính: `docker build -t crm:<commit> .` (CI dựng, ghim theo digest; ADR-43 không dựng trên máy chạy thật).
  Tệp chạy Go tĩnh trên distroless, chạy bằng uid 65532, không shell; kiểm sức khỏe bằng `/crm healthcheck`.
  Image PostgreSQL: `docker build --build-context backup=scripts/backup -t crm-postgres:<commit> docker/postgres`
  (mang sẵn `/opt/crm-backup/{base-backup.sh,weekly-verify.sh}`).
- Compose máy chủ: `docker/compose.prod.yml`; tên biến ở `docker/compose.prod.env.example`, tệp giá trị thật nằm ngoài
  repo. Không dịch vụ nào mở cổng ra máy: lưu lượng vào chỉ qua `cloudflared` (Cloudflare Tunnel). Nhờ vậy
  `cf-connecting-ip` mà `ingest` đọc không giả được bằng cách gọi thẳng IP của máy. **Không thêm `ports:`** cho
  `crm-ingest`, `crm-api` (`tests/deploy` đỏ). Đổi nền tảng (ví dụ Traefik thay tunnel) thì máy phải có tường lửa chỉ
  nhận dải IP của Cloudflare trước khi mở cổng.
- `imager` chỉ ở mạng `imaging` (internal): không ra internet, không tới PostgreSQL (ADR-45).
- Ba vai trò có `stop_grace_period: 30s` (worker cần 25 s tắt êm; ít hơn thì Docker giết giữa chừng, việc dở chờ
  hết giữ chỗ 90 s mới chạy lại). Giới hạn bộ nhớ ingest 256 MiB, api 512 MiB, worker 512 MiB, `GOMEMLIMIT` 90%
  (chưa đo; chỉnh theo Task 22 và bản thử).
- Chạy: `docker compose -f docker/compose.prod.yml --env-file <tệp> --profile migrate run --rm crm-migrate`, rồi
  `up -d --wait crm-worker`, rồi `crm-api`; `crm-ingest` chỉ khi mã của nó đổi. Chưa có kho ngoài: dùng `COMPOSE_FILE`
  có `compose.prod.no-filesync.yml` thay cho `-f` (mục "Chép tệp sang kho ngoài").
- Dev: `make image && make app-up` chạy ba vai trò từ image trên compose dev (profile `app`); `make image-check` kiểm
  cả đường (health 200, `healthy`, uid khác 0, không `.env*` trong image).

## Dựng PostgreSQL lần đầu trên máy chủ (Task 21; việc treo từ Task 4, Task 19)

Làm bằng `postgres` (superuser) **trước lần `crm migrate` đầu tiên**, trên database `crm` rỗng. Mật khẩu sinh mới,
chỉ nằm trong tệp biến của máy chủ:

```sql
CREATE ROLE crm_owner LOGIN PASSWORD '<mới>';
CREATE ROLE crm_app LOGIN NOBYPASSRLS PASSWORD '<mới>';
CREATE ROLE crm_search NOLOGIN BYPASSRLS;
-- Container chép tệp sang kho ngoài (Task M1-45): migration 0126 cấp INSERT vào platform.file_sync_runs, không gì khác.
-- Thiếu role thì migrate dừng ở 0126 (role "crm_filesync" does not exist): tạo role rồi chạy lại migrate.
CREATE ROLE crm_filesync LOGIN NOBYPASSRLS CONNECTION LIMIT 1 PASSWORD '<mới>';
GRANT crm_search TO crm_owner;
-- Migration 0010 chạy GRANT pg_monitor TO crm_app: thiếu dòng này thì migrate lỗi permission denied.
GRANT pg_monitor TO crm_owner WITH ADMIN OPTION;
GRANT CONNECT, CREATE ON DATABASE crm TO crm_owner;
GRANT CONNECT ON DATABASE crm TO crm_app;
GRANT CONNECT ON DATABASE crm TO crm_filesync;
\connect crm
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

Bản dev (`docker/postgres/init/01-roles.sql`) và bản diễn tập (`init-drill/01-roles.sql`) làm đúng các bước này.
Cụm đã dựng trước Task M1-45 (máy chủ đã chạy, volume dev cũ) chạy thêm bằng `postgres`, trước khi migrate tới 0126:
`CREATE ROLE crm_filesync LOGIN NOBYPASSRLS CONNECTION LIMIT 1 PASSWORD '<mới>'; GRANT CONNECT ON DATABASE crm TO
crm_filesync;` (database thử của `internal/testkit` tự tạo role này, mật khẩu dev `crm_filesync_dev`).

## Chép tệp sang kho ngoài mỗi giờ, đối chiếu mỗi ngày (Task M1-45, ADR-36)

- Container `filesync` (`docker/filesync/Dockerfile`, script ở `scripts/filesync/`): khi khởi động chép một lượt, rồi
  mỗi `FILESYNC_INTERVAL_S` giây (mặc định 3600) chép ba bucket `public`, `originals`, `private` (tên thật
  `<S3_BUCKET_PREFIX>public`…, mặc định `crm-public`, `crm-originals`, `crm-private`) sang kho ngoài bằng
  `rclone copy --immutable --checksum` (chỉ thêm: không bao giờ `sync`, `move`, `delete`); `crm-tmp` không chép.
  `crm-originals`, `crm-private` đi qua rclone crypt (tên và nội dung mã hóa trước khi rời máy); `crm-public` không mã hóa.
  Từ 03:30 giờ Việt Nam, mỗi ngày một lần, đếm số tệp và dung lượng hai phía. Mỗi bucket mỗi lượt một dòng
  `platform.file_sync_runs` (`kind` = `copy` / `reconcile`, `bucket` = tên logic `public` / `originals` / `private`, không
  có tiền tố (Task 410), `ok`, `objects`, `bytes`, `detail`); cả ba bucket chép tốt
  thì gọi `FILESYNC_HEARTBEAT_URL` (rỗng: bỏ).
- Danh tính: kho chính bằng `crm-filesync` (chỉ đọc, liệt kê), kho ngoài bằng một khóa chỉ ghi thêm (không xóa);
  database bằng role `crm_filesync` (chỉ `INSERT` một bảng, tối đa 1 kết nối). Container không có khóa của `crm-app`.
- Máy chủ: dịch vụ `filesync` của `docker/compose.prod.yml` chạy cùng `up -d` (image `CRM_FILESYNC_IMAGE` ghim digest,
  volume `filesync-state` giữ mốc lần chép tốt cuối; Task 410). Compose đòi đủ mọi biến `${…:?}` của cả tệp trước khi
  chạy bất kỳ dịch vụ nào, nên chưa có tài khoản kho ngoài thì: điền **giá trị giữ chỗ** (không phải khóa thật) cho
  `CRM_FILESYNC_IMAGE`, `FILESYNC_*`, `RCLONE_CRYPT_PASSWORD*` trong tệp biến, và đặt trong môi trường của máy chủ
  `COMPOSE_FILE=docker/compose.prod.yml:docker/compose.prod.no-filesync.yml` để **mọi** lệnh compose (deploy, lùi bản)
  đều gộp tệp đường lui: `filesync` bị loại khỏi project (không kéo image, không chạy), worker `FILESYNC_EXPECTED=false`
  (`tests/deploy` `TestProdCompose_NoFilesyncOverride`). Khi đó các lệnh dưới bỏ `-f docker/compose.prod.yml`. Có kho
  ngoài thì điền giá trị thật, bỏ `COMPOSE_FILE`, `up -d --wait`, kiểm `file-sync-age` hết cảnh báo.
- Đặt `S3_BUCKET_PREFIX` khác `crm-` thì quyền theo bucket trong tệp danh tính SeaweedFS của máy chủ
  (`CRM_SEAWEEDFS_S3_CONFIG`: `Read:crm-public`… của `crm-app`, `crm-filesync`) phải đổi theo cùng tiền tố, rồi chạy
  `bootstrap.sh main` với cùng `S3_BUCKET_PREFIX`.
- Biến (giá trị chỉ ở tệp biến của máy chủ): `FILESYNC_DATABASE_URL`; `FILESYNC_SRC_ENDPOINT`, `…_ACCESS_KEY_ID`,
  `…_SECRET_ACCESS_KEY`, `FILESYNC_SRC_REGION`, `FILESYNC_SRC_BUCKET_PREFIX` (mặc định `crm-`; compose production lấy
  từ `S3_BUCKET_PREFIX`, cùng biến với ứng dụng, nên hai bên không lệch); `FILESYNC_DST_ENDPOINT`, `…_ACCESS_KEY_ID`, `…_SECRET_ACCESS_KEY`,
  `FILESYNC_DST_REGION`, `FILESYNC_DST_PROVIDER` (mặc định `Other`), `FILESYNC_DST_BUCKET_PREFIX` (mặc định `crm-`;
  tên bucket kho ngoài = tiền tố + `public` / `originals` / `private`: tên bucket Backblaze B2 là duy nhất toàn cầu nên
  thường phải đặt tiền tố riêng); `RCLONE_CRYPT_PASSWORD`, `RCLONE_CRYPT_PASSWORD2` (≥ 32 ký tự, chủ shop giữ trong
  trình quản lý mật khẩu: **mất hai mật khẩu này là mất bản sao của ảnh gốc và tệp riêng tư**); `FILESYNC_HEARTBEAT_URL`.
  Worker đặt `FILESYNC_EXPECTED=true` để bật ba cảnh báo dưới.
- Chạy tay: một lượt `docker compose --profile filesync run --rm filesync --once`; đối chiếu
  `docker compose --profile filesync run --rm --entrypoint /opt/filesync/reconcile.sh filesync`; xem tệp đã giải mã ở
  kho ngoài `… --entrypoint /opt/filesync/ls-offsite.sh filesync private <tiền tố>` (tên bucket logic). Xem kết quả:
  `SELECT kind, bucket, ok, finished_at, objects, bytes, detail FROM platform.file_sync_runs ORDER BY finished_at DESC LIMIT 20;`
- Cảnh báo (bật bằng `FILESYNC_EXPECTED=true` ở worker; `/health/deep` có `lastFileSyncAgeM` = tuổi lần chép tốt cũ
  nhất trong ba bucket, vắng khi còn bucket chưa chép lần nào):
  - `file-sync-age` (urgent): lần chép tốt gần nhất của một bucket quá 90 phút, hay chưa có. Nghĩa: tệp mới chỉ có một
    bản, kho chính hỏng là mất. Việc: `docker compose ps filesync` (chết thì khởi động lại), log của container, rồi
    `detail` của dòng `copy` mới nhất; chạy `run.sh --once` bằng tay; kiểm khóa và chỗ trống của kho ngoài.
  - `file-sync-failed` (normal): lần chép gần nhất của một bucket lỗi. Đọc `detail` (lỗi đầu tiên của rclone).
    `immutable file modified` = một tệp ở kho ngoài khác bản ở kho chính (kho chính bị sửa đè, hoặc kho ngoài bị ghi đè):
    **không xóa hay ghi đè bản ở kho ngoài**; so hai bản (tải về, so sha256), tìm nguyên nhân, báo chủ shop. Lỗi mạng,
    khóa sai: sửa rồi chạy `--once`.
  - `file-sync-mismatch` (normal): lần đối chiếu gần nhất thấy kho ngoài ít tệp hơn kho chính (trừ tệp mới hơn lần chép
    tốt cuối). Đọc `detail` (số tệp hai phía); chạy `--once` rồi `reconcile.sh`; còn lệch thì liệt kê hai phía
    (`rclone lsf`) tìm tệp thiếu, kiểm có ai xóa ở kho ngoài (nhật ký của nhà cung cấp). Kho ngoài nhiều hơn là bình thường.
- **Chưa chạy với kho thật** (Step 6 của Task M1-45, cần chủ shop tạo tài khoản kho ngoài và hai mật khẩu crypt): khi có,
  cấu hình trên staging, chạy một lượt, chép một tệp riêng tư về và đọc được bằng mật khẩu, ghi kết quả vào đây.

## Hạn 24 giờ của crm-tmp (R10, Q18; Task 410)

- `crm-tmp` giữ ảnh tải lên chưa xử lý (ảnh riêng tư còn EXIF, GPS). Xóa sau 24 giờ nhờ **lifecycle TTL fast path** của
  SeaweedFS: `bootstrap.sh main` bật (`s3.bucket.lifecycle.fastpath -name crm-tmp -enable`); mỗi đối tượng ghi sau đó có
  `ttlSec 86400` và nằm ở volume `Ttl:1d`, filer và volume server tự xóa. Quy tắc `Expiration Days=1` một mình **không**
  xóa gì: `weed server` không chạy lifecycle worker (đo 19/11/2026, `docs/do-luong/seaweedfs-m1.md`).
- Kiểm trên máy: `echo 's3.bucket.lifecycle.fastpath -name crm-tmp' | weed shell` (trong container `seaweedfs`) phải in
  `fast path: enabled`. Đối tượng ghi **trước** khi bật không có hạn: liệt kê và xóa bằng `crm-admin`
  (`fs.meta.cat /buckets/crm-tmp/<khóa>` có `ttlSec 0`). Dev: `make probe-test` có `TestTmpExpiry_*`.
- Ràng buộc với Task 409: trần thử lại xử lý ảnh (`IMAGE_RETRY_MAX_HOURS`) phải **nhỏ hơn 24 giờ** (tính từ lúc tải
  lên). Ảnh riêng tư chỉ có bản gốc ở `crm-tmp` (R10), ảnh thường cũng vậy tới khi được chép sang `crm-originals`; thử
  lại sau 24 giờ thì bản gốc đã tự xóa, lượt đó ra `source_missing` thay vì xử lý được. Mặc định hiện là 24 (bằng hạn):
  ảnh còn lỗi hạ tầng ở giờ cuối có thể ra `source_missing`; đặt 23 nếu muốn chắc (task-410.md "Việc phát hiện thêm").
- Đổi hạn: chỉ đối tượng ghi sau lần đổi theo hạn mới (TTL gắn lúc ghi). Không bao giờ bật fast path cho bucket giữ
  phiên bản (SeaweedFS cũng không áp cho chúng).

## Dựng lại kho tệp (Task M1-46, ADR-36; TP08, TP09, TP12)

- **Kho tệp tắt đột ngột** (máy khởi động lại, container chết): khởi động lại SeaweedFS rồi chờ. Tệp đã báo xong
  không mất (diễn tập `docs/do-luong/files-drill-m1.md`); vài giây đầu sau khi healthcheck qua, đọc tệp có thể trả
  500 "volume not found" tới khi volume đăng ký lại; tự hết, không làm gì. Tải lên dở thì nhân viên tải lại.
- **Kho tệp hỏng hẳn** (đĩa hỏng, volume mất): dựng lại từ kho ngoài. Mất các tệp tải lên sau lần chép tốt cuối
  (khoảng một giờ); dữ liệu đơn hàng không ảnh hưởng (database sao lưu đường riêng).
  1. Dừng SeaweedFS chính; giữ nguyên volume hỏng sang một bên nếu còn đọc được (đừng xóa trước khi có bản mới).
  2. Khởi động SeaweedFS với volume rỗng, chạy `bootstrap.sh main …` (bucket, giữ phiên bản, chính sách, hạn crm-tmp, CORS,
     TTL fast path của crm-tmp) bằng image SeaweedFS, `WEED_MASTER` trỏ tới master (trong chính container `seaweedfs`:
     mặc định `localhost:9333`), `S3_BUCKET_PREFIX` như tệp biến. Xem mục "Hạn 24 giờ của crm-tmp".
  3. Chép ngược bằng danh tính **ghi được** kho chính (crm-admin), cùng hai mật khẩu crypt của lúc chép đi:
     `docker compose --profile filesync run --rm -e FILESYNC_SRC_ACCESS_KEY_ID=<khóa crm-admin> -e
     FILESYNC_SRC_SECRET_ACCESS_KEY=<bí mật crm-admin> --entrypoint /opt/filesync/restore.sh filesync`
     (`rclone copy` ba bucket, chỉ thêm; log mỗi bucket số tệp, dung lượng; thoát 1 khi có bucket lỗi thì chạy lại).
  4. Màn "Tệp bị mất" → "Kiểm lại toàn bộ tệp" (hay `POST /api/v1/platform/files/verify`, quyền
     `platform.files.manage`): lệnh nền `file.verify` hỏi kho từng tệp ready theo lô 500, tệp thiếu ảnh gốc hay biến
     thể thành `lost`. Theo dõi: `SELECT status, count(*) FROM platform.jobs WHERE kind = 'file.verify' GROUP BY 1;`
  5. Danh sách tệp mất (`GET /api/v1/platform/files/lost`, màn "Tệp bị mất") có mã mẫu; nhân viên tải lại đúng ảnh gốc
     (cùng tệp) thì ảnh cũ tự về `ready` (lệnh `file.process` phục hồi); ảnh không còn bản gốc thì xóa khỏi mẫu.
  6. Bật lại container filesync, kiểm `file-sync-age` hết cảnh báo.
- Diễn tập trên máy dev, bộ compose riêng (không bao giờ trên bộ dùng chung): `COMPOSE_PROJECT=<dự án riêng>
  scripts/files/crash-drill.sh` (TP08) và `scripts/files/restore-drill.sh` (TP09, in `FILES RESTORE OK
  restore_seconds=<x> lost=<n>`, ghi `file_sync_runs(kind='restore_drill')`). Ghi kết quả vào
  `docs/do-luong/files-drill-m1.md`.

## Sao lưu theo lịch và kiểm bản sao lưu hằng tuần (Task 21, ADR-37)

- Lịch (giờ Việt Nam; nơi chạy chốt ở bước staging — cron của máy hoặc dịch vụ theo lịch của nền tảng). Với cron của
  máy, chạy dưới người dùng có quyền docker:

  ```
  CRON_TZ=Asia/Ho_Chi_Minh
  0 2 * * *  docker compose -f /srv/crm/compose.prod.yml --env-file /srv/crm/crm.env exec -T -u postgres postgres /opt/crm-backup/base-backup.sh
  0 3 * * 0  docker compose -f /srv/crm/compose.prod.yml --env-file /srv/crm/crm.env exec -T -u postgres postgres /opt/crm-backup/weekly-verify.sh
  ```

  Cron không hiểu `CRON_TZ` (cron gốc của Debian) thì đặt giờ UTC: `0 19 * * *` và `0 20 * * 6` (thứ bảy 20:00 UTC =
  chủ nhật 03:00 giờ Việt Nam). Lệnh thoát khác 0 khi hỏng; cảnh báo vẫn đến từ `backup_runs`, không phụ thuộc thư của cron.
- `base-backup.sh`: `wal-g backup-push`, giữ 14 bản đầy đủ; mọi lần chạy (kể cả thiếu khóa mã hóa) ghi
  `platform.backup_runs(kind='base', ok, duration_seconds)`. Hạn mỗi lệnh WAL-G: `CRM_BACKUP_TIMEOUT` (mặc định 4 giờ).
- `weekly-verify.sh`: tải bản mới nhất vào cụm tạm `/var/lib/postgresql/verify` (cần chỗ trống bằng cỡ database trên
  cùng ổ), dừng ở điểm cuối của bản sao lưu, so số dòng `platform.tenants`, `identity.users`, `identity.audit_logs` với
  database thật (dung sai: số dòng tạo sau lúc bắt đầu bản sao lưu), ghi `backup_runs(kind='restore_verify')`, xóa cụm
  tạm. Hạn: `CRM_VERIFY_FETCH_TIMEOUT` (mặc định 2 giờ), `CRM_VERIFY_READY_TIMEOUT` (1 giờ) — WAL-G treo mãi khi sai
  hoặc thiếu khóa, nên quá hạn được tính là hỏng. Một lượt mỗi lúc (khóa `/var/lib/postgresql/verify.lock`).
  Trước khi tải, script đòi chỗ trống ≥ cỡ database thật + `CRM_VERIFY_MARGIN_BYTES` (mặc định 1 GiB); thiếu thì
  hỏng ngay (`không đủ chỗ trống`) thay vì làm đầy ổ của cụm thật. Việc khi gặp: tăng ổ hoặc đặt `CRM_VERIFY_DIR` sang
  ổ khác (không được trùng hay chứa `PGDATA`).
- Cảnh báo (bật bằng `BACKUP_EXPECTED=true` ở worker):
  - `backup-age` (urgent): không có bản `base` thành công trong 26 giờ. Việc: đọc `detail` dòng `base` mới nhất (bước
    hỏng), chạy lại `base-backup.sh` bằng tay, kiểm khóa S3 và `WALG_LIBSODIUM_KEY`, chỗ trống của kho ngoài.
  - `backup-verify` (urgent): lần `restore_verify` gần nhất `ok = false`. Việc: đọc `detail` (`backup-fetch` = không
    tải/giải mã được; `lệch số dòng` = bản sao lưu thiếu dữ liệu; `đang có lượt khác` = hai lượt chồng nhau, chạy lại);
    chạy `base-backup.sh` tạo bản mới rồi `weekly-verify.sh` bằng tay; chưa đạt thì coi như **không có bản sao lưu dùng
    được**, báo chủ shop ngay.
- Diễn tập trên máy dev: `make verify-drill` (Step 2 của Task 21), `scripts/backup/restore-drill.sh` (bấm giờ, Task 2).

## Chạy lát mỏng: một đơn đi trọn (Task M1-33, Điểm dừng M1-A)

- Test tự động: `go test -race -run TestMotDonDiTron ./tests/e2e/` — ba vai trò thật trong tiến trình test, hãng giả
  `tools/fakecarrier` chạy tiến trình riêng, PostgreSQL thật (database thử riêng). Ba ca: đơn đi trọn (kèm dấu vết nối
  webhook → lệnh `fulfillment.refresh_status` → consumer `cash`), hai đơn tranh chiếc cuối, chạy kịch bản bên dưới.
- Kịch bản tay: `scripts/demo/mot-don-di-tron.sh <base-url>` (cần `bash`, `curl`, `jq`). In 6 bước (tạo mẫu, nhập 5 và
  ghi sổ, lên đơn 2 cái ship 30000, tạo vận đơn, hãng báo `picked_up`, hãng báo `delivered`), thời gian và `available`
  sau mỗi bước. Mỗi lệnh ghi một `Idempotency-Key` mới, nên chạy lại tạo đơn mới (đặt `PRODUCT_CODE` khác nếu trùng mã).
- Biến: `WAREHOUSE_ID`, `CARRIER_ACCOUNT_ID` (bắt buộc), `CRM_ORIGIN` (một giá trị của `CONSOLE_ORIGINS`),
  `FAKECARRIER_URL` (trang `/admin` của hãng giả), và một cách xác thực: `CRM_ACCESS_JWT` (thẻ Access, header
  `Cf-Access-Jwt-Assertion`) hoặc `CF_ACCESS_CLIENT_ID` + `CF_ACCESS_CLIENT_SECRET` (service token chủ shop cấp cho
  staging; đặt trong phiên shell, không ghi vào tệp). Lấy hai mã sau `crm seed-dev` (chủ bảng, `psql`). Bảng có RLS ép
  cả chủ bảng (`FORCE ROW LEVEL SECURITY`), nên phải đặt ngữ cảnh tenant trong transaction, không thì ra 0 dòng:

  ```sql
  BEGIN;
  SELECT set_config('app.tenant_id', (SELECT id::text FROM platform.tenants WHERE name = 'ELIMAZ'), true);
  SELECT w.id AS warehouse_id, a.id AS carrier_account_id
  FROM inventory.warehouses w JOIN fulfillment.carrier_accounts a ON a.warehouse_id = w.id
  WHERE w.code = 'MAIN' AND a.external_account_ref = 'fake-1';
  COMMIT;
  ```

- Hãng giả phải gửi webhook về ingest: chạy `tools/fakecarrier` với `FAKECARRIER_WEBHOOK_URL=<ingest>/hooks/fakecarrier`
  và cùng `FAKECARRIER_WEBHOOK_SECRET` với `ingest`, `worker`; `worker`, `ingest` cần `FAKECARRIER_URL` (CRM_ENV khác
  production).
- **Trên compose dev, một lệnh (Task 395):** `make db-up`, `make image`, rồi `make demo-mot-don` (phiên song song: thêm
  `COMPOSE_PROJECT=crm-tNN` và các cổng `CRM_API_PORT`, `CRM_INGEST_PORT`, `CRM_FAKECARRIER_PORT`, `CRM_DEVACCESS_PORT`).
  Lệnh `scripts/demo/mot-don-compose.sh` sinh khóa webhook hãng giả mới (`openssl rand -hex 32`, chỉ trong biến môi
  trường `CRM_FAKECARRIER_SECRET` của lần chạy, không ghi tệp), bật `devaccess` và `fakecarrier`, migrate, `seed-dev`
  (`chu-shop@elimaz.dev`, đổi bằng `DEMO_EMAIL`), dựng lại `crm-ingest`, `crm-api`, `crm-worker` cùng khóa đó, lấy hai
  mã bằng câu SQL trên và thẻ từ `devaccess`, chạy 6 bước, rồi in sổ COD, tổng sổ COD, tồn của đơn.
  - `devaccess` (`tools/devaccess`, chỉ compose dev): máy chủ thẻ Cloudflare Access giả. Bộ khóa HTTPS ở
    `devaccess:8443` chỉ trong mạng compose; `crm-api` đặt `ACCESS_TEAM_DOMAIN=devaccess:8443` và tin chứng chỉ tự ký
    qua `SSL_CERT_DIR` (volume `devaccess-ca` chỉ đọc). Lấy thẻ tay: `curl "http://127.0.0.1:4195/token?email=<email>"`
    (cổng chỉ mở 127.0.0.1; thẻ hạn 1 giờ, có MFA). Khóa, chứng chỉ giữ trong volume `devaccess-data` của dự án compose.
  - Hãng giả giữ kiện trong volume `fakecarrier-data` (mã hãng tăng tiếp qua các lần chạy, không trùng mã vận đơn cũ).
    Xóa volume này mà giữ database thì mã hãng quay về `FC00000001` và trùng. Làm lại từ đầu: xóa cả dự án kèm các profile
    (`make db-reset` không kèm profile nên có thể bỏ sót volume của hãng giả, `devaccess`):
    `docker compose -p <dự án> -f docker/compose.dev.yml --profile app --profile fakecarrier --profile devaccess down -v`.
  - `make app-up` (không có `CRM_FAKECARRIER_SECRET`): ingest, worker không nối hãng giả, như trước; vận đơn mới kẹt
    `creating`.
  - Trang `/admin` của hãng giả và cổng phát thẻ chỉ mở `127.0.0.1`; production không có hai dịch vụ này
    (`tests/deploy`).
- Sổ COD chưa có API (M3): đọc bằng SQL trong cùng kiểu transaction trên (sau `set_config('app.tenant_id', …)`) —
  `SELECT account, balance FROM cash.cod_balances_v WHERE shipment_id = '<id>'`. Sau `delivered`: `carrier_holding`
  bằng COD, `to_collect` 0; tổng mọi dòng `cash.cod_ledger_entries` bằng 0.
- Phần giả: hãng vận chuyển (`tools/fakecarrier`, chỉ ngoài production). Hãng thật, đối soát COD, hoàn hàng thuộc M3;
  màn hình đơn, kho thuộc M2 và Phần C của M1.
- Kết quả trên máy dev (18/11/2026, test e2e): mỗi bước sau commit dưới 0,3 s (tạo vận đơn → `awaiting_pickup` ~0,2 s,
  `picked_up` → `shipping` và sổ COD ~0,25 s, `delivered` → đơn `delivered` và sổ COD ~0,2 s). Compose dev (18/11/2026,
  `make demo-mot-don`, Task 395): đủ 6 bước, tạo vận đơn → `awaiting_pickup` ~0,4 s, `picked_up` → `shipping` ~0,5 s,
  `delivered` → đơn `delivered` ~0,5 s; sổ COD `carrier_holding 530000`, `to_collect 0`, `sales_clearing −530000`, tổng 0;
  tồn còn 3, giữ 0, đang giao 0. Staging: chưa chạy (cần chủ shop OK và service token).

## Chạy dev ba vai trò (Task 3, Task 21; soát lại ở Task 23)

- `make db-up` dựng PostgreSQL 18, Jaeger, SeaweedFS (kho chính, kho ngoài giả). Phiên song song: đặt
  `COMPOSE_PROJECT=crm-tNN` và cổng riêng (`CRM_PG_PORT`, `CRM_JAEGER_UI_PORT`, `CRM_OTLP_PORT`, `CRM_S3_PORT`,
  `CRM_S3_OFFSITE_PORT`); không dùng 4175, 4176, 4180, 4181.
- Chạy từ mã nguồn (nhanh khi sửa): mỗi vai trò một cửa sổ, cùng biến của `docker/compose.dev.yml` (`x-crm-env`):
  `CRM_ROLE=migrate go run ./cmd/crm`, rồi `go run ./cmd/crm seed-dev <email>` (CRM_ROLE=migrate; dựng tenant ELIMAZ,
  chủ shop, định tuyến email; chạy lại không tạo trùng), rồi `CRM_ROLE=worker`, `CRM_ROLE=api`, `CRM_ROLE=ingest`.
- Chạy từ image: `make image && make app-up` (image `crm:<dự án compose>`, ghi đè bằng `CRM_IMAGE`; phiên song song
  không ghi đè image của nhau). `make app-down` gỡ ba vai trò, giữ database. `make image-check` kiểm cả đường.
- Kịch bản đi trọn: `make demo-mot-don` (mục "Chạy lát mỏng" ở trên).

## Đọc cảnh báo và việc phải làm (Task 19, Task 20)

Cảnh báo đến Telegram: `urgent` vào kênh gấp, nhắc lại mỗi 30 phút khi còn; `normal` vào kênh thường, cùng key 10 phút
một lần. Văn bản chỉ có tên luật, số, mã việc, dấu vân tay; không có dữ liệu cá nhân. Mọi cảnh báo cũng nằm trong
`GET /api/v1/platform/health` (quyền `platform.health.read`). Tóm tắt sức khỏe gửi 08:00 mỗi ngày.

| Key | Mức | Nghĩa | Việc phải làm |
|---|---|---|---|
| `queue-lag` | urgent | Việc tới hạn chờ quá 60 s | Xem `worker-heartbeat` còn không; `/health/deep` của worker; log worker theo `job_id` của việc lâu nhất; worker chết thì khởi động lại (việc dở tự về hàng chờ sau 90 s). Kéo dài dù worker sống: đo câu nhận việc (Task 22, phát hiện `ClaimJobs`). |
| `dead:<kind>` | normal | Việc hết lượt thử (8 lần) hay lỗi vĩnh viễn | Đọc `last_error` qua `GET /api/v1/platform/jobs?status=dead`; sửa nguyên nhân rồi `POST /api/v1/platform/jobs/{id}/retry` (kèm `Idempotency-Key`); việc không còn cần thì `…/abandon`. |
| `in-doubt:<kind>` | normal | Lệnh ra ngoài không chắc đã gửi, quá 120 s | **Không gửi lại mù.** Kiểm phía bên ngoài (Meta, hãng) xem lệnh đã tới chưa: đã tới thì `abandon`; chưa tới thì `retry`. |
| `spool` | urgent | ingest còn tệp tạm webhook quá 5 phút | Database không nhận ghi: xem `/health/ready` của ingest, kết nối PostgreSQL, chỗ trống đĩa. Tệp tạm tự đẩy vào khi database trở lại; không xóa tay thư mục spool. |
| `listen-down` | urgent | Mất kết nối `LISTEN` quá 2 phút | Realtime và đánh thức worker chậm lại (worker vẫn quét theo nhịp). Khởi động lại tiến trình báo lỗi; kiểm `max_connections`. |
| `ingest-heartbeat`, `api-heartbeat`, `worker-heartbeat` | urgent | Không có nhịp sống, hoặc cũ quá hạn | Tiến trình chết hoặc treo: `docker compose ps`, log của vai trò đó; khởi động lại. |
| `heartbeat-ping` | normal | Gửi nhịp sống ra ngoài lỗi 5 phút liền | Kiểm mạng ra ngoài và URL nhận nhịp sống (`HEARTBEAT_URL`); nơi nhận sẽ tự báo nếu kéo dài. |
| `disk:<tiến trình>` | urgent | Đĩa dùng trên 80% quá 2 phút | Dọn log cũ, ảnh tạm; xem `db-size`, `wal-size`; tăng ổ. |
| `hooks-attack` | normal | Có tin webhook sai chữ ký trong phút qua | Một vài tin: bỏ qua. Liên tục: kiểm luật giới hạn tần suất của Cloudflare cho `hooks`, khóa webhook có bị đổi phía hãng không. |
| `wal-archive` | urgent | Đẩy WAL lên kho ngoài hỏng hoặc trễ quá 5 phút | Mất khả năng khôi phục tới phút gần nhất. Kiểm khóa S3, chỗ trống kho ngoài, log `wal-g` trong container postgres. |
| `wal-size` | normal | WAL trên máy quá 2 GiB | Thường đi kèm `wal-archive`; xử lý nó trước. |
| `backup-age`, `backup-verify` | urgent | Mục "Sao lưu theo lịch" ở trên | Như đã ghi ở mục đó. |
| `file-sync-age` | urgent | Một bucket quá 90 phút không có lần chép tốt sang kho ngoài | Mục "Chép tệp sang kho ngoài" ở trên. |
| `file-sync-failed`, `file-sync-mismatch` | normal | Lần chép gần nhất lỗi; đối chiếu thấy kho ngoài thiếu tệp | Như trên; không xóa, không ghi đè gì ở kho ngoài. |
| `db-size` | urgent | Database quá 80% ngân sách đĩa | Xem bảng lớn nhất; `jobs` cũ (hạn giữ 30 ngày); tăng ổ. |
| `long-tx` | normal | Transaction mở quá 5 phút | `SELECT pid, now()-xact_start, state, query_id FROM pg_stat_activity ORDER BY 2 DESC`; tìm nơi gọi theo `trace_id`; `pg_terminate_backend(pid)` nếu treo. |
| `lock-wait` | normal | Có phiên chờ khóa quá 5 s | `pg_blocking_pids`; đối chiếu bảng thứ tự khóa (`docs/adr/0046-thu-tu-khoa.md`). |
| `jobs-dead-tuples` | normal | Bảng `jobs` quá 50.000 dòng chết | Autovacuum không theo kịp: `VACUUM (VERBOSE) platform.jobs`; xem `long-tx` (transaction dài chặn vacuum). |
| `pg-connections` | normal | Kết nối của ứng dụng quá 70% ngân sách | Kiểm `DB_MAX_CONNS` từng vai trò (ngân sách ADR-42); tìm rò kết nối qua `pg_stat_activity` (`usename`, `client_addr`, `state`, `backend_start`). |
| `slow-query` | normal | Câu có trung bình trên 500 ms, từ 100 lần gọi | Tra `query_id` trong `pg_stat_statements`; `EXPLAIN (ANALYZE, BUFFERS)`; thêm index bằng migration mới. |
| `new-error:<vân tay>` | normal | Loại lỗi ứng dụng mới | Đọc nhóm lỗi trong `platform.error_events` theo dấu vân tay; mở dấu vết theo `trace_id` của lần gặp đầu. |
| `error-spike` | normal | Quá 50 lỗi trong 5 phút | Thường là phụ thuộc ngoài hỏng hay deploy lỗi; nếu ngay sau deploy: lùi bản (mục dưới). |

Kiểm từ ngoài (`scripts/external-probe`, hai nơi, một nơi không thuộc Cloudflare) báo riêng khi `/health/ready` hay
`/health/deep` không trả lời: máy chủ, tunnel, hoặc cả ba vai trò đều chết — cảnh báo bên trong khi đó không gửi được.

## Tra log và dấu vết

- Log là JSON (`log/slog`) ra stdout của container; máy chủ giữ bằng volume log của nền tảng (chốt ở staging).
  Trường để tra: `request_id` (mỗi yêu cầu HTTP; body lỗi trả về cho màn hình cũng có `request_id`), `job_id` (mỗi lượt
  việc nền), `trace_id`, `span_id` (OpenTelemetry).
- Tra: `docker compose logs --no-log-prefix crm-api | jq -c 'select(.request_id == "<id>")'`; tương tự `job_id` với
  `crm-worker`. Từ một dòng có `trace_id`, mở Jaeger (`http://127.0.0.1:${CRM_JAEGER_UI_PORT:-16686}` ở dev; staging
  sau Cloudflare Access) → Search → dán `trace_id` vào ô Trace ID → thấy cả đường webhook → việc nền → lệnh ra ngoài.
- Log, dấu vết, cảnh báo, audit không bao giờ có SĐT, địa chỉ, nội dung tin, token. Thấy có thì đó là lỗi: báo ngay.

## Deploy, lùi bản

- **Khung giờ không deploy:** 11:30–13:30 và 19:30–21:30 (giờ Việt Nam, giờ bán cao điểm).
- **Thứ tự:** `migrate` (một lần, giữ khóa) → `worker` → `api`; `ingest` chỉ khi mã của nó đổi (ingest đứng lâu nhất
  để webhook không rơi). Migration theo luật mở rộng rồi thu hẹp: bản mới và bản cũ đều chạy được trên schema sau migrate.
- **Image:** chỉ chạy image CI dựng (nhãn mã commit, ghim digest trong tệp biến); không dựng trên máy chạy thật.
- **Lùi một bản deploy:** đặt lại `CRM_IMAGE` về digest của commit trước, `up -d --wait crm-worker`, rồi `crm-api`
  (và `crm-ingest` nếu đã deploy). **Không lùi database**: migration đã chạy được giữ vì bản trước vẫn chạy trên nó
  (luật mở rộng rồi thu hẹp). Bài thử lùi rồi tiến trên staging ghi ở mục "Bài thử trên staging" (khi chạy).
- Sau mỗi lần deploy hay lùi: `/health/deep` 200, không `error-spike` trong 10 phút, tóm tắt sức khỏe không có việc mới
  `dead`.

## Đổi khóa mã hóa trường (`FIELD_ENC_KEYS`, Task M2-33)

Bí mật lưu trong database (khóa API hãng của M3, token Page của M4) được mã hóa bằng gói `fieldcrypt`, khóa đọc từ một
biến `FIELD_ENC_KEYS` của vai trò `api` và `worker` (M3, M4 bắt buộc có; M0–M2 không cần).

- Dạng: `<key_id>:<base64 32 byte>` cách nhau dấu phẩy, ví dụ `k2:<khóa mới>,k1:<khóa cũ>`. `key_id` là 1–16 ký tự
  `a-z0-9`, không trùng. **Khóa đầu** dùng để mã hóa; mọi khóa trong danh sách dùng để giải mã. Tạo khóa:
  `openssl rand -base64 32`. Sai định dạng thì tiến trình không khởi động, lỗi nêu tên biến và vị trí mục, không in khóa.
- Mỗi ô mã hóa lưu kèm `key_id` của khóa đã dùng; bản mã gắn với đúng tenant, bảng, cột, dòng (chép sang dòng khác thì
  không mở được).
- **Đổi khóa định kỳ hoặc khi nghi lộ:**
  1. Thêm khóa mới lên **đầu** danh sách, giữ khóa cũ phía sau; đặt cùng giá trị cho `api` và `worker`, khởi động lại
     cả hai (theo thứ tự deploy). Từ lúc này mọi lần ghi dùng khóa mới; ô cũ vẫn đọc được.
  2. Ghi lại các ô còn dùng khóa cũ (`NeedsRewrap`): việc ghi lại do mốc sở hữu bảng cung cấp (M3 tài khoản hãng, M4
     token Page); chạy tới khi không còn ô nào mang `key_id` cũ.
  3. Bỏ khóa cũ khỏi danh sách, khởi động lại. Bỏ sớm hơn bước 2 thì ô cũ báo `ErrUnknownKey` (không đọc được bí mật,
     phải nhập lại từ trang của hãng hay kết nối lại Page).
- Bản sao lưu cũ chứa bản mã bằng khóa cũ: giữ khóa cũ ngoài máy chủ (cùng chỗ với khóa sao lưu) đủ thời hạn giữ bản sao
  lưu để còn khôi phục được.
- Khi lộ: làm ngay bước 1–3 như trên, đồng thời thu hồi chính các bí mật đã mã hóa ở nơi cấp (mục dưới) vì kẻ có khóa cũ
  và bản sao database đọc được chúng.

## Khi lộ secret

1. **Thu hồi** ngay ở nơi cấp (Meta, hãng, Cloudflare, kho S3, Telegram): vô hiệu hóa khóa cũ.
2. **Đổi khóa:** tạo khóa mới, đặt vào tệp biến của máy chủ, khởi động lại vai trò dùng nó (theo thứ tự deploy).
   Khóa mã hóa sao lưu (`WALG_LIBSODIUM_KEY`) **không đổi mất bản cũ**: giữ khóa cũ ở hai nơi để còn đọc bản sao lưu cũ,
   tạo bản sao lưu đầy đủ mới ngay bằng khóa mới.
3. **Gỡ khỏi mã và log:** xóa khỏi tệp, nếu đã vào lịch sử git thì viết lại lịch sử và báo mọi bản sao; xóa log có
   chứa; chạy `make gitleaks`. Ghi bản ghi sau sự cố.

## Ai nhận cảnh báo, phản hồi trong bao lâu (R9 — chủ shop điền)

| Mức | Ai nhận | Phản hồi trong | Ngoài giờ |
|---|---|---|---|
| urgent | | | |
| normal | | | |
| Kiểm từ ngoài báo hỏng | | | |

## Mẫu bản ghi sau sự cố

```
Ngày, giờ bắt đầu – kết thúc (giờ Việt Nam):
Ảnh hưởng (khách, đơn, tin nhắn, tiền; số lượng):
Phát hiện bằng gì (cảnh báo nào, ai báo), sau bao lâu:
Dòng thời gian:
Nguyên nhân gốc:
Đã làm gì để khôi phục:
Việc phải làm để không lặp lại (người làm, hạn):
Cảnh báo, test, sổ tay cần sửa:
```

## Diễn tập khôi phục

- Máy dev: `scripts/backup/restore-drill.sh` (bấm giờ `restore_seconds`), `make verify-drill` (Task 21).
- Staging (cần chủ shop OK, Task 23 bước 7.6): `restore-drill.sh` và `weekly-verify.sh` với kho ngoài thật; báo
  `restore_seconds`. Dựng lại cả máy: `docs/REBUILD.md`.

## Xuất cấu hình máy chủ (Task 23)

`scripts/ops/export-config.sh <thư mục ra>` với `CRM_COMPOSE_FILE`, `CRM_ENV_FILE`, `CRM_TUNNEL_CONFIG`,
`COMPOSE_PROJECT`, `CRM_CONFIG_S3_URI` (+ `CRM_S3_ENDPOINT`): tệp compose, cấu hình cloudflared, digest image đang chạy,
**tên** biến (không giá trị), giữ 8 bản ở máy và kho ngoài (`config/`). gitleaks thấy giá trị bí mật thì dừng, thoát 3,
không tạo tệp. Lịch đề xuất: chủ nhật 04:00 giờ Việt Nam và sau mỗi lần đổi cấu hình. Lần đầu: đọc lại bản xuất
(`tar -xzOf <tệp> | less`) để chắc không có giá trị khóa nào (Task 23 bước 7.7).

## CI (Task 23)

- `.github/workflows/ci.yml`: job `ci` (`make ci-tools`, `make db-up`, `make ci`), `image` (dựng một lần
  `crm:<mã commit>` và `make image-check`), `console-e2e` (`make console-e2e`), `mutation` (theo lịch chủ nhật 02:00).
  Luật của tệp nằm ở `tests/deploy/ci_test.go`. Chưa chạy trên GitHub: cần chủ shop OK tạo repo, push, branch
  protection, đẩy image lên registry.
- Mutation testing vùng cao: `make mutation` (vài giờ trên máy dev), kết quả và lý do đột biến còn sống ở
  `docs/mutation.md`.
