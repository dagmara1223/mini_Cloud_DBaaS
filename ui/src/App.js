// aby odpalic: cd ui     npm start

import { useEffect, useState, useCallback } from "react";
import {
  Chart as ChartJS,
  LineElement,
  CategoryScale,
  LinearScale,
  PointElement,
  Filler,
  Tooltip,
} from "chart.js";
import { Line } from "react-chartjs-2";
import "./App.css";

ChartJS.register(LineElement, CategoryScale, LinearScale, PointElement, Filler, Tooltip);

// const API = "http://127.0.0.1:8000";
// const HEADERS = { "X-API-Key": "dev-secret-change-me" };
const API = "http://127.0.0.1:9000";
const getToken = () => localStorage.getItem("jwt_token");
const saveToken = (t) => localStorage.setItem("jwt_token", t);
const clearToken = () => localStorage.removeItem("jwt_token");
const authHeaders = (extra = {}) => ({
  "Authorization": `Bearer ${getToken()}`,
  "Content-Type": "application/json",
  ...extra,
});

const fmt = (n) => (n ?? 0).toFixed(1);
const statusColor = (s) =>
  s === "running" ? "#10b981" : s === "stopped" ? "#f59e0b" : "#ef4444";

const Icon = {
  db: (
    <svg width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" viewBox="0 0 24 24">
      <ellipse cx="12" cy="5" rx="9" ry="3" /><path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5" />
      <path d="M3 12c0 1.66 4.03 3 9 3s9-1.34 9-3" />
    </svg>
  ),
  play: (
    <svg width="13" height="13" fill="currentColor" viewBox="0 0 24 24"><path d="M5 3l14 9-14 9V3z" /></svg>
  ),
  stop: (
    <svg width="13" height="13" fill="currentColor" viewBox="0 0 24 24"><rect x="4" y="4" width="16" height="16" rx="2" /></svg>
  ),
  trash: (
    <svg width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" viewBox="0 0 24 24">
      <path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6" />
    </svg>
  ),
  plus: (
    <svg width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" viewBox="0 0 24 24">
      <path d="M12 5v14M5 12h14" />
    </svg>
  ),
  cpu: (
    <svg width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" viewBox="0 0 24 24">
      <rect x="4" y="4" width="16" height="16" rx="2" />
      <rect x="9" y="9" width="6" height="6" />
      <path d="M9 2v2M15 2v2M9 20v2M15 20v2M2 9h2M2 15h2M20 9h2M20 15h2" />
    </svg>
  ),
  mem: (
    <svg width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" viewBox="0 0 24 24">
      <rect x="2" y="6" width="20" height="12" rx="2" />
      <path d="M6 6V4M10 6V4M14 6V4M18 6V4M6 18v2M10 18v2M14 18v2M18 18v2" />
    </svg>
  ),
  refresh: (
    <svg width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" viewBox="0 0 24 24">
      <path d="M23 4v6h-6M1 20v-6h6" />
      <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15" />
    </svg>
  ),
  server: (
    <svg width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" viewBox="0 0 24 24">
      <rect x="2" y="2" width="20" height="8" rx="2" />
      <rect x="2" y="14" width="20" height="8" rx="2" />
      <circle cx="6" cy="6" r="1" fill="currentColor" />
      <circle cx="6" cy="18" r="1" fill="currentColor" />
    </svg>
  ),
  sql: (<svg width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" viewBox="0 0 24 24">
    <path d="M4 7h16M4 12h10M4 17h7" /><path d="M15 15l2 2 4-4" /></svg>),
  warn: (<svg width="22" height="22" fill="none" stroke="currentColor" strokeWidth="1.8" viewBox="0 0 24 24">
    <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg>),
  run: (<svg width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" viewBox="0 0 24 24">
    <polygon points="5 3 19 12 5 21 5 3" /></svg>),
};

