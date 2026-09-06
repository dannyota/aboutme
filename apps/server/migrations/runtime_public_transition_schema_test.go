package migrations

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRuntimePublicTransitionSchemaObjectsExist(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	for _, name := range []string{
		"public_transitions",
		"public_transition_targets",
		"public_transition_replicas",
		"public_transition_acks",
	} {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT to_regclass('public.' || $1) IS NOT NULL`,
			name,
		).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s is missing", name)
		}
	}
}

func TestRuntimePublicTransitionSchemaPreservesVersion15Data(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 15), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('cccccccc-cccc-4ccc-8ccc-cccccccccccc','transition-preserve@example.test','transition preserved')`); err != nil {
		t.Fatal(err)
	}
	fixture := replicaInsert(initiatorID, initiatorInstance, "a")
	fixture = append(fixture, `INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('transition-preserve-lifecycle','initial_serving')`)
	if err := membershipWrite(t, db, fixture...); err != nil {
		t.Fatal(err)
	}
	var generationBefore, discoveryBefore int64
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT discovery_generation FROM public.public_state WHERE singleton) FROM public.runtime_write_state WHERE singleton`).Scan(&generationBefore, &discoveryBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 16), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var generationAfter, discoveryAfter int64
	var userName, instanceID, workflow string
	if err := db.QueryRowContext(ctx, `SELECT s.generation,p.discovery_generation,u.name,r.instance_id,l.workflow_kind FROM public.runtime_write_state s CROSS JOIN public.public_state p CROSS JOIN public.users u CROSS JOIN public.runtime_replicas r CROSS JOIN public.runtime_lifecycle_operations l WHERE s.singleton AND p.singleton AND u.id='cccccccc-cccc-4ccc-8ccc-cccccccccccc' AND r.replica_id=$1 AND l.operation_id='transition-preserve-lifecycle'`, initiatorID).Scan(&generationAfter, &discoveryAfter, &userName, &instanceID, &workflow); err != nil {
		t.Fatal(err)
	}
	if generationAfter != generationBefore+1 || discoveryAfter != discoveryBefore || userName != "transition preserved" || instanceID != initiatorInstance || workflow != "initial_serving" {
		t.Fatalf("preservation generation=%d/%d discovery=%d/%d user=%q instance=%q workflow=%q", generationBefore, generationAfter, discoveryBefore, discoveryAfter, userName, instanceID, workflow)
	}
}

func TestRuntimePublicTransitionSchemaFailedFixtureRollsBackGeneration(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	fixture := transitionFixture()
	fixture = fixture[:len(fixture)-1]
	requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23514", "public_transition_parent_complete", "")
	var after int64
	var parents int
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT count(*) FROM public.public_transitions) FROM public.runtime_write_state WHERE singleton`).Scan(&after, &parents); err != nil {
		t.Fatal(err)
	}
	if after != before || parents != 0 {
		t.Fatalf("failed fixture generation=%d/%d parents=%d", before, after, parents)
	}
}

func TestRuntimePublicTransitionSchemaPublishedDigestAndCompleteClosing(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	if err := transitionWrite(t, db, transitionFixture()...); err != nil {
		t.Fatal(err)
	}
	var digest string
	if err := db.QueryRowContext(ctx, `SELECT encode(public.runtime_public_transition_target_digest($1),'hex')`, transitionID).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if digest != publishedDigest {
		t.Fatalf("digest=%s want=%s", digest, publishedDigest)
	}
}

func TestRuntimePublicTransitionSchemaPublishedVectorIndependentGoEncoding(t *testing.T) {
	encoded := append([]byte("aboutme.public-transition-targets.v1"), 0)
	encoded = binary.BigEndian.AppendUint32(encoded, 2)
	encoded = binary.BigEndian.AppendUint32(encoded, 0)
	encoded = append(encoded, 0)
	encoded = append(encoded, make([]byte, 16)...)
	encoded = binary.BigEndian.AppendUint64(encoded, 7)
	encoded = append(encoded, 1)
	encoded = binary.BigEndian.AppendUint32(encoded, 1)
	encoded = append(encoded, 1)
	resumeBytes, err := hex.DecodeString("00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, resumeBytes...)
	encoded = binary.BigEndian.AppendUint64(encoded, 42)
	encoded = append(encoded, 0)
	const publishedEncoding = "61626f75746d652e7075626c69632d7472616e736974696f6e2d746172676574732e76310000000002000000000000000000000000000000000000000000000000000000000701000000010100112233445566778899aabbccddeeff000000000000002a00"
	if got := hex.EncodeToString(encoded); got != publishedEncoding {
		t.Fatalf("encoded target frame=%s", got)
	}
	digest := sha256.Sum256(encoded)
	if got := hex.EncodeToString(digest[:]); got != publishedDigest {
		t.Fatalf("Go digest=%s want=%s", got, publishedDigest)
	}
}

func TestRuntimePublicTransitionSchemaParentConstraints(t *testing.T) {
	tests := []struct{ name, old, replacement, code, constraint, column string }{
		{"nil_id", transitionID, "00000000-0000-0000-0000-000000000000", "23514", "public_transitions_transition_id_non_nil", ""},
		{"null_initiator", ",'" + initiatorID + "','" + initiatorInstance, ",NULL,'" + initiatorInstance, "23502", "", "initiator_replica_id"},
		{"unknown_operation", "'resume_publication'", "'unknown'", "23514", "public_transitions_operation_check", ""},
		{"unknown_state", "'closing'", "'unknown'", "23514", "public_transitions_state_check", ""},
		{"wrong_deadline", "interval '5 seconds'", "interval '4 seconds'", "23514", "public_transitions_deadline_check", ""},
		{"short_digest", "decode('" + publishedDigest + "','hex')", "decode('00','hex')", "23514", "public_transitions_target_digest_check", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			err := transitionWrite(t, db, replaceTransitionFixture(t, test.old, test.replacement)...)
			requireTransitionPGError(t, err, test.code, test.constraint, test.column)
		})
	}
}

