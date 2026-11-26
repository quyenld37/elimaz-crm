// Chọn phiên live (mockup docs/mockups/m4/live.html trạng thái A, D; HV48): mỗi phiên một nút, phiên đang live có chip
// "Đang live"; "+ Thêm phiên bằng link video" khi CRM chưa tự nhận (startLiveSessionManually, Task 170, cần live.operate
// trên Page). Phiên lấy từ listLiveSessions (since = đầu ngày giờ Việt Nam).
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { startLiveSessionManually, type ChannelAccount, type LiveSession } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { Dialog } from "../staff/staffData";
import { hourMinute, sessionTitle } from "./liveRules";

/** Khóa truy vấn danh sách phiên (không có chủ đề realtime riêng: resync 20 s tải lại). */
export const SESSIONS_KEY = "live.sessions";

export interface SessionPickerProps {
  sessions: readonly LiveSession[];
  selectedId: string | null;
  pageName: (pageId: string) => string;
  /** Page người dùng thêm phiên được (có live.operate). */
  pages: readonly ChannelAccount[];
  canStart: boolean;
}

export function sessionWhen(s: LiveSession): string {
  if (s.status === "live") return `từ ${hourMinute(s.startedAt)}`;
  return s.endedAt === undefined ? hourMinute(s.startedAt) : `${hourMinute(s.startedAt)}–${hourMinute(s.endedAt)}`;
}

export function SessionPicker({ sessions, selectedId, pageName, pages, canStart }: SessionPickerProps) {
  const [adding, setAdding] = useState(false);
  return (
    <nav className="sess" aria-label="Phiên live">
      {sessions.map((s) => (
        <Link
          key={s.id}
          className="sess-a"
          to={`/live/${s.id}`}
          aria-current={s.id === selectedId ? "true" : undefined}
        >
          <span className="row" style={{ gap: 6 }}>
            {s.status === "live" ? (
              <span className="chip chip-err">Đang live</span>
            ) : (
              <span className="chip">Đã kết thúc</span>
            )}
            <b className="ellip">{sessionTitle(s)}</b>
          </span>
          <span className="xs muted">{`${pageName(s.pageId)} · ${sessionWhen(s)}`}</span>
        </Link>
      ))}
      {canStart && (
        <button
          type="button"
          className="btn btn-quiet"
          onClick={() => {
            setAdding(true);
          }}
        >
          + Thêm phiên bằng link video
        </button>
      )}
      {adding && (
        <AddSessionDialog
          pages={pages}
          onClose={() => {
            setAdding(false);
          }}
        />
      )}
    </nav>
  );
}

interface StartBody {
  pageId: string;
  videoIdOrUrl: string;
}

/** Hộp "Thêm phiên live" (trạng thái D của mockup). Thêm xong mở phiên vừa thêm. */
export function AddSessionDialog({ pages, onClose }: { pages: readonly ChannelAccount[]; onClose: () => void }) {
  const qc = useQueryClient();
  const [picked, setPicked] = useState("");
  // Page chọn; chưa chọn thì Page đầu (danh sách Page có thể tải xong sau khi mở hộp).
  const pageId = picked !== "" ? picked : (pages[0]?.id ?? "");
  const [video, setVideo] = useState("");
  const [done, setDone] = useState<LiveSession | null>(null);
  const write = useWriteAction((key, b: StartBody) =>
    unwrap(
      startLiveSessionManually({
        path: { id: b.pageId },
        body: { videoIdOrUrl: b.videoIdOrUrl },
        headers: { "Idempotency-Key": key },
      }),
    ),
  );
  const v = video.trim();
  const submit = async () => {
    const r = await write.run({ pageId, videoIdOrUrl: v });
    if (!r.ok) return;
    void qc.invalidateQueries({ queryKey: [SESSIONS_KEY] });
    setDone(r.data);
  };
  return (
    <Dialog
      title="Thêm phiên live"
      closeButton
      onClose={onClose}
      footer={
        done === null ? (
          <>
            <button type="button" className="btn btn-quiet" onClick={onClose}>
              Hủy
            </button>
            <button
              type="button"
              className="btn btn-dark"
              disabled={write.pending || v === "" || pageId === ""}
              onClick={() => void submit()}
            >
              Thêm phiên
            </button>
          </>
        ) : (
          <Link className="btn btn-dark" to={`/live/${done.id}`} onClick={onClose}>
            Mở phiên
          </Link>
        )
      }
    >
      {done !== null ? (
        <div className="box box-ok small">{`Đã thêm phiên ${sessionTitle(done)}. CRM mở luồng comment trong ít giây.`}</div>
      ) : (
        <>
          <label className="field">
            <span>Page</span>
            <span className="select-wrap">
              <select
                value={pageId}
                onChange={(e) => {
                  setPicked(e.target.value);
                }}
              >
                {pages.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </span>
          </label>
          <label className="field">
            <span>Link hoặc mã video *</span>
            <input
              className="input"
              maxLength={500}
              value={video}
              onChange={(e) => {
                setVideo(e.target.value);
              }}
            />
          </label>
          <p className="hint">Dùng khi CRM chưa tự nhận phiên. CRM lấy mã video từ link.</p>
          {write.message !== null && <p className="field-err">{write.message}</p>}
        </>
      )}
    </Dialog>
  );
}
