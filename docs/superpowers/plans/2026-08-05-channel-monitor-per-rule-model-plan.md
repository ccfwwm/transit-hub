# Channel Monitor Per-Rule Test Model Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let each monitored real connection use a custom test model while blank values continue inheriting the workspace model.

**Architecture:** Add a nullable `test_model_id` override to `channel_monitor_rules`. Resolve the effective model in one backend helper used by summary and scheduled/manual checks; expose persisted and effective values in `ChannelStatus`. Add single-rule inherit/custom controls to the existing editor without changing the workspace defaults or bulk rule behavior.

**Tech Stack:** Go, PostgreSQL migrations performed by idempotent `EnsureSchema`, Vue 3, TypeScript, existing channel monitor API and tests.

---

### Task 1: Add rule model override domain and persistence

**Files:**
- Modify: `backend/internal/modules/channel_monitor/types.go`
- Modify: `backend/internal/modules/channel_monitor/repository.go`
- Test: `backend/internal/modules/channel_monitor/service_test.go`

- [ ] **Step 1: Write failing service tests**
  - Add tests proving a blank rule inherits the OpenAI/Anthropic/Grok workspace model.
  - Add a test proving a non-empty rule override wins over the saved workspace model.
  - Add a test proving clearing the override restores inheritance.

- [ ] **Step 2: Run the focused tests and verify they fail**
  - Run `go test ./internal/modules/channel_monitor -run 'TestRunRuleUses|TestSummary' -count=1` from `backend`.
  - Expected failure: `Rule` has no per-rule model field and the custom model is not passed to the platform fake.

- [ ] **Step 3: Implement the domain and schema changes**
  - Add `TestModelID string` to `Rule` with JSON omitted from the rule itself if it is only internal.
  - Add `TestModelID`, `EffectiveTestModelID`, and `TestModelSource` to `ChannelStatus`.
  - Add nullable `test_model_id` to the create table and an idempotent `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` migration.
  - Add the column to all rule `SELECT`, `INSERT`, `UPDATE`, and scan paths; existing rows remain blank and inherit defaults.

- [ ] **Step 4: Run focused Go tests**
  - Run `go test ./internal/modules/channel_monitor -count=1`.
  - Expected: PASS.

- [ ] **Step 5: Commit the backend model/persistence slice**
  - Run `git add backend/internal/modules/channel_monitor` and commit with `feat: add per-rule monitor model override`.

### Task 2: Add API semantics and one effective-model resolver

**Files:**
- Modify: `backend/internal/modules/channel_monitor/types.go`
- Modify: `backend/internal/modules/channel_monitor/service.go`
- Modify: `backend/internal/modules/channel_monitor/handler.go`
- Modify: `frontend/src/modules/admin/types/channelMonitor.ts`
- Modify: `frontend/src/modules/admin/api/channelMonitor.ts`
- Test: `backend/internal/modules/channel_monitor/service_test.go`

- [ ] **Step 1: Extend the failing tests for resolution and clearing**
  - Assert summary returns `testModelSource=global` for blank overrides.
  - Set a rule override, assert summary returns `custom` and the effective value.
  - Send an empty `testModelId` update and assert the stored override is blank.

- [ ] **Step 2: Implement PATCH request semantics**
  - Add `TestModelID *string` to `UpdateRuleRequest`; omitted means unchanged, empty means clear, non-empty means trimmed custom value.
  - Keep `BulkUpdateRuleRequest` unchanged so bulk threshold/interval changes cannot overwrite individual model choices.
  - Validate and clamp the existing rule values using current logic.

- [ ] **Step 3: Centralize effective model resolution**
  - Add a helper that returns `(effectiveModelID, source)` using the rule override first and `testModelForGroupType` second.
  - Use the helper in `RunRule` and `channelStatus` so manual checks, scheduler checks, and displayed values cannot diverge.

- [ ] **Step 4: Update TypeScript contracts and API wrapper**
  - Add the three channel response fields and optional `testModelId` to the update request type.

- [ ] **Step 5: Run backend and frontend type checks**
  - Run `go test ./internal/modules/channel_monitor -count=1`.
  - Run `npm run typecheck` from `frontend`.

### Task 3: Add per-channel editor and effective-model display

**Files:**
- Modify: `frontend/src/modules/admin/views/StatusMonitorView.vue`
- Modify: `frontend/src/locales/zh-CN.ts`
- Modify: `frontend/src/locales/en-US.ts`

- [ ] **Step 1: Add editor state and failing typecheck-safe UI bindings**
  - Extend the single-channel editor form with `useDefaultTestModel` and `testModelId`.
  - Initialize it from the channel's persisted override and effective model.
  - Do not add model fields to the bulk editor.

- [ ] **Step 2: Render inherit/custom controls**
  - Add a segmented control or radio pair in the single-rule editor.
  - In inherit mode show the current global model as a disabled reference.
  - In custom mode enable a required text input; trim and reject blank custom values before save.

- [ ] **Step 3: Submit and display the model state**
  - Submit `testModelId: ''` when switching to inherit and the trimmed value when custom.
  - Add a compact effective model/source label in the channel detail cell.
  - Add Chinese and English labels for inherit, custom, effective model, and validation error.

- [ ] **Step 4: Verify frontend**
  - Run `npm run typecheck` and `npm run build`.
  - Inspect the editor with a blank override, custom override, and clear-to-inherit state in the status monitor page.

### Task 4: Full verification and release

**Files:**
- No additional source files expected.

- [ ] **Step 1: Run all automated checks**
  - Run `go test ./... -count=1` from `backend`.
  - Run `npm run typecheck` and `npm run build` from `frontend`.
  - Run `git diff --check` and scan the diff for unrelated or sensitive changes.

- [ ] **Step 2: Verify migration and live API behavior**
  - Start the app against the existing database and load `/api/channel-monitor/summary`.
  - Confirm old rules return global inheritance and a custom PATCH affects only the selected rule.

- [ ] **Step 3: Commit and push**
  - Commit the implementation with a focused message, push `main`, and verify the remote commit.

- [ ] **Step 4: Deploy and smoke test production**
  - Rebuild only the TransitHub app service using the existing production Compose file.
  - Confirm PostgreSQL and Redis remain running, health is 200, and the status monitor UI shows effective models.
