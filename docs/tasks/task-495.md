# Task 495 — Chung-CI3 bể Ubicloud 1.100 phút/tháng, bể mac 0 chỗ chỉ dự phòng cuối

Kế hoạch: việc chung về CI (Chung-CI3), giao trong chat 26/11, nối tiếp Task 493 (test từ xa) và 494 (bộ điều phối).
Nhánh gốc `m4`. Loại việc (quy trình mục 3): A

Mục tiêu (chủ shop 26/11): "máy t sử dụng để xử lý các task, còn chạy test thì dùng trong bể" — máy Mac không chạy test;
mọi lượt test chạy ở bể từ xa theo thứ tự blacksmith → ubicloud → github → vps; mac chỉ là dự phòng cuối.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — chỉ đổi script `scripts/ci/pool.py`,
  `scripts/ci/remote-test.sh`, `scripts/ci/merge-test.sh` (chú thích), `.github/workflows/test-remote.yml` và test
  `tests/deploy/pool_test.go`, `tests/deploy/remote_ci_test.go` (hàm test không xuất ra ngoài).
- Gói phụ thuộc ngược (go list): `tests/deploy` không được gói nào nhập.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Tài liệu: `CLAUDE.md` (dòng "Chia máy"), `docs/RUNBOOK.md` (mục CI), docstring `pool.py`.
- Người gọi bộ điều phối: ci-task.sh (đã vá 494: `pool.py run --count N|--all [--local-only]`; giao diện gọi không đổi
  nên ci-task.sh không phải sửa), `scripts/ci/merge-test.sh` (`--no-mac`, `--local-only`).
- Test sẽ chạy: khi viết `go test -race -count=1 ./tests/deploy/ -run '^(TestPool_…|TestMergeTest_…|TestRemote…)$'`;
  ci-task chạy cả gói `tests/deploy` (gói đổi). Lượt thật trên ubicloud: `CRM_CI_RUNNER=ubicloud
  scripts/ci/remote-test.sh` với một gói nhỏ.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ có mã Go; việc này
  là script CI. Đã có sẵn: `pool.py` (bể, khóa, sổ phút, tạm hỏng), cách dựng dịch vụ của bể github trong
  `test-remote.yml`, bộ `gh` giả của `remote_ci_test.go` (`newRemoteEnv`), `poolEnv` của `pool_test.go`.
- Dùng lại / viết mới vì: dùng lại toàn bộ — ubicloud là một dòng bể mới trong `POOLS`, `MULTIPLIER`, `BUDGET` và một
  nhánh nữa trong biểu thức `runs-on`/`MAX_PARTS`; mac dự phòng cuối là một nhánh trong `cmd_run`. Không viết công cụ mới.

## Soát thiết kế

- 5.1–5.10: không áp dụng: không có dữ liệu nghiệp vụ, module, transaction, HTTP hay Valkey; chỉ công cụ CI.
- 5.11 (công nghệ mới): Ubicloud do chủ shop đã nối sẵn vào org (đề bài); không thêm action, thư viện Go/npm hay Python
  ngoài thư viện chuẩn.
- 5.12: theo mẫu sẵn có của 494 (bể = dòng trong `POOLS`, khóa fcntl, sổ phút).
- 6.1 (nhiều phiên cùng lúc): ubicloud dùng chung cơ chế khóa `crm-runner-ubicloud.<n>.lock`; mac dự phòng vẫn đúng một
  chỗ, chung khóa với `run-in-slot.py`. Sổ phút ghi dưới khóa của tệp sổ. Ngân sách đọc lúc chọn bể: nhiều lượt chọn
  cùng lúc khi sổ còn dưới 1.100 phút có thể cùng gửi ubicloud — vượt tối đa vài lượt đang chạy (≤ 3 chỗ), xem
  "Quyết định thay chủ shop".
- 6.2 (gửi lại): bể từ xa mã 3 → tạm hỏng 30 phút, thử bể kế; hết bể từ xa → mac dự phòng (một lần), không lặp.
- 6.6/6.8 (lỗi, chết giữa chừng): mã 1 không thử lại; không bể nào chạy được (kể cả mac tắt hẳn) → mã 3 và ci-task
  in `CI-TASK ĐỎ: không máy nào chạy được`; không bao giờ XANH khi không bể nào chạy.
