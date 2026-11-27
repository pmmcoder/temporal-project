import { useEffect, useState } from "react";
import { Check, Clock3, X } from "lucide-react";
import { ApiError, decideApproval, listApprovals } from "../api";
import { EmptyState, ErrorNotice, PageHeader, RefreshButton } from "../components";
import { formatDate } from "../format";
import type { ApprovalDecision, PendingApproval } from "../types";

export function ApprovalsPage() {
  const [items, setItems] = useState<PendingApproval[]>([]);
  const [comments, setComments] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<ApiError | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    let active = true;
    async function load() {
      try {
        const response = await listApprovals();
        if (active) {
          setItems(response.approvals);
          setError(null);
        }
      } catch (cause) {
        if (active) setError(toApiError(cause));
      } finally {
        if (active) setLoading(false);
      }
    }
    void load();
    const timer = window.setInterval(load, 5_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [refreshKey]);

  async function submit(item: PendingApproval, decision: ApprovalDecision) {
    const id = itemKey(item);
    setBusy(id);
    setError(null);
    try {
      await decideApproval(
        item.workflowId,
        item.runId,
        item.approvalId,
        decision,
        comments[id] ?? ""
      );
      setItems((current) => current.filter((value) => itemKey(value) !== id));
    } catch (cause) {
      setError(toApiError(cause));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="Approvals"
        title="待办"
        actions={
          <RefreshButton loading={loading} onClick={() => setRefreshKey((value) => value + 1)} />
        }
      />
      {error && <ErrorNotice message={error.message} requestId={error.requestId} />}

      {!error && !loading && items.length === 0 ? (
        <EmptyState title="没有等待处理的审批" detail="Workflow 发起人工确认后会出现在这里。" />
      ) : (
        <div className="approval-list">
          {items.map((item) => {
            const id = itemKey(item);
            return (
              <article className="approval-card" key={id}>
                <div className="approval-head">
                  <div>
                    <span className="eyebrow">{item.workflowType}</span>
                    <h2>{item.title}</h2>
                    <code>{item.workflowId}</code>
                  </div>
                  <div className="deadline-chip">
                    <Clock3 size={14} />
                    截止 {formatDate(item.deadline)}
                  </div>
                </div>
                <p className="approval-summary">{item.summary}</p>
                {item.fields?.length ? (
                  <dl className="approval-fields">
                    {item.fields.map((field) => (
                      <div key={`${field.label}:${field.value}`}>
                        <dt>{field.label}</dt>
                        <dd>{field.value}</dd>
                      </div>
                    ))}
                  </dl>
                ) : null}
                {item.allowComment && (
                  <textarea
                    className="approval-comment"
                    value={comments[id] ?? ""}
                    onChange={(event) =>
                      setComments((current) => ({ ...current, [id]: event.target.value }))
                    }
                    placeholder="填写审批备注"
                  />
                )}
                <div className="approval-actions">
                  <button
                    className="danger-button"
                    type="button"
                    disabled={busy === id}
                    onClick={() => void submit(item, "rejected")}
                  >
                    <X size={16} />
                    拒绝
                  </button>
                  <button
                    className="primary-button"
                    type="button"
                    disabled={busy === id}
                    onClick={() => void submit(item, "approved")}
                  >
                    <Check size={16} />
                    通过
                  </button>
                </div>
              </article>
            );
          })}
        </div>
      )}
    </div>
  );
}

function itemKey(item: PendingApproval): string {
  return `${item.workflowId}/${item.runId}/${item.approvalId}`;
}

function toApiError(cause: unknown): ApiError {
  if (cause instanceof ApiError) return cause;
  return new ApiError(0, cause instanceof Error ? cause.message : "无法连接服务");
}
