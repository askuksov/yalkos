import { describe, expect, it } from 'vitest'

import { isInsecureRemote } from './transport'

describe('isInsecureRemote', () => {
  it.each([
    ['https://example.com', false],
    ['https://192.168.1.10', false],
    ['http://localhost:8080', false],
    ['http://localhost.', false],
    ['http://127.0.0.1', false],
    ['http://127.5.5.5', false],
    ['http://[::1]', false],
    ['http://[::1]:5173', false],
    ['http://app.localhost', false],
    ['http://a.b.localhost:3000', false],
    ['http://example.com', true],
    ['http://192.168.1.10', true],
    ['http://10.0.0.5', true],
    ['http://0.0.0.0', true],
    ['http://128.0.0.1', true],
    ['http://localhost.example.com', true],
    ['http://127.0.0.1.example.com', true],
    ['http://notlocalhost', true],
    ['http://[2001:db8::1]', true],
  ])('%s -> %s', (href, insecure) => {
    expect(isInsecureRemote(new URL(href))).toBe(insecure)
  })
})
