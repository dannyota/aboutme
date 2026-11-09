#!/usr/bin/env bash
# Start one Codex job with its role's model, effort, sandbox, and instructions.
# Codex and every sandboxed command get the caller's environment minus names
# that carry or locate credentials, without shell snapshots, login shells, or
# the owner's shell startup files, and with a new private $TMPDIR per launch.
# workspace-write roles also get network access and writable roots for fixed
# per-user Go, golangci-lint, and npm caches, that $TMPDIR, and the main
# checkout's .dev/locks directory, which holds the shared local-check lock.
# Checkout configuration never chooses a root, no root may be or sit under a
# symbolic link, /tmp is not writable, and no root may reach Git metadata, the
# main checkout, the scratchpad, credential stores, or $HOME itself.
set -Eeuo pipefail

exec python3 -I - "$@" <<'PY'
import argparse
import os
from pathlib import Path
import pwd
import re
import subprocess
import sys
import tempfile
import tomllib

CREDENTIAL_PREFIXES = ("ANTHROPIC_", "AWS_", "AZURE_", "CLAUDE_", "CLOUDFLARE_", "CLOUDSDK_",
                       "DOCKER_", "GH_", "GITHUB_", "GIT_", "GOOGLE_", "KUBE", "OP_", "SSH_",
                       "TF_VAR_", "VNGCLOUD_")
CREDENTIAL_WORDS = ("AUTH", "COOKIE", "CREDENTIAL", "JWT", "KEY", "NETRC", "PASS", "SECRET",
                    "TOKEN")
# Shell startup hooks that would run the owner's files before sandboxed commands.
STARTUP_NAMES = {"BASH_ENV", "ENV", "ZDOTDIR"}
# Credential stores under $HOME that no writable root may reach. Roots are
# fixed and free of symbolic links, so this list is defense in depth.
SECRET_PATHS = (".ssh", ".config", ".codex", ".gnupg", ".aws", ".vngcloud", ".docker", ".kube",
                ".netrc", ".npmrc", ".pypirc", "Library/Keychains", "Library/Application Support",
                ".local/share/keyrings")


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


def overlaps(path, other):
    return path == other or path in other.parents or other in path.parents


def symbolic_link(base, path):
    """Return the first symbolic link below base on the way to path, if any."""
    parts = path.relative_to(base).parts
    for depth in range(1, len(parts) + 1):
        if base.joinpath(*parts[:depth]).is_symlink():
            return base.joinpath(*parts[:depth])
    return None


def scratchpad(args):
    """Return the directories of the worktree, brief, and report, and the session
    scratchpad that holds all three unless that is / or holds the account home."""
    places = [Path(path).resolve() for path in (args.worktree, args.brief_file, args.report_file)]
    found = {place.parent for place in places}
    shared = Path(os.path.commonpath([str(place) for place in places]))
    account = Path(pwd.getpwuid(os.getuid()).pw_dir).resolve()
    if shared != Path(shared.anchor) and shared != account and shared not in account.parents:
        found.add(shared)
    return found


