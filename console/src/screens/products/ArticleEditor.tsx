// Trình soạn bài viết dạng khối (kế hoạch M1 Task 326, Q34; mockup docs/mockups/m1/san-pham-sua.html, trạng thái A, B,
// C). Danh sách khối, thêm/xóa/đổi thứ tự, khối ảnh chọn từ ảnh sẵn sàng của mẫu; "Xem trước" dựng giống trang sản
// phẩm trên web. Mọi chữ hiện bằng phần tử văn bản của React (không dangerouslySetInnerHTML, không dùng articleHtml):
// chuỗi như "<img onerror>" hiện nguyên văn, không chạy (R7). Lỗi (trước khi gửi, hay invalid_article của máy chủ) hiện
// tại đúng khối.
import { Fragment, useId, useState, type ReactNode } from "react";
import type { ProductImage } from "../../api";
import {
  BLOCK_LABEL,
  BLOCK_TYPES,
  collapsedRange,
  hasUsableImage,
  imageChoices,
  imageOf,
  itemsOf,
  MAX_BLOCKS,
  newBlock,
  type BlockType,
  type EditBlock,
} from "./articleRules";
import { moveTo } from "./imageRules";
import { coverPicture } from "./productRules";
import "./article.css";

const NUM = new Intl.NumberFormat("vi-VN");
const TEXT_RAW_HINT = "Chữ được giữ nguyên văn, kể cả ký tự < >; không có định dạng HTML.";
const TEXT_EMPTY = "Bài viết chưa có khối nào. Website chỉ hiện mô tả ngắn.";
const TEXT_NO_IMAGE = "Tải ảnh lên khối Ảnh trước, rồi thêm khối ảnh.";
const TEXT_PREVIEW_HINT = "Xem trước dựng giống trang sản phẩm trên web: chữ hiện nguyên văn, không chạy mã.";

export interface ArticleEditorProps {
  blocks: readonly EditBlock[];
  /** Ảnh của mẫu (khối ảnh chọn trong ảnh sẵn sàng). */
  images: readonly ProductImage[];
  /** Khóa khối → câu lỗi. */
  errors: Readonly<Record<string, string>>;
  /** Lỗi của cả bài (số khối, cỡ). */
  articleError?: string;
  /** Thiếu catalog.manage: chỉ hiện Xem trước. */
  readOnly: boolean;
  onChange: (blocks: EditBlock[]) => void;
}

export function ArticleEditor({ blocks, images, errors, articleError, readOnly, onChange }: ArticleEditorProps) {
  const [tab, setTab] = useState<"edit" | "preview">("edit");
  const [expanded, setExpanded] = useState(false);
  const id = useId();
  const count = (
    <div className="row-between">
      <h2>Bài viết chi tiết</h2>
      <span className="small muted">{NUM.format(blocks.length)} khối</span>
    </div>
  );
  if (readOnly)
    return (
      <div className="card">
        {count}
        <Preview blocks={blocks} images={images} />
      </div>
    );

  const tabBtn = (t: "edit" | "preview", label: string) => (
    <button
      type="button"
      className="tab"
      role="tab"
      id={`${id}-${t}`}
      aria-selected={tab === t}
      aria-controls={`${id}-${t}-panel`}
      onClick={() => {
        setTab(t);
      }}
    >
      {label}
    </button>
  );

  return (
    <div className="card">
      {count}
      <div className="tabs" role="tablist" aria-label="Bài viết chi tiết">
        {tabBtn("edit", "Soạn")}
        {tabBtn("preview", "Xem trước")}
      </div>
      <div role="tabpanel" id={`${id}-${tab}-panel`} aria-labelledby={`${id}-${tab}`}>
        {tab === "preview" ? (
          <Preview blocks={blocks} images={images} />
        ) : (
          <Compose
            blocks={blocks}
            images={images}
            errors={errors}
            expanded={expanded}
            onExpand={() => {
              setExpanded(true);
            }}
            onChange={onChange}
          />
        )}
      </div>
      {articleError !== undefined && <p className="field-err">{articleError}</p>}
    </div>
  );
}

