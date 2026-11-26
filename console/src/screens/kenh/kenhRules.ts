// Luật tại chỗ của màn Kênh kết nối và Cài đặt hộp chat (kế hoạch M4 Task 178; mockup docs/mockups/m4/cai-dat.html,
// ket-noi-page.html). Giới hạn chép từ openapi.yaml (AutoReplyCommentConfig, GreetingConfig, QuickReply, LiveSlot,
// UpdatePageSettingsRequest, CreateReplyTemplateRequest, CreateConversationTagRequest): màn báo tại ô trước khi gửi,
// máy chủ vẫn kiểm lại. Danh sách biến của mẫu chép từ máy chủ (conversation/internal/templates/render.go).
import type { ChannelAccount, ChannelFeatures, Grant } from "../../api";

export const REPLIES_MAX = 5;
export const REPLY_MAX_CHARS = 500;
export const QR_MAX = 13;
export const QR_TITLE_MAX = 20;
export const GREETING_MAX_CHARS = 640;
export const PRIVATE_TEXT_MAX_CHARS = 2000;
export const TEMPLATE_BODY_MAX = 2000;
export const TEMPLATE_TITLE_MAX = 100;
export const TAG_NAME_MAX = 40;
export const LIVE_SLOTS_MAX = 12;
/** Số trường webhook CRM đăng ký cho một Page Facebook (Task 149). */
export const WEBHOOK_FIELDS_TOTAL = 11;

/** Số ký tự như máy chủ đếm (rune, không phải đơn vị UTF-16). */
export const charCount = (s: string): number => Array.from(s).length;

/** Câu đếm "n/max" và có vượt không. */
export function counter(s: string, max: number): { text: string; over: boolean } {
  const n = charCount(s);
  return { text: `${String(n)}/${String(max)}`, over: n > max };
}

/** Lỗi độ dài của một ô chữ bắt buộc (bỏ khoảng trắng hai đầu): rỗng → emptyText, quá max → câu kèm số; null khi được. */
function textError(s: string, max: number, emptyText: string): string | null {
  const n = charCount(s.trim());
  if (n === 0) return emptyText;
  return n > max ? `Tối đa ${String(max)} ký tự (đang ${String(n)}).` : null;
}

/** Lỗi của một nút trả lời nhanh (rỗng, quá 20 ký tự). */
export const quickReplyError = (title: string) => textError(title, QR_TITLE_MAX, "Nhập chữ trên nút.");

/** Lỗi của một câu trả lời công khai. */
export const replyLineError = (s: string) => textError(s, REPLY_MAX_CHARS, "Nhập câu trả lời hoặc xóa dòng này.");

/** Số nguyên trong [min, max] từ ô nhập (chỉ chữ số); null khi không phải. */
export const intInRange = (raw: string, min: number, max: number): number | null =>
  /^\d{1,4}$/.test(raw.trim()) && Number(raw) >= min && Number(raw) <= max ? Number(raw) : null;

// --- Biến của mẫu trả lời (bảng dưới ô nội dung, HV55) ---

export const TEMPLATE_VARIABLES: readonly { name: string; desc: string }[] = [
  { name: "ten_khach", desc: 'Tên khách ("bạn" khi chưa có tên)' },
  { name: "ten_page", desc: "Tên Page" },
  { name: "ma_don", desc: "Mã đơn mới nhất của khách" },
  { name: "nhan_vien", desc: "Tên người gửi (tin tự động: tên Page)" },
  { name: "ma_van_don", desc: "Mã vận đơn (sau khi có vận chuyển M3)" },
];

/** Biến {tên} trong nội dung không có trong bảng biến (theo thứ tự gặp, không trùng). Khớp mẫu biến của máy chủ. */
export function unknownVariables(body: string): string[] {
  const known = new Set(TEMPLATE_VARIABLES.map((v) => v.name));
  const out: string[] = [];
  for (const m of body.matchAll(/\{([a-z_]+)\}/g)) {
    const name = m[1] ?? "";
    if (!known.has(name) && !out.includes(name)) out.push(name);
  }
  return out;
}

export const SHORTCUT_RE = /^[a-z0-9_-]{1,32}$/;

// --- Nhãn: màu là tên token của hệ thiết kế (Quyết định 3) ---

export const TAG_COLORS: readonly { value: string; label: string; cls: string }[] = [
  { value: "soft", label: "Hồng", cls: "lb-1" },
  { value: "surface", label: "Xám", cls: "lb-2" },
  { value: "success", label: "Xanh", cls: "lb-3" },
  { value: "edge", label: "Viền", cls: "lb-4" },
];

