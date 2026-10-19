package privacynotice

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const initialFixtureEpoch = "79446cce-0778-4509-9bd1-b4c13ef728bb\n"

func initialFiles() fstest.MapFS {
	return fstest.MapFS{
		".":                      {Mode: fs.ModeDir | 0755, Sys: &syscall.Stat_t{}},
		"epoch":                  {Data: []byte(initialFixtureEpoch), Mode: 0444, Sys: &syscall.Stat_t{Nlink: 1}},
		"initial-epoch":          {Data: []byte(initialFixtureEpoch), Mode: 0444, Sys: &syscall.Stat_t{Nlink: 1}},
		"restore-reconciliation": {Mode: 0444, Sys: &syscall.Stat_t{Nlink: 1}},
	}
}

func TestInitialAuthorityRefusals(t *testing.T) {
	for _, name := range []string{"epoch", "initial-epoch"} {
		for _, bad := range []string{"missing", "mode", "symlink", "directory", "malformed", "rotated"} {
			t.Run(name+"/"+bad, func(t *testing.T) {
				files := initialFiles()
				switch bad {
				case "missing":
					delete(files, name)
				case "mode":
					files[name].Mode = 0644
				case "symlink":
					files[name].Mode = fs.ModeSymlink | 0444
				case "directory":
					files[name].Mode = fs.ModeDir | 0444
				case "malformed":
					files[name].Data = []byte("private fixture invalid epoch")
				case "rotated":
					files[name].Data = []byte("79446cce-0778-4509-9bd1-b4c13ef728bc\n")
				}
				if _, err := (&Service{authorityFS: files}).initialEpoch(); !errors.Is(err, ErrState) {
					t.Fatal("invalid initial authority accepted")
				}
			})
		}
	}
	for _, name := range []string{"send.json", "activation.json", "capture-in-progress", "restore-reconciliation"} {
		t.Run(name, func(t *testing.T) {
			files := initialFiles()
			files[name] = &fstest.MapFile{Data: []byte("private fixture invalid receipt"), Mode: 0444}
			if _, err := (&Service{authorityFS: files}).initialEpoch(); !errors.Is(err, ErrState) {
				t.Fatal("receipt or malformed marker accepted")
			}
		})
	}
}

func TestInitialAuthorityMetadataRefusals(t *testing.T) {
	for _, name := range []string{".", "epoch", "initial-epoch", "restore-reconciliation"} {
		for _, bad := range []string{"owner", "group", "mode", "links", "metadata"} {
			if name == "." && bad == "links" {
				continue
			}
			t.Run(name+"/"+bad, func(t *testing.T) {
				files := initialFiles()
				switch bad {
				case "owner":
					files[name].Sys = &syscall.Stat_t{Uid: 1, Nlink: 1}
				case "group":
					files[name].Sys = &syscall.Stat_t{Gid: 1, Nlink: 1}
				case "mode":
					files[name].Mode |= 0020
				case "links":
					files[name].Sys = &syscall.Stat_t{Nlink: 2}
				case "metadata":
					files[name].Sys = nil
				}
				if _, err := (&Service{authorityFS: files}).initialEpoch(); !errors.Is(err, ErrState) {
					t.Fatal("unsafe authority metadata accepted")
				}
			})
		}
	}
}

type initialRow struct {
	counts [3]int64
	err    error
}

func (r initialRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.counts {
		*dest[i].(*int64) = value
	}
	return nil
}

type initialTx struct {
	pgx.Tx
	statements            []string
	row                   initialRow
	execErr, commitErr    error
	beforeQuery           func()
	committed, rolledBack bool
}

func (tx *initialTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.statements = append(tx.statements, sql)
	return pgconn.CommandTag{}, tx.execErr
}
func (tx *initialTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.statements = append(tx.statements, sql)
	if tx.beforeQuery != nil {
		tx.beforeQuery()
	}
	return tx.row
}
func (tx *initialTx) Commit(context.Context) error   { tx.committed = true; return tx.commitErr }
func (tx *initialTx) Rollback(context.Context) error { tx.rolledBack = true; return nil }

func TestInitialTransactionRefusesWithoutSuccess(t *testing.T) {
	for _, failure := range []string{"campaign", "recipient", "job", "query", "lock", "commit", "rotated", "rotated together", "receipt", "capture", "marker"} {
		t.Run(failure, func(t *testing.T) {
			files := initialFiles()
			tx := &initialTx{}
			switch failure {
			case "campaign":
				tx.row.counts[0] = 1
			case "recipient":
				tx.row.counts[1] = 1
			case "job":
				tx.row.counts[2] = 1
			case "query":
				tx.row.err = errors.New("private fixture query failure")
			case "lock":
				tx.execErr = errors.New("private fixture lock failure")
			case "commit":
				tx.commitErr = errors.New("private fixture unknown commit")
			default:
				tx.beforeQuery = func() {
					switch failure {
					case "rotated", "rotated together":
						files["epoch"].Data = []byte("79446cce-0778-4509-9bd1-b4c13ef728bc\n")
						if failure == "rotated together" {
							files["initial-epoch"].Data = files["epoch"].Data
						}
					case "receipt":
						files["send.json"] = &fstest.MapFile{Mode: 0444}
					case "capture":
						files["capture-in-progress"] = &fstest.MapFile{Mode: 0444}
					case "marker":
						files["restore-reconciliation"].Data = []byte("invalid")
					}
				}
			}
			if _, err := (&Service{authorityFS: files}).reconcileInitialTx(t.Context(), tx); !errors.Is(err, ErrState) {
				t.Fatal("initial transaction returned success on refusal")
			}
			if !tx.rolledBack || (failure != "commit" && tx.committed) {
				t.Fatal("invalid proof committed or skipped cleanup")
			}
		})
	}
}

