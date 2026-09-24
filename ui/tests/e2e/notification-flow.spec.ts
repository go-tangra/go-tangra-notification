import { expect, test, type Page } from '@playwright/test'
import { base, signIn } from './helpers'

// Quickstart §4 flow for the notification remote at the three reference widths:
// inbox, channels drawer validation, templates, ./header bell, backup dialog gating.
// Needs a full platform; skips without operator credentials.
const password = process.env.E2E_OPERATOR_PASSWORD ?? ''
const email = process.env.E2E_OPERATOR_EMAIL ?? 'ops@example.org'
const viewports = [{ name: 'phone', width: 320, height: 640 }, { name: 'tablet', width: 768, height: 1024 }, { name: 'desktop', width: 1280, height: 800 }]

async function openNav(page: Page, group: string, entry: string): Promise<void> {
  const burger = page.getByRole('button', { name: 'Open navigation' })
  if (await burger.isVisible()) await burger.click()
  const g = page.getByTestId('nav-group-' + group)
  if ((await g.getAttribute('aria-expanded')) !== 'true') await g.click()
  await page.getByTestId('nav-' + group).filter({ hasText: entry }).first().click()
}

test.describe('notification remote', () => {
  test.skip(!password, 'E2E_OPERATOR_PASSWORD not set')
  for (const vp of viewports) {
    test(`${vp.name}: bell, inbox, channels, templates, backup`, async ({ page }) => {
      await page.setViewportSize({ width: vp.width, height: vp.height })
      const violations: string[] = []
      await page.addInitScript(() => document.addEventListener('securitypolicyviolation', (e) => console.error('CSP:' + (e as SecurityPolicyViolationEvent).violatedDirective)))
      page.on('console', (m) => { if (m.text().startsWith('CSP:')) violations.push(m.text()) })
      await page.goto(base + '/')
      await signIn(page, email, password)
      await page.getByTestId('bell-button').click()
      await expect(page.getByTestId('bell-open-inbox')).toBeVisible()
      await page.getByTestId('bell-open-inbox').click()
      await expect(page.locator('main h1')).toHaveText('Inbox')
      await openNav(page, 'notification', 'Channels')
      await page.getByTestId('channel-new').click()
      await page.getByTestId('channel-save').click()
      await expect(page.locator('aside[role=dialog]').getByRole('alert').first()).toContainText('required')
      const pwd = page.getByTestId('channel-password').locator('input')
      await expect(pwd).toHaveAttribute('type', 'password')
      await expect(pwd).toHaveAttribute('autocomplete', 'off')
      await page.keyboard.press('Escape')
      await openNav(page, 'notification', 'Templates')
      await expect(page.getByTestId('templates-table')).toBeVisible()
      await openNav(page, 'notification', 'Log')
      if (await page.getByTestId('ops-backup').isVisible()) {
        await page.getByTestId('ops-backup').click()
        await expect(page.getByTestId('backup-dialog')).toBeVisible()
        await page.getByTestId('backup-import').click()
        await expect(page.getByTestId('backup-dialog').getByRole('alert').first()).toContainText('Choose a backup file')
        await page.keyboard.press('Escape')
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
      expect(await page.locator('main [style]').count()).toBe(0)
      expect(violations).toEqual([])
    })
  }
})
