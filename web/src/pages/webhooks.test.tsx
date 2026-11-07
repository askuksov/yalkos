import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../api/client'
import { member, owner, signedInAs } from '../test/fixtures'
import { renderAt } from '../test/render'
import { callsOf, mocked, serve } from '../test/serve'

vi.mock('../api/client', async (orig) => {
  const actual = await orig<typeof import('../api/client')>()
  return { ...actual, request: vi.fn() }
})

beforeEach(() => {
  mocked.mockReset()
})

/** Local-time ISO timestamp, so the expected text does not depend on the time zone. */
const at = (day: number, hour: number, minute: number) => new Date(2026, 10, day, hour, minute).toISOString()

const secret = `yal_wh_${'a'.repeat(40)}`

function delivery(over: Record<string, unknown> = {}) {
  return {
    id: 'd1', resource: 'issue', action: 'update', entityId: 'e1', status: 'SUCCEEDED', attempts: 1,
    responseStatus: 200, error: null, durationMs: 84, createdAt: at(4, 18, 20), lastAttemptAt: at(4, 18, 23), nextAttemptAt: null,
    ...over,
  }
}

function webhook(over: Record<string, unknown> = {}) {
  return {
    id: 'w1', name: 'CI bridge', url: 'https://ci.example.com/hooks/yalkos', secret, enabled: true,
    disabledReason: null, disabledAt: null, resources: ['ISSUE', 'COMMENT'], project: null as { id: string; key: string; name: string } | null,
    createdAt: at(1, 9, 0), updatedAt: at(1, 9, 0), deliveries: [] as unknown[],
    ...over,
  }
}

const yalkos = { id: 'p1', key: 'YLK', name: 'Yalkos' }

function handlers(extra: Record<string, (vars: Record<string, unknown> | undefined) => unknown> = {}, viewer = owner) {
  return { ...signedInAs(viewer), ...extra }
}

describe('webhooks list', () => {
  it('lists webhooks with scope, events, status and last delivery', async () => {
    serve(handlers({
      Webhooks: () => ({
        webhooks: [
          webhook({
            enabled: false, disabledReason: 'FAILURES', disabledAt: at(4, 18, 30),
            deliveries: [delivery({ status: 'FAILED', lastAttemptAt: at(4, 18, 23) })],
          }),
          webhook({ id: 'w2', name: '', url: 'https://hooks.example.org/in', project: yalkos, resources: ['LABEL'], deliveries: [] }),
        ],
      }),
    }))
    renderAt('/settings/webhooks')
    const first = (await screen.findByRole('link', { name: 'CI bridge' })).closest('tr') as HTMLElement
    expect(within(first).getByText('All projects')).toBeInTheDocument()
    expect(within(first).getByText('Issues, Comments')).toBeInTheDocument()
    expect(within(first).getByText('Disabled after failures')).toBeInTheDocument()
    expect(within(first).getByText(/2026-11-04 18:23/)).toBeInTheDocument()
    expect(within(first).getByText(/Failed/)).toBeInTheDocument()

    // No name: the host of the URL stands in; no deliveries yet: a dash.
    const second = (await screen.findByRole('link', { name: 'hooks.example.org' })).closest('tr') as HTMLElement
    expect(within(second).getByText('YLK')).toBeInTheDocument()
    expect(within(second).getByText('Labels')).toBeInTheDocument()
    expect(within(second).getByText('Enabled')).toBeInTheDocument()
    expect(within(second).getByText('—')).toBeInTheDocument()
    expect(within(second).queryByText('All projects')).not.toBeInTheDocument()
    expect(screen.queryByText('No webhooks yet.')).not.toBeInTheDocument()
  })

  it('shows the empty state', async () => {
    serve(handlers({ Webhooks: () => ({ webhooks: [] }) }))
    renderAt('/settings/webhooks')
    expect(await screen.findByText('No webhooks yet.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Webhooks' })).toBeInTheDocument()
    expect(screen.getByText(/Requests are signed in the Linear-compatible format\./)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('opens a webhook from its row', async () => {
    serve(handlers({
      Webhooks: () => ({ webhooks: [webhook()] }),
      Webhook: () => ({ webhook: webhook() }),
    }))
    renderAt('/settings/webhooks')
    await userEvent.click(await screen.findByText('Issues, Comments'))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/settings\/webhooks\/w1$/))
  })
})

