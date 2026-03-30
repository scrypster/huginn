/**
 * useCredentialCatalog
 *
 * Singleton fetcher for the server-side credential provider catalog exposed at
 * GET /api/v1/connections/catalog.  The catalog is fetched once per page load
 * (single in-flight promise de-dup), then cached in module scope for the
 * lifetime of the application.  All callers share the same resolved data.
 *
 * Usage:
 *   const entry = await getCredentialCatalogEntry('datadog')
 *   if (entry) { /* render GenericCredentialForm with entry.fields }
 */

import { api } from './useApi'

// ── Types ────────────────────────────────────────────────────────────────────

export interface FieldOption {
  label: string
  value: string
}

export interface FieldDefinition {
  key: string
  label: string
  /** 'text' | 'password' | 'url' | 'email' | 'select' | 'subdomain' */
  type: string
  required: boolean
  /** 'creds' | 'metadata' — informational only on the frontend */
  stored_in: string
  placeholder?: string
  help_text?: string
  default?: string
  options?: FieldOption[]
}

export interface ValidationConfig {
  available: boolean
  description?: string
}

export interface CredentialCatalogEntry {
  id: string
  name: string
  description: string
  category: string
  icon: string
  icon_color: string
  /** "credentials" | "oauth" | "system" | "database" | "coming_soon" */
  type: string
  default_label: string
  multi_account: boolean
  fields: FieldDefinition[]
  validation: ValidationConfig
}

// ── Module-level cache ────────────────────────────────────────────────────────

let cachedEntries: CredentialCatalogEntry[] | null = null
let fetchPromise: Promise<CredentialCatalogEntry[]> | null = null

// ── Public API ────────────────────────────────────────────────────────────────

/**
 * Fetch (or return the cached copy of) the full credential catalog.
 *
 * The first call initiates a network request.  All subsequent calls — whether
 * before or after the first resolves — return the same promise / cached result.
 * Throws on HTTP error so callers can show an appropriate error state.
 */
export async function fetchCredentialCatalog(): Promise<CredentialCatalogEntry[]> {
  if (cachedEntries !== null) return cachedEntries

  // De-dup concurrent calls: reuse the in-flight promise.
  if (fetchPromise !== null) return fetchPromise

  fetchPromise = api.connections
    .catalog()
    .then((raw) => {
      const entries = raw as unknown as CredentialCatalogEntry[]
      cachedEntries = entries
      fetchPromise = null
      return entries
    })
    .catch((err) => {
      // Clear the in-flight promise on error so callers can retry.
      fetchPromise = null
      throw err
    })

  return fetchPromise
}

/**
 * Return the catalog entry for a given provider ID, or null if not found.
 *
 * This is the primary entry point for `CredentialModal` to decide how to
 * render the form and which API path to use for save/test.
 */
export async function getCredentialCatalogEntry(
  id: string,
): Promise<CredentialCatalogEntry | null> {
  const entries = await fetchCredentialCatalog()
  return entries.find((e) => e.id === id) ?? null
}

/**
 * Reset the module-level cache.  Only for use in tests.
 */
export function _resetCredentialCatalogCache(): void {
  cachedEntries = null
  fetchPromise = null
}
