# Task 414 — Gia cố: GRANT cột số dư, imager không liên kết db/config, lịch chạy ca chạy tay, L1/L3 threat model (M1-r9)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task bù review độc lập M1, dòng 414 của `danh-so-task-toan-cuc.md`; phạm vi A-02, C-05, E-04, E-05 trong `docs/review-doc-lap-m1.md`). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - C-05 (commit cấu trúc): `observability.CheckLive`, `observability.ErrNotLive` dời sang gói con mới `internal/platform/observability/liveprobe` (`liveprobe.CheckLive`, `liveprobe.ErrNotLive`; chỉ thư viện chuẩn). Chỗ dùng: `cmd/crm/main.go:533`, `cmd/imager/main.go:187`, `observability/probe_test.go` (dời theo). `logging.NewLogger(cfg config.Config, stdout)` đổi thành `logging.NewLogger(logDir, role string, stdout)` để `logging` thôi nhập `config`: 6 chỗ gọi — `cmd/crm/main.go:102`, `internal/testkit/personaldata.go:45`, `logging/scrub_test.go:140`, `logging/dailyfile_test.go:149,165`, `logging/redact_test.go:121`.
  - E-05/L1 (commit hành vi): trong `internal/platform/files` chỉ đổi mã không xuất: `variantsPresent` thành `checkVariants` (Head từng biến thể, kiểm `content-type` = `image/<định dạng>`; kiểu `variantsState`), hàm mới `finishVariants` (phần cuối của `makeVariants`: kiểm rồi ghi bản kê), hằng mới `reasonOutputInvalid = "output_invalid"`. Đường dùng `makeVariants`: `file.process`, `file.process_private`, `file.regenerate` (`regenerate.go:163`).
  - Test phụ trợ: `files_test.putObj` (process_test.go) ghi content-type theo đuôi khóa (`image/avif`…) thay vì luôn `image/jpeg` — biến thể giả phải giống imager thật, không thì `TestFileProcess_SkipsExistingVariants` gặp luật L1; thêm `putObjType`. `httpapi.newFilesAPIEnv` gọi `newFilesAPIEnvLog(t, logger)` mới. `tests/arch`: `listedPackage.Deps`, `imagerDepViolation`; testdata thêm `internal/imager/viahealth`, `internal/platform/health`.
