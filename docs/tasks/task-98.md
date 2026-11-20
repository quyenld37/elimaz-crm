# Task 98 — M3-07: Máy trạng thái vận đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 98, M3-07). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới: gói `internal/modules/fulfillment/internal/shipstate` (`Status` + 9 hằng, `Target`, `Source` + 3 hằng,
    `Effect` + 9 hằng, `Input`, `Decision`, `Decide`, `Display`). Gói `internal` nên chỉ `fulfillment/...` nhập được.
  - Commit 1 (cấu trúc, không đổi hành vi): `fulfillment.Canonical` và 13 hằng `Canon*` thành bí danh (type alias,
    `const X = shipstate.X`) của `shipstate.Canonical` để gỡ vòng nhập (shipstate cần `Canonical`, `fulfillment.Advance`
    gọi `shipstate.Decide`). Chỗ dùng `fulfillment.Canon*`/`Canonical` không phải sửa (cùng một kiểu): adapter
    `internal/adapters/carrier/{fake,carriertest}`, `fulfillment/internal/carriers/fake`, `fulfillment/{adapter,types,
    unsupported}.go`, `credentials_test.go`.
  - Commit 3 (cấu trúc, không đổi hành vi): `fulfillment.Status` thêm 4 hằng `StatusDeliveryFailed`, `StatusReturning`,
    `StatusReturned`, `StatusCancelRequested`. `Advance(cur, next Status) (Status, bool)` giữ chữ ký; 2 chỗ gọi:
    `create_shipment.go:257`, `refresh_status.go:134` (hành vi giữ nguyên, test M1 `TestAdvance`,
    `TestAdvance_Property`, `refresh_status_test`, `create_shipment_test` xanh).
- Gói phụ thuộc ngược (go list): `fulfillment` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/cash`,
  `internal/adapters/carrier/{best,spx,vtp,fake,manual,carriertest}`, `internal/testkit/shiptest`,
  `tools/fakecarrier/server`. `shipstate` mới chỉ được `fulfillment` nhập (từ commit 1).
- Operation OpenAPI bị chạm, oasdiff: không (enum `Shipment.status` 9 giá trị đã có từ Task 95).
- Chỗ dùng trên màn hình: không (nhãn `Display` để task sau gọi; màn hình chưa đổi).
- Câu sqlc, bảng, pha migration: không (thuần Go, không database).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/...` (gồm `shipstate` mới: bảng ca, rapid 1.000 lượt,
  fuzz `FuzzDecide` 60 giây), gói phụ thuộc ngược qua cổng kiểm `ci-task.sh` (fulfillment/status.go là vùng rủi ro
  cao).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có máy trạng thái
  chung; tìm `Decide`, `transition`, `applyTransition` → có `order/transition.go` (máy trạng thái đơn, của module
  order) và `fulfillment.Advance`/`rank` (M1, thứ hạng chỉ tiến).
- Dùng lại / viết mới vì: luật vận đơn thuộc module fulfillment (CLAUDE.md: logic nghiệp vụ ở lại trong module), không
  gộp với `order/transition.go` (khác mục đích, khác lý do thay đổi). `Advance` của M1 không bị viết song song: thân
  gọi `shipstate.Decide` để chỉ còn một luật (kế hoạch). `Canonical` của M1 được dời vào `shipstate` (bí danh ở
  `fulfillment`), không định nghĩa kiểu thứ hai. Thư viện: `rapid` và fuzz chuẩn của Go (đã có trong ADR-44).

## Soát thiết kế

- 5.1 chủ dữ liệu: không áp dụng: không có dữ liệu mới; luật nằm trong `fulfillment/internal/shipstate` (module
  fulfillment sở hữu vận đơn).
- 5.2, 5.3: không ghi, không đọc bảng nào (thuần Go). `shipstate` không nhập module khác.
- 5.4, 5.5, 5.6: không áp dụng: không transaction, sự kiện hay lệnh ra ngoài; `Decide` chỉ trả quyết định, bên gọi
  (Task 99 `ApplyTracking`) chạy tác động trong transaction.
- 5.7: không áp dụng: không truy vấn.
- 5.8: không nhập `net/http`, ogen.
- 5.9, 5.10, 5.11: không áp dụng: không đường ghi, không Valkey, không thư viện mới.
- 5.12: theo module mẫu: gói `internal` của module, test gói `_test`, quy luật bằng rapid.
- 6.1–6.9: không áp dụng: hàm thuần, không trạng thái dùng chung, không khóa, không việc nền. Chống lặp và sai thứ tự
  (VC08, VC09) là luật của `Decide` (`duplicate_state`, `older`), có test rapid xáo thứ tự + nhân bản sự kiện.
