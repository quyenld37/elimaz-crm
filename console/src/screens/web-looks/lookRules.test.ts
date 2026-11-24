// Luật thuần của màn Bộ phối (kế hoạch M5 Task 460; mockup docs/mockups/m5/web-looks.html): kẹp chấm 3–97 %, vị trí
// chấm mặc định, thêm/bỏ món, ảnh mặc định, nhân bản, ≤ 6 trên trang chủ, lý do khóa Lưu, thân gửi máy chủ.
import { describe, expect, it } from "vitest";
import type { Look, LookHidden } from "../../api";
import {
  DOT_MAX,
  DOT_MIN,
  HIDDEN_TEXT,
  MAX_HOME,
  addItem,
  blockReason,
  canTurnOnHome,
  clampDot,
  cloneLook,
  dotFromPoint,
  hiddenLine,
  lookLink,
  matchLook,
  newLook,
  nudgeDot,
  photosFor,
  removeItem,
  sameLooks,
  toRequest,
  type PhotoOpt,
} from "./lookRules";

const P1 = "0195f3b2-0000-7000-8000-0000000a0001";
const P2 = "0195f3b2-0000-7000-8000-0000000a0002";
const P3 = "0195f3b2-0000-7000-8000-0000000a0003";
const P4 = "0195f3b2-0000-7000-8000-0000000a0004";
const L1 = "0195f3b2-0000-7000-8000-0000000b0001";
const L2 = "0195f3b2-0000-7000-8000-0000000b0002";

const photo = (productId: string, fileId: string, position: number, color?: string): PhotoOpt => ({
  productId,
  fileId,
  position,
  ready: true,
  ...(color === undefined ? {} : { color }),
});

const look = (p: Partial<Look> = {}): Look => ({
  id: L1,
  onHome: true,
  name: "Đi làm ngày thường",
  photoFileId: "f1",
  items: [{ productId: P1, x: 50, y: 45 }],
  ...p,
});

describe("chấm trên ảnh", () => {
  it("kẹp 3–97 %, làm tròn số nguyên", () => {
    expect(clampDot(-20)).toBe(DOT_MIN);
    expect(clampDot(0)).toBe(3);
    expect(clampDot(140)).toBe(DOT_MAX);
    expect(clampDot(97.4)).toBe(97);
    expect(clampDot(42.6)).toBe(43);
    expect(clampDot(Number.NaN)).toBe(50);
  });

  it("điểm bấm → % của khung ảnh; kéo ra ngoài mép bị kẹp; khung rỗng → null", () => {
    const rect = { left: 100, top: 200, width: 320, height: 400 };
    expect(dotFromPoint(260, 400, rect)).toEqual({ x: 50, y: 50 });
    expect(dotFromPoint(1000, 199, rect)).toEqual({ x: 97, y: 3 });
    expect(dotFromPoint(0, 10_000, rect)).toEqual({ x: 3, y: 97 });
    expect(dotFromPoint(1, 1, { left: 0, top: 0, width: 0, height: 0 })).toBeNull();
  });

  it("phím mũi tên dời 1 %, giữ trong 3–97 %", () => {
    expect(nudgeDot({ x: 50, y: 50 }, "ArrowRight")).toEqual({ x: 51, y: 50 });
    expect(nudgeDot({ x: 97, y: 50 }, "ArrowRight")).toEqual({ x: 97, y: 50 });
    expect(nudgeDot({ x: 50, y: 3 }, "ArrowUp")).toEqual({ x: 50, y: 3 });
    expect(nudgeDot({ x: 50, y: 50 }, "ArrowDown")).toEqual({ x: 50, y: 51 });
    expect(nudgeDot({ x: 4, y: 50 }, "ArrowLeft")).toEqual({ x: 3, y: 50 });
    expect(nudgeDot({ x: 50, y: 50 }, "Enter")).toBeNull();
  });
});

