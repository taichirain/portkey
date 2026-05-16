package config

import "fmt"

func Validate(cfg *Config) error {
	if cfg.Control.Port <= 0 || cfg.Control.Port > 65535 {
		return fmt.Errorf("control.port 必须在 1-65535 范围内")
	}
	if cfg.Data.Port <= 0 || cfg.Data.Port > 65535 {
		return fmt.Errorf("data.port 必须在 1-65535 范围内")
	}
	if cfg.Control.Port == cfg.Data.Port {
		return fmt.Errorf("control.port 和 data.port 不能相同")
	}
	return nil
}
