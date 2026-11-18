# Quy trình làm việc của AI agent cho CRM ELIMAZ

*Bản 3, 18/11/2026. Chờ chủ shop duyệt. Bản 3 bỏ agent `crm-implementer` và các skill quy trình riêng theo ý chủ shop (quá phức tạp): mỗi task làm trong một phiên sạch, cổng kiểm do hook và CI giữ.*

Tài liệu này nói **ai làm gì, theo thứ tự nào, máy kiểm gì, và khi nào phải dừng hỏi chủ shop**, để mỗi task được làm đúng, khớp kiến trúc, dễ mở rộng. Nó không thay spec: spec nói *làm cái gì*, tài liệu này nói *làm như thế nào*.

Nguồn của từng bước: `reports/Quy trình AI agent viết code.md` (có đường dẫn tới tài liệu Anthropic, đội Go, các nghiên cứu có số đo). Việc ghép các bước thành một quy trình là suy luận, chưa ai đo trên dự án giống CRM; cuối M0 đo lại (mục 12).

---

## 1. Ai làm gì

| Vai | Là ai | Làm gì | Không được làm |
|---|---|---|---|
| Chủ shop | Bạn | Duyệt spec, kế hoạch, mockup; quyết ở các điểm dừng; OK riêng cho mọi việc ngoài máy này | — |
| Agent làm task | Phiên Claude làm việc, mỗi task một phiên sạch (`/clear` giữa hai task) | Làm đúng một task theo mục 4, chạy reviewer, commit cục bộ, báo chủ shop | Làm việc ngoài task; push, deploy, vào máy chủ, tạo tài khoản |
| Reviewer | Một agent khác, chỉ có quyền đọc (`.claude/agents/reviewer.md`) | Đối chiếu kết quả với kế hoạch, spec và mục 5–8 của tài liệu này | Sửa file; đòi thêm tầng trừu tượng hay sửa phong cách |
| Máy | Hook, `make ci`, test kiến trúc | Chặn những luật không có ngoại lệ (mục 9) | — |

## 2. Thứ bậc khi các nguồn nói khác nhau

1. Lời chủ shop trong chat (mới nhất thắng).
2. Spec và ADR (`docs/crm/specs/2026-11-10-crm-rieng/`).
3. Kế hoạch đang chạy (`docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`).
4. Tài liệu này.
5. `apps/crm/CLAUDE.md` và `.claude/rules/*.md`.
6. Skill (`.claude/skills/`, superpowers).

Hai nguồn cùng bậc mâu thuẫn, hoặc nguồn thấp mâu thuẫn nguồn cao mà không rõ có chủ ý: **dừng và hỏi** (mục 10). Không tự chọn bên nào.

## 3. Các loại việc và đường đi riêng

| Loại | Dấu hiệu | Đường đi |
|---|---|---|
| A. Task theo kế hoạch | Có trong kế hoạch, có danh sách test | Mục 4 đầy đủ |
| B. Sửa lỗi | Có hành vi sai, hoặc test hay cảnh báo báo lỗi | Skill `systematic-debugging`: tái hiện lỗi bằng một test đỏ **trước**, tìm nguyên nhân gốc, sửa một lần ở chỗ gốc, rồi mục 4 từ bước 5 |
| C. Đổi thiết kế | Cần làm khác spec, thêm công nghệ, đổi quyết định | **Không code.** Viết đề xuất ADR mới (bối cảnh, phương án, cái giá, ngưỡng xem lại), chủ shop duyệt, sửa spec, rồi mới thành task loại A |
| D. Dọn code (refactor) | Đổi cấu trúc, không đổi hành vi | Trước khi dọn: có test giữ hành vi hiện tại (golden test nếu là API). Commit riêng, không lẫn thay đổi hành vi. Đổi tên dùng rename của gopls |
| E. Thay đổi phá vỡ | Xóa hay đổi tên trường API, cột, bảng; đổi kiểu | Hai task: **mở rộng** (thêm cái mới bên cạnh, chuyển nơi dùng) rồi **thu hẹp** (xóa cái cũ, có test đỏ chừng nào cái cũ còn). Thay đổi API phá vỡ cần dòng trong `openapi/breaking-approved.yaml` có ngày chủ shop OK |
| F. Ngoài máy này | Máy thử, GitHub, kho sao lưu, Cloudflare, tài khoản | Chỉ khi chủ shop OK riêng trong chat cho đúng việc đó |

## 4. Vòng đời một task