- Gói phụ thuộc ngược (go list): `liveprobe` ← `cmd/crm`, `cmd/imager`. `logging` có 22 gói nhập, nhưng chỉ 2 gói gọi `NewLogger` (`cmd/crm`, `internal/testkit`); testkit đổi → cổng kiểm của task chạy toàn bộ test. `files` ← `cmd/crm`, `internal/httpapi`, `internal/modules/catalog`, `internal/testkit`, `tests/*` (chữ ký không đổi).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. Hành vi: tệp ảnh có biến thể sai `content-type` → `rejected` lý do `output_invalid` thay vì `ready` (chỉ xảy ra khi imager hoặc kho trả sai; đường thường không đổi). oasdiff: không chênh.
- Chỗ dùng trên màn hình: không sửa console. `imageRules.ts` `rejectText`: lý do lạ → câu chung "xử lý không xong" (đã có), nên `output_invalid` hiện câu chung.
- Câu sqlc, bảng, pha migration: không đổi câu sqlc. Migration mới `0132_inventory_insert_columns.sql` (A-02): `REVOKE INSERT ON inventory.stock_balances, inventory.stock_reservations FROM crm_app` rồi `GRANT INSERT (tenant_id, variant_id, warehouse_id, seller_id)` trên số dư và `GRANT INSERT (id, tenant_id, order_item_id, variant_id, warehouse_id, quantity, expires_at)` trên giữ hàng. Pha: thu hẹp quyền, nhưng mã đang chạy (`EnsureBalance`, `InsertReservation`) chỉ chèn đúng các cột này nên tương thích ngược (bản cũ chạy trên schema mới vẫn đúng). Câu chèn bằng chủ bảng trong test (`fixture_test.go`, `reconcile_test.go`) không đổi.
- Không phải mã Go: `.github/workflows/ci.yml` thêm job `imager-vips` chạy `make test-imager` (E-04); tài liệu mới `docs/ca-chay-tay.md` (lịch các ca chạy tay); `docs/threat-model-m1.md` cột "Task / test" của L1, L3; `docs/nghiem-thu-m1.md` trỏ tới lịch; `docs/danh-muc-dung-chung.md` sinh lại (`make catalog`).
- Test sẽ chạy: `go test -race ./internal/modules/inventory/ -run 'TestBalances_InsertOnlyKeyColumns|TestLedger'`, `./internal/platform/files/...`, `./internal/httpapi/ -run TestFilesAPI`, `./internal/platform/logging/...`, `./internal/platform/observability/...`, `./cmd/...`, `./tests/arch/...`, `./tests/deploy/...`; rồi cổng kiểm `ci-task.sh` một lần (đổi migration và testkit → toàn bộ test).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `observability` (CheckLive), `logging` (NewLogger, Err; CaptureLog của testkit), `files` (FileStore.Head, ObjectInfo.ContentType), `testkit` (CaptureLog, CaptureSpans, SpanText, MemFileStore); tìm `CheckLive`, `variantsPresent`, `X-Amz-Signature`, `GRANT INSERT`, `requireTools`, `go list -deps`.
- Dùng lại / viết mới vì: C-05 dời nguyên `CheckLive` (không viết bản thứ hai trong `cmd/imager`, tránh hai bản healthcheck); luật (g) dùng lại `imagerForbidden` của `tests/arch`, chỉ thêm đầu vào là `Deps` (bắc cầu) bên cạnh `Imports`. L1 dùng lại `FileStore.Head` (đã gọi cho từng khóa) — gộp kiểm "có" và kiểm loại thành một hàm, xóa `variantsPresent`. L3 dùng lại `testkit.CaptureLog`, `CaptureSpans`, `SpanText` như test dữ liệu cá nhân của khách. A-02 theo mẫu GRANT theo cột đã có ở 0115 (UPDATE), test lỗi quyền theo `TestLedger_AppendOnly` (`pgCode`). E-04 dùng lại đích `make test-imager` và stage `test` của `docker/imager/Dockerfile` có sẵn; test luật CI theo khuôn `tests/deploy/ci_test.go`.

## Soát thiết kế

Mục 5 (khớp kiến trúc):
- 5.1, 5.2, 5.3: A-02 chỉ đổi quyền trên bảng của `inventory`; L1 chỉ trong `platform/files`. Không đọc, ghi bảng module khác.
- 5.4, 5.5: không áp dụng: không thêm việc nguyên tử hay sự kiện; L1 kiểm kho ngoài transaction như các lần `Head` sẵn có, từ chối qua `p.reject` (transaction ngắn sẵn có).
- 5.6: không áp dụng: không gọi ra ngoài mới (Head tới kho tệp nội bộ như cũ).
- 5.7: không thêm truy vấn; quyền INSERT theo cột không đụng RLS.
- 5.8: không áp dụng: không đổi lớp HTTP.
- 5.9: không thêm đường ghi.
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới (`liveprobe` chỉ thư viện chuẩn).
- 5.12: gói con `observability/liveprobe` theo luật "mã chung ở `internal/platform/<việc>`", tên danh từ.

