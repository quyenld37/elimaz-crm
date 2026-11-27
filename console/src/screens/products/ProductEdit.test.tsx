// @vitest-environment jsdom
// Màn Sửa mẫu (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, C): tạo mẫu 2 màu × 3 size,
// sửa theo version, SKU trùng → "Mã hàng đã có", người khác vừa lưu → "Mẫu đã thay đổi, tải lại.", chỉ đọc.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../../api";
import type { Attribute, Grant, Me, Product, ProductClassification, ProductImage, Tag, Variant } from "../../api";
import { ProductEdit } from "./ProductEdit";
import { CATS, fail, NAME_80, ok, renderAt, VAY } from "./productsTestkit";

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
  reorderProductImages: vi.fn(),
  reorderProductVariants: vi.fn(),
  deleteProductImage: vi.fn(),
  createUpload: vi.fn(),
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

/** Ảnh sẵn sàng thứ n của mẫu. */
const IMG = (n: number): ProductImage => ({
  id: `0195f3b2-0000-7000-8000-0000000002${String(n).padStart(2, "0")}`,
  fileId: `0195f3b2-0000-7000-8000-0000000003${String(n).padStart(2, "0")}`,
  position: n,
  status: "ready",
  variants: [{ width: 400, format: "jpeg", url: `https://anh.test/${String(n)}.jpg` }],
});

