// Quy tắc thuần của màn Live và Đã chốt, thiếu địa chỉ (kế hoạch M4 Task 177, mockup docs/mockups/m4/live.html,
// thieu-dia-chi.html): gộp comment theo messageId, đếm "N comment mới" khi người dùng đang xem phía trên, đọc thân lỗi
// của lệnh chốt (Task 172), câu cho kết quả xin địa chỉ. Câu chỉ chèn chữ máy chủ đã viết sẵn bằng tiếng Việt (lý do
// của Task 172, 173) hoặc tên người, mã đơn; không chèn mã lỗi.
import type { AskAddressResult, LiveComment, LiveDraftItem, LiveDraftRef, LiveSession } from "../../api";
import { ApiError } from "../../app/errors";
import { VN_TIME_ZONE } from "../../app/time";

/** Chủ đề báo hiệu realtime của comment live (máy chủ: conversation.TopicLiveComments; entity.id = mã phiên). */
export const TOPIC_LIVE_COMMENTS = "live.comments";

/** Mốc after của listLiveComments: "sentAt,messageId" của comment cuối đã có (máy chủ lùi 30 giây). */
export const afterOf = (c: Pick<LiveComment, "sentAt" | "messageId">) => `${c.sentAt},${c.messageId}`;

function compare(a: LiveComment, b: LiveComment): number {
  const d = Date.parse(a.sentAt) - Date.parse(b.sentAt);
  if (d !== 0) return d;
  return a.messageId < b.messageId ? -1 : a.messageId > b.messageId ? 1 : 0;
}

/** Gộp theo messageId (bản trong incoming thắng: trạng thái chốt mới hơn), xếp theo (sentAt, messageId) tăng dần. */
export function mergeComments(prev: readonly LiveComment[], incoming: readonly LiveComment[]): LiveComment[] {
  const byId = new Map<string, LiveComment>();
  for (const c of prev) byId.set(c.messageId, c);
  for (const c of incoming) byId.set(c.messageId, c);
  return [...byId.values()].sort(compare);
}

/**
 * Số comment mới chưa thấy khi người dùng rời cuối danh sách: comment tới sau lúc rời (baseline = số comment lúc đó)
 * mà còn nằm dưới dòng cuối đang thấy (lastVisible, chỉ số). Cuộn xuống thì số giảm dần.
 */
export function unseenCount(total: number, baseline: number, lastVisible: number): number {
  return Math.max(0, total - Math.max(baseline, lastVisible + 1));
}

