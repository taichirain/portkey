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
const { Option } = Select;

const UpstreamsPage: React.FC = () => {
  const [upstreams, setUpstreams] = useState<types.Upstream[]>([]);
  const [loading, setLoading] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [modalVisible, setModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentUpstream, setCurrentUpstream] = useState<types.Upstream | null>(null);
  const [targets, setTargets] = useState<types.Target[]>([]);
  const [form] = Form.useForm();
  const [isEdit, setIsEdit] = useState(false);

  const fetchUpstreams = async (page = 1, pageSize = 10) => {
    setLoading(true);
    try {
      const response = await apiService.getUpstreams(page, pageSize);
      setUpstreams(response.items || []);
      setPagination({
        current: response.page,
        pageSize: response.page_size,
        total: response.total,
      });
    } catch (error) {
      console.error('Failed to fetch upstreams:', error);
      message.error('获取上游列表失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchUpstreams();
  }, []);

  const handleAdd = () => {
    setIsEdit(false);
    setCurrentUpstream(null);
    form.resetFields();
    form.setFieldsValue({
      algorithm: 'round-robin',
      slots: 10000,
    });
    setModalVisible(true);
  };

  const handleEdit = (record: types.Upstream) => {
    setIsEdit(true);
    setCurrentUpstream(record);
    form.setFieldsValue({
      ...record,
    });
    setModalVisible(true);
  };

  const handleDetail = async (record: types.Upstream) => {
    setCurrentUpstream(record);
    try {
      const targetsData = await apiService.getTargetsByUpstreamId(record.id);
      setTargets(targetsData);
    } catch (error) {
      console.error('Failed to fetch targets:', error);
      setTargets([]);
    }
    setDetailVisible(true);
  };

  const handleDelete = async (id: string) => {
    try {
      await apiService.deleteUpstream(id);
      message.success('删除成功');
      fetchUpstreams(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to delete upstream:', error);
      message.error('删除失败');
    }
  };

  const handleSubmit = async (values: types.CreateUpstreamRequest) => {
    try {
      if (isEdit && currentUpstream) {
        await apiService.updateUpstream(currentUpstream.id, values);
        message.success('更新成功');
      } else {
        await apiService.createUpstream(values);
        message.success('创建成功');
      }
      setModalVisible(false);
      fetchUpstreams(pagination.current, pagination.pageSize);
    } catch (error) {
      console.error('Failed to save upstream:', error);
      message.error(isEdit ? '更新失败' : '创建失败');
    }
  };

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (text: string, record: types.Upstream) => (
        <a onClick={() => handleDetail(record)}>{text}</a>
      ),
    },
    {
      title: '负载均衡算法',
      dataIndex: 'algorithm',
      key: 'algorithm',
      render: (algo: string) => (
        <Tag color="blue">{algo}</Tag>
      ),
    },
    {
      title: '插槽数',
      dataIndex: 'slots',
      key: 'slots',
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
      render: (_: unknown, record: types.Upstream) => (
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
            title="确定要删除这个上游吗？"
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
        title="上游管理"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={handleAdd}>
            新建上游
          </Button>
        }
      >
        <Table
          columns={columns}
          dataSource={upstreams}
          rowKey="id"
          loading={loading}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => fetchUpstreams(page, pageSize),
          }}
        />
      </Card>

      <Modal
        title={isEdit ? '编辑上游' : '新建上游'}
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
            name="name"
            label="上游名称"
            rules={[{ required: true, message: '请输入上游名称' }]}
          >
            <Input placeholder="请输入上游名称" />
          </Form.Item>

          <Form.Item name="algorithm" label="负载均衡算法">
            <Select>
              <Option value="round-robin">轮询 (Round-Robin)</Option>
              <Option value="least-connections">最少连接 (Least Connections)</Option>
              <Option value="consistent-hashing">一致性哈希 (Consistent Hashing)</Option>
            </Select>
          </Form.Item>

          <Form.Item name="slots" label="插槽数">
            <InputNumber min={10} max={65536} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item name="tags" label="标签">
            <Select mode="tags" placeholder="输入标签后回车添加" style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="上游详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={[
          <Button key="close" onClick={() => setDetailVisible(false)}>
            关闭
          </Button>,
        ]}
        width={600}
      >
        {currentUpstream && (
          <div>
            <Title level={5}>基本信息</Title>
            <Space direction="vertical" style={{ width: '100%' }}>
              <div>
                <Text strong>ID: </Text>
                <Text code>{currentUpstream.id}</Text>
              </div>
              <div>
                <Text strong>名称: </Text>
                {currentUpstream.name}
              </div>
              <div>
                <Text strong>算法: </Text>
                <Tag color="blue">{currentUpstream.algorithm}</Tag>
              </div>
              <div>
                <Text strong>插槽数: </Text>
                {currentUpstream.slots}
              </div>
            </Space>

            <Divider />

            <Title level={5}>目标节点 ({targets.length})</Title>
            {targets.length > 0 ? (
              <List
                dataSource={targets}
                renderItem={(item) => (
                  <List.Item>
                    <List.Item.Meta
                      title={`${item.target}:${item.port}`}
                      description={
                        <Space>
                          <Tag color={item.enabled ? 'green' : 'default'}>
                            {item.enabled ? '启用' : '禁用'}
                          </Tag>
                          <Text type="secondary">权重: {item.weight}</Text>
                        </Space>
                      }
                    />
                  </List.Item>
                )}
              />
            ) : (
              <Text type="secondary">暂无目标节点</Text>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
};

export default UpstreamsPage;
