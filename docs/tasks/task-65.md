# Task 65 — M2-08 Máy trạng thái đơn, nhãn gộp, phạm vi được sửa (thuần Go)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 65, M2-08). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn nào. Hai gói **mới**:
  `internal/modules/order/internal/state` (chỉ `order` nhập được): `Status` và 7 hằng, `Conditions`, `Check`,
  `EditScope`, `Editable`, sentinel `ErrInvalidTransition`, `ErrNeedsApproval`, `ErrAddressIncomplete`;
  `internal/queries/orderview` (gói thuần, Q32): `Axes`, `LabelKey` và 16 hằng, `Label`, `Labels`. Chưa có chỗ dùng
  (0 tham chiếu): `order` dùng từ Task M2-16, 19–24; `internal/queries` từ Task M2-24; M3, M6 sau. Hằng
  `order.StatusConfirmed`, `order.StatusDelivered` (chuỗi không kiểu, M1) giữ nguyên: đổi sang `state.Status` là việc
  của task đầu tiên dùng máy trạng thái trong `order` (M2-16/18), ghi ở "Việc phát hiện thêm".
- Gói phụ thuộc ngược (go list): không có (hai gói mới, chưa ai nhập). `state` nhập `orderview` (kế hoạch: `Editable`
  nhận `orderview.Axes`). Luật biên (`tests/arch/boundaries_test`): module nhập `internal/queries/orderview` không vi
  phạm luật nào hiện có (luật f chỉ cấm `internal/queries` nhập module); `orderview` không nhập gì ngoài thư viện chuẩn.
- Operation OpenAPI bị chạm, oasdiff: không. (Enum `OrderLabel` 16 giá trị thuộc Task M2-02.)
- Chỗ dùng trên màn hình: chưa. `console/.../labels.ts` (Task M2-28) theo đúng thứ tự `orderview.Labels()`.
- Câu sqlc, bảng, pha migration: không (thuần Go, không database). Giá trị trục khớp CHECK của Task M2-05
  (`pending_reason`, `packing_status`) và 7 trạng thái của M1 (G7).
