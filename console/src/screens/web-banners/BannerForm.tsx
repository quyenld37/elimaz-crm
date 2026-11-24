// Form của banner đang chọn (mockup trạng thái A: banner ảnh; B: banner giảm giá). Mọi thay đổi chỉ sửa danh sách đang
// sửa trên màn (onChange); lưu là lưu cả danh sách (BannerEditor). Chữ để trống = website tự điền.
import type { ReactNode } from "react";
import type { HomeBanner, HomeBannerKind, HomeBannerLinkKind, HomeBannerList } from "../../api";
import { BannerImage, type BannerImageProps } from "./BannerImage";
import {
  ALT_MAX,
  CTA_MAX,
  KICKER_MAX,
  KINDS,
  KIND_LONG,
  KIND_TEXT,
  MAX_PINS,
  THEMES,
  THEME_TEXT,
  TITLE_MAX,
  campaignWindow,
  type Slot,
} from "./bannerRules";
import { Counted, Step, VnTimeInput } from "./listEditor";
import { ProductPicker, campaignFilter } from "./ProductPicker";

const LINK_TEXT: Readonly<Record<HomeBannerLinkKind, string>> = {
  offers: "Trang Ưu đãi",
  campaign: "Chiến dịch giảm giá",
  category: "Danh mục",
  product: "Một sản phẩm",
};
const LINKS: readonly HomeBannerLinkKind[] = ["offers", "campaign", "category", "product"];
const isKind = (v: string): v is HomeBannerKind => KINDS.some((k) => k === v);
const isLink = (v: string): v is HomeBannerLinkKind => LINKS.some((k) => k === v);

export interface BannerFormProps {
  banner: HomeBanner;
  n: number;
  list: HomeBannerList;
  /** Tên mẫu theo mã (mẫu getBanners nhắc và mẫu vừa chọn trên màn). */
  names: ReadonlyMap<string, string>;
  disabled: boolean;
  /** Đã đủ 8 banner: không nhân bản được. */
  full: boolean;
  canSearch: boolean;
  onChange: (b: HomeBanner) => void;
  onKind: (k: HomeBannerKind) => void;
  onPickProduct: (id: string, name: string) => void;
  onClone: () => void;
  onRemove: () => void;
  /** Ô ảnh của banner ảnh (BannerEditor nối với tải lên). */
  image: (slot: Slot) => Omit<BannerImageProps, "slot" | "disabled">;
}

function Window({
  b,
  set,
  campaign,
}: {
  b: HomeBanner;
  set: (p: Partial<HomeBanner>) => void;
  campaign: { startsAt: string; endsAt: string } | undefined;
}) {
  const time = (label: string, v: string | undefined, key: "startsAt" | "endsAt") => (
    <VnTimeInput
      label={label}
      value={v}
      onChange={(iso) => {
        set({ [key]: iso });
      }}
    />
  );
  return (
    <>
      <div className="grid-f">
        {time("Hiện từ (giờ Việt Nam)", b.startsAt, "startsAt")}
        {time("Hiện đến (giờ Việt Nam)", b.endsAt, "endsAt")}
      </div>
      <div className="row">
        <button
          type="button"
          className="btn btn-quiet"
          onClick={() => {
            set({ startsAt: undefined, endsAt: undefined });
          }}
        >
          Luôn hiện
        </button>
        {campaign !== undefined && (
          <button
            type="button"
            className="btn btn-quiet"
            onClick={() => {
              set(campaignWindow(campaign));
            }}
          >
            Theo thời gian chiến dịch
          </button>
        )}
      </div>
    </>
  );
}

/** Ô chọn một mục của danh sách tham chiếu (chiến dịch, danh mục) của getBanners; rỗng = chưa chọn. */
function RefSelect({
  value,
  options,
  label,
  empty,
  hint,
  onChange,
}: {
  value: string | undefined;
  options: readonly { id: string; name: string }[];
  label: string;
  empty: string;
  hint?: ReactNode;
  onChange: (id: string | undefined) => void;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      <span className="select-wrap">
        <select
          value={value ?? ""}
          onChange={(e) => {
            onChange(e.target.value === "" ? undefined : e.target.value);
          }}
        >
          <option value="">{empty}</option>
          {options.map((x) => (
            <option key={x.id} value={x.id}>
              {x.name}
            </option>
          ))}
        </select>
      </span>
      {hint !== undefined && <span className="hint">{hint}</span>}
    </label>
  );
}

interface SelectProps {
  value: string | undefined;
  list: HomeBannerList;
  label: string;
  onChange: (id: string | undefined) => void;
}

