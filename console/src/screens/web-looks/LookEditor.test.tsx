// @vitest-environment jsdom
// Màn Bộ phối (mockup docs/mockups/m5/web-looks.html trạng thái A–D; HV19–HV21). Kế hoạch M5 Task 460 Step 1: bật bộ
// thứ 7 → ô khóa, câu "tối đa 6"; kéo chấm ra mép → kẹp; 409 → tải lại. Thêm: chọn 1–3 sản phẩm, chọn ảnh từ ảnh sản
// phẩm, đặt chấm bằng chạm, tên gợi ý, nhân bản, xóa có xác nhận, xếp thứ tự, chép liên kết /bo-phoi/…, 6 lý do ẩn.
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Look, LookHidden } from "../../api";
import { HIDDEN_TEXT } from "./lookRules";
import {
  A2,
  B1,
  CAT1,
  L,
  P1,
  P2,
  P3,
  P4,
  grantsOf,
  installFake,
  list,
  look,
  looksOf,
  ref,
  renderLooks,
  stubDialog,
  type Fake,
} from "./lookTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const button = (name: string | RegExp) => screen.getByRole("button", { name });
const rows = () => within(screen.getByRole("list", { name: "Danh sách bộ phối" })).getAllByRole("listitem");
const homeBox = (n: number) => screen.getByRole("checkbox", { name: `Trang chủ: bộ ${String(n)}` });
const locked = (el: HTMLElement) =>
  el.closest("fieldset")?.disabled === true ||
  ((el instanceof HTMLInputElement || el instanceof HTMLButtonElement || el instanceof HTMLSelectElement) &&
    el.disabled);
const putBody = () => {
  const w = fake.writes.find((x) => x.method === "PUT");
  if (w === undefined) throw new Error("chưa lưu");
  return w;
};
/** Bấm Lưu, chờ PUT, trả danh sách đã gửi. */
async function save(): Promise<Look[]> {
  fireEvent.click(button("Lưu bộ phối"));
  await waitFor(() => {
    expect(fake.writes.some((w) => w.method === "PUT")).toBe(true);
  });
  return looksOf(putBody().body);
}

/** Mở màn với danh sách l; chờ quyền đọc xong (nút thêm mở khóa). */
async function open(l = fake.list, waitManage = true) {
  fake.list = l;
  const view = renderLooks();
  await screen.findByRole("heading", { name: "Bộ phối", level: 1 });
  if (l.looks.length > 0) await screen.findByRole("list", { name: "Danh sách bộ phối" });
  if (waitManage) {
    await waitFor(() => {
      expect(button("+ Thêm bộ phối")).toHaveProperty("disabled", false);
    });
  }
  return view;
}

/** Khung ảnh rộng 200, cao 250 ở (0, 0). */
function sizeStage(): HTMLElement {
  const stage = screen.getByRole("group", { name: "Khung đặt chấm" });
  vi.spyOn(stage, "getBoundingClientRect").mockReturnValue({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    width: 200,
    height: 250,
    right: 200,
    bottom: 250,
    toJSON: () => ({}),
  });
  return stage;
}

const seven = () => {
  const ls = [1, 2, 3, 4, 5, 6, 7].map((n) => look(n, { onHome: n <= 6 }));
  return list(ls, {
    statuses: list(ls).statuses.map((s, i) =>
      i === 2 ? { ...s, shown: false, reason: "sold-out" as const, productId: P1 } : s,
    ),
  });
};

