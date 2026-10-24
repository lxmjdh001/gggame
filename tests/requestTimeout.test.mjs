import assert from 'node:assert/strict'
import {getEventListeners} from 'node:events'
import {readFileSync} from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import {ref, computed} from 'vue'

function legacyBrowser(fetch, scale = 1) {
  const timers = new Set()
  const durations = []
  class LegacyController {
    constructor() {
      this.native = new AbortController()
      this.signal = this.native.signal
      Object.defineProperty(this.signal, 'reason', {value: undefined})
      Object.defineProperty(this.signal, 'throwIfAborted', {value: undefined})
    }
    abort(...args) {
      assert.equal(args.length, 0, 'legacy abort must not require abort reasons')
      this.native.abort()
    }
  }
  let helper
  function load(file) {
    const context = {
      exports: {}, fetch, AbortController: LegacyController,
      // Deliberately provide no AbortSignal global or newer static APIs.
      setTimeout(callback, delay) {
        durations.push(delay)
        const timer = setTimeout(() => {timers.delete(timer); callback()}, delay * scale)
        timers.add(timer)
        return timer
      },
      clearTimeout(timer) {timers.delete(timer); clearTimeout(timer)},
      require(name) {
        if (name.includes('requestTimeout')) return helper
        if (name === 'vue') return {ref, computed}
        throw new Error('Unexpected dependency: ' + name)
      },
    }
    const source = readFileSync(new URL('../' + file, import.meta.url), 'utf8')
    const {outputText} = ts.transpileModule(source, {compilerOptions: {module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2018}})
    vm.runInNewContext(outputText, context, {filename: file})
    return context.exports
  }
  helper = load('src/lib/requestTimeout.ts')
  return {helper, load, timers, durations, LegacyController}
}

function abortError() {
  const error = new Error('Cancelled')
  error.name = 'AbortError'
  return error
}
function waitForAbort(signal) {
  return new Promise((_, reject) => {
    if (signal.aborted) reject(abortError())
    else signal.addEventListener('abort', () => reject(abortError()), {once: true})
  })
}

test('old browser loads game and account JSON without static AbortSignal APIs', async () => {
  const requests = []
  const browser = legacyBrowser(async (path, options) => {
    requests.push({path, options})
    return {ok: true, status: 200, json: async () => ({data: {loaded: true}})}
  })
  const {getJSON} = browser.load('src/api/games.ts')
  const {api} = browser.load('src/api/account.ts')
  assert.equal((await getJSON('/api/v1/games')).loaded, true)
  assert.equal((await api('/site')).loaded, true)
  assert.deepEqual(browser.durations, [8000, 12000])
  assert.equal(requests[0].options.signal.aborted, false)
  assert.equal(requests[1].options.credentials, 'same-origin')
  assert.equal(browser.timers.size, 0)
})

test('timeout aborts the request and reports a readable timeout error', async () => {
  const browser = legacyBrowser((_, {signal}) => waitForAbort(signal), 0.001)
  const {getJSON} = browser.load('src/api/games.ts')
  await assert.rejects(getJSON('/api/v1/games'), e => e.name === 'TimeoutError' && e.message === '请求超时，请稍后重试')
  assert.equal(browser.timers.size, 0)
})

test('account timeout covers a response body that stalls after successful headers', async () => {
  const browser = legacyBrowser(async (_, {signal}) => ({ok: true, status: 200, json: () => waitForAbort(signal)}), 0.001)
  const {api} = browser.load('src/api/account.ts')
  await assert.rejects(api('/site'), e => e.name === 'TimeoutError')
  assert.equal(browser.timers.size, 0)
})

test('unmount cancellation stays an AbortError and removes its parent listener', async () => {
  const browser = legacyBrowser((_, {signal}) => waitForAbort(signal))
  const parent = new browser.LegacyController()
  const {getJSON} = browser.load('src/api/games.ts')
  const pending = getJSON('/api/v1/games', parent.signal)
  assert.equal(getEventListeners(parent.signal, 'abort').length, 1)
  parent.abort()
  await assert.rejects(pending, e => e.name === 'AbortError')
  assert.equal(getEventListeners(parent.signal, 'abort').length, 0)
  assert.equal(browser.timers.size, 0)
})

test('an already-cancelled parent reaches fetch as an aborted signal', async () => {
  const browser = legacyBrowser((_, {signal}) => {
    assert.equal(signal.aborted, true)
    return waitForAbort(signal)
  })
  const parent = new browser.LegacyController()
  parent.abort()
  await assert.rejects(browser.load('src/api/games.ts').getJSON('/api/v1/games', parent.signal), e => e.name === 'AbortError')
  assert.equal(browser.timers.size, 0)
})

test('completed requests release timers and listeners before a later parent abort', async () => {
  const browser = legacyBrowser()
  const parent = new browser.LegacyController()
  let completedSignal
  assert.equal(await browser.helper.withRequestTimeout(async signal => {completedSignal = signal; return 42}, 1000, parent.signal), 42)
  assert.equal(browser.timers.size, 0)
  assert.equal(getEventListeners(parent.signal, 'abort').length, 0)
  parent.abort()
  assert.equal(completedSignal.aborted, false)
})

test('business errors retain their status and message', async () => {
  const browser = legacyBrowser(async () => ({ok: false, status: 409, json: async () => ({error: {message: '请求编号已用于其他操作'}})}))
  const {api, APIError} = browser.load('src/api/account.ts')
  await assert.rejects(api('/admin/users/example/balance', 'POST', {kind: 'reward'}), e => e instanceof APIError && e.status === 409 && e.message === '请求编号已用于其他操作')
  assert.equal(browser.timers.size, 0)
})

test('synchronous request errors and malformed JSON also clean up timers', async () => {
  const browser = legacyBrowser(async () => ({ok: true, status: 200, json: async () => {throw new SyntaxError('bad JSON')}}))
  const parent = new browser.LegacyController()
  const original = new Error('synchronous failure')
  await assert.rejects(browser.helper.withRequestTimeout(() => {throw original}, 1000, parent.signal), e => e === original)
  assert.equal(getEventListeners(parent.signal, 'abort').length, 0)
  await assert.rejects(browser.load('src/api/games.ts').getJSON('/api/v1/games'), e => e.message === '服务暂不可用，请稍后重试')
  assert.equal(browser.timers.size, 0)
})