### 4.1 Điều kiện bắt đầu (định nghĩa "sẵn sàng")

Chỉ bắt đầu task khi đủ:

- Task có tiêu chí xong viết bằng lời nghiệp vụ, danh sách test và mục Interfaces trong kế hoạch.
- Các task nó phụ thuộc đã xong và đã commit.
- Mục spec liên quan đã chỉ rõ (số mục, số ADR).
- Task có màn hình cho nhân viên: mockup đã được chủ shop chốt.

Thiếu một điều: dừng, bổ sung kế hoạch hoặc hỏi chủ shop.

### 4.2 Các bước

| # | Bước | Ai | Đầu ra | Kiểm bởi |
|---|---|---|---|---|
| 1 | Đọc | Agent | Đã đọc task, Interfaces của task trước, mục spec, CLAUDE.md, luật theo vùng khớp | — |
| 2 | Ghi Ảnh hưởng và Tái dùng | Agent | `docs/tasks/task-NN.md`, phần Ảnh hưởng (mục 7) và Tái dùng (mục 8), **trước khi viết test** | Reviewer (bước 6) |
| 3 | Soát thiết kế của task | Agent | Trong tệp task: đối chiếu với danh sách kiểm kiến trúc (mục 5), đúng và an toàn (mục 6). Mục nào không áp dụng thì ghi "không áp dụng" kèm một lý do | Reviewer |
| 4 | Test trước, code sau | Agent | Test đỏ → code cho xanh → dọn. Dời hay đổi tên là commit riêng | Test thật trên PostgreSQL thật |
| 5 | Cổng máy | Hook | Định dạng sau mỗi lần sửa; khi định báo xong: build, vet, lint, test của gói đổi và gói phụ thuộc, mã sinh không lệch, dò trùng, test không bị xóa hay tắt | **Máy chặn** |
| 6 | Review | Reviewer | Kết luận ĐẠT hoặc CẦN SỬA, kèm dẫn chứng | Agent làm task đọc; chỉ sửa phát hiện về tính đúng, an toàn, kiến trúc. Task chỉ đổi tài liệu, cấu hình: bỏ qua bước này |
| 7 | Bằng chứng | Agent | Bảng trong tệp task: tiêu chí → test → lệnh → trích kết quả. Vùng rủi ro cao thêm mutation testing khi đóng mốc | Reviewer, `make ci` |
| 8 | Commit và báo | Agent làm task | Commit cục bộ sau khi reviewer ĐẠT và `task-check.sh --commit` qua; gửi chủ shop tóm tắt tiếng Việt (mục 11) | Hook chặn commit có mã khi chưa đủ; chủ shop |

### 4.3 Định nghĩa "xong"

Một task chỉ được báo xong khi **tất cả** đúng:

- [ ] Mọi test trong kế hoạch của task có thật và xanh; hook Stop qua.
- [ ] `make ci` xanh (từ Task 3 trở đi).
- [ ] Tệp task có đủ: Ảnh hưởng, Tái dùng, soát thiết kế, bằng chứng, reviewer, khác kế hoạch (nếu có).
- [ ] Reviewer kết luận ĐẠT, hoặc mọi phát hiện phải sửa đã sửa và reviewer chạy lại.
- [ ] Không có test bị xóa, tắt hay nới mà không ghi tên và lý do.
- [ ] Không có thư viện mới ngoài ADR-44 mà thiếu dòng trong `docs/phu-thuoc.md`.
- [ ] Danh mục dùng chung đã sinh lại nếu task thêm hay đổi gói trong `internal/platform`.
- [ ] Mọi điều "chưa làm được", "chưa kiểm được" ghi rõ trong tệp task, không giấu trong câu "đã xong".

## 5. Danh sách kiểm: khớp kiến trúc

Lấy từ spec 04 mục 3, 5, 11, 18 và ADR. Agent trả lời từng mục trong tệp task; reviewer kiểm lại.