function CampaignSelect({ value, list, label, onChange }: SelectProps) {
  const c = list.campaigns.find((x) => x.id === value);
  const hint =
    c === undefined
      ? undefined
      : `${c.running ? "Đang áp dụng." : "Chưa áp dụng (bản nháp, tạm dừng, chưa tới giờ hoặc hết hạn)."} Banner tự ẩn khi chiến dịch tạm dừng hoặc hết hạn.`;
  return (
    <RefSelect
      value={value}
      options={list.campaigns}
      label={label}
      empty="Chọn chiến dịch"
      {...(hint === undefined ? {} : { hint })}
      onChange={onChange}
    />
  );
}

function CategorySelect({ value, list, label, onChange }: SelectProps) {
  return <RefSelect value={value} options={list.categories} label={label} empty="Chọn danh mục" onChange={onChange} />;
}

function Pins(p: BannerFormProps & { set: (x: Partial<HomeBanner>) => void }) {
  const b = p.banner;
  const pins = b.productIds ?? [];
  const full = pins.length >= MAX_PINS;
  return (
    <Step
      label={
        <>
          4. Sản phẩm trên banner{" "}
          <span className="muted">(ghim tối đa 3; chỗ còn lại tự lấy theo nguồn của banner)</span>
        </>
      }
    >
      {pins.length > 0 && (
        <div className="pins">
          {pins.map((id) => {
            const name = p.names.get(id) ?? `Mẫu …${id.slice(-8)}`;
            return (
              <span key={id} className="tag">
                {name}
                <button
                  type="button"
                  aria-label={`Bỏ ghim ${name}`}
                  onClick={() => {
                    p.set({ productIds: pins.filter((x) => x !== id) });
                  }}
                >
                  ×
                </button>
              </span>
            );
          })}
        </div>
      )}
      {full ? (
        <p className="small muted">Đã ghim đủ 3 sản phẩm. Chỉ ghim được sản phẩm thuộc nguồn của banner.</p>
      ) : (
        <ProductPicker
          label="Tìm sản phẩm để ghim"
          verb="Ghim"
          exclude={pins}
          categoryId={b.kind === "category" ? b.categoryId : undefined}
          filter={b.kind === "campaign" ? campaignFilter(p.list, b.campaignId) : undefined}
          canSearch={p.canSearch}
          disabled={p.disabled}
          onPick={(x) => {
            p.onPickProduct(x.id, x.name);
            p.set({ productIds: [...pins, x.id] });
          }}
        />
      )}
    </Step>
  );
}

function TextBanner(p: BannerFormProps & { set: (x: Partial<HomeBanner>) => void }) {
  const b = p.banner;
  const campaign = p.list.campaigns.find((x) => x.id === b.campaignId);
  return (
    <>
      {b.kind === "campaign" && (
        <CampaignSelect
          value={b.campaignId}
          list={p.list}
          label="Sản phẩm lấy từ chiến dịch"
          onChange={(id) => {
            p.set({ campaignId: id, productIds: [] });
          }}
        />
      )}
      {b.kind === "category" && (
        <CategorySelect
          value={b.categoryId}
          list={p.list}
          label="Sản phẩm lấy từ danh mục"
          onChange={(id) => {
            p.set({ categoryId: id, productIds: [] });
          }}
        />
      )}
      <Step label="2. Màu nền">
        <div className="wbn-radios" role="radiogroup" aria-label="Màu nền">
          {THEMES.map((t) => (
            <label key={t} className="check">
              <input
                type="radio"
                name={`theme-${b.id}`}
                checked={(b.theme ?? "auto") === t}
                onChange={() => {
                  p.set({ theme: t });
                }}
              />{" "}
              {THEME_TEXT[t]}
            </label>
          ))}
        </div>
        <p className="hint">Tự động: đỏ cho giảm giá, đen cho xu hướng, sáng cho loại khác.</p>
      </Step>
      <Step
        label={
          <>
            3. Chữ trên banner <span className="muted">(để trống thì dùng chữ tự động)</span>
          </>
        }
      >
        <div className="grid-f">
          <Counted
            label="Dòng nhỏ phía trên"
            value={b.kicker ?? ""}
            max={KICKER_MAX}
            placeholder="Tự động"
            onChange={(v) => {
              p.set({ kicker: v });
            }}
          />
          <Counted
            label="Tiêu đề (dấu phẩy để xuống dòng)"
            value={b.title ?? ""}
            max={TITLE_MAX}
            placeholder="Tự động"
            onChange={(v) => {
              p.set({ title: v });
            }}
          />
          <Counted
            label="Chữ trên nút"
            value={b.cta ?? ""}
            max={CTA_MAX}
            placeholder="Tự động: Mua ngay"
            onChange={(v) => {
              p.set({ cta: v });
            }}
          />
        </div>
      </Step>
      <Pins {...p} />
      <Step label="5. Thời gian hiện">
        <Window b={b} set={p.set} campaign={campaign} />
      </Step>
    </>
  );
}

