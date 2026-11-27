import { useEffect, useState } from "react";
import { ArrowLeft, Ban, ExternalLink, RefreshCw } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { ApiError, cancelRun, getRun, sendSignal } from "../api";
import { ErrorNotice, StatusBadge } from "../components";
import { formatDate, formatDuration } from "../format";
import type { RunDetail } from "../types";

export function RunDetailPage() {
  const { workflowId = "", runId = "" } = useParams();
  const [run, setRun] = useState<RunDetail | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);
  const [canceling, setCanceling] = useState(false);
  const [signalName, setSignalName] = useState("");
  const [signalInput, setSignalInput] = useState("{}");
  const [signaling, setSignaling] = useState(false);

  useEffect(() => {
    let active = true;
    async function load() {
      try {
        const value = await getRun(workflowId, runId);
        if (active) {
          setRun(value);
          setError(null);
        }
      } catch (cause) {
        if (active) setError(toApiError(cause));
      } finally {
        if (active) setLoading(false);
      }
    }

    void load();
    const timer = window.setInterval(load, 3_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [workflowId, runId]);

  async function cancel() {
    setCanceling(true);
    try {
      await cancelRun(workflowId, runId);
      setRun(await getRun(workflowId, runId));
      setError(null);
    } catch (cause) {
      setError(toApiError(cause));
    } finally {
      setCanceling(false);
    }
  }

  async function submitSignal() {
    let input: unknown;
    try {
      input = JSON.parse(signalInput);
    } catch {
      setError(new ApiError(400, "Signal 输入不是合法 JSON"));
      return;
    }
    setSignaling(true);
    try {
      await sendSignal(workflowId, runId, signalName, input);
      setError(null);
    } catch (cause) {
      setError(toApiError(cause));
    } finally {
      setSignaling(false);
    }
  }

  return (
    <div className="page">
      <div className="detail-topline">
        <Link className="back-link" to="/runs"><ArrowLeft size={16} />返回运行</Link>
        <div className="page-actions">
          <button
            className="icon-button"
            type="button"
            title="刷新"
            aria-label="刷新"
            onClick={() => void getRun(workflowId, runId).then(setRun)}
          >
            <RefreshCw className={loading ? "spin" : ""} size={17} />
          </button>
          {run?.status === "running" && (
            <button className="danger-button" type="button" onClick={cancel} disabled={canceling}>
              <Ban size={16} />
              {canceling ? "请求中" : "取消运行"}
            </button>
          )}
        </div>
      </div>

      {error && <ErrorNotice message={error.message} requestId={error.requestId} />}
      {run && (
        <>
          <header className="run-heading">
            <div>
              <span className="eyebrow">{run.workflowType}</span>
              <h1>{run.workflowId}</h1>
              <code>{run.runId}</code>
            </div>
            <StatusBadge status={run.status} />
          </header>

          <section className="detail-grid">
            <DetailItem label="开始时间" value={formatDate(run.startTime)} />
            <DetailItem label="结束时间" value={formatDate(run.closeTime)} />
            <DetailItem label="耗时" value={formatDuration(run.startTime, run.closeTime)} />
            <DetailItem label="任务队列" value={run.taskQueue || "—"} />
            <DetailItem label="历史事件" value={String(run.historyLength)} />
          </section>

          <section className="content-section">
            <div className="section-heading">
              <div>
                <span className="eyebrow">Execution</span>
                <h2>进行中的 Activity</h2>
              </div>
            </div>
            {run.pendingActivities?.length ? (
              <div className="table-frame">
                <table>
                  <thead><tr><th>Activity</th><th>类型</th><th>重试</th><th>最近错误</th><th>下次重试</th></tr></thead>
                  <tbody>
                    {run.pendingActivities.map((activity) => (
                      <tr key={activity.activityId}>
                        <td><code>{activity.activityId}</code></td>
                        <td>{activity.activityType}</td>
                        <td>{activity.attempt} / {activity.maximumAttempts || "—"}</td>
                        <td>{activity.lastError || "—"}</td>
                        <td>{formatDate(activity.nextAttemptScheduleTime)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="muted-copy">当前没有 pending Activity。</p>
            )}
          </section>

          <section className="content-section">
            <div className="section-heading">
              <div>
                <span className="eyebrow">Result</span>
                <h2>运行结果</h2>
              </div>
            </div>
            {run.failure ? (
              <ErrorNotice message={run.failure} />
            ) : run.output !== undefined ? (
              <pre className="result-block">{JSON.stringify(run.output, null, 2)}</pre>
            ) : (
              <p className="muted-copy">
                {run.status === "running" ? "Workflow 仍在运行。" : "没有可展示的结果。"}
              </p>
            )}
          </section>

          <section className="content-section">
            <div className="section-heading">
              <div>
                <span className="eyebrow">History</span>
                <h2>时间线摘要</h2>
              </div>
            </div>
            {run.timeline?.length ? (
              <div className="timeline-list">
                {run.timeline.map((event) => (
                  <div className="timeline-item" key={`${event.eventId}:${event.kind}:${event.status}`}>
                    <time>{formatDate(event.time)}</time>
                    <span className={`timeline-kind ${event.kind}`}>{event.kind}</span>
                    <strong>{event.name || event.status}</strong>
                    <span>{event.status}</span>
                  </div>
                ))}
                {run.timelineTruncated && (
                  <p className="muted-copy">时间线已截断，完整历史请前往 Temporal UI。</p>
                )}
              </div>
            ) : (
              <p className="muted-copy">暂无时间线摘要。</p>
            )}
          </section>

          {run.signals?.length ? (
            <section className="content-section">
              <div className="section-heading">
                <div>
                  <span className="eyebrow">Signals</span>
                  <h2>发送 Signal</h2>
                </div>
              </div>
              <div className="signal-panel">
                <select
                  value={signalName || run.signals[0].Name}
                  onChange={(event) => setSignalName(event.target.value)}
                >
                  {run.signals.map((signal) => (
                    <option key={signal.Name} value={signal.Name}>{signal.Name}</option>
                  ))}
                </select>
                <textarea
                  className="code-input signal-input"
                  value={signalInput}
                  onChange={(event) => setSignalInput(event.target.value)}
                />
                <button
                  className="primary-button"
                  type="button"
                  disabled={signaling}
                  onClick={() => void submitSignal()}
                >
                  {signaling ? "正在发送" : "发送 Signal"}
                </button>
              </div>
            </section>
          ) : null}

          <section className="debug-strip">
            <div>
              <strong>需要查看完整事件历史？</strong>
              <p>堆栈、重试和原始 Payload 以 Temporal UI 为准。</p>
            </div>
            {run.temporalUiUrl ? (
              <a className="secondary-button" href={run.temporalUiUrl} target="_blank" rel="noreferrer">
                在 Temporal UI 中查看 <ExternalLink size={15} />
              </a>
            ) : (
              <span className="muted-copy">Temporal UI 链接未配置</span>
            )}
          </section>
        </>
      )}
    </div>
  );
}

function DetailItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="detail-item">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function toApiError(cause: unknown): ApiError {
  if (cause instanceof ApiError) return cause;
  return new ApiError(0, cause instanceof Error ? cause.message : "无法连接服务");
}
