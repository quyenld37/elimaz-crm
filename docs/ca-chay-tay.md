# Ca kiểm thử chạy tay: lệnh và lịch chạy

Tạo ở Task 414 (phát hiện E-04 của `docs/review-doc-lap-m1.md`). `make ci` và CI chạy `go test ./...` không nhãn build,
trên máy có thể không có libvips: các ca dưới đây **không chạy** ở đó (hoặc SKIP mà gói vẫn báo `ok`). Bằng chứng của
TP01 (đường imager thật), TP03 (bom điểm ảnh), TP08, TP09, TP10 và tiêu chí Xong 7 (hết bộ nhớ) đến từ các lần chạy
ghi ở bảng này, không từ `make ci`.

Luật (giữ bằng `tests/deploy/manual_suites_test.go`): mọi nhãn build của tệp `*_test.go` trong repo có đúng một dòng
ở bảng, đủ lệnh, lịch và lần chạy gần nhất; thêm nhãn mới mà không thêm dòng thì `make ci` đỏ. Ca libvips chạy ở CI
mọi lần push (job `imager-vips` của `.github/workflows/ci.yml`, `IMAGER_REQUIRE_VIPS=1`: thiếu công cụ thì đỏ).

Chạy xong một ca thì sửa cột "Lần chạy gần nhất" (ngày, task, kết quả trích). Task M1-55 (task 56, đóng mốc) chạy mọi
dòng có lịch "trước khi đóng mốc" và chép kết quả vào cột Kết quả của `docs/nghiem-thu-m1.md`. Các ca cần bộ compose
riêng: dùng một dự án compose của mình (`COMPOSE_PROJECT=crm-tNN`), không bao giờ `crm-shared` hay `crm`.

| Nhãn | Ca | Lệnh | Lịch | Lần chạy gần nhất |
|---|---|---|---|---|
| `libvips` | Test của `internal/imager`, `cmd/imager` với libvips thật: TP03 bom điểm ảnh, ảnh hỏng, hết bộ nhớ, treo (Xong 7), GPS bị xóa | `make test-imager` (docker, stage `test` của `docker/imager/Dockerfile`) | Mọi lần push (CI `imager-vips`); trên máy khi đổi `internal/imager`, `docker/imager` hay nâng libvips | 21/11, Task 405 (HEAD `e53d001`): `ok internal/imager 13.188s`, `ok cmd/imager 1.026s`, 49 PASS, 0 SKIP ngoài `TestImager_Measure` (đo tay) — gồm `PixelBombRejectedBeforeDecode`, `OutOfMemoryKilled`, `HangKilledWithinDeadline`, `StillAnswersAfterOOM` |
| `imager` | TP01 đường ảnh trọn vẹn với imager thật (`TestFilesPipeline_*`, `tests/e2e/files_pipeline_test.go`) | `go test -tags imager -count=1 -run TestFilesPipeline ./tests/e2e/` (compose của mình, `--profile imager up -d --wait imager imager-port s3-imaging`) | Khi đổi `internal/platform/files` (xử lý ảnh), `internal/imager`, compose của imager; trước khi đóng mốc; trước mỗi lần deploy | 21/11, Task 405 (compose `crm-t405`): `TestFilesPipeline_PublicPhotoGPS` (2.92s), `TestFilesPipeline_PrivateImages` (2.37s) PASS |
| `imagercompose` | Container imager: không biến bí mật, không tới PostgreSQL, không tới cổng nội bộ của SeaweedFS, hệ thống tệp chỉ đọc | `make test-imager-compose COMPOSE_PROJECT=crm-tNN` | Khi đổi `docker/compose*.yml`, `docker/imager`; trước khi đóng mốc | 21/11, Task 405 (`crm-t405`): 6/6 PASS |
| `drill` | TP08 kho tệp tắt đột ngột (`TestFileStoreCrash`), TP09 dựng lại từ kho ngoài có bấm giờ (`TestFileStoreRestore`) | `COMPOSE_PROJECT=crm-tNN scripts/files/crash-drill.sh`; `COMPOSE_PROJECT=crm-tNN scripts/files/restore-drill.sh` | Hằng tháng; khi nâng bản SeaweedFS hay đổi `internal/platform/files` (kho); trước khi đóng mốc | 21/11, Task 405 (`crm-t405`): `FILES CRASH OK uploads=200 completed=119 failed_midway=81 retried_ok=81`, `FILES RESTORE OK restore_seconds=14.2 lost=2 verify_seconds=0.2 files=22`; sau dựng lại biến thể giữ đúng `content-type` (xem `docs/nghiem-thu-m1.md` tiêu chí 6) |
| `filesync` | TP10 chép sang kho ngoài, đối chiếu, kho ngoài không bị ghi đè (`TestFileSync`) | `docker build -f docker/filesync/Dockerfile -t crm-filesync:tNN .` rồi `CRM_FILESYNC_IMAGE=crm-filesync:tNN go test -tags filesync -count=1 -run TestFileSync ./tests/e2e/` | Khi đổi `docker/filesync`, `scripts/filesync`; trước khi đóng mốc; trước mỗi lần deploy | 21/11, Task 405: `--- PASS: TestFileSync (26.18s)` |
| `perf` | Trần thời gian: câu nhận việc ở 100.000 việc chờ (50 ms), đối soát 50.000 số dư (60 s), lệnh treo của imager bị giết (≤ 2 s sau khi chạy, hạn bước 1 s; `ExecRunner` ≤ 1,5 s với hạn 500 ms) | `make perf-test` (máy rảnh) | Khi đổi câu nhận việc của `queue`, đối soát tồn hay cách giết lệnh con của `imager`; trước khi đóng mốc | 19/11, Task 421: `50.000 số dư: 3.6s` PASS; 21/11, Task 440: `TestImager_HangKilledTimeBudget` (1.01s), `TestExecRunner_TimeoutTimeBudget` (0.50s) PASS |
| `probe` | Tính chất của SeaweedFS bản ghim mà ADR-36, ADR-44 dựa vào (kiểm tổng, ghi không đè, hết hạn `crm-tmp`) | `make probe-test` (sau `make dev` trên compose của mình) | Khi nâng bản SeaweedFS; ghi số đo vào `docs/do-luong/seaweedfs-m1.md` | 18/11, Task 40: PASS (xem `docs/tasks/task-40.md`) |
