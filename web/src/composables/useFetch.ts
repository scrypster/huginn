/**
 * useFetch — shared authenticated fetch utility for composables.
 *
 * Thin wrapper around `apiFetch` from useApi. Provides a single place to
 * inject auth headers, handle 401 token refresh, and parse JSON — so
 * individual composables do not need to re-implement the pattern.
 *
 * Usage:
 *   import { apiFetch } from './useFetch'
 *   const data = await apiFetch<MyType>('/api/v1/something', { method: 'POST', body: JSON.stringify(payload) })
 */
export { apiFetch } from './useApi'
