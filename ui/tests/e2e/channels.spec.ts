import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { goto, signIn } from './helpers'

const email = process.env.E2E_OPERATOR_EMAIL ?? 'ops@example.org'
const password = process.env.E2E_OPERATOR_PASSWORD ?? ''
const mail = process.env.E2E_MAIL ?? 'http://localhost:8027'

test.skip(!password, 'set E2E_OPERATOR_PASSWORD to run the notification e2e suite')

test('create an email channel, send a test message, see it in Mailpit', async ({ page }) => {
  await signIn(page, email, password)
  await goto(page, '/notification/channels', 'channel-new')
  await page.getByTestId('channel-new').click()
  await page.getByTestId('channel-name').locator('input').fill('E2E relay')
  await page.getByTestId('channel-host').locator('input').fill('mailpit')
  await page.getByTestId('channel-port').locator('input').fill('1025')
  await page.getByTestId('channel-tls').locator('select').selectOption('none')
  await page.getByTestId('channel-from').locator('input').fill('noreply@example.org')
  await page.getByTestId('channel-enabled').locator('input').check()
  await page.getByTestId('channel-save').click()
  await expect(page.getByTestId('channel-drawer')).not.toBeVisible()
  await page.getByText('E2E relay').click()
  await page.getByTestId('channel-test-recipient').locator('input').fill('e2e@example.org')
  await page.getByTestId('channel-test-send').click()
  await expect(page.getByTestId('channel-test-result')).toContainText('sent', { timeout: 15_000 })
  const search = await page.request.get(mail + '/api/v1/search?query=' + encodeURIComponent('to:e2e@example.org'))
  expect(((await search.json()) as { messages: unknown[] }).messages.length).toBeGreaterThan(0)
  const results = await new AxeBuilder({ page }).analyze()
  expect(results.violations.filter((v) => v.impact === 'critical')).toEqual([])
  await page.getByTestId('channel-delete').click().catch(() => undefined)
})

test('create a template and preview it without a log entry', async ({ page }) => {
  await signIn(page, email, password)
  await goto(page, '/notification/templates', 'template-new')
  await page.getByTestId('template-new').click()
  await page.getByTestId('template-name').locator('input').fill('E2E welcome')
  await page.getByTestId('template-subject').locator('input').fill('Hello {{.Name}}')
  await page.getByTestId('template-body').locator('textarea').first().fill('<p>Hi {{.Name}}</p>')
  await page.getByTestId('template-variables').locator('input').fill('Name')
  await page.keyboard.press('Enter')
  await page.getByTestId('preview-var-Name').locator('input').fill('<Ana>')
  await page.getByTestId('template-preview').click()
  await expect(page.getByTestId('preview-subject')).toHaveText('Hello <Ana>')
  await expect(page.getByTestId('preview-body')).toContainText('&lt;Ana&gt;')
  const results = await new AxeBuilder({ page }).analyze()
  expect(results.violations.filter((v) => v.impact === 'critical')).toEqual([])
})
