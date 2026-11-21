// Thanh "Chốt bảng kê" (kế hoạch M3 Task 137; mockup docs/mockups/m3/doi-soat-cod.html trạng thái C, D; HV31; TI05,
// TI07). Chốt cả bảng kê một lần, gửi version của bản đang xem (confirmCodStatement). 409 statement_changed: báo bên
// gọi (màn giữ nguyên số đang hiện tới khi người dùng bấm Tải lại), không tự gửi lại. Câu tóm tắt dùng tổng của mọi
// dòng do máy chủ tính (CodMatchTotal), không cộng ở màn.
import { confirmCodStatement, type CodMatchTotal, type CodStatement, type CodStatementConfirmed } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { headers } from "./codData";
import { confirmSummary, formatMoney, timeDay } from "./codRules";

interface ConfirmBody {
  id: string;
  version: number;
}

export interface ConfirmBarProps {
  statement: CodStatement;
  totals: readonly CodMatchTotal[];
  /** Màn đang giữ bản cũ sau 409 (khóa nút tới khi tải lại). */
  frozen: boolean;
  /** Gọi ngay trước khi gửi (bên gọi ghi lại bản đang xem). */
  onSubmit: () => void;
  onConflict: () => void;
  onConfirmed: (c: CodStatementConfirmed) => void;
}

export function ConfirmBar({ statement, totals, frozen, onSubmit, onConflict, onConfirmed }: ConfirmBarProps) {
  const write = useWriteAction((key, b: ConfirmBody) =>
    unwrap(confirmCodStatement({ path: { id: b.id }, body: { version: b.version }, headers: headers(key) })),
  );
  const s = confirmSummary(totals);

  const submit = async () => {
    onSubmit();
    const r = await write.run({ id: statement.id, version: statement.version });
    if (r.ok) {
      onConfirmed(r.data);
      return;
    }
    if (r.error instanceof ApiError && r.error.code === "statement_changed") {
      write.reset();
      onConflict();
    }
  };

  return (
    <div className="box box-info">
      <div className="row-between">
        <div className="grow">
          <b>Chốt bảng kê</b>
          <p className="small">
            {`Ghi tiền về shop cho ${String(s.matched)} dòng khớp`}
            {s.mismatched > 0 && ` và ${String(s.mismatched)} dòng lệch theo số hãng thu`}
            {s.short > 0 && (
              <>
                {"; phần thiếu "}
                <span className="money">{formatMoney(s.short)}</span>
                {' vẫn mở và vào "Lệch đối soát"'}
              </>
            )}
            {". Tách phí hãng "}
            <span className="money">{formatMoney(s.fee)}</span>
            {". Dòng ngoài hệ thống, trùng không ghi."}
          </p>
        </div>
        <button
          type="button"
          className="btn btn-dark"
          disabled={frozen || write.pending}
          onClick={() => {
            void submit();
          }}
        >
          Chốt bảng kê
        </button>
      </div>
      {write.message !== null && (
        <p className="small err-text" role="alert">
          {write.message}
        </p>
      )}
    </div>
  );
}

/** Hộp "Đã chốt" sau khi chốt: giờ, người chốt, cảnh báo không chặn của máy chủ. */
export function ConfirmedBox({ result }: { result: CodStatementConfirmed }) {
  const st = result.statement;
  return (
    <div className="box box-ok small" role="status">
      <b className="ok-text">Đã chốt</b>
      {` ${st.confirmedAt === undefined ? "" : timeDay(st.confirmedAt)}${
        st.confirmedBy === undefined ? "" : ` bởi ${st.confirmedBy.name}`
      }.`}
      {result.warnings.map((w) => (
        <span key={w}>{` Cảnh báo: ${w}; kiểm lại với hãng.`}</span>
      ))}
    </div>
  );
}
