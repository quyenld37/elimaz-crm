# Task 42 — Tiến trình imager (ADR-45)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 42, M1-42). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (`internal/imager`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có ký hiệu cũ nào đổi. Thêm vào gói có sẵn:
  `observability.CheckLive`, `observability.ErrNotLive` (commit 1, thay hàm không xuất `checkLive` của `cmd/crm`, một
  chỗ gọi). Gói mới `internal/imager`
  xuất `Output`, `Request`, `OutputResult`, `Result`, `Limits`, `Runner`, `ExecRunner`, `Process`, `Server`,
  `NewServer`, `NewHTTPClient`, hằng `Status*`, `Reason*`; tệp chạy mới `cmd/imager`. Chưa ai nhập (Task M1-43
  sẽ gọi qua HTTP và có thể dùng lại kiểu `Request`/`Result`).
- Gói phụ thuộc ngược (go list): không có (gói mới). `tests/arch` luật (g) đã có từ Task M1-25 sẽ kiểm `internal/imager`
  và `cmd/imager` không nhập `internal/modules/**`, `internal/platform/{db,tenancy,config,idempotency,queue,commands}`, pgx.
  `go.mod` thêm `github.com/gabriel-vasile/mimetype` v1.4.16 (ADR-45 nêu tên và bản).
- Operation OpenAPI bị chạm, oasdiff: không có. `POST /v1/process`, `GET /health/live` là HTTP nội bộ của `imager`,
  không thuộc `openapi.yaml` của `api` (người gọi duy nhất là worker trên mạng `imaging`).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có (imager không có database).
- Cấu hình, hạ tầng: tệp mới `docker/imager/Dockerfile` (stage `build`, `test`, image chạy). `docker/compose.dev.yml`
  (dịch vụ `imager` 4193 và mạng `imaging` có từ Task M1-40) **có sửa** theo Q1: mạng `imaging` `internal: true`, bỏ
  cổng mở của `imager`, thêm dịch vụ `imager-port` (socat, profile `imager`); `make db-up` không bật profile này.
  `make dev` tự bật profile `imager` khi có Dockerfile. `Makefile` thêm `test-imager` (test trong container có libvips),
  `test-imager-compose` (env và mạng của container trong compose) và biến `CRM_IMAGER_PORT ?= 4193` (export).
  `docs/phu-thuoc.md` thêm dòng libvips, mimetype, tini, socat. Ảnh thử `testdata/images/*` cùng chương trình sinh lại
  `testdata/images/gen/`. Gói `imager` còn xuất `ErrBadRequest`, `Request.Validate`.
- Test sẽ chạy: `make test-imager` (`go test -race ./internal/imager/... ./cmd/imager/...` trong stage `test`, libvips
  8.16.1); `make test-imager-compose` (tag `imagercompose`); trên máy: `go build ./...`, `go vet`, `golangci-lint`,
  `go test ./internal/imager/... ./cmd/imager/...` (ca cần libvips/prlimit tự bỏ qua trên macOS; trong container
  `IMAGER_REQUIRE_VIPS=1` biến bỏ qua thành lỗi), `go test ./tests/arch/...` (luật nhập).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có gì về chạy
  tiến trình con, giới hạn tiến trình, tải/ghi HTTP có giới hạn hay nhận dạng loại tệp. `internal/platform/files`
  (`MemStore`, `FileStore`) là của Task M1-41, chưa có trên nhánh này. `internal/platform/config` có `reader.integer`
  nhưng luật (g) cấm imager nhập `config`. `cmd/crm/main.go` có mẫu tắt êm và `healthcheck`.
