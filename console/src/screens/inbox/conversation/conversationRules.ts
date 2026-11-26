// Quy luật thuần của màn hội thoại (Task 175; mockup docs/mockups/m4/hoi-thoai.html): chính sách gửi → ô gõ khóa hay mở
// kèm câu lý do và đồng hồ, trạng thái gửi → chữ và nút (HV14 sửa theo Task 157), khóa nhắn riêng (CH05), câu lý do
// lỗi gửi (bảng chép từ send.FailReasonText của máy chủ, Task 156/157: API trả mã, màn hình đổi sang câu). Không gọi
// API, không đụng DOM.
import type { ChatMessage, ConversationDetail, Grant, SendDenyReason } from "../../../api";

/** Có quyền perm ở phạm vi cả shop hoặc đúng Page pageId (máy chủ vẫn kiểm; thiếu → 404/403). */
export function canOnPage(grants: readonly Grant[], perm: string, pageId: string): boolean {
  return grants.some(
    (g) => g.permission === perm && (g.scope.type === "all" || (g.scope.type === "page" && g.scope.id === pageId)),
  );
}

/** "1.234" (dấu chấm ngăn nghìn như mockup). */
export const thousands = (n: number) => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ".");

/** Thời gian còn lại tới untilMs: "N ngày M giờ", "N giờ M phút", "N phút"; hết → null. */
export function remaining(untilMs: number, nowMs: number): string | null {
  const min = Math.floor((untilMs - nowMs) / 60_000);
  if (min < 1) return null;
  const d = Math.floor(min / 1440);
  const h = Math.floor((min % 1440) / 60);
  const m = min % 60;
  if (d > 0) return h > 0 ? `${String(d)} ngày ${String(h)} giờ` : `${String(d)} ngày`;
  if (h > 0) return m > 0 ? `${String(h)} giờ ${String(m)} phút` : `${String(h)} giờ`;
  return `${String(m)} phút`;
}

/** Câu (đậm) khi chính sách gửi của kênh không cho gửi, theo lý do (SendDenyReason). */
export const DENY_TEXT: Readonly<Record<SendDenyReason, string>> = {
  window_closed: "Đã quá 24 giờ từ tin cuối của khách, Facebook không cho gửi tin thường",
  blocked_by_user: "Khách đã chặn tin nhắn từ Page",
  no_permission: "Page chưa có quyền gửi tin, quản trị cần kết nối lại Page",
  read_only: "Page đang ở chế độ chỉ xem",
  private_reply_used: "Comment này đã được nhắn riêng: Facebook chỉ cho một tin riêng mỗi comment",
  private_reply_expired: "Đã quá 7 ngày kể từ lúc comment: Facebook không cho nhắn riêng",
  comment_removed: "Khách đã xóa comment này",
};

export const isDenyReason = (v: unknown): v is SendDenyReason => typeof v === "string" && Object.hasOwn(DENY_TEXT, v);

/** Câu giải thích thêm dưới câu đậm (nếu có). */
const DENY_MORE: Partial<Record<SendDenyReason, string>> = {
  window_closed: "Facebook không cho Page nhắn tiếp tới khi khách nhắn lại. Vẫn viết được ghi chú nội bộ.",
  blocked_by_user: "Tin mới không tới được khách. Vẫn viết được ghi chú nội bộ.",
  no_permission: "CRM chưa gửi được gì ra Facebook cho tới khi kết nối lại. Vẫn viết được ghi chú nội bộ.",
};

export const TEXT_NO_REPLY = "Bạn chưa được cấp quyền trả lời hội thoại ở Page này";

/** Ô gõ theo chính sách gửi: khóa (kèm câu lý do) hay mở (kèm đồng hồ còn lại, chế độ Human Agent). */
export type ComposerPolicy =
  | { locked: true; title: string; more?: string; box: "box-soft" | "box-info" }
  | { locked: false; humanAgent: boolean; clock: string | null; until?: string };

