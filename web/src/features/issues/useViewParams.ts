import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router'

import { parseParams, toSearch, type ViewParams } from './filters'

/** View params from the URL; setting them replaces the history entry. */
export function useViewParams(view: 'list' | 'board' = 'list'): [ViewParams, (next: ViewParams) => void] {
  const [sp, setSp] = useSearchParams()
  const params = useMemo(() => parseParams(sp, view), [sp, view])
  const set = useCallback((next: ViewParams) => setSp(toSearch(next), { replace: true }), [setSp])
  return [params, set]
}