let product: Product;
let cls: ProductClassification;

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
/** Mở trang thêm mẫu, nhập mã, tên, một biến thể Đen · S giá 350000. */
const openNewOneVariant = async (code: string, name: string) => {
  renderEdit(mockApi(), "/san-pham/moi");
  await screen.findByRole("heading", { name: "Thêm mẫu" });
  typeIn("Mã mẫu *", code);
  typeIn("Tên mẫu *", name);
  addVia("Thêm màu", "Đen");
  addVia("Thêm size", "S");
  fireEvent.change(screen.getByRole("textbox", { name: "Giá bán của Đen · S" }), { target: { value: "350000" } });
};
/** Lưu mẫu mới (createProduct trả 201), chờ đúng một createProduct và trả thân của nó. */
const saveCreated = async () => {
  vi.mocked(api.createProduct).mockImplementation(() => ok({ ...product, version: 1 }, 201));
  clickSave();
  await waitFor(() => {
    expect(api.createProduct).toHaveBeenCalledTimes(1);
  });
  const body = vi.mocked(api.createProduct).mock.calls[0]?.[0].body;
  if (body === undefined) throw new Error("createProduct không có thân");
  return body;
};
/** Chờ đúng một updateProduct và trả thân của nó. */
const sentUpdate = async () => {
  await waitFor(() => {
    expect(api.updateProduct).toHaveBeenCalledTimes(1);
  });
  return vi.mocked(api.updateProduct).mock.calls[0]?.[0].body;
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
    expect(screen.queryByRole("button", { name: "Đưa Đen · M lên" })).toBeNull();
  });

  it("dời biến thể, thêm size → addVariant rồi reorderProductVariants (mọi biến thể theo bảng, version kế), rồi updateVariant (Task 513)", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    fireEvent.click(screen.getByRole("button", { name: "Đưa Đen · M lên" }));
    expect(screen.getAllByText("Có thay đổi chưa lưu").length).toBeGreaterThan(0);
    addVia("Thêm size", "L");
    fireEvent.change(screen.getByRole("textbox", { name: "Giá bán của Đen · L" }), { target: { value: "420.000" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Giá bán của Đen · S" }), { target: { value: "360.000" } });
    const l = variant(3, "Đen", "L", { price: 420000 });
    vi.mocked(api.addVariant).mockImplementation(() => ok(l, 201));
    vi.mocked(api.reorderProductVariants).mockImplementation(() =>
      ok({
        ...product,
        version: 7,
        variants: [variant(2, "Đen", "M", { version: 4 }), variant(1, "Đen", "S"), l],
      }),
    );
    vi.mocked(api.updateVariant).mockImplementation(() => ok(variant(1, "Đen", "S", { version: 2, price: 360000 })));
    clickSave();
    await waitFor(() => {
      expect(api.updateVariant).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.addVariant).mock.calls[0]?.[0].body.productVersion).toBe(5);
    const call = vi.mocked(api.reorderProductVariants).mock.calls[0]?.[0];
    expect(call?.path).toEqual({ id: PID });
    expect(call?.body).toEqual({
      productVersion: 6,
      variantIds: [variant(2, "x", "x").id, variant(1, "x", "x").id, l.id],
    });
    expect(call?.headers["Idempotency-Key"]).toBeTruthy();
    expect(vi.mocked(api.updateVariant).mock.calls[0]?.[0].body).toEqual({ version: 1, price: 360000 });
  });

  it("sắp thứ tự báo version_conflict → Mẫu đã thay đổi; không gửi bước nào khác", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    fireEvent.click(screen.getByRole("button", { name: "Đưa Đen · S xuống" }));
    vi.mocked(api.reorderProductVariants).mockImplementation(() => fail(409, "version_conflict"));
    clickSave();
    await waitFor(() => {
      expect(api.reorderProductVariants).toHaveBeenCalledTimes(1);
    });
    expect(await screen.findByText(/Mẫu đã thay đổi/)).toBeTruthy();
    expect(api.addVariant).not.toHaveBeenCalled();
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

describe("ProductEdit — khối Ảnh (Task 325)", () => {
  const UPLOAD_GRANTS: Grant[] = [...MANAGER, { permission: "files.upload", scope: { type: "all", id: "" } }];

  it("số ảnh ở đầu trang và trên khối theo ảnh hiện tại; xóa ảnh → số giảm", async () => {
    product = { ...product, images: [IMG(1), IMG(2)] };
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    expect(screen.getByText("2 biến thể · 2 ảnh")).toBeTruthy();
    expect(screen.getByText("2 ảnh")).toBeTruthy();
    vi.mocked(api.deleteProductImage).mockImplementation(() => ok({ ...product, images: [IMG(1)], version: 6 }));
    fireEvent.click(screen.getByRole("button", { name: "Xóa ảnh 2" }));
    expect(await screen.findByText("2 biến thể · 1 ảnh")).toBeTruthy();
  });

  it("thao tác ảnh tăng version → lần lưu mẫu sau gửi version mới (không báo xung đột oan)", async () => {
    product = { ...product, images: [IMG(1), IMG(2)] };
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    vi.mocked(api.reorderProductImages).mockImplementation(() =>
      ok({ ...product, images: [IMG(2), IMG(1)], version: 6 }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Dời ảnh 1 sang phải" }));
    await waitFor(() => {
      expect(api.reorderProductImages).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.reorderProductImages).mock.calls[0]?.[0].body.productVersion).toBe(5);
    typeIn("Tên mẫu *", "Đầm suông");
    vi.mocked(api.updateProduct).mockImplementation(() => ok({ ...product, name: "Đầm suông", version: 7 }));
    await waitFor(() => {
      expect(saveButtons()[0]?.hasAttribute("disabled")).toBe(false);
    });
    clickSave();
    await waitFor(() => {
      expect(api.updateProduct).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.updateProduct).mock.calls[0]?.[0].body).toEqual({ version: 6, name: "Đầm suông" });
  });

  it("xóa ảnh mà người khác vừa sửa mẫu (version nhảy) → hộp Mẫu đã thay đổi", async () => {
    product = { ...product, images: [IMG(1)] };
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    vi.mocked(api.deleteProductImage).mockImplementation(() => ok({ ...product, images: [], version: 8 }));
    fireEvent.click(screen.getByRole("button", { name: "Xóa ảnh 1" }));
    expect(await screen.findByText("Mẫu đã thay đổi, tải lại.")).toBeTruthy();
  });

  it("ảnh đang tải lên không mất khi lưu mẫu (biểu mẫu dựng lại sau khi lưu)", async () => {
    renderEdit(mockApi(UPLOAD_GRANTS), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    vi.mocked(api.createUpload).mockImplementation(() => new Promise<never>(() => undefined));
    const f = new File(["x"], "IMG_2040.jpg", { type: "image/jpeg" });
    fireEvent.change(screen.getByLabelText("Chọn ảnh từ máy"), { target: { files: [f] } });
    expect(await screen.findByText("IMG_2040.jpg")).toBeTruthy();
    typeIn("Tên mẫu *", "Đầm suông");
    vi.mocked(api.updateProduct).mockImplementation(() => ok({ ...product, name: "Đầm suông", version: 6 }));
    clickSave();
    await waitFor(() => {
      expect(vi.mocked(api.getProduct).mock.calls.length).toBeGreaterThan(1);
    });
    expect(await screen.findByText("IMG_2040.jpg")).toBeTruthy();
  });

  it("đang lưu mẫu → nút của khối Ảnh bị khóa", async () => {
    product = { ...product, images: [IMG(1), IMG(2)] };
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    typeIn("Tên mẫu *", "Đầm suông");
    vi.mocked(api.updateProduct).mockImplementation(() => new Promise<never>(() => undefined));
    clickSave();
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Xóa ảnh 1" }).matches(":disabled")).toBe(true);
    });
    expect(screen.getByRole("button", { name: "Dời ảnh 1 sang phải" }).matches(":disabled")).toBe(true);
    expect(screen.getByRole("combobox", { name: "Màu của ảnh 1" }).matches(":disabled")).toBe(true);
  });

  it("trang thêm mẫu chưa có khối Ảnh (chưa có mã mẫu để gắn)", async () => {
    renderEdit(mockApi(UPLOAD_GRANTS), "/san-pham/moi");
    await screen.findByRole("heading", { name: "Thêm mẫu" });
    expect(screen.queryByRole("heading", { name: "Ảnh" })).toBeNull();
  });
});

describe("ProductEdit — bài viết (Task 326)", () => {
  const blockEls = () => screen.getAllByTestId("article-block");

  it("sửa bài viết → updateProduct gửi article đúng thứ tự, chỉ khi bài khác bản đã lưu", async () => {
    product = {
      ...product,
      article: [
        { type: "heading", text: "Chất liệu", level: 2 },
        { type: "paragraph", text: "Lụa Hàn." },
      ],
    };
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    expect(screen.getByText("2 khối")).toBeTruthy();
    expect(screen.queryByText("Có thay đổi chưa lưu")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Đưa khối 2 lên" }));
    fireEvent.click(screen.getByRole("button", { name: "+ Danh sách" }));
    typeIn("Khối 3 · Danh sách", "Giặt tay\nKhông vắt");
    const sent: unknown[] = [];
    vi.mocked(api.updateProduct).mockImplementation((o) => {
      sent.push(o.body);
      return ok({ ...product, version: 6 });
    });
    clickSave();
    await waitFor(() => {
      expect(sent).toEqual([
        {
          version: 5,
          article: [
            { type: "paragraph", text: "Lụa Hàn." },
            { type: "heading", text: "Chất liệu", level: 2 },
            { type: "list", items: ["Giặt tay", "Không vắt"] },
          ],
        },
      ]);
    });
  });

  it("khối sai trước khi gửi → không gửi, hộp lỗi trạng thái C, khối tô đỏ", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    fireEvent.click(screen.getByRole("button", { name: "+ Đoạn văn" }));
    fireEvent.click(screen.getByRole("button", { name: "+ Danh sách" }));
    typeIn("Khối 1 · Đoạn văn", "Ổn");
    clickSave();
    expect(await screen.findByText("Chưa lưu: bài viết có 1 khối sai.")).toBeTruthy();
    expect(
      screen.getByText("Khối 2 (Danh sách): danh sách cần ít nhất một dòng. Sửa khối được tô đỏ rồi lưu lại."),
    ).toBeTruthy();
    expect(blockEls()[1]?.className).toContain("is-err");
    expect(api.updateProduct).not.toHaveBeenCalled();
    // Sửa khối → lỗi tắt.
    typeIn("Khối 2 · Danh sách", "Một");
    expect(screen.queryByText("Chưa lưu: bài viết có 1 khối sai.")).toBeNull();
  });

  it("máy chủ trả invalid_article (khối ảnh trỏ ảnh đã xóa) → lỗi hiện tại đúng khối đó", async () => {
    const gone = "0195f3b2-0000-7000-8000-00000000f0ff";
    product = {
      ...product,
      images: [IMG(1)],
      article: [
        { type: "paragraph", text: "Đoạn 1" },
        { type: "image", fileId: gone },
        { type: "paragraph", text: "Đoạn 3" },
      ],
    };
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    typeIn("Khối 3 · Đoạn văn", "Đoạn 3 sửa");
    vi.mocked(api.updateProduct).mockImplementation(() => fail(422, "invalid_article"));
    clickSave();
    expect(await screen.findByText("Chưa lưu: bài viết có 1 khối sai.")).toBeTruthy();
    expect(
      screen.getByText("Khối 2 (Ảnh): ảnh này không còn dùng được, chọn ảnh khác. Sửa khối được tô đỏ rồi lưu lại."),
    ).toBeTruthy();
    const second = blockEls()[1];
    if (second === undefined) throw new Error("thiếu khối 2");
    expect(second.className).toContain("is-err");
    expect(within(second).getByText("Ảnh này không còn dùng được, chọn ảnh khác.")).toBeTruthy();
    expect(blockEls().filter((b) => b.className.includes("is-err")).length).toBe(1);
    // Chọn ảnh khác → khối hết đỏ.
    fireEvent.change(screen.getByRole("combobox", { name: "Chọn từ ảnh của mẫu (khối 2)" }), {
      target: { value: IMG(1).fileId },
    });
    expect(blockEls()[1]?.className).not.toContain("is-err");
  });

  it("máy chủ trả invalid_article mà màn không tìm ra khối → câu chung", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    fireEvent.click(screen.getByRole("button", { name: "+ Đoạn văn" }));
    typeIn("Khối 1 · Đoạn văn", "Ổn");
    vi.mocked(api.updateProduct).mockImplementation(() => fail(422, "invalid_article"));
    clickSave();
    expect(await screen.findByText("Bài viết có khối không hợp lệ. Kiểm tra lại các khối được đánh dấu")).toBeTruthy();
  });

  it("tạo mẫu kèm bài viết → createProduct gửi article; trang thêm mẫu tắt + Ảnh", async () => {
    await openNewOneVariant("DAM0123", "Đầm");
    expect(screen.getByRole("button", { name: "+ Ảnh" }).matches(":disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "+ Tiêu đề" }));
    typeIn("Khối 1 · Tiêu đề", "Form dáng");
    expect((await saveCreated()).article).toEqual([{ type: "heading", text: "Form dáng", level: 2 }]);
  });
});

describe("ProductEdit — số đo (Task 516, 517)", () => {
  it("hiện số đo đã lưu (72,5); đổi Cao 3,5, xóa Dài → updateProduct gửi measureCm đúng các số còn lại", async () => {
    product = { ...product, measureCm: { length: 88, width: 72.5 } };
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    expect(valueOf(screen.getByRole("textbox", { name: "Dài (cm)" }))).toBe("88");
    expect(valueOf(screen.getByRole("textbox", { name: "Rộng (cm)" }))).toBe("72,5");
    expect(valueOf(screen.getByRole("textbox", { name: "Cao (cm)" }))).toBe("");
    typeIn("Dài (cm)", "");
    typeIn("Cao (cm)", "3,5");
    vi.mocked(api.updateProduct).mockImplementation(() =>
      ok({ ...product, measureCm: { width: 72.5, height: 3.5 }, version: 6 }),
    );
    clickSave();
    expect(await sentUpdate()).toEqual({
      version: 5,
      measureCm: { width: 72.5, height: 3.5 },
    });
  });

  it("số đo sai (0, 1001, hai số lẻ) → không gửi, câu dưới đúng ô", async () => {
    renderEdit(mockApi(), `/san-pham/${PID}`);
    await screen.findByRole("heading", { name: NAME_80 });
    typeIn("Dài (cm)", "0");
    typeIn("Rộng (cm)", "1001");
    typeIn("Cao (cm)", "2,55");
    clickSave();
    expect((await screen.findAllByText("Số cm từ 0,1 đến 1.000, tối đa một số lẻ")).length).toBe(3);
    expect(api.updateProduct).not.toHaveBeenCalled();
    expect(screen.getByRole("textbox", { name: "Dài (cm)" }).getAttribute("aria-invalid")).toBe("true");
  });

  it("tạo mẫu: nhập Dài, Rộng → createProduct gửi measureCm chỉ hai số đó", async () => {
    await openNewOneVariant("QUAN01", "Quần");
    typeIn("Dài (cm)", "88");
    typeIn("Rộng (cm)", "20");
    expect((await saveCreated()).measureCm).toEqual({ length: 88, width: 20 });
  });
});
