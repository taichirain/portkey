import React, { useState, useEffect } from 'react';
import {
  Card,
  Table,
  Button,
  Modal,
  Form,
  Input,
  Select,
  Switch,
  Tag,
  Space,
  message,
  Popconfirm,
  Typography,
  Divider,
  Radio,
} from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, EyeOutlined } from '@ant-design/icons';
import { apiService } from '../services/api';
import * as types from '../types';
import dayjs from 'dayjs';

const { Title, Text } = Typography;
const { Option } = Select;
const { TextArea } = Input;

const AVAILABLE_PLUGINS = [
  { name: 'key-auth', label: 'Key Auth (API Key认证)', description: '基于API Key的认证方式' },
  { name: 'jwt-auth', label: 'JWT Auth (JWT认证)', description: '基于JWT Token的认证方式' },
  { name: 'rate-limiting', label: 'Rate Limiting (限流)', description: '请求速率限制' },
  { name: 'request-transformer', label: 'Request Transformer (请求转换)', description: '请求转换插件' },
  { name: 'response-transformer', label: 'Response Transformer (响应转换)', description: '响应转换插件' },
  { name: 'cors', label: 'CORS (跨域)', description: '跨域资源共享配置' },
  { name: 'ip-restriction', label: 'IP Restriction (IP限制)', description: 'IP白名单/黑名单' },
  { name: 'request-size-limiting', label: 'Request Size Limiting (请求大小限制)', description: '限制请求体大小' },
  { name: 'bot-detection', label: 'Bot Detection (机器人检测)', description: '检测和阻止机器人请求' },
];

