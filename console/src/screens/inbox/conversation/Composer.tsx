// Ô gõ của hội thoại (Task 175; mockup hoi-thoai.html trạng thái A, D; CH03, CH04, CH07, CH09; HV15–HV18, HV17 sửa).
// - Theo chính sách gửi của getConversation: khóa ô gõ (không ẩn) kèm câu lý do khi không được gửi; đồng hồ "Còn … để
//   trả lời" khi có hạn; Page có thẻ Human Agent sau 24 giờ: nút "Trả lời (nhân viên)", gửi kèm humanAgent, đồng hồ đỏ.
// - Gõ "/" mở menu mẫu (TemplateMenu); chọn → renderReplyTemplate điền biến → chữ vào ô để sửa, không gửi luôn.
// - "Ảnh": tải lên kho tệp (mục đích chat_outgoing) qua useUploads của M1, chờ ready, mỗi ảnh một tin attachmentFileId.
// - Tab "Ghi chú nội bộ": addConversationNote, không bao giờ gửi cho khách; viết được cả khi ô trả lời khóa.
// - Máy tính: Enter gửi, Shift + Enter xuống dòng; điện thoại (con trỏ thô): Enter xuống dòng, gửi bằng nút (HV17 sửa).
// Mọi yêu cầu ghi qua useWriteAction (khóa sinh khi bấm, giữ khi tự gửi lại).
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { addConversationNote, type ConversationDetail, type ReplyTemplate } from "../../../api";
import { unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { IMAGE_ACCEPT } from "../../products/imageRules";
import { useUploads, type UploadOptions } from "../../products/useUpload";
import { composerPolicy } from "./conversationRules";
import { idem, sendErrorText, useRenderTemplate, useSendMessage } from "./messageWrites";
import { slashQuery, TemplateMenu, useTemplates } from "./TemplateMenu";

/** Mục đích tệp của ảnh nhân viên gửi khách (máy chủ: conversation.OutgoingImagePurpose, Task 156). */
export const CHAT_IMAGE_PURPOSE = "chat_outgoing";

const TEXT_MAX = 2000;
const NOTE_MAX = 4000;

/** Thiết bị con trỏ thô (điện thoại, máy tính bảng): Enter xuống dòng. */
function coarsePointer(): boolean {
  try {
    return typeof window.matchMedia === "function" && window.matchMedia("(pointer: coarse)").matches;
  } catch {
    return false;
  }
}

export interface ComposerProps {
  detail: ConversationDetail;
  canReply: boolean;
  nowMs: number;
  /** Đang gõ chữ (nhịp tim typing) hay thôi. */
  onTyping: (typing: boolean) => void;
  /** Sau một lần ghi xong (tải lại hội thoại). */
  onSent: () => void;
  uploadOptions?: UploadOptions;
}

export function Composer({ detail, canReply, nowMs, onTyping, onSent, uploadOptions }: ComposerProps) {
  const conversationId = detail.conversation.id;
  const pageId = detail.page.id;
  // Hội thoại comment: máy chủ chỉ nhận sendMessage ở hộp thư (Task 156); trả lời bằng nút dưới từng comment, ô gõ chỉ
  // còn ghi chú nội bộ.
  const commentConv = detail.conversation.kind === "comment";
  const [tab, setTab] = useState<"reply" | "note">(commentConv ? "note" : "reply");
  const [text, setText] = useState("");
  const [note, setNote] = useState("");
  const [active, setActive] = useState(0);
  const [menuClosed, setMenuClosed] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const policy = composerPolicy(detail, canReply, nowMs);
  const send = useSendMessage();
  const sendImage = useSendMessage();
  const render = useRenderTemplate();
  const addNote = useWriteAction((key, b: { conversationId: string; text: string }) =>
    unwrap(addConversationNote({ path: { id: b.conversationId }, body: { text: b.text }, headers: idem(key) })),
  );
  const uploads = useUploads({ ...uploadOptions, purpose: CHAT_IMAGE_PURPOSE });

  const q = policy.locked || menuClosed ? null : slashQuery(text);
  const templates = useTemplates(q, pageId);
  const items = templates.data?.items ?? [];
  const menuOpen = q !== null;

  const onSentRef = useRef(onSent);
  useEffect(() => {
    onSentRef.current = onSent;
  });

  // Ảnh đã xử lý xong: gửi lần lượt, mỗi ảnh một tin (mỗi dòng một lần, kể cả khi effect chạy lại).
  const humanAgent = !policy.locked && policy.humanAgent;
  const { items: ups, update, dismiss } = uploads;
  const ready = ups.find((u) => u.phase === "ready");
  const handled = useRef(new Set<string>());
  useEffect(() => {
    const fileId = ready?.fileId;
    if (ready === undefined || fileId === undefined || sendImage.pending) return;
    const { key } = ready;
    if (handled.current.has(key)) return;
    handled.current.add(key);
    update(key, { phase: "attaching" });
    void sendImage
      .run({ conversationId, req: { attachmentFileId: fileId, ...(humanAgent ? { humanAgent: true } : {}) } })
      .then((r) => {
        if (r.ok) {
          dismiss(key);
          onSentRef.current();
        } else update(key, { phase: "failed", message: sendErrorText(r.error) });
      });
  }, [ready, sendImage, update, dismiss, conversationId, humanAgent]);

  const submit = async () => {
    const t = text.trim();
    if (policy.locked || t === "" || send.pending) return;
    const r = await send.run({ conversationId, req: { text: t, ...(humanAgent ? { humanAgent: true } : {}) } });
    if (r.ok) {
      setText("");
      onTyping(false);
    }
    // Lỗi chính sách (409 send_not_allowed, channel_read_only): tải lại để ô gõ hiện đúng lý do mới.
    onSentRef.current();
  };
  const saveNote = async () => {
    const t = note.trim();
    if (t === "" || addNote.pending) return;
    if ((await addNote.run({ conversationId, text: t })).ok) {
      setNote("");
      onSentRef.current();
    }
  };
  const pick = async (tpl: ReplyTemplate) => {
    setMenuClosed(true);
    const r = await render.run({ templateId: tpl.id, conversationId });
    if (r.ok) setText(r.data.text.slice(0, TEXT_MAX));
  };

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (menuOpen) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const n = items.length;
        if (n > 0) setActive((active + (e.key === "ArrowDown" ? 1 : n - 1)) % n);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setMenuClosed(true);
        return;
      }
      if (e.key === "Enter") {
        e.preventDefault();
        const t = items[Math.min(active, items.length - 1)];
        if (t !== undefined) void pick(t);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing && !coarsePointer()) {
      e.preventDefault();
      void submit();
    }
  };

  const sendLabel = humanAgent ? "Trả lời (nhân viên)" : "Gửi";
  const err = send.error ?? render.error;

  return (
    <div className={tab === "note" ? "cmp is-note" : "cmp"}>
      <div className="cmp-tabs" role="tablist">
        {!commentConv && (
          <button
            type="button"
            className="tab"
            role="tab"
            aria-selected={tab === "reply"}
            onClick={() => {
              setTab("reply");
            }}
          >
            Trả lời
          </button>
        )}
        <button
          type="button"
          className="tab"
          role="tab"
          aria-selected={tab === "note"}
          onClick={() => {
            setTab("note");
          }}
        >
          Ghi chú nội bộ
        </button>
      </div>

      {tab === "note" ? (
        <>
          <textarea
            className="input"
            aria-label="Nội dung ghi chú"
            placeholder="Ghi chú chỉ nhân viên thấy, không gửi cho khách"
            maxLength={NOTE_MAX}
            value={note}
            onChange={(e) => {
              setNote(e.target.value);
            }}
          />
          <div className="cmp-f">
            <span className="grow small muted">
              {commentConv
                ? 'Chỉ nhân viên thấy. Trả lời khách bằng "Trả lời công khai" hoặc "Nhắn riêng" dưới từng comment.'
                : "Chỉ nhân viên thấy, không bao giờ gửi cho khách."}
            </span>
            <button
              type="button"
              className="btn btn-dark"
              disabled={!canReply || addNote.pending || note.trim() === ""}
              onClick={() => void saveNote()}
            >
              Lưu ghi chú
            </button>
          </div>
          {addNote.message !== null && (
            <p className="small err-text" role="alert">
              {addNote.message}
            </p>
          )}
        </>
      ) : (
        <>
          {menuOpen && (
            <TemplateMenu
              items={items}
              active={Math.min(active, Math.max(items.length - 1, 0))}
              pending={templates.isPending}
              error={templates.error}
              pageName={detail.page.name}
              onPick={(t) => void pick(t)}
            />
          )}
          <textarea
            className="input"
            aria-label="Nội dung trả lời"
            placeholder={policy.locked ? "Không gửi được tin cho khách lúc này" : "Gõ trả lời, gõ / để chọn mẫu"}
            disabled={policy.locked}
            maxLength={TEXT_MAX}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setMenuClosed(false);
              setActive(0);
              onTyping(e.target.value !== "");
            }}
            onKeyDown={onKey}
          />
          {policy.locked && (
            <div className={`box ${policy.box} small`}>
              <b>{policy.title}</b>
              {policy.more !== undefined && ` ${policy.more}`}
            </div>
          )}
          {humanAgent && (
            <p className="small">
              Đã quá 24 giờ. Page được Meta cấp thẻ <b>Human Agent</b>: nhân viên tự trả lời được trong 7 ngày từ tin
              cuối của khách. Không dùng cho tin tự động, mẫu nhắc lại.
            </p>
          )}
          {ups.length > 0 && (
            <ul className="ups" aria-label="Ảnh đang gửi">
              {ups.map((u) => (
                <li key={u.key} className={u.phase === "failed" ? "up is-err" : "up"}>
                  <span className="grow ellip">{u.name}</span>
                  {u.phase === "failed" ? (
                    <>
                      <span className="small err-text">{u.message}</span>
                      <button
                        type="button"
                        className="btn btn-quiet"
                        onClick={() => {
                          dismiss(u.key);
                        }}
                      >
                        Bỏ
                      </button>
                    </>
                  ) : (
                    <span className="small st-wait">{u.phase === "attaching" ? "Đang gửi…" : "Đang tải ảnh lên…"}</span>
                  )}
                </li>
              ))}
            </ul>
          )}
          <div className="cmp-f">
            <input
              ref={fileRef}
              type="file"
              accept={IMAGE_ACCEPT}
              multiple
              hidden
              aria-label="Chọn ảnh gửi khách"
              onChange={(e) => {
                const files = Array.from(e.target.files ?? []);
                if (files.length > 0) uploads.add(files);
                e.target.value = "";
              }}
            />
            <button
              type="button"
              className="btn btn-quiet"
              disabled={policy.locked}
              onClick={() => fileRef.current?.click()}
            >
              Ảnh
            </button>
            <button
              type="button"
              className="btn btn-quiet"
              disabled={policy.locked}
              onClick={() => {
                setText("/");
                setMenuClosed(false);
                setActive(0);
              }}
            >
              Mẫu /
            </button>
            <span className={!policy.locked && policy.humanAgent ? "grow clock is-low" : "grow clock"}>
              {!policy.locked && policy.clock !== null
                ? policy.humanAgent
                  ? `Còn ${policy.clock}`
                  : `Còn ${policy.clock} để trả lời`
                : ""}
            </span>
            <button
              type="button"
              className="btn btn-dark"
              disabled={policy.locked || send.pending || text.trim() === ""}
              onClick={() => void submit()}
            >
              {sendLabel}
            </button>
          </div>
          {err !== null && (
            <p className="small err-text" role="alert">
              {sendErrorText(err)}
            </p>
          )}
          {!policy.locked && (
            <p className="hint">
              Enter gửi, Shift + Enter xuống dòng (điện thoại: gửi bằng nút). Gõ / để chọn mẫu. Mỗi tin chữ hoặc một
              ảnh.
            </p>
          )}
        </>
      )}
    </div>
  );
}
