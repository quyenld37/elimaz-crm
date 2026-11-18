# Task 46 — Diễn tập kho tệp — tắt đột ngột, dựng lại từ bản sao, đánh dấu tệp mất (M1-46)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 46, M1-46; TP08, TP09, TP12). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký cũ. Gói `files`: `VerifyKind`,
  `NewVerifyHandler`, `Verifier` (commands.Handler + Looker + DoneHook), `EnqueueVerify`, `ListLost`, `LostFile`,
  `LostCursor`. Gói `testkit` (chỉ test): `StubImager` (imager giả của `files/process_helpers_test.go` gọi nó cho ca
  ảnh tốt). `Processor.variantsPresent` (riêng, sau review).
  Gói `queries`: `FileProducts`, `FileProduct`. `cmd/crm`: worker nối thêm `files.NewVerifyHandler`. `Processor.Execute`/`OnDone` (không đổi chữ ký, 0 chỗ gọi trực tiếp
  ngoài gói: worker gọi qua commands.Runner) đổi hành vi cho tệp `lost`: nguồn là `orig/<sha>`; tệp ảnh công khai vừa
  `ready` xếp `file.process` cho tệp `lost` cùng tenant, cùng nội dung.
- Gói phụ thuộc ngược (go list): `files` ← `cmd/crm`, `internal/httpapi`, `internal/modules/catalog`,
  `files/filestest`, `internal/testkit`; `queries` ← `internal/httpapi`.
- Operation OpenAPI bị chạm, oasdiff: `verifyFiles`, `listLostFiles` đã có trong `openapi.yaml` (Task 331 viết hợp
  đồng, máy chủ đang trả 501); task này hiện thực máy chủ, **không sửa** `openapi.yaml` nên oasdiff không đổi.
- Chỗ dùng trên màn hình: `console/src/screens/lost-files/LostFilesPage.tsx` gọi `listLostFiles`, `verifyFiles`; giữ
  đúng hợp đồng đang có, không đổi console.
- Câu sqlc, bảng, pha migration: thêm câu trong `queries/platform/files.sql` (`ListReadyFilesAfter`,
  `ListVariantKeysOf`, `ListLostSameContent`, `ListLostFilesPage`); dùng lại `LockFile`, `SetFileLost`, `SetFileReady`.
  Bảng `platform.files`, `platform.file_variants` (đọc; ghi `status`, `lost_at` bằng câu sẵn có), `platform.jobs` (xếp
  lệnh), `identity.audit_logs`, `platform.file_sync_runs` (dòng `restore_drill` do bài diễn tập ghi). Câu đọc của
  `internal/queries` qua `catalog.product_images_v`, `catalog.products_v`. **Không migration** (trạng thái `lost`, cột
  `lost_at`, `kind = 'restore_drill'` đã có từ 0124, 0126).
- Test sẽ chạy: `go test -race` của `internal/platform/files/...`, `internal/httpapi/...`, `internal/queries/...`,
  `cmd/crm/...` (gói đổi + gói nhập); `go test -tags drill ./tests/drills/` (TP08, TP09 trên bộ Docker crm-t46); cổng
  kiểm ci-task một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (files, queue, commands,
  queries); `files.Regenerator` (mẫu lệnh có OnDone), `Processor.publish`, `queries.Cursor`/`ParseCursor` (con trỏ
  thời điểm + id), `store.LockFile`/`SetFileLost` (đã có, Task 44 dùng trong filestest), `filestest.Image`,
  `testkit.S3FileStore`/`S3Config(S3Admin)`, `scripts/filesync/lib.sh` (`fs_init`, `src_of`/`dst_of`),
  `scripts/backup/drill-lib.sh` (xem, không hợp: dành cho compose diễn tập sao lưu PostgreSQL).
