// Quy luật thuần của màn Hộp chat (Inbox, Task 174; mockup docs/mockups/m4/inbox.html): bộ lọc trên URL (HV06), gộp
// các lần tải theo id, chữ số chưa đọc, giờ, nhãn, tên kênh. Không gọi API, không đụng DOM.
import type {
  ChannelAccount,
  ConversationKind,
  ConversationStatus,
  ConversationSummary,
  ConversationTag,
  ListConversationsData,
} from "../../api";
import { VN_TIME_ZONE } from "../../app/time";

export const PAGE_SIZE = 50;
/** Khóa localStorage: bộ lọc lần trước trên máy này (HV06 đã sửa). */
export const FILTERS_STORAGE_KEY = "crm.inbox.filters";
/** Khóa localStorage: âm báo tin mới bật ("1") hay tắt (HV04 đã sửa, tắt mặc định). */
export const SOUND_STORAGE_KEY = "crm.inbox.sound";

export type StatusFilter = ConversationStatus | "all";
export type KindFilter = ConversationKind | "all";

export interface InboxFilters {
  /** Mã tài khoản kênh (Page); "" là mọi tài khoản. */
  page: string;
  status: StatusFilter;
  /** "" mọi người, "me", "none" (chưa ai nhận) hay mã nhân viên. */
  assignee: string;
  /** Mã nhãn; "" mọi nhãn. */
  tag: string;
  kind: KindFilter;
  unread: boolean;
  q: string;
  /** "Khách chưa trả lời" (CH16, trạng thái D): hiện danh sách nhắn lại thay danh sách hội thoại. */
  followUp: boolean;
}

/** Bộ lọc mặc định lần đầu (HV06): Đang mở, Mọi người, Mọi nhãn, Tin và comment. */
export const DEFAULT_FILTERS: InboxFilters = {
  page: "",
  status: "open",
  assignee: "",
  tag: "",
  kind: "all",
  unread: false,
  q: "",
  followUp: false,
};

export const STATUS_OPTIONS: readonly [StatusFilter, string][] = [
  ["open", "Đang mở"],
  ["pending", "Chờ khách"],
  ["done", "Xong"],
  ["spam", "Spam"],
  ["all", "Mọi trạng thái"],
];

export const KIND_OPTIONS: readonly [KindFilter, string][] = [
  ["all", "Tin và comment"],
  ["inbox", "Chỉ tin nhắn"],
  ["comment", "Chỉ comment"],
];

const STATUS_TEXT: Readonly<Record<ConversationStatus, string>> = {
  open: "Đang mở",
  pending: "Chờ khách",
  done: "Xong",
  spam: "Spam",
};

/** Tham số URL của bộ lọc; có ít nhất một thì URL là nguồn bộ lọc (không lấy bộ lọc đã lưu). */
export const FILTER_PARAMS = ["page", "status", "assignee", "tag", "kind", "unread", "q", "followup"] as const;

const pick = <T extends string>(opts: readonly [T, string][], v: string | null, dflt: T): T =>
  opts.find(([k]) => k === v)?.[0] ?? dflt;

export function readFilters(sp: URLSearchParams): InboxFilters {
  return {
    page: sp.get("page") ?? "",
    status: pick(STATUS_OPTIONS, sp.get("status"), DEFAULT_FILTERS.status),
    assignee: sp.get("assignee") ?? "",
    tag: sp.get("tag") ?? "",
    kind: pick(KIND_OPTIONS, sp.get("kind"), DEFAULT_FILTERS.kind),
    unread: sp.get("unread") === "1",
    q: sp.get("q") ?? "",
    followUp: sp.get("followup") === "1",
  };
}

/** Chỉ ghi trường khác mặc định (URL gọn; bộ lọc mặc định là URL rỗng). */
export function writeFilters(f: InboxFilters): URLSearchParams {
  const d = DEFAULT_FILTERS;
  const fields: [string, string, string][] = [
    ["page", f.page, d.page],
    ["status", f.status, d.status],
    ["assignee", f.assignee, d.assignee],
    ["tag", f.tag, d.tag],
    ["kind", f.kind, d.kind],
    ["unread", f.unread ? "1" : "", ""],
    ["q", f.q, d.q],
    ["followup", f.followUp ? "1" : "", ""],
  ];
  return new URLSearchParams(fields.filter(([, v, dv]) => v !== dv).map(([k, v]) => [k, v]));
}

export function hasFilterParams(sp: URLSearchParams): boolean {
  return FILTER_PARAMS.some((k) => sp.has(k));
}

export function isDefault(f: InboxFilters): boolean {
  return writeFilters(f).toString() === "";
}

/** Chữ tìm ngắn hơn mức này không gửi (máy chủ trả 422 query_too_short). */
export const MIN_Q = 2;
/** Tìm theo số: từ 4 số cuối SĐT (customer.Search của M2). */
export const MIN_DIGITS = 4;

/** Chữ tìm đủ dài để gửi: tên từ 2 ký tự, chỉ chữ số thì từ 4 số. */
export function searchable(q: string): boolean {
  const t = q.trim();
  return /^\d+$/.test(t) ? t.length >= MIN_DIGITS : t.length >= MIN_Q;
}

export type ListQuery = NonNullable<ListConversationsData["query"]>;

/** Tham số listConversations của bộ lọc (trang đầu, không con trỏ). */
export function listQuery(f: InboxFilters): ListQuery {
  const q: ListQuery = {};
  if (f.status !== "all") q.status = f.status;
  if (f.kind !== "all") q.kind = f.kind;
  if (f.assignee !== "") q.assignee = f.assignee;
  if (f.tag !== "") q.tagId = f.tag;
  if (f.page !== "") q.pageId = f.page;
  if (f.unread) q.unread = true;
  if (searchable(f.q)) q.q = f.q.trim();
  q.limit = PAGE_SIZE;
  return q;
}