Mục 6 (đúng khi chạy đồng thời):
- 6.1, 6.2, 6.3, 6.4, 6.5: không áp dụng: không khóa mới, không đổi câu ghi; REVOKE/GRANT trong migration chạy một lần.
- 6.6: lệnh `file.process` gặp biến thể sai loại → `rejected` (lỗi vĩnh viễn, thử lại không chữa được); lỗi Head khác "không có" → `not_sent` như cũ.
- 6.7, 6.8, 6.9, 6.10, 6.11: không áp dụng: không bên ngoài, không tiền; số dư/giữ hàng chỉ đổi quyền INSERT (quy luật tồn giữ nguyên, thêm lớp chặn ở DB).
- 6.12: L3: test log và span của `createUpload`, `complete`, `file.process` (ảnh tốt, imager từ chối, imager lỗi 500) không chứa chữ ký của đường dẫn ký sẵn; cột `platform.jobs.result`, `last_error` cũng không.
- 6.13: A-02 thu hẹp quyền của `crm_app` (mặc định từ chối các cột còn lại); test 42501.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| A-02: crm_app chèn số dư có `on_hand`/`held`/`version`, giữ hàng có `state`/`state_changed_at` → 42501; EnsureBalance, InsertReservation vẫn chạy | `TestBalances_InsertOnlyKeyColumns` | `go test -race -count=1 -run 'TestBalances_InsertOnlyKeyColumns\|TestLedger\|TestReserve' ./internal/modules/inventory/` | Trước migration: FAIL `số dư on_hand bằng crm_app: mong lỗi quyền 42501, có <nil>` và số dư thành `OnHand:1003` (1.000 không có sổ). Sau 0132: `ok … 16.501s` |
| C-05: luật (g) xét cả phụ thuộc gián tiếp của gói imager; tệp chạy imager không liên kết config, db, pgx | `TestBoundariesTestdata` (ca mới `internal/imager/viahealth` → db qua `internal/platform/health`; `cmd/imager` → pgx qua `internal/imager`), `TestBoundariesRealCode` | `go test -count=1 -run TestBoundaries ./tests/arch/` | Trước sửa: FAIL `cmd/imager phụ thuộc gián tiếp elimaz.com.vn/crm/internal/platform/config`, `…/internal/platform/db`, `github.com/jackc/pgx/v5` (+12 gói pgx). Sau dời `CheckLive` và đổi `NewLogger`: ok; `go list -deps ./cmd/imager ./internal/imager \| grep -E "elimaz\|pgx"` → chỉ `internal/imager`, `internal/platform/logging`, `internal/platform/observability/liveprobe`, `cmd/imager` |
| C-05: hành vi giữ nguyên sau khi dời | `TestCheckLive` (gói `liveprobe`), test của `logging` (`NewLogger` vai trò api, tệp theo ngày của worker), `cmd/crm`, `cmd/imager` | `go test -race -count=1 ./internal/platform/logging/... ./internal/platform/observability/... ./cmd/...` | ok cả 6 gói |
| E-04: mọi nhãn build của test có lệnh, lịch, lần chạy gần nhất trong `docs/ca-chay-tay.md`; ca libvips chạy ở CI mọi lần push | `TestManualSuites_EveryBuildTagScheduled`, `TestCI_ImagerVipsJob` | `go test -count=1 -run 'TestManualSuites\|TestCI_' ./tests/deploy/` | Trước: FAIL `đọc ../../docs/ca-chay-tay.md: no such file`, `ci.yml thiếu job "imager-vips"`. Sau: ok |
| E-04: ca libvips (TP03 bom điểm ảnh, hết bộ nhớ Xong 7) chạy thật, thiếu công cụ thì đỏ | toàn bộ `internal/imager`, `cmd/imager` | `make test-imager` (docker, `IMAGER_REQUIRE_VIPS=1`) | `ok elimaz.com.vn/crm/internal/imager 18.251s`, `ok elimaz.com.vn/crm/cmd/imager 1.030s`, exit 0 (19/11) |
| E-05/L1: biến thể trên kho sai `content-type` → tệp `rejected output_invalid`, 0 biến thể công bố, không bản kê; cả đường bản kê có sẵn (không gọi imager) | `TestFileProcess_RejectsVariantWithWrongType` | `go test -race -count=1 -run 'TestFileProcess_RejectsVariantWithWrongType\|TestFileProcess_SkipsExistingVariants' ./internal/platform/files/` | Trước sửa: FAIL `tệp: {Status:ready …}`. Sau: ok; cả gói `files` ok (84 s) |
| E-05/L3: log, span, `platform.jobs.result`/`last_error` của `file.process` (ảnh tốt, imager từ chối, imager 500) không chứa chữ ký đường dẫn ký sẵn | `TestFileProcess_SignedURLsNotLogged` | `go test -race -count=1 -run TestFileProcess_SignedURLsNotLogged ./internal/platform/files/` | ok (không có rò rỉ, đúng như review). Kiểm bộ phát hiện: tạm thêm `logger.Info("thu", "u", SourceURL)` → FAIL `log, span hoặc lệnh chứa chữ ký đường dẫn ký sẵn "098d9e…"`; đã gỡ |
| E-05/L3: log, span của createUpload, complete (trước/sau tải), getFile không chứa chữ ký, query của putUrl | `TestFilesAPI_SignedURLNotLogged` | `go test -race -count=1 -run TestFilesAPI ./internal/httpapi/` | ok (`3.583s`); test đòi log/span có `/api/v1/files` để chắc chỗ quét có dữ liệu |
| Cổng kiểm của task | — | `ci-task.sh` (chạy nền, sau khi gộp main 0712b68) | build, vet, lint (0 issues), sqlc-vet, jscpd (0 clones), gitleaks, oasdiff, test-guard: qua. Toàn bộ `go test -race ./...` (đổi migration, testkit): mọi gói ok (499 s), `tests/deploy` ok. gen-check, catalog-check đỏ chỉ vì lúc chạy chưa stage (so với index); sau `git add`: `make gen` + `git diff --exit-code` và `make catalog` + `git diff --exit-code` → không chênh |

