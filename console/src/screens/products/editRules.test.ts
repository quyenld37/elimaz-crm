// Luật của biểu mẫu sửa mẫu (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html): lưới màu × size, gợi ý
// SKU, đọc giá, kiểm ô, các bước lưu theo version.
import { describe, expect, it } from "vitest";
import type { Product, ProductClassification, Variant } from "../../api";
import { editBlocks, newBlock } from "./articleRules";
import {
  addOption,
  applyProduct,
  canRemoveOption,
  createBody,
  draftFromProduct,
  emptyDraft,
  parseMoney,
  removeOption,
  savePlan,
  setCode,
  setRow,
  suggestSku,
  validate,
  type Draft,
} from "./editRules";

const PID = "0195f3b2-0000-7000-8000-000000000001";

function variant(i: number, color: string, size: string, extra: Partial<Variant> = {}): Variant {
  return {
    id: `0195f3b2-0000-7000-8000-0000000000${String(i).padStart(2, "0")}`,
    productId: PID,
    sku: `DAM0123-${String(i)}`,
    barcode: `20000000000${String(i).padStart(2, "0")}`,
    color,
    size,
    price: 350000,
    costPrice: 180000,
    status: "active",
    version: 1,
    ...extra,
  };
}

function product(variants: Variant[], extra: Partial<Product> = {}): Product {
  return {
    id: PID,
    code: "DAM0123",
    name: "Đầm suông",
    description: "Mô tả",
    article: [],
    articleHtml: "",
    status: "active",
    version: 7,
    variants,
    images: [],
    ...extra,
  };
}

const CLS: ProductClassification = { productId: PID, version: 7, attributeValueIds: [], tagIds: [] };

function newDraft(): Draft {
  let d = setCode(emptyDraft(), "DAM0123");
  d = { ...d, name: "Đầm suông" };
  for (const c of ["Đen", "Be"]) d = addOption(d, "color", c);
  for (const s of ["S", "M", "L"]) d = addOption(d, "size", s);
  return d;
}

describe("suggestSku", () => {
  it("bỏ dấu, viết hoa, nối bằng gạch", () => {
    expect(suggestSku("dam0123", "Đen", "S")).toBe("DAM0123-DEN-S");
    expect(suggestSku("DAM0123", "Xanh rêu", "XL")).toBe("DAM0123-XANHREU-XL");
    expect(suggestSku("", "Be", "M")).toBe("BE-M");
  });
  it("không quá 64 ký tự", () => {
    expect(suggestSku("A".repeat(60), "Đỏ đô", "M").length).toBeLessThanOrEqual(64);
  });
});

describe("parseMoney", () => {
  it("nhận 350.000, 350000, 350 000, 0", () => {
    expect(parseMoney("350.000")).toBe(350000);
    expect(parseMoney(" 350000 ")).toBe(350000);
    expect(parseMoney("350 000")).toBe(350000);
    expect(parseMoney("1.250.000 ₫")).toBe(1250000);
    expect(parseMoney("0")).toBe(0);
  });
  it("từ chối chữ, số lẻ, nhóm sai, rỗng, quá lớn", () => {
    for (const s of ["", "abc", "350,5", "3.50", "-1", "1.2345", "99999999999999999"]) expect(parseMoney(s)).toBeNull();
  });
});

