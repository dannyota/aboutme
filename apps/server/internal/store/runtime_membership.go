package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	runtimeInstanceIDPattern    = regexp.MustCompile(`^i-[0-9a-f]{17}$`)
	runtimeReleaseDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// RuntimeReplicaIdentity is the complete trusted runtime replica identity.
type RuntimeReplicaIdentity struct {
	ReplicaID            uuid.UUID
	InstanceID           string
	ContainerInstanceARN string
	CaddyTaskARN         string
	GoTaskARN            string
	NuxtTaskARN          string
	ReleaseDigest        string
}

// RuntimeReplicaResult is the fixed result returned by runtime membership operations.
type RuntimeReplicaResult struct {
	ReplicaID                 uuid.UUID
	ReplicaKind               string
	State                     string
	DesiredReplicas           *int16
	ActiveServingReplicas     *int16
	ActiveMaintenanceReplicas *int16
	Partition1Enabled         *bool
	Partition2Enabled         *bool
	CapacityGeneration        int64
	ControllerGeneration      int64
	ControllerOperationID     string
	AdmissionEnabled          bool
	LifecyclePhase            string
	Replayed                  bool
}

// RuntimeReplicaRegistrationStore registers replicas and records joining readiness.
type RuntimeReplicaRegistrationStore interface {
	RegisterServing(context.Context, RuntimeReplicaIdentity) (RuntimeReplicaResult, error)
	RegisterMaintenance(context.Context, RuntimeReplicaIdentity) (RuntimeReplicaResult, error)
	MarkServingJoinReady(context.Context, RuntimeReplicaIdentity) (RuntimeReplicaResult, error)
	MarkMaintenanceJoinReady(context.Context, RuntimeReplicaIdentity) (RuntimeReplicaResult, error)
}

type runtimeReplicaRegistrationStore struct{ runner WriteTxRunner }

// NewRuntimeReplicaRegistrationStore creates a registration store backed by pool.
func NewRuntimeReplicaRegistrationStore(pool *pgxpool.Pool) RuntimeReplicaRegistrationStore {
	return &runtimeReplicaRegistrationStore{runner: NewWriteTxRunner(pool)}
}

type runtimeReplicaRow struct {
	replicaID                        uuid.UUID
	replicaKind, state               string
	desiredValue                     int16
	desiredPresent                   bool
	servingValue                     int16
	servingPresent                   bool
	maintenanceValue                 int16
	maintenancePresent               bool
	partition1Value                  bool
	partition1Present                bool
	partition2Value                  bool
	partition2Present                bool
	capacityGeneration               int64
	controllerGeneration             int64
	controllerOperationID, lifecycle string
	admission, replayed              bool
}

// RegisterServing registers a serving replica.
func (s *runtimeReplicaRegistrationStore) RegisterServing(ctx context.Context, identity RuntimeReplicaIdentity) (RuntimeReplicaResult, error) {
	return s.run(ctx, identity, "serving", func(q *Queries) (runtimeReplicaRow, error) {
		r, err := q.RuntimeRegisterServingReplica(ctx, servingParams(identity))
		return rowFromServingRegister(r), err
	})
}

// RegisterMaintenance registers a maintenance replica.
func (s *runtimeReplicaRegistrationStore) RegisterMaintenance(ctx context.Context, identity RuntimeReplicaIdentity) (RuntimeReplicaResult, error) {
	return s.run(ctx, identity, "maintenance", func(q *Queries) (runtimeReplicaRow, error) {
		r, err := q.RuntimeRegisterMaintenanceReplica(ctx, maintenanceParams(identity))
		return rowFromMaintenanceRegister(r), err
	})
}

// MarkServingJoinReady records serving replica joining readiness.
func (s *runtimeReplicaRegistrationStore) MarkServingJoinReady(ctx context.Context, identity RuntimeReplicaIdentity) (RuntimeReplicaResult, error) {
	return s.run(ctx, identity, "serving", func(q *Queries) (runtimeReplicaRow, error) {
		r, err := q.RuntimeMarkServingReplicaJoinReady(ctx, RuntimeMarkServingReplicaJoinReadyParams(servingParams(identity)))
		return rowFromServingReady(r), err
	})
}

