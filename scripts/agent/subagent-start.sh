#!/usr/bin/env bash
# Hook SubagentStart (general-purpose): nhắc agent chung rằng nó không được sửa mã CRM.
cat <<'JSON'
{"hookSpecificOutput":{"hookEventName":"SubagentStart","additionalContext":"[CRM ELIMAZ] Bạn là agent chung trong apps/crm. Bạn không được sửa mã (internal/, cmd/, tools/, tests/, console/src/, migrations/, queries/, openapi/): việc đó chỉ do agent crm-implementer làm, hook sẽ chặn. Bạn có thể đọc, tìm, nghiên cứu, viết tài liệu khi được giao. Quy trình: docs/quy-trinh-lam-viec.md."}}
JSON
