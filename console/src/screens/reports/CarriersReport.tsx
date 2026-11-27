// Hãng vận chuyển /bao-cao/van-chuyen (Task 240, M6-17; mockup docs/m6/mockup/van-chuyen.html; BC02). Vận đơn tạo trong
// kỳ theo hãng và tài khoản hãng (kể cả Tự giao), trạng thái là trạng thái hiện tại của vận đơn. Một dòng mỗi tài khoản
// như máy chủ trả (không có dòng tổng phụ theo hãng: tỷ lệ và trung bình không cộng được, màn không tự tính); dòng Tổng
// do máy chủ trả. Bấm tên hãng mở Báo cáo đơn nhóm theo Hãng, lọc đúng hãng, cùng kỳ.
import { Link } from "react-router";
import { getSalesCarriersReport, type SalesCarrierRow, type SalesCarriersReport } from "../../api";
import { unwrap } from "../../app/errors";
import { MoneyCell } from "./CellNumber";
import { ReportBanners } from "./ReportBanners";
import { ReportPeriodPicker, SalesReportPage } from "./SalesReportPage";
import { clockTime, count, periodText, rate } from "./format";
import { writePeriod, type Period } from "./period";
import { carrierLabel, isCarriersReport } from "./salesTables";
import { useReport } from "./useReport";
import { useReportPeriod } from "./useReportPeriod";
import { useExportRun } from "./useRunNotice";

export const TEXT_EMPTY = "Chưa có vận đơn nào trong kỳ";
export const ORDERS_PATH = "/bao-cao/don";
const SUB = "Vận đơn theo hãng và tài khoản hãng, kể cả Tự giao. Tỷ lệ thành công tính trên vận đơn đã kết thúc.";

/** Đường tới Báo cáo đơn nhóm theo Hãng, lọc hãng này, cùng kỳ đang xem (cơ sở ngày lên đơn). */
export function ordersOfCarrier(period: Period, carrierKey: string): string {
  const sp = new URLSearchParams();
  writePeriod(sp, period);
  sp.set("tinh", "placed");
  sp.set("nhom", "carrier");
  sp.set("hang", carrierKey);
  return `${ORDERS_PATH}?${sp.toString()}`;
}

function CarrierTable({ d, period }: { d: SalesCarriersReport; period: Period }) {
  const cells = (r: SalesCarrierRow) => (
    <>
      <td>{count(r.created)}</td>
      <td>{count(r.cancelled)}</td>
      <td>{count(r.inProgress)}</td>
      <td>{count(r.returning)}</td>
      <td>{count(r.deliveryFailed)}</td>
      <td>{count(r.delivered)}</td>
      <td>{count(r.returned)}</td>
      <td>{rate(r.successRateBp)}</td>
      <MoneyCell v={r.feeTotal} />
      <MoneyCell v={r.feeAvg} />
      <MoneyCell v={r.codTotal} />
    </>
  );
  return (
    <div className="rt-frame scroll-x">
      <table className="rt" aria-label="Hãng vận chuyển">
        <thead>
          <tr>
            <th>Hãng / tài khoản</th>
            <th>Vận đơn tạo</th>
            <th>Đã hủy</th>
            <th>Đang đi</th>
            <th>Đang hoàn</th>
            <th>Giao thất bại chờ xử lý</th>
            <th>Thành công</th>
            <th>Hoàn</th>
            <th>Tỷ lệ thành công</th>
            <th>Tổng phí</th>
            <th>Phí TB</th>
            <th>Tổng COD</th>
          </tr>
        </thead>
        <tbody>
          {d.rows.map((r) => (
            <tr key={`${r.carrierKey}|${r.carrierAccountId ?? ""}`}>
              <td>
                <Link className="cell-a bold" to={ordersOfCarrier(period, r.carrierKey)}>
                  {carrierLabel(r.carrierKey)}
                </Link>
                {r.accountName !== undefined && r.accountName !== "" && (
                  <>
                    {" "}
                    <span className="nm muted" title={r.accountName}>
                      {r.accountName}
                    </span>
                  </>
                )}
              </td>
              {cells(r)}
            </tr>
          ))}
          <tr className="is-total">
            <td>Tổng</td>
            {cells(d.totals)}
          </tr>
        </tbody>
      </table>
    </div>
  );
}

export function CarriersReport() {
  const rp = useReportPeriod();
  const { period } = rp;
  const params = { from: period.from, to: period.to };
  const rep = useReport<SalesCarriersReport>({
    report: "carriers",
    params,
    load: () => unwrap(getSalesCarriersReport({ query: params })),
    parse: (v) => (isCarriersReport(v) ? v : null),
    enabled: rp.bad === null,
  });
  const exp = useExportRun({
    report: "carriers",
    params,
    title: `Hãng vận chuyển ${periodText(period.from, period.to)}`,
  });
  const data = rep.phase === "ready" ? rep.data : null;

  return (
    <SalesReportPage title="Hãng vận chuyển" sub={SUB} rep={rep} exp={exp} periodBad={rp.bad !== null}>
      <ReportPeriodPicker rp={rp} disabled={rep.phase === "loading" && rp.bad === null} />
      <ReportBanners rep={rep} failedHint="Thu hẹp kỳ, rồi bấm Tải lại." />
      {data !== null && rep.updatedAt !== null && (
        <>
          <div className="meta">
            <span>
              Theo <b>ngày tạo vận đơn</b> · {periodText(period.from, period.to)}, giờ Việt Nam · trạng thái là trạng
              thái hiện tại của vận đơn
            </span>
            <span>
              Cập nhật lúc <b>{clockTime(rep.updatedAt)}</b>
            </span>
          </div>
          {data.rows.length === 0 ? (
            <div className="empty">
              <h2>{TEXT_EMPTY}</h2>
              <p className="muted">Vận đơn tính theo ngày tạo. Chọn kỳ khác.</p>
            </div>
          ) : (
            <CarrierTable d={data} period={period} />
          )}
          <p className="xs muted">
            &quot;Vận đơn tạo&quot; = vận đơn tạo trong kỳ, trừ vận đơn đã hủy (= đang đi + đang hoàn + giao thất bại +
            thành công + hoàn). &quot;Giao thất bại chờ xử lý&quot; = hãng báo giao không được, shop chưa chọn giao lại
            hay hoàn. Tỷ lệ thành công = thành công ÷ (thành công + hoàn); vận đơn đang đi không tính. Phí TB = tổng phí
            ÷ vận đơn chưa hủy. Bấm tên hãng mở Báo cáo đơn nhóm theo Hãng, lọc đúng hãng đó.
          </p>
        </>
      )}
    </SalesReportPage>
  );
}
