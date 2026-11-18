// @vitest-environment jsdom
// Danh sách phiếu kho (kế hoạch M1 Task 328; mockup docs/mockups/m1/kho-phieu.html trạng thái A): cột theo API, lọc
// gửi đúng tham số, phiếu hủy mờ, nút "Tạo phiếu" chỉ khi có quyền soạn.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DocumentEdit } from "./DocumentEdit";
import { DocumentList } from "./DocumentList";
import * as kit from "./documentsTestkit";

const { doc, grantsOf, renderDocs, DOC_EXPORT, DOC_IMPORT } = kit;

let fake: kit.Fake;
const LONG =
  "Công ty TNHH Sản xuất Thương mại và Dịch vụ May mặc Xuất khẩu Thanh Bình Hưng Thịnh Phát, chi nhánh Khu công nghiệp";

beforeEach(() => {
  fake = kit.installFake();
  fake.docs.set(
    DOC_IMPORT,
    doc(DOC_IMPORT, { code: "PN-000123", kind: "import", status: "posted", supplierName: LONG, supplierId: "s1" }),
  );
  fake.docs.set(DOC_EXPORT, doc(DOC_EXPORT, {}));
  fake.docs.set("d-adj", doc("d-adj", { code: "DC-000003", kind: "adjust", status: "cancelled" }));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ui = { list: <DocumentList />, edit: <DocumentEdit /> };

describe("DocumentList", () => {
  it("hiện mã, loại, nhà cung cấp, trạng thái, giờ tạo; phiếu hủy mờ; không nhà cung cấp là —", async () => {
    renderDocs(grantsOf("inventory.documents.create"), "/kho/phieu", ui);
    const link = await screen.findByRole("link", { name: "PN-000123" });
    expect(link.getAttribute("href")).toBe(`/kho/phieu/${DOC_IMPORT}`);
    const imp = link.closest("tr");
    if (imp === null) throw new Error("không có dòng");
    expect(within(imp).getByText("Nhập hàng")).toBeTruthy();
    expect(within(imp).getByText(LONG)).toBeTruthy();
    expect(within(imp).getByText("Đã ghi sổ")).toBeTruthy();
    expect(within(imp).getByText("14:30 18/11")).toBeTruthy();
    const exp = screen.getByRole("link", { name: "PX-000045" }).closest("tr");
    if (exp === null) throw new Error("không có dòng");
    expect(within(exp).getByText("Xuất khác")).toBeTruthy();
    expect(within(exp).getByText("—")).toBeTruthy();
    expect(within(exp).getByText("Nháp")).toBeTruthy();
    const adj = screen.getByRole("link", { name: "DC-000003" }).closest("tr");
    expect(adj?.className).toContain("is-muted");
    expect(screen.getByRole("link", { name: "Tạo phiếu" }).getAttribute("href")).toBe("/kho/phieu/moi");
  });

  it("lọc trạng thái, loại gửi status, kind; không quyền soạn thì không có nút Tạo phiếu", async () => {
    renderDocs(grantsOf(), "/kho/phieu", ui);
    await screen.findByRole("link", { name: "PN-000123" });
    expect(screen.queryByRole("link", { name: "Tạo phiếu" })).toBeNull();
    fireEvent.change(screen.getByRole("combobox", { name: "Trạng thái phiếu" }), { target: { value: "draft" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Loại phiếu" }), { target: { value: "import" } });
    await waitFor(() => {
      const last = fake.listCalls.at(-1);
      expect(last?.get("status")).toBe("draft");
      expect(last?.get("kind")).toBe("import");
    });
    expect(fake.listCalls[0]?.has("status")).toBe(false);
  });
});
