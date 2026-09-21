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
  await page.getByTestId('grant-subject-type').click()
  await page.getByRole('option', { name: 'role' }).click()
  await page.getByTestId('grant-role').click()
  await page.getByRole('option').first().click()
  await page.getByTestId('grant-relation').click()
  await page.getByRole('option', { name: 'sharer' }).click()
  await page.getByTestId('grant-add').click()
  const row = page.locator('[data-test^="grant-row-"]').filter({ hasText: 'sharer' }).first()
  await expect(row).toBeVisible({ timeout: 15_000 })
  const results = await new AxeBuilder({ page }).include('[data-test="permission-drawer"]').analyze()
  expect(results.violations.filter((v) => v.impact === 'critical')).toEqual([])
  await row.locator('[data-test^="grant-revoke-"]').click()
  await expect(row).not.toBeVisible()
})
