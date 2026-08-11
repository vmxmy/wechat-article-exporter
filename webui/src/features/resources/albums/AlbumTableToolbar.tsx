import { TextInput } from '@/components/controls/TextInput'
import { Toolbar } from '@/components/controls/Toolbar'
import { AccountRemoteSelector } from '@/components/presentation'
import type { MessageCatalog } from '@/i18n'

export interface AlbumTableToolbarProps {
  readonly messages: MessageCatalog
  readonly accountID: string | undefined
  readonly onAccountChange: (accountID: string | undefined) => void
  readonly keyword: string
  readonly onKeywordChange: (keyword: string) => void
}

export function AlbumTableToolbar({ messages, accountID, onAccountChange, keyword, onKeywordChange }: AlbumTableToolbarProps) {
  const copy = messages.resources.albums.filters
  return (
    <Toolbar className="album-table-toolbar" label={copy.title} stackAt="medium"
      startContent={
        <>
          <div className="resource-toolbar-search">
            <TextInput label={copy.keyword} value={keyword} onChange={onKeywordChange} placeholder={copy.keyword} isLabelHidden hasClear />
          </div>
          <div className="resource-toolbar-item">
            <AccountRemoteSelector
              label={messages.resources.accounts.columns.name}
              value={accountID}
              onChange={onAccountChange}
              placeholder={messages.resources.accounts.columns.name}
              isLabelHidden
              copy={messages.selectors}
            />
          </div>
        </>
      }
    />
  )
}
