// Màn Kho — Hàng hoàn đơn cũ (kế hoạch M7 Task 273; mockup đã duyệt docs/mockups/m7/hang-hoan-don-cu.html, MO08).
// Nhập vào kho hàng hoàn của đơn bán trên công cụ cũ (trước ngày mở CRM): không tạo đơn hay vận đơn, mỗi kiện một lần
// theo mã tham chiếu. Hàng hoàn của đơn tạo trong CRM vẫn nhận ở "Nhận hàng hoàn" (M3).
import { useState } from "react";
import "../stocktake.css";
import "../m7.css";
import { LegacyReturnForm } from "./LegacyReturnForm";
import { LegacyReturnList } from "./LegacyReturnList";

export function LegacyReturnPage() {
  const [refreshKey, setRefreshKey] = useState(0);
  const [focus, setFocus] = useState<{ day: string; id: string } | null>(null);
  return (
    <div className="m7 stack">
      <div className="page-h">
        <div>
          <h1>Hàng hoàn đơn cũ</h1>
          <p className="sub">
            Nhập vào kho hàng hoàn của đơn bán trên công cụ cũ (trước ngày mở CRM). Không tạo đơn hay vận đơn nào; mỗi
            kiện chỉ nhập một lần theo mã tham chiếu.
          </p>
        </div>
      </div>
      <LegacyReturnForm
        onCreated={() => {
          setRefreshKey((k) => k + 1);
        }}
        onJump={(day, id) => {
          setFocus({ day, id });
        }}
      />
      <LegacyReturnList focus={focus} refreshKey={refreshKey} />
    </div>
  );
}
