import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, realpathSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, extname, isAbsolute, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

// Reuse the parser pinned by the repository's Markdown lint tool.
const require = createRequire(import.meta.url);
const lintRequire = createRequire(require.resolve("markdownlint-cli2"));
const MarkdownIt = lintRequire("markdown-it");
const markdown = new MarkdownIt({ html: true });
const indexes = [
  "docs/README.md",
  "docs/design/README.md",
  "docs/runbooks/README.md",
  "docs/research/README.md",
];

function git(root, args) {
  return execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function names(output) {
  return output.split("\0").filter(Boolean);
}

export function changedFiles(root, base = "HEAD", runGit = git) {
  // Resolve revisions before passing them to diff, so options cannot be injected.
  const commit = runGit(root, [
    "rev-parse",
    "--verify",
    "--end-of-options",
    `${base}^{commit}`,
  ]).trim();
  const changed = runGit(root, [
    "diff", "--name-only", "--no-renames", "-z", commit, "--",
  ]);
  const untracked = runGit(root, [
    "ls-files", "--others", "--exclude-standard", "-z",
  ]);
  return [...new Set([...names(changed), ...names(untracked)])];
}

function inside(root, file) {
  const path = relative(root, file);
  return !isAbsolute(path) && path !== ".." && !path.startsWith(`..${sep}`);
}

function localTarget(root, source, href) {
  if (/^[a-z][a-z\d+.-]*:/iu.test(href) || href.startsWith("//")) return null;
  const hash = href.indexOf("#");
  const path = (hash < 0 ? href : href.slice(0, hash)).split("?")[0];
  const fragment = hash < 0 ? "" : decodeURIComponent(href.slice(hash + 1));
  const decoded = decodeURIComponent(path);
  let file = resolve(root, source);
  if (decoded) {
    file = decoded.startsWith("/")
      ? resolve(root, `.${decoded}`)
      : resolve(dirname(file), decoded);
  }
  if (!inside(root, file)) throw new Error("outside repository");
  return { file, fragment };
}

function inlineText(tokens = []) {
  return tokens
    .map((token) => {
      if (token.type === "text" || token.type === "code_inline") return token.content;
      if (token.type === "softbreak" || token.type === "hardbreak") return " ";
      if (token.type === "image") return inlineText(token.children);
      return "";
    })
    .join("");
}

function srcsetURLs(value) {
  const urls = [];
  let position = 0;
  while (position < value.length) {
    while (position < value.length && /[\t\n\f\r ,]/u.test(value[position])) position++;
    const start = position;
    while (position < value.length && !/[\t\n\f\r ]/u.test(value[position])) position++;
    const url = value.slice(start, position);
    if (!url) break;
    // Commas inside URLs belong to the URL, including data URL payloads.
    urls.push(url.replace(/,+$/u, ""));
    if (url.endsWith(",")) continue;
    // Skip width or density descriptors up to the next candidate.
    while (position < value.length && value[position] !== ",") position++;
  }
  return urls;
}

function parse(text) {
  const links = [];
  const anchors = new Set();
  const slugs = new Set();
  const tokens = markdown.parse(text, {});
  const addHTML = (html, line) => {
    const visible = html.replace(/<!--[\s\S]*?-->/gu, "");
    for (const tag of visible.matchAll(/<[a-z][^>]*>/giu)) {
      const attributes = /\s+([^\s=/>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/gu;
      for (const attr of tag[0].matchAll(attributes)) {
        if (!/^(?:href|src|srcset|id|name)$/iu.test(attr[1])) continue;
        const value = markdown.utils.unescapeAll(attr[2] ?? attr[3] ?? attr[4] ?? "");
        if (/^(?:id|name)$/iu.test(attr[1])) anchors.add(value);
        else if (/^srcset$/iu.test(attr[1])) {
          for (const href of srcsetURLs(value)) links.push({ href, line });
        } else links.push({ href: value, line });
      }
    }
  };
  const visit = (children, line) => {
    for (const token of children) {
      if (token.type === "link_open") {
        links.push({ href: token.attrGet("href"), line });
      }
      if (token.type === "image") {
        links.push({ href: token.attrGet("src"), line });
      }
      if (token.type === "html_inline") addHTML(token.content, line);
      if (token.children) visit(token.children, line);
      if (token.type === "softbreak" || token.type === "hardbreak") line++;
    }
  };
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i];
    const line = (token.map?.[0] ?? 0) + 1;
    if (token.type === "heading_open") {
      const base = inlineText(tokens[i + 1]?.children)
        .toLowerCase()
        .replace(/[^\p{L}\p{M}\p{N}\p{Pc}\- ]/gu, "")
        .replace(/ /gu, "-");
      let slug = base;
      for (let suffix = 1; slugs.has(slug); suffix++) slug = `${base}-${suffix}`;
      slugs.add(slug);
      anchors.add(slug);
    }
    if (token.type === "inline") visit(token.children ?? [], line);
    if (token.type === "html_block") addHTML(token.content, line);
  }
  return { links, anchors };
}

function readMarkdown(root, file) {
  if (!inside(root, realpathSync(file))) throw new Error("outside repository");
  return parse(readFileSync(file, "utf8"));
}

// Check only selected sources. Linked documents supply anchors, not another crawl.
export function checkFiles(root, files) {
  root = realpathSync(root);
  const errors = [];
  const cache = new Map();
  const document = (file) => {
    if (!cache.has(file)) cache.set(file, readMarkdown(root, file));
    return cache.get(file);
  };
  for (const source of [...new Set(files)].sort()) {
    let links;
    try {
      links = document(resolve(root, source)).links;
    } catch (error) {
      errors.push(`${source}: ${error.code === "ENOENT" ? "missing source" : error.message}`);
      continue;
    }
    for (const { href, line } of links) {
      const fail = (reason) => errors.push(`${source}:${line}: ${reason}: ${href}`);
      try {
        const target = localTarget(root, source, href);
        if (!target) continue;
        let { file } = target;
        if (!existsSync(file)) {
          fail("missing file");
          continue;
        }
        if (!inside(root, realpathSync(file))) {
          fail("outside repository");
          continue;
        }
        if (statSync(file).isDirectory()) {
          if (!target.fragment) continue;
          file = resolve(file, "README.md");
          if (!existsSync(file)) {
            fail("directory anchor needs README.md");
            continue;
          }
        }
        if (
          target.fragment && extname(file).toLowerCase() === ".md" &&
          !document(file).anchors.has(target.fragment)
        ) fail("missing anchor");
      } catch (error) {
        fail(error instanceof URIError ? "invalid URL encoding" : error.message);
      }
    }
  }
  return errors;
}

export function selectFiles(root, changed, available) {
  const present = new Set(available.filter((file) => existsSync(resolve(root, file))));
  const selected = new Set(indexes.filter((file) => present.has(file)));
  const changedPaths = new Set(changed.map((file) => resolve(root, file)));
  for (const file of changed) {
    if (extname(file).toLowerCase() === ".md" && present.has(file)) selected.add(file);
    for (let dir = dirname(file); dir !== "."; dir = dirname(dir)) {
      const index = `${dir}/README.md`;
      if (present.has(index)) selected.add(index);
    }
  }
  // Include indexes elsewhere that link to a changed or deleted target.
  for (const file of present) {
    if (!/(^|\/)README\.md$/u.test(file) || selected.has(file)) continue;
    const { links } = readMarkdown(root, resolve(root, file));
    for (const { href } of links) {
      let target;
      try {
        target = localTarget(root, file, href);
      } catch {
        continue;
      }
      if (target && (
        changedPaths.has(target.file) ||
        changedPaths.has(resolve(target.file, "README.md"))
      )) {
        selected.add(file);
        break;
      }
    }
  }
  return [...selected].sort();
}

function main() {
  const args = process.argv.slice(2);
  if (args.length && (args.length !== 2 || args[0] !== "--base" || !args[1])) {
    throw new Error("usage: node scripts/check-doc-links.mjs [--base <commit>]");
  }
  const root = git(process.cwd(), ["rev-parse", "--show-toplevel"]).trim();
  const base = args[1] ?? process.env.DOC_LINK_BASE ?? "HEAD";
  const changed = changedFiles(root, base);
  const available = names(git(root, [
    "ls-files", "--cached", "--others", "--exclude-standard", "-z",
  ]));
  const files = selectFiles(root, changed, available);
  const errors = checkFiles(root, files);
  for (const error of errors) console.error(error);
  if (errors.length) process.exitCode = 1;
  else console.log(`check-doc-links: ok (${files.length} Markdown files)`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main();
  } catch {
    console.error("check-doc-links: could not select files; check the base commit and repository paths");
    process.exitCode = 1;
  }
}
