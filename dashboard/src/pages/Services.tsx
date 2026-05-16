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

const ServicesPage: React.FC = () => {
  const [services, setServices] = useState<types.Service[]>([]);
  const [loading, setLoading] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [modalVisible, setModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentService, setCurrentService] = useState<types.Service | null>(null);
  const [form] = Form.useForm();
  const [isEdit, setIsEdit] = useState(false);

  const fetchServices = async (page = 1, pageSize = 10) => {
    setLoading(true);
    try {
      const response = await apiService.getServices(page, pageSize);
      setServices(response.items || []);
      setPagination({
        current: response.page,
        pageSize: response.page_size,
        total: response.total,
      });
    } catch (error) {
      console.error('Failed to fetch services:', error);
      message.error('获取服务列表失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchServices();
  }, []);

  const handleAdd = () => {
    setIsEdit(false);
    setCurrentService(null);
    form.resetFields();
    form.setFieldsValue({
      protocol: 'http',
      port: 80,
      retries: 5,
      connect_timeout: 60000,
      write_timeout: 60000,
      read_timeout: 60000,
      enabled: true,
    });
    setModalVisible(true);
  };

  const handleEdit = (record: types.Service) => {
    setIsEdit(true);
    setCurrentService(record);
    form.setFieldsValue({
      ...record,
    });
    setModalVisible(true);
  };

  const handleDetail = (record: types.Service) => {
    setCurrentService(record);
    setDetailVisible(true);
  };

  const handleDelete = async (id: string) => {
    try {
      await apiService.deleteService(id);
      message.success('删除成功');
      fetchServices(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to delete service:', error);
      message.error('删除失败');
    }
  };

  const handleSubmit = async (values: types.CreateServiceRequest) => {
    try {
      if (isEdit && currentService) {
        await apiService.updateService(currentService.id, values);
        message.success('更新成功');
      } else {
        await apiService.createService(values);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchServices(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to save service:', error);
      message.error(isEdit ? '更新失败' : '创建失败');
    }
  };

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (text: string, record: types.Service) => (
        <a onClick={() => handleDetail(record)}>{text}</a>
      ),
    },
    {
      title: '协议',
      dataIndex: 'protocol',
      key: 'protocol',
      render: (protocol: string) => (
        <Tag color={protocol === 'https' ? 'red' : 'blue'}>{protocol}</Tag>
      ),
    },
    {
      title: '主机',
      dataIndex: 'host',
      key: 'host',
      render: (host: string, record: types.Service) => (
        <span>{host || '-'}{record.port ? `:${record.port}` : ''}</span>
      ),
    },
    {
      title: '路径',
      dataIndex: 'path',
      key: 'path',
      render: (path: string) => path || '-',
    },
    {
      title: '重试次数',
      dataIndex: 'retries',
      key: 'retries',
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
      render: (_: unknown, record: types.Service) => (
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
            title="确定要删除这个服务吗？"
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
        title="服务管理"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
            新建服务
          </Button>
        }
      >
        <Table
          columns={columns}
          dataSource={services}
          rowKey="id"
          loading={loading}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => fetchServices(page, pageSize),
          }}
        />
      </Card>

      <Modal
        title={isEdit ? '编辑服务' : '新建服务'}
        open={modalVisible}
        onCancel={() => setModalVisible(false)}
        onOk={() => form.submit()}
        width={600}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
        >
          <Form.Item
            name="name"
            label="服务名称"
            rules={[{ required: true, message: '请输入服务名称' }]}
          >
            <Input placeholder="请输入服务名称" />
          </Form.Item>

          <Form.Item name="protocol" label="协议">
            <Select>
              <Option value="http">HTTP</Option>
              <Option value="https">HTTPS</Option>
            </Select>
          </Form.Item>

          <Form.Item name="host" label="目标主机">
            <Input placeholder="例如: example.com" />
          </Form.Item>

          <Form.Item name="port" label="端口">
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="path" label="路径前缀">
            <Input placeholder="例如: /api" />
          </Form.Item>

          <Form.Item name="retries" label="重试次数">
            <InputNumber min={0} max={100} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="connect_timeout" label="连接超时 (毫秒)">
            <InputNumber min={1000} max={300000} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="write_timeout" label="写入超时 (毫秒)">
            <InputNumber min={1000} max={300000} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="read_timeout" label="读取超时 (毫秒)">
            <InputNumber min={1000} max={300000} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="tags" label="标签">
            <Select
              mode="tags"
              placeholder="输入标签后回车添加"
              style={{ width: '100%' }}
            />
          </Form.Item>

          <Form.Item name="enabled" label="启用状态" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="禁用" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="服务详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={[
          <Button key="close" onClick={() => setDetailVisible(false)}>
            关闭
          </Button>,
        ]}
        width={600}
      >
        {currentService && (
          <div>
            <Title level={5}>基本信息</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>ID: </Text>
                <Text code>{currentService.id}</Text>
              </div>
              <div>
                <Text strong>名称: </Text>
                {currentService.name}
              </div>
              <div>
                <Text strong>协议: </Text>
                <Tag color={currentService.protocol === 'https' ? 'red' : 'blue'}>
                  {currentService.protocol}
                </Tag>
              </div>
              <div>
                <Text strong>主机: </Text>
                {currentService.host || '-'}{currentService.port ? `:${currentService.port}` : ''}
              </div>
              <div>
                <Text strong>路径: </Text>
                {currentService.path || '-'}
              </div>
            </Space>

            <Divider />

            <Title level={5}>配置</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>重试次数: </Text>
                {currentService.retries}
              </div>
              <div>
                <Text strong>连接超时: </Text>
                {currentService.connect_timeout}ms
              </div>
              <div>
                <Text strong>写入超时: </Text>
                {currentService.write_timeout}ms
              </div>
              <div>
                <Text strong>读取超时: </Text>
                {currentService.read_timeout}ms
              </div>
              <div>
                <Text strong>状态: </Text>
                <Tag color={currentService.enabled ? 'green' : 'default'}>
                  {currentService.enabled ? '启用' : '禁用'}
                </Tag>
              </div>
            </Space>

            <Divider />

            <Title level={5}>其他信息</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>标签: </Text>
                {currentService.tags && currentService.tags.length > 0
                  ? currentService.tags.map((tag) => <Tag key={tag}>{tag}</Tag>)
                  : '-'}
              </div>
              <div>
                <Text strong>创建时间: </Text>
                {dayjs(currentService.created_at).format('YYYY-MM-DD HH:mm:ss')}
              </div>
              <div>
                <Text strong>更新时间: </Text>
                {dayjs(currentService.updated_at).format('YYYY-MM-DD HH:mm:ss')}
              </div>
            </Space>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default ServicesPage;
