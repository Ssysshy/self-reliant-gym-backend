package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 测试里要模拟"某个变量原本不存在"，t.Setenv 只能设值不能删除，所以手动还原
func unsetForTest(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, old)
			return
		}
		os.Unsetenv(key)
	})
}

func TestLoadDotEnvUsesEnvFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "custom.env")
	if err := os.WriteFile(p, []byte("# 注释行\nGYM_TEST_KEY=从指定文件\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unsetForTest(t, "GYM_TEST_KEY")
	t.Setenv("ENV_FILE", p)

	LoadDotEnv()

	if got := os.Getenv("GYM_TEST_KEY"); got != "从指定文件" {
		t.Fatalf("ENV_FILE 没被读取，GYM_TEST_KEY=%q", got)
	}
}

func TestEnvFileDoesNotOverrideExisting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "custom.env")
	if err := os.WriteFile(p, []byte("GYM_TEST_KEY=文件里的值\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 容器、CI 都是靠这个优先级覆盖文件的，不能反过来
	t.Setenv("ENV_FILE", p)
	t.Setenv("GYM_TEST_KEY", "命令行里的值")

	LoadDotEnv()

	if got := os.Getenv("GYM_TEST_KEY"); got != "命令行里的值" {
		t.Fatalf("已存在的环境变量被文件覆盖了，GYM_TEST_KEY=%q", got)
	}
}

// ENV_FILE 指错时必须直接退出，不能悄悄退回 .env 用开发配置连库
func TestMissingEnvFileFails(t *testing.T) {
	if os.Getenv("GYM_TEST_WANT_EXIT") == "1" {
		os.Setenv("ENV_FILE", filepath.Join(os.TempDir(), "不存在的配置文件.env"))
		LoadDotEnv()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMissingEnvFileFails")
	cmd.Env = append(os.Environ(), "GYM_TEST_WANT_EXIT=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("ENV_FILE 指向不存在的文件时进程没有退出")
	}
	if !strings.Contains(string(out), "ENV_FILE 指定的配置文件读不到") {
		t.Fatalf("退出信息没说清原因：%s", out)
	}
}
