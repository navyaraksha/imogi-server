package preview

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	filedomain "github.com/navyaraksha/imogi/internal/domain/filebatch"
	objectstorage "github.com/navyaraksha/imogi/internal/infrastructure/objectstorage"
)

func mirrorArtifacts(ctx context.Context, repository *memoryRepository, storage *objectstorage.Filesystem, batchID filedomain.BatchID, outputDirectory string) (BatchResult, error) {
	batch, err := repository.GetBatch(ctx, batchID)
	if err != nil {
		return BatchResult{}, err
	}
	if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
		return BatchResult{}, err
	}

	artifacts, err := repository.ListArtifacts(ctx, batchID)
	if err != nil {
		return BatchResult{}, err
	}
	for _, artifact := range artifacts {
		if artifact.Role == filedomain.ArtifactInput {
			continue
		}
		input, _, err := storage.Open(ctx, artifact.FileObject.ObjectKey)
		if err != nil {
			return BatchResult{}, err
		}
		data, readErr := io.ReadAll(input)
		closeErr := input.Close()
		if readErr != nil {
			return BatchResult{}, readErr
		}
		if closeErr != nil {
			return BatchResult{}, closeErr
		}
		path := filepath.Join(outputDirectory, artifact.FileObject.OriginalFilename)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return BatchResult{}, err
		}
	}
	return BatchResult{
		BatchID:     batch.ID.String(),
		Status:      string(batch.Status),
		TotalRows:   batch.TotalRows,
		ValidRows:   batch.ValidRows,
		InvalidRows: batch.InvalidRows,
		WarningRows: batch.WarningRows,
		ArtifactDir: outputDirectory,
	}, nil
}

func writePayrollComponents(ctx context.Context, repository *memoryRepository, storage *objectstorage.Filesystem, batchID filedomain.BatchID, destination string) error {
	artifacts, err := repository.ListArtifacts(ctx, batchID)
	if err != nil {
		return err
	}
	var normalizedArtifact *appfilebatch.Artifact
	for index := range artifacts {
		if artifacts[index].Role == filedomain.ArtifactNormalized {
			normalizedArtifact = &artifacts[index]
			break
		}
	}
	if normalizedArtifact == nil {
		return errors.New("payroll normalized artifact is missing")
	}

	input, _, err := storage.Open(ctx, normalizedArtifact.FileObject.ObjectKey)
	if err != nil {
		return err
	}
	defer input.Close()
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		var row normalizedArtifactRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return err
		}
		if len(row.Components) == 0 {
			continue
		}
		output := map[string]any{
			"sheetName":       row.SheetName,
			"row":             row.Row,
			"employee_number": row.Values["employee_number"],
			"full_name":       row.Values["full_name"],
			"components":      row.Components,
		}
		if err := encoder.Encode(output); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type normalizedArtifactRow struct {
	SheetName  string             `json:"sheetName"`
	Row        int                `json:"row"`
	Values     map[string]string  `json:"values"`
	Components []previewComponent `json:"components"`
}

type previewComponent struct {
	Code   string `json:"code"`
	Type   string `json:"type"`
	Amount string `json:"amount"`
}
