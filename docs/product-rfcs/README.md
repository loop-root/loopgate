**Last updated:** 2026-04-01

# Product RFCs (`RFC-MORPH-*`)

Specs live under **`docs/product-rfcs/`**. IDs remain **`RFC-MORPH-NNNN`** for **stable links** (historical prefix). Content describes **Loopgate** (primary product), **Haven**, sandbox policy, continuity, and **morphlings**.

## Scope

- **Loopgate** (`cmd/loopgate/`) — **authority boundary**: policy, capabilities, approvals, secrets, morphling lifecycle, much of durable memory governance.
- **Haven** — **canonical** operator UI is the **native Swift** app (separate repo). **`cmd/haven/`** in this repository is the **Wails reference** shell only (not shipped product UX).
- **Morphlings** — Loopgate-governed workers (not a public API).

**RFC-MORPH-0001** is Haven client architecture; **RFC-MORPH-0009** is the Loopgate kernel / control-plane architecture.

## Index

| ID | Focus |
|----|--------|
| [RFC-MORPH-0001](./RFC-MORPH-0001:%20Haven%20operator%20client%20architecture.md) | Haven operator client |
| [RFC-MORPH-0002](./RFC-MORPH-0002:%20Morphling%20Task%20Schema.md) | Morphling task schema |
| [RFC-MORPH-0003](./RFC-MORPH-0003:%20Loopgate%20Capability%20Token%20Model.md) | Loopgate capability tokens |
| [RFC-MORPH-0004](./RFC-MORPH-0004:%20Sandbox%20Filesystem%20Policy.md) | Sandbox filesystem (Loopgate-enforced) |
| [RFC-MORPH-0005](./RFC-MORPH-0005:%20Continuity%20and%20Memory%20Model.md) | Continuity & memory (client + Loopgate) |
| [RFC-MORPH-0006](./RFC-MORPH-0006:%20Approval%20&%20Promotion%20Flow.md) | Approval & promotion |
| [RFC-MORPH-0007](./RFC-MORPH-0007:%20Sandbox%20&%20Morphling%20Implementation%20Plan.md) | Implementation sequencing |
| [RFC-MORPH-0008](./RFC-MORPH-0008:%20Morphling%20Class%20Schema%20and%20Lifecycle%20State%20Machine.md) | Morphling classes & lifecycle |
| [RFC-MORPH-0009](./RFC-MORPH-0009:%20Loopgate%20control%20plane%20architecture.md) | Loopgate control plane (kernel) |

Numbered transport/policy RFCs under [`docs/rfcs/`](../rfcs/) complement this set.
