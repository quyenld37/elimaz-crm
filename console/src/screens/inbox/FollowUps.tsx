// Inbox trạng thái D: "Khách chưa trả lời", gửi mẫu nhắc lại một chạm (CH16; HV09; mockup inbox.html trạng thái D).
// Task 174 để lại vì cần đường gửi tin theo mẫu; Task 175 làm bằng đường của màn hội thoại: renderReplyTemplate điền biến
// cho đúng hội thoại → sendMessage chữ (mỗi bước một khóa chống trùng, giữ khi tự gửi lại). Máy chủ (listFollowUps, Task
// 168) chỉ trả hội thoại mà nhân viên nói sau cùng, khách im quá số giờ chọn, cửa sổ 24 giờ còn ít nhất 5 phút; khách
// "không liên hệ", đã chặn Page không có. Dòng đã gửi chuyển "Đã gửi mẫu … lúc …" và rời danh sách ở lần tải sau.
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { listFollowUps, listReplyTemplates, type FollowUpItem, type Grant, type ReplyTemplate } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { canOnPage } from "./conversation/conversationRules";
import { sendErrorText, useRenderTemplate, useSendMessage } from "./conversation/messageWrites";
import { CONVERSATION_TOPIC } from "./useConversationList";

const HOURS = [1, 3, 6, 12] as const;
const DEFAULT_HOURS = 3;

/** "5 giờ", "40 phút" kể từ mốc (làm tròn xuống). */
function since(iso: string, nowMs: number): string {
  const min = Math.max(0, Math.floor((nowMs - Date.parse(iso)) / 60_000));
  return min >= 60 ? `${String(Math.floor(min / 60))} giờ` : `${String(min)} phút`;
}

/** "Còn 19 giờ để nhắn" / "Còn 40 phút để nhắn" tới khi cửa sổ 24 giờ đóng. */
function left(iso: string, nowMs: number): string {
  const min = Math.max(0, Math.floor((Date.parse(iso) - nowMs) / 60_000));
  return min >= 60 ? `Còn ${String(Math.floor(min / 60))} giờ để nhắn` : `Còn ${String(min)} phút để nhắn`;
}

const usable = (t: ReplyTemplate, pageId: string) => t.pageId === undefined || t.pageId === pageId;

interface Sent {
  shortcut: string;
  at: string;
}

export interface FollowUpsProps {
  /** Tài khoản kênh đang lọc ("" mọi tài khoản). */
  pageId: string;
  /** Đuôi URL của bộ lọc để mở hội thoại rồi quay lại. */
  search: string;
  now: number;
  /** Quyền của người xem: gửi mẫu cần conversation.reply ở Page của dòng (máy chủ: sendMessage). */
  grants: readonly Grant[];
}

