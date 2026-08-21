package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type S3Config struct {
	Region            string `yaml:"region"`
	Bucket            string `yaml:"bucket"`
	PresignTTLSeconds int    `yaml:"presignTTLSeconds"`
}

func (c S3Config) IsConfigured() bool {
	return c.Region != "" && c.Bucket != ""
}

func (c S3Config) HasEndpointConfig() bool {
	return c.Region != "" || c.Bucket != ""
}

type Config struct {
	Server struct {
		Port int `yaml:"port"`
	} `yaml:"server"`

	Cognito struct {
		AppClientId     string `yaml:"appClientId"`
		AppClientSecret string `yaml:"appClientSecret"`
		UserPoolId      string `yaml:"userPoolId"`
		Region          string `yaml:"region"`
	} `yaml:"cognito"`

	Openai struct {
		GptApiKey string `yaml:"gptApiKey"`
	} `yaml:"openai"`

	Gemini struct {
		ApiKey string `yaml:"apiKey"`
	} `yaml:"gemini"`

	Database struct {
		URI string `yaml:"uri"`
	} `yaml:"database"`

	Redis struct {
		Addr     string `yaml:"addr"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	} `yaml:"redis"`

	JWT struct {
		Secret string `yaml:"secret"`
		Expiry int    `yaml:"expiry"`
	} `yaml:"jwt"`

	SMTP struct {
		Host        string `yaml:"host"`
		Port        int    `yaml:"port"`
		Username    string `yaml:"username"`    // Gmail address
		Password    string `yaml:"password"`    // App Password
		SenderEmail string `yaml:"senderEmail"` // Same as Username for Gmail
		SenderName  string `yaml:"senderName"`
	} `yaml:"smtp"`

	GoogleOAuth struct {
		ClientID string `yaml:"clientID"`
	} `yaml:"googleOAuth"`

	S3 S3Config `yaml:"s3"`
}

// LoadConfig reads the configuration file
func LoadConfig(path string) (*Config, error) {

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	// Override with environment variables if present
	if envPort := os.Getenv("PORT"); envPort != "" {
		fmt.Sscanf(envPort, "%d", &cfg.Server.Port)
	}

	if envDB := os.Getenv("DATABASE_URI"); envDB != "" {
		cfg.Database.URI = envDB
	}
	if envGemini := os.Getenv("GEMINI_API_KEY"); envGemini != "" {
		cfg.Gemini.ApiKey = envGemini
	}
	if envJWT := os.Getenv("JWT_SECRET"); envJWT != "" {
		cfg.JWT.Secret = envJWT
	}
	if envGoogleClient := os.Getenv("GOOGLE_CLIENT_ID"); envGoogleClient != "" {
		cfg.GoogleOAuth.ClientID = envGoogleClient
	}
	if envAWSRegion := os.Getenv("AWS_REGION"); envAWSRegion != "" {
		cfg.S3.Region = envAWSRegion
	}
	if envS3Bucket := os.Getenv("AWS_S3_BUCKET"); envS3Bucket != "" {
		cfg.S3.Bucket = envS3Bucket
	}
	if envPresignTTL := os.Getenv("AWS_S3_PRESIGN_TTL_SECONDS"); envPresignTTL != "" {
		presignTTL, err := strconv.Atoi(envPresignTTL)
		if err != nil || presignTTL <= 0 {
			return nil, fmt.Errorf("AWS_S3_PRESIGN_TTL_SECONDS must be a positive integer")
		}
		cfg.S3.PresignTTLSeconds = presignTTL
	}
	if cfg.S3.PresignTTLSeconds == 0 {
		cfg.S3.PresignTTLSeconds = 300
	}
	// Add other overrides as needed

	return &cfg, nil
}
