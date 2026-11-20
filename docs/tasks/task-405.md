# Task 405 — M1-55d: Sổ tay và số đo, nghiệm thu M1

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 56 M1-55, chia 19/11: Task 405 = Step 4,
cộng phần "Điểm dừng M1-D" làm được trên máy này). Loại việc (quy trình mục 3): A, **chỉ tài liệu** (không đổi mã Go,
SQL, màn hình, cấu hình).

Việc: (1) `docs/skill-drafts/them-module.md`; (2) mục "Module mẫu" (thêm inventory) và "Bẫy đã gặp" của M1 trong
`CLAUDE.md`; (3) `docs/do-luong/cai-dat-agent-m1.md` cùng số đo như `cai-dat-agent-m0.md`, `cai-dat-agent-m2.md`;
(4) `docs/nghiem-thu-m1.md`: cột Kết quả của tám tiêu chí "Xong M1 khi" và 23 ca M1, có bằng chứng; staging ghi "chờ chủ
shop"; (5) theo ghi chú Task 414: chạy lại các ca chạy tay có lịch "trước khi đóng mốc" (`docs/ca-chay-tay.md`: TP01 imager
thật, restore-drill, crash-drill, imagercompose, filesync, libvips) trên bộ Docker riêng `crm-t405`, cập nhật cột "Lần chạy
gần nhất".

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (task chỉ sửa `.md`).
- Gói phụ thuộc ngược (go list): không có gói Go nào đổi. Tệp tài liệu có test giữ: `docs/ca-chay-tay.md` ←
  `tests/deploy/manual_suites_test.go` (mỗi nhãn build đúng một dòng, đủ cột); `CLAUDE.md` ← giới hạn 200 dòng (luật tay,
  hook không đếm); `docs/nghiem-thu-m1.md`, `docs/do-luong/*`, `docs/skill-drafts/*` không có test đọc. Không sửa
  `docs/views.md`, `docs/quyen.md`, `docs/adr/0046-thu-tu-khoa.md`, `openapi.yaml` (Task 95 đang sửa song song).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có (chỉ chạy lại e2e màn M1 làm bằng chứng tiêu chí 8).
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: các test có tên trong bảng "Tiêu chí → ca kiểm" (chạy lại trên HEAD làm bằng chứng, không phải vì đổi mã);
  ca chạy tay theo `docs/ca-chay-tay.md`; `go test ./tests/deploy/ -run TestManualSuites` (giữ bảng ca chạy tay);
  `scripts/agent/ci-task.sh` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không viết hàm hay kiểu nào. Tệp đo M1 dùng đúng khung và
  lệnh của `docs/do-luong/cai-dat-agent-m0.md`, `cai-dat-agent-m2.md` (mục 12 quy trình) để ba mốc đặt cạnh nhau. Số
  mutation lấy từ `docs/mutation.md` (Task 403), phát hiện review độc lập từ `docs/review-doc-lap-m1.md` (Task 404), không
  đo lại.
- Dùng lại / viết mới vì: `them-module.md` là bản nháp mới (Q30 kế hoạch M1; chủ shop tự chép vào `.claude/skills/`), dẫn
  tới mã mẫu thật (`catalog`, `inventory`) và tệp luật đã có thay vì chép lại nội dung của chúng.

## Soát thiết kế

Task chỉ đổi tài liệu, không chạm database, tiền, quyền, hàng đợi, HTTP.

- 5.1–5.12: không áp dụng: không có mã mới. Riêng `them-module.md` **mô tả** đúng các luật 5.1–5.12 (chủ dữ liệu theo 04
  mục 3, view `_v`, một transaction, sự kiện/lệnh sau commit, `in_doubt`, `WithTenant` + RLS, module không nhập
  `net/http`, `x-permission` + `IdempotencyKey`, không thêm thư viện, theo module mẫu); đối chiếu từng bước với mã thật
  của `catalog`, `inventory` và với `tests/arch`, `tenancy.CheckSchemaRules`.
- 6.1–6.13: không áp dụng: không có mã chạy đồng thời. `them-module.md` nhắc thứ tự khóa ADR-46, `db.RetryTx` chạy lại
  được, test hai đường song song không deadlock (dẫn tới tệp gốc, không chép bảng thứ tự khóa).
- Bộ Docker riêng `crm-t405` (cổng 5905, 20405, 21405, 24405 cho imager): diễn tập xóa volume chỉ của dự án này.

## Bằng chứng

