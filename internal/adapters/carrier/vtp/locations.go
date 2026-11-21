package vtp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

const codeNoParent = "no_parent"

type provinceRow struct {
	ID   json.Number `json:"PROVINCE_ID"`
	Name string      `json:"PROVINCE_NAME"`
}

type wardRow struct {
	ID   json.Number `json:"WARDS_ID"`
	Name string      `json:"WARDS_NAME"`
}

// Locations đọc danh mục địa giới mới V3 (2 cấp): province → listProvinceNew; ward → listWardsNew?provinceId=<ID
// tỉnh của VTP> (ParentCarrierCode là tỉnh đã hỏi). Dòng thiếu ID hay tên bị bỏ. Cấp khác → ErrNotSupported.
func (a *Adapter) Locations(ctx context.Context, acc fulfillment.Account, level, parent string) ([]fulfillment.Location, error) {
	switch level {
	case levelProvince:
		var rows []provinceRow
		if err := a.call(ctx, acc, kindRead, http.MethodGet, "/v3/categories/listProvinceNew", nil, &rows); err != nil {
			return nil, fmt.Errorf("đọc tỉnh Viettel Post: %w", err)
		}
		ls := make([]fulfillment.Location, len(rows))
		for i, r := range rows {
			ls[i] = fulfillment.Location{Level: levelProvince, CarrierCode: r.ID.String(), Name: r.Name}
		}
		return complete(ls), nil
	case levelWard:
		if _, err := strconv.ParseInt(parent, 10, 64); err != nil {
			return nil, fmt.Errorf("đọc phường Viettel Post: %w", rejected(codeNoParent))
		}
		var rows []wardRow
		path := "/v3/categories/listWardsNew?" + url.Values{"provinceId": {parent}}.Encode()
		if err := a.call(ctx, acc, kindRead, http.MethodGet, path, nil, &rows); err != nil {
			return nil, fmt.Errorf("đọc phường Viettel Post: %w", err)
		}
		ls := make([]fulfillment.Location, len(rows))
		for i, r := range rows {
			ls[i] = fulfillment.Location{Level: levelWard, CarrierCode: r.ID.String(), Name: r.Name, ParentCarrierCode: parent}
		}
		return complete(ls), nil
	}
	return nil, fmt.Errorf("địa giới Viettel Post cấp %q: %w", level, fulfillment.ErrNotSupported)
}

// complete bỏ dòng thiếu mã hay tên (mẫu tỉnh của tài liệu có dòng ghi sai tên trường).
func complete(ls []fulfillment.Location) []fulfillment.Location {
	out := ls[:0]
	for _, l := range ls {
		if l.CarrierCode != "" && l.Name != "" {
			out = append(out, l)
		}
	}
	return out
}
