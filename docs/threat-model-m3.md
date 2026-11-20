# Threat model M3 — vận chuyển, hãng giao hàng, tiền COD

Bản đầu viết ở Task M3-00 (số toàn cục 91, 20/11/2026), trước khi có mã M3, theo mẫu `docs/threat-model-m2.md`. Đây là
bước 2 của "Cách làm việc ở mỗi mốc" (spec 08). Task M3-49 (140) hoàn thiện: điền tên test có thật trong mã vào cột
"Task / test", đổi dòng **Chưa có** thành test hoặc ghi vì sao chấp nhận.

Chỉ phủ phần mới của M3; ranh giới của M0 (Access, `ingest`, tiến trình → PostgreSQL), M1 (kho tệp, Excel, hãng giả lát
mỏng), M2 (đơn, khách, phiếu trả) ở `docs/threat-model-m{0,1,2}.md`. Phần nền chung đã làm ở Task 90 (M2-33):
`fieldcrypt`, `webhooks.RoutedSource` + đường `/hooks/{source}/{routeKey}`, `tenancy.PutRoute`, `commands.Abandon`,
`ResolveInDoubt` tôn trọng `AutoRetryNotSent() == false`.

Nguồn: kế hoạch `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Global Constraints, Review Focus,
Q-8, Q-20, Q-33…), spec `04-kien-truc.md` mục 3, 5.2, 5.3, 7, 8, 11; ADR-23, 24, 29, 41, 46. Cột "Task" ghi số nội bộ
M3 (số toàn cục = 91 + NN). Tên test là tên dự kiến, Task M3-49 sửa theo mã thật.

STRIDE: **S** giả danh, **T** sửa trộm, **R** chối bỏ, **I** lộ thông tin, **D** làm ngừng dịch vụ, **E** nâng quyền.

## Sơ đồ dữ liệu (phần mới)

```
 (1) Nhân viên ──(Access)──▶ api ── WriteTx ──────────────────────────────────────────────┐
     Kết nối vận chuyển:     │  lưu khóa API hãng: fieldcrypt.Seal (AAD tenant+bảng+cột+dòng) │
     khóa API, kho lấy hàng  │  ──▶ fulfillment.carrier_accounts.credential_enc (bytea)      ▼
     Gửi hãng, hủy, đổi hãng │  ──▶ commands.Enqueue (payload chỉ mã bản ghi)           PostgreSQL
                             ▼                                                       fulfillment, cash,
 (2) worker ── Execute (ngoài transaction) ── fieldcrypt.Open ──▶ adapter hãng ──HTTPS──▶ API hãng
        ▲          OnDone (transaction) ◀── kết quả (mã vận đơn, phí, trạng thái) ◀─────────┘
        │
 (3) API hãng ──webhook──▶ ingest /hooks/<hãng>/<route_key>
                             tenant_routes(kind='carrier_account', route_key) → tenant
                             chỉ ghi việc "tra lại" ──▶ worker tra bằng API có xác thực ──▶ áp kết quả
                                                       (ApplyTracking: vận đơn → order → inventory)
 (4) Kế toán ──(Access)──▶ api: createUpload ──▶ kho tệp (bucket riêng tư, sha256)
                                ──▶ worker: đọc tệp có giới hạn (xlsx.Read / encoding/csv) ──▶ cash.cod_statement_lines
                                ──▶ khớp, chốt (version) ──▶ sổ COD hai vế
 (5) events shipment.status_changed ──▶ worker consumer cash ──▶ cash.cod_transfers + cod_ledger_entries
                                         (fulfillment không gọi cash)
 (6) worker ── tải nhãn PDF từ máy chủ hãng (chỉ host khai trong adapter) ──▶ kho tệp ──▶ nhân viên kho tải
