# Task 438 — M2-r9: Test nghiệm thu còn thiếu AT05, WAT07, WAT31

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (task bù sau review độc lập M2: `docs/review-doc-lap-m2.md`, phát hiện E-05, E-06; số toàn cục 438 = M2-r9). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task chỉ thêm test trong gói `order_test` (`internal/modules/order`). Mã chạy được kiểm, không sửa: `confirm.go` (`Writer.Confirm`, `Writer.ApproveDiscount`: hiện không gọi `inventory`/`promotion`), `returns.go` `priceReturnLines` (`pricing.RefundFor(it.NetAmount, …)`).
- Gói phụ thuộc ngược (go list): không áp dụng (chỉ tệp `_test.go`; không gói nào nhập `order_test`).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có. Test đọc tồn qua `inventory.Availability` (helper `balance` có sẵn), đọc `promotion.coupons.used_count`, `promotion.coupon_redemptions`, `inventory.stock_ledger`, `inventory.stock_reservations` bằng `CountIn` (crm_app, trong tenant) như các test có sẵn; đổi giá bằng `UPDATE catalog.variants` như `TestCreate_PriceSnapshot`.
- Helper test đổi: `drawDeliveredOrder` (returns_property_test.go) thêm rút giảm cấp đơn ngẫu nhiên — dùng ở `TestReturns_Invariants` và `returns_qty_customer_test.go:139` (cả hai chạy lại); `deliveredOrder` gọi helper mới `deliveredFrom(in)` (giữ nguyên hành vi).
- Test sẽ chạy: khi viết `go test -race ./internal/modules/order/ -run '<tên test>'`; rồi cả gói `order` (vùng rủi ro cao); cổng ci-task một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit: `CountIn`, `Exec`); helper sẵn có của gói test order: `newM2Env`, `createIntent`, `confirm`, `approve`, `manager` (confirm_test.go), `coupon`, `sale10` (create_coupon_test.go), `balance`, `count` (env_test.go), `deliveredOrder`, `mustReturn`, `custReturn`, `rl`, `itemOf` (returns_test.go), `usedCount` (cancel_test.go).
- Dùng lại / viết mới vì: dùng lại các helper trên; chỉ viết mới `deliveredFrom(in order.CreateInput)` (tách thân của `deliveredOrder` để giao được đơn có giảm cấp đơn và mã) và một helper chụp trạng thái giữ hàng/lượt mã trước–sau xác nhận (chưa có helper nào chụp cùng lúc số dư, `used_count`, số dòng lượt, số dòng sổ kho).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng — không thêm mã chạy, bảng, view, đường ghi hay thư viện. Test theo module mẫu: gói `_test`, PostgreSQL thật, mọi câu chạm bảng có `tenant_id` qua `CountIn`/`app` (trong tenant, Bẫy RLS).
- 6.1 Đồng thời: AT05/WAT07 nửa "tranh chiếc cuối/lượt cuối" đã có test (`TestCreate_LastUnitRace`, `TestCreateCoupon_ConcurrentLastUse`); task này thêm nửa còn lại: xác nhận ở mọi đường (new → confirm, draft → confirm, chờ duyệt → quản lý duyệt, chờ duyệt → quản lý xác nhận, nháp chờ duyệt → duyệt → chốt) không giữ thêm hàng, không ghi lượt mã lần hai. Mã có `max_uses 1` nên nếu Confirm gọi lại `Redeem` thì bị `exhausted` (test đỏ); `used_count`, số dòng `coupon_redemptions`, số dư và sổ kho vẫn được assert trực tiếp.
- 6.2–6.9: không áp dụng (không đổi đường ghi, khóa, việc nền).
- 6.10 Tiền (WAT31): ca database — đơn 2 dòng có giảm cấp đơn và mã (phân bổ khác 0 trên cả hai dòng), đã giao, đổi giá cả hai biến thể, trả một phần → tiền hoàn bằng phần của `net_amount` đã chụp (`floor(net × trả / mua)`, đơn vị cuối nhận phần còn lại), không theo giá mới, không theo `unit_price × số − giảm dòng`; trả hết → tổng hoàn = Σ net. Rapid `TestReturns_Invariants` rút thêm giảm cấp đơn để quy luật Σ hoàn ≤ net / = net phủ cả phân bổ cấp đơn.
- 6.11 Quy luật: như trên (rapid có sẵn, mở rộng miền sinh).
- 6.12 Dữ liệu cá nhân: không áp dụng (không log mới).
- 6.13 Quyền: không đổi; ca quản lý dùng `Actor.CanApproveDiscount` như test có sẵn.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| AT05 nửa sau, WAT07 (CRM) nửa sau: xác nhận ở 5 đường (new → confirm; nháp → chốt; chờ duyệt → quản lý duyệt; chờ duyệt → quản lý xác nhận; nháp chờ duyệt → duyệt → chốt) không đổi số dư (Held 1, Available 4), Σ giữ, số dòng giữ, số dòng sổ kho; mã `max_uses 1`: used_count 1, đúng 1 lượt | `confirm_hold_test.go: TestConfirm_NoSecondHoldOrRedemption` (5 ca con) | `go test -race -count=1 ./internal/modules/order/ -run TestConfirm_NoSecondHoldOrRedemption` | `ok … order 1.872s` |
| Test bắt được lỗi "giữ thêm khi xác nhận" | mutant tạm trong `confirm.go`: sau `applyTransition(Confirmed)` gọi `inventory.ResizeHeld(item, qty+1)` | như trên | 5/5 ca con FAIL: "trước … Held:1 Available:4 reserved:1 ledger:2 … sau … Held:2 Available:3 reserved:2 ledger:3"; khôi phục (`git checkout`) → qua |
| Test bắt được lỗi "dùng mã lần hai khi xác nhận" | mutant tạm: sau `applyTransition(Confirmed)` gọi `promotion.Redeem` với mã của đơn | như trên | 5/5 FAIL "bước N: promotion: đơn đã có mã đang dùng"; khôi phục → qua |
| WAT31 ca database: đơn A 250.001 ×3 (giảm dòng 1) + B 180.000, giảm cấp đơn 50.001 + mã 10% (88.000), phân bổ > 0 trên cả hai dòng, Σ phân bổ 138.001; đổi giá A→300.000, B→150.000; trả A ×1 + B ×1 → A = floor(netA/3), B = netB; trả A ×2 cuối → netA − floor(netA/3); Σ hoàn = Σ net = tổng − ship | `returns_test.go: TestCreateReturn_WAT31OrderDiscountAfterPriceChange` | `go test -race -count=1 ./internal/modules/order/ -run TestCreateReturn_WAT31` | `--- PASS (0.28s)` |
| Test bắt được lỗi "hoàn theo đơn giá, bỏ phần giảm cấp đơn" và "hoàn theo giá hiện tại" | mutant trên bản sao `git archive HEAD` ngoài worktree (không sửa mã sinh trong repo): câu `LockOrderItemsForReturn` trả `quantity*unit_price − line_discount`, rồi `… + giá catalog hiện tại × quantity` | `go test ./internal/modules/order/ -run 'TestCreateReturn_WAT31\|TestCreateReturn_RoundingLastUnit\|TestCreateReturn_TD05'` | mutant 1: chỉ test mới FAIL "A 250000 (mong 212903), B 180000 (mong 153291)"; mutant 2: FAIL "A 262902 (mong 212903), B 123291 (mong 153291)"; hai test cũ vẫn qua (đúng lỗ hổng E-06) |
| Rapid phủ giảm cấp đơn | `TestReturns_Invariants`, `TestReceiveReturnQty_CustomerInvariants` (dùng chung `drawDeliveredOrder`, nay rút thêm `OrderDiscount` 0–5% tạm tính) | bản sao + mutant 1: `go test -run TestReturns_Invariants` | bản mới: FAIL "[rapid] failed after 85 tests: … hoàn 55372 > net 55331"; bản cũ (HEAD) trên cùng mutant: `ok` (không bắt được). Mã thật: cả hai PASS (9,98 s; 19,93 s) |
| Không hỏng test có sẵn của confirm, trả hàng | `TestConfirm*`, `TestApproveDiscount*`, `TestCreateReturn*`, `TestReceiveReturn*`, `TestReceiveReturnQty*`, `TestReturns_Invariants` | `go test -race -count=1 ./internal/modules/order/ -run 'TestReturns_Invariants\|TestReturnQty\|TestCreateReturn\|TestReceiveReturn\|TestConfirm\|TestApproveDiscount' -v` | 28 test PASS, `ok … 37.077s` |
| Cổng tĩnh + gói đổi | ci-task | `scripts/agent/ci-task.sh` | lần 1 ĐỎ: `TestLintCleanOnRealCode` gocognit 22 > 20 ở test mới → tách `checkConfirmKeepsHold`; lần 2: "CI-TASK XANH (so với điểm tách nhánh dd70e9a2…)", `ok …/modules/order 407.657s`, `ok …/tests/arch 25.602s`, jscpd 0 clones, gitleaks no leaks, oasdiff, test-guard qua |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ngoài test, cập nhật `docs/nghiem-thu-m2.md` (dòng AT05, WAT07, WAT31 và tiêu chí 2, 4) cho khớp test mới.

