export type RunStatus =
  | "running"
  | "succeeded"
  | "failed"
  | "canceled"
  | "terminated"
  | "timed_out"
  | "continued"
  | "unknown";

export interface WorkflowDefinition {
  id: string;
  displayName: string;
  description: string;
  inputSchema: Record<string, unknown>;
  exampleInput: unknown;
}

export interface RunSummary {
  workflowId: string;
  runId: string;
  workflowType: string;
  status: RunStatus;
  startTime?: string;
  closeTime?: string;
}

export interface RunPage {
  runs: RunSummary[];
  nextPageToken?: string;
}

export interface PendingActivity {
  activityId: string;
  activityType: string;
  attempt: number;
  maximumAttempts: number;
  lastError?: string;
  currentRetryIntervalMs?: number;
  nextAttemptScheduleTime?: string;
}

export interface TimelineEvent {
  time: string;
  kind: string;
  name: string;
  status: string;
  attempt?: number;
  error?: string;
  eventId: number;
}

export interface RunDetail extends RunSummary {
  taskQueue: string;
  historyLength: number;
  pendingActivities: PendingActivity[];
  temporalUiUrl?: string;
  output?: unknown;
  failure?: string;
  signals?: SignalDef[];
  timeline?: TimelineEvent[];
  timelineTruncated: boolean;
}

export interface SignalDef {
  Name: string;
  InputSchema: Record<string, unknown>;
}

export interface StartRunResponse {
  workflowId: string;
  runId: string;
  existing: boolean;
}

export interface ApiErrorBody {
  error?: {
    code?: string;
    message?: string;
    requestId?: string;
  };
}

export interface PendingApproval {
  approvalId: string;
  workflowId: string;
  runId: string;
  workflowType: string;
  title: string;
  summary: string;
  fields?: Array<{ label: string; value: string }>;
  allowComment: boolean;
  createdAt: string;
  deadline: string;
}

export type ApprovalDecision = "approved" | "rejected";

export interface Schedule {
  id: string;
  name: string;
  definitionId: string;
  cron: string;
  timezone: string;
  paused: boolean;
  lastActionTime?: string;
  nextActionTime?: string;
  needsReview: boolean;
}
