# Task 201 — Lõi lượt mua: một transaction

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 201, M5-18). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn, chỉ thêm (0 chỗ tham chiếu cũ gãy).
  `promotion`: `FlashItemForBuy`, `GetFlashItemForBuy`, `Claim`, `Unclaim` (lỗi chưa xuất `errFlashClaimMissing`: bất biến hỏng, lớp HTTP trả 500).
  `inventory`: `TakeFlashUnits`, `HoldFlashUnits`. `order`: `FlashBuyInput`, `FlashBuyResult`, hằng kết quả
  `FlashBought`, `FlashSoldOut`, `FlashOverLimit`, `FlashNotOpen`, sentinel `ErrFlashRetry`, `Writer.BuyFlash`.
- Gói phụ thuộc ngược (go list): `order` ← cmd/crm, httpapi, httpapi/web, cash, cash/internal/refunds, conversation,
  conversation/internal/live, fulfillment, testkit/codtest, testkit/shiptest, wiring/webcache, tools/loadkit;
  `promotion` ← cmd/crm, httpapi, httpapi/web, order, wiring/webcache; `inventory` ← cmd/crm, httpapi, invtest, order,
  promotion, wiring/webcache, tools/loadkit. Chỉ thêm hàm nên không gãy chỗ gọi; chưa ai gọi `BuyFlash` (điểm cuối ở
  Task 203).
- Operation OpenAPI bị chạm, oasdiff: không (điểm cuối mua ở Task 203). Bảng lỗi HTTP: `internal/httpapi/errors_web.go` thêm
  `order.ErrFlashRetry` → 503 `retry_later` (mã có sẵn, không thêm `x-error-codes`), dòng trong `errors_snapshot_test.go`
  (test kiến trúc `TestErrorsMappedRealCode` đòi mọi sentinel xuất ra có dòng).
- Chỗ dùng trên màn hình: không.
- Tệp khác: `internal/modules/order/web.go` — tách công thức khóa nghiệp vụ đơn web thành `webIdempotencyKey` (không
  đổi hành vi, 1 chỗ gọi cũ + 1 chỗ mới).
- Câu sqlc, bảng, pha migration: không migration (bảng có từ 0532). Câu mới: `queries/promotion/flash_claims.sql`
  (`FlashItemForBuy`, `ClaimFlash`, `UnclaimFlash`), `queries/inventory/flash_take.sql` (`TakeFlashUnits`,
  `AnyOtherFlashUnitAvailable`, `HoldFlashUnits`, `InsertFlashReservation`), `queries/ord/flash.sql`
  (`FlashOrderByExternalRef`, `InsertFlashOrderItem`). Bảng chạm: `promotion.flash_sale_claims` (ghi),
  `promotion.flash_sale_items`, `flash_sales` (đọc, không khóa), `inventory.flash_sale_units` (khóa SKIP LOCKED, ghi),
  `inventory.stock_reservations` (chèn), `ord.orders`, `ord.order_items`, `ord.order_status_history`, sự kiện (chèn).
  Không chạm `inventory.stock_balances`.
- Thứ tự khóa (ADR-46): `flash_sale_claims` (nhóm 10, upsert một dòng) → `flash_sale_units` (nhóm 11, `FOR UPDATE SKIP
  LOCKED` theo id tăng dần) → dòng mới (`ord.orders`, `order_items`, `stock_reservations`, lịch sử, sự kiện). Không khóa
  `flash_sales`, `flash_sale_items`, `stock_balances`. Đơn mới (nhóm 7) chỉ là dòng chèn; cùng `ExternalRef` của tx
  khác thì chèn chờ unique (xử lý trong savepoint, xem Quyết định 2).
- Test sẽ chạy: `go test -race -count=1 -run '^TestFlashBuy' ./internal/modules/order/`; build/vet promotion, inventory;
  `tests/arch`; cả gói order, promotion, inventory ở ci-task (vùng rủi ro cao: order, inventory, promotion).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chưa có gì về lượt mua
  flash); `order.nextCode` (mã đơn duy nhất, Q-X1), `order.Get`, `Writer.recordCreated`, `webOrderByRef`,
  `SourceByKey(SourceKeyWeb)`, khóa nghiệp vụ `uuid.NewSHA1(NameSpaceURL, "web:"+ref)` của `web.go`, `insertOrder`
  (`create_m2.go`), `catalog.VariantsForSale`, `db.Savepoint`, `inventory.InsertReservation` (cần tenant_id ở tham số),
  `testkit.ExplainAsApp`, `invtest.Shop`, mẫu test của `promotion/flash_lifecycle_test.go`.
