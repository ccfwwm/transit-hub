# Channel Monitor Per-Rule Test Model Design

## Goal

Allow every real connection monitored by `channel_monitor` to override its test model while preserving the existing workspace-level OpenAI, Anthropic, and Grok defaults.

## Behavior

- Every monitor rule has an optional `test_model_id` override.
- A blank override inherits the workspace model selected from the connection group type.
- A non-blank override is used for both scheduled checks and manual "run now" checks.
- Clearing an override immediately restores inheritance without copying the current workspace default into the rule.
- Model precedence is: per-rule override, workspace type-specific model, built-in default.
- Existing rules have no override after migration and therefore keep their current behavior.

## Backend Design

Add a nullable `test_model_id` column to `channel_monitor_rules` through the module's idempotent `EnsureSchema` migration. Add `TestModelID` to `Rule` and carry it through every rule query and update.

`UpdateRuleRequest.testModelId` is an optional string pointer. An omitted JSON property leaves the rule unchanged; an empty string clears the override; a non-empty value is trimmed and persisted. Bulk rule updates do not expose this field in v1 so a bulk interval or threshold edit cannot overwrite individual models.

Summary channels expose:

- `testModelId`: persisted override, blank when inheriting.
- `effectiveTestModelId`: model that the next check will use.
- `testModelSource`: `custom` or `global`.

A single helper resolves the effective model and is used by both summary construction and `RunRule`, preventing UI and scheduler drift.

## Frontend Design

The single-channel rule editor adds an inherit/custom segmented choice and a model ID input. In inherit mode the input is disabled and shows the currently effective workspace model. Switching back to inherit submits an empty `testModelId` to clear the override.

The channel table displays the effective model with a compact `统一` or `自定义` marker. The existing workspace model dialog remains unchanged and continues to control all inheriting rules. The bulk rule editor does not show a model control.

## Validation And Errors

- Whitespace-only custom values are treated as clearing the override.
- The frontend prevents saving custom mode with an empty model.
- The backend trims model IDs and remains authoritative if clients bypass the UI.
- Unsupported channels may store an override, but it has no effect until active monitoring is supported.

## Tests

- Default rules inherit the correct OpenAI, Anthropic, or Grok workspace model.
- A rule override wins over the workspace model in `RunRule`.
- Clearing the override restores workspace inheritance.
- Summary reports override, effective value, and source consistently.
- Existing schema and repository scans remain compatible after adding the nullable column.
- Frontend typecheck and production build pass; the editor and table states are verified in production.
