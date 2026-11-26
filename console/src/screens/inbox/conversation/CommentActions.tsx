// Thao tác dưới mỗi comment của khách (Task 175; mockup hoi-thoai.html trạng thái C; CH05, CH11; HV19, HV20). Ba nút
// luôn hiện (không chỉ khi rê chuột): Trả lời công khai, Nhắn riêng, Ẩn / Hiện lại trên Page. Nhắn riêng khóa kèm chữ
// hiện thẳng khi comment đã có tin riêng đang sống ("Đã nhắn riêng") hay quá 7 ngày ("Quá 7 ngày"). Nút nào kênh không
// hỗ trợ (features của tài khoản kênh) thì không vẽ. Page chỉ xem: nút khóa (máy chủ từ chối mọi thao tác ra ngoài).
import { useState } from "react";
import {
  hideComment,
  replyToComment,
  sendPrivateReply,
  unhideComment,
  type ChannelFeatures,
  type ChatMessage,
} from "../../../api";
import { unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { privateReplyLock } from "./conversationRules";
import { idem, sendErrorText } from "./messageWrites";

interface ReplyBody {
  conversationId: string;
  commentMessageId: string;
  text: string;
}

const LOCK_TEXT = {
  used: "Đã nhắn riêng 1 tin cho comment này. Facebook chỉ cho một tin; khách trả lời thì nhắn tiếp ở hộp thư.",
  expired: "Quá 7 ngày từ lúc comment: Facebook không cho nhắn riêng nữa.",
} as const;

export interface CommentActionsProps {
  conversationId: string;
  m: ChatMessage;
  features: ChannelFeatures | undefined;
  /** Page chỉ xem: mọi thao tác ra Facebook khóa. */
  readOnly: boolean;
  nowMs: number;
  onChanged: () => void;
}

export function CommentActions({ conversationId, m, features, readOnly, nowMs, onChanged }: CommentActionsProps) {
  const [box, setBox] = useState<"public" | "private" | null>(null);
  const [text, setText] = useState("");
  const pub = useWriteAction((key, b: ReplyBody) =>
    unwrap(
      replyToComment({
        path: { id: b.conversationId },
        body: { commentMessageId: b.commentMessageId, text: b.text },
        headers: idem(key),
      }),
    ),
  );
  const priv = useWriteAction((key, b: ReplyBody) =>
    unwrap(
      sendPrivateReply({
        path: { id: b.conversationId },
        body: { commentMessageId: b.commentMessageId, text: b.text },
        headers: idem(key),
      }),
    ),
  );
  const hide = useWriteAction((key, b: { id: string; hidden: boolean }) =>
    unwrap(
      b.hidden
        ? unhideComment({ path: { id: b.id }, headers: idem(key) })
        : hideComment({ path: { id: b.id }, headers: idem(key) }),
    ),
  );
  const lock = privateReplyLock(m, nowMs);
  const canPrivate = features?.privateReply ?? true;
  const canHide = features?.hideComment ?? true;
  const write = box === "private" ? priv : pub;
  const err = pub.error ?? priv.error ?? hide.error;

  const send = async () => {
    const t = text.trim();
    if (t === "" || box === null) return;
    const r = await write.run({ conversationId, commentMessageId: m.id, text: t });
    if (r.ok) {
      setText("");
      setBox(null);
      onChanged();
    }
  };
  const toggle = (b: "public" | "private") => {
    pub.reset();
    priv.reset();
    setBox(box === b ? null : b);
  };

  return (
    <div className="cacts-w">
      <div className="cacts">
        <button
          type="button"
          className="btn btn-quiet"
          aria-pressed={box === "public"}
          disabled={readOnly}
          onClick={() => {
            toggle("public");
          }}
        >
          Trả lời công khai
        </button>
        {canPrivate && (
          <div className="act">
            <button
              type="button"
              className={lock === null ? "btn btn-quiet" : "btn"}
              aria-pressed={box === "private"}
              disabled={readOnly || lock !== null}
              onClick={() => {
                toggle("private");
              }}
            >
              Nhắn riêng
            </button>
            {lock !== null && <span className="reason">{LOCK_TEXT[lock]}</span>}
          </div>
        )}
        {canHide && (
          <button
            type="button"
            className="btn btn-quiet"
            disabled={readOnly || hide.pending}
            onClick={() =>
              void hide.run({ id: m.id, hidden: m.isHidden }).then((r) => {
                if (r.ok) onChanged();
              })
            }
          >
            {m.isHidden ? "Hiện lại trên Page" : "Ẩn trên Page"}
          </button>
        )}
      </div>
      {readOnly && <span className="reason">Page chỉ xem: CRM không gửi gì ra Facebook.</span>}
      {box !== null && (
        <div className="cbox">
          <textarea
            className="input"
            aria-label={box === "public" ? "Trả lời công khai" : "Nhắn riêng"}
            placeholder={box === "public" ? "Trả lời hiện dưới comment trên Page" : "Tin riêng tới hộp thư của khách"}
            maxLength={box === "public" ? 8000 : 2000}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
            }}
          />
          <div className="cmp-f">
            <span className="grow small muted">
              {box === "private" ? "Facebook chỉ cho một tin riêng mỗi comment, trong 7 ngày." : ""}
            </span>
            <button
              type="button"
              className="btn btn-quiet"
              onClick={() => {
                setBox(null);
              }}
            >
              Thôi
            </button>
            <button
              type="button"
              className="btn btn-dark"
              disabled={write.pending || text.trim() === ""}
              onClick={() => void send()}
            >
              {box === "public" ? "Gửi trả lời" : "Gửi tin riêng"}
            </button>
          </div>
        </div>
      )}
      {err !== null && (
        <span className="small err-text" role="alert">
          {sendErrorText(err)}
        </span>
      )}
    </div>
  );
}
