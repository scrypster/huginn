// Global test setup for jsdom environment.
// Stubs browser APIs that jsdom does not implement so Vue components that
// use them (e.g. IntersectionObserver for infinite scroll) don't throw
// unhandled errors during tests.

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