- 6.10: không áp dụng: không tiền.
- 6.11: quy luật tồn/đơn gián tiếp qua tác động: `EffPickedUp` đúng 1 lần, `EffDelivered` đúng 1 lần, không bao giờ
  tới `returned` — test thuộc tính rapid 1.000 lượt.
- 6.12: không dữ liệu cá nhân (chỉ trạng thái, thời điểm).
- 6.13: không áp dụng: không quyền. Nguồn quyết định không bao giờ là webhook (`Source` không có giá trị webhook);
  cạnh sửa lại `delivered → returning` chỉ nhận `SrcCarrierAPI`/`SrcStaff` (TD10).

## Quyết định thay chủ shop

1. **Gỡ vòng nhập bằng cách dời `Canonical` vào `shipstate`, `fulfillment.Canonical` là bí danh (type alias).**
   Kế hoạch đòi `shipstate` dùng `fulfillment.Canonical` và `fulfillment.Advance` gọi `shipstate.Decide`: Go cấm hai gói
   nhập nhau. Bí danh giữ `fulfillment.Canonical` đúng là cùng một kiểu, mọi adapter không phải sửa. Phương án khác:
   gói thứ ba chỉ chứa `Canonical` (thêm gói không cần), hay `Advance` không gọi `Decide` (trái "chỉ còn một luật").
   Đảo: chuyển khối kiểu, hằng về `fulfillment/canonical.go`. Thêm tệp `shipstate/canonical.go` ngoài danh sách tệp.
2. **Thêm năm cạnh `returned_to_shop` ngoài bảng:** `awaiting_pickup`, `cancel_requested`, `shipping`,
   `delivery_failed`, `delivered` (chỉ API/nhân viên) + `ReturnedToShop` → `returning` với tác động của cạnh `Returning`
   tương ứng cộng `EffCarrierReturned`. Vì sao: khi tra bằng API hãng chỉ trả trạng thái mới nhất; bỏ lỡ tin "đang hoàn"
   thì theo bảng gốc vận đơn kẹt ở `shipping` với anomaly `invalid_edge` dù hàng đã về shop. Cùng tinh thần cạnh
   `awaiting_pickup + Delivered` (thiếu tin vẫn xuất tác động đúng một lần). Không cạnh nào tới `returned` (Q-9 giữ).
   Phương án khác: giữ bảng gốc (nhân viên xử lý tay từng vận đơn). Đảo: xóa năm dòng `CanonReturnedToShop` trong
   `graph.go` và các ca test tương ứng.
3. **`Decision.AdvanceLast` (trường thêm):** bên gọi dời `last_carrier_event_at` khi `Apply` **và** khi sự kiện trùng
   trạng thái nhưng mới hơn. Vì sao: nếu chỉ dời khi `Apply` (như chữ kế hoạch), chuỗi thất bại(t2) → thất bại(t4) đến
   trước, đang chuyển(t3) đến sau: t4 bị bỏ "trùng", t3 mới hơn t2 nên bị áp → vận đơn lùi về `shipping` sai. Test
   thuộc tính bắt được lỗi này (đã thử bỏ trường → FAIL). Task 99 (`ApplyTracking`) phải dùng `AdvanceLast` thay cho
   `Apply` khi ghi `last_carrier_event_at` (ghi ở Việc phát hiện thêm).
4. **`returning + ReturnedToShop` là một cạnh của bảng** (không đặt ở bước 3 "trùng trạng thái" như chữ kế hoạch), nên
   vẫn qua bước tin cũ và vận đơn hiện hành. Vì sao: đặt ở bước 3 thì một bản trùng cũ hơn được "áp" và dời mốc thời gian
   lùi lại. Với tin mới, vận đơn hiện hành: kết quả y như kế hoạch (`Returning`, `[EffCarrierReturned]`).
5. **`EffPickedUp` chỉ phát khi `Input.PickedUp = false` ở mọi cạnh** (kế hoạch ghi điều kiện "hàng còn held" ở cạnh
   `cancel_requested + Returning`): bảo đảm "lấy hàng đúng một lần" cả khi dữ liệu lạ. Đảo: bỏ bộ lọc trong `Decide`.
