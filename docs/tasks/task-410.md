# Task 410 — Hạ tầng kho tệp: cô lập SeaweedFS khỏi imager, filesync trên máy chủ, tiền tố bucket, đo TTL crm-tmp (M1-r5)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (bù review độc lập M1, M1-r5; bảng số
`danh-so-task-toan-cuc.md` dòng 389). Phạm vi: C-01, C-03, C-06, C-08 của `docs/review-doc-lap-m1.md`, làm đúng mục
"Quyết định cho Task 406, 409, 410, 411" cuối tệp đó: chỉ sửa tệp trong repo, không deploy, không vào máy chủ, không tạo
tài khoản, không bí mật trong repo. Loại việc (quy trình mục 3): A (hạ tầng, INFRA=1, bộ Docker riêng `crm-t410`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi hàm không xuất `syncedBuckets()` trong
  `internal/platform/observability/rules.go` (2 chỗ gọi: `rules.go` `fileSyncRules`, `deephealth.go` `fileSyncAge`):
  trả bucket logic `public`, `originals`, `private` thay cho tên thật `crm-…` (C-06).
- Gói phụ thuộc ngược (go list): `observability` được nhập bởi `cmd/crm`, `cmd/imager`, `internal/httpapi`,
  `internal/platform/observability/alertjobs`, `tests/e2e`; không chữ ký nào đổi nên chỉ đổi dữ liệu test
  (`handlers_health_test.go` chèn ba dòng `file_sync_runs`).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (văn bản cảnh báo Telegram `file-sync-*` nay ghi `public`/`originals`/`private`).
- Câu sqlc, bảng, pha migration: không câu nào đổi. Nghĩa cột `platform.file_sync_runs.bucket` đổi từ tên thật
  (`crm-public`) sang tên logic (`public`); CHECK của cột chỉ đòi khác rỗng nên không cần migration. Chưa máy chủ nào
  chạy (dev có dòng cũ `crm-…`: luật coi như "chưa có lần chép tốt" tới lượt chép kế tiếp, ≤ 1 giờ).
- Tệp hạ tầng:
  - `docker/compose.prod.yml`, `docker/compose.dev.yml` (C-01): `seaweedfs` rời mạng `imaging`; dịch vụ mới
    `s3-imaging` (socat ghim digest, chỉ chuyển `8333 → seaweedfs:8333`) ở `default` + `imaging`; worker
    `S3_IMAGER_ENDPOINT=http://s3-imaging:8333`. (C-03) prod thêm dịch vụ `filesync` + volume `filesync-state`.
    (C-06) `S3_BUCKET_PREFIX` của ứng dụng và `FILESYNC_SRC_BUCKET_PREFIX` của filesync lấy từ một biến. Dev:
    `s3-imaging` trong profile `imager`; `s3-bootstrap` có `WEED_MASTER`; image filesync đè được bằng
    `CRM_FILESYNC_IMAGE` (để phiên song song không ghi đè `crm-filesync:dev` của nhau, cùng tên biến với production).
  - `docker/compose.prod.env.example`: biến của filesync, `CRM_FILESYNC_IMAGE`, `S3_BUCKET_PREFIX`.
  - `docker/compose.prod.no-filesync.yml` (mới, sau review lượt 2): đường lui khi chưa có kho ngoài — gộp sau
    `compose.prod.yml` thì `filesync` bị loại khỏi project, worker `FILESYNC_EXPECTED=false`.
  - `scripts/filesync/lib.sh` (+ chú thích `ls-offsite.sh`, `restore.sh`): bucket logic, `FILESYNC_SRC_BUCKET_PREFIX`.
  - `docker/seaweedfs/bootstrap.sh` (C-06, C-08): tên bucket theo tiền tố; bật lifecycle TTL fast path cho `tmp`.
  - `Makefile` `test-imager-compose`: dựng thêm `s3-imaging`.
  - `tests/e2e/files_pipeline_test.go`: mặc định `S3_IMAGER_ENDPOINT` → `http://s3-imaging:8333`.
  - Test: `tests/deploy/compose_storage_test.go` (mới), `tests/deploy/compose_test.go` (trường `Command`),
    `internal/imager/compose_test.go` (ca mới, đối chứng qua `s3-imaging`), `tests/probe/tmp_expiry_test.go` (mới),
    `tests/e2e/filesync_test.go` (bucket logic + `realBucket`), `rules_filesync_test.go`, `handlers_health_test.go`.
  - Tài liệu: `docs/RUNBOOK.md` (filesync máy chủ, mục mới "Hạn 24 giờ của crm-tmp", bước bootstrap),
    `docs/do-luong/seaweedfs-m1.md` (mục 6, mục đo mới), `docs/threat-model-m1.md` (mạng imager),
    chú thích `internal/platform/config/config.go` (S3ImagerEndpoint).
- Test sẽ chạy: `go test ./tests/deploy/...` (compose config, cần lệnh docker); `go test -race
  ./internal/platform/observability/... ./internal/httpapi/...`; `make test-imager-compose` (imager thật trên bộ
  `crm-t410`); `go test -tags probe ./tests/probe/ -run 'TestTmp'` (SeaweedFS dev của `crm-t410`); `go test -tags
  filesync ./tests/e2e/ -run TestFileSync` (container filesync thật); `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về compose
  hay shell); grep `socat`, `imager-port` (dev đã có bộ chuyển cổng socat ghim digest `alpine/socat:1.8.0.3@sha256:beb4…`);
  grep `checkNoAppKeys`, `checkFilesyncEnv`, `prodProject`, `configEnv` trong `tests/deploy`; `compose()` trong
  `internal/imager/compose_test.go`; `adminClient`, `appClient` trong `tests/probe`.
- Dùng lại / viết mới vì: dùng lại đúng image socat của `imager-port` cho `s3-imaging` (một bản cho cả repo); dùng lại
  `checkNoAppKeys` cho filesync production, `configEnv` cho ca đổi tiền tố; dùng lại `compose()` cho phép thử mạng.
  Viết mới một hàm kiểm mạng của imager dùng chung cho dev và prod (`checkImagingNetwork`) thay vì chép hai lần.

## Soát thiết kế

5.1–5.3 không áp dụng: không bảng, không dữ liệu module mới (`file_sync_runs` vẫn của nền tảng, chỉ đổi giá trị ghi).
5.4–5.6 không áp dụng: không transaction, sự kiện hay lệnh ra ngoài mới.
5.7 không áp dụng: `file_sync_runs` không có `tenant_id`.
5.8 không áp dụng: không đổi mã module nghiệp vụ.
5.9 không áp dụng: không đường ghi HTTP mới.
5.10 không áp dụng: không Valkey.
5.11: không thêm thư viện, không thêm công nghệ: `s3-imaging` dùng image socat đã ghim trong repo; lifecycle TTL fast path
là tính năng của SeaweedFS 4.48 đã ghim (ADR-36). Không cần dòng `docs/phu-thuoc.md` (không phải phụ thuộc mã).
5.12: theo mẫu có sẵn: dịch vụ cứng hóa như `imager-port`/`filesync` dev; test compose qua `docker compose config`.
6.1–6.5 không áp dụng: không câu SQL mới, không khóa.
6.6: filesync giữ lịch và trạng thái cũ (mỗi giờ, lỗi một bucket không chặn bucket khác).
6.7 không áp dụng.
6.8: `s3-imaging` chết → imager không tải được ảnh → lỗi tạm của lượt xử lý ảnh (đường thử lại của Task 409), không mất
dữ liệu; `restart: unless-stopped`.
6.9: socat không giữ hạn riêng; hạn do worker (30 s) và imager (`IMAGER_STEP_TIMEOUT`) quyết định như trước.
6.10, 6.11 không áp dụng: không tiền, không quy luật mới.
6.12: không dữ liệu cá nhân mới; cảnh báo chỉ có tên bucket logic. Bản thô có EXIF/GPS ở `crm-tmp` (R10) là lý do
của C-08: phải thật sự bị xóa sau 24 giờ.
6.13 quyền: imager mất đường tới cổng không xác thực (master 9333/19333, volume 8080/18080, filer 8888/18888, S3 gRPC
18333); filesync production không có khóa của `crm-app` (`checkNoAppKeys`), chỉ role `crm_filesync`.

## Quyết định thay chủ shop

- **Q1 (C-01) cách tách mạng:** chọn bộ chuyển cổng `s3-imaging` (socat, chỉ `8333 → seaweedfs:8333`) nằm ở `imaging`
  và `default`; `seaweedfs` chỉ còn ở `default`. Imager vẫn chỉ cầm đường dẫn ký sẵn do worker ký (ADR-45 "chỉ ra mạng
  tới kho tệp qua đường dẫn ký sẵn"); SigV4 ký cả host nên worker ký với `http://s3-imaging:8333` (socat chuyển nguyên
  byte, header Host giữ nguyên). Phương án khác: (a) bật JWT của SeaweedFS (`security.toml`) — chỉ che HTTP của filer,
  volume; gRPC của master/filer/S3 vẫn không xác thực nếu không có mTLS, cấu hình dài và mỗi lần nâng bản phải đo lại;
  (b) worker tải ảnh rồi đẩy byte cho imager ("imager chỉ nhận/trả dữ liệu qua worker" theo nghĩa đen) — đổi giao thức
  imager và đường xử lý ảnh (Task 42, 43, 409), ngoài phạm vi một task hạ tầng. Bộ chuyển cổng cho đúng tính chất
  cần có (imager không tới được gì của kho ngoài S3 có kiểm danh tính) với thay đổi nhỏ nhất. Đảo ngược: revert commit.
