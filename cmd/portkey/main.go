package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/taichirain/portkey/internal/app/all"
	"github.com/taichirain/portkey/internal/app/control"
	"github.com/taichirain/portkey/internal/app/data"
	"github.com/taichirain/portkey/internal/config"
	"go.uber.org/zap"
)

func main() {
	mode := flag.String("mode", "all", "运行模式: control, data, all")
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	logger, err := initLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var app App
	switch *mode {
	case "control":
		app, err = control.NewApp(ctx, cfg, logger)
	case "data":
		app, err = data.NewApp(ctx, cfg, logger)
	case "all":
		app, err = all.NewApp(ctx, cfg, logger)
	default:
		err = fmt.Errorf("未知模式: %s", *mode)
	}

	if err != nil {
		logger.Fatal("初始化应用失败", zap.Error(err))
	}

	go func() {
		if err := app.Start(ctx); err != nil {
			logger.Error("应用启动失败", zap.Error(err))
			cancel()
		}
	}()

	select {
	case <-sigCh:
		logger.Info("收到关闭信号，开始优雅关闭")
	case <-ctx.Done():
		logger.Info("上下文已取消")
	}

	if err := app.Stop(context.Background()); err != nil {
		logger.Error("停止应用时出错", zap.Error(err))
	}

	logger.Info("应用已停止")
}

func initLogger(cfg *config.Config) (*zap.Logger, error) {
	if cfg.Logging.Development {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}

type App interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
