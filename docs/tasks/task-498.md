# Task 498 — Chung-CI4 gitleaks chỉ quét phần của lượt, bể vps tắt mặc định, sửa 3 test chập chờn

Kế hoạch: task chung Chung-CI4 (dọn cổng kiểm và test chập chờn), không nằm trong bảng kế hoạch; nhánh gốc `m4`
(`../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md`). Nguồn việc: giao việc 26/11, `task-494.md`–`task-497.md`.
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Đổi chữ ký hàm không xuất `newWebCache` (gói `main`
  của `cmd/crm`; 1 chỗ gọi `cmd/crm/main.go:519` + 1 chỗ trong `cmd/crm/webcache_test.go`): thêm tham số hạn lệnh
  Valkey (0 = 50 ms mặc định của `cache.Dial`; mã chạy thật truyền 0). Còn lại chỉ sửa tệp test.
- Gói phụ thuộc ngược (go list): `cmd/crm` là gói `main` (không ai nhập). Tệp test của `internal/adapters/facebook`,
  `internal/modules/fulfillment`, `tests/deploy` không đổi gói nào khác. Không chạm `internal/testkit` (chạm thì ci-task
  chạy toàn bộ).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không. Test fulfillment đọc `platform.jobs` (status, last_error) và
  `fulfillment.shipments.create_job_id` bằng câu SQL trong test (chủ bảng, ngữ cảnh tenant).
- Công cụ CI: `Makefile` (đích `gitleaks` nhận `GITLEAKS_LOG_OPTS`), `scripts/ci/static-gates.sh` (gitleaks chỉ quét
  `BASE..HEAD`), `scripts/ci/pool.py` (vps 0 chỗ mặc định), đầu tệp `scripts/ci/ci-task-tu-xa.patch` (chú thích ngoài
  phần diff), `CLAUDE.md`, `docs/RUNBOOK.md`. Không sửa `scripts/agent/` (hook chặn).
- Test sẽ chạy: `tests/deploy` (`TestStaticGates_Steps`, test gitleaks mới, `TestMakeCI_RunsEveryGate`, `TestPool_*`,
  `TestCITaskPatch_Applies`, test merge-test dùng pool); `internal/adapters/facebook` `TestDo_HangAfterReadTimesOut`;
  `cmd/crm` `TestWebCache_*`; `internal/modules/fulfillment` `TestLabels_*`. Chạy lặp ở bể bằng `merge-test.sh` với tệp
  gói chỉ 3 gói đó.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit: `DrainDue`,
  `DrainJobs`, `MakeDueSoon`); hằng `valkeyTestOpTimeout` của Task 496 ở các gói test khác; `cache.DialOptions.OpTimeout`;
  helper `newRepo`/`commit`/`git`/`makeDry`/`staticDry` của `tests/deploy`; `fakegraph.HangAfterRead`.
- Dùng lại / viết mới vì: dùng lại `cache.DialOptions.OpTimeout` (không thêm trường cấu hình); hằng test 2 s khai riêng
  trong `cmd/crm` như Task 496 (một dòng; không đưa vào testkit để không buộc ci-task chạy toàn bộ); `HangAfterRead`
  có sẵn, chỉ đổi thời gian treo trong test; test gitleaks dùng `newRepo`/`git` và chạy đúng công thức `make -n gitleaks`
  in ra (không chép lệnh). Việc chờ lệnh tạo trong `labels_test.go` viết mới (một helper của gói test): `DrainJobs` đưa
  MỌI việc hẹn về hạn (cả lịch tra trạng thái) nên có thể đổi trạng thái vận đơn của các test nhãn; cần đưa đúng lệnh
  tạo về hạn.

## Soát thiết kế

- 5.1–5.10: không áp dụng: không đổi module, transaction, HTTP, quyền, database của mã chạy thật; `newWebCache` chỉ thêm
  tham số truyền thẳng vào `cache.DialOptions` (mã thật truyền 0, giữ 50 ms QĐ22).
