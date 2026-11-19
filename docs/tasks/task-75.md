# Task 75 — M2-18: Móc nối cho M3 — vận đơn hiện hành, lấy hàng, giao, thanh toán

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task M2-18, số toàn cục 75). Loại việc (quy trình mục 3): A

Hợp đồng này là **đầu vào của kế hoạch M3** (báo cáo soát chéo R1, G-M2-3 của kế hoạch M3): `Writer.AttachShipmentBy`,
`Writer.DetachShipment`, `Writer.OnShipmentEvent` (kiểu `ShipmentStep` của M1), `Writer.ApplyPaymentTotals`. M3 gọi
đúng các hàm này (hoặc lớp bọc M1), không viết bản thứ hai (A11).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep cả test):
  - Mới: `Writer.AttachShipmentBy`, `Writer.DetachShipment`, `Writer.OnShipmentEvent`, `Writer.ApplyPaymentTotals`,
    hằng `TopicOrderUpdated` (`order.updated`).
  - `order.AttachShipment` (M1, hàm gói) giữ chữ ký, thành lớp bọc dùng chung lõi với `AttachShipmentBy`: chỗ gọi
    `fulfillment/fulfillment.go:168` và `order/shipment_progress_test.go` — không phải sửa.
  - `Writer.ApplyShipmentProgress` (M1) giữ chữ ký, thành lớp bọc gọi `OnShipmentEvent`: chỗ gọi ngoài gói
    `fulfillment/refresh_status.go:151` và test trong gói — không phải sửa.
  - `order.DetachShipment` (hàm gói của Task 408) **bị xóa**, thay bằng `Writer.DetachShipment`: 2 chỗ gọi —
    `fulfillment/create_shipment.go:317` (`CreateCommand.OnTerminal`, đã có `c.o.Events/Queue/Realtime`) và helper
    `detach` trong `order/shipment_progress_test.go` — sửa cả hai trong task này (Quyết định thay chủ shop 2).
  - Hàm không xuất: `markDelivered` (transition.go) xóa — chỉ `ApplyShipmentProgress` dùng; `OnShipmentEvent` gọi
    thẳng `applyTransition` với người làm và `now` của bên gọi (bỏ `time.Now()` trong module).
  - `store.LockOrderRow` thêm cột `grand_total`, `cod_amount`, `prepaid_total`, `payment_status` (đọc trong cùng câu
    khóa); `store.SetCurrentShipment`, `store.ClearCurrentShipment` xóa (thay bằng câu ở `shiphooks.sql` trả `version`
    cho `Seq`).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest`.
- Operation OpenAPI bị chạm, oasdiff: không (chỉ hàm Go, kế hoạch: "không có API").
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không migration (cột `prepaid_total`, `payment_status`, `cod_amount`,
  `current_shipment_id` đã có, đã GRANT UPDATE ở 0204; ràng buộc `orders_cod_prepaid_check` đã VALIDATE). Mới
  `queries/ord/shiphooks.sql`: `AttachCurrentShipment`, `DetachCurrentShipment`, `SetPaymentTotals` (UPDATE `ord.orders`
  theo `version` đọc dưới khóa, `RETURNING version`; không gán `status` — hợp `order_status_sql_test`). Sửa
  `queries/ord/orders.sql`: `LockOrder` thêm 4 cột; xóa `SetCurrentShipment`, `ClearCurrentShipment`. Bảng chạm:
  `ord.orders`, `ord.order_status_history` (qua `applyTransition`), `inventory.*` (qua `inventory.Writer`),
  `platform.domain_events`, `platform.jobs` (Emit). Tài liệu: `docs/views.md` thêm dòng sự kiện `order.updated`.
- Test sẽ chạy: `go test -race ./internal/modules/order/...`, `./internal/modules/fulfillment/...`,
  `./internal/httpapi/...`, `./tests/arch/...`; rồi cổng `ci-task.sh` một lần trước review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (chỉ gói platform;
  không có gì về đơn/vận đơn); trong module: `lockOrder`, `applyTransition`, `emit`, `signal`, `TxNow` (create.sql),
  `inventory.ReservationStatesByItems`, `Writer.StartTransitByItems`, `ConsumeByItems`, `orderview.Label` (test).
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên; đổi trạng thái chỉ qua `applyTransition` (luật Task 73). Viết
  mới chỉ 3 câu UPDATE trả `version` (câu cũ `SetCurrentShipment`/`ClearCurrentShipment` không trả `version` nên xóa,
  không giữ hai câu cùng việc) và hàm `emitUpdated` nhỏ (dựng `Order` tối thiểu từ dòng khóa rồi `emit` + `signal`).
  Lõi gắn vận đơn (`attachShipment`) dùng chung giữa `AttachShipmentBy` và lớp bọc M1 — một đường, không bản thứ hai.

## Soát thiết kế

- 5.1 Chủ dữ liệu: cột đơn (`current_shipment_id`, tổng thanh toán) thuộc `order` (04 mục 3); `fulfillment`/`cash`
  (M3) gọi public API của `order`, không ghi `ord.*`.
- 5.2 Không ghi bảng module khác: tồn đổi qua `inventory.Writer`; sự kiện qua `events.Registry.Emit`.
- 5.3 Đọc module khác qua view: không đọc bảng module khác (`ReservationStatesByItems` là API của inventory).
- 5.4 Nguyên tử: mọi hàm chạy trong tx của bên gọi (M3: cùng tx với vận đơn/phiếu thu).
- 5.5 Việc sau commit: sự kiện `order.updated`/`order.delivered` qua outbox Emit trong tx.
- 5.6 Gọi ra ngoài: không áp dụng (không gọi hãng; đổi COD qua hãng là M3).
- 5.7 Ngữ cảnh shop: mọi câu chạy trong tx tenant của bên gọi; câu có `seller_id` (ADR-29); `TenantID` của sự kiện lấy
  từ dòng đơn đã khóa.
- 5.8 Không HTTP: chỉ hàm Go.
- 5.9 Khóa chống trùng/quyền: không áp dụng (không có route); gọi lại an toàn: gắn cùng vận đơn, gỡ lần hai, bước đã
  áp, cùng tổng thanh toán → không đổi gì, không tăng version.
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12 Theo mẫu: phương thức `Writer` (Q-T57-1), sentinel đã có trong `errors.go` (`ErrShipmentAlreadyAttached`,
  `ErrInvalidTransition`, `ErrLockedByShipment`, `ErrInvalidInput`, `ErrNotFound`, `ErrStatusConflict`), test gói
  `_test` trên PostgreSQL thật.
- 6.1 Đồng thời: mọi hàm khóa đơn `FOR UPDATE` trước (ADR-46 nhóm 7) rồi mới tới giữ hàng/số dư (nhóm 11); UPDATE kèm
  `version` đọc dưới khóa. Test: 100 cặp `delivered` ∥ `ApplyPaymentTotals` cùng đơn.
- 6.2 Gửi lại: xem 5.9 (sự kiện vận đơn tới hai lần → `applied=false`; AT47 một phiếu xuất).
- 6.3 Thứ tự khóa: đơn (7) → số dư/giữ hàng (11) — đúng ADR-46 "bắt đầu từ dòng ở nhóm sau: khóa đơn trước".
- 6.4, 6.5: bên gọi (M3) chạy trong `db.RetryTx`; hàm không giữ biến cộng dồn ngoài thân.
- 6.6–6.9: không áp dụng (không việc nền, không gọi ngoài).
- 6.10 Tiền: `int64` đồng; `cod = max(grand − paid, 0)` tính trong Go, CHECK `orders_cod_prepaid_check` của database kiểm
  lại; `paidTotal < 0` hay > 9007199254740991 → `ErrInvalidInput` (kiểm trước khi ghi).
- 6.11 Quy luật: test rapid có sẵn của M1 (chỉ vận đơn hiện hành đổi đơn/tồn) chạy qua lớp bọc → phủ `OnShipmentEvent`;
  `ApplyPaymentTotals` kiểm vét cạn mọi cặp chuyển giữa 6 mức trả (cod + prepaid khớp công thức, trạng thái đúng
  ngưỡng, version chỉ tăng khi đổi).
- 6.12 Dữ liệu cá nhân: payload sự kiện chỉ mã và số (A16); không log.
- 6.13 Quyền: không áp dụng (không route; M3 kiểm quyền ở API của nó).

## Quyết định thay chủ shop

1. **Lớp bọc `AttachShipment` (M1) không phát `order.updated`.** Hàm gói của M1 không có `Writer` (không có
   `Events`, `Queue`) nên không phát được sự kiện đúng (kể cả việc cho consumer). Chọn: lõi `attachShipment` (khóa,
   kiểm, ghi, `version + 1`) dùng chung; `Writer.AttachShipmentBy` = lõi + `order.updated` + báo hiệu; lớp bọc = lõi
   với `now = SELECT now()`. Hành vi M1 giữ nguyên, `fulfillment.CreateForOrder` không sửa (đúng kế hoạch). Phương án
   khác: (a) lớp bọc dựng `Writer` với `Registry` rỗng — ghi dòng `domain_events` mà không có việc cho consumer (sai
   lặng lẽ khi M3 thêm consumer), bỏ; (b) sửa `CreateForOrder` nhận `Writer` — trái "không phải sửa". M3 gọi
   `AttachShipmentBy` (kế hoạch M3 dòng 630) nên sự kiện có từ M3. Đảo ngược: xóa lớp bọc ở pha thu hẹp (Q30).
2. **`order.DetachShipment` (hàm gói của Task 408) thay bằng `Writer.DetachShipment`** (chữ ký kế hoạch, trả
   `applied`, phát `order.updated`) và sửa chỗ gọi duy nhất `fulfillment.CreateCommand.OnTerminal` (đã có sẵn
   `Events/Queue/Realtime`) — không để hai hàm cùng tên làm cùng việc (Q30 "không có hai hàm cùng tên", CLAUDE.md "chuyển
   sang cách mới thì xóa cách cũ"). Task 408 là task bù sau M1, không có ràng buộc giữ chữ ký của Q30. Phương án khác:
   giữ hàm gói làm lớp bọc — hai tên trùng, bỏ.
3. **Gọi lại không đổi gì thì không ghi.** `ApplyPaymentTotals` cho ra đúng `prepaid_total`, `payment_status`,
   `cod_amount` hiện tại → không tăng `version`, không phát sự kiện (consumer M3 chạy lại an toàn). Gắn lại cùng vận
   đơn: nil, không đổi (giữ M1). Gỡ vận đơn không phải hiện hành: `applied=false`.
4. **`ErrLockedByShipment` theo đúng chữ kế hoạch**: đơn có `current_shipment_id` (kể cả đã `delivered`) mà
   `cod_amount` sẽ đổi → lỗi; đổi `prepaid_total` mà COD vẫn như cũ (đã trả đủ, ghi thêm) → được. Kế hoạch M3 Q-17
   cùng ý. Trạng thái `refunded` không đặt ở đây (M3).
5. **Tenant của sự kiện lấy từ dòng đơn đã khóa** (lớp bọc M1 truyền `Actor` không có `TenantID`, như `markDelivered`
   cũ); RLS bảo đảm trùng tenant của tx.
6. **`AttachShipmentBy` trên đơn không `confirmed` hay còn `pending_reason` → `ErrInvalidTransition`** (kế hoạch); M1
   trả `ErrStatusConflict` — lớp bọc giờ trả `ErrInvalidTransition` (chỗ gọi duy nhất `CreateForOrder` đã tự kiểm
   `confirmed` trước nên không chạm tới). `ErrStatusConflict` vẫn dùng cho bất biến hỏng ở `OnShipmentEvent`.
7. **Lượt 2 (gộp main): trùng tên `orderEnv.dh01`.** Task 74 có `dh01(v1, v2, code) order.CreateInput` (dựng đầu
   vào DH01 kèm mã); bản của task này tạo đơn confirmed và biến thể — khác nghĩa, nên đổi tên bản của mình thành
   `confirmedDH01()` và cho nó dựng đầu vào bằng `e.dh01(v1, v2, "")` của Task 74 (một nguồn cho dữ liệu DH01).
   Phương án khác: xóa hẳn bản của mình và lặp 3 dòng tạo biến thể ở 11 chỗ gọi — dài hơn, không thêm gì.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước khi có mã | cả hai tệp test mới | `go vet ./internal/modules/order/` | `e.writer().ApplyPaymentTotals undefined`, `AttachShipmentBy undefined`, `TopicOrderUpdated undefined` |
| `AttachShipmentBy(S1)` → current S1, version +1, `order.updated` seq = version; S2 → `ErrShipmentAlreadyAttached`; gắn lại S1 nil; đơn lạ, người bán khác → `ErrNotFound` | `TestAttachShipmentBy` | `go test -race -count=1 -run 'TestAttachShipmentBy\|TestOnShipmentEvent\|TestWriterDetach\|TestShipmentWrappers\|TestApplyPaymentTotals\|TestDetachShipment\|TestAttachShipment$\|TestApplyShipmentProgress_Flow' ./internal/modules/order/` (lệnh A) | `ok elimaz.com.vn/crm/internal/modules/order 98.677s` |
| Đơn `new` (và `draft`) → `ErrInvalidTransition`, không đổi | `TestAttachShipmentBy_NotConfirmed` | lệnh A (`-run TestAttachShipmentBy`) | ok |
| `picked_up(S1)` → V1 `in_transit 2`, `held 0`, `Available` không đổi; `delivered(S1)` → `delivered`, nhãn `delivered`, giữ hàng `consumed`, `on_hand` V1 −2, `order.delivered` seq = version, lịch sử carrier với `now` của bên gọi; `delivered` lần hai → `applied=false`, `on_hand` không giảm | `TestOnShipmentEvent_PickedUpThenDelivered` | lệnh A | ok |
| B1: gắn S1, gỡ S1, gắn S2; `delivered(S1)` → `applied=false`, đơn và tồn không đổi; `delivered(S2)` → applied | `TestOnShipmentEvent_StaleShipment` | lệnh A | ok |
| `delivered` khi chưa `picked_up` → held → in_transit → consumed trong một tx | `TestOnShipmentEvent_DeliveredWithoutPickup` | lệnh A | ok |
| `DetachShipment`: chỉ đúng vận đơn hiện hành, đơn giữ `confirmed`, giữ hàng không đổi, `order.updated`; lần hai/vận đơn khác → `applied=false`; người bán khác, đơn lạ → `ErrNotFound` | `TestWriterDetachShipment`, `TestDetachShipment` (M1-r, helper đổi sang hàm mới) | lệnh A | ok |
| `ApplyPaymentTotals`: 690.000 trả 200.000 → cod 490.000 `partial`; 690.000 → cod 0 `paid`; 700.000 → cod 0 `paid`; 0 → `unpaid`; −1 và 2^53 → `ErrInvalidInput`; cùng tổng → không đổi version; người bán khác → `ErrNotFound`; mỗi lần đổi có `order.updated` seq = version | `TestApplyPaymentTotals` | lệnh A | ok (riêng: `--- PASS: TestApplyPaymentTotals (3.86s)`) |
| Có vận đơn hiện hành, trả 200.000 → `ErrLockedByShipment`, không đổi; COD giữ 0 → được | `TestApplyPaymentTotals_LockedByShipment` | lệnh A | ok |
| Quy luật cod/trạng thái/version qua mọi cặp chuyển 6 mức trả | `TestApplyPaymentTotals_AllTransitions` | lệnh A | ok (`9.77s`) |
| `delivered` ∥ `ApplyPaymentTotals` cùng đơn ×100: không deadlock, version +2, kết quả nhất quán, tồn trừ đúng | `TestApplyPaymentTotals_ConcurrentWithDelivered` | lệnh A | ok (`24.72s`) |
| Lớp bọc M1 `AttachShipment` + `ApplyShipmentProgress(picked_up, delivered)` cùng kết quả như hàm mới; lịch sử carrier | `TestShipmentWrappers_SameAsNew` | lệnh A | ok |
| Test M1 của `ApplyShipmentProgress` vẫn đạt (qua lớp bọc) | `TestApplyShipmentProgress_Flow`, `_DeliveredWithoutPickup`, `_BrokenReservationsIsError`, `_UnknownStep`, `_OnlyCurrentShipment` (rapid), `_ConcurrentDelivered`, `TestAttachShipment`, `TestLock` | `go test -race -count=1 -v ./internal/modules/order/` và `-run 'TestApplyShipmentProgress_ConcurrentDelivered\|TestLock$\|TestDetachShipment\|…'` | mọi test liệt kê `--- PASS` (`_OnlyCurrentShipment` 279 s; lần chạy riêng: `ok … order 29.874s`); cả gói một lần chạy quá 10 phút mặc định lúc máy tải nặng — xem Việc phát hiện thêm |
| fulfillment: hủy vận đơn creating gỡ khỏi đơn qua `Writer.DetachShipment` | `internal/modules/fulfillment` (terminal_test) | `scripts/agent/ci-task.sh` | `ok elimaz.com.vn/crm/internal/modules/fulfillment 48.092s` |
| Luật kiến trúc (status chỉ ở transition.sql, lỗi được ánh xạ) | `tests/arch` | `ci-task.sh` | `ok elimaz.com.vn/crm/tests/arch 8.415s` |
| Cổng tĩnh: lint, jscpd | — | `golangci-lint run ./internal/modules/...`; `jscpd --config .jscpd.json --similarity --baseline jscpd-baseline.json --fail-on-new-clones=0 .` | `0 issues.`; `Found 0 clones.` (lần ci-task đầu báo 1 clone trong shiphooks.go → gom `lockForShipment`, `wired`) |
| Lượt 2: gộp main, trùng `dh01` hết (vet) và test vận đơn, thanh toán, DH01 vẫn đạt | `TestAttachShipmentBy*`, `TestOnShipmentEvent*`, `TestWriterDetachShipment`, `TestApplyPaymentTotals*`, `TestShipmentWrappers_SameAsNew`, `TestApplyShipmentProgress*`, `TestCreateCoupon_DH01` | `go vet ./internal/modules/order/`; `go test -race -count=1 -timeout 580s -run 'Shipment\|PaymentTotals\|Attach\|Detach\|OnShipment\|DH01' ./internal/modules/order/` | `VET_OK`; `ok elimaz.com.vn/crm/internal/modules/order 13.229s` |
| Lượt 2: cổng ci-task trên bản gộp | gói đổi, phụ thuộc, vùng cao; `tests/arch`, `tests/e2e` | `scripts/agent/ci-task.sh` (nền, một lần) | build, vet, lint, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard qua; test qua (`ok …/tests/arch 13.171s`, `ok …/tests/e2e 58.464s`, bước test 140 giây). `gen-check` ĐỎ giả: `git diff --exit-code -- internal console/src/api` chỉ chứa hai tệp test chưa commit của task (`payments_test.go`, `shiphooks_test.go`); `git diff --name-only` trên `internal/httpapi/gen`, `routes_gen.go`, `console/src/api`, `**/internal/store/**` sau `make gen` rỗng → mã sinh khớp nguồn. Lint của hook Stop báo `typecheck` gói `gen` một lần vì ci-task đang `ogen -clean` cùng lúc; chạy lại `golangci-lint run ./internal/modules/order/... ./cmd/...` → `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Lớp bọc `AttachShipment` dùng chung lõi `attachShipment` thay vì gọi `AttachShipmentBy`, và không phát
  `order.updated` (Quyết định 1). Test "lớp bọc cho cùng kết quả" so trạng thái, version, tồn, lịch sử — không so sự kiện.
- "Lịch sử ghi người làm system/carrier": `ord.order_status_history` chỉ ghi đổi trạng thái, nên chỉ bước `delivered`
  có dòng lịch sử (người làm `carrier` — lớp bọc, hay người làm do M3 truyền — hàm mới). Gắn/gỡ vận đơn, tổng thanh toán
  không đổi trạng thái nên không có dòng lịch sử; dấu vết là sự kiện `order.updated` (seq = version).
- `order.DetachShipment` (hàm gói, Task 408) xóa, chỗ gọi `fulfillment.CreateCommand.OnTerminal` sửa (Quyết định 2) —
  ngoài danh sách tệp của kế hoạch. Helper `detach` của `TestDetachShipment` (M1-r) đổi sang gọi `Writer.DetachShipment`;
  mọi khẳng định của test giữ nguyên.
- Test song song: 100 đơn (đơn nhập tay 1 dòng, trả đủ trước khi gửi hãng rồi trả thêm để COD giữ 0 — nếu không
  `ApplyPaymentTotals` đúng ra phải trả `ErrLockedByShipment`), chạy 10 luồng × 10 đơn để gói order không vượt 10 phút
  mặc định của `go test` trên máy dùng chung.
- Quy luật thanh toán kiểm bằng vét cạn mọi cặp chuyển giữa 6 mức trả (36 cặp, `TestApplyPaymentTotals_AllTransitions`)
  thay cho rapid (bản rapid 200 lượt tốn ~100 s; vét cạn phủ hết miền hữu hạn). `order` không thuộc vùng rủi ro cao; quy
  luật vận đơn vẫn có rapid của M1 (`TestApplyShipmentProgress_OnlyCurrentShipment`) chạy qua lớp bọc → `OnShipmentEvent`.

## Việc phát hiện thêm

- Trên bộ PostgreSQL dùng chung lúc nhiều phiên chạy, gói `internal/modules/order` (kể cả test cũ:
  `TestCreateManual_NoOversell` 193 s, `TestCreate_ConcurrentReservations` 112 s) và `internal/modules/catalog` vượt
  10 phút mặc định của `go test`; `ci-task.sh` không đặt `-timeout`. Lần ci-task của task này: catalog
  `TestApply_ThreePriceChanges`, `TestApply_200Models` đỏ (chạy riêng vẫn đỏ: "applying [], mong applied" — đợi worker
  theo giờ thật) và `httpapi` `TestOrderAPI_CreateRetryLater` đỏ một lần (chạy riêng: ok 7 s). Catalog không phụ thuộc
  order/fulfillment (`go list -test -deps ./internal/modules/catalog/`), không do task này. Đề xuất điều phối viên: đặt
  `-timeout` cho bước test của ci-task, xem lại test catalog phụ thuộc giờ thật (Task 400).
- Cho M3 (Task 121, `cash`): khi thêm hàm đặt `refunded`, lưu ý `ApplyPaymentTotals` tính lại `payment_status` từ tổng
  nên gọi sau đó sẽ ghi đè `refunded`; `ApplyPaymentTotals` không xét trạng thái đơn (đơn `cancelled` vẫn ghi được) —
  M3 tự quyết có chặn hay không.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên `git diff --cached` 15 tệp). Phát hiện phải sửa: không có. Quyết định thay
chủ shop 1–6: reviewer đánh giá đúng kế hoạch, an toàn (QĐ1: khoảng trống seq đã có từ M1, M3 chỉ đòi seq tăng dần).
Ghi chú không bắt buộc (chuyển cho M3, ghi ở Việc phát hiện thêm): `ApplyPaymentTotals` tính lại `payment_status` mỗi
lần nên sẽ ghi đè `refunded`; không xét trạng thái đơn (đơn `cancelled` vẫn ghi được); lớp bọc `AttachShipment` kiểm
thêm `pending_reason = ''` (vô hại).

Lượt 2 (gộp main, `git diff --cached` 3 tệp): ĐẠT. Phát hiện phải sửa: không có. Reviewer tự chạy lại `go vet
./internal/modules/order/` (qua), xác nhận `confirmedDH01` dựng đầu vào giống hệt bản cũ (đơn không mã, 690.000), đủ
12 chỗ gọi đã đổi, không assertion nào bị bỏ hay nới; giải thích gen-check đỏ giả hợp lý (phần stage không chạm mã sinh).
