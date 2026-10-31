const allowedSchemes = new Set(['http:', 'https:', 'mailto:', 'mention:'])

/**
 * Keeps relative URLs (attachments, issue links, anchors) and http(s),
 * mailto and mention links; anything else (javascript:, data:, …) becomes ''.
 * The scheme is read the way a browser reads it: tabs, newlines and leading
 * control characters or spaces do not hide it.
 */
export function urlTransform(url: string): string {
  // eslint-disable-next-line no-control-regex
  const cleaned = url.replace(/[\t\n\r]/g, '').replace(/^[\u0000- ]+/, '')
  const colon = cleaned.indexOf(':')
  if (colon === -1) return url
  // A colon after a path, query or fragment start belongs to a relative URL.
  for (const delimiter of ['/', '?', '#']) {
    const at = cleaned.indexOf(delimiter)
    if (at !== -1 && at < colon) return url
  }
  return allowedSchemes.has(cleaned.slice(0, colon).toLowerCase() + ':') ? url : ''
}
