---
name: ado-wiql
version: 1.0.0
description: Count and filter Azure DevOps work items with WIQL.
author: chronos-examples
tags: [azure-devops, research]
tools:
  - wit_query_by_wiql
  - wit_get_work_items_batch_by_ids
---

# When to use

Use this skill for any question about ADO bugs, tasks, features, or user
stories — counts, status distributions, "what's assigned to X", "how many
are open in project Y". Prefer it over local codebase greps whenever the
question mentions Azure DevOps, a project name, work items, or bug counts.

# How to use

1. Call `wit_query_by_wiql` exactly once with a WIQL query. Substitute the
   user's project name verbatim into `[System.TeamProject]`.
2. Read `result.workItems` from the response — its length is the count.
3. If the user wants details (titles, assignees, states), call
   `wit_get_work_items_batch_by_ids` on the returned IDs and summarize.

## Open-bug count template

```
SELECT [System.Id] FROM WorkItems
WHERE [System.TeamProject] = '<project>'
  AND [System.WorkItemType] = 'Bug'
  AND [System.State] NOT IN ('Closed', 'Resolved', 'Done', 'Removed')
```

## Reporting

Report: the count, the exact WIQL you ran, and the project you used. If
the tool call errors (auth failure, unknown project), surface the raw
error — do not fall back to reading local files.
