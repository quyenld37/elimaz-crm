// Màn Tệp xuất và chạy nền /bao-cao/tep-xuat (Task 241, M6-18; mockup docs/m6/mockup/tep-xuat.html trạng thái A–C; BC04).
// Ba nhóm theo quyền người xem (bán hàng, tồn kho, danh sách khách), mỗi nhóm một bảng các lần chạy mới nhất trước.
// Trạng thái bằng chữ; "Tải về" chỉ ở lần chạy Excel đã xong: gọi …/file lấy đường dẫn ký sẵn rồi mở; "Xem" cho lần
// chạy dạng bảng. Tự tải lại khi có báo hiệu report.run_finished và mỗi 20 giây (lưới an toàn của 04 mục 6). Hạn tải lấy
// từ expiresAt của máy chủ.
import { useEffect, useState } from "react";
import { Link } from "react-router";
import type { ReportRun } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { useQuery } from "@tanstack/react-query";
import { listCustomerTags } from "../../api";
import { tagsKey } from "../khach/customerData";
import { ReportsPage } from "./ReportsLayout";
import { markSeen } from "./ExportsNavCount";
import { count } from "./format";
import { openFile } from "./openFile";
import { useReportAccess } from "./reportAccess";
import { FAMILIES, reportName, scopeText, statusText, useRunList, viewPath, type FamilyDef } from "./runsData";
import { VN_TIME_ZONE } from "../../app/time";

const SUB =
  "Các lần xuất Excel và báo cáo nặng chạy nền. Tệp báo cáo tải được trong 7 ngày, tệp danh sách khách 24 giờ; chỉ người tạo và người có quyền quản lý tệp xuất tải được, mỗi lần tải được ghi nhật ký.";

const STAMP = new Intl.DateTimeFormat("en-GB", {
  timeZone: VN_TIME_ZONE,
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

/** "04/12/2026 14:10" theo giờ Việt Nam. */
export function stampText(iso: string): string {
  const p = Object.fromEntries(STAMP.formatToParts(new Date(iso)).map((x) => [x.type, x.value]));
  return `${p.day ?? ""}/${p.month ?? ""}/${p.year ?? ""} ${p.hour ?? ""}:${p.minute ?? ""}`;
}

function DownloadButton({ run, def }: { run: ReportRun; def: FamilyDef }) {
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const go = () => {
    setErr(null);
    setBusy(true);
    void def.file(run.id).then(
      (link) => {
        setBusy(false);
        setErr(openFile(link.url));
      },
      (e: unknown) => {
        setBusy(false);
        setErr(errorMessage(e));
      },
    );
  };
  return (
    <>
      <button type="button" className="btn btn-line" disabled={busy} onClick={go}>
        Tải về
      </button>
      {err !== null && <p className="field-err">{err}</p>}
    </>
  );
}

function RunRow({ run, def, tags }: { run: ReportRun; def: FamilyDef; tags: ReadonlyMap<string, string> }) {
  const scope = scopeText(run, tags);
  return (
    <tr>
      <td>{reportName(run)}</td>
      <td className="l">{scope}</td>
      <td className="l">
        <span className="nm" title={run.requestedByName}>
          {run.requestedByName}
        </span>
      </td>
      <td className="l">
        <span>{statusText(run)}</span>
        {run.status === "ready" && run.truncated && run.rowCount !== undefined && (
          <span className="small muted block">{`Đã cắt ở ${count(run.rowCount)} dòng`}</span>
        )}
      </td>
      <td>{run.rowCount === undefined ? "—" : count(run.rowCount)}</td>
      <td className="l">
        {run.status === "queued" || run.status === "running" || run.status === "failed"
          ? "—"
          : stampText(run.expiresAt)}
      </td>
      <td className="l">
        {run.status === "ready" && run.format === "xlsx" && <DownloadButton run={run} def={def} />}
        {run.status === "ready" && run.format === "json" && (
          <Link className="btn btn-line" to={viewPath(run)}>
            Xem
          </Link>
        )}
      </td>
    </tr>
  );
}

function Group({ def, tags }: { def: FamilyDef; tags: ReadonlyMap<string, string> }) {
  const list = useRunList(def, true);
  const runs = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section className="stack-sm" aria-label={def.title}>
      <h2>{def.title}</h2>
      {list.isPending && <p className="muted">Đang tải danh sách…</p>}
      {list.isError && (
        <div className="box box-err stack-sm" role="alert">
          <b>Không tải được danh sách tệp xuất.</b>
          <p className="small">{errorMessage(list.error)}</p>
          <div className="row">
            <button
              type="button"
              className="btn btn-line"
              onClick={() => {
                void list.refetch();
              }}
            >
              Thử lại
            </button>
          </div>
        </div>
      )}
      {list.isSuccess && runs.length === 0 && (
        <div className="box box-info stack-sm">
          <b>Chưa có tệp xuất nào</b>
          <p className="small">Bấm "Xuất Excel" trên một màn báo cáo; tệp sẽ hiện ở đây.</p>
        </div>
      )}
      {runs.length > 0 && (
        <div className="rt-frame scroll-x">
          <table className="rt" aria-label={def.title}>
            <thead>
              <tr>
                <th>Báo cáo</th>
                <th className="l">{def.scopeHeader}</th>
                <th className="l">Người tạo</th>
                <th className="l">Trạng thái</th>
                <th>Số dòng</th>
                <th className="l">Hạn tải</th>
                <th className="l">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((r) => (
                <RunRow key={r.id} run={r} def={def} tags={tags} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage && (
        <div className="row">
          <button
            type="button"
            className="btn btn-line"
            disabled={list.isFetchingNextPage}
            onClick={() => {
              void list.fetchNextPage();
            }}
          >
            Tải thêm
          </button>
        </div>
      )}
    </section>
  );
}

export function RunsPage() {
  const access = useReportAccess();
  const tags = useQuery({
    queryKey: tagsKey(),
    queryFn: () => unwrap(listCustomerTags()),
    enabled: access.can("customer.export"),
  });
  const tagNames = new Map((tags.data?.items ?? []).map((t) => [t.id, t.name]));
  const groups = FAMILIES.filter((d) => access.can(d.permission));
  useEffect(() => {
    markSeen(new Date());
  }, []);
  return (
    <ReportsPage title="Tệp xuất" sub={SUB}>
      {groups.map((d) => (
        <Group key={d.family} def={d} tags={tagNames} />
      ))}
      <p className="xs muted">
        Nhóm chỉ hiện khi bạn có quyền tương ứng (bán hàng, tồn kho, xuất danh sách khách). Bấm Tải về: máy tạo đường
        dẫn tải có hạn 5 phút rồi mở ngay; bấm lại để có đường dẫn mới. Nội dung ô bắt đầu bằng "=" hiện nguyên chữ.
      </p>
    </ReportsPage>
  );
}
