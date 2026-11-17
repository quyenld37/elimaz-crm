# Task 02 — Ba phép thử ngắn chạy được trên máy này

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`, Task 2. Loại việc (quy trình mục 3): A.
Spec: ADR-37 (sao lưu WAL-G: `archive_timeout` ≤ 5 phút, diễn tập có bấm giờ, mã hóa libsodium), ADR-42 (năm phép thử ngắn),
04 mục 9 "Tìm kiếm dưới RLS" và ADR-29 điều 5 (hàm `SECURITY DEFINER` do role `NOLOGIN BYPASSRLS` làm chủ), 04 mục 13 (image 18 đổi đường dẫn dữ liệu).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Task không có mã Go (repo chưa có `go.mod` ở nhánh này).
- Gói phụ thuộc ngược (go list): không có gói Go nào.
- Operation OpenAPI bị chạm, oasdiff: không có (chưa có `openapi.yaml`).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có migration, không sqlc. SQL chỉ trong cụm PostgreSQL diễn tập dùng một lần
  (bảng `drill`, database `search_probe` với schema `probe`, bảng thay thế `platform.backup_runs` đúng hình của `0010_ops.sql`
  trong kế hoạch). Không chạm database dev 5442 của Task 1.
- Tệp mới: `docker/postgres/Dockerfile`, `docker/postgres/conf.d/crm.conf`, `docker/postgres/initdb/000-crm-conf.sh`,
  `docker/postgres/init-drill/01-roles.sql`, `docker/compose.backup-drill.yml`, `scripts/backup/{drill-lib.sh,base-backup.sh,
  restore-drill.sh,recreate-check.sh,search-plan-probe.sql,search-plan-probe.sh}`, `docs/do-luong/phep-thu-ngan.md`.
- Sửa tệp có sẵn: `docs/tasks/hien-tai.md` (trạng thái task), `docs/phu-thuoc.md` (thêm dòng công cụ). Không sửa `docker/compose.dev.yml`, `docker/postgres/init/01-roles.sql`
  (của Task 1, nhánh khác).
- Docker: chỉ dự án compose `crm-t02` (container, volume, network mang tiền tố `crm-t02`). Image tự dựng: `crm-postgres:18-walg-drill`.
- Test sẽ chạy: ba phép thử của task (`recreate-check.sh`, `restore-drill.sh`, `search-plan-probe.sh`) và `bash -n`/`shellcheck` nếu có.
  Không có gói Go để test.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chưa có (sinh từ Task 3);
  `git ls-files` không có script sao lưu, compose hay Dockerfile nào. Task 1 (nhánh khác) làm `docker/compose.dev.yml` và
  `init/01-roles.sql`: không dùng lại được ở nhánh này nên đặt bản riêng ở `init-drill/` (xem Quyết định thay chủ shop).
- Dùng lại / viết mới vì: viết mới. Trong task, ba phép thử dùng chung một tệp `scripts/backup/drill-lib.sh` (sinh khóa lúc chạy,
  gọi compose với đúng tên dự án, chờ PostgreSQL sẵn sàng, dọn) để không lặp đoạn khởi động ba lần.
  Image `rclone/rclone` (đã có trên máy, ghim digest) dùng để tải object của bucket về, thay vì viết công cụ riêng.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
1. Chủ dữ liệu: không áp dụng — không có dữ liệu sản phẩm; bảng thử nằm trong cụm diễn tập, xóa sau khi chạy.
2. Không ghi bảng module khác: không áp dụng — không module nào.
3. Đọc qua view: không áp dụng.
4. Việc nguyên tử: không áp dụng.
5. Việc sau commit: không áp dụng.
6. Gọi ra ngoài: không gọi gì ngoài máy. Kho S3 là SeaweedFS cục bộ, chỉ thay cho kho ngoài thật (ADR-37 điều 4 chưa kiểm được ở task này, ghi ở bằng chứng).
7. Ngữ cảnh shop: phép thử tìm kiếm dựng bảng `probe.names` đúng luật bảng nghiệp vụ (`tenant_id`, `UNIQUE (tenant_id, id)`,
   `ENABLE` + `FORCE ROW LEVEL SECURITY`, policy `NULLIF(current_setting('app.tenant_id', true), '')::uuid`); chạy bằng `crm_app` (`NOBYPASSRLS`);
   hàm tìm tự lọc `tenant_id` theo ngữ cảnh; thiếu ngữ cảnh phải trả 0 dòng (có kiểm).
8. Nghiệp vụ không biết HTTP: không áp dụng.
9. Đường ghi có khóa chống trùng: không áp dụng — không có API.
10. PostgreSQL là nguồn sự thật: không áp dụng.
11. Công nghệ mới: WAL-G, libsodium (ADR-37), SeaweedFS (cổng 8333 trong kế hoạch, Global Constraints), pg_stat_statements, pg_trgm (ADR-08)
    đều đã có trong spec. Thêm image `rclone/rclone` chỉ để tải object trong diễn tập: là công cụ, không vào sản phẩm; ghi dòng vào `docs/phu-thuoc.md`.
12. Module mẫu: chưa có.

Mục 6 (đúng khi chạy đồng thời):
1–3. Không áp dụng: không có ghi tranh chấp; script chạy tuần tự.
4–5. Không áp dụng: không có đường yêu cầu.
6. Việc nền lỗi: `base-backup.sh` thoát khác 0 và ghi `backup_runs.ok = false` khi bảng có; có phép thử với khóa S3 sai.
7. Không áp dụng.
8. Tiến trình chết giữa chừng: chính là phép thử 4 (tạo lại container, dữ liệu còn) và 2 (máy mất, dựng máy mới từ bản sao lưu).
9. Treo lâu: mọi vòng chờ trong script có hạn (PostgreSQL sẵn sàng 120 s, WAL được lưu 300 s), quá hạn thì thoát khác 0.
10–11. Không áp dụng.
12. Dữ liệu cá nhân: bản sao lưu phải mã hóa (kiểm bằng tìm chuỗi đánh dấu trong mọi object, có mẫu đối chứng không mã hóa phải tìm thấy);
    khóa WAL-G, khóa S3, mật khẩu `postgres` sinh lúc chạy, chỉ sống trong biến môi trường của tiến trình và container, không ghi vào repo hay tệp;
    script không in giá trị khóa.
13. Quyền: `crm_app` không có `BYPASSRLS`, không superuser (có kiểm); hàm tìm `REVOKE EXECUTE FROM PUBLIC`, chỉ `GRANT` cho `crm_app`,
    `search_path` cố định để chặn chiếm hàm.

## Quyết định thay chủ shop

Chủ shop vắng mặt, đã giao quyền quyết trong phạm vi kế hoạch và spec. Bốn quyết định đầu do điều phối viên chốt, chép lại ở đây.

1. **Role cho diễn tập** (điều phối viên): Task 1 (nhánh khác) tạo `docker/postgres/init/01-roles.sql`. Task 2 không sửa tệp đó;
   đặt bản riêng `docker/postgres/init-drill/01-roles.sql` cùng nội dung role (`crm_owner`, `crm_app NOBYPASSRLS`, `crm_search NOLOGIN BYPASSRLS`,
   `GRANT crm_search TO crm_owner`), không mật khẩu (chỉ đăng nhập qua socket trong container). Lúc gộp nhánh: giữ một bản, cho compose diễn tập gắn tệp của Task 1.
2. **Cổng** (điều phối viên): PostgreSQL diễn tập `127.0.0.1:${CRM_DRILL_PG_PORT:-5452}:5432`; không dùng 5442, 16686, 4318. Tên dự án compose `crm-t02`.
   Thêm `127.0.0.1` để cổng không mở ra mạng LAN (chặt hơn, không đổi gì khi dùng trên máy này).
3. **Khóa** (điều phối viên): khóa WAL-G, khóa S3 sinh lúc chạy (`openssl rand`), không ghi vào repo.
4. **Phép thử số 3** (SSE qua Cloudflare): không thuộc task này, không vào máy thử.
5. **Cổng 8333 của SeaweedFS không mở ra máy** (tự quyết): cổng 8333 trên máy này đang bị dự án khác (`trivietbook-s3-1`) giữ.
   SeaweedFS vẫn nghe 8333 trong mạng riêng của compose (WAL-G gọi `http://seaweedfs:8333`), không publish ra máy. Phương án khác: publish cổng khác
   (ví dụ 8334) — không cần vì không công cụ nào trên máy gọi S3 trực tiếp. Đảo ngược: thêm một dòng `ports`.
