# Threat model M4 — hộp chat Facebook, comment, màn Live

Viết ở Task M4-42 (số toàn cục 182, 28/11/2026), sau khi mã M4 đã gộp vào `main`. Đây là bước 2 của "Cách làm việc ở
mỗi mốc" (spec 08). Mọi tên test trong cột "Test" là test có thật trong mã lúc viết (đã grep); dòng **Chưa có** ghi rõ
vì sao chấp nhận hoặc việc nào sẽ làm.

Chỉ phủ phần mới của M4; ranh giới của M0 (Access, `ingest`, tiến trình → PostgreSQL), M1 (kho tệp), M2, M3 ở
`docs/threat-model-m{0,1,2,3}.md`. Nền chung (Task 90): `fieldcrypt`, `webhooks.RoutedSource`, `commands.Abandon`,
`ResolveInDoubt` tôn trọng `AutoRetryNotSent() == false`.

Nguồn: kế hoạch `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (Global Constraints, vùng rủi
ro cao), spec `02-facebook-sync.md`, `04-kien-truc.md` mục 4, 5.2, 5.3, 6, 13; ADR-16, 17, 21, 41, 46.

STRIDE: **S** giả danh, **T** sửa trộm, **R** chối bỏ, **I** lộ thông tin, **D** làm ngừng dịch vụ, **E** nâng quyền.

## Sơ đồ dữ liệu (phần mới)

```
 Meta ──webhook (X-Hub-Signature-256)──▶ ingest /hooks/facebook ──▶ webhooks.deliveries (thân thô) ──▶ việc split
   │                                       (1)                                                         │
   │                                                                     worker: split → inbound.<kind> (chống trùng)
   │                                                                     ──▶ conversation.* , customer.* , files
   │◀──Graph API (token Page, appsecret_proof)── worker (3): gửi tin, trả lời comment, ẩn, tra kết quả, hồ sơ, bài
   │──tệp đính kèm (URL CDN của Meta)──▶ worker (4): tải có giới hạn ──▶ kho tệp riêng tư (chat_image / chat_file)
   │──luồng SSE comment live / đọc định kỳ──▶ worker giữ khóa phiên (6) ──▶ conversation.live_*
   │──OAuth: đăng nhập Facebook trong tab ──▶ console /kenh/facebook/callback ──▶ api (7) ──▶ channel_accounts
   └──callback xóa dữ liệu (signed_request) ──▶ ingest /hooks/facebook/data-deletion (8) ──▶ datadeletion

 Nhân viên ──(Access)──▶ api (2): đọc hội thoại theo Page được giao, gửi, ghi chú, chốt comment live → đơn nháp
                                   ──▶ commands.Enqueue (lệnh gửi có hạn 3 phút) ──▶ worker (3)
 Trình duyệt ◀── đường dẫn ký sẵn có hạn ── kho tệp (5) (ảnh: biến thể mã hóa lại; tệp khác: Content-Disposition: attachment)
