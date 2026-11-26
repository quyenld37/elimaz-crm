// Dải "ai đang xem" ngay dưới đầu hội thoại (Task 175; mockup hoi-thoai.html trạng thái A, B; CH10; HV23). Đọc
// getPresence; báo hiệu conversation.presence của đúng hội thoại tải lại; thêm tự tải mỗi 15 giây vì máy chủ không báo
// khi một người hết hạn (30 giây không có nhịp tim). Không hiện chính mình.
import { useQuery } from "@tanstack/react-query";
import { getPresence, type PresenceState } from "../../../api";
import { unwrap } from "../../../app/errors";
import type { RealtimeMeta } from "../../../app/queryClient";
import { presenceKey } from "./messageWrites";

const VERB: Readonly<Record<PresenceState, string>> = {
  viewing: "đang xem",
  typing: "đang trả lời…",
  drafting_order: "đang lên đơn",
};

const POLL_MS = 15_000;

export function PresenceBar({ conversationId, meId }: { conversationId: string; meId: string }) {
  const meta: RealtimeMeta = { realtimeEntity: conversationId };
  const q = useQuery({
    queryKey: presenceKey(conversationId),
    queryFn: () => unwrap(getPresence({ path: { id: conversationId } })),
    meta,
    refetchInterval: POLL_MS,
  });
  const others = (q.data?.items ?? []).filter((p) => p.userId !== meId);
  if (others.length === 0) return null;
  return (
    <div className="presence" role="status">
      {others.map((p) => (
        <span key={p.userId} className="row presence-i">
          <span className="dot dot-ok" aria-hidden="true" />
          <span>{`${p.displayName} ${VERB[p.state]}`}</span>
        </span>
      ))}
    </div>
  );
}