- 6.9 (treo): bể từ xa còn dùng được nhưng đầy thì chờ chỗ trống như cũ (không rơi về mac); mọi bể từ xa bị loại thì
  dự phòng mac ngay, không chờ.
- 6.12 (bí mật): không secret; Ubicloud nhận job qua ứng dụng GitHub đã cài, workflow không cần token mới.
- 6.3–6.5, 6.7, 6.10, 6.11, 6.13: không áp dụng: không database nghiệp vụ, tiền, quy luật nghiệp vụ.

## Quyết định thay chủ shop

- Vị trí của mac trong `POOLS`: cuối danh sách, 0 chỗ. `CRM_CI_POOL_SLOTS=mac=1` cho mac vào lượt chọn thường nhưng
  vẫn sau mọi bể từ xa (không còn đứng thứ hai như 494). Lý do: chủ shop muốn máy chỉ làm task. Phương án khác: giữ
  mac ở vị trí 2 khi bật — trái mục tiêu.
- `CRM_CI_POOL_SLOTS=mac=0` (ghi rõ) tắt hẳn cả dự phòng cuối (giữ đúng luật 494 "chỉ máy mà mac tắt: mã 3"); không ghi
  gì thì mac là dự phòng 1 chỗ. Phương án khác: mac=0 vẫn cho dự phòng — thì không còn cách nào cấm hẳn máy chạy test.
