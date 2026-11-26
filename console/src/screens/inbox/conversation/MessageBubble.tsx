// Một tin trong hội thoại (Task 175; mockup hoi-thoai.html trạng thái A–C; HV10–HV13, HV19–HV22, HV21 sửa). Nội dung,
// tên người gửi, tên tệp từ khách luôn là chữ React (04 mục 13: không dựng HTML từ nội dung). Loại tin:
// - lịch sử lấy từ Facebook trước khi kết nối CRM: nhạt, viền đứt, "trước khi kết nối CRM" (HV10);
// - tin hệ thống: dòng giữa; tin tự động: bong bóng viền, "Tin tự động" (HV11); tin gửi từ ứng dụng Meta: "Gửi từ ứng
//   dụng Meta (ngoài CRM)", không biết người gửi; tin nhân viên ghi tên;
// - ghi chú nội bộ: nền hồng viền đứt, "Chỉ nhân viên thấy", không bao giờ gửi cho khách (HV12);
// - comment: thao tác comment, "Đã ẩn trên Page", "Khách đã sửa" (lịch sử thu gọn), "Khách đã xóa" giữ nội dung cũ;
//   trả lời công khai của shop lồng dưới comment;
// - tin dài quá 8 dòng: gọn lại, "Xem hết (n ký tự)" (HV22).
import { useState, type ReactNode } from "react";
import type { ChannelFeatures, ChatMessage } from "../../../api";
import { shortTime } from "../../../app/time";
import { AttachmentView } from "./AttachmentView";
import { CommentActions } from "./CommentActions";
import { hiddenNote, isLong, thousands } from "./conversationRules";
import { RevisionsPopover } from "./RevisionsPopover";
import { SendStatus } from "./SendStatus";

const hhmm = (iso: string) => shortTime(iso).split(" ")[0] ?? "";
const dayMonth = (iso: string) => shortTime(iso).split(" ")[1] ?? "";

function Body({ text, extra }: { text: string; extra?: ReactNode }) {
  const [full, setFull] = useState(false);
  const long = isLong(text);
  return (
    <>
      <div className={long ? "bub bub-full" : "bub"}>
        {extra}
        {long && !full ? <span className="clamp8">{text}</span> : text}
      </div>
      {long && !full && (
        <button
          type="button"
          className="btn btn-text"
          onClick={() => {
            setFull(true);
          }}
        >
          {`Xem hết (${thousands(text.length)} ký tự)`}
        </button>
      )}
    </>
  );
}

export interface BubbleContext {
  conversationId: string;
  canReply: boolean;
  readOnly: boolean;
  features: ChannelFeatures | undefined;
  nowMs: number;
  onChanged: () => void;
}

/** Người gửi của tin ra: tự động, ngoài CRM, hay tên nhân viên. */
function outWho(m: ChatMessage): string {
  if (m.origin === "automation") return m.senderName === undefined ? "Tự động" : `Tự động · ${m.senderName}`;
  if (m.origin === "page_external" || m.origin === "own_echo_unmatched") return "Gửi từ ứng dụng Meta (ngoài CRM)";
  return m.senderName ?? m.sender?.name ?? "";
}

/** Tin shop gửi (trả lời comment lồng dưới comment, tin riêng, tin thường). */
function OutBody({ m, ctx }: { m: ChatMessage; ctx: BubbleContext }) {
  const ext = m.origin === "page_external" || m.origin === "own_echo_unmatched";
  return (
    <>
      {m.body !== undefined && m.body !== "" && (
        <Body
          text={m.body}
          {...(m.origin === "automation" ? { extra: <span className="bub-k">Tin tự động</span> } : {})}
        />
      )}
      <AttachmentView conversationId={ctx.conversationId} attachments={m.attachments} outgoing />
      {(m.quickReplies?.length ?? 0) > 0 && (
        <div className="lbs qr-out">
          {m.quickReplies?.map((q) => (
            <span key={q.payload} className="chip chip-line">
              {q.title}
            </span>
          ))}
        </div>
      )}
      {ext ? (
        <div className="meta">
          {hhmm(m.sentAt)} · <span>không biết người gửi</span>
        </div>
      ) : (
        <SendStatus m={m} canReply={ctx.canReply} onChanged={ctx.onChanged} />
      )}
    </>
  );
}

