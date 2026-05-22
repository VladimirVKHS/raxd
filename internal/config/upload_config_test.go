package config_test

// upload_config_test.go — тесты валидации секции upload в конфиге (SR-81/SR-76/F-3).
//
// Покрывает:
//   - отсутствие config.yaml → upload-дефолты применяются (SR-81)
//   - max_file_bytes=0 → ошибка на старте (SR-76)
//   - max_file_bytes > потолка ((max_body_bytes-reserve)*3/4) → ошибка (SR-76)
//   - default_mode="04755" (setuid) → ошибка (ADR-003)
//   - default_mode="0666" (world-writable) → ошибка (ADR-003)

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladimirvkhs/raxd/internal/config"
)

// TestUploadConfigDefaults: без config.yaml upload-дефолты корректны (SR-81).
func TestUploadConfigDefaults(t *testing.T) {
	base := t.TempDir()
	p := config.PathSet{
		ConfigDir:  filepath.Join(base, "raxd"),
		ConfigFile: filepath.Join(base, "raxd", "config.yaml"), // не существует
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load() с отсутствующим config.yaml не должна возвращать ошибку: %v", err)
	}

	// Дефолт max_file_bytes = 716800 (700 KiB; SR-76/AC7).
	if cfg.Upload.MaxFileBytes != 716800 {
		t.Errorf("upload.max_file_bytes default = %d, want 716800", cfg.Upload.MaxFileBytes)
	}
	// Дефолт default_mode = "0600" (AC9/SR-73).
	if cfg.Upload.DefaultMode != "0600" {
		t.Errorf("upload.default_mode default = %q, want \"0600\"", cfg.Upload.DefaultMode)
	}
	// Дефолт overwrite_default = false (AC8).
	if cfg.Upload.OverwriteDefault {
		t.Error("upload.overwrite_default default = true, want false")
	}
	// Дефолт deny_root = false (SR-77).
	if cfg.Upload.DenyRoot {
		t.Error("upload.deny_root default = true, want false")
	}
	// Дефолт root = "" (serve.go резолвит к <StateDir>/uploads; AC5a).
	if cfg.Upload.Root != "" {
		t.Errorf("upload.root default = %q, want empty string", cfg.Upload.Root)
	}
}

// TestUploadMaxFileBytesZeroIsError: max_file_bytes=0 → ошибка на старте (SR-76).
func TestUploadMaxFileBytesZeroIsError(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("upload:\n  max_file_bytes: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("Load() с max_file_bytes=0 должна возвращать ошибку")
	}
	if !strings.Contains(err.Error(), "max_file_bytes") {
		t.Errorf("сообщение об ошибке должно упоминать max_file_bytes; got: %v", err)
	}
	t.Logf("SR-76: OK — max_file_bytes=0 → ошибка: %v", err)
}

// TestUploadMaxFileBytesNegativeIsError: max_file_bytes=-1 → ошибка (SR-76).
func TestUploadMaxFileBytesNegativeIsError(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("upload:\n  max_file_bytes: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("Load() с max_file_bytes=-1 должна возвращать ошибку")
	}
	if !strings.Contains(err.Error(), "max_file_bytes") {
		t.Errorf("сообщение об ошибке должно упоминать max_file_bytes; got: %v", err)
	}
	t.Logf("SR-76: OK — max_file_bytes=-1 → ошибка: %v", err)
}

// TestUploadMaxFileBytesExceedsCeilingIsError: max_file_bytes > потолка (SR-76).
//
// Потолок = (max_body_bytes - 1024) * 3 / 4.
// При max_body_bytes=1048576 (1 MiB, дефолт): потолок = (1048576 - 1024) * 3 / 4 = 785664.
// Ставим max_file_bytes=785856 > 785664 → ошибка.
func TestUploadMaxFileBytesExceedsCeilingIsError(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	// 785856 > ceiling (785664) при дефолтном max_body_bytes=1048576.
	yaml := "upload:\n  max_file_bytes: 785856\n"
	if err := os.WriteFile(cfgFile, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("Load() с max_file_bytes > потолка должна возвращать ошибку")
	}
	if !strings.Contains(err.Error(), "max_file_bytes") {
		t.Errorf("сообщение об ошибке должно упоминать max_file_bytes; got: %v", err)
	}
	t.Logf("SR-76: OK — max_file_bytes > ceiling → ошибка: %v", err)
}

