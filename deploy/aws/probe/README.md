# Bottlerocket host probe

A throwaway OpenTofu root that proves the host networking facts the
[single-host design](../../../docs/design/single-host-production.md#host-and-networking)
depends on. It uses local state and the default VPC, and costs a few cents.

```sh
tofu init && tofu apply
# Run, in order: aboutme-probe-host-server, aboutme-probe-bridge (wait for both
# to be RUNNING), then aboutme-probe-host-client, with
# aws ecs run-task --cluster aboutme-probe --launch-type EC2 --task-definition <family>
aws logs filter-log-events --log-group-name /aboutme/probe --filter-pattern CHECK
# Stop the tasks, deregister the container instance, then:
tofu destroy
```

Every `CHECK` line must read `PASS`, and the check 4 control must report a token
for the host-mode container.
