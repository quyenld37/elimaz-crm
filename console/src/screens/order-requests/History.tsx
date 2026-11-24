// Thẻ "Lịch sử" của chi tiết yêu cầu (mockup docs/mockups/m5/order-requests.html trạng thái B, Task 486): mới nhất
// trước, gộp lúc khách gửi (createdAt, số ảnh), các tin (trả lời khách, ghi chú nội bộ, khách trả lời) và các lần đổi
// trạng thái máy chủ ghi (history). Chỉ tên người làm, không nội dung tin.
import type { OrderRequest } from "../../api";
import { shortTime } from "../../app/time";
import { STATUS_TEXT } from "./requestRules";

interface Entry {
  key: string;
  at: string;
  what: string;
  who: string;
}

const STAFF = "Nhân viên";

/** Các dòng lịch sử, mới nhất trước (cùng giờ: thứ tự máy chủ trả, việc sau đứng trước). */
export function historyEntries(r: OrderRequest): Entry[] {
  const photos = r.files.length > 0 ? ` · ${String(r.files.length)} ảnh` : "";
  const out: Entry[] = [{ key: "created", at: r.createdAt, what: "Khách gửi yêu cầu", who: `Website${photos}` }];
  for (const m of r.messages) {
    const what =
      m.authorType === "customer" ? "Khách trả lời" : m.visibleToCustomer ? "Trả lời khách" : "Ghi chú nội bộ";
    const who = m.authorType === "customer" ? "Website" : (m.authorName ?? STAFF);
    out.push({ key: `m-${m.id}`, at: m.createdAt, what, who });
  }
  (r.history ?? []).forEach((h, i) => {
    const who =
      h.actorType === "customer" ? "Khách · website" : h.actorType === "system" ? "Hệ thống" : (h.actorName ?? STAFF);
    out.push({ key: `h-${String(i)}`, at: h.at, what: `Chuyển sang "${STATUS_TEXT[h.toStatus]}"`, who });
  });
  return out
    .map((e, i) => ({ e, i, t: Date.parse(e.at) }))
    .sort((a, b) => b.t - a.t || b.i - a.i)
    .map(({ e }) => e);
}

export function History({ r }: { r: OrderRequest }) {
  return (
    <section className="card">
      <h2>Lịch sử</h2>
      <ol className="tl">
        {historyEntries(r).map((e) => (
          <li key={e.key}>
            <span className="at">{shortTime(e.at)}</span>
            <div>
              <b>{e.what}</b>
              <p className="who wrap-any">{e.who}</p>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}
