package job

import (
	"context"
	"time"

	domain "github.com/navyaraksha/imogi/internal/domain/job"
)

type Repository interface {
	Create(context.Context, domain.Job) (domain.Job, error)
	Get(context.Context, domain.ID) (domain.Job, error)
	Claim(context.Context, string, string, time.Duration) (domain.Job, error)
	Heartbeat(context.Context, domain.ID, string, time.Duration) error
	MarkSucceeded(context.Context, domain.ID, string) (domain.Job, error)
	MarkRetry(context.Context, domain.ID, string, time.Time, string, string) (domain.Job, error)
	MarkFailed(context.Context, domain.ID, string, string, string) (domain.Job, error)
	MarkCanceled(context.Context, domain.ID, string) (domain.Job, error)
	RequestCancellation(context.Context, domain.ID) (domain.Job, error)
	CreateAttempt(context.Context, domain.AttemptID, domain.ID, int, string) error
	FinishAttempt(context.Context, domain.ID, int, string, *string, *string) error
}

type Handler interface {
	Handle(context.Context, domain.Job) error
}

type HandlerRegistry interface {
	Handler(string) (Handler, bool)
}