func TestRuntimePublicTransitionSchemaEveryRequiredParentColumnRejectsNull(t *testing.T) {
	tests := []struct{ name, old, replacement, column string }{
		{"initiator_instance", ",'" + initiatorInstance + "','sha256:", ",NULL,'sha256:", "initiator_instance_id"},
		{"initiator_release", ",'sha256:" + strings.Repeat("a", 64) + "','resume_publication'", ",NULL,'resume_publication'", "initiator_release_digest"},
		{"operation", ",'resume_publication','closing'", ",NULL,'closing'", "operation"},
		{"state", ",'closing',transaction_timestamp()", ",NULL,transaction_timestamp()", "state"},
		{"created_at", ",transaction_timestamp(),transaction_timestamp()+interval", ",NULL,transaction_timestamp()+interval", "created_at"},
		{"deadline_at", ",transaction_timestamp()+interval '5 seconds',decode", ",NULL,decode", "deadline_at"},
		{"target_digest", "decode('" + publishedDigest + "','hex'))", "NULL)", "target_digest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			requireTransitionPGError(t, transitionWrite(t, db, replaceTransitionFixture(t, test.old, test.replacement)...), "23502", "", test.column)
		})
	}
}

func TestRuntimePublicTransitionSchemaTargetConstraintsAndDigestMutations(t *testing.T) {
	tests := []struct{ name, old, replacement, code, constraint string }{
		{"ordinal_gap", ",1,'resume'", ",2,'resume'", "23514", "public_transition_parent_complete"},
		{"discovery_position", ",0,'discovery'", ",1,'discovery'", "23505", "public_transition_targets_pkey"},
		{"nil_resume", "'00112233-4455-6677-8899-aabbccddeeff'", "'00000000-0000-0000-0000-000000000000'", "23514", "public_transition_targets_identity_check"},
		{"zero_generation", ",42,'non_draining'", ",0,'non_draining'", "23514", "public_transition_targets_generation_check"},
		{"discovery_class", "7,'revoking'", "7,'non_draining'", "23514", "public_transition_targets_class_check"},
		{"kind_changes_digest", "0,'discovery',NULL,7,'revoking'", "0,'resume','00000000-0000-4000-8000-000000000001',7,'revoking'", "23514", "public_transition_parent_complete"},
		{"resume_changes_digest", "00112233-4455-6677-8899-aabbccddeeff", "00112233-4455-6677-8899-aabbccddee00", "23514", "public_transition_parent_complete"},
		{"generation_changes_digest", ",42,'non_draining'", ",43,'non_draining'", "23514", "public_transition_parent_complete"},
		{"class_changes_digest", ",42,'non_draining'", ",42,'revoking'", "23514", "public_transition_parent_complete"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			err := transitionWrite(t, db, replaceTransitionFixture(t, test.old, test.replacement)...)
			requireTransitionPGError(t, err, test.code, test.constraint, "")
		})
	}
}

func TestRuntimePublicTransitionSchemaTargetCountChangesDigest(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := append(transitionFixture(), fmt.Sprintf(`INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES('%s',2,'resume','10112233-4455-6677-8899-aabbccddeeff',9,'revoking')`, transitionID))
	requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23514", "public_transition_parent_complete", "")
}

func TestRuntimePublicTransitionSchemaWholeParentTargetSetMatrix(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func([]string) []string
		code       string
		constraint string
	}{
		{"zero_targets", func(f []string) []string { return append(f[:3], f[4:]...) }, "23514", "public_transition_parent_complete"},
		{"duplicate_discovery", func(f []string) []string {
			return append(f, fmt.Sprintf(`INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES('%s',2,'discovery',NULL,8,'revoking')`, transitionID))
		}, "23505", "public_transition_targets_discovery_idx"},
		{"duplicate_resume", func(f []string) []string {
			return append(f, fmt.Sprintf(`INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES('%s',2,'resume','00112233-4455-6677-8899-aabbccddeeff',8,'revoking')`, transitionID))
		}, "23505", "public_transition_targets_transition_resume_key"},
		{"raw_uuid_order", func(f []string) []string {
			return append(f, fmt.Sprintf(`INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES('%s',2,'resume','00000000-0000-4000-8000-000000000001',8,'revoking')`, transitionID))
		}, "23514", "public_transition_parent_complete"},
		{"five_targets", func(f []string) []string {
			return append(f, fmt.Sprintf(`INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES('%s',2,'resume','10112233-4455-6677-8899-aabbccddeeff',8,'revoking'),('%s',3,'resume','20112233-4455-6677-8899-aabbccddeeff',8,'revoking'),('%s',4,'resume','30112233-4455-6677-8899-aabbccddeeff',8,'revoking')`, transitionID, transitionID, transitionID))
		}, "23514", "public_transition_parent_complete"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			requireTransitionPGError(t, transitionWrite(t, db, test.mutate(transitionFixture())...), test.code, test.constraint, "")
		})
	}
}

