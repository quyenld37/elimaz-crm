# Task 96 — M3-05: Hàm tồn còn thiếu cho vận chuyển: giao một phần, đảo xuất, ghi thất lạc

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 96 = M3-05). Loại việc (quy trình mục 3): B (vùng rủi ro cao `internal/modules/inventory`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu nào có sẵn. Thêm vào `package inventory`: `PartialLine`, `LossLine`, `ErrReasonRequired`, phương thức `Writer.ConsumePartial`, `Writer.ReverseConsumption`, `Writer.RecordLoss`. 0 chỗ tham chiếu có sẵn (tên mới). Hàm M1, M2 (`Reserve`, `Release`, `StartTransit`, `Consume`, `*ByItems`, `ReleaseInTransitByItems`, `ReceiveCustomerReturn`, `MoveToDamaged`, `applyDelta`, `transition`, `lockInState`, `applyMoves`) không đổi chữ ký, không đổi thân (A11, Q-X1).
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/inventory/invtest`, `internal/modules/order` — chỉ cần build lại (không ký hiệu nào họ dùng bị đổi).
- Operation OpenAPI bị chạm, oasdiff: không có (chưa có đường HTTP; Task 97, 99, 114, 117, 118 gọi qua `order`).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: thêm `queries/inventory/transit_m3.sql` (`InTransitReservationsByItems`, `ConsumeReservationPart`, `ReservationsWithStep`), `queries/inventory/loss.sql` (`LedgerHasDocStep`, `LockBalancesByKeys`; dùng cho cả RecordLoss và ReverseConsumption). Thêm ký hiệu nội bộ (không xuất): `docAdjust`, `stepConsumePartial`, `stepReverseConsume`, `stepLoss`, bảng `consumePartial`, `checkNotDone`, `checkAdjust`, `adjustSum`/`addSum`/`sortSums`. Bảng chạm: `inventory.stock_balances` (qua `ApplyDelta` có sẵn), `inventory.stock_ledger` (chèn, đọc), `inventory.stock_reservations` (cột `quantity`, `state` — quyền UPDATE đã có ở 0115 và 0210). Không migration mới.
- Test của gói đổi: `internal/modules/inventory/deadlock_test.go` (dời transaction mở thẳng và vòng chờ deadlock vào `rawTx`, `noNewDeadlocks` dùng chung, xem Tái dùng).
- Test sẽ chạy: cả bộ `go test -race ./internal/modules/inventory/...` (vùng rủi ro cao: gồm test M1 Task 28, M2 Task 22, Task 406); build `./...`; `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có gì cho tồn (đường tồn chỉ ở `inventory`); trong gói: `applyDelta` (một câu UPDATE có điều kiện + một dòng sổ), `lockInState` (khóa số dư rồi giữ hàng theo ADR-46 nhóm 11, kiểm trạng thái), `applyMoves` + bảng `change`/`move` (áp các ngăn của một chuyển trạng thái, dựng `StockShortError`), `consume` (bảng chuyển `in_transit → consumed`), `SetReservationState`, `ReservationIDsByItems`, `Writer.changed` (stock.changed + báo hiệu), `reasonRe`, `compareKey`, `sumReturnLines`-kiểu gộp theo (biến thể, kho).
- Dùng lại / viết mới vì:
  - `ConsumePartial`: phần đủ số dùng nguyên `consume` + `SetReservationState` + `applyMoves` (đúng bước `consume` như `ConsumeByItems`); phần một phần dùng `applyMoves` với bảng `consumePartial` mới (cùng ngăn, bước `consume_partial`) và một câu mới `ConsumeReservationPart` (trừ `quantity`, giữ `in_transit`). `ConsumeByItems` không dùng được: chỉ chuyển cả dòng.
  - `ReverseConsumption`: dùng `ReservationIDsByItems`, `lockInState`, `SetReservationState`, `applyDelta`. Không dùng `applyMoves` vì sổ phải mang `doc_type adjust, doc_id = ref` (không phải mã giữ hàng) và gộp theo (biến thể, kho) để không trùng khóa unique của sổ khi hai dòng đơn cùng biến thể.
  - Trả lời jscpd (lần chạy ci-task đầu báo 4 đoạn trùng mới): transaction mở thẳng không RetryTx và vòng chờ `pg_stat_database.deadlocks` trùng `TestReserve_NoDeadlockOnCrossOrder` → gộp thành `rawTx`, `noNewDeadlocks` trong `deadlock_test.go` (cùng mục đích, cùng lý do đổi), test cũ gọi lại (chỉ đổi cấu trúc test, không đổi điều test kiểm); chuỗi "giao một phần → giao nốt → đảo" lặp trong hai test → `partialThenReverse`.
  - `ReverseConsumption` và `RecordLoss` dùng chung `checkAdjust` (ref, lý do, tác nhân) và `adjustSum`/`addSum`/`sortSums` (cùng chứng từ adjust của vận chuyển, cùng lý do đổi).
  - `RecordLoss`: dùng `applyDelta` (cond `available`) + `Writer.changed`; gộp dòng theo (biến thể, kho) như `sumReturnLines` của M2 nhưng viết riêng (`addSum`/`sortSums`, dùng chung với `ReverseConsumption`): kiểu dòng khác (không có `Damaged`), cùng lý do thay đổi thì mới gộp — ở đây hai việc thay đổi vì lý do khác (trả hàng vs thất lạc). `MoveToDamaged`, `ReceiveCustomerReturn`, phiếu `adjust` của M1-29 không dùng được: phiếu cần nháp/ghi sổ và quyền màn hình; `MoveToDamaged` cộng ngăn hỏng, không trừ `on_hand`.

## Soát thiết kế

5.1 Chủ dữ liệu: số dư, sổ, giữ hàng thuộc `inventory`; không bảng mới. 5.2 Không ghi bảng module khác. 5.3 Không đọc
module khác. 5.4 Nguyên tử: ba hàm chạy trong tx của bên gọi (`order` khóa đơn trước, ADR-46 nhóm 7 trước nhóm 11); lỗi
→ bên gọi lùi cả transaction. 5.5 `stock.changed` qua `events.Emit` trong tx (như mọi đường tồn). 5.6 Không gọi ra
ngoài. 5.7 Mọi câu chạy trong tx tenant của bên gọi (RLS). 5.8 Không HTTP; `ErrReasonRequired` chưa đổi sang HTTP vì chưa
operation nào trả nó (task sau thêm dòng ở `errors.go` khi có đường). 5.9 Không đường ghi HTTP mới. 5.10, 5.11 không áp
dụng (không Valkey, không thư viện). 5.12 Theo mẫu module `inventory`: phương thức `Writer`, sentinel trong `errors.go`,
test gói `_test` trên PostgreSQL thật, quy luật bằng rapid.

6.1 Đồng thời: mọi đổi số dư là `ApplyDelta` có điều kiện (READ COMMITTED đánh giá lại sau khi chờ khóa); giữ hàng khóa
`FOR UPDATE` rồi kiểm trạng thái; câu trừ một phần `WHERE state = 'in_transit' AND quantity > q`. 6.2 Gọi lặp:
`ReverseConsumption`, `RecordLoss` cùng `ref` lần hai → không đổi gì, trả nil (đọc sổ trước); `ConsumePartial` lặp → dòng
đã giao một phần / đã giao đủ → `ErrStateConflict` (kiểm sổ trước, không để lỗi 23505). 6.3 Thứ tự khóa ADR-46 nhóm 11:
`ConsumePartial`, `ReverseConsumption` dùng `lockInState` (số dư theo (variant_id, warehouse_id) tăng dần, rồi giữ hàng
theo id tăng dần) trước mọi ghi; `RecordLoss` chỉ chạm số dư, theo (variant_id, warehouse_id) tăng dần. Test 20
goroutine, transaction mở thẳng (không RetryTx) và `pg_stat_database.deadlocks` không tăng. 6.4, 6.5 như cũ (READ
COMMITTED, câu có điều kiện; bên gọi chạy trong `RetryTx`). 6.6–6.9 không áp dụng (không việc nền, không gọi ngoài).
6.10 không chạm tiền. 6.11 Quy luật: test rapid mới (`TestTransitM3Invariants`) xen ba hàm mới với hàm M1, M2, 1–4
goroutine → cột ≥ 0, sổ = số dư theo ngăn, giữ hàng `held`/`in_transit` = số dư, mỗi đơn vị trừ `on_hand` một lần trừ
khi có đảo. 6.12 Lỗi chỉ mang mã (uuid, số lượng). 6.13 Quyền: hàm nội bộ, bên gọi (`order`, `fulfillment`) kiểm quyền
ở API.

## Quyết định thay chủ shop

| # | Chọn | Vì sao | Phương án khác |
|---|---|---|---|
| Q-T96-1 | Ba hàm là phương thức `Writer` (không phải hàm trần như Interfaces ghi) | Đổi tồn phải phát `stock.changed` qua `Writer.Events/Queue`; Task 91 G-M1-4: "đường ghi tồn là phương thức `inventory.Writer`" | Hàm trần nhận thêm registry (lệch mọi đường tồn khác) |
| Q-T96-2 | `ReverseConsumption`, `RecordLoss`: đã có dòng sổ `adjust` cùng `ref` và cùng bước → trả nil, không đổi gì (kể cả khi lần hai gửi dòng khác) | Kế hoạch: "cùng `ref` lần hai → không đổi gì"; để khóa unique của sổ bắn 23505 thì transaction của bên gọi hỏng | Trả lỗi riêng `ErrAlreadyApplied` |
| Q-T96-3 | `ReverseConsumption` gộp theo (biến thể, kho): một dòng sổ mỗi ngăn | Khóa unique sổ `(doc_type, doc_id, variant, warehouse, bucket, step)`: hai dòng đơn cùng biến thể cùng `ref` sẽ trùng | Mỗi giữ hàng một `ref` con (bên gọi phải sinh) |
| Q-T96-4 | `reason`: rỗng → `ErrReasonRequired`; sai mẫu `^[a-z][a-z0-9_]{0,63}$` → `ErrInvalidInput`; không lưu trong `inventory` (sổ không có cột lý do; `order` mang lý do trong sự kiện `order.delivery_reversed`, Task 97) | Không đổi bảng sổ (migration) cho một trường chỉ để đọc | Thêm cột `reason` vào sổ |
| Q-T96-5 | `ConsumePartial`: mỗi dòng đơn đúng một giữ hàng `in_transit` (0 → `ErrStateConflict`); `Qty > quantity` → `ErrStateConflict`; dòng đơn lặp trong `lines` → `ErrInvalidInput`; giao một phần lần hai, hay giao đủ lần hai sau khi đảo → `ErrStateConflict` (đọc sổ trước) | Không để lỗi 23505 lọt ra; M1 giữ một giữ hàng cho mỗi dòng đơn | Cho bước sổ đánh số (`consume_partial_2`…) để giao một phần nhiều lần — chưa có ca nào cần (Q-15: một lần ghi tay) |
| Q-T96-6 | Đảo chỉ trả lại `quantity` hiện tại của giữ hàng (phần giao qua bước `consume_partial` vẫn tính đã giao) | Đảo là hãng sửa lần "giao xong" cuối; phần giao một phần đã ghi tay có quyền | Đảo cả phần một phần (cần nhớ số gốc) |
| Q-T96-7 | Không sửa `ConsumeByItems` của M1: gọi nó lần hai cho giữ hàng đã đảo → lỗi 23505 (sổ trùng bước `consume`), transaction lùi, không đổi gì; ghi ở "Việc phát hiện thêm" | A11: không viết lại hàm M1; `shipstate` (Task 98) không có cạnh `returning → delivered`, sau đảo chỉ đi đường hoàn | Đổi `applyDelta` để đổi 23505 thành `ErrStateConflict` |
| Q-T96-8 | `RecordLoss` thiếu dòng số dư hay có thể bán < Qty → `ErrStateConflict` (kế hoạch); dòng cùng (biến thể, kho) cộng gộp | Đúng Interfaces | `ErrInsufficient` |
| Q-T96-10 | Kiểm "cùng ref" sau khi khóa số dư liên quan (`LockBalancesOfReservations` / `LockBalancesByKeys` mới, theo (biến thể, kho) tăng dần) | Hai lần gọi song song cùng ref: lần sau chờ khóa rồi thấy dòng sổ của lần trước → nil, không lỗi 23505 | Kiểm trước khi khóa (đua được, lần sau hỏng transaction bằng 23505) |
| Q-T96-9 | Test thuộc tính chạy theo số lượt chung của gói (300, `TestMain`) | ≥ 200 lượt của kế hoạch; không tách cấu hình rapid theo test | Đặt riêng 200 |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TD09: `ConsumePartial([vB×1])` khi đi giao 2 → vB on_hand 9, in_transit 1; giữ hàng còn 1 in_transit; một dòng sổ `consume_partial` mỗi ngăn; `ConsumeByItems` phần còn lại → on_hand 8, in_transit 0, consumed; Qty = quantity → như ConsumeByItems | `TestConsumePartial`, `TestConsumePartial_Mixed` | `go test -race -count=1 ./internal/modules/inventory/`; `ci-task.sh` | `ok … inventory 69.271s` (cả gói); ci-task: `ok … inventory 136.656s` |
| `ConsumePartial([vB×3])` khi giữ 2 → `ErrStateConflict`, số dư không đổi; dòng held, dòng đơn lạ, trùng dòng đơn, Qty ≤ 0, tác nhân lạ, giao một phần lần hai, giao đủ lần hai sau đảo → lỗi, không đổi gì | `TestConsumePartial_Rejects`, `TestConsumePartial_SecondPartialRejected` | như trên | PASS |
| TD10: `ReverseConsumption(D1, R1, "carrier_corrected")` sau `ConsumeByItems` → vA on_hand 5, in_transit 1; cùng R1 → không đổi; thiếu lý do → `ErrReasonRequired`; sau đảo đi đường hoàn được; hai dòng cùng biến thể gộp sổ; đảo sau giao một phần chỉ trả phần giao nốt | `TestReverseConsumption`, `TestReverseConsumption_SameVariantTwoItems`, `TestReverseConsumption_AfterPartial` | như trên | PASS |
| TD04: `RecordLoss(vB×1, P1)` sau `ReleaseInTransitByItems` → vB on_hand 9, có thể bán −1, sổ bước `loss`; cùng P1 → không đổi; thiếu có thể bán, chưa có số dư → `ErrStateConflict` cả lệnh; dòng cùng biến thể gộp | `TestRecordLoss` | như trên | PASS |
| Song song: 20 goroutine (10 đơn chung vA, vB) `StartTransitByItems` + `ConsumePartial` / `ConsumeByItems`, transaction mở thẳng → không lỗi ngoài `ErrStateConflict`, `pg_stat_database.deadlocks` không tăng, số dư khớp sổ và giữ hàng | `TestTransitM3_NoDeadlock` | `go test -race -count=1 -run TestTransitM3_NoDeadlock ./internal/modules/inventory/` | `--- PASS: TestTransitM3_NoDeadlock (2.57s)` |
| Thuộc tính ≥ 200 lượt: chuỗi ngẫu nhiên M1, M2 + ba hàm mới + gọi lặp, 1–3 biến thể, 1–5 đơn dựng sẵn, 1–4 goroutine → cột ≥ 0, sổ = số dư, giữ hàng = số dư, mỗi đơn vị trừ on_hand một lần trừ khi đảo | `TestTransitM3Invariants` | `go test -race -count=1 -v -run TestTransitM3Invariants ./internal/modules/inventory/` | `[rapid] OK, passed 300 tests`; chạy 6 lần liền đều qua |
| Test bắt được lỗi (đột biến tay, đã hoàn nguyên): bỏ bước bớt `quantity` khi giao một phần; bỏ ngăn `in_transit` khi đảo; nhận Qty = 0 | `TestTransitM3Invariants`, `TestConsumePartial_Rejects` | như trên trên mã đột biến | cả ba FAIL (`giữ hàng (held 0, in_transit 3) khác số dư (0, 1)`) |
| Hàm M1, M2 không đổi; gói phụ thuộc vẫn qua | cả bộ `inventory` (M1 Task 28, M2 Task 22, Task 406), `order` | `go test -race -count=1 ./internal/modules/inventory/... ./internal/modules/order/...` | ci-task (lần 2, sau gộp main): `ok … inventory 136.656s`, `ok … order 188.559s`; mọi cổng tĩnh xanh, chỉ test-guard đỏ vì chưa ghi mục "Test bị xóa hoặc tắt" (đã ghi) |

## Test bị xóa hoặc tắt

- Không test nào bị xóa hay tắt. `internal/modules/inventory/deadlock_test.go`: test-guard đếm "số câu kiểm giảm từ 6 xuống 4" vì câu kiểm của `TestReserve_NoDeadlockOnCrossOrder` (lỗi mở transaction, đặt tenant, `pg_sleep`, vòng chờ `deadlocks`) dời vào hai hàm dùng chung `rawTx` (trả lỗi lên test gọi) và `noNewDeadlocks` (`e.t.Fatal` / `e.t.Fatalf`) trong cùng tệp, để trả lời jscpd (xem Tái dùng). Test vẫn kiểm đúng những điều cũ: lỗi bất kỳ của transaction → `t.Fatalf("lượt %d")`, số dư cuối, `pg_stat_database.deadlocks` không tăng trong 2 s.

## Khác kế hoạch

- Ba hàm là phương thức `Writer` (Q-T96-1).
- `queries/inventory/loss.sql` giữ hai câu dùng chung cho chứng từ adjust của vận chuyển (`LedgerHasDocStep`, `LockBalancesByKeys`); `ReservationsWithStep` nhận mảng bước (một câu cho cả giao đủ và giao một phần).
- Sổ giao một phần: hai dòng (ngăn `in_transit` và `on_hand`, như bước `consume` của M1), không phải "đúng một dòng" như Step 1 viết; test kiểm đúng một dòng mỗi ngăn.
- Test thuộc tính dựng sẵn 1–5 đơn (phần lớn đã `in_transit`) trước chuỗi thao tác: phân bố của rapid lệch về số nhỏ nên nếu không dựng sẵn, giao một phần gần như không bao giờ chạm giữ hàng `in_transit` (đã thử: đột biến bỏ bước bớt `quantity` sống). Sau dựng sẵn, đột biến đó và đột biến bỏ ngăn `in_transit` khi đảo đều bị bắt.
- Bước `RecordLoss` trong rapid có xen lần gọi lặp cùng ref (`opLossAgain`, `opReverseAgain`).

## Việc phát hiện thêm

- Cho bên gọi `RecordLoss` (Task 117/118): mọi dòng thất lạc của một `ref` (mã phiếu trả) phải đi trong **một** lần gọi; lần gọi sau cùng `ref` trả nil, không ghi gì (Q-T96-2).
- `ConsumePartial` vẫn nhận giao một phần trên giữ hàng đã bị đảo (đã có sổ `consume`) nếu chưa từng giao một phần; giao đủ sau đó bị chặn (`ErrStateConflict`). Hiện không đường nào đi tới (sau đảo chỉ đi đường hoàn).
- VC06, VC07 (không xuất hai lần) ở mức gói: `checkOnceOut` trong `TestTransitM3Invariants`, `TestTransitM3_NoDeadlock`.

- `ConsumeByItems`/`Consume` của M1 gọi cho giữ hàng đã đảo (`ReverseConsumption`) → lỗi PostgreSQL 23505 (`stock_ledger_step_key`, bước `consume` đã có) thay vì `ErrStateConflict`. Hiện không đường nào đi tới (không cạnh `returning → delivered`); nếu sau này có, cần đổi lỗi trùng bước sổ sang sentinel ở `applyDelta` (một chỗ) hoặc đánh số bước.

## Reviewer

Kết luận: ĐẠT (vòng 1, agent `reviewer`, trên phần đã stage sau khi gộp main). Phát hiện phải sửa: không có. Đã kiểm: đủ
ba hàm, hai kiểu, sentinel; thứ tự khóa ADR-46 nhóm 11 ở cả ba hàm; gọi lặp và song song cùng ref không để lọt 23505;
mã sinh khớp nguồn; không chạm hàm M1, M2; bảng bằng chứng trỏ đúng test có kiểm thật. Ghi chú không bắt buộc (đã ghi
vào "Việc phát hiện thêm"): giao một phần trên giữ hàng đã đảo; `RecordLoss` coi "đã làm" theo (ref, loss); rapid cho
Consume của M1 và đảo chạy lần lượt (lý do Q-T96-7); VC06/VC07 được `checkOnceOut` kiểm.
