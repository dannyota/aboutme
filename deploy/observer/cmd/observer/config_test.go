package main

import "testing"

func clearObserverEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"OBSERVER_PLATFORM",
		"OBSERVER_REGION",
		"OBSERVER_TUF_CACHE",
		"OBSERVER_OBSERVATION_FILE",
		"OBSERVER_OUTPUT_DIR",
		"AWS_REGION",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadConfigRequiresRegion(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformPodman)

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: got nil error with no OBSERVER_REGION")
	}
}

func TestLoadConfigIgnoresAWSRegion(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformPodman)
	t.Setenv("AWS_REGION", "ap-southeast-1")

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: AWS_REGION stood in for OBSERVER_REGION")
	}
}

func TestLoadConfigRequiresPlatform(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_REGION", "HCM03")

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: got nil error with no OBSERVER_PLATFORM")
	}
}

func TestLoadConfigRejectsUnknownPlatform(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", "unknown")
	t.Setenv("OBSERVER_REGION", "HCM03")

	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig: got nil error for unknown OBSERVER_PLATFORM")
	}
}

func TestLoadConfigRejectsRemovedPlatforms(t *testing.T) {
	for _, name := range []string{"ecs", "kubernetes"} {
		t.Run(name, func(t *testing.T) {
			clearObserverEnv(t)
			t.Setenv("OBSERVER_PLATFORM", name)
			t.Setenv("OBSERVER_REGION", "HCM03")
			t.Setenv("OBSERVER_BUCKET", "aboutme-prod-transparency")
			t.Setenv("OBSERVER_CLUSTER", "aboutme-prod")
			t.Setenv("OBSERVER_APP_SERVICE", "aboutme-prod-app")
			t.Setenv("OBSERVER_WEB_SERVICE", "aboutme-prod-web")
			t.Setenv("OBSERVER_MAINTENANCE_SERVICE", "aboutme-prod-maintenance")

			if _, err := loadConfig(); err == nil {
				t.Fatalf("loadConfig: accepted OBSERVER_PLATFORM=%s", name)
			}
		})
	}
}

func TestLoadConfigPodmanNeedsNoBucket(t *testing.T) {
	clearObserverEnv(t)
	t.Setenv("OBSERVER_PLATFORM", platformPodman)
	t.Setenv("OBSERVER_REGION", "HCM03")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ObservationFile != "/observation/observation.json" || cfg.OutputDir != "/public" || cfg.TUFCacheDir != defaultTUFCacheDir {
		t.Fatalf("unexpected podman configuration: %+v", cfg)
	}
	t.Setenv("OBSERVER_OBSERVATION_FILE", "/custom/observation.json")
	t.Setenv("OBSERVER_OUTPUT_DIR", "/custom/public")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ObservationFile != "/custom/observation.json" || cfg.OutputDir != "/custom/public" {
		t.Fatal("podman paths did not honor configuration")
	}
}
