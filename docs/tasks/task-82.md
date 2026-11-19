# Task 82 — M2-25: Chỉ số khách và nhãn theo quy tắc

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 82 = M2-25). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn nào (0 chỗ tham chiếu phải sửa),
  chỉ thêm. Gói `customer`: `Writer.RecomputeStats`, `StatsFor`, `TagCondition`, `TagRule`, `TagRuleInput`,
  `GetTagRule`, `Writer.PutTagRule`, `Writer.ApplyTagRules`, `Writer.ApplyTagRulesBatch`, `StatsConsumer` (consumer
  `customer_stats`), hằng `StatsConsumerName`, `TagRulesNightlySchedule`, `TopicOrderDeliveryReversed`, `Field*`,
  `OpGte`, `OpLte`. `Background` (module.go) thêm consumer, lịch, handler — chữ ký không đổi (1 chỗ gọi:
  `cmd/crm/modules.go`; test `cmd/crm/main_test.go` `TestNewModulesRegistersCash` sửa theo: thêm
  `event.customer_stats`, lịch `customer.tag_rules_nightly`). `ErrInvalidTagRule` (422) đã có, đã ánh xạ ở
  `internal/httpapi/errors.go`. `internal/httpapi/errors_m2_test.go` (`TestM2Routes_Unimplemented`) dùng route quy tắc
  nhãn làm mẫu "chưa làm" → đổi sang `getOrderTimeline` (Task M2-24, còn 501). `export_test.go` thêm
  `NightlyHandlerForTest`.
- Gói phụ thuộc ngược (go list): `internal/modules/customer` ← `cmd/crm`, `internal/httpapi`, `internal/modules/order`,
  `internal/modules/promotion`, `internal/testkit/shiptest`. Không đổi ký hiệu có sẵn nên các gói này chỉ cần build.
- Operation OpenAPI bị chạm, oasdiff: `getCustomerTagRule`, `putCustomerTagRule` (có từ Task M2-02, đang 501). Bỏ dòng
  `description` "Chưa làm…"; thân yêu cầu dùng schema mới `TagConditionInput` (`field`, `op` là chuỗi, không enum) để
  trường lạ tới module và trả 422 `invalid_tag_rule` như kế hoạch đòi (enum của ogen trả 400) — nới yêu cầu, không phá
  vỡ; câu trả lời giữ `TagCondition` có enum. `make gen` sinh lại `internal/httpapi/gen/**`, `console/src/api/**`.
  Audit mới: `customer_tag_rule.put`; nhãn do quy tắc: `customer.tag_add`/`customer.tag_remove` (người làm system).
- Chỗ dùng trên màn hình: chưa có (màn quy tắc nhãn ở task màn hình M2 sau); `console/src/api/**` sinh lại.
- Câu sqlc, bảng, pha migration: không migration (cột chỉ số, `customer_tag_links.source`, `customer_tag_rules`, quyền
  cột đã có ở 0201). Tệp mới `queries/customer/stats.sql` (đếm từ `ord.orders_v`, tiền hoàn từ `ord.returns_v` —
  ngoại lệ đọc view của customer đã khai trong `tests/arch/sqlusage_test.go`; ghi chỉ số gốc, đặt 0 hồ sơ con; đọc chỉ
  số), `queries/customer/tagrules.sql` (đọc, chèn, sửa quy tắc; gắn/gỡ dòng `source rule`; duyệt gốc theo lô).
- Test sẽ chạy: `go test -race ./internal/modules/customer/...` (cả gói), `go test -race ./internal/httpapi/ -run
  'TagRule|M2Routes|Error'`, `go test ./cmd/crm/`, `go test ./tests/arch/...`; cổng ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (background, events,
  queue, schedules); tìm `statsOf`, `ReturnRateBP`, `LockCustomers`, `GroupMembers`, `RootOf`, `InsertTagLink`,
  `auditEntity`, `signal`, `expireDraftsSchedule` (lịch theo tenant của order), `cash.Consumer` (mẫu consumer).
