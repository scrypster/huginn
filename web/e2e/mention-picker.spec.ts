import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { setupInteractiveWS } from './helpers/mock-ws'

async function openSpace(page: import('@playwright/test').Page, spaceId: string) {
  await page.goto(`/#/space/${spaceId}`)
  await expect(page.locator('[data-testid="ws-status-dot"]')).toHaveClass(/bg-huginn-green/, { timeout: 5_000 })
  await page.waitForSelector('.editor-content .ProseMirror', { timeout: 5_000 })
}

async function openMentionPicker(page: import('@playwright/test').Page) {
  const editor = page.locator('.editor-content .ProseMirror')
  await editor.click()
  await page.keyboard.type('@')
  await expect(page.locator('.tippy-box')).toBeVisible({ timeout: 3_000 })
}

async function walkthroughShot(page: import('@playwright/test').Page, name: string) {
  const dir = process.env.WALKTHROUGH_ARTIFACTS
  if (!dir) return
  await page.screenshot({ path: `${dir}/${name}`, fullPage: true })
}

test.describe('Composer @ picker roster', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await setupInteractiveWS(page)
  })

  test('channel picker lists only members', async ({ page }) => {
    // #General = Coder (lead) + GitAgent. No other agents exist in the fixture.
    await openSpace(page, 'space-general')
    await openMentionPicker(page)
    const picker = page.locator('.tippy-box')
    await expect(picker).toContainText('Coder')
    await expect(picker).toContainText('GitAgent')
    await walkthroughShot(page, 'channel_picker_lists_only_members.png')
  })

  test('DM picker lists only that agent', async ({ page }) => {
    await openSpace(page, 'dm-alice')
    await openMentionPicker(page)
    const picker = page.locator('.tippy-box')
    await expect(picker).toContainText('Coder')
    await expect(picker).not.toContainText('GitAgent')
    await walkthroughShot(page, 'dm_picker_lists_only_that_agent.png')
  })

  test('a non-member is not suggested', async ({ page }) => {
    // #Engineering lead is GitAgent only — Coder is not a member.
    await openSpace(page, 'space-eng')
    await openMentionPicker(page)
    const picker = page.locator('.tippy-box')
    await expect(picker).toContainText('GitAgent')
    await expect(picker).not.toContainText('Coder')
    await walkthroughShot(page, 'channel_picker_hides_non_member.png')
  })

  test('leftover typed @Name of a non-member shows a not-in-channel hint', async ({ page }) => {
    await openSpace(page, 'space-eng')
    const editor = page.locator('.editor-content .ProseMirror')
    await editor.click()
    await page.keyboard.type('@Coder leftover')
    await page.locator('button[title="Send (⏎)"]').click()
    await expect(page.getByTestId('unknown-mention-hint')).toContainText('not in this channel')
    await walkthroughShot(page, 'leftover_non_member_mention_hint.png')
  })

  test('mid-text leftover @Name of a non-member shows the same hint', async ({ page }) => {
    await openSpace(page, 'dm-alice')
    const editor = page.locator('.editor-content .ProseMirror')
    await editor.click()
    await page.keyboard.type('please ask @GitAgent about hostname')
    await page.locator('button[title="Send (⏎)"]').click()
    await expect(page.getByTestId('unknown-mention-hint')).toContainText('not in this channel')
    await walkthroughShot(page, 'mid_text_leftover_non_member_mention_hint.png')
  })
})
