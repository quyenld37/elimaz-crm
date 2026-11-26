# Task 493 — CI trên GitHub Actions: cổng kiểm và test từ xa, chia máy

Kế hoạch: không có trong kế hoạch M4; đề bài là lời giao việc của chủ shop ngày 26/11 (mã Chung-CI1, dòng 493 của
`../../docs/superpowers/plans/danh-so-task-toan-cuc.md`). Loại việc (quy trình mục 3): A (công cụ, cấu hình CI; không
chạm mã nghiệp vụ).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm test trong gói `tests/deploy` (gói test,
  không xuất ký hiệu cho gói khác).
- Gói phụ thuộc ngược (go list): không gói nào nhập `tests/deploy`. Không đổi gói Go nào của ứng dụng.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không.
- Tệp đổi: `.github/workflows/test-remote.yml` (mới), `.github/workflows/ci.yml` (mutation chạy bằng tay thay lịch tuần),
  `scripts/ci/remote-test.sh`, `scripts/ci/lib.sh`, `scripts/ci/static-gates.sh`, `scripts/ci/split_packages.py`,
  `scripts/ci/install-tools.sh`, `scripts/ci/test-times.txt` (mới), `scripts/ci/ci-task-tu-xa.patch` (bản vá cho
  `scripts/agent/ci-task.sh`, xem "Dừng để hỏi"), `Makefile` (`ci-tools` thêm oasdiff; đích mới `test-part`; chú thích
  mutation), `tests/deploy/ci_test.go`, `tests/deploy/manual_suites_test.go` (luật ci.yml: schedule → workflow_dispatch),
  `tests/deploy/remote_ci_test.go` (mới), `CLAUDE.md`, `docs/quy-trinh-lam-viec.md`, `docs/RUNBOOK.md` và
  `docs/mutation.md` (bỏ chữ "theo lịch" của lượt mutation). Ngoài repo (không commit):
  `docs/superpowers/plans/danh-so-task-toan-cuc.md` dòng 493.
- Người dùng của công cụ: `scripts/agent/ci-task.sh` (sau khi áp bản vá), lệnh gộp của điều phối viên (gọi
  `scripts/ci/remote-test.sh` trực tiếp), `scripts/agent/merge-task.sh` đọc dấu `crm-ci-test-xanh` (không đổi cách ghi).
- Test sẽ chạy: `go test -count=1 ./tests/deploy/...` (luật ci.yml, test-remote.yml, chia phần, dò base, chọn chế độ,
  remote-test.sh với gh giả, bản vá áp được), `./tests/arch/...` (cổng tĩnh). Chạy thật trên GitHub Actions: lượt gói
  nhỏ, lượt gói cần PostgreSQL, một lượt `ALL`, ci-task (bản vá) ở chế độ từ xa.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có mã Go, không có
  công cụ shell. Tìm trong `scripts/`: `scripts/agent/affected.py` (chọn gói), `ci-task.sh` (`snap_tree`, các cổng tĩnh),
  `run-in-slot.py` (lượt máy), `.github/workflows/ci.yml` (SHA đã ghim của checkout, setup-go, setup-node; `make ci-tools`,
  `make db-up`), `tests/deploy/ci_test.go` (kiểu `workflow`/`job`/`step`, `runs`, `indexOf`, `needs`).
- Dùng lại / viết mới vì: dùng lại `affected.py` (ci-task vẫn tính danh sách gói như cũ), cách `snap_tree` (tree gồm tệp
  chưa commit), các đích make sẵn có (`ci-tools`, `db-up`, `gen-check`, `sqlc-vet`…), SHA action của ci.yml, kiểu và hàm
  đọc workflow của `ci_test.go` (test mới cùng gói nên dùng thẳng). Viết mới: chia gói cân bằng (chưa có), đẩy/chạy/chờ
  workflow (chưa có), dò nhánh gốc và chọn chế độ (chưa có). Cổng tĩnh chuyển thành `scripts/ci/static-gates.sh` để job
  static của GitHub và ci-task (sau bản vá) dùng chung một danh sách, không chép hai nơi.

## Soát thiết kế

