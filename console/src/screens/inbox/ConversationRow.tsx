// Một dòng hội thoại (mockup docs/mockups/m4/inbox.html .cv; HV05): chữ cái đầu, tên (cắt một dòng), giờ, số chưa đọc
// (99+), đoạn tin cuối; tin nhắn ghi "Page · Tin nhắn", comment có chip "Comment" và tên bài; nhãn tối đa 3 rồi +n;
// cuối dòng trạng thái (khi khác Đang mở) và người phụ trách. Mọi chữ của khách là văn bản thuần (04 mục 13); ảnh đại
// diện Facebook (máy chủ ngoài) không nạp.
import { Link } from "react-router";
import type { ConversationSummary } from "../../api";
import { tagClass, tailText, timeText, unreadText, visibleTags } from "./inboxRules";

export interface ConversationRowProps {
  row: ConversationSummary;
  index: number;
  meId: string;
  accountName: string;
  /** Chuỗi tham số bộ lọc (kể cả "?") gắn vào liên kết, để quay lại danh sách giữ bộ lọc. */
  search: string;
  selected: boolean;
  fresh: boolean;
  now: number;
  measure: (el: HTMLAnchorElement | null) => void;
}

function initial(name: string): string {
  const c = name.trim().at(0);
  return c === undefined ? "?" : c.toLocaleUpperCase("vi-VN");
}

export function ConversationRow({
  row,
  index,
  meId,
  accountName,
  search,
  selected,
  fresh,
  now,
  measure,
}: ConversationRowProps) {
  const tags = visibleTags(row.tags);
  const cls = ["cv", fresh ? "is-new" : "", row.unreadCount > 0 ? "is-unread" : ""].filter((c) => c !== "").join(" ");
  const tagLine = (
    <>
      {tags.shown.map((t) => (
        <span key={t.id} className={tagClass(t.color)}>
          {t.name}
        </span>
      ))}
      {tags.more > 0 && (
        <span className="lb lb-more" aria-label={`Còn ${String(tags.more)} nhãn`}>
          {`+${String(tags.more)}`}
        </span>
      )}
      <span className="muted">{tailText(row, meId)}</span>
    </>
  );
  return (
    <Link
      ref={measure}
      data-index={index}
      className={cls}
      to={`/inbox/${row.id}${search}`}
      aria-current={selected ? "true" : undefined}
    >
      <span className="avatar" aria-hidden="true">
        {initial(row.customer.name)}
      </span>
      <span className="cv-n ellip">{row.customer.name}</span>
      <span className="cv-r">
        <span className="cv-t">{timeText(row.lastMessageAt, now)}</span>
        {row.unreadCount > 0 && (
          <span className="unr" aria-label={`${String(row.unreadCount)} chưa đọc`}>
            {unreadText(row.unreadCount)}
          </span>
        )}
      </span>
      <span className="cv-s ellip">{row.lastSnippet ?? ""}</span>
      {row.kind === "comment" ? (
        <>
          <span className="cv-m">
            <span className="chip chip-line">Comment</span>
            <span className="ellip muted">
              {row.post?.snippet === undefined ? `Bài trên ${accountName}` : `Bài: ${row.post.snippet}`}
            </span>
          </span>
          <span className="cv-m">{tagLine}</span>
        </>
      ) : (
        <span className="cv-m">
          <span className="muted">{`${accountName} · Tin nhắn`}</span>
          {tagLine}
        </span>
      )}
    </Link>
  );
}