func TestRuntimePublicTransitionSchemaAcceptsEveryAllowedTargetCount(t *testing.T) {
	tests := []struct {
		name, digest, values string
	}{
		{"one", "15bf78b4473929cc621ad1092a86101df931f1a5dbe26fe580241e487c90e720", fmt.Sprintf(`('%s',0,'discovery',NULL,7,'revoking')`, transitionID)},
		{"three", "9ec7b2a243c28f9481aa07a266ec8b62bc07c1dbca7129ced6ac61f6459aadfd", fmt.Sprintf(`('%s',0,'discovery',NULL,7,'revoking'),('%s',1,'resume','00112233-4455-6677-8899-aabbccddeeff',42,'non_draining'),('%s',2,'resume','10112233-4455-6677-8899-aabbccddeeff',8,'revoking')`, transitionID, transitionID, transitionID)},
		{"four", "0537816a78bb1d221ad4dd5e9ad9381649845c6eda66e477b94d1b2697239eec", fmt.Sprintf(`('%s',0,'discovery',NULL,7,'revoking'),('%s',1,'resume','00112233-4455-6677-8899-aabbccddeeff',42,'non_draining'),('%s',2,'resume','10112233-4455-6677-8899-aabbccddeeff',8,'revoking'),('%s',3,'resume','20112233-4455-6677-8899-aabbccddeeff',9,'non_draining')`, transitionID, transitionID, transitionID, transitionID)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := transitionFixture()
			for i := range fixture {
				fixture[i] = strings.ReplaceAll(fixture[i], publishedDigest, test.digest)
			}
			fixture[3] = `INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES` + test.values
			if err := transitionWrite(t, db, fixture...); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimePublicTransitionSchemaParentTerminalFieldMatrix(t *testing.T) {
	tests := []struct{ name, clause string }{
		{"closing_terminal", `state='closing',terminal_at=clock_timestamp()`},
		{"closing_error", `state='closing',terminal_error_code='canceled'`},
		{"closing_evidence", `state='closing',recovery_fencing_evidence_id='missing'`},
		{"committed_terminal", `state='committed',terminal_at=NULL`},
		{"committed_error", `state='committed',terminal_at=clock_timestamp(),terminal_error_code='canceled'`},
		{"committed_evidence", `state='committed',terminal_at=clock_timestamp(),recovery_fencing_evidence_id='missing'`},
		{"rollback_terminal", `state='rolled_back',terminal_at=NULL,terminal_error_code='canceled'`},
		{"rollback_error", `state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code=NULL`},
		{"rollback_unknown", `state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code='unknown'`},
		{"rollback_forbidden_evidence", `state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code='canceled',recovery_fencing_evidence_id='missing'`},
		{"fenced_missing_evidence", `state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code='initiator_fenced',recovery_fencing_evidence_id=NULL`},
		{"unresolved_terminal", `state='unresolved',terminal_at=NULL,terminal_error_code='recovery_evidence_conflict'`},
		{"unresolved_error", `state='unresolved',terminal_at=clock_timestamp(),terminal_error_code=NULL`},
		{"unresolved_wrong_error", `state='unresolved',terminal_at=clock_timestamp(),terminal_error_code='canceled'`},
		{"unresolved_evidence", `state='unresolved',terminal_at=clock_timestamp(),terminal_error_code='recovery_evidence_conflict',recovery_fencing_evidence_id='missing'`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := append(transitionFixture(), fmt.Sprintf(`UPDATE public.public_transitions SET %s WHERE transition_id='%s'`, test.clause, transitionID))
			code, constraint := "23514", "public_transitions_terminal_shape_check"
			if strings.HasPrefix(test.name, "closing_") {
				code, constraint = "55000", ""
			}
			requireTransitionPGError(t, transitionWrite(t, db, fixture...), code, constraint, "")
		})
	}
}

func TestRuntimePublicTransitionSchemaTargetResultFieldMatrix(t *testing.T) {
	tests := []struct{ name, clause string }{
		{"null_kind_generation", `result_generation=8`},
		{"null_kind_recorded", `result_recorded_at=clock_timestamp()`},
		{"generation_missing_generation", `result_kind='generation',result_recorded_at=clock_timestamp()`},
		{"generation_zero", `result_kind='generation',result_generation=0,result_recorded_at=clock_timestamp()`},
		{"generation_missing_time", `result_kind='generation',result_generation=8`},
		{"retired_has_generation", `result_kind='retired',result_generation=8,result_recorded_at=clock_timestamp()`},
		{"retired_missing_time", `result_kind='retired'`},
		{"discovery_retired", `result_kind='retired',result_recorded_at=clock_timestamp()`},
		{"unknown_kind", `result_kind='unknown'`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			ordinal := 1
			if test.name == "discovery_retired" {
				ordinal = 0
			}
			fixture := append(transitionFixture(), fmt.Sprintf(`UPDATE public.public_transition_targets SET %s WHERE transition_id='%s' AND ordinal=%d`, test.clause, transitionID, ordinal))
			constraint := "public_transition_targets_result_shape_check"
			if test.name == "unknown_kind" {
				constraint = "public_transition_targets_result_kind_check"
			}
			code := "23514"
			if strings.HasPrefix(test.name, "null_kind_") {
				code, constraint = "55000", ""
			}
			requireTransitionPGError(t, transitionWrite(t, db, fixture...), code, constraint, "")
		})
	}
}

func TestRuntimePublicTransitionSchemaCommittedGenerationAndRetirementShapes(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := append(transitionFixture(),
		transitionAckSQL(initiatorID),
		fmt.Sprintf(`UPDATE public.public_transition_targets SET result_kind='generation',result_generation=8,result_recorded_at=clock_timestamp() WHERE transition_id='%s' AND ordinal=0`, transitionID),
		fmt.Sprintf(`UPDATE public.public_transition_targets SET result_kind='retired',result_recorded_at=clock_timestamp() WHERE transition_id='%s' AND ordinal=1`, transitionID),
		fmt.Sprintf(`UPDATE public.public_transitions SET state='committed',terminal_at=clock_timestamp() WHERE transition_id='%s'`, transitionID))
	if err := transitionWrite(t, db, fixture...); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimePublicTransitionSchemaTupleAndDigestForeignKeys(t *testing.T) {
	tests := []struct{ name, old, replacement, constraint string }{
		{"initiator_instance", initiatorInstance, "i-bbbbbbbbbbbbbbbbb", "public_transitions_initiator_fkey"},
		{"required_release", "'sha256:" + strings.Repeat("a", 64) + "',decode", "'sha256:" + strings.Repeat("b", 64) + "',decode", "public_transition_replicas_membership_fkey"},
		{"required_digest", "decode('" + publishedDigest + "','hex'))", "decode('" + strings.Repeat("0", 64) + "','hex'))", "public_transition_replicas_parent_digest_fkey"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			err := transitionWrite(t, db, replaceTransitionFixture(t, test.old, test.replacement)...)
			requireTransitionPGError(t, err, "23503", test.constraint, "")
		})
	}
}

func TestRuntimePublicTransitionSchemaRequiredAndAckFieldMatrix(t *testing.T) {
	requiredCases := []struct{ name, values, code, constraint, column string }{
		{"nil_replica", fmt.Sprintf(`'%s',NULL,'required','i-bbbbbbbbbbbbbbbbb','sha256:%s',decode('%s','hex')`, transitionID, strings.Repeat("b", 64), publishedDigest), "23502", "", "replica_id"},
		{"snapshot", fmt.Sprintf(`'%s','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','unknown','i-bbbbbbbbbbbbbbbbb','sha256:%s',decode('%s','hex')`, transitionID, strings.Repeat("b", 64), publishedDigest), "23514", "public_transition_replicas_snapshot_state_check", ""},
		{"instance", fmt.Sprintf(`'%s','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','required',NULL,'sha256:%s',decode('%s','hex')`, transitionID, strings.Repeat("b", 64), publishedDigest), "23502", "", "instance_id"},
		{"release", fmt.Sprintf(`'%s','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','required','i-bbbbbbbbbbbbbbbbb',NULL,decode('%s','hex')`, transitionID, publishedDigest), "23502", "", "release_digest"},
		{"short_digest", fmt.Sprintf(`'%s','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','required','i-bbbbbbbbbbbbbbbbb','sha256:%s',decode('00','hex')`, transitionID, strings.Repeat("b", 64)), "23514", "public_transition_replicas_target_digest_check", ""},
	}
	for _, test := range requiredCases {
		t.Run("required_"+test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := append(secondReplicaFixture(), transitionFixture()...)
			fixture = append(fixture, `INSERT INTO public.public_transition_replicas(transition_id,replica_id,snapshot_state,instance_id,release_digest,target_digest) VALUES(`+test.values+`)`)
			requireTransitionPGError(t, transitionWrite(t, db, fixture...), test.code, test.constraint, test.column)
		})
	}
	ackCases := []struct{ name, values, code, constraint, column string }{
		{"nil_replica", fmt.Sprintf(`'%s',NULL,decode('%s','hex'),clock_timestamp(),'closed',0,0`, transitionID, publishedDigest), "23502", "", "replica_id"},
		{"short_digest", fmt.Sprintf(`'%s','%s',decode('00','hex'),clock_timestamp(),'closed',0,0`, transitionID, initiatorID), "23514", "public_transition_acks_target_digest_check", ""},
		{"acked_at", fmt.Sprintf(`'%s','%s',decode('%s','hex'),NULL,'closed',0,0`, transitionID, initiatorID, publishedDigest), "23502", "", "acked_at"},
		{"local_result", fmt.Sprintf(`'%s','%s',decode('%s','hex'),clock_timestamp(),'unknown',0,0`, transitionID, initiatorID, publishedDigest), "23514", "public_transition_acks_local_result_check", ""},
		{"revoking_negative", fmt.Sprintf(`'%s','%s',decode('%s','hex'),clock_timestamp(),'closed',-1,0`, transitionID, initiatorID, publishedDigest), "23514", "public_transition_acks_counts_check", ""},
		{"non_draining_negative", fmt.Sprintf(`'%s','%s',decode('%s','hex'),clock_timestamp(),'closed',0,-1`, transitionID, initiatorID, publishedDigest), "23514", "public_transition_acks_counts_check", ""},
		{"digest_mismatch", fmt.Sprintf(`'%s','%s',decode('%s','hex'),clock_timestamp(),'closed',0,0`, transitionID, initiatorID, strings.Repeat("0", 64)), "23503", "public_transition_acks_required_fkey", ""},
	}
	for _, test := range ackCases {
		t.Run("ack_"+test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := append(transitionFixture(), `INSERT INTO public.public_transition_acks(transition_id,replica_id,target_digest,acked_at,local_result,revoking_count,non_draining_count) VALUES(`+test.values+`)`)
			requireTransitionPGError(t, transitionWrite(t, db, fixture...), test.code, test.constraint, test.column)
		})
	}
}

