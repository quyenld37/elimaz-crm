# Threat model M0 — các ranh giới tin cậy của nền tảng

Viết ở Task 23 (M0-23, 18/11/2026), sau khi mã M0 đã có. Phủ năm ranh giới của M0; ranh giới mới của M1 (kho tệp,
`imager`, nhập Excel, hãng giả) ở `docs/threat-model-m1.md`. Nguồn: spec `04-kien-truc.md` mục 4, 8, 11, 17; ADR-14,
16, 18, 19, 26, 30, 35, 37, 38, 41, 43.

STRIDE: **S** giả danh, **T** sửa trộm, **R** chối bỏ, **I** lộ thông tin, **D** làm ngừng dịch vụ, **E** nâng quyền.
Cột "Xử lý": **giảm** (chặn hay làm khó), **loại** (không thể xảy ra theo thiết kế), **chấp nhận** (ghi lý do).
Cột "Test / bước kiểm" ghi tên test có thật (gói: hàm); dòng **chưa có** gom ở mục cuối.

## Sơ đồ dữ liệu

```
 Meta, hãng ──HTTPS──▶ Cloudflare (hooks, giới hạn tần suất) ──tunnel──▶ ingest ──▶ webhook_deliveries, jobs
                                                                          │ (database hỏng: tệp tạm spool)
 Nhân viên ─▶ Cloudflare Access (bước hai) ──tunnel──▶ api ──────────────▶ PostgreSQL (role crm_app, RLS)
              thẻ JWT ở Cf-Access-Jwt-Assertion          │ SSE /api/v1/stream      ▲
                                                         ▼                         │ LISTEN/NOTIFY, jobs
                                                      worker ──── commands ──▶ bên ngoài (Meta, hãng, Telegram,
                                                         │                      nơi nhận nhịp sống)
 postgres (WAL-G) ──WAL, bản đầy đủ, mã hóa libsodium──▶ kho ngoài (không thuộc Cloudflare)
 Kiểm từ ngoài (2 nơi) ──▶ /health/ready, /health/deep (HEALTH_TOKEN)
```

Máy chủ không mở cổng nào ra ngoài: mọi lưu lượng vào đi qua `cloudflared` (Cloudflare Tunnel). Ba vai trò chạy
cùng một image không root, mỗi vai trò một ngân sách kết nối database.

## (1) Meta / hãng → `ingest`

| STRIDE | Mối đe dọa | Xử lý | Test / bước kiểm |
|---|---|---|---|
| S | Kẻ ngoài gửi webhook giả | Giảm: kiểm chữ ký HMAC từng nguồn trước khi ghi; sai chữ ký trả 401, không ghi | `webhooks`: `TestRejects`, `TestErrorBodyShape` |
| S | Giả `cf-connecting-ip` để né khóa theo địa chỉ | Loại: ingest chỉ nhận qua tunnel, không cổng ra máy | `tests/deploy`: `TestProdCompose_OnlyCloudflareIngress`; `webhooks`: `TestClientIP` |
| T | Gửi lại một gói hợp lệ (replay) để nhân đôi sự kiện | Giảm: khóa trùng theo mã sự kiện khi tách gói; cùng thân → một dòng, một việc | `webhooks`: `TestConcurrentSameBodyOneRowOneJob`, `TestSplitDedupesAcrossPackets` |
| R | Không chứng minh được đã nhận gì | Giảm: lưu nguyên thân và header (đã che) 30 ngày | `webhooks`: `TestPostValidStoresRawBodyAndOneSplitJob`, `TestHeadersStoredRedacted`, `TestCleanupDeliveriesOlderThan30Days` |
| I | Thân webhook (SĐT, tin nhắn) lọt vào log, lỗi | Giảm: log không ghi thân; nội dung lỗi qua bộ lọc dữ liệu cá nhân | `webhooks`: `TestIngestDatabaseErrorRecordedAndScrubbed`, `TestIngestPanicRecordedAndScrubbed`; `logging`: `TestScrubTextProperty` |
| D | Dồn dập webhook (đúng hay sai chữ ký) | Giảm: luật tần suất Cloudflare (staging, bước 7.7); giới hạn yêu cầu đang xử lý; khóa địa chỉ gửi sai nhiều lần; cảnh báo `hooks-attack` | `webhooks`: `TestInFlightLimit`, `TestLockoutOverHTTP`; luật Cloudflare: **chưa có** (cần chủ shop OK) |
| D | Database hỏng làm rơi webhook | Giảm: ghi tệp tạm (spool), trả 200, đẩy vào khi database trở lại; cảnh báo `spool` | `webhooks`: `TestDatabaseDownSpoolsAndHealthCounts`; `e2e`: `TestDatabaseOutageKeepsWebhooks` |
| E | Đường khác của ingest (api, admin) bị gọi từ ngoài | Loại: ingest chỉ phục vụ `/hooks/*` và `/health/*`; hãng giả chỉ ngoài production | `webhooks`: `TestIngestServesOnlyHooksAndHealth`; `e2e`: `TestIngestFakeCarrierOnlyOutsideProduction` |

