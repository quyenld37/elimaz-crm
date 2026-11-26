// Khung màn Live và "Đã chốt, thiếu địa chỉ" (Task 174 dựng đường, menu, nạp lười; nội dung ở Task 177, mockup
// docs/mockups/m4/live.html, thieu-dia-chi.html).
import { PendingScreen } from "../inbox/PendingScreen";

export interface LiveScreenProps {
  title: string;
}

export default function LiveScreen({ title }: LiveScreenProps) {
  return <PendingScreen title={title} />;
}