func TestRuntimePublicTransitionSchemaEveryChildRequiredColumnRejectsNull(t *testing.T) {
	targetCases := []struct{ name, values, column string }{
		{"transition_id", `NULL,2,'resume','10112233-4455-6677-8899-aabbccddeeff',8,'revoking'`, "transition_id"},
		{"ordinal", fmt.Sprintf(`'%s',NULL,'resume','10112233-4455-6677-8899-aabbccddeeff',8,'revoking'`, transitionID), "ordinal"},
		{"kind", fmt.Sprintf(`'%s',2,NULL,'10112233-4455-6677-8899-aabbccddeeff',8,'revoking'`, transitionID), "kind"},
		{"expected_generation", fmt.Sprintf(`'%s',2,'resume','10112233-4455-6677-8899-aabbccddeeff',NULL,'revoking'`, transitionID), "expected_generation"},
		{"class", fmt.Sprintf(`'%s',2,'resume','10112233-4455-6677-8899-aabbccddeeff',8,NULL`, transitionID), "class"},
	}
	for _, test := range targetCases {
		t.Run("target_"+test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := append(transitionFixture(), `INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES(`+test.values+`)`)
			requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23502", "", test.column)
		})
	}
	requiredCases := []struct{ name, values, column string }{
		{"transition_id", fmt.Sprintf(`NULL,'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','required','i-bbbbbbbbbbbbbbbbb','sha256:%s',decode('%s','hex')`, strings.Repeat("b", 64), publishedDigest), "transition_id"},
		{"snapshot_state", fmt.Sprintf(`'%s','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',NULL,'i-bbbbbbbbbbbbbbbbb','sha256:%s',decode('%s','hex')`, transitionID, strings.Repeat("b", 64), publishedDigest), "snapshot_state"},
		{"target_digest", fmt.Sprintf(`'%s','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','required','i-bbbbbbbbbbbbbbbbb','sha256:%s',NULL`, transitionID, strings.Repeat("b", 64)), "target_digest"},
	}
	for _, test := range requiredCases {
		t.Run("required_"+test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := append(secondReplicaFixture(), transitionFixture()...)
			fixture = append(fixture, `INSERT INTO public.public_transition_replicas(transition_id,replica_id,snapshot_state,instance_id,release_digest,target_digest) VALUES(`+test.values+`)`)
			requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23502", "", test.column)
		})
	}
	ackCases := []struct{ name, values, column string }{
		{"transition_id", fmt.Sprintf(`NULL,'%s',decode('%s','hex'),clock_timestamp(),'closed',0,0`, initiatorID, publishedDigest), "transition_id"},
		{"target_digest", fmt.Sprintf(`'%s','%s',NULL,clock_timestamp(),'closed',0,0`, transitionID, initiatorID), "target_digest"},
		{"local_result", fmt.Sprintf(`'%s','%s',decode('%s','hex'),clock_timestamp(),NULL,0,0`, transitionID, initiatorID, publishedDigest), "local_result"},
		{"revoking_count", fmt.Sprintf(`'%s','%s',decode('%s','hex'),clock_timestamp(),'closed',NULL,0`, transitionID, initiatorID, publishedDigest), "revoking_count"},
		{"non_draining_count", fmt.Sprintf(`'%s','%s',decode('%s','hex'),clock_timestamp(),'closed',0,NULL`, transitionID, initiatorID, publishedDigest), "non_draining_count"},
	}
	for _, test := range ackCases {
		t.Run("ack_"+test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := append(transitionFixture(), `INSERT INTO public.public_transition_acks(transition_id,replica_id,target_digest,acked_at,local_result,revoking_count,non_draining_count) VALUES(`+test.values+`)`)
			requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23502", "", test.column)
		})
	}
}