function Compose({
  blocks,
  images,
  errors,
  expanded,
  onExpand,
  onChange,
}: {
  blocks: readonly EditBlock[];
  images: readonly ProductImage[];
  errors: Readonly<Record<string, string>>;
  expanded: boolean;
  onExpand: () => void;
  onChange: (blocks: EditBlock[]) => void;
}) {
  const canImage = hasUsableImage(images);
  const full = blocks.length >= MAX_BLOCKS;
  const add = (t: BlockType) => {
    onChange([...blocks, newBlock(t)]);
  };
  const addButtons = BLOCK_TYPES.map((t) => (
    <button
      key={t}
      type="button"
      className="btn btn-quiet"
      disabled={full || (t === "image" && !canImage)}
      onClick={() => {
        add(t);
      }}
    >
      + {BLOCK_LABEL[t]}
    </button>
  ));
  const noImageHint = !canImage && <p className="hint">{TEXT_NO_IMAGE}</p>;

  if (blocks.length === 0)
    return (
      <div className="empty blocks">
        <p className="muted">{TEXT_EMPTY}</p>
        <div className="addbar">{addButtons}</div>
        {noImageHint}
      </div>
    );

  const errIdx = blocks.flatMap((b, i) => (errors[b.key] === undefined ? [] : [i]));
  const gap = collapsedRange(blocks.length, errIdx, expanded);
  const set = (i: number, b: EditBlock) => {
    onChange(blocks.map((x, j) => (j === i ? b : x)));
  };
  const move = (from: number, to: number) => {
    // Dời khối vào đoạn đang thu gọn thì mở hết, để khối vừa dời không biến mất.
    onExpand();
    onChange(moveTo(blocks, from, to));
  };

  return (
    <>
      <div className="blocks">
        {blocks.map((b, i) => {
          if (gap !== null && i >= gap.from && i < gap.to) {
            if (i !== gap.from) return null;
            return (
              <button key="gap" type="button" className="block-gap btn-quiet" onClick={onExpand}>
                Khối {gap.from + 1}–{gap.to} ({NUM.format(gap.to - gap.from)} khối) đang thu gọn — bấm để mở
              </button>
            );
          }
          return (
            <BlockCard
              key={b.key}
              block={b}
              n={i + 1}
              last={i === blocks.length - 1}
              images={images}
              error={errors[b.key]}
              onChange={(nb) => {
                set(i, nb);
              }}
              onMove={(d) => {
                move(i, i + d);
              }}
              onRemove={() => {
                onChange(blocks.filter((_, j) => j !== i));
              }}
            />
          );
        })}
      </div>
      <div className="addbar">
        <span className="small muted">Thêm khối:</span>
        {addButtons}
      </div>
      {noImageHint}
    </>
  );
}