- Test sẽ chạy: `go test -race ./internal/modules/order/internal/state/... ./internal/queries/orderview/...`,
  `go test ./tests/arch/...` (luật biên với gói mới); `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tìm "transition",
  "trạng thái", "nhãn", "label", "state") — không có máy trạng thái chung. Tìm trong `internal`: `ErrInvalidTransition`,
  `invalid_transition` — chưa có; `order.ErrStatusConflict` (M1) là lỗi "đơn không ở trạng thái cần" của đường ghi M1,
  khác việc (kiểm cạnh); `fulfillment`/`inventory` có trạng thái riêng của bảng mình (giữ hàng), không phải đơn.
- Dùng lại / viết mới vì: viết mới (kế hoạch giao đúng hai gói này, Q32). Không đặt máy trạng thái vào
  `internal/platform` (luật nghiệp vụ của `order` ở lại trong module, CLAUDE.md). `Editable` dùng lại `orderview.Axes`
  thay vì khai kiểu thứ hai.

## Soát thiết kế

- 5.1 chủ dữ liệu: luật chuyển trạng thái và phạm vi sửa của đơn thuộc `order` (`order/internal/state`); nhãn gộp là
  phép tính hiển thị thuần, ở `internal/queries/orderview` để `order`, `internal/queries`, M3, M6 cùng một nguồn (Q32).
- 5.2–5.7: không áp dụng: không đọc, ghi database, không sự kiện, không gọi ra ngoài.
- 5.8 nghiệp vụ không biết HTTP: hai gói chỉ nhập `errors` (và `orderview`); lỗi là sentinel, đổi sang HTTP ở
  `errors.go` của lớp HTTP ở task dùng (M2-02/19–21).
- 5.9, 5.10: không áp dụng: không đường ghi, không Valkey.
- 5.11 thư viện: chỉ thư viện chuẩn; rapid (ADR-44) cho test thuộc tính.
- 5.12 module mẫu: gói `internal` của module như `order/internal/pricing` (sentinel trong gói, test gói `_test`).
- 6.1–6.9: không áp dụng: hàm thuần, không trạng thái chung, không khóa. Ghi chú: `Check` không thay khóa dòng; bên
  gọi (M2-19–21) khóa đơn và kiểm `version` trước khi gọi.
- 6.10 tiền: không áp dụng.
- 6.11 quy luật: test duyệt đủ 49 cặp × mọi tổ hợp điều kiện; rapid: `Label` luôn trả một khóa trong `Labels()` với
  trạng thái hợp lệ (trục bất kỳ), `Editable` của đơn có vận đơn hiện hành không bao giờ mở trường hãng giữ.
- 6.12: không áp dụng: không dữ liệu cá nhân.
- 6.13 quyền: không áp dụng (quyền kiểm ở `guard`); `Editable` là luật nghiệp vụ, không thay quyền.

## Quyết định thay chủ shop

- **Q-T65-1 — "điều kiện mặc định" của bảng 49 cặp.** Với `Conditions{}` thì `draft → confirmed` (thiếu địa chỉ) và
  `confirmed → cancel_requested` (chưa vận đơn) đều bị từ chối, nên câu "đúng 11 cặp hợp lệ với điều kiện mặc định"
  không đúng chữ. Chọn: một cặp là **hợp lệ** khi có ít nhất một tổ hợp điều kiện cho qua; test duyệt 49 cặp × 16 tổ
  hợp (`HasCurrentShipment` × `AddressComplete` × 4 `PendingReason`) và đòi đúng 11 cặp; 38 cặp còn lại trả
  `ErrInvalidTransition` với **mọi** tổ hợp. Mạnh hơn cách đọc chữ. Khác: chỉ thử `Conditions{}` (mất 2 cạnh).
- **Q-T65-2 — lỗi khi cạnh có nhưng điều kiện vận đơn sai** (`confirmed → cancelled` khi có vận đơn,
  `confirmed → cancel_requested` khi chưa): trả `ErrInvalidTransition`. Bên gọi (M2-21) chọn đích theo
  `HasCurrentShipment` nên lệch là lỗi lập trình; kế hoạch không khai sentinel riêng. Khác: thêm `ErrHasShipment`
  (dễ thêm sau, không phá ai).
- **Q-T65-3 — `draft → confirmed` khi `PendingReason` khác rỗng:** `discount_approval` → `ErrNeedsApproval` (cùng lỗi
  với `new`); `awaiting_customer`, `out_of_stock` → `ErrInvalidTransition`. Kiểm địa chỉ trước (`ErrAddressIncomplete`).
  Khác: thêm sentinel `ErrPendingReason`.
- **Q-T65-4 — `confirmed → delivered`, `confirmed → returned` không đòi `HasCurrentShipment`.** Kế hoạch không ghi
  điều kiện; `OnShipmentEvent` (M2-18) đã chỉ nhận sự kiện của vận đơn hiện hành (B1). Không thêm điều kiện để không
  chặn đường của M2-18/M3. Khác: đòi vận đơn hiện hành (chặt hơn; thêm được sau bằng một dòng và một test).
- **Q-T65-5 — tổ hợp ngoài bảng của `Label`:** trạng thái lạ → `""` (không thuộc `Labels()`, bên gọi coi là lỗi dữ
  liệu); `new` với lý do lạ → `new`; `confirmed` với vận đơn ở trạng thái ngoài bảng (`creating`, `delivered`…) →
  theo trục đóng gói (`picking` → `packing`, ngược lại `confirmed`; `packed` chỉ khi chưa vận đơn, đúng chữ bảng);
  `cancel_requested`, `cancelled`, `delivered`, `returned` → theo `Status`, bỏ qua hai trục kia.
- **Q-T65-6 — `Editable` với trạng thái lạ → không trường nào** (an toàn: sửa bị từ chối); `draft`, `new` mà có
  `ShipmentStatus` (dữ liệu không thể có) cũng khóa như `confirmed` có vận đơn.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi viết mã | toàn bộ test hai gói (mã khung trả nil, "", EditScope{}) | `go test -race -count=1 ./internal/modules/order/internal/state/... ./internal/queries/orderview/...` | 7 test `--- FAIL` (AllPairs, Conditions, Editable, ShipmentLocks, Labels, Label_Table, AlwaysKnown), `FAIL` hai gói |
| 49 cặp: đúng 11 cặp đi được (10 cạnh 05 mục 2 + `new → new`), 38 cặp còn lại `ErrInvalidTransition` với cả 16 tổ hợp điều kiện; `draft → new` bị từ chối | `TestCheck_AllPairs`, `TestCheck_Conditions/draft→new` | `go test -race -count=1 -v ./internal/modules/order/internal/state/...` | `--- PASS: TestCheck_AllPairs`, `--- PASS: TestCheck_Conditions` |
| `confirmed → cancelled` khi có vận đơn → lỗi; `confirmed → cancel_requested` khi chưa → lỗi | `TestCheck_Conditions` (2 ca) | như trên | PASS (`ErrInvalidTransition`, Q-T65-2) |
| `draft → confirmed` thiếu địa chỉ → `ErrAddressIncomplete`; `new → confirmed` chờ duyệt giảm → `ErrNeedsApproval`; draft có lý do chờ (Q-T65-3) | `TestCheck_Conditions` (27 ca) | như trên | PASS |
| 7 trạng thái đúng chuỗi CHECK của `ord.orders` | `TestStatusValues` | như trên | PASS |
| `Label` đủ 16 hàng của bảng; tổ hợp ngoài bảng (`delivered` + `picking` → `delivered`; trạng thái lạ → `""`; vận đơn `creating`) | `TestLabel_Table` (27 ca) | `go test -race -count=1 -v ./internal/queries/orderview/...` | `--- PASS: TestLabel_Table` |
| `Labels()` đủ 16, đúng thứ tự bảng, đúng chuỗi; bên gọi sửa slice không đổi danh sách gốc | `TestLabels_OrderAndConstants` | như trên | PASS (lần đầu FAIL vì trả slice của mảng chung; đã sửa) |
| Trạng thái hợp lệ + trục bất kỳ → nhãn thuộc `Labels()` | `TestLabel_AlwaysKnownForValidStatus` | như trên | `[rapid] OK, passed 100 tests` |
| `Editable`: `confirmed` + `awaiting_pickup` → chỉ `CareBy`, `TrafficSource`; `confirmed` chưa vận đơn → mọi trường; `delivered` → chỉ `CareBy`; trạng thái lạ → không gì | `TestEditable` (14 ca) | `go test -race -count=1 -v ./internal/modules/order/internal/state/...` | `--- PASS: TestEditable` |
| Có vận đơn hoặc đơn kết thúc: không mở trường hãng giữ; `CareBy` luôn mở | `TestEditable_ShipmentLocks` | như trên | `[rapid] OK, passed 100 tests` |
| Cổng kiểm (sau khi gộp main 789202f) | build, vet, golangci-lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff (so điểm tách nhánh), test-guard — đúng các bước của `ci-task.sh` (hook của phiên chặn gọi thẳng script) | `make build vet lint gen-check sqlc-vet catalog-check jscpd gitleaks test-guard`, `make oasdiff OASDIFF_BASE=-base-ref=789202f` | mọi lệnh `exit 0`; `0 issues.` |
| Gói đổi và phụ thuộc, kiến trúc | `go test -race -count=1 ./internal/modules/order/... ./internal/queries/... ./tests/arch/...` | như cột trái | `ok` cả 6 gói có test (order, pricing, state, queries, orderview, tests/arch) |
| `make ci` trước khi gộp (bản 51b34a5) | toàn bộ | `make ci` | build, vet, lint (`0 issues.`) qua; `go test ./...` chỉ đỏ `TestMakeMutation_CoversHighRiskDirs` (tests/deploy) — có sẵn trên main, không do task này (xem Việc phát hiện thêm) |
| Lint hai gói | golangci-lint | `golangci-lint run ./internal/modules/order/internal/state/... ./internal/queries/orderview/...` | `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `order.StatusConfirmed`, `order.StatusDelivered` (M1, chuỗi không kiểu) trùng việc với `state.Status`; task đầu
  tiên dùng máy trạng thái trong `order` (M2-16 hoặc M2-18) nên chuyển sang `state.Status` và xóa hằng cũ.
