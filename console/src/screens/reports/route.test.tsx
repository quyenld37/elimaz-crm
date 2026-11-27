// Màn Báo cáo tự đăng ký đúng đường, quyền, chỗ trên menu (Task 239, M6-16; Task 389: mỗi màn giữ test đăng ký của mình).
// Nhóm "Báo cáo" đứng giữa "Kho" và "Hệ thống" (quyết định thay chủ shop 27/11, ghi-chu.md).
import { describe, expect, it } from "vitest";
import type { Grant } from "../../api";
import { NAV_GROUPS, canSee, navSections } from "../../app/nav";
import { routeRows } from "../../app/routeTestkit";
import { screens } from "./route";

function grantsOf(...names: string[]): Grant[] {
  return names.map((permission) => ({ permission, scope: { type: "all", id: "shop" } }));
}

describe("route.tsx của màn Báo cáo", () => {
  it("khai đúng path, nhãn, nhóm Báo cáo, quyền, trang ẩn, also, order", () => {
    expect(routeRows(screens)).toEqual([
      ["/bao-cao/tong-quan", "Tổng quan", "Báo cáo", "report.sales.read", false, [], 10],
      ["/bao-cao/ton-kho", "Tồn và xuất nhập tồn", "Báo cáo", "report.inventory.read", false, [], 50],
      ["/bao-cao/tep-xuat", "Tệp xuất", "Báo cáo", "report.sales.read", false, [], 60],
    ]);
  });

  it("nhóm Báo cáo nằm giữa Kho và Hệ thống", () => {
    expect(NAV_GROUPS.indexOf("Báo cáo")).toBe(NAV_GROUPS.indexOf("Kho") + 1);
    expect(NAV_GROUPS.indexOf("Hệ thống")).toBe(NAV_GROUPS.indexOf("Báo cáo") + 1);
  });

  it("chỉ người có report.sales.read thấy mục Tổng quan trên menu", () => {
    const [overview, stock] = screens;
    if (overview === undefined || stock === undefined) throw new Error("thiếu màn");
    expect(canSee(grantsOf("report.sales.read"), overview)).toBe(true);
    expect(canSee(grantsOf("report.inventory.read"), overview)).toBe(false);
    expect(canSee(grantsOf("report.inventory.read"), stock)).toBe(true);
    expect(navSections(screens, grantsOf("report.sales.read")).map((s) => s.group)).toEqual(["Báo cáo"]);
    expect(navSections(screens, [])).toEqual([]);
  });
});
