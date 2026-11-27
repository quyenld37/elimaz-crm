package main

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
)

// variantOrder: mã biến thể của mẫu product theo thứ tự đọc của CRM (vị trí rồi mã).
func (e *seedEnv) variantOrder(product uuid.UUID) []uuid.UUID {
	e.t.Helper()
	var out []uuid.UUID
	e.app(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), "SELECT id FROM catalog.variants WHERE product_id = $1 ORDER BY position, id", product)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err //nolint:wrapcheck // test
	})
	return out
}

// seededFixture: môi trường riêng đã nạp tệp mẫu một lần, kèm tệp đã đọc.
func seededFixture(t *testing.T) (*seedEnv, webFixture) {
	t.Helper()
	e := newSeedEnv(t)
	raw := readFixture(t)
	fx, err := parseWebFixture(raw)
	if err != nil {
		t.Fatal(err)
	}
	e.seed(raw)
	return e, fx
}

func fileOrder(p fixtureProduct) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(p.Variants))
	for _, v := range p.Variants {
		out = append(out, v.ID)
	}
	return out
}

// TestSeedWebFixture_VariantOrder (Task 513): biến thể của mẫu có thứ tự đúng như trong tệp (không theo mã UUID v5); tệp
// đổi thứ tự → chạy lại sắp lại đúng một lần (Updated["variant_order"]), biến thể chỉ có ở CRM đứng sau; chạy lại 0.
func TestSeedWebFixture_VariantOrder(t *testing.T) {
	t.Parallel()
	e, fx := seededFixture(t)
	idx := slices.IndexFunc(fx.Products, func(p fixtureProduct) bool { return len(p.Variants) >= 3 })
	if idx < 0 {
		t.Fatal("tệp mẫu cần một mẫu có ít nhất 3 biến thể")
	}
	fp := fx.Products[idx]
	for _, p := range fx.Products {
		if got := e.variantOrder(p.ID); !slices.Equal(got, fileOrder(p)) {
			t.Errorf("mẫu %s: thứ tự %v, mong như tệp %v", p.Key, got, fileOrder(p))
		}
	}

	// Biến thể nhân viên thêm ở CRM (không có trong tệp).
	var extra catalog.Variant
	e.app(func(tx pgx.Tx) error {
		seller, err := catalog.DefaultSeller(t.Context(), tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		p, err := catalog.GetProduct(t.Context(), tx, seller, fp.ID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		w := catalog.Writer{Events: e.seeder.Events, Queue: e.seeder.Queue}
		extra, err = w.AddVariant(t.Context(), tx, seller, fp.ID, p.Version,
			catalog.VariantInput{SKU: "CRM-ONLY-1", Color: "Riêng CRM", Size: "F", Price: 1000}, e.actor)
		return err //nolint:wrapcheck // test
	})

	// Tệp đảo thứ tự biến thể của mẫu fp.
	swapped := mutate(t, func(m map[string]any) {
		vs := at(m, "products", idx)["variants"].([]any) //nolint:forcetypeassert // test
		slices.Reverse(vs)
	})
	rep := e.seed(swapped)
	if rep.Updated["variant_order"] != 1 || rep.Total() != 1 {
		t.Fatalf("tệp đảo thứ tự: mong đúng 1 lần sắp, có %+v", rep)
	}
	want := slices.Clone(fileOrder(fp))
	slices.Reverse(want)
	want = append(want, extra.ID)
	if got := e.variantOrder(fp.ID); !slices.Equal(got, want) {
		t.Errorf("sau khi đảo: %v, mong %v", got, want)
	}
	if again := e.seed(swapped); again.Total() != 0 {
		t.Fatalf("chạy lại tệp đã đảo phải 0 thay đổi: %+v", again)
	}
}
