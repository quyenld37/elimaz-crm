// Trạng thái gửi dưới tin nhân viên (Task 175; mockup hoi-thoai.html trạng thái B; CH03, CH18, CH19; HV14 sửa theo
// Task 157). Đang gửi, Đã gửi, Đang kiểm tra hiện bằng chữ trong dòng giờ; "Không rõ đã gửi chưa", "Chưa gửi được" và
// "Lỗi" do kênh bận / chưa tới được kênh có hai nút Bỏ / Gửi lại cùng cỡ. Gửi lại tin "không rõ" hỏi lại trước (khách có
// thể nhận hai lần). Máy chủ không tự gửi lại; nút chỉ hiện ở trạng thái máy chủ nhận gửi lại (Task 157 quyết định 1).
import { useState } from "react";
import { discardMessage, resendMessage, type ChatMessage } from "../../../api";
import { unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { shortTime } from "../../../app/time";
import { Dialog } from "../../staff/staffData";
import { statusView } from "./conversationRules";
import { idem, sendErrorText } from "./messageWrites";

const time = (iso: string) => shortTime(iso).split(" ")[0] ?? "";

export function SendStatus({ m, canReply, onChanged }: { m: ChatMessage; canReply: boolean; onChanged: () => void }) {
  const v = statusView(m);
  const [confirm, setConfirm] = useState(false);
  const resend = useWriteAction((key, b: { id: string }) =>
    unwrap(resendMessage({ path: { id: b.id }, headers: idem(key) })),
  );
  const discard = useWriteAction((key, b: { id: string }) =>
    unwrap(discardMessage({ path: { id: b.id }, headers: idem(key) })),
  );
  const busy = resend.pending || discard.pending;
  const doResend = async () => {
    setConfirm(false);
    if ((await resend.run({ id: m.id })).ok) onChanged();
  };
  const doDiscard = async () => {
    if ((await discard.run({ id: m.id })).ok) onChanged();
  };
  const at = time(m.sentAt);

  if (v === null) return <div className="meta">{at}</div>;
  if (v.kind === "line")
    return (
      <div className="meta">
        {at}
        {" · "}
        <span className={v.tone === "ok" ? "st-ok" : v.tone === "wait" ? "st-wait" : ""}>{v.text}</span>
        {v.note !== undefined && <span>{`· ${v.note}`}</span>}
      </div>
    );
  const err = resend.error ?? discard.error;
  return (
    <>
      <div className="sendfix is-err">
        <span>
          <b className="err-text">{v.title}</b> {v.text}
        </span>
        {v.warn !== undefined && <span className="small">{v.warn}</span>}
        {canReply && (v.resend || v.discard) && (
          <div className="acts">
            {v.discard && (
              <button type="button" className="btn btn-quiet" disabled={busy} onClick={() => void doDiscard()}>
                Bỏ
              </button>
            )}
            {v.resend && (
              <button
                type="button"
                className="btn btn-line"
                disabled={busy}
                onClick={() => {
                  if (v.confirm) setConfirm(true);
                  else void doResend();
                }}
              >
                Gửi lại
              </button>
            )}
          </div>
        )}
        {err !== null && (
          <span className="small err-text" role="alert">
            {sendErrorText(err)}
          </span>
        )}
      </div>
      <div className="meta">{at}</div>
      {confirm && (
        <Dialog
          title="Gửi lại tin này?"
          onClose={() => {
            setConfirm(false);
          }}
          footer={
            <>
              <button
                type="button"
                className="btn btn-quiet"
                onClick={() => {
                  setConfirm(false);
                }}
              >
                Thôi
              </button>
              <button type="button" className="btn btn-line" onClick={() => void doResend()}>
                Vẫn gửi lại
              </button>
            </>
          }
        >
          <p>
            CRM không rõ tin đã tới khách chưa. Gửi lại thì <b>khách có thể nhận tin này hai lần</b>. Xem trên điện
            thoại Page trước nếu được.
          </p>
        </Dialog>
      )}
    </>
  );
}