- 5.1–5.10: không áp dụng: không có dữ liệu, module, transaction, HTTP hay Valkey; chỉ công cụ CI.
- 5.11 (công nghệ mới): GitHub Actions đã có sẵn (ci.yml, Task 23); action mới duy nhất `actions/cache` (của chính
  GitHub, ghim SHA) để giữ công cụ và cache lint, giảm phút. Không thêm thư viện Go hay npm. Python chỉ thư viện chuẩn
  (như `affected.py`).
- 5.12: theo mẫu sẵn có: workflow như ci.yml (ghim SHA, quyền `contents: read`, không secret), test luật workflow như
  `ci_test.go`, test script bằng Go chạy script thật như `affected_test.go`.
- 6.1 (hai lượt cùng lúc): mỗi worktree một nhánh `ci/<tên>`; concurrency theo ref, cancel-in-progress (lượt mới của
  cùng worktree hủy lượt cũ). Tìm run theo đúng `headSha` của commit tạm (duy nhất mỗi lần), không lấy "run mới nhất".
- 6.2 (gửi lại): chạy lại remote-test.sh tạo commit tạm mới, push -f, lượt cũ bị hủy; kết quả chỉ đọc từ run của sha mới.
- 6.6/6.8 (lỗi, chết giữa chừng): mất mạng, gh lỗi, không tìm thấy run → mã thoát 3 ("không chạy được từ xa"), ci-task
  lùi về chạy máy và ghi rõ; test đỏ → mã 1. Nhánh `ci/<tên>` bị xóa bằng trap khi thoát (kể cả Ctrl-C).
- 6.9 (treo): timeout-minutes cho mọi job; `go test -timeout`; tìm run có hạn 3 phút.
- 6.12 (dữ liệu cá nhân, bí mật): không secret; không gửi dữ liệu ra ngoài ngoài mã nguồn lên repo private đã có.
- 6.3–6.5, 6.7, 6.10, 6.11, 6.13: không áp dụng: không database nghiệp vụ, tiền, quy luật nghiệp vụ.

## Bằng chứng

