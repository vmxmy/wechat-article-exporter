import { cn } from '@/lib/utils';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';

export type SemanticStatusVariant = 'success' | 'warning' | 'error' | 'accent' | 'neutral';

export interface StatusDotProps {
  variant: SemanticStatusVariant;
  label: string;
  tooltip?: string;
  isPulsing?: boolean;
  className?: string;
}

const variantClasses: Record<SemanticStatusVariant, string> = {
  success: 'bg-emerald-500',
  warning: 'bg-amber-500',
  error: 'bg-destructive',
  accent: 'bg-primary',
  neutral: 'bg-muted-foreground',
};

export function StatusDot({
  variant,
  label,
  tooltip,
  isPulsing = false,
  className,
}: StatusDotProps) {
  const dot = (
    <span
      aria-label={label}
      className={cn(
        'size-2 rounded-full',
        variantClasses[variant],
        isPulsing && 'animate-pulse',
        className
      )}
    />
  );

  if (tooltip === undefined) {
    return dot;
  }

  return (
    <Tooltip>
      <TooltipTrigger>{dot}</TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
