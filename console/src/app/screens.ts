// Danh sách màn của CRM, tự nạp từ src/screens/<tên>/route.tsx (quy ước: src/app/loadScreens.ts). Thêm màn mới là
// thêm một thư mục màn; không sửa tệp này hay tệp chung nào khác. Khung, menu và định tuyến tự có.
import { loadScreens } from "./loadScreens";
import type { Screen } from "./nav";

export const SCREENS: readonly Screen[] = loadScreens(import.meta.glob("../screens/*/route.tsx", { eager: true }));