6. **`CanonLost`, `CanonDamaged` → `Ignored = "review"`** (kế hoạch chỉ ghi anomaly, không ghi lý do; `review` là giá trị
   CHECK sẵn có, như giao một phần). Mã chuẩn ngoài danh sách → như `CanonUnknown` (`unmapped`).
7. **`not_current_moved` tính cả `ReturnedToShop`** (hàng đã rời kho rồi quay về): thêm cảnh báo, an toàn hơn bỏ qua.
8. **`Display` chưa dùng `sub`, `cancelIntent`:** spec chưa nói nhãn chi tiết; giữ chữ ký của kế hoạch, nhãn chỉ theo
   trạng thái, mã lạ trả nguyên mã.
9. **`Advance` (M1) giới hạn ở bốn trạng thái có hạng của M1**, `creating` tới bước sau awaiting được coi như "tạo rồi áp
   tin" (VC10). Vì sao: không giới hạn thì `Advance(delivery_failed, shipping)` thành true và `refresh_status` của M1 sẽ
   chạy lại bước lấy hàng của đơn (sai tồn). Test `TestAdvance_SameAsM1Rank` so 10×10 cặp với bảng hạng cũ.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mỗi dòng bảng cạnh (+ cạnh returned_to_shop) | `TestDecide_Edges` (`movingEdges` 9 ca, `tableEdges` 16 ca, `returnedToShopEdges` 7 ca) | `go test -race ./internal/modules/fulfillment/internal/shipstate/` | `ok … shipstate 3.304s` (trong ci-task) |
