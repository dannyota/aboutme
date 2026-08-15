import { toRaw } from 'vue';

export function cloneReactiveSafe<T>(value: T): T {
  return structuredClone(unwrapReactive(value, new WeakMap())) as T;
}

function unwrapReactive(
  value: unknown,
  seen: WeakMap<object, unknown>,
): unknown {
  if (value === null || typeof value !== 'object') return value;
  const raw = toRaw(value);
  const known = seen.get(raw);
  if (known !== undefined) return known;
  if (Array.isArray(raw)) {
    const result: unknown[] = [];
    seen.set(raw, result);
    result.push(...raw.map((item) => unwrapReactive(item, seen)));
    return result;
  }
  const prototype = Object.getPrototypeOf(raw);
  if (prototype !== Object.prototype && prototype !== null) return raw;
  const result: Record<string, unknown> = {};
  seen.set(raw, result);
  for (const [key, item] of Object.entries(raw)) {
    result[key] = unwrapReactive(item, seen);
  }
  return result;
}
