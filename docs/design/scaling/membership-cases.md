# Required membership cases

These cases verify [membership](replica-membership.md) and
[evidence operations](membership-evidence.md).

- Reject malformed, partial, duplicate, or reused instance, container, and task
  tuples, plus a changed release digest for one UUID. Two replicas may share one
  release digest. Concurrent cross-role reuse of one task ARN admits exactly one
  complete trio; deferred constraints reject a missing, extra, or mismatched
  child.
- Registration and ready replay require the exact immutable tuple. Registration
  replay after later activation/leave/fence returns the current state with
  replayed=true and performs no mutation; it does not reconstruct the original
  registration result. Ready never activates capacity. Two pools racing the
  final serving slot admit one replica.
- Drain blocks new claims and transitions for the exact selected incarnation.
  Graceful leave requires local work joined, zero live claims, and no visible
  closing or unresolved transition. It cannot release work on the caller's word.
- Loss of RDS, ECS, ALB, a heartbeat, an advisory lock, or a deadline never
  fences. Exact EC2 terminated proof for the exact tuple fences an ungraceful
  incarnation and releases only its claims. Proof for `left` adds audit evidence
  without changing the terminal state.
- Proof can commit while a transition parent is locked. It leaves that
  transition unchanged. Only a later, separate lifecycle transaction may recover
  a still closing transition through exact fencing evidence.
- Every real role can execute only its named functions. PUBLIC, direct DML,
  helper execution, forged evidence, and cross-role calls fail.
- Register, ready, leave, and proof each increment capacity generation once and
  leave controller generation unchanged. Exact replay, rejection, rollback, and
  read-only resolution increment neither. Fenced-transition recovery changes
  neither membership generation.
- Two-pool tests cover registration versus transition, activation versus
  transition, claim acquisition versus drain/fence, leave versus release, and
  fencing versus ack without a reverse transition-parent lock edge.
- Loss and proof preserve logical partition flags with one or zero survivors.
  First serving activation enables partition 1; second enables partition 2;
  maintenance activation changes neither; proved scale-in disables partition 2.
