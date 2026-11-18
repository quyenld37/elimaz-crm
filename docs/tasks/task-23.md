# Task 23 — Ranh giới module, CI, mutation testing, sổ tay, review độc lập, staging

Task 23 (M0-23). Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`, Task 23. Loại việc (quy
trình mục 3): A. Spec: ADR-25 (vùng rủi ro, mutation, review độc lập), ADR-26 (build một lần, lùi bản), ADR-30 mục 5
(sao lưu cấu hình), ADR-43 (image dựng ở CI), ADR-44 (cổng kiểm, gremlins).

Việc treo giao cho task này: `make console-e2e` vào CI (Task 48); image `crm:dev` dùng chung mọi phiên → tên theo dự án
compose (Task 395); CI cần SeaweedFS cho test S3 (Task 41); không mở cổng hãng giả ra ngoài trên staging (Task 31);
shellcheck chưa ghim (Task 21); compose máy thử có `init: true` cho PostgreSQL (Task 22).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Mã Go mới chỉ là test (`tests/deploy/ci_test.go`,
  `exportconfig_test.go`, thêm `TestProdCompose_LogLimits` và kiểu `logging` riêng của gói test).
- Gói phụ thuộc ngược (go list): `tests/deploy` là gói chỉ có test, không ai nhập. `go.mod`: `github.com/go-faster/yaml`
  từ indirect thành direct (đã có trong go.sum, không thêm mô-đun).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Hạ tầng, cấu hình: `Makefile` (`CRM_IMAGE` theo dự án compose, đích `image`, `mutation`, `ci-tools`),
  `tools/versions.mk` (`GITLEAKS_VERSION`), `docker/compose.dev.yml` (bốn dịch vụ ứng dụng đọc `${CRM_IMAGE:-crm:dev}`),
  `docker/compose.prod.yml` (giới hạn log, phát hiện của review độc lập), `scripts/ops/image-check.sh`,
  `scripts/demo/mot-don-compose.sh` (tên image), `Dockerfile` (chú thích), `.gitignore` (`/.mutation/`). Tệp mới:
  `.github/workflows/ci.yml`, `.gremlins.yaml`, `scripts/ops/export-config.sh`.
- Người dùng `make image`, `make app-up`, `make demo-mot-don`, `make image-check` ở phiên khác: image nay là
  `crm:<dự án compose>` thay `crm:dev`; ai chạy `docker compose` thẳng không qua make vẫn dùng `crm:dev` như cũ.
  `loadtest/` đã dùng `CRM_IMAGE` riêng (`crm:loadtest`), không đổi.
- Tài liệu: `docs/RUNBOOK.md` (thêm), `docs/REBUILD.md`, `docs/threat-model-m0.md`, `docs/mutation.md`, `docs/adr/0001…0045,
  0047`, `docs/do-luong/cai-dat-agent-m0.md`, `docs/do-luong/review-doc-lap-m0.md`, `docs/phu-thuoc.md`.
- Docker: chỉ dự án `crm-t23` (cổng 5523, 20023, 21023, 17023, 4423).
- Test sẽ chạy: `go test ./tests/deploy/...`; `make mutation` (vùng cao); `make image-check` với `crm:crm-t23`;
  `actionlint`; chạy thử cổng đỏ (lint, test bị xóa, oasdiff) cục bộ; `make ci` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gói Go nào cho
  việc đọc compose hay workflow); `tests/deploy/compose_test.go` (`configEnv`, `prodProject`, kiểu `service`) — dùng lại
  cho test log và image; `scripts/ops/image-check.sh`, `scripts/backup/*.sh` (mẫu `log`/`die`, `set -euo pipefail`);
  `Makefile` (`HIGH_RISK_DIRS` của hook là nguồn danh sách vùng cao; test đọc nó thay vì chép).
- Dùng lại: `configEnv`, `prodProject` cho test compose mới; `make db-up`, `make ci`, `make image-check`,
  `make console-e2e` nguyên vẹn trong CI (CI không có đường riêng); gitleaks (đã trong `make ci`) cho `export-config.sh`.
- Viết mới: `export-config.sh` (chưa có gì gom cấu hình); `ci_test.go` (chưa có test nào đọc workflow);
  `exportconfig_test.go` dùng `docker`, `aws` giả trên PATH vì không được chạm máy chủ, kho ngoài thật.
- jscpd: 0 đoạn trùng trên toàn repo (444 tệp, `docs/do-luong/cai-dat-agent-m0.md`), nên mốc nền `jscpd-baseline.json`
  giữ rỗng; không có đoạn trùng nào cần dòng trả lời (Step 3).

## Soát thiết kế

Mục 5 (khớp kiến trúc):
1–5. Không áp dụng: không có dữ liệu, bảng, việc nghiệp vụ mới.
6. Gọi ra ngoài: `export-config.sh` gửi tệp nén lên kho ngoài bằng `aws` CLI, chỉ khi đặt `CRM_CONFIG_S3_URI`, sau khi
   gitleaks sạch; không phải lệnh nghiệp vụ nên không qua `commands`.
7–10. Không áp dụng: không truy vấn, không route, không Valkey.
11. Công cụ ngoài ADR-44: gremlins đã có trong ADR-44 và `tools/versions.mk`; `go-faster/yaml` (nhập thẳng, chỉ test) và
    actionlint (chỉ chạy tay) ghi ở `docs/phu-thuoc.md`.
12. Module mẫu: không áp dụng.

Mục 6 (đúng khi chạy đồng thời):
1. Hai phiên cùng lúc dựng image: nay mỗi dự án compose một tên image, không ghi đè nhau (việc treo Task 395).
2–11. Không áp dụng: không đường ghi, không tiền, không hàng đợi.
12. Dữ liệu cá nhân, bí mật: `export-config.sh` chỉ xuất TÊN biến; gitleaks dừng khi thấy giá trị; báo lỗi `--redact`;
    test kiểm cả hai chiều (không giá trị nào lọt, có bí mật thì không tạo tệp, không gửi).
13. Quyền: workflow chỉ `contents: read`; không job nào xin quyền ghi hay dùng secret (test kiểm).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `ci.yml` chạy `make ci` sau `ci-tools`, `db-up` (PostgreSQL 18 + SeaweedFS, Task 41); nhánh main cục bộ; `TASK_FILE`; dọn compose | `TestCI_GateJobRunsMakeCI` | `go test -count=1 ./tests/deploy/` | `ok` (đỏ trước khi có ci.yml: "no such file") |
| Image dựng đúng một lần, nhãn `${{ github.sha }}`, qua `image-check`, chờ job ci | `TestCI_ImageBuiltOnceTaggedByCommit` | như trên | `ok` |
| `make console-e2e` trong CI (Task 48); mutation chỉ theo lịch | `TestCI_ConsoleE2E`, `TestCI_MutationScheduledOnly` | như trên | `ok` |
| Lượt theo lịch không chặn/hủy lượt push lên main; không dùng secret (reviewer lượt 1) | `TestCI_ConcurrencySeparatesScheduleFromPush`, `TestCI_NoSecrets` | `go test -count=1 ./tests/deploy/` | `ok` (đỏ trước khi sửa `concurrency.group`) |
| Quyền tối thiểu, action ghim mã commit | `TestCI_TriggersAndLeastPermission`, `TestCI_ActionsPinnedBySHA` | như trên; `actionlint .github/workflows/ci.yml` | `ok`; actionlint không báo gì |
| `make ci` gồm đủ cổng (test kiến trúc nằm trong `go test ./...`) | `TestMakeCI_RunsEveryGate` | như trên | `ok` |
| `make mutation` phủ đúng `HIGH_RISK_DIRS` của hook | `TestMakeMutation_CoversHighRiskDirs` | như trên | `ok` (đỏ trước khi có đích) |
| Image theo dự án compose (Task 395) | `TestMakeImage_TagPerComposeProject`, `TestDevCompose_AppImageFromEnv` | như trên | `ok` (đỏ trước: `docker build -t  .`) |
| Log máy chủ có giới hạn (review độc lập) | `TestProdCompose_LogLimits` | như trên | `ok` (đỏ trước: `crm-api: logging … <nil>`) |
| `export-config.sh`: chỉ tên biến, có digest; thấy bí mật thì dừng, không tạo tệp, không gửi; giữ 8 bản, gửi `config/` | `TestExportConfig_NamesOnlyAndDigests`, `TestExportConfig_StopsOnSecret`, `TestExportConfig_KeepsEightAndUploads` | như trên | `ok` (đỏ trước khi có script) |
| Step 1: cổng đỏ đúng bước — lỗi lint | — | tệp tạm `zz_lintdemo.go` + `golangci-lint run ./internal/platform/textnorm/` | thoát 1, `func unusedThing is unused (unused)`; đã xóa tệp |
| Step 1: test bị xóa không ghi lý do | — | dời tạm `textnorm/fold_test.go` + `TASK_FILE=docs/tasks/task-23.md make test-guard` | thoát 2, `internal/platform/textnorm/fold_test.go: tệp test bị xóa`; đã trả tệp |
| Step 1: thay đổi phá vỡ `openapi.yaml` | — | bản sao đổi đường `…/abandon` + `make oasdiff OASDIFF_BASE=-base=openapi/openapi.yaml OASDIFF_REV=<bản sao>` | thoát 2, `CHƯA DUYỆT: abandonJob … api-path-removed-without-deprecation` |
| Step 3: mốc nền jscpd | — | `jscpd --config .jscpd.json --reporters console .` (`docs/do-luong/cai-dat-agent-m0.md`) | 0 đoạn trùng; mốc nền giữ rỗng |
| Step 4: mutation ≥ 80% mọi gói vùng cao | `TestResolveLookupErrorCountedStill`, `TestResolveZeroTimeoutUsesDefault`, `TestNewWorkerOptionDefaults`, `TestGuardReadRouteNeedsPermission`, `TestCanShopScopeGrantOrderAndEmptyID`, `TestReplaceGrants_ActorLostManageMeanwhile` | `make mutation` (trước gộp main); mỗi đột biến được tính là giết đã cài tay rồi chạy test mới | lần đầu queue 77,65%, commands 79,41% (dưới ngưỡng); sau test thêm queue 82,35%, commands 88,24%, identity 91,53%, guard 100%, tenancy, idempotency 100%. Mọi đột biến cài tay → `FAIL`. `docs/mutation.md` |
| Step 5: ADR 0001–0045, 0047 đủ ba trường | — | `ls docs/adr \| wc -l`; dòng 3–5 mỗi tệp | 47 tệp, dòng 3–5 là `Trạng thái:`, `Ngày:`, `Kiểm bằng:` |
| Step 6: review độc lập toàn nhánh | — | agent riêng, không đọc tệp task | 29/34 mục có mã có test; phát hiện và cách xử lý ở `docs/do-luong/review-doc-lap-m0.md` và "Khác kế hoạch" |
| Step 6b: đo cách cài đặt agent | — | `docs/do-luong/cai-dat-agent-m0.md` | đủ 7 số đo; 17 đề xuất: giữ 11, chỉnh 6 |
| Cổng kiểm lần 2 (sau sửa review lượt 1) | — | `make -k ci` | `vet`, `gen-check`, `sqlc-vet`, `vuln`, `catalog-check`, `jscpd` (`Found 0 clones`), `gitleaks` (`no leaks found`), `console-check` (190 passed) xanh. `lint`: 1 lỗi gci ở `ci_test.go` → `golangci-lint run --fix`, rồi `golangci-lint run ./...` → `0 issues`. `test`: 10 gói đỏ vì `DROP DATABASE … timeout` / hết hạn 10 phút (hook Stop chạy cùng lúc); chạy lại `go test -race -count=1 -timeout 40m -p 4` mười một gói đó → `ok` trừ `queue` `TestStopAfterLongHandlerReleases` (2,48 s); chạy lại `-count=5` test đó và cả gói `queue` → `ok`. `oasdiff`, `test-guard` đỏ vì main đã tiến thêm sau lần gộp (operation `setProductImageColor`, tệp test console của task khác); `make oasdiff OASDIFF_BASE=-base-ref=abd3410` → thoát 0 |
| Image theo dự án compose chạy thật | — | `CRM_API_PORT=4523 CRM_INGEST_PORT=4524 make image-check` | `IMAGE CHECK OK size_bytes=11643342 … user=65532:65532`, ba vai trò healthy; image `crm:crm-t23` |
| Cổng kiểm toàn bộ (sau gộp main abd3410, phần đã stage) | — | `make ci` | build, vet, lint, gen-check, sqlc-vet xanh; `test`: 4 gói (`httpapi`, `identity`, `inventory`, `files`) hết hạn 10 phút / `TestPublicVariants` đỏ khi hook Stop chạy cùng lúc (máy quá tải); chạy lại riêng `go test -race -count=1 -timeout 30m` bốn gói → `ok` cả bốn (hook Stop cùng lúc cũng báo `files` ok). Sau đó `make vuln oasdiff catalog-check jscpd` xanh (`No vulnerabilities found`, `Found 0 clones`), `make gitleaks console-check` xanh (`no leaks found`, vitest 190 passed). `test-guard` so với `main` hiện tại báo `identity/roles_test.go` vì main đã tiến thêm Task 58 sau lần gộp; so với abd3410 (`git diff --cached --name-status abd3410 -- '*_test.go'`) chỉ có tệp test thêm/sửa, không dòng nào bị xóa, không `t.Skip` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `.githooks/pre-commit` (thêm quét gitleaks phần đã stage) **không làm**: hook chặn agent sửa `.githooks/` (chỉ chủ
  shop đổi). Đề xuất ở "Việc còn lại". `make ci` đã có `gitleaks git` và `gitleaks dir`; CI chạy cả hai.
- CI dựng PostgreSQL bằng `make db-up` (compose dev: image PostgreSQL 18 của repo có role, extension) thay `services:
  postgres:18` của GitHub: cùng cách với máy dev, có sẵn SeaweedFS mà test `files` cần (Task 41).
- Thêm đích `ci-tools`, `GITLEAKS_VERSION` (máy CI cài công cụ đã ghim) và job `mutation` theo lịch tuần.
- Review độc lập: phát hiện 1 (câu nhận việc quét cả bảng) đã do Task 394 sửa trên main (gộp vào nhánh này); phát hiện
  2 (log máy chủ không giới hạn) sửa ở đây (`TestProdCompose_LogLimits`); phát hiện 3 (mở rộng vùng cao) giữ lại cho
  chủ shop; phát hiện 4 (sản phẩm Task 23) là chính task này.
- Thêm test cho đột biến còn sống ngoài hai gói dưới ngưỡng (guard.go, identity) vì hai đột biến có ý nghĩa an ninh.
- Gộp main: `docs/tasks/hien-tai.md` (do `new-task.sh` ghi) làm `git merge` dừng; đã trả tệp về bản HEAD bằng
  `git restore`, gộp (fast-forward tới abd3410), rồi chạy lại `new-task.sh 23` để ghi lại đúng trạng thái.
- Kết quả mutation đo trên mã **trước** khi gộp main. Sau gộp, `queue/worker.go`, `commands/indoubt.go`, `guard.go`,
  `permissions.go` không đổi; `identity/staff.go`, `roles.go` đổi (Task 56) nên số dòng và con số identity trong
  `docs/mutation.md` là của bản trước gộp; lượt CI theo lịch đo lại.

## Quyết định thay chủ shop

1. **Gremlins, không cần lỗi cài tay.** gremlins v0.6.0 chạy được với Go 1.27.4, đúng ADR-44; không cần go-mutesting
   hay script cài lỗi.
2. **Ngưỡng 80% áp cho từng gói**, không cộng chung (cộng chung thì gói lớn che gói nhỏ). Đảo ngược: đổi cách đọc.
3. **Mutation không nằm trong `make ci`** (queue mất một giờ); CI chạy theo lịch chủ nhật 02:00 giờ Việt Nam. Phương án
   khác: mỗi push chạy `--diff` — để sau khi có CI thật đo thời gian.
4. **Danh sách vùng cao giữ đúng `HIGH_RISK_DIRS`** của hook và kế hoạch; không tự thêm `platform/db`, `webhooks`, các
   module M1 (review độc lập đề xuất) vì danh sách là luật của chủ shop ở hook.
5. **`export-config.sh` gửi bằng `aws` CLI** (kho S3 bất kỳ qua `--endpoint-url`), chỉ khi đặt `CRM_CONFIG_S3_URI`.
   Phương án khác: `rclone` (đã dùng ở `crm-filesync`) — chốt cùng kho ngoài ở bước 7.2.
6. **Image dev `crm:<dự án compose>`**; compose dev mặc định `crm:dev` khi gọi thẳng không qua make. Phương án khác:
   bắt buộc `CRM_IMAGE` (phá lệnh của phiên khác). Đảo ngược: `CRM_IMAGE=crm:dev`.
7. **Giới hạn log `driver: local`, 5 × 20 MiB mỗi dịch vụ** (chưa đo lượng log thật; chỉnh sau staging). Volume log
   riêng cho ba vai trò vẫn ở bước 7.7.
8. **Action GitHub ghim mã commit** (checkout v7.0.1, setup-go v7.1.0, setup-node v7.1.0; tra bằng `git ls-remote`).

## Việc phát hiện thêm

- `CODE_PATHS` của hook không có `.github`, `scripts/ops`, `.gremlins.yaml`: commit chỉ đổi các tệp đó không cần
  reviewer. Đề xuất chủ shop thêm.
- `test-guard.sh` chọn tệp task bằng `ls -t` (giờ sửa); checkout mới của CI mọi tệp cùng giờ → chọn bừa. CI đặt
  `TASK_FILE` từ `hien-tai.md`; nên cho script đọc `hien-tai.md` luôn.
- `identity/roles.go`: `continue` khi vai trò mẫu đã sửa tay; đột biến `break` còn sống. Test riêng khi làm màn vai trò.
- `queue`: 60 đột biến không phủ (nhánh lỗi database, vòng nền khi đầy chỗ); đề xuất test vòng nền `Concurrency: 1`.
- Ghi chú không bắt buộc của review độc lập (`docs/do-luong/review-doc-lap-m0.md`): bộ kiểm từ ngoài bỏ qua `degraded`;
  luật schema không bắt bảng quên `tenant_id`; không kiểm lúc khởi động rằng `DATABASE_URL` không phải role chủ bảng.
- ADR (đối chiếu khi viết `docs/adr/`): chưa có test cho "CRM không gọi Nhanh" (ADR-11), CSP màn hình (ADR-34), cấm
  `NOTIFY` ngoài `queries/platform/realtime.sql` (ADR-42), hai con số 20 s (ADR-42), "CI đỏ khi `-race` thấy tranh chấp" (ADR-44).
- `scripts/backup/verify-drill.sh` vẫn dựng và chạy tag chung `crm:dev` (dự án `crm-t02`); đường `make` không còn dùng
  `crm:dev` nên không đè nhau, nhưng hai lượt diễn tập song song vẫn có thể (reviewer Task 23).
- `MUTATION_HTTPAPI_EXCLUDE` liệt kê tệp cần loại: tệp mới trong `internal/httpapi` sẽ bị đột biến theo (lượt dài hơn,
  số lẫn) — nên thêm kiểm vào `TestMakeMutation_CoversHighRiskDirs` (reviewer Task 23, không bắt buộc).
- `queue` `TestStopAfterLongHandlerReleases` đỏ một lần khi máy quá tải (chạy lại 5 lần xanh): test nhạy thời gian.
- `make ci` chạy cùng lúc với hook Stop làm máy quá tải (DROP DATABASE hết hạn, gói vượt 10 phút); hook đọc `go list`
  đúng lúc `make gen` xóa tạm `internal/httpapi/gen` thì báo lỗi giả. Đề xuất hook bỏ qua khi `make ci` đang chạy.
- `oasdiff`, `test-guard` so với `main` đang chạy: nhánh gộp main sớm rồi main tiến thêm thì cả hai báo nhầm; CI trên
  GitHub so với base của PR nên không gặp.
- shellcheck vẫn chưa ghim (Task 21); actionlint dùng shellcheck khi có — nên ghim cả hai.

## Việc còn lại (giữ lại, cần OK riêng của chủ shop)

1. Tạo repo GitHub riêng tư `elimaz-crm`, push, branch protection (bắt `ci`, `image`, `console-e2e` xanh), chạy CI lần
   đầu; chọn registry (GHCR) và thêm bước đẩy image theo digest (job `image` khi đó cần `packages: write`).
2. `.githooks/pre-commit`: thêm `gitleaks git --pre-commit --staged --no-banner --redact` ở đầu (chủ shop sửa).
3. Bước 7.2–7.9 của kế hoạch: kho ngoài và khóa mã hóa, xem máy thử, staging ba vai trò từ image CI, bộ kiểm từ ngoài hai
   nơi, diễn tập khôi phục với kho thật, luật tần suất Cloudflare, volume log, `export-config.sh` lần đầu, ba bài thử,
   `heavy.js`/`chaos.sh` trên staging. Compose máy thử: PostgreSQL `init: true` (Task 22), `fakecarrier` không `ports:` (Task 31).
4. Có mở rộng vùng cao (`platform/db`, `webhooks`, `inventory`, `cash`, `order`, `fulfillment`) không — sửa
   `HIGH_RISK_DIRS` của hook; `make mutation` và test tự theo.
5. Step 6b: chủ shop quyết 17 đề xuất (cột trống trong `docs/do-luong/cai-dat-agent-m0.md`).

## Reviewer

Lượt 1 (agent `reviewer`, phần đã stage sau gộp main abd3410): **CẦN SỬA**, hai phát hiện:
1. `ci.yml`: lượt theo lịch và lượt push lên main chung nhóm `concurrency` → push chờ tới 6 giờ hoặc bị hủy. Sửa: nhóm
   `ci-${{ github.event_name }}-${{ github.ref }}`; test `TestCI_ConcurrencySeparatesScheduleFromPush`.
2. RUNBOOK ghi `error-spike` mức urgent, mã gửi normal (`errorrules.go`). Sửa sổ tay theo mã.
Ghi chú không bắt buộc đã làm: bỏ dòng `GITLEAKS_VERSION` trùng; thêm `TestCI_NoSecrets` (khớp câu ở Soát thiết kế 6.13);
REBUILD nói rõ `images.txt` chỉ có digest kéo được khi `CRM_IMAGE` ghim `@sha256`; ADR-25, 44 nói rõ con số mutation của
queue, commands là cài tay. Để lại (ghi ở "Việc phát hiện thêm"): `verify-drill.sh` còn `crm:dev`; danh sách loại tệp
của `MUTATION_HTTPAPI_EXCLUDE`.

Lượt 2 (agent `reviewer` mới, phần đã stage sau sửa): **ĐẠT**. Hai phát hiện lượt 1 đã sửa đúng; không phát hiện mới phải
sửa. Ghi chú không bắt buộc: `image-check.sh` gắn nhãn `$IMAGE-build` hỏng nếu `CRM_IMAGE` dạng `crm@sha256:…` (CI, dev
dùng `crm:<nhãn>`); `TestCI_NoSecrets` đỏ thừa nếu chú thích có chữ `secrets.`.

Kết luận: ĐẠT
