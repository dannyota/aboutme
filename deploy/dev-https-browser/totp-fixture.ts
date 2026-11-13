import { createHmac } from 'node:crypto';

export const TOTP_PERIOD_SECONDS = 30;
const TOTP_DIGITS = 6;
const BASE32_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

export interface TotpClock {
  nowSeconds(): number;
}

export function fixedClock(startSeconds: number): TotpClock {
  let seconds = Math.trunc(startSeconds);
  return {
    nowSeconds: () => seconds,
  };
}

export function systemClock(): TotpClock {
  return { nowSeconds: () => Math.floor(Date.now() / 1000) };
}

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

export function stepAt(unixSeconds: number, period = TOTP_PERIOD_SECONDS): number {
  return Math.floor(unixSeconds / period);
}

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

export function codeForStep(secretBase32: string, step: number): string {
  return hotp(base32Decode(secretBase32), step);
}

export function codeNow(
  secretBase32: string,
  clock: TotpClock,
  period = TOTP_PERIOD_SECONDS,
): string {
  return codeForStep(secretBase32, stepAt(clock.nowSeconds(), period));
}

export function codePreviousStep(
  secretBase32: string,
  clock: TotpClock,
  period = TOTP_PERIOD_SECONDS,
): string {
  return codeForStep(secretBase32, stepAt(clock.nowSeconds(), period) - 1);
}

export function codeNextStep(
  secretBase32: string,
  clock: TotpClock,
  period = TOTP_PERIOD_SECONDS,
): string {
  return codeForStep(secretBase32, stepAt(clock.nowSeconds(), period) + 1);
}

const ASCII_TO_FULLWIDTH_DIGIT_OFFSET = 0xFF10 - 0x30;

export function toFullwidthDigits(code: string): string {
  return [...code].map((char) => {
    const point = char.codePointAt(0) ?? 0;
    return point >= 0x30 && point <= 0x39
      ? String.fromCodePoint(point + ASCII_TO_FULLWIDTH_DIGIT_OFFSET)
      : char;
  }).join('');
}

export function mismatchedCode(exclude: string): string {
  const guess = codeForStep('AAAAAAAAAAAAAAAA', 0);
  if (guess !== exclude) return guess;
  const fallback = codeForStep('AAAAAAAAAAAAAAAA', 1);
  return fallback === exclude ? codeForStep('AAAAAAAAAAAAAAAA', 2) : fallback;
}

export type TimingSection =
  | 'none' | 'primary' | 'skew' | 'replay' | 'concurrent' | 'replace'
  | 'epoch' | 'locale' | 'recovery' | 'attempts' | 'disabled' | 'cleanup';

const TIMING_SECTIONS: readonly TimingSection[] = [
  'none', 'primary', 'skew', 'replay', 'concurrent', 'replace', 'epoch',
  'locale', 'recovery', 'attempts', 'disabled', 'cleanup',
];

export interface SectionTimer {
  enter(next: TimingSection): void;
  finish(): Record<TimingSection, number>;
}

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
