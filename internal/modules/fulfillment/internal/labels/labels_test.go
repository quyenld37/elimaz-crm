package labels_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/labels"
)

func id(b byte) uuid.UUID { return uuid.UUID{15: b} }

// VC04: 3 vận đơn của A, 1 của B, 1 nhập tay, 1 chưa có mã, 1 đã hủy → lô A (3, id tăng dần), lô B (1), 3 bỏ qua có lý do.
func TestPlan_GroupsByAccount(t *testing.T) {
	a, b := id(1), id(2)
	api := func(s, acc uuid.UUID) labels.Shipment {
		return labels.Shipment{ID: s, AccountID: acc, Mode: "api", Status: "awaiting_pickup", HasTracking: true, Labels: true, Batch: true}
	}
	manual := api(id(30), a)
	manual.Mode = "manual"
	creating := api(id(31), b)
	creating.HasTracking, creating.Status = false, "creating"
	cancelled := api(id(32), a)
	cancelled.Status = "cancelled"
	in := []labels.Shipment{api(id(13), a), api(id(20), b), manual, api(id(11), a), creating, api(id(12), a), cancelled, api(id(11), a)}

	groups, skips := labels.Plan(in)
	want := []labels.Group{{AccountID: a, ShipmentIDs: []uuid.UUID{id(11), id(12), id(13)}}, {AccountID: b, ShipmentIDs: []uuid.UUID{id(20)}}}
	if !slices.EqualFunc(groups, want, func(x, y labels.Group) bool {
		return x.AccountID == y.AccountID && slices.Equal(x.ShipmentIDs, y.ShipmentIDs)
	}) {
		t.Fatalf("lô = %v, muốn %v", groups, want)
	}
	wantSkips := []labels.Skip{
		{ShipmentID: id(30), Reason: labels.ReasonNoCarrierLabel},
		{ShipmentID: id(31), Reason: labels.ReasonNoTrackingCode},
		{ShipmentID: id(32), Reason: labels.ReasonCancelled},
	}
	if !slices.Equal(skips, wantSkips) {
		t.Fatalf("bỏ qua = %v, muốn %v", skips, wantSkips)
	}
}

// Hãng không in nhiều vận đơn một lần (Q-18) → mỗi vận đơn một lô; hãng không in nhãn → no_carrier_label.
func TestPlan_SingleAndUnsupported(t *testing.T) {
	a := id(1)
	one := func(s uuid.UUID) labels.Shipment {
		return labels.Shipment{ID: s, AccountID: a, Mode: "api", Status: "shipping", HasTracking: true, Labels: true}
	}
	no := one(id(9))
	no.Labels = false
	groups, skips := labels.Plan([]labels.Shipment{one(id(5)), one(id(3)), no})
	if len(groups) != 2 || groups[0].ShipmentIDs[0] != id(3) || groups[1].ShipmentIDs[0] != id(5) ||
		len(groups[0].ShipmentIDs) != 1 || len(groups[1].ShipmentIDs) != 1 {
		t.Fatalf("lô = %v, muốn hai lô một vận đơn (3, 5)", groups)
	}
	if len(skips) != 1 || skips[0] != (labels.Skip{ShipmentID: id(9), Reason: labels.ReasonNoCarrierLabel}) {
		t.Fatalf("bỏ qua = %v", skips)
	}
}

// Quy luật: mỗi vận đơn (không trùng) ở đúng một lô hoặc đúng một dòng bỏ qua; mỗi lô một tài khoản, id tăng dần;
// hãng in theo lô thì mỗi tài khoản đúng một lô.
func TestPlan_Partition(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ss := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) labels.Shipment {
			return labels.Shipment{
				ID: id(rapid.ByteRange(1, 40).Draw(t, "id")), AccountID: id(rapid.ByteRange(100, 103).Draw(t, "acc")),
				Mode:        rapid.SampledFrom([]string{"api", "manual"}).Draw(t, "mode"),
				Status:      rapid.SampledFrom([]string{"creating", "awaiting_pickup", "cancelled"}).Draw(t, "status"),
				HasTracking: rapid.Bool().Draw(t, "tracking"), Labels: true, Batch: true,
			}
		}), 0, 60).Draw(t, "ss")
		checkPartition(t, ss)
	})
}

// checkPartition kiểm quy luật của Plan trên ss.
func checkPartition(t *rapid.T, ss []labels.Shipment) {
	groups, skips := labels.Plan(ss)
	seen := map[uuid.UUID]int{}
	accounts := map[uuid.UUID]bool{}
	for _, g := range groups {
		if accounts[g.AccountID] {
			t.Fatalf("tài khoản %s có hai lô", g.AccountID)
		}
		accounts[g.AccountID] = true
		if !slices.IsSortedFunc(g.ShipmentIDs, func(x, y uuid.UUID) int { return bytes.Compare(x[:], y[:]) }) {
			t.Fatalf("lô không theo id tăng dần: %v", g.ShipmentIDs)
		}
		for _, s := range g.ShipmentIDs {
			seen[s]++
		}
	}
	for _, s := range skips {
		seen[s.ShipmentID]++
	}
	distinct := map[uuid.UUID]bool{}
	for _, s := range ss {
		distinct[s.ID] = true
	}
	if len(seen) != len(distinct) {
		t.Fatalf("có %d vận đơn trong kết quả, muốn %d", len(seen), len(distinct))
	}
	for s, n := range seen {
		if n != 1 || !distinct[s] {
			t.Fatalf("vận đơn %s xuất hiện %d lần", s, n)
		}
	}
}

func TestCheckPDF(t *testing.T) {
	big := append([]byte("%PDF-1.7\n"), make([]byte, 100)...)
	for _, c := range []struct {
		name string
		b    []byte
		max  int
		want string
	}{
		{"pdf", []byte("%PDF-1.4\n..."), 100, ""},
		{"đúng giới hạn", big, len(big), ""},
		{"quá giới hạn 1 byte", big, len(big) - 1, labels.ErrTooLarge},
		{"rỗng", nil, 100, labels.ErrTooLarge},
		{"html", []byte("<html>lỗi</html>"), 100, labels.ErrInvalidPDF},
		{"thiếu gạch", []byte("%PDF1.4"), 100, labels.ErrInvalidPDF},
	} {
		if got := labels.CheckPDF(c.b, c.max); got != c.want {
			t.Errorf("%s: CheckPDF = %q, muốn %q", c.name, got, c.want)
		}
	}
}
