// Dòng mở của màn Góp ý (mockup docs/mockups/m5/feedback.html trạng thái A dòng mở, C 409): nội dung đầy đủ, mã đơn,
// trang, thiết bị; ghi chú nội bộ ≤ 1000 và nút đổi trạng thái (customer.feedback.manage). Mỗi lệnh ghi chỉ gửi version
// và đúng trường đổi (Bẫy Task 85). 409 → tải lại danh sách, báo trạng thái mới, giữ ghi chú đang gõ (tệp task Quyết
// định 7). Ghi chú, nội dung không ghi log trình duyệt, không ghi localStorage.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { updateFeedback, type Feedback, type UpdateFeedbackRequest } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { shortTime } from "../../app/time";
import { Counted } from "../web-banners/listEditor";
import { headers, putFeedback, reloadFeedback } from "./feedbackData";
import { STATE_ACTION, STATE_TEXT, nextStates } from "./feedbackRules";

/** Số ký tự tối đa của ghi chú nội bộ (UpdateFeedbackRequest.note). */
export const MAX_NOTE = 1000;

const isConflict = (e: unknown) => e instanceof ApiError && e.status === 409 && e.code === "version_conflict";

function ConflictBox({ f }: { f: Feedback }) {
  const who = [f.updatedByName, shortTime(f.updatedAt)].filter((x) => x !== undefined && x !== "").join(", ");
  return (
    <div className="box box-err stack-sm">
      <b>Góp ý vừa được người khác đổi.</b>
      <p>
        {`Đã tải lại: góp ý này giờ là "${STATE_TEXT[f.state]}" (${who}). Ghi chú bạn đang gõ vẫn giữ trong ô; lưu lại nếu cần.`}
      </p>
    </div>
  );
}

export function FeedbackPanel({ f, canManage }: { f: Feedback; canManage: boolean }) {
  const qc = useQueryClient();
  const note = f.note ?? "";
  const [draft, setDraft] = useState(note);
  const [conflict, setConflict] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  const save = useWriteAction((key, b: { id: string; body: UpdateFeedbackRequest }) =>
    unwrap(updateFeedback({ path: { id: b.id }, body: b.body, headers: headers(key) })),
  );
  const busy = save.pending;
  const meta = [
    ...(f.orderRef === undefined ? [] : [`Mã đơn: ${f.orderRef}`]),
    ...(f.page === undefined ? [] : [`Trang khách đang xem: ${f.page}`]),
    ...(f.device === undefined ? [] : [`Thiết bị: ${f.device}`]),
  ];

  const send = async (change: Omit<UpdateFeedbackRequest, "version">, ok: string | null) => {
    setConflict(false);
    setDone(null);
    const r = await save.run({ id: f.id, body: { version: f.version, ...change } });
    if (r.ok) {
      putFeedback(qc, r.data);
      setDone(ok);
      return;
    }
    if (isConflict(r.error)) {
      save.reset();
      setConflict(true);
      await reloadFeedback(qc);
    }
  };

  return (
    <section className="fb-d" aria-label="Chi tiết góp ý">
      <p className="body">{f.body}</p>
      {meta.length > 0 && <p className="small muted">{meta.join(" · ")}</p>}
      {canManage ? (
        <>
          <Counted
            label="Ghi chú nội bộ"
            value={draft}
            max={MAX_NOTE}
            area
            disabled={busy}
            placeholder="Ví dụ: đã gọi lại khách, đã báo hãng…"
            hint="khách không thấy"
            onChange={setDraft}
          />
          <div className="acts">
            <button
              type="button"
              className="btn btn-line"
              disabled={busy || draft.trim() === note}
              onClick={() => {
                void send({ note: draft.trim() }, "Đã lưu ghi chú.");
              }}
            >
              Lưu ghi chú
            </button>
            {nextStates(f.state).map((s) => (
              <button
                key={s}
                type="button"
                className={s === "answered" ? "btn btn-dark" : "btn btn-line"}
                disabled={busy}
                onClick={() => {
                  void send({ state: s }, null);
                }}
              >
                {STATE_ACTION[s]}
              </button>
            ))}
          </div>
          {conflict && <ConflictBox f={f} />}
          {save.message !== null && <div className="box box-err">{save.message}</div>}
          {done !== null && <p className="box box-ok">{done}</p>}
          <p className="xs muted">Thay đổi được ghi vào nhật ký thao tác (chỉ tên trường, không ghi nội dung).</p>
        </>
      ) : (
        note !== "" && (
          <div className="stack-sm">
            <p className="label">Ghi chú nội bộ</p>
            <p className="body">{note}</p>
          </div>
        )
      )}
    </section>
  );
}
