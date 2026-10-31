export type StateTypeName = 'BACKLOG' | 'UNSTARTED' | 'STARTED' | 'COMPLETED' | 'CANCELED'

const shapes: Record<StateTypeName, string> = {
  BACKLOG: 'dashed',
  UNSTARTED: 'circle',
  STARTED: 'half',
  COMPLETED: 'check',
  CANCELED: 'cross',
}

/** State icon by type, coloured by the state colour. */
export function StateIcon({ type, color, size = 14, label }: { type: StateTypeName; color: string; size?: number; label?: string }) {
  const a11y = label ? { role: 'img', 'aria-label': label } : { 'aria-hidden': true }
  return (
    <svg width={size} height={size} viewBox="0 0 14 14" data-shape={shapes[type]} style={{ flex: 'none' }} {...a11y}>
      {type === 'BACKLOG' && <circle cx="7" cy="7" r="5.5" fill="none" stroke={color} strokeWidth="1.5" strokeDasharray="2.2 1.8" />}
      {type === 'UNSTARTED' && <circle cx="7" cy="7" r="5.5" fill="none" stroke={color} strokeWidth="1.5" />}
      {type === 'STARTED' && (
        <>
          <circle cx="7" cy="7" r="5.5" fill="none" stroke={color} strokeWidth="1.5" />
          <path d="M7 3.5 A3.5 3.5 0 0 1 7 10.5 Z" fill={color} />
        </>
      )}
      {type === 'COMPLETED' && (
        <>
          <circle cx="7" cy="7" r="6.25" fill={color} />
          <path d="M4.2 7.2 L6.1 9 L9.8 5.2" fill="none" stroke="var(--bg)" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        </>
      )}
      {type === 'CANCELED' && (
        <>
          <circle cx="7" cy="7" r="6.25" fill={color} />
          <path d="M4.8 4.8 L9.2 9.2 M9.2 4.8 L4.8 9.2" stroke="var(--bg)" strokeWidth="1.6" strokeLinecap="round" />
        </>
      )}
    </svg>
  )
}
