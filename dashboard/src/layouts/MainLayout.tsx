import React, { useMemo } from 'react';
import { Layout, Menu, Dropdown, Avatar, Button, theme, Tag, Space } from 'antd';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import {
  DashboardOutlined,
  ApiOutlined,
  GatewayOutlined,
  ClusterOutlined,
  DeploymentUnitOutlined,
  UserOutlined,
  ToolOutlined,
  HistoryOutlined,
  LogoutOutlined,
  SettingOutlined,
  SafetyCertificateOutlined,
  ExperimentOutlined,
  BarChartOutlined,
} from '@ant-design/icons';
import { useAuth } from '../contexts/AuthContext';
import { Permissions } from '../types';

const { Header, Sider, Content } = Layout;

interface MenuItem {
  key: string;
  icon: React.ReactNode;
  label: string;
  requiredPermission?: string;
  requiredRole?: string;
}

const baseMenuItems: MenuItem[] = [
  {
    key: '/',
    icon: <DashboardOutlined />,
    label: '监控概览',
  },
  {
    key: '/monitoring',
    icon: <BarChartOutlined />,
    label: '运行监控',
  },
  {
    key: '/routes',
    icon: <ApiOutlined />,
    label: '路由管理',
    requiredPermission: Permissions.ROUTES_READ,
  },
  {
    key: '/services',
    icon: <GatewayOutlined />,
    label: '服务管理',
    requiredPermission: Permissions.SERVICES_READ,
  },
  {
    key: '/upstreams',
    icon: <ClusterOutlined />,
    label: '上游管理',
    requiredPermission: Permissions.UPSTREAMS_READ,
  },
  {
    key: '/targets',
    icon: <DeploymentUnitOutlined />,
    label: '目标管理',
    requiredPermission: Permissions.TARGETS_READ,
  },
  {
    key: '/consumers',
    icon: <UserOutlined />,
    label: '消费者管理',
    requiredPermission: Permissions.CONSUMERS_READ,
  },
  {
    key: '/plugins',
    icon: <ToolOutlined />,
    label: '插件管理',
    requiredPermission: Permissions.PLUGINS_READ,
  },
  {
    key: '/revisions',
    icon: <HistoryOutlined />,
    label: '版本发布',
    requiredPermission: Permissions.REVISIONS_READ,
  },
  {
    key: '/traffic-policies',
    icon: <ExperimentOutlined />,
    label: '流量策略',
    requiredPermission: Permissions.TRAFFIC_POLICIES_READ,
  },
];

const MainLayout: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const { 
    user, 
    logout, 
    hasPermission, 
    hasRole, 
    isSuperAdmin,
    isTenantAdmin,
    tenantId,
  } = useAuth();
  const {
    token: { colorBgContainer },
  } = theme.useToken();

  const filteredMenuItems = useMemo(() => {
    return baseMenuItems.filter(item => {
      if (!item.requiredPermission && !item.requiredRole) {
        return true;
      }
      
      if (item.requiredRole) {
        return hasRole(item.requiredRole);
      }
      
      if (item.requiredPermission) {
        return hasPermission(item.requiredPermission);
      }
      
      return true;
    }).map(item => ({
      key: item.key,
      icon: item.icon,
      label: item.label,
    }));
  }, [hasPermission, hasRole]);

  const handleMenuClick = ({ key }: { key: string }) => {
    navigate(key);
  };

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  const getUserRoleTag = () => {
    if (isSuperAdmin) {
      return (
        <Tag color="purple" icon={<SafetyCertificateOutlined />}>
          超级管理员
        </Tag>
      );
    }
    if (isTenantAdmin) {
      return (
        <Tag color="blue">
          租户管理员
        </Tag>
      );
    }
    if (user?.roles && user.roles.length > 0) {
      return (
        <Tag color="green">
          {user.roles[0]}
        </Tag>
      );
    }
    return null;
  };

  const userMenuItems = [
    {
      key: 'user',
      icon: <UserOutlined />,
      label: (
        <Space direction="vertical" size={0}>
          <span>{user?.username}</span>
          {getUserRoleTag()}
          {tenantId && (
            <Tag color="cyan" style={{ marginTop: 4 }}>
              租户: {tenantId.slice(0, 8)}...
            </Tag>
          )}
        </Space>
      ),
      disabled: true,
    },
    {
      type: 'divider' as const,
    },
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: '退出登录',
      onClick: handleLogout,
    },
  ];

  const canPublish = hasPermission(Permissions.REVISIONS_PUBLISH) || isTenantAdmin;

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider width={220} theme="dark">
        <div style={{
          height: 64,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          borderBottom: '1px solid rgba(255,255,255,0.1)',
        }}>
          <DashboardOutlined style={{ fontSize: 24, color: '#fff', marginRight: 8 }} />
          <span style={{ color: '#fff', fontSize: 18, fontWeight: 'bold' }}>Portkey</span>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[location.pathname]}
          items={filteredMenuItems}
          onClick={handleMenuClick}
          style={{ borderRight: 0 }}
        />
      </Sider>
      <Layout>
        <Header style={{
          padding: '0 24px',
          background: colorBgContainer,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          boxShadow: '0 1px 4px rgba(0,0,0,0.1)',
        }}>
          <div style={{ display: 'flex', alignItems: 'center' }}>
            {isSuperAdmin && (
              <Tag color="purple" style={{ marginRight: 16, fontSize: 14, padding: '4px 12px' }}>
                <SafetyCertificateOutlined style={{ marginRight: 4 }} />
                超级管理员模式
              </Tag>
            )}
            {tenantId && !isSuperAdmin && (
              <Tag color="cyan" style={{ marginRight: 16, fontSize: 14, padding: '4px 12px' }}>
                租户 ID: {tenantId}
              </Tag>
            )}
          </div>
          <div style={{ display: 'flex', alignItems: 'center' }}>
            {canPublish && (
              <Button
                type="primary"
                style={{ marginRight: 16 }}
                onClick={() => navigate('/revisions')}
              >
                <SettingOutlined /> 配置发布
              </Button>
            )}
            <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
              <div style={{ cursor: 'pointer', display: 'flex', alignItems: 'center' }}>
                <Avatar icon={<UserOutlined />} style={{ marginRight: 8 }} />
                <span>{user?.username}</span>
              </div>
            </Dropdown>
          </div>
        </Header>
        <Content style={{ margin: 24, padding: 24, background: colorBgContainer, minHeight: 280 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
