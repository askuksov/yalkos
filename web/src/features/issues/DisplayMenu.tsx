import { SlidersHorizontal } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '../../ui/Button'
import { Popover } from '../../ui/Popover'
import { orderFields, type ViewParams } from './filters'
import s from './issues.module.css'

type Props = {
  params: ViewParams
  onChange: (next: ViewParams) => void
  /** Project scope: list/board switch (My issues is list-only). */
  onView?: (view: 'list' | 'board') => void
}

/** View, nesting, order and direction. */
export function DisplayMenu({ params, onChange, onView }: Props) {
  const { t } = useTranslation('issues')
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const [open, setOpen] = useState(false)
  const ids = { nested: useId(), order: useId() }
  return (
    <>
      <Button ref={setAnchor} size="sm" icon={<SlidersHorizontal size={14} aria-hidden />} aria-haspopup="dialog" onClick={() => setOpen(!open)}>
        {t('display.button')}
      </Button>
      <Popover anchor={anchor} open={open} onClose={() => setOpen(false)} placement="bottom-end">
        <div className={s.panel} role="group" aria-label={t('display.button')}>
          {onView && (
            <div className={s.row}>
              <span>{t('display.view')}</span>
              <span className={s.segmented}>
                {(['list', 'board'] as const).map((v) => (
                  <button key={v} type="button" aria-pressed={params.view === v} onClick={() => onView(v)}>
                    {t(`display.${v}`)}
                  </button>
                ))}
              </span>
            </div>
          )}
          {params.view === 'list' && (
            <div className={s.row}>
              <label htmlFor={ids.nested}>{t('display.nested')}</label>
              <input id={ids.nested} type="checkbox" checked={params.nest} onChange={(e) => onChange({ ...params, nest: e.target.checked })} />
            </div>
          )}
          {params.view === 'list' && (
            <div className={s.row}>
              <label htmlFor={ids.order}>{t('display.order')}</label>
              <select id={ids.order} value={params.order} onChange={(e) => onChange({ ...params, order: e.target.value as ViewParams['order'] })}>
                {orderFields.map((f) => (
                  <option key={f} value={f}>
                    {t(`display.orders.${f}`)}
                  </option>
                ))}
              </select>
            </div>
          )}
          {params.view === 'list' && (
            <div className={s.row}>
              <span>{t('display.direction')}</span>
              <span className={s.segmented}>
                {(['ASC', 'DESC'] as const).map((d) => (
                  <button key={d} type="button" aria-pressed={params.dir === d} onClick={() => onChange({ ...params, dir: d })}>
                    {t(`display.${d === 'ASC' ? 'asc' : 'desc'}`)}
                  </button>
                ))}
              </span>
            </div>
          )}
        </div>
      </Popover>
    </>
  )
}
