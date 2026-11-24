// Form sửa một bộ phối (mockup docs/mockups/m5/web-looks.html trạng thái B; HV20, HV21): 1 tên dịp và tên gợi ý,
// 2 sản phẩm trong ảnh (1–3, màu đang mặc), 3 ảnh người mẫu và chấm, 4 tiêu đề, câu mở đầu, câu chuyện, 5 danh mục thêm,
// người mẫu, 6 thời gian hiện. Mọi ô khóa bằng fieldset khi đang lưu hay chỉ có quyền xem.
import type { HomeBannerCategory, Look, LookItem, Product } from "../../api";
import { Counted, Step, VnTimeInput } from "../web-banners/listEditor";
import { ProductPicker } from "../web-banners/ProductPicker";
import { LookStage } from "./LookStage";
import { colorsOf } from "./lookData";
import {
  HEADLINE_MAX,
  MAX_CATEGORIES,
  MAX_ITEMS,
  NAME_MAX,
  NAME_TEMPLATES,
  SIZE_MAX,
  STORY_MAX,
  TITLE_MAX,
  type PhotoOpt,
} from "./lookRules";

export interface LookFormProps {
  look: Look;
  n: number;
  names: ReadonlyMap<string, string>;
  photos: readonly PhotoOpt[];
  details: ReadonlyMap<string, Product>;
  /** Màu theo mẫu từ danh sách (refColors); không có thì lấy từ chi tiết mẫu. */
  colors: ReadonlyMap<string, readonly string[]>;
  categories: readonly HomeBannerCategory[];
  autoTitle: string | undefined;
  disabled: boolean;
  /** Đã đủ 500 bộ: không nhân bản được. */
  full: boolean;
  canSearch: boolean;
  onChange: (l: Look) => void;
  onPickProduct: (id: string, name: string) => void;
  onRemoveProduct: (id: string) => void;
  onClone: () => void;
  onRemove: () => void;
}

const optional = <span className="muted">(không bắt buộc)</span>;

