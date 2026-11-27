import { useEffect, useMemo, useState } from "react";
import { ChevronRight, Code2, Play, Search, WandSparkles } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { ApiError, listWorkflows, newIdempotencyKey, startRun } from "../api";
import { EmptyState, ErrorNotice, PageHeader } from "../components";
import type { WorkflowDefinition } from "../types";

export function WorkflowsPage() {
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([]);
  const [selected, setSelected] = useState<WorkflowDefinition | null>(null);
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<ApiError | null>(null);

  useEffect(() => {
    void listWorkflows()
      .then((response) => {
        setWorkflows(response.workflows);
        setSelected(response.workflows[0] ?? null);
        setError(null);
      })
      .catch((cause) => setError(toApiError(cause)))
      .finally(() => setLoading(false));
  }, []);

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return workflows;
    return workflows.filter((workflow) =>
      `${workflow.id} ${workflow.displayName} ${workflow.description}`.toLowerCase().includes(query)
    );
  }, [search, workflows]);

  return (
    <div className="page">
      <PageHeader eyebrow="Catalog" title="工作流" />
      {error && <ErrorNotice message={error.message} requestId={error.requestId} />}

      <div className="catalog-layout">
        <section className="catalog-list">
          <label className="search-box">
            <Search size={16} />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="搜索工作流"
              aria-label="搜索工作流"
            />
          </label>

          {!loading && filtered.length === 0 ? (
            <EmptyState title="没有匹配的工作流" detail="检查注册代码或调整搜索关键词。" />
          ) : (
            <div className="workflow-list">
              {filtered.map((workflow) => (
                <button
                  className={`workflow-row ${selected?.id === workflow.id ? "selected" : ""}`}
                  key={workflow.id}
                  type="button"
                  onClick={() => setSelected(workflow)}
                >
                  <div>
                    <strong>{workflow.displayName}</strong>
                    <span>{workflow.description || "暂无说明"}</span>
                    <code>{workflow.id}</code>
                  </div>
                  <ChevronRight size={17} />
                </button>
              ))}
            </div>
          )}
        </section>

        <WorkflowStartPanel workflow={selected} />
      </div>
    </div>
  );
}

function WorkflowStartPanel({ workflow }: { workflow: WorkflowDefinition | null }) {
  const navigate = useNavigate();
  const [source, setSource] = useState("");
  const [idempotencyKey, setIdempotencyKey] = useState(newIdempotencyKey);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  useEffect(() => {
    if (!workflow) {
      setSource("");
      return;
    }
    const storageKey = `workflow-input:${workflow.id}`;
    const recent = window.localStorage.getItem(storageKey);
    setSource(recent ?? JSON.stringify(workflow.exampleInput ?? {}, null, 2));
    setIdempotencyKey(newIdempotencyKey());
    setError(null);
  }, [workflow]);

  if (!workflow) {
    return (
      <aside className="start-panel start-panel-empty">
        <Code2 size={24} />
        <strong>选择一个工作流</strong>
        <p>查看输入结构并创建运行。</p>
      </aside>
    );
  }
  const selectedWorkflow = workflow;

  async function submit() {
    let input: unknown;
    try {
      input = JSON.parse(source);
    } catch {
      setError(new ApiError(400, "输入不是合法 JSON"));
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const response = await startRun(selectedWorkflow.id, input, idempotencyKey);
      window.localStorage.setItem(`workflow-input:${selectedWorkflow.id}`, source);
      navigate(
        `/runs/${encodeURIComponent(response.workflowId)}/${encodeURIComponent(response.runId)}`
      );
    } catch (cause) {
      setError(toApiError(cause));
    } finally {
      setSubmitting(false);
    }
  }

  function useExample() {
    setSource(JSON.stringify(selectedWorkflow.exampleInput ?? {}, null, 2));
    setIdempotencyKey(newIdempotencyKey());
    setError(null);
  }

  return (
    <aside className="start-panel">
      <header>
        <div>
          <span className="eyebrow">启动运行</span>
          <h2>{selectedWorkflow.displayName}</h2>
        </div>
        <code>{selectedWorkflow.id}</code>
      </header>
      <p className="panel-description">{selectedWorkflow.description || "暂无说明"}</p>

      <label className="field-label" htmlFor="workflow-input">JSON 输入</label>
      <textarea
        id="workflow-input"
        className="code-input"
        value={source}
        onChange={(event) => setSource(event.target.value)}
        spellCheck={false}
      />

      <details className="schema-details">
        <summary>输入 Schema</summary>
        <pre>{JSON.stringify(selectedWorkflow.inputSchema, null, 2)}</pre>
      </details>

      <div className="idempotency-row">
        <div>
          <span>幂等键</span>
          <code>{idempotencyKey}</code>
        </div>
        <WandSparkles size={16} />
      </div>

      {error && <ErrorNotice message={error.message} requestId={error.requestId} />}

      <div className="panel-actions">
        <button className="secondary-button" type="button" onClick={useExample}>
          使用示例
        </button>
        <button className="primary-button" type="button" onClick={submit} disabled={submitting}>
          <Play size={16} />
          {submitting ? "正在启动" : "启动运行"}
        </button>
      </div>
    </aside>
  );
}

function toApiError(cause: unknown): ApiError {
  if (cause instanceof ApiError) return cause;
  return new ApiError(0, cause instanceof Error ? cause.message : "无法连接服务");
}
