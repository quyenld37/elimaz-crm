// Màn Kho — tồn kho (kế hoạch M1 Task 51; mockup đã duyệt docs/mockups/m1/kho-ton.html, trạng thái A, B, C). Bảng tồn
// bốn ngăn (tồn, lỗi, đang giữ, đang giao) và có thể bán theo một kho; lọc "Thiếu so với phần giữ" (Q8) gửi
// onlyShort=true. Ô chọn kho chỉ có kho người xem được đọc (máy chủ lọc theo phạm vi; API vẫn kiểm từng yêu cầu).
import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { listBalances, listWarehouses, type Balance, type Warehouse } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { TOPICS, queryKey } from "../../app/queryClient";
import "./inventory.css";

export const PAGE_SIZE = 50;
/** Chờ người gõ dừng rồi mới tìm (ms). */
export const SEARCH_DELAY_MS = 300;

export const TEXT_SHORT_TOGGLE = "Thiếu so với phần giữ";
export const TEXT_NO_SHORT = "Không có mã hàng nào thiếu so với phần giữ";
export const TEXT_NO_MATCH = "Không có mã hàng nào khớp";
export const TEXT_NO_WAREHOUSE = "Bạn chưa được xem kho nào";

/** "−2" với dấu trừ thật (U+2212) như mockup. */
export function signed(n: number): string {
  return n < 0 ? `−${String(-n)}` : String(n);
}

/** Ngừng bán khi mẫu hoặc biến thể ngừng bán. */
export function discontinued(b: Balance): boolean {
  return b.productStatus === "discontinued" || b.variantStatus === "discontinued";
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => {
      setV(value);
    }, ms);
    return () => {
      clearTimeout(t);
    };
  }, [value, ms]);
  return v;
}

export function Balances() {
  const warehouses = useQuery({
    queryKey: queryKey("inventory.warehouses"),
    queryFn: () => unwrap(listWarehouses()),
  });
  const [picked, setPicked] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [onlyShort, setOnlyShort] = useState(false);
  const term = useDebounced(q.trim(), SEARCH_DELAY_MS);

  const list = warehouses.data ?? [];
  const wh: Warehouse | undefined = list.find((w) => w.id === picked) ?? list[0];

  const balances = useInfiniteQuery({
    queryKey: queryKey(TOPICS.balance, "list", { warehouseId: wh?.id, q: term, onlyShort }),
    enabled: wh !== undefined,
    // "" là trang đầu (không gửi cursor).
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      unwrap(
        listBalances({
          query: {
            warehouseId: wh?.id ?? "",
            limit: PAGE_SIZE,
            ...(term !== "" && { q: term }),
            ...(onlyShort && { onlyShort: true }),
            ...(pageParam !== "" && { cursor: pageParam }),
          },
        }),
      ),
    getNextPageParam: (last) => last.nextCursor,
    placeholderData: keepPreviousData,
  });

  const rows = balances.data?.pages.flatMap((p) => p.items) ?? [];
  const name = wh?.name ?? "";

  return (
    <>
      <div className="page-h">
        <div>
          <h1>Tồn kho</h1>
          <p className="sub">
            Có thể bán = Tồn − Lỗi − Đang giữ − Đang giao · số tự cập nhật khi có phiếu, đơn, kiểm kho
          </p>
        </div>
      </div>

      {warehouses.isPending && <p className="muted">Đang tải…</p>}
      {warehouses.isError && (
        <div className="box box-err" role="alert">
          {errorMessage(warehouses.error)}
        </div>
      )}
      {warehouses.isSuccess && wh === undefined && (
        <div className="empty">
          <h2>{TEXT_NO_WAREHOUSE}</h2>
          <p className="muted">Nhờ Chủ shop gán vai trò có quyền xem kho.</p>
        </div>
      )}

      {wh !== undefined && (
        <>
          <div className="toolbar">
            <div className="select-wrap">
              <select
                aria-label="Kho"
                value={wh.id}
                onChange={(e) => {
                  setPicked(e.target.value);
                }}
              >
                {list.map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.name}
                  </option>
                ))}
              </select>
            </div>
            <input
              className="input is-search"
              type="search"
              placeholder="Tìm theo SKU, barcode, mã mẫu, tên"
              aria-label="Tìm mã hàng"
              maxLength={100}
              value={q}
              onChange={(e) => {
                setQ(e.target.value);
              }}
            />
            <button
              type="button"
              className="toggle"
              aria-pressed={onlyShort}
              onClick={() => {
                setOnlyShort(!onlyShort);
              }}
            >
              {onlyShort ? `${TEXT_SHORT_TOGGLE} ×` : TEXT_SHORT_TOGGLE}
            </button>
          </div>

          {balances.isError && (
            <div className="box box-err" role="alert" style={{ marginBottom: 16 }}>
              {errorMessage(balances.error)}
            </div>
          )}
          {balances.isPending && <p className="muted">Đang tải…</p>}

          {balances.isSuccess && rows.length === 0 && (
            <EmptyState
              warehouse={name}
              onlyShort={onlyShort}
              searching={term !== ""}
              onClearShort={() => {
                setOnlyShort(false);
              }}
            />
          )}

          {rows.length > 0 && (
            <>
              {onlyShort && (
                <div className="box box-soft" style={{ marginBottom: 16 }}>
                  <b>Các mã dưới đang thiếu so với hàng đã giữ cho đơn.</b> Các đơn đã giữ vẫn giữ nguyên. Cần nhập thêm
                  hàng, hoặc báo khách đổi mẫu và hủy phần giữ ở đơn.
                </div>
              )}
              <div className="tbl-frame">
                <table className="tbl stk">
                  <thead>
                    <tr>
                      <th>Mã hàng</th>
                      <th className="r">Tồn</th>
                      <th className="r">Lỗi</th>
                      <th className="r">Đang giữ</th>
                      <th className="r">Đang giao</th>
                      <th className="r">Có thể bán</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((b) => (
                      <BalanceRow key={b.variantId} b={b} />
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="more">
                <span>
                  {balances.hasNextPage
                    ? `Đang hiện ${String(rows.length)} mã hàng đầu ở ${name}.`
                    : `Đang hiện cả ${String(rows.length)} mã hàng ở ${name}.`}
                </span>
                {balances.hasNextPage && (
                  <button
                    type="button"
                    className="btn btn-quiet"
                    disabled={balances.isFetchingNextPage}
                    onClick={() => void balances.fetchNextPage()}
                  >
                    {`Tải thêm ${String(PAGE_SIZE)}`}
                  </button>
                )}
              </div>
            </>
          )}
        </>
      )}
    </>
  );
}

