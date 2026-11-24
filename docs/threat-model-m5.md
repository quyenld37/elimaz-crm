# Threat model M5 — nối website, bộ đệm và giới hạn tần suất, flash sale

Bản đầu viết ở Task M5-00 (số toàn cục 183, 21/11/2026), trước khi có mã M5, theo mẫu `docs/threat-model-m3.md`. Đây là
bước 2 của "Cách làm việc ở mỗi mốc" (spec 08). Task M5-38 (221) hoàn thiện: điền tên test có thật trong mã vào cột
"Test", đổi dòng **Chưa có** thành test hoặc ghi vì sao chấp nhận.

Chỉ phủ phần mới của M5; ranh giới của M0 (Access, `ingest`, tiến trình → PostgreSQL), M1 (kho tệp, imager), M2 (đơn,
khách, mã giảm), M3 (hãng, webhook, COD) ở `docs/threat-model-m{0,1,2,3}.md`. Chữ ký thật của hàm nhắc tới ở
`docs/m5/doi-chieu-gia-dinh.md`.

Nguồn: kế hoạch `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (QĐ10, 11, 12, 14, 21, 22, 23,
25; Global Constraints; Review Focus), spec `04-kien-truc.md` mục 7, 11, 12, 15, 17; ADR-18, 39, 40, 41, 45, 46, 47. Cột
"Task" ghi số nội bộ M5 (số toàn cục = 183 + NN). Tên test là tên dự kiến, M5-38 sửa theo mã thật. Cột "Đợt": **1** =
website qua CRM (làm trước), **2** = flash sale (để sau, chờ chủ shop xác nhận; `docs/m5/tieu-chi-nghiem-thu.md`).

STRIDE: **S** giả danh, **T** sửa trộm, **R** chối bỏ, **I** lộ thông tin, **D** làm ngừng dịch vụ, **E** nâng quyền.

## Sơ đồ dữ liệu (phần mới)

```
 Trình duyệt khách ──HTTPS──▶ (B2) website elimaz.com.vn (Next.js, bản sao cục bộ SQLite, hàng đợi đơn cục bộ)
   xem, giỏ, đặt hàng,          │  chỉ máy website giữ CRM_SERVICE_KEY; trình duyệt không bao giờ thấy khóa
   tra đơn, yêu cầu + ảnh,      │
   bấm mua flash (đợt 2)        ▼
                          (B1) web-api.<tên miền> (Cloudflare: WAF chỉ cho IP máy website; KHÔNG qua Access)
                                │  Authorization: Bearer ek_<tiền tố 12>_<bí mật>
                                ▼
                     crm-api  /api/v1/web/*  ── khóa dịch vụ: tenant_routes(kind='service_key', tiền tố) → tenant
                                │             → SHA-256 so hằng thời gian → CIDR của khóa → quyền web.* → giới hạn tần suất
                                │             → (đợt 2) cổng nhận vào flash: cờ hết suất, vé/giây, số đang xử lý
                                ├──▶ (B3) Valkey: valkey-cache (bộ đệm đọc), valkey-gate (đếm, cờ, vé) — mạng nội bộ, ACL
                                │         khóa không chứa SĐT/IP (HMAC CACHE_KEY_SECRET); không lệnh Valkey trong RetryTx
                                ├──▶ PostgreSQL (nguồn sự thật): đơn web, yêu cầu về đơn, mã quà, lượt mua flash
                                └──▶ (B5) ảnh khách gửi: FileStore.Put(BucketTmp, TmpKey) ngoài tx
                                          → files.RegisterPrivateImage → lệnh file.process_private (imager)
                                          → biến thể chỉ ở BucketPrivate; tệp không phải ảnh → rejected
 (B4) Nhân viên ──(Access)──▶ crm-api: Khóa dịch vụ, Yêu cầu về đơn hàng, Hãng trên website, Quà sinh nhật,
                                     (đợt 2) Đợt flash sale  — x-permission, WriteTx, audit
```

Dữ liệu nhạy cảm mới: khóa dịch vụ của website (bí mật, chỉ hiện một lần); SĐT, tên, địa chỉ khách gửi kèm đơn web và
yêu cầu về đơn (dữ liệu cá nhân); ảnh khách gửi (riêng tư); mã quà sinh nhật (giá trị tiền); số suất và giá flash.

## (B1) Máy website → `web-api` (khóa dịch vụ) — đợt 1

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test | Đợt |
|---|---|---|---|---|---|
| S | Đoán hoặc dò khóa dịch vụ | 32 byte ngẫu nhiên; tìm tenant theo tiền tố qua `tenant_routes`, so SHA-256 bằng `subtle.ConstantTimeCompare`; khóa sai, tiền tố lạ, khóa đã thu hồi cùng một câu trả lời 401 không phân biệt; giới hạn tần suất theo IP trước bước kiểm khóa | 06, 03 | `TestServiceKey_ConstantTimeAndUniform401` (06) | 1 |
| S | Khóa lộ được dùng từ máy khác | Danh sách CIDR gắn với khóa (CRM kiểm, ngoài luật WAF của Cloudflare); ngoài CIDR → 401 | 06 | `TestServiceKey_CIDRRejected` (06) | 1 |
| E | Khóa dịch vụ gọi route của nhân viên, hoặc phiên nhân viên gọi `/api/v1/web/*` | `x-auth: service_key` chỉ trên path `/api/v1/web/*` (luật 6 `.claude/rules/openapi.md`, `routegen` kiểm); khóa chỉ mang quyền `web.*` (`identity.ServiceKeyPermissions`); `AuthUser.Kind` phân biệt nhân viên/khóa | 05, 06 | `TestRoutegen_ServiceKeyOnlyUnderWeb` (05), `TestServiceKey_StaffRoute403` (06) | 1 |
| I | Khóa nằm trong database, log, audit, dấu vết, `idempotency_keys` | Chỉ lưu SHA-256; `createServiceKey` khai `x-idempotency: no-replay` (thân không lưu, gửi lại → 409 `already_processed`); kiểu khóa có `LogValue()` che; audit chỉ ghi tiền tố | 06 | `TestCreateServiceKey_NoReplayNotStored`, `TestServiceKey_NotInLogsAuditSpans` (06) | 1 |
| I | Tra đơn của người khác, đơn kênh khác (WB03) | Chỉ đơn `channel = 'web'` theo đúng `external_ref`; câu trả lời không có địa chỉ, SĐT, ghi chú nội bộ (QĐ25); mã lạ → 404 | 13 | `TestGetWebOrder_OnlyOwnWeb`, `TestWebGetOrder_HTTP`, `TestWebGetOrder_Shipment` (13) | 1 |
| T | Sửa giá, tiền giảm, phí ship trong thân yêu cầu (WB13) | CRM tính lại giá từ `VariantsForSale`, mã từ `promotion.Check/Redeem`, phí từ cài đặt; số của website chỉ để so; giảm chiến dịch bị chặn 1–90% và ≤ giá dòng | 09, 10, 11 | `TestCreateFromWeb_IgnoresClientPrices` (10), `TestWebOrder_TamperedTotals` (11) | 1 |
| T | Gửi lại đơn, bấm đúp, hàng đợi cục bộ gửi lại (WB07, WB14) | `Idempotency-Key` bắt buộc; `UNIQUE (tenant_id, seller_id, channel, external_ref)` | 11, 29 | `TestWebOrder_Parallel50SameKeyOneOrder` (11) | 1 |
| R | Website chối đã gửi đơn, đã tạo yêu cầu, đã phát mã | Audit với người làm `service` + `ServiceKeyID` + `request_id` trong cùng transaction | 10, 12, 14 | `TestCreateFromWeb_AuditServiceActor` (10) | 1 |
| D | Website (hay khóa bị lộ) dội yêu cầu | Giới hạn 1.200 yêu cầu/phút mỗi khóa, 20 đơn web/phút mỗi khách (QĐ23); `x-body-limit` mặc định 1 MiB; báo giá và báo phí là `POST` `exempt` không ghi gì | 03, 05, 09 | `TestRateLimit_ServiceKeyPerMinute` (03), `TestBodyLimit_413` (05) | 1 |
| E | Thu hồi khóa mà khóa vẫn dùng được lâu | Báo hiệu thu hồi ≤ 1 s, đệm trong tiến trình tối đa 5 s | 06 | `TestServiceKey_RevokeWithin5s` (06) | 1 |
| I | Mã quà sinh nhật bị dò | Mã sinh bằng `promotion.NewCode` (8 ký tự ngẫu nhiên từ bảng 32 chữ, `crypto/rand`, ≈ 1,1 × 10^12 khả năng); `Check` sai mã → `not_found` chung; giới hạn tần suất theo khách và IP | 14, 03 | `TestWebCoupon_GuessRateLimited` (14) | 1 |

## (B2) Trình duyệt khách → website → lượt mua flash sale — đợt 2

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test | Đợt |
|---|---|---|---|---|---|
| D | Lượt mua dồn làm tê liệt `api`, database (FS03, FS04) | Bốn lớp: (1) cờ hết suất ở Valkey trả ngay; (2) giới hạn tần suất theo khách, IP; (3) vé mỗi giây theo món và theo shop; (4) tối đa 64 lượt đang xử lý mỗi tiến trình; transaction `TxFlashBuy` (200 ms khóa, 2 s câu lệnh), `SKIP LOCKED` trên đơn vị | 04, 18, 20 | `TestFlashBuy_FourLayers` (20), k6 `flash_spike.js` (36) | 2 |
| T | Một khách mua vượt giới hạn bằng 100 yêu cầu song song, nhiều khóa chống trùng (FS05) | `flash_sale_claims.quantity ≤ per_customer_limit` kiểm trong transaction (khóa dòng claim trước); giới hạn tần suất là lớp ngoài, không phải chốt | 18, 20 | `TestBuyFlash_PerCustomerLimitParallel` (18) | 2 |
| S | Nhập SĐT người khác để mua quá giới hạn (FS18) | Rủi ro biết trước theo QĐ4 (không OTP); thêm giới hạn theo IP; ghi lại để xem sau đợt đầu | 20 | **Chấp nhận** (QĐ4) | 2 |
| T | Mất câu trả lời "đã mua được" rồi gửi lại khi cờ đã "hết suất" | Cùng `Idempotency-Key` + `X-Attempt` ≥ 2 bỏ qua cờ (vẫn qua vé), trả kết quả đã lưu (QĐ21) | 20 | `TestFlashBuy_RetryAfterSoldOutReturnsStored` (20) | 2 |
| I | SĐT, IP của khách lọt vào khóa Valkey, log, dấu vết | Băm HMAC bằng `CACHE_KEY_SECRET`; kiểu `logging.Phone` tự che | 01, 03, 20 | `TestCacheKey_NoRawPhoneOrIP` (01) | 2 |

## (B3) `api` → Valkey — đợt 1 (bộ đệm, giới hạn tần suất), đợt 2 (cổng nhận vào)

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test | Đợt |
|---|---|---|---|---|---|
| T | Bộ đệm giữ giá cũ khi đặt hàng | Không quyết định nghiệp vụ nào đọc từ Valkey; đặt hàng, lượt mua luôn đọc PostgreSQL; xóa khóa theo báo hiệu sau commit, hạn sống ngắn làm lưới an toàn | 02, 09, 11 | `TestWebQuote_IgnoresCache` (09), `TestWebCache_InvalidatedAfterCommit` (02) | 1 |
| D | Valkey ngừng, chậm, bị xóa sạch (HT27, FS10) | Mỗi lệnh hạn 50 ms; cầu dao mở 5 s sau 5 lỗi liền; bộ đệm hỏng → đọc thẳng PostgreSQL; giới hạn tần suất → bộ đếm trong tiến trình; (đợt 2) bộ điều tiết 50%, cờ và số còn dựng lại từ PostgreSQL | 01, 02, 03, 04, 21 | `TestReadCache_ValkeyDownFallsThrough` (02), `TestRateLimiter_FallbackInProcess` (03), `TestAdmission_ValkeyDownHalfRate` (04) | 1, 2 |
| S, E | Tiến trình khác trên mạng ghi vào Valkey | Hai tiến trình chỉ nghe mạng nội bộ của compose; mỗi bên một người dùng ACL chỉ được nhóm lệnh cần; mật khẩu trong biến môi trường | 01 | `TestValkeyACL_DeniesOtherCommands` (01) | 1 |
| T | Gọi Valkey trong transaction (lùi transaction mà Valkey đã đổi) | Không lệnh Valkey nào trong hàm của `RetryTx`; test kiến trúc: `internal/modules/**` không nhập `internal/platform/cache` | 01, 02 | `TestArch_ModulesDoNotImportCache` (01) | 1 |

## (B4) Nhân viên → năm màn mới

Màn: Khóa dịch vụ, Yêu cầu về đơn hàng, Hãng trên website, Quà sinh nhật (đợt 1); Đợt flash sale (đợt 2).

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test | Đợt |
|---|---|---|---|---|---|
| E | Nhân viên không có quyền tạo, thu hồi khóa dịch vụ; sửa hãng trên web; xem yêu cầu kèm ảnh; soạn, lên lịch, đóng đợt | Quyền mới thêm vào `identity.DefaultRoles` + `docs/quyen.md` (`permissions_doc_test`); `x-permission` trên mọi operation; màn chỉ vẽ nút khi máy chủ cho (luật "Bẫy đã gặp") | 06, 12, 15, 16, 23–26 | `TestM5Routes_PermissionScopes` (05), test màn hình mock đúng quyền (23–26) | 1, 2 |
| I | Khóa dịch vụ hiện lại sau lần tạo | Chỉ trong thân câu trả lời của `createServiceKey`; màn hiện một lần, không lưu trong bộ nhớ trình duyệt; danh sách chỉ hiện tiền tố | 06, 25 | `TestServiceKeysScreen_ShowsOnce` (25) | 1 |
| T | Hai nhân viên sửa cùng cài đặt hãng trên web, cùng đợt flash | `version` trên dòng (khóa rồi so); khóa chống trùng sinh khi mở form | 15, 16, 17 | `TestWebCarrierSettings_VersionConflict` (15) | 1, 2 |
| T | Đổi giá đợt flash sau khi đã lên lịch | QĐ9: chỉ sửa khi `draft`; khi `scheduled`/`open` chỉ thêm suất, đóng sớm | 16, 17 | `TestFlashSale_EditOnlyInDraft` (16) | 2 |
| R | Chối đã tạo/thu hồi khóa, đã trả lời yêu cầu, đã đóng đợt | Audit trong cùng transaction (không giá trị khóa, không nội dung tin, không SĐT) | 06, 12, 17 | `TestServiceKey_AuditPrefixOnly` (06) | 1, 2 |

## (B5) Tệp ảnh khách gửi từ website — đợt 1

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test | Đợt |
|---|---|---|---|---|---|
| T | Tệp HTML/SVG/tệp chạy được giả làm ảnh | M5 không tự kiểm chữ ký: mục đích `order_request_photo` chế độ `private` (`Image: true, Private: true`); `files.RegisterPrivateImage` xếp lệnh `file.process_private`, imager của M1 giải mã nội dung; không phải ảnh hợp lệ → tệp `rejected`, yêu cầu kèm tệp đó bị từ chối (R10) | 12 | `TestOrderRequestAPI_Upload` (HTML khai jpeg → `uploaded` + 1 lệnh `file.process_private`), `TestOrderRequestAPI_WebAndStaffFlow` (tệp bị từ chối → 422 `file_rejected`; màn nhân viên `rejected` không url), `TestProcessPrivate_*` của M1 (12) | 1 |
| I | Ảnh riêng tư của khách lộ công khai | Ảnh thô chỉ ở `crm-tmp` (`BucketTmp`, `files.TmpKey`, tự xóa theo hạn của bucket); biến thể chỉ ở `BucketPrivate`; nhân viên xem bằng `files.DownloadURL` (link ký có hạn); `file.process` công khai từ chối mục đích `private` | 12 | `TestOrderRequestAPI_Upload` (ảnh thô chỉ `crm-tmp`, không đối tượng public/originals/private), `TestOrderRequestAPI_WebAndStaffFlow` (link ký tới `crm-private` `priv/<tenant>/<file>/`), `TestRequestPhotoPurpose_PrivateOnly`; biến thể, hết hạn link: test của M1 (12) | 1 |
| D | Tệp lớn, nhiều tệp làm đầy kho, nghẽn imager | `MaxBytes`, `AllowedTypes` của mục đích (`ErrTooLarge`, `ErrTypeNotAllowed`); `x-body-limit` của operation (tối đa 8 MiB); số tệp mỗi yêu cầu có trần; giới hạn tần suất theo khách | 05, 12 | `TestOrderRequestAPI_Upload` (6 MiB + 1 → 413, gif → 422), `TestRequest_Files` (6 ảnh → `too_many_files`), `TestRequest_CancelAndLimit` (5 yêu cầu/đơn/24 giờ) (12) | 1 |
| T | Gắn tệp của yêu cầu khác, của tenant khác | RLS theo tenant (`files.Status` không thấy tệp tenant khác); một tệp chỉ thuộc một yêu cầu (`UNIQUE (tenant_id, file_id)`); mã tệp lạ, tenant khác, sai mục đích, đã gắn → 422 `file_not_found` (Task 195, Q8) | 12 | `TestRequest_Files`, `TestOrderRequestAPI_WebAndStaffFlow` (12) | 1 |

## Chưa có / chấp nhận

- FS18 nhập SĐT người khác: chấp nhận theo QĐ4 tới khi có OTP.
- Phí ship web khi chưa nối adapter hãng (QĐ-183-4): không phải mối đe dọa bảo mật; ghi ở tiêu chí WB01.
- Thêm dòng ở M5-38 nếu task nào thấy ranh giới mới.