describe("Danh sách (trạng thái A)", () => {
  it("7 bộ, 6/6 trang chủ: đếm, lý do ẩn kèm tên món; bộ thứ 7 khóa ô Trang chủ, câu tối đa 6", async () => {
    await open(seven());
    expect(rows()).toHaveLength(7);
    expect(screen.getByText("7 bộ · 6/6 bật hiện trên trang chủ · 5 đang hiện")).toBeTruthy();
    expect(rows()[0]?.textContent).toContain("Áo sơ mi lụa tay bồng · Đang hiện");
    expect(rows()[2]?.textContent).toContain("Đang ẩn: một món đã hết hàng mọi size (Áo sơ mi lụa tay bồng)");
    expect(rows()[6]?.textContent).toContain("Không hiện trên trang chủ (liên kết riêng vẫn mở được)");
    expect(homeBox(7)).toHaveProperty("disabled", true);
    expect(homeBox(1)).toHaveProperty("disabled", false);
    expect(screen.getAllByText('Trang chủ hiện tối đa 6 bộ phối. Tắt "Trang chủ" ở một bộ khác trước.').length).toBe(1);
    fireEvent.click(homeBox(2));
    expect(homeBox(7)).toHaveProperty("disabled", false);
    fireEvent.click(homeBox(7));
    expect(homeBox(2)).toHaveProperty("disabled", true);
    expect(button("Đưa bộ 1 lên")).toHaveProperty("disabled", true);
    expect(button("Đưa bộ 7 xuống")).toHaveProperty("disabled", true);
  });

  it("chép liên kết đúng https://elimaz.com.vn/bo-phoi/<tên>-<khóa>, báo đã chép; bộ chưa lưu không chép được", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    await open(list([look(1, { name: "Đi làm ngày thường" })]));
    fireEvent.click(button("Chép liên kết bộ 1"));
    expect(writeText).toHaveBeenCalledWith("https://elimaz.com.vn/bo-phoi/di-lam-ngay-thuong-0195f3b2");
    expect(
      await screen.findByText("Đã chép liên kết bộ phối: elimaz.com.vn/bo-phoi/di-lam-ngay-thuong-0195f3b2"),
    ).toBeTruthy();
    fireEvent.click(button("+ Thêm bộ phối"));
    expect(button("Chép liên kết bộ 2")).toHaveProperty("disabled", true);
  });

  it("tìm theo tên dịp hoặc tên món; đang tìm thì không đổi thứ tự", async () => {
    await open(
      list([look(1, { name: "Đi làm" }), look(2, { name: "Hẹn hò", items: [{ productId: P4, x: 50, y: 45 }] })]),
    );
    fireEvent.change(screen.getByLabelText("Tìm bộ phối"), { target: { value: "đầm suông" } });
    expect(rows()).toHaveLength(1);
    expect(rows()[0]?.textContent).toContain("Hẹn hò");
    expect(button("Đưa bộ 2 lên")).toHaveProperty("disabled", true);
    expect(screen.getByText("Xóa ô tìm để đổi thứ tự.")).toBeTruthy();
  });

  it("0 bộ → khối Chưa có bộ phối nào, vẫn thêm được", async () => {
    await open(list([]));
    expect(screen.getByRole("heading", { name: "Chưa có bộ phối nào" })).toBeTruthy();
    expect(button("+ Thêm bộ phối")).toHaveProperty("disabled", false);
  });

  const reasons = Object.keys(HIDDEN_TEXT).filter((r): r is LookHidden => r in HIDDEN_TEXT);
  it.each(reasons)("lý do ẩn %s: dòng danh sách và khung xem trước đúng chữ", async (reason) => {
    const l = look(1, { onHome: reason !== "disabled" });
    await open(list([l], { statuses: list([l]).statuses.map((s) => ({ ...s, shown: false, reason })) }));
    const pv = screen.getByRole("region", { name: "Xem trước" });
    expect(within(pv).getByText(HIDDEN_TEXT[reason], { exact: false })).toBeTruthy();
    if (reason === "disabled") return;
    const text = HIDDEN_TEXT[reason];
    expect(rows()[0]?.textContent).toContain(`Đang ẩn: ${text.charAt(0).toLowerCase()}${text.slice(1)}`);
    expect(within(pv).getByText("Bộ phối này đang ẩn:")).toBeTruthy();
  });
});

/** Một bộ có hai món: P1 (chấm 50, 45), P2 (chấm 50, 66), ảnh A1. */
const twoItems = () =>
  list([
    look(1, {
      items: [
        { productId: P1, x: 50, y: 45 },
        { productId: P2, x: 50, y: 66 },
      ],
    }),
  ]);

