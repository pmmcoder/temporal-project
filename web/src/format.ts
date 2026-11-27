import type { RunStatus } from "./types";

export const statusLabel: Record<RunStatus, string> = {
  running: "运行中",
  succeeded: "成功",
  failed: "失败",
  canceled: "已取消",
  terminated: "已终止",
  timed_out: "超时",
  continued: "已续接",
  unknown: "未知"
};

export function formatDate(value?: string): string {
  if (!value) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false
  }).format(new Date(value));
}

export function formatDuration(start?: string, end?: string): string {
  if (!start) return "—";
  const started = new Date(start).getTime();
  const finished = end ? new Date(end).getTime() : Date.now();
  const seconds = Math.max(0, Math.floor((finished - started) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remaining = seconds % 60;
  if (minutes < 60) return `${minutes}m ${remaining}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

export function statusTone(status: RunStatus): string {
  if (status === "running") return "active";
  if (status === "succeeded") return "success";
  if (status === "failed" || status === "timed_out") return "danger";
  return "neutral";
}
