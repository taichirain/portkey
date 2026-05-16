import React from 'react';
import { ConfigProvider, theme, Spin } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
} from 'react-router-dom';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import MainLayout from './layouts/MainLayout';
import LoginPage from './pages/Login';
import DashboardPage from './pages/Dashboard';
import MonitoringPage from './pages/Monitoring';
import ServicesPage from './pages/Services';
import RoutesPage from './pages/Routes';
import UpstreamsPage from './pages/Upstreams';
import TargetsPage from './pages/Targets';
import ConsumersPage from './pages/Consumers';
import PluginsPage from './pages/Plugins';
import RevisionsPage from './pages/Revisions';
import TrafficPoliciesPage from './pages/TrafficPolicies';
import ForbiddenPage from './pages/Forbidden';

const PrivateRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, loading } = useAuth();

  if (loading) {
    return (
      <div style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        height: '100vh',
      }}>
        <Spin size="large" />
      </div>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
};

const PublicRoute: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isAuthenticated, loading } = useAuth();

  if (loading) {
    return (
      <div style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        height: '100vh',
      }}>
        <Spin size="large" />
      </div>
    );
  }

  if (isAuthenticated) {
    return <Navigate to="/" replace />;
  }

  return <>{children}</>;
};

const AppContent: React.FC = () => {
  return (
    <Routes>
      <Route
        path="/login"
        element={
          <PublicRoute>
            <LoginPage />
          </PublicRoute>
        }
      />
      <Route
        path="/forbidden"
        element={<ForbiddenPage />}
      />
      <Route
        path="/"
        element={
          <PrivateRoute>
            <MainLayout />
          </PrivateRoute>
        }
      >
        <Route index element={<DashboardPage />} />
        <Route path="monitoring" element={<MonitoringPage />} />
        <Route path="services" element={<ServicesPage />} />
        <Route path="routes" element={<RoutesPage />} />
        <Route path="upstreams" element={<UpstreamsPage />} />
        <Route path="targets" element={<TargetsPage />} />
        <Route path="consumers" element={<ConsumersPage />} />
        <Route path="plugins" element={<PluginsPage />} />
        <Route path="traffic-policies" element={<TrafficPoliciesPage />} />
        <Route path="revisions" element={<RevisionsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
};

const App: React.FC = () => {
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: {
          colorPrimary: '#1890ff',
        },
      }}
    >
      <BrowserRouter>
        <AuthProvider>
          <AppContent />
        </AuthProvider>
      </BrowserRouter>
    </ConfigProvider>
  );
};

export default App;