- Dùng lại / viết mới vì: jscpd báo `checkLive` của `cmd/crm/main.go` trùng với lệnh `healthcheck` của imager → gộp:
  dời nguyên hàm sang `observability.CheckLive(ctx, port, timeout)` (gói `observability` chỉ nhập thư viện chuẩn, luật g
  cho imager nhập), commit cấu trúc riêng trước, rồi imager dùng lại. Đoạn dựng giới hạn OOM trùng giữa hai tệp test →
  một hàm `oomLimits`. Tắt êm của `cmd/crm` giữ riêng (khác vai trò, khác thứ phải dừng). Đọc biến môi trường trong `cmd/imager/main.go` bằng hàm nhỏ riêng (không nhập `config`, luật g).
  Kho giả trong test là `httptest` nhỏ trong `server_test.go`/`pipeline_test.go` thay cho `MemStore` (chưa có, Task
  M1-41 song song); Task M1-43 dùng `MemStore` thật. Nhận dạng loại: `net/http.DetectContentType` + `mimetype` (ADR-45).

## Soát thiết kế

- 5.1–5.4: không áp dụng: không bảng, không module nghiệp vụ, không transaction. imager không nối database (ADR-45).
- 5.5: không áp dụng: không sự kiện; worker gọi imager qua lệnh `commands` ở Task M1-43.
- 5.6: imager không gọi bên ngoài nào ngoài kho tệp bằng đường dẫn ký sẵn worker đưa; không giữ trạng thái giữa hai
  yêu cầu, nên worker gửi lại an toàn (đầu ra theo khóa cố định). Trả `rejected timeout` khi một bước quá hạn để
  worker xếp lại (`not_sent`).
- 5.7: không áp dụng: không có `tenant_id`, không truy vấn.
- 5.8: imager không phải module nghiệp vụ; lớp HTTP mỏng (`server.go`) chỉ đọc JSON, giới hạn số việc, gọi `Process`.
- 5.9: không áp dụng: không đường ghi của `api`; `POST /v1/process` không có khóa chống trùng vì không giữ trạng thái
  (kết quả chỉ phụ thuộc đầu vào, ghi đè cùng khóa đầu ra bằng cùng nội dung).
- 5.10: không áp dụng: không Valkey.
- 5.11: libvips 8.16.1 (gói Debian 13, LGPL-2.1, gọi bằng dòng lệnh), `mimetype` v1.4.16 (ADR-45 nêu), `tini`
  (PID 1 gặt tiến trình con mồ côi) — ghi vào `docs/phu-thuoc.md`.
- 5.12: theo cấu trúc tệp của kế hoạch (`cmd/imager`, `internal/imager/{server,pipeline,runner,detect}.go`); lỗi là
  sentinel trong gói; log một lần ở mép (handler) bằng `slog`.
- 6.1: nhiều yêu cầu cùng lúc: mỗi yêu cầu một thư mục tạm riêng (`os.MkdirTemp`), không chia sẻ trạng thái;
  `IMAGER_MAX_PARALLEL` bằng semaphore không chờ → 503 + `Retry-After`.
- 6.2: gửi lại cùng yêu cầu: không giữ trạng thái; ghi lại cùng khóa đầu ra (worker dùng `Looker` ở M1-43).
- 6.3–6.5: không áp dụng: không database.
- 6.6: imager không tự thử lại; trả lý do để worker quyết định (ADR-45, M1-43).
- 6.7: không áp dụng: kho tệp nội bộ, không 429.
- 6.8: imager chết giữa chừng: worker nhận lỗi HTTP → `not_sent`, chạy lại; tệp tạm nằm trên tmpfs mất theo container.
- 6.9: mỗi bước (tải nguồn, mỗi lệnh con, mỗi PUT) có hạn `IMAGER_STEP_TIMEOUT`; lệnh con quá hạn bị giết cả nhóm
  tiến trình (`Setpgid`, `SIGKILL` nhóm). Ngắt kết nối của worker cũng hủy bước đang chạy.
- 6.10: không áp dụng: không tiền.
- 6.11: quy luật vùng cao, có test rapid: với mọi nội dung không phải JPEG/PNG/WebP thật trong danh sách cho phép,
  không có lệnh con nào chạy (không giải mã); lý do luôn thuộc tập đã khai.
