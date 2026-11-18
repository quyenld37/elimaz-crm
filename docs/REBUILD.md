# Dựng lại máy chủ CRM từ đầu

Viết ở Task 23 (M0-23, 18/11/2026). Dùng khi máy chủ mất hẳn (hỏng đĩa, bị chiếm, đổi nhà cung cấp). Chưa diễn tập
trên máy thật: lần đầu làm ở bước staging (Task 23 bước 7, cần chủ shop OK); sau đó diễn tập mỗi quý và ghi thời gian ở
cuối tệp. Nền tảng triển khai (compose thuần hay Coolify) chốt ở staging theo ADR-43; các bước dưới đây dùng compose
thuần (`docker/compose.prod.yml`), cách đã có test (`tests/deploy`).

## Cần có trước khi bắt đầu

- Mã nguồn: repo GitHub riêng tư `elimaz-crm` (bước 7.1). Commit đang chạy: nhãn image `crm:<mã commit>` của CI; bản
  xuất (`images.txt`) có tên image theo tệp biến (`CRM_IMAGE`, ghim `@sha256`) và mã image cục bộ (không kéo được bằng mã này).
- Image: registry của CI giữ image theo digest; digest kéo được là giá trị `CRM_IMAGE` (ghim `@sha256`) trong `images.txt`. Mất registry thì dựng lại
  từ đúng commit trên CI (`docker build -t crm:<commit> .`), không dựng trên máy chạy thật.
- Bản xuất cấu hình mới nhất: kho ngoài, tiền tố `config/` (`scripts/ops/export-config.sh`, giữ 8 bản): tệp compose,
  cấu hình cloudflared, digest image, **tên** mọi biến.
- Bản sao lưu database: kho ngoài (WAL-G, mã hóa libsodium).
- Các khóa (bảng dưới): chủ shop nhập giá trị, không dán vào chat, không vào Git.

## Thứ tự

1. **Máy mới:** Linux có Docker và plugin compose; đồng hồ chạy NTP; tường lửa chặn mọi cổng vào (máy chỉ nhận kết nối
   qua Cloudflare Tunnel, kết nối ra ngoài). Nếu nền tảng là Coolify thì cài Coolify, nhưng vẫn không mở cổng ứng dụng ra.
2. **Lấy bản xuất cấu hình:** `aws s3 cp <kho>/config/<bản mới nhất> .` (kèm `--endpoint-url` của kho), giải nén vào
   `/srv/crm/`. Đọc `env-names.txt`: đó là danh sách biến phải có trong `/srv/crm/crm.env`.
3. **Chủ shop nhập giá trị các khóa** vào `/srv/crm/crm.env` (quyền 600, chủ là người chạy docker) theo bảng dưới.
   Khóa nào đã thu hồi hay nghi lộ thì tạo khóa mới ở nơi cấp (RUNBOOK "Khi lộ secret").
4. **PostgreSQL:** `docker compose -f compose.prod.yml --env-file crm.env up -d --wait postgres`, rồi khôi phục bản sao lưu
   mới nhất vào thư mục dữ liệu (cùng các bước của `scripts/backup/restore-drill.sh`: `wal-g backup-fetch LATEST`, đặt
   `restore_command`, khởi động tới hết WAL). Database rỗng (dựng mới hẳn) thì làm mục "Dựng PostgreSQL lần đầu" của
   RUNBOOK thay bước này. Sau khôi phục: `SELECT count(*) FROM platform.tenants` khác 0, `identity.users` đủ người.
5. **Migrate và ba vai trò:** `run --rm crm-migrate` (chạy cả migration mới hơn bản sao lưu nếu image mới hơn), rồi
   `up -d --wait crm-worker`, `crm-api`, `crm-ingest`, `cloudflared`, `jaeger`, `seaweedfs`, `imager`.
