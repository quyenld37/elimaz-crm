// Màn Sản phẩm — danh sách (kế hoạch M1 Task 50; mockup docs/mockups/m1/san-pham-danh-sach.html, trạng thái A–E).
// Tìm, lọc trạng thái/danh mục/nhãn, cột "Có thể bán" theo kho; trang theo con trỏ 50 mẫu, cuộn ảo theo cửa sổ (TanStack
// Virtual). Bộ lọc nằm trên URL nên quay lại từ trang sửa mẫu giữ nguyên. Khóa truy vấn bắt đầu bằng chủ đề
// catalog.product: báo hiệu realtime tải lại danh sách đang mở.
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useWindowVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import {
  getMe,
  listCategories,
  listProducts,
  listTags,
  listWarehouses,
  type ListProductsData,
  type ProductListItem,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { hasPermission } from "../../app/nav";
import { TOPICS, queryKey } from "../../app/queryClient";
import {
  availableView,
  categoryOptions,
  coverPicture,
  filterSummary,
  hasFilters,
  MAX_Q,
  priceText,
  readFilters,
  STATUS_CHIP,
  STATUS_FILTER,
  STATUSES,
  warehouseOptions,
  writeFilters,
  type Filters,
} from "./productRules";
import "./products.css";

export const TEXT_EMPTY_SHOP = "Chưa có mẫu nào";
const PAGE_SIZE = 50;
/** Chiều cao ước tính một dòng (bảng 1512) trước khi đo thật. */
const ROW_ESTIMATE = 75;
const SEARCH_DELAY_MS = 300;
const NEW_PRODUCT = "/san-pham/moi";
const IMPORT_EXCEL = "/nhap-excel";

type ListQuery = NonNullable<ListProductsData["query"]>;

function listQuery(f: Filters): ListQuery {
  const q: ListQuery = { limit: PAGE_SIZE };
  const text = f.q.trim();
  if (text !== "") q.q = text;
  if (f.status !== "") q.status = [f.status];
  if (f.categoryId !== "") q.categoryId = f.categoryId;
  if (f.tagId !== "") q.tagId = f.tagId;
  if (f.warehouseId !== "") q.warehouseId = f.warehouseId;
  return q;
}

export function ProductList() {
  const [sp, setSp] = useSearchParams();
  // Cùng khóa với khung (src/shell/App.tsx) nên không gọi lại /api/v1/me.
  const me = useQuery({ queryKey: queryKey("identity.me"), queryFn: () => unwrap(getMe()) });
  // Kho tính "Có thể bán": listWarehouses (đòi inventory.read; máy chủ trả kho người xem đọc được). Không có quyền đó
  // thì không gọi (sẽ 403) và không có cột. Cùng khóa với các màn Kho.
  const canReadStock = hasPermission(me.data?.grants ?? [], "inventory.read");
  const whQuery = useQuery({
    queryKey: queryKey("inventory.warehouses"),
    queryFn: () => unwrap(listWarehouses()),
    enabled: canReadStock,
  });
  const warehouses = useMemo(() => warehouseOptions(whQuery.data ?? []), [whQuery.data]);
  // Chờ danh sách kho (hay lỗi) rồi mới gọi listProducts, để không gọi một lần không kho rồi một lần có kho.
  const warehousesSettled = !canReadStock || !whQuery.isPending;
  const warehouseIds = useMemo(() => warehouses.map((w) => w.id), [warehouses]);
  const filters = readFilters(sp, warehouseIds);
  const setFilters = (next: Filters) => {
    setSp(writeFilters(next), { replace: true });
  };

  const cats = useQuery({ queryKey: queryKey("catalog.categories"), queryFn: () => unwrap(listCategories()) });
  const tags = useQuery({ queryKey: queryKey("catalog.tags"), queryFn: () => unwrap(listTags()) });
  const catOptions = useMemo(() => categoryOptions(cats.data ?? []), [cats.data]);
  const names = useMemo(
    () => ({
      category: new Map(catOptions.map((c) => [c.id, c.label])),
      tag: new Map((tags.data ?? []).map((t) => [t.id, t.name])),
    }),
    [catOptions, tags.data],
  );

  // Ô tìm: gõ xong 300 ms mới đổi URL (và gửi). URL đổi từ chỗ khác (Xóa bộ lọc, quay lại) thì ô theo URL.
  const [qInput, setQInput] = useState(filters.q);
  const [seenQ, setSeenQ] = useState(filters.q);
  if (filters.q !== seenQ) {
    setSeenQ(filters.q);
    setQInput(filters.q);
  }
  useEffect(() => {
    if (qInput === filters.q) return;
    const t = setTimeout(() => {
      setSp((prev) => writeFilters({ ...readFilters(prev, warehouseIds), q: qInput.slice(0, MAX_Q) }), {
        replace: true,
      });
    }, SEARCH_DELAY_MS);
    return () => {
      clearTimeout(t);
    };
  }, [qInput, filters.q, setSp, warehouseIds]);

  const query = listQuery(filters);
  const list = useInfiniteQuery({
    queryKey: queryKey(TOPICS.product, "list", query),
    enabled: warehousesSettled,
    queryFn: ({ pageParam }) =>
      unwrap(listProducts({ query: pageParam === "" ? query : { ...query, cursor: pageParam } })),
    // "" là trang đầu (không gửi cursor); nextCursor thiếu là hết.
    initialPageParam: "",
    getNextPageParam: (last) => last.nextCursor,
  });
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const showAvailable = filters.warehouseId !== "";
  const filtered = hasFilters(filters);

  const clearFilters = () => {
    setQInput("");
    setFilters({ q: "", status: "", categoryId: "", tagId: "", warehouseId: filters.warehouseId });
  };

  return (
    <>
      <div className="page-h">
        <div>
          <h1>Sản phẩm</h1>
        </div>
        <div className="row">
          <Link className="btn btn-line" to={IMPORT_EXCEL}>
            Nhập Excel
          </Link>
          <Link className="btn btn-dark" to={NEW_PRODUCT}>
            Thêm mẫu
          </Link>
        </div>
      </div>

      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          placeholder="Tìm theo mã mẫu, tên, SKU hoặc barcode"
          aria-label="Tìm sản phẩm"
          maxLength={MAX_Q}
          value={qInput}
          onChange={(e) => {
            setQInput(e.target.value);
          }}
        />
        {warehouses.length > 0 && (
          <div className="select-wrap">
            <select
              aria-label="Kho tính có thể bán"
              value={filters.warehouseId}
              onChange={(e) => {
                setFilters({ ...filters, warehouseId: e.target.value });
              }}
            >
              {warehouses.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.label}
                </option>
              ))}
            </select>
          </div>
        )}
        <div className="select-wrap">
          <select
            aria-label="Trạng thái"
            value={filters.status}
            onChange={(e) => {
              const v = e.target.value;
              setFilters({ ...filters, status: STATUSES.find((s) => s === v) ?? "" });
            }}
          >
            <option value="">Mọi trạng thái</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {STATUS_FILTER[s]}
              </option>
            ))}
          </select>
        </div>
        <div className="select-wrap">
          <select
            aria-label="Danh mục"
            value={filters.categoryId}
            onChange={(e) => {
              setFilters({ ...filters, categoryId: e.target.value });
            }}
          >
            <option value="">Mọi danh mục</option>
            {catOptions.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}
              </option>
            ))}
          </select>
        </div>
        <div className="select-wrap">
          <select
            aria-label="Nhãn"
            value={filters.tagId}
            onChange={(e) => {
              setFilters({ ...filters, tagId: e.target.value });
            }}
          >
            <option value="">Mọi nhãn</option>
            {(tags.data ?? []).map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      {filtered && (
        <div className="filters-on">
          {filters.status !== "" && (
            <FilterTag
              label={`Trạng thái: ${STATUS_FILTER[filters.status]}`}
              remove="Bỏ lọc trạng thái"
              onRemove={() => {
                setFilters({ ...filters, status: "" });
              }}
            />
          )}
          {filters.categoryId !== "" && (
            <FilterTag
              label={`Danh mục: ${names.category.get(filters.categoryId) ?? "đã chọn"}`}
              remove="Bỏ lọc danh mục"
              onRemove={() => {
                setFilters({ ...filters, categoryId: "" });
              }}
            />
          )}
          {filters.tagId !== "" && (
            <FilterTag
              label={`Nhãn: ${names.tag.get(filters.tagId) ?? "đã chọn"}`}
              remove="Bỏ lọc nhãn"
              onRemove={() => {
                setFilters({ ...filters, tagId: "" });
              }}
            />
          )}
          <button type="button" className="btn btn-text" onClick={clearFilters}>
            Xóa bộ lọc
          </button>
        </div>
      )}

      {list.isError && (
        <div className="box box-err" role="alert" style={{ marginBottom: 16 }}>
          <b>{errorMessage(list.error)}.</b>
        </div>
      )}
      {list.isPending && <p className="muted">Đang tải…</p>}

      {list.isSuccess && items.length === 0 && !filtered && (
        <div className="empty">
          <h2>{TEXT_EMPTY_SHOP}</h2>
          <p className="muted">Thêm từng mẫu, hoặc nhập nhiều mẫu một lần bằng tệp Excel theo mẫu của CRM.</p>
          <div className="row" style={{ justifyContent: "center" }}>
            <Link className="btn btn-line" to={IMPORT_EXCEL}>
              Nhập Excel
            </Link>
            <Link className="btn btn-dark" to={NEW_PRODUCT}>
              Thêm mẫu
            </Link>
          </div>
        </div>
      )}
      {list.isSuccess && items.length === 0 && filtered && (
        <div className="empty">
          <h2>{filters.q.trim() !== "" ? `Không có mẫu khớp "${filters.q.trim()}"` : "Không có mẫu khớp bộ lọc"}</h2>
          {filterSummary(filters, names) !== "" && <p className="muted">Đang lọc: {filterSummary(filters, names)}.</p>}
          <button type="button" className="btn btn-line" onClick={clearFilters}>
            Xóa bộ lọc
          </button>
        </div>
      )}

      {items.length > 0 && (
        <>
          <ProductTable
            items={items}
            showAvailable={showAvailable}
            canLoadMore={list.hasNextPage && !list.isFetchingNextPage && !list.isError}
            onNearEnd={() => void list.fetchNextPage()}
          />
          <div className="more">
            {list.hasNextPage ? (
              <>
                <span>Đang hiện {items.length} mẫu đầu. Cuộn xuống để tải tiếp.</span>
                <button
                  type="button"
                  className="btn btn-quiet"
                  disabled={list.isFetchingNextPage}
                  onClick={() => void list.fetchNextPage()}
                >
                  Tải thêm {PAGE_SIZE} mẫu
                </button>
              </>
            ) : (
              <span>Hết danh sách.</span>
            )}
          </div>
        </>
      )}
    </>
  );
}

