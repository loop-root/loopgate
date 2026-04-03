**Last updated:** 2026-03-24

# RFC 0008: Selector Schema and Extractor Contracts

- Status: Draft
- Applies to: Loopgate extraction config, provider-backed capability results,
  future document extraction, and promotion candidate generation

## 1. Purpose

This RFC defines the narrow v1 selector schema and extractor contracts for
Loopgate.

The goal is to support only a small set of deterministic extraction paths for
known content classes while keeping natural-language content quarantined and
non-authoritative by default.

Loopgate does not "understand documents." It supports a finite set of
extractor contracts.

## 2. Core invariants

- Extraction is deterministic, config-driven, and fail-closed.
- Extraction and promotion are separate decisions.
- Unknown extractor types are denied.
- Missing selector targets are denied.
- Malformed extractor config is denied.
- Extracted shape does not grant trust.
- Oversized extracted content does not become inline content by convenience.
- `blob_ref` is storage indirection, not trust escalation.

## 3. V1 extractor types

Only the following extractor types are supported in v1:

- `json_field_allowlist`
- `json_nested_selector`
- `markdown_frontmatter_keys`
- `markdown_section_selector`
- `html_meta_allowlist`

No other extractor type is implicitly supported.

If a capability declares an unknown extractor type, Loopgate MUST fail closed.

## 4. V1 content classes

This RFC defines selector behavior for:

- `structured_json`
- `markdown`
- `html`

This RFC does not define v1 extraction behavior for:

- `plain_text`
- `code_or_config`
- `binary`

Those content classes remain future work and MUST NOT silently reuse these
extractor rules.

## 5. Shared selector schema requirements

Every extractor config MUST:

- declare one extractor `type`
- define unique output field names
- define explicit per-field metadata policy
- define size policy for inline output

In v1, configured extractor targets are required by default.

If a configured target is missing, extraction fails closed unless a future RFC
adds an explicit `optional` / `required` field-policy knob.

Every extracted field MUST have:

- `name`
- `sensitivity`
- `max_inline_bytes`
- `allow_blob_ref_fallback`

Extractor-specific parameters are defined below.

If two selectors produce the same output field name, Loopgate MUST fail closed.

Example v1 capability shape:

```yaml
content_class: structured_json
extractor: json_field_allowlist
response_fields:
  - name: service
    json_field: service
    sensitivity: benign
    max_inline_bytes: 64
    allow_blob_ref_fallback: false
  - name: message
    json_field: message
    sensitivity: tainted_text
    max_inline_bytes: 256
    allow_blob_ref_fallback: true
```

## 6. Extractor: `json_field_allowlist`

### 6.1 Scope

Valid only for `structured_json`.

### 6.2 Selector shape

Each configured field MUST declare:

- `name`
- `json_field`
- `sensitivity`
- `max_inline_bytes`
- `allow_blob_ref_fallback`

### 6.3 Output rules

V1 `json_field_allowlist` may return only:

- top-level scalar values
- `blob_ref` metadata objects when blob fallback is explicitly allowed

V1 MUST NOT return:

- arrays
- objects
- nested structures

### 6.4 Failure semantics

Loopgate MUST fail closed if:

- the response is not valid JSON
- the top-level JSON value is not an object
- a required field is missing
- a configured field resolves to a non-scalar value
- a configured field exceeds `max_inline_bytes` and blob fallback is disabled
- a configured field name is duplicated or suspicious without explicit policy

If blob fallback is enabled and an extracted scalar exceeds
`max_inline_bytes`, Loopgate MAY return a `blob_ref` for that field instead of
failing the whole capability result.

Example:

```yaml
content_class: structured_json
extractor: json_field_allowlist
response_fields:
  - name: healthy
    json_field: healthy
    sensitivity: benign
    max_inline_bytes: 8
    allow_blob_ref_fallback: false
```

## 7. Extractor: `json_nested_selector`

### 7.1 Scope

Valid only for `structured_json`.

### 7.2 Selector shape

Each configured field MUST declare:

- `name`
- `json_path`
- `sensitivity`
- `max_inline_bytes`
- `allow_blob_ref_fallback`

`json_path` MUST be an exact dotted path of object keys. V1 supports only
object traversal. Arrays, wildcards, and recursive selectors are not
supported.

### 7.3 Output rules

