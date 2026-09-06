# Task 10.7: Caddy client-IP and origin boundary behind ALB

AC-INF-001; mechanism half of AC-OPS-002.

**Dependency:** Task 10.18's approved CloudFront-to-ALB-to-Caddy trust contract,
including its ALB target TLS and authentication model and exact forwarded-header
behavior. This task cannot reuse the old CloudFront-direct CIDR chain unchanged.

**Files:** production and shared Caddy configuration, Caddy image and
entrypoint, route-boundary tests, and the narrow Makefile diff owned by the
integration owner.

## Boundary contract

- Caddy accepts an external request only through the ALB target path selected by
  Task 10.18 and only when exactly one non-empty origin-secret header matches
  the current or next configured secret. Empty, repeated, missing, or wrong
  values fail before routing.
- The node security group admits only the ALB security group. Caddy also applies
  the application-layer source and target-authentication checks selected by Task
  10.18. The design accounts for ALB target TLS behavior and does not claim
  native target-certificate validation that AWS does not perform.
- Caddy derives one canonical viewer address from the exact ALB header behavior
  and trusted source chain proved by Task 10.18. It never trusts a
  viewer-supplied `X-Forwarded-For`, `X-Real-IP`, or `Forwarded` value. It
  strips all inbound forwarding and origin-secret headers, suppresses proxy
  defaults that would recreate them, and emits exactly one canonical `X-Real-IP`
  to colocated Go.
- Go remains loopback or deployment-private behind its colocated Caddy and keeps
  its configured trusted-proxy check. Caddy, Go, and Nuxt internal routes follow
  Task 10.18's one-replica-per-node placement and cannot cross replicas by
  accident.
- External `/print/**` and `/internal-render/**` remain denied before the
  default web proxy. The private redemption listener serves only the one-use
  operation through the paired replica route. Caddy's admin endpoint is
  disabled.
- Health and readiness endpoints are separate. The ALB health route proves the
  conditions Task 10.18 requires before admission; a loopback liveness route
  does not claim database, coordinator, render, SSE, or limiter readiness.
- Structured logs allow only the approved request metadata and canonical client
  address. Query strings, cookies, authorization, CSRF, forwarding headers,
  origin secrets, task credentials, and arbitrary values never enter logs.
- Startup fails closed when required secrets, trusted-source settings, paired
  routes, or TLS/authentication inputs are absent or malformed.

## Failing-first checks

- [ ] Use a real pinned Caddy binary with simulated CloudFront and ALB hops.
      Prove two viewers through one ALB target receive distinct canonical keys;
      forged header prefixes and repeated header instances cannot select a key.
- [ ] Prove current/next secret rotation, empty and repeated secret rejection,
      direct-node refusal, direct-ALB refusal without the CloudFront-origin
      contract, and closed startup with missing trust inputs.
- [ ] Prove exact route parity, public denial of both internal roots, paired
      private redemption and frozen-render routing, admin refusal, and distinct
      liveness/readiness behavior.
- [ ] Capture logs from hostile OAuth, MCP, cookie, CSRF, query, forwarding, and
      secret inputs. Assert the exact allowlisted field set and absence of every
      sentinel value.
- [ ] Run the production image with Task 10.5's security options. Prove the
      non-root UID, minimum capability set, read-only root, bounded writable
      mounts, and target listener selected by Task 10.18.
- [ ] Keep the existing development route-table test green without weakening its
      assertions. Add the production boundary target to CI through the
      integration owner.

**Verification:** existing and production Caddy route suites, entrypoint refusal
tests, in-image `caddy validate`, secret-log scan, and Task 10.18 topology
tests. Task 10.15 repeats the bypass and canonical-client checks through real
CloudFront, ALB, and ECS targets.
