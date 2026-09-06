package config

import "testing"

func TestPrintConfigurationRestrictsPrivateListener(t *testing.T) {
	for _, test := range []struct {
		environment, address string
		ok                   bool
	}{
		{"dev", "127.0.0.1:20082", true}, {"dev", "127.0.0.1:20445", true},
		{"dev", "10.91.0.2:8081", true}, {"prod", "127.0.0.1:8081", true},
		{"staging", "127.0.0.1:8081", true}, {"prod", "10.91.0.2:8081", false},
		{"dev", "0.0.0.0:20082", false}, {"dev", "localhost:20082", false},
		{"dev", "127.0.0.1:20081", false}, {"prod", "127.0.0.1:20082", false},
		{"dev", "127.0.0.1:020082", false}, {"dev", "127.0.0.1:20082 ", false},
	} {
		t.Run(test.environment+"/"+test.address, func(t *testing.T) {
			_, _, err := loadPrintConfig(func(key string) string {
				if key == "PRINT_LISTEN_ADDR" {
					return test.address
				}
				return ""
			}, test.environment)
			if (err == nil) != test.ok {
				t.Fatalf("accepted=%v, want %v", err == nil, test.ok)
			}
		})
	}
}

func TestPrintConfigurationDefaultsAndExecutable(t *testing.T) {
	for _, environment := range []string{"dev", "staging", "prod"} {
		address, executable, err := loadPrintConfig(func(string) string { return "" }, environment)
		want := "127.0.0.1:8081"
		if environment == "dev" {
			want = "127.0.0.1:20082"
		}
		if err != nil || address != want || executable != "/opt/chromium/chrome" {
			t.Fatalf("defaults=%q %q %v", address, executable, err)
		}
	}
	for _, path := range []string{"chrome", "/tmp/../chrome", "/tmp/chrome\n", " /tmp/chrome", "/"} {
		_, _, err := loadPrintConfig(func(key string) string {
			if key == "CHROMIUM_PATH" {
				return path
			}
			return ""
		}, "dev")
		if err == nil {
			t.Fatalf("accepted unsafe executable path %q", path)
		}
	}
	_, executable, err := loadPrintConfig(func(key string) string {
		if key == "CHROMIUM_PATH" {
			return "/tmp/pinned-chrome"
		}
		return ""
	}, "dev")
	if err != nil || executable != "/tmp/pinned-chrome" {
		t.Fatal("rejected absolute executable path")
	}
}