Repo GitHub đã đổi chủ trong lúc làm (thaothe37na/elimaz-crm → quyenld37/elimaz-crm, GitHub tự chuyển hướng; remote
`origin` của repo đã đổi theo, không do task này). Phút tính tiền = tổng mỗi job làm tròn lên phút (cách GitHub tính).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| test-remote.yml: chỉ bấm tay (push chỉ ở ci/dang-ky và không chạy job), inputs base/packages/race_packages/static, quyền đọc, không secret, không image/e2e/Playwright | `TestRemoteWorkflow_DispatchOnlyWithInputs` | `go test -count=1 ./tests/deploy/` | ok (33 s, cả gói) |
| concurrency theo ref, cancel-in-progress | `TestRemoteWorkflow_ConcurrencyPerRefCancels` | như trên | ok |
| action ghim SHA, mọi job có timeout ≤ 60 phút | `TestRemoteWorkflow_PinnedAndTimeouts` | như trên | ok |
| job static: cài công cụ (cache khóa theo `make -n ci-tools`), db-up, `static-gates.sh` với base | `TestRemoteWorkflow_StaticJob` | như trên | ok |
| `make ci-tools` cài đủ công cụ, có oasdiff (thiếu ở bản cũ: lượt đầu đỏ "oasdiff not found") | `TestMakeCITools_InstallsEveryPinnedTool` | như trên | ok |
| job plan chia gói (ALL trừ e2e, trừ arch khi có static); job test ma trận, fail-fast false, kéo sẵn Valkey, `make test-part`, tóm tắt FAIL | `TestRemoteWorkflow_PlanAndTestMatrix` | như trên | ok |
| chia cân bằng theo tệp thời gian; ít gói ít phần; tối đa 6; gói chưa đo ước theo số tệp test; chọn -race | `TestSplit_BalancedByTimes`, `TestSplit_FewPackagesFewParts`, `TestSplit_MaxSixParts`, `TestSplit_UnknownPackagesByTestFiles`, `TestSplit_RaceSelection` | như trên | ok; với `test-times.txt` thật: 6 phần 413–414 s |
| dò nhánh gốc theo điểm tách mới nhất với HEAD: tách từ m4 → m4 (kể cả m4 đã đi tiếp), tách từ main / m4 đã gộp vào main / không có m4 → main; `CRM_BASE_BRANCH` thắng | `TestCILib_BaseBranch` (6 ca) | như trên | ok |
| chọn từ xa / chạy máy (CRM_CI_REMOTE=0, không origin, gh lỗi) kèm lý do | `TestCILib_Mode` (4 ca) | như trên | ok |
| cổng tĩnh đủ như ci-task, bước có điều kiện theo phần đổi | `TestStaticGates_Steps` | như trên | ok |
| remote-test.sh: commit-tree từ tree (gồm tệp chưa commit, cha HEAD), push ci/<nhãn>, dispatch đúng inputs, tìm run theo headSha, in phút, xóa nhánh, không đổi HEAD | `TestRemoteTest_GreenPushesSnapshotAndCleansUp` | như trên | ok |
| test-guard từ xa dùng đúng tệp task: task_file lấy từ trạng thái task của worktree, TASK_FILE thắng | `TestRemoteTest_TaskFileFromWorktreeState` | như trên | ok |
| tệp gói không có → mã 2, không chạy workflow (không xanh giả); `ALL` viết thẳng được | `TestRemoteTest_PackageArgs` | như trên | ok |
| đỏ: mã 1, chỉ in dòng FAIL; vẫn xóa nhánh | `TestRemoteTest_RedShowsFailures` | như trên | ok |
| gh run watch thoát sớm: hỏi lại tới khi xong; không xong thì mã 3 và hủy run | `TestRemoteTest_WatchRetriesUntilDone` | như trên | ok |
| gh lỗi / không có gh: mã 3 (ci-task lùi về máy), dọn nhánh | `TestRemoteTest_InfraErrorExit3` | như trên | ok |
| bản vá ci-task luôn áp được | `TestCITaskPatch_Applies` | như trên | ok |
| ci.yml: mutation chỉ bấm tay, không lịch; job khác không chạy khi bấm tay | `TestCI_MutationManualOnly`, `TestCI_ImagerVipsJob` | như trên | ok |
| Chạy thật, gói nhỏ (fieldcrypt -race, tools/...) không static | run 45753290827 | `remote-test.sh <tree> 7b93b02 p1 r1` | đỏ thật: tools/apicheck thiếu oasdiff, tools/loadkit thiếu CRM_TEST_DATABASE_URL → sửa (`ci-tools` + `make test-part`); test 3 phút 11 giây, 5 phút tính tiền |
| Chạy thật, gói cần PostgreSQL (`./internal/modules/identity/...` -race) | run 45754296111 | `remote-test.sh <tree> 7b93b02 p2 p2` (static=false) | **XANH**; plan 13 s, test 6 phút 11 giây; 8 phút tính tiền |
| Chạy thật, gói nhỏ có static | run 45755272963 | như dòng đầu, static=true | test (1) xanh 9 phút 18 giây; static ĐỎ đúng lỗi thật của bản nháp (gci, gocognit, gosec, jscpd trong `remote_ci_test.go`; tests/arch thiếu CRM_TEST_DATABASE_URL) → đã sửa |
| Chạy thật `packages=ALL`, race ALL, static (một lần) | run 45757593912 | `remote-test.sh <tree> 7b93b02 ALL ALL` | static **XANH** 13 phút 36 giây; 6 phần test 4–13 phút; 5 test đỏ cùng một nguyên nhân (Valkey: xem Việc phát hiện thêm) → job test kéo sẵn image; wall 13 phút 39 giây (04:49:07→05:02:46); **68 phút tính tiền**; số đo từng gói → `scripts/ci/test-times.txt` (93 gói, tổng 2.894 s) |
| ci-task (bản vá) chế độ từ xa, trong bản clone ngoài repo (`git clone --shared`, áp bản vá) | run 45757791655 | `CLAUDE_PROJECT_DIR=<clone> scripts/agent/ci-task.sh` | **XANH**: tự chọn `remote`, nhánh gốc m4, điểm tách 7b93b02; plan 21 s, static 20 phút 43 giây (cache công cụ trượt: 289 s cài; build 143 s, vet 172 s, lint 250 s, arch 198 s), test (1) 5 phút 29 giây; 28 phút tính tiền |
| Lượt bị hủy (không tính): ci-task từ clone dò ra main vì m4 đã đi tiếp (luật "HEAD chứa m4" sai) → sửa luật dò, hủy run | run 45762274770 | — | hủy sau ~3 phút |
| ci-task (bản vá) trước review, gộp m4 lần 1 (no-op) | run 45760512874 | `CLAUDE_PROJECT_DIR=<clone> scripts/agent/ci-task.sh` (bản vá, từ xa) | **CI-TASK XANH (từ xa, so với điểm tách nhánh 7b93b02)**; static 12 phút 12 giây (cache trúng), test (1) `tests/deploy` -race 2 phút 34 giây; wall 12 phút 17 giây, không chờ lượt máy; **17 phút tính tiền** |
| CI_RESULT: ci-task lần cuối, sau khi gộp m4 (fast-forward tới cf7d824) và sửa theo review | run 45763060825 | `CLAUDE_PROJECT_DIR=<clone -b task-493> scripts/agent/ci-task.sh` (bản vá, từ xa) | **CI-TASK XANH (từ xa, so với điểm tách nhánh cf7d824)**: nhánh gốc m4 dò đúng dù m4 đã đi tiếp; static 9 phút 19 giây (cache công cụ và lint trúng), test (1) `tests/deploy` -race 1 phút 57 giây; wall 9 phút 24 giây, không chờ lượt máy; **13 phút tính tiền** |

