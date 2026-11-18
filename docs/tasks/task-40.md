# Task 40 — Phép thử SeaweedFS và cấu hình dev

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 40, M1-40). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm gói test mới `tests/probe` (một tệp
  `seaweedfs_test.go`, build tag `probe`), không xuất ký hiệu nào, không ai nhập.
- Gói phụ thuộc ngược (go list): không có (gói test mới). `go.mod` thêm thư viện ADR-44 `github.com/aws/aws-sdk-go-v2`
  (`service/s3` v1.114.10 đúng bản ADR-44, cộng lõi `aws-sdk-go-v2` v1.48.0 và `smithy-go` v1.28.9 là phụ thuộc kèm của
  SDK, nhập trực tiếp để đọc mã lỗi; không cần `credentials`) — chỉ tệp có tag `probe` dùng, nên không gói nào
  của tệp chạy đổi. Test kiến trúc `tests/arch` luật (d) "chỉ `internal/platform` nhập aws-sdk-go-v2" chạy `go list`
  không tag nên không thấy tệp probe (xem Quyết định Q2).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (CORS của `crm-tmp` chuẩn bị cho màn hình tải ảnh ở M1-50, gốc `CONSOLE_ORIGINS`).
- Câu sqlc, bảng, pha migration: không có.
- Cấu hình: `docker/compose.dev.yml` thêm `seaweedfs` (cổng `${CRM_S3_PORT:-8333}`), `seaweedfs-offsite`
  (`${CRM_S3_OFFSITE_PORT:-8334}`), `s3-bootstrap` (chạy `bootstrap.sh` rồi ngồi chờ, Q5), và ba dịch vụ có profile `imager`, `fakecarrier`,
  `filesync`; mạng `imaging`. Tệp mới `docker/seaweedfs/{s3.json,s3-offsite.json,bootstrap.sh}`. `Makefile` thêm `dev`, `probe-test`;
  `db-up` hiện dùng `up -d --wait` cho mọi dịch vụ không profile, nên sẽ dựng thêm hai SeaweedFS (vô hại, nhẹ) —
  `make test` vẫn chỉ cần PostgreSQL.