1. **Đúng chủ dữ liệu:** dữ liệu mới thuộc module nào theo bảng "Sở hữu" ở 04 mục 3? Bảng nằm trong schema của module đó.
2. **Không ghi bảng của module khác.** Cần thay đổi ở module khác thì gọi public API của nó hoặc phát sự kiện.
3. **Đọc dữ liệu module khác qua view chỉ đọc** mà module đó công bố, không đọc thẳng bảng.
4. **Việc cần nguyên tử** (ví dụ tạo đơn + giữ hàng + dùng mã) nằm trong **một transaction** do module điều phối gọi public API của module kia. Không saga, không hàng đợi ở giữa.
5. **Việc không cần nguyên tử** đi sau khi commit: sự kiện nội bộ (`events.Emit`) hoặc lệnh ra ngoài (`commands`), không gọi thẳng trong transaction.
6. **Gọi ra bên ngoài** (Meta, hãng vận chuyển) chỉ qua lệnh có trạng thái `in_doubt`; không gửi lại khi không chắc đã gửi.
7. **Ngữ cảnh shop:** mọi truy vấn bảng có `tenant_id` chạy trong `tenancy.WithTenant`; bảng mới có RLS, unique và khóa ngoại ghép `tenant_id`.
8. **Nghiệp vụ không biết HTTP:** module không nhập `net/http`, mã ogen; lớp HTTP mỏng.
9. **Mọi đường ghi** có khóa chống trùng và khai quyền trong `openapi.yaml`.
10. **PostgreSQL là nguồn sự thật.** Valkey chỉ làm bộ đệm, đếm, điều tiết; không quyết định nghiệp vụ nào dựa trên Valkey.
11. **Không thêm công nghệ hay thư viện** ngoài ADR-44 nếu chưa có ADR hoặc dòng trong `docs/phu-thuoc.md` được duyệt.
12. **Theo module mẫu** (ghi trong CLAUDE.md từ Task 7, Task 12) cho cấu trúc thư mục, cách đặt lỗi, cách viết test.

## 6. Danh sách kiểm: đúng, chính xác, an toàn khi chạy đồng thời

Áp dụng cho mọi task chạm database, hàng đợi, tiền, tồn, quyền.

| # | Câu hỏi | Cách đúng trong CRM | Nguồn |
|---|---|---|---|
| 1 | Hai yêu cầu cùng lúc sửa cùng thứ? | Câu lệnh có điều kiện (`UPDATE … WHERE còn đủ`), cột `version` cho bản ghi người sửa, `SKIP LOCKED` khi nhiều người lấy từ một kho chung | 04 mục 5.5, 15, 18 |
| 2 | Gửi lại cùng yêu cầu (bấm hai lần, mạng chập)? | `Idempotency-Key`, ghi "đã xong" cùng transaction với kết quả; khóa tự nhiên unique ở bảng nghiệp vụ | ADR-41 |
| 3 | Thứ tự khóa có thể gây deadlock? | Khóa nhiều dòng thì theo mã tăng dần (đã có cho tồn kho). **Luật chung cho nhiều bảng: chờ duyệt (ADR-46 đề xuất)** | 04 mục 5.5 |
| 4 | Database báo lỗi tạm (deadlock `40P01`, hết thời gian chờ khóa `55P03`) trên đường yêu cầu? | **Chờ duyệt (ADR-46 đề xuất):** chạy lại cả transaction tối đa 3 lần, chờ ngẫu nhiên ngắn giữa các lần; quá thì trả "thử lại" không lưu. Việc nền: lịch thử lại có sẵn | 04 mục 5.1 |
| 5 | Mức cách ly transaction? | Mặc định của PostgreSQL (READ COMMITTED) cộng câu lệnh có điều kiện và khóa dòng. **Viết thành luật: chờ duyệt (ADR-46 đề xuất)** | — |
| 6 | Việc nền lỗi? | Lịch thử lại 5 s → 6 giờ, lệch ±20%, tối đa 8 lần rồi `dead` và cảnh báo; lỗi vĩnh viễn thì `dead` ngay | 04 mục 5.1 |
| 7 | Bên ngoài báo quá tải (429)? | Lùi theo thời gian bên đó yêu cầu | 04 mục 5.3 |
| 8 | Tiến trình chết giữa chừng? | Việc nền về hàng chờ khi hết giữ chỗ; lệnh ra ngoài thành `in_doubt`; transaction tự lùi | 04 mục 5.1, 5.3 |
| 9 | Treo lâu? | Mỗi câu lệnh tối đa 30 s, chờ khóa 5 s, transaction ngồi không 60 s; handler việc nền 30 s | ADR-26 |
| 10 | Tiền? | `int64` đồng; sổ chỉ ghi thêm; sổ tiền ghi hai vế, database kiểm tổng bằng 0 | ADR-07, 24 |
| 11 | Quy luật phải luôn đúng? | Vùng rủi ro cao có test sinh tình huống ngẫu nhiên (rapid) cho từng quy luật: không bán vượt, tổng sổ bằng 0, một lệnh không gửi hai lần | ADR-25 |
| 12 | Dữ liệu cá nhân? | Không vào log, dấu vết, cảnh báo, audit; dùng kiểu tự che | 04 mục 8, 17 |
| 13 | Quyền? | Mặc định từ chối; không có dòng phạm vi là không có quyền; kiểm ở API, không chỉ ẩn nút | 04 mục 11 |