function LinkTarget(p: BannerFormProps & { set: (x: Partial<HomeBanner>) => void }) {
  const b = p.banner;
  const kind = b.linkKind ?? "offers";
  const productName = b.linkId === undefined ? undefined : p.names.get(b.linkId);
  return (
    <Step label="4. Bấm vào banner thì đến">
      <div className="grid-f">
        <span className="select-wrap">
          <select
            aria-label="Loại đích"
            value={kind}
            onChange={(e) => {
              if (isLink(e.target.value)) p.set({ linkKind: e.target.value, linkId: undefined });
            }}
          >
            {LINKS.map((k) => (
              <option key={k} value={k}>
                {LINK_TEXT[k]}
              </option>
            ))}
          </select>
        </span>
        {kind === "offers" && <span className="hint">Khách đến trang Ưu đãi, nơi có mọi chương trình đang chạy.</span>}
      </div>
      {kind === "campaign" && (
        <CampaignSelect
          value={b.linkId}
          list={p.list}
          label="Chiến dịch đích"
          onChange={(id) => {
            p.set({ linkId: id });
          }}
        />
      )}
      {kind === "category" && (
        <CategorySelect
          value={b.linkId}
          list={p.list}
          label="Danh mục đích"
          onChange={(id) => {
            p.set({ linkId: id });
          }}
        />
      )}
      {kind === "product" && (
        <>
          {b.linkId !== undefined && (
            <p className="small">
              Sản phẩm đích: <b>{productName ?? `Mẫu …${b.linkId.slice(-8)}`}</b>
            </p>
          )}
          <ProductPicker
            label="Tìm sản phẩm đích"
            verb="Chọn"
            exclude={b.linkId === undefined ? [] : [b.linkId]}
            categoryId={undefined}
            filter={undefined}
            canSearch={p.canSearch}
            disabled={p.disabled}
            onPick={(x) => {
              p.onPickProduct(x.id, x.name);
              p.set({ linkId: x.id });
            }}
          />
        </>
      )}
    </Step>
  );
}

function ImageBanner(p: BannerFormProps & { set: (x: Partial<HomeBanner>) => void }) {
  const b = p.banner;
  const linked = b.linkKind === "campaign" ? p.list.campaigns.find((x) => x.id === b.linkId) : undefined;
  return (
    <>
      <Step label="2. Ảnh cho máy tính *">
        <BannerImage slot="desktop" disabled={p.disabled} {...p.image("desktop")} />
      </Step>
      <Step label="3. Ảnh cho điện thoại (nên có)">
        <BannerImage slot="mobile" disabled={p.disabled} {...p.image("mobile")} />
      </Step>
      <LinkTarget {...p} />
      <div className="step">
        <Counted
          label="5. Mô tả ảnh * (cho Google và người dùng đọc màn hình)"
          value={b.alt ?? ""}
          max={ALT_MAX}
          onChange={(v) => {
            p.set({ alt: v });
          }}
        />
      </div>
      <Step label="6. Thời gian hiện">
        <Window b={b} set={p.set} campaign={linked} />
        <p className="hint">
          Chữ và giá trong ảnh không tự đổi theo CRM. Đặt giờ kết thúc trùng với chương trình để banner tự tắt đúng lúc.
        </p>
      </Step>
    </>
  );
}

export function BannerForm(p: BannerFormProps) {
  const b = p.banner;
  const set = (x: Partial<HomeBanner>) => {
    p.onChange({ ...b, ...x });
  };
  return (
    <fieldset className="card stack wbn-form" disabled={p.disabled} aria-label={`Sửa banner ${String(p.n)}`}>
      <div className="row-between">
        <h2>
          Banner {p.n} · {KIND_TEXT[b.kind]}
        </h2>
        <div className="row">
          <button type="button" className="btn btn-quiet" onClick={p.onClone} disabled={p.disabled || p.full}>
            Nhân bản
          </button>
          <button type="button" className="btn btn-danger" onClick={p.onRemove} disabled={p.disabled}>
            Xóa
          </button>
        </div>
      </div>
      <label className="field">
        <span>1. Loại banner</span>
        <span className="select-wrap">
          <select
            value={b.kind}
            onChange={(e) => {
              if (isKind(e.target.value)) p.onKind(e.target.value);
            }}
          >
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {KIND_LONG[k]}
              </option>
            ))}
          </select>
        </span>
        {b.kind === "image" && (
          <span className="hint">
            Tải banner designer đã làm sẵn (chữ, giá, hình đều nằm trong ảnh). Website hiện đúng ảnh đó.
          </span>
        )}
      </label>
      {b.kind === "image" ? <ImageBanner {...p} set={set} /> : <TextBanner {...p} set={set} />}
    </fieldset>
  );
}
