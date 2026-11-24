# Task 198 — Hãng giao hàng trên website: hãng hiện (của M3), ưu tiên và phí dự phòng, báo phí cho web, ước thời gian giao

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 198, M5-15; kèm "Bổ sung Task 450" ngay sau task). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ THÊM ở `fulfillment` (tệp mới `webcarriers.go`):
  `WebCarrierSettings`, `WebCarrierSettingsChange`, `GetWebCarrierSettings`, `(*Service).SaveWebCarrierSettings`,
  `WebCarrier`, `(*Service).WebCarriers`, `WebCarrierStat`, `WebCarrierStats`, `WebShipQuoteInput`, `ShipOption`,
  `(*Service).QuoteForWeb`, `(*Service).DeliveryEstimates`, `TopicWebCarrierSettingsChanged`, `ErrCarrierNotOnWeb`,
  `ErrLastWebCarrier`, `ErrNoQuote`, `MaxWebFallbackFee`, `WebCarrierSettingsChanged` (payload). `httpapi/web`: kiểu
  mới `Shipping`, `NewShipping`; `httpapi.Deps` thêm trường riêng `webShipping` (prepare, chỉ khi `Fulfillment` khác nil). Đổi hành vi (không đổi chữ ký) `(*Service).UpdateAccount`
  (1 chỗ gọi mã thật: `httpapi/handlers_carrier_accounts.go:178`; 1 chỗ test `accounts_test.go:467`): tắt
  `show_on_web` của hãng active cuối cùng → `ErrLastWebCarrier`; tắt hãng đang ưu tiên → bỏ ưu tiên cùng transaction.
  `webcache`: hằng nội bộ `topicCarrierWebSettings` thay bằng `fulfillment.TopicWebCarrierSettingsChanged` (cùng chuỗi
  `carrier.web_settings_changed`, mapping đã có từ Task 185 → bump `SpaceCarriers`, `SpaceShipQuote`).