const PluginsPage: React.FC = () => {
  const [plugins, setPlugins] = useState<types.Plugin[]>([]);
  const [routes, setRoutes] = useState<types.Route[]>([]);
  const [services, setServices] = useState<types.Service[]>([]);
  const [consumers, setConsumers] = useState<types.Consumer[]>([]);
  const [loading, setLoading] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [modalVisible, setModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentPlugin, setCurrentPlugin] = useState<types.Plugin | null>(null);
  const [form] = Form.useForm();
  const [isEdit, setIsEdit] = useState(false);
  const [scope, setScope] = useState<'global' | 'route' | 'service' | 'consumer'>('global');

  const fetchPlugins = async (page = 1, pageSize = 10) => {
    setLoading(true);
    try {
      const response = await apiService.getPlugins(page, pageSize);
      setPlugins(response.items || []);
      setPagination({
        current: response.page,
        pageSize: response.page_size,
        total: response.total,
      });
    } catch (error) {
      console.error('Failed to fetch plugins:', error);
      message.error('获取插件列表失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchRelatedData = async () => {
    try {
      const [routesRes, servicesRes, consumersRes] = await Promise.all([
        apiService.getRoutes(1, 1000),
        apiService.getServices(1, 1000),
        apiService.getConsumers(1, 1000),
      ]);
      setRoutes(routesRes.items || []);
      setServices(servicesRes.items || []);
      setConsumers(consumersRes.items || []);
    } catch (error) {
      console.error('Failed to fetch related data:', error);
    }
  };

  useEffect(() => {
    fetchPlugins();
    fetchRelatedData();
  }, []);

  const getScopeLabel = (plugin: types.Plugin) => {
    if (plugin.consumer_id) return 'Consumer';
    if (plugin.route_id) return 'Route';
    if (plugin.service_id) return 'Service';
    return 'Global';
  };

  const getScopeColor = (plugin: types.Plugin) => {
    if (plugin.consumer_id) return 'purple';
    if (plugin.route_id) return 'blue';
    if (plugin.service_id) return 'orange';
    return 'green';
  };

  const handleAdd = () => {
    setIsEdit(false);
    setCurrentPlugin(null);
    setScope('global');
    form.resetFields();
    form.setFieldsValue({
      protocols: ['http', 'https'],
      enabled: true,
      run_on: 'first',
      config: '{}',
    });
    setModalVisible(true);
  };

  const handleEdit = (record: types.Plugin) => {
    setIsEdit(true);
    setCurrentPlugin(record);
    let currentScope: 'global' | 'route' | 'service' | 'consumer' = 'global';
    if (record.consumer_id) currentScope = 'consumer';
    else if (record.route_id) currentScope = 'route';
    else if (record.service_id) currentScope = 'service';
    setScope(currentScope);
    form.setFieldsValue({
      name: record.name,
      route_id: record.route_id,
      service_id: record.service_id,
      consumer_id: record.consumer_id,
      protocols: record.protocols,
      enabled: record.enabled,
      run_on: record.run_on,
      tags: record.tags,
      config: JSON.stringify(record.config, null, 2),
    });
    setModalVisible(true);
  };

  const handleDetail = (record: types.Plugin) => {
    setCurrentPlugin(record);
    setDetailVisible(true);
  };

  const handleDelete = async (id: string) => {
    try {
      await apiService.deletePlugin(id);
      message.success('删除成功');
      fetchPlugins(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to delete plugin:', error);
      message.error('删除失败');
    }
  };

  const handleSubmit = async (values: any) => {
    try {
      let config: Record<string, unknown> = {};
      if (values.config) {
        try {
          config = JSON.parse(values.config);
        } catch (e) {
          message.error('配置必须是有效的JSON格式');
          return;
        }
      }

      const request: types.CreatePluginRequest = {
        name: values.name,
        config,
        protocols: values.protocols,
        enabled: values.enabled,
        run_on: values.run_on,
        tags: values.tags,
      };

      if (scope === 'route' && values.route_id) {
        request.route_id = values.route_id;
      }
      if (scope === 'service' && values.service_id) {
        request.service_id = values.service_id;
      }
      if (scope === 'consumer' && values.consumer_id) {
        request.consumer_id = values.consumer_id;
      }

      if (isEdit && currentPlugin) {
        await apiService.updatePlugin(currentPlugin.id, request);
        message.success('更新成功');
      } else {
        await apiService.createPlugin(request);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchPlugins(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to save plugin:', error);
      message.error(isEdit ? '更新失败' : '创建失败');
    }
  };

  const columns = [
    {
      title: '插件名称',
      dataIndex: 'name',
      key: 'name',
      render: (text: string, record: types.Plugin) => (
        <a onClick={() => handleDetail(record)}>{text}</a>
      ),
    },
    {
      title: '作用域',
      key: 'scope',
      render: (_: unknown, record: types.Plugin) => (
        <Tag color={getScopeColor(record)}>{getScopeLabel(record)}</Tag>
      ),
    },
    {
      title: '协议',
      dataIndex: 'protocols',
      key: 'protocols',
      render: (protocols: string[]) => (
        <Space wrap>
          {protocols?.map((p, i) => <Tag key={i}>{p}</Tag>)}
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
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (time: string) => dayjs(time).format('YYYY-MM-DD HH:mm:ss'),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, record: types.Plugin) => (
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
            title="确定要删除这个插件吗？"
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
        title="插件管理"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
            新建插件
          </Button>
        }
      >
        <Table
          columns={columns}
          dataSource={plugins}
          rowKey="id"
          loading={loading}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => fetchPlugins(page, pageSize),
          }}
        />
      </Card>

      <Modal
        title={isEdit ? '编辑插件' : '新建插件'}
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
          <Form.Item
            name="name"
            label="插件名称"
            rules={[{ required: true, message: '请选择插件' }]}
          >
            <Select placeholder="请选择插件">
              {AVAILABLE_PLUGINS.map(plugin => (
                <Option key={plugin.name} value={plugin.name}>
                  {plugin.label}
                </Option>
              ))}
            </Select>
          </Form.Item>

          <Form.Item label="作用域">
            <Radio.Group
              value={scope}
              onChange={(e) => setScope(e.target.value)}
              buttonStyle="solid"
            >
              <Radio.Button value="global">全局</Radio.Button>
              <Radio.Button value="route">路由</Radio.Button>
              <Radio.Button value="service">服务</Radio.Button>
              <Radio.Button value="consumer">消费者</Radio.Button>
            </Radio.Group>
          </Form.Item>

          {scope === 'route' && (
            <Form.Item
              name="route_id"
              label="选择路由"
              rules={[{ required: true, message: '请选择路由' }]}
            >
              <Select placeholder="请选择路由">
                {routes.map(route => (
                  <Option key={route.id} value={route.id}>
                    {route.name || route.id}
                  </Option>
                ))}
              </Select>
            </Form.Item>
          )}

          {scope === 'service' && (
            <Form.Item
              name="service_id"
              label="选择服务"
              rules={[{ required: true, message: '请选择服务' }]}
            >
              <Select placeholder="请选择服务">
                {services.map(service => (
                  <Option key={service.id} value={service.id}>
                    {service.name}
                  </Option>
                ))}
              </Select>
            </Form.Item>
          )}

          {scope === 'consumer' && (
            <Form.Item
              name="consumer_id"
              label="选择消费者"
              rules={[{ required: true, message: '请选择消费者' }]}
            >
              <Select placeholder="请选择消费者">
                {consumers.map(consumer => (
                  <Option key={consumer.id} value={consumer.id}>
                    {consumer.username || consumer.custom_id || consumer.id}
                  </Option>
                ))}
              </Select>
            </Form.Item>
          )}

          <Form.Item name="protocols" label="协议">
            <Select mode="multiple" placeholder="选择协议">
              <Option value="http">HTTP</Option>
              <Option value="https">HTTPS</Option>
            </Select>
          </Form.Item>

          <Form.Item name="run_on" label="执行时机">
            <Select>
              <Option value="first">First</Option>
              <Option value="second">Second</Option>
              <Option value="last">Last</Option>
              <Option value="all">All</Option>
            </Select>
          </Form.Item>

          <Form.Item
            name="config"
            label="配置 (JSON)"
            rules={[{ required: true, message: '请输入配置' }]}
          >
            <TextArea
              rows={8}
              placeholder="请输入插件配置 (JSON格式)，例如: {'key_names': ['apikey'], 'hide_credentials': true}"
              spellCheck={false}
            />
          </Form.Item>

          <Form.Item name="tags" label="标签">
            <Select mode="tags" placeholder="输入标签后回车添加" style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="enabled" label="启用状态" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="禁用" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="插件详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={[
          <Button key="close" onClick={() => setDetailVisible(false)}>
            关闭
          </Button>,
        ]}
        width={600}
      >
        {currentPlugin && (
          <div>
            <Title level={5}>基本信息</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>ID: </Text>
                <Text code>{currentPlugin.id}</Text>
              </div>
              <div>
                <Text strong>名称: </Text>
                {currentPlugin.name}
              </div>
              <div>
                <Text strong>作用域: </Text>
                <Tag color={getScopeColor(currentPlugin)}>
                  {getScopeLabel(currentPlugin)}
                </Tag>
              </div>
              <div>
                <Text strong>协议: </Text>
                {currentPlugin.protocols?.map((p, i) => <Tag key={i}>{p}</Tag>)}
              </div>
              <div>
                <Text strong>执行时机: </Text>
                {currentPlugin.run_on}
              </div>
              <div>
                <Text strong>状态: </Text>
                <Tag color={currentPlugin.enabled ? 'green' : 'default'}>
                  {currentPlugin.enabled ? '启用' : '禁用'}
                </Tag>
              </div>
            </Space>

            <Divider />

            <Title level={5}>配置</Title>
            <div style={{
              background: '#f5f5f5',
              padding: 16,
              borderRadius: 4,
              fontFamily: 'monospace',
              overflowX: 'auto',
            }}>
              <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>
                {JSON.stringify(currentPlugin.config, null, 2)}
              </pre>
            </div>

            <Divider />

            <Title level={5}>其他信息</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>标签: </Text>
                {currentPlugin.tags && currentPlugin.tags.length > 0
                  ? currentPlugin.tags.map((tag) => <Tag key={tag}>{tag}</Tag>)
                  : '-'}
              </div>
              <div>
                <Text strong>创建时间: </Text>
                {dayjs(currentPlugin.created_at).format('YYYY-MM-DD HH:mm:ss')}
              </div>
            </Space>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default PluginsPage;
