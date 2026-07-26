import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ActionGroup } from '../src/components/presentation/ActionGroup'
import { DefinitionList } from '../src/components/presentation/DefinitionList'
import { FieldHint } from '../src/components/presentation/FieldHint'
import { FormDrawer } from '../src/components/presentation/FormDrawer'
import { FormGrid, FormGridFullSpan } from '../src/components/presentation/FormGrid'
import { Panel } from '../src/components/presentation/Panel'
import { PresentationDrawer } from '../src/components/presentation/PresentationDrawer'
import { TypedConfirmationDialog } from '../src/components/presentation/TypedConfirmationDialog'

describe('layout primitives', () => {
  it('Panel renders a surface section with tone and padding data attributes', () => {
    const markup = renderToStaticMarkup(createElement(Panel, { tone: 'accent' }, 'Body'))
    expect(markup).toContain('<section')
    expect(markup).toContain('presentation-panel')
    expect(markup).toContain('data-tone="accent"')
    expect(markup).toContain('data-padding="default"')
  })

  it('Panel renders as a div and drops padding when requested', () => {
    const markup = renderToStaticMarkup(createElement(Panel, { as: 'div', padding: 'none' }, 'Body'))
    expect(markup).toContain('<div')
    expect(markup).toContain('data-padding="none"')
  })

  it('ActionGroup exposes alignment, gap, and stacking controls as data attributes', () => {
    const markup = renderToStaticMarkup(
      createElement(ActionGroup, { align: 'start', gap: 'cluster', stackAt: 'compact', stretchOnStack: true }, createElement('button', null, 'Go'))
    )
    expect(markup).toContain('presentation-action-group')
    expect(markup).toContain('data-align="start"')
    expect(markup).toContain('data-gap="cluster"')
    expect(markup).toContain('data-stack-at="compact"')
    expect(markup).toContain('data-stretch-on-stack="true"')
  })

  it('DefinitionList renders dl/dt/dd rows with a configurable label width', () => {
    const markup = renderToStaticMarkup(
      createElement(DefinitionList, {
        labelWidth: '12rem',
        items: [{ term: 'Account', description: 'Fixture' }]
      })
    )
    expect(markup).toContain('<dl')
    expect(markup).toContain('data-layout="rows"')
    expect(markup).toContain('--definition-list-label:12rem')
    expect(markup).toContain('<dt>Account</dt>')
    expect(markup).toContain('<dd>Fixture</dd>')
  })

  it('DefinitionList tiles layout renders auto-fit cards', () => {
    const markup = renderToStaticMarkup(
      createElement(DefinitionList, { layout: 'tiles', items: [{ term: 'Reads', description: '42' }] })
    )
    expect(markup).toContain('data-layout="tiles"')
  })

  it('DefinitionList exposes row rhythm and collapse breakpoints as data attributes', () => {
    const markup = renderToStaticMarkup(
      createElement(DefinitionList, { rowGap: 'relaxed', collapseAt: 'compact', items: [{ term: 'Updated', description: 'now' }] })
    )
    expect(markup).toContain('data-row-gap="relaxed"')
    expect(markup).toContain('data-collapse-at="compact"')
  })

  it('FieldHint renders neutral copy by default and error tone when requested', () => {
    const neutral = renderToStaticMarkup(createElement(FieldHint, null, 'Helper text'))
    expect(neutral).toContain('presentation-field-hint')
    expect(neutral).toContain('data-tone="neutral"')
    const error = renderToStaticMarkup(createElement(FieldHint, { tone: 'error' }, 'Invalid'))
    expect(error).toContain('data-tone="error"')
  })

  it('FormGrid renders children inside the Astryx grid', () => {
    const markup = renderToStaticMarkup(
      createElement(FormGrid, { columns: 3, minChildWidth: 240 }, createElement('span', null, 'field'))
    )
    expect(markup).toContain('field')
  })

  it('FormGrid forwards a className for descendant scoping', () => {
    const markup = renderToStaticMarkup(
      createElement(FormGrid, { className: 'settings-proxy-form' }, createElement('span', null, 'field'))
    )
    expect(markup).toContain('settings-proxy-form')
    expect(markup).toContain('field')
  })

  it('FormGridFullSpan wraps its children', () => {
    const markup = renderToStaticMarkup(
      createElement(FormGrid, null, createElement(FormGridFullSpan, null, 'wide'))
    )
    expect(markup).toContain('wide')
  })

  it('PresentationDrawer renders its inline SSR shell with labelled dialog semantics', () => {
    const markup = renderToStaticMarkup(
      createElement(
        PresentationDrawer,
        {
          isOpen: true,
          onOpenChange: () => undefined,
          title: 'Delete account',
          description: 'This cannot be undone.',
          closeLabel: 'Close dialog',
          role: 'alertdialog',
          footer: createElement('button', null, 'Delete')
        },
        createElement('p', null, 'Confirmation content')
      )
    )
    expect(markup).toContain('data-presentation-drawer-overlay')
    expect(markup).toContain('presentation-drawer-panel')
    expect(markup).toContain('role="alertdialog"')
    expect(markup).toContain('aria-modal="true"')
    expect(markup).toContain('aria-labelledby=')
    expect(markup).toContain('aria-describedby=')
    expect(markup).toContain('Confirmation content')
    expect(markup).toContain('Close dialog')
    expect(markup).toContain('Delete')
  })

  it('FormDrawer renders header, children, and a submit button targeting the form id', () => {
    const markup = renderToStaticMarkup(
      createElement(
        FormDrawer,
        {
          isOpen: true,
          onOpenChange: () => undefined,
          title: 'Add account',
          description: 'Discover or paste',
          closeLabel: 'Close dialog',
          formId: 'account-add-form',
          submitLabel: 'Save account',
          footerSecondary: createElement('button', null, 'Cancel')
        },
        createElement('form', { id: 'account-add-form' })
      )
    )
    expect(markup).toContain('Add account')
    expect(markup).toContain('Discover or paste')
    expect(markup).toContain('form="account-add-form"')
    expect(markup).toContain('Save account')
    expect(markup).toContain('Cancel')
  })

  it('TypedConfirmationDialog renders the confirmation proof, gated submit, and exact-match token', () => {
    const markup = renderToStaticMarkup(
      createElement(TypedConfirmationDialog, {
        isOpen: true,
        onOpenChange: () => undefined,
        title: 'Delete account',
        description: 'This cannot be undone.',
        closeLabel: 'Close dialog',
        expected: 'account-fixture',
        inputLabel: 'Type the account name',
        inputHint: 'Type it exactly.',
        actionLabel: 'Delete',
        cancelLabel: 'Cancel',
        confirmation: '',
        onConfirmationChange: () => undefined,
        isActionLoading: false,
        onAction: () => undefined
      })
    )
    expect(markup).toContain('role="alertdialog"')
    expect(markup).toContain('aria-labelledby=')
    expect(markup).toContain('confirmation-proof')
    expect(markup).toContain('<code')
    expect(markup).toContain('translate="no"')
    expect(markup).toContain('account-fixture')
    expect(markup).toContain('Delete')
    expect(markup).toContain('Cancel')
    // Empty confirmation must disable the action button.
    expect(markup).toContain('disabled')
  })

  it('TypedConfirmationDialog enables the action when the typed value matches', () => {
    const markup = renderToStaticMarkup(
      createElement(TypedConfirmationDialog, {
        isOpen: true,
        onOpenChange: () => undefined,
        title: 'Delete account',
        description: 'This cannot be undone.',
        closeLabel: 'Close dialog',
        expected: 'account-fixture',
        inputLabel: 'Type the account name',
        inputHint: 'Type it exactly.',
        actionLabel: 'Delete',
        cancelLabel: 'Cancel',
        confirmation: 'account-fixture',
        onConfirmationChange: () => undefined,
        isActionLoading: false,
        onAction: () => undefined
      })
    )
    // Action button renders without a disabled attribute when confirmation matches.
    const actionButton = markup.match(/<button[^>]*>Delete<\/button>/)?.[0] ?? ''
    expect(actionButton).not.toContain('disabled')
  })
})
