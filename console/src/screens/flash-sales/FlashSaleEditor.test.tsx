// @vitest-environment jsdom
// Trang một đợt flash sale (mockup docs/mockups/m5/flash-sales.html trạng thái C–H). Kế hoạch M5 Task 206 Step 1: soạn
// món, lên lịch (xác nhận, thiếu hàng liệt kê đủ mã, chồng giờ), đóng sớm cần xác nhận, thêm suất, số trực tiếp tải lại
// khi có báo hiệu, 409 tải lại, không quyền không nút ghi, tên 120 ký tự.
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { onRealtimeChanged } from "../../app/queryClient";
import {
  HIT_BE,
  LONG_NAME,
  S1,
  grantsOf,
  hookFlashFake,
  typeInto,
  item,
  liveItem,
  renderAt,
  sale,
  type Fake,
} from "./flashTestkit";

let fake: Fake;
hookFlashFake((f) => {
  fake = f;
});
const type = typeInto;
const btn = (name: string | RegExp) => screen.getByRole("button", { name });
const kpiBar = (): HTMLElement => {
  const el = document.querySelector<HTMLElement>(".kpis");
  if (el === null) throw new Error("chưa có hàng số tổng");
  return el;
};
const kpi = (label: string): HTMLElement => {
  const el = within(kpiBar()).getByText(label).closest<HTMLElement>(".kpi");
  if (el === null) throw new Error(`không có ô ${label}`);
  return el;
};
const rowOf = (text: string) => screen.getByText(text).closest("tr") as HTMLElement;
const open = async (id = S1) => {
  const r = renderAt(`/khuyen-mai/flash-sale/${id}`);
  await screen.findByRole("heading", { level: 1 });
  return r;
};
const dlg = (name: string | RegExp) => screen.findByRole("dialog", { name });
const writes = (fragment: string) => fake.writes.filter((w) => w.path.includes(fragment));

