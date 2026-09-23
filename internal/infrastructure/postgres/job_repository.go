package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	appjob "github.com/navyaraksha/imogi/internal/application/job"
	domain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres/sqlc"
)

type JobRepository struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

var _ appjob.Repository = (*JobRepository)(nil)

func NewJobRepository(pool *pgxpool.Pool) (*JobRepository, error) {
	if pool == nil {
		return nil, errors.New("postgres pool is required")
	}
	return &JobRepository{pool: pool, queries: sqlc.New(pool)}, nil
}

func (repository *JobRepository) Create(ctx context.Context, entity domain.Job) (domain.Job, error) {
	row, err := repository.queries.CreateBackgroundJob(ctx, sqlc.CreateBackgroundJobParams{
		ID:             entity.ID.UUID(),
		TenantID:       optionalTenantUUID(entity.TenantID),
		CompanyID:      optionalCompanyUUID(entity.CompanyID),
		JobType:        entity.JobType,
		QueueName:      entity.QueueName,
		Payload:        []byte(entity.Payload),
		IdempotencyKey: entity.IdempotencyKey,
		AvailableAt:    toPGTimestamp(entity.AvailableAt),
		MaxAttempts:    int32(entity.MaxAttempts),
	})
	if err != nil {
		return domain.Job{}, err
	}
	return mapBackgroundJob(row), nil
}

func (repository *JobRepository) Get(ctx context.Context, id domain.ID) (domain.Job, error) {
	row, err := repository.queries.GetBackgroundJob(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrJobNotFound
	}
	if err != nil {
		return domain.Job{}, err
	}
	return mapBackgroundJob(row), nil
}

