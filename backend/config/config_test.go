package config

import "testing"

func TestS3ConfigEndpointState(t *testing.T) {
	tests := []struct {
		name              string
		config            S3Config
		hasEndpointConfig bool
		isConfigured      bool
	}{
		{name: "empty", config: S3Config{}, hasEndpointConfig: false, isConfigured: false},
		{name: "region only", config: S3Config{Region: "us-east-1"}, hasEndpointConfig: true, isConfigured: false},
		{name: "bucket only", config: S3Config{Bucket: "avatar-bucket"}, hasEndpointConfig: true, isConfigured: false},
		{
			name:              "complete",
			config:            S3Config{Region: "us-east-1", Bucket: "avatar-bucket"},
			hasEndpointConfig: true,
			isConfigured:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.config.HasEndpointConfig(); got != test.hasEndpointConfig {
				t.Fatalf("HasEndpointConfig() = %v, want %v", got, test.hasEndpointConfig)
			}
			if got := test.config.IsConfigured(); got != test.isConfigured {
				t.Fatalf("IsConfigured() = %v, want %v", got, test.isConfigured)
			}
		})
	}
}