export function MessageBubble({
  m,
  replies = [],
  ctx,
}: {
  m: ChatMessage;
  /** Trả lời công khai của shop cho comment này (lồng dưới). */
  replies?: readonly ChatMessage[];
  ctx: BubbleContext;
}) {
  if (m.kind === "system") return <p className="sys">{m.body ?? ""}</p>;

  if (m.origin === "history") {
    const who = m.direction === "in" ? "Khách" : "Page";
    return (
      <div className={`msg ${m.direction} hist`}>
        <span className="who">{`${who} · ${dayMonth(m.sentAt)} · trước khi kết nối CRM`}</span>
        {m.body !== undefined && <Body text={m.body} />}
        <AttachmentView
          conversationId={ctx.conversationId}
          attachments={m.attachments}
          outgoing={m.direction === "out"}
        />
      </div>
    );
  }

  if (m.kind === "note")
    return (
      <div className="msg out note">
        <span className="who">{`${m.senderName ?? m.sender?.name ?? ""} · ghi chú`}</span>
        <Body text={m.body ?? ""} extra={<span className="bub-k">Ghi chú nội bộ · Chỉ nhân viên thấy</span>} />
        <div className="meta">{`${hhmm(m.sentAt)} · không gửi cho khách`}</div>
      </div>
    );

  if (m.direction === "out") {
    const cls =
      m.origin === "automation"
        ? " auto"
        : m.origin === "page_external" || m.origin === "own_echo_unmatched"
          ? " ext"
          : "";
    const who = m.kind === "private_reply" ? `Nhắn riêng · ${outWho(m)}` : outWho(m);
    return (
      <div className={`msg out${cls}${m.sendStatus === "discarded" ? " gone" : ""}`}>
        {who !== "" && <span className="who">{who}</span>}
        <OutBody m={m} ctx={ctx} />
      </div>
    );
  }

  const comment = m.kind === "comment";
  const name = m.senderName ?? "";
  return (
    <div className={m.removed ? "msg in gone" : "msg in"}>
      {comment && <span className="who">{name === "" ? "comment" : `${name} · comment`}</span>}
      {m.removed ? (
        <>
          <div className="bub">Khách đã xóa comment này</div>
          <ul className="revs" aria-label="Nội dung trước khi xóa">
            <li>
              <span className="at">{`${hhmm(m.sentAt)} · trước khi xóa`}</span>
              {m.body ?? ""}
            </li>
          </ul>
        </>
      ) : (
        <>
          {m.body !== undefined && m.body !== "" && <Body text={m.body} />}
          <AttachmentView conversationId={ctx.conversationId} attachments={m.attachments} outgoing={false} />
          <div className="meta">
            {hhmm(m.sentAt)}
            {m.isHidden && (
              <>
                {" · "}
                <span className="chip">Đã ẩn trên Page</span>
                <span>{`· ${hiddenNote(m.hiddenReason)}trên Facebook chỉ người comment và bạn bè họ thấy, CRM vẫn hiện đủ`}</span>
              </>
            )}
            {m.editedCount > 0 && (
              <>
                {" · "}
                <span className="chip chip-line">Khách đã sửa</span>
                <RevisionsPopover m={m} />
              </>
            )}
          </div>
          {comment && ctx.canReply && (
            <CommentActions
              conversationId={ctx.conversationId}
              m={m}
              features={ctx.features}
              readOnly={ctx.readOnly}
              nowMs={ctx.nowMs}
              onChanged={ctx.onChanged}
            />
          )}
        </>
      )}
      {replies.length > 0 && (
        <div className="reply-in">
          {replies.map((r) => (
            <div key={r.id} className="msg out reply">
              <span className="who">{`Trả lời công khai · ${outWho(r)}`}</span>
              <OutBody m={r} ctx={ctx} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
