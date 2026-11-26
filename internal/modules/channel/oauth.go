package channel

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/datadeletion"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Đăng nhập Facebook for Business mở trong cùng tab (kế hoạch M4, Task 149; 02 mục 9). Hai loại dòng trong
// channel.oauth_sessions:
//   - dòng start (StartOAuth): băm SHA-256 của state 32 byte ngẫu nhiên, gắn người dùng, hạn 15 phút, chưa có token;
//   - dòng phiên Page (SaveOAuthPages, sau khi đổi mã): token người dùng mã hóa (AAD theo dòng), danh sách Page, hạn
//     30 phút; id của nó là sessionId mà màn hình gửi khi chọn Page để nối.
//
// Đổi mã đánh dấu dòng start đã dùng bằng một câu có điều kiện (chỉ một lần) rồi chèn dòng phiên Page (migration 0401
// không cấp quyền sửa cột token, hạn của dòng có sẵn; tệp task 149, Quyết định 1).

const (
	oauthStartTTL = 15 * time.Minute
	oauthPagesTTL = 30 * time.Minute
	stateBytes    = 32
	// aadSessionColumn là cột của token người dùng (AAD gắn bản mã với đúng dòng phiên).
	aadSessionTable  = "channel.oauth_sessions"
	aadSessionColumn = "user_token_enc"
)

var (
	// ErrOAuthStateInvalid báo state, phiên đăng nhập sai, của người khác, hết hạn hay đã dùng; hoặc mã đăng nhập Meta
	// từ chối (người dùng bắt đầu đăng nhập lại).
	ErrOAuthStateInvalid = errors.New("channel: phiên đăng nhập Facebook không hợp lệ, hết hạn hoặc đã dùng")
	// ErrNoConnector báo tiến trình chưa bật kết nối kênh (chưa có adapter có Connector, cấu hình Meta hay khóa mã hóa).
	ErrNoConnector = errors.New("channel: chưa bật kết nối kênh")
)

// oauthPage là một Page lưu trong phiên đăng nhập (không token).
type oauthPage struct {
	PageID      string `json:"pageId"`
	Name        string `json:"name"`
	CanMessage  bool   `json:"canMessage"`
	CanModerate bool   `json:"canModerate"`
}

func stateHash(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}

// StartOAuth tạo dòng start cho người dùng userID (hạn now + 15 phút) và trả state (32 byte ngẫu nhiên, base64url 43 ký
// tự; bảng chỉ lưu băm). Dọn luôn mọi phiên đã hết hạn của shop.
func StartOAuth(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) (string, error) {
	raw := make([]byte, stateBytes)
	_, _ = rand.Read(raw) // crypto/rand.Read không bao giờ lỗi (Go ≥ 1.24)
	state := base64.RawURLEncoding.EncodeToString(raw)
	now = now.UTC()
	q := store.New(tx)
	if _, err := q.DeleteExpiredOAuthSessions(ctx, now); err != nil {
		return "", fmt.Errorf("dọn phiên đăng nhập hết hạn: %w", err)
	}
	if err := insertSession(ctx, q, uuid.Must(uuid.NewV7()), userID, stateHash(state), nil, "", []oauthPage{},
		now.Add(oauthStartTTL), ""); err != nil {
		return "", err
	}
	return state, nil
}

// insertSession chèn một dòng phiên (tenant của ngữ cảnh tx); userRefHash rỗng (dòng start) → NULL.
func insertSession(ctx context.Context, q *store.Queries, id, userID uuid.UUID, hash, tokenEnc []byte, kid string,
	pages []oauthPage, expires time.Time, userRefHash string,
) error {
	pj, err := json.Marshal(pages)
	if err != nil {
		return fmt.Errorf("mã hóa danh sách Page: %w", err)
	}
	if err := q.InsertOAuthSession(ctx, store.InsertOAuthSessionParams{
		ID: id, UserID: userID, StateHash: hash, UserTokenEnc: tokenEnc,
		UserTokenKid: pgtype.Text{String: kid, Valid: kid != ""}, Pages: pj, ExpiresAt: expires,
		FbUserRefHash: pgtype.Text{String: userRefHash, Valid: userRefHash != ""},
	}); err != nil {
		return fmt.Errorf("ghi phiên đăng nhập Facebook: %w", err)
	}
	return nil
}

// AuthorizeURL dựng địa chỉ hộp thoại đăng nhập Facebook for Business của cấu hình m với state; thiếu mã ứng dụng, mã
// cấu hình đăng nhập hay địa chỉ quay về → ErrNoConnector.
func AuthorizeURL(m config.MetaConfig, state string) (string, error) {
	if m.AppID == "" || m.LoginConfigID == "" || m.OAuthRedirectURL == "" || m.GraphVersion == "" {
		return "", ErrNoConnector
	}
	q := url.Values{}
	q.Set("client_id", m.AppID)
	q.Set("config_id", m.LoginConfigID)
	q.Set("redirect_uri", m.OAuthRedirectURL)
	q.Set("response_type", "code")
	q.Set("state", state)
	u := url.URL{Scheme: "https", Host: "www.facebook.com", Path: "/" + m.GraphVersion + "/dialog/oauth", RawQuery: q.Encode()}
	return u.String(), nil
}

