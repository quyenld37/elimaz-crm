// Trao đổi với khách trên chi tiết yêu cầu (mockup docs/mockups/m5/order-requests.html trạng thái B; HV04): lời nhắn khách
// gửi, tin nhân viên "Khách thấy", ghi chú "Nội bộ"; ô viết tin chọn ai thấy. Nội dung hiện đúng chữ (React tự thoát, không
// HTML). Trả lời không đổi version của yêu cầu (máy chủ), nên không gửi version.
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { replyOrderRequest, type OrderRequest, type ReplyOrderRequestRequest } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { shortTime } from "../../app/time";
import { headers, reloadRequests } from "./requestData";
import { MSG_MAX, messageError, runeCount } from "./requestRules";

function Messages({ r }: { r: OrderRequest }) {
  return (
    <div className="msgs">
      {r.customerNote !== undefined && r.customerNote !== "" && (
        <div className="msg is-cust">
          <div className="msg-h">
            <span className="grow">Khách · qua website</span>
            <span className="xs muted num">{shortTime(r.createdAt)}</span>
          </div>
          <p className="body">{r.customerNote}</p>
        </div>
      )}
      {r.messages.map((m) => {
        const cls = m.authorType === "customer" ? "msg is-cust" : m.visibleToCustomer ? "msg is-staff" : "msg is-int";
        const who =
          m.authorType === "customer"
            ? "Khách · qua website"
            : m.authorType === "system"
              ? "Hệ thống"
              : (m.authorName ?? "Nhân viên");
        return (
          <div key={m.id} className={cls}>
            <div className="msg-h">
              <span className="grow">{who}</span>
              {m.authorType !== "customer" &&
                (m.visibleToCustomer ? (
                  <span className="chip chip-ok">Khách thấy</span>
                ) : (
                  <span className="chip chip-line">Nội bộ · khách không thấy</span>
                ))}
              <span className="xs muted num">{shortTime(m.createdAt)}</span>
            </div>
            <p className="body">{m.body}</p>
          </div>
        );
      })}
    </div>
  );
}

interface ReplyBody {
  id: string;
  req: ReplyOrderRequestRequest;
}

/** Ô viết tin; onPending báo cho thẻ xử lý khóa thao tác khi đang gửi. */
function ReplyBox({ r, busy, onPending }: { r: OrderRequest; busy: boolean; onPending: (p: boolean) => void }) {
  const qc = useQueryClient();
  const id = useId();
  const [text, setText] = useState("");
  const [visible, setVisible] = useState(true);
  const write = useWriteAction((key, b: ReplyBody) =>
    unwrap(replyOrderRequest({ path: { id: b.id }, body: b.req, headers: headers(key) })),
  );
  const err = messageError(text, true);
  const send = async () => {
    onPending(true);
    const res = await write.run({ id: r.id, req: { body: text.trim(), visibleToCustomer: visible } });
    onPending(false);
    if (!res.ok) return;
    setText("");
    reloadRequests(qc);
  };
  const radio = (v: boolean, label: string) => (
    <label className="check">
      <input
        type="radio"
        name={`${id}-vis`}
        checked={visible === v}
        disabled={write.pending}
        onChange={() => {
          setVisible(v);
        }}
      />{" "}
      {label}
    </label>
  );
  return (
    <div className="stack-sm">
      <label className="field">
        <span>Viết tin</span>
        <textarea
          className="input"
          placeholder="Nhập nội dung (5–1000 ký tự)"
          maxLength={MSG_MAX + 200}
          value={text}
          disabled={write.pending}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
      </label>
      <div className="row-between">
        <div className="radios" role="radiogroup" aria-label="Ai thấy tin này">
          {radio(true, "Khách thấy")}
          {radio(false, "Ghi chú nội bộ")}
        </div>
        <div className="row">
          <span className="xs muted num">{`${String(runeCount(text.trim()))} / ${String(MSG_MAX)}`}</span>
          <button
            type="button"
            className="btn btn-dark"
            disabled={err !== null || write.pending || busy}
            onClick={() => {
              void send();
            }}
          >
            {write.pending ? "Đang gửi…" : "Gửi"}
          </button>
        </div>
      </div>
      {text.trim() !== "" && err !== null && <span className="field-err">{err}</span>}
      {write.message !== null && <div className="box box-err">{write.message}</div>}
      <p className="hint">
        Tin &quot;Khách thấy&quot; hiện ở trang đơn hàng của khách trên website. Nội dung hiện đúng chữ đã gõ (không
        chạy mã, không định dạng).
      </p>
    </div>
  );
}

/** Thẻ "Trao đổi với khách"; canWrite false (chỉ xem hay yêu cầu đã đóng) → không có ô viết tin. */
export function Thread(o: { r: OrderRequest; canWrite: boolean; busy: boolean; onPending: (p: boolean) => void }) {
  return (
    <section className="card">
      <div className="row-between">
        <h2>Trao đổi với khách</h2>
        <span className="small muted">Mới nhất ở dưới</span>
      </div>
      <Messages r={o.r} />
      {o.canWrite && <ReplyBox r={o.r} busy={o.busy} onPending={o.onPending} />}
    </section>
  );
}