def system_temp_dir():
    """Return the per-user system temp directory, ignoring the caller's TMPDIR."""
    if sys.platform != "darwin":
        return Path("/tmp").resolve()
    try:
        found = subprocess.run(["getconf", "DARWIN_USER_TEMP_DIR"], stdin=subprocess.DEVNULL,
                               capture_output=True, text=True, check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        found = ""
    if not found.startswith("/"):
        parser.error("cannot find the per-user temp directory")
    return Path(found).resolve()


def run_temp_dir(base):
    """Create this launch's private temp directory and an empty ZDOTDIR in it."""
    try:
        temp = Path(tempfile.mkdtemp(prefix="aboutme-codex.", dir=base))
        (temp / "zdotdir").mkdir(mode=0o700)
    except OSError:
        parser.error("cannot create the temp directory")
    return temp


def writable_roots(args, environment, temp_base):
    """Validate fixed roots, then create them and the temp root; return all roots."""
    try:
        common = subprocess.run(["git", "-C", args.worktree, "rev-parse", "--path-format=absolute",
                                 "--git-common-dir"], env=environment, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        parser.error("cannot find the Git directory")
    common = Path(common).resolve()
    if common.name != ".git":
        parser.error("the main checkout has an unexpected Git layout")
    main, home = common.parent, Path.home().resolve()
    locks = main / ".dev/locks"
    for path in (locks.parent, locks):
        if path.is_symlink() or (path.exists() and not path.is_dir()):
            parser.error("the shared lock directory must be a real directory: " + str(path))
    cache = home / ("Library/Caches" if sys.platform == "darwin" else ".cache")
    caches = {"GOCACHE": cache / "go-build", "GOMODCACHE": home / "go/pkg/mod",
              "GOLANGCI_LINT_CACHE": cache / "golangci-lint", "npm_config_cache": home / ".npm"}
    for name, path in caches.items():
        link = symbolic_link(home, path)
        if link is not None:
            parser.error(f"{name}: {link} is a symbolic link; remove it so the cache is a real "
                         "directory under $HOME")
    # The temp root is a new directory under the base, checked before it exists.
    named = {**caches, "TMPDIR": temp_base / "aboutme-codex.new"}
    secrets = {path for name in SECRET_PATHS for path in (home / name, (home / name).resolve())}
    scratch = scratchpad(args)

    def check(name, value, protected):
        path = Path(value).resolve()
        if path != Path(value):
            parser.error(f"{name}: {value} resolves elsewhere through a symbolic link")
        if overlaps(path, common):
            parser.error(f"{name} overlaps the Git directory")
        if path == home or path in home.parents:
            parser.error(f"{name} covers $HOME")
        for other in sorted(protected):
            if overlaps(path, other):
                parser.error(f"{name} overlaps a protected path: {other}")
        return path

    roots = {name: check(name, value, secrets | scratch | {main}) for name, value in named.items()}
    roots["lock"] = check("the shared lock directory", locks, secrets)
    if roots["lock"] != locks:
        parser.error("the shared lock directory must not resolve elsewhere")
    roots["TMPDIR"] = run_temp_dir(temp_base)
    try:
        for path in roots.values():
            # mkdir(parents=True) ignores mode for parents such as a new .dev.
            for directory in reversed([path, *path.parents]):
                directory.mkdir(mode=0o700, exist_ok=True)
        # local-check.py also locks the pre-move .git lock until no branch older
        # than the .dev/locks move remains; a sandbox can lock it but not create it.
        os.close(os.open(common / "aboutme-local-check.lock",
                         os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600))
    except OSError:
        parser.error("cannot create the writable roots")
    environment.update((name, str(roots[name])) for name in named)
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

environment = {name: value for name, value in os.environ.items()
               if not credential_name(name) and name not in STARTUP_NAMES}
# The login keychain stays reachable from the sandbox. Clearing Git's credential
# helpers stops an accidental push or authenticated fetch; it is no boundary.
environment.update({"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "credential.helper",
                    "GIT_CONFIG_VALUE_0": "", "GIT_TERMINAL_PROMPT": "0"})
# A shell snapshot replays the owner's interactive shell exports, and a login
# shell reads the owner's profile; either can restore a removed credential.
command = ["codex"] + (["--search"] if args.search else []) + [
    "exec", "-m", model, "-c", f"model_reasoning_effort={effort}",
    "-c", "developer_instructions=" + toml_string(instructions),
    "-c", "features.shell_snapshot=false", "-c", "allow_login_shell=false", "-s", sandbox,
]
temp_base = system_temp_dir()
if sandbox == "workspace-write":
    # Keys from the Codex configuration reference for sandbox_mode = "workspace-write".
    roots = writable_roots(args, environment, temp_base)
    temp = Path(environment["TMPDIR"])
    command += ["-c", "sandbox_workspace_write.network_access=true",
                "-c", "sandbox_workspace_write.exclude_slash_tmp=true",
                "-c", "sandbox_workspace_write.writable_roots=["
                + ",".join(toml_string(root) for root in roots) + "]"]
else:
    temp = run_temp_dir(temp_base)
# zsh reads startup files from ZDOTDIR, here empty, and writes here-documents
# under TMPPREFIX, which defaults to /tmp/zsh.
environment.update({"TMPDIR": str(temp), "TMPPREFIX": str(temp / "zsh"),
                    "ZDOTDIR": str(temp / "zdotdir")})
command += ["-C", args.worktree, "--ephemeral", "-o", args.report_file, "--", brief]
with open(os.devnull, "rb") as source:
    os.dup2(source.fileno(), 0)
os.execvpe(command[0], command, environment)
PY