- 5.11 (công nghệ): không thêm công cụ; `gitleaks git --log-opts` là cờ có sẵn của gitleaks 8.30.1 đã ghim.
- 5.12 (theo mẫu): bể tắt = `SLOTS[pool] == 0` như cơ chế `CRM_CI_POOL_SLOTS` của 494/495; hạn Valkey của test như 496.
- 6.1 (nhiều phiên cùng lúc): gitleaks chỉ quét lịch sử của lượt (`BASE..HEAD`), nên nhánh tạm `ci/*` của phiên khác đã
  fetch về runner không còn làm đỏ lượt của mình (lỗi 26/11). `make gitleaks` ở máy (không biến) giữ nghĩa cũ (mọi
  nhánh, `make ci`). Lịch sử trước BASE đã được quét ở lượt của chính nó; thư mục làm việc vẫn quét cả (`gitleaks dir`).
- 6.8/6.9 (treo, hạn): test Facebook: máy giả treo 1 phút (hay tới khi bên gọi bỏ) thay vì 11 s, nên hạn 10 s của client
  luôn tới trước kể cả khi tiến trình bị dừng hơn 1 s (lý do đỏ `<nil>` ở ci-task 172: hai bộ đếm 10 s và 11 s cùng chín
  sau một lần dừng, máy giả trả lời trước khi client thấy hạn). Test fulfillment: chờ theo điều kiện (lệnh tạo kết thúc),
  có trần số vòng chống treo; kết thúc không phải done thì đỏ kèm trạng thái và lỗi của lệnh.
