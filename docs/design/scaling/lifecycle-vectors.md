# Lifecycle digest vectors

## Authority and clarification

[Lifecycle replay](lifecycle-replay.md) fixes the byte encoding.
[Lifecycle operations](lifecycle-operations.md) fixes the eight function
signatures and result semantics. Migration 00015 fixes the stored step columns
and null matrix. These vectors pin that contract for SQL and Go tests.

## Canonical stream

Every digest is SHA-256 over:

1. ASCII aboutme.runtime-lifecycle-step.v1;
2. one NUL byte;
3. one kind byte: 0x01 arguments or 0x02 result;
4. the fields below, with no separator or trailing byte.

Each field is type byte, presence byte, uint32 big-endian payload length, then
payload. Types are UUID 0x01, int8 0x02, text 0x03 and boolean 0x04. Null is
presence 0x00 and zero length. Present is 0x01. UUID is uuid_send's 16 bytes.
Every integer, including stored smallint counts, is promoted to signed bigint
and encoded by int8send as eight-byte two's complement big-endian. Text is exact
UTF-8. Boolean payload is one byte, 0x00 false or 0x01 true.

Arguments always start with these canonical header fields, despite the Go/SQL
function parameter order:

1. operation_id text present;
2. action text present;
3. expected_controller_generation int8 present.

The remaining fields are the function arguments after removing operation_id and
expected_controller_generation. They appear once in their signature order.
workflow_kind is not a function argument and is excluded. The parent/action
compatibility check binds it separately.

Results encode these fields in this exact order:

1. operation_id text present;
2. action text present;
3. result_generation int8 present, the post-action controller generation;
4. result_kind text;
5. replica_id UUID nullable;
6. replica_kind text nullable;
7. replica_state text nullable;
8. desired_replicas int8 nullable;
9. active_serving_replicas int8 nullable;
10. active_maintenance_replicas int8 nullable;
11. partition_1_enabled boolean nullable;
12. partition_2_enabled boolean nullable;
13. capacity_generation int8 present;
14. controller_generation int8 present;
15. controller_operation_id text present;
16. admission_enabled boolean present;
17. lifecycle_phase text present;
18. write_gate text nullable;
19. write_generation int8 nullable.

This resolves the apparent repetition literally from lifecycle-replay.md:
controller generation is encoded first as the result header's result_generation
and again at field 14 because controller_generation is a stored result column.
The schema requires them equal. operation_id is encoded first and again through
stored controller_operation_id at field 15; the schema also requires those
equal. Removing either occurrence would change the accepted stream.

result_kind is encoded at field 4. It is stored replay evidence, not derived
from action during hashing. argument_digest, result_digest, expected_generation,
workflow_kind, argument_replaced_replica_id as a result, recorded_at and
replayed are not typed result columns and are excluded. replayed is deliberately
response-local.

## Exact action field lists

### prepare_scale_out

- Arguments: operation_id, action, expected_controller_generation.
- Result: the three result header fields, then result_kind=capacity; null
  replica_id/kind/state; desired, active-serving, active-maintenance,
  partition-1 and partition-2; capacity_generation, controller_generation,
  controller_operation_id, admission_enabled, lifecycle_phase; null write_gate
  and write_generation.

### activate_replica_capacity

- Arguments: operation_id, action, expected_controller_generation, replica_id,
  instance_id, container_instance_arn, caddy_task_arn, go_task_arn,
  nuxt_task_arn, release_digest, replaced_replica_id. The final UUID uses an
  explicit null field outside replacement_serving and a present field for a
  replacement.
- Result: the three result header fields, then result_kind=replica; replica_id,
  replica_kind, replica_state; five explicit null count/partition fields;
  capacity_generation, controller_generation, controller_operation_id,
  admission_enabled, lifecycle_phase; null write_gate and write_generation.

### prepare_scale_in

- Arguments: operation_id, action, expected_controller_generation, replica_id,
  instance_id, release_digest.
- Result: the three result header fields, then result_kind=replica; replica_id,
  replica_kind, replica_state=draining; five explicit null count/partition
  fields; capacity_generation, controller_generation, controller_operation_id,
  admission_enabled, lifecycle_phase; null write_gate and write_generation.

### finish_scale_in

- Arguments: operation_id, action, expected_controller_generation, replica_id,
  instance_id, release_digest, leave_operation_id, fencing_evidence_id.
  leave_operation_id is nullable for the abrupt branch. The accepted exact EC2
  audit proof makes fencing_evidence_id present in either branch.
