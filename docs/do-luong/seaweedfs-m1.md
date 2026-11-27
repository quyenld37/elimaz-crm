# Phép thử SeaweedFS cho M1 (Task M1-40)

Đo ngày 18/11/2026 trên máy dev (Apple Silicon, Docker Desktop 29.2.1, 7,65 GiB cho Docker), SeaweedFS
`chrislusf/seaweedfs:4.48@sha256:4e61d15f…` (`weed server` một container: master, volume, filer, S3), aws-sdk-go-v2
`service/s3` v1.114.10. Lệnh: `make dev` rồi `make probe-test` (test `tests/probe/seaweedfs_test.go`, tag `probe`).
Phiên này chạy với `CRM_S3_PORT=8440`, `CRM_S3_OFFSITE_PORT=8441` vì cổng 8333 của máy đang bị dự án khác giữ.

## Kết quả từng mục của kế hoạch

| # | Mục | Đạt | Số đo, cách làm | Test |
|---|---|---|---|---|
| 1 | `PutObject` với kiểm tổng mặc định của aws-sdk-go-v2 | đạt | Mặc định (`WhenSupported`, CRC32) ghi được, đọc lại đúng byte. **Không cần** đặt `RequestChecksumCalculation = WhenRequired` (ADR-44 "thử ở M1": đã thử) | `TestPutObject_DefaultChecksum` |
| 2 | Ký sẵn `PUT` có ký `content-length = 1000` | đạt | 1.000 byte → 200; 1.001 byte → 403 `SignatureDoesNotMatch`, đối tượng không được ghi. `X-Amz-SignedHeaders=content-length;host` | `TestPresignedPut_ContentLengthSigned` |
| 3 | Ký sẵn `GET` hạn 2 s | đạt | Đọc ngay → 200; sau 3 s → 403 | `TestPresignedGet_Expires` |
| 4 | `crm-app` xóa không `VersionId` trên bucket giữ phiên bản | đạt | Tạo dấu xóa (`DeleteMarker = true`); đọc bản mới nhất → lỗi; `crm-admin` đọc bản cũ theo `VersionId` ra đúng nội dung. Cả ba bucket `crm-public`, `crm-originals`, `crm-private` | `TestDelete_AppCannotPurge` |
| 5 | `crm-app` xóa có `VersionId` → AccessDenied | đạt, **nhờ chính sách bucket** | Xem mục "Quyền xóa" dưới. `DeleteObject` có `VersionId` (cả bản thật và dấu xóa) → `AccessDenied`; `DeleteObjects` có `VersionId` → lỗi `AccessDenied` cho từng đối tượng; bản cũ còn nguyên | `TestDelete_AppCannotPurge` |
| 6 | Quy tắc hết hạn của `crm-tmp` | đạt (Task 410, đo 19/11) | Quy tắc S3 `PutBucketLifecycleConfiguration` (`Expiration Days=1`, cộng `AbortIncompleteMultipartUpload` 1 ngày) đọc lại đúng, **nhưng một mình nó không xóa gì**: `weed server` không chạy lifecycle worker (xem mục "Hạn 24 giờ của crm-tmp" dưới). Cách xóa thật: lifecycle TTL fast path của `crm-tmp` (`bootstrap.sh` bật) — đối tượng có `ttlSec 86400`, dữ liệu ở volume `Ttl:1d`, filer và volume server tự xóa | `TestTmpExpiration`, `TestTmpExpiry_*` |
| 7 | Tải nhiều phần 3 × 5 MiB | đạt | Ghép được, đọc lại đúng 15 MiB | `TestMultipartUpload` |
| 8 | CORS `OPTIONS` từ `http://localhost:4192` | đạt | `Access-Control-Allow-Origin: http://localhost:4192`, `Access-Control-Allow-Methods: PUT`; gốc lạ `http://evil.example` → không có header cho phép; ba bucket còn lại, kể cả từ gốc của màn hình → không có header cho phép (xem mục cổng, mạng: cờ `-s3.allowedOrigins`) | `TestTmpCORS` |
| 9 | `PutObject` với `If-None-Match: *` lên khóa đã có | đạt (có hỗ trợ) | Lần hai → `PreconditionFailed`, nội dung lần một còn nguyên. `FileStore` (Task M1-41) dùng được `If-None-Match: *` thay cho `HeadObject` trước | `TestPutIfNoneMatch` |
| 10 | Bộ nhớ SeaweedFS (ADR-36 "chưa xác minh") | đo | Xem bảng dưới | `TestMemory` |
| 11 | `docker compose config`, `up` không profile | đạt | `config --quiet` hợp lệ có và không profile; không profile chỉ có `postgres`, `jaeger`, `seaweedfs`, `seaweedfs-offsite`, `s3-bootstrap`; `make dev` (từ volume rỗng) xong trong khoảng 7 s khi chưa có `docker/imager`, `tools/fakecarrier`, `docker/filesync` | `TestComposeConfig`; `make dev` |

