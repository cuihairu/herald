import { lazy, Suspense } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Spin } from 'antd'
import DashboardLayout from './layouts/DashboardLayout'

// Route-level code splitting: every page is its own lazy chunk, so the
// first paint only waits for the shell (layout + login) and each route's
// code loads on navigation. antd/react stay shared chunks.
const LoginPage = lazy(() => import('./pages/LoginPage'))
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const ProvidersPage = lazy(() => import('./pages/ProvidersPage'))
const ProviderConfigPage = lazy(() => import('./pages/ProviderConfigPage'))
const WorkersPage = lazy(() => import('./pages/WorkersPage'))
const LogsPage = lazy(() => import('./pages/LogsPage'))
const SendPage = lazy(() => import('./pages/SendPage'))
const RulesPage = lazy(() => import('./pages/RulesPage'))
const GroupsPage = lazy(() => import('./pages/GroupsPage'))
const RostersPage = lazy(() => import('./pages/RostersPage'))
const IncidentsPage = lazy(() => import('./pages/IncidentsPage'))

// Exported for the direct unit test: lazy imports resolve synchronously
// under vitest, so the fallback never renders inside route tests.
export function PageLoading() {
  return (
    <div style={{ display: 'flex', justifyContent: 'center', padding: 80 }}>
      <Spin />
    </div>
  )
}

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const token = localStorage.getItem('herald_token')
  if (!token) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <Suspense fallback={<PageLoading />}>
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
          <Route path="rules" element={<RulesPage />} />
          <Route path="groups" element={<GroupsPage />} />
          <Route path="rosters" element={<RostersPage />} />
          <Route path="incidents" element={<IncidentsPage />} />
          <Route path="workers" element={<WorkersPage />} />
          <Route path="logs" element={<LogsPage />} />
          <Route path="send" element={<SendPage />} />
        </Route>
      </Routes>
    </Suspense>
  )
}
