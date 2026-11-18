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
  production). Compose dev hiện chưa đặt các biến này cho `fakecarrier`, `crm-ingest`, `crm-worker` (xem
  `docs/tasks/task-33.md`, "Việc còn lại").
- Sổ COD chưa có API (M3): đọc bằng SQL trong cùng kiểu transaction trên (sau `set_config('app.tenant_id', …)`) —
  `SELECT account, balance FROM cash.cod_balances_v WHERE shipment_id = '<id>'`. Sau `delivered`: `carrier_holding`
  bằng COD, `to_collect` 0; tổng mọi dòng `cash.cod_ledger_entries` bằng 0.
- Phần giả: hãng vận chuyển (`tools/fakecarrier`, chỉ ngoài production). Hãng thật, đối soát COD, hoàn hàng thuộc M3;
  màn hình đơn, kho thuộc M2 và Phần C của M1.
- Kết quả trên máy dev (18/11/2026, test e2e): mỗi bước sau commit dưới 0,3 s (tạo vận đơn → `awaiting_pickup` ~0,2 s,
  `picked_up` → `shipping` và sổ COD ~0,25 s, `delivered` → đơn `delivered` và sổ COD ~0,2 s). Staging: chưa chạy (cần
  chủ shop OK và service token).
