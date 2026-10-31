import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { GraphQLRequestError } from '../../api/client'
import { uploadFile } from './upload'

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function reply(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('uploadFile', () => {
  it('posts the issue id before the file, same-origin', async () => {
    fetchMock.mockResolvedValue(reply(201, { id: 'a1', url: '/api/attachments/a1/shot.png', name: 'shot.png', size: 3, contentType: 'image/png', inline: true }))
    const file = new File(['abc'], 'shot.png', { type: 'image/png' })
    const res = await uploadFile(file, 'i1')
    expect(res).toEqual({ id: 'a1', url: '/api/attachments/a1/shot.png', name: 'shot.png', size: 3, contentType: 'image/png', inline: true })
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/attachments')
    expect(init.method).toBe('POST')
    expect(init.credentials).toBe('same-origin')
    const form = init.body as FormData
    expect([...form.keys()]).toEqual(['issueId', 'file'])
    expect(form.get('issueId')).toBe('i1')
  })

  it('leaves out the issue id for a pending upload', async () => {
    fetchMock.mockResolvedValue(reply(201, { id: 'a1', url: '/u', name: 'x', size: 1, contentType: 'text/plain', inline: false }))
    await uploadFile(new File(['x'], 'x.txt'))
    expect([...(fetchMock.mock.calls[0][1] as RequestInit).body as FormData].map(([k]) => k)).toEqual(['file'])
  })

  it('turns 413 into too_large with the limit', async () => {
    fetchMock.mockResolvedValue(reply(413, { error: 'file is too large', field: 'file', reason: 'too_large', params: { maxMb: 25 } }))
    const err = await uploadFile(new File(['x'], 'x.bin')).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(GraphQLRequestError)
    expect(err).toMatchObject({ code: 'INVALID_INPUT', reason: 'too_large', params: { maxMb: 25 } })
  })

  it('turns 401 into an authentication error', async () => {
    fetchMock.mockResolvedValue(reply(401, { error: 'authentication required' }))
    await expect(uploadFile(new File(['x'], 'x.bin'))).rejects.toMatchObject({ code: 'AUTHENTICATION_ERROR' })
  })

  it('maps 400 field errors and network failures', async () => {
    fetchMock.mockResolvedValue(reply(400, { error: 'no such issue', field: 'issueId', reason: 'not_found' }))
    await expect(uploadFile(new File(['x'], 'x.bin'), 'nope')).rejects.toMatchObject({ code: 'INVALID_INPUT', field: 'issueId', reason: 'not_found' })
    fetchMock.mockRejectedValue(new TypeError('offline'))
    await expect(uploadFile(new File(['x'], 'x.bin'))).rejects.toMatchObject({ code: 'NETWORK_ERROR' })
    fetchMock.mockResolvedValue(reply(403, { error: 'forbidden' }))
    await expect(uploadFile(new File(['x'], 'x.bin'))).rejects.toMatchObject({ code: 'FORBIDDEN' })
  })
})
