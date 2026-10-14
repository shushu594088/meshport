import { test, expect } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
const password = process.env.MESHPORT_TEST_PASSWORD || 'meshport-test-password-2026'
test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await page.getByLabel('管理员密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '概览', exact: true })).toBeVisible()
})

test('one-click delay includes nodes outside filters, individual retry and cancellation', async ({ page }) => {
  await page.route('**/api/state', async route => {
    const response = await route.fetch()
    const state = await response.json()
    state.service.running = true
    await route.fulfill({ json: state })
  })
  const names = Array.from({ length: 25 }, (_, i) => '节点' + (i + 1))
  await page.route('**/api/nodes', route => route.fulfill({ json: { proxies: {
    PROXY: { name: 'PROXY', type: 'Selector', now: names[0], all: names },
    ...Object.fromEntries(names.map(name => [name, { name, type: 'Shadowsocks' }])),
  } } }))
  let calls: string[] = [], failOne = true, slow = false
  await page.route('**/api/tests/delay', async route => {
    const name = route.request().postDataJSON().name
    calls.push(name)
    if (slow) await new Promise(resolve => setTimeout(resolve, 1200))
    await route.fulfill({ json: { status: name === '节点2' && failOne ? 'failed' : 'supported', delay: 0, detail: name === '节点2' && failOne ? '连接超时' : '节点可用', checked_at: new Date().toISOString(), revision: 1 } })
  })
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await page.getByRole('link', { name: '节点', exact: true }).click()
  await page.getByRole('searchbox').fill('节点2')
  await page.getByRole('button', { name: '一键测速', exact: true }).click()
  await expect(page.getByText('已完成 25 项测试', { exact: true })).toBeVisible()
  expect(new Set(calls).size).toBe(25)
  const card = page.locator('.node-card').filter({ has: page.getByText('节点2', { exact: true }) })
  await expect(card).toContainText('测试失败')
  failOne = false
  await card.getByRole('button', { name: '测速 节点2', exact: true }).click()
  await expect(card).toContainText('0 ms')
  await expect(page.locator('.group-result')).toContainText('节点1')
  slow = true; calls = []
  await page.getByRole('button', { name: '一键测速', exact: true }).click()
  await page.getByRole('button', { name: '取消测速', exact: true }).click()
  await expect(page.getByText('测试已取消，已完成的结果保留', { exact: true })).toBeVisible()
  expect(calls.length).toBeLessThan(25)
})

test('site statuses, per-site retest, loading, mobile and accessibility', async ({ page }) => {
  await page.route('**/api/state', async route => {
    const response = await route.fetch(); const state = await response.json()
    state.service.running = true; await route.fulfill({ json: state })
  })
  let calls = 0
  await page.route('**/api/tests/site', async route => {
    calls++
    const id = route.request().postDataJSON().id
    await new Promise(resolve => setTimeout(resolve, 70))
    await route.fulfill({ json: { status: id === 'bilibili-tw' ? 'unsupported' : id === 'bahamut' ? 'failed' : id === 'chatgpt' ? 'supported' : 'reachable', region: id === 'chatgpt' ? 'JP' : undefined, delay: 123, detail: id === 'bahamut' ? '连接超时，请重试' : '检查完成', checked_at: new Date().toISOString(), revision: 1 } })
  })
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await page.getByRole('link', { name: '测试', exact: true }).click()
  await expect(page.locator('.site-card')).toHaveCount(12)
  await page.getByRole('button', { name: '测试全部', exact: true }).click()
  await expect(page.getByRole('button', { name: '取消测试', exact: true })).toBeVisible()
  await expect(page.getByText('已完成 12 项测试', { exact: true })).toBeVisible()
  expect(calls).toBe(12)
  const chat = page.locator('.site-card').filter({ has: page.getByRole('heading', { name: 'ChatGPT Web', exact: true }) })
  await expect(chat).toContainText('JP')
  await expect(chat.locator('.test-badge')).toHaveText('支持')
  await page.getByRole('button', { name: '测试 ChatGPT Web', exact: true }).click()
  await expect(page.getByText('已完成 1 项测试', { exact: true })).toBeVisible()
  expect(calls).toBe(13)
  await page.screenshot({ path: '../build/ui-tests-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']).analyze()).violations).toEqual([])
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.screenshot({ path: '../build/ui-tests-mobile.png', fullPage: true })
})

test('website offline state and transport failure can recover', async ({ page }) => {
  await page.getByRole('link', { name: '测试', exact: true }).click()
  await expect(page.getByRole('heading', { name: '内核已停止', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '测试全部', exact: true })).toBeDisabled()
  await page.route('**/api/state', async route => { const response = await route.fetch(); const state = await response.json(); state.service.running = true; await route.fulfill({ json: state }) })
  await page.getByRole('button', { name: '刷新状态', exact: true }).click()
  await page.route('**/api/tests/site', route => route.abort())
  await page.getByRole('button', { name: '测试 ChatGPT Web', exact: true }).click()
  const card = page.locator('.site-card').filter({ hasText: 'ChatGPT Web' })
  await expect(card).toContainText('无法连接后台')
  await expect(page.getByRole('button', { name: '测试 ChatGPT Web', exact: true })).toBeEnabled()
})

test('version check failure, update confirmation, restart reconnection and completion', async ({ page }) => {
  let started = false, polls = 0, checkFails = true
  await page.route('**/api/update/status', route => {
    polls++
    if (started && polls === 1) return route.abort()
    return route.fulfill({ json: { current: started ? '0.1.2' : '0.1.1', status: started ? { phase: 'done', message: '更新完成，配置与订阅已保留', version: '0.1.2', updated_at: new Date().toISOString() } : { phase: 'idle', message: '尚未开始更新' } } })
  })
  await page.route('**/api/update', route => {
    if (route.request().method() === 'POST') { started = true; polls = 0; return route.fulfill({ status: 202, json: { ok: true } }) }
    if (checkFails) return route.fulfill({ status: 503, json: { error: 'GitHub 请求失败，请稍后重试' } })
    return route.fulfill({ json: { current: '0.1.1', latest: '0.1.2', available: true, can_update: true, url: 'https://github.com/shushu594088/meshport/releases/tag/v0.1.2', notes: '', published_at: new Date().toISOString(), checked_at: new Date().toISOString() } })
  })
  await page.getByRole('link', { name: '设置', exact: true }).click()
  await page.getByRole('button', { name: '检查最新版本', exact: true }).click()
  await expect(page.locator('.version-panel').getByRole('alert')).toContainText('GitHub 请求失败')
  checkFails = false
  await page.getByRole('button', { name: '检查最新版本', exact: true }).click()
  await expect(page.locator('.version-panel')).toContainText('v0.1.2')
  await page.getByLabel('Web 端口', { exact: true }).fill('18889')
  await expect(page.getByRole('button', { name: '一键更新', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '放弃修改', exact: true }).click()
  await page.getByRole('button', { name: '一键更新', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '更新 Meshport？' }).getByRole('button', { name: '取消', exact: true })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: '一键更新', exact: true })).toBeFocused()
  await page.getByRole('button', { name: '一键更新', exact: true }).click()
  await page.getByRole('button', { name: '更新并重启后台', exact: true }).click()
  await expect(page.locator('.version-panel')).toContainText('正在等待重新连接')
  await expect(page.locator('.version-panel')).toContainText('更新完成，配置与订阅已保留')
  await expect(page.getByRole('button', { name: '检查最新版本', exact: true })).toBeEnabled()
  await page.screenshot({ path: '../build/ui-version.png', fullPage: true })
})
