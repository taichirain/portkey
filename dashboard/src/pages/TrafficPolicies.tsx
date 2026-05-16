import React, { useState, useEffect } from 'react';
import {
  Card,
  Table,
  Button,
  Modal,
  Form,
  Input,
  InputNumber,
  Select,
  Switch,
  Tag,
  Space,
  message,
  Popconfirm,
  Typography,
  Divider,
  Descriptions,
  Tabs,
  Alert,
} from 'antd';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  EyeOutlined,
  ExperimentOutlined,
} from '@ant-design/icons';
import { apiService } from '../services/api';
import * as types from '../types';
import dayjs from 'dayjs';

const { Title, Text } = Typography;
const { Option } = Select;
const { TextArea } = Input;

const POLICY_TYPES = [
  { value: 'header', label: 'Header 匹配' },
  { value: 'weight', label: '权重灰度' },
  { value: 'query', label: 'Query 参数' },
  { value: 'cookie', label: 'Cookie 匹配' },
  { value: 'ip', label: 'IP 匹配' },
  { value: 'path', label: '路径匹配' },
  { value: 'method', label: '方法匹配' },
  { value: 'consumer', label: '消费者匹配' },
  { value: 'tag', label: '标签匹配' },
  { value: 'compound', label: '复合条件 (AND/OR)' },
  { value: 'fallback', label: '回退策略 (健康检查)' },
];

const MATCH_OPERATORS = [
  { value: 'exact', label: '精确匹配' },
  { value: 'prefix', label: '前缀匹配' },
  { value: 'suffix', label: '后缀匹配' },
  { value: 'contains', label: '包含' },
  { value: 'regex', label: '正则表达式' },
];

interface FormValues {
  name?: string;
  route_id: string;
  priority?: number;
  type: types.TrafficPolicyType;
  target_service_id: string;
  enabled?: boolean;
  tags?: string[];
  header?: string;
  header_value?: string;
  header_operator?: types.MatchOperator;
  weight_percentage?: number;
  query_key?: string;
  query_value?: string;
  query_operator?: types.MatchOperator;
  cookie_name?: string;
  cookie_value?: string;
  cookie_operator?: types.MatchOperator;
  ip_list?: string;
  cidr_list?: string;
  ip_negate?: boolean;
  path_pattern?: string;
  path_operator?: types.MatchOperator;
  methods?: string[];
  method_negate?: boolean;
  match_config_json?: string;
  compound_operator?: types.CompoundOperator;
  compound_conditions?: string;
  fallback_min_healthy_targets?: number;
}

