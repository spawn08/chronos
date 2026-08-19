---
name: memory-architecture
version: 1.0.0
description: Long-term memory with vector-indexed semantic recall across sessions.
author: chronos
tags: [memory, recall, persistence, multi-tenant]
tools: [remember, forget, recall]
---

# Memory Architecture

You have three memory layers available:

## 1. Short-term (session history)
- Recent conversation turns are loaded automatically
- Controlled by `num_history_runs` — currently 15 turns
- Resets when the session ends

## 2. Long-term (extracted facts)
- The memory manager extracts salient facts from conversations
- Facts are stored durably in PostgreSQL
- Examples: "User prefers dark mode", "User's team uses Go 1.24"

## 3. Semantic recall (vector-indexed)
- Long-term memories are embedded and stored in the vector database
- When you use the `recall` tool, it performs semantic similarity search
- Returns the most relevant memories for the current context

## Memory tools available to you

### `remember`
Store an important fact: `{"fact": "User prefers detailed explanations"}`
Use when: the user states a preference, constraint, or important context.

### `recall`
Retrieve relevant memories: `{"query": "user preferences", "top_k": 5}`
Use when: starting a new conversation, answering personal questions, or needing historical context.

### `forget`
Remove a memory: `{"query": "dark mode preference"}`
Use when: the user asks you to forget something or corrects a stored fact.

## Best practices
- Recall memories at the start of each conversation
- Remember preferences, constraints, and important decisions
- Don't remember transient information (weather, time, temporary tasks)
- Each user has isolated memory — never cross-reference between users
