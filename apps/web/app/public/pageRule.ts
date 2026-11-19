const PAGE_RULE_ATTRIBUTE = 'data-public-page-rule';

export function syncPageRule(target: Document, rule: string): void {
  const current = target.head.querySelector<HTMLStyleElement>(
    `style[${PAGE_RULE_ATTRIBUTE}]`,
  );
  if (current !== null) {
    if (current.textContent !== rule) current.textContent = rule;
    return;
  }
  const style = target.createElement('style');
  style.setAttribute(PAGE_RULE_ATTRIBUTE, '');
  style.textContent = rule;
  target.head.append(style);
}

export function removePageRule(target: Document): void {
  target.head.querySelector(`style[${PAGE_RULE_ATTRIBUTE}]`)?.remove();
}