## (2) Nhân viên → Cloudflare Access → `api`

| STRIDE | Mối đe dọa | Xử lý | Test / bước kiểm |
|---|---|---|---|
| S | Thẻ giả, hết hạn, sai `aud`, sai khóa | Giảm: kiểm chữ ký theo bộ khóa của Access, `aud`, hạn; khóa lạ thì tải lại có giới hạn | `cfaccess`: `TestVerify`, `TestKeyRotation`, `TestUnknownKidThrottled`; `httpapi`: `TestAuthTokens` |
| S | Đăng nhập không qua bước hai | Giảm: đòi dấu bước hai trong thẻ (ADR-35) | `identity`: `TestCheckMFA`, `TestCheckMFAProperty` |
| S | Tài khoản bị khóa vẫn dùng thẻ còn hạn | Giảm: đọc trạng thái người dùng mỗi yêu cầu | `httpapi`: `TestAuthUsersAndGrants` |
| T | Trang khác gửi yêu cầu ghi bằng phiên của nhân viên (CSRF) | Giảm: yêu cầu ghi đòi `Origin` thuộc `CONSOLE_ORIGINS` và kiểu nội dung JSON | `httpapi`: `TestOriginRejectsWrites`, `TestOriginSkipsReads` |
| T | Bấm hai lần, mạng chập làm ghi hai lần | Loại: mọi đường ghi có `Idempotency-Key`, lưu cùng transaction | `idempotency`: `TestAcquireConcurrentSameKeyOneClaim`, `TestPropertyCounterMatchesFinalKeys`; `arch`: `TestRoutesIdempotencyModes` |
| R | Nhân viên chối đã sửa | Giảm: nhật ký thao tác chỉ ghi thêm, cùng transaction | `identity`: `TestAuditLogsAppendOnly`, `TestRecordAuditRolledBackWithTransaction` |
| I | Thấy dữ liệu của shop khác | Loại: RLS ép cả chủ bảng, ngữ cảnh tenant mỗi transaction | `tenancy`: `TestTenantsSeeOnlyOwnRows`, `TestPropertyQueriesOnlySeeOwnTenant`, `TestReusedConnectionOutsideContextSeesNothing` |
| I | Vết lỗi, dữ liệu cá nhân trong câu trả lời lỗi | Giảm: body lỗi chỉ `{error, request_id}`; panic trả 500 không vết | `httpapi`: `TestPanicReturns500WithoutTrace`, `TestInternalErrorPersonalDataNotLeaked` |
| D | Một người gọi dồn dập | Giảm: giới hạn theo người dùng; giới hạn thân yêu cầu | `httpapi`: `TestUserRateLimitHTTP`, `TestBodyLimit` |
| E | Route mới quên khai quyền | Loại: route thiếu `x-permission` không dựng được; mặc định từ chối | `arch`: `TestRoutesPublicSetAndPermissions`; `httpapi`: `TestAuthWithoutVerifierDenies` |
| E | Quyền ở kho này dùng sang kho khác; tự nâng quyền | Giảm: quyền là cặp (quyền, phạm vi); không cấp quá quyền của người cấp | `identity`: `TestCanProperty`, `TestReplaceGrants_ExceedsActor`, `TestReplaceGrants_SelfModify`; `httpapi`: `TestWarehouseScopedRole` |

## (3) `worker` → bên ngoài

