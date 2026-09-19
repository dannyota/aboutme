# Bottlerocket host probe

This historical, throwaway OpenTofu root proved the host networking facts for
the
[single-host design](../../../docs/design/single-host-production.md#host-and-networking).
The [production runbook](../../../docs/runbooks/production.md) describes the
current production environment. The probe uses local state and the default VPC.
Resources incur charges until `tofu destroy` removes them.

```sh
tofu init && tofu apply
# Run, in order: aboutme-probe-host-server, aboutme-probe-bridge (wait for both
# to be RUNNING), then aboutme-probe-host-client, with
# aws ecs run-task --region ap-southeast-1 --cluster aboutme-probe --launch-type EC2 --task-definition <family>
aws logs filter-log-events --region ap-southeast-1 --log-group-name /aboutme/probe --filter-pattern CHECK
# Stop the tasks, deregister the container instance, then:
tofu destroy
```

Every `CHECK` line must read `PASS`, and the check 4 control must report a token
for the host-mode container.
