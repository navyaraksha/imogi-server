//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/xuri/excelize/v2"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	"github.com/navyaraksha/imogi/internal/domain/employee"
	filedomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/domain/organization"
	fileparser "github.com/navyaraksha/imogi/internal/infrastructure/fileparser"
	objectstorage "github.com/navyaraksha/imogi/internal/infrastructure/objectstorage"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

// TestImportWorkflowPostgresAndS3 is intentionally opt-in. It requires a
// migrated database and an existing S3 bucket; MinIO is the recommended local
// setup, while production can point the same adapter at Neon Object Storage.
func TestImportWorkflowPostgresAndS3(t *testing.T) {
	// Keep the opt-in test consistent with cmd/server and cmd/worker: local
	// development secrets may live in .env, while CI can provide real env vars.
	_ = godotenv.Load(".env", "../../.env")
	databaseURL := os.Getenv("INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	endpoint := firstNonBlankEnv("INTEGRATION_S3_ENDPOINT", "OBJECT_STORAGE_ENDPOINT", "AWS_ENDPOINT_URL_S3")
	bucket := firstNonBlankEnv("INTEGRATION_S3_BUCKET", "OBJECT_STORAGE_BUCKET", "AWS_S3_BUCKET", "S3_BUCKET")
	accessKey := firstNonBlankEnv("INTEGRATION_S3_ACCESS_KEY", "OBJECT_STORAGE_ACCESS_KEY", "AWS_ACCESS_KEY_ID")
	secretKey := firstNonBlankEnv("INTEGRATION_S3_SECRET_KEY", "OBJECT_STORAGE_SECRET_KEY", "AWS_SECRET_ACCESS_KEY")
	region := firstNonBlankEnv("INTEGRATION_S3_REGION", "OBJECT_STORAGE_REGION", "AWS_REGION", "AWS_DEFAULT_REGION")
	secure := strings.EqualFold(firstNonBlankEnv("INTEGRATION_S3_SECURE", "OBJECT_STORAGE_SECURE"), "true") || strings.HasPrefix(strings.ToLower(endpoint), "https://")
	if databaseURL == "" || endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		t.Skip("set DATABASE_URL and an S3 endpoint, bucket, access key, and secret key")
	}
	ctx := context.Background()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	tenantUUID := mustV7(t)
	companyUUID := mustV7(t)
	userUUID := mustV7(t)
	seedTenantCompanyUser(t, ctx, pool, tenantUUID, companyUUID, userUUID)
	protector, err := security.NewAESGCMProtector(mustBase64Key(t, "SENSITIVE_DATA_ENCRYPTION_KEY_BASE64"), mustBase64Key(t, "SENSITIVE_DATA_LOOKUP_KEY_BASE64"))
	if err != nil {
		t.Fatal(err)
	}
	employeeRepository, err := postgres.NewRepository(pool, protector)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := organization.TenantID(tenantUUID)
	companyID := organization.CompanyID(companyUUID)
	userID := identitydomain.UserID(userUUID)
	existingID, err := employee.NewEmployeeID()
	if err != nil {
		t.Fatal(err)
	}
	nik, err := employee.ParseNIK("3174010101010001")
	if err != nil {
		t.Fatal(err)
	}
	existing, err := employee.NewEmployee(existingID, "EMP-001", nik, "Existing Employee", employee.PersonalData{FullName: "Existing Employee"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := existing.BindOwnership(tenantID, companyID); err != nil {
		t.Fatal(err)
	}
	if _, err := employeeRepository.CreateEmployee(ctx, existing); err != nil {
		t.Fatal(err)
	}
	employmentID, err := employee.NewEmploymentID()
	if err != nil {
		t.Fatal(err)
	}
	employment, err := employee.NewEmployment(employmentID, existingID, companyID, employee.EmploymentPermanent, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := employeeRepository.CreateEmployment(ctx, employment); err != nil {
		t.Fatal(err)
	}

	storage, err := objectstorage.New(objectstorage.Config{Driver: "s3", Endpoint: endpoint, Region: region, Bucket: bucket, AccessKey: accessKey, SecretKey: secretKey, Secure: secure})
	if err != nil {
		t.Fatal(err)
	}
	fileRepository, err := postgres.NewFileBatchRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	parser, err := fileparser.New(1000)
	if err != nil {
		t.Fatal(err)
	}
	systemClock := clock.System{}
	principal := security.Principal{UserID: userID.UUID(), TenantID: tenantUUID, Subject: "integration", Capabilities: allFileCapabilities(), CompanyIDs: map[uuid.UUID]struct{}{companyUUID: {}}}
	requestContext := security.WithPrincipal(ctx, principal)
	service, err := appfilebatch.NewService(fileRepository, storage, security.ContextAuthorizer{}, systemClock, 15*time.Minute, 20<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := appfilebatch.NewValidator(fileRepository, storage, parser, systemClock, 1000)
	if err != nil {
		t.Fatal(err)
	}
	validator.SetIdentityMatcher(employeeRepository)
	payrollWriter, err := postgres.NewPayrollImportWriter(pool, systemClock)
	if err != nil {
		t.Fatal(err)
	}
	committer, err := appfilebatch.NewCommitter(fileRepository, employeeRepository, storage, parser, systemClock, payrollWriter)
	if err != nil {
		t.Fatal(err)
	}

	masterTemplate := createTemplate(t, fileRepository, tenantID, companyID, "employee_master", "master_employees")
	masterBytes := workbookBytes(t, "master_employees", [][]string{{"KTP", "Nama", "No Karyawan", "PTKP", "TGL MASUK", "JENIS"}, {"3174010101010002", "New Employee", "NF001", "TK/0", "2026-01-01", "PERMANENT"}})
	masterBatch, validationJob := createRealBatch(t, requestContext, service, companyID, filedomain.OperationEmployeeMaster, "master.xlsx", masterBytes, &masterTemplate, nil)
	if err := validator.Handle(requestContext, validationJob); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.RequestCommit(requestContext, masterBatch.ID); err != nil {
		t.Fatal(err)
	}
	if err := committer.Commit(requestContext, masterBatch.ID); err != nil {
		t.Fatal(err)
	}
	assertArtifactRoles(t, fileRepository, requestContext, masterBatch.ID, []filedomain.ArtifactRole{filedomain.ArtifactReadyImport, filedomain.ArtifactImportReceipt})

	// Re-importing the same NIK is a warning-only reconciliation. The allowed
	// number/PTKP changes are committed as historical versions while identity
	// and personal data remain unchanged.
	masterUpdateBytes := workbookBytes(t, "master_employees", [][]string{{"KTP", "Nama", "No Karyawan", "PTKP", "TGL MASUK", "JENIS"}, {"3174010101010002", "New Employee", "EMP-002", "K/1", "2026-01-01", "PERMANENT"}})
	masterUpdateBatch, masterUpdateJob := createRealBatch(t, requestContext, service, companyID, filedomain.OperationEmployeeMaster, "master-update.xlsx", masterUpdateBytes, &masterTemplate, nil)
	if err := validator.Handle(requestContext, masterUpdateJob); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.RequestCommit(requestContext, masterUpdateBatch.ID); err != nil {
		t.Fatal(err)
	}
	if err := committer.Commit(requestContext, masterUpdateBatch.ID); err != nil {
		t.Fatal(err)
	}

	payrollTemplate := createTemplate(t, fileRepository, tenantID, companyID, "payroll_ledger", "DKM Rekap")
	payrollBytes := workbookBytes(t, "DKM Rekap", [][]string{{"No Karyawan", "Nama", "Gross Income", "Taxable Income", "Take Home Pay"}, {"EMP-001", "Existing Employee", "10000000", "9000000", "8500000"}})
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	payDate := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	pContext := &filedomain.PayrollImportContext{TaxYear: 2026, TaxMonth: 1, CoverageFrom: from, CoverageTo: to, PayDate: &payDate, RunType: "regular"}
	payrollBatch, payrollJob := createRealBatch(t, requestContext, service, companyID, filedomain.OperationPayrollLedger, "salary.xlsm", payrollBytes, &payrollTemplate, pContext)
	if err := validator.Handle(requestContext, payrollJob); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.RequestCommit(requestContext, payrollBatch.ID); err != nil {
		t.Fatal(err)
	}
	if err := committer.Commit(requestContext, payrollBatch.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := fileRepository.GetBatch(requestContext, payrollBatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.PayrollContext == nil || completed.PayrollContext.PayrollRunID == nil {
		t.Fatal("payroll run was not linked to import context")
	}
	assertArtifactRoles(t, fileRepository, requestContext, payrollBatch.ID, []filedomain.ArtifactRole{filedomain.ArtifactReadyImport, filedomain.ArtifactImportReceipt})
}

func createTemplate(t *testing.T, repository *postgres.FileBatchRepository, tenantID organization.TenantID, companyID organization.CompanyID, templateType, sheet string) filedomain.ImportTemplate {
	t.Helper()
	id, err := filedomain.NewTemplateID()
	if err != nil {
		t.Fatal(err)
	}
	configuration, _ := json.Marshal(map[string]any{"sheetName": sheet, "headerRow": 1, "dataStartRow": 2, "defaultTaxMethod": "monthly", "columns": map[string]string{"nik": "KTP", "full_name": "Nama", "employee_number": "No Karyawan", "ptkp_code": "PTKP", "join_date": "TGL MASUK", "employment_type": "JENIS", "gross_income": "Gross Income", "taxable_income": "Taxable Income", "take_home_pay": "Take Home Pay"}})
	template := filedomain.ImportTemplate{ID: id, TenantID: &tenantID, CompanyID: &companyID, TemplateType: templateType, Version: "integration-1", FileFormat: "xlsx", Status: "active", Configuration: configuration}
	if _, err := repository.CreateTemplate(context.Background(), template); err != nil {
		t.Fatal(err)
	}
	return template
}

func createRealBatch(t *testing.T, ctx context.Context, service *appfilebatch.Service, companyID organization.CompanyID, operation filedomain.Operation, filename string, content []byte, template *filedomain.ImportTemplate, payrollContext *filedomain.PayrollImportContext) (filedomain.ImportBatch, jobdomain.Job) {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	mimeType := "application/octet-stream"
	if extension == "xlsx" {
		mimeType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	if extension == "xlsm" {
		mimeType = "application/vnd.ms-excel.sheet.macroEnabled.12"
	}
	created, err := service.CreateBatch(ctx, appfilebatch.CreateBatchInput{CompanyID: companyID, Operation: operation, Filename: filename, Extension: extension, MIMEType: mimeType, ExpectedSize: int64(len(content)), TemplateID: &template.ID, PayrollContext: payrollContext})
	if err != nil {
		t.Fatal(err)
	}
	batch, job, err := service.UploadDirect(ctx, created.Batch.ID, bytes.NewReader(content), int64(len(content)), mimeType)
	if err != nil {
		t.Fatal(err)
	}
	return batch, job
}

func workbookBytes(t *testing.T, sheet string, rows [][]string) []byte {
	t.Helper()
	workbook := excelize.NewFile()
	defer workbook.Close()
	if err := workbook.SetSheetName("Sheet1", sheet); err != nil {
		t.Fatal(err)
	}
	for rowIndex, row := range rows {
		if err := workbook.SetSheetRow(sheet, fmt.Sprintf("A%d", rowIndex+1), &row); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := workbook.Write(&output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func assertArtifactRoles(t *testing.T, repository *postgres.FileBatchRepository, ctx context.Context, batchID filedomain.BatchID, expected []filedomain.ArtifactRole) {
	t.Helper()
	artifacts, err := repository.ListArtifacts(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}
	found := map[filedomain.ArtifactRole]bool{}
	for _, item := range artifacts {
		found[item.Role] = true
	}
	for _, role := range expected {
		if !found[role] {
			t.Fatalf("artifact %q not found", role)
		}
	}
}

func mustV7(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := identitydomain.NewUUIDv7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedTenantCompanyUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, companyID, userID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO platform.tenants (id, slug, name, status) VALUES ($1, $2, $3, 'active'); INSERT INTO organization.companies (id, tenant_id, code, legal_name, display_name, status) VALUES ($4, $1, $5, $6, $6, 'active'); INSERT INTO platform.users (id, google_subject, email, display_name) VALUES ($7, $8, $9, $9)`, tenantID, "it-"+strings.ReplaceAll(tenantID.String()[:8], "-", ""), "Integration Tenant", companyID, "IT", "Integration Company", userID, "integration-"+userID.String(), "integration@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
}

func mustBase64Key(t *testing.T, name string) []byte {
	t.Helper()
	value := os.Getenv(name)
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return decoded
}

func firstNonBlankEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
