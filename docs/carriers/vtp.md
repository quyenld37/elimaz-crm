# Viettel Post (khóa hãng `vtp`)

Task 125 (M3-34): xác thực, địa giới, báo phí, tạo vận đơn. Task 126 (M3-35): hủy, webhook, nhãn, bảng kê. Gọi thật ở
môi trường thử của hãng chỉ ở Task 49, khi chủ shop OK. Tên địa giới đặc biệt: xem `dia-gioi.md`.

## Nguồn và ngày đọc

- Cổng đối tác công khai `https://partner2.viettelpost.vn` (ứng dụng Angular). Trang HTML chỉ có khung "Loading...";
  nội dung tài liệu nằm trong các tệp JS và tệp chữ `assets/i18n/vi.json` của chính trang đó. **Đọc ngày 21/11/2026**
  bằng cách tải các tệp tĩnh đó (không đăng nhập, không gọi API nào của hãng). Trang và tệp đã đọc:
  - `/document/environment-parameter` (môi trường, phong bì trả lời, bảng thông báo lỗi), `/document/token-authen`,
    `/document/new-place-province`, `/document/new-place-ward`, `/document/get-list-service-by-address-id`
    (`getPriceAll`), `/document/charges-id-address` (`getPrice`), `/document/create-order-id-address` (`createOrder`),
    `/document/webhook`; khóa `error-status`, `document.*` của `vi.json`.
- Ghi chú cũ: `docs/nhanh-survey/recheck-20261018/viettel-official-webhook.json` (trang Webhook, đọc 18/10/2026) và
  `docs/crm/specs/core-e2e/05-carrier-integration.md` mục I15. Bảng trạng thái của trang Webhook đọc ngày 21/11 trùng
  với bản 18/10.
- Payload mẫu trong `internal/adapters/carrier/vtp/testdata/` chép từ các trang trên (mỗi tệp có trường `_source`); số
  điện thoại, tên, địa chỉ thay bằng dữ liệu giả.

## Môi trường

| Môi trường tài khoản (`carrier_accounts.environment`) | Base URL |
|---|---|
| `sandbox` | `https://partnerdev.viettelpost.vn` (Development) |
| `production` | `https://partner.viettelpost.vn` (Production) |

`vtp.Config.BaseURL` khác rỗng thì thay cả hai (chỉ test và máy chủ giả `vtptest` dùng). Môi trường khác hai giá trị
trên: adapter trả lỗi lớp `permanent`, mã `unknown_environment`, không gọi mạng.

Mọi API (trừ đăng nhập) gửi header `Token: <token>` và `Content-Type: application/json`. Trả lời có phong bì
`{"status": 200, "error": false, "message": "OK", "data": …}`; lỗi: `error: true`, `message` là chuỗi hay mảng. Riêng
`getPriceAll` trả **mảng trần** (không phong bì) theo mẫu của tài liệu.

## Xác thực (`CheckConnection`, mọi lời gọi)

- `POST /v2/user/Login` thân `{"USERNAME": "…", "PASSWORD": "…"}` → `data.token`, `data.expired` (mẫu: `0`; mẫu của
  `LoginVTP`: mốc mili giây). Khóa của tài khoản trong `Credentials`: `username`, `password`.
- Tài liệu còn hai cách: `POST /v2/user/LoginVTP {"token": …}` (token tạo trên viettelpost.vn, cần OTP) và
  `POST /v2/user/ownerconnect` (ủy quyền tài khoản B bằng token của A; "token dài hạn có thời hạn 1 năm"). M3 chỉ dùng
  `Login` (shop dùng chính tài khoản của mình, Global Constraints). Hai cách kia chưa làm.
- Mã truy cập giữ trong bộ nhớ của tiến trình theo tài khoản (khóa: mã tài khoản CRM, môi trường, băm của bộ khóa); đổi
  bộ khóa thì tự đăng nhập lại. `expired > 0` thì coi là hết hạn trước mốc đó 1 phút. Gặp lỗi token (HTTP 401, mã 202
  hay thông báo "Token invalid…") thì bỏ mã cũ, đăng nhập lại **một lần** rồi gọi lại; lần hai vẫn lỗi → lớp `auth`.