Ca thêm (không có trong danh sách kế hoạch, cùng mục đích ADR-36 "ứng dụng không xóa vĩnh viễn được"):

| Ca | Kết quả | Test |
|---|---|---|
| `crm-app` tắt giữ phiên bản, đặt hoặc xóa quy tắc hết hạn, ghi hoặc xóa chính sách bucket, ghi hoặc xóa CORS, xóa bucket, tạo bucket mới | mọi ca bị từ chối, trên cả bốn bucket | `TestAppCannotLoosenBuckets` |
| `crm-filesync` ở kho chính: liệt kê, đọc ba bucket được; ghi, xóa → AccessDenied; `crm-tmp` → không đọc được. Ở kho ngoài giả: ghi được; xóa thường và xóa có `VersionId` → AccessDenied | đạt | `TestFilesyncIdentity` |

## Hạn 24 giờ của crm-tmp (Task 410, review độc lập M1 C-08; R10, Q18)

Đo ngày 19/11/2026 trên bộ compose riêng `crm-t410` (cùng bản SeaweedFS ghim, Docker Desktop, Apple Silicon).

| Phép đo | Kết quả |
|---|---|
| Đối tượng PUT vào `crm-tmp` khi chỉ có quy tắc `Expiration Days=1` (như Task 40) | `fs.meta.cat` → `ttlSec: 0`; volume của collection `crm-tmp` có `Ttl:` rỗng. Không có gì đánh dấu hạn trên đối tượng |
| Lifecycle worker có chạy trong `weed server` không | Không: `weed server -h` không có cờ worker/admin; log của container không có dòng lifecycle nào ngoài lệnh tay. Quy tắc `Expiration` chỉ được thực thi bởi `weed worker` (nối `weed admin`) hoặc lệnh tay `s3.lifecycle.run-shard` ("manually run one daily-replay pass") |
| Bucket thử có quy tắc `Expiration Date=2020-01-01` (đối tượng hết hạn ngay) | Sau 16 phút đối tượng vẫn đọc được (200). Chạy tay `s3.lifecycle.run-shard -shards 0-15 -s3 localhost:18333` → 404. Tức là cơ chế đúng nhưng không ai chạy: **trước Task 410 ảnh thô ở `crm-tmp` không bao giờ bị xóa** |
| Bật `s3.bucket.lifecycle.fastpath -name crm-tmp -enable` rồi PUT ký sẵn bằng `crm-app` | `ttlSec: 86400`, chunk ở volume collection `crm-tmp` `Ttl:1d` |
| CopyObject `crm-tmp` → `crm-originals` (đường `CopyIfAbsent` của ảnh gốc) | Bản chép có chunk mới ở volume không hạn, `ttlSec: 0`: ảnh gốc giữ lâu không mang hạn của `crm-tmp` |
| Ba bucket giữ phiên bản sau khi ghi | Không volume nào của chúng có `Ttl` (fast path không áp cho bucket giữ phiên bản) |
| Cơ chế hạn của filer bản ghim chạy thật | Mục ghi qua filer với `?ttl=1m` (`ttlSec: 60`) trả 404 sau khoảng 61–69 s, không ai gọi xóa (đo hai lần) |

