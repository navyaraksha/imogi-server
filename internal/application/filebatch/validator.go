package filebatch

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
	jobdomain "github.com/navyaraksha/imogi/internal/domain/job"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	platformstorage "github.com/navyaraksha/imogi/internal/platform/objectstorage"
	"github.com/navyaraksha/imogi/internal/platform/tabular"
)

type RowReader = tabular.RowReader
type Parser = tabular.Parser

type Validator struct {
	repository Repository
	storage    platformstorage.Storage
	parser     Parser
	clock      clock.Clock
	maxRows    int
	matcher    IdentityMatcher
}

func (validator *Validator) SetIdentityMatcher(matcher IdentityMatcher) {
	validator.matcher = matcher
}

func NewValidator(repository Repository, storage platformstorage.Storage, parser Parser, systemClock clock.Clock, maxRows int) (*Validator, error) {
	if repository == nil || storage == nil || parser == nil || systemClock == nil || maxRows <= 0 {
		return nil, errors.New("validator dependencies are required")
	}
	return &Validator{repository: repository, storage: storage, parser: parser, clock: systemClock, maxRows: maxRows}, nil
}

func (validator *Validator) Handle(ctx context.Context, job jobdomain.Job) error {
	var payload struct {
		BatchID string `json:"batchId"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.BatchID == "" {
		return fmt.Errorf("invalid validation payload: %w", err)
	}
	batchID, err := domain.ParseBatchID(payload.BatchID)
	if err != nil {
		return err
	}
	return validator.Validate(ctx, batchID)
}

func (validator *Validator) Validate(ctx context.Context, batchID domain.BatchID) error {
	// Validation writes summary and error artifacts before the batch status is
	// advanced, allowing clients to inspect the complete validation result.
	startedAt := validator.clock.Now().UTC()
	batch, err := validator.repository.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	if _, err := validator.repository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: domain.BatchValidating, ValidationStartedAt: &startedAt}); err != nil {
		return err
	}
	if err := validator.repository.ClearValidationRows(ctx, batchID); err != nil {
		return validator.fail(ctx, batchID, startedAt, err)
	}
	file, err := validator.repository.GetFileObject(ctx, batch.InputFileID)
	if err != nil {
		return err
	}
	input, _, err := validator.storage.Open(ctx, file.ObjectKey)
	if err != nil {
		return fmt.Errorf("open input object: %w", err)
	}
	defer input.Close()
	var configuredNames []string
	configuration := templateConfiguration{}
	if batch.TemplateID != nil {
		template, templateErr := validator.repository.GetTemplate(ctx, *batch.TemplateID)
		if templateErr != nil {
			return validator.fail(ctx, batchID, startedAt, templateErr)
		}
		configuration = parseTemplateConfiguration(template.Configuration)
		configuredNames = configuration.sheetNames()
	}
	sheets, closeSheets, err := validator.openSheets(input, file.DetectedExtension, batch.Operation, configuredNames)
	if err != nil {
		return validator.fail(ctx, batchID, startedAt, err)
	}
	defer closeSheets()
	if len(sheets) == 0 {
		return validator.fail(ctx, batchID, startedAt, errors.New("workbook has no visible sheets"))
	}

	result := validationResult{Operation: string(batch.Operation), Errors: make([]validationError, 0), Normalized: make([]normalizedRow, 0)}
	for _, sheet := range sheets {
		if err := validator.validateSheet(ctx, batch, sheet, configuration, &result); err != nil {
			return validator.fail(ctx, batchID, startedAt, err)
		}
	}
	result.FinishedAt = validator.clock.Now().UTC()
	if err := validator.writeArtifacts(ctx, batch, result); err != nil {
		return validator.fail(ctx, batchID, startedAt, err)
	}
	status := domain.BatchValidated
	if result.InvalidRows > 0 {
		status = domain.BatchValidationFailed
	}
	_, err = validator.repository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: status, ValidationFinishedAt: &result.FinishedAt, TotalRows: intPtr(result.TotalRows), ValidRows: intPtr(result.ValidRows), InvalidRows: intPtr(result.InvalidRows), WarningRows: intPtr(result.WarningRows)})
	return err
}

type validationResult struct {
	Operation   string            `json:"operation"`
	Sheets      []string          `json:"sheets"`
	TotalRows   int               `json:"totalRows"`
	ValidRows   int               `json:"validRows"`
	InvalidRows int               `json:"invalidRows"`
	WarningRows int               `json:"warningRows"`
	Errors      []validationError `json:"errors"`
	Normalized  []normalizedRow   `json:"-"`
	FinishedAt  time.Time         `json:"finishedAt"`
}

type validationError struct {
	SheetName string `json:"sheetName"`
	Row       int    `json:"row"`
	Field     string `json:"field,omitempty"`
	Code      string `json:"code"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}

type normalizedRow struct {
	SheetName  string                `json:"sheetName"`
	Row        int                   `json:"row"`
	Values     map[string]string     `json:"values"`
	Status     string                `json:"status"`
	Issues     []validationError     `json:"issues,omitempty"`
	Components []normalizedComponent `json:"components,omitempty"`
}

type normalizedComponent struct {
	Code   string `json:"code"`
	Type   string `json:"type"`
	Amount string `json:"amount"`
}

type openedSheet struct {
	Name string
	Rows RowReader
}

type templateConfiguration struct {
	SheetName             string                       `json:"sheetName"`
	SheetNames            []string                     `json:"sheetNames"`
	Layout                string                       `json:"layout"`
	HeaderRow             int                          `json:"headerRow"`
	HeaderRows            []int                        `json:"headerRows"`
	DataStartRow          int                          `json:"dataStartRow"`
	Columns               map[string]string            `json:"columns"`
	UnitMappings          map[string]map[string]string `json:"unitMappings"`
	DefaultTaxMethod      string                       `json:"defaultTaxMethod"`
	DefaultEmploymentType string                       `json:"defaultEmploymentType"`
	Regions               []repeatedBlockConfiguration `json:"regions"`
}

type repeatedBlockConfiguration struct {
	Name                 string                   `json:"name"`
	EmployeeNumberColumn string                   `json:"employeeNumberColumn"`
	FullNameColumn       string                   `json:"fullNameColumn"`
	TakeHomeColumn       string                   `json:"takeHomeColumn"`
	IgnoreRows           []repeatedBlockIgnore    `json:"ignoreRows"`
	Earnings             []repeatedBlockComponent `json:"earnings"`
	Deductions           []repeatedBlockComponent `json:"deductions"`
}

type repeatedBlockIgnore struct {
	EmployeeNumber string `json:"employeeNumber"`
	FullName       string `json:"fullName"`
}

type repeatedBlockComponent struct {
	Code   string `json:"code"`
	Type   string `json:"type"`
	Column string `json:"column"`
}

func parseTemplateConfiguration(configuration json.RawMessage) templateConfiguration {
	value := templateConfiguration{HeaderRow: 1, DataStartRow: 2}
	if len(configuration) == 0 || json.Unmarshal(configuration, &value) != nil {
		return value
	}
	if value.HeaderRow < 1 {
		value.HeaderRow = 1
	}
	if value.DataStartRow <= 0 {
		value.DataStartRow = value.HeaderRow + 1
	}
	if value.DataStartRow <= value.HeaderRow {
		value.DataStartRow = value.HeaderRow + 1
	}
	if strings.TrimSpace(value.DefaultTaxMethod) == "" {
		value.DefaultTaxMethod = "monthly"
	}
	if strings.TrimSpace(value.DefaultEmploymentType) == "" {
		value.DefaultEmploymentType = "permanent"
	}
	return value
}

func (configuration templateConfiguration) sheetNames() []string {
	if len(configuration.SheetNames) > 0 {
		return configuration.SheetNames
	}
	if strings.TrimSpace(configuration.SheetName) != "" {
		return []string{configuration.SheetName}
	}
	return nil
}

func (validator *Validator) openSheets(input io.ReadCloser, extension string, operation domain.Operation, configuredNames []string) ([]openedSheet, func(), error) {
	if workbookParser, ok := validator.parser.(tabular.WorkbookParser); ok {
		workbook, err := workbookParser.OpenWorkbook(input, extension)
		if err != nil {
			return nil, func() {}, err
		}
		infos := workbook.Sheets()
		if len(infos) == 0 {
			_ = workbook.Close()
			return nil, func() {}, errors.New("workbook has no sheets")
		}
		opened := make([]openedSheet, 0, len(infos))
		for _, info := range selectSheetInfos(operation, infos, configuredNames) {
			if info.Hidden {
				continue
			}
			rows, err := workbook.OpenSheet(info.Name)
			if err != nil {
				_ = workbook.Close()
				return nil, func() {}, err
			}
			opened = append(opened, openedSheet{Name: info.Name, Rows: rows})
		}
		return opened, func() {
			for _, sheet := range opened {
				_ = sheet.Rows.Close()
			}
			_ = workbook.Close()
		}, nil
	}
	rows, err := validator.parser.Open(input, extension)
	if err != nil {
		return nil, func() {}, err
	}
	return []openedSheet{{Name: "Sheet1", Rows: rows}}, func() { _ = rows.Close() }, nil
}

func selectSheetInfos(operation domain.Operation, infos []tabular.SheetInfo, configuredNames []string) []tabular.SheetInfo {
	visible := make([]tabular.SheetInfo, 0, len(infos))
	for _, info := range infos {
		if !info.Hidden {
			visible = append(visible, info)
		}
	}
	if len(configuredNames) > 0 {
		selected := make([]tabular.SheetInfo, 0, len(configuredNames))
		for _, configuredName := range configuredNames {
			for _, info := range visible {
				if strings.EqualFold(strings.TrimSpace(configuredName), info.Name) {
					selected = append(selected, info)
				}
			}
		}
		return selected
	}
	if operation != domain.OperationEmployeeMaster {
		return visible
	}
	preferred := map[string]struct{}{
		"masteremployees": {},
		"masteremployee":  {},
		"master":          {},
	}
	for _, info := range visible {
		if _, ok := preferred[normalizeSheetName(info.Name)]; ok {
			return []tabular.SheetInfo{info}
		}
	}
	if len(visible) > 1 {
		return visible[:1]
	}
	return visible
}

func normalizeSheetName(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if character >= 'a' && character <= 'z' {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func configuredHeader(header []string, columns map[string]string) []string {
	if len(columns) == 0 {
		return header
	}
	bySource := make(map[string]string, len(columns))
	for canonical, source := range columns {
		bySource[normalizeColumn(source)] = normalizeColumn(canonical)
	}
	result := append([]string(nil), header...)
	for index, source := range result {
		if canonical, ok := bySource[normalizeColumn(source)]; ok {
			result[index] = canonical
		}
	}
	return result
}

func (validator *Validator) validateSheet(ctx context.Context, batch domain.ImportBatch, sheet openedSheet, configuration templateConfiguration, result *validationResult) error {
	if strings.EqualFold(strings.TrimSpace(configuration.Layout), "repeated_blocks") {
		return validator.validateRepeatedBlocks(ctx, batch, sheet, configuration, result)
	}
	operation := batch.Operation
	for row := 1; row < configuration.HeaderRow; row++ {
		if !sheet.Rows.Next() {
			return fmt.Errorf("sheet %q: header row %d is not present", sheet.Name, configuration.HeaderRow)
		}
	}
	header, err := nextColumns(sheet.Rows)
	if err != nil {
		return fmt.Errorf("sheet %q: %w", sheet.Name, err)
	}
	result.Sheets = append(result.Sheets, sheet.Name)
	header = configuredHeader(header, configuration.Columns)
	if err := validateHeader(operation, header); err != nil {
		result.InvalidRows++
		result.Errors = append(result.Errors, validationError{SheetName: sheet.Name, Row: configuration.HeaderRow, Code: "INVALID_HEADER", Severity: "error", Message: err.Error()})
		return nil
	}
	dataRows := 0
	for skipped := configuration.HeaderRow + 1; skipped < configuration.DataStartRow; skipped++ {
		if !sheet.Rows.Next() {
			break
		}
	}
	for sheet.Rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		result.TotalRows++
		dataRows++
		rowNumber := configuration.DataStartRow + dataRows - 1
		if result.TotalRows > validator.maxRows {
			result.Errors = append(result.Errors, validationError{SheetName: sheet.Name, Row: rowNumber, Code: "ROW_LIMIT_EXCEEDED", Severity: "error", Message: "maximum row limit exceeded"})
			result.InvalidRows++
			break
		}
		values, err := sheet.Rows.Columns()
		if err != nil {
			return err
		}
		rawValues := rawRowValues(header, values)
		mapped := rowValues(header, values)
		rowIssues := make([]validationError, 0)
		if err := validateEmployeeRow(operation, header, values); err != nil {
			rowIssues = append(rowIssues, validationError{SheetName: sheet.Name, Row: rowNumber, Code: "INVALID_ROW", Severity: "error", Message: err.Error()})
		}
		status := "valid"
		matchStatus, candidate, matchIssue := validator.resolveIdentity(ctx, batch, rawValues, rowNumber, operation, sheet.Name)
		if matchStatus != "" {
			status = matchStatus
		}
		if matchIssue != nil {
			rowIssues = append(rowIssues, *matchIssue)
		}
		blockingIssues := countBlockingValidationErrors(rowIssues)
		if blockingIssues > 0 {
			status = "invalid"
			result.InvalidRows++
		} else {
			if len(rowIssues) > 0 {
				status = "valid_with_warnings"
				result.WarningRows++
			}
			result.ValidRows++
		}
		if len(rowIssues) > 0 {
			result.Errors = append(result.Errors, rowIssues...)
		}
		result.Normalized = append(result.Normalized, normalizedRow{SheetName: sheet.Name, Row: rowNumber, Values: mapped, Status: status, Issues: rowIssues})
		if err := validator.persistValidationRow(ctx, batch.ID, sheet.Name, rowNumber, mapped, status, rowIssues, candidate); err != nil {
			return err
		}
	}
	return sheet.Rows.Err()
}

func (validator *Validator) validateRepeatedBlocks(ctx context.Context, batch domain.ImportBatch, sheet openedSheet, configuration templateConfiguration, result *validationResult) error {
	if len(configuration.Regions) == 0 {
		return fmt.Errorf("sheet %q repeated_blocks layout has no regions", sheet.Name)
	}
	result.Sheets = append(result.Sheets, sheet.Name)
	dataStartRow := configuration.DataStartRow
	if dataStartRow <= 0 {
		dataStartRow = 1
	}
	for currentRow := 1; currentRow < dataStartRow; currentRow++ {
		if !sheet.Rows.Next() {
			return fmt.Errorf("sheet %q data row %d is not present", sheet.Name, dataStartRow)
		}
	}

	for currentRow := dataStartRow; sheet.Rows.Next(); currentRow++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		values, err := sheet.Rows.Columns()
		if err != nil {
			return err
		}
		for _, region := range configuration.Regions {
			row, ok, err := normalizeRepeatedBlockRow(sheet.Name, currentRow, values, region)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			result.TotalRows++
			if result.TotalRows > validator.maxRows {
				result.Errors = append(result.Errors, validationError{SheetName: sheet.Name, Row: currentRow, Code: "ROW_LIMIT_EXCEEDED", Severity: "error", Message: "maximum row limit exceeded"})
				result.InvalidRows++
				return nil
			}

			issues := validateRepeatedBlockValues(sheet.Name, currentRow, row.Values)
			status := "valid"
			matchStatus, candidate, matchIssue := validator.resolveIdentity(ctx, batch, row.RawValues, currentRow, batch.Operation, sheet.Name)
			if matchStatus != "" {
				status = matchStatus
			}
			if matchIssue != nil {
				issues = append(issues, *matchIssue)
			}
			blockingIssues := countBlockingValidationErrors(issues)
			if blockingIssues > 0 {
				status = "invalid"
				result.InvalidRows++
			} else {
				if len(issues) > 0 {
					status = "valid_with_warnings"
					result.WarningRows++
				}
				result.ValidRows++
			}
			if len(issues) > 0 {
				result.Errors = append(result.Errors, issues...)
			}
			result.Normalized = append(result.Normalized, normalizedRow{SheetName: sheet.Name, Row: currentRow, Values: row.Values, Status: status, Issues: issues, Components: row.Components})
			if err := validator.persistValidationRow(ctx, batch.ID, sheet.Name, currentRow, row.Values, status, issues, candidate); err != nil {
				return err
			}
		}
	}
	return sheet.Rows.Err()
}

type repeatedBlockRow struct {
	Values     map[string]string
	RawValues  map[string]string
	Components []normalizedComponent
}

func normalizeRepeatedBlockRow(sheetName string, rowNumber int, values []string, configuration repeatedBlockConfiguration) (repeatedBlockRow, bool, error) {
	name := cellValue(values, configuration.FullNameColumn)
	number := cellValue(values, configuration.EmployeeNumberColumn)
	if strings.TrimSpace(name) == "" && strings.TrimSpace(number) == "" {
		return repeatedBlockRow{}, false, nil
	}
	if isRepeatedBlockSummary(name, number) || matchesRepeatedBlockIgnore(name, number, configuration.IgnoreRows) {
		return repeatedBlockRow{}, false, nil
	}
	if strings.TrimSpace(name) == "" && strings.TrimSpace(number) == "" {
		return repeatedBlockRow{}, false, nil
	}

	components := make([]normalizedComponent, 0, len(configuration.Earnings)+len(configuration.Deductions))
	var earningsTotal int64
	for _, component := range append(append([]repeatedBlockComponent(nil), configuration.Earnings...), configuration.Deductions...) {
		amount, err := parseImportAmount(cellValue(values, component.Column))
		if err != nil {
			return repeatedBlockRow{}, false, fmt.Errorf("sheet %q row %d column %s: %w", sheetName, rowNumber, component.Column, err)
		}
		if amount == 0 {
			continue
		}
		if component.Type == "earning" {
			earningsTotal += amount
		}
		components = append(components, normalizedComponent{Code: component.Code, Type: component.Type, Amount: fmt.Sprintf("%d", amount)})
	}
	takeHome, err := parseImportAmount(cellValue(values, configuration.TakeHomeColumn))
	if err != nil {
		return repeatedBlockRow{}, false, fmt.Errorf("sheet %q row %d column %s: %w", sheetName, rowNumber, configuration.TakeHomeColumn, err)
	}
	canonical := map[string]string{
		"employee_number": number,
		"full_name":       name,
		"gross_income":    fmt.Sprintf("%d", earningsTotal),
		"taxable_income":  fmt.Sprintf("%d", earningsTotal),
		"take_home_pay":   fmt.Sprintf("%d", takeHome),
	}
	raw := map[string]string{
		"employee_number": number,
		"full_name":       name,
	}
	return repeatedBlockRow{Values: canonical, RawValues: raw, Components: components}, true, nil
}

func validateRepeatedBlockValues(sheetName string, rowNumber int, values map[string]string) []validationError {
	issues := make([]validationError, 0, 2)
	if strings.TrimSpace(values["full_name"]) == "" && strings.TrimSpace(values["employee_number"]) == "" {
		issues = append(issues, validationError{SheetName: sheetName, Row: rowNumber, Code: "IDENTITY_FIELDS_MISSING", Severity: "error", Message: "employee_number or full_name is required"})
	}
	if values["gross_income"] == "0" && values["take_home_pay"] == "0" {
		issues = append(issues, validationError{SheetName: sheetName, Row: rowNumber, Code: "PAYROLL_ZERO_AMOUNT", Severity: "warning", Message: "payroll row has zero gross income and take-home pay"})
	}
	return issues
}

func cellValue(values []string, column string) string {
	index, err := excelColumnIndex(column)
	if err != nil || index < 0 || index >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[index])
}

