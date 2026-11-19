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

## Lần chạy M2 — 19/11/2026 (Task 89, M2-32)

Vùng cao M2 (kế hoạch M2 Task 89 Step 1): `internal/modules/order/**`, `internal/modules/promotion/**`,
`internal/modules/customer/{merge,fororder,stats}.go`. Ba vùng này **chưa** nằm trong `HIGH_RISK_DIRS` (tệp quy trình,
chỉ chủ shop sửa), nên `make mutation` và lượt CI chủ nhật chưa gồm chúng; Task 89 chạy gremlins trực tiếp với đúng
`.gremlins.yaml` (ngưỡng 80%):

```sh
RAPID_CHECKS=25 GOFLAGS='-count=1 -skip=Deadlock' gremlins unleash --config .gremlins.yaml --workers 4 \
  -o .mutation/order.json ./internal/modules/order
RAPID_CHECKS=25 GOFLAGS='-count=1 -skip=Deadlock' gremlins unleash --config .gremlins.yaml --workers 2 \
  -o .mutation/promotion.json ./internal/modules/promotion
# customer: loại mọi tệp trừ merge.go, fororder.go, stats.go (-E '^(tên tệp)\.go$' cho từng tệp khác của gói)
```

Chế độ nhanh (quyết định thay chủ shop, Task 89): `RAPID_CHECKS=25` thay 200 và bỏ các test `*Deadlock*` (chúng kiểm
thứ tự khóa, không bắt đột biến điều kiện, mỗi test 15–30 s). Hai thay đổi này chỉ làm hiệu quả đo được **thấp hơn**
thật (ít lượt hơn), không cao hơn.

| Vùng | Bị giết | Còn sống | Không phủ | Hiệu quả lượt gremlins | Sau test thêm (kiểm tay) | Thời gian |
|---|---|---|---|---|---|---|
| `internal/modules/order/**` | 787 | 92 | 205 | **89,53%** | **91,01%** (800/879) | 3 giờ 21 phút |
| `internal/modules/promotion/**` | 165 | 25 | 78 | **86,84%** | **88,42%** (168/190) | 21 phút |
| `customer/{merge,fororder,stats}.go` | 81 | 9 | 19 | **90,00%** | 90,00% | 25 phút |

Cả ba vùng trên ngưỡng 80% ngay ở lượt gremlins. "Sau test thêm": đột biến được cài tay vào một bản sao của mã và chạy
đúng test mới → đỏ; chưa chạy lại cả lượt gremlins (order mất hơn ba giờ).

### Test thêm (M2)

| Đột biến | Test | Kiểm tay |
|---|---|---|
| `order/edit.go:484` (điều kiện `moreMoney` của bản sửa C-02): `+` → `-` hai chỗ, `>` → `>=`, `>` → `<=` | `TestEdit_DiscountApprovalAmountUp` | 4/4 cài → FAIL |
| `order/edit.go:158` (`\|\|` cuối dòng → `&&`), `:159` (`\|\|` → `&&`), `:160` (`\|\|` → `&&`; `*in.TrafficSourceID == uuid.Nil` → `!=`) | `TestEdit_Errors` (ca "giảm đơn âm", "ghi chú hãng quá dài", "người phụ trách rỗng", "nguồn đơn rỗng") | 4/4 cài → FAIL |
| `order/create_coupon.go:32` (`phone == ""` → `!=`; `p.addr != nil` → `==`; `&&` → `\|\|`): mã gắn SĐT so với SĐT người nhận khi khách chưa có SĐT | `TestCreateCoupon_BoundPhoneUsesRecipient` | 3/3 cài → FAIL |
| `order/create_coupon.go:38` (nền tính mã: `-` → `+` hai chỗ) — **có ý nghĩa tiền**: mã % tính trên số trước giảm tay | `TestCreateCoupon_BaseAfterManualDiscounts` | 2/2 cài → FAIL |
| `promotion/redeem.go:278` (`&&` → `\|\|`: mã không giới hạn lượt mỗi khách) | `TestCheck_NoPerCustomerLimit` | cài → FAIL |
| `promotion/manage.go:130` (lượt còn = tối đa − đã dùng: đổi dấu) | `TestRemainingUses` | 2/2 cài → FAIL |

Bản sửa A-03 (`customer/stats.go`, gốc lead → customer khi tính lại) có test `TestStatsPromotesRootAfterMerge`; lượt
gremlins của customer chạy trước khi có đoạn mã này nên chưa đo đột biến của nó.

### Đột biến còn sống và vì sao chấp nhận (M2)