```

Dữ liệu nhạy cảm mới: token Page (bí mật, `fieldcrypt`), App Secret (biến môi trường), nội dung tin và comment, tên,
ảnh đại diện, PSID của khách (dữ liệu cá nhân), ảnh, tệp khách gửi.

## (1) Meta → `ingest`

| STRIDE | Mối đe dọa | Cách xử lý | Test |
|---|---|---|---|
| S | Kẻ ngoài gửi webhook giả (tin, comment giả) | HMAC-SHA256 trên thân thô với App Secret, so bằng `hmac.Equal`; thiếu/sai chữ ký → 401, không ghi | `TestVerifySignature`, `TestChallenge` (facebook), `TestRejects` (webhooks), `TestIngestE2E` |
| S | Gửi lại nguyên một webhook thật đã bắt được (phát lại) | Chống trùng theo khóa sự kiện (`mid`, `comment_id`+`verb`+thời điểm) ở `EnqueueInbound`, `UNIQUE` của tin | `TestSplitDedupesAcrossPackets`, `TestEnqueueInboundDedupesAndDelivers`, `TestInboundSecondMessageAndDuplicateMID`, `TestConcurrentSameBodyOneRowOneJob` |
| T | Gói tin hỏng, khóa lạ, thời gian lạ làm vỡ bộ tách | Bộ tách bỏ qua phần không hiểu (đếm `skipped`), gói hỏng → `dead` có lý do; fuzz | `TestSplit_SkippedAndErrors`, `TestSplit_EntryTimeUnits`, `FuzzSplit` (`split_fuzz_test.go`), `TestSplitUnreadableBodyIsDead` |
| T | Tin về sai thứ tự làm mất tin mới | Giữ tin mới nhất theo thời điểm của Meta | `TestInboundOutOfOrderKeepsNewest`, `TestSplitOrdersByOccurredAt` |
| R | Không chứng minh được Meta đã gửi gì | Thân thô giữ 30 ngày ở `webhooks.deliveries` (header đã che) | `TestPostValidStoresRawBodyAndOneSplitJob`, `TestHeadersStoredRedacted`, `TestCleanupDeliveriesOlderThan30Days` |
| I | Nội dung tin vào log, dấu vết | Log chỉ mã; kiểu cá nhân có `LogValue()` | `TestInboundDoesNotLeakContent`, `TestIngestPanicRecordedAndScrubbed` |
| D | Dồn webhook (ca live 100 sự kiện/s, tấn công) | Giới hạn số yêu cầu đang xử lý, khóa địa chỉ sai chữ ký nhiều lần, ghi tạm ra đĩa khi database chậm, chỉ ghi thân rồi trả 200 | `TestInFlightLimit`, `TestLockoutOverHTTP`, `TestDatabaseDownSpoolsAndHealthCounts`, `TestEndpoint_DatabaseDownSpools`; tải: `docs/do-luong/thu-tai-m4.md` (Task 180) |
| D | Một sự kiện lỗi chặn cả gói / cả hội thoại | Mỗi sự kiện một việc riêng | `TestSplitOneFailureDoesNotBlockPacket`, `TestSplitFailedEventDoesNotBlockSameKey` |
| E | Webhook của Page shop A ghi vào tenant shop B | Tra tenant theo `tenant_routes` của Page (`external_id`); Page không biết → bỏ qua | `TestInboundIgnoresUnknownTypeAndAccount`, `TestRoutedSplitUsesRouteKey`, `TestConnect_PageConnectedElsewhere` |

## (2) Nhân viên → `api`

| STRIDE | Mối đe dọa | Cách xử lý | Test |
|---|---|---|---|
| E | Nhân viên thấy hội thoại, tệp, khách của Page không được giao | Quyền có phạm vi Page (`access.PageScope`); danh sách lọc theo `VisiblePages`; mã của Page khác → 404 | `TestPageScope`, `TestVisiblePages`, `TestRequire` (access), `TestPageScope_PQ04`, `TestConversationReadAPI_PQ04`, `TestPresence_Access`, `TestRender_AccessAndScope`, `TestClaim_SellerAndAccess`, `TestGetAttachmentURL` |
| E | Gửi tin, trả lời, ẩn comment khi Page đang ở chế độ chỉ xem | `send_mode = 'read_only'` mặc định; chặn ở lệnh gửi và ở mọi đường tự động | `TestSendReadOnly_MO03`, `TestCommentActions_ReadOnly`, `TestGreeting_ReadOnlyAndHistory`, `TestAutoHide_ReadOnlyAndStale`, `TestWriter_SetSendMode`, `TestChannelAPI_SetSendMode` |
| E | Gửi ngoài cửa sổ 24 giờ / 7 ngày (vi phạm chính sách Meta, Page bị khóa) | `SendPolicy` tính ở máy chủ; `send_not_allowed` 409; `HUMAN_AGENT` chỉ khi bật | `TestSendPolicy_Message`, `TestSendPolicy_PrivateReply`, `TestSendPolicy_CommentAndOverrides`, `TestSendPolicy_CH09`, `TestConversationReadAPI_SendPolicy`, `TestWriter_SetHumanAgent` |
| T | Bấm gửi hai lần, hai người cùng nhắn riêng một comment | `Idempotency-Key` + `client_ref` duy nhất; một tin riêng mỗi comment (unique một phần) | `TestSendProperty_AtMostOncePerClientRef`, `TestPrivateReply_OnlyOnce`, `TestSchema_OnePrivateReplyPerComment`, `TestCommentActionsAPI_PrivateReplyRace`, `TestIdempotencyFiftyParallelSameKey` |
| T | Hai nhân viên chốt cùng một comment live, bán quá tồn | Khóa comment rồi giữ hàng theo ADR-46 trong một transaction; thuộc tính rapid | `TestClaim_LV07_TwoStaffOneComment`, `TestClaimProperty_NeverOversellOneClaimPerComment`, `TestClaim_LastUnits`, `TestClaim_MergedCustomerLockOrder`, `TestLiveClaimsAPI_SameKey` |
| I | Ghi chú nội bộ lọt ra khách | Ghi chú là loại tin riêng, không bao giờ vào lệnh gửi; CHECK ở database | `TestNote_NeverLeaves`, `TestSchema_NoteNeverLeaves`, `TestNote_CheckBlocksSending` |
| R | Nhân viên chối đã gửi, đã ẩn, đã chuyển chế độ gửi thật | Tin ra mang `sent_by`; audit cho đổi chế độ gửi, phân công; cảnh báo `send-mode` | `TestAutomatedKeepsStaffMark`, `TestSendModeAlertCommand`, `TestAssign_CH06` |
| I | Mẫu trả lời dùng chung lộ tên Page khác | Mẫu dùng chung chỉ người có mọi Page tạo được | `TestTemplatePermissions_CommonNeedsAllPages` |

## (3) `worker` → Meta Graph API

| STRIDE | Mối đe dọa | Cách xử lý | Test |
|---|---|---|---|
| I | Token Page lộ qua database, bản sao lưu, API, log, thông báo lỗi | `credential_enc` (`fieldcrypt`, AAD gắn tenant + dòng); view `channel_accounts_v` không có cột token; log, lỗi Graph đã che | `TestCredentials_RoundTripAndBinding`, `TestCredentials_StoreRejects`, `TestDo_NoSecretsInLogsAndMessages`, `TestGraphError_MessageScrubbed`, `TestDo_MetricsLabelWithoutIDs` |
| S | Token bị lấy cắp dùng từ nơi khác | Mọi lời gọi có `appsecret_proof` (Meta từ chối token thiếu proof khi bật "Require App Secret") | `TestDo_VersionPrefixAndProof`, `TestDo_WrongAppSecretRejected` |
| T | Gửi hai lần (worker chết sau khi Meta nhận, mạng treo) | Trạng thái `in_doubt`, không tự gửi lại; chờ echo 60 s theo `metadata`/`mid`; tra kết quả; hạn 3 phút không gửi trễ | `TestInDoubt_CH18`, `TestInDoubt_EchoWithin60s`, `TestInDoubt_WorkerCrash`, `TestExpired_CH19`, `TestResend_Concurrent`, `TestDo_SentState`, `TestDo_HangAfterReadTimesOut`, `TestReplyToComment_InDoubtLookup` |
| D | Vượt giới hạn của Meta, Page bị chặn | Thùng token theo Page, đọc header mức dùng, `Retry-After`, `blocked_until` | `TestBuckets_Concurrent`, `TestBuckets_Blocked`, `TestDo_RateLimitRetryAfterFromUsage`, `TestSendRateLimit`, `TestSendSlowDown` |
| R | Không biết vì sao Meta từ chối | Phân loại lỗi Graph (tạm / vĩnh viễn / mất quyền / chính sách) lưu vào lệnh; cảnh báo `channel-auth`, `meta-policy` | `TestClassify`, `TestSendMetaErrors`, `TestHealth_TokenRevoked`, `TestHealth_PolicyEnforcement`, `TestReportTokenInvalid` |

## (4) Tải tệp đính kèm từ Meta

| STRIDE | Mối đe dọa | Cách xử lý | Test |
|---|---|---|---|
| S/E | SSRF: URL trong webhook trỏ vào mạng nội bộ (`169.254.169.254`, `postgres:5432`) | Chỉ `https`, chỉ host CDN của Meta; chặn IP riêng/loopback/link-local ở bước nối (cả sau chuyển hướng, DNS rebinding) | `TestDownload_Rejects`, `TestBlockedIP` |
| D | Tệp khổng lồ, máy chủ treo | Giới hạn byte khi đọc, hạn thời gian | `TestDownload_SizeLimit`, `TestInboundImage_Truncated`, `TestInboundAttachment_EmptyFile` |
| T/E | Tệp độc (HTML, SVG khai là ảnh) | Nhận dạng loại từ nội dung, không tin header; ảnh mã hóa lại qua `imager` (mục đích `chat_image` riêng tư); tệp khác không bao giờ mở trực tiếp | `TestDownload_FromMetaHostSniffsType`, `TestInboundAttachment_Refusals`, `TestProcessPrivate_RejectsNonImage`, `TestProcessPrivate_NeverInPublicBucket` |

## (5) Tên miền tệp (mở tệp HTML khách gửi)

| STRIDE | Mối đe dọa | Cách xử lý | Test |
|---|---|---|---|
| E | Khách gửi tệp `.html`; nhân viên mở → chạy script trên tên miền của CRM | Tệp ở bucket riêng tư, đường dẫn ký sẵn có hạn trên tên miền kho tệp (khác tên miền console); tệp không phải ảnh luôn có `Content-Disposition: attachment` (tên hệ thống sinh `tep-<băm>.<đuôi>`), nên trình duyệt tải về, không hiển thị | `TestGetAttachmentURL` (đường dẫn có `attachment`), `TestProcessPrivate_SignedExpiringLink` |
| I | Đường dẫn tệp bị chia sẻ ra ngoài | Hạn ngắn (`AttachmentURLTTL`); hỏi lại quyền theo Page mỗi lần ký | `TestGetAttachmentURL`, `TestAccess_NotFound` |

## (6) Luồng comment live

| STRIDE | Mối đe dọa | Cách xử lý | Test |
|---|---|---|---|
| T | Hai worker cùng đọc một phiên → comment ghi đôi, đọc lệch | Khóa phiên có hạn (lease) theo `owner`; nhả khi dừng êm; worker chết → worker khác nhận trong 15 s | `TestTwoWorkersNeverReadSameSession`, `TestLV10_CrashedWorkerReopensWithin15s`, `TestGracefulStopReleasesLease`, `TestLeaseConstants` |
| D | Luồng SSE đứt / im lặng → thiếu comment | Về đọc định kỳ; đối chiếu cuối phiên, thiếu → điền và cảnh báo `live-missing` | `TestLV02_StreamDropsAndWebhookStillExact`, `TestSilentStreamFallsBackToPoll`, `TestStream404FallsBackToPoll`, `TestReconcileFillsMissingAndAlerts` |
| T | Comment đến hai đường (webhook `feed` và SSE) | Một khóa chống trùng chung cho mọi đường (`Via`) | `TestLV02_StreamDropsAndWebhookStillExact`, `TestInbound_LiveStatusAndLiveComment` |

## (7) OAuth kết nối Page

| STRIDE | Mối đe dọa | Cách xử lý | Test |
|---|---|---|---|
| S | CSRF: kẻ khác dụ quản trị gắn Page của kẻ đó vào shop (`code` của kẻ khác) | `state` ngẫu nhiên gắn với người dùng và phiên, dùng một lần, có hạn; sai → `oauth_state_invalid` | `TestOAuth_StartAndAuthorizeURL`, `TestOAuth_StateAndPagesSession`, `TestChannelConnect_Exchange`, `TestChannelConnect_ExchangeConcurrent` |
| I | Mã `code` nằm lại trên thanh địa chỉ, lịch sử, header `Referer` | Màn callback đọc `code`, `state` rồi xóa khỏi thanh địa chỉ (`history.replaceState`) trước khi gọi API | `FacebookCallback.test.tsx` ("trước lời gọi đó location.search đã rỗng"; "tham số lỗi cũng bị xóa") |
| E | Gắn Page đã thuộc shop khác | `UNIQUE` toàn cục của route Page; trả lỗi riêng | `TestConnect_PageConnectedElsewhere`, `TestConnect_Reconnect` |
| I | Ngắt kết nối mà token còn | Ngắt xóa bản mã ngay cả khi Meta đã thu hồi token | `TestDisconnect`, `TestDisconnect_RevokedTokenStillCleared` |

## (8) Callback xóa dữ liệu của Meta

| STRIDE | Mối đe dọa | Cách xử lý | Test |
|---|---|---|---|
| S | Giả `signed_request` để xóa dữ liệu của khách bất kỳ | Kiểm HMAC-SHA256 với App Secret, thuật toán phải `HMAC-SHA256`, so hằng thời gian; sai → 400, không ghi | `TestParseSignedRequest_Rejects`, `TestParseSignedRequest_Valid`, `TestEndpoint_BadSignature`, `TestEndpoint_SignedOK` |
| R | Không chứng minh đã xóa | Mã xác nhận và trang trạng thái; yêu cầu lưu theo băm `user_id` | `TestEndpoint_StatusUnknown`, `TestUserRefHash`, `TestProcessor_AllTenantsThenCompleted` |
| D | Lỗi giữa chừng để lại dữ liệu nửa xóa | Mỗi tenant một transaction; lỗi → lùi và thử lại | `TestProcessor_DeleterErrorRollsBack`, `TestDeleteOAuthDataFor_AllTenants` |

## Rủi ro còn mở (ghi lại, không chặn M4)

- **Chờ server thử của hệ mới (Task 181):** payload thật của Meta (E3), `metadata` trên echo (E8), Standard Access (E1),
  system user (E6), luồng SSE thật (E4). Mọi test trên đây chạy với payload mẫu và máy chủ Meta giả `tools/fakegraph`.
- **Đổi khóa token Page:** chưa có việc tự ghi lại token mang `key_id` cũ (M3 cũng chỉ báo `needsRewrap`); đường hiện
  tại là kết nối lại Page (`docs/RUNBOOK.md`, mục "Đổi khóa mã hóa trường"). Ghi ở "Việc phát hiện thêm" của
  `docs/tasks/task-182.md`.
- Cấu hình "Require App Secret" của app Meta và luật giới hạn tần suất Cloudflare cho `/hooks/facebook*` làm khi dựng
  staging (Task 181, cần chủ shop).
