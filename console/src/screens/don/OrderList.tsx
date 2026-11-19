// Danh sách đơn /don (kế hoạch M2 Task M2-28; mockup docs/mockups/m2/don-danh-sach.html trạng thái A–C; HV14, HV15).
// Báo hiệu order.changed của đơn X: đơn X đang hiện thì chỉ đọc lại đúng dòng đó (tìm theo mã) và tô nền 5 giây; đơn X
// chưa hiện thì đọc đơn X, nhãn khớp bộ lọc mới tải lại trang đầu. Cả trang tự tải lại mỗi 20 giây (lưới an toàn khi
// mất báo hiệu) và khi nối lại (resync). Tải thêm theo con trỏ, không số trang.
import { useInfiniteQuery, useQuery, useQueryClient, type InfiniteData, type QueryKey } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import {
  getOrder,
  listOrderDuplicates,
  listOrders,
  listStaff,
  type ListOrdersData,
  type OrderLabel,
  type OrderListItem,
  type OrderPage,
} from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey, subscribeChanged } from "../../app/queryClient";
import { shortTime } from "../../app/time";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { displayPhone } from "../len-don/orderRules";
import { formatMoney } from "../products/productRules";
import { LABEL_BAR, LABEL_TEXT, isAttention, labelChip } from "./labels";
import { ORDER_TOPIC, usePerms, useSources } from "./orderData";
import "./don.css";

const PAGE = 50;
const BAR: readonly (OrderLabel | "all")[] = [...LABEL_BAR, "all"];
const REFRESH_MS = 20_000;
const FLASH_MS = 5_000;
const DAY_MS = 86_400_000;
const VN_OFFSET_MS = 7 * 3_600_000;

type Range = "7d" | "today" | "yesterday" | "30d" | "all";
const RANGE_TEXT: Readonly<Record<Range, string>> = {
  "7d": "7 ngày qua",
  today: "Hôm nay",
  yesterday: "Hôm qua",
  "30d": "30 ngày qua",
  all: "Mọi ngày",
};
const RANGES: readonly Range[] = ["7d", "today", "yesterday", "30d", "all"];

const EMPTY_HINT: Partial<Readonly<Record<OrderLabel, string>>> = {
  cancel_requested: "Đơn đã gửi hãng mà bị hủy sẽ nằm ở đây cho tới khi hãng xác nhận.",
  awaiting_discount_approval: "Đơn giảm tay vượt mức của shop sẽ nằm ở đây chờ Chủ hoặc Quản lý duyệt.",
  draft: "Đơn nháp giữ hàng tới khi hết hạn rồi tự hủy.",
};

// 0 giờ (giờ Việt Nam) của ngày cách ngày của now `daysAgo` ngày.
function vnMidnight(now: number, daysAgo: number): number {
  return Math.floor((now + VN_OFFSET_MS) / DAY_MS) * DAY_MS - VN_OFFSET_MS - daysAgo * DAY_MS;
}

function rangeQuery(r: Range, now: number): { createdFrom?: string; createdTo?: string } {
  const iso = (ms: number) => new Date(ms).toISOString();
  switch (r) {
    case "7d":
      return { createdFrom: iso(vnMidnight(now, 6)) };
    case "today":
      return { createdFrom: iso(vnMidnight(now, 0)) };
    case "yesterday":
      return { createdFrom: iso(vnMidnight(now, 1)), createdTo: iso(vnMidnight(now, 0)) };
    case "30d":
      return { createdFrom: iso(vnMidnight(now, 29)) };
    case "all":
      return {};
  }
}

// useVnToday trả 0 giờ (giờ Việt Nam) của ngày hiện tại và tự đổi sang ngày mới đúng lúc qua nửa đêm, để bộ lọc "Hôm
// nay", "Hôm qua"… của màn mở lâu không lệch một ngày (review độc lập M2, D-03).
function useVnToday(): number {
  const [today, setToday] = useState(() => vnMidnight(Date.now(), 0));
  useEffect(() => {
    const t = setTimeout(
      () => {
        setToday(vnMidnight(Date.now(), 0));
      },
      Math.max(today + DAY_MS - Date.now(), 0) + 1_000,
    );
    return () => {
      clearTimeout(t);
    };
  }, [today]);
  return today;
}

type Query = NonNullable<ListOrdersData["query"]>;

const timeText = (iso: string) => shortTime(iso).replace(" ", " · ");

function replaceRow(data: InfiniteData<OrderPage> | undefined, row: OrderListItem) {
  if (data === undefined) return data;
  return { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.map((o) => (o.id === row.id ? row : o)) })) };
}

