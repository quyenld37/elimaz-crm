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
  `up -d --wait crm-worker`, rồi `crm-api`; `crm-ingest` chỉ khi mã của nó đổi.
- Dev: `make image && make app-up` chạy ba vai trò từ image trên compose dev (profile `app`); `make image-check` kiểm
  cả đường (health 200, `healthy`, uid khác 0, không `.env*` trong image).

## Dựng PostgreSQL lần đầu trên máy chủ (Task 21; việc treo từ Task 4, Task 19)

Làm bằng `postgres` (superuser) **trước lần `crm migrate` đầu tiên**, trên database `crm` rỗng. Mật khẩu sinh mới,
chỉ nằm trong tệp biến của máy chủ:

```sql
CREATE ROLE crm_owner LOGIN PASSWORD '<mới>';
CREATE ROLE crm_app LOGIN NOBYPASSRLS PASSWORD '<mới>';
CREATE ROLE crm_search NOLOGIN BYPASSRLS;
GRANT crm_search TO crm_owner;
-- Migration 0010 chạy GRANT pg_monitor TO crm_app: thiếu dòng này thì migrate lỗi permission denied.
GRANT pg_monitor TO crm_owner WITH ADMIN OPTION;
GRANT CONNECT, CREATE ON DATABASE crm TO crm_owner;
GRANT CONNECT ON DATABASE crm TO crm_app;
\connect crm
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

Bản dev (`docker/postgres/init/01-roles.sql`) và bản diễn tập (`init-drill/01-roles.sql`) làm đúng các bước này.

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