- **Q2 (C-08) kết quả đo và cách sửa:** đo ngày 19/11 trên bộ `crm-t410` (SeaweedFS 4.48): `weed server` **không** chạy
  lifecycle worker; quy tắc `Expiration` chỉ được thực thi khi có `weed worker` + `weed admin` hoặc khi chạy tay
  `s3.lifecycle.run-shard`. Đối tượng vào `crm-tmp` có `ttlSec: 0` → **ảnh thô không bao giờ bị xóa** (nghi ngờ C-08
  là thật). Sửa: bật lifecycle TTL fast path cho `crm-tmp` (`s3.bucket.lifecycle.fastpath -enable`, bucket không giữ
  phiên bản): `PutObject` gắn `ttlSec: 86400`, dữ liệu vào volume `Ttl:1d`, filer và volume server tự xóa, không cần
  worker. Phương án khác: thêm container `weed worker` + `weed admin` (thêm hai tiến trình, thêm cổng quản trị), hoặc
  container chạy `s3.lifecycle.run-shard` mỗi giờ (cần quyền tới master/filer gRPC, thêm một dịch vụ). Fast path là
  một lệnh trong `bootstrap.sh`, không thêm tiến trình. Cái giá: TTL gắn lúc ghi, đổi quy tắc sau này không áp lại cho
  đối tượng cũ (crm-tmp cố định 1 ngày, Q18). Đảo ngược: `-disable` và revert commit.
