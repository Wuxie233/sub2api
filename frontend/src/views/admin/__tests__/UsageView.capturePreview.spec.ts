import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

import UsageView from '../UsageView.vue'

const { list, getStats, getSnapshotV2, getById, getModelStats, listErrorLogs, previewLink } = vi.hoisted(() => {
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
    previewLink: vi.fn(),
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
  adminUsageAPI: { list, previewLink },
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
  let originalCreateObjectURL: typeof URL.createObjectURL | undefined
  let originalRevokeObjectURL: typeof URL.revokeObjectURL | undefined

  beforeEach(() => {
    vi.useFakeTimers()
    originalCreateObjectURL = URL.createObjectURL
    originalRevokeObjectURL = URL.revokeObjectURL
    list.mockReset(); getStats.mockReset(); getSnapshotV2.mockReset()
    getModelStats.mockReset(); listErrorLogs.mockReset(); previewLink.mockReset()
    showWarning.mockReset(); showError.mockReset()

    list.mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockResolvedValue({ models: [] })
    listErrorLogs.mockResolvedValue({ items: [], total: 0, pages: 0 })

  })

  afterEach(() => {
    vi.useRealTimers()
    URL.createObjectURL = originalCreateObjectURL as typeof URL.createObjectURL
    URL.revokeObjectURL = originalRevokeObjectURL as typeof URL.revokeObjectURL
    document.body.innerHTML = ''
  })

  it('opens an in-app iframe modal with the signed preview URL and no blob URL', async () => {
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
    const createObjectURLSpy = vi.fn()
    const revokeObjectURLSpy = vi.fn()
    URL.createObjectURL = createObjectURLSpy as typeof URL.createObjectURL
    URL.revokeObjectURL = revokeObjectURLSpy as typeof URL.revokeObjectURL
    previewLink.mockResolvedValue({ url: '/usage-capture-view?request_id=req-cap-1&token=signed' })

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    await wrapper.find('[data-test="usage-table"] .preview-btn').trigger('click')
    await flushPromises()
    await nextTick()

    expect(previewLink).toHaveBeenCalledWith('req-cap-1', 5)
    expect(openSpy).not.toHaveBeenCalled()
    expect(createObjectURLSpy).not.toHaveBeenCalled()

    const iframe = document.body.querySelector('iframe')
    expect(iframe).not.toBeNull()
    expect(iframe?.getAttribute('src')).toBe('/usage-capture-view?request_id=req-cap-1&token=signed')
    expect((wrapper.vm as any).previewOpen).toBe(true)

    // Close via the component handler and assert cleanup.
    ;(wrapper.vm as any).closeCapturePreview()
    await flushPromises()
    await nextTick()

    expect(revokeObjectURLSpy).not.toHaveBeenCalled()
    expect((wrapper.vm as any).previewOpen).toBe(false)
    expect((wrapper.vm as any).previewUrl).toBe('')

    openSpy.mockRestore()
    wrapper.unmount()
  })

  it('shows the unavailable warning on a 404 and does not open the modal', async () => {
    previewLink.mockRejectedValue({ status: 404 })

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
