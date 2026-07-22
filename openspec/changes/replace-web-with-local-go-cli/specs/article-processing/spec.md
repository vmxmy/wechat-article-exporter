## ADDED Requirements

### Requirement: Article response validation
The processor SHALL classify downloaded WeChat HTML as valid article, deleted article, known unavailable state, risk-control response, or parse error before the content enters the valid article library.

#### Scenario: Valid article
- **WHEN** the document contains a supported article payload and content root
- **THEN** the processor returns normalized article data, HTML, comment identifier when available, and discovered resources

#### Scenario: Known unavailable state
- **WHEN** the response identifies a known article restriction or unavailable reason
- **THEN** the processor returns the stable status reason and preserves no invalid content as the article body

### Requirement: CGI-data parsing
The processor SHALL parse current supported `window.cgiData` and related WeChat payload variants into a versioned normalized article model without executing untrusted page scripts.

#### Scenario: Supported payload variant
- **WHEN** a fixture contains a recognized payload encoding
- **THEN** title, account identity, body, publication metadata, message type, media metadata, album references, and engagement fields are normalized

#### Scenario: Malformed embedded data
- **WHEN** embedded payload data is syntactically invalid or exceeds configured limits
- **THEN** parsing fails safely without script execution, uncontrolled allocation, or partial record mutation

### Requirement: HTML normalization
The processor SHALL create a self-contained safe article document by making content visible, removing scripts, ads, tracking and irrelevant interface elements, preserving meaningful WeChat styling, rendering publication and author metadata, and rewriting resources through an explicit mapping.

#### Scenario: Normalize a standard article
- **WHEN** a valid article HTML document and complete resource mapping are supplied
- **THEN** the output contains the article content and local resource references, contains no executable remote scripts, and does not require WeChat JavaScript to reveal content

#### Scenario: Missing resource mapping
- **WHEN** a referenced resource has no local mapping
- **THEN** normalization records the missing resource and follows the caller's configured strict or best-effort policy

### Requirement: Text and Markdown rendering
The processor SHALL render semantically useful text and Markdown from the normalized article model while preserving headings, paragraphs, lists, links, images, code blocks, quotes, tables, and media references where the target format supports them.

#### Scenario: Render Markdown
- **WHEN** a normalized article contains mixed rich content
- **THEN** the Markdown output preserves reading order and equivalent semantics without retaining unsafe HTML unless explicitly allowed

### Requirement: Message-type coverage
The processor SHALL support standard graphic articles, text shares, image shares, audio cards, video shares, embedded media, paid articles when valid credentials provide content, and known unavailable-content variants represented by the repository fixtures.

#### Scenario: Audio article
- **WHEN** an article contains a supported audio card
- **THEN** the normalized model retains title, duration, source URL, album information, and a playable or downloadable local resource reference

#### Scenario: Image-share article
- **WHEN** an article payload represents an image share
- **THEN** image order, captions, and resource references are preserved in normalized and exported forms

### Requirement: Comments rendering
The processor SHALL merge downloaded top-level comments and replies into HTML, text, Markdown, JSON, DOCX, and PDF exports when requested, preserving stable ordering and author metadata subject to privacy settings.

#### Scenario: Include comments
- **WHEN** an export requests comments and local comment data exists
- **THEN** comments and replies are included after the article body in a documented structure

### Requirement: Parser and renderer limits
The processor SHALL enforce bounded input sizes, nesting, decoded payload sizes, resource counts, and output sizes, and SHALL not fetch external resources during pure parsing or rendering.

#### Scenario: Oversized input
- **WHEN** article HTML exceeds the configured maximum accepted size
- **THEN** processing stops with a classified limit error and no partial article content is committed

### Requirement: Regression compatibility
The Go processor SHALL be verified against the existing repository `samples/` corpus and sanitized production fixtures using normalized semantic snapshots and, for HTML, explicit structural and resource assertions.

#### Scenario: Fixture corpus
- **WHEN** the parser/renderer regression suite runs
- **THEN** every supported fixture either matches its approved normalized result or has an explicitly reviewed compatibility exception
