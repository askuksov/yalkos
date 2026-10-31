import { createContext, useContext } from 'react'

export type SelectionApi = {
  selected: string | null
  setSelected: (id: string) => void
  checked: ReadonlySet<string>
  toggle: (id: string) => void
  checkable: boolean
}

export const SelectionContext = createContext<SelectionApi>({
  selected: null, setSelected: () => {}, checked: new Set(), toggle: () => {}, checkable: false,
})

export function useSelectionContext(): SelectionApi {
  return useContext(SelectionContext)
}
