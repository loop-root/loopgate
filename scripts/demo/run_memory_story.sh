#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(pwd)}"
OUTPUT_ROOT="${OUTPUT_ROOT:-/tmp/loopgate-demo-runs}"
RUN_STAMP="${RUN_STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
QDRANT_URL="${QDRANT_URL:-http://127.0.0.1:6333}"
GOCACHE_DIR="${GOCACHE_DIR:-$REPO_ROOT/.cache/go-build}"
MEMBENCH_ROOT="${MEMBENCH_ROOT:-$HOME/Dev/memBench/snapshot/loopgate_pre_split}"

mkdir -p "$OUTPUT_ROOT"

if [ ! -f "$MEMBENCH_ROOT/go.mod" ]; then
  echo "memorybench snapshot not found at: $MEMBENCH_ROOT" >&2
  echo "Set MEMBENCH_ROOT to the extracted memBench snapshot root before running this demo." >&2
  exit 1
fi

run_memorybench() {
  (
    cd "$MEMBENCH_ROOT"
    env GOCACHE="$GOCACHE_DIR" go run ./cmd/memorybench "$@"
  )
}

echo "Running Loopgate continuity demo scenarios..."
run_memorybench \
  -output-root "$OUTPUT_ROOT" \
  -run-id "continuity_demo_task_resumption_${RUN_STAMP}" \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root "$REPO_ROOT" \
  -continuity-seeding-mode production_write_parity \
  -continuity-benchmark-local-slot-preference=false \
  -scenario-set demo_task_resumption

run_memorybench \
  -output-root "$OUTPUT_ROOT" \
  -run-id "continuity_demo_slot_truth_${RUN_STAMP}" \
  -profile fixtures \
  -backend continuity_tcl \
  -repo-root "$REPO_ROOT" \
  -continuity-seeding-mode production_write_parity \
  -continuity-benchmark-local-slot-preference=false \
  -scenario-set demo_slot_truth

echo "Running RAG comparison demo scenarios..."
run_memorybench \
  -output-root "$OUTPUT_ROOT" \
  -run-id "rag_demo_task_resumption_${RUN_STAMP}" \
  -profile fixtures \
  -backend rag_stronger \
  -repo-root "$REPO_ROOT" \
  -rag-qdrant-url "$QDRANT_URL" \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures \
  -scenario-set demo_task_resumption

run_memorybench \
  -output-root "$OUTPUT_ROOT" \
  -run-id "rag_demo_slot_truth_${RUN_STAMP}" \
  -profile fixtures \
  -backend rag_stronger \
  -repo-root "$REPO_ROOT" \
  -rag-qdrant-url "$QDRANT_URL" \
  -rag-collection memorybench_rerank \
  -rag-seed-fixtures \
  -scenario-set demo_slot_truth

echo
echo "Demo artifacts written under: $OUTPUT_ROOT"
