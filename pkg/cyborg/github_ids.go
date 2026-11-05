package cyborg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	orgdatacore "github.com/openshift-eng/cyborg-data/go"
)

// NormalizeGitHubIDs wraps a Cyborg data source, lowercasing its GitHub index
// keys on every load, including watcher reloads.
func NormalizeGitHubIDs(source orgdatacore.DataSource) orgdatacore.DataSource {
	return &normalizedGitHubIDs{DataSource: source}
}

type normalizedGitHubIDs struct {
	orgdatacore.DataSource
}

func (s *normalizedGitHubIDs) Load(ctx context.Context) (io.ReadCloser, error) {
	reader, err := s.DataSource.Load(ctx)
	if err != nil {
		return nil, err
	}
	var data orgdatacore.Data
	decodeErr := json.NewDecoder(reader).Decode(&data)
	closeErr := reader.Close()
	if decodeErr != nil {
		return nil, fmt.Errorf("decode Cyborg data for GitHub ID normalization: %w", decodeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close Cyborg data reader: %w", closeErr)
	}

	index := data.Indexes.GitHubIDMappings.GitHubIDToUID
	if index != nil {
		normalized := make(map[string]string, len(index))
		for login, uid := range index {
			key := strings.ToLower(login)
			if existing, ok := normalized[key]; ok && existing != uid {
				return nil, fmt.Errorf("conflicting Cyborg employees for a case-insensitive GitHub login")
			}
			normalized[key] = uid
		}
		data.Indexes.GitHubIDMappings.GitHubIDToUID = normalized
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode normalized Cyborg data: %w", err)
	}
	return io.NopCloser(bytes.NewReader(encoded)), nil
}