const CLOCK = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});
const HM = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});
const DAY = new Intl.DateTimeFormat("en-CA", {
  timeZone: VN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

const fmt = (f: Intl.DateTimeFormat, iso: string) => {
  const ms = Date.parse(iso);
  return Number.isNaN(ms) ? "" : f.format(ms);
};
/** "20:01:12" giờ Việt Nam. */
export const clockTime = (iso: string) => fmt(CLOCK, iso);
/** "20:01" giờ Việt Nam. */
export const hourMinute = (iso: string) => fmt(HM, iso);

/** Đầu ngày hôm nay theo giờ Việt Nam (UTC+7, không đổi giờ mùa hè), dạng ISO, cho listLiveSessions?since. */
export function startOfTodayVN(nowMs: number): string {
  return new Date(`${DAY.format(nowMs)}T00:00:00+07:00`).toISOString();
}

/** "2 phút", "1 giờ 5 phút" từ startedAt tới nowMs. */
export function elapsedText(startedAt: string, nowMs: number): string {
  const min = Math.max(0, Math.floor((nowMs - Date.parse(startedAt)) / 60_000));
  if (min < 60) return `${String(min)} phút`;
  const h = Math.floor(min / 60);
  const m = min % 60;
  return m === 0 ? `${String(h)} giờ` : `${String(h)} giờ ${String(m)} phút`;
}

/** "Đầm lụa be L × 1, Áo len kem M × 2". */
export const itemsText = (items: readonly LiveDraftItem[]) =>
  items.map((i) => `${i.name} × ${String(i.quantity)}`).join(", ");

/** Tên phiên: tiêu đề, không có thì "Live " + mã video. */
export const sessionTitle = (s: Pick<LiveSession, "title" | "liveVideoId">) =>
  s.title !== undefined && s.title.trim() !== "" ? s.title : `Live ${s.liveVideoId}`;

/** Lỗi của lệnh chốt comment (Task 172) đã đọc thành dữ liệu màn hình vẽ được. */
export type ClaimProblem =
  | { kind: "claimed"; by: string; orderId: string | null; orderCode: string | null }
  | { kind: "out_of_stock"; available: number | null; variantId: string | null }
  | { kind: "open_draft"; drafts: LiveDraftRef[] }
  | { kind: "seller_mismatch" }
  | { kind: "version_conflict" }
  | { kind: "forbidden" };

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const str = (v: unknown): string | null => (typeof v === "string" && v !== "" ? v : null);

function draftItem(v: unknown): LiveDraftItem | null {
  if (!isRecord(v)) return null;
  const { variantId, name, quantity } = v;
  if (typeof variantId !== "string" || typeof name !== "string" || typeof quantity !== "number") return null;
  return { variantId, name, quantity };
}

/** Một LiveDraftRef trong thân lỗi open_draft_exists; sai dạng → null (bỏ qua). */
function draftRef(v: unknown): LiveDraftRef | null {
  if (!isRecord(v)) return null;
  const { orderId, code, items, grandTotal, version, expiresAt } = v;
  if (typeof orderId !== "string" || typeof code !== "string" || typeof grandTotal !== "number") return null;
  if (typeof version !== "number" || !Array.isArray(items)) return null;
  const list = items.map(draftItem);
  if (list.some((i) => i === null)) return null;
  return {
    orderId,
    code,
    grandTotal,
    version,
    items: list.filter((i): i is LiveDraftItem => i !== null),
    ...(typeof expiresAt === "string" ? { expiresAt } : {}),
  };
}

/** Đọc lỗi API của claimLiveComment; lỗi khác (mạng, bận, mã khác) → null (màn hiện câu chung). */
export function claimProblem(err: unknown): ClaimProblem | null {
  if (!(err instanceof ApiError)) return null;
  const d = err.details;
  switch (err.code) {
    case "comment_claimed":
      return {
        kind: "claimed",
        by: str(d.claimedBy) ?? "Người khác",
        orderId: str(d.orderId),
        orderCode: str(d.orderCode),
      };
    case "out_of_stock": {
      const ids = Array.isArray(d.variantIds) ? d.variantIds : [];
      return {
        kind: "out_of_stock",
        available: typeof d.available === "number" ? d.available : null,
        variantId: str(ids[0]) ?? str(d.variantId),
      };
    }
    case "open_draft_exists": {
      const drafts = (Array.isArray(d.drafts) ? d.drafts : []).map(draftRef);
      return { kind: "open_draft", drafts: drafts.filter((x): x is LiveDraftRef => x !== null) };
    }
    case "seller_mismatch":
      return { kind: "seller_mismatch" };
    case "version_conflict":
      return { kind: "version_conflict" };
    case "forbidden":
      return err.status === 403 ? { kind: "forbidden" } : null;
    default:
      return null;
  }
}

/** Câu cho kết quả xin địa chỉ ngay sau khi chốt (Task 172 Quyết định 3; HV42). */
export function askAfterClaim(ask: AskAddressResult): { ok: boolean; text: string } {
  if (ask.status === "queued") {
    return {
      ok: true,
      text: "Đã xếp tin nhắn riêng xin địa chỉ cho comment, trạng thái hiện ở Đã chốt, thiếu địa chỉ.",
    };
  }
  const why = ask.reason !== undefined && ask.reason !== "" ? `${ask.reason}. ` : "";
  return {
    ok: false,
    text: `${why}Đơn nháp vẫn giữ hàng; mở hội thoại để nhắn khách hoặc gọi khách, nhắc lại ở Đã chốt, thiếu địa chỉ.`,
  };
}

/** Chip cột "Xin địa chỉ" của bảng đơn nháp (Task 173 Quyết định 4; tệp task 177 Quyết định 5). */
export function askChip(ask: AskAddressResult): { cls: string; label: string; reason: string | null } {
  const reason = ask.reason !== undefined && ask.reason !== "" ? ask.reason : null;
  if (ask.status === "queued" && reason === null) return { cls: "chip chip-ok", label: "Đã xin địa chỉ", reason };
  if (ask.status === "not_requested") return { cls: "chip", label: "Chưa xin", reason };
  return { cls: "chip chip-err", label: "Chưa xin được", reason };
}
