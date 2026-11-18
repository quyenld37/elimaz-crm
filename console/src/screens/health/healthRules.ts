// Luật hiển thị của màn Sức khỏe hệ thống (kế hoạch M1 Task 53; mockup docs/mockups/m1/suc-khoe.html). Chỉ định dạng
// và phân loại số API getPlatformHealth trả; quyết định "ngừng" theo cùng ngưỡng với /health/deep và luật cảnh báo.
import type { AlertSummary, PlatformHealth, ProcessHealth } from "../../api";

/** Nhịp sống cũ hơn mức này là tiến trình ngừng (máy chủ: metrics.HeartbeatMaxAge = 90 s). */
export const STALE_AFTER_S = 90;

/** Vai trò luật cảnh báo luôn kiểm (observability.heartbeatRules): thiếu nhịp sống là hỏng nên luôn có ô. */
const REQUIRED_ROLES = ["worker", "ingest"];
/** Thứ tự ô như mockup; vai trò khác đứng sau, theo tên. */
const ROLE_ORDER = ["api", "worker", "ingest"];

/** Tuổi tròn xuống: "45 giây", "3 phút", "6 giờ", "5 ngày". */
export function ageText(s: number): string {
  if (s < 60) return `${String(Math.floor(s))} giây`;
  if (s < 3600) return `${String(Math.floor(s / 60))} phút`;
  if (s < 86_400) return `${String(Math.floor(s / 3600))} giờ`;
  return `${String(Math.floor(s / 86_400))} ngày`;
}

/** Tuổi nhịp sống: dưới một giờ ghi số giây ("200 giây", như mockup), từ một giờ như ageText. */
export function beatText(s: number): string {
  return s < 3600 ? `${count(Math.floor(s))} giây` : ageText(s);
}

/** Tuổi tính bằng giờ (sao lưu, diễn tập): dưới 24 giờ ghi giờ, từ 24 giờ ghi ngày. */
export function hoursText(h: number): string {
  return h < 24 ? `${String(h)} giờ` : `${String(Math.floor(h / 24))} ngày`;
}

/** Số đếm có dấu chấm ngăn nghìn: 1.204. */
export function count(n: number): string {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ".");
}

const two = (n: number) => String(n).padStart(2, "0");

/** "14:29" (hoặc "14:32:05" khi seconds) theo giờ máy người xem. */
export function clock(d: Date, seconds = false): string {
  const hm = `${two(d.getHours())}:${two(d.getMinutes())}`;
  return seconds ? `${hm}:${two(d.getSeconds())}` : hm;
}

/** "14:29 18/11" theo giờ máy người xem. */
export function clockDay(d: Date): string {
  return `${clock(d)} ${two(d.getDate())}/${two(d.getMonth() + 1)}`;
}

export interface ProcessRow {
  role: string;
  /** Tuổi nhịp sống mới nhất của vai trò (giây); null khi chưa có. */
  ageS: number | null;
  down: boolean;
}

/**
 * Một ô mỗi vai trò, lấy nhịp sống mới nhất trong các tiến trình cùng vai trò (tiến trình cũ của lần deploy trước vẫn
 * còn dòng); worker, ingest luôn có ô. Ngừng khi tuổi quá STALE_AFTER_S hoặc chưa có nhịp sống.
 */
export function processRows(processes: readonly ProcessHealth[]): ProcessRow[] {
  const newest = new Map<string, number>();
  for (const p of processes) {
    const cur = newest.get(p.role);
    if (cur === undefined || p.lastHeartbeatAgeS < cur) newest.set(p.role, p.lastHeartbeatAgeS);
  }
  const roles = [...new Set([...newest.keys(), ...REQUIRED_ROLES])];
  const rank = (r: string) => {
    const i = ROLE_ORDER.indexOf(r);
    return i < 0 ? ROLE_ORDER.length : i;
  };
  roles.sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
  return roles.map((role) => {
    const ageS = newest.get(role) ?? null;
    return { role, ageS, down: ageS === null || ageS > STALE_AFTER_S };
  });
}

/** Lúc gửi cảnh báo gấp mới nhất về nhịp sống của vai trò (khóa "<vai trò>-heartbeat"), nếu có trong 24 giờ. */
export function downAlertAt(alerts: readonly AlertSummary[], role: string): Date | undefined {
  let at: Date | undefined;
  for (const a of alerts) {
    if (a.severity !== "urgent" || a.key !== `${role}-heartbeat`) continue;
    const d = new Date(a.sentAt);
    if (at === undefined || d > at) at = d;
  }
  return at;
}

/** Trạng thái B của mockup: mọi tiến trình chạy, không việc hỏng hay không chắc đã gửi, không cảnh báo, không lỗi. */
export function allClear(h: PlatformHealth): boolean {
  return (
    processRows(h.processes).every((p) => !p.down) &&
    h.queue.dead === 0 &&
    h.queue.inDoubt === 0 &&
    h.alerts.length === 0 &&
    h.errors.length === 0
  );
}
