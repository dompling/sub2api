import { beforeEach, describe, expect, it, vi } from 'vitest'
import { create, update } from '../keys'

const { post, put } = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { post, put } }))

describe('API key model restrictions', () => {
  beforeEach(() => {
    post.mockReset().mockResolvedValue({ data: { id: 1 } })
    put.mockReset().mockResolvedValue({ data: { id: 1 } })
  })

  it('sends exact and wildcard key restrictions without replacing the group model allowlist', async () => {
    await create('Restricted key', 42, undefined, undefined, undefined, undefined, undefined, undefined, ['gpt-*', 'claude-sonnet-4'])

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'Restricted key',
      group_id: 42,
      allowed_models: ['gpt-*', 'claude-sonnet-4'],
    })
  })

  it('keeps unrestricted creation compatible with callers that omit the whitelist', async () => {
    await create('Unrestricted key', 42)

    expect(post).toHaveBeenCalledWith('/keys', { name: 'Unrestricted key', group_id: 42 })
  })

  it('distinguishes clearing key restrictions from leaving them unchanged on update', async () => {
    await update(1, { allowed_models: [] })
    await update(1, { name: 'Renamed key' })

    expect(put).toHaveBeenNthCalledWith(1, '/keys/1', { allowed_models: [] })
    expect(put).toHaveBeenNthCalledWith(2, '/keys/1', { name: 'Renamed key' })
  })
})
