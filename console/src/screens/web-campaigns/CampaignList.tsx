// Danh sách Chiến dịch giảm giá /khuyen-mai/chien-dich (kế hoạch M5 Task 458, M5-46; mockup
// docs/mockups/m5/web-campaigns.html trạng thái A, C, D; HV16). Máy chủ trả mới nhất trước, lọc một trạng thái hiển thị,
// q theo tên; tải thêm theo con trỏ, giữ cỡ trang 10/20/50. Kích hoạt, tạm dừng, sửa khi đang chạy, xóa là hộp thoại
// trên danh sách. Báo hiệu promotion.web_campaign tải lại danh sách.
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { listWebCampaigns, type WebCampaign, type WebCampaignPage, type WebCampaignStatus } from "../../api";
import { errorMessage, unwrap } from "../../app/errors";
import { queryKey } from "../../app/queryClient";
import { SEARCH_DELAY_MS, useDebounced } from "../inventory/Balances";
import { ActCell, ActLink } from "../../shared/ActCell";
import { MoreBar } from "../ma-giam-gia/MoreBar";
import { CAMPAIGN_TOPIC, reloadCampaigns, storeCampaign, usePerms } from "./campaignData";
import { DeleteDialog, StateDialog, type StateMode } from "./CampaignDialogs";
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  STATUSES,
  TEXT_READ_ONLY,
  campaignLink,
  linkText,
  periodText,
  rowActions,
  rowSub,
  statusChip,
  statusText,
  type RowAction,
} from "./campaignRules";
import "../don/don.css";
import "./web-campaigns.css";

type Open = { kind: StateMode | "delete"; campaign: WebCampaign } | null;

const ACTION_TEXT: Readonly<Record<RowAction, string>> = {
  edit: "Sửa",
  pauseEdit: "Sửa",
  pause: "Tạm dừng",
  enable: "Kích hoạt",
  delete: "Xóa",
  copy: "Chép liên kết",
};

const asStatus = (v: string): WebCampaignStatus | "" => STATUSES.find((s) => s === v) ?? "";
const asSize = (v: string): number => PAGE_SIZES.find((n) => String(n) === v) ?? DEFAULT_PAGE_SIZE;

function Row(o: { c: WebCampaign; canManage: boolean; onAction: (a: RowAction, c: WebCampaign) => void }) {
  const { c } = o;
  const chip = statusChip(c.status);
  return (
    <tr className={c.status === "ended" ? "is-muted" : undefined}>
      <td className="lead">
        <div className="nm">
          <b className="clamp2" title={c.name}>
            {c.name}
          </b>
          <span className="small muted">{rowSub(c)}</span>
        </div>
      </td>
      <td data-label="Giảm" className="num">
        <b>{`${String(c.percent)}%`}</b>
      </td>
      <td data-label="Thời gian (giờ VN)">
        <span className="small num">{periodText(c)}</span>
      </td>
      <td data-label="Trạng thái">
        <span className={chip.className}>{chip.label}</span>
      </td>
      <ActCell>
        {rowActions(c, o.canManage).map((a) =>
          a === "edit" ? (
            <ActLink key={a} to={`/khuyen-mai/chien-dich/${c.id}`}>
              {ACTION_TEXT[a]}
            </ActLink>
          ) : (
            <button
              key={a}
              type="button"
              className="btn btn-quiet"
              onClick={() => {
                o.onAction(a, c);
              }}
            >
              {ACTION_TEXT[a]}
            </button>
          ),
        )}
      </ActCell>
    </tr>
  );
}

/** Ô chọn của thanh lọc: options là [giá trị, chữ]. */
export function Pick(o: { label: string; value: string; options: [string, string][]; onPick: (v: string) => void }) {
  return (
    <div className="select-wrap">
      <select
        aria-label={o.label}
        value={o.value}
        onChange={(e) => {
          o.onPick(e.target.value);
        }}
      >
        {o.options.map(([v, t]) => (
          <option key={v} value={v}>
            {t}
          </option>
        ))}
      </select>
    </div>
  );
}

