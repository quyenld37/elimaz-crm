// Màn Vai trò (tự đăng ký, quy ước src/app/loadScreens.ts).
import type { ScreenRoute } from "../../app/loadScreens";
import { RoleEdit } from "./RoleEdit";
import { RoleList } from "./RoleList";

export const screens: readonly ScreenRoute[] = [
  // Task M1-56: Vai trò tự định nghĩa (mockup docs/mockups/m1/vai-tro.html).
  {
    path: "/vai-tro",
    also: ["/vai-tro/:id"],
    label: "Vai trò",
    group: "Quản trị",
    permission: "identity.roles.manage",
    order: 20,
    element: <RoleList />,
  },
  {
    path: "/vai-tro/:id",
    label: "Vai trò",
    group: "Quản trị",
    permission: "identity.roles.manage",
    hidden: true,
    order: 20,
    element: <RoleEdit />,
  },
];