V1 `json_nested_selector` may return only:

- scalar values reached through exact object-key traversal
- `blob_ref` metadata objects when blob fallback is explicitly allowed

V1 MUST NOT return:

- arrays
- objects
- values reached through wildcard or ambiguous selection

### 7.4 Failure semantics

Loopgate MUST fail closed if:

- the response is not valid JSON
- the top-level JSON value is not an object
- a configured path element is missing
- traversal reaches a non-object before the final element
- the final resolved value is an array or object
- a configured field exceeds `max_inline_bytes` and blob fallback is disabled

If blob fallback is enabled and an extracted scalar exceeds
`max_inline_bytes`, Loopgate MAY return a `blob_ref` for that field instead of
failing the whole capability result.

Example:

```yaml
content_class: structured_json
extractor: json_nested_selector
response_fields:
  - name: status_description
    json_path: status.description
    sensitivity: tainted_text
    max_inline_bytes: 128
    allow_blob_ref_fallback: false
```

## 8. Extractor: `markdown_frontmatter_keys`

### 7.1 Scope

Valid only for `markdown`.

### 7.2 Selector shape

Each configured field MUST declare:

- `name`
- `frontmatter_key`
- `sensitivity`
- `max_inline_bytes`
- `allow_blob_ref_fallback`

### 7.3 Output rules

V1 `markdown_frontmatter_keys` may return only:

- scalar frontmatter values
- `blob_ref` metadata objects when blob fallback is explicitly allowed

V1 MUST NOT return:

- arrays
- objects
- raw frontmatter blobs

Example:

```yaml
content_class: markdown
extractor: markdown_frontmatter_keys
response_fields:
  - name: version
    frontmatter_key: version
    sensitivity: benign
    max_inline_bytes: 32
    allow_blob_ref_fallback: false
  - name: release_id
    frontmatter_key: release_id
    sensitivity: benign
    max_inline_bytes: 64
    allow_blob_ref_fallback: false
```

### 7.4 Trust model

Frontmatter is more structured than document prose, but it is still untrusted
content.

Frontmatter extraction does not imply prompt or memory eligibility.

Promotion policy for extracted frontmatter values remains governed by RFC 0005
and RFC 0006.

### 7.5 Failure semantics

Loopgate MUST fail closed if:

- the markdown source does not contain valid frontmatter when this extractor is
  configured
- a configured frontmatter key is missing
- a configured frontmatter key resolves to a non-scalar value
- a configured frontmatter value exceeds `max_inline_bytes` and blob fallback
  is disabled
- extractor config is malformed

## 9. Extractor: `markdown_section_selector`

### 8.1 Scope

Valid only for `markdown`.

### 8.2 Selector shape

Each configured field MUST declare:

- `name`
- `heading_path`
- `sensitivity`
- `max_inline_bytes`
- `allow_blob_ref_fallback`

`heading_path` MUST be an explicit ordered list of heading labels.

V1 heading selection MUST use exact path matching only.

### 8.3 Output rules

V1 `markdown_section_selector` returns only:

- raw section text
- or `blob_ref` metadata when blob fallback is explicitly allowed

Returned section text MUST be treated as tainted text and display-only in v1.

V1 `markdown_section_selector` MUST NOT:

- summarize content
- pick "best matching" headings
- merge multiple sections
- perform fuzzy heading matching
- return nested structure

Example:

```yaml
content_class: markdown
extractor: markdown_section_selector
response_fields:
  - name: overview
    heading_path:
      - Overview
    sensitivity: tainted_text
    max_inline_bytes: 512
    allow_blob_ref_fallback: true
```

### 8.4 Failure semantics

Loopgate MUST fail closed if:

- the configured heading path is missing
- the heading path matches ambiguously
- the selected section exceeds `max_inline_bytes` and blob fallback is disabled
- extractor config is malformed

If blob fallback is enabled, oversized section text MAY be returned as
`blob_ref` metadata only.

## 10. Extractor: `html_meta_allowlist`

### 9.1 Scope

Valid only for `html`.

### 9.2 Selector shape

Each configured field MUST declare:

- `name`
- exactly one of:
  - `html_title: true`
  - `meta_name`
  - `meta_property`
- `sensitivity`
- `max_inline_bytes`
- `allow_blob_ref_fallback`

V1 HTML metadata extraction MUST use `tainted_text` sensitivity.

