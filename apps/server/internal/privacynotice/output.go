package privacynotice

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"time"
)

type outcomeCounts struct {
	Unqueued  int64 `json:"unqueued"`
	Queued    int64 `json:"queued"`
	Accepted  int64 `json:"accepted"`
	Deleted   int64 `json:"deleted"`
	Failed    int64 `json:"failed"`
	Cancelled int64 `json:"cancelled"` //nolint:misspell // Exact JSON outcome spelling matches the generated store member.
}

type reasonCounts struct {
	Expired           int64 `json:"expired"`
	AttemptsExhausted int64 `json:"attempts_exhausted"`
	ProviderRejected  int64 `json:"provider_rejected"`
	PayloadInvalid    int64 `json:"payload_invalid"`
	JobMissing        int64 `json:"job_missing"`
	DeliveryFailed    int64 `json:"delivery_failed"`
}

func sumMatches(want int64, values ...int64) bool {
	if want < 0 {
		return false
	}
	var sum int64
	for _, value := range values {
		if value < 0 || sum > math.MaxInt64-value {
			return false
		}
		sum += value
	}
	return sum == want
}

// JSON returns only the closed aggregate wire object, never the store row.
func (r Report) JSON(activation bool) ([]byte, error) {
	c, t := r.Campaign, r.Totals
	if !validCampaign(c) || c.ExpectedCount < 0 || c.ActivationCount < 0 || !sumMatches(t.Actual, t.Unqueued, t.Queued, t.Accepted, t.Deleted, t.Failed, t.Cancelled) || !sumMatches(t.Failed, t.Expired, t.AttemptsExhausted, t.ProviderRejected, t.PayloadInvalid, t.JobMissing, t.DeliveryFailed) || !sumMatches(c.RecoveryCount, c.RecoveryExpiredCount, c.RecoveryAttemptsExhaustedCount, c.RecoveryProviderRejectedCount, c.RecoveryPayloadInvalidCount, c.RecoveryJobMissingCount, c.RecoveryDeliveryFailedCount) { //nolint:misspell // Generated store member follows the fixed database spelling.
		return nil, ErrState
	}
	result := "blocked"
	switch r.Code {
	case 0:
		if c.State != "sealed" && c.State != "activated" {
			return nil, ErrState
		}
		result = "ready"
		if activation {
			if c.State != "activated" {
				return nil, ErrState
			}
			result = "activated"
		}
	case 2:
		if c.State != "capturing" && c.State != "sealed" {
			return nil, ErrState
		}
		result = "pending"
	case 1:
	default:
		return nil, ErrState
	}
	if r.result != "" {
		if (r.result == "sealed" && r.Code == 0 && c.State == "sealed") || (r.result == "queue_full" && r.Code == 2 && c.State == "sealed") {
			result = r.result
		} else {
			return nil, ErrState
		}
	}
	var last *time.Time
	if t.Accepted > 0 {
		last = &t.LastAcceptedAt
	}
	timeValue := func(value *time.Time) (*string, error) {
		if value == nil {
			return nil, nil
		}
		formatted := formatTime(*value)
		parsed, err := parseTime(formatted)
		if err != nil || !parsed.Equal(*value) {
			return nil, ErrState
		}
		return &formatted, nil
	}
	values := make([]*string, 0, 6)
	for _, value := range []*time.Time{&c.CutoffAt, c.SealedAt, c.FirstActivatedAt, c.ActivatedAt, c.AbortedAt, last} {
		text, err := timeValue(value)
		if err != nil {
			return nil, err
		}
		values = append(values, text)
	}
	output := struct {
		SchemaVersion    int           `json:"schema_version"`
		Result           string        `json:"result"`
		CampaignID       string        `json:"campaign_id"`
		ContentSHA256    string        `json:"content_sha256"`
		CutoffAt         *string       `json:"cutoff_at"`
		ExpectedCount    int64         `json:"expected_count"`
		State            string        `json:"state"`
		ActualCount      int64         `json:"actual_count"`
		Counts           outcomeCounts `json:"counts"`
		FailureCounts    reasonCounts  `json:"failure_counts"`
		RecoveryCount    int64         `json:"recovery_count"`
		RecoveryCounts   reasonCounts  `json:"recovery_counts"`
		ActivationCount  int64         `json:"activation_count"`
		SealedAt         *string       `json:"sealed_at"`
		FirstActivatedAt *string       `json:"first_activated_at"`
		ActivatedAt      *string       `json:"activated_at"`
		AbortedAt        *string       `json:"aborted_at"`
		LastAcceptedAt   *string       `json:"last_accepted_at"`
	}{1, result, c.ID, hex.EncodeToString(c.ContentSha256), values[0], c.ExpectedCount, c.State, t.Actual,
		outcomeCounts{t.Unqueued, t.Queued, t.Accepted, t.Deleted, t.Failed, t.Cancelled}, reasonCounts{t.Expired, t.AttemptsExhausted, t.ProviderRejected, t.PayloadInvalid, t.JobMissing, t.DeliveryFailed}, c.RecoveryCount, reasonCounts{c.RecoveryExpiredCount, c.RecoveryAttemptsExhaustedCount, c.RecoveryProviderRejectedCount, c.RecoveryPayloadInvalidCount, c.RecoveryJobMissingCount, c.RecoveryDeliveryFailedCount}, c.ActivationCount, values[1], values[2], values[3], values[4], values[5]} //nolint:misspell // Generated store member follows the fixed database spelling.
	data, err := json.Marshal(output)
	if err != nil || len(data)+1 > 4096 {
		return nil, ErrState
	}
	return append(data, '\n'), nil
}