- 6.12: đường dẫn ký sẵn có chữ ký (coi như token): không bao giờ ghi vào log hay vào lỗi trả về; log chỉ có trạng thái,
  lý do, loại, số đo. Tiến trình con nhận môi trường tối thiểu (không thừa hưởng biến của imager). Ảnh ra bỏ hết
  siêu dữ liệu (`keep=none`): EXIF, GPS.
- 6.13: imager không xác thực bên gọi (ADR-45: không giữ khóa); bảo vệ là mạng: chỉ mạng `imaging`. Không theo
  chuyển hướng HTTP (một đường dẫn ký sẵn không bao giờ chuyển hướng), chỉ nhận `http`/`https`.

## Quyết định thay chủ shop

- **Q1 — Mạng `imaging` của compose dev thành `internal: true`, thêm dịch vụ chuyển cổng `imager-port`.** Đo (Docker
  29.2.1 trên máy này, 18/11/2026): với mạng bridge thường của Task M1-40, `imager` không phân giải được tên `postgres`
  nhưng **nối được `192.168.0.3:5432` (IP của PostgreSQL ở mạng default) qua gateway** — tách hai mạng bridge không chặn
  được. Ca `TestImagerCompose_CannotReachPostgres` của kế hoạch đỏ. Chọn: `imaging` `internal: true` (không gateway: không
  tới mạng khác, không ra internet — đúng ADR-45 và ghi chú của Task M1-40 cho production), bỏ cổng mở của `imager`, thêm
  `imager-port` (`alpine/socat` ghim digest, chỉ dev, chỉ chuyển `127.0.0.1:${CRM_IMAGER_PORT:-4193}` → `imager:4193`,
  nằm ở cả hai mạng, chỉ đọc, người dùng 65534, bỏ mọi capability) để worker dev trên máy vẫn gọi được cổng 4193 như
  kế hoạch. Điều phối viên giao làm phần dev/compose này. Phương án khác: giữ mạng thường và chấp nhận dev nối được
  PostgreSQL theo IP (trái ADR-45, test đỏ); chạy worker trong compose (đổi cách dev của M0). Đảo: sửa 3 khối của
  `compose.dev.yml`. Lưu ý: đổi `internal` của một mạng đã có cần `docker compose down` rồi `up` (compose tạo lại mạng
  nhưng mất bí danh dịch vụ của container cũ — đo được, `seaweedfs` không phân giải được tới khi dựng lại).
- **Q2 — libvips là gói Debian 13 (`libvips-tools` 8.16.1-1+deb13u2) cộng `libheif-plugin-aomenc`, không dựng từ mã nguồn
  (Q14).** Đo: `vips -l` có `heifsave`, `vips thumbnail x.jpg x.avif` ra `heif-compression: av1`. Đúng điều kiện kế hoạch
  ghi. Bản ghim trong Dockerfile; nâng bản thì chạy lại `make test-imager` và phép đo.
- **Q3 — Một lần `vipsheader <tệp>` (dòng tóm tắt) thay cho ba lần `-f vips-loader`, `-f width`, `-f height`; kích thước
  thật của đầu ra đọc từ JPEG cùng chiều rộng bằng `image/jpeg.DecodeConfig` (chỉ phần đầu, mã Go).** Đo: mỗi tiến
  trình con libvips khởi động 25–110 ms trên máy này; bản đầu 27 tiến trình/ảnh, nay 13 (`TestImager_ThirteenChildProcessesPerImage`),
  đúng con số "× 13" kế hoạch dùng để đo. Dòng tóm tắt chỉ có số và tên liệt kê của libvips (tên tệp do imager đặt),
  đọc bằng biểu thức chặt; không khớp → `decode_failed`. Phương án khác: đúng chữ kế hoạch (`-f width`, `-f height`,
  thêm 2 tiến trình/ảnh). Đảo: một hàm.
