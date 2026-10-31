import { useMutation } from '@tanstack/react-query'
import { type FormEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'

import type { CurrentProject } from '../../app/projectContext'
import { errorMessage, GraphQLRequestError, request } from '../../api/client'
import type { UpdateProjectInput } from '../../gql/graphql'
import { Button } from '../../ui/Button'
import { useToast } from '../../ui/useToast'
import { ColorPicker } from './ColorPicker'
import { keyPattern, normalizeKey } from './keySuggest'
import { UpdateProjectMutation } from './operations'
import s from './projects.module.css'
import { useInvalidateProjects } from './useInvalidateProjects'

/** Name, key, description, icon, color; a key change moves to the new URL. */
export function GeneralSettings({ project, readOnly }: { project: CurrentProject; readOnly: boolean }) {
  const { t } = useTranslation(['projects', 'common'])
  const navigate = useNavigate()
  const toast = useToast()
  const invalidate = useInvalidateProjects()
  const ids = { name: useId(), key: useId(), desc: useId(), icon: useId() }
  const [form, setForm] = useState({ name: project.name, key: project.key, description: project.description, icon: project.icon ?? '' })
  const [color, setColor] = useState(project.color)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const save = useMutation({
    mutationFn: (input: UpdateProjectInput) => request(UpdateProjectMutation, { id: project.id, input }),
    onSuccess: (d) => {
      setErrors({})
      toast.show(t('settings.saved'))
      void invalidate()
      if (d.updateProject.key !== project.key) navigate(`/p/${d.updateProject.key}/settings`, { replace: true })
    },
    onError: (err) => {
      const f = err instanceof GraphQLRequestError && err.code === 'INVALID_INPUT' ? (err.field ?? 'form') : 'form'
      setErrors({ [['name', 'key', 'description', 'icon', 'color'].includes(f) ? f : 'form']: errorMessage(err) })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const input: UpdateProjectInput = {}
    if (form.name !== project.name) input.name = form.name
    if (form.key !== project.key) input.key = form.key
    if (form.description !== project.description) input.description = form.description
    if ((form.icon || null) !== project.icon) input.icon = form.icon || null
    if (color !== project.color) input.color = color
    if (Object.keys(input).length) save.mutate(input)
  }
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: k === 'key' ? normalizeKey(e.target.value) : e.target.value })
  const err = (k: string) => errors[k] && <p role="alert" className={s.error}>{errors[k]}</p>
  return (
    <section className={s.section} aria-labelledby="general-title">
      <h2 id="general-title">{t('settings.general')}</h2>
      <form onSubmit={submit} className={s.section}>
        <fieldset disabled={readOnly} className={s.form} style={{ border: 0, padding: 0, margin: 0 }}>
          <label htmlFor={ids.name}>{t('settings.name')}</label>
          <div className={s.field}>
            <input id={ids.name} required value={form.name} onChange={set('name')} />
            {err('name')}
          </div>
          <label htmlFor={ids.key}>{t('settings.key')}</label>
          <div className={s.field}>
            <input id={ids.key} required value={form.key} onChange={set('key')} aria-invalid={!keyPattern.test(form.key) || undefined} />
            <p className={s.hint}>{t('settings.keyNote')}</p>
            {err('key')}
          </div>
          <label htmlFor={ids.desc}>{t('settings.description')}</label>
          <div className={s.field}>
            <textarea id={ids.desc} rows={3} value={form.description} onChange={set('description')} />
            {err('description')}
          </div>
          <label htmlFor={ids.icon}>{t('settings.icon')}</label>
          <div className={s.field}>
            <input id={ids.icon} value={form.icon} maxLength={32} onChange={set('icon')} />
            {err('icon')}
          </div>
          <span>{t('settings.color')}</span>
          <div className={s.field}>
            <ColorPicker value={color} onChange={setColor} label={t('settings.color')} disabled={readOnly} />
            {err('color')}
          </div>
        </fieldset>
        {err('form')}
        {!readOnly && (
          <div className={s.actions}>
            <Button type="submit" variant="primary" disabled={save.isPending || !keyPattern.test(form.key)}>
              {t('settings.save')}
            </Button>
          </div>
        )}
      </form>
    </section>
  )
}
