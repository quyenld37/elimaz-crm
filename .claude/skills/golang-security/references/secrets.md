# Secrets Management Security Rules

Hardcoded secrets, credentials, and sensitive data in source code is a major security vulnerability.

**Rules:**

1. Secrets MUST be loaded from environment variables or secret managers (dự án: biến môi trường do Coolify cấp; đọc một lần trong `main`, truyền vào bằng struct cấu hình, không biến toàn cục).
2. NEVER commit secrets to VCS.
3. `.gitignore` MUST exclude secret files (`.env`, `*.key`, `*.pem`).

---

## Table of Contents

- [Hardcoded Secrets and Credentials — Critical](#hardcoded-secrets-and-credentials--critical)
- [Hardcoded Database Passwords — Critical](#hardcoded-database-passwords--critical)
- [Secrets Storage Best Practices](#secrets-storage-best-practices)
  - [Environment Variables](#environment-variables)
  - [Secret Managers](#secret-managers)
  - [.gitignore Patterns](#gitignore-patterns)
- [Secret Detection Patterns](#secret-detection-patterns)
- [CWE References](#cwe-references)

## Hardcoded Secrets and Credentials — Critical

**Bad:**

```go
const (
    AWS_ACCESS_KEY    = "AKIAIOSFODNN7EXAMPLE"  // DON'T
    AWS_SECRET_KEY    = "wJalrXUtnFEMI/K7MDENG"  // DON'T
    DATABASE_PASSWORD = "SuperSecret123!"         // DON'T
    JWT_SECRET        = "my-super-secret-jwt-key" // DON'T
)

var config = Config{
    APIKey:  "abc123-xyz789-secret-key", // DON'T
    Secret:  "my-super-secret-value",    // DON'T
    DatabaseURL: "user:passw0rd!@localhost:5432/db", // DON'T
}
```

**Good:**

```go
import "os"

type Config struct {
    AWSAccessKey     string
    AWSSecretKey     string
    DatabasePassword string
    JWTSecret        string
}

func LoadConfig() (*Config, error) {
    cfg := &Config{
        AWSAccessKey:     os.Getenv("AWS_ACCESS_KEY_ID"),
        AWSSecretKey:     os.Getenv("AWS_SECRET_ACCESS_KEY"),
        DatabasePassword: os.Getenv("DATABASE_PASSWORD"),
        JWTSecret:        os.Getenv("JWT_SECRET"),
    }

    if cfg.JWTSecret == "" {
        return nil, errors.New("JWT_SECRET is required")
    }
    return cfg, nil
}
```

---

## Hardcoded Database Passwords — Critical

**Bad:**

```go
// MySQL
dsn := "user:Password123!@tcp(localhost:3306)/dbname" // DON'T

// PostgreSQL
dsn := "user=postgres password=P@ssw0rd! dbname=mydb host=localhost" // DON'T
```

**Good:**

```go
// PostgreSQL with pgx (dự án)
func connectPostgres(ctx context.Context) (*pgxpool.Pool, error) {
    connStr := os.Getenv("DATABASE_URL")
    if connStr == "" {
        return nil, errors.New("DATABASE_URL required")
    }
    return pgxpool.New(ctx, connStr)
}
```

---

## Secrets Storage Best Practices

### Environment Variables

```go
type EnvSecretLoader struct{}

func (l *EnvSecretLoader) Load(required []string) (map[string]string, error) {
    secrets := make(map[string]string)
    missing := []string{}
    for _, name := range required {
        value := os.Getenv(name)
        if value == "" {
            missing = append(missing, name)
            continue
        }
        secrets[name] = value
    }
    if len(missing) > 0 {
        return nil, fmt.Errorf("missing: %v", missing)
    }
    return secrets, nil
}
```

### Secret Managers

Dự án hiện không dùng dịch vụ quản lý bí mật riêng (AWS Secrets Manager không có trong ADR-44); ví dụ dưới chỉ minh họa cách đặt interface ở phía dùng.

```go
type SecretManager interface {
    GetSecret(ctx context.Context, name string) (string, error)
}

// AWS Secrets Manager
type AWSSecretsManager struct {
    client *secretsmanager.Client
}

func (m *AWSSecretsManager) GetSecret(ctx context.Context, name string) (string, error) {
    result, err := m.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
        SecretId: aws.String(name),
    })
    if err != nil {
        return "", err
    }
    if result.SecretString != nil {
        return *result.SecretString, nil
    }
    return string(result.SecretBinary), nil
}
```

### .gitignore Patterns

```
# Secrets
.env
.env.local
.env.*.local
*.key
*.pem
*.p12
*.pfx
secrets/
credentials/
```

---

## Secret Detection Patterns

| Pattern         | Example                         |
| --------------- | ------------------------------- |
| API Keys        | `Key = "sk_live_..."`           |
| Passwords       | `password = "..."`              |
| Tokens          | `token = "..."`                 |
| Private Keys    | `BEGIN PRIVATE KEY`             |
| AWS Credentials | `AWS_ACCESS_KEY_ID = "AKIA..."` |
| JWT Secrets     | `jwtSecret = "..."`             |

---

## CWE References

- **CWE-798**: Use of Hard-coded Credentials
- **CWE-312**: Cleartext Storage of Sensitive Information
- **CWE-532**: Insertion of Sensitive Information into Log File
- **CWE-359**: Exposure of Private Personal Information