- Dùng lại / viết mới vì: dùng lại `statsOf` (một chỗ tính `ReturnRateBP`; `StatsFor` đọc qua nó), `LockCustomers`
  (khóa nhóm theo id tăng dần như gộp), `RootOf`, `GroupMembers`, `InsertTagLink` (gắn dòng rule, ON CONFLICT giữ dòng
  staff), `auditEntity`, `signal`, `events.Consumer`, `schedules.Schedule` theo tenant. Viết mới: câu đếm chỉ số (chưa
  có), câu quy tắc nhãn, bộ đánh giá điều kiện (nghiệp vụ của customer, không dùng chung).

## Soát thiết kế

5.1 Chủ dữ liệu: chỉ số, nhãn, quy tắc thuộc `customer` (bảng có sẵn ở schema `customer`). 5.2 Không ghi bảng module
khác: chỉ ghi `customer.*`. 5.3 Đọc đơn, phiếu qua view `ord.orders_v`, `ord.returns_v` (ngoại lệ Q19 đã khai, không
nhập Go `order`). 5.4 Nguyên tử: tính lại chỉ số + áp quy tắc trong cùng transaction của việc consumer. 5.5 Sau commit:
consumer `customer_stats` nghe `order.*`, `customer.merged/merge_undone` qua hàng đợi (không gọi thẳng trong transaction
của đơn). 5.6 Gọi ra ngoài: không có. 5.7 Tenant: mọi câu trong tx của worker (WithTenant của việc) hoặc `WriteTx`;
không bảng mới. 5.8 Module không nhập HTTP; handler mỏng `handlers_customer_tagrules.go`. 5.9 PUT có `x-permission:
customer.tags.manage` và `IdempotencyKey` (có sẵn); GET `customer.read`. 5.10 Không Valkey. 5.11 Không thư viện mới.
5.12 Theo mẫu `tags.go`, `merge.go` (Writer, audit, báo hiệu) và `order/drafts.go` (lịch theo tenant).

