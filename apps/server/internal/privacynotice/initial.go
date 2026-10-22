package privacynotice

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// InitialResult contains no campaign state or recipient data.
type InitialResult struct{}

// JSON returns the closed empty-state proof after reconciliation succeeds.
func (InitialResult) JSON() ([]byte, error) {
	output := struct {
		SchemaVersion  int    `json:"schema_version"`
		Result         string `json:"result"`
		CampaignID     string `json:"campaign_id"`
		ContentSHA256  string `json:"content_sha256"`
		CampaignCount  int    `json:"campaign_count"`
		RecipientCount int    `json:"recipient_count"`
		NoticeJobCount int    `json:"notice_job_count"`
	}{SchemaVersion: 1, Result: "initial_reconciled", CampaignID: authmail.PrivacyNoticeID, ContentSHA256: hex.EncodeToString(contentDigest())}
	data, err := json.Marshal(output)
	if err != nil || len(data)+1 > 4096 {
		return nil, ErrState
	}
	return append(data, '\n'), nil
}

// ReconcileInitial proves no notice history exists without changing business rows.
func (s *Service) ReconcileInitial(ctx context.Context) (InitialResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return InitialResult{}, ErrState
	}
	return s.reconcileInitialTx(ctx, tx)
}

func (s *Service) reconcileInitialTx(ctx context.Context, tx pgx.Tx) (result InitialResult, resultErr error) {
	defer func() {
		if rollbackErr := rollback(ctx, tx); rollbackErr != nil {
			resultErr = rollbackErr
		}
	}()
	epoch, err := s.initialEpoch()
	if err != nil {
		return result, ErrState
	}
	if boundErr := boundTransaction(ctx, tx); boundErr != nil {
		return result, ErrState
	}
	if lockErr := store.New(tx).LockPrivacyNoticeCommand(ctx); lockErr != nil {
		return result, ErrState
	}
	for _, statement := range []string{
		"LOCK TABLE privacy_notice_campaigns IN SHARE MODE",
		"LOCK TABLE privacy_notice_recipients IN SHARE MODE",
		"LOCK TABLE auth_email_jobs IN SHARE MODE",
	} {
		if _, lockErr := tx.Exec(ctx, statement); lockErr != nil {
			return result, ErrState
		}
	}
	var campaigns, recipients, jobs int64
	if queryErr := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM privacy_notice_campaigns),
		(SELECT count(*) FROM privacy_notice_recipients),
		(SELECT count(*) FROM auth_email_jobs WHERE kind='privacy_notice')`).Scan(&campaigns, &recipients, &jobs); queryErr != nil || campaigns != 0 || recipients != 0 || jobs != 0 {
		return result, ErrState
	}
	if current, authorityErr := s.initialEpoch(); authorityErr != nil || current != epoch {
		return result, ErrState
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return result, ErrState
	}
	return result, nil
}

func (s *Service) initialEpoch() (string, error) {
	epoch, err := s.epoch(true)
	if err != nil {
		return "", ErrState
	}
	for _, name := range []string{".", "epoch", "initial-epoch"} {
		if metadataErr := s.initialAuthorityMetadata(name); metadataErr != nil {
			return "", ErrState
		}
	}
	initial, err := s.authorityFile("initial-epoch")
	if err != nil || string(initial) != epoch+"\n" {
		return "", ErrState
	}
	for _, name := range []string{"send.json", "activation.json", "capture-in-progress"} {
		if absenceErr := s.initialAuthorityAbsent(name); absenceErr != nil {
			return "", ErrState
		}
	}
	marker, err := s.authorityFile("restore-reconciliation")
	if errors.Is(err, fs.ErrNotExist) {
		if absenceErr := s.initialAuthorityAbsent("restore-reconciliation"); absenceErr != nil {
			return "", ErrState
		}
	} else if err != nil || len(marker) != 0 || s.initialAuthorityMetadata("restore-reconciliation") != nil {
		return "", ErrState
	}
	return epoch, nil
}

func (s *Service) initialAuthorityAbsent(name string) error {
	var err error
	if s.authorityFS == nil {
		_, err = os.Lstat(authorityDirectory + "/" + name)
	} else {
		_, err = fs.Stat(s.authorityFS, name)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return ErrState
	}
	return nil
}

func (s *Service) initialAuthorityMetadata(name string) error {
	var info fs.FileInfo
	var err error
	if s.authorityFS == nil {
		info, err = os.Lstat(authorityDirectory + "/" + name)
	} else {
		info, err = fs.Stat(s.authorityFS, name)
	}
	if err != nil {
		return ErrState
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return ErrState
	}
	if name == "." {
		if !info.IsDir() || info.Mode() != fs.ModeDir|0755 {
			return ErrState
		}
	} else if !info.Mode().IsRegular() || info.Mode() != 0444 || stat.Nlink != 1 {
		return ErrState
	}
	return nil
}