6. **WAL-G v3.0.9, bản dựng `pg-22.04`** (tự quyết): bản ổn định mới nhất (20/08/2026). Image `postgres:18-bookworm` là Debian 12 (glibc 2.36);
   bản dựng cho Ubuntu 24.04 cần glibc 2.39 nên không chạy được, bản 22.04 (glibc 2.35) chạy được. Ghim sha256 cho cả `aarch64` và `amd64`.
7. **Cách bật `crm.conf`** (tự quyết): image chép `crm.conf` vào `/etc/postgresql/conf.d/` và một script initdb nối `include_dir` vào
   `postgresql.conf` khi khởi tạo cụm. Phương án khác: truyền từng tham số bằng `-c` trong compose — bỏ vì kế hoạch đòi một tệp cấu hình.
8. **Kế hoạch chạy qua hàm** (tự quyết): `EXPLAIN` một hàm `SECURITY DEFINER` chỉ thấy "Function Scan". Để thấy kế hoạch thật bên trong,
   bật `auto_explain` (có sẵn trong PostgreSQL) cho riêng `crm_app` trong riêng database thử `search_probe`, mức `NOTICE` để psql in ra.
   Phương án khác: viết một hàm thứ hai chạy `EXPLAIN` câu bên trong — bỏ vì chép câu SQL hai nơi, dễ lệch.