function FilterTag({ label, remove, onRemove }: { label: string; remove: string; onRemove: () => void }) {
  return (
    <span className="tag">
      {label}{" "}
      <button type="button" aria-label={remove} onClick={onRemove}>
        ×
      </button>
    </span>
  );
}

interface ProductTableProps {
  items: readonly ProductListItem[];
  showAvailable: boolean;
  /** Còn trang sau và không đang tải: cuộn tới gần cuối thì gọi onNearEnd. */
  canLoadMore: boolean;
  onNearEnd: () => void;
}

/** Bảng cuộn ảo theo cửa sổ: chỉ dựng các dòng đang thấy (cộng 8 dòng mỗi phía), hai dòng đệm giữ chiều cao. */
function ProductTable({ items, showAvailable, canLoadMore, onNearEnd }: ProductTableProps) {
  const bodyRef = useRef<HTMLTableSectionElement>(null);
  const [margin, setMargin] = useState(0);
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (el !== null) setMargin(el.getBoundingClientRect().top + window.scrollY);
  }, []);
  const v = useWindowVirtualizer({
    count: items.length,
    estimateSize: () => ROW_ESTIMATE,
    overscan: 8,
    scrollMargin: margin,
    // Đo lại trong khung hình kế tiếp: tránh vòng ResizeObserver khi dòng đệm đổi cao ngay trong lúc đo.
    useAnimationFrameWithResizeObserver: true,
    // Dòng chưa có kích thước (chưa bố cục) giữ số ước tính, không coi là 0.
    measureElement: (el) => {
      const h = el.getBoundingClientRect().height;
      return h > 0 ? h : ROW_ESTIMATE;
    },
  });
  const rows = v.getVirtualItems();
  const last = rows.at(-1);
  const lastIndex = last?.index ?? -1;
  useEffect(() => {
    if (canLoadMore && lastIndex >= items.length - 1) onNearEnd();
  }, [canLoadMore, lastIndex, items.length, onNearEnd]);

  const cols = showAvailable ? 5 : 4;
  const first = rows.at(0);
  const padTop = first === undefined ? 0 : first.start - margin;
  const padBottom = last === undefined ? 0 : v.getTotalSize() - (last.end - margin);
  return (
    <div className="tbl-frame">
      <table className="tbl">
        <thead>
          <tr>
            <th>Mẫu</th>
            <th>Danh mục</th>
            <th className="r">Giá bán</th>
            {showAvailable && <th className="r">Có thể bán</th>}
            <th>Trạng thái</th>
          </tr>
        </thead>
        <tbody ref={bodyRef}>
          {padTop > 0 && (
            <tr className="v-pad" aria-hidden="true">
              <td colSpan={cols} style={{ height: padTop }} />
            </tr>
          )}
          {rows.map((r) => {
            const p = items.at(r.index);
            return (
              p !== undefined && (
                <ProductRow key={p.id} p={p} index={r.index} measure={v.measureElement} showAvailable={showAvailable} />
              )
            );
          })}
          {padBottom > 0 && (
            <tr className="v-pad" aria-hidden="true">
              <td colSpan={cols} style={{ height: padBottom }} />
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

interface ProductRowProps {
  p: ProductListItem;
  index: number;
  measure: (el: HTMLTableRowElement | null) => void;
  showAvailable: boolean;
}

function ProductRow({ p, index, measure, showAvailable }: ProductRowProps) {
  const chip = STATUS_CHIP[p.status];
  const price = priceText(p);
  return (
    <tr ref={measure} data-index={index} className={p.status === "discontinued" ? "is-muted" : undefined}>
      <td className="lead">
        <Link className="p-cell" to={`/san-pham/${encodeURIComponent(p.id)}`}>
          <Thumb p={p} />
          <span className="grow">
            <span className="code">{p.code}</span>
            <span className="clamp2">{p.name}</span>
          </span>
        </Link>
      </td>
      <td data-label="Danh mục">
        {p.categoryPath.length > 0 ? p.categoryPath.join(" > ") : <span className="muted">Chưa có danh mục</span>}
      </td>
      <td data-label="Giá bán" className={price.includes("–") ? "r money nowrap" : "r money"}>
        {price}
      </td>
      {showAvailable && <AvailableCell p={p} />}
      <td data-label="Trạng thái">
        <span className={chip.className}>{chip.label}</span>
      </td>
    </tr>
  );
}

function AvailableCell({ p }: { p: ProductListItem }) {
  const a = availableView(p);
  if (a === null) return <td data-label="Có thể bán" className="r num" />;
  if (a.tone === "short")
    return (
      <td data-label="Có thể bán" className="r">
        <span>
          <span className="num bold err-text">{a.text}</span>
          <br />
          <span className="xs err-text">{a.note}</span>
        </span>
      </td>
    );
  if (a.tone === "zero")
    return (
      <td data-label="Có thể bán" className="r">
        <span>
          <span className="num">{a.text}</span> <span className="xs muted">{a.note}</span>
        </span>
      </td>
    );
  return (
    <td data-label="Có thể bán" className="r num">
      {a.text}
    </td>
  );
}

function Thumb({ p }: { p: ProductListItem }) {
  const c = coverPicture(p.cover);
  if (c.kind === "none") return <span className="thumb is-empty">Chưa có ảnh</span>;
  if (c.kind === "lost") return <span className="thumb is-empty">Ảnh bị mất</span>;
  return (
    <span className="thumb">
      <picture>
        {c.sources.map((s) => (
          <source key={s.type} type={s.type} srcSet={s.srcSet} sizes="48px" />
        ))}
        <img src={c.src} srcSet={c.srcSet} sizes="48px" alt="" loading="lazy" width={48} height={60} />
      </picture>
    </span>
  );
}
