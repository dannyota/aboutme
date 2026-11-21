package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	schema "danny.vn/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	CheckpointRetention = 30 * 24 * time.Hour
	checkpointIdle      = 10 * time.Minute
	checkpointLifetime  = time.Hour
)

type CheckpointWrite struct {
	GrantID *uuid.UUID
	Cause   string
	Force   bool
}

type CheckpointSession struct {
	Actor       *string
	StartedAt   *time.Time
	LastWriteAt *time.Time
}

type CheckpointState struct {
	Title         string             `json:"title"`
	Lng           *string            `json:"lng"`
	SchemaVersion int32              `json:"schemaVersion"`
	Document      CheckpointDocument `json:"document"`
}

type CheckpointDocument struct {
	PersonalDetails schema.PersonalDetails    `json:"personalDetails"`
	Content         map[string]schema.Section `json:"content"`
	Customization   schema.Customization      `json:"customization"`
}

type CheckpointInput struct {
	ResumeID uuid.UUID
	UserID   uuid.UUID
	Write    CheckpointWrite
	Before   CheckpointState
	After    CheckpointState
	Session  CheckpointSession
	Now      time.Time
}

func checkpointState(title string, lng *string, doc schema.Resume, version int32) CheckpointState {
	doc.PersonalDetails.Photo = nil
	return CheckpointState{Title: title, Lng: lng, SchemaVersion: version, Document: CheckpointDocument{
		PersonalDetails: doc.PersonalDetails, Content: doc.Content, Customization: doc.Customization,
	}}
}

func (s CheckpointState) ResumeDocument() schema.Resume {
	return schema.Resume{SchemaVersion: int64(s.SchemaVersion), PersonalDetails: s.Document.PersonalDetails,
		Content: s.Document.Content, Customization: s.Document.Customization}
}

func checkpointJSON(state CheckpointState) ([]byte, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err = dec.Decode(&value); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err = enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte{'\n'}), nil
}

func CheckpointTx(ctx context.Context, q *store.Queries, input CheckpointInput) (CheckpointSession, error) {
	input.Before.Document.PersonalDetails.Photo = nil
	input.After.Document.PersonalDetails.Photo = nil
	before, err := checkpointJSON(input.Before)
	if err != nil {
		return CheckpointSession{}, fmt.Errorf("resume: encode prior checkpoint: %w", err)
	}
	if !input.Write.Force {
		after, encodeErr := checkpointJSON(input.After)
		if encodeErr != nil {
			return CheckpointSession{}, fmt.Errorf("resume: encode resulting checkpoint: %w", encodeErr)
		}
		if bytes.Equal(before, after) {
			return input.Session, nil
		}
	}

	actor := "owner"
	if input.Write.GrantID != nil {
		actor = "grant:" + input.Write.GrantID.String()
	}
	started := input.Session.StartedAt
	if input.Write.Force || input.Session.Actor == nil || *input.Session.Actor != actor ||
		input.Session.LastWriteAt == nil || input.Now.Sub(*input.Session.LastWriteAt) > checkpointIdle ||
		started == nil || input.Now.Sub(*started) > checkpointLifetime {
		byteSize := len(before)
		if byteSize > math.MaxInt32 {
			return CheckpointSession{}, errors.New("resume: checkpoint exceeds stored size range")
		}
		if err = insertCheckpoint(ctx, q, input, int32(byteSize)); err != nil {
			return CheckpointSession{}, err
		}
		started = &input.Now
	}
	return CheckpointSession{Actor: &actor, StartedAt: started, LastWriteAt: &input.Now}, nil
}