func TestRuntimePublicTransitionSchemaFencedRollbackRequiresRealEvidence(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := transitionFixture()
	proof := fmt.Sprintf(`INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,evidence_id,requested_at,observed_terminated_at,observed_state) VALUES('%s','%s','sha256:%s','ec2_terminated_v1','transition-fence-proof',transaction_timestamp(),transaction_timestamp(),'terminated')`, initiatorID, initiatorInstance, strings.Repeat("a", 64))
	fixture = append(fixture[:2], append([]string{proof}, fixture[2:]...)...)
	fixture = append(fixture, fmt.Sprintf(`UPDATE public.public_transitions SET state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code='initiator_fenced',recovery_fencing_evidence_id='transition-fence-proof' WHERE transition_id='%s'`, transitionID))
	if err := transitionWrite(t, db, fixture...); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimePublicTransitionSchemaOwnerRequiresEntryAndIgnoresSearchPath(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck
	if _, authErr := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_runtime_owner`); authErr != nil {
		t.Fatal(authErr)
	}
	_, err = conn.ExecContext(ctx, `DELETE FROM public.public_transition_acks`)
	requireTransitionPGError(t, err, "AM001", "", "")
	if _, err := conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
		t.Fatal(err)
	}
	if err := transitionWrite(t, db, append(transitionFixture(), `SET LOCAL search_path=pg_temp,public`)...); err != nil {
		t.Fatalf("hostile search path redirected helper: %v", err)
	}
}

func TestRuntimePublicTransitionSchemaCommittedRequiresResultsAndAcks(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := transitionFixture()
	fixture = append(fixture, fmt.Sprintf(`UPDATE public.public_transitions SET state='committed',terminal_at=clock_timestamp() WHERE transition_id='%s'`, transitionID))
	requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23514", "public_transition_parent_complete", "")

	db, _ = runtimeMembershipDB(t)
	fixture = transitionFixture()
	fixture = append(fixture,
		fmt.Sprintf(`UPDATE public.public_transition_targets SET result_kind='generation',result_generation=expected_generation+1,result_recorded_at=clock_timestamp() WHERE transition_id='%s'`, transitionID),
		fmt.Sprintf(`UPDATE public.public_transitions SET state='committed',terminal_at=clock_timestamp() WHERE transition_id='%s'`, transitionID))
	requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23514", "public_transition_parent_complete", "")

	db, _ = runtimeMembershipDB(t)
	fixture = transitionFixture()
	fixture = append(fixture,
		fmt.Sprintf(`INSERT INTO public.public_transition_acks(transition_id,replica_id,target_digest,acked_at,local_result,revoking_count,non_draining_count) VALUES('%s','%s',decode('%s','hex'),clock_timestamp(),'closed',1,1)`, transitionID, initiatorID, publishedDigest),
		fmt.Sprintf(`UPDATE public.public_transition_targets SET result_kind='generation',result_generation=expected_generation+1,result_recorded_at=clock_timestamp() WHERE transition_id='%s'`, transitionID),
		fmt.Sprintf(`UPDATE public.public_transitions SET state='committed',terminal_at=clock_timestamp() WHERE transition_id='%s'`, transitionID))
	if err := transitionWrite(t, db, fixture...); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimePublicTransitionSchemaNullRequiredMatrixFieldsFail(t *testing.T) {
	tests := []struct {
		name       string
		statements []string
		constraint string
	}{
		{
			name: "rolled_back_error_code",
			statements: []string{fmt.Sprintf(
				`UPDATE public.public_transitions SET state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code=NULL WHERE transition_id='%s'`, transitionID)},
			constraint: "public_transitions_terminal_shape_check",
		},
		{
			name: "generation_result_generation",
			statements: []string{
				fmt.Sprintf(`INSERT INTO public.public_transition_acks(transition_id,replica_id,target_digest,acked_at,local_result,revoking_count,non_draining_count) VALUES('%s','%s',decode('%s','hex'),clock_timestamp(),'closed',1,1)`, transitionID, initiatorID, publishedDigest),
				fmt.Sprintf(`UPDATE public.public_transition_targets SET result_kind='generation',result_generation=expected_generation+1,result_recorded_at=clock_timestamp() WHERE transition_id='%s' AND ordinal=0`, transitionID),
				fmt.Sprintf(`UPDATE public.public_transition_targets SET result_kind='generation',result_generation=NULL,result_recorded_at=clock_timestamp() WHERE transition_id='%s' AND ordinal=1`, transitionID),
				fmt.Sprintf(`UPDATE public.public_transitions SET state='committed',terminal_at=clock_timestamp() WHERE transition_id='%s'`, transitionID),
			},
			constraint: "public_transition_targets_result_shape_check",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := append(transitionFixture(), test.statements...)
			requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23514", test.constraint, "")
		})
	}
}

func TestRuntimePublicTransitionSchemaNonCommittedAckSetsAndNullResults(t *testing.T) {
	for _, state := range []string{"closing", "rolled_back", "unresolved"} {
		for _, ack := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_ack_%t", state, ack), func(t *testing.T) {
				db, _ := runtimeMembershipDB(t)
				fixture := transitionFixture()
				if ack {
					fixture = append(fixture, fmt.Sprintf(`INSERT INTO public.public_transition_acks(transition_id,replica_id,target_digest,acked_at,local_result,revoking_count,non_draining_count) VALUES('%s','%s',decode('%s','hex'),clock_timestamp(),'closed',0,0)`, transitionID, initiatorID, publishedDigest))
				}
				switch state {
				case "rolled_back":
					fixture = append(fixture, fmt.Sprintf(`UPDATE public.public_transitions SET state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code='canceled' WHERE transition_id='%s'`, transitionID))
				case "unresolved":
					fixture = append(fixture, fmt.Sprintf(`UPDATE public.public_transitions SET state='unresolved',terminal_at=clock_timestamp(),terminal_error_code='recovery_evidence_conflict' WHERE transition_id='%s'`, transitionID))
				}
				if err := transitionWrite(t, db, fixture...); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRuntimePublicTransitionSchemaTwoReplicaAckCompletenessMatrix(t *testing.T) {
	for _, state := range []string{"closing", "rolled_back", "unresolved", "committed"} {
		for ackCount := range 3 {
			t.Run(fmt.Sprintf("%s_%d", state, ackCount), func(t *testing.T) {
				db, _ := runtimeMembershipDB(t)
				fixture := append(secondReplicaFixture(), transitionFixture()...)
				fixture = append(fixture, secondRequiredReplicaSQL())
				if ackCount >= 1 {
					fixture = append(fixture, transitionAckSQL(initiatorID))
				}
				if ackCount == 2 {
					fixture = append(fixture, transitionAckSQL("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"))
				}
				switch state {
				case "rolled_back":
					fixture = append(fixture, fmt.Sprintf(`UPDATE public.public_transitions SET state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code='canceled' WHERE transition_id='%s'`, transitionID))
				case "unresolved":
					fixture = append(fixture, fmt.Sprintf(`UPDATE public.public_transitions SET state='unresolved',terminal_at=clock_timestamp(),terminal_error_code='recovery_evidence_conflict' WHERE transition_id='%s'`, transitionID))
				case "committed":
					fixture = append(fixture,
						fmt.Sprintf(`UPDATE public.public_transition_targets SET result_kind='generation',result_generation=expected_generation+1,result_recorded_at=clock_timestamp() WHERE transition_id='%s'`, transitionID),
						fmt.Sprintf(`UPDATE public.public_transitions SET state='committed',terminal_at=clock_timestamp() WHERE transition_id='%s'`, transitionID))
				}
				err := transitionWrite(t, db, fixture...)
				if state == "committed" && ackCount < 2 {
					requireTransitionPGError(t, err, "23514", "public_transition_parent_complete", "")
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRuntimePublicTransitionSchemaTerminalRowsAndChildrenAreImmutable(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := append(transitionFixture(), fmt.Sprintf(`UPDATE public.public_transitions SET state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code='canceled' WHERE transition_id='%s'`, transitionID))
	if err := transitionWrite(t, db, fixture...); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		fmt.Sprintf(`UPDATE public.public_transitions SET terminal_at=clock_timestamp() WHERE transition_id='%s'`, transitionID),
		fmt.Sprintf(`DELETE FROM public.public_transitions WHERE transition_id='%s'`, transitionID),
		fmt.Sprintf(`INSERT INTO public.public_transition_acks(transition_id,replica_id,target_digest,acked_at,local_result,revoking_count,non_draining_count) VALUES('%s','%s',decode('%s','hex'),clock_timestamp(),'closed',0,0)`, transitionID, initiatorID, publishedDigest),
	} {
		requireTransitionPGError(t, transitionWrite(t, db, statement), "55000", "", "")
	}
}

func TestRuntimePublicTransitionSchemaEveryChildMutationIsRejected(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := append(transitionFixture(),
		transitionAckSQL(initiatorID),
		fmt.Sprintf(`UPDATE public.public_transition_targets SET result_kind='generation',result_generation=expected_generation+1,result_recorded_at=clock_timestamp() WHERE transition_id='%s'`, transitionID),
		fmt.Sprintf(`UPDATE public.public_transitions SET state='committed',terminal_at=clock_timestamp() WHERE transition_id='%s'`, transitionID))
	if err := transitionWrite(t, db, fixture...); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct{ name, statement string }{
		{"target_update", fmt.Sprintf(`UPDATE public.public_transition_targets SET result_generation=result_generation+1 WHERE transition_id='%s'`, transitionID)},
		{"target_delete", fmt.Sprintf(`DELETE FROM public.public_transition_targets WHERE transition_id='%s'`, transitionID)},
		{"required_update", fmt.Sprintf(`UPDATE public.public_transition_replicas SET snapshot_state='required' WHERE transition_id='%s'`, transitionID)},
		{"required_delete", fmt.Sprintf(`DELETE FROM public.public_transition_replicas WHERE transition_id='%s'`, transitionID)},
		{"ack_update", fmt.Sprintf(`UPDATE public.public_transition_acks SET revoking_count=revoking_count+1 WHERE transition_id='%s'`, transitionID)},
		{"ack_delete", fmt.Sprintf(`DELETE FROM public.public_transition_acks WHERE transition_id='%s'`, transitionID)},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			requireTransitionPGError(t, transitionWrite(t, db, mutation.statement), "55000", "", "")
		})
	}
}

func TestRuntimePublicTransitionSchemaTruncateAndPrivileges(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	for _, table := range []string{"public_transitions", "public_transition_targets", "public_transition_replicas", "public_transition_acks"} {
		requireTransitionPGError(t, transitionWrite(t, db, `TRUNCATE public.`+table+` CASCADE`), "AM001", "", "")
	}
	requireTransitionPGError(t, transitionWrite(t, db, `TRUNCATE public.public_transitions CASCADE`), "AM001", "", "")
	for _, role := range []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command", "aboutme_fencing_proof", "aboutme_restore_verify", "aboutme_migrator"} {
		var dml, execute bool
		if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1,'public.public_transitions','SELECT,INSERT,UPDATE,DELETE,TRUNCATE'),has_function_privilege($1,'public.runtime_public_transition_target_digest(uuid)','EXECUTE')`, role).Scan(&dml, &execute); err != nil {
			t.Fatal(err)
		}
		if dml || execute {
			t.Errorf("role %s dml=%t execute=%t", role, dml, execute)
		}
	}
}

