# Task 520 — Flash: mã đơn website gửi lại với SĐT khác, xóa trạng thái cổng của một món

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (sửa lỗi phát hiện ở Task 204; không
có mục riêng trong kế hoạch, giao qua ghi chú điều phối M5; nền: Task 201 / M5-18, Task 203 / M5-20, Task 204 / M5-21).
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `cache.AdmissionGate` thêm phương thức `ClearState(ctx, tenant, item) error`. Bên hiện thực: `cache.gate` (gate.go)
    và bản giả `fakeGate` (`internal/httpapi/admission_test.go`) — thêm phương thức cho cả hai. Bên dùng interface:
    `cmd/crm/servicekey.go`, `internal/httpapi/{server,admission}.go`, `internal/httpapi/web/{catalog,flash_state}.go`,
    `internal/wiring/flashstate/loop.go` (chỉ loop gọi phương thức mới).
  - `order.Writer.BuyFlash` (chữ ký không đổi; hành vi: mã ngoài đã có đơn flash cùng món nhưng của khách khác →
    `ErrDuplicateOrder` thay vì trả đơn đó). Chỗ gọi: `internal/httpapi/web/flash.go` (`Flash.Buy`), test order,
    flashstate, e2e.
  - Không ký hiệu nào đổi tên.
- Gói phụ thuộc ngược (go list): `internal/platform/cache` ← cmd/crm, httpapi, httpapi/web, platform/observability,
  wiring/flashstate, wiring/webcache. `internal/modules/order` ← nhiều gói (chữ ký không đổi; chỉ store sinh lại thêm cột).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. `webFlashBuy` trả thêm ca 422 `idempotency_key_reused`
  cho mã ngoài của khách khác — mã này đã có trong `x-error-codes` và đã trả cho mã ngoài của món khác (QĐ-204-5).
- Tài liệu sinh: `docs/danh-muc-dung-chung.md` (`make catalog`, chú thích của `AdmissionGate` có thêm ClearState).
- Chỗ dùng trên màn hình: không (điểm cuối của website; console không gọi).
- Câu sqlc, bảng, pha migration: `queries/ord/flash.sql` `FlashOrderByExternalRef` thêm cột `o.customer_id` (đọc, không
  bảng mới, không migration). Sinh lại `internal/modules/order/internal/store`. Bên dùng câu: `flashbuy.go`,
  `flashcomplete.go` (không dùng cột mới).
- Test sẽ chạy: `order` `-run '^(TestFlashBuy_SameExternalRef|TestFlashBuy_RefOfOtherCustomer|TestFlashBuy_RefOfOtherCustomerParallel)$'`;
  `cache` `-run '^TestAdmissionGate_ClearState$'`; `flashstate` `-run '^(TestLoop_CloseClearsState|TestLoop_SignalsRecompute)$'`;
  `httpapi/web` `-run '^(TestWebFlashBuy_RefOtherPhone|TestWebFlashBuy_RefOtherPhoneParallel|TestWebFlashBuy_ExternalRefTakenReleasesKey)$'`; build/vet `httpapi` (fakeGate). Cả gói ở ci-task (order, cache,
  flashstate, httpapi, httpapi/web — vùng rủi ro cao order).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục cache (AdmissionGate,
  EntityKey, eval, Conn.Do); tìm "Clear\|Delete\|Del(" trong `internal/platform/cache` — không có lệnh xóa một khóa
  nào (bộ đệm đọc làm mới bằng phiên bản, không DEL); cổng không có lệnh xóa.
- Dùng lại / viết mới vì: dùng lại `EntityKey("fs", …)` (cùng khóa của SetState), `Conn.Do` (phân loại lỗi
  ErrUnavailable), `ErrDuplicateOrder` + dòng ánh xạ có sẵn (422 `idempotency_key_reused`, `flashRefusal` trả lại khóa
  chống trùng), câu `FlashOrderByExternalRef` (chỉ thêm cột). Viết mới: `gate.ClearState` (lệnh DEL một khóa), sổ
  "món đã ghi" trong `flashstate.Loop` (chỉ vòng biết món nào nó đã ghi).

## Soát thiết kế

- 5.1–5.3: không bảng, không migration; câu sqlc của order chỉ thêm cột của bảng order.
- 5.4: lượt mua vẫn một transaction (RetryTx TxFlashBuy qua `WriteTxWith`); từ chối mã ngoài của khách khác là
  `ErrDuplicateOrder` → `flashRefusal` → fn trả không-final → transaction lùi (claim, đơn vị đã khóa bỏ hết), trả lại
  khóa chống trùng (không lưu kết quả của người kia theo khóa).