func TestInitialTransactionLocksAndClosedResult(t *testing.T) {
	for _, marked := range []bool{false, true} {
		files := initialFiles()
		if !marked {
			delete(files, "restore-reconciliation")
		}
		tx := &initialTx{}
		result, err := (&Service{authorityFS: files}).reconcileInitialTx(t.Context(), tx)
		if err != nil || !tx.committed || !tx.rolledBack {
			t.Fatal("empty initial proof failed")
		}
		if len(tx.statements) != 7 || !strings.Contains(tx.statements[2], "pg_advisory_xact_lock") {
			t.Fatal("campaign lock not acquired first")
		}
		for i, table := range []string{"privacy_notice_campaigns", "privacy_notice_recipients", "auth_email_jobs"} {
			if tx.statements[i+3] != "LOCK TABLE "+table+" IN SHARE MODE" {
				t.Fatal("table lock order differs")
			}
		}
		data, encodeErr := result.JSON()
		fields, parseErr := strictObject(data)
		if encodeErr != nil || parseErr != nil || len(fields) != 7 || len(data) > 4096 || data[len(data)-1] != '\n' {
			t.Fatal("initial result is not closed bounded JSON")
		}
		for _, name := range []string{"campaign_count", "recipient_count", "notice_job_count"} {
			if string(fields[name]) != "0" {
				t.Fatal("initial count is not integer zero")
			}
		}
	}
}

func TestInitialReconciliationEmptyAndOrdinaryMail(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		ctx, s := noticeTestService(t)
		s.authorityFS = initialFiles()
		if ordinary {
			if _, err := s.pool.Exec(ctx, `INSERT INTO users(email,name) VALUES ('fixture@example.com','Fixture'); INSERT INTO auth_email_jobs(kind,state,user_id,created_at,expires_at,sent_at,attempts) SELECT 'password_changed','sent',id,now(),now()+interval '1 hour',now(),1 FROM users`); err != nil {
				t.Fatal("ordinary mail fixture failed")
			}
		}
		for range 2 {
			if _, err := s.ReconcileInitial(ctx); err != nil {
				t.Fatal("empty notice proof refused ordinary mail or repeat")
			}
		}
		var unchanged bool
		if s.pool.QueryRow(ctx, `SELECT (SELECT count(*)=0 FROM privacy_notice_campaigns) AND (SELECT count(*)=0 FROM privacy_notice_recipients) AND (SELECT count(*)=$1 FROM auth_email_jobs)`, map[bool]int{false: 0, true: 1}[ordinary]).Scan(&unchanged) != nil || !unchanged {
			t.Fatal("initial proof changed business rows")
		}
	}
}

func TestInitialMarkerStillBlocksCampaignCommands(t *testing.T) {
	ctx, s := noticeTestService(t)
	s.authorityFS = initialFiles()
	for _, action := range []string{"prepare", "enqueue", "status", "activate"} {
		report, err := s.Run(ctx, action)
		if err == nil && report.Code != 1 {
			t.Fatal("initial marker allowed campaign action")
		}
	}
	var empty bool
	if s.pool.QueryRow(ctx, `SELECT count(*)=0 FROM privacy_notice_campaigns`).Scan(&empty) != nil || !empty {
		t.Fatal("marked command changed campaign state")
	}
}

