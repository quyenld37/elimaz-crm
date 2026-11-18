// Luật thuần của trình soạn bài viết (kế hoạch M1 Task 326, Q34; mockup docs/mockups/m1/san-pham-sua.html, trạng thái
// A, B, C). Bài viết là danh sách khối (paragraph, heading, list, image), không HTML. Máy chủ (article.Validate) là nơi
// quyết định; ở đây kiểm cùng luật để chỉ ra khối sai trước khi gửi, và để tìm khối sai khi máy chủ trả invalid_article
// (thân lỗi chỉ có mã, không nói khối nào).
import type { ArticleBlock, ProductImage } from "../../api";

export type BlockType = ArticleBlock["type"];

/** Khối đang soạn: đủ mọi ô để đổi loại không mất chữ; articleBody chỉ gửi trường của đúng loại. */
export interface EditBlock {
  /** Khóa React, không gửi đi. */
  key: string;
  type: BlockType;
  /** paragraph, heading. */
  text: string;
  /** heading: giữ cấp đã lưu; khối mới cấp 2 (mockup không có ô chọn cấp). */
  level: 2 | 3;
  /** list: mỗi dòng một ý. */
  lines: string;
  /** image: "" là chưa chọn. */
  fileId: string;
  /** image: giữ chú thích đã lưu (mockup không có ô sửa chú thích). */
  caption: string;
}

/** Giới hạn của article.Validate (internal/modules/catalog/internal/article). */
export const MAX_BLOCKS = 200;
export const MAX_TEXT = 5000;
export const MAX_ITEMS = 50;
const MAX_BYTES = 200 * 1024;

export const BLOCK_LABEL: Readonly<Record<BlockType, string>> = {
  paragraph: "Đoạn văn",
  heading: "Tiêu đề",
  list: "Danh sách",
  image: "Ảnh",
};
/** Thứ tự nút "Thêm khối" (mockup). */
export const BLOCK_TYPES: readonly BlockType[] = ["paragraph", "heading", "list", "image"];

const NUM = new Intl.NumberFormat("vi-VN");
/** Số ký tự như máy chủ đếm (điểm mã Unicode). */
const charCount = (s: string) => Array.from(s).length;

let seq = 0;
const nextKey = () => {
  seq++;
  return `k${String(seq)}`;
};

export function newBlock(type: BlockType): EditBlock {
  return { key: nextKey(), type, text: "", level: 2, lines: "", fileId: "", caption: "" };
}

/** Khối đang soạn từ bài đã lưu (getProduct.article). */
export function editBlocks(article: readonly ArticleBlock[]): EditBlock[] {
  return article.map((b) => ({
    ...newBlock(b.type),
    text: b.text ?? "",
    level: b.level ?? 2,
    lines: (b.items ?? []).join("\n"),
    fileId: b.fileId ?? "",
    caption: b.caption ?? "",
  }));
}

/** Ý của khối danh sách: mỗi dòng một ý, bỏ dòng chỉ có khoảng trắng; chữ giữ nguyên. */
export const itemsOf = (lines: string) => lines.split("\n").filter((l) => l.trim() !== "");

/** Thân article gửi updateProduct/createProduct: chỉ trường của đúng loại khối. */
export function articleBody(blocks: readonly EditBlock[]): ArticleBlock[] {
  return blocks.map((b): ArticleBlock => {
    switch (b.type) {
      case "paragraph":
        return { type: "paragraph", text: b.text };
      case "heading":
        return { type: "heading", text: b.text, level: b.level };
      case "list":
        return { type: "list", items: itemsOf(b.lines) };
      case "image":
        return b.caption === ""
          ? { type: "image", fileId: b.fileId }
          : { type: "image", fileId: b.fileId, caption: b.caption };
    }
  });
}

/** Hai bài cùng nội dung (không so khóa React). */
export function sameArticle(a: readonly EditBlock[], b: readonly EditBlock[]): boolean {
  return JSON.stringify(articleBody(a)) === JSON.stringify(articleBody(b));
}