- Result: the three result header fields, then result_kind=replica; replica_id,
  replica_kind, replica_state; desired, active-serving, active-maintenance,
  partition-1 and partition-2; capacity_generation, controller_generation,
  controller_operation_id, admission_enabled, lifecycle_phase; null write_gate
  and write_generation.

### prepare_maintenance_drain

- Arguments: operation_id, action, expected_controller_generation, replica_id,
  instance_id, release_digest.
- Result: the three result header fields, then result_kind=replica; replica_id,
  replica_kind=maintenance, replica_state=draining; five explicit null
  count/partition fields; capacity_generation, controller_generation,
  controller_operation_id, admission_enabled, lifecycle_phase; null write_gate
  and write_generation.

### begin_replica_termination

- Arguments: operation_id, action, expected_controller_generation, request_id,
  replica_id, instance_id, release_digest, reason. request_id remains before
  replica_id because that is its named function-signature order.
- Result: the three result header fields, then result_kind=replica; replica_id,
  replica_kind, replica_state=terminating; five explicit null count/partition
  fields; capacity_generation, controller_generation, controller_operation_id,
  admission_enabled, lifecycle_phase; null write_gate and write_generation.

### begin_wake

- Arguments: operation_id, action, expected_controller_generation, wake_mode,
  expected_write_generation. wake_mode text is exactly serving or maintenance.
- Result: the three result header fields, then result_kind=capacity; three
  explicit null replica fields; desired, active-serving, active-maintenance,
  partition-1 and partition-2; capacity_generation, controller_generation,
  controller_operation_id, admission_enabled, lifecycle_phase;
  write_gate=closing and write_generation present.

### complete_wake

- Arguments: operation_id, action, expected_controller_generation,
  expected_write_generation.
- Result: the three result header fields, then result_kind=capacity; three
  explicit null replica fields; desired, active-serving, active-maintenance,
  partition-1 and partition-2; capacity_generation, controller_generation,
  controller_operation_id, admission_enabled, lifecycle_phase; write_gate=open
  and write_generation present.

## Public synthetic fixture

Every vector uses expected controller generation 40 and result/controller
generation 41. UUIDs are 11111111-2222-4333-8444-555555555555 and replacement
aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee. Instance is i-0123456789abcdef0. Release
is `sha256:` followed by 64 lowercase a characters. The exact shared strings
are:

- container_instance_arn:
  arn:aws:ecs:ap-southeast-1:123456789012:container-instance/aboutme/fixture
- caddy_task_arn:
  arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/caddy-fixture
- go_task_arn: arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/go-fixture
- nuxt_task_arn:
  arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/nuxt-fixture
- release_digest:
  sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

These ten rows are independent encoding fixtures. They are not one executable
workflow. In particular, the common expected controller generation 40 and result
generation 41 cannot describe sequential begin-wake and complete-wake actions in
one operation.

Each fixture has these exact action-specific values. Fields not listed retain
the common values above. Every result stores controller_operation_id equal to
its operation_id and controller_generation 41.

- prepare_scale_out: operation_id op-scale-out; no extra arguments; capacity
  result desired=2, active-serving=1, active-maintenance=0, partition-1=true,
  partition-2=false, capacity-generation=71, admission=true, phase=online, write
  fields null.
- activate_null_replacement: operation_id op-activate-new; exact shared replica
  and identity fields; replaced-replica null; replica result kind=serving,
  state=active, all count/partition fields null, capacity-generation=71,
  admission=true, phase=online, write fields null.
- activate_nonnull_replacement: operation_id op-activate-replacement; exact
  shared replica and identity fields; replaced-replica
  aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee; the same active serving result as the
  null variant with controller_operation_id changed to this operation ID.
- prepare_scale_in: operation_id op-scale-in; replica, instance and release
  arguments; replica result kind=serving, state=draining, all count/partition
  fields null, capacity-generation=71, admission=true, phase=online, write
  fields null.
- finish_scale_in: operation_id op-scale-in-finish; replica, instance and
  release arguments, leave_operation_id leave-fixture and fencing_evidence_id
  proof-fixture; replica result kind=serving, state=left, desired=1,
  active-serving=1, active-maintenance=0, partition-1=true, partition-2=false,
  capacity-generation=71, admission=true, phase=online, write fields null.