- Dùng lại / viết mới vì: dùng lại `LockFile`, `SetFileLost`, `SetFileReady` (đã cho phép từ `lost`), đường
  `file.process` (tệp `lost` đã `processable`), `queries.ParseCursor`/`Cursor` cho con trỏ của `listLostFiles`, `lib.sh`
  của filesync cho bước chép ngược (`scripts/filesync/restore.sh` mới chỉ đảo chiều `rclone copy`). Viết mới `Verifier`
  (chưa có lệnh đối chiếu kho nào), `FileProducts` (chưa có câu tìm mẫu theo mã tệp), imager giả của bài diễn tập (imager
  giả của `files_test` nằm trong gói test, không nhập được từ `tests/drills`).

## Soát thiết kế

- 5.1 Chủ dữ liệu: trạng thái tệp thuộc `platform` (gói files); mẫu và ảnh thuộc catalog, chỉ đọc qua view.
- 5.2 Không ghi bảng module khác: files chỉ ghi `platform.files`, `platform.jobs`; không ghi catalog.
- 5.3 Đọc qua view: `queries.FileProducts` đọc `catalog.product_images_v` + `catalog.products_v`.
- 5.4 Nguyên tử: đặt `lost` cho cả lô và xếp lệnh lô sau trong một transaction (OnDone của lệnh).
- 5.5 Không nguyên tử: Head kho tệp chạy trong `Execute` ngoài transaction (ADR-46); phục hồi tệp `lost` đi qua lệnh `file.process`.
- 5.6 Gọi ra ngoài: kho tệp là dịch vụ ngoài; `file.verify` chỉ đọc (Head) nên `AutoRetryNotSent = true`, Lookup trả not_sent.
- 5.7 Ngữ cảnh shop: lệnh có TenantID, Runner chạy OnDone trong ngữ cảnh tenant; Execute đọc trong `tenancy.WithTenant`.
- 5.8 Không HTTP trong module: files, queries không nhập net/http; handler mỏng.
- 5.9 Đường ghi: `verifyFiles` có `IdempotencyKey`, `x-permission: platform.files.manage` (đã có); ghi audit.
- 5.10 Valkey: không áp dụng (không dùng).
- 5.11 Thư viện: không thêm (Docker, rclone, SeaweedFS đã có; test diễn tập dùng thư viện chuẩn + aws-sdk đã có).
- 5.12 Module mẫu: lệnh theo mẫu `Regenerator`; đường ghi theo `RegenerateImages` (WriteTx + audit).
- 6.1 Đồng thời: OnDone khóa từng tệp (`LockFile`, id tăng dần) và chỉ đặt `lost` khi tệp còn `ready`, chưa xóa, **cùng
  version** lúc Execute thấy thiếu (tệp được xử lý lại giữa chừng không bị đánh dấu nhầm).
- 6.2 Gửi lại: Idempotency-Key ở `verifyFiles`; lệnh lô sau có DedupeKey `<run>:<id cuối>` nên OnDone chạy lại không
  xếp trùng; lệnh phục hồi có DedupeKey `<tệp>:restore:<lost_at>`.
- 6.3 Thứ tự khóa: ADR-46 nhóm 12b (`platform.files` theo id tăng dần) rồi chèn `platform.jobs`; Processor.OnDone giữ
  khóa tệp của chính nó, không khóa tệp `lost` khác (chỉ xếp lệnh).
- 6.4 Lỗi tạm DB: Execute đọc lỗi → not_sent (thử lại); OnDone trong transaction của Runner.
- 6.5 Cách ly: READ COMMITTED + khóa dòng + so version.
- 6.6 Việc nền lỗi: Head lỗi (không phải "không có") → not_sent, **không** đánh dấu `lost`; hết lượt → dead, cảnh báo jobs.
- 6.7 Quá tải: không áp dụng (kho nội bộ); Head song song có trần (8).
- 6.8 Chết giữa chừng: lệnh in_doubt → Looker trả not_sent → chạy lại (chỉ đọc, an toàn).
- 6.9 Treo: mỗi Execute ≤ 30 s (hạn của loại); một lượt dừng ở 500 tệp hoặc khi quá 20 s, phần còn lại sang lệnh lô sau.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: test bảng cho các loại tệp (ảnh công khai, ảnh riêng tư, tệp máy chủ ghi, tệp tạm) và ca tệp đổi
  version giữa Execute và OnDone. Không thêm rapid: không có quy luật số học mới.