/** Nghe order.changed: đơn đang hiện → đọc lại đúng dòng; đơn chưa hiện mà nhãn khớp bộ lọc → tải lại danh sách. */
function useRowRefresh(key: QueryKey, label: OrderLabel | "all") {
  const qc = useQueryClient();
  const [flash, setFlash] = useState<ReadonlySet<string>>(new Set());
  const ctx = useRef({ key, label });
  useEffect(() => {
    ctx.current = { key, label };
  });
  useEffect(() => {
    const timers = new Set<ReturnType<typeof setTimeout>>();
    const mark = (id: string) => {
      setFlash((s) => new Set(s).add(id));
      const t = setTimeout(() => {
        timers.delete(t);
        setFlash((s) => {
          const n = new Set(s);
          n.delete(id);
          return n;
        });
      }, FLASH_MS);
      timers.add(t);
    };
    const refresh = async (id: string) => {
      const { key: k, label: l } = ctx.current;
      const data = qc.getQueryData<InfiniteData<OrderPage>>(k);
      const row = data?.pages.flatMap((p) => p.items).find((o) => o.id === id);
      try {
        if (row !== undefined) {
          const page = await unwrap(listOrders({ query: { q: row.code, limit: 1 } }));
          const fresh = page.items.find((o) => o.id === id);
          if (fresh === undefined) return;
          qc.setQueryData<InfiniteData<OrderPage>>(k, (d) => replaceRow(d, fresh));
          mark(id);
          return;
        }
        const o = await unwrap(getOrder({ path: { id } }));
        if (l === "all" || o.label === l) void qc.invalidateQueries({ queryKey: k, exact: true });
      } catch {
        // Đọc lại hỏng (mạng, đơn đã không còn quyền xem): lần tự tải lại 20 giây sẽ đúng lại.
      }
    };
    const off = subscribeChanged(qc, (e) => {
      if (e.topic === ORDER_TOPIC) void refresh(e.entity.id);
    });
    return () => {
      off();
      for (const t of timers) clearTimeout(t);
    };
  }, [qc]);
  return flash;
}

