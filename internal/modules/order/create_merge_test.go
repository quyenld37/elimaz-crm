package order_test

// Ca thêm ở Task 403 (mutation testing vùng cao M1, docs/mutation.md): biên của mergeLines, Writer thiếu phụ thuộc.

import (
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Gộp hai dòng cùng biến thể: tổng số lượng đúng MaxInt32 được; vượt một cái, hay tổng giảm dòng không hợp lệ (âm,
// tràn) → ErrInvalidInput.
func TestMergeLines_Bounds(t *testing.T) {
	t.Parallel()
	v := uuid.New()
	got, err := order.MergeLines([]order.LineInput{{VariantID: v, Quantity: math.MaxInt32 - 5}, {VariantID: v, Quantity: 5}})
	if err != nil || len(got) != 1 || got[0].Quantity != math.MaxInt32 {
		t.Fatalf("tổng đúng MaxInt32: %+v %v", got, err)
	}
	got, err = order.MergeLines([]order.LineInput{
		{VariantID: v, Quantity: 1, LineDiscount: catalog.MaxMoney - 7}, {VariantID: v, Quantity: 1, LineDiscount: 7},
	})
	if err != nil || len(got) != 1 || got[0].LineDiscount != catalog.MaxMoney {
		t.Fatalf("tổng giảm dòng đúng MaxMoney: %+v %v", got, err)
	}
	bad := map[string][]order.LineInput{
		"tổng số lượng MaxInt32+1": {{VariantID: v, Quantity: math.MaxInt32 - 4}, {VariantID: v, Quantity: 5}},
		"giảm dòng sau âm":         {{VariantID: v, Quantity: 1}, {VariantID: v, Quantity: 1, LineDiscount: -1}},
		"giảm dòng đầu âm":         {{VariantID: v, Quantity: 1, LineDiscount: -1}, {VariantID: v, Quantity: 1}},
		"giảm dòng tràn":           {{VariantID: v, Quantity: 1, LineDiscount: math.MaxInt64}, {VariantID: v, Quantity: 1, LineDiscount: 1}},
		"giảm dòng MaxMoney+1":     {{VariantID: v, Quantity: 1, LineDiscount: catalog.MaxMoney - 6}, {VariantID: v, Quantity: 1, LineDiscount: 7}},
	}
	for name, items := range bad {
		if _, err := order.MergeLines(items); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("%s: lỗi = %v, mong ErrInvalidInput", name, err)
		}
	}
}

// Writer thiếu Events hoặc Queue → CreateManual lỗi (không tạo đơn, không giữ hàng).
func TestCreateManual_WriterNotWired(t *testing.T) {
	t.Parallel()
	e := newOrderEnv(t)
	v := e.variant(1000, 5)
	full := e.writer()
	for name, w := range map[string]order.Writer{
		"thiếu Queue":  {Events: full.Events, Realtime: full.Realtime},
		"thiếu Events": {Queue: full.Queue, Realtime: full.Realtime},
	} {
		err := e.app(func(tx pgx.Tx) error {
			c, err := customer.Create(t.Context(), tx, "Chị Lan")
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			in := e.input(0, item(v, 1))
			in.CustomerID = c
			_, err = w.CreateManual(t.Context(), tx, in, e.actor)
			return err //nolint:wrapcheck // test
		})
		if err == nil {
			t.Errorf("%s: tạo đơn phải lỗi", name)
		}
	}
	if b := e.balance(v); b.Held != 0 {
		t.Errorf("giữ hàng %d, mong 0", b.Held)
	}
}
