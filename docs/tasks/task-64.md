# Task 64 — M2-07 Tính tiền đơn (thuần Go, không database)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 64, M2-07). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn nào. Gói **mới** `internal/modules/order/internal/pricing` (chỉ `order` nhập được, vì nằm dưới `order/internal`): `MaxMoney`, `Line`, `Input`, `LineTotals`, `Totals`, `Compute`, `Allocate`, `ManualDiscountBP`, `RefundFor`, sentinel `ErrLineDiscountExceeds`, `ErrOrderDiscountExceeds`, `ErrNegative`, `ErrMoneyOverflow`. Chưa có chỗ dùng (0 tham chiếu): `order` bắt đầu dùng ở Task M2-16, 17, 20, 22; khi đó `order/errors.go` bọc lỗi gói này thành sentinel của `order` (`ErrMoneyOverflow` → `ErrTotalOverflow`).
- Gói phụ thuộc ngược (go list): không có (gói mới, chưa ai nhập). Không đổi `order/create.go` (cách tính tổng của M1 giữ nguyên tới Task M2-16).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: chưa. `testdata/vectors.json` là bộ vector chung; màn Lên đơn nhanh (Task M2-27, `pricing.ts`) chạy cùng tệp. Định dạng tệp ghi trong `vectors_test.go`.
- Câu sqlc, bảng, pha migration: không (thuần Go, không database).
- Test sẽ chạy: `go test -race ./internal/modules/order/internal/pricing/...` (gói mới, không phụ thuộc database); `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tìm "money", "tiền", "tràn", "chia") — không có hàm tiền chung trong `internal/platform`. Tìm `MaxMoney|addMoney|mulMoney|9007199254740991` trong `internal`: `catalog.MaxMoney` (hằng), `order/create.go` `addMoney`/`mulMoney` (không xuất, `qty int32`), `inventory/documents.go` `maxMoney`, `catalog/internal/importer` `MaxMoney`. `testkit.SetRapidChecks` cho số lượt rapid.
- Dùng lại / viết mới vì: dùng lại `testkit.SetRapidChecks` (TestMain, 1.000 lượt). `MaxMoney` của gói khai `= catalog.MaxMoney`? Không: `pricing` là gói thuần, kế hoạch khai `const MaxMoney int64 = 1<<53 - 1` riêng; test so bằng `catalog.MaxMoney` để hai hằng không lệch. `addMoney`/`mulMoney` của `order/create.go` không xuất và là đường tính của M1 sẽ được thay bằng `pricing.Compute` ở Task M2-16 (khi đó xóa cách cũ, CLAUDE.md luật 4); không gộp ở task này vì ngoài phạm vi. Nhân chia không tràn viết mới bằng `math/bits` (thư viện chuẩn), không có hàm sẵn.

## Soát thiết kế

- 5.1 chủ dữ liệu: tính tiền đơn thuộc `order` (Global Constraints: chỉ gói `order/internal/pricing` tính tổng đơn); không có dữ liệu lưu.
- 5.2, 5.3, 5.4, 5.5, 5.6, 5.7: không áp dụng: không đọc, ghi database, không sự kiện, không gọi ra ngoài.
- 5.8 nghiệp vụ không biết HTTP: gói chỉ nhập `errors`, `math`, `math/bits`, `cmp`, `slices`.
- 5.9: không áp dụng: không đường ghi.
- 5.10: không áp dụng.
- 5.11 thư viện: chỉ thư viện chuẩn; rapid (đã có trong ADR-44) cho test.
- 5.12 module mẫu: gói `internal` của module (như `catalog/internal/importer`), sentinel lỗi trong gói, test gói `_test`.
- 6.1–6.9: không áp dụng: hàm thuần, không trạng thái chung, không database, không khóa.
- 6.10 tiền: mọi số `int64` đồng trong [0, `MaxMoney`]; phép nhân trung gian (số lượng × đơn giá, cơ sở × số giảm, giảm × 10.000, tiền dòng × số trả) tính bằng `math/bits.Mul64` 128 bit nên không tràn; mọi tổng kiểm vượt `MaxMoney` → `ErrMoneyOverflow`, không cắt. Chia làm tròn xuống, phần lẻ dồn dòng `Base` lớn nhất (Q4 của kế hoạch).
- 6.11 quy luật: rapid 1.000 lượt cho Σ`Allocated` = giảm cấp đơn, `0 ≤ Allocated_i ≤ Base_i`, Σ`Net` + ship = `GrandTotal ≥ 0`, tổng tiền hoàn mọi cách chia số lượng trả = `Net`.
- 6.12: không áp dụng: không dữ liệu cá nhân.
- 6.13: không áp dụng: không quyền.

## Quyết định thay chủ shop

- **Q-T64-1 — phần lẻ vượt chỗ còn của dòng lớn nhất.** Luật của kế hoạch ("phần dư cộng vào dòng có base lớn nhất")
  có lúc làm `Allocated > Base` của dòng đó, trái quy luật `0 ≤ Allocated_i ≤ Base_i` cũng của kế hoạch. Ví dụ: ba
  dòng Base 1/1/1, giảm 2 → mỗi dòng floor 0, phần dư 2 dồn hết vào dòng 1 thì dòng 1 bị giảm 2 > 1. Chọn: phần dư
  vẫn vào dòng Base lớn nhất (bằng nhau thì dòng đứng trước) nhưng tối đa tới Base của dòng; còn thừa thì sang dòng
  lớn kế tiếp theo cùng thứ tự. Với giá thật của shop (Base hàng chục nghìn đồng, phần dư < số dòng) kết quả **trùng**
  luật gốc; chỉ khác ở ca Base vài đồng. Vì sao: giữ cả hai quy luật của kế hoạch, đúng ý Q4 (lẻ dồn dòng lớn nhất),
  không có dòng nào âm tiền. Phương án khác: (a) giữ luật gốc, bỏ quy luật `≤ Base` (dòng có Net âm, trigger M2-05 và
  tiền hoàn sai); (b) chia theo phương pháp phần dư lớn nhất (Hamilton) — đổi hẳn Q4. Dễ đảo: một hàm `Allocate`, đổi
  thì sinh lại `vectors.json` và sửa `pricing.ts` cùng lúc.
- **Q-T64-2 — đầu vào sai của `RefundFor` trả 0.** Chữ ký của kế hoạch không có lỗi. Chọn: `qty ≤ 0`, trả ≤ 0, số âm,
  trả quá số còn lại, đã hoàn quá `net` → 0; kết quả luôn trong `[0, net − refundedBefore]`. Vì sao: không bao giờ hoàn
  quá, không panic trên đường yêu cầu; Task M2-22 kiểm số lượng trả trước và báo lỗi nghiệp vụ. Khác: trả `(int64,
  error)` (đổi interface kế hoạch, màn hình M2-27 không dùng hàm này nên vẫn đảo được sau).
- **Q-T64-3 — `Allocate` panic khi điều kiện sai** (base âm, `amount` ngoài `[0, Σbase]`, `Σbase > MaxMoney`). Chữ ký
  kế hoạch không có lỗi; `Compute` kiểm trước (`ErrOrderDiscountExceeds`, `ErrMoneyOverflow`) nên chỉ là lỗi lập
  trình. Khác: trả toàn 0 lặng lẽ (giấu lỗi tiền).
- **Q-T64-4 — `Qty = 0` được nhận** (Gross 0); `Qty < 0` → `ErrNegative`. Kiểm "số lượng > 0" là của `order` (đã có
  `ErrInvalidInput` của M1), không lặp ở gói tính tiền. Vector chung chỉ dùng số lượng 1–5.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi viết mã | toàn bộ test gói (mã giả trả 0) | `go test -race ./internal/modules/order/internal/pricing/...` | 16 test `--- FAIL` (DH01, DH05, Allocate…, Errors, ManualDiscountBP, bốn rapid, RefundFor, Vectors), `FAIL` |
| DH01: Subtotal 680.000, DiscountTotal 20.000, GrandTotal 690.000, V1 Net 480.000 | `TestCompute_DH01` | `go test -race -count=1 -v ./internal/modules/order/internal/pricing/...` | `--- PASS: TestCompute_DH01` |
| DH05: 100.001 → 50001/18000/32000; ΣNet + ship = GrandTotal | `TestCompute_DH05` | như trên | `--- PASS: TestCompute_DH05` |
| Hai Base bằng nhau, giảm 1 → 1/0; dòng lớn nhất không đứng đầu | `TestAllocate_TieGoesToFirst`, `TestAllocate_LargestBaseNotFirst` | như trên | PASS |
| Phần lẻ có trần Base (Q-T64-1) | `TestAllocate_RemainderCappedAtBase` | như trên | PASS (`[1 1 1]`, 2 → `[1 1 0]`) |
| Giá 0 không chia cho 0; giảm 1 trên Σbase 0 → `ErrOrderDiscountExceeds` | `TestCompute_AllZero`, `TestAllocate_ZeroBase` | như trên | PASS |
| `UnitPrice 2^52 × 3` → `ErrMoneyOverflow`; tích vượt int64; các lỗi âm, giảm vượt | `TestCompute_Errors` (15 ca) | như trên | PASS |
| Tích Base × giảm ≈ 2^105 không tràn | `TestAllocate_HugeProduct` | như trên | PASS |
| `ManualDiscountBP` 100.000 → 1000, 100.001 → 1001, mã không tính; Totals không thể có từ `Compute` → `MaxInt64` (luôn cần duyệt) | `TestManualDiscountBP` (14 ca) | như trên | PASS |
| `RefundFor` ba ca kế hoạch + đơn vị cuối nhận phần lẻ; đầu vào sai → 0 | `TestRefundFor`, `TestRefundFor_InvalidReturnsZero` | như trên | PASS |
| Thuộc tính Σ`Allocated` = giảm đơn + mã, `0 ≤ Allocated_i ≤ Base_i`, Σ`Net` + ship = `GrandTotal ≥ 0` | `TestCompute_Invariants`, `TestAllocate_Invariants` | như trên | `[rapid] OK, passed 1000 tests` (mỗi test) |
| Đầu vào bất kỳ → một trong bốn lỗi hoặc quy luật đúng, không panic | `TestCompute_AnyInput` | như trên | `[rapid] OK, passed 1000 tests` |
| Mọi cách chia số lượng trả cho tổng hoàn = Net | `TestRefundFor_SplitSumsToNet` | như trên | `[rapid] OK, passed 1000 tests` |
| Mỗi bộ trong `vectors.json` (200 bộ) cho đúng totals; sinh lại ra cùng tệp | `TestVectors` | `go test ./internal/modules/order/internal/pricing -run TestVectors -update-vectors` hai lần, `shasum` | PASS; `cb04b04d…` cả hai lần |
| `MaxMoney` = `catalog.MaxMoney` | `TestMaxMoney_SameAsCatalog` | như trên | PASS |
| Cổng kiểm (sau khi gộp main 80fc1b9) | build, vet, golangci-lint, gen-check, sqlc vet, govulncheck, oasdiff, catalog-check, jscpd, test-guard, gitleaks | `make ci` (lần 1: dừng ở `go test ./...` vì quá 20 phút trên DB chung, các cổng trước đều qua); sau gộp: `make build vet lint oasdiff catalog-check jscpd test-guard gitleaks` | `exit 0`; `0 issues.`; `Found 0 clones.`; `no leaks found` |
| Cổng kiểm sau lần gộp main thứ hai (a4492e7) và sửa sau review vòng 1 | như dòng trên + gen-check, sqlc-vet, vuln | `make build vet lint gen-check sqlc-vet vuln oasdiff catalog-check jscpd test-guard gitleaks` | `exit 0`; `0 issues.`; `affected by 0 vulnerabilities`; `Found 0 clones.`; `no leaks found` |
| Gói phụ thuộc: order, kiến trúc | `go test -race -count=1 ./tests/arch/... ./internal/modules/order/...` | như cột trái | `ok tests/arch`, `ok internal/modules/order`, `ok …/order/internal/pricing` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Nhân 128 bit bằng `math/bits.Mul64`/`Div64` thay cho `math/big` (kế hoạch: "nhân bằng `math/big` khi tích có thể
  tràn"). Cùng thư viện chuẩn, đúng tuyệt đối (thương luôn vừa 64 bit vì `base ≤ Σbase`), không cấp phát.
- `Allocate`: phần lẻ có trần `Base` (Q-T64-1).
- `ManualDiscountBP`: kế hoạch ghi "`Subtotal = 0` → 0". Mã trả 0 khi không có giảm tay (gồm mọi `Subtotal = 0` mà
  `Compute` sinh ra); riêng Totals không thể có từ `Compute` (có giảm mà `Subtotal = 0`, số âm, tỷ lệ vượt int64) trả
  `MaxInt64` để đơn luôn phải chờ duyệt (hướng an toàn), theo ghi chú reviewer vòng 1.
- Gộp main **hai** lần, cả hai trước reviewer vòng cuối: lần 1 tới 80fc1b9 (Task 325) trước vòng 1; main có thêm Task 23
  và a4492e7 (trạng thái task ra khỏi git) nên gộp lần 2 (fast-forward, không xung đột) trước vòng 2, để test-guard so
  đúng và reviewer duyệt bản đã gộp. Sau khi reviewer ĐẠT và commit: không gộp nữa.
- Vector chung sinh bằng `validInputGen().Example(i)`, i = 0…199 (rapid v1.3.0, hạt giống là `i`), 1–6 dòng.
  Lệnh: `go test ./internal/modules/order/internal/pricing -run TestVectors -update-vectors` (chạy hai lần cho cùng
  `shasum` cb04b04d…). 189/200 bộ có giảm cấp đơn hay mã, 25/200 có dòng bị giảm tới hết Base.

## Việc phát hiện thêm

- `make ci` toàn bộ (`go test -race ./...`) vượt 20 phút khi nhiều phiên cùng dùng PostgreSQL chung crm-shared; task này
  thuần Go nên chạy riêng gói đổi, `order`, `tests/arch` và mọi cổng khác của `make ci`. Điều phối viên chạy cả bộ khi gộp.
- `tests/deploy` `TestMakeMutation_CoversHighRiskDirs` đỏ **trên main** (a4492e7 mở rộng vùng rủi ro cao với
  `internal/platform/db`, `internal/platform/webhooks`, `internal/modules/inventory` nhưng `make mutation` chưa đột biến
  ba thư mục này). Task này không chạm Makefile hay `tests/`; để điều phối viên giao sửa.
- Task M2-16: thay `addMoney`/`mulMoney` và cách tính tổng của `order/create.go` (M1) bằng `pricing.Compute`, xóa cách cũ;
  bọc lỗi gói này trong `order/errors.go`.

## Reviewer

Vòng 1 (agent `reviewer`, trên bản đã gộp 80fc1b9): **ĐẠT**, không phát hiện phải sửa. Đã kiểm điều kiện `bits.Div64`
không panic, Allocate (Σ đúng, trần Base, thứ tự phần lẻ), RefundFor (tổng = Net, không âm), ManualDiscountBP (ceil),
vector tất định, bốn quyết định Q-T64-1…4. Ghi chú không bắt buộc đã xử lý: (1) `ManualDiscountBP` với Totals không thể
có từ `Compute` (số âm, `q++` quay vòng ở `MaxUint64`) trả 0 nghĩa là "không cần duyệt" → sửa thành `MaxInt64` (luôn
cần duyệt), thêm 7 ca test; (2) chú thích thứ tự kiểm lỗi của `Compute` sửa cho khớp mã; (3) `make ci` chưa trọn: đã
ghi ở Việc phát hiện thêm.

Vòng 2 (agent `reviewer`, trên bản đã gộp a4492e7): **ĐẠT**, không phát hiện phải sửa. Đã kiểm từng nhánh mới của
`ManualDiscountBP` (không chia cho 0, `Div64` không panic, `q++` không vượt `MaxInt64`, Totals hợp lệ cho [0, 10000]),
7 ca test mới đi qua đủ nhánh, chú thích `Compute` khớp mã. Ghi chú không bắt buộc (ghi lệch "`Subtotal = 0` → 0" vào
Khác kế hoạch) đã làm.
