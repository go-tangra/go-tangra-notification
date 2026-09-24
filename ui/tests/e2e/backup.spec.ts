import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { base, signIn } from './helpers'

const email = process.env.E2E_OPERATOR_EMAIL ?? 'ops@example.org'
const password = process.env.E2E_OPERATOR_PASSWORD ?? ''

test.skip(!password, 'set E2E_OPERATOR_PASSWORD to run the notification e2e suite')

test('export a backup (no credentials by default) and re-import it', async ({ page }) => {
  await signIn(page, email, password)
  const cookies = await page.context().cookies(base)
  const csrf = cookies.find((c) => c.name === '__Host-csrf')?.value ?? ''
  const res = await page.request.post(base + '/api/notification/v1/backup/export', { headers: { 'X-CSRF-Token': csrf, Origin: base } })
  expect(res.status()).toBe(200)
  const body = await res.text()
  expect(body).not.toContain('NOTIF-MARKER-PW')
  const doc = JSON.parse(body) as { version: number; channels: unknown[] }
  expect(doc.version).toBe(1)
  const rep = await page.request.post(base + '/api/notification/v1/backup/import?mode=skip', {
    headers: { 'X-CSRF-Token': csrf, Origin: base, 'Content-Type': 'application/json' },
    data: doc,
  })
  expect(rep.status()).toBe(200)
  await page.goto(base + '/notification/log')
  const results = await new AxeBuilder({ page }).analyze()
  expect(results.violations.filter((v) => v.impact === 'critical')).toEqual([])
})
