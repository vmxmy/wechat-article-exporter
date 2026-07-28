import {
  DropdownMenu as ShadcnDropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem as ShadcnDropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Icons } from '@/components/icons'
import { Button, type ButtonSize, type ButtonVariant } from './Button'

export interface DropdownAction {
  label: string
  onClick: () => void
  isDisabled?: boolean
}

export type DropdownMenuItem =
  | DropdownAction
  | { type: 'section'; title: string; items: DropdownAction[] }
  | { type: 'divider' }

export interface DropdownButton {
  label: string
  variant: ButtonVariant
  size: ButtonSize
  isDisabled: boolean
}

export interface DropdownMenuProps {
  button: DropdownButton
  items: DropdownMenuItem[]
  menuWidth?: string
  /** Class name for the trigger button; popup styling stays on className. */
  triggerClassName?: string
  className?: string
}

function ActionItem({ action }: { action: DropdownAction }) {
  return (
    <ShadcnDropdownMenuItem
      disabled={action.isDisabled}
      onClick={() => action.onClick()}
    >
      {action.label}
    </ShadcnDropdownMenuItem>
  )
}

export function DropdownMenu({
  button,
  items,
  menuWidth,
  triggerClassName,
  className
}: DropdownMenuProps) {
  return (
    <ShadcnDropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            label={
              <>
                {button.label}
                <Icons.chevronDown className='size-4' />
              </>
            }
            variant={button.variant}
            size={button.size}
            className={triggerClassName}
            isDisabled={button.isDisabled}
          />
        }
      />
      <DropdownMenuContent
        className={className}
        style={menuWidth ? { minWidth: menuWidth } : undefined}
      >
        {items.map((item, index) => {
          if (!('type' in item)) {
            return <ActionItem key={index} action={item} />
          }

          if (item.type === 'divider') {
            return <DropdownMenuSeparator key={index} />
          }

          return (
            <DropdownMenuGroup key={index}>
              <DropdownMenuLabel>{item.title}</DropdownMenuLabel>
              {item.items.map((action, actionIndex) => (
                <ActionItem key={actionIndex} action={action} />
              ))}
            </DropdownMenuGroup>
          )
        })}
      </DropdownMenuContent>
    </ShadcnDropdownMenu>
  )
}
