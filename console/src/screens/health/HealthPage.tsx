// Màn Sức khỏe hệ thống (kế hoạch M1 Task 53; mockup đã duyệt docs/mockups/m1/suc-khoe.html, trạng thái A, B). Đọc
// getPlatformHealth (quyền platform.health.read), tự tải lại mỗi 20 s (ADR-42) và khi lớp realtime báo resync (nối
// sẵn ở queryClient). Trạng thái C (không có quyền) do định tuyến chung lo (app/router.tsx).
import { useQuery } from "@tanstack/react-query";
import { useId, type ReactNode } from "react";
import { Link } from "react-router";
import { getPlatformHealth, type BackupHealth, type PlatformHealth } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import {
  ageText,
  allClear,
  beatText,
  clock,
  clockDay,
  count,
  downAlertAt,
  hoursText,
  processRows,
  type ProcessRow,
} from "./healthRules";
import "./health.css";

/** Chu kỳ tự tải lại (ADR-42). */
export const REFRESH_MS = 20_000;
/** Số loại lỗi hiện trên màn này; đủ 10 loại ở màn Sự kiện lỗi (Task 330). */
const TOP_ERRORS = 3;
/** Đường dẫn màn Sự kiện lỗi (Task 330, tên theo mockup su-kien-loi.html). */
export const ERRORS_PATH = "/su-kien-loi";

/** Việc bị ảnh hưởng khi một vai trò ngừng (spec 04: ba vai trò ingest, api, worker). */
const ROLE_EFFECT: Record<string, string> = {
  worker: "Việc nền (xử lý ảnh, gửi lệnh ra hãng) đang chờ.",
  ingest: "Tin và webhook mới chưa được nhận vào hệ thống.",
  api: "Màn hình có thể không tải được dữ liệu.",
};

export function HealthPage() {
  const q = useQuery({
    queryKey: queryKey("platform.health"),
    queryFn: () => unwrap(getPlatformHealth()),
    refetchInterval: REFRESH_MS,
  });
  const h = q.data;

  return (
    <>
      <div className="page-h">
        <div>
          <h1>Sức khỏe hệ thống</h1>
          <p className="sub">
            Tự cập nhật mỗi 20 giây
            {q.dataUpdatedAt > 0 && ` · lần cuối ${clock(new Date(q.dataUpdatedAt), true)}`}
          </p>
        </div>
        <button
          type="button"
          className="btn btn-quiet"
          disabled={q.isFetching}
          onClick={() => {
            void q.refetch();
          }}
        >
          Tải lại ngay
        </button>
      </div>

      {q.isPending && <p className="muted">Đang tải…</p>}
      {q.isError && (
        <div className="box box-err" style={{ marginBottom: 16 }}>
          {h === undefined
            ? errorMessage(q.error)
            : `Không tải lại được: ${errorMessage(q.error)} Số dưới là lần cuối.`}
        </div>
      )}
      {h !== undefined && (allClear(h) ? <AllClear h={h} /> : <Detail h={h} />)}
    </>
  );
}

/** Trạng thái B: mọi thứ bình thường. */
function AllClear({ h }: { h: PlatformHealth }) {
  return (
    <div className="stack">
      <div className="box box-ok">
        <b className="ok-text">Mọi tiến trình đang chạy.</b> Không có việc hỏng, không có cảnh báo trong 24 giờ.
      </div>
      <div className="kpis">
        {processRows(h.processes).map((p) => (
          <ProcessKpi key={p.role} p={p} withAge={false} />
        ))}
        <Kpi label="Hỏng / Không chắc đã gửi">
          <div className="v">
            {count(h.queue.dead)} / {count(h.queue.inDoubt)}
          </div>
        </Kpi>
      </div>
      <div className="kpis">
        <BackupKpis b={h.backup} />
      </div>
    </div>
  );
}

/** Trạng thái A: có gì đó cần xem. */
function Detail({ h }: { h: PlatformHealth }) {
  const rows = processRows(h.processes);
  const down = rows.filter((p) => p.down);
  return (
    <div className="stack">
      {down.length > 0 && (
        <div className="box box-err" role="alert">
          <b>
            {count(down.length)} tiến trình ngừng: {down.map((p) => p.role).join(", ")}.
          </b>{" "}
          <span>
            {down
              .map((p) => {
                const at = downAlertAt(h.alerts, p.role);
                return [ROLE_EFFECT[p.role], at && `Đã gửi cảnh báo gấp lúc ${clock(at)}.`].filter(Boolean).join(" ");
              })
              .filter((s) => s !== "")
              .join(" ")}
          </span>
        </div>
      )}

      <div>
        <h2>Tiến trình</h2>
        <div className="kpis" style={{ marginTop: 8 }}>
          {rows.map((p) => (
            <ProcessKpi key={p.role} p={p} withAge />
          ))}
        </div>
      </div>

      <div>
        <h2>Hàng đợi việc nền</h2>
        <div className="kpis" style={{ marginTop: 8 }}>
          <Kpi label="Việc chờ lâu nhất">
            <div className="v">{h.queue.pending === 0 ? "—" : ageText(h.queue.oldestPendingAgeS)}</div>
          </Kpi>
          <Kpi label="Đang chờ">
            <div className="v">{count(h.queue.pending)}</div>
          </Kpi>
          <Kpi label="Hỏng (đã thử hết lần)" bad={h.queue.dead > 0}>
            <div className="v">{count(h.queue.dead)}</div>
            {h.queue.dead > 0 && <p className="small">Cần người xem</p>}
          </Kpi>
          <Kpi label="Không chắc đã gửi" bad={h.queue.inDoubt > 0}>
            <div className="v">{count(h.queue.inDoubt)}</div>
            {h.queue.inDoubt > 0 && <p className="small">Không tự gửi lại; cần tra ở bên nhận</p>}
          </Kpi>
        </div>
      </div>

      <div className="cols">
        <div className="stack">
          <div className="kpis">
            <Kpi label="Tin chờ ghi (ingest)">
              <div className="v">{count(h.ingest.spoolFiles)}</div>
              <p className="small muted">tệp đệm trên đĩa</p>
            </Kpi>
            <BackupKpis b={h.backup} />
          </div>
          <ErrorsCard h={h} />
        </div>
        <AlertsCard h={h} />
      </div>
    </div>
  );
}

