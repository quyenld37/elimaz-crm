// Quy tắc thuần của các danh sách vận hành (kế hoạch M3 Task 135; mockup docs/mockups/m3/van-hanh.html): nhãn quá hạn,
// giờ hạn gọi, kết quả cuộc gọi, câu của từng loại "Cần xem", nhãn hàng giữ quá hạn, liên kết gọi điện.
import type { AgedReservation, CallbackTask } from "../../api";

/** Chủ đề realtime (máy chủ: fulfillment.TopicCallbackTask, TopicShipment); khóa truy vấn bắt đầu bằng chuỗi này. */
export const CALLBACK_TOPIC = "callback_task.changed";
export const SHIPMENT_TOPIC = "shipment.changed";
/** Số dòng mỗi lần tải (mockup A: "đang hiện 50, cuộn để tải tiếp"). */
export const PAGE = 50;

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const VN_OFFSET_MS = 7 * HOUR;

const two = (n: number) => String(n).padStart(2, "0");

/** "10:00" theo giờ Việt Nam; sai dạng → "". */
export function clock(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const d = new Date(t + VN_OFFSET_MS);
  return `${two(d.getUTCHours())}:${two(d.getUTCMinutes())}`;
}

/** "22/11" theo giờ Việt Nam. */
export function dayMonth(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const d = new Date(t + VN_OFFSET_MS);
  return `${two(d.getUTCDate())}/${two(d.getUTCMonth() + 1)}`;
}

/** "hạn 10:00 · 22/11". */
export const dueText = (iso: string) => `hạn ${clock(iso)} · ${dayMonth(iso)}`;

/** "Quá hạn 3 giờ" / "Quá hạn 2 ngày"; chưa quá hạn → null. Dưới một giờ vẫn ghi 1 giờ (cờ overdue của máy chủ là gốc). */
export function overdueLabel(t: Pick<CallbackTask, "dueAt" | "overdue">, now: number): string | null {
  if (!t.overdue) return null;
  const late = Math.max(now - Date.parse(t.dueAt), 0);
  if (late >= DAY) return `Quá hạn ${String(Math.floor(late / DAY))} ngày`;
  return `Quá hạn ${String(Math.max(Math.floor(late / HOUR), 1))} giờ`;
}

/** Kết quả nhân viên chọn được (RecordCallbackOutcomeRequest.outcome), đúng thứ tự mockup trạng thái B. */
export const OUTCOMES = [
  { key: "redeliver", label: "Khách nhận, giao lại" },
  { key: "redeliver_new_address", label: "Giao lại tới địa chỉ mới" },
  { key: "customer_cancel", label: "Khách hủy, không nhận" },
  { key: "no_answer", label: "Không nghe máy" },
  { key: "reschedule", label: "Khách hẹn gọi lại" },
] as const;
export type OutcomeKey = (typeof OUTCOMES)[number]["key"];

/** Kết quả có ô "Gọi lại lúc" (hẹn lại): bắt buộc khi hẹn lại, tùy chọn khi không nghe máy. */
export const needsCallTime = (o: OutcomeKey) => o === "no_answer" || o === "reschedule";

const OUTCOME_HELP: Readonly<Record<OutcomeKey, string>> = {
  redeliver: 'Giao lại: CRM báo hãng giao lại (khi hãng hỗ trợ) hoặc hiện câu "Gọi hãng yêu cầu giao lại".',
  redeliver_new_address: "Gửi yêu cầu đổi địa chỉ cho hãng; địa chỉ trên đơn chỉ đổi khi hãng nhận.",
  customer_cancel: "Khách hủy: hãng chuyển hoàn, hàng đi luồng hàng hoàn.",
  no_answer: "Không nghe máy: việc sang Đang xử lý; hẹn giờ gọi lại nếu cần.",
  reschedule: "Khách hẹn gọi lại: chọn giờ gọi lại, việc sang Đang xử lý.",
};
export const outcomeHelp = (o: OutcomeKey) => OUTCOME_HELP[o];