func excelColumnIndex(value string) (int, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return 0, errors.New("column is required")
	}
	index := 0
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return 0, fmt.Errorf("invalid Excel column %q", value)
		}
		index = index*26 + int(character-'A'+1)
	}
	return index - 1, nil
}

func parseImportAmount(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0, nil
	}
	value = strings.ReplaceAll(value, "Rp", "")
	value = strings.ReplaceAll(value, "rp", "")
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, ".", "")
	value = strings.ReplaceAll(value, ",", "")
	value = strings.ReplaceAll(value, "(", "-")
	value = strings.ReplaceAll(value, ")", "")
	var result int64
	negative := false
	for index, character := range value {
		if index == 0 && character == '-' {
			negative = true
			continue
		}
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("invalid amount %q", value)
		}
		result = result*10 + int64(character-'0')
	}
	if negative {
		result = -result
	}
	return result, nil
}

func isRepeatedBlockSummary(name, number string) bool {
	value := strings.ToUpper(strings.TrimSpace(name + " " + number))
	return strings.Contains(value, "TOTAL") || strings.Contains(value, "JUMLAH") || strings.Contains(value, "NO. NAMA")
}

func matchesRepeatedBlockIgnore(name, number string, ignored []repeatedBlockIgnore) bool {
	for _, item := range ignored {
		if (item.EmployeeNumber == "" || normalizeIdentity(item.EmployeeNumber) == normalizeIdentity(number)) &&
			(item.FullName == "" || normalizeIdentity(item.FullName) == normalizeIdentity(name)) {
			return true
		}
	}
	return false
}

