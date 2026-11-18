// @vitest-environment jsdom
// Màn Danh sách sản phẩm (kế hoạch M1 Task 50; mockup docs/mockups/m1/san-pham-danh-sach.html, trạng thái A–E):
// 0 / 1 / 200 mẫu, tên 80 ký tự, lọc, tìm không thấy, cột "Có thể bán" theo kho người xem có quyền, lỗi API.
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../../api";
import type { Grant, ListProductsData, Me, ProductListItem, ProductPage, Tag, Warehouse } from "../../api";
import { ProductList, TEXT_EMPTY_SHOP } from "./ProductList";
import { CATS, NAME_80, renderAt, VAY } from "./productsTestkit";

vi.mock("../../api", async (orig) => ({
  ...(await orig<typeof import("../../api")>()),
  listProducts: vi.fn(),
  listCategories: vi.fn(),
  listTags: vi.fn(),
  getMe: vi.fn(),
  listWarehouses: vi.fn(),
}));

const KHO = "0195f3b2-0000-7000-8000-00000000a001";
const KHO_PHU = "0195f3b2-0000-7000-8000-00000000a002";
const KHO_CU = "0195f3b2-0000-7000-8000-00000000a003";
const HANG_MOI = "0195f3b2-0000-7000-8000-00000000d001";

const TAGS: Tag[] = [{ id: HANG_MOI, code: "moi", name: "Hàng mới" }];

function product(i: number, extra: Partial<ProductListItem> = {}): ProductListItem {
  const n = String(i).padStart(4, "0");
  return {
    id: `0195f3b2-0000-7000-8000-00000001${n}`,
    code: `DAM${n}`,
    name: `Đầm mẫu ${n}`,
    status: "active",
    categoryPath: ["Váy", "Váy dài"],
    variantCount: 2,
    minPrice: 350000,
    maxPrice: 420000,
    updatedAt: "2026-11-18T08:00:00Z",
    ...extra,
  };
}

const ok = <T,>(data: T) =>
  Promise.resolve({ data, request: new Request("http://x"), response: new Response(null, { status: 200 }) });

type Query = NonNullable<ListProductsData["query"]>;
let pages: (q: Query) => ProductPage;
let calls: Query[];
/** Kho listWarehouses trả (máy chủ đã lọc theo inventory.read). */
let warehouses: Warehouse[];
let warehouseCalls: number;
/** Như Chủ, Quản lý: catalog.read và inventory.read toàn shop. */
const READ_ALL_STOCK: Grant[] = ["catalog.read", "inventory.read"].map((permission) => ({
  permission,
  scope: { type: "all", id: "" },
}));

function mockApi(grants: Grant[] = [{ permission: "catalog.read", scope: { type: "all", id: "" } }]) {
  calls = [];
  vi.mocked(api.listProducts).mockImplementation((opts) => {
    const q = opts?.query ?? {};
    calls.push(q);
    return ok(pages(q));
  });
  vi.mocked(api.listCategories).mockImplementation(() => ok(CATS));
  vi.mocked(api.listTags).mockImplementation(() => ok(TAGS));
  warehouseCalls = 0;
  vi.mocked(api.listWarehouses).mockImplementation(() => {
    warehouseCalls++;
    return ok(warehouses);
  });
  const me: Me = { userId: "u1", email: "lan@elimaz.vn", grants };
  vi.mocked(api.getMe).mockImplementation(() => ok(me));
  return me;
}

function renderList(me: Me, path = "/san-pham") {
  return renderAt(me, path, <ProductList />);
}