- Luật biên "module nhập `internal/queries/orderview`" (ngoại lệ ghi trong `boundaries_test`) thuộc Task M2-02; hiện
  không luật nào chặn nên task này không sửa `boundaries_test`.

- `tests/deploy` `TestMakeMutation_CoversHighRiskDirs` đỏ trên main: commit a4492e7 mở rộng vùng rủi ro cao
  (`internal/platform/db`, `webhooks`, `modules/inventory`) nhưng `make mutation` chưa đột biến ba thư mục này. Task này
  không chạm `Makefile`; để điều phối viên giao sửa.
- Hook `guard-bash.sh` của phiên nạp từ trước khi có `ci-task.sh` nên chặn gọi script này; đã chạy tay đúng các bước
  của nó.

## Reviewer

Kết luận: ĐẠT (vòng 1, trên bản đã gộp main 789202f). Không phát hiện ảnh hưởng tính đúng. Reviewer so từng cạnh
của `Check` với sơ đồ đơn 05 mục 2: đủ 10 cạnh, chỉ thêm `new → new`, `draft → new` bị từ chối; bảy chuỗi trạng thái
khớp CHECK ở `migrations/0117_customer_ord.sql`; số ca trong bảng bằng chứng khớp mã (27, 27, 14). Ghi chú không bắt
buộc (không sửa): chú thích test rapid của nhãn nói hơi quá (16 nhãn đạt được do `TestLabel_Table` phủ); nhãn
`confirmed` + `packed` + vận đơn `creating` hiện `confirmed` (đúng chữ bảng, Q-T65-5, xem lại ở M2-16/M3); Q-T65-4 nên
thêm điều kiện vận đơn hiện hành nếu sau này có đường thứ hai gọi `Check` cho `delivered`/`returned`.
