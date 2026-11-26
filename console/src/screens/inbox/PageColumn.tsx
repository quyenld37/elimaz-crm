// Chọn tài khoản kênh trên đầu cột danh sách (HV03 đã sửa: "tài khoản kênh" thay "Page" để sau này Zalo OA, TikTok
// vào cùng danh sách). Dòng đầu "Tất cả tài khoản" cộng mọi số; mỗi tài khoản: biểu tượng chữ của kênh, tên, chip
// trạng thái (Mất kết nối, Đã ngắt, Chỉ xem; HV08) và số hội thoại chưa đọc (countConversations, trên 99 → "99+").
import type { ChannelAccount, ConversationCounts } from "../../api";
import { accountChip, channelBadge, unreadText } from "./inboxRules";

export interface PageColumnProps {
  accounts: readonly ChannelAccount[];
  counts: ConversationCounts | undefined;
  /** Tài khoản đang lọc; "" là tất cả. */
  value: string;
  onChange: (pageId: string) => void;
}

function Unread({ n }: { n: number }) {
  if (n <= 0) return null;
  return (
    <span className="unr" aria-label={`${String(n)} chưa đọc`}>
      {unreadText(n)}
    </span>
  );
}

export function PageColumn({ accounts, counts, value, onChange }: PageColumnProps) {
  const unreadOf = (id: string) => counts?.byPage.find((c) => c.pageId === id)?.unread ?? 0;
  const total = accounts.reduce((n, a) => n + unreadOf(a.id), 0);
  return (
    <nav className="pg" aria-label="Tài khoản kênh">
      <button
        type="button"
        className="pg-a"
        aria-pressed={value === ""}
        onClick={() => {
          onChange("");
        }}
      >
        <span className="grow">Tất cả tài khoản</span>
        {total > 0 ? <Unread n={total} /> : counts !== undefined && <span className="small muted">đã đọc hết</span>}
      </button>
      {accounts.map((a) => {
        const chip = accountChip(a);
        return (
          <button
            key={a.id}
            type="button"
            className="pg-a"
            aria-pressed={value === a.id}
            onClick={() => {
              onChange(a.id);
            }}
          >
            <span className="ch-ic is-sm" aria-hidden="true">
              {channelBadge(a.channel)}
            </span>
            <span className="grow ellip">{a.name}</span>
            {chip !== null && <span className={chip.cls}>{chip.text}</span>}
            <Unread n={unreadOf(a.id)} />
          </button>
        );
      })}
    </nav>
  );
}