// MarkMaintenanceJoinReady records maintenance replica joining readiness.
func (s *runtimeReplicaRegistrationStore) MarkMaintenanceJoinReady(ctx context.Context, identity RuntimeReplicaIdentity) (RuntimeReplicaResult, error) {
	return s.run(ctx, identity, "maintenance", func(q *Queries) (runtimeReplicaRow, error) {
		r, err := q.RuntimeMarkMaintenanceReplicaJoinReady(ctx, RuntimeMarkMaintenanceReplicaJoinReadyParams(maintenanceParams(identity)))
		return rowFromMaintenanceReady(r), err
	})
}

func (s *runtimeReplicaRegistrationStore) run(ctx context.Context, identity RuntimeReplicaIdentity, kind string, query func(*Queries) (runtimeReplicaRow, error)) (RuntimeReplicaResult, error) {
	var zero RuntimeReplicaResult
	if ctx == nil {
		return zero, errors.New("store: nil replica registration context")
	}
	if s == nil || s.runner == nil {
		return zero, errors.New("store: nil replica registration runner")
	}
	if err := validateRuntimeReplicaIdentity(identity); err != nil {
		return zero, err
	}
	var decoded RuntimeReplicaResult
	err := s.runner.ExecWrite(ctx, func(q *Queries) error {
		row, err := query(q)
		if err != nil {
			return fmt.Errorf("store: execute replica registration: %w", err)
		}
		decoded, err = decodeRuntimeReplicaResult(row, identity, kind)
		return err
	})
	if err != nil {
		return zero, err
	}
	return decoded, nil
}