func (validator *Validator) resolveIdentity(ctx context.Context, batch domain.ImportBatch, values map[string]string, rowNumber int, operation domain.Operation, sheetName string) (string, *IdentityCandidate, *validationError) {
	if validator.matcher == nil || (operation != domain.OperationEmployeeMaster && operation != domain.OperationPayrollLedger) {
		return "", nil, nil
	}
	nik := strings.TrimSpace(values["nik"])
	name := strings.TrimSpace(values["full_name"])
	number := strings.TrimSpace(values["employee_number"])
	if name == "" && number == "" && nik == "" {
		return "unresolved", nil, &validationError{SheetName: sheetName, Row: rowNumber, Code: "IDENTITY_FIELDS_MISSING", Severity: "error", Message: "NIK, full_name, or employee_number is required for identity matching"}
	}
	candidates, err := validator.matcher.FindIdentityCandidates(ctx, batch.TenantID.UUID(), batch.CompanyID.UUID(), nik, name, number)
	if err != nil {
		return "unresolved", nil, &validationError{SheetName: sheetName, Row: rowNumber, Code: "IDENTITY_LOOKUP_FAILED", Severity: "error", Message: err.Error()}
	}
	for index := range candidates {
		if candidates[index].NIKMatch {
			if name != "" && normalizeIdentity(candidates[index].FullName) != normalizeIdentity(name) {
				return "conflict", nil, &validationError{SheetName: sheetName, Row: rowNumber, Code: "NIK_IDENTITY_CONFLICT", Severity: "error", Message: "NIK belongs to a different employee identity"}
			}
			candidate := candidates[index]
			if operation == domain.OperationEmployeeMaster {
				if changed := immutableMasterFieldsChanged(values, candidate.PersonalFields); len(changed) > 0 {
					return "conflict", &candidate, &validationError{SheetName: sheetName, Row: rowNumber, Field: changed[0], Code: "IMMUTABLE_EMPLOYEE_FIELD_CHANGED", Severity: "error", Message: "existing employee personal data cannot be changed through this import"}
				}
				return "matched", &candidate, &validationError{SheetName: sheetName, Row: rowNumber, Code: "EMPLOYEE_ALREADY_EXISTS", Severity: "warning", Message: "employee identity already exists; allowed historical fields will be reconciled"}
			}
			return "matched", &candidate, nil
		}
	}
	nameMatches := make([]IdentityCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if normalizeIdentity(candidate.FullName) == normalizeIdentity(name) {
			nameMatches = append(nameMatches, candidate)
		}
	}
	if len(nameMatches) == 1 {
		return "matched", &nameMatches[0], nil
	}
	if len(nameMatches) > 1 && number != "" {
		filtered := filterCandidatesByNumber(nameMatches, number)
		if len(filtered) == 1 {
			return "matched", &filtered[0], nil
		}
	}
	if len(nameMatches) > 1 {
		return "ambiguous", nil, &validationError{SheetName: sheetName, Row: rowNumber, Code: "AMBIGUOUS_EMPLOYEE_MATCH", Severity: "error", Message: "multiple employees match the supplied name"}
	}
	if number != "" {
		numberMatches := filterCandidatesByNumber(candidates, number)
		if len(numberMatches) == 1 {
			return "matched", &numberMatches[0], nil
		}
		if len(numberMatches) > 1 {
			return "ambiguous", nil, &validationError{SheetName: sheetName, Row: rowNumber, Code: "AMBIGUOUS_EMPLOYEE_NUMBER", Severity: "error", Message: "employee number matches multiple historical records"}
		}
	}
	if operation == domain.OperationPayrollLedger {
		return "unmatched", nil, &validationError{SheetName: sheetName, Row: rowNumber, Code: "EMPLOYEE_UNMATCHED", Severity: "error", Message: "no exact employee identity match was found"}
	}
	return "unmatched", nil, &validationError{SheetName: sheetName, Row: rowNumber, Code: "EMPLOYEE_NOT_FOUND_NEW_RECORD", Severity: "warning", Message: "no existing employee matched; row will create a new employee record"}
}

