/** Project key format: a letter, then up to 6 letters or digits. */
export const keyPattern = /^[A-Z][A-Z0-9]{0,6}$/

/** Uppercases and keeps letters/digits only (as the user types a key). */
export function normalizeKey(s: string): string {
  return s.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 7)
}

/**
 * Key from a project name: initials of up to 3 words ("Yalkos Web App" → YWA),
 * one word → its first 3 letters; must start with a letter (else "P" first);
 * a taken key gets 2..9 appended.
 */
export function suggestKey(name: string, taken: Set<string>): string {
  const words = name
    .normalize('NFKD')
    .toUpperCase()
    .split(/[^A-Z0-9]+/)
    .filter(Boolean)
  let base = words.length > 1 ? words.slice(0, 3).map((w) => w[0]).join('') : (words[0] ?? '').slice(0, 3)
  if (!/^[A-Z]/.test(base)) base = `P${base}`
  base = base.slice(0, 6)
  if (!taken.has(base)) return base
  for (let n = 2; n <= 9; n++) if (!taken.has(`${base}${n}`)) return `${base}${n}`
  return base
}
