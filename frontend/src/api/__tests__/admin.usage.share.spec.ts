import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { createCaptureShare, listCaptureShares, revokeCaptureShare } from '@/api/admin/usage'

describe('admin usage api capture shares', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('creates a share with full payload and returns the response', async () => {
    const response = {
      share_id: 'AbC123',
      path: '/s/AbC123',
      expires_at: '2026-07-30T00:00:00Z',
      created_at: '2026-06-30T00:00:00Z',
    }
    post.mockResolvedValue({ data: response })

    const result = await createCaptureShare({
      request_id: 'req-1',
      api_key_id: 9,
      expires_in_days: 7,
      label: 'shared with bob',
    })

    expect(post).toHaveBeenCalledWith('/admin/usage/captures/shares', {
      request_id: 'req-1',
      api_key_id: 9,
      expires_in_days: 7,
      label: 'shared with bob',
    })
    expect(result).toBe(response)
  })

  it('creates a permanent share by omitting expires_in_days', async () => {
    post.mockResolvedValue({ data: { share_id: 'x', path: '/s/x', expires_at: null, created_at: 'now' } })

    await createCaptureShare({ request_id: 'req-2' })

    expect(post).toHaveBeenCalledWith('/admin/usage/captures/shares', { request_id: 'req-2' })
  })

  it('lists shares with pagination params and signal', async () => {
    const page = { items: [], total: 0, page: 1, page_size: 20, pages: 0 }
    get.mockResolvedValue({ data: page })
    const controller = new AbortController()

    const result = await listCaptureShares({ request_id: 'req-3', page: 2, page_size: 20 }, { signal: controller.signal })

    expect(get).toHaveBeenCalledWith('/admin/usage/captures/shares', {
      params: { request_id: 'req-3', page: 2, page_size: 20 },
      signal: controller.signal,
    })
    expect(result).toBe(page)
  })

  it('revokes a share by id', async () => {
    post.mockResolvedValue({ data: { ok: true } })

    await revokeCaptureShare(42)

    expect(post).toHaveBeenCalledWith('/admin/usage/captures/shares/42/revoke')
  })
})
