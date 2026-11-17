# Phép thử ngắn đầu M0 (ADR-42) — kết quả trên máy này

Kế hoạch M0 bản 5, Task 2. Đo ngày 18/11/2026. Số 3 (SSE qua Cloudflare) cần máy thử, chờ Điểm dừng A. Số 5 ở `flash-sale-go.md` (Task 1).

**Máy đo:** Apple M5, 10 nhân, 16 GB RAM, macOS 26.6.2; Docker Desktop (10 CPU, 7,65 GiB cho máy ảo). PostgreSQL 18.7 (`postgres:18-bookworm`
ghim digest), WAL-G v3.0.9, SeaweedFS 4.48 làm kho S3 cục bộ. Image `crm-postgres:18-walg-drill`: 180 MB.
Kết quả chỉ để tham khảo: kho S3 cùng máy, không qua mạng thật.

## Tóm tắt

| Phép thử | Lệnh | Kết quả | Ngưỡng | Đạt |
|---|---|---|---|---|
| 1. Kế hoạch chạy câu tìm tên bằng `crm_app` | `scripts/backup/search-plan-probe.sh` | (a) thẳng: Seq Scan, 19,9–21,7 ms; (b) qua hàm: Bitmap Index Scan trên index trigram, 0,5–1,1 ms | (a) có Seq Scan, (b) dùng index | Có |
| 2. Khôi phục có bấm giờ | `scripts/backup/restore-drill.sh` | `rpo_seconds=58`, `restore_seconds=3` và `4` (hai lần), `encrypted=yes`, đủ 150/150 dòng | RPO ≤ 300 s; mã hóa | Có |
| 4. Tạo lại container PostgreSQL 18 | `scripts/backup/recreate-check.sh` | `RECREATE OK`, `PGDATA=/var/lib/postgresql/18/docker` nằm trên volume gắn ở `/var/lib/postgresql` | Dòng còn sau khi tạo lại | Có |

## 2. Khôi phục có bấm giờ

Dòng kết quả lần chạy cuối:

```
RESTORE DRILL OK rpo_seconds=58 restore_seconds=4 encrypted=yes nokey_rc=137 wrongkey_rc=137 rows=150
  db_bytes=8074943 cluster_bytes=31592860 bucket_kib=9944 objects=11 archive_failed=0
```

- **Dung lượng lúc đo:** database `crm` 7,7 MB; cả cụm 30,1 MB; kho sau sao lưu 9,7 MB (11 object: 1 bản đầy đủ, 5 tệp WAL đã nén và mã hóa).
- **`restore_seconds` = 3–4 s**, tính từ lúc bật container mới rỗng tới lúc `pg_is_in_recovery() = false` (đã phát lại hết WAL, nhận ghi),
  không chỉ tới lúc nhận kết nối: PostgreSQL nhận kết nối chỉ đọc khi còn đang phát lại. Số này chỉ đúng cho database rất nhỏ trên cùng máy;
  mục tiêu 1 giờ của ADR-37 phải đo lại với kho ngoài thật và dữ liệu cỡ thật (Task 23).
- **`rpo_seconds` = 58 s**: từ lúc ghi xong 50 dòng cuối tới lúc tệp WAL chứa chúng được đẩy lên kho (`pg_stat_archiver`). Khớp `archive_timeout = 60`.
- **Mã hóa:** tải mọi object của bucket về, tìm chuỗi `PLAINTEXT-MARKER-7f3a` (có trong 150 dòng): không thấy. Đối chứng: cùng cách tìm trên một bản sao lưu
  không mã hóa (tiền tố riêng, xóa ngay) thì thấy, nên phép tìm có giá trị dù dữ liệu được nén lz4.
- **Không có khóa, khóa sai:** không khôi phục được. Cùng lệnh với đúng khóa thì được (đối chứng).
- **Đường lỗi của `base-backup.sh`:** khóa S3 sai → thoát khác 0, ghi `backup_runs.ok = false` (bảng thay thế đúng hình `0010_ops.sql`); đúng khóa → `ok = true`.

### Điều phát hiện khi đo

1. **WAL-G không thoát khi giải mã hỏng.** Không có khóa: log `Extraction error in part_001.tar.lz4: … lz4: bad magic number`; khóa sai: `… corrupted chunk`;
   nhưng tiến trình **không thoát**, cứ chờ (lần đầu treo hơn 8 phút). Script diễn tập đặt `timeout 120` nên mã thoát là 137 (bị giết).
   Hệ quả cho Task 21: `weekly-verify.sh` và sổ tay khôi phục phải có giới hạn thời gian cho `backup-fetch`, nếu không một bản sao lưu hỏng làm việc kiểm treo mãi thay vì báo lỗi.
