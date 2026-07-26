import { expect, type Page } from '@playwright/test'

export async function selectStaticSelectorOption(page: Page, label: string, option: string): Promise<void>
export async function selectStaticSelectorOption(page: Page, label: string, currentOption: string, option: string): Promise<void>
export async function selectStaticSelectorOption(page: Page, label: string, currentOption: string, nextOption?: string) {
  const option = nextOption ?? currentOption
  const trigger = page.getByRole('combobox', { name: label, exact: true })
    .or(page.locator('[role="combobox"]').filter({ hasText: currentOption }))
    .first()

  await expect(trigger, `Static selector ${label} did not expose a combobox trigger.`).toHaveCount(1)
  await trigger.evaluate((element) => (element as HTMLButtonElement).click())
  await expect(trigger).toHaveAttribute('aria-expanded', 'true')

  const listboxID = await trigger.getAttribute('aria-controls')
  if (!listboxID) throw new Error(`Static selector ${label} did not expose its listbox relationship.`)

  const listbox = page.locator(`[id=${JSON.stringify(listboxID)}]`)
  await listbox.getByRole('option', { name: option, exact: true }).evaluate((element) => (element as HTMLElement).click())
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')
}

export async function selectRemoteSelectorOption(page: Page, label: string, option: string) {
  const input = page.getByRole('combobox', { name: label, exact: true })
  await input.scrollIntoViewIfNeeded()
  await input.focus()
  await input.evaluate((element, value) => {
    const nativeSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
    nativeSetter?.call(element, value)
    element.dispatchEvent(new InputEvent('input', { bubbles: true, data: value, inputType: 'insertText' }))
    element.dispatchEvent(new Event('change', { bubbles: true }))
  }, option)

  const listboxID = await input.getAttribute('aria-controls')
  if (!listboxID) throw new Error(`Remote selector ${label} did not expose its listbox relationship.`)

  const result = page.locator(`[id=${JSON.stringify(listboxID)}]`).getByRole('option').filter({ hasText: option }).first()
  await expect(result).toBeVisible()
  await result.evaluate((element) => (element as HTMLElement).click())
  await expect(input).toHaveAttribute('aria-expanded', 'false')
}
