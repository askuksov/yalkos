import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { useLocation, useNavigate } from 'react-router'

import { errorMessage, request } from '../api/client'
import { LoginMutation } from '../api/operations'
import { Layout } from './Layout'

export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const qc = useQueryClient()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const login = useMutation({
    mutationFn: () => request(LoginMutation, { email, password }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['viewer'] })
      const from = (location.state as { from?: string } | null)?.from
      navigate(from ?? '/', { replace: true })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    login.mutate()
  }
  return (
    <Layout title="Sign in">
      <form onSubmit={submit}>
        <label>
          Email
          <input type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label>
          Password
          <input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>
        {login.error && <p role="alert">{errorMessage(login.error)}</p>}
        <button type="submit" disabled={login.isPending}>Sign in</button>
      </form>
    </Layout>
  )
}
