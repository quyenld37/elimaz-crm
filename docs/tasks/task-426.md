# Task 426 — Trần thử lại ảnh nhỏ hơn hạn sống crm-tmp (M1-r4b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (### Task 426, M1-r4b). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm `config.TmpObjectTTL` (hằng, 24 giờ) và
  `config.DefaultImageRetryMax` (23 giờ); đổi giá trị `files.DefaultRetryMax` 24 → 23 giờ (giờ là
  `config.DefaultImageRetryMax`): 4 chỗ (process.go 2, process_private_test.go 1, test mới). `config.Config.ImageRetryMax`
  giữ tên và kiểu, đổi mặc định và luật kiểm: 1 chỗ dùng (`cmd/crm/main.go:250`) + `m1_test.go`.
- Gói phụ thuộc ngược (go list): `config` ← cmd/crm, httpapi, platform/{logging,metrics,observability,telemetry}, testkit;
  `files` ← cmd/crm, httpapi, modules/{catalog,customer}, files/filestest, testkit. `files` đã nhập `config` gián tiếp
  qua `metrics` nên cạnh mới files → config không tạo vòng; `config` không nhập `files` (files → metrics → config sẽ vòng).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: `go test -race ./internal/platform/config/... ./internal/platform/files/...` và gói phụ thuộc theo
  `ci-task.sh`; ca mới: `TestLoadWorkerImageRetryMax` (sửa: mặc định 23, 23 qua, 24 bằng hạn lỗi, 48 lớn hơn lỗi),
  `TestTmpObjectTTLMatchesBootstrap` (hạn trong `docker/seaweedfs/bootstrap.sh` khớp hằng),
  `TestFileProcess_DefaultCapBeforeTmpExpiry` (ảnh hết trần mặc định trước khi nguồn crm-tmp bị xóa).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md`, grep `TTL|crm-tmp|RetryMax`
  trong internal, cmd, docker, tests: hạn sống crm-tmp chỉ có ở `bootstrap.sh` (`<Days>1</Days>`, fast path suy ra
  ttlSec 86400) và trong chữ (chú thích, RUNBOOK, probe); không có hằng Go nào.
- Dùng lại / viết mới vì: dùng lại `reader.integer` và danh sách `problems` của `config.Load` (thông báo từ chối khởi
  động như mọi biến khác), dùng lại `procEnv.backdate`/`img.down` của test files. Viết mới một hằng `config.TmpObjectTTL`
  làm nguồn Go duy nhất; `files.DefaultRetryMax` lấy từ `config.DefaultImageRetryMax` thay vì viết số lần hai.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.x kiến trúc: hằng ở `internal/platform/config` (gói đọc cấu hình; `files` đã phụ thuộc nó qua `metrics`); không
  gói mới, không biến toàn cục (chỉ hằng), không đọc biến môi trường ngoài config.
- Database, RLS, khóa, transaction, tiền, quyền, hàng đợi đồng thời (6.x): không áp dụng — chỉ đổi giá trị mặc định
  và kiểm cấu hình lúc khởi động; luật thử lại của `Processor.retryLater` không đổi.
- Lỗi: từ chối khởi động qua `config.ErrInvalid` (đã có), thông báo nêu tên biến, giá trị và hạn crm-tmp; không log.
- Dữ liệu cá nhân: không chạm.
- Vận hành: đổi mặc định 24 → 23 giờ có hiệu lực khi worker khởi động lại; máy nào đang khai `IMAGE_RETRY_MAX_HOURS`
  ≥ 24 sẽ không khởi động được (cố ý, ghi RUNBOOK). Kiểm `docker/compose.prod.env.example`, compose: không khai biến này.

## Quyết định thay chủ shop

- Nguồn chung của hạn sống crm-tmp: hằng Go `config.TmpObjectTTL` + test đọc `docker/seaweedfs/bootstrap.sh` đòi
  `<Expiration><Days>` bằng đúng hằng (sai lệch là test đỏ). Vì: hạn thật do bootstrap.sh đặt lên kho (fast path lấy
  từ quy tắc Days), không phải biến môi trường; thêm biến `S3_TMP_TTL_HOURS` sẽ là một nút có thể nói sai so với kho.
  Không đổi `docker/`. Phương án khác: một tệp chung (vd. `docker/seaweedfs/tmp-ttl-days`) cả shell và Go cùng đọc —
  cần đổi bootstrap.sh và nhúng tệp ngoài gói, bỏ vì chạm hạ tầng; dễ đổi sau.
- Khoảng an toàn 1 giờ (mặc định 23 giờ, như kế hoạch gợi ý): trần tính từ lúc tệp vào processing, đường PUT ký sẵn
  hạn 15 phút, nên còn ≥ 45 phút dư cho luồng thường. Trần ≥ hạn sống → từ chối khởi động (giữ khoảng 1–168 cũ cho
  thông báo "ngoài khoảng", thêm luật "phải nhỏ hơn hạn crm-tmp").

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mặc định nhỏ hơn hạn sống (23 giờ) | `TestLoadWorkerImageRetryMax` | `go test -race -count=1 ./internal/platform/config/ -run ImageRetryMax` | trước sửa: `m1_test.go:225: mặc định: 24h0m0s <nil>` FAIL; sau: PASS |
| Trần hợp lệ (1, 23) nhận; bằng hạn (24) và lớn hơn (25, 48, 168) từ chối khởi động: lỗi `ErrInvalid` nêu `IMAGE_RETRY_MAX_HOURS` và `crm-tmp` | `TestLoadWorkerImageRetryMax` | như trên | PASS |
| Hạn sống có một nguồn, hai nơi lệch là đỏ | `TestTmpObjectTTLMatchesBootstrap` | `go test -count=1 ./internal/platform/config/ -run TmpObjectTTL` | PASS; đổi tạm hằng thành 48 giờ → FAIL (đã trả lại) |
| Ảnh hết trần mặc định trước khi nguồn bị xóa | `TestFileProcess_DefaultCapBeforeTmpExpiry` (imager hỏng mãi, lùi mốc tới `TmpObjectTTL − 30 phút`, trần mặc định) | `go test -race -count=1 ./internal/platform/files/ -run DefaultCapBeforeTmpExpiry` | trước sửa: `process_retry_test.go:393: … muốn rejected processing_failed, có {Status:processing …}` FAIL; sau: PASS (cả gói `ok …/files 86.215s`) |
| ci-task.sh xanh | — | `ci-task.sh` sau khi gộp main 60839fb | build, vet, lint (0 issues), catalog, jscpd, test-guard xanh; test: config, files, cmd/crm và các gói phụ thuộc ok, **trừ** 2 lỗi không do task này: `TestM2Routes_Unimplemented` (httpapi giống hệt main, xem Việc phát hiện thêm) và `TestLintCatchesPlantedMistakes` (`parallel golangci-lint is running`, khóa của phiên khác; hook Stop chạy lại `tests/arch`: ok 36.698s) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- **Lỗi có sẵn trên main, không sửa ở task này:** `internal/httpapi/errors_m2_test.go` `TestM2Routes_Unimplemented`
  lấy `GET /api/v1/orders/{id}/timeline` làm route "chưa làm" (mong 501), mà Task 81 (M2-24) đã làm route đó → nay 404.
  `git diff main -- internal/httpapi` rỗng: lỗi giống hệt trên main. Cần chọn route M2 khác còn chưa làm (điều phối
  viên / lượt sửa của Task 81).
- Trần tính từ lúc tệp vào `processing` (complete), hạn crm-tmp tính từ lúc PUT. Nếu khách PUT xong mà gọi complete
  muộn hơn khoảng 1 giờ, lượt cuối vẫn có thể ra `source_missing`. Dư 1 giờ đủ cho luồng thường (màn hình gọi complete
  ngay sau PUT); muốn chặn hẳn thì tính trần từ `created_at` của tệp — việc riêng, chưa làm.
- `tests/probe/tmp_expiry_test.go` còn viết số 86400 / `1d` riêng (probe đo kho thật, chạy bằng `make probe-test`);
  có thể đổi sang `config.TmpObjectTTL` ở một task probe sau. Hạn trong bootstrap.sh đã có test đối chiếu ở task này.

## Reviewer

Kết luận: ĐẠT (lượt 1, 19/11/2026). Phát hiện phải sửa: không có. Reviewer đối chiếu đủ 4 yêu cầu của kế hoạch
(mặc định 23 giờ, từ chối khởi động khi trần ≥ hạn sống với ca bằng 24, một nguồn hạn sống có test canh bootstrap.sh,
RUNBOOK), đúng 4 chỗ dùng `files.DefaultRetryMax`, cạnh files → config không tạo vòng; xác nhận HEAD trùng main ở
`internal/httpapi` nên `TestM2Routes_Unimplemented` đỏ như trên main. Ghi chú không bắt buộc: cận trên 168 nay thừa
(mọi giá trị ≥ 24 đã bị chặn) — giữ, không ảnh hưởng tính đúng.
