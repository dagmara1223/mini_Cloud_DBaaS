import { useEffect, useState } from "react";
import "./App.css";

import {
  Chart as ChartJS,
  LineElement,
  CategoryScale,
  LinearScale,
  PointElement
} from "chart.js";
import { Line } from "react-chartjs-2";

ChartJS.register(LineElement, CategoryScale, LinearScale, PointElement);

function App() {
  const [status, setStatus] = useState("loading");
  const [databases, setDatabases] = useState([]);
  const [cpu, setCpu] = useState([]);
  const [dbCpu, setDbCpu] = useState({});

  useEffect(() => {
    const fetchData = async () => {
      try {
        const headers = {
          "X-API-Key": "dev-secret-change-me"
        };

        // HEALTH
        const healthRes = await fetch("http://127.0.0.1:8000/health", { headers });
        if (!healthRes.ok) throw new Error("health failed");
        const healthData = await healthRes.json();
        setStatus(healthData.status);

        // DATABASES
        const dbRes = await fetch("http://127.0.0.1:8000/databases", { headers });
        if (!dbRes.ok) throw new Error("db failed");
        const dbData = await dbRes.json();
        setDatabases(dbData);

        // METRICS (opcjonalnie)
        try {
          const metricsRes = await fetch("http://127.0.0.1:8000/metrics", { headers });
          if (metricsRes.ok) {
            const metricsData = await metricsRes.json();

            setCpu(prev => [...prev.slice(-20), metricsData.cpu_percent]);
          }
        } catch (e) {
          console.log("metrics failed (ignored)");
        }
        const newDbCpu = {};

        for (const db of dbData) {
          try {
            const res = await fetch(`http://127.0.0.1:8000/databases/${db.db_id}/metrics`, { headers });
            const data = await res.json();

            const prevData = dbCpu[db.db_id] || [];

            newDbCpu[db.db_id] = [
              ...prevData.slice(-10),
              data.cpu_percent
            ];
          } catch {
            newDbCpu[db.db_id] = dbCpu[db.db_id] || [];
          }
        }

        setDbCpu(prev => {
          const updated = { ...prev };

          for (const dbId in newDbCpu) {
            const prevData = prev[dbId] || [];

            updated[dbId] = [
              ...prevData.slice(-10),
              newDbCpu[dbId].slice(-1)[0]
            ];
          }

          return updated;
        });

      } catch (err) {
        console.error("FETCH ERROR:", err);
        setStatus("error");
      }
    };

    fetchData();
    const interval = setInterval(fetchData, 5000);


    return () => clearInterval(interval);
  }, []);

  const chartData = {
    labels: cpu.map((_, i) => i),
    datasets: [
      {
        label: "CPU",
        data: cpu,
        borderColor: "#2563eb",
        tension: 0.3
      }
    ]

  };

  return (
    <div className="layout">

      {/* SIDEBAR */}
      <div className="sidebar">
        <h2>☁️ Cloud</h2>
        <div className="nav-item">Dashboard</div>
        <div className="nav-item">Databases</div>
        <div className="nav-item">Metrics</div>
      </div>

      {/* MAIN */}
      <div className="main">

        {/* TOPBAR */}
        <div className="topbar">
          <div className="title">Mini Cloud DBaaS</div>
          <div className="badge">live</div>
        </div>

        <div className="grid">

          {/* STATUS */}
          <div className="card">
            <div className="card-title">System Status</div>

            <div className={`status-pill ${status}`}>
              {status === "ok" ? "🟢 Healthy" : "🔴 Error"}
            </div>

            <div className="kpi-row">

              <div className="kpi">
                <div className="kpi-label">Total DBs</div>
                <div className="kpi-value">{databases.length}</div>
              </div>

              <div className="kpi">
                <div className="kpi-label">Running</div>
                <div className="kpi-value green">
                  {databases.filter(db => db.status === "running").length}
                </div>
              </div>

              <div className="kpi">
                <div className="kpi-label">Stopped</div>
                <div className="kpi-value red">
                  {databases.filter(db => db.status !== "running").length}
                </div>
              </div>

            </div>

            <div className="last-update">
              Last update: {new Date().toLocaleTimeString()}
            </div>
          </div>

          {/* METRICS */}
          <div className="card">
            <div className="card-title">CPU Usage (live)</div>

            <Line
              data={chartData}
              options={{
                responsive: true,
                plugins: {
                  legend: { display: false }
                },
                scales: {
                  x: {
                    display: false
                  },
                  y: {
                    min: 0,
                    max: 100
                  }
                }
              }}
            />

            <div className="cpu-label">
              Current: {cpu[cpu.length - 1] ?? 0}%
            </div>
          </div>


          {/* DATABASES */}
          <div className="card full">
            <div className="card-title">Databases</div>

            {databases.map((db) => (
              <div key={db.db_id} className="db-item">
                <span>{db.db_name}</span>
                <span className={
                  db.status === "running" ? "db-running" : "db-other"
                }>
                  {db.status}
                </span>
              </div>
            ))}

          </div>

          <div className="mini-metrics">

            {databases.map(db => (
              <div key={db.db_id} className="mini-card">

                <div className="mini-title">
                  {db.db_name}
                </div>

                <Line
                  data={{
                    labels: dbCpu[db.db_id]?.map((_, i) => i) || [],
                    datasets: [
                      {
                        data: dbCpu[db.db_id] || [],
                        borderColor: "#3b82f6",
                        tension: 0.4
                      }
                    ]
                  }}
                  options={{
                    plugins: { legend: { display: false } },
                    scales: {
                      x: { display: false },
                      y: { display: false }
                    }
                  }}
                />

                <div className="mini-status">
                  {db.status}
                </div>

              </div>
            ))}

          </div>

        </div>
      </div>
    </div>
  );
}

export default App;