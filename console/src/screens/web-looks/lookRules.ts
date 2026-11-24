// Luật thuần của màn Bộ phối (kế hoạch M5 Task 460; mockup docs/mockups/m5/web-looks.html; admin cũ
// apps/admin-*/src/lib/looks.mjs, components/Looks.tsx). Hiện/ẩn, lý do, tiêu đề tự động, đường dẫn do máy chủ tính
// (getLooks, previewLooks của Task 454); ở đây chỉ dựng chữ, đổi bộ đang sửa và dựng thân Look. Máy chủ kiểm lại mọi
// luật (≤ 500 bộ, ≤ 6 trên trang chủ, 1–3 món, chấm 0–100 %, ảnh của món, độ dài chữ, giờ).
import type { Look, LookHidden, LookItem } from "../../api";
import { WEBSITE_ORIGIN } from "../web-campaigns/campaignRules";

export const MAX_LOOKS = 500;
export const MAX_HOME = 6;
export const MAX_ITEMS = 3;
export const NAME_MAX = 28;
export const TITLE_MAX = 60;
export const HEADLINE_MAX = 90;
export const STORY_MAX = 220;
export const SIZE_MAX = 6;
export const MAX_CATEGORIES = 6;
export const HEIGHT_MIN = 130;
export const HEIGHT_MAX = 200;
/** Chấm giữ trong 3–97 % để khách vẫn bấm được (admin cũ); máy chủ nhận 0–100 %. */
export const DOT_MIN = 3;
export const DOT_MAX = 97;

/** Tên dịp gợi ý (lookTemplates của admin cũ). */
export const NAME_TEMPLATES = [
  "Đi làm ngày thường",
  "Họp quan trọng",
  "Đứng lớp cả ngày",
  "Hẹn sau giờ làm",
  "Cuối tuần ở nhà",
] as const;

/** 6 lý do ẩn của website, đúng thứ tự kiểm và chữ của mockup trạng thái C. */
export const HIDDEN_TEXT: Readonly<Record<LookHidden, string>> = {
  disabled: "Không hiện trên trang chủ",
  scheduled: "Chưa tới giờ hiện",
  expired: "Đã hết thời gian hiện",
  "product-missing": "Một món đã ngừng bán",
  "sold-out": "Một món đã hết hàng mọi size",
  "photo-missing": "Ảnh đã chọn không còn, chọn lại ảnh",
};

const lowerFirst = (s: string) => s.charAt(0).toLowerCase() + s.slice(1);

/** Chữ lý do ẩn kèm tên món gây ra (nếu có). */
export function hiddenText(reason: LookHidden, productName: string | undefined): string {
  return HIDDEN_TEXT[reason] + (productName === undefined ? "" : ` (${productName})`);
}

/**
 * Dòng trạng thái ở danh sách. Không lên trang chủ không phải cảnh báo (liên kết riêng vẫn mở); lý do khác là cảnh báo
 * đỏ "Đang ẩn: …".
 */
export function hiddenLine(
  s: { shown: boolean; reason?: LookHidden },
  productName: string | undefined,
): { text: string; warn: boolean } {
  if (s.shown) return { text: "Đang hiện", warn: false };
  if (s.reason === "disabled") return { text: "Không hiện trên trang chủ (liên kết riêng vẫn mở được)", warn: false };
  if (s.reason === undefined) return { text: "Đang ẩn", warn: true };
  return { text: `Đang ẩn: ${lowerFirst(hiddenText(s.reason, productName))}`, warn: true };
}

/** Một ảnh có thể chọn làm ảnh người mẫu: ảnh đang hiện của một món (url là biến thể nhỏ nhất để làm ảnh nhỏ). */
export interface PhotoOpt {
  productId: string;
  fileId: string;
  color?: string;
  position: number;
  ready: boolean;
  thumb?: string;
  large?: string;
}

/** % trên ảnh kẹp vào 3–97, làm tròn số nguyên; NaN → giữa ảnh. */
export function clampDot(v: number): number {
  if (Number.isNaN(v)) return 50;
  return Math.min(DOT_MAX, Math.max(DOT_MIN, Math.round(v)));
}

/** Điểm bấm (clientX, clientY) → chấm theo % của khung ảnh, đã kẹp; khung chưa có kích thước → null. */
export function dotFromPoint(
  clientX: number,
  clientY: number,
  rect: { left: number; top: number; width: number; height: number },
): { x: number; y: number } | null {
  if (rect.width <= 0 || rect.height <= 0) return null;
  return {
    x: clampDot(((clientX - rect.left) / rect.width) * 100),
    y: clampDot(((clientY - rect.top) / rect.height) * 100),
  };
}

