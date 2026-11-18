// @vitest-environment jsdom
// Màn Sửa mẫu (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, C): tạo mẫu 2 màu × 3 size,
// sửa theo version, SKU trùng → "Mã hàng đã có", người khác vừa lưu → "Mẫu đã thay đổi, tải lại.", chỉ đọc.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../../api";
import type { Attribute, Grant, Me, Product, ProductClassification, Tag, Variant } from "../../api";
import { ProductEdit } from "./ProductEdit";
import { CATS, NAME_80, renderAt, VAY } from "./productsTestkit";

vi.mock("../../api", async (orig) => ({
  ...(await orig<typeof import("../../api")>()),
  getProduct: vi.fn(),
  getProductClassification: vi.fn(),
  createProduct: vi.fn(),
  updateProduct: vi.fn(),
  addVariant: vi.fn(),
  updateVariant: vi.fn(),
  setProductClassification: vi.fn(),
  listCategories: vi.fn(),
  listTags: vi.fn(),
  listAttributes: vi.fn(),
  getMe: vi.fn(),
}));

const PID = "0195f3b2-0000-7000-8000-000000000001";
const MOI = "0195f3b2-0000-7000-8000-00000000d001";
const CONG_SO = "0195f3b2-0000-7000-8000-00000000d002";
const LUA = "0195f3b2-0000-7000-8000-00000000e011";

const TAGS: Tag[] = [
  { id: MOI, code: "moi", name: "Hàng mới" },
  { id: CONG_SO, code: "cong-so", name: "Công sở" },
];
const ATTRS: Attribute[] = [
  {
    id: "0195f3b2-0000-7000-8000-00000000e001",
    code: "chat-lieu",
    name: "Chất liệu",
    position: 1,
    values: [{ id: LUA, attributeId: "0195f3b2-0000-7000-8000-00000000e001", value: "Lụa", position: 1 }],
  },
];

const MANAGER: Grant[] = ["catalog.read", "catalog.manage"].map((permission) => ({
  permission,
  scope: { type: "all", id: "" },
}));

function variant(i: number, color: string, size: string, extra: Partial<Variant> = {}): Variant {
  return {
    id: `0195f3b2-0000-7000-8000-0000000001${String(i).padStart(2, "0")}`,
    productId: PID,
    sku: `DAM0123-${String(i)}`,
    barcode: `200000000001${String(i)}`,
    color,
    size,
    price: 350000,
    costPrice: 180000,
    status: "active",
    version: 1,
    ...extra,
  };
}

let product: Product;
let cls: ProductClassification;

const ok = <T,>(data: T, status = 200) =>
  Promise.resolve({ data, request: new Request("http://x"), response: new Response(null, { status }) });
const fail = (status: number, error: string) =>
  Promise.resolve({
    data: undefined,
    error: { error, request_id: "r1" },
    request: new Request("http://x"),
    response: new Response(null, { status }),
  });

function mockApi(grants: Grant[] = MANAGER): Me {
  vi.mocked(api.getProduct).mockImplementation(() => ok(product));
  vi.mocked(api.getProductClassification).mockImplementation(() => ok(cls));
  vi.mocked(api.listCategories).mockImplementation(() => ok(CATS));
  vi.mocked(api.listTags).mockImplementation(() => ok(TAGS));
  vi.mocked(api.listAttributes).mockImplementation(() => ok(ATTRS));
  const me: Me = { userId: "u1", email: "lan@elimaz.vn", grants };
  vi.mocked(api.getMe).mockImplementation(() => ok(me));
  return me;
}

function Where() {
  const loc = useLocation();
  return <p data-testid="where">{loc.pathname}</p>;
}

function renderEdit(me: Me, path: string) {
  return renderAt(
    me,
    path,
    <>
      <Routes>
        <Route path="/san-pham/:id" element={<ProductEdit />} />
      </Routes>
      <Where />
    </>,
  );
}

