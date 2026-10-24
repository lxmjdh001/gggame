/** Support browsers without static AbortSignal helpers or abort reasons. */
export async function withRequestTimeout<T>(request: (signal: AbortSignal) => Promise<T>, timeoutMs: number, parent?: AbortSignal): Promise<T> {
  const controller = new AbortController()
  let timedOut = false
  const timer = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, timeoutMs)
  const cancel = () => {
    clearTimeout(timer)
    controller.abort()
  }
  if (parent?.aborted) cancel()
  else parent?.addEventListener('abort', cancel, { once: true })

  try {
    return await request(controller.signal)
  } catch (error) {
    if (timedOut) {
      const timeout = new Error('请求超时，请稍后重试')
      timeout.name = 'TimeoutError'
      throw timeout
    }
    throw error
  } finally {
    clearTimeout(timer)
    parent?.removeEventListener('abort', cancel)
  }
}
