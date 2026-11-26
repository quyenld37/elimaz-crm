// Khung hội thoại ở cột giữa của /inbox/:conversationId (Task 174 dựng khung, nạp lười; nội dung ở Task 175: tin, ô gõ,
// trạng thái gửi, thao tác comment, ai đang xem). Trên điện thoại hội thoại là màn riêng: nút "‹" về danh sách giữ bộ
// lọc (HV02).
import { Link } from "react-router";

export interface ConversationPaneProps {
  conversationId: string;
  /** Đường về danh sách (kèm bộ lọc). */
  backTo: string;
}

export default function ConversationPane({ conversationId, backTo }: ConversationPaneProps) {
  return (
    <div data-conversation={conversationId}>
      <div className="pane-h">
        <Link className="ibtn only-phone" to={backTo} aria-label="Về danh sách hội thoại">
          ‹
        </Link>
        <b>Hội thoại</b>
      </div>
      <div className="pane-b">
        <p className="muted">Khung hội thoại đang được làm.</p>
      </div>
    </div>
  );
}
