// Một ô ảnh của banner ảnh (mockup trạng thái A, C; HV18): chọn hay kéo ảnh vào, đang tải, đang xử lý, sẵn sàng (kích
// thước — từ danh sách banner, không thì getFile —, cảnh báo lệch tỉ lệ > 2 %), bị từ chối (lý do máy chủ trả). Tải lên do màn gọi (onPick); ô chỉ kiểm loại và cỡ tệp trước.
import { useId, type DragEvent } from "react";
import { BANNER_ACCEPT, BANNER_MAX_BYTES, FRAME, ratioWarning, type ImageState, type Slot } from "./bannerRules";
import { useFileInfo } from "./bannerData";

const SLOT_TEXT: Readonly<Record<Slot, string>> = { desktop: "máy tính", mobile: "điện thoại" };
const TEXT_TOO_BIG = "Ảnh quá 8 MB. Xuất lại ảnh nhỏ hơn rồi tải lên";
const TEXT_TYPE = "Chỉ nhận ảnh JPG, PNG hoặc WebP";

/** Lỗi của tệp trước khi tải (loại, cỡ); null khi gửi được. */
export function fileProblem(f: File): string | null {
  if (!BANNER_ACCEPT.split(",").includes(f.type)) return TEXT_TYPE;
  if (f.size > BANNER_MAX_BYTES) return TEXT_TOO_BIG;
  return null;
}

export interface BannerImageProps {
  slot: Slot;
  fileId: string | undefined;
  state: ImageState | null;
  /** Cỡ ảnh gốc máy chủ trả trong danh sách banner (Task 488); vắng → đọc getFile khi có quyền tải ảnh. */
  size?: { w: number; h: number };
  /** Tên tệp vừa chọn (chỉ để hiện). */
  name?: string;
  /** Lỗi trước khi tải (loại, cỡ tệp). */
  problem: string | null;
  /** Có quyền files.upload. */
  canUpload: boolean;
  disabled: boolean;
  onPick: (f: File) => void;
  onRemove: () => void;
}

export function BannerImage({
  slot,
  fileId,
  state,
  size,
  name,
  problem,
  canUpload,
  disabled,
  onPick,
  onRemove,
}: BannerImageProps) {
  const inputId = useId();
  const f = FRAME[slot];
  const ready = state?.phase === "ready";
  const info = useFileInfo(fileId, ready && canUpload && size === undefined);
  const w = size?.w ?? info.data?.width;
  const h = size?.h ?? info.data?.height;
  const warn = w !== undefined && h !== undefined ? ratioWarning(w, h, slot) : null;
  const what = SLOT_TEXT[slot];
  const pick = (files: FileList | null) => {
    const file = files?.[0];
    if (file !== undefined && !disabled) onPick(file);
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    pick(e.dataTransfer.files);
  };
  const hint =
    slot === "desktop"
      ? `Chữ nằm trong vùng giữa rộng ${String(f.safe)} px. JPG, PNG hoặc WebP, tối đa 8 MB.`
      : `Chữ nằm trong vùng giữa rộng ${String(f.safe)} px. Không có ảnh điện thoại thì điện thoại dùng ảnh máy tính, chữ sẽ nhỏ khó đọc.`;
  const chooser = (label: string) => (
    <>
      <label className="btn btn-quiet" htmlFor={inputId} aria-disabled={disabled || !canUpload}>
        {label}
      </label>
      <input
        id={inputId}
        className="sr-only"
        type="file"
        accept={BANNER_ACCEPT}
        aria-label={`Chọn ảnh cho ${what}`}
        disabled={disabled || !canUpload}
        onChange={(e) => {
          pick(e.target.files);
          e.target.value = "";
        }}
      />
    </>
  );
  const remove = (
    <button type="button" className="btn btn-quiet" aria-label={`Bỏ ảnh ${what}`} onClick={onRemove}>
      Bỏ ảnh
    </button>
  );

  if (state === null) {
    return (
      <div
        className="drop"
        onDragOver={(e) => {
          e.preventDefault();
        }}
        onDrop={onDrop}
      >
        <b>Kéo ảnh vào đây hoặc bấm để chọn</b>
        <span className="hint">
          Khung {f.w} × {f.h}. {hint}
        </span>
        {chooser("Chọn ảnh")}
        {problem !== null && <p className="small err-text">{problem}</p>}
        {!canUpload && <p className="reason">Cần quyền tải ảnh lên kho tệp.</p>}
      </div>
    );
  }
  return (
    <div className="img-row">
      <div
        className={`img-ph${slot === "mobile" ? " is-phone" : ""}${state.phase === "rejected" || state.phase === "missing" ? " is-bad" : state.phase === "ready" ? "" : " is-proc"}`}
      >
        {state.phase === "uploading"
          ? "Đang tải ảnh…"
          : state.phase === "processing"
            ? "Đang xử lý…"
            : state.phase === "ready"
              ? (name ?? "Ảnh đã tải")
              : "Ảnh không dùng được"}
      </div>
      <div className="stack-sm">
        {state.phase === "uploading" && <p className="hint">{name}</p>}
        {state.phase === "processing" && (
          <p className="small">
            <span className="chip">Đang xử lý ảnh</span>
          </p>
        )}
        {state.phase === "ready" && (
          <p className="small">
            <span className="chip chip-ok">Sẵn sàng</span>
            {w !== undefined && h !== undefined && (
              <>
                {" "}
                {w} × {h} · {warn === null ? "đúng tỉ lệ" : <b className="err-text">{warn}</b>}
              </>
            )}
          </p>
        )}
        {warn !== null && (
          <p className="hint">
            Vẫn lưu được; nên xuất lại ảnh đúng {f.w} × {f.h}.
          </p>
        )}
        {(state.phase === "rejected" || state.phase === "missing") && (
          <>
            <p className="small err-text">{state.message ?? "Ảnh không còn trên máy chủ. Tải ảnh khác"}</p>
            <p className="hint">Ảnh bị từ chối không dùng được; banner có ảnh này không lưu được.</p>
          </>
        )}
        {problem !== null && <p className="small err-text">{problem}</p>}
        {state.phase !== "uploading" && state.phase !== "processing" && (
          <div className="row">
            {chooser(state.phase === "ready" ? "Thay ảnh" : "Tải ảnh khác")}
            {remove}
          </div>
        )}
        {state.phase === "processing" && <div className="row">{remove}</div>}
        {state.phase === "ready" && <p className="hint">{hint}</p>}
      </div>
    </div>
  );
}
