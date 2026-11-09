#!/usr/bin/env bash
# Start one Codex job with its role's model, effort, sandbox, and instructions.
# Codex and every sandboxed command get the caller's environment minus names
# that carry or locate credentials. workspace-write roles also get network
# access and writable roots for the Go, golangci-lint, and npm caches, $TMPDIR,
# and the main checkout's .dev/locks directory, which holds the shared
# local-check lock. /tmp is not writable, so every root is checked here and none
# may overlap the Git directory.
set -Eeuo pipefail

exec python3 -I - "$@" <<'PY'
import argparse
import os
from pathlib import Path
import re
import subprocess
import sys
import tomllib

CREDENTIAL_PREFIXES = ("ANTHROPIC_", "AWS_", "AZURE_", "CLAUDE_", "CLOUDFLARE_", "CLOUDSDK_",
                       "DOCKER_", "GH_", "GITHUB_", "GIT_", "GOOGLE_", "KUBE", "OP_", "SSH_",
                       "TF_VAR_", "VNGCLOUD_")
CREDENTIAL_WORDS = ("AUTH", "COOKIE", "CREDENTIAL", "JWT", "KEY", "NETRC", "PASS", "SECRET",
                    "TOKEN")


def toml_string(value):
    escapes = {"\\": "\\\\", '"': '\\"', "\b": "\\b", "\t": "\\t",
               "\n": "\\n", "\f": "\\f", "\r": "\\r"}
    return '"' + "".join(
        escapes.get(char, f"\\u{ord(char):04x}" if ord(char) < 32 or ord(char) == 127 else char)
        for char in value
    ) + '"'


def credential_name(name):
    name = name.upper()
    return name.startswith(CREDENTIAL_PREFIXES) or any(word in name for word in CREDENTIAL_WORDS)


def output(command, cwd, environment):
    return subprocess.run(command, cwd=cwd, env=environment, stdin=subprocess.DEVNULL,
                          capture_output=True, text=True, check=True).stdout


def user_cache_dir(environment):
    if sys.platform == "darwin":
        return Path.home() / "Library/Caches"
    return Path(environment.get("XDG_CACHE_HOME") or Path.home() / ".cache")


def overlaps(path, other):
    return path == other or path in other.parents or other in path.parents


def writable_roots(worktree, environment):
    """Return the cache, temp, and lock roots, creating each after validation."""
    try:
        common = Path(output(["git", "-C", worktree, "rev-parse", "--path-format=absolute",
                              "--git-common-dir"], worktree, environment).strip()).resolve()
        go_cache, go_modules = output(["go", "env", "GOCACHE", "GOMODCACHE"],
                                      worktree, environment).splitlines()
        npm_cache = output(["npm", "config", "get", "cache"], worktree, environment).strip()
    except (OSError, ValueError, subprocess.CalledProcessError):
        parser.error("cannot resolve the Git directory and the Go and npm caches")
    if common.name != ".git":
        parser.error("the main checkout has an unexpected Git layout")
    lint_cache = environment.get("GOLANGCI_LINT_CACHE") or user_cache_dir(environment) / "golangci-lint"
    locks = common.parent / ".dev/locks"
    for path in (locks.parent, locks):
        if path.is_symlink() or (path.exists() and not path.is_dir()):
            parser.error("the shared lock directory must be a real directory: " + str(path))
    named = {"GOCACHE": go_cache, "GOMODCACHE": go_modules, "GOLANGCI_LINT_CACHE": lint_cache,
             "npm_config_cache": npm_cache, "TMPDIR": environment.get("TMPDIR") or "/tmp"}
    roots = {}
    for name, value in [*named.items(), ("lock", locks)]:
        path = Path(value)
        if not path.is_absolute():
            parser.error(f"{name} must be an absolute path")
        path = path.resolve()
        if overlaps(path, common):
            parser.error(f"{name} overlaps the Git directory")
        if name != "lock" and overlaps(path, common.parent):
            parser.error(f"{name} overlaps the main checkout")
        roots[name] = path
    if roots["lock"] != locks:
        parser.error("the shared lock directory must not resolve elsewhere")
    try:
        for path in roots.values():
            # mkdir(parents=True) ignores mode for parents such as a new .dev.
            for directory in reversed([path, *path.parents]):
                directory.mkdir(mode=0o700, exist_ok=True)
    except OSError:
        parser.error("cannot create the writable roots")
    environment.update((name, str(roots[name])) for name in named)
    # zsh writes here-documents under TMPPREFIX, which defaults to /tmp/zsh.
    environment["TMPPREFIX"] = str(roots["TMPDIR"] / "zsh")
    return list(dict.fromkeys(str(path) for path in roots.values()))


parser = argparse.ArgumentParser(prog="scripts/codex-agent.sh")
parser.add_argument("--model")
parser.add_argument("--effort")
parser.add_argument("--search", action="store_true")
parser.add_argument("role")
parser.add_argument("worktree")
parser.add_argument("brief_file")
parser.add_argument("report_file")
args = parser.parse_args()
if not re.fullmatch(r"[a-z_]+", args.role):
    parser.error("role must match ^[a-z_]+$")

role_file = Path(args.worktree) / ".codex/agents" / f"{args.role}.toml"
try:
    with role_file.open("rb") as source:
        role = tomllib.load(source)
    model = args.model if args.model is not None else role["model"]
    effort = args.effort if args.effort is not None else role["model_reasoning_effort"]
    sandbox = role["sandbox_mode"]
    instructions = role["developer_instructions"]
    if not isinstance(sandbox, str) or sandbox not in {"read-only", "workspace-write"}:
        parser.error("invalid sandbox_mode")
    if not isinstance(effort, str) or effort not in {"low", "medium", "high", "xhigh"}:
        parser.error("invalid model_reasoning_effort")
    if not isinstance(model, str) or not re.fullmatch(r"gpt-[0-9]+(\.[0-9]+)?-[a-z]+", model):
        parser.error("invalid model")
    if not isinstance(instructions, str) or not instructions:
        parser.error("developer_instructions must be a nonempty string")
    brief = Path(args.brief_file).read_text(encoding="utf-8")
except (OSError, ValueError, KeyError) as error:
    parser.error(str(error))

environment = {name: value for name, value in os.environ.items() if not credential_name(name)}
# The login keychain stays reachable from the sandbox. Clearing Git's credential
# helpers stops an accidental push or authenticated fetch; it is no boundary.
environment.update({"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "credential.helper",
                    "GIT_CONFIG_VALUE_0": "", "GIT_TERMINAL_PROMPT": "0"})
# A shell snapshot replays the owner's interactive shell exports, including
# credentials, before every sandboxed command.
command = ["codex"] + (["--search"] if args.search else []) + [
    "exec", "-m", model, "-c", f"model_reasoning_effort={effort}",
    "-c", "developer_instructions=" + toml_string(instructions),
    "-c", "features.shell_snapshot=false", "-s", sandbox,
]
if sandbox == "workspace-write":
    # Keys from the Codex configuration reference for sandbox_mode = "workspace-write".
    roots = writable_roots(args.worktree, environment)
    command += ["-c", "sandbox_workspace_write.network_access=true",
                "-c", "sandbox_workspace_write.exclude_slash_tmp=true",
                "-c", "sandbox_workspace_write.writable_roots=["
                + ",".join(toml_string(root) for root in roots) + "]"]
command += ["-C", args.worktree, "--ephemeral", "-o", args.report_file, "--", brief]
with open(os.devnull, "rb") as source:
    os.dup2(source.fileno(), 0)
os.execvpe(command[0], command, environment)
PY