- prepare_maintenance_drain: operation_id op-maint-drain; replica, instance and
  release arguments; replica result kind=maintenance, state=draining, all
  count/partition fields null, capacity-generation=71, admission=true,
  phase=online, write fields null.
- begin_replica_termination: operation_id op-terminate; request_id
  request-fixture, replica, instance, release and reason readiness_failed;
  replica result kind=serving, state=terminating, all count/partition fields
  null, capacity-generation=71, admission=true, phase=online, write fields null.
- begin_wake_serving: operation_id op-wake-serving; wake_mode serving and
  expected-write-generation=100; capacity result desired=1, active-serving=0,
  active-maintenance=0, both partitions=false, capacity-generation=71,
  admission=false, phase=starting, write_gate=closing, write-generation=101.
- begin_wake_maintenance: operation_id op-wake-maintenance; wake_mode
  maintenance and expected-write-generation=100; result values equal the serving
  wake except controller_operation_id equals op-wake-maintenance.
- complete_wake: operation_id op-wake-complete and
  expected-write-generation=101; capacity result desired=1, active-serving=0,
  active-maintenance=0, both partitions=false, capacity-generation=72,
  admission=true, phase=online, write_gate=open, write-generation=102.

| Fixture                      | Argument bytes | Argument SHA-256                                                 | Result bytes | Result SHA-256                                                   |
| ---------------------------- | -------------- | ---------------------------------------------------------------- | ------------ | ---------------------------------------------------------------- |
| prepare_scale_out            | 90             | cf000ef3bf307c8cdf230c419a40430e53390fe2cae60111951cc08a28a344f3 | 255          | 97583a4d21b893730e66b7862246cc28922a0227c3cded0674bf10bd83d5414c |
| activate_null_replacement    | 523            | a0cea0d8c5c4b701e413c009af40bdc1d09a696fb160494fe01f3a45e7a6e1b9 | 271          | bd3d991093d7ad39d56858eafbab2afb3aa27b7dd224059d6d37474741a5a8a2 |
| activate_nonnull_replacement | 547            | 22cabd95d5d83122cbec4395f2e1b0256190af5dbaff7eb7e14160b3d7a6c5c2 | 287          | 01ae3ed8ce5342cfcf18f2ebe547bbf1c63a1f0b9ea64c1b920669767409ab7f |
| prepare_scale_in             | 212            | 39c457fd9b3c30904396ce70488a72b21a559649a55f90569c4bc767271ba5b8 | 256          | 775a098df0a51ea8eadc869fe5ff36b8c0e04c811401250b45ace66ad3c52f04 |
| finish_scale_in              | 256            | dbf2187ecde3122476847d63589c3ff6ab15c72b10477fbf3b51d13ac1c9943a | 291          | 92cad6bfc24856d63242f115a379671a39724417a8335476564d809bd04eb64a |
| prepare_maintenance_drain    | 224            | 66beb1e1fa2549e66550ba9a9a898b225451293d89020eb6aac2640d0177c9ef | 275          | ba98747923b9791adfb3af8a8f057891a952ede94edcbcb660f5ab53b7b6c584 |
| begin_replica_termination    | 265            | 9b40c99609c143fa0ddcdd4a43383da128719b027178648a4531a2058b84836b | 270          | 7cd11ad109ec65a6de4075f6d59786fbeb984ad6d36dc63009d58fb65c2ff2eb |
| begin_wake_serving           | 113            | 4c2165a2ee539b0c45ba52f4605461547ea966ca12f5ef57f02659ae689a5119 | 271          | a97a896208cf389d17f85eabead18af5a388df346b618797cabb1a3760226dd9 |
| begin_wake_maintenance       | 121            | 6117c2414121a2755b65160441f5146a3a3712eaa983cd1bf469a01162c8840a | 279          | 95bfcd2e8956df38433cbe7cf3395e94f564c433e5ab2b121a1818566722ae2d |
| complete_wake                | 104            | c380d0ac76ef9e143cc8d49f54f214509e37bf1ef42596402d264a3821101abb | 271          | b7e07a8ca2aaaccfcb247b21b6976c3b77f9ed7c474a5b15eab3f13ccb75973d |

## Acceptance use

The migration installs one literal vector per action and both extra variants:
null/non-null activation replacement and serving/maintenance begin-wake. SQL
must compute every argument and result hash above from fixed literals. Go tests
repeat the same byte counts and hashes through an independent encoder. Changing
field order, omitting nulls, deriving result_kind, removing duplicated result
header fields or encoding smallint payloads in two bytes must fail.
