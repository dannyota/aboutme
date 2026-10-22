package privacynotice

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const authorityDirectory = "/var/lib/aboutme/privacy-notice"
const wireTimeFormat = "2006-01-02T15:04:05.000000Z"

type receipt struct {
	epoch     string
	cutoff    time.Time
	expected  int64
	issued    time.Time
	accepted  int64
	deleted   int64
	activated time.Time
}

func formatTime(value time.Time) string { return value.UTC().Format(wireTimeFormat) }

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(wireTimeFormat, value)
	if err != nil || parsed.Year() < 1 || parsed.Year() > 9999 || formatTime(parsed) != value {
		return time.Time{}, ErrState
	}
	return parsed, nil
}

func countValue(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || (len(raw) > 1 && raw[0] == '0') {
		return 0, ErrState
	}
	for _, value := range raw {
		if value < '0' || value > '9' {
			return 0, ErrState
		}
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, ErrState
	}
	return value, nil
}

func strictObject(data []byte) (map[string]json.RawMessage, error) {
	if len(data) > 4096 || !utf8.Valid(data) || !validUnicodeEscapes(data) || bytes.HasPrefix(data, []byte{239, 187, 191}) {
		return nil, ErrState
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, ErrState
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		key, keyErr := decoder.Token()
		if keyErr != nil {
			return nil, ErrState
		}
		name, ok := key.(string)
		if !ok {
			return nil, ErrState
		}
		if _, exists := fields[name]; exists {
			return nil, ErrState
		}
		var raw json.RawMessage
		if decodeErr := decoder.Decode(&raw); decodeErr != nil || bytes.Equal(raw, []byte("null")) {
			return nil, ErrState
		}
		fields[name] = raw
	}
	last, lastErr := decoder.Token()
	if lastErr != nil || last != json.Delim('}') {
		return nil, ErrState
	}
	if _, tailErr := decoder.Token(); !errors.Is(tailErr, io.EOF) {
		return nil, ErrState
	}
	return fields, nil
}