function Pieces(p: LookFormProps & { set: (x: Partial<Look>) => void }) {
  const { look } = p;
  const setItem = (k: number, it: LookItem) => {
    p.set({ items: look.items.map((x, j) => (j === k ? it : x)) });
  };
  return (
    <Step label={`2. Sản phẩm trong ảnh (1–${String(MAX_ITEMS)})`}>
      {look.items.map((it, k) => {
        const name = p.names.get(it.productId) ?? "Sản phẩm";
        const colors = p.colors.get(it.productId) ?? colorsOf(p.details.get(it.productId));
        const color = it.color ?? "";
        const options = color === "" || colors.includes(color) ? colors : [color, ...colors];
        return (
          <div className="piece" key={it.productId}>
            <span className="ord">{k + 1}</span>
            <b className="wrap-any">{name}</b>
            <span className="select-wrap">
              <select
                aria-label={`Màu trong ảnh của ${name}`}
                value={color}
                onChange={(e) => {
                  const base = { productId: it.productId, x: it.x, y: it.y };
                  setItem(k, e.target.value === "" ? base : { ...base, color: e.target.value });
                }}
              >
                <option value="">Tự chọn màu còn hàng</option>
                {options.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
            </span>
            <button
              type="button"
              className="ibtn"
              aria-label={`Bỏ ${name}`}
              onClick={() => {
                p.onRemoveProduct(it.productId);
              }}
            >
              ×
            </button>
          </div>
        );
      })}
      {look.items.length >= MAX_ITEMS ? (
        <input
          className="input is-search"
          placeholder="Đã đủ 3 sản phẩm — bỏ một món để thêm"
          aria-label="Tìm sản phẩm"
          disabled
        />
      ) : (
        <ProductPicker
          label="Tìm sản phẩm"
          verb="Chọn"
          exclude={look.items.map((i) => i.productId)}
          categoryId={undefined}
          filter={undefined}
          canSearch={p.canSearch}
          disabled={p.disabled}
          onPick={(x) => {
            p.onPickProduct(x.id, x.name);
          }}
        />
      )}
    </Step>
  );
}

function Categories(p: LookFormProps & { set: (x: Partial<Look>) => void }) {
  const ids = p.look.categoryIds ?? [];
  const nameOf = (id: string) => p.categories.find((c) => c.id === id)?.name ?? "Danh mục đã xóa";
  const left = p.categories.filter((c) => !ids.includes(c.id));
  return (
    <>
      {ids.length > 0 && (
        <div className="chips">
          {ids.map((id) => (
            <span className="tag" key={id}>
              {nameOf(id)}{" "}
              <button
                type="button"
                className="ibtn"
                aria-label={`Bỏ danh mục ${nameOf(id)}`}
                onClick={() => {
                  p.set({ categoryIds: ids.filter((x) => x !== id) });
                }}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      {ids.length < MAX_CATEGORIES && left.length > 0 && (
        <span className="select-wrap">
          <select
            aria-label="Thêm danh mục"
            value=""
            onChange={(e) => {
              if (e.target.value !== "") p.set({ categoryIds: [...ids, e.target.value] });
            }}
          >
            <option value="">Thêm danh mục…</option>
            {left.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </span>
      )}
      <p className="hint">Tối đa 6 danh mục; trống thì dùng danh mục của các món trong ảnh.</p>
    </>
  );
}

function Window({ look, set }: { look: Look; set: (x: Partial<Look>) => void }) {
  return (
    <div className="grid-f">
      <VnTimeInput
        label="Hiện từ (giờ Việt Nam)"
        value={look.startsAt}
        onChange={(iso) => {
          set({ startsAt: iso });
        }}
      />
      <VnTimeInput
        label="Hiện đến (giờ Việt Nam)"
        value={look.endsAt}
        onChange={(iso) => {
          set({ endsAt: iso });
        }}
      />
    </div>
  );
}

export function LookForm(p: LookFormProps) {
  const { look } = p;
  const set = (x: Partial<Look>) => {
    p.onChange({ ...look, ...x });
  };
  const name = look.name.trim() === "" ? "Bộ mới" : look.name;
  const height = look.modelHeightCm;
  return (
    <section className="card stack lf" aria-label="Sửa bộ phối">
      <div className="row-between">
        <h2 className="wrap-any">
          Bộ {p.n} · {name}
        </h2>
        <div className="row">
          <button type="button" className="btn btn-quiet" disabled={p.disabled || p.full} onClick={p.onClone}>
            Nhân bản
          </button>
          <button type="button" className="btn btn-danger" disabled={p.disabled} onClick={p.onRemove}>
            Xóa
          </button>
        </div>
      </div>
      <fieldset className="fs" disabled={p.disabled}>
        <Counted
          label="Tên dịp"
          caption="1. Tên dịp *"
          value={look.name}
          max={NAME_MAX}
          placeholder="Ví dụ: Đi làm ngày thường"
          onChange={(v) => {
            set({ name: v });
          }}
        />
        <div className="chips">
          {NAME_TEMPLATES.map((t) => (
            <button
              key={t}
              type="button"
              className="btn btn-quiet"
              onClick={() => {
                set({ name: t });
              }}
            >
              {t}
            </button>
          ))}
        </div>
        <Pieces {...p} set={set} />
        <Step label="3. Ảnh người mẫu">
          <LookStage
            look={look}
            photos={p.photos}
            names={p.names}
            disabled={p.disabled}
            onPhoto={(fileId) => {
              set({ photoFileId: fileId });
            }}
            onDot={(k, d) => {
              set({ items: look.items.map((it, j) => (j === k ? { ...it, ...d } : it)) });
            }}
          />
        </Step>
        <Step label={<>4. Dòng tiêu đề, câu mở đầu, câu chuyện {optional}</>}>
          <Counted
            label="Dòng tiêu đề"
            value={look.title ?? ""}
            max={TITLE_MAX}
            placeholder={`Tự động: ${p.autoTitle ?? "tên các món"}`}
            hint="trống thì dùng tên các món"
            onChange={(v) => {
              set({ title: v });
            }}
          />
          <Counted
            label="Câu mở đầu"
            value={look.headline ?? ""}
            max={HEADLINE_MAX}
            hint={`trống thì "Mặc gì cho ${name}?"`}
            onChange={(v) => {
              set({ headline: v });
            }}
          />
          <Counted
            label="Câu chuyện"
            value={look.story ?? ""}
            max={STORY_MAX}
            hint="trống thì không hiện"
            area
            onChange={(v) => {
              set({ story: v });
            }}
          />
        </Step>
        <Step label="5. Thêm lựa chọn cho dịp này">
          <Categories {...p} set={set} />
          <div className="grid-f">
            <label className="field">
              <span>Người mẫu cao (cm)</span>
              <input
                className="input num"
                type="number"
                inputMode="numeric"
                aria-label="Người mẫu cao (cm)"
                value={height === undefined || height === 0 ? "" : String(height)}
                onChange={(e) => {
                  const v = Number.parseInt(e.target.value, 10);
                  set({ modelHeightCm: Number.isNaN(v) ? undefined : v });
                }}
              />
              <span className="hint">130–200</span>
            </label>
            <label className="field">
              <span>Đang mặc size</span>
              <input
                className="input"
                aria-label="Đang mặc size"
                value={look.modelSize ?? ""}
                maxLength={SIZE_MAX}
                onChange={(e) => {
                  set({ modelSize: e.target.value });
                }}
              />
              <span className="hint">Tối đa 6 ký tự</span>
            </label>
          </div>
        </Step>
        <Step label={<>6. Thời gian hiện {optional}</>}>
          <Window look={look} set={set} />
        </Step>
      </fieldset>
    </section>
  );
}
