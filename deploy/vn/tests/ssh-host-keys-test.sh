#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
dropin=$repo_root/deploy/vn/host/etc/systemd/system/ssh-keygen.service.d/90-aboutme.conf
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/root/etc/ssh"

# Apply systemd's ExecStart reset to the provider's failing command. The
# resulting command runs against a temporary key directory, never /etc/ssh.
printf '[Service]\nExecStart=/usr/local/sbin/runtime-ssh-host-keys.sh\n' >"$work/vendor.service"
mapfile -t commands < <(awk '
  /^ExecStart=/ {
    command = substr($0, 11)
    if (command == "") result = ""
    else result = result command "\n"
  }
  END { printf "%s", result }
' "$work/vendor.service" "$dropin")
((${#commands[@]} == 1)) || { echo "host key service needs one command" >&2; exit 1; }
read -r -a command <<<"${commands[0]}"
[[ ${command[*]} == '/usr/bin/ssh-keygen -A' ]] || {
  echo "host key service did not select safe default key generation" >&2
  exit 1
}

generate() {
  "${command[@]}" -f "$work/root" >"$work/keygen.out" 2>"$work/keygen.err"
}

generate
for type in rsa ecdsa ed25519; do
  key=$work/root/etc/ssh/ssh_host_${type}_key
  [[ -s $key && -s $key.pub ]] || { echo "a supported host key is missing" >&2; exit 1; }
  [[ $(stat -c %a "$key") == 600 ]] || { echo "host key permissions are not private" >&2; exit 1; }
done
[[ ! -e $work/root/etc/ssh/ssh_host_dsa_key ]] || { echo "a DSA host key was generated" >&2; exit 1; }

sha256sum "$work/root/etc/ssh/"* >"$work/before.sha"
generate
sha256sum "$work/root/etc/ssh/"* >"$work/after.sha"
cmp -s "$work/before.sha" "$work/after.sha" || { echo "existing host keys changed" >&2; exit 1; }

# A missing key can be filled without replacing another host identity.
cp "$work/root/etc/ssh/ssh_host_ed25519_key" "$work/kept-key"
cp "$work/root/etc/ssh/ssh_host_ed25519_key.pub" "$work/kept-key.pub"
rm "$work/root/etc/ssh/ssh_host_ecdsa_key" "$work/root/etc/ssh/ssh_host_ecdsa_key.pub"
generate
[[ -s $work/root/etc/ssh/ssh_host_ecdsa_key && -s $work/root/etc/ssh/ssh_host_ecdsa_key.pub ]] || {
  echo "the missing host key was not generated" >&2
  exit 1
}
cmp -s "$work/kept-key" "$work/root/etc/ssh/ssh_host_ed25519_key" || { echo "an existing private key changed" >&2; exit 1; }
cmp -s "$work/kept-key.pub" "$work/root/etc/ssh/ssh_host_ed25519_key.pub" || { echo "an existing public key changed" >&2; exit 1; }

echo "SSH host key tests passed"