describe("lưới màu × size", () => {
  it("2 màu × 3 size → 6 dòng mới theo thứ tự màu rồi size, SKU gợi ý", () => {
    const d = newDraft();
    expect(d.rows.map((r) => `${r.color}·${r.size}`)).toEqual(["Đen·S", "Đen·M", "Đen·L", "Be·S", "Be·M", "Be·L"]);
    expect(d.rows[0]?.sku).toBe("DAM0123-DEN-S");
    expect(d.rows.every((r) => r.id === undefined)).toBe(true);
  });
  it("màu trùng (khác hoa thường, thừa khoảng trắng) hay rỗng thì bỏ qua", () => {
    const d = addOption(addOption(newDraft(), "color", " đen "), "color", "  ");
    expect(d.colors).toEqual(["Đen", "Be"]);
    expect(d.rows.length).toBe(6);
  });
  it("đổi mã mẫu → SKU gợi ý của dòng chưa sửa tay đổi theo; dòng đã sửa tay giữ nguyên", () => {
    let d = newDraft();
    d = setRow(d, d.rows[0]?.key ?? "", { sku: "TU-DAT" });
    d = setCode(d, "VAY01");
    expect(d.rows[0]?.sku).toBe("TU-DAT");
    expect(d.rows[1]?.sku).toBe("VAY01-DEN-M");
  });
  it("bỏ màu chỉ bỏ dòng chưa lưu; màu có biến thể đã lưu thì không bỏ được", () => {
    const base = draftFromProduct(product([variant(1, "Đen", "S")]), CLS);
    let d = addOption(base, "color", "Be");
    expect(d.rows.length).toBe(2);
    expect(canRemoveOption(d, "color", "Đen")).toBe(false);
    expect(canRemoveOption(d, "color", "Be")).toBe(true);
    expect(removeOption(d, "color", "Đen")).toBe(d);
    d = removeOption(d, "color", "Be");
    expect(d.colors).toEqual(["Đen"]);
    expect(d.rows.length).toBe(1);
  });
  it("mẫu đã lưu: màu, size theo thứ tự xuất hiện; dòng xếp theo màu rồi size; giá hiện 350.000", () => {
    const d = draftFromProduct(
      product([variant(1, "Be", "M"), variant(2, "Đen", "S"), variant(3, "Be", "S", { costPrice: undefined })]),
      CLS,
    );
    expect(d.colors).toEqual(["Be", "Đen"]);
    expect(d.sizes).toEqual(["M", "S"]);
    expect(d.rows.map((r) => `${r.color}·${r.size}`)).toEqual(["Be·M", "Be·S", "Đen·S"]);
    expect(d.rows[0]?.price).toBe("350.000");
    expect(d.rows[1]?.costPrice).toBe("");
  });
  it("version của bản nháp là version nhỏ hơn của mẫu và phân loại (đọc lệch thì lưu báo xung đột, không đè)", () => {
    expect(draftFromProduct(product([]), { ...CLS, version: 9 }).version).toBe(7);
    expect(draftFromProduct(product([], { version: 10 }), { ...CLS, version: 8 }).version).toBe(8);
  });
});

describe("validate", () => {
  it("bản đủ không lỗi", () => {
    let d = newDraft();
    for (const r of d.rows) d = setRow(d, r.key, { price: "350.000" });
    expect(validate(d, "create").ok).toBe(true);
  });
  it("thiếu mã, tên, giá; SKU sai dạng; SKU trùng trong bảng (không phân biệt hoa thường); barcode sai dạng", () => {
    let d = { ...newDraft(), code: "", name: " " };
    const [a, b, c] = d.rows.map((r) => r.key);
    d = setRow(d, a ?? "", { sku: "dup", price: "1.000" });
    d = setRow(d, b ?? "", { sku: "DUP", price: "abc", barcode: "x" });
    d = setRow(d, c ?? "", { sku: "có dấu", price: "1000" });
    const v = validate(d, "create");
    expect(v.ok).toBe(false);
    expect(v.fields.code).toBeDefined();
    expect(v.fields.name).toBeDefined();
    expect(v.rows[a ?? ""]?.sku).toBe("Trùng SKU với dòng khác");
    expect(v.rows[b ?? ""]?.sku).toBe("Trùng SKU với dòng khác");
    expect(v.rows[b ?? ""]?.price).toBe("Giá không hợp lệ");
    expect(v.rows[b ?? ""]?.barcode).toBeDefined();
    expect(v.rows[c ?? ""]?.sku).toBeDefined();
  });
  it("sửa mẫu: không kiểm mã mẫu (không đổi được)", () => {
    const d = draftFromProduct(product([variant(1, "Đen", "S")], { code: "" }), CLS);
    expect(validate(d, "edit").ok).toBe(true);
  });
  it("mô tả quá 1.000 ký tự, tên quá 200 ký tự", () => {
    const d = { ...newDraft(), name: "x".repeat(201), description: "y".repeat(1001) };
    const v = validate(d, "create");
    expect(v.fields.name).toBeDefined();
    expect(v.fields.description).toBeDefined();
  });
});

describe("createBody", () => {
  it("một yêu cầu kèm mọi biến thể; barcode, giá nhập trống thì không gửi", () => {
    let d = newDraft();
    for (const r of d.rows) d = setRow(d, r.key, { price: "350.000" });
    d = setRow(d, d.rows[0]?.key ?? "", { barcode: "2000000000015", costPrice: "180.000" });
    const b = createBody(d);
    expect(b.code).toBe("DAM0123");
    expect(b.status).toBe("draft");
    expect(b.variants.length).toBe(6);
    expect(b.variants[0]).toEqual({
      sku: "DAM0123-DEN-S",
      barcode: "2000000000015",
      color: "Đen",
      size: "S",
      price: 350000,
      costPrice: 180000,
    });
    expect(b.variants[1]).toEqual({ sku: "DAM0123-DEN-M", color: "Đen", size: "M", price: 350000 });
  });
});

