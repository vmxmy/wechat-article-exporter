import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ApiError,
  deleteSavedQuery,
  getAccountPage,
  getDiagnosticBundleDownloadURL,
  getExportArtifactDownloadURL,
  getRuntimeStatus,
  logout
} from '../src/lib/api'

const fetchMock = vi.fn()

afterEach(() => {
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

describe('browser API client', () => {
  it('unwraps the versioned response envelope and sends same-origin credentials', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      apiVersion: 'v1',
      data: { version: 'test-build', session: 'authenticated' }
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getRuntimeStatus()).resolves.toEqual({ version: 'test-build', session: 'authenticated' })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/runtime', expect.objectContaining({
      credentials: 'same-origin',
      headers: { Accept: 'application/json' }
    }))
  })

  it('normalizes bounded workspace pages and preserves encoded query parameters', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      items: [{ id: 'account-1', name: 'Fixture' }],
      total: 31,
      offset: 10,
      limit: 10
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getAccountPage({ page: 2, pageSize: 10, search: 'a & b', sort: 'name', direction: 'asc' })).resolves.toEqual({
      data: [{ id: 'account-1', name: 'Fixture' }],
      pagination: { page: 2, pageSize: 10, total: 31 }
    })
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/accounts?offset=10&limit=10&keyword=a+%26+b&sort=name%3Aasc',
      expect.any(Object)
    )
  })

  it('adds the CSRF proof and scoped confirmation for saved-query deletion', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(jsonResponse({}))
    vi.stubGlobal('fetch', fetchMock)

    await expect(deleteSavedQuery('draft / query')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/saved-queries', {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'X-CSRF-Token': 'csrf-fixture'
      },
      body: JSON.stringify({ name: 'draft / query', confirm: 'delete-saved-query:draft / query' })
    })
  })

  it('revokes the local session with the CSRF proof and accepts an empty response', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ apiVersion: 'v1', data: { csrfToken: 'csrf-fixture' } }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(logout()).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/session/logout', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'X-CSRF-Token': 'csrf-fixture'
      },
      body: '{}'
    })
  })

  it('surfaces safe server failures and safely encodes opaque download identifiers', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: { message: 'session expired' } }, 401))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getRuntimeStatus()).rejects.toEqual(expect.objectContaining<ApiError>({
      name: 'ApiError', status: 401, message: 'session expired'
    }))
    expect(getExportArtifactDownloadURL('export / one', 'artifact?two')).toBe('/api/v1/exports/export%20%2F%20one/artifact?artifactId=artifact%3Ftwo')
    expect(getDiagnosticBundleDownloadURL('bundle / one')).toBe('/api/v1/maintenance/diagnostic-bundles/bundle%20%2F%20one')
  })
})

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}
