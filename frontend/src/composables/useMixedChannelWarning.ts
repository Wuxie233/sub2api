import { ref } from 'vue'

export interface MixedChannelWarningDetails {
  groupName: string
  currentPlatform: string
  otherPlatform: string
}

function isMixedChannelWarningError(error: any): boolean {
  return error?.response?.status === 409 && error?.response?.data?.error === 'mixed_channel_warning'
}

function extractMixedChannelWarningDetails(error: any): MixedChannelWarningDetails {
  const details = error?.response?.data?.details || {}
  return {
    groupName: details.group_name || 'Unknown',
    currentPlatform: details.current_platform || 'Unknown',
    otherPlatform: details.other_platform || 'Unknown'
  }
}

export function useMixedChannelWarning() {
  const show = ref(false)
  const details = ref<MixedChannelWarningDetails | null>(null)

  const pendingPayload = ref<any | null>(null)
  const pendingRequest = ref<((payload: any) => Promise<any>) | null>(null)
  const pendingOnSuccess = ref<(() => void) | null>(null)
  const pendingOnError = ref<((error: any) => void) | null>(null)

  const clearPending = () => {
    pendingPayload.value = null
    pendingRequest.value = null
    pendingOnSuccess.value = null
    pendingOnError.value = null
    details.value = null
  }

  const tryRequest = async (
    payload: any,
    request: (payload: any) => Promise<any>,
    opts?: {
      onSuccess?: () => void
      onError?: (error: any) => void
    }
  ): Promise<boolean> => {
    try {
      await request(payload)
      opts?.onSuccess?.()
      return true
    } catch (error: any) {
      if (isMixedChannelWarningError(error)) {
        details.value = extractMixedChannelWarningDetails(error)
        pendingPayload.value = payload
        pendingRequest.value = request
        pendingOnSuccess.value = opts?.onSuccess || null
        pendingOnError.value = opts?.onError || null
        show.value = true
        return false
      }

      if (opts?.onError) {
        opts.onError(error)
        return false
      }
      throw error
    }
  }

  const confirm = async (): Promise<boolean> => {
    show.value = false
    if (!pendingPayload.value || !pendingRequest.value) {
      clearPending()
      return false
    }

    pendingPayload.value.confirm_mixed_channel_risk = true

    try {
      await pendingRequest.value(pendingPayload.value)
      pendingOnSuccess.value?.()
      return true
    } catch (error: any) {
      if (pendingOnError.value) {
        pendingOnError.value(error)
        return false
      }
      throw error
    } finally {
      clearPending()
    }
  }

  const cancel = () => {
    show.value = false
    clearPending()
  }

  return {
    show,
    details,
    tryRequest,
    confirm,
    cancel
  }
}