describe("Đợt nháp", () => {
  it("hiện món, giá thường, giá flash, suất, mỗi khách, hạn giữ và đủ nút soạn", async () => {
    await open();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("Đêm 12.12 khung 1");
    expect(screen.getByText("Nháp")).toBeTruthy();
    const row = rowOf("SP0002-DEN-M");
    expect(within(row).getByText("690.000 ₫")).toBeTruthy();
    expect(within(row).getByText("199.000 ₫")).toBeTruthy();
    expect(within(row).getByText("140")).toBeTruthy();
    expect(within(row).getByText("10 phút")).toBeTruthy();
    for (const n of ["Sửa tên, giờ", "+ Thêm món", "Lên lịch"]) expect(btn(n)).toBeTruthy();
    expect(within(row).getByRole("button", { name: "Sửa" })).toBeTruthy();
    expect(within(row).getByRole("button", { name: "Bỏ" })).toBeTruthy();
  });

  it("không có promotion.flash.manage → không một nút ghi nào, vẫn xem được món", async () => {
    fake.grants = grantsOf();
    await open();
    await screen.findByText("SP0001-DEN-M");
    for (const n of ["Sửa tên, giờ", "+ Thêm món", "Lên lịch", "Sửa", "Bỏ", "Đóng sớm", "Thêm suất"]) {
      expect(screen.queryByRole("button", { name: n })).toBeNull();
    }
    expect(screen.getByText(/Bạn chỉ có quyền xem flash sale/)).toBeTruthy();
  });

  it("tên 120 ký tự trong tiêu đề xuống dòng (lớp wrap-any), không cắt chữ", async () => {
    fake.sales = [sale({ name: LONG_NAME })];
    await open();
    const h = screen.getByRole("heading", { level: 1 });
    expect(h.textContent).toContain(LONG_NAME);
    expect(h.className).toContain("wrap-any");
  });

  it("đợt không có trong máy chủ → báo không tìm thấy", async () => {
    renderAt("/khuyen-mai/flash-sale/0195f3b2-0000-7000-8000-00000000ffff");
    await screen.findByText("Không tìm thấy đợt flash sale.");
  });

  it("sửa tên, giờ: chỉ gửi trường đổi kèm version", async () => {
    await open();
    fireEvent.click(btn("Sửa tên, giờ"));
    const d = await dlg("Sửa tên, giờ");
    expect(within(d).getByLabelText<HTMLInputElement>("Giờ mở (giờ VN) *").value).toBe("2026-12-12T00:00");
    type(within(d).getByLabelText("Tên đợt *"), "Đêm 12.12 mới");
    fireEvent.click(within(d).getByRole("button", { name: "Lưu" }));
    await waitFor(() => {
      expect(writes("/flash-sales/" + S1)).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({ method: "PATCH", body: { version: 3, name: "Đêm 12.12 mới" } });
    expect(Object.keys(fake.writes[0]?.body ?? {}).sort()).toEqual(["name", "version"]);
  });
});

describe("Thêm, sửa, bỏ món", () => {
  const openAdd = async () => {
    await open();
    fireEvent.click(btn("+ Thêm món"));
    return dlg(/Thêm món vào/);
  };

  it("tìm biến thể: ngừng bán và đã có trong đợt không chọn được; giá flash không thấp hơn giá thường bị chặn", async () => {
    fake.hits = [HIT_BE, { ...HIT_BE, variantId: "dup", sku: "SP0001-DEN-M" }, ...fake.hits.slice(1)];
    fake.items[S1] = [item(1, { variantId: "dup" })];
    const d = await openAdd();
    type(within(d).getByLabelText("Tìm biến thể theo tên hoặc mã hàng"), "DS");
    const be = await within(d).findByRole("radio", { name: /DS2201-BE-M/ });
    expect((await within(d).findByRole("radio", { name: /DS1802-REU-S/ })).hasAttribute("disabled")).toBe(true);
    expect(within(d).getByText(/đang ngừng bán, không thêm được/)).toBeTruthy();
    fireEvent.click(be);
    type(within(d).getByLabelText("Giá flash *"), "690.000");
    expect(within(d).getByText("Giá flash phải thấp hơn giá bán thường 690.000 ₫.")).toBeTruthy();
    expect(within(d).getByRole("button", { name: "Thêm món" })).toHaveProperty("disabled", true);
  });

  it("biến thể đã nằm trong đợt hiện mờ, không chọn được", async () => {
    fake.hits = [{ ...HIT_BE, variantId: item(1).variantId, sku: "SP0001-DEN-M" }];
    const d = await openAdd();
    type(within(d).getByLabelText("Tìm biến thể theo tên hoặc mã hàng"), "SP0001");
    const r = await within(d).findByRole("radio", { name: /SP0001-DEN-M/ });
    expect(r.hasAttribute("disabled")).toBe(true);
    expect(within(d).getByText(/đã có trong đợt này/)).toBeTruthy();
  });

  it("thêm món hợp lệ gửi đúng thân kèm Idempotency-Key; để trống mỗi khách, hạn giữ → mặc định 1 và 10", async () => {
    const d = await openAdd();
    type(within(d).getByLabelText("Tìm biến thể theo tên hoặc mã hàng"), "DS2201");
    fireEvent.click(await within(d).findByRole("radio", { name: /DS2201-BE-M/ }));
    type(within(d).getByLabelText("Giá flash *"), "199.000");
    type(within(d).getByLabelText("Số suất *"), "200");
    fireEvent.click(within(d).getByRole("button", { name: "Thêm món" }));
    await waitFor(() => {
      expect(writes("/items")).toHaveLength(1);
    });
    const w = writes("/items")[0];
    expect(w?.method).toBe("POST");
    expect(w?.key).toBeTruthy();
    expect(w?.body).toEqual({
      variantId: HIT_BE.variantId,
      salePrice: 199000,
      quantity: 200,
      perCustomerLimit: 1,
      holdMinutes: 10,
    });
  });

  it("lỗi not_a_discount của máy chủ hiện trong hộp, hộp còn mở", async () => {
    const d = await openAdd();
    type(within(d).getByLabelText("Tìm biến thể theo tên hoặc mã hàng"), "DS2201");
    fireEvent.click(await within(d).findByRole("radio", { name: /DS2201-BE-M/ }));
    type(within(d).getByLabelText("Giá flash *"), "100.000");
    type(within(d).getByLabelText("Số suất *"), "5");
    fake.replies.push({ status: 422, body: { error: "not_a_discount", request_id: "t" } });
    fireEvent.click(within(d).getByRole("button", { name: "Thêm món" }));
    await within(d).findByText("Giá flash phải thấp hơn giá bán thường của sản phẩm");
  });

  it("sửa món: chỉ gửi trường đổi kèm version của món; không đổi gì thì khóa nút", async () => {
    await open();
    fireEvent.click(within(rowOf("SP0002-DEN-M")).getByRole("button", { name: "Sửa" }));
    const d = await dlg("Sửa món: SP0002-DEN-M");
    expect(within(d).getByRole("button", { name: "Lưu món" })).toHaveProperty("disabled", true);
    type(within(d).getByLabelText("Số suất *"), "96");
    fireEvent.click(within(d).getByRole("button", { name: "Lưu món" }));
    await waitFor(() => {
      expect(writes("/items/")).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({ method: "PATCH", body: { version: 1, quantity: 96 } });
    expect(Object.keys(fake.writes[0]?.body ?? {}).sort()).toEqual(["quantity", "version"]);
  });

  it("bỏ món cần xác nhận, rồi gửi DELETE kèm Idempotency-Key", async () => {
    await open();
    fireEvent.click(within(rowOf("SP0001-DEN-M")).getByRole("button", { name: "Bỏ" }));
    const d = await dlg("Bỏ món SP0001-DEN-M khỏi đợt?");
    expect(fake.writes).toHaveLength(0);
    fireEvent.click(within(d).getByRole("button", { name: "Bỏ món" }));
    await waitFor(() => {
      expect(fake.writes).toHaveLength(1);
    });
    expect(fake.writes[0]).toMatchObject({ method: "DELETE" });
    expect(fake.writes[0]?.path).toContain(item(1).id);
    expect(fake.writes[0]?.key).toBeTruthy();
  });
});

describe("Lên lịch", () => {
  const openSchedule = async () => {
    await open();
    fireEvent.click(btn("Lên lịch"));
    return dlg('Lên lịch "Đêm 12.12 khung 1"?');
  };

  it("hộp xác nhận nêu số suất bị giữ và từng món; chưa gửi gì trước khi bấm", async () => {
    const d = await openSchedule();
    expect(within(d).getByText(/240 suất của 2 món sẽ bị giữ khỏi bán thường ngay khi bấm/)).toBeTruthy();
    expect(within(d).getByText(/00:00 12\/12/)).toBeTruthy();
    expect(within(d).getByText("SP0001-DEN-M")).toBeTruthy();
    expect(fake.writes).toHaveLength(0);
    expect(within(d).getByRole("button", { name: "Lên lịch, giữ 240 suất" })).toBeTruthy();
  });

  it("nhiều món: nêu ba món đầu, còn lại gộp một dòng", async () => {
    fake.items[S1] = [1, 2, 3, 4, 5].map((i) => item(i, { quantity: 10 * i }));
    const d = await openSchedule();
    expect(within(d).getByText(/150 suất của 5 món/)).toBeTruthy();
    expect(within(d).queryByText("SP0004-DEN-M")).toBeNull();
    expect(within(d).getByText(/và 2 món khác: giữ 90/)).toBeTruthy();
  });

  it("thành công: gửi version đã thấy; đợt thành Đã lên lịch, chỉ còn Thêm suất và Đóng sớm", async () => {
    const d = await openSchedule();
    fireEvent.click(within(d).getByRole("button", { name: "Lên lịch, giữ 240 suất" }));
    await screen.findByText("Đã lên lịch");
    expect(fake.writes[0]).toMatchObject({ method: "POST", body: { version: 3 } });
    expect(fake.writes[0]?.path).toBe(`/api/v1/flash-sales/${S1}/schedule`);
    expect(fake.writes[0]?.key).toBeTruthy();
    for (const n of ["Sửa tên, giờ", "+ Thêm món", "Lên lịch", "Bỏ"]) {
      expect(screen.queryByRole("button", { name: n })).toBeNull();
    }
    expect(btn("Đóng sớm")).toBeTruthy();
    expect((await screen.findAllByRole("button", { name: "Thêm suất" })).length).toBeGreaterThan(0);
    expect(screen.getByText(/không sửa được món, giá, giờ/)).toBeTruthy();
  });

  it("thiếu hàng 2 mã: liệt kê đúng 2 mã hàng với cần, có thể bán, thiếu; đợt vẫn là Nháp", async () => {
    const d = await openSchedule();
    fake.replies.push({
      status: 409,
      body: {
        error: "insufficient_stock",
        request_id: "t",
        shortages: [
          { variantId: "v1", sku: "SM0412-TRANG-S", need: 150, sellable: 96 },
          { variantId: "v2", sku: "QA0920-DEN-S", need: 100, sellable: 72 },
        ],
      },
    });
    fireEvent.click(within(d).getByRole("button", { name: "Lên lịch, giữ 240 suất" }));
    await within(d).findByText("Chưa lên lịch: 2 món không đủ hàng để giữ.");
    const table = within(d).getByRole("table");
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain("SM0412-TRANG-S");
    expect(rows[0]?.textContent).toContain("150");
    expect(rows[0]?.textContent).toContain("96");
    expect(rows[0]?.textContent).toContain("54");
    expect(rows[1]?.textContent).toContain("QA0920-DEN-S");
    expect(rows[1]?.textContent).toContain("28");
    expect(screen.queryByText("Đã lên lịch")).toBeNull();
    expect(screen.getByText("Nháp")).toBeTruthy();
  });

  it("chồng giờ (flash_overlap) nêu lý do và cách xử lý", async () => {
    const d = await openSchedule();
    fake.replies.push({ status: 422, body: { error: "flash_overlap", request_id: "t" } });
    fireEvent.click(within(d).getByRole("button", { name: "Lên lịch, giữ 240 suất" }));
    await within(d).findByText("Chưa lên lịch: một món của đợt đã nằm trong đợt flash sale khác trùng giờ.");
    expect(within(d).getByText(/Bỏ món khỏi một đợt hoặc dời giờ/)).toBeTruthy();
  });

  it("503 rồi tự gửi lại cùng khóa: chỉ giữ suất một lần", async () => {
    const d = await openSchedule();
    fake.replies.push({ status: 503, body: { error: "retry_later", request_id: "t" } });
    fireEvent.click(within(d).getByRole("button", { name: "Lên lịch, giữ 240 suất" }));
    await screen.findByText("Đã lên lịch", undefined, { timeout: 4000 });
    expect(fake.writes).toHaveLength(2);
    expect(fake.writes[0]?.key).toBe(fake.writes[1]?.key);
  });

  it("409 version_conflict: 'Đợt vừa được người khác sửa, tải lại' và Tải lại đọc lại đợt", async () => {
    const d = await openSchedule();
    fake.replies.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    fireEvent.click(within(d).getByRole("button", { name: "Lên lịch, giữ 240 suất" }));
    await within(d).findByText("Đợt vừa được người khác sửa, tải lại.");
    const before = fake.detailCalls;
    fake.sales = [sale({ name: "Tên do người khác sửa", version: 4 })];
    fireEvent.click(within(d).getByRole("button", { name: "Tải lại" }));
    await screen.findByRole("heading", { level: 1, name: /Tên do người khác sửa/ });
    expect(fake.detailCalls).toBeGreaterThan(before);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("đợt không có món → nút Lên lịch khóa kèm lý do", async () => {
    fake.items[S1] = [];
    await open();
    expect(btn("Lên lịch")).toHaveProperty("disabled", true);
    expect(screen.getByText(/Thêm ít nhất một món/)).toBeTruthy();
  });
});

describe("Đợt đang mở", () => {
  const openSale = () => {
    fake.sales = [sale({ status: "open", version: 5, endsAt: "2099-01-01T00:00:00Z" })];
    const [a, b] = fake.items[S1] ?? [];
    if (a === undefined || b === undefined) throw new Error("thiếu món");
    fake.live[S1] = [
      liveItem(a, { available: 26, held: 14, sold: 60, buyers: 70 }),
      liveItem(b, { available: 0, held: 6, sold: 134, buyers: 120 }),
    ];
  };

  const openLive = async () => {
    openSale();
    await open();
    await screen.findByText("Suất đã bán");
  };
  const openSlots = async () => {
    await openLive();
    fireEvent.click(within(rowOf("SP0001-DEN-M")).getByRole("button", { name: "Thêm suất" }));
    return dlg("Thêm suất: SP0001-DEN-M");
  };

  it("số trực tiếp: tổng và từng món; món hết suất tô lỗi; không hiện tên, SĐT khách", async () => {
    await openLive();
    expect(within(kpi("Suất")).getByText("240")).toBeTruthy();
    expect(within(kpi("Còn trống")).getByText("26")).toBeTruthy();
    expect(within(kpi("Suất đang giữ")).getByText("20")).toBeTruthy();
    expect(within(kpi("Suất đã bán")).getByText("194")).toBeTruthy();
    expect(within(kpi("Người mua (cộng theo món)")).getByText("190")).toBeTruthy();
    const row = rowOf("SP0002-DEN-M");
    expect(row.className).toContain("is-err");
    expect(within(row).getByText("134")).toBeTruthy();
    expect(screen.getByText(/tự làm mới mỗi 5 giây/)).toBeTruthy();
  });

  it("báo hiệu flash_sale.changed → số trực tiếp tải lại trong 1 giây", async () => {
    openSale();
    const { qc } = await open();
    await screen.findByText("Suất đã bán");
    const before = fake.liveCalls;
    const [a] = fake.items[S1] ?? [];
    if (a === undefined) throw new Error("thiếu món");
    fake.live[S1] = [liveItem(a, { available: 10, held: 0, sold: 90, buyers: 88 }), ...(fake.live[S1]?.slice(1) ?? [])];
    act(() => {
      onRealtimeChanged(qc, { topic: "flash_sale.changed", entity: { type: "flash_sale", id: S1 } });
    });
    await waitFor(
      () => {
        expect(within(rowOf("SP0001-DEN-M")).getByText("90")).toBeTruthy();
      },
      { timeout: 1000 },
    );
    expect(fake.liveCalls).toBeGreaterThan(before);
  });

  it("đóng sớm cần xác nhận: nêu 3 nhóm suất; bấm Đóng đợt mới gửi", async () => {
    await openLive();
    fireEvent.click(btn("Đóng sớm"));
    const d = await dlg('Đóng sớm "Đêm 12.12 khung 1"?');
    expect(fake.writes).toHaveLength(0);
    expect(within(d).getByText("26 suất còn trống")).toBeTruthy();
    expect(within(d).getByText("20 suất khách đang giữ")).toBeTruthy();
    expect(within(d).getByText(/194 suất đã bán/)).toBeTruthy();
    expect(within(d).getByText(/Không mở lại được/)).toBeTruthy();
    fireEvent.click(within(d).getByRole("button", { name: "Đóng đợt" }));
    await screen.findByText("Đã đóng");
    expect(fake.writes[0]?.path).toBe(`/api/v1/flash-sales/${S1}/close`);
    expect(fake.writes[0]?.body).toEqual({});
    expect(fake.writes[0]?.key).toBeTruthy();
  });

  it("số trực tiếp lỗi tải: báo lỗi có Thử lại (không vẽ số 0); hộp đóng sớm không bịa số, vẫn đóng được", async () => {
    openSale();
    fake.liveFails = true;
    await open();
    await screen.findByText("Chưa tải được số liệu trực tiếp.");
    expect(screen.queryByText("Suất đã bán")).toBeNull();
    fireEvent.click(btn("Đóng sớm"));
    const d = await dlg(/Đóng sớm/);
    expect(within(d).getByText(/Chưa tải được số liệu của đợt/)).toBeTruthy();
    expect(within(d).queryByText(/\d suất còn trống/)).toBeNull();
    fireEvent.click(within(d).getByRole("button", { name: "Đóng đợt" }));
    await screen.findByText("Đã đóng");
    expect(fake.writes[0]?.path).toBe(`/api/v1/flash-sales/${S1}/close`);
  });

  it("Hủy ở hộp đóng sớm không gửi gì", async () => {
    openSale();
    await open();
    fireEvent.click(btn("Đóng sớm"));
    const d = await dlg(/Đóng sớm/);
    fireEvent.click(within(d).getByRole("button", { name: "Hủy" }));
    expect(fake.writes).toHaveLength(0);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("thêm suất: hiện số sau khi thêm, gửi add; thiếu hàng hiện 'cần 600, có thể bán 85' và không thêm", async () => {
    const d = await openSlots();
    type(within(d).getByLabelText("Thêm *"), "600");
    expect(within(d).getByText(/Sau khi thêm: 700 suất/)).toBeTruthy();
    fake.replies.push({
      status: 409,
      body: {
        error: "insufficient_stock",
        request_id: "t",
        shortages: [{ variantId: "v1", sku: "SP0001-DEN-M", need: 600, sellable: 85 }],
      },
    });
    fireEvent.click(within(d).getByRole("button", { name: "Thêm 600 suất" }));
    await within(d).findByText("Không đủ hàng: cần 600, có thể bán 85. Chưa thêm suất nào.");
    type(within(d).getByLabelText("Thêm *"), "50");
    fireEvent.click(within(d).getByRole("button", { name: "Thêm 50 suất" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(fake.writes).toHaveLength(2);
    expect(fake.writes[1]).toMatchObject({ method: "POST", body: { add: 50 } });
    expect(fake.writes[1]?.path).toBe(`/api/v1/flash-sales/${S1}/items/${item(1).id}/add-slots`);
  });

  it("thêm suất ngoài 1–20.000 bị chặn", async () => {
    const d = await openSlots();
    type(within(d).getByLabelText("Thêm *"), "20001");
    expect(within(d).getByText("Từ 1 đến 20.000 suất")).toBeTruthy();
    expect(within(d).getByRole("button", { name: /^Thêm .* suất$/ })).toHaveProperty("disabled", true);
  });

  it("không quyền quản lý: số trực tiếp vẫn hiện, không Đóng sớm, không Thêm suất", async () => {
    openSale();
    fake.grants = grantsOf();
    await open();
    await screen.findByText("Suất đã bán");
    expect(screen.queryByRole("button", { name: "Đóng sớm" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Thêm suất" })).toBeNull();
  });
});

describe("Đợt đã đóng", () => {
  it("chỉ xem: KPI suất, đã bán, đã trả, người mua; không nút ghi", async () => {
    fake.sales = [sale({ status: "closed", closedReason: "manual", version: 9 })];
    const [a, b] = fake.items[S1] ?? [];
    if (a === undefined || b === undefined) throw new Error("thiếu món");
    fake.live[S1] = [
      liveItem(a, { available: 0, sold: 70, released: 30, buyers: 60 }),
      liveItem(b, { available: 0, sold: 78, released: 62, buyers: 71 }),
    ];
    await open();
    await screen.findByText("Đã trả về bán thường");
    expect(within(kpi("Đã bán")).getByText("148")).toBeTruthy();
    expect(within(kpi("Đã trả về bán thường")).getByText("92")).toBeTruthy();
    expect(within(kpi("Người mua (cộng theo món)")).getByText("131")).toBeTruthy();
    expect(screen.getByText(/Đợt đã đóng: chỉ xem/)).toBeTruthy();
    for (const n of ["Đóng sớm", "Thêm suất", "Lên lịch", "Sửa tên, giờ", "+ Thêm món"]) {
      expect(screen.queryByRole("button", { name: n })).toBeNull();
    }
  });
});
