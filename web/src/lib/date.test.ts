import { expect, it } from 'vitest'

import { formatDateTime } from './date'

it('formats as YYYY-MM-DD HH:mm in local time', () => {
  const d = new Date(2026, 9, 30, 9, 5) // local 2026-10-30 09:05
  expect(formatDateTime(d.toISOString())).toBe('2026-10-30 09:05')
})
