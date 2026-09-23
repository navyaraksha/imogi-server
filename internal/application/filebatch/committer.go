package filebatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appemployee "github.com/navyaraksha/imogi/internal/application/employee"
	employeedomain "github.com/navyaraksha/imogi/internal/domain/employee"
	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	platformstorage "github.com/navyaraksha/imogi/internal/platform/objectstorage"
)

// Committer implements the first all-or-nothing import processor. It is
// intentionally limited to employee_master until the employment, assignment,
// tax-profile, and payroll ledgers have their import mappings.
type Committer struct {
	fileRepository Repository
	employeeRepo   appemployee.Repository
	storage        platformstorage.Storage
	parser         Parser
	clock          clock.Clock
}

func NewCommitter(fileRepository Repository, employeeRepo appemployee.Repository, storage platformstorage.Storage, parser Parser, systemClock clock.Clock) (*Committer, error) {
	if fileRepository == nil || employeeRepo == nil || storage == nil || parser == nil || systemClock == nil {
		return nil, errors.New("committer dependencies are required")
	}
	return &Committer{fileRepository: fileRepository, employeeRepo: employeeRepo, storage: storage, parser: parser, clock: systemClock}, nil
}

func (committer *Committer) Handle(ctx context.Context, job jobdomain.Job) error {
	var payload struct {
		BatchID string `json:"batchId"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.BatchID == "" {
		return fmt.Errorf("invalid commit payload: %w", err)
	}
	batchID, err := domain.ParseBatchID(payload.BatchID)
	if err != nil {
		return err
	}
	return committer.Commit(ctx, batchID)
}

func (committer *Committer) Commit(ctx context.Context, batchID domain.BatchID) error {
	batch, err := committer.fileRepository.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	if batch.Operation != domain.OperationEmployeeMaster {
		return fmt.Errorf("%w: commit processor for %s is not available", domain.ErrUnsupportedOperation, batch.Operation)
	}
	startedAt := committer.clock.Now().UTC()
	if _, err := committer.fileRepository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: domain.BatchCommitting, CommitStartedAt: &startedAt}); err != nil {
		return err
	}
	committed := 0
	completed := false
	defer func() {
		if completed {
			return
		}
		finishedAt := committer.clock.Now().UTC()
		_, _ = committer.fileRepository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: domain.BatchCommitFailed, CommitFinishedAt: &finishedAt, CommittedRows: intPtr(committed), RejectedRows: intPtr(batch.TotalRows - committed)})
	}()
	file, err := committer.fileRepository.GetFileObject(ctx, batch.InputFileID)
	if err != nil {
		return err
	}
	input, _, err := committer.storage.Open(ctx, file.ObjectKey)
	if err != nil {
		return err
	}
	defer input.Close()
	rows, err := committer.parser.Open(input, file.DetectedExtension)
	if err != nil {
		return err
	}
	defer rows.Close()
	header, err := nextColumns(rows)
	if err != nil {
		return err
	}
	if err := validateHeader(batch.Operation, header); err != nil {
		return err
	}

	err = committer.employeeRepo.WithinTransaction(ctx, func(tx appemployee.Transaction) error {
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			values, err := rows.Columns()
			if err != nil {
				return err
			}
			item, err := employeeFromRow(batch, header, values, committer.clock.Now())
			if err != nil {
				return err
			}
			if _, err := tx.CreateEmployee(ctx, item); err != nil {
				return err
			}
			committed++
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	finishedAt := committer.clock.Now().UTC()
	_, err = committer.fileRepository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: domain.BatchCompleted, CommitFinishedAt: &finishedAt, CommittedRows: intPtr(committed), RejectedRows: intPtr(batch.TotalRows - committed)})
	if err == nil {
		completed = true
	}
	return err
}

func employeeFromRow(batch domain.ImportBatch, header, values []string, now time.Time) (employeedomain.Employee, error) {
	row := make(map[string]string, len(header))
	for index, name := range header {
		if index < len(values) {
			row[name] = strings.TrimSpace(values[index])
		}
	}
	nik, err := employeedomain.ParseNIK(row["nik"])
	if err != nil {
		return employeedomain.Employee{}, err
	}
	employeeID, err := employeedomain.NewEmployeeID()
	if err != nil {
		return employeedomain.Employee{}, err
	}
	item, err := employeedomain.NewEmployee(employeeID, row["employee_number"], nik, row["full_name"], employeedomain.PersonalData{}, now)
	if err != nil {
		return employeedomain.Employee{}, err
	}
	if err := item.BindOwnership(batch.TenantID, batch.CompanyID); err != nil {
		return employeedomain.Employee{}, err
	}
	return item, nil
}