function BalanceRow({ b }: { b: Balance }) {
  const off = discontinued(b);
  const short = b.available < 0;
  const cls = short ? "is-err" : off ? "is-muted" : undefined;
  return (
    <tr className={cls}>
      <td className="lead">
        <span className="v-cell">
          <span className="sku">
            {b.sku} · {b.barcode}
          </span>
          <span className="clamp2">
            {b.productName} · {b.color} · {b.size}
            {off && (
              <>
                {" "}
                <span className="chip chip-line">Ngừng bán</span>
              </>
            )}
          </span>
        </span>
      </td>
      <td data-label="Tồn" className="r num">
        {b.onHand}
      </td>
      <td data-label="Lỗi" className="r num">
        {b.damaged}
      </td>
      <td data-label="Đang giữ" className="r num">
        {b.held}
      </td>
      <td data-label="Đang giao" className="r num">
        {b.inTransit}
      </td>
      <AvailableCell n={b.available} />
    </tr>
  );
}

function AvailableCell({ n }: { n: number }) {
  if (n < 0) {
    return (
      <td data-label="Có thể bán" className="av r">
        <span>
          <span className="avail err-text">{signed(n)}</span>
          <br />
          <span className="xs err-text">{`Thiếu ${String(-n)} so với phần giữ`}</span>
        </span>
      </td>
    );
  }
  if (n === 0) {
    return (
      <td data-label="Có thể bán" className="av r">
        <span>
          <span className="avail">0</span> <span className="xs muted">Hết hàng</span>
        </span>
      </td>
    );
  }
  return (
    <td data-label="Có thể bán" className="av r avail">
      {n}
    </td>
  );
}

function EmptyState(p: { warehouse: string; onlyShort: boolean; searching: boolean; onClearShort: () => void }) {
  if (p.onlyShort) {
    return (
      <div className="empty">
        <h2>{TEXT_NO_SHORT}</h2>
        <button type="button" className="btn btn-line" onClick={p.onClearShort}>
          Tắt lọc
        </button>
      </div>
    );
  }
  if (p.searching) {
    return (
      <div className="empty">
        <h2>{TEXT_NO_MATCH}</h2>
      </div>
    );
  }
  return (
    <div className="empty">
      <h2>{`${p.warehouse} chưa có hàng`}</h2>
      <p className="muted">Tồn kho tăng khi ghi sổ phiếu nhập.</p>
    </div>
  );
}
