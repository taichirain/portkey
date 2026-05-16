import React, { useState, useEffect } from 'react';
import {
  Card,
  Row,
  Col,
  Statistic,
  Table,
  Tag,
  Space,
  Typography,
  Divider,
  Button,
  message,
} from 'antd';
import {
  DashboardOutlined,
  ApiOutlined,
  GatewayOutlined,
  ClusterOutlined,
  DeploymentUnitOutlined,
  UserOutlined,
  ToolOutlined,
  HistoryOutlined,
  RocketOutlined,
  SyncOutlined,
  CheckCircleOutlined,
  ExclamationCircleOutlined,
} from '@ant-design/icons';
import { apiService } from '../services/api';
import * as types from '../types';
import dayjs from 'dayjs';
import { useNavigate } from 'react-router-dom';

const { Title, Text } = Typography;

const DashboardPage: React.FC = () => {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [stats, setStats] = useState({
    services: 0,
    routes: 0,
    upstreams: 0,
    targets: 0,
    consumers: 0,
    plugins: 0,
  });
  const [activeRevision, setActiveRevision] = useState<types.ActiveRevisionResponse | null>(null);
  const [recentRoutes, setRecentRoutes] = useState<types.Route[]>([]);
  const [recentServices, setRecentServices] = useState<types.Service[]>([]);

  const fetchStats = async () => {
    setLoading(true);
    try {
      const [servicesRes, routesRes, upstreamsRes, targetsRes, consumersRes, pluginsRes] = await Promise.all([
        apiService.getServices(1, 1),
        apiService.getRoutes(1, 1),
        apiService.getUpstreams(1, 1),
        apiService.getTargets(1, 1),
        apiService.getConsumers(1, 1),
        apiService.getPlugins(1, 1),
      ]);

      setStats({
        services: servicesRes.total,
        routes: routesRes.total,
        upstreams: upstreamsRes.total,
        targets: targetsRes.total,
        consumers: consumersRes.total,
        plugins: pluginsRes.total,
      });

      const [recentRoutesRes, recentServicesRes] = await Promise.all([
        apiService.getRoutes(1, 5),
        apiService.getServices(1, 5),
      ]);
      setRecentRoutes(recentRoutesRes.items || []);
      setRecentServices(recentServicesRes.items || []);

      try {
        const activeRev = await apiService.getActiveRevision();
        setActiveRevision(activeRev);
      } catch (e) {
        console.log('No active revision');
      }
    } catch (error) {
      console.error('Failed to fetch stats:', error);
      message.error('获取统计数据失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchStats();
  }, []);

  const routeColumns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (text: string) => text || '-',
    },
    {
      title: '路径',
      dataIndex: 'paths',
      key: 'paths',
      render: (paths: string[]) => (
        <Space wrap>
          {paths?.slice(0, 2).map((p, i) => (
            <Tag key={i} color="blue">{p}</Tag>
          ))}
          {paths?.length > 2 && <Tag>+{paths.length - 2}</Tag>}
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      key: 'enabled',
      render: (enabled: boolean) => (
        <Tag color={enabled ? 'green' : 'default'}>
          {enabled ? '启用' : '禁用'}
        </Tag>
      ),
    },
  ];

  const serviceColumns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
    },
    {
      title: '地址',
      key: 'address',
      render: (_: unknown, record: types.Service) => (
        <Text code>
          {record.protocol}://{record.host || '-'}{record.port ? `:${record.port}` : ''}{record.path || ''}
        </Text>
      ),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      key: 'enabled',
      render: (enabled: boolean) => (
        <Tag color={enabled ? 'green' : 'default'}>
          {enabled ? '启用' : '禁用'}
        </Tag>
      ),
    },
  ];

  return (
    <div>
      <Card
        title={
          <Space>
            <DashboardOutlined />
            监控概览
          </Space>
        }
        extra={
          <Button icon={<SyncOutlined />} onClick={fetchStats} loading={loading}>
            刷新
          </Button>
        }
      >
        <Row gutter={[16, 16]}>
          <Col xs={12} sm={12} md={8} lg={4}>
            <Card hoverable onClick={() => navigate('/services')}>
              <Statistic
                title="服务"
                value={stats.services}
                prefix={<GatewayOutlined />}
                valueStyle={{ color: '#1890ff' }}
              />
            </Card>
          </Col>
          <Col xs={12} sm={12} md={8} lg={4}>
            <Card hoverable onClick={() => navigate('/routes')}>
              <Statistic
                title="路由"
                value={stats.routes}
                prefix={<ApiOutlined />}
                valueStyle={{ color: '#52c41a' }}
              />
            </Card>
          </Col>
          <Col xs={12} sm={12} md={8} lg={4}>
            <Card hoverable onClick={() => navigate('/upstreams')}>
              <Statistic
                title="上游"
                value={stats.upstreams}
                prefix={<ClusterOutlined />}
                valueStyle={{ color: '#fa8c16' }}
              />
            </Card>
          </Col>
          <Col xs={12} sm={12} md={8} lg={4}>
            <Card hoverable onClick={() => navigate('/targets')}>
              <Statistic
                title="目标节点"
                value={stats.targets}
                prefix={<DeploymentUnitOutlined />}
                valueStyle={{ color: '#722ed1' }}
              />
            </Card>
          </Col>
          <Col xs={12} sm={12} md={8} lg={4}>
            <Card hoverable onClick={() => navigate('/consumers')}>
              <Statistic
                title="消费者"
                value={stats.consumers}
                prefix={<UserOutlined />}
                valueStyle={{ color: '#eb2f96' }}
              />
            </Card>
          </Col>
          <Col xs={12} sm={12} md={8} lg={4}>
            <Card hoverable onClick={() => navigate('/plugins')}>
              <Statistic
                title="插件"
                value={stats.plugins}
                prefix={<ToolOutlined />}
                valueStyle={{ color: '#13c2c2' }}
              />
            </Card>
          </Col>
        </Row>
      </Card>

      <Divider />

      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card
            title={
              <Space>
                <HistoryOutlined />
                当前配置版本
              </Space>
            }
            extra={
              <Button
                type="primary"
                size="small"
                icon={<RocketOutlined />}
                onClick={() => navigate('/revisions')}
              >
                发布管理
              </Button>
            }
            style={{ minHeight: 200 }}
          >
            {activeRevision ? (
              <div>
                <Space align="center" style={{ marginBottom: 16 }}>
                  <CheckCircleOutlined style={{ fontSize: 24, color: '#52c41a' }} />
                  <Title level={4} style={{ margin: 0 }}>
                    {activeRevision.version}
                  </Title>
                  <Tag color="green">已发布</Tag>
                </Space>
                <Divider style={{ margin: '12px 0' }} />
                <Space direction="vertical" style={{ width: '100%' }}>
                  <div>
                    <Text strong>描述: </Text>
                    <Text>{activeRevision.description || '-'}</Text>
                  </div>
                  <div>
                    <Text strong>发布时间: </Text>
                    <Text>
                      {activeRevision.published_at
                        ? dayjs(activeRevision.published_at).format('YYYY-MM-DD HH:mm:ss')
                        : '-'}
                    </Text>
                  </div>
                  <div>
                    <Text strong>快照内容: </Text>
                    <Space>
                      <Tag>服务: {activeRevision.snapshot?.services?.length || 0}</Tag>
                      <Tag>路由: {activeRevision.snapshot?.routes?.length || 0}</Tag>
                      <Tag>上游: {activeRevision.snapshot?.upstreams?.length || 0}</Tag>
                    </Space>
                  </div>
                </Space>
              </div>
            ) : (
              <div style={{ textAlign: 'center', padding: '40px 0' }}>
                <ExclamationCircleOutlined style={{ fontSize: 48, color: '#faad14' }} />
                <Title level={5} style={{ marginTop: 16 }}>
                  暂无活跃版本
                </Title>
                <Text type="secondary">请前往发布页面创建并发布一个版本</Text>
                <div style={{ marginTop: 16 }}>
                  <Button
                    type="primary"
                    icon={<RocketOutlined />}
                    onClick={() => navigate('/revisions')}
                  >
                    去发布
                  </Button>
                </div>
              </div>
            )}
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card
            title={
              <Space>
                <ApiOutlined />
                快速操作
              </Space>
            }
          >
            <Row gutter={[16, 16]}>
              <Col xs={12}>
                <Button
                  type="primary"
                  ghost
                  block
                  icon={<GatewayOutlined />}
                  onClick={() => navigate('/services')}
                >
                  管理服务
                </Button>
              </Col>
              <Col xs={12}>
                <Button
                  type="primary"
                  ghost
                  block
                  icon={<ApiOutlined />}
                  onClick={() => navigate('/routes')}
                >
                  管理路由
                </Button>
              </Col>
              <Col xs={12}>
                <Button
                  type="primary"
                  ghost
                  block
                  icon={<ClusterOutlined />}
                  onClick={() => navigate('/upstreams')}
                >
                  管理上游
                </Button>
              </Col>
              <Col xs={12}>
                <Button
                  type="primary"
                  ghost
                  block
                  icon={<ToolOutlined />}
                  onClick={() => navigate('/plugins')}
                >
                  管理插件
                </Button>
              </Col>
              <Col xs={24}>
                <Button
                  type="primary"
                  block
                  icon={<RocketOutlined />}
                  onClick={() => navigate('/revisions')}
                  size="large"
                >
                  发布新版本
                </Button>
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>

      <Divider />

      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card
            title={
              <Space>
                <ApiOutlined />
                最近路由 ({recentRoutes.length})
              </Space>
            }
            extra={
              <Button size="small" onClick={() => navigate('/routes')}>
                查看全部
              </Button>
            }
          >
            {recentRoutes.length > 0 ? (
              <Table
                columns={routeColumns}
                dataSource={recentRoutes}
                rowKey="id"
                pagination={false}
                size="small"
              />
            ) : (
              <div style={{ textAlign: 'center', padding: '20px 0' }}>
                <Text type="secondary">暂无路由</Text>
              </div>
            )}
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card
            title={
              <Space>
                <GatewayOutlined />
                最近服务 ({recentServices.length})
              </Space>
            }
            extra={
              <Button size="small" onClick={() => navigate('/services')}>
                查看全部
              </Button>
            }
          >
            {recentServices.length > 0 ? (
              <Table
                columns={serviceColumns}
                dataSource={recentServices}
                rowKey="id"
                pagination={false}
                size="small"
              />
            ) : (
              <div style={{ textAlign: 'center', padding: '20px 0' }}>
                <Text type="secondary">暂无服务</Text>
              </div>
            )}
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default DashboardPage;
