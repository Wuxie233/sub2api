import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import UsageView from '../UsageView.vue'

const { list, getStats, getSnapshotV2, getById, getModelStats, listErrorLogs, previewCapture } = vi.hoisted(() => {
  vi.stubGlobal('localStorage', {
    getItem: vi.fn(() => null),
    setItem: vi.fn(),
    removeItem: vi.fn(),
  })
  return {
    list: vi.fn(),
    getStats: vi.fn(),
    getSnapshotV2: vi.fn(),
    getById: vi.fn(),
    getModelStats: vi.fn(),
    listErrorLogs: vi.fn(),
    previewCapture: vi.fn(),
  }
})

const messages: Record<string, string> = {
  'usage.previewTitle': 'Conversation Preview',
  'usage.previewUnavailable': 'Capture record is unavailable or has expired',
  'usage.previewFailed': 'Failed to open conversation preview',
}

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: { list, getStats },
    dashboard: { getSnapshotV2, getModelStats },
    users: { getById },
  },
}))

vi.mock('@/api/admin/usage', () => ({
  adminUsageAPI: { list, previewCapture },
}))

vi.mock('@/api/admin/ops', () => ({ listErrorLogs }))

const showWarning = vi.fn()
const showError = vi.fn()
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess: vi.fn(), showInfo: vi.fn() }),
}))

vi.mock('@/utils/format', () => ({
  formatReasoningEffort: (value: string | null | undefined) => value ?? '-',
  formatDateTime: (v: unknown) => String(v ?? ''),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => messages[key] ?? key }) }
})

vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))

const AppLayoutStub = { template: '<div><slot /></div>' }
const UsageFiltersStub = { template: '<div><slot name="after-reset" /></div>' }
// Stub table that emits a preview for a concrete row when its button is clicked.
const UsageTableStub = {
  emits: ['preview'],
  template:
    '<div data-test="usage-table"><button class="preview-btn" @click="$emit(\'preview\', { request_id: \'req-cap-1\', model: \'claude-3-haiku\', api_key_id: 5 })">preview</button></div>',
}

// Keep the real URL constructor; only attach the two blob helpers as spies.
const createObjectURL = vi.fn(() => 'blob:mock-capture-url')
const revokeObjectURL = vi.fn()

function mountView() {
  return mount(UsageView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        UsageStatsCards: true,
        UsageFilters: UsageFiltersStub,
        UsageTable: UsageTableStub,
        UsageExportProgress: true,
        UsageCleanupDialog: true,
        UserBalanceHistoryModal: true,
        Pagination: true,
        Select: true,
        DateRangePicker: true,
        Icon: true,
        TokenUsageTrend: true,
        ModelDistributionChart: true,
        GroupDistributionChart: true,
        EndpointDistributionChart: true,
        OpsErrorLogTable: true,
        OpsErrorDetailModal: true,
        // BaseDialog intentionally NOT stubbed so the iframe modal really renders.
      },
    },
  })
}

describe('admin UsageView capture preview modal', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset(); getStats.mockReset(); getSnapshotV2.mockReset()
    getModelStats.mockReset(); listErrorLogs.mockReset(); previewCapture.mockReset()
    showWarning.mockReset(); showError.mockReset()
    createObjectURL.mockClear(); revokeObjectURL.mockClear()

    list.mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockResolvedValue({ models: [] })
    listErrorLogs.mockResolvedValue({ items: [], total: 0, pages: 0 })

    ;(globalThis.URL as any).createObjectURL = createObjectURL
    ;(globalThis.URL as any).revokeObjectURL = revokeObjectURL
  })

  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('opens an in-app iframe modal with the blob URL (no window.open) and revokes on close', async () => {
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    previewCapture.mockResolvedValue(new Blob(['<html></html>'], { type: 'text/html' }))

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    await wrapper.find('[data-test="usage-table"] .preview-btn').trigger('click')
    await flushPromises()
    await nextTick()

    expect(previewCapture).toHaveBeenCalledWith('req-cap-1', 5)
    expect(openSpy).not.toHaveBeenCalled()
    expect(createObjectURL).toHaveBeenCalledTimes(1)

    // BaseDialog teleports to body; assert the iframe is present with the blob URL.
    const iframe = document.body.querySelector('iframe')
    expect(iframe).not.toBeNull()
    expect(iframe?.getAttribute('src')).toBe('blob:mock-capture-url')
    expect((wrapper.vm as any).previewOpen).toBe(true)

    // Close via the component handler and assert cleanup.
    ;(wrapper.vm as any).closeCapturePreview()
    await flushPromises()
    await nextTick()

    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-capture-url')
    expect((wrapper.vm as any).previewOpen).toBe(false)
    expect((wrapper.vm as any).previewUrl).toBe('')

    openSpy.mockRestore()
    wrapper.unmount()
  })

  it('shows the unavailable warning on a 404 and does not open the modal', async () => {
    previewCapture.mockRejectedValue({ status: 404 })

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    await wrapper.find('[data-test="usage-table"] .preview-btn').trigger('click')
    await flushPromises()
    await nextTick()

    expect(showWarning).toHaveBeenCalledWith('Capture record is unavailable or has expired')
    expect((wrapper.vm as any).previewOpen).toBe(false)
    expect(document.body.querySelector('iframe')).toBeNull()

    wrapper.unmount()
  })
})