- Test sẽ chạy: `go test -tags probe -count=1 ./tests/probe/...` trên compose dev của `crm-t40` (`CRM_S3_PORT=8440`,
  `CRM_S3_OFFSITE_PORT=8441`); `docker compose config` có và không có profile; `go build ./...`, `go vet -tags probe`,
  `golangci-lint run --build-tags probe ./tests/probe/`, `go test ./tests/arch/...` (luật nhập không vỡ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có gì về S3 hay
  kho tệp (`internal/platform/files` là của Task M1-41). `docker/compose.backup-drill.yml` đã ghim SeaweedFS 4.48 theo
  digest và tạo bucket bằng rclone.
- Dùng lại / viết mới vì: dùng lại đúng bản ghim `chrislusf/seaweedfs:4.48@sha256:4e61d15f…` (kế hoạch yêu cầu).
  `bootstrap.sh` chạy trong chính image SeaweedFS (có `weed shell` và `curl`) thay vì thêm image rclone hay aws-cli:
  rclone không đặt được giữ phiên bản, hạn, CORS, chính sách bucket; aws-cli là công cụ mới ngoài ADR-44. Gọi S3 trong
  test bằng aws-sdk-go-v2 (ADR-44). Không viết `FileStore` ở đây (Task M1-41).

## Soát thiết kế

- 5.1–5.4: không áp dụng: không bảng, không module, không transaction.
- 5.5, 5.6: không áp dụng: không sự kiện, không lệnh ra ngoài.
- 5.7: không áp dụng: không truy vấn database.
- 5.8: không áp dụng: không có mã module; tệp probe ở `tests/probe`, không phải mã ứng dụng.
- 5.9: không áp dụng: không đường ghi API.
- 5.10: không áp dụng: không Valkey.
- 5.11: aws-sdk-go-v2 có trong ADR-44 (bản `service/s3` v1.114.10 đúng ADR). SeaweedFS đã có dòng trong
  `docs/phu-thuoc.md` (Task 2); sửa dòng đó để ghi thêm vai trò kho chính dev. Không thêm công cụ nào khác.
- 5.12: theo mẫu probe/spike của M0 (`spikes/flashsale` đo và ghi `docs/do-luong/`); số đo ghi
  `docs/do-luong/seaweedfs-m1.md`.
- 6.1–6.11: không áp dụng: không database, tiền, tồn, hàng đợi. Riêng ý "không ghi đè" (mục 9 của phép thử) chính là
  câu hỏi 6.1 cho kho tệp: đo `If-None-Match: *`; không có thì ghi lại để `FileStore` kiểm bằng `HeadObject` trước.
- 6.12: khóa trong `s3.json` là khóa dev cố định, chỉ cho compose dev, không phải bí mật thật (kế hoạch); không có dữ
  liệu cá nhân. Test không in khóa. gitleaks phải không báo (đặt khóa dạng chữ dev rõ ràng).
- 6.13 (quyền): đây là trọng tâm. Mặc định từ chối: `crm-app` chỉ có Read, Write, List trên bốn bucket, không Admin;
  xóa vĩnh viễn (`DeleteObject` có `VersionId`, kể cả qua `DeleteObjects`), tắt giữ phiên bản, đặt quy tắc hết hạn,
  sửa chính sách bucket đều bị chặn bằng chính sách bucket (đo ở mục 5 và các ca thêm). `crm-filesync` chỉ đọc kho
  chính, ghi được kho ngoài giả, không xóa vĩnh viễn ở kho ngoài.

## Quyết định thay chủ shop

- **Q1 — Chặn xóa vĩnh viễn bằng chính sách bucket.** Đo: quyền `Write` của SeaweedFS 4.48 cho `crm-app` xóa được một
  phiên bản (`DeleteObject?versionId`), tắt giữ phiên bản và đặt hạn cho bản cũ; `s3.json` không tách được các việc
  này. Chọn: `bootstrap.sh` (bằng `crm-admin`) đặt chính sách bucket `Deny` các việc đó cho `crm-app` trên bốn bucket
  (và cho `crm-filesync` ở kho ngoài giả, thêm `DeleteObject`). Vì: đúng S3 chuẩn mà ADR-36 chọn, mục 4, 5 đạt với bản
  ghim, nên không phải Điểm dừng M1-B; dễ đảo ngược (một tệp script). Mỗi tên việc có ca kiểm vì SeaweedFS nhận im lặng
  tên sai (Principal phải `arn:aws:iam::000000000000:user/<tên>`, `PutBucketCors` chứ không `PutBucketCORS`). Phương án
  khác: Object Lock chế độ GOVERNANCE (nặng hơn, không bỏ khóa được một khi bật); IAM policy gắn người dùng qua
  `weed shell s3.policy.attach` (cấu hình nằm trong filer, không trong tệp dev đọc được); dừng hỏi (không cần, spec
  đã nói "ứng dụng không xóa vĩnh viễn được" và cách này đạt đúng điều đó).
- **Q2 — Tệp probe nhập aws-sdk-go-v2 ngoài `internal/platform`.** Kế hoạch đặt test ở `tests/probe` (tag `probe`).
  Luật (d) của test kiến trúc ("chỉ `internal/platform` nhập aws-sdk-go-v2") nhắm mã ứng dụng; `go list ./...` không
  tag nên không thấy tệp này, `make test`, `make build` không đổi. Giữ đúng chỗ kế hoạch ghi, không thêm ngoại lệ vào
  test kiến trúc. Phương án khác: thêm ngoại lệ tường minh cho `tests/probe` trong `boundaries_test.go` (sửa test
  kiến trúc vì một tệp đo); đặt probe trong `internal/platform/files` (gói của Task M1-41, chưa có).
- **Q3 — Mạng `imaging` không `internal: true` ở dev.** Đo: mạng internal chặn ra internet nhưng Docker cũng bỏ cổng mở
  ra máy, mà worker dev chạy trên máy phải gọi `imager` ở 127.0.0.1:4193 (kế hoạch ghi cổng 4193). Chọn: mạng bridge
  riêng chỉ có `imager` và `seaweedfs`, `imager` `read_only`, `/tmp` tmpfs, 2 GiB, 2 CPU, không biến bí mật, người
  dùng 65534, bỏ mọi capability; chặn ra internet là việc của compose production. Phương án khác: `internal: true` và
  không mở cổng (worker dev không gọi được imager tới khi worker chạy trong compose). Đảo: một dòng.
- **Q4 — Hai tệp cấu hình danh tính (`s3.json`, `s3-offsite.json`).** Hai kho có cùng tên bucket (Task M1-45 chép
  `<chính>:<bucket>` sang `<ngoài>:<bucket>`), nên một tệp chung cho cả hai sẽ cho `crm-filesync` quyền ghi ở kho chính
  nếu có quyền ghi ở kho ngoài. Mỗi kho một tệp; `crm-app` không có trong kho ngoài.
- **Q5 — `s3-bootstrap` ngồi chờ sau khi xong.** Container đã thoát làm `docker compose up -d --wait` (lệnh của
  `make db-up`) báo lỗi ở lần chạy thứ hai. Chọn: chạy xong thì `exec sleep infinity`, healthcheck thấy `/tmp/xong`.
  Phương án khác: profile riêng và `make` gọi `compose run --rm` (lúc đó `docker compose up` trơn không tạo bucket).
- **Q6 — `make db-up` dựng cả SeaweedFS.** Mọi dịch vụ không profile chạy với `up -d --wait`; thêm khoảng 150 MiB bộ nhớ
  cho các phiên chỉ cần PostgreSQL. Giữ vậy để `docker compose up` (mục 11) đúng nghĩa "mọi dịch vụ dev"; phiên chỉ cần
  PostgreSQL vẫn chạy được `docker compose up -d --wait postgres`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 1. PutObject kiểm tổng mặc định | `TestPutObject_DefaultChecksum` | `make probe-test` (`CRM_S3_PORT=8440 CRM_S3_OFFSITE_PORT=8441`, sau `docker compose -p crm-t40 down -v` và `make dev`) | PASS: "PutObject với kiểm tổng mặc định (WhenSupported, CRC32) được, đọc lại đúng byte" |
| 2. Ký sẵn PUT 1.000/1.001 byte | `TestPresignedPut_ContentLengthSigned` | như trên | PASS: "1.000 byte → 200; 1.001 byte → 403 (SignatureDoesNotMatch)" |
| 3. Ký sẵn GET hạn 2 s | `TestPresignedGet_Expires` | như trên | PASS: "đọc ngay → 200; sau 3 s → 403" |
| 4, 5. Dấu xóa; crm-app không xóa vĩnh viễn | `TestDelete_AppCannotPurge` (3 bucket) | như trên | PASS: "dấu xóa được tạo, crm-admin đọc bản cũ; crm-app DeleteObject/DeleteObjects có VersionId → AccessDenied" |
| crm-app không nới bucket (ca thêm) | `TestAppCannotLoosenBuckets` (4 bucket) | như trên | PASS. Trước khi sửa tên `PutBucketCORS` → `PutBucketCors`: FAIL "crm-app ghi CORS: được phép" ×4 |
| 6. Hạn 1 ngày crm-tmp | `TestTmpExpiration` | như trên | PASS: "crm-tmp có quy tắc S3 Expiration Days=1 …, ba bucket kia không có" |
| 7. Nhiều phần 3 × 5 MiB | `TestMultipartUpload` | như trên | PASS: "3 phần × 5 MiB ghép được, đọc lại đúng 15 MiB" |
| 8. CORS từ localhost:4192, chỉ crm-tmp | `TestTmpCORS` | như trên | PASS: Allow-Origin="http://localhost:4192" Allow-Methods="PUT"; gốc lạ → rỗng; ba bucket kia → rỗng. Trước khi đổi cờ `-s3.allowedOrigins` (phát hiện của reviewer vòng 1): FAIL "OPTIONS crm-public từ http://localhost:4192: … Allow-Methods=\"GET, PUT, POST, DELETE, HEAD\"" |
| 9. If-None-Match: * | `TestPutIfNoneMatch` | như trên | PASS: "lần ghi thứ hai bị từ chối: PreconditionFailed" |
| crm-filesync hai kho (ca thêm) | `TestFilesyncIdentity` | như trên | PASS, gồm ở kho ngoài: xóa, xóa có VersionId, tắt giữ phiên bản, xóa chính sách, đặt hạn → AccessDenied |
| 10. Bộ nhớ | `TestMemory`; `docker stats` | như trên; `docker stats --no-stream` 20 s sau khi dựng rỗng | rỗng 67 MiB; cao nhất khi ghi 1.000 × 200 KB: 326 MiB (3,1 s); ba lần đo 326–450 MiB (docs/do-luong/seaweedfs-m1.md) |
| 11. compose config, up không profile | `TestComposeConfig`; `make dev` | như trên; `docker compose -f docker/compose.dev.yml --profile imager --profile fakecarrier --profile filesync config --quiet` | PASS: không profile `[jaeger postgres seaweedfs seaweedfs-offsite s3-bootstrap]`; `make dev` từ volume rỗng xong 7,1 s; config có profile: hợp lệ |
| Test đỏ trước khi có cấu hình | cả bộ | `go test -tags probe -count=1 ./tests/probe/` trước khi viết compose | FAIL: thiếu `docker/seaweedfs/s3.json`, "không profile: thiếu dịch vụ seaweedfs" |
| bootstrap chạy lại không lỗi | — | `docker compose -p crm-t40 … restart s3-bootstrap`; `make db-up` lần hai | "main xong", "offsite xong"; container healthy |
| Không vỡ cổng kiểm khác | — | `go build ./...`, `go vet ./...`, `go vet -tags probe ./tests/probe/`, `golangci-lint run ./...`, `golangci-lint run --build-tags probe ./tests/probe/`, `go test -race ./tests/arch/...`, `gitleaks dir`, `govulncheck -tags probe ./tests/probe/`, `jscpd` | 0 issues; arch ok; "no leaks found"; "No vulnerabilities found"; 0 clones |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm `docker/seaweedfs/s3-offsite.json` (Q4) và dịch vụ `s3-bootstrap` (chạy `bootstrap.sh` cho cả hai kho).
- Thêm hai bộ ca ngoài danh sách 11 mục (`TestAppCannotLoosenBuckets`, `TestFilesyncIdentity`): cùng mục đích "ứng dụng
  không xóa vĩnh viễn được" và danh tính `crm-filesync` mà Interfaces đã nêu.
- `Makefile`: thêm `dev`, `probe-test`, biến `CRM_S3_PORT`, `CRM_S3_OFFSITE_PORT`; `db-up` giờ dựng cả SeaweedFS (Q6).
- Cổng `imager`, `fakecarrier` cũng đọc từ biến (`CRM_IMAGER_PORT`, `CRM_FAKECARRIER_PORT`, mặc định 4193, 4194) như
  `CRM_PG_PORT`, để nhiều phiên song song.

## Việc phát hiện thêm

- Hạn 1 ngày của `crm-tmp` mới đọc lại được, **chưa xác minh** đối tượng bị xóa thật sau 24 giờ (vòng lifecycle hằng
  ngày của SeaweedFS, fast path TTL tắt). Nên kiểm ở Task M1-41 hoặc M1-46 (ví dụ một đối tượng cũ hơn 1 ngày ở staging).
- Ký sẵn đường dẫn cho `imager` (Task M1-42, M1-43): đường dẫn ký cho `127.0.0.1:${CRM_S3_PORT}` không tới được từ
  trong container `imager` (ở đó kho là `seaweedfs:8333`), và chữ ký SigV4 gồm `host`. Task M1-43 phải chọn endpoint ký
  cho imager khác endpoint ký cho trình duyệt.
- Compose production (Task 21) cần mạng `imaging` `internal: true` cho `imager` (Q3), và cờ `-s3.allowedOrigins`
  không khớp gốc nào như dev (chỉ `crm-tmp` có CORS riêng).
- `seaweedfs-offsite` dev để `-s3.allowedOrigins` mặc định `"*"` (ghi chú reviewer vòng 2); đặt cùng giá trị với kho
  chính khi có task chạm tệp compose.

## Reviewer

Kết luận: ĐẠT (vòng 2).

- Vòng 1 (CẦN SỬA): `t.Skip` mới trong `TestMemory` không ghi ở "Test bị xóa hoặc tắt" → đã bỏ dòng skip (tệp đã có tag
  `probe`). Ghi chú không bắt buộc, đều đã làm: CORS chung `-s3.allowedOrigins` có thể mở cả bucket khác (thêm ca kiểm,
  ca đỏ thật, đổi cờ thành gốc không tồn tại); `t.Fatalf` trong goroutine lấy mẫu (đổi sang `dockerMemErr` trả lỗi);
  thiếu ca nới cấu hình của `crm-filesync` ở kho ngoài (đã thêm); câu `go.mod` ở Ảnh hưởng (đã sửa).
- Vòng 2 (ĐẠT): không có phát hiện phải sửa. Ghi chú không bắt buộc: `seaweedfs-offsite` vẫn để `-s3.allowedOrigins`
  mặc định `"*"`; không ảnh hưởng tính đúng (trình duyệt không gọi kho ngoài giả, cổng chỉ ở 127.0.0.1). Để lại, ghi ở
  "Việc phát hiện thêm".