- **Q3 (C-06) bucket logic trong `file_sync_runs`:** chọn ghi tên logic (`public`…) như đề xuất thứ nhất của review:
  luật cảnh báo, `/health/deep` không cần biết tiền tố, không đổi chữ ký `DeepHealth`. Phương án khác: giữ tên thật và
  truyền tiền tố vào `RuleOptions`, `DeepHealth` (đổi chữ ký hai hàm xuất ra ngoài). Tiền tố của ứng dụng và của
  filesync lấy từ cùng một biến `S3_BUCKET_PREFIX` trong compose production nên không lệch được. Đảo ngược: revert.
- **Q4 (C-03) `FILESYNC_EXPECTED`:** giữ `"true"` ở worker production vì compose nay có dịch vụ `filesync`; test đòi
  worker bật cảnh báo thì phải có dịch vụ. Chưa có kho ngoài (cần chủ shop tạo tài khoản): tệp đường lui
  `compose.prod.no-filesync.yml` đặt qua `COMPOSE_FILE` (sau review lượt 2: `--scale` hay override không có trong mọi
  lệnh deploy thì không giữ được). Image `CRM_FILESYNC_IMAGE` truyền bằng biến như `CRM_IMAGER_IMAGE` (CI hiện
  chưa dựng image imager, filesync: ghi ở "Việc phát hiện thêm").
- **Q5 (ghi chú Task 409):** ràng buộc trần thử lại ảnh riêng tư và hạn `crm-tmp` ghi ở RUNBOOK và
  `docs/do-luong/seaweedfs-m1.md`: trần thử lại (`IMAGE_RETRY_MAX_HOURS`, Task 409) phải nhỏ hơn 24 giờ, không thì ảnh
  gốc ở `crm-tmp` đã tự xóa trước lượt thử cuối và lượt đó ra `source_missing`.

## Bằng chứng