func immutableMasterFieldsChanged(source, current map[string]string) []string {
	allowed := map[string]struct{}{"employee_number": {}, "ptkp_code": {}, "effective_from": {}, "department": {}, "department_id": {}, "position": {}, "position_id": {}, "group": {}, "group_id": {}, "location": {}, "location_id": {}}
	changed := make([]string, 0)
	for field, value := range source {
		if _, ok := allowed[field]; ok || strings.TrimSpace(value) == "" {
			continue
		}
		if currentValue, ok := current[field]; ok && strings.TrimSpace(currentValue) != strings.TrimSpace(value) {
			changed = append(changed, field)
		}
	}
	return changed
}

func filterCandidatesByNumber(candidates []IdentityCandidate, number string) []IdentityCandidate {
	normalized := normalizeIdentity(number)
	filtered := make([]IdentityCandidate, 0, len(candidates))
	seen := make(map[uuid.UUID]struct{})
	for _, candidate := range candidates {
		if normalizeIdentity(candidate.EmployeeNumber) != normalized {
			continue
		}
		if _, exists := seen[candidate.EmployeeID]; exists {
			continue
		}
		seen[candidate.EmployeeID] = struct{}{}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func normalizeIdentity(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func (validator *Validator) persistValidationRow(ctx context.Context, batchID domain.BatchID, sheetName string, rowNumber int, values map[string]string, status string, rowIssues []validationError, candidate *IdentityCandidate) error {
	rowID, err := domain.NewImportRowID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return err
	}
	var sourceNumber *string
	if value := strings.TrimSpace(values["employee_number"]); value != "" {
		sourceNumber = &value
	}
	var sourceName *string
	if value := strings.TrimSpace(values["full_name"]); value != "" {
		sourceName = &value
	}
	issues := make([]ValidationIssue, 0, len(rowIssues))
	for _, item := range rowIssues {
		issueID, issueErr := domain.NewImportRowIssueID()
		if issueErr != nil {
			return issueErr
		}
		var fieldName *string
		if item.Field != "" {
			value := item.Field
			fieldName = &value
		}
		issues = append(issues, ValidationIssue{ID: issueID, FieldName: fieldName, ErrorCode: item.Code, Severity: item.Severity, Description: item.Message})
	}
	var employeeID, employmentID *uuid.UUID
	if candidate != nil {
		employeeID = &candidate.EmployeeID
		employmentID = candidate.EmploymentID
	}
	return validator.repository.CreateValidationRow(ctx, ValidationRow{
		ID: rowID, BatchID: batchID, SheetName: sheetName, RowNumber: rowNumber,
		SourceEmployeeNumber: sourceNumber, SourceFullName: sourceName,
		MatchStatus: status, NormalizedPayload: payload, IssueCount: len(issues),
		BlockingIssueCount: countBlockingIssues(issues), Issues: issues, EmployeeID: employeeID, EmploymentID: employmentID,
	})
}

func countBlockingIssues(issues []ValidationIssue) int {
	count := 0
	for _, issue := range issues {
		if issue.Severity == "error" {
			count++
		}
	}
	return count
}

func (validator *Validator) writeArtifacts(ctx context.Context, batch domain.ImportBatch, result validationResult) error {
	summary, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := validator.writeArtifact(ctx, batch, domain.ArtifactValidationSummary, "validation-summary.json", "json", "application/json", summary); err != nil {
		return err
	}
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	_ = writer.Write([]string{"sheet_name", "row_no", "field", "error_code", "severity", "description", "masked_value", "candidate_count"})
	for _, item := range result.Errors {
		_ = writer.Write([]string{item.SheetName, fmt.Sprintf("%d", item.Row), item.Field, item.Code, item.Severity, item.Message, "", "0"})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	if err := validator.writeArtifact(ctx, batch, domain.ArtifactValidationErrors, "validation-errors.csv", "csv", "text/csv", output.Bytes()); err != nil {
		return err
	}

	normalized, err := marshalNDJSON(result.Normalized)
	if err != nil {
		return err
	}
	if err := validator.writeArtifact(ctx, batch, domain.ArtifactNormalized, "normalized.ndjson", "ndjson", "application/x-ndjson", normalized); err != nil {
		return err
	}
	if !hasBlockingValidationErrors(result.Errors) {
		return validator.writeArtifact(ctx, batch, domain.ArtifactReadyImport, "ready-import.ndjson", "ndjson", "application/x-ndjson", normalized)
	}
	return nil
}

func countBlockingValidationErrors(issues []validationError) int {
	count := 0
	for _, issue := range issues {
		if issue.Severity == "error" {
			count++
		}
	}
	return count
}

func hasBlockingValidationErrors(issues []validationError) bool {
	return countBlockingValidationErrors(issues) > 0
}

func marshalNDJSON(rows []normalizedRow) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return nil, err
		}
	}
	return output.Bytes(), nil
}

