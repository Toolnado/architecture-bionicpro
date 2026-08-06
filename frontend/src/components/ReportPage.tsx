import React, { useCallback, useEffect, useState } from 'react';

const AUTH_URL = process.env.REACT_APP_AUTH_URL || 'http://localhost:8000';

interface SessionInfo {
  sessionId: string;
  username: string;
  email: string;
  roles: string[];
}

interface ReportDay {
  date: string;
  prosthesisSerial: string;
  model: string;
  readingsTotal: number;
  gesturesRecognized: number;
  recognitionRate: number;
  avgLatencyMs: number;
  p95LatencyMs: number;
  maxLatencyMs: number;
  minBatteryLevel: number;
  avgBatteryLevel: number;
  avgSignalQuality: number;
  errorEvents: number;
}

interface Report {
  username: string;
  client?: { fullName: string; city: string; contractNumber: string };
  prostheses: string[];
  period: {
    from: string;
    to: string;
    requestedTo: string;
    dataCoveredUntil: string;
    truncated: boolean;
  };
  totals: {
    days: number;
    readingsTotal: number;
    gesturesRecognized: number;
    recognitionRate: number;
    avgLatencyMs: number;
    maxLatencyMs: number;
    minBatteryLevel: number;
    avgBatteryLevel: number;
    avgSignalQuality: number;
    errorEvents: number;
  };
  days: ReportDay[];
  generatedAt: string;
}

