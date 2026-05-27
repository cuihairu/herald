import { Routes, Route, Navigate } from 'react-router-dom'
import DashboardLayout from './layouts/DashboardLayout'
import LoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import ProvidersPage from './pages/ProvidersPage'
import ProviderConfigPage from './pages/ProviderConfigPage'
import WorkersPage from './pages/WorkersPage'
import LogsPage from './pages/LogsPage'
import SendPage from './pages/SendPage'

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const token = localStorage.getItem('herald_token')
  if (!token) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <PrivateRoute>
            <DashboardLayout />
          </PrivateRoute>
        }
      >
        <Route index element={<DashboardPage />} />
        <Route path="providers" element={<ProvidersPage />} />
        <Route path="providers/:name/config" element={<ProviderConfigPage />} />
        <Route path="workers" element={<WorkersPage />} />
        <Route path="logs" element={<LogsPage />} />
        <Route path="send" element={<SendPage />} />
      </Route>
    </Routes>
  )
}
