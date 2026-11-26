// Đơn nháp đang mở của khách (Task 176; mockup khung-ben-phai.html trạng thái B "don-nhap-live"; HV25): đơn nháp của cả
// nhóm khách (thẻ hội thoại, openDrafts), hạn giữ hàng; "Hoàn tất" mở khung lên đơn với đơn nháp đó (cần order.update và
// order.confirm như máy chủ kiểm).
import type { ChatOrderRef } from "../../../api";
import { shortTime } from "../../../app/time";
import { formatMoney } from "../../products/productRules";

export interface OpenDraftsProps {
  drafts: readonly ChatOrderRef[];
  canComplete: boolean;
  onComplete: (draft: ChatOrderRef) => void;
}

export function OpenDrafts({ drafts, canComplete, onComplete }: OpenDraftsProps) {
  if (drafts.length === 0) return null;
  return (
    <div className="sec">
      <h3>Đơn nháp đang mở</h3>
      {drafts.map((d) => (
        <div key={d.id} className="box box-info small">
          <b>{`${d.code} · tạo ${shortTime(d.createdAt)}`}</b>
          <br />
          <span className="money">{formatMoney(d.grandTotal)}</span>
          {d.draftExpiresAt !== undefined && (
            <>
              <br />
              {`Giữ hàng tới ${shortTime(d.draftExpiresAt)}`}
            </>
          )}
          {canComplete && (
            <div className="row" style={{ marginTop: 8 }}>
              <button
                type="button"
                className="btn btn-line"
                aria-label={`Hoàn tất ${d.code}`}
                onClick={() => {
                  onComplete(d);
                }}
              >
                Hoàn tất
              </button>
            </div>
          )}
        </div>
      ))}
      <p className="xs muted">Hoàn tất mở đơn nháp ở khung lên đơn bên dưới để điền địa chỉ rồi xác nhận.</p>
    </div>
  );
}