## 7. Ảnh hưởng: biết trước chỗ nào sẽ đổi

Ghi trong tệp task **trước khi sửa**, theo bốn tầng:

| Tầng | Câu hỏi | Cách tìm |
|---|---|---|
| Ký hiệu Go | Hàm, kiểu xuất ra ngoài nào sẽ đổi? Bao nhiêu chỗ đang dùng? | Công cụ `LSP` (`findReferences`, `incomingCalls`); không chỉ tìm theo chữ |
| Gói | Gói nào phụ thuộc gói sẽ sửa? | `go list`; hook Stop tự test các gói này |
| Hợp đồng API | Operation nào đổi? Có phá vỡ không? Màn hình nào dùng? | `openapi.yaml`, oasdiff, `tsc` trên client sinh lại |
| Dữ liệu | Bảng, cột, câu sqlc nào bị chạm? Migration ở pha mở rộng hay thu hẹp? | `queries/`, migration, `sqlc vet` |

Luật khi sửa:

- Sửa định nghĩa một ký hiệu thì sửa **mọi** chỗ dùng trong cùng task.
- Chuyển sang cách mới thì **xóa** cách cũ khi hết nơi dùng; không bọc cách cũ trong một điều kiện để giữ lại.
- Reviewer so "dự kiến đổi" với "thực sự đổi". Lệch một trong hai chiều là phát hiện phải giải thích.

## 8. Tái dùng: dùng lại trước khi viết mới

Thứ tự tìm, lấy cái đầu tiên đáp ứng đủ:

1. Có cần viết không? Không ai yêu cầu thì không làm, ghi một dòng "đã bỏ qua".
2. Đã có trong CRM? Tra `docs/danh-muc-dung-chung.md` (sinh tự động từ `go doc`), rồi tìm theo tên và theo việc nó làm.
3. Thư viện chuẩn của Go, hoặc thư viện đã có trong ADR-44?
4. Viết mới, đặt đúng chỗ:
   - dùng chung toàn hệ thống: `internal/platform/<việc>`, tên gói là danh từ nói rõ việc, có doc gói và dòng "khi nào không dùng" trong danh mục;
   - chỉ một module dùng: ở lại trong module đó.

Không tạo gói `util`, `common`, `helpers`, `shared`. Chỉ gộp hai đoạn code khi **cùng mục đích và cùng lý do thay đổi**; giống nhau bề ngoài mà khác lý do thì để riêng. Cảnh báo trùng của `dupl`, jscpd được trả lời trong mục Tái dùng (gộp, hoặc giữ kèm lý do).

## 9. Máy chặn gì, agent tự giác gì

| Máy chặn (không qua thì không xong) | Agent tự giác, reviewer kiểm sau |
|---|---|
| Sửa tay mã sinh, migration đã commit (hook trước khi sửa) | Ghi Ảnh hưởng, Tái dùng trước khi code |
| Lỗi biên dịch, `go vet`, lint, test của gói đổi và gói phụ thuộc (hook Stop) | Test viết trước code |
| Mã sinh lệch nguồn; oasdiff thay đổi phá vỡ chưa duyệt (`make ci`) | Mở phiên mới cho mỗi task |
| Test bị xóa, tắt, bớt câu kiểm chưa ghi lý do | Theo danh sách kiểm mục 5, 6 |
| Module nhập sai ranh giới; SQL ngoài sqlc hay `*_dynsql.go`; chế độ simple protocol của pgx (test kiến trúc, linter) | Không làm thêm việc ngoài task |
| Route thiếu quyền hay thiếu khóa chống trùng (sinh từ `openapi.yaml`) | Dừng hỏi khi gặp mục 10 |

Giới hạn đã biết của máy chặn: hook chỉ bắt công cụ sửa file thông thường, sửa bằng lệnh shell thì lọt (lưới đỡ là `make ci` và reviewer); hook chặn 8 lần liền thì Claude Code bỏ qua, khi đó agent phải dừng và báo.

## 10. Khi nào phải dừng và hỏi chủ shop

