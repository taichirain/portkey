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
} from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons';
import { apiService } from '../services/api';
import * as types from '../types';
import dayjs from 'dayjs';

const { Text } = Typography;
const { Option } = Select;

const TargetsPage: React.FC = () => {
  const [targets, setTargets] = useState<types.Target[]>([]);
  const [upstreams, setUpstreams] = useState<types.Upstream[]>([]);
  const [loading, setLoading] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [modalVisible, setModalVisible] = useState(false);
  const [currentTarget, setCurrentTarget] = useState<types.Target | null>(null);
  const [form] = Form.useForm();
  const [isEdit, setIsEdit] = useState(false);

  const fetchTargets = async (page = 1, pageSize = 10) => {
    setLoading(true);
    try {
      const response = await apiService.getTargets(page, pageSize);
      setTargets(response.items || []);
      setPagination({
        current: response.page,
        pageSize: response.page_size,
        total: response.total,
      });
    } catch (error) {
      console.error('Failed to fetch targets:', error);
      message.error('获取目标列表失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchUpstreams = async () => {
    try {
      const response = await apiService.getUpstreams(1, 1000);
      setUpstreams(response.items || []);
    } catch (error) {
      console.error('Failed to fetch upstreams:', error);
    }
  };

  useEffect(() => {
    fetchTargets();
    fetchUpstreams();
  }, []);

  const handleAdd = () => {
    setIsEdit(false);
    setCurrentTarget(null);
    form.resetFields();
    form.setFieldsValue({
      port: 80,
      weight: 100,
      enabled: true,
    });
    setModalVisible(true);
  };

  const handleEdit = (record: types.Target) => {
    setIsEdit(true);
    setCurrentTarget(record);
    form.setFieldsValue({
      ...record,
    });
    setModalVisible(true);
  };

  const handleDelete = async (id: string) => {
    try {
      await apiService.deleteTarget(id);
      message.success('删除成功');
      fetchTargets(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to delete target:', error);
      message.error('删除失败');
    }
  };

  const handleSubmit = async (values: types.CreateTargetRequest) => {
    try {
      if (isEdit && currentTarget) {
        await apiService.updateTarget(currentTarget.id, values);
        message.success('更新成功');
      } else {
        await apiService.createTarget(values);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchTargets(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to save target:', error);
      message.error(isEdit ? '更新失败' : '创建失败');
    }
  };

  const getUpstreamName = (upstreamId: string) => {
    const upstream = upstreams.find(u => u.id === upstreamId);
    return upstream ? upstream.name : upstreamId;
  };

  const columns = [
    {
      title: '目标地址',
      key: 'target',
      render: (_: unknown, record: types.Target) => (
        <Text code>{record.target}:{record.port}</Text>
      ),
    },
    {
      title: '上游',
      dataIndex: 'upstream_id',
      key: 'upstream_id',
      render: (upstreamId: string) => getUpstreamName(upstreamId),
    },
    {
      title: '权重',
      dataIndex: 'weight',
      key: 'weight',
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
      render: (_: unknown, record: types.Target) => (
        <Space size="small">
          <Button
            type="text"
            icon={<EditOutlined />}
            onClick={() => handleEdit(record)}
          />
          <Popconfirm
            title="确定要删除这个目标吗？"
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
        title="目标管理"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
            新建目标
          </Button>
        }
      >
        <Table
          columns={columns}
          dataSource={targets}
          rowKey="id"
          loading={loading}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => fetchTargets(page, pageSize),
          }}
        />
      </Card>

      <Modal
        title={isEdit ? '编辑目标' : '新建目标'}
        open={modalVisible}
        onCancel={() => setModalVisible(false)}
        onOk={() => form.submit()}
        width={500}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
        >
          {!isEdit && (
            <Form.Item
              name="upstream_id"
              label="所属上游"
              rules={[{ required: true, message: '请选择上游' }]}
            >
              <Select placeholder="请选择上游">
                {upstreams.map(upstream => (
                  <Option key={upstream.id} value={upstream.id}>
                    {upstream.name}
                  </Option>
                ))}
              </Select>
            </Form.Item>
          )}

          <Form.Item
            name="target"
            label="目标地址"
            rules={[{ required: true, message: '请输入目标地址' }]}
          >
            <Input placeholder="例如: 192.168.1.100 或 backend.example.com" />
          </Form.Item>

          <Form.Item
            name="port"
            label="端口"
            rules={[{ required: true, message: '请输入端口' }]}
          >
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="weight" label="权重">
            <InputNumber min={1} max={1000} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="tags" label="标签">
            <Select mode="tags" placeholder="输入标签后回车添加" style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="enabled" label="启用状态" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="禁用" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default TargetsPage;
