import type { ReactNode } from 'react';

import {
  Collapsible as ShadcnCollapsible,
  CollapsibleContent,
  CollapsibleTrigger
} from '@/components/ui/collapsible';
import { Icons } from '@/components/icons';
import { cn } from '@/lib/utils';

export interface CollapsibleProps {
  trigger: string;
  children: ReactNode;
  defaultIsOpen?: boolean;
  isOpen?: boolean;
  onOpenChange?: (isOpen: boolean) => void;
  className?: string;
}

export function Collapsible({
  trigger,
  children,
  defaultIsOpen,
  isOpen,
  onOpenChange,
  className
}: CollapsibleProps) {
  return (
    <ShadcnCollapsible
      className={cn('group', className)}
      defaultOpen={defaultIsOpen}
      open={isOpen}
      onOpenChange={onOpenChange}
    >
      <CollapsibleTrigger className='flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm font-medium transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2'>
        {trigger}
        <Icons.chevronDown className='size-4 shrink-0 transition-transform group-data-[open]:rotate-180' />
      </CollapsibleTrigger>
      <CollapsibleContent>{children}</CollapsibleContent>
    </ShadcnCollapsible>
  );
}
