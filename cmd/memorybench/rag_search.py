#!/usr/bin/env python3

import argparse
import json
import os
import sys


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Qdrant helper for memorybench rag_baseline.")
    subparsers = parser.add_subparsers(dest="command", required=False)

    search_parser = subparsers.add_parser("search", help="Run hybrid retrieval against Qdrant.")
    search_parser.add_argument("--qdrant-url", required=True, help="Qdrant base URL, for example http://127.0.0.1:6333")
    search_parser.add_argument("--collection", required=True, help="Qdrant collection name")
    search_parser.add_argument("--query", required=True, help="Search query text")
    search_parser.add_argument("--scope", default="", help="Optional benchmark scope filter")
    search_parser.add_argument("--top-k", type=int, default=5, help="Maximum number of search results")
    search_parser.add_argument("--candidate-pool", type=int, default=0, help="Candidate pool size before reranking")
    search_parser.add_argument("--embedding-model", default="BAAI/bge-small-en-v1.5", help="Dense embedding model name")
    search_parser.add_argument("--sparse-model", default="prithivida/Splade_PP_en_v1", help="Sparse embedding model name")
    search_parser.add_argument("--reranker-model", default="", help="Optional reranker model name")

    seed_parser = subparsers.add_parser("seed", help="Seed Qdrant from JSON corpus on stdin.")
    seed_parser.add_argument("--qdrant-url", required=True, help="Qdrant base URL, for example http://127.0.0.1:6333")
    seed_parser.add_argument("--collection", required=True, help="Qdrant collection name")
    seed_parser.add_argument("--embedding-model", default="BAAI/bge-small-en-v1.5", help="Dense embedding model name")
    seed_parser.add_argument("--sparse-model", default="prithivida/Splade_PP_en_v1", help="Sparse embedding model name")

    parsed_args = parser.parse_args()
    if parsed_args.command is None:
        parsed_args.command = "search"
    return parsed_args


def main() -> int:
    args = parse_args()
    os.environ.setdefault("HAYSTACK_TELEMETRY_ENABLED", "false")

    try:
        from haystack import Document
        from haystack.document_stores.types import DuplicatePolicy
        from haystack_integrations.components.embedders.fastembed import (
            FastembedDocumentEmbedder,
            FastembedTextEmbedder,
            FastembedSparseDocumentEmbedder,
            FastembedSparseTextEmbedder,
        )
        from haystack_integrations.components.rankers.fastembed import FastembedRanker
        from haystack_integrations.components.retrievers.qdrant import QdrantHybridRetriever
        from haystack_integrations.document_stores.qdrant import QdrantDocumentStore
    except Exception as exc:
        print(f"import error: {exc}", file=sys.stderr)
        return 1

    if args.command == "seed":
        try:
            raw_payload = json.load(sys.stdin)
            raw_documents = raw_payload.get("documents") or []
            if not raw_documents:
                raise ValueError("no documents supplied")
            haystack_documents = []
            for raw_document in raw_documents:
                metadata = dict(raw_document.get("metadata") or {})
                metadata.update(
                    {
                        "node_id": str(raw_document.get("document_id") or ""),
                        "node_kind": str(raw_document.get("document_kind") or "rag_chunk"),
                        "scope": str(raw_document.get("scope") or ""),
                        "created_at_utc": str(raw_document.get("created_at_utc") or ""),
                        "exact_signature": str(raw_document.get("exact_signature") or ""),
                        "family_signature": str(raw_document.get("family_signature") or ""),
                        "provenance_ref": str(raw_document.get("provenance_ref") or ""),
                    }
                )
                haystack_documents.append(
                    Document(
                        id=str(raw_document.get("document_id") or ""),
                        content=str(raw_document.get("content") or ""),
                        meta=metadata,
                    )
                )
            dense_document_embedder = FastembedDocumentEmbedder(model=args.embedding_model)
            dense_document_embedder.warm_up()
            haystack_documents = dense_document_embedder.run(documents=haystack_documents)["documents"]
            dense_embedding_dimension = len(haystack_documents[0].embedding or [])
            if dense_embedding_dimension <= 0:
                raise ValueError("dense document embeddings were not produced")

            sparse_document_embedder = FastembedSparseDocumentEmbedder(model=args.sparse_model)
            sparse_document_embedder.warm_up()
            haystack_documents = sparse_document_embedder.run(documents=haystack_documents)["documents"]
            document_store = QdrantDocumentStore(
                url=args.qdrant_url,
                index=args.collection,
                embedding_dim=dense_embedding_dimension,
                use_sparse_embeddings=True,
                recreate_index=True,
            )
            document_store.write_documents(haystack_documents, policy=DuplicatePolicy.OVERWRITE)
        except Exception as exc:
            print(f"seed error: {exc}", file=sys.stderr)
            return 3
        return 0

    try:
        dense_embedder = FastembedTextEmbedder(model=args.embedding_model)
        dense_embedder.warm_up()
        dense_embedding = dense_embedder.run(text=args.query)["embedding"]
        dense_embedding_dimension = len(dense_embedding or [])
        if dense_embedding_dimension <= 0:
            raise ValueError("dense query embedding was not produced")

        sparse_embedder = FastembedSparseTextEmbedder(model=args.sparse_model)
        sparse_embedder.warm_up()
        sparse_embedding = sparse_embedder.run(text=args.query)["sparse_embedding"]
        document_store = QdrantDocumentStore(
            url=args.qdrant_url,
            index=args.collection,
            embedding_dim=dense_embedding_dimension,
            use_sparse_embeddings=True,
        )

        candidate_pool = args.top_k
        if args.reranker_model:
            candidate_pool = max(args.candidate_pool or 0, args.top_k)
            if candidate_pool < args.top_k:
                candidate_pool = args.top_k

        query_filters = None
        if args.scope:
            query_filters = {"field": "meta.scope", "operator": "==", "value": args.scope}
            candidate_pool = max(candidate_pool, 10)

        retriever = QdrantHybridRetriever(document_store=document_store, top_k=candidate_pool)
        retrieved_documents = retriever.run(
            query_embedding=dense_embedding,
            query_sparse_embedding=sparse_embedding,
            filters=query_filters,
            top_k=candidate_pool,
        )["documents"]
        if args.reranker_model:
            ranker = FastembedRanker(model_name=args.reranker_model, top_k=candidate_pool)
            ranker.warm_up()
            retrieved_documents = ranker.run(query=args.query, documents=retrieved_documents, top_k=candidate_pool)["documents"]
    except Exception as exc:
        print(f"search error: {exc}", file=sys.stderr)
        return 2

    retrieved_documents.sort(
        key=lambda document: (
            -(float(document.score or 0.0)),
            str((document.meta or {}).get("node_id") or document.id or ""),
            str((document.meta or {}).get("scope") or ""),
        )
    )
    retrieved_documents = retrieved_documents[: args.top_k]

    results = []
    for document in retrieved_documents:
        metadata = document.meta or {}
        results.append(
            {
                "document_id": str(metadata.get("node_id") or document.id or ""),
                "document_kind": str(metadata.get("node_kind") or "rag_chunk"),
                "scope": str(metadata.get("scope") or ""),
                "created_at_utc": str(metadata.get("created_at_utc") or ""),
                "snippet": document.content or "",
                "exact_signature": str(metadata.get("exact_signature") or ""),
                "family_signature": str(metadata.get("family_signature") or ""),
                "provenance_ref": str(metadata.get("provenance_ref") or ""),
                "score": float(document.score or 0.0),
            }
        )

    json.dump({"results": results}, sys.stdout)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
