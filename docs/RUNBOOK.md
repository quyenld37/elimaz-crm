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
- Ràng buộc với trần thử lại xử lý ảnh (Task 409, Task 426): `IMAGE_RETRY_MAX_HOURS` (worker) phải **nhỏ hơn** hạn sống
  của `crm-tmp`. Ảnh riêng tư chỉ có bản gốc ở `crm-tmp` (R10), ảnh thường cũng vậy tới khi được chép sang
  `crm-originals`; thử lại sau hạn thì bản gốc đã tự xóa, lượt đó ra `source_missing` dù ảnh hợp lệ. Mặc định **23**
  giờ (dư 1 giờ: trần tính từ lúc tệp vào processing, đường PUT ký sẵn hạn 15 phút). Khai `24` trở lên thì worker
  **từ chối khởi động** (`IMAGE_RETRY_MAX_HOURS=… phải nhỏ hơn hạn sống của crm-tmp (24 giờ)`): hạ giá trị rồi chạy lại.
- Hạn sống có một nguồn Go: `config.TmpObjectTTL` (`internal/platform/config/config.go`); hạn thật do `bootstrap.sh`
  đặt (`<Expiration><Days>`, fast path theo đó). Test `TestTmpObjectTTLMatchesBootstrap` đỏ khi hai nơi lệch: đổi hạn
  thì sửa cả hai cùng lúc, và giữ `IMAGE_RETRY_MAX_HOURS` nhỏ hơn hạn mới.
- Đổi hạn: chỉ đối tượng ghi sau lần đổi theo hạn mới (TTL gắn lúc ghi). Không bao giờ bật fast path cho bucket giữ
  phiên bản (SeaweedFS cũng không áp cho chúng).

## Kho tệp xuất crm-exports (Task 515; kế hoạch M6 G5, Q12)

- Bucket `<S3_BUCKET_PREFIX>exports` (mặc định `crm-exports`) giữ tệp Excel của báo cáo (khóa `reports/<tenant>/…`) và
  danh sách khách có SĐT (khóa `customers/<tenant>/…`). **Không giữ phiên bản, không chép ra kho ngoài** (filesync chỉ
  chép `public`, `originals`, `private`; `crm-filesync` không có quyền gì trên bucket này), **tự xóa theo tiền tố**:
  `reports/` sau 7 ngày, `customers/` sau 1 ngày. Cơ chế như crm-tmp: hai luật S3 `Expiration` lọc theo tiền tố + lifecycle
  TTL fast path, PutObject gắn `ttlSec` theo luật khớp khóa (604800 / 86400), filer và volume server tự xóa.
- Ứng dụng: `crm-app` chỉ Read/Write/List; chính sách bucket từ chối **cả `DeleteObject`** (bucket không phiên bản: xóa
  là mất hẳn, ADR-36), bật phiên bản, đổi luật hết hạn, chính sách, CORS, xóa bucket. Hạn tải phía ứng dụng
  (`REPORT_FILE_TTL` ≤ 168h, `CUSTOMER_EXPORT_TTL` ≤ 24h; worker từ chối khởi động nếu vượt) không bao giờ dài hơn hạn
  kho (`config.ReportExportObjectTTL`, `CustomerExportObjectTTL`; `TestExportObjectTTLMatchesBootstrap` đòi bootstrap.sh
  khớp). Đổi hạn thì sửa cả hai nơi cùng lúc; chỉ tệp ghi sau lần đổi theo hạn mới.
- Kiểm trên máy (trong container `seaweedfs`): `echo 's3.bucket.lifecycle.fastpath -name crm-exports' | weed shell` in
  `fast path: enabled`; `fs.meta.cat /buckets/crm-exports/<khóa>` có `ttlSec` 604800 (reports) hay 86400 (customers).
  Dev: `make probe-test` có `TestExports_*`.
- **Máy thử / máy thật (bước của Task 244, cần chủ shop OK lúc vào máy chủ):**
  1. Tệp danh tính SeaweedFS của máy chủ (`CRM_SEAWEEDFS_S3_CONFIG`, ngoài repo): thêm vào `actions` của `crm-app`
     `"Read:<tiền tố>exports", "Write:<tiền tố>exports", "List:<tiền tố>exports"`; **không** thêm gì cho
     `crm-filesync`. Khởi động lại `seaweedfs` để nạp tệp.
  2. Chạy `bootstrap.sh main <endpoint> <tệp danh tính>` bằng image SeaweedFS ghim, cùng `S3_BUCKET_PREFIX`,
     `WEED_MASTER` như mục "Dựng lại kho tệp" bước 2 (chạy lại nhiều lần không lỗi). Log phải có `tạo …exports: 200`
     (hay 409 nếu đã có), `chính sách …exports`, `hạn theo tiền tố …exports: 200`, `hạn TTL fast path …exports: bật`.
  3. Kiểm: lệnh fast path ở trên; `crm-admin` đọc lại luật (`GetBucketLifecycleConfiguration`: đúng hai luật
     `reports/` 7, `customers/` 1) và giữ phiên bản rỗng; ghi thử một khóa `reports/probe/x` bằng `crm-app`, `fs.meta.cat`
     thấy `ttlSec 604800`, rồi để luật tự xóa (crm-app không xóa được). Chạy migration `0606` trước khi bật tệp xuất
     (hai mục đích `report_export`, `customer_export` trỏ `exports`).
  4. Đường lui: tệp xuất chưa chạy thì không cần làm gì (bucket rỗng); đã có tệp thì để luật hết hạn dọn, không xóa tay.
