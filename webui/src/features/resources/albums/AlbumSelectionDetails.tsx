import { TechnicalDetails } from '../../../components/presentation'
import type { MessageCatalog } from '../../../i18n'
import type { AlbumRecord } from '../../../lib/api'

export type AlbumRecordWithAccountName = AlbumRecord

interface AlbumSelectionDetailsProps {
  readonly album?: AlbumRecordWithAccountName
  readonly messages: MessageCatalog
}

export function AlbumSelectionDetails({ album, messages }: AlbumSelectionDetailsProps) {
  if (!album) return null
  const albumIDLabel = messages.exports.selection.albumId
  const accountIDLabel = messages.resources.albums.filters.accountId
  return <TechnicalDetails
    label={`${messages.resources.albums.title}: ${albumIDLabel}`}
    items={[
      { label: albumIDLabel, value: album.id, copyLabel: albumIDLabel },
      { label: accountIDLabel, value: album.accountId, copyLabel: accountIDLabel }
    ]}
  />
}
