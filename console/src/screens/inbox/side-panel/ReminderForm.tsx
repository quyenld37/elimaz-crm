// Nhắc việc trên hội thoại (Task 176; mockup khung-ben-phai.html trạng thái A "nhac-viec"; Task 169): nhắc của chính
// người xem (listMyReminders, máy chủ không có danh sách theo hội thoại) lọc theo hội thoại này; "Xong" →
// completeReminder; "Nhắc tôi lúc…" mở ô giờ (giờ máy, đổi sang UTC) và việc cần làm → createReminder. Giờ trống hay đã
// qua báo tại ô, không gửi (máy chủ còn kiểm tối đa 90 ngày). Ghi cần conversation.reply trên Page.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { completeReminder, createReminder, listMyReminders, type CreateReminderRequest } from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { shortTime } from "../../../app/time";
import { localInputToIso } from "./labels";
import { remindersKey } from "./sidePanelData";
import { TimeField } from "./TimeField";

const MAX_NOTE = 500;

export interface ReminderFormProps {
  conversationId: string;
  canEdit: boolean;
}

/** Lỗi của ô giờ: trống hay đã qua; null khi dùng được. */
export function timeError(v: string, now: number, empty: string): string | null {
  const iso = localInputToIso(v);
  if (iso === null) return empty;
  return Date.parse(iso) <= now ? "Giờ phải sau bây giờ." : null;
}

export function ReminderForm({ conversationId, canEdit }: ReminderFormProps) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [at, setAt] = useState("");
  const [note, setNote] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const list = useQuery({ queryKey: remindersKey(), queryFn: () => unwrap(listMyReminders()) });
  const mine = (list.data?.items ?? [])
    .filter((r) => r.conversationId === conversationId && (r.status === "pending" || r.status === "fired"))
    .sort((a, b) => Date.parse(a.remindAt) - Date.parse(b.remindAt));
  const reload = () => {
    void qc.invalidateQueries({ queryKey: remindersKey() });
  };
  const create = useWriteAction((key, body: CreateReminderRequest) =>
    unwrap(createReminder({ path: { id: conversationId }, body, headers: { "Idempotency-Key": key } })),
  );
  const done = useWriteAction((key, rid: string) =>
    unwrap(completeReminder({ path: { id: rid }, headers: { "Idempotency-Key": key } })),
  );

  const save = async () => {
    const e = timeError(at, Date.now(), "Chọn giờ nhắc.");
    setErr(e);
    const iso = localInputToIso(at);
    if (e !== null || iso === null) return;
    const n = note.trim();
    const r = await create.run({ remindAt: iso, ...(n === "" ? {} : { note: n }) });
    if (!r.ok) return;
    setOpen(false);
    setAt("");
    setNote("");
    reload();
  };

  return (
    <section className="sec" aria-label="Nhắc việc">
      <h3>Nhắc việc</h3>
      {list.isError && (
        <p className="err-text" role="alert">
          {errorMessage(list.error)}
        </p>
      )}
      {mine.map((r) => (
        <div key={r.id} className="row-between">
          <p className="small">
            {r.note === undefined || r.note === "" ? "Nhắc việc" : r.note} · <b>{shortTime(r.remindAt)}</b>
            {r.status === "fired" && " · đã tới giờ"}
          </p>
          {canEdit && (
            <button
              type="button"
              className="btn btn-quiet"
              disabled={done.pending}
              onClick={() => {
                void done.run(r.id).then((x) => {
                  if (x.ok) reload();
                });
              }}
            >
              Xong
            </button>
          )}
        </div>
      ))}
      {list.isSuccess && mine.length === 0 && !open && <p className="small muted">Chưa có nhắc việc nào của bạn.</p>}
      {done.message !== null && (
        <p className="err-text" role="alert">
          {done.message}
        </p>
      )}
      {canEdit && !open && (
        <div className="row">
          <button
            type="button"
            className="btn btn-quiet"
            onClick={() => {
              setOpen(true);
            }}
          >
            Nhắc tôi lúc…
          </button>
        </div>
      )}
      {canEdit && open && (
        <div className="mini-f">
          <TimeField
            label="Nhắc lúc *"
            value={at}
            error={err}
            onChange={(v) => {
              setAt(v);
              setErr(null);
            }}
          />
          <label className="field">
            <span>Việc cần làm</span>
            <input
              className="input"
              maxLength={MAX_NOTE}
              value={note}
              onChange={(e) => {
                setNote(e.target.value);
              }}
            />
          </label>
          {create.message !== null && (
            <p className="err-text" role="alert">
              {create.message}
            </p>
          )}
          <div className="row">
            <button
              type="button"
              className="btn btn-dark"
              disabled={create.pending}
              onClick={() => {
                void save();
              }}
            >
              Lưu nhắc
            </button>
            <button
              type="button"
              className="btn btn-quiet"
              onClick={() => {
                setOpen(false);
                setErr(null);
              }}
            >
              Hủy
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