2. **Siêu dữ liệu không mã hóa.** `metadata.json`, `files_metadata.json`, `*_backup_stop_sentinel.json` là JSON đọc được: tên tệp trong thư mục dữ liệu, tên database,
   LSN, thời điểm. Không có dữ liệu dòng (đã kiểm bằng chuỗi đánh dấu). Chấp nhận được; ghi để không ai tưởng "mọi thứ trên kho đều mã hóa".
3. **Image bản 18 đổi đường dẫn dữ liệu** đúng như 04 mục 13 nghi: `PGDATA=/var/lib/postgresql/18/docker`. Volume phải gắn ở `/var/lib/postgresql`
   (gắn ở `/var/lib/postgresql/data` như các bản cũ thì dữ liệu nằm ngoài volume). `recreate-check.sh` kiểm cả điều này.
4. **`docker compose down -v` không xóa volume của service nằm trong profile** khi không nêu profile. Lần chạy trước bị dừng để lại volume khôi phục cũ,
   lần sau WAL-G từ chối (`Directory … must be empty`). Script dọn bằng `--profile restore --profile tools` và kiểm không còn volume nào.

## 1. Kế hoạch chạy câu tìm tên dưới RLS

Bảng `probe.names`: 200.000 dòng, hai tenant mỗi bên 100.000, `ENABLE` + `FORCE ROW LEVEL SECURITY`, policy `NULLIF(current_setting('app.tenant_id', true), '')::uuid`,
GIN `pg_trgm` trên `name_search`, `UNIQUE (tenant_id, id)`. Chạy bằng `crm_app` (không superuser, không `BYPASSRLS`; script kiểm), ngữ cảnh tenant A, tìm `'%tuyet 1234%'`.
Kế hoạch của câu bên trong hàm lấy bằng `auto_explain` (chỉ bật cho `crm_app` trong database thử `search_probe`).

(a) `LIKE` thẳng trên bảng:

```
 Limit (actual time=20.380..21.711 rows=1.00 loops=1)
   ->  Sort (actual time=20.380..21.710 rows=1.00 loops=1)
         ->  Gather (actual time=0.386..21.703 rows=1.00 loops=1)
               ->  Parallel Seq Scan on names (actual time=9.390..19.349 rows=0.50 loops=2)
                     Filter: ((tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid) AND (name_search ~~ '%tuyet 1234%'::text))
                     Rows Removed by Filter: 100000
 Execution Time: 21.742 ms
```

(b) qua `probe.search(q, lim)` (`SECURITY DEFINER`, chủ `crm_search`, tự lọc tenant):

```
Limit  (actual time=0.499..0.500 rows=1.00 loops=1)
  ->  Sort  (actual time=0.499..0.499 rows=1.00 loops=1)
        ->  Bitmap Heap Scan on names n  (actual time=0.494..0.495 rows=1.00 loops=1)
              Recheck Cond: (name_search ~~ '%tuyet 1234%'::text)
              Filter: (tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)
              ->  Bitmap Index Scan on names_name_search_trgm  (actual time=0.484..0.484 rows=2.00 loops=1)
duration: 0.501 ms
```

Kiểm thêm trong cùng phép thử: hàm và câu thẳng trả cùng số dòng (khác 0); `q = '%'` trả 0 dòng (ký tự đại diện bị thoát);
thiếu ngữ cảnh tenant thì hàm và bảng đều trả 0 dòng. Khớp số đã ghi ở 04 mục 9 (22 ms so với 0,9 ms).
Một lần chạy khác (auto_explain đo cả câu (a)) cho 40,3 ms và 1,1 ms; thứ tự chênh lệch không đổi.

## 4. Tạo lại container

```
RECREATE OK pgdata=/var/lib/postgresql/18/docker old=5558dbf36427 new=fe9ba5f2109d
```

Chèn một dòng có mã ngẫu nhiên, `docker compose rm -sf postgres` (xóa hẳn container), `up -d` với cùng volume, dòng còn; mã container khác nhau.

## Chưa đo được ở đây

- Kho ngoài thật (ADR-37 điều 4), thời gian tải qua mạng, khôi phục dữ liệu cỡ thật: Task 23, cần chủ shop tạo tài khoản kho.
- Phép thử số 3 (SSE qua Cloudflare): Điểm dừng A.