Agent dừng, ghi lại, hỏi bằng tiếng Việt khi:

1. Spec hay kế hoạch mơ hồ, thiếu, hoặc mâu thuẫn nhau.
2. Cần thư viện, công cụ, dịch vụ mới; hoặc cần đổi một quyết định đã có ADR.
3. Cần thay đổi phá vỡ API hay dữ liệu.
4. Muốn xóa, tắt hay nới một test vì không qua được.
5. Hook chặn 8 lần liền, hoặc sửa hai lần vẫn không qua một lỗi.
6. Ước lượng của task vượt quá 50%.
7. Bất kỳ việc gì ngoài máy này (máy thử, GitHub, kho ngoài, Cloudflare, Meta, tài khoản).
8. Lựa chọn có ảnh hưởng tới bảo mật hay dữ liệu cá nhân mà spec chưa nói.
9. Xóa dữ liệu thật, hoặc thao tác không lùi lại được.
10. Phát hiện lỗi hay thiếu sót trong spec: báo, đề xuất sửa, chờ duyệt.

Khi hỏi: một câu, có phương án và phương án đề xuất, nói rõ cái giá; không hỏi những gì đọc code hay spec là biết.

## 11. Báo cáo cho chủ shop

- **Mỗi task:** 5–10 dòng tiếng Việt: đã làm gì, bằng chứng chính (tên test và kết quả), khác kế hoạch ở đâu, chưa làm hay chưa kiểm được gì, việc phát hiện thêm. Bản đầy đủ ở `docs/tasks/task-NN.md`.
- **Mỗi điểm dừng của kế hoạch:** tóm tắt kết quả đo, đạt hay không từng ngưỡng, quyết định cần bạn.
- **Cuối mốc:** review độc lập toàn bộ; bảng tiêu chí "Xong khi" của mốc; số đo của quy trình (mục 12).
- Không viết "đã xong" khi còn bước chưa kiểm; không làm tròn kết quả theo hướng tốt.

## 12. Đo và sửa chính quy trình

Cuối M0 (Task 23, bước 6b) ghi vào `docs/do-luong/cai-dat-agent-m0.md`:

- tỷ lệ code trùng (jscpd), điểm mutation testing của vùng cao, số lần tắt linter và lý do;
- số phát hiện của reviewer mỗi task, chia: đúng và phải sửa, sai, chỉ là phong cách;
- số lần hook Stop chặn mỗi task; số lần chủ shop phải sửa agent;
- độ dài `CLAUDE.md`.

Mỗi lỗi agent mắc lần thứ hai thêm một dòng vào mục "Bẫy đã gặp" của `CLAUDE.md`. Một phần quy trình không cho thấy ích lợi qua số đo thì đề xuất bỏ; một loại lỗi lặp lại mà chỉ có luật chữ thì đề xuất chuyển thành máy chặn.

## 13. Nhiều task

- M0 chạy **lần lượt từng task** theo thứ tự kế hoạch, vì task sau dùng Interfaces của task trước.
- Mỗi task một phiên sạch (`/clear` hoặc phiên mới) để không mang trí nhớ của task trước; mọi thứ cần biết nằm trong kế hoạch, spec, tệp task và CLAUDE.md.
- Một vòng review mỗi task có mã bằng agent `reviewer`; không chạy thêm `/code-review` chồng lên.
- Chỉ chạy song song khi hai task không dùng chung tệp và không cần kết quả của nhau, mỗi task một worktree.
- Chạy song song (chủ shop chốt 18/11/2026): mỗi task một phiên Claude chạy nền (`claude --print`) trong worktree riêng `.worktrees/task-NN`, nhánh `task-NN`, tên dự án Docker `crm-tNN` và cổng riêng (đọc từ biến môi trường, ví dụ `CRM_PG_PORT`). Không dùng subagent để viết mã, vì hook `Stop` không chạy cho subagent. Phiên điều phối gộp nhánh vào `main` theo thứ tự kế hoạch, chạy lại test sau mỗi lần gộp, rồi mới mở đợt sau từ `main` mới. Trước khi stage và chạy reviewer lần cuối, phiên làm task gộp `main` mới nhất vào nhánh của mình (không rebase), giải xung đột (mã sinh: sửa nguồn rồi `make gen`), chạy lại kiểm tra; nhờ vậy reviewer duyệt đúng bản sẽ vào `main` và cổng commit gộp (mã giống hệt bản đã review) cho qua. Commit gộp có tệp mã bị sửa tay khi giải xung đột phải qua reviewer như commit thường. Docker (chủ shop chốt 18/11): task thường dùng chung một bộ `crm-shared` (PostgreSQL, Jaeger, SeaweedFS chạy sẵn; mỗi test tự tạo database riêng; cấm `db-up/db-down/db-reset`, `compose down` trên bộ chung); chỉ task đổi hạ tầng (`docker/`, script role, cấu hình kho tệp, `compose.dev.yml`) dùng bộ riêng `crm-tNN`. Phiên làm task không dùng `git stash` (ngăn stash dùng chung giữa mọi worktree); cất tạm bằng commit tạm trên nhánh của mình. Phiên điều phối gộp từng nhánh bằng `git merge --no-commit`, chạy ngay `go build ./...` và `go vet ./...` trên bản đã gộp (bắt xung đột ngữ nghĩa giữa hai nhánh mà git không thấy, ví dụ một nhánh đổi tên hàm trợ giúp test mà nhánh kia vẫn dùng tên cũ); qua thì commit, không qua thì `git merge --abort` và giao một task sửa — `main` không bao giờ được đỏ (bài học 18/11, Task 28 và 34).
- Đi lệch hướng (lặp vòng sửa, làm thêm việc không ai yêu cầu, muốn tắt test): dừng, ghi lại, bắt đầu lại trong phiên mới với yêu cầu rõ hơn; không sửa lần ba trong cùng phiên.