// TestUploadMaxFileBytesAtCeilingIsOK: max_file_bytes = потолку — допустимо (SR-76).
func TestUploadMaxFileBytesAtCeilingIsOK(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	// Потолок при max_body_bytes=1048576: (1048576-1024)*3/4 = 785664.
	yaml := "upload:\n  max_file_bytes: 785664\n"
	if err := os.WriteFile(cfgFile, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load() с max_file_bytes=785664 (=потолку) не должна возвращать ошибку: %v", err)
	}
	if cfg.Upload.MaxFileBytes != 785664 {
		t.Errorf("MaxFileBytes = %d, want 785664", cfg.Upload.MaxFileBytes)
	}
	t.Logf("SR-76: OK — max_file_bytes=ceiling допустимо")
}

// TestUploadDefaultModeSetuidIsError: default_mode="04755" (setuid) → ошибка (ADR-003).
func TestUploadDefaultModeSetuidIsError(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("upload:\n  default_mode: \"04755\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("Load() с default_mode=04755 (setuid) должна возвращать ошибку")
	}
	if !strings.Contains(err.Error(), "default_mode") {
		t.Errorf("сообщение об ошибке должно упоминать default_mode; got: %v", err)
	}
	t.Logf("ADR-003: OK — default_mode=04755 → ошибка: %v", err)
}

// TestUploadDefaultModeSetgidIsError: default_mode="02755" (setgid) → ошибка (ADR-003).
func TestUploadDefaultModeSetgidIsError(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("upload:\n  default_mode: \"02755\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("Load() с default_mode=02755 (setgid) должна возвращать ошибку")
	}
	if !strings.Contains(err.Error(), "default_mode") {
		t.Errorf("сообщение об ошибке должно упоминать default_mode; got: %v", err)
	}
	t.Logf("ADR-003: OK — default_mode=02755 → ошибка: %v", err)
}

// TestUploadDefaultModeWorldWritableIsError: default_mode="0666" (world-writable) → ошибка (ADR-003).
func TestUploadDefaultModeWorldWritableIsError(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("upload:\n  default_mode: \"0666\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("Load() с default_mode=0666 (world-writable) должна возвращать ошибку")
	}
	if !strings.Contains(err.Error(), "default_mode") {
		t.Errorf("сообщение об ошибке должно упоминать default_mode; got: %v", err)
	}
	t.Logf("ADR-003: OK — default_mode=0666 → ошибка: %v", err)
}

// TestUploadDefaultModeWorldWritable0777IsError: default_mode="0777" → ошибка (ADR-003).
func TestUploadDefaultModeWorldWritable0777IsError(t *testing.T) {
	base := t.TempDir()
	cfgFile := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("upload:\n  default_mode: \"0777\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("Load() с default_mode=0777 должна возвращать ошибку")
	}
	t.Logf("ADR-003: OK — default_mode=0777 → ошибка: %v", err)
}

// TestUploadDefaultModeValidIsOK: допустимые дефолтные моды без ошибок (ADR-003).
func TestUploadDefaultModeValidIsOK(t *testing.T) {
	cases := []string{"0600", "0700", "0644", "0400", "0640"}
	for _, mode := range cases {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			cfgFile := filepath.Join(base, "config.yaml")
			yaml := "upload:\n  default_mode: \"" + mode + "\"\n"
			if err := os.WriteFile(cfgFile, []byte(yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			p := config.PathSet{ConfigDir: base, ConfigFile: cfgFile}
			_, err := config.Load(p)
			if err != nil {
				t.Errorf("Load() с default_mode=%s не должна возвращать ошибку; got: %v", mode, err)
			}
		})
	}
}