func (validator *Validator) writeArtifact(ctx context.Context, batch domain.ImportBatch, role domain.ArtifactRole, filename, extension, mimeType string, data []byte) error {
	fileID, err := domain.NewFileObjectID()
	if err != nil {
		return err
	}
	artifactID, err := domain.NewArtifactID()
	if err != nil {
		return err
	}
	key := fmt.Sprintf("tenants/%s/companies/%s/batches/%s/%s", batch.TenantID.String(), batch.CompanyID.String(), batch.ID.String(), filename)
	info, err := validator.storage.Put(ctx, key, bytes.NewReader(data), int64(len(data)), mimeType)
	if err != nil {
		return err
	}
	provider := validator.storage.Provider()
	encryptionMode := "filesystem-private"
	if provider == "s3" {
		encryptionMode = "sse-s3"
	}
	file := domain.FileObject{ID: fileID, TenantID: batch.TenantID, CompanyID: batch.CompanyID, StorageProvider: provider, ObjectKey: key, OriginalFilename: filename, DetectedExtension: extension, DetectedMIMEType: mimeType, SizeBytes: info.SizeBytes, SHA256: info.SHA256, EncryptionMode: encryptionMode, Status: domain.FileObjectAvailable, CreatedBy: identitydomain.UserID{}, CreatedAt: validator.clock.Now().UTC(), UpdatedAt: validator.clock.Now().UTC()}
	if err := validator.repository.CreateArtifactFile(ctx, file, batch.ID, artifactID, role); err != nil {
		return err
	}
	return nil
}

