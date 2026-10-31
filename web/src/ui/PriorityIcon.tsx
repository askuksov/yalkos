import { useTranslation } from 'react-i18next'

import { priorityKey } from './priority'

/** none — a dash; low/medium/high — 1/2/3 bars; urgent — a filled square with "!". */
export function PriorityIcon({ priority, size = 14, decorative }: { priority: number; size?: number; decorative?: boolean }) {
  const { t } = useTranslation()
  const label = t(`priority.${priorityKey(priority)}`)
  const bars = priority === 2 ? 3 : priority === 3 ? 2 : priority === 4 ? 1 : 0
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 14 14"
      data-priority={priority}
      style={{ flex: 'none' }}
      {...(decorative ? { 'aria-hidden': true } : { role: 'img', 'aria-label': label })}
    >
      {!decorative && <title>{label}</title>}
      {priority === 0 && <path d="M3 7 H11" stroke="var(--fg-muted)" strokeWidth="1.5" strokeLinecap="round" />}
      {priority === 1 && (
        <>
          <rect x="1.5" y="1.5" width="11" height="11" rx="2.5" fill="var(--danger)" />
          <path d="M7 4 V7.8 M7 9.8 V10" stroke="var(--bg)" strokeWidth="1.6" strokeLinecap="round" />
        </>
      )}
      {priority >= 2 &&
        [0, 1, 2].map((i) => (
          <rect key={i} x={2 + i * 3.6} y={9 - i * 3} width="2.4" height={3 + i * 3} rx="0.8" fill={i < bars ? 'var(--fg)' : 'var(--border)'} />
        ))}
    </svg>
  )
}
