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
      ["/bao-cao/don", "Báo cáo đơn", "Báo cáo", "report.sales.read", false, [], 20],
      ["/bao-cao/van-chuyen", "Hãng vận chuyển", "Báo cáo", "report.sales.read", false, [], 30],
      ["/bao-cao/ban-chay", "Bán chạy", "Báo cáo", "report.sales.read", false, [], 40],
      ["/bao-cao/ton-kho", "Tồn và xuất nhập tồn", "Báo cáo", "report.inventory.read", false, [], 50],
      ["/bao-cao/tep-xuat", "Tệp xuất", "Báo cáo", "report.sales.read", false, [], 60],
    ]);
  });

  it("Tệp xuất: vai kho (chỉ report.inventory.read) cũng thấy, vai không có quyền báo cáo thì không (Task 519)", () => {
    const files = screens.find((s) => s.path === "/bao-cao/tep-xuat");
    if (files === undefined) throw new Error("thiếu màn");
    expect(files.orPermissions).toEqual(["report.inventory.read"]);
    expect(canSee(grantsOf("report.inventory.read"), files)).toBe(true);
    expect(canSee(grantsOf("report.sales.read"), files)).toBe(true);
    expect(canSee(grantsOf("customer.export"), files)).toBe(false);
    expect(navSections(screens, grantsOf("report.inventory.read")).flatMap((g) => g.items.map((i) => i.label))).toEqual(
      ["Tồn và xuất nhập tồn", "Tệp xuất"],
    );
  });

  it("nhóm Báo cáo nằm giữa Kho và Hệ thống", () => {
    expect(NAV_GROUPS.indexOf("Báo cáo")).toBe(NAV_GROUPS.indexOf("Kho") + 1);
    expect(NAV_GROUPS.indexOf("Hệ thống")).toBe(NAV_GROUPS.indexOf("Báo cáo") + 1);
  });

  it("chỉ người có report.sales.read thấy mục Tổng quan trên menu", () => {
    const overview = screens.find((s) => s.path === "/bao-cao/tong-quan");
    const stock = screens.find((s) => s.path === "/bao-cao/ton-kho");
    if (overview === undefined || stock === undefined) throw new Error("thiếu màn");
    for (const path of ["/bao-cao/don", "/bao-cao/van-chuyen", "/bao-cao/ban-chay"]) {
      const s = screens.find((x) => x.path === path);
      if (s === undefined) throw new Error(`thiếu màn ${path}`);
      expect(canSee(grantsOf("report.sales.read"), s)).toBe(true);
      expect(canSee(grantsOf("report.inventory.read"), s)).toBe(false);
    }
    expect(canSee(grantsOf("report.sales.read"), overview)).toBe(true);
    expect(canSee(grantsOf("report.inventory.read"), overview)).toBe(false);
    expect(canSee(grantsOf("report.inventory.read"), stock)).toBe(true);
    expect(navSections(screens, grantsOf("report.sales.read")).map((s) => s.group)).toEqual(["Báo cáo"]);
    expect(navSections(screens, [])).toEqual([]);
  });
});
