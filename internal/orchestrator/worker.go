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

	// 根據 Config 的 Skills 設定動態追加參數
	finalArgs := append([]string{}, w.Config.Args...)
	for _, skill := range w.Config.Skills {
		finalArgs = append(finalArgs, "--skill", skill)
	}

	cmd := exec.CommandContext(m.ctx, w.Config.Cmd, finalArgs...)
	
	cmd.Env = os.Environ()
	for k, v := range w.Config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// 目前為簡化演示，依舊將輸出導向全域 Stdout/Stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	w.Cmd = cmd
	w.IsRunning = true
	log.Printf("[Worker:%s] successfully started with skills %v", w.ID, w.Config.Skills)
	return nil
}

func (m *WorkerManager) StopAll() {
	m.cancel()
	m.wg.Wait()
}
