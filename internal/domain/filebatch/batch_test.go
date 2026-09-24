package filebatch

import (
	"testing"
	"time"

	"github.com/google/uuid"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	"github.com/navyaraksha/imogi/internal/domain/organization"
)

func TestImportBatchCommitRequiresSuccessfulValidation(t *testing.T) {
	batch, err := NewImportBatch(
		BatchID(uuid.MustParse("0199c5d8-8b20-7c31-8d9e-9a4f5f46b111")),
		organization.TenantID(uuid.MustParse("0199c5d8-8b20-7c31-8d9e-9a4f5f46b112")),
		organization.CompanyID(uuid.MustParse("0199c5d8-8b20-7c31-8d9e-9a4f5f46b113")),
		OperationEmployeeMaster,
		FileObjectID(uuid.MustParse("0199c5d8-8b20-7c31-8d9e-9a4f5f46b114")),
		identitydomain.UserID(uuid.MustParse("0199c5d8-8b20-7c31-8d9e-9a4f5f46b115")),
		time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := batch.RequestCommit(time.Now()); err != ErrBatchValidationRequired {
		t.Fatalf("expected validation requirement, got %v", err)
	}
	batch.Status = BatchValidated
	batch.InvalidRows = 0
	committed, err := batch.RequestCommit(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if committed.Status != BatchCommitRequested {
		t.Fatalf("expected commit_requested, got %s", committed.Status)
	}
}
