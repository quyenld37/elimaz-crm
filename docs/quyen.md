# Bảng quyền của CRM

Bảng chuẩn duy nhất của mọi khóa quyền (kế hoạch M1, A9). Mã: `identity.Permissions` (loại phạm vi) và
`identity.DefaultRoles` (vai trò mẫu nhận) trong `internal/modules/identity`. Test `TestPermissionsDoc_MatchesCode`
đòi bảng này và mã khớp nhau từng dòng.

Luật cho mốc sau: chỉ **thêm** dòng (và thêm quyền vào `Permissions`, `DefaultRoles`); không khai lại quyền đã có,
không đổi loại phạm vi của quyền đã có (ngoại lệ đã duyệt: Task 315 lượt 2 chuyển sáu quyền quản trị sang `toàn shop`).
Quyền mới ghi chính sách áp cho cả shop (cài đặt shop, giá chung, mã giảm giá, phí ship, kết nối kênh…) hay quản
trị nhân viên, vai trò, nền tảng là loại `toàn shop`.

- **Loại phạm vi** `none`: quyền không gắn với đích; có hiệu lực toàn shop khi nhân viên có quyền này trong **bất kỳ**
  gán vai trò nào (gán theo một kho, một Page cũng được; Task 315). `theo đích: <loại>`: grant `all` hoặc grant đúng
  đích (một Page, một kho) cho quyền trên đích đó. Phạm vi chỉ thu hẹp quyền theo đích.