- Ép `CRM_CI_RUNNER=mac` (người gõ) chạy được dù mặc định 0 chỗ và không in dòng dự phòng (không phải dự phòng).
- Bể từ xa chỉ đầy (không hỏng, còn phút) thì chờ chỗ trống, không rơi về mac (đúng nghĩa "chỉ khi mọi bể từ xa tạm
  hỏng/hết ngân sách/không nhận lượt đó").
- Số phần tối đa trên ubicloud: 4 (github 6). Lý do: 1.100 phút/tháng, mỗi job tốn ~1–2 phút dựng (lượt thật: plan 22
  giây + test 5 phút 9 giây cho 1 gói, tính 7 phút). Đảo ngược: sửa một số trong `MAX_PARTS`.
- Ngân sách đọc lúc chọn bể (như blacksmith/github của 494): các lượt đang chạy khi sổ còn dưới 1.100 vẫn chạy xong, nên
  có thể vượt vài chục phút (≤ 3 lượt cùng lúc). Cutoff 1 USD ở console Ubicloud là lớp chặn thứ hai. Phương án khác:
  giữ trước phút ước lượng — phức tạp hơn, ước lượng sai thì chặn oan.
- Không sửa ci-task.sh và bản vá: giao diện gọi `pool.py run --count|--all [--local-only]` không đổi; nhánh dự phòng
  mã 2 của ci-task (chạy lại với `CRM_CI_REMOTE=0`) đi qua `--local-only` nên cũng in dòng dự phòng cuối.

## Việc điều phối viên cần làm

- Phiên làm task hiện có biến `CRM_CI_POOL_SLOTS=mac=0` trong môi trường: với bản này nó tắt HẲN cả dự phòng cuối (mọi bể
  từ xa hỏng → `CI-TASK ĐỎ: không máy nào chạy được`, không bao giờ XANH). Muốn có dự phòng mac thì bỏ biến đó (mac đã
  0 chỗ mặc định); muốn cấm hẳn máy chạy test thì giữ.
- Chú thích đầu ci-task.sh (và `scripts/ci/ci-task-tu-xa.patch`) còn ghi thứ tự cũ "blacksmith → mac → github → vps";
  chỉ là chú thích, không đổi hành vi — sửa khi có dịp (phiên task không được sửa scripts của quy trình).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Thứ tự mới blacksmith → ubicloud → github → vps, không mac khi còn bể từ xa | `TestPool_FastestFreeFirst` (4 bước) | `go test -race -count=1 -run '^(TestPool_\|TestMergeTest_\|TestRemoteWorkflow_RunnerInput\|TestRemoteTest_RunnerAndLedger)' ./tests/deploy/` | `ok elimaz.com.vn/crm/tests/deploy 14.171s` |
| Bể từ xa chỉ đầy: chờ, không rơi về mac | `TestPool_WaitsForFirstFreeSlot`, `TestPool_ConcurrentNeverExceedsSlots` (mac tối đa 0), `TestPool_SlotsOverride` (phần mặc định) | như trên | ok |
| ubicloud hết 1.100 phút (×1) thì bỏ qua, báo lý do; status có ubicloud 1100/1100 | `TestPool_MonthlyMinutesBudget` | như trên | ok |
| mac 0 chỗ mặc định; dự phòng cuối khi mọi bể từ xa bị loại, in đúng dòng `== DỰ PHÒNG CUỐI: chạy trên máy Mac (lý do: …)`; `--no-mac`/`mac=0` → mã 3 | `TestPool_MacLastResortOnly`, `TestPool_InfraErrorMarksDownAndMovesOn` (4 bể mã 3 → mac một lần; mac cũng 3 → trả 3) | như trên | ok |
| `--local-only` vẫn chạy trên mac (in dòng dự phòng); trong lượt máy đã giữ cũng in | `TestPool_MacLastResortOnly`, `TestPool_SlotAlreadyHeldRunsOnMac`, `TestPool_ForcedRunner` | như trên | ok |
| `CRM_CI_POOL_SLOTS` vẫn đổi số chỗ; `mac=1` cho mac vào lượt thường (sau bể từ xa) | `TestPool_SlotsOverride` | như trên | ok |
| Ép `CRM_CI_RUNNER=mac`/`ubicloud` | `TestPool_ForcedRunner` | như trên | ok |
| Workflow: lựa chọn ubicloud, runs-on `ubicloud-standard-2`, dựng dịch vụ như github, ≤ 4 phần | `TestRemoteWorkflow_RunnerInput` | như trên | ok |
| remote-test.sh nhận ubicloud, ghi sổ ×1 | `TestRemoteTest_RunnerAndLedger` | như trên | ok |
| merge-test qua bộ điều phối mới | `TestMergeTest_RunsMergedCommitThroughPool`, `TestMergeTest_PackageFiles`, `TestMergeTest_DefaultLabelPerRun` | như trên | ok |
| Cả gói `tests/deploy` (máy) | cả gói | `go test -count=1 ./tests/deploy/` | `ok elimaz.com.vn/crm/tests/deploy 86.842s` |
| Lượt thật trên ubicloud: runner nhận job, `make db-up` dựng dịch vụ, test PostgreSQL xanh | gói `./internal/platform/tenancy/...` (-race) | `CRM_CI_RUNNER=ubicloud CRM_CI_STATIC=false scripts/ci/remote-test.sh <tree> m4 <tệp gói> <tệp gói>` | run 45792956394: plan 22 giây, test (1) 5 phút 9 giây (gồm dựng compose), nhãn job `ubicloud-standard-2`, runner `gr3dymkpkdvy2jnvnbr1j1gymq`; 7 phút máy = 7 phút tính vào sổ; wall 389 giây; `REMOTE-TEST XANH` |
| Cổng task | ci-task (cổng tĩnh + `tests/deploy` -race, từ xa) | ci-task.sh | run 45796068864 blacksmith: static 3 phút 7 giây, test 1 phút 13 giây; `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 21998b0c…)` |
| Tự soát rẻ | lint, jscpd | `golangci-lint run ./tests/deploy/...`, `make jscpd` | `0 issues.`, `Found 0 clones.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, `git diff --cached`). Không phát hiện phải sửa. Đã soát: `cmd_run` không lặp vô hạn (mọi bể đã
chạy vào `tried`, mac dự phòng đúng một lần, mã của mac trả thẳng); không XANH khi không bể nào chạy (`--no-mac`/`mac=0`
→ mã 3 → ci-task in `CI-TASK ĐỎ`); bể từ xa chỉ đầy thì chờ; `--local-only` in dòng dự phòng ở cả hai đường; nhánh mã 2
của ci-task đi qua `--local-only`; status, workflow, remote-test.sh khớp; bảng bằng chứng khớp tên test thật.
Ghi chú không bắt buộc: `mac=1` thì mac là bể thường, không in dòng dự phòng (đã ghi ở Quyết định); vượt 1.100 vài lượt
đang chạy (đã ghi); thêm CLAUDE.md, RUNBOOK vào Ảnh hưởng (đã thêm).