- 5.5–5.6: ClearState gọi Valkey **ngoài** transaction (refreshTenant ghi cổng sau khi tx đọc xong), như SetState.
- 5.7: câu đọc vẫn trong tenancy.WithTenant.
- 5.8: handler không đổi; lỗi đổi mã ở `errors_order.go` (có sẵn).
- 5.9: quyền, Idempotency-Key không đổi.
- 5.10: cổng chỉ là gợi ý; xóa trạng thái → `known = false` → lượt mua xuống database (an toàn cả khi xóa nhầm).
- 5.11: không thư viện mới. 5.12: theo mẫu SetState (gate) và `flashOrderByRef` (đơn khác → ErrDuplicateOrder).
- 6.1 (hai lượt cùng mã ngoài, hai SĐT, song song): bước 2 đọc không khóa; bước 5 chèn đụng unique `orders_external_ref_key`
  → `sameRefOrder` → Unclaim → `flashOrderByRef` so `customer_id` → ErrDuplicateOrder. Ba chỗ trả đơn theo mã ngoài
  (bước 2, sau claim thất bại, sau đụng unique) đều qua `flashOrderByRef` nên cùng một phép so. Test song song ở order
  và HTTP.
- 6.2: không lộ thông tin: lỗi 422 thân `{"error","request_id"}`, không mã đơn, không hạn giữ; trả lỗi trước khi lưu.
- 6.3: không thêm khóa. Thứ tự khóa ADR-46 không đổi.
- 6.4: Valkey lỗi khi ClearState → lỗi bọc ErrUnavailable, vòng log lần đầu của chuỗi lỗi, món vẫn ở sổ để lượt sau xóa
  lại; trạng thái tự hết hạn 10 s.
- 6.5: READ COMMITTED; vòng đọc món còn hoạt động rồi xóa món đã rời phạm vi: nếu đợt mở lại giữa chừng (không có
  đường mở lại đợt đã đóng) lượt sau ghi lại; xóa nhầm chỉ làm `known = false`.
- 6.6–6.7: không việc nền mới, không gọi dịch vụ ngoài.
- 6.8: hai tiến trình api: mỗi vòng có sổ riêng, mỗi vòng nhận báo hiệu flash_sale và tự xóa; tiến trình khởi động lại
  (sổ rỗng) → trạng thái cũ tự hết sau 10 s như trước.
- 6.9–6.13: fn trong RetryTx đã gán lại (out, refused); không log SĐT; sổ của vòng chỉ đọc/ghi trong goroutine `run`
  (refresh chỉ được gọi từ đó), không cần khóa.

## Quyết định thay chủ shop

- QĐ-520-1: mã ngoài đã có đơn của khách khác (SĐT khác) → 422 `idempotency_key_reused` (dùng lại `ErrDuplicateOrder`,
  cùng mã với "mã ngoài đã dùng cho món khác", QĐ-204-5), không thêm mã lỗi mới. Vì sao: với website cả hai đều là
  "mã đơn này đã dùng, sinh mã mới", thân lỗi không mang thông tin đơn kia, không đổi hợp đồng. Phương án khác: mã mới
  `external_ref_taken` (409) — phải đổi openapi, website và câu tiếng Việt, không thêm giá trị.
- QĐ-520-2: "cùng người" so theo `customer_id` của đơn với khách tìm/tạo từ SĐT của lượt mới (SĐT là danh tính khách,
  `FindOrCreateForOrder`). Phương án khác: so SĐT trên đơn — đơn nháp flash chưa có địa chỉ/SĐT giao hàng.
- QĐ-520-3: kế hoạch không đòi xóa trạng thái cổng (trạng thái tự hết 10 s), nhưng khi đợt đóng/hủy món rời khỏi
  `FlashRemaining` nên cờ, số còn cũ còn tới 10 s (cờ "hết" cũ trả sold_out thay vì not_open). Thêm `ClearState` và gọi
  ở vòng dựng lại cho mọi món vòng đã ghi mà lượt đọc trong phạm vi không còn trả (đóng đợt, hủy đợt, món bị bỏ).
  Hết suất: không xóa — đã ghi cờ hết (`SetState(0, true)`) ở handler và vòng; xóa sẽ làm mất cờ. Phương án khác: để tự
  hết hạn (không làm gì) — trái yêu cầu của điều phối.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mã ngoài đã có đơn của khách khác → ErrDuplicateOrder, không trả đơn kia, claim/đơn vị không đổi; khách gốc gửi lại vẫn đúng đơn | `TestFlashBuy_RefOfOtherCustomer` | `go test -race -count=1 -run '^(TestFlashBuy_RefOfOtherCustomer\|TestFlashBuy_RefOfOtherCustomerParallel)$' ./internal/modules/order/` | `ok … order 2.175s`. Bỏ phép so `CustomerID` → đỏ: `mã ngoài của khách khác: {Result:bought … Code:DH0000001 …}, <nil>` |
