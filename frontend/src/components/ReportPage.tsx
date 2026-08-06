import React, { useCallback, useEffect, useState } from 'react';

const AUTH_URL = process.env.REACT_APP_AUTH_URL || 'http://localhost:8000';

interface SessionInfo {
  sessionId: string;
  username: string;
  email: string;
  roles: string[];
  accessTokenExpiry: string;
  rotatedAt: string;
}

const ReportPage: React.FC = () => {
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [initialized, setInitialized] = useState(false);
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
  };

  const downloadReport = async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await fetch(`${AUTH_URL}/api/reports`, { credentials: 'include' });
      if (response.status === 401) {
        setSession(null);
        throw new Error('Session expired, please sign in again');
      }
      if (!response.ok) {
        throw new Error(`Report request failed: ${response.status}`);
      }

      const blob = await response.blob();
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'prosthesis-report.pdf';
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.URL.revokeObjectURL(url);

      await loadSession();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
    } finally {
      setLoading(false);
    }
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
    <div className="flex flex-col items-center justify-center min-h-screen bg-gray-100">
      <div className="p-8 bg-white rounded-lg shadow-md">
        <h1 className="text-2xl font-bold mb-2">Usage Reports</h1>
        <p className="text-sm text-gray-600 mb-6">
          {session.username} &middot; {session.roles.join(', ')}
        </p>

        <div className="flex gap-2">
          <button
            onClick={downloadReport}
            disabled={loading}
            className={`px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600 ${
              loading ? 'opacity-50 cursor-not-allowed' : ''
            }`}
          >
            {loading ? 'Generating Report...' : 'Download Report'}
          </button>
          <button
            onClick={logout}
            className="px-4 py-2 bg-gray-200 text-gray-800 rounded hover:bg-gray-300"
          >
            Logout
          </button>
        </div>

        <p className="mt-6 text-xs text-gray-500 break-all">
          session id: {session.sessionId}
        </p>

        {error && (
          <div className="mt-4 p-4 bg-red-100 text-red-700 rounded">
            {error}
          </div>
        )}
      </div>
    </div>
  );
};

export default ReportPage;
