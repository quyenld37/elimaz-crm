# Task 478 — affected.py: đổi tệp dữ liệu chạy test đọc tệp đó

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm hàm test (gói `deploy_test`, không xuất ra ngoài) trong `tests/deploy/affected_test.go`.
- Gói phụ thuộc ngược (go list): `tests/deploy` không được gói nào nhập (gói chỉ có test). `affected.py` được gọi bởi `ci-task.sh` (dòng 52, `--base … --split`) và `stop-check.sh` (dòng 42, `--split`); hai bên chỉ đọc stdout dạng `race|plain <gói>` (hay import path trần) và mã thoát — định dạng không đổi. Riêng `stop-check.sh` dòng 37 chỉ gọi affected.py khi có tệp `.go`/`go.mod`/`go.sum` đổi, nên thay đổi chỉ gồm tệp dữ liệu không được hook Stop test (reviewer phát hiện): bản vá sửa luôn điều kiện đó (gọi khi có tệp đổi ngoài `docs/tasks/`). `ci-task.sh` luôn gọi affected.py, không phải sửa (chú thích dòng 68 "affected.py chỉ xét tệp Go" nay cũ — ghi ở Việc phát hiện thêm).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có. Luật migration của Task 473 giữ nguyên (mọi tệp trong `migrations/` bị loại khỏi luật tệp dữ liệu).
- Hành vi đổi của affected.py: (a) tệp không phải .go trong thư mục một gói (lấy gói gần nhất phía trên, không tính gốc) → như mã của gói đó (lan lên gói nhập nó); tệp trong `<gói>/testdata/` → chỉ test gói đó, trừ khi mã chạy của gói nhúng nó (`EmbedFiles` của `go list`, vd `carriertest` nhúng `testdata`) → như mã; (b) tên tệp kèm phần đường dẫn đứng trước nó trong một chuỗi Go của tệp `_test.go`, khớp đuôi đường dẫn tệp đổi → test của gói chứa tệp test (không lan); (c) gói thêm theo (a), (b) xếp `plain`; tệp dữ liệu trong vùng rủi ro cao vẫn kéo cả vùng nhưng `plain`, chỉ `race` khi có tệp .go viết tay trong vùng đổi. Tài liệu .md ngoài thư mục gói: không theo (a), chỉ theo (b) (test đọc `docs/quyen.md`, `docs/ca-chay-tay.md` theo đường dẫn). Bộ quét (b) tách token Go: chỉ xét chuỗi `"…"`/`` `…` ``, bỏ chú thích và rune.
- Test sẽ chạy: `go test -count=1 ./tests/deploy/...` trên bản sao tạm có affected.py đã vá (hook không cho phiên task sửa thư mục script quy trình); ci-task trong worktree chạy với affected.py CŨ của nhánh — xem "Khác kế hoạch".

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ liệt kê mã Go (không có gì về chọn gói test). Trong affected.py đã có: `spread` (lan theo Imports), `test_only` (chỉ test gói, không lan), `direct` (race), `by_dir`, cách quét `git ls-files` + đọc thân tệp của luật migration (Task 473).
- Dùng lại / viết mới vì: dùng lại `spread`, `test_only`, `direct`, `by_dir`, `nfc`, cách quét `git ls-files -z` và danh sách loại trừ (`.worktrees/`, `spikes/`, `console/`); `EmbedFiles` lấy từ JSON `go list` đã chạy sẵn (không gọi thêm lệnh). Viết mới: `is_data`, `owner_dir`, khối tìm tên tệp trong chuỗi của `_test.go`. Test: dùng lại `affected`, `affectedRun`, `mark`, `wantKinds` có sẵn; thêm ca mới cùng kiểu Task 473 (không phụ thuộc lịch sử git).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng — task chỉ đổi công cụ chọn test (Python) và test của nó; không chạm dữ liệu, module, HTTP, database; không thêm thư viện (thư viện chuẩn Python, `go list` sẵn có).
- 6.1–6.13: không áp dụng — không database, hàng đợi, tiền, tồn, quyền. Rủi ro riêng của task là BỎ SÓT gói (test không chạy → main đỏ như Task 192) hoặc PHÌNH (máy nóng). Chống bỏ sót: luật (a) rộng (gói gần nhất phía trên), testdata do mã chạy nhúng tính như mã. Chống phình: luật (b) đòi khớp đuôi đường dẫn; gói thêm xếp `plain`; đo trên lịch sử (bảng bằng chứng). Lưới an toàn giữ nguyên: toàn bộ test -race trên main sau mỗi đợt gộp.