describe("thêm, bỏ món; ảnh", () => {
  const photos = [photo(P1, "a2", 2, "Trắng"), photo(P1, "a1", 1, "Đen"), photo(P2, "b1", 1)];

  it("thêm món: chấm mới theo vị trí mặc định, không đổi chấm cũ; bộ chưa có ảnh → ảnh đầu của màu đã chọn", () => {
    const a = addItem(newLook(L1), P1, "Trắng", photos);
    expect(a.items).toEqual([{ productId: P1, color: "Trắng", x: 50, y: 45 }]);
    expect(a.photoFileId).toBe("a2");
    const moved = { ...a, items: [{ productId: P1, color: "Trắng", x: 20, y: 20 }] };
    const b = addItem(moved, P2, "", photos);
    expect(b.items).toEqual([
      { productId: P1, color: "Trắng", x: 20, y: 20 },
      { productId: P2, x: 50, y: 66 },
    ]);
    expect(b.photoFileId).toBe("a2");
    const c = addItem(addItem(b, P3, "", photos), P4, "", photos);
    expect(c.items).toHaveLength(3);
    expect(addItem(c, P1, "", photos)).toBe(c);
    expect(addItem(b, P1, "", photos)).toBe(b);
  });

  it("bỏ món: ảnh của món đó bỏ, chọn ảnh đầu của món còn lại; hết món → không ảnh", () => {
    const l = look({
      photoFileId: "a1",
      items: [
        { productId: P1, x: 10, y: 10 },
        { productId: P2, x: 50, y: 66 },
      ],
    });
    const a = removeItem(l, P1, photos);
    expect(a.items).toEqual([{ productId: P2, x: 50, y: 66 }]);
    expect(a.photoFileId).toBe("b1");
    expect(removeItem(a, P2, photos).photoFileId).toBe("");
    const keep = removeItem(look({ photoFileId: "b1", items: l.items }), P1, photos);
    expect(keep.photoFileId).toBe("b1");
  });

  it("ảnh để chọn: theo thứ tự món, màu đã chọn đứng trước, không trùng; món không có trong bộ bỏ", () => {
    const l = look({
      items: [
        { productId: P2, x: 50, y: 50 },
        { productId: P1, color: "Đen", x: 50, y: 50 },
      ],
    });
    const got = photosFor(l, [...photos, photo(P1, "a1", 1, "Đen"), photo(P3, "c1", 1)]).map((p) => p.fileId);
    expect(got).toEqual(["b1", "a1", "a2"]);
  });
});

describe("danh sách", () => {
  it("nhân bản: mã mới, không lên trang chủ, chép sâu món", () => {
    const l = look();
    const c = cloneLook(l, L2);
    expect(c).toMatchObject({ id: L2, onHome: false, name: l.name, photoFileId: "f1" });
    expect(c.items).not.toBe(l.items);
    expect(c.items).toEqual(l.items);
  });

  it("trang chủ tối đa 6: bộ đang bật luôn tắt được; bộ chưa bật chỉ bật khi còn chỗ", () => {
    const ls = [1, 2, 3, 4, 5, 6, 7].map((n) => look({ id: `L${String(n)}`, onHome: n <= MAX_HOME }));
    const [first, , , , , , seventh] = ls;
    if (first === undefined || seventh === undefined) throw new Error("thiếu bộ");
    expect(canTurnOnHome(ls, seventh)).toBe(false);
    expect(canTurnOnHome(ls, first)).toBe(true);
    expect(canTurnOnHome(ls.slice(1), seventh)).toBe(true);
  });

  it("tìm theo tên dịp hoặc tên món, không phân biệt hoa thường, dấu", () => {
    const names = new Map([[P1, "Áo sơ mi lụa tay bồng"]]);
    expect(matchLook(look(), "đi làm", names)).toBe(true);
    expect(matchLook(look(), "SO MI", names)).toBe(true);
    expect(matchLook(look(), "đầm", names)).toBe(false);
    expect(matchLook(look(), "  ", names)).toBe(true);
  });

  it("liên kết đầy đủ của trang bộ", () => {
    expect(lookLink("/bo-phoi/di-lam-ngay-thuong-0193b2f1")).toBe(
      "https://elimaz.com.vn/bo-phoi/di-lam-ngay-thuong-0193b2f1",
    );
  });
});