describe("Sửa một bộ (trạng thái B)", () => {
  it("thêm bộ: tên gợi ý, chọn 3 món (màu, ảnh tự chọn), đổi ảnh, tiêu đề tự động, lưu gửi đúng thân", async () => {
    await open(list([]));
    fireEvent.click(button("+ Thêm bộ phối"));
    expect(screen.getByRole("heading", { name: "Bộ 1 · Bộ mới" })).toBeTruthy();
    expect(button("Lưu bộ phối")).toHaveProperty("disabled", true);
    expect(screen.getByText(/Bộ 1 chưa có tên dịp\./)).toBeTruthy();
    fireEvent.click(button("Họp quan trọng"));
    expect(screen.getByLabelText("Tên dịp")).toHaveProperty("value", "Họp quan trọng");
    expect(screen.getByText("14 / 28")).toBeTruthy();
    const search = () => screen.getByLabelText("Tìm sản phẩm");
    fireEvent.change(search(), { target: { value: "sơ mi" } });
    fireEvent.click(await screen.findByRole("button", { name: "Chọn Áo sơ mi lụa tay bồng" }));
    await waitFor(() => {
      expect(screen.getByLabelText("Màu trong ảnh của Áo sơ mi lụa tay bồng")).toHaveProperty("value", "Trắng");
    });
    await waitFor(() => {
      expect(button("Ảnh 1").getAttribute("aria-pressed")).toBe("true");
    });
    fireEvent.change(search(), { target: { value: "chân" } });
    fireEvent.click(await screen.findByRole("button", { name: "Chọn Chân váy bút chì" }));
    fireEvent.change(search(), { target: { value: "giày" } });
    fireEvent.click(await screen.findByRole("button", { name: "Chọn Giày mũi nhọn da bò" }));
    expect(await screen.findByPlaceholderText("Đã đủ 3 sản phẩm — bỏ một món để thêm")).toHaveProperty(
      "disabled",
      true,
    );
    fireEvent.change(screen.getByLabelText("Màu trong ảnh của Áo sơ mi lụa tay bồng"), { target: { value: "Đen" } });
    await waitFor(() => {
      expect(
        within(screen.getByRole("group", { name: "Ảnh của các sản phẩm đã chọn" })).getAllByRole("button"),
      ).toHaveLength(4);
    });
    // Màu Đen đứng trước: Ảnh 1 = A2 (Đen), Ảnh 2 = A1 (Trắng), Ảnh 3 = B1, Ảnh 4 = C1.
    fireEvent.click(button("Ảnh 3"));
    fireEvent.change(screen.getByLabelText("Câu mở đầu"), { target: { value: "Họp sáng thứ Hai mặc gì?" } });
    fireEvent.change(screen.getByLabelText("Người mẫu cao (cm)"), { target: { value: "162" } });
    fireEvent.change(screen.getByLabelText("Đang mặc size"), { target: { value: "S" } });
    fireEvent.change(screen.getByLabelText("Thêm danh mục"), { target: { value: CAT1 } });
    fireEvent.change(screen.getByLabelText("Hiện từ (giờ Việt Nam)"), { target: { value: "2026-12-01T08:00" } });
    await waitFor(() => {
      expect(screen.getByLabelText("Dòng tiêu đề").getAttribute("placeholder")).toBe(
        "Tự động: Áo sơ mi lụa tay bồng, Chân váy bút chì, Giày mũi nhọn da bò",
      );
    });
    expect(button("Lưu bộ phối")).toHaveProperty("disabled", false);
    const [sent] = await save();
    expect(putBody().body.revision).toBe(4);
    expect(putBody().key).toMatch(/^[0-9a-f-]{36}$/);
    expect(sent).toMatchObject({
      onHome: false,
      name: "Họp quan trọng",
      photoFileId: B1,
      headline: "Họp sáng thứ Hai mặc gì?",
      modelHeightCm: 162,
      modelSize: "S",
      categoryIds: [CAT1],
      startsAt: "2026-12-01T01:00:00.000Z",
      items: [
        { productId: P1, color: "Đen", x: 50, y: 45 },
        { productId: P2, color: "Đen", x: 50, y: 66 },
        { productId: P3, color: "Kem", x: 50, y: 75 },
      ],
    });
    await screen.findByText("Đã lưu bộ phối.");
  });

  it("kéo chấm ra ngoài mép phải dưới → kẹp ở 97 %, câu giải thích; chạm ảnh đặt chấm đang chọn", async () => {
    await open(twoItems());
    const stage = sizeStage();
    const dot1 = button("Chấm 1: Áo sơ mi lụa tay bồng");
    fireEvent.pointerDown(dot1, { pointerId: 1, clientX: 100, clientY: 100 });
    fireEvent.pointerMove(stage, { pointerId: 1, clientX: 400, clientY: 900 });
    fireEvent.pointerUp(stage, { pointerId: 1 });
    expect(dot1.style.left).toBe("97%");
    expect(dot1.style.top).toBe("97%");
    expect(screen.getByText("Chấm 1 kéo ra mép: giữ ở 3–97% để khách vẫn bấm được.")).toBeTruthy();
    // Sau khi thả, rê chuột không dời chấm nữa.
    fireEvent.pointerMove(stage, { pointerId: 1, clientX: 10, clientY: 10 });
    expect(dot1.style.left).toBe("97%");
    // Chọn chấm 2 rồi chạm ảnh.
    fireEvent.click(button("Chấm 2: Chân váy bút chì"));
    fireEvent.pointerDown(stage, { pointerId: 2, clientX: 40, clientY: 200 });
    fireEvent.pointerUp(stage, { pointerId: 2 });
    // Phím mũi tên dời chấm đang chọn 1 %.
    fireEvent.keyDown(button("Chấm 2: Chân váy bút chì"), { key: "ArrowRight" });
    const [sent] = await save();
    expect(sent?.items).toEqual([
      { productId: P1, x: 97, y: 97 },
      { productId: P2, x: 21, y: 80 },
    ]);
  });

  it("chọn mẫu rồi bấm Bỏ thay đổi trước khi đọc xong chi tiết mẫu → món không được thêm lại", async () => {
    let releaseProduct: () => void = () => undefined;
    const held = new Promise<void>((r) => {
      releaseProduct = r;
    });
    const orig = globalThis.fetch;
    const asked: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const path = new URL(req.url).pathname;
        asked.push(path);
        if (path === `/api/v1/catalog/products/${P3}`) await held;
        return orig(req);
      }),
    );
    const { qc } = await open(list([look(1)]));
    fireEvent.click(homeBox(1));
    fireEvent.change(screen.getByLabelText("Tìm sản phẩm"), { target: { value: "giày" } });
    fireEvent.click(await screen.findByRole("button", { name: "Chọn Giày mũi nhọn da bò" }));
    fireEvent.click(button("Bỏ thay đổi"));
    releaseProduct();
    // Chờ theo sự kiện: chi tiết mẫu đã vào bộ nhớ đệm (waitFor kiểm ở lượt sau, khi pickProduct đã chạy tiếp xong).
    await waitFor(() => {
      expect(qc.getQueryData(["catalog.product", P3, "look"])).toBeDefined();
    });
    expect(asked).toContain(`/api/v1/catalog/products/${P3}`);
    expect(screen.queryByRole("button", { name: "Bỏ Giày mũi nhọn da bò" })).toBeNull();
    expect(screen.queryByText("Có thay đổi chưa lưu")).toBeNull();
  });

  it("bỏ món đang có ảnh → ảnh chuyển sang ảnh của món còn lại", async () => {
    await open(twoItems());
    fireEvent.click(button("Bỏ Áo sơ mi lụa tay bồng"));
    const [sent] = await save();
    expect(sent).toMatchObject({ photoFileId: B1, items: [{ productId: P2, x: 50, y: 66 }] });
  });

  it("nhân bản: bản sao ngay sau, không lên trang chủ; xóa hỏi xác nhận kèm liên kết sẽ không mở được", async () => {
    await open(list([look(1, { name: "Tiệc cuối năm công ty" }), look(2)]));
    fireEvent.click(button("Nhân bản"));
    expect(rows()).toHaveLength(3);
    expect(screen.getByRole("heading", { name: "Bộ 2 · Tiệc cuối năm công ty" })).toBeTruthy();
    expect(homeBox(2)).toHaveProperty("checked", false);
    fireEvent.click(within(rows()[0] ?? document.body).getByRole("button", { name: /Tiệc cuối năm công ty/ }));
    fireEvent.click(button("Xóa"));
    const dlg = screen.getByRole("dialog", { name: 'Xóa bộ "Tiệc cuối năm công ty"?' });
    expect(dlg.textContent).toContain("elimaz.com.vn/bo-phoi/tiec-cuoi-nam-cong-ty-0195f3b2");
    expect(dlg.textContent).toContain("sẽ không mở được nữa");
    fireEvent.click(within(dlg).getByRole("button", { name: "Hủy" }));
    expect(rows()).toHaveLength(3);
    fireEvent.click(button("Xóa"));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Xóa bộ phối" }));
    expect(rows()).toHaveLength(2);
    fireEvent.click(button("Đưa bộ 2 lên"));
    const sent = await save();
    expect(sent.map((l) => l.id)).toEqual([L(2), sent[1]?.id]);
    expect(sent[1]).toMatchObject({ name: "Tiệc cuối năm công ty", onHome: false });
    expect(sent[1]?.id).not.toBe(L(1));
  });

  it("đủ 500 bộ → nút thêm và Nhân bản khóa, câu tối đa 500", async () => {
    // 500 dòng: tìm theo chữ thay vì theo vai trò (tính tên truy cập của 2.500 nút rất chậm trên jsdom).
    await open(list(Array.from({ length: 500 }, (_, i) => look(i + 1, { onHome: false }))), false);
    const byText = (t: string) => screen.getByText(t, { selector: "button" });
    await waitFor(() => {
      expect(byText("Xóa")).toHaveProperty("disabled", false);
    });
    expect(byText("+ Thêm bộ phối")).toHaveProperty("disabled", true);
    expect(byText("Nhân bản")).toHaveProperty("disabled", true);
    expect(screen.getByText("Đã đủ 500 bộ phối. Xóa một bộ để thêm bộ mới.")).toBeTruthy();
  });
});

