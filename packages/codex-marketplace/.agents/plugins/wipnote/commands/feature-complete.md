# /wipnote:feature-complete

Mark a feature as complete

## Usage

```
/wipnote:feature-complete [feature-id]
```

## Parameters

- `feature-id` (optional): The feature ID to complete. If not provided, completes the current active feature.


## Examples

```bash
/wipnote:feature-complete feature-001
```
Complete a specific feature

```bash
/wipnote:feature-complete
```
Complete the current active feature



## Instructions for Claude

### Implementation:

**DO THIS:**

1. **Get current feature if not specified:**
   ```bash
   erinn find features --status in-progress
   ```
   If no feature_id given, use the first in-progress feature from the list.

2. **Complete the feature:**
   ```bash
   erinn feature complete {feature_id}
   ```

3. **Get updated project status:**
   ```bash
   erinn status
   ```

4. **Present summary** using the output template below.

5. **Recommend next steps:**
   ```bash
   erinn analytics summary
   ```
   - If pending features exist → Suggest starting the next feature
   - If all features done → Congratulate on completion
   - Offer to run `/wipnote:plan` for new work

### Output Format:

## Feature Completed

**ID:** {feature_id}
**Title:** {title}
**Status:** done

### Progress Update
{progress from `erinn status` output}

### What's Next?
{top recommendation from `erinn analytics summary`}

**DELEGATION**: Delegate implementation based on complexity:
- Simple fixes (1-2 files) → `Task(subagent_type="wipnote:haiku-coder")`
- Features (3-8 files) → `Task(subagent_type="wipnote:sonnet-coder")`
- Architecture (10+ files) → `Task(subagent_type="wipnote:opus-coder")`