function Kpi({ label, bad = false, children }: { label: string; bad?: boolean; children: ReactNode }) {
  const id = useId();
  return (
    <div className={bad ? "kpi is-bad" : "kpi"} role="group" aria-labelledby={id}>
      <span className="small muted" id={id}>
        {label}
      </span>
      {children}
    </div>
  );
}

function ProcessKpi({ p, withAge }: { p: ProcessRow; withAge: boolean }) {
  let state: string;
  if (!p.down) state = "Đang chạy";
  else if (p.ageS === null) state = "Chưa có nhịp sống";
  else state = `Ngừng ${ageText(p.ageS)}`;
  return (
    <div className={p.down ? "kpi is-bad" : "kpi"} role="group" aria-label={p.role}>
      <span className="small muted">{p.role}</span>
      <div className={p.down ? "st-l err-text" : "st-l ok-text"}>
        <span className={p.down ? "dot dot-err" : "dot dot-ok"} aria-hidden="true" />
        {state}
      </div>
      {withAge && p.ageS !== null && (
        <p className={p.down ? "small" : "small muted"}>
          {p.down ? `Nhịp sống cuối ${beatText(p.ageS)} trước` : `Nhịp sống ${beatText(p.ageS)} trước`}
        </p>
      )}
    </div>
  );
}

function BackupKpis({ b }: { b: BackupHealth }) {
  const drillFailed = b.lastDrillOk === false;
  return (
    <>
      <Kpi label="Sao lưu đầy đủ gần nhất">
        <div className="v">{b.lastBaseAgeH === undefined ? "—" : hoursText(b.lastBaseAgeH)}</div>
        <p className="small muted">{b.lastBaseAgeH === undefined ? "Chưa có bản nào" : "trước"}</p>
      </Kpi>
      <Kpi label="Diễn tập khôi phục gần nhất" bad={drillFailed}>
        <div className="v">{b.lastDrillAgeH === undefined ? "—" : hoursText(b.lastDrillAgeH)}</div>
        {b.lastDrillAgeH === undefined && <p className="small muted">Chưa diễn tập</p>}
        {b.lastDrillOk !== undefined && (
          <div className={drillFailed ? "st-l err-text" : "st-l ok-text"}>
            <span className={drillFailed ? "dot dot-err" : "dot dot-ok"} aria-hidden="true" />
            {drillFailed ? "Không đạt" : "Đạt"}
          </div>
        )}
      </Kpi>
    </>
  );
}

function ErrorsCard({ h }: { h: PlatformHealth }) {
  const id = useId();
  return (
    <section className="card" aria-labelledby={id}>
      <div className="row-between">
        <h2 id={id}>Lỗi nhiều nhất 24 giờ</h2>
        {h.errors.length > 0 && (
          <Link className="btn btn-quiet" to={ERRORS_PATH}>
            {`Xem đủ ${String(h.errors.length)} loại`}
          </Link>
        )}
      </div>
      {h.errors.length === 0 ? (
        <p className="muted">Không có lỗi trong 24 giờ.</p>
      ) : (
        <ul className="alerts">
          {h.errors.slice(0, TOP_ERRORS).map((e) => (
            <li key={e.fingerprint} className="row-between">
              <span className="grow">
                <b className="wrap">{e.name}</b>
                <br />
                <span className="small muted">
                  {e.role} · lần cuối {clock(new Date(e.lastSeenAt))}
                </span>
              </span>
              <span className="num bold">{count(e.count)} lần</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function AlertsCard({ h }: { h: PlatformHealth }) {
  const id = useId();
  return (
    <section className="card" aria-labelledby={id}>
      <h2 id={id}>Cảnh báo đã gửi 24 giờ</h2>
      {h.alerts.length === 0 ? (
        <p className="muted">Không có cảnh báo trong 24 giờ.</p>
      ) : (
        <ul className="alerts">
          {h.alerts.map((a, i) => (
            <li key={`${a.key}-${a.sentAt}-${String(i)}`} className="row-between">
              <span className="grow">
                <b className="wrap">{a.key}</b>
                <br />
                <span className="small muted">{clockDay(new Date(a.sentAt))}</span>
              </span>
              {a.severity === "urgent" ? (
                <span className="chip chip-err">Gấp</span>
              ) : (
                <span className="chip">Thường</span>
              )}
            </li>
          ))}
        </ul>
      )}
      <p className="hint" style={{ marginTop: 8 }}>
        Gấp: cần xử lý ngay. Thường: xem trong ngày.
      </p>
    </section>
  );
}