6.1 Đồng thời: tính lại khóa cả nhóm (gốc + hồ sơ con) theo id tăng dần bằng `LockCustomers`, đọc lại gốc và nhóm dưới
khóa, lệch thì lùi savepoint làm lại (tối đa 3 lần, như gộp); hai sự kiện của hai đơn khác nhau chạy song song nối tiếp
nhau ở khóa nhóm và mỗi lần tính từ dữ liệu gốc đã commit (READ COMMITTED: câu sau khóa thấy dữ liệu mới) → số cuối đúng
dù sự kiện tới sai thứ tự. Quy tắc nhãn có `version` (PUT sửa có điều kiện). 6.2 Gửi lại: PUT có khóa chống trùng;
consumer chạy lại cho cùng kết quả (tính lại toàn bộ, không cộng dồn). 6.3 Thứ tự khóa ADR-46 nhóm 5: `customers` (theo
id) → `customer_tag_links` → `customer_tag_rules` (chỉ đọc); gộp + hoàn tác: khóa hợp hai nhóm một lần theo id; lịch đêm
khóa từng gốc theo id tăng dần. Không khóa `ord.*`. 6.4 Lỗi tạm: việc nền có lịch thử lại. 6.5 READ COMMITTED + khóa
dòng. 6.6 Việc nền: consumer 8 lần rồi dead; lịch đêm lỗi thì kỳ sau chạy lại; payload hỏng → `ErrPermanent`. 6.7 Không
áp dụng: không gọi bên ngoài. 6.8 Tiến trình chết: transaction lùi, việc chạy lại. 6.9 Mỗi việc lịch đêm tối đa 1.000
gốc (dưới hạn 30 s), lô sau là việc mới. 6.10 Tiền `int64` đồng; `total_spent` không âm (kẹp 0 theo CHECK). 6.11 Không
chạm vùng rủi ro cao; quy luật "tính lại không phụ thuộc thứ tự" có test sai thứ tự. 6.12 Không dữ liệu cá nhân trong
audit, payload (chỉ mã, số). 6.13 Quyền: GET `customer.read`, PUT `customer.tags.manage` (guard mặc định từ chối).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Ca 1: A có đơn delivered 690.000, returned (hoàn qua hãng), cancelled → 2/1/1/690.000, tỷ lệ 5000; khách trả một món 250.000 (kho nhận) → 440.000 | `TestStatsFromSource` (đơn, phiếu tạo qua `order.Writer`: `Create`, `OnShipmentEvent`, `OnShipmentReturning`, `ReceiveReturn`, `Cancel`, `CreateReturn`) | `go test -race ./internal/modules/customer/ -run 'TestStats\|TestTagRule' -v` | `--- PASS: TestStatsFromSource (0.79s)` |
| Ca 2: gộp B (delivered 300.000) vào A → A 2 giao, 740.000, B về 0; hoàn tác → A 440.000, B 300.000 | `TestStatsFromSource` (`mergeAndFeed`, `UndoMerge` + sự kiện `customer.merged`/`merge_undone` từ `platform.domain_events`) | như trên | PASS |
| Ca 3: sự kiện sai thứ tự (`order.returned` trước `order.delivered`) → số cuối đúng | `TestStatsFromSource` (`feed(true)`: giao ngược thứ tự ghi) | như trên | PASS |
| Ca 4: "Hay hoàn" gắn rule cho A; thêm hai đơn giao → 2500 → gỡ; nhãn tay cùng tên của C không bị gỡ; tắt quy tắc → gỡ; version lệch/thiếu → ErrVersionConflict | `TestTagRuleReturnRate` | như trên | `--- PASS: TestTagRuleReturnRate (0.82s)` |
| Ca 5: "Lâu không mua" `days_since_last_order gte 90`: +89 ngày không gắn, +91 ngày lịch đêm gắn; khách chưa mua không gắn; lô đầy ghi việc nối tiếp | `TestTagRuleNightly` | như trên | `--- PASS: TestTagRuleNightly (0.53s)` |
| Ca 6: `field: "phone"` → 422 `invalid_tag_rule` (HTTP); module: phép lạ, rỗng, 11 điều kiện, âm, > 2^53−1 → `ErrInvalidTagRule`; nhãn lạ, quy tắc chưa có → `ErrNotFound` | `TestCustomerTagRuleAPI`, `TestTagRuleInvalid` | `go test -race ./internal/httpapi/ -run 'TagRule\|M2Routes\|Error'`; lệnh module như trên | `ok elimaz.com.vn/crm/internal/httpapi`; `--- PASS: TestTagRuleInvalid (0.29s)` |
| Ca 7: `StatsFor(B)` khi đã gộp = chỉ số của A (`ReturnRateBP` 3333); `order.delivery_reversed` giả → tính lại từ dữ liệu gốc (số sửa tay bị ghi đè) | `TestStatsFromSource` | như trên | PASS |
| Payload hỏng / thiếu khách / khách lạ → `ErrPermanent`; chạy lại cùng sự kiện cho cùng kết quả; tập rỗng (chỉ đơn hủy) → 0, `LastOrderAt` nil | `TestStatsConsumerPayload` | `go test -race ./internal/modules/customer/ -run TestStatsConsumerPayload` | `ok … customer 2.778s` |
| Tính lại song song với gộp không deadlock, số cuối đúng | `TestStatsConcurrentWithMerge` | `go test -race ./internal/modules/customer/ -run TestStatsConcurrent -count=3` | `ok … customer 5.585s` |
| Consumer và lịch được nối vào tiến trình | `TestNewModulesRegistersCash` | `go test ./cmd/crm/ -run TestNewModules` | `ok elimaz.com.vn/crm/cmd/crm` |
| Cổng tĩnh và test gói đổi + phụ thuộc (sau khi gộp main f15c4ef) | build, vet, lint, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard, console-check, test | ci-task (lần 2) | mọi bước qua; `ok` customer 106 s, httpapi 82 s, order 222 s, promotion, cash, fulfillment, files, realtime, cmd/crm, tests/arch, tests/e2e. gen-check đỏ chỉ vì `stats_test.go` (ca tập rỗng) sửa lúc cổng đang chạy, chưa stage; đã stage, `make gen` không đổi mã sinh. |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `RecomputeStats`, `ApplyTagRules` là phương thức của `customer.Writer` (kế hoạch ghi hàm gói): đổi nhãn, chỉ số cần
  báo hiệu `customer.changed` qua `Writer.Realtime` và audit như `AddTag`. Chữ ký còn lại đúng kế hoạch (`ctx, tx, id,
  now`). `StatsFor` là hàm gói như kế hoạch.
