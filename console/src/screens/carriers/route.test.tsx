// Màn Kết nối vận chuyển, Cài đặt vận chuyển tự đăng ký đúng đường, quyền, chỗ trên menu (Task 389: mỗi màn giữ test
// đăng ký của mình).
import { describe, expect, it } from "vitest";
import type { Grant } from "../../api";
import { canSee } from "../../app/nav";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

const perms = (...names: string[]): Grant[] =>
  names.map((permission) => ({ permission, scope: { type: "all", id: "" } }));

describe("route.tsx của màn Kết nối vận chuyển", () => {
  it("khai đúng path, nhãn, nhóm Vận chuyển, quyền, trang ẩn, also, order", () => {
    const rows = routeRows(screens);
    expect(rows.map((r) => r[0])).toEqual(["/settings/carriers", "/settings/carriers/:id", "/settings/fulfillment"]);
    expect(rows).toEqual([
      [
        "/settings/carriers",
        "Kết nối vận chuyển",
        "Vận chuyển",
        "fulfillment.carrier_accounts.read",
        false,
        ["/settings/carriers/:id"],
        50,
      ],
      ["/settings/carriers/:id", "Tài khoản hãng", "Vận chuyển", "fulfillment.carrier_accounts.read", true, [], 51],
      ["/settings/fulfillment", "Cài đặt vận chuyển", "Vận chuyển", "fulfillment.settings.manage", false, [], 60],
    ]);
  });

  it("Kết nối cần quyền đọc tài khoản hãng và inventory.read; Cài đặt cần settings.manage và fulfillment.read", () => {
    const [list, , settings] = screens;
    if (list === undefined || settings === undefined) throw new Error("thiếu màn");
    expect(canSee(perms("fulfillment.carrier_accounts.read"), list)).toBe(false);
    expect(canSee(perms("fulfillment.carrier_accounts.read", "inventory.read"), list)).toBe(true);
    expect(canSee(perms("fulfillment.settings.manage"), settings)).toBe(false);
    expect(canSee(perms("fulfillment.settings.manage", "fulfillment.read"), settings)).toBe(true);
  });
});
