import React, { useState, useEffect } from 'react';

const API_BASE = window.location.port === '3000' || window.location.port === '5173'
  ? 'http://localhost:9000'
  : '';

function App() {
  const [port, setPort] = useState(5050);
  const [healthInterval, setHealthInterval] = useState(500);
  const [healthPath, setHealthPath] = useState('/ping');
  const [backends, setBackends] = useState([
    { url: 'http://localhost:8080', priority: '1' },
    { url: 'http://localhost:8081', priority: '1' }
  ]);

  const [status, setStatus] = useState({
    running: false,
    port: 0,
    health_check_path: '',
    health_check_interval_ms: 0,
    backends: []
  });

  const [error, setError] = useState('');
  const [successMsg, setSuccessMsg] = useState('');
  const [darkMode, setDarkMode] = useState(false);

  // Toggle Dark Mode
  useEffect(() => {
    if (darkMode) {
      document.body.classList.add('dark');
    } else {
      document.body.classList.remove('dark');
    }
  }, [darkMode]);

  // Fetch status of the load balancer manually
  const fetchStatus = async (silent = false) => {
    try {
      const res = await fetch(`${API_BASE}/api/status`);
      if (res.ok) {
        const data = await res.json();
        setStatus(data);
        setError('');
        if (!silent) showSuccess('Status retrieved successfully.');
      } else {
        setError('Error fetching load balancer status.');
      }
    } catch (err) {
      setError('Cannot connect to control API server.');
    }
  };

  // Initial fetch and 30 second polling
  useEffect(() => {
    fetchStatus(true);
    const interval = setInterval(() => {
      fetchStatus(true);
    }, 30000);
    return () => clearInterval(interval);
  }, []);

  // Update State from config
  const applyConfigToForm = (cfg) => {
    if (cfg.port) setPort(cfg.port);
    if (cfg.health_check_interval_ms) setHealthInterval(cfg.health_check_interval_ms);
    if (cfg.health_check_path) setHealthPath(cfg.health_check_path);
    if (cfg.backends) {
      setBackends(cfg.backends.map(b => ({ url: b.url, priority: b.priority ? String(b.priority) : '1' })));
    }
  };

  // Synchronize form with the currently active running config
  const syncWithRunningConfig = () => {
    if (status.port) {
      applyConfigToForm(status);
      showSuccess('Synced inputs with active config.');
    } else {
      setError('No active configuration to sync.');
    }
  };

  const showSuccess = (msg) => {
    setSuccessMsg(msg);
    setTimeout(() => setSuccessMsg(''), 3000);
  };

  const handleBackendChange = (index, field, value) => {
    const updated = [...backends];
    updated[index][field] = value;
    setBackends(updated);
  };

  const addBackend = () => {
    setBackends([...backends, { url: 'http://localhost:', priority: '1' }]);
  };

  const removeBackend = (index) => {
    if (backends.length <= 1) {
      setError('At least one backend is required.');
      return;
    }
    setBackends(backends.filter((_, i) => i !== index));
  };

  // Actions
  const handleStart = async () => {
    try {
      const cfg = {
        port,
        health_check_interval_ms: healthInterval,
        health_check_path: healthPath,
        backends: backends.map(b => ({ url: b.url, priority: b.priority }))
      };

      const res = await fetch(`${API_BASE}/api/config`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(cfg)
      });

      if (res.ok) {
        showSuccess('Configuration applied and Load Balancer started!');
        fetchStatus(true);
      } else {
        const errText = await res.text();
        setError(`Failed to apply configuration: ${errText}`);
      }
    } catch (err) {
      setError('Connection failure trying to start balancer.');
    }
  };

  const handleStop = async () => {
    try {
      const res = await fetch(`${API_BASE}/api/stop`, { method: 'POST' });
      if (res.ok) {
        showSuccess('Load Balancer stopped.');
        fetchStatus(true);
      } else {
        setError('Failed to stop balancer.');
      }
    } catch (err) {
      setError('Connection failure trying to stop balancer.');
    }
  };

  return (
    <div className="app-container">
      <div className="top-bar">
        <div>Load Balancer Least Connections</div>
        <div className="top-bar-controls">
          <div className="top-bar-link" onClick={() => setDarkMode(!darkMode)}>
            [{darkMode ? 'Light Mode' : 'Dark Mode'}]
          </div>
          <div className="top-bar-link" onClick={syncWithRunningConfig}>[Sync Config]</div>
          <div className="top-bar-link">[Copy Link] ▼</div>
        </div>
      </div>

      <h1>Load Balancer</h1>
      <div className="header-desc">
      </div>

      {error && <div className="alert alert-error">{error}</div>}
      {successMsg && <div className="alert alert-success">{successMsg}</div>}

      <div className="main-content">

        {/* Left column: Configuration */}
        <div className="config-section">
          <div className="form-row">
            <div className="form-group">
              <label>Listen Port</label>
              <input
                type="number"
                value={port}
                onChange={(e) => setPort(Number(e.target.value))}
                placeholder="5050"
              />
            </div>
            <div className="form-group">
              <label>Health Check Interval (ms)</label>
              <input
                type="number"
                value={healthInterval}
                onChange={(e) => setHealthInterval(Number(e.target.value))}
                placeholder="500"
              />
            </div>
          </div>

          <div className="form-group">
            <label>Health Check Path</label>
            <input
              type="text"
              value={healthPath}
              onChange={(e) => setHealthPath(e.target.value)}
              placeholder="/ping"
            />
          </div>

          <div className="form-group backends-container">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <label>Backends</label>
              <button className="btn" onClick={addBackend} style={{ padding: '0.2rem 0.5rem', marginBottom: '0.5rem' }}>
                + Add
              </button>
            </div>

            <div className="backends-list">
              {backends.map((backend, index) => (
                <div className="backend-item" key={index}>
                  <input
                    type="text"
                    value={backend.url}
                    onChange={(e) => handleBackendChange(index, 'url', e.target.value)}
                    placeholder="http://localhost:8080"
                  />
                  <input
                    type="number"
                    className="backend-priority"
                    value={backend.priority}
                    onChange={(e) => handleBackendChange(index, 'priority', e.target.value)}
                    placeholder="1"
                    min="1"
                  />
                  <button
                    className="btn btn-danger"
                    onClick={() => removeBackend(index)}
                    style={{ padding: '0.55rem 0.75rem' }}
                  >
                    X
                  </button>
                </div>
              ))}
            </div>
          </div>

          <div style={{ display: 'flex', gap: '1rem', marginTop: 'auto', paddingTop: '1rem' }}>
            {!status.running ? (
              <button className="btn btn-primary" onClick={handleStart} style={{ flex: 1 }}>
                Start Load Balancer
              </button>
            ) : (
              <>
                <button className="btn btn-primary" onClick={handleStart} style={{ flex: 1 }}>
                  Restart & Apply Changes
                </button>
                <button className="btn btn-danger" onClick={handleStop} style={{ flex: 1 }}>
                  Stop Engine
                </button>
              </>
            )}
          </div>
        </div>

        {/* Right column: Monitoring */}
        <div className="status-area">
          <div className="status-header">
            <h3>Live Monitoring Status</h3>
            <div style={{ display: 'flex', gap: '1rem', alignItems: 'center' }}>
              <span className={`status-indicator ${status.running ? 'running' : ''}`}>
                {status.running ? 'STATE: RUNNING' : 'STATE: STOPPED'}
              </span>
              <button className="btn" onClick={() => fetchStatus(false)}>
                Refresh Status
              </button>
            </div>
          </div>

          <div className="status-cards-container">
            {status.running && status.backends ? (
              <div>
                {status.backends.map((b, idx) => (
                  <div className="backend-status-card" key={idx}>
                    <div>
                      <div className="backend-status-url">{b.url}</div>
                      <div className="backend-status-meta">
                        Priority: {b.priority} | Active Connections: {b.connections}
                      </div>
                    </div>
                    <div>
                      <span className={`badge ${b.isDead ? 'badge-offline' : 'badge-online'}`}>
                        {b.isDead ? 'OFFLINE' : 'ONLINE'}
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div style={{ color: 'var(--text-muted)', fontSize: '0.9rem', fontStyle: 'italic', fontFamily: 'monospace' }}>
                No live stats available. Start the balancer to begin monitoring logic. (Auto refreshes every 30s)
              </div>
            )}
          </div>
        </div>

      </div>
    </div>
  );
}

export default App;
