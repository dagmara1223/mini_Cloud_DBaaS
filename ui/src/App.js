import { useEffect, useState } from "react";
import "./App.css";

function App() {
  const [status, setStatus] = useState("loading");
  const [databases, setDatabases] = useState([]);

  useEffect(() => {
    fetch("http://localhost:8000/health", {
      headers: { "X-API-Key": "dev-secret-change-me" }
    })
      .then(res => res.json())
      .then(data => setStatus(data.status))
      .catch(() => setStatus("error"));

    fetch("http://localhost:8000/databases", {
      headers: { "X-API-Key": "dev-secret-change-me" }
    })
      .then(res => res.json())
      .then(data => setDatabases(data))
      .catch(() => setDatabases([]));
  }, []);

  return (
    <div className="container">

      {/* header */}
      <div className="header">
        <div className="title">☁️ Mini Cloud DBaaS</div>
        <div className="badge">demo</div>
      </div>

      <div className="grid">

        {/* stats */}
        <div className="card">
          <div className="card-title">System Status</div>
          <div className={`status ${status === "ok" ? "status-ok" : "status-error"}`}>
            {status}
          </div>
        </div>

        {/* Lb */}
        <div className="card">
          <div className="card-title">Load Balancer</div>
          <div>Active node: <span className="node">node-1</span></div>
        </div>

        {/* dbs */}
        <div className="card full">
          <div className="card-title">Databases</div>

          <div className="db-list">
            {databases.length === 0 ? (
              <div style={{ color: "#6b7280" }}>No databases</div>
            ) : (
              databases.map((db) => (
                <div key={db.db_id} className="db-item">
                  <span className="db-name">{db.db_name}</span>
                  <span className={`db-status ${db.status === "running" ? "db-running" : "db-other"
                    }`}>
                    {db.status}
                  </span>
                </div>
              ))
            )}
          </div>
        </div>

      </div>
    </div>
  );
}

export default App;