import { useTranslation } from 'react-i18next'

import { shortcutLabel } from '../../lib/hotkeys'
import { Dialog } from '../../ui/Dialog'
import { Kbd } from '../../ui/Kbd'
import s from './command.module.css'

type Row = { keys: string[]; label: string }

/** Keyboard shortcuts, labelled with US-layout keys. */
export function ShortcutsHelp({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation('command')
  const groups: { title: string; rows: Row[] }[] = [
    {
      title: t('help.anywhere'),
      rows: [
        { keys: [shortcutLabel('mod+k')], label: t('help.rows.palette') },
        { keys: ['Esc'], label: t('help.rows.escape') },
      ],
    },
    {
      title: t('help.shell'),
      rows: [
        { keys: ['/'], label: t('help.rows.search') },
        { keys: ['C'], label: t('help.rows.create') },
        { keys: ['['], label: t('help.rows.sidebar') },
        { keys: ['?'], label: t('help.rows.help') },
      ],
    },
    {
      title: t('help.lists'),
      rows: [
        { keys: ['J', 'K'], label: t('help.rows.next') },
        { keys: ['H', 'L'], label: t('help.rows.columns') },
        { keys: ['Enter'], label: t('help.rows.open') },
        { keys: ['X'], label: t('help.rows.select') },
        { keys: ['F'], label: t('help.rows.filter') },
      ],
    },
    {
      title: t('help.issue'),
      rows: [
        { keys: ['S'], label: t('help.rows.state') },
        { keys: ['A'], label: t('help.rows.assignee') },
        { keys: ['P'], label: t('help.rows.priority') },
        { keys: ['E'], label: t('help.rows.edit') },
      ],
    },
  ]
  return (
    <Dialog open={open} onClose={onClose} title={t('help.title')}>
      <div className={s.help}>
        {groups.map((g) => (
          <section key={g.title}>
            <h3>{g.title}</h3>
            <dl>
              {g.rows.map((r) => (
                <div key={r.label} className={s.helpRow}>
                  <dt>{r.label}</dt>
                  <dd>
                    {r.keys.map((k) => (
                      <Kbd key={k}>{k}</Kbd>
                    ))}
                  </dd>
                </div>
              ))}
            </dl>
          </section>
        ))}
      </div>
    </Dialog>
  )
}