9. **Bấm giờ khôi phục tới lúc nhận ghi**, không chỉ tới lúc nhận kết nối (tự quyết): khi có `recovery.signal`, PostgreSQL nhận kết nối chỉ đọc
   trước khi phát lại xong WAL; đếm lúc đó có thể thiếu dòng. Đồng hồ dừng khi `pg_is_in_recovery() = false`.
10. **RPO đo thật** (tự quyết): `rpo_seconds` = thời điểm WAL chứa 50 dòng cuối đã được lưu lên kho (`pg_stat_archiver`) trừ thời điểm ghi xong;
    vẫn chờ đủ 70 s như kế hoạch.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi có image và compose | `recreate-check.sh` | `scripts/backup/recreate-check.sh` (18/11 01:03) | `open …/docker/compose.backup-drill.yml: no such file or directory`, `LỖI: dựng image PostgreSQL thất bại`, exit 1 |
| Step 1: dựng image (WAL-G ghim sha256), `crm.conf` được nạp, `wal-g backup-list` chạy không lỗi | `restore-drill.sh` đoạn đầu | `scripts/backup/restore-drill.sh` | `sha256sum -c`: `OK`; `wal-g version v3.0.9`; `archive_mode/archive_timeout/shared_preload_libraries` = `on 1min pg_stat_statements`; `backup-list`: `No backups found`, exit 0 |
| Step 2: tạo lại container, dữ liệu còn; PGDATA của image 18 nằm trên volume | `recreate-check.sh` | `scripts/backup/recreate-check.sh` (lượt cuối, 23 s) | `RECREATE OK pgdata=/var/lib/postgresql/18/docker old=5558dbf36427 new=fe9ba5f2109d`, exit 0 |
| Step 3: khôi phục vào container mới rỗng, đủ 150 dòng, RPO ≤ 300 s, có bấm giờ | `restore-drill.sh` bước 3, 5, 6 | `scripts/backup/restore-drill.sh` (lượt cuối, 346 s) | `RESTORE DRILL OK rpo_seconds=58 restore_seconds=4 … rows=150`, exit 0 |
| Step 3: bản sao lưu mã hóa (không object nào chứa chuỗi đánh dấu; đối chứng không mã hóa thì thấy) | `restore-drill.sh` bước 4 | như trên | `encrypted=yes objects=11`; bước đối chứng không báo lỗi (grep thấy chuỗi trong bản không mã hóa) |
| Step 3: không khóa, khóa sai thì không khôi phục được; đúng khóa thì được | `restore-drill.sh` bước 7 | như trên | `nokey_rc=137 wrongkey_rc=137` (WAL-G báo `lz4: bad magic number` / `corrupted chunk` rồi treo, bị giết sau 120 s); đúng khóa: `FETCH_USABLE` |
| `base-backup.sh` thoát khác 0 khi lỗi và ghi `backup_runs` ok = false / true | `restore-drill.sh` bước 2 | như trên | Khóa S3 sai: exit ≠ 0, 1 dòng `ok = false`; đúng khóa: 1 dòng `ok = true` (script dừng nếu sai) |
| Step 4: (a) LIKE thẳng bằng `crm_app` có Seq Scan; (b) qua hàm dùng index; kết quả đúng, thiếu ngữ cảnh 0 dòng | `search-plan-probe.sh` + `.sql` | `scripts/backup/search-plan-probe.sh` (lượt cuối, 19 s) | `SEARCH PLAN OK direct=seq_scan direct_ms=21.742 function=trgm_index function_ms=0.501`; `KIEM KET QUA OK rows=1 no_context_rows=0` |
| Dọn đúng phạm vi: không còn container, volume của `crm-t02` | sau lượt cuối | `docker ps -a --filter name=crm-t02 -q \| wc -l`; `docker volume ls -q \| grep -c crm-t02` | `0`, `0` |
| Cú pháp shell | mọi `scripts/backup/*.sh` | `bash -n` | không lỗi (`shellcheck` chưa cài trên máy) |

