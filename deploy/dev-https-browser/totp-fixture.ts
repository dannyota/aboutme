/**
 * Deterministic RFC 6238 TOTP helper for the browser proof. Computes codes
 * for a secret this run reads from the page, using an injected clock so no
 * caller reaches the system clock directly (docs/design/totp-second-factor-contract.md
 * "TOTP profile and code verification"). Never logs a secret or a code.
 */
import { createHmac } from 'node:crypto';

export const TOTP_PERIOD_SECONDS = 30;
const TOTP_DIGITS = 6;
const BASE32_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

/** An injectable source of the current time, in whole Unix seconds. */
export interface TotpClock {
  nowSeconds(): number;
}

/** A clock fixed at construction, advanced only by explicit steps. */
export function fixedClock(startSeconds: number): TotpClock {
  let seconds = Math.trunc(startSeconds);
  return {
    nowSeconds: () => seconds,
  };
}

/** A clock backed by the real system time, used only where freshness matters. */
export function systemClock(): TotpClock {
  return { nowSeconds: () => Math.floor(Date.now() / 1000) };
}

/** Decodes the canonical uppercase unpadded RFC 4648 Base32 secret. */
function base32Decode(secret: string): Buffer {
  const cleaned = secret.replace(/\s+/gu, '').toUpperCase();
  if (!/^[A-Z2-7]+$/u.test(cleaned)) {
    throw new Error('TOTP secret is not canonical Base32');
  }
  let bits = '';
  for (const char of cleaned) {
    const value = BASE32_ALPHABET.indexOf(char);
    if (value === -1) throw new Error('TOTP secret has an invalid character');
    bits += value.toString(2).padStart(5, '0');
  }
  const bytes: number[] = [];
  for (let index = 0; index + 8 <= bits.length; index += 8) {
    bytes.push(Number.parseInt(bits.slice(index, index + 8), 2));
  }
  return Buffer.from(bytes);
}

/** The time step for a moment, per RFC 6238 with T0=0. */
export function stepAt(unixSeconds: number, period = TOTP_PERIOD_SECONDS): number {
  return Math.floor(unixSeconds / period);
}

/** HOTP (RFC 4226) over one 8-byte big-endian counter, HMAC-SHA-1, six digits. */
function hotp(secretBytes: Buffer, counter: number): string {
  const counterBytes = Buffer.alloc(8);
  counterBytes.writeUInt32BE(0, 0);
  counterBytes.writeUInt32BE(counter, 4);
  const digest = createHmac('sha1', secretBytes).update(counterBytes).digest();
  const offset = (digest[digest.length - 1] ?? 0) & 0x0f;
  const truncated = (
    ((digest[offset] ?? 0) & 0x7f) << 24
    | ((digest[offset + 1] ?? 0) & 0xff) << 16
    | ((digest[offset + 2] ?? 0) & 0xff) << 8
    | ((digest[offset + 3] ?? 0) & 0xff)
  ) >>> 0;
  return String(truncated % 10 ** TOTP_DIGITS).padStart(TOTP_DIGITS, '0');
}

/** The six-digit code for one time step. */
export function codeForStep(secretBase32: string, step: number): string {
  return hotp(base32Decode(secretBase32), step);
}

/** The six-digit code for the step the clock names, without exposing it. */
export function codeNow(
  secretBase32: string,
  clock: TotpClock,
  period = TOTP_PERIOD_SECONDS,
): string {
  return codeForStep(secretBase32, stepAt(clock.nowSeconds(), period));
}

/** The code one step before the clock's current step. */
export function codePreviousStep(
  secretBase32: string,
  clock: TotpClock,
  period = TOTP_PERIOD_SECONDS,
): string {
  return codeForStep(secretBase32, stepAt(clock.nowSeconds(), period) - 1);
}

/** The code one step after the clock's current step. */
export function codeNextStep(
  secretBase32: string,
  clock: TotpClock,
  period = TOTP_PERIOD_SECONDS,
): string {
  return codeForStep(secretBase32, stepAt(clock.nowSeconds(), period) + 1);
}

const ASCII_TO_FULLWIDTH_DIGIT_OFFSET = 0xFF10 - 0x30;

/**
 * Replaces every ASCII digit with its Unicode fullwidth counterpart, so the
 * proof can submit a well-shaped six-character value that is not six ASCII
 * digits (docs/design/totp-second-factor-contract.md "TOTP profile and code
 * verification": non-ASCII digits fail as `400 request_invalid`).
 */
export function toFullwidthDigits(code: string): string {
  return [...code].map((char) => {
    const point = char.codePointAt(0) ?? 0;
    return point >= 0x30 && point <= 0x39
      ? String.fromCodePoint(point + ASCII_TO_FULLWIDTH_DIGIT_OFFSET)
      : char;
  }).join('');
}

/** A well-shaped six-digit code that is guaranteed not to equal `exclude`. */
export function mismatchedCode(exclude: string): string {
  const guess = codeForStep('AAAAAAAAAAAAAAAA', 0);
  if (guess !== exclude) return guess;
  const fallback = codeForStep('AAAAAAAAAAAAAAAA', 1);
  return fallback === exclude ? codeForStep('AAAAAAAAAAAAAAAA', 2) : fallback;
}

// --- CI section timing (diagnostics only) -----------------------------------
//
// Wall-clock timing for the browser proof's own CI diagnostics: which part of
// the run is slow. Holds no code, secret, email, or URL, and is never read by
// verify-evidence.mjs; it is a sidecar file outside the verified schema.

/** The closed set of section names the enabled TOTP proof times. */
export type TimingSection =
  | 'none' | 'primary' | 'skew' | 'replay' | 'concurrent' | 'replace'
  | 'epoch' | 'locale' | 'recovery' | 'attempts' | 'disabled' | 'cleanup';

const TIMING_SECTIONS: readonly TimingSection[] = [
  'none', 'primary', 'skew', 'replay', 'concurrent', 'replace', 'epoch',
  'locale', 'recovery', 'attempts', 'disabled', 'cleanup',
];

/** Accumulates wall-clock milliseconds spent in each closed-list section. */
export interface SectionTimer {
  /** Closes the current section and opens `next`. */
  enter(next: TimingSection): void;
  /** Closes the current section and returns every section's total. */
  finish(): Record<TimingSection, number>;
}

/** A section timer starting in `'none'`, backed by the real clock. */
export function newSectionTimer(nowMs: () => number = Date.now): SectionTimer {
  let current: TimingSection = 'none';
  let enteredAt = nowMs();
  const elapsedMs = Object.fromEntries(
    TIMING_SECTIONS.map((section) => [section, 0]),
  ) as Record<TimingSection, number>;
  const close = (now: number): void => {
    elapsedMs[current] += now - enteredAt;
    enteredAt = now;
  };
  return {
    enter(next) {
      close(nowMs());
      current = next;
    },
    finish() {
      close(nowMs());
      return elapsedMs;
    },
  };
}