- Gói phụ thuộc ngược (go list): fulfillment được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/adapters/carrier/**`,
  `internal/modules/cash/**`, `internal/testkit/{shiptest,codtest}`, `tools/fakecarrier/server`; chỉ thêm ký hiệu nên
  không gói nào phải sửa; `httpapi` (handler mới), `httpapi/web` (tệp mới), `wiring/webcache` (hằng) sửa.
- Operation OpenAPI bị chạm, oasdiff: THÊM 5 operation, không đổi operation cũ. Nhân viên: `getWebCarrierSettings`
  `GET /api/v1/web-carrier-settings` (`fulfillment.carrier_accounts.read`), `saveWebCarrierSettings`
  `PUT /api/v1/web-carrier-settings` (`fulfillment.carrier_accounts.manage`, IdempotencyKey). Website (`x-auth:
  service_key`, `web.shipping.quote` — quyền đã có ở `identity/servicekeys.go:61`): `webShippingCarriers`
  `GET /api/v1/web/shipping/carriers`, `webShippingQuote` `POST /api/v1/web/shipping/quote` (`x-idempotency: exempt`),
  `webDeliveryEstimates` `GET /api/v1/web/shipping/estimates`. Mã lỗi mới trong `x-error-codes`: `carrier_not_on_web`,
  `last_web_carrier` (cả hai 422); `updateCarrierAccount` có thể trả thêm 422 `last_web_carrier` (bổ sung mô tả).
- Chỗ dùng trên màn hình: chưa có màn hình (M5-25, Task 208 vẽ sau); console chỉ đổi mã sinh `console/src/api/**`
  (make gen) và thêm câu tiếng Việt của 2 mã lỗi mới vào `console/src/app/errors.ts` (test `errors.test.ts`).
- Câu sqlc, bảng, pha migration: migration mới `0506_channel_carrier_settings.sql` (pha mở rộng: bảng mới
  `fulfillment.channel_carrier_settings` theo Interfaces, RLS, GRANT cho crm_app); câu mới `queries/fulfillment/channel_settings.sql`
  (đọc, chèn mặc định, khóa, ghi cài đặt; danh sách tài khoản hiện trên web không có khóa API; khóa tài khoản để kiểm;
  đếm hãng hiện còn lại; số vận đơn 30 ngày theo tài khoản; trung vị ngày giao theo tỉnh trên `fulfillment.shipments`
  của chính module, khóa JSON `ship_to->>'provinceCode'`). Thứ tự khóa: nhóm 8 `carrier_accounts → … →
  channel_carrier_settings` (đã có trong ADR-46).
- Test sẽ chạy: `go test -race -run 'WebCarrier|QuoteForWeb|DeliveryEstimates|UpdateAccount' ./internal/modules/fulfillment/`
  (gói fulfillment là vùng rủi ro cao? — không nằm trong HIGH_RISK_DIRS trừ `fulfillment/status.go`, không chạm);
  `./internal/httpapi/web/ -run WebShipping`, `./internal/httpapi/ -run 'WebCarrierSettings|DomainErrorSnapshot'`,
  `./internal/wiring/webcache/`, `./tests/arch/`, console `errors.test.ts`; còn lại do `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (cache.ReadCache,
  events.Registry, realtime.Bus, tenancy.WithTenant), `fulfillment.QuoteNow`/`ActiveAccounts`/`LocationMap` (M3),
  mẫu cài đặt một dòng mỗi tenant `order.GetWebIntake`/`SetWebIntake` (Task 456), mẫu route web có bộ đệm
  `web.Catalog.IntakeStatus`/`Locations`, mapping `webcache` (đã có `EntityCarrierSettings` → `SpaceCarriers`,
  `SpaceShipQuote`), lỗi `fulfillment.ErrVersionConflict`, `ErrSettingOutOfRange`, mã `retry_later`.
- Dùng lại / viết mới vì: dùng lại `QuoteNow` (gọi hãng song song có hạn, ngoài transaction), `ActiveAccounts` (tài
  khoản đã giải mã), `LocationMap`, `ReadCache.GetOrLoad`, sự kiện và mapping webcache sẵn có, sentinel lỗi sẵn có
  (version, khoảng giá trị) và mã HTTP sẵn có. Viết mới: bảng cài đặt kênh (kế hoạch chỉ định), câu đọc tài khoản hiện
  trên web có tên (`ActiveAccounts` không có `Name` và giải mã khóa — danh sách cho web không cần khóa), thống kê vận
  đơn 30 ngày và trung vị ngày giao (chưa có câu nào làm). Mẫu Ensure/Lock/Update giống `order.web_intake` nhưng khác
  module, khác lý do đổi → không gộp (luật "logic nghiệp vụ ở lại module").

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: cài đặt hãng trên web thuộc `fulfillment` (QĐ28, B15) → bảng trong schema `fulfillment`.
- 5.2 Không ghi bảng module khác: chỉ ghi `fulfillment.channel_carrier_settings`, `carrier_accounts` (của mình); audit
  qua `identity.RecordAudit` ở lớp HTTP (mẫu `handlers_fulfillment_settings.go`).
- 5.3 Đọc module khác qua view: không đọc bảng module khác (vận đơn là bảng của chính fulfillment; địa chỉ kho qua
  `inventory.warehouses_v` có sẵn trong `ActiveAccounts`).
- 5.4 Nguyên tử: lưu cài đặt + sự kiện + báo hiệu một transaction; tắt hãng + bỏ ưu tiên + sự kiện một transaction.
- 5.5 Không nguyên tử sau commit: làm mới bộ đệm website qua sự kiện `carrier.web_settings_changed` → consumer webcache.
- 5.6 Gọi ra ngoài: báo phí cho web là ĐỌC đồng bộ (không ghi ở hãng, không tạo gì) qua `QuoteNow` của M3 (A19, A2-12)
  sau khi ĐÓNG transaction; không phải lệnh `commands` vì không có tác dụng phụ ở hãng. Hiện production không có adapter
  báo phí (tích hợp hãng tạm dừng 21/11, tài khoản `manual`) → mọi hãng `rejected` không gọi mạng → phí dự phòng.
- 5.7 RLS: bảng mới có `tenant_id`, RLS + FORCE, khóa chính (tenant_id, channel), FK ghép (tenant_id, preferred_account_id).
- 5.8 Module không nhập net/http, ogen: đúng; lớp `httpapi/web` đổi kiểu.
- 5.9 Đường ghi có khóa chống trùng và quyền: `saveWebCarrierSettings` có IdempotencyKey, `x-permission`; ba route web
  chỉ đọc (quote `x-idempotency: exempt` có lý do).
- 5.10 Valkey chỉ là bộ đệm: phí báo cho web là hiển thị; đơn web vẫn kiểm phí ở CRM (M5-10/11 nhận `shippingFee`).
  Khóa đệm báo phí gồm `version` cài đặt đọc từ PostgreSQL.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu: module (Task 7), cài đặt một dòng có version (Task 456), route web có đệm (Task 190/191).
- 6.1 Đồng thời: cài đặt có `version` (khóa dòng rồi so); hai lệnh tắt hai hãng cuối cùng cùng lúc: mỗi lệnh giữ khóa
  tài khoản của mình rồi khóa dòng cài đặt (nhóm 8, cùng thứ tự) → lệnh sau chờ, đếm lại thấy lệnh trước đã commit
  (READ COMMITTED, câu mới) → 422; có test song song.
- 6.2 Gửi lại: PUT có IdempotencyKey (refusableWrite: từ chối không lưu).
- 6.3 Thứ tự khóa: lưu cài đặt khóa tài khoản ưu tiên (carrier_accounts) trước rồi dòng cài đặt; tắt hãng khóa tài
  khoản trước rồi dòng cài đặt → cùng nhóm 8 ADR-46, không ngược chiều; test song song không deadlock.
- 6.4 Lỗi tạm: `WriteTx`/`refusableWrite` (RetryTx) sẵn có; hàm ghi gán lại biến trong thân.
- 6.5 Cách ly: READ COMMITTED + khóa dòng.
- 6.6 Việc nền: không thêm (consumer webcache có sẵn).
- 6.7 429 của hãng: `QuoteNow` đã phân loại; web bỏ hãng lỗi (không thử lại đồng bộ).
- 6.8 Tiến trình chết: báo phí không ghi gì; lưu cài đặt là một transaction.
- 6.9 Treo: mỗi hãng hạn 3 s trong `QuoteNow`; transaction đọc đóng trước khi gọi hãng (test bằng `pg_stat_activity`).
- 6.10 Tiền: phí dự phòng `int64` đồng, CHECK 0–500.000 ở bảng và kiểm ở module.
- 6.11 Quy luật: không vùng rủi ro cao; test ca biên (mọi hãng lỗi, dự phòng/không, ưu tiên).
- 6.12 Dữ liệu cá nhân: đầu vào báo phí chỉ mã tỉnh, phường, khối lượng, tiền — không tên, SĐT, địa chỉ; khóa đệm không
  chứa dữ liệu cá nhân; `Account` có `LogValue` tự che.
- 6.13 Quyền: route nhân viên theo quyền M3 có sẵn (A9, không thêm quyền); route web theo quyền khóa dịch vụ
  `web.shipping.quote`; test 403 cho khóa thiếu quyền.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Ba tài khoản active, hai `show_on_web` (web_sort 2, 1) → đúng hai theo web_sort; tắt qua `UpdateAccount` → không còn | `TestWebCarriers_ShowOnWebOrder` (fulfillment) | `go test -race -count=1 -run '^(TestWebCarriers_ShowOnWebOrder…8 test mới)$' ./internal/modules/fulfillment/` | `ok` (trước khi có mã: FAIL `undefined: fulfillment.WebCarrierSettings`) |
| Website ≤ 60 s: đệm hãng 60 s; ưu tiên → đứng đầu, `preferred: true`; khóa thiếu `web.shipping.quote` → 403 | `TestWebShippingCarriers` (httpapi/web, Valkey thật) | `go test -race -count=1 -run '^(TestWebShippingCarriers…3 test)$' ./internal/httpapi/web/` | `ok … 3.392s` |
| Lưu ưu tiên; ưu tiên không hiện/tạm dừng/không có → `ErrCarrierNotOnWeb`; version cũ → `ErrVersionConflict`; phí ngoài 0–500.000 → `ErrSettingOutOfRange`; sự kiện Seq = version | `TestSaveWebCarrierSettings` | như dòng 1 | `ok` |
| API nhân viên: 403 theo quyền; 422 `carrier_not_on_web`; 409; 422 `setting_out_of_range`; audit `fulfillment.web_carrier_settings.update`; tắt hãng ưu tiên → bỏ ưu tiên; tắt hãng cuối → 422 `last_web_carrier` | `TestWebCarrierSettingsAPI` | `go test -race -count=1 -run '^(TestWebCarrierSettingsAPI…)$' ./internal/httpapi/` | `ok … 3.006s` |
| Báo phí VTP 30.000, Best 28.000 → hai lựa chọn (rẻ trước khi không ưu tiên; ưu tiên đứng đầu dù đắt); VTP treo 5 s → chỉ Best trong < 3,5 s | `TestQuoteForWeb_PreferredAndTimeout` | như dòng 1 | `ok` |
| Mọi hãng lỗi (cả hãng không có adapter như tài khoản manual), không dự phòng → `ErrNoQuote` / HTTP 503 `retry_later`; dự phòng 30.000 → một lựa chọn `estimated` | `TestQuoteForWeb_Fallback`, `TestWebShippingQuote` | dòng 1, dòng 2 | `ok` |
| Gọi lại cùng mức làm tròn → không gọi hãng (đệm 10 phút); lưu cài đặt → version mới trong khóa → gọi lại hãng; 503 không bị đệm; mã tỉnh lạ, khối lượng 0 → 400 | `TestWebShippingQuote` | dòng 2 | `ok` |
| `QuoteForWeb` không gọi hãng khi đang mở transaction (`pg_stat_activity` không có phiên `idle in transaction`) | `TestQuoteForWeb_NoOpenTransaction` | dòng 1 | `ok` |
| Ước thời gian: 25 vận đơn Hà Nội trung vị 2 ngày, 10 Cà Mau, 1 quá 60 ngày → chỉ Hà Nội 2; HTTP rỗng `{"provinces":[]}`, đệm 1 giờ | `TestDeliveryEstimates`, `TestWebDeliveryEstimates` | dòng 1, dòng 2 | `ok` |
| Task 450: tắt hãng cuối → `ErrLastWebCarrier`; tắt hãng ưu tiên → bỏ ưu tiên (version 3, 2 sự kiện); hãng tạm dừng tắt được; hai lệnh tắt hai hãng cuối song song → đúng một qua, không deadlock | `TestUpdateAccount_WebCarrierRules`, `TestUpdateAccount_LastWebCarrierConcurrent` | dòng 1 | `ok` |
| Mã lỗi mới ánh xạ đúng | `TestDomainErrorSnapshot` (3 dòng mới) | dòng 4 | `ok` |
| Câu tiếng Việt cho mã lỗi mới | `console/src/app/errors.test.ts` | `cd console && npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| Cổng tĩnh (gen-check, lint, jscpd, oasdiff, test kiến trúc, console) + test -race gói đổi (httpapi, httpapi/web, fulfillment, webcache) + test gói phụ thuộc | cả bộ của các gói | ci-task của task (cổng kiểm theo CLAUDE.md) | `CI-TASK XANH (so với điểm tách nhánh b4dd070…)`; `ok …/internal/httpapi 30.8s`, `ok …/httpapi/web 19.6s`, `ok …/modules/fulfillment 32.9s`, `ok …/wiring/webcache 5.5s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `SaveWebCarrierSettings` là phương thức `(*Service)` nhận `fulfillment.Actor` (theo bảng đối chiếu Task 439 dòng
  M5-15: module không nhập identity; audit ở lớp HTTP) và trả `WebCarrierSettingsChange` (trước/sau cho audit).
  Thêm `WebCarrierStats` (số vận đơn 30 ngày cho màn M5-25, "Bổ sung Task 450").
- Luật "ít nhất một hãng" không khóa mọi tài khoản theo id tăng dần như ghi chú Task 450: `UpdateAccount` đã giữ khóa
  tài khoản của mình, khóa thêm tài khoản id nhỏ hơn sẽ ngược thứ tự, có thể deadlock. Thay bằng: khóa dòng
  `channel_carrier_settings` (cuối nhóm 8, sau `carrier_accounts`) làm điểm tuần tự rồi đếm tài khoản active hiện trên
  web còn lại (READ COMMITTED: câu đếm thấy lệnh trước đã commit). Có test song song.
- Khóa đệm báo phí thêm tỉnh và COD (làm tròn lên 500.000) cạnh phường, khối lượng, giá trị, version: phí phụ thuộc
  COD; phí trả là phí của mức đã làm tròn. Mã tỉnh/phường giới hạn chữ, số ≤ 10 ký tự trong hợp đồng (phần khóa của
  bộ đệm chỉ nhận `[A-Za-z0-9_-]`).
- `fallbackFee` trong hợp đồng là `$ref Money` (luật `TestMoneyFieldsContract`); trần 500.000 kiểm ở module → 422
  `setting_out_of_range` (mã sẵn có), và CHECK ở bảng.
- `WebCarriers` đặt tài khoản ưu tiên đứng đầu (rồi web_sort), cùng thứ tự với báo phí.
- Tên operation danh sách hãng là `webShippingCarriers` (kế hoạch ghi `webCarriers`; đổi để không trùng nghĩa với
  tài khoản hãng của nhân viên) — M5-31 (website) dùng tên này.
- Ca "lưu cài đặt → sự kiện → đệm đổi thế hệ": khóa đệm báo phí có version nên lần đọc ngay sau khi lưu đã gọi lại hãng
  (test); sự kiện `carrier.web_settings_changed` → bump `SpaceCarriers`, `SpaceShipQuote` đã có test ở webcache
  (`invalidator_test.go`, Task 185); task này kiểm sự kiện được phát (`TestSaveWebCarrierSettings`).

## Quyết định thay chủ shop

1. **Bảng mới `fulfillment.channel_carrier_settings` (0506)** thay vì khóa JSON `fulfillment.web` trong
   `platform.settings`: đúng Interfaces của kế hoạch, ADR-46 nhóm 8 đã chừa chỗ, FK giữ ưu tiên luôn trỏ tài khoản có
   thật. Phương án khác: khóa `platform.settings` (không FK). Đảo ngược: migration thu hẹp xóa bảng.
2. **Khi không hãng nào báo phí được** (hiện là mọi lúc trên production: tích hợp hãng tạm dừng 21/11, tài khoản
   `manual` không có adapter → `rejected`, không gọi mạng): có phí dự phòng → một lựa chọn `estimated` (không hãng);
   không có → 503 `retry_later`. Không đặt phí dự phòng mặc định (QĐ19, QĐ-183-4: chủ shop tự nhập). Phương án khác:
   phí mặc định cứng — bỏ vì đoán giá thay chủ shop.
3. **Tạm dừng/thu hồi hãng bằng `setCarrierAccountStatus` không bị luật "ít nhất một hãng" chặn** (dừng hãng là thao
   tác vận hành khẩn); khi đó website thấy danh sách rỗng và phí dự phòng. Ưu tiên trỏ tài khoản tạm dừng bị bỏ qua khi
   đọc; bật lại thì ưu tiên trở lại.
4. **Số vận đơn 30 ngày theo hãng** đếm vận đơn chưa hủy tạo trong 30 ngày, mọi kênh bán (fulfillment không đọc kênh
   của đơn). Phương án khác: chỉ đơn web (cần view của order) — để M5-25 nếu chủ shop muốn.

## Việc phát hiện thêm

- Trước khi website chuyển sang báo phí của CRM: chủ shop phải nhập phí dự phòng (màn M5-25, Task 208) và bật
  `showOnWeb` cho tài khoản hãng nhập tay; không thì website nhận 503 (QĐ-183-4, nhắc cho M5-31).
- `docs/adr/0046-thu-tu-khoa.md` nhóm 8 còn câu điều kiện "(nếu M5-15 chọn khóa `platform.settings` … không tạo)":
  bảng đã tạo ở 0506, câu đó có thể bỏ (việc tài liệu, không làm ở task này).

## Reviewer

Kết luận: ĐẠT (reviewer, trên phần đã stage sau ci-task xanh).

- Phát hiện phải sửa: Không có.
- Ảnh hưởng dự kiến khớp thực tế; chỗ gọi `UpdateAccount` duy nhất (`handlers_carrier_accounts.go:178`) đi qua
  `refusableWrite(isShipmentRefusal)` nên `ErrLastWebCarrier` → 422 và không lưu theo khóa chống trùng; tên hằng cũ
  của webcache không còn.
- Bảng bằng chứng: mọi dòng trỏ test có thật và kiểm đúng điều tiêu chí nói.
- Ghi chú không bắt buộc (không sửa, ghi lại cho chủ shop): (1) đệm báo phí chỉ đổi theo version cài đặt — tắt
  `show_on_web` một hãng không phải ưu tiên thì báo phí có thể còn hãng đó tối đa 10 phút (đúng kế hoạch; muốn khớp
  hơn thì tăng version ở mọi lần tắt hãng); (2) tên operation `webShippingCarriers` — đã ghi ở Khác kế hoạch; (3) chỗ
  đặt hai dòng trong `errors.ts`; (4) lần lưu bị từ chối có thể để lại dòng cài đặt mặc định (tương đương chưa có dòng).
