// Bước 3 của form bộ phối (mockup trạng thái B): chọn ảnh người mẫu từ ảnh của các món (HV21), đặt chấm sản phẩm bằng
// chạm (chọn số rồi bấm vào ảnh) hoặc kéo, phím mũi tên dời 1 %. Chấm luôn kẹp trong 3–97 % (lookRules.clampDot).
import { useRef, useState, type PointerEvent } from "react";
import type { Look } from "../../api";
import { DOT_MAX, DOT_MIN, dotFromPoint, nudgeDot, photosFor, type PhotoOpt } from "./lookRules";

interface Props {
  look: Look;
  photos: readonly PhotoOpt[];
  names: ReadonlyMap<string, string>;
  disabled: boolean;
  onPhoto: (fileId: string) => void;
  onDot: (index: number, p: { x: number; y: number }) => void;
}

const atEdge = (v: number) => v <= DOT_MIN || v >= DOT_MAX;

export function LookStage({ look, photos, names, disabled, onPhoto, onDot }: Props) {
  const [picked, setPicked] = useState(0);
  const drag = useRef<number | null>(null);
  const stage = useRef<HTMLDivElement>(null);
  const opts = photosFor(look, photos);
  const current = photos.find((p) => p.fileId === look.photoFileId);
  const pick = Math.min(picked, Math.max(look.items.length - 1, 0));
  const edge = look.items.findIndex((i) => atEdge(i.x) || atEdge(i.y));

  const place = (e: PointerEvent, k: number) => {
    const r = stage.current?.getBoundingClientRect();
    const p = r === undefined ? null : dotFromPoint(e.clientX, e.clientY, r);
    if (p !== null && k < look.items.length) onDot(k, p);
  };
  const down = (e: PointerEvent<HTMLDivElement>) => {
    if (disabled || look.items.length === 0) return;
    const dot = e.target instanceof Element ? e.target.closest<HTMLElement>("[data-dot]") : null;
    const k = dot === null ? pick : Number(dot.dataset.dot);
    drag.current = k;
    setPicked(k);
    try {
      e.currentTarget.setPointerCapture(e.pointerId);
    } catch {
      // Trình duyệt không hỗ trợ giữ con trỏ: vẫn kéo được khi con trỏ còn trong ảnh.
    }
    place(e, k);
  };
  const move = (e: PointerEvent<HTMLDivElement>) => {
    if (drag.current !== null && !disabled) place(e, drag.current);
  };
  const up = () => {
    drag.current = null;
  };

  return (
    <>
      {opts.length === 0 ? (
        <p className="small muted">Chọn sản phẩm trước; ảnh người mẫu lấy từ ảnh của các sản phẩm đã chọn.</p>
      ) : (
        <div className="lk-thumbs" role="group" aria-label="Ảnh của các sản phẩm đã chọn">
          {opts.map((p, k) => (
            <button
              key={`${p.productId}:${p.fileId}`}
              type="button"
              className="lk-thumb"
              aria-label={`Ảnh ${String(k + 1)}`}
              aria-pressed={p.fileId === look.photoFileId}
              disabled={disabled}
              onClick={() => {
                onPhoto(p.fileId);
              }}
            >
              {p.thumb === undefined ? String(k + 1) : <img src={p.thumb} alt="" />}
            </button>
          ))}
        </div>
      )}
      <p className="hint">
        Chọn từ ảnh của {look.items.length} sản phẩm trên (màu đã chọn đứng trước). Đặt chấm: chọn số rồi bấm vào ảnh,
        hoặc kéo chấm.
      </p>
      {look.photoFileId !== "" && current === undefined && (
        <p className="reason">Ảnh đã chọn không còn trong ảnh của các sản phẩm. Chọn lại ảnh.</p>
      )}
      <div
        ref={stage}
        className="lk-photo is-stage"
        role="group"
        aria-label="Khung đặt chấm"
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
      >
        {current?.large !== undefined && <img src={current.large} alt="" draggable={false} />}
        {look.items.map((it, k) => (
          <button
            key={it.productId}
            type="button"
            data-dot={k}
            className={`lk-dot${k === pick ? " is-pick" : ""}`}
            style={{ left: `${String(it.x)}%`, top: `${String(it.y)}%` }}
            aria-label={`Chấm ${String(k + 1)}: ${names.get(it.productId) ?? "sản phẩm"}`}
            disabled={disabled}
            onClick={() => {
              setPicked(k);
            }}
            onKeyDown={(e) => {
              const p = nudgeDot(it, e.key);
              if (p === null || disabled) return;
              e.preventDefault();
              setPicked(k);
              onDot(k, p);
            }}
          >
            {k + 1}
          </button>
        ))}
      </div>
      {edge >= 0 && <p className="small muted">Chấm {edge + 1} kéo ra mép: giữ ở 3–97% để khách vẫn bấm được.</p>}
    </>
  );
}
