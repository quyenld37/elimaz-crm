// Dòng tin của hội thoại (Task 175; mockup hoi-thoai.html). Trang tin mới nhất là truy vấn có meta realtimeEntity: báo
// hiệu conversation.changed của đúng hội thoại này tải lại nó; "Xem tin cũ hơn" tải trang trước bằng con trỏ before và
// giữ lại; mọi trang gộp theo id (bản tải sau thắng). Comment có thẻ bài viết ở đầu; trả lời công khai của shop
// (parentMessageId) lồng dưới comment của khách. Đánh dấu đã đọc tới tin mới nhất của khách khi hội thoại còn chưa đọc.
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { listMessages, markConversationRead, type ChatMessage, type ChatPostRef } from "../../../api";
import { errorMessage, unwrap } from "../../../app/errors";
import { useWriteAction } from "../../../app/idempotency";
import { lastInbound, mergeMessages } from "./conversationRules";
import { MessageBubble, type BubbleContext } from "./MessageBubble";
import { PostCard } from "./PostCard";
import { MESSAGE_PAGE, idem, latestMessagesQuery } from "./messageWrites";

/** Tin cũ hơn đã tải bằng "Xem tin cũ hơn" (trang nối nhau), kèm con trỏ của trang kế. */
interface Older {
  pages: ChatMessage[][];
  cursor: string | undefined;
  loading: boolean;
  error: unknown;
}

export interface MessageListProps {
  ctx: BubbleContext;
  unreadCount: number;
  post: ChatPostRef | undefined;
}

export function MessageList({ ctx, unreadCount, post }: MessageListProps) {
  const id = ctx.conversationId;
  const latest = useQuery(latestMessagesQuery(id));
  const [older, setOlder] = useState<Older>({ pages: [], cursor: undefined, loading: false, error: null });
  const startedOlder = older.pages.length > 0;
  const cursor = startedOlder ? older.cursor : latest.data?.olderCursor;

  const loadOlder = async () => {
    if (cursor === undefined || older.loading) return;
    setOlder((o) => ({ ...o, loading: true, error: null }));
    try {
      const p = await unwrap(listMessages({ path: { id }, query: { before: cursor, limit: MESSAGE_PAGE } }));
      setOlder((o) => ({ pages: [p.items, ...o.pages], cursor: p.olderCursor, loading: false, error: null }));
    } catch (e) {
      setOlder((o) => ({ ...o, loading: false, error: e }));
    }
  };

  const all = mergeMessages([...older.pages, latest.data?.items ?? []]);

  // Đã đọc: tới tin mới nhất của khách, mỗi tin một lần (máy chủ không gửi "đã xem" cho khách, Q10).
  const read = useWriteAction((key, b: { id: string; upTo: string }) =>
    unwrap(markConversationRead({ path: { id: b.id }, body: { upToMessageId: b.upTo }, headers: idem(key) })),
  );
  const marked = useRef<string | null>(null);
  const upTo = lastInbound(latest.data?.items ?? [])?.id;
  useEffect(() => {
    if (unreadCount === 0 || upTo === undefined || marked.current === upTo) return;
    marked.current = upTo;
    void read.run({ id, upTo });
  }, [unreadCount, upTo, id, read]);

  // Trả lời công khai của shop lồng dưới comment cha (cha có trong danh sách); còn lại theo thứ tự thời gian.
  const ids = new Set(all.map((m) => m.id));
  const nested = (m: ChatMessage) =>
    m.direction === "out" && m.kind === "comment" && m.parentMessageId !== undefined && ids.has(m.parentMessageId);
  const repliesOf = (m: ChatMessage) => all.filter((r) => nested(r) && r.parentMessageId === m.id);

  return (
    <div className="thread" role="log" aria-label="Tin của hội thoại">
      {cursor !== undefined && (
        <button
          type="button"
          className="btn btn-quiet more-btn"
          disabled={older.loading}
          onClick={() => void loadOlder()}
        >
          {older.loading ? "Đang tải tin cũ…" : "Xem tin cũ hơn"}
        </button>
      )}
      {older.error !== null && (
        <p className="small err-text" role="alert">
          {errorMessage(older.error)}
        </p>
      )}
      {post !== undefined && <PostCard conversationId={id} post={post} />}
      {latest.isPending && <p className="muted">Đang tải tin…</p>}
      {latest.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(latest.error)}
        </div>
      )}
      {latest.isSuccess && all.length === 0 && <p className="muted">Chưa có tin nào trong hội thoại này.</p>}
      {all
        .filter((m) => !nested(m))
        .map((m) => (
          <MessageBubble key={m.id} m={m} replies={repliesOf(m)} ctx={ctx} />
        ))}
    </div>
  );
}
