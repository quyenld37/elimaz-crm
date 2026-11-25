package conversation

import (
	"context"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/calls"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/presence"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/reminders"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/templates"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Deps là phụ thuộc phần chạy nền của conversation (kế hoạch M4, Task 143).
type Deps = deps.Deps

// areas gom Parts của mọi phần (mỗi phần một tệp khung trong internal/<phần>/).
func areas(d Deps) []deps.Parts {
	return []deps.Parts{
		inbound.Parts(d), send.Parts(d), automation.Parts(d), live.Parts(d), assign.Parts(d), templates.Parts(d),
		presence.Parts(d), reminders.Parts(d), calls.Parts(d), history.Parts(d),
	}
}

// collect nối trường pick của mọi phần, bỏ phần tử nil.
func collect[T comparable](d Deps, pick func(deps.Parts) []T) []T {
	var zero T
	var out []T
	for _, p := range areas(d) {
		for _, v := range pick(p) {
			if v != zero {
				out = append(out, v)
			}
		}
	}
	return out
}

// InboundHandlers là handler sự kiện vào của conversation (một handler cho mỗi khóa trong d.Registry, Task 153).
func InboundHandlers(d Deps) []webhooks.InboundHandler {
	return collect(d, func(p deps.Parts) []webhooks.InboundHandler { return p.Inbound })
}

// CommandHandlers là lệnh ra ngoài (command.fb.*) của conversation.
func CommandHandlers(d Deps) []commands.Handler {
	return collect(d, func(p deps.Parts) []commands.Handler { return p.Commands })
}

// JobHandlers là việc nền trong database của conversation.
func JobHandlers(d Deps) []queue.Handler {
	return collect(d, func(p deps.Parts) []queue.Handler { return p.Jobs })
}

// Schedules là lịch định kỳ của conversation.
func Schedules(d Deps) []schedules.Schedule {
	var out []schedules.Schedule
	for _, p := range areas(d) {
		out = append(out, p.Schedules...)
	}
	return out
}

// Background là luồng nền chạy trong worker (luồng đọc comment live, Task 170); mỗi hàm chạy tới khi ctx bị hủy.
func Background(d Deps) []func(context.Context) error {
	var out []func(context.Context) error
	for _, p := range areas(d) {
		for _, f := range p.Background {
			if f != nil {
				out = append(out, f)
			}
		}
	}
	return out
}
