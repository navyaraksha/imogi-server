package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr                string
	HTTPAllowedOrigins      []string
	HTTPShutdownTimeout     time.Duration
	SessionTTL              time.Duration
	SessionCookieSecure     bool
	DatabaseURL             string
	DatabaseMaxConns        int32
	DatabaseMinConns        int32
	GoogleClientID          string
	GoogleHostedDomain      string
	BootstrapAdminEmails    []string
	EncryptionKey           []byte
	LookupKey               []byte
	ObjectStorageDriver     string
	ObjectStorageEndpoint   string
	ObjectStorageRegion     string
	ObjectStorageBucket     string
	ObjectStorageAccessKey  string
	ObjectStorageSecretKey  string
	ObjectStorageSecure     bool
	ObjectStoragePresignTTL time.Duration
	FileStorageRoot         string
	FileMaxUploadBytes      int64
	FileMaxRows             int
	JobWorkerConcurrency    int
	JobPollInterval         time.Duration
	JobLeaseDuration        time.Duration
	JobHeartbeatInterval    time.Duration
	JobMaxAttempts          int
	JobShutdownTimeout      time.Duration
}

func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}
	databaseURL, err := required("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	googleClientID, err := required("GOOGLE_CLIENT_ID")
	if err != nil {
		return Config{}, err
	}
	encryptionKey, err := requiredBase64("SENSITIVE_DATA_ENCRYPTION_KEY_BASE64")
	if err != nil {
		return Config{}, err
	}
	lookupKey, err := requiredBase64("SENSITIVE_DATA_LOOKUP_KEY_BASE64")
	if err != nil {
		return Config{}, err
	}
	if len(lookupKey) == 0 {
		return Config{}, errors.New("SENSITIVE_DATA_LOOKUP_KEY_BASE64 cannot be empty")
	}

	shutdownTimeout, err := durationEnv("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	maxConns, err := int32Env("DB_MAX_CONNS", 10)
	if err != nil {
		return Config{}, err
	}
	minConns, err := int32Env("DB_MIN_CONNS", 1)
	if err != nil {
		return Config{}, err
	}
	if minConns < 0 || maxConns < 1 || minConns > maxConns {
		return Config{}, errors.New("DB_MIN_CONNS and DB_MAX_CONNS must be valid and min <= max")
	}
	if len(encryptionKey) != 16 && len(encryptionKey) != 24 && len(encryptionKey) != 32 {
		return Config{}, errors.New("SENSITIVE_DATA_ENCRYPTION_KEY_BASE64 must decode to 16, 24, or 32 bytes")
	}
	sessionTTL, err := durationEnv("SESSION_TTL", 8*time.Hour)
	if err != nil {
		return Config{}, err
	}
	sessionCookieSecure, err := boolEnv("SESSION_COOKIE_SECURE", false)
	if err != nil {
		return Config{}, err
	}
	objectStorageEndpoint := firstNonBlankEnv("OBJECT_STORAGE_ENDPOINT", "AWS_ENDPOINT_URL_S3")
	objectStorageSecureDefault := strings.HasPrefix(strings.ToLower(objectStorageEndpoint), "https://")
	objectStorageSecure, err := boolEnv("OBJECT_STORAGE_SECURE", objectStorageSecureDefault)
	if err != nil {
		return Config{}, err
	}
	objectStoragePresignTTL, err := durationEnv("OBJECT_STORAGE_PRESIGN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	fileMaxUploadBytes, err := int64Env("FILE_MAX_UPLOAD_BYTES", 100*1024*1024)
	if err != nil || fileMaxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("FILE_MAX_UPLOAD_BYTES must be positive")
	}
	fileMaxRows, err := intEnv("FILE_MAX_ROWS", 500000)
	if err != nil || fileMaxRows <= 0 {
		return Config{}, fmt.Errorf("FILE_MAX_ROWS must be positive")
	}
	workerConcurrency, err := intEnv("JOB_WORKER_CONCURRENCY", 2)
	if err != nil || workerConcurrency <= 0 {
		return Config{}, fmt.Errorf("JOB_WORKER_CONCURRENCY must be positive")
	}
	jobPollInterval, err := durationEnv("JOB_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	jobLeaseDuration, err := durationEnv("JOB_LEASE_DURATION", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	jobHeartbeatInterval, err := durationEnv("JOB_HEARTBEAT_INTERVAL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	jobMaxAttempts, err := intEnv("JOB_MAX_ATTEMPTS", 5)
	if err != nil || jobMaxAttempts < 1 || jobMaxAttempts > 20 {
		return Config{}, fmt.Errorf("JOB_MAX_ATTEMPTS must be between 1 and 20")
	}
	jobShutdownTimeout, err := durationEnv("JOB_SHUTDOWN_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	objectStorageDriver := strings.ToLower(strings.TrimSpace(os.Getenv("OBJECT_STORAGE_DRIVER")))
	if objectStorageDriver == "" {
		if objectStorageEndpoint != "" && firstNonBlankEnv("OBJECT_STORAGE_ACCESS_KEY", "AWS_ACCESS_KEY_ID") != "" && firstNonBlankEnv("OBJECT_STORAGE_SECRET_KEY", "AWS_SECRET_ACCESS_KEY") != "" {
			objectStorageDriver = "s3"
		} else {
			objectStorageDriver = "filesystem"
		}
	}
	objectStorageRegion := firstNonBlankEnv("OBJECT_STORAGE_REGION", "AWS_REGION", "AWS_DEFAULT_REGION")
	if objectStorageRegion == "" {
		objectStorageRegion = "us-east-1"
	}

	return Config{
		HTTPAddr:                envOrDefault("HTTP_ADDR", ":8080"),
		HTTPAllowedOrigins:      csvEnv("HTTP_ALLOWED_ORIGINS"),
		HTTPShutdownTimeout:     shutdownTimeout,
		SessionTTL:              sessionTTL,
		SessionCookieSecure:     sessionCookieSecure,
		DatabaseURL:             databaseURL,
		DatabaseMaxConns:        maxConns,
		DatabaseMinConns:        minConns,
		GoogleClientID:          googleClientID,
		GoogleHostedDomain:      strings.ToLower(strings.TrimSpace(os.Getenv("GOOGLE_HOSTED_DOMAIN"))),
		BootstrapAdminEmails:    csvEnv("GOOGLE_BOOTSTRAP_PLATFORM_ADMIN_EMAILS"),
		EncryptionKey:           encryptionKey,
		LookupKey:               lookupKey,
		ObjectStorageDriver:     objectStorageDriver,
		ObjectStorageEndpoint:   objectStorageEndpoint,
		ObjectStorageRegion:     objectStorageRegion,
		ObjectStorageBucket:     firstNonBlankEnv("OBJECT_STORAGE_BUCKET", "AWS_S3_BUCKET", "S3_BUCKET"),
		ObjectStorageAccessKey:  firstNonBlankEnv("OBJECT_STORAGE_ACCESS_KEY", "AWS_ACCESS_KEY_ID"),
		ObjectStorageSecretKey:  firstNonBlankEnv("OBJECT_STORAGE_SECRET_KEY", "AWS_SECRET_ACCESS_KEY"),
		ObjectStorageSecure:     objectStorageSecure,
		ObjectStoragePresignTTL: objectStoragePresignTTL,
		FileStorageRoot:         envOrDefault("FILE_STORAGE_ROOT", ".data/objects"),
		FileMaxUploadBytes:      fileMaxUploadBytes,
		FileMaxRows:             fileMaxRows,
		JobWorkerConcurrency:    workerConcurrency,
		JobPollInterval:         jobPollInterval,
		JobLeaseDuration:        jobLeaseDuration,
		JobHeartbeatInterval:    jobHeartbeatInterval,
		JobMaxAttempts:          jobMaxAttempts,
		JobShutdownTimeout:      jobShutdownTimeout,
	}, nil
}

func boolEnv(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return parsed, nil
}

// loadDotEnv provides a local-development convenience without overriding
// environment variables supplied by the process manager or deployment.
func loadDotEnv(path string) error {
	if err := godotenv.Load(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load %s: %w", path, err)
	}
	return nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func requiredBase64(name string) ([]byte, error) {
	value, err := required(name)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be valid base64: %w", name, err)
	}
	return decoded, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func firstNonBlankEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}

func int32Env(name string, fallback int32) (int32, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return int32(parsed), nil
}

func intEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func int64Env(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func csvEnv(name string) []string {
	parts := strings.Split(os.Getenv(name), ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(strings.ToLower(part))
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}
