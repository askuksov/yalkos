import type { ReactNode } from 'react'

import s from './ui.module.css'

/** A key label, e.g. <Kbd>C</Kbd>. Labels show the US layout key. */
export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className={s.kbd}>{children}</kbd>
}