const TrafficPoliciesPage: React.FC = () => {
  const [policies, setPolicies] = useState<types.TrafficPolicy[]>([]);
  const [routes, setRoutes] = useState<types.Route[]>([]);
  const [services, setServices] = useState<types.Service[]>([]);
  const [loading, setLoading] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [modalVisible, setModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentPolicy, setCurrentPolicy] = useState<types.TrafficPolicy | null>(null);
  const [form] = Form.useForm<FormValues>();
  const [isEdit, setIsEdit] = useState(false);
  const [selectedType, setSelectedType] = useState<types.TrafficPolicyType>('header');

  const fetchPolicies = async (page = 1, pageSize = 10) => {
    setLoading(true);
    try {
      const response = await apiService.getTrafficPolicies(page, pageSize);
      setPolicies(response.items || []);
      setPagination({
        current: response.page,
        pageSize: response.page_size,
        total: response.total,
      });
    } catch (error) {
      console.error('Failed to fetch traffic policies:', error);
      message.error('获取流量策略列表失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchRoutes = async () => {
    try {
      const response = await apiService.getRoutes(1, 100);
      setRoutes(response.items || []);
    } catch (error) {
      console.error('Failed to fetch routes:', error);
    }
  };

  const fetchServices = async () => {
    try {
      const response = await apiService.getServices(1, 100);
      setServices(response.items || []);
    } catch (error) {
      console.error('Failed to fetch services:', error);
    }
  };

  useEffect(() => {
    fetchPolicies();
    fetchRoutes();
    fetchServices();
  }, []);

  const getRouteName = (routeId: string) => {
    const route = routes.find(r => r.id === routeId);
    return route ? route.name : routeId.slice(0, 8) + '...';
  };

  const getServiceName = (serviceId: string) => {
    const service = services.find(s => s.id === serviceId);
    return service ? service.name : serviceId.slice(0, 8) + '...';
  };

  const getPolicyTypeLabel = (type: string) => {
    const pt = POLICY_TYPES.find(p => p.value === type);
    return pt ? pt.label : type;
  };

  const formatMatchConfig = (type: string, config: Record<string, unknown>) => {
    switch (type) {
      case 'header':
        const h = config as unknown as types.HeaderMatchConfig;
        return `Header: ${h.header} = ${h.value} (${h.operator || 'exact'})`;
      case 'weight':
        const w = config as unknown as types.WeightMatchConfig;
        return `权重: ${w.percentage}%`;
      case 'query':
        const q = config as unknown as types.QueryMatchConfig;
        return `Query: ${q.key} = ${q.value}`;
      case 'path':
        const p = config as unknown as types.PathMatchConfig;
        return `路径: ${p.pattern}`;
      case 'method':
        const m = config as unknown as types.MethodMatchConfig;
        return `方法: ${m.methods?.join(', ')}`;
      case 'compound':
        const c = config as unknown as types.CompoundMatchConfig;
        const op = c.operator === 'and' ? 'AND' : 'OR';
        return `复合条件: ${op} (${c.conditions?.length || 0} 个子条件)`;
      case 'fallback':
        const f = config as unknown as types.FallbackMatchConfig;
        return `回退策略: 最小健康目标数 ${f.min_healthy_targets || 1}`;
      default:
        return JSON.stringify(config);
    }
  };

  const parseMatchConfigToForm = (type: string, config: Record<string, unknown>) => {
    const values: Partial<FormValues> = {};
    switch (type) {
      case 'header':
        const h = config as unknown as types.HeaderMatchConfig;
        values.header = h.header;
        values.header_value = h.value;
        values.header_operator = h.operator;
        break;
      case 'weight':
        const w = config as unknown as types.WeightMatchConfig;
        values.weight_percentage = w.percentage;
        break;
      case 'query':
        const q = config as unknown as types.QueryMatchConfig;
        values.query_key = q.key;
        values.query_value = q.value;
        values.query_operator = q.operator;
        break;
      case 'cookie':
        const ck = config as unknown as types.CookieMatchConfig;
        values.cookie_name = ck.name;
        values.cookie_value = ck.value;
        values.cookie_operator = ck.operator;
        break;
      case 'ip':
        const ip = config as unknown as types.IPMatchConfig;
        values.ip_list = ip.ip_list?.join('\n');
        values.cidr_list = ip.cidr_list?.join('\n');
        values.ip_negate = ip.negate;
        break;
      case 'path':
        const p = config as unknown as types.PathMatchConfig;
        values.path_pattern = p.pattern;
        values.path_operator = p.operator;
        break;
      case 'method':
        const m = config as unknown as types.MethodMatchConfig;
        values.methods = m.methods;
        values.method_negate = m.negate;
        break;
      case 'compound':
        const c = config as unknown as types.CompoundMatchConfig;
        values.compound_operator = c.operator;
        values.compound_conditions = JSON.stringify(c.conditions || [], null, 2);
        break;
      case 'fallback':
        const f = config as unknown as types.FallbackMatchConfig;
        values.fallback_min_healthy_targets = f.min_healthy_targets || 1;
        break;
      default:
        values.match_config_json = JSON.stringify(config, null, 2);
    }
    return values;
  };

  const buildMatchConfig = (values: FormValues): Record<string, unknown> => {
    switch (values.type) {
      case 'header':
        return {
          header: values.header,
          value: values.header_value,
          operator: values.header_operator || 'exact',
        } as unknown as Record<string, unknown>;
      case 'weight':
        return {
          percentage: values.weight_percentage || 10,
        } as unknown as Record<string, unknown>;
      case 'query':
        return {
          key: values.query_key,
          value: values.query_value,
          operator: values.query_operator || 'exact',
        } as unknown as Record<string, unknown>;
      case 'cookie':
        return {
          name: values.cookie_name,
          value: values.cookie_value,
          operator: values.cookie_operator || 'exact',
        } as unknown as Record<string, unknown>;
      case 'ip':
        return {
          ip_list: values.ip_list?.split('\n').map(s => s.trim()).filter(Boolean),
          cidr_list: values.cidr_list?.split('\n').map(s => s.trim()).filter(Boolean),
          negate: values.ip_negate || false,
        } as unknown as Record<string, unknown>;
      case 'path':
        return {
          pattern: values.path_pattern,
          operator: values.path_operator || 'exact',
        } as unknown as Record<string, unknown>;
      case 'method':
        return {
          methods: values.methods || [],
          negate: values.method_negate || false,
        } as unknown as Record<string, unknown>;
      case 'compound':
        let conditions: types.CompoundCondition[] = [];
        if (values.compound_conditions) {
          try {
            conditions = JSON.parse(values.compound_conditions);
          } catch {
            conditions = [];
          }
        }
        return {
          operator: values.compound_operator || 'and',
          conditions: conditions,
        } as unknown as Record<string, unknown>;
      case 'fallback':
        return {
          fallback_service_id: values.target_service_id,
          min_healthy_targets: values.fallback_min_healthy_targets || 1,
        } as unknown as Record<string, unknown>;
      default:
        if (values.match_config_json) {
          try {
            return JSON.parse(values.match_config_json);
          } catch {
            return {};
          }
        }
        return {};
    }
  };

  const handleAdd = () => {
    setIsEdit(false);
    setCurrentPolicy(null);
    setSelectedType('header');
    form.resetFields();
    form.setFieldsValue({
      priority: 100,
      type: 'header',
      enabled: true,
      weight_percentage: 10,
      header_operator: 'exact',
      query_operator: 'exact',
      path_operator: 'exact',
      cookie_operator: 'exact',
    });
    setModalVisible(true);
  };

  const handleEdit = (record: types.TrafficPolicy) => {
    setIsEdit(true);
    setCurrentPolicy(record);
    setSelectedType(record.type);
    form.setFieldsValue({
      name: record.name,
      route_id: record.route_id,
      priority: record.priority,
      type: record.type,
      target_service_id: record.target_service_id,
      enabled: record.enabled,
      tags: record.tags,
      ...parseMatchConfigToForm(record.type, record.match_config),
    });
    setModalVisible(true);
  };

  const handleDetail = (record: types.TrafficPolicy) => {
    setCurrentPolicy(record);
    setDetailVisible(true);
  };

  const handleDelete = async (id: string) => {
    try {
      await apiService.deleteTrafficPolicy(id);
      message.success('删除成功');
      fetchPolicies(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to delete traffic policy:', error);
      message.error('删除失败');
    }
  };

  const handleSubmit = async (values: FormValues) => {
    try {
      const matchConfig = buildMatchConfig(values);
      const request: types.CreateTrafficPolicyRequest = {
        name: values.name,
        route_id: values.route_id,
        priority: values.priority,
        type: values.type,
        match_config: matchConfig,
        target_service_id: values.target_service_id,
        enabled: values.enabled,
        tags: values.tags,
      };

      if (isEdit && currentPolicy) {
        const updateReq: types.UpdateTrafficPolicyRequest = request;
        await apiService.updateTrafficPolicy(currentPolicy.id, updateReq);
        message.success('更新成功');
      } else {
        await apiService.createTrafficPolicy(request);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchPolicies(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to save traffic policy:', error);
      message.error(isEdit ? '更新失败' : '创建失败');
    }
  };

  const handleTypeChange = (type: types.TrafficPolicyType) => {
    setSelectedType(type);
  };

  const renderMatchConfigForm = () => {
    switch (selectedType) {
      case 'header':
        return (
          <>
            <Form.Item
              name="header"
              label="Header 名称"
              rules={[{ required: true, message: '请输入 Header 名称' }]}
            >
              <Input placeholder="例如: X-Canary" />
            </Form.Item>
            <Form.Item
              name="header_value"
              label="匹配值"
              rules={[{ required: true, message: '请输入匹配值' }]}
            >
              <Input placeholder="例如: beta" />
            </Form.Item>
            <Form.Item name="header_operator" label="匹配方式">
              <Select>
                {MATCH_OPERATORS.map(op => (
                  <Option key={op.value} value={op.value}>{op.label}</Option>
                ))}
              </Select>
            </Form.Item>
          </>
        );
      case 'weight':
        return (
          <Form.Item
            name="weight_percentage"
            label="流量百分比"
            rules={[
              { required: true, message: '请输入百分比' },
              { type: 'number', min: 1, max: 100, message: '百分比范围 1-100' },
            ]}
          >
            <InputNumber min={1} max={100} style={{ width: '100%' }} addonAfter="%" />
          </Form.Item>
        );
      case 'query':
        return (
          <>
            <Form.Item
              name="query_key"
              label="Query 参数名"
              rules={[{ required: true, message: '请输入参数名' }]}
            >
              <Input placeholder="例如: version" />
            </Form.Item>
            <Form.Item
              name="query_value"
              label="匹配值"
              rules={[{ required: true, message: '请输入匹配值' }]}
            >
              <Input placeholder="例如: v2" />
            </Form.Item>
            <Form.Item name="query_operator" label="匹配方式">
              <Select>
                {MATCH_OPERATORS.map(op => (
                  <Option key={op.value} value={op.value}>{op.label}</Option>
                ))}
              </Select>
            </Form.Item>
          </>
        );
      case 'path':
        return (
          <>
            <Form.Item
              name="path_pattern"
              label="路径模式"
              rules={[{ required: true, message: '请输入路径模式' }]}
            >
              <Input placeholder="例如: /api/v2/*" />
            </Form.Item>
            <Form.Item name="path_operator" label="匹配方式">
              <Select>
                {MATCH_OPERATORS.map(op => (
                  <Option key={op.value} value={op.value}>{op.label}</Option>
                ))}
              </Select>
            </Form.Item>
          </>
        );
      case 'method':
        return (
          <>
            <Form.Item
              name="methods"
              label="HTTP 方法"
              rules={[{ required: true, message: '请选择至少一个方法' }]}
            >
              <Select mode="multiple" placeholder="选择方法">
                <Option value="GET">GET</Option>
                <Option value="POST">POST</Option>
                <Option value="PUT">PUT</Option>
                <Option value="DELETE">DELETE</Option>
                <Option value="PATCH">PATCH</Option>
                <Option value="HEAD">HEAD</Option>
                <Option value="OPTIONS">OPTIONS</Option>
              </Select>
            </Form.Item>
            <Form.Item name="method_negate" label="反向匹配" valuePropName="checked">
              <Switch checkedChildren="是" unCheckedChildren="否" />
            </Form.Item>
          </>
        );
      case 'cookie':
        return (
          <>
            <Form.Item
              name="cookie_name"
              label="Cookie 名称"
              rules={[{ required: true, message: '请输入 Cookie 名称' }]}
            >
              <Input placeholder="例如: canary_group" />
            </Form.Item>
            <Form.Item name="cookie_value" label="匹配值">
              <Input placeholder="例如: beta" />
            </Form.Item>
            <Form.Item name="cookie_operator" label="匹配方式">
              <Select>
                {MATCH_OPERATORS.map(op => (
                  <Option key={op.value} value={op.value}>{op.label}</Option>
                ))}
              </Select>
            </Form.Item>
          </>
        );
      case 'ip':
        return (
          <>
            <Form.Item name="ip_list" label="IP 列表 (每行一个)">
              <TextArea rows={3} placeholder="例如:&#10;192.168.1.100&#10;192.168.1.101" />
            </Form.Item>
            <Form.Item name="cidr_list" label="CIDR 列表 (每行一个)">
              <TextArea rows={3} placeholder="例如:&#10;192.168.1.0/24&#10;10.0.0.0/8" />
            </Form.Item>
            <Form.Item name="ip_negate" label="反向匹配" valuePropName="checked">
              <Switch checkedChildren="是" unCheckedChildren="否" />
            </Form.Item>
          </>
        );
      case 'compound':
        return (
          <>
            <Form.Item name="compound_operator" label="组合方式">
              <Select>
                <Option value="and">AND (所有条件都匹配)</Option>
                <Option value="or">OR (任一条件匹配)</Option>
              </Select>
            </Form.Item>
            <Form.Item
              name="compound_conditions"
              label="子条件配置 (JSON 数组)"
              help='例如: [{"type": "header", "match_config": {"header": "X-Canary", "value": "beta", "operator": "exact"}}]'
            >
              <TextArea
                rows={12}
                placeholder={JSON.stringify([
                  {
                    type: 'header',
                    match_config: {
                      header: 'X-Canary',
                      value: 'beta',
                      operator: 'exact'
                    }
                  },
                  {
                    type: 'weight',
                    match_config: {
                      percentage: 10
                    }
                  }
                ], null, 2)}
              />
            </Form.Item>
            <Card size="small" type="inner" title="支持的子条件类型">
              <ul style={{ margin: 0, paddingLeft: 20 }}>
                <li><Text code>header</Text> - Header 匹配: {"{\"header\": \"X-Canary\", \"value\": \"beta\", \"operator\": \"exact\"}"}</li>
                <li><Text code>weight</Text> - 权重: {"{\"percentage\": 10}"}</li>
                <li><Text code>query</Text> - Query 参数: {"{\"key\": \"version\", \"value\": \"v2\", \"operator\": \"exact\"}"}</li>
                <li><Text code>path</Text> - 路径: {"{\"pattern\": \"/api/v2/*\", \"operator\": \"prefix\"}"}</li>
                <li><Text code>method</Text> - 方法: {"{\"methods\": [\"GET\", \"POST\"], \"negate\": false}"}</li>
                <li><Text code>cookie</Text> - Cookie: {"{\"name\": \"canary\", \"value\": \"beta\", \"operator\": \"exact\"}"}</li>
                <li><Text code>ip</Text> - IP: {"{\"ip_list\": [\"192.168.1.100\"], \"cidr_list\": [], \"negate\": false}"}</li>
                <li><Text code>consumer</Text> - 消费者: {"{\"consumer_ids\": [\"consumer-uuid\"], \"tags\": [], \"match_mode\": \"any\"}"}</li>
                <li><Text code>tag</Text> - 标签: {"{\"tags\": [\"vip\"], \"match_mode\": \"any\", \"source_type\": \"consumer\"}"}</li>
              </ul>
            </Card>
          </>
        );
      case 'fallback':
        return (
          <>
            <Form.Item
              name="fallback_min_healthy_targets"
              label="最小健康目标数"
              help="当原服务的健康目标数小于此值时，自动切换到目标服务"
            >
              <InputNumber min={1} style={{ width: '100%' }} placeholder="默认: 1" />
            </Form.Item>
            <Alert
              message="回退策略说明"
              description={
                <div>
                  <p>回退策略用于在原服务不健康时自动切换到备用服务：</p>
                  <ol>
                    <li>策略类型选择 <Text code>fallback</Text></li>
                    <li>目标服务选择作为备用的服务</li>
                    <li>当原服务的健康目标数小于"最小健康目标数"时触发回退</li>
                    <li>请求将被路由到目标服务</li>
                  </ol>
                  <p><Text strong>注意：</Text>此策略依赖健康检查结果。请确保已启用健康检查插件。</p>
                </div>
              }
              type="info"
              showIcon
            />
          </>
        );
      default:
        return (
          <Form.Item
            name="match_config_json"
            label="匹配配置 (JSON)"
            rules={[{ required: true, message: '请输入 JSON 配置' }]}
          >
            <TextArea rows={8} placeholder='{"key": "value"}' />
          </Form.Item>
        );
    }
  };

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (text: string, record: types.TrafficPolicy) => (
        <a onClick={() => handleDetail(record)}>{text || '-'}</a>
      ),
    },
    {
      title: '策略类型',
      dataIndex: 'type',
      key: 'type',
      render: (type: string) => (
        <Tag color={type === 'weight' ? 'orange' : type === 'header' ? 'blue' : 'default'}>
          {getPolicyTypeLabel(type)}
        </Tag>
      ),
    },
    {
      title: '绑定路由',
      dataIndex: 'route_id',
      key: 'route_id',
      render: (routeId: string) => getRouteName(routeId),
    },
    {
      title: '目标服务',
      dataIndex: 'target_service_id',
      key: 'target_service_id',
      render: (serviceId: string) => getServiceName(serviceId),
    },
    {
      title: '优先级',
      dataIndex: 'priority',
      key: 'priority',
      render: (priority: number) => priority,
    },
    {
      title: '匹配条件',
      dataIndex: 'match_config',
      key: 'match_config',
      render: (config: Record<string, unknown>, record: types.TrafficPolicy) => (
        <Text code ellipsis style={{ maxWidth: 200 }}>
          {formatMatchConfig(record.type, config)}
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
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (time: string) => dayjs(time).format('YYYY-MM-DD HH:mm:ss'),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, record: types.TrafficPolicy) => (
        <Space size="small">
          <Button
            type="text"
            icon={<EyeOutlined />}
            onClick={() => handleDetail(record)}
          />
          <Button
            type="text"
            icon={<EditOutlined />}
            onClick={() => handleEdit(record)}
          />
          <Popconfirm
            title="确定要删除这个流量策略吗？"
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
          >
            <Button type="text" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <Card
        title={
          <Space>
            <ExperimentOutlined />
            流量策略管理
          </Space>
        }
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
            新建策略
          </Button>
        }
      >
        <Table
          columns={columns}
          dataSource={policies}
          rowKey="id"
          loading={loading}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => fetchPolicies(page, pageSize),
          }}
        />
      </Card>

      <Modal
        title={isEdit ? '编辑流量策略' : '新建流量策略'}
        open={modalVisible}
        onCancel={() => setModalVisible(false)}
        onOk={() => form.submit()}
        width={700}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
        >
          <Tabs
            activeKey={selectedType}
            onChange={(key) => handleTypeChange(key as types.TrafficPolicyType)}
          >
            <Tabs.TabPane tab="基本配置" key="basic">
              <Form.Item
                name="name"
                label="策略名称"
              >
                <Input placeholder="请输入策略名称" />
              </Form.Item>

              <Form.Item
                name="route_id"
                label="绑定路由"
                rules={[{ required: true, message: '请选择绑定的路由' }]}
              >
                <Select placeholder="选择路由">
                  {routes.map(route => (
                    <Option key={route.id} value={route.id}>{route.name}</Option>
                  ))}
                </Select>
              </Form.Item>

              <Form.Item
                name="type"
                label="策略类型"
                rules={[{ required: true, message: '请选择策略类型' }]}
              >
                <Select onChange={handleTypeChange}>
                  {POLICY_TYPES.map(pt => (
                    <Option key={pt.value} value={pt.value}>{pt.label}</Option>
                  ))}
                </Select>
              </Form.Item>

              <Form.Item
                name="target_service_id"
                label="目标服务"
                rules={[{ required: true, message: '请选择目标服务' }]}
              >
                <Select placeholder="选择目标服务">
                  {services.map(service => (
                    <Option key={service.id} value={service.id}>{service.name}</Option>
                  ))}
                </Select>
              </Form.Item>

              <Form.Item name="priority" label="优先级">
                <InputNumber min={0} style={{ width: '100%' }} placeholder="数字越小优先级越高" />
              </Form.Item>

              <Form.Item name="enabled" label="启用状态" valuePropName="checked">
                <Switch checkedChildren="启用" unCheckedChildren="禁用" />
              </Form.Item>

              <Form.Item name="tags" label="标签">
                <Select
                  mode="tags"
                  placeholder="输入标签后回车添加"
                  style={{ width: '100%' }}
                />
              </Form.Item>
            </Tabs.TabPane>
          </Tabs>

          <Divider />

          <Title level={5}>匹配条件配置</Title>
          {renderMatchConfigForm()}
        </Form>
      </Modal>

      <Modal
        title="流量策略详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={[
          <Button key="close" onClick={() => setDetailVisible(false)}>
            关闭
          </Button>,
        ]}
        width={700}
      >
        {currentPolicy && (
          <div>
            <Descriptions bordered column={2}>
              <Descriptions.Item label="ID" span={2}>
                <Text code>{currentPolicy.id}</Text>
              </Descriptions.Item>
              <Descriptions.Item label="名称">
                {currentPolicy.name || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="策略类型">
                <Tag color={currentPolicy.type === 'weight' ? 'orange' : 'blue'}>
                  {getPolicyTypeLabel(currentPolicy.type)}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="绑定路由">
                {getRouteName(currentPolicy.route_id)}
              </Descriptions.Item>
              <Descriptions.Item label="目标服务">
                {getServiceName(currentPolicy.target_service_id)}
              </Descriptions.Item>
              <Descriptions.Item label="优先级">
                {currentPolicy.priority}
              </Descriptions.Item>
              <Descriptions.Item label="状态">
                <Tag color={currentPolicy.enabled ? 'green' : 'default'}>
                  {currentPolicy.enabled ? '启用' : '禁用'}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="创建时间">
                {dayjs(currentPolicy.created_at).format('YYYY-MM-DD HH:mm:ss')}
              </Descriptions.Item>
              <Descriptions.Item label="更新时间">
                {dayjs(currentPolicy.updated_at).format('YYYY-MM-DD HH:mm:ss')}
              </Descriptions.Item>
              <Descriptions.Item label="标签" span={2}>
                {currentPolicy.tags && currentPolicy.tags.length > 0
                  ? currentPolicy.tags.map((tag: string) => <Tag key={tag}>{tag}</Tag>)
                  : '-'}
              </Descriptions.Item>
            </Descriptions>

            <Divider />

            <Title level={5}>匹配配置</Title>
            <Card size="small" type="inner">
              <Text code style={{ whiteSpace: 'pre-wrap' }}>
                {JSON.stringify(currentPolicy.match_config, null, 2)}
              </Text>
            </Card>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default TrafficPoliciesPage;
