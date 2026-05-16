import React, { useState, useEffect } from 'react';
import {
  Card,
  Table,
  Button,
  Modal,
  Form,
  Input,
  Select,
  Tag,
  Space,
  message,
  Popconfirm,
  Typography,
  Divider,
  List,
} from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, EyeOutlined } from '@ant-design/icons';
import { apiService } from '../services/api';
import * as types from '../types';
import dayjs from 'dayjs';

const { Title, Text } = Typography;

const ConsumersPage: React.FC = () => {
  const [consumers, setConsumers] = useState<types.Consumer[]>([]);
  const [plugins, setPlugins] = useState<types.Plugin[]>([]);
  const [loading, setLoading] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [modalVisible, setModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentConsumer, setCurrentConsumer] = useState<types.Consumer | null>(null);
  const [form] = Form.useForm();
  const [isEdit, setIsEdit] = useState(false);

  const fetchConsumers = async (page = 1, pageSize = 10) => {
    setLoading(true);
    try {
      const response = await apiService.getConsumers(page, pageSize);
      setConsumers(response.items || []);
      setPagination({
        current: response.page,
        pageSize: response.page_size,
        total: response.total,
      });
    } catch (error) {
      console.error('Failed to fetch consumers:', error);
      message.error('获取消费者列表失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchConsumers();
  }, []);

  const handleAdd = () => {
    setIsEdit(false);
    setCurrentConsumer(null);
    form.resetFields();
    setModalVisible(true);
  };

  const handleEdit = (record: types.Consumer) => {
    setIsEdit(true);
    setCurrentConsumer(record);
    form.setFieldsValue({
      username: record.username || undefined,
      custom_id: record.custom_id || undefined,
      tags: record.tags,
    });
    setModalVisible(true);
  };

  const handleDetail = async (record: types.Consumer) => {
    setCurrentConsumer(record);
    try {
      const pluginsData = await apiService.getPluginsByConsumerId(record.id);
      setPlugins(pluginsData);
    } catch (error) {
      console.error('Failed to fetch plugins:', error);
      setPlugins([]);
    }
    setDetailVisible(true);
  };

  const handleDelete = async (id: string) => {
    try {
      await apiService.deleteConsumer(id);
      message.success('删除成功');
      fetchConsumers(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to delete consumer:', error);
      message.error('删除失败');
    }
  };

  const handleSubmit = async (values: types.CreateConsumerRequest) => {
    try {
      if (isEdit && currentConsumer) {
        await apiService.updateConsumer(currentConsumer.id, values);
        message.success('更新成功');
      } else {
        await apiService.createConsumer(values);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchConsumers(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to save consumer:', error);
      message.error(isEdit ? '更新失败' : '创建失败');
    }
  };

  const columns = [
    {
      title: '用户名',
      dataIndex: 'username',
      key: 'username',
      render: (text: string, record: types.Consumer) => (
        <a onClick={() => handleDetail(record)}>{text || '-'}</a>
      ),
    },
    {
      title: 'Custom ID',
      dataIndex: 'custom_id',
      key: 'custom_id',
      render: (text: string) => text || '-',
    },
    {
      title: '标签',
      dataIndex: 'tags',
      key: 'tags',
      render: (tags: string[]) => (
        <Space wrap>
          {tags?.map((tag, index) => (
            <Tag key={index}>{tag}</Tag>
          )) || '-'}
        </Space>
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
      render: (_: unknown, record: types.Consumer) => (
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
            title="确定要删除这个消费者吗？"
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
        title="消费者管理"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
            新建消费者
          </Button>
        }
      >
        <Table
          columns={columns}
          dataSource={consumers}
          rowKey="id"
          loading={loading}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => fetchConsumers(page, pageSize),
          }}
        />
      </Card>

      <Modal
        title={isEdit ? '编辑消费者' : '新建消费者'}
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
          <Form.Item
            name="username"
            label="用户名"
          >
            <Input placeholder="请输入用户名 (可选)" />
          </Form.Item>

          <Form.Item
            name="custom_id"
            label="Custom ID"
          >
            <Input placeholder="请输入 Custom ID (可选)" />
          </Form.Item>

          <Form.Item name="tags" label="标签">
            <Select mode="tags" placeholder="输入标签后回车添加" style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="消费者详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={[
          <Button key="close" onClick={() => setDetailVisible(false)}>
            关闭
          </Button>,
        ]}
        width={600}
      >
        {currentConsumer && (
          <div>
            <Title level={5}>基本信息</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>ID: </Text>
                <Text code>{currentConsumer.id}</Text>
              </div>
              <div>
                <Text strong>用户名: </Text>
                {currentConsumer.username || '-'}
              </div>
              <div>
                <Text strong>Custom ID: </Text>
                {currentConsumer.custom_id || '-'}
              </div>
              <div>
                <Text strong>标签: </Text>
                {currentConsumer.tags && currentConsumer.tags.length > 0
                  ? currentConsumer.tags.map((tag) => <Tag key={tag}>{tag}</Tag>)
                  : '-'}
              </div>
            </Space>

            <Divider />

            <Title level={5}>关联插件 ({plugins.length})</Title>
            {plugins.length > 0 ? (
              <List
                dataSource={plugins}
                renderItem={(item) => (
                  <List.Item>
                    <List.Item.Meta
                      title={
                        <Space>
                          {item.name}
                          <Tag color={item.enabled ? 'green' : 'default'}>
                            {item.enabled ? '启用' : '禁用'}
                          </Tag>
                        </Space>
                      }
                    />
                  </List.Item>
                )}
              />
            ) : (
              <Text type="secondary">暂无关联插件</Text>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
};

export default ConsumersPage;
