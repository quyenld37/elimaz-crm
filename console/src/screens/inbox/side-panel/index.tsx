// Khung bên phải của /inbox/:conversationId (Task 174 dựng khung, nạp lười; nội dung ở Task 176: thẻ khách, lên đơn,
// nhãn, người phụ trách, nhắc việc, gọi điện). Dưới 1200 px khung xuống dưới hội thoại (m4.css).
export interface SidePanelProps {
  conversationId: string;
}

export default function SidePanel({ conversationId }: SidePanelProps) {
  return (
    <div className="sec" data-conversation={conversationId}>
      <h3>Khách</h3>
      <p className="muted small">Thẻ khách và lên đơn đang được làm.</p>
    </div>
  );
}
