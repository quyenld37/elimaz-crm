package live

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Danh sách comment của phiên live (màn Live, Task 172; LV02 phần đọc).
const (
	// CommentsDefaultLimit, CommentsMaxLimit là cỡ trang mặc định và tối đa.
	CommentsDefaultLimit = 200
	CommentsMaxLimit     = 500
	// CommentsSlack là khoảng lùi của mốc after màn hình gửi (comment commit muộn vẫn tới; màn hình bỏ trùng theo mã).
	CommentsSlack = 30 * time.Second
	// pagePrefix đánh dấu nextCursor của trang tiếp theo (vị trí chính xác, không lùi).
	pagePrefix = "p:"
	// privateNone là trạng thái nhắn riêng khi comment chưa có tin riêng.
	privateNone = "none"
)

// ErrCursorInvalid là lỗi: mốc after không đọc được (400).
var ErrCursorInvalid = errors.New("live: mốc after không hợp lệ")

// CommentQuery là bộ lọc danh sách comment: mốc after ("sentAt,id" của comment cuối màn hình đã có — lùi
// CommentsSlack; hay nextCursor của trang trước — đúng vị trí), cỡ trang (0 → mặc định, tối đa CommentsMaxLimit), chỉ
// comment có SĐT, chỉ comment chưa chốt.
type CommentQuery struct {
	After                    string
	Limit                    int
	PhoneOnly, UnhandledOnly bool
}

// CommentRow là một comment của phiên: khách, nội dung, có SĐT, ẩn, ghi nhận (nil: chưa chốt), nhắn riêng (none hay
// send_status của tin riêng mới nhất), khách có nháp của phiên.
type CommentRow struct {
	MessageID, ConversationID, CustomerID uuid.UUID
	CustomerName, Text                    string
	SentAt                                time.Time
	HasPhone, IsHidden                    bool
	Claim                                 *ClaimRef
	PrivateState                          string
	PrivateMessageID                      *uuid.UUID
	CustomerHasOpenDraft                  bool
}

// ListComments trả comment của khách dưới phiên live sessionID theo thứ tự đến (cần conversation.read trên Page; không
// có → ErrNotFound) và nextCursor (rỗng khi hết trang); trong tx tenant của bên gọi.
func ListComments(ctx context.Context, tx pgx.Tx, u identity.AuthUser, sessionID uuid.UUID, cq CommentQuery,
) ([]CommentRow, string, error) {
	q := store.New(tx)
	row, err := q.GetLiveSession(ctx, sessionID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, "", ErrNotFound
	case err != nil:
		return nil, "", fmt.Errorf("đọc phiên live: %w", err)
	}
	if err := access.Require(u, permRead, row.ChannelAccountID); err != nil {
		return nil, "", err //nolint:wrapcheck // sentinel của phần (404)
	}
	at, id, err := parseAfter(cq.After)
	if err != nil {
		return nil, "", err
	}
	lim := cq.Limit
	if lim <= 0 {
		lim = CommentsDefaultLimit
	}
	lim = min(lim, CommentsMaxLimit)
	s := sessionFrom(row)
	rows, err := q.ListSessionComments(ctx, store.ListSessionCommentsParams{
		LiveSessionID: s.ID, ChannelAccountID: s.AccountID, PostRefs: postRefs(s), AfterAt: at, AfterID: id,
		PhoneOnly: cq.PhoneOnly, UnhandledOnly: cq.UnhandledOnly, Lim: int32(lim),
	})
	if err != nil {
		return nil, "", fmt.Errorf("đọc comment của phiên live: %w", err)
	}
	out := make([]CommentRow, 0, len(rows))
	for _, r := range rows {
		c := commentRow(r)
		if cq.PhoneOnly && !c.HasPhone {
			continue // câu SQL chỉ lọc thô (có chữ số)
		}
		out = append(out, c)
	}
	next := ""
	if len(rows) == lim {
		last := rows[len(rows)-1]
		next = pagePrefix + last.SentAt.UTC().Format(time.RFC3339Nano) + "," + last.ID.String()
	}
	return out, next, nil
}

func commentRow(r store.ListSessionCommentsRow) CommentRow {
	c := CommentRow{
		MessageID: r.ID, ConversationID: r.ConversationID, CustomerID: r.CustomerID, CustomerName: r.CustomerName,
		Text: r.Body, SentAt: r.SentAt.UTC(), HasPhone: automation.ContainsPhone(r.Body), IsHidden: r.IsHidden,
		PrivateState: privateNone, CustomerHasOpenDraft: r.CustomerHasOpenDraft,
	}
	if r.ClaimOrderID.Valid {
		c.Claim = &ClaimRef{
			By: r.ClaimBy.Bytes, ByName: r.ClaimByName, OrderID: r.ClaimOrderID.Bytes, OrderCode: r.ClaimOrderCode,
		}
	}
	if r.PrivateStatus.Valid {
		id := r.PrivateID
		c.PrivateState, c.PrivateMessageID = r.PrivateStatus.String, &id
	}
	return c
}

// postRefs là các mã bài trên kênh có thể là bài của phiên s (mã bài, mã live, mã video; như MatchLiveComment).
func postRefs(s Session) []string {
	out := []string{s.LiveVideoID}
	for _, v := range []string{s.VideoID, s.PostExternalID} {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// parseAfter đọc mốc after: rỗng → từ đầu; "p:<sentAt>,<id>" → đúng vị trí; "<sentAt>,<id>" → lùi CommentsSlack.
func parseAfter(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Unix(0, 0).UTC(), uuid.Nil, nil
	}
	exact := strings.HasPrefix(s, pagePrefix)
	ts, rawID, ok := strings.Cut(strings.TrimPrefix(s, pagePrefix), ",")
	if !ok {
		return time.Time{}, uuid.Nil, ErrCursorInvalid
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrCursorInvalid
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrCursorInvalid
	}
	if exact {
		return at.UTC(), id, nil
	}
	return at.UTC().Add(-CommentsSlack), uuid.Nil, nil
}