const ReportPage: React.FC = () => {
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [initialized, setInitialized] = useState(false);
  const [report, setReport] = useState<Report | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadSession = useCallback(async () => {
    try {
      const response = await fetch(`${AUTH_URL}/auth/session`, { credentials: 'include' });
      if (response.status === 401) {
        setSession(null);
        return;
      }
      if (!response.ok) {
        throw new Error(`Session request failed: ${response.status}`);
      }
      setSession(await response.json());
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
    } finally {
      setInitialized(true);
    }
  }, []);

  useEffect(() => {
    loadSession();
  }, [loadSession]);

  const login = () => {
    window.location.href = `${AUTH_URL}/auth/login?return_to=/`;
  };

  const logout = async () => {
    await fetch(`${AUTH_URL}/auth/logout`, { method: 'POST', credentials: 'include' });
    setSession(null);
    setReport(null);
  };

  const generateReport = async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await fetch(`${AUTH_URL}/api/reports`, { credentials: 'include' });

      if (response.status === 401) {
        setSession(null);
        throw new Error('Session expired, please sign in again');
      }
      if (response.status === 403) {
        throw new Error('Недостаточно прав: нужна роль prothetic_user');
      }
      if (response.status === 409) {
        const body = await response.json();
        throw new Error(`Данные ещё не обработаны Airflow. Готовы по ${body.dataCoveredUntil}`);
      }
      if (response.status === 503) {
        throw new Error('Витрина ещё не наполнена: дождитесь первого запуска Airflow');
      }
      if (!response.ok) {
        throw new Error(`Report request failed: ${response.status}`);
      }

      setReport(await response.json());
      await loadSession();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
    } finally {
      setLoading(false);
    }
  };

  const downloadReport = () => {
    if (!report) {
      return;
    }
    const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' });
    const url = window.URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `prosthesis-report-${report.username}-${report.period.from}_${report.period.to}.json`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.URL.revokeObjectURL(url);
  };

  if (!initialized) {
    return <div>Loading...</div>;
  }

  if (!session) {
    return (
      <div className="flex flex-col items-center justify-center min-h-screen bg-gray-100">
        <button
          onClick={login}
          className="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600"
        >
          Login
        </button>
        {error && (
          <div className="mt-4 p-4 bg-red-100 text-red-700 rounded">
            {error}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col items-center min-h-screen bg-gray-100 py-10">
      <div className="p-8 bg-white rounded-lg shadow-md w-full max-w-4xl">
        <h1 className="text-2xl font-bold mb-2">Usage Reports</h1>
        <p className="text-sm text-gray-600 mb-6">
          {session.username} &middot; {session.roles.join(', ')}
        </p>

        <div className="flex gap-2">
          <button
            onClick={generateReport}
            disabled={loading}
            className={`px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600 ${
              loading ? 'opacity-50 cursor-not-allowed' : ''
            }`}
          >
            {loading ? 'Generating Report...' : 'Generate Report'}
          </button>
          <button
            onClick={downloadReport}
            disabled={!report}
            className={`px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700 ${
              report ? '' : 'opacity-50 cursor-not-allowed'
            }`}
          >
            Download Report
          </button>
          <button
            onClick={logout}
            className="px-4 py-2 bg-gray-200 text-gray-800 rounded hover:bg-gray-300"
          >
            Logout
          </button>
        </div>

        {error && (
          <div className="mt-4 p-4 bg-red-100 text-red-700 rounded">
            {error}
          </div>
        )}

        {report && (
          <div className="mt-8">
            <h2 className="text-lg font-semibold">
              {report.client ? report.client.fullName : report.username}
            </h2>
            <p className="text-sm text-gray-600">
              Период {report.period.from} — {report.period.to}
              {report.period.truncated && (
                <span className="text-amber-700">
                  {' '}(запрошено по {report.period.requestedTo}, Airflow обработал по{' '}
                  {report.period.dataCoveredUntil})
                </span>
              )}
            </p>
            <p className="text-sm text-gray-600 mb-4">
              Протезы: {report.prostheses.join(', ') || '—'}
            </p>

            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
              <Metric label="Дней в отчёте" value={report.totals.days} />
              <Metric label="Событий телеметрии" value={report.totals.readingsTotal} />
              <Metric
                label="Распознано жестов"
                value={`${(report.totals.recognitionRate * 100).toFixed(1)}%`}
              />
              <Metric label="Средняя задержка" value={`${report.totals.avgLatencyMs} мс`} />
              <Metric label="Пиковая задержка" value={`${report.totals.maxLatencyMs} мс`} />
              <Metric label="Средний заряд" value={`${report.totals.avgBatteryLevel}%`} />
              <Metric label="Минимальный заряд" value={`${report.totals.minBatteryLevel}%`} />
              <Metric label="Ошибок" value={report.totals.errorEvents} />
            </div>

            <div className="overflow-x-auto">
              <table className="min-w-full text-sm">
                <thead>
                  <tr className="text-left text-gray-500 border-b">
                    <th className="py-2 pr-4">Дата</th>
                    <th className="py-2 pr-4">Протез</th>
                    <th className="py-2 pr-4">События</th>
                    <th className="py-2 pr-4">Распознано</th>
                    <th className="py-2 pr-4">Задержка, мс</th>
                    <th className="py-2 pr-4">p95, мс</th>
                    <th className="py-2 pr-4">Заряд, %</th>
                    <th className="py-2">Ошибки</th>
                  </tr>
                </thead>
                <tbody>
                  {report.days.slice(-14).reverse().map((day) => (
                    <tr key={`${day.date}-${day.prosthesisSerial}`} className="border-b last:border-0">
                      <td className="py-2 pr-4">{day.date.slice(0, 10)}</td>
                      <td className="py-2 pr-4">{day.prosthesisSerial}</td>
                      <td className="py-2 pr-4">{day.readingsTotal}</td>
                      <td className="py-2 pr-4">{(day.recognitionRate * 100).toFixed(1)}%</td>
                      <td className="py-2 pr-4">{day.avgLatencyMs}</td>
                      <td className="py-2 pr-4">{day.p95LatencyMs}</td>
                      <td className="py-2 pr-4">{day.avgBatteryLevel}</td>
                      <td className="py-2">{day.errorEvents}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

const Metric: React.FC<{ label: string; value: React.ReactNode }> = ({ label, value }) => (
  <div className="p-3 bg-gray-50 rounded">
    <div className="text-xs text-gray-500">{label}</div>
    <div className="text-lg font-semibold">{value}</div>
  </div>
);

export default ReportPage;