func (validator *Validator) fail(ctx context.Context, batchID domain.BatchID, startedAt time.Time, err error) error {
	return validator.failWithCounts(ctx, batchID, startedAt, 0, 0, 0, 0, err)
}

func (validator *Validator) failWithCounts(ctx context.Context, batchID domain.BatchID, startedAt time.Time, total, valid, invalid, warnings int, cause error) error {
	finished := validator.clock.Now().UTC()
	_, updateErr := validator.repository.UpdateBatchStatus(ctx, BatchStatusUpdate{BatchID: batchID, Status: domain.BatchValidationFailed, ValidationStartedAt: &startedAt, ValidationFinishedAt: &finished, TotalRows: &total, ValidRows: &valid, InvalidRows: &invalid, WarningRows: &warnings})
	if updateErr != nil {
		return fmt.Errorf("%v; update failed batch: %w", cause, updateErr)
	}
	return cause
}

func nextColumns(rows RowReader) ([]string, error) {
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("input file has no header row")
	}
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	for index := range columns {
		columns[index] = normalizeColumn(columns[index])
	}
	return columns, nil
}

func validateHeader(operation domain.Operation, header []string) error {
	if len(header) == 0 {
		return errors.New("input file has an empty header row")
	}
	if operation == domain.OperationEmployeeMaster {
		for _, required := range []string{"nik", "full_name"} {
			if !containsColumn(header, required) {
				return fmt.Errorf("missing required column %q", required)
			}
		}
	}
	return nil
}

