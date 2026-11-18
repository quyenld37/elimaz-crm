# Mutation testing vùng rủi ro cao

Kế hoạch M0 bản 5, Task 23 Step 4; ADR-25, ADR-44. Công cụ: gremlins v0.6.0 (chạy được với Go 1.27.4, không cần
phương án go-mutesting hay lỗi cài tay). Cấu hình `.gremlins.yaml`; lệnh `make mutation` (cần `make db-up`).

**Đột biến** (mutant) là một thay đổi nhỏ cố ý vào mã (đảo điều kiện, đổi `<=` thành `<`, đổi `+` thành `-`…). Bộ test
"giết" đột biến khi có ít nhất một test đỏ. **Hiệu quả** (test efficacy) = bị giết / (bị giết + còn sống); ngưỡng 80%.
Đột biến "không phủ" (không test nào chạy qua dòng đó) và "quá hạn" (TIMED OUT: test treo, coi như bị bắt) không tính vào
hiệu quả.

Thư mục: `make mutation` đọc thẳng `HIGH_RISK_DIRS` của hook (một nguồn, Task 392): `tenancy`, `queue`, `commands`,
`idempotency`, `platform/db`, `platform/webhooks`, `modules/identity`, `modules/inventory`; riêng `internal/httpapi` chỉ
`guard.go` (mọi tệp khác của gói bị loại, tính lúc chạy). `TestMakeMutation_CoversHighRiskDirs` và
`TestMakeMutation_SingleSourceAndFileEntries` giữ luật này. Ba gói `platform/db`, `platform/webhooks`,
`modules/inventory` chưa có lần chạy nào (lượt CI theo lịch hoặc `make mutation` sẽ đo).

## Lần chạy 18/11/2026 (máy dev, `GOFLAGS=-count=1`, 2 lượt song song)

| Gói | Bị giết | Còn sống | Quá hạn | Không phủ | Hiệu quả lần đầu | Sau test thêm | Thời gian |
|---|---|---|---|---|---|---|---|
| `internal/platform/tenancy` | 12 | 0 | 0 | 0 | 100% | 100% | 1 phút 19 s |
| `internal/platform/queue` | 66 | 19 | 6 | 60 | **77,65%** | **82,35%** (70/85) | 1 giờ 6 phút |
| `internal/platform/commands` | 27 | 7 | 0 | 17 | **79,41%** | **88,24%** (30/34) | 1 phút 54 s |
| `internal/platform/idempotency` | 15 | 0 | 0 | 3 | 100% | 100% | 15 s |
| `internal/modules/identity` | 106 | 12 | 0 | 26 | 89,83% | 91,53% (108/118) | 22 phút |
| `internal/httpapi/guard.go` | 6 | 1 | 0 | 0 | 85,71% | 100% (7/7) | 1 phút 17 s |

"Sau test thêm": mỗi đột biến được tính là bị giết đã được cài tay vào mã và chạy đúng test mới → đỏ (lệnh ở phần
"Test thêm"); chưa chạy lại cả lượt gremlins của hai gói này (queue mất một giờ). Lượt theo lịch của CI (`mutation`, chủ
nhật) chạy lại toàn bộ.

Bẫy đã gặp: chạy gremlins khi kết quả `go test` còn trong cache thì thời gian đo ban đầu gần 0, mọi đột biến thành "quá
hạn" và hiệu quả hiện 100% giả (lần chạy đầu 18/11). `make mutation` đặt `GOFLAGS=-count=1`.

## Test thêm

| Đột biến | Test | Kiểm tay |
|---|---|---|
| `commands/indoubt.go:53` `still++` → `still--` (Lookup lỗi) | `TestResolveLookupErrorCountedStill` | cài đột biến → FAIL |
| `commands/indoubt.go:100` `timeout <= 0` → `< 0` và → `> 0` | `TestResolveZeroTimeoutUsesDefault` | cài đột biến → FAIL |
| `queue/worker.go:110` `WorkerID == ""` → `!=` | `TestNewWorkerOptionDefaults` | cài đột biến → FAIL |
| `queue/worker.go:119` `Poll <= 0` → `< 0`, → `> 0` | `TestNewWorkerOptionDefaults` | cài đột biến → FAIL |
| `queue/worker.go:122` `Logger == nil` → `!=` | `TestNewWorkerOptionDefaults` | cài đột biến → FAIL |
| `httpapi/guard.go:16` `perm == "" && !rt.Write` → `\|\|`: route đọc có khai quyền thành "chỉ cần đăng nhập" — **đột biến có ý nghĩa an ninh**, bộ test cũ không bắt | `TestGuardReadRouteNeedsPermission` (gọi thẳng `guard`) | cài đột biến → FAIL |
| `identity/staff.go:352` `!Can(fresh, users.manage) \|\| !canTouch` → `&&`: người thao tác bị gỡ quyền quản lý nhân viên giữa chừng vẫn sửa được — **có ý nghĩa an ninh** | `TestReplaceGrants_ActorLostManageMeanwhile` | cài đột biến → FAIL |
| `identity/permissions.go:180` `isAll`: `&&` → `\|\|` (grant kho thiếu mã thành all) | `TestCanShopScopeGrantOrderAndEmptyID` | cài đột biến → FAIL |