- **Q4 — Thêm lý do `source_failed` (tải nguồn lỗi, mã khác 200) và `internal` (tạo thư mục tạm lỗi).** Danh sách lý
  do của kế hoạch không có chỗ cho lỗi tải nguồn (link hết hạn, kho tạm lỗi), mà gộp vào lý do nào có sẵn đều sai nghĩa.
  Hai lý do này, cùng `timeout` và `upload_failed`, là lỗi tạm: ghi vào "Việc phát hiện thêm" cho Task M1-43 xử lý như
  `not_sent`.
- **Q5 — `OutputResult` thêm `PixelWidth`, `PixelHeight`; `Output` thêm `Headers` tùy chọn.** `Width` giữ là chiều rộng
  đã xin (khóa của biến thể, cột `width` của `file_variants` có UNIQUE theo nó — ảnh gốc 500 px thì ba chiều rộng 640,
  1080, 1600 cùng ra 500 px thật, dùng kích thước thật làm khóa sẽ trùng). `Headers`: chữ ký `PUT` của Task M1-41 có
  `content-type` (và có thể header khác); imager gửi đúng header bên ký đưa, mặc định `Content-Type: image/<định dạng>`.
- **Q6 — Đọc biến môi trường trong `cmd/imager/main.go` bằng `os.Getenv` có `//nolint:forbidigo` kèm lý do.** Luật lint
  chỉ cho `internal/platform/config` đọc biến, nhưng luật (g) của test kiến trúc cấm imager nhập `config` (không có biến
  bí mật). Phương án khác: thêm ngoại lệ đường dẫn vào `.golangci.yml` (nới cấu hình chung vì một tệp).
- **Q7 — Tiến trình con chạy với môi trường tối thiểu** (`PATH`, `LANG`, `VIPS_BLOCK_UNTRUSTED=1` chặn các bộ đọc
  libvips không dành cho dữ liệu lạ, `MALLOC_ARENA_MAX=2`, `TMPDIR`, `VIPS_CONCURRENCY`=`IMAGER_MAX_PARALLEL`), không
  thừa hưởng biến của imager. `--fail-on error` cho `vips thumbnail`: đo thấy mặc định libvips coi JPEG cụt là cảnh
  báo, trả mã 0 và tô xám phần thiếu; `fail_on=truncated` trong tên tệp không có tác dụng với `thumbnail`.
  `--height 10000000`: mặc định của `vips thumbnail <w>` giới hạn cả chiều cao bằng `w` (ảnh dọc ra hẹp hơn), đo được và
  có `TestImager_PortraitKeepsWidth`. `tini` làm PID 1 trong image để gặt tiến trình con mồ côi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): `observability.CheckLive` giữ đúng hành vi `checkLive` cũ | `TestCheckLive` (200 → nil; 503 → `ErrNotLive`; không có tiến trình → lỗi) | `go test -race -count=1 ./internal/platform/observability/` | `ok … 1.418s` |