const TEXT_EMPTY = "Khối chưa có chữ.";
const TEXT_LIST_EMPTY = "Danh sách cần ít nhất một dòng.";
const TEXT_LIST_MANY = `Danh sách tối đa ${String(MAX_ITEMS)} dòng.`;
const TEXT_LINE_LONG = `Mỗi dòng tối đa ${NUM.format(MAX_TEXT)} ký tự.`;
const TEXT_CONTROL = "Có ký tự điều khiển (như Tab) không dùng được, xóa đi.";
const TEXT_NO_IMAGE = "Chọn một ảnh của mẫu.";
const TEXT_IMAGE_GONE = "Ảnh này không còn dùng được, chọn ảnh khác.";
const TEXT_MANY_BLOCKS = `Bài viết tối đa ${String(MAX_BLOCKS)} khối.`;
const TEXT_TOO_BIG = "Bài viết quá dài (tối đa 200 KB), bớt khối hoặc chữ.";

// Ký tự điều khiển C0, DEL, C1 (như validText của máy chủ).
// eslint-disable-next-line no-control-regex -- đúng là cần bắt ký tự điều khiển
const CONTROL = /[\u0000-\u001f\u007f-\u009f]/;
const hasControl = (s: string, newline: boolean) => CONTROL.test(newline ? s.replaceAll("\n", "") : s);
const tooLong = (s: string) => `Tối đa ${NUM.format(MAX_TEXT)} ký tự (đang có ${NUM.format(charCount(s))}).`;

/** Lỗi của chữ: bắt buộc thì phải có chữ khác khoảng trắng; tối đa MAX_TEXT ký tự; không ký tự điều khiển. */
function textError(s: string, required: boolean, newline: boolean): string | null {
  if (required && s.trim() === "") return TEXT_EMPTY;
  if (charCount(s) > MAX_TEXT) return tooLong(s);
  if (hasControl(s, newline)) return TEXT_CONTROL;
  return null;
}

function blockError(b: EditBlock): string | null {
  switch (b.type) {
    case "paragraph":
      return textError(b.text, true, true);
    case "heading":
      return textError(b.text, true, false);
    case "list": {
      const items = itemsOf(b.lines);
      if (items.length === 0) return TEXT_LIST_EMPTY;
      if (items.length > MAX_ITEMS) return TEXT_LIST_MANY;
      if (items.some((x) => charCount(x) > MAX_TEXT)) return TEXT_LINE_LONG;
      if (items.some((x) => hasControl(x, false))) return TEXT_CONTROL;
      return null;
    }
    case "image":
      if (b.fileId === "") return TEXT_NO_IMAGE;
      return textError(b.caption, false, false);
  }
}

/**
 * Cỡ JSON như máy chủ đo lại (encoding/json của Go mã hóa < > & và U+2028, U+2029 thành \uXXXX, 6 byte thay vì 1–3): đo trên chuỗi
 * JSON của trình duyệt, cộng phần chênh đó.
 */
function jsonBytes(body: readonly ArticleBlock[]): number {
  const s = JSON.stringify(body);
  const extra = (s.match(/[<>&\u2028\u2029]/g) ?? []).length;
  return new TextEncoder().encode(s).length + extra * 5;
}

export interface ArticleCheck {
  /** Khóa khối → câu lỗi. */
  blocks: Record<string, string>;
  /** Lỗi của cả bài (số khối, cỡ). */
  article?: string;
}

/** Kiểm bài theo luật của article.Validate. */
export function checkArticle(blocks: readonly EditBlock[]): ArticleCheck {
  const out: Record<string, string> = {};
  for (const b of blocks) {
    const e = blockError(b);
    if (e !== null) out[b.key] = e;
  }
  if (blocks.length > MAX_BLOCKS) return { blocks: out, article: TEXT_MANY_BLOCKS };
  if (jsonBytes(articleBody(blocks)) > MAX_BYTES) return { blocks: out, article: TEXT_TOO_BIG };
  return { blocks: out };
}

/** Ảnh của mẫu dùng được trong bài: sẵn sàng, có biến thể. */
const usable = (img: ProductImage) => img.status === "ready" && (img.variants ?? []).length > 0;

