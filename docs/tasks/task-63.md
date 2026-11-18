# Task 63 — M2-06: Khuyến mãi — schema, đánh giá mã, lượt dùng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task M2-06, số toàn cục 63). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** ký hiệu mới trong `internal/modules/promotion`
  (`Coupon`, `EvalInput`, `Evaluate`, `Redemption`, `Redeem`, `Release`, `Detach`, `Reprice`, `CheckResult`, `Check`).
  Không sửa ký hiệu đã có: `Reason`, 10 hằng `Reason*`, `RejectError`, 4 sentinel (Task 59) giữ nguyên; chỗ dùng hiện
  có: `internal/httpapi/errors.go` (`promotionError`, `*promotion.RejectError`) — không đổi.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi` nhập `promotion` (chỉ `Background`, lỗi); chưa gói nào
  gọi hàm mới (order dùng ở Task M2-08, M2-14).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml` (operation coupon vẫn 501 tới Task M2-14).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: migration mới `0206_promotion.sql` (pha mở rộng: tạo schema `promotion`, 4 bảng,
  view `promotion.coupons_v`, GRANT); câu mới `queries/promotion/{coupons.sql,redeem.sql}`; khối mới trong `sqlc.yaml`
  sinh `internal/modules/promotion/internal/store`. Index `promotion.coupons_code_key` miễn luật (g) (dòng mới ở
  `docs/views.md`). `sqlc generate` sinh lại `models.go` của mọi gói store (sqlc đưa mọi bảng của `migrations/` vào
  models của từng khối: thêm 4 kiểu `Promotion*`, không đổi kiểu có sẵn): `internal/platform/internal/store`,
  `internal/modules/{identity,catalog,inventory,customer,order,fulfillment,cash}/internal/store`.
- Tệp thêm cho test: `internal/modules/promotion/export_test.go` (đưa `errOrderHasCoupon` ra test gói ngoài).
- Test sẽ chạy: `go test -race ./internal/modules/promotion/...`; vì đổi migration, cổng kiểm của task chạy toàn bộ
  (gồm `tests/arch`: viewsdoc, sqlusage, boundaries, sqlc vet; luật schema).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db, tenancy,
  logging, textnorm); grep `bits.Mul64`/`mulDiv`; `customer.NormalizePhone`; `testkit` (`OpenStaff`, `CheckViews`,
  `SetRapidChecks`), `invtest.NewShop`.
- Dùng lại / viết mới vì: dùng lại `logging.Phone` (tự che), `customer.NormalizePhone` (so SĐT gắn mã; promotion được
  gọi customer theo bảng "Được gọi"), sentinel và `RejectError` của Task 59, `testkit.*`, `invtest.NewShop` (người bán,
  kho cho đơn thử). Viết mới `percentOf` (floor(a×b/10000), tích 128 bit): bản giống là `mulDiv` trong
  `order/internal/pricing` (phần riêng của order, promotion không được nhập order); 3 dòng, khác lý do thay đổi (luật
  mã giảm vs chia giảm theo dòng) nên giữ riêng.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

Mục 5:
- 5.1 chủ dữ liệu: mã, đợt mã, lượt dùng thuộc `promotion` (04 mục 3) → schema `promotion` mới.
- 5.2 không ghi bảng module khác: chỉ ghi `promotion.*`; khóa ngoại tới `ord.orders`, `customer.customers`,
  `catalog.sellers` chỉ khai ở migration.
- 5.3 đọc qua view: câu `queries/promotion` không nhắc bảng hay view của module khác; module khác đọc `promotion.coupons_v`.
- 5.4 nguyên tử: `Redeem`, `Release`, `Detach`, `Reprice` nhận `pgx.Tx` của bên gọi (đơn), không tự mở transaction.
- 5.5 không áp dụng: không phát sự kiện ở task này. 5.6 không áp dụng: không gọi ra ngoài.
- 5.7 tenant: 4 bảng có `tenant_id`, `UNIQUE (tenant_id, id)` (trừ bảng khóa ghép), khóa ngoại ghép, `enable_tenant_rls`;
  mọi hàm chạy trong tx tenant của bên gọi; test tenant B không thấy mã của A.
