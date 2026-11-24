// Màn Quà sinh nhật /khuyen-mai/qua-sinh-nhat (kế hoạch M5 Task 209 / M5-26, bốn tab theo Task 450; API Task 468;
// mockup docs/mockups/m5/birthday-gifts.html). Đầu trang: cài đặt đang dùng và lượt chạy cuối, bốn số tổng như mockup
// (Cần nhắn, Sắp gửi · 7 ngày tới, Mã đã dùng · năm, Khách đã lưu; số của máy chủ, Task 487), "Tạo mã ngay". Tab giữ trên địa chỉ (?tab=), đợt đang mở (?dot=). Chỉ vẽ hành vi API có; quyền ghi promotion.birthday.manage.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useSearchParams } from "react-router";
import { runBirthdayGifts, type BirthdayRun, type BirthdaySettings } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { shortTime } from "../../app/time";
import { formatMoney } from "../products/productRules";
import { BatchList } from "./BatchList";
import { ErrorBox, Toast } from "./BirthdayParts";
import { CustomersTab } from "./CustomersTab";
import {
  MANAGE,
  TODO_LIMIT,
  headers,
  reloadBirthday,
  useGiftSettings,
  useTodo,
  useTotals,
  usePerms,
} from "./birthdayData";
import { STATES } from "./birthdayRules";
import { SettingsTab } from "./SettingsTab";
import { TodoTab } from "./TodoTab";
import "../don/don.css";
import "./birthday-gifts.css";

const TABS = [
  ["can", "Cần nhắn"],
  ["khach", "Tất cả khách"],
  ["cai-dat", "Cài đặt quà"],
  ["dot-ma", "Đợt mã"],
] as const;
type Tab = (typeof TABS)[number][0];
const asTab = (v: string | null): Tab => TABS.find(([t]) => t === v)?.[0] ?? "can";

const TEXT_READ_ONLY =
  'Bạn chỉ xem được: cần quyền "Quản lý quà sinh nhật" để tạo mã, đánh dấu đã nhắn, xuất CSV và đổi cài đặt.';
const NUM = new Intl.NumberFormat("vi-VN");

const triggerText = (r: BirthdayRun) => (r.trigger === "manual" ? "bấm tay" : "theo lịch");

function runText(r: BirthdayRun | undefined): string {
  if (r === undefined) return "Chưa có lượt tạo mã nào.";
  if (r.finishedAt === undefined) return `Lượt ${triggerText(r)} bắt đầu ${shortTime(r.startedAt)} đang chạy.`;
  return `Lần chạy gần nhất ${shortTime(r.startedAt)} (${triggerText(r)}): ${String(r.given)} mã mới, ${String(r.failed)} lỗi.`;
}

function RunBox({ s, onSettings }: { s: BirthdaySettings; onSettings: () => void }) {
  return (
    <div className="box box-info stack-sm bg-run" data-testid="bg-run">
      <p>
        <span className={s.enabled ? "chip chip-ok" : "chip chip-dark"}>{s.enabled ? "Đang bật" : "Đang tắt"}</span>{" "}
        {`Giảm ${formatMoney(s.amount)} · gửi trước ${String(s.daysBefore)} ngày · dùng trong ${String(s.validDays)} ngày · `}
        <button type="button" className="linkbtn" onClick={onSettings}>
          Đổi cài đặt
        </button>
      </p>
      {!s.enabled && (
        <p>
          <b>Quà đang tắt:</b> CRM không tạo mã mới, kể cả cho khách đã đến hạn; mã đã tạo vẫn dùng được tới hạn.
        </p>
      )}
      <p className="small">{`${runText(s.lastRun)} Lịch tự chạy mỗi giờ trong 9:00–21:00.`}</p>
    </div>
  );
}

function Kpi(o: { id: string; label: string; value: string; note: string; bad?: boolean }) {
  return (
    <div className={o.bad === true ? "kpi is-bad" : "kpi"} data-testid={o.id}>
      <p className="small muted">{o.label}</p>
      <p className="v">{o.value}</p>
      <p className="xs muted">{o.note}</p>
    </div>
  );
}

