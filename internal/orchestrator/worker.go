package orchestrator

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Worker struct {
	ID        string
	Name      string
	Config    CollaboratorConfig
	Tags      []string
	Cmd       *exec.Cmd
	IsRunning bool
	stdin     io.WriteCloser // 用於在啟動後幫使用者輸入指令
	mu        sync.Mutex
}

type WorkerManager struct {
	workers []*Worker
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewWorkerManager(configs []CollaboratorConfig) *WorkerManager {
	ctx, cancel := context.WithCancel(context.Background())
	mgr := &WorkerManager{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, c := range configs {
		mgr.workers = append(mgr.workers, &Worker{
			ID:   c.ID,
			Name: c.Name,
			Config: c,
			Tags: c.Tags,
		})
	}

	return mgr
}

func (m *WorkerManager) StartAll() {
	for _, w := range m.workers {
		m.wg.Add(1)
		go m.runWorkerWithRestart(w)
	}
}

func (m *WorkerManager) runWorkerWithRestart(w *Worker) {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
			if err := m.startWorker(w); err != nil {
				log.Printf("[Worker:%s] failed to start: %v, retrying in 5s...", w.ID, err)
				time.Sleep(5 * time.Second)
				continue
			}

			// 監控子程序退出
			err := w.Cmd.Wait()
			w.mu.Lock()
			w.IsRunning = false
			if w.stdin != nil {
				w.stdin.Close()
			}
			w.mu.Unlock()

			if m.ctx.Err() != nil {
				log.Printf("[Worker:%s] stopped due to system shutdown", w.ID)
				return
			}

			log.Printf("[Worker:%s] process exited with error: %v. Restarting in 3s...", w.ID, err)
			time.Sleep(3 * time.Second)
		}
	}
}

func (m *WorkerManager) startWorker(w *Worker) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	cmd := exec.CommandContext(m.ctx, w.Config.Cmd, w.Config.Args...)
	
	// 設定 Stdin 以便啟動後注入指令
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	w.stdin = stdin

	cmd.Env = os.Environ()
	for k, v := range w.Config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	w.Cmd = cmd
	w.IsRunning = true

	// 🚀 自動注入技能指令：這相當於在啟動後幫你打入 "skills use [skill_name]"
	go func() {
		// 稍微等待 Agent 初始化
		time.Sleep(2 * time.Second)
		for _, skill := range w.Config.Skills {
			log.Printf("[Worker:%s] Auto-loading skill: %s", w.ID, skill)
			// 注意：根據 CLI 版本，語法可能是 "skill use" 或直接叫技能名，
			// 這裡我們暫定採用通用的啟動溝通模式
			fmt.Fprintf(w.stdin, "skills use %s\n", skill)
		}
	}()

	log.Printf("[Worker:%s] successfully started", w.ID)
	return nil
}

func (m *WorkerManager) StopAll() {
	m.cancel()
	m.wg.Wait()
}