## Quyết định thay chủ shop

- **C-05 — dời `CheckLive` sang gói con `observability/liveprobe`, và `logging.NewLogger` nhận `logDir, role` thay vì `config.Config`.** Ngoài `observability`, `cmd/imager` còn kéo `config` qua `logging` (review chỉ nêu `observability`). Chọn bỏ cả hai đường để luật (g) đúng ở mức tệp chạy, và cho luật (g) xét `Deps` (bắc cầu). Phương án khác: viết healthcheck riêng trong `cmd/imager` (hai bản một hàm), hay tách phần lọc lỗi của `logging` ra gói khác (đổi 22 gói nhập) — bỏ vì to hơn. Đảo ngược: đổi lại import.
- **A-02 — migration 0132 thu hẹp INSERT theo cột** đúng đề xuất của review. Ghi dòng `-- contract:` dù REVOKE không nằm trong danh sách lệnh bắt buộc, để người đọc thấy đây là thu hẹp có kiểm tương thích.
- **E-04 — "lịch chạy" là: job CI `imager-vips` (mọi lần push) cho ca libvips + bảng `docs/ca-chay-tay.md` (lệnh, lịch, lần chạy gần nhất) cho ca cần compose riêng, giữ bằng test.** Không đưa diễn tập (giết container, xóa volume), filesync, e2e imager vào CI: cần bộ compose đầy đủ trên máy chạy CI, chưa chạy thử được trên GitHub (CI chưa bật, cần chủ shop OK tạo repo), và diễn tập phá hỏng thật. Phương án khác: job CI theo lịch tuần cho drill/filesync — để sau khi CI chạy thật trên GitHub. Đảo ngược: xóa job, xóa tệp và test.
- **L1 — kiểm `content-type` mọi biến thể trước `ready` (cả biến thể có sẵn, cả khi tin bản kê); sai → `rejected output_invalid`.** Lý do mới không có câu riêng trên màn (`rejectText` hiện câu chung "xử lý không xong"), nên không đổi màn; chỉ xảy ra khi imager hay kho trả sai. Chỉ kiểm `content-type` (đúng đề xuất của threat model), không đọc byte đầu (magic) của từng biến thể: imager giả của test ghi byte giả, và `content-type` nằm trong chữ ký PUT. Biến thể sai vẫn nằm trên kho ở khóa chưa công bố (không có dòng biến thể, không bản kê). Đường dựng lại tệp mất (Task M1-46): bản chép về từ kho ngoài giữ hoặc đoán `content-type` theo đuôi `.avif/.webp/.jpeg` nên vẫn qua.
- **L3 — chỉ thêm test** (review: thiếu test, không rò rỉ). So bằng chữ ký thật của từng đường dẫn (MemStore `sig`; S3 `X-Amz-Signature`) thay vì chỉ tìm chuỗi `X-Amz-Signature`, vì test chạy với MemStore.