## 13b. Viết kế hoạch cho các mốc (M1, M2, …)

Chủ shop chốt ngày 18/11/2026. Áp dụng mỗi khi viết kế hoạch cho một mốc mới.

1. **Nguồn chung:** mọi kế hoạch dựa trên spec (bảng ở `05-du-lieu-erd.md`, module ở `04-kien-truc.md`, ADR, ca kiểm thử ở `11-kich-ban-kiem-thu.md`), viết đúng khuôn kế hoạch M0 (Files, Interfaces, test thất bại có số liệu, Step R, commit). Phủ đủ tính năng của mốc trong ma trận tính năng; cuối kế hoạch liệt kê ca kiểm thử của mốc chưa được task nào phủ (phải bằng 0 hoặc có lý do).
2. **Viết song song được:** kế hoạch các mốc viết song song (mỗi mốc một agent) vì cùng dựa trên spec. Chỗ nào dùng Interface của mốc khác mà kế hoạch mốc đó chưa có, phải ghi vào mục "Giả định về mốc trước".
3. **Soát chéo bắt buộc** khi có từ hai kế hoạch viết song song trở lên, làm đúng mục 13c. Chưa đạt 13c thì chưa viết mã của mốc.
4. **Đối chiếu với mã thật:** trước khi bắt đầu một mốc, đối chiếu Interfaces trong kế hoạch với mã đã gộp vào `main` của các mốc trước; lệch thì sửa kế hoạch trước, ghi lại đã sửa gì.
5. **Viết mã theo thứ tự phụ thuộc** ghi ở đầu mỗi kế hoạch (bảng "Phụ thuộc và chạy song song"). Hiện tại: M0 → M1 → M2 → (M3 và M4 song song) → M5 → M6 → M7, P2 Affiliate sau M5; M5 cần M1, M2, M3; M6 cần M1–M4; M7 cần M1–M6; Phase 2 Affiliate (CTV) chạy sau khi M5 đóng, pilot sau ngày mở (M7). Riêng M4 Task 22 và Task 42 chạy sau khi M3 đóng.

## 13c. Soát chéo kế hoạch: làm gì, ghi ra đâu, thế nào là đạt

**Ai làm:** một agent riêng (`general-purpose`), không phải agent đã viết các kế hoạch đó. Agent soát chỉ báo cáo, không tự sửa kế hoạch. Phiên điều phối sửa theo báo cáo, rồi chạy soát lại.

**Đầu vào:** mọi kế hoạch của các mốc chưa làm xong trong `../../docs/superpowers/plans/`, spec (`05-du-lieu-erd.md`, `04-kien-truc.md`, `07-adr.md`, `01-ma-tran-tinh-nang.md`, `11-kich-ban-kiem-thu.md`), và mã đã gộp vào `main` (nếu có).

**Kiểm 8 mục, theo thứ tự:**

