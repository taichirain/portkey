package config

type Config struct {
	Control     ControlConfig `yaml:"control"`
	Data        DataConfig    `yaml:"data"`
	Logging     LoggingConfig `yaml:"logging"`
	DPInstances []DPInstance  `yaml:"dp_instances"`
}

type DPInstance struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type ControlConfig struct {
	Host string         `yaml:"host"`
	Port int            `yaml:"port"`
	DB   PostgresConfig `yaml:"db"`
}

type DataConfig struct {
	Host       string         `yaml:"host"`
	Port       int            `yaml:"port"`
	ControlURL string         `yaml:"control_url"`
	Redis      RedisConfig    `yaml:"redis"`
}

type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	SSLMode  string `yaml:"sslmode"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type LoggingConfig struct {
	Level       string `yaml:"level"`
	Development bool   `yaml:"development"`
}

func Default() *Config {
	return &Config{
		Control: ControlConfig{
			Host: "127.0.0.1",
			Port: 8001,
			DB: PostgresConfig{
				Host:     "127.0.0.1",
				Port:     5432,
				User:     "postgres",
				Password: "",
				Database: "portkey",
				SSLMode:  "disable",
			},
		},
		Data: DataConfig{
			Host:       "127.0.0.1",
			Port:       8080,
			ControlURL: "http://127.0.0.1:8001",
			Redis: RedisConfig{
				Host: "127.0.0.1",
				Port: 6379,
				DB:   0,
			},
		},
		Logging: LoggingConfig{
			Level:       "info",
			Development: true,
		},
	}
}
