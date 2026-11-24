// Lọc mẫu theo nguồn của banner giảm giá (Task 459): chiến dịch theo sản phẩm, theo danh mục (cả con cháu), toàn shop.
import { describe, expect, it } from "vitest";
import type { ProductListItem } from "../../api";
import { campaignFilter } from "./ProductPicker";
import { C1, CAT1, P1, PRODUCTS, list } from "./bannerTestkit";

const CHILD = "0195f3b2-0000-7000-8000-0000000c0002";
const OTHER = "0195f3b2-0000-7000-8000-0000000c0003";
const withCat = (p: ProductListItem | undefined, categoryId: string): ProductListItem => {
  if (p === undefined) throw new Error("thiếu mẫu");
  return { ...p, categoryId };
};

describe("campaignFilter", () => {
  it("chiến dịch theo sản phẩm: chỉ mẫu được chọn", () => {
    const l = list([]);
    l.campaigns = l.campaigns.map((c) => ({ ...c, targetIds: [P1] }));
    const f = campaignFilter(l, C1);
    expect(PRODUCTS.filter((p) => f?.(p) ?? true).map((p) => p.id)).toEqual([P1]);
  });
  it("chiến dịch theo danh mục: mẫu ở danh mục chọn và danh mục con; danh mục khác bị loại", () => {
    const l = list([], {
      categories: [
        { id: CAT1, name: "Đầm" },
        { id: CHILD, parentId: CAT1, name: "Đầm công sở" },
        { id: OTHER, name: "Áo" },
      ],
    });
    l.campaigns = l.campaigns.map((c) => ({ ...c, scope: "categories", targetIds: [CAT1] }));
    const f = campaignFilter(l, C1);
    expect(f?.(withCat(PRODUCTS[0], CHILD))).toBe(true);
    expect(f?.(withCat(PRODUCTS[1], CAT1))).toBe(true);
    expect(f?.(withCat(PRODUCTS[1], OTHER))).toBe(false);
    expect(f?.({ ...withCat(PRODUCTS[1], OTHER), categoryId: undefined })).toBe(false);
  });
  it("toàn shop hay chưa chọn chiến dịch: không lọc", () => {
    const l = list([]);
    l.campaigns = l.campaigns.map((c) => ({ ...c, scope: "all" }));
    expect(campaignFilter(l, C1)).toBeUndefined();
    expect(campaignFilter(l, undefined)).toBeUndefined();
  });
});