- `CheckConnection` = đăng nhập mới (không dùng mã trong bộ nhớ). Sai tài khoản (mã 204, "Invalid owner account or
  password!") → `Health{OK: false}` và lỗi lớp `auth`.

## Địa giới (`Locations`) — danh mục mới V3 (2 cấp, sau sáp nhập 2025)

- Tỉnh: `GET /v3/categories/listProvinceNew` → `data[]` gồm `PROVINCE_ID` (số), `PROVINCE_CODE`, `PROVINCE_NAME`.
- Phường/xã: `GET /v3/categories/listWardsNew?provinceId=<id>` → `data[]` gồm `WARDS_ID`, `WARDS_NAME` (bảng tham số ghi
  thêm `PROVINCE_ID`, mẫu lại ghi `DISTRICT_ID`; adapter lấy tỉnh cha từ tham số đã gửi, không đọc hai trường này).
- `CarrierCode` = ID dạng số của VTP (chuỗi). Dòng thiếu ID hay tên bị bỏ (mẫu tỉnh của tài liệu có một dòng ghi sai
  tên trường `WPROVINCE_NAME`). Danh mục cũ V2 (3 cấp, có quận/huyện) không dùng.
- Mẫu của trang không có header `Token`; adapter vẫn gửi (cần xác minh ở Task 49 là không bị từ chối).

## Báo phí (`Quote`)

- Không chọn dịch vụ: `POST /v2/order/getPriceAll` thân `SENDER_PROVINCE`, `SENDER_DISTRICT`, `SENDER_WARD`,
  `RECEIVER_PROVINCE`, `RECEIVER_DISTRICT`, `RECEIVER_WARD`, `PRODUCT_TYPE` (`HH` = hàng), `PRODUCT_WEIGHT` (g),
  `PRODUCT_PRICE` (giá trị hàng), `MONEY_COLLECTION` (COD), `PRODUCT_LENGTH/WIDTH/HEIGHT` (cm), `TYPE: 1` (trong
  nước). Trả mảng `MA_DV_CHINH`, `TEN_DICHVU`, `GIA_CUOC` ("tổng cước dịch vụ đã bao gồm VAT, không bao gồm phụ phí"),
  `THOI_GIAN` ("48 giờ"), `EXTRA_SERVICE[]`. `QuoteResult.Total = GIA_CUOC`, `ExpectedDays` = số giờ / 24 làm tròn lên.
- Có dịch vụ: `POST /v2/order/getPrice` (thêm `ORDER_SERVICE`, `ORDER_SERVICE_ADD`, `NATIONAL_TYPE: 1`) → `data`
  gồm `MONEY_TOTAL` (tổng cước), `MONEY_TOTAL_FEE` (cước chính), `MONEY_FEE` (xăng dầu), `MONEY_COLLECTION_FEE` (phí
  thu hộ), `MONEY_OTHER_FEE`, `MONEY_VAS`, `MONEY_VAT`, `KPI_HT` (giờ). `Total = MONEY_TOTAL`; các phần còn lại vào
  `Breakdown`.
- Địa chỉ 2 cấp: gửi `*_DISTRICT: null` như mẫu tạo đơn của tài liệu (mẫu `createOrder` dùng ID V3 với
  `SENDER_DISTRICT: null`). **Cần xác minh ở Task 49:** bảng tham số của hai API giá ghi `*_DISTRICT` bắt buộc; mẫu của
  hai API giá là địa chỉ 3 cấp.
- Mã CRM của tỉnh/phường đổi sang ID VTP qua `Account.Locations`; thiếu một mã (cả nơi gửi) hay mã không phải số → lỗi
  `address_unmapped` (bọc `fulfillment.ErrAddressUnmapped`), không gọi mạng.

## Tạo vận đơn (`CreateShipment`)

`POST /v2/order/createOrder` (địa chỉ ID). Trường gửi:

| Trường VTP | Lấy từ |
|---|---|
| `ORDER_NUMBER` ("Mã đơn hàng" của đối tác; webhook trả lại ở `ORDER_REFERENCE`) | `CreateRequest.Code` (mã vận đơn CRM, ví dụ `E261118ABC123`) |
| `CHECK_UNIQUE: true` ("Nếu mã đơn hàng của request đã tồn tại trên hệ thống, hệ thống sẽ trả về lỗi") | luôn bật |
| `SENDER_FULLNAME/PHONE/ADDRESS/PROVINCE/WARD`, `SENDER_DISTRICT: null` | `Account.Pickup` |
| `RECEIVER_FULLNAME/PHONE/ADDRESS/PROVINCE/WARD`, `RECEIVER_DISTRICT: null` | `CreateRequest.To` |
| `PRODUCT_TYPE: "HH"`, `PRODUCT_WEIGHT`, `PRODUCT_LENGTH/WIDTH/HEIGHT`, `PRODUCT_PRICE`, `PRODUCT_QUANTITY` | `Parcel` |
| `MONEY_COLLECTION` | `CODAmount` |
| `ORDER_PAYMENT`: 1 không thu hộ; 2 thu hộ tiền hàng và cước; 3 thu tiền hàng, không thu cước; 4 chỉ thu cước | `Options.Payer` (shop/receiver) × COD > 0 |
| `ORDER_SERVICE` | `CreateRequest.Service`; rỗng → dịch vụ rẻ nhất của `getPriceAll` (Quyết định trong task 125) |
| `ORDER_SERVICE_ADD: ""`, `ORDER_NOTE`, `GROUPADDRESS_ID: 0`, `CUS_ID: 0` | `Options.Note` |

Trả `data.ORDER_NUMBER` ("Mã vận đơn của Viettelpost, VTP tự sinh") → `TrackingCode`; `data.MONEY_TOTAL` → `Fee`
(`FeeKnown = true`).

## Mã lỗi (`ClassifyError`, trong `errors.go`)

Mã trong thân (`status`), theo khóa `error-status` của tài liệu; HTTP theo thông lệ (tài liệu không nói).

| Nguồn | Nghĩa (tài liệu) | Lớp CRM | Mã `CarrierError.Code` |
|---|---|---|---|
| HTTP 401/403, thân 202, "Token invalid…", "Header Token is required" | lỗi token | `auth` (sau một lần đăng nhập lại) | `vtp_token` |
| Đăng nhập: thân 204, "Invalid owner account or password!" | sai tài khoản/mật khẩu, tài khoản khóa | `auth` | `vtp_login` |
| HTTP 429 | quá tần suất | `rate_limited` (`Retry-After`, mặc định 30 s), chưa gửi | `http_429` |
| HTTP 503, không nối được | chưa nhận | `not_sent_retry` | `http_503`, `dial` |
| Thân 201 "Hủy nhập phiếu gửi", 203 thiếu trường, 204 dữ liệu sai / "Giá không áp dụng cho hành trình này" | hãng từ chối | `rejected` | `vtp_201`, `vtp_203`, `vtp_204` |
| Tạo đơn, thân 206 "Mã đơn hàng đã tồn tại trên hệ thống" | đơn đã có ở VTP, không biết mã hãng | `unknown_outcome` (in_doubt, người nhập mã hãng) | `vtp_206_duplicate` |
| Tạo đơn: thân 205 "Lỗi hệ thống", HTTP 5xx khác, quá giờ, đứt kết nối sau khi gửi, thân không đọc được | không rõ | `unknown_outcome` | `vtp_205`, `http_5xx`, `transport`, `bad_reply` |
| Việc khác (báo phí, địa giới): 205, 5xx | lỗi tạm | `not_sent_retry` | như trên |

Thông báo của hãng không chép vào lỗi (chỉ mã). Thân yêu cầu (tên, SĐT, địa chỉ) không vào log, lỗi, dấu vết.

Hạn giờ client (Global Constraints): báo phí 10 s, tạo vận đơn 20 s, địa giới và đăng nhập 15 s.

## Khả năng (`Capabilities`)

| Khả năng | Giá trị | Lý do |
|---|---|---|
| `Quote`, `Create`, `Locations` | true | có API (trên) |
| `DeclaredValue` | true | `PRODUCT_PRICE` = giá trị hàng trong cả báo phí và tạo đơn |
| `LookupByRef` | **false** | tài liệu không có API tra đơn theo mã đối tác hay mã vận đơn (danh sách API: địa danh, dịch vụ, tính cước, tạo đơn, cập nhật đơn, cập nhật trạng thái, link in). `LookupByCode` trả `ErrNotSupported`: vận đơn "không rõ" luôn chờ người (Task 12) |
| `Inspection`, `FeeOnRefuse` | false | làm qua dịch vụ cộng thêm (`XMG` "Thu tiền xem hàng", `EXTRA_MONEY`) mà mã dịch vụ cộng thêm do `getPriceAll` trả theo từng tài khoản; chưa có mẫu thật để đổi `none/view/try` sang mã. Task 49 xác minh rồi bật |
| `Track` | false | tài liệu không có API hành trình; trạng thái về bằng webhook (Task 126) |
| `Cancel`, `UpdateAddress`, `Redeliver`, `Labels`, `Webhook`, `Statement` | false ở Task 125 | `UpdateOrder` (TYPE 4 hủy, 2 duyệt hoàn, 3 phát tiếp), `edit`, `printing-code`, webhook: Task 126 |

`Options.Pickup` (hãng tới lấy hay shop mang ra bưu cục): tài liệu không có trường tương ứng ở `createOrder`; bỏ qua.

## Bảng trạng thái → `Canonical` (`statuses.go`, mẫu `testdata/statuses.json`)

| Mã | Tên (tài liệu) | Cuối? | `Canonical` |
|---|---|---|---|
| 101 | Viettel Post hủy lấy hàng | Y | `cancelled` |
| 102 | Lấy hàng thất bại | – | `awaiting_pickup` |
| 103 | Điều phối bưu cục lấy hàng | – | `awaiting_pickup` |
| 104 | Lấy hàng điều phối bưu tá | **Y (mâu thuẫn)** | `unknown` |
| 107 | Đối tác yêu cầu hủy (qua API) | Y | `cancelled` |
| 200 | Lấy hàng thành công | – | `picked_up` |
| 201 | Viettel Post hủy đơn hàng | Y | `cancelled` |
| 202 | Sửa phiếu gửi | – | `unknown` (sự kiện sửa, không phải trạng thái hành trình) |
| 300 | Khai thác đi | – | `in_transit` |
| 400 | Khai thác đến | – | `in_transit` |
| 500 | Giao bưu tá đi phát | – | `out_for_delivery` |
| 501 | Thành công – Phát thành công | Y | `delivered` |
| 503 | Tiêu hủy – Theo yêu cầu khách hàng | Y | `unknown` (không rõ hàng bị hủy bỏ hay hủy đơn; xem câu hỏi) |
| 504 | Thành công – Chuyển trả người gửi | Y | `returned_to_shop` |
| 505 | Yêu cầu chuyển hoàn (phát thất bại, chờ shop duyệt hoàn hay phát tiếp) | – | `delivery_failed` |
| 506 | Phát thất bại | – | `delivery_failed` |
| 507 | Khách hàng đến bưu cục nhận (phát thất bại) | – | `delivery_failed` |
| 508 | Phát tiếp (đơn vị yêu cầu) | – | `in_transit` |
| 509 | Chuyển tiếp bưu cục khác | – | `in_transit` |
| 515 | Duyệt hoàn | – | `returning` |
| 550 | Phát tiếp (khách yêu cầu) | – | `in_transit` |

Mã khác → `unknown` (không đổi trạng thái vận đơn, chờ người). `vi.json` còn một danh sách tên trạng thái không có mã
("Bưu tá đã nhận hàng", "Chuyển hoàn bưu cục gốc", "Tồn - …"): chưa đổi vì không có mã.

## Câu hỏi gửi Viettel Post (chưa có trả lời; điểm tài liệu mâu thuẫn — không hard-code)

1. **Mã 104**: bảng trạng thái ghi là trạng thái cuối, nhưng ghi chú "trạng thái cuối 101/107/201/501/503/504" không có
   104, và mẫu webhook dùng 104 với tên "Giao cho Bưu tá đi nhận" (đang đi lấy). 104 có phải trạng thái cuối không?
2. **Mã 503** "Tiêu hủy/Hủy – theo yêu cầu khách hàng": hàng bị tiêu hủy (mất) hay đơn bị hủy trước khi giao? Tiền
   COD, phí xử lý thế nào?
3. Có API tra đơn theo `ORDER_NUMBER` của đối tác (hay theo mã vận đơn) không? Không có thì khi tạo đơn quá giờ, CRM
   chỉ biết đơn đã tồn tại qua lỗi 206 mà không biết mã vận đơn.
4. Trả lời lỗi đến bằng HTTP 200 kèm `status` 20x trong thân, hay bằng mã HTTP khác? Có giới hạn tần suất không, trả
   429 kèm `Retry-After` không?
5. Địa chỉ 2 cấp ở `getPrice`/`getPriceAll`: gửi `*_DISTRICT` là `null`, `0` hay bỏ trường?
6. Mã dịch vụ cộng thêm cho "cho xem hàng", "cho thử", và thu phí khi khách không nhận (`XMG` + `EXTRA_MONEY`?).
7. Token của `Login` có hạn bao lâu (mẫu `expired: 0`)?

## Webhook (Task 126)

Ghi chú từ trang Webhook: POST JSON, header `Authorization` là token xác thực, thân `{"DATA": {...}, "TOKEN": "…"}`;
một URL mỗi tài khoản; trả lời dưới 1 s; thử lại tối đa 5 lần; có thể trùng hay thừa; `ORDER_NUMBER` là mã vận đơn,
`ORDER_REFERENCE` là mã đối tác; thời điểm `ORDER_STATUSDATE` dạng `dd/MM/yyyy HH:mm:ss` (giờ Việt Nam).
