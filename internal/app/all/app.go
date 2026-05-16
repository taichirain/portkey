package all

import (
	"context"
	"fmt"

	"github.com/taichirain/portkey/internal/app/control"
	"github.com/taichirain/portkey/internal/app/data"
	"github.com/taichirain/portkey/internal/config"
	"go.uber.org/zap"
)

type App struct {
	controlApp *control.App
	dataApp    *data.App
	logger     *zap.Logger
}

func NewApp(ctx context.Context, cfg *config.Config, logger *zap.Logger) (*App, error) {
	logger.Info("初始化 All-in-One 模式")

	controlApp, err := control.NewApp(ctx, cfg, logger.Named("control"))
	if err != nil {
		return nil, fmt.Errorf("初始化 Control Plane 失败: %w", err)
	}

	dataApp, err := data.NewApp(ctx, cfg, logger.Named("data"))
	if err != nil {
		return nil, fmt.Errorf("初始化 Data Plane 失败: %w", err)
	}

	return &App{
		controlApp: controlApp,
		dataApp:    dataApp,
		logger:     logger,
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	a.logger.Info("启动 All-in-One 模式")

	if err := a.controlApp.Start(ctx); err != nil {
		return fmt.Errorf("启动 Control Plane 失败: %w", err)
	}

	if err := a.dataApp.Start(ctx); err != nil {
		return fmt.Errorf("启动 Data Plane 失败: %w", err)
	}

	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.logger.Info("停止 All-in-One 模式")

	if err := a.dataApp.Stop(ctx); err != nil {
		a.logger.Warn("停止 Data Plane 出错", zap.Error(err))
	}

	if err := a.controlApp.Stop(ctx); err != nil {
		a.logger.Warn("停止 Control Plane 出错", zap.Error(err))
	}

	return nil
}
