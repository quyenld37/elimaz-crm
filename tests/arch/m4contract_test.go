package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// m4Operations là bảng operation của kế hoạch M4, Task 143 (M4-03; hợp đồng viết một lần cho cả mốc): operationId,
// "METHOD đường", quyền. Dòng gộp của bảng kế hoạch (create/update/delete…) tách thành từng operation.
func m4Operations() [][3]string {
	return [][3]string{
		{"listChannelAccounts", "GET /api/v1/channels", "conversation.read"},
		{"setChannelSendMode", "PUT /api/v1/channels/{id}/send-mode", "channel.manage"},
		{"setChannelHumanAgent", "PUT /api/v1/channels/{id}/human-agent", "channel.manage"},
		{"startFacebookOAuth", "POST /api/v1/channels/facebook/oauth/start", "channel.manage"},
		{"exchangeFacebookOAuth", "POST /api/v1/channels/facebook/oauth/exchange", "channel.manage"},
		{"connectFacebookPage", "POST /api/v1/channels/facebook/pages", "channel.manage"},
		{"disconnectChannel", "POST /api/v1/channels/{id}/disconnect", "channel.manage"},
		{"runChannelHealthCheck", "POST /api/v1/channels/{id}/health-check", "channel.manage"},
		{"getPageSettings", "GET /api/v1/channels/{id}/page-settings", "conversation.read"},
		{"updatePageSettings", "PUT /api/v1/channels/{id}/page-settings", "conversation.settings"},
		{"listAssignableUsers", "GET /api/v1/channels/{id}/assignable-users", "conversation.read"},
		{"listAutomationRules", "GET /api/v1/channels/{id}/automation-rules", "conversation.read"},
		{"updateAutomationRule", "PUT /api/v1/channels/{id}/automation-rules/{kind}", "conversation.settings"},
		{"startLiveSessionManually", "POST /api/v1/channels/{id}/live-sessions", "live.operate"},
		{"listConversations", "GET /api/v1/conversations", "conversation.read"},
		{"countConversations", "GET /api/v1/conversations/counts", "conversation.read"},
		{"getConversation", "GET /api/v1/conversations/{id}", "conversation.read"},
		{"getConversationCustomerCard", "GET /api/v1/conversations/{id}/customer-card", "conversation.read"},
		{"getConversationOrderContext", "GET /api/v1/conversations/{id}/order-context", "conversation.read"},
		{"listMessages", "GET /api/v1/conversations/{id}/messages", "conversation.read"},
		{"sendMessage", "POST /api/v1/conversations/{id}/messages", "conversation.reply"},
		{"markConversationRead", "POST /api/v1/conversations/{id}/read", "conversation.read"},
		{"setConversationStatus", "POST /api/v1/conversations/{id}/status", "conversation.reply"},
		{"getAttachmentUrl", "GET /api/v1/conversations/{id}/attachments/{fileId}/url", "conversation.read"},
		{"replyToComment", "POST /api/v1/conversations/{id}/comment-replies", "conversation.reply"},
		{"sendPrivateReply", "POST /api/v1/conversations/{id}/private-replies", "conversation.reply"},
		{"claimConversation", "POST /api/v1/conversations/{id}/claim", "conversation.reply"},
		{"assignConversation", "POST /api/v1/conversations/{id}/assign", "conversation.assign"},
		{"listConversationAssignments", "GET /api/v1/conversations/{id}/assignments", "conversation.read"},
		{"addConversationTag", "PUT /api/v1/conversations/{id}/tags/{tagId}", "conversation.reply"},
		{"removeConversationTag", "DELETE /api/v1/conversations/{id}/tags/{tagId}", "conversation.reply"},
		{"addConversationNote", "POST /api/v1/conversations/{id}/notes", "conversation.reply"},
		{"getPresence", "GET /api/v1/conversations/{id}/presence", "conversation.read"},
		{"setPresence", "POST /api/v1/conversations/{id}/presence", "conversation.read"},
		{"createReminder", "POST /api/v1/conversations/{id}/reminders", "conversation.reply"},
		{"listMessageRevisions", "GET /api/v1/messages/{id}/revisions", "conversation.read"},
		{"resendMessage", "POST /api/v1/messages/{id}/resend", "conversation.reply"},
		{"discardMessage", "POST /api/v1/messages/{id}/discard", "conversation.reply"},
		{"hideComment", "POST /api/v1/messages/{id}/hide", "conversation.reply"},
		{"unhideComment", "POST /api/v1/messages/{id}/unhide", "conversation.reply"},
		{"listConversationTags", "GET /api/v1/conversation-tags", "conversation.read"},
		{"createConversationTag", "POST /api/v1/conversation-tags", "conversation.settings"},
		{"updateConversationTag", "PATCH /api/v1/conversation-tags/{id}", "conversation.settings"},
		{"deleteConversationTag", "DELETE /api/v1/conversation-tags/{id}", "conversation.settings"},
		{"listReplyTemplates", "GET /api/v1/reply-templates", "conversation.read"},
		{"createReplyTemplate", "POST /api/v1/reply-templates", "conversation.settings"},
		{"updateReplyTemplate", "PATCH /api/v1/reply-templates/{id}", "conversation.settings"},
		{"deleteReplyTemplate", "DELETE /api/v1/reply-templates/{id}", "conversation.settings"},
		{"renderReplyTemplate", "POST /api/v1/reply-templates/{id}/render", "conversation.read"},
		{"listFollowUps", "GET /api/v1/follow-ups", "conversation.read"},
		{"listMyReminders", "GET /api/v1/me/reminders", "conversation.read"},
		{"completeReminder", "POST /api/v1/reminders/{id}/done", "conversation.reply"},
		{"cancelReminder", "POST /api/v1/reminders/{id}/cancel", "conversation.reply"},
		{"listCustomerCalls", "GET /api/v1/customers/{id}/calls", "conversation.read"},
		{"createCallLog", "POST /api/v1/customers/{id}/calls", "conversation.reply"},
		{"listCustomerConversations", "GET /api/v1/customers/{id}/conversations", "conversation.read"},
		{"setCustomerDoNotContact", "PUT /api/v1/customers/{id}/do-not-contact", "customer.write"},
		{"listLiveSessions", "GET /api/v1/live-sessions", "conversation.read"},
		{"endLiveSessionManually", "POST /api/v1/live-sessions/{id}/end", "live.operate"},
		{"listLiveComments", "GET /api/v1/live-sessions/{id}/comments", "conversation.read"},
		{"listPendingDrafts", "GET /api/v1/live-sessions/{id}/pending-drafts", "conversation.read"},
		{"remindPendingDrafts", "POST /api/v1/live-sessions/{id}/pending-drafts/remind", "live.operate"},
		{"getLiveCommentContext", "GET /api/v1/live-comments/{id}/context", "conversation.read"},
		{"claimLiveComment", "POST /api/v1/live-comments/{id}/claim", "live.operate"},
		// Thêm sau kế hoạch (Task 501, sửa sau gộp M4 đợt 4): danh sách bài của Page cho màn cài đặt.
		{"listPagePosts", "GET /api/v1/channels/{id}/posts", "conversation.read"},
	}
}

