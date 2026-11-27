import { AlertTriangle, LoaderCircle, RefreshCw } from "lucide-react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { statusLabel, statusTone } from "./format";
import type { RunStatus } from "./types";

export function StatusBadge({ status }: { status: RunStatus }) {
  return (
    <span className={`status-badge ${statusTone(status)}`}>
      <span className="status-dot" />
      {statusLabel[status]}
    </span>
  );
}

export function PageHeader({
  title,
  eyebrow,
  actions
}: {
  title: string;
  eyebrow: string;
  actions?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div>
        <span className="eyebrow">{eyebrow}</span>
        <h1>{title}</h1>
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  );
}

export function IconButton({
  label,
  loading,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  label: string;
  loading?: boolean;
  children: ReactNode;
}) {
  return (
    <button className="icon-button" type="button" title={label} aria-label={label} {...props}>
      {loading ? <LoaderCircle className="spin" size={17} /> : children}
    </button>
  );
}

export function RefreshButton({
  loading,
  onClick
}: {
  loading: boolean;
  onClick: () => void;
}) {
  return (
    <button className="secondary-button" type="button" onClick={onClick} disabled={loading}>
      <RefreshCw className={loading ? "spin" : ""} size={16} />
      刷新
    </button>
  );
}

export function ErrorNotice({
  message,
  requestId
}: {
  message: string;
  requestId?: string;
}) {
  return (
    <div className="error-notice" role="alert">
      <AlertTriangle size={18} />
      <div>
        <strong>操作未完成</strong>
        <p>{message}</p>
        {requestId && <code>request {requestId}</code>}
      </div>
    </div>
  );
}

export function EmptyState({
  title,
  detail
}: {
  title: string;
  detail: string;
}) {
  return (
    <div className="empty-state">
      <div className="empty-line" />
      <strong>{title}</strong>
      <p>{detail}</p>
    </div>
  );
}
