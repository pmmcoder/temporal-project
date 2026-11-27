import { useEffect, useState } from "react";
import { ArrowRight, SlidersHorizontal } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { ApiError, listRuns, listWorkflows } from "../api";
import { EmptyState, ErrorNotice, PageHeader, RefreshButton, StatusBadge } from "../components";
import { formatDate, formatDuration } from "../format";
import type { RunStatus, RunSummary, WorkflowDefinition } from "../types";

const statuses: Array<{ value: RunStatus | ""; label: string }> = [
  { value: "", label: "全部状态" },
  { value: "running", label: "运行中" },
  { value: "succeeded", label: "成功" },
  { value: "failed", label: "失败" },
  { value: "canceled", label: "已取消" },
  { value: "timed_out", label: "超时" }
];

export function RunsPage() {
  const navigate = useNavigate();
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([]);
  const [runs, setRuns] = useState<RunSummary[]>([]);
  const [status, setStatus] = useState<RunStatus | "">("");
  const [workflow, setWorkflow] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [pageToken, setPageToken] = useState("");
  const [previousTokens, setPreviousTokens] = useState<string[]>([]);
  const [nextPageToken, setNextPageToken] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<ApiError | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    void listWorkflows()
      .then((response) => setWorkflows(response.workflows))
      .catch(() => setWorkflows([]));
  }, []);

  useEffect(() => {
    let active = true;

    async function load() {
      try {
        const response = await listRuns({
          status,
          workflow,
          pageToken,
          from: from ? new Date(from).toISOString() : undefined,
          to: to ? new Date(to).toISOString() : undefined
        });
        if (active) {
          setRuns(response.runs);
          setNextPageToken(response.nextPageToken ?? "");
          setError(null);
        }
      } catch (cause) {
        if (active) setError(toApiError(cause));
      } finally {
        if (active) setLoading(false);
      }
    }

    setLoading(true);
    void load();
    const timer = window.setInterval(load, 5_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [status, workflow, from, to, pageToken, refreshKey]);

  function changeWorkflow(value: string) {
    setWorkflow(value);
    setPageToken("");
    setPreviousTokens([]);
  }

  function changeStatus(value: RunStatus | "") {
    setStatus(value);
    setPageToken("");
    setPreviousTokens([]);
  }

  function nextPage() {
    if (!nextPageToken) return;
    setPreviousTokens((tokens) => [...tokens, pageToken]);
    setPageToken(nextPageToken);
  }

  function previousPage() {
    const previous = previousTokens.at(-1);
    if (previous === undefined) return;
    setPreviousTokens((tokens) => tokens.slice(0, -1));
    setPageToken(previous);
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="Operations"
        title="运行"
        actions={
          <RefreshButton loading={loading} onClick={() => setRefreshKey((value) => value + 1)} />
        }
      />

      <section className="toolbar" aria-label="运行筛选">
        <div className="toolbar-label">
          <SlidersHorizontal size={15} />
          筛选
        </div>
        <label>
          <span>工作流</span>
          <select value={workflow} onChange={(event) => changeWorkflow(event.target.value)}>
            <option value="">全部工作流</option>
            {workflows.map((item) => (
              <option key={item.id} value={item.id}>{item.displayName}</option>
            ))}
          </select>
        </label>
        <label>
          <span>状态</span>
          <select
            value={status}
            onChange={(event) => changeStatus(event.target.value as RunStatus | "")}
          >
            {statuses.map((item) => (
              <option key={item.value} value={item.value}>{item.label}</option>
            ))}
          </select>
        </label>
        <label>
          <span>开始时间</span>
          <input
            className="date-filter"
            type="datetime-local"
            value={from}
            onChange={(event) => {
              setFrom(event.target.value);
              setPageToken("");
              setPreviousTokens([]);
            }}
          />
        </label>
        <label>
          <span>结束时间</span>
          <input
            className="date-filter"
            type="datetime-local"
            value={to}
            onChange={(event) => {
              setTo(event.target.value);
              setPageToken("");
              setPreviousTokens([]);
            }}
          />
        </label>
        <span className="result-count">{runs.length} 条运行</span>
      </section>

      {error && <ErrorNotice message={error.message} requestId={error.requestId} />}

      {!error && !loading && runs.length === 0 ? (
        <EmptyState title="没有符合条件的运行" detail="调整筛选条件，或从工作流页面启动一次运行。" />
      ) : (
        <div className="table-frame">
          <table className="run-table">
            <thead>
              <tr>
                <th>状态</th>
                <th>工作流</th>
                <th>Workflow ID</th>
                <th>开始时间</th>
                <th>耗时</th>
                <th aria-label="打开" />
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr
                  key={`${run.workflowId}/${run.runId}`}
                  tabIndex={0}
                  onClick={() => navigate(`/runs/${encodeURIComponent(run.workflowId)}/${encodeURIComponent(run.runId)}`)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      navigate(`/runs/${encodeURIComponent(run.workflowId)}/${encodeURIComponent(run.runId)}`);
                    }
                  }}
                >
                  <td><StatusBadge status={run.status} /></td>
                  <td>
                    <strong className="table-primary">{run.workflowType}</strong>
                    <span className="table-secondary">{run.runId.slice(0, 8)}</span>
                  </td>
                  <td><code className="id-code">{run.workflowId}</code></td>
                  <td>{formatDate(run.startTime)}</td>
                  <td className="mono">{formatDuration(run.startTime, run.closeTime)}</td>
                  <td><Link className="row-link" to={`/runs/${encodeURIComponent(run.workflowId)}/${encodeURIComponent(run.runId)}`} aria-label="查看运行"><ArrowRight size={16} /></Link></td>
                </tr>
              ))}
            </tbody>
          </table>
          <footer className="pagination">
            <span>每页最多 50 条</span>
            <div>
              <button
                className="secondary-button"
                type="button"
                onClick={previousPage}
                disabled={previousTokens.length === 0}
              >
                上一页
              </button>
              <button
                className="secondary-button"
                type="button"
                onClick={nextPage}
                disabled={!nextPageToken}
              >
                下一页
              </button>
            </div>
          </footer>
        </div>
      )}
    </div>
  );
}

function toApiError(cause: unknown): ApiError {
  if (cause instanceof ApiError) return cause;
  return new ApiError(0, cause instanceof Error ? cause.message : "无法连接服务");
}