## Bằng chứng

Thư mục bản vá (ngoài git): `/private/tmp/claude-501/-Users-luongducquyen-Documents-Th-i-trang/3f611c4b-a555-41c3-82e1-b84aa5a61f94/scratchpad/task-478/` — `affected-478.patch` (vá `scripts/agent/affected.py` và `scripts/agent/stop-check.sh`; áp từ gốc repo bằng `git apply`), `affected.py`, `stop-check.sh` (bản đầy đủ đã vá; `*.orig.*` là bản gốc), `affected_test-478.patch` và `affected_test.go` (bản đầy đủ của `tests/deploy/affected_test.go`), `diff-<task>.txt`/`old-*`/`new-*` (số liệu đo). Bản sao tạm để chạy test: `…/task-478/clone` (git clone của nhánh + `git apply affected-478.patch` + chép test + `git add -u` trong bản sao, để test mới được `git ls-files` thấy như sau khi commit). Số liệu dưới là lần chạy sau khi sửa theo reviewer lượt 1.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test mới đỏ trên affected.py cũ (test trước) | 5 ca `TestAffected_DataFile*`, `TestAffected_TestdataOnlyOwnTestsUnlessEmbedded` | `go test -count=1 -run 'TestAffected_(DataFile\|Testdata)' ./tests/deploy/` (worktree, script cũ) | `FAIL` cả 5: vd `tools/routegen: muốn "plain", có ""`, `không --split: muốn có tools/routegen, có [] (thoát 0)` |
| (b) đổi openapi.yaml chọn tools/routegen (có/không --split), không -race | `TestAffected_DataFileSelectsTestsReadingIt` | `go test -count=1 -run 'TestAffected' ./tests/deploy/` (bản sao đã vá) | `ok elimaz.com.vn/crm/tests/deploy 10.139s` (mọi TestAffected cũ + mới) |
| (b) chỉ trùng tên không đủ; chuỗi có phần đường dẫn khớp đuôi thì chọn (`"../../../docs/quyen.md"` → identity, không lan httpapi) | `TestAffected_DataFileNameMatchNeedsPathSuffix` | như trên | ok |
| (a) testdata → chỉ test gói; testdata mã chạy nhúng → lan (carriertest → vtptest, cả thư mục con mới `testdata/zz478moi/`) | `TestAffected_TestdataOnlyOwnTestsUnlessEmbedded` | như trên | ok |
| (a) tệp trong thư mục gói → lan (catalog → httpapi); .md ngoài gói → không gói nào | `TestAffected_DataFileInPackageDirSpreads` | như trên | ok |
| (c) tệp dữ liệu trong vùng rủi ro cao: cả vùng nhưng plain; thêm .go viết tay → race | `TestAffected_DataFileInHighRiskIsPlainUnlessCodeChanges` | như trên | ok |
| Cả gói tests/deploy, vet, lint trên bản đã vá | cả gói | `go vet ./tests/deploy/`; `golangci-lint run ./tests/deploy/...`; `go test -race -count=1 ./tests/deploy/...` (bản sao) | `vet-ok`; `0 issues.`; `ok elimaz.com.vn/crm/tests/deploy 28.574s`. `bash -n stop-check.sh` (bản vá) → `bash-n-ok` |
| Lịch sử: diff thật của Task 192 (hai cha của `90368af`: `963158c`..`18e51de`) nay chọn tools/routegen | đo tay | `git diff --name-only 963158c 18e51de \| python3 affected.py --split` (cũ, mới; HIGH_RISK_DIRS của lib.sh) | cũ 36 gói (race 6), mới 37 (race 6); thêm đúng `plain tools/routegen`. Với `--base 963158c`: thoát 0, có `plain …/tools/routegen` |
| Không phình (task gần đây, diff hai cha của commit gộp) | đo tay | như trên | 190: 79→80; 191: 42→43; 452: 79→80 (mỗi task chỉ thêm `plain tools/routegen`, vì đều đổi openapi.yaml; race không đổi). 475: 11→11, 471: 3→3, 470: 47→47, 469: 4→4, 473: 1→1 (không đổi). Đo trong bản sao có test mới đã vào index: test mới không tự kéo `tests/deploy` (đường dẫn thật trong test viết tách bằng `dataPath`). 476: thoát 4 cả hai bản (tests/drills, có từ trước). 193: chưa gộp. |
| Đầu vào rỗng, chỉ tệp task; tài liệu test đọc | đo tay | `printf '' \| …`; `echo docs/tasks/task-478.md \| …`; `echo docs/quyen.md \| …` | rỗng, tệp task: thoát 0, không gói nào; quyen.md: `plain …/internal/modules/identity` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Hook không cho phiên task sửa hay `git add` `scripts/agent/`. Theo đề bài: bản vá `affected.py` và test mới nằm ở thư mục bản vá phía trên (ngoài git), đã chạy trên bản sao tạm. Test mới KHÔNG được commit trong lượt này: trên nhánh còn affected.py cũ, 5 ca mới đỏ (đúng thiết kế test trước) nên ci-task/hook sẽ đỏ. Lượt này chỉ commit tệp task.
- **Việc của điều phối viên:** (1) từ gốc repo `git apply …/task-478/affected-478.patch` (đổi `scripts/agent/affected.py` và `scripts/agent/stop-check.sh`) và commit; (2) mở lại phiên task 478 để chép `…/task-478/affected_test.go` vào `tests/deploy/affected_test.go` (hoặc `git apply affected_test-478.patch`), chạy `go test -count=1 -run TestAffected ./tests/deploy/`, reviewer, commit.
- Luật (a) dùng thêm `EmbedFiles` của `go list` (đề bài nói testdata → chỉ test của gói; nhưng `internal/adapters/carrier/carriertest` nhúng `testdata` vào mã chạy, nên testdata đó tính như mã) — xem Quyết định thay chủ shop.