| Commit 1: `crm healthcheck` của api/ingest vẫn chạy | `TestHealthcheckCommand`, `TestBootEachRoleThenGracefulStop` | `go test -race -count=1 -run 'TestHealthcheckCommand\|TestBootEachRoleThenGracefulStop' ./tests/e2e/` (PostgreSQL 5542) | `ok … 6.405s` |
| Commit 2: `photo-gps.jpg` → ok, 12 đầu ra; không còn EXIF/GPS/XMP/IPTC (`vipsheader -a`); rộng đúng 320/640/1080/1600; định dạng thật đúng (AVIF là AV1); Bytes/SHA256/Content-Type khớp kho | `TestImager_PhotoGPS_TwelveOutputsNoMetadata` (kiểm trước: ảnh nguồn có `GPSLatitude`) | `make test-imager` (container, libvips 8.16.1, `IMAGER_REQUIRE_VIPS=1`) | `--- PASS` (1,34 s). Đột biến `keep=none`→`keep=all`: `FAIL … 320.avif còn siêu dữ liệu (exif)` |
| Ảnh gốc 500 px không phóng to; ảnh dọc rộng đúng; CMYK → sRGB | `TestImager_NoUpscale`, `TestImager_PortraitKeepsWidth`, `TestImager_CMYKConvertedToSRGB` | `make test-imager` | `--- PASS` cả ba |
| TP03: HTML đổi đuôi `.jpg` → `not_image`, 0 lần gọi `vips`/`vipsheader` | `TestImager_RejectsHTMLAsJPEG` | `make test-imager`; `go test ./internal/imager/` trên máy | `--- PASS` |
| TP03: bom 30.000 × 30.000 (64 KB) → `too_many_pixels`, `vips` 0 lần (chỉ `vipsheader`) | `TestImager_PixelBombRejectedBeforeDecode` | `make test-imager` | `--- PASS` (0,03 s) |
| 21 MiB với MaxBytes 20 MiB → `too_large`; `truncated.jpg` → `decode_failed`, không ghi đầu ra | `TestImager_TooLarge`, `TestImager_TruncatedDecodeFailed` | `make test-imager` | `--- PASS` |
| Loại ngoài danh sách (WebP khi chỉ cho JPEG, GIF, HEIC) → `type_not_allowed`; SVG, rỗng, ZIP → `not_image`; bộ đọc libvips khác loại đã nhận → `type_not_allowed` | `TestImager_TypeNotAllowed`, `TestImager_LoaderMustMatchDetectedType` | `make test-imager` | `--- PASS` |
| Quy luật (rapid): nội dung không phải loại cho phép thì không lệnh con nào chạy; lý do luôn thuộc tập đã khai | `TestImager_PropertyNoDecodeUnlessAllowedImage` | `make test-imager` | `[rapid] OK, passed 100 tests` |
| Treo: `hang.sh`, hạn 1 s → `timeout` trong ≤ 2 s; nhóm tiến trình (sh và `sleep` con) không còn (`kill -0 -pgid` → ESRCH) | `TestImager_HangKilledWithinDeadline`, `TestExecRunner_TimeoutKillsProcessGroup` | `make test-imager` | `--- PASS` (1,04 s; 0,50 s) |
| Hết bộ nhớ: `oom.sh` xin 2 GiB dưới 256 MiB → `decode_failed` ("memory exhausted"); imager trả lời yêu cầu kế tiếp | `TestImager_OutOfMemoryKilled`, `TestExecRunner_MemoryLimit`, `TestServer_StillAnswersAfterOOM` | `make test-imager` | `--- PASS` cả ba |
| 3 yêu cầu cùng lúc với tối đa 2 → yêu cầu thứ 3 nhận 503 + `Retry-After`; rảnh thì nhận lại | `TestServer_BusyReturns503` | `make test-imager`; `go test -race` trên máy | `--- PASS` |
| `PutURL` hết hạn (403) → `upload_failed`; nguồn 403 → `source_failed`; tệp tạm bị xóa | `TestImager_ExpiredPutURL`, `TestImager_SourceUnavailable`, `TestImager_TempFilesRemoved` | `make test-imager` | `--- PASS` |
| 13 tiến trình con mỗi ảnh (1 `vipsheader` + 12 `vips`), kết quả đúng thứ tự yêu cầu | `TestImager_ThirteenChildProcessesPerImage` | `make test-imager` | `--- PASS` |
| HTTP: rejected vẫn 200; log không có đường dẫn ký sẵn; JSON sai → 400; `/health/live` 200; tiến trình con không thừa hưởng biến của imager | `TestServer_OKAndRejectedBothReturn200`, `TestServer_BadRequest` (13 ca), `TestServer_HealthAndMethods`, `TestExecRunner_MinimalEnvironment` | `make test-imager` | `--- PASS` |
| Cấu hình `cmd/imager` (mặc định, `256MiB`, sai → mã 2) | `TestLoad_*`, `TestRun_*` | `make test-imager` | `ok elimaz.com.vn/crm/cmd/imager` |
| Compose: env của tiến trình imager không khớp `(?i)secret\|password\|token\|key\|database`; không nối được `postgres:5432` theo tên và theo IP (đối chứng: `seaweedfs:8333` được); không ra internet; người dùng 65534, chỉ đọc trừ `/tmp`; máy gọi được imager qua `imager-port` | `TestImagerCompose_*` (5 test, tag `imagercompose`) | `make test-imager-compose COMPOSE_PROJECT=crm-t42 CRM_PG_PORT=5542 CRM_S3_PORT=8452 CRM_S3_OFFSITE_PORT=8453 CRM_IMAGER_PORT=4293` | 5 `--- PASS`, `ok … 1.300s`. Trước Q1: `FAIL … imager nối được 192.168.0.3:5432` |
| Đo thời gian, CPU, bộ nhớ, phần tốn thêm của dòng lệnh | `TestImager_Measure` (+ đo `/usr/bin/time`) | xem `docs/do-luong/imager-m1.md` | Tổng một ảnh 1,3–10,5 s (< 30 s); RSS đỉnh 109 MiB; phần khởi động 7–35% ảnh thật, tới 79% ảnh tổng hợp → đề xuất, không đổi |
| Luật nhập (g), build không cgo, lint, trùng mã, lỗ hổng | `tests/arch`; build; golangci-lint (cả tag `imagercompose`); jscpd; govulncheck | `go test ./tests/arch/...`; `CGO_ENABLED=0 go build -trimpath ./...`; `go vet ./...`; `golangci-lint run ./...`; `make jscpd`; `govulncheck ./internal/imager/... ./cmd/imager/...` | `ok`; build ok; vet ok; `0 issues`; `Found 0 clones`; `No vulnerabilities found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kho giả trong test là `httptest` trong `pipeline_test.go` (GET nguồn, PUT đầu ra, đường dẫn hết hạn trả 403 như
  SeaweedFS) thay cho `MemStore` của Task M1-41 (chưa có trên nhánh này, chạy song song). Task M1-43 dùng `MemStore`.
- Lý do `source_failed`, `internal`; trường `PixelWidth`, `PixelHeight`, `Headers` (Q4, Q5). Một lần `vipsheader` (Q3).
- `docker/compose.dev.yml` có sửa (Q1), dù kế hoạch ghi task này không sửa tệp đó: ca mạng của chính kế hoạch đỏ nếu
  không sửa; điều phối viên giao làm phần dev/compose tương ứng.
- Thêm `make test-imager-compose` (test tag `imagercompose`, chạy trên máy với docker) cho các ca env và mạng; thêm
  ca: không ra internet, chạy bằng 65534 và chỉ đọc, máy gọi được imager qua `imager-port`.
- Ảnh 12 MP thật: máy không có ảnh 12 MP gốc; đo bằng ảnh thật 3700×2467 (9,1 MP, ảnh vải) và chính ảnh đó phóng lên
  4255×2837 (12,1 MP). Ảnh đo không commit (chỉ `big-24mp.jpg` tổng hợp nằm trong `testdata`).
- Cấu trúc: dời `checkLive` của `cmd/crm` sang `observability.CheckLive` (commit riêng, để imager dùng lại; jscpd).

## Việc phát hiện thêm

- **Cho Task M1-43 (worker):** (1) `timeout`, `source_failed`, `upload_failed`, `internal` là lỗi tạm → `not_sent`,
  không phải `rejected` (kế hoạch M1-43 hiện ghi "rejected khác → rejected", sẽ đánh dấu `upload_failed` thành từ chối
  vĩnh viễn). (2) Đường dẫn ký sẵn phải ký với host mà imager tới được: imager chỉ ở mạng `imaging` nên host phải là
  `seaweedfs:8333`, không phải `127.0.0.1:<CRM_S3_PORT>` của máy (SigV4 ký cả host) — worker dev trên máy cần một
  endpoint ký riêng cho imager, hoặc chạy worker trong compose. (3) `PresignPut` của Task M1-41 ký `content-length`,
  mà kích thước biến thể chỉ biết sau khi mã hóa: biến thể cần ký không có `content-length` (hoặc tham số kích thước 0
  nghĩa là không ký), và truyền `PresignedPut.Headers` vào `Output.Headers`.
- **Cho compose production (Task M1-55/staging):** mạng của imager phải `internal: true` (đo Q1: mạng bridge thường
  không chặn tới IP của mạng khác), worker nối vào mạng đó, imager không mở cổng; giữ `read_only`, tmpfs `/tmp`,
  2 GiB, 2 CPU, người dùng 65534, `cap_drop: ALL`, `no-new-privileges`, không biến bí mật. Thêm vào runbook khi có
  compose production.
- Threat model L2 (kiểm host của `SourceURL`/`PutURL` trùng địa chỉ kho) vẫn "Chưa có": imager không có cấu hình địa
  chỉ kho; chặn bằng mạng `internal` (Q1). Có thể thêm biến `IMAGER_ALLOWED_HOSTS` ở task sau nếu cần.
- `cmd/imager`: khi nhận SIGTERM, việc đang chạy bị hủy ngay (context của tín hiệu là `BaseContext`) và trả `timeout`
  (worker chạy lại), thay vì làm xong trong 20 s của `Shutdown`. Không sai dữ liệu; có thể đổi sang
  `context.WithoutCancel` ở task sau nếu muốn tắt êm thật (reviewer ghi chú, không bắt buộc).
- `http.Server` của imager chỉ có `ReadHeaderTimeout`; bên gọi gửi thân chậm giữ chỗ lâu. Bên gọi duy nhất là worker
  trên mạng `internal` nên chỉ ghi lại.
- Sau khi `imaging` thành `internal`, Task M1-40 nên chạy lại `make probe-test` (máy gọi `seaweedfs` qua `CRM_S3_PORT`;
  `seaweedfs` vẫn ở mạng default, cấu hình hai mạng giống `imager-port` đã đạt).
- Phần tốn thêm của cách dòng lệnh vượt 30% ở một số lần đo (xem `docs/do-luong/imager-m1.md`): đề xuất đo lại trên
  máy staging (ít tải) ở Task M1-55 trước khi theo "Xem lại khi" của ADR-45; không tự đổi.

## Reviewer

Kết luận: ĐẠT (commit 1 cấu trúc; commit 2 hành vi)

- Commit 1 (dời `checkLive` → `observability.CheckLive`): ĐẠT, không có phát hiện về tính đúng. Reviewer so từng điểm
  (URL, timeout 3 s, đọc hết body, mã khác 200 là lỗi) với hàm cũ; thông điệp lỗi dài thêm, không test nào so chuỗi;
  không còn chỗ dùng `checkLive`. Ghi chú không bắt buộc: nêu `CheckLive`, `ErrNotLive` ở dòng ký hiệu của mục Ảnh
  hưởng — đã thêm.
- Commit 2 (imager, Dockerfile, compose, test, đo): ĐẠT, không có phát hiện phải sửa. Reviewer soát runner (giết cả
  nhóm chỉ trong `Cancel` nên không giết nhầm nhóm đã cấp lại, quá giờ tách khỏi lỗi giải mã, giới hạn stdout/stderr,
  môi trường tối thiểu), pipeline (không lệnh con nào trước khi nhận loại, kiểm số điểm ảnh trước `vips`, MaxBytes đếm
  đúng cả khi không có Content-Length, dòng tóm tắt neo theo đường dẫn do imager đặt nên chữ trong ảnh không chèn được,
  không chèn tham số, không theo chuyển hướng), server (503 không chờ, giới hạn thân, log không có URL), compose và
  test (không ca nào lặng lẽ bỏ qua trong `make test-imager` nhờ `IMAGER_REQUIRE_VIPS=1`). Ghi chú không bắt buộc: mục
  Ảnh hưởng mâu thuẫn với Q1 về `compose.dev.yml` — đã sửa; tắt êm hủy việc đang chạy, thân chậm giữ chỗ, chạy lại
  `make probe-test` — ghi vào "Việc phát hiện thêm".
