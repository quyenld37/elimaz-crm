package testkit

import "github.com/google/uuid"

// StubCodeSuffix lộ stubCodeSuffix cho test của gói testkit_test.
func StubCodeSuffix(id uuid.UUID, attempt int) string { return stubCodeSuffix(id, attempt) }
