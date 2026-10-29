import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'

import { errorMessage, request } from '../api/client'
import { SetupMutation } from '../api/operations'
import { Layout } from './Layout'

export function SetupPage() {
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
      await qc.invalidateQueries()
      navigate('/', { replace: true })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setup.mutate()
  }
  return (
    <Layout title="Set up Yalkos">
      <p>The setup code is printed in the server log (“setup required”).</p>
      <form onSubmit={submit}>
        <label>Setup code<input value={form.code} onChange={set('code')} required /></label>
        <label>Workspace name<input value={form.workspaceName} onChange={set('workspaceName')} required /></label>
        <label>Your name<input value={form.name} onChange={set('name')} required /></label>
        <label>Email<input type="email" autoComplete="username" value={form.email} onChange={set('email')} required /></label>
        <label>Password<input type="password" autoComplete="new-password" value={form.password} onChange={set('password')} required minLength={8} /></label>
        {setup.error && <p role="alert">{errorMessage(setup.error)}</p>}
        <button type="submit" disabled={setup.isPending}>Create workspace</button>
      </form>
    </Layout>
  )
}
