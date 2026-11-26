// Khung màn Kênh kết nối và Cài đặt hộp chat (Task 174 dựng đường, menu, nạp lười; nội dung ở Task 178, mockup
// docs/mockups/m4/ket-noi-page.html, cai-dat.html).
import { PendingScreen } from "../inbox/PendingScreen";

export interface SettingsScreenProps {
  title: string;
}

export default function SettingsScreen({ title }: SettingsScreenProps) {
  return <PendingScreen title={title} />;
}