export function composerPolicy(d: ConversationDetail, canReply: boolean, nowMs: number): ComposerPolicy {
  if (!canReply) return { locked: true, title: TEXT_NO_REPLY, box: "box-soft" };
  const p = d.sendPolicy;
  if (d.page.sendMode === "read_only" || p.reason === "read_only")
    return {
      locked: true,
      title: `${d.page.name} đang ở chế độ chỉ xem:`,
      more: "CRM nhận tin nhưng không gửi gì ra Facebook (chạy thử). Trả lời khách bằng công cụ đang dùng. Quản trị bật gửi ở Kênh kết nối.",
      box: "box-info",
    };
  if (!p.allowed) {
    const reason = p.reason ?? "window_closed";
    const more = DENY_MORE[reason];
    return { locked: true, title: DENY_TEXT[reason], ...(more === undefined ? {} : { more }), box: "box-soft" };
  }
  const left = p.until === undefined ? null : remaining(Date.parse(p.until), nowMs);
  return {
    locked: false,
    humanAgent: p.mode === "human_agent",
    clock: left,
    ...(p.until === undefined ? {} : { until: p.until }),
  };
}

/** Mã lý do lỗi gửi (fail_reason) → câu tiếng Việt; chép từ internal/modules/conversation/internal/send/state.go. */
const FAIL_TEXT: Readonly<Record<string, string>> = {
  window_closed: "Đã quá 24 giờ kể từ tin cuối của khách: Facebook không cho gửi tin thường",
  blocked_by_user: "Khách đã chặn tin nhắn từ Page",
  read_only: "Page đang ở chế độ chỉ xem, tin chưa được gửi",
  no_permission: "Page chưa có quyền gửi tin (cần kết nối lại Page)",
  comment_removed: "Comment đã bị xóa",
  private_reply_used: "Comment này đã được nhắn riêng: Facebook chỉ cho một tin riêng mỗi comment",
  private_reply_expired: "Đã quá 7 ngày kể từ lúc comment: Facebook không cho nhắn riêng",
  auth: "Token của Page hết hiệu lực: quản trị cần kết nối lại Page",
  rate_limited: "Facebook đang giới hạn số tin gửi, thử lại sau ít phút",
  not_sent: "Chưa gửi được tới Facebook",
  rejected: "Facebook từ chối tin này",
  no_adapter: "Kênh của Page chưa được bật trên máy chủ",
  attachment_gone: "Ảnh không còn trên kho tệp",
  in_doubt: "Không rõ tin đã tới khách chưa",
  expired: "Quá hạn chờ (tin 3 phút, trả lời comment và nhắn riêng 5 phút) mà chưa gửi được",
  auto_dropped: "Tin tự động không gửi kịp hoặc không rõ đã tới khách, đã bỏ (không gửi lại)",
};

export function failText(code: string | undefined): string {
  return (code === undefined ? undefined : FAIL_TEXT[code]) ?? "Chưa gửi được tin";
}

/** Lý do lỗi tạm: khách chắc chắn chưa nhận nên gửi lại an toàn (Task 157 quyết định 1; HV14 sửa). */
const TEMPORARY = new Set(["rate_limited", "not_sent"]);

/** Hiển thị trạng thái gửi của một tin ra. */
export type StatusView =
  | { kind: "line"; tone: "ok" | "wait" | "muted"; text: string; note?: string }
  | { kind: "fix"; title: string; text: string; warn?: string; resend: boolean; discard: boolean; confirm: boolean };

