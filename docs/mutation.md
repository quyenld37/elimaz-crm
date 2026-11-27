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
`modules/inventory` chưa có lần chạy nào (lượt `mutation` của CI, chạy bằng tay, hoặc `make mutation` sẽ đo).

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
"Test thêm"); chưa chạy lại cả lượt gremlins của hai gói này (queue mất một giờ). Lượt `mutation` của CI (chạy bằng tay
từ Task 493) chạy lại toàn bộ.

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

## Lần chạy 19/11/2026 — vùng cao M1 (Task 403, M1-55b)

Máy dev bận (load 15–28, nhiều phiên khác cùng chạy), `GOFLAGS="-count=1 -p=3"`, `workers: 2` của `.gremlins.yaml`.
Vùng cao M1 theo dòng "Vùng rủi ro cao của M1" của kế hoạch M1. Các mục chưa có trong `HIGH_RISK_DIRS` (điều phối viên
thêm, xem `docs/tasks/task-403.md` mục "Dừng để hỏi") được chạy bằng đúng lệnh `make mutation` sẽ chạy cho mục đó:
thư mục → `gremlins unleash --config .gremlins.yaml -E '/' ./<thư mục>`; tệp lẻ → thêm `-E '^<tệp khác>\.go$'` cho mọi tệp
khác của gói. Khác `make mutation` một điểm: thư mục cũng loại thư mục con (`-E '/'`) để không đột biến mã sinh sqlc
`internal/store` (mã sinh không sửa tay; đột biến ở đó đo test của sqlc, không đo luật nghiệp vụ).

Ba điều chỉnh khi đọc số của gremlins v0.6.0 (giữ cho các lần chạy sau):

1. **Không biên dịch được ≠ bị giết.** `ARITHMETIC_BASE` trên phép nối chuỗi (`"a" + b` → `"a" - b`) làm gói không
   biên dịch; gremlins ghi KILLED. Các đột biến này bị loại khỏi phép tính (kiểm bằng cách cài từng đột biến KILLED rồi
   `go test -run '^$'`). Đột biến bị `go vet` (chạy trong `go test`) chặn — ví dụ `h == "a" && h == "b"` — vẫn tính là
   bị giết: cổng CI cũng chạy vet.
