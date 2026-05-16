第一阶段测试执行策略：快速验证核心功能（不需要数据库）
# 运行所有不需要数据库的单元/集成测试
go test ./tests/integration -run "TestM2|TestM3|TestM4|TestM5|TestM8|TestM10|TestM12" -v

# 运行 Dashboard E2E 测试（需要先启动后端）
cd dashboard
npm run test:e2e

第二阶段：完整验证（需要 PostgreSQL）
# 运行需要数据库的集成测试
go test -tags=integration ./tests/integration -run "TestM13|TestMultiCP" -v

### 第三阶段：手工 smoke 测试
1. Dashboard 完整流程 ：
   - 登录 → 创建上游/目标/服务/路由 → 创建 Traffic Policy → 发布版本 → 验证 DP 生效
2. 代理验证 ：
   - 发送真实请求经过 DP，验证插件、限流、灰度发布等功能
