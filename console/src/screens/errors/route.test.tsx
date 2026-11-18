// Màn Sự kiện lỗi tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình; quy ước chung ở
// src/app/screens.test.tsx).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/he-thong/su-kien-loi", "Sự kiện lỗi", "Hệ thống", "platform.health.read", false, [], 20],
  ]);
});
