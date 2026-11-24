// Tìm mẫu đang bán để ghim lên banner (≤ 3, thuộc nguồn của banner) hay làm đích "Một sản phẩm" của banner ảnh.
// Danh sách mẫu lấy từ listProducts (catalog.read); lọc trước theo nguồn nếu biết (chiến dịch theo sản phẩm, theo danh
// mục; banner danh mục gửi categoryId). Máy chủ vẫn kiểm (422 pin_not_in_source).
import { useState } from "react";
import type { HomeBannerList, ProductListItem } from "../../api";
import { errorMessage } from "../../app/errors";
import { useProductSearch } from "./bannerData";

/** Mẫu có thể chọn: id thuộc nguồn (undefined = không lọc). */
export type SourceFilter = ((p: ProductListItem) => boolean) | undefined;

/** Bộ lọc nguồn của banner giảm giá theo chiến dịch (mọi sản phẩm, sản phẩm chọn, danh mục chọn và con cháu). */
export function campaignFilter(list: HomeBannerList, campaignId: string | undefined): SourceFilter {
  const c = list.campaigns.find((x) => x.id === campaignId);
  if (c === undefined || c.scope === "all") return undefined;
  if (c.scope === "products") return (p) => c.targetIds.includes(p.id);
  const parent = new Map(list.categories.map((x) => [x.id, x.parentId]));
  const inScope = (id: string | undefined): boolean => {
    for (let cur = id, n = 0; cur !== undefined && n < 50; cur = parent.get(cur), n++)
      if (c.targetIds.includes(cur)) return true;
    return false;
  };
  return (p) => inScope(p.categoryId);
}

export function ProductPicker({
  label,
  verb,
  exclude,
  categoryId,
  filter,
  canSearch,
  disabled,
  onPick,
}: {
  label: string;
  /** Chữ đầu nút chọn ("Ghim", "Chọn"). */
  verb: string;
  exclude: readonly string[];
  categoryId: string | undefined;
  filter: SourceFilter;
  /** Có quyền catalog.read. */
  canSearch: boolean;
  disabled: boolean;
  onPick: (p: ProductListItem) => void;
}) {
  const [q, setQ] = useState("");
  const search = useProductSearch(q, categoryId, canSearch);
  if (!canSearch) return <p className="reason">Cần quyền xem sản phẩm để tìm mẫu.</p>;
  const items = (search.data?.items ?? []).filter((p) => !exclude.includes(p.id) && (filter?.(p) ?? true));
  return (
    <div className="stack-sm">
      <input
        className="input is-search"
        type="search"
        aria-label={label}
        placeholder="Tìm theo tên, mã mẫu"
        value={q}
        disabled={disabled}
        onChange={(e) => {
          setQ(e.target.value);
        }}
      />
      {search.isError && <p className="small err-text">{errorMessage(search.error)}</p>}
      {q.trim() !== "" && search.isSuccess && items.length === 0 && (
        <p className="small muted">Không có mẫu đang bán phù hợp.</p>
      )}
      {items.length > 0 && (
        <ul className="wbn-res">
          {items.map((p) => (
            <li key={p.id}>
              <button
                type="button"
                className="btn btn-quiet"
                aria-label={`${verb} ${p.name}`}
                disabled={disabled}
                onClick={() => {
                  onPick(p);
                }}
              >
                {p.name} <span className="muted small">{p.code}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
