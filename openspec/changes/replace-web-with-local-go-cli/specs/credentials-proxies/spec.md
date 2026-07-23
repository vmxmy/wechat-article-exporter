## ADDED Requirements

### Requirement: Credential import and management
The system SHALL import WeChat article credentials from supported JSON, environment, stdin, or interactive entry; validate required fields; associate each credential with the correct account; and track validation time and validity without exposing secret values.

#### Scenario: Import valid credentials
- **WHEN** a user imports a credential record containing the required account and request fields
- **THEN** the system stores the secret securely, records non-secret metadata in the library, and offers a validation request

#### Scenario: Import malformed credentials
- **WHEN** required fields are absent, oversized, or structurally invalid
- **THEN** the system rejects the record without persisting a partial secret

### Requirement: Secure secret storage
The system SHALL store WeChat sessions, article credentials, and proxy authorization in the OS credential store when available and SHALL use an explicitly initialized encrypted local vault with restrictive permissions as the documented fallback.

#### Scenario: OS credential store available
- **WHEN** the platform credential store can be used
- **THEN** secret bytes are not written to the SQLite database, configuration, logs, diagnostics, or command history

#### Scenario: Encrypted fallback setup
- **WHEN** the OS credential store is unavailable
- **THEN** the user must explicitly initialize or unlock the encrypted fallback before persistent secrets are stored

#### Scenario: Non-interactive vault unlock
- **WHEN** a non-TTY process selects the encrypted vault
- **THEN** unlock succeeds only with a protected passphrase file or explicitly provided automation environment, while absent input, wrong passphrase, an uninitialized vault, or group/world-readable passphrase file fails closed without echoing the secret

#### Scenario: Process restart
- **WHEN** a new process selects an initialized vault and supplies the correct passphrase source
- **THEN** it decrypts the same profile-scoped secrets, and deleting one profile removes only that profile's vault entries

### Requirement: Secret redaction
The system SHALL redact cookies, tokens, pass tickets, keys, app message tokens, authorization values, and sensitive URL query fields from logs, errors, dry runs, job records, JSON output, and diagnostics.

#### Scenario: Upstream error includes request URL
- **WHEN** an error contains a URL with sensitive query parameters
- **THEN** the rendered and structured error replaces sensitive values while retaining non-secret diagnostic context

### Requirement: Direct-first networking
The system SHALL attempt supported WeChat requests directly by default and SHALL use a proxy only when configured for that request class or when the user explicitly chooses proxy fallback.

#### Scenario: Direct request succeeds
- **WHEN** direct access to the target WeChat resource succeeds
- **THEN** no proxy receives the URL, headers, cookies, or response content

#### Scenario: Direct request fails with configured fallback
- **WHEN** a retryable direct request fails and a trusted fallback route is configured
- **THEN** the scheduler can retry through an eligible proxy according to policy and records the route decision without secrets

### Requirement: Proxy configuration and trust
The system SHALL support multiple HTTP-compatible proxy endpoints, optional authorization, request-class eligibility, explicit trust level, priority, health status, cooldown, and enable/disable state.

#### Scenario: Add a proxy
- **WHEN** a user adds a proxy endpoint
- **THEN** the system validates HTTPS or approved loopback HTTP, normalizes the URL, stores authorization separately, and does not mark it trusted for credentials by default

#### Scenario: Proxy health check
- **WHEN** a user tests configured proxies
- **THEN** the system reports latency, HTTP status, response validity, and credential-safety eligibility without sending real credentials during the test

### Requirement: Sensitive-request routing
Requests containing WeChat cookies, credentials, reading metrics parameters, comments parameters, or paid-content authorization SHALL be sent directly or through an explicitly credential-trusted proxy only.

#### Scenario: Untrusted proxy is the only route
- **WHEN** a sensitive request cannot go directly and only untrusted proxies are configured
- **THEN** the system refuses the request and explains how to configure a trusted route

#### Scenario: Trust a proxy for credentials
- **WHEN** a user marks a proxy credential-trusted
- **THEN** the system presents the exact classes of secrets and destinations involved and requires explicit confirmation

### Requirement: Proxy health and selection
The scheduler SHALL record bounded success/failure metrics, apply cooldown after repeated failures, prefer healthy routes, and avoid permanently excluding a recovered proxy without retesting.

#### Scenario: Proxy enters cooldown
- **WHEN** a proxy crosses the configured consecutive failure threshold
- **THEN** it is temporarily removed from selection and becomes eligible for a later probe

### Requirement: Network host restrictions
All user-supplied destination URLs SHALL be validated against the relevant operation's allowed schemes and hosts, redirects SHALL be revalidated, and proxy endpoints SHALL be protected against SSRF to local and link-local addresses unless explicitly approved for development.

#### Scenario: Redirect to local metadata endpoint
- **WHEN** a permitted remote URL redirects to a loopback, private, link-local, or cloud metadata address
- **THEN** the system blocks the redirect and reports a destination-policy error
