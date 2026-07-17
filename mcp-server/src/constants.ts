export const MCP_ROUTE = '/mcp';

const DAY_IN_SECONDS = 24 * 60 * 60;

// Access tokens are deliberately shorter than refresh grants. Compatible MCP clients
// refresh them automatically, while a leaked bearer token has a bounded lifetime.
export const OAUTH_ACCESS_TOKEN_TTL_SECONDS = 7 * DAY_IN_SECONDS;
export const OAUTH_REFRESH_TOKEN_TTL_SECONDS = 180 * DAY_IN_SECONDS;

// Dynamic client registrations must outlive refresh grants, otherwise a still-valid
// refresh token becomes unusable when its client record disappears.
export const OAUTH_CLIENT_REGISTRATION_TTL_SECONDS = 365 * DAY_IN_SECONDS;
