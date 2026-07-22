## ADDED Requirements

### Requirement: Local QR-code login
The system SHALL perform WeChat Official Account login locally by obtaining a login QR code, presenting it in the terminal and as an optional image file, polling login status at a bounded interval, and completing the login without routing credentials through project-operated services.

#### Scenario: Interactive QR login
- **WHEN** an interactive user runs `login`
- **THEN** the terminal shows a scannable QR representation, status transitions, expiry time, and a cancel action

#### Scenario: Non-interactive QR login
- **WHEN** a non-TTY user runs `login` with an output path
- **THEN** the system writes the QR image to that path, prints redacted instructions to stderr, and emits only the final result to stdout

#### Scenario: Expired QR code
- **WHEN** the QR code expires before authorization completes
- **THEN** the system reports expiry and offers or performs a bounded refresh without retaining the expired login state

### Requirement: Session capture and persistence
The system SHALL capture the authenticated WeChat token and cookies, persist them per profile in secure local storage, and maintain an HTTP cookie jar with correct domain, path, expiry, and secure attributes.

#### Scenario: Successful login
- **WHEN** WeChat confirms login and returns a token and cookies
- **THEN** the system verifies the session against a lightweight authenticated endpoint before marking the profile authenticated

#### Scenario: Process restart
- **WHEN** the user restarts the binary before the WeChat session expires
- **THEN** authenticated commands reuse the saved session without requiring a new QR login

### Requirement: Session status and expiry handling
The system SHALL expose authenticated account identity, session age, expiry estimate, last validation, and validation result without exposing the underlying token or cookie values.

#### Scenario: Expired session during a command
- **WHEN** WeChat rejects the saved session as expired
- **THEN** the command stops safely, marks the session invalid, preserves local data, and instructs the user to log in again

#### Scenario: Network failure during validation
- **WHEN** session validation cannot reach WeChat
- **THEN** the system distinguishes an unknown network state from an explicitly invalid session

### Requirement: Account switching
The system SHALL support the same account-switching flow available in the current Web implementation when the authenticated WeChat session exposes multiple manageable official accounts.

#### Scenario: List switchable accounts
- **WHEN** the active session has multiple manageable accounts
- **THEN** the user can list their identities without revealing session secrets

#### Scenario: Switch the active account
- **WHEN** the user selects a different manageable account
- **THEN** the system completes the WeChat switch flow, validates the new identity, and associates subsequent synchronized data with the selected profile context

### Requirement: Logout and revocation
The system SHALL support local logout and best-effort upstream logout, SHALL remove local session secrets atomically, and SHALL not delete the local article library unless separately requested.

#### Scenario: Normal logout
- **WHEN** a user runs `logout`
- **THEN** the upstream logout is attempted, local session secrets are removed even if the upstream call fails, and cached content remains available

### Requirement: Login transport hardening
The login client SHALL restrict redirects and upstream hosts, set a bounded user agent and timeouts, reject insecure transports except explicitly approved loopback development endpoints, and avoid logging login payloads.

#### Scenario: Unexpected redirect host
- **WHEN** a login response redirects to a host outside the approved WeChat host set
- **THEN** the system rejects the redirect and does not forward session cookies
