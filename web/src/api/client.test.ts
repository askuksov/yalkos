import { afterEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError, REQUEST_TIMEOUT_MS, request } from './client'
import { DeleteIssueMutation } from '../features/issues/fragments'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

/** A fetch that never answers on its own and rejects when its signal aborts. */
function hangingFetch() {
  return vi.fn((_url: string, init?: RequestInit) => {
    return new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
    })
  })
}

describe('request timeout', () => {
  it('fails a request that gets no answer with TIMEOUT', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', hangingFetch())
    const done = request(DeleteIssueMutation, { id: 'i1' }).catch((e: unknown) => e)
    await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT_MS - 1)
    let settled = false
    void done.then(() => {
      settled = true
    })
    await Promise.resolve()
    expect(settled).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    const err = await done
    expect(err).toBeInstanceOf(GraphQLRequestError)
    expect((err as GraphQLRequestError).code).toBe('TIMEOUT')
  })

  it('keeps NETWORK_ERROR for a request that fails before the bound', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('offline'))))
    const err = await request(DeleteIssueMutation, { id: 'i1' }).catch((e: unknown) => e)
    expect((err as GraphQLRequestError).code).toBe('NETWORK_ERROR')
  })

  it('stops the timer once the answer is read', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify({ data: { deleteIssue: true } })))))
    await expect(request(DeleteIssueMutation, { id: 'i1' })).resolves.toEqual({ deleteIssue: true })
    expect(vi.getTimerCount()).toBe(0)
  })
})
