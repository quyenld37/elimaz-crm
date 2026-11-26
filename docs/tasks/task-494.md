# Task 494 — CI chia máy song song: điều phối Blacksmith, Mac, GitHub, VPS

Kế hoạch: không có trong kế hoạch; đề bài là lời giao việc của chủ shop ngày 26/11 (mã Chung-CI2, dòng 494 của
`../../docs/superpowers/plans/danh-so-task-toan-cuc.md`): "các task nào làm song song mà cần test thì cứ chia cho các
máy này chạy song song". Tách từ task-493 (chưa gộp m4). Loại việc (quy trình mục 3): A (công cụ CI; không chạm mã
nghiệp vụ).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm test trong gói `tests/deploy` (gói test).
- Gói phụ thuộc ngược (go list): không gói nào nhập `tests/deploy`. Không đổi gói Go nào của ứng dụng.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không.
- Tệp đổi: `.github/workflows/test-remote.yml` (input `runner` → runs-on của static/plan/test; vps bỏ dựng compose, dùng
  cổng dịch vụ có sẵn; số phần theo máy), `scripts/ci/pool.py` (mới: bộ điều phối bể máy, khóa chỗ, sổ phút, tạm hỏng),
  `scripts/ci/remote-test.sh` (gửi `runner`, hạn bắt đầu run, ghi sổ phút), `scripts/ci/install-tools.sh` (dấu đã cài
  cho máy tự cài), `scripts/ci/merge-test.sh` (mới), `scripts/ci/ci-task-tu-xa.patch` (ci-task qua bộ điều phối),
  `tests/deploy/remote_ci_test.go`, `tests/deploy/pool_test.go` (mới), `tests/deploy/ci_test.go` (thêm trường `RunsOn`
  của job, `If` của bước để đọc workflow), `CLAUDE.md` (dòng lệnh). Ngoài repo (không
  commit): dòng 494 của `danh-so-task-toan-cuc.md`.
- Người dùng của công cụ: `scripts/agent/ci-task.sh` (sau khi áp bản vá), lệnh gộp của điều phối viên (`merge-test.sh`),
  `run-in-slot.py` (bể mac dùng chung tệp khóa `crm-test-slot.lock`, không đổi script).
- Test sẽ chạy: `go test -count=1 ./tests/deploy/...` (luật workflow, bộ điều phối với gh giả, remote-test.sh, bản vá áp
  được), `./tests/arch/...` ở cổng tĩnh. Chạy thật: 4 lượt song song qua bộ điều phối, một lượt ALL trên blacksmith.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có mã Go. Trong
  `scripts/`: `scripts/agent/run-in-slot.py` (khóa fcntl một lượt của máy), `scripts/ci/remote-test.sh`, `lib.sh`
  (`ci_mode`, `ci_base_branch`), `split_packages.py` (`--max-parts`), `install-tools.sh`; test 493 (`fakeGH`,
  `newRemoteEnv`, `newRepo`, `git`, `commit`).
- Dùng lại / viết mới vì: bể mac giữ đúng tệp khóa của `run-in-slot.py` (một lượt máy cho cả hai đường, không thêm khóa
  thứ hai) và đặt `CRM_TEST_SLOT_HELD=1` cho lệnh con; chạy từ xa vẫn qua `remote-test.sh` (thêm `runner`), chia phần vẫn
  `split_packages.py`; ci-task và merge-test dùng `ci_mode`/`ci_base_branch`. Test dùng lại `fakeGH` (thêm trạng thái
  bắt đầu run) và các hàm git của 493. Viết mới: chọn bể, giữ chỗ, sổ phút, tạm hỏng (chưa có). Python chỉ thư viện
  chuẩn (`fcntl` như run-in-slot; macOS không có lệnh `flock`).

## Soát thiết kế

- 5.1–5.10: không áp dụng: không có dữ liệu nghiệp vụ, module, transaction, HTTP hay Valkey; chỉ công cụ CI.
- 5.11 (công nghệ mới): Blacksmith và runner tự cài trên VPS do chủ shop dựng sẵn (đề bài); không thêm action mới,
  thư viện Go/npm hay Python ngoài thư viện chuẩn.
- 5.12: theo mẫu sẵn có: khóa fcntl như `run-in-slot.py`; test script bằng Go chạy script thật như 493.
- 6.1 (nhiều phiên cùng lúc): mỗi chỗ một tệp khóa `crm-runner-<bể>.<n>.lock`, `flock` độc quyền không chờ; tiến trình
  chết thì hệ điều hành nhả khóa; lệnh con kế thừa khóa nên chỗ còn giữ khi lệnh con còn chạy. Sổ phút ghi thêm dòng dưới
  khóa riêng của tệp sổ. Mỗi lượt từ xa một nhánh `ci/<nhãn>` (concurrency theo ref).