const NUDGE: Readonly<Record<string, [number, number]>> = {
  ArrowLeft: [-1, 0],
  ArrowRight: [1, 0],
  ArrowUp: [0, -1],
  ArrowDown: [0, 1],
};

/** Phím mũi tên dời chấm 1 %, vẫn trong 3–97 %; phím khác → null. */
export function nudgeDot(p: { x: number; y: number }, key: string): { x: number; y: number } | null {
  const d = NUDGE[key];
  if (d === undefined) return null;
  return { x: clampDot(p.x + d[0]), y: clampDot(p.y + d[1]) };
}

/** Chiều cao mặc định của chấm thứ k khi bộ có n món (spreadDots của admin cũ: trên xuống dưới, giữa ảnh). */
const SPREAD: Readonly<Record<number, readonly number[]>> = { 1: [45], 2: [32, 66], 3: [25, 50, 75] };

/** Bộ mới: chưa lên trang chủ, chưa tên, chưa món, chưa ảnh. */
export function newLook(id: string): Look {
  return { id, onHome: false, name: "", photoFileId: "", items: [] };
}

/**
 * Ảnh để chọn của bộ l: theo thứ tự món, trong mỗi món ảnh của màu đã chọn đứng trước rồi tới ảnh khác theo thứ tự;
 * không trùng; ảnh của mẫu không có trong bộ bỏ.
 */
export function photosFor(l: Look, photos: readonly PhotoOpt[]): PhotoOpt[] {
  const out: PhotoOpt[] = [];
  for (const it of l.items) {
    const own = photos.filter((p) => p.productId === it.productId).sort((a, b) => a.position - b.position);
    const want = it.color ?? "";
    const first = want === "" ? [] : own.filter((p) => p.color === want);
    for (const p of [...first, ...own])
      if (!out.some((x) => x.fileId === p.fileId && x.productId === p.productId)) out.push(p);
  }
  return out;
}

/**
 * Thêm món productId (màu color, rỗng = website tự chọn màu còn hàng) với chấm ở vị trí mặc định của nó; chấm cũ giữ
 * nguyên. Bộ chưa có ảnh → ảnh đầu của món này (màu đã chọn trước). Đủ 3 món hay món đã có → trả nguyên l.
 */
export function addItem(l: Look, productId: string, color: string, photos: readonly PhotoOpt[]): Look {
  if (l.items.length >= MAX_ITEMS || l.items.some((i) => i.productId === productId)) return l;
  const k = l.items.length;
  const item: LookItem = { productId, ...(color === "" ? {} : { color }), x: 50, y: SPREAD[k + 1]?.[k] ?? 50 };
  const next = { ...l, items: [...l.items, item] };
  if (l.photoFileId !== "") return next;
  const first = photosFor({ ...next, items: [item] }, photos)[0];
  return { ...next, photoFileId: first?.fileId ?? "" };
}

/** Bỏ món productId; ảnh đang chọn không còn thuộc món nào thì chọn ảnh đầu của các món còn lại (hết → không ảnh). */
export function removeItem(l: Look, productId: string, photos: readonly PhotoOpt[]): Look {
  const next = { ...l, items: l.items.filter((i) => i.productId !== productId) };
  const left = photosFor(next, photos);
  if (left.some((p) => p.fileId === l.photoFileId)) return next;
  return { ...next, photoFileId: left[0]?.fileId ?? "" };
}

/** Nhân bản: mã mới (liên kết mới), không lên trang chủ (không vượt 6), chép sâu món và danh mục. */
export function cloneLook(l: Look, id: string): Look {
  return {
    ...l,
    id,
    onHome: false,
    items: l.items.map((i) => ({ ...i })),
    ...(l.categoryIds === undefined ? {} : { categoryIds: [...l.categoryIds] }),
  };
}

export const homeCount = (ls: readonly Look[]): number => ls.filter((l) => l.onHome).length;

/** Ô Trang chủ của l bấm được: đang bật thì luôn tắt được; đang tắt thì chỉ bật khi trang chủ còn chỗ. */
export const canTurnOnHome = (ls: readonly Look[], l: Look): boolean => l.onHome || homeCount(ls) < MAX_HOME;

/** Chữ bỏ dấu, đ → d, chữ thường (so khớp khi tìm). */
export const foldText = (s: string): string =>
  s
    .normalize("NFD")
    .replace(/\p{Diacritic}/gu, "")
    .replace(/đ/gi, "d")
    .toLowerCase();