6. **Kho tệp:** chép lại từ kho ngoài vào SeaweedFS (bản sao mỗi giờ, ADR-36; cách chép ở `docs/do-luong/seaweedfs-m1.md`).
7. **Trỏ tên miền:** Cloudflare Tunnel mới (`CLOUDFLARE_TUNNEL_TOKEN` mới) trỏ `crm`, `hooks` tới tunnel; Access của
   `crm` giữ nguyên; `hooks` không qua Access, chỉ `/hooks/*` và `/health`; luật giới hạn tần suất của `hooks` còn.
8. **Kiểm:** `/health/deep` trả 200 (với `HEALTH_TOKEN`); kiểm từ ngoài hai nơi báo xanh; Telegram nhận cảnh báo thử;
   một webhook selftest đi trọn (`SELFTEST_WEBHOOK_SECRET`); `base-backup.sh` chạy được ngay (bản sao lưu đầu của máy
   mới); lịch sao lưu, kiểm hằng tuần, `export-config.sh` cài lại vào cron.
9. Ghi thời gian từng bước vào bảng "Lần dựng lại" dưới đây.

## Các khóa phải có và ai giữ

Danh sách tên đầy đủ ở `docker/compose.prod.env.example` và `env-names.txt` của bản xuất. "Ai giữ": nơi cất bản gốc
ngoài máy chủ. Mọi ô "chủ shop" nghĩa là chủ shop tự cất ở hai nơi (ví dụ trình quản lý mật khẩu và giấy niêm phong).

| Biến | Việc | Tạo ở đâu | Ai giữ |
|---|---|---|---|
| `POSTGRES_PASSWORD`, mật khẩu `crm_owner`, `crm_app` (trong `CRM_*_DATABASE_URL`) | Database | Sinh mới lúc dựng (`openssl rand -hex 32`) | Chỉ tệp biến của máy; dựng lại thì sinh mới, đổi mật khẩu role sau khôi phục |
| `WALG_LIBSODIUM_KEY` | Giải mã bản sao lưu | Chủ shop tạo một lần (bước 7.2) | **Chủ shop, hai nơi** — mất khóa này là mất mọi bản sao lưu |
| `WALG_AWS_ACCESS_KEY_ID`, `WALG_AWS_SECRET_ACCESS_KEY`, `WALG_S3_PREFIX`, `WALG_AWS_ENDPOINT`, `WALG_AWS_REGION` | Kho ngoài | Tài khoản kho ngoài của chủ shop | Chủ shop |
| `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `CRM_SEAWEEDFS_S3_CONFIG` | Kho tệp trên máy | Sinh mới lúc dựng | Tệp biến của máy |
| `CLOUDFLARE_TUNNEL_TOKEN` | Đường hầm vào máy | Cloudflare Zero Trust | Chủ shop (tài khoản Cloudflare) |
| `ACCESS_TEAM_DOMAIN`, `ACCESS_AUD`, `ACCESS_MFA_CLAIM`, `ACCESS_MFA_VALUES` | Kiểm thẻ đăng nhập | Ứng dụng Access của `crm` | Không bí mật; ghi trong bản xuất |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `TELEGRAM_URGENT_CHAT_ID` | Cảnh báo | BotFather | Chủ shop |
| `HEARTBEAT_URL` | Nhịp sống gửi ra ngoài | Dịch vụ nhận nhịp sống chủ shop chọn | Chủ shop |
| `HEALTH_TOKEN` | Bảo vệ `/health/deep` | Sinh mới | Tệp biến của máy và cấu hình bộ kiểm từ ngoài |
| `SELFTEST_WEBHOOK_SECRET` | Webhook tự kiểm | Sinh mới | Tệp biến của máy và bộ kiểm từ ngoài |
| Khóa webhook, token của Meta, hãng vận chuyển | Kênh bán, vận chuyển (M3, M4) | Trang nhà phát triển của từng hãng | Chủ shop |
| `CRM_IMAGE`, `CRM_IMAGER_IMAGE`, `CRM_POSTGRES_IMAGE` | Image đang chạy | CI | Không bí mật; `images.txt` của bản xuất |

## Lần dựng lại

| Ngày | Ở đâu | Từ bản sao lưu lúc | Thời gian từng bước | Tổng | Ghi chú |
|---|---|---|---|---|---|
| (chưa làm) | | | | | |
