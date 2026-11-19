// Hộp "Thêm khách" của danh sách khách (mockup khach-danh-sach.html: nút Thêm khách). Tên bắt buộc, SĐT tùy chọn;
// SĐT đã thuộc khách khác → máy chủ trả 409 identity_taken kèm ownerId: hiện liên kết tới hồ sơ đó, không tạo trùng.
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { createCustomer, type CreateCustomerRequest } from "../../api";
import { ApiError, unwrap } from "../../app/errors";
import { useWriteAction } from "../../app/idempotency";
import { cleanPhone } from "../len-don/orderRules";
import { Dialog } from "../staff/staffData";
import { headers, reloadCustomers } from "./customerData";

const MAX_NAME = 120;

/** Ô "Họ tên" của khách (thêm khách, sửa hồ sơ): tối đa 120 ký tự như hợp đồng. */
export function NameField({
  value,
  onChange,
  error = null,
}: {
  value: string;
  onChange: (v: string) => void;
  error?: string | null;
}) {
  return (
    <label className="field">
      <span className="label">Họ tên</span>
      <input
        className="input"
        maxLength={MAX_NAME}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      />
      {error !== null && <span className="field-err">{error}</span>}
    </label>
  );
}

export function CreateCustomerDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const [needName, setNeedName] = useState(false);
  const create = useWriteAction((key, body: CreateCustomerRequest) =>
    unwrap(createCustomer({ body, headers: headers(key) })),
  );
  const owner =
    create.error instanceof ApiError && create.error.code === "identity_taken"
      ? create.error.details.ownerId
      : undefined;

  const save = async () => {
    if (name.trim() === "") {
      setNeedName(true);
      return;
    }
    const p = cleanPhone(phone);
    const r = await create.run({ fullName: name.trim(), ...(p === "" ? {} : { phone: p }) });
    if (!r.ok) return;
    reloadCustomers(qc);
    void navigate(`/khach/${r.data.rootId}`);
  };

  return (
    <Dialog
      title="Thêm khách"
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn btn-line" onClick={onClose}>
            Hủy
          </button>
          <button
            type="button"
            className="btn btn-dark"
            disabled={create.pending}
            onClick={() => {
              void save();
            }}
          >
            Thêm khách
          </button>
        </>
      }
    >
      <div className="stack-sm">
        <NameField
          value={name}
          onChange={(v) => {
            setName(v);
            setNeedName(false);
          }}
          error={needName ? "Điền họ tên khách" : null}
        />
        <label className="field">
          <span className="label">SĐT</span>
          <input
            className="input num"
            inputMode="tel"
            value={phone}
            onChange={(e) => {
              setPhone(e.target.value);
            }}
          />
        </label>
        {create.message !== null && (
          <div className="box box-err" role="alert">
            <b>{create.message}.</b>
            {typeof owner === "string" && (
              <>
                {" "}
                <Link className="cell-a" to={`/khach/${owner}`}>
                  Mở hồ sơ khách đó
                </Link>
              </>
            )}
          </div>
        )}
      </div>
    </Dialog>
  );
}
