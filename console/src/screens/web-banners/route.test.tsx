// Màn Banner trang chủ tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/website/banner", "Banner trang chủ", "Website", "content.read", false, [], 10],
  ]);
});