- 6.12 Dữ liệu cá nhân: kết quả lệnh chỉ có mã tệp, version; log diễn tập chỉ số đếm.
- 6.13 Quyền: `platform.files.manage` cho cả hai đường; test 403 với người có `catalog.read` / `catalog.manage` mà không có `platform.files.manage`.

## Quyết định thay chủ shop

- Q1 — "Tải lại ảnh đó → ready" đi đường nào: khi một ảnh công khai vừa `ready`, xếp `file.process` cho mọi tệp `lost`
  cùng tenant, cùng nội dung (sha256); với tệp `lost`, `file.process` đọc nguồn từ ảnh gốc `orig/<sha>` (khóa tải lên
  trong crm-tmp đã hết hạn) rồi đi đúng đường cũ (tìm tệp trùng → chép biến thể, hoặc gọi imager). Vì sao: đúng câu
  của kế hoạch "tệp lost sau đó có lại đối tượng (tải lại) → file.process đưa về ready", nhân viên chỉ cần tải lại ảnh
  gốc như bình thường, mẫu tự hiện lại ảnh cũ; không đổi console. Khác: nút "tải lại vào đúng tệp này" (đổi hợp đồng
  API, màn hình: ngoài phạm vi), hoặc để `file.verify` phục hồi (phải chạy lại đối chiếu mới thấy). Dễ đảo: bỏ một lời
  gọi `enqueueRestore`.
- Q2 — Một lượt đối chiếu là một chuỗi lệnh, không phải một lệnh: mỗi lệnh `file.verify` kiểm một lô (≤ 500 tệp và
  ≤ 20 s), OnDone xếp lô sau (cùng `runId`). Vì sao: hạn một lần Execute là 30 s, giữ chỗ 90 s (spec 04 mục 5.1); kho
  vài chục nghìn ảnh × 13 đối tượng không xong trong một lần. `verifyFiles` trả `jobId` của lô đầu (hợp đồng đã có).
- Q3 — `listLostFiles` lấy mẫu của mọi người bán trong tenant (không lọc theo người bán): đường này là việc quản trị kho
  tệp (`platform.files.manage`), tệp không thuộc người bán. Một tệp gắn nhiều mẫu → mẫu chưa xóa, mã nhỏ nhất.
- Q4 — Đối chiếu bỏ qua tệp tạm (bucket crm-tmp hay có `expires_at`: hết hạn là bị xóa đúng luật) và ảnh riêng tư chỉ
  kiểm biến thể (R10: không giữ ảnh gốc). Head lỗi khác "không có" → thử lại, không bao giờ đánh dấu mất.
- Q5 — Bài diễn tập dùng imager giả (`testkit.StubImager`, ghi biến thể giả qua đúng PutURL) thay cho container imager:
  diễn tập đo kho tệp và đường chép, không đo giải mã ảnh (đã có `tests/e2e/files_pipeline_test.go`, nhãn imager).
  Máy chủ thử của bài diễn tập nới giới hạn tần suất (một nhân viên gửi ~600 yêu cầu trong vài giây).
- Q6 — Chép ngược bằng script mới `scripts/filesync/restore.sh` chạy trong container filesync với khóa crm-admin cho kho
  chính (crm-filesync chỉ đọc kho chính). Dòng `file_sync_runs(kind='restore_drill')` do bài diễn tập ghi (bucket
  `all`), không do restore.sh (dựng lại thật không phải diễn tập).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TP12: đúng 2 lost, listLostFiles 2 dòng kèm productCode, getProduct 200 với ảnh lost, tải lại → ready; 403; phân trang; con trỏ hỏng 400; audit; gửi lại cùng khóa | `TestVerify_MarksLost` (internal/httpapi) | `go test -race -count=1 -run 'TestVerify_MarksLost\|TestFilesAPI' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 8.350s` |