/**
 * Máy chủ trả invalid_article: tìm khối sai. Ngoài luật của checkArticle, máy chủ chỉ nhận khối ảnh trỏ tệp ảnh sản
 * phẩm sẵn sàng — khối ảnh trỏ ảnh không còn trong mẫu hay bị mất là khối nghi sai. Rỗng: không tìm ra (màn báo câu chung).
 */
export function locateInvalid(blocks: readonly EditBlock[], images: readonly ProductImage[]): Record<string, string> {
  const ok = new Set(images.filter(usable).map((i) => i.fileId));
  const own = checkArticle(blocks).blocks;
  const out: Record<string, string> = {};
  for (const b of blocks) {
    const e = own[b.key] ?? (b.type === "image" && !ok.has(b.fileId) ? TEXT_IMAGE_GONE : undefined);
    if (e !== undefined) out[b.key] = e;
  }
  return out;
}

const lowerFirst = (s: string) => s.charAt(0).toLocaleLowerCase("vi") + s.slice(1);

/** Hộp lỗi của trạng thái C: số khối sai, câu của khối sai đầu tiên. */
export function articleSummary(
  blocks: readonly EditBlock[],
  errors: Readonly<Record<string, string>>,
): { title: string; detail: string } | null {
  const bad = blocks.flatMap((b, i) => {
    const e = errors[b.key];
    return e === undefined ? [] : [{ n: i + 1, b, e }];
  });
  const first = bad[0];
  if (first === undefined) return null;
  return {
    title: `Chưa lưu: bài viết có ${NUM.format(bad.length)} khối sai.`,
    detail: `Khối ${String(first.n)} (${BLOCK_LABEL[first.b.type]}): ${lowerFirst(first.e)} Sửa khối được tô đỏ rồi lưu lại.`,
  };
}

/** Bài dài hơn số khối này thì thu gọn đoạn giữa (mockup: 30 khối → hiện 1–4 và 29–30). */
const COLLAPSE_OVER = 8;
const HEAD = 4;
const TAIL = 2;

/** Đoạn khối thu gọn [from, to) theo chỉ số; null khi không thu gọn (bài ngắn, đã mở, hay có khối sai trong đoạn). */
export function collapsedRange(
  count: number,
  errorIndexes: readonly number[],
  expanded: boolean,
): { from: number; to: number } | null {
  if (expanded || count <= COLLAPSE_OVER) return null;
  const from = HEAD;
  const to = count - TAIL;
  if (errorIndexes.some((i) => i >= from && i < to)) return null;
  return { from, to };
}

export interface ImageChoice {
  value: string;
  label: string;
}

/**
 * Lựa chọn của ô "Chọn từ ảnh của mẫu": ảnh sẵn sàng, đánh số theo thứ tự ảnh của mẫu (như khối Ảnh), kèm màu. Ảnh đang
 * chọn không còn dùng được vẫn hiện ở đầu (để nhân viên thấy và đổi); chưa chọn → "Chọn ảnh".
 */
export function imageChoices(images: readonly ProductImage[], current: string): ImageChoice[] {
  const out: ImageChoice[] = [];
  const list = images.flatMap((img, i) =>
    usable(img)
      ? [
          {
            value: img.fileId,
            label: `Ảnh ${String(i + 1)}${img.color === undefined || img.color === "" ? "" : ` · ${img.color}`}`,
          },
        ]
      : [],
  );
  if (current === "") out.push({ value: "", label: "Chọn ảnh" });
  else if (!list.some((c) => c.value === current)) out.push({ value: current, label: "Ảnh không còn dùng được" });
  return [...out, ...list];
}

/** Ảnh của mẫu ứng với fileId (để vẽ ảnh nhỏ, xem trước); không dùng được → undefined. */
export function imageOf(images: readonly ProductImage[], fileId: string): ProductImage | undefined {
  return images.find((i) => i.fileId === fileId && usable(i));
}

/** Còn ảnh nào để thêm khối ảnh. */
export const hasUsableImage = (images: readonly ProductImage[]) => images.some(usable);