## Quyết định thay chủ shop

- WAT31 ghi "đạt phần giảm cấp đơn, mã"; phần phí ship và hoa hồng affiliate của WAT31 không thuộc M2 (M3, M6), không tự viết test cho chúng. Phương án khác: ghi WAT31 đạt trọn — sai vì spec nêu cả ship, affiliate.
- Mutant chứng minh test đỏ chạy trên bản sao `git archive HEAD` ngoài worktree (câu SQL sinh) hoặc sửa tạm rồi `git checkout` (confirm.go), không để lại thay đổi mã chạy nào.

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (vòng 1, agent reviewer trên `git diff --cached`, main dd70e9a — không có gì mới để gộp). Không có phát hiện phải sửa. Reviewer đối chiếu: test chụp và so cả struct trạng thái (số dư, Σ giữ, số dòng giữ, sổ kho, used_count, lượt active, tổng lượt) trước–sau xác nhận ở 5 đường; ca 100.001/1.000.000 vượt ngưỡng `>` (discountpolicy.go:26) nên đi đúng đường chờ duyệt; WAT31 lấy số mong đợi từ `NetAmount` đã chụp, không từ giá mới; rapid giữ tổng giảm tay ≤ 10%; không Skip, không nới assert, không đổi mã chạy, mã sinh, migration. Ghi chú không bắt buộc (đã sửa): mục Ảnh hưởng ghi nhầm tên bảng `inventory.stock_movements`, test đọc `inventory.stock_ledger`.