/** Câu cạnh nhãn "Đang xử lý": "Đang xử lý · gọi lại 15:00". */
export function statusText(t: Pick<CallbackTask, "status" | "nextCallAt">): string {
  if (t.status === "in_progress") {
    return t.nextCallAt === undefined ? "Đang xử lý" : `Đang xử lý · gọi lại ${clock(t.nextCallAt)}`;
  }
  if (t.status === "done") return "Đã xong";
  if (t.status === "closed") return "Đã đóng";
  return "";
}

/** "Nhận 3 · Hoàn 2 (40%)" của thống kê giao hàng; tỷ lệ theo phần vạn. */
export function statsText(s: { delivered: number; returned: number; returnRateBp: number }): string {
  const pct = Math.round(s.returnRateBp / 100);
  return `Nhận ${String(s.delivered)} · Hoàn ${String(s.returned)} (${String(pct)}%)`;
}

/** Nhãn rủi ro: "Hay hoàn" (high), "Có hoàn" (watch), không có thì null. */
export const riskChip = (risk: "none" | "watch" | "high"): string | null =>
  risk === "high" ? "Hay hoàn" : risk === "watch" ? "Có hoàn" : null;

/** Liên kết gọi điện. Chỉ giữ chữ số và dấu + đầu số; số rỗng → null. */
export function telHref(phone: string): string | null {
  const p = phone.trim().replace(/(?!^\+)[^0-9]/g, "");
  return p === "" ? null : `tel:${p}`;
}

/** "0912 888 000" cho số 10 chữ số trong nước; số khác giữ nguyên. */
export function phoneText(phone: string): string {
  const m = /^(\d{4})(\d{3})(\d{3})$/.exec(phone);
  return m === null ? phone : `${m[1] ?? ""} ${m[2] ?? ""} ${m[3] ?? ""}`;
}

const REVIEW_KIND: Readonly<Record<string, string>> = {
  in_doubt_create: "Không rõ đã tạo",
  missing_items: "Kiện hoàn thiếu món",
  partial_delivery: "Giao một phần",
  tracking_conflict: "Mã vận đơn trùng",
  stock_short: "Thiếu tồn khi xuất",
  manual_cancel: "Hủy tay ở hãng",
  in_doubt_cancel: "Không rõ đã hủy",
};

/** Tên loại việc "Cần xem"; loại lạ (chuỗi mở) → hiện nguyên mã. */
export const reviewKindLabel = (kind: string) =>
  Object.hasOwn(REVIEW_KIND, kind) ? (REVIEW_KIND[kind] ?? kind) : kind;

/** Loại có hai nút "Đã tạo"/"Chưa tạo" thay cho "Đã xử lý" (resolveShipmentCreate). */
export const isCreateDoubt = (kind: string) => kind === "in_doubt_create";
/** Loại chỉ xử lý được ở đơn (khối Vận chuyển của đơn): màn chỉ có "Mở đơn". */
export const openOrderOnly = (kind: string) => kind === "partial_delivery";

/** Nhãn đếm theo loại trong các việc đã tải: [{kind, n}] theo thứ tự xuất hiện đầu tiên. */
export function kindCounts(items: readonly { kind: string }[]): { kind: string; n: number }[] {
  const m = new Map<string, number>();
  for (const it of items) m.set(it.kind, (m.get(it.kind) ?? 0) + 1);
  return [...m].map(([kind, n]) => ({ kind, n }));
}

/** "Đang giữ · 23 ngày" / "Đang giao · 184 ngày". */
export function agedState(r: Pick<AgedReservation, "state" | "ageDays">): string {
  return `${r.state === "held" ? "Đang giữ" : "Đang giao"} · ${String(r.ageDays)} ngày`;
}

/** "Tải đủ" hay chưa: còn nextCursor thì chưa được coi là hết (CLAUDE.md, Task 84). */
export const hasMore = (next: string | undefined) => next !== undefined && next !== "";
