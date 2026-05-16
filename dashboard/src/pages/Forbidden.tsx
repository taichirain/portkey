import React from 'react';
import { Result, Button } from 'antd';
import { LockOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';

const ForbiddenPage: React.FC = () => {
  const navigate = useNavigate();

  return (
    <Result
      status="403"
      icon={<LockOutlined />}
      title="权限不足"
      subTitle="抱歉，您没有权限访问此页面"
      extra={
        <Button type="primary" onClick={() => navigate('/')}>
          返回首页
        </Button>
      }
    />
  );
};

export default ForbiddenPage;
