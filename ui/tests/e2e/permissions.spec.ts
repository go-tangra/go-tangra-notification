import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { goto, signIn } from './helpers'

const email = process.env.E2E_OPERATOR_EMAIL ?? 'ops@example.org'
const password = process.env.E2E_OPERATOR_PASSWORD ?? ''

test.skip(!password, 'set E2E_OPERATOR_PASSWORD to run the notification e2e suite')

test('grant Sharer to a role on a channel, then revoke it', async ({ page }) => {
  await signIn(page, email, password)
  await goto(page, '/notification/permissions', 'perm-channels')
  const firstChannel = page.locator('[data-test^="perm-channel-"]').first()
  await expect(firstChannel).toBeVisible()
  await firstChannel.click()
  await expect(page.getByTestId('permission-drawer')).toBeVisible()
  // Kit permission drawer: pick a role from the combobox, the level from the select, then Grant.
  await page.locator('#perm-subject').fill('Role:')
  await page.getByRole('option').first().click()
  await page.locator('#perm-level').selectOption('sharer')
  await page.getByRole('button', { name: 'Grant' }).click()
  const row = page.getByTestId('permission-drawer').locator('tbody tr, li.card').filter({ hasText: 'sharer' }).first()
  await expect(row).toBeVisible({ timeout: 15_000 })
  const results = await new AxeBuilder({ page }).include('[data-test="permission-drawer"]').analyze()
  expect(results.violations.filter((v) => v.impact === 'critical')).toEqual([])
  await row.getByRole('button', { name: 'Revoke' }).click()
  await expect(row).not.toBeVisible()
})