func validateRuntimeReplicaIdentity(i RuntimeReplicaIdentity) error {
	arns := []string{i.ContainerInstanceARN, i.CaddyTaskARN, i.GoTaskARN, i.NuxtTaskARN}
	if i.ReplicaID == uuid.Nil || !runtimeInstanceIDPattern.MatchString(i.InstanceID) || !runtimeReleaseDigestPattern.MatchString(i.ReleaseDigest) {
		return errors.New("store: invalid replica identity")
	}
	seen := make(map[string]struct{}, len(arns))
	for _, value := range arns {
		if len(value) == 0 || len(value) > 512 {
			return errors.New("store: invalid replica identity")
		}
		for _, b := range []byte(value) {
			if b < 0x21 || b > 0x7e {
				return errors.New("store: invalid replica identity")
			}
		}
		if _, exists := seen[value]; exists {
			return errors.New("store: invalid replica identity")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func decodeRuntimeReplicaResult(r runtimeReplicaRow, identity RuntimeReplicaIdentity, kind string) (RuntimeReplicaResult, error) {
	var zero RuntimeReplicaResult
	validState := r.state == "joining" || r.state == "active" || r.state == "draining" || r.state == "left" || r.state == "terminating" || r.state == "fenced"
	validLifecycle := r.lifecycle == "offline" || r.lifecycle == "starting" || r.lifecycle == "online" || r.lifecycle == "stopping"
	if r.replicaID != identity.ReplicaID || r.replicaKind != kind || !validState || r.desiredPresent || r.servingPresent || r.maintenancePresent || r.partition1Present || r.partition2Present || r.capacityGeneration <= 0 || r.controllerGeneration <= 0 || !runtimePrintableText(r.controllerOperationID, 128) || !validLifecycle || r.admission != (r.lifecycle == "online") {
		return zero, errors.New("store: invalid replica registration result")
	}
	return RuntimeReplicaResult{ReplicaID: r.replicaID, ReplicaKind: r.replicaKind, State: r.state, CapacityGeneration: r.capacityGeneration, ControllerGeneration: r.controllerGeneration, ControllerOperationID: r.controllerOperationID, AdmissionEnabled: r.admission, LifecyclePhase: r.lifecycle, Replayed: r.replayed}, nil
}

func runtimePrintableText(value string, maximum int) bool {
	if len(value) == 0 || len(value) > maximum {
		return false
	}
	for _, b := range []byte(value) {
		if b < 0x20 || b > 0x7e {
			return false
		}
	}
	return true
}

func servingParams(i RuntimeReplicaIdentity) RuntimeRegisterServingReplicaParams {
	return RuntimeRegisterServingReplicaParams{ReplicaID: i.ReplicaID, InstanceID: i.InstanceID, ContainerInstanceArn: i.ContainerInstanceARN, CaddyTaskArn: i.CaddyTaskARN, GoTaskArn: i.GoTaskARN, NuxtTaskArn: i.NuxtTaskARN, ReleaseDigest: i.ReleaseDigest}
}

func maintenanceParams(i RuntimeReplicaIdentity) RuntimeRegisterMaintenanceReplicaParams {
	return RuntimeRegisterMaintenanceReplicaParams(servingParams(i))
}

func commonRow(id uuid.UUID, kind, state string, desired int16, desiredP bool, serving int16, servingP bool, maintenance int16, maintenanceP bool, p1 bool, p1P bool, p2 bool, p2P bool, capGen, ctrlGen int64, operation string, admission bool, lifecycle string, replayed bool) runtimeReplicaRow {
	return runtimeReplicaRow{id, kind, state, desired, desiredP, serving, servingP, maintenance, maintenanceP, p1, p1P, p2, p2P, capGen, ctrlGen, operation, lifecycle, admission, replayed}
}

func rowFromServingRegister(r RuntimeRegisterServingReplicaRow) runtimeReplicaRow {
	return commonRow(r.ReplicaID, r.ReplicaKind, r.State, r.DesiredReplicasValue, r.DesiredReplicasPresent, r.ActiveServingReplicasValue, r.ActiveServingReplicasPresent, r.ActiveMaintenanceReplicasValue, r.ActiveMaintenanceReplicasPresent, r.Partition1EnabledValue, r.Partition1EnabledPresent, r.Partition2EnabledValue, r.Partition2EnabledPresent, r.CapacityGeneration, r.ControllerGeneration, r.ControllerOperationID, r.AdmissionEnabled, r.LifecyclePhase, r.Replayed)
}
func rowFromMaintenanceRegister(r RuntimeRegisterMaintenanceReplicaRow) runtimeReplicaRow {
	return commonRow(r.ReplicaID, r.ReplicaKind, r.State, r.DesiredReplicasValue, r.DesiredReplicasPresent, r.ActiveServingReplicasValue, r.ActiveServingReplicasPresent, r.ActiveMaintenanceReplicasValue, r.ActiveMaintenanceReplicasPresent, r.Partition1EnabledValue, r.Partition1EnabledPresent, r.Partition2EnabledValue, r.Partition2EnabledPresent, r.CapacityGeneration, r.ControllerGeneration, r.ControllerOperationID, r.AdmissionEnabled, r.LifecyclePhase, r.Replayed)
}
func rowFromServingReady(r RuntimeMarkServingReplicaJoinReadyRow) runtimeReplicaRow {
	return commonRow(r.ReplicaID, r.ReplicaKind, r.State, r.DesiredReplicasValue, r.DesiredReplicasPresent, r.ActiveServingReplicasValue, r.ActiveServingReplicasPresent, r.ActiveMaintenanceReplicasValue, r.ActiveMaintenanceReplicasPresent, r.Partition1EnabledValue, r.Partition1EnabledPresent, r.Partition2EnabledValue, r.Partition2EnabledPresent, r.CapacityGeneration, r.ControllerGeneration, r.ControllerOperationID, r.AdmissionEnabled, r.LifecyclePhase, r.Replayed)
}
func rowFromMaintenanceReady(r RuntimeMarkMaintenanceReplicaJoinReadyRow) runtimeReplicaRow {
	return commonRow(r.ReplicaID, r.ReplicaKind, r.State, r.DesiredReplicasValue, r.DesiredReplicasPresent, r.ActiveServingReplicasValue, r.ActiveServingReplicasPresent, r.ActiveMaintenanceReplicasValue, r.ActiveMaintenanceReplicasPresent, r.Partition1EnabledValue, r.Partition1EnabledPresent, r.Partition2EnabledValue, r.Partition2EnabledPresent, r.CapacityGeneration, r.ControllerGeneration, r.ControllerOperationID, r.AdmissionEnabled, r.LifecyclePhase, r.Replayed)
}
