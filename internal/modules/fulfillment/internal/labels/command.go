package labels

import "bytes"

// Mã lỗi của lô failed (label_batches.error_code; LabelBatch.errorCode).
const (
	// ErrInvalidPDF: hãng trả thân không bắt đầu bằng %PDF-.
	ErrInvalidPDF = "invalid_pdf"
	// ErrTooLarge: tệp nhãn quá giới hạn (20 MiB) hay rỗng.
	ErrTooLarge = "too_large"
	// ErrCarrierRejected: hãng từ chối yêu cầu in (mã sai, kiện đã hủy ở hãng…).
	ErrCarrierRejected = "carrier_rejected"
	// ErrNotSupported: hãng không in nhãn qua API.
	ErrNotSupported = "not_supported"
	// ErrAccountUnavailable: tài khoản hãng không dùng được (không active, không phải api, khóa không đọc được).
	ErrAccountUnavailable = "account_unavailable"
	// ErrAuth: hãng từ chối khóa API (tài khoản chuyển auth_failed).
	ErrAuth = "auth"
	// ErrTimeout: hết lượt thử hay hết hạn chờ mà chưa tải được nhãn.
	ErrTimeout = "timeout"
)

// pdfMagic là 5 byte đầu của mọi tệp PDF.
const pdfMagic = "%PDF-"

// CheckPDF trả mã lỗi của tệp nhãn b ("" khi dùng được): rỗng hay quá maxBytes → too_large; không bắt đầu bằng %PDF-
// → invalid_pdf.
func CheckPDF(b []byte, maxBytes int) string {
	switch {
	case len(b) == 0 || len(b) > maxBytes:
		return ErrTooLarge
	case !bytes.HasPrefix(b, []byte(pdfMagic)):
		return ErrInvalidPDF
	}
	return ""
}