- Dựng lại kho tệp từ kho ngoài **không** khôi phục tệp xuất (không có bản sao, đúng ý: dữ liệu tạm có SĐT). Người dùng
  xuất lại; dòng `platform.files` cũ hết hạn tải theo `expires_at`.

## Dựng lại kho tệp (Task M1-46, ADR-36; TP08, TP09, TP12)

- **Kho tệp tắt đột ngột** (máy khởi động lại, container chết): khởi động lại SeaweedFS rồi chờ. Tệp đã báo xong
  không mất (diễn tập `docs/do-luong/files-drill-m1.md`); vài giây đầu sau khi healthcheck qua, đọc tệp có thể trả
  500 "volume not found" tới khi volume đăng ký lại; tự hết, không làm gì. Tải lên dở thì nhân viên tải lại.
- **Kho tệp hỏng hẳn** (đĩa hỏng, volume mất): dựng lại từ kho ngoài. Mất các tệp tải lên sau lần chép tốt cuối
  (khoảng một giờ); dữ liệu đơn hàng không ảnh hưởng (database sao lưu đường riêng).
  1. Dừng SeaweedFS chính; giữ nguyên volume hỏng sang một bên nếu còn đọc được (đừng xóa trước khi có bản mới).
  2. Khởi động SeaweedFS với volume rỗng, chạy `bootstrap.sh main …` (bucket, giữ phiên bản, chính sách, hạn crm-tmp, CORS,
     TTL fast path của crm-tmp, bucket crm-exports cùng luật tự xóa) bằng image SeaweedFS, `WEED_MASTER` trỏ tới master (trong chính container `seaweedfs`:
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

- Hãng giả phải gửi webhook về ingest: chạy `tools/fakecarrier` với `FAKECARRIER_WEBHOOK_URL=<ingest>/hooks/fake`
  (hãng giả tự thêm `/<route_key>` của tài khoản; từ Task M3-10 ingest chỉ nhận đường có route_key) và cùng `FAKECARRIER_WEBHOOK_SECRET` với `ingest`, `worker`; `worker`, `ingest` cần `FAKECARRIER_URL` (CRM_ENV khác
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

## Đơn, khách, mã giảm giá (M2; Task M2-32)

Mọi câu dưới đây chạy bằng `psql` với role chủ bảng trong một transaction có `SET LOCAL app.tenant_id = '<mã tenant>'`
(RLS áp cả cho chủ bảng: thiếu dòng này thì ra 0 dòng mà không lỗi). Chỉ đọc; sửa dữ liệu đi qua API hay task riêng.

**Đơn kẹt `cancel_requested`.** Đơn đã gửi hãng mà nhân viên hủy thì chờ hãng trả lời (TD03); chỉ đường của hãng
(`OnShipmentCancelConfirmed` → `cancelled`, `OnShipmentCancelRejected` → `confirmed`) đưa đơn ra khỏi trạng thái này,
nút Hủy trả `invalid_transition`. Tìm đơn chờ quá 24 giờ:

```sql
SELECT id, code, current_shipment_id, updated_at FROM ord.orders
WHERE status = 'cancel_requested' AND updated_at < now() - interval '24 hours' ORDER BY updated_at;
```

Đơn có `current_shipment_id`: xem vận đơn đó ở `fulfillment.shipments` và lệnh hủy gửi hãng (`platform.commands`, M3);
lệnh `in_doubt` thì hỏi hãng rồi xử lý theo mục "Đọc cảnh báo". Đơn **không** còn vận đơn hiện hành mà vẫn
`cancel_requested` là lỗi dữ liệu (review độc lập M2, B-02, task bù 431): không sửa tay, báo người làm task.

**Đơn nháp không tự hủy.** Lịch `order.expire_drafts` chạy mỗi 60 giây cho từng tenant, mỗi lượt tối đa một lô đơn:

```sql
SELECT name, next_run_at, last_enqueued_at FROM platform.schedules WHERE name = 'order.expire_drafts';
SELECT status, count(*), max(finished_at) FROM platform.jobs WHERE kind = 'order.expire_drafts'
  AND created_at > now() - interval '1 hour' GROUP BY status;
SELECT count(*) FROM ord.orders WHERE status = 'draft' AND draft_expires_at < now() - interval '5 minutes';
```

`last_enqueued_at` cũ hơn vài phút: tiến trình `worker` không chạy hay không nhận lịch (xem "Đọc cảnh báo"). Việc
`dead`: đọc lỗi trong `platform.jobs.last_error`, thử lại qua `POST /api/v1/platform/jobs/{id}/retry`.

**Lệch `used_count` của mã.** `used_count` phải bằng số lượt đang hiệu lực (`coupon_redemptions.status = 'active'`);
lượt mỗi khách (`coupon_customer_uses.used`) bằng số lượt đang hiệu lực của khách đó. Câu đối chiếu (0 dòng là đúng):

```sql
SELECT c.id, c.code, c.used_count, count(r.id) FILTER (WHERE r.status = 'active') AS active
FROM promotion.coupons c LEFT JOIN promotion.coupon_redemptions r ON r.coupon_id = c.id
GROUP BY c.id HAVING c.used_count <> count(r.id) FILTER (WHERE r.status = 'active');

SELECT u.coupon_id, u.customer_id, u.used, count(r.id) FILTER (WHERE r.status = 'active') AS active
FROM promotion.coupon_customer_uses u LEFT JOIN promotion.coupon_redemptions r
  ON r.coupon_id = u.coupon_id AND r.customer_id = u.customer_id
GROUP BY u.coupon_id, u.customer_id, u.used HAVING u.used <> count(r.id) FILTER (WHERE r.status = 'active');
```

Có dòng lệch: không `UPDATE` tay (crm_app chỉ được đổi qua `Redeem`/`Release`); ghi lại mã, giờ, các đơn liên quan và
mở task sửa. Lưu ý: sau khi gộp khách, lượt mỗi khách đang tính theo hồ sơ gốc lúc dùng (review độc lập M2, A-02, task
bù 430) nên câu thứ hai có thể lệch theo nhóm mà không phải lỗi đếm.

**Hoàn tác gộp bị chặn.** `merge_not_latest`: nhóm đã có lần gộp mới hơn — hoàn tác lần mới nhất trước, theo thứ tự
ngược. `merge_window_expired`: quá 30 ngày, không hoàn tác được; tách hồ sơ là việc tay của chủ shop (chuyển đơn sai về
hồ sơ đúng không có ở M2). Xem chuỗi gộp của một hồ sơ:

```sql
SELECT id, survivor_id, merged_id, cardinality(moved_ids) AS so_ho_so, matched_by, created_at, undone_at
FROM customer.customer_merges WHERE survivor_id = '<mã>' OR merged_id = '<mã>' ORDER BY created_at DESC;
```

**Chỉ số khách sai** (số đơn, tổng chi, tỷ lệ hoàn). Chỉ số luôn tính lại toàn bộ từ `ord.orders_v`, `ord.returns_v`
của mọi hồ sơ trong nhóm (consumer `customer_stats`), nên sai thường do việc của consumer chưa chạy hay chết:

```sql
SELECT status, count(*), max(finished_at) FROM platform.jobs WHERE kind = 'event.customer_stats'
  AND created_at > now() - interval '1 day' GROUP BY status;
```

Việc `dead` của đúng khách: thử lại qua `POST /api/v1/platform/jobs/{id}/retry` (tính lại cho gốc hiện tại). Không có
việc lỗi mà số vẫn sai (Task 434): chạy lệnh vận hành trong container worker (dùng DATABASE_URL = `crm_app` của
container, RLS vẫn áp; mã là của hồ sơ gốc hay hồ sơ con, tenant tự tìm):

```sh
docker compose -f /srv/crm/compose.prod.yml --env-file /srv/crm/crm.env exec -T crm-worker \
  /crm recompute-customer-stats <mã khách>
# tenant <mã>, gốc <mã>: 3 đơn, 2 đã giao, 1 hoàn qua hãng, tổng chi 740000 đồng, tỷ lệ hoàn 3333/10000, đơn cuối …
```

Lệnh làm đúng hai bước của consumer `customer_stats` trong một transaction: tính lại chỉ số cả nhóm từ đơn, phiếu (gốc
`lead` có đơn sang `customer`), rồi áp quy tắc nhãn; chạy lại bao nhiêu lần cũng cho cùng số. Mã thoát 0 xong; 1 khi
khách không có ở tenant nào hay database lỗi (chạy lại); 2 khi thiếu mã hay mã sai dạng.

**Pha thu hẹp sau khi bản M2 chạy ổn trên staging (Q30).** Lớp bọc M1 `customer.Create(ctx, tx, fullName)` (gọi
`CreateCustomer` với nguồn `staff`, người làm `system`) còn để giữ chữ ký M1; khi `grep -rn "customer.Create(" internal
cmd` không còn chỗ gọi ngoài test thì xóa trong một task thu hẹp (cùng test của nó). Mã đơn và mã phiếu trả dùng
sequence (`ord.order_code_seq`, `ord.return_code_seq`), không có bảng bộ đếm phải xóa (R2).

## Flash sale: Valkey ngừng, đóng đợt khẩn cấp, số liệu lệch (Task M5-37; FS10, FS11, FS12, HT27)

Kết quả thử hỏng giữa tải: `docs/do-luong/thu-tai-m5.md` (chạy lại: `CRM_CI_LOADTEST=m5:chaos scripts/ci/remote-test.sh …`,
hay `loadtest/m5_run.sh chaos` trên máy có compose dev riêng; không bao giờ trên bộ chung `crm-shared`).

- **Valkey ngừng hay bị xóa sạch** (`valkey-cache` hay `valkey-gate`): không cần làm gì. Cầu dao mở, `api` chạy dự phòng
  (chậm hơn, không sai: số suất do PostgreSQL giữ), cờ và bộ đếm dựng lại từ PostgreSQL khi Valkey trở lại. Chỉ theo dõi
  cảnh báo `valkey-down:cache|gate` (mở quá 2 phút). Quá 30 phút vẫn còn: khởi động lại container
  (`docker compose -p <dự án> --profile valkey up -d valkey-gate valkey-cache`), không xóa volume `valkey-gate`.
- **Đóng đợt khẩn cấp** (bán nhầm giá, lỗi dữ liệu): màn Flash sale → Đóng đợt (nhân viên có `promotion.flash.manage`), hoặc
  `POST /api/v1/flash-sales/{id}/close`. Suất còn trống trả về tồn; người đang giữ suất vẫn hoàn tất được tới hết hạn giữ.
  Đợt đã đóng không mở lại: tạo đợt mới.
- **`crm verify-flash --sale <id>` báo lệch** (đã bán + đang giữ + còn lại ≠ số suất, hay suất `held` không có đơn): dừng, KHÔNG sửa
  tay số dư. Đóng đợt (bước trên), chờ hết hạn giữ (tối đa số phút giữ của đợt), chạy lại `verify-flash`; còn lệch thì
  giữ nguyên dữ liệu, gửi kết quả `verify-flash` kèm giờ cho người viết mã. Ghi vào "Mẫu bản ghi sau sự cố".
- **Đổi số của bộ điều tiết** (cổng nhận vào): biến môi trường của vai trò `api` rồi khởi động lại `api`:
  `FLASH_ADMIT_PER_SECOND` (100 vé/giây một món), `FLASH_ADMIT_GLOBAL_PER_SECOND` (300 cả shop), `FLASH_MAX_INFLIGHT` (64 lượt
  đang xử lý); giới hạn khóa dịch vụ `RATE_LIMITS_JSON` (`web_key`, số khuyến nghị ở `thu-tai-m5.md`). Tăng vé/giây thì tăng
  `FLASH_MAX_INFLIGHT` theo và xem `tx_retry` trong `thu-tai-m5.md` trước; đổi xong thử một đợt nhỏ.
- **PostgreSQL ngừng giữa đợt**: số đo M5-37 (run 46135866042) cho thấy chỉ ~5% yêu cầu mua nhận 503 `retry_later`, phần lớn
  nhận mã lỗi khác (chưa rõ mã): website CHƯA hiện "tạm ngưng" đúng như kế hoạch, việc sửa mã đang chờ (xem "Việc phát hiện
  thêm" của `docs/tasks/task-220.md`). Dữ liệu vẫn đúng: bật lại PostgreSQL rồi chạy `verify-flash` cho đợt đang mở và
  kiểm không đơn trùng; nếu đang bán, cân nhắc đóng đợt trong lúc PostgreSQL ngừng.
- **`api` chết giữa đợt**: compose khởi động lại; đơn đang tạo dở lùi trọn; chạy `verify-flash` cho đợt đang mở.

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

## Danh mục mẫu cho e2e website (`crm seed-web-fixture`, Task 218)

Bộ e2e của website ở nguồn CRM (`apps/storefront-crm`, `playwright.crm.config.ts`) cần đúng danh mục fixture của
website trong CRM, đúng id (UUID v5 của id fixture, website tự tính). Chỉ dev, test, staging: `CRM_ENV=production` →
thoát 2, không đọc tệp, không mở database.

```
# 1. Website xuất tệp (chỉ ghi tệp, không gọi gì)
cd apps/storefront-crm && npx jiti scripts/export-fixture-for-crm.ts --out /tmp/elimaz-fixture.json
# 2. CRM đã migrate và seed-dev (nhân viên có email ở tenant cần seed); chạy bằng chủ bảng. Biến bắt buộc như vai trò
#    migrate: CRM_ENV (dev|test|staging), DATABASE_URL, DATABASE_OWNER_URL (cùng biến của docker/compose.dev.yml)
CRM_ROLE=migrate go run ./cmd/crm seed-web-fixture --file /tmp/elimaz-fixture.json --staff-email <email nhân viên>
# in (lần đầu, tệp mẫu 27/11): seed-web-fixture: tenant <mã>; tạo admin_unit 4, attribute 7, attribute_value 11,
#   category 6, coupon 1, product 9, variant 39; sửa stock 38; ảnh chưa seed: 19
# chạy lại cùng tệp: seed-web-fixture: tenant <mã>; tạo 0; sửa 0; ảnh chưa seed: 19
```

- Ghi một transaction qua public API (danh mục, thuộc tính, mẫu, biến thể, phân loại, mã giảm, tồn, địa giới thử);
  lỗi giữa chừng → không ghi gì. Người làm của audit là nhân viên `--staff-email`; tenant là tenant của nhân viên đó;
  mẫu thuộc người bán mặc định của tenant (người bán đường web dùng), `seller.id` của tệp chỉ để kiểm.
- Chạy lại cùng tệp: `tạo 0; sửa 0`. Tệp đã sửa giá, tồn (ca WB02): sửa đúng phần đổi, phát `price.changed`,
  `stock.changed` như nhân viên sửa. Tồn đặt **tuyệt đối**: số có thể bán ở kho MAIN = `stock` của tệp (phiếu điều
  chỉnh "seed-web-fixture"); giữ hàng của đơn cũ không bị nhả. Mã giảm còn đúng `usageLimit` lượt sau mỗi lần seed
  (số lượt tối đa = `usageLimit` + số đã dùng).
- Thoát 2: production, sai cách gọi, không phải `CRM_ROLE=migrate`, tệp sai (version ≠ 1, id không phải UUID v5 dưới
  `namespace`, tham chiếu lạ, sku/mã trùng, giá ≤ 0, tồn âm…). Thoát 1: lỗi database, mã địa giới thử trùng đơn vị đã
  có khác tên (`customer: mã đơn vị hành chính đã có…`), dữ liệu cùng id đã khác ở chỗ không sửa được (mã mẫu) → dùng
  database mới.
- Bộ đệm địa giới của web giữ 1 ngày: seed trước khi bật `api`, hoặc xóa khóa địa giới ở `valkey-cache` của máy e2e.
- Ngày tạo của mẫu mới = 2026-01-01 00:00 UTC + `position` giây (sắp xếp «Mới nhất» theo tệp; Task 511). Mẫu đã có
  từ lần seed trước giữ ngày tạo cũ (catalog không sửa ngày tạo): cần đúng thứ tự thì dùng database mới.
- Hãng giao hàng trên web cho checkout e2e (Task 511): thêm `--web-carrier "Hãng giao hàng A"` (tùy chọn
  `--web-carrier-fee 30000`, mặc định 30000, 0–500000). Lệnh bật tài khoản hãng giả `web-e2e` (adapter `fake`) ở kho
  MAIN đúng tên, hiện trên web, là ưu tiên, phí dự phòng = phí; ghi mã địa giới thử của tệp cho hãng giả (mã hãng =
  mã CRM). In thêm `sửa carrier_account 1, carrier_location 4, web_carrier_settings 1` lần đầu; chạy lại 0. Để báo
  phí đúng 30.000 kèm tên hãng: chạy `tools/fakecarrier` với `FAKECARRIER_QUOTE_FEE=30000` và đặt `FAKECARRIER_URL`
  cho `api` (và `worker`); hãng giả không chạy thì web chỉ có phí ước tính 30.000 không tên hãng.
- Chưa làm (tệp task 218, 511): ảnh sản phẩm (cần kho tệp và worker xử lý ảnh); giữ thứ tự biến thể của fixture (CRM
  chưa có thứ tự biến thể do nhân viên đặt; web và màn hình nhân viên cùng xếp theo thứ tự tạo).

## Nhập dữ liệu website đêm mở (`crm import-website-data`, Task 467)

Nhập một lần dữ liệu admin cũ của website (chiến dịch giảm giá, banner trang chủ kèm ảnh, bộ phối, góp ý, cài đặt quà
sinh nhật, tạm dừng nhận đơn web, hãng trên web) vào một tenant CRM. Đầu vào là thư mục `admin-export-<giờ>/` do
website xuất (`node scripts/export-admin-data-for-crm.ts --out-dir <thư mục ngoài repo>`, Task 466; data.json
`version` 1). Không đọc Nhanh, không gọi website. **Hồ sơ sinh nhật không nhập ở đây** (website
`scripts/import-birthdays-to-crm.ts`); `unmatched.json` chỉ được đếm, không nhập.

1. Chép thư mục xuất lên máy chủ (thư mục 0700: có SĐT, nội dung góp ý), gắn vào container worker chỉ đọc.
2. Chạy khô (cùng đường trong một transaction rồi lùi; không tải ảnh, chỉ kiểm tệp ảnh tại máy):

```sh
docker compose -f /srv/crm/compose.prod.yml --env-file /srv/crm/crm.env exec -T crm-worker \
  /crm import-website-data --dir /import/admin-export-<giờ> --tenant <mã tenant UUID> --dry-run
# import-website-data: CHẠY KHÔ (đã lùi, không ghi gì), tenant <mã>
# loại                   thêm  bỏ qua   lỗi
# góp ý                    20       0     0
# …
# - chiến dịch: <mã> đang bật trên website → nhập ở tạm dừng: xem trước rồi bật lại
```

3. Đọc từng dòng «lỗi»: mẫu chưa có trong CRM, danh mục không khớp đúng một danh mục theo đường tên, ảnh banner hỏng,
   ảnh bộ phối là ảnh riêng SKU, hãng không có tài khoản đang hoạt động… Sửa dữ liệu CRM (hay xuất lại ở website) rồi
   chạy khô lại tới khi chỉ còn lỗi đã chấp nhận.
4. Chạy thật: thay `--dry-run` bằng `--apply` (tuỳ chọn `--image-wait 3m`). Phải chạy trong container **worker**
   (`CRM_ROLE=worker`: có S3_*; vai trò khác thoát 2 «--apply cần kho tệp»), và tiến trình worker phải đang chạy để
   xử lý ảnh banner (`file.process`). Mỗi loại một transaction (thứ tự khóa ADR-46); một bản ghi lỗi không làm hỏng
   bản ghi khác; ảnh banner hỏng chỉ làm banner đó lỗi.
5. Chạy lại bao nhiêu lần cũng được: khóa là `id` của website (chiến dịch, banner, bộ phối, góp ý); cài đặt so giá trị
   trước khi ghi; nhân viên CRM đã lưu khác thì giữ của CRM. Lần hai in toàn «bỏ qua», 0 thêm.

Mã thoát: 0 xong không lỗi; 1 có bản ghi lỗi (xem bảng) hay database lỗi; 2 sai cờ, thư mục, `version` lạ, tenant
không có, `--apply` không có kho tệp. Bảng và log chỉ có số, mã, lý do (không SĐT, tên, nội dung góp ý). Chiến dịch
đang bật trên website nhập ở **tạm dừng** (chủ shop OK 27/11): nhân viên xem trước rồi bật lại trên màn Chiến dịch.
Không chuyển yêu cầu đơn cũ, nhật ký cũ. Đường lui: dữ liệu đã nhập là dữ liệu thường của CRM (sửa, xóa trên màn hình;
audit `actor_type = 'system'`, `actor_id` là mã cố định của `import-website-data`). Chạy thật trên máy chủ: M7 (chưa
chạy; Task 467 chỉ chạy khô trên database thử riêng, xem `docs/tasks/task-467.md`).

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

## Hộp chat Facebook và màn Live (M4; Task M4-42)

Kênh Facebook chỉ bật khi vai trò có `META_APP_SECRET` (và `FIELD_ENC_KEYS` cho token Page). Mọi việc dưới đây làm
trên màn hình (`/settings/channels`, `/inbox`, `/live`) bằng tài khoản có quyền `channel.manage` trừ khi ghi khác. **Chờ server thử
của hệ mới:** các bước có Meta thật (kết nối Page thật, live thật) chưa chạy trên staging (Task 181); lệnh dưới đây đã
kiểm với máy chủ Meta giả `tools/fakegraph` và test của gói.

### Cảnh báo của M4

Văn bản chỉ có tên Page (công khai), kênh, mã tài khoản hay mã phiên; không token, không nội dung tin.

| Key | Mức | Nghĩa | Việc phải làm |
|---|---|---|---|
| `channel-auth:<tài khoản>` | urgent | Token Page hết hiệu lực hoặc mất quyền (Meta trả lỗi 190/10/200, kiểm sức khỏe thấy mất quyền). Tin vẫn nhận, chưa gửi được; Page ở `degraded` | Quản trị Page **kết nối lại Page** (mục dưới). Nếu do đổi mật khẩu Facebook hay gỡ app: kết nối lại bằng người còn quyền quản trị Page. |
| `channel-fields:<tài khoản>` | normal | Page thiếu trường webhook đã đăng ký; CRM đã tự đăng ký lại | Không cần làm gì nếu không lặp lại. Lặp lại: kiểm trang app Meta (11 trường webhook) và quyền của token (kết nối lại). |
| `channel-subscribe:<tài khoản>` | urgent | Lệnh đăng ký nhận tin cho Page thất bại hẳn (sau khi kết nối) | Page không nhận tin mới. Bấm "Kiểm tra kết nối" (`POST /api/v1/channels/{id}/health-check`); vẫn lỗi → kết nối lại Page; vẫn lỗi → xem `last_error` của lệnh `command.fb.subscribe_page` (`GET /api/v1/platform/jobs?status=dead`). |
| `webhook-silence:<tài khoản>` | urgent | Trong giờ bán, Page không nhận sự kiện nào từ Meta hơn 30 phút; CRM đã tự đăng ký lại và hỏi bù tin, comment | Xem trang trạng thái của Meta (sự cố toàn cầu?); xem `/health/ready` của `ingest`, tunnel Cloudflare cho `hooks`; thử nhắn Page bằng một tài khoản thật. Tin bị lỡ được hỏi bù tự động (`TestBackfill_Dedupe`). |
| `meta-policy:<tài khoản>` | urgent | Meta báo vi phạm chính sách nhắn tin (`messaging_policy_enforcement`) | Dừng gửi tự động (tắt tự trả lời, chào, nhắc theo dõi ở `/settings/pages/<Page>`); đọc thư của Meta trong Business Suite; xử lý trong 7 ngày (không xử lý có thể bị chặn gửi). Báo chủ shop. |
| `live-source:<phiên>` | normal | Luồng comment trực tiếp (SSE) của Meta hỏng; CRM đã chuyển sang hỏi định kỳ mỗi 2 s, tự thử lại luồng | Không cần làm gì trong phiên; comment vẫn về (chậm hơn vài giây). Sau phiên: nếu lặp lại nhiều phiên, đặt `source_mode = poll` cho Page. |
| `live-missing:<phiên>` | normal | Đối chiếu cuối phiên: CRM có ít comment hơn số Meta đếm; phần thiếu đã được điền | Mở phiên trên `/live`, soát các comment vừa điền xem có ai đặt hàng chưa được chốt. Thiếu nhiều lần liền: báo kỹ thuật (luồng và hỏi định kỳ cùng lỡ). |
| `graph-version` | normal | Phiên bản Graph API đang dùng (`META_GRAPH_VERSION`) còn dưới 180 ngày là hết hạn | Kỹ thuật: nâng `META_GRAPH_VERSION`, `META_GRAPH_VERSION_EXPIRES`, chạy lại test hợp đồng (`channeltest.RunContract`) với máy chủ Meta giả, deploy trước hạn. |
| `send-mode:<tài khoản>` | normal | Page vừa chuyển từ chỉ xem sang **gửi thật** | Xác nhận đúng là chủ shop chủ động bật (audit `channel.send_mode`). Không phải → tắt ngay (mục dưới) và đổi mật khẩu tài khoản đã bật. |
| `data-use-checkup:<hạn>` | normal | Hạn "Data Use Checkup" hằng năm của app Meta (`META_DATA_USE_CHECKUP_DUE`) còn ≤ 30 ngày; nhắc lại 7 ngày một lần | Chủ shop vào trang app Meta → App Review → Data Use Checkup, xác nhận các quyền; xong thì đặt hạn năm sau vào `META_DATA_USE_CHECKUP_DUE`. Quá hạn: app bị hạn chế quyền. |

Lệnh gửi ra ngoài của M4 (`command.fb.send_message`, `command.fb.reply_comment`, `command.fb.private_reply`, `command.fb.hide_comment` và các lệnh `command.fb.auto_*`) dùng chung cảnh
báo `in-doubt:<kind>` và `dead:<kind>` của M0 ở bảng trên.

### Kết nối lại Page

1. `/settings/channels` → Page cần kết nối lại → "Kết nối lại" (hoặc "Kết nối Page" nếu đã ngắt): đăng nhập Facebook bằng người có
   quyền quản trị Page, chọn đúng Page, cấp đủ quyền được hỏi.
2. CRM thay token (mã hóa bằng khóa đầu của `FIELD_ENC_KEYS`), đăng ký lại 11 trường webhook, chuyển Page về `active`.
   Lịch sử hội thoại giữ nguyên; chế độ gửi giữ nguyên như trước khi hỏng.
3. Kiểm: "Kiểm tra kết nối" xanh; nhắn thử Page từ một tài khoản thật, tin hiện trên `/inbox` trong vài giây.
4. Page "đã kết nối ở shop khác": Page đó đang gắn với tenant khác, ngắt ở tenant kia trước.

### Bật, tắt chế độ chỉ xem

- Page mới kết nối luôn ở **chỉ xem** (`read_only`): nhận tin, comment; không gửi gì ra ngoài, kể cả tự động.
- Bật gửi thật: `/settings/channels` → Page → "Chế độ gửi" → "Gửi thật" (`PUT /api/v1/channels/{id}/send-mode`, cần `Idempotency-Key`,
  kèm `version`). Cảnh báo `send-mode` được gửi. Chỉ bật khi app đã có quyền (App Review / Standard Access, Task 181).
- Tắt (về chỉ xem) khi: Meta cảnh báo chính sách, nghi gửi nhầm hàng loạt, đổi người quản trị. Có hiệu lực ngay với lệnh
  chưa gửi (lệnh đang chờ bị từ chối `read_only`, không gửi trễ).

### Tin "không rõ đã gửi" và "chưa gửi được"

- Lệnh gửi có hạn 3 phút: quá hạn mà chưa gửi → tin "chưa gửi được" (không bao giờ gửi trễ). Nhân viên bấm "Gửi lại"
  (`POST /api/v1/messages/{id}/resend`) hoặc "Bỏ" (`…/discard`).
- Mạng treo sau khi Meta có thể đã nhận → tin **"không rõ đã gửi"** (`in_doubt`): CRM chờ echo của Meta 60 s; echo về thì
  tự gộp thành "đã gửi". Sau 60 s màn hình hỏi nhân viên. **Không gửi lại mù:** mở hội thoại trên Business Suite (hoặc
  Messenger của Page) xem tin đã tới khách chưa; đã tới → "Bỏ" (giữ một bản, echo về sau vẫn gộp); chưa tới → "Gửi lại".
- Trả lời comment, nhắn riêng comment "không rõ": CRM tự tra Meta (`lookup`) trước khi hỏi; nhắn riêng chỉ được một lần
  mỗi comment, nên "Bỏ" mở khóa lại cho lần nhắn sau.
- Tin **tự động** (tin chào, tự trả lời comment, tự nhắn riêng, nhắc theo dõi) không rõ đã gửi hay quá hạn 5 phút: CRM
  tự bỏ (trạng thái "đã bỏ", lý do `auto_dropped`, lượt quy tắc `dropped`), không hỏi nhân viên, không gửi lại (04 mục
  5.3). Muốn chắc khách đã nhận: xem hội thoại trên Business Suite, nhắn tay nếu cần.

### Theo dõi live bằng tay khi không tự nhận

CRM tự nhận phiên live qua webhook (`live_videos`) và hỏi định kỳ danh sách video của Page. Phiên không hiện trên `/live`
sau 1 phút từ lúc phát:

1. `/live` → "Theo dõi bằng tay" → dán mã video hoặc đường dẫn video live của Page (`POST /api/v1/channels/{id}/live-sessions`).
2. Comment từ lúc đó về qua luồng của Meta hoặc hỏi định kỳ; comment trước đó được đọc bù.
3. Hết live: phiên tự kết thúc khi Meta báo; không thì "Kết thúc" (`POST /api/v1/live-sessions/{id}/end`) — CRM chạy đối
  chiếu cuối phiên một lần (cảnh báo `live-missing` nếu thiếu).
4. Worker giữ luồng bị khởi động lại giữa phiên: worker khác nhận lại trong 15 s, đọc bù phần lỡ; không cần làm gì.

### Đổi khóa `FIELD_ENC_KEYS` với token Page

Theo mục "Đổi khóa mã hóa trường" ở dưới; **một khóa dùng chung** cho M3 (khóa API hãng), M4 (token Page), M7. Riêng
token Page:

1. Thêm khóa mới lên đầu danh sách, giữ khóa cũ; đặt cho `api`, `worker` (và `ingest` nếu có biến), khởi động lại.
2. Ghi lại token Page bằng khóa mới: chưa có việc tự ghi lại (ghi ở `docs/tasks/task-182.md`), nên **kết nối lại từng
   Page** (mục trên); token mới luôn mã hóa bằng khóa đầu. Kiểm còn token khóa cũ (chạy bằng `crm_app`, trong ngữ cảnh
   tenant):
   ```sql
   BEGIN; SELECT set_config('app.tenant_id', '<mã tenant>', true);
   SELECT id, name FROM channel.channel_accounts WHERE credential_kid = '<key_id cũ>'; COMMIT;
   ```
3. Hết dòng (ở mọi tenant, và khóa API hãng của M3 cũng đã ghi lại) mới bỏ khóa cũ. Bỏ sớm: Page báo `channel-auth`,
   kết nối lại là hết.

### Yêu cầu xóa dữ liệu của Meta

- Người dùng Facebook gỡ app hoặc yêu cầu xóa → Meta gọi `POST /hooks/facebook/data-deletion` (ký `signed_request`
  bằng App Secret). `ingest` kiểm chữ ký, ghi yêu cầu (theo băm `user_id`), trả mã xác nhận và đường dẫn trang trạng thái
  `GET /hooks/facebook/data-deletion/status/<mã>`.
- Worker (lịch `platform.data_deletion`) xóa dữ liệu đăng nhập Facebook của người đó (phiên OAuth, danh tính người dùng
  app) ở **mọi** tenant, rồi trạng thái thành "đã xong". Tin nhắn khách gửi Page **không** thuộc yêu cầu này (là dữ liệu
  của Page, không phải của người dùng app); khách đòi xóa hội thoại thì xử lý như yêu cầu xóa dữ liệu khách của M2.
- Kiểm: yêu cầu kẹt chưa xong quá 1 ngày → xem lệnh `dead` của lịch `platform.data_deletion`; sửa nguyên nhân rồi
  `retry`. Không xóa tay dòng yêu cầu (là bằng chứng đã xử lý cho Meta).

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
  `crm:<mã commit>` và `make image-check`), `console-e2e` (`make console-e2e`), `mutation` (chỉ chạy bằng tay: `gh workflow run ci.yml --ref main`, Task 493 bỏ lịch tuần vì tốn phút). Test từ xa của task: `.github/workflows/test-remote.yml` qua `scripts/ci/remote-test.sh`; máy chạy do
  `scripts/ci/pool.py` chọn (Task 494, 495): blacksmith → ubicloud (`ubicloud-standard-2`, dừng ở 1.100 phút/tháng) →
  github (vps tắt mặc định từ Task 498, `CRM_CI_POOL_SLOTS=vps=1` bật lại); máy Mac chỉ dự phòng cuối (dòng `== DỰ PHÒNG CUỐI: chạy trên máy Mac (lý do: …)`). Xem chỗ bận và sổ
  phút: `python3 scripts/ci/pool.py status`.
  Gộp mốc (m4 vào main): test toàn bộ + e2e một lệnh `CRM_CI_E2E=1 scripts/ci/merge-test.sh <commit gộp>` (Task 499):
  thêm job `e2e` (`make dev` rồi `go test -race ./tests/e2e/...`) và `console-e2e` (`make console-e2e`, trình duyệt
  cache theo `console/package-lock.json`); chỉ ở bể từ xa (không Mac, không vps). Lượt đầu 27/11: hai job e2e ~1,5 và
  ~4 phút máy blacksmith.
  Luật của tệp nằm ở `tests/deploy/ci_test.go`. Chưa chạy trên GitHub: cần chủ shop OK tạo repo, push, branch
  protection, đẩy image lên registry.
- Mutation testing vùng cao: `make mutation` (vài giờ trên máy dev), kết quả và lý do đột biến còn sống ở
  `docs/mutation.md`.
