// @vitest-environment jsdom
// Bảng biến thể màu × size (kế hoạch M1 Task 324; mockup docs/mockups/m1/san-pham-sua.html, thẻ "Biến thể").
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it } from "vitest";
import type { Variant } from "../../api";
import { addOption, draftFromProduct, emptyDraft, setCode, type Draft, type RowErrors } from "./editRules";
import { VariantGrid } from "./VariantGrid";

afterEach(cleanup);

function Harness({
  initial,
  errors = {},
  readOnly = false,
}: {
  initial: Draft;
  errors?: Record<string, RowErrors>;
  readOnly?: boolean;
}) {
  const [d, setD] = useState(initial);
  return <VariantGrid draft={d} errors={errors} readOnly={readOnly} onChange={setD} />;
}

const saved: Variant = {
  id: "0195f3b2-0000-7000-8000-000000000011",
  productId: "0195f3b2-0000-7000-8000-000000000001",
  sku: "DAM0123-DEN-S",
  barcode: "2000000000015",
  color: "Đen",
  size: "S",
  price: 350000,
  costPrice: 180000,
  status: "active",
  version: 1,
};

const valueOf = (el: HTMLElement) => (el instanceof HTMLInputElement ? el.value : null);

function addVia(label: string, value: string) {
  const input = screen.getByRole("textbox", { name: label });
  fireEvent.change(input, { target: { value } });
  fireEvent.keyDown(input, { key: "Enter" });
}

describe("VariantGrid", () => {
  it("thêm 2 màu, 3 size bằng Enter → 6 dòng, SKU gợi ý, câu đếm", () => {
    render(<Harness initial={setCode(emptyDraft(), "DAM0123")} />);
    expect(screen.getByText("Thêm màu và size để có biến thể.")).toBeTruthy();
    addVia("Thêm màu", "Đen");
    addVia("Thêm màu", "Be");
    for (const s of ["S", "M", "L"]) addVia("Thêm size", s);
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows.length).toBe(6);
    expect(rows.at(0)?.textContent).toContain("Đen · S");
    expect(valueOf(screen.getByRole("textbox", { name: "SKU của Be · L" }))).toBe("DAM0123-BE-L");
    expect(screen.getByText("2 màu × 3 size = 6 biến thể")).toBeTruthy();
    expect(valueOf(screen.getByRole("textbox", { name: "Thêm size" }))).toBe("");
  });

  it("nút bỏ màu: chỉ có ở màu chưa có biến thể đã lưu; bỏ màu mới bỏ dòng của nó", () => {
    const d = addOption(
      draftFromProduct(
        {
          id: saved.productId,
          code: "DAM0123",
          name: "Đầm",
          description: "",
          article: [],
          articleHtml: "",
          status: "active",
          version: 3,
          variants: [saved],
          images: [],
        },
        { productId: saved.productId, version: 3, attributeValueIds: [], tagIds: [] },
      ),
      "color",
      "Be",
    );
    render(<Harness initial={d} />);
    expect(screen.queryByRole("button", { name: "Bỏ màu Đen" })).toBeNull();
    expect(screen.getAllByRole("row").length - 1).toBe(2);
    fireEvent.click(screen.getByRole("button", { name: "Bỏ màu Be" }));
    expect(screen.getAllByRole("row").length - 1).toBe(1);
    // Biến thể đã lưu có ô trạng thái; Ngừng bán làm mờ dòng.
    const status = screen.getByRole("combobox", { name: "Trạng thái của Đen · S" });
    fireEvent.change(status, { target: { value: "discontinued" } });
    expect(status.closest("tr")?.className).toContain("is-muted");
  });

  it("lỗi của dòng: viền đỏ, câu dưới ô, dòng tô đỏ", () => {
    const d = addOption(addOption(setCode(emptyDraft(), "DAM0123"), "color", "Đen"), "size", "L");
    const key = d.rows[0]?.key ?? "";
    render(<Harness initial={d} errors={{ [key]: { sku: "Mã hàng đã có", price: "Nhập giá bán" } }} />);
    const sku = screen.getByRole("textbox", { name: "SKU của Đen · L" });
    expect(sku.className).toContain("is-err");
    expect(sku.getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByText("Mã hàng đã có")).toBeTruthy();
    expect(screen.getByText("Nhập giá bán")).toBeTruthy();
    expect(sku.closest("tr")?.className).toContain("is-err");
  });

  it("chỉ đọc: không có ô thêm màu/size, không nút bỏ", () => {
    const d = addOption(addOption(emptyDraft(), "color", "Đen"), "size", "L");
    render(<Harness initial={d} readOnly />);
    expect(screen.queryByRole("textbox", { name: "Thêm màu" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Bỏ màu Đen" })).toBeNull();
  });
});
