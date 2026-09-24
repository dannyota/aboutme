# Sourced by deploy.sh. Implements the notification-suppression protocol
# around the app/maintenance handoff (docs/runbooks/production.md, "Deploy").
# Needs $site_alarm_region, $work, say(), aws_(), aws_site_alarm_(),
# fence_checkpoint, and the tracking variables site_alarm_restore,
# task_stopped_rule_restore, site_up_epoch, signaled, and site_alarm_waited
# already defined by deploy.sh.

# The planned handoff stops the app and starts maintenance, so suppress only
# the resulting site-down and stopped-task notifications. Database, capacity,
# Scheduler, and host recovery alarms remain live throughout the deploy.
restore_deploy_notifications() { # [1 to wait for the site-down alarm's post-recovery health first]
  local failed=0 actions state wait_for_alarm=${1:-0}
  if ((task_stopped_rule_restore)); then
    if ! aws_ events enable-rule --name aboutme-prod-task-stopped >/dev/null; then
      say "could not re-enable the task-stopped notification rule"
      failed=1
    else
      state=$(aws_ events describe-rule --name aboutme-prod-task-stopped --query State --output text) || state=""
      if [[ $state != ENABLED ]]; then
        say "task-stopped notification rule is '$state', want ENABLED"
        failed=1
      else
        task_stopped_rule_restore=0
      fi
    fi
  fi
  if ((site_alarm_restore)); then
    # A finished run passes 1 and waits, whether it finished by success or a
    # later failure such as a warm-up or smoke check, unless this exit already
    # ran the wait once. A signal during the wait stops it early and restores
    # at once instead of blocking on the alarm's evaluation delay.
    if ((wait_for_alarm)); then
      wait_for_site_alarm_healthy || failed=1
    fi
    if ! aws_site_alarm_ cloudwatch enable-alarm-actions --alarm-names aboutme-prod-site-down >/dev/null; then
      say "could not re-enable site-down alarm actions"
      failed=1
    else
      actions=$(aws_site_alarm_ cloudwatch describe-alarms --alarm-names aboutme-prod-site-down \
        --query 'MetricAlarms[0].ActionsEnabled' --output text) || actions=""
      if [[ $actions != True ]]; then
        say "site-down alarm actions are '$actions', want True"
        failed=1
      else
        site_alarm_restore=0
      fi
    fi
  fi
  return "$failed"
}

pause_deploy_notifications() {
  local actions state
  actions=$(aws_site_alarm_ cloudwatch describe-alarms --alarm-names aboutme-prod-site-down \
    --query 'MetricAlarms[0].ActionsEnabled' --output text) || { say "could not read site-down alarm actions"; return 1; }
  case $actions in True|False) ;; *) say "site-down alarm actions are '$actions', want True or False"; return 1;; esac
  state=$(aws_ events describe-rule --name aboutme-prod-task-stopped --query State --output text) ||
    { say "could not read the task-stopped notification rule"; return 1; }
  case $state in ENABLED|DISABLED) ;; *) say "task-stopped notification rule is '$state', want ENABLED or DISABLED"; return 1;; esac

  say "deployment notification states: site-down actions=$actions, task-stopped rule=$state"

  if [[ $actions == True ]]; then
    fence_checkpoint || { say "fence checkpoint failed before pausing notifications"; return 1; }
    # Mark first because AWS can accept a request even when the client loses its
    # response. Cleanup must then treat the action as possibly disabled.
    site_alarm_restore=1
    if ! aws_site_alarm_ cloudwatch disable-alarm-actions --alarm-names aboutme-prod-site-down >/dev/null; then
      say "could not disable site-down alarm actions"
      restore_deploy_notifications || true
      return 1
    fi
    actions=$(aws_site_alarm_ cloudwatch describe-alarms --alarm-names aboutme-prod-site-down \
      --query 'MetricAlarms[0].ActionsEnabled' --output text) || actions=""
    if [[ $actions != False ]]; then
      say "site-down alarm actions are '$actions', want False before the handoff"
      restore_deploy_notifications || true
      return 1
    fi
  fi

  if [[ $state == ENABLED ]]; then
    fence_checkpoint || { say "fence checkpoint failed before pausing notifications"; return 1; }
    task_stopped_rule_restore=1
    if ! aws_ events disable-rule --name aboutme-prod-task-stopped >/dev/null; then
      say "could not disable the task-stopped notification rule"
      restore_deploy_notifications || true
      return 1
    fi
    state=$(aws_ events describe-rule --name aboutme-prod-task-stopped --query State --output text) || state=""
    if [[ $state != DISABLED ]]; then
      say "task-stopped notification rule is '$state', want DISABLED before the handoff"
      restore_deploy_notifications || true
      return 1
    fi
  fi
}

# Waits until the site-down alarm has fully reevaluated the recovery: its
# StateValue reads OK, its shape matches this deploy's expectations
# (ComparisonOperator LessThanThreshold, Statistic Minimum, from the same
# describe read), and Route 53 has reported, for the alarm's own health
# check, EvaluationPeriods + 1 healthy minutes among the newest datapoints
# returned, all timestamped at or after the minute the site came back up
# (site_up_epoch, rounded up). CloudWatch evaluates the alarm on a delay
# behind Route 53, so after a short maintenance window StateValue can still
# read OK while the window's unhealthy datapoints are not yet evaluated;
# StateValue alone does not prove recovery, and the extra minute of margin
# beyond EvaluationPeriods absorbs the evaluator's own lag behind the metric
# read.
#
# Bounded by DEPLOY_ALARM_WAIT seconds total (default 900), polled every
# DEPLOY_ALARM_POLL seconds (default 20): by attempt count, computed from
# those two values so the stubbed test harness can bound it without a real
# wait, and by wall clock, so a slow AWS call cannot stretch the wait far past
# its bound. A describe or metric read error counts as not-yet-healthy rather
# than failing the wait outright; its last stderr line is kept and, if
# nothing later succeeds, named in the timeout message next to a shape
# mismatch when one was seen. The wait logs the minute it treats as the
# site's recovery start, so a caller can compare it against the metric read's
# own start time.
#
# Runs a read command with fd 2 pointed at $work/alarm-read.err for its
# duration only, so a failing call's stderr can be inspected without an
# explicit redirection on the command itself. on_exit points fd 2 back at fd 9
# in case a signal lands while the redirection is in place.
read_capturing_stderr() { # out-var-name command...
  local -n _out=$1
  shift
  exec 3>&2 2>"$work/alarm-read.err"
  _out=$("$@")
  local rc=$?
  exec 2>&3 3>&-
  return "$rc"
}

