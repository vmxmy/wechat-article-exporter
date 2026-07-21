export interface CliToolDescriptor {
  name: string;
  inputSchema: { type: 'object'; [key: string]: unknown };
  annotations?: {
    readOnlyHint?: boolean;
    destructiveHint?: boolean;
    [key: string]: unknown;
  };
}

const CURRENT_READ_TOOLS = new Set([
  'download_article',
  'search_accounts',
  'list_articles',
  'get_account_by_url',
  'get_account_details',
  'get_author_info',
  'list_album',
  'get_account_name',
]);

const SENSITIVE_KEY =
  /(?:authorization|access[_-]?token|refresh[_-]?token|auth[_-]?key|api[_-]?key|client[_-]?secret|password|\btoken\b)/i;

export function requiredToolConfirmation(tool: CliToolDescriptor): string | null {
  if (tool.annotations?.destructiveHint === true) return tool.name;
  if (tool.annotations?.readOnlyHint === true || CURRENT_READ_TOOLS.has(tool.name)) return null;
  return tool.name;
}

export function assertToolConfirmation(tool: CliToolDescriptor, confirmation?: string): void {
  const required = requiredToolConfirmation(tool);
  if (required && confirmation !== required) {
    throw new Error(
      `Refusing protected operation without exact confirmation. Retry with --confirm ${required}, or inspect it first with --dry-run.`
    );
  }
}

export function redactSensitiveValue(value: unknown, key = ''): unknown {
  if (SENSITIVE_KEY.test(key)) return '[REDACTED]';
  if (Array.isArray(value)) return value.map(item => redactSensitiveValue(item));
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>).map(([childKey, childValue]) => [
        childKey,
        redactSensitiveValue(childValue, childKey),
      ])
    );
  }
  return value;
}

export function createToolDryRun(toolName: string, args: Record<string, unknown>): Record<string, unknown> {
  return {
    success: true,
    dryRun: true,
    operation: 'mcp.tools/call',
    tool: toolName,
    arguments: redactSensitiveValue(args),
    note: 'No MCP connection or tool call was made.',
  };
}