export function FollowUps({ pageId, search, now: nowMs, grants }: FollowUpsProps) {
  const [hours, setHours] = useState<number>(DEFAULT_HOURS);
  const [tplId, setTplId] = useState("");
  const [sent, setSent] = useState<ReadonlyMap<string, Sent>>(new Map());
  const [errors, setErrors] = useState<ReadonlyMap<string, string>>(new Map());
  const [busy, setBusy] = useState<string | null>(null);
  const list = useQuery({
    queryKey: queryKey(CONVERSATION_TOPIC, "follow-ups", pageId, hours),
    queryFn: () => unwrap(listFollowUps({ query: { afterHours: hours, ...(pageId === "" ? {} : { pageId }) } })),
  });
  const templates = useQuery({
    // Khóa khác hẳn khóa của menu "/" (TemplateMenu: [chủ đề, mã Page, chữ gõ]): hai hàm tải khác nhau.
    queryKey: queryKey("conversation.templates", "follow-ups", pageId),
    queryFn: () => unwrap(listReplyTemplates({ query: pageId === "" ? {} : { pageId } })),
  });
  const render = useRenderTemplate();
  const send = useSendMessage();
  const tpls = templates.data?.items ?? [];
  const tpl = tpls.find((t) => t.id === tplId) ?? tpls[0];

  const sendTo = async (row: FollowUpItem, t: ReplyTemplate) => {
    const id = row.conversationId;
    setBusy(id);
    setErrors((m) => new Map([...m].filter(([k]) => k !== id)));
    const r = await render.run({ templateId: t.id, conversationId: id });
    const s = r.ok ? await send.run({ conversationId: id, req: { text: r.data.text } }) : r;
    setBusy(null);
    if (s.ok) setSent((m) => new Map(m).set(id, { shortcut: t.shortcut, at: s.data.sentAt }));
    else setErrors((m) => new Map(m).set(id, sendErrorText(s.error)));
  };

  const rows = list.data?.items ?? [];
  return (
    <>
      <div className="pane-b fu">
        <div className="fu-row">
          <label className="field">
            <span>Im lặng quá</span>
            <span className="select-wrap">
              <select
                aria-label="Im lặng quá"
                value={hours}
                onChange={(e) => {
                  setHours(Number(e.target.value));
                }}
              >
                {HOURS.map((h) => (
                  <option key={h} value={h}>
                    {`${String(h)} giờ`}
                  </option>
                ))}
              </select>
            </span>
          </label>
          {tpls.length > 0 && (
            <label className="field">
              <span>Mẫu nhắc lại</span>
              <span className="select-wrap">
                <select
                  aria-label="Mẫu nhắc lại"
                  value={tpl?.id ?? ""}
                  onChange={(e) => {
                    setTplId(e.target.value);
                  }}
                >
                  {tpls.map((t) => (
                    <option key={t.id} value={t.id}>
                      {`/${t.shortcut} — ${t.title}`}
                    </option>
                  ))}
                </select>
              </span>
            </label>
          )}
        </div>
        {templates.isSuccess && tpls.length === 0 && (
          <p className="box box-info small">Chưa có mẫu trả lời nào để nhắn lại. Quản lý tạo mẫu ở Cài đặt hộp chat.</p>
        )}
        <p className="small muted">
          {`Hội thoại tin nhắn đang mở hoặc chờ khách mà shop nói sau cùng và khách im lặng quá ${String(hours)} giờ. Không có trong danh sách: khách quá 24 giờ (còn dưới 5 phút), khách "không liên hệ", khách đã chặn Page. Bấm "Gửi mẫu" gửi ngay một tin theo mẫu; dòng chuyển "Đã gửi".`}
        </p>
        {list.isPending && <p className="muted">Đang tải…</p>}
        {list.isError && (
          <div className="box box-err" role="alert">
            {errorMessage(list.error)}
          </div>
        )}
        {list.isSuccess && rows.length === 0 && (
          <div className="empty">
            <p className="muted small">Không có khách nào đang chờ nhắn lại.</p>
          </div>
        )}
      </div>
      {rows.length > 0 && (
        <ul className="cvlist fu-list" aria-label="Khách chưa trả lời">
          {rows.map((r) => {
            const done = sent.get(r.conversationId);
            const err = errors.get(r.conversationId);
            const fit = tpl !== undefined && usable(tpl, r.pageId);
            const canSend = canOnPage(grants, "conversation.reply", r.pageId);
            return (
              <li key={r.conversationId} className="cv">
                <span className="avatar" aria-hidden="true">
                  {r.customer.name.trim().charAt(0).toUpperCase()}
                </span>
                <Link className="cv-n ellip" to={`/inbox/${r.conversationId}${search}`}>
                  {r.customer.name}
                </Link>
                <span className="cv-r">
                  <span className="cv-t">{since(r.lastStaffMessageAt, nowMs)}</span>
                </span>
                <span className="cv-m">
                  <span className="clock">{left(r.windowEndsAt, nowMs)}</span>
                </span>
                <span className="cv-m">
                  {done !== undefined ? (
                    <span className="meta">
                      <span className="st-ok">{`Đã gửi mẫu /${done.shortcut} lúc ${shortTime(done.at).split(" ")[0] ?? ""}`}</span>
                      <span>· dòng rời danh sách ở lần tải sau</span>
                    </span>
                  ) : !canSend ? (
                    <span className="small muted">Bạn chưa được cấp quyền trả lời ở Page này</span>
                  ) : tpl === undefined ? null : fit ? (
                    <button
                      type="button"
                      className="btn btn-dark"
                      disabled={busy !== null}
                      onClick={() => void sendTo(r, tpl)}
                    >
                      {`Gửi mẫu /${tpl.shortcut}`}
                    </button>
                  ) : (
                    <span className="small muted">{`Mẫu /${tpl.shortcut} chỉ dùng cho Page khác`}</span>
                  )}
                </span>
                {err !== undefined && (
                  <span className="cv-m small err-text" role="alert">
                    {err}
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