Không chờ được 24 giờ thật trong test: bằng chứng là hai nửa (đối tượng của `crm-tmp` mang `ttlSec 86400` + volume
`Ttl:1d`; cơ chế `ttlSec` của filer xóa thật ở hạn ngắn). `make probe-test` chạy lại cả hai (`TestTmpExpiry_*`).
Chọn fast path thay cho thêm `weed worker` + `weed admin` hay một container chạy `s3.lifecycle.run-shard` theo lịch: không
thêm tiến trình, không thêm cổng quản trị (task-410.md, "Quyết định thay chủ shop" Q2). Cái giá: hạn gắn lúc ghi, đổi quy
tắc sau này không áp lại cho đối tượng đã có; đối tượng ghi trước khi bật fast path không có hạn (RUNBOOK "Hạn 24 giờ
của crm-tmp"). Tải nhiều phần chưa xong (`AbortIncompleteMultipartUpload`) vẫn cần lifecycle worker; ứng dụng không
dùng tải nhiều phần vào `crm-tmp` (đường dẫn ký sẵn một `PUT`, `files.CreateUpload`).

Ràng buộc với Task 409: trần thử lại xử lý ảnh (`IMAGE_RETRY_MAX_HOURS`) phải nhỏ hơn 24 giờ — Task 426: mặc định 23, khai ≥ 24 thì worker từ chối khởi động; ảnh riêng
tư chỉ có bản gốc ở `crm-tmp` (ảnh thường cũng vậy tới khi chép sang `crm-originals`), thử lại sau hạn thì lượt đó ra
`source_missing` (RUNBOOK "Hạn 24 giờ của crm-tmp").

## Hạn theo tiền tố của crm-exports (Task 515; kế hoạch M6 G5)

Đo ngày 27/11/2026 trên bộ compose riêng `crm-t515` (SeaweedFS 4.48 ghim, Docker Desktop, Apple Silicon). Bucket không
giữ phiên bản, hai luật `Expiration` lọc theo tiền tố (`reports/` 7 ngày, `customers/` 1 ngày), bật fast path.

| Phép đo | Kết quả |
|---|---|
| PutObject `reports/…` bằng `crm-app` | `ttlSec: 604800`, chunk ở volume collection `crm-exports` `Ttl:1w` |
| PutObject `customers/…` | `ttlSec: 86400`, volume `Ttl:1d` |
| PutObject khóa ngoài hai tiền tố (đo tay, ứng dụng không ghi khóa như vậy) | `ttlSec: 0` |
| `GetBucketVersioning` | trạng thái rỗng (chưa từng bật); PutObject không trả `VersionId` |
| `crm-app` DeleteObject, bật/tắt phiên bản, đổi luật, chính sách, CORS, xóa bucket | AccessDenied (chính sách bucket) |
| `crm-filesync` liệt kê `crm-exports` ở kho chính | bị từ chối; kho ngoài giả không có bucket này |
| Chạy lại `bootstrap.sh main` | `tạo crm-exports: 409`, các bước sau 200/204, không lỗi |

Kết luận: fast path xét luật theo tiền tố của từng khóa lúc ghi, nên một bucket có hai hạn khác nhau mà không cần lifecycle
worker. Test: `tests/probe/exports_test.go` (`TestExports_*`), `TestAppCannotLoosenBuckets/crm-exports`.

## Quyền xóa: điều ADR-36 phải biết

Quyền `Write` trong `s3.json` của SeaweedFS 4.48 **gồm cả** xóa vĩnh viễn một phiên bản, tắt giữ phiên bản
(`PutBucketVersioning Suspended`) và đặt quy tắc hết hạn cho bản cũ. Đo bằng tay trước khi viết cấu hình: danh tính chỉ có
`Read`, `Write`, `List` xóa được `DeleteObject?versionId=…` (204), tắt được giữ phiên bản (200), đặt được
`NoncurrentVersionExpiration` (200). Không có dạng hành động nào trong `s3.json` tách riêng các việc này.

Cách chặn đã đo được: **chính sách bucket** (`PutBucketPolicy`, do `crm-admin` đặt trong `bootstrap.sh`) từ chối các
việc `s3:DeleteObjectVersion`, `s3:PutBucketVersioning`, `s3:PutLifecycleConfiguration`, `s3:PutBucketPolicy`,
`s3:DeleteBucketPolicy`, `s3:PutBucketCors`, `s3:DeleteBucketCors`, `s3:DeleteBucket` với danh tính ứng dụng. Hai điều dễ sai:

- Principal phải ghi `arn:aws:iam::000000000000:user/<tên danh tính>`. Đã thử: `"crm-app"`, khóa truy cập,
  `arn:aws:iam:::user/crm-app`, `arn:seaweed:iam:::user/crm-app` đều được nhận (204) nhưng **không có tác dụng**;
  `"*"` có tác dụng nhưng chặn cả `crm-admin`.
- Tên việc phân biệt hoa thường: `s3:PutBucketCORS` (cách viết của AWS) được nhận nhưng không có tác dụng;
  `s3:PutBucketCors` có tác dụng. Mỗi tên trong danh sách có ca kiểm trong `TestAppCannotLoosenBuckets`, nên nâng
  SeaweedFS mà đổi cách đọc chính sách thì test đỏ.

Vì chính sách bucket nhận im lặng một tên sai, **mọi lần nâng SeaweedFS phải chạy lại `make probe-test`** (luật ghim
phiên bản của ADR-36). Không phải Điểm dừng M1-B: mục 4 và 5 đạt với bản ghim.

## Bộ nhớ (mục 10)

`docker stats` (cột MemUsage của container `weed server` gộp master, volume, filer, S3). Ghi 1.000 tệp 200 KB vào
`crm-originals` (giữ phiên bản), 16 luồng song song, từ máy qua cổng mở.

| Lúc | Bộ nhớ |
|---|---|
| Rảnh, kho rỗng, 20 s sau khi dựng | 67 MiB (kho ngoài giả: 76 MiB) |
| Rảnh sau khởi động lại, có khoảng 15 MiB dữ liệu | 131 MiB |
| Đầu `TestMemory` sau các test khác | 165 MiB |
| Cao nhất trong lúc ghi 1.000 × 200 KB (mẫu mỗi 0,5 s) | 326–450 MiB qua ba lần đo |
| 20 s sau khi ghi xong | 353 MiB (không tụt ngay) |
| Thời gian ghi 1.000 tệp | 1,8–5,8 s qua ba lần đo |

Đọc: một container SeaweedFS cần cỡ **0,5 GiB** khi có ghi dồn ở cỡ này; lúc rảnh dưới 0,2 GiB. Số trên Docker Desktop
macOS, chưa đo trên máy chủ Linux thật; con số có thể gồm bộ đệm tệp của cgroup. Đo lại ở staging (Task 56, M1-55) và khi số
ảnh vượt vài triệu (ADR-36 "Xem lại khi").

## Cổng, mạng và các điều dev cần biết

- `weed server` nằm trên hai mạng (`default`, `imaging`) thì chỉ nghe trên một IP nó tự chọn, cổng mở ra máy trả
  "Empty reply". Phải chạy với `-ip.bind=0.0.0.0` (đã đặt trong `compose.dev.yml`).
- Mạng Docker `internal: true` chặn container ra internet (đo: `curl example.com` không tới) nhưng **cũng bỏ cổng mở ra
  máy** (đo: 127.0.0.1 không tới). Vì worker dev chạy trên máy cần gọi `imager` ở 4193, mạng `imaging` ở dev không
  `internal`.
- `s3-bootstrap` chạy xong thì ngồi chờ (healthcheck thấy `/tmp/xong`): container đã thoát làm `docker compose up
  --wait` báo lỗi. Chạy lại: `docker compose restart s3-bootstrap`. Chạy hai lần liền không lỗi.
- `-s3.allowedOrigins` là CORS **chung** cho mọi bucket không có CORS riêng, mặc định `"*"`. Đo: khi cờ này bằng
  `CONSOLE_ORIGINS`, `OPTIONS` vào `crm-public` từ `http://localhost:4192` được `Allow-Methods: GET, PUT, POST, DELETE,
  HEAD`. Nên cờ được đặt thành một gốc không tồn tại (`https://khong-cho-phep.invalid`); chỉ `crm-tmp` có CORS riêng
  (từ `CONSOLE_ORIGINS`, đặt trong `bootstrap.sh`). Production phải giữ cùng cách này.
