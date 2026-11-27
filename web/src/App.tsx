import { useEffect, useState } from "react";
import { Activity, Boxes, CalendarClock, ClipboardCheck, LayoutList, Server } from "lucide-react";
import { NavLink, Navigate, Route, Routes } from "react-router-dom";
import { RunDetailPage } from "./pages/RunDetailPage";
import { RunsPage } from "./pages/RunsPage";
import { WorkflowsPage } from "./pages/WorkflowsPage";
import { ApprovalsPage } from "./pages/ApprovalsPage";
import { SchedulesPage } from "./pages/SchedulesPage";

export default function App() {
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    let active = true;

    async function checkHealth() {
      try {
        const response = await fetch("/api/v1/healthz");
        if (active) setConnected(response.ok);
      } catch {
        if (active) setConnected(false);
      }
    }

    void checkHealth();
    const timer = window.setInterval(checkHealth, 15_000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, []);

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark"><Activity size={19} /></div>
          <div>
            <strong>Flowstation</strong>
            <span>本地工作流平台</span>
          </div>
        </div>

        <nav className="nav" aria-label="主导航">
          <NavLink to="/runs">
            <LayoutList size={17} />
            运行
          </NavLink>
          <NavLink to="/workflows">
            <Boxes size={17} />
            工作流
          </NavLink>
          <NavLink to="/approvals">
            <ClipboardCheck size={17} />
            待办
          </NavLink>
          <NavLink to="/schedules">
            <CalendarClock size={17} />
            定时任务
          </NavLink>
        </nav>

        <div className="sidebar-foot">
          <div className={`service-state ${connected ? "online" : "offline"}`}>
            <span className="state-dot" />
            <div>
              <strong>{connected ? "服务已连接" : "服务不可用"}</strong>
              <span>127.0.0.1:8080</span>
            </div>
          </div>
          <div className="local-note">
            <Server size={14} />
            仅本机可访问
          </div>
        </div>
      </aside>

      <main className="main">
        <Routes>
          <Route path="/" element={<Navigate to="/runs" replace />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/runs/:workflowId/:runId" element={<RunDetailPage />} />
          <Route path="/workflows" element={<WorkflowsPage />} />
          <Route path="/approvals" element={<ApprovalsPage />} />
          <Route path="/schedules" element={<SchedulesPage />} />
          <Route path="*" element={<Navigate to="/runs" replace />} />
        </Routes>
      </main>
    </div>
  );
}
