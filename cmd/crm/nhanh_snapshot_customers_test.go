package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

func writeSnapshot(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nhanh-snapshot-20261128T000000Z.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Đổi khách của Nhanh: nhiều SĐT (mobile, mobile2, chuỗi có dấu phẩy, số), ngày sinh nhiều dạng, nhóm và nhãn (chuỗi,
// mảng chuỗi, đối tượng có name), ghi chú kèm địa chỉ cũ; id dạng số hay chuỗi; thiếu trường vẫn đổi được.
func TestNhanhImportCustomers_ParseCustomers(t *testing.T) {
	t.Parallel()
	p := writeSnapshot(t, `{"version":1,"customers":[
		{"id":501,"name":"Khách Giả, Một","mobile":"0900000001","mobile2":"0900000002, 0900000003","birthday":"1990-02-01",
		 "group":{"id":3,"name":"VIP"},"tags":["Mới","Sỉ"],"note":"ghi chú","address":"1 Đường Giả"},
		{"id":"502","name":"Khách Hai","mobile":900000004,"birthday":"15/08/1985","tags":"A; B"},
		{"name":"Không id","mobile":"0900000005","birthday":"0000-00-00"},
		"not-an-object"]}`)
	got, err := readNhanhCustomers(p)
	if err != nil || len(got) != 4 {
		t.Fatalf("đọc: %v, %d khách", err, len(got))
	}
	c := got[0]
	if c.Row != 1 || c.LegacyID != 501 || c.Name != "Khách Giả, Một" ||
		strings.Join(c.Phones, "|") != "0900000001|0900000002|0900000003" ||
		c.Birthday == nil || !c.Birthday.Equal(time.Date(1990, 2, 1, 0, 0, 0, 0, time.UTC)) ||
		strings.Join(c.Tags, "|") != "Mới|Sỉ|VIP" || c.Note != "ghi chú\nĐịa chỉ cũ: 1 Đường Giả" {
		t.Errorf("khách 1: %+v", c)
	}
	c = got[1]
	if c.LegacyID != 502 || strings.Join(c.Phones, "|") != "900000004" || c.Birthday == nil || c.Birthday.Day() != 15 ||
		strings.Join(c.Tags, "|") != "A|B" {
		t.Errorf("khách 2: %+v", c)
	}
	if got[2].LegacyID != 0 || got[2].Birthday != nil || got[3].Row != 4 || got[3].LegacyID != 0 {
		t.Errorf("khách thiếu id / sai khuôn: %+v %+v", got[2], got[3])
	}
}

func TestNhanhImportCustomers_SnapshotChecks(t *testing.T) {
	t.Parallel()
	if _, err := readNhanhCustomers(filepath.Join(t.TempDir(), "none.json")); !errors.Is(err, errNhanhSnapshotBad) {
		t.Errorf("tệp không có: %v", err)
	}
	if _, err := readNhanhCustomers(t.TempDir()); !errors.Is(err, errNhanhSnapshotBad) {
		t.Errorf("thư mục: %v", err)
	}
	_, err := readNhanhCustomers(writeSnapshot(t, `{"version":1,"customers":[{"name":"BI-MAT-KHACH"`))
	if !errors.Is(err, errNhanhSnapshotBad) || strings.Contains(err.Error(), "BI-MAT-KHACH") {
		t.Errorf("JSON hỏng: %v", err)
	}
	if _, err := readNhanhCustomers(writeSnapshot(t, `{"version":2,"customers":[]}`)); !errors.Is(err, errNhanhSnapshotVersion) {
		t.Errorf("phiên bản lạ: %v", err)
	}
}

// Lệnh qua đúng đường của main trên database thử có một tenant: chạy khô → không ghi, in số đếm; --apply → nhập; chạy lại →
// bỏ qua hết; khách lỗi → thoát 1 và in {dòng, cột, mã}; tenant lạ, thiếu cờ, tệp sai → thoát 2. Không đâu in tên, SĐT.
func TestRun_NhanhImportCustomersCommandLine(t *testing.T) {
	t.Parallel()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	owner, err := db.Open(t.Context(), ownerURL, db.PoolOptions{MaxConns: 2})
	must(t, err)
	t.Cleanup(owner.Pool.Close)
	s := &testkit.Staff{Owner: owner}
	tenant := s.Tenant(t, "ELIMAZ")
	snap := writeSnapshot(t, `{"version":1,"customers":[
		{"id":601,"name":"BI-MAT-TEN","mobile":"0911222333","mobile2":"0911222444","tags":["BI-MAT-NHAN"]},
		{"id":602,"name":"Khách hai","mobile":"0911222555"},
		{"id":603,"name":"Không SĐT"}]}`)
	env := map[string]string{"CRM_ROLE": "migrate", "CRM_ENV": "dev", "DATABASE_URL": appURL, "DATABASE_OWNER_URL": ownerURL}
	cmd := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := run(t.Context(), append([]string{nhanhImportCustomersCommand}, args...), func(k string) string { return env[k] },
			&out, &errOut)
		all := out.String() + errOut.String()
		for _, bad := range []string{"BI-MAT", "0911", "911222"} {
			if strings.Contains(all, bad) {
				t.Errorf("lệnh in %q: %s", bad, all)
			}
		}
		return code, all
	}
	count := func() int { return s.CountIn(t, tenant, "SELECT count(*) FROM customer.legacy_customers") }
	if code, out := cmd("--snapshot", snap, "--tenant", tenant.String(), "--dry-run"); code != exitFail ||
		!strings.Contains(out, "CHẠY KHÔ") || !strings.Contains(out, "nhập mới 2") ||
		!strings.Contains(out, "dòng 3, cột mobile, mã required") || count() != 0 {
		t.Fatalf("chạy khô: thoát %d, %d dòng\n%s", code, count(), out)
	}
	if code, out := cmd("--snapshot", snap, "--tenant", tenant.String(), "--apply"); code != exitFail ||
		!strings.Contains(out, "ĐÃ GHI") || !strings.Contains(out, "nhập mới 2") || count() != 2 {
		t.Fatalf("apply: thoát %d, %d dòng\n%s", code, count(), out)
	}
	if n := s.CountIn(t, tenant, "SELECT count(*) FROM customer.customer_identities WHERE kind = 'phone'"); n != 3 {
		t.Errorf("số SĐT: %d", n)
	}
	if code, out := cmd("--snapshot", snap, "--tenant", tenant.String(), "--apply"); code != exitFail ||
		!strings.Contains(out, "nhập mới 0, bỏ qua 2") || count() != 2 {
		t.Fatalf("chạy lại: thoát %d, %d dòng\n%s", code, count(), out)
	}
	if code, out := cmd("--snapshot", snap, "--tenant", uuid.NewString(), "--dry-run"); code != exitConfig ||
		!strings.Contains(out, errNhanhImportTenant.Error()) {
		t.Fatalf("tenant lạ: thoát %d\n%s", code, out)
	}
	if code, _ := cmd("--snapshot", snap, "--tenant", tenant.String()); code != exitConfig {
		t.Fatalf("thiếu --dry-run/--apply: thoát %d", code)
	}
	if code, _ := cmd("--snapshot", filepath.Join(t.TempDir(), "none"), "--tenant", tenant.String(), "--dry-run"); code != exitConfig {
		t.Fatalf("tệp không có: thoát %d", code)
	}
}
