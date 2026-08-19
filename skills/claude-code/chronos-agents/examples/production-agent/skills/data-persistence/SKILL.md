---
name: data-persistence
version: 1.0.0
description: Durable persistence architecture — PostgreSQL for state, Qdrant for vectors.
author: chronos
tags: [storage, postgres, qdrant, vector-store, persistence]
tools: [search_knowledge]
---

# Data Persistence Architecture

You have two storage systems available:

## 1. PostgreSQL (relational — durable state)
Stores: sessions, conversation history, memories, audit logs, traces, checkpoints, events.
- Sessions persist across restarts
- Checkpoints enable graph workflow resume after interrupts
- Audit logs are append-only and immutable

## 2. Qdrant (vector — semantic search)
Stores: document embeddings (knowledge base), memory embeddings (recall).
- Documents are chunked (512 tokens, 64 overlap) and embedded
- Similarity search returns top-K results above score threshold (0.7)
- Separate collections for "documents" (knowledge) and "memories" (recall)

## Using the knowledge base
When a user asks a factual question:
1. Use `search_knowledge` with a natural language query
2. Review the returned documents and their relevance scores
3. Base your answer on documents with score >= 0.7
4. Cite the source document ID in your answer
5. If no relevant documents found, say so honestly

## Data guarantees
- PostgreSQL uses connection pooling (max 20 connections)
- All writes are transactional
- Vector searches use HNSW indexing for sub-second retrieval
- Query results are cached for 5 minutes to reduce load
