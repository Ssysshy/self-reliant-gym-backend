// Package config 在启动前准备运行配置。
package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv 从工作目录起向上最多查找三层 .env，把其中的键值写入进程环境。
// 已经存在的环境变量优先，便于容器、CI 或命令行临时覆盖本地文件。
// 找不到 .env 时保持原样，生产环境仍按容器注入的变量运行。
//
// 设了 ENV_FILE 就只读那一个文件，不再向上查找：
//
//	ENV_FILE=.env.production go run ./cmd/api
//
// 这是本地验证生产配置（以及 CI 指定测试配置）的开关。指定的文件读不到时直接退出，
// 不退回 .env —— 静默用开发配置连上库，比启动失败难查得多。
func LoadDotEnv() {
	if p := os.Getenv("ENV_FILE"); p != "" {
		if fi, err := os.Stat(p); err != nil || fi.IsDir() || !load(p) {
			log.Fatalf("ENV_FILE 指定的配置文件读不到：%s", p)
		}
		return
	}
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for depth := 0; depth < 3; depth++ {
		if load(filepath.Join(dir, ".env")) {
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func load(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, strings.Trim(strings.TrimSpace(value), `"'`))
	}
	return true
}