- 6.2 (gửi lại): bể hỏng (mã 3) → đánh dấu 30 phút, chọn lại bể khác; run đã gửi bị hủy trước khi thử bể khác.
- 6.6/6.8 (lỗi, chết giữa chừng): run không bắt đầu sau 3 phút → hủy, mã 3; mã 1 (đỏ thật) không thử lại; mã 2 không
  đánh dấu hỏng.
- 6.9 (treo): hạn tìm run, hạn bắt đầu run, timeout-minutes của job.
- 6.12 (bí mật): không secret; sổ phút chỉ ghi mã run, bể, phút.
- 6.3–6.5, 6.7, 6.10, 6.11, 6.13: không áp dụng: không database nghiệp vụ, tiền, quy luật nghiệp vụ.

## Bằng chứng

Phút máy = tổng mỗi job làm tròn lên phút; phút tính = phút máy × 2 (blacksmith), × 1 (github), × 0 (vps). Lượt chạy thật
gửi từ một bản clone ngoài repo (`git clone --shared`, commit tạm dựng bằng `git commit-tree` của đúng phần đang sửa,
không đổi nhánh task-494). Thời gian tính từ lúc gọi tới lúc có kết quả (wall).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chọn chỗ trống nhanh nhất blacksmith → mac → github → vps; mac đặt `CRM_TEST_SLOT_HELD=1` | `TestPool_FastestFreeFirst` | `go test -race -count=1 -run '^(TestPool_\|TestMergeTest_)' ./tests/deploy/` | ok (11 s) |
| Hết chỗ: chờ chỗ ĐẦU TIÊN trống ở bất kỳ bể nào, in "== chờ lượt test của máy" | `TestPool_WaitsForFirstFreeSlot` | như trên | ok |
| vps chỉ ≤ 15 gói, không ALL, không khi chưa biết số gói; không bể nào nhận → mã 3, không chạy lệnh | `TestPool_VPSOnlySmallSets` (4 ca) | như trên | ok |
| Ép bể `CRM_CI_RUNNER`: bỏ qua tạm hỏng/luật vps, chờ đúng bể đó; bể lạ mã 2; `--local-only` thắng | `TestPool_ForcedRunner` | như trên | ok |
| Sổ phút theo tháng UTC, blacksmith ×2 hết ở 2.700, github hết ở 1.600, tháng trước không tính; `status` | `TestPool_MonthlyMinutesBudget` | như trên | ok |
| Mã 3 trên bể từ xa → tạm hỏng 30 phút, chuyển bể; hết hạn dùng lại; mã 1 không thử lại; mọi bể mã 3 → trả 3 sau 3 lần | `TestPool_InfraErrorMarksDownAndMovesOn` | như trên | ok |
| 12 lượt cùng lúc: không bể nào vượt số chỗ, đủ 12 lượt, lấp đủ 4 chỗ blacksmith | `TestPool_ConcurrentNeverExceedsSlots` | như trên | ok |
| Tiến trình giữ chỗ chết (kill -9 cả nhóm): chỗ trống lại | `TestPool_SlotFreedWhenProcessDies` | như trên | ok |
| Lồng nhau chạy luôn ở bể đã giữ; `CRM_CI_POOL_SLOTS` đổi/tắt số chỗ | `TestPool_NestedRunsDirectly`, `TestPool_SlotsOverride` | như trên | ok |
| merge-test.sh: commit gộp qua bộ điều phối, base = cha thứ nhất, ALL, ghi sổ; đỏ mã 1; ép mac với bản khác HEAD mã 2; không từ xa → mac (HEAD sạch) hay mã 3; tệp gói thiếu mã 2 | `TestMergeTest_RunsMergedCommitThroughPool` | như trên | ok |
| merge-test (sau review): tệp gói chứa `ALL` là ALL; máy dev `go list` lỗi → mã 2, không XANH; không đếm được gói → không vps | `TestMergeTest_PackageFiles`, `TestMergeTest_RunsMergedCommitThroughPool` | như dòng dưới | ok |
| merge-test (review lượt 2): nhãn nhánh mặc định riêng mỗi lượt (hai lượt không đặt `CRM_CI_LABEL` → hai nhánh `ci/merge-<thư mục>-<pid>` khác nhau); chỉ máy mà mac tắt → mã 3, không chờ mãi | `TestMergeTest_DefaultLabelPerRun` | `go test -race -count=1 -run '^(TestPool_\|TestMergeTest_)' ./tests/deploy/`; lint; `make jscpd` | ok (22 s); 0 issues; 0 clones |
| Bên gọi đã giữ lượt máy (`CRM_TEST_SLOT_HELD=1`) và chỉ chạy được ở máy: chạy luôn ở mac, không tự chờ khóa của cha | `TestPool_SlotAlreadyHeldRunsOnMac` | như trên | ok |
| Workflow: input runner (choice) → runs-on của static/plan/test; cổng dịch vụ vps 5600/20600/21600/4600; vps bỏ db-up, cache GitHub, Go cache; MAX_PARTS 2/1/6 | `TestRemoteWorkflow_RunnerInput` | `go test -race -count=1 -run '^(TestRemote\|TestInstallTools_\|TestCITaskPatch\|TestCILib\|TestSplit\|TestStaticGates)' ./tests/deploy/` | đỏ trước khi sửa workflow (25 dòng lỗi), rồi ok (22 s) |
| install-tools.sh: dấu `.ci-tools-<khóa>` bỏ cài lại; setup-node trước khi cài (vps không có npm) | `TestInstallTools_StampSkipsReinstall` | như trên | ok |
| remote-test.sh gửi `runner` (mặc định github, mac → mã 2), ghi sổ phút (blacksmith 5 → 10) | `TestRemoteTest_RunnerAndLedger` | như trên | ok |
| Run nằm hàng chờ quá hạn: hủy run, mã 3, không chờ | `TestRemoteTest_NotStartedInTimeExit3` | như trên | ok |
| Bản vá ci-task áp được, đi qua `pool.py run`, luôn in dòng CI-TASK | `TestCITaskPatch_Applies` | như trên | ok |
| Cả gói tests/deploy (không -race) | — | `go test -count=1 ./tests/deploy/` | ok (260 s) |
| Lint, mã lặp | — | `golangci-lint run ./tests/deploy/...`; `make jscpd` | 0 issues; 0 clones |
| **Chạy thật 4 lượt song song** (`CRM_CI_POOL_SLOTS=blacksmith=1,github=1` để 4 lượt phải tràn sang bể khác; static=false; mỗi lượt một gói nhỏ, -race) | merge-test.sh ×4 cùng lúc (cách nhau 1 s) | `merge-test.sh HEAD HEAD~1 <gói> <gói>` | 4 lượt vào **4 bể khác nhau**, mỗi bể 1/1 chỗ: (a) fieldcrypt → **blacksmith** run 45768881271 XANH, 153 s, 3 phút máy (6 tính); (b) idempotency → **mac** XANH, 13 s; (c) tools/catalog → **github** run 45768883687 XANH, 349 s, 6 phút; (d) tenancy → **vps** run 45768886152 ĐỎ thật sau 54 s: `npm: command not found` (vps không có Node) → thêm setup-node vào job test |
| Lượt vps sau khi sửa (ép vps) | run 45769655075 | `CRM_CI_RUNNER=vps merge-test.sh HEAD HEAD~1 <tenancy>` | **XANH**, wall ~12 phút (lần đầu cài `make ci-tools` trên vps, sau đó có dấu, không cài lại), 13 phút máy, 0 phút tính |
| Lượt ALL + static trên blacksmith, lần 1 | run 45770854099 | `CRM_CI_RUNNER=blacksmith merge-test.sh HEAD HEAD~1` (ALL, -race ALL) | ĐỎ thật: jscpd (2 đoạn lặp mới trong pool_test.go), data race trong 2 test pool (đọc strings.Builder lúc tiến trình còn ghi; ở máy không chạy -race nên không thấy) → đã sửa (`outputAndCode`, `syncBuf`, `startBg`); wall 324 s, 14 phút máy |
| **Lượt ALL + static trên blacksmith, sau khi sửa** | run 45771746183 | như trên | **XANH**: plan 13 s, static 2 phút 12 giây (đủ công cụ), test (1) 3 phút 54 giây, test (2) 3 phút 13 giây (2 phần); **wall 312 s (5 phút 12 giây)**; 12 phút máy, 24 phút tính |
| CI_RESULT: ci-task (lượt máy chờ 2.698 s sau phiên task-155, rồi điều phối viên dừng và chạy lại TỪ XA bằng ci-task đã vá trên m4 2f7d2932 với scripts/ci của worktree này) | run 45782113763 | `scripts/agent/ci-task.sh` | **CI-TASK XANH (từ xa, so với điểm tách nhánh f6c6c625)**: nhánh gốc m4 dò đúng; static 14 phút 39 giây, plan 7 s, test (1) `tests/deploy` -race 5 phút 42 giây; 22 phút máy github. Tree đã test d1269bb = đúng tree hiện tại (đã so) |
| CI_RESULT lần 2, sau khi sửa theo review: cùng cổng ci-task (cổng tĩnh + `tests/deploy` -race, so với f6c6c625) qua bộ điều phối mới (hook chặn gọi ci-task của m4; ci-task chưa vá của worktree chạy trên Mac, chủ shop đã đưa test khỏi Mac) | run 45787203383 | `TASK_FILE=docs/tasks/task-494.md CRM_CI_LABEL=task-494 scripts/ci/merge-test.sh <tree 3f325d4> f6c6c625 <tests/deploy> <tests/deploy>` | **MERGE-TEST XANH (blacksmith)**: mac bị bỏ đúng (tree khác HEAD); static 3 phút 7 giây, test (1) 1 phút 20 giây; **wall 222 s**, 7 phút máy (14 tính). Sau lượt này: tệp task, và theo review lượt 2 thêm chặn `mac=0` + `--local-only` trong pool.py cùng `TestMergeTest_DefaultLabelPerRun` (chạy ở máy: test -race, lint, jscpd, dòng trên) |
| Sổ phút sau các lượt thật | — | `scripts/ci/pool.py status` | blacksmith 58/2700, github 6/1600, vps không tính; mac đang giữ bởi ci-task của task-155 |