func validateEmployeeRow(operation domain.Operation, header, values []string) error {
	if operation != domain.OperationEmployeeMaster {
		return nil
	}
	row := make(map[string]string, len(header))
	for index, name := range header {
		if index < len(values) {
			row[name] = strings.TrimSpace(values[index])
		}
	}
	if len([]rune(row["nik"])) != 16 {
		return errors.New("nik must contain exactly 16 digits")
	}
	for _, character := range row["nik"] {
		if character < '0' || character > '9' {
			return errors.New("nik must contain only digits")
		}
	}
	if row["full_name"] == "" {
		return errors.New("full_name is required")
	}
	return nil
}

func rowValues(header, values []string) map[string]string {
	row := make(map[string]string, len(header))
	for index, name := range header {
		if index < len(values) {
			if isSensitiveArtifactField(name) {
				continue
			}
			row[name] = maskSensitiveImportValue(name, strings.TrimSpace(values[index]))
		}
	}
	return row
}

func isSensitiveArtifactField(field string) bool {
	switch normalizeColumn(field) {
	case "bank", "bank_account", "bank_account_name", "rekening", "account_number", "account_name", "npwp":
		return true
	default:
		return false
	}
}

func rawRowValues(header, values []string) map[string]string {
	row := make(map[string]string, len(header))
	for index, name := range header {
		if index < len(values) {
			row[name] = strings.TrimSpace(values[index])
		}
	}
	return row
}

