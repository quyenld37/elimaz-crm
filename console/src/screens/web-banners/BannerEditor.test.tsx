// @vitest-environment jsdom
// Màn Banner trang chủ (mockup docs/mockups/m5/web-banners.html trạng thái A–E; HV18, HV19). Kế hoạch M5 Task 459
// Step 1: 8 banner → nút thêm khóa; ảnh đang xử lý → nút lưu khóa kèm lý do; mỗi lý do ẩn đúng chữ; 409 → hộp tải lại,
// không mất bản đang sửa; đang lưu khóa mọi thao tác (kể cả kéo thả).
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HomeBannerHidden } from "../../api";
import { HIDDEN_TEXT } from "./bannerRules";
import {
  B,
  CAT1,
  F_MOB,
  F_NEW,
  P1,
  P3,
  grantsOf,
  imageBanner,
  installFake,
  jpg,
  list,
  promo,
  renderBanners,
  stubDialog,
  type Fake,
} from "./bannerTestkit";

let fake: Fake;
beforeEach(() => {
  stubDialog();
  fake = installFake();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const button = (name: string | RegExp) => screen.getByRole("button", { name });
const rows = () => within(screen.getByRole("list", { name: "Banner trên trang chủ" })).getAllByRole("listitem");
/** Ô bị khóa: chính ô hay fieldset bao nó (jsdom không đổi thuộc tính disabled của ô trong fieldset khóa). */
const locked = (el: HTMLElement) =>
  el.closest("fieldset")?.disabled === true ||
  ((el instanceof HTMLInputElement || el instanceof HTMLButtonElement || el instanceof HTMLSelectElement) &&
    el.disabled);
const saveBody = () => {
  const w = fake.writes.find((x) => x.method === "PUT");
  if (w === undefined) throw new Error("chưa lưu");
  return w;
};
/** Bấm Lưu, chờ yêu cầu PUT, trả banner đầu của thân đã gửi. */
async function saveFirst(): Promise<unknown> {
  fireEvent.click(button("Lưu banner"));
  await waitFor(() => {
    expect(fake.writes.some((w) => w.method === "PUT")).toBe(true);
  });
  const bs = saveBody().body.banners;
  return Array.isArray(bs) ? bs[0] : null;
}
const bannerIds = () => {
  const bs = saveBody().body.banners;
  return Array.isArray(bs) ? bs.map((b: { id: string }) => b.id) : [];
};

/** Mở màn với danh sách l; chờ quyền đọc xong (ô bật của banner 1, hay nút thêm khi chưa có banner, mở khóa). */
async function open(l = fake.list, waitManage = true) {
  fake.list = l;
  const view = renderBanners();
  await screen.findByRole("list", { name: "Banner trên trang chủ" });
  if (!waitManage) return view;
  await waitFor(() => {
    const el =
      l.banners.length === 0 ? button("+ Thêm banner") : screen.getByRole("checkbox", { name: "Bật banner 1" });
    expect(el).toHaveProperty("disabled", false);
  });
  return view;
}

describe("Danh sách (trạng thái A)", () => {
  it("7 banner: thứ tự, loại, dòng hiện/ẩn kèm lý do, đếm", async () => {
    const bs = [1, 2, 3, 4, 5, 6, 7].map((n) => (n === 2 ? imageBanner(2) : promo(n)));
    await open(
      list(bs, {
        statuses: bs.map((b, i) =>
          i === 1
            ? { id: b.id, shown: false, reason: "scheduled" }
            : i === 2
              ? { id: b.id, shown: false, reason: "campaign-not-running" }
              : i === 6
                ? { id: b.id, shown: false, reason: "disabled" }
                : { id: b.id, shown: true },
        ),
      }),
    );
    expect(rows()).toHaveLength(7);
    expect(screen.getByText("4 đang hiện · 3 đang ẩn")).toBeTruthy();
    expect(rows()[0]?.textContent).toContain("Banner 1");
    expect(rows()[0]?.textContent).toContain("Giảm giá · Đang hiện");
    expect(rows()[1]?.textContent).toContain("Black Friday 27.11");
    expect(rows()[1]?.textContent).toContain("Ảnh thiết kế · Đang ẩn: chưa tới giờ hiện");
    expect(rows()[2]?.textContent).toContain("Đang ẩn: chiến dịch chưa áp dụng");
    expect(button("Đưa banner 1 lên")).toHaveProperty("disabled", true);
    expect(button("Đưa banner 7 xuống")).toHaveProperty("disabled", true);
  });

  const reasons = Object.keys(HIDDEN_TEXT).filter((r): r is HomeBannerHidden => r in HIDDEN_TEXT);
  it.each(reasons)("lý do ẩn %s: dòng danh sách và khung xem trước đúng chữ", async (reason) => {
    await open(list([promo(1)], { statuses: [{ id: B(1), shown: false, reason }] }));
    const text = HIDDEN_TEXT[reason];
    expect(rows()[0]?.textContent).toContain(`Đang ẩn: ${text.charAt(0).toLowerCase()}${text.slice(1)}`);
    const pv = screen.getByRole("region", { name: "Xem trước" });
    expect(within(pv).getByText("Banner này đang ẩn")).toBeTruthy();
    expect(within(pv).getByText(text)).toBeTruthy();
  });

  it("0 banner → khối Chưa có banner, vẫn thêm được", async () => {
    await open(list([]));
    expect(screen.getByRole("heading", { name: "Chưa có banner" })).toBeTruthy();
    expect(button("+ Thêm banner")).toHaveProperty("disabled", false);
  });
});

describe("Sửa danh sách", () => {
  it("8 banner → ô loại và nút thêm khóa, câu Đã đủ 8", async () => {
    await open(list([1, 2, 3, 4, 5, 6, 7, 8].map((n) => promo(n))));
    expect(button("+ Thêm banner")).toHaveProperty("disabled", true);
    expect(screen.getByLabelText("Loại banner mới")).toHaveProperty("disabled", true);
    expect(screen.getByText("Đã đủ 8 banner. Xóa một banner để thêm banner mới.")).toBeTruthy();
    expect(button("Nhân bản")).toHaveProperty("disabled", true);
  });

  it("thêm Xu hướng, bật/tắt, lên, nhân bản (bản sao tắt), xóa → lưu gửi revision và đúng danh sách", async () => {
    await open(list([promo(1), promo(2)]));
    expect(screen.queryByRole("button", { name: "Lưu banner" })).toBeNull();
    fireEvent.change(screen.getByLabelText("Loại banner mới"), { target: { value: "trending" } });
    fireEvent.click(button("+ Thêm banner"));
    expect(rows()).toHaveLength(3);
    expect(screen.getByRole("heading", { name: "Banner 3 · Xu hướng" })).toBeTruthy();
    fireEvent.click(screen.getByRole("checkbox", { name: "Bật banner 1" }));
    fireEvent.click(button("Đưa banner 2 lên"));
    fireEvent.click(within(rows()[0] ?? document.body).getByRole("button", { name: /Banner 2/ }));
    fireEvent.click(button("Nhân bản"));
    expect(rows()).toHaveLength(4);
    fireEvent.click(within(rows()[2] ?? document.body).getByRole("button", { name: /Banner 1/ }));
    fireEvent.click(button("Xóa"));
    expect(rows()).toHaveLength(3);
    fireEvent.click(button("Lưu banner"));
    await waitFor(() => {
      expect(fake.writes.some((w) => w.method === "PUT")).toBe(true);
    });
    const body = saveBody().body;
    expect(body.revision).toBe(4);
    expect(saveBody().key).toMatch(/^[0-9a-f-]{36}$/);
    const bs = Array.isArray(body.banners) ? body.banners : [];
    expect(bs).toHaveLength(3);
    expect(bs[0]).toMatchObject({ id: B(2), enabled: true });
    expect(bs[1]).toMatchObject({ kind: "campaign", title: "Banner 2", enabled: false });
    expect(bs[2]).toMatchObject({ kind: "trending", enabled: true, theme: "auto" });
    await screen.findByText("Đã lưu danh sách banner.");
    expect(screen.queryByRole("button", { name: "Lưu banner" })).toBeNull();
  });

  it("Bỏ thay đổi trả về bản đã lưu", async () => {
    await open(list([promo(1)]));
    fireEvent.click(button("Xóa"));
    expect(screen.getByRole("heading", { name: "Chưa có banner" })).toBeTruthy();
    fireEvent.click(button("Bỏ thay đổi"));
    expect(rows()).toHaveLength(1);
  });

  it("người khác lưu trong lúc đang sửa: giữ bản sửa; Bỏ thay đổi nạp bản mới nhất của máy chủ", async () => {
    const { qc } = await open(list([promo(1)]));
    fireEvent.click(button("Xóa"));
    fake.list = list([promo(1), promo(7)], { revision: 5 });
    await qc.invalidateQueries({ queryKey: ["content.banners"] });
    await waitFor(() => {
      expect(fake.writes).toHaveLength(0);
    });
    expect(screen.getByRole("heading", { name: "Chưa có banner" })).toBeTruthy();
    fireEvent.click(button("Bỏ thay đổi"));
    expect(rows()).toHaveLength(2);
    expect(rows()[1]?.textContent).toContain("Banner 7");
    expect(screen.queryByRole("button", { name: "Lưu banner" })).toBeNull();
  });

  it("sửa rồi hoàn tác tay sau khi người khác lưu → màn nạp bản mới nhất, không vẽ bản cũ", async () => {
    const { qc } = await open(list([promo(1)]));
    fireEvent.click(screen.getByRole("checkbox", { name: "Bật banner 1" }));
    fake.list = list([promo(1), promo(7)], { revision: 5 });
    await qc.invalidateQueries({ queryKey: ["content.banners"] });
    expect(rows()).toHaveLength(1);
    fireEvent.click(screen.getByRole("checkbox", { name: "Bật banner 1" }));
    await waitFor(() => {
      expect(rows()).toHaveLength(2);
    });
    expect(rows()[1]?.textContent).toContain("Banner 7");
    expect(screen.queryByRole("button", { name: "Lưu banner" })).toBeNull();
  });

  it("bản mới từ máy chủ bỏ mất banner đang chọn, hay danh sách đầu rỗng → form hiện banner đầu của bản mới", async () => {
    const { qc } = await open(list([promo(1), promo(2)]));
    fireEvent.click(within(rows()[1] ?? document.body).getByRole("button", { name: /Banner 2/ }));
    expect(screen.getByRole("heading", { name: "Banner 2 · Giảm giá" })).toBeTruthy();
    fake.list = list([promo(1)], { revision: 5 });
    await qc.invalidateQueries({ queryKey: ["content.banners"] });
    await waitFor(() => {
      expect(rows()).toHaveLength(1);
    });
    expect(screen.getByRole("heading", { name: "Banner 1 · Giảm giá" })).toBeTruthy();
    fake.list = list([], { revision: 6 });
    await qc.invalidateQueries({ queryKey: ["content.banners"] });
    await screen.findByRole("heading", { name: "Chưa có banner" });
    fake.list = list([promo(8)], { revision: 7 });
    await qc.invalidateQueries({ queryKey: ["content.banners"] });
    expect(await screen.findByRole("heading", { name: "Banner 1 · Giảm giá" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Chưa có banner" })).toBeNull();
  });

  it("ghim tối đa 3 mẫu thuộc chiến dịch; Theo thời gian chiến dịch chép giờ; chữ có bộ đếm", async () => {
    await open(list([promo(1)]));
    fireEvent.change(screen.getByLabelText("Tìm sản phẩm để ghim"), { target: { value: "á" } });
    fireEvent.click(await screen.findByRole("button", { name: "Ghim Áo khoác dạ dáng dài" }));
    fireEvent.click(await screen.findByRole("button", { name: "Ghim Áo len cổ lọ" }));
    fireEvent.change(screen.getByLabelText("Tìm sản phẩm để ghim"), { target: { value: "chân" } });
    fireEvent.click(await screen.findByRole("button", { name: "Ghim Chân váy dạ xếp ly" }));
    expect(screen.getByText("Đã ghim đủ 3 sản phẩm. Chỉ ghim được sản phẩm thuộc nguồn của banner.")).toBeTruthy();
    expect(screen.queryByLabelText("Tìm sản phẩm để ghim")).toBeNull();
    fireEvent.click(button("Bỏ ghim Áo len cổ lọ"));
    expect(screen.getByLabelText("Tìm sản phẩm để ghim")).toBeTruthy();
    fireEvent.click(button("Theo thời gian chiến dịch"));
    expect(screen.getByLabelText("Hiện từ (giờ Việt Nam)")).toHaveProperty("value", "2026-11-15T00:00");
    expect(screen.getByLabelText("Hiện đến (giờ Việt Nam)")).toHaveProperty("value", "2026-11-25T23:59");
    fireEvent.change(screen.getByLabelText("Tiêu đề (dấu phẩy để xuống dòng)"), {
      target: { value: "Xả hàng thu đông, chỉ còn 10 ngày" },
    });
    expect(screen.getByText("33 / 60")).toBeTruthy();
    expect(await saveFirst()).toMatchObject({
      productIds: [P1, P3],
      startsAt: "2026-11-14T17:00:00Z",
      endsAt: "2026-11-25T16:59:00Z",
      title: "Xả hàng thu đông, chỉ còn 10 ngày",
    });
  });

  it("banner danh mục: tìm mẫu lọc theo danh mục của banner", async () => {
    const calls: string[] = [];
    const orig = globalThis.fetch;
    vi.stubGlobal(
      "fetch",
      vi.fn((req: Request) => {
        if (new URL(req.url).pathname === "/api/v1/catalog/products") calls.push(new URL(req.url).search);
        return orig(req);
      }),
    );
    await open(list([promo(1, { kind: "category", campaignId: undefined, categoryId: CAT1 })]));
    fireEvent.change(screen.getByLabelText("Tìm sản phẩm để ghim"), { target: { value: "đầm" } });
    await screen.findByRole("button", { name: "Ghim Đầm len ôm" });
    expect(calls.some((q) => q.includes(`categoryId=${CAT1}`) && q.includes("status=active"))).toBe(true);
  });
});

describe("Ảnh (HV18, trạng thái A, C)", () => {
  it("ảnh điện thoại đã lưu đang xử lý → Lưu khóa kèm lý do", async () => {
    const l = list([promo(1), imageBanner(2)]);
    l.images = l.images.map((i) => (i.fileId === F_MOB ? { ...i, status: "processing" } : i));
    await open(l);
    fireEvent.click(screen.getByRole("checkbox", { name: "Bật banner 1" }));
    expect(button("Lưu banner")).toHaveProperty("disabled", true);
    expect(screen.getByText("Ảnh điện thoại của banner 2 đang xử lý, chờ xong mới lưu được.")).toBeTruthy();
  });

  it("tải ảnh: đang xử lý → Lưu khóa; xong → Sẵn sàng, cảnh báo lệch tỉ lệ, Lưu gửi mã tệp mới", async () => {
    fake.fileStatus = "processing";
    await open(list([imageBanner(1, { imageDesktop: undefined, imageMobile: undefined })]));
    fireEvent.change(screen.getByLabelText("Chọn ảnh cho máy tính"), { target: { files: [jpg("giang-sinh-pc.jpg")] } });
    await screen.findByText("Đang xử lý ảnh");
    expect(button("Lưu banner")).toHaveProperty("disabled", true);
    expect(screen.getByText("Ảnh máy tính của banner 1 đang xử lý, chờ xong mới lưu được.")).toBeTruthy();
    fake.fileStatus = "ready";
    await screen.findByText("Sẵn sàng");
    expect(await screen.findByText("khác tỉ lệ 2368 × 600: khoảng 19% ảnh ở trên và dưới sẽ bị cắt")).toBeTruthy();
    expect(button("Lưu banner")).toHaveProperty("disabled", false);
    const create = fake.writes.find((w) => w.path === "/api/v1/files/uploads");
    expect(create?.body).toMatchObject({ purpose: "web_banner", fileName: "giang-sinh-pc.jpg" });
    expect(await saveFirst()).toMatchObject({ imageDesktop: F_NEW });
  });

  it("ảnh bị từ chối vì quá nhỏ → câu lý do, Lưu khóa; Bỏ ảnh mở lại ô chọn", async () => {
    fake.fileStatus = "rejected";
    fake.rejectReason = "too_small";
    await open(list([imageBanner(1, { imageMobile: undefined })]));
    fireEvent.change(screen.getByLabelText("Chọn ảnh cho điện thoại"), { target: { files: [jpg("nho.jpg")] } });
    await screen.findByText("Ảnh quá nhỏ: cần rộng ít nhất 600 px. Xuất lại ảnh rồi tải lên");
    expect(button("Lưu banner")).toHaveProperty("disabled", true);
    expect(screen.getByText("Ảnh điện thoại của banner 1 bị từ chối. Tải ảnh khác hoặc bỏ ảnh.")).toBeTruthy();
    fireEvent.click(button("Bỏ ảnh điện thoại"));
    expect(screen.getByLabelText("Chọn ảnh cho điện thoại")).toBeTruthy();
  });

  it("tệp quá 8 MB hay không phải ảnh → báo ngay, không tải", async () => {
    await open(list([imageBanner(1, { imageMobile: undefined })]));
    fireEvent.change(screen.getByLabelText("Chọn ảnh cho điện thoại"), {
      target: { files: [jpg("to.jpg", (8 << 20) + 1)] },
    });
    expect(screen.getByText("Ảnh quá 8 MB. Xuất lại ảnh nhỏ hơn rồi tải lên")).toBeTruthy();
    expect(fake.writes.some((w) => w.path === "/api/v1/files/uploads")).toBe(false);
  });
});

describe("Xem trước (trạng thái D)", () => {
  it("sửa xong 350 ms → previewBanners với danh sách đang sửa; trạng thái trả về hiện ở danh sách", async () => {
    fake.previewReply = () => ({ statuses: [{ id: B(1), shown: false, reason: "disabled" }] });
    await open(list([promo(1)]));
    fireEvent.click(screen.getByRole("checkbox", { name: "Bật banner 1" }));
    await waitFor(() => {
      expect(rows()[0]?.textContent).toContain("Đang ẩn: đang tắt");
    });
    expect(fake.previews).toHaveLength(1);
    expect(fake.previews[0]).toMatchObject({ banners: [{ id: B(1), enabled: false }] });
    fireEvent.click(button("Điện thoại"));
    expect(button("Điện thoại").getAttribute("aria-pressed")).toBe("true");
  });
});

describe("Lưu (trạng thái E)", () => {
  it("409 → hộp người khác vừa lưu; Giữ bản đang sửa giữ bản; Tải bản mới nạp bản máy chủ", async () => {
    await open(list([promo(1), promo(2)]));
    fireEvent.click(button("Xóa"));
    fake.saveErrors.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    fake.list = list([promo(1), promo(2), promo(9)], { revision: 5 });
    fireEvent.click(button("Lưu banner"));
    const dlg = await screen.findByRole("dialog", { name: "Banner vừa được người khác lưu" });
    fireEvent.click(within(dlg).getByRole("button", { name: "Giữ bản đang sửa" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(rows()).toHaveLength(1);
    expect(button("Lưu banner")).toBeTruthy();
    fake.saveErrors.push({ status: 409, body: { error: "version_conflict", request_id: "t" } });
    fireEvent.click(button("Lưu banner"));
    const again = await screen.findByRole("dialog", { name: "Banner vừa được người khác lưu" });
    fireEvent.click(within(again).getByRole("button", { name: "Tải bản mới" }));
    await waitFor(() => {
      expect(rows()).toHaveLength(3);
    });
    expect(screen.queryByRole("button", { name: "Lưu banner" })).toBeNull();
  });

  it("đang lưu: khóa bật/tắt, lên/xuống, thêm, sửa, bỏ thay đổi, kéo thả", async () => {
    let release: () => void = () => undefined;
    fake.hold = new Promise<void>((r) => {
      release = r;
    });
    await open(list([promo(1), promo(2)]));
    fireEvent.click(screen.getByRole("checkbox", { name: "Bật banner 2" }));
    fireEvent.click(button("Lưu banner"));
    await screen.findByRole("button", { name: "Đang lưu…" });
    expect(locked(screen.getByRole("checkbox", { name: "Bật banner 1" }))).toBe(true);
    expect(locked(button("Đưa banner 1 xuống"))).toBe(true);
    expect(locked(button("+ Thêm banner"))).toBe(true);
    expect(locked(button("Nhân bản"))).toBe(true);
    expect(locked(button("Bỏ thay đổi"))).toBe(true);
    expect(screen.getByText("Trong lúc lưu không bật/tắt, đổi thứ tự, sửa hay kéo thả được.")).toBeTruthy();
    const first = rows()[0];
    expect(first?.getAttribute("draggable")).toBe("false");
    // Kéo thả lọt vào (trình duyệt cũ) vẫn bị bỏ qua.
    if (first !== undefined && rows()[1] !== undefined) {
      fireEvent.dragStart(first);
      fireEvent.drop(rows()[1] ?? first);
    }
    release();
    await screen.findByText("Đã lưu danh sách banner.");
    expect(bannerIds()).toEqual([B(1), B(2)]);
    expect(rows()[0]?.textContent).toContain("Banner 1");
  });

  it("kéo thả đổi thứ tự khi không lưu", async () => {
    await open(list([promo(1), promo(2), promo(3)]));
    const [a, , c] = rows();
    if (a === undefined || c === undefined) throw new Error("thiếu dòng");
    fireEvent.dragStart(a);
    fireEvent.dragOver(c);
    fireEvent.drop(c);
    expect(rows().map((r) => r.textContent.includes("Banner 1"))).toEqual([false, false, true]);
  });

  it("chỉ content.read → câu chỉ xem, mọi ô và nút sửa khóa", async () => {
    fake.grants = grantsOf();
    await open(list([promo(1)]), false);
    await screen.findByText(/Bạn chỉ có quyền xem nội dung website/);
    expect(locked(screen.getByRole("checkbox", { name: "Bật banner 1" }))).toBe(true);
    expect(locked(button("+ Thêm banner"))).toBe(true);
    expect(locked(button("Xóa"))).toBe(true);
    expect(rows()[0]?.getAttribute("draggable")).toBe("false");
  });

  it("lỗi 422 của máy chủ hiện câu tiếng Việt, giữ bản đang sửa", async () => {
    await open(list([promo(1)]));
    fireEvent.click(screen.getByRole("checkbox", { name: "Bật banner 1" }));
    fake.saveErrors.push({ status: 422, body: { error: "pin_not_in_source", request_id: "t" } });
    fireEvent.click(button("Lưu banner"));
    await screen.findByText("Sản phẩm ghim phải đang bán và thuộc chiến dịch hoặc danh mục của banner");
    expect(button("Lưu banner")).toHaveProperty("disabled", false);
  });
});