Bảng thời gian từng bể (26/11): mac 13 s (1 gói, máy rảnh); blacksmith 153 s (1 gói), 312 s (ALL + static);
github 349 s (1 gói); vps 54 s đến lỗi, ~12 phút lần đầu (gồm cài công cụ). Tổng phút đã tiêu trong task: blacksmith 29
phút máy (58 tính), github 6, vps 15 (không tính).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không có kế hoạch; khác đề bài: đề bài gợi ý flock của shell, dùng `fcntl.flock` của Python (macOS không có lệnh
  `flock`; cùng cách `run-in-slot.py`). Khóa cache công cụ vẫn băm `make -n ci-tools` (luật của Task 493, chặt hơn hash
  `tools/versions.mk`: đổi cách cài cũng trượt cache).
- Thêm `CRM_CI_POOL_SLOTS` (đổi số chỗ) — cần cho lượt chứng minh 4 lượt rơi vào ≥ 3 bể (mặc định blacksmith 4 chỗ nuốt
  cả 4 lượt), cũng để chủ shop bớt tải một bể.
- Test job thêm setup-node (vps không có npm cho `make ci-tools`).
- pool_test: các test bộ điều phối viết trước mã nhưng chạy lần đầu sau khi viết pool.py (pool.py chưa có thì chắc chắn đỏ);
  test workflow và remote-test đã thấy đỏ trước khi sửa.