const typeIn = (name: string, value: string) => {
  fireEvent.change(screen.getByRole("textbox", { name }), { target: { value } });
};
const addVia = (label: string, value: string) => {
  typeIn(label, value);
  fireEvent.keyDown(screen.getByRole("textbox", { name: label }), { key: "Enter" });
};
const saveButtons = () => screen.getAllByRole("button", { name: "Lưu mẫu" });
const clickSave = () => {
  const b = saveButtons().at(0);
  if (b === undefined) throw new Error("không có nút Lưu mẫu");
  fireEvent.click(b);
};
/** Giá trị của ô nhập hay ô chọn. */
const valueOf = (el: HTMLElement) =>
  el instanceof HTMLInputElement || el instanceof HTMLSelectElement || el instanceof HTMLTextAreaElement
    ? el.value
    : null;

beforeEach(() => {
  product = {
    id: PID,
    code: "DAM0123",
    name: NAME_80,
    description: "Đầm suông dáng dài",
    article: [],
    articleHtml: "",
    status: "active",
    version: 5,
    variants: [variant(1, "Đen", "S"), variant(2, "Đen", "M", { version: 4 })],
    images: [],
  };
  cls = { productId: PID, version: 5, categoryId: VAY, attributeValueIds: [], tagIds: [MOI] };
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ProductEdit — tạo mẫu", () => {
  it("2 màu × 3 size → 6 dòng; Lưu gửi một createProduct có khóa chống trùng, rồi sang trang sửa", async () => {
    renderEdit(mockApi(), "/san-pham/moi");
    expect(await screen.findByRole("heading", { name: "Thêm mẫu" })).toBeTruthy();
    typeIn("Mã mẫu *", "DAM0123");
    typeIn("Tên mẫu *", NAME_80);
    expect(screen.getByText("80 ký tự")).toBeTruthy();
    for (const c of ["Đen", "Be"]) addVia("Thêm màu", c);
    for (const s of ["S", "M", "L"]) addVia("Thêm size", s);
    const rows = within(screen.getByRole("table")).getAllByRole("row").slice(1);
    expect(rows.length).toBe(6);
    for (const r of rows)
      fireEvent.change(within(r).getByRole("textbox", { name: /^Giá bán/ }), { target: { value: "350.000" } });

    const created: Product = { ...product, variants: [], version: 1, status: "draft" };
    vi.mocked(api.createProduct).mockImplementation(() => ok(created, 201));
    clickSave();
    await waitFor(() => {
      expect(screen.getByTestId("where").textContent).toBe(`/san-pham/${PID}`);
    });
    expect(api.createProduct).toHaveBeenCalledTimes(1);
    const call = vi.mocked(api.createProduct).mock.calls[0]?.[0];
    expect(call?.headers["Idempotency-Key"]).toMatch(/^[0-9a-f-]{36}$/);
    expect(call?.body.variants.length).toBe(6);
    expect(call?.body.variants[5]).toEqual({ sku: "DAM0123-BE-L", color: "Be", size: "L", price: 350000 });
    expect(call?.body.status).toBe("draft");
    expect(api.setProductClassification).not.toHaveBeenCalled();
  });

  it("ô trống, giá sai → không gửi, câu dưới ô", async () => {
    renderEdit(mockApi(), "/san-pham/moi");
    await screen.findByRole("heading", { name: "Thêm mẫu" });
    addVia("Thêm màu", "Đen");
    addVia("Thêm size", "S");
    fireEvent.change(screen.getByRole("textbox", { name: "Giá bán của Đen · S" }), { target: { value: "35,5" } });
    clickSave();
    expect(await screen.findByText("Nhập mã mẫu")).toBeTruthy();
    expect(screen.getByText("Nhập tên mẫu")).toBeTruthy();
    expect(screen.getByText("Giá không hợp lệ")).toBeTruthy();
    expect(api.createProduct).not.toHaveBeenCalled();
  });

  it("SKU đã có ở mẫu khác (sku_taken) → câu Mã hàng đã có, ở lại trang tạo", async () => {
    renderEdit(mockApi(), "/san-pham/moi");
    await screen.findByRole("heading", { name: "Thêm mẫu" });
    typeIn("Mã mẫu *", "AO0456");
    typeIn("Tên mẫu *", "Áo");
    addVia("Thêm màu", "Đen");
    addVia("Thêm size", "L");
    fireEvent.change(screen.getByRole("textbox", { name: "Giá bán của Đen · L" }), { target: { value: "420000" } });
    vi.mocked(api.createProduct).mockImplementation(() => fail(409, "sku_taken"));
    clickSave();
    expect(await screen.findByText(/^Mã hàng đã có/)).toBeTruthy();
    expect(screen.getByTestId("where").textContent).toBe("/san-pham/moi");
  });
});

