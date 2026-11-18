// Luật hiển thị của màn Danh sách sản phẩm (kế hoạch M1 Task 50; mockup docs/mockups/m1/san-pham-danh-sach.html).
import { describe, expect, it } from "vitest";
import type { Category, Grant, ProductListItem } from "../../api";
import {
  availableView,
  categoryOptions,
  coverPicture,
  DEFAULT_FILTERS,
  filterSummary,
  formatMoney,
  hasFilters,
  priceText,
  readFilters,
  warehouseOptions,
  writeFilters,
} from "./productRules";

const item = (extra: Partial<ProductListItem> = {}): ProductListItem => ({
  id: "0195f3b2-0000-7000-8000-0000000000p1",
  code: "DAM0123",
  name: "Đầm suông",
  status: "active",
  categoryPath: [],
  variantCount: 3,
  updatedAt: "2026-11-18T08:00:00Z",
  ...extra,
});

const UUID_A = "0195f3b2-0000-7000-8000-00000000a001";
const UUID_B = "0195f3b2-0000-7000-8000-00000000a002";

describe("formatMoney, priceText", () => {
  it("định dạng đồng theo kiểu Việt Nam, không làm tròn", () => {
    expect(formatMoney(350000)).toBe("350.000 ₫");
    expect(formatMoney(0)).toBe("0 ₫");
    expect(formatMoney(1234567891)).toBe("1.234.567.891 ₫");
  });
  it("một giá hay khoảng giá; chưa có biến thể → gạch", () => {
    expect(priceText(item({ minPrice: 289000, maxPrice: 289000 }))).toBe("289.000 ₫");
    expect(priceText(item({ minPrice: 350000, maxPrice: 420000 }))).toBe("350.000 ₫ – 420.000 ₫");
    expect(priceText(item({ variantCount: 0 }))).toBe("—");
  });
});

describe("availableView", () => {
  it("không gửi kho → không có", () => {
    expect(availableView(item())).toBeNull();
  });
  it("âm → đỏ kèm câu thiếu so với phần giữ", () => {
    expect(availableView(item({ available: -2 }))).toEqual({
      text: "−2",
      tone: "short",
      note: "Thiếu 2 so với phần giữ",
    });
  });
  it("0 của mẫu đang bán → Hết hàng; mẫu đang nhập chỉ số 0", () => {
    expect(availableView(item({ available: 0 }))).toEqual({ text: "0", tone: "zero", note: "Hết hàng" });
    expect(availableView(item({ available: 0, status: "draft" }))).toEqual({ text: "0", tone: "normal" });
  });
  it("số dương có dấu chấm nghìn", () => {
    expect(availableView(item({ available: 1200 }))).toEqual({ text: "1.200", tone: "normal" });
  });
});

describe("categoryOptions", () => {
  const cat = (
    id: string,
    name: string,
    position: number,
    parentId?: string,
    status: Category["status"] = "active",
  ): Category => ({
    id,
    name,
    slug: id,
    depth: parentId === undefined ? 0 : 1,
    position,
    status,
    version: 1,
    ...(parentId === undefined ? {} : { parentId }),
  });
  it("theo cây, con ngay sau cha, nhãn từ gốc tới lá; bỏ danh mục lưu trữ", () => {
    const list = [
      cat("c2", "Áo", 2),
      cat("c1", "Váy", 1),
      cat("c11", "Váy dài", 2, "c1"),
      cat("c12", "Chân váy", 1, "c1"),
      cat("c21", "Áo sơ mi", 1, "c2"),
      cat("c3", "Cũ", 3, undefined, "archived"),
      cat("c31", "Con của cũ", 1, "c3"),
    ];
    expect(categoryOptions(list)).toEqual([
      { id: "c1", label: "Váy" },
      { id: "c12", label: "Váy > Chân váy" },
      { id: "c11", label: "Váy > Váy dài" },
      { id: "c2", label: "Áo" },
      { id: "c21", label: "Áo > Áo sơ mi" },
    ]);
  });
});

