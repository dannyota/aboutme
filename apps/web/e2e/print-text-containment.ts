import type { Locator } from '@playwright/test';

/** Check Range rectangles and visible clips without changing print layout. */
export async function measurePrintTextContainment(container: Locator) {
  return container.evaluate((root) => {
    type Box = { left: number; right: number; top: number; bottom: number };
    type Line = {
      node: string;
      text: string;
      range: Box;
      baseline: number;
      fontSize: number;
    };
    const bounds = (rect: Box): Box => ({
      left: rect.left, right: rect.right,
      top: rect.top, bottom: rect.bottom,
    });
    const path = (element: Element): string => {
      const parts: string[] = [];
      for (let node: Element | null = element; node;
        node = node.parentElement) {
        const index = Array.from(node.parentElement?.children ?? [])
          .filter((sibling) => sibling.tagName === node!.tagName)
          .indexOf(node) + 1;
        const classes = Array.from(node.classList, (name) =>
          `.${CSS.escape(name)}`).join('');
        parts.unshift(`${node.localName}${classes}:nth-of-type(${index})`);
        if (node === root) {
          break;
        }
      }
      return parts.join(' > ');
    };
    const clips = /^(hidden|clip)$/u;
    const page = root.closest('.resume-document') ?? root;
    const lines: Line[] = [];
    const owners: Element[] = [];
    const failures: {
      node: string;
      container: string;
      text: string;
      kind: string;
      axis: string;
      delta: number;
      allowance: number;
      range: Box | null;
      box: Box;
    }[] = [];
    let visible = true;
    let contained = true;
    let hasText = false;
    const fail = (line: Line, element: Element, kind: string,
      axis: string, delta: number, box: Box, allowance = 1) => {
      contained = false;
      if (failures.length < 8) {
        failures.push({
          node: line.node, container: path(element),
          text: line.text.slice(0, 100), kind, axis, delta, allowance,
          range: line.range, box: bounds(box),
        });
      }
    };
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      const parent = node.parentElement;
      const text = node.textContent;
      if (!parent || !text?.trim()) {
        continue;
      }
      hasText = true;
      const nodePath = path(parent) + ' > #text['
        + (Array.from(parent.childNodes).filter((child) =>
          child.nodeType === Node.TEXT_NODE).indexOf(node as Text) + 1)
        + ']';
      const css = getComputedStyle(parent);
      let owner = parent;
      while (getComputedStyle(owner).display === 'inline'
        && owner.parentElement && owner !== root) {
        owner = owner.parentElement;
      }
      const range = document.createRange();
      range.selectNodeContents(node);
      const rects = Array.from(range.getClientRects()).filter((rect) =>
        rect.width > 0 && rect.height > 0);
      if (rects.length === 0) {
        visible = false;
        if (failures.length < 8) {
          failures.push({
            node: nodePath, container: path(parent), text: text.slice(0, 100),
            kind: 'unpainted', axis: 'visibility', delta: 0, allowance: 0,
            range: null, box: bounds(parent.getBoundingClientRect()),
          });
        }
      }
      for (const rect of rects) {
        const fontSize = parseFloat(css.fontSize) || rect.height;
        // For equal-size runs, Range bottoms track baseline spacing.
        const line = { node: nodePath, text, range: bounds(rect),
          baseline: rect.bottom, fontSize };
        for (const [index, previous] of lines.entries()) {
          // Adjacent inline runs can have different font metrics on one line.
          const overlap = Math.min(rect.right, previous.range.right)
            - Math.max(rect.left, previous.range.left);
          const distance = Math.abs(line.baseline - previous.baseline);
          const minimum = Math.min(fontSize, previous.fontSize) / 2;
          const distinct = (previous.node === nodePath && distance > 0.01)
            || overlap > 1;
          if (owners[index] === owner && distinct && distance < minimum) {
            fail(line, owner, 'overlap', 'y', minimum - distance,
              previous.range, 0);
          }
        }
        lines.push(line);
        owners.push(owner);
        const viewport = { left: 0,
          right: document.documentElement.clientWidth,
          top: 0, bottom: innerHeight };
        const horizontal = Math.max(viewport.left - rect.left,
          rect.right - viewport.right);
        if (horizontal > 1) {
          fail(line, document.documentElement, 'range', 'x', horizontal,
            viewport);
        }
        for (let ancestor: Element | null = parent; ancestor;
          ancestor = ancestor.parentElement) {
          const style = getComputedStyle(ancestor);
          const box = ancestor.getBoundingClientRect();
          if (style.display === 'none' || style.visibility !== 'visible'
            || Number(style.opacity) === 0) {
            visible = false;
            fail(line, ancestor, 'unpainted', 'visibility', 0, box, 0);
          }
          const scoped = root.contains(ancestor) || ancestor === page;
          if (ancestor.scrollWidth > ancestor.clientWidth + 1) {
            fail(line, ancestor, 'scroll', 'x',
              ancestor.scrollWidth - ancestor.clientWidth, box);
          }
          const x = Math.max(box.left - rect.left, rect.right - box.right);
          if ((scoped || clips.test(style.overflowX)) && x > 1) {
            fail(line, ancestor, 'range', 'x', x, box);
          }
          const allowance = (rect.height - fontSize) / 2 + 1;
          const y = Math.max(box.top - rect.top, rect.bottom - box.bottom);
          if (clips.test(style.overflowY) && y > allowance) {
            fail(line, ancestor, 'clip', 'y', y, box, allowance);
          }
        }
      }
    }
    if (!hasText) {
      failures.push({
        node: path(root), container: path(root), text: '',
        kind: 'empty', axis: 'visibility', delta: 0, allowance: 0,
        range: null, box: bounds(root.getBoundingClientRect()),
      });
    }
    return { visible: visible && hasText, contained, failures, lines };
  });
}