| VC09 tin cũ → `older`, không anomaly | `TestDecide_PlanCases` ca "VC09 tin cũ" | như trên | ok |
| VC08 trùng trạng thái → `duplicate_state` (mới hơn: `AdvanceLast`) | `TestDecide_PlanCases` "VC08 …" (2 ca) | như trên | ok |
| delivered + in_transit mới hơn → `invalid_edge` + anomaly | `TestDecide_PlanCases` "delivered rồi in_transit mới hơn" | như trên | ok |
| VC11 không hiện hành: hủy → không anomaly; lấy hàng → `not_current_moved` | `TestDecide_PlanCases` 2 ca "VC11 …", 6 ca "không hiện hành, hàng rời kho …", 2 ca thêm (chờ lấy, tin cũ) | như trên | ok |
| VC12 cancel_requested + picked_up → `[EffPickedUp, EffCancelRejected]` | `TestDecide_PlanCases` "VC12 …" | như trên | ok |
| Thiếu tin lấy hàng: awaiting + delivered → `[EffPickedUp, EffDelivered]` | `TestDecide_PlanCases` "thiếu tin lấy hàng", "đã lấy hàng thì không phát lại picked_up" | như trên | ok |
| TD10: API/nhân viên → `[EffDeliveryReversed, EffReturning]`; hệ thống, rỗng → `invalid_edge` | `TestDecide_Edges` "TD10 …" (4 ca) | như trên | ok |
| unknown → `unmapped`; giao một phần → `review` + `partial_reported`; mất, hỏng → `lost_or_damaged` | `TestDecide_PlanCases` | như trên | ok |
| Không tới `returned`, không rời `cancelled`/`returned` (9 trạng thái × 13 sự kiện × 3 nguồn) | `TestDecide_TerminalAndReturned` | như trên | ok |
| Thuộc tính 1.000 lượt: cuối = trạng thái sự kiện mới nhất, picked_up 1 lần, delivered 1/0, không returned, không anomaly | `TestDecide_ShuffledJourney` (`testkit.SetRapidChecks(1000)`) | như trên | ok; đột biến tay "bỏ AdvanceLast khi trùng" → `FAIL … cuối shipping, muốn delivery_failed` |
| Fuzz không sập, quyết định nhất quán | `FuzzDecide` | `go test -run '^$' -fuzz=FuzzDecide -fuzztime=60s ./internal/modules/fulfillment/internal/shipstate/` | bản cuối: `elapsed: 1m0s, execs: 2235718 … PASS` |
| `Target`, `Display` | `TestTarget`, `TestDisplay` | như trên | ok |
| `Advance` giữ đúng luật hạng M1 (10×10 cặp), test M1 xanh | `TestAdvance_SameAsM1Rank`, `TestAdvance`, `TestAdvance_Property`, `TestStatusStep`, `TestStatusFromCanonical`, `refresh_status_test`, `create_shipment_test` | `go test -race ./internal/modules/fulfillment/` | `ok … fulfillment 24.826s`; đột biến tay "bỏ giới hạn cur" → 3 lỗi `muốn …` |
| Cổng kiểm của task | build, vet, lint, catalog, jscpd (0 clone), gitleaks, oasdiff, test-guard, test gói đổi + phụ thuộc | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 4535a0dc…)` (lần 1 ĐỎ do funlen/gocognit ở test, đã tách hàm) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm tệp `shipstate/canonical.go` (kiểu `Canonical` dời từ M1, Quyết định 1) và trường `Decision.AdvanceLast`
  (Quyết định 3); năm cạnh `returned_to_shop` (Quyết định 2); `returning + ReturnedToShop` thành cạnh (Quyết định 4).
- Hằng xuất thêm cho bên gọi: `Statuses()`, `Canonicals()`, `Reason*`, `Anomaly*` (chuỗi khớp CHECK của migration).
- Ba commit thay vì hai: (1) dời `Canonical` (cấu trúc), (2) máy trạng thái (hành vi), (3) `Advance` gọi `Decide`
  (cấu trúc, test M1 xanh).

## Việc phát hiện thêm

- Task 99: cân nhắc mở "Cần xem" khi tin bị bỏ vì `older` là tin kết thúc (`delivered`, `returning`) — chỉ xảy ra khi dữ
  liệu hãng bất thường (reviewer commit 2).
- **Task 99 (`ApplyTracking`)**: ghi `last_carrier_event_at` khi `Decision.AdvanceLast` (không chỉ khi `Apply`), và
  cập nhật `PickedUp` theo mốc `picked_up_at`/hàng không còn `held`. Kế hoạch dòng 748 ghi "khi Apply" — cần sửa theo.
- `cancel_requested + Delivered/DeliveryFailed` (hãng giao dù đã yêu cầu hủy) vẫn là `invalid_edge` theo bảng của kế
  hoạch: vận đơn vào "Cần xem". Có thể cần cạnh riêng (hủy không thành rồi giao) — để chủ shop/kế hoạch quyết.
- `delivery_failed + DeliveryFailed` (thất bại lần 2 mà không thấy tin giao lại, khi tra API chỉ có trạng thái mới
  nhất) là `duplicate_state`: không tạo việc gọi lại lần 2. Task 99/Task gọi lại nên xem `sub_status`/số lần thất bại.

## Reviewer

- Commit 1/3 (dời `Canonical`, alias): **ĐẠT**, không có phát hiện phải sửa. Reviewer đọc từng chỗ dùng `Canonical`/
  `Canon*` (fake, carriertest, fixtures, carriers/fake, unsupported, adapter, types, credentials_test): cùng kiểu, cùng
  giá trị, không có method trên `Canonical`. Ghi chú: nhãn commit ở mục Ảnh hưởng lệch, danh sách adapter thừa — đã sửa.
- Commit 2/3 (máy trạng thái `shipstate`): **ĐẠT**, không có phát hiện phải sửa. Reviewer kiểm từng Quyết định 2–7, bảng
  cạnh khớp kế hoạch dòng 702–714 (tác động và thứ tự), chuỗi lý do/anomaly/nguồn khớp CHECK migration 0300, số ca bảng
  bằng chứng khớp. Ghi chú đã xử lý: comment `graph.go` ghi "bốn cạnh" (sửa ở commit 3), fuzz chạy lại đủ 60 giây, dòng
  VC11 ghi rõ số ca. Ghi chú để Task 99: một tin `older` là tin kết thúc (vd. `delivered` đến sau một tin mới hơn bất
  thường) có thể nên mở "Cần xem" (đã thêm vào Việc phát hiện thêm).
- Commit 3/3 (`Advance` gọi `Decide`, 4 hằng, xóa `rank`): **ĐẠT**, không có phát hiện phải sửa. Reviewer kiểm tay mọi
  nhóm cặp (cur, next) so với bảng hạng cũ, hai chỗ gọi (`create_shipment.go:257`, `refresh_status.go:134`) không đổi
  hành vi kể cả với vận đơn M3 trong DB; `rank` đã xóa. Ghi chú: Task 99 dùng thẳng `Decide`, không qua `Advance`
  (để không mất tác động).

Kết luận: ĐẠT