function LoginScreen({ onLogin }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");

  const submit = async () => {
    if (!username || !password) { setErr("Wypełnij wszystkie pola."); return; }
    setLoading(true); setErr("");
    try {
      const res = await fetch(`${API}/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, password }),
      });
      if (!res.ok) throw new Error("Nieprawidłowy login lub hasło.");
      const data = await res.json();
      saveToken(data.token);
      onLogin(username);
    } catch (e) { setErr(e.message); }
    finally { setLoading(false); }
  };

  const handleKey = (e) => { if (e.key === "Enter") submit(); };

  return (
    <div className="login-bg">
      <div className="login-card">
        <div className="login-logo">
          <span className="login-logo-icon">☁</span>
          <div>
            <div className="login-title">MiniCloud</div>
            <div className="login-sub">DBaaS Console</div>
          </div>
        </div>
        <div className="login-divider" />
        <div className="login-form">
          {err && <div className="modal-err">{err}</div>}
          <label className="inp-label">Username</label>
          <input className="inp" placeholder="admin" value={username} onChange={(e) => setUsername(e.target.value)} onKeyDown={handleKey} autoFocus />
          <label className="inp-label">Password</label>
          <input className="inp" type="password" placeholder="••••••••" value={password} onChange={(e) => setPassword(e.target.value)} onKeyDown={handleKey} />
          <button className="btn-primary login-btn" onClick={submit} disabled={loading}>
            {loading ? "Logowanie..." : "Zaloguj"}
          </button>
        </div>
        <div className="login-hint">Domyślne dane: <code>admin</code> / <code>password</code></div>
      </div>
    </div>
  );
}

function SparkLine({ data, color = "#0ea5e9", height = 60 }) {
  const labels = data.map((_, i) => i);
  return (
    <Line
      height={height}
      data={{
        labels,
        datasets: [{
          data,
          borderColor: color,
          borderWidth: 1.5,
          pointRadius: 0,
          tension: 0.4,
          fill: true,
          backgroundColor: color + "18",
        }],
      }}
      options={{
        responsive: true,
        animation: false,
        plugins: { legend: { display: false }, tooltip: { enabled: false } },
        scales: {
          x: { display: false },
          y: { display: false, min: 0, max: 100 },
        },
      }}
    />
  );
}
// potwierdzenie usuniecia 
function ConfirmDeleteDialog({ dbName, onConfirm, onCancel }) {
  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal confirm-modal" onClick={(e) => e.stopPropagation()}>
        <div className="confirm-icon-wrap">
          <span className="confirm-icon">{Icon.warn}</span>
        </div>
        <div className="confirm-title">Usuń bazę danych</div>
        <div className="confirm-msg">
          Czy na pewno chcesz usunąć bazę <strong>{dbName}</strong>?<br />
          <span className="confirm-warn">Ta operacja jest nieodwracalna. Wszystkie dane przepadną.</span>
        </div>
        <div className="confirm-actions">
          <button className="btn-ghost" onClick={onCancel}>Anuluj</button>
          <button className="btn-danger" onClick={onConfirm}>Usuń</button>
        </div>
      </div>
    </div>
  );
}

// panel sql 
function SQLPanel({ databases }) {
  const [selectedDb, setSelectedDb] = useState("");
  const [query, setQuery] = useState("SELECT version();");
  const [result, setResult] = useState(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");

  const runQuery = async () => {
    if (!selectedDb) { setErr("Wybierz bazę danych."); return; }
    if (!query.trim()) { setErr("Wpisz zapytanie SQL."); return; }
    setLoading(true);
    setErr("");
    setResult(null);
    try {
      const res = await fetch(`${API}/databases/${selectedDb}/query`, {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify({ query: query.trim() }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.detail);
      setResult(data);
    } catch (e) {
      setErr(e.message);
    } finally {
      setLoading(false);
    }
  };

  const handleKey = (e) => {
    // Ctrl+Enter lub Cmd+Enter uruchamia query :p
    if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
      e.preventDefault();
      runQuery();
    }
  };

  const runningDbs = databases.filter((d) => d.status === "running");
  const columns = result?.rows?.length > 0 ? Object.keys(result.rows[0]) : [];

  return (
    <div className="sql-panel">
      {/* Wybór bazy */}
      <div className="sql-toolbar">
        <select
          className="sql-select"
          value={selectedDb}
          onChange={(e) => setSelectedDb(e.target.value)}
        >
          <option value="">— wybierz bazę —</option>
          {runningDbs.map((db) => (
            <option key={db.db_id} value={db.db_id}>
              {db.db_name} (port {db.port})
            </option>
          ))}
        </select>
        <button className="btn-run" onClick={runQuery} disabled={loading}>
          {loading ? "..." : <>{Icon.run}&nbsp;Run</>}
        </button>
        <span className="sql-hint">Ctrl+Enter</span>
      </div>

      {/* Edytor zapytania */}
      <textarea
        className="sql-editor"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={handleKey}
        spellCheck={false}
        rows={5}
        placeholder="SELECT * FROM tabela;"
      />

      {/* Błąd */}
      {err && <div className="sql-err">{err}</div>}

      {/* Wynik */}
      {result && (
        <div className="sql-result">
          <div className="sql-result-meta">
            {result.rows?.length > 0
              ? `${result.rows.length} ${result.rows.length === 1 ? "wiersz" : "wierszy"}`
              : result.affected_rows !== undefined
                ? `Zaktualizowano ${result.affected_rows} wierszy`
                : "OK"}
            <span className="sql-result-status">✓ {result.status}</span>
          </div>

          {columns.length > 0 && (
            <div className="sql-table-wrap">
              <table className="sql-table">
                <thead>
                  <tr>{columns.map((c) => <th key={c}>{c}</th>)}</tr>
                </thead>
                <tbody>
                  {result.rows.map((row, i) => (
                    <tr key={i}>
                      {columns.map((c) => (
                        <td key={c}>{row[c] === null ? <span className="sql-null">NULL</span> : String(row[c])}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {runningDbs.length === 0 && (
        <div className="sql-no-dbs">Brak uruchomionych baz. Uruchom bazę żeby wykonać query.</div>
      )}
    </div>
  );
}


// modal do tworzenia bazy
function CreateDBModal({ onClose, onCreate }) {
  const [form, setForm] = useState({ db_name: "", owner: "", password: "" });
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");

  const submit = async () => {
    if (!form.db_name || !form.owner || !form.password) {
      setErr("Wypełnij wszystkie pola.");
      return;
    }
    setLoading(true);
    try {
      const res = await fetch(`${API}/databases`, {
        method: "POST",
        headers: authHeaders(),
        body: JSON.stringify(form),
      });
      if (!res.ok) throw new Error((await res.json()).detail);
      const data = await res.json();
      onCreate(data);
      onClose();
    } catch (e) {
      setErr(e.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <span className="modal-title">Nowa baza danych</span>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="modal-body">
          {err && <div className="modal-err">{err}</div>}
          <label>Nazwa bazy</label>
          <input
            className="inp"
            value={form.db_name}
            onChange={(e) => setForm({ ...form, db_name: e.target.value })}
          />
          <label>Owner</label>
          <input
            className="inp"
            value={form.owner}
            onChange={(e) => setForm({ ...form, owner: e.target.value })}
          />
          <label>Hasło</label>
          <input
            className="inp"
            type="password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
          />
          <div className="modal-actions">
            <button className="btn-ghost" onClick={onClose}>Anuluj</button>
            <button className="btn-primary" onClick={submit} disabled={loading}>
              {loading ? "Tworzenie..." : "Utwórz"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

//powaidomienie
function Toast({ msg, type, onDone }) {
  useEffect(() => {
    const t = setTimeout(onDone, 3000);
    return () => clearTimeout(t);
  }, []);
  return <div className={`toast toast-${type}`}>{msg}</div>;
}

// glowna aplikacja
export default function App() {
  const [user, setUser] = useState(() => getToken() ? "admin" : null);
  const handleLogin = (username) => setUser(username);
  const handleLogout = () => { clearToken(); setUser(null); };
  const [health, setHealth] = useState(null);
  const [metrics, setMetrics] = useState(null);
  const [databases, setDatabases] = useState([]);
  const [cpuHistory, setCpuHistory] = useState([]);
  const [memHistory, setMemHistory] = useState([]);
  const [dbCpu, setDbCpu] = useState({});
  const [showCreate, setShowCreate] = useState(false);
  const [toast, setToast] = useState(null);
  const [lastUpdate, setLastUpdate] = useState(null);
  const [activeSection, setActiveSection] = useState("dashboard");
  const [confirmDelete, setConfirmDelete] = useState(null); // { db_id, db_name }

  const notify = (msg, type = "ok") => setToast({ msg, type });

  const fetchAll = useCallback(async () => {
    try {
      const [hRes, mRes, dbRes] = await Promise.all([
        fetch(`${API}/health`, { headers: authHeaders() }),
        fetch(`${API}/metrics`, { headers: authHeaders() }),
        fetch(`${API}/databases`, { headers: authHeaders() }),
      ]);
      if (hRes.status === 401 || mRes.status === 401 || dbRes.status === 401) {
        handleLogout();
        notify("Sesja wygasła. Zaloguj się ponownie.", "err");
        return;
      }
      if (hRes.ok) setHealth(await hRes.json());
      if (mRes.ok) {
        const m = await mRes.json();
        setMetrics(m);
        setCpuHistory((p) => [...p.slice(-40), m.cpu_percent]);
        setMemHistory((p) => [...p.slice(-40), m.mem_percent]);
      }
      if (dbRes.ok) {
        const dbs = await dbRes.json();
        setDatabases(dbs);
        // per-db CPU
        for (const db of dbs) {
          try {
            const r = await fetch(`${API}/databases/${db.db_id}/metrics`, { headers: authHeaders() });
            if (r.ok) {
              const d = await r.json();
              setDbCpu((prev) => ({
                ...prev,
                [db.db_id]: [...(prev[db.db_id] || []).slice(-20), d.cpu_percent],
              }));
            }
          } catch { /* ignoruj */ }
        }
      }
      setLastUpdate(new Date());
    } catch { /* serwer offline */ }
  }, []);

  useEffect(() => {
    fetchAll();
    const iv = setInterval(fetchAll, 5000);
    return () => clearInterval(iv);
  }, [fetchAll]);

  // akcje 
  const handleAction = async (db_id, action) => {
    const method = action === "delete" ? "DELETE" : "POST";
    const url = action === "delete"
      ? `${API}/databases/${db_id}`
      : `${API}/databases/${db_id}/${action}`;
    try {
      const res = await fetch(url, { method, headers: authHeaders() });
      if (!res.ok) throw new Error((await res.json()).detail);
      notify(
        action === "delete" ? "Baza usunięta" :
          action === "start" ? "Baza uruchomiona" : "Baza zatrzymana",
        "ok"
      );
      fetchAll();
    } catch (e) {
      notify(e.message, "err");
    }
  };

  const isOnline = health?.status === "ok";
  const running = databases.filter((d) => d.status === "running").length;
  const stopped = databases.filter((d) => d.status !== "running").length;
  const askDelete = (db) => setConfirmDelete({ db_id: db.db_id, db_name: db.db_name });
  const ActionBtns = ({ db }) => (
    <div className="db-mini-actions">
      {db.status !== "running"
        ? <button className="act-btn green" onClick={() => handleAction(db.db_id, "start")} title="Start">{Icon.play}</button>
        : <button className="act-btn amber" onClick={() => handleAction(db.db_id, "stop")} title="Stop">{Icon.stop}</button>
      }
      <button className="act-btn red" onClick={() => askDelete(db)} title="Delete">{Icon.trash}</button>
    </div>
  );
  if (!user) return <LoginScreen onLogin={handleLogin} />;
  return (
    <div className="shell">
      {/* sidebar */}
      <aside className="sidebar">
        <div className="sidebar-logo">
          <div className="logo-icon">☁</div>
          <div>
            <div className="logo-title">MiniCloud</div>
            <div className="logo-sub">DBaaS Console</div>
          </div>
        </div>
        <nav className="nav">
          {[
            { id: "dashboard", label: "Dashboard", icon: "⊞" },
            { id: "databases", label: "Databases", icon: "⛃" },
            { id: "sql", label: "SQL Query", icon: "▷" },
            { id: "metrics", label: "Metrics", icon: "⎍" },
          ].map((item) => (
            <button
              key={item.id}
              className={`nav-item ${activeSection === item.id ? "active" : ""}`}
              onClick={() => setActiveSection(item.id)}
            >
              <span className="nav-icon">{item.icon}</span>
              {item.label}
            </button>
          ))}
        </nav>
        <div className="sidebar-footer">
          <div className={`node-status ${isOnline ? "online" : "offline"}`}>
            <span className="node-dot" />
            <div>
              <div className="node-label">node-1</div>
              <div className="node-state">{isOnline ? "online" : "offline"}</div>
            </div>
          </div>
        </div>
      </aside>

      {/* main apka */}
      <main className="main">
        <header className="topbar">
          <div className="topbar-left">
            <span className="page-title">
              {activeSection === "dashboard" && "Dashboard"}
              {activeSection === "databases" && "Databases"}
              {activeSection === "sql" && "SQL Query"}
              {activeSection === "metrics" && "Metrics"}
            </span>
            {lastUpdate && (
              <span className="last-upd">{Icon.refresh}&nbsp;{lastUpdate.toLocaleTimeString()}</span>
            )}
          </div>
          <div className="topbar-right">
            <div className="user-chip">
              <span className="user-dot">●</span>
              <span className="user-name">{user}</span>
            </div>
            <button className="btn-ghost btn-logout" onClick={handleLogout}>
              Wyloguj
            </button>
            <button className="btn-primary" onClick={() => setShowCreate(true)}>
              {Icon.plus}&nbsp;New Database
            </button>
          </div>
        </header>

        <div className="content">

          {/* dashboard */}
          {activeSection === "dashboard" && (<>
            <div className="kpi-row">
              <div className="kpi-card"><div className="kpi-icon blue">{Icon.server}</div><div className="kpi-body"><div className="kpi-label">Node status</div><div className={`kpi-value ${isOnline ? "green" : "red"}`}>{isOnline ? "Healthy" : "Offline"}</div></div></div>
              <div className="kpi-card"><div className="kpi-icon teal">{Icon.db}</div><div className="kpi-body"><div className="kpi-label">Total databases</div><div className="kpi-value">{databases.length}</div></div></div>
              <div className="kpi-card"><div className="kpi-icon green">{Icon.play}</div><div className="kpi-body"><div className="kpi-label">Running</div><div className="kpi-value green">{running}</div></div></div>
              <div className="kpi-card"><div className="kpi-icon amber">{Icon.stop}</div><div className="kpi-body"><div className="kpi-label">Stopped</div><div className="kpi-value amber">{stopped}</div></div></div>
              <div className="kpi-card"><div className="kpi-icon blue">{Icon.cpu}</div><div className="kpi-body"><div className="kpi-label">CPU</div><div className="kpi-value">{fmt(metrics?.cpu_percent)}%</div></div></div>
              <div className="kpi-card"><div className="kpi-icon purple">{Icon.mem}</div><div className="kpi-body"><div className="kpi-label">Memory</div><div className="kpi-value">{fmt(metrics?.mem_percent)}%</div></div></div>
            </div>
            <div className="charts-row">
              <div className="chart-card"><div className="chart-header"><span>{Icon.cpu} CPU Usage</span><span className="chart-cur">{fmt(cpuHistory.at(-1))}%</span></div><SparkLine data={cpuHistory} color="#0ea5e9" height={80} /></div>
              <div className="chart-card"><div className="chart-header"><span>{Icon.mem} Memory Usage</span><span className="chart-cur">{fmt(memHistory.at(-1))}%</span></div><SparkLine data={memHistory} color="#a78bfa" height={80} /></div>
            </div>
            <div className="section-title">Active databases</div>
            <div className="db-mini-grid">
              {databases.length === 0 && <div className="empty-state">Brak baz. Kliknij „New Database" żeby dodać pierwszą.</div>}
              {databases.map((db) => (
                <div key={db.db_id} className="db-mini-card">
                  <div className="db-mini-top">
                    <span className="db-mini-name">{db.db_name}</span>
                    <span className="db-badge" style={{ color: statusColor(db.status) }}>● {db.status}</span>
                  </div>
                  <div className="db-mini-owner">owner: {db.owner}</div>
                  <div className="db-mini-port">port: {db.port}</div>
                  <div className="db-mini-chart"><SparkLine data={dbCpu[db.db_id] || [0]} color="#10b981" height={35} /></div>
                  <ActionBtns db={db} />
                </div>
              ))}
            </div>
          </>)}

          {/* databases */}
          {activeSection === "databases" && (<>
            <div className="section-title">All databases&nbsp;<span className="badge-count">{databases.length}</span></div>
            <div className="db-table-wrap">
              <table className="db-table">
                <thead><tr><th>ID</th><th>Name</th><th>Owner</th><th>Port</th><th>Status</th><th>CPU</th><th>Actions</th></tr></thead>
                <tbody>
                  {databases.length === 0 && <tr><td colSpan={7} className="empty-td">Brak baz danych</td></tr>}
                  {databases.map((db) => (
                    <tr key={db.db_id}>
                      <td className="td-mono">{db.db_id}</td>
                      <td className="td-bold">{db.db_name}</td>
                      <td>{db.owner}</td>
                      <td className="td-mono">{db.port}</td>
                      <td><span className="status-chip" style={{ color: statusColor(db.status) }}>● {db.status}</span></td>
                      <td className="td-spark"><SparkLine data={dbCpu[db.db_id] || [0]} color="#10b981" height={28} /></td>
                      <td><div className="tbl-actions">
                        {db.status !== "running"
                          ? <button className="act-btn green" onClick={() => handleAction(db.db_id, "start")} title="Start">{Icon.play}</button>
                          : <button className="act-btn amber" onClick={() => handleAction(db.db_id, "stop")} title="Stop">{Icon.stop}</button>
                        }
                        <button className="act-btn red" onClick={() => askDelete(db)} title="Delete">{Icon.trash}</button>
                      </div></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>)}

          {/* live sql!!*/}
          {activeSection === "sql" && (<>
            <div className="section-title">{Icon.sql}&nbsp;SQL Query Editor</div>
            <SQLPanel databases={databases} />
          </>)}

          {/* matryki */}
          {activeSection === "metrics" && (<>
            <div className="section-title">Node metrics</div>
            <div className="metrics-grid">
              <div className="chart-card big"><div className="chart-header"><span>{Icon.cpu} CPU Usage — live</span><span className="chart-cur">{fmt(cpuHistory.at(-1))}%</span></div><SparkLine data={cpuHistory} color="#0ea5e9" height={120} /></div>
              <div className="chart-card big"><div className="chart-header"><span>{Icon.mem} Memory Usage — live</span><span className="chart-cur">{fmt(memHistory.at(-1))}%</span></div><SparkLine data={memHistory} color="#a78bfa" height={120} /></div>
            </div>
            <div className="section-title" style={{ marginTop: 28 }}>Per-database CPU</div>
            <div className="charts-row">
              {databases.map((db) => (
                <div key={db.db_id} className="chart-card">
                  <div className="chart-header"><span>{db.db_name}</span><span className="chart-cur" style={{ color: statusColor(db.status) }}>● {db.status}</span></div>
                  <SparkLine data={dbCpu[db.db_id] || [0]} color="#10b981" height={70} />
                </div>
              ))}
              {databases.length === 0 && <div className="empty-state">Brak baz do wyświetlenia metryk.</div>}
            </div>
            <div className="metrics-raw">
              <div className="raw-title">Raw metrics snapshot</div>
              <div className="raw-grid">
                {[
                  ["db_count", metrics?.db_count ?? "–"],
                  ["active_dbs", metrics?.active_dbs ?? "–"],
                  ["cpu_percent", metrics ? fmt(metrics.cpu_percent) + "%" : "–"],
                  ["mem_percent", metrics ? fmt(metrics.mem_percent) + "%" : "–"],
                  ["mem_available", metrics ? metrics.mem_available_mb + " MB" : "–"],
                ].map(([k, v]) => (
                  <div key={k} className="raw-item"><div className="raw-key">{k}</div><div className="raw-val">{String(v)}</div></div>
                ))}
              </div>
            </div>
          </>)}

        </div>
      </main>

      {/* modals */}
      {showCreate && (
        <CreateDBModal
          onClose={() => setShowCreate(false)}
          onCreate={(db) => { notify(`Baza „${db.db_name}" utworzona na porcie ${db.port}`, "ok"); fetchAll(); }}
        />
      )}

      {confirmDelete && (
        <ConfirmDeleteDialog
          dbName={confirmDelete.db_name}
          onConfirm={() => { handleAction(confirmDelete.db_id, "delete"); setConfirmDelete(null); }}
          onCancel={() => setConfirmDelete(null)}
        />
      )}

      {toast && <Toast msg={toast.msg} type={toast.type} onDone={() => setToast(null)} />}
    </div>
  );
}