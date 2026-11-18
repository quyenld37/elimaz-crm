package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// s3Timeout là hạn của một lời gọi kho (Head, Put, Copy, Delete; Open chỉ tính tới lúc có câu trả lời đầu).
const s3Timeout = 60 * time.Second

// S3Config là cấu hình nối kho S3 (danh tính crm-app). Endpoint cũng là gốc của đường dẫn ký sẵn mà trình duyệt gọi.
type S3Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	// BucketPrefix + Bucket là tên bucket thật (mặc định crm-).
	BucketPrefix string
}

// LogValue che khóa bí mật.
func (c S3Config) LogValue() slog.Value {
	return slog.GroupValue(slog.String("endpoint", c.Endpoint), slog.String("region", c.Region),
		slog.String("bucket_prefix", c.BucketPrefix), slog.Bool("secret_set", c.SecretAccessKey != ""))
}

// S3Store là FileStore trên kho S3 (SeaweedFS ở M1, ADR-36) bằng aws-sdk-go-v2, đường dẫn kiểu path-style.
type S3Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	prefix  string
}

var errS3Config = errors.New("files: cấu hình S3 thiếu hay sai")

// NewS3Store dựng client; không gọi mạng (khởi động không phụ thuộc kho đang sống).
func NewS3Store(cfg S3Config) (*S3Store, error) {
	u, err := url.Parse(cfg.Endpoint)
	switch {
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
		return nil, fmt.Errorf("%w: Endpoint phải là URL http(s)", errS3Config)
	case cfg.Region == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" || cfg.BucketPrefix == "":
		return nil, fmt.Errorf("%w: thiếu Region, AccessKeyID, SecretAccessKey hay BucketPrefix", errS3Config)
	}
	creds := aws.Credentials{AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey, Source: "crm-config"}
	client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: true,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return creds, nil
		}),
		HTTPClient: &http.Client{Timeout: 10 * time.Minute}, // tệp lớn đọc lâu; mỗi lời gọi có ctx riêng
	})
	return &S3Store{client: client, presign: s3.NewPresignClient(client), prefix: cfg.BucketPrefix}, nil
}

func (s *S3Store) bucket(b Bucket) (*string, error) {
	if !b.valid() {
		return nil, fmt.Errorf("%w: bucket %q lạ", errS3Config, b)
	}
	return aws.String(s.prefix + string(b)), nil
}

// apiCode trả mã lỗi S3 (NotFound, NoSuchKey, PreconditionFailed…) và mã HTTP nếu có.
func apiCode(err error) (string, int) {
	code, status := "", 0
	if ae, ok := errors.AsType[smithy.APIError](err); ok {
		code = ae.ErrorCode()
	}
	var re interface{ HTTPStatusCode() int }
	if errors.As(err, &re) {
		status = re.HTTPStatusCode()
	}
	return code, status
}

func isNotFound(err error) bool {
	code, status := apiCode(err)
	return code == "NotFound" || code == "NoSuchKey" || status == http.StatusNotFound
}

func isPrecondition(err error) bool {
	code, status := apiCode(err)
	return code == "PreconditionFailed" || status == http.StatusPreconditionFailed
}

// PresignPut implements FileStore: ký content-length, content-type, If-None-Match: * (không ghi đè).
func (s *S3Store) PresignPut(ctx context.Context, b Bucket, key string, size int64, contentType string, ttl time.Duration) (PresignedPut, error) {
	bk, err := s.bucket(b)
	if err != nil {
		return PresignedPut{}, err
	}
	if size <= 0 || ttl <= 0 {
		return PresignedPut{}, fmt.Errorf("ký PUT %s: kích thước và hạn phải > 0", b)
	}
	return s.presignPut(ctx, b, &s3.PutObjectInput{
		Bucket: bk, Key: aws.String(key), ContentLength: aws.Int64(size), ContentType: aws.String(contentType),
		IfNoneMatch: aws.String("*"),
	}, ttl)
}

