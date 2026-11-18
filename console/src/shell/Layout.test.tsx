// @vitest-environment jsdom
// Khung điều hướng theo mockup M1 (docs/mockups/m1/khung.js): menu trái theo bốn nhóm, chỉ mục có quyền; thanh trên
// và menu trượt trên điện thoại; trang không có hoặc không có quyền.
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import type { Grant } from "../api";
import type { Screen } from "../app/nav";
import { AppRoutes, TEXT_EMPTY_HOME, TEXT_NOT_FOUND } from "../app/router";
import { Layout } from "./Layout";

afterEach(() => {
  cleanup();
});

const all = { type: "all" as const, id: "" };
const grant = (permission: string, scope: Grant["scope"] = all): Grant => ({ permission, scope });

const screens: Screen[] = [
  {
    path: "/san-pham",
    also: ["/san-pham/:id"],
    label: "Danh sách sản phẩm",
    group: "Sản phẩm",
    permission: "catalog.read",
    element: <h1>Màn sản phẩm</h1>,
  },
  {
    path: "/san-pham/:id",
    label: "Sửa mẫu",
    group: "Sản phẩm",
    permission: "catalog.read",
    hidden: true,
    element: <h1>Màn sửa</h1>,
  },
  { path: "/kho/ton", label: "Tồn kho", group: "Kho", permission: "inventory.read", element: <h1>Màn tồn</h1> },
  {
    path: "/nhan-vien",
    label: "Nhân viên",
    group: "Quản trị",
    permission: "identity.users.manage",
    element: <h1>Màn nhân viên</h1>,
  },
  {
    path: "/suc-khoe",
    label: "Sức khỏe hệ thống",
    group: "Hệ thống",
    permission: "platform.health.read",
    element: <h1>Màn sức khỏe</h1>,
  },
];

function renderAt(path: string, grants: Grant[], list: Screen[] = screens) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Layout screens={list} grants={grants} email="lan@elimaz.vn" banner={<p>băng</p>}>
        <AppRoutes screens={list} grants={grants} />
      </Layout>
    </MemoryRouter>,
  );
}

const nav = () => screen.getByRole("navigation", { name: "Menu chính" });

describe("menu trái", () => {
  it("nhóm theo thứ tự mockup; chỉ mục có quyền; mục ẩn không lên menu", () => {
    renderAt("/san-pham", [grant("catalog.read"), grant("platform.health.read"), grant("identity.users.manage")]);
    const groups = within(nav())
      .getAllByRole("group")
      .map((g) => g.getAttribute("aria-label"));
    expect(groups).toEqual(["Sản phẩm", "Hệ thống", "Quản trị"]);
    const links = within(nav())
      .getAllByRole("link")
      .map((a) => a.textContent);
    expect(links).toEqual(["Danh sách sản phẩm", "Sức khỏe hệ thống", "Nhân viên"]);
  });

  it("quyền theo kho (phạm vi warehouse) vẫn hiện mục Kho", () => {
    renderAt("/kho/ton", [grant("inventory.read", { type: "warehouse", id: "w1" })]);
    expect(within(nav()).getByRole("link", { name: "Tồn kho" })).toBeTruthy();
  });

  it("không có quyền nào → menu rỗng; vào trang chủ thấy câu trống", () => {
    renderAt("/", []);
    expect(within(nav()).queryAllByRole("link")).toHaveLength(0);
    expect(screen.getByText(TEXT_EMPTY_HOME)).toBeTruthy();
  });

  it("mục đang mở có aria-current, kể cả trang con (sửa mẫu → Danh sách sản phẩm)", () => {
    renderAt("/san-pham/abc", [grant("catalog.read")]);
    expect(within(nav()).getByRole("link", { name: "Danh sách sản phẩm" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("heading", { name: "Màn sửa" })).toBeTruthy();
  });

  it("trang chủ chuyển tới màn đầu tiên có quyền", () => {
    renderAt("/", [grant("inventory.read", { type: "warehouse", id: "w1" })]);
    expect(screen.getByRole("heading", { name: "Màn tồn" })).toBeTruthy();
  });

  it("trang không có hoặc không có quyền → câu không tìm thấy", () => {
    renderAt("/nhan-vien", [grant("catalog.read")]);
    expect(screen.getByText(TEXT_NOT_FOUND)).toBeTruthy();
    cleanup();
    renderAt("/khong-co", [grant("catalog.read")]);
    expect(screen.getByText(TEXT_NOT_FOUND)).toBeTruthy();
  });

  it("ô người dùng có email; băng báo nằm trên nội dung", () => {
    renderAt("/san-pham", [grant("catalog.read")]);
    expect(screen.getAllByText("lan@elimaz.vn").length).toBeGreaterThan(0);
    const main = screen.getByRole("main");
    expect(main.firstElementChild?.textContent).toBe("băng");
  });
});

describe("thanh trên và menu trượt (điện thoại)", () => {
  it("thanh trên có tên màn đang mở", () => {
    renderAt("/kho/ton", [grant("inventory.read")]);
    expect(screen.getByRole("banner").textContent).toContain("Tồn kho");
  });

  it("☰ mở menu, Escape đóng và trả focus về nút", () => {
    renderAt("/san-pham", [grant("catalog.read")]);
    const btn = screen.getByRole("button", { name: "Mở menu" });
    expect(btn.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(btn);
    expect(btn.getAttribute("aria-expanded")).toBe("true");
    expect(document.querySelector(".app")?.classList.contains("nav-open")).toBe(true);
    expect(screen.getByRole("main").hasAttribute("inert")).toBe(true);
    expect(document.activeElement?.closest("nav")).toBe(nav());
    fireEvent.keyDown(document, { key: "Escape" });
    expect(btn.getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByRole("main").hasAttribute("inert")).toBe(false);
    expect(document.activeElement).toBe(btn);
  });

  it("bấm một mục trong menu → đi tới màn đó và đóng menu", () => {
    renderAt("/san-pham", [grant("catalog.read"), grant("inventory.read")]);
    fireEvent.click(screen.getByRole("button", { name: "Mở menu" }));
    act(() => {
      fireEvent.click(within(nav()).getByRole("link", { name: "Tồn kho" }));
    });
    expect(screen.getByRole("heading", { name: "Màn tồn" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Mở menu" }).getAttribute("aria-expanded")).toBe("false");
  });

  it("bấm nền mờ → đóng menu", () => {
    renderAt("/san-pham", [grant("catalog.read")]);
    fireEvent.click(screen.getByRole("button", { name: "Mở menu" }));
    fireEvent.click(screen.getByRole("button", { name: "Đóng menu" }));
    expect(screen.getByRole("button", { name: "Mở menu" }).getAttribute("aria-expanded")).toBe("false");
  });

  it("nút tài khoản có chữ đầu của email và mở menu (ô người dùng ở cuối menu)", () => {
    renderAt("/san-pham", [grant("catalog.read")]);
    const acc = screen.getByRole("button", { name: "Tài khoản lan@elimaz.vn" });
    expect(acc.textContent).toBe("L");
    fireEvent.click(acc);
    expect(screen.getByRole("button", { name: "Mở menu" }).getAttribute("aria-expanded")).toBe("true");
  });
});
