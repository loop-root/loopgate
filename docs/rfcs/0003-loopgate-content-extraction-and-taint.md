**Last updated:** 2026-03-24

# RFC 0003: Loopgate Content Extraction, Provenance, and Taint Policy

- Status: Draft
- Applies to: Loopgate capability results derived from remote content, local documents, repo text, and future skill/document ingestion paths

## 1. Purpose

This RFC defines the normative rules for how Loopgate classifies content,
extracts structured fields, records provenance, and applies taint/default
classification before any result is relayed to operator clients or future UI surfaces.

The goal is to keep untrusted content from becoming prompt-eligible,
memory-eligible, or authority-bearing by accident.

## 2. Core invariant

No natural-language authority.

Natural-language text from:

- skills
- webpages
- docs
- markdown
- repo files
- user prompts

MUST NOT directly create or expand authority.

Authority may be created or expanded only by:

- typed capability registration
- explicit schema/config
- Loopgate policy
- approval state
- token/session validation

## 3. Scope

This RFC covers:

- content classes
- extractor types
- structured-result requirements
- provenance metadata
- taint defaults
- classification defaults
- quarantine requirements
- promotion rules

This RFC does not cover:

- provider token handling
- operator client prompt compilation internals
- browser rendering policy beyond the metadata contract

## 4. Content classes

Every capability result that derives from fetched, uploaded, or parsed content
MUST declare one content class.

Allowed content classes:

- `structured_json`
- `html`
- `markdown`
- `plain_text`
- `code_or_config`
- `binary`

Different content classes MUST NOT share one generic extraction rule.

## 5. Extractor types

Every capability that returns structured fields from content MUST declare an
explicit extractor type.

Allowed extractor types:

- `json_field_allowlist`
- `json_nested_selector`
- `html_selector_allowlist`
- `markdown_section_selector`
- `regex_capture`
- `yaml_key_allowlist`
- `toml_key_allowlist`
- `none`

If no extractor is defined, or if the extractor type is `none`:

- `structured_result` MUST be an empty object
- `quarantine_ref` MUST be present
- classification MUST remain non-prompt-eligible
- classification MUST remain non-memory-eligible
- classification MUST be `display_only` or `audit_only`

Loopgate MUST fail closed. It MUST NOT perform best-effort extraction.

## 6. Extraction rules by content class

### 6.1 structured_json

Preferred extractor types:

- `json_field_allowlist`
- `json_nested_selector`

Rules:

- extraction MUST use deterministic parsing
- fields MUST be explicitly named
- field size limits SHOULD exist
- provider/capability config SHOULD be able to mark fields as required,
  optional, or redact-if-present

### 6.2 html

Preferred extractor types:

- `html_selector_allowlist`

Rules:

- only deterministic selectors are allowed
- raw HTML MUST NOT be prompt-eligible by default
- model-based extraction in Loopgate is prohibited

### 6.3 markdown

Preferred extractor types:

- `markdown_section_selector`
- `regex_capture`

Rules:

- extraction MUST be bounded to explicit sections or patterns
- freeform markdown summarization in Loopgate is prohibited

### 6.4 plain_text

Preferred extractor types:

- `regex_capture`
- `none`

Rules:

- plain text SHOULD remain quarantine-first unless a narrowly scoped extractor
  is defined

### 6.5 code_or_config

Preferred extractor types:

- `json_field_allowlist`
- `json_nested_selector`
- `yaml_key_allowlist`
- `toml_key_allowlist`

Rules:

- prefer real parsers over text heuristics
- model-based interpretation in Loopgate is prohibited

### 6.6 binary

Preferred extractor types:

- `none`

Rules:

- binary content MUST remain quarantined unless a future deterministic binary
  parser is explicitly added and reviewed

## 7. Structured result contract

Loopgate MUST relay structured fields through `structured_result`.

`structured_result` MUST contain only extracted, allowlisted fields.

Loopgate MUST NOT forward raw content blobs in `structured_result`.

If extraction fails or no extractor is defined:

- `structured_result = {}`
- `quarantine_ref` required
- classification remains restrictive

## 8. Provenance metadata

Every extracted result MUST carry metadata sufficient for operator clients and future UI
surfaces to make deterministic decisions.

Required metadata fields:

- `content_origin`
  - `remote` or `local`
- `content_class`
- `content_type`
- `extractor`
- `derived_from_quarantine_ref`
- `field_trust`
  - `deterministic` or `model_derived`
- `prompt_eligible`
- `memory_eligible`
- `display_only`
- `audit_only`
- `quarantined`

Recommended additional metadata fields:

- `field_count`
- `sensitivity`
- `size_bytes`

Operator clients and future UI surfaces MUST obey this metadata.

## 9. Taint rule

Any field derived from:

- remote content
- user-uploaded docs
- webpages
- repo text
- skill text

is tainted by default.

Default tainted classification:

- `prompt_eligible = false`
- `memory_eligible = false`
- `quarantined = true`

Only an explicit deterministic extractor policy may relax this.

## 10. Model-derived extraction

Loopgate MUST NOT use a model as a privileged extractor for content.

Model-derived extraction MAY exist later only as:

- advisory
- non-authoritative
- outside the privileged control-plane extraction path

If model-derived extraction is ever introduced elsewhere:

- `field_trust` MUST be `model_derived`
- prompt and memory eligibility MUST still default to false
- the extracted result MUST NOT create authority

## 11. Quarantine rules

Raw content that is fetched, uploaded, or parsed from untrusted sources MUST be
stored in quarantine before any structured relay path returns.

Quarantine rules:

- viewing quarantined content and promoting quarantined content are different
  actions
- viewing quarantined content MUST NOT automatically make it prompt-eligible
- viewing quarantined content MUST NOT automatically make it memory-eligible
- access to quarantined content SHOULD require explicit operator intent

## 12. Promotion rules

Promotion MUST be explicit and auditable.

Possible promotion targets:

- displayable
- memory-eligible
- prompt-eligible

Promotion MUST NOT occur:

- implicitly
- because content was viewed
- because content was summarized in natural language

If promotion is implemented later, it SHOULD require:

- explicit operator action
- durable audit event
- preserved provenance linkage to the source quarantine record

Promotion MUST create a new derived artifact or append-only promotion record.

Promotion MUST NOT mutate the original quarantined source artifact in place.

Recommended promotion record fields:

- `source_quarantine_ref`
- `source_content_sha256`
- `promotion_target`
- `promoted_by`
- `promoted_at_utc`
- `derived_artifact_ref`

Viewing quarantined content and promoting quarantined content are distinct
actions. Viewing MUST NOT implicitly produce a promotion record.

## 13. Current implementation guidance

The current codebase is closest to:

- `structured_json`
- `json_field_allowlist`

for provider-backed capabilities, with raw payload quarantine already present.

Initial implementation work for this RFC SHOULD proceed in this order:

1. formalize content class and extractor config fields
2. attach provenance metadata to all configured capability results
3. enforce taint defaults in Loopgate result construction
4. add deterministic nested JSON selectors
5. add HTML and markdown extractors only after the above are stable

## 14. Open questions

- Which concrete doc/web extraction use cases are required first
  - version strings
  - metadata
  - headings
  - bounded status text
- Which promotion steps should exist, and who may authorize them
- how promotion approval and operator identity should bind to future browser/UI
  sessions