// PresignPutAnySize implements FileStore: như PresignPut nhưng không ký content-length.
func (s *S3Store) PresignPutAnySize(ctx context.Context, b Bucket, key string, contentType string, ttl time.Duration) (PresignedPut, error) {
	bk, err := s.bucket(b)
	if err != nil {
		return PresignedPut{}, err
	}
	if ttl <= 0 || contentType == "" {
		return PresignedPut{}, fmt.Errorf("ký PUT %s: hạn phải > 0, phải có loại", b)
	}
	return s.presignPut(ctx, b, &s3.PutObjectInput{
		Bucket: bk, Key: aws.String(key), ContentType: aws.String(contentType), IfNoneMatch: aws.String("*"),
	}, ttl)
}

func (s *S3Store) presignPut(ctx context.Context, b Bucket, in *s3.PutObjectInput, ttl time.Duration) (PresignedPut, error) {
	exp := time.Now().Add(ttl).Truncate(time.Second)
	req, err := s.presign.PresignPutObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return PresignedPut{}, fmt.Errorf("ký PUT %s: %w", b, err)
	}
	headers := map[string]string{}
	for k, vs := range req.SignedHeader {
		// Host và Content-Length do trình duyệt tự đặt (không đặt tay được); vẫn nằm trong chữ ký.
		if len(vs) == 0 || http.CanonicalHeaderKey(k) == "Host" || http.CanonicalHeaderKey(k) == "Content-Length" {
			continue
		}
		headers[http.CanonicalHeaderKey(k)] = vs[0]
	}
	return PresignedPut{URL: req.URL, Headers: headers, ExpiresAt: exp}, nil
}

// PresignGet implements FileStore.
func (s *S3Store) PresignGet(ctx context.Context, b Bucket, key string, ttl time.Duration, attachmentName string) (string, error) {
	bk, err := s.bucket(b)
	if err != nil {
		return "", err
	}
	in := &s3.GetObjectInput{Bucket: bk, Key: aws.String(key)}
	if cd := contentDisposition(attachmentName); cd != "" {
		in.ResponseContentDisposition = aws.String(cd)
	}
	req, err := s.presign.PresignGetObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("ký GET %s: %w", b, err)
	}
	return req.URL, nil
}

// Head implements FileStore.
func (s *S3Store) Head(ctx context.Context, b Bucket, key string) (ObjectInfo, error) {
	bk, err := s.bucket(b)
	if err != nil {
		return ObjectInfo{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s3Timeout)
	defer cancel()
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: bk, Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return ObjectInfo{}, fmt.Errorf("head %s: %w", b, ErrObjectNotFound)
		}
		return ObjectInfo{}, fmt.Errorf("head %s: %w", b, err)
	}
	return ObjectInfo{Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType), ETag: aws.ToString(out.ETag)}, nil
}

// Open implements FileStore.
func (s *S3Store) Open(ctx context.Context, b Bucket, key string, maxBytes int64) (io.ReadCloser, error) {
	bk, err := s.bucket(b)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: bk, Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("mở %s: %w", b, ErrObjectNotFound)
		}
		return nil, fmt.Errorf("mở %s: %w", b, err)
	}
	if n := aws.ToInt64(out.ContentLength); n > maxBytes {
		_ = out.Body.Close()
		return nil, fmt.Errorf("mở %s: %d byte > %d: %w", b, n, maxBytes, ErrTooLarge)
	}
	return &limitedBody{rc: out.Body, left: maxBytes}, nil
}

// limitedBody đọc tối đa left byte; còn dữ liệu sau đó → ErrTooLarge (kho trả khác Content-Length đã khai).
type limitedBody struct {
	rc   io.ReadCloser
	left int64
}

