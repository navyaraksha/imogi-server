package filebatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	appemployee "github.com/navyaraksha/imogi/internal/application/employee"
	employeedomain "github.com/navyaraksha/imogi/internal/domain/employee"
	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	organizationdomain "github.com/navyaraksha/imogi/internal/domain/organization"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	platformstorage "github.com/navyaraksha/imogi/internal/platform/objectstorage"
	"github.com/navyaraksha/imogi/internal/platform/tabular"
)

// Committer implements the all-or-nothing import processors that are currently
// backed by the employee and payroll ledgers. Master imports reconcile the
// employee projection into immutable employment, assignment, tax-profile, and
// employee-number history records.
type Committer struct {
	fileRepository Repository
	employeeRepo   appemployee.Repository
	storage        platformstorage.Storage
	parser         Parser
	clock          clock.Clock
	payrollWriter  PayrollImportWriter
}

func NewCommitter(fileRepository Repository, employeeRepo appemployee.Repository, storage platformstorage.Storage, parser Parser, systemClock clock.Clock, payrollWriter PayrollImportWriter) (*Committer, error) {
	if fileRepository == nil || employeeRepo == nil || storage == nil || parser == nil || systemClock == nil {
		return nil, errors.New("committer dependencies are required")
	}
	return &Committer{fileRepository: fileRepository, employeeRepo: employeeRepo, storage: storage, parser: parser, clock: systemClock, payrollWriter: payrollWriter}, nil
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
	// Commit is intentionally all-or-nothing: domain writes and the final batch
	// status must not expose a partially imported employee dataset.
	batch, err := committer.fileRepository.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	if batch.Operation != domain.OperationEmployeeMaster {
		if batch.Operation != domain.OperationPayrollLedger || committer.payrollWriter == nil {
			return fmt.Errorf("%w: commit processor for %s is not available", domain.ErrUnsupportedOperation, batch.Operation)
		}
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
	if batch.Operation == domain.OperationPayrollLedger {
		if err := committer.commitPayroll(ctx, batch); err != nil {
			return err
		}
		finishedAt := committer.clock.Now().UTC()
		_, err = committer.fileRepository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: domain.BatchCompleted, CommitFinishedAt: &finishedAt, CommittedRows: intPtr(batch.ValidRows), RejectedRows: intPtr(batch.InvalidRows)})
		if err == nil {
			completed = true
		}
		return err
	}
	file, err := committer.fileRepository.GetFileObject(ctx, batch.InputFileID)
	if err != nil {
		return err
	}
	input, _, err := committer.storage.Open(ctx, file.ObjectKey)
	if err != nil {
		return err
	}
	defer input.Close()
	rows, closeRows, header, err := committer.openConfiguredRows(ctx, input, file.DetectedExtension, batch)
	if err != nil {
		return err
	}
	defer closeRows()
	if err := validateHeader(batch.Operation, header); err != nil {
		return err
	}
	configuration := templateConfiguration{HeaderRow: 1, DataStartRow: 2, DefaultTaxMethod: "monthly", DefaultEmploymentType: "permanent"}
	if batch.TemplateID != nil {
		template, templateErr := committer.fileRepository.GetTemplate(ctx, *batch.TemplateID)
		if templateErr != nil {
			return templateErr
		}
		configuration = parseTemplateConfiguration(template.Configuration)
	}

	err = committer.employeeRepo.WithinTransaction(ctx, func(tx appemployee.Transaction) error {
		matcher, _ := committer.employeeRepo.(IdentityMatcher)
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			values, err := rows.Columns()
			if err != nil {
				return err
			}
			master, err := parseEmployeeMasterRow(header, values, configuration)
			if err != nil {
				return err
			}
			var candidate *IdentityCandidate
			if matcher != nil {
				raw := make(map[string]string, len(header))
				for index, name := range header {
					if index < len(values) {
						raw[name] = strings.TrimSpace(values[index])
					}
				}
				candidates, matchErr := matcher.FindIdentityCandidates(ctx, batch.TenantID.UUID(), batch.CompanyID.UUID(), raw["nik"], raw["full_name"], raw["employee_number"])
				if matchErr != nil {
					return matchErr
				}
				for _, matchedCandidate := range candidates {
					if !matchedCandidate.NIKMatch {
						continue
					}
					matched := matchedCandidate
					candidate = &matched
					break
				}
			}
			var employment *employeedomain.Employment
			if candidate == nil {
				item, itemErr := master.newEmployee(batch, committer.clock.Now())
				if itemErr != nil {
					return itemErr
				}
				if _, err := tx.CreateEmployee(ctx, item); err != nil {
					return err
				}
				candidate = &IdentityCandidate{EmployeeID: item.ID.UUID()}
				employment, err = committer.reconcileMasterHistory(ctx, tx, batch, master, item.ID, committer.clock.Now())
				if err != nil {
					return err
				}
			} else if employment, err = committer.reconcileMasterHistory(ctx, tx, batch, master, employeedomain.EmployeeID(candidate.EmployeeID), committer.clock.Now()); err != nil {
				return err
			}
			if master.EmployeeNumber != "" && (candidate.EmployeeNumber == "" || normalizeIdentity(master.EmployeeNumber) != normalizeIdentity(candidate.EmployeeNumber)) {
				var employmentID *employeedomain.EmploymentID
				if employment != nil {
					value := employment.ID
					employmentID = &value
				}
				if err := committer.recordEmployeeNumberCorrection(ctx, tx, batch, employeedomain.EmployeeID(candidate.EmployeeID), master.EmployeeNumber, employmentID); err != nil {
					return err
				}
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

func (committer *Committer) recordEmployeeNumberCorrection(ctx context.Context, tx appemployee.Transaction, batch domain.ImportBatch, employeeID employeedomain.EmployeeID, number string, employmentID *employeedomain.EmploymentID) error {
	effectiveFrom := committer.clock.Now().UTC()
	if err := tx.CloseOpenEmployeeNumberHistory(ctx, employeeID, effectiveFrom, effectiveFrom.AddDate(0, 0, -1)); err != nil {
		return err
	}
	historyID, err := employeedomain.NewEmployeeNumberHistoryID()
	if err != nil {
		return err
	}
	historyType := employeedomain.EmployeeNumberPermanent
	if strings.HasPrefix(strings.ToUpper(number), "NF") {
		historyType = employeedomain.EmployeeNumberTemporary
	}
	history, err := employeedomain.NewEmployeeNumberHistory(historyID, batch.TenantID, batch.CompanyID, employeeID, employmentID, number, historyType, effectiveFrom, employeedomain.EmployeeNumberSourceImport, effectiveFrom)
	if err != nil {
		return err
	}
	batchID := batch.ID.UUID()
	history.SourceBatchID = &batchID
	userID := batch.CreatedBy.UUID()
	history.CreatedBy = &userID
	if _, err := tx.CreateEmployeeNumberHistory(ctx, history); err != nil {
		return err
	}
	numberCopy := number
	return tx.UpdateEmployeeNumberProjection(ctx, employeeID, &numberCopy, string(historyType))
}

type employeeMasterRow struct {
	EmployeeNumber        string
	NIK                   employeedomain.NIK
	FullName              string
	Personal              employeedomain.PersonalData
	PTKPCode              string
	TaxMethod             string
	JoinDate              *time.Time
	EndDate               *time.Time
	Termination           string
	EmploymentType        employeedomain.EmploymentType
	EffectiveFrom         *time.Time
	EffectiveFromExplicit bool
	LocationID            *organizationdomain.UnitID
	DepartmentID          *organizationdomain.UnitID
	PositionID            *organizationdomain.UnitID
	GroupID               *organizationdomain.UnitID
}

func parseEmployeeMasterRow(header, values []string, configuration templateConfiguration) (employeeMasterRow, error) {
	row := make(map[string]string, len(header))
	for index, name := range header {
		if index < len(values) {
			row[name] = strings.TrimSpace(values[index])
		}
	}
	nik, err := employeedomain.ParseNIK(row["nik"])
	if err != nil {
		return employeeMasterRow{}, err
	}
	gender, err := parseImportGender(row["gender"])
	if err != nil {
		return employeeMasterRow{}, err
	}
	birthDate, err := parseImportDate(row["birth_date"])
	if err != nil {
		return employeeMasterRow{}, fmt.Errorf("birth_date: %w", err)
	}
	joinDate, err := parseImportDate(row["join_date"])
	if err != nil {
		return employeeMasterRow{}, fmt.Errorf("join_date: %w", err)
	}
	endDate, err := parseImportDate(row["end_date"])
	if err != nil {
		return employeeMasterRow{}, fmt.Errorf("end_date: %w", err)
	}
	effectiveFrom, err := parseImportDate(row["effective_from"])
	if err != nil {
		return employeeMasterRow{}, fmt.Errorf("effective_from: %w", err)
	}
	if effectiveFrom == nil {
		effectiveFrom = joinDate
	}
	employmentType := configuration.DefaultEmploymentType
	if strings.TrimSpace(row["employment_type"]) != "" {
		employmentType = normalizeEmploymentType(row["employment_type"])
	}
	parsedEmploymentType, err := employeedomain.ParseEmploymentType(employmentType)
	if err != nil {
		return employeeMasterRow{}, err
	}
	return employeeMasterRow{
		EmployeeNumber: strings.TrimSpace(row["employee_number"]), NIK: nik, FullName: strings.TrimSpace(row["full_name"]),
		Personal: employeedomain.PersonalData{FullName: strings.TrimSpace(row["full_name"]), BirthPlace: optionalString(row["birth_place"]), BirthDate: birthDate, Gender: gender, Email: optionalString(row["email"]), Phone: optionalString(row["phone"]), Address: optionalString(row["address"])},
		PTKPCode: strings.TrimSpace(row["ptkp_code"]), TaxMethod: firstNonBlank(row["tax_method"], configuration.DefaultTaxMethod), JoinDate: joinDate, EndDate: endDate,
		Termination: firstNonBlank(row["termination_reason"], "imported resignation"), EmploymentType: parsedEmploymentType,
		EffectiveFrom: effectiveFrom, EffectiveFromExplicit: row["effective_from"] != "",
		LocationID: parseMappedUnitID(row, "location_id", "location", configuration.UnitMappings), DepartmentID: parseMappedUnitID(row, "department_id", "department", configuration.UnitMappings), PositionID: parseMappedUnitID(row, "position_id", "position", configuration.UnitMappings), GroupID: parseMappedUnitID(row, "group_id", "group", configuration.UnitMappings),
	}, nil
}

func (row employeeMasterRow) newEmployee(batch domain.ImportBatch, now time.Time) (employeedomain.Employee, error) {
	id, err := employeedomain.NewEmployeeID()
	if err != nil {
		return employeedomain.Employee{}, err
	}
	item, err := employeedomain.NewEmployee(id, row.EmployeeNumber, row.NIK, row.FullName, row.Personal, now)
	if err != nil {
		return employeedomain.Employee{}, err
	}
	if err := item.BindOwnership(batch.TenantID, batch.CompanyID); err != nil {
		return employeedomain.Employee{}, err
	}
	return item, nil
}

func (committer *Committer) reconcileMasterHistory(ctx context.Context, tx appemployee.Transaction, batch domain.ImportBatch, row employeeMasterRow, employeeID employeedomain.EmployeeID, now time.Time) (*employeedomain.Employment, error) {
	var employment *employeedomain.Employment
	if row.JoinDate != nil {
		entity, err := committer.reconcileEmployment(ctx, tx, batch, row, employeeID, now)
		if err != nil {
			return nil, err
		}
		employment = &entity
		if err := committer.reconcileAssignment(ctx, tx, row, entity, now); err != nil {
			return nil, err
		}
	}
	if row.PTKPCode == "" {
		return employment, nil
	}
	profiles, err := tx.ListTaxProfiles(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	effectiveFrom := now.UTC()
	if row.EffectiveFromExplicit && row.EffectiveFrom != nil {
		effectiveFrom = *row.EffectiveFrom
	} else if len(profiles) == 0 && row.JoinDate != nil {
		effectiveFrom = *row.JoinDate
	}
	for _, profile := range profiles {
		if profile.EffectiveTo == nil && profile.PTKPCode == row.PTKPCode && profile.TaxMethod == row.TaxMethod {
			return employment, nil
		}
		if profile.EffectiveTo == nil && profile.EffectiveFrom.Equal(effectiveFrom) {
			effectiveFrom = profile.EffectiveFrom.AddDate(0, 0, 1)
		}
	}
	profileID, err := employeedomain.NewTaxProfileID()
	if err != nil {
		return nil, err
	}
	profile, err := employeedomain.NewTaxProfile(profileID, employeeID, row.NIK, nil, row.PTKPCode, row.TaxMethod, effectiveFrom, now)
	if err != nil {
		return nil, err
	}
	if err := profile.BindOwnership(batch.TenantID, batch.CompanyID); err != nil {
		return nil, err
	}
	if _, err := tx.GetOpenTaxProfileForUpdate(ctx, employeeID); err == nil {
		openProfiles, listErr := tx.ListTaxProfiles(ctx, employeeID)
		if listErr != nil {
			return nil, listErr
		}
		for _, current := range openProfiles {
			if current.EffectiveTo == nil {
				closeDate := effectiveFrom.AddDate(0, 0, -1)
				if closeDate.Before(current.EffectiveFrom) {
					effectiveFrom = current.EffectiveFrom.AddDate(0, 0, 1)
					profile.EffectiveFrom = effectiveFrom
					closeDate = effectiveFrom.AddDate(0, 0, -1)
				}
				current.EffectiveTo = &closeDate
				if _, closeErr := tx.CloseTaxProfile(ctx, current); closeErr != nil {
					return nil, closeErr
				}
				break
			}
		}
	}
	_, err = tx.CreateTaxProfile(ctx, profile)
	return employment, err
}

func (committer *Committer) reconcileEmployment(ctx context.Context, tx appemployee.Transaction, batch domain.ImportBatch, row employeeMasterRow, employeeID employeedomain.EmployeeID, now time.Time) (employeedomain.Employment, error) {
	employments, err := tx.ListEmployments(ctx, employeeID)
	if err != nil {
		return employeedomain.Employment{}, err
	}
	for _, employment := range employments {
		if employment.JoinDate.Equal(*row.JoinDate) {
			if employment.EmploymentType != row.EmploymentType {
				return employeedomain.Employment{}, fmt.Errorf("employment type changed for %s", employeeID.String())
			}
			if employment.EndDate != nil && (row.EndDate == nil || !employment.EndDate.Equal(*row.EndDate)) {
				return employeedomain.Employment{}, fmt.Errorf("employment end date changed for %s", employeeID.String())
			}
			if row.EndDate != nil && employment.EndDate == nil {
				if err := committer.endImportedEmployment(ctx, tx, employment, *row.EndDate, row.Termination, now); err != nil {
					return employeedomain.Employment{}, err
				}
				employment.EndDate = row.EndDate
			}
			return employment, nil
		}
	}
	id, err := employeedomain.NewEmploymentID()
	if err != nil {
		return employeedomain.Employment{}, err
	}
	employment, err := employeedomain.NewEmployment(id, employeeID, batch.CompanyID, row.EmploymentType, *row.JoinDate, now)
	if err != nil {
		return employeedomain.Employment{}, err
	}
	if err := employment.BindTenant(batch.TenantID); err != nil {
		return employeedomain.Employment{}, err
	}
	if row.EndDate != nil {
		if err := employment.Resign(*row.EndDate, row.Termination, now); err != nil {
			return employeedomain.Employment{}, err
		}
	}
	created, err := tx.CreateEmployment(ctx, employment)
	return created, err
}

func (committer *Committer) endImportedEmployment(ctx context.Context, tx appemployee.Transaction, employment employeedomain.Employment, endDate time.Time, reason string, now time.Time) error {
	if assignment, err := tx.GetOpenAssignmentForUpdate(ctx, employment.ID); err == nil {
		if err := assignment.Close(endDate, now); err != nil {
			return err
		}
		if _, err := tx.CloseAssignment(ctx, assignment); err != nil {
			return err
		}
	} else if !errors.Is(err, employeedomain.ErrAssignmentNotFound) {
		return err
	}
	if err := employment.Resign(endDate, reason, now); err != nil {
		return err
	}
	_, err := tx.EndEmployment(ctx, employment)
	return err
}

func (committer *Committer) reconcileAssignment(ctx context.Context, tx appemployee.Transaction, row employeeMasterRow, employment employeedomain.Employment, now time.Time) error {
	if row.LocationID == nil && row.DepartmentID == nil && row.PositionID == nil && row.GroupID == nil {
		return nil
	}
	if current, err := tx.GetOpenAssignmentForUpdate(ctx, employment.ID); err == nil {
		if sameAssignment(current, row) {
			return nil
		}
		effectiveFrom := employment.JoinDate
		if row.EffectiveFrom != nil {
			effectiveFrom = *row.EffectiveFrom
		}
		closeDate := effectiveFrom
		if current.EffectiveFrom.Before(closeDate) {
			closeDate = closeDate.AddDate(0, 0, -1)
		} else {
			return fmt.Errorf("assignment effective date overlaps existing history")
		}
		if err := current.Close(closeDate, now); err != nil {
			return err
		}
		if _, err := tx.CloseAssignment(ctx, current); err != nil {
			return err
		}
	} else if !errors.Is(err, employeedomain.ErrAssignmentNotFound) {
		return err
	}
	id, err := employeedomain.NewAssignmentID()
	if err != nil {
		return err
	}
	effectiveFrom := employment.JoinDate
	if row.EffectiveFrom != nil {
		effectiveFrom = *row.EffectiveFrom
	}
	assignment, err := employeedomain.NewAssignment(id, employment.ID, row.LocationID, row.DepartmentID, row.PositionID, row.GroupID, effectiveFrom, now)
	if err != nil {
		return err
	}
	if employment.EndDate != nil {
		if err := assignment.Close(*employment.EndDate, now); err != nil {
			return err
		}
	}
	_, err = tx.CreateAssignment(ctx, assignment)
	return err
}

func sameAssignment(current employeedomain.Assignment, row employeeMasterRow) bool {
	return sameUnit(current.LocationID, row.LocationID) && sameUnit(current.DepartmentID, row.DepartmentID) && sameUnit(current.PositionID, row.PositionID) && sameUnit(current.GroupID, row.GroupID)
}

func sameUnit(left, right *organizationdomain.UnitID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.UUID() == right.UUID()
}

func parseImportDate(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "2/1/2006", "02-01-2006", "2-Jan-2006", "02 Jan 2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			parsed = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.UTC)
			return &parsed, nil
		}
	}
	if serial, err := strconv.ParseFloat(value, 64); err == nil && serial > 0 && serial < 100000 {
		parsed := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).Add(time.Duration(serial*24) * time.Hour)
		return &parsed, nil
	}
	return nil, fmt.Errorf("unsupported date %q", value)
}

func parseImportGender(value string) (*employeedomain.Gender, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "l", "lk", "male", "pria":
		value = string(employeedomain.GenderMale)
	case "p", "female", "wanita", "perempuan":
		value = string(employeedomain.GenderFemale)
	default:
		value = string(employeedomain.GenderUnspecified)
	}
	gender, err := employeedomain.ParseGender(value)
	if err != nil {
		return nil, err
	}
	return &gender, nil
}

