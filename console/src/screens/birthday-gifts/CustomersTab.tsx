// Tab "Tất cả khách" của màn Quà sinh nhật (mockup trạng thái B): khách đã lưu sinh nhật với tám trạng thái quà, lọc
// trạng thái (kèm số đếm của máy chủ), tháng sinh (mặc định tháng hiện tại), tìm tên hay SĐT (máy chủ chuẩn hóa số
// gõ 0 đầu, Task 209), tải thêm theo con trỏ, xuất CSV (exportBirthdayCustomers: tệp có SĐT, máy chủ ghi nhật ký).
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import {
  exportBirthdayCustomers,
  listBirthdayCustomers,
  type BirthdayCustomer,
  type BirthdayCustomerPage,
  type BirthdayCustomerStateFilter,
  type BirthdayExportRequest,
} from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { Pick } from "../web-campaigns/CampaignList";
import { ErrorBox, Who } from "./BirthdayParts";
import { birthdayKey, headers, saveTextFile } from "./birthdayData";
import { STATES, STATE_TEXT, dayMonth, fullDate, maskPhone, sameVnDay, stateChip } from "./birthdayRules";

const PAGE = 50;
const TEXT_ALREADY_EXPORTED =
  "Tệp đã xuất ở lần bấm trước nhưng chưa tải về được (tệp có SĐT nên máy chủ không giữ lại). Bấm Xuất CSV lần nữa.";
const two = (n: number) => String(n).padStart(2, "0");
const asState = (v: string): BirthdayCustomerStateFilter => STATES.find((s) => s === v) ?? "all";
/** Tháng hiện tại theo giờ Việt Nam (1–12). */
const vnMonth = () => Number(fullDate(new Date().toISOString()).slice(3, 5));

function Row({ c, enabled, now }: { c: BirthdayCustomer; enabled: boolean; now: number }) {
  const chip = stateChip(c.state, enabled);
  const via = [c.zalo ? "Zalo" : "", c.mail ? "Email" : ""].filter((v) => v !== "").join(" · ");
  const stopped = c.state === "stopped";
  return (
    <tr className={stopped ? "is-muted" : undefined}>
      <td className="lead">
        <Who name={c.fullName} phone={maskPhone(c.phone)} />
      </td>
      <td data-label="Sinh nhật" className="num">{`${two(c.day)}/${two(c.month)}`}</td>
      <td data-label="Ngày gửi mã" className="num">
        {stopped ? "—" : sameVnDay(c.sendOn, now) ? "hôm nay" : dayMonth(c.sendOn)}
      </td>
      <td data-label="Nhận qua">{via === "" ? "—" : via}</td>
      <td data-label="Mã quà">
        {c.code === undefined ? <span className="muted">—</span> : <span className="code">{c.code}</span>}
      </td>
      <td data-label="Trạng thái">
        <span className={chip.className}>{chip.label}</span>
      </td>
    </tr>
  );
}

export function CustomersTab(o: { enabled: boolean; canManage: boolean; onToast: (t: string | null) => void }) {
  const [text, setText] = useState("");
  const [state, setState] = useState<BirthdayCustomerStateFilter>("all");
  const [month, setMonth] = useState(vnMonth);
  const q = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const filter: BirthdayExportRequest = {
    ...(state === "all" ? {} : { state }),
    ...(month === 0 ? {} : { month }),
    ...(q === "" ? {} : { q }),
  };
  const list = useInfiniteQuery({
    queryKey: birthdayKey("customers", filter),
    initialPageParam: "",
    getNextPageParam: (last: BirthdayCustomerPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(
        listBirthdayCustomers({
          query: { ...filter, limit: PAGE, ...(pageParam === "" ? {} : { cursor: pageParam }) },
        }),
      ),
  });
  const exporter = useWriteAction((key: string, body: BirthdayExportRequest) =>
    unwrap(exportBirthdayCustomers({ body, headers: headers(key) })),
  );
  const first = list.data?.pages[0];
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const [now] = useState(() => Date.now());

  const onExport = () => {
    o.onToast(null);
    void exporter.run(filter).then((r) => {
      if (!r.ok) return;
      saveTextFile(r.data.filename, r.data.csv, "text/csv;charset=utf-8");
      o.onToast(`Đã xuất ${String(r.data.rows)} dòng vào tệp ${r.data.filename}.`);
    });
  };
  const exportError =
    exporter.error instanceof ApiError && exporter.error.code === "already_processed"
      ? TEXT_ALREADY_EXPORTED
      : exporter.message;

  function body() {
    if (list.isError) return <ErrorBox error={list.error} />;
    if (first === undefined) return null;
    if (rows.length === 0)
      return (
        <div className="empty">
          <h2>Không có khách nào khớp</h2>
          <p className="muted">Đổi bộ lọc trạng thái, tháng sinh hoặc từ tìm.</p>
        </div>
      );
    return (
      <>
        <div className="tbl-frame">
          <table className="tbl">
            <thead>
              <tr>
                <th>Khách</th>
                <th>Sinh nhật</th>
                <th>Ngày gửi mã</th>
                <th>Nhận qua</th>
                <th>Mã quà</th>
                <th>Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <Row key={c.customerId} c={c} enabled={o.enabled} now={now} />
              ))}
            </tbody>
          </table>
        </div>
        <MoreBar text={`Đang hiện ${String(rows.length)} / ${String(first.total)} khách`} pages={list} />
      </>
    );
  }

  return (
    <>
      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          aria-label="Tìm khách"
          placeholder="Tìm tên hoặc số điện thoại"
          value={text}
          maxLength={100}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
        <Pick
          label="Trạng thái"
          value={state}
          options={[
            ["all", "Mọi trạng thái"],
            ...STATES.map((s): [string, string] => [
              s,
              first === undefined ? STATE_TEXT[s] : `${STATE_TEXT[s]} (${String(first.stateCounts[s])})`,
            ]),
          ]}
          onPick={(v) => {
            setState(asState(v));
          }}
        />
        <Pick
          label="Tháng sinh"
          value={String(month)}
          options={[
            ["0", "Mọi tháng"],
            ...Array.from({ length: 12 }, (_, i): [string, string] => [String(i + 1), `Tháng ${String(i + 1)}`]),
          ]}
          onPick={(v) => {
            setMonth(Number(v));
          }}
        />
        {o.canManage && (
          <button type="button" className="btn btn-line" disabled={exporter.pending} onClick={onExport}>
            {exporter.pending ? "Đang xuất…" : "Xuất CSV"}
          </button>
        )}
      </div>
      {exportError !== null && <div className="box box-err bg-gap">{exportError}</div>}
      {body()}
      {o.canManage && (
        <p className="hint">
          Tệp CSV có số điện thoại, email đầy đủ (tên, SĐT, email, ngày sinh, trạng thái, ngày gửi mã, mã quà, đồng ý
          Zalo, Email), theo bộ lọc đang chọn. Mỗi lần xuất ghi vào nhật ký thao tác kèm số dòng.
        </p>
      )}
    </>
  );
}