describe("savePlan", () => {
  const base = draftFromProduct(product([variant(1, "Đen", "S"), variant(2, "Đen", "M", { version: 4 })]), {
    ...CLS,
    tagIds: ["t1"],
  });

  it("không đổi gì → không bước nào", () => {
    expect(savePlan(base, base)).toEqual([]);
  });

  it("thứ tự: mẫu → phân loại → thêm biến thể → sửa biến thể; chỉ gửi trường đã đổi", () => {
    let e = { ...base, name: "Tên mới", tagIds: ["t1", "t2"] };
    e = addOption(e, "size", "L");
    const m = e.rows.find((r) => r.size === "M")?.key ?? "";
    e = setRow(e, m, { price: "390.000", costPrice: "" });
    const l = e.rows.find((r) => r.size === "L")?.key ?? "";
    e = setRow(e, l, { price: "420.000" });
    const plan = savePlan(base, e);
    expect(plan.map((s) => s.kind)).toEqual(["product", "classification", "add", "variant"]);
    expect(plan[0]).toEqual({ kind: "product", body: { name: "Tên mới" } });
    expect(plan[1]).toEqual({
      kind: "classification",
      body: { categoryId: null, attributeValueIds: [], tagIds: ["t1", "t2"] },
    });
    expect(plan[2]).toMatchObject({ kind: "add", key: l, body: { sku: "DAM0123-DEN-L", color: "Đen", size: "L" } });
    expect(plan[3]).toEqual({
      kind: "variant",
      key: m,
      id: variant(2, "Đen", "M").id,
      body: { version: 4, price: 390000, costPrice: null },
    });
  });

  it("đổi trạng thái biến thể → Ngừng bán", () => {
    const k = base.rows[0]?.key ?? "";
    const plan = savePlan(base, setRow(base, k, { status: "discontinued" }));
    expect(plan).toEqual([
      { kind: "variant", key: k, id: variant(1, "Đen", "S").id, body: { version: 1, status: "discontinued" } },
    ]);
  });

  it("thứ tự nhãn khác nhau nhưng cùng tập → không đổi", () => {
    const b2 = { ...base, tagIds: ["a", "b"] };
    expect(savePlan(b2, { ...b2, tagIds: ["b", "a"] })).toEqual([]);
  });
});

describe("bài viết trong bản nháp (Task 326)", () => {
  const ART: Product["article"] = [
    { type: "heading", text: "Chất liệu", level: 3 },
    { type: "paragraph", text: "Lụa <b>Hàn</b>" },
  ];
  const base = draftFromProduct(product([variant(1, "Đen", "S")], { article: ART }), CLS);

  it("mở mẫu: khối đọc từ article; không đổi → không bước nào", () => {
    expect(base.article.map((b) => b.type)).toEqual(["heading", "paragraph"]);
    expect(savePlan(base, { ...base, article: editBlocks(ART) })).toEqual([]);
  });

  it("đổi thứ tự khối → bước sửa mẫu gửi cả bài", () => {
    const e = { ...base, article: [...base.article].reverse() };
    expect(savePlan(base, e)).toEqual([{ kind: "product", body: { article: [...ART].reverse() } }]);
  });

  it("lưu xong: bản đã lưu nhận bài của máy chủ → không còn thay đổi", () => {
    const e = { ...base, article: base.article.slice(1) };
    const pair = applyProduct({ base, edit: e }, product([], { article: ART.slice(1), version: 8 }));
    expect(savePlan(pair.base, pair.edit)).toEqual([]);
  });

  it("validate: khối sai → không ok, lỗi theo khóa khối; createBody gửi bài khi có khối", () => {
    const bad = { ...newBlock("list") };
    const v = validate({ ...newDraft(), article: [bad] }, "create");
    expect(v.ok).toBe(false);
    expect(v.article).toEqual({ [bad.key]: "Danh sách cần ít nhất một dòng." });
    expect(createBody(newDraft()).article).toBeUndefined();
    expect(createBody({ ...newDraft(), article: editBlocks(ART) }).article).toEqual(ART);
  });
});
