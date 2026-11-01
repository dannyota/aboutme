import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, symlinkSync, writeFileSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { checkFiles, selectFiles, changedFiles } from "../check-doc-links.mjs";

function fixture(t, files) {
  const root = mkdtempSync(join(tmpdir(), "aboutme-doc-links-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const [file, content] of Object.entries(files)) {
    mkdirSync(dirname(join(root, file)), { recursive: true });
    writeFileSync(join(root, file), content);
  }
  return root;
}

test("reports missing files and anchors with source locations", (t) => {
  const root = fixture(t, {
    "docs/page.md": "# Page\n[missing](absent.md)\n[anchor](target.md#absent)\n",
    "docs/target.md": "# Present\n",
  });
  assert.deepEqual(checkFiles(root, ["docs/page.md"]), [
    "docs/page.md:2: missing file: absent.md",
    "docs/page.md:3: missing anchor: target.md#absent",
  ]);
});

test("resolves relative, root, encoded, image, and reference links", (t) => {
  const root = fixture(t, {
    "docs/nested/page.md": [
      "# Page",
      "[self](#page)",
      "[relative](../target.md#rich-text)",
      "[root](/docs/target.md#rich-text-1)",
      "[encoded](../space%20name.md#ti%E1%BA%BFng-vi%E1%BB%87t)",
      "[brackets](../target\(old\).md)",
      '[angle](<../space name.md> "Title")',
      "![image](../image.svg)",
      "[full][target] [collapsed][] [shortcut]",
      "",
      "[target]: ../target.md#code--emphasis",
      "[collapsed]: ../target.md#setext",
      "[shortcut]: ../target.md#explicit",
      "",
      "[directory](../folder/#folder)",
      "[code](../../sample.js#L1-L2)",
      "[query](../target.md?raw=1#rich-text)",
    ].join("\n"),
    "docs/target.md": "# Rich text\n# Rich text\n# `Code` & *emphasis*\nSetext\n------\n<a id=\"explicit\"></a>\n",
    "docs/space name.md": "# Tiếng Việt\n",
    "docs/target(old).md": "# Old\n",
    "docs/folder/README.md": "# Folder\n",
    "docs/image.svg": "<svg/>\n",
    "sample.js": "// fixture\n",
  });
  assert.deepEqual(checkFiles(root, ["docs/nested/page.md"]), []);
});

test("ignores external links, comments, and fenced, indented, and inline code", (t) => {
  const root = fixture(t, {
    "page.md": [
      "[web](https://example.invalid/missing#anchor)",
      "[mail](mailto:fixture@example.invalid) [host](//example.invalid)",
      "<!-- [comment](absent.md) -->",
      "`[inline](absent.md)`",
      "", "    [indented](absent.md)", "",
      "```md", "[fenced](absent.md)", "# Fake", "```",
      "~~~", "# Also fake", "~~~",
    ].join("\n"),
    "links.md": "[fake](page.md#fake)\n[also](page.md#also-fake)\n",
  });
  assert.deepEqual(checkFiles(root, ["page.md"]), []);
  assert.equal(checkFiles(root, ["links.md"]).length, 2);
});

test("detects missing reference and image destinations", (t) => {
  const root = fixture(t, {
    "page.md": "[ref][missing]\n![image](missing.svg)\n\n[missing]: absent.md\n",
  });
  assert.equal(checkFiles(root, ["page.md"]).length, 2);
});

test("checks HTML links without treating data attributes or titles as links", (t) => {
  const root = fixture(t, {
    "page.md": [
      '<a href="target.md#anchor">valid</a>',
      '<span data-href="absent.md" title="href=absent.md">text</span>',
      '<!-- <a href="absent.md">comment</a> -->',
      '<img src="missing.svg" alt="fixture">',
    ].join("\n"),
    "target.md": '<a name="anchor"></a>\n',
  });
  assert.deepEqual(checkFiles(root, ["page.md"]), [
    "page.md:4: missing file: missing.svg",
  ]);
});

test("accepts local srcset assets with optional width or density descriptors", (t) => {
  for (const srcset of [
    "assets/banner.svg",
    "assets/banner.svg 640w, assets/large.svg 1280w",
    "assets/banner.svg 1x,assets/large.svg 2x",
    "assets/banner.svg, assets/large.svg",
    "\n assets/banner.svg\t1x,\n assets/large.svg 2x\n",
    "https://example.invalid/banner.svg 1x, assets/large.svg 2x",
    "data:image/png;base64,AAAA 1x, assets/large.svg 2x",
    "assets/banner,wide.svg 1x, assets/large.svg 2x",
    "assets/banner.svg?size=1&amp;format=svg 1x, assets/large.svg 2x",
  ]) {
    const root = fixture(t, {
      "page.md": `<source srcset="${srcset}">\n`,
      "assets/banner.svg": "<svg/>\n",
      "assets/large.svg": "<svg/>\n",
      "assets/banner,wide.svg": "<svg/>\n",
    });
    assert.deepEqual(checkFiles(root, ["page.md"]), [], srcset);
  }
});

test("reports each missing srcset asset without its descriptor", (t) => {
  for (const srcset of [
    "missing.svg",
    "missing.svg 640w, present.svg 1280w",
    "present.svg 1x,missing.svg 2x",
    "present.svg, missing.svg",
    "https://example.invalid/banner.svg 1x, missing.svg 2x",
    "data:image/png;base64,AAAA, missing.svg 2x",
  ]) {
    const root = fixture(t, {
      "page.md": `# Page\n<img srcset='${srcset}' data-srcset="ignored.svg">\n`,
      "present.svg": "<svg/>\n",
    });
    assert.deepEqual(checkFiles(root, ["page.md"]), [
      "page.md:2: missing file: missing.svg",
    ], srcset);
  }
  const root = fixture(t, {
    "page.md": '<img SRCSET="first.svg 1x,second.svg 2x">\n',
  });
  assert.deepEqual(checkFiles(root, ["page.md"]), [
    "page.md:1: missing file: first.svg",
    "page.md:1: missing file: second.svg",
  ]);
});

test("keeps duplicate heading suffixes unique", (t) => {
  const root = fixture(t, {
    "page.md": "# Same\n# Same\n# Same-1\n[valid](#same-1-1)\n[bad](#same-2)\n",
  });
  assert.deepEqual(checkFiles(root, ["page.md"]), [
    "page.md:5: missing anchor: #same-2",
  ]);
});

test("rejects malformed encoding and paths outside the repository", (t) => {
  const root = fixture(t, {
    "page.md": "[escape](../outside.md)\n[bad](bad%FF.md)\n[symlink](escape.md)\n",
  });
  symlinkSync(tmpdir(), join(root, "escape.md"));
  const errors = checkFiles(root, ["page.md"]);
  assert.equal(errors.length, 3);
  assert.match(errors[0], /outside repository/);
  assert.match(errors[1], /invalid URL encoding/);
  assert.match(errors[2], /outside repository/);
});

test("checks changed Markdown and affected indexes without crawling history", (t) => {
  const files = {
    "docs/README.md": "[design](design/README.md) [research](research/README.md)\n",
    "docs/design/README.md": "[page](page.md)\n",
    "docs/design/page.md": "# Page\n",
    "docs/runbooks/README.md": "# Runbooks\n",
    "docs/research/README.md": "[history](old/README.md)\n",
    "docs/research/old/README.md": "[historical](removed.md)\n",
    "docs/other/README.md": "[target](../design/page.md#page)\n",
    "docs/nested/README.md": "[removed](gone.md)\n",
    "docs/new page.md": "# New\n",
  };
  const root = fixture(t, files);
  const selected = selectFiles(root, ["docs/design/page.md", "docs/new page.md", "docs/nested/gone.md"], Object.keys(files));
  assert(selected.includes("docs/other/README.md"));
  assert(selected.includes("docs/new page.md"));
  assert(selected.includes("docs/nested/README.md"));
  assert(!selected.includes("docs/nested/gone.md"));
  assert(!selected.includes("docs/research/old/README.md"));
  assert.deepEqual(checkFiles(root, selected), [
    "docs/nested/README.md:1: missing file: gone.md",
  ]);
  const historicalChange = selectFiles(root, ["docs/research/old/README.md"], Object.keys(files));
  assert(checkFiles(root, historicalChange).some((error) => error.includes("removed.md")));
});

test("Git scope includes deletions, working changes, and untracked Markdown", () => {
  const calls = [];
  const git = (_root, args) => {
    calls.push(args);
    if (args[0] === "rev-parse") return "a".repeat(40) + "\n";
    if (args[0] === "diff") return "docs/deleted.md\0docs/changed.md\0";
    return "docs/new page.md\0";
  };
  assert.deepEqual(changedFiles("/fixture", "base", git), [
    "docs/deleted.md", "docs/changed.md", "docs/new page.md",
  ]);
  assert.deepEqual(calls[0], ["rev-parse", "--verify", "--end-of-options", "base^{commit}"]);
  assert.deepEqual(calls[1], ["diff", "--name-only", "--no-renames", "-z", "a".repeat(40), "--"]);
  assert.throws(() => changedFiles("/fixture", "bad", () => { throw new Error("bad base"); }), /bad base/);
});

test("repository indexes resolve and ADR guidance names the topic record", () => {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
  const files = ["docs/README.md", "docs/design/README.md", "docs/design/decisions.md", "docs/runbooks/README.md", "docs/research/README.md"];
  assert.deepEqual(checkFiles(root, files), []);
  for (const file of ["docs/README.md", "docs/design/decisions.md"]) {
    const text = readFileSync(join(root, file), "utf8");
    assert.doesNotMatch(text, /append-only|supersede, never edit|needs a new ADR|approval-rule/);
    assert.match(text, /owner's approval/);
    assert.match(text, /History/);
  }
  const decisions = readFileSync(join(root, "docs/design/decisions.md"), "utf8");
  assert.match(decisions, /\[ADR 0026\]\([^\n]+\) is the production approval/);
  assert.doesNotMatch(decisions, /AWS becomes test only/);
});