func (repository *JobRepository) Claim(ctx context.Context, queueName, workerID string, leaseDuration time.Duration) (domain.Job, error) {
	row, err := repository.queries.ClaimBackgroundJob(ctx, sqlc.ClaimBackgroundJobParams{
		WorkerID:      &workerID,
		LeaseDuration: pgtype.Interval{Microseconds: leaseDuration.Microseconds(), Valid: true},
		QueueName:     queueName,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, appjob.ErrNoJobAvailable
	}
	if err != nil {
		return domain.Job{}, err
	}
	return mapBackgroundJob(row), nil
}

func (repository *JobRepository) Heartbeat(ctx context.Context, id domain.ID, workerID string, leaseDuration time.Duration) error {
	return repository.queries.HeartbeatBackgroundJob(ctx, sqlc.HeartbeatBackgroundJobParams{
		LeaseDuration: pgtype.Interval{Microseconds: leaseDuration.Microseconds(), Valid: true},
		ID:            id.UUID(),
		WorkerID:      &workerID,
	})
}

func (repository *JobRepository) MarkSucceeded(ctx context.Context, id domain.ID, workerID string) (domain.Job, error) {
	row, err := repository.queries.MarkBackgroundJobSucceeded(ctx, sqlc.MarkBackgroundJobSucceededParams{ID: id.UUID(), WorkerID: &workerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrJobNotLeased
	}
	if err != nil {
		return domain.Job{}, err
	}
	return mapBackgroundJob(row), nil
}

func (repository *JobRepository) MarkRetry(ctx context.Context, id domain.ID, workerID string, availableAt time.Time, code, message string) (domain.Job, error) {
	errorCode := code
	errorMessage := message
	row, err := repository.queries.MarkBackgroundJobRetry(ctx, sqlc.MarkBackgroundJobRetryParams{
		AvailableAt:  toPGTimestamp(availableAt),
		ErrorCode:    &errorCode,
		ErrorMessage: &errorMessage,
		ID:           id.UUID(),
		WorkerID:     &workerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrJobNotLeased
	}
	if err != nil {
		return domain.Job{}, err
	}
	return mapBackgroundJob(row), nil
}

func (repository *JobRepository) MarkFailed(ctx context.Context, id domain.ID, workerID, code, message string) (domain.Job, error) {
	errorCode := code
	errorMessage := message
	row, err := repository.queries.MarkBackgroundJobFailed(ctx, sqlc.MarkBackgroundJobFailedParams{
		ErrorCode:    &errorCode,
		ErrorMessage: &errorMessage,
		ID:           id.UUID(),
		WorkerID:     &workerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrJobNotLeased
	}
	if err != nil {
		return domain.Job{}, err
	}
	return mapBackgroundJob(row), nil
}

func (repository *JobRepository) MarkCanceled(ctx context.Context, id domain.ID, workerID string) (domain.Job, error) {
	row, err := repository.queries.MarkBackgroundJobCanceled(ctx, sqlc.MarkBackgroundJobCanceledParams{ID: id.UUID(), WorkerID: &workerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrJobNotLeased
	}
	if err != nil {
		return domain.Job{}, err
	}
	return mapBackgroundJob(row), nil
}

func (repository *JobRepository) RequestCancellation(ctx context.Context, id domain.ID) (domain.Job, error) {
	row, err := repository.queries.RequestBackgroundJobCancellation(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrJobNotFound
	}
	if err != nil {
		return domain.Job{}, err
	}
	return mapBackgroundJob(row), nil
}

func (repository *JobRepository) CreateAttempt(ctx context.Context, attemptID domain.AttemptID, jobID domain.ID, attemptNumber int, workerID string) error {
	_, err := repository.queries.CreateBackgroundJobAttempt(ctx, sqlc.CreateBackgroundJobAttemptParams{
		ID:            attemptID.UUID(),
		JobID:         jobID.UUID(),
		AttemptNumber: int32(attemptNumber),
		WorkerID:      workerID,
	})
	return err
}

func (repository *JobRepository) FinishAttempt(ctx context.Context, jobID domain.ID, attemptNumber int, outcome string, errorCode, errorMessage *string) error {
	return repository.queries.FinishBackgroundJobAttempt(ctx, sqlc.FinishBackgroundJobAttemptParams{
		Outcome:       &outcome,
		ErrorCode:     errorCode,
		ErrorMessage:  errorMessage,
		JobID:         jobID.UUID(),
		AttemptNumber: int32(attemptNumber),
	})
}

func optionalTenantUUID(value *organization.TenantID) *uuid.UUID {
	if value == nil {
		return nil
	}
	id := value.UUID()
	return &id
}

func optionalCompanyUUID(value *organization.CompanyID) *uuid.UUID {
	if value == nil {
		return nil
	}
	id := value.UUID()
	return &id
}

func mapBackgroundJob(row sqlc.PlatformBackgroundJob) domain.Job {
	var payload json.RawMessage
	if len(row.Payload) > 0 {
		payload = append(json.RawMessage(nil), row.Payload...)
	}
	return domain.Job{
		ID:             domain.ID(row.ID),
		TenantID:       mapTenantID(row.TenantID),
		CompanyID:      mapCompanyID(row.CompanyID),
		JobType:        row.JobType,
		QueueName:      row.QueueName,
		Status:         domain.Status(row.Status),
		Priority:       int(row.Priority),
		Payload:        payload,
		IdempotencyKey: row.IdempotencyKey,
		AvailableAt:    timestamp(row.AvailableAt),
		AttemptCount:   int(row.AttemptCount),
		MaxAttempts:    int(row.MaxAttempts),
		LeaseOwner:     row.LeaseOwner,
		LeaseUntil:     nullableTimestamp(row.LeaseUntil),
		HeartbeatAt:    nullableTimestamp(row.HeartbeatAt),
		LastErrorCode:  row.LastErrorCode,
		LastError:      row.LastErrorMessage,
		CreatedAt:      timestamp(row.CreatedAt),
		StartedAt:      nullableTimestamp(row.StartedAt),
		CompletedAt:    nullableTimestamp(row.CompletedAt),
		UpdatedAt:      timestamp(row.UpdatedAt),
	}
}

func mapTenantID(value *uuid.UUID) *organization.TenantID {
	if value == nil {
		return nil
	}
	id := organization.TenantID(*value)
	return &id
}

func mapCompanyID(value *uuid.UUID) *organization.CompanyID {
	if value == nil {
		return nil
	}
	id := organization.CompanyID(*value)
	return &id
}