Mọi lệnh chạy trên bộ Docker riêng `crm-t410` (PostgreSQL 5910, S3 20410, kho ngoài giả 21410), ngày 19/11/2026.
Trước khi sửa, các test mới đều đỏ đúng lý do (ghi ở cột cuối "trước:").

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| C-01: trên mạng của imager (prod) chỉ có imager, crm-worker, s3-imaging; seaweedfs không ở đó; s3-imaging chỉ chuyển `8333 → seaweedfs:8333`, cứng hóa, ghim digest; worker ký cho `http://s3-imaging:8333` | `TestProdCompose_ImagerReachesOnlyS3` | `go test -count=1 ./tests/deploy/` | `ok elimaz.com.vn/crm/tests/deploy 3.383s` (trước: "seaweedfs chung mạng imaging với imager…", "thiếu dịch vụ s3-imaging") |
| C-01 như trên cho compose dev (thêm imager-port; s3-imaging trong profile imager) | `TestDevCompose_ImagerReachesOnlyS3` | như trên | `ok` (trước: cùng hai lỗi) |
| C-01 chạy thật: từ container imager, mọi cổng 8333, 8888, 18888, 9333, 19333, 8080, 18080, 18333 của seaweedfs (theo tên và theo IP) không nối được; s3-imaging không mở cổng nào ngoài 8333; s3-imaging:8333 nối được | `TestImagerCompose_CannotReachStoreInternals` (+ đối chứng `mustReachS3` trong `TestImagerCompose_CannotReachPostgres`) | `docker compose -p crm-t410 … --profile imager up -d --wait imager imager-port s3-imaging postgres seaweedfs` rồi `COMPOSE_PROJECT=crm-t410 go test -tags imagercompose -count=1 -run '^TestImagerCompose_' -v ./internal/imager/` | `--- PASS: TestImagerCompose_CannotReachStoreInternals (3.93s)`, 6/6 PASS, `ok … 6.751s` (trước: "imager nối được seaweedfs:8333 … 172.31.0.2:19333 …" — 16 cổng) |
| C-01 không làm hỏng đường ảnh: imager tải gốc và đẩy biến thể qua s3-imaging bằng đường dẫn ký sẵn (công khai có GPS, riêng tư) | `TestFilesPipeline_PublicPhotoGPS`, `TestFilesPipeline_PrivateImages` | `go test -tags imager -count=1 -run TestFilesPipeline -v ./tests/e2e/` | `--- PASS: TestFilesPipeline_PublicPhotoGPS (4.87s)`, `--- PASS: TestFilesPipeline_PrivateImages (6.86s)`, `ok … 12.467s` |
| C-03: worker prod `FILESYNC_EXPECTED=true` thì có dịch vụ `filesync` (không profile), không khóa crm-app, role crm_filesync, nguồn `http://seaweedfs:8333`, đủ biến, cứng hóa, init, volume `/state`, mem_limit, không ở edge/imaging; image theo biến (ghim digest) | `TestProdCompose_FilesyncService`, `TestProdCompose_ImagesPinnedNotBuilt`, `TestProdCompose_LogLimits` | `go test -count=1 ./tests/deploy/` | `ok` (trước: "FILESYNC_EXPECTED=true nhưng compose production không có dịch vụ filesync…") |
| C-03 đường lui chưa có kho ngoài: gộp `compose.prod.no-filesync.yml` → không có `filesync`, worker `FILESYNC_EXPECTED=false`, mọi dịch vụ khác còn đủ | `TestProdCompose_NoFilesyncOverride` | `go test -count=1 -run 'NoFilesyncOverride\|FilesyncService' -v ./tests/deploy/`; thêm `docker compose -f docker/compose.prod.yml -f docker/compose.prod.no-filesync.yml --env-file docker/compose.prod.env.example up --dry-run -d` | `--- PASS: TestProdCompose_NoFilesyncOverride (0.10s)`; dry-run kéo image của mọi dịch vụ trừ filesync (không dòng `crm-filesync`). Thiếu tệp override → test đỏ ("docker compose config … exit status 1") |
| C-06: ứng dụng và filesync cùng tiền tố từ `S3_BUCKET_PREFIX` (rỗng → `crm-`, `stg-` → `stg-`) | `TestProdCompose_BucketPrefixShared` | như trên | `ok` (trước: `S3_BUCKET_PREFIX = <nil>`) |
| C-06: filesync chép ba bucket logic, nguồn theo `FILESYNC_SRC_BUCKET_PREFIX`, đích theo `FILESYNC_DST_BUCKET_PREFIX`; bucket lạ (kể cả tên cũ `crm-public`) lỗi | `TestFilesyncLib_BucketNames` | như trên | `ok` (trước: `FILESYNC_BUCKETS` = "crm-public crm-originals crm-private", `dst_of crm-public` không lỗi) |
| C-06: luật cảnh báo và `/health/deep` đọc dòng `file_sync_runs` theo bucket logic | `TestRule_FileSyncAge`, `TestRule_FileSyncAgeAnyBucket`, `TestRule_FileSyncFailed`, `TestDeepHealthFileSync`; `TestHealthDeepFileSyncAge` (gói `internal/httpapi`) | `go test -race -count=1 -run 'FileSync' ./internal/platform/observability/`; `go test -race -count=1 -run 'FileSync' ./internal/httpapi/` | `ok … 5.022s` (trước: 4 FAIL, ví dụ "crm-public chưa có lần chép tốt…"); httpapi: `ok elimaz.com.vn/crm/internal/httpapi 2.675s` |
| C-06 chạy thật: container filesync (image dựng từ nhánh này, `crm-filesync:t410`) chép, đối chiếu, ghi `file_sync_runs` theo tên logic, `ls-offsite.sh private …` giải mã | `TestFileSync` | `docker build -f docker/filesync/Dockerfile -t crm-filesync:t410 .` rồi `CRM_FILESYNC_IMAGE=crm-filesync:t410 go test -tags filesync -count=1 -run TestFileSync -v ./tests/e2e/` | `--- PASS: TestFileSync (31.31s)` |
| Image filesync dev đè được bằng `CRM_FILESYNC_IMAGE`, mặc định `crm-filesync:dev` | `TestDevCompose_FilesyncImageFromEnv` | `go test -count=1 ./tests/deploy/` | `ok` (trước: image "crm-filesync:dev") |
| C-08: `crm-tmp` bật lifecycle TTL fast path (bootstrap.sh) | `TestTmpExpiry_FastPathEnabled` | `go test -tags probe -count=1 -run 'TestTmpExpiry' -v ./tests/probe/` | `KẾT QUẢ: crm-tmp bật lifecycle TTL fast path` (trước: "crm-tmp chưa bật lifecycle TTL fast path") |
| C-08: PUT ký sẵn của crm-app vào crm-tmp → `ttlSec 86400`, volume `Ttl:1d`; chép sang crm-originals → `ttlSec 0`, volume không hạn; ba bucket giữ lâu không volume nào có hạn | `TestTmpExpiry_UploadStampedOneDay` | như trên | `KẾT QUẢ: PUT ký sẵn vào crm-tmp → ttlSec 86400, volume 41 Ttl 1d; chép sang crm-originals → ttlSec 0, volume 52 không hạn; …` (trước: "đối tượng crm-tmp có ttlSec 0, muốn 86400") |
| C-08: cơ chế `ttlSec` của filer bản ghim xóa thật (hạn ngắn, không chờ được 24 giờ) | `TestTmpExpiry_TTLEntryDisappears` | như trên | `KẾT QUẢ: mục ttlSec 60 biến mất sau 1m2s, không ai gọi xóa` |
| C-08: quy tắc Expiration một mình không xóa (không có lifecycle worker) | đo tay, ghi ở `docs/do-luong/seaweedfs-m1.md` | bucket thử `probe-ttl` quy tắc `Date=2020-01-01`, PUT, đọc lại sau 16 phút; rồi `s3.lifecycle.run-shard -shards 0-15 -s3 localhost:18333` | sau 16 phút `auto 200`; sau `run-shard` đối tượng thử thứ nhất `404` |
| Cổng tĩnh và toàn bộ test sau khi gộp main (build, vet, golangci-lint, catalog, jscpd, gitleaks, oasdiff, test-guard, `go test -race ./...`, test deploy) | mọi gói | `ci-task.sh` của quy trình (lượt 1 bị cắt ở hạn 10 phút của lệnh nền, lint báo 3 lỗi phức tạp/nolint trong test mới → đã tách hàm; lượt 2 trọn vẹn) | `== test (toàn bộ: affected.py thoát 4): 621 giây`, `== test deploy …: 5 giây`, `exit=0` |
| Như trên, sau các sửa của review lượt 2 (tệp đường lui, test mới, RUNBOOK) | mọi gói | `ci-task.sh` của quy trình (lần chạy lại được phép sau review) | `== test (toàn bộ: affected.py thoát 4): 463 giây`, `== test deploy …: 4 giây`, `exit=0` |
| Các phép thử cũ của crm-tmp không đổi (quy tắc, CORS, chính sách) | `TestTmpExpiration`, `TestTmpCORS`, `TestAppCannotLoosenBuckets` | `go test -tags probe -count=1 -run 'TestTmpExpiry|TestTmpExpiration|TestTmpCORS|TestAppCannotLoosen' -v ./tests/probe/` | 6/6 PASS, `ok … 65.279s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Quyết định 19/11 cho Task 410 ghi "imager chỉ nhận/trả dữ liệu qua worker". Bản làm: imager vẫn tự tải gốc và đẩy
  biến thể với kho, nhưng **chỉ** qua cổng S3 (bộ chuyển cổng `s3-imaging`) bằng đường dẫn do worker ký; không tới được
  cổng nào khác của SeaweedFS. Lý do và phương án khác ở "Quyết định thay chủ shop" Q1. Muốn đúng câu chữ (worker đẩy
  byte cho imager) thì cần một task đổi giao thức imager.

## Việc phát hiện thêm

- `IMAGE_RETRY_MAX_HOURS` (Task 409) mặc định 24, bằng đúng hạn `crm-tmp`: ảnh còn lỗi hạ tầng ở giờ cuối của trần thử
  lại có thể ra `source_missing` (bản gốc đã tự xóa). Đề xuất: mặc định 23 hoặc `config.Load` đòi < 24. Không tự đổi
  (mã của Task 409); đã ghi ràng buộc ở RUNBOOK và `docs/do-luong/seaweedfs-m1.md`.
- CI (`.github/workflows/ci.yml`) chỉ dựng image chính; image `imager`, `filesync`, `postgres` mà compose production đòi
  (`CRM_IMAGER_IMAGE`, `CRM_FILESYNC_IMAGE`, `CRM_POSTGRES_IMAGE`) chưa có bước dựng. Cần một task CI (đẩy image lên
  registry cần OK riêng của chủ shop).
- Tải nhiều phần chưa xong vào `crm-tmp` (`AbortIncompleteMultipartUpload`) vẫn chỉ được dọn bởi lifecycle worker (không
  chạy). Ứng dụng hiện không dùng tải nhiều phần vào `crm-tmp`; nếu sau này dùng thì cần `s3.clean.uploads` theo lịch.
- Máy chủ (việc của chủ shop, ngoài máy này): khi dựng kho thật, chạy `bootstrap.sh main` với `WEED_MASTER`, kiểm
  `fast path: enabled`; tạo bucket kho ngoài, khóa `crm-filesync`, hai mật khẩu crypt rồi mới `up` dịch vụ `filesync`.

## Reviewer

Kết luận: ĐẠT (lượt 3, 19/11/2026).

- Lượt 1 — CẦN SỬA: đường lui "chưa có kho ngoài" trong RUNBOOK không chạy được vì `compose.prod.yml` đòi mọi biến
  `${…:?}` của cả tệp. Sửa: ghi giá trị giữ chỗ; thêm ghi chú đổi tiền tố thì đổi quyền theo bucket trong tệp danh tính
  SeaweedFS; thêm mục "Khác kế hoạch"; thêm lệnh httpapi vào bảng bằng chứng.
- Lượt 2 — CẦN SỬA: `--scale filesync=0` có thể vẫn kéo image và override ngoài repo không có trong mọi lệnh deploy.
  Sửa: thêm `docker/compose.prod.no-filesync.yml` (filesync vào profile không bật, worker `FILESYNC_EXPECTED=false`),
  RUNBOOK dùng `COMPOSE_FILE` cho mọi lệnh, test `TestProdCompose_NoFilesyncOverride`, `up --dry-run` không kéo image
  filesync.
- Lượt 3 — ĐẠT, không phát hiện phải sửa. Ghi chú không bắt buộc (không làm): lùi bằng cách checkout bản repo trước Task
  410 thì `COMPOSE_FILE` trỏ tới tệp không có; mục "Chạy tay" của RUNBOOK còn `--profile filesync` (cách của dev, có từ
  trước); `IMAGE_RETRY_MAX_HOURS` mặc định 24 nên giao task riêng (đã ở "Việc phát hiện thêm").