function Kpis({ enabled }: { enabled: boolean }) {
  const todo = useTodo();
  const totals = useTotals();
  const c = totals.data?.stateCounts;
  const year = totals.data?.yearGifts;
  const n = (v: number | undefined) => (v === undefined ? (totals.isError ? "—" : "…") : NUM.format(v));
  const todoCount = todo.data?.items.length;
  const saved = c === undefined ? undefined : STATES.reduce((sum, s) => sum + c[s], 0);
  return (
    <div className="kpis bg-kpis" data-testid="bg-kpis">
      <Kpi
        id="kpi-todo"
        label="Cần nhắn"
        value={
          todoCount === undefined
            ? todo.isError
              ? "—"
              : "…"
            : `${NUM.format(todoCount)}${todoCount >= TODO_LIMIT ? "+" : ""}`
        }
        note="mã chưa gửi tự động được"
        bad={todoCount !== undefined && todoCount > 0}
      />
      <Kpi
        id="kpi-week"
        label="Sắp gửi · 7 ngày tới"
        value={n(totals.data?.upcomingWeek)}
        note={enabled ? "khách đủ điều kiện" : "quà đang tắt, chưa tạo mã"}
      />
      <Kpi
        id="kpi-used"
        label={year === undefined ? "Mã đã dùng · năm nay" : `Mã đã dùng · năm ${String(year.year)}`}
        value={year === undefined ? n(undefined) : `${NUM.format(year.used)} / ${NUM.format(year.issued)}`}
        note="mã đã tặng"
      />
      <Kpi
        id="kpi-saved"
        label="Khách đã lưu sinh nhật"
        value={n(saved)}
        note={c === undefined ? "" : `${NUM.format(c.stopped)} khách đã hủy nhận`}
      />
    </div>
  );
}

export function BirthdayGifts() {
  const [params, setParams] = useSearchParams();
  const tab = asTab(params.get("tab"));
  const perms = usePerms();
  const canManage = perms.can(MANAGE);
  const qc = useQueryClient();
  const settings = useGiftSettings();
  const todo = useTodo();
  const [toast, setToast] = useState<string | null>(null);
  const [busyRun, setBusyRun] = useState(false);
  const runNow = useWriteAction((key: string) => unwrap(runBirthdayGifts({ headers: headers(key) })));

  const go = (t: Tab, extra: Record<string, string> = {}) => {
    setParams({ tab: t, ...extra });
  };
  const onRun = () => {
    setBusyRun(false);
    setToast(null);
    void runNow.run(undefined).then((r) => {
      if (r.ok) {
        setToast(`Đã chạy xong: ${String(r.data.given)} mã mới, ${String(r.data.failed)} lỗi.`);
        reloadBirthday(qc);
      } else if (r.error instanceof ApiError && r.error.code === "another_run_in_progress") {
        runNow.reset();
        setBusyRun(true);
      }
    });
  };
  const enabled = settings.data?.enabled ?? true;
  const todoCount = todo.data?.items.length;

  return (
    <div className="bg">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Khuyến mãi / Quà sinh nhật</p>
          <h1>Quà sinh nhật</h1>
          <p className="sub">
            Khách lưu ngày sinh ở trang cảm ơn của website. Trước sinh nhật, CRM tạo mỗi khách một mã chỉ dùng một lần
            cho đơn giao tới đúng số của khách; website tự gửi qua Zalo, email với khách đồng ý kênh đó.
          </p>
        </div>
        {canManage && (
          <div className="row">
            <button type="button" className="btn btn-dark" disabled={runNow.pending} onClick={onRun}>
              {runNow.pending ? "Đang tạo mã…" : "Tạo mã ngay"}
            </button>
          </div>
        )}
      </div>
      {perms.loaded && !canManage && <p className="reason">{TEXT_READ_ONLY}</p>}
      {busyRun && (
        <div className="box box-err stack-sm bg-gap">
          <b>Đang có một lần tạo mã khác chạy.</b>
          <p>Thử lại sau vài phút; không cần bấm lại nhiều lần. Kết quả hiện ở dòng "Lần chạy gần nhất".</p>
        </div>
      )}
      {runNow.message !== null && <div className="box box-err bg-gap">{runNow.message}</div>}
      {settings.isError ? (
        <ErrorBox error={settings.error} />
      ) : settings.data !== undefined ? (
        <RunBox
          s={settings.data}
          onSettings={() => {
            go("cai-dat");
          }}
        />
      ) : null}
      <Kpis enabled={enabled} />

      <div className="scroll-x">
        <div className="tabs" role="tablist" aria-label="Quà sinh nhật">
          {TABS.map(([t, label]) => (
            <button
              key={t}
              type="button"
              className="tab"
              role="tab"
              aria-selected={t === tab}
              onClick={() => {
                go(t);
              }}
            >
              {t === "can" && todoCount !== undefined ? `${label} (${String(todoCount)})` : label}
            </button>
          ))}
        </div>
      </div>
      <Toast text={toast} />
      <div className="bg-panel">
        {tab === "can" && <TodoTab todo={todo} canManage={canManage} onToast={setToast} />}
        {tab === "khach" && <CustomersTab enabled={enabled} canManage={canManage} onToast={setToast} />}
        {tab === "cai-dat" && <SettingsTab settings={settings} canManage={canManage} onToast={setToast} />}
        {tab === "dot-ma" && (
          <BatchList
            open={params.get("dot") ?? ""}
            onOpen={(id) => {
              go("dot-ma", { dot: id });
            }}
          />
        )}
      </div>
    </div>
  );
}
