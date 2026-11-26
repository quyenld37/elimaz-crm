// @vitest-environment jsdom
// Màn tự đăng ký (Task 393, M1-x2): mỗi màn là thư mục src/screens/<tên>/ có route.tsx xuất `screens`; khung tự nạp
// bằng import.meta.glob, không sửa tệp chung nào. Tệp màn sai quy ước → lỗi khi nạp, nêu tên tệp.
import { cleanup, render, screen, within } from "@testing-library/react";
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { copyFile, mkdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { createElement } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import type { Grant } from "../api";
import { Layout } from "../shell/Layout";
import { loadScreens, type ScreenRoute } from "./loadScreens";
import { NAV_GROUPS, allowedScreens, canSee, navSections, type Screen } from "./nav";
import { AppRoutes } from "./router";
import { SCREENS } from "./screens";

afterEach(() => {
  cleanup();
});

const el = createElement("h1", null, "màn");
const ok = { path: "/a", label: "A", group: "Kho", permission: "inventory.read", order: 10, element: el };

// Test chung chỉ kiểm quy ước trên mọi thư mục màn (Task 389, M1-x3): không có danh sách màn viết cứng nào ở đây.
// "Màn X có đường Y, quyền Z" nằm trong src/screens/<tên>/route.test.tsx của từng màn, nên thêm màn không sửa tệp này.
const ROUTE_FILES: Record<string, unknown> = import.meta.glob("../screens/*/route.tsx", { eager: true });

/** Khóa quyền trong bảng chuẩn docs/quyen.md (cột đầu của các dòng bảng: | `khóa` | …). */
function permissionTable(): Set<string> {
  const doc = readFileSync(join(import.meta.dirname, "..", "..", "..", "docs", "quyen.md"), "utf8");
  return new Set([...doc.matchAll(/^\| `([a-z0-9_.]+)` \|/gm)].map((m) => m[1] ?? ""));
}
const PERMISSIONS = permissionTable();

/**
 * Lỗi quy ước của các mô-đun route.tsx (khóa là đường tệp, như import.meta.glob). Đủ trường, nhóm menu hợp lệ, không
 * trùng path: loadScreens kiểm (ném lỗi). Thêm ở đây: quyền phải có trong bảng quyền; order không trùng giữa các mục
 * menu hiện (không hidden) trong cùng nhóm.
 */
function conventionProblems(modules: Record<string, unknown>, permissions: ReadonlySet<string>): string[] {
  loadScreens(modules);
  const problems: string[] = [];
  const slot = new Map<string, string>();
  for (const file of Object.keys(modules).sort()) {
    for (const s of (modules[file] as { screens: readonly ScreenRoute[] }).screens) {
      for (const p of [s.permission, ...(s.alsoRequires ?? [])]) {
        if (!permissions.has(p)) problems.push(`${file}: "${s.path}" khai quyền "${p}" không có trong docs/quyen.md`);
      }
      if (s.hidden === true) continue;
      const key = `${s.group}#${String(s.order)}`;
      const other = slot.get(key);
      if (other !== undefined) {
        problems.push(`nhóm "${s.group}" trùng order ${String(s.order)}: ${other} và ${file} "${s.path}"`);
      }
      slot.set(key, `${file} "${s.path}"`);
    }
  }
  return problems;
}

describe("quy ước trên mọi src/screens/*/route.tsx", () => {
  it("tìm thấy các thư mục màn, và SCREENS có đúng mọi màn chúng khai, không thêm không bớt", () => {
    const files = Object.keys(ROUTE_FILES);
    expect(files.length).toBeGreaterThan(0);
    const declared = files.flatMap((f) => (ROUTE_FILES[f] as { screens: readonly ScreenRoute[] }).screens);
    expect(SCREENS.map((s) => s.path).sort()).toEqual(declared.map((s) => s.path).sort());
  });

  it("bảng quyền docs/quyen.md đọc được", () => {
    expect(PERMISSIONS.has("catalog.read")).toBe(true);
    expect(PERMISSIONS.has("identity.users.manage")).toBe(true);
  });

  it("đủ trường, nhóm hợp lệ, không trùng path, quyền thuộc bảng quyền, order không trùng trong nhóm", () => {
    expect(conventionProblems(ROUTE_FILES, PERMISSIONS)).toEqual([]);
  });

  it("NAV_GROUPS: Hộp chat đứng đầu (M4 HV01, Task 174); Website giữa Khuyến mãi và Sản phẩm (M5, Task 208)", () => {
    expect(NAV_GROUPS).toEqual([
      "Hộp chat",
      "Đơn hàng",
      "Vận chuyển",
      "Tiền",
      "Khách hàng",
      "Khuyến mãi",
      "Website",
      "Sản phẩm",
      "Kho",
      "Hệ thống",
      "Quản trị",
    ]);
  });

  it("nhóm Website, Quản trị xếp mục đúng mục 'Xếp module' của kế hoạch M5 (HV01; Task 462)", () => {
    // Thứ tự mục do kế hoạch chốt (không phải danh sách màn): các màn tự khai order trong route.tsx của mình.
    const all: Grant[] = SCREENS.flatMap((s) => [s.permission, ...(s.alsoRequires ?? [])]).map((permission) => ({
      permission,
      scope: { type: "all", id: "" },
    }));
    const labels = (g: string) =>
      navSections(SCREENS, all)
        .find((x) => x.group === g)
        ?.items.map((s) => s.label);
    expect(labels("Website")).toEqual(["Banner trang chủ", "Bộ phối", "Hãng trên web", "Nhận đơn web", "Khóa dịch vụ"]);
    expect(labels("Quản trị")?.at(-1)).toBe("Nhật ký thao tác");
  });

  it("SCREENS xếp theo nhóm menu (NAV_GROUPS) rồi order", () => {
    const rank = (g: string) => NAV_GROUPS.findIndex((x) => x === g);
    const ranks = SCREENS.map((s) => rank(s.group));
    expect(ranks).toEqual([...ranks].sort((a, b) => a - b));
  });
});

describe("conventionProblems bắt màn sai quy ước", () => {
  const perms = new Set(["inventory.read", "catalog.read"]);
  const mod = (...screens: object[]) => ({ screens: screens.map((s) => ({ ...ok, ...s })) });

  it("màn đúng quy ước → không lỗi", () => {
    expect(conventionProblems({ "../screens/a/route.tsx": mod({}) }, perms)).toEqual([]);
  });

  it("quyền không có trong bảng quyền → lỗi nêu tệp, path và quyền", () => {
    expect(conventionProblems({ "../screens/a/route.tsx": mod({ permission: "kho.xem" }) }, perms)).toEqual([
      '../screens/a/route.tsx: "/a" khai quyền "kho.xem" không có trong docs/quyen.md',
    ]);
  });

  it("quyền đi kèm (alsoRequires) không có trong bảng quyền → lỗi như quyền chính", () => {
    expect(
      conventionProblems({ "../screens/a/route.tsx": mod({ alsoRequires: ["catalog.read", "kho.dem"] }) }, perms),
    ).toEqual(['../screens/a/route.tsx: "/a" khai quyền "kho.dem" không có trong docs/quyen.md']);
  });

  it("hai mục menu hiện cùng nhóm trùng order → lỗi nêu cả hai tệp", () => {
    const got = conventionProblems(
      { "../screens/a/route.tsx": mod({}), "../screens/b/route.tsx": mod({ path: "/b" }) },
      perms,
    );
    expect(got).toEqual(['nhóm "Kho" trùng order 10: ../screens/a/route.tsx "/a" và ../screens/b/route.tsx "/b"']);
  });

  it("trang ẩn hay khác nhóm được dùng lại order", () => {
    const got = conventionProblems(
      {
        "../screens/a/route.tsx": mod({}, { path: "/a/:id", hidden: true }),
        "../screens/b/route.tsx": mod({ path: "/b", group: "Sản phẩm", permission: "catalog.read" }),
      },
      perms,
    );
    expect(got).toEqual([]);
  });

  it("thiếu trường hay trùng path → lỗi của loadScreens", () => {
    expect(() => conventionProblems({ "../screens/x/route.tsx": mod({ order: undefined }) }, perms)).toThrow(
      /x\/route\.tsx.*order/,
    );
    expect(() =>
      conventionProblems({ "../screens/a/route.tsx": mod({}), "../screens/b/route.tsx": mod({}) }, perms),
    ).toThrow(/"\/a".*a\/route\.tsx.*b\/route\.tsx/);
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
    ["alsoRequires không phải mảng chuỗi", { screens: [{ ...ok, alsoRequires: "catalog.read" }] }, /alsoRequires/],
    ["alsoRequires có chuỗi rỗng", { screens: [{ ...ok, alsoRequires: [""] }] }, /x\/route\.tsx.*alsoRequires/],
    ["badge không phải phần tử React", { screens: [{ ...ok, badge: 12 }] }, /x\/route\.tsx.*badge/],
  ])("%s → lỗi nêu tên tệp và trường", (_name, mod, msg) => {
    expect(() => loadScreens({ "../screens/x/route.tsx": mod })).toThrow(msg);
  });

  it("badge là phần tử React thì giữ nguyên trên màn (số đếm trên menu, HV02 M3)", () => {
    const badge = createElement("span", null, "3");
    const got = loadScreens({ "../screens/x/route.tsx": { screens: [{ ...ok, badge }] } });
    expect(got[0]?.badge).toBe(badge);
  });

  it("trùng path giữa hai tệp → lỗi nêu path và cả hai tệp", () => {
    expect(() =>
      loadScreens({ "../screens/a/route.tsx": { screens: [ok] }, "../screens/b/route.tsx": { screens: [ok] } }),
    ).toThrow(/"\/a".*a\/route\.tsx.*b\/route\.tsx/);
  });
});

// Màn cần thêm quyền đi kèm (alsoRequires, E-02 review độc lập M1): API của màn đòi quyền khác quyền chính. Thiếu một
// quyền đi kèm thì như thiếu quyền chính: không có mục menu, route hiện "không được xem".
describe("quyền đi kèm của màn (alsoRequires)", () => {
  const all = (...perms: string[]): Grant[] =>
    perms.map((permission) => ({ permission, scope: { type: "all", id: "" } }));
  const dem = { ...ok, path: "/dem", label: "Đếm", alsoRequires: ["inventory.read", "catalog.read"], order: 20 };
  const screens = loadScreens({ "../screens/a/route.tsx": { screens: [ok, dem] } });

  it("canSee: đủ quyền chính và mọi quyền đi kèm (phạm vi bất kỳ)", () => {
    const d = screens.find((s) => s.path === "/dem");
    if (d === undefined) throw new Error("thiếu màn");
    expect(d.alsoRequires).toEqual(["inventory.read", "catalog.read"]);
    expect(canSee(all("inventory.read", "catalog.read"), d)).toBe(true);
    expect(canSee(all("inventory.read"), d)).toBe(false);
    expect(canSee(all("catalog.read"), d)).toBe(false);
  });

  it("thiếu quyền đi kèm: không có trong menu, route báo không được xem", () => {
    const grants = all("inventory.read");
    expect(allowedScreens(screens, grants).map((s) => s.path)).toEqual(["/a"]);
    render(
      <MemoryRouter initialEntries={["/dem"]}>
        <AppRoutes screens={screens} grants={grants} />
      </MemoryRouter>,
    );
    expect(screen.getByText(/Tài khoản của bạn không được xem Đếm/)).toBeTruthy();
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

  const thu = (path: string, permission = "thu.read") => `import type { ScreenRoute } from "../../app/loadScreens";
export const screens: readonly ScreenRoute[] = [
  { path: "${path}", label: "Màn thử", group: "Kho", permission: "${permission}", order: 50, element: <h1>Nội dung màn thử</h1> },
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

  it("màn thử được test quy ước chung kiểm luôn: quyền lạ bị bắt, quyền có trong bảng thì qua", async () => {
    const dir = await probe({ _thu: thu("/thu", "thu.read"), _kho: thu("/thu-kho", "inventory.read") });
    try {
      const load = async (name: string) =>
        (await import(/* @vite-ignore */ join(dir, "screens", name, "route.tsx"))) as unknown;
      expect(conventionProblems({ "_thu/route.tsx": await load("_thu") }, PERMISSIONS)).toEqual([
        '_thu/route.tsx: "/thu" khai quyền "thu.read" không có trong docs/quyen.md',
      ]);
      expect(conventionProblems({ "_kho/route.tsx": await load("_kho") }, PERMISSIONS)).toEqual([]);
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
