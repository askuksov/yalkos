import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'

import { errorMessage, request } from '../api/client'
import { LoginMutation } from '../api/operations'
import { startSession } from '../auth/session'
import { Button } from '../ui/Button'
import { Layout } from './Layout'

export function LoginPage() {
  const { t } = useTranslation('auth')
  const navigate = useNavigate()
  const location = useLocation()
  const qc = useQueryClient()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const login = useMutation({
    mutationFn: () => request(LoginMutation, { email, password }),
    onSuccess: async () => {
      startSession()
      // Reset (not just invalidate): RequireAuth is unmounted here, so stale
      // setupStatus/viewer entries would redirect back on the first render.
      await qc.resetQueries()
      const from = (location.state as { from?: string } | null)?.from
      navigate(from ?? '/', { replace: true })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    login.mutate()
  }
  return (
    <Layout title={t('login.title')}>
      <form onSubmit={submit}>
        <label>
          {t('login.email')}
          <input type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label>
          {t('login.password')}
          <input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>
        {login.error && <p role="alert">{errorMessage(login.error)}</p>}
        <Button type="submit" variant="primary" disabled={login.isPending}>
          {t('login.submit')}
        </Button>
      </form>
    </Layout>
  )
}