**order** (79 sau test thêm), theo nhóm:
- **Kiểm đầu vào nhiều lớp** (`create.go:40, 87, 98, 99, 104, 107`, `create_m2.go:197, 263`, `edit.go:158:60, 186`,
  `payments.go:30`, `returns.go:199, 267, 270, 277`, `sources.go:144`, `internal/pricing/pricing.go:112, 129, 134, 181,
  201, 209`, `internal/pricing/refund.go:11, 12`): điều kiện từ chối số âm, tràn `MaxMoney`/`MaxInt32`, chuỗi rỗng, ký tự
  điều khiển, trần số dòng. Phần lớn có lớp kiểm thứ hai trả cùng `ErrInvalidInput` (ví dụ `edit.go:158:60`: giảm đơn âm
  bị `pricing.Compute` chặn lại), hoặc chỉ khác ở đúng ranh (100 dòng, `MaxMoney` = 10^12 đồng) mà test phải dựng 100
  biến thể hay số tiền nghìn tỷ. Hậu quả nếu sai: từ chối một đầu vào hợp lệ ở ranh, hoặc lớp sau từ chối thay; không
  ghi tiền sai. Đề xuất test bảng ranh khi làm task bù 438.
- **Cắt trang, giới hạn** (`drafts.go:37`, `duplicates.go:104, 224, 276, 291`, `list_dynsql.go:138, 201`): `limit <= 0`
  → `< 0`, `> limit` → `>=` — chỉ khác ở trang rỗng hoặc trang đúng `limit` dòng; con trỏ trang đã có test rapid.
- **Phát sự kiện, báo hiệu** (`transition.go:92, 126`, `shiphooks.go:198`, `order.go:147`, `create_m2.go:506, 525`):
  `emit` thành công thì đột biến `err != nil` → `== nil` bỏ qua `signal` (báo hiệu realtime, màn hình còn lượt hỏi lại 20 s)
  — test không kiểm báo hiệu từng thao tác; `Writer` thiếu `Events`/`Queue` chỉ xảy ra khi nối sai trong `cmd/crm`.
- **Ghi lịch sử, audit theo trường** (`edit.go:276, 280`, `settings.go:61, 62`, `timeline.go:76`): đổi điều kiện "trường
  đổi thật" làm ghi thêm một mục lịch sử/audit cho trường không đổi, hay đổi dung lượng cấp trước; không đổi dữ liệu đơn.
- **Vòng lặp** (`edit.go:296, 325`, `duplicates.go:205`, `returns.go:235`, `internal/pricing/pricing.go:152`):
  `continue` ↔ `break` ở vòng chỉ có một phần tử khớp trong test; ghi task bù 438 (test hai dòng cùng loại).
- **Còn lại** (`cancel.go:184` `> 0` → `>= 0` cờ "đang giữ hàng": khi số giữ = 0 đường nhả hàng không có dòng để nhả;
  `duplicates.go:114, 136, 441` phân tích con trỏ/payload hỏng và thứ tự cặp: lỗi cùng trả `ErrInvalidInput`/bỏ qua;
  `returns.go:238` `ConditionMissing`; `create_m2.go:305` lấy SĐT người nhận làm SĐT khách khi khách mới chưa nhập SĐT —
  `fororder` kiểm lại SĐT rỗng; `edit.go:432` `len(fresh) > 0` → `>= 0`: đọc lại biến thể với danh sách rỗng trả 0 dòng).

**promotion** (22): `manage.go:142–158` (`AuditFields`, `Bound`: trường audit nil/có), `manage.go:338, 377, 452, 469,
486, 494` (so con trỏ, độ dài tên, cắt trang), `batch.go:174, 200` (lô mã: ranh của phần chia), `evaluate.go:75, 85`
(`BoundPhone == ""` hai vế cùng kết quả khi đơn không SĐT; giá trị 0 đã bị chặn ở tạo mã), `redeem.go:68` (Writer
thiếu phụ thuộc), `redeem.go:231` (đường đọc lại sau khóa khi đơn đã có lượt của mã khác — chỉ xảy ra khi hai lần dùng mã
chạy chen, test song song `TestRedeem_ConcurrentMaxUses` đi đường khác). Không đột biến nào làm vượt `max_uses`/lượt
mỗi khách (các test song song giết hết).

**customer** (9): `merge.go:124, 127, 130, 225` (đọc lại dưới khóa: `||` → `&&` chỉ khác khi một lần gộp khác commit
chen giữa — test song song gộp chéo trả `ErrMergeConflict` qua đường khác), `fororder.go:116, 162, 163, 175` (trường
audit và nhánh audit khi gốc khác hồ sơ).
