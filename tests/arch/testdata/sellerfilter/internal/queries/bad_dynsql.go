package queries

import "strings"

func listOK(b *strings.Builder) {
	b.WriteString(`SELECT o.id FROM ord.orders_v AS o WHERE o.seller_id = $1`)
	b.WriteString("\n\tAND o.status = $2")
}

func listBad(b *strings.Builder) {
	b.WriteString(`SELECT o.id FROM ord.orders_v AS o
JOIN ord.order_items_v AS i ON i.order_id = o.id
WHERE o.seller_id = $1`)
}