// Mọi operation M4 có trong Routes đúng đường, quyền, cờ ghi; route ghi bắt buộc Idempotency-Key (ADR-41); không
// operationId trùng với mốc trước (checkContractOps); mọi quyền nằm trong danh sách quyền (TestRoutesPublicSetAndPermissions).
func TestM4Contract_Operations(t *testing.T) {
	t.Parallel()
	if n := len(m4Operations()); n != 65 {
		t.Fatalf("bảng M4 có 65 operation (64 của kế hoạch, dòng gộp đã tách, + listPagePosts của Task 501), bảng test có %d", n)
	}
	checkContractOps(t, "M4", m4Operations())
	seen := map[string]bool{}
	for _, w := range m4Operations() {
		if seen[w[1]] {
			t.Errorf("hai operation M4 cùng %s", w[1])
		}
		seen[w[1]] = true
	}
}

// m4PendingList đọc testdata/m4_pending.txt: operationId M4 còn trả 501 (mỗi dòng một mã; # là chú thích).
func m4PendingList(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "m4_pending.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.SplitSeq(string(b), "\n") {
		if op := strings.TrimSpace(line); op != "" && !strings.HasPrefix(op, "#") {
			out = append(out, op)
		}
	}
	return out
}

// Danh sách tạm trả 501 (testdata/m4_pending.txt) chỉ chứa operation M4, không trùng dòng. Task hiện thực operation
// xóa dòng của mình; đối chiếu 501 với danh sách: TestM4Contract_StatusCodes (internal/httpapi).
func TestM4Contract_PendingList(t *testing.T) {
	t.Parallel()
	m4 := map[string]bool{}
	for _, w := range m4Operations() {
		m4[w[0]] = true
	}
	seen := map[string]bool{}
	for _, op := range m4PendingList(t) {
		switch {
		case !m4[op]:
			t.Errorf("m4_pending.txt có %q không phải operation M4", op)
		case seen[op]:
			t.Errorf("m4_pending.txt có %q hai lần", op)
		}
		seen[op] = true
	}
}
