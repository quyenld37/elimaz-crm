# Task 499 — Chung-CI5 chạy e2e Go và Playwright ở bể Blacksmith

Kế hoạch: task chung Chung-CI5 (e2e ở bể), không nằm trong bảng kế hoạch; nhánh gốc `m4`
(`../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md`). Nguồn việc: giao việc 27/11 (chủ shop 26/11:
"gộp m4 vào main phải test full luôn e2e"; máy Mac chỉ làm task, test chạy ở bể), `task-493.md`–`task-498.md`.
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ sửa công cụ CI và test của `tests/deploy`.
- Gói phụ thuộc ngược (go list): `tests/deploy` chỉ có tệp test, không gói nào nhập. Không chạm `internal/**`, `testkit`.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không (console chỉ được chạy Playwright ở bể, không sửa mã console).
- Câu sqlc, bảng, pha migration: không.
- Công cụ CI: `.github/workflows/test-remote.yml` (input `e2e`, job `e2e` chạy `go test -race ./tests/e2e/...` trên
  compose dev đủ profile như `make dev`, job `console-e2e` chạy `make console-e2e` có cache trình duyệt Playwright),
  `scripts/ci/remote-test.sh` (biến `CRM_CI_E2E=1` → `-f e2e=true`; từ chối với vps; in dòng lỗi Playwright),
  `scripts/ci/merge-test.sh` (truyền `CRM_CI_E2E`; lượt e2e không bao giờ chạy ở Mac/vps), `CLAUDE.md`,
  `docs/RUNBOOK.md` (lệnh của điều phối viên). Không sửa `scripts/agent/` (hook chặn). Test: tệp mới
  `tests/deploy/remote_e2e_test.go`; `remote_ci_test.go` (gh giả `fakeGH` thêm `FAKE_PW_FAIL` in dòng Playwright đỏ;
  danh sách chữ cấm của `TestRemoteWorkflow_DispatchOnlyWithInputs`, xem "Test bị xóa hoặc tắt").
- Test sẽ chạy: `tests/deploy` các test `TestRemoteWorkflow_*`, `TestRemoteTest_*`, `TestMergeTest_*`, `TestPool_*`
  (chạy bằng `-run`); cả gói `tests/deploy` ở ci-task. Chứng minh: một lượt thật `CRM_CI_E2E=1 merge-test.sh` (toàn bộ
  + e2e) trên bản m4 hiện tại ở bể blacksmith.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: danh mục chỉ có mã Go; việc này là YAML/bash. Đã đọc
  `ci.yml` (job `console-e2e` sẵn có: `npx playwright install-deps chromium webkit` rồi `make console-e2e`), job
  `loadtest`/`test` của `test-remote.yml` (dựng compose, chọn runner), `make dev` (compose dev đủ profile).
- Dùng lại / viết mới vì: dùng lại `make dev` (đủ PostgreSQL, Jaeger, SeaweedFS ×2, Valkey, imager, fakecarrier,
  filesync — đúng những gì `tests/e2e` cần), `make test-part RACE=./tests/e2e/...` (đủ biến test như `make test`),
  `make console-e2e` (đúng lệnh máy dev), biểu thức `runs-on` theo input runner như job `loadtest`, khung `remoteEnv` và
  gh giả của `tests/deploy`. Viết mới chỉ hai job, một input và một biến.

## Soát thiết kế

- 5.1–5.10: không áp dụng: không đổi module, transaction, HTTP, quyền, database, hàng đợi của mã chạy thật.
- 5.11 (công nghệ): không thêm thư viện; action dùng đúng các bản đã ghim sha trong `test-remote.yml`
  (`actions/cache` cho `~/.cache/ms-playwright`, khóa theo `console/package-lock.json`).
- 5.12 (theo mẫu): job mới làm theo mẫu job `loadtest` (if theo input, không chạy trên vps, runs-on theo input runner,
  tóm tắt vào `GITHUB_STEP_SUMMARY`), biến mới theo mẫu `CRM_CI_LOADTEST` ở remote-test.sh (kiểm giá trị, mã 2).
