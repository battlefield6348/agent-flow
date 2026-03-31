package orchestrator

import (
	"context"
	"fmt"
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
			ID:     c.ID,
			Name:   c.Name,
			Config: c,
			Tags:   c.Tags,
		})
	}

	return mgr
}

// StartAll 啟動所有配置中的子程序
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

	// 這裡使用 os/exec 來啟動子程序
	cmd := exec.CommandContext(m.ctx, w.Config.Cmd, w.Config.Args...)

	// 注入環境變數
	cmd.Env = os.Environ()
	for k, v := range w.Config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// 暫時將子程序的輸出導向到主程序的 stdout (用於調試)
	// 未來在 MCP 實作時，這部分會被重導向至我們內部的通訊管道
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	w.Cmd = cmd
	w.IsRunning = true
	log.Printf("[Worker:%s] successfully started: %s %v", w.ID, w.Config.Cmd, w.Config.Args)
	return nil
}

func (m *WorkerManager) StopAll() {
	m.cancel()
	m.wg.Wait()
}