- 5.8 không HTTP: không nhập `net/http`, ogen.
- 5.9 không áp dụng: không thêm đường ghi HTTP (M2-14).
- 5.10 không áp dụng: không Valkey.
- 5.11 không thêm thư viện (pgx, uuid, rapid đã có).
- 5.12 theo module mẫu: store sqlc riêng, lỗi sentinel và `RejectError`, test gói `_test` trên PostgreSQL thật, rapid.

Mục 6 (kế hoạch giao câu 1, 3, 5, 11; thêm 10, 12):
- 6.1 đồng thời: `UPDATE coupons SET used_count = used_count + 1 WHERE … AND (max_uses IS NULL OR used_count <
  max_uses)` và `INSERT … ON CONFLICT DO UPDATE … WHERE used < limit`; 0 dòng → từ chối; CHECK ở database chặn vượt.
  Test 20 goroutine `max_uses 1`; hai đơn cùng khách `per_customer_limit 1`.
- 6.3 thứ tự khóa: nhóm 10 ADR-46 `coupons` → `coupon_customer_uses` → `coupon_redemptions`. `Release`, `Detach`,
  `Reprice` đọc dòng lượt (không khóa) để biết mã, khóa `coupons` FOR UPDATE, đọc lại dòng lượt sau khi giữ khóa (mọi
  đường ghi lượt của một mã đều giữ khóa dòng mã trước), rồi đổi `coupon_customer_uses` trước `coupon_redemptions`.
- 6.5 READ COMMITTED + khóa dòng mã + câu có điều kiện; không đổi mức cách ly.
- 6.11 rapid 200 lượt: chuỗi Redeem/Release/Detach ngẫu nhiên → `used_count` = số lượt active của mã, `used` của
  khách = số lượt active của khách, không vượt `max_uses`, `per_customer_limit`.
- 6.10 tiền int64 đồng; phần trăm tính tích 128 bit, không tràn với Base tới 2^53−1.
- 6.12 SĐT gắn mã là `logging.Phone`; không đưa vào lỗi; view không có `bound_phone`, `bound_customer_id`.
- 6.2, 6.4, 6.6–6.9, 6.13 không áp dụng: không có đường HTTP, việc nền, gọi ra ngoài, quyền ở task này (bên gọi lo;
  chống trùng của đơn là khóa chống trùng của đường tạo đơn và `UNIQUE (tenant_id, coupon_id, order_id)`).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi viết mã | các test dưới | `go vet ./internal/modules/promotion/` (trước khi có `coupon.go`, `evaluate.go`, `redeem.go`) | `undefined: promotion.Coupon` (không biên dịch) |
