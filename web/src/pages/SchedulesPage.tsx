import { useEffect, useState } from "react";
import { CalendarPlus, Pause, Pencil, Play, Trash2 } from "lucide-react";
import {
  ApiError,
  createSchedule,
  deleteSchedule,
  listSchedules,
  listWorkflows,
  setSchedulePaused,
  updateSchedule
} from "../api";
import { EmptyState, ErrorNotice, PageHeader, RefreshButton } from "../components";
import { formatDate } from "../format";
import type { Schedule, WorkflowDefinition } from "../types";

export function SchedulesPage() {
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [editing, setEditing] = useState<Schedule | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    let active = true;
    Promise.all([listSchedules(), listWorkflows()])
      .then(([scheduleResponse, workflowResponse]) => {
        if (!active) return;
        setSchedules(scheduleResponse.schedules);
        setWorkflows(workflowResponse.workflows);
        setError(null);
      })
      .catch((cause) => {
        if (active) setError(toApiError(cause));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [refreshKey]);

  async function toggle(item: Schedule) {
    setBusy(item.id);
    try {
      await setSchedulePaused(item.id, !item.paused);
      setSchedules((current) =>
        current.map((value) => value.id === item.id ? { ...value, paused: !value.paused } : value)
      );
      setError(null);
    } catch (cause) {
      setError(toApiError(cause));
    } finally {
      setBusy("");
    }
  }

  async function remove(item: Schedule) {
    setBusy(item.id);
    try {
      await deleteSchedule(item.id);
      setSchedules((current) => current.filter((value) => value.id !== item.id));
      setError(null);
    } catch (cause) {
      setError(toApiError(cause));
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="Schedules"
        title="定时任务"
        actions={
          <RefreshButton loading={loading} onClick={() => setRefreshKey((value) => value + 1)} />
        }
      />
      {error && <ErrorNotice message={error.message} requestId={error.requestId} />}

      <div className="schedule-layout">
        <section>
          {!loading && schedules.length === 0 ? (
            <EmptyState title="还没有定时任务" detail="在右侧创建一个 Cron 定时任务。" />
          ) : (
            <div className="schedule-list">
              {schedules.map((item) => (
                <article className="schedule-card" key={item.id}>
                  <div className="schedule-main">
                    <div>
                      <span className={`status-badge ${item.paused ? "neutral" : "active"}`}>
                        <span className="status-dot" />
                        {item.paused ? "已暂停" : "运行中"}
                      </span>
                      {item.needsReview && <span className="status-badge danger"><span className="status-dot" />需复核</span>}
                      <h2>{item.name || item.id}</h2>
                      <code>{item.definitionId}</code>
                    </div>
                    <div className="schedule-time">
                      <span>{item.cron}</span>
                      <small>{item.timezone}</small>
                    </div>
                  </div>
                  <div className="schedule-meta">
                    <span>上次：{formatDate(item.lastActionTime)}</span>
                    <span>下次：{formatDate(item.nextActionTime)}</span>
                  </div>
                  <div className="schedule-actions">
                    <button
                      className="secondary-button"
                      type="button"
                      disabled={busy === item.id}
                      onClick={() => setEditing(item)}
                    >
                      <Pencil size={15} />
                      编辑
                    </button>
                    <button
                      className="secondary-button"
                      type="button"
                      disabled={busy === item.id}
                      onClick={() => void toggle(item)}
                    >
                      {item.paused ? <Play size={15} /> : <Pause size={15} />}
                      {item.paused ? "恢复" : "暂停"}
                    </button>
                    <button
                      className="danger-button"
                      type="button"
                      disabled={busy === item.id}
                      onClick={() => void remove(item)}
                    >
                      <Trash2 size={15} />
                      删除
                    </button>
                  </div>
                </article>
              ))}
            </div>
          )}
        </section>
        <CreateScheduleForm
          workflows={workflows}
          editing={editing}
          onCancelEdit={() => setEditing(null)}
          onSaved={(schedule) => {
            setEditing(null);
            if (schedule) {
              setSchedules((current) =>
                current.some((value) => value.id === schedule.id)
                  ? current.map((value) => value.id === schedule.id ? schedule : value)
                  : [schedule, ...current]
              );
            } else {
              setRefreshKey((value) => value + 1);
            }
          }}
        />
      </div>
    </div>
  );
}

function CreateScheduleForm({
  workflows,
  editing,
  onCancelEdit,
  onSaved
}: {
  workflows: WorkflowDefinition[];
  editing: Schedule | null;
  onCancelEdit: () => void;
  onSaved: (schedule: Schedule | null) => void;
}) {
  const [workflowId, setWorkflowId] = useState("");
  const [name, setName] = useState("");
  const [cron, setCron] = useState("0 9 * * *");
  const [timezone, setTimezone] = useState("Asia/Shanghai");
  const [source, setSource] = useState("{}");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  useEffect(() => {
    if (editing) {
      setWorkflowId(editing.definitionId);
      setName(editing.name);
      setCron(editing.cron);
      setTimezone(editing.timezone);
      setSource(JSON.stringify(workflows.find((value) => value.id === editing.definitionId)?.exampleInput ?? {}, null, 2));
    } else if (!workflowId && workflows[0]) {
      setWorkflowId(workflows[0].id);
    }
  }, [editing, workflowId, workflows]);

  useEffect(() => {
    const selected = workflows.find((value) => value.id === workflowId);
    if (selected) setSource(JSON.stringify(selected.exampleInput ?? {}, null, 2));
  }, [workflowId, workflows]);

  async function submit() {
    let input: unknown;
    try {
      input = JSON.parse(source);
    } catch {
      setError(new ApiError(400, "输入不是合法 JSON"));
      return;
    }
    setSubmitting(true);
    try {
      if (editing) {
        await updateSchedule(editing.id, {
          name: name || editing.name,
          input,
          cron,
          timezone,
          paused: editing.paused
        });
        onSaved(null);
      } else {
        const schedule = await createSchedule({
          name: name || workflowId,
          workflowId,
          input,
          cron,
          timezone,
          paused: false
        });
        onSaved(schedule);
      }
      setError(null);
    } catch (cause) {
      setError(toApiError(cause));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <aside className="start-panel schedule-form">
      <header>
        <div>
          <span className="eyebrow">{editing ? "编辑任务" : "新建任务"}</span>
          <h2>{editing ? editing.name : "定时执行"}</h2>
        </div>
        <CalendarPlus size={20} />
      </header>
      <label className="field-label">工作流</label>
      <select
        value={workflowId}
        disabled={Boolean(editing)}
        onChange={(event) => setWorkflowId(event.target.value)}
      >
        {workflows.map((workflow) => (
          <option key={workflow.id} value={workflow.id}>{workflow.displayName}</option>
        ))}
      </select>
      <label className="field-label">名称</label>
      <input value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：每日问候" />
      <div className="form-pair">
        <label>
          <span className="field-label">Cron</span>
          <input value={cron} onChange={(event) => setCron(event.target.value)} />
        </label>
        <label>
          <span className="field-label">时区</span>
          <input value={timezone} onChange={(event) => setTimezone(event.target.value)} />
        </label>
      </div>
      <label className="field-label">输入 JSON</label>
      <textarea className="code-input schedule-input" value={source} onChange={(event) => setSource(event.target.value)} />
      {error && <ErrorNotice message={error.message} requestId={error.requestId} />}
      <button className="primary-button schedule-submit" type="button" onClick={() => void submit()} disabled={submitting}>
        <CalendarPlus size={16} />
        {submitting ? "正在保存" : editing ? "保存修改" : "创建定时任务"}
      </button>
      {editing && (
        <button className="secondary-button schedule-submit" type="button" onClick={onCancelEdit}>
          取消编辑
        </button>
      )}
    </aside>
  );
}

function toApiError(cause: unknown): ApiError {
  if (cause instanceof ApiError) return cause;
  return new ApiError(0, cause instanceof Error ? cause.message : "无法连接服务");
}