- Dùng lại / viết mới vì: dùng lại `nextCode`, `Get`, `recordCreated`, `SourceByKey`, `catalog.VariantsForSale`
  (ảnh chụp mã, tên), `db.Savepoint`, cùng công thức khóa nghiệp vụ của đơn web (một hàm `webIdempotencyKey` mới gọi ở
  cả `web.go` và `flashbuy.go`, để hai đường không lệch). Viết mới `insertFlashDraft` (kế hoạch: không qua `Create` vì
  `Create` gọi `inventory.Reserve` làm tăng held lần nữa; `insertOrder` gắn chặt với `prepared/drafted` của `Create`).
  Câu chèn dòng đơn mới `InsertFlashOrderItem` vì `InsertOrderItem` không có cột `flash_sale_item_id`. Câu đọc đơn theo
  mã ngoài mới `FlashOrderByExternalRef` vì cần `draft_expires_at` và món flash của dòng (`WebOrderByExternalRef` không
  có). Câu chèn giữ hàng mới `InsertFlashReservation` (lấy tenant từ `app.tenant_id`, không đổi số dư).

## Soát thiết kế

5.1 Chủ dữ liệu: claims thuộc `promotion`, đơn vị và giữ hàng thuộc `inventory`, đơn thuộc `order` (spec 04 mục 3);
mỗi câu SQL nằm trong `queries/<module>` của chủ bảng. 5.2 Không ghi bảng module khác: `order` gọi
`promotion.Claim/Unclaim/GetFlashItemForBuy` và `inventory.TakeFlashUnits/HoldFlashUnits`. 5.3 Đọc module khác: qua
hàm công khai (không đọc bảng). 5.4 Nguyên tử: một transaction của bên gọi (`RetryTx` với `TxFlashBuy`), `order` điều
phối. 5.5 Sau commit: sự kiện `order.created` qua hàng đợi sự kiện trong tx (outbox). 5.6 Không gọi ra ngoài. 5.7 Mọi
câu chạy trong tx tenant; không bảng mới. 5.8 Module không nhập HTTP. 5.9 Không có đường HTTP ở task này (Task 203).
5.10 Valkey: không dùng; quyết định bán chỉ từ PostgreSQL. 5.11 Không thư viện mới. 5.12 Theo mẫu `create_m2.go`
(điều phối nhiều module trong tx của bên gọi, lỗi bọc `%w`).

6.1 Đồng thời: giới hạn mỗi khách là một câu upsert có điều kiện (`… WHERE quantity + EXCLUDED.quantity <= limit`), hai
lượt cùng khách tuần tự hóa trên dòng claim; đơn vị lấy bằng `SKIP LOCKED`; đổi đơn vị có điều kiện `status =
'available'` và phải đổi đúng `len(ids)` dòng. 6.2 Gửi lại: `ExternalRef` là khóa tự nhiên (unique
`orders_external_ref_key` và `idempotency_key`); lượt hai trả đơn cũ, không lấy thêm đơn vị; hai lượt cùng mã ngoài
đồng thời: lượt sau chờ unique, lùi savepoint, trả claim, trả đơn đã có. 6.3 Thứ tự khóa: claims (10) → units (11) →
dòng mới; không khóa `stock_balances` nên không giao với đường hủy/đóng đợt (đóng đợt: `flash_sales` → `stock_balances`
→ `flash_sale_units`; lượt mua không giữ hai khóa trên). 6.4 Lỗi tạm: `RetryTx` chạy lại 40P01/55P03; đơn vị đang bị
khóa (bỏ qua bởi SKIP LOCKED) mà thiếu → `ErrFlashRetry`, bên gọi lùi cả tx, không lưu. 6.5 READ COMMITTED: mỗi câu
thấy bản đã commit mới nhất; câu `EXISTS` đơn vị trống khác (không khóa, loại các id đã lấy) phân biệt "hết" với "đang
bị khóa". 6.6 Không việc nền. 6.7 Không gọi bên ngoài. 6.8 Tiến trình chết: tx tự lùi, không còn claim, đơn, đơn vị
giữ (test panic FS12). 6.9 `TxFlashBuy`: lock_timeout 200 ms, statement_timeout 2 s. 6.10 Tiền: `int64` đồng,
`unit_price = sale_price`, tổng do trigger `check_order_totals` kiểm. 6.11 Quy luật: test rapid năm bất biến của kế
hoạch. 6.12 Không log; không dữ liệu cá nhân (đơn nháp không có SĐT, địa chỉ). 6.13 Quyền: kiểm ở điểm cuối Task 203.
Hàm chạy lại an toàn (ADR-46 điểm 3): `BuyFlash` gán lại mọi kết quả ở mỗi lần chạy, không giữ trạng thái ngoài tx.

