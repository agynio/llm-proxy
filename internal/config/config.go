package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddress         = ":8080"
	defaultLLMServiceAddress     = "llm:50051"
	defaultUsersServiceAddress   = "users:50051"
	defaultAuthzServiceAddress   = "authorization:50051"
	defaultZitiManagementAddress = "ziti-management:50051"
	defaultZitiLeaseInterval     = 1 * time.Minute
	defaultZitiEnrollmentTimeout = 5 * time.Minute
)

type Config struct {
	ListenAddress               string
	LLMServiceAddress           string
	UsersServiceAddress         string
	AuthorizationServiceAddress string
	ZitiManagementAddress       string
	ZitiEnabled                 bool
	ZitiLeaseRenewalInterval    time.Duration
	ZitiEnrollmentTimeout       time.Duration
}

func LoadConfigFromEnv() (*Config, error) {
	zitiEnabled, err := envBool("ZITI_ENABLED")
	if err != nil {
		return nil, err
	}

	zitiLeaseRenewalInterval, err := envDuration("ZITI_LEASE_RENEWAL_INTERVAL", defaultZitiLeaseInterval)
	if err != nil {
		return nil, err
	}
	if zitiLeaseRenewalInterval <= 0 {
		return nil, fmt.Errorf("ZITI_LEASE_RENEWAL_INTERVAL must be positive")
	}

	zitiEnrollmentTimeout, err := envDuration("ZITI_ENROLLMENT_TIMEOUT", defaultZitiEnrollmentTimeout)
	if err != nil {
		return nil, err
	}
	if zitiEnrollmentTimeout <= 0 {
		return nil, fmt.Errorf("ZITI_ENROLLMENT_TIMEOUT must be positive")
	}

	return &Config{
		ListenAddress:               envOrDefault("LISTEN_ADDRESS", defaultListenAddress),
		LLMServiceAddress:           envOrDefault("LLM_SERVICE_ADDRESS", defaultLLMServiceAddress),
		UsersServiceAddress:         envOrDefault("USERS_SERVICE_ADDRESS", defaultUsersServiceAddress),
		AuthorizationServiceAddress: envOrDefault("AUTHORIZATION_SERVICE_ADDRESS", defaultAuthzServiceAddress),
		ZitiManagementAddress:       envOrDefault("ZITI_MANAGEMENT_ADDRESS", defaultZitiManagementAddress),
		ZitiEnabled:                 zitiEnabled,
		ZitiLeaseRenewalInterval:    zitiLeaseRenewalInterval,
		ZitiEnrollmentTimeout:       zitiEnrollmentTimeout,
	}, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}

	return parsed, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", name, err)
	}

	return parsed, nil
}
