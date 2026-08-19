---
name: summarize
version: 1.0.0
description: Summarize documents, conversations, and data concisely.
author: chronos-examples
tags: [nlp, summarization]
tools: [file_read]
---

# Summarization Skill

## When to use
Activate this skill when the user asks you to:
- Summarize a document, article, or conversation
- Create an executive summary or brief
- Condense long text into key points

## Approach
1. Read the full content first
2. Identify the main themes and key points
3. Produce a summary that is:
   - 20-30% of the original length (unless specified)
   - Structured with bullet points for clarity
   - Preserving all critical facts and numbers
   - Written in the same tone as the source

## Output format
- **Short summary**: 2-3 sentences capturing the essence
- **Key points**: Bulleted list of main takeaways
- **Action items**: Any tasks or decisions mentioned (if applicable)
