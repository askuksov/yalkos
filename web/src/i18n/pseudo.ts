// Dev-only pseudo-locale: every catalog string becomes ⟦Ŝàmplé⟧-style text,
// about 30 % longer, so hard-coded strings and cramped layouts stand out.
// Interpolation placeholders ({{name}}) and nesting ($t(...)) stay intact.

const accents: Record<string, string> = {
  a: 'à', b: 'ƀ', c: 'ç', d: 'ď', e: 'é', f: 'ƒ', g: 'ĝ', h: 'ĥ', i: 'î', j: 'ĵ', k: 'ķ', l: 'ľ', m: 'ɱ',
  n: 'ñ', o: 'ö', p: 'þ', q: 'ǫ', r: 'ř', s: 'š', t: 'ţ', u: 'û', v: 'ṽ', w: 'ŵ', x: 'ẋ', y: 'ý', z: 'ž',
  A: 'À', B: 'Ɓ', C: 'Ç', D: 'Ď', E: 'É', F: 'Ƒ', G: 'Ĝ', H: 'Ĥ', I: 'Î', J: 'Ĵ', K: 'Ķ', L: 'Ľ', M: 'Ṁ',
  N: 'Ñ', O: 'Ö', P: 'Þ', Q: 'Ǫ', R: 'Ř', S: 'Š', T: 'Ţ', U: 'Û', V: 'Ṽ', W: 'Ŵ', X: 'Ẋ', Y: 'Ý', Z: 'Ž',
}

const placeholder = /(\{\{[^}]*\}\}|\$t\([^)]*\))/

export function pseudoize(s: string): string {
  const out = s
    .split(placeholder)
    .map((part, i) => (i % 2 === 1 ? part : part.replace(/[A-Za-z]/g, (c) => accents[c] ?? c)))
    .join('')
  const pad = '~'.repeat(Math.ceil(s.length * 0.3))
  return `⟦${out}${pad}⟧`
}

type Catalog = { [key: string]: string | Catalog }

/** Maps every string of a catalog through pseudoize. */
export function pseudoCatalog(c: Catalog): Catalog {
  const out: Catalog = {}
  for (const [k, v] of Object.entries(c)) out[k] = typeof v === 'string' ? pseudoize(v) : pseudoCatalog(v)
  return out
}
