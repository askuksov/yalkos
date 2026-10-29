const pad = (n: number) => String(n).padStart(2, '0')

/** Formats an ISO timestamp as "YYYY-MM-DD HH:mm" in local time. */
export function formatDateTime(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
