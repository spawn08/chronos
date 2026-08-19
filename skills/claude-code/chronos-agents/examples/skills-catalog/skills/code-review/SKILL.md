---
name: code-review
version: 1.0.0
description: Review code for correctness, security, performance, and maintainability.
author: chronos-examples
tags: [code-review, quality]
tools: [file_read, file_grep, shell]
---

# Code Review Skill

## When to use
Activate this skill when:
- Reviewing a pull request or code diff
- Checking code for bugs, vulnerabilities, or anti-patterns
- Evaluating code quality before merge

## Review checklist
1. **Correctness**: Does the code do what it claims?
2. **Security**: SQL injection, XSS, command injection, hardcoded secrets
3. **Error handling**: Are errors caught, wrapped with context, and propagated?
4. **Edge cases**: Nil pointers, empty inputs, boundary conditions
5. **Performance**: N+1 queries, unbounded allocations, missing indexes
6. **Naming**: Are variables, functions, and types clearly named?
7. **Tests**: Are critical paths covered? Are edge cases tested?

## Output format
For each finding:
- **File:Line** — location
- **Severity** — critical / warning / suggestion
- **Issue** — what's wrong
- **Fix** — how to fix it
