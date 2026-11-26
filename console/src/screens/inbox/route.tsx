// Màn Hộp chat (Inbox, Task 174; mockup docs/mockups/m4/inbox.html; HV01: nhóm "Hộp chat" đứng đầu menu). Cùng một màn
// cho danh sách và hội thoại đang mở: /inbox/:conversationId thêm cột hội thoại và khung bên phải (máy tính), hay thành
// màn riêng (điện thoại, HV02). Mọi API đọc của màn cần conversation.read; máy chủ lọc theo Page được giao.
import type { ScreenRoute } from "../../app/loadScreens";
import { InboxPage } from "./InboxPage";

export const screens: readonly ScreenRoute[] = [
  {
    path: "/inbox",
    label: "Hộp chat",
    group: "Hộp chat",
    permission: "conversation.read",
    order: 10,
    also: ["/inbox/:conversationId"],
    element: <InboxPage />,
  },
  {
    path: "/inbox/:conversationId",
    label: "Hội thoại",
    group: "Hộp chat",
    permission: "conversation.read",
    hidden: true,
    order: 900,
    element: <InboxPage />,
  },
];
