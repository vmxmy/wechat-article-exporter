export type AccountEntryMode = 'create' | 'edit'

export interface AccountDraft {
  readonly fakeid: string
  readonly name: string
  readonly alias: string
}