describe("ProductEdit — sửa mẫu", () => {
  it("hiện mẫu: tên 80 ký tự, chip trạng thái, phân loại đang gán, bảng 2 dòng", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    expect(await screen.findByRole("heading", { name: NAME_80 })).toBeTruthy();
    expect(screen.getByText("Đang bán", { selector: ".chip" })).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Mã mẫu *" }).matches(":disabled")).toBe(true);
    expect(valueOf(await screen.findByRole("combobox", { name: "Danh mục" }))).toBe(VAY);
    expect(screen.getByText("Hàng mới")).toBeTruthy();
    expect(within(screen.getByRole("table")).getAllByRole("row").length - 1).toBe(2);
    expect(screen.queryByText("Có thay đổi chưa lưu")).toBeNull();
  });

  it("đổi tên, giá, thêm nhãn, thêm size → updateProduct, setProductClassification, addVariant, updateVariant theo version", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    typeIn("Tên mẫu *", "Đầm suông");
    fireEvent.change(screen.getByRole("textbox", { name: "Giá bán của Đen · M" }), { target: { value: "390.000" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Thêm nhãn" }), { target: { value: CONG_SO } });
    addVia("Thêm size", "L");
    fireEvent.change(screen.getByRole("textbox", { name: "Giá bán của Đen · L" }), { target: { value: "420.000" } });
    expect(screen.getAllByText("Có thay đổi chưa lưu").length).toBeGreaterThan(0);

    vi.mocked(api.updateProduct).mockImplementation((o) => ok({ ...product, name: o.body.name ?? "", version: 6 }));
    vi.mocked(api.setProductClassification).mockImplementation((o) =>
      ok({ ...cls, version: 7, tagIds: o.body.tagIds }),
    );
    vi.mocked(api.addVariant).mockImplementation(() => ok(variant(3, "Đen", "L", { price: 420000 }), 201));
    vi.mocked(api.updateVariant).mockImplementation(() => ok(variant(2, "Đen", "M", { version: 5, price: 390000 })));
    clickSave();
    await waitFor(() => {
      expect(api.updateVariant).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.updateProduct).mock.calls[0]?.[0].body).toEqual({ version: 5, name: "Đầm suông" });
    expect(vi.mocked(api.setProductClassification).mock.calls[0]?.[0].body).toEqual({
      version: 6,
      categoryId: VAY,
      attributeValueIds: [],
      tagIds: [MOI, CONG_SO],
    });
    expect(vi.mocked(api.addVariant).mock.calls[0]?.[0].body).toMatchObject({
      productVersion: 7,
      sku: "DAM0123-DEN-L",
      price: 420000,
    });
    expect(vi.mocked(api.updateVariant).mock.calls[0]?.[0]).toMatchObject({
      path: { id: variant(2, "Đen", "M").id },
      body: { version: 4, price: 390000 },
    });
    // Lưu xong đọc lại mẫu.
    await waitFor(() => {
      expect(vi.mocked(api.getProduct).mock.calls.length).toBeGreaterThan(1);
    });
  });

  it("SKU trùng khi sửa biến thể → Mã hàng đã có ở đúng dòng; bước trước đã lưu không gửi lại", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    typeIn("Tên mẫu *", "Đầm suông");
    fireEvent.change(screen.getByRole("textbox", { name: "SKU của Đen · M" }), { target: { value: "AO0456-DEN-L" } });
    vi.mocked(api.updateProduct).mockImplementation(() => ok({ ...product, name: "Đầm suông", version: 6 }));
    vi.mocked(api.updateVariant).mockImplementation(() => fail(409, "sku_taken"));
    clickSave();
    const err = await screen.findByText("Mã hàng đã có");
    expect(err.closest("tr")?.textContent).toContain("Đen · M");
    // Lưu lại: chỉ còn bước sửa biến thể.
    vi.mocked(api.updateVariant).mockImplementation(() =>
      ok(variant(2, "Đen", "M", { version: 5, sku: "DAM0123-DEN-M" })),
    );
    fireEvent.change(screen.getByRole("textbox", { name: "SKU của Đen · M" }), { target: { value: "DAM0123-DEN-M" } });
    clickSave();
    await waitFor(() => {
      expect(api.updateVariant).toHaveBeenCalledTimes(2);
    });
    expect(api.updateProduct).toHaveBeenCalledTimes(1);
  });

  it("người khác vừa lưu (version_conflict) → Mẫu đã thay đổi, tải lại; Tải lại mẫu bỏ bản nháp, đọc bản mới", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    typeIn("Tên mẫu *", "Tên của tôi");
    vi.mocked(api.updateProduct).mockImplementation(() => fail(409, "version_conflict"));
    clickSave();
    expect(await screen.findByText("Mẫu đã thay đổi, tải lại.")).toBeTruthy();
    expect(screen.getByText("Có người vừa lưu mẫu này. Thay đổi của bạn chưa được lưu.")).toBeTruthy();
    product = { ...product, name: "Tên người khác vừa lưu", version: 6 };
    cls = { ...cls, version: 6 };
    fireEvent.click(screen.getByRole("button", { name: "Tải lại mẫu" }));
    expect(await screen.findByRole("heading", { name: "Tên người khác vừa lưu" })).toBeTruthy();
    expect(valueOf(screen.getByRole("textbox", { name: "Tên mẫu *" }))).toBe("Tên người khác vừa lưu");
    expect(screen.queryByText("Mẫu đã thay đổi, tải lại.")).toBeNull();
  });

  it("lưu xong mà đọc lại lỗi → giữ giá trị vừa lưu, báo chưa tải lại được (không hiện lại bản cũ)", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    typeIn("Tên mẫu *", "Đầm suông");
    vi.mocked(api.updateProduct).mockImplementation(() => ok({ ...product, name: "Đầm suông", version: 6 }));
    vi.mocked(api.getProduct).mockImplementation(() => fail(503, "internal"));
    clickSave();
    expect(await screen.findByText(/^Đã lưu, nhưng chưa tải lại được mẫu/)).toBeTruthy();
    expect(valueOf(screen.getByRole("textbox", { name: "Tên mẫu *" }))).toBe("Đầm suông");
    expect(screen.getByRole("heading", { name: "Đầm suông" })).toBeTruthy();
    expect(screen.queryByText("Có thay đổi chưa lưu")).toBeNull();
  });

  it("Hủy thay đổi đưa bản nháp về bản đã lưu", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    typeIn("Tên mẫu *", "Khác");
    fireEvent.click(screen.getByRole("button", { name: "Hủy thay đổi" }));
    expect(valueOf(screen.getByRole("textbox", { name: "Tên mẫu *" }))).toBe(NAME_80);
  });

  it("chỉ có catalog.read → chỉ xem, không nút Lưu", async () => {
    renderEdit(mockApi([{ permission: "catalog.read", scope: { type: "all", id: "" } }]), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    expect(screen.queryByRole("button", { name: "Lưu mẫu" })).toBeNull();
    expect(screen.getByRole("textbox", { name: "Tên mẫu *" }).matches(":disabled")).toBe(true);
  });

  it("mẫu không có (404) → câu không tìm thấy; mã không phải uuid → không gọi API", async () => {
    vi.mocked(api.getProduct).mockImplementation(() => fail(404, "not_found"));
    const me = mockApi();
    vi.mocked(api.getProduct).mockImplementation(() => fail(404, "not_found"));
    renderEdit(me, `/san-pham/${PID}`);
    expect(await screen.findByText("Không tìm thấy, có thể đã bị xóa")).toBeTruthy();
    cleanup();
    vi.mocked(api.getProduct).mockClear();
    renderEdit(me, "/san-pham/abc");
    expect(await screen.findByText(/Không có mẫu này/)).toBeTruthy();
    expect(api.getProduct).not.toHaveBeenCalled();
  });
});