// CheckOAuthState trả id dòng start của state khi dòng thuộc userID, chưa dùng, còn hạn lúc now; không thì
// ErrOAuthStateInvalid. Chỉ đọc: SaveOAuthPages đánh dấu đã dùng (chỉ một lần thắng).
func CheckOAuthState(ctx context.Context, tx pgx.Tx, userID uuid.UUID, state string, now time.Time) (uuid.UUID, error) {
	if state == "" {
		return uuid.Nil, ErrOAuthStateInvalid
	}
	r, err := store.New(tx).GetOAuthStart(ctx, stateHash(state))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, ErrOAuthStateInvalid
	case err != nil:
		return uuid.Nil, fmt.Errorf("đọc phiên đăng nhập Facebook: %w", err)
	case r.UserID != userID || r.UsedAt != nil || !now.Before(r.ExpiresAt):
		return uuid.Nil, ErrOAuthStateInvalid
	}
	return r.ID, nil
}

// SaveOAuthPages đánh dấu dòng start startID đã dùng (đúng userID, còn hạn lúc now, chưa dùng; không thì
// ErrOAuthStateInvalid) rồi lưu phiên Page của login: token người dùng mã hóa bằng kr, danh sách Page, băm mã người
// dùng app (datadeletion.UserRefHash, để xóa theo yêu cầu của Meta), hạn now + 30 phút. Trả id phiên Page (sessionId).
func SaveOAuthPages(ctx context.Context, tx pgx.Tx, kr *fieldcrypt.Keyring, startID, userID uuid.UUID, login OAuthLogin,
	now time.Time,
) (uuid.UUID, error) {
	switch {
	case kr == nil:
		return uuid.Nil, ErrNoKeyring
	case login.Token == "" || login.AppUserID == "":
		return uuid.Nil, ErrInvalidCredentials
	}
	now = now.UTC()
	q := store.New(tx)
	tenantID, err := q.UseOAuthStart(ctx, store.UseOAuthStartParams{Now: &now, ID: startID, UserID: userID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, ErrOAuthStateInvalid
	case err != nil:
		return uuid.Nil, fmt.Errorf("đánh dấu phiên đăng nhập đã dùng: %w", err)
	}
	id := uuid.Must(uuid.NewV7())
	plain := []byte(login.Token)
	ct, kid, err := kr.Seal(plain, sessionAAD(tenantID, id))
	clear(plain)
	if err != nil {
		return uuid.Nil, fmt.Errorf("mã hóa token người dùng: %w", err)
	}
	ps := make([]oauthPage, 0, len(login.Pages))
	for _, p := range login.Pages {
		ps = append(ps, oauthPage(p))
	}
	// state_hash của dòng phiên Page: băm của byte ngẫu nhiên không lộ ra ngoài (cột bắt buộc, duy nhất).
	if err := insertSession(ctx, q, id, userID, stateHash(rand.Text()), ct, kid, ps, now.Add(oauthPagesTTL),
		datadeletion.UserRefHash(login.AppUserID)); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// OAuthUserToken giải mã token người dùng của phiên Page sessionID để lấy token Page pageID: phiên phải thuộc userID,
// còn hạn lúc now (không thì ErrOAuthStateInvalid), và có pageID trong danh sách Page (không thì ErrPageNotAccessible).
func OAuthUserToken(ctx context.Context, tx pgx.Tx, kr *fieldcrypt.Keyring, sessionID, userID uuid.UUID, pageID string,
	now time.Time,
) (logging.Secret, error) {
	if kr == nil {
		return "", ErrNoKeyring
	}
	r, err := store.New(tx).GetOAuthPages(ctx, sessionID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrOAuthStateInvalid
	case err != nil:
		return "", fmt.Errorf("đọc phiên Page: %w", err)
	case r.UserID != userID || !now.Before(r.ExpiresAt):
		return "", ErrOAuthStateInvalid
	}
	var pages []oauthPage
	if err := json.Unmarshal(r.Pages, &pages); err != nil {
		return "", fmt.Errorf("đọc danh sách Page của phiên: %w", err)
	}
	if !slices.ContainsFunc(pages, func(p oauthPage) bool { return p.PageID == pageID }) {
		return "", ErrPageNotAccessible
	}
	plain, err := kr.Open(r.UserTokenEnc, r.UserTokenKid.String, sessionAAD(r.TenantID, sessionID))
	if err != nil {
		return "", fmt.Errorf("giải mã token người dùng: %w", err)
	}
	tok := logging.Secret(plain)
	clear(plain)
	return tok, nil
}

func sessionAAD(tenantID, id uuid.UUID) fieldcrypt.AAD {
	return fieldcrypt.AAD{TenantID: tenantID, Table: aadSessionTable, Column: aadSessionColumn, RowID: id}
}