| Song song 8+8 lượt hai khách cùng mã ngoài: một khách bought một đơn, khách kia ErrDuplicateOrder; held 1, đã lấy 1 | `TestFlashBuy_RefOfOtherCustomerParallel` | như trên | `ok` |
| HTTP: SĐT khác cùng mã đơn → 422 `idempotency_key_reused`, thân không có mã đơn, khóa trả lại, 1 đơn; SĐT gốc khóa mới → đúng mã | `TestWebFlashBuy_RefOtherPhone` | `go test -race -count=1 -run '^(TestWebFlashBuy_RefOtherPhone\|TestWebFlashBuy_RefOtherPhoneParallel)$' ./internal/httpapi/web/` | `ok … httpapi/web 4.301s` |
| HTTP song song 8+8 (mỗi lượt một khóa): một SĐT 200 bought cùng mã, SĐT kia 422; 1 đơn | `TestWebFlashBuy_RefOtherPhoneParallel` | như trên | `ok` |
| `ClearState`: xóa ngay một món (SoldOut không biết, Remaining không có), món/tenant khác giữ, món chưa có không lỗi, Valkey dừng → ErrUnavailable | `TestAdmissionGate_ClearState` | `go test -race -count=1 -run '^TestAdmissionGate_ClearState$' ./internal/platform/cache/` | `ok … cache 2.989s` |
| Đóng đợt → vòng gọi ClearState đúng món (không phải tự hết hạn), trạng thái không còn | `TestLoop_CloseClearsState` | `go test -race -count=1 -run '^(TestLoop_CloseClearsState\|TestLoop_SignalsRecompute)$' ./internal/wiring/flashstate/` | `ok … flashstate 2.579s`. Bỏ lời gọi clearGone → đỏ: `chờ quá lâu: vòng xóa trạng thái sau khi đóng đợt` |
| Cổng tĩnh + test gói đổi và phụ thuộc (race: httpapi, httpapi/web, order…, cache, flashstate) | ci-task | `scripts/agent/ci-task.sh` | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/46066552678`, `CI-TASK XANH (từ xa trên github …)`. Lượt trước đỏ ở lint (gocognit 2 test, nolint thừa) — đã sửa: tách test song song, bỏ nolint |

## E2E viết sẵn, chưa chạy

(không có)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không có mục kế hoạch riêng; phạm vi theo ghi chú điều phối M5 (Task 520). Thêm phương thức `ClearState` vào
  interface `cache.AdmissionGate` của kế hoạch M5 Task 187 (QĐ-520-3).

## Việc phát hiện thêm

- Kế hoạch M5 Task 187 (mục Interfaces của `AdmissionGate`) nên thêm dòng `ClearState` khi điều phối viên gộp.

## Reviewer

Kết luận: ĐẠT (lượt 1, trên phần đã stage sau khi gộp main ed324f7f).

- Phát hiện phải sửa: không có.
- Đã kiểm: cả 3 chỗ trả đơn theo mã ngoài (bước 2, sau Claim không được, `sameRefOrder`) qua `flashOrderByRef` có so
  `CustomerID`; `customer_id` NOT NULL nên so `uuid.UUID` đúng; lượt bị từ chối lùi cả transaction (kể cả khách vừa
  tạo); `ClearState` gọi ngoài RetryTx; sổ `written` chỉ dùng trong goroutine `run`; mã sinh khớp nguồn; không test
  bị nới; 6 test bằng chứng kiểm đúng tiêu chí trên PostgreSQL và Valkey thật.
- Ghi chú không bắt buộc: kế hoạch M5 Task 187 chưa ghi `ClearState` (đã nêu ở "Khác kế hoạch", cập nhật khi gộp);
  `ClearState` lỗi thì lượt đó bỏ các SetState còn lại — chấp nhận (Valkey lỗi thì SetState cũng lỗi, lượt sau làm lại).
