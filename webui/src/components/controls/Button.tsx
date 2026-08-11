import * as React from 'react'
import {
  Button as ShadcnButton,
  buttonVariants,
} from '@/components/ui/button'
import { cn } from '@/lib/utils'

type ShadcnVariant = React.ComponentProps<typeof ShadcnButton>['variant']
type ShadcnSize = React.ComponentProps<typeof ShadcnButton>['size']

export type ButtonVariant =
  | 'primary'
  | 'secondary'
  | 'ghost'
  | 'destructive'
  | 'transparent'
  | 'surface'
  | 'muted'

export type ButtonSize = 'sm' | 'default'

const VARIANT_MAP: Record<ButtonVariant, ShadcnVariant> = {
  primary: 'default',
  secondary: 'secondary',
  ghost: 'ghost',
  destructive: 'destructive',
  transparent: 'ghost',
  surface: 'outline',
  muted: 'ghost',
}

const SIZE_MAP: Record<ButtonSize, ShadcnSize> = {
  sm: 'sm',
  default: 'default',
}

export interface ButtonProps
  extends Omit<React.ComponentProps<typeof ShadcnButton>, 'variant' | 'size'> {
  label: React.ReactNode
  /** Keeps an explicit accessible name when a compact visual label is used. */
  accessibleLabel?: string
  variant?: ButtonVariant
  size?: ButtonSize
  isLoading?: boolean
  isDisabled?: boolean
}

export function Button({
  label,
  accessibleLabel,
  variant = 'primary',
  size = 'default',
  isLoading = false,
  isDisabled = false,
  disabled,
  className,
  children,
  ...props
}: ButtonProps) {
  return (
    <ShadcnButton
      variant={VARIANT_MAP[variant]}
      size={SIZE_MAP[size]}
      isLoading={isLoading}
      disabled={disabled ?? isDisabled}
      aria-label={accessibleLabel}
      className={cn(className)}
      {...props}
    >
      {children ?? label}
    </ShadcnButton>
  )
}

export { buttonVariants }