function BlockCard({
  block: b,
  n,
  last,
  images,
  error,
  onChange,
  onMove,
  onRemove,
}: {
  block: EditBlock;
  n: number;
  last: boolean;
  images: readonly ProductImage[];
  error: string | undefined;
  onChange: (b: EditBlock) => void;
  onMove: (d: -1 | 1) => void;
  onRemove: () => void;
}) {
  const invalid = error !== undefined;
  const cls = `input${invalid ? " is-err" : ""}`;
  const name = `Khối ${String(n)} · ${BLOCK_LABEL[b.type]}`;
  const aria = { "aria-label": name, "aria-invalid": invalid || undefined };
  let body: ReactNode;
  let hint: string | null = null;
  switch (b.type) {
    case "heading":
      body = (
        <input
          className={cls}
          value={b.text}
          {...aria}
          onChange={(e) => {
            onChange({ ...b, text: e.target.value });
          }}
        />
      );
      break;
    case "paragraph":
      body = (
        <textarea
          className={cls}
          value={b.text}
          {...aria}
          onChange={(e) => {
            onChange({ ...b, text: e.target.value });
          }}
        />
      );
      if (/[<>]/.test(b.text)) hint = TEXT_RAW_HINT;
      break;
    case "list":
      body = (
        <textarea
          className={cls}
          value={b.lines}
          {...aria}
          onChange={(e) => {
            onChange({ ...b, lines: e.target.value });
          }}
        />
      );
      hint = "Mỗi dòng là một ý";
      break;
    case "image":
      body = (
        <div className="row">
          <Thumb img={imageOf(images, b.fileId)} />
          <label className="field grow">
            <span>Chọn từ ảnh của mẫu</span>
            <span className="select-wrap">
              <select
                className={invalid ? "is-err" : undefined}
                aria-label={`Chọn từ ảnh của mẫu (khối ${String(n)})`}
                aria-invalid={invalid || undefined}
                value={b.fileId}
                onChange={(e) => {
                  onChange({ ...b, fileId: e.target.value });
                }}
              >
                {imageChoices(images, b.fileId).map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
            </span>
          </label>
        </div>
      );
      break;
  }
  return (
    <div className={`block${invalid ? " is-err" : ""}`} data-testid="article-block">
      <div className="block-h">
        <span className="grow">
          {n} · {BLOCK_LABEL[b.type]}
        </span>
        <button
          type="button"
          className="ibtn"
          aria-label={`Đưa khối ${String(n)} lên`}
          disabled={n === 1}
          onClick={() => {
            onMove(-1);
          }}
        >
          ↑
        </button>
        <button
          type="button"
          className="ibtn"
          aria-label={`Đưa khối ${String(n)} xuống`}
          disabled={last}
          onClick={() => {
            onMove(1);
          }}
        >
          ↓
        </button>
        <button type="button" className="ibtn" aria-label={`Xóa khối ${String(n)}`} onClick={onRemove}>
          ×
        </button>
      </div>
      {body}
      {invalid && <span className="field-err">{error}</span>}
      {!invalid && hint !== null && <span className="hint">{hint}</span>}
    </div>
  );
}

/** <picture> của một ảnh sẵn sàng của mẫu (cùng luật avif → webp → jpeg của ảnh bìa). */
function Picture({ img, sizes, alt }: { img: ProductImage; sizes: string; alt: string }) {
  const c = coverPicture({ imageId: img.id, status: img.status, ...(img.variants ? { variants: img.variants } : {}) });
  if (c.kind !== "ready") return null;
  return (
    <picture>
      {c.sources.map((s) => (
        <source key={s.type} type={s.type} srcSet={s.srcSet} sizes={sizes} />
      ))}
      <img src={c.src} srcSet={c.srcSet} sizes={sizes} alt={alt} loading="lazy" />
    </picture>
  );
}

function Thumb({ img }: { img: ProductImage | undefined }) {
  if (img === undefined) return <span className="thumb is-empty" />;
  return (
    <span className="thumb">
      <Picture img={img} sizes="48px" alt="" />
    </span>
  );
}

/** Đoạn văn: xuống dòng thành <br> (như bộ dựng HTML của máy chủ). */
function Lines({ text }: { text: string }) {
  return text.split("\n").map((l, i) => (
    <Fragment key={i}>
      {i > 0 && <br />}
      {l}
    </Fragment>
  ));
}

/**
 * Xem trước: như catalog.RenderArticleHTML — tiêu đề cấp 2/3 (ở đây h3/h4 vì thẻ đã có h2), đoạn có <br>, danh sách,
 * ảnh kèm chú thích; khối ảnh chưa chọn hay ảnh không dùng được thì bỏ.
 */
function Preview({ blocks, images }: { blocks: readonly EditBlock[]; images: readonly ProductImage[] }) {
  return (
    <>
      <div className="preview" data-testid="article-preview">
        {blocks.length === 0 && <p className="muted">{TEXT_EMPTY}</p>}
        {blocks.map((b) => {
          switch (b.type) {
            case "heading":
              return b.level === 3 ? <h4 key={b.key}>{b.text}</h4> : <h3 key={b.key}>{b.text}</h3>;
            case "paragraph":
              return (
                <p key={b.key}>
                  <Lines text={b.text} />
                </p>
              );
            case "list":
              return (
                <ul key={b.key}>
                  {itemsOf(b.lines).map((l, i) => (
                    <li key={i}>{l}</li>
                  ))}
                </ul>
              );
            case "image": {
              const img = imageOf(images, b.fileId);
              if (img === undefined) return null;
              return (
                <figure key={b.key}>
                  <div className="img-ph">
                    <Picture img={img} sizes="240px" alt={b.caption} />
                  </div>
                  {b.caption !== "" && <figcaption className="small muted">{b.caption}</figcaption>}
                </figure>
              );
            }
          }
        })}
      </div>
      <p className="hint">{TEXT_PREVIEW_HINT}</p>
    </>
  );
}
