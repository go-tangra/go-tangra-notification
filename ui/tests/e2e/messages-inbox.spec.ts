import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { goto, signIn } from './helpers'

const email = process.env.E2E_OPERATOR_EMAIL ?? 'ops@example.org'
const password = process.env.E2E_OPERATOR_PASSWORD ?? ''

test.skip(!password, 'set E2E_OPERATOR_PASSWORD to run the notification e2e suite')

test('create a category and send a message to everyone; the bell badge updates live', async ({ page }) => {
  await signIn(page, email, password)
  await goto(page, '/notification/categories', 'category-new')
  await page.getByTestId('category-new').click()
  await page.getByTestId('category-dialog').locator('input[data-field=name]').fill('E2E ops')
  await page.getByTestId('category-dialog').getByRole('button', { name: 'Save' }).click()
  await expect(page.getByTestId('category-dialog')).not.toBeVisible()
  await goto(page, '/notification/messages', 'message-new')
  await page.getByTestId('message-new').click()
  await page.getByTestId('message-title').locator('input').fill('E2E maintenance')
  await page.getByTestId('message-content').locator('textarea').first().fill('Tonight at 22:00')
  await page.getByTestId('message-everyone').locator('input').check()
  await page.getByTestId('message-send').click()
  await expect(page.getByTestId('message-drawer')).not.toBeVisible()
  await expect(page.getByText('E2E maintenance')).toBeVisible()
  await expect(page.getByTestId('bell-badge')).toBeVisible({ timeout: 15_000 })
  const results = await new AxeBuilder({ page }).include('[data-test="messages-table"]').analyze()
  expect(results.violations.filter((v) => v.impact === 'critical')).toEqual([])
})