- **Loại phạm vi** `toàn shop` (quyền quản trị, Task 315 lượt 2): chỉ có hiệu lực từ gán vai trò **toàn shop**
  (`all`). Gán vai trò có quyền này cho một kho hay một Page vẫn lưu được (không lỗi), nhưng quyền quản trị của gán đó
  không có hiệu lực; các quyền khác của vai trò vẫn có. API trả kèm cảnh báo `admin_needs_shop_scope` ("Quyền quản
  trị chỉ hiệu lực khi gán toàn shop") ở `StaffMember.warnings` (danh sách nhân viên, sau khi gán) và `Me.warnings`;
  `Me.grants` chỉ liệt kê grant có hiệu lực. Ví dụ: Chủ gán theo kho A đọc sản phẩm, ghi phiếu kho A, nhưng không
  quản lý nhân viên, không xem nhật ký, không vào mục nền tảng.
- Quyền quản trị (loại `toàn shop`) hiện có: `identity.users.manage`, `identity.roles.manage`, `platform.jobs.manage`,
  `platform.health.read`, `platform.files.manage`, `audit.read`; chính sách toàn shop của M2: `order.settings.manage`,
  `promotion.manage`; của M3: `fulfillment.carrier_accounts.manage`, `fulfillment.settings.manage`; của M5:
  `identity.service_keys.manage`, `order.web_intake.manage`, `content.read`, `content.manage`; của M6: `report.export`,
  `report.exports.manage`, `customer.export` (xuất, tệp xuất của người khác, danh sách khách: Q13, Q14 của kế hoạch M6).
- **Quyền `web.*` chỉ khóa dịch vụ giữ** (Task 189, ADR-18): khóa của website (`ek_…`, header `Authorization`) chỉ mang
  các quyền `web.*` được cấp lúc tạo (mã: `identity.ServiceKeyPermissions`, không thuộc `identity.Permissions`); không
  vai trò nhân viên nào nhận được chúng (vai trò tự định nghĩa → `unknown_permission`), và khóa không nhận quyền nào
  ngoài `web.*` (`permission_not_allowed_for_key`). Route `/api/v1/web/*` chỉ nhận khóa, route của nhân viên không nhận khóa.
- **Lên đơn giữ hàng theo phạm vi kho** (Task 411, quyết định 19/11 cho phát hiện D-02): `order.create` vẫn là loại
  `none` (vào màn Lên đơn, kiểm mã giảm giá, kiểm đơn trùng như cũ), nhưng lúc tạo đơn (`POST /api/v1/orders`) kho giữ
  hàng của đơn — `warehouseId` gửi lên, bỏ trống là kho chính — phải nằm trong phạm vi của chính quyền `order.create`:
  gán vai trò **toàn shop** hoặc gán **theo đúng kho đó**. Ngoài phạm vi (gán theo kho khác, chỉ gán theo một Page, kho
  không có) → 403 `forbidden`, không tạo đơn, không giữ hàng; yêu cầu không lưu theo `Idempotency-Key` (được cấp kho
  rồi gửi lại cùng khóa thì xử lý lại). Chủ shop, Quản lý gán toàn shop không bị ảnh hưởng. Mã: `identity.GrantedAt`.
- **Phiếu đổi hàng cần quyền lên đơn ở kho của đơn gốc** (Task 436, quyết định 21/11 cho phát hiện B-06 của review
  độc lập M2): phiếu đổi (`POST /api/v1/orders/{id}/returns`, `kind: exchange`) tạo một đơn thay thế giữ hàng ở kho của
  đơn gốc (không chọn kho) và có thể thẳng `confirmed`. Ngoài `order.return` của route, người tạo phải có `order.create`
  ở kho đó (gán toàn shop hoặc theo đúng kho), như luật lên đơn ở trên. Không có → 403 `forbidden`, không phiếu, không
  giữ hàng, không lưu theo `Idempotency-Key`. Không đòi thêm `order.confirm`: tạo đơn `confirmed` qua `POST
  /api/v1/orders` cũng chỉ cần `order.create`. Phiếu khách trả hàng (`customer_return`, không tạo đơn) chỉ cần
  `order.return`. Màn Đơn khóa lựa chọn "Đổi hàng" khi nhân viên không có `order.create` ở gán nào. Mã:
  `exchangeWarehouseAllowed` (`internal/httpapi/handlers_order_returns.go`).
- `GET /api/v1/me` không cần quyền nào: chỉ cần đăng nhập và tài khoản không bị khóa (`x-authenticated`, Task 315).
- **Vai trò mẫu**: `owner` Chủ, `manager` Quản lý, `sale` Bán hàng, `warehouse` Kho, `accountant` Kế toán. Vai trò
  chủ shop đã sửa (`customized`) không được thêm quyền tự động, trừ `owner` luôn đủ mọi quyền. Mỗi lần `migrate` xong,
  CRM đồng bộ vai trò mẫu cho mọi shop (`identity.SyncDefaultRoles`): shop chưa có vai trò mẫu nhận đủ; quyền mới thêm
  vào bảng này vào các vai trò mẫu chưa sửa; vai trò đã sửa và vai trò tự định nghĩa không đổi.
- Gán vai trò cho một nhân viên: người gán phải có mọi quyền của vai trò đó trên đúng phạm vi gán (quyền `none`: người
  gán có nó ở bất kỳ grant nào, vì nó có hiệu lực toàn shop với cả hai bên; quyền `toàn shop`: người gán có nó ở gán
  toàn shop, dù gán theo kho); không ai sửa quyền hay khóa chính mình;
  không khóa hay gỡ `owner` của Chủ còn hoạt động cuối cùng (PQ07).

## Vai trò tự định nghĩa

Task M1-56 (Q35), quyền `identity.roles.manage`; màn "Vai trò" của bàn điều khiển.

- Tạo vai trò từ danh sách quyền có sẵn (`GET /api/v1/identity/permissions`, nhãn tiếng Việt ở
  `identity.PermissionCatalog`); khóa tự sinh `custom_` + 8 ký tự; tên không trùng (không phân biệt hoa thường, kể cả
  vai trò đã lưu trữ). Phạm vi (toàn shop, một kho, một Page) chọn khi gán vai trò cho người ở màn Nhân viên.
- Mỗi quyền **thêm vào hay gỡ khỏi** một vai trò, người sửa phải có quyền đó ở phạm vi toàn shop (`all`); vì vậy không
  ai tự nâng quyền bằng cách sửa vai trò mình đang giữ. Quyền lạ → `unknown_permission`.
- Vai trò `owner` (Chủ) không sửa, không lưu trữ được (`owner_role_locked`); luôn đủ mọi quyền.
- Sửa một vai trò mẫu đặt `customized`: bản cập nhật sau (`SyncDefaultRoles`) không thêm lại quyền đã gỡ.
- Lưu trữ khi không còn nhân viên đang hoạt động nào giữ (`role_in_use`). Lưu trữ là gỡ mọi quyền của vai trò, nên
  người lưu trữ cũng phải có mọi quyền đó ở toàn shop. Vai trò đã lưu trữ không gán mới được (`role_archived`), không
  sửa được, và **không cho quyền** (người bị khóa còn giữ nó, được mở lại, không lấy lại quyền); gỡ gán cũ vẫn được.
- Sửa, lưu trữ mang `version` (`version_conflict`); mỗi lần ghi có một dòng audit `identity.role.*` với danh sách quyền
  trước và sau.

| Khóa | Loại phạm vi | Mốc đặt ra | Vai trò mẫu nhận | Việc |
|---|---|---|---|---|
| `realtime.subscribe` | none | M0 | owner, manager, sale, warehouse, accountant | Mở luồng tin báo thời gian thực |
| `audit.read` | toàn shop | M0 | owner, manager | Xem nhật ký thao tác |
| `platform.jobs.manage` | toàn shop | M0 | owner | Xem, chạy lại, bỏ việc nền lỗi |
| `platform.health.read` | toàn shop | M0 | owner | Xem tình trạng hệ thống |
| `identity.users.manage` | toàn shop | M0 | owner | Quản lý nhân viên: tạo, gán vai trò, khóa, mở |
| `conversation.read` | theo đích: page | M0 | owner, manager, sale | Đọc hội thoại của Page được giao (Bán hàng nhận từ M4) |
| `catalog.read` | none | M1 | owner, manager, sale, warehouse, accountant | Xem sản phẩm, biến thể, giá |
| `catalog.manage` | none | M1 | owner, manager | Tạo, sửa, ngừng bán sản phẩm; danh mục, thuộc tính, nhãn |
| `catalog.import` | none | M1 | owner, manager | Nhập sản phẩm bằng tệp Excel |
| `order.create` | none | M1 | owner, manager, sale | Tạo đơn |
| `order.read` | none | M1 | owner, manager, sale, warehouse, accountant | Xem đơn (Kho nhận từ M2) |
| `files.upload` | none | M1 | owner, manager, sale, accountant | Tải ảnh, tệp lên kho tệp (Kế toán nhận từ M3: tải bảng kê COD; Bán hàng nhận từ M4: gửi ảnh cho khách) |
| `platform.files.manage` | toàn shop | M1 | owner | Quản trị kho tệp (khôi phục, tình trạng bản sao) |
| `inventory.suppliers.manage` | none | M1 | owner, manager, warehouse | Quản lý danh bạ nhà cung cấp |
| `identity.roles.manage` | toàn shop | M1 | owner | Tạo, sửa, lưu trữ vai trò tự định nghĩa (Task M1-56) |
| `inventory.read` | theo đích: warehouse | M1 | owner, manager, sale, warehouse, accountant | Xem tồn, sổ kho của kho được giao |
| `inventory.documents.create` | theo đích: warehouse | M1 | owner, manager, warehouse | Soạn nháp phiếu nhập, xuất, hàng lỗi |
| `inventory.documents.post` | theo đích: warehouse | M1 | owner, manager | Ghi sổ phiếu kho |
| `inventory.stocktake` | theo đích: warehouse | M1 | owner, manager, warehouse | Đếm kiểm kho |
| `inventory.stocktake.apply` | theo đích: warehouse | M1 | owner, manager | Áp chênh lệch kiểm kho vào sổ |
| `fulfillment.read` | theo đích: warehouse | M1 | owner, manager, sale, warehouse, accountant | Xem vận đơn của kho được giao |
| `fulfillment.ship` | theo đích: warehouse | M1 | owner, manager, sale, warehouse | Tạo, gửi vận đơn từ kho được giao |
| `customer.read` | none | M2 | owner, manager, sale, warehouse, accountant | Xem, tìm khách, hồ sơ, dòng thời gian |
| `customer.write` | none | M2 | owner, manager, sale | Tạo, sửa hồ sơ khách, danh tính, địa chỉ, ghi chú, gắn nhãn |
| `customer.merge` | none | M2 | owner, manager | Gộp hai khách, hoàn tác gộp |
| `customer.import` | none | M2 | owner, manager | Nhập khách bằng tệp Excel |
| `customer.tags.manage` | none | M2 | owner, manager | Tạo, sửa nhãn khách và quy tắc nhãn |
| `order.update` | none | M2 | owner, manager, sale | Sửa đơn (kiểm phiên bản), thêm ghi chú đơn |
| `order.confirm` | none | M2 | owner, manager, sale | Xác nhận đơn, chuyển chờ khách, chốt đơn nháp |
| `order.cancel` | none | M2 | owner, manager, sale | Hủy đơn, xử lý yêu cầu hủy |
| `order.discount.approve` | none | M2 | owner, manager | Duyệt đơn giảm tay vượt ngưỡng; tự lên đơn giảm vượt ngưỡng không phải chờ |
| `order.return` | none | M2 | owner, manager, sale | Tạo phiếu trả hàng, đổi hàng (M3 dùng lại); đổi hàng cần thêm `order.create` ở kho của đơn gốc |
| `order.return.receive` | none | M2 | owner, manager, warehouse | Nhận hàng trả về kho, ghi tình trạng từng dòng (M3 dùng lại) |
| `order.duplicates.resolve` | none | M2 | owner, manager | Xử lý cặp đơn có thể trùng |
| `order.settings.manage` | toàn shop | M2 | owner, manager | Cài đặt đơn của shop (ngưỡng duyệt giảm giá…), nguồn đơn |
| `promotion.read` | none | M2 | owner, manager, sale, accountant | Xem mã giảm giá, đợt mã (M5 dùng lại) |
| `promotion.manage` | toàn shop | M2 | owner, manager | Tạo, sửa, ngừng mã giảm giá và đợt mã |
| `fulfillment.carrier_accounts.read` | none | M3 | owner, manager, warehouse | Xem kết nối hãng vận chuyển (tài khoản hãng, trạng thái kết nối; không bao giờ thấy khóa API) |
| `fulfillment.carrier_accounts.manage` | toàn shop | M3 | owner, manager | Thêm, sửa, tắt tài khoản hãng, nhập khóa API, kiểm kết nối, địa chỉ lấy hàng của kho, đồng bộ địa giới |
| `fulfillment.settings.manage` | toàn shop | M3 | owner, manager | Cài đặt vận chuyển của shop (ngưỡng đơn kẹt, chu kỳ tra, hạn gọi lại, mức khách hay hoàn…) |
| `fulfillment.quote` | theo đích: warehouse | M3 | owner, manager, sale, warehouse | So phí các hãng cho đơn giao từ kho được giao |
| `fulfillment.cancel` | theo đích: warehouse | M3 | owner, manager, sale, warehouse | Hủy vận đơn ở hãng, đổi hãng (hủy đơn vẫn qua `order.cancel`) |
| `fulfillment.print` | theo đích: warehouse | M3 | owner, manager, warehouse | In tem theo lô, phiếu nhặt hàng của kho được giao |
| `fulfillment.pack` | theo đích: warehouse | M3 | owner, manager, warehouse | Đánh dấu đơn đã đóng gói |
| `fulfillment.manual_status` | theo đích: warehouse | M3 | owner, manager, warehouse | Cập nhật tay trạng thái vận đơn (hãng tự giao, nhập mã tay) |
| `fulfillment.correct` | none | M3 | owner, manager | Sửa sai vận đơn (đảo "Thành công"), giao một phần, xử lý vận đơn "Cần xem" |
| `fulfillment.callbacks.work` | none | M3 | owner, manager, sale | Làm danh sách gọi lại khách giao thất bại: ghi kết quả, nhận việc |
| `inventory.aged.read` | theo đích: warehouse | M3 | owner, manager, warehouse | Xem hàng giữ quá hạn của kho được giao |
| `cash.cod.read` | none | M3 | owner, manager, accountant | Xem tiền COD theo vận đơn, bảng kê, lệch đối soát, quá hạn, khoản phải hoàn |
| `cash.statements.import` | none | M3 | owner, accountant | Nhập bảng kê COD của hãng |
| `cash.statements.confirm` | none | M3 | owner, accountant | Chốt bảng kê COD (ghi tiền về sổ) |
| `cash.adjust` | none | M3 | owner, accountant | Điều chỉnh sổ COD có xem trước và lý do |
| `cash.payments.record` | none | M3 | owner, manager, sale, accountant | Ghi phiếu thu, cọc, chuyển khoản trên đơn |
| `cash.refunds.record` | none | M3 | owner, accountant | Ghi phiếu chi trả khoản hoàn tiền cho khách |
| `cash.refunds.waive` | none | M3 | owner | Đóng khoản hoàn "không hoàn" kèm lý do (khách đồng ý mất cọc) |
| `identity.service_keys.manage` | toàn shop | M5 | owner | Tạo, xem, thu hồi khóa dịch vụ của website (khóa hiện một lần) |
| `order.web_intake.manage` | toàn shop | M5 | owner, manager | Tạm dừng (kèm lý do), mở lại nhận đơn web; xem trạng thái dùng `order.read` |
| `order.requests.read` | none | M5 | owner, manager, sale | Xem yêu cầu về đơn của khách (hủy, đổi, trả, câu hỏi), ảnh kèm (link có hạn); nhận báo hiệu yêu cầu mới |
| `order.requests.manage` | none | M5 | owner, manager, sale | Trả lời khách, ghi chú nội bộ, đổi trạng thái yêu cầu về đơn |
| `promotion.birthday.manage` | toàn shop | M5 | owner, manager | Cài đặt quà sinh nhật, Tạo mã ngay, đánh dấu Đã nhắn, xuất danh sách khách sinh nhật (xem dùng `promotion.read`) |
| `content.read` | toàn shop | M5 | owner, manager | Xem banner trang chủ website, trạng thái hiện/ẩn, xem trước (Task 453) |
| `content.manage` | toàn shop | M5 | owner, manager | Sửa, sắp xếp, bật/tắt banner trang chủ website; tải ảnh banner dùng thêm `files.upload` (Task 453) |
| `customer.feedback.read` | toàn shop | M5 | owner, manager | Xem góp ý của khách (cả nội dung, liên lạc) và khối góp ý trong hồ sơ khách (Task 455) |
| `customer.feedback.manage` | toàn shop | M5 | owner, manager | Đổi trạng thái góp ý (mới, đã xem, đã trả lời), ghi chú nội bộ (Task 455) |
| `channel.manage` | toàn shop | M4 | owner, manager | Kết nối, ngắt kết nối Page, đổi chế độ gửi (chỉ xem / gửi thật), bật trả lời "nhân viên", kiểm sức khỏe kênh (Task 143) |
| `conversation.reply` | theo đích: page | M4 | owner, manager, sale | Trả lời tin, comment, nhắn riêng, ẩn comment, ghi chú, nhãn, nhận hội thoại, nhắc việc, ghi cuộc gọi trên Page được giao |
| `conversation.assign` | theo đích: page | M4 | owner, manager | Giao hội thoại cho nhân viên khác trên Page được giao |
| `conversation.settings` | theo đích: page | M4 | owner, manager | Cài đặt hộp chat của Page: nhãn, mẫu trả lời, quy tắc tự động, khung giờ live, xoay vòng |
| `live.operate` | theo đích: page | M4 | owner, manager, sale | Theo dõi, kết thúc phiên Live; chốt đơn từ comment Live, nhắc khách thiếu địa chỉ (cần thêm `order.create`) |
| `report.sales.read` | theo đích: page | M6 | owner, manager, sale, accountant | Xem báo cáo bán hàng (tổng quan, đơn, hãng, bán chạy) và lần chạy nền của mình; đơn không gắn Page chỉ người gán toàn shop thấy (Task 225) |
| `report.inventory.read` | theo đích: warehouse | M6 | owner, manager, warehouse | Xem báo cáo tồn hôm nay, tồn ngày đã qua, xuất nhập tồn của kho được giao (Task 225) |
| `report.export` | toàn shop | M6 | owner, manager, accountant | Xuất báo cáo bán hàng, tồn kho, danh sách sản phẩm ra Excel, tải tệp của mình (Task 225) |
| `report.exports.manage` | toàn shop | M6 | owner, manager | Xem, tải lần chạy và tệp xuất của mọi nhân viên (Task 225) |
| `customer.export` | toàn shop | M6 | owner, manager | Xuất danh sách khách ra Excel (có SĐT; mỗi lần tạo, tải ghi nhật ký; tệp giữ 24 giờ) (Task 225) |
| `web.catalog.read` | none | M5 | | Khóa dịch vụ: đọc sản phẩm, giá, tồn cho website |
| `web.orders.create` | none | M5 | | Khóa dịch vụ: tạo đơn web |
| `web.orders.read` | none | M5 | | Khóa dịch vụ: tra đơn kênh web theo mã đơn web |
| `web.orders.cancel` | none | M5 | | Khóa dịch vụ: khách tự hủy đơn web |
| `web.requests.write` | none | M5 | | Khóa dịch vụ: gửi yêu cầu về đơn (đổi, trả, câu hỏi) |
| `web.coupons.issue` | none | M5 | | Khóa dịch vụ: phát mã giảm giá của website; đọc quà sinh nhật cần gửi (outbox); báo quà sinh nhật gửi lỗi (`webBirthdayGiftFailed`: lỗi 1, 2 rời outbox 6 giờ, lần 3 thôi tự gửi; Task 491) |
| `web.birthdays.write` | none | M5 | | Khóa dịch vụ: gửi đăng ký sinh nhật, dừng nhận tin sinh nhật của khách; nhập hồ sơ sinh nhật cũ của website giữ mã dừng, lúc đăng ký, cờ đơn cũ `legacyDelivered` (`webImportBirthdays`; Task 491) |
| `web.feedback.write` | none | M5 | | Khóa dịch vụ: gửi góp ý của khách từ form /gop-y (Task 455) |
| `web.shipping.quote` | none | M5 | | Khóa dịch vụ: báo phí ship cho giỏ hàng |
| `web.flash.buy` | none | M5 | | Khóa dịch vụ: bấm mua flash sale |
