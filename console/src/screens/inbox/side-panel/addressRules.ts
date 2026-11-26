// "Lấy địa chỉ từ tin HH:MM của khách" (Task 501; mockup khung-ben-phai.html trạng thái B; HV26): tìm tin khách mới nhất
// trông như địa chỉ để chép nguyên văn vào ô "Số nhà, đường" của form lên đơn — nhân viên sửa và chọn tỉnh, xã; màn
// không tự tách tỉnh, xã. Chỉ là gợi ý trên màn: không gửi gì, không lưu gì.
import type { ChatMessage } from "../../../api";
import { shortTime } from "../../../app/time";

/** Độ dài tối đa ô "Số nhà, đường" (ShipAddress.line, AddressFields). */
const LINE_MAX = 500;

/** Từ chỉ đơn vị hành chính, đường, ngõ trong địa chỉ Việt Nam (đứng riêng thành một từ). */
const ADDRESS_WORD =
  /(?<!\p{L})(ngõ|ngách|hẻm|kiệt|đường|phố|phường|xã|quận|huyện|thị trấn|thị xã|tỉnh|thành phố|tp|thôn|ấp|xóm|khu phố|tổ dân phố|chung cư|tòa|kđt|[pq]\.)(?!\p{L})/u;

/** Tin trông như địa chỉ: có chữ số (số nhà, số ngõ, tổ, thôn…) và ít nhất một từ chỉ địa chỉ. */
export function looksLikeAddress(text: string): boolean {
  const s = text.normalize("NFC").toLowerCase();
  return /\d/.test(s) && ADDRESS_WORD.test(s);
}

export interface AddressSuggestion {
  /** Chữ của nút: "Lấy địa chỉ từ tin 09:14 của khách" (khác ngày: kèm ngày/tháng). */
  label: string;
  /** Nội dung tin (bỏ khoảng trắng hai đầu, tối đa 500 đơn vị như maxLength của ô). */
  text: string;
}

/**
 * Gợi ý địa chỉ từ tin khách mới nhất trông như địa chỉ trong items (thứ tự bất kỳ): chỉ tin khách gửi (tin nhắn,
 * comment), chưa bị xóa; không có → null. Giờ theo giờ Việt Nam: cùng ngày với nowMs → "HH:MM", khác ngày → "HH:MM dd/MM".
 */
export function addressSuggestion(items: readonly ChatMessage[], nowMs: number): AddressSuggestion | null {
  let best: ChatMessage | undefined;
  for (const m of items) {
    if (m.direction !== "in" || m.origin !== "customer" || m.removed) continue;
    if (m.kind !== "message" && m.kind !== "comment") continue;
    if (m.body === undefined || !looksLikeAddress(m.body)) continue;
    if (best === undefined || Date.parse(m.sentAt) > Date.parse(best.sentAt)) best = m;
  }
  if (best?.body === undefined) return null;
  const at = shortTime(best.sentAt);
  const [hm = "", day = ""] = at.split(" ");
  const today = shortTime(new Date(nowMs).toISOString()).split(" ")[1] ?? "";
  const when = day === today ? hm : at;
  return { label: `Lấy địa chỉ từ tin ${when} của khách`, text: best.body.trim().slice(0, LINE_MAX) };
}
