// Ô thêm sản phẩm của màn Lên đơn (mockup docs/mockups/m2/len-don.html trạng thái A, B). Tìm bằng searchVariants (G4:
// barcode đúng, SKU, mã mẫu, tên không dấu). "Có thể bán" là số của kho mặc định (kho chính, đầu danh sách
// listWarehouses) đọc bằng listBalances, chỉ khi người dùng có inventory.read; không có thì chỉ hiện đang bán hay không.
// Biến thể ngừng bán hiện mờ, không chọn được (máy chủ trả variant_not_for_sale).
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useId, useState, type RefObject } from "react";
import { listBalances, listWarehouses, searchVariants, type VariantSearchHit } from "../../api";
import { unwrap } from "../../app/errors";
import { TOPICS, queryKey } from "../../app/queryClient";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { formatMoney } from "../products/productRules";
import { useCombo } from "./useCombo";

/** Số gợi ý tối đa. */
export const VARIANT_LIMIT = 8;

export interface VariantPickerProps {
  /** Chọn biến thể; available là số có thể bán ở kho mặc định (null khi không đọc được). */
  onPick: (h: VariantSearchHit, available: number | null) => void;
  inputRef: RefObject<HTMLInputElement | null>;
  /** Có inventory.read: đọc số có thể bán. */
  canReadStock: boolean;
}

/** "Đen · M" (bỏ phần rỗng). */
export const optionText = (h: Pick<VariantSearchHit, "color" | "size">) =>
  [h.color, h.size].filter(Boolean).join(" · ");

/**
 * Số có thể bán ở kho mặc định theo chữ tìm (một trang listBalances). map null khi không có quyền hay chưa tải;
 * complete: trang đã đủ (không nextCursor), chỉ khi đó biến thể vắng mặt mới chắc là không có số dư (0).
 */
function useAvailable(term: string, enabled: boolean): { map: Map<string, number> | null; complete: boolean } {
  const warehouses = useQuery({
    queryKey: queryKey("inventory.warehouses"),
    enabled,
    queryFn: () => unwrap(listWarehouses()),
  });
  const main = warehouses.data?.[0];
  const balances = useQuery({
    queryKey: queryKey(TOPICS.balance, "order-form", { warehouseId: main?.id, q: term }),
    enabled: enabled && main !== undefined && term !== "",
    queryFn: () => unwrap(listBalances({ query: { warehouseId: main?.id ?? "", q: term, limit: 100 } })),
  });
  if (!balances.isSuccess) return { map: null, complete: false };
  return {
    map: new Map(balances.data.items.map((b) => [b.variantId, b.available])),
    complete: balances.data.nextCursor === undefined,
  };
}

export function VariantPicker({ onPick, inputRef, canReadStock }: VariantPickerProps) {
  const listId = useId();
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const term = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const found = useQuery({
    queryKey: queryKey(TOPICS.product, "variant-search", term),
    enabled: open && term !== "",
    queryFn: () => unwrap(searchVariants({ query: { q: term, limit: VARIANT_LIMIT } })),
    placeholderData: keepPreviousData,
  });
  const { map, complete } = useAvailable(term, canReadStock && open);
  const hits = open && term !== "" && text.trim() !== "" ? (found.data ?? []).slice(0, VARIANT_LIMIT) : [];
  // Biến thể không có dòng số dư ở kho mặc định: có thể bán 0 nếu trang số dư đã đủ; còn trang sau thì chưa biết (null),
  // không báo "hết hàng" nhầm.
  const availableOf = (h: VariantSearchHit) => {
    if (map === null) return null;
    return map.get(h.variantId) ?? (complete ? 0 : null);
  };

  const pick = (i: number) => {
    const h = hits[i];
    if (!h?.sellable) return;
    onPick(h, availableOf(h));
    setText("");
    setOpen(false);
  };
  const nav = useCombo(setOpen, hits.length, pick);

  return (
    <div className="combo" onBlur={nav.onBlur}>
      <input
        ref={inputRef}
        className="input is-search"
        type="search"
        role="combobox"
        aria-label="Thêm sản phẩm theo mã, tên hoặc barcode"
        aria-expanded={hits.length > 0}
        aria-controls={listId}
        aria-autocomplete="list"
        autoComplete="off"
        maxLength={100}
        placeholder="Mã, tên hoặc quét barcode (F3)"
        value={text}
        {...nav.input((s) => {
          setText(s);
        })}
      />
      {hits.length > 0 && (
        <div className="sugg" role="listbox" id={listId} aria-label="Kết quả tìm sản phẩm" onMouseDown={nav.keepFocus}>
          {hits.map((h, i) => {
            const n = availableOf(h);
            return (
              <div
                key={h.variantId}
                className="sugg-i"
                role="option"
                aria-selected={nav.active === i}
                aria-disabled={!h.sellable}
                onClick={() => {
                  pick(i);
                }}
              >
                <span className="ellip">
                  <b>{h.sku}</b> · {[h.productName, optionText(h)].filter(Boolean).join(" · ")}
                </span>
                {!h.sellable ? (
                  <span className="small err-text">ngừng bán</span>
                ) : n !== null && n <= 0 ? (
                  <span className="small err-text">hết hàng</span>
                ) : (
                  <span className="small">
                    {n === null ? formatMoney(h.price) : `${formatMoney(h.price)} · có thể bán ${String(n)}`}
                  </span>
                )}
              </div>
            );
          })}
        </div>
      )}
      {found.isError && open && <p className="field-err">Chưa tìm được sản phẩm. Thử lại sau ít giây.</p>}
    </div>
  );
}
