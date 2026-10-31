import { createContext, useContext, useEffect } from 'react'

export type CreateIssueOptions = {
  projectId?: string
  parentId?: string
  /** Shown on the parent chip; the server derives the project from parentId. */
  parentIdentifier?: string
  stateId?: string
  /** New issues from a milestone page join that milestone. */
  milestoneId?: string
}

export type CreateIssueApi = {
  openCreateIssue: (opts?: CreateIssueOptions) => void
  /** Defaults for C and "New issue" from the current page (project, parent). */
  setPageDefaults: (opts: CreateIssueOptions | null) => void
  /** Opens with the current page defaults. */
  openForPage: () => void
}

export const CreateIssueContext = createContext<CreateIssueApi | null>(null)

export function useCreateIssue(): CreateIssueApi {
  const api = useContext(CreateIssueContext)
  if (!api) throw new Error('useCreateIssue outside CreateIssueProvider')
  return api
}

/** Registers create defaults while the calling page is shown. */
export function useCreateDefaults(opts: CreateIssueOptions | null) {
  const { setPageDefaults } = useCreateIssue()
  const key = JSON.stringify(opts)
  useEffect(() => {
    setPageDefaults(key === 'null' ? null : (JSON.parse(key) as CreateIssueOptions))
    return () => setPageDefaults(null)
  }, [key, setPageDefaults])
}
