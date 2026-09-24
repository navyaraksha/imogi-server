package job

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/navyaraksha/imogi/internal/domain/organization"
)

type Status string

const (
	StatusQueued          Status = "queued"
	StatusRunning         Status = "running"
	StatusRetryScheduled  Status = "retry_scheduled"
	StatusSucceeded       Status = "succeeded"
	StatusFailed          Status = "failed"
	StatusDeadLetter      Status = "dead_letter"
	StatusCancelRequested Status = "cancel_requested"
	StatusCanceled        Status = "canceled"
)

type Job struct {
	ID             ID
	TenantID       *organization.TenantID
	CompanyID      *organization.CompanyID
	JobType        string
	QueueName      string
	Status         Status
	Priority       int
	Payload        json.RawMessage
	IdempotencyKey *string
	AvailableAt    time.Time
	AttemptCount   int
	MaxAttempts    int
	LeaseOwner     *string
	LeaseUntil     *time.Time
	HeartbeatAt    *time.Time
	LastErrorCode  *string
	LastError      *string
	CreatedAt      time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	UpdatedAt      time.Time
}

func New(
	id ID,
	tenantID *organization.TenantID,
	companyID *organization.CompanyID,
	jobType string,
	queueName string,
	payload json.RawMessage,
	idempotencyKey *string,
	now time.Time,
) (Job, error) {
	if id.UUID() == uuid.Nil || strings.TrimSpace(jobType) == "" || strings.TrimSpace(queueName) == "" {
		return Job{}, fmt.Errorf("%w: id, job type, and queue are required", ErrInvalidJob)
	}
	if (tenantID == nil) != (companyID == nil) {
		return Job{}, fmt.Errorf("%w: tenant and company scope must be provided together", ErrInvalidJob)
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if !json.Valid(payload) || string(payload) == "null" {
		return Job{}, ErrInvalidJobPayload
	}
	now = now.UTC()
	return Job{
		ID:             id,
		TenantID:       tenantID,
		CompanyID:      companyID,
		JobType:        strings.TrimSpace(jobType),
		QueueName:      strings.TrimSpace(queueName),
		Status:         StatusQueued,
		Priority:       100,
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
		AvailableAt:    now,
		MaxAttempts:    5,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (job Job) IsTerminal() bool {
	// Terminal jobs must not be leased or retried again by a worker.
	switch job.Status {
	case StatusSucceeded, StatusFailed, StatusDeadLetter, StatusCanceled:
		return true
	default:
		return false
	}
}
