// Danh sách kiện hàng hoàn của đơn cũ đã nhập, theo ngày (Task 273; mockup docs/mockups/m7/hang-hoan-don-cu.html,
// trạng thái C; HV11). Mặc định 7 ngày gần nhất (giờ Việt Nam); còn trang sau thì không coi là hết.
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { listLegacyReturns, listWarehouses, type LegacyReturn } from "../../../api";
import { unwrap } from "../../../app/errors";
import { queryKey } from "../../../app/queryClient";
import { shortTime } from "../../../app/time";
import { useQuery } from "@tanstack/react-query";
import { fmtInt } from "../../imports/importRules";
import { RetryBox } from "../m7Parts";
import { dayTitle, formatVnDate, groupByDay, parseVnDate, returnCounts, vnToday } from "./legacyRules";

const PAGE = 100;

export function LegacyReturnList(props: { focus: { day: string; id: string } | null; refreshKey: number }) {
  const { focus, refreshKey } = props;
  const [from, setFrom] = useState(() => vnToday(6));
  const [to, setTo] = useState(() => vnToday());
  const [fromText, setFromText] = useState(() => formatVnDate(vnToday(6)));
  const [toText, setToText] = useState(() => formatVnDate(vnToday()));
  const warehouses = useQuery({ queryKey: queryKey("inventory.warehouses"), queryFn: () => unwrap(listWarehouses()) });

  const list = useInfiniteQuery({
    queryKey: queryKey("inventory.legacy_returns", from, to, refreshKey),
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      unwrap(
        listLegacyReturns({ query: { from, to, limit: PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) } }),
      ),
    getNextPageParam: (last) => last.nextCursor,
  });

  // "Xem lần nhập cũ" của lỗi trùng: đổi khoảng ngày về đúng ngày đó (chỉnh state ngay lúc vẽ, không qua effect) rồi
  // cuộn tới kiện khi danh sách đã tải.
  const [seenFocus, setSeenFocus] = useState(focus);
  if (focus !== seenFocus) {
    setSeenFocus(focus);
    if (focus !== null) {
      setFrom(focus.day);
      setTo(focus.day);
      setFromText(formatVnDate(focus.day));
      setToText(formatVnDate(focus.day));
    }
  }
  const items: LegacyReturn[] = list.data?.pages.flatMap((p) => p.items) ?? [];
  const settled = !list.isFetching;
  useEffect(() => {
    if (focus === null || !settled) return;
    document.getElementById(`kien-${focus.id}`)?.scrollIntoView({ block: "center" });
  }, [focus, settled, items.length]);

  const groups = groupByDay(items);
  const totals = returnCounts(items);
  const more = list.hasNextPage;
  const name = (id: string) => warehouses.data?.find((w) => w.id === id)?.name ?? "";

  // Gõ đủ một ngày có thật thì áp ngay; chưa đủ thì chỉ đổi chữ trong ô.
  const range = (apply: (day: string) => void, set: (t: string) => void) => (v: string) => {
    set(v);
    const d = parseVnDate(v);
    if (d !== null) apply(d);
  };

  return (
    <div className="stack">
      <h2>Đã nhập</h2>
      <div className="toolbar">
        <label className="field">
          <span>Từ ngày</span>
          <input
            className="input"
            inputMode="numeric"
            value={fromText}
            onChange={(e) => {
              range(setFrom, setFromText)(e.target.value);
            }}
          />
        </label>
        <label className="field">
          <span>Đến ngày</span>
          <input
            className="input"
            inputMode="numeric"
            value={toText}
            onChange={(e) => {
              range(setTo, setToText)(e.target.value);
            }}
          />
        </label>
      </div>

      {list.isPending && <p className="muted">Đang tải…</p>}
      {list.isError && (
        <RetryBox
          title="Chưa tải được danh sách."
          error={list.error}
          onRetry={() => {
            void list.refetch();
          }}
        />
      )}
      {list.isSuccess && items.length === 0 && !more && (
        <div className="empty">
          <h2>{`Chưa nhập kiện nào từ ${formatVnDate(from)} đến ${formatVnDate(to)}`}</h2>
          <p className="small muted">Đổi khoảng ngày, hoặc nhập kiện đầu tiên ở trên.</p>
        </div>
      )}
      {items.length > 0 && (
        <p className="small muted">
          {more
            ? `Đã tải ${fmtInt(items.length)} kiện, còn nữa. Bấm "Xem thêm" hoặc thu hẹp khoảng ngày.`
            : `${fmtInt(items.length)} kiện · ${fmtInt(totals.total)} cái${totals.damaged > 0 ? ` (${fmtInt(totals.damaged)} hỏng)` : ""} trong ${fmtInt(groups.length)} ngày`}
        </p>
      )}
      {groups.map((g) => (
        <div className="day" key={g.day}>
          <h3>{dayTitle(g.day, g.items.length)}</h3>
          {g.items.map((r) => {
            const c = returnCounts(r);
            return (
              <div className="qrow" id={`kien-${r.id}`} key={r.id}>
                <div>
                  <span className="bold">{r.reference}</span>{" "}
                  <span className="small muted">{`· ${shortTime(r.createdAt).slice(0, 5)} · ${r.createdBy.name}`}</span>
                  <br />
                  <span className="small">
                    {`${name(r.warehouseId)} · ${fmtInt(c.sellable)} bán lại được`}
                    {c.damaged > 0 && (
                      <>
                        {", "}
                        <span className="err-text">{`${fmtInt(c.damaged)} hỏng`}</span>
                      </>
                    )}
                  </span>
                </div>
                <Link className="btn btn-text" to={`/kho/phieu/${r.stockDocumentId}`}>
                  Phiếu nhập
                </Link>
              </div>
            );
          })}
        </div>
      ))}
      {more && (
        <button
          type="button"
          className="btn btn-line"
          disabled={list.isFetchingNextPage}
          onClick={() => {
            void list.fetchNextPage();
          }}
        >
          Xem thêm
        </button>
      )}
    </div>
  );
}