## Dừng để hỏi

- **L1, phần header `X-Content-Type-Options: nosniff` cho bucket công khai** (`crm-public` qua CDN): cần quy tắc Cloudflare (việc ngoài máy) hoặc đổi cấu hình SeaweedFS/compose (hạ tầng, ngoài phạm vi phiên này). Chưa làm; threat model ghi "Chưa có — chờ chủ shop". Phần kiểm đầu ra của L1 đã làm.
- **Job CI `imager-vips` chưa chạy trên GitHub** (CI cả repo chưa bật, cần OK của chủ shop). Đã chạy đúng lệnh của job trên máy (`make test-imager`: ok).

## Test bị xóa hoặc tắt

(không có test nào bị xóa, tắt hay nới.) Sửa dữ liệu giả: `putObj` (process_test.go) ghi `content-type` theo đuôi khóa thay vì luôn `image/jpeg` — biến thể `.avif`/`.webp` có sẵn trong `TestFileProcess_SkipsExistingVariants` trước đây mang `image/jpeg`, điều imager thật không bao giờ ghi; điều test kiểm giữ nguyên.

## Khác kế hoạch

- C-05 sửa thêm `logging.NewLogger` (review chỉ nêu `observability`): `logging` cũng kéo `config` vào tệp chạy imager.

## Việc phát hiện thêm

- Chạy lại theo lịch `docs/ca-chay-tay.md` sau L1 (đổi `internal/platform/files`): `go test -tags imager -run TestFilesPipeline ./tests/e2e/` (TP01, đường thường vẫn `ready` với imager và SeaweedFS thật) và `scripts/files/restore-drill.sh` (TP09: bản chép về từ kho ngoài phải giữ `content-type` đúng, không thì tệp dựng lại thành `rejected output_invalid`). Cần bộ compose riêng của một dự án (không chạy trên `crm-shared`), nên không làm trong phiên này; Task M1-55 (task 56) chạy trước khi đóng mốc. Đã kiểm gián tiếp: S3Store giữ `content-type` đã ký (`s3store_test.go`), rclone đoán `content-type` theo đuôi `.avif/.webp/.jpeg` khi không giữ được siêu dữ liệu.

## Reviewer

- Commit 1 (cấu trúc, C-05: `liveprobe`, `NewLogger(logDir, role, …)`, luật (g) bắc cầu): ĐẠT, không có phát hiện về tính đúng. Reviewer chạy lại `go list -deps ./cmd/imager` (chỉ còn `internal/imager`, `logging`, `liveprobe`), `go list -e` trên testdata (Deps của `viahealth` có `platform/db`, của `cmd/imager` có pgx — đúng hai dòng mong mới), grep không còn `observability.CheckLive` hay `NewLogger(cfg`, đúng 6 chỗ gọi `NewLogger`. Ghi chú không bắt buộc: danh mục dùng chung chỉ liệt kê gói cấp đầu nên `CheckLive` không còn hiện trong danh mục (giữ: cùng cách với `alertjobs`).
- Commit 2 (hành vi: A-02 migration 0132, E-04 job `imager-vips` + `docs/ca-chay-tay.md`, L1 `checkVariants`/`finishVariants`, L3 hai test): ĐẠT, không có phát hiện về tính đúng. Reviewer soát riêng `file.regenerate` (tệp ready giữ ready, lệnh `rejected output_invalid` như khi imager từ chối), đường bản kê và dựng lại tệp mất, kho S3 thật giữ `content-type` đã ký (`s3store_test.go:224-239`), mọi câu chèn bằng crm_app (đúng 4 và 7 cột), goose cho phép số migration chen (0132 < 02xx). Ghi chú không bắt buộc: (1) kiểm "có dữ liệu" của `TestFileProcess_SignedURLsNotLogged` có thể đạt chỉ nhờ log — giữ, vì cả ba ảnh thuộc tenant A nên phần quét `platform.jobs` có dòng; (2) nên chạy lại TP01 e2e và `restore-drill.sh` với imager, SeaweedFS thật sau L1 — ghi ở "Việc phát hiện thêm".