describe("warehouseOptions", () => {
  it("chỉ kho có inventory.read theo phạm vi kho, không trùng, xếp theo mã", () => {
    const grants: Grant[] = [
      { permission: "inventory.read", scope: { type: "warehouse", id: UUID_B } },
      { permission: "inventory.read", scope: { type: "warehouse", id: UUID_A } },
      { permission: "inventory.read", scope: { type: "warehouse", id: UUID_A } },
      { permission: "inventory.stocktake", scope: { type: "warehouse", id: "khac" } },
      { permission: "inventory.read", scope: { type: "all", id: "" } },
    ];
    expect(warehouseOptions(grants)).toEqual([
      { id: UUID_A, label: `Kho ${UUID_A}` },
      { id: UUID_B, label: `Kho ${UUID_B}` },
    ]);
  });
});

describe("readFilters, writeFilters", () => {
  it("mặc định: mọi trạng thái, kho đầu tiên có quyền", () => {
    expect(readFilters(new URLSearchParams(), [UUID_A, UUID_B])).toEqual({ ...DEFAULT_FILTERS, warehouseId: UUID_A });
    expect(readFilters(new URLSearchParams(), [])).toEqual(DEFAULT_FILTERS);
  });
  it("đọc lại đúng thứ đã ghi", () => {
    const f = { q: "đầm nhung", status: "active", categoryId: UUID_A, tagId: UUID_B, warehouseId: UUID_B } as const;
    expect(readFilters(writeFilters(f), [UUID_A, UUID_B])).toEqual(f);
  });
  it("bỏ giá trị lạ trên URL (trạng thái lạ, mã không phải uuid, kho không có quyền, tìm quá 100 ký tự)", () => {
    const sp = new URLSearchParams({
      status: "deleted",
      category: "x' or 1=1",
      tag: "abc",
      kho: UUID_B,
      q: "a".repeat(150),
    });
    expect(readFilters(sp, [UUID_A])).toEqual({ ...DEFAULT_FILTERS, q: "a".repeat(100), warehouseId: UUID_A });
  });
  it("hasFilters không tính kho", () => {
    expect(hasFilters({ ...DEFAULT_FILTERS, warehouseId: UUID_A })).toBe(false);
    expect(hasFilters({ ...DEFAULT_FILTERS, q: " " })).toBe(false);
    expect(hasFilters({ ...DEFAULT_FILTERS, tagId: UUID_A })).toBe(true);
  });
});

describe("filterSummary", () => {
  it("câu Đang lọc của trạng thái E", () => {
    const f = { ...DEFAULT_FILTERS, status: "active", categoryId: "c1", tagId: "t1" } as const;
    const names = { category: new Map([["c1", "Váy"]]), tag: new Map([["t1", "Hàng mới"]]) };
    expect(filterSummary(f, names)).toBe("Trạng thái Đang bán, danh mục Váy, nhãn Hàng mới");
    expect(filterSummary(DEFAULT_FILTERS, names)).toBe("");
  });
});

describe("coverPicture", () => {
  const v = (width: number, format: "avif" | "webp" | "jpeg") => ({
    width,
    format,
    url: `/f/${String(width)}.${format}`,
  });
  it("chưa có ảnh, ảnh mất", () => {
    expect(coverPicture(undefined)).toEqual({ kind: "none" });
    expect(coverPicture({ imageId: "i", status: "lost" })).toEqual({ kind: "lost" });
    expect(coverPicture({ imageId: "i", status: "ready", variants: [] })).toEqual({ kind: "lost" });
  });
  it("ảnh sẵn: nguồn avif, webp theo bề rộng; ảnh dự phòng là jpeg nhỏ nhất", () => {
    expect(
      coverPicture({
        imageId: "i",
        status: "ready",
        variants: [v(640, "jpeg"), v(160, "jpeg"), v(160, "avif"), v(320, "avif"), v(160, "webp")],
      }),
    ).toEqual({
      kind: "ready",
      sources: [
        { type: "image/avif", srcSet: "/f/160.avif 160w, /f/320.avif 320w" },
        { type: "image/webp", srcSet: "/f/160.webp 160w" },
      ],
      src: "/f/160.jpeg",
      srcSet: "/f/160.jpeg 160w, /f/640.jpeg 640w",
    });
  });
});
