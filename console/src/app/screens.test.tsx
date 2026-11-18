// @vitest-environment jsdom
// Màn tự đăng ký (Task 393, M1-x2): mỗi màn là thư mục src/screens/<tên>/ có route.tsx xuất `screens`; khung tự nạp
// bằng import.meta.glob, không sửa tệp chung nào. Tệp màn sai quy ước → lỗi khi nạp, nêu tên tệp.
import { cleanup, render, screen, within } from "@testing-library/react";
import { randomUUID } from "node:crypto";
import { copyFile, mkdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { createElement } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import type { Grant } from "../api";
import { Layout } from "../shell/Layout";
import { loadScreens } from "./loadScreens";
import type { Screen } from "./nav";
import { AppRoutes } from "./router";
import { SCREENS } from "./screens";

afterEach(() => {
  cleanup();
});

const el = createElement("h1", null, "màn");
const ok = { path: "/a", label: "A", group: "Kho", permission: "inventory.read", order: 10, element: el };

describe("SCREENS nạp từ src/screens/*/route.tsx", () => {
  it("đủ các màn đã có, đúng path, nhãn, nhóm, quyền, trang con và thứ tự menu như danh sách cũ", () => {
    expect(SCREENS.map((s) => [s.path, s.label, s.group, s.permission, s.hidden === true, s.also ?? []])).toEqual([
      ["/san-pham", "Danh sách sản phẩm", "Sản phẩm", "catalog.read", false, ["/san-pham/:id"]],
      ["/san-pham/:id", "Sửa mẫu", "Sản phẩm", "catalog.read", true, []],
      ["/kho/ton", "Tồn kho", "Kho", "inventory.read", false, []],
      ["/suc-khoe", "Sức khỏe hệ thống", "Hệ thống", "platform.health.read", false, []],
      ["/nhan-vien", "Nhân viên", "Quản trị", "identity.users.manage", false, ["/nhan-vien/:id/quyen"]],
      ["/nhan-vien/:id/quyen", "Nhân viên", "Quản trị", "identity.users.manage", true, []],
      ["/vai-tro", "Vai trò", "Quản trị", "identity.roles.manage", false, ["/vai-tro/:id"]],
      ["/vai-tro/:id", "Vai trò", "Quản trị", "identity.roles.manage", true, []],
    ]);
  });
});

describe("loadScreens kiểm quy ước", () => {
  it("xếp theo nhóm menu (NAV_GROUPS) rồi order, không theo tên thư mục", () => {
    const got = loadScreens({
      "../screens/a/route.tsx": { screens: [{ ...ok, path: "/qt", group: "Quản trị", order: 1 }] },
      "../screens/b/route.tsx": { screens: [{ ...ok, path: "/k2", order: 20 }] },
      "../screens/c/route.tsx": { screens: [{ ...ok, path: "/k1", order: 10 }] },
      "../screens/d/route.tsx": { screens: [{ ...ok, path: "/sp", group: "Sản phẩm", order: 99 }] },
    });
    expect(got.map((s) => s.path)).toEqual(["/sp", "/k1", "/k2", "/qt"]);
    expect(got[0]).not.toHaveProperty("order");
  });

  it.each([
    ["không xuất screens", {}, /x\/route\.tsx.*screens/],
    ["screens rỗng", { screens: [] }, /x\/route\.tsx.*screens/],
    ["thiếu path", { screens: [{ ...ok, path: undefined }] }, /x\/route\.tsx.*path/],
    ["path không bắt đầu bằng /", { screens: [{ ...ok, path: "a" }] }, /x\/route\.tsx.*path/],
    ["thiếu nhãn", { screens: [{ ...ok, label: "" }] }, /x\/route\.tsx.*label/],
    ["nhóm menu lạ", { screens: [{ ...ok, group: "Khác" }] }, /x\/route\.tsx.*group/],
    ["thiếu quyền", { screens: [{ ...ok, permission: "" }] }, /x\/route\.tsx.*permission/],
    ["thiếu thứ tự", { screens: [{ ...ok, order: undefined }] }, /x\/route\.tsx.*order/],
    ["element không phải phần tử React", { screens: [{ ...ok, element: "chữ" }] }, /x\/route\.tsx.*element/],
    ["also không phải mảng chuỗi", { screens: [{ ...ok, also: "/b" }] }, /x\/route\.tsx.*also/],
    ["hidden không phải true/false", { screens: [{ ...ok, hidden: "có" }] }, /x\/route\.tsx.*hidden/],
  ])("%s → lỗi nêu tên tệp và trường", (_name, mod, msg) => {
    expect(() => loadScreens({ "../screens/x/route.tsx": mod })).toThrow(msg);
  });

  it("trùng path giữa hai tệp → lỗi nêu path và cả hai tệp", () => {
    expect(() =>
      loadScreens({ "../screens/a/route.tsx": { screens: [ok] }, "../screens/b/route.tsx": { screens: [ok] } }),
    ).toThrow(/"\/a".*a\/route\.tsx.*b\/route\.tsx/);
  });
});

// Thêm một màn thử đúng quy ước rồi nạp lại bằng bản sao y nguyên src/app/screens.ts (cùng mẫu glob): màn có trong
// menu và điều hướng được, không sửa tệp chung nào. Chạy trong một thư mục tạm riêng dưới src/ (không phải src/screens
// thật) để không lẫn vào lần nạp của test khác hay dev server đang chạy.
describe("thêm thư mục màn mới, không sửa tệp chung", () => {
  const src = join(import.meta.dirname, "..");

  async function probe(routes: Record<string, string>): Promise<string> {
    const dir = join(src, `probe-${randomUUID()}`);
    await mkdir(join(dir, "app"), { recursive: true });
    await copyFile(join(src, "app", "screens.ts"), join(dir, "app", "screens.ts"));
    await writeFile(join(dir, "app", "loadScreens.ts"), `export * from "../../app/loadScreens";\n`);
    await writeFile(join(dir, "app", "nav.ts"), `export * from "../../app/nav";\n`);
    for (const [name, code] of Object.entries(routes)) {
      await mkdir(join(dir, "screens", name), { recursive: true });
      await writeFile(join(dir, "screens", name, "route.tsx"), code);
    }
    return dir;
  }

  const thu = (path: string) => `import type { ScreenRoute } from "../../app/loadScreens";
export const screens: readonly ScreenRoute[] = [
  { path: "${path}", label: "Màn thử", group: "Kho", permission: "thu.read", order: 50, element: <h1>Nội dung màn thử</h1> },
];
`;

  it("src/screens/_thu/route.tsx → có mục menu và route", async () => {
    const dir = await probe({ _thu: thu("/thu") });
    try {
      const mod = (await import(/* @vite-ignore */ join(dir, "app", "screens.ts"))) as { SCREENS: readonly Screen[] };
      expect(mod.SCREENS.map((s) => s.path)).toEqual(["/thu"]);
      const grants: Grant[] = [{ permission: "thu.read", scope: { type: "all", id: "" } }];
      render(
        <MemoryRouter initialEntries={["/thu"]}>
          <Layout screens={mod.SCREENS} grants={grants} email="a@b.vn" banner={null}>
            <AppRoutes screens={mod.SCREENS} grants={grants} />
          </Layout>
        </MemoryRouter>,
      );
      const nav = screen.getByRole("navigation", { name: "Menu chính" });
      expect(within(nav).getByRole("link", { name: "Màn thử" }).getAttribute("aria-current")).toBe("page");
      expect(screen.getByRole("heading", { name: "Nội dung màn thử" })).toBeTruthy();
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });

  it("hai thư mục màn trùng path → nạp báo lỗi nêu cả hai tệp", async () => {
    const dir = await probe({ _thu: thu("/thu"), _thu2: thu("/thu") });
    try {
      await expect(import(/* @vite-ignore */ join(dir, "app", "screens.ts"))).rejects.toThrow(
        /"\/thu".*_thu\/route\.tsx.*_thu2\/route\.tsx/,
      );
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });
});
