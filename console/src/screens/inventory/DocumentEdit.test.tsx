// @vitest-environment jsdom
// Soạn, ghi sổ, hủy phiếu kho (kế hoạch M1 Task 328; mockup docs/mockups/m1/kho-phieu.html trạng thái B–F).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DocumentEdit, TEXT_NO_ADJUST_WAREHOUSE, TEXT_NO_POST_RIGHT } from "./DocumentEdit";
import { DocumentList } from "./DocumentList";
import {
  doc,
  grantAt,
  grantsOf,
  installFake,
  renderDocs,
  DOC_EXPORT,
  DOC_IMPORT,
  V1,
  V2,
  W1,
  W2,
  TWO_WAREHOUSES,
  type Fake,
} from "./documentsTestkit";

let fake: Fake;
const ALL = grantsOf("inventory.documents.create", "inventory.documents.post", "inventory.suppliers.manage");
const ui = { list: <DocumentList />, edit: <DocumentEdit /> };

beforeEach(() => {
  // jsdom chưa có showModal của <dialog>.
  HTMLDialogElement.prototype.showModal = function (this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function (this: HTMLDialogElement) {
    this.removeAttribute("open");
  };
  fake = installFake();
  fake.docs.set(
    DOC_EXPORT,
    doc(DOC_EXPORT, {
      note: "Xuất mẫu cho buổi chụp ảnh",
      lines: [
        { variantId: V1, quantity: 3 },
        { variantId: V2, quantity: 2 },
      ],
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const posts = () => fake.writes.filter((w) => w.path.endsWith("/post"));

async function scan(text: string) {
  const input = screen.getByRole("textbox", { name: "Thêm dòng" });
  fireEvent.change(input, { target: { value: text } });
  fireEvent.keyDown(input, { key: "Enter" });
  await waitFor(() => {
    expect((input as HTMLInputElement).value).toBe("");
  });
}

describe("DocumentEdit — phiếu nhập mới", () => {
  it("thêm nhanh nhà cung cấp trong ô chọn, quét mã hai lần, lưu nháp gửi tạo phiếu rồi thay dòng", async () => {
    renderDocs(ALL, "/kho/phieu/moi?kind=import", ui);
    const combo = await screen.findByRole("combobox", { name: "Nhà cung cấp" });
    fireEvent.focus(combo);
    fireEvent.change(combo, { target: { value: "Xưởng Hoa" } });
    const add = await screen.findByRole("option", { name: '+ Thêm nhà cung cấp "Xưởng Hoa"' });
    expect(screen.getByRole("option", { name: "Xưởng may Hoa Sen" })).toBeTruthy();
    fireEvent.click(add);
    await waitFor(() => {
      expect((combo as HTMLInputElement).value).toBe("Xưởng Hoa");
    });
    expect(fake.writes.filter((w) => w.path === "/api/v1/inventory/suppliers")).toHaveLength(1);
    expect(fake.writes[0]?.body).toEqual({ name: "Xưởng Hoa" });
    expect(fake.writes[0]?.key).toBeTruthy();

    await scan("QU0789-DEN-M");
    await scan("QU0789-DEN-M");
    const row = screen.getByText("QU0789-DEN-M").closest("tr");
    if (row === null) throw new Error("không có dòng");
    expect(within(row).getByRole<HTMLInputElement>("textbox", { name: "Số lượng" }).value).toBe("2");
    fireEvent.change(within(row).getByRole("textbox", { name: "Số lượng" }), { target: { value: "10" } });
    fireEvent.change(within(row).getByRole("textbox", { name: "Giá nhập" }), { target: { value: "180.000" } });
    expect(within(row).getByText("1.800.000 ₫")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Ghi sổ…" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await screen.findByRole("heading", { name: "Phiếu nhập PN-000125" });
    const create = fake.writes.find((w) => w.path === "/api/v1/inventory/documents");
    const supplierId = fake.suppliers.find((s) => s.name === "Xưởng Hoa")?.id;
    expect(create?.body).toEqual({ warehouseId: W1, kind: "import", supplierId });
    const lines = fake.writes.find((w) => w.path.endsWith("/lines"));
    expect(lines?.body).toEqual({ version: 1, lines: [{ variantId: V1, quantity: 10, unitCost: 180000 }] });
    // Sau khi lưu: phiếu nháp có nút ghi sổ, hiện tên nhà cung cấp.
    expect(await screen.findByRole("button", { name: "Ghi sổ…" })).toBeTruthy();
    expect(screen.getByText("Xưởng Hoa")).toBeTruthy();
  });

  it("đổi loại sang phiếu xuất: không có ô nhà cung cấp", async () => {
    renderDocs(ALL, "/kho/phieu/moi?kind=import", ui);
    await screen.findByRole("combobox", { name: "Nhà cung cấp" });
    fireEvent.change(screen.getByRole("combobox", { name: "Loại phiếu" }), { target: { value: "export" } });
    expect(screen.queryByRole("combobox", { name: "Nhà cung cấp" })).toBeNull();
    expect(screen.getByRole("heading", { name: "Phiếu xuất mới" })).toBeTruthy();
  });
});

describe("DocumentEdit — phiếu xuất nháp", () => {
  it("không có ô nhà cung cấp; cột có thể bán; ghi sổ vượt phần giữ → không đủ hàng: còn 2, phiếu vẫn nháp", async () => {
    fake.failPost = {
      status: 409,
      body: { error: "insufficient_stock", variantId: V1, available: 2 },
    };
    renderDocs(ALL, `/kho/phieu/${DOC_EXPORT}`, ui);
    await screen.findByRole("heading", { name: "Phiếu xuất PX-000045" });
    expect(screen.queryByText("Nhà cung cấp")).toBeNull();
    const row = (await screen.findByText("QU0789-DEN-M")).closest("tr");
    if (row === null) throw new Error("không có dòng");
    expect(row.querySelector('td[data-label="Có thể bán"]')?.textContent).toBe("2");

    fireEvent.click(screen.getByRole("button", { name: "Ghi sổ…" }));
    const dlg = await screen.findByRole("dialog", { name: "Ghi sổ phiếu PX-000045?" });
    expect(within(dlg).getByText(/giảm/)).toBeTruthy();
    fireEvent.click(within(dlg).getByRole("button", { name: "Ghi sổ" }));
    await screen.findByText("Không đủ hàng: còn 2.");
    expect(within(row).getByText("Không đủ hàng: còn 2")).toBeTruthy();
    expect(row.className).toContain("is-err");
    expect(screen.getByText("Nháp", { selector: ".chip" })).toBeTruthy();
    expect(posts()).toHaveLength(1);
    expect(posts()[0]?.body).toEqual({ version: 1 });
  });

  it("bấm đúp Ghi sổ chỉ gửi một lần; nút thành Đang ghi sổ… rồi phiếu thành Đã ghi sổ (chỉ xem)", async () => {
    const placeholder = () => undefined;
    fake.holdPost = placeholder;
    renderDocs(ALL, `/kho/phieu/${DOC_EXPORT}`, ui);
    fireEvent.click(await screen.findByRole("button", { name: "Ghi sổ…" }));
    const dlg = await screen.findByRole("dialog");
    const go = within(dlg).getByRole("button", { name: "Ghi sổ" });
    fireEvent.click(go);
    fireEvent.click(go);
    await waitFor(() => {
      expect(within(dlg).getByRole("button", { name: "Đang ghi sổ…" }).hasAttribute("disabled")).toBe(true);
    });
    // Máy chủ đã nhận yêu cầu và đang giữ câu trả lời (holdPost đã thành hàm thả).
    await waitFor(() => {
      expect(fake.holdPost).not.toBe(placeholder);
    });
    fake.holdPost();
    await screen.findByText("Đã ghi sổ", { selector: ".chip" });
    expect(posts()).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Lưu nháp" })).toBeNull();
    expect(screen.getByText("Phiếu đã ghi sổ không sửa được. Sửa sai bằng phiếu điều chỉnh.")).toBeTruthy();
  });

  it("sửa số lượng rồi ghi sổ: lưu dòng trước, ghi sổ với version mới", async () => {
    renderDocs(ALL, `/kho/phieu/${DOC_EXPORT}`, ui);
    const row = (await screen.findByText("QU0789-DEN-M")).closest("tr");
    if (row === null) throw new Error("không có dòng");
    fireEvent.click(within(row).getByRole("button", { name: "Bớt 1" }));
    fireEvent.click(screen.getByRole("button", { name: "Ghi sổ…" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Ghi sổ" }));
    await screen.findByText("Đã ghi sổ", { selector: ".chip" });
    const paths = fake.writes.map((w) => w.path.split("/").at(-1));
    expect(paths).toEqual(["lines", "post"]);
    expect(fake.writes[0]?.body).toEqual({
      version: 1,
      lines: [
        { variantId: V1, quantity: 2 },
        { variantId: V2, quantity: 2 },
      ],
    });
    expect(fake.writes[1]?.body).toEqual({ version: 2 });
  });

  it("không có quyền ghi sổ: nút mờ kèm lý do; vẫn lưu nháp được", async () => {
    renderDocs(grantsOf("inventory.documents.create"), `/kho/phieu/${DOC_EXPORT}`, ui);
    const btn = await screen.findByRole("button", { name: "Ghi sổ…" });
    expect(btn.hasAttribute("disabled")).toBe(true);
    expect(
      screen.getByText("Cần quyền ghi sổ phiếu kho (vai trò Chủ hoặc Quản lý). Lưu nháp rồi báo người duyệt."),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Lưu nháp" })).toBeTruthy();
  });

  it("chỉ quyền xem: không nút sửa, không ô số lượng", async () => {
    renderDocs(grantsOf(), `/kho/phieu/${DOC_EXPORT}`, ui);
    await screen.findByText("QU0789-DEN-M");
    expect(screen.queryByRole("button", { name: "Lưu nháp" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Hủy phiếu" })).toBeNull();
    expect(screen.queryByRole("textbox", { name: "Số lượng" })).toBeNull();
  });

  it("hủy phiếu có xác nhận, gửi version; phiếu thành Đã hủy", async () => {
    renderDocs(ALL, `/kho/phieu/${DOC_EXPORT}`, ui);
    fireEvent.click(await screen.findByRole("button", { name: "Hủy phiếu" }));
    const dlg = await screen.findByRole("dialog", { name: "Hủy phiếu PX-000045?" });
    fireEvent.click(within(dlg).getByRole("button", { name: "Hủy phiếu" }));
    await screen.findByText("Đã hủy", { selector: ".chip" });
    expect(fake.writes.map((w) => [w.path.split("/").at(-1), w.body])).toEqual([["cancel", { version: 1 }]]);
  });

  it("người khác vừa sửa: câu tải lại, bấm Tải lại lấy bản mới", async () => {
    renderDocs(ALL, `/kho/phieu/${DOC_EXPORT}`, ui);
    const first = (await screen.findByText("QU0789-DEN-M")).closest("tr");
    if (first === null) throw new Error("không có dòng");
    const d = fake.docs.get(DOC_EXPORT);
    if (d === undefined) throw new Error("thiếu phiếu");
    fake.docs.set(DOC_EXPORT, { ...d, version: 5, lines: [{ variantId: V2, quantity: 7 }] });
    fireEvent.click(within(first).getByRole("button", { name: "Bớt 1" }));
    fireEvent.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await screen.findByText("Người khác vừa sửa mục này. Tải lại để xem bản mới rồi sửa lại");
    fireEvent.click(screen.getByRole("button", { name: "Tải lại" }));
    await waitFor(() => {
      expect(screen.queryByText("QU0789-DEN-M")).toBeNull();
    });
    const row = screen.getByText("VAY0101-BE-M").closest("tr");
    expect((row?.querySelector("input") as HTMLInputElement | null)?.value).toBe("7");
  });
});

describe("DocumentEdit — phiếu nhập đã ghi sổ", () => {
  it("chỉ xem: tên nhà cung cấp, kho, tổng", async () => {
    fake.docs.set(
      DOC_IMPORT,
      doc(DOC_IMPORT, {
        code: "PN-000123",
        kind: "import",
        status: "posted",
        postedAt: "2026-11-18T02:15:00Z",
        supplierId: "s1",
        supplierName: "Xưởng Hoa",
        lines: [{ variantId: V1, quantity: 10, unitCost: 180000 }],
      }),
    );
    renderDocs(ALL, `/kho/phieu/${DOC_IMPORT}`, ui);
    await screen.findByRole("heading", { name: "Phiếu nhập PN-000123" });
    expect(screen.getByText("Xưởng Hoa")).toBeTruthy();
    expect(screen.getByText(/Ghi sổ lúc 09:15 18\/11/)).toBeTruthy();
    expect(await screen.findByText("Kho chính")).toBeTruthy();
    expect(screen.getAllByText("1.800.000 ₫").length).toBeGreaterThan(0);
    expect(screen.queryByRole("textbox")).toBeNull();
  });
});

// E-01 (review độc lập M1): máy chủ kiểm quyền phiếu theo kho của phiếu (canAt trong handlers_inventory.go); phiếu
// điều chỉnh đòi thêm inventory.documents.post khi tạo và thay dòng. Người xem đọc được cả hai kho (inventory.read toàn
// shop) nhưng chỉ soạn, ghi sổ ở kho chính.
describe("DocumentEdit — quyền theo kho (E-01)", () => {
  const atW1 = (...perms: string[]) => [...grantsOf(), ...perms.map((p) => grantAt(p, W1))];
  const warehouseChoices = async () => {
    const field = (await screen.findByText("Kho", { selector: ".field > span" })).closest("label");
    if (field === null) throw new Error("không có ô kho");
    return within(field)
      .getAllByRole("option")
      .map((o) => o.textContent);
  };

  beforeEach(() => {
    fake.warehouses = TWO_WAREHOUSES;
  });

  it("phiếu mới chỉ cho chọn kho có quyền soạn ở kho đó", async () => {
    renderDocs(atW1("inventory.documents.create"), "/kho/phieu/moi?kind=export", ui);
    await waitFor(async () => {
      expect(await warehouseChoices()).toEqual(["Kho chính"]);
    });
  });

  it("quyền soạn toàn shop: chọn được mọi kho đang dùng", async () => {
    renderDocs(grantsOf("inventory.documents.create"), "/kho/phieu/moi?kind=export", ui);
    await waitFor(async () => {
      expect(await warehouseChoices()).toEqual(["Kho chính", "Kho phụ"]);
    });
  });

  it("phiếu điều chỉnh mới cần cả quyền ghi sổ ở kho: thiếu thì không có kho và không lưu được", async () => {
    renderDocs(
      [...atW1("inventory.documents.create", "inventory.documents.post"), grantAt("inventory.documents.create", W2)],
      "/kho/phieu/moi?kind=adjust",
      ui,
    );
    await waitFor(async () => {
      expect(await warehouseChoices()).toEqual(["Kho chính"]);
    });
    cleanup();
    renderDocs(atW1("inventory.documents.create"), "/kho/phieu/moi?kind=adjust", ui);
    expect(await screen.findByText(TEXT_NO_ADJUST_WAREHOUSE)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Lưu nháp" }).hasAttribute("disabled")).toBe(true);
  });

  it("phiếu nháp ở kho phụ: quyền soạn, ghi sổ chỉ ở kho chính → chỉ xem, không nút", async () => {
    fake.docs.set(DOC_EXPORT, { ...doc(DOC_EXPORT, { warehouseId: W2 }), lines: [{ variantId: V1, quantity: 1 }] });
    renderDocs(atW1("inventory.documents.create", "inventory.documents.post"), `/kho/phieu/${DOC_EXPORT}`, ui);
    await screen.findByText("QU0789-DEN-M");
    expect(screen.queryByRole("button", { name: "Lưu nháp" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Ghi sổ…" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Hủy phiếu" })).toBeNull();
  });

  it("phiếu nháp ở kho chính: soạn toàn shop, ghi sổ chỉ ở kho phụ → Ghi sổ mờ kèm lý do", async () => {
    renderDocs(
      [...grantsOf("inventory.documents.create"), grantAt("inventory.documents.post", W2)],
      `/kho/phieu/${DOC_EXPORT}`,
      ui,
    );
    const btn = await screen.findByRole("button", { name: "Ghi sổ…" });
    expect(btn.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(TEXT_NO_POST_RIGHT)).toBeTruthy();
  });

  it("phiếu điều chỉnh nháp: có soạn mà không có ghi sổ ở kho → chỉ xem (máy chủ từ chối thay dòng)", async () => {
    fake.docs.set(
      DOC_EXPORT,
      doc(DOC_EXPORT, { kind: "adjust", code: "DC-000004", lines: [{ variantId: V1, quantity: -1 }] }),
    );
    renderDocs(grantsOf("inventory.documents.create"), `/kho/phieu/${DOC_EXPORT}`, ui);
    await screen.findByText("QU0789-DEN-M");
    expect(screen.queryByRole("button", { name: "Lưu nháp" })).toBeNull();
  });
});