describe("lý do khóa Lưu", () => {
  it.each([
    [look({ name: "  " }), "Bộ 1 chưa có tên dịp."],
    [look({ items: [] }), "Bộ 1 chưa có sản phẩm. Chọn 1–3 sản phẩm."],
    [look({ photoFileId: "" }), "Bộ 1 chưa chọn ảnh người mẫu."],
    [look({ modelHeightCm: 120 }), "Bộ 1: chiều cao người mẫu phải từ 130 đến 200 cm."],
    [
      look({ startsAt: "2026-12-01T00:00:00Z", endsAt: "2026-12-01T00:00:00Z" }),
      "Bộ 1: giờ kết thúc phải sau giờ bắt đầu.",
    ],
  ])("%#: %s", (l, want) => {
    expect(blockReason([l])).toBe(want);
  });

  it("đủ → null; quá 6 bộ trên trang chủ → câu tối đa 6", () => {
    expect(blockReason([look()])).toBeNull();
    const ls = [1, 2, 3, 4, 5, 6, 7].map((n) => look({ id: `L${String(n)}` }));
    expect(blockReason(ls)).toBe("Trang chủ hiện tối đa 6 bộ phối. Tắt Trang chủ ở một bộ trước khi lưu.");
  });
});

describe("thân gửi máy chủ", () => {
  it("bỏ ô chữ rỗng, cắt khoảng trắng tên, bỏ màu rỗng, bỏ chiều cao 0", () => {
    const l = look({
      name: " Đi làm ",
      title: "",
      headline: "Mặc gì?",
      story: "",
      modelHeightCm: 0,
      modelSize: "",
      categoryIds: [],
      items: [{ productId: P1, color: "", x: 50, y: 45 }],
    });
    expect(toRequest([l])).toEqual([
      {
        id: L1,
        onHome: true,
        name: "Đi làm",
        headline: "Mặc gì?",
        photoFileId: "f1",
        items: [{ productId: P1, x: 50, y: 45 }],
      },
    ]);
    expect(sameLooks([l], [look({ name: "Đi làm", headline: "Mặc gì?" })])).toBe(true);
    expect(sameLooks([l], [look({ name: "Đi làm" })])).toBe(false);
  });
});

describe("chữ lý do ẩn", () => {
  it("đủ 6 lý do của website, đúng chữ mockup", () => {
    expect(Object.keys(HIDDEN_TEXT)).toEqual([
      "disabled",
      "scheduled",
      "expired",
      "product-missing",
      "sold-out",
      "photo-missing",
    ] satisfies LookHidden[]);
    expect(HIDDEN_TEXT["sold-out"]).toBe("Một món đã hết hàng mọi size");
  });

  it("dòng danh sách: hiện; không lên trang chủ (không cảnh báo); ẩn kèm tên món", () => {
    expect(hiddenLine({ shown: true }, undefined)).toEqual({ text: "Đang hiện", warn: false });
    expect(hiddenLine({ shown: false, reason: "disabled" }, undefined)).toEqual({
      text: "Không hiện trên trang chủ (liên kết riêng vẫn mở được)",
      warn: false,
    });
    expect(hiddenLine({ shown: false, reason: "sold-out" }, "Đầm suông cổ vuông")).toEqual({
      text: "Đang ẩn: một món đã hết hàng mọi size (Đầm suông cổ vuông)",
      warn: true,
    });
    expect(hiddenLine({ shown: false, reason: "photo-missing" }, undefined)).toEqual({
      text: "Đang ẩn: ảnh đã chọn không còn, chọn lại ảnh",
      warn: true,
    });
  });
});