2. **Biểu thức `case` của `switch {}` luôn hiện "không phủ".** Gremlins lấy vùng phủ theo khối lệnh của `go test
   -cover`; biểu thức sau `case` nằm ngoài mọi khối nên mọi đột biến ở đó thành NOT COVERED dù test chạy qua. Các đột
   biến này được cài tay từng cái (cùng phép đổi của gremlins) và chạy test của gói; kết quả ghi trong bảng (cột "cài
   tay").
3. Test cần libvips và `prlimit` (Linux) tự bỏ qua trên máy dev macOS: số của `internal/imager` đo **không** có các test
   đó. Lượt `mutation` của CI (Linux, có libvips) có thể giết thêm, không bớt.

| Mục | Gremlins: giết / sống / không phủ (không biên dịch) | Cài tay (không phủ) | Hiệu quả trước test thêm | Sau test thêm | Thời gian |
|---|---|---|---|---|---|
| `catalog/internal/importer/apply.go` | 14 / 0 / 19 (13) | 19: 15 giết, 4 sống | 16/20 = 80,0% | 20/20 = 100% | 13 s |
| `fulfillment/status.go` | 4 / 0 / 0 (0) | — | 100% | 100% | 32 s |
| `modules/cash` | 34 / 4 / 8 (3) | 8: 6 giết, 2 sống | 37/43 = 86,0% | 43/43 = 100% | 10 phút 48 s |
| `internal/imager` | 92 / 28 / 33 (16) | 31 có nghĩa (2 không biên dịch) | 78/106 = 73,6% (chỉ phần gremlins chạy) | 136/137 = 99,3% | 4 phút 39 s |
| `catalog/article_html.go` | 50 / 11 / 0 (5) | — | 45/56 = 80,4% | 52/56 = 92,9% | 17 phút |
| `platform/files` (trừ `memstore.go`) | 316 / 85 / 154 (51); 2 quá hạn; **18 chưa chạy** | 83: 65 giết, 16 sống, 2 không biên dịch → sau test thêm 78/81 | 265/350 = **75,7%** | 330/350 = 94,3% | ~6 giờ (hai phần) |
| `modules/inventory` | 187 / 16 / 66 (5); 1 quá hạn; **45 chưa chạy** | 6/66: 6 giết | 182/198 = 91,9% | 192/198 = 97,0% | dừng sau 3 giờ 50 phút |
| `modules/order` (6 tệp M1) | 55 / 16 / 0 (1) | — | 54/70 = **77,1%** | 69/70 = 98,6% | 1 giờ 41 phút |
| `modules/identity` (`staff.go`, `roles.go`) | không chạy lại: dùng lần đo 18/11 (91,53%) | — | — | — | — |

"Sau test thêm": mỗi đột biến còn sống (và mỗi đột biến không phủ cài tay) được cài lại vào một bản sao mã và chạy
đúng test mới → đỏ; chưa chạy lại cả lượt gremlins.

Ghi chú cho các dòng mới (đo đêm 19–20/11):

- **Phần chưa chạy.** Từ 01:39 ngày 20/11 máy dev vào trạng thái ngủ ("Dark Wake Thermal Emergency", `pmset -g log`), chỉ
  thức khoảng một phút mỗi 15 phút, nên lượt `inventory` dừng ở 203/248 đột biến và phần 2 của `platform/files` ở
  333/351 (18 đột biến cuối của `verify.go` chưa chạy). Hiệu quả trên tính trên phần đã chạy; lượt `mutation` của CI sẽ đo đủ
  khi các thư mục này vào `HIGH_RISK_DIRS`.
- **`platform/files`** chạy thành hai phần (phần 1 bị dừng giữa chừng; phần 2 giữ 9 tệp `process`, `process_private`,
  `purpose`, `regenerate`, `register`, `s3store`, `store`, `upload`, `verify`), số trong bảng là hợp của hai phần.
  `memstore.go` (kho tệp giả trong bộ nhớ, chỉ test dùng) không tính. Đột biến không phủ chỉ cài tay 83/154 (phần có
  trong log lúc bắt đầu cài); phần còn lại không tính vào hiệu quả như định nghĩa của gremlins.
- **`inventory`**: cả gói test mất hơn 10 phút trên máy đang tải, nên đột biến không phủ cài tay chỉ chạy test của tệp
  tương ứng (bỏ test `*Deadlock*`); làm được 6/66 trước khi máy ngủ. Một đột biến quá hạn (treo tới hạn của gremlins).
- **`order`**: Task 89 (mục "Lần chạy M2" ở trên) đã đo cả gói `order` (91,01%, 800/879) và xếp 15 đột biến của
  `create.go`, `transition.go` vào nhóm chấp nhận; test thêm của Task 403 giết 15 đột biến đó (bảng dưới), nên số của cả
  gói sau Task 403 là 815/879 = 92,7%. Ở 6 tệp M1, đột biến còn lại duy nhất là `order.go:147` (tương đương, xem dưới).
- `verify.go:235:12` (`err != nil` → `==`) làm test treo hơn 30 phút khi cài tay; dừng tay và tính là quá hạn.

### Test thêm (Task 403)

| Đột biến | Test | Ý nghĩa |
|---|---|---|
| `apply.go:18` `continue` → `break` | `TestRecheck_ProductWithoutVariantsSkipped` | mẫu không có dòng biến thể làm dừng kiểm lại các mẫu sau |
| `apply.go:26` `!has \|\| ex.ID != …` → `&&` | `TestRecheck_RecreatedSameKey` ca "mẫu có sẵn vừa bị xóa rồi tạo lại cùng mã" | áp nhập Excel lên mẫu khác cùng mã |
| `apply.go:47` hai `\|\|` đầu → `&&` | `TestRecheck_RecreatedSameKey` ca "biến thể sửa vừa bị xóa rồi tạo lại cùng SKU", `TestRecheck_UpdateWithoutExistingID` | ghi đè biến thể khác cùng SKU |
| `cash/cod.go:97` `actor.ID != uuid.Nil` → `==` | `TestPostLimitsAndActorID` | `actor_id` của bút toán lưu sai người |
| `cash/cod.go:122` `len(key) > 200`, `len(reason) > 500` → `>=` | `TestPostLimitsAndActorID` | biên độ dài khớp CHECK của bảng |
| `cash/cod.go:136` `> maxAmount`, `< -maxAmount` → `>=`, `<=` | `TestPostLimitsAndActorID` | dòng đúng ±(2^53 − 1) bị từ chối sai |
| `cash/consumer.go:43` `err != nil \|\| id == Nil` → `&&` | `TestConsumerPermanentErrors` (payload sai kiểu) | sự kiện hỏng bị bỏ qua im lặng thay vì lỗi vĩnh viễn |
| `imager/client.go:94` (4 đột biến Retry-After) | `TestClient_RetryAfterBounds` | chờ 0 s (gửi dồn) hay 301 s |
| `imager/pipeline.go:114–129` biên `Validate` (33 đầu ra, rộng 8193, chất lượng −1/101, 9 header) | `TestRequestValidate_Boundaries` | yêu cầu vượt giới hạn lọt vào imager |
| `pipeline.go:231`, `247` `> MaxBytes` → `>=`; `241` mất lỗi đọc | `TestImager_MaxBytesBoundary`, `TestImager_SourceCutMidway` | nguồn bị cắt giữa chừng vẫn được xử lý |
| `pipeline.go:296` `w*h > MaxPixels` (`*` → `/`, `>` → `>=`) | `TestImager_MaxPixelsBoundary` | **bom điểm ảnh lọt qua khi không có libvips thật trong test** |
| `pipeline.go:307`, `312` (dòng tóm tắt thiếu tên tệp, chiều 0, số tràn) | `TestImager_SummaryLineRejected` | |
| `pipeline.go:337` `Quality > 0` | `TestImager_QualityOption` | |
| `pipeline.go:383` (JPEG đầu ra khai chiều 0) | `TestImager_OutputZeroDimensionRejected` | |
| `pipeline.go:419` Content-Type của PUT; `428` mã 299; `434` hết hạn khi PUT | `TestImager_PutContentType`, `TestImager_PutStatusRange`, `TestImager_PutTimeout` | đường dẫn ký sẵn ký cả Content-Type |
| `pipeline.go:468` (`context.Canceled` là hết hạn) | `TestImager_CanceledCountsAsTimeout` | |
| `runner.go:94`, `96` (`capped`) | `TestExecRunner_StdoutExactlyAtLimit`, `TestExecRunner_StderrKeptUpToLimit` | |
| `article_html.go:113` `continue` → `break`; `119` `err != nil` → `==` | `TestArticleImages_SaveChecksEveryNewFile` | **tệp mới sau một tệp cũ (hay sau một tệp mới hợp lệ) không được kiểm — gắn được tệp chưa ready/của mục đích khác vào bài viết** |
| `article_html.go:87` `len(vs) > 0` → `>=` | `TestResolveArticleImages_MissingFileLeavesNil` | |
| `article_html.go:178` biên chiều rộng 1, 10000 | `TestRenderArticleHTML_VariantBounds` | |
| `article_html.go:203` `w - 640` → `w + 640` | `TestRenderArticleHTML_SrcClosestTo640` | |
| `files/content.go:41` `maxBytes+1` → `-1` | `TestReadContent` (đọc đúng `maxBytes` byte) | tệp nhập Excel bị cắt bớt im lặng |
| `files/process.go:581` rộng 0, `590` đầu ra sai rộng/định dạng/0 byte | `TestFileProcess_OddImagerResultNotReady` (test mới, các ca `ODD:h0`, `ODD:w0`, `ODD:type`, `ODD:w`, `ODD:fmt`, `ODD:b0`) | **imager trả rộng 0 → tệp ready với width NULL** |
| `files/process.go:185`, `197`, `41` (nhánh chờ thử lại; trần tính từ lúc ghi lệnh; chờ ≥ 30 s khi imager quá giờ) | `TestProcessor_ExecuteDoneSkipsRetry`, `TestProcessor_RetryCapFromJobWhenProcessingOlder`, `TestProcessor_TimeoutRetryAtLeast30s` | tệp kẹt processing bị rejected ngay ở lỗi tạm đầu tiên |
| `files/process.go:683`, `696`, `709` (OnDone: thiếu tenant, tệp đã ready/đã xóa, mã băm sai dạng) | `TestProcessor_OnDoneWithoutTenantIgnored`, `TestProcessor_OnDoneSkipsReadyAndDeleted`, `TestProcessor_OnDoneBadSHARejected` | |
| `files/process.go:744`, `750`, `760` (OnTerminal: dead/expired, thiếu tenant, tệp riêng tư hay không phải ảnh) | `TestProcessor_OnTerminal` | lệnh công khai chết làm hỏng ảnh riêng tư, tệp Excel |
| `files/process.go:245` (dựng lại tệp mất từ ảnh gốc) | `TestVerifier_MarksMissingLost` (xóa cả tệp tạm của x như lifecycle crm-tmp) | MemStore không tự xóa crm-tmp nên trước đây đột biến vẫn qua |
| `files/process.go:628` (bản kê vượt/đúng 40 triệu điểm ảnh) | `TestFileProcess_ManifestPixelLimit` | |
| `files/purpose.go:74–75` (`samePurpose` so đủ trường) | `TestRegisterPurpose_PanicsOnEachChangedField` | khai lại mục đích khác mà không panic |
| `files/regenerate.go:29`, `33`, `95`, `verify.go:33`, `38` (hạn, số lượt của lệnh) | `TestProcessor_KindTimeout`, `TestRegenerate_ExecuteGuards` | |
| `files/regenerate.go:114`, `125`, `130`, `148`, `171`, `172`, `182` (chặn đầu Execute/OnDone của file.regenerate) | `TestRegenerate_ExecuteGuards`, `TestRegenerate_SkipsWhenNotNeeded`, `TestRegenerate_OnDoneOnlyReadySameSHA`, `TestRegenerate_OnDoneIgnoresOddJobs` | chèn biến thể vào tệp đã xóa / khác mã băm |
| `files/verify.go:125` (payload file.verify) | `TestVerifier_BadPayload` | |
| `files/s3store.go:105`, `120`, `137`, `193`, `207`, `262`, `298` (tham số ký PUT, header ký, biên Open/đọc, Put 0 byte, Put từ vị trí đọc dở) | `TestFileStore_PresignRejectsBadParams`, `TestFileStore_OpenExactlyMaxBytes`, `TestLimitedBody_Boundary`, `TestFileStore_Contract` | kho trả thừa byte làm `io.ReadAll` quay mãi |
| `files/upload.go:128`, `170`; `lost.go:31`, `35`; `original_url.go:15` | `TestCreateUpload_TooLarge` (tên đúng 255 ký tự), `TestCreateUpload_CreatedBy`, `TestListLost_Pages`, `TestOriginalTTL` | |
| `inventory/documents.go:319`, `335`, `373` (limit 1…200, trang cuối, đúng 500 dòng) | `TestListDocuments_LimitBounds`, `TestReplaceLines_MaxLines` | |
| `inventory/movement.go:113` (Writer thiếu Events/Queue) | `TestPostDocument_WriterNotWired` | đổi tồn không có sự kiện |
| `inventory/returns.go:75` (`err != nil` → `==` sau dòng hàng hỏng) | `TestReceiveCustomerReturn` (mỗi dòng số dư có stock.changed) | **nhận hàng trả bỏ dở các dòng sau, không phát sự kiện** |
| `inventory/returns.go:92` (thiếu mã phiếu/biến thể/kho) | `TestMoveToDamaged` | |
| `order/create.go:40`, `98`, `99` (thiếu khách/người bán; đúng 500/120 ký tự; ký tự điều khiển) | `TestCreateManual_InvalidInput` | |
| `order/create.go:87`, `104`, `107` (gộp dòng: tổng đúng MaxInt32, giảm dòng âm/tràn/đúng MaxMoney) | `TestMergeLines_Bounds` | |
| `order/transition.go:92`, `126` (báo hiệu sau đổi trạng thái) | `TestApplyTransition_Signals` | |

### Đột biến còn sống và vì sao chấp nhận (Task 403)

- `imager/runner.go:96` `room > 0` → `>= 0`: tương đương (`room` không âm; ghi `p[:0]` không đổi gì).
- `catalog/article_html.go:202` `w > 640` → `>=`: tương đương (`distance(640)` bằng 0 ở cả hai nhánh).
- `catalog/article_html.go:223` `r >= 0x7f` → `> 0x7f`: tương đương — ký tự DEL (0x7f) lọt qua thì `url.Parse` ngay sau
  đó từ chối (ký tự điều khiển), vẫn `ErrInvalidArticle`.
- `catalog/article_html.go:84`, `98` (`||` ↔ `&&` trong điều kiện "khối image có fileId"): tương đương với mọi đầu vào
  hợp lệ — `article.Validate`/`Check` bảo đảm khối image luôn có `fileId` và khối khác không bao giờ có; đường duy nhất
  gọi hai hàm này nhận khối từ `DecodeArticle`.
- `files/process.go:202` `elapsed < RetryMax` → `<=`, `223` `d > left` → `>=`: tương đương (chỉ khác khi bằng đúng tới
  nano giây; nhánh "bằng" gán cùng giá trị).
- `files/process.go:590` `o.Bytes <= 0` → `< 0`: database chặn (`CHECK (size_bytes > 0)` của `file_variants`) — ca
  `ODD:b0` vẫn không ready.
- `files/process.go:810` `n > 0` → `>= 0`: tương đương với mọi bên gọi (assemble/readManifest đã đòi > 0; bản ≥ 1).
- `files/process.go:245` `Status == Lost && len(sha) == 32` → `||`: tương đương — tệp xử lý được khác lost (uploaded,
  processing) chưa bao giờ có `content_sha256` (chỉ markReady ghi).
- `files/download.go:108` `v.Width > best` → `>=`: tương đương (`UNIQUE (tenant_id, file_id, preset_version, width,
  format)`).
- `files/regenerate.go:58` (hai `||` đầu → `&&`), `59` `continue` → `break`: tương đương — chỉ đường xử lý ảnh ghi
  `content_sha256`, câu `ListFilesMissingPreset` đòi `content_sha256 IS NOT NULL` và `mode = 'standard'`, nên tệp không
  phải ảnh hay ảnh riêng tư không bao giờ có trong kết quả.
- `files/regenerate.go:86` `o.Signer == nil` → `!=`: tương đương — `newProcessor` lại đặt `Signer = Store` khi thiếu.
- `files/regenerate.go:171` (`||` trước `res.Skipped` → `&&`): tương đương — vế `res.Preset != pl.Preset` vẫn bắt kết
  quả hỏng (`res.Preset = 0`) vì bản trong lệnh luôn ≥ 1.
- `files/s3store.go:35`: chỉ đổi thuộc tính log `secret_set`. `s3store.go:299`: chỉ đổi chữ của thông báo lỗi.
- `files/s3store.go:91`, `96` (nhận "không có", "điều kiện sai" theo mã lỗi S3 hoặc mã HTTP): các dấu hiệu dư nhau — kho
  thật trả cả mã lỗi lẫn mã HTTP, đổi một vế vẫn còn vế kia.
- `files/s3store.go:214` `len(p) > left` → `>=`: tương đương (bằng nhau thì `p[:left]` là `p`).
- `files/store.go:96` `len(s)*3` → `/3`: chỉ là dung lượng ban đầu của slice.
- `files/upload.go:195` (`||` → `&&`): tệp đã xóa mà còn awaiting chỉ bị hỏi kho thêm một lần; `ApplyUpload` kiểm lại
  `DeletedAt` dưới khóa và trả `ErrNotFound`.
- `files/original_url.go:28` (`||` → `&&`): `detected_type` của tệp ảnh chỉ là image/jpeg, image/png, image/webp.
- `inventory/byitems.go:106` `d > 0` → `>= 0`: tương đương (`d == 0` đã trả về ở dòng 102).
- `inventory/documents.go:102` `q < 0` → `<= 0`: tương đương (dòng 382 từ chối `q == 0` trước).
- `inventory/movement.go:87` (`abs`) `v < 0` → `<= 0`: tương đương (−0 = 0).
- `inventory/reconcile.go:64` `batch > ReconcileBatch` → `>=` (tương đương) và → `<=` (mọi batch hợp lệ thành
  `ReconcileBatch`: cùng kết quả, chỉ khác số câu đọc).
- `inventory/reserve.go:178` (`k != 0` → `==` trong hàm sắp thứ tự đổi số dư): không đổi kết quả — mọi dòng số dư đã khóa
  đúng thứ tự ADR-46 ở `lockInState`; bước đổi số dư chỉ cập nhật dòng đã giữ khóa, mỗi `stock.changed` mang `Seq` riêng.
- `order/order.go:147` (`Events == nil || Queue == nil` → `&&`): `inventory.Writer` tự kiểm lại nên vẫn lỗi và lùi
  transaction (`TestCreateManual_WriterNotWired` giữ hành vi này).

## Lần chạy M4 — 28/11/2026 (Task 182, M4-42)

Vùng cao M4 theo dòng "Vùng rủi ro cao của M4" của kế hoạch M4: `internal/modules/channel`,
`internal/adapters/facebook/{signature,policy,send,download}.go`, `internal/modules/conversation/internal/{inbound,send,
automation,live,access}`. Chưa có trong `HIGH_RISK_DIRS` (xem `docs/tasks/task-182.md`, QĐ-182-2), nên chạy bằng đúng
lệnh `make mutation` sẽ chạy cho mục đó: `gremlins unleash --config .gremlins.yaml -E '/' [-E '^<tệp ngoài nhóm>\.go$' …]
./<gói>` (thư mục con, kể cả mã sinh `internal/store`, bị loại).

**Nơi chạy:** máy Mac không chạy test nặng (chủ shop 27/11), nên mỗi nhóm tệp là một test Go tạm (`tests/mutationrun/<nhóm>`,
gọi lệnh trên, luôn đỏ để log hiện, hạn 27 phút) đẩy lên GitHub Actions bằng `scripts/ci/remote-test.sh <tree> HEAD <tệp
gói>` (`CRM_CI_STATIC=false`, máy `ubuntu-24.04`, `--workers 4 --timeout-coefficient 10`, `GOFLAGS=-count=1`); kết quả đọc
bằng `gh run view <run> --log-failed`. Bộ chạy không commit. Ngưỡng tính theo gói (tổng các nhóm).

Hai điều chỉnh khi đọc số: (1) gremlins chỉ chạy test **của chính gói** bị đột biến — đột biến do test gói khác bắt
(`automation` kiểm cờ lịch sử của `inbound`…) vẫn ghi LIVED; (2) đột biến nối chuỗi không biên dịch được vẫn ghi KILLED
(như lần M1) — chưa loại, nên số "bị giết" có thể cao hơn thật vài đơn vị.

| Gói | Lượt 1 (bị giết / sống) | Hiệu quả | Lượt 2 sau test thêm | Hiệu quả |
|---|---|---|---|---|
| `channel` | 253 / 41 | 86,05% | (không chạy lại; `TestReportTokenInvalid_AnyAuthReasonOneAlert` giết thêm `health.go:395`, `401`, kiểm bằng overlay) | ≥ 86,05% |
| `adapters/facebook` (4 tệp) | 75 / 7 | 91,46% | 80 / 2 | 97,56% |
| `conversation/internal/access` | 2 / 1 | 66,67% | 5 / 0 | 100% |
| `conversation/internal/inbound` | 205 / 76 | 72,95% | 266 / 16 | 94,33% |
| `conversation/internal/send` | 279 / 73 | 79,26% | 334 / 29 | 92,01% |
| `conversation/internal/automation` | 194 / 51 | 79,18% | 232 / 20 | 92,06% |
| `conversation/internal/live` | 205 / 55 (một nhóm dừng ở 27 phút) | 78,85% | 284 / 26 (nhóm `sessions/reconcile/detect` dừng ở 27 phút: 88 / 8 phần đã chạy) | 91,61% |

Lượt 2 chạy trên cây có test thêm của Task 182 (`mutation_m4_test.go` mỗi gói, `terminal_test.go`, `access_test.go`,
ca thêm ở `send_test.go`, `download_test.go`); sau lượt 2 còn thêm `TestAutoHide_RejectionReportsOnlyAuth`,
`TestHideComment_RejectionReportsOnlyAuth` (giết `automation/command.go:87:43` và đột biến tương ứng ở `send/hide.go`,
kiểm bằng overlay) và sửa test chập chờn `TestMutationM4_ForcedSSENeverFallsBackToPoll` (lượt 2 của nhóm
`manager/lease` hỏng vì nó, chạy lại: 29 / 8).

Mỗi test mới đều được kiểm bằng `go test -overlay` (bản chép có đột biến → đỏ; mã thật → xanh). Lưu ý cho lần sau:
đường dẫn worktree có dấu (`Thời trang`) thì phải đặt `PWD` đúng thư mục, không thì Go bỏ qua overlay và mọi đột biến
trông như còn sống.

### Test thêm (M4)

| Gói | Test | Bắt gì (tóm tắt) |
|---|---|---|
| `facebook` | `TestSend_HideError`, ca mã nút rỗng / 1001 / đúng 1000 ký tự (`TestSend_RejectBeforeCall`), đúng 3 lần chuyển hướng (`TestDownload_Rejects`), trần 0 = 25 MiB (`TestDownload_SizeLimit`) | ẩn lỗi token không thành "không rõ"; biên của nút trả lời nhanh, chuyển hướng, trần tải |
| `access` | `TestSignal`, ca "quyền khác trước quyền đọc" (`TestVisiblePages`) | báo hiệu mang đúng phạm vi Page; `continue` → `break` |
| `channel` | `TestReportTokenInvalid_AnyAuthReasonOneAlert` | một cảnh báo cho mỗi lần mất quyền, với mọi lý do mất quyền |
| `inbound` | 15 test `TestMutM4_*` | việc sau khi ghi tệp đính kèm (hook, báo hiệu, tải tệp của comment/echo), mốc đã xem chỉ báo hiệu khi đổi, sửa tin lần hai, cờ lịch sử/tự động theo đường vào, comment live, chặn payload hỏng của việc tải tệp/hồ sơ/bài |
| `send` | 17 test `TestMutM4_*` | lỗi 551 không hạ Page; mốc chặn khi Meta xin giãn; gửi lại ảnh, bỏ tin; nhắn riêng khi comment cha đã xóa; tin tự động xếp lệnh; Lookup không tenant/không token |
| `automation` | 8 test `TestMutationM4_*` + `TestAutoReply_InDoubtDropped`, `TestAutoHide_RejectionReportsOnlyAuth` | biên SĐT, giới hạn cấu hình (số câu, độ dài, số bài, giờ), biên giờ của nhắc theo dõi, quy tắc lưu ghi người sửa |
| `live` | 13 test `TestMutationM4_*` | biên chốt (20 dòng, số lượng), thêm vào nháp của khách khác, đối chiếu cuối phiên (đếm, tràn int32), dò phiên (bỏ Page ngắt, tiếp sau lỗi), luồng SSE im/ép sse |

### Đột biến còn sống và vì sao chấp nhận (M4, sau lượt 2)

- **Tương đương** (cùng kết quả với mọi đầu vào có thể):
  `facebook/download.go:93` (`>` → `>=` khi đã bằng trần); `inbound/attachments.go:311`, `posts.go:229`,
  `automation/followup.go:74` (cắt chuỗi đúng bằng độ dài); `inbound/comments.go:140`, `echo.go:74`, `messages.go:73`
  (xếp tải tệp với danh sách rỗng không làm gì); `inbound/messages.go:108`, `profile.go:177`, `187`, `201` (lỗi luôn đi
  kèm `ok = false`, tệp 0 byte đã bị loại vì không phải ảnh, `SetNameFromChannel` tự từ chối tên rỗng);
  `send/merge.go:177`, `comment_reply.go:131`, `message.go:137`, `private_reply.go:177`, `248`, `resend.go:231`,
  `292` (trạng thái rỗng khác `queued`; adapter trả Sent luôn kèm mã; khóa luồng không bắt đầu bằng `:`; cột
  `attachments` mặc định `'[]'`); `automation/phone.go:43`, `45`, `62` (giới hạn chỉ gọi với 1 hoặc −1; bỏ qua `+` thì
  `84…` vẫn khớp); `automation/autohide.go:23`, `39`, `autoreply.go:124`, `greeting.go:67`, `71` (quy tắc tắt / tin của
  Page / tin lịch sử đã bị cổng chung `Run` chặn — `TestRun_NoRowGates`); `live/source_fb.go:219`, `239`, `254`
  (chỉ khác khi hai mốc bằng nhau tới nano giây hoặc ctx hủy đúng lúc đó); `live/reconcile.go:138` (adapter thật luôn trả
  mã comment).
- **Chỉ đổi chỉ số, log:** `facebook/download.go:69` (thời gian giữ kết nối rảnh); `inbound/comments.go:166`,
  `handler.go:187`; `live/manager.go:69`, `199`, `source_fb.go:76`.
- **Payload hỏng không xảy ra** (việc, lệnh luôn do chính mã ghi, đủ mã và tenant): `send/merge.go:23`, `64`, `81`,
  `command.go:91:54`, `comment_reply.go:108:54`, `indoubt.go:45:54`, `hide.go:147`, `186`, `private_reply.go:284`;
  `automation/autoreply.go:128`, `228`, `greeting.go:75`, `command.go:108`, `211`.
- **Khoảng trống thật, chấp nhận ở M4** (ghi để lần sau viết test khi có cách tiêm lỗi):
  `send/private_reply.go:121`, `124` (chỉ đổi thứ tự khóa ADR-46 — cần test hai transaction xen nhau),
  `private_reply.go:103`, `autoaction.go:165` (lỗi trùng unique 23505 — khóa hội thoại đã xếp hàng các lần nhắn riêng,
  không dựng được bằng API), `autoaction.go:191` (lỗi database của `channel.Account` sau khi đọc comment),
  `private_reply.go:298` (máy Meta giả luôn trả `recipient_id`); `inbound/comments.go:64`, `110`, `echo.go:41`, `75`
  (sự kiện "add" chen giữa lúc khóa và lúc chèn; khóa lại hội thoại sau khi echo đã khớp — chỉ đổi `updated_at`);
  `automation/command.go:87:61`, `155` (lỗi lớp auth luôn đi với Rejected; lý do rỗng của lệnh ẩn không xảy ra);
  `send/hide.go:120`, `131`, `205` (cổng của lệnh ẩn: Page chỉ xem đổi giữa lúc ghi và lúc chạy — đã có
  `TestMutM4_HideExecuteReadOnlyAndSlowDown` cho nhánh chính); `live/source_fb.go:243` (30 lần hỏi định kỳ lỗi liền mới
  dừng nguồn — test cần ≥ 6 s giờ thật), `source_fb.go:175`, `238` (lỗi database khi ghi lô lúc ctx còn sống),
  `live/manager.go:219` (hạn cục bộ trước hạn database — chỉ đo được bằng khe giờ thật 0,8 s, trái luật Task 400; test
  đã viết rồi bỏ), `live/sessions.go:126`, `178`–`195`, `267`, `detect.go:172`, `remind.go:140`, `180` (nhóm chạy lượt 2
  phần sau 27 phút; chưa phân loại từng cái — đo lại khi `HIGH_RISK_DIRS` có M4).
- Danh sách lượt 1 (222 đột biến sống) và lý do từng cái theo từng agent viết test: tóm tắt trong bảng "Test thêm" ở trên;
  bản đầy đủ không giữ trong repo (log run của GitHub Actions giữ 90 ngày, số run ghi ở `docs/tasks/task-182.md`).
