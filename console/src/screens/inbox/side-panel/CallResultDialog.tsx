// Hộp thoại kết quả cuộc gọi (Task 176; mockup khung-ben-phai.html trạng thái C "ket-qua-goi"; HV30; CH17 phía màn
// hình): sáu kết quả; "Hẹn gọi lại" bắt buộc chọn giờ (báo tại ô, không gửi) — máy chủ tự tạo nhắc việc cho người gọi;
// ghi chú tùy chọn → createCallLog (gắn hội thoại này). Chưa chọn kết quả thì nút Lưu khóa.
import { useId, useState } from "react";
import { createCallLog, type CallOutcome, type CreateCallLogRequest } from "../../../api";
import { unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { Dialog } from "../../staff/staffData";
import { CALL_OUTCOMES, CALL_OUTCOME_TEXT, localInputToIso } from "./labels";
import { timeError } from "./ReminderForm";
import { TimeField } from "./TimeField";

const MAX_NOTE = 1000;

export interface CallResultDialogProps {
  customerId: string;
  conversationId: string;
  /** Dòng trên đầu hộp thoại: tên khách · SĐT. */
  who: string;
  onSaved: () => void;
  onClose: () => void;
}

export function CallResultDialog({ customerId, conversationId, who, onSaved, onClose }: CallResultDialogProps) {
  const id = useId();
  const [outcome, setOutcome] = useState<CallOutcome | null>(null);
  const [at, setAt] = useState("");
  const [note, setNote] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const write = useWriteAction((key, body: CreateCallLogRequest) =>
    unwrap(createCallLog({ path: { id: customerId }, body, headers: { "Idempotency-Key": key } })),
  );
  const save = async () => {
    if (outcome === null) return;
    let callbackAt: string | null = null;
    if (outcome === "callback") {
      const e = timeError(at, Date.now(), "Chọn giờ gọi lại.");
      setErr(e);
      callbackAt = localInputToIso(at);
      if (e !== null || callbackAt === null) return;
    }
    const n = note.trim();
    const r = await write.run({
      outcome,
      conversationId,
      ...(callbackAt === null ? {} : { callbackAt }),
      ...(n === "" ? {} : { note: n }),
    });
    if (r.ok) onSaved();
  };
  return (
    <Dialog
      title="Kết quả cuộc gọi"
      closeButton
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-quiet" onClick={onClose}>
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={outcome === null || write.pending}
            onClick={() => {
              void save();
            }}
          >
            Lưu
          </button>
        </>
      }
    >
      <p className="small muted">{who}</p>
      <div className="radios" role="radiogroup" aria-label="Kết quả">
        {CALL_OUTCOMES.map((o) => (
          <label key={o} className="check">
            <input
              type="radio"
              name={`${id}-kq`}
              checked={outcome === o}
              onChange={() => {
                setOutcome(o);
                setErr(null);
              }}
            />{" "}
            {CALL_OUTCOME_TEXT[o]}
          </label>
        ))}
      </div>
      {outcome === "callback" && (
        <TimeField
          label="Gọi lại lúc *"
          value={at}
          error={err}
          onChange={(v) => {
            setAt(v);
            setErr(null);
          }}
        />
      )}
      <label className="field">
        <span>Ghi chú</span>
        <textarea
          className="input"
          style={{ minHeight: 64 }}
          maxLength={MAX_NOTE}
          value={note}
          onChange={(e) => {
            setNote(e.target.value);
          }}
        />
      </label>
      {write.message !== null && (
        <p className="err-text" role="alert">
          {write.message}
        </p>
      )}
      <p className="xs muted">
        Lưu vào hồ sơ khách và hội thoại này; &quot;Hẹn gọi lại&quot; tự tạo nhắc việc cho bạn.
      </p>
    </Dialog>
  );
}