| STRIDE | Mối đe dọa | Xử lý | Test / bước kiểm |
|---|---|---|---|
| T | Gửi một lệnh hai lần (tin nhắn, vận đơn) khi không chắc đã gửi | Loại: lệnh không chắc → `in_doubt`, không tự gửi lại; người kiểm rồi quyết | `commands`: `TestPropertyNeverSentTwice`, `TestTimeoutAfterSendInDoubtNeverResent`, `TestStopDuringExecuteInDoubt` |
| I | Token của bên ngoài lọt vào log, dấu vết, cảnh báo | Giảm: log che sâu theo tên khóa và kiểu; văn bản cảnh báo chỉ có tên luật và số | `logging`: `TestRedactNested`, `TestSensitiveKey`, `TestLoggerRedactsStructAndTypes` |
| D | Bên ngoài chậm, treo | Giảm: hạn mỗi lệnh dưới hạn giữ việc; lùi theo yêu cầu 429 | `commands`: `TestExecuteTimeoutInDoubt`, `TestCommandTimeoutBelowLease` |
| S | Nơi nhận nhịp sống, Telegram bị giả | Chấp nhận: chỉ gửi ra (HTTPS, kiểm chứng chỉ mặc định của Go); không nhận lệnh từ đó | — |

## (4) Tiến trình → PostgreSQL

| STRIDE | Mối đe dọa | Xử lý | Test / bước kiểm |
|---|---|---|---|
| E | Ứng dụng có quyền đổi schema, bỏ qua RLS | Loại: ứng dụng chạy bằng `crm_app` (NOBYPASSRLS, không sở hữu bảng); chỉ `migrate` dùng `crm_owner` | `identity`: `TestAppRolePrivilegesAndRLS`; `tenancy`: `TestCheckSchemaRulesEachRule` |
| T | SQL injection | Loại: SQL tĩnh qua sqlc; SQL dựng lúc chạy chỉ trong `*_dynsql.go` có test trên PostgreSQL thật; cấm simple protocol | `arch`: `TestDynSQL…`/`sqlusage_test.go`, `sqlmode_test.go` |
| T | Sửa migration đã chạy, lệnh thu hẹp lọt vào | Giảm: checksum mỗi tệp đã áp dụng; lệnh thu hẹp cần `-- contract:` | `e2e`: `migrate_test.go`; hook chặn sửa migration đã commit |
| D | Transaction treo giữ khóa, câu chậm | Giảm: `statement_timeout` 30 s, `lock_timeout` 5 s, ngồi không 60 s; cảnh báo `long-tx`, `lock-wait`, `slow-query` | `arch`: `txusage_test.go`; `observability`: `pgwatch_test.go` |
| D | Hết kết nối | Giảm: ngân sách kết nối mỗi vai trò; cảnh báo `pg-connections` | `observability`: `pgwatch_test.go` |
| I | Lộ chuỗi kết nối, mật khẩu | Giảm: chỉ trong tệp biến của máy chủ; gitleaks trong `make ci`; `export-config.sh` chỉ xuất tên | `tests/deploy`: `TestExportConfig_NamesOnlyAndDigests`, `TestExportConfig_StopsOnSecret` |

## (5) Máy → kho ngoài (sao lưu)

| STRIDE | Mối đe dọa | Xử lý | Test / bước kiểm |
|---|---|---|---|
| I | Kho ngoài bị lộ, đọc được dữ liệu khách | Giảm: WAL-G mã hóa libsodium trước khi rời máy; khóa giữ ở hai nơi ngoài máy chủ | `restore-drill.sh`, `verify-drill.sh` (dev); staging: **cần chủ shop OK** (bước 7.2, 7.6) |
| T | Bản sao lưu hỏng mà không ai biết | Giảm: kiểm khôi phục hằng tuần, so số dòng; cảnh báo `backup-verify`, `backup-age`, `wal-archive` | `make verify-drill`; `observability`: `rules_test.go` |
| D | Kẻ chiếm máy xóa bản sao lưu | Giảm: khóa S3 của máy chỉ ghi thêm (không xóa) — **chưa có**, làm khi chủ shop tạo kho (bước 7.2) | — |
| R | Không biết cấu hình máy đã đổi gì | Giảm: `export-config.sh` mỗi tuần, giữ 8 bản ở kho ngoài | `tests/deploy`: `TestExportConfig_KeepsEightAndUploads` |

## Chưa có (giữ lại cho staging, cần chủ shop OK)

1. Luật giới hạn tần suất Cloudflare cho `hooks-staging` (bước 7.7).
2. Khóa S3 của máy chủ không có quyền xóa bản sao lưu cũ (vòng đời do kho tự xóa theo hạn) — chọn khi chủ shop tạo kho ngoài (bước 7.2).
3. Diễn tập khôi phục với kho ngoài thật (bước 7.6).
4. Cổng 4194 của hãng giả không được mở ra ngoài trên staging (việc treo Task 31): compose máy thử chỉ bật `fakecarrier` không `ports:`.
