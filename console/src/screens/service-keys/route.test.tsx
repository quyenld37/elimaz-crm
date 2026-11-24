// Màn Khóa dịch vụ tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test đăng ký của mình).
import { expect, it } from "vitest";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

it("route.tsx khai đúng path, nhãn, nhóm Website, quyền, order", () => {
  expect(routeRows(screens)).toEqual([
    ["/website/khoa-dich-vu", "Khóa dịch vụ", "Website", "identity.service_keys.manage", false, [], 50],
  ]);
});
