// Chi tiết một đợt mã quà sinh nhật (kế hoạch M5 Task 209, mockup trạng thái D): mức giảm, số đã tặng, đã dùng, hết
// hạn chưa dùng (getCouponBatch), từng mã với trạng thái và hạn dùng (listCoupons?batchId, tải thêm). Mã hiện đầy đủ
// cho người có promotion.read (A9). API không trả SĐT gắn với mã, nên không có cột "Gắn với số" của mockup.
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { listCoupons, type CouponPage } from "../../api";
import { unwrap } from "../../app/errors";
import { useBatch } from "../ma-giam-gia/couponData";
import { discountText } from "../ma-giam-gia/couponRules";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { formatMoney } from "../products/productRules";
import { ErrorBox } from "./BirthdayParts";
import { birthdayKey } from "./birthdayData";
import { codeState, lastDay } from "./birthdayRules";

const PAGE = 50;

export function BatchDetail({ id }: { id: string }) {
  const batch = useBatch(id);
  const codes = useInfiniteQuery({
    queryKey: birthdayKey("codes", id),
    initialPageParam: "",
    getNextPageParam: (last: CouponPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listCoupons({ query: { batchId: id, limit: PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const rows = codes.data?.pages.flatMap((p) => p.items) ?? [];
  const [now] = useState(() => Date.now());
  const b = batch.data;

  return (
    <div className="stack bg-batch" data-testid="bg-batch">
      {batch.isError && <ErrorBox error={batch.error} />}
      {b !== undefined && (
        <div className="stack-sm">
          <h2>{`${b.name} · ${String(b.quantity)} mã`}</h2>
          <p className="small muted">
            {`Giảm ${discountText(b.template)} · đơn từ ${formatMoney(b.template.minSubtotal ?? 0)} · mỗi mã dùng 1 lần, chỉ cho đơn giao tới số đã gắn`}
          </p>
          <p className="small">
            {`Đã tặng ${String(b.givenCount ?? 0)} · đã dùng ${String(b.usedCount ?? 0)} · hết hạn chưa dùng ${String(b.expiredCount ?? 0)}`}
          </p>
        </div>
      )}
      {codes.isError && <ErrorBox error={codes.error} />}
      {rows.length > 0 && (
        <>
          <div className="tbl-frame">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Mã</th>
                  <th>Trạng thái</th>
                  <th>Hạn dùng</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((c) => {
                  const st = codeState(c, now);
                  return (
                    <tr key={c.id} className={st.muted === true ? "is-muted" : undefined}>
                      <td className="lead">
                        <span className="code">{c.code}</span>
                      </td>
                      <td data-label="Trạng thái">
                        <span className={st.className}>{st.label}</span>
                      </td>
                      <td data-label="Hạn dùng" className="num">
                        {lastDay(c.validTo)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <MoreBar text={`${String(rows.length)} mã`} pages={codes} />
        </>
      )}
    </div>
  );
}
