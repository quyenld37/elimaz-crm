// Màn Khóa dịch vụ (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M5 Task 208 (M5-25); mockup
// docs/mockups/m5/service-keys.html, menu nhóm "Website" (HV01, HV13 của docs/mockups/m5). Chỉ người có quyền
// identity.service_keys.manage (vai trò Chủ) thấy mục; mọi API của màn đòi đúng quyền đó.
import type { ScreenRoute } from "../../app/loadScreens";
import { ServiceKeys } from "./ServiceKeys";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/website/khoa-dich-vu",
    label: "Khóa dịch vụ",
    group: "Website",
    permission: "identity.service_keys.manage",
    order: 50,
    element: <ServiceKeys />,
  },
];
