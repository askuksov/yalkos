/** Stable hue for an id, so a user keeps one colour everywhere. */
export function hueOf(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0
  return h % 360
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  const chars = parts.length > 1 ? [parts[0][0], parts[parts.length - 1][0]] : [...(parts[0] ?? '?')].slice(0, 2)
  return chars.join('').toUpperCase()
}
