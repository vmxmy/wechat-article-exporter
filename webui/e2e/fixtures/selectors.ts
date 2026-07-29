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

  // Base UI sets aria-controls asynchronously and may recreate the listbox portal when
  // the filtered result set loads. Re-resolve the option from the live aria-controls on
  // each poll so a stale/loading listbox never wins, and wait out the loading state.
  let optionElement: import('@playwright/test').Locator | undefined
  await expect.poll(async () => {
    const listboxID = await input.getAttribute('aria-controls')
    if (!listboxID) return false
    const listbox = page.locator(`[id=${JSON.stringify(listboxID)}]`)
    // The option is reachable only once the listbox has settled (not still loading/empty).
    const match = listbox.getByRole('option').filter({ hasText: option }).first()
    const visible = await match.isVisible().catch(() => false)
    optionElement = visible ? match : undefined
    return visible
  }, { message: `Remote selector ${label} never exposed a visible "${option}" option.`, timeout: 10_000 }).toBeTruthy()

  await optionElement!.evaluate((element) => (element as HTMLElement).click())
  await expect(input).toHaveAttribute('aria-expanded', 'false')
}