- Thân `putCustomerTagRule` dùng schema mới `TagConditionInput` (không enum) — enum của ogen trả 400 `bad_request`,
  trong khi kế hoạch đòi `field: "phone"` → 422 `invalid_tag_rule`. Câu trả lời giữ `TagCondition` có enum.
- Lịch đêm chia lô bằng việc nối tiếp: mỗi việc tối đa 1.000 gốc (`after` = gốc cuối), lô đầy thì ghi việc kế tiếp
  cùng kỳ trong cùng transaction, để mỗi việc ở dưới hạn 30 s và không giữ khóa mọi khách trong một transaction.

## Quyết định thay chủ shop

- **QĐ1 — lịch đêm áp mọi quy tắc**, không chỉ quy tắc có `days_since_last_order`: nhờ vậy quy tắc vừa đặt, vừa sửa
  hay vừa tắt tới mọi khách trong vòng một ngày (kế hoạch không nói khi nào khách hiện có được áp quy tắc mới). Kết quả
  của quy tắc không theo ngày không đổi vì áp lại cho cùng kết quả. Phương án khác: chỉ quy tắc theo ngày (khách cũ chỉ
  được áp khi có sự kiện mới), hoặc PUT áp ngay cho mọi khách (một transaction dài). Dễ đảo: lọc `rules` trong
  `ApplyTagRulesBatch`.
- **QĐ2 — khách chưa có đơn nào không đạt điều kiện `days_since_last_order`** ("lâu không mua" chỉ cho người đã mua).
  Phương án khác: coi như vô hạn ngày.
- **QĐ3 — quy tắc tắt (`enabled false`) → dòng rule của nhãn đó bị gỡ** ở lần áp kế tiếp (sự kiện hay lịch đêm).
- **QĐ4 — `total_spent` chỉ trừ tiền hoàn của phiếu thuộc đơn đang `delivered`, và kẹp ≥ 0** (CHECK của bảng); đơn bị
  đảo giao (M3) không còn tính tiền mua lẫn tiền hoàn.
- **QĐ5 — ghi chỉ số không tăng `version` của hồ sơ** (version là của người sửa hồ sơ; chỉ số đổi theo đơn không được
  làm hỏng lần sửa đang mở). Ghi `stats_updated_at`.
- **QĐ6 — lịch đêm chỉ đụng dòng rule của chính gốc** (khóa từng gốc); dòng rule còn sót ở hồ sơ con sau gộp do consumer
  dọn khi tính lại nhóm (khóa cả nhóm).

## Việc phát hiện thêm

- `internal/modules/customer/search.go` (`hitOf`) tự tính lại `ReturnRateBP` thay vì gọi `statsOf` (cùng công thức,
  hai chỗ). Nên đổi `hitOf` dùng `statsOf` ở một commit cấu trúc riêng để giữ "một chỗ tính tỷ lệ" (Q-X2).
- Lịch đêm (`nightlyHandler.Handle`) chỉ ghi lô nối tiếp khi lô đầy (`n == batch`); nếu một gốc bị gộp đúng lúc
  `LockRootsAfter` đang khóa thì lô ngắn hơn và các gốc còn lại của tenant chờ tới đêm sau. Sửa chặt hơn: nối tiếp khi
  `n > 0`, dừng khi lô rỗng (reviewer ghi chú, không bắt buộc).
- Consumer không nghe `order.updated` (kế hoạch không liệt kê): nếu sau này sửa được `grand_total` của đơn đã giao mà
  không đổi trạng thái thì cần thêm topic này.

## Reviewer

Kết luận: ĐẠT (lần 1, trên phần đã stage sau khi gộp main f15c4ef). Phát hiện phải sửa: không có. Ảnh hưởng khớp
thực tế, không nơi gọi bị sót; bảng bằng chứng: mọi test có thật và kiểm đúng tiêu chí; không test bị xóa, tắt, nới.
Ghi chú không bắt buộc: (1) `LockRootsAfter` có thể trả ít hơn cỡ lô khi một gốc vừa bị gộp lúc đang khóa, lịch đêm
khi đó dừng sớm, các gốc còn lại chờ tới đêm sau (hiếm, chỉ trễ một ngày) — ghi ở "Việc phát hiện thêm"; (2) báo hiệu
`customer.changed` mỗi lần tính lại kể cả khi số không đổi — vô hại.
