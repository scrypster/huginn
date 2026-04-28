// Global test setup for jsdom environment.
// Stubs browser APIs that jsdom does not implement so Vue components that
// use them (e.g. IntersectionObserver for infinite scroll) don't throw
// unhandled errors during tests.
import { config } from '@vue/test-utils'

class MockIntersectionObserver {
  observe = () => {}
  unobserve = () => {}
  disconnect = () => {}
}

Object.defineProperty(globalThis, 'IntersectionObserver', {
  writable: true,
  configurable: true,
  value: MockIntersectionObserver,
})

// Shared no-op injections used by views/composables under test.
const noop = () => {}
const wsStub = {
  on: noop,
  off: noop,
  send: noop,
  close: noop,
  addEventListener: noop,
  removeEventListener: noop,
  readyState: 1,
}

config.global.provide = {
  ...(config.global.provide || {}),
  ws: wsStub,
  markSpaceSeen: noop,
}