export function CampaignList() {
  const perms = usePerms();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [text, setText] = useState("");
  const [status, setStatus] = useState<WebCampaignStatus | "">("");
  const [size, setSize] = useState(DEFAULT_PAGE_SIZE);
  const [open, setOpen] = useState<Open>(null);
  const [toast, setToast] = useState<string | null>(null);
  const q = useDebounced(text.trim(), SEARCH_DELAY_MS);
  const canManage = perms.can("promotion.manage");

  const query = { ...(q === "" ? {} : { q }), ...(status === "" ? {} : { status }), limit: size };
  const list = useInfiniteQuery({
    queryKey: queryKey(CAMPAIGN_TOPIC, "list", query),
    initialPageParam: "",
    getNextPageParam: (last: WebCampaignPage) => last.nextCursor,
    queryFn: ({ pageParam }) =>
      unwrap(listWebCampaigns({ query: { ...query, ...(pageParam === "" ? {} : { cursor: pageParam }) } })),
  });
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const filtered = q !== "" || status !== "";

  const copy = (c: WebCampaign) => {
    navigator.clipboard.writeText(campaignLink(c)).then(
      () => {
        setToast(`Đã chép liên kết: ${linkText(c)}`);
      },
      () => {
        setToast(`Trình duyệt không cho chép; liên kết: ${linkText(c)}`);
      },
    );
  };
  const onAction = (a: RowAction, c: WebCampaign) => {
    if (a === "copy") copy(c);
    else if (a === "delete") setOpen({ kind: "delete", campaign: c });
    else if (a === "pause" || a === "enable" || a === "pauseEdit") setOpen({ kind: a, campaign: c });
  };
  const close = () => {
    setOpen(null);
  };

  const create = canManage && (
    <Link className="btn btn-dark" to="/khuyen-mai/chien-dich/moi">
      + Tạo chiến dịch
    </Link>
  );

  function body() {
    if (list.isError) return <div className="box box-err">{errorMessage(list.error)}</div>;
    if (list.isSuccess && rows.length === 0) {
      return filtered ? (
        <div className="empty">
          <h2>Không có chiến dịch nào khớp</h2>
        </div>
      ) : (
        <div className="empty">
          <h2>Chưa có chiến dịch</h2>
          <p className="muted">Tạo chiến dịch để giảm giá theo phần trăm trên website.</p>
          {create}
        </div>
      );
    }
    if (rows.length === 0) return null;
    return (
      <>
        <div className="tbl-frame">
          <table className="tbl cp">
            <thead>
              <tr>
                <th>Chiến dịch</th>
                <th>Giảm</th>
                <th>Thời gian (giờ VN)</th>
                <th>Trạng thái</th>
                <th className="c-act">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <Row key={c.id} c={c} canManage={canManage} onAction={onAction} />
              ))}
            </tbody>
          </table>
        </div>
        <MoreBar text={`${String(rows.length)} chiến dịch`} pages={list} />
      </>
    );
  }

  return (
    <div className="wc">
      <div className="page-h">
        <div className="grow">
          <p className="crumb">Khuyến mãi / Chiến dịch giảm giá</p>
          <h1>Chiến dịch giảm giá</h1>
          <p className="sub">
            Giảm theo phần trăm trên website. Giờ nhập theo giờ Việt Nam. CRM tính giá khi khách xem và đặt hàng; nhiều
            chiến dịch cùng một món thì lấy giá thấp nhất.
          </p>
        </div>
        <div className="row">{create}</div>
      </div>
      {perms.loaded && !canManage && <p className="reason">{TEXT_READ_ONLY}</p>}

      <div className="toolbar">
        <input
          className="input is-search"
          type="search"
          aria-label="Tìm chiến dịch"
          placeholder="Tìm chiến dịch theo tên"
          value={text}
          maxLength={120}
          onChange={(e) => {
            setText(e.target.value);
          }}
        />
        <Pick
          label="Trạng thái"
          value={status}
          options={[["", "Mọi trạng thái"], ...STATUSES.map((s): [string, string] => [s, statusText(s)])]}
          onPick={(v) => {
            setStatus(asStatus(v));
          }}
        />
        <Pick
          label="Số dòng mỗi trang"
          value={String(size)}
          options={PAGE_SIZES.map((n): [string, string] => [String(n), `${String(n)} / trang`])}
          onPick={(v) => {
            setSize(asSize(v));
          }}
        />
      </div>
      {body()}
      {toast !== null && (
        <div className="toast wc-toast" role="status">
          <span className="grow">{toast}</span>
        </div>
      )}

      {open !== null && open.kind !== "delete" && (
        <StateDialog
          campaign={open.campaign}
          mode={open.kind}
          onClose={close}
          onDone={(c) => {
            const edit = open.kind === "pauseEdit";
            setOpen(null);
            storeCampaign(qc, c);
            reloadCampaigns(qc);
            if (edit) void navigate(`/khuyen-mai/chien-dich/${c.id}`);
          }}
        />
      )}
      {open?.kind === "delete" && (
        <DeleteDialog
          campaign={open.campaign}
          onClose={close}
          onDone={() => {
            setOpen(null);
            reloadCampaigns(qc);
          }}
        />
      )}
    </div>
  );
}
