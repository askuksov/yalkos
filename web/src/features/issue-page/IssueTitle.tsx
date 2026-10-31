import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useIssueMutation } from '../../lib/issueCache'
import s from './issue-page.module.css'
import type { PageIssue } from './types'

/** Click to edit; Enter or leaving the field saves, Esc cancels. One line, 1–255 characters. */
export function IssueTitle({ issue, readOnly }: { issue: PageIssue; readOnly: boolean }) {
  const { t } = useTranslation('issue')
  const update = useIssueMutation()
  const [draft, setDraft] = useState<string | null>(null)

  const save = () => {
    if (draft === null) return
    const title = draft.replace(/\s+/g, ' ').trim()
    setDraft(null)
    if (!title || title === issue.title) return
    update.mutate({ id: issue.id, input: { title }, optimistic: { title } })
  }

  if (draft !== null) {
    return (
      <input
        className={s.titleInput}
        aria-label={t('title.label')}
        autoFocus
        maxLength={255}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            save()
          } else if (e.key === 'Escape') {
            e.preventDefault()
            setDraft(null)
          }
        }}
      />
    )
  }
  return readOnly ? (
    <h1 className={s.title}>{issue.title}</h1>
  ) : (
    <h1 className={s.title}>
      <button type="button" className={s.titleButton} title={t('title.edit')} onClick={() => setDraft(issue.title)}>
        {issue.title}
      </button>
    </h1>
  )
}