### 9.3 Output rules

V1 `html_meta_allowlist` may return only:

- HTML `<title>` text
- HTML `<meta name=... content=...>` text
- HTML `<meta property=... content=...>` text
- `blob_ref` metadata objects when blob fallback is explicitly allowed

V1 `html_meta_allowlist` MUST NOT:

- extract body text
- extract arbitrary attributes
- infer selectors dynamically
- auto-upgrade extracted text into prompt or memory eligibility

### 9.4 Failure semantics

Loopgate MUST fail closed if:

- the HTML document does not contain a `<head>` section
- a configured `html_title` target is missing
- a configured `meta_name` target is missing
- a configured `meta_property` target is missing
- a configured selector matches more than one result where exactly one is expected
- HTML metadata config is malformed
- extracted text exceeds `max_inline_bytes` and blob fallback is disabled

Loopgate MUST NOT silently pick the first match unless a future RFC defines
that exact contract explicitly.

## 11. Canonicalization rules

V1 canonicalization MUST be explicit and boring.

### 10.1 General rules

- line endings MUST normalize to `\n`
- no best-effort semantic rewriting is allowed
- extractor output MUST preserve content meaning rather than "cleaning" it

### 10.2 JSON

- parsing MUST use deterministic JSON decoding
- output field names come from config, not source key order
- scalar values MUST preserve their JSON scalar meaning

### 10.3 Markdown frontmatter

- frontmatter parsing MUST be deterministic
- key matching MUST be exact
- scalar values MUST preserve parsed scalar meaning

### 10.4 Markdown sections

- heading path matching MUST be exact and ordered
- heading path matching is case-sensitive in v1
- selected section text MUST normalize line endings only
- no fuzzy whitespace-based heading matching

### 10.5 HTML metadata

- tag name matching MUST be deterministic
- `meta_name` and `meta_property` matching MUST be exact
- extracted title/meta text MUST normalize line endings only and trim leading/trailing whitespace
- extracted title text remains tainted/display-only by default

## 11.6 Content-type expectations

V1 content-type matching is intentionally strict.

- `structured_json` expects JSON media types
- `markdown` expects markdown media types only
- `html` expects `text/html`

If a remote source returns the wrong content type, Loopgate MUST fail closed
rather than attempting best-effort extraction.

## 12. Oversized output and blob fallback

Oversized extracted content MUST NOT be silently truncated in v1.

For every extracted field, one of two outcomes is allowed:

1. fail closed if the field exceeds `max_inline_bytes`
2. return `blob_ref` metadata if `allow_blob_ref_fallback = true`

V1 `blob_ref` fallback rules:

- fallback applies only to size overflow, not malformed or ambiguous extraction
- fallback does not make the field prompt-eligible or memory-eligible
- fallback does not make the field directly promotable
- fallback returns metadata only, not preview content

## 13. Promotion implications

Extraction does not imply promotion.

V1 target rules remain:

- extracted frontmatter scalars may be considered for promotion under RFC 0005
  and RFC 0006
- extracted markdown section text remains tainted scalar text and is
  display-only in v1
- `blob_ref` fields are not directly promotable in v1

## 14. Failure matrix

The following conditions MUST fail closed:

- unknown extractor type
- extractor/content-class mismatch
- malformed selector config
- duplicate output field name
- missing selector target
- ambiguous selector match
- non-scalar output where scalar is required
- oversized output without explicit blob fallback

The following conditions MAY degrade to `blob_ref` only when explicitly
configured:

- oversized scalar output

## 15. Initial implementation order

Recommended implementation order:

1. `json_field_allowlist` hardening continues as baseline
2. `markdown_frontmatter_keys`
3. `markdown_section_selector`
4. `html_meta_allowlist`
5. stop and validate ergonomics before anything broader than HTML metadata

## 16. Non-goals for v1

This RFC explicitly does not introduce:

- HTML text extraction
- generic plain-text extraction
- regex capture for arbitrary text
- markdown summarization
- model-derived extraction in Loopgate

## 17. Open questions

- whether extractor config should later support explicit `required` vs
  `optional` field policy
- whether frontmatter should later support explicit enum validation in config
- whether markdown sections should later expose tiny trusted previews in UI
- whether future HTML extraction should expand beyond title/meta after ergonomics review