Số đo và kế hoạch chạy đầy đủ: `docs/do-luong/phep-thu-ngan.md`.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `scripts/backup/drill-lib.sh` (dùng chung cho ba phép thử) và `scripts/backup/search-plan-probe.sh` (chạy tệp `.sql` và kiểm kế hoạch bằng máy,
  vì kế hoạch chỉ ghi "in kế hoạch"). Thêm `docker/postgres/initdb/000-crm-conf.sh` (nạp `crm.conf`, quyết định 7) và `docker/postgres/init-drill/01-roles.sql` (quyết định 1).
- `restore-drill.sh` kiểm nhiều hơn kế hoạch: đối chứng không mã hóa cho phép tìm chuỗi; khóa sai ngoài không khóa; đối chứng đúng khóa; đường lỗi của `base-backup.sh`.
- `base-backup.sh` ghi `backup_runs` không kèm `duration_seconds` (Task 21 thêm, đúng kế hoạch); không ghi nội dung lỗi của WAL-G vào `detail`, chỉ ghi bước hỏng.
- `docs/phu-thuoc.md`: thêm rclone (công cụ diễn tập), WAL-G, SeaweedFS (phiên bản, giấy phép).

## Việc phát hiện thêm

- **Task 21:** `wal-g backup-fetch` không thoát khi giải mã hỏng (không khóa, khóa sai), chỉ ghi log rồi treo. `weekly-verify.sh` phải đặt giới hạn thời gian
  (ví dụ `timeout`) và coi quá hạn là `ok = false`, nếu không bản sao lưu hỏng làm việc kiểm treo thay vì báo lỗi.
- **Gộp với Task 1:** image này tự chép `000-crm-conf.sh` vào `/docker-entrypoint-initdb.d/`. Nếu `compose.dev.yml` của Task 1 chuyển sang image này và gắn
  cả thư mục `init/` vào `/docker-entrypoint-initdb.d`, script đó bị che và `crm.conf` không được nạp: gắn từng tệp (như compose diễn tập) hoặc đổi đường dẫn.
  Hai tệp role (`init/01-roles.sql`, `init-drill/01-roles.sql`) nên còn một.
- **Siêu dữ liệu sao lưu không mã hóa** (tên tệp, tên database, LSN): ghi vào threat model ở Task 23.
- `shellcheck` chưa có trong `tools/versions.mk`; script shell của sao lưu sẽ nhiều thêm ở Task 19, 21 — nên ghim và cho vào `make ci`.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, 18/11/2026, trên đúng phần đã stage). Phát hiện phải sửa: không có.

Ghi chú không bắt buộc của reviewer, không sửa trong task này (không ảnh hưởng kết quả đã đo), chuyển thành việc sau:
- `restore-drill.sh` bước 4: `grep` gặp lỗi đọc tệp (mã 2) rơi vào nhánh "không thấy". Lượt đo thật không có lỗi đọc (11 object tải đủ); nên tách mã 1 và 2 khi Task 21 sửa các script sao lưu.
- So tên tệp WAL theo chuỗi có thể trúng tệp `.backup`/`.history`; trong luồng hiện tại không cho kết quả đạt sai.
- `base-backup.sh` thiếu `WALG_LIBSODIUM_KEY` thì thoát khác 0 nhưng không ghi `ok = false`: Task 19/21 nên ghi cả trường hợp này để có cảnh báo.