func (l *limitedBody) Read(p []byte) (int, error) {
	if l.left <= 0 {
		var one [1]byte
		if n, _ := l.rc.Read(one[:]); n > 0 {
			return 0, ErrTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.rc.Read(p)
	l.left -= int64(n)
	return n, err //nolint:wrapcheck // io.Reader trả nguyên io.EOF
}

func (l *limitedBody) Close() error { return l.rc.Close() } //nolint:wrapcheck // io.Closer

// CopyIfAbsent implements FileStore: đích đã có → nil. Kiểm Head trước rồi CopyObject có If-None-Match: * (hai lần chép
// cùng lúc: lần sau bị từ chối 412, coi như đã có).
func (s *S3Store) CopyIfAbsent(ctx context.Context, src Bucket, srcKey string, dst Bucket, dstKey string) error {
	sb, err := s.bucket(src)
	if err != nil {
		return err
	}
	db, err := s.bucket(dst)
	if err != nil {
		return err
	}
	if _, err := s.Head(ctx, dst, dstKey); err == nil {
		return nil
	} else if !errors.Is(err, ErrObjectNotFound) {
		return fmt.Errorf("chép %s → %s: %w", src, dst, err)
	}
	ctx, cancel := context.WithTimeout(ctx, s3Timeout)
	defer cancel()
	_, err = s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: db, Key: aws.String(dstKey), CopySource: aws.String(*sb + "/" + url.PathEscape(srcKey)),
		IfNoneMatch: aws.String("*"),
	})
	switch {
	case err == nil, isPrecondition(err):
		return nil
	case isNotFound(err):
		return fmt.Errorf("chép %s → %s: %w", src, dst, ErrObjectNotFound)
	}
	return fmt.Errorf("chép %s → %s: %w", src, dst, err)
}

// Put implements FileStore: If-None-Match: * nên khóa đã có → ErrObjectExists. Nội dung không tua lại được thì đọc vào
// bộ nhớ (tối đa size byte) để SDK tính chữ ký trên nội dung.
func (s *S3Store) Put(ctx context.Context, b Bucket, key string, r io.Reader, size int64, contentType string) error {
	bk, err := s.bucket(b)
	if err != nil {
		return err
	}
	if size <= 0 {
		return fmt.Errorf("ghi %s: kích thước phải > 0", b)
	}
	body, err := exactBody(r, size)
	if err != nil {
		return fmt.Errorf("ghi %s: %w", b, err)
	}
	ctx, cancel := context.WithTimeout(ctx, s3Timeout)
	defer cancel()
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: bk, Key: aws.String(key), Body: body, ContentLength: aws.Int64(size), ContentType: aws.String(contentType),
		IfNoneMatch: aws.String("*"),
	})
	switch {
	case err == nil:
		return nil
	case isPrecondition(err):
		return fmt.Errorf("ghi %s: %w", b, ErrObjectExists)
	}
	return fmt.Errorf("ghi %s: %w", b, err)
}

// exactBody trả nội dung tua lại được, đúng size byte; khác size → lỗi, không gửi gì.
func exactBody(r io.Reader, size int64) (io.ReadSeeker, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		start, err := rs.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, fmt.Errorf("đo nội dung: %w", err)
		}
		end, err := rs.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, fmt.Errorf("đo nội dung: %w", err)
		}
		if _, err := rs.Seek(start, io.SeekStart); err != nil {
			return nil, fmt.Errorf("đo nội dung: %w", err)
		}
		if end-start != size {
			return nil, fmt.Errorf("khai %d byte, có %d", size, end-start)
		}
		return rs, nil
	}
	data, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return nil, fmt.Errorf("đọc nội dung: %w", err)
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("khai %d byte, có %d", size, len(data))
	}
	return bytes.NewReader(data), nil
}

// Delete implements FileStore: không có VersionId nên chỉ tạo dấu xóa ở bucket giữ phiên bản.
func (s *S3Store) Delete(ctx context.Context, b Bucket, key string) error {
	bk, err := s.bucket(b)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s3Timeout)
	defer cancel()
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: bk, Key: aws.String(key)}); err != nil {
		return fmt.Errorf("xóa %s: %w", b, err)
	}
	return nil
}