## Test bị xóa hoặc tắt

- `TestCI_MutationScheduledOnly` (tests/deploy/ci_test.go) đổi thành `TestCI_MutationManualOnly`: luật cũ đòi lịch tuần,
  chủ shop yêu cầu bỏ lịch (tiết kiệm phút); luật mới đòi workflow_dispatch và cấm schedule. Không bớt câu kiểm.

## Khác kế hoạch

- `remote-test.sh` đặt ở `scripts/ci/` thay vì `scripts/agent/` và ci-task đổi bằng bản vá chờ chủ shop áp (xem
  "Dừng để hỏi"): hook `guard-edit.sh` chặn agent sửa `scripts/agent/*`.
- test-remote.yml có thêm khối `push: branches: [ci/dang-ky]` (mọi job bỏ qua ở lượt đó, 0 phút): GitHub trả 404
  "workflow test-remote.yml not found on the default branch" khi bấm chạy workflow chưa có trên main; một lần push nhánh
  `ci/dang-ky` làm GitHub biết workflow (id 431164966), sau đó `gh workflow run --ref ci/<…>` chạy được. Bỏ khối này khi
  tệp đã lên main.
- Thêm input `task_file` (tệp task cho test-guard), đích make `test-part`, `scripts/ci/install-tools.sh`,
  `scripts/ci/static-gates.sh` (một danh sách cổng tĩnh cho job static và ci-task).

## Việc phát hiện thêm

- `internal/testkit/valkey.go` `StartValkey`: lấy mã container từ `CombinedOutput()` của `docker run -d`; máy chưa có
  image thì dòng "Unable to find image… Pulling…" lẫn vào mã → `docker port valkey: exit status 1` (lượt ALL: 5 test đỏ,
  mỗi job đúng test đầu tiên). Nên dùng `Output()` (chỉ stdout). Task này chỉ tránh bằng cách kéo sẵn image trong
  workflow (testkit là vùng rủi ro cao, ngoài phạm vi).
- `make ci-tools` thiếu oasdiff (đã sửa trong task này vì cổng oasdiff và test tools/apicheck cần): ci.yml cũng sẽ đỏ.
- Repo GitHub đã đổi chủ sang `quyenld37/elimaz-crm` giữa phiên (một lượt bị dừng SIGTERM cùng lúc, run 45754295832).

## Quyết định thay chủ shop