## Quyết định thay chủ shop

1. **testdata do mã chạy nhúng → như mã (lan).** Đề bài: tệp trong testdata chỉ chạy test của gói. Thực tế `carriertest/fixtures.go` có `//go:embed testdata` trong mã chạy, `vtptest` nhập nó; theo đúng đề bài thì đổi fixture sẽ không chạy test của vtptest (lỗ giống Task 192). Chọn: tra `EmbedFiles` (cả thư mục con chứa tệp nhúng, để bắt tệp mới/xóa). Phương án khác: theo đúng chữ đề bài (bỏ sót), hoặc coi mọi testdata như mã (phình).
2. **Luật (b) đòi khớp đuôi đường dẫn**, không chỉ trùng tên: lần đo đầu, chỉ so tên làm Task 192/190/191/452 kéo thêm `tools/catalog` (chuỗi `"testdata/ts/index.ts"` trùng tên `console/src/api/index.ts`). Phần đường dẫn đi kèm tên trong chuỗi (bỏ `.`, `..`) phải là đuôi đường dẫn tệp đổi; chuỗi chỉ có tên (`"openapi.yaml"` trong `filepath.Join`) vẫn khớp. Phương án khác: chỉ so tên (rộng hơn, an toàn hơn chút, thêm gói vô ích).
3. **.md trong thư mục gói tính như tệp dữ liệu của gói** (đề bài chỉ bỏ .md ngoài thư mục gói); hiện chỉ có `scripts/external-probe/README.md`. **.md ngoài thư mục gói vẫn theo luật (b)** — khác chữ đề bài (bỏ hẳn): reviewer chỉ ra `internal/modules/identity/permissions_doc_test.go` đọc `docs/quyen.md`, `tests/deploy/manual_suites_test.go` đọc `docs/ca-chay-tay.md`; bỏ hẳn thì sót đúng kiểu Task 192. Nhờ luật khớp đuôi đường dẫn, đo lại không phình (tệp task `docs/tasks/task-NN.md` không chọn gói nào). Phương án khác: bỏ hẳn .md (sót hai test trên).
4. **Tệp dữ liệu trong vùng rủi ro cao:** vẫn chạy cả bộ của vùng (luật cũ: "chạm vùng thì chạy cả bộ") nhưng `plain`; trước đây mọi tệp (cả .md) trong vùng kéo cả vùng vào `-race`. Theo ý (c) của đề bài.
5. Tệp đổi chỉ là dữ liệu/tài liệu giờ vẫn gọi `go list` (vài giây) thay vì thoát ngay — cần để biết thư mục gói.
6. **Hook Stop:** sửa `scripts/agent/stop-check.sh` dòng 37 trong cùng bản vá để thay đổi chỉ gồm tệp dữ liệu cũng gọi affected.py (bỏ qua `docs/tasks/`, tệp đổi mỗi lượt). Phương án khác: để nguyên, chỉ ci-task bắt (hook Stop lọt).
7. Không bắt được: test dựng tên tệp lúc chạy (nối chuỗi, glob theo đuôi như `migrations/*.sql`). Lưới an toàn giữ nguyên: toàn bộ test trên main sau mỗi đợt gộp.

