package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var watchExt = map[string]bool{
	".go":   true,
	".html": true,
	".json": true,
	".css":  true,
	".js":   true,
}

type snapshot map[string]time.Time

func main() {
	log.SetFlags(log.Ltime)

	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goBin += ".exe"
	}

	if _, err := os.Stat(goBin); err != nil {
		goBin = "go"
	}

	log.Printf("dev watcher started at %s", root)
	log.Printf("open http://localhost%s", addrForLog())
	log.Printf("watching .go, .html, .json, .css, .js files")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proc := startServer(ctx, goBin)
	last, err := scan(root)
	if err != nil {
		log.Fatal(err)
	}

	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		next, err := scan(root)
		if err != nil {
			log.Printf("scan error: %v", err)
			continue
		}

		if changed(last, next) {
			log.Printf("change detected, restarting server")
			stopServer(proc)
			proc = startServer(ctx, goBin)
			last = next
		}
	}
}

func startServer(ctx context.Context, goBin string) *exec.Cmd {
	log.Println("building portal-dev.exe...")
	buildCmd := exec.Command(goBin, "build", "-o", "portal-dev.exe", "./cmd/portal")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		log.Printf("build failed: %v", err)
		return nil
	}

	cmd := exec.CommandContext(ctx, "./portal-dev.exe")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = devEnv(os.Environ())

	if err := cmd.Start(); err != nil {
		log.Printf("start server: %v", err)
		return nil
	}

	log.Printf("server process started pid=%d", cmd.Process.Pid)
	return cmd
}

func stopServer(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func scan(root string) (snapshot, error) {
	files := make(snapshot)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == ".tmp" || name == "tmp" || name == "node_modules" || name == "data" {
				return filepath.SkipDir
			}
			return nil
		}

		if !watchExt[strings.ToLower(filepath.Ext(path))] {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		files[path] = info.ModTime()
		return nil
	})
	return files, err
}

func changed(a, b snapshot) bool {
	if len(a) != len(b) {
		return true
	}

	for path, modA := range a {
		if modB, ok := b[path]; !ok || !modA.Equal(modB) {
			log.Printf("file changed: %s", path)
			return true
		}
	}

	for path := range b {
		if _, ok := a[path]; !ok {
			log.Printf("file added: %s", path)
			return true
		}
	}

	return false
}

func devEnv(base []string) []string {
	env := setDefaultEnv(base, "APP_ENV", "development")
	env = setDefaultEnv(env, "DEBUG", "true")
	env = setDefaultEnv(env, "ADDR", ":8080")
	env = setDefaultEnv(env, "DATABASE_URL", "postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable")
	return env
}

func setDefaultEnv(env []string, key, value string) []string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return env
		}
	}
	return append(env, fmt.Sprintf("%s=%s", key, value))
}

func addrForLog() string {
	addr := os.Getenv("ADDR")
	if addr == "" {
		return ":8080"
	}
	return addr
}