## Quyết định thay chủ shop

1. `BuyFlash` là phương thức `Writer.BuyFlash(ctx, tx, in, a Actor)` thay cho hàm tự do của kế hoạch: `recordCreated`
   cần `Events`, `Queue`, `Realtime` của `Writer` và actor ghi lịch sử (khóa dịch vụ website). Khác: thêm Actor vào
   `FlashBuyInput` — không theo mẫu các lệnh ghi khác của order.
2. Hai lượt cùng `ExternalRef` chạy đồng thời: chèn đơn trong savepoint; đụng unique của mã ngoài/khóa nghiệp vụ → lùi
   savepoint, `Unclaim`, đọc lại và trả đơn đã có (`bought`). Khác: trả `ErrFlashRetry` để bên gọi chạy lại cả tx (tốn
   một lượt).
3. Đã có đơn cùng `ExternalRef` nhưng không phải đơn flash của món này → `ErrDuplicateOrder` (không trả "bought" cho đơn
   khác). Khác: trả `bought` với đơn đó như kế hoạch viết — sai nghĩa khi mã ngoài bị dùng lại cho món khác.
4. Thiếu đơn vị mà không còn đơn vị trống nào khác (ngoài các đơn vị vừa lấy) → `sold_out`, kể cả khi còn ít hơn `qty`
   (ví dụ còn 1, mua 2). Câu `EXISTS` loại các id đã lấy, nếu không lượt mua sẽ báo `ErrFlashRetry` mãi.
5. `Qty` ngoài 1–10 hay thiếu mã → `ErrInvalidInput`; biến thể của món không còn bán được → `ErrNotForSale` (lấy ảnh
   chụp mã, tên qua `catalog.VariantsForSale`). Không kiểm tạm dừng nhận đơn web ở lõi (thuộc điểm cuối Task 203).
6. Trước bước 3, `Claim` trả `false` thì đọc lại đơn theo `ExternalRef` (lượt cùng mã đồng thời vừa commit) trước khi
   trả `over_limit`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| [FS03] 10 suất, 50 khách đồng thời qty 1 → 10 bought, 40 sold_out; 10 đơn vị held, 10 giữ hàng held, held số dư không đổi, 10 đơn draft kênh web nguồn web hạn now+10 phút, lịch sử, 10 order.created | `TestFlashBuy_SoldOutUnderContention` | `go test -race -count=1 -run '^TestFlashBuy_' ./internal/modules/order/` | `ok … order 23.431s` (máy, PostgreSQL chung) |