- 6.1 (nhiều phiên cùng lúc): mỗi run có máy riêng, compose riêng trong job; e2e không chạy trên vps (dịch vụ dùng chung,
  test filesync và dboutage dựng/dừng container). Nhãn nhánh ci/* riêng mỗi lượt merge-test như cũ.
- 6.8/6.9 (treo, hạn): job mới có `timeout-minutes` ≤ 60 (luật `TestRemoteWorkflow_PinnedAndTimeouts`), `go test -timeout`.
- 6.12 (bí mật): không dùng secret (luật `TestRemoteWorkflow_DispatchOnlyWithInputs`), chỉ khóa dev công khai.
- 6.2–6.7, 6.10, 6.11, 6.13: không áp dụng: không đổi tiền, khóa, hàng đợi, gửi ra ngoài của mã chạy thật.

## Quyết định thay chủ shop

- Lượt `CRM_CI_E2E=1` chỉ chạy ở bể từ xa blacksmith/ubicloud/github: không lùi về Mac (kể cả HEAD sạch, kể cả khi
  không chạy được từ xa → mã 3), không vps (merge-test chọn bể như ALL; remote-test từ chối vps mã 2; job bỏ qua vps).
  Lý do: chủ shop 26/11 "máy Mac chỉ làm task, test chạy ở bể"; vps có dịch vụ dùng chung mà TestFileSync và test dừng
  database đụng tới, và job bị bỏ qua ở vps thì ra XANH giả. Phương án khác: cho Mac làm dự phòng cuối như lượt thường —
  một dòng trong `merge-test.sh` (`flags+=(--no-mac)`).
- Job e2e dựng dịch vụ bằng `make dev` (đủ profile, `--build` image imager, fakecarrier, filesync mỗi lượt), không cache
  lớp docker. Lý do: đo thật cả job chỉ 1 phút 18–25 giây trên blacksmith; thêm cache là thêm action và luật. Phương án
  khác: cache buildx — khi job chậm lên.
- `console-e2e` chạy trên máy của input runner (blacksmith khi bể chọn blacksmith), như các job khác, thay vì cố định
  `ubuntu-24.04` như `ci.yml`. Lý do: phút GitHub dùng chung với website; bể đã chọn chỗ trống.
- `ci-task` (lượt của task) không bật e2e (mặc định `e2e=false`), đúng luật chủ shop 24/11: e2e/Playwright chỉ khi xong
  toàn bộ, ở lượt gộp của điều phối viên.
- Test e2e/Playwright đỏ trên m4 không sửa ở task này (đúng giao việc): ghi ở "Việc phát hiện thêm".

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Input `e2e` (mặc định 'false'); chỉ job `e2e`, `console-e2e` theo `inputs.e2e == 'true'`, dispatch, không vps; ALL của plan vẫn trừ tests/e2e | `TestRemoteWorkflow_E2EInput` | `go test -count=1 -run '^(TestRemoteWorkflow_.*\|TestRemoteTest_.*\|TestMergeTest_.*\|TestPool_.*\|TestCITaskPatch_Applies)$' ./tests/deploy/` | `ok … 30.422s` (trước khi sửa: đỏ "input e2e phải có") |
| Job e2e: runs-on blacksmith/ubicloud/github, setup-go, `make dev` (đủ profile imager, fakecarrier, filesync, valkey), kéo Valkey, `make test-part RACE=./tests/e2e/...`, `COMPOSE_PROJECT_NAME` = dự án compose, tóm tắt; không job nào dùng ngữ cảnh `env.` trong env của job | `TestRemoteWorkflow_E2EJob` | như trên | ok (lượt đầu GitHub từ chối dispatch "Unrecognized named-value: 'env'" — đã sửa và thêm câu kiểm) |
| Job console-e2e: setup-node, cache `~/.cache/ms-playwright` khóa `hashFiles('console/package-lock.json')`, `playwright install-deps chromium webkit`, `make console-e2e` (Chromium mọi spec, WebKit .phone), tóm tắt | `TestRemoteWorkflow_ConsoleE2EJob` | như trên | ok |
| Playwright, `make dev`, tests/e2e không ở static/plan/test/loadtest | `TestRemoteWorkflow_E2EOnlyInE2EJobs` | như trên | ok |
| remote-test: `CRM_CI_E2E=1` → `-f e2e=true`, mặc định `e2e=false`, giá trị lạ / vps → mã 2 không chạy workflow | `TestRemoteTest_E2EInput` | như trên | ok (trước: đỏ) |
| remote-test in dòng spec Playwright đỏ và "N failed", không in dòng qua | `TestRemoteTest_E2EFailuresShown` | như trên | ok (trước: đỏ) |
| merge-test `CRM_CI_E2E=1`: một lệnh toàn bộ + e2e (ALL, -race ALL, static, e2e=true) ở blacksmith; không chạy được từ xa → mã 3 không Mac; ép mac → 2; bể thường hỏng + vps bật + ít gói → mã 3 không vps/Mac | `TestMergeTest_E2E` | như trên | ok (trước: đỏ) |
| Chạy thật hai job e2e ở bể (soát môi trường) | run 45866256833 | `CRM_CI_E2E=1 CRM_CI_STATIC=false CRM_CI_RUNNER=blacksmith merge-test.sh <tree> HEAD <rỗng> <rỗng>` | `e2e: failure, 1 phút 25 giây`, `console-e2e: failure, 4 phút 12 giây`, 7 phút máy; đỏ là test của m4 (dưới) |
| Chạy thật một lượt đầy đủ toàn bộ + e2e trên m4 4370f657 + phần của task (tree 9ab14f38) | run 45867168803 | `CRM_CI_E2E=1 CRM_CI_RUNNER=blacksmith scripts/ci/merge-test.sh <tree> HEAD` | 00:22:17 → 00:27:31 (5 phút 14 giây); `plan 14 s`, `test (1) success 3 phút 57 giây`, `test (2) success 3 phút 57 giây`, `static failure 2 phút 43 giây` (jscpd: đoạn trùng của chính tệp test mới — đã sửa, `make jscpd` → `Found 0 clones.`), `e2e failure 1 phút 18 giây`, `console-e2e failure 3 phút 33 giây`; 18 phút máy blacksmith (36 phút tính ×2); `MERGE-TEST ĐỎ (blacksmith)` |

| Cổng tĩnh (cả jscpd sau khi sửa) + gói đổi | `tests/deploy` (-race), `tests/arch` ở cổng tĩnh | `scripts/agent/ci-task.sh` (run 45868450679, blacksmith) | `static: success, 2 phút 45 giây`, `test (1): success`, `e2e: bỏ qua`, `console-e2e: bỏ qua` (lượt task không bật e2e), `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 4370f657…)` |

## Test bị xóa hoặc tắt

- `tests/deploy/remote_ci_test.go` `TestRemoteWorkflow_DispatchOnlyWithInputs`: bỏ "playwright", "console-e2e" khỏi danh
  sách chữ cấm trên cả tệp `test-remote.yml` (giữ "-tags e2e", "docker build", "make image"). Lý do: chủ shop 26/11 đổi
  yêu cầu — lượt gộp phải chạy e2e và Playwright ở bể. Thay bằng câu kiểm hẹp hơn `TestRemoteWorkflow_E2EOnlyInE2EJobs`
  (chỉ job e2e, console-e2e được chạy chúng) và `TestRemoteWorkflow_E2EInput` (chỉ khi `inputs.e2e == 'true'`).

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

Test đỏ thật trên m4 4370f657 (cả hai lượt 45866256833, 45867168803 giống nhau; còn lại của tests/e2e đều qua — gói
không có `t.Skip`). Không sửa ở task này, để điều phối viên mở task:

- `tests/e2e` `TestLiveFlow_CommentToConfirmedOrder` (`live_flow_test.go:232`): `POST /api/v1/live-comments/{id}/claim:
  403 bad_origin`, muốn 201. Nghi gốc: hàm `liveFlow.call` (`live_flow_test.go:182`) không đặt header `Origin` =
  `stackOrigin` cho route ghi (middleware `internal/httpapi/origin.go`), như `idempotency_test.go:85`,
  `mot_don_di_tron_test.go:244` có đặt. Lỗi của test (Task 173).
- `tests/e2e` `TestFBInboundLatency` (`fb_inbound_latency_test.go:121`): `testkit: mở luồng SSE: mã 403`. Nghi gốc:
  `/api/v1/stream` đòi quyền `realtime.subscribe` (`openapi.yaml`), test chỉ cấp `conversation.read` theo Page
  (`fb_inbound_latency_test.go:81`). Lỗi của test.
- `console/e2e/carriers.spec.ts:172` (3 ca 1512, 1280, 390): "yêu cầu tới API chưa khai trong test":
  `GET /api/v1/web-carrier-settings`. Nghi gốc: màn hãng vận chuyển gọi thêm API cài đặt hãng web (Task 488/490) mà spec
  chưa khai mock trong `fixtures.ts`/spec.
- `console/e2e/order-requests.spec.ts:213` (360 × 800, "danh sách và chi tiết đã đóng không tràn ngang với chữ dài"):
  tràn ngang 163 px (`noHorizontalScroll`). Nghi gốc: chữ dài không xuống dòng ở 360; cần kiểm trên Mac xem có phải do
  phông Linux khác không (163 px là lớn, nhiều khả năng lỗi thật).
- Không có artifact (ảnh, trace) của Playwright trên run: log đã đủ dòng lỗi; muốn tải trace thì thêm
  `actions/upload-artifact` ghim sha (việc sau, nếu cần).

## Reviewer

Kết luận: ĐẠT (lượt 1, không có phát hiện phải sửa). Reviewer đối chiếu điều kiện `if` hai chiều của mọi job, thứ tự
bước, khóa cache, đường `CRM_CI_E2E` qua pool.py → `__inner` → remote-test.sh, `--no-mac` loại cả nhánh chạy máy, 7 test
mới và các hàm hỗ trợ có thật; nới chữ cấm của `TestRemoteWorkflow_DispatchOnlyWithInputs` có ghi tên, lý do và câu
kiểm hẹp thay thế. Ghi chú không bắt buộc (không sửa): `TestRemoteWorkflow_ConsoleE2EJob` không kiểm chuỗi
`ubicloud-standard-2` trong `runs-on` (workflow có); mẫu `| tee` + `PIPESTATUS` đúng vì workflow không bật pipefail.