func normalizeEmploymentType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(value, "tetap") || strings.Contains(value, "permanent") {
		return string(employeedomain.EmploymentPermanent)
	}
	return string(employeedomain.EmploymentNonPermanent)
}

func parseMappedUnitID(row map[string]string, idField, labelField string, mappings map[string]map[string]string) *organizationdomain.UnitID {
	value := firstNonBlank(row[idField], row[labelField])
	if value == "" {
		return nil
	}
	sourceValue := value
	if fieldMappings := mappings[idField]; fieldMappings != nil {
		if mapped := firstNonBlank(fieldMappings[value], fieldMappings[strings.ToLower(value)]); mapped != "" {
			value = mapped
		}
	}
	if fieldMappings := mappings[labelField]; fieldMappings != nil {
		if mapped := firstNonBlank(fieldMappings[sourceValue], fieldMappings[strings.ToLower(sourceValue)]); mapped != "" {
			value = mapped
		}
	}
	id, err := organizationdomain.ParseUnitID(value)
	if err != nil {
		return nil
	}
	return &id
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (committer *Committer) openConfiguredRows(ctx context.Context, input io.ReadCloser, extension string, batch domain.ImportBatch) (RowReader, func(), []string, error) {
	configuration := templateConfiguration{HeaderRow: 1, DataStartRow: 2}
	if batch.TemplateID != nil {
		template, err := committer.fileRepository.GetTemplate(ctx, *batch.TemplateID)
		if err != nil {
			return nil, func() {}, nil, err
		}
		configuration = parseTemplateConfiguration(template.Configuration)
	}
	if workbookParser, ok := committer.parser.(tabular.WorkbookParser); ok {
		workbook, err := workbookParser.OpenWorkbook(input, extension)
		if err != nil {
			return nil, func() {}, nil, err
		}
		infos := selectSheetInfos(batch.Operation, workbook.Sheets(), configuration.sheetNames())
		if len(infos) == 0 {
			_ = workbook.Close()
			return nil, func() {}, nil, errors.New("configured workbook sheet was not found")
		}
		rows, err := workbook.OpenSheet(infos[0].Name)
		if err != nil {
			_ = workbook.Close()
			return nil, func() {}, nil, err
		}
		for row := 1; row < configuration.HeaderRow; row++ {
			if !rows.Next() {
				_ = rows.Close()
				_ = workbook.Close()
				return nil, func() {}, nil, errors.New("configured header row was not found")
			}
		}
		header, err := nextColumns(rows)
		if err != nil {
			_ = rows.Close()
			_ = workbook.Close()
			return nil, func() {}, nil, err
		}
		for skipped := configuration.HeaderRow + 1; skipped < configuration.DataStartRow; skipped++ {
			if !rows.Next() {
				break
			}
		}
		return rows, func() { _ = rows.Close(); _ = workbook.Close() }, configuredHeader(header, configuration.Columns), nil
	}
	rows, err := committer.parser.Open(input, extension)
	if err != nil {
		return nil, func() {}, nil, err
	}
	header, err := nextColumns(rows)
	if err != nil {
		_ = rows.Close()
		return nil, func() {}, nil, err
	}
	return rows, func() { _ = rows.Close() }, configuredHeader(header, configuration.Columns), nil
}

func (committer *Committer) commitPayroll(ctx context.Context, batch domain.ImportBatch) error {
	rows, err := committer.fileRepository.ListValidationRows(ctx, batch.ID)
	if err != nil {
		return err
	}
	items := make([]PayrollImportRow, 0, len(rows))
	for _, row := range rows {
		if row.BlockingIssueCount > 0 {
			return fmt.Errorf("row %s still has blocking validation issues", row.ID.UUID().String())
		}
		values := make(map[string]string)
		if err := json.Unmarshal(row.NormalizedPayload, &values); err != nil {
			return err
		}
		items = append(items, PayrollImportRow{ID: row.ID, SheetName: row.SheetName, RowNumber: row.RowNumber, EmployeeID: row.EmployeeID, EmploymentID: row.EmploymentID, Values: values})
	}
	receipt, err := committer.payrollWriter.CommitPayrollImport(ctx, batch, items)
	if err != nil {
		return err
	}
	if err := committer.fileRepository.LinkPayrollContext(ctx, batch.ID, receipt.PayrollPeriodID, receipt.PayrollRunID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return committer.writeReceiptArtifact(ctx, batch, data)
}

func (committer *Committer) writeReceiptArtifact(ctx context.Context, batch domain.ImportBatch, data []byte) error {
	fileID, err := domain.NewFileObjectID()
	if err != nil {
		return err
	}
	artifactID, err := domain.NewArtifactID()
	if err != nil {
		return err
	}
	key := fmt.Sprintf("tenants/%s/companies/%s/batches/%s/import-receipt.json", batch.TenantID.String(), batch.CompanyID.String(), batch.ID.String())
	info, err := committer.storage.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/json")
	if err != nil {
		return err
	}
	file := domain.FileObject{ID: fileID, TenantID: batch.TenantID, CompanyID: batch.CompanyID, StorageProvider: committer.storage.Provider(), ObjectKey: key, OriginalFilename: "import-receipt.json", DetectedExtension: "json", DetectedMIMEType: "application/json", SizeBytes: info.SizeBytes, SHA256: info.SHA256, EncryptionMode: "filesystem-private", Status: domain.FileObjectAvailable, CreatedAt: committer.clock.Now().UTC(), UpdatedAt: committer.clock.Now().UTC()}
	if committer.storage.Provider() == "s3" {
		file.EncryptionMode = "sse-s3"
	}
	return committer.fileRepository.CreateArtifactFile(ctx, file, batch.ID, artifactID, domain.ArtifactImportReceipt)
}
