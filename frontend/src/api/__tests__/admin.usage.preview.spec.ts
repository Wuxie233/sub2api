import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
  },
}))

import { previewLink } from '@/api/admin/usage'

describe('admin usage api previewLink', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('requests a signed capture preview URL via apiClient and returns it', async () => {
    const response = { url: '/usage-capture-view?request_id=req-123&token=signed' }
    get.mockResolvedValue({ data: response })

    const result = await previewLink('req-123', 7)

    expect(get).toHaveBeenCalledWith('/admin/usage/captures/preview-link', {
      params: { request_id: 'req-123', api_key_id: 7 },
    })
    expect(result).toBe(response)
  })

  it('omits api_key_id when not provided', async () => {
    get.mockResolvedValue({ data: { url: '/usage-capture-view?request_id=req-456&token=signed' } })

    await previewLink('req-456')

    expect(get).toHaveBeenCalledWith('/admin/usage/captures/preview-link', {
      params: { request_id: 'req-456', api_key_id: undefined },
    })
  })
})