beforeEach(() => {
  pages = () => ({ items: [] });
  warehouses = [{ id: KHO, code: "MAIN", name: "Kho chính", status: "active" }];
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("ProductList", () => {
  it("0 mẫu, không lọc → Chưa có mẫu nào, nút Nhập Excel và Thêm mẫu", async () => {
    renderList(mockApi());
    expect(await screen.findByRole("heading", { name: TEXT_EMPTY_SHOP })).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.getAllByRole("link", { name: "Thêm mẫu" }).length).toBe(2);
    expect(screen.getAllByRole("link", { name: "Nhập Excel" }).at(0)?.getAttribute("href")).toBe("/nhap-excel");
  });

  it("đúng 1 mẫu → một dòng, Hết danh sách, liên kết sang trang sửa", async () => {
    pages = () => ({ items: [product(1, { minPrice: 390000, maxPrice: 390000 })] });
    renderList(mockApi());
    const row = (await screen.findByText("DAM0001")).closest("tr");
    expect(row).not.toBeNull();
    const r = within(row as HTMLElement);
    expect(r.getByText("Đầm mẫu 0001")).toBeTruthy();
    expect(r.getByText("Váy > Váy dài")).toBeTruthy();
    expect(r.getByText("390.000 ₫")).toBeTruthy();
    expect(r.getByText("Đang bán").className).toContain("chip-ok");
    expect(r.getByRole("link").getAttribute("href")).toBe(`/san-pham/${product(1).id}`);
    expect(screen.getByText("Hết danh sách.")).toBeTruthy();
    expect(calls[0]).toEqual({ limit: 50 });
  });

  it("200 mẫu: trang đầu 50 chỉ dựng phần đang thấy (danh sách ảo); Tải thêm gửi con trỏ", async () => {
    const all = Array.from({ length: 200 }, (_, i) => product(i + 1));
    pages = (q) => {
      const start = q.cursor === undefined ? 0 : Number(q.cursor);
      const end = start + 50;
      return { items: all.slice(start, end), ...(end < all.length ? { nextCursor: String(end) } : {}) };
    };
    renderList(mockApi());
    await screen.findByText("DAM0001");
    const rows = () => screen.getAllByRole("row").length - 1; // trừ dòng tiêu đề
    expect(rows()).toBeGreaterThan(0);
    expect(rows()).toBeLessThan(50);
    expect(screen.getByText("Đang hiện 50 mẫu đầu. Cuộn xuống để tải tiếp.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm 50 mẫu" }));
    await screen.findByText("Đang hiện 100 mẫu đầu. Cuộn xuống để tải tiếp.");
    expect(calls[1]).toEqual({ limit: 50, cursor: "50" });
  });

  it("tên 80 ký tự giữ nguyên chữ, ô tên cắt hai dòng (clamp2)", async () => {
    expect(NAME_80.length).toBe(80);
    pages = () => ({ items: [product(1, { name: NAME_80 })] });
    renderList(mockApi());
    const name = await screen.findByText(NAME_80);
    expect(name.className).toContain("clamp2");
  });

  it("ảnh bìa: chưa có ảnh, ảnh bị mất, ảnh sẵn có <picture>", async () => {
    pages = () => ({
      items: [
        product(1),
        product(2, { cover: { imageId: "i2", status: "lost" } }),
        product(3, {
          cover: { imageId: "i3", status: "ready", variants: [{ width: 160, format: "jpeg", url: "/f/a.jpeg" }] },
        }),
      ],
    });
    const { container } = renderList(mockApi());
    expect(await screen.findByText("Chưa có ảnh")).toBeTruthy();
    expect(screen.getByText("Ảnh bị mất")).toBeTruthy();
    const img = container.querySelector("picture img");
    expect(img?.getAttribute("src")).toBe("/f/a.jpeg");
    expect(img?.getAttribute("alt")).toBe("");
  });

  it("trạng thái: đang nhập (chip thường), ngừng bán (dòng mờ)", async () => {
    pages = () => ({ items: [product(1, { status: "draft" }), product(2, { status: "discontinued" })] });
    renderList(mockApi());
    expect(await screen.findByText("Đang nhập · ẩn với khách")).toBeTruthy();
    expect(screen.getByText("Ngừng bán", { selector: ".chip" }).closest("tr")?.className).toContain("is-muted");
  });

  it("không có inventory.read → không gọi listWarehouses (máy chủ trả 403), không ô chọn kho, không cột", async () => {
    pages = () => ({ items: [product(1)] });
    renderList(mockApi());
    await screen.findByText("DAM0001");
    expect(screen.queryByRole("columnheader", { name: "Có thể bán" })).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Kho tính có thể bán" })).toBeNull();
    expect(calls.at(0)?.warehouseId).toBeUndefined();
    expect(warehouseCalls).toBe(0);
  });

  // E-07 (review độc lập M1): Chủ, Quản lý, vai trò gán toàn shop có inventory.read toàn shop; máy chủ cho họ đọc mọi
  // kho (listWarehouses), nên màn phải có ô chọn kho và cột Có thể bán.
  it("inventory.read toàn shop → ô chọn các kho đang dùng API trả (tên kho), gửi warehouseId kho đầu một lần", async () => {
    warehouses = [
      { id: KHO, code: "MAIN", name: "Kho chính", status: "active" },
      { id: KHO_CU, code: "OLD", name: "Kho cũ", status: "closed" },
      { id: KHO_PHU, code: "SUB", name: "Kho phụ", status: "active" },
    ];
    pages = () => ({ items: [product(1, { available: 46 })] });
    renderList(mockApi(READ_ALL_STOCK));
    expect(await screen.findByRole("columnheader", { name: "Có thể bán" })).toBeTruthy();
    const box = screen.getByRole("combobox", { name: "Kho tính có thể bán" });
    expect(
      within(box)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Kho chính", "Kho phụ"]);
    // Chờ danh sách kho rồi mới gọi listProducts: không có lần gọi thừa không kèm kho.
    expect(calls).toEqual([{ limit: 50, warehouseId: KHO }]);
    fireEvent.change(box, { target: { value: KHO_PHU } });
    await waitFor(() => {
      expect(calls.at(-1)).toEqual({ limit: 50, warehouseId: KHO_PHU });
    });
  });

  it("có inventory.read nhưng API trả 0 kho → không ô chọn kho, không cột", async () => {
    warehouses = [];
    pages = () => ({ items: [product(1)] });
    renderList(mockApi(READ_ALL_STOCK));
    await screen.findByText("DAM0001");
    expect(screen.queryByRole("combobox", { name: "Kho tính có thể bán" })).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "Có thể bán" })).toBeNull();
    expect(calls.at(0)?.warehouseId).toBeUndefined();
  });

  it("inventory.read theo kho → gửi warehouseId; âm hiện đỏ kèm câu thiếu, 0 hiện Hết hàng", async () => {
    pages = () => ({
      items: [product(1, { available: -2 }), product(2, { available: 0 }), product(3, { available: 46 })],
    });
    renderList(
      mockApi([
        { permission: "catalog.read", scope: { type: "all", id: "" } },
        { permission: "inventory.read", scope: { type: "warehouse", id: KHO } },
      ]),
    );
    expect(await screen.findByText("Thiếu 2 so với phần giữ")).toBeTruthy();
    expect(screen.getByText("−2").className).toContain("err-text");
    expect(screen.getByText("Hết hàng")).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "Có thể bán" })).toBeTruthy();
    expect(screen.getByRole("option", { name: "Kho chính" })).toBeTruthy();
    expect(calls[0]).toEqual({ limit: 50, warehouseId: KHO });
  });

  it("lọc trạng thái, danh mục, nhãn → gửi đúng tham số, có chip bỏ lọc và Xóa bộ lọc", async () => {
    pages = () => ({ items: [product(1)] });
    renderList(mockApi());
    await screen.findByText("DAM0001");
    await screen.findByRole("option", { name: "Váy" });
    fireEvent.change(screen.getByRole("combobox", { name: "Trạng thái" }), { target: { value: "active" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Danh mục" }), { target: { value: VAY } });
    fireEvent.change(screen.getByRole("combobox", { name: "Nhãn" }), { target: { value: HANG_MOI } });
    await waitFor(() => {
      expect(calls.at(-1)).toEqual({ limit: 50, status: ["active"], categoryId: VAY, tagId: HANG_MOI });
    });
    expect(screen.getByText("Trạng thái: Đang bán")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Bỏ lọc trạng thái" }));
    await waitFor(() => {
      expect(calls.at(-1)).toEqual({ limit: 50, categoryId: VAY, tagId: HANG_MOI });
    });
    fireEvent.click(screen.getByRole("button", { name: "Xóa bộ lọc" }));
    await waitFor(() => {
      expect(calls.at(-1)).toEqual({ limit: 50 });
    });
    expect(screen.queryByRole("button", { name: "Xóa bộ lọc" })).toBeNull();
  });

  it("tìm chờ 300 ms rồi gửi; không thấy → câu của trạng thái E", async () => {
    pages = (q) => (q.q === undefined ? { items: [product(1)] } : { items: [] });
    renderList(mockApi(), `/san-pham?status=active&category=${VAY}`);
    await screen.findByText("DAM0001");
    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.change(screen.getByRole("searchbox", { name: "Tìm sản phẩm" }), { target: { value: "đầm nhung" } });
    expect(calls.some((c) => c.q !== undefined)).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(await screen.findByRole("heading", { name: 'Không có mẫu khớp "đầm nhung"' })).toBeTruthy();
    expect(screen.getByText("Đang lọc: Trạng thái Đang bán, danh mục Váy.")).toBeTruthy();
    expect(calls.at(-1)).toEqual({ limit: 50, q: "đầm nhung", status: ["active"], categoryId: VAY });
  });

  it("API lỗi → hộp báo bằng câu tiếng Việt", async () => {
    mockApi();
    vi.mocked(api.listProducts).mockImplementation(() =>
      Promise.resolve({
        data: undefined,
        error: { error: "forbidden", request_id: "r1" },
        request: new Request("http://x"),
        response: new Response(null, { status: 403 }),
      }),
    );
    renderList({ userId: "u1", email: "lan@elimaz.vn", grants: [] });
    expect((await screen.findByRole("alert")).textContent).toContain("Bạn chưa được cấp quyền làm việc này");
  });
});
