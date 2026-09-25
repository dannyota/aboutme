import { parse as parseTemplate } from '@vue/compiler-dom';
import { parse as parseSfc } from '@vue/compiler-sfc';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import ts from 'typescript';

// Shared parsing for the source guards that keep display text in the
// Vietnamese and English catalogs instead of in components and helpers.

export const appRoot = resolve(process.cwd(), 'app');

const displayKeys = new Set([
  'ariaLabel',
  'description',
  'help',
  'hint',
  'label',
  'message',
  'placeholder',
  'text',
  'title',
]);
const guardedAttributes = new Set([
  'aria-label',
  'cancel-label',
  'close-label',
  'confirm-label',
  'description',
  'label',
  'placeholder',
  'title',
]);

type TemplateProp = {
  readonly type: number;
  readonly name: string;
  readonly value?: { readonly content: string };
  readonly arg?: { readonly content: string };
  readonly exp?: { readonly content: string };
};
type TemplateNode = {
  readonly type: number;
  readonly content?: string;
  readonly props?: readonly TemplateProp[];
  readonly children?: readonly TemplateNode[];
  readonly branches?: readonly TemplateNode[];
};

export type ApprovedLiterals = Readonly<Record<string, readonly string[]>>;

export function source(path: string): string {
  return readFileSync(join(appRoot, path), 'utf8');
}

export function literalTexts(node: ts.Node): readonly string[] {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return node.text === '' ? [] : [node.text];
  }
  if (ts.isTemplateExpression(node)) {
    return [
      node.head.text,
      ...node.templateSpans.map((span) => span.literal.text),
    ].filter((text) => text.trim() !== '');
  }
  return [];
}

export function propertyName(
  name: ts.PropertyName | ts.BindingName,
): string | undefined {
  return ts.isIdentifier(name) || ts.isStringLiteral(name)
    ? name.text
    : undefined;
}

export function expressionLiterals(input: string): readonly string[] {
  const file = ts.createSourceFile(
    'template.ts',
    input,
    ts.ScriptTarget.Latest,
  );
  const statement = file.statements[0];
  if (statement === undefined || !ts.isExpressionStatement(statement)) {
    return [];
  }
  const literals: string[] = [];
  const walk = (node: ts.Expression): void => {
    literals.push(...literalTexts(node));
    if (ts.isParenthesizedExpression(node)) {
      walk(node.expression);
    } else if (ts.isConditionalExpression(node)) {
      walk(node.whenTrue);
      walk(node.whenFalse);
    } else if (
      ts.isBinaryExpression(node)
      && [
        ts.SyntaxKind.PlusToken,
        ts.SyntaxKind.QuestionQuestionToken,
        ts.SyntaxKind.BarBarToken,
      ].includes(node.operatorToken.kind)
    ) {
      walk(node.left);
      walk(node.right);
    }
  };
  walk(statement.expression);
  return literals;
}

function promptCall(node: ts.CallExpression): boolean {
  const expression = node.expression;
  if (ts.isIdentifier(expression)) {
    return ['alert', 'confirm', 'prompt'].includes(expression.text);
  }
  if (!ts.isPropertyAccessExpression(expression)) return false;
  const receiver = expression.expression;
  return ts.isIdentifier(receiver)
    && ['globalThis', 'window'].includes(receiver.text)
    && ['alert', 'confirm', 'prompt'].includes(expression.name.text);
}

/**
 * Returns the literal checks for one guard. A literal listed for a path in
 * `approvedLiterals` passes in text, bound attributes, and scripts; a static
 * guarded attribute never passes.
 */
