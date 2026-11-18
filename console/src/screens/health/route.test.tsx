// Màn Sức khỏe hệ thống tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình; quy
// ước chung ở src/app/screens.test.tsx).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { SCREENS } from "../../app/screens";
import { ERRORS_PATH } from "./HealthPage";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm, quyền, trang ẩn, also, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/suc-khoe", "Sức khỏe hệ thống", "Hệ thống", "platform.health.read", false, [], 10],
  ]);
});

it('nút "Xem đủ N loại" trỏ tới màn Sự kiện lỗi có thật, cùng quyền', () => {
  const target = SCREENS.find((s) => s.path === ERRORS_PATH);
  expect(target?.label).toBe("Sự kiện lỗi");
  expect(target?.permission).toBe("platform.health.read");
});
