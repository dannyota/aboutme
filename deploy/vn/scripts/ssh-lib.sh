# shellcheck shell=bash
# Laptop SSH port. Keep the default aligned with prod/variables.tf.
SSH_PORT=${SSH_PORT:-22922}
[[ $SSH_PORT =~ ^[0-9]{4,5}$ ]] && ((10#$SSH_PORT >= 1024 && 10#$SSH_PORT <= 32767)) || {
  echo "SSH_PORT must be an integer from 1024 to 32767" >&2
  exit 1
}