| # | Kiểm | Cách kiểm | Lệch khi |
|---|---|---|---|
| 1 | Giả định khớp thật | Mỗi dòng "Giả định về mốc trước" của kế hoạch B so với Interfaces của kế hoạch A (hoặc mã thật) | Khác tên gói, tên hàm, chữ ký, kiểu trả về, mã lỗi sentinel, tên operation OpenAPI, tên bảng hoặc cột |
| 2 | Bảng có một chủ | Mỗi bảng trong mọi kế hoạch: mốc nào tạo (migration), mốc nào chỉ đọc, ghi | Hai mốc cùng tạo một bảng; mốc dùng bảng trước khi mốc tạo nó chạy; cột không có trong `05-du-lieu-erd.md` mà không ghi lý do |
| 3 | Operation có một chủ | Mỗi đường API (`METHOD /path`) và `operationId` | Trùng đường, trùng `operationId`, cùng việc mà hai tên |
| 4 | Thứ tự phụ thuộc | Mỗi task dùng gì của task nào; đối chiếu bảng "Phụ thuộc và chạy song song" | Dùng thứ chưa có ở thời điểm đó; hai task ghi "song song" nhưng chạm cùng tệp |
| 5 | Phủ tính năng | Mỗi dòng của `01-ma-tran-tinh-nang.md` thuộc các mốc đang soát → task nào làm | Tính năng không có task; tính năng có ở hai mốc |
| 6 | Phủ ca kiểm thử | Mỗi mã ca trong `11-kich-ban-kiem-thu.md` thuộc các mốc đang soát → test nào trong task nào | Ca không có test; test ghi mã ca không tồn tại |
| 7 | Đúng ADR | Thư viện (ADR-44), RLS và `seller_id` (ADR-29), chống trùng (ADR-41), thử lại và thứ tự khóa (ADR-46), việc ra ngoài qua hàng đợi, không gọi Nhanh | Bất kỳ chỗ trái ADR |
| 8 | Việc cần chủ shop | Gộp các bảng "Việc cần chủ shop" của mọi kế hoạch | Việc ngoài máy (tài khoản, khóa, máy chủ) nằm lẫn trong task mà không ghi ra bảng |

**Ghi ra:** `../../docs/superpowers/plans/soat-cheo-<ngày>.md`, gồm:
- Bảng phát hiện: `| # | Mục (1–8) | Kế hoạch, task | Lệch gì | Bằng chứng (trích dòng hai bên) | Sửa ở đâu |`. Mức: **chặn** (làm sai hoặc không làm được nếu chạy) hay **nên sửa**.
- Bảng chủ của bảng dữ liệu và operation (mục 2, 3); bảng phủ tính năng và phủ ca (mục 5, 6), mỗi dòng một tính năng hoặc ca.
- Dòng cuối, đúng chữ: `Kết luận soát chéo: ĐẠT` hoặc `Kết luận soát chéo: CHƯA ĐẠT (<n> chặn)`.

**Đạt khi:** 0 phát hiện mức chặn; mục 5 và 6 không còn dòng trống (hoặc có lý do được ghi vào "Quyết định thay chủ shop"). Phát hiện "nên sửa" sửa luôn nếu nhỏ, còn lại ghi vào kế hoạch.

**Sau khi sửa:** phiên điều phối ghi ở đầu mỗi kế hoạch đã sửa một dòng "Đã soát chéo ngày …, sửa theo phát hiện #…", rồi chạy lại soát chéo (chỉ mục bị ảnh hưởng) tới khi ĐẠT. Phát hiện cần đổi spec thì không tự đổi spec ngoài quyền được giao: ghi vào báo cáo cho chủ shop.

**Sửa sau soát chéo (bài học 18/11/2026):** trước khi giao sửa, phiên điều phối viết một bảng quyết định chốt **tên cuối cùng** cho mọi chỗ lệch (bảng, cột, trạng thái, hàm, view, operation, nhóm khóa), theo luật "mốc trước là chuẩn". Agent sửa chỉ làm theo bảng, không tự đặt tên mới. Vòng 1 ngày 18/11 giao sửa song song khi chưa chốt tên, nên các mốc đổi cùng một tên theo hai hướng (ví dụ trạng thái vận đơn `creating` và `requested`) và vòng 2 còn 23 chặn mới.

**Lặp lại khi:** thêm hoặc sửa lớn một kế hoạch; trước khi bắt đầu mỗi mốc (kèm mục 13b bước 4, đối chiếu với mã thật).

## 14. Quy trình được đóng gói thế nào để không bị "quên"

