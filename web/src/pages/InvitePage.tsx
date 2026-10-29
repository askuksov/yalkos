import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { useNavigate, useParams } from 'react-router'

import { errorMessage, request } from '../api/client'
import { AcceptInviteMutation } from '../api/operations'
import { Layout } from './Layout'

export function InvitePage() {
  const { token = '' } = useParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [form, setForm] = useState({ name: '', email: '', password: '' })
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value })
  const accept = useMutation({
    mutationFn: () => request(AcceptInviteMutation, { input: { token, ...form } }),
    onSuccess: async () => {
      // Reset (not just invalidate): RequireAuth is unmounted here, so stale
      // setupStatus/viewer entries would redirect back on the first render.
      await qc.resetQueries()
      navigate('/', { replace: true })
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    accept.mutate()
  }
  return (
    <Layout title="Join the workspace">
      <form onSubmit={submit}>
        <label>Name<input value={form.name} onChange={set('name')} required /></label>
        <label>Email<input type="email" autoComplete="username" value={form.email} onChange={set('email')} required /></label>
        <label>Password<input type="password" autoComplete="new-password" value={form.password} onChange={set('password')} required minLength={8} /></label>
        {accept.error && <p role="alert">{errorMessage(accept.error)}</p>}
        <button type="submit" disabled={accept.isPending}>Join workspace</button>
      </form>
    </Layout>
  )
}
