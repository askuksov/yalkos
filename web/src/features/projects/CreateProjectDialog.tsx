import { useMutation } from '@tanstack/react-query'
import { type FormEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import { errorMessage, GraphQLRequestError, request } from '../../api/client'
import { Button } from '../../ui/Button'
import { Dialog } from '../../ui/Dialog'
import { keyPattern, normalizeKey, suggestKey } from './keySuggest'
import { CreateProjectMutation } from './operations'
import s from './projects.module.css'
import { useInvalidateProjects } from './useInvalidateProjects'

type Props = { open: boolean; onClose: () => void; takenKeys: Set<string> }

export function CreateProjectDialog({ open, onClose, takenKeys }: Props) {
  const { t } = useTranslation(['projects', 'common'])
  return (
    <Dialog open={open} onClose={onClose} title={t('create.title')}>
      <CreateForm onClose={onClose} takenKeys={takenKeys} />
    </Dialog>
  )
}

function CreateForm({ onClose, takenKeys }: Omit<Props, 'open'>) {
  const { t } = useTranslation(['projects', 'common'])
  const navigate = useNavigate()
  const invalidate = useInvalidateProjects()
  const ids = { name: useId(), key: useId(), desc: useId(), keyHint: useId() }
  const [name, setName] = useState('')
  const [key, setKey] = useState('')
  const [keyEdited, setKeyEdited] = useState(false)
  const [description, setDescription] = useState('')
  const [errors, setErrors] = useState<Record<string, string>>({})
  const shownKey = keyEdited ? key : suggestKey(name, takenKeys)
  const keyOk = keyPattern.test(shownKey)
  const create = useMutation({
    mutationFn: () => request(CreateProjectMutation, { input: { name, key: shownKey, ...(description.trim() ? { description } : {}) } }),
    onSuccess: (d) => {
      void invalidate()
      onClose()
      navigate(`/p/${d.createProject.key}`)
    },
    onError: (err) => {
      const field = err instanceof GraphQLRequestError && err.code === 'INVALID_INPUT' ? err.field : undefined
      setErrors({ [field === 'name' || field === 'key' || field === 'description' ? field : 'form']: errorMessage(err) })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (keyOk && name.trim()) create.mutate()
  }
  return (
    <form onSubmit={submit} className={s.section}>
      <div className={s.form}>
        <label htmlFor={ids.name}>{t('create.name')}</label>
        <div className={s.field}>
          <input id={ids.name} required value={name} onChange={(e) => setName(e.target.value)} aria-invalid={!!errors.name || undefined} />
          {errors.name && <p role="alert" className={s.error}>{errors.name}</p>}
        </div>
        <label htmlFor={ids.key}>{t('create.key')}</label>
        <div className={s.field}>
          <input
            id={ids.key}
            required
            value={shownKey}
            aria-describedby={ids.keyHint}
            aria-invalid={(!keyOk && shownKey !== '') || !!errors.key || undefined}
            onChange={(e) => {
              setKeyEdited(true)
              setKey(normalizeKey(e.target.value))
            }}
          />
          <p id={ids.keyHint} className={s.hint}>
            {keyOk || !shownKey ? t('create.keyHint', { key: shownKey || 'YLK' }) : t('create.keyInvalid')}
          </p>
          {errors.key && <p role="alert" className={s.error}>{errors.key}</p>}
        </div>
        <label htmlFor={ids.desc}>{t('create.description')}</label>
        <textarea id={ids.desc} rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
      </div>
      {errors.form && <p role="alert" className={s.error}>{errors.form}</p>}
      <div className={s.actions}>
        <Button onClick={onClose}>{t('common:actions.cancel')}</Button>
        <Button type="submit" variant="primary" disabled={!keyOk || !name.trim() || create.isPending}>
          {t('create.submit')}
        </Button>
      </div>
    </form>
  )
}