func insertCheckpoint(ctx context.Context, q *store.Queries, input CheckpointInput, byteSize int32) error {
	cause := input.Write.Cause
	var actorName *string
	if input.Write.GrantID != nil {
		name, err := q.GetCheckpointActorName(ctx, store.GetCheckpointActorNameParams{
			GrantID: *input.Write.GrantID, UserID: input.UserID,
		})
		if err != nil {
			return fmt.Errorf("resume: read checkpoint actor name: %w", err)
		}
		actorName = &name
		cause = "assistant"
	} else if cause == "" {
		cause = "edit"
	}
	pd, content, customization, err := encodeParts(input.Before.ResumeDocument())
	if err != nil {
		return fmt.Errorf("resume: encode checkpoint document: %w", err)
	}
	if _, err = q.InsertResumeCheckpoint(ctx, store.InsertResumeCheckpointParams{
		ResumeID: input.ResumeID, UserID: input.UserID, StartedAt: input.Now, Cause: cause,
		ActorGrantID: input.Write.GrantID, ActorName: actorName, Title: input.Before.Title, Lng: input.Before.Lng,
		SchemaVersion: input.Before.SchemaVersion, PersonalDetails: pd, Content: content, Customization: customization,
		ByteSize: byteSize,
	}); err != nil {
		return fmt.Errorf("resume: insert checkpoint: %w", err)
	}
	if err = q.PruneResumeCheckpoints(ctx, store.PruneResumeCheckpointsParams{
		ResumeID: input.ResumeID, UserID: input.UserID,
	}); err != nil {
		return fmt.Errorf("resume: prune checkpoints: %w", err)
	}
	return nil
}

func (s *Store) checkpointBeforeCAS(ctx context.Context, q *store.Queries, userID, id uuid.UUID, expected int64) (store.Resume, Resume, error) {
	if _, err := q.LockUserForResumeWrite(ctx, userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Resume{}, Resume{}, ErrNotFound
		}
		return store.Resume{}, Resume{}, fmt.Errorf("resume: lock checkpoint owner: %w", err)
	}
	row, err := q.GetResumeForCheckpointWrite(ctx, store.GetResumeForCheckpointWriteParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Resume{}, Resume{}, ErrNotFound
	}
	if err != nil {
		return store.Resume{}, Resume{}, fmt.Errorf("resume: read prior checkpoint state: %w", err)
	}
	before, err := s.projectRow(row)
	if err != nil {
		return store.Resume{}, Resume{}, err
	}
	if row.Revision != expected {
		if err = attachShowcase(ctx, q, &before); err != nil {
			return store.Resume{}, Resume{}, err
		}
		return store.Resume{}, Resume{}, &RevisionMismatchError{CurrentRevision: row.Revision, Current: before}
	}
	return row, before, nil
}

func (s *Store) checkpointResumeWrite(ctx context.Context, q *store.Queries, row store.Resume, before Resume,
	title string, lng *string, doc schema.Resume, write CheckpointWrite,
) (CheckpointSession, error) {
	return CheckpointTx(ctx, q, CheckpointInput{
		ResumeID: row.ID, UserID: row.UserID, Write: write,
		Before: checkpointState(before.Title, before.Lng, before.Doc, s.proj.CurrentVersion()), After: checkpointState(title, lng, doc, docmigrate.CurrentVersion),
		Session: CheckpointSession{Actor: row.SessionActor, StartedAt: row.SessionStartedAt, LastWriteAt: row.SessionLastWriteAt},
		Now:     s.now().UTC(),
	})
}

func ProjectCheckpoint(projector *docmigrate.Projector, row store.ResumeCheckpoint) (CheckpointState, error) {
	pd, content, customization, err := projector.Project(row.PersonalDetails, row.Content, row.Customization, row.SchemaVersion)
	if err != nil {
		return CheckpointState{}, fmt.Errorf("resume: project checkpoint: %w", err)
	}
	doc, err := DecodeParts(pd, content, customization, projector.CurrentVersion())
	if err != nil {
		return CheckpointState{}, fmt.Errorf("resume: decode checkpoint: %w", err)
	}
	return checkpointState(row.Title, row.Lng, doc, projector.CurrentVersion()), nil
}