| Evaluate: 660000→50000, 400000→40000, 333333→33333, fixed 30000/Base 20000→20000, min_subtotal 299999, not_started 30/11, expired đúng valid_to 31/12 00:00, channel, bound_other (SĐT khác), disabled, seller S2/S1; thứ tự kiểm; Base 2^53−1 không tràn; không kiểm lượt | `TestEvaluate` (29 ca, `priceCases`, `ruleCases`) | `go test -race -count=1 -v ./internal/modules/promotion/` | `--- PASS: TestEvaluate` |
| Redeem đồng thời max_uses 1, 20 goroutine → 1 thành công, 19 exhausted, used_count 1 | `TestRedeem_ConcurrentMaxUses` | như trên | `--- PASS: TestRedeem_ConcurrentMaxUses (2.14s)` |
| per_customer_limit 1: cùng khách hai đơn đồng thời → 1 thành công, 1 customer_limit; khách khác thành công | `TestRedeem_PerCustomerLimit` | như trên | `--- PASS` |
| Redemption trả về, mã không phân biệt hoa thường, not_found, disabled (không đổi lượt), channel, bound_other, lỗi không chứa SĐT, đơn đã có mã → chặn; unique active mỗi đơn ở database | `TestRedeem_ResultAndRejects` | như trên | `--- PASS` |
| Release → used_count 0, dòng released (released_at, release_reason), used của khách 0; lần hai false không đổi; mã tắt cờ → false, used_count giữ 1 | `TestRelease` | như trên | `--- PASS` |
| Detach rồi Redeem lại cùng đơn → dòng cũ về active, một dòng, used_count 1 | `TestDetach_ThenRedeemAgain` | như trên | `--- PASS` |
| Reprice theo Base mới; dưới min_subtotal → RejectError; đơn không mã → 0 | `TestReprice` | như trên | `--- PASS` |
| Check: không đổi lượt, lượt còn, customer_limit, exhausted, not_found, min_subtotal | `TestCheck` | như trên | `--- PASS` |
| used_count = -1 (và vượt max_uses) → vi phạm CHECK 23514 | `TestSchema_UsedCountCheck` | như trên | `--- PASS` |
| View coupons_v: security_invoker, cột khớp docs/views.md, không bound_phone/bound_customer_id, tenant B không thấy mã A | `TestSchema_CouponsView`; `TestViewsDocMatchesDatabase` | như trên; `go test -count=1 -run 'TestViewsDoc' ./tests/arch/` | `--- PASS`; `--- PASS: TestViewsDocMatchesDatabase` |
| Luật tenant, luật (g) (coupons_code_key seller-exempt) | `TestSchema_Rules` | như trên | `--- PASS` |
| Quy luật rapid 200 lượt: used_count = số active của mã, used = số active của khách, không vượt giới hạn, kết quả khớp mô hình | `TestRedemption_Invariants` | như trên (rapid.checks 200 từ `TestMain`) | `--- PASS: TestRedemption_Invariants (133.14s)` |
| Câu sqlc qua sqlc vet; queries/promotion không nhắc bảng module khác; ranh giới gói; lỗi xuất đều ánh xạ | `TestSqlcVet`, `TestSQLUsage*`, `TestBoundaries*`, `TestErrorsMapped*` | `go test -count=1 -v -run 'TestViewsDoc|TestSQLUsage|TestBoundaries|TestSqlcVet|TestErrorsMapped' ./tests/arch/` | 11 `--- PASS` |
| Cổng kiểm của task | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, test toàn bộ | cổng kiểm `ci-task.sh` (một lần, trước khi stage); sau đó `make lint`, `make gen-check`, `make jscpd` và `go test -race -count=1` các gói đã đỏ | Lần chạy cổng: lint (gocognit test rapid), gen-check (mã sinh chưa stage), jscpd (2 đoạn trùng trong test) đỏ → đã sửa: `0 issues.`, gen-check xanh, `Found 0 clones.`; phần test toàn bộ: 241 lỗi đều `SQLSTATE 57P03` (PostgreSQL chung vào chế độ khôi phục giữa chừng, không do task này) và `TestImager_HangKilledWithinDeadline`, `TestNotSentPastExpiryExpires` (đo thời gian lúc máy tải cao) → chạy lại `imager`, `files`, `observability`, `queue`, `realtime`, `schedules`, `commands`, `order`, `promotion`: tất cả `ok`; các gói khác `ok` ngay lần đầu (gồm `tests/arch`, `tests/e2e`) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `coupon_customer_uses` đếm lượt của mọi mã (kế hoạch: chỉ mã có `per_customer_limit`). Câu chèn có điều kiện
  `per_customer_limit IS NULL OR used < limit`. Lý do: số đếm luôn đúng nếu M2-14 sau này cho đặt giới hạn mỗi khách
  trên mã đã dùng, và quy luật rapid "used của khách = số lượt active" đúng cho mọi mã. Đảo lại: bỏ một nhánh trong
  `Redeem` (lúc đó `Release` vẫn đúng vì `used > 0`).
