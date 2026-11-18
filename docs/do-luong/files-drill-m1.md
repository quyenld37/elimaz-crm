# Diễn tập kho tệp M1 (Task M1-46; TP08, TP09, TP12)

Đo ngày 19/11/2026 trên máy dev (macOS 26.6.2, 10 nhân, 16 GiB, Docker 29.2.1), bộ compose riêng `crm-t46`
(PostgreSQL 18, SeaweedFS 4.48 kho chính và kho ngoài giả, image `crm-filesync:dev` với rclone 1.74.4). Ảnh do imager
giả (`testkit.StubImager`) tạo biến thể; mọi bước khác là đường thật: API `httpapi` (createUpload → PUT thẳng lên kho
bằng đường dẫn ký sẵn → complete), worker `file.process`, `file.verify`, container filesync.

## TP08 — kho tệp tắt đột ngột giữa lúc ghi

Lệnh: `COMPOSE_PROJECT=crm-t46 scripts/files/crash-drill.sh` (test `TestFileStoreCrash`, nhãn build `drill`).

200 lượt tải lên song song (20 lượt cùng lúc, mỗi tệp 256 KiB); khi đủ 100 lượt complete trả 200 thì
`docker kill -s KILL` container SeaweedFS rồi `up -d --wait` lại.

| Lần chạy | complete 200 | lỗi giữa chừng | tải lại được | tệp đã báo xong đọc ra sai | kho dậy lại sau |
|---|---|---|---|---|---|
| 1 (19/11) | 119 | 81 | 81 / 81 | 0 | 14,9 s |
| 2 (19/11, sau review nội bộ) | 107 | 93 | 93 / 93 | 0 | 20,0 s |
| 3 (19/11, bản commit) | 119 | 81 | 81 / 81 | 0 | 13,0 s |

Kết quả: `FILES CRASH OK uploads=200 completed=119 failed_midway=81 retried_ok=81`. Mọi tệp đã báo xong còn nguyên
(sha256 khớp); lượt dở nhận lỗi (PUT bị từ chối kết nối, hoặc complete 500/409) và createUpload mới thì được.

**Điều đo được thêm:** ngay sau khi healthcheck của SeaweedFS qua (`curl /` của cổng S3), có vài giây kho trả **500
InternalError** khi đọc tệp nằm trên volume chưa đăng ký lại với master (`LookupFileId … volume 31 not found` trong
log). Lần chạy đầu 5 tệp đọc lỗi kiểu này; đọc lại vài giây sau thì đúng sha256. Dữ liệu không mất, nhưng trong
khoảng đó API trả 500 cho complete và ảnh không hiện. Bài diễn tập đọc lại tới 2 phút khi lỗi khác "không có".

## TP09, TP12 — kho tệp hỏng hẳn, dựng lại từ bản sao

Lệnh: `COMPOSE_PROJECT=crm-t46 scripts/files/restore-drill.sh` (test `TestFileStoreRestore`).

Các bước: 20 ảnh tải lên và xử lý xong → chép một lượt sang kho ngoài (`run.sh --once`) → 2 ảnh tải thêm → **bấm
giờ**: dừng SeaweedFS chính, xóa container và volume `crm-t46_seaweed`, khởi động rỗng, `bootstrap.sh` (dựng lại
bucket, chính sách, giữ phiên bản), `restore.sh` (rclone copy ngược, giải mã `crm-originals`, `crm-private`) → hết
giờ → `verifyFiles` và chờ lượt đối chiếu xong.

| Lần chạy | restore_seconds | đối chiếu (s) | tệp của bài | đối tượng trong kho lúc diễn tập | dung lượng | lost |
|---|---|---|---|---|---|---|
| 1 (19/11) | 14,5 | 0,2 | 22 | 316 | 1,5 MB | 2 |
| 2 (19/11) | 24,1 | 0,6 | 22 | 596 | 2,9 MB | 2 |
| 3 (19/11, script gắn từ mã, không dựng image) | 17,4 | 0,6 | 22 | 876 | 4,2 MB | 2 |
| 4 (19/11, sau review nội bộ) | 24,0 | 0,5 | 22 | 1.180 | 5,7 MB | 2 |
| 5 (19/11, bản commit; đo `du -sk /data` của SeaweedFS chính) | 17,4 | 0,3 | 22 | — | 12,8 MiB (13.112 KiB, gồm mọi phiên bản và siêu dữ liệu) | 2 |

Số đối tượng tăng giữa các lần vì kho ngoài chỉ thêm: lần dựng sau chép về cả tệp của các lần trước. Đúng 2 ảnh tải
sau lần chép cuối thành `lost` (TP09: mất phần tải lên sau lần chép tốt cuối; TP12: danh sách tệp mất); 20 ảnh còn
lại đọc được ảnh gốc và đủ 12 biến thể. Mỗi lần ghi một dòng `platform.file_sync_runs(kind = 'restore_drill',
bucket = 'all')` có `objects` (số tệp của bài), `bytes` (dung lượng thư mục dữ liệu SeaweedFS, `du -sk /data`),
`detail = restore_seconds=… verify_seconds=… lost=…`. Lần 1–4 đếm đối tượng bằng liệt kê S3; bản commit đo `du` để
không thêm client S3 quản trị vào mã test (luật ranh giới: chỉ `internal/platform` nhập aws-sdk).

**Ước lượng cho máy chủ:** thời gian dựng lại gần như toàn bộ là `rclone copy` từ kho ngoài; kho dev nhỏ (vài MB)
nên con số trên chủ yếu là khởi động container. Với kho thật (vài chục GB ảnh) thời gian tỉ lệ với băng thông tải về
từ Backblaze B2; đo lại trên staging khi có kho ngoài thật (việc treo của Task M1-45 Step 6).
