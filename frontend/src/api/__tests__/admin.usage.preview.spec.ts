import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
  },
}))

import { previewCapture } from '@/api/admin/usage'

describe('admin usage api previewCapture', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('requests the capture preview as a blob via apiClient and returns the blob', async () => {
    const blob = new Blob(['<html></html>'], { type: 'text/html' })
    get.mockResolvedValue({ data: blob })

    const result = await previewCapture('req-123', 7)

    expect(get).toHaveBeenCalledWith('/admin/usage/captures/preview', {
      params: { request_id: 'req-123', api_key_id: 7 },
      responseType: 'blob',
    })
    expect(result).toBe(blob)
  })

  it('omits api_key_id when not provided', async () => {
    const blob = new Blob(['<html></html>'], { type: 'text/html' })
    get.mockResolvedValue({ data: blob })

    await previewCapture('req-456')

    expect(get).toHaveBeenCalledWith('/admin/usage/captures/preview', {
      params: { request_id: 'req-456', api_key_id: undefined },
      responseType: 'blob',
    })
  })
})