- Không vượt hook: không sửa `scripts/agent/`, viết bản vá `scripts/ci/ci-task-tu-xa.patch` (+ test giữ bản vá luôn áp
  được) và kiểm bản vá bằng cách chạy nó trong bản clone ngoài repo. Phương án khác: dừng hẳn phần ci-task (mất bằng
  chứng chạy thật), hay viết qua đường khác để lách hook (trái CLAUDE.md).
- Đăng ký workflow bằng push `ci/dang-ky` thay vì push main (không được phép). Phương án khác: chủ shop push riêng
  `.github/workflows/test-remote.yml` lên main rồi bỏ khối push (khuyến nghị khi gộp task).
- Dò nhánh gốc theo điểm tách mới nhất với HEAD (m4 hay main), không theo "HEAD chứa m4" như đề bài viết: m4 đi tiếp sau
  khi task tách thì HEAD không còn chứa đầu m4 và luật cũ rơi về main → chạy toàn bộ test so với main (gặp thật 26/11, run
  45762274770). Luật mới vẫn cho m4 với mọi task tách từ m4, main với task tách từ main hay sau khi m4 đã gộp vào main.
- Chia phần: tối đa 6, mỗi phần ≥ 120 s test (mỗi job tốn ~2–3 phút dựng), trọng số từ `test-times.txt` (đo ở lượt ALL),
  gói chưa đo ước theo số tệp test. ALL bỏ `tests/arch` khi static chạy (static đã chạy nó, tiết kiệm ~6 phút).
- Lượt ci-task cuối chạy bằng bản vá (từ xa, trong clone) thay cho ci-task chưa vá ở máy: ci-task chưa vá lấy base
  với main nên chạy toàn bộ test và xếp lượt máy 20–40 phút — đúng điều task này sửa; cùng cổng, base đúng (m4).

## Dừng để hỏi

- Chủ shop áp bản vá ci-task (hook chặn agent): ở gốc `apps/crm`: `git apply scripts/ci/ci-task-tu-xa.patch`, rồi
  `go test -count=1 -run TestCITaskPatch ./tests/deploy/` (bản vá ngược áp được = đã áp). Tới khi áp, ci-task vẫn chạy
  máy như cũ; điều phối viên dùng được `scripts/ci/remote-test.sh` ngay.
- Khi gộp lên main: đưa `.github/workflows/test-remote.yml` lên main (push main cần OK riêng) rồi bỏ khối `push:
  ci/dang-ky`.
- Hook `bashguard.py` chặn lệnh `git push` của agent; `remote-test.sh` push bên trong script (chủ shop OK 26/11 cho
  `ci/*`). Nếu muốn agent khác cũng dùng, cần chủ shop xác nhận đây là đường push được phép.

## Reviewer

Lượt 1: CẦN SỬA — (1) `remote-test.sh` coi tệp gói không có là "không test" → xanh giả (gõ `ALL` hay sai đường dẫn);
(2) bản vá ci-task chỉ đọc dòng "STATIC ĐỎ:", không đọc mã thoát của `static-gates.sh`. Đã sửa cả hai (mã 2 khi thiếu
tệp, nhận `ALL` viết thẳng, `TestRemoteTest_PackageArgs`; `PIPESTATUS[0]` + cổng đỏ `static-gates(thoát N)`), cùng ghi chú
không bắt buộc: hủy run khi bỏ cuộc, kiểm `jq`, bỏ dòng trùng trong `test-times.txt`, sửa chữ "theo lịch" ở RUNBOOK và
mutation.md. Sửa thêm luật dò nhánh gốc (m4 đi tiếp) gặp khi chạy thật.

Lượt 2 (sau khi gộp m4): Kết luận: ĐẠT. Không có phát hiện phải sửa. Ghi chú không bắt buộc: task tách từ m4 rồi tự gộp
main mới hơn thì dò ra main (chỉ chạy nhiều test hơn, không bỏ sót); Ctrl-C trong lúc tìm run thì run đã dispatch không
bị hủy (hiếm); CLAUDE.md chưa nêu mã 2 (đã thêm).