export function literalGuard(approvedLiterals: ApprovedLiterals): {
  readonly templateViolations: (path: string, input: string) => string[];
  readonly scriptViolations: (path: string, input: string) => string[];
  readonly sourceViolations: (path: string, input?: string) => string[];
} {
  const allowed = (path: string, text: string): boolean =>
    approvedLiterals[path]?.includes(text) ?? false;

  const templateViolations = (path: string, input: string): string[] => {
    const parsed = parseTemplate(input, { comments: false });
    const violations: string[] = [];
    const report = (kind: string, text: string): void => {
      if (!allowed(path, text)) {
        violations.push(`${path}: ${kind} ${JSON.stringify(text)}`);
      }
    };
    const walk = (node: TemplateNode): void => {
      if (node.type === 2 && node.content?.trim() !== '') {
        report('static text', node.content.trim());
      }
      if (node.type === 1) {
        for (const prop of node.props ?? []) {
          if (
            prop.type === 6
            && guardedAttributes.has(prop.name)
            && prop.value?.content.trim() !== ''
          ) {
            violations.push(
              `${path}: static ${prop.name} ${
                JSON.stringify(prop.value.content)}`,
            );
          }
          if (
            prop.type === 7
            && prop.name === 'bind'
            && prop.arg !== undefined
            && guardedAttributes.has(prop.arg.content)
            && prop.exp !== undefined
          ) {
            for (const text of expressionLiterals(prop.exp.content)) {
              report(`bound ${prop.arg.content}`, text);
            }
          }
        }
        for (const child of node.children ?? []) walk(child);
      } else if (node.type === 0 || node.type === 10 || node.type === 11) {
        for (const child of node.children ?? []) walk(child);
      } else if (node.type === 9) {
        for (const branch of node.branches ?? []) walk(branch);
      } else if (node.type === 5) {
        const content = (node as unknown as { content?: { content?: string } })
          .content?.content;
        if (content !== undefined) {
          for (const text of expressionLiterals(content)) {
            report('interpolation', text);
          }
        }
      }
    };
    walk(parsed as unknown as TemplateNode);
    return violations;
  };

  const scriptViolations = (path: string, input: string): string[] => {
    const file = ts.createSourceFile(path, input, ts.ScriptTarget.Latest, true);
    const violations: string[] = [];
    const report = (node: ts.Node, text: string): void => {
      if (!allowed(path, text)) {
        const line
          = file.getLineAndCharacterOfPosition(node.getStart()).line + 1;
        violations.push(`${path}:${line}: ${JSON.stringify(text)}`);
      }
    };
    const displayFunction = (node: ts.Node): boolean => {
      if (!ts.isFunctionDeclaration(node) || node.name === undefined) {
        return false;
      }
      return displayKeys.has(node.name.text)
        || /(?:label|message|text)$/iu.test(node.name.text);
    };
    const displayContext = (node: ts.Node): boolean => {
      let current: ts.Node = node;
      while (current.parent !== undefined && !ts.isSourceFile(current.parent)) {
        const parent = current.parent;
        if (
          ts.isPropertyAssignment(parent) || ts.isVariableDeclaration(parent)
        ) {
          const name = propertyName(parent.name);
          return current === parent.initializer
            && name !== undefined
            && displayKeys.has(name);
        }
        if (ts.isReturnStatement(parent)) {
          let owner: ts.Node | undefined = parent.parent;
          while (owner !== undefined && !ts.isFunctionDeclaration(owner)) {
            owner = owner.parent;
          }
          return owner !== undefined && displayFunction(owner);
        }
        if (
          ts.isConditionalExpression(parent) && current === parent.condition
        ) {
          return false;
        }
        if (
          ts.isCallExpression(parent) || ts.isArrayLiteralExpression(parent)
        ) {
          return false;
        }
        current = parent;
      }
      return false;
    };
    const walk = (node: ts.Node): void => {
      if (ts.isCallExpression(node) && promptCall(node)) {
        const [message] = node.arguments;
        if (message !== undefined) {
          for (const text of literalTexts(message)) report(message, text);
        }
      }
      for (const text of literalTexts(node)) {
        if (displayContext(node)) report(node, text);
      }
      ts.forEachChild(node, walk);
    };
    walk(file);
    return violations;
  };

  const sourceViolations = (path: string, input = source(path)): string[] => {
    if (!path.endsWith('.vue')) return scriptViolations(path, input);
    const descriptor = parseSfc(input, { filename: path }).descriptor;
    return [
      ...(descriptor.template === null
        ? []
        : templateViolations(path, descriptor.template.content)),
      ...(descriptor.script === null
        ? []
        : scriptViolations(path, descriptor.script.content)),
      ...(descriptor.scriptSetup === null
        ? []
        : scriptViolations(path, descriptor.scriptSetup.content)),
    ];
  };

  return { templateViolations, scriptViolations, sourceViolations };
}

function plainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

/**
 * Lists every place the Vietnamese and English catalogs differ in shape or
 * hold an empty string. A function passes when both sides have the same
 * arity; with `functionFixtures`, it must also return nonempty text for the
 * fixture arguments listed under its path.
 */
export function parityViolations(
  vietnamese: unknown,
  english: unknown,
  path = 'copy',
  functionFixtures?: Readonly<Record<string, readonly unknown[]>>,
): string[] {
  if (typeof vietnamese === 'function' || typeof english === 'function') {
    if (typeof vietnamese !== typeof english) {
      return [`${path}: function shape`];
    }
    if (functionFixtures === undefined) {
      return (vietnamese as { readonly length: number }).length
        === (english as { readonly length: number }).length
        ? []
        : [`${path}: function shape`];
    }
    const fixture = functionFixtures[path];
    if (fixture === undefined) return [`${path}: missing function fixture`];
    const vi = (vietnamese as (...args: unknown[]) => unknown)(...fixture);
    const en = (english as (...args: unknown[]) => unknown)(...fixture);
    return typeof vi === 'string' && vi.trim() !== ''
      && typeof en === 'string' && en.trim() !== ''
      ? []
      : [`${path}: empty function value`];
  }
  if (Array.isArray(vietnamese) || Array.isArray(english)) {
    if (!Array.isArray(vietnamese) || !Array.isArray(english)) {
      return [`${path}: array shape`];
    }
    if (vietnamese.length !== english.length) {
      return [`${path}: array length`];
    }
    return vietnamese.flatMap((value, index) =>
      parityViolations(
        value,
        english[index],
        `${path}[${index}]`,
        functionFixtures,
      ),
    );
  }
  if (!plainObject(vietnamese) || !plainObject(english)) {
    if (typeof vietnamese !== typeof english) return [`${path}: value shape`];
    return typeof vietnamese === 'string' && vietnamese.trim() === ''
      ? [`${path}: empty Vietnamese value`]
      : typeof english === 'string' && english.trim() === ''
        ? [`${path}: empty English value`]
        : [];
  }
  const viKeys = Object.keys(vietnamese).sort();
  const enKeys = Object.keys(english).sort();
  if (viKeys.join('\0') !== enKeys.join('\0')) {
    return [`${path}: keys ${viKeys.join(',')} !== ${enKeys.join(',')}`];
  }
  return viKeys.flatMap((key) =>
    parityViolations(
      vietnamese[key],
      english[key],
      `${path}.${key}`,
      functionFixtures,
    ),
  );
}

export function filesBelow(directory: string): string[] {
  if (!existsSync(directory)) return [];
  if (!statSync(directory).isDirectory()) return [directory];
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const file = join(directory, entry.name);
    return entry.isDirectory() ? filesBelow(file) : [file];
  });
}

export function scriptContents(path: string, input: string): string {
  if (!path.endsWith('.vue')) return input;
  const descriptor = parseSfc(input, { filename: path }).descriptor;
  return [descriptor.script?.content, descriptor.scriptSetup?.content]
    .filter((content): content is string => content !== undefined)
    .join('\n');
}

/** Resolves an import specifier to a path under `app/` without extension. */
export function resolvedImport(file: string, specifier: string): string {
  const target = specifier.startsWith('@/') || specifier.startsWith('~/')
    ? join(appRoot, specifier.slice(2))
    : resolve(join(appRoot, file, '..'), specifier);
  return relative(appRoot, target).replace(/\\/gu, '/').replace(/\.ts$/u, '');
}