## Việc phát hiện thêm

- Sổ phút chỉ đếm lượt đi qua `remote-test.sh` của máy này, bắt đầu từ 26/11 (không có phút của Task 493 trước đó, của
  ci.yml hay của website trên cùng gói GitHub). Nếu muốn chính xác, đọc phút thật của tài khoản qua API thanh toán
  (cần quyền quản trị, ngoài phạm vi).
- vps chưa có Node: mỗi run setup-node tải Node vào thư mục công cụ của runner (giữ lại sau lần đầu).

## Quyết định thay chủ shop

- Bể mac dùng CHUNG tệp khóa với `run-in-slot.py` thay vì `crm-runner-mac.1.lock`: một lượt test của máy cho cả ci-task
  cũ (chưa vá) và bộ điều phối; khác tệp thì hai đường chạy chồng lên máy. Phương án khác: khóa riêng (đơn giản hơn, máy
  có thể chạy 2 lượt nặng cùng lúc).
- Bản vá ci-task: mã 3 trong chỗ từ xa → thoát 3 để bộ điều phối chọn bể khác (không lùi thẳng về máy như bản 493); bộ
  điều phối thoát khác 0/1 thì ci-task vẫn in "CI-TASK ĐỎ: không máy nào chạy được …" (bên chờ dò dòng CI-TASK). Mã 2 vẫn
  lùi về chạy máy như cũ.
