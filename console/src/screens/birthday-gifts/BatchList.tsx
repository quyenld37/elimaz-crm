// Tab "Đợt mã" của màn Quà sinh nhật (kế hoạch M5 Task 209, mockup trạng thái D; WB05): đợt mã quà sinh nhật mới nhất
// trước (listCouponBatches?purpose=birthday; "Mã chuyển từ website cũ" là purpose=imported) với số đã tặng, đã dùng,
// hết hạn của từng đợt (Task 487), tải thêm theo con trỏ;
// "Mở" hiện chi tiết đợt bên dưới. Chỉ xem: phát mã do lượt phát của CRM làm.
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { listCouponBatches, type CouponBatch, type CouponBatchPage } from "../../api";
import { unwrap } from "../../app/errors";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { Pick } from "../web-campaigns/CampaignList";
import { BatchDetail } from "./BatchDetail";
import { ErrorBox } from "./BirthdayParts";
import { birthdayKey } from "./birthdayData";
import { fullDate } from "./birthdayRules";

const PAGE = 50;
type Purpose = "birthday" | "imported";
const PURPOSES: readonly [Purpose, string][] = [
  ["birthday", "Quà sinh nhật"],
  ["imported", "Mã chuyển từ website cũ"],
];

function Row({ b, onOpen }: { b: CouponBatch; onOpen: () => void }) {
  return (
    <tr>
      <td className="lead">
        <b>{b.name}</b>
        <br />
        <span className="small muted">{`tạo ${fullDate(b.createdAt)}`}</span>
      </td>
      <td data-label="Mã" className="r num">
        {b.quantity}
      </td>
      <td data-label="Đã tặng" className="r num">
        {b.givenCount ?? 0}
      </td>
      <td data-label="Đã dùng" className="r num">
        {b.usedCount ?? 0}
      </td>
      <td data-label="Hết hạn" className="r num">
        {b.expiredCount ?? 0}
      </td>
      <td className="c-act">
        {b.quantity > 0 ? (
          <button type="button" className="btn btn-quiet" onClick={onOpen}>
            Mở
          </button>
        ) : (
          <span className="small muted">Không có khách đến hạn</span>
        )}
      </td>
    </tr>
  );
}

export function BatchList(o: { open: string; onOpen: (id: string) => void }) {
  const [purpose, setPurpose] = useState<Purpose>("birthday");
  const list = useInfiniteQuery({
    queryKey: birthdayKey("batches", purpose),
    initialPageParam: "",
    getNextPageParam: (last: CouponBatchPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(
        listCouponBatches({ query: { purpose, limit: PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) } }),
      ),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];

  function body() {
    if (list.isError) return <ErrorBox error={list.error} />;
    if (!list.isSuccess) return null;
    if (rows.length === 0)
      return (
        <div className="empty">
          <h2>Chưa có đợt mã nào</h2>
          <p className="muted">Lượt phát quà tạo mỗi ngày một đợt khi có khách đến hạn.</p>
        </div>
      );
    return (
      <>
        <div className="tbl-frame">
          <table className="tbl">
            <thead>
              <tr>
                <th>Đợt</th>
                <th className="r">Mã</th>
                <th className="r">Đã tặng</th>
                <th className="r">Đã dùng</th>
                <th className="r">Hết hạn</th>
                <th className="c-act">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((b) => (
                <Row
                  key={b.id}
                  b={b}
                  onOpen={() => {
                    o.onOpen(b.id);
                  }}
                />
              ))}
            </tbody>
          </table>
        </div>
        <MoreBar text={`${String(rows.length)} đợt`} pages={list} />
      </>
    );
  }

  return (
    <>
      <div className="toolbar">
        <Pick
          label="Loại đợt"
          value={purpose}
          options={PURPOSES.map(([v, t]): [string, string] => [v, t])}
          onPick={(v) => {
            setPurpose(v === "imported" ? "imported" : "birthday");
          }}
        />
      </div>
      {body()}
      {o.open !== "" && <BatchDetail id={o.open} />}
    </>
  );
}
