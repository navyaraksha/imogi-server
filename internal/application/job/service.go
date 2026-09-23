package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	domain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/clock"
)

var ErrNoJobAvailable = errors.New("no background job available")

type Service struct {
	repository Repository
	clock      clock.Clock
}

func NewService(repository Repository, systemClock clock.Clock) (*Service, error) {
	if repository == nil || systemClock == nil {
		return nil, errors.New("job service dependencies are required")
	}
	return &Service{repository: repository, clock: systemClock}, nil
}

type CreateInput struct {
	TenantID       *organization.TenantID
	CompanyID      *organization.CompanyID
	JobType        string
	QueueName      string
	Payload        json.RawMessage
	IdempotencyKey *string
}

func (service *Service) Create(ctx context.Context, input CreateInput) (domain.Job, error) {
	id, err := domain.NewID()
	if err != nil {
		return domain.Job{}, fmt.Errorf("generate job id: %w", err)
	}
	entity, err := domain.New(id, input.TenantID, input.CompanyID, input.JobType, input.QueueName, input.Payload, input.IdempotencyKey, service.clock.Now())
	if err != nil {
		return domain.Job{}, err
	}
	return service.repository.Create(ctx, entity)
}

func (service *Service) Get(ctx context.Context, id domain.ID) (domain.Job, error) {
	return service.repository.Get(ctx, id)
}

func (service *Service) Cancel(ctx context.Context, id domain.ID) (domain.Job, error) {
	return service.repository.RequestCancellation(ctx, id)
}

type Worker struct {
	repository    Repository
	registry      HandlerRegistry
	workerID      string
	queueNames    []string
	leaseDuration time.Duration
	pollInterval  time.Duration
	clock         clock.Clock
}

type WorkerConfig struct {
	WorkerID      string
	QueueName     string
	QueueNames    []string
	LeaseDuration time.Duration
	PollInterval  time.Duration
}

func NewWorker(repository Repository, registry HandlerRegistry, systemClock clock.Clock, config WorkerConfig) (*Worker, error) {
	if repository == nil || registry == nil || systemClock == nil {
		return nil, errors.New("worker dependencies are required")
	}
	queueNames := append([]string(nil), config.QueueNames...)
	if len(queueNames) == 0 && config.QueueName != "" {
		queueNames = []string{config.QueueName}
	}
	if config.WorkerID == "" || len(queueNames) == 0 || config.LeaseDuration <= 0 || config.PollInterval <= 0 {
		return nil, errors.New("worker configuration is invalid")
	}
	return &Worker{repository: repository, registry: registry, workerID: config.WorkerID, queueNames: queueNames, leaseDuration: config.LeaseDuration, pollInterval: config.PollInterval, clock: systemClock}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(worker.pollInterval)
	defer ticker.Stop()
	for {
		if err := worker.RunOnce(ctx); err != nil && !errors.Is(err, ErrNoJobAvailable) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (worker *Worker) RunOnce(ctx context.Context) error {
	var entity domain.Job
	var err error
	for _, queueName := range worker.queueNames {
		entity, err = worker.repository.Claim(ctx, queueName, worker.workerID, worker.leaseDuration)
		if !errors.Is(err, ErrNoJobAvailable) {
			break
		}
	}
	if errors.Is(err, ErrNoJobAvailable) {
		return ErrNoJobAvailable
	}
	if err != nil {
		return err
	}

	attemptID, err := domain.NewAttemptID()
	if err != nil {
		return err
	}
	if err := worker.repository.CreateAttempt(ctx, attemptID, entity.ID, entity.AttemptCount, worker.workerID); err != nil {
		return err
	}
	handler, ok := worker.registry.Handler(entity.JobType)
	if !ok {
		processingErr := fmt.Errorf("job handler not registered: %s", entity.JobType)
		_ = worker.repository.FinishAttempt(ctx, entity.ID, entity.AttemptCount, "failed", stringPtr("UNKNOWN_JOB_TYPE"), stringPtr(processingErr.Error()))
		return worker.finishFailure(ctx, entity, "UNKNOWN_JOB_TYPE", processingErr)
	}

	jobContext, cancel := context.WithCancel(ctx)
	defer cancel()
	var cancellationRequested atomic.Bool
	heartbeatDone := make(chan struct{})
	cancellationDone := make(chan struct{})
	go worker.heartbeat(jobContext, entity.ID, heartbeatDone, cancel)
	go worker.watchCancellation(jobContext, entity.ID, &cancellationRequested, cancellationDone, cancel)
	err = handler.Handle(jobContext, entity)
	cancel()
	<-heartbeatDone
	<-cancellationDone
	worker.finishAttempt(ctx, entity, err, cancellationRequested.Load())
	return worker.finish(ctx, entity, err, cancellationRequested.Load())
}

func (worker *Worker) watchCancellation(ctx context.Context, id domain.ID, requested *atomic.Bool, done chan<- struct{}, cancel context.CancelFunc) {
	defer close(done)
	interval := worker.leaseDuration / 10
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := worker.repository.Get(ctx, id)
			if err != nil {
				continue
			}
			if job.Status == domain.StatusCancelRequested || job.Status == domain.StatusCanceled {
				requested.Store(true)
				cancel()
				return
			}
		}
	}
}

func (worker *Worker) finishAttempt(ctx context.Context, entity domain.Job, processingErr error, cancellationRequested bool) {
	outcome := "succeeded"
	var errorCode, errorMessage *string
	if cancellationRequested {
		outcome = "canceled"
	} else if processingErr != nil {
		outcome = "failed"
		code := "JOB_FAILED"
		if errors.Is(processingErr, context.Canceled) || retryable(processingErr) {
			outcome = "retry"
			code = "RETRYABLE_ERROR"
		}
		errorCode = &code
		errorMessage = stringPtr(processingErr.Error())
	}
	_ = worker.repository.FinishAttempt(ctx, entity.ID, entity.AttemptCount, outcome, errorCode, errorMessage)
}

func (worker *Worker) heartbeat(ctx context.Context, id domain.ID, done chan<- struct{}, cancel context.CancelFunc) {
	defer close(done)
	ticker := time.NewTicker(worker.leaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := worker.repository.Heartbeat(ctx, id, worker.workerID, worker.leaseDuration); err != nil {
				cancel()
				return
			}
		}
	}
}

func (worker *Worker) finish(ctx context.Context, entity domain.Job, processingErr error, cancellationRequested bool) error {
	if cancellationRequested {
		_, err := worker.repository.MarkCanceled(ctx, entity.ID, worker.workerID)
		return err
	}
	if processingErr == nil {
		_, err := worker.repository.MarkSucceeded(ctx, entity.ID, worker.workerID)
		return err
	}
	if errors.Is(processingErr, context.Canceled) || retryable(processingErr) {
		availableAt := worker.clock.Now().Add(retryDelay(entity.AttemptCount))
		_, err := worker.repository.MarkRetry(ctx, entity.ID, worker.workerID, availableAt, "RETRYABLE_ERROR", processingErr.Error())
		return err
	}
	return worker.finishFailure(ctx, entity, "JOB_FAILED", processingErr)
}

func (worker *Worker) finishFailure(ctx context.Context, entity domain.Job, code string, processingErr error) error {
	_, err := worker.repository.MarkFailed(ctx, entity.ID, worker.workerID, code, processingErr.Error())
	return err
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<uint(attempt-1)) * time.Minute
}

func stringPtr(value string) *string {
	return &value
}
