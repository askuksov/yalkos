import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '../../ui/Button'
import { Popover } from '../../ui/Popover'
import s from './projects.module.css'

const presets = ['#95a2b3', '#888888', '#f2c94c', '#f2994a', '#eb5757', '#bb87fc', '#5e6ad2', '#3a7bd5', '#26b5ce', '#4cb782', '#0f783c', '#6b6b73']
const hexRe = /^#[0-9a-fA-F]{6}$/

type Props = { value: string | null; onChange: (color: string) => void; label: string; disabled?: boolean }

/** 12 preset colours and a hex field, in a popover from a swatch button. */
export function ColorPicker({ value, onChange, label, disabled }: Props) {
  const { t } = useTranslation(['projects', 'common'])
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const [open, setOpen] = useState(false)
  const [hex, setHex] = useState(value ?? '')
  const pick = (c: string) => {
    onChange(c)
    setOpen(false)
  }
  return (
    <>
      <button
        ref={setAnchor}
        type="button"
        className={s.colorButton}
        style={{ background: value ?? 'transparent' }}
        aria-label={label}
        disabled={disabled}
        onClick={() => setOpen(!open)}
      />
      <Popover anchor={anchor} open={open} onClose={() => setOpen(false)}>
        <div className={s.swatches}>
          {presets.map((c) => (
            <button
              key={c}
              type="button"
              className={s.swatch}
              style={{ background: c }}
              aria-label={t('colors.preset', { color: c })}
              aria-pressed={c.toLowerCase() === value?.toLowerCase()}
              onClick={() => pick(c)}
            />
          ))}
        </div>
        <form
          className={s.hex}
          onSubmit={(e) => {
            e.preventDefault()
            // React bubbles submit through the portal into an enclosing form (General settings).
            e.stopPropagation()
            if (hexRe.test(hex)) pick(hex.toLowerCase())
          }}
        >
          <input aria-label={t('colors.custom')} placeholder="#3a7bd5" value={hex} onChange={(e) => setHex(e.target.value.trim())} />
          <Button type="submit" size="sm" disabled={!hexRe.test(hex)}>
            {t('common:actions.save')}
          </Button>
        </form>
      </Popover>
    </>
  )
}