func maskSensitiveImportValue(field, value string) string {
	if value == "" {
		return value
	}
	switch field {
	case "nik", "npwp", "bank_account", "rekening", "account_number":
		if len([]rune(value)) <= 4 {
			return "****"
		}
		runes := []rune(value)
		return strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-4:])
	default:
		return value
	}
}

func normalizeColumn(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "nama", "name":
		return "full_name"
	case "nomor_ktp", "no_ktp", "ktp":
		return "nik"
	case "no_karyawan", "nomor_karyawan", "employee_no", "employee_number":
		return "employee_number"
	case "ptkp":
		return "ptkp_code"
	case "tgl_lahir", "tanggal_lahir":
		return "birth_date"
	case "tgl_masuk", "tanggal_masuk":
		return "join_date"
	case "tgl_resign", "tanggal_resign":
		return "end_date"
	case "jenis_kelamin":
		return "gender"
	case "tel", "telepon", "no_telepon":
		return "phone"
	case "alamat":
		return "address"
	case "jenis_karyawan", "jenis_pekerjaan":
		return "employment_type"
	default:
		return value
	}
}

func containsColumn(columns []string, expected string) bool {
	for _, column := range columns {
		if column == expected {
			return true
		}
	}
	return false
}

func intPtr(value int) *int { return &value }