func TestRuntimePublicTransitionSchemaCatalog(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	var badOwners, columnACLs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname=ANY($1) AND pg_get_userbyid(relowner)<>'aboutme_runtime_owner'`, []string{"public_transitions", "public_transition_targets", "public_transition_replicas", "public_transition_acks"}).Scan(&badOwners); err != nil || badOwners != 0 {
		t.Fatalf("owners=%d error=%v", badOwners, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.column_privileges WHERE table_schema='public' AND table_name LIKE 'public_transition%' AND grantee<>'aboutme_runtime_owner'`).Scan(&columnACLs); err != nil || columnACLs != 0 {
		t.Fatalf("column ACLs=%d error=%v", columnACLs, err)
	}
	var helpersOK bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=6 AND bool_and(pg_get_userbyid(proowner)='aboutme_runtime_owner' AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE')) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY(ARRAY['runtime_public_transition_target_digest','runtime_assert_public_transition_parent','runtime_validate_public_transition_parent','runtime_validate_public_transition_child','runtime_validate_public_transition_target','runtime_reject_public_transition_truncate'])`).Scan(&helpersOK); err != nil || !helpersOK {
		t.Fatalf("helpers=%t error=%v", helpersOK, err)
	}
	var triggerCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace='public'::regnamespace AND c.relname=ANY($1) AND NOT t.tgisinternal AND t.tgname=ANY($2)`,
		[]string{"public_transitions", "public_transition_targets", "public_transition_replicas", "public_transition_acks"},
		[]string{"public_transitions_immutable", "public_transition_targets_immutable", "public_transition_replicas_immutable", "public_transition_acks_immutable", "public_transitions_parent_complete", "public_transition_targets_parent_complete", "public_transition_replicas_parent_complete", "public_transition_acks_parent_complete", "public_transitions_write_entry", "public_transition_targets_write_entry", "public_transition_replicas_write_entry", "public_transition_acks_write_entry", "public_transitions_no_truncate", "public_transition_targets_no_truncate", "public_transition_replicas_no_truncate", "public_transition_acks_no_truncate"}).Scan(&triggerCount); err != nil || triggerCount != 16 {
		t.Fatalf("exact trigger count=%d error=%v", triggerCount, err)
	}
	var triggerShapesOK bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=4 FROM pg_trigger WHERE tgname LIKE 'public_transition%_parent_complete' AND tgdeferrable AND tginitdeferred AND (tgtype&1)=1`).Scan(&triggerShapesOK); err != nil {
		t.Fatal(err)
	}
	var writeShapes, truncateShapes bool
	if err := db.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=4 FROM pg_trigger WHERE tgname LIKE 'public_transition%_write_entry' AND (tgtype&2)=2 AND (tgtype&1)=0),
 (SELECT count(*)=4 FROM pg_trigger WHERE tgname LIKE 'public_transition%_no_truncate' AND (tgtype&2)=2 AND (tgtype&1)=0)`).Scan(&writeShapes, &truncateShapes); err != nil || !triggerShapesOK || !writeShapes || !truncateShapes {
		t.Fatalf("trigger shapes constraint=%t write=%t truncate=%t error=%v", triggerShapesOK, writeShapes, truncateShapes, err)
	}
}

func TestRuntimePublicTransitionSchemaRealRolesCannotCallOrMutate(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	for _, role := range []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command", "aboutme_fencing_proof", "aboutme_restore_verify"} {
		t.Run(role, func(t *testing.T) {
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close() //nolint:errcheck
			if _, err := conn.ExecContext(context.Background(), `SET SESSION AUTHORIZATION `+role); err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{
				`INSERT INTO public.public_transitions(transition_id) VALUES('10000000-0000-4000-8000-000000000000')`,
				`UPDATE public.public_transitions SET state='closing'`,
				`DELETE FROM public.public_transitions`,
				`TRUNCATE public.public_transition_acks`,
				`SELECT public.runtime_public_transition_target_digest('10000000-0000-4000-8000-000000000000')`,
			} {
				_, callErr := conn.ExecContext(context.Background(), statement)
				requireTransitionPGError(t, callErr, "42501", "", "")
			}
			if _, err := conn.ExecContext(context.Background(), `RESET SESSION AUTHORIZATION`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimePublicTransitionSchemaTerminalizationSerializesChildInsert(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	if err := transitionWrite(t, db, transitionFixture()...); err != nil {
		t.Fatal(err)
	}
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelSetup()
	_, terminalTx := beginTransitionWrite(t, db)
	if _, err := terminalTx.ExecContext(setupCtx, fmt.Sprintf(`UPDATE public.public_transitions SET state='rolled_back',terminal_at=clock_timestamp(),terminal_error_code='canceled' WHERE transition_id='%s'`, transitionID)); err != nil {
		t.Fatal(err)
	}
	_, childTx := beginTransitionWrite(t, db)
	var childPID int
	if err := childTx.QueryRowContext(setupCtx, `SELECT pg_backend_pid()`).Scan(&childPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	raceCtx, cancelRace := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRace()
	go func() {
		_, err := childTx.ExecContext(raceCtx, transitionAckSQL(initiatorID))
		result <- err
	}()
	if err := waitForTransitionCondition(db, `SELECT cardinality(pg_blocking_pids($1))>0 OR EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, childPID); err != nil {
		rollbackTransitionWrite(t, terminalTx)
		cancelRace()
		childErr := receiveTransitionRace(t, result)
		rollbackTransitionWrite(t, childTx)
		t.Fatalf("%v; child result: %v", err, childErr)
	}
	if err := finishTransitionWrite(terminalTx); err != nil {
		cancelRace()
		childErr := receiveTransitionRace(t, result)
		rollbackTransitionWrite(t, childTx)
		t.Fatalf("finish terminal transaction: %v; child result: %v", err, childErr)
	}
	requireTransitionPGError(t, receiveTransitionRace(t, result), "55000", "", "")
	rollbackTransitionWrite(t, childTx)
	var ackCount int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM public.public_transition_acks WHERE transition_id=$1`, transitionID).Scan(&ackCount); err != nil || ackCount != 0 {
		t.Fatalf("late ack count=%d error=%v", ackCount, err)
	}
}

func TestRuntimePublicTransitionSchemaDuplicateChildrenSerializeOnParent(t *testing.T) {
	for _, child := range []struct {
		name, statement, constraint string
	}{
		{"required", secondRequiredReplicaSQL(), "public_transition_replicas_pkey"},
		{"ack", transitionAckSQL(initiatorID), "public_transition_acks_pkey"},
	} {
		t.Run(child.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := transitionFixture()
			if child.name == "required" {
				fixture = append(secondReplicaFixture(), fixture...)
			}
			if err := transitionWrite(t, db, fixture...); err != nil {
				t.Fatal(err)
			}
			setupCtx, cancelSetup := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSetup()
			_, firstTx := beginTransitionWrite(t, db)
			if _, err := firstTx.ExecContext(setupCtx, child.statement); err != nil {
				t.Fatal(err)
			}
			_, secondTx := beginTransitionWrite(t, db)
			var secondPID int
			if err := secondTx.QueryRowContext(setupCtx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			raceCtx, cancelRace := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelRace()
			go func() {
				_, err := secondTx.ExecContext(raceCtx, child.statement)
				result <- err
			}()
			if err := waitForTransitionCondition(db, `SELECT cardinality(pg_blocking_pids($1))>0 OR EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, secondPID); err != nil {
				rollbackTransitionWrite(t, firstTx)
				cancelRace()
				secondErr := receiveTransitionRace(t, result)
				rollbackTransitionWrite(t, secondTx)
				t.Fatalf("%v; second result: %v", err, secondErr)
			}
			if err := finishTransitionWrite(firstTx); err != nil {
				cancelRace()
				secondErr := receiveTransitionRace(t, result)
				rollbackTransitionWrite(t, secondTx)
				t.Fatalf("finish first child transaction: %v; second result: %v", err, secondErr)
			}
			requireTransitionPGError(t, receiveTransitionRace(t, result), "23505", child.constraint, "")
			rollbackTransitionWrite(t, secondTx)
		})
	}
}

func TestRuntimePublicTransitionSchemaForcedConstraintSchedulesLaterChanges(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := transitionFixture()
	fixture = append(fixture,
		`SET CONSTRAINTS public_transitions_parent_complete,public_transition_targets_parent_complete,public_transition_replicas_parent_complete,public_transition_acks_parent_complete IMMEDIATE`,
		`SET CONSTRAINTS public_transitions_parent_complete,public_transition_targets_parent_complete,public_transition_replicas_parent_complete,public_transition_acks_parent_complete DEFERRED`,
		fmt.Sprintf(`INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES('%s',2,'resume','10112233-4455-6677-8899-aabbccddeeff',8,'revoking')`, transitionID),
	)
	requireTransitionPGError(t, transitionWrite(t, db, fixture...), "23514", "public_transition_parent_complete", "")
}

func requireTransitionPGError(t *testing.T, err error, code, constraint, column string) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid transition write succeeded")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.ConstraintName != constraint || pgErr.ColumnName != column {
		t.Fatalf("error code=%q constraint=%q column=%q: %v", pgErrCode(pgErr), pgErrConstraint(pgErr), pgErrColumn(pgErr), err)
	}
}
