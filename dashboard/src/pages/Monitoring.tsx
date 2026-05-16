import React, { useState, useEffect, useCallback } from 'react';
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
  Switch,
  message,
  Tooltip,
  Progress,
  Alert,
} from 'antd';
import {
  LineChartOutlined,
  SyncOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  WarningOutlined,
  DashboardOutlined,
  CloudServerOutlined,
  ApiOutlined,
  ThunderboltOutlined,
  BarChartOutlined,
} from '@ant-design/icons';
import { apiService } from '../services/api';
import * as types from '../types';

const { Title, Text } = Typography;

const formatUptime = (seconds: number): string => {
  if (seconds < 60) return `${Math.floor(seconds)} 秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时`;
  return `${Math.floor(seconds / 86400)} 天 ${Math.floor((seconds % 86400) / 3600)} 小时`;
};

const MonitoringPage: React.FC = () => {
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [metrics, setMetrics] = useState<types.MonitoringMetricsResponse | null>(null);
  const [dpStatus, setDpStatus] = useState<types.DPStatusResponse | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const [metricsRes, dpStatusRes] = await Promise.all([
        apiService.getMonitoringMetrics(),
        apiService.getDPStatus(),
      ]);
      setMetrics(metricsRes);
      setDpStatus(dpStatusRes);
    } catch (error) {
      console.error('Failed to fetch monitoring data:', error);
      message.error('获取监控数据失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useEffect(() => {
    let interval: NodeJS.Timeout | null = null;
    if (autoRefresh) {
      interval = setInterval(fetchData, 10000);
    }
    return () => {
      if (interval) clearInterval(interval);
    };
  }, [autoRefresh, fetchData]);

  const dpColumns = [
    {
      title: '实例名',
      dataIndex: 'name',
      key: 'name',
      render: (text: string) => (
        <Space>
          <CloudServerOutlined />
          <Text strong>{text}</Text>
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'online',
      key: 'online',
      render: (online: boolean, record: types.DPStatusInstance) => {
        if (online) {
          if (record.revision_mismatch) {
            return (
              <Tooltip title="版本不一致">
                <Tag icon={<WarningOutlined />} color="warning">
                  在线
                </Tag>
              </Tooltip>
            );
          }
          return (
            <Tag icon={<CheckCircleOutlined />} color="success">
              在线
            </Tag>
          );
        }
        return (
          <Tag icon={<CloseCircleOutlined />} color="error">
            离线
          </Tag>
        );
      },
    },
    {
      title: 'QPS',
      key: 'qps',
      render: (_: unknown, record: types.DPStatusInstance) => {
        const dpMetrics = metrics?.per_dp?.find(d => d.name === record.name);
        if (!dpMetrics?.online) return '-';
        return <Text strong>{dpMetrics.qps_1m.toFixed(1)}</Text>;
      },
    },
    {
      title: '错误率',
      key: 'error_rate',
      render: (_: unknown, record: types.DPStatusInstance) => {
        const dpMetrics = metrics?.per_dp?.find(d => d.name === record.name);
        if (!dpMetrics?.online) return '-';
        const rate = (dpMetrics.error_rate_1m * 100).toFixed(2);
        return (
          <Text strong style={{ color: dpMetrics.error_rate_1m > 0.01 ? '#ff4d4f' : undefined }}>
            {rate}%
          </Text>
        );
      },
    },
    {
      title: '平均延迟',
      key: 'latency',
      render: (_: unknown, record: types.DPStatusInstance) => {
        const dpMetrics = metrics?.per_dp?.find(d => d.name === record.name);
        if (!dpMetrics?.online) return '-';
        return <Text>{dpMetrics.avg_latency_ms_1m.toFixed(1)} ms</Text>;
      },
    },
    {
      title: '版本',
      key: 'version',
      render: (_: unknown, record: types.DPStatusInstance) => {
        if (!record.online) return '-';
        const shortRev = record.revision_id?.slice(0, 8) || '-';
        if (record.revision_mismatch) {
          return (
            <Tooltip title={`该 DP 版本与 CP 不一致: ${record.revision_id}`}>
              <Tag color="warning">{shortRev}</Tag>
            </Tooltip>
          );
        }
        return <Text code>{shortRev}</Text>;
      },
    },
    {
      title: '运行时间',
      key: 'uptime',
      render: (_: unknown, record: types.DPStatusInstance) => {
        const dpMetrics = metrics?.per_dp?.find(d => d.name === record.name);
        if (!dpMetrics?.online) return '-';
        return <Text>{formatUptime(dpMetrics.uptime_seconds)}</Text>;
      },
    },
  ];

  const aggregated = metrics?.aggregated;
  const onlineDPs = metrics?.per_dp?.filter(d => d.online).length || 0;
  const totalDPs = metrics?.per_dp?.length || 0;

  const getStatusPercent = () => {
    if (!aggregated?.status_distribution) return null;
    const dist = aggregated.status_distribution;
    const total = dist['2xx'] + dist['3xx'] + dist['4xx'] + dist['5xx'];
    if (total === 0) return null;
    return {
      '2xx': (dist['2xx'] / total) * 100,
      '3xx': (dist['3xx'] / total) * 100,
      '4xx': (dist['4xx'] / total) * 100,
      '5xx': (dist['5xx'] / total) * 100,
    };
  };

  const statusPercent = getStatusPercent();

  const hasNoDPs = !metrics?.per_dp || metrics.per_dp.length === 0;

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
          <Space>
            <Switch
              checkedChildren="自动"
              unCheckedChildren="关闭"
              checked={autoRefresh}
              onChange={setAutoRefresh}
            />
            <Button icon={<SyncOutlined />} onClick={fetchData} loading={loading}>
              刷新
            </Button>
          </Space>
        }
      >
        {hasNoDPs ? (
          <Alert
            message="未配置 DP 实例"
            description="请在 config.yaml 中添加 dp_instances 配置，或者检查 CP 是否能连接到 DP 实例。"
            type="warning"
            showIcon
          />
        ) : (
          <Row gutter={[16, 16]}>
            <Col xs={12} sm={12} md={8} lg={4}>
              <Card>
                <Statistic
                  title="当前 QPS"
                  value={aggregated?.qps_1m || 0}
                  precision={1}
                  prefix={<LineChartOutlined />}
                  suffix="req/s"
                  valueStyle={{ color: '#1890ff' }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={12} md={8} lg={4}>
              <Card>
                <Statistic
                  title="错误率"
                  value={(aggregated?.error_rate_1m || 0) * 100}
                  precision={2}
                  suffix="%"
                  prefix={<ThunderboltOutlined />}
                  valueStyle={{ color: (aggregated?.error_rate_1m || 0) > 0.01 ? '#ff4d4f' : '#52c41a' }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={12} md={8} lg={4}>
              <Card>
                <Statistic
                  title="平均延迟"
                  value={aggregated?.avg_latency_ms_1m || 0}
                  precision={1}
                  suffix="ms"
                  prefix={<ApiOutlined />}
                  valueStyle={{ color: '#722ed1' }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={12} md={8} lg={4}>
              <Card>
                <Statistic
                  title="活跃连接"
                  value={aggregated?.requests_active || 0}
                  prefix={<CloudServerOutlined />}
                  valueStyle={{ color: '#fa8c16' }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={12} md={8} lg={4}>
              <Card>
                <Statistic
                  title="DP 在线/总数"
                  value={onlineDPs}
                  suffix={`/ ${totalDPs}`}
                  prefix={<BarChartOutlined />}
                  valueStyle={{ color: onlineDPs === totalDPs ? '#52c41a' : '#faad14' }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={12} md={8} lg={4}>
              <Card>
                <Statistic
                  title="累计请求"
                  value={aggregated?.requests_total || 0}
                  prefix={<ApiOutlined />}
                  valueStyle={{ color: '#13c2c2' }}
                />
              </Card>
            </Col>
          </Row>
        )}
      </Card>

      <Divider />

      <Card
        title={
          <Space>
            <CloudServerOutlined />
            DP 实例状态
          </Space>
        }
      >
        <Table
          columns={dpColumns}
          dataSource={dpStatus?.dp_instances || []}
          rowKey="name"
          pagination={false}
          loading={loading}
          locale={{
            emptyText: '暂无 DP 实例，请在 config.yaml 中配置 dp_instances',
          }}
        />
      </Card>

      <Divider />

      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card
            title={
              <Space>
                <BarChartOutlined />
                状态码分布
              </Space>
            }
          >
            {statusPercent ? (
              <Space direction="vertical" style={{ width: '100%' }}>
                <div>
                  <Space>
                    <Tag color="success">2xx</Tag>
                    <Text strong>{aggregated?.status_distribution['2xx']}</Text>
                    <Text type="secondary">{statusPercent['2xx'].toFixed(1)}%</Text>
                  </Space>
                  <Progress
                    percent={statusPercent['2xx']}
                    showInfo={false}
                    strokeColor="#52c41a"
                    style={{ marginTop: 8 }}
                  />
                </div>
                <div>
                  <Space>
                    <Tag color="blue">3xx</Tag>
                    <Text strong>{aggregated?.status_distribution['3xx']}</Text>
                    <Text type="secondary">{statusPercent['3xx'].toFixed(1)}%</Text>
                  </Space>
                  <Progress
                    percent={statusPercent['3xx']}
                    showInfo={false}
                    strokeColor="#1890ff"
                    style={{ marginTop: 8 }}
                  />
                </div>
                <div>
                  <Space>
                    <Tag color="orange">4xx</Tag>
                    <Text strong>{aggregated?.status_distribution['4xx']}</Text>
                    <Text type="secondary">{statusPercent['4xx'].toFixed(1)}%</Text>
                  </Space>
                  <Progress
                    percent={statusPercent['4xx']}
                    showInfo={false}
                    strokeColor="#fa8c16"
                    style={{ marginTop: 8 }}
                  />
                </div>
                <div>
                  <Space>
                    <Tag color="error">5xx</Tag>
                    <Text strong>{aggregated?.status_distribution['5xx']}</Text>
                    <Text type="secondary">{statusPercent['5xx'].toFixed(1)}%</Text>
                  </Space>
                  <Progress
                    percent={statusPercent['5xx']}
                    showInfo={false}
                    strokeColor="#ff4d4f"
                    style={{ marginTop: 8 }}
                  />
                </div>
              </Space>
            ) : (
              <div style={{ textAlign: 'center', padding: '40px 0' }}>
                <Text type="secondary">暂无数据</Text>
              </div>
            )}
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card
            title={
              <Space>
                <ThunderboltOutlined />
                功能计数器
              </Space>
            }
          >
            <Row gutter={[16, 16]}>
              <Col span={12}>
                <Card size="small">
                  <Statistic
                    title="限流命中"
                    value={aggregated?.rate_limited_total || 0}
                    valueStyle={{ color: '#ff4d4f' }}
                  />
                </Card>
              </Col>
              <Col span={12}>
                <Card size="small">
                  <Statistic
                    title="流量策略命中"
                    value={aggregated?.policy_hit_total || 0}
                    valueStyle={{ color: '#722ed1' }}
                  />
                </Card>
              </Col>
              <Col span={12}>
                <Card size="small">
                  <Statistic
                    title="累计错误"
                    value={aggregated?.errors_total || 0}
                    valueStyle={{ color: '#fa8c16' }}
                  />
                </Card>
              </Col>
              <Col span={12}>
                <Card size="small">
                  <Statistic
                    title="当前活跃"
                    value={aggregated?.requests_active || 0}
                    valueStyle={{ color: '#1890ff' }}
                  />
                </Card>
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default MonitoringPage;
