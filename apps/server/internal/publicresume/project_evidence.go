package publicresume

import (
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"danny.vn/aboutme/apps/server/internal/project"
)

const maxEvidenceTextCharacters = 200

var errEvidence = errors.New("public project evidence is malformed")

type PublicEvidence struct {
	Kind           string
	Title          string
	URL            string
	RepositoryName string
	ItemDate       time.Time
	CheckedAt      time.Time
	Confirmed      bool
}

type storedEvidence struct {
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	RepositoryName string `json:"repositoryName"`
	ItemDate       string `json:"itemDate"`
	CheckedAt      string `json:"checkedAt"`
	Confirmed      bool   `json:"confirmed"`
}

func (p ProjectRead) pageEvidence(raw json.RawMessage) ([]PublicEvidence, error) {
	if !p.evidence {
		return []PublicEvidence{}, nil
	}
	var stored []storedEvidence
	if len(raw) == 0 || json.Unmarshal(raw, &stored) != nil || stored == nil || len(stored) > project.MaxEvidence {
		return nil, errEvidence
	}
	items := make([]PublicEvidence, 0, len(stored))
	for _, item := range stored {
		itemDate, itemDateOK := evidenceInstant(item.ItemDate)
		checkedAt, checkedAtOK := evidenceInstant(item.CheckedAt)
		if !project.ValidEvidenceURL(item.Kind, item.URL) || !evidenceText(item.Title) || !evidenceText(item.RepositoryName) || !itemDateOK || !checkedAtOK {
			return nil, errEvidence
		}
		items = append(items, PublicEvidence{
			Kind: item.Kind, Title: item.Title, URL: item.URL, RepositoryName: item.RepositoryName,
			ItemDate: itemDate, CheckedAt: checkedAt, Confirmed: item.Confirmed,
		})
	}
	return items, nil
}

func evidenceText(text string) bool {
	return text != "" && utf8.ValidString(text) && utf8.RuneCountInString(text) <= maxEvidenceTextCharacters
}

func evidenceInstant(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	utc := parsed.UTC()
	return utc, utc.Year() >= 1 && utc.Year() <= 9999
}