## Việc phát hiện thêm

- Chú thích `scripts/agent/ci-task.sh` dòng 68 ("affected.py chỉ xét tệp Go…") nay cũ một phần. Không sửa (tệp quy trình, ngoài phạm vi).
- Task 476 (và mọi task đổi `tests/drills/*.go`): affected.py thoát 4 (`tests/drills` chỉ có tệp nhãn build, không khớp gói) → ci-task chạy toàn bộ test. Có từ trước, không do task này.

## Reviewer

Kết luận: ĐẠT (lượt 2; review bản vá ngoài git ở thư mục bản vá và tệp task — commit này chỉ có tệp task).

- Lượt 1 — CẦN SỬA, 3 phát hiện, đã sửa: (1) test mới chứa nguyên đường dẫn `openapi/openapi.yaml`, `console/src/api/index.ts` nên sau khi commit sẽ tự kéo `tests/deploy` vào mọi task M5 → viết tách bằng `dataPath`, bộ quét bỏ chú thích; đo lại không phình; (2) bỏ hẳn .md ngoài gói làm sót `docs/quyen.md` (identity), `docs/ca-chay-tay.md` (tests/deploy) → .md vẫn theo luật (b), thêm ca test; (3) `stop-check.sh:37` không gọi affected.py khi chỉ đổi tệp dữ liệu → vá luôn trong cùng bản vá.
- Lượt 2 — ĐẠT, không phát hiện phải sửa. Ghi chú không bắt buộc: hook Stop nay chạy thêm `go build` + `go list` khi chỉ đổi tài liệu ngoài `docs/tasks/` hay `console/` (vài giây, đã chọn ở Quyết định 6).
- ci-task trên worktree (chỉ tệp task đổi): `CI-TASK XANH (so với điểm tách nhánh 070272d…)`; lần chạy đầu bị dừng do hết thời hạn chạy nền (chờ lượt máy), chạy lại một lần.
