import type { AutomationSettings } from '../api.generated';
export const defaultAutomation: AutomationSettings = { version: 1, enabled: false, development_tool: 'codex', development_model: 'gpt-6.1-sol', leader_tool: 'codex', leader_model: 'gpt-6.1-sol', planning_timeout_seconds: 300, planning_budget_seconds: 900, planning_max_retries: 2 };
export const emptyPhaseB = { conversations: [], automation: defaultAutomation, planning_document: null, leader_messages: [], planning_runs: [], scope_requests: [], decisions: [], blocks: [] };
