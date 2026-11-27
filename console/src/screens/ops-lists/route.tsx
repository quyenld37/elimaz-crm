// Danh sách vận hành (tự đăng ký, quy ước src/app/loadScreens.ts). Kế hoạch M3 Task 135 (M3-44); mockup
// docs/mockups/m3/van-hanh.html (màn 4); nhóm menu "Vận chuyển" (HV01), số đếm cạnh mục (HV02).
import type { ScreenRoute } from "../../app/loadScreens";
import { AgedHoldsPage } from "./AgedHoldsPage";
import { CallbacksPage } from "./CallbacksPage";
import { OpsNavCount } from "./OpsNavCount";
import { ReviewPage } from "./ReviewPage";
import { StuckPage } from "./StuckPage";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/ops/callbacks",
    label: "Cần gọi lại",
    group: "Vận chuyển",
    permission: "fulfillment.callbacks.work",
    order: 40,
    badge: <OpsNavCount which="callbacks" />,
    element: <CallbacksPage />,
  },
  {
    path: "/ops/stuck",
    label: "Đơn kẹt",
    group: "Vận chuyển",
    permission: "fulfillment.read",
    order: 41,
    badge: <OpsNavCount which="stuck" />,
    element: <StuckPage />,
  },
  {
    path: "/ops/review",
    label: "Cần xem",
    group: "Vận chuyển",
    permission: "fulfillment.read",
    order: 42,
    badge: <OpsNavCount which="review" />,
    element: <ReviewPage />,
  },
  {
    path: "/ops/aged-holds",
    label: "Giữ hàng quá hạn",
    group: "Vận chuyển",
    permission: "inventory.aged.read",
    order: 43,
    element: <AgedHoldsPage />,
  },
];
