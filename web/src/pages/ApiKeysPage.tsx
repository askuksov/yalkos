import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { type FormEvent, useState } from 'react'
import { Link } from 'react-router'

import { errorMessage, request } from '../api/client'
import { ApiKeysQuery, CreateApiKeyMutation, RevokeApiKeyMutation } from '../api/operations'
import { formatDateTime } from '../lib/date'
import { Layout } from './Layout'

export function ApiKeysPage() {
  const qc = useQueryClient()
  const keys = useQuery({ queryKey: ['apiKeys'], queryFn: () => request(ApiKeysQuery) })
  const [name, setName] = useState('')
  const [created, setCreated] = useState('')
  const refresh = () => qc.invalidateQueries({ queryKey: ['apiKeys'] })
  const create = useMutation({
    mutationFn: () => request(CreateApiKeyMutation, { name }),
    onSuccess: (d) => {
      setCreated(d.createApiKey.key)
      setName('')
      void refresh()
    },
  })
  const revoke = useMutation({ mutationFn: (id: string) => request(RevokeApiKeyMutation, { id }), onSuccess: refresh })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate()
  }

  return (
    <Layout title="API keys">
      <Link to="/">← Home</Link>
      <p>Use a key with any Linear-compatible tool: <code>Authorization: &lt;key&gt;</code>.</p>
      {created ? (
        <section className="secret">
          <p>
            <strong>You will not see this key again.</strong> Copy it now.
          </p>
          <code>{created}</code>{' '}
          <button type="button" onClick={() => void navigator.clipboard?.writeText(created)}>Copy</button>{' '}
          <button type="button" onClick={() => setCreated('')}>Done</button>
        </section>
      ) : (
        <form onSubmit={submit}>
          <label>Key name<input value={name} onChange={(e) => setName(e.target.value)} required maxLength={100} /></label>
          <button type="submit" disabled={create.isPending}>Create key</button>
        </form>
      )}
      {(create.error ?? revoke.error) && <p role="alert">{errorMessage(create.error ?? revoke.error)}</p>}
      <table>
        <thead><tr><th>Name</th><th>Key</th><th>Created</th><th>Last used</th><th /></tr></thead>
        <tbody>
          {keys.data?.apiKeys.map((k) => (
            <tr key={k.id}>
              <td>{k.name}</td>
              <td><code>{k.displayPrefix}</code></td>
              <td>{formatDateTime(k.createdAt)}</td>
              <td>{k.lastUsedAt ? formatDateTime(k.lastUsedAt) : 'never'}</td>
              <td><button type="button" onClick={() => revoke.mutate(k.id)}>Revoke</button></td>
            </tr>
          ))}
        </tbody>
      </table>
    </Layout>
  )
}