Chạy 21/11 trên bộ Docker riêng `crm-t405` (`CRM_PG_PORT=5905`, `CRM_S3_PORT=20405`, `CRM_S3_OFFSITE_PORT=21405`,
`CRM_IMAGER_PORT=24405`), HEAD `e53d001`; sau `git merge main` (fast-forward `4535a0d`) chạy lại hàng 1 và hàng 2.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Ca nghiệm thu M1 (bảng "Tiêu chí → ca kiểm"), mọi tên test có trong mã | 38 test: `TestMotDonDiTron`, `…_HaiDonSongSongTon1`, `TestValidate_SevenSeededErrors`, `TestApply_200Models`, `TestApply_SameFileTwice`, `TestCreateProduct_SixVariants`, `TestAttachImage_OrderAndColor`, `TestPostImport_IncreasesOnHandAndLedger`, `TestAvailability_Formula`, `TestInventoryInvariants`, `TestStocktake_InTransitNotAdjusted`, `TestCreateProduct_DuplicateSKU`, `TestStocktake_ShortageKeepsHolds`, `TestPostDamage_ReducesAvailable`, `TestPostExport_CannotEatHeld`, `TestVariantForSale_Discontinued`, `TestCreateOrder_DiscontinuedVariant`, `TestReconcile_ReportsExactVariant`, `TestOrderItems_SellerFK`, `TestAttachShipment`, `TestApplyShipmentProgress_Flow`, `TestReplaceGrants_{ExceedsActor,SelfModify,VersionAndInput,ActorLostManageMeanwhile}`, `TestLock_LastOwner{,Concurrent}`, `TestFileProcess_ReadyWithTwelveVariants`, `TestFileProcess_HangDoesNotBlockWorker`, `TestCreateUpload_TooLarge`, `TestOriginalURL_ExpiresAndNeedsPermission`, `TestImageSoftDeleteRestore`, `TestRegeneratePresetV2`, `TestRule_FileSyncAge`, `TestVerify_MarksLost`, `TestSupplier_OnImportOnly`, `TestArticle_RejectsUnknownBlock`, `TestCustomRole_CannotExceedActor` | `go test -race -count=1 -run '^(…38 tên…)$' -v ./internal/modules/{catalog,inventory,order,identity}/... ./internal/platform/{files,observability}/... ./internal/httpapi/... ./tests/e2e/`; tên: vòng `git grep "func <tên>"` | e53d001: 38 `--- PASS`, 9 gói `ok` (vd. `ok …/modules/inventory 25.641s`); 4535a0d: 38 PASS, 0 FAIL. Mọi tên có trong mã; `forEachSeller` của kế hoạch là `testkit.ForEachSeller` (`internal/testkit/sellers.go:32`) |
| TP01 đường ảnh trọn vẹn với imager thật | `TestFilesPipeline_PublicPhotoGPS`, `TestFilesPipeline_PrivateImages` | `docker compose -p crm-t405 -f docker/compose.dev.yml --profile imager up -d --wait --build`; `CRM_IMAGER_PORT=24405 go test -tags imager -count=1 -run TestFilesPipeline -v ./tests/e2e/` | `--- PASS: TestFilesPipeline_PublicPhotoGPS (2.92s)`, `--- PASS: TestFilesPipeline_PrivateImages (2.37s)`, `ok …/tests/e2e 6.003s`; 4535a0d: `ok …/tests/e2e 4.329s` |
| TP09, tiêu chí 6: dựng lại từ kho ngoài, có bấm giờ; biến thể giữ `content-type` | `TestFileStoreRestore`; kiểm HEAD từng đối tượng sau dựng lại | `scripts/files/restore-drill.sh`; script `curl --aws-sigv4 … -I` từng khóa của `crm-public`, `crm-originals`, `crm-private` | `FILES RESTORE OK restore_seconds=14.2 lost=2 verify_seconds=0.2 files=22 store_kib=4796`, `--- PASS: TestFileStoreRestore (22.69s)`; `crm-public` 273 đối tượng: 84 `image/avif`, 84 `image/jpeg`, 84 `image/webp`, 21 `application/json`; `crm-private` 4/4/4 ảnh + 1 json đúng loại; `crm-originals` 21 `application/octet-stream` (Việc phát hiện thêm) |
| TP08, tiêu chí 5: tắt kho tệp đột ngột | `TestFileStoreCrash` | `scripts/files/crash-drill.sh` | `FILES CRASH OK uploads=200 completed=119 failed_midway=81 retried_ok=81`, `--- PASS: TestFileStoreCrash (36.59s)` |
| Container imager cô lập | `TestImagerCompose_*` (6) | `make test-imager-compose COMPOSE_PROJECT=crm-t405 CRM_IMAGER_PORT=24405` | 6 `--- PASS` (NoSecrets, CannotReachPostgres, CannotReachStoreInternals, ReadOnlyAndUnprivileged, NoInternet, HostReachesImager…), `ok …/internal/imager 3.090s` |
| TP10 chép sang kho ngoài (container) | `TestFileSync` | `docker build -q -f docker/filesync/Dockerfile -t crm-filesync:t405 .`; `CRM_FILESYNC_IMAGE=crm-filesync:t405 go test -tags filesync -count=1 -run TestFileSync -v ./tests/e2e/` | `--- PASS: TestFileSync (26.18s)` |
| TP03, tiêu chí 7: libvips thật (bom điểm ảnh, treo, hết bộ nhớ) | `TestImager_PixelBombRejectedBeforeDecode`, `TestImager_HangKilledWithinDeadline`, `TestImager_OutOfMemoryKilled`, `TestServer_StillAnswersAfterOOM`, `TestExecRunner_{TimeoutKillsProcessGroup,MemoryLimit}` | `make test-imager TEST_IMAGER_FLAGS=-v` | 49 `--- PASS`, 1 SKIP (`TestImager_Measure`, đo tay); `ok …/internal/imager 13.188s`, `ok …/cmd/imager 1.026s`, exit 0 |
| Tiêu chí 8: màn hình M1 | vitest cả console; Playwright 16 tệp màn M1 | `npx vitest run`; `CRM_E2E_PORT=25405 npx playwright test e2e/{health,errors,imports,inventory,lost-files,product-article,product-edit,product-images,products,roles,shell.phone,shell,staff,stock-documents,stocktake.phone,suppliers}.spec.ts`; WebKit sau `npx playwright install webkit`: `--project=webkit e2e/shell.phone.spec.ts e2e/stocktake.phone.spec.ts` | `Test Files 71 passed`, `Tests 891 passed`; chromium `157 passed`; WebKit `8 passed`, `2 failed` (`bấm đúp Áp dụng`, `máy tính 1512`: `<html> intercepts pointer events` khi `click`/`dblclick`; chạy lại `--workers=1` vẫn đỏ) — Việc phát hiện thêm |
| `docs/ca-chay-tay.md` vẫn đúng luật sau khi sửa cột "Lần chạy gần nhất" | `tests/deploy` (`manual_suites_test.go`) | `go test -count=1 ./tests/deploy/` | `ok …/tests/deploy 3.211s` |
| Số đo `cai-dat-agent-m1.md` | — | `jscpd --config .jscpd.json --reporters console .`; ba lệnh `git grep "//nolint"` của mục 3; `wc -l CLAUDE.md` | `Found 0 clones`, 900 tệp, 164.120 dòng (Go 688 / 124.956); 1.349 / 831 / 0; nhóm M1 260; `125 CLAUDE.md` |
| Cổng kiểm của task (sau gộp main) | — | `scripts/agent/ci-task.sh` (một lần, trước review) | `CI-TASK XANH (so với điểm tách nhánh 4535a0dc…)`: catalog-check, jscpd `Found 0 clones`, gitleaks `no leaks found`, oasdiff, test-guard, `ok …/tests/arch 20.522s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch Step 4 ghi thêm "Module mẫu (catalog, inventory)": `catalog` đã có trong `CLAUDE.md` từ Task 27, task này chỉ
  thêm `inventory` và một câu trỏ tới `docs/skill-drafts/them-module.md`.
- Ngoài Step 4, theo lời giao: chạy lại mọi ca có lịch "trước khi đóng mốc" của `docs/ca-chay-tay.md` (trừ `perf`, xem
  Quyết định 3) và cập nhật cột "Lần chạy gần nhất"; chạy lại 38 test Go của bảng ca và e2e màn M1 làm bằng chứng.
- `docs/do-luong/cai-dat-agent-m2.md` (cột M1 còn "—") giữ nguyên: là ảnh chụp ngày 19/11; số M1 nằm ở tệp mới.

## Quyết định thay chủ shop

1. **Task chỉ tài liệu vẫn chạy reviewer** — chọn: chạy (lời giao yêu cầu; `CLAUDE.md` cho phép bỏ). Vì sao: tệp nghiệm
   thu và "Bẫy đã gặp" là nơi mốc sau và chủ shop dựa vào; reviewer đối chiếu số với nguồn. Phương án khác: bỏ reviewer.
2. **Hai ca WebKit đỏ của màn Kiểm kho** — chọn: không sửa, không nới test; ghi "Việc phát hiện thêm" cho điều phối viên.
   Vì sao: ngoài phạm vi (task tài liệu), sửa test là dấu hiệu chệch hướng; cùng nút bấm bằng `tap` đạt nên khả năng cao là
   giả lập chuột của WebKit, không phải lỗi màn. Phương án khác: đổi `click` → `tap` trong spec (task riêng, cần reviewer).
3. **`make perf-test` không chạy lại** — chọn: giữ lần chạy 19/11 (Task 421). Vì sao: luật "máy rảnh", lúc làm load
   9–31; số đo thời gian dưới tải không có nghĩa; thử tải và perf thuộc Task 402 (chủ shop dời 19/11). Phương án khác: chạy
   và ghi kèm tải máy.
4. **Cài trình duyệt WebKit của Playwright** (`npx playwright install webkit`, vào `~/Library/Caches/ms-playwright`, bản
   2388 mà Playwright 1.65.1 đã ghim cần) — chọn: cài. Vì sao: máy thiếu bản này nên 10 ca điện thoại không chạy được; chỉ
   là tệp đệm cục bộ, không đổi repo, xóa thư mục là đảo ngược.
5. **Năm dòng "Bẫy đã gặp" của M1** — chọn: chỉ thêm lỗi phải sửa lặp ở ít nhất hai task (luật mục 12), một câu mỗi lỗi,
   kèm số task; không thêm lỗi chỉ gặp một lần (38, 323, 40). `CLAUDE.md` 125/200 dòng.
6. **Tiêu chí cần staging, nhân viên, kho ngoài thật, thử tải** — ghi "chờ chủ shop" hoặc "chờ Task 402 (dời theo chủ
   shop 19/11)"; không deploy, không tạo tài khoản. Tiêu chí 8 ghi "phần máy đạt" (22/23 ca có test đạt), không ghi "đạt"
   vì phần nhân viên chạy trên staging chưa có.

## Việc phát hiện thêm

- **Ảnh gốc mất `content-type` sau khi dựng lại từ kho ngoài.** Sau `restore-drill.sh`, 21/21 đối tượng `crm-originals`
  có `application/octet-stream` (bucket mã hóa bằng rclone crypt, khóa không có đuôi nên rclone không đoán được loại);
  biến thể (`crm-public`, `crm-private`) giữ đúng loại. Ảnh hưởng: xử lý lại (`file.regenerate`, TP07) không xét loại của
  ảnh gốc (imager dò theo nội dung) nên vẫn chạy; đường dẫn ảnh gốc ký sẵn (TP05) mở trên trình duyệt có thể bị tải về
  thay vì hiện. Đề xuất task nhỏ: `PresignGet` của ảnh gốc đặt `ResponseContentType` theo `declared_type` đã kiểm, hoặc
  filesync giữ siêu dữ liệu; thêm kiểm `content-type` vào `TestFileStoreRestore`.
- **Hai ca WebKit đỏ** ở `console/e2e/stocktake.phone.spec.ts` (`bấm đúp Áp dụng → chỉ một yêu cầu áp dụng`, `máy tính
  1512 › cùng màn trong cột hẹp…`): `locator.click`/`dblclick` hết 30 s vì `<html lang="vi"> intercepts pointer events`
  trên giả lập iPhone 13 (WebKit 27.2, Playwright 1.65.1); các ca `tap` cùng tệp đạt (8/10). Task 329 ghi webkit đạt 18/11
  nên cần soát bản WebKit lúc đó so với nay; đề xuất task: dùng `tap` cho ca điện thoại, tách ca 1512 sang project chromium.
- Task 415 (M1-r10, ghi cho M2 và kế hoạch: A-03, D-04..D-06, E-03) và Task 424 (M1-x10) chưa gộp vào main ngày 21/11;
  không chặn tiêu chí "Xong M1" nào (ghi chú cho M2 và giảm số gói test).

## Reviewer

Kết luận: ĐẠT (lượt 2, agent `reviewer`, trên `git diff --cached` sau khi gộp main `4535a0d`).

- Lượt 1 — CẦN SỬA: 23 dòng bảng "Tiêu chí → ca kiểm thử" của `docs/nghiem-thu-m1.md` có `||` (thừa một ô rỗng), nên
  cột Kết quả hiện trống khi xem bảng. Đã sửa: `|| ` → `| `, mỗi dòng 5 cột như tiêu đề (awk `NF=7` cả 23 dòng). Ghi chú
  không bắt buộc đã làm: `them-module.md` ghi tên lỗi thiếu phụ thuộc đúng cả hai module (`errWriterNotWired` ở catalog,
  `errNotWired` ở inventory). Reviewer đã đối chiếu: đủ 38 tên test có trong mã; số mutation khớp `docs/mutation.md`; số
  review độc lập khớp `docs/review-doc-lap-m1.md`; mục Reviewer của 43, 46, 325, 330, 400, 409, 410, 420 khớp bảng đo;
  năm dòng Bẫy mới có căn cứ ở ít nhất hai task (330 chỉ là ghi chú, vẫn còn 48, 325); `docs/ca-chay-tay.md` vẫn khớp
  `tests/deploy/manual_suites_test.go`; mọi tên trong `them-module.md` và "Module mẫu" có thật.
- Lượt 2 — ĐẠT, không có phát hiện phải sửa, không có ghi chú.
