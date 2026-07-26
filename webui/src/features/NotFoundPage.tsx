import { Link } from '@/components/controls/Link'
import { PageHeader, PageStack } from '../components/presentation'
import type { MessageCatalog } from '../i18n'

export function NotFoundPage({ messages }: { readonly messages: MessageCatalog }) {
  return (
    <PageStack className="not-found" aria-labelledby="not-found-title">
      <PageHeader
        eyebrow={messages.navigation.system}
        title={messages.notFound.title}
        titleId="not-found-title"
        description={messages.notFound.description}
        actions={<Link href="/" isStandalone hasUnderline>{messages.notFound.home}</Link>}
      />
    </PageStack>
  )
}
