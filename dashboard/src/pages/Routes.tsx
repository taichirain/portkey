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
} from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, EyeOutlined } from '@ant-design/icons';
import { apiService } from '../services/api';
import * as types from '../types';
import dayjs from 'dayjs';

const { Title, Text } = Typography;
const { Option } = Select;

const RoutesPage: React.FC = () => {
  const [routes, setRoutes] = useState<types.Route[]>([]);
  const [services, setServices] = useState<types.Service[]>([]);
  const [loading, setLoading] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [modalVisible, setModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentRoute, setCurrentRoute] = useState<types.Route | null>(null);
  const [form] = Form.useForm();
  const [isEdit, setIsEdit] = useState(false);

  const fetchRoutes = async (page = 1, pageSize = 10) => {
    setLoading(true);
    try {
      const response = await apiService.getRoutes(page, pageSize);
      setRoutes(response.items || []);
      setPagination({
        current: response.page,
        pageSize: response.page_size,
        total: response.total,
      });
    } catch (error) {
      console.error('Failed to fetch routes:', error);
      message.error('获取路由列表失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchServices = async () => {
    try {
      const response = await apiService.getServices(1, 1000);
      setServices(response.items || []);
    } catch (error) {
      console.error('Failed to fetch services:', error);
    }
  };

  useEffect(() => {
    fetchRoutes();
    fetchServices();
  }, []);

  const handleAdd = () => {
    setIsEdit(false);
    setCurrentRoute(null);
    form.resetFields();
    form.setFieldsValue({
      protocols: ['http'],
      methods: ['GET', 'POST'],
      strip_path: true,
      preserve_host: false,
      regex_priority: 0,
      enabled: true,
    });
    setModalVisible(true);
  };

  const handleEdit = (record: types.Route) => {
    setIsEdit(true);
    setCurrentRoute(record);
    form.setFieldsValue({
      ...record,
    });
    setModalVisible(true);
  };

  const handleDetail = (record: types.Route) => {
    setCurrentRoute(record);
    setDetailVisible(true);
  };

  const handleDelete = async (id: string) => {
    try {
      await apiService.deleteRoute(id);
      message.success('删除成功');
      fetchRoutes(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to delete route:', error);
      message.error('删除失败');
    }
  };

  const handleSubmit = async (values: types.CreateRouteRequest) => {
    try {
      if (isEdit && currentRoute) {
        await apiService.updateRoute(currentRoute.id, values);
        message.success('更新成功');
      } else {
        await apiService.createRoute(values);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchRoutes(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to save route:', error);
      message.error(isEdit ? '更新失败' : '创建失败');
    }
  };

  const getServiceName = (serviceId: string) => {
    const service = services.find(s => s.id === serviceId);
    return service ? service.name : serviceId;
  };

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (text: string, record: types.Route) => (
        <a onClick={() => handleDetail(record)}>{text || '-'}</a>
      ),
    },
    {
      title: '服务',
      dataIndex: 'service_id',
      key: 'service_id',
      render: (serviceId: string) => getServiceName(serviceId),
    },
    {
      title: '路径',
      dataIndex: 'paths',
      key: 'paths',
      render: (paths: string[]) => (
        <Space wrap>
          {paths?.map((path, index) => (
            <Tag key={index} color="blue">{path}</Tag>
          )) || '-'}
        </Space>
      ),
    },
    {
      title: '方法',
      dataIndex: 'methods',
      key: 'methods',
      render: (methods: string[]) => (
        <Space wrap>
          {methods?.map((method, index) => (
            <Tag key={index} color="green">{method}</Tag>
          )) || '-'}
        </Space>
      ),
    },
    {
      title: '主机',
      dataIndex: 'hosts',
      key: 'hosts',
      render: (hosts: string[]) => (
        <Space wrap>
          {hosts?.map((host, index) => (
            <Tag key={index} color="orange">{host}</Tag>
          )) || '-'}
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
      render: (_: unknown, record: types.Route) => (
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
            title="确定要删除这个路由吗？"
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
        title="路由管理"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
            新建路由
          </Button>
        }
      >
        <Table
          columns={columns}
          dataSource={routes}
          rowKey="id"
          loading={loading}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => fetchRoutes(page, pageSize),
          }}
        />
      </Card>

      <Modal
        title={isEdit ? '编辑路由' : '新建路由'}
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
            label="路由名称"
          >
            <Input placeholder="请输入路由名称" />
          </Form.Item>

          <Form.Item
            name="service_id"
            label="关联服务"
            rules={[{ required: true, message: '请选择关联服务' }]}
          >
            <Select placeholder="请选择服务">
              {services.map(service => (
                <Option key={service.id} value={service.id}>
                  {service.name}
                </Option>
              ))}
            </Select>
          </Form.Item>

          <Form.Item name="protocols" label="协议">
            <Select mode="multiple" placeholder="选择协议">
              <Option value="http">HTTP</Option>
              <Option value="https">HTTPS</Option>
            </Select>
          </Form.Item>

          <Form.Item name="methods" label="HTTP方法">
            <Select mode="multiple" placeholder="选择HTTP方法">
              <Option value="GET">GET</Option>
              <Option value="POST">POST</Option>
              <Option value="PUT">PUT</Option>
              <Option value="DELETE">DELETE</Option>
              <Option value="PATCH">PATCH</Option>
              <Option value="OPTIONS">OPTIONS</Option>
              <Option value="HEAD">HEAD</Option>
            </Select>
          </Form.Item>

          <Form.Item name="hosts" label="主机 (Hosts)">
            <Select mode="tags" placeholder="输入主机名后回车添加，例如: api.example.com" />
          </Form.Item>

          <Form.Item name="paths" label="路径">
            <Select mode="tags" placeholder="输入路径后回车添加，例如: /api/*" />
          </Form.Item>

          <Form.Item name="strip_path" label="移除路径前缀" valuePropName="checked">
            <Switch checkedChildren="是" unCheckedChildren="否" />
          </Form.Item>

          <Form.Item name="preserve_host" label="保留主机头" valuePropName="checked">
            <Switch checkedChildren="是" unCheckedChildren="否" />
          </Form.Item>

          <Form.Item name="regex_priority" label="正则优先级">
            <InputNumber min={0} max={1000} style={{ width: '100%' }} />
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
        title="路由详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={[
          <Button key="close" onClick={() => setDetailVisible(false)}>
            关闭
          </Button>,
        ]}
        width={600}
      >
        {currentRoute && (
          <div>
            <Title level={5}>基本信息</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>ID: </Text>
                <Text code>{currentRoute.id}</Text>
              </div>
              <div>
                <Text strong>名称: </Text>
                {currentRoute.name || '-'}
              </div>
              <div>
                <Text strong>关联服务: </Text>
                {getServiceName(currentRoute.service_id)}
              </div>
            </Space>

            <Divider />

            <Title level={5}>匹配条件</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>协议: </Text>
                {currentRoute.protocols?.map((p, i) => <Tag key={i}>{p}</Tag>)}
              </div>
              <div>
                <Text strong>方法: </Text>
                {currentRoute.methods?.map((m, i) => <Tag key={i} color="green">{m}</Tag>)}
              </div>
              <div>
                <Text strong>主机: </Text>
                {currentRoute.hosts?.length > 0
                  ? currentRoute.hosts.map((h, i) => <Tag key={i} color="orange">{h}</Tag>)
                  : '-'}
              </div>
              <div>
                <Text strong>路径: </Text>
                {currentRoute.paths?.length > 0
                  ? currentRoute.paths.map((p, i) => <Tag key={i} color="blue">{p}</Tag>)
                  : '-'}
              </div>
            </Space>

            <Divider />

            <Title level={5}>其他信息</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>移除路径前缀: </Text>
                {currentRoute.strip_path ? '是' : '否'}
              </div>
              <div>
                <Text strong>保留主机头: </Text>
                {currentRoute.preserve_host ? '是' : '否'}
              </div>
              <div>
                <Text strong>正则优先级: </Text>
                {currentRoute.regex_priority}
              </div>
              <div>
                <Text strong>状态: </Text>
                <Tag color={currentRoute.enabled ? 'green' : 'default'}>
                  {currentRoute.enabled ? '启用' : '禁用'}
                </Tag>
              </div>
              <div>
                <Text strong>标签: </Text>
                {currentRoute.tags && currentRoute.tags.length > 0
                  ? currentRoute.tags.map((tag) => <Tag key={tag}>{tag}</Tag>)
                  : '-'}
              </div>
              <div>
                <Text strong>创建时间: </Text>
                {dayjs(currentRoute.created_at).format('YYYY-MM-DD HH:mm:ss')}
              </div>
            </Space>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default RoutesPage;
