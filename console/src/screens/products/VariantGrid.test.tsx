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
  busy = false,
}: {
  initial: Draft;
  errors?: Record<string, RowErrors>;
  readOnly?: boolean;
  busy?: boolean;
}) {
  const [d, setD] = useState(initial);
  return <VariantGrid draft={d} errors={errors} readOnly={readOnly} busy={busy} onChange={setD} />;
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

  describe("thứ tự (Task 513; mockup docs/mockups/m5/variant-order.html)", () => {
    const six = () => {
      let d = setCode(emptyDraft(), "DAM0123");
      for (const c of ["Đen", "Be"]) d = addOption(d, "color", c);
      for (const s of ["S", "M", "L"]) d = addOption(d, "size", s);
      return d;
    };
    const order = () =>
      screen
        .getAllByRole("row")
        .slice(1)
        .map((r) => r.querySelector(".v-name")?.textContent);

    it("nút ↑ ↓: dời một bậc, dòng đã dời nền xám; đầu bảng không lên, cuối bảng không xuống", () => {
      render(<Harness initial={six()} />);
      expect(screen.getByRole("button", { name: "Đưa Đen · S lên" }).hasAttribute("disabled")).toBe(true);
      expect(screen.getByRole("button", { name: "Đưa Be · L xuống" }).hasAttribute("disabled")).toBe(true);
      fireEvent.click(screen.getByRole("button", { name: "Đưa Be · S lên" }));
      expect(order()).toEqual(["Đen · S", "Đen · M", "Be · S", "Đen · L", "Be · M", "Be · L"]);
      expect(screen.getByText("Be · S").closest("tr")?.className).toContain("is-moved");
      fireEvent.click(screen.getByRole("button", { name: "Đưa Đen · S xuống" }));
      expect(order()).toEqual(["Đen · M", "Đen · S", "Be · S", "Đen · L", "Be · M", "Be · L"]);
    });

    it("bàn phím: phím ↑ ↓ trên tay nắm dời dòng, tay nắm giữ tiêu điểm; phím khác không làm gì", () => {
      render(<Harness initial={six()} />);
      const grip = screen.getByRole("button", { name: "Kéo để dời Đen · L" });
      grip.focus();
      fireEvent.keyDown(grip, { key: "ArrowUp" });
      fireEvent.keyDown(screen.getByRole("button", { name: "Kéo để dời Đen · L" }), { key: "ArrowUp" });
      expect(order()).toEqual(["Đen · L", "Đen · S", "Đen · M", "Be · S", "Be · M", "Be · L"]);
      expect(document.activeElement?.getAttribute("aria-label")).toBe("Kéo để dời Đen · L");
      fireEvent.keyDown(document.activeElement ?? grip, { key: "ArrowDown" });
      fireEvent.keyDown(document.activeElement ?? grip, { key: "Enter" });
      expect(order()).toEqual(["Đen · S", "Đen · L", "Đen · M", "Be · S", "Be · M", "Be · L"]);
    });

    it("kéo thả: thả dòng lên dòng khác → đứng ở chỗ dòng đó; dòng đang kéo mờ", () => {
      render(<Harness initial={six()} />);
      const from = screen.getByText("Be · L").closest("tr");
      const to = screen.getByText("Đen · M").closest("tr");
      if (from === null || to === null) throw new Error("thiếu dòng");
      fireEvent.dragStart(from);
      expect(from.className).toContain("is-drag");
      fireEvent.dragOver(to);
      fireEvent.drop(to);
      fireEvent.dragEnd(from);
      expect(order()).toEqual(["Đen · S", "Be · L", "Đen · M", "Đen · L", "Be · S", "Be · M"]);
      expect(screen.getByText("Be · L").closest("tr")?.className).not.toContain("is-drag");
    });

    it("đang lưu: dòng không kéo được, thả không đổi thứ tự", () => {
      render(<Harness initial={six()} busy />);
      const from = screen.getByText("Be · L").closest("tr");
      const to = screen.getByText("Đen · M").closest("tr");
      if (from === null || to === null) throw new Error("thiếu dòng");
      expect(from.getAttribute("draggable")).not.toBe("true");
      fireEvent.dragStart(from);
      fireEvent.dragOver(to);
      fireEvent.drop(to);
      expect(order()).toEqual(["Đen · S", "Đen · M", "Đen · L", "Be · S", "Be · M", "Be · L"]);
      expect(from.className).not.toContain("is-drag");
    });

    it("chỉ xem: cột Thứ tự hiện số, không tay nắm, không nút, dòng không kéo được", () => {
      render(<Harness initial={six()} readOnly />);
      expect(screen.getByRole("columnheader", { name: "Thứ tự" })).toBeTruthy();
      expect(screen.queryByRole("button", { name: /Kéo để dời|Đưa .* lên/ })).toBeNull();
      const first = screen.getByText("Đen · S").closest("tr");
      expect(first?.querySelector(".ord")?.textContent).toBe("1");
      expect(first?.getAttribute("draggable")).not.toBe("true");
    });

    it("tên rất dài vẫn có đủ nút với nhãn đầy đủ", () => {
      const long = "Xanh rêu đậm pha ánh kim loại bạc kiểu cổ điển xưa";
      render(<Harness initial={addOption(addOption(six(), "color", long), "size", "XXL")} />);
      expect(screen.getByRole("button", { name: `Đưa ${long} · XXL xuống` }).hasAttribute("disabled")).toBe(true);
      expect(screen.getByRole("button", { name: `Kéo để dời ${long} · XXL` })).toBeTruthy();
    });
  });
});