/** Bộ l khớp chữ tìm q (tên dịp hoặc tên món, bỏ dấu, không phân biệt hoa thường); chữ rỗng khớp mọi bộ. */
export function matchLook(l: Look, q: string, names: ReadonlyMap<string, string>): boolean {
  const t = foldText(q.trim());
  if (t === "") return true;
  return [l.name, ...l.items.map((i) => names.get(i.productId) ?? "")].some((s) => foldText(s).includes(t));
}

/** Liên kết đầy đủ của trang bộ (path do máy chủ trả: /bo-phoi/<tên>-<khóa>). */
export const lookLink = (path: string): string => WEBSITE_ORIGIN + path;
/** Liên kết hiện trong câu (bỏ https://). */
export const linkText = (path: string): string => lookLink(path).replace(/^https?:\/\//, "");

/** Vấn đề làm bộ l chưa lưu được (kiểm trên màn trước máy chủ); null khi đủ. */
export function lookProblem(l: Look): string | null {
  if (l.name.trim() === "") return "chưa có tên dịp";
  if (l.items.length === 0) return "chưa có sản phẩm. Chọn 1–3 sản phẩm";
  if (l.photoFileId === "") return "chưa chọn ảnh người mẫu";
  const h = l.modelHeightCm;
  if (h !== undefined && h !== 0 && (h < HEIGHT_MIN || h > HEIGHT_MAX))
    return `chiều cao người mẫu phải từ ${String(HEIGHT_MIN)} đến ${String(HEIGHT_MAX)} cm`;
  if (l.startsAt !== undefined && l.endsAt !== undefined && Date.parse(l.endsAt) <= Date.parse(l.startsAt))
    return "giờ kết thúc phải sau giờ bắt đầu";
  return null;
}

/** Lý do nút Lưu khóa: quá 6 bộ trên trang chủ, hay bộ đầu tiên chưa đủ; null khi lưu được. */
export function blockReason(ls: readonly Look[]): string | null {
  if (homeCount(ls) > MAX_HOME) return "Trang chủ hiện tối đa 6 bộ phối. Tắt Trang chủ ở một bộ trước khi lưu.";
  for (const [i, l] of ls.entries()) {
    const p = lookProblem(l);
    if (p !== null) return `Bộ ${String(i + 1)}${p.startsWith("chưa") ? " " : ": "}${p}.`;
  }
  return null;
}

const filled = (s: string | undefined): s is string => s !== undefined && s.trim() !== "";

function pick(l: Look): Look {
  return {
    id: l.id,
    onHome: l.onHome,
    name: l.name.trim(),
    ...(filled(l.title) ? { title: l.title.trim() } : {}),
    ...(filled(l.headline) ? { headline: l.headline.trim() } : {}),
    ...(filled(l.story) ? { story: l.story.trim() } : {}),
    photoFileId: l.photoFileId,
    ...(l.categoryIds === undefined || l.categoryIds.length === 0 ? {} : { categoryIds: [...l.categoryIds] }),
    ...(l.modelHeightCm === undefined || l.modelHeightCm === 0 ? {} : { modelHeightCm: l.modelHeightCm }),
    ...(filled(l.modelSize) ? { modelSize: l.modelSize.trim() } : {}),
    ...(l.startsAt === undefined ? {} : { startsAt: l.startsAt }),
    ...(l.endsAt === undefined ? {} : { endsAt: l.endsAt }),
    items: l.items.map((i) => ({
      productId: i.productId,
      ...(filled(i.color) ? { color: i.color } : {}),
      x: i.x,
      y: i.y,
    })),
  };
}

/** Thân gửi saveLooks/previewLooks: bỏ ô chữ rỗng, chiều cao 0, danh mục rỗng, màu rỗng. */
export function toRequest(ls: readonly Look[]): Look[] {
  return ls.map(pick);
}

/** Hai danh sách giống nhau theo thân gửi (để biết có thay đổi chưa lưu). */
export function sameLooks(a: readonly Look[], b: readonly Look[]): boolean {
  return JSON.stringify(toRequest(a)) === JSON.stringify(toRequest(b));
}

/** Dòng người mẫu: "Mẫu 1m62 · size S", "Mẫu 1m62", "Mẫu mặc size S" hay rỗng (LookModelLine của máy chủ). */
export function modelLine(height: number | undefined, size: string | undefined): string {
  const h =
    height === undefined || height <= 0
      ? ""
      : `${String(Math.floor(height / 100))}m${String(height % 100).padStart(2, "0")}`;
  const s = size?.trim() ?? "";
  if (h !== "" && s !== "") return `Mẫu ${h} · size ${s}`;
  if (h !== "") return `Mẫu ${h}`;
  if (s !== "") return `Mẫu mặc size ${s}`;
  return "";
}
