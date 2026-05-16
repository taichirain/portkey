import React, { useState, useEffect } from 'react';
import {
  Card,
  Table,
  Button,
  Modal,
  Form,
  Input,
  Tag,
  Space,
  message,
  Popconfirm,
  Typography,
  Divider,
  Descriptions,
  Result,
  Statistic,
  Row,
  Col,
  Alert,
} from 'antd';
import {
  PlusOutlined,
  RocketOutlined,
  HistoryOutlined,
  CheckCircleOutlined,
  SyncOutlined,
  EyeOutlined,
  RollbackOutlined,
  WarningOutlined,
  CloseCircleOutlined,
} from '@ant-design/icons';
import { apiService } from '../services/api';
import * as types from '../types';
import dayjs from 'dayjs';

const { Title, Text } = Typography;
const { TextArea } = Input;

interface OperationResult {
  type: 'success' | 'error' | 'warning';
  operation: 'publish' | 'rollback' | 'create' | 'validate';
  message: string;
  detail?: string;
  timestamp: string;
}

interface ApiError {
  response?: {
    data?: {
      error?: {
        message?: string;
        code?: string;
      };
    };
  };
  message?: string;
}

const RevisionsPage: React.FC = () => {
  const [revisions, setRevisions] = useState<types.Revision[]>([]);
  const [activeRevision, setActiveRevision] = useState<types.ActiveRevisionResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 10, total: 0 });
  const [createModalVisible, setCreateModalVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [currentRevision, setCurrentRevision] = useState<types.Revision | null>(null);
  const [validating, setValidating] = useState(false);
  const [validationResult, setValidationResult] = useState<types.ValidationResponse | null>(null);
  const [snapshotResult, setSnapshotResult] = useState<types.SnapshotResponse | null>(null);
  const [publishing, setPublishing] = useState(false);
  const [operationResult, setOperationResult] = useState<OperationResult | null>(null);
  const [form] = Form.useForm();

  const extractErrorMessage = (error: ApiError): { message: string; detail: string } => {
    const apiMessage = error?.response?.data?.error?.message;
    const apiCode = error?.response?.data?.error?.code;
    const defaultMessage = error?.message || '未知错误';

    let message = defaultMessage;
    let detail = '';

    if (apiMessage) {
      message = apiMessage;
    }

    if (apiCode) {
      detail = `错误代码: ${apiCode}`;
    }

    if (apiCode === 'NOT_FOUND') {
      message = '资源不存在';
      detail = apiMessage || '请检查您请求的资源是否存在';
    } else if (apiCode === 'VALIDATION_ERROR') {
      message = '参数验证失败';
      detail = apiMessage || '请检查输入的参数格式';
    } else if (apiCode === 'CONCURRENCY_ERROR' || apiMessage?.includes('并发') || apiMessage?.includes('conflict')) {
      message = '操作冲突';
      detail = '有其他用户正在执行相同操作，请稍后重试';
    } else if (apiMessage?.includes('锁') || apiMessage?.includes('lock')) {
      message = '资源被锁定';
      detail = '该操作正在被其他实例执行，请稍后重试';
    }

    return { message, detail };
  };

  const showOperationResult = (
    type: OperationResult['type'],
    operation: OperationResult['operation'],
    msg: string,
    detail?: string
  ) => {
    setOperationResult({
      type,
      operation,
      message: msg,
      detail,
      timestamp: new Date().toISOString(),
    });

    if (type === 'success') {
      message.success(msg);
    } else if (type === 'error') {
      message.error(msg);
    } else {
      message.warning(msg);
    }
  };

  const fetchRevisions = async (page = 1, pageSize = 10) => {
    setLoading(true);
    try {
      const response = await apiService.getRevisions(page, pageSize);
      setRevisions(response.items || []);
      setPagination({
        current: response.page,
        pageSize: response.page_size,
        total: response.total,
      });
    } catch (error) {
      console.error('Failed to fetch revisions:', error);
      message.error('获取版本列表失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchActiveRevision = async () => {
    try {
      const response = await apiService.getActiveRevision();
      setActiveRevision(response);
    } catch (error) {
      console.error('Failed to fetch active revision:', error);
    }
  };

  useEffect(() => {
    fetchRevisions();
    fetchActiveRevision();
  }, []);

  const handleValidate = async () => {
    setValidating(true);
    setValidationResult(null);
    try {
      const result = await apiService.validateConfig();
      setValidationResult(result);
      if (result.valid) {
        showOperationResult(
          'success',
          'validate',
          '配置验证通过',
          '当前配置没有错误，可以安全发布'
        );
      } else {
        showOperationResult(
          'error',
          'validate',
          `配置验证失败: 发现 ${result.errors?.length || 0} 个错误`,
          result.errors?.map((e, i) => `${i + 1}. ${e.field}: ${e.message}`).join('\n')
        );
      }
    } catch (error) {
      console.error('Failed to validate:', error);
      const { message: errMsg, detail } = extractErrorMessage(error as ApiError);
      showOperationResult('error', 'validate', `验证失败: ${errMsg}`, detail);
    } finally {
      setValidating(false);
    }
  };

  const handleCreateSnapshot = async () => {
    setValidating(true);
    try {
      const result = await apiService.createSnapshot();
      setSnapshotResult(result);
      showOperationResult(
        'success',
        'validate',
        '快照创建成功',
        `包含 ${result.services_count} 个服务, ${result.routes_count} 个路由, ${result.upstreams_count} 个上游`
      );
    } catch (error) {
      console.error('Failed to create snapshot:', error);
      const { message: errMsg, detail } = extractErrorMessage(error as ApiError);
      showOperationResult('error', 'validate', `创建快照失败: ${errMsg}`, detail);
    } finally {
      setValidating(false);
    }
  };

  const handleCreateRevision = async (values: types.CreateRevisionRequest) => {
    setPublishing(true);
    try {
      const result = await apiService.createRevision(values);
      showOperationResult(
        'success',
        'create',
        '版本创建成功',
        `版本 ${result.version || values.version} 已创建`
      );
      setCreateModalVisible(false);
      form.resetFields();
      fetchRevisions();
    } catch (error) {
      console.error('Failed to create revision:', error);
      const { message: errMsg, detail } = extractErrorMessage(error as ApiError);
      showOperationResult('error', 'create', `创建版本失败: ${errMsg}`, detail);
    } finally {
      setPublishing(false);
    }
  };

  const handleCreateAndPublish = async (values: types.CreateRevisionRequest) => {
    setPublishing(true);
    try {
      const result = await apiService.createAndPublish(values);
      showOperationResult(
        'success',
        'publish',
        '版本创建并发布成功',
        `版本 ${result.version || values.version} 已创建并发布为活跃版本`
      );
      setCreateModalVisible(false);
      form.resetFields();
      fetchRevisions();
      fetchActiveRevision();
    } catch (error) {
      console.error('Failed to create and publish:', error);
      const { message: errMsg, detail } = extractErrorMessage(error as ApiError);
      showOperationResult('error', 'publish', `创建并发布失败: ${errMsg}`, detail);
    } finally {
      setPublishing(false);
    }
  };

  const handlePublish = async (revisionId: string) => {
    setPublishing(true);
    try {
      const result = await apiService.publishRevision({ revision_id: revisionId });
      showOperationResult(
        'success',
        'publish',
        '发布成功',
        `版本 ${result.version || revisionId.slice(0, 8)} 已成功发布为活跃版本`
      );
      fetchRevisions();
      fetchActiveRevision();
    } catch (error) {
      console.error('Failed to publish:', error);
      const { message: errMsg, detail } = extractErrorMessage(error as ApiError);
      showOperationResult('error', 'publish', `发布失败: ${errMsg}`, detail);
    } finally {
      setPublishing(false);
    }
  };

  const handleRollback = async (revisionId: string) => {
    try {
      const result = await apiService.rollback({ target_revision_id: revisionId });
      showOperationResult(
        'success',
        'rollback',
        '回滚成功',
        `已回滚到版本 ${result.version || revisionId.slice(0, 8)}`
      );
      fetchRevisions();
      fetchActiveRevision();
    } catch (error) {
      console.error('Failed to rollback:', error);
      const { message: errMsg, detail } = extractErrorMessage(error as ApiError);
      showOperationResult('error', 'rollback', `回滚失败: ${errMsg}`, detail);
    }
  };

  const handleDetail = (record: types.Revision) => {
    setCurrentRevision(record);
    setDetailVisible(true);
  };

  const isActive = (revision: types.Revision) => {
    return activeRevision?.revision_id === revision.id;
  };

  const columns = [
    {
      title: '版本号',
      dataIndex: 'version',
      key: 'version',
      render: (text: string, record: types.Revision) => (
        <Space>
          <a onClick={() => handleDetail(record)}>{text}</a>
          {isActive(record) && (
            <Tag color="green">当前活跃</Tag>
          )}
        </Space>
      ),
    },
    {
      title: '描述',
      dataIndex: 'description',
      key: 'description',
      render: (text: string) => text || '-',
      ellipsis: true,
    },
    {
      title: '发布时间',
      dataIndex: 'published_at',
      key: 'published_at',
      render: (time: string | undefined) =>
        time ? dayjs(time).format('YYYY-MM-DD HH:mm:ss') : '-',
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
      render: (_: unknown, record: types.Revision) => (
        <Space size="small">
          <Button
            type="text"
            icon={<EyeOutlined />}
            onClick={() => handleDetail(record)}
          />
          {!isActive(record) && (
            <Popconfirm
              title="确定要发布这个版本吗？"
              onConfirm={() => handlePublish(record.id)}
              okText="确定"
              cancelText="取消"
            >
              <Button
                type="text"
                icon={<RocketOutlined />}
                loading={publishing}
              >
                发布
              </Button>
            </Popconfirm>
          )}
          {!isActive(record) && activeRevision && (
            <Popconfirm
              title="确定要回滚到这个版本吗？"
              onConfirm={() => handleRollback(record.id)}
              okText="确定"
              cancelText="取消"
            >
              <Button
                type="text"
                icon={<RollbackOutlined />}
              >
                回滚
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <Card
        title={
          <Space>
            <HistoryOutlined />
            配置发布
          </Space>
        }
        extra={
          <Space>
            <Button
              icon={<CheckCircleOutlined />}
              onClick={handleValidate}
              loading={validating}
            >
              验证配置
            </Button>
            <Button
              icon={<SyncOutlined />}
              onClick={handleCreateSnapshot}
              loading={validating}
            >
              创建快照
            </Button>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setCreateModalVisible(true)}
            >
              新建版本
            </Button>
          </Space>
        }
      >
        {validationResult && (
          <Card
            size="small"
            style={{ marginBottom: 16 }}
            title={
              <Space>
                <CheckCircleOutlined />
                配置验证结果
              </Space>
            }
          >
            {validationResult.valid ? (
              <Result
                status="success"
                title="配置验证通过"
                subTitle="当前配置没有错误，可以安全发布"
              />
            ) : (
              <Result
                status="error"
                title="配置验证失败"
                subTitle={`发现 ${validationResult.errors?.length} 个错误`}
              >
                <div>
                  {validationResult.errors?.map((err, index) => (
                    <Text key={index} type="danger" code>
                      {err.field}: {err.message}
                    </Text>
                  ))}
                </div>
              </Result>
            )}
          </Card>
        )}

        {snapshotResult && (
          <Card
            size="small"
            style={{ marginBottom: 16 }}
            title={
              <Space>
                <SyncOutlined />
                快照信息
              </Space>
            }
          >
            <Row gutter={16}>
              <Col span={6}>
                <Statistic
                  title="服务数量"
                  value={snapshotResult.services_count}
                />
              </Col>
              <Col span={6}>
                <Statistic
                  title="路由数量"
                  value={snapshotResult.routes_count}
                />
              </Col>
              <Col span={6}>
                <Statistic
                  title="上游数量"
                  value={snapshotResult.upstreams_count}
                />
              </Col>
              <Col span={6}>
                <Statistic
                  title="快照时间"
                  value={snapshotResult.timestamp}
                  valueStyle={{ fontSize: 14 }}
                />
              </Col>
            </Row>
          </Card>
        )}

        {activeRevision && (
          <Card
            size="small"
            style={{ marginBottom: 16 }}
            type="inner"
            title={
              <Space>
                <RocketOutlined style={{ color: '#52c41a' }} />
                当前活跃版本
              </Space>
            }
            extra={
              <Tag color="green">已发布</Tag>
            }
          >
            <Descriptions column={4} size="small">
              <Descriptions.Item label="版本号">
                <Text strong>{activeRevision.version}</Text>
              </Descriptions.Item>
              <Descriptions.Item label="描述">
                {activeRevision.description || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="发布时间">
                {activeRevision.published_at
                  ? dayjs(activeRevision.published_at).format('YYYY-MM-DD HH:mm:ss')
                  : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="快照内容">
                服务: {activeRevision.snapshot?.services?.length || 0} 个,
                路由: {activeRevision.snapshot?.routes?.length || 0} 个,
                上游: {activeRevision.snapshot?.upstreams?.length || 0} 个
              </Descriptions.Item>
            </Descriptions>
          </Card>
        )}

        {operationResult && (
          <Alert
            message={
              <Space>
                {operationResult.type === 'success' && <CheckCircleOutlined style={{ color: '#52c41a' }} />}
                {operationResult.type === 'error' && <CloseCircleOutlined style={{ color: '#ff4d4f' }} />}
                {operationResult.type === 'warning' && <WarningOutlined style={{ color: '#faad14' }} />}
                <Text strong>
                  {operationResult.operation === 'publish' && '发布操作'}
                  {operationResult.operation === 'rollback' && '回滚操作'}
                  {operationResult.operation === 'create' && '创建操作'}
                  {operationResult.operation === 'validate' && '验证操作'}
                </Text>
              </Space>
            }
            description={
              <div>
                <Text>{operationResult.message}</Text>
                {operationResult.detail && (
                  <div style={{ marginTop: 8 }}>
                    <Text type="secondary" code style={{ whiteSpace: 'pre-wrap' }}>
                      {operationResult.detail}
                    </Text>
                  </div>
                )}
                <Text type="secondary" style={{ fontSize: 12, marginTop: 8, display: 'block' }}>
                  时间: {dayjs(operationResult.timestamp).format('YYYY-MM-DD HH:mm:ss')}
                </Text>
              </div>
            }
            type={operationResult.type}
            showIcon
            closable
            onClose={() => setOperationResult(null)}
            style={{ marginBottom: 16 }}
          />
        )}

        <Divider />

        <Title level={5}>历史版本</Title>
        <Table
          columns={columns}
          dataSource={revisions}
          rowKey="id"
          loading={loading}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showQuickJumper: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => fetchRevisions(page, pageSize),
          }}
        />
      </Card>

      <Modal
        title="新建版本"
        open={createModalVisible}
        onCancel={() => setCreateModalVisible(false)}
        footer={null}
        width={600}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleCreateRevision}
        >
          <Form.Item
            name="version"
            label="版本号"
            rules={[{ required: true, message: '请输入版本号' }]}
          >
            <Input placeholder="例如: v1.0.0 或 2024.01.01" />
          </Form.Item>

          <Form.Item
            name="description"
            label="版本描述"
          >
            <TextArea
              rows={4}
              placeholder="描述这个版本的变更内容..."
            />
          </Form.Item>

          <Form.Item>
            <Space>
              <Button
                type="default"
                onClick={() => form.submit()}
                loading={publishing}
              >
                仅创建版本
              </Button>
              <Button
                type="primary"
                onClick={() => {
                  form.validateFields().then((values) => {
                    handleCreateAndPublish(values);
                  });
                }}
                loading={publishing}
                icon={<RocketOutlined />}
              >
                创建并发布
              </Button>
            </Space>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="版本详情"
        open={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={[
          <Button key="close" onClick={() => setDetailVisible(false)}>
            关闭
          </Button>,
        ]}
        width={700}
      >
        {currentRevision && (
          <div>
            <Descriptions bordered column={2}>
              <Descriptions.Item label="版本号" span={2}>
                <Text strong style={{ fontSize: 16 }}>
                  {currentRevision.version}
                </Text>
                {isActive(currentRevision) && (
                  <Tag color="green" style={{ marginLeft: 8 }}>
                    当前活跃
                  </Tag>
                )}
              </Descriptions.Item>
              <Descriptions.Item label="描述" span={2}>
                {currentRevision.description || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="创建时间">
                {dayjs(currentRevision.created_at).format('YYYY-MM-DD HH:mm:ss')}
              </Descriptions.Item>
              <Descriptions.Item label="发布时间">
                {currentRevision.published_at
                  ? dayjs(currentRevision.published_at).format('YYYY-MM-DD HH:mm:ss')
                  : '未发布'}
              </Descriptions.Item>
            </Descriptions>

            {currentRevision.snapshot && (
              <>
                <Divider />
                <Title level={5}>快照内容</Title>
                <Row gutter={16}>
                  <Col span={6}>
                    <Card size="small">
                      <Statistic
                        title="服务"
                        value={currentRevision.snapshot.services?.length || 0}
                      />
                    </Card>
                  </Col>
                  <Col span={6}>
                    <Card size="small">
                      <Statistic
                        title="路由"
                        value={currentRevision.snapshot.routes?.length || 0}
                      />
                    </Card>
                  </Col>
                  <Col span={6}>
                    <Card size="small">
                      <Statistic
                        title="上游"
                        value={currentRevision.snapshot.upstreams?.length || 0}
                      />
                    </Card>
                  </Col>
                  <Col span={6}>
                    <Card size="small">
                      <Statistic
                        title="目标"
                        value={currentRevision.snapshot.targets?.length || 0}
                      />
                    </Card>
                  </Col>
                </Row>
              </>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
};

export default RevisionsPage;