/** Lớp CSS của màu nhãn; màu lạ vẽ như Xám. */
export const tagClass = (color: string): string => TAG_COLORS.find((c) => c.value === color)?.cls ?? "lb-2";

/** So tên nhãn không phân biệt hoa thường, khoảng trắng hai đầu (báo trùng tại ô trước khi gửi). */
export const sameName = (a: string, b: string): boolean =>
  a.trim().toLocaleLowerCase("vi") === b.trim().toLocaleLowerCase("vi");

// --- Khung giờ live (Task 160 quyết định 6: from < to trong ngày, không chồng; chạm mép được) ---

const HHMM = /^([01]\d|2[0-3]):[0-5]\d$/;

export interface SlotInput {
  from: string;
  to: string;
  trafficSourceId: string;
}

/** Lỗi của từng khung giờ theo vị trí (undefined = được). */
export function slotErrors(slots: readonly SlotInput[]): (string | undefined)[] {
  const errs: (string | undefined)[] = slots.map((s) => {
    if (!HHMM.test(s.from) || !HHMM.test(s.to)) return "Giờ dạng HH:MM, ví dụ 19:00.";
    if (s.from >= s.to) return "Giờ bắt đầu phải trước giờ kết thúc (trong cùng ngày).";
    if (s.trafficSourceId === "") return "Chọn nguồn đơn.";
    return undefined;
  });
  const valid = errs.map((e) => e === undefined);
  slots.forEach((a, i) => {
    if (!valid[i]) return;
    const j = slots.findIndex((b, k) => k !== i && valid[k] === true && a.from < b.to && b.from < a.to);
    if (j >= 0) errs[i] = `Chồng với khung ${slots[j]?.from ?? ""}–${slots[j]?.to ?? ""}.`;
  });
  return errs;
}

// --- Quyền ---

/** Có quyền ở phạm vi cả shop. */
export const hasAll = (grants: readonly Grant[], permission: string): boolean =>
  grants.some((g) => g.permission === permission && g.scope.type === "all");

/** Có quyền trên Page (phạm vi cả shop hoặc đúng Page). */
export const hasOnPage = (grants: readonly Grant[], permission: string, pageId: string): boolean =>
  grants.some(
    (g) =>
      g.permission === permission && (g.scope.type === "all" || (g.scope.type === "page" && g.scope.id === pageId)),
  );

// --- Tính năng kênh (HV33): khối không hỗ trợ bị ẩn hẳn ---

const FACEBOOK_FEATURES: ChannelFeatures = {
  messages: true,
  comments: true,
  liveComments: true,
  privateReply: true,
  hideComment: true,
  quickReplies: true,
  humanAgent: true,
};
const NO_FEATURES: ChannelFeatures = {
  messages: true,
  comments: false,
  liveComments: false,
  privateReply: false,
  hideComment: false,
  quickReplies: false,
  humanAgent: false,
};

/** Tính năng của tài khoản; thiếu (kênh chưa bật ở tiến trình) → bộ của Facebook khi kênh là facebook (Quyết định 8). */
export const featuresOf = (a: ChannelAccount): ChannelFeatures =>
  a.features ?? (a.channel === "facebook" ? FACEBOOK_FEATURES : NO_FEATURES);

export const FEATURE_LABELS: readonly { key: keyof ChannelFeatures; label: string }[] = [
  { key: "messages", label: "Tin nhắn" },
  { key: "comments", label: "Comment" },
  { key: "liveComments", label: "Live" },
  { key: "privateReply", label: "Nhắn riêng comment" },
  { key: "hideComment", label: "Ẩn comment" },
  { key: "quickReplies", label: "Nút trả lời nhanh" },
];

/** Chữ trong biểu tượng kênh (mockup .ch-ic). */
export const channelIcon = (channel: string): string =>
  channel === "facebook" ? "FB" : channel.slice(0, 4).toUpperCase();

/** Tên loại tài khoản kênh. */
export const channelKind = (channel: string): string => (channel === "facebook" ? "Facebook Page" : channel);

/** Địa chỉ đăng nhập Facebook máy chủ trả: chỉ mở khi đúng https://www.facebook.com/… (không chuyển tới đường lạ). */
export function isFacebookAuthorizeUrl(raw: string): boolean {
  try {
    const u = new URL(raw);
    return u.protocol === "https:" && (u.hostname === "www.facebook.com" || u.hostname === "facebook.com");
  } catch {
    return false;
  }
}
