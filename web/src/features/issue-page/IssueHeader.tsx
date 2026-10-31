import { Copy, Link2, MoreHorizontal } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { Button } from '../../ui/Button'
import { Menu } from '../../ui/Menu'
import { useToast } from '../../ui/useToast'
import s from './issue-page.module.css'
import type { PageIssue } from './types'

type Props = {
  issue: PageIssue
  readOnly: boolean
  onCreateSubIssue: () => void
  onMove: () => void
  onDelete: () => void
}

function issueLink(identifier: string): string {
  return `${location.origin}/issue/${identifier}`
}

/** Breadcrumb (project › parent › issue), copy buttons and the ⋯ menu. */
export function IssueHeader({ issue, readOnly, onCreateSubIssue, onMove, onDelete }: Props) {
  const { t } = useTranslation(['issue', 'common'])
  const toast = useToast()
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const [open, setOpen] = useState(false)
  const copy = (text: string) => {
    void navigator.clipboard?.writeText(text)
    toast.show(t('common:status.copied'))
  }
  return (
    <header className={s.header}>
      <nav aria-label={t('breadcrumb')} className={s.breadcrumb}>
        <Link to={`/p/${issue.project.key}`}>{issue.project.key}</Link>
        {issue.parent && (
          <>
            <span aria-hidden>›</span>
            <Link to={`/issue/${issue.parent.identifier}`}>{issue.parent.identifier}</Link>
          </>
        )}
        <span aria-hidden>›</span>
        <span aria-current="page">{issue.identifier}</span>
      </nav>
      <span className={s.grow} />
      <Button size="sm" variant="ghost" icon={<Copy size={14} aria-hidden />} onClick={() => copy(issue.identifier)}>
        {t('copyId')}
      </Button>
      <Button size="sm" variant="ghost" icon={<Link2 size={14} aria-hidden />} onClick={() => copy(issueLink(issue.identifier))}>
        {t('copyLink')}
      </Button>
      <Button ref={setAnchor} size="sm" variant="ghost" icon={<MoreHorizontal size={16} aria-hidden />} aria-label={t('more')} aria-haspopup="menu" onClick={() => setOpen(!open)} />
      <Menu
        anchor={anchor}
        open={open}
        onClose={() => setOpen(false)}
        label={t('more')}
        items={[
          { label: t('menu.copyIdentifier'), onSelect: () => copy(issue.identifier) },
          { label: t('menu.copyLink'), onSelect: () => copy(issueLink(issue.identifier)) },
          { label: t('menu.createSubIssue'), onSelect: onCreateSubIssue, disabled: readOnly },
          { label: t('menu.move'), onSelect: onMove, disabled: readOnly },
          { label: t('menu.delete'), onSelect: onDelete, danger: true, disabled: readOnly },
        ]}
      />
    </header>
  )
}
