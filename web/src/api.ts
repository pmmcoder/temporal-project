import type {
  ApiErrorBody,
  ApprovalDecision,
  PendingApproval,
  Schedule,
  RunDetail,
  RunPage,
  RunStatus,
  StartRunResponse,
  WorkflowDefinition
} from "./types";

export class ApiError extends Error {
  readonly status: number;
  readonly code?: string;
  readonly requestId?: string;

  constructor(status: number, message: string, code?: string, requestId?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers
    }
  });

  if (!response.ok) {
    let body: ApiErrorBody | undefined;
    try {
      body = (await response.json()) as ApiErrorBody;
    } catch {
      body = undefined;
    }
    throw new ApiError(
      response.status,
      body?.error?.message ?? `请求失败 (${response.status})`,
      body?.error?.code,
      body?.error?.requestId ?? response.headers.get("X-Request-ID") ?? undefined
    );
  }

  return (await response.json()) as T;
}

export function listWorkflows(): Promise<{ workflows: WorkflowDefinition[] }> {
  return request("/api/v1/workflows");
}

export function listRuns(filters: {
  workflow?: string;
  status?: RunStatus | "";
  pageToken?: string;
  from?: string;
  to?: string;
}): Promise<RunPage> {
  const search = new URLSearchParams();
  if (filters.workflow) search.set("workflow", filters.workflow);
  if (filters.status) search.set("status", filters.status);
  if (filters.pageToken) search.set("pageToken", filters.pageToken);
  if (filters.from) search.set("from", filters.from);
  if (filters.to) search.set("to", filters.to);
  const suffix = search.size > 0 ? `?${search.toString()}` : "";
  return request(`/api/v1/runs${suffix}`);
}

export function getRun(workflowId: string, runId: string): Promise<RunDetail> {
  return request(
    `/api/v1/runs/${encodeURIComponent(workflowId)}/${encodeURIComponent(runId)}`
  );
}

export function startRun(
  workflowId: string,
  input: unknown,
  idempotencyKey: string
): Promise<StartRunResponse> {
  return request(`/api/v1/workflows/${encodeURIComponent(workflowId)}/runs`, {
    method: "POST",
    body: JSON.stringify({ input, idempotencyKey })
  });
}

export function cancelRun(workflowId: string, runId: string): Promise<void> {
  return request(
    `/api/v1/runs/${encodeURIComponent(workflowId)}/${encodeURIComponent(runId)}/cancel`,
    { method: "POST", body: "{}" }
  );
}

export function sendSignal(
  workflowId: string,
  runId: string,
  name: string,
  input: unknown
): Promise<void> {
  return request(
    `/api/v1/runs/${encodeURIComponent(workflowId)}/${encodeURIComponent(runId)}/signals/${encodeURIComponent(name)}`,
    {
      method: "POST",
      body: JSON.stringify({ input })
    }
  );
}

export function newIdempotencyKey(): string {
  return crypto.randomUUID().replaceAll("-", "");
}

export function listApprovals(): Promise<{ approvals: PendingApproval[] }> {
  return request("/api/v1/approvals");
}

export function decideApproval(
  workflowId: string,
  runId: string,
  approvalId: string,
  decision: ApprovalDecision,
  comment: string
): Promise<void> {
  return request(
    `/api/v1/approvals/${encodeURIComponent(workflowId)}/${encodeURIComponent(runId)}/${encodeURIComponent(approvalId)}/decision`,
    {
      method: "POST",
      body: JSON.stringify({ decision, comment })
    }
  );
}

export function listSchedules(): Promise<{ schedules: Schedule[] }> {
  return request("/api/v1/schedules");
}

export function createSchedule(input: {
  name: string;
  workflowId: string;
  input: unknown;
  cron: string;
  timezone: string;
  paused: boolean;
}): Promise<Schedule> {
  return request("/api/v1/schedules", {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function setSchedulePaused(id: string, paused: boolean): Promise<void> {
  return request(`/api/v1/schedules/${encodeURIComponent(id)}/${paused ? "pause" : "resume"}`, {
    method: "POST",
    body: "{}"
  });
}

export function updateSchedule(
  id: string,
  input: {
    name: string;
    input: unknown;
    cron: string;
    timezone: string;
    paused: boolean;
  }
): Promise<void> {
  return request(`/api/v1/schedules/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(input)
  });
}

export function deleteSchedule(id: string): Promise<void> {
  return request(`/api/v1/schedules/${encodeURIComponent(id)}`, {
    method: "DELETE",
    body: "{}"
  });
}