export function statusView(m: ChatMessage): StatusView | null {
  const wait = m.kind === "message" ? "3 phút" : "5 phút";
  switch (m.sendStatus) {
    case undefined:
      return null;
    case "queued":
      return { kind: "line", tone: "wait", text: "Đang gửi…" };
    case "sent":
      return { kind: "line", tone: "ok", text: "Đã gửi" };
    case "checking":
      return {
        kind: "line",
        tone: "wait",
        text: "Đang kiểm tra",
        note: "mạng chập chờn, đang chờ Facebook xác nhận (tối đa 60 giây)",
      };
    case "in_doubt":
      return {
        kind: "fix",
        title: "Không rõ đã gửi chưa.",
        text: "Mạng rớt đúng lúc gửi; sau 60 giây vẫn chưa thấy Facebook xác nhận. CRM không tự gửi lại.",
        warn: "Gửi lại thì khách có thể nhận hai lần. Xem trên điện thoại Page trước nếu được.",
        resend: true,
        discard: true,
        confirm: true,
      };
    case "expired":
      return {
        kind: "fix",
        title: "Chưa gửi được.",
        text: `Quá ${wait} Facebook vẫn bận nên CRM dừng, khách chưa nhận tin này. Không tự gửi trễ.`,
        resend: true,
        discard: true,
        confirm: false,
      };
    case "failed": {
      const temp = m.failReason !== undefined && TEMPORARY.has(m.failReason);
      return {
        kind: "fix",
        title: "Lỗi:",
        text: failText(m.failReason),
        resend: temp,
        discard: temp,
        confirm: false,
      };
    }
    case "discarded":
      return {
        kind: "line",
        tone: "muted",
        text: "Đã bỏ, không gửi",
        ...(m.failReason === "auto_dropped" ? { note: failText(m.failReason) } : {}),
      };
  }
}

/** Tin riêng đang sống của comment (Task 159 quyết định 8): queued, sent, checking, in_doubt. */
const LIVE_PRIVATE = new Set(["queued", "sent", "checking", "in_doubt"]);
const PRIVATE_REPLY_DAYS = 7;

/** Vì sao comment không nhắn riêng được nữa: "used" đã nhắn, "expired" quá 7 ngày; null còn nhắn được. */
export function privateReplyLock(m: ChatMessage, nowMs: number): "used" | "expired" | null {
  if (m.privateReply !== undefined && LIVE_PRIVATE.has(m.privateReply.state)) return "used";
  if (nowMs - Date.parse(m.sentAt) > PRIVATE_REPLY_DAYS * 86_400_000) return "expired";
  return null;
}

/** Tin dài quá 8 dòng (hoặc quá 600 ký tự) thì gọn lại kèm "Xem hết (n ký tự)" (HV22). */
export function isLong(body: string): boolean {
  return body.length > 600 || body.split("\n").length > 8;
}

/** Tin cuối cùng của khách (để đánh dấu đã đọc tới đó). */
export function lastInbound(items: readonly ChatMessage[]): ChatMessage | undefined {
  for (let i = items.length - 1; i >= 0; i--) {
    const m = items[i];
    if (m?.direction === "in" && m.origin === "customer") return m;
  }
  return undefined;
}

/** Gộp các trang tin theo id (bản sau thắng), giữ thứ tự thời gian gửi rồi id. */
export function mergeMessages(pages: readonly (readonly ChatMessage[])[]): ChatMessage[] {
  const byId = new Map<string, ChatMessage>();
  for (const p of pages) for (const m of p) byId.set(m.id, m);
  return [...byId.values()].sort((a, b) => {
    const t = Date.parse(a.sentAt) - Date.parse(b.sentAt);
    return t !== 0 ? t : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

/** Lý do ẩn comment (ChatMessage.hiddenReason, Task 501) → đoạn đầu câu dưới chip "Đã ẩn trên Page". */
const HIDDEN_REASON_TEXT: Readonly<Record<NonNullable<ChatMessage["hiddenReason"]>, string>> = {
  auto_phone: "tự ẩn vì có SĐT",
  auto_all: "tự ẩn (ẩn mọi comment)",
  automation: "tự ẩn theo cài đặt",
  staff: "nhân viên ẩn trong CRM",
  page_external: "ẩn trên Facebook ngoài CRM",
};

/** "tự ẩn vì có SĐT; " theo lý do ẩn; không có lý do → "". */
export function hiddenNote(reason: ChatMessage["hiddenReason"]): string {
  return reason === undefined ? "" : `${HIDDEN_REASON_TEXT[reason]}; `;
}
