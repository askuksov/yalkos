/** Dotted IPv4 address inside 127.0.0.0/8. */
function isLoopbackV4(host: string): boolean {
  const parts = host.split('.')
  if (parts.length !== 4 || parts[0] !== '127') return false
  return parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255)
}

/** Loopback hosts: localhost, *.localhost, 127.0.0.0/8 and ::1. */
function isLoopbackHost(hostname: string): boolean {
  // A trailing dot is a fully qualified name; IPv6 hosts arrive in brackets.
  const host = hostname.toLowerCase().replace(/\.$/, '').replace(/^\[(.*)\]$/, '$1')
  return host === 'localhost' || host.endsWith('.localhost') || host === '::1' || isLoopbackV4(host)
}

/** True for plain HTTP to a host that is not loopback: secrets sent there cross a network unencrypted. */
export function isInsecureRemote(where: { protocol: string; hostname: string }): boolean {
  return where.protocol === 'http:' && !isLoopbackHost(where.hostname)
}