| Lệnh file.verify: thiếu ảnh gốc / một biến thể / đối tượng tệp máy chủ → lost; ảnh riêng tư, tệp tạm, tenant khác giữ ready; nối lô (lô 2, ≥ 4 lệnh); phục hồi đúng 1 lệnh `:restore:`; tải lại ảnh mất một biến thể → biến thể được làm lại, về ready (sau review) | `TestVerifier_MarksMissingLost` (+ `checkReupload`) | `go test -race -count=1 ./internal/platform/files/...` | `ok elimaz.com.vn/crm/internal/platform/files 51.709s`; gỡ phần sửa `process.go` thì đỏ: `verify_test.go:160: biến thể đã mất của y chưa được làm lại` |
| Head lỗi mạng → not_sent, không lost | `TestVerifier_StoreErrorMarksNothing` | như trên | ok |
| Tệp đổi version giữa Execute và OnDone → không lost; OnDone chạy 2 lần → đúng 1 lệnh lô sau | `TestVerifier_ChangedFileNotMarked` | như trên | ok |
| Mẫu theo tệp qua view: 1 mẫu, 2 mẫu (mã nhỏ nhất), ảnh đã xóa, tệp không gắn, tenant khác, rỗng | `TestFileProducts` | `go test -race -count=1 ./internal/queries/...` | `ok elimaz.com.vn/crm/internal/queries 113.388s` |
| TP08: 200 lượt, giết SeaweedFS ở ~100 complete; mọi tệp đã báo xong đúng sha256; lượt dở tải lại được | `TestFileStoreCrash` (tag drill) | `COMPOSE_PROJECT=crm-t46 scripts/files/crash-drill.sh` | `FILES CRASH OK uploads=200 completed=119 failed_midway=81 retried_ok=81` (bản commit) |
| TP09, TP12: dựng lại từ kho ngoài có bấm giờ, lost=2 đúng 2 ảnh sau lần chép cuối, ảnh còn lại đọc được gốc + 12 biến thể, dòng restore_drill | `TestFileStoreRestore` (tag drill) | `COMPOSE_PROJECT=crm-t46 scripts/files/restore-drill.sh` | `FILES RESTORE OK restore_seconds=17.4 lost=2 verify_seconds=0.3 files=22 store_kib=13112` (bản commit) |
| Cổng tĩnh + toàn bộ test (đổi testkit) | — | ci-task (lần 1, trước khi stage) | toàn bộ `go test -race ./...` xanh trừ `tests/arch` lint (gocognit, nolint thừa) và jscpd 3 đoạn trùng; gen-check, catalog-check đỏ vì chưa stage. Đã sửa; `golangci-lint run ./...` → `0 issues.`, `make jscpd` → `Found 0 clones.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `openapi/openapi.yaml` không sửa: hai operation đã có từ Task 331, máy chủ hiện thực đúng hợp đồng đó.
- Thêm `tests/drills/drill_test.go` (khung chung), `tests/drills/filestore_restore_test.go` (TP09 viết bằng Go, script
  `restore-drill.sh` bọc ngoài), `scripts/files/drill-lib.sh`, `scripts/filesync/restore.sh`, `internal/platform/files/lost.go`,
  `internal/queries/fileproducts_dynsql.go` (+ test), `internal/testkit/imager.go` (`StubImager`).
- Imager giả của `files_test` gọi `testkit.StubImager` cho ca ảnh tốt (bỏ đoạn trùng jscpd).
- `TestVerify_MarksLost` xóa đối tượng bằng MemStore (`Delete`) trong test HTTP; xóa trên SeaweedFS thật nằm ở bài diễn
  tập TP09 (xóa cả volume) — kế hoạch ghi "bằng crm-admin".

## Việc phát hiện thêm

- SeaweedFS 4.48 sau `kill -9` + khởi động lại: healthcheck (`curl /` cổng S3) qua trước khi volume đăng ký lại với
  master; vài giây đầu đọc tệp trả 500 "volume not found" (lần chạy đầu 5/119 tệp), sau đó đọc đúng. Dữ liệu không mất.
  Nên làm healthcheck sâu hơn (ví dụ `weed shell volume.list` hay GET một đối tượng mẫu) ở compose dev và production —
  task riêng (đổi hạ tầng), chưa làm.
- Lần chạy đầu `restore-drill.sh` dựng lại image `crm-filesync:dev` (thẻ image dùng chung giữa các dự án compose trên
  máy) từ worktree này: nội dung = script của main lúc đó + `restore.sh`. Đã đổi bài diễn tập sang gắn `scripts/filesync`
  chỉ đọc vào container, không dựng image nữa.
- Chưa đo với kho ngoài thật (Backblaze B2): đợi tài khoản kho ngoài (việc treo của Task M1-45 Step 6).
- (Reviewer, không bắt buộc) Đường dùng lại tệp trùng (`findDuplicate` → `Reuse`) chép bảng biến thể của tệp trùng mà
  không hỏi kho: nếu tệp trùng còn `ready` dù biến thể đã mất (trước khi chạy đối chiếu), tệp mới cũng thiếu. Sau
  `verifyFiles` mọi tệp cùng nội dung đều `lost` nên đường phục hồi của task này không đi vào khe đó. Việc riêng nếu muốn.
- (Reviewer, không bắt buộc) `ListLostSameContent` không lọc theo mục đích: tệp `lost` khác mục đích cùng sha cũng được
  xếp `file.process`; nội dung không hợp mục đích đó thì lệnh `dead` (tệp vẫn `lost`), có thể sinh cảnh báo hàng đợi thừa.

## Reviewer

Lần 1: CẦN SỬA — một phát hiện: tệp lost vì thiếu một biến thể, khi tải lại thì `makeVariants` tin bản kê
`manifest.json` (còn nguyên) mà không kiểm biến thể → tệp mới và tệp lost về ready dù thiếu biến thể. Đã sửa:
`Processor.variantsPresent` — bản kê chỉ được tin khi mọi biến thể còn trên kho, thiếu thì làm lại phần thiếu; thêm ca
tải lại ảnh thiếu biến thể vào `TestVerifier_MarksMissingLost` (đỏ khi gỡ phần sửa). Ghi chú không bắt buộc đã làm:
lỗi của `testkit.StubImager` khi mã khác 200 không còn `%w` nil; mục 6.13 sửa cho khớp test.

Sau lần 1, ci-task chạy lại: đỏ ở (a) funlen của `TestVerifier_MarksMissingLost` → tách `checkReupload`; (b) luật ranh
giới (d): `testkit.S3AdminClient` nhập aws-sdk ngoài `internal/platform` → gỡ, trả `adminClient` về `s3store_test.go`,
bài diễn tập đo dung lượng bằng `du` trong container; (c) `TestProcessPrivate_PropertyNeverPublic` quá hạn ghi kết quả
(`commit transaction: timeout`) khi cả bộ `go test ./...` chạy cùng lúc — chạy riêng `-count=3` qua (53 s), không do
task này (phần sửa chỉ thêm Head trên MemStore). Sau sửa: `golangci-lint run ./internal/... ./cmd/...` → `0 issues.`,
`make jscpd` → `Found 0 clones.`, `go test -race ./internal/platform/files/... ./internal/testkit/ ./tests/arch/` → ok.

Lần 2: ĐẠT — không còn phát hiện phải sửa; Ảnh hưởng khớp thực tế; mọi dòng bằng chứng trỏ tới test có thật và kiểm
đúng điều tiêu chí nói; không test nào bị xóa, tắt hay nới.

Kết luận: ĐẠT