- 6.12 (bí mật): test gitleaks dựng chuỗi dạng khóa lúc chạy (không nằm trong mã), như `exportconfig_test.go`; `--redact`.
- 6.2–6.7, 6.10, 6.11, 6.13: không áp dụng: không đổi tiền, khóa, hàng đợi, gửi ra ngoài của mã chạy thật.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Việc 1 — cổng tĩnh gọi `make gitleaks GITLEAKS_LOG_OPTS="$base..HEAD"`; đỏ trước khi sửa | `TestStaticGates_Steps` | `go test -count=1 -run '^(TestGitleaks_ScansOnlyRunRange\|TestStaticGates_Steps)$' ./tests/deploy/` (trước khi sửa mã) | trước: `FAIL … cổng tĩnh thiếu "make gitleaks GITLEAKS_LOG_OPTS=…..HEAD"`, `GITLEAKS_LOG_OPTS phải tới gitleaks git --log-opts`; sau: `ok` |
| Việc 1 — bí mật chỉ ở nhánh `ci/task-khac`: lượt `BASE..HEAD` xanh; `make gitleaks` không biến vẫn thấy (nghĩa cũ, không `--log-opts`); bí mật trong `BASE..HEAD` đã xóa ở commit sau vẫn đỏ, không in giá trị (gitleaks thật, chạy đúng công thức `make -n gitleaks`) | `TestGitleaks_ScansOnlyRunRange` | như trên | `ok elimaz.com.vn/crm/tests/deploy` |
| Việc 1 — trên runner thật, cổng tĩnh (gồm gitleaks mới) xanh dù GitHub có nhiều nhánh `ci/*` | job static của ci-task | `scripts/agent/ci-task.sh` | run 45862288834 (blacksmith): `static: success, 3 phút 14 giây`; `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh ebf50c54…)` |
| Việc 2 — vps mặc định 0 chỗ: không được chọn kể cả 1 gói và mọi bể khác hỏng (báo `bỏ bể vps: tắt mặc định (CRM_CI_POOL_SLOTS=vps=1 bật lại)`), khi đó mac dự phòng; `vps=1` bật lại; ép vps khi tắt: mã 2, không chạy; `status`: `vps: 0/0 chỗ bận` | `TestPool_VPSOffByDefault` (mới); luật vps khi bật: `TestPool_FastestFreeFirst`, `TestPool_VPSOnlySmallSets`, `TestPool_ForcedRunner`, `TestPool_MonthlyMinutesBudget`; lỗi hạ tầng chỉ thử 3 bể: `TestPool_InfraErrorMarksDownAndMovesOn` | `go test -count=1 -run '^(TestGitleaks_ScansOnlyRunRange\|TestStaticGates_Steps\|TestPool_.*\|TestMakeCI_RunsEveryGate\|TestCITaskPatch_Applies\|TestMergeTest.*)$' ./tests/deploy/` | trước khi sửa `pool.py`: `--- FAIL: TestPool_VPSOffByDefault`, `--- FAIL: TestPool_InfraErrorMarksDownAndMovesOn`; sau: `ok elimaz.com.vn/crm/tests/deploy 14.917s`; log ci-task thật in `== bỏ bể vps: tắt mặc định (CRM_CI_POOL_SLOTS=vps=1 bật lại)` |
| Việc 3 — Facebook: máy giả treo 1 phút, client bỏ ở hạn 10 s (maybe_sent/unknown, DeadlineExceeded, một lần gọi) | `TestDo_HangAfterReadTimesOut` | `go test -race -count=1 -run '^TestDo_HangAfterReadTimesOut$' ./internal/adapters/facebook/` | `ok …/facebook 11.581s` |
| Việc 3 — webcache: hạn lệnh Valkey của test 2 s qua tham số mới của `newWebCache` (mã thật truyền 0 = 50 ms) | `TestWebCache_PriceChangedRefreshesProduct`, `TestWebCache_StockChangedRefreshesAvailability` | `go test -race -count=1 -run '^TestWebCache_' ./cmd/crm/` | `ok …/cmd/crm 11.086s` |
| Việc 3 — fulfillment: lệnh tạo còn "chưa tới hạn" sau drain (giả lập đồng hồ lùi: run_at + 1 phút) → `awaitCreated` đưa đúng lệnh đó về hạn, ra mã hãng; việc hẹn khác giữ lịch | `TestLabels_AwaitCreatedPastClockStep` (mới), mọi `TestLabels_*` (qua `shippedOf`) | `go test -race -count=1 -run '^TestLabels_' ./internal/modules/fulfillment/` | `ok …/fulfillment 6.537s`; `--- PASS: TestLabels_AwaitCreatedPastClockStep (0.64s)` |
| Việc 3 — không chập chờn khi chạy lặp ở bể: cùng tree 4c1a362a, 3 gói (cmd/crm, facebook, fulfillment, -race) 2 lượt song song, cộng lượt ci-task là 3 lượt xanh | các gói nêu bên | `CRM_CI_STATIC=false scripts/ci/merge-test.sh 4c1a362a ebf50c54 <tệp 3 gói> <tệp 3 gói>` ×2 | run 45862894797 `MERGE-TEST XANH (blacksmith)`; run 45862897718 `MERGE-TEST XANH (blacksmith)` |
| Việc 4 — bản vá vẫn áp ngược được sau khi thêm chú thích đầu tệp | `TestCITaskPatch_Applies` | `go test -count=1 -run '^TestCITaskPatch_Applies$' ./tests/deploy/` | `ok` |
| Tự soát trước ci-task | — | `make jscpd`; `golangci-lint run ./cmd/crm/... ./internal/adapters/facebook/... ./internal/modules/fulfillment/... ./tests/deploy/...`; `go test -count=1 ./tests/arch/` | `Found 0 clones`; `0 issues.`; `ok tests/arch 130.159s` |
| Gộp m4 | — | `git merge-base --is-ancestor m4 HEAD` | m4 (ebf50c54) đã là gốc của nhánh: không có gì để gộp |

## Test bị xóa hoặc tắt

- `TestDo_HangAfterReadTimesOut`: trần chống treo đổi 20 s → 1 phút (= thời gian máy giả treo; quá nó thì máy giả đã tự
  trả lời và câu kiểm lỗi đã đỏ trước). Ngưỡng dưới chặt hơn: 9,5 s → 10 s (hạn đặt sau `start` nên không thể sớm hơn).
  Lý do: chính cặp 10 s/11 s là gốc chập chờn; không bỏ câu kiểm nào.