- Plan job chạy cùng máy đã chọn (không đặt cố định ở github): phút tính đúng bể, vps không tốn phút.
- Không chạy ci-task bản vá trong clone: hook chặn lệnh gọi script trong `scripts/agent/` của clone; chạy đúng
  `scripts/agent/ci-task.sh` (chưa vá) của worktree như hướng dẫn. Bằng chứng chạy thật của đường từ xa là merge-test
  (cùng `remote-test.sh`, cùng bộ điều phối); đường ci-task bản vá được kiểm bằng test áp bản vá và đọc mã.
- 4 lượt chứng minh dùng static=false (tiết kiệm phút); cổng tĩnh trên máy mới được chứng minh bằng lượt ALL blacksmith.

## Dừng để hỏi

- Chủ shop áp bản vá ci-task (hook chặn agent): ở gốc `apps/crm`, nếu đã áp bản 493: `git apply -R` bản cũ trước (hoặc
  `git checkout scripts/agent/ci-task.sh`), rồi `git apply scripts/ci/ci-task-tu-xa.patch`; kiểm:
  `go test -count=1 -run TestCITaskPatch ./tests/deploy/`. Bản vá này thay hẳn bản 493 (cùng gốc ci-task.sh chưa vá).
- Khi gộp lên main: như Task 493 (đưa test-remote.yml lên main, bỏ khối `push: ci/dang-ky`).

## Reviewer

Lượt 1: CẦN SỬA — (1) `merge-test.sh` ở máy dev: `go list` lỗi hay tệp gói chứa `ALL` cho RACE/PLAIN rỗng →
`make test-part` thoát 0 → XANH giả; (2) nhãn nhánh mặc định `merge-<thư mục>` trùng giữa các lượt cùng lúc →
concurrency của workflow hủy run của nhau, ĐỎ giả. Đã sửa cả hai (`go list` lỗi → mã 2; tệp chứa ALL → ALL; nhãn thêm
pid; `TestMergeTest_PackageFiles`), cùng ghi chú không bắt buộc: `CRM_TEST_SLOT_HELD=1` với `--local-only` chạy luôn ở mac
(`TestPool_SlotAlreadyHeldRunsOnMac`); thêm `tests/deploy/ci_test.go` vào mục Ảnh hưởng. Không sửa: `status` giữ khóa
trong chốc lát (không sai kết quả); nhánh mã 2 của bản vá còn giữ chỗ từ xa khi lùi về máy (hiếm, chỉ chiếm thừa chỗ).

Lượt 2: CẦN SỬA — bằng chứng ghi "nhãn mặc định riêng mỗi lượt" nhưng không test nào chạy đường mặc định (môi trường test
luôn đặt `CRM_CI_LABEL=t1`). Đã thêm `TestMergeTest_DefaultLabelPerRun` (hai lượt không nhãn → hai nhánh khác nhau), cùng
ghi chú không bắt buộc: `--local-only` khi bể mac tắt trả 3 thay vì chờ mãi. Không sửa: danh sách bể tính một lần trước
khi chờ; `mark_down` ghi không nguyên tử (hậu quả chỉ là thử bể đó thêm một lần).

Lượt 3: Kết luận: ĐẠT. Không có phát hiện phải sửa; các phát hiện lượt 1–2 đã sửa và có test chạy đúng đường đó. Ghi chú
không bắt buộc (không sửa): ngoại lệ Python lạ trong pool.py thoát 1 thì bản vá ci-task không in dòng CI-TASK (hiếm); merge-test
kiểm "HEAD sạch" trước khi chờ chỗ; run bị hủy không vào sổ phút (sổ là con số ước).

## Gộp vào m4 (điều phối viên, 26/11)
- Gộp sạch (CLAUDE.md tự trộn). Thay bản vá ci-task: `git apply -R` bản 493 (ci-task.sh về đúng bản gốc bd1c9848) rồi `git apply scripts/ci/ci-task-tu-xa.patch` (bản 494); `bash -n` qua.
- Kiểm bản gộp ở bể: `CRM_CI_POOL_SLOTS=mac=0 scripts/ci/merge-test.sh <tree> HEAD` (gói ./tests/deploy, cổng tĩnh) → MERGE-TEST XANH trên blacksmith, run https://github.com/quyenld37/elimaz-crm/actions/runs/45790193366 (static 3 phút 44 giây, test 1 phút 13 giây, 7 phút máy).
- Bước sau (Task 495): bể Ubicloud (1.100 phút/tháng), bể mac 0 chỗ, chỉ dự phòng cuối.
