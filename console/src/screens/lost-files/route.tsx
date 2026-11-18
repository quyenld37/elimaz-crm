// Màn Tệp bị mất (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { LostFilesPage } from "./LostFilesPage";

export const screens: readonly ScreenRoute[] = [
  // Task 331: Tệp bị mất (mockup docs/mockups/m1/tep-bi-mat.html); thứ ba trong nhóm Hệ thống (sau Sức khỏe, Sự kiện lỗi).
  {
    path: "/he-thong/tep-bi-mat",
    label: "Tệp bị mất",
    group: "Hệ thống",
    permission: "platform.files.manage",
    order: 30,
    element: <LostFilesPage />,
  },
];
