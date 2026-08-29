import { test, expect, type Page } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { setupConnectedWS } from './helpers/mock-ws'

// Field names below mirror internal/checkpoint/checkpoint.go's RunRecord /
// RevertResult verbatim — those structs have no `json:` tags, so Go's
// default encoding/json emits the exported Go field names as-is
// (PascalCase). Mocking anything else would test an invented shape, not
// the real API.
const runningRun = {
  ThreadID: 'thread-running',
  AgentID: 'Coder',
  TaskSummary: 'Refactor the parser',
  Status: 'completed',
  PreSnapshot: 'pre-abc',
  PostSnapshot: 'post-def',
  TouchedPaths: ['a.go', 'b.go'],
  Pushed: false,
  PRURL: '',
  CreatedAt: '2026-08-20T10:00:00Z',
  CompletedAt: '2026-08-20T10:05:00Z',
  CaptureError: '',
  IgnoredAtBegin: [],
  IgnoredTouched: [],
}

const captureFailedRun = {
  ...runningRun,
  ThreadID: 'thread-capture-failed',
  Status: 'capture_failed',
  CaptureError: 'shadow git snapshot failed: disk full',
  TouchedPaths: [],
}

async function gotoSection(page: Page, title: string, pathPattern: RegExp) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click(`button[title="${title}"]`)
  await expect(page).toHaveURL(pathPattern, { timeout: 5000 })
}

test.describe('Run Checkpoints', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await setupConnectedWS(page)
  })

  test('panel renders runs from a mocked API with status chips', async ({ page }) => {
    await page.route(/\/api\/v1\/checkpoints\/(\?.*)?$/, route => {
      if (route.request().method() !== 'GET') return route.continue()
      return route.fulfill({ json: [runningRun, captureFailedRun] })
    })

    await gotoSection(page, 'Settings', /#\/settings$/)
    await page.getByRole('button', { name: 'Checkpoints', exact: true }).click()

    const rows = page.locator('[data-testid="checkpoint-run-row"]')
    await expect(rows).toHaveCount(2)
    await expect(page.locator('[data-testid="checkpoint-chip-protected"]')).toBeVisible()
    await expect(page.locator('[data-testid="checkpoint-chip-capture-failed"]')).toContainText('not protected')
    await expect(page.getByText('2 files touched')).toBeVisible()
  })

  test('revert flow requires confirmation and shows the full honesty result', async ({ page }) => {
    await page.route(/\/api\/v1\/checkpoints\/(\?.*)?$/, route => {
      if (route.request().method() !== 'GET') return route.continue()
      return route.fulfill({ json: [runningRun] })
    })
    await page.route(/\/api\/v1\/checkpoints\/thread-running\/revert$/, route => {
      if (route.request().method() !== 'POST') return route.continue()
      return route.fulfill({
        json: {
          Restored: ['a.go'],
          Deleted: [],
          SkippedEdited: ['b.go'],
          NotRestorable: [],
          Failed: {},
          Warning: '1 file(s) were hand-edited after this run and were left alone (pass All to override).',
          NothingCaptured: false,
        },
      })
    })

    await gotoSection(page, 'Settings', /#\/settings$/)
    await page.getByRole('button', { name: 'Checkpoints', exact: true }).click()

    await page.locator('[data-testid="checkpoint-revert-open"]').click()
    const dialog = page.locator('[data-testid="checkpoint-revert-dialog"]')
    await expect(dialog).toBeVisible()

    const confirmBtn = dialog.locator('[data-testid="checkpoint-revert-confirm-btn"]')
    await expect(confirmBtn).toBeDisabled()

    await dialog.locator('[data-testid="checkpoint-revert-confirm"]').check()
    await expect(confirmBtn).toBeEnabled()

    await confirmBtn.click()

    await expect(dialog.locator('[data-testid="checkpoint-result-restored-count"]')).toHaveText('1')
    await expect(dialog.locator('[data-testid="checkpoint-result-skipped-edited"]')).toContainText('b.go')
    await expect(dialog.locator('[data-testid="checkpoint-result-warning"]')).toContainText('hand-edited')
  })

  test('a pushed run requires the extra checkbox before revert is enabled', async ({ page }) => {
    const pushedRun = { ...runningRun, ThreadID: 'thread-pushed', Pushed: true }
    await page.route(/\/api\/v1\/checkpoints\/(\?.*)?$/, route => {
      if (route.request().method() !== 'GET') return route.continue()
      return route.fulfill({ json: [pushedRun] })
    })

    await gotoSection(page, 'Settings', /#\/settings$/)
    await page.getByRole('button', { name: 'Checkpoints', exact: true }).click()

    await page.locator('[data-testid="checkpoint-revert-open"]').click()
    const dialog = page.locator('[data-testid="checkpoint-revert-dialog"]')
    await expect(dialog.getByText('already pushed')).toBeVisible()

    const confirmBtn = dialog.locator('[data-testid="checkpoint-revert-confirm-btn"]')
    await dialog.locator('[data-testid="checkpoint-revert-confirm"]').check()
    await expect(confirmBtn).toBeDisabled()

    await dialog.locator('[data-testid="checkpoint-revert-allow-pushed"]').check()
    await expect(confirmBtn).toBeEnabled()
  })

  test('capture_failed run shows an honest "not protected" marker and disables revert', async ({ page }) => {
    await page.route(/\/api\/v1\/checkpoints\/(\?.*)?$/, route => {
      if (route.request().method() !== 'GET') return route.continue()
      return route.fulfill({ json: [captureFailedRun] })
    })

    await gotoSection(page, 'Settings', /#\/settings$/)
    await page.getByRole('button', { name: 'Checkpoints', exact: true }).click()

    const chip = page.locator('[data-testid="checkpoint-chip-capture-failed"]')
    await expect(chip).toContainText('not protected')
    await expect(page.locator('[data-testid="checkpoint-revert-open"]')).toBeDisabled()
    await expect(page.locator('[data-testid="checkpoint-view-diff"]')).toBeDisabled()
  })
})
