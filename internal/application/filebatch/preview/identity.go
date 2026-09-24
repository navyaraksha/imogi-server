package preview

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	filedomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	identitydomain "github.com/navyaraksha/imogi/internal/domain/identity"
)

func buildIdentityMatcher(repository *memoryRepository, batchID filedomain.BatchID) (appfilebatch.IdentityMatcher, error) {
	rows, err := repository.ListValidationRows(context.Background(), batchID)
	if err != nil {
		return nil, err
	}

	identityByKey := make(map[string]appfilebatch.IdentityCandidate)
	for _, row := range rows {
		if row.BlockingIssueCount > 0 {
			continue
		}

		var values map[string]string
		if err := json.Unmarshal(row.NormalizedPayload, &values); err != nil {
			return nil, err
		}

		employeeID, err := identitydomain.NewUUIDv7()
		if err != nil {
			return nil, err
		}
		employmentID, err := identitydomain.NewUUIDv7()
		if err != nil {
			return nil, err
		}
		candidate := appfilebatch.IdentityCandidate{
			EmployeeID:     employeeID,
			EmploymentID:   uuidPtr(employmentID),
			EmployeeNumber: values["employee_number"],
			FullName:       values["full_name"],
			PersonalFields: values,
		}

		identityKey := normalizeIdentity(values["nik"]) + "|" + normalizeIdentity(values["full_name"])
		if identityKey == "|" {
			identityKey = "row|" + row.ID.UUID().String()
		}
		if existing, ok := identityByKey[identityKey]; ok {
			// Multiple master rows with the same NIK and name represent
			// employment history, not multiple people.
			if existing.EmployeeNumber == "" && candidate.EmployeeNumber != "" {
				existing.EmployeeNumber = candidate.EmployeeNumber
				existing.PersonalFields["employee_number"] = candidate.EmployeeNumber
			}
			identityByKey[identityKey] = existing
			continue
		}
		identityByKey[identityKey] = candidate
	}

	matcher := &memoryIdentityMatcher{
		byNumber: make(map[string][]appfilebatch.IdentityCandidate),
		byName:   make(map[string][]appfilebatch.IdentityCandidate),
	}
	for _, candidate := range identityByKey {
		if key := normalizeIdentity(candidate.EmployeeNumber); key != "" {
			matcher.byNumber[key] = append(matcher.byNumber[key], candidate)
		}
		if key := normalizeIdentity(candidate.FullName); key != "" {
			matcher.byName[key] = append(matcher.byName[key], candidate)
		}
	}
	return matcher, nil
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

func uuidPtr(value uuid.UUID) *uuid.UUID { return &value }

// memoryIdentityMatcher is scoped to one preview and intentionally contains no
// persistence behavior. It returns all candidates so production identity
// resolution can report ambiguity instead of silently selecting a row.
type memoryIdentityMatcher struct {
	byNumber map[string][]appfilebatch.IdentityCandidate
	byName   map[string][]appfilebatch.IdentityCandidate
}

func (matcher *memoryIdentityMatcher) FindIdentityCandidates(_ context.Context, _, _ uuid.UUID, _, name, number string) ([]appfilebatch.IdentityCandidate, error) {
	seen := make(map[uuid.UUID]struct{})
	result := make([]appfilebatch.IdentityCandidate, 0)
	candidates := append(
		append([]appfilebatch.IdentityCandidate(nil), matcher.byNumber[normalizeIdentity(number)]...),
		matcher.byName[normalizeIdentity(name)]...,
	)
	for _, candidate := range candidates {
		if _, ok := seen[candidate.EmployeeID]; ok {
			continue
		}
		seen[candidate.EmployeeID] = struct{}{}
		result = append(result, candidate)
	}
	return result, nil
}
