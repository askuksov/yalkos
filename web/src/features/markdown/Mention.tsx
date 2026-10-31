import type { ReactNode } from 'react'

import { useMembers } from '../../api/hooks'
import s from './markdown.module.css'

/** A mention: the member's current name, else the stored link text. */
export function Mention({ userId, children }: { userId: string; children: ReactNode }) {
  const user = useMembers().data?.find((u) => u.id === userId)
  return (
    <span className={s.mention} data-mention={userId}>
      {user ? `@${user.name}` : children}
    </span>
  )
}