describe("Xem trước (trạng thái C)", () => {
  it("sửa → previewLooks chỉ gửi bộ đủ thông tin; trạng thái trả về hiện ở danh sách; đổi thiết bị", async () => {
    fake.previewReply = (looks) => ({
      statuses: looks.map((l) => ({
        id: l.id,
        shown: false,
        reason: "product-missing",
        productId: P1,
        autoTitle: "x",
        linkKey: "0195f3b2",
        path: "/bo-phoi/x-0195f3b2",
      })),
      products: [{ ...ref(P1), onSale: false }],
      photos: [],
    });
    await open(list([look(1)]));
    fireEvent.click(button("+ Thêm bộ phối"));
    fireEvent.click(homeBox(1));
    await waitFor(() => {
      expect(rows()[0]?.textContent).toContain("Đang ẩn: một món đã ngừng bán (Áo sơ mi lụa tay bồng)");
    });
    expect(fake.previews.at(-1)).toMatchObject({ looks: [{ id: L(1) }] });
    expect(looksOf(fake.previews.at(-1) ?? {})).toHaveLength(1);
    expect(rows()[1]?.textContent).toContain("Chưa đủ để hiện");
    fireEvent.click(within(rows()[0] ?? document.body).getByRole("button", { name: /Bộ 1/ }));
    const pv = screen.getByRole("region", { name: "Xem trước" });
    expect(within(pv).getByText("Bộ phối này đang ẩn:")).toBeTruthy();
    fireEvent.click(button("Điện thoại"));
    expect(button("Điện thoại").getAttribute("aria-pressed")).toBe("true");
  });

  it("bộ đang hiện: ảnh đã chọn, chấm, câu mở đầu mặc định, dòng người mẫu, tên món", async () => {
    await open(list([look(1, { name: "Đi làm", modelHeightCm: 162, modelSize: "S", photoFileId: A2 })]));
    const pv = screen.getByRole("region", { name: "Xem trước" });
    expect(within(pv).getByText("Mặc gì cho Đi làm?")).toBeTruthy();
    expect(within(pv).getByText("Mẫu 1m62 · size S")).toBeTruthy();
    expect(within(pv).getByText("Áo sơ mi lụa tay bồng")).toBeTruthy();
    expect(pv.querySelector("img")?.getAttribute("src")).toBe(`/img/${A2}-320.webp`);
    expect(pv.querySelectorAll(".lk-dot")).toHaveLength(1);
  });
});

