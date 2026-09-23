package pagination

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func EncodeUUID(id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String()))
}

func DecodeUUID(value string) (uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("decode cursor: %w", err)
	}
	id, err := uuid.Parse(strings.TrimSpace(string(decoded)))
	if err != nil {
		return uuid.Nil, fmt.Errorf("decode cursor UUID: %w", err)
	}
	return id, nil
}
