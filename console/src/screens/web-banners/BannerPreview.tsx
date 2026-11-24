// Xem trước banner trên máy tính / điện thoại (mockup trạng thái D): khung đúng tỉ lệ 2368 × 600 hay 1080 × 640, vạch
// đứt là vùng an toàn; banner ẩn ghi lý do. Chỉ vẽ phần CRM có: chữ đã nhập (trống → "chữ tự động" của website), màu
// nền, số sản phẩm ghim, ảnh đã xử lý. Sản phẩm và giá thật do website tự lấy (Task 464), không vẽ ở đây.
import { useState } from "react";
import type { HomeBanner, HomeBannerList, HomeBannerStatus, HomeBannerTheme } from "../../api";
import { FRAME, HIDDEN_TEXT, KIND_TEXT, type Slot } from "./bannerRules";

/** Màu tự động như website: đỏ cho giảm giá, đen cho xu hướng, sáng cho loại khác. */
const autoTheme = (b: HomeBanner): Exclude<HomeBannerTheme, "auto"> =>
  b.kind === "campaign" ? "red" : b.kind === "trending" ? "black" : "light";

/** Đường dẫn ảnh đã xử lý của fileId (ảnh đã lưu: images của getBanners; ảnh mới: urls của màn). */
export type ImageUrl = (fileId: string | undefined, slot: Slot) => string | undefined;

function Frame({
  b,
  n,
  device,
  status,
  url,
}: {
  b: HomeBanner;
  n: number;
  device: Slot;
  status: HomeBannerStatus | undefined;
  url: ImageUrl;
}) {
  const desk = device === "desktop";
  const f = FRAME[device];
  const cap = `Banner ${String(n)}${desk ? "" : " trên điện thoại"} · khung ${String(f.w)} × ${String(f.h)}, vạch đứt là vùng an toàn`;
  if (status !== undefined && !status.shown) {
    return (
      <div className="pv-frame">
        <div className={`bn-pv ${desk ? "is-desk" : "is-phone"} is-hidden`}>
          <span className="t">Banner này đang ẩn</span>
          {status.reason !== undefined && <span className="small">{HIDDEN_TEXT[status.reason]}</span>}
        </div>
        <p className="xs muted">Banner {n}</p>
      </div>
    );
  }
  if (b.kind === "image") {
    const src = desk ? url(b.imageDesktop, "desktop") : (url(b.imageMobile, "mobile") ?? url(b.imageDesktop, "mobile"));
    return (
      <div className="pv-frame">
        <div className={`bn-pv ${desk ? "is-desk" : "is-phone"} is-img`}>
          {src === undefined ? (
            <span className="small">Chưa có ảnh đã xử lý</span>
          ) : (
            <img src={src} alt={b.alt ?? ""} />
          )}
          <span className="safe" />
        </div>
        <p className="xs muted">{cap}</p>
      </div>
    );
  }
  const theme = b.theme === undefined || b.theme === "auto" ? autoTheme(b) : b.theme;
  const pins = b.productIds?.length ?? 0;
  return (
    <div className="pv-frame">
      <div className={`bn-pv ${desk ? "is-desk" : "is-phone"} is-${theme}`}>
        <span className="safe" />
        <span className="k">
          {b.kicker !== undefined && b.kicker !== "" ? b.kicker : `${KIND_TEXT[b.kind].toUpperCase()} (chữ tự động)`}
        </span>
        <span className="t">
          {b.title !== undefined && b.title !== ""
            ? b.title.split(",").map((part, i) => (
                <span key={i} className="line">
                  {part.trim()}
                </span>
              ))
            : "Tiêu đề tự động"}
        </span>
        <span className="small">
          {b.cta !== undefined && b.cta !== "" ? b.cta : "Nút: chữ tự động"}
          {pins > 0 ? ` · ${String(pins)} sản phẩm ghim` : ""}
        </span>
      </div>
      <p className="xs muted">{cap}</p>
    </div>
  );
}

export function BannerPreview({
  banners,
  statuses,
  url,
  error,
}: {
  banners: readonly HomeBanner[];
  statuses: ReadonlyMap<string, HomeBannerStatus>;
  url: ImageUrl;
  /** Câu lỗi của lần xem trước cuối (luật sai: 422), null khi không có. */
  error: string | null;
}) {
  const [device, setDevice] = useState<Slot>("desktop");
  return (
    <section className="card stack" aria-label="Xem trước">
      <div className="row-between">
        <h2>Xem trước</h2>
        <div className="row" role="group" aria-label="Thiết bị">
          <button
            type="button"
            className="toggle"
            aria-pressed={device === "desktop"}
            onClick={() => {
              setDevice("desktop");
            }}
          >
            Máy tính
          </button>
          <button
            type="button"
            className="toggle"
            aria-pressed={device === "mobile"}
            onClick={() => {
              setDevice("mobile");
            }}
          >
            Điện thoại
          </button>
        </div>
      </div>
      <p className="hint">Tự làm mới 0,35 giây sau khi ngừng sửa. Sản phẩm và giá thật do website lấy khi hiện.</p>
      {error !== null && <div className="box box-err">Chưa xem trước được: {error}</div>}
      {banners.length === 0 && <p className="small muted">Chưa có banner để xem trước.</p>}
      {banners.map((b, i) => (
        <Frame key={b.id} b={b} n={i + 1} device={device} status={statuses.get(b.id)} url={url} />
      ))}
    </section>
  );
}

/** Đường ảnh của ảnh đã lưu theo images của getBanners: máy tính dùng bản rộng 2400, điện thoại bản 1200. */
export function savedImageUrl(images: HomeBannerList["images"]): ImageUrl {
  return (fileId, slot) => {
    const img = images.find((x) => x.fileId === fileId && x.status === "ready" && !x.deleted);
    return slot === "desktop" ? img?.desktop : (img?.desktopSmall ?? img?.desktop);
  };
}