describe('new webhook', () => {
  it('creates a webhook and opens it with the secret revealed', async () => {
    const user = userEvent.setup()
    serve(handlers({
      Webhooks: () => ({ webhooks: [] }),
      CreateWebhook: (vars) => {
        const input = vars?.input as { resources: string[] }
        if (input.resources.length === 0) {
          throw new GraphQLRequestError('INVALID_INPUT', 'choose at least one event type', { field: 'resources', reason: 'required' })
        }
        return { createWebhook: { id: 'w9' } }
      },
      Webhook: () => ({ webhook: webhook({ id: 'w9', name: '' }) }),
    }))
    renderAt('/settings/webhooks')
    await user.click(await screen.findByRole('button', { name: 'New webhook' }))
    const dialog = await screen.findByRole('dialog', { name: 'New webhook' })

    // URL is required.
    const submit = within(dialog).getByRole('button', { name: 'Create webhook' })
    expect(submit).toBeDisabled()
    expect(within(dialog).getByText('Leave empty to generate one')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('Enabled')).toBeChecked()
    await user.type(within(dialog).getByLabelText('URL'), 'https://ci.example.com/hooks/yalkos')
    expect(submit).toBeEnabled()

    // No event type chosen: the API refuses and the text shows next to the events.
    await user.click(submit)
    expect(await within(dialog).findByText('Choose at least one event type')).toBeInTheDocument()
    expect(callsOf('CreateWebhook')[0].input).toEqual({ url: 'https://ci.example.com/hooks/yalkos', resources: [], enabled: true })
    expect(screen.getByTestId('where')).toHaveTextContent(/^\/settings\/webhooks$/)

    // Positive control: with an event the same form goes through.
    await user.click(within(dialog).getByRole('checkbox', { name: 'Issues' }))
    await user.click(within(dialog).getByRole('checkbox', { name: 'Milestones' }))
    await user.click(submit)
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/settings\/webhooks\/w9$/))
    expect(callsOf('CreateWebhook')).toHaveLength(2)
    expect(callsOf('CreateWebhook')[1].input).toEqual({
      url: 'https://ci.example.com/hooks/yalkos', resources: ['ISSUE', 'MILESTONE'], enabled: true,
    })
    // The secret is shown once after creation.
    expect(await screen.findByText(secret)).toBeInTheDocument()
    expect(screen.getByText('Copy the secret into your receiver')).toBeInTheDocument()
  })

  it('sends name, scope and a chosen secret', async () => {
    const user = userEvent.setup()
    serve(handlers({
      Webhooks: () => ({ webhooks: [] }),
      CreateWebhook: () => ({ createWebhook: { id: 'w9' } }),
      Webhook: () => ({ webhook: webhook({ id: 'w9' }) }),
    }))
    renderAt('/settings/webhooks')
    await user.click(await screen.findByRole('button', { name: 'New webhook' }))
    const dialog = await screen.findByRole('dialog', { name: 'New webhook' })
    await user.type(within(dialog).getByLabelText('URL'), 'https://ci.example.com/in')
    await user.type(within(dialog).getByLabelText('Name'), 'CI')
    await user.selectOptions(await within(dialog).findByLabelText('Project'), 'Yalkos')
    await user.click(within(dialog).getByRole('checkbox', { name: 'Comments' }))
    await user.type(within(dialog).getByLabelText('Secret'), 'my-own-secret')
    await user.click(within(dialog).getByLabelText('Enabled'))
    await user.click(within(dialog).getByRole('button', { name: 'Create webhook' }))
    await waitFor(() => expect(callsOf('CreateWebhook')).toHaveLength(1))
    expect(callsOf('CreateWebhook')[0].input).toEqual({
      url: 'https://ci.example.com/in', name: 'CI', projectId: 'p1', resources: ['COMMENT'], secret: 'my-own-secret', enabled: false,
    })
  })

  it('cancel closes the dialog without creating', async () => {
    const user = userEvent.setup()
    serve(handlers({ Webhooks: () => ({ webhooks: [] }) }))
    renderAt('/settings/webhooks')
    await user.click(await screen.findByRole('button', { name: 'New webhook' }))
    const dialog = await screen.findByRole('dialog', { name: 'New webhook' })
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New webhook' })).not.toBeInTheDocument())
    expect(callsOf('CreateWebhook')).toHaveLength(0)
  })
})

