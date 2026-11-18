// Luật của trình soạn bài viết (kế hoạch M1 Task 326, Q34): khối API ↔ khối đang soạn, kiểm khối theo đúng luật của
// article.Validate trên máy chủ, tìm khối sai khi máy chủ trả invalid_article, thu gọn bài dài, lựa chọn ảnh.
import { describe, expect, it } from "vitest";
import type { ArticleBlock, ProductImage } from "../../api";
import {
  articleBody,
  articleSummary,
  checkArticle,
  collapsedRange,
  editBlocks,
  imageChoices,
  locateInvalid,
  MAX_BLOCKS,
  newBlock,
  sameArticle,
  type EditBlock,
} from "./articleRules";

const F1 = "0195f3b2-0000-7000-8000-00000000f001";
const F2 = "0195f3b2-0000-7000-8000-00000000f002";
const F3 = "0195f3b2-0000-7000-8000-00000000f003";
const XSS = "Mẹo phối: đi cùng giày mũi nhọn. <img src=x onerror=alert(1)>";

const IMAGES: ProductImage[] = [
  {
    id: "i1",
    fileId: F1,
    position: 1,
    status: "ready",
    color: "Đen",
    variants: [{ width: 400, format: "jpeg", url: "/a1.jpg" }],
  },
  { id: "i2", fileId: F2, position: 2, status: "lost" },
  { id: "i3", fileId: F3, position: 3, status: "ready", variants: [{ width: 400, format: "jpeg", url: "/a3.jpg" }] },
];

const ARTICLE: ArticleBlock[] = [
  { type: "heading", text: "Chất liệu và form dáng", level: 2 },
  { type: "paragraph", text: "Lụa Hàn cao cấp.\nRủ tự nhiên." },
  { type: "list", items: ["Giặt tay nước lạnh", "Không vắt mạnh"] },
  { type: "image", fileId: F1, caption: "Ảnh mặc thử" },
  { type: "heading", text: "Bảo quản", level: 3 },
  { type: "paragraph", text: XSS },
];

const block = (b: Partial<EditBlock> & Pick<EditBlock, "type">): EditBlock => ({ ...newBlock(b.type), ...b });

describe("editBlocks / articleBody", () => {
  it("đi một vòng giữ nguyên khối, thứ tự, chữ (kể cả < >)", () => {
    const eb = editBlocks(ARTICLE);
    expect(eb.map((b) => b.type)).toEqual(["heading", "paragraph", "list", "image", "heading", "paragraph"]);
    expect(new Set(eb.map((b) => b.key)).size).toBe(eb.length);
    expect(eb[2]?.lines).toBe("Giặt tay nước lạnh\nKhông vắt mạnh");
    expect(articleBody(eb)).toEqual(ARTICLE);
  });

  it("danh sách: mỗi dòng một ý, bỏ dòng trống; ảnh không chú thích thì không gửi caption", () => {
    const body = articleBody([
      block({ type: "list", lines: "Một\n\n  \nHai\n" }),
      block({ type: "image", fileId: F1 }),
      block({ type: "heading", text: "Tiêu đề" }),
    ]);
    expect(body).toEqual([
      { type: "list", items: ["Một", "Hai"] },
      { type: "image", fileId: F1 },
      { type: "heading", text: "Tiêu đề", level: 2 },
    ]);
  });

  it("sameArticle so nội dung, không so khóa", () => {
    expect(sameArticle(editBlocks(ARTICLE), editBlocks(ARTICLE))).toBe(true);
    const moved = editBlocks(ARTICLE).reverse();
    expect(sameArticle(editBlocks(ARTICLE), moved)).toBe(false);
  });
});