## Đột biến còn sống và vì sao chấp nhận

**commands** (4):
- `indoubt.go:34` `len(kinds) == 0 || limit <= 0` → `&&`, và `limit <= 0` → `< 0`: tương đương. Không có Looker thì câu
  đọc nhận danh sách loại rỗng và trả 0 dòng; `limit = 0` thì `LIMIT 0` trả 0 dòng. Kết quả giống hệt.
- `indoubt.go:37` `1<<31-1` → `1<<31+1`, `-1` → `+1`: chặn trên của `limit` khi đổi sang `int32`; chỉ khác khi gọi với
  `limit` trên 2 tỉ (bên gọi dùng 50).

**queue** (15):
- `worker.go:398` `limit <= 0` → `< 0`; `worker.go:692` `<= maxErrorLen` → `<`; `trace.go:38` `len(stored) > 0` → `>= 0`:
  tương đương (nhận 0 việc trả 0 dòng; chuỗi đúng `maxErrorLen` ký tự cắt ra vẫn chính nó; `trace_context` rỗng thì
  `json.Unmarshal` lỗi và bỏ qua như nhánh gốc).
- `worker.go:319`, `320` (số chỗ trống của `dispatch`: `-` → `+`, `<= 0` → `< 0`, `||` → `&&`), `worker.go:340`
  (`busy.Add(-1)` → `Add(1)`): vòng nền nhận việc chỉ khác khi worker đã đầy chỗ; test hiện chạy ít việc hơn
  `Concurrency` nên không chạm. Hậu quả nếu sai: worker nhận quá số việc hay ngừng nhận — `TestPropertyQueue` và bài
  thử tải (Task 22) sẽ thấy việc chờ; chấp nhận ở M0, ghi làm việc phát hiện thêm (test vòng nền với `Concurrency: 1`).
- `worker.go:179`, `341`, `323`, `578`, `598`, `282` (`err != nil` → `== nil` ở nhánh lỗi database của `RunOnce`,
  vòng nền, `sweep`, `update`, `finish`, `LISTEN`): chỉ đi qua khi PostgreSQL trả lỗi giữa chừng; nhánh đổi chiều làm
  log sai hay bỏ lỗi, không đổi trạng thái việc (trạng thái nằm trong transaction đã lùi). Bài hỏng database
  (`tests/e2e`: `TestDatabaseOutageKeepsWebhooks`, bài chaos Task 22) phủ đường này ở mức hệ thống, không ở mức gói.

**identity** (10):
- `permissions.go:134` (`continue` → `break` trong `switch`): `break` chỉ thoát `switch`, tương đương.
- `roles.go:72` (`continue` → `break`): vai trò mẫu đã sửa tay (không phải Chủ) làm dừng đồng bộ các vai trò mẫu sau nó
  — sai thật, chỉ xảy ra khi chủ shop sửa vai trò mẫu (màn vai trò, M7). Ghi ở "Việc phát hiện thêm" của Task 23.
- `staff.go:150`, `186`, `192`, `323`, `400`, `409` (ba chỗ): điều kiện kiểm đầu vào của đường quản lý nhân viên (email,
  tên, số grant tối đa, vai trò còn hoạt động, phạm vi hợp lệ). Chưa phân tích từng đột biến tới mức chứng minh vô hại;
  đột biến đổi hướng làm hàm nhận đầu vào sai hay trả sai mã lỗi, không bỏ được lần kiểm quyền (đã có test giết mọi đột
  biến của nhánh kiểm quyền). Hiệu quả 91,5% trên ngưỡng; đề xuất thêm test bảng đầu vào khi làm màn quản lý nhân viên.

**guard.go**: không còn đột biến sống.
