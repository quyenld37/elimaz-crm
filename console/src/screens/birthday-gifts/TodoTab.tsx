// Tab "Cần nhắn" của màn Quà sinh nhật (mockup trạng thái A): mã chưa gửi tự động (khách không đồng ý Zalo, email, hay
// gửi lỗi), sinh nhật gần nhất trước. Chép tin (đúng mẫu tin của website), mở Zalo với số đầy đủ, "Đã nhắn" một hay
// nhiều mã (markBirthdayMessaged). Bảng chỉ hiện số che giữa.
import { useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { useState } from "react";
import { markBirthdayMessaged, type BirthdayGift, type BirthdayGiftList } from "../../api";
import { unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { ErrorBox, Who } from "./BirthdayParts";
import { TODO_LIMIT, headers, reloadBirthday } from "./birthdayData";
import { birthdayInText, giftMessage, lastDay, maskPhone, zaloLink } from "./birthdayRules";

const two = (n: number) => String(n).padStart(2, "0");

function Row(o: {
  g: BirthdayGift;
  canManage: boolean;
  checked: boolean;
  busy: boolean;
  onCheck: () => void;
  onCopy: () => void;
  onMark: () => void;
}) {
  const { g } = o;
  return (
    <tr>
      {o.canManage && (
        <td className="c-chk">
          <label className="check">
            <input type="checkbox" checked={o.checked} aria-label={`Chọn ${g.fullName}`} onChange={o.onCheck} />
          </label>
        </td>
      )}
      <td className="lead who-c">
        <Who name={g.fullName} phone={maskPhone(g.phone)} />
      </td>
      <td data-label="Sinh nhật">
        <span className="num">{`${two(g.day)}/${two(g.month)}`}</span>
        {" · "}
        <span className={g.birthdayIn === 0 ? "small err-text" : "small muted"}>{birthdayInText(g.birthdayIn)}</span>
      </td>
      <td data-label="Mã quà">
        <span className="code">{g.code}</span>
      </td>
      <td data-label="Hạn dùng">
        <span className="num small">{lastDay(g.validTo)}</span>
      </td>
      <td className="c-act">
        <div className="row">
          <button type="button" className="btn btn-quiet" onClick={o.onCopy}>
            Chép tin
          </button>
          <a className="btn btn-quiet" href={zaloLink(g.phone)} target="_blank" rel="noreferrer">
            Mở Zalo
          </a>
          {o.canManage && (
            <button type="button" className="btn btn-line" disabled={o.busy} onClick={o.onMark}>
              Đã nhắn
            </button>
          )}
        </div>
      </td>
    </tr>
  );
}

export function TodoTab(o: {
  todo: UseQueryResult<BirthdayGiftList>;
  canManage: boolean;
  onToast: (t: string | null) => void;
}) {
  const qc = useQueryClient();
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set());
  const mark = useWriteAction((key: string, codes: string[]) =>
    unwrap(markBirthdayMessaged({ body: { codes }, headers: headers(key) })),
  );
  if (o.todo.isError) return <ErrorBox error={o.todo.error} />;
  const items = o.todo.data?.items;
  if (items === undefined) return null;
  if (items.length === 0)
    return (
      <div className="empty">
        <h2>Không có mã nào cần nhắn tay</h2>
        <p className="muted">Mã mới đều đã được website gửi tự động.</p>
      </div>
    );
  const shown = new Set(items.map((g) => g.code));
  const sel = [...picked].filter((c) => shown.has(c));

  const toggle = (code: string) => {
    const next = new Set(picked);
    if (next.has(code)) next.delete(code);
    else next.add(code);
    setPicked(next);
  };
  const copy = (g: BirthdayGift) => {
    navigator.clipboard.writeText(giftMessage(g)).then(
      () => {
        o.onToast(`Đã chép tin cho ${g.fullName}. Dán vào Zalo rồi bấm "Đã nhắn".`);
      },
      () => {
        o.onToast("Trình duyệt không cho chép tin. Cho phép trang chép vào bộ nhớ tạm rồi bấm lại.");
      },
    );
  };
  const markCodes = (codes: string[]) => {
    o.onToast(null);
    void mark.run(codes).then((r) => {
      if (!r.ok) return;
      const done = r.data.marked.length;
      const before = codes.length - done;
      o.onToast(
        before > 0
          ? `Đã ghi đã nhắn ${String(done)} mã; ${String(before)} mã đã được ghi trước đó.`
          : `Đã ghi đã nhắn ${String(done)} mã.`,
      );
      setPicked(new Set());
      reloadBirthday(qc);
    });
  };

  return (
    <>
      {o.canManage && sel.length > 0 && (
        <div className="selbar">
          <span className="grow">{`Đã chọn ${String(sel.length)} khách`}</span>
          <button
            type="button"
            className="btn"
            onClick={() => {
              setPicked(new Set());
            }}
          >
            Bỏ chọn
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={mark.pending}
            onClick={() => {
              markCodes(sel);
            }}
          >
            Đánh dấu đã nhắn
          </button>
        </div>
      )}
      {mark.message !== null && <div className="box box-err bg-gap">{mark.message}</div>}
      <div className="tbl-frame">
        <table className="tbl bd">
          <thead>
            <tr>
              {o.canManage && (
                <th className="c-chk">
                  <label className="check">
                    <input
                      type="checkbox"
                      aria-label="Chọn cả trang"
                      checked={sel.length === items.length}
                      onChange={() => {
                        setPicked(sel.length === items.length ? new Set() : new Set(shown));
                      }}
                    />
                  </label>
                </th>
              )}
              <th>Khách</th>
              <th>Sinh nhật</th>
              <th>Mã quà</th>
              <th>Hạn dùng</th>
              <th className="c-act">Thao tác</th>
            </tr>
          </thead>
          <tbody>
            {items.map((g) => (
              <Row
                key={g.code}
                g={g}
                canManage={o.canManage}
                checked={picked.has(g.code)}
                busy={mark.pending}
                onCheck={() => {
                  toggle(g.code);
                }}
                onCopy={() => {
                  copy(g);
                }}
                onMark={() => {
                  markCodes([g.code]);
                }}
              />
            ))}
          </tbody>
        </table>
      </div>
      {items.length >= TODO_LIMIT && (
        <p className="hint">{`Đang hiện ${String(TODO_LIMIT)} mã đầu; đánh dấu xong sẽ hiện tiếp các mã còn lại.`}</p>
      )}
      <p className="hint">"Mở Zalo" mở cuộc trò chuyện với số đầy đủ của khách; danh sách chỉ hiện số che giữa.</p>
    </>
  );
}