func validUnicodeEscapes(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		value, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value < 0xd800 || value > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, lowErr := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if lowErr != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

func parseReceipt(data []byte, activation bool) (receipt, error) {
	fields, err := strictObject(data)
	if err != nil {
		return receipt{}, err
	}
	allowed := []string{"schema_version", "campaign_id", "content_sha256", "epoch", "cutoff_at", "expected_count", "issued_at"}
	if activation {
		allowed = []string{"schema_version", "campaign_id", "content_sha256", "epoch", "cutoff_at", "expected_count", "accepted_count", "deleted_count", "activated_at"}
	}
	if len(fields) != len(allowed) {
		return receipt{}, ErrState
	}
	for _, name := range allowed {
		if _, ok := fields[name]; !ok {
			return receipt{}, ErrState
		}
	}
	stringField := func(name string) (string, error) {
		var value string
		if decodeErr := json.Unmarshal(fields[name], &value); decodeErr != nil {
			return "", ErrState
		}
		return value, nil
	}
	version, err := countValue(fields["schema_version"])
	if err != nil || version != 1 {
		return receipt{}, ErrState
	}
	campaign, err := stringField("campaign_id")
	if err != nil || campaign != authmail.PrivacyNoticeID {
		return receipt{}, ErrState
	}
	digest, err := stringField("content_sha256")
	if err != nil || digest != hex.EncodeToString(contentDigest()) {
		return receipt{}, ErrState
	}
	epoch, err := stringField("epoch")
	if err != nil || !validEpoch(epoch) {
		return receipt{}, ErrState
	}
	cutoffText, err := stringField("cutoff_at")
	if err != nil {
		return receipt{}, ErrState
	}
	cutoff, err := parseTime(cutoffText)
	if err != nil {
		return receipt{}, ErrState
	}
	expected, err := countValue(fields["expected_count"])
	if err != nil {
		return receipt{}, ErrState
	}
	result := receipt{epoch: epoch, cutoff: cutoff, expected: expected}
	if !activation {
		text, textErr := stringField("issued_at")
		if textErr != nil {
			return receipt{}, ErrState
		}
		result.issued, err = parseTime(text)
	} else {
		result.accepted, err = countValue(fields["accepted_count"])
		if err != nil {
			return receipt{}, ErrState
		}
		result.deleted, err = countValue(fields["deleted_count"])
		if err != nil || result.accepted > math.MaxInt64-result.deleted || result.accepted+result.deleted != expected {
			return receipt{}, ErrState
		}
		text, textErr := stringField("activated_at")
		if textErr != nil {
			return receipt{}, ErrState
		}
		result.activated, err = parseTime(text)
	}
	if err != nil {
		return receipt{}, ErrState
	}
	return result, nil
}

func validEpoch(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value && id.Version() == 4 && id.Variant() == uuid.RFC4122
}

func (s *Service) files() fs.FS {
	if s.authorityFS != nil {
		return s.authorityFS
	}
	return os.DirFS(authorityDirectory)
}

func (s *Service) authorityFile(name string) ([]byte, error) {
	if s.authorityFS == nil {
		info, err := os.Lstat(authorityDirectory + "/" + name)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, ErrState
		}
	}
	info, err := fs.Stat(s.files(), name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0444 || info.Size() > 4096 {
		return nil, ErrState
	}
	file, err := s.files().Open(name)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 4096 {
		return nil, ErrState
	}
	return data, nil
}

func (s *Service) epoch(allowRestore bool) (string, error) {
	info, err := fs.Stat(s.files(), ".")
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0755 {
		return "", ErrState
	}
	if s.authorityFS == nil {
		root, rootErr := os.Lstat(authorityDirectory)
		if rootErr != nil || root.Mode()&fs.ModeSymlink != 0 {
			return "", ErrState
		}
	}
	if !allowRestore {
		if _, markerErr := fs.Stat(s.files(), "restore-reconciliation"); !errors.Is(markerErr, fs.ErrNotExist) {
			return "", ErrState
		}
	}
	data, err := s.authorityFile("epoch")
	if err != nil || len(data) != 37 || data[36] != '\n' || !validEpoch(string(data[:36])) {
		return "", ErrState
	}
	return string(data[:36]), nil
}

func validCampaignTimes(c store.PrivacyNoticeCampaign, now time.Time) bool {
	for _, value := range []*time.Time{&c.CutoffAt, c.SealedAt, c.FirstActivatedAt, c.ActivatedAt, c.AbortedAt, c.RetiredAt, c.PurgedAt, c.LastAcceptedAt} {
		if value == nil {
			continue
		}
		parsed, err := parseTime(formatTime(*value))
		if err != nil || !parsed.Equal(*value) || value.After(now) {
			return false
		}
	}
	if c.SealedAt == nil {
		return c.State == "capturing"
	}
	if c.ActivatedAt != nil && (c.ActivatedAt.Before(*c.SealedAt) || (c.FirstActivatedAt != nil && c.ActivatedAt.Before(*c.FirstActivatedAt)) || (c.LastAcceptedAt != nil && c.ActivatedAt.Before(*c.LastAcceptedAt))) {
		return false
	}
	return !c.SealedAt.Before(c.CutoffAt) && (c.FirstActivatedAt == nil || !c.FirstActivatedAt.Before(*c.SealedAt))
}

func (s *Service) validateHost(report Report, needSend bool) bool {
	epoch, err := s.epoch(false)
	c := report.Campaign
	if err != nil || !validCampaign(c) || !validCampaignTimes(c, s.now()) {
		return false
	}
	if report.Totals.Accepted > 0 && report.Totals.LastAcceptedAt.After(s.now()) {
		return false
	}
	for _, activation := range []bool{false, true} {
		name := "send.json"
		if activation {
			name = "activation.json"
		}
		data, readErr := s.authorityFile(name)
		if errors.Is(readErr, fs.ErrNotExist) {
			if needSend && !activation {
				return false
			}
			continue
		}
		if readErr != nil {
			return false
		}
		r, parseErr := parseReceipt(data, activation)
		if parseErr != nil || r.epoch != epoch || !r.cutoff.Equal(c.CutoffAt) || r.expected != c.ExpectedCount || c.SealedAt == nil {
			return false
		}
		if !activation {
			if c.State != "sealed" || r.issued.Before(*c.SealedAt) || r.issued.After(s.now()) {
				return false
			}
		} else {
			if c.State != "activated" || c.ActivatedAt == nil || c.AcceptedCount == nil || c.DeletedCount == nil || r.accepted != *c.AcceptedCount || r.deleted != *c.DeletedCount || r.accepted != report.Totals.Accepted || r.deleted != report.Totals.Deleted || !r.activated.Equal(*c.ActivatedAt) || r.activated.Before(*c.SealedAt) || r.activated.Before(report.Totals.LastAcceptedAt) || r.activated.After(s.now()) {
				return false
			}
		}
	}
	return true
}

// AuthorizeSend reads current host files and checks the sealed campaign.
func (s *Service) AuthorizeSend(ctx context.Context, q *store.Queries) bool {
	report, err := readReport(ctx, q)
	if err != nil || report.Campaign.State != "sealed" || report.Code == 1 || report.Totals.Actual != report.Campaign.ExpectedCount {
		return false
	}
	return s.validateHost(report, true)
}

func (s *Service) hostStateValid(report Report) bool { return s.validateHost(report, false) }

func (s *Service) recoveryFenced() bool {
	if _, err := s.epoch(true); err != nil {
		return false
	}
	for _, name := range []string{"send.json", "activation.json"} {
		if _, err := s.authorityFile(name); !errors.Is(err, fs.ErrNotExist) {
			return false
		}
	}
	return true
}
