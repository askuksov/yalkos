import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router'

import { errorMessage, request } from '../api/client'
import { SetupMutation } from '../api/operations'
import { startSession } from '../auth/session'
import { Button } from '../ui/Button'
import { Layout } from './Layout'

export function SetupPage() {
  const { t } = useTranslation('auth')
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [form, setForm] = useState({
    code: params.get('code') ?? '', workspaceName: '', name: '', email: '', password: '',
  })
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value })
  const setup = useMutation({
    mutationFn: () => request(SetupMutation, { input: form }),
    onSuccess: async () => {
      startSession()
      // Reset (not just invalidate): RequireAuth is unmounted here, so stale
      // setupStatus/viewer entries would redirect back on the first render.
      await qc.resetQueries()
      navigate('/', { replace: true })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setup.mutate()
  }
  return (
    <Layout title={t('setup.title')}>
      <p>{t('setup.hint')}</p>
      <form onSubmit={submit}>
        <label>{t('setup.code')}<input value={form.code} onChange={set('code')} required /></label>
        <label>{t('setup.workspaceName')}<input value={form.workspaceName} onChange={set('workspaceName')} required /></label>
        <label>{t('setup.name')}<input value={form.name} onChange={set('name')} required /></label>
        <label>{t('setup.email')}<input type="email" autoComplete="username" value={form.email} onChange={set('email')} required /></label>
        <label>{t('setup.password')}<input type="password" autoComplete="new-password" value={form.password} onChange={set('password')} required minLength={8} /></label>
        {setup.error && <p role="alert">{errorMessage(setup.error)}</p>}
        <Button type="submit" variant="primary" disabled={setup.isPending}>{t('setup.submit')}</Button>
      </form>
    </Layout>
  )
}