| [FS05] giới hạn 2, một khách, 20 lượt khác mã ngoài → 2 bought, 18 over_limit, claims 2; qty 3 > giới hạn → over_limit | `TestFlashBuy_PerCustomerLimit` | như trên | ok |
| Cùng ExternalRef: lần lượt và 8 lượt đồng thời → cùng đơn, không thêm đơn vị/claim; giờ có phần dưới µs vẫn trả y hệt; mã ngoài của món khác → ErrDuplicateOrder | `TestFlashBuy_SameExternalRef` | `go test -race -count=1 -run '^TestFlashBuy_SameExternalRef$' ./internal/modules/order/` | `ok … 2.199s` |
| [ADR-40] tx khác khóa 1/2 đơn vị cuối 300 ms, mua qty 2 → ErrFlashRetry; sau lùi claims 0, 2 available, 0 đơn; nhả khóa → mua được | `TestFlashBuy_LockedUnitsRetry` | như dòng 1 | ok |
| [FS07] còn 1 mua 2 → sold_out, claim trả; 0 trống 4 held → sold_out | `TestFlashBuy_SoldOutMeansNoFreeSlot` | như dòng 1 | ok |
| [QĐ8] trước starts_at; đúng ends_at với đợt đã open (Tick); đợt nháp → not_open, không ghi; món không có → promotion.ErrNotFound; qty 0 → ErrInvalidInput | `TestFlashBuy_NotOpen` | như dòng 1 | ok |
| [FS12] panic ngay sau lượt mua trong RetryTx → 0 đơn, 3 available, claims 0, 0 giữ hàng | `TestFlashBuy_PanicRollsBack` | như dòng 1 | ok |
| Câu TakeFlashUnits (bắt câu sqlc thật) trên 15.000 đơn vị bằng crm_app → không Seq Scan | `TestFlashBuy_TakePlan` | như dòng 1 | ok |
| Rapid (200 lượt): 1–20 khách, giới hạn 1–3, 1–30 suất, qty 1–3, 1–25 người mua đồng thời, 1/5 hủy giữa chừng → 5 bất biến của kế hoạch, giữ hàng khớp dòng đơn, sold_out chỉ khi không đủ suất trống | `TestFlashBuy_Invariants` | như dòng 1 | ok |
| Bảng lỗi HTTP: ErrFlashRetry → 503 retry_later; mọi sentinel xuất ra có dòng ánh xạ | `TestDomainErrorSnapshot` (bảng `errors_snapshot_test.go`), `TestErrorsMappedRealCode` | `go test -count=1 -run Snapshot ./internal/httpapi/`; `go test -count=1 -run '^TestErrorsMappedRealCode$' ./tests/arch/` | ok; ok (lần đầu đỏ: thiếu dòng cho 2 sentinel, đã sửa) |
| Cổng tĩnh + test gói bị ảnh hưởng (race: httpapi, inventory, order, promotion; thường: gói phụ thuộc ngược) | ci-task | ci-task (bể ubicloud) | `CI-TASK XANH (từ xa trên ubicloud …)` trên bản cuối, run 45949106265: static, test 1–4 success (lượt trước 45947189907 xanh trên bản trước sửa làm tròn hạn) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `BuyFlash` là phương thức của `Writer`, thêm tham số `Actor` (Quyết định 1).
- `promotion.Unclaim` trả lỗi chưa xuất `errFlashClaimMissing` (kế hoạch chỉ ghi "0 dòng → lỗi"): bất biến hỏng, không
  cần mã HTTP riêng.
- Test thuộc tính chạy 200 lượt (mặc định `testkit.SetRapidChecks(200)` của gói order) thay vì 100.
- Hạn giữ làm tròn tới micro giây (độ chính xác timestamptz) để lượt lặp cùng mã ngoài trả đúng giá trị như lần đầu.

## Việc phát hiện thêm

- `order.ExpireDrafts` / `Cancel` của M2 nhả mọi dòng giữ hàng bằng đường thường (held −): với đơn nháp flash sẽ trừ
  held sai (đơn vị suất vẫn nằm trong held của đợt). Nhánh flash của `Cancel` đã có trong kế hoạch Task 202; tới lúc đó
  chưa có đơn flash thật vì điểm cuối mua ở Task 203.
- Lượt lặp cùng `ExternalRef` trả `bought` với đơn đó dù đơn đã hết hạn/hủy (lõi không xét trạng thái của đơn đã có).
  Điểm cuối Task 203/204 nên hiện trạng thái thật của đơn (ví dụ `hold_expired`).
- (Reviewer) Khóa nghiệp vụ đơn web `webIdempotencyKey` = `"web:"+ref`, không có người bán, trong khi unique mã ngoài
  có người bán: hai người bán cùng tenant dùng chung một mã ngoài → lượt mua nhận `ErrFlashRetry` mãi tới trần thử
  lại. Công thức có từ `CreateFromWeb` (Task 193); website hiện chỉ một người bán. Task 203 xét.

## Reviewer

Kết luận: ĐẠT (vòng 1, không có phát hiện phải sửa).

- Đủ yêu cầu, bằng chứng có thật cho mọi dòng; ảnh hưởng khớp (không tệp nào đổi mà mục Ảnh hưởng không nêu); tái dùng
  hợp lệ; không còn cách cũ (`"web:"` chỉ còn trong `webIdempotencyKey`); không test bị xóa/tắt; luật dự án đạt.
- Ghi chú không bắt buộc: khóa nghiệp vụ đơn web không có người bán (đã ghi vào Việc phát hiện thêm); nhánh flash của
  `Cancel`/`ExpireDrafts` phải làm trước Task 203 (Task 202); chạy lại ci-task trên bản cuối (đã chạy, xanh).
