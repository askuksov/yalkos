const allowedSchemes = new Set(['http:', 'https:', 'mailto:', 'mention:'])

/**
 * Keeps relative URLs (attachments, issue links, anchors) and http(s),
 * mailto and mention links; anything else (javascript:, data:, …) becomes ''.
 */
export function urlTransform(url: string): string {
  const scheme = /^([a-z][a-z0-9+.-]*):/i.exec(url.trim())
  if (!scheme) return url
  return allowedSchemes.has(scheme[1].toLowerCase() + ':') ? url : ''
}