export function OrderList() {
  const navigate = useNavigate();
  const perms = usePerms();
  const sources = useSources();
  const [label, setLabel] = useState<OrderLabel | "all">("new");
  const [text, setText] = useState("");
  const q = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const [source, setSource] = useState("");
  const [careBy, setCareBy] = useState("");
  const [range, setRange] = useState<Range>("7d");
  const [dup, setDup] = useState(false);
  const today = useVnToday();

  const query: Query = {
    ...(label === "all" ? {} : { label: [label] }),
    ...rangeQuery(range, today),
    ...(q === "" ? {} : { q }),
    ...(source === "" ? {} : { trafficSourceId: [source] }),
    ...(careBy === "" ? {} : { careBy }),
    ...(dup ? { duplicateSuspect: true } : {}),
    limit: PAGE,
  };
  const key = queryKey(ORDER_TOPIC, "list", query);
  const list = useInfiniteQuery({
    queryKey: key,
    meta: { realtimeManual: true },
    refetchInterval: REFRESH_MS,
    // "" là trang đầu (chưa có con trỏ).
    initialPageParam: "",
    getNextPageParam: (last: OrderPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listOrders({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const flash = useRowRefresh(key, label);
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];

  const dups = useQuery({
    queryKey: queryKey(ORDER_TOPIC, "duplicates", "count"),
    queryFn: () => unwrap(listOrderDuplicates({ query: { limit: 100 } })),
  });
  const dupCount = dups.data === undefined ? 0 : dups.data.items.length;
  const dupText =
    dupCount === 0 ? "Đơn trùng" : `Đơn trùng (${dups.data?.nextCursor === undefined ? String(dupCount) : "100+"})`;

  const canStaff = perms.can("identity.users.manage");
  const staff = useQuery({
    queryKey: queryKey("identity.staff"),
    enabled: canStaff,
    queryFn: () => unwrap(listStaff()),
  });

  const open = (id: string) => {
    void navigate(`/don/${id}`);
  };
  const labelName = label === "all" ? "Tất cả" : LABEL_TEXT[label];

  return (
    <div>
      <div className="page-h">
        <div>
          <h1>Đơn hàng</h1>
          <p className="sub">
            Tự cập nhật khi có đơn đổi
            {list.dataUpdatedAt > 0 && ` · lần cuối ${new Date(list.dataUpdatedAt).toLocaleTimeString("vi-VN")}`}
          </p>
        </div>
        <div className="row">
          <Link className="btn btn-line" to="/don/trung">
            {dupText}
          </Link>
          {perms.can("order.create") && (
            <Link className="btn btn-dark" to="/don/moi">
              Lên đơn
            </Link>
          )}
        </div>
      </div>

      <div className="scroll-x" role="group" aria-label="Lọc theo nhãn đơn">
        <div className="lblbar">
          {BAR.map((l) => (
            <button
              key={l}
              type="button"
              className={l !== "all" && isAttention(l) ? "lbl is-attn" : "lbl"}
              aria-pressed={label === l}
              onClick={() => {
                setLabel(l);
              }}
            >
              {l === "all" ? "Tất cả" : LABEL_TEXT[l]}
            </button>
          ))}
        </div>
      </div>
      <p className="hint" style={{ margin: "4px 0 16px" }}>
        Nhãn của vận chuyển (Đang đóng gói, Chờ thu gom, Đang chuyển, Thất bại, Đang chuyển hoàn) thêm ở M3. Nhãn viền
        đỏ là nhãn cần người xử lý.
      </p>

      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          placeholder="Mã đơn (DH…), SĐT hoặc 4 số cuối, tên khách"
          aria-label="Tìm đơn"
          maxLength={100}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
        <div className="select-wrap">
          <select
            aria-label="Nguồn đơn"
            value={source}
            onChange={(e) => {
              setSource(e.target.value);
            }}
          >
            <option value="">Mọi nguồn</option>
            {sources.items.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
        <div className="select-wrap">
          <select
            aria-label="Người phụ trách"
            value={careBy}
            onChange={(e) => {
              setCareBy(e.target.value);
            }}
          >
            <option value="">Mọi người</option>
            {perms.userId !== "" && <option value={perms.userId}>Của tôi</option>}
            {(staff.data ?? [])
              .filter((u) => u.id !== perms.userId && u.status === "active")
              .map((u) => (
                <option key={u.id} value={u.id}>
                  {u.displayName}
                </option>
              ))}
          </select>
        </div>
        <div className="select-wrap">
          <select
            aria-label="Ngày tạo"
            value={range}
            onChange={(e) => {
              const r = RANGES.find((x) => x === e.target.value);
              if (r !== undefined) setRange(r);
            }}
          >
            {RANGES.map((r) => (
              <option key={r} value={r}>
                {RANGE_TEXT[r]}
              </option>
            ))}
          </select>
        </div>
        <button
          type="button"
          className="toggle"
          aria-pressed={dup}
          onClick={() => {
            setDup(!dup);
          }}
        >
          Nghi trùng
        </button>
      </div>

      {list.isError && <div className="box box-err">{errorMessage(list.error)}</div>}
      {list.isSuccess && rows.length === 0 && (
        <div className="empty">
          {q === "" ? (
            <>
              <h2>{label === "all" ? "Không có đơn nào" : `Không có đơn "${labelName}"`}</h2>
              <p className="muted">
                {(label === "all" ? undefined : EMPTY_HINT[label]) ??
                  `Chưa có đơn nào trong ${RANGE_TEXT[range].toLowerCase()}.`}
              </p>
            </>
          ) : (
            <>
              <h2>Không tìm thấy đơn nào</h2>
              <p className="muted">
                {range === "all"
                  ? `Không có đơn khớp "${q}".`
                  : `Không có đơn khớp "${q}" trong ${RANGE_TEXT[range].toLowerCase()}.`}
              </p>
              {range !== "all" && (
                <button
                  type="button"
                  className="btn btn-line"
                  onClick={() => {
                    setRange("all");
                  }}
                >
                  Tìm trong mọi ngày
                </button>
              )}
            </>
          )}
        </div>
      )}
      {rows.length > 0 && (
        <div className="tbl-frame">
          <table className="tbl">
            <thead>
              <tr>
                <th>Mã đơn</th>
                <th>Khách</th>
                <th>Nhãn</th>
                <th className="r">Tổng</th>
                <th className="r">COD</th>
                <th className="r">Món</th>
                <th>Nguồn</th>
                <th>Phụ trách</th>
                <th>Tạo lúc</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((o) => (
                <tr
                  key={o.id}
                  className={flash.has(o.id) ? "o-row is-new" : "o-row"}
                  onClick={() => {
                    open(o.id);
                  }}
                >
                  <td className="lead">
                    <div className="o-code">
                      <Link className="cell-a bold num" to={`/don/${o.id}`}>
                        {o.code}
                      </Link>
                      {o.duplicateSuspect && <span className="chip chip-err">Nghi trùng</span>}
                    </div>
                  </td>
                  <td data-label="Khách">
                    <div className="o-cus">
                      <span className="ellip">{o.customerName}</span>
                      {o.shipPhone !== undefined && o.shipPhone !== "" && (
                        <span className="small muted num">{displayPhone(o.shipPhone)}</span>
                      )}
                    </div>
                  </td>
                  <td data-label="Nhãn">
                    <span className={labelChip(o.label)}>{LABEL_TEXT[o.label]}</span>
                  </td>
                  <td data-label="Tổng" className="r">
                    <span className="money">{formatMoney(o.grandTotal)}</span>
                  </td>
                  <td data-label="COD" className="r">
                    <span className="money">{formatMoney(o.codAmount)}</span>
                  </td>
                  <td data-label="Món" className="r num">
                    {o.itemCount}
                  </td>
                  <td data-label="Nguồn">{sources.nameOf(o.trafficSourceId)}</td>
                  <td data-label="Phụ trách" className={o.careBy === undefined ? "muted" : undefined}>
                    {o.careBy?.name ?? "Chưa giao ai"}
                  </td>
                  <td data-label="Tạo lúc" className="num nowrap">
                    {timeText(o.createdAt)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {rows.length > 0 && (
        <div className="more">
          <span>{`Đang hiện ${String(rows.length)} đơn mới nhất của nhãn "${labelName}"`}</span>
          {list.hasNextPage && (
            <button
              type="button"
              className="btn btn-line"
              disabled={list.isFetchingNextPage}
              onClick={() => {
                void list.fetchNextPage();
              }}
            >
              Tải thêm 50 đơn
            </button>
          )}
        </div>
      )}
    </div>
  );
}