```

Dữ liệu nhạy cảm mới: khóa API, mật khẩu, token của tài khoản hãng (bí mật); địa chỉ, SĐT, tên người nhận gửi sang hãng
và địa chỉ mới của yêu cầu đổi địa chỉ (dữ liệu cá nhân); tệp bảng kê (có tên, SĐT khách của hãng); số tiền COD.

## (1) Nhân viên → `api`: tài khoản hãng, gửi hãng, hủy, đổi địa chỉ

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| S | Gửi đơn của người bán A bằng tài khoản hãng của người bán B | `shipments` có `seller_id`; khóa ngoại ghép `(tenant_id, seller_id, carrier_account_id)` và `(tenant_id, seller_id, order_id)`: database từ chối; mọi câu đọc có `seller_id` | 1, 48 | `TestShipments_SellerFKRejectsCrossSeller` (1), `m3_two_sellers` (48) |
| E | Nhân viên kho X gửi hãng, in nhãn, nhận hàng hoàn của kho Y (leo quyền sang kho khác) | `fulfillment.ship` (M1, `ScopeByTarget` theo kho) kiểm trên kho của tài khoản hãng **và** kho của đơn; quyền mới có phạm vi kho; ngoài phạm vi → 404 | 4, 12, 16 | `TestM3Routes_PermissionScopes` (4), `TestShipAPI_OtherWarehouse404` (12) |
| E | Nhân viên không có quyền sửa tay trạng thái, đảo "Thành công", giao một phần | Quyền riêng `fulfillment.correct`; `x-permission` trên mọi route M3; luật kiến trúc route không có `x-permission` thì đỏ | 4, 26, 27 | `TestM3Routes_PermissionScopes` (4) |
| T | Bấm "Gửi hãng" hai lần, hai người gửi cùng lúc → hai vận đơn | `UNIQUE (tenant_id, order_id) WHERE status <> 'cancelled'` của M1; `Idempotency-Key`; `order.Lock` trước khóa vận đơn | 12 | `TestShip_Parallel50OneShipment` (12) |
| T | Đổi COD, địa chỉ của đơn đã gửi mà hãng không biết | `ErrLockedByShipment` của M2; đổi địa chỉ chỉ qua yêu cầu tới hãng, đơn chỉ đổi khi hãng nhận | 17 | `TestAddressChange_OnlyAfterCarrierAccepts` (17) |
| R | Nhân viên chối đã gửi hãng, đã hủy, đã sửa tay trạng thái | Audit trong cùng transaction (tên trường, không giá trị cá nhân, không khóa API); lịch sử vận đơn `shipment_events` chỉ ghi thêm | 9, 12, 14, 27 | `TestCarrierAccount_AuditNoSecret` (9) |
| I | Khóa API lộ qua API đọc, log, dấu vết, audit, `jobs.result` | Chỉ lưu `credential_enc` (`fieldcrypt`, AES-256-GCM, AAD gắn dòng); API trả `hasCredentials: true`, không bao giờ trả khóa; kiểu `fulfillment.Credentials` có `LogValue()` → `[Redacted]`; audit chỉ ghi tên trường `credentials` | 3, 9 | `TestCredentials_LogValueRedacted` (3), `TestCarrierAccount_NeverReturnsSecret`, `TestCarrierAccount_LogsSpansNoSecret` (9) |
| I | Bản mã khóa API chép sang dòng hay shop khác vẫn giải được | AAD `{TenantID, Table: "fulfillment.carrier_accounts", Column: "credential_enc", RowID}`; `Open` sai AAD → `ErrDecrypt` | 9 | `TestCarrierAccount_CiphertextBoundToRow` (9) |
| I | Đoán mã vận đơn, mã tài khoản hãng của shop khác | RLS; mã lạ hay của tenant/kho khác → 404 | 4, 12 | `TestShipAPI_OtherTenant404` (12) |

## (2) `worker` → API hãng

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| T | Máy chết sau khi hãng đã tạo vận đơn → tạo lần hai | Lệnh `commands` với mã nội bộ là mã tham chiếu; không chắc đã gửi → `in_doubt`, tra hãng theo mã nội bộ; `AutoRetryNotSent = false` cho lệnh tạo (hãng nói chưa có → `dead confirmed_not_sent`, người bấm "Gửi lại") | 12 | `TestCreateCommand_InDoubtLookupFound`, `TestCreateCommand_NotSentNoAutoRetry` (12) |
| T | Lệnh tạo thất bại khi đơn đang `cancel_requested` (Task 431) để đơn kẹt | `OnTerminal` → `Writer.DetachShipment` (đã nhận `cancel_requested`, hủy đơn) | 12 | `TestCreateCommand_TerminalCancelRequested` (12) |
| D | Hãng chậm hay treo giữ worker, chiếm kết nối database | Gọi ngoài transaction; hạn giờ client 10–20 s; `Retry-After` của hãng; ngân sách tra bù 200 vận đơn/tài khoản/10 phút | 3, 11 | `carriertest.Run` ca hạn giờ (3), `TestRefreshBudgetPerAccount` (11) |
| I | Gửi dữ liệu khách thừa sang hãng, hoặc ghi địa chỉ, SĐT vào `jobs.result`, log | Payload việc chỉ mang mã; `CreateRequest` chỉ có trường hãng cần; `ship_to` che trong log | 3, 12 | `TestCreateCommand_ResultNoPersonalData` (12) |
| E | SSRF qua URL nhãn do hãng trả (hãng hay kẻ giả hãng trả URL trỏ vào mạng nội bộ) | Worker chỉ tải từ máy chủ của hãng khai cứng trong adapter (danh sách host, HTTPS); URL khác → lỗi vĩnh viễn, không tải; không theo chuyển hướng sang host khác; giới hạn cỡ tệp | 16 | `TestLabelDownload_RejectsForeignHost`, `TestLabelDownload_NoRedirectOffHost` (16) |
| S | Tài khoản hãng bị thu hồi quyền mà vẫn gửi, hoặc tự đổi sang tài khoản khác | Lỗi xác thực → tài khoản `auth_failed`, báo "không sẵn sàng"; không tự chọn tài khoản khác | 9, 12 | `TestAuthFailed_NoFallbackAccount` (12) |

## (3) API hãng → `ingest` → worker (webhook)

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| S | Giả mạo webhook hãng: người ngoài biết mã vận đơn gửi "đã giao" | Webhook chỉ là tín hiệu: handler chỉ ghi việc tra lại; trạng thái chỉ đổi theo kết quả tra bằng API có xác thực; chữ ký (nếu hãng có) kiểm bằng bí mật theo hãng trong biến môi trường | 10, 11 | `TestWebhook_FakeDeliveredNoChange` (10), `TestRefresh_UsesAuthenticatedLookup` (11) |
| S | Webhook không mang mã shop; kẻ gửi khai `tenant_id` | Tìm shop bằng `route_key` 32 ký tự ngẫu nhiên trong URL (`tenant_routes`, `tenancy.PutRoute`); không tin trường nào của thân; khóa lạ → 404; route `/hooks/{source}` không khóa **không** được nhận cho nguồn hãng (Task 90 ghi) | 9, 10 | `TestWebhook_UnknownRouteKey404`, `TestWebhook_NoRouteKeyRejected` (10) |
| S | Nguồn hãng không ký payload mà `VerifySignature` trả `true` vô điều kiện | Nguồn không có chữ ký phải ghi rõ trong adapter và chỉ dựa vào route key + tra lại; contract test kiểm nguồn có chữ ký từ chối chữ ký sai | 3, 10 | `carriertest.Run` ca chữ ký (3) |
| T | Hai tài khoản trùng mã vận đơn → ghép nhầm đơn | Mọi lần tra đi theo `(carrier_account_id, tracking_code)`; trùng trong cùng hãng → "Cần xem" `tracking_conflict` | 1, 10, 32 | `TestTrackingConflict_TwoAccounts` (10) |
| T | Tin lặp, sai thứ tự, tin về trước kết quả tạo | Máy trạng thái thuần Go, khóa dedupe theo hãng/tài khoản; tin chưa khớp được giữ và khớp sau | 7, 8, 10 | `TestShipState_Properties` (rapid, 7), `TestApply_DuplicateAndOutOfOrder` (8) |
| D | Dồn hàng nghìn webhook (thật hay giả) làm nghẽn | `ingest` không chạm database nghiệp vụ; trùng gộp theo khóa dedupe; giới hạn thân của M0; tra bù gộp theo vận đơn | 10, 48 | `carrier_webhooks.js` (48) |
| I | `shipment_events.raw` giữ địa chỉ, SĐT người nhận do hãng gửi | Chỉ giữ trạng thái, thời điểm, mã, lý do sau `logging.Redact` | 8, 10 | `TestShipmentEvent_RawRedacted` (8) |

## (4) Kế toán → tệp bảng kê → kho tệp → worker → `cash`

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| T | Sửa trộm số tiền bảng kê (sửa tệp sau khi nhập, sửa dòng trước khi chốt) | Tệp băm sha256 lúc nhập, lưu bucket riêng tư, không ghi đè; dòng bảng kê không sửa được sau khi chốt; người nhập và người chốt ghi audit; chốt mang `version` | 31, 32 | `TestStatement_Sha256Stored`, `TestStatement_AuditImporterAndConfirmer` (31, 32) |
| T | Cùng bảng kê nhập hai lần; một vận đơn có trong hai bảng kê | Unique theo sha256 tệp và theo khóa dòng hãng; mỗi dòng ghi tiền đúng một lần (khóa `idempotency_key` của bút toán) | 31, 32 | `TestImport_SameFileTwiceRejected` (31), `TestMatch_ShipmentInTwoStatements` (32) |
| T | Hai kế toán chốt cùng lúc → ghi trùng tiền | Khóa dòng bảng kê rồi so `version`; người sau 409 | 32 | `TestConfirm_TwoAccountantsParallel` (32) |
| R | Chối bỏ thao tác tiền (nhập, chốt, điều chỉnh, đóng nghĩa vụ hoàn "không hoàn") | Audit mọi thao tác `cash` (người làm, giờ, lý do) trong cùng transaction; sổ chỉ ghi thêm (trigger chặn `UPDATE`/`DELETE`); sửa sai bằng bút toán đảo có lý do | 2, 30, 32, 33 | `TestLedger_NoUpdateDelete` (2), `TestCashAudit_EveryWrite` (33) |
| T | Phần mềm ghi bút toán lệch hai vế | Constraint trigger kiểm cân lúc commit (M1), mở rộng cho loại mới | 2 | `TestLedger_UnbalancedRejectedAtCommit` (2) |
| D | Tệp `.xlsx` độc (bom zip, hàng triệu dòng, ô khổng lồ, công thức) làm tê liệt worker | `xlsx.Read` với `Limits` (cỡ tệp, giải nén, số dòng, ký tự ô; công thức thành lỗi ô); CSV qua `encoding/csv` có trần dòng, cột, ký tự; phân tích ở worker có hạn giờ, lỗi → `parse_failed` | 31 | `TestStatementParse_ZipBomb`, `TestStatementParse_TooManyRows`, `TestStatementParse_Formula` (31) |
| I | Tệp bảng kê chứa tên, SĐT khách lọt vào database, log | Dòng bảng kê chỉ giữ mã vận đơn và số tiền; tệp gốc ở bucket riêng tư, tải qua URL ký có hạn; giữ 400 ngày | 31 | `TestStatementLines_NoPersonalColumns` (31) |
| E | Nhân viên kho, bán hàng xem hay chốt tiền | Quyền `cash.cod.read`, `cash.statements.import`, `cash.statements.confirm`, `cash.adjust`, `cash.refunds.waive` chỉ ở vai trò kế toán, chủ | 4 | `TestM3Roles_CashOnlyAccountant` (4) |

## (5) `events` → consumer `cash` (sổ COD theo sự kiện vận đơn)

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| T | Sự kiện chạy lặp ghi tiền hai lần | Khóa `evt:<eventID>:<kind>` (M1) duy nhất; `cash.Post` trả `created=false` | 29 | `TestCodConsumer_ReplayOnce` (29) |
| T | Vận đơn không hiện hành ghi tiền cho đơn | Chỉ vận đơn hiện hành (`orders.current_shipment_id`) đổi đơn, tồn, tiền | 8, 29 | `TestCodConsumer_NonCurrentShipmentIgnored` (29) |
| T | Hãng đảo "đã giao" khi tiền đã về shop → tự trừ tiền sai | Không tự động: vận đơn vào "Cần xem" `remitted_then_returned`, kế toán điều chỉnh có xem trước | 27, 33 | `TestReverse_RemittedGoesToReview` (27) |

## (6) Tệp nhãn và kho tệp

| STRIDE | Mối đe dọa | Cách xử lý | Task | Test |
|---|---|---|---|---|
| I | Nhãn (có địa chỉ, SĐT người nhận) tải được bởi người ngoài | Mục đích tệp `carrier_label` ở bucket riêng tư; URL ký ngắn hạn; tải cần `fulfillment.print` theo kho; lô nhãn dọn sau 30 ngày | 4, 16 | `TestLabelBatch_DownloadNeedsPermission` (16) |
| D | PDF nhãn khổng lồ do hãng trả làm đầy đĩa | Giới hạn cỡ tệp khi tải; vượt → lỗi lô, không lưu | 16 | `TestLabelDownload_SizeLimit` (16) |

## Rủi ro còn mở (ghi lại, không chặn M3)

| Rủi ro | Vì sao chấp nhận / khi nào xử lý |
|---|---|
| Khóa API hãng thật chưa có; adapter VTP, Best, SPX chưa kiểm với môi trường thử | Chờ chủ shop (mục "Việc cần chủ shop"); trước đó chỉ hãng giả và nhập mã tay |
| Xoay khóa `FIELD_ENC_KEYS` chưa có mã hóa lại hàng loạt | Ngoài phạm vi M3; RUNBOOK ghi cách thay khóa từng tài khoản (Task 49) |
| `FindWebhookDelivery` không lọc `body_kind='event'` (Task 90 ghi) | Va chạm sha256 chỉ trên lý thuyết; Task M3-10 (101) xét thêm điều kiện khi đụng tới |
| Migration `0291` thêm `CHECK` có quét bảng dưới khóa ACCESS EXCLUSIVE (Task 90 ghi) | Bảng còn nhỏ trước ngày mở; RUNBOOK ghi khi deploy |