- `TestPool_InfraErrorMarksDownAndMovesOn`: số lần thử khi mọi bể mã 3 đổi 4 → 3 (và 5 → 4 kèm mac) vì vps tắt mặc định
  (hành vi mới theo giao việc); thêm câu kiểm không thử vps. Các test luật vps khác bật lại vps bằng `CRM_CI_POOL_SLOTS=vps=1`
  và giữ nguyên câu kiểm.

## Quyết định thay chủ shop

- Ép `CRM_CI_RUNNER=vps` khi vps đang tắt mặc định: từ chối mã 2 kèm cách bật (`CRM_CI_POOL_SLOTS=vps=1`), như mọi bể
  0 chỗ trước đây. Lý do: chủ shop muốn bỏ hẳn vps; một biến ép nằm sót trong môi trường không được lặng lẽ gửi việc tới
  máy chậm. Phương án khác: cho ép chạy như mac — dễ đảo ngược (một dòng trong `cmd_run`).
- gitleaks ở cổng tĩnh quét `BASE..HEAD` cả khi ci-task chạy ở máy (cùng `static-gates.sh`); `make gitleaks`, `make ci`
  giữ quét mọi nhánh. Lý do: "phần của lượt" là đúng nghĩa của cổng một task; lịch sử trước BASE đã qua cổng ở lượt của nó.
- Test fulfillment: không đổi `testkit` (lề 0,5 s của `DrainDue`) mà chờ đúng lệnh tạo trong helper của gói test. Lý do:
  đổi testkit buộc ci-task chạy toàn bộ và đổi hành vi của mọi gói; lề lớn hơn sẽ đụng lịch thử lại 1 s của `files`.
  Nguyên nhân gốc là suy luận (không có log của lần đỏ): mọi đường khác để lại vận đơn không mã đều làm lệnh kết thúc
  (in_doubt/dead) — với trường hợp đó helper vẫn đỏ, nay kèm trạng thái và `last_error` của lệnh để lần sau biết gốc.

## Khác kế hoạch

- Việc 4: chỉ thêm chú thích ở đầu `scripts/ci/ci-task-tu-xa.patch` (ngoài phần diff). Dòng `+` trong bản vá (chú thích
  đầu `ci-task.sh` ghi thứ tự bể của 494) không sửa: bản vá đã áp, sửa dòng `+` thì bản vá ngược không còn khớp
  (`TestCITaskPatch_Applies` đỏ). **Cho điều phối viên/chủ shop:** sửa chú thích đầu `scripts/agent/ci-task.sh` bằng tay
  (thứ tự: blacksmith → ubicloud → github; vps tắt mặc định; mac dự phòng cuối) rồi làm lại bản vá.

## Việc phát hiện thêm

- Lệnh chạy nền dạng `mkdir … ; scripts/agent/ci-task.sh > …` bị hook chặn (nhắc `scripts/agent/` trong lệnh ghép): chạy
  ci-task đúng một lệnh trơn.

## Reviewer

Kết luận: ĐẠT (lượt 1, không có phát hiện phải sửa). Reviewer đối chiếu đủ hai chỗ gọi `newWebCache`, các câu kiểm của
`TestGitleaks_ScansOnlyRunRange`, `TestPool_VPSOffByDefault`, `TestLabels_AwaitCreatedPastClockStep`, ngưỡng mới của
`TestDo_HangAfterReadTimesOut` (hạn đặt sau `start`), bản vá chỉ đổi phần trước `diff --git`.
Ghi chú không bắt buộc (không sửa): `awaitCreated` cũng chờ được lệnh tạo pending vì lý do khác lệch đồng hồ (chỉ nới
phần dựng dữ liệu, câu kiểm của test nhãn giữ nguyên; xem "Quyết định thay chủ shop"); `off_reason` ghi "tắt mặc định"
cả khi người dùng tự đặt `vps=0` (chỉ câu chữ); chú thích cũ "vps" còn ở `scripts/ci/merge-test.sh:3` và đầu
`tests/deploy/pool_test.go` (dòng của Task 494, 495; dòng Task 498 đã thêm bên dưới).
