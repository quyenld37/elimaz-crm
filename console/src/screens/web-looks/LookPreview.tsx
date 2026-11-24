// Xem trước bộ đang chọn trên trang chủ (mockup trạng thái C): ảnh người mẫu và chấm, "Hôm nay mặc gì?", câu mở đầu
// (trống → "Mặc gì cho <tên>?"), tiêu đề đã nhập, dòng người mẫu, câu chuyện, tên các món; bộ đang ẩn ghi lý do. Chỉ vẽ
// phần CRM có: giá, size, còn hàng do website tự lấy (Task 464), không vẽ ở đây.
import { useState } from "react";
import type { Look, LookStatus } from "../../api";
import { hiddenText, modelLine } from "./lookRules";

interface Props {
  look: Look | undefined;
  n: number;
  status: LookStatus | undefined;
  names: ReadonlyMap<string, string>;
  photo: string | undefined;
  error: string | null;
}

function Card({ look, names, photo }: { look: Look; names: Props["names"]; photo: string | undefined }) {
  const name = look.name.trim() === "" ? "Bộ mới" : look.name;
  const headline = look.headline?.trim() ?? "";
  const title = look.title?.trim() ?? "";
  const model = modelLine(look.modelHeightCm, look.modelSize);
  const story = look.story?.trim() ?? "";
  return (
    <div className="lk-card">
      <div className="lk-photo">
        {photo === undefined ? (
          <span className="small muted lk-empty">Chưa có ảnh đã xử lý</span>
        ) : (
          <img src={photo} alt="" />
        )}
        {look.items.map((it, k) => (
          <span key={it.productId} className="lk-dot" style={{ left: `${String(it.x)}%`, top: `${String(it.y)}%` }}>
            {k + 1}
          </span>
        ))}
      </div>
      <div className="stack-sm">
        <p className="xs label">HÔM NAY MẶC GÌ?</p>
        <h3 className="wrap-any">{headline === "" ? `Mặc gì cho ${name}?` : headline}</h3>
        {title !== "" && <p className="small wrap-any">{title}</p>}
        {model !== "" && <p className="small">{model}</p>}
        {story !== "" && <p className="small wrap-any">{story}</p>}
        <ol className="small lk-items">
          {look.items.map((it) => (
            <li key={it.productId}>{names.get(it.productId) ?? "Sản phẩm"}</li>
          ))}
        </ol>
      </div>
    </div>
  );
}

export function LookPreview({ look, n, status, names, photo, error }: Props) {
  const [phone, setPhone] = useState(false);
  const reason = status?.shown === false ? status.reason : undefined;
  return (
    <section className="card stack-sm" aria-label="Xem trước">
      <div className="row-between">
        <h3>Xem trước{look === undefined ? "" : ` · Bộ ${String(n)}`}</h3>
        <div className="row" role="group" aria-label="Thiết bị">
          <button
            type="button"
            className="toggle"
            aria-pressed={!phone}
            onClick={() => {
              setPhone(false);
            }}
          >
            Máy tính
          </button>
          <button
            type="button"
            className="toggle"
            aria-pressed={phone}
            onClick={() => {
              setPhone(true);
            }}
          >
            Điện thoại
          </button>
        </div>
      </div>
      {error !== null && <div className="box box-err">{error}</div>}
      {reason !== undefined && (
        <div className="box box-soft">
          <b>Bộ phối này đang ẩn:</b>{" "}
          <span>{hiddenText(reason, status?.productId === undefined ? undefined : names.get(status.productId))}.</span>{" "}
          {reason === "disabled"
            ? "Liên kết riêng vẫn mở được."
            : "Khách mở liên kết riêng sẽ thấy báo bộ phối tạm hết."}
        </div>
      )}
      {look === undefined ? (
        <p className="muted small">Chọn một bộ để xem trước.</p>
      ) : (
        <div className={`pv-frame${phone ? " is-phone" : ""}`}>
          <Card look={look} names={names} photo={photo} />
        </div>
      )}
    </section>
  );
}