describe("Lưu (trạng thái D)", () => {
  /** Lần lưu kế tiếp trả 409; bấm Lưu, chờ hộp người khác vừa lưu. */
  async function saveConflict() {
    fake.saveErrors.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    fireEvent.click(button("Lưu bộ phối"));
    return screen.findByRole("dialog", { name: "Bộ phối vừa được người khác lưu" });
  }

  it("409 → hộp người khác vừa lưu; Giữ bản đang sửa giữ bản; Tải bản mới nạp bản máy chủ", async () => {
    await open(list([look(1), look(2)]));
    fireEvent.click(button("Xóa"));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Xóa bộ phối" }));
    fake.list = list([look(1), look(2), look(9)], { revision: 5 });
    fireEvent.click(within(await saveConflict()).getByRole("button", { name: "Giữ bản đang sửa" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(rows().map((r) => r.textContent.includes("Bộ 2"))).toEqual([true]);
    expect(putBody().body.revision).toBe(4);
    fireEvent.click(within(await saveConflict()).getByRole("button", { name: "Tải bản mới" }));
    await waitFor(() => {
      expect(rows()).toHaveLength(3);
    });
    expect(rows()[2]?.textContent).toContain("Bộ 9");
    expect(screen.queryByText("Có thay đổi chưa lưu")).toBeNull();
  });

  it("người khác lưu trong lúc đang sửa: giữ bản sửa; Bỏ thay đổi nạp bản mới nhất", async () => {
    const { qc } = await open(list([look(1)]));
    fireEvent.click(homeBox(1));
    fake.list = list([look(1), look(7)], { revision: 5 });
    await qc.invalidateQueries({ queryKey: ["content.looks"] });
    expect(rows()).toHaveLength(1);
    fireEvent.click(button("Bỏ thay đổi"));
    await waitFor(() => {
      expect(rows()).toHaveLength(2);
    });
  });

  it("đang lưu: khóa ô Trang chủ, lên/xuống, thêm, nhân bản, sửa, chấm, bỏ thay đổi", async () => {
    let release: () => void = () => undefined;
    fake.hold = new Promise<void>((r) => {
      release = r;
    });
    await open(list([look(1), look(2)]));
    fireEvent.click(homeBox(2));
    fireEvent.click(button("Lưu bộ phối"));
    await screen.findByRole("button", { name: "Đang lưu…" });
    expect(locked(homeBox(1))).toBe(true);
    expect(locked(button("Đưa bộ 1 xuống"))).toBe(true);
    expect(locked(button("+ Thêm bộ phối"))).toBe(true);
    expect(locked(button("Nhân bản"))).toBe(true);
    expect(locked(screen.getByLabelText("Tên dịp"))).toBe(true);
    expect(locked(button("Chấm 1: Áo sơ mi lụa tay bồng"))).toBe(true);
    expect(locked(button("Bỏ thay đổi"))).toBe(true);
    const stage = sizeStage();
    // Chạm ảnh, kéo, phím mũi tên lúc đang lưu không dời chấm (kiểm trước khi lưu xong: lưu xong nạp lại bản máy chủ).
    const dot1 = button("Chấm 1: Áo sơ mi lụa tay bồng");
    fireEvent.pointerDown(stage, { pointerId: 1, clientX: 10, clientY: 10 });
    fireEvent.pointerMove(stage, { pointerId: 1, clientX: 180, clientY: 200 });
    fireEvent.pointerUp(stage, { pointerId: 1 });
    fireEvent.keyDown(dot1, { key: "ArrowLeft" });
    expect(dot1.style.left).toBe("50%");
    expect(dot1.style.top).toBe("45%");
    release();
    await screen.findByText("Đã lưu bộ phối.");
  });

  it("chỉ content.read → câu chỉ xem, ô và nút sửa khóa, vẫn chép liên kết", async () => {
    fake.grants = grantsOf();
    await open(list([look(1)]), false);
    await screen.findByText(/Bạn chỉ có quyền xem nội dung website/);
    expect(locked(homeBox(1))).toBe(true);
    expect(locked(button("+ Thêm bộ phối"))).toBe(true);
    expect(locked(button("Xóa"))).toBe(true);
    expect(locked(screen.getByLabelText("Tên dịp"))).toBe(true);
    expect(button("Chép liên kết bộ 1")).toHaveProperty("disabled", false);
  });

  it("lỗi 422 của máy chủ hiện câu tiếng Việt, giữ bản đang sửa", async () => {
    await open(list([look(1)]));
    fireEvent.click(homeBox(1));
    fake.saveErrors.push({ status: 422, body: { error: "look_photo_invalid", request_id: "t" } });
    fireEvent.click(button("Lưu bộ phối"));
    await screen.findByText("Ảnh người mẫu phải là một ảnh của sản phẩm trong bộ. Chọn lại ảnh");
    expect(button("Lưu bộ phối")).toHaveProperty("disabled", false);
  });
});
