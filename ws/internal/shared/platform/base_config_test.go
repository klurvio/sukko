package platform

import (
	"testing"
)

func TestBaseConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  BaseConfig
		wantErr bool
	}{
		{
			name:    "valid defaults",
			config:  BaseConfig{LogLevel: "info", LogFormat: "json", Environment: "local"},
			wantErr: false,
		},
		{
			name:    "valid dev environment",
			config:  BaseConfig{LogLevel: "debug", LogFormat: "pretty", Environment: "dev"},
			wantErr: false,
		},
		{
			name:    "valid prod environment",
			config:  BaseConfig{LogLevel: "warn", LogFormat: "json", Environment: "prod"},
			wantErr: false,
		},
		{
			name:    "valid fatal log level",
			config:  BaseConfig{LogLevel: "fatal", LogFormat: "json", Environment: "local"},
			wantErr: false,
		},
		{
			name:    "valid custom environment",
			config:  BaseConfig{LogLevel: "error", LogFormat: "json", Environment: "custom-env"},
			wantErr: false,
		},
		{
			name:    "invalid log format text",
			config:  BaseConfig{LogLevel: "info", LogFormat: "text", Environment: "local"},
			wantErr: true,
		},
		{
			name:    "invalid log level",
			config:  BaseConfig{LogLevel: "invalid", LogFormat: "json", Environment: "local"},
			wantErr: true,
		},
		{
			name:    "invalid log format",
			config:  BaseConfig{LogLevel: "info", LogFormat: "xml", Environment: "local"},
			wantErr: true,
		},
		{
			name:    "empty environment",
			config:  BaseConfig{LogLevel: "info", LogFormat: "json", Environment: ""},
			wantErr: true,
		},
		{
			name:    "whitespace-only environment",
			config:  BaseConfig{LogLevel: "info", LogFormat: "json", Environment: "   "},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("BaseConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