/** Một lần tải: các dòng và lúc gửi yêu cầu (ms của máy). Lần tải gửi sau là bản mới hơn. */
export interface Stamped {
  at: number;
  rows: readonly ConversationSummary[];
}

/** Thứ tự của máy chủ: lastMessageAt giảm dần, rồi id giảm dần. */
export function compareRows(a: ConversationSummary, b: ConversationSummary): number {
  const t = Date.parse(b.lastMessageAt) - Date.parse(a.lastMessageAt);
  if (t !== 0) return t;
  if (a.id === b.id) return 0;
  return a.id < b.id ? 1 : -1;
}

/** Gộp các lần tải theo id (mỗi id lấy bản của lần tải gửi sau cùng) rồi sắp lại theo (lastMessageAt, id). */
export function mergeRows(loads: readonly Stamped[]): ConversationSummary[] {
  const best = new Map<string, { at: number; row: ConversationSummary }>();
  for (const l of loads) {
    for (const row of l.rows) {
      const cur = best.get(row.id);
      if (cur === undefined || l.at >= cur.at) best.set(row.id, { at: l.at, row });
    }
  }
  return [...best.values()].map((v) => v.row).sort(compareRows);
}

/** Số chưa đọc trên huy hiệu: trên 99 → "99+" (số thật đặt trong aria-label). */
export function unreadText(n: number): string {
  return n > 99 ? "99+" : String(n);
}

const VN_DAY = new Intl.DateTimeFormat("en-CA", {
  timeZone: VN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});
const VN_HM = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});
const VN_DM = new Intl.DateTimeFormat("en-GB", { timeZone: VN_TIME_ZONE, day: "2-digit", month: "2-digit" });

/** Giờ tin cuối ở dòng hội thoại: "vừa xong", "n phút", "09:14" (cùng ngày), "Hôm qua", "20/11" (giờ Việt Nam). */
export function timeText(iso: string, now: number): string {
  const ms = Date.parse(iso);
  if (Number.isNaN(ms)) return "";
  const diff = now - ms;
  if (diff < 60_000) return "vừa xong";
  if (diff < 3_600_000) return `${String(Math.floor(diff / 60_000))} phút`;
  const day = VN_DAY.format(ms);
  if (day === VN_DAY.format(now)) return VN_HM.format(ms);
  if (day === VN_DAY.format(now - 86_400_000)) return "Hôm qua";
  return VN_DM.format(ms);
}

/** Nhãn trên dòng: tối đa 3, còn lại "+n" (HV05). */
export function visibleTags(tags: readonly ConversationTag[]): { shown: ConversationTag[]; more: number } {
  return { shown: tags.slice(0, 3), more: Math.max(tags.length - 3, 0) };
}

// Màu nhãn là tên token của hệ thiết kế (HV55: bốn sắc có sẵn, chọn ở màn Cài đặt, Task 178). Tên lạ → sắc trung tính.
const TAG_CLASS: Readonly<Record<string, string>> = { soft: "lb-1", surface: "lb-2", success: "lb-3", edge: "lb-4" };

export function tagClass(color: string): string {
  return `lb ${TAG_CLASS[color] ?? "lb-2"}`;
}

/** Người phụ trách ở cuối dòng: "Của tôi", "Chưa ai nhận", tên. */
export function assigneeText(r: ConversationSummary, meId: string): string {
  if (r.assignee === undefined) return "Chưa ai nhận";
  return r.assignee.id === meId ? "Của tôi" : r.assignee.name;
}

/** Phần cuối dòng: trạng thái khi khác "Đang mở", rồi người phụ trách ("· Xong · Lê Thu Hằng"). */
export function tailText(r: ConversationSummary, meId: string): string {
  const parts = r.status === "open" ? [] : [STATUS_TEXT[r.status]];
  return `· ${[...parts, assigneeText(r, meId)].join(" · ")}`;
}

/** Biểu tượng chữ của kênh (mockup Kênh kết nối, HV32: mọi kênh cùng một kiểu, không màu thương hiệu). */
export function channelBadge(channel: string): string {
  return channel === "facebook" ? "FB" : channel.slice(0, 2).toUpperCase();
}

/** Tên loại tài khoản trong câu chữ: Page cho Facebook. */
export function accountNoun(a: Pick<ChannelAccount, "channel">): string {
  return a.channel === "facebook" ? "Page" : "Tài khoản";
}

/** Tên nền tảng của kênh trong câu chữ. */
export function channelName(a: Pick<ChannelAccount, "channel">): string {
  return a.channel === "facebook" ? "Facebook" : a.channel;
}

/** Chip trạng thái cạnh tên tài khoản (HV08); null khi bình thường. */
export function accountChip(a: ChannelAccount): { text: string; cls: string } | null {
  if (a.status === "degraded") return { text: "Mất kết nối", cls: "chip chip-err" };
  if (a.status === "disconnected") return { text: "Đã ngắt", cls: "chip" };
  if (a.sendMode === "read_only") return { text: "Chỉ xem", cls: "chip" };
  return null;
}

/** Lý do mất kết nối theo statusReason của máy chủ (Task 149, 150). */
export function degradedReason(a: ChannelAccount): string {
  const r = a.statusReason ?? "";
  if (r.startsWith("meta_policy")) return "Meta đang hạn chế Page này.";
  if (r === "missing_permissions") return "Page thiếu quyền mà CRM cần (có người đã gỡ bớt quyền).";
  if (r === "token_invalid" || r === "auth_failed") {
    return "Facebook không còn nhận mã truy cập của Page (đổi mật khẩu hoặc gỡ quyền).";
  }
  return `${channelName(a)} báo lỗi kết nối.`;
}
