// Cần xem /ops/review (kế hoạch M3 Task 135; mockup docs/mockups/m3/van-hanh.html trạng thái E; VC18). Mỗi việc một câu
// hướng dẫn (do máy chủ viết theo loại) và nút xử lý theo loại (QĐ-135-e): "Không rõ đã tạo" → Đã tạo / Chưa tạo
// (resolveShipmentCreate); loại khác → Đã xử lý kèm cách xử lý (resolveShipmentAnomaly). Lọc theo loại ở trình duyệt.
import { useState } from "react";
import { Link } from "react-router";
import { listShipmentsForReview, type ShipmentReviewItem } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { ResolveAnomalyDialog, ResolveCreateDialog } from "./ReviewDialogs";
import { reviewKey, useOpsPaged, usePerms } from "./opsData";
import { isCreateDoubt, kindCounts, openOrderOnly, reviewKindLabel } from "./opsRules";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { ListState, OpsFrame, Seg } from "./OpsTabs";
import "./ops.css";

interface Pick {
  item: ShipmentReviewItem;
  how: "created" | "not_created" | "anomaly";
}

export function ReviewPage() {
  const perms = usePerms();
  const [kind, setKind] = useState("");
  const [pick, setPick] = useState<Pick | null>(null);
  const paged = useOpsPaged(reviewKey(), (query) => unwrap(listShipmentsForReview({ query })));
  const { list } = paged;
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const failed = list.isError && list.data === undefined ? errorMessage(list.error) : null;
  const kinds = kindCounts(rows);
  const shown = kind === "" ? rows : rows.filter((r) => r.kind === kind);
  const total = list.data?.pages[0]?.counts.review;
  const canFix = perms.can("fulfillment.correct");
  const canShip = perms.can("fulfillment.ship");
  return (
    <OpsFrame title="Cần xem" sub="Việc hệ thống không tự quyết được: mỗi dòng một câu hướng dẫn và nút xử lý.">
      <div className="ops-filters">
        <Seg
          label="Loại việc"
          value={kind}
          options={[
            { key: "", label: `Mọi loại ${String(total ?? rows.length)}` },
            ...kinds.map((k) => ({ key: k.kind, label: `${reviewKindLabel(k.kind)} ${String(k.n)}` })),
          ]}
          onChange={setKind}
        />
      </div>
      <ListState
        pending={list.isPending}
        error={failed}
        empty={!list.isPending && failed === null && shown.length === 0}
        emptyTitle="Không có việc cần xem"
        emptyText="Vận đơn nào cần người quyết sẽ vào đây."
      />
      {shown.length > 0 && (
        <div className="tbl-frame">
          <table className="tbl ops-t">
            <thead>
              <tr>
                <th>Đơn · loại</th>
                <th>Cần làm</th>
                <th className="c-act" />
              </tr>
            </thead>
            <tbody>
              {shown.map((it) => (
                <tr key={it.anomalyId}>
                  <td className="lead">
                    <span className="who-c">
                      {it.orderId === undefined ? (
                        <span className="num bold">{it.orderCode}</span>
                      ) : (
                        <Link className="num bold" to={`/don/${it.orderId}`}>
                          {it.orderCode}
                        </Link>
                      )}
                      <span className="xs muted">{reviewKindLabel(it.kind)}</span>
                    </span>
                  </td>
                  <td data-label="Cần làm" className="small wrap-any">
                    {it.label}
                  </td>
                  <td className="c-act">
                    {isCreateDoubt(it.kind) ? (
                      <>
                        <button
                          type="button"
                          className="btn btn-dark"
                          disabled={!canShip}
                          title={canShip ? undefined : "Cần quyền gửi hãng ở kho của đơn"}
                          onClick={() => {
                            setPick({ item: it, how: "created" });
                          }}
                        >
                          Đã tạo
                        </button>
                        <button
                          type="button"
                          className="btn btn-line"
                          disabled={!canShip}
                          title={canShip ? undefined : "Cần quyền gửi hãng ở kho của đơn"}
                          onClick={() => {
                            setPick({ item: it, how: "not_created" });
                          }}
                        >
                          Chưa tạo
                        </button>
                      </>
                    ) : (
                      !openOrderOnly(it.kind) && (
                        <button
                          type="button"
                          className="btn btn-dark"
                          disabled={!canFix}
                          title={canFix ? undefined : "Cần quyền sửa vận đơn"}
                          onClick={() => {
                            setPick({ item: it, how: "anomaly" });
                          }}
                        >
                          Đã xử lý
                        </button>
                      )
                    )}
                    {it.orderId !== undefined && (
                      <Link className="btn btn-line" to={`/don/${it.orderId}`}>
                        {openOrderOnly(it.kind) ? "Ghi giao một phần" : "Mở đơn"}
                      </Link>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage && <MoreBar text={`${String(rows.length)} việc đã tải`} pages={list} />}
      <p className="hint">"Đã xử lý" đòi ghi cách xử lý. Lọc theo loại chỉ áp cho các việc đã tải.</p>
      {pick !== null && pick.how === "anomaly" && (
        <ResolveAnomalyDialog
          item={pick.item}
          onClose={() => {
            setPick(null);
          }}
        />
      )}
      {pick !== null && pick.how !== "anomaly" && (
        <ResolveCreateDialog
          item={pick.item}
          result={pick.how}
          onClose={() => {
            setPick(null);
          }}
        />
      )}
    </OpsFrame>
  );
}
