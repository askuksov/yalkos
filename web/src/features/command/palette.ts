import { createContext, useContext, useEffect } from 'react'

import type { IssueTarget } from './recent'

export type PalettePage = 'root' | 'search' | 'state' | 'assignee' | 'priority' | 'parent' | 'move' | 'due' | 'delete'

export type PaletteApi = {
  open: (page?: PalettePage, target?: IssueTarget[]) => void
  /** Issues the palette's "This issue" group acts on when opened without a target. */
  setPageTarget: (target: IssueTarget[] | null) => void
  showHelp: () => void
}

export const PaletteContext = createContext<PaletteApi | null>(null)

export function usePalette(): PaletteApi {
  const api = useContext(PaletteContext)
  if (!api) throw new Error('usePalette outside PaletteProvider')
  return api
}

/** Sets the palette's default targets while the calling page shows them. */
export function usePageTarget(target: IssueTarget[] | null) {
  const { setPageTarget } = usePalette()
  const key = JSON.stringify(target)
  useEffect(() => {
    setPageTarget(key === 'null' ? null : (JSON.parse(key) as IssueTarget[]))
    return () => setPageTarget(null)
  }, [key, setPageTarget])
}