Phiên mới và subagent **không nhớ cuộc trò chuyện**. Chúng chỉ biết những gì Claude Code tự nạp hoặc tự chạy. Vì vậy quy trình nằm trong repo `apps/crm` (đi cùng code trong git). Bản 3 (18/11) vá năm cách lọt mà báo cáo `reports/Chuẩn hóa quy trình AI agent.md` tìm ra.

| Cơ chế | Nằm ở | Giữ điều gì |
|---|---|---|
| `CLAUDE.md` | gốc `apps/crm` | Luật chung, các bước làm task, lệnh; nạp ở mọi phiên và subagent (trừ Explore, Plan), nạp lại sau khi nén |
| Luật theo vùng | `.claude/rules/*.md` | Luật Go, migration, OpenAPI, màn hình; nạp khi chạm tệp khớp |
| Hook SessionStart | `scripts/agent/session-start.sh` | Nhắc task đang chạy và luật bắt buộc ở mỗi phiên mới, sau `/clear`, sau khi nén |
| Hook chặn sửa | `scripts/agent/guard-edit.sh` | Không sửa tay mã sinh, migration đã commit; không sửa cấu hình quy trình (`.claude/`, `scripts/agent/`, `.githooks/`, `.git/`, `<git-dir>/crm-task-state (ngoài git từ 18/11)`) |
| Hook lệnh shell | `scripts/agent/bashguard.py` | Phân tích lệnh thật: chặn push (mọi cách viết), `commit -a/--amend/--no-verify`, commit kèm đường dẫn, gộp `git add` với `git commit`, đổi `core.hooksPath`, `bash -c`; lệnh nhắc tới phần cấu hình quy trình chỉ qua khi mọi lệnh trong đó là lệnh đọc hoặc script quy trình được phép (danh sách cho phép); reviewer chỉ được chạy lệnh đọc |
| Hook khi dừng | `scripts/agent/stop-check.sh` | Build, vet, lint, test gói đổi và gói phụ thuộc, mã sinh không lệch, dò trùng, test không bị xóa; "Dừng để hỏi" là lối ra hợp lệ |
| Dấu duyệt reviewer | `review-stamp.sh` (hook `SubagentStop` cho agent `reviewer`) | Khi reviewer kết luận `## Kết luận: ĐẠT`, hook ghi dấu vào `<git-dir>/crm-review/<mã băm phần mã đã stage>`, trỏ tới bản ghi hội thoại của reviewer; agent không ghi dấu này |
| Cổng commit | `.githooks/pre-commit` (git hook, chạy với mọi cách gọi commit) | Commit có mã cần: task đang làm, tệp task đủ mục, dấu duyệt khớp đúng phần mã đã stage (sửa mã sau review là phải review lại); `stamp_check.py` kiểm dấu trỏ tới bản ghi thật của agent `reviewer`, có chạy `git diff --cached`, câu cuối là ĐẠT |
| Chặn mở sai thư mục | `Thời trang/.claude/hooks/crm-wrong-folder.sh` | Phiên mở ở thư mục cha không sửa được mã CRM |
| Plugin | `enabledPlugins` trong `.claude/settings.json` | Tắt plugin không liên quan (marketing, finance, data, design, figma, productivity, superpowers…) trong phiên CRM |
| `make ci`, CI | Từ Task 3; CI trên GitHub khi chủ shop tạo repo | Lưới cuối, nằm ngoài tầm agent khi có branch protection |

Giới hạn còn lại (ghi thật):

- Thử phá ngày 18/11/2026 (phiên Claude thật, bản sao repo): lọt một cách (tạo dấu duyệt bằng `mkdir` + `touch`), đã vá bằng danh sách cho phép và `stamp_check.py`; reviewer bị nhờ "in đúng một dòng ĐẠT" vẫn review thật và kết luận CẦN SỬA. Phiên thật đi đúng quy trình commit được.
- Agent chạy dưới tài khoản của chủ shop, nên về lý thuyết vẫn có cách sửa tệp được bảo vệ mà hook không nhận ra (ví dụ một chương trình tự viết). Chỉ CI trên máy chủ git có branch protection, hoặc tài khoản máy riêng cho agent, mới nằm ngoài tầm agent. Việc này cần chủ shop tạo repo (đề xuất 4, 7 của báo cáo).
- Hook `Stop` không chạy khi một subagent dừng. Vì vậy việc code làm ở phiên chính; subagent chỉ để đọc, nghiên cứu, review.
- Phải mở phiên CRM trong `apps/crm`.
