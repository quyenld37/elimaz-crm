// Chỗ chừa cho phần màn M4 chưa làm (Task 174 dựng đường, menu và khung; nội dung ở Task 177 (Live), 178 (Kênh kết nối, Cài đặt)). Chỉ có
// trên nhánh tích hợp m4; mỗi task màn hình thay chỗ này bằng màn thật.
export interface PendingScreenProps {
  title: string;
}

export function PendingScreen({ title }: PendingScreenProps) {
  return (
    <div>
      <div className="page-h">
        <div>
          <h1>{title}</h1>
        </div>
      </div>
      <div className="empty">
        <h2>Màn này đang được làm</h2>
        <p className="muted">Hộp chat đã dùng được; phần này sẽ có ở bản sau.</p>
      </div>
    </div>
  );
}