describe("checkArticle", () => {
  it("bài đủ bốn loại khối, chữ có < > → không lỗi", () => {
    expect(checkArticle(editBlocks(ARTICLE))).toEqual({ blocks: {} });
  });

  it("danh sách không dòng nào, đoạn trống, ảnh chưa chọn → lỗi đúng khối", () => {
    const bs = [
      block({ type: "list", lines: "\n " }),
      block({ type: "paragraph", text: "  " }),
      block({ type: "image" }),
      block({ type: "heading", text: "Ổn" }),
    ];
    const r = checkArticle(bs);
    expect(r.blocks).toEqual({
      [bs[0]?.key ?? ""]: "Danh sách cần ít nhất một dòng.",
      [bs[1]?.key ?? ""]: "Khối chưa có chữ.",
      [bs[2]?.key ?? ""]: "Chọn một ảnh của mẫu.",
    });
  });

  it("quá 5.000 ký tự, quá 50 dòng, dòng quá dài, ký tự điều khiển (Tab), xuống dòng trong tiêu đề", () => {
    const bs = [
      block({ type: "paragraph", text: "a".repeat(5001) }),
      block({ type: "paragraph", text: "đ".repeat(5000) }),
      block({ type: "list", lines: Array.from({ length: 51 }, (_, i) => `Ý ${String(i)}`).join("\n") }),
      block({ type: "list", lines: `ngắn\n${"b".repeat(5001)}` }),
      block({ type: "paragraph", text: "cột\tcột" }),
      block({ type: "heading", text: "hai\ndòng" }),
      block({ type: "image", fileId: F1, caption: "chú\u0007thích" }),
      block({ type: "paragraph", text: "xuống\ndòng được" }),
    ];
    const r = checkArticle(bs).blocks;
    const at = (i: number) => r[bs[i]?.key ?? ""];
    expect(at(0)).toBe("Tối đa 5.000 ký tự (đang có 5.001).");
    expect(at(1)).toBeUndefined();
    expect(at(2)).toBe("Danh sách tối đa 50 dòng.");
    expect(at(3)).toBe("Mỗi dòng tối đa 5.000 ký tự.");
    expect(at(4)).toBe("Có ký tự điều khiển (như Tab) không dùng được, xóa đi.");
    expect(at(5)).toBe("Có ký tự điều khiển (như Tab) không dùng được, xóa đi.");
    expect(at(6)).toBe("Có ký tự điều khiển (như Tab) không dùng được, xóa đi.");
    expect(at(7)).toBeUndefined();
  });

  it("quá 200 khối hay quá 200 KB → lỗi cả bài", () => {
    const many = Array.from({ length: MAX_BLOCKS + 1 }, () => block({ type: "paragraph", text: "x" }));
    expect(checkArticle(many).article).toBe("Bài viết tối đa 200 khối.");
    // 45 khối × 4.999 ký tự "<" (máy chủ mã hóa mỗi ký tự thành 6 byte) > 200 KB.
    const big = Array.from({ length: 45 }, () => block({ type: "paragraph", text: "<".repeat(4999) }));
    expect(checkArticle(big).article).toBe("Bài viết quá dài (tối đa 200 KB), bớt khối hoặc chữ.");
  });
});

describe("locateInvalid (máy chủ trả invalid_article)", () => {
  it("khối sai theo luật, và khối ảnh trỏ ảnh không còn dùng được (đã xóa, bị mất)", () => {
    const bs = [
      block({ type: "image", fileId: F1 }),
      block({ type: "image", fileId: F2 }),
      block({ type: "image", fileId: "0195f3b2-0000-7000-8000-00000000f0ff" }),
      block({ type: "list", lines: "" }),
    ];
    const r = locateInvalid(bs, IMAGES);
    expect(Object.keys(r)).toEqual([bs[1]?.key, bs[2]?.key, bs[3]?.key]);
    expect(r[bs[1]?.key ?? ""]).toBe("Ảnh này không còn dùng được, chọn ảnh khác.");
  });

  it("không tìm ra khối nào → rỗng (màn báo câu chung)", () => {
    expect(locateInvalid(editBlocks(ARTICLE), IMAGES)).toEqual({});
  });
});

describe("articleSummary", () => {
  it("câu của trạng thái C: số khối sai, khối đầu tiên sai", () => {
    const bs = editBlocks(ARTICLE);
    const last = block({ type: "list", lines: "" });
    const all = [...bs, last];
    expect(articleSummary(all, { [last.key]: "Danh sách cần ít nhất một dòng." })).toEqual({
      title: "Chưa lưu: bài viết có 1 khối sai.",
      detail: "Khối 7 (Danh sách): danh sách cần ít nhất một dòng. Sửa khối được tô đỏ rồi lưu lại.",
    });
  });
});

describe("collapsedRange", () => {
  it("30 khối: thu gọn khối 5–28 (chỉ số 4..27)", () => {
    expect(collapsedRange(30, [], false)).toEqual({ from: 4, to: 28 });
  });
  it("ít khối, đã mở, hay có khối sai trong đoạn thu gọn → không thu gọn", () => {
    expect(collapsedRange(8, [], false)).toBeNull();
    expect(collapsedRange(30, [], true)).toBeNull();
    expect(collapsedRange(30, [10], false)).toBeNull();
    expect(collapsedRange(30, [29], false)).toEqual({ from: 4, to: 28 });
  });
});

describe("imageChoices", () => {
  it("chỉ ảnh sẵn sàng, đánh số theo thứ tự ảnh của mẫu, kèm màu", () => {
    expect(imageChoices(IMAGES, "")).toEqual([
      { value: "", label: "Chọn ảnh" },
      { value: F1, label: "Ảnh 1 · Đen" },
      { value: F3, label: "Ảnh 3" },
    ]);
  });
  it("ảnh đang chọn không còn dùng được → vẫn hiện để nhân viên thấy", () => {
    expect(imageChoices(IMAGES, F2)).toEqual([
      { value: F2, label: "Ảnh không còn dùng được" },
      { value: F1, label: "Ảnh 1 · Đen" },
      { value: F3, label: "Ảnh 3" },
    ]);
  });
});
