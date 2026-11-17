// Tệp mẫu: SQL dựng lúc chạy của identity nhắc schema platform (phải bị báo).
package identity

const q = "SELECT key FROM platform.idempotency_keys WHERE key = $1"