describe('webhook page', () => {
  it('masks the secret until Show, copies it, rotates after confirm', async () => {
    const user = userEvent.setup()
    const fresh = `yal_wh_${'b'.repeat(40)}`
    let current = secret
    serve(handlers({
      Webhook: () => ({ webhook: webhook({ secret: current }) }),
      RotateWebhookSecret: () => {
        current = fresh
        return { rotateWebhookSecret: { id: 'w1', secret: fresh } }
      },
    }))
    renderAt('/settings/webhooks/w1')
    expect(await screen.findByText('yal_wh_••••')).toBeInTheDocument()
    expect(screen.queryByText(secret)).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Show' }))
    expect(screen.getByText(secret)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Hide' }))
    expect(screen.queryByText(secret)).not.toBeInTheDocument()

    // Copy works while the secret is masked.
    await user.click(screen.getByRole('button', { name: 'Copy' }))
    expect(await navigator.clipboard.readText()).toBe(secret)

    // Rotation asks first; Cancel changes nothing.
    await user.click(screen.getByRole('button', { name: 'Rotate secret' }))
    const dialog = await screen.findByRole('dialog', { name: 'Rotate secret' })
    expect(within(dialog).getByText('Rotate the secret? Deliveries signed with the old secret stop being accepted by the receiver until you update it.')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(callsOf('RotateWebhookSecret')).toHaveLength(0)

    await user.click(screen.getByRole('button', { name: 'Rotate secret' }))
    await user.click(within(await screen.findByRole('dialog', { name: 'Rotate secret' })).getByRole('button', { name: 'Rotate' }))
    await waitFor(() => expect(callsOf('RotateWebhookSecret')).toEqual([{ id: 'w1' }]))
    // The new secret is shown so it can be copied right away.
    expect(await screen.findByText(fresh)).toBeInTheDocument()
    expect(screen.queryByText(secret)).not.toBeInTheDocument()
  })

  it('sends a test and shows the result inline', async () => {
    const user = userEvent.setup()
    let outcome = delivery({ id: 'dt', resource: 'test', action: 'test', status: 'SUCCEEDED', responseStatus: 200, durationMs: 84 })
    let log: unknown[] = []
    serve(handlers({
      Webhook: () => ({ webhook: webhook({ deliveries: log }) }),
      TestWebhook: () => {
        log = [outcome]
        return { testWebhook: outcome }
      },
    }))
    renderAt('/settings/webhooks/w1')
    await user.click(await screen.findByRole('button', { name: 'Send test' }))
    expect(await screen.findByText('Delivered: HTTP 200 in 84 ms')).toBeInTheDocument()
    expect(callsOf('TestWebhook')).toEqual([{ id: 'w1' }])
    // The result is added to the log.
    const table = await screen.findByRole('table', { name: 'Deliveries' })
    expect(within(table).getByText('Test')).toBeInTheDocument()

    outcome = delivery({ id: 'dt2', resource: 'test', action: 'test', status: 'FAILED', responseStatus: null, error: 'timeout after 5s', durationMs: 5000 })
    await user.click(screen.getByRole('button', { name: 'Send test' }))
    expect(await screen.findByText('Failed: timeout after 5s')).toBeInTheDocument()
    expect(screen.queryByText('Delivered: HTTP 200 in 84 ms')).not.toBeInTheDocument()
  })

  it('shows the failures banner and enables the webhook', async () => {
    const user = userEvent.setup()
    let state = webhook({ enabled: false, disabledReason: 'FAILURES', disabledAt: at(4, 18, 30) })
    serve(handlers({
      Webhook: () => ({ webhook: state }),
      UpdateWebhook: () => {
        state = webhook()
        return { updateWebhook: state }
      },
    }))
    renderAt('/settings/webhooks/w1')
    expect(await screen.findByText('Disabled after repeated failed deliveries on 2026-11-04 18:30.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Enable' }))
    await waitFor(() => expect(callsOf('UpdateWebhook')).toEqual([{ id: 'w1', input: { enabled: true } }]))
    await waitFor(() => expect(screen.queryByText(/Disabled after repeated failed deliveries/)).not.toBeInTheDocument())
  })

  it('shows no banner for a webhook switched off by hand', async () => {
    serve(handlers({ Webhook: () => ({ webhook: webhook({ enabled: false }) }) }))
    renderAt('/settings/webhooks/w1')
    expect(await screen.findByLabelText('Enabled')).not.toBeChecked()
    expect(screen.queryByText(/Disabled after repeated failed deliveries/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Enable' })).not.toBeInTheDocument()
  })

  it('saves only what changed, and only when the form is dirty', async () => {
    const user = userEvent.setup()
    let state = webhook({ project: yalkos })
    serve(handlers({
      Webhook: () => ({ webhook: state }),
      UpdateWebhook: (vars) => {
        const input = vars?.input as Record<string, unknown>
        state = { ...state, ...(input.name !== undefined ? { name: input.name as string } : {}), ...('projectId' in input ? { project: null } : {}) }
        return { updateWebhook: state }
      },
    }))
    renderAt('/settings/webhooks/w1')
    const name = await screen.findByLabelText('Name')
    expect(name).toHaveValue('CI bridge')
    expect(screen.queryByRole('button', { name: 'Save changes' })).not.toBeInTheDocument()

    await user.clear(name)
    await user.type(name, 'Deploys')
    await user.selectOptions(await screen.findByLabelText('Project'), 'All projects')
    await user.click(screen.getByRole('checkbox', { name: 'Labels' }))
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(callsOf('UpdateWebhook')).toHaveLength(1))
    expect(callsOf('UpdateWebhook')[0]).toEqual({
      id: 'w1', input: { name: 'Deploys', projectId: null, resources: ['ISSUE', 'COMMENT', 'LABEL'] },
    })
    // Saved: the form is clean again.
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Save changes' })).not.toBeInTheDocument())
  })

  it('renders deliveries with attempts and next attempt', async () => {
    const user = userEvent.setup()
    const rows = [
      delivery({ id: 'd-retry', status: 'RETRYING', attempts: 2, responseStatus: 503, error: 'receiver returned 503', nextAttemptAt: at(4, 19, 23), lastAttemptAt: at(4, 18, 23) }),
      delivery({ id: 'd-ok', resource: 'comment', action: 'create', status: 'SUCCEEDED', attempts: 1, durationMs: 120, nextAttemptAt: null }),
      delivery({ id: 'd-fail', resource: 'label', action: 'remove', status: 'FAILED', attempts: 4, responseStatus: null, error: 'connection refused', durationMs: null }),
      delivery({ id: 'd-ms', resource: 'milestone', action: 'create', status: 'PENDING', attempts: 0, responseStatus: null, durationMs: null, lastAttemptAt: null }),
      delivery({ id: 'd-cancel', resource: 'comment', action: 'remove', status: 'CANCELED', attempts: 1, responseStatus: null, durationMs: null }),
    ]
    serve(handlers({ Webhook: () => ({ webhook: webhook({ deliveries: rows }) }) }))
    renderAt('/settings/webhooks/w1')
    const table = await screen.findByRole('table', { name: 'Deliveries' })
    for (const text of ['Issue updated', 'Comment created', 'Comment removed', 'Label removed', 'Milestone created']) {
      expect(within(table).getByText(text)).toBeInTheDocument()
    }
    for (const text of ['Retrying', 'Delivered', 'Failed', 'Pending', 'Canceled']) {
      expect(within(table).getByText(text)).toBeInTheDocument()
    }
    const retry = within(table).getByText('Retrying').closest('tr') as HTMLElement
    expect(within(retry).getByText('2 of 4')).toBeInTheDocument()
    expect(within(retry).getByText('503')).toBeInTheDocument()
    expect(within(retry).getByText('2026-11-04 19:23')).toBeInTheDocument()
    expect(within(retry).getByText('84 ms')).toBeInTheDocument()
    // Next attempt shows for Retrying only.
    const ok = within(table).getByText('Delivered').closest('tr') as HTMLElement
    expect(within(ok).queryByText('2026-11-04 19:23')).not.toBeInTheDocument()
    expect(within(table).getAllByText('2026-11-04 19:23')).toHaveLength(1)

    // A row expands to the error and the delivery id.
    expect(screen.queryByText('receiver returned 503')).not.toBeInTheDocument()
    expect(screen.queryByText('d-retry')).not.toBeInTheDocument()
    await user.click(within(retry).getByRole('button', { name: 'Show details' }))
    expect(screen.getByText('receiver returned 503')).toBeInTheDocument()
    expect(screen.getByText('d-retry')).toBeInTheDocument()
    expect(screen.queryByText('connection refused')).not.toBeInTheDocument()
    await user.click(within(retry).getByRole('button', { name: 'Hide details' }))
    expect(screen.queryByText('receiver returned 503')).not.toBeInTheDocument()
  })

  it('refreshes the delivery log on request', async () => {
    const user = userEvent.setup()
    serve(handlers({ Webhook: () => ({ webhook: webhook() }) }))
    renderAt('/settings/webhooks/w1')
    expect(await screen.findByText('No deliveries yet.')).toBeInTheDocument()
    const before = callsOf('Webhook').length
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(callsOf('Webhook').length).toBeGreaterThan(before))
  })

  it('deletes after confirm', async () => {
    const user = userEvent.setup()
    serve(handlers({
      Webhook: () => ({ webhook: webhook() }),
      Webhooks: () => ({ webhooks: [] }),
      DeleteWebhook: () => ({ deleteWebhook: true }),
    }))
    renderAt('/settings/webhooks/w1')
    await user.click(await screen.findByRole('button', { name: 'Delete webhook' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete webhook' })
    expect(within(dialog).getByText('Delete this webhook? Pending deliveries are dropped.')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(callsOf('DeleteWebhook')).toHaveLength(0)
    expect(screen.getByTestId('where')).toHaveTextContent(/^\/settings\/webhooks\/w1$/)

    await user.click(screen.getByRole('button', { name: 'Delete webhook' }))
    await user.click(within(await screen.findByRole('dialog', { name: 'Delete webhook' })).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(callsOf('DeleteWebhook')).toEqual([{ id: 'w1' }]))
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/settings\/webhooks$/))
  })

  it('shows not found for an unknown webhook', async () => {
    serve(handlers({
      Webhook: () => {
        throw new GraphQLRequestError('NOT_FOUND', 'Webhook not found')
      },
    }))
    renderAt('/settings/webhooks/nope')
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
  })
})

describe('access', () => {
  it('hides the sidebar link and the page for members', async () => {
    serve(handlers({ Webhooks: () => ({ webhooks: [] }), Webhook: () => ({ webhook: webhook() }) }, member))
    renderAt('/settings/webhooks')
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'API keys' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Webhooks' })).not.toBeInTheDocument()
    expect(callsOf('Webhooks')).toHaveLength(0)
  })

  it('hides a webhook URL from members', async () => {
    serve(handlers({ Webhook: () => ({ webhook: webhook() }) }, member))
    renderAt('/settings/webhooks/w1')
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
    expect(callsOf('Webhook')).toHaveLength(0)
    expect(screen.queryByText(secret)).not.toBeInTheDocument()
  })

  it('shows the sidebar link to owners and opens the list', async () => {
    const user = userEvent.setup()
    serve(handlers({ Webhooks: () => ({ webhooks: [] }), ApiKeys: () => ({ apiKeys: [] }) }))
    renderAt('/settings/api-keys')
    const apiKeys = await screen.findByRole('link', { name: 'API keys' })
    const link = screen.getByRole('link', { name: 'Webhooks' })
    // The link follows "API keys".
    expect(apiKeys.compareDocumentPosition(link) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    await user.click(link)
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/settings\/webhooks$/))
    expect(await screen.findByText('No webhooks yet.')).toBeInTheDocument()
  })
})