func TestInitialReconciliationRejectsCampaignAndEveryJobState(t *testing.T) {
	for _, state := range []string{"capturing", "sealed", "activated", "aborted", "retired"} {
		t.Run(state, func(t *testing.T) {
			ctx, s := noticeTestService(t)
			s.authorityFS = initialFiles()
			if _, err := s.pool.Exec(ctx, `INSERT INTO privacy_notice_campaigns(id,content_sha256,cutoff_at,expected_count,state,sealed_at,first_activated_at,activation_count,activated_at,accepted_count,deleted_count,aborted_at,retired_at) VALUES ('crowdsec-postgres-paths-v1',$1,now(),0,$2,now(),CASE WHEN $2='activated' THEN now() END,CASE WHEN $2='activated' THEN 1 ELSE 0 END,CASE WHEN $2='activated' THEN now() END,0,0,CASE WHEN $2='aborted' THEN now() END,CASE WHEN $2='retired' THEN now() END)`, contentDigest(), state); err != nil {
				t.Fatal("campaign fixture failed")
			}
			if _, err := s.ReconcileInitial(ctx); err == nil {
				t.Fatal("existing campaign reconciled")
			}
		})
	}
	for _, state := range []string{"pending", "leased", "sent", "terminal"} {
		t.Run(state, func(t *testing.T) {
			ctx, s := noticeTestService(t)
			s.authorityFS = initialFiles()
			if _, err := s.pool.Exec(ctx, `INSERT INTO users(email,name) VALUES ('fixture@example.com','Fixture'); INSERT INTO auth_email_jobs(kind,state,user_id,key_id,nonce,ciphertext,expires_at,next_attempt_at) SELECT 'privacy_notice','pending',id,'fixture',decode(repeat('00',12),'hex'),decode('00','hex'),now()+interval '1 hour',now() FROM users`); err != nil {
				t.Fatal("notice job fixture failed")
			}
			updates := map[string]string{
				"pending":  `UPDATE auth_email_jobs SET attempts=0`,
				"leased":   `UPDATE auth_email_jobs SET state='leased',attempts=1,next_attempt_at=NULL,lease_owner='fixture',lease_expires_at=now()+interval '30 seconds'`,
				"sent":     `UPDATE auth_email_jobs SET state='sent',attempts=1,next_attempt_at=NULL,key_id=NULL,nonce=NULL,ciphertext=NULL,sent_at=now()`,
				"terminal": `UPDATE auth_email_jobs SET state='terminal',next_attempt_at=NULL,key_id=NULL,nonce=NULL,ciphertext=NULL,terminal_at=now()`,
			}
			if _, err := s.pool.Exec(ctx, updates[state]); err != nil {
				t.Fatal("notice job state fixture failed")
			}
			if _, err := s.ReconcileInitial(ctx); err == nil {
				t.Fatal("existing notice job reconciled")
			}
		})
	}
}

func TestInitialReconciliationRejectsOrphanRecipientsAndMissingSchema(t *testing.T) {
	for _, fixture := range []string{"recipient", "campaign table", "recipient table", "job table"} {
		t.Run(fixture, func(t *testing.T) {
			ctx, s := noticeTestService(t)
			s.authorityFS = initialFiles()
			statements := map[string]string{
				"recipient":       `ALTER TABLE privacy_notice_recipients DROP CONSTRAINT privacy_notice_recipients_campaign_id_fkey; INSERT INTO privacy_notice_recipients(id,campaign_id,job_id,state) VALUES ('79446cce-0778-4509-9bd1-b4c13ef728bb','crowdsec-postgres-paths-v1','79446cce-0778-4509-9bd1-b4c13ef728bc','unqueued')`,
				"campaign table":  `DROP TABLE privacy_notice_campaigns CASCADE`,
				"recipient table": `DROP TABLE privacy_notice_recipients CASCADE`,
				"job table":       `DROP TABLE auth_email_jobs CASCADE`,
			}
			if _, err := s.pool.Exec(ctx, statements[fixture]); err != nil {
				t.Fatal("invalid schema fixture failed")
			}
			if _, err := s.ReconcileInitial(ctx); !errors.Is(err, ErrState) {
				t.Fatal("invalid schema or orphan recipient reconciled")
			}
		})
	}
}

func TestInitialReconciliationAppRoleAndMissingGrants(t *testing.T) {
	for _, granted := range []bool{false, true} {
		ctx, s := noticeTestService(t)
		s.authorityFS = initialFiles()
		if !granted {
			if _, err := s.pool.Exec(ctx, `REVOKE ALL ON privacy_notice_recipients FROM aboutme_app`); err != nil {
				t.Fatal("grant refusal fixture failed")
			}
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatal("app role transaction failed")
		}
		t.Cleanup(func() {
			if rollback(ctx, tx) != nil {
				t.Error("app role cleanup failed")
			}
		})
		if _, roleErr := tx.Exec(ctx, `SET LOCAL ROLE aboutme_app`); roleErr != nil {
			t.Fatal("app role fixture failed")
		}
		_, reconcileErr := s.reconcileInitialTx(ctx, tx)
		if (granted && reconcileErr != nil) || (!granted && !errors.Is(reconcileErr, ErrState)) {
			t.Fatal("initial proof ignored app role grants")
		}
	}
}

func TestInitialReconciliationLockContentionAndTimeout(t *testing.T) {
	for _, lock := range []string{"advisory", "table"} {
		t.Run(lock, func(t *testing.T) {
			ctx, s := noticeTestService(t)
			s.authorityFS = initialFiles()
			tx, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal("blocker transaction failed")
			}
			t.Cleanup(func() {
				if rollback(ctx, tx) != nil {
					t.Error("blocker cleanup failed")
				}
			})
			if lock == "advisory" {
				if lockErr := store.New(tx).LockPrivacyNoticeCommand(ctx); lockErr != nil {
					t.Fatal("advisory blocker failed")
				}
			} else if _, lockErr := tx.Exec(ctx, `LOCK TABLE auth_email_jobs IN ROW EXCLUSIVE MODE`); lockErr != nil {
				t.Fatal("table blocker failed")
			}
			limited, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			if _, reconcileErr := s.ReconcileInitial(limited); !errors.Is(reconcileErr, ErrState) {
				t.Fatal("contended proof did not refuse before deadline")
			}
		})
	}
}