- Thêm `TestSchema_Rules` (luật tenant, luật (g) của 4 bảng mới) ngoài danh sách test của kế hoạch.

## Việc phát hiện thêm

(không có)

## Quyết định thay chủ shop

1. **Đơn đã có lượt mã active mà gọi `Redeem` lần nữa** → lỗi `errOrderHasCoupon` (không xuất, không ánh xạ HTTP,
   ra 500): kế hoạch giao order Detach trước khi đổi mã nên đây là lỗi lập trình; unique `(tenant_id, order_id) WHERE
   status = 'active'` (Q27) là chốt cuối. Phương án khác: sentinel xuất + mã HTTP 409 (phải sửa `errors.go`, chạm tệp
   dùng chung), hoặc coi là thành công (che lỗi của bên gọi). Đảo lại dễ: đổi tên thành `ErrOrderHasCoupon` và thêm
   một dòng ánh xạ.
2. **Mã gắn khách và/hoặc SĐT**: đơn dùng được khi khớp ít nhất một thứ đã gắn (khách gốc trùng, hoặc SĐT đơn chuẩn hóa
   bằng `customer.NormalizePhone` trùng `bound_phone`). Kế hoạch không nói ca gắn cả hai. Phương án khác: đòi khớp cả
   hai (chặt hơn; khách đổi số sẽ bị từ chối).
3. **`Reprice` khi đơn không có lượt active** → `(0, nil)` (đơn sửa mà không có mã thì phần giảm mã là 0).
4. **`Check` trả mã bị từ chối trong kết quả** (`OK = false`, `Reason`), không phải lỗi, khớp `CouponCheck` của hợp
   đồng (`ok`, `discount`, `reason`, `remainingUses`). `RemainingUses` chỉ có khi mã qua `Evaluate` và có `max_uses`.
5. **CHECK thêm ở database** (ngoài kế hoạch, đều là ràng buộc mà hợp đồng API đã có): mẫu mã `^[A-Za-z0-9-]{3,32}$`,
   `channels` ⊂ {facebook, web, manual}, `bound_phone` dạng `+84…`, cột tiền trong [0, 2^53−1], `max_uses ≥ 1`,
   `per_customer_limit ≥ 1`, dòng `released` có đủ `released_at` và `release_reason` (1–100 ký tự), tên 1–120 ký tự.
   Thêm sau khó (dữ liệu đã có), bỏ thì dễ.
6. **`created_by` không có khóa ngoại tới `identity.users`**, như `ord.orders`, `customer.*` (mẫu sẵn có).
7. **Quyền `crm_app`**: SELECT, INSERT 4 bảng; UPDATE theo cột chỉ cho cột mà task này đổi (`used_count`, `used`,
   cột trạng thái của lượt). M2-14 (sửa mã) thêm quyền cột của nó. `USAGE` schema cho cả `crm_search` như 0112.
8. **Lý do trả lượt khi `Detach`** ghi `detached`.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, lần 1, trên phần đã stage sau khi gộp main — main không đổi so với điểm tách).

- Phát hiện phải sửa: không có. Đã kiểm thứ tự khóa nhóm 10, tràn số `percentOf`, SĐT không vào lỗi/view, migration
  chỉ CREATE/GRANT, ranh giới module, `Redeem` từ chối sau khi đã tăng lượt là đúng vì bên gọi lùi cả transaction.
- Ghi chú không bắt buộc: (1) câu bảng bằng chứng về "không đổi lượt" chỉ đúng cho mã disabled → đã sửa câu;
  (2) `lockActive` mất lỗi gốc nếu lần đọc lại thứ ba gặp lỗi database thật (gần như không xảy ra, transaction vẫn
  lùi) → giữ nguyên; (3) `release` bỏ qua số dòng của hai câu trừ lượt (luôn 1 khi giữ khóa mã, rapid đã kiểm) → giữ.
