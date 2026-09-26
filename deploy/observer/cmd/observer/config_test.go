package main

import "testing"

// clearObserverEnv resets every OBSERVER_* and the AWS_REGION variable
// loadConfig reads, so each test starts from nothing.
func clearObserverEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"OBSERVER_PLATFORM",
		"OBSERVER_CLUSTER",
		"OBSERVER_APP_SERVICE",
		"OBSERVER_WEB_SERVICE",
		"OBSERVER_MAINTENANCE_SERVICE",
		"OBSERVER_BUCKET",
		"OBSERVER_REGION",
		"OBSERVER_NAMESPACE",
		"OBSERVER_S3_ENDPOINT",
		"OBSERVER_TUF_CACHE",
		"AWS_REGION",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadConfigRequiresBucket(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformECS)
	t.Setenv("OBSERVER_REGION", "ap-southeast-1")

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: got nil error with no OBSERVER_BUCKET")
	}
}

func TestLoadConfigRequiresRegion(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformECS)
	t.Setenv("OBSERVER_BUCKET", "aboutme-prod-transparency")

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: got nil error with no OBSERVER_REGION or AWS_REGION")
	}
}

func TestLoadConfigRegionFallsBackToAWSRegion(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformKubernetes)
	t.Setenv("OBSERVER_BUCKET", "aboutme-prod-transparency")
	t.Setenv("AWS_REGION", "ap-southeast-1")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Region != "ap-southeast-1" {
		t.Errorf("loadConfig: Region = %q, want ap-southeast-1", cfg.Region)
	}
}

func TestLoadConfigRequiresPlatform(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_BUCKET", "aboutme-prod-transparency")
	t.Setenv("OBSERVER_REGION", "ap-southeast-1")

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: got nil error with no OBSERVER_PLATFORM")
	}
}

func TestLoadConfigRejectsUnknownPlatform(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", "podman")
	t.Setenv("OBSERVER_BUCKET", "aboutme-prod-transparency")
	t.Setenv("OBSERVER_REGION", "ap-southeast-1")

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: got nil error for OBSERVER_PLATFORM=podman")
	}
}

func TestLoadConfigECSRequiresServices(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformECS)
	t.Setenv("OBSERVER_BUCKET", "aboutme-prod-transparency")
	t.Setenv("OBSERVER_REGION", "ap-southeast-1")
	t.Setenv("OBSERVER_CLUSTER", "aboutme-prod")

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: got nil error with no OBSERVER_APP_SERVICE")
	}
}

func TestLoadConfigECSComplete(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformECS)
	t.Setenv("OBSERVER_BUCKET", "aboutme-prod-transparency")
	t.Setenv("OBSERVER_REGION", "ap-southeast-1")
	t.Setenv("OBSERVER_CLUSTER", "aboutme-prod")
	t.Setenv("OBSERVER_APP_SERVICE", "aboutme-prod-app")
	t.Setenv("OBSERVER_WEB_SERVICE", "aboutme-prod-web")
	t.Setenv("OBSERVER_MAINTENANCE_SERVICE", "aboutme-prod-maintenance")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Namespace != defaultNamespace {
		t.Errorf("loadConfig: Namespace = %q, want default %q", cfg.Namespace, defaultNamespace)
	}
	if cfg.TUFCacheDir != defaultTUFCacheDir {
		t.Errorf("loadConfig: TUFCacheDir = %q, want default %q", cfg.TUFCacheDir, defaultTUFCacheDir)
	}
}

func TestLoadConfigKubernetesMinimal(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformKubernetes)
	t.Setenv("OBSERVER_BUCKET", "aboutme-prod-transparency")
	t.Setenv("OBSERVER_REGION", "HCM03")
	t.Setenv("OBSERVER_NAMESPACE", "aboutme")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Namespace != "aboutme" {
		t.Errorf("loadConfig: Namespace = %q, want aboutme", cfg.Namespace)
	}
}