# cloudwatch:GetMetricStatistics is outside the deploy role's closed IAM list
# (deploy/aws/modules/identity/main.tf), so the metric read runs under the
# base caller's own credentials, the same way origin_ip uses them for
# ec2:DescribeAddresses.
wait_for_site_alarm_healthy() {
  local poll=${DEPLOY_ALARM_POLL:-20} wait_s=${DEPLOY_ALARM_WAIT:-900}
  local poll_for_count=$((poll > 0 ? poll : 1))
  local attempts=$((wait_s / poll_for_count))
  ((attempts > 0)) || attempts=1
  local up_min=$(( (site_up_epoch + 59) / 60 * 60 )) start_iso
  TZ=UTC printf -v start_iso '%(%Y-%m-%dT%H:%M:%SZ)T' "$up_min"
  say "waiting for the site-down alarm (healthy minutes since $start_iso)"
  local deadline=$((EPOCHSECONDS + wait_s))
  local attempt desc state periods period threshold hc_id comparison statistic
  local last_state="unknown" last_periods=0 last_healthy=0
  local last_read_error="" last_shape_error=""
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if ((signaled)); then
      site_alarm_waited=1
      say "skipping the site-down alarm wait; re-enabling its actions now"
      return 1
    fi
    ((EPOCHSECONDS <= deadline)) || break
    if read_capturing_stderr desc aws_site_alarm_ cloudwatch describe-alarms \
        --alarm-names aboutme-prod-site-down --output json; then
      last_read_error=""
      state=$(jq -r '.MetricAlarms[0].StateValue // empty' <<<"$desc")
      periods=$(jq -r '.MetricAlarms[0].EvaluationPeriods // empty' <<<"$desc")
      period=$(jq -r '.MetricAlarms[0].Period // empty' <<<"$desc")
      threshold=$(jq -r '.MetricAlarms[0].Threshold // empty' <<<"$desc")
      hc_id=$(jq -r '.MetricAlarms[0].Dimensions[]? | select(.Name == "HealthCheckId") | .Value' <<<"$desc")
      comparison=$(jq -r '.MetricAlarms[0].ComparisonOperator // empty' <<<"$desc")
      statistic=$(jq -r '.MetricAlarms[0].Statistic // empty' <<<"$desc")
      if [[ -n $state && $periods =~ ^[0-9]+$ && $period =~ ^[0-9]+$ && -n $threshold && -n $hc_id ]]; then
        last_state=$state
        last_periods=$((periods + 1))
        if [[ $comparison != LessThanThreshold || $statistic != Minimum ]]; then
          last_shape_error="unexpected alarm shape: $comparison/$statistic"
        elif [[ $state == OK ]]; then
          last_shape_error=""
          local end_iso margin stats healthy
          TZ=UTC printf -v end_iso '%(%Y-%m-%dT%H:%M:%SZ)T' "$EPOCHSECONDS"
          margin=$((periods + 1))
          if read_capturing_stderr stats aws --region "$site_alarm_region" cloudwatch get-metric-statistics \
              --namespace AWS/Route53 --metric-name HealthCheckStatus --statistic Minimum \
              --period "$period" --start-time "$start_iso" --end-time "$end_iso" \
              --dimensions "Name=HealthCheckId,Value=$hc_id" --output json; then
            last_read_error=""
            # Count the healthy datapoints at the end of the series: a healthy
            # minute followed by an unhealthy one does not count.
            healthy=$(jq --argjson t "$threshold" --argjson n "$margin" \
              '[.Datapoints // [] | sort_by(.Timestamp) | reverse | .[] | (.Minimum >= $t)]
               | (index(false) // length) | if . > $n then $n else . end' <<<"$stats" 2>/dev/null) || healthy=0
            [[ $healthy =~ ^[0-9]+$ ]] || healthy=0
            last_healthy=$healthy
            if ((healthy >= margin)); then
              site_alarm_waited=1
              return 0
            fi
          else
            last_read_error=$(tail -n1 "$work/alarm-read.err" 2>/dev/null)
          fi
        else
          last_shape_error=""
        fi
      fi
    else
      last_read_error=$(tail -n1 "$work/alarm-read.err" 2>/dev/null)
    fi
    # A Ctrl-C can kill the read itself, so check before sleeping as well as
    # at the top of the next attempt.
    if ((signaled)); then
      site_alarm_waited=1
      say "skipping the site-down alarm wait; re-enabling its actions now"
      return 1
    fi
    ((attempt == attempts)) || sleep "$poll"
  done
  site_alarm_waited=1
  local msg="site-down alarm is '$last_state' and Route 53 reported $last_healthy of $last_periods healthy minutes since the site came up after $wait_s s; re-enabling its actions; check the site"
  [[ -z $last_shape_error ]] || msg+="; $last_shape_error"
  [[ -z $last_read_error ]] || msg+="; last read error: $last_read_error"
  say "$msg"
  return 1
}
